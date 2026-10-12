package authentication

import (
	"context"
	"errors"
	"testing"

	"automation-hub-idp/internal/app/services"
	"github.com/go-redis/redis/v8"
)

type countingAuthResource struct{ count int }

func (c *countingAuthResource) Close() error { c.count++; return nil }

func TestAuthenticationCleanupClosesOnlyOwnedResources(t *testing.T) {
	closer := &countingAuthResource{}
	owned := &services.OwnedResources{}
	owned.Add(closer)
	a := &service{ownedResources: owned}
	if a.Close() != nil || a.Close() != nil || closer.count != 1 {
		t.Fatal("owned resource cleanup not idempotent")
	}
	if (&service{}).Close() != nil {
		t.Fatal("borrowed service cleanup failed")
	}
}

func TestHandlerCleanupClosesLimiterButNotAuthentication(t *testing.T) {
	clientLimiter := newPasswordResetLimiter("redis:6379", "synthetic-key")
	counter := clientLimiter.counter.(redisPasswordResetCounter)
	if counter.client.PoolStats().TotalConns != 0 {
		t.Fatal("unexpected live connection")
	}
	authCloser := &countingAuthResource{}
	owned := &services.OwnedResources{}
	owned.Add(authCloser)
	a := &service{ownedResources: owned}
	handler := newHandler(a, clientLimiter)
	if handler.Close() != nil || handler.Close() != nil {
		t.Fatal("handler cleanup failed")
	}
	if authCloser.count != 0 {
		t.Fatal("handler closed borrowed authentication service")
	}
	if err := counter.client.Ping(context.Background()).Err(); !errors.Is(err, redis.ErrClosed) {
		t.Fatalf("expected closed Redis client, got %v", err)
	}
	if a.Close() != nil || authCloser.count != 1 {
		t.Fatal("authentication cleanup missing")
	}
}
