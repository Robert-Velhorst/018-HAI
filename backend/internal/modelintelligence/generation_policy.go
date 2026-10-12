package modelintelligence

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

type generationLimits struct {
	maxInputTokens  int
	maxOutputTokens int
}

// authorizeGeneration applies the controls available to Model Intelligence
// before any provider call. Paid profiles must use the canonical LLM policy
// router, which owns paid-use approval and budget accounting.
func (s *Service) authorizeGeneration(profile ModelProfile, prompt string, requestedOutputTokens int) (generationLimits, error) {
	if (profile.Paid != nil && *profile.Paid) || profile.BillingStatus == BillingPaid {
		return generationLimits{}, fmt.Errorf("modelintelligence: paid model %s is blocked; paid calls require the canonical LLM approval and budget boundary", profile.Key())
	}
	if profile.BillingStatus != BillingUnmetered {
		return generationLimits{}, fmt.Errorf("modelintelligence: model %s has unknown billing; generation is blocked until the operator attests direct local, unmetered inference or the request is routed through the canonical LLM policy router", profile.Key())
	}
	if !profile.Local {
		return generationLimits{}, fmt.Errorf("modelintelligence: model %s lacks an exact operator attestation for local inference; external generation requires the canonical LLM policy router", profile.Key())
	}

	budget := s.TokenBudgetDefaults()
	if budget.MaximumInputTokens <= 0 || budget.MaximumOutputTokens <= 0 {
		return generationLimits{}, fmt.Errorf("modelintelligence: generation blocked because the token budget is zero or unavailable")
	}
	if !utf8.ValidString(prompt) {
		return generationLimits{}, fmt.Errorf("modelintelligence: prompt is not valid UTF-8")
	}
	// Tokenizer implementations differ. Use UTF-8 bytes as the conservative
	// admission ceiling; the ~4-byte estimate remains telemetry only.
	inputUpperBound := len(prompt)
	if inputUpperBound > budget.MaximumInputTokens {
		return generationLimits{}, fmt.Errorf("modelintelligence: input UTF-8 byte upper bound %d exceeds the configured token maximum %d", inputUpperBound, budget.MaximumInputTokens)
	}
	contextOutputLimit := budget.MaximumOutputTokens
	if profile.ContextWindow > 0 {
		contextOutputLimit = profile.ContextWindow - inputUpperBound
		if contextOutputLimit <= 0 {
			return generationLimits{}, fmt.Errorf("modelintelligence: input leaves no verified context window for output")
		}
	}
	if requestedOutputTokens <= 0 {
		return generationLimits{}, fmt.Errorf("modelintelligence: requested output token limit must be positive")
	}
	if requestedOutputTokens > budget.MaximumOutputTokens {
		requestedOutputTokens = budget.MaximumOutputTokens
	}
	if requestedOutputTokens > contextOutputLimit {
		requestedOutputTokens = contextOutputLimit
	}
	return generationLimits{maxInputTokens: budget.MaximumInputTokens, maxOutputTokens: requestedOutputTokens}, nil
}

func validateInferenceRequest(req InferenceRequest) error {
	if !utf8.ValidString(req.Prompt) {
		return fmt.Errorf("modelintelligence: prompt is not valid UTF-8")
	}
	if req.MaxInputTokens <= 0 || len(req.Prompt) > req.MaxInputTokens {
		return fmt.Errorf("modelintelligence: input UTF-8 byte upper bound %d exceeds or lacks a positive input token limit", len(req.Prompt))
	}
	if req.MaxOutputTokens <= 0 {
		return fmt.Errorf("modelintelligence: output token limit must be positive")
	}
	return nil
}

func validateInferenceResult(req InferenceRequest, result *InferenceResult) error {
	if result == nil {
		return fmt.Errorf("modelintelligence: provider returned no inference result")
	}
	if !utf8.ValidString(result.Output) {
		return fmt.Errorf("modelintelligence: provider returned output that is not valid UTF-8")
	}
	if result.InputUsageReported {
		if result.InputTokensActual < 0 {
			return fmt.Errorf("modelintelligence: provider reported negative input token usage")
		}
		if result.InputTokensActual > req.MaxInputTokens {
			return fmt.Errorf("modelintelligence: provider-reported input tokens %d exceed request limit %d", result.InputTokensActual, req.MaxInputTokens)
		}
	}
	if result.OutputUsageReported {
		if result.OutputTokensActual < 0 {
			return fmt.Errorf("modelintelligence: provider reported negative output token usage")
		}
		if result.OutputTokensActual > req.MaxOutputTokens {
			return fmt.Errorf("modelintelligence: provider-reported output tokens %d exceed request limit %d", result.OutputTokensActual, req.MaxOutputTokens)
		}
	}
	if req.RequireReportedOutputUsage && !result.OutputUsageReported {
		return fmt.Errorf("modelintelligence: provider did not report output token usage; generated output was withheld")
	}
	if result.OutputUsageReported && strings.TrimSpace(result.Output) != "" && result.OutputTokensActual == 0 {
		return fmt.Errorf("modelintelligence: provider reported zero output tokens for non-empty output")
	}
	if result.InputUsageReported && strings.TrimSpace(req.Prompt) != "" && result.InputTokensActual == 0 {
		return fmt.Errorf("modelintelligence: provider reported zero input tokens for a non-empty prompt")
	}
	// Estimates remain explicitly estimates; the provider-reported counters are
	// retained separately and are used to validate exact request limits when present.
	result.InputTokensEstimate = estimateTokens(req.Prompt)
	result.OutputTokensEstimate = estimateTokens(result.Output)
	if result.OutputUsageReported && result.DurationMs > 0 {
		result.TokensPerSecond = float64(result.OutputTokensActual) / (float64(result.DurationMs) / 1000)
	}
	return nil
}
