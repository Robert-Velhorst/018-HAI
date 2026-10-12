package pursuit

import (
	"context"
	"fmt"
	"strings"

	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/plangraph"

	"github.com/google/uuid"
)

// WithAcceptedPlanResolver adds owner-scoped planning provenance checks. An
// accepted plan remains advisory and does not alter pursuit execution gates.
func WithAcceptedPlanResolver(value Service, resolver plangraph.AcceptedRevisionResolver) (Service, error) {
	service, ok := value.(*service)
	if !ok || service == nil {
		return nil, fmt.Errorf("pursuit service does not support accepted plan resolution")
	}
	if resolver == nil {
		return nil, fmt.Errorf("accepted plan resolver is required")
	}
	service.acceptedPlanResolver = resolver
	return service, nil
}

func (s *service) resolvePursuitCoordinationPlan(
	ownerIdentity string,
	pursuitID uuid.UUID,
	reference plangraph.AcceptedRevisionReference,
) (*plangraph.AcceptedRevisionBinding, error) {
	binding, err := s.resolveAcceptedCoordinationPlan(ownerIdentity, reference)
	if err != nil || binding == nil {
		return binding, err
	}
	if pursuitID == uuid.Nil || strings.TrimSpace(binding.Node.Bindings.PursuitID) != pursuitID.String() {
		return nil, fmt.Errorf("accepted coordination node is not bound to pursuit %s", pursuitID)
	}
	return binding, nil
}

func (s *service) resolvePortfolioCoordinationPlan(
	ownerIdentity string,
	inputs []PortfolioPursuitPlanningInput,
	reference plangraph.AcceptedRevisionReference,
) (*plangraph.AcceptedRevisionBinding, error) {
	binding, err := s.resolveAcceptedCoordinationPlan(ownerIdentity, reference)
	if err != nil || binding == nil {
		return binding, err
	}
	boundPursuits := make(map[string]struct{}, len(binding.Nodes))
	for _, node := range binding.Nodes {
		if pursuitID := strings.TrimSpace(node.Bindings.PursuitID); pursuitID != "" {
			boundPursuits[pursuitID] = struct{}{}
		}
	}
	for _, input := range inputs {
		if _, ok := boundPursuits[input.PursuitID.String()]; !ok {
			return nil, fmt.Errorf("accepted coordination plan does not contain pursuit %s", input.PursuitID)
		}
	}
	return binding, nil
}

func (s *service) resolveAcceptedCoordinationPlan(
	ownerIdentity string,
	reference plangraph.AcceptedRevisionReference,
) (*plangraph.AcceptedRevisionBinding, error) {
	if reference.IsZero() {
		return nil, nil
	}
	if s.acceptedPlanResolver == nil {
		return nil, fmt.Errorf("accepted coordination plan validation is unavailable")
	}
	binding, err := s.acceptedPlanResolver.ResolveAccepted(
		context.Background(), strings.TrimSpace(ownerIdentity), reference,
	)
	if err != nil {
		return nil, fmt.Errorf("validate accepted coordination plan: %w", err)
	}
	if binding == nil || binding.CanExecute {
		return nil, fmt.Errorf("accepted coordination plan violated the advisory-only invariant")
	}
	return binding, nil
}

// PostgreSQL CHAR(64) pads an absent digest. Only that absent representation
// is canonicalized; present or malformed nonempty digests remain exact.
func portfolioCoordinationDigest(value string) string {
	if strings.Trim(value, " ") == "" {
		return ""
	}
	return value
}

func coordinationReferenceForAllocation(allocation *models.PursuitPortfolioAllocation) plangraph.AcceptedRevisionReference {
	if allocation == nil {
		return plangraph.AcceptedRevisionReference{}
	}
	reference := plangraph.AcceptedRevisionReference{
		Revision: allocation.CoordinationPlanRevision,
		Digest:   portfolioCoordinationDigest(allocation.CoordinationPlanDigest),
		NodeID:   allocation.CoordinationPlanNodeID,
	}
	if allocation.CoordinationPlanID != nil {
		reference.PlanID = *allocation.CoordinationPlanID
	}
	return reference
}

func validatePortfolioCoordinationBindingShape(allocation *models.PursuitPortfolioAllocation) error {
	if allocation == nil {
		return fmt.Errorf("portfolio allocation is required")
	}
	allAbsent := allocation.CoordinationPlanID == nil &&
		allocation.CoordinationPlanRevision == 0 &&
		strings.TrimSpace(allocation.CoordinationPlanDigest) == "" &&
		strings.TrimSpace(allocation.CoordinationPlanNodeID) == ""
	if allAbsent {
		return nil
	}
	if allocation.CoordinationPlanID == nil || *allocation.CoordinationPlanID == uuid.Nil ||
		allocation.CoordinationPlanRevision == 0 ||
		!portfolioDigestPattern.MatchString(strings.TrimSpace(allocation.CoordinationPlanDigest)) ||
		strings.TrimSpace(allocation.CoordinationPlanNodeID) == "" {
		return fmt.Errorf("portfolio allocation has an incomplete coordination plan binding")
	}
	return nil
}

func (s *service) revalidatePortfolioAllocationCoordinationPlan(
	ownerIdentity string,
	allocation *models.PursuitPortfolioAllocation,
	items []models.PursuitPortfolioAllocationItem,
) (*plangraph.AcceptedRevisionBinding, error) {
	if err := validatePortfolioCoordinationBindingShape(allocation); err != nil {
		return nil, err
	}
	binding, err := s.resolveAcceptedCoordinationPlan(ownerIdentity, coordinationReferenceForAllocation(allocation))
	if err != nil || binding == nil {
		return binding, err
	}
	boundPursuits := make(map[string]struct{}, len(binding.Nodes))
	for _, node := range binding.Nodes {
		if pursuitID := strings.TrimSpace(node.Bindings.PursuitID); pursuitID != "" {
			boundPursuits[pursuitID] = struct{}{}
		}
	}
	for _, item := range items {
		if _, ok := boundPursuits[item.PursuitID.String()]; !ok {
			return nil, fmt.Errorf("accepted coordination plan no longer contains pursuit %s", item.PursuitID)
		}
	}
	return binding, nil
}

func applyPortfolioCoordinationBinding(
	allocation *models.PursuitPortfolioAllocation,
	binding *plangraph.AcceptedRevisionBinding,
) {
	if allocation == nil || binding == nil {
		return
	}
	planID := binding.PlanID
	allocation.CoordinationPlanID = &planID
	allocation.CoordinationPlanRevision = binding.Revision
	allocation.CoordinationPlanDigest = binding.Digest
	allocation.CoordinationPlanNodeID = binding.NodeID
}

func coordinationReferenceFromBinding(binding *plangraph.AcceptedRevisionBinding) plangraph.AcceptedRevisionReference {
	if binding == nil {
		return plangraph.AcceptedRevisionReference{}
	}
	return plangraph.AcceptedRevisionReference{
		PlanID: binding.PlanID, Revision: binding.Revision,
		Digest: binding.Digest, NodeID: binding.NodeID,
	}
}
