package authentication

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// googleOAuth implements "Sign in with Google" for the IDP: the OAuth
// authorization-code flow scoped to the user's identity (openid email profile),
// used to establish a HAI session — distinct from the backend's Gmail connector,
// which reads mail. Endpoints are injectable so the flow is unit-testable.
type googleOAuth struct {
	clientID      string
	clientSecret  string
	redirectURL   string
	stateSecret   []byte
	authEndpoint  string
	tokenEndpoint string
	userInfoURL   string
	httpClient    *http.Client
}

const (
	googleDefaultAuthEndpoint  = "https://accounts.google.com/o/oauth2/v2/auth"
	googleDefaultTokenEndpoint = "https://oauth2.googleapis.com/token"
	googleDefaultUserInfoURL   = "https://openidconnect.googleapis.com/v1/userinfo"
	googleLoginScope           = "openid email profile"
)

// newGoogleOAuth builds the flow from the environment. jwtSecret signs the CSRF
// state so it needs no server-side storage.
func newGoogleOAuth(jwtSecret string) *googleOAuth {
	sum := sha256.Sum256([]byte("google-login-state|" + jwtSecret))
	return &googleOAuth{
		clientID:      strings.TrimSpace(os.Getenv("GOOGLE_OAUTH_CLIENT_ID")),
		clientSecret:  strings.TrimSpace(os.Getenv("GOOGLE_OAUTH_CLIENT_SECRET")),
		redirectURL:   strings.TrimSpace(os.Getenv("GOOGLE_LOGIN_REDIRECT_URL")),
		stateSecret:   sum[:],
		authEndpoint:  googleDefaultAuthEndpoint,
		tokenEndpoint: googleDefaultTokenEndpoint,
		userInfoURL:   googleDefaultUserInfoURL,
		httpClient: &http.Client{
			Timeout: 20 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

// Configured reports whether Google login can run.
func (g *googleOAuth) Configured() bool {
	return g.clientID != "" && g.clientID == strings.TrimSpace(g.clientID) &&
		g.clientSecret != "" && g.clientSecret == strings.TrimSpace(g.clientSecret) &&
		validGoogleRedirectURL(g.redirectURL)
}

func validGoogleRedirectURL(raw string) bool {
	if raw == "" || raw != strings.TrimSpace(raw) {
		return false
	}
	redirect, err := url.Parse(raw)
	if err != nil || redirect == nil || !redirect.IsAbs() || redirect.Opaque != "" || redirect.User != nil ||
		redirect.Hostname() == "" || redirect.Fragment != "" || !strings.HasSuffix(strings.TrimRight(redirect.Path, "/"), "/auth/google/callback") {
		return false
	}
	if port := redirect.Port(); port != "" {
		parsedPort, err := strconv.Atoi(port)
		if err != nil || parsedPort < 1 || parsedPort > 65535 {
			return false
		}
	}

	switch strings.ToLower(redirect.Scheme) {
	case "https":
		return true
	case "http":
		host := strings.TrimSuffix(strings.ToLower(redirect.Hostname()), ".")
		if host == "localhost" {
			return true
		}
		ip := net.ParseIP(host)
		return ip != nil && ip.IsLoopback()
	default:
		return false
	}
}

func (g *googleOAuth) signState() (string, error) {
	buf := make([]byte, 24)
	if _, err := io.ReadFull(rand.Reader, buf); err != nil {
		return "", err
	}
	nonce := base64.RawURLEncoding.EncodeToString(buf)
	payload := fmt.Sprintf("%d|%s", time.Now().Add(10*time.Minute).Unix(), nonce)
	enc := base64.RawURLEncoding.EncodeToString([]byte(payload))
	mac := hmac.New(sha256.New, g.stateSecret)
	mac.Write([]byte(enc))
	return enc + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func (g *googleOAuth) verifyState(state string) error {
	parts := strings.SplitN(strings.TrimSpace(state), ".", 2)
	if len(parts) != 2 {
		return fmt.Errorf("malformed state")
	}
	mac := hmac.New(sha256.New, g.stateSecret)
	mac.Write([]byte(parts[0]))
	if !hmac.Equal([]byte(base64.RawURLEncoding.EncodeToString(mac.Sum(nil))), []byte(parts[1])) {
		return fmt.Errorf("state signature mismatch")
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return fmt.Errorf("state decode: %w", err)
	}
	fields := strings.SplitN(string(raw), "|", 2)
	if len(fields) != 2 || fields[1] == "" {
		return fmt.Errorf("malformed state")
	}
	exp, err := strconv.ParseInt(fields[0], 10, 64)
	if err != nil || time.Now().Unix() >= exp {
		return fmt.Errorf("state expired")
	}
	return nil
}

// pkceVerifierForState derives an opaque verifier from a valid signed state.
// The verifier is never sent through the browser; only its S256 challenge is
// included in the authorization request. Domain separation keeps this use of
// the state key independent from state signing.
func (g *googleOAuth) pkceVerifierForState(state string) (string, error) {
	if err := g.verifyState(state); err != nil {
		return "", fmt.Errorf("verify state for PKCE: %w", err)
	}
	mac := hmac.New(sha256.New, g.stateSecret)
	mac.Write([]byte("google-login-pkce-v1|"))
	mac.Write([]byte(state))
	verifier := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if _, err := googlePKCES256Challenge(verifier); err != nil {
		return "", fmt.Errorf("derive PKCE verifier: %w", err)
	}
	return verifier, nil
}

func googlePKCES256Challenge(verifier string) (string, error) {
	if len(verifier) < 43 || len(verifier) > 128 {
		return "", fmt.Errorf("PKCE verifier must be 43 to 128 characters")
	}
	for i := 0; i < len(verifier); i++ {
		c := verifier[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '.' || c == '_' || c == '~') {
			return "", fmt.Errorf("PKCE verifier contains an invalid character")
		}
	}
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:]), nil
}

// AuthCodeURL returns the Google consent URL, with a fresh signed state.
func (g *googleOAuth) AuthCodeURL() (string, error) {
	state, err := g.signState()
	if err != nil {
		return "", err
	}
	verifier, err := g.pkceVerifierForState(state)
	if err != nil {
		return "", err
	}
	challenge, err := googlePKCES256Challenge(verifier)
	if err != nil {
		return "", err
	}
	q := url.Values{}
	q.Set("client_id", g.clientID)
	q.Set("redirect_uri", g.redirectURL)
	q.Set("response_type", "code")
	q.Set("scope", googleLoginScope)
	q.Set("state", state)
	q.Set("code_challenge", challenge)
	q.Set("code_challenge_method", "S256")
	return g.authEndpoint + "?" + q.Encode(), nil
}

// Exchange verifies the state, trades the code for an access token, and returns
// the authenticated user's verified email address.
func (g *googleOAuth) Exchange(ctx context.Context, code, state string) (string, error) {
	verifier, err := g.pkceVerifierForState(state)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(code) == "" {
		return "", fmt.Errorf("missing authorization code")
	}

	form := url.Values{}
	form.Set("code", code)
	form.Set("client_id", g.clientID)
	form.Set("client_secret", g.clientSecret)
	form.Set("redirect_uri", g.redirectURL)
	form.Set("grant_type", "authorization_code")
	form.Set("code_verifier", verifier)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.tokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := g.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("token exchange failed: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("read token response: %w", err)
	}

	var tok struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
		ErrorDesc   string `json:"error_description"`
	}
	if err := json.Unmarshal(body, &tok); err != nil {
		return "", fmt.Errorf("token response unparseable (HTTP %d)", resp.StatusCode)
	}
	if tok.Error != "" {
		return "", fmt.Errorf("google oauth error %q: %s", tok.Error, tok.ErrorDesc)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("token exchange returned HTTP %d", resp.StatusCode)
	}
	if tok.AccessToken == "" {
		return "", fmt.Errorf("no access token returned")
	}

	return g.fetchEmail(ctx, tok.AccessToken)
}

func (g *googleOAuth) fetchEmail(ctx context.Context, accessToken string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, g.userInfoURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err := g.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("userinfo request failed: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("read userinfo response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("userinfo returned HTTP %d", resp.StatusCode)
	}

	var info struct {
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
	}
	if err := json.Unmarshal(body, &info); err != nil {
		return "", fmt.Errorf("userinfo unparseable: %w", err)
	}
	email := strings.ToLower(strings.TrimSpace(info.Email))
	if email == "" {
		return "", fmt.Errorf("google did not return an email address")
	}
	if !info.EmailVerified {
		return "", fmt.Errorf("google email address is not verified")
	}
	return email, nil
}
