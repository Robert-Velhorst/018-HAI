package runtimelab

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/background"
	"automation-hub-backend/internal/executionbroker"
	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/operations"

	"github.com/google/uuid"
)

func labOutcomeReceipt() *executionbroker.ExecutionResult {
	return &executionbroker.ExecutionResult{
		RuntimeID: executionbroker.LocalSafeWorkerID, OK: true,
		Output: executionbroker.SafeWorkerOutput{
			ArtifactPath: "workspace/receipt.txt", ArtifactHash: "observed-hash", MarkerFound: true,
			BoundedOutput: "token=synthetic-lab-receipt-secret",
		},
		Verification: executionbroker.SafeWorkerVerification{Passed: true, FileExists: true},
	}
}

func TestSafeOutcomeAttemptPreservesReceiptWithoutPromotingPartialCompletion(t *testing.T) {
	for _, recorded := range []bool{false, true} {
		operation := &models.Operation{ID: uuid.New(), Status: string(operations.StatusRunning), VerificationStatus: string(operations.VerificationPending)}
		if recorded {
			operation.Status = string(operations.StatusInterrupted)
		}
		outcome := background.SafeOutcome{Operation: operation, Receipt: labOutcomeReceipt(), Interrupted: recorded}
		attempt := runtimeAttemptFromSafeOutcome(operation.ID.String(), outcome, errors.Join(background.ErrSafeOutcomeUncertain, errors.New("unexpected private storage diagnostic")), time.Now().UTC())
		if attempt.Status != AttemptInconclusive || attempt.VerificationPassed || !attempt.ReconciliationRequired || attempt.Interrupted != recorded || attempt.OutcomeRecorded != recorded {
			t.Fatalf("partial outcome promoted to accepted completion: %#v", attempt)
		}
		if attempt.Receipt == nil || attempt.Receipt.Output.ArtifactHash != outcome.Receipt.Output.ArtifactHash || attempt.Receipt.Verification != outcome.Receipt.Verification || attempt.OperationStatus != operation.Status {
			t.Fatal("partial receipt or operation status was lost")
		}
		if strings.Contains(attempt.Detail, "unexpected private storage diagnostic") || strings.Contains(attempt.BoundedOutput, "synthetic-lab-receipt-secret") || strings.Contains(attempt.Receipt.Output.BoundedOutput, "synthetic-lab-receipt-secret") {
			t.Fatal("public attempt leaked execution/storage diagnostics or synthetic secret")
		}
		if outcome.Receipt.Output.BoundedOutput != "token=synthetic-lab-receipt-secret" {
			t.Fatal("public attempt mutated its source receipt")
		}
	}
}

func TestSafeOutcomeAttemptErrorOverridesContradictoryVerifiedFlag(t *testing.T) {
	operation := &models.Operation{ID: uuid.New(), Status: string(operations.StatusCompleted), VerificationStatus: string(operations.VerificationPassed)}
	outcome := background.SafeOutcome{Operation: operation, Receipt: labOutcomeReceipt(), Verified: true}
	attempt := runtimeAttemptFromSafeOutcome(operation.ID.String(), outcome, errors.New("completion persistence uncertain"), time.Now().UTC())
	if attempt.Status != AttemptInconclusive || attempt.VerificationPassed || attempt.OutcomeRecorded || !attempt.ReconciliationRequired {
		t.Fatalf("contradictory verified flag overrode error: %#v", attempt)
	}
	attempt = runtimeAttemptFromSafeOutcome(operation.ID.String(), background.SafeOutcome{}, nil, time.Now().UTC())
	if attempt.Status != AttemptInconclusive || attempt.VerificationPassed || attempt.Receipt != nil || attempt.OutcomeRecorded {
		t.Fatalf("missing outcome became accepted/retryable proof: %#v", attempt)
	}
}

func TestSafeOutcomeAttemptRecoveryPreservesInterruptedAndContradictoryReceipts(t *testing.T) {
	for _, status := range []operations.OperationStatus{operations.StatusInterrupted, operations.StatusCompleted} {
		state, err := json.Marshal(map[string]any{
			"safeWorkerExecution": map[string]any{"result": labOutcomeReceipt(), "outcomeUncertain": true},
		})
		if err != nil {
			t.Fatal(err)
		}
		operation := models.Operation{
			ID: uuid.New(), Status: string(status), VerificationStatus: string(operations.VerificationPassed),
			WorldModelStateJSON: string(state), LastError: "unexpected private storage diagnostic",
		}
		attempt := runtimeAttemptFromOperation(operation)
		if attempt.Status != AttemptInconclusive || attempt.VerificationPassed || !attempt.ReconciliationRequired || !attempt.OutcomeRecorded || attempt.Interrupted != (status == operations.StatusInterrupted) {
			t.Fatalf("recovered uncertain evidence became completed: %#v", attempt)
		}
		if attempt.Receipt == nil || attempt.Receipt.Output.ArtifactHash != "observed-hash" || strings.Contains(attempt.Receipt.Output.BoundedOutput, "synthetic-lab-receipt-secret") || strings.Contains(attempt.Detail, "unexpected private storage diagnostic") {
			t.Fatal("recovered receipt was discarded or exposed diagnostics")
		}
	}
	for _, state := range []string{"null", "{", `{"safeWorkerExecution":null}`, `{"safeWorkerExecution":{}}`} {
		attempt := runtimeAttemptFromOperation(models.Operation{Status: string(operations.StatusCompleted), VerificationStatus: string(operations.VerificationPassed), WorldModelStateJSON: state})
		if attempt.Status != AttemptInconclusive || attempt.VerificationPassed || !attempt.ReconciliationRequired {
			t.Fatalf("malformed/missing execution evidence accepted: %q, %#v", state, attempt)
		}
	}
	for _, status := range []operations.OperationStatus{operations.StatusCompleted, operations.StatusAwaitingApproval} {
		state, err := json.Marshal(map[string]any{"safeWorkerExecution": map[string]any{"result": labOutcomeReceipt(), "outcomeUncertain": false}})
		if err != nil {
			t.Fatal(err)
		}
		attempt := runtimeAttemptFromOperation(models.Operation{Status: string(status), VerificationStatus: string(operations.VerificationPassed), WorldModelStateJSON: string(state)})
		if attempt.Status != AttemptInconclusive || attempt.VerificationPassed || !attempt.ReconciliationRequired || attempt.Receipt == nil {
			t.Fatalf("incomplete/review-required receipt became successful: %#v", attempt)
		}
	}
}

func TestSafeOutcomeAttemptSnapshotsCannotMutateRetainedReceipts(t *testing.T) {
	service := &Service{}
	original := service.record(RuntimeAttempt{RuntimeID: executionbroker.LocalSafeWorkerID, Receipt: labOutcomeReceipt(), CreatedAt: time.Now().UTC()})
	original.Receipt.Output.ArtifactHash = "caller-mutated"
	first := service.Attempts(executionbroker.LocalSafeWorkerID)
	if len(first) != 1 || first[0].Receipt.Output.ArtifactHash != "observed-hash" {
		t.Fatal("record returned an alias to retained receipt")
	}
	first[0].Receipt.Output.ArtifactHash = "list-mutated"
	second := service.Attempts(executionbroker.LocalSafeWorkerID)
	if len(second) != 1 || second[0].Receipt.Output.ArtifactHash != "observed-hash" {
		t.Fatal("attempt list returned an alias to retained receipt")
	}
}

type labOutcomeFailureRepository struct {
	operations.Repository
	operations.ClaimRepository
	status       string
	releaseCalls int
}

func (r *labOutcomeFailureRepository) TransitionClaimed(ctx context.Context, claim operations.ExecutionClaim, operation models.Operation, event models.OperationEvent, release bool) (*models.Operation, error) {
	if operation.Status == r.status {
		return nil, errors.New("unexpected private storage diagnostic")
	}
	return r.ClaimRepository.TransitionClaimed(ctx, claim, operation, event, release)
}

func (r *labOutcomeFailureRepository) ReleaseClaim(ctx context.Context, claim operations.ExecutionClaim) error {
	r.releaseCalls++
	return r.ClaimRepository.ReleaseClaim(ctx, claim)
}

func TestSafeWorkerSelfTestFinalizationFailureRetainsReceiptAndRecoveryFence(t *testing.T) {
	for _, status := range []operations.OperationStatus{operations.StatusVerifying, operations.StatusCompleted} {
		t.Run(string(status), func(t *testing.T) {
			base := operations.NewMemoryRepository()
			repository := &labOutcomeFailureRepository{Repository: base, ClaimRepository: base, status: string(status)}
			service := NewService(newAuthorizedRuntimeLabTestBroker(t, t.TempDir(), "local-operator", "local"), operations.NewService(repository), "local-operator", "local").WithSafeExecutionPolicy(allowRuntimeLabSafeExecution)
			attempt, found := service.SelfTest(context.Background(), executionbroker.LocalSafeWorkerID)
			if !found || attempt.Status != AttemptInconclusive || attempt.Receipt == nil || attempt.Receipt.Output.ArtifactHash == "" || attempt.VerificationPassed || attempt.Interrupted || attempt.OutcomeRecorded || !attempt.ReconciliationRequired || repository.releaseCalls != 0 {
				t.Fatalf("self-test lost receipt or claimed terminal persistence: %#v, releases=%d", attempt, repository.releaseCalls)
			}
			if strings.Contains(attempt.Detail, "unexpected private storage diagnostic") {
				t.Fatal("self-test exposed storage error")
			}
			retained := service.Attempts(executionbroker.LocalSafeWorkerID)
			if len(retained) != 1 || retained[0].Receipt == nil || retained[0].Receipt.Output.ArtifactHash != attempt.Receipt.Output.ArtifactHash {
				t.Fatal("in-memory attempt store discarded partial receipt")
			}
			recovery, err := service.ops.RecoverExpiredClaims(context.Background(), "local-operator", "local", 10)
			if err != nil || recovery.LiveRunning+recovery.LiveVerifying != 1 || recovery.UnleasedRunning+recovery.UnleasedVerifying != 0 {
				t.Fatalf("self-test orphaned post-effect recovery: %#v, %v", recovery, err)
			}
			operationID, err := uuid.Parse(attempt.OperationID)
			if err != nil {
				t.Fatal(err)
			}
			if retry, err := service.ops.ClaimOperation(context.Background(), "local-operator", "local", operationID, uuid.New(), time.Minute); retry != nil || !errors.Is(err, operations.ErrOperationNotClaimable) {
				t.Fatalf("partial self-test gained retry authority: %#v, %v", retry, err)
			}
		})
	}
}
