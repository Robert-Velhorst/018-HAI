package task

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/automation"
	"automation-hub-backend/internal/models"

	"github.com/google/uuid"
)

func TestAutomationToolExecutorOutcomeRetainsResultAndError(t *testing.T) {
	for _, status := range []string{"completed", "failed", "blocked", "indeterminate", "running"} {
		t.Run(status, func(t *testing.T) {
			id := uuid.New()
			cause := &outcomeLaunchError{message: "launch projection unavailable api_key=outcome-error-secret"}
			launch := &automation.LaunchResult{
				AutomationID: id, LaunchEventID: uuid.New(), RuntimeType: "openclaw",
				RuntimeTaskID: "runtime-task-1", ExecutionReference: "ocgw:run-1",
				LaunchType: "agent_runtime", Target: "local-runtime", Status: status,
				Message: "runtime outcome", Output: "runtime evidence", ExitCode: 0,
				DurationMs: 123, RequiresApproval: status == "blocked", LaunchedAt: time.Now().UTC(),
				AuditEvents: []string{"immutable intent persisted", "runtime dispatched"},
				RuntimeRouteTrace: &models.AutomationRuntimeRouteTrace{
					RuntimeID: "openclaw", RecommendedSkills: []string{"review"},
				},
			}
			launcher := &fakeAutomationLauncher{result: launch, err: cause}
			got, err := NewAutomationToolExecutor(launcher).Execute(outcomeExecutionRequest(id))
			if got == nil || err == nil || !errors.Is(err, cause) {
				t.Fatalf("result/error pair was lost: result=%#v err=%v", got, err)
			}
			var typedCause *outcomeLaunchError
			if !errors.As(err, &typedCause) || typedCause != cause {
				t.Fatalf("typed launch error was lost: %v", err)
			}
			if IsToolFailureBeforeDispatch(err) || !got.OutcomeUncertain {
				t.Fatalf("launch error claimed safe pre-dispatch: result=%#v err=%v", got, err)
			}
			if strings.Contains(err.Error(), "outcome-error-secret") {
				t.Fatalf("error exposed a secret: %v", err)
			}
			if got.AutomationID != id.String() || got.LaunchEventID != launch.LaunchEventID.String() ||
				got.RuntimeTaskID != launch.RuntimeTaskID || got.ExecutionReference != launch.ExecutionReference ||
				got.RuntimeType != launch.RuntimeType || got.LaunchType != launch.LaunchType ||
				got.Target != launch.Target || got.Status != launch.Status ||
				got.Message != launch.Message || got.Output != launch.Output ||
				got.ExitCode != launch.ExitCode || got.DurationMs != launch.DurationMs ||
				got.RequiresApproval != launch.RequiresApproval || !got.ExecutedAt.Equal(launch.LaunchedAt) ||
				!reflect.DeepEqual(got.AuditEvents, launch.AuditEvents) ||
				got.RuntimeRouteTrace == nil || got.RuntimeRouteTrace.RuntimeID != launch.RuntimeRouteTrace.RuntimeID ||
				!reflect.DeepEqual(got.RuntimeRouteTrace.RecommendedSkills, launch.RuntimeRouteTrace.RecommendedSkills) {
				t.Fatalf("launch metadata changed: got=%#v launch=%#v", got, launch)
			}
			got.AuditEvents[0] = "mutated"
			got.RuntimeRouteTrace.RecommendedSkills[0] = "mutated"
			if launch.AuditEvents[0] == "mutated" || launch.RuntimeRouteTrace.RecommendedSkills[0] == "mutated" {
				t.Fatal("mapped result aliases launcher-owned metadata")
			}
			if launcher.launchCalls != 1 {
				t.Fatalf("launch calls = %d, want exactly one", launcher.launchCalls)
			}
		})
	}
}

func TestAutomationToolExecutorOutcomeMissingResultIsUncertain(t *testing.T) {
	cause := errors.New("summary update failed token=missing-result-secret")
	for _, test := range []struct {
		name string
		err  error
	}{
		{"nil-result-nil-error", nil},
		{"nil-result-summary-error", cause},
		{"nil-result-approval-error", automation.ErrApprovalProofConsumed},
	} {
		t.Run(test.name, func(t *testing.T) {
			launchErr := test.err
			id := uuid.New()
			launcher := &fakeAutomationLauncher{err: launchErr}
			got, err := NewAutomationToolExecutor(launcher).Execute(outcomeExecutionRequest(id))
			if got == nil || err == nil || got.Status != "indeterminate" || !got.OutcomeUncertain || got.ExitCode != -1 {
				t.Fatalf("missing launch result was not uncertain: result=%#v err=%v", got, err)
			}
			if got.AutomationID != id.String() || got.LaunchEventID != "" || !got.ExecutedAt.IsZero() || got.RequiresApproval {
				t.Fatalf("missing result invented launch evidence or approval requirements: %#v", got)
			}
			if launchErr != nil && !errors.Is(err, launchErr) {
				t.Fatalf("original error identity was lost: %v", err)
			}
			if IsToolFailureBeforeDispatch(err) || strings.Contains(err.Error(), "missing-result-secret") {
				t.Fatalf("post-call error was unsafe: %v", err)
			}
			if launcher.launchCalls != 1 {
				t.Fatalf("launch calls = %d, want one without retry", launcher.launchCalls)
			}
		})
	}
}

func TestAutomationToolExecutorOutcomeStatusUncertainty(t *testing.T) {
	for _, test := range []struct {
		status    string
		uncertain bool
	}{
		{"completed", false}, {"blocked", false}, {"needs_approval", false},
		{"failed", true}, {"indeterminate", true}, {"needs_review", true},
		{"pending", true}, {"queued", true}, {"running", true}, {"unknown", true},
		{"", true}, {"new-provider-status", true}, {" COMPLETED ", true},
	} {
		t.Run(test.status, func(t *testing.T) {
			id := uuid.New()
			launcher := &fakeAutomationLauncher{result: &automation.LaunchResult{
				AutomationID: id, LaunchEventID: uuid.New(), LaunchType: "script", Status: test.status,
			}}
			got, err := NewAutomationToolExecutor(launcher).Execute(outcomeExecutionRequest(id))
			if err != nil || got == nil || got.OutcomeUncertain != test.uncertain {
				t.Fatalf("status %q: result=%#v err=%v, want uncertainty %t", test.status, got, err, test.uncertain)
			}
		})
	}
}

func TestAutomationToolExecutorOutcomeMalformedCompletionIsUncertain(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*automation.LaunchResult)
	}{
		{"no-event", func(r *automation.LaunchResult) { r.LaunchEventID = uuid.Nil }},
		{"no-automation", func(r *automation.LaunchResult) { r.AutomationID = uuid.Nil }},
		{"wrong-automation", func(r *automation.LaunchResult) { r.AutomationID = uuid.New() }},
		{"nonzero-exit", func(r *automation.LaunchResult) { r.ExitCode = 1 }},
		{"approval-still-required", func(r *automation.LaunchResult) { r.RequiresApproval = true }},
	} {
		t.Run(test.name, func(t *testing.T) {
			id := uuid.New()
			launch := &automation.LaunchResult{AutomationID: id, LaunchEventID: uuid.New(), Status: "completed"}
			test.change(launch)
			got, err := NewAutomationToolExecutor(&fakeAutomationLauncher{result: launch}).Execute(outcomeExecutionRequest(id))
			if err != nil || got == nil || !got.OutcomeUncertain {
				t.Fatalf("malformed completion was trusted: result=%#v err=%v", got, err)
			}
		})
	}
}

func TestAutomationToolExecutorOutcomePreservesHTTPReceiptStatus(t *testing.T) {
	for _, test := range []struct {
		launchType string
		code       int
		uncertain  bool
	}{
		{"api", 200, false}, {"api", 204, false}, {"api", 302, false},
		{"api", 404, false}, {"api", 503, false}, {"api", 0, true},
		{"docker_service", 204, false}, {"docker_service", 304, false}, {"docker_service", 0, true},
		{"script", 0, false}, {"script", 200, true}, {"agent_runtime", 0, false},
	} {
		t.Run(fmt.Sprintf("%s/%d", test.launchType, test.code), func(t *testing.T) {
			id := uuid.New()
			launch := &automation.LaunchResult{AutomationID: id, LaunchEventID: uuid.New(), LaunchType: test.launchType, Status: "completed", ExitCode: test.code}
			got, err := NewAutomationToolExecutor(&fakeAutomationLauncher{result: launch}).Execute(outcomeExecutionRequest(id))
			if err != nil || got == nil || got.OutcomeUncertain != test.uncertain || got.ExitCode != test.code {
				t.Fatalf("receipt HTTP/process semantics were confused: result=%#v err=%v", got, err)
			}
		})
	}
}

func TestAutomationToolExecutorOutcomePreDispatchErrorsRemainDistinct(t *testing.T) {
	for _, stage := range []string{"validation", "inspection", "proof", "envelope"} {
		t.Run(stage, func(t *testing.T) {
			id := uuid.New()
			request := outcomeExecutionRequest(id)
			cause := fmt.Errorf("approval infrastructure token=predispatch-secret: %w", automation.ErrApprovalDecisionMissing)
			launcher := &fakeAutomationLauncher{}
			if stage == "validation" {
				request.AutomationID = "invalid"
			} else {
				request.WorkflowID = uuid.NewString()
				request.ApprovalSourceID = "workflow-decision:" + uuid.NewString()
				if stage == "inspection" {
					launcher.approvalInspectionErr = cause
				} else if stage == "proof" {
					launcher.issueErr = cause
				}
			}
			got, err := NewAutomationToolExecutor(launcher).Execute(request)
			if got != nil || !IsToolFailureBeforeDispatch(err) || launcher.launchCalls != 0 {
				t.Fatalf("early failure lost pre-dispatch classification: result=%#v err=%v calls=%d", got, err, launcher.launchCalls)
			}
			if strings.Contains(err.Error(), "predispatch-secret") {
				t.Fatalf("early error exposed a secret: %v", err)
			}
			if (stage == "inspection" || stage == "proof") && !errors.Is(err, automation.ErrApprovalDecisionMissing) {
				t.Fatalf("approval error identity lost: %v", err)
			}
		})
	}
}

func TestAutomationToolExecutorOutcomeSanitizesLaunchMetadata(t *testing.T) {
	id := uuid.New()
	launcher := &fakeAutomationLauncher{result: &automation.LaunchResult{
		AutomationID: id, LaunchEventID: uuid.New(), LaunchType: "script", Status: "indeterminate",
		Target:        "https://example.invalid/run?token=target-outcome-secret",
		RuntimeTaskID: "token=runtime-task-outcome-secret", ExecutionReference: "api_key=reference-outcome-secret",
		Message: "api_key=message-outcome-secret", Output: "Bearer output-outcome-secret",
		AuditEvents: []string{"password=audit-outcome-secret"},
		RuntimeRouteTrace: &models.AutomationRuntimeRouteTrace{
			RuntimeID: "token=trace-runtime-secret", Intent: "password=trace-intent-secret",
			ExecutionMode: "api_key=trace-mode-secret", RiskLevel: "secret=trace-risk-secret",
			RecommendedSkills:   []string{"token=trace-skill-secret"},
			VisibleProviders:    []string{"token=trace-provider-secret"},
			VisibleTools:        []string{"token=trace-tool-secret"},
			RelevantMaps:        []string{"token=trace-map-secret"},
			BlockedSurfaces:     []string{"token=trace-surface-secret"},
			RequiredControls:    []string{"token=trace-control-secret"},
			ValidationChecklist: []string{"token=trace-checklist-secret"},
		},
	}}
	got, err := NewAutomationToolExecutor(launcher).Execute(outcomeExecutionRequest(id))
	if err != nil || got == nil {
		t.Fatalf("Execute: result=%#v err=%v", got, err)
	}
	encoded, encodeErr := json.Marshal(got)
	if encodeErr != nil {
		t.Fatalf("encode result: %v", encodeErr)
	}
	for _, secret := range []string{
		"target-outcome-secret", "runtime-task-outcome-secret", "reference-outcome-secret",
		"message-outcome-secret", "output-outcome-secret", "audit-outcome-secret",
		"trace-runtime-secret", "trace-intent-secret", "trace-mode-secret", "trace-risk-secret",
		"trace-skill-secret", "trace-provider-secret", "trace-tool-secret", "trace-map-secret",
		"trace-surface-secret", "trace-control-secret", "trace-checklist-secret",
	} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("mapped result leaked %q", secret)
		}
	}
	if launcher.result.RuntimeRouteTrace.RuntimeID != "token=trace-runtime-secret" ||
		launcher.result.RuntimeRouteTrace.RecommendedSkills[0] != "token=trace-skill-secret" {
		t.Fatal("sanitization mutated launcher-owned route trace")
	}
}

func TestAutomationToolExecutorOutcomeBoundsCorrelationMetadata(t *testing.T) {
	id := uuid.New()
	launcher := &fakeAutomationLauncher{result: &automation.LaunchResult{
		AutomationID: id, LaunchEventID: uuid.New(), Status: "indeterminate",
		RuntimeTaskID: strings.Repeat("x", 4096), ExecutionReference: strings.Repeat("y", 4096),
	}}
	got, err := NewAutomationToolExecutor(launcher).Execute(outcomeExecutionRequest(id))
	if err != nil || got == nil || len(got.RuntimeTaskID) != 512 || len(got.ExecutionReference) != 2048 {
		t.Fatalf("correlation metadata was not bounded: result=%#v err=%v", got, err)
	}
}

func TestAutomationToolExecutorOutcomeReceiptMismatchOverridesBlocked(t *testing.T) {
	id := uuid.New()
	launcher := &fakeAutomationLauncher{result: &automation.LaunchResult{
		AutomationID: uuid.New(), LaunchEventID: uuid.New(), Status: "blocked", RequiresApproval: true,
	}}
	got, err := NewAutomationToolExecutor(launcher).Execute(outcomeExecutionRequest(id))
	if err != nil || got == nil || !got.OutcomeUncertain || got.AutomationID != launcher.result.AutomationID.String() {
		t.Fatalf("mismatched blocked receipt was trusted or rewritten: result=%#v err=%v", got, err)
	}
}

func TestAutomationToolExecutorOutcomeUnavailableExecutorIsPreDispatch(t *testing.T) {
	for _, executor := range []*AutomationToolExecutor{nil, NewAutomationToolExecutor(nil)} {
		got, err := executor.Execute(outcomeExecutionRequest(uuid.New()))
		if got != nil || !IsToolFailureBeforeDispatch(err) {
			t.Fatalf("unavailable executor did not retain pre-dispatch classification: result=%#v err=%v", got, err)
		}
	}
}

type outcomeLaunchError struct{ message string }

func (e *outcomeLaunchError) Error() string { return e.message }

func outcomeExecutionRequest(id uuid.UUID) ToolExecutionRequest {
	return ToolExecutionRequest{OwnerIdentity: "alice", TaskID: "task-outcome-1", AutomationID: id.String(), Task: "Run reviewed task"}
}
