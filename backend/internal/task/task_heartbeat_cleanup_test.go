package task

import (
	"sync"
	"testing"
	"time"
)

func TestTaskHeartbeatStopIsIdempotentAndSafeForDeferredCleanup(t *testing.T) {
	r := NewMemoryTaskStateRepository()
	svc := newDurableTaskTestService(t, r, nil).(*service)
	claim, err := r.ClaimTaskOperation("alice", "synthetic-heartbeat", stringsOfLength("a", 64), "run", "worker", time.Now().UTC(), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	stop := svc.startTaskOperationHeartbeat(claim, "worker")
	var closers sync.WaitGroup
	for i := 0; i < 8; i++ {
		closers.Add(1)
		go func() {
			defer closers.Done()
			if stop() {
				t.Error("idle heartbeat reported lost lease")
			}
		}()
	}
	closers.Wait()
	if stop() {
		t.Fatal("repeat stop changed lease outcome")
	}
}

func TestTaskOperationPanicPropagatesWithoutReexecutingUnknownOutcome(t *testing.T) {
	r := NewMemoryTaskStateRepository()
	svc := newDurableTaskTestService(t, r, nil).(*service)
	request := IntakeRequest{OwnerIdentity: "alice", IdempotencyKey: "synthetic-panic", Request: "Prepare local task plan", ProjectKey: "018-HAI"}
	func() {
		defer func() {
			if recover() != "synthetic panic" {
				t.Error("operation panic was swallowed or changed")
			}
		}()
		_, _ = svc.withTaskOperation(request, "run", func(IntakeRequest) (*CompletionPlan, error) { panic("synthetic panic") })
	}()
	if _, err := svc.withTaskOperation(request, "run", func(IntakeRequest) (*CompletionPlan, error) {
		t.Error("unsettled operation was executed twice")
		return nil, nil
	}); err != ErrTaskOperationInProgress {
		t.Fatalf("unsettled operation = %v", err)
	}
}
