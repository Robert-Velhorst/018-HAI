package worker

import (
	"context"
	"errors"
	"testing"
	"time"

	"automation-hub-backend/internal/backoff"
)

func TestRetriesUntilSuccess(t *testing.T) {
	policy := backoff.Policy{Base: time.Second, Factor: 2, Max: time.Minute}
	var sleeps []int64
	sleep := func(ns int64) { sleeps = append(sleeps, ns) }

	// Fail the first two attempts, succeed on the third.
	attempts, err := RunWithRetry(5, policy, sleep, func(attempt int) error {
		if attempt < 3 {
			return errors.New("transient")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d, want 3", attempts)
	}
	// Two failed attempts => two backoff sleeps of 1s then 2s.
	if len(sleeps) != 2 || sleeps[0] != int64(time.Second) || sleeps[1] != int64(2*time.Second) {
		t.Fatalf("sleeps = %v, want [1s 2s]", sleeps)
	}
}

func TestStopsAfterMaxAttempts(t *testing.T) {
	policy := backoff.DefaultPolicy()
	calls := 0
	attempts, err := RunWithRetry(3, policy, func(int64) {}, func(int) error {
		calls++
		return errors.New("always fails")
	})
	if err == nil {
		t.Fatalf("expected failure after exhausting attempts")
	}
	if attempts != 3 || calls != 3 {
		t.Fatalf("attempts=%d calls=%d, want 3/3", attempts, calls)
	}
}

func TestNoSleepAfterFinalAttempt(t *testing.T) {
	sleeps := 0
	RunWithRetry(2, backoff.DefaultPolicy(), func(int64) { sleeps++ }, func(int) error { return errors.New("x") })
	// 2 attempts => only 1 inter-attempt sleep.
	if sleeps != 1 {
		t.Fatalf("sleeps = %d, want 1 (no sleep after the last attempt)", sleeps)
	}
}

func TestCancellationStopsRetries(t *testing.T) {
	calls := 0
	sleeps := 0
	wrappedCancelErr := errors.Join(errors.New("worker stopped"), context.Canceled)

	attempts, err := RunWithRetry(3, backoff.DefaultPolicy(), func(int64) { sleeps++ }, func(int) error {
		calls++
		return wrappedCancelErr
	})

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if attempts != 1 || calls != 1 {
		t.Fatalf("attempts=%d calls=%d, want 1/1 after cancellation", attempts, calls)
	}
	if sleeps != 0 {
		t.Fatalf("sleeps=%d, want 0 after cancellation", sleeps)
	}
}
