package ambientmonitor

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"
)

type Service struct {
	repository Repository
	collector  Collector
	sink       Sink
	now        func() time.Time
}

func NewService(repository Repository, collector Collector, sink Sink) *Service {
	return &Service{repository: repository, collector: collector, sink: sink, now: time.Now}
}

func newService(repository Repository, collector Collector, sink Sink, now func() time.Time) *Service {
	service := NewService(repository, collector, sink)
	if now != nil {
		service.now = now
	}
	return service
}

func (s *Service) RegisterTarget(ctx context.Context, request RegisterTargetRequest) (MonitorTarget, bool, error) {
	if err := s.available(false); err != nil {
		return MonitorTarget{}, false, err
	}
	scope, err := validateScope(request.Scope)
	if err != nil {
		return MonitorTarget{}, false, err
	}
	request.Scope = scope
	for name, item := range map[string]string{"target id": request.TargetID, "outcome id": request.OutcomeID, "indicator id": request.IndicatorID, "idempotency key": request.IdempotencyKey} {
		if err := validateIdentifier(name, strings.TrimSpace(item)); err != nil {
			return MonitorTarget{}, false, err
		}
	}
	if err := validateSourceKind(request.SourceKind); err != nil {
		return MonitorTarget{}, false, err
	}
	if err := validateCadence(request.Cadence); err != nil {
		return MonitorTarget{}, false, err
	}
	now := s.now().UTC().Truncate(time.Microsecond)
	if request.RequestedAt, err = validateRequestTime("request time", request.RequestedAt, now); err != nil {
		return MonitorTarget{}, false, err
	}
	if request.FirstRunAt, err = validateTime("first run time", request.FirstRunAt); err != nil {
		return MonitorTarget{}, false, err
	}
	if request.FirstRunAt.Before(now.Add(-maxScheduleHorizon)) || request.FirstRunAt.After(now.Add(maxScheduleHorizon)) {
		return MonitorTarget{}, false, fmt.Errorf("%w: first run time exceeds scheduling horizon", ErrInvalidInput)
	}
	target := MonitorTarget{ContractVersion: ContractVersion, ID: strings.TrimSpace(request.TargetID), Scope: scope, OutcomeID: strings.TrimSpace(request.OutcomeID), IndicatorID: strings.TrimSpace(request.IndicatorID), SourceKind: request.SourceKind, Enabled: request.Enabled, Cadence: request.Cadence, NextRunAt: request.FirstRunAt, CreatedAt: request.RequestedAt, UpdatedAt: request.RequestedAt, Authority: advisoryAuthority()}
	return s.repository.CreateTarget(ctx, scope.OwnerID, scope.WorkspaceID, strings.TrimSpace(request.IdempotencyKey), target)
}

func (s *Service) SetEnabled(ctx context.Context, request SetEnabledRequest) (MonitorTarget, bool, error) {
	if err := s.available(false); err != nil {
		return MonitorTarget{}, false, err
	}
	scope, err := validateScope(request.Scope)
	if err != nil {
		return MonitorTarget{}, false, err
	}
	request.Scope = scope
	if err := validateIdentifier("target id", strings.TrimSpace(request.TargetID)); err != nil {
		return MonitorTarget{}, false, err
	}
	if err := validateIdentifier("idempotency key", strings.TrimSpace(request.IdempotencyKey)); err != nil {
		return MonitorTarget{}, false, err
	}
	now := s.now().UTC().Truncate(time.Microsecond)
	if request.RequestedAt, err = validateRequestTime("request time", request.RequestedAt, now); err != nil {
		return MonitorTarget{}, false, err
	}
	digest, err := exactDigest("set_enabled", struct {
		Scope       Scope
		TargetID    string
		Enabled     bool
		RequestedAt time.Time
	}{scope, strings.TrimSpace(request.TargetID), request.Enabled, request.RequestedAt})
	if err != nil {
		return MonitorTarget{}, false, err
	}
	return s.repository.SetEnabled(ctx, scope.OwnerID, scope.WorkspaceID, strings.TrimSpace(request.IdempotencyKey), digest, strings.TrimSpace(request.TargetID), request.Enabled, request.RequestedAt)
}

func (s *Service) Target(ctx context.Context, scope Scope, targetID string) (MonitorTarget, error) {
	if err := s.available(false); err != nil {
		return MonitorTarget{}, err
	}
	clean, err := validateScope(scope)
	if err != nil {
		return MonitorTarget{}, err
	}
	if err := validateIdentifier("target id", strings.TrimSpace(targetID)); err != nil {
		return MonitorTarget{}, err
	}
	target, err := s.repository.GetTarget(ctx, clean.OwnerID, clean.WorkspaceID, strings.TrimSpace(targetID))
	if err != nil {
		return MonitorTarget{}, err
	}
	if target.Scope != clean {
		return MonitorTarget{}, ErrScopeViolation
	}
	return validateTarget(target)
}
func (s *Service) Targets(ctx context.Context, scope Scope) ([]MonitorTarget, error) {
	if err := s.available(false); err != nil {
		return nil, err
	}
	clean, err := validateScope(scope)
	if err != nil {
		return nil, err
	}
	items, err := s.repository.ListTargets(ctx, clean.OwnerID, clean.WorkspaceID)
	if err != nil {
		return nil, err
	}
	for i := range items {
		if items[i].Scope != clean {
			return nil, ErrScopeViolation
		}
		items[i], err = validateTarget(items[i])
		if err != nil {
			return nil, err
		}
	}
	return items, nil
}

func (s *Service) ClaimDue(ctx context.Context, request ClaimDueRequest) ([]MonitorTarget, error) {
	if err := s.available(false); err != nil {
		return nil, err
	}
	scope, err := validateScope(request.Scope)
	if err != nil {
		return nil, err
	}
	if err := validateIdentifier("worker id", strings.TrimSpace(request.WorkerID)); err != nil {
		return nil, err
	}
	now := s.now().UTC().Truncate(time.Microsecond)
	if request.Now, err = validateRequestTime("claim time", request.Now, now); err != nil {
		return nil, err
	}
	if err := validateLeaseDuration(request.LeaseDuration); err != nil {
		return nil, err
	}
	if request.Limit < 1 || request.Limit > maxClaimLimit {
		return nil, fmt.Errorf("%w: claim limit must be between 1 and %d", ErrInvalidInput, maxClaimLimit)
	}
	items, err := s.repository.ClaimDue(ctx, scope.OwnerID, scope.WorkspaceID, strings.TrimSpace(request.WorkerID), request.Now, request.LeaseDuration, request.Limit)
	if err != nil {
		return nil, err
	}
	for i := range items {
		if items[i].Scope != scope || items[i].Lease.WorkerID != strings.TrimSpace(request.WorkerID) {
			return nil, ErrScopeViolation
		}
		items[i], err = validateTarget(items[i])
		if err != nil {
			return nil, err
		}
	}
	return items, nil
}

func (s *Service) DueScopes(ctx context.Context, at time.Time, limit int) ([]Scope, error) {
	if err := s.available(false); err != nil {
		return nil, err
	}
	now := s.now().UTC().Truncate(time.Microsecond)
	cleanAt, err := validateRequestTime("due scope time", at, now)
	if err != nil {
		return nil, err
	}
	if limit < 1 || limit > maxClaimLimit {
		return nil, fmt.Errorf("%w: due scope limit must be between 1 and %d", ErrInvalidInput, maxClaimLimit)
	}
	items, err := s.repository.ListDueScopes(ctx, cleanAt, limit)
	if err != nil {
		return nil, err
	}
	for index := range items {
		items[index], err = validateScope(items[index])
		if err != nil {
			return nil, ErrScopeViolation
		}
	}
	return items, nil
}

func (s *Service) Complete(ctx context.Context, request CompleteRequest) (Completion, error) {
	if err := s.available(false); err != nil {
		return Completion{}, err
	}
	scope, err := validateScope(request.Scope)
	if err != nil {
		return Completion{}, err
	}
	if err := validateIdentifier("target id", strings.TrimSpace(request.TargetID)); err != nil {
		return Completion{}, err
	}
	if err := validateIdentifier("worker id", strings.TrimSpace(request.WorkerID)); err != nil {
		return Completion{}, err
	}
	if err := validateIdentifier("idempotency key", strings.TrimSpace(request.IdempotencyKey)); err != nil {
		return Completion{}, err
	}
	if request.LeaseGeneration == 0 {
		return Completion{}, fmt.Errorf("%w: lease generation is required", ErrInvalidInput)
	}
	now := s.now().UTC().Truncate(time.Microsecond)
	if request.CompletedAt, err = validateRequestTime("completion time", request.CompletedAt, now); err != nil {
		return Completion{}, err
	}
	if request.Collected, err = validateCollected(request.Collected, request.CompletedAt); err != nil {
		return Completion{}, err
	}
	// Collection can finish a few milliseconds after the scheduler's as-of
	// timestamp. Preserve the source observation time and advance the immutable
	// persistence timestamp so observed_at can never follow recorded_at.
	if request.Collected.ObservedAt.After(request.CompletedAt) {
		request.CompletedAt = request.Collected.ObservedAt
	}
	target, err := s.repository.GetTarget(ctx, scope.OwnerID, scope.WorkspaceID, strings.TrimSpace(request.TargetID))
	if err != nil {
		return Completion{}, err
	}
	if target.Scope != scope {
		return Completion{}, ErrScopeViolation
	}
	digest, err := exactDigest("complete", struct {
		Scope              Scope
		TargetID, WorkerID string
		Generation         uint64
		Collected          CollectedObservation
		CompletedAt        time.Time
	}{scope, target.ID, strings.TrimSpace(request.WorkerID), request.LeaseGeneration, request.Collected, request.CompletedAt})
	if err != nil {
		return Completion{}, err
	}
	observationID, err := newRecordID("obs")
	if err != nil {
		return Completion{}, err
	}
	runID, err := newRecordID("run")
	if err != nil {
		return Completion{}, err
	}
	observation := ObservationRecord{ContractVersion: ContractVersion, ID: observationID, Scope: scope, TargetID: target.ID, OutcomeID: target.OutcomeID, IndicatorID: target.IndicatorID, SourceKind: target.SourceKind, Value: request.Collected.Value, ObservedAt: request.Collected.ObservedAt, RecordedAt: request.CompletedAt, SourceDigest: request.Collected.SourceDigest, Authority: advisoryAuthority()}
	observation.RecordDigest, err = exactDigest("observation_record", struct {
		Scope                            Scope
		TargetID, OutcomeID, IndicatorID string
		SourceKind                       SourceKind
		Value                            float64
		ObservedAt, RecordedAt           time.Time
		SourceDigest                     string
	}{scope, target.ID, target.OutcomeID, target.IndicatorID, target.SourceKind, observation.Value, observation.ObservedAt, observation.RecordedAt, observation.SourceDigest})
	if err != nil {
		return Completion{}, err
	}
	run := MonitorRun{ContractVersion: ContractVersion, ID: runID, Scope: scope, TargetID: target.ID, OutcomeID: target.OutcomeID, IndicatorID: target.IndicatorID, SourceKind: target.SourceKind, LeaseGeneration: request.LeaseGeneration, Status: RunCompleted, StartedAt: target.Lease.ClaimedAt, FinishedAt: request.CompletedAt, ObservationID: observation.ID, ObservationDigest: observation.RecordDigest, IdempotencyDigest: digest, Authority: advisoryAuthority()}
	run.RecordDigest, err = runDigest(run)
	if err != nil {
		return Completion{}, err
	}
	snapshot, err := legacyCompositionSnapshot(request.CompletedAt)
	if err != nil {
		return Completion{}, err
	}
	if provider, ok := s.sink.(SnapshotProvider); ok && provider != nil && !isTypedNil(provider) {
		captured, captureErr := provider.CaptureSnapshot(ctx, AdvisorySignal{
			Observation: observation,
			Run:         run,
			Authority:   advisoryAuthority(),
		})
		if captureErr == nil {
			snapshot, err = validateCompositionSnapshot(captured)
			if err != nil {
				return Completion{}, err
			}
		}
	}
	next, err := nextCadence(target.NextRunAt, target.Cadence, request.CompletedAt)
	if err != nil {
		return Completion{}, err
	}
	storedObservation, storedRun, created, err := s.repository.Complete(ctx, scope.OwnerID, scope.WorkspaceID, strings.TrimSpace(request.IdempotencyKey), digest, target.ID, strings.TrimSpace(request.WorkerID), request.LeaseGeneration, request.Collected.SourceDigest, observation, run, snapshot, next)
	if err != nil {
		return Completion{}, err
	}
	delivery, err := s.repository.GetCompositionByRun(ctx, scope.OwnerID, scope.WorkspaceID, storedRun.ID)
	if err != nil {
		return Completion{}, err
	}
	return Completion{Observation: storedObservation, Run: storedRun, Composition: delivery, Created: created, Composed: delivery.Status == CompositionSucceeded, Authority: advisoryAuthority()}, nil
}

func (s *Service) Fail(ctx context.Context, request FailRequest) (MonitorRun, bool, error) {
	if err := s.available(false); err != nil {
		return MonitorRun{}, false, err
	}
	scope, err := validateScope(request.Scope)
	if err != nil {
		return MonitorRun{}, false, err
	}
	for name, item := range map[string]string{"target id": request.TargetID, "worker id": request.WorkerID, "idempotency key": request.IdempotencyKey, "failure code": request.FailureCode} {
		if err := validateIdentifier(name, strings.TrimSpace(item)); err != nil {
			return MonitorRun{}, false, err
		}
	}
	if err := validateBoundedText("failure summary", request.FailureSummary, maxFailureLength, true); err != nil {
		return MonitorRun{}, false, err
	}
	if request.LeaseGeneration == 0 {
		return MonitorRun{}, false, fmt.Errorf("%w: lease generation is required", ErrInvalidInput)
	}
	now := s.now().UTC().Truncate(time.Microsecond)
	if request.FailedAt, err = validateRequestTime("failure time", request.FailedAt, now); err != nil {
		return MonitorRun{}, false, err
	}
	target, err := s.repository.GetTarget(ctx, scope.OwnerID, scope.WorkspaceID, strings.TrimSpace(request.TargetID))
	if err != nil {
		return MonitorRun{}, false, err
	}
	if target.Scope != scope {
		return MonitorRun{}, false, ErrScopeViolation
	}
	digest, err := exactDigest("fail", struct {
		Scope                       Scope
		TargetID, WorkerID          string
		Generation                  uint64
		FailureCode, FailureSummary string
		FailedAt                    time.Time
	}{scope, target.ID, strings.TrimSpace(request.WorkerID), request.LeaseGeneration, strings.TrimSpace(request.FailureCode), strings.TrimSpace(request.FailureSummary), request.FailedAt})
	if err != nil {
		return MonitorRun{}, false, err
	}
	runID, err := newRecordID("run")
	if err != nil {
		return MonitorRun{}, false, err
	}
	run := MonitorRun{ContractVersion: ContractVersion, ID: runID, Scope: scope, TargetID: target.ID, OutcomeID: target.OutcomeID, IndicatorID: target.IndicatorID, SourceKind: target.SourceKind, LeaseGeneration: request.LeaseGeneration, Status: RunFailed, StartedAt: target.Lease.ClaimedAt, FinishedAt: request.FailedAt, FailureCode: strings.TrimSpace(request.FailureCode), FailureSummary: strings.TrimSpace(request.FailureSummary), IdempotencyDigest: digest, Authority: advisoryAuthority()}
	run.RecordDigest, err = runDigest(run)
	if err != nil {
		return MonitorRun{}, false, err
	}
	next, err := nextCadence(target.NextRunAt, target.Cadence, request.FailedAt)
	if err != nil {
		return MonitorRun{}, false, err
	}
	return s.repository.Fail(ctx, scope.OwnerID, scope.WorkspaceID, strings.TrimSpace(request.IdempotencyKey), digest, target.ID, strings.TrimSpace(request.WorkerID), request.LeaseGeneration, run, next)
}

func (s *Service) ProcessClaim(ctx context.Context, request ProcessClaimRequest) (Completion, error) {
	if err := s.available(true); err != nil {
		return Completion{}, err
	}
	scope, err := validateScope(request.Scope)
	if err != nil {
		return Completion{}, err
	}
	if err := validateIdentifier("target id", strings.TrimSpace(request.TargetID)); err != nil {
		return Completion{}, err
	}
	if err := validateIdentifier("worker id", strings.TrimSpace(request.WorkerID)); err != nil {
		return Completion{}, err
	}
	if err := validateIdentifier("idempotency key", strings.TrimSpace(request.IdempotencyKey)); err != nil {
		return Completion{}, err
	}
	if request.LeaseGeneration == 0 {
		return Completion{}, fmt.Errorf("%w: lease generation is required", ErrInvalidInput)
	}
	now := s.now().UTC().Truncate(time.Microsecond)
	if request.CompletedAt, err = validateRequestTime("completion time", request.CompletedAt, now); err != nil {
		return Completion{}, err
	}
	target, err := s.Target(ctx, scope, request.TargetID)
	if err != nil {
		return Completion{}, err
	}
	if replay, found, replayErr := s.repository.FindCompletion(ctx, scope.OwnerID, scope.WorkspaceID, strings.TrimSpace(request.IdempotencyKey)); replayErr != nil {
		return Completion{}, replayErr
	} else if found {
		if replay.Run.TargetID != target.ID || replay.Observation.TargetID != target.ID || replay.Run.LeaseGeneration != request.LeaseGeneration {
			return Completion{}, ErrIdempotencyConflict
		}
		delivery, deliveryErr := s.repository.GetCompositionByRun(ctx, scope.OwnerID, scope.WorkspaceID, replay.Run.ID)
		if deliveryErr != nil {
			return Completion{}, deliveryErr
		}
		replay.Composition = delivery
		replay.Composed = delivery.Status == CompositionSucceeded
		return replay, nil
	}
	// An active lease must match before even the read-only collector runs. An
	// inactive lease is allowed through because it may be an exact replay used
	// to recover a transient advisory-sink failure; Complete still performs the
	// authoritative repository fencing check.
	if target.Lease.Active() {
		if err := ownedLease(target, strings.TrimSpace(request.WorkerID), request.LeaseGeneration, request.CompletedAt); err != nil {
			return Completion{}, err
		}
	}
	collected, collectErr := s.collector.Collect(ctx, target)
	if collectErr != nil {
		_, _, failErr := s.Fail(ctx, FailRequest{IdempotencyKey: request.IdempotencyKey, Scope: scope, TargetID: request.TargetID, WorkerID: request.WorkerID, LeaseGeneration: request.LeaseGeneration, FailureCode: "collector_failed", FailureSummary: "collector returned an error", FailedAt: request.CompletedAt})
		if failErr != nil {
			return Completion{}, failErr
		}
		return Completion{}, ErrCollectorFailed
	}
	completion, err := s.Complete(ctx, CompleteRequest{IdempotencyKey: request.IdempotencyKey, Scope: scope, TargetID: request.TargetID, WorkerID: request.WorkerID, LeaseGeneration: request.LeaseGeneration, Collected: collected, CompletedAt: request.CompletedAt})
	if err != nil {
		return Completion{}, err
	}
	return completion, nil
}

// ProcessDue claims and processes a bounded workspace batch. Individual
// failures are returned as sanitized codes so one bad source cannot strand the
// other leases or expose collector/provider details.
func (s *Service) ProcessDue(ctx context.Context, request ProcessDueRequest) (ProcessDueResult, error) {
	claimed, err := s.ClaimDue(ctx, ClaimDueRequest{
		Scope: request.Scope, WorkerID: request.WorkerID, Now: request.Now,
		LeaseDuration: request.LeaseDuration, Limit: request.Limit,
	})
	if err != nil {
		return ProcessDueResult{}, err
	}
	result := ProcessDueResult{
		Claimed: len(claimed), Completions: make([]Completion, 0, len(claimed)),
		Failures: make([]ProcessFailure, 0), Authority: advisoryAuthority(),
	}
	for _, target := range claimed {
		keyDigest, digestErr := exactDigest("process_due", struct {
			Scope      Scope
			TargetID   string
			WorkerID   string
			Generation uint64
			At         time.Time
		}{target.Scope, target.ID, strings.TrimSpace(request.WorkerID), target.Lease.Generation, request.Now.UTC().Truncate(time.Microsecond)})
		if digestErr != nil {
			result.Failures = append(result.Failures, ProcessFailure{TargetID: target.ID, Code: "request_digest_failed"})
			continue
		}
		processRequest := ProcessClaimRequest{
			IdempotencyKey: "due-" + keyDigest[:32], Scope: target.Scope,
			TargetID: target.ID, WorkerID: request.WorkerID,
			LeaseGeneration: target.Lease.Generation, CompletedAt: s.now().UTC().Truncate(time.Microsecond),
		}
		// A fast local collector can finish in the same clock tick as its claim.
		// Durable target transitions require strictly monotonic timestamps, so do
		// not ask Postgres to persist a completion at the claim timestamp.
		if !processRequest.CompletedAt.After(target.Lease.ClaimedAt) {
			processRequest.CompletedAt = target.Lease.ClaimedAt.Add(time.Microsecond)
		}
		completion, processErr := s.ProcessClaim(ctx, processRequest)
		if processErr != nil {
			code := processFailureCode(processErr)
			result.Failures = append(result.Failures, ProcessFailure{TargetID: target.ID, Code: code})
			if code == "monitor_failed" {
				failureDigest, digestErr := exactDigest("process_due_failure", struct {
					Scope      Scope
					TargetID   string
					WorkerID   string
					Generation uint64
					ProcessKey string
				}{target.Scope, target.ID, strings.TrimSpace(request.WorkerID), target.Lease.Generation, processRequest.IdempotencyKey})
				if digestErr == nil {
					failedAt := s.now().UTC().Truncate(time.Microsecond)
					_, _, _ = s.Fail(ctx, FailRequest{
						IdempotencyKey:  "fail-" + failureDigest[:32],
						Scope:           target.Scope,
						TargetID:        target.ID,
						WorkerID:        request.WorkerID,
						LeaseGeneration: target.Lease.Generation,
						FailureCode:     code,
						FailureSummary:  "monitor processing could not be persisted",
						FailedAt:        failedAt,
					})
				}
			}
			continue
		}
		result.Completions = append(result.Completions, completion)
	}
	compositionWorker := strings.TrimSpace(request.WorkerID) + "-composition"
	if len(compositionWorker) > maxIdentifierLength {
		compositionWorker = "ambient-composition-worker"
	}
	// Collection and immutable snapshot capture can finish after the sweep's
	// original as-of time. Claim compositions against the current service clock
	// so handoffs enqueued by this pass are eligible immediately, while future
	// retry schedules remain fenced by their own next-attempt timestamp.
	compositionAsOf := s.now().UTC().Truncate(time.Microsecond)
	if compositionAsOf.Before(request.Now) {
		compositionAsOf = request.Now
	}
	for _, completion := range result.Completions {
		if completion.Composition.Status == CompositionPending && compositionAsOf.Before(completion.Composition.NextAttemptAt) {
			compositionAsOf = completion.Composition.NextAttemptAt
		}
	}
	compositions, compositionErr := s.ProcessCompositions(ctx, ProcessCompositionsRequest{
		Scope: request.Scope, WorkerID: compositionWorker, Now: compositionAsOf,
		LeaseDuration: request.LeaseDuration, Limit: request.Limit,
	})
	if compositionErr != nil {
		return result, compositionErr
	}
	result.Compositions = compositions
	for index := range result.Completions {
		for _, delivery := range compositions.Records {
			if result.Completions[index].Run.ID == delivery.RunID {
				result.Completions[index].Composition = delivery
				result.Completions[index].Composed = delivery.Status == CompositionSucceeded
				break
			}
		}
	}
	return result, nil
}

func processFailureCode(err error) string {
	switch {
	case errors.Is(err, ErrCollectorFailed), errors.Is(err, ErrCollectorUnavailable):
		return "collector_failed"
	case errors.Is(err, ErrSinkFailed):
		return "advisory_composition_failed"
	case errors.Is(err, ErrLeaseLost):
		return "lease_lost"
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return "cancelled"
	default:
		return "monitor_failed"
	}
}

func (s *Service) RecoverExpiredLeases(ctx context.Context, scope Scope, at time.Time) (int, error) {
	if err := s.available(false); err != nil {
		return 0, err
	}
	clean, err := validateScope(scope)
	if err != nil {
		return 0, err
	}
	now := s.now().UTC().Truncate(time.Microsecond)
	if at, err = validateRequestTime("recovery time", at, now); err != nil {
		return 0, err
	}
	return s.repository.RecoverExpiredLeases(ctx, clean.OwnerID, clean.WorkspaceID, at)
}
func (s *Service) Observations(ctx context.Context, scope Scope, targetID string, limit int) ([]ObservationRecord, error) {
	if err := s.available(false); err != nil {
		return nil, err
	}
	clean, err := validateScope(scope)
	if err != nil {
		return nil, err
	}
	if err := validateIdentifier("target id", strings.TrimSpace(targetID)); err != nil {
		return nil, err
	}
	return s.repository.ListObservations(ctx, clean.OwnerID, clean.WorkspaceID, strings.TrimSpace(targetID), limit)
}
func (s *Service) Runs(ctx context.Context, scope Scope, targetID string, limit int) ([]MonitorRun, error) {
	if err := s.available(false); err != nil {
		return nil, err
	}
	clean, err := validateScope(scope)
	if err != nil {
		return nil, err
	}
	if err := validateIdentifier("target id", strings.TrimSpace(targetID)); err != nil {
		return nil, err
	}
	return s.repository.ListRuns(ctx, clean.OwnerID, clean.WorkspaceID, strings.TrimSpace(targetID), limit)
}

func (s *Service) available(requireCollector bool) error {
	if s == nil || s.repository == nil || isTypedNil(s.repository) || s.now == nil {
		return ErrRepositoryUnavailable
	}
	if requireCollector && (s.collector == nil || isTypedNil(s.collector)) {
		return ErrCollectorUnavailable
	}
	return nil
}
func isTypedNil(value any) bool {
	if value == nil {
		return true
	}
	kind := reflect.ValueOf(value).Kind()
	return (kind == reflect.Pointer || kind == reflect.Map || kind == reflect.Slice || kind == reflect.Func || kind == reflect.Interface || kind == reflect.Chan) && reflect.ValueOf(value).IsNil()
}
func runDigest(run MonitorRun) (string, error) {
	copy := run
	copy.ID = ""
	copy.RecordDigest = ""
	return exactDigest("monitor_run", copy)
}
