package source

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"automation-hub-backend/internal/lifecycle"

	"github.com/google/uuid"
)

type googleOAuthReadRetryTransport struct {
	base           http.RoundTripper
	allowedOrigins map[string]struct{}
	refresh        func(context.Context, string) (string, error)

	mu               sync.Mutex
	accessToken      string
	refreshAttempted bool
	refreshDone      chan struct{}
	refreshErr       error
}

func (t *googleOAuthReadRetryTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request == nil {
		return nil, fmt.Errorf("Google OAuth request is required")
	}
	if !t.isGoogleRead(request) {
		return t.baseTransport().RoundTrip(request)
	}

	accessToken, err := t.accessTokenForRequest(request.Context())
	if err != nil {
		return nil, fmt.Errorf("Google source sync cannot continue after token refresh failure: %w", err)
	}
	firstRequest := request.Clone(request.Context())
	firstRequest.Header = request.Header.Clone()
	if firstRequest.Header == nil {
		firstRequest.Header = make(http.Header)
	}
	if accessToken != "" {
		firstRequest.Header.Set("Authorization", "Bearer "+accessToken)
	}

	response, err := t.baseTransport().RoundTrip(firstRequest)
	if err != nil || response == nil || response.StatusCode != http.StatusUnauthorized {
		return response, err
	}

	refreshedToken, shouldRetry, refreshErr := t.refreshOnce(request.Context(), accessToken)
	if !shouldRetry {
		if refreshErr != nil {
			closeResponseBody(response)
			return nil, fmt.Errorf("refresh Google OAuth token after HTTP 401: %w", refreshErr)
		}
		return response, nil
	}
	closeResponseBody(response)

	retryRequest := request.Clone(request.Context())
	retryRequest.Header = request.Header.Clone()
	if retryRequest.Header == nil {
		retryRequest.Header = make(http.Header)
	}
	retryRequest.Header.Set("Authorization", "Bearer "+refreshedToken)
	return t.baseTransport().RoundTrip(retryRequest)
}

func (t *googleOAuthReadRetryTransport) isGoogleRead(request *http.Request) bool {
	if request.Method != http.MethodGet || request.Body != nil && request.Body != http.NoBody || request.URL == nil {
		return false
	}
	_, allowed := t.allowedOrigins[googleOAuthRequestOrigin(request.URL)]
	return allowed
}

func googleOAuthRequestOrigin(target *url.URL) string {
	if target == nil {
		return ""
	}
	scheme := strings.ToLower(target.Scheme)
	host := strings.ToLower(target.Hostname())
	port := target.Port()
	if (scheme == "https" && port == "443") || (scheme == "http" && port == "80") {
		port = ""
	}
	if port != "" {
		host = net.JoinHostPort(host, port)
	} else if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return scheme + "://" + host
}

func (t *googleOAuthReadRetryTransport) baseTransport() http.RoundTripper {
	if t.base != nil {
		return t.base
	}
	return http.DefaultTransport
}

func (t *googleOAuthReadRetryTransport) accessTokenForRequest(ctx context.Context) (string, error) {
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		t.mu.Lock()
		done := t.refreshDone
		if !t.refreshAttempted || done == nil {
			accessToken, err := t.accessToken, t.refreshErr
			t.mu.Unlock()
			return accessToken, err
		}
		t.mu.Unlock()
		select {
		case <-done:
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
}

// refreshOnce coalesces concurrent 401s and permits one forced refresh for the
// lifetime of a source-sync client. Only callers still using the rejected token
// retry; a 401 from the newly refreshed token is returned without another try.
func (t *googleOAuthReadRetryTransport) refreshOnce(ctx context.Context, rejectedToken string) (string, bool, error) {
	if err := ctx.Err(); err != nil {
		return "", false, err
	}
	t.mu.Lock()
	if !t.refreshAttempted {
		t.refreshAttempted = true
		t.refreshDone = make(chan struct{})
		// Each request can stop waiting independently. The shared refresh has
		// its own timeout and is attempted only once for this sync client.
		refreshCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), sourceHTTPTimeout())
		done := t.refreshDone
		if !lifecycle.Go(refreshCtx, "google-oauth-refresh", func() {
			t.completeRefresh(refreshCtx, cancel, rejectedToken, done)
		}) {
			cancel()
			t.refreshErr = context.Canceled
			close(done)
			t.refreshDone = nil
		}
	}
	done := t.refreshDone
	t.mu.Unlock()
	if done != nil {
		select {
		case <-done:
		case <-ctx.Done():
			return "", false, ctx.Err()
		}
	}
	if err := ctx.Err(); err != nil {
		return "", false, err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.refreshErr != nil {
		return "", false, t.refreshErr
	}
	if t.accessToken == "" || t.accessToken == rejectedToken {
		return "", false, nil
	}
	return t.accessToken, true, nil
}

func (t *googleOAuthReadRetryTransport) completeRefresh(ctx context.Context, cancel context.CancelFunc, rejectedToken string, done chan struct{}) {
	defer cancel()
	var refreshedToken string
	var refreshErr error
	if t.refresh == nil {
		refreshErr = fmt.Errorf("Google OAuth refresh callback is unavailable")
	} else {
		refreshedToken, refreshErr = t.refresh(ctx, rejectedToken)
		refreshedToken = strings.TrimSpace(refreshedToken)
		if refreshErr == nil && refreshedToken == "" {
			refreshErr = fmt.Errorf("Google OAuth refresh returned an empty access token")
		} else if refreshErr == nil && refreshedToken == rejectedToken {
			refreshErr = fmt.Errorf("Google OAuth refresh returned the rejected access token unchanged")
		}
	}
	if err := ctx.Err(); err != nil {
		refreshErr = err
	}

	t.mu.Lock()
	if refreshErr == nil {
		t.accessToken = refreshedToken
	}
	t.refreshErr = refreshErr
	close(done)
	t.refreshDone = nil
	t.mu.Unlock()
}

func closeResponseBody(response *http.Response) {
	if response != nil && response.Body != nil {
		_ = response.Body.Close()
	}
}

func googleOAuthAPIHost(connectorKey string) (string, bool) {
	switch strings.TrimSpace(connectorKey) {
	case gmailConnectorKey:
		return "gmail.googleapis.com", true
	case driveConnectorKey, calendarConnectorKey:
		return "www.googleapis.com", true
	case contactsConnectorKey:
		return "people.googleapis.com", true
	default:
		return "", false
	}
}

func (s *service) googleOAuthReadHTTPClient(sourceID uuid.UUID, connectorKey, accessToken string) *http.Client {
	host, supported := googleOAuthAPIHost(connectorKey)
	transport := &googleOAuthReadRetryTransport{
		base:           sourceHTTPTransport(),
		allowedOrigins: make(map[string]struct{}),
		accessToken:    accessToken,
	}
	if supported {
		transport.allowedOrigins["https://"+host] = struct{}{}
	}
	transport.refresh = func(ctx context.Context, rejectedToken string) (string, error) {
		return s.forceGoogleAccessTokenRefresh(ctx, sourceID, connectorKey, rejectedToken)
	}
	return &http.Client{
		Timeout:       sourceHTTPTimeout(),
		Transport:     transport,
		CheckRedirect: transport.checkRedirect,
	}
}

func (t *googleOAuthReadRetryTransport) checkRedirect(request *http.Request, via []*http.Request) error {
	if request == nil || request.URL == nil || len(via) == 0 || len(via) > 5 {
		return http.ErrUseLastResponse
	}
	if request.Method != http.MethodGet || request.Body != nil && request.Body != http.NoBody {
		return http.ErrUseLastResponse
	}
	if _, allowed := t.allowedOrigins[googleOAuthRequestOrigin(via[0].URL)]; !allowed {
		return http.ErrUseLastResponse
	}
	target := request.URL
	if !trustedGoogleOAuthRedirectTarget(target) {
		return http.ErrUseLastResponse
	}
	if googleOAuthRequestOrigin(target) != googleOAuthRequestOrigin(via[0].URL) {
		request.Header.Del("Authorization")
		request.Header.Del("Cookie")
	}
	return nil
}

func trustedGoogleOAuthRedirectTarget(target *url.URL) bool {
	if target == nil || target.User != nil || !strings.EqualFold(target.Scheme, "https") {
		return false
	}
	if target.Port() != "" && target.Port() != "443" {
		return false
	}
	host := strings.TrimSuffix(strings.ToLower(target.Hostname()), ".")
	for _, suffix := range []string{"googleapis.com", "googleusercontent.com"} {
		if host == suffix || strings.HasSuffix(host, "."+suffix) {
			return true
		}
	}
	return false
}
