package workflow

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"automation-hub-backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestPostgresReminderCandidatesAreOwnerScopedAndExcludeClosedWork(t *testing.T) {
	dsn := os.Getenv("HAI_TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("HAI_TEST_DATABASE_DSN not set; skipping Postgres reminder query test")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open Postgres: %v", err)
	}
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatalf("begin transaction: %v", tx.Error)
	}
	t.Cleanup(func() { _ = tx.Rollback().Error })

	repo := NewGormRepository(tx)
	reminderAt := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	create := func(owner, state, label string) uuid.UUID {
		item, createErr := repo.CreateItem(&models.WorkflowItem{
			ID:            uuid.New(),
			OwnerIdentity: owner,
			Title:         label,
			CurrentState:  state,
			TaskType:      "administrative",
			RiskLevel:     "low",
			AutonomyLevel: "manual",
		})
		if createErr != nil {
			t.Fatalf("create workflow: %v", createErr)
		}
		if _, createErr = repo.CreateChecklistItem(&models.WorkflowChecklistItem{
			ID:         uuid.New(),
			WorkflowID: item.ID,
			Label:      label,
			Status:     "open",
			ReminderAt: &reminderAt,
		}); createErr != nil {
			t.Fatalf("create reminder: %v", createErr)
		}
		return item.ID
	}
	wantedID := create("reminder-owner", StateReady, "owner reminder")
	create("foreign-owner", StateReady, "foreign reminder")
	create("reminder-owner", StateCompleted, "completed reminder")

	candidates, err := repo.FindReminderCandidatesForOwner(
		"reminder-owner", reminderAt.Add(time.Hour), 100,
	)
	if err != nil {
		t.Fatalf("find reminder candidates: %v", err)
	}
	if len(candidates) != 1 || candidates[0].Workflow.ID != wantedID ||
		candidates[0].Reminder.WorkflowID != wantedID {
		t.Fatalf("reminder candidates = %#v, want only owner workflow %s", candidates, wantedID)
	}
}

func TestPostgresWorkflowItemIdempotencyUsesURIWhenSourceIDIsMissing(t *testing.T) {
	dsn := os.Getenv("HAI_TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("HAI_TEST_DATABASE_DSN not set; skipping Postgres workflow URI idempotency test")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open Postgres: %v", err)
	}
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatalf("begin transaction: %v", tx.Error)
	}
	t.Cleanup(func() { _ = tx.Rollback().Error })
	repo := NewGormRepository(tx)

	owner := "workflow-uri-idempotency-" + uuid.NewString()
	first := &models.WorkflowItem{
		ID:             uuid.New(),
		OwnerIdentity:  owner,
		Title:          "URI-only source",
		Description:    "Create a checklist from this source",
		CurrentState:   StateReady,
		TaskType:       "administrative",
		RiskLevel:      "low",
		AutonomyLevel:  "execute_low_risk",
		SourceType:     "trello",
		SourceURI:      "https://trello.example/card/" + uuid.NewString(),
		SourceRevision: "revision-1",
	}
	created, firstInsert, err := repo.CreateItemIdempotent(first)
	if err != nil || !firstInsert || created == nil || created.ID != first.ID {
		t.Fatalf("first URI-only insert = (%#v, %t, %v), want inserted original item", created, firstInsert, err)
	}

	replay := *first
	replay.ID = uuid.New()
	got, replayInserted, err := repo.CreateItemIdempotent(&replay)
	if err != nil || replayInserted || got == nil || got.ID != first.ID {
		t.Fatalf("URI-only replay = (%#v, %t, %v), want existing item without insert", got, replayInserted, err)
	}

	revised := replay
	revised.ID = uuid.New()
	revised.SourceRevision = "revision-2"
	if _, _, err := repo.CreateItemIdempotent(&revised); err == nil {
		t.Fatal("URI-only identity accepted a different active source revision")
	}

	otherOwner := replay
	otherOwner.ID = uuid.New()
	otherOwner.OwnerIdentity = owner + "-other"
	otherOwner.SourceRevision = "revision-1"
	if got, inserted, err := repo.CreateItemIdempotent(&otherOwner); err != nil || !inserted || got == nil || got.ID != otherOwner.ID {
		t.Fatalf("other owner's URI-only insert = (%#v, %t, %v), want an independent item", got, inserted, err)
	}

	concurrentOwner := owner + "-concurrent"
	concurrentURI := "https://trello.example/card/" + uuid.NewString()
	t.Cleanup(func() {
		if err := db.Where("owner_identity = ?", concurrentOwner).Delete(&models.WorkflowItem{}).Error; err != nil {
			t.Errorf("clean up concurrent URI idempotency workflows: %v", err)
		}
	})
	type insertResult struct {
		item     *models.WorkflowItem
		inserted bool
		err      error
	}
	const workers = 8
	start := make(chan struct{})
	results := make(chan insertResult, workers)
	var wait sync.WaitGroup
	for index := 0; index < workers; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			candidate := &models.WorkflowItem{
				ID: uuid.New(), OwnerIdentity: concurrentOwner, Title: "Concurrent URI source",
				CurrentState: StateReady, TaskType: "administrative", RiskLevel: "low",
				AutonomyLevel: "execute_low_risk", SourceType: "trello",
				SourceURI: concurrentURI, SourceRevision: "concurrent-revision",
			}
			stored, inserted, createErr := NewGormRepository(db).CreateItemIdempotent(candidate)
			results <- insertResult{item: stored, inserted: inserted, err: createErr}
		}()
	}
	close(start)
	wait.Wait()
	close(results)
	createdCount := 0
	var sharedID uuid.UUID
	for result := range results {
		if result.err != nil {
			t.Errorf("concurrent URI-only insert: %v", result.err)
			continue
		}
		if result.item == nil {
			t.Error("concurrent URI-only insert returned no workflow")
			continue
		}
		if result.inserted {
			createdCount++
		}
		if sharedID == uuid.Nil {
			sharedID = result.item.ID
		} else if result.item.ID != sharedID {
			t.Errorf("concurrent URI-only insert returned workflow %s; want shared workflow %s", result.item.ID, sharedID)
		}
	}
	if createdCount != 1 {
		t.Errorf("concurrent URI-only inserts created %d workflows, want exactly one", createdCount)
	}
}

func TestPostgresSourceRetractionCannotBeReactivatedOrClaimed(t *testing.T) {
	db := workflowTransactionalPostgres(t)
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatalf("begin transaction: %v", tx.Error)
	}
	t.Cleanup(func() { _ = tx.Rollback().Error })
	repo := NewGormRepository(tx)
	owner := "workflow-source-retraction-guard-" + uuid.NewString()
	item, err := repo.CreateItem(&models.WorkflowItem{
		ID: uuid.New(), OwnerIdentity: owner, Title: "Previously approved source workflow",
		CurrentState: StateReady, TaskType: "administrative", RiskLevel: "low",
		AutonomyLevel: "execute_low_risk", RequiresApproval: true, ApprovalStatus: "approved",
		SourceType: "email", SourceID: uuid.NewString(), SourceURI: "local://source/retracted",
	})
	if err != nil {
		t.Fatalf("create source workflow: %v", err)
	}
	if _, err := repo.CreateEvent(&models.WorkflowEvent{
		WorkflowID: item.ID, EventType: "workflow.source_retracted", Trigger: "source_retraction",
	}); err != nil {
		t.Fatalf("create source retraction event: %v", err)
	}

	candidates, err := repo.FindRunnableItemsForOwner(owner, time.Now().UTC(), 10)
	if err != nil {
		t.Fatalf("find runnable workflows: %v", err)
	}
	for _, candidate := range candidates {
		if candidate.ID == item.ID {
			t.Fatal("source-retracted workflow remained in the runnable queue")
		}
	}
	if _, claimed, err := repo.ClaimRunnableItemForOwner(owner, item.ID, "stale-approval-worker", time.Now().UTC(), time.Now().UTC().Add(time.Minute)); err != nil || claimed {
		t.Fatalf("claim source-retracted workflow = (%t, %v), want no claim", claimed, err)
	}

	expected := *item
	updated := expected
	updated.NextAction = "attempted reactivation"
	if _, changed, err := repo.UpdateWorkflowItemCAS(&expected, &updated); err != nil || changed {
		t.Fatalf("CAS source-retracted workflow = (changed=%t, err=%v), want fail-closed conflict", changed, err)
	}
	stored, err := repo.FindItem(item.ID)
	if err != nil || stored.CurrentState != StateReady || stored.NextAction == "attempted reactivation" {
		t.Fatalf("source retraction guard changed persisted workflow: %#v err=%v", stored, err)
	}
}

func TestPostgresRetractedInterruptionAllowsEvidenceBackedCompletionButNotRetry(t *testing.T) {
	db := workflowTransactionalPostgres(t)
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatalf("begin transaction: %v", tx.Error)
	}
	t.Cleanup(func() { _ = tx.Rollback().Error })
	repo := NewGormRepository(tx)
	item, err := repo.CreateItem(&models.WorkflowItem{
		ID: uuid.New(), OwnerIdentity: "workflow-retracted-recovery-" + uuid.NewString(),
		Title: "Interrupted, then retracted", CurrentState: StateBlocked,
		TaskType: "administrative", RiskLevel: "low", AutonomyLevel: "execute_low_risk",
		RecoveryStatus: RecoveryNeedsReview, SourceType: "email", SourceID: uuid.NewString(),
		SourceURI: "local://source/interrupted-retracted",
	})
	if err != nil {
		t.Fatalf("create interrupted workflow: %v", err)
	}
	if _, err := repo.CreateEvent(&models.WorkflowEvent{WorkflowID: item.ID, EventType: "workflow.source_retracted"}); err != nil {
		t.Fatalf("create source retraction event: %v", err)
	}

	expected := *item
	retry := expected
	retry.CurrentState = StateReady
	retry.RecoveryStatus = RecoveryRetryConfirmed
	retryLink := &models.WorkflowSourceLink{WorkflowID: item.ID, SourceURI: "local://recovery/retry", Relationship: "execution_reconciliation"}
	retryEvidence := &models.WorkflowEvidenceClaim{
		WorkflowID: item.ID, ClaimText: "operator reconciled previous execution", SourceURI: retryLink.SourceURI,
		Reliability: "operator_attestation", Status: "human_approved",
	}
	if _, resolved, err := repo.ResolveInterruptedExecutionCAS(&expected, &retry, retryLink, retryEvidence, nil); err != nil || resolved {
		t.Fatalf("source-retracted retry resolution = (resolved=%t, err=%v), want blocked", resolved, err)
	}

	completed := expected
	completed.CurrentState = StateCompleted
	completed.VerificationStatus = "human_approved"
	completed.RecoveryStatus = RecoveryCompletionConfirmed
	completed.ApprovalStatus = approvalStatus(false)
	completedAt := time.Now().UTC()
	completed.CompletedAt = &completedAt
	completionURI := "local://recovery/completion"
	completionLink := &models.WorkflowSourceLink{
		WorkflowID: item.ID, SourceType: "recovery_evidence", SourceURI: completionURI,
		Relationship: "completion_evidence",
	}
	completionEvidence := &models.WorkflowEvidenceClaim{
		WorkflowID: item.ID, ClaimText: "operator confirmed the independently reconciled outcome",
		SourceURI: completionURI, Reliability: "operator_attestation", Status: "human_approved",
	}
	completionGate := &models.WorkflowQualityGate{
		WorkflowID: item.ID, Gate: "verification before completion", Status: "passed",
		Reason: "operator supplied independent completion evidence",
	}
	if _, resolved, err := repo.ResolveInterruptedExecutionCAS(&expected, &completed, completionLink, completionEvidence, completionGate); err != nil || !resolved {
		t.Fatalf("evidence-backed completion resolution = (resolved=%t, err=%v), want accepted", resolved, err)
	}
	stored, err := repo.FindItem(item.ID)
	if err != nil || stored.CurrentState != StateCompleted || stored.VerificationStatus != "human_approved" {
		t.Fatalf("reconciled completion=%#v err=%v", stored, err)
	}
}

func TestPostgresWorkflowApprovalResolutionIsCompareAndSwap(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("HAI_TEST_DATABASE_DSN"))
	if dsn == "" {
		t.Skip("HAI_TEST_DATABASE_DSN not set; skipping Postgres workflow approval race test")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open Postgres: %v", err)
	}
	owner := "workflow-approval-race-" + uuid.NewString()
	repo := NewGormRepository(db)
	item, err := repo.CreateItem(&models.WorkflowItem{
		ID: uuid.New(), OwnerIdentity: owner, Title: "Concurrent pending approval",
		CurrentState: StateNeedsApproval, TaskType: "legal", RiskLevel: "high",
		AutonomyLevel: "draft_only", RequiresApproval: true, ApprovalStatus: "pending",
	})
	if err != nil {
		t.Fatalf("create pending workflow: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Transaction(func(tx *gorm.DB) error {
			if err := tx.Where("workflow_id = ?", item.ID).Delete(&models.WorkflowEvent{}).Error; err != nil {
				return err
			}
			if err := tx.Where("workflow_id = ?", item.ID).Delete(&models.WorkflowDecision{}).Error; err != nil {
				return err
			}
			if err := tx.Where("workflow_id = ?", item.ID).Delete(&models.WorkflowTransition{}).Error; err != nil {
				return err
			}
			return tx.Where("id = ?", item.ID).Delete(&models.WorkflowItem{}).Error
		}); err != nil {
			t.Errorf("clean up workflow approval race item: %v", err)
		}
	})

	const contenders = 8
	start := make(chan struct{})
	type resolutionResult struct {
		item     *models.WorkflowItem
		resolved bool
		err      error
	}
	results := make(chan resolutionResult, contenders)
	var wait sync.WaitGroup
	for range contenders {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			updated, resolved, resolveErr := repo.ResolvePendingApproval(item.ID, ApprovalResolutionMutation{
				Approved: true, Actor: owner, DecisionRule: "manual approval gate",
				RejectionReason: "Concurrent approval test.",
			})
			results <- resolutionResult{item: updated, resolved: resolved, err: resolveErr}
		}()
	}
	close(start)
	wait.Wait()
	close(results)
	winners := 0
	for result := range results {
		if result.err != nil {
			t.Errorf("resolve concurrent approval: %v", result.err)
			continue
		}
		if result.resolved {
			winners++
			if result.item == nil || result.item.CurrentState != StateReady || result.item.ApprovalStatus != "approved" {
				t.Errorf("winning resolution returned invalid workflow: %#v", result.item)
			}
		} else if result.item != nil {
			t.Errorf("stale resolution returned an item: %#v", result.item)
		}
	}
	if winners != 1 {
		t.Fatalf("successful compare-and-swap resolutions = %d, want exactly one", winners)
	}
	final, err := repo.FindItem(item.ID)
	if err != nil {
		t.Fatalf("find resolved workflow: %v", err)
	}
	if final.CurrentState != StateReady || final.ApprovalStatus != "approved" {
		t.Fatalf("final state/status = %q/%q, want ready/approved", final.CurrentState, final.ApprovalStatus)
	}
	transitions, err := repo.FindTransitions(item.ID)
	if err != nil {
		t.Fatalf("find approval transitions: %v", err)
	}
	decisions, err := repo.FindDecisions(item.ID)
	if err != nil {
		t.Fatalf("find approval decisions: %v", err)
	}
	events, err := repo.FindEvents(item.ID)
	if err != nil {
		t.Fatalf("find approval events: %v", err)
	}
	if len(transitions) != 1 || transitions[0].Trigger != "approval_resolution" ||
		len(decisions) != 1 || decisions[0].Decision != "approved" ||
		len(events) != 1 || events[0].EventType != "workflow.approval" {
		t.Fatalf("approval lifecycle audit records are incomplete: transitions=%#v decisions=%#v events=%#v", transitions, decisions, events)
	}
}

func TestPostgresWorkflowApprovalEventFailureRollsBackResolution(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("HAI_TEST_DATABASE_DSN"))
	if dsn == "" {
		t.Skip("HAI_TEST_DATABASE_DSN not set; skipping Postgres workflow approval atomicity test")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open Postgres: %v", err)
	}
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatalf("begin transaction: %v", tx.Error)
	}
	t.Cleanup(func() { _ = tx.Rollback().Error })
	workflowID := uuid.New()
	constraintName := "chk_workflow_approval_event_failure_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	constraint := "CHECK (workflow_id <> '" + workflowID.String() + "'::uuid OR event_type <> 'workflow.approval')"
	if err := tx.Exec("ALTER TABLE workflow_events ADD CONSTRAINT " + constraintName + " " + constraint).Error; err != nil {
		t.Fatalf("install transaction-scoped event failure: %v", err)
	}

	repo := NewGormRepository(tx)
	owner := "workflow-approval-atomicity-" + uuid.NewString()
	item, err := repo.CreateItem(&models.WorkflowItem{
		ID: workflowID, OwnerIdentity: owner, Title: "Approval audit atomicity",
		CurrentState: StateNeedsApproval, TaskType: "administrative", RiskLevel: "low",
		AutonomyLevel: "manual", RequiresApproval: true, ApprovalStatus: "pending",
		SourceURI: "manual://approval-atomicity",
	})
	if err != nil {
		t.Fatalf("create pending workflow: %v", err)
	}

	updated, resolved, err := repo.ResolvePendingApproval(item.ID, ApprovalResolutionMutation{
		Approved: true, Actor: owner, DecisionRule: "manual approval gate",
		RejectionReason: "Approve only after durable audit succeeds.",
	})
	if err == nil || resolved || updated != nil {
		t.Fatalf("resolution = (%#v, %t, %v), want event persistence failure", updated, resolved, err)
	}
	persisted, err := repo.FindItem(item.ID)
	if err != nil {
		t.Fatalf("reload pending workflow: %v", err)
	}
	if persisted.CurrentState != StateNeedsApproval || persisted.ApprovalStatus != "pending" {
		t.Fatalf("state after event failure = %q/%q, want needs_approval/pending", persisted.CurrentState, persisted.ApprovalStatus)
	}
	transitions, err := repo.FindTransitions(item.ID)
	if err != nil {
		t.Fatalf("find transitions after rollback: %v", err)
	}
	decisions, err := repo.FindDecisions(item.ID)
	if err != nil {
		t.Fatalf("find decisions after rollback: %v", err)
	}
	events, err := repo.FindEvents(item.ID)
	if err != nil {
		t.Fatalf("find events after rollback: %v", err)
	}
	if len(transitions) != 0 || len(decisions) != 0 || len(events) != 0 {
		t.Fatalf("audit records escaped failed approval transaction: transitions=%#v decisions=%#v events=%#v", transitions, decisions, events)
	}
}

func TestWorkflowSourceAdvisoryLockKeyUsesUnambiguousPostgresTextEncoding(t *testing.T) {
	first := workflowSourceAdvisoryLockKey("a", "uri", "", "", "bc")
	second := workflowSourceAdvisoryLockKey("ab", "uri", "", "", "c")

	if strings.ContainsRune(first, '\x00') || strings.ContainsRune(second, '\x00') {
		t.Fatal("advisory lock key contains NUL, which PostgreSQL text parameters reject")
	}
	if first == second {
		t.Fatalf("distinct source identity tuples collided: %q", first)
	}
}

func TestPostgresReminderActivationRepositoryIsOwnerScopedReplayableAndLinear(t *testing.T) {
	dsn := os.Getenv("HAI_TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("HAI_TEST_DATABASE_DSN not set; skipping Postgres reminder activation test")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open Postgres: %v", err)
	}
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatalf("begin transaction: %v", tx.Error)
	}
	t.Cleanup(func() { _ = tx.Rollback().Error })

	repo := NewGormRepository(tx)
	activationRepo, ok := repo.(reminderActivationRepository)
	if !ok {
		t.Fatal("Postgres workflow repository does not expose durable reminder activation storage")
	}
	owner := "activation-owner-" + uuid.NewString() + "@example.com"
	workflow, err := repo.CreateItem(&models.WorkflowItem{
		ID: uuid.New(), OwnerIdentity: owner, Title: "Review internal reminder",
		CurrentState: StateReady, TaskType: "administrative", RiskLevel: "low",
		AutonomyLevel: "manual", RequiresApproval: true, ApprovalStatus: "pending",
	})
	if err != nil {
		t.Fatalf("create activation workflow: %v", err)
	}
	reminderAt := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	checklist, err := repo.CreateChecklistItem(&models.WorkflowChecklistItem{
		ID: uuid.New(), WorkflowID: workflow.ID, Label: "Review reminder internally",
		Status: "open", RequiresApproval: true, ReminderAt: &reminderAt,
	})
	if err != nil {
		t.Fatalf("create activation checklist: %v", err)
	}
	if foreign, loadErr := activationRepo.LoadReminderActivationSourceForOwner("foreign-"+owner, checklist.ID); loadErr != nil || foreign != nil {
		t.Fatalf("foreign owner activation source = %#v, %v", foreign, loadErr)
	}
	source, err := activationRepo.LoadReminderActivationSourceForOwner(owner, checklist.ID)
	if err != nil || source == nil {
		t.Fatalf("load owner activation source = %#v, %v", source, err)
	}
	digest, err := reminderEvidenceDigest(*source)
	if err != nil {
		t.Fatalf("digest activation source: %v", err)
	}

	activationService := NewService(repo).(ReminderActivationService)
	prepareRequest := ReminderActivationPrepareRequest{
		ExpectedReminderDigest: digest,
		IdempotencyKey:         "postgres:activation:" + checklist.ID.String(),
		ActivationKind:         ReminderActivationKindInternal,
		Confirmation:           ReminderActivationPrepareConfirmation,
	}
	prepared, err := activationService.PrepareReminderActivationForOwner(owner, owner, checklist.ID, prepareRequest)
	if err != nil {
		t.Fatalf("prepare activation: %v", err)
	}
	replayed, err := activationService.PrepareReminderActivationForOwner(owner, owner, checklist.ID, prepareRequest)
	if err != nil || !replayed.Replayed || replayed.Request.ID != prepared.Request.ID {
		t.Fatalf("activation replay = %#v, %v", replayed, err)
	}

	approveRequest := ReminderActivationDecisionRequest{
		Decision: ReminderActivationDecisionApproved, Reason: "Owner reviewed the internal reminder.",
		Confirmation:                    ReminderActivationApproveConfirmation,
		ExpectedActivationRequestDigest: prepared.Request.RecordDigest,
	}
	approved, err := activationService.DecideReminderActivationForOwner(owner, owner, prepared.Request.ID, approveRequest)
	if err != nil {
		t.Fatalf("approve activation preparation: %v", err)
	}
	approvedReplay, err := activationService.DecideReminderActivationForOwner(owner, owner, prepared.Request.ID, approveRequest)
	if err != nil || !approvedReplay.Replayed || approvedReplay.Decision.ID != approved.Decision.ID {
		t.Fatalf("activation decision replay = %#v, %v", approvedReplay, err)
	}
	revoked, err := activationService.DecideReminderActivationForOwner(owner, owner, prepared.Request.ID, ReminderActivationDecisionRequest{
		Decision: ReminderActivationDecisionRevoked, Reason: "Owner revoked the internal preparation.",
		Confirmation:                    ReminderActivationRevokeConfirmation,
		ExpectedActivationRequestDigest: prepared.Request.RecordDigest,
		ExpectedPreviousDecisionID:      approved.Decision.ID.String(),
	})
	if err != nil || revoked.CanExecute || revoked.Decision.PreviousDecisionID == nil ||
		*revoked.Decision.PreviousDecisionID != approved.Decision.ID {
		t.Fatalf("activation revocation = %#v, %v", revoked, err)
	}
}

func TestPostgresReminderDeliveryReplayUsesStableEvidenceNotGeneratedRecordIdentity(t *testing.T) {
	dsn := os.Getenv("HAI_TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("HAI_TEST_DATABASE_DSN not set; skipping Postgres reminder delivery replay test")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open Postgres: %v", err)
	}
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatalf("begin transaction: %v", tx.Error)
	}
	t.Cleanup(func() { _ = tx.Rollback().Error })

	repo := NewGormRepository(tx)
	owner := "delivery-owner-" + uuid.NewString() + "@example.com"
	workflow, err := repo.CreateItem(&models.WorkflowItem{
		ID: uuid.New(), OwnerIdentity: owner, Title: "Replay one internal reminder",
		CurrentState: StateReady, TaskType: "administrative", RiskLevel: "low", AutonomyLevel: "manual",
	})
	if err != nil {
		t.Fatal(err)
	}
	reminderAt := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	checklist, err := repo.CreateChecklistItem(&models.WorkflowChecklistItem{
		ID: uuid.New(), WorkflowID: workflow.ID, Label: "Review reminder replay", Status: "open", ReminderAt: &reminderAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	source, err := repo.(reminderActivationRepository).LoadReminderActivationSourceForOwner(owner, checklist.ID)
	if err != nil || source == nil {
		t.Fatalf("source=%#v err=%v", source, err)
	}
	digest, err := reminderEvidenceDigest(*source)
	if err != nil {
		t.Fatal(err)
	}
	engine := NewService(repo)
	activation := engine.(ReminderActivationService)
	delivery := engine.(ReminderDeliveryService)
	prepared, err := activation.PrepareReminderActivationForOwner(owner, owner, checklist.ID, ReminderActivationPrepareRequest{
		ExpectedReminderDigest: digest, IdempotencyKey: "postgres:delivery:prepare:" + checklist.ID.String(),
		ActivationKind: ReminderActivationKindInternal, Confirmation: ReminderActivationPrepareConfirmation,
	})
	if err != nil {
		t.Fatal(err)
	}
	approved, err := activation.DecideReminderActivationForOwner(owner, owner, prepared.Request.ID, ReminderActivationDecisionRequest{
		Decision: ReminderActivationDecisionApproved, Reason: "Authorize one internal reminder.",
		Confirmation: ReminderActivationApproveConfirmation, ExpectedActivationRequestDigest: prepared.Request.RecordDigest,
	})
	if err != nil {
		t.Fatal(err)
	}
	authorizeRequest := ReminderDeliveryAuthorizeRequest{
		ExpectedActivationRequestDigest: prepared.Request.RecordDigest, ExpectedActivationDecisionDigest: approved.Decision.RecordDigest,
		ExpectedReminderDigest: digest, IdempotencyKey: "postgres:delivery:authorize:" + checklist.ID.String(),
		Channel: ReminderDeliveryChannelInApp, Confirmation: ReminderDeliveryAuthorizeConfirmation,
	}
	authorized, err := delivery.AuthorizeReminderDeliveryForOwner(owner, owner, prepared.Request.ID, authorizeRequest)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := delivery.AuthorizeReminderDeliveryForOwner(owner, owner, prepared.Request.ID, authorizeRequest)
	if err != nil || !replayed.Replayed || replayed.Authorization.ID != authorized.Authorization.ID {
		t.Fatalf("authorization replay=%#v err=%v", replayed, err)
	}
	changedAuthorization := authorizeRequest
	changedAuthorization.IdempotencyKey = "postgres:delivery:changed:" + checklist.ID.String()
	if _, err = delivery.AuthorizeReminderDeliveryForOwner(owner, owner, prepared.Request.ID, changedAuthorization); err == nil {
		t.Fatal("one approved preparation must not create a second delivery authorization")
	}

	deliveryRepo := repo.(reminderDeliveryRepository)
	due, err := deliveryRepo.FindDueReminderDeliveryAuthorizations(owner, reminderAt.Add(time.Second), 10, ReminderDeliveryMaxAttempts)
	if err != nil {
		t.Fatalf("find due reminder delivery authorization: %v", err)
	}
	if len(due) != 1 || due[0].Authorization.ID != authorized.Authorization.ID || due[0].AttemptCount != 0 {
		t.Fatalf("due reminder delivery authorizations=%#v, want newly authorized reminder %s", due, authorized.Authorization.ID)
	}
	attempt := &models.WorkflowReminderDeliveryAttempt{
		ID: uuid.New(), AuthorizationID: authorized.Authorization.ID, OwnerIdentity: owner,
		AttemptNumber: 1, Status: ReminderDeliveryStatusRetryableFailure, Reason: "transient internal sink failure",
		ReminderDigest: digest, AuthorizationDigest: authorized.Authorization.RecordDigest,
		Authority: ReminderDeliveryAttemptAuthority, AttemptedAt: time.Now().UTC().Truncate(time.Microsecond),
	}
	attempt.RecordDigest, err = digestReminderActivationPayload(attempt)
	if err != nil {
		t.Fatal(err)
	}
	stored, created, err := deliveryRepo.SaveReminderDeliveryAttempt(attempt)
	if err != nil || !created {
		t.Fatalf("first receipt=%#v created=%v err=%v", stored, created, err)
	}
	retry := *attempt
	retry.ID = uuid.New()
	retry.AttemptedAt = retry.AttemptedAt.Add(time.Second)
	retry.RecordDigest, _ = digestReminderActivationPayload(&retry)
	replayedAttempt, created, err := deliveryRepo.SaveReminderDeliveryAttempt(&retry)
	if err != nil || created || replayedAttempt.ID != stored.ID {
		t.Fatalf("receipt replay=%#v created=%v err=%v", replayedAttempt, created, err)
	}
	conflict := retry
	conflict.ID = uuid.New()
	conflict.Status = ReminderDeliveryStatusSuppressed
	conflict.Reason = "different terminal evidence"
	conflict.RecordDigest, _ = digestReminderActivationPayload(&conflict)
	if _, _, err = deliveryRepo.SaveReminderDeliveryAttempt(&conflict); err == nil {
		t.Fatal("attempt-number reuse with different evidence must fail")
	}
}

func TestFrameworkSelectionProvenanceSurvivesPostgresRepositoryRoundTrip(t *testing.T) {
	dsn := os.Getenv("HAI_TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("HAI_TEST_DATABASE_DSN not set; skipping Postgres repository round-trip test")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open Postgres: %v", err)
	}
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatalf("begin transaction: %v", tx.Error)
	}
	t.Cleanup(func() {
		_ = tx.Rollback().Error
	})

	repo := NewGormRepository(tx)
	item, err := repo.CreateItem(&models.WorkflowItem{
		ID:            uuid.New(),
		OwnerIdentity: "postgres-owner",
		Title:         "Framework selection provenance round trip",
		Description:   "Verify durable workflow observability.",
		CurrentState:  StateReady,
		TaskType:      "administrative",
		RiskLevel:     "low",
		AutonomyLevel: "manual",
	})
	if err != nil {
		t.Fatalf("create workflow item: %v", err)
	}
	selection := testFrameworkSelection("postgres-round-trip-plan")
	runResult := &TaskRunResult{
		PlanID:             selection.TaskPlanID,
		CompletionStatus:   "validated",
		VerificationStatus: "verified",
		Passed:             true,
		FrameworkSelection: &selection,
	}
	engine := NewService(repo)
	implementation, ok := engine.(*service)
	if !ok {
		t.Fatalf("unexpected workflow service implementation %T", engine)
	}
	if err := implementation.storeTaskFrameworkSelection(item.ID, runResult); err != nil {
		t.Fatalf("store framework selection: %v", err)
	}

	decisions, err := repo.FindDecisions(item.ID)
	if err != nil {
		t.Fatalf("find decisions: %v", err)
	}
	decoded := frameworkSelectionsFromDecisions(decisions)
	if len(decoded) != 1 || decoded[0] != selection {
		t.Fatalf("Postgres decision round trip = %#v, want %#v", decoded, selection)
	}
	events, err := repo.FindEvents(item.ID)
	if err != nil {
		t.Fatalf("find events: %v", err)
	}
	foundEvent := false
	for _, event := range events {
		if event.EventType == frameworkSelectionEventType &&
			event.SourceURI == "framework-selection://"+selection.SelectionDecisionID {
			foundEvent = true
			break
		}
	}
	if !foundEvent {
		t.Fatalf("Postgres framework selection event missing: %#v", events)
	}
	detail, err := engine.GetForOwner("postgres-owner", item.ID)
	if err != nil {
		t.Fatalf("get owner workflow: %v", err)
	}
	if len(detail.FrameworkSelections) != 1 || detail.FrameworkSelections[0] != selection {
		t.Fatalf("owner API detail provenance = %#v", detail.FrameworkSelections)
	}
	if _, err := engine.GetForOwner("foreign-owner", item.ID); err == nil {
		t.Fatalf("foreign owner could retrieve Postgres selection provenance")
	}
}

func TestPostgresWorkflowApprovalDecisionLookupIsDurableAndOwnerScoped(t *testing.T) {
	dsn := os.Getenv("HAI_TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("HAI_TEST_DATABASE_DSN not set; skipping Postgres approval lookup test")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open Postgres: %v", err)
	}
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatalf("begin transaction: %v", tx.Error)
	}
	t.Cleanup(func() {
		_ = tx.Rollback().Error
	})

	repo := NewGormRepository(tx)
	alice, err := repo.CreateItem(&models.WorkflowItem{
		ID:               uuid.New(),
		OwnerIdentity:    "alice",
		Title:            "Alice durable approval",
		CurrentState:     StateReady,
		TaskType:         "administrative",
		RiskLevel:        "high",
		AutonomyLevel:    "approve_before_execute",
		RequiresApproval: true,
		ApprovalStatus:   "approved",
	})
	if err != nil {
		t.Fatalf("create Alice workflow: %v", err)
	}
	bob, err := repo.CreateItem(&models.WorkflowItem{
		ID:               uuid.New(),
		OwnerIdentity:    "bob",
		Title:            "Bob durable approval",
		CurrentState:     StateReady,
		TaskType:         "administrative",
		RiskLevel:        "high",
		AutonomyLevel:    "approve_before_execute",
		RequiresApproval: true,
		ApprovalStatus:   "approved",
	})
	if err != nil {
		t.Fatalf("create Bob workflow: %v", err)
	}

	digest := strings.Repeat("d", 64)
	binding := "automation-action:automation.docker.start:" + digest
	aliceDecision, err := repo.CreateDecision(&models.WorkflowDecision{
		ID:           uuid.New(),
		WorkflowID:   alice.ID,
		DecisionType: "approval",
		Decision:     "approved",
		Reason:       "Alice approved the exact Docker action",
		RuleApplied:  binding,
		Approved:     true,
		Actor:        "alice",
	})
	if err != nil {
		t.Fatalf("create Alice decision: %v", err)
	}
	bobDecision, err := repo.CreateDecision(&models.WorkflowDecision{
		ID:           uuid.New(),
		WorkflowID:   bob.ID,
		DecisionType: "approval",
		Decision:     "approved",
		Reason:       "Bob approved his exact Docker action",
		RuleApplied:  binding,
		Approved:     true,
		Actor:        "bob",
	})
	if err != nil {
		t.Fatalf("create Bob decision: %v", err)
	}
	rejectedDecision, err := repo.CreateDecision(&models.WorkflowDecision{
		ID:           uuid.New(),
		WorkflowID:   alice.ID,
		DecisionType: "approval",
		Decision:     "rejected",
		Reason:       "Alice rejected this action",
		RuleApplied:  binding,
		Approved:     false,
		Actor:        "alice",
	})
	if err != nil {
		t.Fatalf("create rejected decision: %v", err)
	}

	record, err := repo.FindApprovalDecisionForOwner(
		context.Background(),
		"alice",
		aliceDecision.ID.String(),
	)
	if err != nil {
		t.Fatalf("find Alice decision: %v", err)
	}
	if record.DecisionID != aliceDecision.ID.String() ||
		record.WorkflowID != alice.ID.String() ||
		record.OwnerIdentity != "alice" ||
		record.ActionBinding != binding ||
		record.Actor != "alice" ||
		!record.Approved {
		t.Fatalf("Postgres approval projection = %#v", record)
	}

	for _, test := range []struct {
		name       string
		owner      string
		decisionID string
	}{
		{name: "Alice cannot read Bob decision", owner: "alice", decisionID: bobDecision.ID.String()},
		{name: "Bob cannot read Alice decision", owner: "bob", decisionID: aliceDecision.ID.String()},
		{name: "invented decision", owner: "alice", decisionID: uuid.NewString()},
	} {
		t.Run(test.name, func(t *testing.T) {
			found, lookupErr := repo.FindApprovalDecisionForOwner(
				context.Background(),
				test.owner,
				test.decisionID,
			)
			if found != nil || !errors.Is(lookupErr, gorm.ErrRecordNotFound) {
				t.Fatalf("lookup = %#v, %v, want nil record-not-found", found, lookupErr)
			}
		})
	}

	rejected, err := repo.FindApprovalDecisionForOwner(
		context.Background(),
		"alice",
		rejectedDecision.ID.String(),
	)
	if err != nil {
		t.Fatalf("find rejected decision: %v", err)
	}
	if rejected.Approved || rejected.Decision != "rejected" {
		t.Fatalf("rejected Postgres projection = %#v", rejected)
	}

}
