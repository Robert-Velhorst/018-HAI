package workflow

import (
	"errors"
	"strings"
	"testing"

	"automation-hub-backend/internal/models"
)

type failingWorkflowAuditRepository struct {
	*fakeWorkflowRepo
	transitionErr error
	decisionErr   error
	eventErr      error
}

func (r *failingWorkflowAuditRepository) CreateTransition(transition *models.WorkflowTransition) (*models.WorkflowTransition, error) {
	if r.transitionErr != nil {
		return nil, r.transitionErr
	}
	return r.fakeWorkflowRepo.CreateTransition(transition)
}

func (r *failingWorkflowAuditRepository) CreateDecision(decision *models.WorkflowDecision) (*models.WorkflowDecision, error) {
	if r.decisionErr != nil {
		return nil, r.decisionErr
	}
	return r.fakeWorkflowRepo.CreateDecision(decision)
}

func (r *failingWorkflowAuditRepository) CreateEvent(event *models.WorkflowEvent) (*models.WorkflowEvent, error) {
	if r.eventErr != nil {
		return nil, r.eventErr
	}
	return r.fakeWorkflowRepo.CreateEvent(event)
}

func (r *failingWorkflowAuditRepository) CommitWorkflowIntake(finalization WorkflowIntakeFinalization) (*models.WorkflowItem, bool, error) {
	if r.transitionErr != nil {
		return nil, false, workflowAuditPersistenceFailure("intake transition", r.transitionErr)
	}
	if r.decisionErr != nil {
		return nil, false, workflowAuditPersistenceFailure("intake decisions", r.decisionErr)
	}
	if r.eventErr != nil {
		return nil, false, workflowAuditPersistenceFailure("intake events", r.eventErr)
	}
	return r.fakeWorkflowRepo.CommitWorkflowIntake(finalization)
}

func TestRetractSourceReportsIncompleteAuditPersistence(t *testing.T) {
	tests := []struct {
		name string
		fail func(*failingWorkflowAuditRepository, error)
	}{
		{name: "transition", fail: func(repo *failingWorkflowAuditRepository, err error) { repo.transitionErr = err }},
		{name: "decision", fail: func(repo *failingWorkflowAuditRepository, err error) { repo.decisionErr = err }},
		{name: "event", fail: func(repo *failingWorkflowAuditRepository, err error) { repo.eventErr = err }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			base := newFakeWorkflowRepo()
			repo := &failingWorkflowAuditRepository{fakeWorkflowRepo: base}
			service := NewService(repo)
			record, err := service.Intake(IntakeRequest{
				OwnerIdentity: "alice",
				Input:         "Follow up: prepare the project checklist.",
				SourceType:    "email",
				SourceID:      "audit-failure-" + test.name,
				SourceURI:     "local://message/audit-failure-" + test.name,
			})
			if err != nil {
				t.Fatalf("Intake: %v", err)
			}

			repoError := errors.New("injected " + test.name + " persistence failure")
			test.fail(repo, repoError)
			err = service.RetractSource("email", "audit-failure-"+test.name, "source was removed")
			if !errors.Is(err, repoError) || !strings.Contains(err.Error(), "audit history is incomplete") {
				t.Fatalf("RetractSource error = %v; want visible incomplete-audit failure", err)
			}
			stored := base.items[record.Item.ID]
			if stored == nil || stored.CurrentState != StateBlocked || !strings.HasPrefix(stored.ApprovalReason, sourceRetractionQuarantinePrefix) {
				t.Fatalf("source workflow was not left quarantined: %#v", stored)
			}
		})
	}
}

func TestRetractCompletedSourceReportsAuditPersistenceFailure(t *testing.T) {
	base := newFakeWorkflowRepo()
	repo := &failingWorkflowAuditRepository{fakeWorkflowRepo: base}
	service := NewService(repo)
	record, err := service.Intake(IntakeRequest{
		OwnerIdentity: "alice",
		Input:         "Prepare the project checklist.",
		SourceType:    "email",
		SourceID:      "completed-audit-failure",
		SourceURI:     "local://message/completed-audit-failure",
	})
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}
	base.items[record.Item.ID].CurrentState = StateCompleted
	repo.eventErr = errors.New("injected completed-source audit failure")

	err = service.RetractSource("email", "completed-audit-failure", "source was removed")
	if !errors.Is(err, repo.eventErr) || !strings.Contains(err.Error(), "completed workflow was retained") {
		t.Fatalf("RetractSource error = %v; want visible audit failure with retained completion", err)
	}
}

func TestWorkerDoesNotStartWhenClaimAuditCannotBePersisted(t *testing.T) {
	base := newFakeWorkflowRepo()
	repo := &failingWorkflowAuditRepository{fakeWorkflowRepo: base}
	runner := &fakeTaskRunner{result: &TaskRunResult{
		Passed: true, CompletionStatus: "completed", VerificationStatus: "verified",
	}}
	service := NewServiceWithTaskRunner(repo, runner)
	record, err := service.Intake(IntakeRequest{
		OwnerIdentity: "alice",
		Input:         "Create a checklist for organizing project documents.",
		SourceType:    "manual",
		SourceID:      "worker-audit-failure",
		SourceURI:     "local://note/worker-audit-failure",
	})
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}
	if record.Item.CurrentState != StateReady {
		t.Fatalf("intake state = %q; want ready so worker claim can be tested", record.Item.CurrentState)
	}
	repo.eventErr = errors.New("injected worker-start audit failure")

	result, err := service.RunOneForOwner("alice", record.Item.ID)
	if err != nil {
		t.Fatalf("RunOneForOwner: %v", err)
	}
	if result.Status != "blocked" || result.State != StateBlocked || !strings.Contains(result.Message, "task execution was not started") {
		t.Fatalf("worker result = %#v; want blocked before execution with audit failure", result)
	}
	if len(runner.requests) != 0 {
		t.Fatalf("task runner received %d requests despite failed worker-start audit", len(runner.requests))
	}
	stored := base.items[record.Item.ID]
	if stored == nil || stored.CurrentState != StateBlocked || stored.WorkerClaimID != "" || !strings.Contains(stored.LastWorkerError, "task execution was not started") {
		t.Fatalf("worker claim was not safely blocked and released: %#v", stored)
	}
}

func TestResolveInterruptedExecutionReportsAuditPersistenceFailure(t *testing.T) {
	base := newFakeWorkflowRepo()
	repo := &failingWorkflowAuditRepository{fakeWorkflowRepo: base}
	service := NewService(repo)
	record := recoverInterruptedWorkflow(t, base, service, "Create Trello checklist for low risk admin work")
	repo.decisionErr = errors.New("injected decision persistence failure")

	_, err := service.ResolveInterruptedExecution(record.Item.ID, InterruptedExecutionResolutionRequest{
		Decision: "keep_blocked",
		Note:     "The external outcome remains unknown, so keep this work blocked.",
		Actor:    "Robert",
	})
	if !errors.Is(err, repo.decisionErr) || !strings.Contains(err.Error(), "audit history is incomplete") {
		t.Fatalf("ResolveInterruptedExecution error = %v; want visible incomplete-audit failure", err)
	}
	stored := base.items[record.Item.ID]
	if stored == nil || stored.CurrentState != StateBlocked || stored.RecoveryStatus != RecoveryNeedsReview ||
		stored.RecoveryNote != "The external outcome remains unknown, so keep this work blocked." {
		t.Fatalf("interrupted workflow did not remain blocked after audit failure: %#v", stored)
	}
}
