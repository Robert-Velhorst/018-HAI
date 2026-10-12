package task

import (
	"reflect"
	"strings"
	"time"

	"automation-hub-backend/internal/verification"
)

// Metadata can narrow the model-less path, but cannot grant execution authority.
// The existing risk, participant, evidence and runtime gates still apply.
func (s *service) modelExecutionPreflightBlocker(plan *CompletionPlan, request IntakeRequest) string {
	if plan != nil && deterministicReadOnlyRuntimeCompleted(completedToolExecution(plan.ExecutionResult)) {
		// Reusing an immutable, already-completed probe neither relaunches the
		// current mutable runtime nor requests model generation.
		return ""
	}
	if plan != nil && strings.TrimSpace(plan.ModelDecision.SelectedModelID) != "" {
		return ""
	}
	const missing = "no capable model was selected; configure an eligible model before retrying"
	if !modelRouteIsNotRequiredForDeterministicRuntime(plan) {
		return missing
	}
	inspector, ok := s.toolExecutor.(ReadOnlyRuntimeInspector)
	if !ok || inspector == nil {
		return missing + "; the selected runtime has no verified read-only metadata"
	}
	value := reflect.ValueOf(inspector)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		if value.IsNil() {
			return missing + "; the selected runtime has no verified read-only metadata"
		}
	}
	readOnly, err := inspector.IsReadOnlyRuntime(request.AutomationID)
	if err != nil {
		// Do not expose repository errors, which can contain credential-bearing URLs.
		return missing + "; read-only runtime metadata could not be verified"
	}
	if !readOnly {
		return missing + "; model-free execution is restricted to registered GET or HEAD actions"
	}
	return ""
}

func modelPreflightBlockedExecution(plan *CompletionPlan, reason string) *ExecutionResult {
	now := time.Now().UTC()
	plan.Events = append(plan.Events, event("model-preflight", reason))
	result := &ExecutionResult{
		StartedAt: now, CompletedAt: now, Mode: "blocked",
		VerificationStatus: verification.StatusNeedsReview,
		Output:             "Controlled runtime and model execution were blocked before dispatch.", BlockedReason: reason,
		Actions: []ExecutedAction{executedAction("governance.model_preflight", "blocked", plan.Request, reason, now)},
	}
	if previous := plan.ExecutionResult; previous != nil && (previous.ToolExecution != nil || executionOutcomeUncertain(previous)) {
		result.OutcomeUncertain = executionOutcomeUncertain(previous)
		result.ToolExecution = previous.ToolExecution
		result.Actions = append(append([]ExecutedAction{}, previous.Actions...), result.Actions...)
		result.Output = "Further execution was blocked; prior runtime evidence was retained for review."
	}
	return result
}
