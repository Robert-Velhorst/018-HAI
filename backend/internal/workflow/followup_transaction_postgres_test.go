package workflow

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/pgtestguard"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Never use HAI_TEST_DATABASE_DSN or the Compose database. The caller must
// explicitly own this disposable database. Each test owns an isolated schema.
func followUpPostgres(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := pgtestguard.RequireDedicatedPostgresTestDSN(t,
		"HAI_WORKFLOW_FOLLOWUP_TEST_DSN", "hai_workflow_followup_test")
	if os.Getenv("HAI_WORKFLOW_FOLLOWUP_TEST_DATABASE_OWNED") != "true" {
		t.Skip("HAI_WORKFLOW_FOLLOWUP_TEST_DATABASE_OWNED=true is required for an owned disposable database")
	}
	config, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatal("parse guarded follow-up PostgreSQL DSN")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	t.Cleanup(cancel)
	basePool := sql.OpenDB(stdlib.GetConnector(*config))
	t.Cleanup(func() { _ = basePool.Close() })
	base, err := gorm.Open(postgres.New(postgres.Config{Conn: basePool}), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal("open guarded follow-up PostgreSQL database")
	}
	schema := "hai_followup_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if err := base.WithContext(ctx).Exec("CREATE SCHEMA " + schema).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := base.Exec("DROP SCHEMA " + schema + " CASCADE").Error; err != nil {
			t.Errorf("clean up owned follow-up test schema: %v", err)
		}
	})
	config.RuntimeParams["search_path"] = schema
	config.RuntimeParams["application_name"] = schema
	pool := sql.OpenDB(stdlib.GetConnector(*config))
	pool.SetMaxOpenConns(8)
	t.Cleanup(func() { _ = pool.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal("open isolated follow-up test schema")
	}
	db = db.WithContext(ctx)
	// Model defaults need this function, but no shared extension is installed.
	if err := db.Exec("CREATE FUNCTION uuid_generate_v4() RETURNS uuid LANGUAGE sql AS 'SELECT pg_catalog.gen_random_uuid()'").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.WorkflowItem{}, &models.WorkflowOpenLoop{}, &models.WorkflowChecklistItem{},
		&models.WorkflowProposal{}, &models.WorkflowTransition{}, &models.WorkflowDecision{}, &models.WorkflowEvent{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func followUpPostgresFixture(t *testing.T, db *gorm.DB) (*GormRepository, models.WorkflowOpenLoop) {
	t.Helper()
	r := &GormRepository{DB: db}
	item, err := r.CreateItem(&models.WorkflowItem{
		ID: uuid.New(), OwnerIdentity: "alice", Title: "Transactional follow-up", CurrentState: StateWaitingInput,
		RiskLevel: "low", ApprovalStatus: "not_required", NextAction: "wait for response",
	})
	if err != nil {
		t.Fatal(err)
	}
	loop, err := r.CreateOpenLoop(&models.WorkflowOpenLoop{
		ID: uuid.New(), WorkflowID: item.ID, Status: "processing", ClaimID: "worker-a",
		LeaseUntil: timePtr(time.Now().Add(time.Minute)), FollowUpAt: timePtr(time.Now().Add(-time.Hour)),
		WaitingFor: "client response", NextAction: "prepare draft", ResponsibleParty: "assistant",
	})
	if err != nil {
		t.Fatal(err)
	}
	return r, *loop
}

func assertFollowUpProjectionCounts(t *testing.T, db *gorm.DB, workflowID uuid.UUID, want int64) {
	t.Helper()
	for _, model := range []interface{}{&models.WorkflowChecklistItem{}, &models.WorkflowProposal{}, &models.WorkflowTransition{}, &models.WorkflowDecision{}, &models.WorkflowEvent{}} {
		var count int64
		if err := db.Model(model).Where("workflow_id = ?", workflowID).Count(&count).Error; err != nil || count != want {
			t.Fatalf("%T count = %d, error = %v, want %d", model, count, err, want)
		}
	}
}

func waitForFollowUpLock(t *testing.T, db *gorm.DB) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var count int64
		if err := db.Raw(`SELECT count(*) FROM pg_stat_activity
WHERE datname = current_database() AND application_name = current_setting('application_name')
AND wait_event_type = 'Lock'`).Scan(&count).Error; err != nil {
			t.Fatal(err)
		}
		if count > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("follow-up worker did not observably wait on the owned row lock")
}

func TestPostgresFollowUpProjectionRollbackAtEveryWriteAndReplay(t *testing.T) {
	db := followUpPostgres(t)
	for _, table := range []string{"workflow_items", "workflow_checklist_items", "workflow_proposals", "workflow_transitions", "workflow_decisions", "workflow_events", "workflow_open_loops"} {
		t.Run(table, func(t *testing.T) {
			r, loop := followUpPostgresFixture(t, db)
			before, err := r.FindItem(loop.WorkflowID)
			if err != nil {
				t.Fatal(err)
			}
			filter := fmt.Sprintf("NEW.workflow_id = '%s'::uuid", loop.WorkflowID)
			if table == "workflow_items" {
				filter = fmt.Sprintf("NEW.id = '%s'::uuid", loop.WorkflowID)
			}
			if err := db.Exec(fmt.Sprintf(`CREATE FUNCTION fail_followup() RETURNS trigger LANGUAGE plpgsql AS $body$
BEGIN
  IF %s THEN RAISE EXCEPTION 'injected follow-up write failure'; END IF;
  RETURN NEW;
END $body$`, filter)).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Exec(fmt.Sprintf("CREATE TRIGGER fail_followup BEFORE INSERT OR UPDATE ON %s FOR EACH ROW EXECUTE FUNCTION fail_followup()", table)).Error; err != nil {
				t.Fatal(err)
			}
			commit, err := r.commitDueOpenLoop("alice", loop.WorkflowID, loop.ID, loop.ClaimID)
			if err == nil || commit != nil {
				t.Fatalf("injected failure returned commit=%+v err=%v", commit, err)
			}
			assertFollowUpProjectionCounts(t, db, loop.WorkflowID, 0)
			stored, err := r.FindItem(loop.WorkflowID)
			if err != nil || !reflect.DeepEqual(before, stored) {
				t.Fatalf("workflow state survived rollback: %+v, error=%v", stored, err)
			}
			var storedLoop models.WorkflowOpenLoop
			if err := db.First(&storedLoop, "id = ?", loop.ID).Error; err != nil || storedLoop.Status != "processing" || storedLoop.ClaimID != loop.ClaimID {
				t.Fatalf("loop claim changed on rollback: %+v, error=%v", storedLoop, err)
			}
			if err := db.Exec("DROP TRIGGER fail_followup ON " + table).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Exec("DROP FUNCTION fail_followup()").Error; err != nil {
				t.Fatal(err)
			}
			commit, err = r.commitDueOpenLoop("alice", loop.WorkflowID, loop.ID, loop.ClaimID)
			if err != nil || commit == nil || commit.Status != "triggered" || commit.Replayed {
				t.Fatalf("retry commit=%+v err=%v", commit, err)
			}
			assertFollowUpProjectionCounts(t, db, loop.WorkflowID, 1)
			// Model an acknowledgement lost after commit followed by later review.
			if err := db.Model(&models.WorkflowItem{}).Where("id = ?", loop.WorkflowID).
				Updates(map[string]interface{}{"current_state": StateBlocked, "recovery_status": RecoveryNeedsReview, "next_action": "review uncertain outcome"}).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Model(&models.WorkflowProposal{}).Where("workflow_id = ?", loop.WorkflowID).Update("status", "rejected").Error; err != nil {
				t.Fatal(err)
			}
			reviewed, err := r.FindItem(loop.WorkflowID)
			if err != nil {
				t.Fatal(err)
			}
			commit, err = r.commitDueOpenLoop("alice", loop.WorkflowID, loop.ID, loop.ClaimID)
			if err != nil || commit == nil || !commit.Replayed || !reflect.DeepEqual(reviewed, commit.Item) {
				t.Fatalf("replay changed reviewed workflow: commit=%+v err=%v", commit, err)
			}
			assertFollowUpProjectionCounts(t, db, loop.WorkflowID, 1)
			var proposal models.WorkflowProposal
			if err := db.Where("workflow_id = ?", loop.WorkflowID).Take(&proposal).Error; err != nil || proposal.Status != "rejected" {
				t.Fatalf("replay overwrote resolved proposal: %+v err=%v", proposal, err)
			}
		})
	}
}

func TestPostgresFollowUpProjectionConcurrentWorkersAndClaimRecovery(t *testing.T) {
	db := followUpPostgres(t)
	r, loop := followUpPostgresFixture(t, db)
	start := make(chan struct{})
	results := make(chan *followUpCommit, 12)
	errorsCh := make(chan error, 12)
	var workers sync.WaitGroup
	for i := 0; i < 12; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			commit, err := r.commitDueOpenLoop("alice", loop.WorkflowID, loop.ID, loop.ClaimID)
			results <- commit
			errorsCh <- err
		}()
	}
	close(start)
	workers.Wait()
	close(results)
	close(errorsCh)
	for err := range errorsCh {
		if err != nil {
			t.Fatalf("concurrent projection: %v", err)
		}
	}
	commits := 0
	for result := range results {
		if result == nil {
			t.Fatal("missing commit result")
		}
		if !result.Replayed {
			commits++
		}
	}
	if commits != 1 {
		t.Fatalf("new commits = %d, want exactly one", commits)
	}
	assertFollowUpProjectionCounts(t, db, loop.WorkflowID, 1)

	// A recovered/reclaimed processing loop with a receipt must not reapply
	// workflow state or create artifacts, even when it has a new claim ID.
	if err := db.Model(&models.WorkflowOpenLoop{}).Where("id = ?", loop.ID).
		Updates(map[string]interface{}{"status": "processing", "claim_id": "worker-b", "lease_until": time.Now().Add(time.Minute)}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := r.commitDueOpenLoop("alice", loop.WorkflowID, loop.ID, loop.ClaimID); !errors.Is(err, errFollowUpClaimLost) {
		t.Fatalf("stale worker commit error = %v", err)
	}
	if released, err := r.releaseDueOpenLoopClaim("alice", loop.WorkflowID, loop.ID, loop.ClaimID); err != nil || released {
		t.Fatalf("stale worker released replacement claim: %t, %v", released, err)
	}
	commit, err := r.commitDueOpenLoop("alice", loop.WorkflowID, loop.ID, "worker-b")
	if err != nil || commit == nil || !commit.Replayed {
		t.Fatalf("reclaimed replay commit=%+v err=%v", commit, err)
	}
	assertFollowUpProjectionCounts(t, db, loop.WorkflowID, 1)
}

func TestPostgresFollowUpProjectionConcurrentSchedulerClaims(t *testing.T) {
	db := followUpPostgres(t)
	r, loop := followUpPostgresFixture(t, db)
	if err := db.Model(&models.WorkflowOpenLoop{}).Where("id = ?", loop.ID).
		Updates(map[string]interface{}{"status": "open", "claim_id": "", "lease_until": nil}).Error; err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	type response struct {
		summary *OpenLoopRunSummary
		err     error
	}
	results := make(chan response, 12)
	var workers sync.WaitGroup
	for i := 0; i < 12; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			summary, err := NewService(r).RunDueOpenLoopsForOwner("alice", RunDueRequest{Limit: 5})
			results <- response{summary, err}
		}()
	}
	close(start)
	workers.Wait()
	close(results)
	triggered := 0
	for result := range results {
		if result.err != nil || result.summary == nil {
			t.Fatalf("scheduler result=%+v", result)
		}
		triggered += result.summary.Triggered
	}
	if triggered != 1 {
		t.Fatalf("scheduler triggers = %d, want exactly one", triggered)
	}
	assertFollowUpProjectionCounts(t, db, loop.WorkflowID, 1)
}

func TestPostgresFollowUpProjectionLeaseExpiryAfterLockWait(t *testing.T) {
	db := followUpPostgres(t)
	for _, table := range []string{"workflow_items", "workflow_open_loops"} {
		t.Run(table, func(t *testing.T) {
			r, loop := followUpPostgresFixture(t, db)
			if err := db.Model(&models.WorkflowOpenLoop{}).Where("id = ?", loop.ID).Update("lease_until", time.Now().Add(400*time.Millisecond)).Error; err != nil {
				t.Fatal(err)
			}
			lock := db.Begin()
			if lock.Error != nil {
				t.Fatal(lock.Error)
			}
			t.Cleanup(func() { _ = lock.Rollback().Error })
			id := loop.WorkflowID
			if table == "workflow_open_loops" {
				id = loop.ID
			}
			if err := lock.Exec("SELECT id FROM "+table+" WHERE id = ? FOR UPDATE", id).Error; err != nil {
				t.Fatal(err)
			}
			result := make(chan error, 1)
			go func() {
				_, err := r.commitDueOpenLoop("alice", loop.WorkflowID, loop.ID, loop.ClaimID)
				result <- err
			}()
			waitForFollowUpLock(t, db)
			// The held row prevents any projection until the stored lease expires.
			time.Sleep(600 * time.Millisecond)
			if err := lock.Commit().Error; err != nil {
				t.Fatal(err)
			}
			if err := <-result; !errors.Is(err, errFollowUpClaimLost) {
				t.Fatalf("projection after expired lock wait error = %v", err)
			}
			assertFollowUpProjectionCounts(t, db, loop.WorkflowID, 0)
			if released, err := r.releaseDueOpenLoopClaim("alice", loop.WorkflowID, loop.ID, loop.ClaimID); err != nil || released {
				t.Fatalf("expired claim released by stale worker: %t, %v", released, err)
			}
		})
	}
}

func TestPostgresFollowUpProjectionExpiryDuringWritesRollsBack(t *testing.T) {
	db := followUpPostgres(t)
	for _, target := range []struct{ table, operation string }{{"workflow_events", "INSERT"}, {"workflow_open_loops", "UPDATE"}} {
		t.Run(target.table, func(t *testing.T) {
			r, loop := followUpPostgresFixture(t, db)
			if err := db.Exec(`CREATE FUNCTION delay_followup() RETURNS trigger LANGUAGE plpgsql AS $body$
BEGIN PERFORM pg_sleep(0.6); RETURN NEW; END $body$`).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Model(&models.WorkflowOpenLoop{}).Where("id = ?", loop.ID).Update("lease_until", gorm.Expr("clock_timestamp() + interval '400 milliseconds'")).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Exec(fmt.Sprintf("CREATE TRIGGER delay_followup BEFORE %s ON %s FOR EACH ROW EXECUTE FUNCTION delay_followup()", target.operation, target.table)).Error; err != nil {
				t.Fatal(err)
			}
			if commit, err := r.commitDueOpenLoop("alice", loop.WorkflowID, loop.ID, loop.ClaimID); commit != nil || !errors.Is(err, errFollowUpClaimLost) {
				t.Fatalf("expired projection commit=%+v err=%v", commit, err)
			}
			assertFollowUpProjectionCounts(t, db, loop.WorkflowID, 0)
			item, err := r.FindItem(loop.WorkflowID)
			if err != nil || item.CurrentState != StateWaitingInput {
				t.Fatalf("expired projection workflow=%+v err=%v", item, err)
			}
			if err := db.Exec("DROP TRIGGER delay_followup ON " + target.table).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Exec("DROP FUNCTION delay_followup()").Error; err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestPostgresFollowUpProjectionSerializesWithRetractionAndApproval(t *testing.T) {
	db := followUpPostgres(t)
	for _, name := range []string{"retraction", "approval", "execution", "foreign_owner", "closed"} {
		t.Run(name, func(t *testing.T) {
			r, loop := followUpPostgresFixture(t, db)
			lock := db.Begin()
			if lock.Error != nil {
				t.Fatal(lock.Error)
			}
			t.Cleanup(func() { _ = lock.Rollback().Error })
			updates := map[string]interface{}{}
			switch name {
			case "retraction":
				updates["approval_reason"] = sourceRetractionQuarantinePrefix + "removed"
				updates["current_state"] = StateBlocked
			case "approval":
				updates["requires_approval"] = true
				updates["risk_level"] = "high"
			case "execution":
				updates["worker_claim_id"] = "execution-worker"
				updates["worker_lease_until"] = time.Now().Add(time.Minute)
				updates["current_state"] = StateInProgress
			case "foreign_owner":
				updates["owner_identity"] = "bob"
			case "closed":
				updates["current_state"] = StateCompleted
			}
			if err := lock.Model(&models.WorkflowItem{}).Where("id = ?", loop.WorkflowID).Updates(updates).Error; err != nil {
				t.Fatal(err)
			}
			type response struct {
				commit *followUpCommit
				err    error
			}
			results := make(chan response, 1)
			go func() {
				commit, err := r.commitDueOpenLoop("alice", loop.WorkflowID, loop.ID, loop.ClaimID)
				results <- response{commit, err}
			}()
			waitForFollowUpLock(t, db)
			if err := lock.Commit().Error; err != nil {
				t.Fatal(err)
			}
			result := <-results
			if name == "approval" {
				if result.err != nil || result.commit == nil || result.commit.Item.CurrentState != StateNeedsApproval || result.commit.Item.ApprovalStatus != "pending" {
					t.Fatalf("fresh approval state lost: %+v", result)
				}
				assertFollowUpProjectionCounts(t, db, loop.WorkflowID, 1)
			} else if name == "closed" {
				if result.err != nil || result.commit == nil || result.commit.Status != "resolved" {
					t.Fatalf("closed workflow projection: %+v", result)
				}
				checklist, err := r.FindChecklist(loop.WorkflowID)
				if err != nil || len(checklist) != 0 {
					t.Fatal("closed workflow created follow-up artifacts")
				}
			} else {
				if result.err == nil || result.commit != nil {
					t.Fatalf("unsafe projection = %+v", result)
				}
				assertFollowUpProjectionCounts(t, db, loop.WorkflowID, 0)
			}
		})
	}
}

func TestPostgresFollowUpProjectionRejectsAmbiguousLegacyAdoption(t *testing.T) {
	db := followUpPostgres(t)
	for _, kind := range []string{"checklist", "proposal"} {
		t.Run(kind, func(t *testing.T) {
			r, loop := followUpPostgresFixture(t, db)
			other := loop
			other.ID, other.ClaimID = uuid.New(), "worker-b"
			if _, err := r.CreateOpenLoop(&other); err != nil {
				t.Fatal(err)
			}
			legacyID := uuid.New()
			if kind == "checklist" {
				_, err := r.CreateChecklistItem(&models.WorkflowChecklistItem{
					ID: legacyID, WorkflowID: loop.WorkflowID, Status: "open",
					Label: "Resolve due open loop: " + compact(loop.WaitingFor, 160),
				})
				if err != nil {
					t.Fatal(err)
				}
			} else {
				_, err := r.CreateProposal(&models.WorkflowProposal{
					ID: legacyID, WorkflowID: loop.WorkflowID, Status: "open",
					RecommendedAction: "Follow-up due: " + firstNonEmpty(loop.NextAction, loop.WaitingFor),
				})
				if err != nil {
					t.Fatal(err)
				}
			}
			before, err := r.FindItem(loop.WorkflowID)
			if err != nil {
				t.Fatal(err)
			}
			for _, current := range []models.WorkflowOpenLoop{loop, other} {
				commit, err := r.commitDueOpenLoop("alice", current.WorkflowID, current.ID, current.ClaimID)
				if commit != nil || err == nil || !strings.Contains(err.Error(), "matches multiple") {
					t.Fatalf("ambiguous legacy projection commit=%+v err=%v", commit, err)
				}
			}
			after, err := r.FindItem(loop.WorkflowID)
			if err != nil || !reflect.DeepEqual(before, after) {
				t.Fatal("ambiguous legacy adoption changed workflow")
			}
			checklist, err := r.FindChecklist(loop.WorkflowID)
			if err != nil {
				t.Fatal(err)
			}
			proposals, err := r.FindProposals(loop.WorkflowID)
			if err != nil {
				t.Fatal(err)
			}
			if (kind == "checklist" && (len(checklist) != 1 || checklist[0].ID != legacyID || len(proposals) != 0)) ||
				(kind == "proposal" && (len(proposals) != 1 || proposals[0].ID != legacyID || len(checklist) != 0)) {
				t.Fatal("ambiguous legacy adoption created or replaced artifacts")
			}
			for _, model := range []interface{}{&models.WorkflowDecision{}, &models.WorkflowEvent{}, &models.WorkflowTransition{}} {
				var count int64
				if err := db.Model(model).Where("workflow_id = ?", loop.WorkflowID).Count(&count).Error; err != nil || count != 0 {
					t.Fatal("ambiguous legacy adoption left partial audit records")
				}
			}
		})
	}
}

func TestPostgresFollowUpProjectionPreservesAdoptedLegacyArtifactsForLaterLoop(t *testing.T) {
	db := followUpPostgres(t)
	r, loop := followUpPostgresFixture(t, db)
	legacyChecklist, err := r.CreateChecklistItem(&models.WorkflowChecklistItem{
		ID: uuid.New(), WorkflowID: loop.WorkflowID, Status: "open", Position: 27,
		Label: "Resolve due open loop: " + compact(loop.WaitingFor, 160),
	})
	if err != nil {
		t.Fatal(err)
	}
	legacyProposal, err := r.CreateProposal(&models.WorkflowProposal{
		ID: uuid.New(), WorkflowID: loop.WorkflowID, Status: "open", Options: "Operator's original options",
		RecommendedAction: "Follow-up due: " + firstNonEmpty(loop.NextAction, loop.WaitingFor),
	})
	if err != nil {
		t.Fatal(err)
	}
	if commit, err := r.commitDueOpenLoop("alice", loop.WorkflowID, loop.ID, loop.ClaimID); err != nil || commit == nil || commit.Replayed {
		t.Fatalf("unambiguous adoption commit=%+v err=%v", commit, err)
	}
	other := loop
	other.ID, other.ClaimID = uuid.New(), "worker-b"
	if _, err := r.CreateOpenLoop(&other); err != nil {
		t.Fatal(err)
	}
	if commit, err := r.commitDueOpenLoop("alice", other.WorkflowID, other.ID, other.ClaimID); commit != nil || err == nil || !strings.Contains(err.Error(), "matches multiple") {
		t.Fatalf("later loop reused already-adopted legacy artifacts: commit=%+v err=%v", commit, err)
	}
	checklist, err := r.FindChecklist(loop.WorkflowID)
	if err != nil || len(checklist) != 1 || checklist[0].ID != legacyChecklist.ID || checklist[0].Position != 27 {
		t.Fatalf("legacy checklist replaced or changed: %+v, error=%v", checklist, err)
	}
	proposals, err := r.FindProposals(loop.WorkflowID)
	if err != nil || len(proposals) != 1 || proposals[0].ID != legacyProposal.ID || proposals[0].Options != legacyProposal.Options {
		t.Fatalf("legacy proposal replaced or changed: %+v, error=%v", proposals, err)
	}
	assertFollowUpProjectionCounts(t, db, loop.WorkflowID, 1)
}

func TestPostgresFollowUpProjectionDistinctLoopsWithIdenticalText(t *testing.T) {
	db := followUpPostgres(t)
	r, loop := followUpPostgresFixture(t, db)
	other := loop
	other.ID, other.ClaimID = uuid.New(), "worker-b"
	if _, err := r.CreateOpenLoop(&other); err != nil {
		t.Fatal(err)
	}
	errorsCh := make(chan error, 2)
	for _, current := range []models.WorkflowOpenLoop{loop, other} {
		current := current
		go func() {
			commit, err := r.commitDueOpenLoop("alice", current.WorkflowID, current.ID, current.ClaimID)
			if err == nil && (commit == nil || commit.Replayed) {
				err = errors.New("distinct loop did not create its own projection")
			}
			errorsCh <- err
		}()
	}
	for i := 0; i < 2; i++ {
		if err := <-errorsCh; err != nil {
			t.Fatal(err)
		}
	}
	checklist, err := r.FindChecklist(loop.WorkflowID)
	if err != nil || len(checklist) != 2 || checklist[0].ID == checklist[1].ID {
		t.Fatalf("distinct loop checklists collapsed: %+v, error=%v", checklist, err)
	}
	proposals, err := r.FindProposals(loop.WorkflowID)
	if err != nil || len(proposals) != 2 || proposals[0].ID == proposals[1].ID {
		t.Fatalf("distinct loop proposals collapsed: %+v, error=%v", proposals, err)
	}
	for _, model := range []interface{}{&models.WorkflowDecision{}, &models.WorkflowEvent{}} {
		var count int64
		if err := db.Model(model).Where("workflow_id = ?", loop.WorkflowID).Count(&count).Error; err != nil || count != 2 {
			t.Fatalf("distinct loop %T count=%d error=%v", model, count, err)
		}
	}
}

func TestPostgresFollowUpProjectionEmergencyStopAfterClaim(t *testing.T) {
	db := followUpPostgres(t)
	r, loop := followUpPostgresFixture(t, db)
	t.Setenv("HAI_EMERGENCY_STOP", "true")
	commit, err := r.commitDueOpenLoop("alice", loop.WorkflowID, loop.ID, loop.ClaimID)
	if commit != nil || err == nil {
		t.Fatalf("emergency stop allowed follow-up projection: commit=%+v error=%v", commit, err)
	}
	assertFollowUpProjectionCounts(t, db, loop.WorkflowID, 0)
}

func TestPostgresFollowUpProjectionRejectsNestedTransaction(t *testing.T) {
	db := followUpPostgres(t)
	_, loop := followUpPostgresFixture(t, db)
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	defer tx.Rollback()
	commit, err := (&GormRepository{DB: tx}).commitDueOpenLoop("alice", loop.WorkflowID, loop.ID, loop.ClaimID)
	if commit != nil || err == nil || !strings.Contains(err.Error(), "root") {
		t.Fatalf("uncommitted nested transaction returned durable success: commit=%+v error=%v", commit, err)
	}
	assertFollowUpProjectionCounts(t, db, loop.WorkflowID, 0)
}
