package autonomypolicy

import (
	"time"

	"automation-hub-backend/internal/autonomygate"
	"automation-hub-backend/internal/operations"
	"automation-hub-backend/internal/privacyfilter"
)

// Input is the policy input (§10.13): the operation text, its privacy scan, the
// current mode, and whether the action is reversible.
type Input struct {
	Title         string
	Content       string
	OperationType string
	SourceDerived bool
	Privacy       privacyfilter.ScanResult
	Mode          Mode
	Reversible    bool
	EmergencyStop bool
}

// Decision is the policy output. It maps directly onto Operation fields.
type Decision struct {
	Risk              operations.RiskLevel
	Autonomy          operations.AutonomyLevel
	Decision          operations.CurrentDecision
	RequiresApproval  bool
	Reversible        bool
	Owner             operations.OwnerType
	Reason            string
	PolicyRule        string
	RecommendedAction string
	NextReviewAt      *time.Time
}

// Decide computes the autonomy decision for an operation deterministically.
func Decide(in Input, now time.Time) Decision {
	risk := ClassifyRisk(in.Title + " " + in.Content)

	// Emergency stop / emergency mode: block all execution.
	if in.EmergencyStop || in.Mode == ModeEmergencyStopped {
		return block(risk, "emergency_stop", "emergency stop active; execution blocked")
	}
	// Paused: no background processing — observe only.
	if in.Mode == ModePaused {
		return observe(risk, "mode_paused", "background processing paused")
	}
	// Always-high-risk domains require Robert regardless of mode (§25).
	if risk == operations.RiskHigh {
		return approval(risk, "always_high_risk", "high-risk domain requires Robert's approval", now)
	}
	// Read-only mode: classify/summarize only; no drafts for external use, no execution.
	if in.Mode == ModeReadOnly {
		return observe(risk, "mode_read_only", "read-only mode; observe/summarize only")
	}
	// Draft-only mode: internal drafts allowed, no external execution.
	if in.Mode == ModeDraftOnly {
		return draft(risk, "mode_draft_only", "draft-only mode; internal draft prepared")
	}
	// Imported source content is untrusted. It may inform planning, but cannot
	// authorize even a bounded host-side worker effect. The current approval
	// transition is not bound to a source revision, so review cannot unblock it.
	if in.SourceDerived {
		return approval(risk, "source_derived_owner_review_hold", "source-derived work is held for owner review; current approval does not authorize execution", now)
	}

	lowSafe := risk == operations.RiskLow && in.Reversible

	// Approval-required mode: only internal low-risk reversible work is automatic.
	if in.Mode == ModeApprovalRequired {
		if lowSafe && gateAllowsAuto(risk, in.Reversible) {
			return auto(risk, "mode_approval_required_low_safe", "low-risk reversible internal work runs automatically")
		}
		return approval(risk, "mode_approval_required", "approval required for anything beyond internal low-risk", now)
	}

	// Autonomous-safe mode: low-risk internal reversible runs automatically;
	// medium/high require approval.
	if in.Mode == ModeAutonomousSafe {
		if lowSafe && gateAllowsAuto(risk, in.Reversible) {
			return auto(risk, "mode_autonomous_safe_low", "low-risk reversible operation runs via the safe local worker")
		}
		return approval(risk, "mode_autonomous_safe_medium_high", "medium/high risk requires approval in autonomous-safe mode", now)
	}

	// Default: be conservative.
	return approval(risk, "default_conservative", "no permissive mode matched; requiring approval", now)
}

// gateAllowsAuto cross-checks the Phase-1 autonomy gate: an auto candidate is
// only permitted when the gate also returns Auto.
func gateAllowsAuto(risk operations.RiskLevel, reversible bool) bool {
	d := autonomygate.Decide(autonomygate.Signals{
		Confidence: 0.9,
		Risk:       string(risk),
		Reversible: reversible,
	})
	return d == autonomygate.Auto
}

func auto(risk operations.RiskLevel, rule, reason string) Decision {
	return Decision{
		Risk: risk, Autonomy: operations.AutonomyAuto, Decision: operations.DecisionRunSafeLocalWorker,
		RequiresApproval: false, Reversible: true, Owner: operations.OwnerHAI,
		Reason: reason, PolicyRule: rule, RecommendedAction: "run safe local worker",
	}
}

func approval(risk operations.RiskLevel, rule, reason string, now time.Time) Decision {
	review := now.Add(24 * time.Hour)
	return Decision{
		Risk: risk, Autonomy: operations.AutonomyApproval, Decision: operations.DecisionAskRobert,
		RequiresApproval: true, Reversible: false, Owner: operations.OwnerRobert,
		Reason: reason, PolicyRule: rule, RecommendedAction: "ask Robert to approve", NextReviewAt: &review,
	}
}

func draft(risk operations.RiskLevel, rule, reason string) Decision {
	return Decision{
		Risk: risk, Autonomy: operations.AutonomyDraft, Decision: operations.DecisionCreateDraft,
		RequiresApproval: false, Reversible: true, Owner: operations.OwnerHAI,
		Reason: reason, PolicyRule: rule, RecommendedAction: "prepare an internal draft",
	}
}

func observe(risk operations.RiskLevel, rule, reason string) Decision {
	return Decision{
		Risk: risk, Autonomy: operations.AutonomyObserve, Decision: operations.DecisionObserveOnly,
		RequiresApproval: false, Reversible: true, Owner: operations.OwnerHAI,
		Reason: reason, PolicyRule: rule, RecommendedAction: "observe only",
	}
}

func block(risk operations.RiskLevel, rule, reason string) Decision {
	return Decision{
		Risk: risk, Autonomy: operations.AutonomyBlocked, Decision: operations.DecisionBlock,
		RequiresApproval: false, Reversible: false, Owner: operations.OwnerHAI,
		Reason: reason, PolicyRule: rule, RecommendedAction: "blocked",
	}
}
