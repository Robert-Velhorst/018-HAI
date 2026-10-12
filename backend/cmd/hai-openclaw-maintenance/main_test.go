package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestOnceModeUsesAuthenticatedAPIAndExits(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/api/v1/openclaw-maintenance-worker/leases" || r.Header.Get("Authorization") != "Bearer "+strings.Repeat("a", 64) || r.Header.Get("X-HAI-Backend-Key") != strings.Repeat("b", 64) {
			t.Error("incorrect route or credentials")
		}
		w.WriteHeader(204)
	}))
	defer server.Close()
	t.Setenv("HAI_OPENCLAW_MAINTENANCE_URL", server.URL)
	t.Setenv("HAI_OPENCLAW_MAINTENANCE_TOKEN", strings.Repeat("a", 64))
	t.Setenv("BACKEND_API_SHARED_KEY", strings.Repeat("b", 64))
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := run(ctx, []string{"--once"}); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || ctx.Err() != nil {
		t.Fatal("once mode did not return after one cycle")
	}
	for _, args := range [][]string{{"--typo"}, {"check", "invalid"}, {"--once", "extra"}} {
		if err := run(ctx, args); err == nil || !strings.Contains(err.Error(), "usage:") {
			t.Fatalf("invalid arguments accepted: %v", args)
		}
	}
	if calls != 1 {
		t.Fatal("invalid arguments contacted backend")
	}
}
