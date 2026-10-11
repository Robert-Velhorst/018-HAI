package ratelimit

import (
	"testing"
	"time"
)

func TestDisabledLimiterAlwaysAllows(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	l := New(0, time.Minute)
	if l.Enabled() {
		t.Fatalf("limiter with zero limit should be disabled")
	}
	for i := 0; i < 1000; i++ {
		if !l.Allow("ip", now) {
			t.Fatalf("disabled limiter denied a request")
		}
	}
	if l.Remaining("ip", now) != -1 {
		t.Fatalf("disabled Remaining = %d, want -1", l.Remaining("ip", now))
	}
}

func TestAllowEnforcesLimitWithinWindow(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	l := New(3, time.Minute)
	for i := 0; i < 3; i++ {
		if !l.Allow("ip", now) {
			t.Fatalf("request %d denied within limit", i+1)
		}
	}
	if l.Allow("ip", now) {
		t.Fatalf("4th request allowed; limit is 3")
	}
	if r := l.Remaining("ip", now); r != 0 {
		t.Fatalf("Remaining after limit = %d, want 0", r)
	}
}

func TestWindowResetsAfterElapsed(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	l := New(2, time.Minute)
	l.Allow("ip", start)
	l.Allow("ip", start)
	if l.Allow("ip", start.Add(30*time.Second)) {
		t.Fatalf("request within same window over limit was allowed")
	}
	// Once the window elapses, the counter resets.
	if !l.Allow("ip", start.Add(61*time.Second)) {
		t.Fatalf("request after window elapsed was denied")
	}
	if r := l.Remaining("ip", start.Add(61*time.Second)); r != 1 {
		t.Fatalf("Remaining in fresh window = %d, want 1", r)
	}
}

func TestKeysAreIsolated(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	l := New(1, time.Minute)
	if !l.Allow("a", now) || !l.Allow("b", now) {
		t.Fatalf("distinct keys should each get their own budget")
	}
	if l.Allow("a", now) {
		t.Fatalf("key a exceeded its own limit")
	}
}

func TestLimiterBoundsUniqueKeyMemory(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	l := New(1, time.Minute)
	l.maxEntries = 2

	if !l.Allow("oldest", start) || !l.Allow("newer", start.Add(time.Second)) || !l.Allow("newest", start.Add(2*time.Second)) {
		t.Fatal("first request for a new key should be allowed")
	}
	if len(l.counts) != 2 {
		t.Fatalf("stored keys = %d, want bounded size 2", len(l.counts))
	}
	if _, exists := l.counts["oldest"]; exists {
		t.Fatal("oldest active key was not evicted at the memory ceiling")
	}
}

func TestLimiterPrunesExpiredKeysBeforeEvictingActiveOnes(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	l := New(1, time.Minute)
	l.maxEntries = 2
	l.Allow("expired", start)
	l.Allow("active", start.Add(61*time.Second))
	l.Allow("new", start.Add(62*time.Second))

	if _, exists := l.counts["expired"]; exists {
		t.Fatal("expired key remained after the limiter reached its memory ceiling")
	}
	if _, exists := l.counts["active"]; !exists {
		t.Fatal("active key was evicted while an expired key was available")
	}
}
