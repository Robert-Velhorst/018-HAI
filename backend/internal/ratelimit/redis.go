package ratelimit

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/redis/go-redis/v9"
)

// counter is the minimal Redis surface the limiter needs. Isolating it behind an
// interface lets the decision logic be unit-tested with a deterministic fake,
// while the real implementation talks to Redis.
type counter interface {
	// IncrementWindow counts one hit for key in the fixed window of size window
	// and returns the post-increment count and the time until that window
	// resets.
	IncrementWindow(ctx context.Context, key string, window time.Duration) (count int64, reset time.Duration, err error)
}

// RedisLimiter is a fixed-window limiter whose counters live in Redis, so the
// limit is enforced consistently across restarts and across multiple backend
// instances sharing one Redis — which the in-process limiter cannot do.
type RedisLimiter struct {
	counter counter
	limit   int
	window  time.Duration
	prefix  string
	// fallback preserves a local ceiling when Redis fails after startup. It
	// keeps the API available without silently turning a configured limiter
	// into an unlimited pass-through.
	fallback Enforcer
}

// NewRedisLimiter builds a Redis-backed Enforcer from an address (host:port).
// It returns an error only when the address cannot be reached at all, so the
// caller can fall back to the in-process limiter at startup.
func NewRedisLimiter(ctx context.Context, addr string, limit int, window time.Duration) (*RedisLimiter, error) {
	client := redis.NewClient(&redis.Options{
		Addr:         addr,
		DialTimeout:  2 * time.Second,
		ReadTimeout:  2 * time.Second,
		WriteTimeout: 2 * time.Second,
	})
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("redis rate-limit store unreachable at %s: %w", addr, err)
	}
	return &RedisLimiter{
		counter:  redisCounter{client: client},
		limit:    limit,
		window:   window,
		prefix:   "ratelimit:",
		fallback: Memory(limit, window),
	}, nil
}

func (r *RedisLimiter) Enabled() bool { return r.limit > 0 && r.window > 0 }

func (r *RedisLimiter) Allow(ctx context.Context, key string) Decision {
	if !r.Enabled() {
		return Decision{Allowed: true, Remaining: -1}
	}

	count, reset, err := r.counter.IncrementWindow(ctx, r.prefix+key, r.window)
	if err != nil {
		if r.fallback != nil && r.fallback.Enabled() {
			log.Printf("ratelimit: redis unavailable, using in-process fallback: %v", err)
			return r.fallback.Allow(ctx, key)
		}
		return Decision{Allowed: false, Remaining: 0, RetryAfter: r.window}
	}

	remaining := r.limit - int(count)
	if remaining < 0 {
		remaining = 0
	}
	if int(count) > r.limit {
		return Decision{Allowed: false, Remaining: 0, RetryAfter: reset}
	}
	return Decision{Allowed: true, Remaining: remaining, RetryAfter: reset}
}

// redisCounter is the real Redis implementation of counter.
type redisCounter struct {
	client *redis.Client
}

// IncrementWindow uses a time-bucketed key so each fixed window is a distinct
// Redis key that expires on its own. Counting a hit is a single INCR; the
// EXPIRE (idempotent, refreshed each hit) guarantees the key is reclaimed a
// window after its last use. This is correct under continuous load — unlike
// refreshing the TTL of one shared key, which would never let the window reset.
func (c redisCounter) IncrementWindow(ctx context.Context, key string, window time.Duration) (int64, time.Duration, error) {
	nowMs := time.Now().UnixMilli()
	windowMs := window.Milliseconds()
	if windowMs <= 0 {
		return 0, 0, fmt.Errorf("non-positive window")
	}
	bucket := nowMs / windowMs
	bucketKey := fmt.Sprintf("%s:%d", key, bucket)

	pipe := c.client.Pipeline()
	incr := pipe.Incr(ctx, bucketKey)
	// A one-second grace over the window so the key outlives the last hit it
	// counts, without lingering meaningfully longer.
	pipe.Expire(ctx, bucketKey, window+time.Second)
	if _, err := pipe.Exec(ctx); err != nil {
		return 0, 0, err
	}

	resetMs := (bucket+1)*windowMs - nowMs
	return incr.Val(), time.Duration(resetMs) * time.Millisecond, nil
}
