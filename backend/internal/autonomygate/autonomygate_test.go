package autonomygate

import (
	"math"
	"testing"
)

func TestApprovalAlwaysAuto(t *testing.T) {
	if Decide(Signals{Risk: "high", Reversible: false, Approved: true}) != Auto {
		t.Fatalf("explicit approval should permit auto")
	}
}

func TestHighRiskIrreversibleBlocks(t *testing.T) {
	if Decide(Signals{Confidence: 0.99, Risk: "high", Reversible: false}) != Block {
		t.Fatalf("high risk + irreversible must block")
	}
}

func TestHighRiskOrLowConfidenceReviews(t *testing.T) {
	if Decide(Signals{Confidence: 0.99, Risk: "high", Reversible: true}) != Review {
		t.Fatalf("high risk reversible should review")
	}
	if Decide(Signals{Confidence: 0.3, Risk: "low", Reversible: true}) != Review {
		t.Fatalf("low confidence should review")
	}
}

func TestOnlyLowRiskHighConfidenceCasesAuto(t *testing.T) {
	if Decide(Signals{Confidence: 0.9, Risk: "low", Reversible: true}) != Auto {
		t.Fatalf("safe case should auto")
	}
	if Decide(Signals{Confidence: 0.9, Risk: "medium", Reversible: true}) != Review {
		t.Fatalf("medium risk should require review before execution")
	}
	if Decide(Signals{Confidence: 0.9, Risk: "medium", Reversible: false}) != Review {
		t.Fatalf("medium risk irreversible should review")
	}
}

func TestUnrecognizedRiskRequiresReview(t *testing.T) {
	for _, test := range []struct {
		name string
		risk string
	}{
		{name: "empty", risk: ""},
		{name: "unknown", risk: "unknown"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := Decide(Signals{Confidence: 0.9, Risk: test.risk, Reversible: true}); got != Review {
				t.Fatalf("unrecognized risk %q: got %q, want %q", test.risk, got, Review)
			}
		})
	}
}

func TestInvalidConfidenceRequiresReview(t *testing.T) {
	for _, confidence := range []struct {
		name  string
		value float64
	}{
		{name: "NaN", value: math.NaN()},
		{name: "above range", value: 1.01},
		{name: "positive infinity", value: math.Inf(1)},
	} {
		t.Run(confidence.name, func(t *testing.T) {
			if got := Decide(Signals{Confidence: confidence.value, Risk: "low", Reversible: true}); got != Review {
				t.Fatalf("invalid confidence %v: got %q, want %q", confidence.value, got, Review)
			}
		})
	}
}

func TestIrreversibleLowRiskRequiresReview(t *testing.T) {
	if got := Decide(Signals{Confidence: 0.9, Risk: "low", Reversible: false}); got != Review {
		t.Fatalf("irreversible low-risk action: got %q, want %q", got, Review)
	}
}
