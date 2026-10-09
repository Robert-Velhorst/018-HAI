package task

import (
	"errors"
	"strings"
	"time"

	"automation-hub-backend/internal/verification"
)

type toolFailureBeforeDispatch struct{ err error }

func (e *toolFailureBeforeDispatch) Error() string {
	if e == nil || e.err == nil {
		return "tool failure has no pre-dispatch proof"
	}
	return e.err.Error()
}

func (e *toolFailureBeforeDispatch) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.err
}

// MarkToolFailureBeforeDispatch is for trusted executor checks that have not
// invoked the runtime. A transport, launch, or audit error must not use it.
func MarkToolFailureBeforeDispatch(err error) error {
	if err == nil {
		return nil
	}
	return &toolFailureBeforeDispatch{err: err}
}

func IsToolFailureBeforeDispatch(err error) bool {
	marked := false
	for err != nil {
		if _, joined := err.(interface{ Unwrap() []error }); joined {
			return false
		}
		if before, ok := err.(*toolFailureBeforeDispatch); ok {
			if before == nil || before.err == nil {
				return false
			}
			marked = true
		}
		err = errors.Unwrap(err)
	}
	return marked
}

var ErrTaskOutcomeReconciliationRequired = errors.New("uncertain runtime outcome requires reconciliation; ordinary approval cannot repeat execution")

var ErrTaskReviewConfigurationUnavailable = errors.New("reviewed automation configuration is unavailable; create a new review after repairing configuration")

// ToolExecutionOutcomeUncertain distinguishes a receipt from proof of a
// terminal outcome. Failed or nonterminal launches may already have effects.
func ToolExecutionOutcomeUncertain(result *ToolExecutionResult) bool {
	if result == nil {
		return false
	}
	if result.OutcomeUncertain {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(result.Status)) {
	case "completed":
		return result.RequiresApproval
	case "blocked", "needs_approval":
		return false
	default:
		return true
	}
}

// ExecutionOutcomeUncertain includes legacy action-only evidence, so adapters
// cannot silently turn a missing receipt into a safe no-effect result.
func ExecutionOutcomeUncertain(result *ExecutionResult) bool {
	if result == nil {
		return false
	}
	if result.OutcomeUncertain || ToolExecutionOutcomeUncertain(result.ToolExecution) {
		return true
	}
	for _, action := range result.Actions {
		if action.Name == "automation.launch" {
			switch strings.ToLower(strings.TrimSpace(action.Status)) {
			case "completed", "reused", "blocked", "needs_approval":
			default:
				return true
			}
		}
	}
	return false
}

func executionOutcomeUncertain(result *ExecutionResult) bool {
	return ExecutionOutcomeUncertain(result)
}

const uncertainToolOutcomeReason = "controlled runtime outcome is uncertain; inspect the launch audit and reconcile possible side effects before authorizing a separate attempt"

func blockUncertainToolOutcome(result *ExecutionResult, reason string, plan *CompletionPlan, started time.Time) *ExecutionResult {
	result.OutcomeUncertain = true
	if reason = strings.TrimSpace(reason); reason != "" {
		reason = uncertainToolOutcomeReason + ": " + reason
	} else {
		reason = uncertainToolOutcomeReason
	}
	if result.ToolExecution == nil {
		result.Actions = append(result.Actions, executedAction("automation.launch", "indeterminate", plan.Request, sanitizeTaskOperationalText(reason, 2048), started))
	}
	result = blockExecution(result, reason, plan, started)
	result.VerificationStatus = verification.StatusUncertain
	result.Output = "Runtime execution may have occurred. Completion was not verified; automatic retry is disabled. " + result.BlockedReason
	return result
}

func blockPriorUncertainExecution(plan *CompletionPlan, request IntakeRequest, started time.Time) *ExecutionResult {
	result := newExecutionResult(plan, request, started)
	if previous := plan.ExecutionResult; previous != nil {
		result.ToolExecution = previous.ToolExecution
		result.Actions = append([]ExecutedAction{}, previous.Actions...)
	}
	return blockUncertainToolOutcome(result, "prior runtime evidence was retained without repeating execution", plan, started)
}
