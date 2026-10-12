package services

import (
	"strings"
	"time"

	"github.com/go-redis/redis/v8"
)

// NewIdentityRedisClient creates a caller-owned, lazy connection pool.
func NewIdentityRedisClient(address string) *redis.Client {
	address = strings.TrimSpace(address)
	if address == "" {
		return nil
	}
	return redis.NewClient(&redis.Options{
		Addr:         address,
		PoolSize:     4,
		MinIdleConns: 0,
		PoolTimeout:  750 * time.Millisecond,
		DialTimeout:  750 * time.Millisecond,
		ReadTimeout:  750 * time.Millisecond,
		WriteTimeout: 750 * time.Millisecond,
		IdleTimeout:  5 * time.Minute,
		MaxConnAge:   30 * time.Minute,
		// A lost response must not silently repeat a security-store mutation.
		MaxRetries: -1,
	})
}
