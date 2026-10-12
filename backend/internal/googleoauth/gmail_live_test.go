//go:build live

// Reproducible live acceptance for the read-only Gmail client. It is excluded
// from normal tests and needs a dedicated sandbox OAuth client and mailbox:
//
//	GMAIL_LIVE_CLIENT_ID
//	GMAIL_LIVE_CLIENT_SECRET
//	GMAIL_LIVE_REFRESH_TOKEN
//	GMAIL_LIVE_EXPECT_MESSAGE_ID
//
// The test refreshes a read-only access token, reads one explicitly selected
// message, and prints no message content, addresses, subject, or token value.
package googleoauth

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func liveGmailClient(t *testing.T) GmailClient {
	t.Helper()
	clientID := strings.TrimSpace(os.Getenv("GMAIL_LIVE_CLIENT_ID"))
	clientSecret := strings.TrimSpace(os.Getenv("GMAIL_LIVE_CLIENT_SECRET"))
	refreshToken := strings.TrimSpace(os.Getenv("GMAIL_LIVE_REFRESH_TOKEN"))
	if clientID == "" || clientSecret == "" || refreshToken == "" {
		t.Skip("GMAIL_LIVE_CLIENT_ID / GMAIL_LIVE_CLIENT_SECRET / GMAIL_LIVE_REFRESH_TOKEN not set; skipping live Gmail test")
	}
	config := Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		Scopes:       []string{GmailReadonlyScope},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	token, err := config.Refresh(ctx, refreshToken)
	if err != nil {
		t.Fatalf("refresh dedicated Gmail read-only credential: %v", err)
	}
	if strings.TrimSpace(token.AccessToken) == "" {
		t.Fatal("Gmail OAuth refresh returned no access token")
	}
	return GmailClient{AccessToken: token.AccessToken}
}

func TestLiveGmailReadOnlyMessageProjection(t *testing.T) {
	messageID := strings.TrimSpace(os.Getenv("GMAIL_LIVE_EXPECT_MESSAGE_ID"))
	if messageID == "" {
		t.Skip("GMAIL_LIVE_EXPECT_MESSAGE_ID not set; skipping live Gmail message projection test")
	}
	client := liveGmailClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	historyID, err := client.GetProfileHistoryID(ctx)
	if err != nil || strings.TrimSpace(historyID) == "" {
		t.Fatalf("read Gmail profile history cursor: %v", err)
	}
	message, err := client.GetMessageMetadata(ctx, messageID)
	if err != nil {
		t.Fatalf("read one expected Gmail message: %v", err)
	}
	if strings.TrimSpace(message.ID) != messageID || strings.TrimSpace(message.HistoryID) == "" {
		t.Fatal("Gmail message projection did not retain the expected stable identifiers")
	}
	if len(message.Body) > maxGmailBodyBytes+3 {
		t.Fatalf("Gmail message body exceeded configured bound: %d bytes", len(message.Body))
	}
	for _, attachment := range message.Attachments {
		if attachment.Fetched && len(attachment.Content) > maxGmailAttachmentBytes {
			t.Fatalf("fetched Gmail attachment %q exceeded configured bound", attachment.Filename)
		}
	}
}
