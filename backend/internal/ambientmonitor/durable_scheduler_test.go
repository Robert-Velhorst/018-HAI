package ambientmonitor

import (
	"context"
	"errors"
	"testing"
	"time"
)

type schedulerStub struct {
	scopes               []Scope
	compositionScopes    []Scope
	recovered            int
	compositionRecovered int
	processed            int
	recoverResult        int
	compositionResult    int
	processResult        ProcessDueResult
	processError         error
}

type sweepObserverStub struct{ observations []SweepMetrics }

func (s *sweepObserverStub) ObserveOutcomeMonitorSweep(observation SweepMetrics) {
	s.observations = append(s.observations, observation)
}

func (s *schedulerStub) DueScopes(context.Context, time.Time, int) ([]Scope, error) {
	return append([]Scope(nil), s.scopes...), nil
}
func (s *schedulerStub) PendingCompositionScopes(context.Context, time.Time, int) ([]Scope, error) {
	return append([]Scope(nil), s.compositionScopes...), nil
}
func (s *schedulerStub) RecoverExpiredLeases(context.Context, Scope, time.Time) (int, error) {
	s.recovered++
	return s.recoverResult, nil
}
func (s *schedulerStub) RecoverExpiredCompositionLeases(context.Context, Scope, time.Time) (int, error) {
	s.compositionRecovered++
	return s.compositionResult, nil
}
func (s *schedulerStub) ProcessDue(context.Context, ProcessDueRequest) (ProcessDueResult, error) {
	s.processed++
	if s.processed > 1 && (s.processResult.Claimed > 0 || s.processResult.Compositions.Claimed > 0) {
		return ProcessDueResult{Authority: advisoryAuthority()}, s.processError
	}
	result := s.processResult
	result.Authority = advisoryAuthority()
	return result, s.processError
}

func TestRunMonitorSweepProcessesEveryDueScope(t *testing.T) {
	stub := &schedulerStub{scopes: []Scope{{OwnerID: "owner-a", WorkspaceID: "workspace-a"}, {OwnerID: "owner-b", WorkspaceID: "workspace-b"}}}
	observer := &sweepObserverStub{}
	if err := runMonitorSweep(t.Context(), stub, time.Date(2026, time.August, 5, 9, 0, 0, 0, time.UTC), func() bool { return true }, observer); err != nil {
		t.Fatal(err)
	}
	if stub.recovered != 2 || stub.compositionRecovered != 2 || stub.processed != 2 {
		t.Fatalf("recovered=%d compositionRecovered=%d processed=%d, want 2/2/2", stub.recovered, stub.compositionRecovered, stub.processed)
	}
	if len(observer.observations) != 1 || observer.observations[0].Result != SweepResultCompleted || observer.observations[0].DueCollectionScopes != 2 {
		t.Fatalf("observations = %#v", observer.observations)
	}
}

func TestRunMonitorSweepObservesRecoveryAndProcessingOutcomes(t *testing.T) {
	scope := Scope{OwnerID: "owner-a", WorkspaceID: "workspace-a"}
	stub := &schedulerStub{
		scopes: []Scope{scope}, compositionScopes: []Scope{scope},
		recoverResult: 2, compositionResult: 1,
		processResult: ProcessDueResult{
			Claimed:     1,
			Completions: []Completion{{}},
			Compositions: ProcessCompositionsResult{
				Claimed: 2, Succeeded: 1,
				Failures: []CompositionFailure{{Retrying: true}},
			},
		},
	}
	observer := &sweepObserverStub{}
	if err := runMonitorSweep(t.Context(), stub, time.Now().UTC(), func() bool { return true }, observer); err != nil {
		t.Fatal(err)
	}
	if len(observer.observations) != 1 {
		t.Fatalf("observations = %#v", observer.observations)
	}
	got := observer.observations[0]
	if got.DueCollectionScopes != 1 || got.DueCompositionScopes != 1 ||
		got.CollectionLeasesRecovered != 2 || got.CompositionLeasesRecovered != 1 ||
		got.CollectionClaimed != 1 || got.CollectionCompleted != 1 ||
		got.CompositionClaimed != 2 || got.CompositionSucceeded != 1 || got.CompositionRetrying != 1 {
		t.Fatalf("observation = %#v", got)
	}
}

func TestRunMonitorSweepReturnsSanitizedAggregateFailure(t *testing.T) {
	stub := &schedulerStub{scopes: []Scope{{OwnerID: "owner-a", WorkspaceID: "workspace-a"}}, processError: errors.New("Authorization: Bearer secret")}
	observer := &sweepObserverStub{}
	err := runMonitorSweep(t.Context(), stub, time.Date(2026, time.August, 5, 9, 0, 0, 0, time.UTC), func() bool { return true }, observer)
	if err == nil || err.Error() != "ambient outcome sweep failed for 1 scoped batch(es)" {
		t.Fatalf("error = %v", err)
	}
	if len(observer.observations) != 1 || observer.observations[0].Result != SweepResultFailed {
		t.Fatalf("observations = %#v", observer.observations)
	}
}

func TestRunMonitorSweepStopsWhenPermissionIsWithdrawnMidSweep(t *testing.T) {
	stub := &schedulerStub{scopes: []Scope{
		{OwnerID: "owner-a", WorkspaceID: "workspace-a"},
		{OwnerID: "owner-b", WorkspaceID: "workspace-b"},
	}}
	allowedChecks := 0
	allowed := func() bool {
		allowedChecks++
		return stub.processed == 0
	}
	observer := &sweepObserverStub{}
	if err := runMonitorSweep(t.Context(), stub, time.Date(2026, time.August, 5, 9, 0, 0, 0, time.UTC), allowed, observer); err != nil {
		t.Fatal(err)
	}
	if stub.processed != 1 || stub.recovered != 1 || stub.compositionRecovered != 1 {
		t.Fatalf("processed=%d recovered=%d compositionRecovered=%d, want 1/1/1 after permission withdrawal", stub.processed, stub.recovered, stub.compositionRecovered)
	}
	if allowedChecks < 3 {
		t.Fatalf("allowed checks = %d, want checks before and during scoped work", allowedChecks)
	}
	if len(observer.observations) != 1 || observer.observations[0].Result != SweepResultInterrupted {
		t.Fatalf("observations = %#v", observer.observations)
	}
}

func TestMonitorSchedulerEnvironmentBounds(t *testing.T) {
	t.Setenv("OUTCOME_MONITOR_BATCH_LIMIT", "999")
	t.Setenv("OUTCOME_MONITOR_POLL_SECONDS", "0")
	t.Setenv("OUTCOME_MONITOR_SCHEDULER_ENABLED", "off")
	if monitorBatchLimit() != 20 || monitorPollInterval() != 15*time.Second || DurableSchedulerEnabled() {
		t.Fatal("invalid scheduler environment did not fall back safely")
	}
}
