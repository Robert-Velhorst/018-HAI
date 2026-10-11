package pursuit

import (
	"fmt"
	"testing"
	"time"

	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/workflow"
	"github.com/google/uuid"
)

func TestPursuitCompletionRequiresActualInternalVerification(t *testing.T) {
	for _, status := range []string{"missing", "failed", "needs_review", "verified", "external_label"} {
		t.Run(status, func(t *testing.T) {
			repo := newFakeRepo()
			s := NewService(repo, nil)
			p, err := s.Create(CreateRequest{Title: "Check completion provenance"})
			if err != nil {
				t.Fatal(err)
			}
			runID := uuid.New()
			if status != "missing" {
				repo.verificationRuns[runID] = models.VerificationRun{ID: runID, Status: status}
			}
			linkID := runID.String()
			if status == "external_label" {
				linkID = "external-verification-label"
			}
			if _, err := s.Link(p.ID, LinkRequest{LinkType: LinkVerification, LinkID: linkID, SourceURI: "local://unverified-label", Relationship: "completion_evidence"}); err != nil {
				t.Fatal(err)
			}
			_, err = s.Update(p.ID, UpdateRequest{CompletionState: CompletionVerified, Actor: "operator"})
			if (err == nil) != (status == "verified") {
				t.Fatalf("status %s completion error = %v", status, err)
			}
		})
	}
}

func TestPursuitWorkflowCompletionRejectsContradictoryRecovery(t *testing.T) {
	for _, contradiction := range []string{"recovery", "worker_error", "blocked_reason", "approval", "known_verified"} {
		t.Run(contradiction, func(t *testing.T) {
			repo := newFakeRepo()
			s := NewService(repo, nil)
			p, err := s.Create(CreateRequest{Title: "Check workflow recovery"})
			if err != nil {
				t.Fatal(err)
			}
			item := models.WorkflowItem{ID: uuid.New(), CurrentState: workflow.StateCompleted, VerificationStatus: "verified"}
			switch contradiction {
			case "recovery":
				item.RecoveryStatus = workflow.RecoveryNeedsReview
			case "worker_error":
				item.LastWorkerError = "receipt persistence failed"
			case "blocked_reason":
				item.BlockedReason = "outcome requires reconciliation"
			case "approval":
				item.RequiresApproval = true
				item.ApprovalStatus = "pending"
			}
			repo.workflows[item.ID] = item
			if _, err := s.Link(p.ID, LinkRequest{LinkType: LinkWorkflow, LinkID: item.ID.String()}); err != nil {
				t.Fatal(err)
			}
			detail, err := s.Detail(p.ID)
			if err != nil {
				t.Fatal(err)
			}
			accepted := contradiction == "known_verified"
			if detail.Summary.CompletionCandidate != accepted || workflowsReadyForCompletion([]models.WorkflowItem{item}) != accepted || acceptedWorkflowCompletionEvidence([]models.WorkflowItem{item}) != boolCount(accepted) {
				t.Fatalf("contradictory completion state = %#v", detail.Summary)
			}
			_, err = s.Update(p.ID, UpdateRequest{CompletionState: CompletionVerified, Actor: "operator"})
			if (err == nil) != accepted {
				t.Fatalf("completion error = %v, accepted = %v", err, accepted)
			}
		})
	}
}

func TestPursuitRuntimeUncertainStatusRequiresReview(t *testing.T) {
	for _, status := range []string{"", "unknown", "indeterminate", "new_provider_status", "failed", "completed", "pending", "running"} {
		attempt := models.AutomationLaunchEvent{Status: status}
		want := status != "completed" && status != "pending" && status != "running"
		if got := runtimeAttemptNeedsReview(attempt); got != want {
			t.Errorf("status %q review = %v, want %v", status, got, want)
		}
	}
	for _, attempt := range []models.AutomationLaunchEvent{{Status: "completed", RequiresApproval: true}, {Status: "completed", ExitCode: 1}} {
		if !runtimeAttemptNeedsReview(attempt) || completedRuntimeAttempts([]models.AutomationLaunchEvent{attempt}) != 0 {
			t.Fatalf("contradictory runtime counted as completed: %#v", attempt)
		}
	}
}

func TestPursuitRecoveryRequiresVerifiedUnblockedWorkflow(t *testing.T) {
	attempt := models.AutomationLaunchEvent{ID: uuid.New()}
	item := models.WorkflowItem{CurrentState: workflow.StateCompleted, SourceURI: "automation-launch://" + attempt.ID.String(), RecoveryStatus: workflow.RecoveryCompletionConfirmed}
	if runtimeAttemptRecoveredByWorkflow(attempt, []models.WorkflowItem{item}) {
		t.Fatal("recovery label substituted for verified evidence")
	}
	item.VerificationStatus = "verified"
	if !runtimeAttemptRecoveredByWorkflow(attempt, []models.WorkflowItem{item}) {
		t.Fatal("known verified recovery not accepted")
	}
	item.RecoveryStatus = workflow.RecoveryNeedsReview
	if runtimeAttemptRecoveredByWorkflow(attempt, []models.WorkflowItem{item}) {
		t.Fatal("uncertain recovery suppressed runtime blocker")
	}
}

func TestPursuitTaskActivityDoesNotAnnounceContradictorySuccess(t *testing.T) {
	now := time.Now().UTC()
	for _, attempt := range []models.PursuitTaskAttempt{{Status: "validated", BlockedReason: "unknown outcome", CompletedAt: &now}, {Status: "validated", VerificationStatus: "needs_review", CompletedAt: &now}, {Status: "review_required"}} {
		kind, _ := pursuitTaskAttemptActivity(attempt)
		if kind != "pursuit.task_attempt_review_required" {
			t.Fatalf("uncertain activity = %s", kind)
		}
	}
}

func TestPursuitOldDirectUncertaintyCannotDisappearBehindDisplayLimit(t *testing.T) {
	repo := newFakeRepo()
	s := NewService(repo, nil)
	p, err := s.Create(CreateRequest{Title: "Keep unresolved work visible", OwnerIdentity: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	item := models.WorkflowItem{ID: uuid.New(), CurrentState: workflow.StateCompleted, VerificationStatus: "verified"}
	repo.workflows[item.ID] = item
	if _, err := s.Link(p.ID, LinkRequest{LinkType: LinkWorkflow, LinkID: item.ID.String()}); err != nil {
		t.Fatal(err)
	}
	for n := 0; n < 30; n++ {
		id := fmt.Sprintf("recent-%d", n)
		repo.taskAttempts[id] = models.PursuitTaskAttempt{PursuitID: p.ID, TaskPlanID: id, OwnerIdentity: "alice", Status: "validated", VerificationStatus: "verified", UpdatedAt: time.Now().UTC()}
	}
	repo.taskAttempts["old-uncertainty"] = models.PursuitTaskAttempt{PursuitID: p.ID, TaskPlanID: "old-uncertainty", OwnerIdentity: "alice", Status: "validated", VerificationStatus: "uncertain", UpdatedAt: time.Now().UTC().Add(-time.Hour)}
	detail, err := s.DetailForOwner("alice", p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Summary.CompletionCandidate || len(detail.Blockers) == 0 || len(detail.TaskAttempts) != 21 {
		t.Fatalf("old unresolved task hidden: %#v", detail.Summary)
	}
	if _, err := s.Update(p.ID, UpdateRequest{CompletionState: CompletionVerified, Actor: "alice"}); err == nil {
		t.Fatal("old task uncertainty bypassed completion guard")
	}
	delete(repo.taskAttempts, "old-uncertainty")
	repo.taskAttempts["bob-uncertainty"] = models.PursuitTaskAttempt{PursuitID: p.ID, TaskPlanID: "bob-uncertainty", OwnerIdentity: "bob", Status: "review_required"}
	detail, err = s.DetailForOwner("alice", p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !detail.Summary.CompletionCandidate || len(detail.Blockers) != 0 {
		t.Fatal("foreign-owned task leaked or blocked Alice's project")
	}
}

func TestPursuitRejectedApprovalBlocksIndependentCompletionEvidence(t *testing.T) {
	for _, status := range []string{"rejected", "revoked", "expired", "new_status"} {
		t.Run(status, func(t *testing.T) {
			repo := newFakeRepo()
			s := NewService(repo, nil)
			p, err := s.Create(CreateRequest{Title: "Preserve approval refusal"})
			if err != nil {
				t.Fatal(err)
			}
			item := models.WorkflowItem{ID: uuid.New(), CurrentState: workflow.StateCompleted, LastTaskPlanID: uuid.NewString(), VerificationStatus: "verified", RequiresApproval: true, ApprovalStatus: status}
			repo.workflows[item.ID] = item
			if _, err := s.Link(p.ID, LinkRequest{LinkType: LinkWorkflow, LinkID: item.ID.String()}); err != nil {
				t.Fatal(err)
			}
			runID := uuid.New()
			repo.verificationRuns[runID] = models.VerificationRun{ID: runID, Status: "verified"}
			if _, err := s.Link(p.ID, LinkRequest{LinkType: LinkVerification, LinkID: runID.String()}); err != nil {
				t.Fatal(err)
			}
			if _, err := s.Update(p.ID, UpdateRequest{CompletionState: CompletionVerified, Actor: "operator"}); err == nil {
				t.Fatal("independent evidence bypassed required approval")
			}
			if taskRunStatus(item) == "completed" || !taskRunNeedsReview(item) {
				t.Fatal("task-run projection advertised unapproved completion")
			}
		})
	}
}

func TestPursuitCompletedReceiptUsesLaunchSpecificCodes(t *testing.T) {
	for _, test := range []struct {
		kind     string
		code     int
		accepted bool
	}{{"api", 200, true}, {"api", 204, true}, {"api", 404, true}, {"api", 0, false}, {"docker_service", 204, true}, {"docker_service", 304, true}, {"docker_service", 200, false}, {"script", 0, true}, {"script", 1, false}} {
		t.Run(fmt.Sprintf("%s-%d", test.kind, test.code), func(t *testing.T) {
			intent := models.AutomationLaunchEvent{ID: uuid.New(), Status: "pending", LaunchType: test.kind + "_intent"}
			outcome := models.AutomationLaunchEvent{ID: uuid.New(), Status: "completed", LaunchType: test.kind, ExitCode: test.code}
			attempts := []models.AutomationLaunchEvent{intent, outcome}
			if runtimeAttemptNeedsReview(outcome) == test.accepted || completedRuntimeAttempts(attempts) != boolCount(test.accepted) || (len(runtimeAttemptBlockers(attempts, nil, nil)) == 0) != test.accepted {
				t.Fatalf("completed receipt code misclassified: %#v", outcome)
			}
			outcome.Status = "failed"
			if !runtimeAttemptNeedsReview(outcome) {
				t.Fatal("HTTP code promoted failed receipt")
			}
		})
	}
}

func TestPursuitTaskRunRequiresAcceptedCompletedVerification(t *testing.T) {
	for _, status := range []string{"", "unknown", "uncertain", "conflicting", "verified", "test_passed"} {
		item := models.WorkflowItem{CurrentState: workflow.StateCompleted, VerificationStatus: status}
		accepted := status == "verified" || status == "test_passed"
		if (taskRunStatus(item) == "completed") != accepted || taskRunNeedsReview(item) == accepted {
			t.Errorf("task-run status %q reported as accepted=%v", status, accepted)
		}
	}
}

func boolCount(value bool) int {
	if value {
		return 1
	}
	return 0
}

func TestPursuitAllExplicitRuntimeReceiptsParticipateInCompletion(t *testing.T) {
	repo := newFakeRepo()
	s := NewService(repo, nil)
	p, err := s.Create(CreateRequest{Title: "Review every exact runtime receipt"})
	if err != nil {
		t.Fatal(err)
	}
	item := models.WorkflowItem{ID: uuid.New(), CurrentState: workflow.StateCompleted, VerificationStatus: "verified"}
	repo.workflows[item.ID] = item
	if _, err := s.Link(p.ID, LinkRequest{LinkType: LinkWorkflow, LinkID: item.ID.String()}); err != nil {
		t.Fatal(err)
	}
	for n := 0; n < 51; n++ {
		launch := models.AutomationLaunchEvent{ID: uuid.New(), Status: "completed", StartedAt: time.Now().UTC().Add(time.Duration(-n) * time.Minute)}
		if n == 50 {
			launch.Status = "indeterminate"
		}
		repo.launchEvents = append(repo.launchEvents, launch)
		if _, err := s.Link(p.ID, LinkRequest{LinkType: LinkAgentRuntime, LinkID: launch.ID.String(), Relationship: "execution_attempt"}); err != nil {
			t.Fatal(err)
		}
	}
	detail, err := s.Detail(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Summary.CompletionCandidate || len(detail.RuntimeAttempts) != 51 || len(detail.Blockers) != 1 {
		t.Fatalf("explicit historical uncertainty disappeared: %#v", detail.Summary)
	}
	if _, err := s.Update(p.ID, UpdateRequest{CompletionState: CompletionVerified, Actor: "operator"}); err == nil {
		t.Fatal("runtime display cap bypassed completion guard")
	}
	repo.launchEvents[50].Status = "completed"
	if _, err := s.Update(p.ID, UpdateRequest{CompletionState: CompletionVerified, Actor: "operator"}); err != nil {
		t.Fatalf("known reconciled completion refused: %v", err)
	}
}

func TestPursuitTerminalDirectRunVerificationParity(t *testing.T) {
	for _, status := range []string{"", "unknown", "provider_new", "\tuncertain\n", "\u00a0uncertain\u3000", "verified", "schema_validated"} {
		attempt := models.PursuitTaskAttempt{Mode: "run", Status: "validated", VerificationStatus: status}
		want := status != "verified" && status != "schema_validated"
		if pursuitTaskAttemptNeedsReview(attempt) != want {
			t.Errorf("terminal run %q needs review = %v", status, want)
		}
		attempt.Mode = "plan"
		if status == "" || status == "unknown" || status == "provider_new" {
			if pursuitTaskAttemptNeedsReview(attempt) {
				t.Error("plan-only validation promoted to failed run")
			}
		}
	}
}
