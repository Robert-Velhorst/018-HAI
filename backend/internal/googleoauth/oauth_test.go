package googleoauth

import (
	"context"
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
