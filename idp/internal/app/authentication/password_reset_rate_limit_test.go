package authentication

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type passwordResetCounterFunc func(context.Context, string, string) (bool, time.Duration, error)

func (f passwordResetCounterFunc) Allow(ctx context.Context, ipKey, accountKey string) (bool, time.Duration, error) {
	return f(ctx, ipKey, accountKey)
}

func TestPasswordResetLimiterMissingRedisAddressFailsClosed(t *testing.T) {
	limiter := newPasswordResetLimiter("  ", "synthetic-key")
	counter, ok := limiter.counter.(redisPasswordResetCounter)
	if !ok || counter.client != nil {
		t.Fatal("missing address must not create an implicit localhost client")
	}
	decision := limiter.allow(context.Background(), "192.0.2.1", "test@example.invalid")
	if decision.Allowed || decision.RetryAfter != passwordResetRateWindow {
		t.Fatalf("missing Redis must deny reset: %+v", decision)
	}
	if len(limiter.fallback.ipBuckets) != 0 || len(limiter.fallback.userBuckets) != 0 {
		t.Fatal("missing production Redis fell back to process-local rate limits")
	}
}

func TestPasswordResetLimiterDeniesWhenRedisIsUnavailable(t *testing.T) {
	fallback := newMemoryPasswordResetCounter()
	limiter := &passwordResetLimiter{
		counter: passwordResetCounterFunc(func(context.Context, string, string) (bool, time.Duration, error) {
			return false, 0, errors.New("redis unavailable")
		}),
		fallback: fallback,
	}

	decision := limiter.allow(context.Background(), "192.0.2.10", "person@example.com")

	require.False(t, decision.Allowed, "a Redis outage must not bypass shared abuse limits")
	require.Equal(t, passwordResetRateWindow, decision.RetryAfter)
	require.Zero(t, fallback.requests, "the process-local fallback must not be used for Redis-backed requests")
}

func TestPasswordResetLimiterAllowsRequestsAfterRedisRecovers(t *testing.T) {
	fallback := newMemoryPasswordResetCounter()
	redisCalls := 0
	limiter := &passwordResetLimiter{
		counter: passwordResetCounterFunc(func(context.Context, string, string) (bool, time.Duration, error) {
			redisCalls++
			if redisCalls == 1 {
				return false, 0, errors.New("redis unavailable")
			}
			return true, time.Minute, nil
		}),
		fallback: fallback,
	}

	failed := limiter.allow(context.Background(), "192.0.2.10", "person@example.com")
	recovered := limiter.allow(context.Background(), "192.0.2.10", "person@example.com")

	require.False(t, failed.Allowed)
	require.True(t, recovered.Allowed, "Redis should resume governing requests as soon as it is available")
	require.Equal(t, 2, redisCalls)
	require.Zero(t, fallback.requests)
}

func TestPasswordResetLimiterBucketsByIPAndNormalizedAccount(t *testing.T) {
	counter := newMemoryPasswordResetCounter()
	now := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)
	ipKey := passwordResetBucketKey("ip", normalizeResetIP("203.0.113.10"), "")
	accountKey := passwordResetBucketKey("account", normalizeResetEmail(" Owner@Example.com "), "")

	for i := 0; i < passwordResetAccountLimit; i++ {
		decision := counter.allow(ipKey, accountKey, now)
		require.True(t, decision.Allowed, "request %d should be within account limit", i+1)
	}
	blocked := counter.allow(
		passwordResetBucketKey("ip", normalizeResetIP("203.0.113.11"), ""),
		passwordResetBucketKey("account", normalizeResetEmail("owner@example.com"), ""),
		now,
	)
	require.False(t, blocked.Allowed, "email normalization and account bucketing must hold across IPs")
	require.Greater(t, blocked.RetryAfter, time.Duration(0))

	otherAccount := counter.allow(
		ipKey,
		passwordResetBucketKey("account", normalizeResetEmail("other@example.com"), ""),
		now,
	)
	require.True(t, otherAccount.Allowed, "a different account has its own account bucket")
	ipCounter := newMemoryPasswordResetCounter()
	ipOnlyKey := passwordResetBucketKey("ip", normalizeResetIP("198.51.100.10"), "")
	for i := 0; i < passwordResetIPLimit; i++ {
		decision := ipCounter.allow(
			ipOnlyKey,
			passwordResetBucketKey("account", normalizeResetEmail(fmt.Sprintf("different-%d@example.com", i)), ""),
			now,
		)
		require.True(t, decision.Allowed, "IP request %d should remain within its limit", i+1)
	}
	blockedByIP := ipCounter.allow(
		ipOnlyKey,
		passwordResetBucketKey("account", normalizeResetEmail("last@example.com"), ""),
		now,
	)
	require.False(t, blockedByIP.Allowed, "one IP must have a separate aggregate limit across accounts")
}

func TestPasswordResetLimiterWindowsExpireAndEmailIsNormalized(t *testing.T) {
	counter := newMemoryPasswordResetCounter()
	now := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)
	decision := counter.allow(
		passwordResetBucketKey("ip", normalizeResetIP("2001:0db8::1"), ""),
		passwordResetBucketKey("account", normalizeResetEmail("User@Example.com"), ""),
		now,
	)
	require.True(t, decision.Allowed)
	require.Equal(t, "2001:db8::1", normalizeResetIP(" 2001:0db8:0:0:0:0:0:1 "))
	require.Equal(t, "user@example.com", normalizeResetEmail(" User@Example.com "))

	reset := counter.allow(
		passwordResetBucketKey("ip", normalizeResetIP("2001:db8::1"), ""),
		passwordResetBucketKey("account", normalizeResetEmail("user@example.com"), ""),
		now.Add(passwordResetRateWindow),
	)
	require.True(t, reset.Allowed, "both buckets should reset at the end of their window")
}

func TestPasswordResetRedisKeysAreHMACedAndScopedByPurpose(t *testing.T) {
	account := "owner@example.com"
	key := passwordResetBucketKey("account", account, "stable-idp-secret")
	require.NotContains(t, key, account)
	require.NotEqual(t, key, passwordResetBucketKey("account", account, "another-idp-secret"))
	require.NotEqual(t, key, passwordResetBucketKey("ip", account, "stable-idp-secret"))
}

func TestPasswordResetLimiterIsConcurrentAndBoundedPerAccount(t *testing.T) {
	counter := newMemoryPasswordResetCounter()
	ipKey := passwordResetBucketKey("ip", "192.0.2.10", "")
	accountKey := passwordResetBucketKey("account", "user@example.com", "")
	now := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)
	var mu sync.Mutex
	allowed := 0
	var workers sync.WaitGroup
	for i := 0; i < 24; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			if counter.allow(ipKey, accountKey, now).Allowed {
				mu.Lock()
				allowed++
				mu.Unlock()
			}
		}()
	}
	workers.Wait()
	require.Equal(t, passwordResetAccountLimit, allowed)
}
