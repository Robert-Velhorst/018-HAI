package googleoauth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func testConfig(tokenURL string) Config {
	return Config{
		ClientID:      "client-123.apps.googleusercontent.com",
		ClientSecret:  "secret-abc",
		RedirectURL:   "https://example.ngrok-free.dev/api/v1/sources/oauth/google/callback",
		Scopes:        []string{GmailReadonlyScope},
		TokenEndpoint: tokenURL,
	}
}

func TestConfiguredRequiresAllThreeValues(t *testing.T) {
	full := testConfig("")
	if !full.Configured() {
		t.Fatal("a fully-populated config should be Configured")
	}
	for _, mutate := range []func(*Config){
		func(c *Config) { c.ClientID = "" },
		func(c *Config) { c.ClientSecret = "" },
		func(c *Config) { c.RedirectURL = "" },
	} {
		c := testConfig("")
		mutate(&c)
		if c.Configured() {
			t.Fatalf("config missing a required field must not be Configured: %+v", c)
		}
	}
}

func TestConfiguredRejectsTemplatePlaceholders(t *testing.T) {
	for _, value := range []string{
		"your-google-oauth-client-id",
		"YOUR_GOOGLE_OAUTH_CLIENT_SECRET",
		"replace-me",
		"changeme",
		"<set-this-value>",
	} {
		c := testConfig("")
		c.ClientSecret = value
		if c.Configured() {
			t.Fatalf("template placeholder %q must not enable OAuth", value)
		}
	}
}

func TestConfiguredRejectsUntrustedOAuthURLs(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Config)
	}{
		{
			name: "remote cleartext authorization endpoint",
			mutate: func(c *Config) {
				c.AuthEndpoint = "http://accounts.google.com/o/oauth2/v2/auth"
			},
		},
		{
			name: "untrusted authorization host",
			mutate: func(c *Config) {
				c.AuthEndpoint = "https://attacker.example/auth"
			},
		},
		{
			name: "remote cleartext token endpoint",
			mutate: func(c *Config) {
				c.TokenEndpoint = "http://attacker.example/token"
			},
		},
		{
			name: "untrusted token host",
			mutate: func(c *Config) {
				c.TokenEndpoint = "https://attacker.example/token"
			},
		},
		{
			name: "cleartext non-loopback redirect",
			mutate: func(c *Config) {
				c.RedirectURL = "http://callback.example/oauth"
			},
		},
		{
			name: "redirect with user information",
			mutate: func(c *Config) {
				c.RedirectURL = "https://user:password@callback.example/oauth"
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := testConfig("")
			test.mutate(&cfg)
			if cfg.Configured() {
				t.Fatal("unsafe OAuth URL configuration was accepted")
			}
		})
	}
}

func TestAuthorizeURLCarriesConsentParams(t *testing.T) {
	c := testConfig("")
	raw := c.AuthorizeURL("state-xyz")
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("authorize URL did not parse: %v", err)
	}
	q := u.Query()
	checks := map[string]string{
		"client_id":     c.ClientID,
		"redirect_uri":  c.RedirectURL,
		"response_type": "code",
		"scope":         GmailReadonlyScope,
		"access_type":   "offline", // required to receive a refresh token
		"prompt":        "consent",
		"state":         "state-xyz",
	}
	for k, want := range checks {
		if got := q.Get(k); got != want {
			t.Errorf("authorize URL %s = %q, want %q", k, got, want)
		}
	}
	if !strings.HasPrefix(raw, DefaultAuthEndpoint) {
		t.Errorf("authorize URL should target Google's auth endpoint, got %q", raw)
	}
}

func TestAuthorizeURLRequiresExactlyOneSupportedReadOnlyScope(t *testing.T) {
	validScopes := []struct {
		name  string
		scope string
	}{
		{name: "gmail", scope: GmailReadonlyScope},
		{name: "drive", scope: DriveReadonlyScope},
		{name: "calendar", scope: CalendarReadonlyScope},
		{name: "contacts", scope: ContactsReadonlyScope},
	}
	for _, test := range validScopes {
		t.Run(test.name, func(t *testing.T) {
			cfg := testConfig("")
			cfg.Scopes = []string{test.scope}
			raw := cfg.AuthorizeURL("state-xyz")
			if raw == "" {
				t.Fatal("AuthorizeURL rejected a supported connector read-only scope")
			}
			parsed, err := url.Parse(raw)
			if err != nil {
				t.Fatalf("parse authorization URL: %v", err)
			}
			if got := parsed.Query().Get("scope"); got != test.scope {
				t.Fatalf("scope = %q, want %q", got, test.scope)
			}
		})
	}

	invalidScopes := []struct {
		name   string
		scopes []string
	}{
		{name: "missing", scopes: nil},
		{name: "empty", scopes: []string{""}},
		{name: "whitespace", scopes: []string{" " + GmailReadonlyScope}},
		{name: "duplicate", scopes: []string{GmailReadonlyScope, GmailReadonlyScope}},
		{name: "combined connector scopes", scopes: []string{GmailReadonlyScope, DriveReadonlyScope}},
		{name: "write scope", scopes: []string{"https://www.googleapis.com/auth/gmail.modify"}},
		{name: "identity scope", scopes: []string{"openid"}},
		{name: "unknown scope", scopes: []string{"https://example.test/scope"}},
	}
	for _, test := range invalidScopes {
		t.Run("reject/"+test.name, func(t *testing.T) {
			cfg := testConfig("")
			cfg.Scopes = test.scopes
			if got := cfg.AuthorizeURL("state-xyz"); got != "" {
				t.Fatalf("AuthorizeURL = %q; want empty for scopes %q", got, test.scopes)
			}
			if got := cfg.AuthorizeURLWithPKCE("state-xyz", strings.Repeat("a", 43)); got != "" {
				t.Fatalf("AuthorizeURLWithPKCE = %q; want empty for scopes %q", got, test.scopes)
			}
		})
	}
}

func TestConfiguredAllowsScopeLessBaseConfiguration(t *testing.T) {
	cfg := testConfig("")
	cfg.Scopes = nil
	if !cfg.Configured() {
		t.Fatal("base OAuth configuration readiness must not require connector-specific scopes")
	}
}

func TestExchangeCodeParsesTokens(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		if r.Form.Get("grant_type") != "authorization_code" {
			t.Errorf("grant_type = %q, want authorization_code", r.Form.Get("grant_type"))
		}
		if r.Form.Get("code") != "the-code" {
			t.Errorf("code = %q, want the-code", r.Form.Get("code"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"at-1","refresh_token":"rt-1","token_type":"Bearer","scope":"` + GmailReadonlyScope + `","expires_in":3600}`))
	}))
	defer srv.Close()

	tok, err := testConfig(srv.URL).ExchangeCode(context.Background(), "the-code")
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}
	if tok.AccessToken != "at-1" || tok.RefreshToken != "rt-1" {
		t.Fatalf("tokens = %+v, want at-1/rt-1", tok)
	}
	if !tok.Valid(time.Now()) {
		t.Fatal("freshly-issued token should be valid")
	}
	if tok.Expiry.Before(time.Now().Add(30 * time.Minute)) {
		t.Fatalf("expiry = %v, want ~1h out", tok.Expiry)
	}
}

func TestExchangeCodeWithPKCEIncludesVerifier(t *testing.T) {
	const verifier = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ012"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		if got := r.Form.Get("code_verifier"); got != verifier {
			t.Errorf("code_verifier = %q, want supplied verifier", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"at-pkce","token_type":"Bearer","expires_in":3600}`))
	}))
	defer srv.Close()

	if _, err := testConfig(srv.URL).ExchangeCodeWithPKCE(context.Background(), "code", verifier); err != nil {
		t.Fatalf("ExchangeCodeWithPKCE: %v", err)
	}
}

func TestExchangeCodeSurfacesGoogleError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"Bad Request"}`))
	}))
	defer srv.Close()

	_, err := testConfig(srv.URL).ExchangeCode(context.Background(), "bad")
	if err == nil {
		t.Fatal("expected an error for invalid_grant")
	}
	if !strings.Contains(err.Error(), "invalid_grant") {
		t.Fatalf("error should carry Google's code, got %v", err)
	}
}

func TestAuthorizeURLWithPKCEUsesS256WithoutExposingVerifier(t *testing.T) {
	c := testConfig("")
	verifier := strings.Repeat("verifier_", 6)[:48]
	raw := c.AuthorizeURLWithPKCE("state-xyz", verifier)
	if raw == "" {
		t.Fatal("AuthorizeURLWithPKCE returned an empty URL")
	}
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("authorize URL did not parse: %v", err)
	}
	digest := sha256.Sum256([]byte(verifier))
	wantChallenge := base64.RawURLEncoding.EncodeToString(digest[:])
	if got := u.Query().Get("code_challenge"); got != wantChallenge {
		t.Fatalf("code_challenge = %q, want S256 digest", got)
	}
	if got := u.Query().Get("code_challenge_method"); got != "S256" {
		t.Fatalf("code_challenge_method = %q, want S256", got)
	}
	if strings.Contains(raw, verifier) {
		t.Fatal("authorization URL exposed the PKCE verifier")
	}
	if got := u.Query().Get("state"); got != "state-xyz" {
		t.Fatalf("state = %q, want original state", got)
	}
}

func TestPKCERejectsInvalidVerifierBeforeBuildingRequest(t *testing.T) {
	c := testConfig("")
	for _, verifier := range []string{"short", strings.Repeat("a", 129), strings.Repeat("a", 42) + "!"} {
		if got := c.AuthorizeURLWithPKCE("state", verifier); got != "" {
			t.Errorf("AuthorizeURLWithPKCE accepted invalid verifier %q", verifier)
		}
		called := false
		c.HTTPClient = &http.Client{Transport: oauthRoundTripFunc(func(*http.Request) (*http.Response, error) {
			called = true
			return nil, errors.New("unexpected token request")
		})}
		if _, err := c.ExchangeCodeWithPKCE(context.Background(), "code", verifier); err == nil {
			t.Errorf("ExchangeCodeWithPKCE accepted invalid verifier %q", verifier)
		}
		if called {
			t.Fatal("invalid PKCE verifier reached the token endpoint")
		}
	}
}

func TestAuthorizeURLFailsClosedForUntrustedEndpointOrRedirect(t *testing.T) {
	tests := []struct {
		name   string
		state  string
		mutate func(*Config)
	}{
		{
			name:  "untrusted authorization endpoint",
			state: "state-xyz",
			mutate: func(c *Config) {
				c.AuthEndpoint = "https://attacker.example/auth"
			},
		},
		{
			name:  "insecure redirect URI",
			state: "state-xyz",
			mutate: func(c *Config) {
				c.RedirectURL = "http://attacker.example/callback"
			},
		},
		{
			name:  "empty state",
			state: " ",
			mutate: func(c *Config) {
				c.RedirectURL = "https://example.test/callback"
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := testConfig("")
			test.mutate(&cfg)
			if got := cfg.AuthorizeURL(test.state); got != "" {
				t.Fatalf("AuthorizeURL = %q, want empty for unsafe input", got)
			}
		})
	}
}

func TestRefreshPreservesMachineReadableProviderAccountStateError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"reauthentication required","error_subtype":"invalid_rapt"}`))
	}))
	defer srv.Close()

	_, err := testConfig(srv.URL).Refresh(context.Background(), "rt-revoked")
	if err == nil {
		t.Fatal("Refresh succeeded for an invalidated refresh grant")
	}
	if !errors.Is(err, ErrReauthorizationRequired) {
		t.Fatalf("Refresh error = %v, want ErrReauthorizationRequired", err)
	}
	var providerErr *OAuthProviderError
	if !errors.As(err, &providerErr) {
		t.Fatalf("Refresh error type = %T, want *OAuthProviderError", err)
	}
	if providerErr.Code != "invalid_grant" || providerErr.Subtype != "invalid_rapt" {
		t.Fatalf("provider error = %#v, want invalid_grant/invalid_rapt", providerErr)
	}
}

func TestOAuthProviderErrorFormattingRedactsProviderControlledFields(t *testing.T) {
	const secret = "test-refresh-token-must-not-appear-in-logs"
	err := &OAuthProviderError{Code: secret, Description: "request rejected: " + secret, Subtype: secret}

	formatted := fmt.Sprintf("%v | %+v | %#v | %s", err, err, err, err)
	if strings.Contains(formatted, secret) {
		t.Fatalf("formatted provider error exposed provider-controlled credential: %q", formatted)
	}
	if !strings.Contains(formatted, "google oauth provider rejected the request") {
		t.Fatalf("formatted provider error lost its safe summary: %q", formatted)
	}
	if err.Code != secret || err.Description != "request rejected: "+secret || err.Subtype != secret {
		t.Fatal("redacting formatted output mutated typed provider error fields")
	}
}

func TestRefreshDoesNotClassifyTemporaryProviderErrorAsReauthorization(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":"temporarily_unavailable","error_description":"try again later"}`))
	}))
	defer srv.Close()

	_, err := testConfig(srv.URL).Refresh(context.Background(), "rt-test")
	if err == nil {
		t.Fatal("Refresh succeeded for a temporary provider failure")
	}
	if errors.Is(err, ErrReauthorizationRequired) {
		t.Fatalf("temporary provider failure incorrectly requires reauthorization: %v", err)
	}
	var providerErr *OAuthProviderError
	if !errors.As(err, &providerErr) || providerErr.Code != "temporarily_unavailable" {
		t.Fatalf("Refresh error = %#v, want typed temporarily_unavailable provider error", err)
	}
}

func TestExchangeCodeRequiresBearerTokenAndFiniteLifetime(t *testing.T) {
	for _, test := range []struct {
		name string
		body string
	}{
		{name: "missing token type", body: `{"access_token":"redacted-test-token","expires_in":3600}`},
		{name: "unsupported token type", body: `{"access_token":"redacted-test-token","token_type":"mac","expires_in":3600}`},
		{name: "missing lifetime", body: `{"access_token":"redacted-test-token","token_type":"Bearer"}`},
		{name: "zero lifetime", body: `{"access_token":"redacted-test-token","token_type":"Bearer","expires_in":0}`},
		{name: "negative lifetime", body: `{"access_token":"redacted-test-token","token_type":"Bearer","expires_in":-1}`},
		{name: "overflowing lifetime", body: `{"access_token":"redacted-test-token","token_type":"Bearer","expires_in":10000000000}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(test.body))
			}))
			defer server.Close()

			if _, err := testConfig(server.URL).ExchangeCode(context.Background(), "test-code"); err == nil {
				t.Fatal("ExchangeCode accepted a token without safe bearer/lifetime metadata")
			}
		})
	}
}

func TestExchangeCodeDoesNotFollowCredentialBearingRedirect(t *testing.T) {
	targetRequests := make(chan struct{}, 1)
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		targetRequests <- struct{}{}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"redacted-test-token","token_type":"Bearer","expires_in":3600}`))
	}))
	defer target.Close()

	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", target.URL)
		w.WriteHeader(http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()

	if _, err := testConfig(redirect.URL).ExchangeCode(context.Background(), "test-code"); err == nil {
		t.Fatal("ExchangeCode accepted a redirect response")
	}
	select {
	case <-targetRequests:
		t.Fatal("token request followed a credential-bearing redirect")
	default:
	}
}

func TestTokenRequestRejectsUntrustedEndpointBeforeSendingCredentials(t *testing.T) {
	called := false
	cfg := testConfig("http://oauth-attacker.invalid/token")
	cfg.HTTPClient = &http.Client{Transport: oauthRoundTripFunc(func(*http.Request) (*http.Response, error) {
		called = true
		return nil, errors.New("unexpected request")
	})}

	if _, err := cfg.Refresh(context.Background(), "test-refresh-token"); err == nil {
		t.Fatal("Refresh accepted an untrusted token endpoint")
	}
	if called {
		t.Fatal("token request reached the transport before endpoint validation")
	}
}

func TestGoogleAPIClientsRejectUntrustedBaseURLsBeforeSendingBearerTokens(t *testing.T) {
	const accessToken = "test-access-token"
	clients := []struct {
		name string
		call func(*http.Client, string) error
	}{
		{
			name: "gmail",
			call: func(client *http.Client, baseURL string) error {
				_, err := (GmailClient{AccessToken: accessToken, BaseURL: baseURL, HTTPClient: client}).GetProfileHistoryID(context.Background())
				return err
			},
		},
		{
			name: "drive",
			call: func(client *http.Client, baseURL string) error {
				_, err := (DriveClient{AccessToken: accessToken, BaseURL: baseURL, HTTPClient: client}).GetStartPageToken(context.Background())
				return err
			},
		},
		{
			name: "calendar",
			call: func(client *http.Client, baseURL string) error {
				_, err := (CalendarClient{AccessToken: accessToken, BaseURL: baseURL, HTTPClient: client}).ListPrimaryEventsPage(context.Background(), "", "", "", 50)
				return err
			},
		},
		{
			name: "people",
			call: func(client *http.Client, baseURL string) error {
				_, err := (PeopleClient{AccessToken: accessToken, BaseURL: baseURL, HTTPClient: client}).ListConnectionsPage(context.Background(), "", "", 50)
				return err
			},
		},
	}
	endpoints := []string{
		"http://oauth-attacker.example/api",
		"https://oauth-attacker.example/api",
	}

	for _, api := range clients {
		for _, endpoint := range endpoints {
			t.Run(api.name+"/"+strings.Split(endpoint, ":")[0], func(t *testing.T) {
				called := false
				httpClient := &http.Client{Transport: oauthRoundTripFunc(func(request *http.Request) (*http.Response, error) {
					called = true
					if request.Header.Get("Authorization") == "Bearer "+accessToken {
						t.Error("bearer token reached an untrusted API endpoint")
					}
					return nil, errors.New("unexpected request to untrusted endpoint")
				})}
				if err := api.call(httpClient, endpoint); err == nil {
					t.Fatal("untrusted API endpoint was accepted")
				}
				if called {
					t.Fatal("HTTP transport was called for an untrusted API endpoint")
				}
			})
		}
	}
}

func TestExchangeCodeRejectsInsecureRedirectBeforeTokenRequest(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	}))
	defer server.Close()

	cfg := testConfig(server.URL)
	cfg.RedirectURL = "http://attacker.example/oauth/callback"
	if _, err := cfg.ExchangeCode(context.Background(), "test-code"); err == nil {
		t.Fatal("ExchangeCode accepted a cleartext non-loopback redirect URI")
	}
	if called {
		t.Fatal("token exchange reached the transport with an insecure redirect URI")
	}
}

// Google typically omits a new refresh token on refresh; the prior one must be
// preserved so the connector keeps working.
func TestRefreshPreservesExistingRefreshToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.Form.Get("grant_type") != "refresh_token" {
			t.Errorf("grant_type = %q, want refresh_token", r.Form.Get("grant_type"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"at-2","token_type":"Bearer","expires_in":3600}`))
	}))
	defer srv.Close()

	tok, err := testConfig(srv.URL).Refresh(context.Background(), "rt-original")
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if tok.AccessToken != "at-2" {
		t.Fatalf("access token = %q, want at-2", tok.AccessToken)
	}
	if tok.RefreshToken != "rt-original" {
		t.Fatalf("refresh token = %q, want the original to be preserved", tok.RefreshToken)
	}
}

func TestTokenValidityWindow(t *testing.T) {
	now := time.Now()
	if (Token{AccessToken: ""}).Valid(now) {
		t.Fatal("empty access token is never valid")
	}
	if (Token{AccessToken: "x", Expiry: now.Add(-time.Minute)}).Valid(now) {
		t.Fatal("expired token must be invalid")
	}
	if !(Token{AccessToken: "x", Expiry: now.Add(time.Hour)}).Valid(now) {
		t.Fatal("token an hour out should be valid")
	}
	if (Token{AccessToken: "x"}).Valid(now) {
		t.Fatal("token with unknown expiry must not be treated as valid indefinitely")
	}
	// Within the refresh skew → treated as needing refresh.
	if (Token{AccessToken: "x", Expiry: now.Add(10 * time.Second)}).Valid(now) {
		t.Fatal("token inside the skew window should be treated as invalid")
	}
}

func TestNewStateIsRandom(t *testing.T) {
	a, err := NewState()
	if err != nil {
		t.Fatalf("NewState: %v", err)
	}
	b, _ := NewState()
	if a == b {
		t.Fatal("two states should differ")
	}
	if len(a) < 20 {
		t.Fatalf("state too short to be a real CSRF token: %q", a)
	}
}

// The connector must request only the scopes it declares. include_granted_scopes
// would fold in every scope the account previously granted the project, making
// the stored grant wider than the documented least-privilege claim.
func TestAuthorizeURLRequestsOnlyDeclaredScopes(t *testing.T) {
	cfg := Config{
		ClientID:    "cid",
		RedirectURL: "http://localhost:8080/cb",
		Scopes:      []string{GmailReadonlyScope},
	}
	raw := cfg.AuthorizeURL("state-123")
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse authorize url: %v", err)
	}
	q := parsed.Query()
	if got := q.Get("scope"); got != GmailReadonlyScope {
		t.Fatalf("scope = %q, want only %q", got, GmailReadonlyScope)
	}
	if q.Has("include_granted_scopes") {
		t.Fatal("include_granted_scopes must not be sent: it widens the grant beyond the declared scope")
	}
	if q.Get("access_type") != "offline" || q.Get("prompt") != "consent" {
		t.Fatalf("refresh token would not be issued: access_type=%q prompt=%q", q.Get("access_type"), q.Get("prompt"))
	}
}

type oauthRoundTripFunc func(*http.Request) (*http.Response, error)

func (f oauthRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}
