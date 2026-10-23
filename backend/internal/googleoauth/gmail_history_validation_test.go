package googleoauth

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGmailHistoryRejectsMalformedResponseIDs(t *testing.T) {
	for _, test := range []struct {
		name string
		body string
	}{
		{"response_cursor", `{"historyId":"invalid-cursor","history":[]}`},
		{"response_cursor_whitespace", `{"historyId":" 205 ","history":[]}`},
		{"event_id", `{"historyId":"205","history":[{"id":"bad-event","messagesAdded":[{"message":{"id":"m1"}}]}]}`},
		{"event_id_sign", `{"historyId":"205","history":[{"id":"-201","messagesDeleted":[{"message":{"id":"m1"}}]}]}`},
		{"event_id_without_changes", `{"historyId":"205","history":[{"id":"bad-event"}]}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprint(w, test.body)
			}))
			defer server.Close()
			page, err := (GmailClient{BaseURL: server.URL, AccessToken: "test-token"}).ListHistoryPage(context.Background(), "200", "", 25)
			if err == nil || page.HistoryID != "" || len(page.Changes) != 0 || len(page.MessageIDs) != 0 {
				t.Fatalf("invalid response returned a usable page: page=%+v err=%v", page, err)
			}
		})
	}
}

func TestGmailHistoryPreservesLargeDecimalIDs(t *testing.T) {
	const large = "18446744073709551615"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"historyId":%q,"history":[{"id":%q,"messagesAdded":[{"message":{"id":"m1"}}]}]}`, large, large)
	}))
	defer server.Close()
	page, err := (GmailClient{BaseURL: server.URL, AccessToken: "test-token"}).ListHistoryPage(context.Background(), "200", "", 25)
	if err != nil || page.HistoryID != large || len(page.Changes) != 1 || page.Changes[0].HistoryID != large {
		t.Fatalf("valid decimal identifier changed: page=%+v err=%v", page, err)
	}
}
