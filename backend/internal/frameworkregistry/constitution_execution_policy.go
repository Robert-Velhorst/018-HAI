package frameworkregistry

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// Public Constitution execution capability names. These values are the only
// capability vocabulary accepted by EvaluateConstitutionExecutionPolicy.
const (
	ConstitutionCapabilityMemoryRead            = capabilityMemoryRead
	ConstitutionCapabilityDocumentRead          = capabilityDocumentRead
	ConstitutionCapabilityWebAccess             = capabilityWebAccess
	ConstitutionCapabilityToolExecution         = capabilityToolExecution
	ConstitutionCapabilityLocalExecution        = capabilityLocalExecution
	ConstitutionCapabilityExecution             = capabilityExecution
	ConstitutionCapabilityExternalCommunication = capabilityExternalCommunication
	ConstitutionCapabilityLegalGovernmentAction = capabilityLegalGovernmentAction
	ConstitutionCapabilityFinancialAction       = capabilityFinancialAction
	ConstitutionCapabilityAccountChange         = capabilityAccountChange
	ConstitutionCapabilityDestructiveAction     = capabilityDestructiveAction
	ConstitutionCapabilityPublicPosting         = capabilityPublicPosting
	ConstitutionCapabilityConsequentialAction   = capabilityConsequentialAction
)

// ConstitutionExecutionPolicyRequest asks the active owner-scoped
// Constitution to classify explicit capabilities and an authority
// requirement. It is a policy evaluation input, not an approval or authority
// token.
type ConstitutionExecutionPolicyRequest struct {
	OwnerIdentity         string   `json:"-"`
	RequestedCapabilities []string `json:"requestedCapabilities"`
	RequiredAuthority     int      `json:"requiredAuthority"`
}

// ConstitutionExecutionPolicyDecision reports only the restrictions imposed
// by the active Constitution. AllowedCapabilities means "not denied by this
// Constitution"; it does not prove approval, satisfy another policy layer, or
// grant execution authority.
type ConstitutionExecutionPolicyDecision struct {
	RequestedCapabilities          []string `json:"requestedCapabilities"`
	AllowedCapabilities            []string `json:"allowedCapabilities"`
	DeniedCapabilities             []string `json:"deniedCapabilities"`
	ApprovalRequiredCapabilities   []string `json:"approvalRequiredCapabilities"`
	RequiredAuthority              int      `json:"requiredAuthority"`
	EffectiveAuthorityCeiling      int      `json:"effectiveAuthorityCeiling"`
	RequiredAuthorityWithinCeiling bool     `json:"requiredAuthorityWithinCeiling"`
	ConstitutionSatisfied          bool     `json:"constitutionSatisfied"`
	GrantsAuthority                bool     `json:"grantsAuthority"`
	ConstitutionID                 string   `json:"constitutionId"`
	ConstitutionVersion            int      `json:"constitutionVersion"`
	ConstitutionSource             string   `json:"constitutionSource"`
	ConstitutionDigest             string   `json:"constitutionDigest"`
}

// EvaluateConstitutionExecutionPolicy deterministically applies the protected
// and typed rules of the owner's active Constitution to explicit capability
// names. The decision can only retain or reduce the caller's available
// authority; it never grants authority and is not an execution authorization.
func (s *Service) EvaluateConstitutionExecutionPolicy(
	request ConstitutionExecutionPolicyRequest,
) (*ConstitutionExecutionPolicyDecision, error) {
	return s.EvaluateConstitutionExecutionPolicyContext(context.Background(), request)
}

func (s *Service) EvaluateConstitutionExecutionPolicyContext(
	ctx context.Context,
	request ConstitutionExecutionPolicyRequest,
) (*ConstitutionExecutionPolicyDecision, error) {
	owner := strings.TrimSpace(request.OwnerIdentity)
	if owner == "" {
		return nil, fmt.Errorf("owner identity is required")
	}
	if request.RequiredAuthority < 0 || request.RequiredAuthority > 10 {
		return nil, fmt.Errorf("required authority must be between 0 and 10")
	}

	capabilities, err := normalizeRequestedConstitutionCapabilities(
		request.RequestedCapabilities,
	)
	if err != nil {
		return nil, err
	}

	constitution, source, err := s.ActiveConstitutionContext(ctx, owner)
	if err != nil {
		return nil, err
	}
	effective, err := compileEffectiveConstitutionRules(constitution)
	if err != nil {
		return nil, fmt.Errorf("compile active Constitution rules: %w", err)
	}
	digest, err := constitutionReproducibilityDigest(constitution)
	if err != nil {
		return nil, err
	}

	allowed := make([]string, 0, len(capabilities))
	denied := make([]string, 0, len(capabilities))
	approvalRequired := make([]string, 0, len(capabilities))
	for _, capability := range capabilities {
		if _, prohibited := effective.DeniedCapabilities[capability]; prohibited {
			denied = append(denied, capability)
		} else {
			allowed = append(allowed, capability)
		}
		if _, requiresApproval := effective.ApprovalRequired[capability]; requiresApproval {
			approvalRequired = append(approvalRequired, capability)
		}
	}

	withinCeiling := request.RequiredAuthority <= effective.AuthorityCeiling
	return &ConstitutionExecutionPolicyDecision{
		RequestedCapabilities:          capabilities,
		AllowedCapabilities:            allowed,
		DeniedCapabilities:             denied,
		ApprovalRequiredCapabilities:   approvalRequired,
		RequiredAuthority:              request.RequiredAuthority,
		EffectiveAuthorityCeiling:      effective.AuthorityCeiling,
		RequiredAuthorityWithinCeiling: withinCeiling,
		ConstitutionSatisfied:          len(denied) == 0 && withinCeiling,
		GrantsAuthority:                false,
		ConstitutionID:                 strings.TrimSpace(constitution.ID),
		ConstitutionVersion:            constitution.Version,
		ConstitutionSource:             source,
		ConstitutionDigest:             digest,
	}, nil
}

func normalizeRequestedConstitutionCapabilities(values []string) ([]string, error) {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		capability := strings.ToLower(strings.TrimSpace(value))
		seen[capability] = struct{}{}
	}
	result := make([]string, 0, len(seen))
	for capability := range seen {
		result = append(result, capability)
	}
	sort.Strings(result)
	for _, capability := range result {
		if _, supported := supportedConstitutionCapabilities[capability]; !supported {
			return nil, fmt.Errorf("unknown Constitution capability %q", capability)
		}
	}
	return result, nil
}
