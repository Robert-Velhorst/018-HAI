package accountfeed

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFetchHTTPFeedRejectsOversizedBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte(strings.Repeat("x", maxFeedBytes+1)))
	}))
	defer server.Close()

	feed := testFeed("")
	feed.SourceType = SourceHTTPJSONFeed
	feed.URL = server.URL
	if _, err := fetchFeedBytes(context.Background(), feed, FetchOptions{AllowHTTP: true, AllowLoopbackURL: feed.URL}); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("fetchFeedBytes error = %v, want explicit size-limit rejection", err)
	}
}

func TestFetchHTTPFeedDoesNotFollowRedirects(t *testing.T) {
	finalRequests := 0
	final := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		finalRequests++
		_, _ = writer.Write([]byte(`[]`))
	}))
	defer final.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Redirect(writer, request, final.URL, http.StatusFound)
	}))
	defer redirect.Close()

	feed := testFeed("")
	feed.SourceType = SourceHTTPJSONFeed
	feed.URL = redirect.URL
	if _, err := fetchFeedBytes(context.Background(), feed, FetchOptions{AllowHTTP: true, AllowLoopbackURL: feed.URL}); err == nil || !strings.Contains(err.Error(), "HTTP 302") {
		t.Fatalf("fetchFeedBytes error = %v, want redirect rejection", err)
	}
	if finalRequests != 0 {
		t.Fatalf("redirect target received %d request(s), want none", finalRequests)
	}
}

func TestFetchHTTPFeedRequiresSeparateLoopbackOptIn(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		requests++
		_, _ = writer.Write([]byte(`[]`))
	}))
	defer server.Close()

	feed := testFeed("")
	feed.SourceType = SourceHTTPJSONFeed
	feed.URL = server.URL
	if _, err := fetchFeedBytes(t.Context(), feed, FetchOptions{AllowHTTP: true}); err == nil || !strings.Contains(err.Error(), "loopback") {
		t.Fatalf("loopback fetch error = %v, want explicit policy rejection", err)
	}
	if requests != 0 {
		t.Fatalf("loopback server received %d request(s) without opt-in", requests)
	}
	if body, err := fetchFeedBytes(t.Context(), feed, FetchOptions{AllowHTTP: true, AllowLoopbackURL: feed.URL}); err != nil || string(body) != `[]` {
		t.Fatalf("explicitly opted-in local fetch = %q, %v", body, err)
	}
	if requests != 1 {
		t.Fatalf("loopback server received %d request(s), want exactly one opted-in request", requests)
	}
}

func TestFetchHTTPFeedHonorsRequestCancellation(t *testing.T) {
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		close(started)
		<-request.Context().Done()
	}))
	defer server.Close()

	feed := testFeed("")
	feed.SourceType = SourceHTTPJSONFeed
	feed.URL = server.URL
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		_, err := fetchFeedBytes(ctx, feed, FetchOptions{AllowHTTP: true, AllowLoopbackURL: feed.URL})
		done <- err
	}()
	<-started
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled HTTP fetch error = %v, want context.Canceled", err)
	}
}

func TestValidateFeedURLRejectsPrivateNetworkAddress(t *testing.T) {
	if err := validateFeedURL("http://192.168.1.10/feed.json"); err == nil || !strings.Contains(err.Error(), "private") {
		t.Fatalf("validateFeedURL error = %v, want private-address rejection", err)
	}
}
