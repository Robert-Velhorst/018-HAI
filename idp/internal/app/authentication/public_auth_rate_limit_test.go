package authentication

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type publicAuthCounterFunc func(context.Context, string, int, time.Duration) (bool, time.Duration, error)

func (f publicAuthCounterFunc) Allow(ctx context.Context, key string, limit int, window time.Duration) (bool, time.Duration, error) {
	return f(ctx, key, limit, window)
}

func TestPublicAuthLimiterAppliesIndependentPerIPActionLimits(t *testing.T) {
	limiter := newMemoryPublicAuthRateLimiter()
	for i := 0; i < publicRegisterIPLimit; i++ {
		require.True(t, limiter.allow(context.Background(), "register", "192.0.2.4:9000").Allowed)
	}
	blockedRegistration := limiter.allow(context.Background(), "register", "192.0.2.4:9000")
	require.False(t, blockedRegistration.Allowed)
	require.Greater(t, blockedRegistration.RetryAfter, time.Duration(0))
	require.True(t, limiter.allow(context.Background(), "register", "192.0.2.5:9000").Allowed, "a separate client IP has its own bucket")
	require.True(t, limiter.allow(context.Background(), "login", "192.0.2.4:9000").Allowed, "registration and login use separate limits")

	loginLimiter := newMemoryPublicAuthRateLimiter()
	for i := 0; i < publicLoginIPLimit; i++ {
		require.True(t, loginLimiter.allow(context.Background(), "login", "[2001:db8::1]:443").Allowed)
	}
	require.False(t, loginLimiter.allow(context.Background(), "login", "[2001:db8::1]:443").Allowed)

	resetLimiter := newMemoryPublicAuthRateLimiter()
	for i := 0; i < publicResetConfirmIPLimit; i++ {
		require.True(t, resetLimiter.allow(context.Background(), "password_reset_confirm", "192.0.2.4:9000").Allowed)
	}
	require.False(t, resetLimiter.allow(context.Background(), "password_reset_confirm", "192.0.2.4:9000").Allowed)
	require.True(t, resetLimiter.allow(context.Background(), "login", "192.0.2.4:9000").Allowed, "reset confirmation and login use separate limits")
}

func TestPublicAuthLimiterFailsClosedForUntrustedIdentityAndUnavailableRedis(t *testing.T) {
	limiter := newMemoryPublicAuthRateLimiter()
	for _, remoteAddr := range []string{"", "unknown:443", "203.0.113.4", "0.0.0.0:0", "224.0.0.1:443"} {
		decision := limiter.allow(context.Background(), "login", remoteAddr)
		require.False(t, decision.Allowed, "remote address %q must not be accepted", remoteAddr)
	}
	require.Empty(t, limiter.fallback.buckets, "untrusted identities must not consume or share an anonymous bucket")

	productionLimiter := newPublicAuthRateLimiter("", "not-a-real-secret")
	decision := productionLimiter.allow(context.Background(), "register", "192.0.2.4:9000")
	require.False(t, decision.Allowed, "production mode must fail closed when Redis is not configured")
	require.Equal(t, publicAuthRateWindow, decision.RetryAfter)
	require.Empty(t, productionLimiter.fallback.buckets, "production must not silently fall back to process-local counters")
}

func TestPublicAuthLimiterDeniesRedisErrorsWithoutFallback(t *testing.T) {
	fallback := newMemoryPublicAuthCounter()
	limiter := &publicAuthRateLimiter{
		counter: publicAuthCounterFunc(func(context.Context, string, int, time.Duration) (bool, time.Duration, error) {
			return false, 0, errors.New("redis unavailable")
		}),
		fallback: fallback,
	}
	decision := limiter.allow(context.Background(), "login", "192.0.2.4:9000")
	require.False(t, decision.Allowed)
	require.Equal(t, publicAuthRateWindow, decision.RetryAfter)
	require.Empty(t, fallback.buckets)
}

func TestPublicAuthLimiterIsAtomicUnderConcurrency(t *testing.T) {
	counter := newMemoryPublicAuthCounter()
	key := publicAuthBucketKey("login", "192.0.2.9", "test-key")
	now := time.Now()
	var mu sync.Mutex
	allowed := 0
	var workers sync.WaitGroup
	for i := 0; i < publicLoginIPLimit+12; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			if counter.allow(key, publicLoginIPLimit, now).Allowed {
				mu.Lock()
				allowed++
				mu.Unlock()
			}
		}()
	}
	workers.Wait()
	require.Equal(t, publicLoginIPLimit, allowed)
	require.Contains(t, key, "hai:idp:public-auth:v1:")
	require.NotContains(t, key, "192.0.2.9")
	require.EqualValues(t, publicLoginIPLimit+12, counter.requests)
}
