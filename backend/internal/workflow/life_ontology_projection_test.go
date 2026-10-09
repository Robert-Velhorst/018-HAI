package workflow

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/lifeontology"
	"automation-hub-backend/internal/models"

	"github.com/google/uuid"
)

func TestWorkflowTransitionsProjectImmutableOwnerScopedLifeGraph(t *testing.T) {
	repo := newFakeWorkflowRepo()
	graph := lifeontology.NewService(lifeontology.NewMemoryRepository(), time.Now)
	base := NewService(repo)
	service, err := WithLifeOntologyProjection(base, graph)
	if err != nil {
		t.Fatalf("attach life ontology projection: %v", err)
	}

	record, err := service.Intake(IntakeRequest{
		OwnerIdentity: "owner-1",
		Input:         "Create a low-risk Trello checklist for the HAI connector acceptance review.",
		ProjectKey:    "018-HAI",
		SourceType:    "trello",
		SourceID:      "card-life-graph-acceptance",
		SourceURI:     "local://trello/card-life-graph-acceptance",
		SourceLabel:   "HAI connector acceptance",
		Actor:         "test-operator",
	})
	if err != nil {
		t.Fatalf("intake workflow: %v", err)
	}

	entities, err := graph.QueryEntities(context.Background(), "owner-1", lifeontology.EntityQuery{
		AllowLocalOnly: true,
		Limit:          100,
	})
	if err != nil {
		t.Fatalf("query projected entities: %v", err)
	}
	if len(entities) == 0 {
		t.Fatalf("workflow intake produced no life-graph entities; projection audit events: %#v", record.Events)
	}
	assertWorkflowProjectionEntities(t, entities, record.Item.ID.String())

	relations, err := graph.QueryRelations(context.Background(), "owner-1", lifeontology.RelationQuery{
		AllowLocalOnly: true,
		Limit:          100,
	})
	if err != nil {
		t.Fatalf("query projected relations: %v", err)
	}
	if !hasWorkflowProjectionRelation(relations, lifeontology.RelationBelongsToProject) ||
		!hasWorkflowProjectionRelation(relations, lifeontology.RelationDerivedFrom) {
		t.Fatalf("workflow projection relations are incomplete: %#v", relations)
	}

	if _, err := service.Transition(record.Item.ID, TransitionRequest{
		TargetState: StateBlocked,
		Message:     "Acceptance evidence is still being reviewed.",
		Actor:       "test-operator",
	}); err != nil {
		t.Fatalf("persist blocked transition: %v", err)
	}
	workflowEntities, err := graph.QueryEntities(context.Background(), "owner-1", lifeontology.EntityQuery{
		Types:          []lifeontology.EntityType{lifeontology.EntityWorkflow},
		AllowLocalOnly: true,
		Limit:          100,
	})
	if err != nil {
		t.Fatalf("query workflow observations: %v", err)
	}
	if len(workflowEntities) < 2 {
		t.Fatalf("expected immutable observations for intake and transition, got %d", len(workflowEntities))
	}
	if !containsWorkflowStatus(workflowEntities, StateBlocked) {
		t.Fatalf("blocked workflow observation was not projected: %#v", workflowEntities)
	}

	updated, err := service.Get(record.Item.ID)
	if err != nil {
		t.Fatalf("read workflow with projection audit: %v", err)
	}
	if !containsWorkflowEvent(updated, "workflow.life_graph_projected") {
		t.Fatalf("successful graph projection was not audited: %#v", updated.Events)
	}
	for _, entity := range workflowEntities {
		if entity.OwnerIdentity != "owner-1" || !entity.LocalOnly {
			t.Fatalf("workflow projection lost owner or local-only boundary: %#v", entity)
		}
	}
	otherOwner, err := graph.QueryEntities(context.Background(), "owner-2", lifeontology.EntityQuery{AllowLocalOnly: true})
	if err != nil || len(otherOwner) != 0 {
		t.Fatalf("owner-scoped graph leaked records: count=%d err=%v", len(otherOwner), err)
	}
}

func TestWorkflowProjectionObservationClampsFutureCASRevision(t *testing.T) {
	now := time.Date(2026, time.September, 26, 17, 20, 0, 0, time.UTC)
	item := &models.WorkflowItem{
		ID:        uuid.New(),
		CreatedAt: now.Add(-time.Minute),
		UpdatedAt: now.Add(time.Microsecond),
	}

	observedAt, err := workflowProjectionObservedAt(item, now)
	if err != nil {
		t.Fatal(err)
	}
	if !observedAt.Equal(now) {
		t.Fatalf("future CAS revision should be clamped to projection time: got %s want %s", observedAt, now)
	}
}

func TestWorkflowGraphFailureDoesNotRollbackDurableTransition(t *testing.T) {
	repo := newFakeWorkflowRepo()
	base := NewService(repo)
	service, err := WithLifeOntologyProjection(base, failingWorkflowProjector{})
	if err != nil {
		t.Fatalf("attach failing life ontology projection: %v", err)
	}

	record, err := service.Intake(IntakeRequest{
		OwnerIdentity: "owner-1",
		Input:         "Create a low-risk checklist for a durable workflow failure-isolation test.",
		ProjectKey:    "018-HAI",
		SourceType:    "manual",
		SourceID:      "projection-failure-isolation",
		Actor:         "test-operator",
	})
	if err != nil {
		t.Fatalf("graph failure rolled back workflow intake: %v", err)
	}
	if len(record.Transitions) == 0 {
		t.Fatal("durable workflow transition was not persisted")
	}
	if !containsWorkflowEvent(record, "workflow.life_graph_projection_failed") {
		t.Fatalf("graph failure was not exposed in workflow audit: %#v", record.Events)
	}
	for _, event := range record.Events {
		if event.EventType == "workflow.life_graph_projection_failed" && !strings.Contains(event.RuleApplied, "cannot roll back") {
			t.Fatalf("failure audit does not explain isolation boundary: %#v", event)
		}
	}
}

type failingWorkflowProjector struct{}

func (failingWorkflowProjector) ProjectOperationalRecord(context.Context, lifeontology.OperationalProjectionRequest) (lifeontology.OperationalProjectionResult, error) {
	return lifeontology.OperationalProjectionResult{}, errors.New("graph store unavailable")
}

func assertWorkflowProjectionEntities(t *testing.T, entities []lifeontology.Entity, workflowID string) {
	t.Helper()
	wanted := map[lifeontology.EntityType]bool{
		lifeontology.EntityWorkflow: false,
		lifeontology.EntityDocument: false,
		lifeontology.EntityProject:  false,
	}
	for _, entity := range entities {
		if _, ok := wanted[entity.Type]; ok {
			wanted[entity.Type] = true
		}
		if entity.Type == lifeontology.EntityWorkflow {
			matched := false
			for _, key := range entity.ExternalKeys {
				matched = matched || (key.Namespace == "hai/workflow" && key.Value == workflowID)
			}
			if !matched {
				t.Fatalf("workflow entity is missing stable external identity: %#v", entity)
			}
		}
	}
	for kind, found := range wanted {
		if !found {
			t.Fatalf("missing %s projection in %#v", kind, entities)
		}
	}
}

func hasWorkflowProjectionRelation(relations []lifeontology.Relation, kind lifeontology.RelationType) bool {
	for _, relation := range relations {
		if relation.Type == kind {
			return true
		}
	}
	return false
}

func containsWorkflowStatus(entities []lifeontology.Entity, state string) bool {
	for _, entity := range entities {
		if entity.Attributes["workflow_state"] == state {
			return true
		}
	}
	return false
}

func containsWorkflowEvent(record *WorkflowRecord, eventType string) bool {
	for _, event := range record.Events {
		if event.EventType == eventType {
			return true
		}
	}
	return false
}
