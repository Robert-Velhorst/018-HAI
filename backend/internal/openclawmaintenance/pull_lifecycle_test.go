package openclawmaintenance

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"automation-hub-backend/internal/lifecycle"
)

func TestUnconfirmedInstallerCancellationRetainsRuntimeOwnership(t *testing.T) {
	g := lifecycle.New(context.Background())
	ctx, cancel := context.WithCancel(g.Context())
	defer cancel()
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer func() {
		once.Do(func() { close(release) })
		drain, stop := context.WithTimeout(context.Background(), time.Second)
		defer stop()
		if err := g.StopAndWait(drain); err != nil {
			t.Error(err)
		}
	}()
	done := make(chan maintenanceExecutionResult, 1)
	go func() {
		report, err := executeWithPermitMonitor(ctx, Job{Kind: "apply"}, time.Hour, 15*time.Millisecond,
			func(context.Context) error { return nil }, func(context.Context) (Report, error) {
				close(entered)
				<-release
				return Report{}, nil
			})
		done <- maintenanceExecutionResult{report: report, err: err}
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("synthetic installer did not enter")
	}
	cancel()
	select {
	case result := <-done:
		if !errors.Is(result.err, ErrExecutionCancellationUnconfirmed) || result.report.Outcome != "needs_review" {
			t.Fatalf("unconfirmed execution reported a settled outcome: %+v, %v", result.report, result.err)
		}
	case <-time.After(time.Second):
		t.Fatal("installer cancellation monitor did not return")
	}
	drain, stop := context.WithTimeout(context.Background(), 15*time.Millisecond)
	defer stop()
	if err := g.StopAndWait(drain); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("unreturned installer escaped join: %v", err)
	}
	if _, err := executeWithPermitMonitor(g.WithContext(context.Background()), Job{Kind: "apply"}, time.Hour, time.Second,
		func(context.Context) error { return nil }, func(context.Context) (Report, error) {
			t.Error("installer executed after owner stopped")
			return Report{}, nil
		}); !errors.Is(err, context.Canceled) {
		t.Fatalf("late installer = %v", err)
	}
	once.Do(func() { close(release) })
}
