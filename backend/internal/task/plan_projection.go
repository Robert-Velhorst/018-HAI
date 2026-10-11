package task

import (
	"context"
	"fmt"
	"strings"

	"automation-hub-backend/internal/plangraph"
)

// CoordinationPlanProjector is the narrow write contract used by durable task
// planning. It may create an advisory draft, but it cannot accept a revision or
// grant tool/runtime execution authority.
type CoordinationPlanProjector interface {
	Preview(context.Context, string, plangraph.PreviewRequest) (*plangraph.Plan, error)
}

// WithCoordinationPlanProjector enables automatic draft projection for durable
// Plan operations. Side-effect-free previews and Run operations do not use it.
func WithCoordinationPlanProjector(value Service, projector CoordinationPlanProjector) (Service, error) {
	service, ok := value.(*service)
	if !ok || service == nil {
		return nil, fmt.Errorf("task service does not support coordination plan projection")
	}
	if projector == nil {
		return nil, fmt.Errorf("coordination plan projector is required")
	}
	service.coordinationProjector = projector
	return service, nil
}

func (s *service) projectCoordinationDraft(plan *CompletionPlan, request IntakeRequest) error {
	if s.coordinationProjector == nil || plan == nil || plan.CoordinationPlan != nil {
		return nil
	}
	operationID := strings.TrimSpace(request.operationID)
	ownerIdentity := strings.TrimSpace(request.OwnerIdentity)
	if operationID == "" {
		return fmt.Errorf("project coordination draft: durable task operation id is required")
	}
	if ownerIdentity == "" {
		return fmt.Errorf("project coordination draft: owner identity is required")
	}
	nodes, edges := taskCoordinationGraph(plan, request)
	draft, err := s.coordinationProjector.Preview(taskExecutionContext(request), ownerIdentity, plangraph.PreviewRequest{
		IdempotencyKey: "task-plan-graph-" + operationID,
		Title:          sanitizeTaskOperationalText(plan.RealGoal, 300),
		Nodes:          nodes,
		Edges:          edges,
		CreatedBy:      ownerIdentity,
	})
	if err != nil {
		return fmt.Errorf("project coordination draft: %w", err)
	}
	if draft == nil || draft.Status != plangraph.StatusDraft || draft.CanExecute {
		return fmt.Errorf("project coordination draft: projector violated the advisory draft invariant")
	}
	plan.CoordinationDraft = draft
	plan.Events = append(plan.Events, event(
		"coordination-plan",
		"immutable advisory draft projected; explicit acceptance is still required and never grants execution authority",
	))
	return nil
}

func taskCoordinationGraph(plan *CompletionPlan, request IntakeRequest) ([]plangraph.Node, []plangraph.Edge) {
	bindings := plangraph.Bindings{
		TaskID:    strings.TrimSpace(plan.ID),
		PursuitID: strings.TrimSpace(request.PursuitID),
	}
	risk := taskPlanGraphRisk(plan.RiskAssessment.Level)
	nodes := make([]plangraph.Node, 0, len(plan.Steps)+1)
	nodes = append(nodes, plangraph.Node{
		ID:            "task",
		Type:          "task_plan",
		Title:         sanitizeTaskOperationalText(plan.RealGoal, 300),
		Owner:         strings.TrimSpace(request.OwnerIdentity),
		Status:        taskPlanRootStatus(plan.RiskAssessment),
		Risk:          risk,
		ApprovalState: taskPlanRootApproval(plan.RiskAssessment),
		Bindings:      bindings,
	})
	edges := make([]plangraph.Edge, 0, len(plan.Steps))
	previous := "task"
	for index, step := range plan.Steps {
		nodeID := fmt.Sprintf("step-%03d", index+1)
		nodes = append(nodes, plangraph.Node{
			ID:            nodeID,
			Type:          "task_step",
			Title:         sanitizeTaskOperationalText(step.Name, 300),
			Owner:         strings.TrimSpace(request.OwnerIdentity),
			Status:        taskStepPlanGraphStatus(step),
			Risk:          risk,
			ApprovalState: taskStepPlanGraphApproval(step),
			Bindings:      bindings,
		})
		edges = append(edges, plangraph.Edge{
			ID:   fmt.Sprintf("dependency-%03d", index+1),
			From: previous,
			To:   nodeID,
			Type: "finish_to_start",
		})
		previous = nodeID
	}
	return nodes, edges
}

func taskPlanGraphRisk(value string) plangraph.Risk {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "low":
		return plangraph.RiskLow
	case "high", "critical":
		return plangraph.RiskHigh
	default:
		return plangraph.RiskMedium
	}
}

func taskPlanRootApproval(risk RiskAssessment) plangraph.ApprovalState {
	if !risk.ApprovalRequired {
		return plangraph.ApprovalNotRequired
	}
	if risk.ApprovalGranted {
		return plangraph.ApprovalGranted
	}
	return plangraph.ApprovalRequired
}

func taskPlanRootStatus(risk RiskAssessment) plangraph.NodeStatus {
	if risk.ApprovalRequired && !risk.ApprovalGranted {
		return plangraph.NodeNeedsApproval
	}
	if !risk.AllowedNow {
		return plangraph.NodeBlocked
	}
	return plangraph.NodeReady
}

func taskStepPlanGraphApproval(step TaskStep) plangraph.ApprovalState {
	if step.RequiresApproval {
		return plangraph.ApprovalRequired
	}
	return plangraph.ApprovalNotRequired
}

func taskStepPlanGraphStatus(step TaskStep) plangraph.NodeStatus {
	if step.RequiresApproval {
		return plangraph.NodeNeedsApproval
	}
	if !step.Allowed {
		return plangraph.NodeBlocked
	}
	switch strings.ToLower(strings.TrimSpace(step.Status)) {
	case "ready":
		return plangraph.NodeReady
	case "blocked":
		return plangraph.NodeBlocked
	case "waiting":
		return plangraph.NodeWaiting
	case "needs_approval":
		return plangraph.NodeNeedsApproval
	case "completed":
		return plangraph.NodeCompleted
	case "failed":
		return plangraph.NodeFailed
	default:
		return plangraph.NodePlanned
	}
}
