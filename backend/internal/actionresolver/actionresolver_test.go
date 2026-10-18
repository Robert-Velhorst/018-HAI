package actionresolver

import (
	"fmt"
	"math"
	"testing"
)

func TestDestructiveLowConfidenceBlocks(t *testing.T) {
	if Resolve(Action{Description: "delete account", Confidence: 0.4, Destructive: true}) != Block {
		t.Fatalf("destructive + low confidence must block")
	}
}

func TestMissingParamsOrLowConfidenceClarifies(t *testing.T) {
	if Resolve(Action{Description: "send email", Confidence: 0.9, MissingParams: []string{"recipient"}}) != Clarify {
		t.Fatalf("missing params should clarify")
	}
	if Resolve(Action{Description: "send email", Confidence: 0.3}) != Clarify {
		t.Fatalf("low confidence should clarify")
	}
}

func TestConfidentCompleteActionProceeds(t *testing.T) {
	if Resolve(Action{Description: "add note", Confidence: 0.95}) != Proceed {
		t.Fatalf("confident complete action should proceed")
	}
	// Destructive but confident and complete: still proceeds (autonomy gate/approval
	// handles the risk separately).
	if Resolve(Action{Description: "archive", Confidence: 0.95, Destructive: true}) != Proceed {
		t.Fatalf("destructive but confident should proceed at the resolver layer")
	}
}

func TestInvalidConfidenceCannotProceed(t *testing.T) {
	for _, confidence := range []float64{math.NaN(), math.Inf(1), math.Inf(-1), -0.01, 1.01} {
		t.Run(fmt.Sprintf("%v", confidence), func(t *testing.T) {
			if got := Resolve(Action{Confidence: confidence, Destructive: true}); got != Block {
				t.Fatalf("destructive action with invalid confidence resolved to %q, want block", got)
			}
			if got := Resolve(Action{Confidence: confidence}); got != Clarify {
				t.Fatalf("action with invalid confidence resolved to %q, want clarification", got)
			}
		})
	}
}
