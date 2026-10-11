package task

import (
	"fmt"
	"strings"

	"automation-hub-backend/internal/plangraph"
)

// WithAcceptedPlanResolver adds read-only accepted-plan validation to task
// planning and execution. It does not alter any execution authority provider.
func WithAcceptedPlanResolver(value Service, resolver plangraph.AcceptedRevisionResolver) (Service, error) {
	service, ok := value.(*service)
	if !ok || service == nil {
		return nil, fmt.Errorf("task service does not support accepted plan resolution")
	}
	if resolver == nil {
		return nil, fmt.Errorf("accepted plan resolver is required")
	}
	service.acceptedPlanResolver = resolver
	return service, nil
}

func (s *service) resolveCoordinationPlan(request IntakeRequest) (*plangraph.AcceptedRevisionBinding, error) {
	reference := request.CoordinationPlan
	if reference.IsZero() {
		return nil, nil
	}
	if s.acceptedPlanResolver == nil {
		return nil, fmt.Errorf("accepted coordination plan validation is unavailable")
	}
	binding, err := s.acceptedPlanResolver.ResolveAccepted(taskExecutionContext(request), strings.TrimSpace(request.OwnerIdentity), reference)
	if err != nil {
		return nil, fmt.Errorf("validate accepted coordination plan: %w", err)
	}
	if binding == nil || binding.CanExecute {
		return nil, fmt.Errorf("accepted coordination plan violated the advisory-only invariant")
	}
	if pursuitID := strings.TrimSpace(request.PursuitID); pursuitID != "" &&
		strings.TrimSpace(binding.Node.Bindings.PursuitID) != pursuitID {
		return nil, fmt.Errorf("accepted coordination node is not bound to pursuit %s", pursuitID)
	}
	if workflowID := strings.TrimSpace(request.WorkflowID); workflowID != "" &&
		strings.TrimSpace(binding.Node.Bindings.WorkflowID) != workflowID {
		return nil, fmt.Errorf("accepted coordination node is not bound to workflow %s", workflowID)
	}
	return binding, nil
}
