package safety

import (
	"context"
	"errors"
	"sync"
	"testing"
)

// Capture a clear context snapshot, then cancel before lock admission. This
// deterministically exercises cancellation between the first check and TryRLock.
type cancelAfterFenceContextCheck struct {
	context.Context
	cancel context.CancelFunc
	once   sync.Once
}

func (c *cancelAfterFenceContextCheck) Err() error {
	err := c.Context.Err()
	c.once.Do(c.cancel)
	return err
}

func TestExecutionCommitFenceRejectsCancellationDuringAdmission(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx := &cancelAfterFenceContextCheck{Context: parent, cancel: cancel}
	release, err := AcquireExecutionCommitFenceContext(ctx)
	if release != nil {
		release()
	}
	if !errors.Is(err, context.Canceled) || release != nil {
		t.Fatalf("cancelled admission returned release=%v err=%v", release != nil, err)
	}
	if !emergencyStopFence.TryLock() {
		t.Fatal("cancelled admission retained its read lock")
	}
	emergencyStopFence.Unlock()
}

func TestExecutionCommitFenceStillAdmitsLiveContext(t *testing.T) {
	release, err := AcquireExecutionCommitFenceContext(context.Background())
	if err != nil || release == nil {
		t.Fatalf("live admission returned release=%v err=%v", release != nil, err)
	}
	if emergencyStopFence.TryLock() {
		emergencyStopFence.Unlock()
		release()
		t.Fatal("successful admission did not retain its read lock")
	}
	release()
	if !emergencyStopFence.TryLock() {
		t.Fatal("release did not free the admission read lock")
	}
	emergencyStopFence.Unlock()
}
