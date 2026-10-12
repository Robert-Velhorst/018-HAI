package router

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewHTTPServerAppliesRequestBoundsAndHandler(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	server := newHTTPServer(":8080", handler)
	if server.Handler == nil || server.Addr != ":8080" {
		t.Fatalf("server handler/address not configured: handler=%T addr=%q", server.Handler, server.Addr)
	}
	if server.ReadHeaderTimeout != 5*time.Second || server.ReadTimeout != 15*time.Second ||
		server.WriteTimeout != 90*time.Second || server.IdleTimeout != 60*time.Second || server.MaxHeaderBytes != 32*1024 {
		t.Fatalf("unexpected HTTP server limits: %#v", server)
	}

	response := httptest.NewRecorder()
	server.Handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/health", nil))
	if response.Code != http.StatusNoContent {
		t.Fatalf("handler status = %d, want %d", response.Code, http.StatusNoContent)
	}
}
