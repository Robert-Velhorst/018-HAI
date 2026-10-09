package modelintelligence

import (
	"context"
	"reflect"
	"time"

	"automation-hub-backend/internal/safety"
)

// InferenceRequest is a bounded model call routed to a lane.
type InferenceRequest struct {
	Lane                       RoutingLane
	Prompt                     string
	MaxInputTokens             int
	MaxOutputTokens            int
	RequireReportedOutputUsage bool
	Effort                     ReasoningEffort
}

// InferenceResult is the bounded outcome of a model call plus telemetry.
type InferenceResult struct {
	ProviderID           string      `json:"providerId"`
	ModelID              string      `json:"modelId"`
	Lane                 RoutingLane `json:"lane"`
	Output               string      `json:"output"`
	InputTokensEstimate  int         `json:"inputTokensEstimate"`
	OutputTokensEstimate int         `json:"outputTokensEstimate"`
	InputTokensActual    int         `json:"inputTokensActual,omitempty"`
	OutputTokensActual   int         `json:"outputTokensActual,omitempty"`
	InputUsageReported   bool        `json:"inputUsageReported"`
	OutputUsageReported  bool        `json:"outputUsageReported"`
	DurationMs           int64       `json:"durationMs"`
	TokensPerSecond      float64     `json:"tokensPerSecond"`
	OK                   bool        `json:"ok"`
	Error                string      `json:"error,omitempty"`
}

// ProbeResult is the truthful outcome of probing a provider (§10.17).
type ProbeResult struct {
	ProviderID string         `json:"providerId"`
	Status     ProviderStatus `json:"status"`
	ModelsSeen int            `json:"modelsSeen"`
	DurationMs int64          `json:"durationMs"`
	Detail     string         `json:"detail,omitempty"`
	CheckedAt  time.Time      `json:"checkedAt"`
}

// Provider is a model provider adapter. Providers must report truthful status,
// never mark themselves active without a successful probe, and never execute
// external actions.
type Provider interface {
	ID() string
	DisplayName() string
	// Profiles returns the architecture-aware profiles this provider serves.
	Profiles() []ModelProfile
	// Probe checks reachability and returns a truthful status.
	Probe(ctx context.Context, now time.Time) ProbeResult
	// Generate performs a bounded inference call. It must return an error (not a
	// fabricated result) when the provider is not usable.
	Generate(ctx context.Context, req InferenceRequest, now time.Time) (InferenceResult, error)
}

// Interfaces can hold a typed nil adapter or gate; these are not usable dependencies.
func nilModelDependency(value any) bool {
	if value == nil {
		return true
	}
	switch reflected := reflect.ValueOf(value); reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}

type redactedModelError struct {
	cause error
}

func (e redactedModelError) Error() string { return safety.RedactSecrets(e.cause.Error()) }
func (e redactedModelError) Unwrap() error { return e.cause }

func redactModelError(err error) error {
	if err == nil {
		return nil
	}
	return redactedModelError{cause: err}
}

// estimateTokens is a deterministic, provider-agnostic approximation used
// only when a provider does not report exact usage.
func estimateTokens(s string) int {
	n := len(s) / 4
	if n < 1 && len(s) > 0 {
		return 1
	}
	return n
}
