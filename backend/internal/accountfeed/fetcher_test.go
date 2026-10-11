package accountfeed

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestValidateFeedURLRejectsUnsafeAuthority(t *testing.T) {
	t.Parallel()

	for _, rawURL := range []string{
		"file:///tmp/feed.json",
		"http://metadata.google.internal/feed",
		"http://169.254.169.254/feed",
		"http://0.0.0.0/feed",
		"http://user:secret@example.com/feed",
	} {
		if err := validateFeedURL(rawURL); err == nil {
			t.Errorf("validateFeedURL(%q) succeeded, want rejection", rawURL)
		}
	}
}

func TestFeedHTTPClientRejectsBlockedDNSResolution(t *testing.T) {
	t.Parallel()

	client := newFeedHTTPClient(func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("169.254.169.254")}}, nil
	})
	transport := client.Transport.(*http.Transport)
	_, err := transport.DialContext(context.Background(), "tcp", "example.com:80")
	if err == nil || !strings.Contains(err.Error(), "blocked address space") {
		t.Fatalf("DialContext error = %v, want blocked address rejection", err)
	}
}

func TestFetchFeedBytesDoesNotFollowRedirects(t *testing.T) {
	t.Parallel()

	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"items":[{"id":"should-not-be-read"}]}`))
	}))
	defer destination.Close()

	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", destination.URL)
		w.WriteHeader(http.StatusFound)
	}))
	defer redirect.Close()

	_, err := fetchFeedBytes(context.Background(), Feed{SourceType: SourceHTTPJSONFeed, URL: redirect.URL}, FetchOptions{AllowHTTP: true})
	if err == nil || !strings.Contains(err.Error(), "feed HTTP 302") {
		t.Fatalf("fetchFeedBytes redirect error = %v, want HTTP 302 rejection", err)
	}
}

func TestFetchFeedBytesRejectsOversizedResponse(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, strings.Repeat("x", maxFeedBytes+1))
	}))
	defer server.Close()

	_, err := fetchFeedBytes(context.Background(), Feed{SourceType: SourceHTTPJSONFeed, URL: server.URL}, FetchOptions{AllowHTTP: true})
	if err == nil || !strings.Contains(err.Error(), "feed exceeds") {
		t.Fatalf("fetchFeedBytes oversized error = %v, want size rejection", err)
	}
}

func TestFetchFeedBytesReturnsContextCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := fetchFeedBytes(ctx, Feed{SourceType: SourceHTTPJSONFeed, URL: "http://localhost:9/feed"}, FetchOptions{AllowHTTP: true})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("fetchFeedBytes error = %v, want context cancellation", err)
	}
}
