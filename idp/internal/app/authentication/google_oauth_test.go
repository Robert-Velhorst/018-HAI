package authentication

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

func newTestGoogleOAuth() *googleOAuth {
	g := newGoogleOAuth("test-jwt-secret")
	g.clientID = "client-1.apps.googleusercontent.com"
	g.clientSecret = "secret"
	g.redirectURL = "https://example.ngrok-free.dev/api/v1/auth/google/callback"
	return g
}

func TestGoogleConfiguredNeedsAllThree(t *testing.T) {
	g := newTestGoogleOAuth()
	if !g.Configured() {
		t.Fatal("fully populated should be configured")
	}
	g.clientSecret = ""
	if g.Configured() {
		t.Fatal("missing secret must not be configured")
	}
}

func TestGoogleUsesCurrentOpenIDConnectUserInfoEndpoint(t *testing.T) {
	g := newGoogleOAuth("test-jwt-secret")
	if g.userInfoURL != "https://openidconnect.googleapis.com/v1/userinfo" {
		t.Fatalf("userinfo endpoint = %q, want current Google OpenID Connect endpoint", g.userInfoURL)
	}
}

func TestGoogleConfiguredRejectsWhitespaceAroundProviderValues(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*googleOAuth)
	}{
		{name: "client ID", mutate: func(g *googleOAuth) { g.clientID = " " + g.clientID }},
		{name: "client secret", mutate: func(g *googleOAuth) { g.clientSecret += " " }},
		{name: "callback URL", mutate: func(g *googleOAuth) { g.redirectURL += " " }},
	} {
		t.Run(test.name, func(t *testing.T) {
			g := newTestGoogleOAuth()
			test.mutate(g)
			if g.Configured() {
				t.Fatal("malformed provider configuration must not be reported as ready")
			}
		})
	}
}

func TestGoogleConfiguredRequiresSafeCallbackURL(t *testing.T) {
	tests := []struct {
		name        string
		redirectURL string
		want        bool
	}{
		{name: "https registered callback", redirectURL: "https://login.example.com/api/v1/auth/google/callback", want: true},
		{name: "local http callback", redirectURL: "http://localhost:8088/api/v1/auth/google/callback", want: true},
		{name: "loopback IPv6 callback", redirectURL: "http://[::1]:8088/api/v1/auth/google/callback", want: true},
		{name: "missing callback", redirectURL: "", want: false},
		{name: "relative callback", redirectURL: "/api/v1/auth/google/callback", want: false},
		{name: "insecure public callback", redirectURL: "http://login.example.com/api/v1/auth/google/callback", want: false},
		{name: "wrong callback route", redirectURL: "https://login.example.com/login", want: false},
		{name: "callback credentials", redirectURL: "https://user:pass@login.example.com/api/v1/auth/google/callback", want: false},
		{name: "callback fragment", redirectURL: "https://login.example.com/api/v1/auth/google/callback#fragment", want: false},
		{name: "invalid port", redirectURL: "https://login.example.com:70000/api/v1/auth/google/callback", want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			g := newTestGoogleOAuth()
			g.redirectURL = test.redirectURL
			if got := g.Configured(); got != test.want {
				t.Fatalf("Configured() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestGoogleAuthCodeURL(t *testing.T) {
	g := newTestGoogleOAuth()
	raw, err := g.AuthCodeURL()
	if err != nil {
		t.Fatalf("AuthCodeURL: %v", err)
	}
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse AuthCodeURL: %v", err)
	}
	q := u.Query()
	if q.Get("client_id") != g.clientID {
		t.Errorf("client_id = %q", q.Get("client_id"))
	}
	if q.Get("scope") != googleLoginScope {
		t.Errorf("scope = %q, want %q", q.Get("scope"), googleLoginScope)
	}
	if q.Get("state") == "" {
		t.Error("state should be present")
	}
	if q.Get("code_challenge_method") != "S256" {
		t.Fatalf("code_challenge_method = %q, want S256", q.Get("code_challenge_method"))
	}
	verifier, err := g.pkceVerifierForState(q.Get("state"))
	if err != nil {
		t.Fatalf("derive verifier from state: %v", err)
	}
	wantChallenge, err := googlePKCES256Challenge(verifier)
	if err != nil {
		t.Fatalf("compute expected challenge: %v", err)
	}
	if q.Get("code_challenge") != wantChallenge {
		t.Fatalf("code_challenge = %q, want S256(%q) = %q", q.Get("code_challenge"), verifier, wantChallenge)
	}
	if q.Has("code_verifier") || strings.Contains(raw, verifier) {
		t.Fatal("PKCE verifier must not be exposed in the authorization URL")
	}
	if !strings.HasPrefix(raw, googleDefaultAuthEndpoint) {
		t.Errorf("should target Google's auth endpoint")
	}
}

func TestGooglePKCEVerifierIsBoundToSignedState(t *testing.T) {
	g := newTestGoogleOAuth()
	stateA, err := g.signState()
	if err != nil {
		t.Fatalf("sign state A: %v", err)
	}
	stateB, err := g.signState()
	if err != nil {
		t.Fatalf("sign state B: %v", err)
	}
	verifierA, err := g.pkceVerifierForState(stateA)
	if err != nil {
		t.Fatalf("derive verifier A: %v", err)
	}
	verifierAAgain, err := g.pkceVerifierForState(stateA)
	if err != nil {
		t.Fatalf("derive verifier A again: %v", err)
	}
	verifierB, err := g.pkceVerifierForState(stateB)
	if err != nil {
		t.Fatalf("derive verifier B: %v", err)
	}
	if verifierA != verifierAAgain {
		t.Fatal("same signed attempt must derive the same verifier for callback")
	}
	if verifierA == verifierB {
		t.Fatal("separate signed attempts must not share a verifier")
	}
	if _, err := g.pkceVerifierForState(stateA + "tampered"); err == nil {
		t.Fatal("a tampered state must not derive a PKCE verifier")
	}
}

func TestGooglePKCES256ChallengeRejectsInvalidVerifiers(t *testing.T) {
	for name, verifier := range map[string]string{
		"empty":     "",
		"too short": strings.Repeat("a", 42),
		"too long":  strings.Repeat("a", 129),
		"bad char":  strings.Repeat("a", 42) + " ",
		"unicode":   strings.Repeat("a", 42) + "é",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := googlePKCES256Challenge(verifier); err == nil {
				t.Fatal("invalid PKCE verifier was accepted")
			}
		})
	}
}

func TestGoogleStateRoundTrip(t *testing.T) {
	g := newTestGoogleOAuth()
	state, err := g.signState()
	if err != nil {
		t.Fatalf("signState: %v", err)
	}
	if err := g.verifyState(state); err != nil {
		t.Fatalf("verifyState should accept its own state: %v", err)
	}
	if err := g.verifyState(state + "x"); err == nil {
		t.Fatal("a tampered state must be rejected")
	}
	other := newGoogleOAuth("different-secret")
	if err := other.verifyState(state); err == nil {
		t.Fatal("state must not verify under a different signing secret")
	}
}

func TestGoogleStateIsExpiredAtItsExpirationSecond(t *testing.T) {
	g := newTestGoogleOAuth()
	state, err := g.signState()
	if err != nil {
		t.Fatalf("signState: %v", err)
	}
	parts := strings.SplitN(state, ".", 2)
	if len(parts) != 2 {
		t.Fatal("generated state has invalid format")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		t.Fatalf("decode state payload: %v", err)
	}
	fields := strings.SplitN(string(payload), "|", 2)
	if len(fields) != 2 {
		t.Fatal("generated state has invalid payload")
	}
	expiresAt := time.Now().Unix()
	encodedPayload := base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(expiresAt, 10) + "|" + fields[1]))
	mac := hmac.New(sha256.New, g.stateSecret)
	_, _ = mac.Write([]byte(encodedPayload))
	expiredState := encodedPayload + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	if err := g.verifyState(expiredState); err == nil {
		t.Fatal("state must be rejected when current Unix time equals its expiration")
	}
}

func TestGoogleExchangeReturnsEmail(t *testing.T) {
	g := newTestGoogleOAuth()
	authorizeURL, err := g.AuthCodeURL()
	if err != nil {
		t.Fatalf("AuthCodeURL: %v", err)
	}
	authorizeRequest, err := url.Parse(authorizeURL)
	if err != nil {
		t.Fatalf("parse AuthCodeURL: %v", err)
	}
	state := authorizeRequest.Query().Get("state")
	if state == "" {
		t.Fatal("authorization URL omitted state")
	}
	wantVerifier, err := g.pkceVerifierForState(state)
	if err != nil {
		t.Fatalf("derive PKCE verifier: %v", err)
	}
	wantChallenge, err := googlePKCES256Challenge(wantVerifier)
	if err != nil {
		t.Fatalf("derive PKCE challenge: %v", err)
	}
	if got := authorizeRequest.Query().Get("code_challenge"); got != wantChallenge {
		t.Fatalf("authorization code_challenge = %q, want %q", got, wantChallenge)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/token":
			_ = r.ParseForm()
			if r.Form.Get("grant_type") != "authorization_code" {
				t.Errorf("grant_type = %q", r.Form.Get("grant_type"))
			}
			if got := r.Form.Get("code_verifier"); got != wantVerifier {
				t.Errorf("code_verifier = %q, want the verifier bound to the signed state", got)
			}
			_, _ = w.Write([]byte(`{"access_token":"at-1","token_type":"Bearer"}`))
		case "/userinfo":
			if r.Header.Get("Authorization") != "Bearer at-1" {
				t.Errorf("missing bearer token on userinfo")
			}
			_, _ = w.Write([]byte(`{"email":"Person@Example.com","email_verified":true}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	g.tokenEndpoint = srv.URL + "/token"
	g.userInfoURL = srv.URL + "/userinfo"

	email, err := g.Exchange(context.Background(), "the-code", state)
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	if email != "person@example.com" {
		t.Fatalf("email = %q, want normalized person@example.com", email)
	}
}

func TestGoogleExchangeRejectsBadState(t *testing.T) {
	g := newTestGoogleOAuth()
	tokenEndpointCalled := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokenEndpointCalled = true
		http.Error(w, "unexpected token request", http.StatusInternalServerError)
	}))
	defer srv.Close()
	g.tokenEndpoint = srv.URL
	if _, err := g.Exchange(context.Background(), "code", "forged.state"); err == nil {
		t.Fatal("Exchange must reject an unsigned/forged state before calling Google")
	}
	if tokenEndpointCalled {
		t.Fatal("a forged state must be rejected before token exchange")
	}
}

func TestGoogleExchangeSurfacesError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"bad"}`))
	}))
	defer srv.Close()

	g := newTestGoogleOAuth()
	g.tokenEndpoint = srv.URL
	state, _ := g.signState()
	if _, err := g.Exchange(context.Background(), "code", state); err == nil || !strings.Contains(err.Error(), "invalid_grant") {
		t.Fatalf("expected invalid_grant error, got %v", err)
	}
}

func TestGoogleExchangeRejectsUnverifiedEmail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/token":
			_, _ = w.Write([]byte(`{"access_token":"at-1","token_type":"Bearer"}`))
		case "/userinfo":
			_, _ = w.Write([]byte(`{"email":"person@example.com","email_verified":false}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	g := newTestGoogleOAuth()
	g.tokenEndpoint = srv.URL + "/token"
	g.userInfoURL = srv.URL + "/userinfo"
	state, _ := g.signState()

	if _, err := g.Exchange(context.Background(), "code", state); err == nil || !strings.Contains(err.Error(), "not verified") {
		t.Fatalf("expected unverified email to be rejected, got %v", err)
	}
}

func TestGoogleExchangeDoesNotFollowTokenEndpointRedirects(t *testing.T) {
	redirectFollowed := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			http.Redirect(w, r, "/unexpected", http.StatusFound)
		case "/unexpected":
			redirectFollowed = true
			_, _ = w.Write([]byte(`{"access_token":"redirected-token"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	g := newTestGoogleOAuth()
	g.tokenEndpoint = srv.URL + "/token"
	state, err := g.signState()
	if err != nil {
		t.Fatalf("signState: %v", err)
	}
	if _, err := g.Exchange(context.Background(), "code", state); err == nil {
		t.Fatal("redirect response must not be accepted as a token exchange")
	}
	if redirectFollowed {
		t.Fatal("OAuth client followed a token endpoint redirect")
	}
}
