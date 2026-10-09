package workflow

import (
	"context"
	"fmt"
	"strings"

	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/plangraph"

	"github.com/google/uuid"
)

const coordinationProjectionFailurePrefix = "coordination draft unavailable: "

// CoordinationPlanProjector creates advisory plan revisions. It cannot accept
// a revision or grant workflow, tool, runtime, or provider authority.
type CoordinationPlanProjector interface {
	Preview(context.Context, string, plangraph.PreviewRequest) (*plangraph.Plan, error)
}

func WithCoordinationPlanProjector(value Service, projector CoordinationPlanProjector) (Service, error) {
	service, ok := value.(*service)
	if !ok || service == nil {
		return nil, fmt.Errorf("workflow service does not support coordination plan projection")
	}
	if projector == nil {
		return nil, fmt.Errorf("coordination plan projector is required")
	}
	service.coordinationProjector = projector
	return service, nil
}

// ensureWorkflowCoordinationDraft is the last execution gate as well as a
// recovery path for older records. A plan is bound with a revision-guarded
// update, and its audit receipt must be durable before a worker may claim it.
func (s *service) ensureWorkflowCoordinationDraft(item *models.WorkflowItem, actor string) error {
	if s.coordinationProjector == nil || item == nil || item.ID == uuid.Nil || item.CoordinationPlanID != nil {
		return nil
	}
	if item.CoordinationDraftPlanID != nil {
		return s.ensureWorkflowCoordinationDraftAudit(item, actor)
	}
	checklist, err := s.repo.FindChecklist(item.ID)
	if err == nil {
		var draft *plangraph.Plan
		draft, err = s.projectWorkflowCoordinationDraft(item, checklist)
		if err == nil {
			updated := *item
			applyWorkflowCoordinationDraftBinding(&updated, draft)
			if strings.HasPrefix(strings.TrimSpace(updated.BlockedReason), coordinationProjectionFailurePrefix) {
				updated.BlockedReason = ""
				if updated.RequiresApproval && updated.ApprovalStatus != "approved" {
					updated.CurrentState = StateNeedsApproval
				} else {
					updated.CurrentState = StateReady
				}
			}
			var transition *models.WorkflowTransition
			if updated.CurrentState != item.CurrentState {
				transition = &models.WorkflowTransition{
					WorkflowID: item.ID, FromState: item.CurrentState, ToState: updated.CurrentState,
					Trigger: "coordination_projection_recovery", Actor: firstNonEmpty(actor, "engine"),
					Reason: "coordination draft is durable; projection-only execution block was cleared",
				}
			}
			message := workflowCoordinationDraftProjectedMessage(*updated.CoordinationDraftPlanID)
			persisted, changed, updateErr := s.repo.CommitWorkflowCoordinationProjection(WorkflowCoordinationProjectionFinalization{
				Expected: item, Updated: &updated, Transition: transition,
				Decision: models.WorkflowDecision{
					WorkflowID: item.ID, DecisionType: "coordination_plan", Decision: "drafted",
					Reason:      "immutable advisory coordination draft linked before workflow execution",
					RuleApplied: "plan graph projection", Actor: firstNonEmpty(actor, "engine"),
				},
				Event: models.WorkflowEvent{
					WorkflowID: item.ID, EventType: "workflow.coordination_draft_projected",
					FromState: item.CurrentState, ToState: updated.CurrentState,
					Message: message, Trigger: "coordination_projection", RuleApplied: "plan graph projection",
					Actor: firstNonEmpty(actor, "engine"),
				},
			})
			if updateErr != nil {
				err = fmt.Errorf("persist coordination draft and audit receipt: %w", updateErr)
			} else if !changed || persisted == nil {
				err = ErrWorkflowIntakeConcurrentChange
			} else {
				*item = *persisted
				return nil
			}
		}
	}
	return s.recordCoordinationProjectionFailure(item, err, actor)
}

func (s *service) ensureWorkflowCoordinationDraftAudit(item *models.WorkflowItem, actor string) error {
	if item == nil || item.CoordinationDraftPlanID == nil {
		return workflowPersistenceFailure("workflow coordination draft", fmt.Errorf("draft binding is missing"))
	}
	events, err := s.repo.FindEvents(item.ID)
	if err != nil {
		return workflowPersistenceFailure("workflow coordination draft audit", err)
	}
	decisions, err := s.repo.FindDecisions(item.ID)
	if err != nil {
		return workflowPersistenceFailure("workflow coordination draft decision", err)
	}
	message := workflowCoordinationDraftProjectedMessage(*item.CoordinationDraftPlanID)
	eventRecorded := false
	for _, event := range events {
		if event.EventType == "workflow.coordination_draft_projected" && event.Message == message {
			eventRecorded = true
			break
		}
	}
	decisionRecorded := false
	for _, decision := range decisions {
		if decision.DecisionType == "coordination_plan" && decision.Decision == "drafted" &&
			decision.Reason == "immutable advisory coordination draft linked before workflow execution" &&
			decision.RuleApplied == "plan graph projection" {
			decisionRecorded = true
			break
		}
	}
	if eventRecorded && decisionRecorded {
		return nil
	}
	updated := *item
	resolvedActor := firstNonEmpty(actor, "engine")
	_, changed, err := s.repo.CommitWorkflowCoordinationProjection(WorkflowCoordinationProjectionFinalization{
		Expected: item, Updated: &updated,
		Decision: models.WorkflowDecision{
			WorkflowID: item.ID, DecisionType: "coordination_plan", Decision: "drafted",
			Reason:      "immutable advisory coordination draft linked before workflow execution",
			RuleApplied: "plan graph projection", Actor: resolvedActor,
		},
		Event: models.WorkflowEvent{
			WorkflowID: item.ID, EventType: "workflow.coordination_draft_projected",
			FromState: item.CurrentState, ToState: item.CurrentState, Message: message,
			Trigger: "workflow_intake", RuleApplied: "plan graph projection", Actor: resolvedActor,
		},
	})
	if err != nil {
		return workflowPersistenceFailure("repair workflow coordination draft audit", err)
	}
	if !changed {
		return ErrWorkflowIntakeConcurrentChange
	}
	return nil
}

func (s *service) projectWorkflowCoordinationDraft(item *models.WorkflowItem, checklist []models.WorkflowChecklistItem) (*plangraph.Plan, error) {
	owner := strings.TrimSpace(item.OwnerIdentity)
	if owner == "" {
		return nil, fmt.Errorf("workflow owner identity is required")
	}
	nodes, edges := workflowCoordinationGraph(*item, checklist)
	draft, err := s.coordinationProjector.Preview(context.Background(), owner, plangraph.PreviewRequest{
		IdempotencyKey: "workflow-plan-graph-" + item.ID.String(),
		Title:          compact(item.Title, 300),
		Nodes:          nodes,
		Edges:          edges,
		CreatedBy:      owner,
	})
	if err != nil {
		return nil, fmt.Errorf("project workflow coordination draft: %w", err)
	}
	if draft == nil || draft.Status != plangraph.StatusDraft || draft.Revision != 1 || draft.CanExecute {
		return nil, fmt.Errorf("project workflow coordination draft: projector violated the immutable advisory invariant")
	}
	if workflowCoordinationRoot(draft, item.ID.String()) == nil {
		return nil, fmt.Errorf("project workflow coordination draft: projector lost the workflow binding")
	}
	return draft, nil
}

func (s *service) recordCoordinationProjectionFailure(item *models.WorkflowItem, projectionErr error, actor string) error {
	if item == nil {
		return workflowPersistenceFailure("workflow coordination draft", fmt.Errorf("workflow is required"))
	}
	reason := "coordination draft projection failed"
	if projectionErr != nil {
		reason = compact(projectionErr.Error(), 420)
	}
	updated := *item
	if updated.CurrentState == StateReady || strings.HasPrefix(strings.TrimSpace(updated.BlockedReason), coordinationProjectionFailurePrefix) {
		updated.CurrentState = StateBlocked
		updated.BlockedReason = coordinationProjectionFailurePrefix + reason
		updated.NextAction = "restore workflow plan projection before execution"
	}
	resolvedActor := firstNonEmpty(actor, "engine")
	var transition *models.WorkflowTransition
	if updated.CurrentState != item.CurrentState {
		transition = &models.WorkflowTransition{
			WorkflowID: item.ID, FromState: item.CurrentState, ToState: updated.CurrentState,
			Trigger: "coordination_projection_failure", Actor: resolvedActor,
			Reason: "workflow execution is blocked until an advisory coordination draft is durable",
		}
	}
	persisted, changed, err := s.repo.CommitWorkflowCoordinationFailure(WorkflowCoordinationProjectionFinalization{
		Expected: item, Updated: &updated, Transition: transition,
		Decision: models.WorkflowDecision{
			WorkflowID: item.ID, DecisionType: "coordination_plan", Decision: "unavailable",
			Reason:      "workflow execution remains gated until an advisory coordination draft is durable",
			RuleApplied: reason, Actor: resolvedActor,
		},
		Event: models.WorkflowEvent{
			WorkflowID: item.ID, EventType: "workflow.coordination_draft_failed",
			FromState: item.CurrentState, ToState: updated.CurrentState,
			Message: "workflow retained; execution requires a durable advisory coordination draft",
			Trigger: "workflow_intake", RuleApplied: "plan graph projection", Actor: resolvedActor,
		},
	})
	if err != nil {
		return workflowPersistenceFailure("finalize workflow coordination projection failure", err)
	}
	if !changed || persisted == nil {
		return ErrWorkflowIntakeConcurrentChange
	}
	*item = *persisted
	return workflowPersistenceFailure("workflow coordination draft projection", projectionErr)
}

func workflowCoordinationDraftProjectedMessage(planID uuid.UUID) string {
	return fmt.Sprintf("immutable advisory workflow draft %s projected; explicit acceptance and separate effect authority remain required", planID)
}

func applyWorkflowCoordinationDraftBinding(item *models.WorkflowItem, draft *plangraph.Plan) {
	if item == nil || draft == nil {
		return
	}
	root := workflowCoordinationRoot(draft, item.ID.String())
	if root == nil {
		return
	}
	planID := draft.ID
	item.CoordinationDraftPlanID = &planID
	item.CoordinationDraftRevision = draft.Revision
	item.CoordinationDraftDigest = draft.Digest
	item.CoordinationDraftNodeID = root.ID
}

func workflowCoordinationRoot(draft *plangraph.Plan, workflowID string) *plangraph.Node {
	if draft == nil {
		return nil
	}
	for index := range draft.Nodes {
		node := &draft.Nodes[index]
		if node.ID == "workflow" && node.Type == "workflow" && node.Bindings.WorkflowID == workflowID {
			return node
		}
	}
	return nil
}

func workflowCoordinationGraph(item models.WorkflowItem, checklist []models.WorkflowChecklistItem) ([]plangraph.Node, []plangraph.Edge) {
	bindings := plangraph.Bindings{WorkflowID: item.ID.String()}
	risk := workflowPlanRisk(item.RiskLevel)
	nodes := make([]plangraph.Node, 0, len(checklist)+1)
	nodes = append(nodes, plangraph.Node{
		ID:            "workflow",
		Type:          "workflow",
		Title:         compact(item.Title, 300),
		Owner:         strings.TrimSpace(item.OwnerIdentity),
		Status:        workflowPlanStatus(item.CurrentState),
		Deadline:      item.DueAt,
		Risk:          risk,
		ApprovalState: workflowPlanApproval(item.RequiresApproval, item.ApprovalStatus),
		Bindings:      bindings,
	})
	edges := make([]plangraph.Edge, 0, len(checklist))
	previous := "workflow"
	for index, step := range checklist {
		nodeID := fmt.Sprintf("checklist-%03d", index+1)
		nodes = append(nodes, plangraph.Node{
			ID:            nodeID,
			Type:          "workflow_step",
			Title:         compact(step.Label, 300),
			Owner:         strings.TrimSpace(item.OwnerIdentity),
			Status:        workflowChecklistPlanStatus(step),
			Deadline:      step.DueAt,
			Risk:          risk,
			ApprovalState: workflowPlanApproval(step.RequiresApproval, ""),
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

func workflowPlanRisk(value string) plangraph.Risk {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "low":
		return plangraph.RiskLow
	case "high", "critical":
		return plangraph.RiskHigh
	default:
		return plangraph.RiskMedium
	}
}

func workflowPlanApproval(required bool, status string) plangraph.ApprovalState {
	if !required {
		return plangraph.ApprovalNotRequired
	}
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "approved":
		return plangraph.ApprovalGranted
	case "rejected":
		return plangraph.ApprovalRejected
	default:
		return plangraph.ApprovalRequired
	}
}

func workflowPlanStatus(value string) plangraph.NodeStatus {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case StateReady:
		return plangraph.NodeReady
	case StateBlocked:
		return plangraph.NodeBlocked
	case StateWaitingInput:
		return plangraph.NodeWaiting
	case StateNeedsApproval:
		return plangraph.NodeNeedsApproval
	case StateCompleted, StateArchived:
		return plangraph.NodeCompleted
	case "failed":
		return plangraph.NodeFailed
	default:
		return plangraph.NodePlanned
	}
}

func workflowChecklistPlanStatus(step models.WorkflowChecklistItem) plangraph.NodeStatus {
	if step.RequiresApproval {
		return plangraph.NodeNeedsApproval
	}
	switch strings.ToLower(strings.TrimSpace(step.Status)) {
	case "completed", "done":
		return plangraph.NodeCompleted
	case "blocked":
		return plangraph.NodeBlocked
	case "waiting":
		return plangraph.NodeWaiting
	case "failed":
		return plangraph.NodeFailed
	case "ready":
		return plangraph.NodeReady
	default:
		return plangraph.NodePlanned
	}
}
