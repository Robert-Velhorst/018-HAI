package autonomypolicy

import (
	"testing"
	"time"

	"automation-hub-backend/internal/operations"
	"automation-hub-backend/internal/privacyfilter"
)

var now = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

func TestClassifyRiskTaxonomy(t *testing.T) {
	if ClassifyRisk("Letter from the municipality about taxes") != operations.RiskHigh {
		t.Fatalf("government/tax must be high risk")
	}
	if ClassifyRisk("Draft a follow-up reply for the Upwork client") != operations.RiskMedium {
		t.Fatalf("draft/upwork must be medium risk")
	}
	if ClassifyRisk("Summarize the local planning notes file") != operations.RiskLow {
		t.Fatalf("summarize must be low risk")
	}
}

func TestLowRiskAutonomousSafeRunsAutomatically(t *testing.T) {
	d := Decide(Input{
		Title: "Summarize local notes", OperationType: "file",
		Privacy: privacyfilter.ScanResult{PrivacyRiskLevel: privacyfilter.RiskNone, SafeForCloudModel: true},
		Mode:    ModeAutonomousSafe, Reversible: true,
	}, now)
	if d.Decision != operations.DecisionRunSafeLocalWorker || d.Autonomy != operations.AutonomyAuto || d.RequiresApproval {
		t.Fatalf("low-risk reversible should auto-run the safe worker: %+v", d)
	}
}

func TestSourceDerivedLowRiskWorkIsHeldForReviewButNotAuthorized(t *testing.T) {
	for _, mode := range []Mode{ModeApprovalRequired, ModeAutonomousSafe} {
		d := Decide(Input{
			Title: "Organize workspace notes", Content: "Summarize this message",
			OperationType: "review_message", SourceDerived: true,
			Mode: mode, Reversible: true,
		}, now)
		if d.Decision != operations.DecisionAskRobert || !d.RequiresApproval || d.Autonomy != operations.AutonomyApproval || d.PolicyRule != "source_derived_owner_review_hold" {
			t.Fatalf("source-derived work in mode %s must remain execution-blocked after review: %+v", mode, d)
		}
	}
	for _, mode := range []Mode{ModeDraftOnly, ModeReadOnly} {
		d := Decide(Input{SourceDerived: true, Mode: mode, Reversible: true}, now)
		if d.Decision == operations.DecisionRunSafeLocalWorker {
			t.Fatalf("source-derived internal-only mode %s authorized a host effect: %+v", mode, d)
		}
	}
}

func TestHighRiskAlwaysRequiresApproval(t *testing.T) {
	for _, mode := range []Mode{ModeAutonomousSafe, ModeApprovalRequired, ModeDraftOnly, ModeReadOnly} {
		d := Decide(Input{
			Title: "Government legal letter about a court hearing", OperationType: "document",
			Mode: mode, Reversible: false,
		}, now)
		if !d.RequiresApproval || d.Decision != operations.DecisionAskRobert || d.Autonomy != operations.AutonomyApproval {
			t.Fatalf("high risk in mode %s must require approval: %+v", mode, d)
		}
		if d.NextReviewAt == nil {
			t.Fatalf("approval decision should set a next review time")
		}
	}
}

func TestEmergencyStopAndPaused(t *testing.T) {
	stop := Decide(Input{Title: "Summarize notes", Mode: ModeAutonomousSafe, Reversible: true, EmergencyStop: true}, now)
	if stop.Decision != operations.DecisionBlock || stop.Autonomy != operations.AutonomyBlocked {
		t.Fatalf("emergency stop must block: %+v", stop)
	}
	paused := Decide(Input{Title: "Summarize notes", Mode: ModePaused, Reversible: true}, now)
	if paused.Decision != operations.DecisionObserveOnly {
		t.Fatalf("paused must observe only: %+v", paused)
	}
}

func TestReadOnlyAndDraftModes(t *testing.T) {
	ro := Decide(Input{Title: "Summarize notes", Mode: ModeReadOnly, Reversible: true}, now)
	if ro.Decision != operations.DecisionObserveOnly {
		t.Fatalf("read-only should observe/summarize: %+v", ro)
	}
	do := Decide(Input{Title: "Prepare notes", OperationType: "file", Mode: ModeDraftOnly, Reversible: true}, now)
	if do.Decision != operations.DecisionCreateDraft || do.Autonomy != operations.AutonomyDraft {
		t.Fatalf("draft-only should create a draft: %+v", do)
	}
}
