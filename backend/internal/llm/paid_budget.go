package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"time"

	"automation-hub-backend/internal/models"

	"github.com/google/uuid"
)

func (s *Service) reservePaidInferenceBudget(
	ctx context.Context,
	provider Provider,
	model Model,
	request GenerateRequest,
	decision *RouteDecision,
) (uuid.UUID, float64, int, int, error) {
	if !s.policy.PaidCallsAllowed || s.policy.DailyPaidBudgetEUR <= 0 {
		return uuid.Nil, 0, 0, 0, ErrPaidBudgetUnavailable
	}
	repository, ok := s.generationHistory.(PaidGenerationBudgetRepository)
	if !ok {
		return uuid.Nil, 0, 0, 0, ErrPaidBudgetUnavailable
	}
	if math.IsNaN(model.InputCostPerMillionTokensEUR) || math.IsInf(model.InputCostPerMillionTokensEUR, 0) || model.InputCostPerMillionTokensEUR < 0 ||
		math.IsNaN(model.OutputCostPerMillionTokensEUR) || math.IsInf(model.OutputCostPerMillionTokensEUR, 0) || model.OutputCostPerMillionTokensEUR < 0 ||
		math.IsNaN(model.EstimatedCostEUR) || math.IsInf(model.EstimatedCostEUR, 0) || model.EstimatedCostEUR < 0 {
		return uuid.Nil, 0, 0, 0, ErrPaidPricingUnavailable
	}
	inputTokens := estimateTokens(buildPrompt(request))
	outputTokens := request.MaxTokens
	if outputTokens <= 0 {
		outputTokens = 800
	}
	tokenCostEUR := estimateModelUsageCostEUR(model, inputTokens, outputTokens)
	reservationEUR := math.Max(tokenCostEUR, model.EstimatedCostEUR)
	if reservationEUR <= 0 || math.IsNaN(reservationEUR) || math.IsInf(reservationEUR, 0) {
		return uuid.Nil, 0, 0, 0, ErrPaidPricingUnavailable
	}
	if err := ctx.Err(); err != nil {
		return uuid.Nil, 0, 0, 0, err
	}
	id := uuid.New()
	var fallbackJSON string
	if decision != nil {
		encoded, err := json.Marshal(fallbackLabels(decision.FallbackPath))
		if err != nil {
			return uuid.Nil, 0, 0, 0, fmt.Errorf("encode fallback reservation metadata: %w", err)
		}
		fallbackJSON = string(encoded)
	}
	record := &models.LLMGenerationRecord{
		ID: id, ProviderID: provider.ID, ModelID: model.ID, ModelName: model.Name,
		Tier: model.Tier, Status: "reserved",
		Reason:           "paid inference budget reserved before provider dispatch; final usage is pending",
		EstimatedCostEUR: reservationEUR, InputTokens: inputTokens, OutputTokens: outputTokens,
		UsageSource: "estimated_uncertain", FallbackPathJSON: fallbackJSON,
		LoggedAt: time.Now().UTC(),
	}
	if s.now != nil {
		record.LoggedAt = s.currentTime()
	}
	if err := repository.ReservePaidGeneration(ctx, record, s.policy.DailyPaidBudgetEUR); err != nil {
		return uuid.Nil, 0, 0, 0, err
	}
	return id, reservationEUR, inputTokens, outputTokens, nil
}
