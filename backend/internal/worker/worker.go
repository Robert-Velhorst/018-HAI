// Package worker provides a small, testable retry runner for background jobs. It
// composes the backoff policy for delay computation and takes an injected sleep
// function so retry timing is deterministic in tests (no real waiting).
package worker

import (
	"context"
	"errors"

	"automation-hub-backend/internal/backoff"
)

// SleepFunc waits for the given duration. Tests inject a recording no-op.
type SleepFunc func(nanoseconds int64)

// RunWithRetry runs fn until it succeeds or maxAttempts is reached, sleeping the
// backoff delay between attempts. It returns the number of attempts made and the
// final error (nil on success). fn receives the 1-based attempt number.
func RunWithRetry(maxAttempts int, policy backoff.Policy, sleep SleepFunc, fn func(attempt int) error) (int, error) {
	if maxAttempts < 1 {
		maxAttempts = 1
	}
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		lastErr = fn(attempt)
		if lastErr == nil {
			return attempt, nil
		}
		if errors.Is(lastErr, context.Canceled) {
			return attempt, lastErr
		}
		if attempt < maxAttempts {
			if sleep != nil {
				sleep(int64(policy.Delay(attempt)))
			}
		}
	}
	return maxAttempts, lastErr
}
