package source

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func TestTrelloQuietBoardCursorRequiresAnObservedArray(t *testing.T) {
	for _, payload := range []string{"null", "{}", "[]"} {
		t.Run(payload, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != "/1/boards/board-1/actions" {
					t.Errorf("unexpected activity request: %s %s", r.Method, r.URL.Path)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(payload))
			}))
			defer server.Close()
			configureTrelloTest(t, server.URL)
			base, err := url.Parse(server.URL)
			if err != nil {
				t.Fatal(err)
			}
			client := &trelloAPIClient{base: base, key: "test-key", token: "test-read-token"}
			quiet, cursor, err := trelloQuietBoardCursor(context.Background(), client, "board-1", "existing-cursor", time.Now().UTC())
			if payload == "[]" {
				if err != nil || !quiet || cursor != "existing-cursor" {
					t.Fatalf("observed empty array rejected: quiet=%t cursor=%q err=%v", quiet, cursor, err)
				}
			} else if err == nil || quiet || cursor != "" {
				t.Fatalf("unobserved activity was accepted: quiet=%t cursor=%q err=%v", quiet, cursor, err)
			}
		})
	}
}
