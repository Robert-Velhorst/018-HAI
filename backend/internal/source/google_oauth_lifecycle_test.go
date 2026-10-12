package source

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"automation-hub-backend/internal/lifecycle"
)

func TestOAuthSharedRefreshIsJoinedAfterCallerCancellation(t *testing.T) {
	g := lifecycle.New(context.Background())
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer func() {
		once.Do(func() { close(release) })
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := g.StopAndWait(ctx); err != nil {
			t.Error(err)
		}
	}()
	transport := &googleOAuthReadRetryTransport{accessToken: "stale", refresh: func(context.Context, string) (string, error) {
		close(entered)
		<-release
		return "fresh", nil
	}}
	caller, cancel := context.WithCancel(g.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, _, err := transport.refreshOnce(caller, "stale"); done <- err }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("refresh did not start")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("caller error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("caller could not cancel independently")
	}
	ctx, stop := context.WithTimeout(context.Background(), 15*time.Millisecond)
	defer stop()
	if err := g.StopAndWait(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("refresh escaped join: %v", err)
	}
	once.Do(func() { close(release) })
}

func TestOAuthRefreshRefusedAdmissionUnblocksEveryWaiter(t *testing.T) {
	g := lifecycle.New(context.Background())
	g.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := g.StopAndWait(ctx); err != nil {
		t.Fatal(err)
	}
	transport := &googleOAuthReadRetryTransport{accessToken: "stale", refresh: func(context.Context, string) (string, error) {
		t.Error("refresh started after shutdown")
		return "fresh", nil
	}}
	attached := g.WithContext(ctx)
	for i := 0; i < 2; i++ {
		if _, retry, err := transport.refreshOnce(attached, "stale"); retry || !errors.Is(err, context.Canceled) {
			t.Fatalf("refused refresh = %t, %v", retry, err)
		}
	}
	if _, err := transport.accessTokenForRequest(attached); !errors.Is(err, context.Canceled) {
		t.Fatalf("token waiter = %v", err)
	}
}
