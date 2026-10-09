// Package reconcile scans stored records for broken invariants and proposes
// safe repairs. The scan is pure (no I/O): callers load records, run Scan, and
// decide whether to apply the proposed repairs.
package reconcile

import (
	"fmt"

	"automation-hub-backend/internal/invariants"
	"automation-hub-backend/internal/models"
)

// Finding is one record that violates an invariant, with a proposed repair.
type Finding struct {
	MemoryID   string                 `json:"memoryId"`
	Violations []invariants.Violation `json:"violations"`
	Repair     string                 `json:"repair"`
	Repairable bool                   `json:"repairable"`
}

// Report summarizes a reconciliation scan.
type Report struct {
	Scanned  int       `json:"scanned"`
	Findings []Finding `json:"findings"`
}

// Clean reports whether the scan found no violations.
func (r Report) Clean() bool { return len(r.Findings) == 0 }

// ScanMemories checks each memory against its invariants and proposes a repair
// where one is safe to derive automatically.
func ScanMemories(memories []models.ContextMemory) Report {
	report := Report{Scanned: len(memories)}
	for _, m := range memories {
		if finding, found := ScanMemory(m); found {
			report.Findings = append(report.Findings, finding)
		}
	}
	return report
}

// ScanMemory shares the same integrity rules with streaming callers, which
// need not retain an entire corpus or its findings in memory.
func ScanMemory(m models.ContextMemory) (Finding, bool) {
	v := invariants.ValidateMemory(m)
	if invariants.Valid(v) {
		return Finding{}, false
	}
	return Finding{
		MemoryID: m.ID.String(), Violations: v,
		Repair: proposeRepair(m, v), Repairable: repairable(v),
	}, true
}

// repairable reports whether every violation has a safe automatic repair.
// Only out-of-range confidence and over-long tags can be safely auto-repaired;
// missing required content/kind need human input.
func repairable(violations []invariants.Violation) bool {
	for _, viol := range violations {
		switch viol.Field {
		case "confidence":
			if viol.Rule != "range" {
				return false
			}
		case "tags":
			continue
		default:
			return false
		}
	}
	return true
}

func proposeRepair(m models.ContextMemory, violations []invariants.Violation) string {
	for _, violation := range violations {
		if violation.Field == "confidence" && violation.Rule == "finite" {
			return "requires manual input: replace non-finite confidence using verified source information"
		}
	}
	if repairable(violations) {
		return fmt.Sprintf("clamp confidence to [0,1] and/or truncate tags to 512 bytes without splitting UTF-8 for memory %s", m.ID)
	}
	return "requires manual input: fill missing required fields (content/kind)"
}
