package source

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestProviderSyncErrorClassification(t *testing.T) {
	tests := []struct {
		name           string
		status         int
		cause          error
		wantHTTPStatus int
		wantRetryable  bool
		wantMessage    string
	}{
		{name: "rate limited", status: http.StatusTooManyRequests, cause: errors.New("secret response"), wantHTTPStatus: http.StatusServiceUnavailable, wantRetryable: true, wantMessage: "rate limit"},
		{name: "request timeout", status: http.StatusRequestTimeout, wantHTTPStatus: http.StatusGatewayTimeout, wantRetryable: true, wantMessage: "timed out"},
		{name: "redirect is not retried", status: http.StatusFound, wantHTTPStatus: http.StatusBadGateway, wantRetryable: false, wantMessage: "redirected"},
		{name: "temporary provider failure", status: http.StatusServiceUnavailable, cause: errors.New("secret response"), wantHTTPStatus: http.StatusBadGateway, wantRetryable: true, wantMessage: "temporary error"},
		{name: "not implemented is permanent", status: http.StatusNotImplemented, wantHTTPStatus: http.StatusBadGateway, wantRetryable: false, wantMessage: "does not support"},
		{name: "unsupported HTTP version is permanent", status: http.StatusHTTPVersionNotSupported, wantHTTPStatus: http.StatusBadGateway, wantRetryable: false, wantMessage: "does not support"},
		{name: "bad credentials", status: http.StatusUnauthorized, cause: errors.New("secret response"), wantHTTPStatus: http.StatusBadGateway, wantRetryable: false, wantMessage: "configured credentials"},
		{name: "forbidden is permanent by default", status: http.StatusForbidden, wantHTTPStatus: http.StatusBadGateway, wantRetryable: false, wantMessage: "configured credentials"},
		{name: "deadline", cause: context.DeadlineExceeded, wantHTTPStatus: http.StatusGatewayTimeout, wantRetryable: true, wantMessage: "timed out"},
		{name: "cancellation", cause: context.Canceled, wantHTTPStatus: http.StatusServiceUnavailable, wantRetryable: true, wantMessage: "interrupted"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := newProviderSyncError("Trello", test.status, test.cause)
			if err.statusCode != test.wantHTTPStatus || err.Retryable() != test.wantRetryable {
				t.Fatalf("status/retryable = %d/%v, want %d/%v", err.statusCode, err.Retryable(), test.wantHTTPStatus, test.wantRetryable)
			}
			if !strings.Contains(err.Error(), test.wantMessage) || strings.Contains(err.Error(), "secret response") {
				t.Fatalf("public error = %q, expected safe message containing %q", err.Error(), test.wantMessage)
			}
		})
	}
}

func TestGitHubRateLimitErrorOptionsDistinguishRateLimitsFromPermissions(t *testing.T) {
	tests := []struct {
		name        string
		status      int
		remaining   string
		reset       string
		retryAfter  string
		body        string
		wantLimited bool
		wantDelay   string
	}{
		{name: "primary limit uses reset", status: http.StatusForbidden, remaining: "0", reset: strconv.FormatInt(time.Now().Add(90*time.Second).Unix(), 10), wantLimited: true},
		{name: "secondary limit honors provider delay", status: http.StatusForbidden, retryAfter: "120", body: `{"message":"You have exceeded a secondary rate limit"}`, wantLimited: true, wantDelay: "120"},
		{name: "secondary limit uses documented minimum when header is absent", status: http.StatusForbidden, body: `{"message":"secondary rate limit"}`, wantLimited: true, wantDelay: "60"},
		{name: "ordinary permission denial remains permanent", status: http.StatusForbidden, body: `{"message":"Resource not accessible by integration"}`},
		{name: "429 is a rate limit", status: http.StatusTooManyRequests, wantLimited: true, wantDelay: "60"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := &http.Response{
				StatusCode: test.status,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(test.body)),
			}
			response.Header.Set("X-RateLimit-Remaining", test.remaining)
			response.Header.Set("X-RateLimit-Reset", test.reset)
			response.Header.Set("Retry-After", test.retryAfter)
			options, limited := githubRateLimitErrorOptions(response)
			if limited != test.wantLimited {
				t.Fatalf("rate-limited = %v, want %v", limited, test.wantLimited)
			}
			if !limited {
				return
			}
			if options.RetryableOverride == nil || !*options.RetryableOverride {
				t.Fatal("rate limit must be retryable")
			}
			delay, valid, exceedsLimit := parseProviderRetryAfter(options.RetryAfterHeader, time.Now().UTC())
			if !valid || exceedsLimit {
				t.Fatalf("Retry-After %q is not a bounded delay", options.RetryAfterHeader)
			}
			if test.wantDelay != "" && delay != time.Duration(mustParseInt64(t, test.wantDelay))*time.Second {
				t.Fatalf("retry delay = %s, want %s seconds", delay, test.wantDelay)
			}
			if test.name == "primary limit uses reset" && (delay < 80*time.Second || delay > 90*time.Second) {
				t.Fatalf("primary reset delay = %s, want between 80 and 90 seconds", delay)
			}
		})
	}
}

func mustParseInt64(t *testing.T, value string) int64 {
	t.Helper()
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		t.Fatalf("parse int64: %v", err)
	}
	return parsed
}

func TestFetchGitHubJSONRateLimitAndDeterministicResponseFailures(t *testing.T) {
	t.Setenv("CONNECTED_SOURCE_HTTP_ALLOWED_HOSTS", "127.0.0.1")
	t.Setenv("CONNECTED_SOURCE_HTTP_ALLOW_LINK_LOCAL", "true")
	tests := []struct {
		name            string
		status          int
		remaining       string
		retryAfter      string
		body            string
		wantRetryable   bool
		wantRetryAfter  time.Duration
		wantPublicError string
	}{
		{
			name:   "primary rate limit is deferred",
			status: http.StatusForbidden, remaining: "0",
			retryAfter: "120", body: `{"message":"private rate limit details"}`,
			wantRetryable: true, wantRetryAfter: 2 * time.Minute, wantPublicError: "temporarily refused",
		},
		{
			name:   "permission denial is permanent",
			status: http.StatusForbidden, remaining: "10", body: `{"message":"Resource not accessible by integration"}`,
			wantRetryable: false, wantPublicError: "configured credentials",
		},
		{
			name:   "malformed success body is not retried",
			status: http.StatusOK, body: "not json",
			wantRetryable: false, wantPublicError: "could not be processed",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.Header.Get("Accept") != "application/vnd.github+json" {
					t.Errorf("request method/accept = %s/%q", r.Method, r.Header.Get("Accept"))
				}
				if test.remaining != "" {
					w.Header().Set("X-RateLimit-Remaining", test.remaining)
				}
				if test.retryAfter != "" {
					w.Header().Set("Retry-After", test.retryAfter)
				}
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(test.body))
			}))
			defer server.Close()
			t.Setenv("CONNECTED_SOURCE_HTTP_LOOPBACK_ADDRS", server.Listener.Addr().String())
			base, err := url.Parse(server.URL)
			if err != nil {
				t.Fatalf("parse test server URL: %v", err)
			}
			_, err = fetchGitHubJSON(t.Context(), base, "/repos/example/project", "", 1, "test-secret")
			if err == nil {
				t.Fatal("fetchGitHubJSON unexpectedly succeeded")
			}
			var providerErr *providerSyncError
			if !errors.As(err, &providerErr) {
				t.Fatalf("error type = %T, want providerSyncError: %v", err, err)
			}
			if providerErr.Retryable() != test.wantRetryable {
				t.Fatalf("Retryable() = %v, want %v", providerErr.Retryable(), test.wantRetryable)
			}
			if test.wantRetryAfter > 0 && providerErr.RetryAfter() != test.wantRetryAfter {
				t.Fatalf("RetryAfter() = %s, want %s", providerErr.RetryAfter(), test.wantRetryAfter)
			}
			if !strings.Contains(err.Error(), test.wantPublicError) || strings.Contains(err.Error(), "private rate limit details") || strings.Contains(err.Error(), "test-secret") {
				t.Fatalf("unsafe or unexpected public error: %q", err.Error())
			}
		})
	}
}

func TestProviderSyncErrorAllowsProviderSpecificRetryClassification(t *testing.T) {
	retryable := true
	err := newProviderSyncErrorWithOptions("GitHub", http.StatusForbidden, errors.New("private rate-limit response"), providerSyncErrorOptions{
		RetryableOverride: &retryable,
		RetryAfterHeader:  "120",
	})
	if !err.Retryable() {
		t.Fatal("provider-specific rate-limit classification was not applied")
	}
	if err.statusCode != http.StatusServiceUnavailable {
		t.Fatalf("rate-limited forbidden status = %d, want service unavailable", err.statusCode)
	}
	if got := err.RetryAfter(); got != 2*time.Minute {
		t.Fatalf("RetryAfter() = %s, want 2m", got)
	}
	if strings.Contains(err.Error(), "private rate-limit response") {
		t.Fatalf("public error exposed provider detail: %q", err.Error())
	}

	notRetryable := false
	err = newProviderSyncErrorWithOptions("Example", http.StatusServiceUnavailable, nil, providerSyncErrorOptions{
		RetryableOverride: &notRetryable,
	})
	if err.Retryable() || !strings.Contains(err.Error(), "manual review") {
		t.Fatalf("provider-specific permanent classification = (%v, %q), want false/manual-review message", err.Retryable(), err.Error())
	}
}

func TestProviderSyncErrorBoundsRetryAfterMetadata(t *testing.T) {
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name          string
		header        string
		wantDelay     time.Duration
		wantRetryable bool
		wantMessage   string
	}{
		{name: "valid seconds", header: "15", wantDelay: 15 * time.Second, wantRetryable: true},
		{name: "valid HTTP date", header: now.Add(90 * time.Second).Format(http.TimeFormat), wantDelay: 90 * time.Second, wantRetryable: true},
		{name: "expired HTTP date does not delay backoff", header: now.Add(-time.Minute).Format(http.TimeFormat), wantDelay: 0, wantRetryable: true},
		{name: "invalid metadata ignored", header: "tomorrow-ish", wantDelay: 0, wantRetryable: true},
		{name: "over-limit valid delay disables automatic retry", header: "86401", wantDelay: 0, wantRetryable: false, wantMessage: "beyond the automatic retry limit"},
		{name: "overflowing delay disables automatic retry", header: "999999999999999999999999999999", wantDelay: 0, wantRetryable: false, wantMessage: "beyond the automatic retry limit"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := newProviderSyncErrorWithOptions("Trello", http.StatusTooManyRequests, nil, providerSyncErrorOptions{
				RetryAfterHeader: test.header,
				Now:              now,
			})
			if got := err.RetryAfter(); got != test.wantDelay {
				t.Fatalf("RetryAfter() = %s, want %s", got, test.wantDelay)
			}
			if got := err.Retryable(); got != test.wantRetryable {
				t.Fatalf("Retryable() = %v, want %v", got, test.wantRetryable)
			}
			if test.wantMessage != "" && !strings.Contains(err.Error(), test.wantMessage) {
				t.Fatalf("public error = %q, want safe message containing %q", err.Error(), test.wantMessage)
			}
		})
	}
}

func TestClassifySyncFailureSeparatesContextFromBadInput(t *testing.T) {
	tests := []struct {
		name        string
		err         error
		wantStatus  int
		wantRetry   bool
		wantHandled bool
	}{
		{name: "deadline", err: context.DeadlineExceeded, wantStatus: http.StatusGatewayTimeout, wantRetry: true, wantHandled: true},
		{name: "canceled", err: context.Canceled, wantStatus: http.StatusServiceUnavailable, wantRetry: true, wantHandled: true},
		{name: "bad request remains caller error", err: errors.New("invalid sync request"), wantHandled: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			details, handled := classifySyncFailure(test.err)
			if handled != test.wantHandled {
				t.Fatalf("handled = %v, want %v", handled, test.wantHandled)
			}
			if handled && (details.statusCode != test.wantStatus || details.retryable != test.wantRetry) {
				t.Fatalf("failure details = %#v, want status/retryable %d/%v", details, test.wantStatus, test.wantRetry)
			}
		})
	}
}
