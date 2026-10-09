package workflow

import (
	"errors"
	"strings"
	"testing"

	"automation-hub-backend/internal/models"
	"github.com/google/uuid"
)

func (r *fakeWorkflowRepo) CommitChecklistUpdate(expected, updated *models.WorkflowChecklistItem, event models.WorkflowEvent) (*models.WorkflowChecklistItem, bool, error) {
	if expected == nil || updated == nil || expected.ID != updated.ID || expected.WorkflowID != updated.WorkflowID || event.WorkflowID != expected.WorkflowID {
		return nil, false, errors.New("invalid checklist references")
	}
	for _, current := range r.checklist[expected.WorkflowID] {
		if current.ID != expected.ID || current.Status != expected.Status || !current.UpdatedAt.Equal(expected.UpdatedAt) {
			continue
		}
		before := append([]models.WorkflowChecklistItem(nil), r.checklist[expected.WorkflowID]...)
		eventsBefore := append([]models.WorkflowEvent(nil), r.events[expected.WorkflowID]...)
		result, err := r.UpdateChecklistItem(updated)
		if err == nil {
			_, err = r.CreateEvent(&event)
		}
		if err != nil {
			r.checklist[expected.WorkflowID] = before
			r.events[expected.WorkflowID] = eventsBefore
			return nil, false, err
		}
		return result, true, nil
	}
	return nil, false, nil
}

type checklistCommitFailureRepository struct {
	*fakeWorkflowRepo
	cause error
	calls int
}

func (r *checklistCommitFailureRepository) CommitChecklistUpdate(expected, updated *models.WorkflowChecklistItem, event models.WorkflowEvent) (*models.WorkflowChecklistItem, bool, error) {
	r.calls++
	return nil, false, workflowAuditPersistenceFailure("checklist event", r.cause)
}

func TestChecklistServiceUsesAtomicCommitFailure(t *testing.T) {
	base := newFakeWorkflowRepo()
	workflowID := uuid.New()
	_, err := base.CreateItem(&models.WorkflowItem{ID: workflowID, Title: "Checklist audit", CurrentState: StateReady})
	if err != nil {
		t.Fatal(err)
	}
	item, err := base.CreateChecklistItem(&models.WorkflowChecklistItem{WorkflowID: workflowID, Label: "Review evidence", Status: "open"})
	if err != nil {
		t.Fatal(err)
	}
	cause := errors.New("audit storage unavailable")
	repository := &checklistCommitFailureRepository{fakeWorkflowRepo: base, cause: cause}
	_, err = NewService(repository).UpdateChecklistItem(workflowID, item.ID, ChecklistUpdateRequest{Status: "done"})
	if !errors.Is(err, cause) || repository.calls != 1 {
		t.Fatalf("error=%v, commits=%d; want typed atomic commit failure", err, repository.calls)
	}
	if base.checklist[workflowID][0].Status != "open" || len(base.events[workflowID]) != 0 {
		t.Fatal("failed atomic commit changed checklist or events")
	}
}

type completionChecklistRepository struct {
	*fakeWorkflowRepo
	readError error
	stale     bool
	commits   int
}

func (r *completionChecklistRepository) FindChecklist(id uuid.UUID) ([]models.WorkflowChecklistItem, error) {
	if r.readError != nil {
		return nil, r.readError
	}
	return r.fakeWorkflowRepo.FindChecklist(id)
}

func (r *completionChecklistRepository) CommitChecklistUpdate(expected, updated *models.WorkflowChecklistItem, event models.WorkflowEvent) (*models.WorkflowChecklistItem, bool, error) {
	r.commits++
	if r.stale {
		return nil, false, nil
	}
	return r.fakeWorkflowRepo.CommitChecklistUpdate(expected, updated, event)
}

func TestAutomaticChecklistProgressUsesAtomicCommit(t *testing.T) {
	for _, mode := range []string{"success", "read_failure", "commit_failure", "stale", "already_done", "absent"} {
		t.Run(mode, func(t *testing.T) {
			base := newFakeWorkflowRepo()
			workflowID := uuid.New()
			label := "Verify completion before closing"
			status := "open"
			if mode == "already_done" {
				status = "done"
			}
			if mode != "absent" {
				if _, err := base.CreateChecklistItem(&models.WorkflowChecklistItem{WorkflowID: workflowID, Label: label, Status: status}); err != nil {
					t.Fatal(err)
				}
			}
			cause := errors.New("completion storage unavailable")
			repository := &completionChecklistRepository{fakeWorkflowRepo: base, stale: mode == "stale"}
			if mode == "read_failure" {
				repository.readError = cause
			}
			var repo Repository = repository
			if mode == "commit_failure" {
				repo = &checklistCommitFailureRepository{fakeWorkflowRepo: base, cause: cause}
			}
			svc := NewService(repo).(*service)
			err := svc.markChecklistProgress(workflowID, label)
			failed := mode == "read_failure" || mode == "commit_failure" || mode == "stale"
			if (err != nil) != failed {
				t.Fatalf("error=%v, want failure=%v", err, failed)
			}
			if (mode == "read_failure" || mode == "commit_failure") && !errors.Is(err, cause) {
				t.Fatalf("storage failure cause lost: %v", err)
			}
			if mode == "success" {
				if base.checklist[workflowID][0].Status != "done" || len(base.events[workflowID]) != 1 || base.events[workflowID][0].Trigger != "worker_completion" {
					t.Fatal("completion checklist and audit were not committed together")
				}
				if err := svc.markChecklistProgress(workflowID, label); err != nil || len(base.events[workflowID]) != 1 || repository.commits != 1 {
					t.Fatal("repeated completion emitted a duplicate checklist audit")
				}
			} else if len(base.events[workflowID]) != 0 || (mode != "absent" && base.checklist[workflowID][0].Status != status) {
				t.Fatal("failed or unnecessary completion changed checklist or audit")
			}
		})
	}
}

func TestPostgresChecklistAuditFailureRollsBackStatus(t *testing.T) {
	db := workflowTransactionalPostgres(t)
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	t.Cleanup(func() { _ = tx.Rollback().Error })
	workflowID := uuid.New()
	constraintName := "chk_checklist_event_failure_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	constraint := "CHECK (workflow_id <> '" + workflowID.String() + "'::uuid OR event_type <> 'workflow.checklist')"
	if err := tx.Exec("ALTER TABLE workflow_events ADD CONSTRAINT " + constraintName + " " + constraint).Error; err != nil {
		t.Fatal(err)
	}
	repository := NewGormRepository(tx)
	_, err := repository.CreateItem(&models.WorkflowItem{
		ID: workflowID, OwnerIdentity: "checklist-atomicity-" + uuid.NewString(), Title: "Checklist audit atomicity",
		CurrentState: StateReady, TaskType: "administrative", RiskLevel: "low", AutonomyLevel: "manual",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = repository.CreateChecklistItem(&models.WorkflowChecklistItem{WorkflowID: workflowID, Label: "Review evidence", Status: "open"})
	if err != nil {
		t.Fatal(err)
	}
	persisted, err := repository.FindChecklist(workflowID)
	if err != nil || len(persisted) != 1 {
		t.Fatalf("read checklist revision=%#v, error=%v", persisted, err)
	}
	expected := persisted[0]
	updated := expected
	updated.Status = "done"
	event := models.WorkflowEvent{WorkflowID: workflowID, EventType: "workflow.checklist", Message: "checklist item marked done", Trigger: "checklist_update", Actor: "operator"}
	_, committed, err := repository.CommitChecklistUpdate(&expected, &updated, event)
	var auditErr *WorkflowAuditPersistenceError
	if committed || !errors.As(err, &auditErr) || auditErr.Operation != "checklist event" {
		t.Fatalf("committed=%v, error=%v; want audit failure", committed, err)
	}
	checklist, err := repository.FindChecklist(workflowID)
	if err != nil || len(checklist) != 1 || checklist[0].Status != "open" || !checklist[0].UpdatedAt.Equal(expected.UpdatedAt) {
		t.Fatalf("checklist after rollback=%#v, error=%v", checklist, err)
	}
	if err := tx.Exec("ALTER TABLE workflow_events DROP CONSTRAINT " + constraintName).Error; err != nil {
		t.Fatal(err)
	}
	_, committed, err = repository.CommitChecklistUpdate(&expected, &updated, event)
	if err != nil || !committed {
		t.Fatalf("commit after audit recovery=%v, error=%v", committed, err)
	}
	_, committed, err = repository.CommitChecklistUpdate(&expected, &updated, event)
	if err != nil || committed {
		t.Fatalf("stale revision committed=%v, error=%v", committed, err)
	}
	events, err := repository.FindEvents(workflowID)
	if err != nil || len(events) != 1 || events[0].EventType != "workflow.checklist" {
		t.Fatalf("events=%#v, error=%v; want one committed event", events, err)
	}
}
