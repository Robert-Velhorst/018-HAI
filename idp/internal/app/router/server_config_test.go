package router

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestIdentityHTTPServerBoundsAndHandler(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/auth/login" {
			t.Errorf("unexpected path: %q", r.URL.Path)
		}
		w.WriteHeader(http.StatusUnauthorized)
	})
	server := newHTTPServer(":8081", handler)
	if server.Addr != ":8081" {
		t.Fatalf("address = %q", server.Addr)
	}
	if server.ReadHeaderTimeout != 5*time.Second || server.ReadTimeout != 15*time.Second ||
		server.WriteTimeout != 90*time.Second || server.IdleTimeout != 60*time.Second {
		t.Fatalf("unexpected HTTP deadlines: header=%v read=%v write=%v idle=%v",
			server.ReadHeaderTimeout, server.ReadTimeout, server.WriteTimeout, server.IdleTimeout)
	}
	if server.MaxHeaderBytes != 32*1024 {
		t.Fatalf("header limit = %d", server.MaxHeaderBytes)
	}
	response := httptest.NewRecorder()
	server.Handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/auth/login", nil))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("handler status = %d; authentication response was not preserved", response.Code)
	}
}
