package source

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestGoogleOAuthReadRetryTransportRefreshesOncePerSync(t *testing.T) {
	var mu sync.Mutex
	var seen []string
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		requests++
		seen = append(seen, r.Header.Get("Authorization"))
		if r.URL.Path == "/second" || r.Header.Get("Authorization") != "Bearer fresh-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	refreshCalls := 0
	transport := &googleOAuthReadRetryTransport{
		base:           http.DefaultTransport,
		allowedOrigins: map[string]struct{}{server.URL: {}},
		accessToken:    "stale-token",
		refresh: func(context.Context, string) (string, error) {
			refreshCalls++
			return "fresh-token", nil
		},
	}
	client := &http.Client{Transport: transport}

	first, err := client.Get(server.URL + "/first")
	if err != nil {
		t.Fatalf("first GET: %v", err)
	}
	_ = first.Body.Close()
	if first.StatusCode != http.StatusOK {
		t.Fatalf("first GET status = %d, want 200", first.StatusCode)
	}
	second, err := client.Get(server.URL + "/second")
	if err != nil {
		t.Fatalf("second GET: %v", err)
	}
	_ = second.Body.Close()
	if second.StatusCode != http.StatusUnauthorized {
		t.Fatalf("second GET status = %d, want un-retried 401", second.StatusCode)
	}
	if requests != 3 || refreshCalls != 1 {
		t.Fatalf("requests=%d refreshes=%d, want 3 requests and exactly 1 refresh", requests, refreshCalls)
	}
	if len(seen) != 3 || seen[0] != "Bearer stale-token" || seen[1] != "Bearer fresh-token" || seen[2] != "Bearer fresh-token" {
		t.Fatalf("Authorization sequence = %#v", seen)
	}
}

type googleOAuthTestRoundTripper func(*http.Request) (*http.Response, error)

func (f googleOAuthTestRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func googleOAuthTestResponse(request *http.Request, status int) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("")), Request: request}
}

type googleOAuthWaitObservedContext struct {
	context.Context
	waiting chan struct{}
	once    sync.Once
}

func (ctx *googleOAuthWaitObservedContext) Done() <-chan struct{} {
	ctx.once.Do(func() { close(ctx.waiting) })
	return ctx.Context.Done()
}

func TestGoogleOAuthReadRetryTransportCoalescesSimultaneous401s(t *testing.T) {
	const callers = 8
	arrivals := make(chan struct{}, callers)
	release := make(chan struct{})
	var refreshes, retries atomic.Int32
	transport := &googleOAuthReadRetryTransport{
		allowedOrigins: map[string]struct{}{"https://www.googleapis.com": {}},
		accessToken:    "stale-token",
		base: googleOAuthTestRoundTripper(func(request *http.Request) (*http.Response, error) {
			if request.Header.Get("Authorization") == "Bearer stale-token" {
				arrivals <- struct{}{}
				select {
				case <-release:
					return googleOAuthTestResponse(request, http.StatusUnauthorized), nil
				case <-request.Context().Done():
					return nil, request.Context().Err()
				}
			}
			if request.Header.Get("Authorization") != "Bearer fresh-token" {
				return nil, errors.New("unexpected retry credential")
			}
			retries.Add(1)
			return googleOAuthTestResponse(request, http.StatusOK), nil
		}),
		refresh: func(context.Context, string) (string, error) {
			refreshes.Add(1)
			return "fresh-token", nil
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	errs := make(chan error, callers)
	for i := 0; i < callers; i++ {
		go func() {
			request, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://www.googleapis.com/read", nil)
			response, err := transport.RoundTrip(request)
			if response != nil {
				closeResponseBody(response)
				if response.StatusCode != http.StatusOK {
					err = errors.New("request was not retried successfully")
				}
			}
			errs <- err
		}()
	}
	for i := 0; i < callers; i++ {
		select {
		case <-arrivals:
		case <-ctx.Done():
			t.Fatal("requests did not overlap")
		}
	}
	close(release)
	for i := 0; i < callers; i++ {
		if err := <-errs; err != nil {
			t.Fatalf("concurrent GET: %v", err)
		}
	}
	if refreshes.Load() != 1 || retries.Load() != callers {
		t.Fatalf("refreshes/retries = %d/%d, want 1/%d", refreshes.Load(), retries.Load(), callers)
	}
}

func TestGoogleOAuthReadRetryTransportCancellationDoesNotPoisonSharedRefresh(t *testing.T) {
	for _, cancelledCaller := range []string{"leader", "waiter"} {
		t.Run(cancelledCaller, func(t *testing.T) {
			started := make(chan context.Context, 1)
			release := make(chan struct{})
			var releaseOnce sync.Once
			finish := func() { releaseOnce.Do(func() { close(release) }) }
			defer finish()
			var refreshes atomic.Int32
			transport := &googleOAuthReadRetryTransport{
				accessToken: "stale-token",
				refresh: func(ctx context.Context, _ string) (string, error) {
					refreshes.Add(1)
					started <- ctx
					select {
					case <-release:
						return "fresh-token", nil
					case <-ctx.Done():
						return "", ctx.Err()
					}
				},
			}
			ctx, stop := context.WithTimeout(context.Background(), 5*time.Second)
			defer stop()
			leader, cancelLeader := context.WithCancel(ctx)
			defer cancelLeader()
			waiter, cancelWaiter := context.WithCancel(ctx)
			defer cancelWaiter()
			observedLeader := &googleOAuthWaitObservedContext{Context: leader, waiting: make(chan struct{})}
			observedWaiter := &googleOAuthWaitObservedContext{Context: waiter, waiting: make(chan struct{})}
			leaderResult := make(chan error, 1)
			waiterResult := make(chan error, 1)
			go func() {
				token, retry, err := transport.refreshOnce(observedLeader, "stale-token")
				if err == nil && (token != "fresh-token" || !retry) {
					err = errors.New("leader did not receive refreshed token")
				}
				leaderResult <- err
			}()
			var refreshCtx context.Context
			select {
			case refreshCtx = <-started:
			case <-ctx.Done():
				t.Fatal("refresh did not start")
			}
			if _, bounded := refreshCtx.Deadline(); !bounded {
				t.Fatal("shared refresh has no timeout")
			}
			go func() {
				token, retry, err := transport.refreshOnce(observedWaiter, "stale-token")
				if err == nil && (token != "fresh-token" || !retry) {
					err = errors.New("waiter did not receive refreshed token")
				}
				waiterResult <- err
			}()
			for _, waiting := range []chan struct{}{observedLeader.waiting, observedWaiter.waiting} {
				select {
				case <-waiting:
				case <-ctx.Done():
					t.Fatal("caller did not enter the shared refresh wait")
				}
			}
			cancelledResult, healthyResult := leaderResult, waiterResult
			if cancelledCaller == "leader" {
				cancelLeader()
			} else {
				cancelWaiter()
				cancelledResult, healthyResult = waiterResult, leaderResult
			}
			select {
			case err := <-cancelledResult:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("cancelled caller error = %v", err)
				}
			case <-ctx.Done():
				t.Fatal("cancelled caller kept waiting for refresh")
			}
			if err := refreshCtx.Err(); err != nil {
				t.Fatalf("one caller cancelled the shared refresh: %v", err)
			}
			finish()
			select {
			case err := <-healthyResult:
				if err != nil {
					t.Fatalf("healthy caller: %v", err)
				}
			case <-ctx.Done():
				t.Fatal("healthy caller did not receive refresh result")
			}
			if token, err := transport.accessTokenForRequest(ctx); err != nil || token != "fresh-token" {
				t.Fatalf("later request token/error = %q/%v", token, err)
			}
			if refreshes.Load() != 1 {
				t.Fatalf("refreshes = %d, want 1", refreshes.Load())
			}
			if _, retry, err := transport.refreshOnce(ctx, "fresh-token"); err != nil || retry {
				t.Fatalf("rejected fresh token retry/error = %v/%v", retry, err)
			}
			if refreshes.Load() != 1 {
				t.Fatal("a rejected fresh token triggered a second refresh")
			}
		})
	}
}

func TestGoogleOAuthReadRetryTransportCancelledRequestNeverStartsRefresh(t *testing.T) {
	transport := &googleOAuthReadRetryTransport{accessToken: "stale-token", refresh: func(context.Context, string) (string, error) {
		t.Fatal("cancelled request started a refresh")
		return "", nil
	}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := transport.refreshOnce(ctx, "stale-token"); !errors.Is(err, context.Canceled) {
		t.Fatalf("refresh error = %v", err)
	}
	if _, err := transport.accessTokenForRequest(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("token lookup error = %v", err)
	}
}

func TestGoogleOAuthReadRetryTransportNeverRetriesWritesOrOtherHosts(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()
	refreshCalls := 0
	transport := &googleOAuthReadRetryTransport{
		base:           http.DefaultTransport,
		allowedOrigins: map[string]struct{}{server.URL: {}},
		accessToken:    "stale-token",
		refresh: func(context.Context, string) (string, error) {
			refreshCalls++
			return "fresh-token", nil
		},
	}
	client := &http.Client{Transport: transport}

	write, err := http.NewRequest(http.MethodPost, server.URL+"/write", strings.NewReader("payload"))
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Do(write)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	_ = response.Body.Close()

	if requests != 1 || refreshCalls != 0 {
		t.Fatalf("POST requests=%d refreshes=%d, want 1 and 0", requests, refreshCalls)
	}
	// A GET to a host outside the connector's fixed Google API origin is also
	// outside the refresh/retry boundary.
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer other.Close()
	response, err = client.Get(other.URL + "/read")
	if err != nil {
		t.Fatalf("other-host GET: %v", err)
	}
	_ = response.Body.Close()
	if refreshCalls != 0 {
		t.Fatalf("refreshes after non-Google-host GET = %d, want 0", refreshCalls)
	}
}

func TestGoogleOAuthReadRetryTransportReturnsRefreshFailureWithoutRetry(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()
	refreshErr := errors.New("token repository unavailable")
	transport := &googleOAuthReadRetryTransport{
		base:           http.DefaultTransport,
		allowedOrigins: map[string]struct{}{server.URL: {}},
		accessToken:    "stale-token",
		refresh: func(context.Context, string) (string, error) {
			return "", refreshErr
		},
	}
	client := &http.Client{Transport: transport}
	_, err := client.Get(server.URL + "/read")
	if !errors.Is(err, refreshErr) {
		t.Fatalf("GET error = %v, want refresh error", err)
	}
	_, secondErr := client.Get(server.URL + "/read-again")
	if !errors.Is(secondErr, refreshErr) {
		t.Fatalf("second GET error = %v, want the stored refresh failure", secondErr)
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want no retry or later request with the stale token", requests)
	}
}

func TestGoogleOAuthRedirectPolicyAllowsGoogleMediaHostsWithoutForwardingCredentials(t *testing.T) {
	transport := &googleOAuthReadRetryTransport{
		allowedOrigins: map[string]struct{}{"https://www.googleapis.com": {}},
	}
	initial, err := http.NewRequest(http.MethodGet, "https://www.googleapis.com/drive/v3/files/file?alt=media", nil)
	if err != nil {
		t.Fatal(err)
	}
	initial.Header.Set("Authorization", "Bearer secret-token")
	initial.Header.Set("Cookie", "private=session")
	redirect, err := http.NewRequest(http.MethodGet, "https://abc123.googleusercontent.com/download/drive/v3/files/file?sig=signed", nil)
	if err != nil {
		t.Fatal(err)
	}
	redirect.Header = initial.Header.Clone()
	if err := transport.checkRedirect(redirect, []*http.Request{initial}); err != nil {
		t.Fatalf("trusted media redirect rejected: %v", err)
	}
	if redirect.Header.Get("Authorization") != "" || redirect.Header.Get("Cookie") != "" {
		t.Fatalf("credentials forwarded to media host: authorization=%q cookie=%q", redirect.Header.Get("Authorization"), redirect.Header.Get("Cookie"))
	}
}

func TestGoogleOAuthRedirectPolicyRejectsUntrustedAndUnsafeRedirects(t *testing.T) {
	transport := &googleOAuthReadRetryTransport{
		allowedOrigins: map[string]struct{}{"https://www.googleapis.com": {}},
	}
	initial, err := http.NewRequest(http.MethodGet, "https://www.googleapis.com/drive/v3/files/file?alt=media", nil)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		method string
		url    string
	}{
		{name: "external host", method: http.MethodGet, url: "https://attacker.example/file"},
		{name: "insecure scheme", method: http.MethodGet, url: "http://abc.googleusercontent.com/file"},
		{name: "nonstandard port", method: http.MethodGet, url: "https://abc.googleusercontent.com:444/file"},
		{name: "userinfo", method: http.MethodGet, url: "https://user:pass@abc.googleusercontent.com/file"},
		{name: "method change", method: http.MethodPost, url: "https://abc.googleusercontent.com/file"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request, err := http.NewRequest(test.method, test.url, nil)
			if err != nil {
				t.Fatal(err)
			}
			if err := transport.checkRedirect(request, []*http.Request{initial}); !errors.Is(err, http.ErrUseLastResponse) {
				t.Fatalf("checkRedirect() = %v, want redirect refusal", err)
			}
		})
	}
}
