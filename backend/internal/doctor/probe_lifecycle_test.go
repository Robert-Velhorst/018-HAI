package doctor

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"automation-hub-backend/internal/lifecycle"
)

func TestTimedOutReadinessProbeRemainsOwnedUntilActualReturn(t *testing.T) {
	g := lifecycle.New(context.Background())
	release := make(chan struct{})
	var once sync.Once
	defer func() {
		once.Do(func() { close(release) })
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := g.StopAndWait(ctx); err != nil {
			t.Error(err)
		}
	}()
	checks := RunProbes(g.Context(), 15*time.Millisecond, []Probe{{Name: "synthetic-held", Critical: true, Run: func(context.Context) error {
		<-release
		return nil
	}}})
	if len(checks) != 1 || checks[0].Severity != SeverityFail {
		t.Fatal("timeout reported readiness")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Millisecond)
	defer cancel()
	if err := g.StopAndWait(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timed-out probe escaped join: %v", err)
	}
	once.Do(func() { close(release) })
}
