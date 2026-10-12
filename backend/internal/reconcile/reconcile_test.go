package reconcile

import (
	"math"
	"reflect"
	"strings"
	"testing"

	"automation-hub-backend/internal/models"
	"github.com/google/uuid"
)

func TestSingleMemoryMatchesBatchScan(t *testing.T) {
	for _, memory := range []models.ContextMemory{
		{ID: uuid.New(), Content: "x", Kind: "preference", Confidence: 0.8},
		{ID: uuid.New(), Content: "x", Kind: "preference", Confidence: 2},
		{ID: uuid.New(), Kind: "", Confidence: 0.5},
		{ID: uuid.New(), Content: "x", Kind: "preference", Confidence: math.NaN()},
	} {
		finding, found := ScanMemory(memory)
		batch := ScanMemories([]models.ContextMemory{memory})
		if batch.Scanned != 1 || found != (len(batch.Findings) == 1) {
			t.Fatal("single/batch finding count differs")
		}
		if found && !reflect.DeepEqual(finding, batch.Findings[0]) {
			t.Fatalf("single/batch finding differs: %+v vs %+v", finding, batch.Findings)
		}
	}
}

func TestNonFiniteConfidenceRequiresManualSourceReview(t *testing.T) {
	for _, confidence := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		memory := models.ContextMemory{ID: uuid.New(), Content: "x", Kind: "preference", Confidence: confidence}
		finding, found := ScanMemory(memory)
		if !found || finding.MemoryID != memory.ID.String() || finding.Repairable ||
			len(finding.Violations) != 1 || finding.Violations[0].Rule != "finite" ||
			!strings.Contains(finding.Repair, "verified source information") {
			t.Fatalf("non-finite confidence falsely accepted or auto-repairable: %+v", finding)
		}
	}
}

func TestScanCleanDataHasNoFindings(t *testing.T) {
	memories := []models.ContextMemory{
		{ID: uuid.New(), Content: "ok", Kind: "preference", Confidence: 0.8},
	}
	r := ScanMemories(memories)
	if !r.Clean() || r.Scanned != 1 {
		t.Fatalf("clean scan expected: %+v", r)
	}
}

func TestScanFlagsAndClassifiesRepairs(t *testing.T) {
	memories := []models.ContextMemory{
		{ID: uuid.New(), Content: "ok", Kind: "preference", Confidence: 2.0}, // repairable (confidence)
		{ID: uuid.New(), Content: "", Kind: "", Confidence: 0.5},             // needs manual input
	}
	r := ScanMemories(memories)
	if len(r.Findings) != 2 {
		t.Fatalf("expected 2 findings, got %d", len(r.Findings))
	}

	var repairable, manual int
	for _, f := range r.Findings {
		if f.Repairable {
			repairable++
		} else {
			manual++
		}
	}
	if repairable != 1 || manual != 1 {
		t.Fatalf("classification wrong: %d repairable, %d manual", repairable, manual)
	}
}
