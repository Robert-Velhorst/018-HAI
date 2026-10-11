package source

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"automation-hub-backend/internal/config"
	"automation-hub-backend/internal/googleoauth"
	"automation-hub-backend/internal/models"

	"github.com/google/uuid"
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

// --- signed, durable, single-use CSRF state ---------------------------------
// The browser value carries the source id and expiry, HMAC-signed with a server
// secret. Only its SHA-256 digest is stored, owner-bound, in PostgreSQL so the
// attempt survives restarts but cannot be replayed. Format:
// base64(payload)."."base64(mac).

func stateSecret() ([]byte, error) {
	s := strings.TrimSpace(config.AppConfig.OAuthStateSigningKey)
	if s == "" {
		return nil, fmt.Errorf("oauth state signing key is not configured")
	}
	sum := sha256.Sum256([]byte("oauth-state|" + s))
	return sum[:], nil
}

func signState(sourceID uuid.UUID) (string, time.Time, error) {
	nonce, err := googleoauth.NewState()
	if err != nil {
		return "", time.Time{}, err
	}
	expiresAt := time.Now().UTC().Add(10 * time.Minute).Truncate(time.Second)
	payload := fmt.Sprintf("%s|%d|%s", sourceID.String(), expiresAt.Unix(), nonce)
	enc := base64.RawURLEncoding.EncodeToString([]byte(payload))
	secret, err := stateSecret()
	if err != nil {
		return "", time.Time{}, err
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(enc))
	return enc + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), expiresAt, nil
}

func verifyState(state string) (uuid.UUID, time.Time, error) {
	parts := strings.SplitN(strings.TrimSpace(state), ".", 2)
	if len(parts) != 2 {
		return uuid.Nil, time.Time{}, fmt.Errorf("malformed oauth state")
	}
	secret, err := stateSecret()
	if err != nil {
		return uuid.Nil, time.Time{}, err
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(parts[0]))
	expected := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expected), []byte(parts[1])) {
		return uuid.Nil, time.Time{}, fmt.Errorf("oauth state signature mismatch")
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return uuid.Nil, time.Time{}, fmt.Errorf("oauth state decode: %w", err)
	}
	fields := strings.Split(string(raw), "|")
	if len(fields) != 3 {
		return uuid.Nil, time.Time{}, fmt.Errorf("oauth state payload shape")
	}
	exp, err := strconv.ParseInt(fields[1], 10, 64)
	if err != nil || time.Now().Unix() > exp {
		return uuid.Nil, time.Time{}, fmt.Errorf("oauth state expired; restart the connection")
	}
	id, err := uuid.Parse(fields[0])
	if err != nil {
		return uuid.Nil, time.Time{}, fmt.Errorf("oauth state source id invalid")
	}
	return id, time.Unix(exp, 0).UTC(), nil
}

func oauthStateDigest(state string) string {
	digest := sha256.Sum256([]byte(strings.TrimSpace(state)))
	return fmt.Sprintf("%x", digest[:])
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
	if err := rejectRevokedSource(source); err != nil {
		return "", err
	}
	cfg, err := googleOAuthConfigForConnector(source.ConnectorKey)
	if err != nil {
		return "", err
	}
	state, expiresAt, err := signState(sourceID)
	if err != nil {
		return "", err
	}
	if err := s.repo.SaveOAuthState(&models.SourceOAuthState{
		SourceID:      sourceID,
		OwnerIdentity: source.OwnerIdentity,
		StateDigest:   oauthStateDigest(state),
		ExpiresAt:     expiresAt,
	}); err != nil {
		return "", err
	}
	return cfg.AuthorizeURL(state), nil
}

// CompleteGoogleOAuth handles the callback: verify state, exchange the code, and
// store the encrypted tokens against the source.
func (s *service) CompleteGoogleOAuth(ctx context.Context, code, state, ownerIdentity string) (uuid.UUID, error) {
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	if ownerIdentity == "" {
		return uuid.Nil, ErrOAuthStateInvalid
	}
	sourceID, _, err := verifyState(state)
	if err != nil {
		return uuid.Nil, ErrOAuthStateInvalid
	}
	if err := s.repo.ConsumeOAuthState(sourceID, ownerIdentity, oauthStateDigest(state), time.Now().UTC()); err != nil {
		return uuid.Nil, err
	}
	if strings.TrimSpace(code) == "" {
		return uuid.Nil, fmt.Errorf("missing authorization code")
	}
	if !googleOAuthReady() {
		return uuid.Nil, fmt.Errorf("google oauth is not configured")
	}
	source, err := s.repo.FindSource(sourceID)
	if err != nil {
		return uuid.Nil, err
	}
	if err := rejectRevokedSource(source); err != nil {
		return uuid.Nil, err
	}
	cfg, err := googleOAuthConfigForConnector(source.ConnectorKey)
	if err != nil {
		return uuid.Nil, err
	}
	token, err := cfg.ExchangeCode(ctx, code)
	if err != nil {
		return uuid.Nil, err
	}
	// Consent can complete after the operator revoked the source in another
	// session. Re-read before persistence; the repository repeats this check
	// under a source-row lock so revocation and credential storage serialize.
	source, err = s.repo.FindSource(sourceID)
	if err != nil {
		return uuid.Nil, err
	}
	if err := rejectRevokedSource(source); err != nil {
		return uuid.Nil, err
	}
	if err := s.storeToken(sourceID, token); err != nil {
		return uuid.Nil, err
	}
	s.audit(sourceID, "source.oauth_connected", "google account connected with least-privilege "+source.ConnectorKey+" read scope")
	return sourceID, nil
}

func (s *service) storeToken(sourceID uuid.UUID, token *googleoauth.Token) error {
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
		if existing, findErr := s.repo.FindOAuthToken(sourceID); findErr == nil {
			refreshCt = append([]byte(nil), existing.RefreshToken...)
			if strings.TrimSpace(token.Scope) == "" {
				token.Scope = existing.Scope
			}
		}
	}
	if refreshCt == nil {
		refreshCt, err = codec.Encrypt(token.RefreshToken)
		if err != nil {
			return err
		}
	}
	return s.repo.SaveOAuthToken(&models.SourceOAuthToken{
		SourceID:     sourceID,
		Provider:     googleProvider,
		AccessToken:  accessCt,
		RefreshToken: refreshCt,
		Scope:        token.Scope,
		Expiry:       token.Expiry,
	})
}

// googleAccessToken returns a currently-valid access token for exactly the
// source connector that received the grant.
func (s *service) googleAccessToken(ctx context.Context, sourceID uuid.UUID, connectorKey string) (string, error) {
	source, err := s.repo.FindSource(sourceID)
	if err != nil {
		return "", err
	}
	if err := rejectRevokedSource(source); err != nil {
		return "", err
	}
	if source.ConnectorKey != connectorKey {
		return "", fmt.Errorf("google grant connector mismatch")
	}
	stored, err := s.repo.FindOAuthToken(sourceID)
	if err != nil {
		return "", fmt.Errorf("no connected google account for this source; connect it first")
	}
	codec, err := tokenCodec()
	if err != nil {
		return "", err
	}
	access, err := codec.Decrypt(stored.AccessToken)
	if err != nil {
		return "", err
	}
	refresh, err := codec.Decrypt(stored.RefreshToken)
	if err != nil {
		return "", err
	}
	tok := googleoauth.Token{AccessToken: access, RefreshToken: refresh, Expiry: stored.Expiry, Scope: stored.Scope}
	requiredScope, err := requiredGoogleScope(connectorKey)
	if err != nil {
		return "", err
	}
	if !hasOAuthScope(tok.Scope, requiredScope) {
		return "", fmt.Errorf("stored google grant does not include the required read-only scope; reconnect the source")
	}
	if tok.Valid(time.Now()) {
		return tok.AccessToken, nil
	}
	if strings.TrimSpace(refresh) == "" {
		return "", fmt.Errorf("access token expired and no refresh token stored; reconnect the google account")
	}
	cfg, err := googleOAuthConfigForConnector(connectorKey)
	if err != nil {
		return "", err
	}
	refreshed, err := cfg.Refresh(ctx, refresh)
	if err != nil {
		return "", fmt.Errorf("token refresh failed: %w", err)
	}
	if strings.TrimSpace(refreshed.Scope) == "" {
		refreshed.Scope = stored.Scope
	}
	if err := s.storeToken(sourceID, refreshed); err != nil {
		return "", err
	}
	return refreshed.AccessToken, nil
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
	return fetchGmailSourceWithClient(ctx, googleoauth.GmailClient{AccessToken: access}, source)
}

func fetchGmailSourceWithClient(ctx context.Context, client googleoauth.GmailClient, source *models.ConnectedSource) ([]ImportItem, string, error) {
	cursor, err := decodeGmailCursor(source.Cursor)
	if err != nil {
		return nil, "", err
	}
	if cursor.Phase == "backfill" {
		if cursor.HistoryID == "" {
			cursor.HistoryID, err = client.GetProfileHistoryID(ctx)
			if err != nil {
				return nil, "", fmt.Errorf("capture Gmail history boundary: %w", err)
			}
		}
		page, err := client.ListMessageIDsPage(ctx, gmailFetchLimit, "", cursor.PageToken)
		if err != nil {
			return nil, "", err
		}
		messages := client.FetchMessageIDs(ctx, page.IDs)
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
		return nil, "", err
	}
	messages := client.FetchMessageIDs(ctx, page.MessageIDs)
	if page.NextPageToken != "" {
		cursor.PageToken = page.NextPageToken
	} else {
		cursor.PageToken = ""
		cursor.HistoryID = firstNonEmpty(page.HistoryID, cursor.HistoryID)
	}
	next, err := encodeGmailCursor(cursor)
	return gmailMessagesToImportItems(messages, source), next, err
}

func gmailMessagesToImportItems(messages []googleoauth.GmailMessage, source *models.ConnectedSource) []ImportItem {
	projectKey := firstNonEmpty(source.DefaultProjectKey, "Robert-life-os")
	items := make([]ImportItem, 0, len(messages))
	for _, m := range messages {
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
				content += fmt.Sprintf("\n- %s (%s, %d bytes; content fetched=%t)", firstNonEmpty(attachment.Filename, "unnamed"), firstNonEmpty(attachment.MimeType, "unknown"), attachment.Size, attachment.Fetched)
				if attachment.Fetched && strings.TrimSpace(attachment.Content) != "" {
					content += "\n  Extracted attachment text: " + attachment.Content
				}
			}
		}
		metadata, _ := json.Marshal(map[string]any{
			"source": "gmail", "from": m.From, "to": m.To,
			"date": formatOptionalTime(m.Date), "threadId": m.ThreadID,
			"historyId": m.HistoryID, "attachments": len(m.Attachments), "readonly": true,
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
