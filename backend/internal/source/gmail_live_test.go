//go:build live

// Sandbox-only acceptance coverage for the Gmail connector projection. It is
// deliberately excluded from normal tests and reads one explicit message only.
// Required variables:
//
//	GMAIL_LIVE_CLIENT_ID
//	GMAIL_LIVE_CLIENT_SECRET
//	GMAIL_LIVE_REFRESH_TOKEN
//	GMAIL_LIVE_EXPECT_MESSAGE_ID
package source

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/googleoauth"
	"automation-hub-backend/internal/models"

	"github.com/google/uuid"
)

func liveGmailProjectionClient(t *testing.T) googleoauth.GmailClient {
	t.Helper()
	clientID := strings.TrimSpace(os.Getenv("GMAIL_LIVE_CLIENT_ID"))
	clientSecret := strings.TrimSpace(os.Getenv("GMAIL_LIVE_CLIENT_SECRET"))
	refreshToken := strings.TrimSpace(os.Getenv("GMAIL_LIVE_REFRESH_TOKEN"))
	if clientID == "" || clientSecret == "" || refreshToken == "" {
		t.Skip("GMAIL_LIVE_CLIENT_ID / GMAIL_LIVE_CLIENT_SECRET / GMAIL_LIVE_REFRESH_TOKEN not set; skipping live Gmail source projection test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	token, err := (googleoauth.Config{
		ClientID: clientID, ClientSecret: clientSecret,
		Scopes: []string{googleoauth.GmailReadonlyScope},
	}).Refresh(ctx, refreshToken)
	if err != nil {
		t.Fatalf("refresh dedicated Gmail read-only credential: %v", err)
	}
	if strings.TrimSpace(token.AccessToken) == "" {
		t.Fatal("Gmail OAuth refresh returned no access token")
	}
	return googleoauth.GmailClient{AccessToken: token.AccessToken}
}

func TestLiveGmailSourceProjection(t *testing.T) {
	messageID := strings.TrimSpace(os.Getenv("GMAIL_LIVE_EXPECT_MESSAGE_ID"))
	if messageID == "" {
		t.Skip("GMAIL_LIVE_EXPECT_MESSAGE_ID not set; skipping live Gmail source projection test")
	}
	client := liveGmailProjectionClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	message, err := client.GetMessageMetadata(ctx, messageID)
	if err != nil {
		t.Fatalf("read one expected Gmail message: %v", err)
	}
	items := gmailMessagesToImportItems([]googleoauth.GmailMessage{message}, &models.ConnectedSource{
		ID: uuid.New(), ConnectorKey: gmailConnectorKey, DefaultProjectKey: "live-gmail-sandbox",
	})
	if len(items) != 1 {
		t.Fatalf("Gmail source projection yielded %d items, want 1", len(items))
	}
	item := items[0]
	if item.ExternalID != "gmail:"+messageID || item.ItemType != "email_message" {
		t.Fatal("Gmail source projection did not preserve the expected external identity and item type")
	}
	if item.ProjectKey != "live-gmail-sandbox" || !strings.HasPrefix(item.SourceURI, "https://mail.google.com/") {
		t.Fatal("Gmail source projection did not preserve project provenance or source link")
	}
	if len(item.Content) > 2*1024*1024 {
		t.Fatalf("Gmail source projection exceeded its expected bounded content envelope: %d bytes", len(item.Content))
	}
}
