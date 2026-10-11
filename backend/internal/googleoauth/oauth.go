// Package googleoauth implements the Google OAuth 2.0 authorization-code flow
// used by real connectors (Gmail first). It is deliberately self-contained and
// hand-rolled over net/http, matching the rest of the backend's connector code,
// and its network endpoints are injectable so the whole flow can be unit-tested
// against a mock server without touching Google.
package googleoauth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Endpoints are Google's real OAuth endpoints; tests override them.
const (
	DefaultAuthEndpoint  = "https://accounts.google.com/o/oauth2/v2/auth"
	DefaultTokenEndpoint = "https://oauth2.googleapis.com/token"

	// GmailReadonlyScope grants read-only access to Gmail. Read-only is
	// deliberate: a connector ingests, it does not send or delete mail.
	GmailReadonlyScope = "https://www.googleapis.com/auth/gmail.readonly"
	// DriveReadonlyScope grants read-only access to files the user can access.
	// HAI never uploads, edits, shares, trashes, or deletes Drive content.
	DriveReadonlyScope = "https://www.googleapis.com/auth/drive.readonly"
	// ContactsReadonlyScope grants read-only access to the signed-in user's
	// contacts. HAI imports candidates for governed review and never edits the
	// Google address book.
	ContactsReadonlyScope = "https://www.googleapis.com/auth/contacts.readonly"
	// CalendarReadonlyScope grants read-only access to Calendar events. HAI
	// reads the primary calendar and never creates, edits, or deletes events.
	CalendarReadonlyScope = "https://www.googleapis.com/auth/calendar.readonly"
)

// Config describes an OAuth client. AuthEndpoint/TokenEndpoint default to
// Google's when empty.
type Config struct {
	ClientID      string
	ClientSecret  string
	RedirectURL   string
	Scopes        []string
	AuthEndpoint  string
	TokenEndpoint string
	HTTPClient    *http.Client
}

// Configured reports whether the minimum needed to start a flow is present.
// A connector whose OAuth app is not configured must say so rather than pretend
// to work.
func (c Config) Configured() bool {
	return configuredValue(c.ClientID) &&
		configuredValue(c.ClientSecret) &&
		configuredValue(c.RedirectURL)
}

// configuredValue rejects the sample values commonly copied from .env.example.
// Treating those as credentials lets the UI begin a consent flow that can never
// complete and misrepresents an unconfigured deployment as ready.
func configuredValue(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return false
	}
	for _, marker := range []string{"your-", "your_", "replace", "changeme", "change-me", "<set-", "<your"} {
		if strings.Contains(value, marker) {
			return false
		}
	}
	return true
}

func (c Config) authEndpoint() string {
	if strings.TrimSpace(c.AuthEndpoint) != "" {
		return c.AuthEndpoint
	}
	return DefaultAuthEndpoint
}

func (c Config) tokenEndpoint() string {
	if strings.TrimSpace(c.TokenEndpoint) != "" {
		return c.TokenEndpoint
	}
	return DefaultTokenEndpoint
}

func (c Config) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return &http.Client{Timeout: 20 * time.Second}
}

// Token is an OAuth token set. RefreshToken may be empty on a re-consent that
// Google chose not to re-issue one for; callers must preserve any prior refresh
// token in that case.
type Token struct {
	AccessToken  string
	RefreshToken string
	TokenType    string
	Scope        string
	Expiry       time.Time
}

// Valid reports whether the access token is present and not within the skew of
// expiry, so callers know when to refresh.
func (t Token) Valid(now time.Time) bool {
	if strings.TrimSpace(t.AccessToken) == "" {
		return false
	}
	if t.Expiry.IsZero() {
		return true
	}
	return now.Before(t.Expiry.Add(-30 * time.Second))
}

// NewState returns a cryptographically random state value for CSRF protection
// on the authorization redirect.
func NewState() (string, error) {
	buf := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// AuthorizeURL builds the Google consent URL. access_type=offline and
// prompt=consent request a refresh token, so the connector can keep syncing
// without the user re-authorizing each hour.
//
// include_granted_scopes is deliberately NOT set. With it, Google folds every
// scope the account previously granted this project into the new grant, so a
// live run produced a token carrying userinfo.email, userinfo.profile and
// openid alongside gmail.readonly. The connector only needs gmail.readonly, and
// a stored grant wider than the documented least-privilege claim is exactly the
// kind of gap this codebase refuses to paper over.
func (c Config) AuthorizeURL(state string) string {
	q := url.Values{}
	q.Set("client_id", c.ClientID)
	q.Set("redirect_uri", c.RedirectURL)
	q.Set("response_type", "code")
	q.Set("scope", strings.Join(c.Scopes, " "))
	q.Set("access_type", "offline")
	q.Set("prompt", "consent")
	q.Set("state", state)
	return c.authEndpoint() + "?" + q.Encode()
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	Scope        string `json:"scope"`
	ExpiresIn    int64  `json:"expires_in"`
	Error        string `json:"error"`
	ErrorDesc    string `json:"error_description"`
}

// ExchangeCode trades an authorization code for tokens.
func (c Config) ExchangeCode(ctx context.Context, code string) (*Token, error) {
	form := url.Values{}
	form.Set("code", code)
	form.Set("client_id", c.ClientID)
	form.Set("client_secret", c.ClientSecret)
	form.Set("redirect_uri", c.RedirectURL)
	form.Set("grant_type", "authorization_code")
	return c.postToken(ctx, form)
}

// Refresh obtains a fresh access token from a refresh token. Google usually
// omits a new refresh token here, so the caller keeps the existing one.
func (c Config) Refresh(ctx context.Context, refreshToken string) (*Token, error) {
	form := url.Values{}
	form.Set("refresh_token", refreshToken)
	form.Set("client_id", c.ClientID)
	form.Set("client_secret", c.ClientSecret)
	form.Set("grant_type", "refresh_token")
	tok, err := c.postToken(ctx, form)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(tok.RefreshToken) == "" {
		tok.RefreshToken = refreshToken
	}
	return tok, nil
}

func (c Config) postToken(ctx context.Context, form url.Values) (*Token, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.tokenEndpoint(), strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("token endpoint request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}

	var parsed tokenResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("token endpoint returned unparseable response (HTTP %d)", resp.StatusCode)
	}
	if parsed.Error != "" {
		return nil, fmt.Errorf("google oauth error %q: %s", parsed.Error, parsed.ErrorDesc)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("token endpoint returned HTTP %d", resp.StatusCode)
	}
	if strings.TrimSpace(parsed.AccessToken) == "" {
		return nil, fmt.Errorf("token endpoint returned no access token")
	}

	tok := &Token{
		AccessToken:  parsed.AccessToken,
		RefreshToken: parsed.RefreshToken,
		TokenType:    parsed.TokenType,
		Scope:        parsed.Scope,
	}
	if parsed.ExpiresIn > 0 {
		tok.Expiry = time.Now().Add(time.Duration(parsed.ExpiresIn) * time.Second)
	}
	return tok, nil
}
