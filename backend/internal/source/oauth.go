package source

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"automation-hub-backend/internal/config"
	"automation-hub-backend/internal/googleoauth"
	"automation-hub-backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	googleProvider       = "google"
	gmailConnectorKey    = "gmail"
	driveConnectorKey    = "google-drive"
	contactsConnectorKey = "google-contacts"
	calendarConnectorKey = "google-calendar"
	gmailFetchLimit      = 25
	driveFetchLimit      = 100
)

// googleOAuthConfig builds the base OAuth client from the environment. Callers
// must set the connector-specific least-privilege scope before authorization.
func googleOAuthConfig() googleoauth.Config {
	return googleoauth.Config{
		ClientID:     config.AppConfig.GoogleOAuthClientID,
		ClientSecret: config.AppConfig.GoogleOAuthClientSecret,
		RedirectURL:  config.AppConfig.GoogleOAuthRedirectURL,
	}
}

func googleOAuthConfigForConnector(connectorKey string) (googleoauth.Config, error) {
	cfg := googleOAuthConfig()
	switch strings.TrimSpace(connectorKey) {
	case gmailConnectorKey:
		cfg.Scopes = []string{googleoauth.GmailReadonlyScope}
	case driveConnectorKey:
		cfg.Scopes = []string{googleoauth.DriveReadonlyScope}
	case contactsConnectorKey:
		cfg.Scopes = []string{googleoauth.ContactsReadonlyScope}
	case calendarConnectorKey:
		cfg.Scopes = []string{googleoauth.CalendarReadonlyScope}
	default:
		return googleoauth.Config{}, fmt.Errorf("connector %q does not use Google OAuth", connectorKey)
	}
	return cfg, nil
}

func googleOAuthReady() bool {
	return googleOAuthConfig().Configured() &&
		strings.TrimSpace(config.AppConfig.OAuthTokenEncryptionKey) != "" &&
		strings.TrimSpace(config.AppConfig.OAuthStateSigningKey) != ""
}

// tokenCodec uses a dedicated secret. OAuth refresh tokens are account
// credentials and must not silently fall back to an unrelated application key.
func tokenCodec() (*googleoauth.Codec, error) {
	return googleoauth.NewCodec(config.AppConfig.OAuthTokenEncryptionKey)
}

// googleOAuthAtomicTokenRepository is required for OAuth writes. A separate
// source check followed by SaveOAuthToken can persist credentials after the
// source owner, connector, or revocation state has changed.
type googleOAuthAtomicTokenRepository interface {
	SaveGoogleOAuthTokenForSource(ctx context.Context, token *models.SourceOAuthToken, ownerIdentity, connectorKey string) (reconnectCleared bool, err error)
}

type googleOAuthAtomicRefreshTokenRepository interface {
	SaveGoogleOAuthRefreshTokenForSource(ctx context.Context, token *models.SourceOAuthToken, ownerIdentity, connectorKey string, expected *models.SourceOAuthToken) error
}

type googleOAuthRefreshFailureRepository interface {
	SetGoogleOAuthReconnectRequiredForToken(ctx context.Context, expected *models.SourceOAuthToken) (changed bool, err error)
}

var (
	errGoogleOAuthSourceBindingChanged = errors.New("Google OAuth source binding changed; restart the connection")
	errGoogleOAuthSourceInactive       = errors.New("Google OAuth source is paused or disabled; enable it before connecting")
	errGoogleOAuthReconnectRequired    = errors.New("Google OAuth source requires owner reconnection")
	errGoogleOAuthTokenChanged         = errors.New("Google OAuth grant changed during refresh; retry with the current connection")
)

// SaveGoogleOAuthTokenForSource binds token persistence and reconnect-state
// recovery to the exact source snapshot, under one PostgreSQL transaction.
func (r *GormRepository) SaveGoogleOAuthTokenForSource(ctx context.Context, token *models.SourceOAuthToken, ownerIdentity, connectorKey string) (bool, error) {
	if ctx == nil {
		return false, fmt.Errorf("Google OAuth persistence context is required")
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if r == nil || r.DB == nil || r.DB.Dialector == nil || r.DB.Dialector.Name() != "postgres" {
		return false, fmt.Errorf("atomic Google OAuth persistence requires PostgreSQL")
	}
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	connectorKey = strings.TrimSpace(connectorKey)
	if token == nil || token.SourceID == uuid.Nil || token.Provider != googleProvider ||
		len(token.AccessToken) == 0 || len(token.RefreshToken) == 0 ||
		ownerIdentity == "" || !isGoogleOAuthConnector(connectorKey) {
		return false, fmt.Errorf("Google OAuth token and source binding are incomplete")
	}

	var reconnectCleared bool
	err := r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		var source models.ConnectedSource
		// Token persistence and revocation take the source row first, then the
		// token row. This serializes callbacks with revocation without locking
		// the token table for every OAuth connection.
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND owner_identity = ? AND connector_key = ?",
				token.SourceID, ownerIdentity, connectorKey).
			First(&source).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errGoogleOAuthSourceBindingChanged
		}
		if err != nil {
			return fmt.Errorf("recheck Google OAuth source binding: %w", err)
		}
		if source.RevokedAt != nil || strings.EqualFold(strings.TrimSpace(source.Status), "revoked") {
			return ErrSourceRevoked
		}
		if !source.Enabled || strings.EqualFold(strings.TrimSpace(source.Status), "paused") {
			return errGoogleOAuthSourceInactive
		}
		if err := ctx.Err(); err != nil {
			return err
		}

		var existing models.SourceOAuthToken
		err = tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("source_id = ?", token.SourceID).First(&existing).Error
		switch {
		case err == nil:
			token.ID = existing.ID
			token.CreatedAt = existing.CreatedAt
			if err := tx.Select("*").Save(token).Error; err != nil {
				return fmt.Errorf("save Google OAuth token: %w", err)
			}
		case errors.Is(err, gorm.ErrRecordNotFound):
			if token.ID == uuid.Nil {
				token.ID = uuid.New()
			}
			if err := tx.Create(token).Error; err != nil {
				return fmt.Errorf("create Google OAuth token: %w", err)
			}
		default:
			return fmt.Errorf("read existing Google OAuth token: %w", err)
		}

		if strings.EqualFold(strings.TrimSpace(source.Status), "reconnect_required") {
			result := tx.Model(&models.ConnectedSource{}).
				Where("id = ? AND owner_identity = ? AND connector_key = ? AND enabled = ? AND revoked_at IS NULL AND lower(status) = ?",
					token.SourceID, ownerIdentity, connectorKey, true, "reconnect_required").
				Updates(map[string]any{"status": "active", "updated_at": time.Now().UTC()})
			if result.Error != nil {
				return fmt.Errorf("clear Google OAuth reconnect status: %w", result.Error)
			}
			if result.RowsAffected != 1 {
				return errGoogleOAuthSourceBindingChanged
			}
			reconnectCleared = true
		}
		return ctx.Err()
	})
	if err != nil {
		return false, err
	}
	return reconnectCleared, nil
}

// SaveGoogleOAuthRefreshTokenForSource updates a token only if the grant that
// was refreshed is still current. A same-owner reconnect may replace the grant
// while a provider refresh is in flight; the old response must not overwrite it.
func (r *GormRepository) SaveGoogleOAuthRefreshTokenForSource(
	ctx context.Context,
	token *models.SourceOAuthToken,
	ownerIdentity, connectorKey string,
	expected *models.SourceOAuthToken,
) error {
	if ctx == nil {
		return fmt.Errorf("Google OAuth refresh persistence context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if r == nil || r.DB == nil || r.DB.Dialector == nil || r.DB.Dialector.Name() != "postgres" {
		return fmt.Errorf("atomic Google OAuth refresh persistence requires PostgreSQL")
	}
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	connectorKey = strings.TrimSpace(connectorKey)
	if token == nil || expected == nil || token.SourceID == uuid.Nil || expected.SourceID != token.SourceID ||
		token.Provider != googleProvider || expected.Provider != googleProvider ||
		len(token.AccessToken) == 0 || len(token.RefreshToken) == 0 ||
		expected.ID == uuid.Nil || len(expected.AccessToken) == 0 || len(expected.RefreshToken) == 0 ||
		expected.UpdatedAt.IsZero() || ownerIdentity == "" || !isGoogleOAuthConnector(connectorKey) {
		return fmt.Errorf("Google OAuth refresh token and expected grant binding are incomplete")
	}

	return r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		var source models.ConnectedSource
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND owner_identity = ? AND connector_key = ?", token.SourceID, ownerIdentity, connectorKey).
			First(&source).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errGoogleOAuthSourceBindingChanged
		}
		if err != nil {
			return fmt.Errorf("recheck Google OAuth refresh source binding: %w", err)
		}
		if err := googleOAuthSourceReadyForTokenUse(&source); err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}

		var current models.SourceOAuthToken
		err = tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("source_id = ? AND provider = ?", token.SourceID, googleProvider).
			First(&current).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errGoogleOAuthTokenChanged
		}
		if err != nil {
			return fmt.Errorf("recheck Google OAuth grant before refresh save: %w", err)
		}
		if current.ID != expected.ID || !bytes.Equal(current.AccessToken, expected.AccessToken) ||
			!bytes.Equal(current.RefreshToken, expected.RefreshToken) || current.Scope != expected.Scope ||
			!current.Expiry.Equal(expected.Expiry) || !current.UpdatedAt.Equal(expected.UpdatedAt) {
			return errGoogleOAuthTokenChanged
		}
		if err := ctx.Err(); err != nil {
			return err
		}

		token.ID = current.ID
		token.CreatedAt = current.CreatedAt
		if err := tx.Select("*").Save(token).Error; err != nil {
			return fmt.Errorf("save refreshed Google OAuth grant: %w", err)
		}
		return ctx.Err()
	})
}

// --- signed OAuth state -----------------------------------------------------
// Successful callback state binds the authorization flow to a source owner and
// connector as well as an expiry and nonce. The older source-only format remains
// readable for the callback's denial path, but is never accepted for token writes.

func stateSecret() ([]byte, error) {
	s := strings.TrimSpace(config.AppConfig.OAuthStateSigningKey)
	if s == "" {
		return nil, fmt.Errorf("oauth state signing key is not configured")
	}
	sum := sha256.Sum256([]byte("oauth-state|" + s))
	return sum[:], nil
}

type googleOAuthStateClaims struct {
	Version      int    `json:"v"`
	SourceID     string `json:"sourceId"`
	OwnerBinding string `json:"ownerBinding"`
	ConnectorKey string `json:"connectorKey"`
	ExpiresAt    int64  `json:"expiresAt"`
	Nonce        string `json:"nonce"`
}

func signState(sourceID uuid.UUID) (string, error) {
	nonce, err := googleoauth.NewState()
	if err != nil {
		return "", err
	}
	payload := fmt.Sprintf("%s|%d|%s", sourceID.String(), time.Now().Add(10*time.Minute).Unix(), nonce)
	return signOAuthStatePayload([]byte(payload))
}

func signBoundGoogleOAuthState(sourceID uuid.UUID, ownerIdentity, connectorKey string) (string, error) {
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	connectorKey = strings.TrimSpace(connectorKey)
	if sourceID == uuid.Nil || ownerIdentity == "" || len(ownerIdentity) > 255 || !isGoogleOAuthConnector(connectorKey) {
		return "", fmt.Errorf("Google OAuth source binding is incomplete")
	}
	ownerBinding, err := googleOAuthOwnerBinding(ownerIdentity)
	if err != nil {
		return "", err
	}
	nonce, err := googleoauth.NewState()
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(googleOAuthStateClaims{
		Version: 1, SourceID: sourceID.String(), OwnerBinding: ownerBinding,
		ConnectorKey: connectorKey, ExpiresAt: time.Now().Add(10 * time.Minute).Unix(), Nonce: nonce,
	})
	if err != nil {
		return "", fmt.Errorf("encode Google OAuth state: %w", err)
	}
	return signOAuthStatePayload(payload)
}

func signOAuthStatePayload(payload []byte) (string, error) {
	enc := base64.RawURLEncoding.EncodeToString(payload)
	secret, err := stateSecret()
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(enc))
	return enc + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func googleOAuthCodeVerifier(state string) (string, error) {
	secret, err := stateSecret()
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte("google-oauth-pkce|" + strings.TrimSpace(state)))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func googleOAuthOwnerBinding(ownerIdentity string) (string, error) {
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	if ownerIdentity == "" || len(ownerIdentity) > 255 {
		return "", fmt.Errorf("Google OAuth source owner is required")
	}
	secret, err := stateSecret()
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte("google-oauth-owner|" + ownerIdentity))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func verifyState(state string) (uuid.UUID, error) {
	payload, err := verifiedOAuthStatePayload(state)
	if err != nil {
		return uuid.Nil, err
	}
	var claims googleOAuthStateClaims
	if err := json.Unmarshal(payload, &claims); err == nil && claims.Version == 1 {
		if _, err := validateBoundGoogleOAuthState(claims); err != nil {
			return uuid.Nil, err
		}
		id, err := uuid.Parse(claims.SourceID)
		if err != nil {
			return uuid.Nil, fmt.Errorf("oauth state source id invalid")
		}
		return id, nil
	}
	return verifyLegacyGoogleOAuthState(payload)
}

func verifyBoundGoogleOAuthState(state string) (googleOAuthStateClaims, error) {
	payload, err := verifiedOAuthStatePayload(state)
	if err != nil {
		return googleOAuthStateClaims{}, err
	}
	var claims googleOAuthStateClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return googleOAuthStateClaims{}, fmt.Errorf("oauth state payload shape")
	}
	if _, err := validateBoundGoogleOAuthState(claims); err != nil {
		return googleOAuthStateClaims{}, err
	}
	return claims, nil
}

func validateBoundGoogleOAuthState(claims googleOAuthStateClaims) (uuid.UUID, error) {
	ownerBinding, bindingErr := base64.RawURLEncoding.DecodeString(claims.OwnerBinding)
	if claims.Version != 1 || bindingErr != nil || len(ownerBinding) != sha256.Size ||
		!isGoogleOAuthConnector(claims.ConnectorKey) || strings.TrimSpace(claims.ConnectorKey) != claims.ConnectorKey {
		return uuid.Nil, fmt.Errorf("oauth state source binding is invalid")
	}
	if claims.ExpiresAt <= time.Now().Unix() {
		return uuid.Nil, fmt.Errorf("oauth state expired; restart the connection")
	}
	nonce, err := base64.RawURLEncoding.DecodeString(claims.Nonce)
	if err != nil || len(nonce) != 32 {
		return uuid.Nil, fmt.Errorf("oauth state nonce is invalid")
	}
	id, err := uuid.Parse(claims.SourceID)
	if err != nil || id == uuid.Nil {
		return uuid.Nil, fmt.Errorf("oauth state source id invalid")
	}
	return id, nil
}

func verifiedOAuthStatePayload(state string) ([]byte, error) {
	state = strings.TrimSpace(state)
	if len(state) == 0 || len(state) > 4096 {
		return nil, fmt.Errorf("malformed oauth state")
	}
	parts := strings.SplitN(strings.TrimSpace(state), ".", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("malformed oauth state")
	}
	secret, err := stateSecret()
	if err != nil {
		return nil, err
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(parts[0]))
	expected := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expected), []byte(parts[1])) {
		return nil, fmt.Errorf("oauth state signature mismatch")
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, fmt.Errorf("oauth state decode: %w", err)
	}
	return raw, nil
}

func verifyLegacyGoogleOAuthState(raw []byte) (uuid.UUID, error) {
	fields := strings.Split(string(raw), "|")
	if len(fields) != 3 {
		return uuid.Nil, fmt.Errorf("oauth state payload shape")
	}
	exp, err := strconv.ParseInt(fields[1], 10, 64)
	if err != nil || time.Now().Unix() >= exp {
		return uuid.Nil, fmt.Errorf("oauth state expired; restart the connection")
	}
	id, err := uuid.Parse(fields[0])
	if err != nil || id == uuid.Nil {
		return uuid.Nil, fmt.Errorf("oauth state source id invalid")
	}
	return id, nil
}

// StartGoogleOAuth returns the least-privilege Google consent URL for the
// selected Gmail, Drive, Contacts, or Calendar source.
func (s *service) StartGoogleOAuth(sourceID uuid.UUID) (string, error) {
	if !googleOAuthReady() {
		return "", fmt.Errorf("google oauth is not configured; set the GOOGLE_OAUTH_* values and dedicated HAI OAuth encryption/signing keys")
	}
	source, err := s.repo.FindSource(sourceID)
	if err != nil {
		return "", err
	}
	if err := googleOAuthSourceAllowed(source); err != nil {
		return "", err
	}
	if strings.TrimSpace(source.OwnerIdentity) == "" {
		return "", fmt.Errorf("Google OAuth requires a source assigned to an authenticated owner")
	}
	cfg, err := googleOAuthConfigForConnector(source.ConnectorKey)
	if err != nil {
		return "", err
	}
	state, err := signBoundGoogleOAuthState(sourceID, source.OwnerIdentity, source.ConnectorKey)
	if err != nil {
		return "", err
	}
	verifier, err := googleOAuthCodeVerifier(state)
	if err != nil {
		return "", err
	}
	authorizeURL := cfg.AuthorizeURLWithPKCE(state, verifier)
	if authorizeURL == "" {
		return "", fmt.Errorf("google oauth authorization request could not be constructed")
	}
	return authorizeURL, nil
}

// CompleteGoogleOAuth handles the callback: verify state, exchange the code, and
// store the encrypted tokens against the source.
func (s *service) CompleteGoogleOAuth(ctx context.Context, code, state string) (uuid.UUID, error) {
	if ctx == nil {
		return uuid.Nil, fmt.Errorf("Google OAuth callback context is required")
	}
	if !googleOAuthReady() {
		return uuid.Nil, fmt.Errorf("google oauth is not configured")
	}
	if strings.TrimSpace(code) == "" {
		return uuid.Nil, fmt.Errorf("missing authorization code")
	}
	claims, err := verifyBoundGoogleOAuthState(state)
	if err != nil {
		return uuid.Nil, err
	}
	if err := ctx.Err(); err != nil {
		return uuid.Nil, err
	}
	sourceID, _ := uuid.Parse(claims.SourceID)
	source, err := s.repo.FindSource(sourceID)
	if err != nil {
		return uuid.Nil, err
	}
	if err := googleOAuthSourceMatchesBinding(source, sourceID, claims.OwnerBinding, claims.ConnectorKey); err != nil {
		return uuid.Nil, err
	}
	// Revocation is destructive and deletes the stored refresh token. A user
	// can revoke a source while their browser is at Google consent, so verify
	// the source state again before exchanging and persisting a new grant.
	if err := googleOAuthSourceAllowed(source); err != nil {
		return uuid.Nil, err
	}
	cfg, err := googleOAuthConfigForConnector(claims.ConnectorKey)
	if err != nil {
		return uuid.Nil, err
	}
	verifier, err := googleOAuthCodeVerifier(state)
	if err != nil {
		return uuid.Nil, err
	}
	token, err := cfg.ExchangeCodeWithPKCE(ctx, code, verifier)
	if err != nil {
		return uuid.Nil, err
	}
	if err := ctx.Err(); err != nil {
		return uuid.Nil, err
	}
	if err := s.storeToken(ctx, sourceID, claims.OwnerBinding, claims.ConnectorKey, token); err != nil {
		return uuid.Nil, err
	}
	s.audit(sourceID, "source.oauth_connected", "google account connected with least-privilege "+source.ConnectorKey+" read scope")
	return sourceID, nil
}

func googleOAuthSourceAllowed(source *models.ConnectedSource) error {
	if source == nil {
		return fmt.Errorf("connected source is required")
	}
	if source.RevokedAt != nil || strings.EqualFold(strings.TrimSpace(source.Status), "revoked") {
		return ErrSourceRevoked
	}
	if !source.Enabled || strings.EqualFold(strings.TrimSpace(source.Status), "paused") {
		return errGoogleOAuthSourceInactive
	}
	return nil
}

func googleOAuthSourceReadyForTokenUse(source *models.ConnectedSource) error {
	if err := googleOAuthSourceAllowed(source); err != nil {
		return err
	}
	if strings.EqualFold(strings.TrimSpace(source.Status), "reconnect_required") {
		return errGoogleOAuthReconnectRequired
	}
	return nil
}

func googleOAuthSourceMatchesBinding(source *models.ConnectedSource, sourceID uuid.UUID, ownerBinding, connectorKey string) error {
	if source == nil || source.ID != sourceID {
		return fmt.Errorf("Google OAuth source could not be verified")
	}
	currentOwnerBinding, err := googleOAuthOwnerBinding(source.OwnerIdentity)
	if err != nil || !hmac.Equal([]byte(currentOwnerBinding), []byte(ownerBinding)) {
		return fmt.Errorf("Google OAuth source owner changed during authorization; restart the connection")
	}
	if strings.TrimSpace(source.ConnectorKey) != strings.TrimSpace(connectorKey) {
		return fmt.Errorf("Google OAuth source connector changed during authorization; restart the connection")
	}
	return googleOAuthSourceAllowed(source)
}

func (s *service) storeToken(ctx context.Context, sourceID uuid.UUID, ownerBinding, connectorKey string, token *googleoauth.Token) error {
	return s.storeGoogleOAuthToken(ctx, sourceID, ownerBinding, connectorKey, token, nil)
}

func (s *service) storeRefreshedToken(
	ctx context.Context,
	sourceID uuid.UUID,
	ownerBinding, connectorKey string,
	token *googleoauth.Token,
	expected *models.SourceOAuthToken,
) error {
	if expected == nil || expected.SourceID != sourceID || expected.Provider != googleProvider ||
		expected.ID == uuid.Nil || expected.UpdatedAt.IsZero() ||
		len(expected.AccessToken) == 0 || len(expected.RefreshToken) == 0 {
		return fmt.Errorf("Google OAuth refresh is not bound to a complete stored grant")
	}
	return s.storeGoogleOAuthToken(ctx, sourceID, ownerBinding, connectorKey, token, expected)
}

func (s *service) storeGoogleOAuthToken(
	ctx context.Context,
	sourceID uuid.UUID,
	ownerBinding, connectorKey string,
	token *googleoauth.Token,
	expected *models.SourceOAuthToken,
) error {
	if ctx == nil {
		return fmt.Errorf("Google OAuth token persistence context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if token == nil {
		return fmt.Errorf("Google OAuth token response is required")
	}
	source, err := s.repo.FindSource(sourceID)
	if err != nil {
		return err
	}
	if err := googleOAuthSourceMatchesBinding(source, sourceID, ownerBinding, connectorKey); err != nil {
		return err
	}
	expectedScope, err := requiredGoogleScope(connectorKey)
	if err != nil {
		return err
	}
	storedScope, err := validateGoogleOAuthScope(token.Scope, expectedScope)
	if err != nil {
		return err
	}
	codec, err := tokenCodec()
	if err != nil {
		return err
	}
	accessCt, err := codec.Encrypt(token.AccessToken)
	if err != nil {
		return err
	}
	var refreshCt []byte
	if strings.TrimSpace(token.RefreshToken) == "" {
		if expected != nil {
			return fmt.Errorf("Google refresh returned no refresh token; refusing to save a stale grant")
		}
		existing, findErr := s.repo.FindOAuthToken(sourceID)
		switch {
		case findErr == nil:
			if existing == nil || existing.SourceID != sourceID || existing.Provider != googleProvider || len(existing.RefreshToken) == 0 {
				return fmt.Errorf("Google omitted the refresh token and no stored refresh token is available")
			}
			preservedRefreshToken, decryptErr := codec.Decrypt(existing.RefreshToken)
			if decryptErr != nil {
				return fmt.Errorf("could not verify the existing Google refresh token; refusing to replace it: %w", decryptErr)
			}
			if strings.TrimSpace(preservedRefreshToken) == "" {
				return fmt.Errorf("could not verify the existing Google refresh token; refusing to replace an empty token")
			}
			refreshCt = append([]byte(nil), existing.RefreshToken...)
		case errors.Is(findErr, gorm.ErrRecordNotFound):
			return fmt.Errorf("Google omitted the refresh token for a new connection")
		case findErr != nil:
			return fmt.Errorf("could not read the existing Google refresh token; refusing to replace it: %w", findErr)
		}
	}
	if len(refreshCt) == 0 {
		refreshCt, err = codec.Encrypt(token.RefreshToken)
		if err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	currentSource, err := s.repo.FindSource(sourceID)
	if err != nil {
		return err
	}
	if err := googleOAuthSourceMatchesBinding(currentSource, sourceID, ownerBinding, connectorKey); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	storedToken := &models.SourceOAuthToken{
		SourceID:     sourceID,
		Provider:     googleProvider,
		AccessToken:  accessCt,
		RefreshToken: refreshCt,
		Scope:        storedScope,
		Expiry:       token.Expiry,
	}
	var reconnectCleared bool
	if expected != nil {
		refreshRepo, ok := s.repo.(googleOAuthAtomicRefreshTokenRepository)
		if !ok {
			return fmt.Errorf("Google OAuth refresh persistence requires a token-snapshot repository")
		}
		err = refreshRepo.SaveGoogleOAuthRefreshTokenForSource(ctx, storedToken, currentSource.OwnerIdentity, connectorKey, expected)
	} else {
		atomicRepo, ok := s.repo.(googleOAuthAtomicTokenRepository)
		if !ok {
			return fmt.Errorf("Google OAuth token persistence requires an atomic source-binding repository")
		}
		reconnectCleared, err = atomicRepo.SaveGoogleOAuthTokenForSource(ctx, storedToken, currentSource.OwnerIdentity, connectorKey)
	}
	if err != nil {
		return err
	}
	if reconnectCleared {
		s.audit(sourceID, "source.oauth_reconnected", "Google read-only account grant was refreshed after reauthorization")
	}
	return nil
}

// googleAccessToken returns a currently-valid access token for exactly the
// source connector that received the grant.
func (s *service) googleAccessToken(ctx context.Context, sourceID uuid.UUID, connectorKey string) (string, error) {
	source, err := s.repo.FindSource(sourceID)
	if err != nil {
		return "", err
	}
	if err := googleOAuthSourceReadyForTokenUse(source); err != nil {
		return "", err
	}
	if source.ConnectorKey != connectorKey {
		return "", fmt.Errorf("google grant connector mismatch")
	}
	if strings.TrimSpace(source.OwnerIdentity) == "" {
		return "", fmt.Errorf("google grant is not bound to an authenticated source owner")
	}
	stored, err := s.repo.FindOAuthToken(sourceID)
	if errors.Is(err, gorm.ErrRecordNotFound) || err == nil && stored == nil {
		return "", fmt.Errorf("no connected Google account for this source; connect it first: %w", gorm.ErrRecordNotFound)
	}
	if err != nil {
		return "", fmt.Errorf("load stored Google OAuth grant: %w", err)
	}
	if stored == nil || stored.SourceID != sourceID || stored.Provider != googleProvider {
		return "", fmt.Errorf("stored OAuth token does not belong to this Google source")
	}
	codec, err := tokenCodec()
	if err != nil {
		return "", err
	}
	access, err := codec.Decrypt(stored.AccessToken)
	if err != nil {
		return "", err
	}
	var refresh string
	if len(stored.RefreshToken) > 0 {
		refresh, err = codec.Decrypt(stored.RefreshToken)
		if err != nil {
			return "", err
		}
	}
	tok := googleoauth.Token{AccessToken: access, RefreshToken: refresh, Expiry: stored.Expiry, Scope: stored.Scope}
	requiredScope, err := requiredGoogleScope(connectorKey)
	if err != nil {
		return "", err
	}
	if _, err := validateGoogleOAuthScope(tok.Scope, requiredScope); err != nil {
		return "", fmt.Errorf("stored google grant does not include the required read-only scope; reconnect the source")
	}
	if tok.Valid(time.Now()) {
		return tok.AccessToken, nil
	}
	if strings.TrimSpace(refresh) == "" {
		if persistErr := s.recordGoogleOAuthRefreshFailure(ctx, stored, googleoauth.ErrReauthorizationRequired); persistErr != nil {
			return "", fmt.Errorf("Google refresh token is unavailable; could not persist reconnect-required status: %w", persistErr)
		}
		return "", fmt.Errorf("%w: access token expired and no refresh token is stored", errGoogleOAuthReconnectRequired)
	}
	cfg, err := googleOAuthConfigForConnector(connectorKey)
	if err != nil {
		return "", err
	}
	refreshed, err := cfg.Refresh(ctx, refresh)
	if err != nil {
		if persistErr := s.recordGoogleOAuthRefreshFailure(ctx, stored, err); persistErr != nil {
			return "", fmt.Errorf("token refresh failed: %w (could not persist reconnect-required status: %v)", err, persistErr)
		}
		return "", fmt.Errorf("token refresh failed: %w", err)
	}
	if strings.TrimSpace(refreshed.Scope) == "" {
		refreshed.Scope = stored.Scope
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	ownerBinding, err := googleOAuthOwnerBinding(source.OwnerIdentity)
	if err != nil {
		return "", err
	}
	if err := s.storeRefreshedToken(ctx, sourceID, ownerBinding, connectorKey, refreshed, stored); err != nil {
		return "", err
	}
	return refreshed.AccessToken, nil
}

func (s *service) forceGoogleAccessTokenRefresh(
	ctx context.Context,
	sourceID uuid.UUID,
	connectorKey, rejectedAccessToken string,
) (string, error) {
	cfg, err := googleOAuthConfigForConnector(connectorKey)
	if err != nil {
		return "", err
	}
	return s.forceGoogleAccessTokenRefreshWith(ctx, sourceID, connectorKey, rejectedAccessToken, cfg.Refresh)
}

func (s *service) forceGoogleAccessTokenRefreshWith(
	ctx context.Context,
	sourceID uuid.UUID,
	connectorKey, rejectedAccessToken string,
	refreshToken func(context.Context, string) (*googleoauth.Token, error),
) (string, error) {
	if ctx == nil {
		return "", fmt.Errorf("Google OAuth refresh context is required")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	source, err := s.repo.FindSource(sourceID)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return "", ctxErr
	}
	if err != nil {
		return "", fmt.Errorf("load Google OAuth source: %w", err)
	}
	if err := googleOAuthSourceReadyForTokenUse(source); err != nil {
		return "", err
	}
	if source.ConnectorKey != connectorKey {
		return "", fmt.Errorf("Google grant connector mismatch")
	}
	if strings.TrimSpace(source.OwnerIdentity) == "" {
		return "", fmt.Errorf("Google grant is not bound to an authenticated source owner")
	}
	stored, err := s.repo.FindOAuthToken(sourceID)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return "", ctxErr
	}
	if errors.Is(err, gorm.ErrRecordNotFound) || err == nil && stored == nil {
		return "", fmt.Errorf("no connected Google account for this source; connect it first: %w", gorm.ErrRecordNotFound)
	}
	if err != nil {
		return "", fmt.Errorf("load stored Google OAuth grant: %w", err)
	}
	if stored.SourceID != sourceID || stored.Provider != googleProvider {
		return "", fmt.Errorf("stored OAuth token does not belong to this Google source")
	}
	codec, err := tokenCodec()
	if err != nil {
		return "", err
	}
	access, err := codec.Decrypt(stored.AccessToken)
	if err != nil {
		return "", fmt.Errorf("decrypt stored Google access token: %w", err)
	}
	var refresh string
	if len(stored.RefreshToken) > 0 {
		refresh, err = codec.Decrypt(stored.RefreshToken)
		if err != nil {
			return "", fmt.Errorf("decrypt stored Google refresh token: %w", err)
		}
	}
	token := googleoauth.Token{AccessToken: access, RefreshToken: refresh, Expiry: stored.Expiry, Scope: stored.Scope}
	requiredScope, err := requiredGoogleScope(connectorKey)
	if err != nil {
		return "", err
	}
	if _, err := validateGoogleOAuthScope(token.Scope, requiredScope); err != nil {
		return "", fmt.Errorf("stored Google grant does not include the required read-only scope; reconnect the source")
	}
	ownerBinding, err := googleOAuthOwnerBinding(source.OwnerIdentity)
	if err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	currentSource, err := s.repo.FindSource(sourceID)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return "", ctxErr
	}
	if err != nil {
		return "", fmt.Errorf("recheck Google OAuth source before token use: %w", err)
	}
	if err := googleOAuthSourceMatchesBinding(currentSource, sourceID, ownerBinding, connectorKey); err != nil {
		return "", err
	}
	if err := googleOAuthSourceReadyForTokenUse(currentSource); err != nil {
		return "", err
	}
	if strings.TrimSpace(access) != "" && access != rejectedAccessToken && token.Valid(time.Now()) {
		return access, nil
	}
	if strings.TrimSpace(refresh) == "" {
		if persistErr := s.recordGoogleOAuthRefreshFailure(ctx, stored, googleoauth.ErrReauthorizationRequired); persistErr != nil {
			return "", fmt.Errorf("Google refresh token is unavailable; could not persist reconnect-required status: %w", persistErr)
		}
		return "", fmt.Errorf("%w: the stored Google refresh token is unavailable", errGoogleOAuthReconnectRequired)
	}
	if refreshToken == nil {
		return "", fmt.Errorf("Google OAuth refresh provider is unavailable")
	}
	refreshed, err := refreshToken(ctx, refresh)
	if err != nil {
		if persistErr := s.recordGoogleOAuthRefreshFailure(ctx, stored, err); persistErr != nil {
			return "", fmt.Errorf("Google token refresh failed: %w (could not persist reconnect-required status: %v)", err, persistErr)
		}
		return "", fmt.Errorf("Google token refresh failed: %w", err)
	}
	if refreshed == nil || strings.TrimSpace(refreshed.AccessToken) == "" || refreshed.AccessToken == rejectedAccessToken {
		if persistErr := s.recordGoogleOAuthRefreshFailure(ctx, stored, googleoauth.ErrReauthorizationRequired); persistErr != nil {
			return "", fmt.Errorf("Google refresh did not replace the rejected access token; could not persist reconnect-required status: %w", persistErr)
		}
		return "", fmt.Errorf("%w: Google refresh did not replace the rejected access token", errGoogleOAuthReconnectRequired)
	}
	if strings.TrimSpace(refreshed.RefreshToken) == "" {
		refreshed.RefreshToken = refresh
	}
	if strings.TrimSpace(refreshed.Scope) == "" {
		refreshed.Scope = stored.Scope
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := s.storeRefreshedToken(ctx, sourceID, ownerBinding, connectorKey, refreshed, stored); err != nil {
		return "", err
	}
	return refreshed.AccessToken, nil
}

func (s *service) recordGoogleOAuthRefreshFailure(ctx context.Context, expected *models.SourceOAuthToken, refreshErr error) error {
	if !errors.Is(refreshErr, googleoauth.ErrReauthorizationRequired) {
		return nil
	}
	if ctx == nil || expected == nil || expected.SourceID == uuid.Nil || expected.Provider != googleProvider ||
		len(expected.AccessToken) == 0 && len(expected.RefreshToken) == 0 {
		return fmt.Errorf("Google refresh failure is not bound to the rejected token")
	}
	repository, ok := s.repo.(googleOAuthRefreshFailureRepository)
	if !ok {
		return fmt.Errorf("Google refresh failure persistence requires a token-bound repository")
	}
	changed, err := repository.SetGoogleOAuthReconnectRequiredForToken(ctx, expected)
	if err != nil {
		return err
	}
	if changed {
		s.audit(expected.SourceID, "source.oauth_reauthorization_required", "Google requires the owner to reconnect this read-only source")
	}
	return nil
}

func requiredGoogleScope(connectorKey string) (string, error) {
	switch strings.TrimSpace(connectorKey) {
	case gmailConnectorKey:
		return googleoauth.GmailReadonlyScope, nil
	case driveConnectorKey:
		return googleoauth.DriveReadonlyScope, nil
	case contactsConnectorKey:
		return googleoauth.ContactsReadonlyScope, nil
	case calendarConnectorKey:
		return googleoauth.CalendarReadonlyScope, nil
	default:
		return "", fmt.Errorf("connector %q does not use Google OAuth", connectorKey)
	}
}

func validateGoogleOAuthScope(grantedScope, requestedScope string) (string, error) {
	requested := strings.Fields(requestedScope)
	if len(requested) != 1 {
		return "", fmt.Errorf("Google connector must request exactly one read-only scope")
	}
	granted := strings.Fields(grantedScope)
	if len(granted) == 0 {
		return requested[0], nil
	}
	if len(granted) != 1 || granted[0] != requested[0] {
		return "", fmt.Errorf("Google granted scopes do not exactly match the connector's requested read-only scope")
	}
	return requested[0], nil
}

func isGoogleOAuthConnector(connectorKey string) bool {
	switch strings.TrimSpace(connectorKey) {
	case gmailConnectorKey, driveConnectorKey, contactsConnectorKey, calendarConnectorKey:
		return true
	default:
		return false
	}
}

func hasOAuthScope(granted, required string) bool {
	for _, scope := range strings.Fields(granted) {
		if scope == required {
			return true
		}
	}
	return false
}

const gmailCursorPrefix = "gmail:v1:"

type gmailCursor struct {
	Version   int    `json:"v"`
	Phase     string `json:"phase"`
	PageToken string `json:"pageToken,omitempty"`
	HistoryID string `json:"historyId,omitempty"`
}

func encodeGmailCursor(cursor gmailCursor) (string, error) {
	cursor.Version = 1
	payload, err := json.Marshal(cursor)
	if err != nil {
		return "", err
	}
	return gmailCursorPrefix + base64.RawURLEncoding.EncodeToString(payload), nil
}

func decodeGmailCursor(value string) (gmailCursor, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return gmailCursor{Version: 1, Phase: "backfill"}, nil
	}
	// Timestamp cursors came from the previous best-effort implementation. A
	// bounded provider-native backfill safely upgrades them; raw-item upserts
	// prevent duplicates.
	if _, err := time.Parse(time.RFC3339, value); err == nil {
		return gmailCursor{Version: 1, Phase: "backfill"}, nil
	}
	if !strings.HasPrefix(value, gmailCursorPrefix) {
		return gmailCursor{}, fmt.Errorf("unsupported Gmail cursor; reset or reconnect this source")
	}
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(value, gmailCursorPrefix))
	if err != nil {
		return gmailCursor{}, fmt.Errorf("decode Gmail cursor: %w", err)
	}
	var cursor gmailCursor
	if err := json.Unmarshal(payload, &cursor); err != nil {
		return gmailCursor{}, fmt.Errorf("decode Gmail cursor: %w", err)
	}
	if cursor.Version != 1 || (cursor.Phase != "backfill" && cursor.Phase != "history") {
		return gmailCursor{}, fmt.Errorf("unsupported Gmail cursor version or phase")
	}
	if cursor.Phase == "history" && !decimalGmailHistoryID(cursor.HistoryID) {
		return gmailCursor{}, fmt.Errorf("Gmail history cursor is missing a valid history ID")
	}
	if cursor.Phase == "backfill" && cursor.HistoryID != "" && !decimalGmailHistoryID(cursor.HistoryID) {
		return gmailCursor{}, fmt.Errorf("Gmail backfill cursor contains an invalid history boundary")
	}
	return cursor, nil
}

// gmailIncrementalQuery turns a stored cursor into a Gmail search query so a
// sync only fetches mail that arrived after the last run. The cursor is the
// newest message timestamp seen. Gmail's `after:` is second-granular and
// inclusive at the boundary, so a message can be re-listed; that is harmless
// because raw items are upserted by external id rather than duplicated.
func gmailIncrementalQuery(cursor string) string {
	parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(cursor))
	if err != nil {
		return ""
	}
	return fmt.Sprintf("after:%d", parsed.UTC().Unix())
}

// fetchGmailSource performs bounded historical backfill and then advances using
// Gmail's historyId feed. Expired history IDs restart a deduplicated backfill,
// as required by Gmail's synchronization contract.
func (s *service) fetchGmailSource(ctx context.Context, source *models.ConnectedSource) ([]ImportItem, string, error) {
	access, err := s.googleAccessToken(ctx, source.ID, gmailConnectorKey)
	if err != nil {
		return nil, "", err
	}
	client := googleoauth.GmailClient{
		AccessToken: access,
		HTTPClient:  s.googleOAuthReadHTTPClient(source.ID, gmailConnectorKey, access),
	}
	return fetchGmailSourceWithClient(ctx, client, source)
}

func fetchGmailSourceWithClient(ctx context.Context, client googleoauth.GmailClient, source *models.ConnectedSource) ([]ImportItem, string, error) {
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	cursor, err := decodeGmailCursor(source.Cursor)
	if err != nil {
		return nil, "", err
	}
	if cursor.Phase == "backfill" {
		if cursor.HistoryID == "" {
			cursor.HistoryID, err = client.GetProfileHistoryID(ctx)
			if err != nil {
				return nil, "", googleProviderSyncError("Gmail", fmt.Errorf("capture Gmail history boundary: %w", err))
			}
		}
		page, err := client.ListMessageIDsPage(ctx, gmailFetchLimit, "", cursor.PageToken)
		if err != nil {
			return nil, "", googleProviderSyncError("Gmail", err)
		}
		if page.NextPageToken != "" && page.NextPageToken == cursor.PageToken {
			return nil, "", fmt.Errorf("Gmail backfill page did not advance its provider cursor")
		}
		messages, err := fetchGmailMessages(ctx, client, page.IDs)
		if err != nil {
			return nil, "", err
		}
		if err := ctx.Err(); err != nil {
			return nil, "", err
		}
		if page.NextPageToken != "" {
			cursor.PageToken = page.NextPageToken
		} else {
			cursor.Phase = "history"
			cursor.PageToken = ""
		}
		next, err := encodeGmailCursor(cursor)
		return gmailMessagesToImportItems(messages, source), next, err
	}

	page, err := client.ListHistoryPage(ctx, cursor.HistoryID, cursor.PageToken, gmailFetchLimit)
	if errors.Is(err, googleoauth.ErrHistoryCursorExpired) {
		reset := *source
		reset.Cursor = ""
		return fetchGmailSourceWithClient(ctx, client, &reset)
	}
	if err != nil {
		return nil, "", googleProviderSyncError("Gmail", err)
	}
	if page.NextPageToken != "" && page.NextPageToken == cursor.PageToken {
		return nil, "", fmt.Errorf("Gmail history page did not advance its provider cursor")
	}
	messages, err := fetchGmailMessages(ctx, client, page.MessageIDs)
	if err != nil {
		return nil, "", err
	}
	for i := range messages {
		if messages[i].ContentStatus != "size_limit" {
			continue
		}
		for _, change := range page.Changes {
			if change.Type == "message_added" && change.MessageID == messages[i].ID {
				messages[i].HistoryID = change.HistoryID
				break
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	if page.NextPageToken != "" {
		cursor.PageToken = page.NextPageToken
	} else {
		cursor.PageToken = ""
		cursor.HistoryID, err = maxGmailHistoryID(cursor.HistoryID, page.HistoryID)
		if err != nil {
			return nil, "", fmt.Errorf("advance Gmail history cursor: %w", err)
		}
	}
	next, err := encodeGmailCursor(cursor)
	if err != nil {
		return nil, "", err
	}
	items := gmailMessagesToImportItems(messages, source)
	items = append(items, gmailHistoryChangesToImportItems(page.Changes, source)...)
	return items, next, nil
}

func maxGmailHistoryID(current, candidate string) (string, error) {
	current = strings.TrimSpace(current)
	candidate = strings.TrimSpace(candidate)
	if !decimalGmailHistoryID(candidate) {
		return "", fmt.Errorf("provider returned an empty or malformed history id")
	}
	if current == "" {
		return candidate, nil
	}
	if !decimalGmailHistoryID(current) || !decimalGmailHistoryID(candidate) {
		return "", fmt.Errorf("provider returned a malformed history id")
	}
	currentNumber := strings.TrimLeft(current, "0")
	candidateNumber := strings.TrimLeft(candidate, "0")
	if currentNumber == "" {
		currentNumber = "0"
	}
	if candidateNumber == "" {
		candidateNumber = "0"
	}
	if len(currentNumber) > len(candidateNumber) || (len(currentNumber) == len(candidateNumber) && currentNumber > candidateNumber) {
		return current, nil
	}
	return candidate, nil
}

func decimalGmailHistoryID(value string) bool {
	if value == "" {
		return false
	}
	for _, digit := range value {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	return true
}

func fetchGmailMessages(ctx context.Context, client googleoauth.GmailClient, ids []string) ([]googleoauth.GmailMessage, error) {
	messages, err := client.FetchMessageIDs(ctx, ids)
	if err != nil {
		return nil, googleProviderSyncError("Gmail", fmt.Errorf("fetch Gmail message page: %w", err))
	}
	return messages, nil
}

func gmailMessagesToImportItems(messages []googleoauth.GmailMessage, source *models.ConnectedSource) []ImportItem {
	projectKey := firstNonEmpty(source.DefaultProjectKey, "Robert-life-os")
	items := make([]ImportItem, 0, len(messages))
	for _, m := range messages {
		if m.ContentStatus == "size_limit" {
			metadata, _ := json.Marshal(map[string]any{
				"source": "gmail", "sourceId": source.ID, "messageId": m.ID,
				"historyId": m.HistoryID, "contentStatus": "size_limit",
				"contentLimitBytes": m.ContentLimitBytes,
				"reviewRequired": true, "reviewStatus": "pending", "readonly": true,
			})
			items = append(items, ImportItem{
				ExternalID: "gmail:content-limited:" + m.ID,
				Title:      "Gmail message exceeds the ingestion limit",
				Content:    fmt.Sprintf("Gmail returned a message larger than HAI's configured %d-byte response limit. This review item preserves the stable message ID and source link; message content was not imported. Review the original message in Gmail or choose an approved bounded extraction path before treating its content as processed.", m.ContentLimitBytes),
				SourceURI:  "https://mail.google.com/mail/u/0/#all/" + m.ID,
				ItemType:   "email_message_content_limited",
				ProjectKey: projectKey,
				Metadata:   string(metadata),
			})
			continue
		}
		if m.Unavailable {
			metadata, _ := json.Marshal(map[string]any{
				"source": "gmail", "messageUnavailable": true,
				"availabilityStatus": "message_no_longer_available", "readonly": true,
			})
			items = append(items, ImportItem{
				ExternalID: "gmail:unavailable:" + m.ID,
				Title:      "Gmail message no longer available",
				Content:    "Gmail listed this message, but it is no longer accessible. Its stable message ID was retained so this permanent provider outcome is visible; message content was not imported.",
				SourceURI:  "https://mail.google.com/mail/u/0/#all/" + m.ID,
				ItemType:   "email_message_unavailable",
				ProjectKey: projectKey,
				Metadata:   string(metadata),
			})
			continue
		}
		content := strings.Join([]string{
			"From: " + m.From,
			"To: " + m.To,
			"Subject: " + m.Subject,
			"Date: " + formatOptionalTime(m.Date),
			"Thread: " + m.ThreadID,
		}, "\n")
		messageText := firstNonEmpty(strings.TrimSpace(m.Body), strings.TrimSpace(m.Snippet))
		if messageText != "" {
			content += "\n\nMessage:\n" + messageText
		}
		if len(m.Attachments) > 0 {
			content += fmt.Sprintf("\n\nAttachments (%d):", len(m.Attachments))
			for _, attachment := range m.Attachments {
				content += fmt.Sprintf("\n- %s (%s, %d bytes; content status=%s)", firstNonEmpty(attachment.Filename, "unnamed"), firstNonEmpty(attachment.MimeType, "unknown"), attachment.Size, firstNonEmpty(attachment.ContentStatus, "not_fetched"))
				if attachment.Fetched && strings.TrimSpace(attachment.Content) != "" {
					content += "\n  Extracted attachment text: " + attachment.Content
				}
			}
		}
		if m.AttachmentsOmitted > 0 {
			content += fmt.Sprintf("\n- %d additional attachments not listed; the configured per-message metadata limit was reached", m.AttachmentsOmitted)
		}
		attachmentStatuses := make([]string, 0, len(m.Attachments))
		reviewReasons := make([]string, 0, 3)
		if m.BodyTruncated {
			reviewReasons = append(reviewReasons, "message_body_truncated")
		}
		if m.BodyEncodingWarning {
			reviewReasons = append(reviewReasons, "message_body_encoding_warning")
		}
		for _, attachment := range m.Attachments {
			status := firstNonEmpty(attachment.ContentStatus, "not_fetched")
			attachmentStatuses = append(attachmentStatuses, status)
			if status != "fetched" && status != "empty" {
				reviewReasons = append(reviewReasons, "attachment_"+status)
			}
		}
		if m.AttachmentsOmitted > 0 {
			reviewReasons = append(reviewReasons, "attachment_metadata_omitted")
		}
		coverageWarnings := []string{}
		if m.BodyTruncated {
			coverageWarnings = append(coverageWarnings, fmt.Sprintf("the message body was truncated at %d bytes", m.BodyLimitBytes))
		}
		if m.BodyEncodingWarning {
			coverageWarnings = append(coverageWarnings, "the message body contained malformed or unsupported text encoding")
		}
		for _, reason := range reviewReasons {
			if strings.HasPrefix(reason, "attachment_") {
				coverageWarnings = append(coverageWarnings, "one or more attachments could not be fully extracted or listed")
				break
			}
		}
		if len(coverageWarnings) > 0 {
			content += "\n\nImport coverage warning: " + strings.Join(coverageWarnings, "; ") + ". Review the complete message and attachment list in Gmail before relying on this extraction."
		}
		reviewStatusValue := "not_required"
		if len(reviewReasons) > 0 {
			reviewStatusValue = "pending"
		}
		metadata, _ := json.Marshal(map[string]any{
			"source": "gmail", "from": m.From, "to": m.To,
			"date": formatOptionalTime(m.Date), "threadId": m.ThreadID,
			"historyId": m.HistoryID, "attachments": len(m.Attachments),
			"attachmentContentStatuses": attachmentStatuses,
			"attachmentsOmitted": m.AttachmentsOmitted,
			"bodyTruncated": m.BodyTruncated, "bodyLimitBytes": m.BodyLimitBytes,
			"bodyEncodingWarning": m.BodyEncodingWarning,
			"reviewRequired": len(reviewReasons) > 0, "reviewStatus": reviewStatusValue,
			"reviewReasons": reviewReasons, "readonly": true,
		})
		items = append(items, ImportItem{
			ExternalID: "gmail:" + m.ID,
			Title:      firstNonEmpty(m.Subject, "(no subject)"),
			Content:    content,
			SourceURI:  "https://mail.google.com/mail/u/0/#all/" + m.ID,
			ItemType:   "email_message",
			ProjectKey: projectKey,
			Metadata:   string(metadata),
		})
	}
	return items
}

func gmailHistoryChangesToImportItems(changes []googleoauth.GmailHistoryChange, source *models.ConnectedSource) []ImportItem {
	projectKey := firstNonEmpty(source.DefaultProjectKey, "Robert-life-os")
	items := make([]ImportItem, 0, len(changes))
	seen := make(map[string]bool, len(changes))
	for _, change := range changes {
		changeType := strings.TrimSpace(change.Type)
		if changeType != "message_deleted" && changeType != "labels_added" && changeType != "labels_removed" {
			continue
		}
		messageID := strings.TrimSpace(change.MessageID)
		historyID := strings.TrimSpace(change.HistoryID)
		if messageID == "" || historyID == "" {
			continue
		}
		labels := googleHistoryLabels(change.LabelIDs)
		identity, _ := json.Marshal(struct {
			HistoryID string   `json:"historyId"`
			MessageID string   `json:"messageId"`
			Type      string   `json:"type"`
			LabelIDs  []string `json:"labelIds"`
		}{historyID, messageID, changeType, labels})
		digest := sha256.Sum256(identity)
		itemType := "email_message_labels_changed"
		title := "Gmail labels changed"
		content := "Gmail reported labels added to this message: " + strings.Join(labels, ", ") + "."
		if changeType == "message_deleted" {
			itemType = "email_message_deleted"
			title = "Gmail message deleted"
			content = "Gmail reported that this message was deleted. This is a source-linked tombstone event; previously imported message content and evidence are preserved."
		} else if changeType == "labels_removed" {
			content = "Gmail reported labels removed from this message: " + strings.Join(labels, ", ") + "."
		}
		metadata, _ := json.Marshal(map[string]any{
			"source": "gmail", "changeType": changeType, "messageId": messageID,
			"historyId": historyID, "threadId": strings.TrimSpace(change.ThreadID),
			"labelIds": labels, "readonly": true, "evidencePreserved": true,
		})
		externalID := fmt.Sprintf("gmail:history:%s:%s:%x", historyID, changeType, digest[:16])
		if seen[externalID] {
			continue
		}
		seen[externalID] = true
		items = append(items, ImportItem{
			ExternalID: externalID,
			Title:      title,
			Content:    content,
			SourceURI:  "https://mail.google.com/mail/u/0/#all/" + messageID,
			ItemType:   itemType,
			ProjectKey: projectKey,
			Metadata:   string(metadata),
		})
	}
	return items
}

func googleHistoryLabels(labels []string) []string {
	seen := make(map[string]bool, len(labels))
	result := make([]string, 0, len(labels))
	for _, label := range labels {
		label = strings.TrimSpace(label)
		if label != "" && !seen[label] {
			seen[label] = true
			result = append(result, label)
		}
	}
	sort.Strings(result)
	return result
}
