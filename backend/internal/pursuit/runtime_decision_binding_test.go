package pursuit

import (
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/models"
	"github.com/google/uuid"
)

func TestRuntimeDecisionRequiresMatchingExplicitReferences(t *testing.T) {
	id, other := uuid.New(), uuid.New()
	decision := "runtime:" + id.String() + ":review"
	evidence := "automation-launch://" + id.String()
	for _, tc := range []struct {
		name, decision, evidence string
		want                     bool
	}{
		{"matching", decision, evidence, true},
		{"missing both", "", "", false},
		{"missing decision", "", evidence, false},
		{"missing evidence", decision, "", false},
		{"invalid decision", "runtime:invalid:review", evidence, false},
		{"invalid evidence", decision, "https://example.invalid", false},
		{"foreign decision", "runtime:" + other.String() + ":review", evidence, false},
		{"foreign evidence", decision, "automation-launch://" + other.String(), false},
		{"both foreign", "runtime:" + other.String() + ":review", "automation-launch://" + other.String(), false},
		{"zero reference", "runtime:" + uuid.Nil.String() + ":review", "automation-launch://" + uuid.Nil.String(), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := runtimeAttemptForDecision([]models.AutomationLaunchEvent{{ID: id}}, DecisionResolutionRequest{DecisionID: tc.decision, EvidenceURI: tc.evidence})
			if ok != tc.want || (ok && got.ID != id) {
				t.Fatalf("selected = %s, %t; want matching attempt = %t", got.ID, ok, tc.want)
			}
		})
	}
	if _, ok := runtimeAttemptForDecision([]models.AutomationLaunchEvent{{ID: uuid.Nil}}, DecisionResolutionRequest{DecisionID: "runtime:" + uuid.Nil.String() + ":review", EvidenceURI: "automation-launch://" + uuid.Nil.String()}); ok {
		t.Fatal("zero-ID attempt must not be selectable")
	}
	got, ok := runtimeAttemptForDecision([]models.AutomationLaunchEvent{{ID: other}, {ID: id}}, DecisionResolutionRequest{DecisionID: decision, EvidenceURI: evidence})
	if !ok || got.ID != id {
		t.Fatal("selection must use matching identity rather than list order")
	}
}

func TestRuntimeDecisionRejectsUnboundOrNonPendingResolutionWithoutEffects(t *testing.T) {
	for _, approved := range []bool{false, true} {
		for _, mode := range []string{"missing evidence", "foreign evidence", "foreign decision", "running attempt", "wrong decision type"} {
			t.Run(mode+map[bool]string{false: "/decline", true: "/approve"}[approved], func(t *testing.T) {
				repo := newFakeRepo()
				intake := &fakeWorkflowIntake{repo: repo}
				svc := NewService(repo, intake)
				created, err := svc.Create(CreateRequest{Title: "Review a bound runtime attempt", OwnerIdentity: "alice"})
				if err != nil {
					t.Fatal(err)
				}
				launchID := uuid.New()
				status := "blocked"
				if mode == "running attempt" {
					status = "running"
				}
				repo.launchEvents = append(repo.launchEvents, models.AutomationLaunchEvent{ID: launchID, OwnerIdentity: "alice", Status: status, LaunchType: "agent_runtime", RuntimeType: "openclaw", StartedAt: time.Now().UTC()})
				_, err = svc.Link(created.ID, LinkRequest{LinkType: LinkAgentRuntime, LinkID: launchID.String(), Relationship: "execution_attempt", SourceURI: "automation-launch://" + launchID.String()})
				if err != nil {
					t.Fatal(err)
				}
				before, err := svc.DetailForOwner("alice", created.ID)
				if err != nil {
					t.Fatal(err)
				}
				if len(before.RuntimeAttempts) != 1 {
					t.Fatal("fixture must expose the owner's linked runtime attempt")
				}
				request := DecisionResolutionRequest{DecisionID: "runtime:" + launchID.String() + ":review", DecisionType: "runtime_attempt_review", EvidenceURI: "automation-launch://" + launchID.String(), Approved: approved, Actor: "Robert"}
				switch mode {
				case "missing evidence":
					request.EvidenceURI = ""
				case "foreign evidence":
					request.EvidenceURI = "automation-launch://" + uuid.NewString()
				case "foreign decision":
					request.DecisionID = "runtime:" + uuid.NewString() + ":review"
				case "wrong decision type":
					request.DecisionType = "unrecognized"
				}
				if _, err := svc.ResolveDecisionForOwner("alice", created.ID, request); err == nil || !strings.Contains(err.Error(), "runtime") {
					t.Fatalf("want runtime binding rejection, got %v", err)
				}
				after, err := svc.DetailForOwner("alice", created.ID)
				if err != nil {
					t.Fatal(err)
				}
				if intake.calls != 0 || len(after.Workflows) != len(before.Workflows) || len(after.Activity) != len(before.Activity) || len(after.Links) != len(before.Links) {
					t.Fatal("rejected decision changed workflows, activity, or links")
				}
			})
		}
	}
}

func TestRuntimeDecisionMatchingOwnerScopedReplayIsIdempotent(t *testing.T) {
	for _, approved := range []bool{false, true} {
		t.Run(map[bool]string{false: "decline", true: "approve"}[approved], func(t *testing.T) {
			repo := newFakeRepo()
			intake := &fakeWorkflowIntake{repo: repo}
			svc := NewService(repo, intake)
			created, err := svc.Create(CreateRequest{Title: "Safely review runtime recovery", OwnerIdentity: "alice"})
			if err != nil {
				t.Fatal(err)
			}
			id := uuid.New()
			repo.launchEvents = append(repo.launchEvents, models.AutomationLaunchEvent{ID: id, OwnerIdentity: "alice", Status: "blocked", LaunchType: "agent_runtime", RuntimeType: "openclaw", StartedAt: time.Now().UTC()})
			if _, err := svc.Link(created.ID, LinkRequest{LinkType: LinkAgentRuntime, LinkID: id.String(), Relationship: "execution_attempt", SourceURI: "automation-launch://" + id.String()}); err != nil {
				t.Fatal(err)
			}
			request := DecisionResolutionRequest{DecisionID: "runtime:" + id.String() + ":review", DecisionType: "runtime_attempt_review", EvidenceURI: "automation-launch://" + id.String(), Approved: approved, Actor: "Robert"}
			first, err := svc.ResolveDecisionForOwner("alice", created.ID, request)
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			if approved {
				calls = 1
			}
			request.DecisionID = " RUNTIME:" + strings.ToUpper(id.String()) + ":REVIEW "
			request.DecisionType = " RUNTIME_ATTEMPT_REVIEW "
			request.EvidenceURI = " automation-launch://" + strings.ToUpper(id.String()) + "/ "
			second, err := svc.ResolveDecisionForOwner("alice", created.ID, request)
			if err != nil || intake.calls != calls || len(second.Activity) != len(first.Activity) || len(second.Workflows) != calls {
				t.Fatalf("valid replay must preserve activity and workflow counts: err=%v calls=%d", err, intake.calls)
			}
			request.EvidenceURI = "automation-launch://" + uuid.NewString()
			if _, err := svc.ResolveDecisionForOwner("alice", created.ID, request); err == nil {
				t.Fatal("resolved decision must not bypass reference binding on replay")
			}
		})
	}
}

func TestRuntimeDecisionStoresCanonicalResolutionReferences(t *testing.T) {
	repo := newFakeRepo()
	svc := NewService(repo, nil)
	created, err := svc.Create(CreateRequest{Title: "Canonical runtime review", OwnerIdentity: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	id := uuid.New()
	repo.launchEvents = append(repo.launchEvents, models.AutomationLaunchEvent{ID: id, OwnerIdentity: "alice", Status: "blocked", LaunchType: "agent_runtime", StartedAt: time.Now().UTC()})
	if _, err := svc.Link(created.ID, LinkRequest{LinkType: LinkAgentRuntime, LinkID: id.String(), Relationship: "execution_attempt", SourceURI: "automation-launch://" + id.String()}); err != nil {
		t.Fatal(err)
	}
	detail, err := svc.ResolveDecisionForOwner("alice", created.ID, DecisionResolutionRequest{DecisionID: " RUNTIME:" + strings.ToUpper(id.String()) + ":REVIEW ", DecisionType: " RUNTIME_ATTEMPT_REVIEW ", EvidenceURI: " automation-launch://" + strings.ToUpper(id.String()) + "/ ", Actor: "Robert"})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, activity := range detail.Activity {
		if activity.EventType == "pursuit.decision_resolved" {
			found = true
			if activity.SourceID != runtimeAttemptDecisionID(models.AutomationLaunchEvent{ID: id}) || activity.SourceType != "pursuit_decision:runtime_attempt_review:rejected" || activity.SourceURI != "automation-launch://"+id.String() {
				t.Fatalf("resolution references are not canonical: %#v", activity)
			}
		}
	}
	if !found || len(detail.DecisionQueue) != 0 {
		t.Fatal("canonical resolution must suppress the matching pending runtime review")
	}
}
