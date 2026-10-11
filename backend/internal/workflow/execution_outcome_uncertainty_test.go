package workflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/models"
)

func TestTaskRunResultExecutionOutcomeUncertainJSON(t *testing.T) {
	var result TaskRunResult
	if err := json.Unmarshal([]byte(`{"executionOutcomeUncertain":true,"passed":true}`), &result); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if !result.ExecutionOutcomeUncertain || !result.Passed {
		t.Fatalf("uncertainty was lost at the result boundary: %#v", result)
	}
	encoded, err := json.Marshal(result)
	if err != nil || !strings.Contains(string(encoded), `"executionOutcomeUncertain":true`) {
		t.Fatalf("encode result = (%s, %v), want the explicit uncertainty field", encoded, err)
	}
	var legacy TaskRunResult
	if err := json.Unmarshal([]byte(`{"passed":true}`), &legacy); err != nil || legacy.ExecutionOutcomeUncertain {
		t.Fatalf("legacy result = (%#v, %v), want uncertainty false by default", legacy, err)
	}
}

func TestSafeNoSideEffectMarkerRejectsAllJoinedErrors(t *testing.T) {
	safe := MarkTaskFailureSafeNoSideEffect(errors.New("preflight denied"))
	if !IsTaskFailureSafeNoSideEffect(fmt.Errorf("context: %w", safe)) {
		t.Fatal("ordinary wrapping lost trusted preflight evidence")
	}
	for _, err := range []error{
		nil, errors.New("unknown"), errors.Join(safe, errors.New("dispatch missing")),
		fmt.Errorf("outer: %w", errors.Join(safe, errors.New("unknown"))),
		MarkTaskFailureSafeNoSideEffect(errors.Join(errors.New("preflight"), errors.New("unknown"))),
		fmt.Errorf("outer: %w", MarkTaskFailureSafeNoSideEffect(errors.Join(safe, errors.New("unknown")))),
	} {
		if IsTaskFailureSafeNoSideEffect(err) {
			t.Fatalf("ambiguous joined error admitted as no-effect: %v", err)
		}
	}
}

func TestRunDueJoinedRunnerFailureCannotBecomeSafeRetry(t *testing.T) {
	safe := MarkTaskFailureSafeNoSideEffect(errors.New("preflight denied"))
	for _, failure := range []error{
		errors.Join(safe, errors.New("unknown runtime outcome")),
		MarkTaskFailureSafeNoSideEffect(errors.Join(errors.New("preflight"), errors.New("unknown runtime outcome"))),
		fmt.Errorf("wrapped: %w", MarkTaskFailureSafeNoSideEffect(errors.Join(safe, errors.New("unknown runtime outcome")))),
	} {
		repo := newFakeWorkflowRepo()
		runner := &fakeTaskRunner{err: failure}
		engine := NewServiceWithTaskRunner(repo, runner)
		record, err := engine.Intake(IntakeRequest{Input: "Create Trello checklist for low risk admin work"})
		if err != nil {
			t.Fatal(err)
		}
		summary, err := engine.RunDue(RunDueRequest{Limit: 1})
		if err != nil || summary.Blocked != 1 || summary.Retried != 0 || summary.Completed != 0 {
			t.Fatalf("mixed failure scheduled retry: summary=%#v err=%v", summary, err)
		}
		updated, err := engine.Get(record.Item.ID)
		if err != nil || updated.Item.RecoveryStatus != RecoveryNeedsReview || updated.Item.NextRunAt != nil {
			t.Fatalf("mixed failure lacks interrupted recovery fence: record=%#v err=%v", updated, err)
		}
		if _, err := engine.RunDue(RunDueRequest{Limit: 1}); err != nil || len(runner.requests) != 1 {
			t.Fatalf("mixed failure was dispatched again: err=%v calls=%d", err, len(runner.requests))
		}
	}
}

func TestRunDueExplicitOutcomeUncertaintyRequiresInterruptedResolution(t *testing.T) {
	tests := []struct {
		name     string
		status   string
		passed   bool
		review   bool
		approval bool
		executed bool
		noLaunch bool
		failure  string
		err      error
	}{
		{name: "nil error and no other flags"},
		{name: "passed completed", status: "completed", passed: true},
		{name: "passed validated", status: "validated", passed: true},
		{name: "passed validated without launch evidence", status: "validated", passed: true, noLaunch: true},
		{name: "ordinary review", status: "review_required", review: true},
		{name: "approval only", status: "approval_required", approval: true},
		{name: "passed with approval", status: "completed", passed: true, approval: true},
		{name: "passed with review and approval", status: "completed", passed: true, review: true, approval: true},
		{name: "safe error", err: MarkTaskFailureSafeNoSideEffect(errors.New("preflight token=synthetic-runner-secret"))},
		{name: "wrapped safe error", passed: true, err: fmt.Errorf("adapter: %w", MarkTaskFailureSafeNoSideEffect(errors.New("preflight token=synthetic-runner-secret")))},
		{name: "unknown error", err: errors.New("dispatch token=synthetic-runner-secret")},
		{name: "sanitized failure", failure: "dispatch receipt missing password=synthetic-result-secret"},
		{name: "external action", status: "completed", passed: true, executed: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repo := newFakeWorkflowRepo()
			runner := &fakeTaskRunner{result: &TaskRunResult{
				PlanID:                    "explicit-uncertainty-plan",
				CompletionStatus:          test.status,
				VerificationStatus:        "verified",
				Passed:                    test.passed,
				ReviewRequired:            test.review,
				ApprovalRequired:          test.approval,
				ExternalActionExecuted:    test.executed,
				ExecutionOutcomeUncertain: true,
				FailureReason:             test.failure,
				Output:                    "private-output-must-not-be-retained",
				RuntimeEvidenceURI:        "automation-launch://11111111-1111-1111-1111-111111111111",
				RuntimeEvidenceLabel:      "Uncertain launch receipt",
				RuntimeRouteTrace: &models.AutomationRuntimeRouteTrace{
					RuntimeID: "uncertain-runtime", Intent: "administration", ExecutionMode: "controlled",
				},
			}, err: test.err}
			if test.noLaunch {
				runner.result.RuntimeEvidenceURI = ""
			}
			service := NewServiceWithTaskRunner(repo, runner)
			record, err := service.Intake(IntakeRequest{Input: "Create Trello checklist for low risk admin work"})
			if err != nil {
				t.Fatalf("Intake: %v", err)
			}

			summary, err := service.RunDue(RunDueRequest{Limit: 1})
			if err != nil {
				t.Fatalf("RunDue: %v", err)
			}
			if summary.Blocked != 1 || summary.Completed != 0 || summary.Retried != 0 || len(summary.Results) != 1 || len(runner.requests) != 1 {
				t.Fatalf("uncertain task must block after exactly one dispatch: summary=%#v calls=%d", summary, len(runner.requests))
			}
			result := summary.Results[0]
			if result.State != StateBlocked || !result.ReviewRequired || result.VerificationStatus != "needs_review" || result.FrameworkSelection == nil {
				t.Fatalf("uncertain result did not expose review and framework provenance: %#v", result)
			}
			updated, err := service.Get(record.Item.ID)
			if err != nil {
				t.Fatalf("Get: %v", err)
			}
			item := updated.Item
			if item.CurrentState != StateBlocked || item.RecoveryStatus != RecoveryNeedsReview || item.NextRunAt != nil ||
				item.CompletedAt != nil || item.WorkerClaimID != "" || item.WorkerLeaseUntil != nil || item.RetryCount != 1 ||
				item.VerificationStatus != "needs_review" || item.LastTaskPlanID != runner.result.PlanID || item.RecoveryNote == "" {
				t.Fatalf("uncertain result was not durably gated for interrupted resolution: %#v", item)
			}
			if !strings.Contains(item.BlockedReason, "outcome is uncertain") || item.LastWorkerError != item.BlockedReason ||
				(test.failure != "" && !strings.Contains(item.BlockedReason, "dispatch receipt missing")) {
				t.Fatalf("uncertain failure context was lost: %#v", item)
			}
			if _, exists := repo.attestations[item.ID]; exists {
				t.Fatal("uncertain result created a completion attestation")
			}
			expectedClaims := 1
			if test.noLaunch {
				expectedClaims = 0
			}
			if len(updated.FrameworkSelections) != 1 || len(updated.Evidence) != expectedClaims {
				t.Fatalf("partial result evidence was lost: selections=%#v evidence=%#v", updated.FrameworkSelections, updated.Evidence)
			}
			if !test.noLaunch {
				claim := updated.Evidence[0]
				if claim.SourceURI != runner.result.RuntimeEvidenceURI || claim.Status != "needs_review" || !claim.NeedsReview ||
					!strings.Contains(claim.ClaimText, "runtime=uncertain-runtime") || strings.Contains(claim.ClaimText, runner.result.Output) {
					t.Fatalf("uncertain runtime claim was accepted as verified or retained free-form output: %#v", claim)
				}
			}
			encoded, err := json.Marshal(struct {
				Record *WorkflowRecord
				Result WorkflowRunResult
			}{updated, result})
			if err != nil {
				t.Fatalf("encode persisted review evidence: %v", err)
			}
			for _, private := range []string{"synthetic-result-secret", "synthetic-runner-secret", runner.result.Output} {
				if strings.Contains(string(encoded), private) {
					t.Fatalf("review persistence or response leaked %q", private)
				}
			}
			if !hasDecision(updated.Decisions, "worker_execution", "needs_review") {
				t.Fatal("uncertain execution did not leave a review decision")
			}
			if _, err := service.Transition(item.ID, TransitionRequest{TargetState: StateReady}); err == nil {
				t.Fatal("ordinary transition bypassed interrupted resolution")
			}
			if _, err := service.ResolveApproval(item.ID, ApprovalResolutionRequest{Approved: true, Note: "approve retry"}); err == nil {
				t.Fatal("ordinary approval bypassed interrupted resolution")
			}
			if len(updated.Proposals) == 0 {
				t.Fatal("fixture did not create an ordinary proposal")
			}
			if _, err := service.ResolveProposal(item.ID, updated.Proposals[0].ID, ProposalResolutionRequest{Approved: true}); err == nil {
				t.Fatal("ordinary proposal approval bypassed interrupted resolution")
			}
			if summary, err := service.RunDue(RunDueRequest{Limit: 1}); err != nil || summary.Checked != 0 || len(runner.requests) != 1 {
				t.Fatalf("uncertain execution was automatically redispatched: summary=%#v err=%v calls=%d", summary, err, len(runner.requests))
			}
			if _, err := service.RunOneForOwner(item.OwnerIdentity, item.ID); err != nil || len(runner.requests) != 1 {
				t.Fatalf("direct run bypassed interrupted resolution: err=%v calls=%d", err, len(runner.requests))
			}
		})
	}
}

func TestRunDueExplicitOutcomeUncertaintyRetainsRouteWithoutEvidenceURI(t *testing.T) {
	repo := newFakeWorkflowRepo()
	runner := &fakeTaskRunner{result: &TaskRunResult{
		PlanID: "route-only-uncertain-plan", ExecutionOutcomeUncertain: true,
		Passed: true, CompletionStatus: "completed", VerificationStatus: "verified",
		Output:            "private-route-output",
		RuntimeRouteTrace: &models.AutomationRuntimeRouteTrace{RuntimeID: "route-only-runtime", ExecutionMode: "controlled"},
	}}
	service := NewServiceWithTaskRunner(repo, runner)
	record, err := service.Intake(IntakeRequest{Input: "Create Trello checklist for low risk admin work"})
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}
	if summary, err := service.RunDue(RunDueRequest{Limit: 1}); err != nil || summary.Blocked != 1 || summary.Completed != 0 || summary.Retried != 0 {
		t.Fatalf("RunDue = (%#v, %v), want interrupted review", summary, err)
	}
	updated, err := service.Get(record.Item.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if updated.Item.RecoveryStatus != RecoveryNeedsReview || len(updated.FrameworkSelections) != 1 || len(updated.Evidence) != 0 {
		t.Fatalf("route-only result lost recovery/provenance or invented evidence: %#v", updated)
	}
	found := false
	for _, event := range updated.Events {
		if event.EventType == "workflow.runtime_route_trace" && event.Trigger == "uncertain_task_outcome" && strings.Contains(event.Message, "runtime=route-only-runtime") {
			found = true
		}
		if strings.Contains(event.Message, runner.result.Output) {
			t.Fatal("route-only result retained private output")
		}
	}
	if !found {
		t.Fatal("uncertain route trace was not retained without a runtime evidence URI")
	}
}

func TestRunDueExplicitOutcomeUncertaintyAuditsEvidencePersistenceFailure(t *testing.T) {
	for _, failure := range []string{"framework", "runtime", "both"} {
		t.Run(failure, func(t *testing.T) {
			repo := newFakeWorkflowRepo()
			runner := &fakeTaskRunner{result: &TaskRunResult{
				PlanID: "uncertain-persistence-plan", ExecutionOutcomeUncertain: true,
				Passed: true, VerificationStatus: "verified", ApprovalRequired: true,
				RuntimeEvidenceURI: "automation-launch://22222222-2222-2222-2222-222222222222",
			}}
			service := NewServiceWithTaskRunner(repo, runner)
			record, err := service.Intake(IntakeRequest{Input: "Create Trello checklist for low risk admin work"})
			if err != nil {
				t.Fatalf("Intake: %v", err)
			}
			if failure == "framework" || failure == "both" {
				selection := testFrameworkSelection("different-plan")
				runner.result.FrameworkSelection = &selection
			}
			if failure == "runtime" || failure == "both" {
				repo.createEvidenceClaimErr = errors.New("runtime evidence store unavailable token=synthetic-store-secret")
			}
			summary, err := service.RunDue(RunDueRequest{Limit: 1})
			if err != nil || summary.Blocked != 1 || summary.Completed != 0 || summary.Retried != 0 {
				t.Fatalf("RunDue = (%#v, %v), evidence failures must not unlock uncertain execution", summary, err)
			}
			updated, err := service.Get(record.Item.ID)
			if err != nil {
				t.Fatalf("Get: %v", err)
			}
			if updated.Item.RecoveryStatus != RecoveryNeedsReview || updated.Item.NextRunAt != nil || !strings.Contains(updated.Item.BlockedReason, "evidence persistence failed") {
				t.Fatalf("uncertain evidence failure was not durably blocked: %#v", updated.Item)
			}
			if failure == "framework" && len(updated.Evidence) != 1 {
				t.Fatal("framework persistence failure prevented runtime evidence preservation")
			}
			if failure == "runtime" && len(updated.FrameworkSelections) != 1 {
				t.Fatal("runtime evidence failure lost framework provenance")
			}
			audited := false
			for _, event := range updated.Events {
				if event.EventType == "workflow.worker_review_required" && strings.Contains(event.Message, "evidence persistence failed") {
					audited = true
				}
			}
			if !audited {
				t.Fatal("uncertain evidence persistence failure was not audited")
			}
			encoded, err := json.Marshal(struct {
				Record  *WorkflowRecord
				Summary *WorkflowRunSummary
			}{updated, summary})
			if err != nil || strings.Contains(string(encoded), "synthetic-store-secret") {
				t.Fatalf("persistence failure details were not sanitized: err=%v", err)
			}
		})
	}
}

func TestExplicitOutcomeUncertaintyRetryRequiresReconciliationEvidence(t *testing.T) {
	repo := newFakeWorkflowRepo()
	runner := &fakeTaskRunner{result: &TaskRunResult{PlanID: "uncertain-prior-plan", ExecutionOutcomeUncertain: true, ApprovalRequired: true}}
	service := NewServiceWithTaskRunner(repo, runner)
	record, err := service.Intake(IntakeRequest{Input: "Create Trello checklist for low risk admin work"})
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}
	if summary, err := service.RunDue(RunDueRequest{Limit: 1}); err != nil || summary.Blocked != 1 {
		t.Fatalf("RunDue = (%#v, %v), want uncertain execution block", summary, err)
	}
	resolution := InterruptedExecutionResolutionRequest{
		Decision: "retry", Note: "Reviewed prior execution termination and confirmed no checklist was created.",
		Actor: "Robert",
	}
	if _, err := service.ResolveInterruptedExecution(record.Item.ID, resolution); err == nil {
		t.Fatal("retry accepted without prior execution reconciliation")
	}
	resolution.PriorExecutionReconciled = true
	if _, err := service.ResolveInterruptedExecution(record.Item.ID, resolution); err == nil {
		t.Fatal("retry accepted without a reconciliation evidence link")
	}
	resolution.EvidenceURI = "local://review/prior-execution-reconciliation"
	resolved, err := service.ResolveInterruptedExecution(record.Item.ID, resolution)
	if err != nil {
		t.Fatalf("ResolveInterruptedExecution: %v", err)
	}
	if resolved.Item.CurrentState != StateReady || resolved.Item.RecoveryStatus != RecoveryRetryConfirmed || !hasDecision(resolved.Decisions, "interrupted_execution", "retry") {
		t.Fatalf("evidence-backed recovery was not durably recorded: %#v", resolved)
	}
	runner.result = &TaskRunResult{PlanID: "reconciled-retry-plan", CompletionStatus: "validated", VerificationStatus: "verified", Passed: true}
	if summary, err := service.RunDue(RunDueRequest{Limit: 1}); err != nil || summary.Completed != 1 || len(runner.requests) != 2 {
		t.Fatalf("reconciled retry = (%#v, %v), calls=%d", summary, err, len(runner.requests))
	}
	completed, err := service.Get(record.Item.ID)
	if err != nil || completed.Item.RecoveryStatus != RecoveryCompletedAfterRetry || completed.Item.CompletedAt == nil {
		t.Fatalf("reconciled retry did not complete normally: record=%#v err=%v", completed, err)
	}
}

func TestHandleRunTaskReviewRequiredTreatsExplicitUncertaintyAsInterrupted(t *testing.T) {
	repo := newFakeWorkflowRepo()
	service := NewService(repo).(*service)
	record, err := service.Intake(IntakeRequest{Input: "Create Trello checklist for low risk admin work"})
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}
	now := time.Now().UTC()
	item, owned, err := repo.ClaimRunnableItem(record.Item.ID, "uncertain-helper-claim", now, now.Add(time.Minute))
	if err != nil || !owned || item == nil {
		t.Fatalf("claim review fixture = (%#v, %t, %v)", item, owned, err)
	}
	result := service.handleRunTaskReviewRequired(item, item.WorkerClaimID,
		&TaskRunResult{ExecutionOutcomeUncertain: true}, "outcome is uncertain token=synthetic-helper-secret", "verified")
	stored, err := repo.FindItem(item.ID)
	if err != nil || !result.ReviewRequired || stored.RecoveryStatus != RecoveryNeedsReview || stored.CurrentState != StateBlocked || stored.WorkerClaimID != "" ||
		result.VerificationStatus != "needs_review" || stored.VerificationStatus != "needs_review" || strings.Contains(stored.BlockedReason, "synthetic-helper-secret") {
		t.Fatalf("uncertain review helper = %#v, stored=%#v err=%v", result, stored, err)
	}
}

func TestRunDueCertainResultsKeepExistingCompletionApprovalAndRetryBehavior(t *testing.T) {
	tests := []struct {
		name   string
		result *TaskRunResult
		err    error
		state  string
		status string
	}{
		{name: "completed", result: &TaskRunResult{PlanID: "certain-completion", Passed: true, CompletionStatus: "validated", VerificationStatus: "verified"}, state: StateCompleted, status: "completed"},
		{name: "approval", result: &TaskRunResult{PlanID: "certain-approval", ApprovalRequired: true}, state: StateNeedsApproval, status: "blocked"},
		{name: "ordinary review", result: &TaskRunResult{PlanID: "certain-review", ReviewRequired: true}, state: StateBlocked, status: "blocked"},
		{name: "safe retry", err: MarkTaskFailureSafeNoSideEffect(errors.New("preflight rejected before dispatch")), state: StateReady, status: "retry_scheduled"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repo := newFakeWorkflowRepo()
			runner := &fakeTaskRunner{result: test.result, err: test.err}
			service := NewServiceWithTaskRunner(repo, runner)
			record, err := service.Intake(IntakeRequest{Input: "Create Trello checklist for low risk admin work"})
			if err != nil {
				t.Fatalf("Intake: %v", err)
			}
			summary, err := service.RunDue(RunDueRequest{Limit: 1})
			if err != nil || len(summary.Results) != 1 || summary.Results[0].Status != test.status {
				t.Fatalf("certain result behavior changed: summary=%#v err=%v", summary, err)
			}
			updated, err := service.Get(record.Item.ID)
			if err != nil || updated.Item.CurrentState != test.state || updated.Item.RecoveryStatus == RecoveryNeedsReview {
				t.Fatalf("certain result entered interrupted recovery: record=%#v err=%v", updated, err)
			}
		})
	}
}
