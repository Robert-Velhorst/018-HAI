package executionauth

import (
	"context"
	"fmt"

	"automation-hub-backend/internal/frameworkregistry"
)

// ConstitutionPolicyService is the narrow framework-registry contract needed
// by the execution boundary. The Constitution classifies restrictions but
// never grants execution authority.
type ConstitutionPolicyService interface {
	EvaluateConstitutionExecutionPolicyContext(
		context.Context,
		frameworkregistry.ConstitutionExecutionPolicyRequest,
	) (*frameworkregistry.ConstitutionExecutionPolicyDecision, error)
}

type ConstitutionPolicyAdapter struct {
	service ConstitutionPolicyService
}

func NewConstitutionPolicyAdapter(
	service ConstitutionPolicyService,
) (*ConstitutionPolicyAdapter, error) {
	if service == nil {
		return nil, fmt.Errorf("Constitution policy service is required")
	}
	return &ConstitutionPolicyAdapter{service: service}, nil
}

func (a *ConstitutionPolicyAdapter) EvaluateExecutionPolicy(
	ctx context.Context,
	owner string,
	capabilities []string,
	requiredAuthority int,
) (ConstitutionDecision, error) {
	if ctx == nil {
		return ConstitutionDecision{}, fmt.Errorf("Constitution evaluation context is required")
	}
	if err := ctx.Err(); err != nil {
		return ConstitutionDecision{}, err
	}
	decision, err := a.service.EvaluateConstitutionExecutionPolicyContext(
		ctx,
		frameworkregistry.ConstitutionExecutionPolicyRequest{
			OwnerIdentity:         owner,
			RequestedCapabilities: append([]string(nil), capabilities...),
			RequiredAuthority:     requiredAuthority,
		},
	)
	if err != nil {
		return ConstitutionDecision{}, err
	}
	if err := ctx.Err(); err != nil {
		return ConstitutionDecision{}, err
	}
	if decision == nil {
		return ConstitutionDecision{}, fmt.Errorf(
			"Constitution policy service returned no decision",
		)
	}
	if decision.GrantsAuthority {
		return ConstitutionDecision{}, fmt.Errorf(
			"Constitution policy must not grant execution authority",
		)
	}
	return ConstitutionDecision{
		ID:                           decision.ConstitutionID,
		Version:                      decision.ConstitutionVersion,
		Source:                       decision.ConstitutionSource,
		Digest:                       decision.ConstitutionDigest,
		AuthorityCeiling:             decision.EffectiveAuthorityCeiling,
		DeniedCapabilities:           append([]string(nil), decision.DeniedCapabilities...),
		ApprovalRequiredCapabilities: append([]string(nil), decision.ApprovalRequiredCapabilities...),
	}, nil
}
