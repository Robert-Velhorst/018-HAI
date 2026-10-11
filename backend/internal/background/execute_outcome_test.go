package background

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/executionbroker"
	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/operations"

	"github.com/google/uuid"
)

type outcomeSafeExecutor struct {
	result executionbroker.ExecutionResult
	err    error
	calls  int
}

func (e *outcomeSafeExecutor) ExecuteLocalSafeWorker(context.Context, executionbroker.SafeWorkerInput) (executionbroker.ExecutionResult, error) {
	e.calls++
	return e.result, e.err
}

func verifiedOutcomeReceipt() executionbroker.ExecutionResult {
	return executionbroker.ExecutionResult{
		RuntimeID: executionbroker.LocalSafeWorkerID,
		OK:        true,
		Output: executionbroker.SafeWorkerOutput{
			ArtifactPath: "workspace/receipt.txt", ArtifactHash: "receipt-hash",
			MarkerFound: true, BoundedOutput: "partial receipt",
		},
		Verification: executionbroker.SafeWorkerVerification{
			FileExists: true, InsideWorkspace: true, HashMatches: true,
			MarkerFound: true, OutputBounded: true, Passed: true,
		},
	}
}

func TestSourceDerivedOwnerApprovalDoesNotAuthorizeWorkerDispatch(t *testing.T) {
	service := operations.NewService(operations.NewMemoryRepository())
	feedID := uuid.New()
	created, err := service.Ingest(operations.NewOperationInput{
		OwnerUserID: "user-1", WorkspaceID: "local", Title: "Review imported message",
		OperationType: "review_message", SourceType: "account_feed", AccountFeedID: &feedID,
		SourceProvider: "mail", SourceAccount: "owner-account", SourceExternalID: "message-1",
		SourceRevisionHash: "revision-1", DedupeKey: uuid.NewString(),
	})
	if err != nil {
		t.Fatal(err)
	}
	op := created.Operation
	op.RiskLevel = string(operations.RiskLow)
	op.AutonomyLevel = string(operations.AutonomyAuto)
	op.OwnerType = string(operations.OwnerHAI)
	op.CurrentDecision = string(operations.DecisionRunSafeLocalWorker)
	op.VerificationStatus = string(operations.VerificationPending)
	classified, err := service.Transition(op, operations.StatusClassified, "hai", "", "classified")
	if err != nil {
		t.Fatal(err)
	}
	awaiting, err := service.Transition(*classified, operations.StatusAwaitingApproval, "hai", "", "held for owner review")
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		op   models.Operation
	}{
		{name: "unapproved", op: *awaiting},
	}
	approvedInput := *awaiting
	approvedInput.OwnerType = string(operations.OwnerRobert)
	approved, err := service.Transition(approvedInput, operations.StatusApproved, string(operations.OwnerRobert), "user-1", "approved by operator")
	if err != nil {
		t.Fatal(err)
	}
	if approved.ApprovalID != nil {
		t.Fatalf("ordinary approval unexpectedly created a revision-bound acceptance: %s", *approved.ApprovalID)
	}
	events, err := service.Events(approved.ID)
	if err != nil {
		t.Fatal(err)
	}
	last := events[len(events)-1]
	if last.ActorType != string(operations.OwnerRobert) || last.ActorID != "user-1" || last.AfterStatus != string(operations.StatusApproved) || last.PayloadJSON != "{}" {
		t.Fatalf("unexpected ordinary approval audit event: %#v", last)
	}
	tests = append(tests, struct {
		name string
		op   models.Operation
	}{name: "approved_without_source_binding", op: *approved})

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			executor := &outcomeSafeExecutor{result: verifiedOutcomeReceipt()}
			outcome, err := executeSafeOperation(context.Background(), service, executor, test.op, nil, time.Now().UTC())
			if err == nil || executor.calls != 0 || outcome.Receipt != nil || outcome.Verified {
				t.Fatalf("source-derived operation reached worker dispatch: outcome=%#v err=%v calls=%d", outcome, err, executor.calls)
			}
			if !strings.Contains(err.Error(), "ordinary owner approval is not bound to its source revision") {
				t.Fatalf("refusal did not explain the unsupported acceptance flow: %v", err)
			}
		})
	}
}

func TestSafeExecutionUncertainOutcomeCannotCompleteOrRetry(t *testing.T) {
	lateErr := errors.New("post-dispatch error")
	tests := []struct {
		name   string
		result executionbroker.ExecutionResult
		err    error
	}{
		{"receipt and error", verifiedOutcomeReceipt(), lateErr},
		{"missing result without error", executionbroker.ExecutionResult{}, nil},
		{"missing result and error", executionbroker.ExecutionResult{}, lateErr},
		{"receipt contradicts authorization refusal", verifiedOutcomeReceipt(), executionbroker.ErrAuthorizationDenied},
		{"consumed authority contradicts refusal", executionbroker.ExecutionResult{Output: executionbroker.SafeWorkerOutput{Progress: executionbroker.SafeWorkerProgress{Authorization: "consumed"}}}, executionbroker.ErrAuthorizationDenied},
		{"unknown consumption contradicts refusal", executionbroker.ExecutionResult{Output: executionbroker.SafeWorkerOutput{Progress: executionbroker.SafeWorkerProgress{Authorization: "unknown"}}}, executionbroker.ErrAuthorizationDenied},
		{"entered filesystem effect contradicts refusal", executionbroker.ExecutionResult{Output: executionbroker.SafeWorkerOutput{Progress: executionbroker.SafeWorkerProgress{EffectStarted: true}}}, executionbroker.ErrAuthorizationMismatch},
		{"passed flags contradict partial progress", func() executionbroker.ExecutionResult {
			r := verifiedOutcomeReceipt()
			r.Output.Progress.Authorization = "unknown"
			return r
		}(), nil},
		{"joined refusal and late error", executionbroker.ExecutionResult{}, errors.Join(executionbroker.ErrAuthorizationDenied, lateErr)},
		{"verification failed after execution", func() executionbroker.ExecutionResult {
			r := verifiedOutcomeReceipt()
			r.OK, r.Verification.Passed, r.Verification.HashMatches = false, false, false
			return r
		}(), nil},
		{"contradictory passed flag", func() executionbroker.ExecutionResult {
			r := verifiedOutcomeReceipt()
			r.Verification.HashMatches = false
			return r
		}(), nil},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := operations.NewService(operations.NewMemoryRepository())
			op := createReadyRegressionOperation(t, service)
			op.EvidenceJSON = `{"source":{"reference":"preserve-me"},"number":9007199254740993}`
			op.WorldModelStateJSON = `{"policyRule":"preserve-rule","number":9007199254740993}`
			saved, err := service.Save(op, "test_source_evidence", "test", "retain original evidence")
			if err != nil {
				t.Fatal(err)
			}
			claimed, err := service.ClaimOperation(context.Background(), "user-1", "local", op.ID, uuid.New(), operationClaimLease)
			if err != nil || claimed == nil {
				t.Fatalf("claim: %#v, %v", claimed, err)
			}
			executor := &outcomeSafeExecutor{result: test.result, err: test.err}
			outcome, err := executeSafeOperation(context.Background(), service, executor, *saved, &claimed.Claim, time.Now().UTC())
			if err == nil || !outcome.Interrupted || outcome.Verified || outcome.Failed || outcome.Operation == nil || outcome.Receipt == nil {
				t.Fatalf("outcome=%#v err=%v; want retained interrupted receipt", outcome, err)
			}
			if test.err != nil && !errors.Is(err, test.err) {
				t.Fatalf("execution error was lost: %v", err)
			}
			if outcome.Receipt.Output != test.result.Output || outcome.Receipt.Verification != test.result.Verification {
				t.Fatal("partial receipt changed")
			}
			stored, err := service.Get("user-1", "local", op.ID)
			if err != nil {
				t.Fatal(err)
			}
			if stored.Status != string(operations.StatusInterrupted) || stored.CompletedAt != nil || stored.VerificationStatus == string(operations.VerificationPassed) {
				t.Fatalf("uncertain operation completed: %#v", stored)
			}
			var evidence map[string]json.RawMessage
			if err := json.Unmarshal([]byte(stored.EvidenceJSON), &evidence); err != nil {
				t.Fatal(err)
			}
			if string(evidence["source"]) != `{"reference":"preserve-me"}` || string(evidence["number"]) != "9007199254740993" {
				t.Fatalf("original source evidence changed: %s", stored.EvidenceJSON)
			}
			var state map[string]json.RawMessage
			if err := json.Unmarshal([]byte(stored.WorldModelStateJSON), &state); err != nil {
				t.Fatal(err)
			}
			if string(state["policyRule"]) != `"preserve-rule"` || string(state["number"]) != "9007199254740993" {
				t.Fatalf("original execution state changed: %s", stored.WorldModelStateJSON)
			}
			var receipt struct {
				Result           executionbroker.ExecutionResult `json:"result"`
				OutcomeUncertain bool                            `json:"outcomeUncertain"`
			}
			if err := json.Unmarshal(state["safeWorkerExecution"], &receipt); err != nil || !receipt.OutcomeUncertain || receipt.Result.Output.ArtifactHash != test.result.Output.ArtifactHash {
				t.Fatalf("durable receipt missing: %s, %v", stored.WorldModelStateJSON, err)
			}
			// Re-ingestion may replace source evidence, but must not erase receipts.
			refreshed, err := service.Ingest(operations.NewOperationInput{
				OwnerUserID: "user-1", WorkspaceID: "local", DedupeKey: op.DedupeKey,
				Title: stored.Title, OperationType: stored.OperationType, SourceType: stored.SourceType,
				EvidenceJSON: `{"source":"refreshed"}`,
			})
			if err != nil || refreshed.Operation.WorldModelStateJSON != stored.WorldModelStateJSON {
				t.Fatalf("re-ingestion lost runtime receipt: %#v, %v", refreshed, err)
			}
			if retry, err := service.ClaimOperation(context.Background(), "user-1", "local", op.ID, uuid.New(), operationClaimLease); !errors.Is(err, operations.ErrOperationNotClaimable) || retry != nil {
				t.Fatalf("uncertain operation is retryable: %#v, %v", retry, err)
			}
			if retry, err := service.ClaimNext(context.Background(), "user-1", "local", uuid.New(), operationClaimLease); err != nil || retry != nil {
				t.Fatalf("uncertain operation auto-claimed: %#v, %v", retry, err)
			}
			for _, status := range []operations.OperationStatus{operations.StatusReady, operations.StatusClassified, operations.StatusApproved} {
				stored.Status = string(status)
				if _, err := executeSafeOperation(context.Background(), service, executor, *stored, nil, time.Now().UTC()); !errors.Is(err, ErrSafeOutcomeUncertain) || executor.calls != 1 {
					t.Fatalf("prior uncertain evidence permitted %s dispatch: calls=%d err=%v", status, executor.calls, err)
				}
			}
		})
	}
}

func TestSafeExecutionAuthorizationRefusalPreservesDeterministicRetry(t *testing.T) {
	for _, refusal := range []error{
		executionbroker.ErrAuthorizationRequired,
		executionbroker.ErrAuthorizationDenied,
		fmt.Errorf("wrapped: %w", executionbroker.ErrAuthorizationMismatch),
	} {
		t.Run(refusal.Error(), func(t *testing.T) {
			service := operations.NewService(operations.NewMemoryRepository())
			op := createReadyRegressionOperation(t, service)
			executor := &outcomeSafeExecutor{result: executionbroker.ExecutionResult{RuntimeID: executionbroker.LocalSafeWorkerID}, err: refusal}
			outcome, err := executeSafeOperation(context.Background(), service, executor, op, nil, time.Now().UTC())
			if err != nil || !outcome.Failed || outcome.Interrupted || outcome.Verified {
				t.Fatalf("pre-effect refusal treated as uncertain: %#v, %v", outcome, err)
			}
			claimed, err := service.ClaimOperation(context.Background(), "user-1", "local", op.ID, uuid.New(), operationClaimLease)
			if err != nil || claimed == nil {
				t.Fatalf("deterministic explicit retry rejected: %#v, %v", claimed, err)
			}
			executor.result, executor.err = verifiedOutcomeReceipt(), nil
			outcome, err = executeSafeOperation(context.Background(), service, executor, claimed.Operation, &claimed.Claim, time.Now().UTC())
			if err != nil || !outcome.Verified || outcome.Interrupted || executor.calls != 2 {
				t.Fatalf("deterministic retry failed: %#v, %v, calls=%d", outcome, err, executor.calls)
			}
		})
	}
}

type outcomeTransitionFailureRepository struct {
	operations.Repository
	operations.ClaimRepository
	status string
	err    error
}

func (r *outcomeTransitionFailureRepository) TransitionClaimed(ctx context.Context, claim operations.ExecutionClaim, op models.Operation, event models.OperationEvent, release bool) (*models.Operation, error) {
	if op.Status == r.status {
		return nil, r.err
	}
	return r.ClaimRepository.TransitionClaimed(ctx, claim, op, event, release)
}

func TestSafeExecutionFinalizationFailureRetainsRealReceipt(t *testing.T) {
	for _, status := range []operations.OperationStatus{operations.StatusVerifying, operations.StatusCompleted} {
		t.Run(string(status), func(t *testing.T) {
			base := operations.NewMemoryRepository()
			injected := errors.New("finalization persistence failed")
			repository := &outcomeTransitionFailureRepository{Repository: base, ClaimRepository: base, status: string(status), err: injected}
			service := operations.NewService(repository)
			op := createReadyRegressionOperation(t, service)
			claimed, err := service.ClaimOperation(context.Background(), "user-1", "local", op.ID, uuid.New(), operationClaimLease)
			if err != nil || claimed == nil {
				t.Fatalf("claim: %#v, %v", claimed, err)
			}
			broker := newAuthorizedBackgroundTestBroker(t, t.TempDir(), "user-1", "local")
			outcome, err := ExecuteSafeOperationClaimed(context.Background(), service, broker, claimed.Operation, claimed.Claim, time.Now().UTC(), allowSyntheticSafePolicy)
			if !errors.Is(err, injected) || outcome.Verified || outcome.Receipt == nil || outcome.Operation == nil || outcome.Receipt.Output.ArtifactHash == "" {
				t.Fatalf("real receipt lost on persistence failure: %#v, %v", outcome, err)
			}
			if v := broker.SafeWorker().Verify(safePayload(op), outcome.Receipt.Output); !v.Passed {
				t.Fatalf("test did not create a real verified artifact: %#v", v)
			}
			stored, err := service.Get("user-1", "local", op.ID)
			if err != nil || stored.Status == string(operations.StatusCompleted) {
				t.Fatalf("unpersisted completion recorded: %#v, %v", stored, err)
			}
			if retry, err := service.ClaimOperation(context.Background(), "user-1", "local", op.ID, uuid.New(), operationClaimLease); !errors.Is(err, operations.ErrOperationNotClaimable) || retry != nil {
				t.Fatalf("unfinalized execution allowed retry: %#v, %v", retry, err)
			}
		})
	}
}

func TestSafeExecutionPriorReceiptFailsClosedBeforeDispatch(t *testing.T) {
	tests := []struct {
		name     string
		evidence string
	}{
		{"malformed execution state", `{`},
		{"non-object execution state", `[]`},
		{"null execution state", `null`},
		{"null receipt", `{"safeWorkerExecution":null}`},
		{"missing receipt proof", `{"safeWorkerExecution":{}}`},
		{"false certainty on failed receipt", `{"safeWorkerExecution":{"outcomeUncertain":false,"result":{"ok":false}}}`},
		{"contradictory pre-effect proof", `{"safeWorkerExecution":{"beforeEffect":true,"result":{"output":{"artifactHash":"partial"}}}}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := operations.NewService(operations.NewMemoryRepository())
			op := createReadyRegressionOperation(t, service)
			op.WorldModelStateJSON = test.evidence
			executor := &outcomeSafeExecutor{result: verifiedOutcomeReceipt()}
			if _, err := executeSafeOperation(context.Background(), service, executor, op, nil, time.Now().UTC()); err == nil || executor.calls != 0 {
				t.Fatalf("invalid prior evidence dispatched: calls=%d err=%v", executor.calls, err)
			}
			stored, err := service.Get("user-1", "local", op.ID)
			if err != nil || stored.Status != string(operations.StatusReady) {
				t.Fatalf("invalid evidence advanced operation: %#v, %v", stored, err)
			}
		})
	}
}

func TestSafeExecutionUncertainPersistenceFailureRetainsReceiptAndErrors(t *testing.T) {
	base := operations.NewMemoryRepository()
	persistErr := errors.New("interruption persistence failed")
	execErr := errors.New("post-dispatch failure")
	repository := &outcomeTransitionFailureRepository{Repository: base, ClaimRepository: base, status: string(operations.StatusInterrupted), err: persistErr}
	service := operations.NewService(repository)
	op := createReadyRegressionOperation(t, service)
	claimed, err := service.ClaimOperation(context.Background(), "user-1", "local", op.ID, uuid.New(), operationClaimLease)
	if err != nil || claimed == nil {
		t.Fatalf("claim: %#v, %v", claimed, err)
	}
	executor := &outcomeSafeExecutor{result: verifiedOutcomeReceipt(), err: execErr}
	outcome, err := executeSafeOperation(context.Background(), service, executor, claimed.Operation, &claimed.Claim, time.Now().UTC())
	if !errors.Is(err, persistErr) || !errors.Is(err, execErr) || !errors.Is(err, ErrSafeOutcomeUncertain) || outcome.Receipt == nil || outcome.Operation == nil || outcome.Verified || outcome.Interrupted {
		t.Fatalf("failed persistence hid receipt/errors or claimed recovery: %#v, %v", outcome, err)
	}
	stored, err := service.Get("user-1", "local", op.ID)
	if err != nil || stored.Status != string(operations.StatusRunning) {
		t.Fatalf("failed interruption changed durable state: %#v, %v", stored, err)
	}
	if retry, err := service.ClaimNext(context.Background(), "user-1", "local", uuid.New(), operationClaimLease); err != nil || retry != nil {
		t.Fatalf("unfinalized execution auto-claimed: %#v, %v", retry, err)
	}
}

func TestSafeExecutionUnconfiguredWorkerCanRetryAfterConfigurationRepair(t *testing.T) {
	service := operations.NewService(operations.NewMemoryRepository())
	op := createReadyRegressionOperation(t, service)
	broker := newAuthorizedBackgroundTestBroker(t, "", "user-1", "local")
	firstClaim, err := service.ClaimOperation(context.Background(), "user-1", "local", op.ID, uuid.New(), operationClaimLease)
	if err != nil || firstClaim == nil {
		t.Fatalf("initial claim: %#v, %v", firstClaim, err)
	}
	outcome, err := ExecuteSafeOperationClaimed(context.Background(), service, broker, firstClaim.Operation, firstClaim.Claim, time.Now().UTC(), allowSyntheticSafePolicy)
	if err != nil || !outcome.Failed || outcome.Interrupted {
		t.Fatalf("unconfigured pre-effect refusal treated as uncertain: %#v, %v", outcome, err)
	}
	claimed, err := service.ClaimOperation(context.Background(), "user-1", "local", op.ID, uuid.New(), operationClaimLease)
	if err != nil || claimed == nil {
		t.Fatalf("configuration repair retry rejected: %#v, %v", claimed, err)
	}
	broker = newAuthorizedBackgroundTestBroker(t, t.TempDir(), "user-1", "local")
	outcome, err = ExecuteSafeOperationClaimed(context.Background(), service, broker, claimed.Operation, claimed.Claim, time.Now().UTC(), allowSyntheticSafePolicy)
	if err != nil || !outcome.Verified || outcome.Interrupted {
		t.Fatalf("configuration repair retry failed: %#v, %v", outcome, err)
	}
}
