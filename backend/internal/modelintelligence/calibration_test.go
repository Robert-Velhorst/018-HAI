package modelintelligence

import (
	"math"
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

func TestParseTriageOutputFailsClosed(t *testing.T) {
	if _, _, ok := parseTriageOutput("category=financial"); ok {
		t.Fatal("triage without summary must fail schema validation")
	}
	category, summary, ok := parseTriageOutput("category=financial; summary=review invoice")
	if !ok || category != "financial" || summary != "review invoice" {
		t.Fatalf("valid triage output = %q %q %v", category, summary, ok)
	}
}

func TestTheWilsonBoundStaysAProportionWhenNothingWasAccepted(t *testing.T) {
	for total := 1; total <= 40; total++ {
		bound := wilsonLowerBound(0, total)
		if bound != 0 || math.Signbit(bound) {
			t.Fatalf("no accepted outputs out of %d gave %v, want a plain zero", total, bound)
		}
	}
	if bound := wilsonLowerBound(8, 10); bound <= 0 || bound >= 1 {
		t.Fatalf("8 of 10 accepted gave %v, want a proportion", bound)
	}
}
