package workflow

import "testing"

// Calculated from the retained pre-success-criteria field layout, including the
// empty automation-selection fingerprint, not from the current revision helper.
const preCriteriaReviewRevision = "d6d169f25e9ffe947e2f83d695df4d5cfe9f4ed4dd9c91422d72a014f7ac878f"

func preCriteriaReviewRequest() IntakeRequest {
	return IntakeRequest{
		OwnerIdentity: "alice", Input: "Review retained evidence.", ProjectKey: "018-HAI",
		SourceType: "email", SourceID: "legacy-no-criteria",
	}
}

func TestIntakeReplaysPreCriteriaSourceRevisionWithoutReplacingExistingWork(t *testing.T) {
	for _, state := range []string{StateNeedsApproval, StateReady, StateInProgress} {
		t.Run(state, func(t *testing.T) {
			repo := newFakeWorkflowRepo()
			engine := NewService(repo)
			request := preCriteriaReviewRequest()
			first, err := engine.Intake(request)
			if err != nil {
				t.Fatal(err)
			}
			if first.Item.SourceRevision == preCriteriaReviewRevision {
				t.Fatal("fixture must distinguish current and pre-criteria layouts")
			}
			repo.items[first.Item.ID].SourceRevision = preCriteriaReviewRevision
			repo.items[first.Item.ID].CurrentState = state
			for range 2 {
				replay, err := engine.Intake(request)
				if err != nil || replay == nil || replay.Item.ID != first.Item.ID || replay.Item.Archived || replay.Item.CurrentState != state {
					t.Fatalf("unchanged pre-criteria intake did not preserve the workflow in state %s: %v", state, err)
				}
				if len(repo.items) != 1 || replay.Item.SourceRevision != preCriteriaReviewRevision {
					t.Fatal("replay created new work or rewrote the historical revision")
				}
			}
		})
	}
}

func TestPreCriteriaReplayNeverIgnoresChangedCriteriaOrOwner(t *testing.T) {
	for _, mode := range []string{"new_criteria", "stored_criteria", "different_owner"} {
		t.Run(mode, func(t *testing.T) {
			repo := newFakeWorkflowRepo()
			engine := NewService(repo)
			request := preCriteriaReviewRequest()
			first, err := engine.Intake(request)
			if err != nil {
				t.Fatal(err)
			}
			repo.items[first.Item.ID].SourceRevision = preCriteriaReviewRevision
			switch mode {
			case "new_criteria":
				request.SuccessCriteria = []string{"Keep this as a draft"}
			case "stored_criteria":
				repo.items[first.Item.ID].SuccessCriteria = []string{"Do not drop this condition"}
			case "different_owner":
				request.OwnerIdentity = "bob"
			}
			changed, err := engine.Intake(request)
			if err != nil || changed == nil || changed.Item.ID == first.Item.ID {
				t.Fatalf("changed criteria or owner reused legacy work: %v", err)
			}
		})
	}
}

func TestLegacyWorkflowRevisionCompatibilityRejectsAmbiguousFields(t *testing.T) {
	request := preCriteriaReviewRequest()
	selection := `{"Required":false,"Reason":"","Candidates":[]}`
	if got := workflowSourceRevisionForLayout(request, request.Input, false, selection); got != preCriteriaReviewRevision {
		t.Fatalf("pre-criteria layout differs from the retained reference: %s", got)
	}
	for _, label := range []string{"one\x1ftwo", "one\x1etwo", "one\x1dtwo", "~hai-revision:6162"} {
		request.SourceLabel = label
		if workflowSourceRevisionForLayout(request, request.Input, false, selection) != "" {
			t.Fatal("ambiguous legacy fields must require normal revision review")
		}
	}
	request.SourceLabel = ""
	request.SuccessCriteria = []string{"Keep a draft"}
	if workflowSourceRevisionForLayout(request, request.Input, false, selection) != "" {
		t.Fatal("legacy layout discarded requested success criteria")
	}
}
