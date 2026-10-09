// Package autonomygate decides whether an action can run automatically, needs
// human review, or must be blocked — minimizing human decisions for safe cases
// while never auto-running unapproved risky or irreversible ones. Pure and deterministic.
package autonomygate

import "strings"

// Decision is the gate's verdict.
type Decision string

const (
	Auto   Decision = "auto"   // safe to run without a human
	Review Decision = "review" // route to a human review queue
	Block  Decision = "block"  // do not run
)

// Signals are the inputs to the decision.
type Signals struct {
	Confidence float64 // [0,1]
	Risk       string  // "low" | "medium" | "high"
	Reversible bool
	Approved   bool // an explicit human approval already exists
}

// Decide returns the autonomy verdict.
//
//   - An explicit approval always permits Auto.
//   - High risk that is irreversible is Blocked without approval.
//   - Medium/high risk or low confidence needs Review.
//   - Low risk with high confidence and reversibility runs Auto.
//   - Invalid risk or confidence inputs never run Auto.
func Decide(s Signals) Decision {
	if s.Approved {
		return Auto
	}
	risk := strings.ToLower(strings.TrimSpace(s.Risk))

	if risk == "high" && !s.Reversible {
		return Block
	}
	if risk != "low" ||
		!s.Reversible ||
		!(s.Confidence >= 0 && s.Confidence <= 1) ||
		s.Confidence < 0.6 {
		return Review
	}
	return Auto
}
