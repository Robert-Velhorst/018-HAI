package safety

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"automation-hub-backend/internal/lifecycle"
)

func TestEmergencyStopMonitorRetainsHeldControlReadUntilReturn(t *testing.T) {
	for _, key := range []string{"HAI_EMERGENCY_STOP", "AUTONOMY_EMERGENCY_STOP", "EMERGENCY_STOP"} {
		t.Setenv(key, "false")
	}
	g := lifecycle.New(context.Background())
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	var reads atomic.Int32
	restore := SetEmergencyStopProvider(EmergencyStopProviderFunc(func() (bool, string, error) {
		if reads.Add(1) == 2 {
			close(entered)
			<-release
		}
		return false, "", nil
	}))
	defer restore()
	defer func() {
		once.Do(func() { close(release) })
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := g.StopAndWait(ctx); err != nil {
			t.Error(err)
		}
	}()
	_, cancel := WithEmergencyStop(g.Context())
	defer cancel()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("emergency-stop monitor did not enter control read")
	}
	cancel()
	ctx, stop := context.WithTimeout(context.Background(), 15*time.Millisecond)
	defer stop()
	if err := g.StopAndWait(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("held emergency-stop read escaped join: %v", err)
	}
	before := reads.Load()
	late, lateCancel := WithEmergencyStop(g.WithContext(context.Background()))
	defer lateCancel()
	if late.Err() == nil || reads.Load() != before {
		t.Fatal("stopped owner permitted a new control read or clear execution context")
	}
	once.Do(func() { close(release) })
}
