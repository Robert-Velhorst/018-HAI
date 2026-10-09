package authentication

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log"
	"net"
	"strings"
	"sync"
	"time"

	"automation-hub-idp/internal/app/services"
	"github.com/go-redis/redis/v8"
)

const (
	publicAuthRateWindow       = 15 * time.Minute
	publicRegisterIPLimit      = 10
	publicLoginIPLimit         = 30
	publicResetConfirmIPLimit  = 20
	publicAuthMaxMemoryBuckets = 8192
)

type publicAuthRateDecision struct {
	Allowed    bool
	RetryAfter time.Duration
}

type publicAuthCounter interface {
	Allow(context.Context, string, int, time.Duration) (bool, time.Duration, error)
}

type publicAuthRateLimiter struct {
	counter  publicAuthCounter
	fallback *memoryPublicAuthCounter
	secret   string
	logOnce  sync.Once
	owned    *services.OwnedResources
}

func (l *publicAuthRateLimiter) Close() error {
	if l == nil {
		return nil
	}
	return l.owned.Close()
}

func newPublicAuthRateLimiter(redisAddress, secret string) *publicAuthRateLimiter {
	client := services.NewIdentityRedisClient(redisAddress)
	owned := &services.OwnedResources{}
	owned.Add(client)
	return &publicAuthRateLimiter{
		counter:  redisPublicAuthCounter{client: client},
		fallback: newMemoryPublicAuthCounter(),
		secret:   secret,
		owned:    owned,
	}
}

func newMemoryPublicAuthRateLimiter() *publicAuthRateLimiter {
	return &publicAuthRateLimiter{fallback: newMemoryPublicAuthCounter(), owned: &services.OwnedResources{}}
}

func (l *publicAuthRateLimiter) allow(ctx context.Context, action, remoteAddr string) publicAuthRateDecision {
	ip, ok := ipFromRemoteAddr(remoteAddr)
	if !ok {
		return publicAuthRateDecision{RetryAfter: publicAuthRateWindow}
	}
	limit := 0
	switch action {
	case "register":
		limit = publicRegisterIPLimit
	case "login":
		limit = publicLoginIPLimit
	case "password_reset_confirm":
		limit = publicResetConfirmIPLimit
	default:
		return publicAuthRateDecision{RetryAfter: publicAuthRateWindow}
	}
	key := publicAuthBucketKey(action, ip, l.secret)
	if l.counter == nil {
		return l.fallback.allow(key, limit, time.Now())
	}
	requestCtx, cancel := context.WithTimeout(ctx, 900*time.Millisecond)
	allowed, retryAfter, err := l.counter.Allow(requestCtx, key, limit, publicAuthRateWindow)
	cancel()
	if err != nil {
		l.logOnce.Do(func() { log.Print("public authentication rate-limit store unavailable; denying request") })
		return publicAuthRateDecision{RetryAfter: publicAuthRateWindow}
	}
	return publicAuthRateDecision{Allowed: allowed, RetryAfter: retryAfter}
}

func ipFromRemoteAddr(remoteAddr string) (string, bool) {
	host, _, err := net.SplitHostPort(strings.TrimSpace(remoteAddr))
	if err != nil {
		return "", false
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	if ip == nil || ip.IsUnspecified() || ip.IsMulticast() {
		return "", false
	}
	if ipv4 := ip.To4(); ipv4 != nil {
		return ipv4.String(), true
	}
	if ipv6 := ip.To16(); ipv6 != nil {
		return ipv6.String(), true
	}
	return "", false
}

func publicAuthBucketKey(action, ip, secret string) string {
	message := action + "\x00" + ip
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(message))
	return "hai:idp:public-auth:v1:" + hex.EncodeToString(mac.Sum(nil))
}

type publicAuthBucket struct {
	count   int
	resetAt time.Time
}

type memoryPublicAuthCounter struct {
	mu       sync.Mutex
	buckets  map[string]publicAuthBucket
	requests uint64
}

func newMemoryPublicAuthCounter() *memoryPublicAuthCounter {
	return &memoryPublicAuthCounter{buckets: make(map[string]publicAuthBucket)}
}

func (m *memoryPublicAuthCounter) allow(key string, limit int, now time.Time) publicAuthRateDecision {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.requests++
	if m.requests%256 == 0 {
		for bucketKey, bucket := range m.buckets {
			if !now.Before(bucket.resetAt) {
				delete(m.buckets, bucketKey)
			}
		}
	}
	bucket, exists := m.buckets[key]
	if !exists || !now.Before(bucket.resetAt) {
		if !exists && len(m.buckets) >= publicAuthMaxMemoryBuckets {
			return publicAuthRateDecision{RetryAfter: publicAuthRateWindow}
		}
		bucket = publicAuthBucket{resetAt: now.Add(publicAuthRateWindow)}
	}
	if bucket.count >= limit {
		return publicAuthRateDecision{RetryAfter: bucket.resetAt.Sub(now)}
	}
	bucket.count++
	m.buckets[key] = bucket
	return publicAuthRateDecision{Allowed: true, RetryAfter: bucket.resetAt.Sub(now)}
}

type redisPublicAuthCounter struct{ client *redis.Client }

var publicAuthRateScript = redis.NewScript(`
local count = redis.call('INCR', KEYS[1])
if count == 1 then redis.call('PEXPIRE', KEYS[1], ARGV[2]) end
local ttl = redis.call('PTTL', KEYS[1])
if count > tonumber(ARGV[1]) then return {0, ttl} end
return {1, ttl}
`)

func (r redisPublicAuthCounter) Allow(ctx context.Context, key string, limit int, window time.Duration) (bool, time.Duration, error) {
	if r.client == nil {
		return false, 0, errors.New("public auth rate-limit store is unavailable")
	}
	result, err := publicAuthRateScript.Run(ctx, r.client, []string{key}, limit, window.Milliseconds()).Int64Slice()
	if err != nil {
		return false, 0, err
	}
	if len(result) != 2 || result[1] < 0 {
		return false, 0, errors.New("unexpected public auth rate-limit response")
	}
	retryAfter := time.Duration(result[1]) * time.Millisecond
	if retryAfter < time.Second {
		retryAfter = time.Second
	}
	return result[0] == 1, retryAfter, nil
}
