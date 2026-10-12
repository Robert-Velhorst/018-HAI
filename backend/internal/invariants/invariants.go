// Package invariants defines pure data-integrity rules for core models. The
// checks perform no I/O so they can validate inputs at the edge and audit
// existing rows during reconciliation with identical logic.
package invariants

import (
	"math"
	"strings"

	"automation-hub-backend/internal/models"
)

// Violation describes a single broken invariant.
type Violation struct {
	Field  string `json:"field"`
	Rule   string `json:"rule"`
	Detail string `json:"detail"`
}

// Valid reports whether a set of violations is empty.
func Valid(violations []Violation) bool { return len(violations) == 0 }

// ValidateMemory returns the invariants a context memory violates, if any.
func ValidateMemory(m models.ContextMemory) []Violation {
	var v []Violation
	if strings.TrimSpace(m.Content) == "" {
		v = append(v, Violation{Field: "content", Rule: "required", Detail: "content must not be empty"})
	}
	if strings.TrimSpace(m.Kind) == "" {
		v = append(v, Violation{Field: "kind", Rule: "required", Detail: "kind must not be empty"})
	}
	if math.IsNaN(m.Confidence) || math.IsInf(m.Confidence, 0) {
		v = append(v, Violation{Field: "confidence", Rule: "finite", Detail: "confidence must be finite"})
	} else if m.Confidence < 0 || m.Confidence > 1 {
		v = append(v, Violation{Field: "confidence", Rule: "range", Detail: "confidence must be within [0,1]"})
	}
	if len(m.Tags) > 512 {
		v = append(v, Violation{Field: "tags", Rule: "max_length", Detail: "joined tags must not exceed 512 bytes"})
	}
	return v
}
