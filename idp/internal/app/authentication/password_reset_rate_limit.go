package authentication

import (
	"automation-hub-idp/internal/app/services"
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

	"github.com/go-redis/redis/v8"
)

const (
	passwordResetRateWindow       = 15 * time.Minute
	passwordResetIPLimit          = 20
	passwordResetAccountLimit     = 3
	passwordResetMaxMemoryBuckets = 8192
)

type passwordResetRateDecision struct {
	Allowed    bool
	RetryAfter time.Duration
}

type passwordResetCounter interface {
	Allow(context.Context, string, string) (bool, time.Duration, error)
}

type passwordResetLimiter struct {
	counter        passwordResetCounter
	fallback       *memoryPasswordResetCounter
	keySecret      string
	logOnce        sync.Once
	ownedResources *services.OwnedResources
}

func (l *passwordResetLimiter) Close() error {
	if l == nil {
		return nil
	}
	return l.ownedResources.Close()
}

func newPasswordResetLimiter(redisAddress, keySecret string) *passwordResetLimiter {
	client := services.NewIdentityRedisClient(redisAddress)
	owned := &services.OwnedResources{}
	if client != nil {
		owned.Add(client)
	}
	return &passwordResetLimiter{
		counter:        redisPasswordResetCounter{client: client},
		fallback:       newMemoryPasswordResetCounter(),
		keySecret:      keySecret,
		ownedResources: owned,
	}
}

func newMemoryOnlyPasswordResetLimiter() *passwordResetLimiter {
	return &passwordResetLimiter{fallback: newMemoryPasswordResetCounter()}
}

func (l *passwordResetLimiter) allow(ctx context.Context, remoteIP, email string) passwordResetRateDecision {
	ipKey := passwordResetBucketKey("ip", normalizeResetIP(remoteIP), l.keySecret)
	accountKey := passwordResetBucketKey("account", normalizeResetEmail(email), l.keySecret)
	if l.counter != nil {
		ctx, cancel := context.WithTimeout(ctx, 900*time.Millisecond)
		allowed, retryAfter, err := l.counter.Allow(ctx, ipKey, accountKey)
		cancel()
		if err == nil {
			return passwordResetRateDecision{Allowed: allowed, RetryAfter: retryAfter}
		}
		l.logOnce.Do(func() {
			log.Printf("password-reset rate-limit store unavailable; denying request")
		})
		return passwordResetRateDecision{RetryAfter: passwordResetRateWindow}
	}
	return l.fallback.allow(ipKey, accountKey, time.Now())
}

func passwordResetBucketKey(kind, value, secret string) string {
	message := kind + "\x00" + value
	var digest []byte
	if secret == "" {
		plainDigest := sha256.Sum256([]byte(message))
		digest = plainDigest[:]
	} else {
		mac := hmac.New(sha256.New, []byte(secret))
		_, _ = mac.Write([]byte(message))
		digest = mac.Sum(nil)
	}
	return "hai:idp:password-reset:v1:" + kind + ":" + hex.EncodeToString(digest)
}

func normalizeResetIP(value string) string {
	value = strings.TrimSpace(value)
	if ip := net.ParseIP(value); ip != nil {
		return ip.String()
	}
	if value == "" {
		return "unknown"
	}
	return strings.ToLower(value)
}

func normalizeResetEmail(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

type resetWindowBucket struct {
	count   int
	resetAt time.Time
}

type memoryPasswordResetCounter struct {
	mu          sync.Mutex
	ipBuckets   map[string]resetWindowBucket
	userBuckets map[string]resetWindowBucket
	requests    uint64
}

func newMemoryPasswordResetCounter() *memoryPasswordResetCounter {
	return &memoryPasswordResetCounter{
		ipBuckets:   make(map[string]resetWindowBucket),
		userBuckets: make(map[string]resetWindowBucket),
	}
}

func (m *memoryPasswordResetCounter) allow(ipKey, accountKey string, now time.Time) passwordResetRateDecision {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.requests++
	if m.requests%256 == 0 {
		m.prune(now)
	}

	ipBucket, ipExists := m.ipBuckets[ipKey]
	if !ipExists || !now.Before(ipBucket.resetAt) {
		if !ipExists && len(m.ipBuckets) >= passwordResetMaxMemoryBuckets {
			return passwordResetRateDecision{RetryAfter: passwordResetRateWindow}
		}
		ipBucket = resetWindowBucket{count: 0, resetAt: now.Add(passwordResetRateWindow)}
	}
	if ipBucket.count >= passwordResetIPLimit {
		return passwordResetRateDecision{RetryAfter: ipBucket.resetAt.Sub(now)}
	}
	ipBucket.count++
	m.ipBuckets[ipKey] = ipBucket

	accountBucket, accountExists := m.userBuckets[accountKey]
	if !accountExists || !now.Before(accountBucket.resetAt) {
		if !accountExists && len(m.userBuckets) >= passwordResetMaxMemoryBuckets {
			return passwordResetRateDecision{RetryAfter: passwordResetRateWindow}
		}
		accountBucket = resetWindowBucket{count: 0, resetAt: now.Add(passwordResetRateWindow)}
	}
	if accountBucket.count >= passwordResetAccountLimit {
		return passwordResetRateDecision{RetryAfter: accountBucket.resetAt.Sub(now)}
	}
	accountBucket.count++
	m.userBuckets[accountKey] = accountBucket
	return passwordResetRateDecision{Allowed: true, RetryAfter: passwordResetRateWindow}
}

func (m *memoryPasswordResetCounter) prune(now time.Time) {
	for key, bucket := range m.ipBuckets {
		if !now.Before(bucket.resetAt) {
			delete(m.ipBuckets, key)
		}
	}
	for key, bucket := range m.userBuckets {
		if !now.Before(bucket.resetAt) {
			delete(m.userBuckets, key)
		}
	}
}

type redisPasswordResetCounter struct {
	client *redis.Client
}

var passwordResetRateScript = redis.NewScript(`
local ipCount = redis.call('INCR', KEYS[1])
if ipCount == 1 then redis.call('PEXPIRE', KEYS[1], ARGV[3]) end
local ipTTL = redis.call('PTTL', KEYS[1])
if ipCount > tonumber(ARGV[1]) then return {0, ipTTL} end

local accountCount = redis.call('INCR', KEYS[2])
if accountCount == 1 then redis.call('PEXPIRE', KEYS[2], ARGV[3]) end
local accountTTL = redis.call('PTTL', KEYS[2])
if accountCount > tonumber(ARGV[2]) then return {0, accountTTL} end

if ipTTL < accountTTL then return {1, ipTTL} end
return {1, accountTTL}
`)

func (r redisPasswordResetCounter) Allow(ctx context.Context, ipKey, accountKey string) (bool, time.Duration, error) {
	if r.client == nil {
		return false, 0, errors.New("password-reset rate-limit Redis client is unavailable")
	}
	result, err := passwordResetRateScript.Run(ctx, r.client, []string{ipKey, accountKey},
		passwordResetIPLimit,
		passwordResetAccountLimit,
		passwordResetRateWindow.Milliseconds(),
	).Int64Slice()
	if err != nil {
		return false, 0, err
	}
	if len(result) != 2 {
		return false, 0, errors.New("unexpected password-reset rate-limit response")
	}
	retryAfter := time.Duration(result[1]) * time.Millisecond
	if retryAfter < time.Second {
		retryAfter = time.Second
	}
	return result[0] == 1, retryAfter, nil
}
