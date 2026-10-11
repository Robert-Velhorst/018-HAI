package durablejob

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"automation-hub-backend/internal/lifecycle"
	"automation-hub-backend/internal/models"
)

func TestRuntimeOwnsHandlerAfterPollingLoopReturns(t *testing.T) {
	for _, reason := range []string{"cancellation", "lease-loss"} {
		t.Run(reason, func(t *testing.T) {
			g := lifecycle.New(context.Background())
			release, entered := make(chan struct{}), make(chan struct{})
			var once sync.Once
			defer func() {
				once.Do(func() { close(release) })
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				if err := g.StopAndWait(ctx); err != nil {
					t.Error(err)
				}
			}()
			var repo Repository = newFakeRepo()
			lease := time.Minute
			if reason == "lease-loss" {
				repo = &leaseLosingRepo{fakeRepo: newFakeRepo()}
				lease = 15 * time.Millisecond
			}
			runner := NewRunner(repo, Options{WorkerID: "owned-test", Lease: lease})
			runner.Register("held", func(context.Context, models.DurableJob) error {
				close(entered)
				<-release
				return nil
			})
			if _, err := runner.Enqueue("held", "{}", time.Time{}, 3); err != nil {
				t.Fatal(err)
			}
			loop := make(chan error, 1)
			if !lifecycle.Go(g.Context(), "poll-loop", func() {
				_, err := runner.RunOnce(g.Context())
				loop <- err
			}) {
				t.Fatal("loop admission refused")
			}
			select {
			case <-entered:
			case <-time.After(time.Second):
				t.Fatal("handler not started")
			}
			if reason == "cancellation" {
				g.Stop()
			}
			select {
			case err := <-loop:
				if reason == "cancellation" && !errors.Is(err, context.Canceled) || reason == "lease-loss" && err != nil {
					t.Fatalf("loop outcome = %v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("poll loop did not return")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Millisecond)
			defer cancel()
			if err := g.StopAndWait(ctx); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("handler escaped join after %s: %v", reason, err)
			}
			once.Do(func() { close(release) })
		})
	}
}
