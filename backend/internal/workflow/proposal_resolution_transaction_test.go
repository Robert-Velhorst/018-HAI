package workflow

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"automation-hub-backend/internal/lifeontology"
	"automation-hub-backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestPostgresProposalResolutionWaitsForSourceRetractionAndSeesQuarantine(t *testing.T) {
	db := workflowTransactionalPostgres(t)
	owner := "workflow-proposal-retraction-" + uuid.NewString()
	sourceID := "email-" + uuid.NewString()
	item, proposal := createProposalResolutionFixture(t, db, owner, sourceID, StateNeedsApproval, true)
	cleanupProposalResolutionFixture(t, db, item.ID)

	engine := NewService(NewGormRepository(db)).(*service)
	retractionTx := db.Begin()
	if retractionTx.Error != nil {
		t.Fatalf("begin source-retraction transaction: %v", retractionTx.Error)
	}
	t.Cleanup(func() { _ = retractionTx.Rollback().Error })
	projection, err := engine.RetractSourceInTransaction(
		retractionTx,
		owner,
		owner,
		"email",
		sourceID,
		"source removed during proposal resolution",
	)
	if err != nil {
		_ = retractionTx.Rollback().Error
		t.Fatalf("retract source in transaction: %v", err)
	}
	if projection != nil {
		t.Fatal("unexpected projection without a configured graph projector")
	}

	resolutionStarted := make(chan struct{})
	resolutionResult := make(chan error, 1)
	go func() {
		close(resolutionStarted)
		_, resolveErr := engine.ResolveProposal(item.ID, proposal.ID, ProposalResolutionRequest{
			Status: "approved", Actor: owner,
		})
		resolutionResult <- resolveErr
	}()
	<-resolutionStarted
	waitForWorkflowRowLockWait(t, db)

	if err := retractionTx.Commit().Error; err != nil {
		t.Fatalf("commit source retraction: %v", err)
	}
	if err := <-resolutionResult; !errors.Is(err, ErrWorkflowSourceRetracted) {
		t.Fatalf("proposal resolution error = %v, want ErrWorkflowSourceRetracted after the lock is released", err)
	}

	storedItem, err := NewGormRepository(db).FindItem(item.ID)
	if err != nil || storedItem.CurrentState != StateBlocked ||
		!strings.HasPrefix(storedItem.ApprovalReason, sourceRetractionQuarantinePrefix) {
		t.Fatalf("stored workflow = %#v, err=%v; want quarantined blocked state", storedItem, err)
	}
	storedProposals, err := NewGormRepository(db).FindProposals(item.ID)
	if err != nil || len(storedProposals) != 1 || storedProposals[0].Status != "open" {
		t.Fatalf("stored proposals = %#v, err=%v; want original proposal still open", storedProposals, err)
	}
	decisions, err := NewGormRepository(db).FindDecisions(item.ID)
	if err != nil || len(decisions) != 1 || decisions[0].DecisionType != "source_retraction" {
		t.Fatalf("stored decisions = %#v, err=%v; want only source-retraction decision", decisions, err)
	}
}

func TestPostgresConcurrentProposalResolutionsCommitExactlyOneDecision(t *testing.T) {
	db := workflowTransactionalPostgres(t)
	owner := "workflow-proposal-concurrent-" + uuid.NewString()
	item, proposal := createProposalResolutionFixture(t, db, owner, "", StateReady, false)
	cleanupProposalResolutionFixture(t, db, item.ID)
	engine := NewService(NewGormRepository(db))

	type result struct {
		status string
		err    error
	}
	start := make(chan struct{})
	results := make(chan result, 2)
	var workers sync.WaitGroup
	for _, status := range []string{"approved", "rejected"} {
		status := status
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			_, err := engine.ResolveProposal(item.ID, proposal.ID, ProposalResolutionRequest{
				Status: status, Actor: owner, Note: "concurrent " + status,
			})
			results <- result{status: status, err: err}
		}()
	}
	close(start)
	workers.Wait()
	close(results)

	winners := 0
	for result := range results {
		if result.err == nil {
			winners++
			continue
		}
		if !errors.Is(result.err, ErrWorkflowProposalAlreadyResolved) {
			t.Errorf("%s resolution error = %v, want the losing resolver to observe terminal proposal status", result.status, result.err)
		}
	}
	if winners != 1 {
		t.Fatalf("successful concurrent resolutions = %d, want exactly one", winners)
	}

	storedProposals, err := NewGormRepository(db).FindProposals(item.ID)
	if err != nil || len(storedProposals) != 1 ||
		(storedProposals[0].Status != "approved" && storedProposals[0].Status != "rejected") {
		t.Fatalf("stored proposals = %#v, err=%v; want one terminal proposal", storedProposals, err)
	}
	decisions, err := NewGormRepository(db).FindDecisions(item.ID)
	if err != nil || len(decisions) != 1 || decisions[0].DecisionType != "proposal" {
		t.Fatalf("stored decisions = %#v, err=%v; want exactly one proposal decision", decisions, err)
	}
	events, err := NewGormRepository(db).FindEvents(item.ID)
	proposalEvents := 0
	for _, event := range events {
		if event.EventType == "workflow.proposal" {
			proposalEvents++
		}
	}
	if err != nil || proposalEvents != 1 {
		t.Fatalf("proposal audit events = %d, err=%v; want exactly one", proposalEvents, err)
	}
}

func TestPostgresProposalResolutionRollsBackWhenAuditWriteFails(t *testing.T) {
	db := workflowTransactionalPostgres(t)
	owner := "workflow-proposal-rollback-" + uuid.NewString()
	item, proposal := createProposalResolutionFixture(t, db, owner, "", StateNeedsApproval, false)
	cleanupProposalResolutionFixture(t, db, item.ID)
	var projectionCalls atomic.Int32
	engine, err := WithLifeOntologyProjection(
		NewService(NewGormRepository(db)),
		proposalResolutionProjectionFunc(func(context.Context, lifeontology.OperationalProjectionRequest) (lifeontology.OperationalProjectionResult, error) {
			projectionCalls.Add(1)
			return lifeontology.OperationalProjectionResult{AdvisoryOnly: true}, nil
		}),
	)
	if err != nil {
		t.Fatalf("configure graph projection: %v", err)
	}

	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")
	functionName := "hai_test_fail_proposal_audit_" + suffix
	triggerName := "hai_test_fail_proposal_audit_" + suffix
	functionSQL := fmt.Sprintf(`CREATE FUNCTION %s() RETURNS trigger
LANGUAGE plpgsql AS $body$
BEGIN
  IF NEW.workflow_id = '%s'::uuid AND NEW.event_type = 'workflow.proposal' THEN
    RAISE EXCEPTION 'forced proposal audit failure';
  END IF;
  RETURN NEW;
END
$body$`, functionName, item.ID)
	if err := db.Exec(functionSQL).Error; err != nil {
		t.Fatalf("create proposal audit failure function: %v", err)
	}
	if err := db.Exec(fmt.Sprintf(
		"CREATE TRIGGER %s BEFORE INSERT ON workflow_events FOR EACH ROW EXECUTE FUNCTION %s()",
		triggerName,
		functionName,
	)).Error; err != nil {
		_ = db.Exec(fmt.Sprintf("DROP FUNCTION %s()", functionName)).Error
		t.Fatalf("create proposal audit failure trigger: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Exec(fmt.Sprintf("DROP TRIGGER IF EXISTS %s ON workflow_events", triggerName)).Error; err != nil {
			t.Errorf("drop proposal audit failure trigger: %v", err)
		}
		if err := db.Exec(fmt.Sprintf("DROP FUNCTION IF EXISTS %s()", functionName)).Error; err != nil {
			t.Errorf("drop proposal audit failure function: %v", err)
		}
	})

	_, err = engine.ResolveProposal(item.ID, proposal.ID, ProposalResolutionRequest{
		Status: "approved", Actor: owner, Note: "approve while injecting a workflow-scoped audit failure",
	})
	var auditErr *WorkflowAuditPersistenceError
	if !errors.As(err, &auditErr) || auditErr.Operation != "proposal event" {
		t.Fatalf("proposal resolution error = %v, want typed proposal audit-event persistence failure", err)
	}

	storedItem, err := NewGormRepository(db).FindItem(item.ID)
	if err != nil || storedItem.CurrentState != StateNeedsApproval {
		t.Fatalf("stored workflow = %#v, err=%v; want the pre-resolution state after rollback", storedItem, err)
	}
	storedProposals, err := NewGormRepository(db).FindProposals(item.ID)
	if err != nil || len(storedProposals) != 1 || storedProposals[0].Status != "open" {
		t.Fatalf("stored proposals = %#v, err=%v; want proposal still open after rollback", storedProposals, err)
	}
	transitions, err := NewGormRepository(db).FindTransitions(item.ID)
	if err != nil || len(transitions) != 0 {
		t.Fatalf("stored transitions = %#v, err=%v; want none after rollback", transitions, err)
	}
	decisions, err := NewGormRepository(db).FindDecisions(item.ID)
	if err != nil || len(decisions) != 0 {
		t.Fatalf("stored decisions = %#v, err=%v; want none after rollback", decisions, err)
	}
	events, err := NewGormRepository(db).FindEvents(item.ID)
	if err != nil || len(events) != 0 {
		t.Fatalf("stored events = %#v, err=%v; want none after rollback", events, err)
	}
	if projectionCalls.Load() != 0 {
		t.Fatalf("graph projection calls = %d, want none after rollback", projectionCalls.Load())
	}
}

func TestPostgresProposalResolutionProjectsCommittedState(t *testing.T) {
	db := workflowTransactionalPostgres(t)
	owner := "workflow-proposal-projection-" + uuid.NewString()
	item, proposal := createProposalResolutionFixture(t, db, owner, "", StateNeedsApproval, false)
	cleanupProposalResolutionFixture(t, db, item.ID)

	var projectionCalls atomic.Int32
	var projectedStatus lifeontology.LifecycleStatus
	engine, err := WithLifeOntologyProjection(
		NewService(NewGormRepository(db)),
		proposalResolutionProjectionFunc(func(_ context.Context, request lifeontology.OperationalProjectionRequest) (lifeontology.OperationalProjectionResult, error) {
			projectionCalls.Add(1)
			projectedStatus = request.Status
			return lifeontology.OperationalProjectionResult{AdvisoryOnly: true}, nil
		}),
	)
	if err != nil {
		t.Fatalf("configure graph projection: %v", err)
	}
	if _, err := engine.ResolveProposal(item.ID, proposal.ID, ProposalResolutionRequest{
		Status: "approved", Actor: owner,
	}); err != nil {
		t.Fatalf("resolve proposal: %v", err)
	}
	if projectionCalls.Load() != 1 || projectedStatus != lifeontology.StatusActive {
		t.Fatalf("post-commit graph projection calls=%d status=%q; want one projection of committed active state", projectionCalls.Load(), projectedStatus)
	}
}

func createProposalResolutionFixture(
	t *testing.T,
	db *gorm.DB,
	owner string,
	sourceID string,
	state string,
	requiresApproval bool,
) (*models.WorkflowItem, *models.WorkflowProposal) {
	t.Helper()
	approvalStatus := approvalStatus(false)
	if requiresApproval {
		approvalStatus = "pending"
	}
	item, err := NewGormRepository(db).CreateItem(&models.WorkflowItem{
		ID: uuid.New(), OwnerIdentity: owner, Title: "Atomic proposal resolution",
		Description: "Resolve a workflow proposal transactionally", CurrentState: state,
		TaskType: "administrative", RiskLevel: "low", AutonomyLevel: "manual",
		RequiresApproval: requiresApproval, ApprovalStatus: approvalStatus,
		SourceType: "email", SourceID: sourceID, SourceURI: "local://email/" + firstNonEmpty(sourceID, uuid.NewString()),
	})
	if err != nil {
		t.Fatalf("create workflow fixture: %v", err)
	}
	proposal, err := NewGormRepository(db).CreateProposal(&models.WorkflowProposal{
		ID: uuid.New(), WorkflowID: item.ID, RecommendedAction: "review the requested workflow action", Status: "open",
	})
	if err != nil {
		t.Fatalf("create proposal fixture: %v", err)
	}
	return item, proposal
}

func cleanupProposalResolutionFixture(t *testing.T, db *gorm.DB, workflowID uuid.UUID) {
	t.Helper()
	t.Cleanup(func() {
		for _, model := range []interface{}{
			&models.WorkflowProposal{}, &models.WorkflowEvent{}, &models.WorkflowDecision{}, &models.WorkflowTransition{},
		} {
			if err := db.Where("workflow_id = ?", workflowID).Delete(model).Error; err != nil {
				t.Errorf("clean up proposal workflow records: %v", err)
			}
		}
		if err := db.Where("id = ?", workflowID).Delete(&models.WorkflowItem{}).Error; err != nil {
			t.Errorf("clean up proposal workflow item: %v", err)
		}
	})
}

func waitForWorkflowRowLockWait(t *testing.T, db *gorm.DB) {
	t.Helper()
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var waits int64
		err := db.Raw(`SELECT COUNT(*)
FROM pg_stat_activity
WHERE datname = current_database()
  AND pid <> pg_backend_pid()
  AND wait_event_type = 'Lock'
  AND query ILIKE '%workflow_items%'`).Scan(&waits).Error
		if err != nil {
			t.Fatalf("inspect PostgreSQL workflow row-lock wait: %v", err)
		}
		if waits > 0 {
			return
		}
		select {
		case <-deadline.C:
			t.Fatal("proposal resolution never waited on the workflow row held by source retraction")
		case <-ticker.C:
		}
	}
}

type proposalResolutionProjectionFunc func(context.Context, lifeontology.OperationalProjectionRequest) (lifeontology.OperationalProjectionResult, error)

func (function proposalResolutionProjectionFunc) ProjectOperationalRecord(
	ctx context.Context,
	request lifeontology.OperationalProjectionRequest,
) (lifeontology.OperationalProjectionResult, error) {
	return function(ctx, request)
}
