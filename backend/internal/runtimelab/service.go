package runtimelab

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"automation-hub-backend/internal/agentruntime"
	"automation-hub-backend/internal/background"
	"automation-hub-backend/internal/executionbroker"
	"automation-hub-backend/internal/idempotency"
	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/operations"
	"automation-hub-backend/internal/safety"

	"github.com/google/uuid"
)

// RuntimeAttempt is an auditable record of a runtime execution/self-test attempt
// (§10.9). It is only ever created from a real attempt — a setup_required record
// truthfully means nothing executed.
type RuntimeAttempt struct {
	ID                     string                           `json:"id"`
	RuntimeID              string                           `json:"runtimeId"`
	OperationID            string                           `json:"operationId,omitempty"`
	Status                 RuntimeAttemptStatus             `json:"status"`
	IdempotencyKey         string                           `json:"idempotencyKey,omitempty"`
	Detail                 string                           `json:"detail"`
	BoundedOutput          string                           `json:"boundedOutput,omitempty"`
	VerificationPassed     bool                             `json:"verificationPassed"`
	DiscoveryRecovered     bool                             `json:"discoveryRecovered"`
	CreatedAt              time.Time                        `json:"createdAt"`
	Receipt                *executionbroker.ExecutionResult `json:"receipt,omitempty"`
	OperationStatus        string                           `json:"operationStatus,omitempty"`
	Interrupted            bool                             `json:"interrupted"`
	ReconciliationRequired bool                             `json:"reconciliationRequired"`
	OutcomeRecorded        bool                             `json:"outcomeRecorded"` // repository transition, not durable-storage proof
}

// RuntimeSummary is a runtime's truthful status for the overview.
type RuntimeSummary struct {
	Info              RuntimeInfo                   `json:"info"`
	Status            executionbroker.RuntimeStatus `json:"status"`
	ClaimLevel        executionbroker.ClaimLevel    `json:"claimLevel"`
	CanExecute        bool                          `json:"canExecute"`
	Capabilities      []string                      `json:"capabilities"`
	SetupRequirements []SetupRequirement            `json:"setupRequirements,omitempty"`
	LastAttempt       *RuntimeAttempt               `json:"lastAttempt,omitempty"`
}

// Service is the Runtime Lab: it probes runtimes, self-tests them (safely, via
// the Operation Ledger for the local safe worker), and records attempts. It
// never claims execution for a runtime that did not actually run.
type Service struct {
	reg                 *Registry
	broker              *executionbroker.Broker
	ops                 *operations.Service // optional; enables ledger-backed self-test
	owner               string
	space               string
	now                 func() time.Time
	safeExecutionPolicy func(title, description, operationType string) bool

	mu       sync.Mutex
	attempts []RuntimeAttempt
	seq      int
}

// WithSafeExecutionPolicy attaches the live runtime-policy check required for
// local safe-worker self-tests. Without a policy, self-tests fail closed.
func (s *Service) WithSafeExecutionPolicy(policy func(title, description, operationType string) bool) *Service {
	s.safeExecutionPolicy = policy
	return s
}

// NewService builds a runtime lab service.
func NewService(broker *executionbroker.Broker, ops *operations.Service, ownerUserID, workspaceID string) *Service {
	return NewServiceWithAgentRuntimeRegistry(broker, ops, ownerUserID, workspaceID, nil)
}

// NewServiceWithAgentRuntimeRegistry builds Runtime Lab against the canonical
// production agent-runtime registry. The registry is optional to preserve the
// isolated Runtime Lab contract used by focused tests and local tooling.
func NewServiceWithAgentRuntimeRegistry(
	broker *executionbroker.Broker,
	ops *operations.Service,
	ownerUserID, workspaceID string,
	agentRegistry *agentruntime.Registry,
) *Service {
	return &Service{
		reg:    NewRegistryWithAgentRuntimeRegistry(broker, agentRegistry),
		broker: broker,
		ops:    ops,
		owner:  ownerUserID,
		space:  workspaceID,
		now:    time.Now,
	}
}

// Overview returns every runtime's truthful status + last attempt.
func (s *Service) Overview(ctx context.Context) []RuntimeSummary {
	out := make([]RuntimeSummary, 0, len(s.reg.Adapters()))
	for _, a := range s.reg.Adapters() {
		h := a.HealthCheck(ctx)
		out = append(out, RuntimeSummary{
			Info:              a.Info(),
			Status:            h.Status,
			ClaimLevel:        h.Claim,
			CanExecute:        h.Status.CanExecute(),
			Capabilities:      a.Capabilities(),
			SetupRequirements: h.SetupRequirements,
			LastAttempt:       s.lastAttempt(a.Info().ID),
		})
	}
	return out
}

// FeatureParity returns the source-reviewed feature/disposition inventory.
// Reading it never probes, installs, configures, or executes a runtime.
func (s *Service) FeatureParity() (RuntimeParityOverview, error) {
	return RuntimeFeatureParity(s.now().UTC())
}

// RuntimeFeatureParity returns one runtime inventory without broadening its
// declared readiness ceiling.
func (s *Service) RuntimeFeatureParity(runtimeID string) (RuntimeParityInventory, bool, error) {
	overview, err := s.FeatureParity()
	if err != nil {
		return RuntimeParityInventory{}, false, err
	}
	runtimeID = normalizeRuntimeID(runtimeID)
	for _, inventory := range overview.Inventories {
		if inventory.RuntimeID == runtimeID {
			return inventory, true, nil
		}
	}
	return RuntimeParityInventory{}, false, nil
}

// Probe probes a runtime truthfully.
func (s *Service) Probe(ctx context.Context, runtimeID string) (ProbeResult, bool) {
	a, ok := s.reg.Adapter(runtimeID)
	if !ok {
		return ProbeResult{}, false
	}
	result := a.Probe(ctx, s.now().UTC())
	return s.persistOpenClawDiscovery(result), true
}

// SelfTest runs a safe self-test. For the local safe worker it executes a real
// bounded task through the Operation Ledger and verifies it. For every other
// runtime it records a truthful setup_required attempt — no fake execution.
func (s *Service) SelfTest(ctx context.Context, runtimeID string) (RuntimeAttempt, bool) {
	a, ok := s.reg.Adapter(runtimeID)
	if !ok {
		return RuntimeAttempt{}, false
	}
	now := s.now().UTC()

	if runtimeID == executionbroker.LocalSafeWorkerID && s.ops != nil {
		return s.selfTestSafeWorker(ctx, a, now), true
	}

	// Non-safe runtime: never fake execution. Report the health-derived status.
	h := a.HealthCheck(ctx)
	status := AttemptSetupRequired
	if h.Status.CanExecute() {
		// Reachable but no operator-verified integration -> inconclusive, not success.
		status = AttemptInconclusive
	}
	return s.record(RuntimeAttempt{
		RuntimeID: runtimeID,
		Status:    status,
		Detail:    h.Detail,
		CreatedAt: now,
	}), true
}

func (s *Service) selfTestSafeWorker(ctx context.Context, a Adapter, now time.Time) RuntimeAttempt {
	if ctx == nil {
		ctx = context.Background()
	}
	// Create a real Operation for the self-test so it flows through the ledger.
	s.mu.Lock()
	s.seq++
	seq := s.seq
	s.mu.Unlock()

	in := operations.NewOperationInput{
		OwnerUserID:   s.owner,
		WorkspaceID:   s.space,
		Title:         "Runtime self-test: local safe worker",
		Description:   "Safe workspace-confined write/read-back/hash self-test through the Operation Ledger.",
		OperationType: "runtime_self_test",
		SourceType:    "runtime_lab",
		DedupeKey:     idempotency.OperationDedupeKey(s.space, "runtime_self_test", "runtime_lab", executionbroker.LocalSafeWorkerID, itoa(seq)),
	}
	ingest, err := s.ops.Ingest(in)
	if err != nil {
		return s.record(RuntimeAttempt{RuntimeID: executionbroker.LocalSafeWorkerID, Status: AttemptFailed, Detail: "self-test operation could not be ingested", CreatedAt: now})
	}
	op := ingest.Operation
	op.CurrentDecision = string(operations.DecisionRunSafeLocalWorker)
	op.RiskLevel = string(operations.RiskLow)
	op.AutonomyLevel = string(operations.AutonomyAuto)
	op.OwnerType = string(operations.OwnerHAI)
	op.RequiresApproval = false
	classified, err := s.ops.Transition(op, operations.StatusClassified, "hai", "", "runtime self-test classified")
	if err != nil {
		return s.record(RuntimeAttempt{RuntimeID: executionbroker.LocalSafeWorkerID, OperationID: op.ID.String(), Status: AttemptFailed, Detail: "self-test operation could not be classified", CreatedAt: now})
	}

	claimed, err := s.ops.ClaimOperation(
		ctx, s.owner, s.space, classified.ID, uuid.New(), 5*time.Minute,
	)
	if err != nil {
		detail := "self-test execution claim could not be acquired"
		if errors.Is(err, operations.ErrOperationClaimed) {
			detail = operations.ErrOperationClaimed.Error()
		}
		return s.record(RuntimeAttempt{RuntimeID: executionbroker.LocalSafeWorkerID, OperationID: op.ID.String(), Status: AttemptFailed, Detail: detail, CreatedAt: now})
	}
	if s.safeExecutionPolicy == nil || !s.safeExecutionPolicy(claimed.Operation.Title, claimed.Operation.Description, claimed.Operation.OperationType) {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		releaseErr := s.ops.ReleaseClaim(cleanupCtx, claimed.Claim)
		cancel()
		detail := "safe-worker execution blocked by current runtime policy"
		if s.safeExecutionPolicy == nil {
			detail = "safe-worker execution blocked because runtime policy is unavailable"
		}
		if releaseErr != nil && !errors.Is(releaseErr, operations.ErrClaimLost) {
			detail = "safe-worker execution blocked and operation claim release failed"
		}
		return s.record(RuntimeAttempt{
			RuntimeID:   executionbroker.LocalSafeWorkerID,
			OperationID: op.ID.String(),
			Status:      AttemptBlocked,
			Detail:      detail,
			CreatedAt:   now,
		})
	}
	outcome, err := background.ExecuteSafeOperationClaimed(ctx, s.ops, s.broker, claimed.Operation, claimed.Claim, now,
		func(op models.Operation) bool {
			return s.safeExecutionPolicy != nil && s.safeExecutionPolicy(op.Title, op.Description, op.OperationType)
		})
	if err != nil {
		if outcome.Receipt == nil && !outcome.Verified && !outcome.Interrupted && !errors.Is(err, background.ErrSafeOutcomeUncertain) {
			cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			releaseErr := s.ops.ReleaseClaim(cleanupCtx, claimed.Claim)
			cancel()
			if releaseErr != nil && !errors.Is(releaseErr, operations.ErrClaimLost) {
				err = errors.Join(err, releaseErr)
			}
		}
	}
	return s.record(runtimeAttemptFromSafeOutcome(op.ID.String(), outcome, err, now))
}

func runtimeAttemptFromSafeOutcome(operationID string, outcome background.SafeOutcome, err error, now time.Time) RuntimeAttempt {
	attempt := RuntimeAttempt{
		RuntimeID:       executionbroker.LocalSafeWorkerID,
		OperationID:     operationID,
		IdempotencyKey:  idempotency.RuntimeAttemptIdempotencyKey(operationID, "self-test", executionbroker.LocalSafeWorkerID, ""),
		Interrupted:     outcome.Interrupted,
		OutcomeRecorded: outcome.Interrupted || outcome.Failed,
		CreatedAt:       now,
	}
	if outcome.Operation != nil {
		attempt.OperationStatus = outcome.Operation.Status
		attempt.BoundedOutput = safety.RedactSecrets(outcome.Operation.ResultSummary)
	}
	if outcome.Receipt != nil {
		receipt := executionbroker.PublicExecutionResult(*outcome.Receipt)
		attempt.Receipt = &receipt
		attempt.BoundedOutput = receipt.Output.BoundedOutput
	}
	verified := err == nil && outcome.Verified && !outcome.Interrupted && !outcome.Failed &&
		outcome.Operation != nil && outcome.Operation.Status == string(operations.StatusCompleted) &&
		outcome.Operation.VerificationStatus == string(operations.VerificationPassed)
	switch {
	case verified:
		attempt.Status = AttemptSucceeded
		attempt.Detail = "safe worker executed and verified through the Operation Ledger"
		attempt.VerificationPassed = true
		attempt.OutcomeRecorded = true
	case outcome.Interrupted || errors.Is(err, background.ErrSafeOutcomeUncertain) ||
		((outcome.Receipt != nil || outcome.Verified) && err != nil) || (err == nil && !outcome.Failed):
		attempt.Status = AttemptInconclusive
		attempt.Detail = "safe worker outcome requires reconciliation; retained receipt does not confirm recorded completion"
		attempt.ReconciliationRequired = true
	default:
		attempt.Status = AttemptFailed
		attempt.Detail = "safe worker execution did not complete"
	}
	return attempt
}

// Attempts returns the recorded attempts for a runtime (newest first).
func (s *Service) Attempts(runtimeID string) []RuntimeAttempt {
	runtimeID = normalizeRuntimeID(runtimeID)
	s.mu.Lock()
	out := make([]RuntimeAttempt, 0, len(s.attempts))
	for i := len(s.attempts) - 1; i >= 0; i-- {
		if s.attempts[i].RuntimeID == runtimeID {
			out = append(out, publicRuntimeAttempt(s.attempts[i]))
		}
	}
	s.mu.Unlock()

	if runtimeID == executionbroker.LocalSafeWorkerID {
		out = mergeRuntimeAttempts(out, s.safeWorkerLedgerAttempts())
	}
	if runtimeID == "openclaw" {
		out = mergeRuntimeAttempts(out, s.openClawLedgerAttempts())
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out
}

func (s *Service) openClawLedgerAttempts() []RuntimeAttempt {
	discovery, found := s.durableOpenClawDiscovery()
	if !found {
		return nil
	}
	return []RuntimeAttempt{{
		ID:                 "rta-openclaw-discovery-" + discovery.EvidenceSHA256,
		RuntimeID:          "openclaw",
		Status:             AttemptSucceeded,
		Detail:             "durable, read-only OpenClaw discovery recovered from the Operation Ledger",
		DiscoveryRecovered: true,
		CreatedAt:          discovery.CheckedAt,
	}}
}

func (s *Service) record(a RuntimeAttempt) RuntimeAttempt {
	a = publicRuntimeAttempt(a)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seq++
	a.ID = "rta-" + itoa(s.seq)
	s.attempts = append(s.attempts, a)
	return publicRuntimeAttempt(a)
}

func publicRuntimeAttempt(attempt RuntimeAttempt) RuntimeAttempt {
	attempt.Detail = safety.RedactSecrets(attempt.Detail)
	attempt.BoundedOutput = safety.RedactSecrets(attempt.BoundedOutput)
	if attempt.Receipt != nil {
		receipt := executionbroker.PublicExecutionResult(*attempt.Receipt)
		attempt.Receipt = &receipt
	}
	return attempt
}

func (s *Service) lastAttempt(runtimeID string) *RuntimeAttempt {
	attempts := s.Attempts(runtimeID)
	if len(attempts) > 0 {
		attempt := attempts[0]
		return &attempt
	}
	return nil
}

func (s *Service) safeWorkerLedgerAttempts() []RuntimeAttempt {
	if s.ops == nil {
		return nil
	}
	ledger, err := s.ops.List(operations.Filter{
		OwnerUserID: s.owner,
		WorkspaceID: s.space,
		Limit:       200,
	})
	if err != nil {
		return nil
	}
	attempts := make([]RuntimeAttempt, 0, len(ledger))
	for _, operation := range ledger {
		if operation.OperationType != "runtime_self_test" ||
			operation.SourceType != "runtime_lab" ||
			operation.RuntimeID != executionbroker.LocalSafeWorkerID {
			continue
		}
		attempts = append(attempts, runtimeAttemptFromOperation(operation))
	}
	return attempts
}

func runtimeAttemptFromOperation(operation models.Operation) RuntimeAttempt {
	createdAt := operation.UpdatedAt
	if operation.CompletedAt != nil {
		createdAt = *operation.CompletedAt
	}
	status := AttemptPending
	switch operations.OperationStatus(operation.Status) {
	case operations.StatusCompleted:
		if operations.VerificationStatus(operation.VerificationStatus) == operations.VerificationPassed {
			status = AttemptSucceeded
		} else {
			status = AttemptFailed
		}
	case operations.StatusFailed:
		status = AttemptFailed
	case operations.StatusBlocked:
		status = AttemptBlocked
	case operations.StatusRunning, operations.StatusVerifying:
		status = AttemptRunning
	case operations.StatusInterrupted:
		status = AttemptInconclusive
	}
	attempt := RuntimeAttempt{
		ID:                     "rta-operation-" + operation.ID.String(),
		RuntimeID:              executionbroker.LocalSafeWorkerID,
		OperationID:            operation.ID.String(),
		Status:                 status,
		IdempotencyKey:         operation.DedupeKey,
		Detail:                 "safe worker execution recovered from the recorded Operation Ledger",
		BoundedOutput:          safety.RedactSecrets(operation.ResultSummary),
		VerificationPassed:     status == AttemptSucceeded,
		OperationStatus:        operation.Status,
		Interrupted:            operation.Status == string(operations.StatusInterrupted),
		ReconciliationRequired: operation.Status == string(operations.StatusInterrupted),
		OutcomeRecorded:        operation.Status == string(operations.StatusInterrupted) || operation.Status == string(operations.StatusFailed) || operation.Status == string(operations.StatusCompleted),
		CreatedAt:              createdAt,
	}
	if operation.LastError != "" {
		attempt.Detail = "safe worker execution did not complete; inspect the recorded operation"
	}
	if strings.TrimSpace(operation.WorldModelStateJSON) != "" {
		var state map[string]json.RawMessage
		if err := json.Unmarshal([]byte(operation.WorldModelStateJSON), &state); err != nil || state == nil {
			attempt.ReconciliationRequired = true
		} else if raw, exists := state["safeWorkerExecution"]; exists {
			var execution struct {
				Result           *executionbroker.ExecutionResult `json:"result"`
				OutcomeUncertain *bool                            `json:"outcomeUncertain"`
			}
			if err := json.Unmarshal(raw, &execution); err != nil || execution.OutcomeUncertain == nil || execution.Result == nil {
				attempt.ReconciliationRequired = true
			} else {
				result := execution.Result
				complete := result.RuntimeID == executionbroker.LocalSafeWorkerID && result.OK && result.Verification.Passed &&
					result.Verification.FileExists && result.Verification.InsideWorkspace && result.Verification.HashMatches &&
					result.Verification.MarkerFound && result.Verification.OutputBounded && result.Output.MarkerFound &&
					result.Output.ArtifactPath != "" && result.Output.ArtifactHash != ""
				attempt.ReconciliationRequired = attempt.ReconciliationRequired || *execution.OutcomeUncertain ||
					(status == AttemptSucceeded && !complete) || operation.Status == string(operations.StatusAwaitingApproval)
			}
			if execution.Result != nil {
				receipt := executionbroker.PublicExecutionResult(*execution.Result)
				attempt.Receipt = &receipt
				attempt.BoundedOutput = receipt.Output.BoundedOutput
			}
		}
	}
	if attempt.ReconciliationRequired {
		attempt.Status = AttemptInconclusive
		attempt.VerificationPassed = false
		attempt.Detail = "safe worker outcome requires reconciliation; recorded receipt does not confirm completion"
	}
	return attempt
}

func mergeRuntimeAttempts(primary, recovered []RuntimeAttempt) []RuntimeAttempt {
	seenOperations := make(map[string]struct{}, len(primary))
	out := append([]RuntimeAttempt(nil), primary...)
	for _, attempt := range primary {
		if attempt.OperationID != "" {
			seenOperations[attempt.OperationID] = struct{}{}
		}
	}
	for _, attempt := range recovered {
		if _, exists := seenOperations[attempt.OperationID]; exists {
			continue
		}
		out = append(out, attempt)
	}
	return out
}

func normalizeRuntimeID(runtimeID string) string {
	return strings.ToLower(strings.TrimSpace(runtimeID))
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
