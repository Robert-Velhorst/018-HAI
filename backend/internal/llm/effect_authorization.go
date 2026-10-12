package llm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"automation-hub-backend/internal/safety"
)

const (
	EffectOperationGenerate  = "llm.generate"
	EffectOperationModelPull = "llm.model.pull"
)

// EffectContext is trusted server-side provenance. It is deliberately excluded
// from API JSON so a client cannot assert an owner, approval, or authorization
// fact. Composition roots must derive it from authenticated task state.
type EffectContext struct {
	OwnerIdentity         string
	ActorIdentity         string
	ActorKind             string
	TaskID                string
	ProjectKey            string
	MandateID             string
	ApprovalSourceID      string
	ApprovalBindingDigest string
}

// FinalEffectAuthorizationRequest is the exact request presented immediately
// before a provider POST or model installation/update. Prompt and payload
// contents are represented only by SHA-256 digests.
type FinalEffectAuthorizationRequest struct {
	Operation             string
	OwnerIdentity         string
	ActorIdentity         string
	TaskID                string
	ProjectKey            string
	ProviderID            string
	ModelID               string
	EndpointKey           string
	ProviderLocal         bool
	ActorKind             string
	MandateID             string
	ApprovalSourceID      string
	ApprovalBindingDigest string
	EstimatedCostEUR      float64
	Paid                  bool
	RequiresApproval      bool
	PromptDigest          string
	PayloadDigest         string
	ConfigurationHash     string
	EffectDigest          string
}

// FinalEffectAuthorizer must validate and atomically consume durable authority
// for this exact request. Returning nil means the effect may be exercised once;
// it does not authorize any different provider, model, cost, task, or payload.
type FinalEffectAuthorizer interface {
	AuthorizeFinalEffect(context.Context, FinalEffectAuthorizationRequest) error
}

type FinalEffectAuthorizerFunc func(context.Context, FinalEffectAuthorizationRequest) error

func (f FinalEffectAuthorizerFunc) AuthorizeFinalEffect(
	ctx context.Context,
	request FinalEffectAuthorizationRequest,
) error {
	return f(ctx, request)
}

type EmergencyStopState struct {
	Active bool
	Reason string
}

// EmergencyStopEvaluator is injected so unreadable stop state fails closed and
// tests can prove the final recheck occurs immediately before the effect.
type EmergencyStopEvaluator interface {
	EvaluateEmergencyStop(context.Context) (EmergencyStopState, error)
}

type EmergencyStopEvaluatorFunc func(context.Context) (EmergencyStopState, error)

func (f EmergencyStopEvaluatorFunc) EvaluateEmergencyStop(
	ctx context.Context,
) (EmergencyStopState, error) {
	return f(ctx)
}

type processEmergencyStopEvaluator struct{}

func (processEmergencyStopEvaluator) EvaluateEmergencyStop(
	context.Context,
) (EmergencyStopState, error) {
	decision := safety.EvaluateEmergencyStop()
	return EmergencyStopState{
		Active: decision.Active,
		Reason: decision.Reason,
	}, nil
}

// WithFinalEffectAuthorization installs the mandatory effect boundary. A nil
// authorizer is not a permissive default: real provider POSTs and model pulls
// fail closed until a production adapter is injected.
func (s *Service) WithFinalEffectAuthorization(
	authorizer FinalEffectAuthorizer,
	emergencyStop EmergencyStopEvaluator,
) *Service {
	if s == nil {
		return nil
	}
	s.finalEffectAuthorizer = authorizer
	if emergencyStop != nil {
		s.emergencyStop = emergencyStop
	}
	return s
}

// WithMaintenanceEffectContext supplies server-derived provenance for
// scheduled model installation/update. Read-only checks do not consume it.
func (s *Service) WithMaintenanceEffectContext(effectContext EffectContext) *Service {
	if s == nil {
		return nil
	}
	normalized := normalizeEffectContext(effectContext)
	s.maintenanceEffectContext = &normalized
	return s
}

func normalizeEffectContext(input EffectContext) EffectContext {
	actorKind := strings.ToLower(strings.TrimSpace(input.ActorKind))
	if actorKind == "" {
		actorKind = "system"
	}
	return EffectContext{
		OwnerIdentity:         strings.TrimSpace(input.OwnerIdentity),
		ActorIdentity:         strings.TrimSpace(input.ActorIdentity),
		ActorKind:             actorKind,
		TaskID:                strings.TrimSpace(input.TaskID),
		ProjectKey:            strings.TrimSpace(input.ProjectKey),
		MandateID:             strings.TrimSpace(input.MandateID),
		ApprovalSourceID:      strings.TrimSpace(input.ApprovalSourceID),
		ApprovalBindingDigest: strings.ToLower(strings.TrimSpace(input.ApprovalBindingDigest)),
	}
}

func validateEffectContext(input *EffectContext) (EffectContext, error) {
	if input == nil {
		return EffectContext{}, fmt.Errorf("server-side effect authorization context is required")
	}
	context := normalizeEffectContext(*input)
	switch {
	case context.OwnerIdentity == "":
		return EffectContext{}, fmt.Errorf("effect owner identity is required")
	case context.ActorIdentity == "":
		return EffectContext{}, fmt.Errorf("effect actor identity is required")
	case context.ActorKind != "system" && context.ActorKind != "human":
		return EffectContext{}, fmt.Errorf("effect actor kind must be system or human")
	case context.TaskID == "":
		return EffectContext{}, fmt.Errorf("effect task id is required")
	case context.ProjectKey == "":
		return EffectContext{}, fmt.Errorf("effect project key is required")
	default:
		return context, nil
	}
}

func buildFinalEffectAuthorizationRequest(
	operation string,
	effectContext *EffectContext,
	provider Provider,
	model Model,
	endpoint string,
	estimatedCostEUR float64,
	prompt []byte,
	payload []byte,
	configurationHash string,
) (FinalEffectAuthorizationRequest, error) {
	trustedContext, err := validateEffectContext(effectContext)
	if err != nil {
		return FinalEffectAuthorizationRequest{}, err
	}
	endpointKey, err := maintenanceEndpointKey(endpoint)
	if err != nil {
		return FinalEffectAuthorizationRequest{}, fmt.Errorf("effect endpoint is invalid")
	}
	request := FinalEffectAuthorizationRequest{
		Operation:             strings.TrimSpace(operation),
		OwnerIdentity:         trustedContext.OwnerIdentity,
		ActorIdentity:         trustedContext.ActorIdentity,
		TaskID:                trustedContext.TaskID,
		ProjectKey:            trustedContext.ProjectKey,
		ProviderID:            strings.TrimSpace(provider.ID),
		ModelID:               strings.TrimSpace(model.ID),
		EndpointKey:           endpointKey,
		ProviderLocal:         provider.Local,
		ActorKind:             trustedContext.ActorKind,
		MandateID:             trustedContext.MandateID,
		ApprovalSourceID:      trustedContext.ApprovalSourceID,
		ApprovalBindingDigest: trustedContext.ApprovalBindingDigest,
		EstimatedCostEUR:      estimatedCostEUR,
		Paid:                  provider.Paid || estimatedCostEUR > 0,
		RequiresApproval:      provider.Paid || model.RequiresApproval || estimatedCostEUR > 0 || model.Tier == TierExpensive,
		PromptDigest:          sha256Hex(prompt),
		PayloadDigest:         sha256Hex(payload),
		ConfigurationHash:     strings.TrimSpace(configurationHash),
	}
	if request.Operation == "" || request.ProviderID == "" || request.ModelID == "" {
		return FinalEffectAuthorizationRequest{}, fmt.Errorf("effect operation, provider, and model are required")
	}
	if request.Operation == EffectOperationModelPull {
		if request.ProviderID != "ollama" && request.ProviderID != miniSWEOllamaProviderID {
			return FinalEffectAuthorizationRequest{}, fmt.Errorf("model installation and updates are not implemented for provider %q", request.ProviderID)
		}
		if !provider.Local {
			return FinalEffectAuthorizationRequest{}, fmt.Errorf("model installation and updates require a local Ollama runtime")
		}
		if isOllamaCloudModelID(request.ModelID) {
			return FinalEffectAuthorizationRequest{}, fmt.Errorf("cloud-hosted Ollama model tags cannot be installed as local model binaries")
		}
	}
	if request.EstimatedCostEUR < 0 {
		return FinalEffectAuthorizationRequest{}, fmt.Errorf("effect estimated cost cannot be negative")
	}
	if request.RequiresApproval && request.ApprovalSourceID == "" {
		return FinalEffectAuthorizationRequest{}, fmt.Errorf("server-side approval source is required for paid or approval-gated model effects")
	}
	if request.ApprovalSourceID != "" && request.ApprovalBindingDigest == "" {
		return FinalEffectAuthorizationRequest{}, fmt.Errorf("server-side approval binding digest is required when an approval source is present")
	}
	digestPayload := request
	digestPayload.EffectDigest = ""
	encoded, err := json.Marshal(digestPayload)
	if err != nil {
		return FinalEffectAuthorizationRequest{}, fmt.Errorf("encode final effect binding: %w", err)
	}
	request.EffectDigest = sha256Hex(encoded)
	return request, nil
}

func sha256Hex(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}

func (s *Service) authorizeFinalEffect(
	ctx context.Context,
	request FinalEffectAuthorizationRequest,
) error {
	if s.finalEffectAuthorizer == nil {
		return fmt.Errorf("LLM final-effect authorizer is not configured")
	}
	state, err := s.evaluateEmergencyStop(ctx)
	if err != nil {
		return fmt.Errorf("emergency-stop state is unavailable")
	}
	if state.Active {
		return fmt.Errorf("emergency stop is active: %s", safety.RedactSecrets(strings.TrimSpace(state.Reason)))
	}
	if err := s.finalEffectAuthorizer.AuthorizeFinalEffect(ctx, request); err != nil {
		return fmt.Errorf("final-effect authorization was rejected: %w", err)
	}
	// Recheck after authorization consumption and immediately before the
	// caller invokes the network/install effect.
	state, err = s.evaluateEmergencyStop(ctx)
	if err != nil {
		return fmt.Errorf("emergency-stop state is unavailable after authorization")
	}
	if state.Active {
		return fmt.Errorf("emergency stop became active before final effect: %s", safety.RedactSecrets(strings.TrimSpace(state.Reason)))
	}
	return nil
}

func (s *Service) evaluateEmergencyStop(ctx context.Context) (EmergencyStopState, error) {
	evaluator := s.emergencyStop
	if evaluator == nil {
		evaluator = processEmergencyStopEvaluator{}
	}
	return evaluator.EvaluateEmergencyStop(ctx)
}
