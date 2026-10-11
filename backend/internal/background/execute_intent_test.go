package background

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"automation-hub-backend/internal/executionbroker"
	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/operations"

	"github.com/google/uuid"
)

type intentInspectingExecutor struct {
	before func(executionbroker.SafeWorkerInput)
	result executionbroker.ExecutionResult
	err    error
	calls  int
}

func (e *intentInspectingExecutor) ExecuteLocalSafeWorker(_ context.Context, in executionbroker.SafeWorkerInput) (executionbroker.ExecutionResult, error) {
	e.calls++
	if e.before != nil {
		e.before(in)
	}
	return e.result, e.err
}

func storedSafeIntent(t *testing.T, service *operations.Service, op models.Operation) (*models.Operation, safeWorkerExecutionEvidence) {
	t.Helper()
	stored, err := service.Get(op.OwnerUserID, op.WorkspaceID, op.ID)
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]json.RawMessage
	if err := json.Unmarshal([]byte(stored.WorldModelStateJSON), &state); err != nil {
		t.Fatal(err)
	}
	var evidence safeWorkerExecutionEvidence
	if err := json.Unmarshal(state["safeWorkerExecution"], &evidence); err != nil {
		t.Fatal(err)
	}
	if evidence.Intent == nil {
		t.Fatal("attempt intent was not persisted")
	}
	if _, err := uuid.Parse(evidence.Intent.AttemptID); err != nil {
		t.Fatalf("invalid attempt identity: %v", err)
	}
	return stored, evidence
}

func TestSafeExecutionIntentCommittedBeforeDispatch(t *testing.T) {
	for _, claimed := range []bool{false, true} {
		t.Run(map[bool]string{false: "unclaimed", true: "claimed"}[claimed], func(t *testing.T) {
			service := operations.NewService(operations.NewMemoryRepository())
			op := createReadyRegressionOperation(t, service)
			op.WorldModelStateJSON = `{"policyRule":"keep","exact":9007199254740993}`
			saved, err := service.Save(op, "source_context", "test", "preserve context")
			if err != nil {
				t.Fatal(err)
			}
			op = *saved
			var claim *operations.ExecutionClaim
			if claimed {
				leased, err := service.ClaimOperation(context.Background(), op.OwnerUserID, op.WorkspaceID, op.ID, uuid.New(), operationClaimLease)
				if err != nil || leased == nil {
					t.Fatalf("claim: %#v, %v", leased, err)
				}
				op, claim = leased.Operation, &leased.Claim
			}
			var observed safeWorkerExecutionEvidence
			executor := &intentInspectingExecutor{result: verifiedOutcomeReceipt(), before: func(in executionbroker.SafeWorkerInput) {
				stored, evidence := storedSafeIntent(t, service, op)
				observed = evidence
				if stored.Status != string(operations.StatusRunning) || !evidence.OutcomeUncertain || evidence.BeforeEffect || evidence.Result.OK || safeWorkerHasEffectEvidence(evidence.Result) {
					t.Fatalf("pre-dispatch evidence fabricated a result: %#v, %#v", stored, evidence)
				}
				hash := sha256.Sum256([]byte(in.Marker))
				if evidence.Intent.ArtifactName != in.ArtifactName || evidence.Intent.MarkerHash != hex.EncodeToString(hash[:]) {
					t.Fatal("intent does not bind the dispatched input")
				}
				if claim != nil && evidence.Intent.ClaimGeneration != claim.Generation {
					t.Fatal("claim generation was not retained")
				}
				var state map[string]json.RawMessage
				if err := json.Unmarshal([]byte(stored.WorldModelStateJSON), &state); err != nil || string(state["exact"]) != "9007199254740993" || string(state["policyRule"]) != `"keep"` {
					t.Fatalf("intent erased or rounded context: %s, %v", stored.WorldModelStateJSON, err)
				}
			}}
			outcome, err := executeSafeOperation(context.Background(), service, executor, op, claim, time.Now())
			if err != nil || !outcome.Verified || executor.calls != 1 {
				t.Fatalf("outcome: %#v, %v; calls=%d", outcome, err, executor.calls)
			}
			_, final := storedSafeIntent(t, service, op)
			if final.OutcomeUncertain || final.Intent.AttemptID != observed.Intent.AttemptID || !safeWorkerCompleted(final.Result) {
				t.Fatal("verified result lost the same persisted attempt identity")
			}
		})
	}
}

func TestSafeExecutionIntentFailurePreventsDispatch(t *testing.T) {
	base := operations.NewMemoryRepository()
	persistErr := errors.New("running intent transaction refused")
	repository := &outcomeTransitionFailureRepository{Repository: base, ClaimRepository: base, status: string(operations.StatusRunning), err: persistErr}
	service := operations.NewService(repository)
	op := createReadyRegressionOperation(t, service)
	leased, err := service.ClaimOperation(context.Background(), op.OwnerUserID, op.WorkspaceID, op.ID, uuid.New(), operationClaimLease)
	if err != nil || leased == nil {
		t.Fatalf("claim: %#v, %v", leased, err)
	}
	executor := &intentInspectingExecutor{result: verifiedOutcomeReceipt()}
	outcome, err := executeSafeOperation(context.Background(), service, executor, leased.Operation, &leased.Claim, time.Now())
	if !errors.Is(err, persistErr) || executor.calls != 0 || outcome.Receipt != nil {
		t.Fatalf("failed intent dispatched: %#v, %v, calls=%d", outcome, err, executor.calls)
	}
	stored, err := service.Get(op.OwnerUserID, op.WorkspaceID, op.ID)
	if err != nil || stored.Status != string(operations.StatusReady) || stored.WorldModelStateJSON != op.WorldModelStateJSON {
		t.Fatalf("failed intent changed durable state: %#v, %v", stored, err)
	}
}

func TestSafeExecutionFirstResultWriteFailureRetainsIntentAcrossRestart(t *testing.T) {
	base := operations.NewMemoryRepository()
	persistErr := errors.New("first result transaction refused")
	repository := &outcomeTransitionFailureRepository{Repository: base, ClaimRepository: base, status: string(operations.StatusInterrupted), err: persistErr}
	service := operations.NewService(repository)
	op := createReadyRegressionOperation(t, service)
	leased, err := service.ClaimOperation(context.Background(), op.OwnerUserID, op.WorkspaceID, op.ID, uuid.New(), operationClaimLease)
	if err != nil || leased == nil {
		t.Fatalf("claim: %#v, %v", leased, err)
	}
	executor := &intentInspectingExecutor{result: verifiedOutcomeReceipt(), err: errors.New("post-dispatch uncertainty")}
	outcome, err := executeSafeOperation(context.Background(), service, executor, leased.Operation, &leased.Claim, time.Now())
	if !errors.Is(err, persistErr) || !errors.Is(err, ErrSafeOutcomeUncertain) || outcome.Receipt == nil || outcome.Verified || executor.calls != 1 {
		t.Fatalf("failed result write hid outcome: %#v, %v", outcome, err)
	}
	// Reconstruct the service over retained state, not the in-process result.
	restarted := operations.NewService(base)
	stored, evidence := storedSafeIntent(t, restarted, op)
	if stored.Status != string(operations.StatusRunning) || !evidence.OutcomeUncertain || safeWorkerHasEffectEvidence(evidence.Result) {
		t.Fatal("missing result was persisted as completion or absence of uncertainty")
	}
	if _, err := safeExecutionState(*stored); !errors.Is(err, ErrSafeOutcomeUncertain) {
		t.Fatalf("cold state lost the replay restriction: %v", err)
	}
	// Even a caller's stale or incorrectly reset status must not clear intent.
	stored.Status = string(operations.StatusReady)
	if _, err := executeSafeOperation(context.Background(), restarted, executor, *stored, nil, time.Now()); !errors.Is(err, ErrSafeOutcomeUncertain) || executor.calls != 1 {
		t.Fatalf("cold intent allowed replay: %v; calls=%d", err, executor.calls)
	}
}

func TestSafeExecutionCancellationBeforeDispatchDoesNotCallExecutor(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	executor := &intentInspectingExecutor{result: verifiedOutcomeReceipt()}
	_, err := executeSafeWorker(ctx, executor, executionbroker.SafeWorkerInput{})
	if !errors.Is(err, context.Canceled) || executor.calls != 0 {
		t.Fatalf("canceled dispatch called executor: %v; calls=%d", err, executor.calls)
	}
}

type intentCommitCancellationRepository struct {
	*operations.MemoryRepository
	cancel             context.CancelFunc
	running            *models.Operation
	finalizeContextErr error
	finalizeDeadline   bool
}

func (r *intentCommitCancellationRepository) TransitionClaimed(ctx context.Context, claim operations.ExecutionClaim, op models.Operation, event models.OperationEvent, release bool) (*models.Operation, error) {
	if op.Status == string(operations.StatusInterrupted) {
		r.finalizeContextErr = ctx.Err()
		_, r.finalizeDeadline = ctx.Deadline()
	}
	saved, err := r.MemoryRepository.TransitionClaimed(ctx, claim, op, event, release)
	if err == nil && op.Status == string(operations.StatusRunning) {
		r.running = saved
		r.cancel()
	}
	return saved, err
}

func TestSafeExecutionCancellationAfterIntentCommitRetainsReplayBarrier(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	repository := &intentCommitCancellationRepository{MemoryRepository: operations.NewMemoryRepository(), cancel: cancel}
	service := operations.NewService(repository)
	op := createReadyRegressionOperation(t, service)
	leased, err := service.ClaimOperation(ctx, op.OwnerUserID, op.WorkspaceID, op.ID, uuid.New(), operationClaimLease)
	if err != nil || leased == nil {
		t.Fatalf("claim: %#v, %v", leased, err)
	}
	executor := &intentInspectingExecutor{result: verifiedOutcomeReceipt()}
	outcome, err := executeSafeOperation(ctx, service, executor, leased.Operation, &leased.Claim, time.Now())
	if !errors.Is(err, context.Canceled) || !errors.Is(err, ErrSafeOutcomeUncertain) || !outcome.Interrupted || outcome.Verified || outcome.Failed || executor.calls != 0 {
		t.Fatalf("cancellation after intent commit: %#v, %v; calls=%d", outcome, err, executor.calls)
	}
	if repository.running == nil || repository.finalizeContextErr != nil || !repository.finalizeDeadline {
		t.Fatalf("interruption did not use a live bounded context: running=%#v, err=%v, deadline=%v", repository.running, repository.finalizeContextErr, repository.finalizeDeadline)
	}
	var runningState map[string]json.RawMessage
	if err := json.Unmarshal([]byte(repository.running.WorldModelStateJSON), &runningState); err != nil {
		t.Fatal(err)
	}
	var initial safeWorkerExecutionEvidence
	if err := json.Unmarshal(runningState["safeWorkerExecution"], &initial); err != nil || initial.Intent == nil || !initial.OutcomeUncertain {
		t.Fatalf("cancellation preceded a persisted uncertain intent: %#v, %v", initial, err)
	}
	stored, evidence := storedSafeIntent(t, service, op)
	if stored.Status != string(operations.StatusInterrupted) || stored.CompletedAt != nil || !evidence.OutcomeUncertain || evidence.BeforeEffect || safeWorkerHasEffectEvidence(evidence.Result) {
		t.Fatalf("canceled intent fabricated completion or retry permission: %#v, %#v", stored, evidence)
	}
	if *evidence.Intent != *initial.Intent || evidence.Intent.ClaimGeneration != leased.Claim.Generation {
		t.Fatal("cancellation lost the committed attempt identity or claim generation")
	}
	if err := service.RenewClaim(context.Background(), leased.Claim, operationClaimLease); !errors.Is(err, operations.ErrClaimLost) {
		t.Fatalf("interruption did not release the original claim: %v", err)
	}
	stored.Status = string(operations.StatusReady)
	if _, err := executeSafeOperation(context.Background(), service, executor, *stored, nil, time.Now()); !errors.Is(err, ErrSafeOutcomeUncertain) || executor.calls != 0 {
		t.Fatalf("canceled intent allowed replay: %v; calls=%d", err, executor.calls)
	}
}
