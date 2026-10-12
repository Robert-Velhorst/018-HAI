package llm

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"automation-hub-backend/internal/lifecycle"
)

func TestScheduledPermitMonitorsRemainOwnedAndDoNotStartWorkAfterCancellation(t *testing.T) {
	for _, mode := range []string{"maintenance", "provider-probe"} {
		t.Run(mode, func(t *testing.T) {
			g := lifecycle.New(context.Background())
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			defer func() {
				once.Do(func() { close(release) })
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				if err := g.StopAndWait(ctx); err != nil {
					t.Error(err)
				}
			}()
			var checks, executions atomic.Int32
			allowed := func() bool {
				if checks.Add(1) == 2 {
					close(entered)
					<-release
				}
				return true
			}
			done := make(chan bool, 1)
			go func() {
				if mode == "maintenance" {
					r := runScheduledModelMaintenance(g.Context(), allowed, func(context.Context) ModelMaintenanceRun {
						executions.Add(1)
						return ModelMaintenanceRun{}
					})
					done <- r.Cancelled
				} else {
					err := runScheduledProviderProbe(g.Context(), allowed, func(context.Context) error { executions.Add(1); return nil })
					done <- errors.Is(err, context.Canceled)
				}
			}()
			select {
			case <-entered:
			case <-time.After(time.Second):
				t.Fatal("permission monitor did not enter held callback")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Millisecond)
			defer cancel()
			if err := g.StopAndWait(ctx); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("permission callback escaped ownership: %v", err)
			}
			once.Do(func() { close(release) })
			select {
			case cancelled := <-done:
				if !cancelled || executions.Load() != 0 {
					t.Fatal("work started after shutdown during permission check")
				}
			case <-time.After(time.Second):
				t.Fatal("canceled permission monitor did not finish")
			}
		})
	}
}

func TestScheduledPermitMonitorsRefuseStoppedOwnershipWithLiveCaller(t *testing.T) {
	g := lifecycle.New(context.Background())
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := g.StopAndWait(ctx); err != nil {
		t.Fatal(err)
	}
	live := g.WithContext(context.Background())
	if run := runScheduledModelMaintenance(live, func() bool { return true }, func(context.Context) ModelMaintenanceRun {
		t.Error("maintenance ran after owner stopped")
		return ModelMaintenanceRun{}
	}); !run.Cancelled {
		t.Fatal("stopped maintenance did not report cancellation")
	}
	if err := runScheduledProviderProbe(live, func() bool { return true }, func(context.Context) error {
		t.Error("probe ran after owner stopped")
		return nil
	}); !errors.Is(err, context.Canceled) {
		t.Fatalf("stopped probe = %v", err)
	}
}
