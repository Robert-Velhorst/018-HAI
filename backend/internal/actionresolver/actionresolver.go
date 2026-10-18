// Package actionresolver classifies an ambiguous external action into a safe
// next step: proceed, ask the user to clarify, or block. Pure and deterministic
// so the resolution of "the AI wasn't sure" is auditable.
package actionresolver

import "math"

// Resolution is the recommended handling of an ambiguous action.
type Resolution string

const (
	Proceed Resolution = "proceed"
	Clarify Resolution = "clarify"
	Block   Resolution = "block"
)

// Action describes a proposed external action and the AI's certainty about it.
type Action struct {
	Description   string
	Confidence    float64 // [0,1]
	Destructive   bool
	MissingParams []string
}

// Resolve returns how to handle the action:
//
//   - Destructive with low confidence → Block (never guess a destructive action).
//   - Missing required parameters, or low confidence → Clarify with the user.
//   - Otherwise → Proceed.
func Resolve(a Action) Resolution {
	validConfidence := !math.IsNaN(a.Confidence) && !math.IsInf(a.Confidence, 0) && a.Confidence >= 0 && a.Confidence <= 1
	lowConfidence := !validConfidence || a.Confidence < 0.6
	if a.Destructive && lowConfidence {
		return Block
	}
	if len(a.MissingParams) > 0 || lowConfidence {
		return Clarify
	}
	return Proceed
}
