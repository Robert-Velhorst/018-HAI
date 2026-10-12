package source

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/googleoauth"
	"automation-hub-backend/internal/models"
)

func TestGoogleProviderSyncErrorClassifiesProviderStatuses(t *testing.T) {
	for _, provider := range []string{"Google Drive", "Google Calendar", "Gmail"} {
		for _, test := range []struct {
			status    int
			retryable bool
		}{{http.StatusUnauthorized, false}, {http.StatusNotFound, false}, {http.StatusBadRequest, false}, {http.StatusTooManyRequests, true}, {http.StatusServiceUnavailable, true}, {http.StatusNotImplemented, false}, {http.StatusHTTPVersionNotSupported, false}} {
			t.Run(provider+"/"+http.StatusText(test.status), func(t *testing.T) {
				cause := &googleoauth.ProviderAPIError{Provider: provider, StatusCode: test.status, RetryAfterHeader: "17"}
				err := googleProviderSyncError(provider, cause)
				var providerErr *providerSyncError
				if !errors.As(err, &providerErr) {
					t.Fatalf("error type = %T, want providerSyncError: %v", err, err)
				}
				if providerErr.upstreamStatus != test.status || providerErr.Retryable() != test.retryable {
					t.Fatalf("upstream=%d retryable=%v; want upstream=%d retryable=%v", providerErr.upstreamStatus, providerErr.Retryable(), test.status, test.retryable)
				}
				if test.retryable && providerErr.RetryAfter() != 17*time.Second {
					t.Fatalf("RetryAfter() = %s, want 17s", providerErr.RetryAfter())
				}
			})
		}
	}
}

func TestGoogleProviderRetryAfterParsing(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	futureDate := now.Add(2 * time.Minute).Format(http.TimeFormat)
	tooFarDate := now.Add(maxProviderSyncRetryAfter + time.Hour).Format(http.TimeFormat)
	tooLong := fmt.Sprintf("%d", (maxProviderSyncRetryAfter/time.Second)+1)
	for _, test := range []struct {
		name             string
		header           string
		wantDelay        time.Duration
		wantRetryable    bool
		wantDelayAtLeast time.Duration
	}{
		{name: "seconds", header: "23", wantDelay: 23 * time.Second, wantRetryable: true},
		{name: "HTTP date", header: futureDate, wantRetryable: true, wantDelayAtLeast: 90 * time.Second},
		{name: "malformed value is ignored", header: "soon-ish", wantRetryable: true},
		{name: "excessive value disables automatic retry", header: tooLong, wantRetryable: false},
		{name: "excessive HTTP date disables automatic retry", header: tooFarDate, wantRetryable: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			cause := &googleoauth.ProviderAPIError{Provider: "Google Drive", StatusCode: http.StatusServiceUnavailable, RetryAfterHeader: test.header}
			err := googleProviderSyncError("Google Drive", cause)
			var providerErr *providerSyncError
			if !errors.As(err, &providerErr) {
				t.Fatalf("error type = %T, want providerSyncError", err)
			}
			if providerErr.Retryable() != test.wantRetryable {
				t.Fatalf("Retryable() = %v, want %v", providerErr.Retryable(), test.wantRetryable)
			}
			if test.wantDelay > 0 && providerErr.RetryAfter() != test.wantDelay {
				t.Fatalf("RetryAfter() = %s, want %s", providerErr.RetryAfter(), test.wantDelay)
			}
			if test.wantDelayAtLeast > 0 && providerErr.RetryAfter() < test.wantDelayAtLeast {
				t.Fatalf("RetryAfter() = %s, want at least %s", providerErr.RetryAfter(), test.wantDelayAtLeast)
			}
		})
	}
}

func TestGmailSourceMapsProvider429IntoDurableRetryMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "19")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":"secret provider body"}`))
	}))
	defer server.Close()

	_, _, err := fetchGmailSourceWithClient(context.Background(), googleoauth.GmailClient{AccessToken: "token", BaseURL: server.URL}, &models.ConnectedSource{})
	var providerErr *providerSyncError
	if !errors.As(err, &providerErr) || !providerErr.Retryable() || providerErr.RetryAfter() != 19*time.Second || providerErr.upstreamStatus != http.StatusTooManyRequests {
		t.Fatalf("source error = %#v; want retryable Gmail 429 with 19s durable delay", err)
	}
	if strings.Contains(err.Error(), "secret provider body") {
		t.Fatalf("provider body leaked through source error: %v", err)
	}
}

func TestDriveSourceMapsProvider429IntoDurableRetryMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "19")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":"secret provider body"}`))
	}))
	defer server.Close()

	_, _, err := fetchDriveSourceWithClient(context.Background(), googleoauth.DriveClient{AccessToken: "token", BaseURL: server.URL}, &models.ConnectedSource{})
	var providerErr *providerSyncError
	if !errors.As(err, &providerErr) || !providerErr.Retryable() || providerErr.RetryAfter() != 19*time.Second || providerErr.upstreamStatus != http.StatusTooManyRequests {
		t.Fatalf("source error = %#v; want retryable 429 with 19s durable delay", err)
	}
	if strings.Contains(err.Error(), "secret provider body") {
		t.Fatalf("provider body leaked through source error: %v", err)
	}
}

func TestCalendarSourceMapsProvider503DateIntoDurableRetryMetadata(t *testing.T) {
	retryAt := time.Now().UTC().Add(2 * time.Minute).Format(http.TimeFormat)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", retryAt)
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":"secret provider body"}`))
	}))
	defer server.Close()

	_, _, err := fetchCalendarSourceWithClient(context.Background(), googleoauth.CalendarClient{AccessToken: "token", BaseURL: server.URL}, &models.ConnectedSource{}, time.Now().UTC())
	var providerErr *providerSyncError
	if !errors.As(err, &providerErr) || !providerErr.Retryable() || providerErr.RetryAfter() < 90*time.Second || providerErr.upstreamStatus != http.StatusServiceUnavailable {
		t.Fatalf("source error = %#v; want retryable 503 with bounded HTTP-date delay", err)
	}
	if strings.Contains(err.Error(), "secret provider body") {
		t.Fatalf("provider body leaked through source error: %v", err)
	}
}
