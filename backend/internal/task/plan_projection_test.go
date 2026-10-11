package task

import (
	"context"
	"errors"
	"strings"
	"testing"

	"automation-hub-backend/internal/plangraph"
)

type recordingCoordinationProjector struct {
	service *plangraph.Service
	calls   int
	owner   string
	request plangraph.PreviewRequest
	err     error
}

type cancellationAwareCoordinationProjector struct{}

func (cancellationAwareCoordinationProjector) Preview(ctx context.Context, _ string, _ plangraph.PreviewRequest) (*plangraph.Plan, error) {
	return nil, ctx.Err()
}

func (projector *recordingCoordinationProjector) Preview(ctx context.Context, owner string, request plangraph.PreviewRequest) (*plangraph.Plan, error) {
	projector.calls++
	projector.owner = owner
	projector.request = request
	if projector.err != nil {
		return nil, projector.err
	}
	if projector.service == nil {
		return &plangraph.Plan{Status: plangraph.StatusDraft}, nil
	}
	return projector.service.Preview(ctx, owner, request)
}

func TestDurableTaskPlanProjectsImmutableAdvisoryCoordinationDraft(t *testing.T) {
	projector := &recordingCoordinationProjector{
		service: plangraph.NewService(plangraph.NewMemoryRepository(), nil),
	}
	configured, err := WithCoordinationPlanProjector(
		NewService(&fakeMemoryService{}, newTaskTestLLMService(t)),
		projector,
	)
	if err != nil {
		t.Fatalf("configure projector: %v", err)
	}
	plan, err := configured.Plan(IntakeRequest{
		OwnerIdentity:  "owner-a",
		IdempotencyKey: "task-projection-1",
		Request:        "Summarize the current project notes and identify the next safe action.",
		ProjectKey:     "project-a",
	})
	if err != nil {
		t.Fatalf("plan task: %v", err)
	}
	if projector.calls != 1 || projector.owner != "owner-a" {
		t.Fatalf("unexpected projection calls=%d owner=%q", projector.calls, projector.owner)
	}
	if plan.CoordinationPlan != nil || plan.CoordinationDraft == nil {
		t.Fatalf("expected only a draft coordination graph: accepted=%+v draft=%+v", plan.CoordinationPlan, plan.CoordinationDraft)
	}
	draft := plan.CoordinationDraft
	if draft.Status != plangraph.StatusDraft || draft.Revision != 1 || draft.CanExecute {
		t.Fatalf("draft violated immutable advisory contract: %+v", draft)
	}
	if got, want := len(draft.Nodes), len(plan.Steps)+1; got != want {
		t.Fatalf("unexpected node count: got %d want %d", got, want)
	}
	if got, want := len(draft.Edges), len(plan.Steps); got != want {
		t.Fatalf("unexpected edge count: got %d want %d", got, want)
	}
	nodes := make(map[string]plangraph.Node, len(draft.Nodes))
	for _, node := range draft.Nodes {
		nodes[node.ID] = node
		if node.Owner != "owner-a" || node.Bindings.TaskID != plan.ID || node.Bindings.PursuitID != "" {
			t.Fatalf("node is not exactly owner/task bound: %+v", node)
		}
		if node.EstimatedMinutes != 0 || node.EstimatedCostEUR != 0 {
			t.Fatalf("projection invented unsupported estimates: %+v", node)
		}
	}
	if nodes["task"].Type != "task_plan" {
		t.Fatalf("missing task root: %+v", nodes["task"])
	}
	previous := "task"
	for index := range plan.Steps {
		nodeID := strings.TrimSpace(draft.Edges[index].To)
		if draft.Edges[index].From != previous || draft.Edges[index].Type != "finish_to_start" {
			t.Fatalf("dependency %d is not a deterministic chain: %+v", index, draft.Edges[index])
		}
		previous = nodeID
	}
	if projector.request.IdempotencyKey != "task-plan-graph-"+plan.OperationID {
		t.Fatalf("draft is not bound to durable operation: %+v", projector.request)
	}
}

func TestAcceptedCoordinationPlanSuppressesCompetingDraftProjection(t *testing.T) {
	projector := &recordingCoordinationProjector{}
	base := NewService(&fakeMemoryService{}, newTaskTestLLMService(t))
	configured, err := WithAcceptedPlanResolver(base, &acceptedPlanResolverStub{binding: &plangraph.AcceptedRevisionBinding{
		PlanID: taskPlanReference().PlanID, Revision: 2, Digest: strings.Repeat("a", 64), NodeID: "task-node",
		Node: plangraph.Node{ID: "task-node"}, CanExecute: false,
	}})
	if err != nil {
		t.Fatalf("configure accepted resolver: %v", err)
	}
	configured, err = WithCoordinationPlanProjector(configured, projector)
	if err != nil {
		t.Fatalf("configure projector: %v", err)
	}
	plan, err := configured.Plan(IntakeRequest{
		OwnerIdentity:    "owner-a",
		IdempotencyKey:   "task-projection-accepted",
		Request:          "Summarize the project notes.",
		CoordinationPlan: taskPlanReference(),
	})
	if err != nil {
		t.Fatalf("plan accepted task: %v", err)
	}
	if projector.calls != 0 || plan.CoordinationDraft != nil || plan.CoordinationPlan == nil {
		t.Fatalf("accepted provenance should suppress a competing draft: calls=%d plan=%+v", projector.calls, plan)
	}
}

func TestTaskPreviewNeverProjectsDurableCoordinationState(t *testing.T) {
	projector := &recordingCoordinationProjector{}
	configured, err := WithCoordinationPlanProjector(
		NewService(&fakeMemoryService{}, newTaskTestLLMService(t)),
		projector,
	)
	if err != nil {
		t.Fatalf("configure projector: %v", err)
	}
	previewer := configured.(PreviewService)
	plan, err := previewer.Preview(IntakeRequest{OwnerIdentity: "owner-a", Request: "Summarize project notes."})
	if err != nil {
		t.Fatalf("preview task: %v", err)
	}
	if projector.calls != 0 || plan.CoordinationDraft != nil {
		t.Fatalf("side-effect-free preview projected durable state: calls=%d draft=%+v", projector.calls, plan.CoordinationDraft)
	}
}

func TestCoordinationProjectionFailureFailsDurablePlanClosed(t *testing.T) {
	projector := &recordingCoordinationProjector{err: errors.New("storage unavailable")}
	configured, err := WithCoordinationPlanProjector(
		NewService(&fakeMemoryService{}, newTaskTestLLMService(t)),
		projector,
	)
	if err != nil {
		t.Fatalf("configure projector: %v", err)
	}
	_, err = configured.Plan(IntakeRequest{
		OwnerIdentity:  "owner-a",
		IdempotencyKey: "task-projection-failure",
		Request:        "Summarize project notes.",
	})
	if err == nil || !strings.Contains(err.Error(), "project coordination draft") {
		t.Fatalf("projection failure should fail closed, got %v", err)
	}
	if projector.calls != 1 || len(configured.Logs()) != 0 {
		t.Fatalf("failed projection must not become a successful task log: calls=%d logs=%d", projector.calls, len(configured.Logs()))
	}
}

func TestCoordinationProjectionPropagatesTaskCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	service := NewService(nil, nil).(*service)
	service.coordinationProjector = cancellationAwareCoordinationProjector{}

	err := service.projectCoordinationDraft(&CompletionPlan{RealGoal: "Prepare evidence"}, IntakeRequest{
		OwnerIdentity:    "owner-a",
		operationID:      "operation-a",
		executionContext: ctx,
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("project coordination draft error = %v, want context canceled", err)
	}
}
