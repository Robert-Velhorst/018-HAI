// Package ratelimit provides a small, dependency-free fixed-window rate limiter
// keyed by an arbitrary client identifier (typically the client IP). The limiter
// takes the current time as a parameter so its behaviour is fully deterministic
// and unit-testable without sleeping.
package ratelimit

import (
	"sync"
	"time"
)

const defaultMaxEntries = 4096

// Limiter is a per-key fixed-window counter. It is safe for concurrent use.
type Limiter struct {
	mu         sync.Mutex
	limit      int
	window     time.Duration
	maxEntries int
	counts     map[string]*windowCount
}

type windowCount struct {
	windowStart time.Time
	count       int
}

// New returns a Limiter allowing up to limit requests per window per key.
// A non-positive limit or window yields a disabled limiter that always allows.
func New(limit int, window time.Duration) *Limiter {
	return &Limiter{
		limit:      limit,
		window:     window,
		maxEntries: defaultMaxEntries,
		counts:     make(map[string]*windowCount),
	}
}

// Enabled reports whether the limiter enforces any limit.
func (l *Limiter) Enabled() bool {
	return l.limit > 0 && l.window > 0
}

// Allow records a request for key at time now and reports whether it is within
// the limit. A disabled limiter always allows. The window resets once now has
// advanced past the current window for that key.
func (l *Limiter) Allow(key string, now time.Time) bool {
	if !l.Enabled() {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	entry, ok := l.counts[key]
	if !ok || now.Sub(entry.windowStart) >= l.window {
		if !ok {
			l.makeRoom(now)
		}
		l.counts[key] = &windowCount{windowStart: now, count: 1}
		return true
	}
	if entry.count >= l.limit {
		return false
	}
	entry.count++
	return true
}

// makeRoom bounds local fallback memory. Expired windows are removed first;
// if every entry is still active, the oldest window is evicted. Rotating
// arbitrary client keys can therefore weaken only their own local fallback
// accounting, not grow the process without bound.
func (l *Limiter) makeRoom(now time.Time) {
	if l.maxEntries <= 0 || len(l.counts) < l.maxEntries {
		return
	}
	for key, entry := range l.counts {
		if now.Sub(entry.windowStart) >= l.window {
			delete(l.counts, key)
		}
	}
	if len(l.counts) < l.maxEntries {
		return
	}
	var oldestKey string
	var oldest time.Time
	for key, entry := range l.counts {
		if oldestKey == "" || entry.windowStart.Before(oldest) {
			oldestKey = key
			oldest = entry.windowStart
		}
	}
	if oldestKey != "" {
		delete(l.counts, oldestKey)
	}
}

// Remaining returns how many requests key may still make in its current window
// at time now. A disabled limiter reports -1 (unbounded).
func (l *Limiter) Remaining(key string, now time.Time) int {
	if !l.Enabled() {
		return -1
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	entry, ok := l.counts[key]
	if !ok || now.Sub(entry.windowStart) >= l.window {
		return l.limit
	}
	remaining := l.limit - entry.count
	if remaining < 0 {
		return 0
	}
	return remaining
}
