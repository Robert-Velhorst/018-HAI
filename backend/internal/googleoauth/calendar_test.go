package googleoauth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCalendarClientUsesReadonlyBoundedInitialRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet || request.Header.Get("Authorization") != "Bearer token" {
			t.Fatalf("request = %s auth=%q", request.Method, request.Header.Get("Authorization"))
		}
		query := request.URL.Query()
		if query.Get("timeMin") == "" || query.Get("syncToken") != "" || query.Get("showDeleted") != "true" || query.Get("singleEvents") != "true" {
			t.Fatalf("initial query = %v", query)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"items":[{"id":"event-1","summary":"Review"}],"nextSyncToken":"sync-1"}`))
	}))
	defer server.Close()

	page, err := (CalendarClient{AccessToken: "token", BaseURL: server.URL}).ListPrimaryEventsPage(context.Background(), "", "", "2025-01-01T00:00:00Z", 200)
	if err != nil || len(page.Events) != 1 || page.NextSyncToken != "sync-1" {
		t.Fatalf("page=%#v err=%v", page, err)
	}
}

func TestCalendarClientIncrementalRequestOmitsTimeMin(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		query := request.URL.Query()
		if query.Get("syncToken") != "sync-1" || query.Get("pageToken") != "page-2" || query.Has("timeMin") {
			t.Fatalf("incremental query = %v", query)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"items":[],"nextSyncToken":"sync-2"}`))
	}))
	defer server.Close()
	_, err := (CalendarClient{AccessToken: "token", BaseURL: server.URL}).ListPrimaryEventsPage(context.Background(), "page-2", "sync-1", "ignored", 200)
	if err != nil {
		t.Fatal(err)
	}
}

func TestCalendarClientReturnsTypedExpiredSyncToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) { writer.WriteHeader(http.StatusGone) }))
	defer server.Close()
	_, err := (CalendarClient{AccessToken: "token", BaseURL: server.URL}).ListPrimaryEventsPage(context.Background(), "", "expired", "", 200)
	if !errors.Is(err, ErrCalendarSyncTokenExpired) {
		t.Fatalf("error = %v", err)
	}
}

func TestCalendarProviderAPIErrorPreservesSafeRetryMetadata(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusNotFound, http.StatusBadRequest, http.StatusTooManyRequests, http.StatusServiceUnavailable, http.StatusGone} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Retry-After", "17")
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"error":"private response content"}`))
			}))
			defer server.Close()

			_, err := (CalendarClient{AccessToken: "token", BaseURL: server.URL}).ListPrimaryEventsPage(context.Background(), "", "sync-token", "", 100)
			var apiErr *ProviderAPIError
			if !errors.As(err, &apiErr) || apiErr.StatusCode != status || apiErr.RetryAfterHeader != "17" {
				t.Fatalf("error = %#v; want typed status %d and Retry-After metadata", err, status)
			}
			if strings.Contains(err.Error(), "private response content") {
				t.Fatalf("provider body leaked through error: %v", err)
			}
			if status == http.StatusGone && !errors.Is(err, ErrCalendarSyncTokenExpired) {
				t.Fatalf("410 error = %v; want ErrCalendarSyncTokenExpired preserved", err)
			}
		})
	}
}
