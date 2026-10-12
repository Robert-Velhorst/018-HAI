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
	Lane                   RoutingLane `json:"lane"`
	ProviderID             string      `json:"providerId"`
	ModelID                string      `json:"modelId"`
	TotalRuns              int         `json:"totalRuns"`
	ProviderCallSuccesses  int         `json:"providerCallSuccesses"`
	ProviderCallFailures   int         `json:"providerCallFailures"`
	EvaluatedRuns          int         `json:"evaluatedRuns"`
	AcceptedOutputs        int         `json:"acceptedOutputs"`
	RejectedOutputs        int         `json:"rejectedOutputs"`
	NeedsReview            int         `json:"needsReview"`
	UnvalidatedRuns        int         `json:"unvalidatedRuns"`
	AcceptanceRate         float64     `json:"acceptanceRate"`
	WilsonLowerBound       float64     `json:"wilsonLowerBound"`
	AverageInputTokens     float64     `json:"averageInputTokens"`
	AverageOutputTokens    float64     `json:"averageOutputTokens"`
	AverageDurationMs      float64     `json:"averageDurationMs"`
	AverageTokensPerSecond float64     `json:"averageTokensPerSecond"`
	AverageCostEUR         float64     `json:"averageCostEur"`
	AverageFallbackDepth   float64     `json:"averageFallbackDepth"`
	Confidence             string      `json:"confidence"`
	LastObservedAt         time.Time   `json:"lastObservedAt"`
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
	model                                             ModelCalibration
	sumInput, sumOutput, sumTPS, sumCost, sumFallback float64
	sumDuration                                       float64
}

func (s *TelemetryStore) Calibration() CalibrationSummary {
	s.mu.Lock()
	records := make([]ModelRunTelemetry, len(s.records))
	copy(records, s.records)
	s.mu.Unlock()

	byKey := make(map[string]*calibrationAccumulator)
	summary := CalibrationSummary{
		GeneratedAt: time.Now().UTC(),
		Explanation: "Leaders require evaluated outputs and are ranked by conservative accepted-output evidence before token, cost, latency, or speed. Acceptance is validator-specific and is not automatically external truth.",
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
		current.sumInput += float64(record.InputTokens)
		current.sumOutput += float64(record.OutputTokens)
		current.sumDuration += float64(record.DurationMs)
		current.sumTPS += record.TokensPerSecond
		current.sumCost += record.EstimatedCostEUR
		current.sumFallback += float64(record.FallbackDepth)
		if record.CreatedAt.After(current.model.LastObservedAt) {
			current.model.LastObservedAt = record.CreatedAt
		}
	}

	for _, current := range byKey {
		model := current.model
		runs := float64(model.TotalRuns)
		if model.EvaluatedRuns > 0 {
			model.AcceptanceRate = float64(model.AcceptedOutputs) / float64(model.EvaluatedRuns)
			model.WilsonLowerBound = wilsonLowerBound(model.AcceptedOutputs, model.EvaluatedRuns)
		}
		if runs > 0 {
			model.AverageInputTokens = current.sumInput / runs
			model.AverageOutputTokens = current.sumOutput / runs
			model.AverageDurationMs = current.sumDuration / runs
			model.AverageTokensPerSecond = current.sumTPS / runs
			model.AverageCostEUR = current.sumCost / runs
			model.AverageFallbackDepth = current.sumFallback / runs
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
				TokensPerSecond: model.AverageTokensPerSecond, Runs: model.TotalRuns,
				EvaluatedRuns: model.EvaluatedRuns, AcceptedOutputs: model.AcceptedOutputs,
				AcceptanceRate: model.AcceptanceRate, Confidence: model.Confidence,
				AverageTokens:     model.AverageInputTokens + model.AverageOutputTokens,
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
	leftTokens := left.AverageInputTokens + left.AverageOutputTokens
	rightTokens := right.AverageInputTokens + right.AverageOutputTokens
	if leftTokens != rightTokens {
		return leftTokens < rightTokens
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
	if successes == 0 {
		// The bound is exactly zero here: the centre and the margin are the same
		// quantity. Computing it anyway leaves floating-point noise a few parts
		// in 10^18 either side of zero, which is not an acceptance rate and,
		// when it lands on negative zero, is not even stable in storage.
		return 0
	}
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
	model.AverageInputTokens = math.Round(model.AverageInputTokens*100) / 100
	model.AverageOutputTokens = math.Round(model.AverageOutputTokens*100) / 100
	model.AverageDurationMs = math.Round(model.AverageDurationMs*100) / 100
	model.AverageTokensPerSecond = math.Round(model.AverageTokensPerSecond*100) / 100
	model.AverageCostEUR = math.Round(model.AverageCostEUR*1000000) / 1000000
	model.AverageFallbackDepth = math.Round(model.AverageFallbackDepth*100) / 100
}
