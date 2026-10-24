package llm

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestScheduledMaintenanceCancellationCannotBeReportedAsSuccess(t *testing.T) {
	for _, stage := range []string{"before run", "parent during run", "permission during run", "successful run"} {
		t.Run(stage, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			var allowed atomic.Bool
			allowed.Store(true)
			if stage == "before run" {
				cancel()
			}
			called := false
			result := runScheduledModelMaintenance(ctx, allowed.Load, func(runCtx context.Context) ModelMaintenanceRun {
				called = true
				switch stage {
				case "parent during run":
					cancel()
					<-runCtx.Done()
				case "permission during run":
					allowed.Store(false)
					<-runCtx.Done()
				}
				// The wrapper must not depend on the callback echoing cancellation.
				return ModelMaintenanceRun{Checked: 1}
			})
			if stage == "before run" && called {
				t.Fatal("cancelled entry ran maintenance")
			}
			if stage != "before run" && !called {
				t.Fatal("permitted maintenance did not start")
			}
			wantCancelled := stage != "successful run"
			if result.Cancelled != wantCancelled {
				t.Fatalf("Cancelled=%t want %t", result.Cancelled, wantCancelled)
			}
			if called && result.Checked != 1 {
				t.Fatal("cancellation discarded completed observation counts")
			}
			if stage == "permission during run" && ctx.Err() != nil {
				t.Fatal("permission monitor did not cancel before the safety deadline")
			}
		})
	}
}
