package modelintelligence

import (
	"fmt"
	"math"
	"sort"
	"time"
)

// ModelCalibration summarizes redacted operational outcomes for one model and
// lane. It never contains prompts, outputs, source content, or credentials.
type ModelCalibration struct {
	Lane                                RoutingLane `json:"lane"`
	ProviderID                          string      `json:"providerId"`
	ModelID                             string      `json:"modelId"`
	TotalRuns                           int         `json:"totalRuns"`
	ProviderCallSuccesses               int         `json:"providerCallSuccesses"`
	ProviderCallFailures                int         `json:"providerCallFailures"`
	EvaluatedRuns                       int         `json:"evaluatedRuns"`
	AcceptedOutputs                     int         `json:"acceptedOutputs"`
	RejectedOutputs                     int         `json:"rejectedOutputs"`
	NeedsReview                         int         `json:"needsReview"`
	UnvalidatedRuns                     int         `json:"unvalidatedRuns"`
	AcceptanceRate                      float64     `json:"acceptanceRate"`
	WilsonLowerBound                    float64     `json:"wilsonLowerBound"`
	ProviderReportedUsageRuns           int         `json:"providerReportedUsageRuns"`
	PartialUsageRuns                    int         `json:"partialUsageRuns"`
	EstimatedUsageRuns                  int         `json:"estimatedUsageRuns"`
	InvalidUsageRuns                    int         `json:"invalidUsageRuns"`
	AverageProviderReportedInputTokens  float64     `json:"averageProviderReportedInputTokens"`
	AverageProviderReportedOutputTokens float64     `json:"averageProviderReportedOutputTokens"`
	AveragePartialInputTokens           float64     `json:"averagePartialInputTokens"`
	AveragePartialOutputTokens          float64     `json:"averagePartialOutputTokens"`
	AverageEstimatedInputTokens         float64     `json:"averageEstimatedInputTokens"`
	AverageEstimatedOutputTokens        float64     `json:"averageEstimatedOutputTokens"`
	ObservedSpeedSamples                int         `json:"observedSpeedSamples"`
	AverageInputTokens                  float64     `json:"averageInputTokens"`
	AverageOutputTokens                 float64     `json:"averageOutputTokens"`
	AverageDurationMs                   float64     `json:"averageDurationMs"`
	AverageTokensPerSecond              float64     `json:"averageTokensPerSecond"`
	AverageCostEUR                      float64     `json:"averageCostEur"`
	AverageFallbackDepth                float64     `json:"averageFallbackDepth"`
	Confidence                          string      `json:"confidence"`
	LastObservedAt                      time.Time   `json:"lastObservedAt"`
}

type CalibrationSummary struct {
	TotalRuns       int                `json:"totalRuns"`
	EvaluatedRuns   int                `json:"evaluatedRuns"`
	AcceptedOutputs int                `json:"acceptedOutputs"`
	RejectedOutputs int                `json:"rejectedOutputs"`
	NeedsReview     int                `json:"needsReview"`
	UnvalidatedRuns int                `json:"unvalidatedRuns"`
	Models          []ModelCalibration `json:"models"`
	LaneLeaders     []LaneWinner       `json:"laneLeaders"`
	GeneratedAt     time.Time          `json:"generatedAt"`
	Explanation     string             `json:"explanation"`
}

type calibrationAccumulator struct {
	model                                 ModelCalibration
	sumTPS, sumCost, sumFallback          float64
	sumDuration                           float64
	sumReportedInput, sumReportedOutput   float64
	sumPartialInput, sumPartialOutput     float64
	sumEstimatedInput, sumEstimatedOutput float64
}

func (s *TelemetryStore) Calibration() CalibrationSummary {
	s.mu.Lock()
	records := make([]ModelRunTelemetry, len(s.records))
	copy(records, s.records)
	s.mu.Unlock()

	byKey := make(map[string]*calibrationAccumulator)
	summary := CalibrationSummary{
		GeneratedAt: time.Now().UTC(),
		Explanation: "Leaders require evaluated outputs and are ranked by conservative accepted-output evidence before token, cost, latency, or speed. Fully provider-reported, partial, and estimated token counts are aggregated separately; observed speed uses only fully provider-reported output counts. Acceptance is validator-specific and is not automatically external truth.",
	}
	for _, record := range records {
		status := normalizeValidationStatus(record.ValidationStatus)
		key := string(record.Lane) + "\x00" + record.ProviderID + "\x00" + record.ModelID
		current := byKey[key]
		if current == nil {
			current = &calibrationAccumulator{model: ModelCalibration{
				Lane: record.Lane, ProviderID: record.ProviderID, ModelID: record.ModelID,
			}}
			byKey[key] = current
		}
		current.model.TotalRuns++
		summary.TotalRuns++
		if record.OK {
			current.model.ProviderCallSuccesses++
		} else {
			current.model.ProviderCallFailures++
		}
		if status.evaluated() {
			current.model.EvaluatedRuns++
			summary.EvaluatedRuns++
			if status.accepted() {
				current.model.AcceptedOutputs++
				summary.AcceptedOutputs++
			} else if status == ValidationNeedsReview {
				current.model.NeedsReview++
				summary.NeedsReview++
			} else {
				current.model.RejectedOutputs++
				summary.RejectedOutputs++
			}
		} else {
			current.model.UnvalidatedRuns++
			summary.UnvalidatedRuns++
		}
		switch normalizeTokenUsageSource(record.UsageSource) {
		case TokenUsageProviderReported:
			current.model.ProviderReportedUsageRuns++
			current.sumReportedInput += float64(record.InputTokens)
			current.sumReportedOutput += float64(record.OutputTokens)
			if record.OutputTokens > 0 && record.DurationMs > 0 {
				current.sumTPS += float64(record.OutputTokens) / (float64(record.DurationMs) / 1000)
				current.model.ObservedSpeedSamples++
			}
		case TokenUsageProviderReportedPartial:
			current.model.PartialUsageRuns++
			current.sumPartialInput += float64(record.InputTokens)
			current.sumPartialOutput += float64(record.OutputTokens)
		case TokenUsageEstimated, TokenUsageEstimatedUncertain:
			current.model.EstimatedUsageRuns++
			current.sumEstimatedInput += float64(record.InputTokens)
			current.sumEstimatedOutput += float64(record.OutputTokens)
		case TokenUsageProviderReportInvalid:
			current.model.InvalidUsageRuns++
		}
		current.sumDuration += float64(record.DurationMs)
		current.sumCost += record.EstimatedCostEUR
		current.sumFallback += float64(record.FallbackDepth)
		if record.CreatedAt.After(current.model.LastObservedAt) {
			current.model.LastObservedAt = record.CreatedAt
		}
	}

	for _, current := range byKey {
		model := current.model
		if model.EvaluatedRuns > 0 {
			model.AcceptanceRate = float64(model.AcceptedOutputs) / float64(model.EvaluatedRuns)
			model.WilsonLowerBound = wilsonLowerBound(model.AcceptedOutputs, model.EvaluatedRuns)
		}
		if model.ProviderReportedUsageRuns > 0 {
			count := float64(model.ProviderReportedUsageRuns)
			model.AverageProviderReportedInputTokens = current.sumReportedInput / count
			model.AverageProviderReportedOutputTokens = current.sumReportedOutput / count
			model.AverageInputTokens = model.AverageProviderReportedInputTokens
			model.AverageOutputTokens = model.AverageProviderReportedOutputTokens
		}
		if model.PartialUsageRuns > 0 {
			count := float64(model.PartialUsageRuns)
			model.AveragePartialInputTokens = current.sumPartialInput / count
			model.AveragePartialOutputTokens = current.sumPartialOutput / count
		}
		if model.EstimatedUsageRuns > 0 {
			count := float64(model.EstimatedUsageRuns)
			model.AverageEstimatedInputTokens = current.sumEstimatedInput / count
			model.AverageEstimatedOutputTokens = current.sumEstimatedOutput / count
		}
		if model.TotalRuns > 0 {
			runs := float64(model.TotalRuns)
			model.AverageDurationMs = current.sumDuration / runs
			model.AverageCostEUR = current.sumCost / runs
			model.AverageFallbackDepth = current.sumFallback / runs
		}
		if model.ObservedSpeedSamples > 0 {
			model.AverageTokensPerSecond = current.sumTPS / float64(model.ObservedSpeedSamples)
		}
		model.Confidence = calibrationConfidence(model.EvaluatedRuns)
		roundCalibration(&model)
		summary.Models = append(summary.Models, model)
	}

	sort.SliceStable(summary.Models, func(i, j int) bool {
		left, right := summary.Models[i], summary.Models[j]
		if left.Lane != right.Lane {
			return laneOrder(left.Lane) < laneOrder(right.Lane)
		}
		return betterCalibration(left, right)
	})
	for _, lane := range allLanes() {
		for _, model := range summary.Models {
			if model.Lane != lane || model.AcceptedOutputs == 0 {
				continue
			}
			summary.LaneLeaders = append(summary.LaneLeaders, LaneWinner{
				Lane: model.Lane, ProviderID: model.ProviderID, ModelID: model.ModelID,
				TokensPerSecond: model.AverageTokensPerSecond, ObservedSpeedSamples: model.ObservedSpeedSamples, Runs: model.TotalRuns,
				EvaluatedRuns: model.EvaluatedRuns, AcceptedOutputs: model.AcceptedOutputs,
				AcceptanceRate: model.AcceptanceRate, Confidence: model.Confidence,
				AverageTokens:     model.AverageProviderReportedInputTokens + model.AverageProviderReportedOutputTokens,
				AverageDurationMs: model.AverageDurationMs, AverageCostEUR: model.AverageCostEUR,
				Reason: fmt.Sprintf("%d/%d evaluated outputs accepted; conservative lower bound %.1f%%. Efficiency breaks ties only after outcome evidence.", model.AcceptedOutputs, model.EvaluatedRuns, model.WilsonLowerBound*100),
			})
			break
		}
	}
	return summary
}

func betterCalibration(left, right ModelCalibration) bool {
	if left.WilsonLowerBound != right.WilsonLowerBound {
		return left.WilsonLowerBound > right.WilsonLowerBound
	}
	if left.AcceptanceRate != right.AcceptanceRate {
		return left.AcceptanceRate > right.AcceptanceRate
	}
	if left.EvaluatedRuns != right.EvaluatedRuns {
		return left.EvaluatedRuns > right.EvaluatedRuns
	}
	if left.AverageCostEUR != right.AverageCostEUR {
		return left.AverageCostEUR < right.AverageCostEUR
	}
	if left.ProviderReportedUsageRuns > 0 && right.ProviderReportedUsageRuns > 0 {
		leftTokens := left.AverageProviderReportedInputTokens + left.AverageProviderReportedOutputTokens
		rightTokens := right.AverageProviderReportedInputTokens + right.AverageProviderReportedOutputTokens
		if leftTokens != rightTokens {
			return leftTokens < rightTokens
		}
	}
	if left.AverageDurationMs != right.AverageDurationMs {
		return left.AverageDurationMs < right.AverageDurationMs
	}
	if left.ProviderID != right.ProviderID {
		return left.ProviderID < right.ProviderID
	}
	return left.ModelID < right.ModelID
}

func wilsonLowerBound(successes, total int) float64 {
	if total <= 0 || successes < 0 || successes > total {
		return 0
	}
	z := 1.96
	n := float64(total)
	p := float64(successes) / n
	denominator := 1 + z*z/n
	center := p + z*z/(2*n)
	margin := z * math.Sqrt((p*(1-p)+z*z/(4*n))/n)
	return (center - margin) / denominator
}

func calibrationConfidence(evaluated int) string {
	switch {
	case evaluated >= 20:
		return "established"
	case evaluated >= 5:
		return "emerging"
	default:
		return "insufficient"
	}
}

func laneOrder(lane RoutingLane) int {
	for index, candidate := range allLanes() {
		if lane == candidate {
			return index
		}
	}
	return len(allLanes())
}

func roundCalibration(model *ModelCalibration) {
	model.AcceptanceRate = math.Round(model.AcceptanceRate*10000) / 10000
	model.WilsonLowerBound = math.Round(model.WilsonLowerBound*10000) / 10000
	model.AverageProviderReportedInputTokens = math.Round(model.AverageProviderReportedInputTokens*100) / 100
	model.AverageProviderReportedOutputTokens = math.Round(model.AverageProviderReportedOutputTokens*100) / 100
	model.AveragePartialInputTokens = math.Round(model.AveragePartialInputTokens*100) / 100
	model.AveragePartialOutputTokens = math.Round(model.AveragePartialOutputTokens*100) / 100
	model.AverageEstimatedInputTokens = math.Round(model.AverageEstimatedInputTokens*100) / 100
	model.AverageEstimatedOutputTokens = math.Round(model.AverageEstimatedOutputTokens*100) / 100
	model.AverageInputTokens = math.Round(model.AverageInputTokens*100) / 100
	model.AverageOutputTokens = math.Round(model.AverageOutputTokens*100) / 100
	model.AverageDurationMs = math.Round(model.AverageDurationMs*100) / 100
	model.AverageTokensPerSecond = math.Round(model.AverageTokensPerSecond*100) / 100
	model.AverageCostEUR = math.Round(model.AverageCostEUR*1000000) / 1000000
	model.AverageFallbackDepth = math.Round(model.AverageFallbackDepth*100) / 100
}
