package workflow

import (
	"errors"
	"strings"
	"testing"

	"automation-hub-backend/internal/memory"
	"automation-hub-backend/internal/models"
	"github.com/google/uuid"
)

type memoryContextPersistenceRepository struct {
	*fakeWorkflowRepo
	mode  string
	cause error
}

func (r *memoryContextPersistenceRepository) CreateChecklistItem(item *models.WorkflowChecklistItem) (*models.WorkflowChecklistItem, error) {
	if strings.HasPrefix(item.Label, "Apply learned context:") {
		if r.mode == "checklist_storage" {
			return nil, r.cause
		}
		if r.mode == "checklist_missing" {
			return nil, nil
		}
	}
	return r.fakeWorkflowRepo.CreateChecklistItem(item)
}

func (r *memoryContextPersistenceRepository) CreateSourceLink(link *models.WorkflowSourceLink) (*models.WorkflowSourceLink, error) {
	if link.SourceType == "memory" {
		if r.mode == "source_storage" {
			return nil, r.cause
		}
		if r.mode == "source_mismatch" {
			copy := *link
			copy.ID = uuid.New()
			copy.SourceID = uuid.NewString()
			return &copy, nil
		}
	}
	return r.fakeWorkflowRepo.CreateSourceLink(link)
}

func (r *memoryContextPersistenceRepository) CreateDecision(decision *models.WorkflowDecision) (*models.WorkflowDecision, error) {
	if decision.DecisionType == "memory_context" && r.mode == "decision_storage" {
		return nil, r.cause
	}
	return r.fakeWorkflowRepo.CreateDecision(decision)
}

func (r *memoryContextPersistenceRepository) CreateEvent(event *models.WorkflowEvent) (*models.WorkflowEvent, error) {
	if event.EventType == "workflow.memory_context" && r.mode == "audit_storage" {
		return nil, r.cause
	}
	return r.fakeWorkflowRepo.CreateEvent(event)
}

func TestIntakeBlocksUnconfirmedLearnedContext(t *testing.T) {
	for _, mode := range []string{"success", "checklist_storage", "checklist_missing", "source_storage", "source_mismatch", "decision_storage", "audit_storage"} {
		t.Run(mode, func(t *testing.T) {
			base := newFakeWorkflowRepo()
			cause := errors.New("learned-context storage unavailable")
			repo := &memoryContextPersistenceRepository{fakeWorkflowRepo: base, mode: mode, cause: cause}
			mem := &fakeWorkflowMemoryService{retrieveResult: &memory.RetrieveResult{
				UsedContext: []memory.RankedMemory{{Memory: models.ContextMemory{
					ID: uuid.New(), Kind: "lesson", Summary: "Include customer address and access in client checklists.", Confidence: 0.86,
				}}},
			}}
			record, err := NewServiceWithMemory(repo, mem).Intake(IntakeRequest{
				Input: "Create a Trello checklist for a client quote workflow with site visit planning.", SourceType: "manual",
			})
			if mode == "success" {
				if err != nil || record == nil || !hasEventType(record.Events, "workflow.memory_context") || !hasSourceRelationship(record.SourceLinks, "planning_context") {
					t.Fatalf("successful memory intake not confirmed: record=%#v error=%v", record, err)
				}
				return
			}
			var persistenceErr *WorkflowPersistenceError
			if record != nil || !errors.As(err, &persistenceErr) || len(base.items) != 1 {
				t.Fatalf("record=%#v error=%v items=%d; want retained, blocked intake", record, err, len(base.items))
			}
			if strings.HasSuffix(mode, "_storage") && !errors.Is(err, cause) {
				t.Fatalf("storage cause lost: %v", err)
			}
			for id, item := range base.items {
				if item.CurrentState != StateBlocked || item.NextRunAt != nil || hasEventType(base.events[id], "workflow.memory_context") {
					t.Fatalf("failed learned context remained executable or emitted a success audit: item=%#v events=%#v", item, base.events[id])
				}
			}
		})
	}
}
