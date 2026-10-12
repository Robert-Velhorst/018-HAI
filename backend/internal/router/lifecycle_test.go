package router

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"
)

type blockingServer struct {
	started  chan struct{}
	stopped  chan struct{}
	shutdown bool
}

func (s *blockingServer) ListenAndServe() error {
	close(s.started)
	<-s.stopped
	return http.ErrServerClosed
}

func (s *blockingServer) Shutdown(context.Context) error {
	s.shutdown = true
	close(s.stopped)
	return nil
}

type failingServer struct{ err error }

func (s failingServer) ListenAndServe() error             { return s.err }
func (s failingServer) Shutdown(context.Context) error    { return nil }

func TestServeUntilContextDoneShutsDownServer(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	server := &blockingServer{started: make(chan struct{}), stopped: make(chan struct{})}

	done := make(chan error, 1)
	go func() { done <- serveUntilContextDone(ctx, server) }()
	select {
	case <-server.started:
	case <-time.After(time.Second):
		t.Fatal("server did not start")
	}
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serveUntilContextDone: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("server did not stop after lifecycle context cancellation")
	}
	if !server.shutdown {
		t.Fatal("server shutdown was not requested")
	}
}

func TestServeUntilContextDoneReturnsListenFailure(t *testing.T) {
	want := errors.New("listen failed")
	if err := serveUntilContextDone(context.Background(), failingServer{err: want}); !errors.Is(err, want) {
		t.Fatalf("serveUntilContextDone error = %v, want %v", err, want)
	}
}
