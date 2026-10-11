package ratelimit

import (
	"context"
	"errors"
	"testing"
	"time"
)

// fakeCounter is a deterministic stand-in for Redis: it counts hits per key in
// memory and can be told to fail, so the limiter's decision logic is testable
// without a running Redis.
type fakeCounter struct {
	counts map[string]int64
	reset  time.Duration
	err    error
}

func newFakeCounter() *fakeCounter {
	return &fakeCounter{counts: map[string]int64{}, reset: 30 * time.Second}
}

func (f *fakeCounter) IncrementWindow(_ context.Context, key string, _ time.Duration) (int64, time.Duration, error) {
	if f.err != nil {
		return 0, 0, f.err
	}
	f.counts[key]++
	return f.counts[key], f.reset, nil
}

func redisLimiterWith(c counter, limit int) *RedisLimiter {
	return &RedisLimiter{counter: c, limit: limit, window: time.Minute, prefix: "ratelimit:", fallback: Memory(limit, time.Minute)}
}

func TestRedisLimiterAllowsUpToLimitThenBlocks(t *testing.T) {
	limiter := redisLimiterWith(newFakeCounter(), 3)
	ctx := context.Background()

	for i := 1; i <= 3; i++ {
		d := limiter.Allow(ctx, "1.2.3.4")
		if !d.Allowed {
			t.Fatalf("request %d blocked, want allowed", i)
		}
		if want := 3 - i; d.Remaining != want {
			t.Fatalf("request %d remaining = %d, want %d", i, d.Remaining, want)
		}
	}

	d := limiter.Allow(ctx, "1.2.3.4")
	if d.Allowed {
		t.Fatal("4th request allowed, want blocked")
	}
	if d.Remaining != 0 {
		t.Fatalf("blocked remaining = %d, want 0", d.Remaining)
	}
	if d.RetryAfter <= 0 {
		t.Fatalf("blocked RetryAfter = %v, want a positive reset hint", d.RetryAfter)
	}
}

// The whole point of a shared store: separate clients are counted separately.
func TestRedisLimiterCountsPerKey(t *testing.T) {
	limiter := redisLimiterWith(newFakeCounter(), 1)
	ctx := context.Background()

	if d := limiter.Allow(ctx, "clientA"); !d.Allowed {
		t.Fatal("clientA first request should be allowed")
	}
	if d := limiter.Allow(ctx, "clientB"); !d.Allowed {
		t.Fatal("clientB first request should be allowed despite clientA using its quota")
	}
	if d := limiter.Allow(ctx, "clientA"); d.Allowed {
		t.Fatal("clientA second request should be blocked")
	}
}

// A Redis outage must not make a configured resource ceiling disappear. The
// local fallback keeps the process available and still bounds each key.
func TestRedisLimiterUsesBoundedLocalFallbackWhenRedisErrors(t *testing.T) {
	fc := newFakeCounter()
	fc.err = errors.New("connection refused")
	limiter := redisLimiterWith(fc, 1)

	if d := limiter.Allow(context.Background(), "1.2.3.4"); !d.Allowed {
		t.Fatal("first request blocked while Redis is down")
	}
	if d := limiter.Allow(context.Background(), "1.2.3.4"); d.Allowed {
		t.Fatal("second request escaped the local fallback ceiling")
	}
}

func TestRedisLimiterFailsClosedWithoutFallback(t *testing.T) {
	fc := newFakeCounter()
	fc.err = errors.New("connection refused")
	limiter := redisLimiterWith(fc, 1)
	limiter.fallback = nil

	if d := limiter.Allow(context.Background(), "1.2.3.4"); d.Allowed {
		t.Fatal("request allowed while Redis is down and fail-closed, want blocked")
	}
}

func TestRedisLimiterDisabledIsPassthrough(t *testing.T) {
	limiter := redisLimiterWith(newFakeCounter(), 0)
	if limiter.Enabled() {
		t.Fatal("limiter with limit 0 should be disabled")
	}
	for i := 0; i < 100; i++ {
		if d := limiter.Allow(context.Background(), "1.2.3.4"); !d.Allowed {
			t.Fatalf("disabled limiter blocked request %d", i)
		}
	}
}
