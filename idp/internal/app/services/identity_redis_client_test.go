package services

import (
	"context"
	"testing"
	"time"

	"automation-hub-idp/internal/app/config"
)

func TestIdentityRedisPoolHasFixedResourceBounds(t *testing.T) {
	client := NewIdentityRedisClient("  redis:6379  ")
	if client == nil {
		t.Fatal("configured client missing")
	}
	defer client.Close()
	options := client.Options()
	if options.Addr != "redis:6379" || options.PoolSize != 4 || options.MinIdleConns != 0 {
		t.Fatalf("unexpected address or capacity: addr=%q size=%d idle=%d", options.Addr, options.PoolSize, options.MinIdleConns)
	}
	if options.PoolTimeout != 750*time.Millisecond || options.DialTimeout != 750*time.Millisecond ||
		options.ReadTimeout != 750*time.Millisecond || options.WriteTimeout != 750*time.Millisecond {
		t.Fatal("Redis operation or pool acquisition deadline changed")
	}
	// go-redis normalizes -1 into zero retry attempts.
	if options.MaxRetries != 0 || options.IdleTimeout != 5*time.Minute || options.MaxConnAge != 30*time.Minute {
		t.Fatal("unexpected retry/lifetime configuration")
	}
	if stats := client.PoolStats(); stats.TotalConns != 0 || stats.IdleConns != 0 {
		t.Fatalf("constructor opened connections: %+v", stats)
	}
}

func TestIdentityRedisMissingAddressDoesNotDefaultToLocalhost(t *testing.T) {
	for _, address := range []string{"", "  "} {
		if client := NewIdentityRedisClient(address); client != nil {
			client.Close()
			t.Error("empty address created a Redis client")
		}
	}
}

func TestDefaultRedisBlockListMissingConfigFailsClosed(t *testing.T) {
	previous := config.RedisConfig
	config.RedisConfig = nil
	t.Cleanup(func() { config.RedisConfig = previous })
	store := NewRedisTokenBlockListService().(*tokenBlockListServiceImpl)
	if err := store.Ping(context.Background()); err == nil {
		t.Error("missing Redis accepted readiness")
	}
	if err := store.available(); err == nil {
		t.Error("missing Redis accepted session operations")
	}
	if err := store.Close(); err != nil {
		t.Fatalf("missing client cleanup: %v", err)
	}
}
