package pursuit

import (
	"strings"
	"testing"

	"automation-hub-backend/internal/models"
)

func TestPortfolioAllocationDigestCanonicalizesOnlyAbsentCharPadding(t *testing.T) {
	allocation := &models.PursuitPortfolioAllocation{}
	want, err := digestPortfolioAllocation(allocation)
	if err != nil {
		t.Fatal(err)
	}
	allocation.CoordinationPlanDigest = strings.Repeat(" ", 64)
	got, err := digestPortfolioAllocation(allocation)
	if err != nil || got != want {
		t.Fatalf("padded absence changed digest: %v", err)
	}
	if coordinationReferenceForAllocation(allocation).Digest != "" {
		t.Fatal("absent reference retains database padding")
	}
	if allocation.CoordinationPlanDigest != strings.Repeat(" ", 64) {
		t.Fatal("hashing mutated immutable source evidence")
	}
	for _, value := range []string{strings.Repeat("a", 64), strings.Repeat("b", 64), " " + strings.Repeat("a", 63), "\t"} {
		allocation.CoordinationPlanDigest = value
		if portfolioCoordinationDigest(value) != value || coordinationReferenceForAllocation(allocation).Digest != value {
			t.Fatal("nonempty coordination evidence was rewritten")
		}
		changed, err := digestPortfolioAllocation(allocation)
		if err != nil || changed == want {
			t.Fatalf("nonempty evidence lost digest binding: %v", err)
		}
	}
}
