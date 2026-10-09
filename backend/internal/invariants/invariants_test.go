package invariants

import (
	"math"
	"strings"
	"testing"

	"automation-hub-backend/internal/models"
)

func TestConfidenceMustBeFinite(t *testing.T) {
	for _, value := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		violations := ValidateMemory(models.ContextMemory{Content: "x", Kind: "k", Confidence: value})
		if len(violations) != 1 || violations[0].Field != "confidence" || violations[0].Rule != "finite" {
			t.Fatalf("non-finite confidence %v: violations=%+v", value, violations)
		}
	}
}

func TestValidMemoryHasNoViolations(t *testing.T) {
	m := models.ContextMemory{Content: "prefer local models", Kind: "preference", Confidence: 0.8, Tags: "llm,routing"}
	if v := ValidateMemory(m); !Valid(v) {
		t.Fatalf("valid memory reported violations: %+v", v)
	}
}

func TestValidateMemoryFlagsEachBrokenRule(t *testing.T) {
	m := models.ContextMemory{Content: "  ", Kind: "", Confidence: 1.5, Tags: strings.Repeat("x", 513)}
	v := ValidateMemory(m)
	if Valid(v) {
		t.Fatalf("invalid memory reported no violations")
	}
	fields := map[string]bool{}
	for _, viol := range v {
		fields[viol.Field] = true
	}
	for _, want := range []string{"content", "kind", "confidence", "tags"} {
		if !fields[want] {
			t.Fatalf("missing violation for %q; got %+v", want, v)
		}
	}
}

func TestConfidenceBoundsAreInclusive(t *testing.T) {
	for _, c := range []float64{0, 1} {
		m := models.ContextMemory{Content: "x", Kind: "k", Confidence: c}
		if !Valid(ValidateMemory(m)) {
			t.Fatalf("confidence %v should be valid (inclusive bounds)", c)
		}
	}
}
