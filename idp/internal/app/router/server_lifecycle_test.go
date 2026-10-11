package router

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"
)

type lifecycleServer struct {
	serve    func() error
	shutdown func(context.Context) error
	close    func() error
}

func (s lifecycleServer) ListenAndServe() error              { return s.serve() }
func (s lifecycleServer) Shutdown(ctx context.Context) error { return s.shutdown(ctx) }
func (s lifecycleServer) Close() error                       { return s.close() }

func receiveLifecycle(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(2 * time.Second):
		t.Fatal("lifecycle did not finish")
		return nil
	}
}

func TestIdentityShutdownWaitsForDrainBeforeCleanup(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{})
	serveStop := make(chan struct{})
	draining := make(chan struct{})
	release := make(chan struct{})
	closed := make(chan struct{})
	server := lifecycleServer{
		serve: func() error { close(started); <-serveStop; return http.ErrServerClosed },
		shutdown: func(drainCtx context.Context) error {
			if drainCtx.Err() != nil {
				t.Error("shutdown inherited cancelled request context")
			}
			close(serveStop)
			close(draining)
			select {
			case <-release:
				return nil
			case <-drainCtx.Done():
				return drainCtx.Err()
			}
		},
		close: func() error { t.Error("forced close after successful shutdown"); return nil },
	}
	done := make(chan error, 1)
	go func() {
		done <- runWithOwnedDatabase(func() error { close(closed); return nil }, func() error { return serveIdentity(ctx, server, time.Second) })
	}()
	<-started
	cancel()
	<-draining
	select {
	case <-closed:
		t.Error("storage closed while handlers draining")
	default:
	}
	select {
	case <-done:
		t.Fatal("serve returned before drain completed")
	default:
	}
	close(release)
	if err := receiveLifecycle(t, done); err != nil {
		t.Fatal(err)
	}
	select {
	case <-closed:
	default:
		t.Fatal("storage cleanup missing")
	}
}

func TestIdentityShutdownTimeoutPreservesResourcesAndErrors(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{})
	serveStop := make(chan struct{})
	closeFailure := errors.New("socket close failed")
	closed := 0
	server := lifecycleServer{
		serve:    func() error { close(started); <-serveStop; return http.ErrServerClosed },
		shutdown: func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() },
		close:    func() error { close(serveStop); return closeFailure },
	}
	done := make(chan error, 1)
	go func() {
		done <- runWithOwnedDatabase(func() error { closed++; return nil }, func() error { return serveIdentity(ctx, server, 20*time.Millisecond) })
	}()
	<-started
	cancel()
	err := receiveLifecycle(t, done)
	if !errors.Is(err, errIdentityDrainIncomplete) || !errors.Is(err, context.DeadlineExceeded) || !errors.Is(err, closeFailure) {
		t.Fatalf("errors lost: %v", err)
	}
	if closed != 0 {
		t.Fatal("storage closed without verified handler drain")
	}
}

func TestIdentityServeFailureStillDrainsAndPreservesError(t *testing.T) {
	listenFailure := errors.New("listen failed")
	drained := false
	server := lifecycleServer{
		serve:    func() error { return listenFailure },
		shutdown: func(context.Context) error { drained = true; return nil },
		close:    func() error { t.Error("unexpected forced close"); return nil },
	}
	if err := serveIdentity(context.Background(), server, time.Second); !errors.Is(err, listenFailure) || !drained {
		t.Fatalf("err=%v drained=%v", err, drained)
	}
}

func TestIdentityServeRejectsInvalidOrCancelledStartup(t *testing.T) {
	server := lifecycleServer{serve: func() error { t.Fatal("invalid startup began serving"); return nil }}
	if serveIdentity(nil, server, time.Second) == nil || serveIdentity(context.Background(), nil, time.Second) == nil || serveIdentity(context.Background(), server, 0) == nil {
		t.Fatal("invalid input accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := serveIdentity(ctx, server, time.Second); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
}
