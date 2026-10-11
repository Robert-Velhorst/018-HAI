package router

import (
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

func TestServeUntilCancelledStopsHTTPServer(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "ok")
	})
	server := &http.Server{Handler: mux, ReadHeaderTimeout: time.Second}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- serveUntilCancelled(ctx, server, listener)
	}()

	client := &http.Client{Timeout: time.Second}
	response, err := client.Get("http://" + listener.Addr().String() + "/healthz")
	if err != nil {
		cancel()
		t.Fatalf("request running server: %v", err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK {
		cancel()
		t.Fatalf("status = %d, want %d", response.StatusCode, http.StatusOK)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serveUntilCancelled: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server did not stop after application context cancellation")
	}

	if _, err := client.Get("http://" + listener.Addr().String() + "/healthz"); err == nil {
		t.Fatal("server still accepted requests after shutdown")
	}
}

func TestRoutesDoNotDetachBackgroundWorkers(t *testing.T) {
	source, err := os.ReadFile("routes.go")
	if err != nil {
		t.Fatalf("read routes.go: %v", err)
	}
	if strings.Contains(string(source), "context.Background()") {
		t.Fatal("routes.go must pass the application context to background workers")
	}
}

func TestServeUntilCancelledRejectsIncompleteLifecycle(t *testing.T) {
	if err := serveUntilCancelled(nil, &http.Server{}, nil); err == nil {
		t.Fatal("serveUntilCancelled accepted a nil lifecycle context/listener")
	}
}

func TestProductionHTTPServerHasBoundedConnectionResources(t *testing.T) {
	server := newHTTPServer(":8080", http.NewServeMux())
	if server.ReadHeaderTimeout <= 0 || server.ReadTimeout <= 0 || server.IdleTimeout <= 0 {
		t.Fatalf("server timeouts must be positive: header=%s read=%s idle=%s", server.ReadHeaderTimeout, server.ReadTimeout, server.IdleTimeout)
	}
	if server.MaxHeaderBytes != httpMaxHeaderBytes {
		t.Fatalf("MaxHeaderBytes = %d, want %d", server.MaxHeaderBytes, httpMaxHeaderBytes)
	}
	if server.WriteTimeout != 0 {
		t.Fatalf("WriteTimeout = %s, want 0 so bounded long-running responses are not cut off", server.WriteTimeout)
	}
}
