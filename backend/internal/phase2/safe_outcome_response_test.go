package phase2

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/background"
	"automation-hub-backend/internal/executionbroker"
	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/operations"

	"github.com/google/uuid"
)

func phase2OutcomeReceipt() *executionbroker.ExecutionResult {
	return &executionbroker.ExecutionResult{
		RuntimeID: executionbroker.LocalSafeWorkerID, OK: true,
		Output: executionbroker.SafeWorkerOutput{
			ArtifactPath: "workspace/receipt.txt", ArtifactHash: "observed-hash",
			MarkerFound: true, BoundedOutput: "token=synthetic-receipt-secret",
		},
		Verification: executionbroker.SafeWorkerVerification{Passed: true, FileExists: true},
	}
}

func TestSafeOutcomeResponsePreservesSnapshotWithoutClaimingCompletion(t *testing.T) {
	for _, recorded := range []bool{false, true} {
		operation := &models.Operation{
			ID: uuid.New(), Status: string(operations.StatusRunning), VerificationStatus: string(operations.VerificationPending),
			LastError: "unexpected private storage diagnostic", Description: "token=synthetic-operation-secret",
			WorldModelStateJSON: `{"token":"synthetic-state-secret","number":9007199254740993}`,
		}
		if recorded {
			operation.Status = string(operations.StatusInterrupted)
		}
		outcome := background.SafeOutcome{Operation: operation, Receipt: phase2OutcomeReceipt(), Interrupted: recorded}
		cause := errors.Join(background.ErrSafeOutcomeUncertain, errors.New("unexpected private storage diagnostic"))
		response := safeOperationRunResponse(outcome, cause)
		raw, err := json.Marshal(response)
		if err != nil {
			t.Fatal(err)
		}
		for _, forbidden := range []string{"synthetic-receipt-secret", "synthetic-operation-secret", "synthetic-state-secret", "unexpected private storage diagnostic"} {
			if strings.Contains(string(raw), forbidden) {
				t.Fatalf("public outcome leaked %q", forbidden)
			}
		}
		if response["verified"] != false || response["failed"] != false || response["interrupted"] != recorded || response["outcomeRecorded"] != recorded || response["reconciliationRequired"] != true {
			t.Fatalf("partial receipt promoted to accepted/durable result: %s", raw)
		}
		receipt, ok := response["receipt"].(executionbroker.ExecutionResult)
		if !ok || receipt.Output.ArtifactHash != outcome.Receipt.Output.ArtifactHash || receipt.Verification != outcome.Receipt.Verification {
			t.Fatal("actual artifact evidence was lost")
		}
		var snapshot models.Operation
		if err := json.Unmarshal(response["operation"].(json.RawMessage), &snapshot); err != nil || snapshot.ID != operation.ID || snapshot.Status != operation.Status {
			t.Fatalf("operation snapshot discarded: %#v, %v", snapshot, err)
		}
		if !strings.Contains(snapshot.WorldModelStateJSON, "9007199254740993") || operation.LastError != "unexpected private storage diagnostic" || outcome.Receipt.Output.BoundedOutput != "token=synthetic-receipt-secret" {
			t.Fatal("projection changed original evidence or exact source numbers")
		}
	}
}

func TestSafeOutcomeResponseErrorOverridesContradictoryVerifiedFlag(t *testing.T) {
	outcome := background.SafeOutcome{
		Operation: &models.Operation{Status: string(operations.StatusCompleted), VerificationStatus: string(operations.VerificationPassed)},
		Receipt:   phase2OutcomeReceipt(), Verified: true,
	}
	response := safeOperationRunResponse(outcome, errors.New("completion persistence uncertain"))
	if response["verified"] != false || response["outcomeRecorded"] != false || response["reconciliationRequired"] != true {
		t.Fatalf("receipt or flag overrode an execution error: %#v", response)
	}
	response = safeOperationRunResponse(background.SafeOutcome{}, errors.New("unexpected private storage diagnostic"))
	if response["verified"] != false || response["outcomeRecorded"] != false || response["error"] != "safe execution could not be completed" {
		t.Fatalf("empty outcome fabricated completion or exposed error: %#v", response)
	}
}

func TestSafeOutcomeResponseOversizeRedactionPreservesCorrelationAndState(t *testing.T) {
	operation := &models.Operation{ID: uuid.New(), Status: string(operations.StatusInterrupted), VerificationStatus: string(operations.VerificationPending), Description: strings.Repeat("x", 2*1024*1024)}
	response := safeOperationRunResponse(background.SafeOutcome{Operation: operation, Receipt: phase2OutcomeReceipt(), Interrupted: true}, background.ErrSafeOutcomeUncertain)
	var snapshot models.Operation
	if err := json.Unmarshal(response["operation"].(json.RawMessage), &snapshot); err != nil || snapshot.ID != operation.ID || snapshot.Status != operation.Status || response["operationId"] != operation.ID.String() {
		t.Fatalf("discarded large payload also discarded correlation/state: %#v, %v", snapshot, err)
	}
	if snapshot.Description != "" || response["receipt"] == nil {
		t.Fatal("oversize redaction lost retained receipt or exposed large source payload")
	}
}

type phase2OutcomeFailureRepository struct {
	operations.Repository
	operations.ClaimRepository
	status       string
	releaseCalls int
}

func (r *phase2OutcomeFailureRepository) TransitionClaimed(ctx context.Context, claim operations.ExecutionClaim, operation models.Operation, event models.OperationEvent, release bool) (*models.Operation, error) {
	if operation.Status == r.status {
		return nil, errors.New("unexpected private storage diagnostic")
	}
	return r.ClaimRepository.TransitionClaimed(ctx, claim, operation, event, release)
}

func (r *phase2OutcomeFailureRepository) ReleaseClaim(ctx context.Context, claim operations.ExecutionClaim) error {
	r.releaseCalls++
	return r.ClaimRepository.ReleaseClaim(ctx, claim)
}

func TestRunOperationFinalizationFailureRetainsReceiptAndRecoveryFence(t *testing.T) {
	for _, status := range []operations.OperationStatus{operations.StatusVerifying, operations.StatusCompleted} {
		t.Run(string(status), func(t *testing.T) {
			module := newTestModule(t)
			base := operations.NewMemoryRepository()
			repository := &phase2OutcomeFailureRepository{Repository: base, ClaimRepository: base, status: string(status)}
			module.svc = operations.NewService(repository)
			router := newTestRouter(module, "local-operator", true)
			operation := createSafeExecutableOperation(t, module.svc)
			response := do(t, router, http.MethodPost, "/operations/"+operation.ID.String()+"/run")
			if response.Code != http.StatusConflict || strings.Contains(response.Body.String(), "unexpected private storage diagnostic") {
				t.Fatal("failed finalization returned success or exposed storage error")
			}
			var result struct {
				Receipt                *executionbroker.ExecutionResult `json:"receipt"`
				Verified               bool                             `json:"verified"`
				Interrupted            bool                             `json:"interrupted"`
				OutcomeRecorded        bool                             `json:"outcomeRecorded"`
				ReconciliationRequired bool                             `json:"reconciliationRequired"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if result.Receipt == nil || result.Receipt.Output.ArtifactHash == "" || result.Verified || result.Interrupted || result.OutcomeRecorded || !result.ReconciliationRequired || repository.releaseCalls != 0 {
				t.Fatalf("partial execution snapshot or recovery fence lost: %#v releases=%d", result, repository.releaseCalls)
			}
			recovery, err := module.svc.RecoverExpiredClaims(context.Background(), "local-operator", "local", 10)
			if err != nil || recovery.LiveRunning+recovery.LiveVerifying != 1 || recovery.UnleasedRunning+recovery.UnleasedVerifying != 0 {
				t.Fatalf("post-effect failure orphaned its recovery claim: %#v, %v", recovery, err)
			}
			if retry, err := module.svc.ClaimOperation(context.Background(), "local-operator", "local", operation.ID, uuid.New(), time.Minute); retry != nil || !errors.Is(err, operations.ErrOperationNotClaimable) {
				t.Fatalf("partial execution gained retry authority: %#v, %v", retry, err)
			}
		})
	}
}
