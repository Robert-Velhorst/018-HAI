package openclawmaintenance

import (
	"context"
	"errors"
	"testing"
)

func TestCancelledContainedProcessIsNotResumed(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	resumed := false

	err := resumeContainedThreadIfActive(ctx, func() (uint32, error) {
		resumed = true
		return 1, nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("resume error = %v, want context cancellation", err)
	}
	if resumed {
		t.Fatal("cancelled installer thread was resumed")
	}
}
