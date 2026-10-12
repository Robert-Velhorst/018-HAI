package workflow

import (
	"context"
	"errors"
	"strings"
	"testing"

	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/plangraph"

	"github.com/google/uuid"
)

type recordingWorkflowCoordinationProjector struct {
	service *plangraph.Service
	calls   int
	owner   string
	request plangraph.PreviewRequest
	err     error
}

func (projector *recordingWorkflowCoordinationProjector) Preview(ctx context.Context, owner string, request plangraph.PreviewRequest) (*plangraph.Plan, error) {
	projector.calls++
	projector.owner = owner
	projector.request = request
	if projector.err != nil {
		return nil, projector.err
	}
	return projector.service.Preview(ctx, owner, request)
}

func TestWorkflowIntakeProjectsImmutableAdvisoryCoordinationDraft(t *testing.T) {
	projector := &recordingWorkflowCoordinationProjector{
		service: plangraph.NewService(plangraph.NewMemoryRepository(), nil),
	}
	configured, err := WithCoordinationPlanProjector(NewService(newFakeWorkflowRepo()), projector)
	if err != nil {
		t.Fatalf("configure projector: %v", err)
	}
	record, err := configured.Intake(IntakeRequest{
		OwnerIdentity: "owner-a",
		Input:         "Create a low-risk administrative checklist and verify completion.",
		ProjectKey:    "project-a",
		SourceType:    "manual",
		SourceID:      "workflow-projection-1",
	})
	if err != nil {
		t.Fatalf("workflow intake: %v", err)
	}
	if projector.calls != 1 || projector.owner != "owner-a" {
		t.Fatalf("unexpected projection calls=%d owner=%q", projector.calls, projector.owner)
	}
	item := record.Item
	if item.CoordinationPlanID != nil || item.CoordinationDraftPlanID == nil {
		t.Fatalf("expected only a draft coordination binding: %+v", item)
	}
	if item.CoordinationDraftRevision != 1 || item.CoordinationDraftDigest == "" || item.CoordinationDraftNodeID != "workflow" {
		t.Fatalf("draft binding is incomplete: %+v", item)
	}
	plans, err := projector.service.List(context.Background(), "owner-a")
	if err != nil || len(plans) != 1 {
		t.Fatalf("list projected plans: count=%d err=%v", len(plans), err)
	}
	draft := plans[0]
	if draft.Status != plangraph.StatusDraft || draft.Revision != 1 || draft.CanExecute {
		t.Fatalf("draft violated advisory contract: %+v", draft)
	}
	if len(draft.Nodes) != len(record.Checklist)+1 || len(draft.Edges) != len(record.Checklist) {
		t.Fatalf("workflow graph did not preserve checklist: nodes=%d edges=%d checklist=%d", len(draft.Nodes), len(draft.Edges), len(record.Checklist))
	}
	for _, node := range draft.Nodes {
		if node.Owner != "owner-a" || node.Bindings.WorkflowID != item.ID.String() {
			t.Fatalf("node is not exactly owner/workflow bound: %+v", node)
		}
		if node.EstimatedMinutes != 0 || node.EstimatedCostEUR != 0 {
			t.Fatalf("projection invented unsupported estimates: %+v", node)
		}
	}
	previous := "workflow"
	for index, edge := range draft.Edges {
		if edge.From != previous || edge.Type != "finish_to_start" {
			t.Fatalf("dependency %d is not a deterministic chain: %+v", index, edge)
		}
		previous = edge.To
	}
	if projector.request.IdempotencyKey != "workflow-plan-graph-"+item.ID.String() {
		t.Fatalf("draft idempotency is not workflow-bound: %+v", projector.request)
	}
	if strings.Contains(strings.ToLower(projector.request.Title), "execute") && draft.CanExecute {
		t.Fatal("workflow text broadened advisory authority")
	}
}

func TestAcceptedWorkflowCoordinationPlanSuppressesCompetingDraft(t *testing.T) {
	reference := workflowPlanReference(strings.Repeat("b", 64))
	resolver := &workflowAcceptedPlanResolverStub{binding: &plangraph.AcceptedRevisionBinding{
		PlanID: reference.PlanID, Revision: reference.Revision, Digest: reference.Digest,
		NodeID: reference.NodeID, Node: plangraph.Node{ID: reference.NodeID}, CanExecute: false,
	}}
	projector := &recordingWorkflowCoordinationProjector{
		service: plangraph.NewService(plangraph.NewMemoryRepository(), nil),
	}
	configured, err := WithAcceptedPlanResolver(NewService(newFakeWorkflowRepo()), resolver)
	if err != nil {
		t.Fatal(err)
	}
	configured, err = WithCoordinationPlanProjector(configured, projector)
	if err != nil {
		t.Fatal(err)
	}
	record, err := configured.Intake(IntakeRequest{
		OwnerIdentity:    "owner-a",
		Input:            "Prepare an administrative checklist.",
		CoordinationPlan: reference,
	})
	if err != nil {
		t.Fatalf("workflow intake: %v", err)
	}
	if projector.calls != 0 || record.Item.CoordinationDraftPlanID != nil || record.Item.CoordinationPlanID == nil {
		t.Fatalf("accepted provenance should suppress a competing draft: calls=%d item=%+v", projector.calls, record.Item)
	}
}

func TestWorkflowCoordinationProjectionFailureBlocksAndReplayRecovers(t *testing.T) {
	projector := &recordingWorkflowCoordinationProjector{
		service: plangraph.NewService(plangraph.NewMemoryRepository(), nil),
		err:     errors.New("plan storage unavailable"),
	}
	configured, err := WithCoordinationPlanProjector(NewService(newFakeWorkflowRepo()), projector)
	if err != nil {
		t.Fatal(err)
	}
	request := IntakeRequest{
		OwnerIdentity: "owner-a",
		Input:         "Create a low-risk administrative checklist.",
		SourceType:    "manual",
		SourceID:      "workflow-projection-recovery",
	}
	first, err := configured.Intake(request)
	if err != nil {
		t.Fatalf("retain intake during projection outage: %v", err)
	}
	if first.Item.CurrentState != StateBlocked || !strings.HasPrefix(first.Item.BlockedReason, coordinationProjectionFailurePrefix) {
		t.Fatalf("projection outage did not block execution: %+v", first.Item)
	}
	if first.Item.CoordinationDraftPlanID != nil {
		t.Fatal("failed projection recorded a draft binding")
	}

	projector.err = nil
	second, err := configured.Intake(request)
	if err != nil {
		t.Fatalf("recover idempotent workflow projection: %v", err)
	}
	if second.Item.ID != first.Item.ID || second.Item.CoordinationDraftPlanID == nil {
		t.Fatalf("replay did not recover the same workflow: first=%+v second=%+v", first.Item, second.Item)
	}
	if second.Item.CurrentState != StateReady || second.Item.BlockedReason != "" {
		t.Fatalf("successful recovery did not restore safe runnable state: %+v", second.Item)
	}
	if projector.calls != 2 {
		t.Fatalf("unexpected projection attempts: %d", projector.calls)
	}
}

func TestWorkerCannotClaimReadyWorkflowUntilCoordinationDraftAndReceiptAreDurable(t *testing.T) {
	repo := newFakeWorkflowRepo()
	item, err := repo.CreateItem(&models.WorkflowItem{
		ID: uuid.New(), OwnerIdentity: "owner-a", Title: "Legacy ready workflow",
		CurrentState: StateReady, ApprovalStatus: "not_required", RiskLevel: "low",
	})
	if err != nil {
		t.Fatalf("create legacy workflow: %v", err)
	}
	projector := &recordingWorkflowCoordinationProjector{
		service: plangraph.NewService(plangraph.NewMemoryRepository(), nil),
		err:     errors.New("plan storage unavailable"),
	}
	configured, err := WithCoordinationPlanProjector(NewService(repo), projector)
	if err != nil {
		t.Fatalf("configure projector: %v", err)
	}

	result, err := configured.RunOneForOwner("owner-a", item.ID)
	if err != nil {
		t.Fatalf("run one: %v", err)
	}
	if result == nil || result.Status != "blocked" {
		t.Fatalf("run result = %+v, want blocked", result)
	}
	stored, err := repo.FindItem(item.ID)
	if err != nil {
		t.Fatalf("reload workflow: %v", err)
	}
	if stored.CurrentState != StateBlocked || stored.WorkerClaimID != "" || stored.CoordinationDraftPlanID != nil {
		t.Fatalf("workflow was not durably held before claim: %+v", stored)
	}
	transitions, err := repo.FindTransitions(item.ID)
	if err != nil || len(transitions) != 1 || transitions[0].FromState != StateReady || transitions[0].ToState != StateBlocked {
		t.Fatalf("projection failure transition = %+v err=%v, want ready->blocked audit", transitions, err)
	}
	decisions, err := repo.FindDecisions(item.ID)
	if err != nil {
		t.Fatalf("load projection failure decisions: %v", err)
	}
	decisionRecorded := false
	for _, decision := range decisions {
		if decision.DecisionType == "coordination_plan" && decision.Decision == "unavailable" {
			decisionRecorded = true
			break
		}
	}
	if !decisionRecorded {
		t.Fatalf("projection failure decision was not recorded: %+v", decisions)
	}
}

func TestCoordinationProjectionRejectsStateChangeWithoutTransition(t *testing.T) {
	workflowID := uuid.New()
	expected := &models.WorkflowItem{ID: workflowID, CurrentState: StateBlocked}
	updated := *expected
	updated.CurrentState = StateReady
	planID := uuid.New()
	updated.CoordinationDraftPlanID = &planID
	repository := NewGormRepository(nil)

	_, _, err := repository.CommitWorkflowCoordinationProjection(WorkflowCoordinationProjectionFinalization{
		Expected: expected,
		Updated:  &updated,
		Decision: models.WorkflowDecision{WorkflowID: workflowID, DecisionType: "coordination_plan"},
		Event: models.WorkflowEvent{
			WorkflowID: workflowID, EventType: "workflow.coordination_draft_projected",
			FromState: StateBlocked, ToState: StateReady,
		},
	})
	if err == nil || !strings.Contains(err.Error(), "state changes require exactly one matching transition") {
		t.Fatalf("commit error = %v, want transition integrity rejection before database access", err)
	}
}

func TestCoordinationFailureRejectsStateChangeWithoutTransition(t *testing.T) {
	workflowID := uuid.New()
	expected := &models.WorkflowItem{ID: workflowID, CurrentState: StateReady}
	updated := *expected
	updated.CurrentState = StateBlocked
	repository := NewGormRepository(nil)

	_, _, err := repository.CommitWorkflowCoordinationFailure(WorkflowCoordinationProjectionFinalization{
		Expected: expected,
		Updated:  &updated,
		Decision: models.WorkflowDecision{WorkflowID: workflowID, DecisionType: "coordination_plan", Decision: "unavailable"},
		Event: models.WorkflowEvent{
			WorkflowID: workflowID, EventType: "workflow.coordination_draft_failed",
			FromState: StateReady, ToState: StateBlocked,
		},
	})
	if err == nil || !strings.Contains(err.Error(), "state changes require exactly one matching transition") {
		t.Fatalf("commit error = %v, want transition integrity rejection before database access", err)
	}
}

func TestWorkflowCoordinationDraftAuditRepairsMissingDecisionWithoutDuplicatingReceipt(t *testing.T) {
	repo := newFakeWorkflowRepo()
	planID := uuid.New()
	item, err := repo.CreateItem(&models.WorkflowItem{
		ID: uuid.New(), OwnerIdentity: "owner-a", Title: "Previously projected workflow",
		CurrentState: StateReady, ApprovalStatus: "not_required", RiskLevel: "low",
		CoordinationDraftPlanID: &planID, CoordinationDraftRevision: 1,
		CoordinationDraftDigest: strings.Repeat("a", 64), CoordinationDraftNodeID: "workflow",
	})
	if err != nil {
		t.Fatalf("create workflow: %v", err)
	}
	message := workflowCoordinationDraftProjectedMessage(planID)
	if _, err := repo.CreateEvent(&models.WorkflowEvent{
		WorkflowID: item.ID, EventType: "workflow.coordination_draft_projected",
		FromState: StateReady, ToState: StateReady, Message: message,
	}); err != nil {
		t.Fatalf("create preexisting receipt: %v", err)
	}
	configured, err := WithCoordinationPlanProjector(NewService(repo), &recordingWorkflowCoordinationProjector{
		service: plangraph.NewService(plangraph.NewMemoryRepository(), nil),
	})
	if err != nil {
		t.Fatalf("configure projector: %v", err)
	}
	service := configured.(*service)
	if err := service.ensureWorkflowCoordinationDraft(item, "engine"); err != nil {
		t.Fatalf("repair workflow coordination audit: %v", err)
	}
	events, err := repo.FindEvents(item.ID)
	if err != nil || len(events) != 1 {
		t.Fatalf("events=%+v err=%v, want one existing projection receipt", events, err)
	}
	decisions, err := repo.FindDecisions(item.ID)
	if err != nil || len(decisions) != 1 || decisions[0].DecisionType != "coordination_plan" || decisions[0].Decision != "drafted" {
		t.Fatalf("decisions=%+v err=%v, want repaired durable draft decision", decisions, err)
	}
}
