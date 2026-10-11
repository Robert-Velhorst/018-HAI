package modelintelligence

import (
	"testing"
	"time"
)

func TestLaneLeaderRequiresEvaluatedOutput(t *testing.T) {
	store := NewTelemetryStore()
	store.Record(ModelRunTelemetry{
		ProviderID: "fast", ModelID: "unvalidated", Lane: LaneDrafting,
		OK: true, TokensPerSecond: 500, ValidationStatus: ValidationUnvalidated,
		CreatedAt: time.Now().UTC(),
	})
	if leaders := store.LaneWinners(); len(leaders) != 0 {
		t.Fatalf("unvalidated provider success must not produce a lane leader: %#v", leaders)
	}
}

func TestLaneLeaderRanksAcceptedOutcomesBeforeSpeed(t *testing.T) {
	store := NewTelemetryStore()
	now := time.Now().UTC()
	for index := 0; index < 4; index++ {
		status := ValidationFailed
		if index == 0 {
			status = ValidationSchemaValidated
		}
		store.Record(ModelRunTelemetry{
			ProviderID: "fast", ModelID: "weak", Lane: LaneFastTriage,
			OK: true, TokensPerSecond: 500, ValidationStatus: status,
			InputTokens: 10, OutputTokens: 5, DurationMs: 20, CreatedAt: now.Add(time.Duration(index) * time.Second),
		})
		store.Record(ModelRunTelemetry{
			ProviderID: "steady", ModelID: "capable", Lane: LaneFastTriage,
			OK: true, TokensPerSecond: 25, ValidationStatus: ValidationSchemaValidated,
			InputTokens: 30, OutputTokens: 15, DurationMs: 200, CreatedAt: now.Add(time.Duration(index) * time.Second),
		})
	}
	leaders := store.LaneWinners()
	if len(leaders) != 1 {
		t.Fatalf("leaders = %#v, want one calibrated lane", leaders)
	}
	if leaders[0].ProviderID != "steady" || leaders[0].ModelID != "capable" {
		t.Fatalf("completion-first leader = %#v, want steady/capable", leaders[0])
	}
	if leaders[0].AcceptanceRate != 1 || leaders[0].AcceptedOutputs != 4 {
		t.Fatalf("leader outcome evidence = %#v", leaders[0])
	}
}

func TestCalibrationSeparatesProviderSuccessFromValidation(t *testing.T) {
	store := NewTelemetryStore()
	now := time.Now().UTC()
	store.Record(ModelRunTelemetry{
		ProviderID: "local", ModelID: "model", Lane: LaneVerifier, OK: true,
		ValidationStatus: ValidationNeedsReview, ValidationMethod: "claim_check_v1", CreatedAt: now,
	})
	store.Record(ModelRunTelemetry{
		ProviderID: "local", ModelID: "model", Lane: LaneVerifier, OK: false,
		ValidationStatus: ValidationUnvalidated, CreatedAt: now.Add(time.Second),
	})
	summary := store.Calibration()
	if summary.TotalRuns != 2 || summary.EvaluatedRuns != 1 || summary.NeedsReview != 1 || summary.UnvalidatedRuns != 1 {
		t.Fatalf("summary = %#v", summary)
	}
	if len(summary.Models) != 1 || summary.Models[0].ProviderCallSuccesses != 1 || summary.Models[0].ProviderCallFailures != 1 {
		t.Fatalf("model calibration = %#v", summary.Models)
	}
	if len(summary.LaneLeaders) != 0 {
		t.Fatalf("needs-review output cannot become a leader: %#v", summary.LaneLeaders)
	}
}

func TestCalibrationSeparatesUsageProvenanceAndCountsObservedSpeedSamples(t *testing.T) {
	store := NewTelemetryStore()
	now := time.Now().UTC()
	records := []ModelRunTelemetry{
		{
			ProviderID: "local", ModelID: "model", Lane: LaneDrafting,
			UsageSource: TokenUsageProviderReported, InputTokens: 20, OutputTokens: 10,
			DurationMs: 2000, TokensPerSecond: 900, ValidationStatus: ValidationSchemaValidated,
			CreatedAt: now,
		},
		{
			ProviderID: "local", ModelID: "model", Lane: LaneDrafting,
			UsageSource: TokenUsageProviderReported, InputTokens: 30, OutputTokens: 0,
			DurationMs: 2000, TokensPerSecond: 700, ValidationStatus: ValidationSchemaValidated,
			CreatedAt: now.Add(time.Second),
		},
		{
			ProviderID: "local", ModelID: "model", Lane: LaneDrafting,
			UsageSource: TokenUsageProviderReported, InputTokens: 40, OutputTokens: 20,
			DurationMs: 0, TokensPerSecond: 600, ValidationStatus: ValidationSchemaValidated,
			CreatedAt: now.Add(2 * time.Second),
		},
		{
			ProviderID: "local", ModelID: "model", Lane: LaneDrafting,
			UsageSource: TokenUsageProviderReportedPartial, InputTokens: 80, OutputTokens: 40,
			DurationMs: 1000, TokensPerSecond: 300, ValidationStatus: ValidationSchemaValidated,
			CreatedAt: now.Add(3 * time.Second),
		},
		{
			ProviderID: "local", ModelID: "model", Lane: LaneDrafting,
			UsageSource: TokenUsageEstimated, InputTokens: 1000, OutputTokens: 500,
			DurationMs: 100, TokensPerSecond: 10000, ValidationStatus: ValidationSchemaValidated,
			CreatedAt: now.Add(4 * time.Second),
		},
		{
			ProviderID: "local", ModelID: "model", Lane: LaneDrafting,
			UsageSource: TokenUsageEstimatedUncertain, InputTokens: 2000, OutputTokens: 1000,
			DurationMs: 100, TokensPerSecond: 20000, ValidationStatus: ValidationSchemaValidated,
			CreatedAt: now.Add(5 * time.Second),
		},
		{
			ProviderID: "local", ModelID: "model", Lane: LaneDrafting,
			UsageSource: TokenUsageProviderReportInvalid, InputTokens: 9000, OutputTokens: 9000,
			DurationMs: 1, TokensPerSecond: 9000000, ValidationStatus: ValidationSchemaValidated,
			CreatedAt: now.Add(6 * time.Second),
		},
	}
	for _, record := range records {
		store.Record(record)
	}

	summary := store.Calibration()
	if len(summary.Models) != 1 {
		t.Fatalf("models = %#v, want one model aggregate", summary.Models)
	}
	model := summary.Models[0]
	if model.ProviderReportedUsageRuns != 3 || model.PartialUsageRuns != 1 || model.EstimatedUsageRuns != 2 || model.InvalidUsageRuns != 1 {
		t.Fatalf("usage cohort counts = %#v", model)
	}
	if model.AverageProviderReportedInputTokens != 30 || model.AverageProviderReportedOutputTokens != 10 {
		t.Fatalf("provider-reported averages = %.2f/%.2f, want 30/10", model.AverageProviderReportedInputTokens, model.AverageProviderReportedOutputTokens)
	}
	if model.AverageInputTokens != model.AverageProviderReportedInputTokens || model.AverageOutputTokens != model.AverageProviderReportedOutputTokens {
		t.Fatalf("compatibility averages must remain provider-reported only: %#v", model)
	}
	if model.AveragePartialInputTokens != 80 || model.AveragePartialOutputTokens != 40 {
		t.Fatalf("partial usage averages = %.2f/%.2f, want 80/40", model.AveragePartialInputTokens, model.AveragePartialOutputTokens)
	}
	if model.AverageEstimatedInputTokens != 1500 || model.AverageEstimatedOutputTokens != 750 {
		t.Fatalf("estimated usage averages = %.2f/%.2f, want 1500/750", model.AverageEstimatedInputTokens, model.AverageEstimatedOutputTokens)
	}
	if model.ObservedSpeedSamples != 1 || model.AverageTokensPerSecond != 5 {
		t.Fatalf("observed speed = %.2f tok/s across %d samples, want 5 across 1", model.AverageTokensPerSecond, model.ObservedSpeedSamples)
	}
	if len(summary.LaneLeaders) != 1 || summary.LaneLeaders[0].AverageTokens != 40 || summary.LaneLeaders[0].TokensPerSecond != 5 {
		t.Fatalf("leader efficiency metrics = %#v, want fully reported counts and observed speed only", summary.LaneLeaders)
	}
}

func TestParseTriageOutputFailsClosed(t *testing.T) {
	if _, _, ok := parseTriageOutput("category=financial"); ok {
		t.Fatal("triage without summary must fail schema validation")
	}
	category, summary, ok := parseTriageOutput("category=financial; summary=review invoice")
	if !ok || category != "financial" || summary != "review invoice" {
		t.Fatalf("valid triage output = %q %q %v", category, summary, ok)
	}
}
