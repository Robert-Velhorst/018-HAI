package ambientmonitor

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"automation-hub-backend/internal/proactivity"
)

type deterministicCollector struct {
	mu    sync.Mutex
	value CollectedObservation
	err   error
	calls int
	after func()
}

func (c *deterministicCollector) Collect(_ context.Context, _ MonitorTarget) (CollectedObservation, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	if c.after != nil {
		c.after()
	}
	return c.value, c.err
}

type recordingSink struct {
	mu       sync.Mutex
	signals  []AdvisorySignal
	err      error
	failures int
	result   CompositionResult
}

func (s *recordingSink) Compose(_ context.Context, signal AdvisorySignal) (CompositionResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.signals = append(s.signals, signal)
	if s.failures > 0 {
		s.failures--
		return CompositionResult{}, errors.New("temporary advisory composition failure")
	}
	return s.result, s.err
}

func (s *recordingSink) CaptureSnapshot(_ context.Context, signal AdvisorySignal) (CompositionSnapshot, error) {
	return testPinnedCompositionSnapshot(signal.Run.Scope.OwnerID, signal.Run.FinishedAt)
}

func testPinnedCompositionSnapshot(owner string, at time.Time) (CompositionSnapshot, error) {
	advisor := proactivity.NewServiceWithClock(
		proactivity.NewMemoryRepository(),
		func() time.Time { return at },
	)
	policy, _, err := advisor.RecordPolicy(context.Background(), owner, "test-snapshot-policy", proactivity.DefaultPreferences(owner))
	if err != nil {
		return CompositionSnapshot{}, err
	}
	capturedAt := at.UTC()
	if policy.RecordedAt.After(capturedAt) {
		capturedAt = policy.RecordedAt.UTC()
	}
	attention, err := advisor.CaptureEvaluationSnapshot(context.Background(), owner, capturedAt)
	if err != nil {
		return CompositionSnapshot{}, err
	}
	capturedAt = attention.CapturedAt.UTC()
	value := CompositionSnapshot{
		ContractVersion:    compositionSnapshotVersion,
		Status:             CompositionSnapshotPinned,
		ComposerVersion:    currentComposerVersion,
		CapturedAt:         capturedAt,
		OutcomeRevision:    1,
		OutcomeAuditDigest: strings.Repeat("1", 64),
		Attention:          attention,
	}
	value.SnapshotDigest, err = compositionSnapshotDigest(value)
	return value, err
}

func TestServiceLifecycleExactReplayAndAdvisoryComposition(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 5, 8, 0, 0, 0, time.UTC)
	current := now
	collector := &deterministicCollector{value: CollectedObservation{
		Value: 4, ObservedAt: now.Add(30 * time.Second), SourceDigest: strings.Repeat("a", 64),
	}}
	sink := &recordingSink{}
	service := newService(NewMemoryRepository(), collector, sink, func() time.Time { return current })
	scope := Scope{OwnerID: "owner-robert", WorkspaceID: "workspace-hai"}
	register := testRegisterRequest(scope, now)

	target, created, err := service.RegisterTarget(t.Context(), register)
	if err != nil || !created {
		t.Fatalf("RegisterTarget() = (%+v, %v, %v)", target, created, err)
	}
	assertAdvisoryControl(t, target.Authority)
	replayedTarget, replayCreated, err := service.RegisterTarget(t.Context(), register)
	if err != nil || replayCreated || replayedTarget != target {
		t.Fatalf("RegisterTarget replay = (%+v, %v, %v), want original", replayedTarget, replayCreated, err)
	}
	changed := register
	changed.Cadence = 20 * time.Minute
	if _, _, err := service.RegisterTarget(t.Context(), changed); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("changed exact replay error = %v, want %v", err, ErrIdempotencyConflict)
	}

	claims, err := service.ClaimDue(t.Context(), ClaimDueRequest{
		Scope: scope, WorkerID: "worker-a", Now: now, LeaseDuration: 2 * time.Minute, Limit: 10,
	})
	if err != nil || len(claims) != 1 || claims[0].Lease.Generation != 1 {
		t.Fatalf("ClaimDue() = (%+v, %v)", claims, err)
	}
	if duplicate, err := service.ClaimDue(t.Context(), ClaimDueRequest{Scope: scope, WorkerID: "worker-b", Now: now, LeaseDuration: 2 * time.Minute, Limit: 10}); err != nil || len(duplicate) != 0 {
		t.Fatalf("second ClaimDue() = (%+v, %v), want none", duplicate, err)
	}

	current = now.Add(time.Minute)
	process := ProcessClaimRequest{IdempotencyKey: "complete-run-1", Scope: scope, TargetID: target.ID, WorkerID: "worker-a", LeaseGeneration: 1, CompletedAt: current}
	completion, err := service.ProcessClaim(t.Context(), process)
	if err != nil || !completion.Created || completion.Composed || completion.Composition.Status != CompositionPending {
		t.Fatalf("ProcessClaim() = (%+v, %v)", completion, err)
	}
	compositionResult, err := service.ProcessCompositions(t.Context(), ProcessCompositionsRequest{Scope: scope, WorkerID: "composition-worker-a", Now: current, LeaseDuration: time.Minute, Limit: 1})
	if err != nil || compositionResult.Succeeded != 1 || len(compositionResult.Records) != 1 {
		t.Fatalf("ProcessCompositions() = (%+v, %v)", compositionResult, err)
	}
	if completion.Run.Status != RunCompleted || completion.Observation.Value != 4 || completion.Observation.SourceDigest != strings.Repeat("a", 64) {
		t.Fatalf("unexpected completion: %+v", completion)
	}
	assertAdvisoryControl(t, completion.Authority)
	assertAdvisoryControl(t, completion.Observation.Authority)
	assertAdvisoryControl(t, completion.Run.Authority)

	replay, err := service.ProcessClaim(t.Context(), process)
	if err != nil || replay.Created || !replay.Composed || replay.Run.ID != completion.Run.ID || replay.Observation.ID != completion.Observation.ID {
		t.Fatalf("ProcessClaim replay = (%+v, %v), want immutable original", replay, err)
	}
	if collector.calls != 1 {
		t.Fatalf("collector calls = %d, want replay to use the immutable completion", collector.calls)
	}
	if len(sink.signals) != 1 {
		t.Fatalf("sink calls = %+v, want one durable composition", sink.signals)
	}
	assertAdvisoryControl(t, sink.signals[0].Authority)

	observations, err := service.Observations(t.Context(), scope, target.ID, 10)
	if err != nil || len(observations) != 1 || observations[0].ID != completion.Observation.ID {
		t.Fatalf("Observations() = (%+v, %v)", observations, err)
	}
	runs, err := service.Runs(t.Context(), scope, target.ID, 10)
	if err != nil || len(runs) != 1 || runs[0].ID != completion.Run.ID {
		t.Fatalf("Runs() = (%+v, %v)", runs, err)
	}
	storedTarget, err := service.Target(t.Context(), scope, target.ID)
	if err != nil || storedTarget.Lease.Active() || storedTarget.Lease.Generation != 1 || !storedTarget.NextRunAt.Equal(now.Add(10*time.Minute)) {
		t.Fatalf("stored target after completion = (%+v, %v)", storedTarget, err)
	}
}

func TestCompositionMayDisableOnlyItsOwnExpiredMonitor(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 5, 9, 0, 0, 0, time.UTC)
	current := now
	scope := Scope{OwnerID: "owner-expiry", WorkspaceID: "workspace-expiry"}
	collector := &deterministicCollector{value: CollectedObservation{
		Value: 1, ObservedAt: now, SourceDigest: strings.Repeat("e", 64),
	}}
	sink := &recordingSink{result: CompositionResult{DisableTarget: true}}
	service := newService(NewMemoryRepository(), collector, sink, func() time.Time { return current })
	request := testRegisterRequest(scope, now)
	target, _, err := service.RegisterTarget(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := service.ClaimDue(t.Context(), ClaimDueRequest{
		Scope: scope, WorkerID: "expiry-worker", Now: now, LeaseDuration: time.Minute, Limit: 1,
	})
	if err != nil || len(claims) != 1 {
		t.Fatalf("ClaimDue() = (%+v, %v)", claims, err)
	}
	current = now.Add(30 * time.Second)
	process := ProcessClaimRequest{
		IdempotencyKey: "expiry-completion", Scope: scope, TargetID: target.ID,
		WorkerID: "expiry-worker", LeaseGeneration: claims[0].Lease.Generation, CompletedAt: current,
	}
	completion, err := service.ProcessClaim(t.Context(), process)
	if err != nil || completion.Composed {
		t.Fatalf("ProcessClaim() = (%+v, %v)", completion, err)
	}
	if _, err := service.ProcessCompositions(t.Context(), ProcessCompositionsRequest{Scope: scope, WorkerID: "expiry-composition-worker", Now: current, LeaseDuration: time.Minute, Limit: 1}); err != nil {
		t.Fatal(err)
	}
	stored, err := service.Target(t.Context(), scope, target.ID)
	if err != nil || stored.Enabled || stored.Lease.Active() {
		t.Fatalf("expired monitor remained active: (%+v, %v)", stored, err)
	}
	replay, err := service.ProcessClaim(t.Context(), process)
	if err != nil || replay.Created || !replay.Composed {
		t.Fatalf("expiry replay = (%+v, %v)", replay, err)
	}
}

func TestProcessDueCompletesEveryClaimedTarget(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 5, 8, 0, 0, 0, time.UTC)
	collector := &deterministicCollector{value: CollectedObservation{
		Value: 2, ObservedAt: now, SourceDigest: strings.Repeat("c", 64),
	}}
	service := newService(NewMemoryRepository(), collector, &recordingSink{}, func() time.Time { return now })
	scope := Scope{OwnerID: "owner-batch", WorkspaceID: "workspace-batch"}
	for index, targetID := range []string{"target-one", "target-two"} {
		request := testRegisterRequest(scope, now)
		request.IdempotencyKey = "register-" + targetID
		request.TargetID = targetID
		request.OutcomeID = "outcome-batch"
		request.IndicatorID = fmt.Sprintf("indicator-%d", index+1)
		if _, _, err := service.RegisterTarget(t.Context(), request); err != nil {
			t.Fatalf("register %s: %v", targetID, err)
		}
	}
	dueScopes, err := service.DueScopes(t.Context(), now, 10)
	if err != nil || len(dueScopes) != 1 || dueScopes[0] != scope {
		t.Fatalf("DueScopes() = (%+v, %v)", dueScopes, err)
	}

	result, err := service.ProcessDue(t.Context(), ProcessDueRequest{
		Scope: scope, WorkerID: "worker-batch", Now: now,
		LeaseDuration: time.Minute, Limit: 10,
	})
	if err != nil || result.Claimed != 2 || len(result.Completions) != 2 || len(result.Failures) != 0 || result.Compositions.Succeeded != 2 {
		t.Fatalf("ProcessDue() = (%+v, %v)", result, err)
	}
	assertAdvisoryControl(t, result.Authority)
	if collector.calls != 2 {
		t.Fatalf("collector calls = %d, want 2", collector.calls)
	}
	for _, completion := range result.Completions {
		if !completion.Run.FinishedAt.After(completion.Run.StartedAt) {
			t.Fatalf("completion did not advance beyond claim: started=%s finished=%s", completion.Run.StartedAt, completion.Run.FinishedAt)
		}
	}
}

func TestProcessDueRecordsAfterACollectorObservationCreatedMillisecondsLater(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 5, 8, 0, 0, 0, time.UTC)
	collector := &deterministicCollector{value: CollectedObservation{
		Value: 1, ObservedAt: now.Add(10 * time.Millisecond), SourceDigest: strings.Repeat("f", 64),
	}}
	service := newService(NewMemoryRepository(), collector, &recordingSink{}, func() time.Time { return now })
	scope := Scope{OwnerID: "owner-timestamp", WorkspaceID: "workspace-timestamp"}
	if _, _, err := service.RegisterTarget(t.Context(), testRegisterRequest(scope, now)); err != nil {
		t.Fatal(err)
	}

	result, err := service.ProcessDue(t.Context(), ProcessDueRequest{
		Scope: scope, WorkerID: "worker-timestamp", Now: now,
		LeaseDuration: time.Minute, Limit: 1,
	})
	if err != nil || len(result.Completions) != 1 || len(result.Failures) != 0 {
		t.Fatalf("ProcessDue() = (%+v, %v)", result, err)
	}
	completion := result.Completions[0]
	if completion.Observation.RecordedAt.Before(completion.Observation.ObservedAt) {
		t.Fatalf("recorded_at %s precedes observed_at %s", completion.Observation.RecordedAt, completion.Observation.ObservedAt)
	}
	if !completion.Run.FinishedAt.Equal(completion.Observation.RecordedAt) {
		t.Fatalf("run finished_at %s != observation recorded_at %s", completion.Run.FinishedAt, completion.Observation.RecordedAt)
	}
}

func TestProcessDueComposesNewDeliveryWhenCollectionAdvancesClock(t *testing.T) {
	t.Parallel()
	startedAt := time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC)
	current := startedAt
	sink := &recordingSink{}
	collector := &deterministicCollector{value: CollectedObservation{
		Value: 1, ObservedAt: startedAt.Add(250 * time.Millisecond), SourceDigest: strings.Repeat("9", 64),
	}}
	collector.after = func() { current = collector.value.ObservedAt }
	service := newService(NewMemoryRepository(), collector, sink, func() time.Time { return current })
	scope := Scope{OwnerID: "owner-clock-advance", WorkspaceID: "workspace-clock-advance"}
	if _, _, err := service.RegisterTarget(t.Context(), testRegisterRequest(scope, startedAt)); err != nil {
		t.Fatal(err)
	}

	result, err := service.ProcessDue(t.Context(), ProcessDueRequest{
		Scope: scope, WorkerID: "worker-clock-advance", Now: startedAt,
		LeaseDuration: time.Minute, Limit: 1,
	})
	if err != nil {
		t.Fatalf("ProcessDue() error = %v", err)
	}
	if result.Claimed != 1 || len(result.Completions) != 1 || result.Compositions.Succeeded != 1 {
		t.Fatalf("ProcessDue() = %+v, want collection and composition in one pass", result)
	}
	if !result.Completions[0].Composed || result.Completions[0].Composition.Status != CompositionSucceeded {
		t.Fatalf("completion handoff = %+v, want succeeded", result.Completions[0].Composition)
	}
	if collector.calls != 1 || len(sink.signals) != 1 {
		t.Fatalf("collector calls = %d sink signals = %d, want one of each", collector.calls, len(sink.signals))
	}
}

func TestProcessDueRetriesTransientCompositionWithoutRecollection(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 5, 8, 30, 0, 0, time.UTC)
	collector := &deterministicCollector{value: CollectedObservation{
		Value: 1, ObservedAt: now, SourceDigest: strings.Repeat("d", 64),
	}}
	sink := &recordingSink{failures: 1}
	service := newService(NewMemoryRepository(), collector, sink, func() time.Time { return now })
	scope := Scope{OwnerID: "owner-retry", WorkspaceID: "workspace-retry"}
	if _, _, err := service.RegisterTarget(t.Context(), testRegisterRequest(scope, now)); err != nil {
		t.Fatal(err)
	}
	result, err := service.ProcessDue(t.Context(), ProcessDueRequest{
		Scope: scope, WorkerID: "worker-retry", Now: now,
		LeaseDuration: time.Minute, Limit: 1,
	})
	if err != nil || len(result.Completions) != 1 || len(result.Failures) != 0 {
		t.Fatalf("ProcessDue() = (%+v, %v)", result, err)
	}
	if collector.calls != 1 || len(sink.signals) != 1 || len(result.Compositions.Failures) != 1 || !result.Compositions.Failures[0].Retrying {
		t.Fatalf("first pass collector=%d sink=%d compositions=%+v", collector.calls, len(sink.signals), result.Compositions)
	}
	now = now.Add(time.Minute + time.Second)
	result, err = service.ProcessDue(t.Context(), ProcessDueRequest{Scope: scope, WorkerID: "worker-retry", Now: now, LeaseDuration: time.Minute, Limit: 1})
	if err != nil || result.Claimed != 0 || result.Compositions.Succeeded != 1 || collector.calls != 1 || len(sink.signals) != 2 {
		t.Fatalf("retry pass = (%+v, %v), collector=%d sink=%d", result, err, collector.calls, len(sink.signals))
	}
}

func TestLeaseRecoveryFencesStaleWorkerAndIncrementsGeneration(t *testing.T) {
	t.Parallel()
	current := time.Date(2026, time.August, 5, 9, 0, 0, 0, time.UTC)
	service := newService(NewMemoryRepository(), nil, nil, func() time.Time { return current })
	scope := Scope{OwnerID: "owner-a", WorkspaceID: "workspace-a"}
	target, _, err := service.RegisterTarget(t.Context(), testRegisterRequest(scope, current))
	if err != nil {
		t.Fatal(err)
	}
	claims, err := service.ClaimDue(t.Context(), ClaimDueRequest{Scope: scope, WorkerID: "worker-old", Now: current, LeaseDuration: 30 * time.Second, Limit: 1})
	if err != nil || len(claims) != 1 {
		t.Fatalf("first claim = (%+v, %v)", claims, err)
	}

	current = current.Add(31 * time.Second)
	recovered, err := service.RecoverExpiredLeases(t.Context(), scope, current)
	if err != nil || recovered != 1 {
		t.Fatalf("RecoverExpiredLeases() = (%d, %v)", recovered, err)
	}
	_, err = service.Complete(t.Context(), CompleteRequest{IdempotencyKey: "stale-complete", Scope: scope, TargetID: target.ID, WorkerID: "worker-old", LeaseGeneration: 1, Collected: CollectedObservation{Value: 1, ObservedAt: current, SourceDigest: strings.Repeat("b", 64)}, CompletedAt: current})
	if !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("stale Complete() error = %v, want %v", err, ErrLeaseLost)
	}

	claims, err = service.ClaimDue(t.Context(), ClaimDueRequest{Scope: scope, WorkerID: "worker-new", Now: current, LeaseDuration: time.Minute, Limit: 1})
	if err != nil || len(claims) != 1 || claims[0].Lease.Generation != 2 {
		t.Fatalf("reclaim = (%+v, %v), want generation 2", claims, err)
	}
	run, created, err := service.Fail(t.Context(), FailRequest{IdempotencyKey: "failed-run-2", Scope: scope, TargetID: target.ID, WorkerID: "worker-new", LeaseGeneration: 2, FailureCode: "source_unavailable", FailureSummary: "source snapshot was unavailable", FailedAt: current.Add(10 * time.Second)})
	if err != nil || !created || run.Status != RunFailed {
		t.Fatalf("Fail() = (%+v, %v, %v)", run, created, err)
	}
	replay, replayCreated, err := service.Fail(t.Context(), FailRequest{IdempotencyKey: "failed-run-2", Scope: scope, TargetID: target.ID, WorkerID: "worker-new", LeaseGeneration: 2, FailureCode: "source_unavailable", FailureSummary: "source snapshot was unavailable", FailedAt: current.Add(10 * time.Second)})
	if err != nil || replayCreated || replay.ID != run.ID {
		t.Fatalf("Fail replay = (%+v, %v, %v)", replay, replayCreated, err)
	}
	stored, err := service.Target(t.Context(), scope, target.ID)
	if err != nil || stored.Lease.Generation != 2 || stored.Lease.Active() {
		t.Fatalf("target after failure = (%+v, %v)", stored, err)
	}
}

func TestEnabledStateCancelsLeaseAndNeverReusesGeneration(t *testing.T) {
	t.Parallel()
	current := time.Date(2026, time.August, 5, 9, 30, 0, 0, time.UTC)
	service := newService(NewMemoryRepository(), nil, nil, func() time.Time { return current })
	scope := Scope{OwnerID: "owner-a", WorkspaceID: "workspace-a"}
	target, _, err := service.RegisterTarget(t.Context(), testRegisterRequest(scope, current))
	if err != nil {
		t.Fatal(err)
	}
	claims, err := service.ClaimDue(t.Context(), ClaimDueRequest{Scope: scope, WorkerID: "worker-a", Now: current, LeaseDuration: time.Minute, Limit: 1})
	if err != nil || len(claims) != 1 || claims[0].Lease.Generation != 1 {
		t.Fatalf("first claim = (%+v, %v)", claims, err)
	}
	disabled, created, err := service.SetEnabled(t.Context(), SetEnabledRequest{IdempotencyKey: "disable-target", Scope: scope, TargetID: target.ID, Enabled: false, RequestedAt: current})
	if err != nil || !created || disabled.Enabled || disabled.Lease.Active() || disabled.Lease.Generation != 1 {
		t.Fatalf("disable = (%+v, %v, %v)", disabled, created, err)
	}
	if due, err := service.ClaimDue(t.Context(), ClaimDueRequest{Scope: scope, WorkerID: "worker-b", Now: current, LeaseDuration: time.Minute, Limit: 1}); err != nil || len(due) != 0 {
		t.Fatalf("disabled claim = (%+v, %v)", due, err)
	}
	if _, err := service.Complete(t.Context(), CompleteRequest{IdempotencyKey: "disabled-stale", Scope: scope, TargetID: target.ID, WorkerID: "worker-a", LeaseGeneration: 1, Collected: CollectedObservation{Value: 1, ObservedAt: current, SourceDigest: strings.Repeat("d", 64)}, CompletedAt: current}); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("disabled stale completion error = %v", err)
	}
	enabled, _, err := service.SetEnabled(t.Context(), SetEnabledRequest{IdempotencyKey: "enable-target", Scope: scope, TargetID: target.ID, Enabled: true, RequestedAt: current})
	if err != nil || !enabled.Enabled || enabled.Lease.Generation != 1 {
		t.Fatalf("enable = (%+v, %v)", enabled, err)
	}
	claims, err = service.ClaimDue(t.Context(), ClaimDueRequest{Scope: scope, WorkerID: "worker-b", Now: current, LeaseDuration: time.Minute, Limit: 1})
	if err != nil || len(claims) != 1 || claims[0].Lease.Generation != 2 {
		t.Fatalf("claim after re-enable = (%+v, %v)", claims, err)
	}
}

func TestConcurrentClaimDueLeasesTargetOnce(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 5, 9, 45, 0, 0, time.UTC)
	service := newService(NewMemoryRepository(), nil, nil, func() time.Time { return now })
	scope := Scope{OwnerID: "owner-a", WorkspaceID: "workspace-a"}
	if _, _, err := service.RegisterTarget(t.Context(), testRegisterRequest(scope, now)); err != nil {
		t.Fatal(err)
	}
	type result struct {
		items []MonitorTarget
		err   error
	}
	start := make(chan struct{})
	results := make(chan result, 2)
	for _, worker := range []string{"worker-a", "worker-b"} {
		worker := worker
		go func() {
			<-start
			items, err := service.ClaimDue(context.Background(), ClaimDueRequest{Scope: scope, WorkerID: worker, Now: now, LeaseDuration: time.Minute, Limit: 1})
			results <- result{items: items, err: err}
		}()
	}
	close(start)
	claimed := 0
	for range 2 {
		outcome := <-results
		if outcome.err != nil {
			t.Fatal(outcome.err)
		}
		claimed += len(outcome.items)
	}
	if claimed != 1 {
		t.Fatalf("concurrent claims = %d, want exactly one", claimed)
	}
}

func TestCollectorFailureIsSanitizedAndDoesNotReachSink(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 5, 10, 0, 0, 0, time.UTC)
	collector := &deterministicCollector{err: errors.New("Authorization: Bearer should-not-leak")}
	sink := &recordingSink{}
	service := newService(NewMemoryRepository(), collector, sink, func() time.Time { return now })
	scope := Scope{OwnerID: "owner-a", WorkspaceID: "workspace-a"}
	target, _, err := service.RegisterTarget(t.Context(), testRegisterRequest(scope, now))
	if err != nil {
		t.Fatal(err)
	}
	claims, err := service.ClaimDue(t.Context(), ClaimDueRequest{Scope: scope, WorkerID: "worker-a", Now: now, LeaseDuration: time.Minute, Limit: 1})
	if err != nil || len(claims) != 1 {
		t.Fatalf("claim = (%+v, %v)", claims, err)
	}
	_, err = service.ProcessClaim(t.Context(), ProcessClaimRequest{IdempotencyKey: "collector-failure", Scope: scope, TargetID: target.ID, WorkerID: "worker-a", LeaseGeneration: 1, CompletedAt: now})
	if !errors.Is(err, ErrCollectorFailed) {
		t.Fatalf("ProcessClaim() error = %v", err)
	}
	runs, err := service.Runs(t.Context(), scope, target.ID, 10)
	if err != nil || len(runs) != 1 {
		t.Fatalf("runs = (%+v, %v)", runs, err)
	}
	if runs[0].FailureSummary != "collector returned an error" || strings.Contains(strings.ToLower(runs[0].FailureSummary), "bearer") {
		t.Fatalf("unsafe failure summary: %q", runs[0].FailureSummary)
	}
	if len(sink.signals) != 0 {
		t.Fatalf("sink received %d signals after failed collection", len(sink.signals))
	}
}

func TestSinkFailureLeavesSourceBackedCompletionDurable(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 5, 11, 0, 0, 0, time.UTC)
	collector := &deterministicCollector{value: CollectedObservation{Value: 2, ObservedAt: now, SourceDigest: strings.Repeat("c", 64)}}
	sink := &recordingSink{err: errors.New("composition unavailable")}
	service := newService(NewMemoryRepository(), collector, sink, func() time.Time { return now })
	scope := Scope{OwnerID: "owner-a", WorkspaceID: "workspace-a"}
	target, _, err := service.RegisterTarget(t.Context(), testRegisterRequest(scope, now))
	if err != nil {
		t.Fatal(err)
	}
	claims, err := service.ClaimDue(t.Context(), ClaimDueRequest{Scope: scope, WorkerID: "worker-a", Now: now, LeaseDuration: time.Minute, Limit: 1})
	if err != nil || len(claims) != 1 {
		t.Fatalf("claim = (%+v, %v)", claims, err)
	}
	completion, err := service.ProcessClaim(t.Context(), ProcessClaimRequest{IdempotencyKey: "sink-failure", Scope: scope, TargetID: target.ID, WorkerID: "worker-a", LeaseGeneration: 1, CompletedAt: now})
	if err != nil || !completion.Created || completion.Composed || completion.Composition.Status != CompositionPending {
		t.Fatalf("ProcessClaim() = (%+v, %v)", completion, err)
	}
	compositionResult, err := service.ProcessCompositions(t.Context(), ProcessCompositionsRequest{Scope: scope, WorkerID: "composition-worker-a", Now: now, LeaseDuration: time.Minute, Limit: 1})
	if err != nil || len(compositionResult.Failures) != 1 || !compositionResult.Failures[0].Retrying {
		t.Fatalf("failed composition = (%+v, %v)", compositionResult, err)
	}
	observations, listErr := service.Observations(t.Context(), scope, target.ID, 10)
	if listErr != nil || len(observations) != 1 {
		t.Fatalf("durable observations = (%+v, %v)", observations, listErr)
	}
	sink.err = nil
	now = now.Add(time.Minute + time.Second)
	service.now = func() time.Time { return now }
	compositionResult, err = service.ProcessCompositions(t.Context(), ProcessCompositionsRequest{Scope: scope, WorkerID: "composition-worker-b", Now: now, LeaseDuration: time.Minute, Limit: 1})
	if err != nil || compositionResult.Succeeded != 1 {
		t.Fatalf("composition recovery = (%+v, %v)", compositionResult, err)
	}
	replay, err := service.ProcessClaim(t.Context(), ProcessClaimRequest{IdempotencyKey: "sink-failure", Scope: scope, TargetID: target.ID, WorkerID: "worker-a", LeaseGeneration: 1, CompletedAt: now})
	if err != nil || replay.Created || !replay.Composed || replay.Run.ID != completion.Run.ID {
		t.Fatalf("sink recovery replay = (%+v, %v)", replay, err)
	}
	observations, listErr = service.Observations(t.Context(), scope, target.ID, 10)
	if listErr != nil || len(observations) != 1 {
		t.Fatalf("sink retry created duplicate observations: (%+v, %v)", observations, listErr)
	}
	if collector.calls != 1 {
		t.Fatalf("collector calls = %d, want sink retry without recollection", collector.calls)
	}
}

func testRegisterRequest(scope Scope, now time.Time) RegisterTargetRequest {
	return RegisterTargetRequest{IdempotencyKey: "register-target-1", Scope: scope, TargetID: "target-open-loops", OutcomeID: "outcome-reliable-work", IndicatorID: "indicator-open-loops", SourceKind: SourceWorkflowOpenLoopCount, Enabled: true, Cadence: 10 * time.Minute, FirstRunAt: now, RequestedAt: now}
}

func assertAdvisoryControl(t *testing.T, control AuthorityControl) {
	t.Helper()
	if control.Label != AuthorityLabel || control.CanExecute || control.CanDeliver || control.CanNotify || control.CanWriteCalendar || control.CanMutateWorkflow || control.CanAuthorizeMandate || control.CanMutateLearning {
		t.Fatalf("unsafe authority control: %+v", control)
	}
}
