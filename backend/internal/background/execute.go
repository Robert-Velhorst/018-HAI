package background

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"automation-hub-backend/internal/executionbroker"
	"automation-hub-backend/internal/idempotency"
	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/operations"
	"automation-hub-backend/internal/safety"

	"github.com/google/uuid"
)

// SafeOutcome reports the result of executing an Operation via the local safe
// worker.
type SafeOutcome struct {
	Operation   *models.Operation
	Receipt     *executionbroker.ExecutionResult
	Verified    bool
	Failed      bool
	Interrupted bool
}

type safeOperationExecutor interface {
	ExecuteLocalSafeWorker(context.Context, executionbroker.SafeWorkerInput) (executionbroker.ExecutionResult, error)
}

type safeWorkerBeforeEffectError struct{ err error }

func (e *safeWorkerBeforeEffectError) Error() string { return e.err.Error() }
func (e *safeWorkerBeforeEffectError) Unwrap() error { return e.err }

// ErrSafeOutcomeUncertain prevents replay without reconciliation of effects.
var ErrSafeOutcomeUncertain = errors.New("safe worker outcome requires reconciliation before another execution")

var ErrSafeExecutionPolicyUnavailable = errors.New("safe worker requires a live final-effect policy")
var ErrSafeExecutionPolicyChanged = errors.New("safe worker blocked by current final-effect policy")

// SafeExecutionPolicy reads live controls for the locked operation snapshot.
// It must not mutate controls or reenter the operation repository. Production
// control writers share the execution fence held while this callback runs.
type SafeExecutionPolicy func(models.Operation) bool

type safeWorkerExecutionEvidence struct {
	Result           executionbroker.ExecutionResult `json:"result"`
	OutcomeUncertain bool                            `json:"outcomeUncertain"`
	BeforeEffect     bool                            `json:"beforeEffect,omitempty"`
	Intent           *safeWorkerExecutionIntent      `json:"intent,omitempty"`
}

// Intent survives a crash before the worker's result can be saved. It records
// the planned attempt, not consumption, dispatch, or an observed file identity.
type safeWorkerExecutionIntent struct {
	AttemptID       string `json:"attemptId"`
	ArtifactName    string `json:"artifactName"`
	MarkerHash      string `json:"markerHash"`
	ClaimGeneration int64  `json:"claimGeneration,omitempty"`
}

// ExecuteSafeOperation runs the local safe worker for an Operation and gates
// completion on passing verification (§8/§10.15). It drives the Operation
// classified/ready/failed -> ready -> running -> verifying -> completed.
// Uncertain execution is interrupted for reconciliation, not marked retryable.
// It refuses inconsistent, approval-required, non-low-risk, non-automatic, or
// non-HAI-owned Operations before the local file-writing worker can run.
func ExecuteSafeOperation(ctx context.Context, svc *operations.Service, broker *executionbroker.Broker, op models.Operation, now time.Time) (SafeOutcome, error) {
	return executeSafeOperation(ctx, svc, broker, op, nil, now)
}

// ExecuteSafeOperationClaimed requires exactly one live server policy and fences
// every operation transition to the durable claim generation. Missing policy
// fails closed; it is not permission to reuse an earlier admission decision.
func ExecuteSafeOperationClaimed(ctx context.Context, svc *operations.Service, broker *executionbroker.Broker, op models.Operation, claim operations.ExecutionClaim, now time.Time, policy ...SafeExecutionPolicy) (SafeOutcome, error) {
	return executeSafeOperation(ctx, svc, broker, op, &claim, now, policy...)
}

func executeSafeOperation(ctx context.Context, svc *operations.Service, broker safeOperationExecutor, op models.Operation, claim *operations.ExecutionClaim, now time.Time, policy ...SafeExecutionPolicy) (SafeOutcome, error) {
	ctx, finish, err := operations.BindExecutionContext(ctx)
	if err != nil {
		return SafeOutcome{}, err
	}
	defer finish()
	if err := operations.Validate(op); err != nil {
		return SafeOutcome{}, err
	}
	sourceApproved := operations.IsSourceDerived(op)
	if sourceApproved {
		if svc == nil {
			return SafeOutcome{}, operations.ErrSourceApprovalRequired
		}
		stored, err := svc.Get(op.OwnerUserID, op.WorkspaceID, op.ID)
		if err != nil {
			return SafeOutcome{}, err
		}
		if !sameSourceIdentitySnapshot(*stored, op) {
			return SafeOutcome{}, operations.ErrSourceIdentityImmutable
		}
		if _, err := svc.SourceApprovalForExecution(op); err != nil {
			if errors.Is(err, operations.ErrSourceApprovalRequired) {
				return SafeOutcome{}, fmt.Errorf("operation %s ordinary owner approval is not bound to its source revision and cannot authorize execution: %w", op.ID, err)
			}
			return SafeOutcome{}, err
		}
		if claim == nil {
			return SafeOutcome{}, operations.ErrSafeEffectUnsupported
		}
	}
	if err := safeExecutionPolicyErrorWithSourceApproval(op, sourceApproved); err != nil {
		return SafeOutcome{}, err
	}
	if _, realBroker := broker.(*executionbroker.Broker); realBroker && claim == nil {
		return SafeOutcome{}, &safeWorkerBeforeEffectError{err: operations.ErrSafeEffectUnsupported}
	}
	if _, realBroker := broker.(*executionbroker.Broker); realBroker && (len(policy) != 1 || policy[0] == nil) {
		return SafeOutcome{}, &safeWorkerBeforeEffectError{err: ErrSafeExecutionPolicyUnavailable}
	}
	state, err := safeExecutionState(op)
	if err != nil {
		return SafeOutcome{}, err
	}
	approvedSnapshot := op

	// A source-derived approval is consumed atomically with approved->running;
	// all other safe work retains the classified/failed->ready path.
	if !sourceApproved {
		switch operations.OperationStatus(op.Status) {
		case operations.StatusClassified, operations.StatusFailed:
			ready, err := transitionExecution(ctx, svc, claim, op, operations.StatusReady, "ready for safe local worker")
			if err != nil {
				return SafeOutcome{}, err
			}
			op = *ready
		case operations.StatusReady:
			// already ready
		default:
			return SafeOutcome{}, fmt.Errorf("operation %s cannot be safe-executed from status %s", op.ID, op.Status)
		}
	}
	if err := operations.ValidateExecutionContext(ctx); err != nil {
		return SafeOutcome{Operation: &op}, err
	}

	op.RuntimeID = executionbroker.LocalSafeWorkerID
	op.VerificationStatus = string(operations.VerificationPending)
	in := safePayload(op)
	markerHash := sha256.Sum256([]byte(in.Marker))
	intent := &safeWorkerExecutionIntent{
		AttemptID: uuid.NewString(), ArtifactName: in.ArtifactName,
		MarkerHash: hex.EncodeToString(markerHash[:]),
	}
	if claim != nil {
		intent.ClaimGeneration = claim.Generation
	}
	state["safeWorkerExecution"], err = json.Marshal(safeWorkerExecutionEvidence{
		Result:           executionbroker.ExecutionResult{RuntimeID: executionbroker.LocalSafeWorkerID},
		OutcomeUncertain: true, Intent: intent,
	})
	if err != nil {
		return SafeOutcome{Operation: &op}, err
	}
	op.WorldModelStateJSON, err = idempotency.CanonicalJSONString(state)
	if err != nil {
		return SafeOutcome{Operation: &op}, err
	}
	// Commit uncertainty with the running transition before entering the broker.
	// A crash or a failed first result write must not leave an empty receipt that
	// could later be interpreted as permission to retry.
	if !sourceApproved {
		running, err := transitionExecution(ctx, svc, claim, op, operations.StatusRunning, "safe worker attempt recorded before dispatch")
		if err != nil {
			return SafeOutcome{}, err
		}
		op = *running
	} else {
		running, err := svc.ConsumeSourceApprovalClaimed(ctx, *claim, approvedSnapshot, op, string(operations.OwnerHAI), claim.Owner.String())
		if err != nil {
			return SafeOutcome{}, err
		}
		op = *running
		// Verify the consumed receipt after the fenced transition and before the
		// locked effect boundary; that boundary rechecks the full snapshot and live
		// policy immediately before dispatch.
		if err := svc.ValidateConsumedSourceApproval(op); err != nil {
			return SafeOutcome{Operation: &op}, err
		}
	}

	var res executionbroker.ExecutionResult
	var execErr error
	if _, realBroker := broker.(*executionbroker.Broker); realBroker {
		entered := false
		execErr = svc.WithClaimedSafeEffect(ctx, *claim, op, func(effectCtx context.Context) error {
			release, err := safety.AcquireExecutionCommitFenceContext(effectCtx)
			if err != nil {
				return err
			}
			defer release()
			if err := operations.ValidateExecutionContext(effectCtx); err != nil {
				return err
			}
			if !policy[0](op) {
				return ErrSafeExecutionPolicyChanged
			}
			entered = true
			res, err = executeSafeWorker(effectCtx, broker, in)
			return err
		})
		if !entered && execErr != nil {
			res.RuntimeID = executionbroker.LocalSafeWorkerID
			execErr = &safeWorkerBeforeEffectError{err: execErr}
		}
	} else {
		// Synthetic fault executors are package-private. Public execution takes
		// a concrete broker and cannot bypass the locked repository boundary.
		res, execErr = executeSafeWorker(ctx, broker, in)
	}
	outcome := SafeOutcome{Operation: &op, Receipt: &res}
	uncertain := !safeWorkerCompleted(res) || execErr != nil
	preEffectRefusal := safeWorkerFailureBeforeEffect(res, execErr)
	if preEffectRefusal && !errors.Is(execErr, context.Canceled) && !errors.Is(execErr, context.DeadlineExceeded) {
		uncertain = false
	}
	state["safeWorkerExecution"], err = json.Marshal(safeWorkerExecutionEvidence{Result: res, OutcomeUncertain: uncertain, BeforeEffect: preEffectRefusal && !uncertain, Intent: intent})
	if err != nil {
		return outcome, errors.Join(execErr, err)
	}
	// Keep runtime receipts outside source evidence, which feed re-ingestion
	// replaces. Retain the existing policy/world-model fields alongside them.
	op.WorldModelStateJSON, err = idempotency.CanonicalJSONString(state)
	if err != nil {
		return outcome, errors.Join(execErr, err)
	}
	op.ResultSummary = res.Output.BoundedOutput
	if uncertain {
		cause := errors.Join(ErrSafeOutcomeUncertain, execErr)
		op.LastError = cause.Error()
		op.VerificationStatus = string(operations.VerificationPending)
		finalizeCtx := ctx
		if ctx.Err() != nil {
			var cancel context.CancelFunc
			finalizeCtx, cancel = context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
		}
		interrupted, transitionErr := transitionExecution(finalizeCtx, svc, claim, op, operations.StatusInterrupted, "safe worker outcome uncertain; inspect retained receipt; no automatic replay")
		if transitionErr != nil {
			return outcome, errors.Join(cause, transitionErr)
		}
		outcome.Operation, outcome.Interrupted = interrupted, true
		return outcome, cause
	}
	if execErr != nil {
		op.VerificationStatus = string(operations.VerificationFailed)
		op.LastError = execErr.Error()
		failed, err := transitionExecution(ctx, svc, claim, op, operations.StatusFailed, "safe worker error: "+execErr.Error())
		if err != nil {
			return outcome, errors.Join(execErr, err)
		}
		outcome.Operation, outcome.Failed = failed, true
		return outcome, nil
	}

	verifying, err := transitionExecution(ctx, svc, claim, op, operations.StatusVerifying, "verifying safe worker result")
	if err != nil {
		return outcome, err
	}
	op = *verifying

	op.VerificationStatus = string(operations.VerificationPassed)
	completed, err := transitionExecution(ctx, svc, claim, op, operations.StatusCompleted, "verified and completed")
	if err != nil {
		op.VerificationStatus = verifying.VerificationStatus
		return outcome, err
	}
	outcome.Operation, outcome.Verified = completed, true
	return outcome, nil
}

func sameSourceIdentitySnapshot(stored, candidate models.Operation) bool {
	return stored.SourceProvider == candidate.SourceProvider &&
		stored.SourceAccount == candidate.SourceAccount &&
		stored.SourceExternalID == candidate.SourceExternalID &&
		stored.SourceIdentityHash == candidate.SourceIdentityHash &&
		stored.SourceRevisionHash == candidate.SourceRevisionHash &&
		stored.SourceObservationGeneration == candidate.SourceObservationGeneration &&
		sameUUIDPointer(stored.SourceID, candidate.SourceID) &&
		sameUUIDPointer(stored.SourceObservationID, candidate.SourceObservationID) &&
		sameUUIDPointer(stored.AccountFeedID, candidate.AccountFeedID)
}

func sameUUIDPointer(left, right *uuid.UUID) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func safeWorkerCompleted(res executionbroker.ExecutionResult) bool {
	v := res.Verification
	p := res.Output.Progress
	progressComplete := p == (executionbroker.SafeWorkerProgress{}) ||
		(p.Authorization == "consumed" && p.EffectStarted && p.ArtifactCreated && p.WriteComplete &&
			p.SyncComplete && p.ReadComplete && p.IdentityVerified && p.FileClosed &&
			p.BytesWritten > 0 && p.BytesWritten == p.ReadBytes)
	return progressComplete && res.RuntimeID == executionbroker.LocalSafeWorkerID && res.OK && v.Passed &&
		v.FileExists && v.InsideWorkspace && v.HashMatches && v.MarkerFound && v.OutputBounded &&
		res.Output.ArtifactPath != "" && res.Output.ArtifactHash != "" && res.Output.MarkerFound
}

func executeSafeWorker(ctx context.Context, executor safeOperationExecutor, in executionbroker.SafeWorkerInput) (executionbroker.ExecutionResult, error) {
	if err := operations.ValidateExecutionContext(ctx); err != nil {
		return executionbroker.ExecutionResult{RuntimeID: executionbroker.LocalSafeWorkerID}, &safeWorkerBeforeEffectError{err: err}
	}
	if broker, ok := executor.(*executionbroker.Broker); ok && broker != nil && broker.SafeWorker() != nil {
		health := broker.SafeWorker().HealthCheck(ctx)
		if health.Status == executionbroker.RuntimeNotConfigured {
			return executionbroker.ExecutionResult{RuntimeID: executionbroker.LocalSafeWorkerID},
				&safeWorkerBeforeEffectError{err: fmt.Errorf("safe worker is not configured: %s", health.Detail)}
		}
	}
	return executor.ExecuteLocalSafeWorker(ctx, in)
}

// Only the local preflight and these broker refusals prove no filesystem effect.
// A joined error or any returned effect evidence defeats that proof.
func safeWorkerFailureBeforeEffect(res executionbroker.ExecutionResult, err error) bool {
	if safeWorkerHasEffectEvidence(res) {
		return false
	}
	beforeEffect := false
	for err != nil {
		if _, joined := err.(interface{ Unwrap() []error }); joined {
			return false
		}
		if marker, ok := err.(*safeWorkerBeforeEffectError); ok && marker != nil && marker.err != nil {
			beforeEffect = true
		}
		if next := errors.Unwrap(err); next != nil {
			err = next
			continue
		}
		return beforeEffect || err == executionbroker.ErrAuthorizationRequired || err == executionbroker.ErrAuthorizationDenied || err == executionbroker.ErrAuthorizationMismatch
	}
	return false
}

func safeWorkerHasEffectEvidence(res executionbroker.ExecutionResult) bool {
	return res.OK || res.Output.ArtifactPath != "" || res.Output.ArtifactHash != "" ||
		res.Output.MarkerFound || res.Output.BoundedOutput != "" ||
		res.Output.Progress != (executionbroker.SafeWorkerProgress{}) ||
		res.Verification != (executionbroker.SafeWorkerVerification{})
}

func safeExecutionState(op models.Operation) (map[string]json.RawMessage, error) {
	state := make(map[string]json.RawMessage)
	if raw := strings.TrimSpace(op.WorldModelStateJSON); raw != "" {
		if err := json.Unmarshal([]byte(raw), &state); err != nil || state == nil {
			return nil, fmt.Errorf("operation %s has invalid execution state; repair it before execution", op.ID)
		}
	}
	if raw, exists := state["safeWorkerExecution"]; exists {
		var prior safeWorkerExecutionEvidence
		if err := json.Unmarshal(raw, &prior); err != nil || string(raw) == "null" {
			return nil, fmt.Errorf("operation %s has invalid safe worker evidence; reconcile it before execution", op.ID)
		}
		if prior.OutcomeUncertain || (!safeWorkerCompleted(prior.Result) && !prior.BeforeEffect) ||
			(prior.BeforeEffect && safeWorkerHasEffectEvidence(prior.Result)) {
			return nil, ErrSafeOutcomeUncertain
		}
	}
	return state, nil
}

func transitionExecution(ctx context.Context, svc *operations.Service, claim *operations.ExecutionClaim, op models.Operation, to operations.OperationStatus, message string) (*models.Operation, error) {
	if claim == nil {
		return svc.Transition(op, to, "hai", "", message)
	}
	return svc.TransitionClaimed(ctx, *claim, op, to, "hai", "", message)
}

func safeExecutionPolicyError(op models.Operation) error {
	return safeExecutionPolicyErrorWithSourceApproval(op, false)
}

func safeExecutionPolicyErrorWithSourceApproval(op models.Operation, sourceApproved bool) error {
	if operations.IsSourceDerived(op) {
		if !sourceApproved || (operations.OperationStatus(op.Status) != operations.StatusApproved && operations.OperationStatus(op.Status) != operations.StatusRunning) {
		return fmt.Errorf("operation %s is source-derived; ordinary owner approval is not bound to its source revision and cannot authorize execution", op.ID)
		}
		return operations.Validate(op)
	}
	if operations.CurrentDecision(op.CurrentDecision) != operations.DecisionRunSafeLocalWorker {
		return fmt.Errorf("operation %s is not safe-executable (decision=%s); no real runtime is available in Phase 2A", op.ID, op.CurrentDecision)
	}
	if op.RequiresApproval {
		return fmt.Errorf("operation %s requires approval and cannot run in the safe local worker", op.ID)
	}
	if operations.RiskLevel(op.RiskLevel) != operations.RiskLow {
		return fmt.Errorf("operation %s has non-low risk %q and cannot run in the safe local worker", op.ID, op.RiskLevel)
	}
	if operations.AutonomyLevel(op.AutonomyLevel) != operations.AutonomyAuto {
		return fmt.Errorf("operation %s has non-automatic autonomy %q and cannot run in the safe local worker", op.ID, op.AutonomyLevel)
	}
	if operations.OwnerType(op.OwnerType) != operations.OwnerHAI {
		return fmt.Errorf("operation %s is owned by %q and cannot run in the safe local worker", op.ID, op.OwnerType)
	}
	return operations.Validate(op)
}
