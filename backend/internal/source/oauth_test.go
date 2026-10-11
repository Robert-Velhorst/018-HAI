package source

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/config"
	"automation-hub-backend/internal/googleoauth"
	"automation-hub-backend/internal/models"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// gmailIncrementalQuery turns the stored cursor into Gmail's `q` filter so a
// sync fetches only mail newer than the last run.
func TestGmailIncrementalQuery(t *testing.T) {
	cases := []struct {
		name, cursor, want string
	}{
		{"empty cursor fetches recent", "", ""},
		{"unparseable cursor is ignored", "not-a-time", ""},
		{"rfc3339 cursor becomes after: filter", "2026-07-23T03:39:26Z", "after:1784777966"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := gmailIncrementalQuery(tc.cursor); got != tc.want {
				t.Fatalf("gmailIncrementalQuery(%q) = %q, want %q", tc.cursor, got, tc.want)
			}
		})
	}
}

func TestGmailBackfillCapturesHistoryBoundaryAndMessageContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/users/me/profile":
			_, _ = w.Write([]byte(`{"historyId":"100"}`))
		case "/users/me/messages":
			_, _ = w.Write([]byte(`{"messages":[{"id":"m1"}]}`))
		case "/users/me/messages/m1":
			_, _ = w.Write([]byte(`{"id":"m1","threadId":"t1","historyId":"99","snippet":"Follow up: send the requested document.","internalDate":"1700000000000","payload":{"headers":[{"name":"From","value":"lawyer@example.com"},{"name":"Subject","value":"Document request"}]}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	source := &models.ConnectedSource{DefaultProjectKey: "legal"}
	items, cursorValue, err := fetchGmailSourceWithClient(context.Background(), googleoauth.GmailClient{AccessToken: "token", BaseURL: server.URL}, source)
	if err != nil || len(items) != 1 || !strings.Contains(items[0].Content, "requested document") || !strings.Contains(items[0].Metadata, `"historyId":"99"`) {
		t.Fatalf("items=%#v cursor=%q err=%v", items, cursorValue, err)
	}
	cursor, err := decodeGmailCursor(cursorValue)
	if err != nil || cursor.Phase != "history" || cursor.HistoryID != "100" {
		t.Fatalf("cursor=%#v err=%v", cursor, err)
	}
}

func TestGmailRepeatedPageTokenDoesNotAdvanceCursor(t *testing.T) {
	for _, phase := range []string{"backfill", "history"} {
		t.Run(phase, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/users/me/profile":
					_, _ = w.Write([]byte(`{"historyId":"200"}`))
				case "/users/me/messages":
					_, _ = w.Write([]byte(`{"messages":[],"nextPageToken":"repeat"}`))
				case "/users/me/history":
					_, _ = w.Write([]byte(`{"history":[],"historyId":"201","nextPageToken":"repeat"}`))
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()

			original := ""
			if phase == "backfill" {
				original, _ = encodeGmailCursor(gmailCursor{Phase: "backfill", PageToken: "repeat"})
			} else {
				original, _ = encodeGmailCursor(gmailCursor{Phase: "history", HistoryID: "200", PageToken: "repeat"})
			}
			items, next, err := fetchGmailSourceWithClient(context.Background(), googleoauth.GmailClient{AccessToken: "token", BaseURL: server.URL}, &models.ConnectedSource{Cursor: original})
			if err == nil || len(items) != 0 || next != "" {
				t.Fatalf("items=%#v next=%q err=%v; repeated provider token must fail without checkpoint", items, next, err)
			}
		})
	}
}

func TestGmailOversizedMessageIsRetainedAndBackfillAdvances(t *testing.T) {
	oversizedBody := strings.Repeat("x", (8<<20)+1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/users/me/profile":
			_, _ = w.Write([]byte(`{"historyId":"200"}`))
		case "/users/me/messages":
			_, _ = w.Write([]byte(`{"messages":[{"id":"m-large"},{"id":"m-normal"}]}`))
		case "/users/me/messages/m-large":
			_, _ = w.Write([]byte(oversizedBody))
		case "/users/me/messages/m-normal":
			_, _ = w.Write([]byte(`{"id":"m-normal","threadId":"t1","snippet":"normal message","internalDate":"1700000000000"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	source := &models.ConnectedSource{ID: uuid.New()}
	items, nextCursor, err := fetchGmailSourceWithClient(context.Background(), googleoauth.GmailClient{AccessToken: "token", BaseURL: server.URL}, source)
	if err != nil || len(items) != 2 {
		t.Fatalf("items=%#v cursor=%q err=%v; size-limited message must not pin the backfill page", items, nextCursor, err)
	}
	if items[0].ExternalID != "gmail:content-limited:m-large" || items[0].ItemType != "email_message_content_limited" ||
		!strings.Contains(items[0].Metadata, `"contentStatus":"size_limit"`) ||
		!strings.Contains(items[0].Metadata, `"reviewRequired":true`) ||
		!strings.Contains(items[0].Metadata, `"reviewStatus":"pending"`) ||
		!strings.Contains(items[0].Metadata, `"messageId":"m-large"`) ||
		!strings.Contains(items[0].Metadata, `"sourceId":"`+source.ID.String()+`"`) ||
		!strings.Contains(items[0].Metadata, `"contentLimitBytes":8388608`) ||
		items[0].SourceURI != "https://mail.google.com/mail/u/0/#all/m-large" {
		t.Fatalf("oversized message was not retained explicitly: %#v", items[0])
	}
	if items[1].ExternalID != "gmail:m-normal" || !strings.Contains(items[1].Content, "normal message") {
		t.Fatalf("later message was not ingested: %#v", items[1])
	}
	cursor, err := decodeGmailCursor(nextCursor)
	if err != nil || cursor.Phase != "history" || cursor.HistoryID != "200" {
		t.Fatalf("backfill did not advance to the captured history boundary: %#v err=%v", cursor, err)
	}
}

func TestGmailOversizedHistoryMessageIsReviewableAndAdvancesHistoryCursor(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/users/me/history":
			_, _ = w.Write([]byte(`{"historyId":"305","history":[{"id":"302","messagesAdded":[{"message":{"id":"m-large","threadId":"thread-1"}}]}]}`))
		case "/users/me/messages/m-large":
			_, _ = w.Write([]byte(strings.Repeat("x", (8<<20)+1)))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	previous, err := encodeGmailCursor(gmailCursor{Phase: "history", HistoryID: "300"})
	if err != nil {
		t.Fatal(err)
	}
	source := &models.ConnectedSource{ID: uuid.New(), Cursor: previous}
	items, nextCursor, err := fetchGmailSourceWithClient(context.Background(), googleoauth.GmailClient{AccessToken: "token", BaseURL: server.URL}, source)
	if err != nil || len(items) != 1 {
		t.Fatalf("items=%#v next=%q err=%v; oversized history message should become one review item", items, nextCursor, err)
	}
	item := items[0]
	if item.ExternalID != "gmail:content-limited:m-large" || item.ItemType != "email_message_content_limited" ||
		item.SourceURI != "https://mail.google.com/mail/u/0/#all/m-large" ||
		!strings.Contains(item.Metadata, `"sourceId":"`+source.ID.String()+`"`) ||
		!strings.Contains(item.Metadata, `"historyId":"302"`) ||
		!strings.Contains(item.Metadata, `"reviewRequired":true`) ||
		!strings.Contains(item.Metadata, `"reviewStatus":"pending"`) ||
		!strings.Contains(item.Metadata, `"contentLimitBytes":8388608`) {
		t.Fatalf("oversized history message lacks review/provenance details: %#v", item)
	}
	if strings.Contains(item.Content, strings.Repeat("x", 32)) || strings.Contains(item.Content, "From:") {
		t.Fatalf("oversized content leaked into review item: %#v", item)
	}
	cursor, err := decodeGmailCursor(nextCursor)
	if err != nil || cursor.Phase != "history" || cursor.HistoryID != "305" || cursor.PageToken != "" {
		t.Fatalf("history cursor did not advance after review item was emitted: %#v err=%v", cursor, err)
	}
}

func TestGmailHistoryChangesAreImportedWithoutReplacingMessageEvidence(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/users/me/messages/new-message" {
			_, _ = w.Write([]byte(`{"id":"new-message","threadId":"thread-2","historyId":"201","snippet":"newly imported evidence"}`))
			return
		}
		if r.URL.Path != "/users/me/history" {
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get("startHistoryId") != "200" {
			t.Errorf("startHistoryId = %q, want 200", r.URL.Query().Get("startHistoryId"))
		}
		if r.URL.Query().Get("pageToken") == "next" {
			_, _ = w.Write([]byte(`{"historyId":"205","history":[{"id":"204","labelsRemoved":[{"message":{"id":"existing-message","threadId":"thread-1"},"labelIds":["STARRED"]}]}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"historyId":"204","nextPageToken":"next","history":[
			{"id":"201","messagesAdded":[{"message":{"id":"new-message","threadId":"thread-2"}}]},
			{"id":"202","messagesDeleted":[{"message":{"id":"existing-message","threadId":"thread-1"}}]},
			{"id":"203","labelsAdded":[{"message":{"id":"existing-message","threadId":"thread-1"},"labelIds":["INBOX","STARRED"]}]}
		]}`))
	}))
	defer server.Close()

	cursor, err := encodeGmailCursor(gmailCursor{Phase: "history", HistoryID: "200"})
	if err != nil {
		t.Fatal(err)
	}
	source := &models.ConnectedSource{Cursor: cursor, DefaultProjectKey: "legal"}
	client := googleoauth.GmailClient{AccessToken: "token", BaseURL: server.URL}

	firstItems, firstCursor, err := fetchGmailSourceWithClient(context.Background(), client, source)
	if err != nil {
		t.Fatalf("first history page: %v", err)
	}
	firstState, err := decodeGmailCursor(firstCursor)
	if err != nil || firstState.HistoryID != "200" || firstState.PageToken != "next" {
		t.Fatalf("first page cursor = %#v, err=%v; page cursor must not advance history boundary", firstState, err)
	}
	if len(firstItems) != 3 || firstItems[0].ExternalID != "gmail:new-message" {
		t.Fatalf("first page items = %#v", firstItems)
	}
	if !hasGmailChangeItem(firstItems, "message_deleted", "existing-message") ||
		!hasGmailChangeItem(firstItems, "labels_added", "existing-message") {
		t.Fatalf("first page omitted deletion or label evidence: %#v", firstItems)
	}
	for _, item := range firstItems {
		if strings.Contains(item.ExternalID, "existing-message") && item.ExternalID == "gmail:existing-message" {
			t.Fatalf("history change reused the original message evidence key: %#v", item)
		}
	}

	source.Cursor = firstCursor
	secondItems, secondCursor, err := fetchGmailSourceWithClient(context.Background(), client, source)
	if err != nil {
		t.Fatalf("second history page: %v", err)
	}
	secondState, err := decodeGmailCursor(secondCursor)
	if err != nil || secondState.HistoryID != "205" || secondState.PageToken != "" {
		t.Fatalf("terminal page cursor = %#v, err=%v; expected monotonic boundary 205", secondState, err)
	}
	if len(secondItems) != 1 || !hasGmailChangeItem(secondItems, "labels_removed", "existing-message") {
		t.Fatalf("second page items = %#v", secondItems)
	}

	message := gmailMessagesToImportItems([]googleoauth.GmailMessage{{ID: "existing-message", Subject: "Original evidence", Body: "Preserve this"}}, source)
	tombstone := gmailHistoryChangesToImportItems([]googleoauth.GmailHistoryChange{{HistoryID: "202", MessageID: "existing-message", Type: "message_deleted"}}, source)
	if len(message) != 1 || len(tombstone) != 1 || message[0].ExternalID == tombstone[0].ExternalID ||
		!strings.Contains(message[0].Content, "Preserve this") || !strings.Contains(tombstone[0].Content, "preserved") {
		t.Fatalf("deletion tombstone must remain separate from prior message evidence: message=%#v tombstone=%#v", message, tombstone)
	}
	duplicate := gmailHistoryChangesToImportItems([]googleoauth.GmailHistoryChange{{HistoryID: "202", MessageID: "existing-message", Type: "message_deleted"}}, source)
	if duplicate[0].ExternalID != tombstone[0].ExternalID {
		t.Fatalf("replayed tombstone changed identity: first=%q replay=%q", tombstone[0].ExternalID, duplicate[0].ExternalID)
	}
}

func TestGmailHistoryCursorNeverRegresses(t *testing.T) {
	for _, test := range []struct{ current, candidate, want string }{
		{"200", "201", "201"},
		{"201", "200", "201"},
		{"0201", "200", "0201"},
		{"99999999999999999999", "100000000000000000000", "100000000000000000000"},
	} {
		got, err := maxGmailHistoryID(test.current, test.candidate)
		if err != nil || got != test.want {
			t.Errorf("maxGmailHistoryID(%q, %q) = %q, %v; want %q", test.current, test.candidate, got, err, test.want)
		}
	}
	if _, err := maxGmailHistoryID("200", "not-a-number"); err == nil {
		t.Fatal("malformed provider history ID must not be checkpointed")
	}
}

func hasGmailChangeItem(items []ImportItem, changeType, messageID string) bool {
	for _, item := range items {
		if strings.Contains(item.Metadata, `"changeType":"`+changeType+`"`) &&
			strings.Contains(item.Metadata, `"messageId":"`+messageID+`"`) {
			return true
		}
	}
	return false
}

func TestGmailSyncDoesNotAdvanceCursorWhenMessageFetchFails(t *testing.T) {
	for _, phase := range []string{"backfill", "history"} {
		t.Run(phase, func(t *testing.T) {
			firstMessageFetched := false
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if got := r.Header.Get("Authorization"); got != "Bearer token" {
					http.Error(w, "missing test bearer token", http.StatusUnauthorized)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/users/me/profile":
					_, _ = w.Write([]byte(`{"historyId":"200"}`))
				case "/users/me/messages":
					_, _ = w.Write([]byte(`{"messages":[{"id":"m1"},{"id":"m2"}]}`))
				case "/users/me/history":
					_, _ = w.Write([]byte(`{"history":[{"id":"201","messagesAdded":[{"message":{"id":"m1"}},{"message":{"id":"m2"}}]}],"historyId":"201"}`))
				case "/users/me/messages/m1":
					firstMessageFetched = true
					_, _ = w.Write([]byte(`{"id":"m1","threadId":"t1","historyId":"199","snippet":"first message","internalDate":"1700000000000"}`))
				case "/users/me/messages/m2":
					http.Error(w, "temporary provider failure", http.StatusServiceUnavailable)
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()

			source := &models.ConnectedSource{}
			if phase == "history" {
				cursor, err := encodeGmailCursor(gmailCursor{Phase: "history", HistoryID: "200"})
				if err != nil {
					t.Fatal(err)
				}
				source.Cursor = cursor
			}

			items, nextCursor, err := fetchGmailSourceWithClient(context.Background(), googleoauth.GmailClient{
				AccessToken: "token",
				BaseURL:     server.URL,
			}, source)
			if err == nil {
				t.Fatalf("fetchGmailSourceWithClient returned items=%#v cursor=%q without surfacing the failed message fetch", items, nextCursor)
			}
			var providerErr *providerSyncError
			if !errors.As(err, &providerErr) || providerErr.upstreamStatus != http.StatusServiceUnavailable || !providerErr.Retryable() {
				t.Fatalf("fetch error = %v, want typed retryable provider HTTP 503", err)
			}
			if nextCursor != "" {
				t.Fatalf("cursor advanced to %q after partial message fetch", nextCursor)
			}
			if len(items) != 0 {
				t.Fatalf("partial items=%#v returned despite page failure", items)
			}
			if !firstMessageFetched {
				t.Fatal("test did not reach the successful first message before the failing message")
			}
		})
	}
}

func TestGmailSyncAdvancesPastMessageNoLongerAvailable(t *testing.T) {
	for _, phase := range []string{"backfill", "history"} {
		t.Run(phase, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/users/me/profile":
					_, _ = w.Write([]byte(`{"historyId":"200"}`))
				case "/users/me/messages":
					_, _ = w.Write([]byte(`{"messages":[{"id":"m1"},{"id":"m2"}]}`))
				case "/users/me/history":
					_, _ = w.Write([]byte(`{"history":[{"id":"201","messagesAdded":[{"message":{"id":"m1"}},{"message":{"id":"m2"}}]}],"historyId":"201"}`))
				case "/users/me/messages/m1":
					_, _ = w.Write([]byte(`{"id":"m1","threadId":"t1","historyId":"199","snippet":"available message","internalDate":"1700000000000"}`))
				case "/users/me/messages/m2":
					w.WriteHeader(http.StatusNotFound)
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()

			source := &models.ConnectedSource{}
			if phase == "history" {
				cursor, err := encodeGmailCursor(gmailCursor{Phase: "history", HistoryID: "200"})
				if err != nil {
					t.Fatal(err)
				}
				source.Cursor = cursor
			}

			items, nextCursor, err := fetchGmailSourceWithClient(context.Background(), googleoauth.GmailClient{AccessToken: "token", BaseURL: server.URL}, source)
			if err != nil || len(items) != 2 || items[0].ExternalID != "gmail:m1" || items[1].ExternalID != "gmail:unavailable:m2" {
				t.Fatalf("items=%#v cursor=%q err=%v", items, nextCursor, err)
			}
			if items[1].ItemType != "email_message_unavailable" || !strings.Contains(items[1].Metadata, `"messageUnavailable":true`) {
				t.Fatalf("permanently unavailable message was not retained as an observable record: %#v", items[1])
			}
			cursor, err := decodeGmailCursor(nextCursor)
			if err != nil {
				t.Fatal(err)
			}
			if phase == "backfill" && (cursor.Phase != "history" || cursor.HistoryID != "200") {
				t.Fatalf("backfill cursor did not reach captured history boundary: %#v", cursor)
			}
			if phase == "history" && (cursor.Phase != "history" || cursor.HistoryID != "201") {
				t.Fatalf("history cursor did not advance past permanently missing mail: %#v", cursor)
			}
		})
	}
}

func TestGmailUnavailableObservationDoesNotReplaceImportedEvidence(t *testing.T) {
	message := gmailMessagesToImportItems([]googleoauth.GmailMessage{{ID: "m1", Subject: "Original", Body: "Original body"}}, &models.ConnectedSource{})
	unavailable := gmailMessagesToImportItems([]googleoauth.GmailMessage{{ID: "m1", Unavailable: true}}, &models.ConnectedSource{})
	limited := gmailMessagesToImportItems([]googleoauth.GmailMessage{{ID: "m1", ContentStatus: "size_limit"}}, &models.ConnectedSource{})
	if len(message) != 1 || len(unavailable) != 1 || len(limited) != 1 {
		t.Fatalf("expected separate original and provider observations: message=%#v unavailable=%#v limited=%#v", message, unavailable, limited)
	}
	if message[0].ExternalID != "gmail:m1" || unavailable[0].ExternalID != "gmail:unavailable:m1" || limited[0].ExternalID != "gmail:content-limited:m1" {
		t.Fatalf("provider observations reused the original evidence key: message=%q unavailable=%q limited=%q", message[0].ExternalID, unavailable[0].ExternalID, limited[0].ExternalID)
	}
	if !strings.Contains(message[0].Content, "Original body") {
		t.Fatalf("original evidence was altered: %#v", message[0])
	}
}

func TestGmailAttachmentFailureDoesNotAdvanceBackfillCursor(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/users/me/profile":
			_, _ = w.Write([]byte(`{"historyId":"300"}`))
		case "/users/me/messages":
			_, _ = w.Write([]byte(`{"messages":[{"id":"m1"}],"nextPageToken":"next-page"}`))
		case "/users/me/messages/m1":
			_, _ = w.Write([]byte(`{"id":"m1","payload":{"parts":[{"mimeType":"text/plain","filename":"note.txt","body":{"attachmentId":"a1","size":12}}]}}`))
		case "/users/me/messages/m1/attachments/a1":
			w.WriteHeader(http.StatusServiceUnavailable)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	source := &models.ConnectedSource{}
	items, next, err := fetchGmailSourceWithClient(context.Background(), googleoauth.GmailClient{AccessToken: "token", BaseURL: server.URL}, source)
	if err == nil || len(items) != 0 || next != "" || source.Cursor != "" {
		t.Fatalf("items=%#v next=%q sourceCursor=%q err=%v; transient attachment errors must leave the page retryable", items, next, source.Cursor, err)
	}
	var providerErr *providerSyncError
	if !errors.As(err, &providerErr) || providerErr.upstreamStatus != http.StatusServiceUnavailable || !providerErr.Retryable() {
		t.Fatalf("error = %v; want typed retryable provider HTTP 503", err)
	}
}

func TestGmailProjectionReportsUnsupportedAndUnavailableAttachments(t *testing.T) {
	items := gmailMessagesToImportItems([]googleoauth.GmailMessage{{
		ID: "m1",
		Attachments: []googleoauth.GmailAttachment{
			{Filename: "scan.pdf", MimeType: "application/pdf", ContentStatus: "unsupported_mime"},
			{Filename: "gone.txt", MimeType: "text/plain", ContentStatus: "unavailable"},
			{Filename: "broken.txt", MimeType: "text/plain", ContentStatus: "invalid_encoding"},
		},
		AttachmentsOmitted: 2,
	}}, &models.ConnectedSource{})
	if len(items) != 1 || !strings.Contains(items[0].Content, "content status=unsupported_mime") ||
		!strings.Contains(items[0].Content, "content status=unavailable") ||
		!strings.Contains(items[0].Content, "content status=invalid_encoding") ||
		!strings.Contains(items[0].Content, "2 additional attachments not listed") ||
		!strings.Contains(items[0].Metadata, `"attachmentsOmitted":2`) ||
		!strings.Contains(items[0].Metadata, `"reviewRequired":true`) ||
		!strings.Contains(items[0].Metadata, `"reviewStatus":"pending"`) ||
		!strings.Contains(items[0].Metadata, `"attachment_metadata_omitted"`) ||
		!strings.Contains(items[0].Content, "Import coverage warning") {
		t.Fatalf("attachment extraction outcomes are not visible in the source item: %#v", items)
	}
}

func TestGmailProjectionMarksBodyEncodingWarningForReview(t *testing.T) {
	items := gmailMessagesToImportItems([]googleoauth.GmailMessage{{
		ID: "m1", Body: "safe replacement text", BodyEncodingWarning: true,
	}}, &models.ConnectedSource{})
	if len(items) != 1 || !strings.Contains(items[0].Content, "malformed or unsupported text encoding") ||
		!strings.Contains(items[0].Metadata, `"bodyEncodingWarning":true`) ||
		!strings.Contains(items[0].Metadata, `"reviewRequired":true`) ||
		!strings.Contains(items[0].Metadata, `"reviewReasons":["message_body_encoding_warning"]`) {
		t.Fatalf("body encoding warning was not surfaced as a source review item: %#v", items)
	}
}

func TestGmailProjectionMarksTruncatedBodyForReview(t *testing.T) {
	items := gmailMessagesToImportItems([]googleoauth.GmailMessage{{
		ID: "m1", Body: "partial message", BodyTruncated: true, BodyLimitBytes: 512 << 10,
	}}, &models.ConnectedSource{})
	if len(items) != 1 || !strings.Contains(items[0].Content, "body was truncated at 524288 bytes") ||
		!strings.Contains(items[0].Content, "Review the complete message and attachment list in Gmail") ||
		!strings.Contains(items[0].Metadata, `"bodyTruncated":true`) ||
		!strings.Contains(items[0].Metadata, `"reviewRequired":true`) ||
		!strings.Contains(items[0].Metadata, `"reviewReasons":["message_body_truncated"]`) {
		t.Fatalf("truncated message was not explicitly marked for source review: %#v", items)
	}
}

func TestGoogleOAuthStateFailsClosedWithoutDedicatedKey(t *testing.T) {
	previous := config.AppConfig
	t.Cleanup(func() { config.AppConfig = previous })
	config.AppConfig.OAuthStateSigningKey = ""
	if _, err := signState(uuid.Nil); err == nil {
		t.Fatal("signState must fail without a dedicated signing key")
	}
}

func TestGoogleOAuthStartUsesAStableSecretBoundPKCEVerifier(t *testing.T) {
	previous := config.AppConfig
	t.Cleanup(func() { config.AppConfig = previous })
	config.AppConfig.GoogleOAuthClientID = "client-id"
	config.AppConfig.GoogleOAuthClientSecret = "client-secret"
	config.AppConfig.GoogleOAuthRedirectURL = "https://example.test/google/callback"
	config.AppConfig.OAuthTokenEncryptionKey = "token-encryption-key"
	config.AppConfig.OAuthStateSigningKey = "state-signing-key"

	sourceID := uuid.New()
	svc := NewService(newFakeSourceRepo(&models.ConnectedSource{
		ID: sourceID, OwnerIdentity: "alice@example.test", ConnectorKey: gmailConnectorKey, Enabled: true, Status: "active",
	}), nil).(*service)
	raw, err := svc.StartGoogleOAuth(sourceID)
	if err != nil {
		t.Fatalf("StartGoogleOAuth: %v", err)
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse authorization URL: %v", err)
	}
	query := parsed.Query()
	state := query.Get("state")
	if got := query.Get("code_challenge_method"); got != "S256" {
		t.Fatalf("code_challenge_method = %q, want S256", got)
	}
	verifier, err := googleOAuthCodeVerifier(state)
	if err != nil {
		t.Fatal(err)
	}
	if len(verifier) < 43 || query.Get("code_challenge") == verifier || query.Get("code_challenge") == "" {
		t.Fatal("authorization URL did not expose only a PKCE challenge")
	}
	digest := sha256.Sum256([]byte(verifier))
	if got, want := query.Get("code_challenge"), base64.RawURLEncoding.EncodeToString(digest[:]); got != want {
		t.Fatalf("challenge = %q, want %q", got, want)
	}
	if id, err := verifyState(state); err != nil || id != sourceID {
		t.Fatalf("signed state failed validation: id=%s err=%v", id, err)
	}
	claims, err := verifyBoundGoogleOAuthState(state)
	if err != nil || claims.SourceID != sourceID.String() || claims.ConnectorKey != gmailConnectorKey {
		t.Fatalf("bound OAuth state = %#v, err=%v", claims, err)
	}
	expectedOwnerBinding, err := googleOAuthOwnerBinding("alice@example.test")
	if err != nil || claims.OwnerBinding != expectedOwnerBinding {
		t.Fatalf("OAuth state owner binding = %q, err=%v", claims.OwnerBinding, err)
	}
	if strings.Contains(state, "alice@example.test") {
		t.Fatal("OAuth state exposed the source owner's identity")
	}
}

func TestGoogleOAuthPKCEVerifierIsBoundToSignedState(t *testing.T) {
	previous := config.AppConfig
	t.Cleanup(func() { config.AppConfig = previous })
	config.AppConfig.OAuthStateSigningKey = "state-signing-key"

	first, err := signBoundGoogleOAuthState(uuid.New(), "alice@example.test", gmailConnectorKey)
	if err != nil {
		t.Fatal(err)
	}
	second, err := signBoundGoogleOAuthState(uuid.New(), "alice@example.test", gmailConnectorKey)
	if err != nil {
		t.Fatal(err)
	}
	firstVerifier, err := googleOAuthCodeVerifier(first)
	if err != nil {
		t.Fatal(err)
	}
	repeatedVerifier, err := googleOAuthCodeVerifier(first)
	if err != nil {
		t.Fatal(err)
	}
	secondVerifier, err := googleOAuthCodeVerifier(second)
	if err != nil {
		t.Fatal(err)
	}
	if firstVerifier != repeatedVerifier || firstVerifier == secondVerifier {
		t.Fatal("PKCE verifier must be deterministic for one state and distinct across states")
	}
	if _, err := verifyState(first + "tampered"); err == nil {
		t.Fatal("tampered state was accepted")
	}
}

func TestGoogleOAuthCallbackRejectsOwnerOrConnectorChangeBeforeExchange(t *testing.T) {
	previous := config.AppConfig
	t.Cleanup(func() { config.AppConfig = previous })
	config.AppConfig.GoogleOAuthClientID = "client-id"
	config.AppConfig.GoogleOAuthClientSecret = "client-secret"
	config.AppConfig.GoogleOAuthRedirectURL = "https://example.test/google/callback"
	config.AppConfig.OAuthTokenEncryptionKey = "token-encryption-key"
	config.AppConfig.OAuthStateSigningKey = "state-signing-key"

	for _, tc := range []struct {
		name            string
		changeOwner     string
		changeConnector string
	}{
		{name: "owner changed", changeOwner: "bob@example.test"},
		{name: "connector changed", changeConnector: driveConnectorKey},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sourceID := uuid.New()
			repo := newFakeSourceRepo(&models.ConnectedSource{
				ID: sourceID, OwnerIdentity: "alice@example.test", ConnectorKey: gmailConnectorKey,
				Enabled: true, Status: "active",
			})
			svc := NewService(repo, nil).(*service)
			authorizeURL, err := svc.StartGoogleOAuth(sourceID)
			if err != nil {
				t.Fatalf("StartGoogleOAuth: %v", err)
			}
			parsed, err := url.Parse(authorizeURL)
			if err != nil {
				t.Fatal(err)
			}
			state := parsed.Query().Get("state")
			if tc.changeOwner != "" {
				repo.sources[sourceID].OwnerIdentity = tc.changeOwner
			}
			if tc.changeConnector != "" {
				repo.sources[sourceID].ConnectorKey = tc.changeConnector
			}

			if _, err := svc.CompleteGoogleOAuth(context.Background(), "authorization-code", state); err == nil {
				t.Fatal("callback accepted a source whose owner or connector changed")
			}
			if _, saved := repo.oauthTokens[sourceID]; saved {
				t.Fatal("callback persisted OAuth tokens after a source binding mismatch")
			}
		})
	}
}

func TestGoogleOAuthStartRejectsOwnerlessLegacySource(t *testing.T) {
	previous := config.AppConfig
	t.Cleanup(func() { config.AppConfig = previous })
	config.AppConfig.GoogleOAuthClientID = "client-id"
	config.AppConfig.GoogleOAuthClientSecret = "client-secret"
	config.AppConfig.GoogleOAuthRedirectURL = "https://example.test/google/callback"
	config.AppConfig.OAuthTokenEncryptionKey = "token-encryption-key"
	config.AppConfig.OAuthStateSigningKey = "state-signing-key"

	sourceID := uuid.New()
	svc := NewService(newFakeSourceRepo(&models.ConnectedSource{
		ID: sourceID, ConnectorKey: gmailConnectorKey, Enabled: true, Status: "active",
	}), nil).(*service)
	if _, err := svc.StartGoogleOAuth(sourceID); err == nil {
		t.Fatal("OAuth started for an ownerless legacy source")
	}
}

type sourceOwnerChangingRepository struct {
	Repository
	calls        int
	changeOn     int
	changedOwner string
}

type googleOAuthRefreshCASRepository struct {
	*fakeSourceRepo
	refreshSaveCalls int
}

func (r *googleOAuthRefreshCASRepository) SaveGoogleOAuthRefreshTokenForSource(
	ctx context.Context,
	token *models.SourceOAuthToken,
	ownerIdentity, connectorKey string,
	expected *models.SourceOAuthToken,
) error {
	r.refreshSaveCalls++
	if err := ctx.Err(); err != nil {
		return err
	}
	source := r.sources[token.SourceID]
	if source == nil || source.OwnerIdentity != ownerIdentity || source.ConnectorKey != connectorKey {
		return errGoogleOAuthSourceBindingChanged
	}
	current := r.oauthTokens[token.SourceID]
	if current == nil || current.ID != expected.ID || !current.UpdatedAt.Equal(expected.UpdatedAt) ||
		string(current.AccessToken) != string(expected.AccessToken) ||
		string(current.RefreshToken) != string(expected.RefreshToken) || current.Scope != expected.Scope ||
		!current.Expiry.Equal(expected.Expiry) {
		return errGoogleOAuthTokenChanged
	}
	_, err := r.fakeSourceRepo.SaveGoogleOAuthTokenForSource(ctx, token, ownerIdentity, connectorKey)
	return err
}

func (r *sourceOwnerChangingRepository) FindSource(id uuid.UUID) (*models.ConnectedSource, error) {
	r.calls++
	source, err := r.Repository.FindSource(id)
	if err == nil && r.calls == r.changeOn {
		source.OwnerIdentity = r.changedOwner
	}
	return source, err
}

func TestStoreTokenRechecksOwnerImmediatelyBeforePersistence(t *testing.T) {
	previous := config.AppConfig
	t.Cleanup(func() { config.AppConfig = previous })
	config.AppConfig.OAuthTokenEncryptionKey = "token-key"
	config.AppConfig.OAuthStateSigningKey = "state-key"

	sourceID := uuid.New()
	base := newFakeSourceRepo(&models.ConnectedSource{
		ID: sourceID, OwnerIdentity: "alice@example.test", ConnectorKey: gmailConnectorKey,
		Enabled: true, Status: "active",
	})
	repo := &sourceOwnerChangingRepository{Repository: base, changeOn: 2, changedOwner: "bob@example.test"}
	ownerBinding := mustGoogleOAuthOwnerBinding(t, "alice@example.test")
	err := NewService(repo, nil).(*service).storeToken(context.Background(), sourceID, ownerBinding, gmailConnectorKey, &googleoauth.Token{
		AccessToken: "access-token", RefreshToken: "refresh-token", Scope: googleoauth.GmailReadonlyScope,
		Expiry: time.Now().Add(time.Hour),
	})
	if err == nil {
		t.Fatal("owner change after token preparation was not rejected")
	}
	if _, saved := base.oauthTokens[sourceID]; saved {
		t.Fatal("token was persisted after the source owner changed")
	}
}

func TestStoreTokenHonorsCancellationBeforePersistence(t *testing.T) {
	previous := config.AppConfig
	t.Cleanup(func() { config.AppConfig = previous })
	config.AppConfig.OAuthTokenEncryptionKey = "token-key"
	config.AppConfig.OAuthStateSigningKey = "state-key"

	sourceID := uuid.New()
	repo := newFakeSourceRepo(&models.ConnectedSource{
		ID: sourceID, OwnerIdentity: "alice@example.test", ConnectorKey: gmailConnectorKey,
		Enabled: true, Status: "active",
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := NewService(repo, nil).(*service).storeToken(ctx, sourceID, mustGoogleOAuthOwnerBinding(t, "alice@example.test"), gmailConnectorKey, &googleoauth.Token{
		AccessToken: "access-token", RefreshToken: "refresh-token", Scope: googleoauth.GmailReadonlyScope,
		Expiry: time.Now().Add(time.Hour),
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("storeToken error = %v, want context.Canceled", err)
	}
	if _, saved := repo.oauthTokens[sourceID]; saved {
		t.Fatal("cancelled token persistence wrote a grant")
	}
}

func TestGoogleAccessTokenRejectsTokenStoredUnderAnotherProvider(t *testing.T) {
	sourceID := uuid.New()
	repo := newFakeSourceRepo(&models.ConnectedSource{
		ID: sourceID, OwnerIdentity: "alice@example.test", ConnectorKey: gmailConnectorKey,
		Enabled: true, Status: "active",
	})
	if err := repo.SaveOAuthToken(&models.SourceOAuthToken{
		SourceID: sourceID, Provider: "other-provider", AccessToken: []byte("opaque"), RefreshToken: []byte("opaque"),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := NewService(repo, nil).(*service).googleAccessToken(context.Background(), sourceID, gmailConnectorKey); err == nil {
		t.Fatal("non-Google token was accepted for a Google source")
	}
}

func TestGoogleAccessTokenRejectsInactiveOrReconnectRequiredSource(t *testing.T) {
	previous := config.AppConfig
	t.Cleanup(func() { config.AppConfig = previous })
	config.AppConfig.OAuthTokenEncryptionKey = "token-key"

	codec, err := tokenCodec()
	if err != nil {
		t.Fatal(err)
	}
	accessCipher, err := codec.Encrypt("still-valid-access-token")
	if err != nil {
		t.Fatal(err)
	}
	refreshCipher, err := codec.Encrypt("refresh-token")
	if err != nil {
		t.Fatal(err)
	}
	revokedAt := time.Now().UTC()
	tests := []struct {
		name   string
		source models.ConnectedSource
	}{
		{name: "paused", source: models.ConnectedSource{Enabled: true, Status: "paused"}},
		{name: "disabled", source: models.ConnectedSource{Enabled: false, Status: "active"}},
		{name: "revoked", source: models.ConnectedSource{Enabled: true, Status: "revoked", RevokedAt: &revokedAt}},
		{name: "reconnect required", source: models.ConnectedSource{Enabled: true, Status: "reconnect_required"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sourceID := uuid.New()
			source := tc.source
			source.ID = sourceID
			source.OwnerIdentity = "alice@example.test"
			source.ConnectorKey = gmailConnectorKey
			repo := newFakeSourceRepo(&source)
			if err := repo.SaveOAuthToken(&models.SourceOAuthToken{
				SourceID: sourceID, Provider: googleProvider, AccessToken: accessCipher,
				RefreshToken: refreshCipher, Scope: googleoauth.GmailReadonlyScope,
				Expiry: time.Now().Add(time.Hour),
			}); err != nil {
				t.Fatal(err)
			}

			if _, err := NewService(repo, nil).(*service).googleAccessToken(context.Background(), sourceID, gmailConnectorKey); err == nil {
				t.Fatal("Google token was available to a paused, disabled, revoked, or reconnect-required source")
			}
			if repo.oauthTokenSingleQueries != 0 {
				t.Fatalf("token lookup count = %d, want rejection before loading a cached token", repo.oauthTokenSingleQueries)
			}
		})
	}
}

func TestStaleRefreshCannotOverwriteSameOwnerReconnect(t *testing.T) {
	previous := config.AppConfig
	t.Cleanup(func() { config.AppConfig = previous })
	config.AppConfig.OAuthTokenEncryptionKey = "token-key"
	config.AppConfig.OAuthStateSigningKey = "state-key"

	sourceID := uuid.New()
	repo := &googleOAuthRefreshCASRepository{fakeSourceRepo: newFakeSourceRepo(&models.ConnectedSource{
		ID: sourceID, OwnerIdentity: "alice@example.test", ConnectorKey: gmailConnectorKey,
		Enabled: true, Status: "active",
	})}
	codec, err := tokenCodec()
	if err != nil {
		t.Fatal(err)
	}
	oldAccessCipher, err := codec.Encrypt("old-access")
	if err != nil {
		t.Fatal(err)
	}
	oldRefreshCipher, err := codec.Encrypt("old-refresh")
	if err != nil {
		t.Fatal(err)
	}
	oldToken := &models.SourceOAuthToken{
		ID: uuid.New(), SourceID: sourceID, Provider: googleProvider,
		AccessToken: oldAccessCipher, RefreshToken: oldRefreshCipher,
		Scope: googleoauth.GmailReadonlyScope, Expiry: time.Now().Add(-time.Minute),
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	if err := repo.SaveOAuthToken(oldToken); err != nil {
		t.Fatal(err)
	}
	staleSnapshot, err := repo.FindOAuthToken(sourceID)
	if err != nil {
		t.Fatal(err)
	}

	svc := NewService(repo, nil).(*service)
	ownerBinding := mustGoogleOAuthOwnerBinding(t, "alice@example.test")
	if err := svc.storeToken(context.Background(), sourceID, ownerBinding, gmailConnectorKey, &googleoauth.Token{
		AccessToken: "reconnected-access", RefreshToken: "reconnected-refresh",
		Scope: googleoauth.GmailReadonlyScope, Expiry: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("store same-owner reconnect grant: %v", err)
	}
	currentAfterReconnect := *repo.oauthTokens[sourceID]

	err = svc.storeRefreshedToken(context.Background(), sourceID, ownerBinding, gmailConnectorKey, &googleoauth.Token{
		AccessToken: "late-old-refresh-access", RefreshToken: "old-refresh",
		Scope: googleoauth.GmailReadonlyScope, Expiry: time.Now().Add(time.Hour),
	}, staleSnapshot)
	if !errors.Is(err, errGoogleOAuthTokenChanged) {
		t.Fatalf("stale refresh save error = %v, want current-grant conflict", err)
	}
	if repo.refreshSaveCalls != 1 {
		t.Fatalf("atomic refresh save calls = %d, want 1", repo.refreshSaveCalls)
	}
	current := repo.oauthTokens[sourceID]
	if string(current.AccessToken) != string(currentAfterReconnect.AccessToken) ||
		string(current.RefreshToken) != string(currentAfterReconnect.RefreshToken) {
		t.Fatal("late refresh overwrote the same-owner reconnect grant")
	}
}

func TestGoogleOAuthCallbackCancellationPreventsPersistence(t *testing.T) {
	previous := config.AppConfig
	t.Cleanup(func() { config.AppConfig = previous })
	config.AppConfig.GoogleOAuthClientID = "client-id"
	config.AppConfig.GoogleOAuthClientSecret = "client-secret"
	config.AppConfig.GoogleOAuthRedirectURL = "https://example.test/google/callback"
	config.AppConfig.OAuthTokenEncryptionKey = "token-encryption-key"
	config.AppConfig.OAuthStateSigningKey = "state-signing-key"

	sourceID := uuid.New()
	repo := newFakeSourceRepo(&models.ConnectedSource{
		ID: sourceID, OwnerIdentity: "alice@example.test", ConnectorKey: gmailConnectorKey,
		Enabled: true, Status: "active",
	})
	state, err := signBoundGoogleOAuthState(sourceID, "alice@example.test", gmailConnectorKey)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := NewService(repo, nil).(*service).CompleteGoogleOAuth(ctx, "authorization-code", state); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled callback error = %v, want context.Canceled", err)
	}
	if _, saved := repo.oauthTokens[sourceID]; saved {
		t.Fatal("cancelled callback persisted OAuth tokens")
	}
}

type successfulGoogleOAuthCallbackService struct {
	Service
	completions int
}

func (s *successfulGoogleOAuthCallbackService) CompleteGoogleOAuth(context.Context, string, string) (uuid.UUID, error) {
	s.completions++
	return uuid.New(), nil
}

func TestGoogleOAuthCallbackCookiePreventsBrowserReplay(t *testing.T) {
	gin.SetMode(gin.TestMode)
	configureGoogleOAuthHandlerState(t)
	service := &successfulGoogleOAuthCallbackService{}
	router := gin.New()
	router.GET("/sources/oauth/google/callback", NewHandler(service).GoogleOAuthCallback)
	state, err := signBoundGoogleOAuthState(uuid.New(), "alice@example.test", gmailConnectorKey)
	if err != nil {
		t.Fatal(err)
	}
	path := "/sources/oauth/google/callback?" + url.Values{"state": []string{state}, "code": []string{"one-time-code"}}.Encode()
	request := httptest.NewRequest(http.MethodGet, path, nil)
	request.AddCookie(&http.Cookie{Name: googleOAuthStateCookieName, Value: state})
	first := httptest.NewRecorder()
	router.ServeHTTP(first, request)
	if first.Code != http.StatusFound || service.completions != 1 {
		t.Fatalf("first callback status=%d completions=%d", first.Code, service.completions)
	}
	if !strings.Contains(first.Header().Get("Set-Cookie"), "Max-Age=0") {
		t.Fatalf("successful callback did not consume its browser state cookie: %q", first.Header().Get("Set-Cookie"))
	}
	replay := httptest.NewRecorder()
	router.ServeHTTP(replay, httptest.NewRequest(http.MethodGet, path, nil))
	if replay.Code != http.StatusBadRequest || service.completions != 1 {
		t.Fatalf("replayed callback status=%d completions=%d, want rejection without a second completion", replay.Code, service.completions)
	}
}

func TestGoogleOAuthConfigUsesOneConnectorSpecificReadonlyScope(t *testing.T) {
	cases := []struct {
		connector string
		wantScope string
	}{
		{gmailConnectorKey, googleoauth.GmailReadonlyScope},
		{driveConnectorKey, googleoauth.DriveReadonlyScope},
		{contactsConnectorKey, googleoauth.ContactsReadonlyScope},
		{calendarConnectorKey, googleoauth.CalendarReadonlyScope},
	}
	for _, tc := range cases {
		t.Run(tc.connector, func(t *testing.T) {
			cfg, err := googleOAuthConfigForConnector(tc.connector)
			if err != nil {
				t.Fatal(err)
			}
			if len(cfg.Scopes) != 1 || cfg.Scopes[0] != tc.wantScope {
				t.Fatalf("scopes = %#v, want only %q", cfg.Scopes, tc.wantScope)
			}
		})
	}
	if _, err := googleOAuthConfigForConnector("unknown"); err == nil {
		t.Fatal("unknown connector must not receive a Google OAuth configuration")
	}
}

func TestGoogleOAuthRejectsRevokedSourceBeforeConsentOrTokenExchange(t *testing.T) {
	previous := config.AppConfig
	t.Cleanup(func() { config.AppConfig = previous })
	config.AppConfig.GoogleOAuthClientID = "client"
	config.AppConfig.GoogleOAuthClientSecret = "secret"
	config.AppConfig.GoogleOAuthRedirectURL = "https://example.test/callback"
	config.AppConfig.OAuthTokenEncryptionKey = "token-key"
	config.AppConfig.OAuthStateSigningKey = "state-key"

	revokedAt := time.Now().UTC()
	sourceID := uuid.New()
	service := NewService(newFakeSourceRepo(&models.ConnectedSource{
		ID: sourceID, ConnectorKey: gmailConnectorKey, Enabled: false,
		Status: "revoked", RevokedAt: &revokedAt,
	}), nil).(*service)

	if _, err := service.StartGoogleOAuth(sourceID); !errors.Is(err, ErrSourceRevoked) {
		t.Fatalf("StartGoogleOAuth error = %v, want ErrSourceRevoked", err)
	}
	if err := googleOAuthSourceAllowed(&models.ConnectedSource{Status: "revoked"}); !errors.Is(err, ErrSourceRevoked) {
		t.Fatalf("callback source guard error = %v, want ErrSourceRevoked", err)
	}
}

func TestGmailCursorUpgradesTimestampAndRoundTripsNativeState(t *testing.T) {
	legacy, err := decodeGmailCursor("2026-07-23T03:39:26Z")
	if err != nil || legacy.Phase != "backfill" {
		t.Fatalf("legacy cursor = %#v, %v", legacy, err)
	}
	encoded, err := encodeGmailCursor(gmailCursor{Phase: "history", HistoryID: "123", PageToken: "next"})
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeGmailCursor(encoded)
	if err != nil || decoded.Phase != "history" || decoded.HistoryID != "123" || decoded.PageToken != "next" {
		t.Fatalf("decoded cursor = %#v, %v", decoded, err)
	}
}

func TestGoogleConnectionHealthDistinguishesDisconnectedAndReady(t *testing.T) {
	previous := config.AppConfig
	t.Cleanup(func() { config.AppConfig = previous })
	config.AppConfig.GoogleOAuthClientID = "client"
	config.AppConfig.GoogleOAuthClientSecret = "secret"
	config.AppConfig.GoogleOAuthRedirectURL = "https://example.test/callback"
	config.AppConfig.OAuthTokenEncryptionKey = "token-key"
	config.AppConfig.OAuthStateSigningKey = "state-key"

	sourceID := uuid.New()
	cursor, _ := encodeGmailCursor(gmailCursor{Phase: "history", HistoryID: "100"})
	repo := newFakeSourceRepo(&models.ConnectedSource{ID: sourceID, OwnerIdentity: "alice@example.test", ConnectorKey: gmailConnectorKey, Enabled: true, Status: "active", Cursor: cursor})
	service := NewService(repo, nil).(*service)
	health, err := service.ConnectionHealth(sourceID)
	if err != nil || health.Status != "disconnected" || health.Authorized {
		t.Fatalf("disconnected health = %#v, %v", health, err)
	}
	if err := repo.SaveOAuthToken(googleOAuthTestStoredGrant(t, sourceID, googleoauth.GmailReadonlyScope)); err != nil {
		t.Fatal(err)
	}
	health, err = service.ConnectionHealth(sourceID)
	if err != nil || health.Status != "ready" || !health.Authorized || health.CursorPhase != "history" {
		t.Fatalf("ready health = %#v, %v", health, err)
	}
}

func TestGoogleConnectionHealthPropagatesTokenRepositoryFailures(t *testing.T) {
	previous := config.AppConfig
	t.Cleanup(func() { config.AppConfig = previous })
	config.AppConfig.GoogleOAuthClientID = "client"
	config.AppConfig.GoogleOAuthClientSecret = "secret"
	config.AppConfig.GoogleOAuthRedirectURL = "https://example.test/callback"
	config.AppConfig.OAuthTokenEncryptionKey = "token-key"
	config.AppConfig.OAuthStateSigningKey = "state-key"

	sourceID := uuid.New()
	base := newFakeSourceRepo(&models.ConnectedSource{
		ID: sourceID, ConnectorKey: gmailConnectorKey, Enabled: true, Status: "active",
	})
	lookupErr := errors.New("temporary token repository failure")
	repo := &oauthTokenLookupFailureRepository{Repository: base, err: lookupErr}

	health, err := NewService(repo, nil).(*service).ConnectionHealth(sourceID)
	if health != nil || !errors.Is(err, lookupErr) {
		t.Fatalf("ConnectionHealth() = (%#v, %v), want repository error", health, err)
	}
}

func TestGoogleConnectionHealthHonorsDurableReconnectState(t *testing.T) {
	previous := config.AppConfig
	t.Cleanup(func() { config.AppConfig = previous })
	config.AppConfig.GoogleOAuthClientID = "client"
	config.AppConfig.GoogleOAuthClientSecret = "secret"
	config.AppConfig.GoogleOAuthRedirectURL = "https://example.test/callback"
	config.AppConfig.OAuthTokenEncryptionKey = "token-key"
	config.AppConfig.OAuthStateSigningKey = "state-key"

	sourceID := uuid.New()
	repo := newFakeSourceRepo(&models.ConnectedSource{
		ID: sourceID, ConnectorKey: gmailConnectorKey, Enabled: true, Status: "reconnect_required",
	})
	if err := repo.SaveOAuthToken(&models.SourceOAuthToken{
		SourceID: sourceID, Scope: googleoauth.GmailReadonlyScope,
		RefreshToken: []byte("encrypted"), Expiry: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	health, err := NewService(repo, nil).(*service).ConnectionHealth(sourceID)
	if err != nil || health.Status != "reconnect_required" || !health.RequiresReconnect || health.Authorized {
		t.Fatalf("reconnect-required health = %#v, %v", health, err)
	}
}

func TestGoogleReauthorizationStateTransitionsAreNarrowAndRecoverable(t *testing.T) {
	previous := config.AppConfig
	t.Cleanup(func() { config.AppConfig = previous })
	config.AppConfig.OAuthTokenEncryptionKey = "token-key"
	config.AppConfig.OAuthStateSigningKey = "state-key"

	sourceID := uuid.New()
	repo := newFakeSourceRepo(&models.ConnectedSource{
		ID: sourceID, OwnerIdentity: "alice@example.test", ConnectorKey: gmailConnectorKey, Enabled: true, Status: "active",
	})
	if err := repo.SaveOAuthToken(&models.SourceOAuthToken{
		SourceID: sourceID, Provider: googleProvider, RefreshToken: []byte("old-refresh-ciphertext"),
	}); err != nil {
		t.Fatal(err)
	}
	oldToken, err := repo.FindOAuthToken(sourceID)
	if err != nil {
		t.Fatalf("load old Google token: %v", err)
	}
	svc := NewService(repo, nil).(*service)

	if err := svc.recordGoogleOAuthRefreshFailure(context.Background(), oldToken, errors.New("temporary provider outage")); err != nil {
		t.Fatal(err)
	}
	if got := repo.sources[sourceID].Status; got != "active" {
		t.Fatalf("transient provider failure changed source status to %q", got)
	}
	if err := svc.recordGoogleOAuthRefreshFailure(context.Background(), oldToken, googleoauth.ErrReauthorizationRequired); err != nil {
		t.Fatal(err)
	}
	if got := repo.sources[sourceID].Status; got != "reconnect_required" {
		t.Fatalf("invalid_grant status = %q, want reconnect_required", got)
	}

	if err := svc.storeToken(context.Background(), sourceID, mustGoogleOAuthOwnerBinding(t, "alice@example.test"), gmailConnectorKey, &googleoauth.Token{
		AccessToken: "new-access", RefreshToken: "new-refresh",
		Scope: googleoauth.GmailReadonlyScope, Expiry: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	if got := repo.sources[sourceID].Status; got != "active" {
		t.Fatalf("status after storing replacement grant = %q, want active", got)
	}
	if err := svc.recordGoogleOAuthRefreshFailure(context.Background(), oldToken, googleoauth.ErrReauthorizationRequired); err != nil {
		t.Fatal(err)
	}
	if got := repo.sources[sourceID].Status; got != "active" {
		t.Fatalf("stale refresh failure changed replacement grant status to %q", got)
	}

	pausedID := uuid.New()
	paused := newFakeSourceRepo(&models.ConnectedSource{
		ID: pausedID, OwnerIdentity: "alice@example.test", ConnectorKey: gmailConnectorKey, Enabled: true, Status: "paused",
	})
	changed, err := paused.SetGoogleOAuthReconnectRequired(pausedID, true)
	if err != nil || changed || paused.sources[pausedID].Status != "paused" {
		t.Fatalf("paused source transition changed=%v status=%q err=%v", changed, paused.sources[pausedID].Status, err)
	}
	if err := paused.SaveOAuthToken(&models.SourceOAuthToken{
		SourceID: pausedID, Scope: googleoauth.GmailReadonlyScope,
		RefreshToken: []byte("encrypted"), Expiry: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	config.AppConfig.GoogleOAuthClientID = "client"
	config.AppConfig.GoogleOAuthClientSecret = "secret"
	config.AppConfig.GoogleOAuthRedirectURL = "https://example.test/callback"
	config.AppConfig.OAuthStateSigningKey = "state-key"
	health, err := NewService(paused, nil).(*service).ConnectionHealth(pausedID)
	if err != nil || health.Status != "paused" || health.Authorized {
		t.Fatalf("paused Google connection health = %#v, %v", health, err)
	}
}

func TestStoreTokenAcceptsOnlyTheExactConnectorScope(t *testing.T) {
	previous := config.AppConfig
	t.Cleanup(func() { config.AppConfig = previous })
	config.AppConfig.OAuthTokenEncryptionKey = "token-key"
	config.AppConfig.OAuthStateSigningKey = "state-key"

	tests := []struct {
		name      string
		grant     string
		wantError bool
	}{
		{name: "omitted scope means requested scope"},
		{name: "exact scope", grant: googleoauth.GmailReadonlyScope},
		{name: "broader scope", grant: googleoauth.GmailReadonlyScope + " " + googleoauth.DriveReadonlyScope, wantError: true},
		{name: "unexpected scope", grant: googleoauth.DriveReadonlyScope, wantError: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sourceID := uuid.New()
			repo := newFakeSourceRepo(&models.ConnectedSource{
				ID: sourceID, OwnerIdentity: "alice@example.test", ConnectorKey: gmailConnectorKey, Enabled: true, Status: "active",
			})
			err := NewService(repo, nil).(*service).storeToken(context.Background(), sourceID, mustGoogleOAuthOwnerBinding(t, "alice@example.test"), gmailConnectorKey, &googleoauth.Token{
				AccessToken: "access-token", RefreshToken: "refresh-token", Scope: tc.grant, Expiry: time.Now().Add(time.Hour),
			})
			if tc.wantError {
				if err == nil {
					t.Fatal("broader or unexpected scope was accepted")
				}
				if _, saved := repo.oauthTokens[sourceID]; saved {
					t.Fatal("token was stored despite a scope mismatch")
				}
				return
			}
			if err != nil {
				t.Fatalf("storeToken: %v", err)
			}
			stored := repo.oauthTokens[sourceID]
			if stored == nil || stored.Scope != googleoauth.GmailReadonlyScope {
				t.Fatalf("stored token scope = %#v, want canonical requested scope", stored)
			}
		})
	}
}

type oauthTokenLookupFailureRepository struct {
	Repository
	err error
}

func (r *oauthTokenLookupFailureRepository) FindOAuthToken(uuid.UUID) (*models.SourceOAuthToken, error) {
	return nil, r.err
}

func TestStoreTokenDoesNotOverwriteRefreshTokenAfterRepositoryReadFailure(t *testing.T) {
	previous := config.AppConfig
	t.Cleanup(func() { config.AppConfig = previous })
	config.AppConfig.OAuthTokenEncryptionKey = "token-key"
	config.AppConfig.OAuthStateSigningKey = "state-key"

	sourceID := uuid.New()
	base := newFakeSourceRepo(&models.ConnectedSource{
		ID: sourceID, OwnerIdentity: "alice@example.test", ConnectorKey: gmailConnectorKey, Enabled: true, Status: "active",
	})
	original := &models.SourceOAuthToken{
		SourceID: sourceID, Provider: googleProvider, AccessToken: []byte("old-access-ciphertext"),
		RefreshToken: []byte("old-refresh-ciphertext"), Scope: googleoauth.GmailReadonlyScope,
		Expiry: time.Now().Add(time.Hour),
	}
	if err := base.SaveOAuthToken(original); err != nil {
		t.Fatal(err)
	}
	readErr := errors.New("temporary token repository failure")
	repo := &oauthTokenLookupFailureRepository{Repository: base, err: readErr}
	err := NewService(repo, nil).(*service).storeToken(context.Background(), sourceID, mustGoogleOAuthOwnerBinding(t, "alice@example.test"), gmailConnectorKey, &googleoauth.Token{
		AccessToken: "new-access-token", Scope: googleoauth.GmailReadonlyScope, Expiry: time.Now().Add(2 * time.Hour),
	})
	if !errors.Is(err, readErr) {
		t.Fatalf("storeToken error = %v, want repository read failure", err)
	}
	stored := base.oauthTokens[sourceID]
	if stored == nil || string(stored.RefreshToken) != string(original.RefreshToken) || string(stored.AccessToken) != string(original.AccessToken) {
		t.Fatalf("stored OAuth token changed after failed read: %#v", stored)
	}
}

func TestStoreTokenRejectsInitialGrantWithoutRefreshToken(t *testing.T) {
	previous := config.AppConfig
	t.Cleanup(func() { config.AppConfig = previous })
	config.AppConfig.OAuthTokenEncryptionKey = "token-key"
	config.AppConfig.OAuthStateSigningKey = "state-key"

	sourceID := uuid.New()
	repo := newFakeSourceRepo(&models.ConnectedSource{
		ID: sourceID, OwnerIdentity: "alice@example.test", ConnectorKey: gmailConnectorKey, Enabled: true, Status: "active",
	})
	err := NewService(repo, nil).(*service).storeToken(context.Background(), sourceID, mustGoogleOAuthOwnerBinding(t, "alice@example.test"), gmailConnectorKey, &googleoauth.Token{
		AccessToken: "access-token", Scope: googleoauth.GmailReadonlyScope, Expiry: time.Now().Add(time.Hour),
	})
	if err == nil {
		t.Fatal("initial grant without a refresh token was accepted")
	}
	if _, saved := repo.oauthTokens[sourceID]; saved {
		t.Fatal("initial grant without a refresh token was stored")
	}
}

func TestGoogleAccessTokenDistinguishesMissingGrantFromRepositoryFailure(t *testing.T) {
	sourceID := uuid.New()
	base := newFakeSourceRepo(&models.ConnectedSource{
		ID: sourceID, OwnerIdentity: "alice@example.test", ConnectorKey: gmailConnectorKey,
		Enabled: true, Status: "active",
	})
	_, missingErr := NewService(base, nil).(*service).googleAccessToken(context.Background(), sourceID, gmailConnectorKey)
	if !errors.Is(missingErr, gorm.ErrRecordNotFound) || !strings.Contains(missingErr.Error(), "no connected Google account") {
		t.Fatalf("missing grant error = %v, want a distinct not-connected result", missingErr)
	}

	readErr := errors.New("database read failed")
	repo := &oauthTokenLookupFailureRepository{Repository: base, err: readErr}
	_, repositoryErr := NewService(repo, nil).(*service).googleAccessToken(context.Background(), sourceID, gmailConnectorKey)
	if !errors.Is(repositoryErr, readErr) {
		t.Fatalf("repository error = %v, want underlying lookup failure", repositoryErr)
	}
	if strings.Contains(repositoryErr.Error(), "no connected Google account") {
		t.Fatalf("repository failure was misreported as disconnected: %v", repositoryErr)
	}
	_, forcedRepositoryErr := NewService(repo, nil).(*service).forceGoogleAccessTokenRefreshWith(
		context.Background(), sourceID, gmailConnectorKey, "rejected-access", nil,
	)
	if !errors.Is(forcedRepositoryErr, readErr) || strings.Contains(forcedRepositoryErr.Error(), "no connected Google account") {
		t.Fatalf("forced refresh lookup error = %v, want distinct repository failure", forcedRepositoryErr)
	}
}

func TestForcedGoogleOAuthRefreshUsesAtomicGrantSnapshot(t *testing.T) {
	previous := config.AppConfig
	t.Cleanup(func() { config.AppConfig = previous })
	config.AppConfig.OAuthTokenEncryptionKey = "token-key"
	config.AppConfig.OAuthStateSigningKey = "state-key"

	sourceID := uuid.New()
	now := time.Now().UTC()
	repo := &googleOAuthRefreshCASRepository{fakeSourceRepo: newFakeSourceRepo(&models.ConnectedSource{
		ID: sourceID, OwnerIdentity: "alice@example.test", ConnectorKey: gmailConnectorKey,
		Enabled: true, Status: "active",
	})}
	codec, err := tokenCodec()
	if err != nil {
		t.Fatal(err)
	}
	accessCipher, err := codec.Encrypt("rejected-access")
	if err != nil {
		t.Fatal(err)
	}
	refreshCipher, err := codec.Encrypt("stored-refresh")
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveOAuthToken(&models.SourceOAuthToken{
		ID: uuid.New(), SourceID: sourceID, Provider: googleProvider,
		AccessToken: accessCipher, RefreshToken: refreshCipher,
		Scope: googleoauth.GmailReadonlyScope, Expiry: now.Add(time.Hour),
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}

	refreshCalls := 0
	access, err := NewService(repo, nil).(*service).forceGoogleAccessTokenRefreshWith(
		context.Background(), sourceID, gmailConnectorKey, "rejected-access",
		func(_ context.Context, refresh string) (*googleoauth.Token, error) {
			refreshCalls++
			if refresh != "stored-refresh" {
				t.Fatalf("refresh credential = %q, want stored grant", refresh)
			}
			return &googleoauth.Token{AccessToken: "new-access", Scope: googleoauth.GmailReadonlyScope, Expiry: now.Add(2 * time.Hour)}, nil
		},
	)
	if err != nil || access != "new-access" {
		t.Fatalf("forced refresh access=%q err=%v", access, err)
	}
	if refreshCalls != 1 || repo.refreshSaveCalls != 1 {
		t.Fatalf("refresh calls=%d atomic saves=%d, want 1 each", refreshCalls, repo.refreshSaveCalls)
	}
	stored := repo.oauthTokens[sourceID]
	storedAccess, err := codec.Decrypt(stored.AccessToken)
	if err != nil || storedAccess != "new-access" {
		t.Fatalf("persisted access=%q err=%v", storedAccess, err)
	}
	storedRefresh, err := codec.Decrypt(stored.RefreshToken)
	if err != nil || storedRefresh != "stored-refresh" {
		t.Fatalf("persisted refresh=%q err=%v", storedRefresh, err)
	}
}

func TestForcedGoogleOAuthRefreshMarksMissingRefreshTokenReconnectRequired(t *testing.T) {
	previous := config.AppConfig
	t.Cleanup(func() { config.AppConfig = previous })
	config.AppConfig.OAuthTokenEncryptionKey = "token-key"
	config.AppConfig.OAuthStateSigningKey = "state-key"

	sourceID := uuid.New()
	now := time.Now().UTC()
	repo := newFakeSourceRepo(&models.ConnectedSource{
		ID: sourceID, OwnerIdentity: "alice@example.test", ConnectorKey: gmailConnectorKey,
		Enabled: true, Status: "active",
	})
	codec, err := tokenCodec()
	if err != nil {
		t.Fatal(err)
	}
	accessCipher, err := codec.Encrypt("rejected-access")
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveOAuthToken(&models.SourceOAuthToken{
		ID: uuid.New(), SourceID: sourceID, Provider: googleProvider,
		AccessToken: accessCipher, Scope: googleoauth.GmailReadonlyScope,
		Expiry: now.Add(time.Hour), CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	refreshCalls := 0
	_, err = NewService(repo, nil).(*service).forceGoogleAccessTokenRefreshWith(
		context.Background(), sourceID, gmailConnectorKey, "rejected-access",
		func(context.Context, string) (*googleoauth.Token, error) {
			refreshCalls++
			return nil, nil
		},
	)
	if !errors.Is(err, errGoogleOAuthReconnectRequired) || refreshCalls != 0 {
		t.Fatalf("refresh error=%v callback calls=%d, want reconnect-required without stale-token retry", err, refreshCalls)
	}
	if got := repo.sources[sourceID].Status; got != "reconnect_required" {
		t.Fatalf("source status = %q, want reconnect_required", got)
	}
}

func TestForcedGoogleOAuthRefreshMarksUnchangedRejectedTokenReconnectRequired(t *testing.T) {
	previous := config.AppConfig
	t.Cleanup(func() { config.AppConfig = previous })
	config.AppConfig.OAuthTokenEncryptionKey = "token-key"
	config.AppConfig.OAuthStateSigningKey = "state-key"

	sourceID := uuid.New()
	now := time.Now().UTC()
	repo := newFakeSourceRepo(&models.ConnectedSource{
		ID: sourceID, OwnerIdentity: "alice@example.test", ConnectorKey: gmailConnectorKey,
		Enabled: true, Status: "active",
	})
	codec, err := tokenCodec()
	if err != nil {
		t.Fatal(err)
	}
	accessCipher, err := codec.Encrypt("rejected-access")
	if err != nil {
		t.Fatal(err)
	}
	refreshCipher, err := codec.Encrypt("stored-refresh")
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveOAuthToken(&models.SourceOAuthToken{
		ID: uuid.New(), SourceID: sourceID, Provider: googleProvider,
		AccessToken: accessCipher, RefreshToken: refreshCipher,
		Scope: googleoauth.GmailReadonlyScope, Expiry: now.Add(time.Hour),
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	_, err = NewService(repo, nil).(*service).forceGoogleAccessTokenRefreshWith(
		context.Background(), sourceID, gmailConnectorKey, "rejected-access",
		func(context.Context, string) (*googleoauth.Token, error) {
			return &googleoauth.Token{
				AccessToken: "rejected-access", RefreshToken: "stored-refresh",
				Scope: googleoauth.GmailReadonlyScope, Expiry: now.Add(2 * time.Hour),
			}, nil
		},
	)
	if !errors.Is(err, errGoogleOAuthReconnectRequired) {
		t.Fatalf("refresh error = %v, want reconnect-required", err)
	}
	if got := repo.sources[sourceID].Status; got != "reconnect_required" {
		t.Fatalf("source status = %q, want reconnect_required", got)
	}
}

func TestStoreTokenDoesNotPreserveUndecryptableRefreshToken(t *testing.T) {
	previous := config.AppConfig
	t.Cleanup(func() { config.AppConfig = previous })
	config.AppConfig.OAuthTokenEncryptionKey = "current-token-key"
	config.AppConfig.OAuthStateSigningKey = "state-key"

	tests := []struct {
		name          string
		refreshCipher []byte
	}{
		{name: "ciphertext from previous encryption key"},
		{name: "corrupt ciphertext", refreshCipher: []byte("corrupt-ciphertext")},
	}
	oldCodec, err := googleoauth.NewCodec("previous-token-key")
	if err != nil {
		t.Fatal(err)
	}
	oldRefreshCipher, err := oldCodec.Encrypt("previous-refresh-token")
	if err != nil {
		t.Fatal(err)
	}
	tests[0].refreshCipher = oldRefreshCipher

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sourceID := uuid.New()
			repo := newFakeSourceRepo(&models.ConnectedSource{
				ID: sourceID, OwnerIdentity: "alice@example.test", ConnectorKey: gmailConnectorKey, Enabled: true, Status: "reconnect_required",
			})
			oldAccessCipher, err := oldCodec.Encrypt("previous-access-token")
			if err != nil {
				t.Fatal(err)
			}
			original := &models.SourceOAuthToken{
				SourceID: sourceID, Provider: googleProvider, AccessToken: oldAccessCipher,
				RefreshToken: append([]byte(nil), tc.refreshCipher...), Scope: googleoauth.GmailReadonlyScope,
				Expiry: time.Now().Add(time.Hour),
			}
			if err := repo.SaveOAuthToken(original); err != nil {
				t.Fatal(err)
			}

			err = NewService(repo, nil).(*service).storeToken(context.Background(), sourceID, mustGoogleOAuthOwnerBinding(t, "alice@example.test"), gmailConnectorKey, &googleoauth.Token{
				AccessToken: "new-access-token", Scope: googleoauth.GmailReadonlyScope, Expiry: time.Now().Add(2 * time.Hour),
			})
			if err == nil {
				t.Fatal("storeToken accepted an undecryptable existing refresh token")
			}
			stored := repo.oauthTokens[sourceID]
			if stored == nil || string(stored.RefreshToken) != string(original.RefreshToken) || string(stored.AccessToken) != string(original.AccessToken) {
				t.Fatalf("stored OAuth token changed after refresh-token verification failure: %#v", stored)
			}
			if got := repo.sources[sourceID].Status; got != "reconnect_required" {
				t.Fatalf("source status = %q after failed reauthorization, want reconnect_required", got)
			}
		})
	}
}

func TestGoogleConnectionHealthsLoadTokensInOneBatch(t *testing.T) {
	previous := config.AppConfig
	t.Cleanup(func() { config.AppConfig = previous })
	config.AppConfig.GoogleOAuthClientID = "client"
	config.AppConfig.GoogleOAuthClientSecret = "secret"
	config.AppConfig.GoogleOAuthRedirectURL = "https://example.test/callback"
	config.AppConfig.OAuthTokenEncryptionKey = "token-key"
	config.AppConfig.OAuthStateSigningKey = "state-key"

	gmailID := uuid.New()
	driveID := uuid.New()
	repo := newFakeSourceRepo(
		&models.ConnectedSource{ID: gmailID, OwnerIdentity: "alice@example.test", ConnectorKey: gmailConnectorKey, Enabled: true, Status: "active"},
		&models.ConnectedSource{ID: driveID, OwnerIdentity: "alice@example.test", ConnectorKey: driveConnectorKey, Enabled: true, Status: "active"},
		&models.ConnectedSource{ID: uuid.New(), ConnectorKey: "local-folder", Enabled: true, LocalOnly: true, Status: "active"},
	)
	if err := repo.SaveOAuthToken(googleOAuthTestStoredGrant(t, gmailID, googleoauth.GmailReadonlyScope)); err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveOAuthToken(googleOAuthTestStoredGrant(t, driveID, googleoauth.DriveReadonlyScope)); err != nil {
		t.Fatal(err)
	}

	sources, err := repo.FindSources(true)
	if err != nil {
		t.Fatal(err)
	}
	health, err := NewService(repo, nil).(*service).ConnectionHealths(sources)
	if err != nil || len(health) != 3 {
		t.Fatalf("health=%#v err=%v", health, err)
	}
	if repo.oauthTokenBatchQueries != 1 || repo.oauthTokenSingleQueries != 0 {
		t.Fatalf("token lookups batch/single = %d/%d, want 1/0", repo.oauthTokenBatchQueries, repo.oauthTokenSingleQueries)
	}
	healthByConnector := make(map[string]ConnectionHealth, len(health))
	for _, item := range health {
		healthByConnector[item.ConnectorKey] = item
	}
	for _, connector := range []string{gmailConnectorKey, driveConnectorKey} {
		item := healthByConnector[connector]
		if item.Status != "ready" || !item.Authorized {
			t.Fatalf("Google health = %#v, want ready authorized", item)
		}
	}
}

func mustGoogleOAuthOwnerBinding(t *testing.T, ownerIdentity string) string {
	t.Helper()
	binding, err := googleOAuthOwnerBinding(ownerIdentity)
	if err != nil {
		t.Fatalf("googleOAuthOwnerBinding: %v", err)
	}
	return binding
}

func googleOAuthTestStoredGrant(t *testing.T, sourceID uuid.UUID, scope string) *models.SourceOAuthToken {
	t.Helper()
	codec, err := tokenCodec()
	if err != nil {
		t.Fatal(err)
	}
	access, err := codec.Encrypt("replacement-access")
	if err != nil {
		t.Fatal(err)
	}
	refresh, err := codec.Encrypt("stored-refresh")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	return &models.SourceOAuthToken{
		ID: uuid.New(), SourceID: sourceID, Provider: googleProvider,
		AccessToken: access, RefreshToken: refresh, Scope: scope,
		Expiry: now.Add(time.Hour), CreatedAt: now, UpdatedAt: now,
	}
}

type googleOAuthReuseRaceRepository struct {
	Repository
	sourceReads    int
	onSourceRead   func(int)
	afterTokenRead func()
	recheckErr     error
}

func (r *googleOAuthReuseRaceRepository) FindSource(id uuid.UUID) (*models.ConnectedSource, error) {
	r.sourceReads++
	if r.onSourceRead != nil {
		r.onSourceRead(r.sourceReads)
	}
	if r.sourceReads == 2 && r.recheckErr != nil {
		return nil, r.recheckErr
	}
	return r.Repository.FindSource(id)
}

func (r *googleOAuthReuseRaceRepository) FindOAuthToken(id uuid.UUID) (*models.SourceOAuthToken, error) {
	token, err := r.Repository.FindOAuthToken(id)
	if r.afterTokenRead != nil {
		r.afterTokenRead()
	}
	return token, err
}

func TestForcedGoogleOAuthRefreshReuseRechecksSourceAndCancellation(t *testing.T) {
	previous := config.AppConfig
	t.Cleanup(func() { config.AppConfig = previous })
	config.AppConfig.OAuthTokenEncryptionKey = "token-key"
	config.AppConfig.OAuthStateSigningKey = "state-key"
	recheckErr := errors.New("source repository unavailable during recheck")
	tests := []struct {
		name        string
		change      func(*models.ConnectedSource)
		cancelRead  string
		wantErr     error
		wantFailure bool
	}{
		{name: "current replacement grant"},
		{name: "cancel initial source read", cancelRead: "source", wantErr: context.Canceled},
		{name: "cancel token read", cancelRead: "token", wantErr: context.Canceled},
		{name: "cancel source recheck", cancelRead: "recheck", wantErr: context.Canceled},
		{name: "paused during token read", change: func(s *models.ConnectedSource) { s.Status = "paused" }, wantErr: errGoogleOAuthSourceInactive},
		{name: "disabled during token read", change: func(s *models.ConnectedSource) { s.Enabled = false }, wantErr: errGoogleOAuthSourceInactive},
		{name: "revoked during token read", change: func(s *models.ConnectedSource) { s.Status = "revoked" }, wantErr: ErrSourceRevoked},
		{name: "reconnect required during token read", change: func(s *models.ConnectedSource) { s.Status = "reconnect_required" }, wantErr: errGoogleOAuthReconnectRequired},
		{name: "owner replaced during token read", change: func(s *models.ConnectedSource) { s.OwnerIdentity = "bob@example.test" }, wantFailure: true},
		{name: "connector replaced during token read", change: func(s *models.ConnectedSource) { s.ConnectorKey = driveConnectorKey }, wantFailure: true},
		{name: "source recheck repository failure", wantErr: recheckErr},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			sourceID := uuid.New()
			base := newFakeSourceRepo(&models.ConnectedSource{
				ID: sourceID, OwnerIdentity: "alice@example.test", ConnectorKey: gmailConnectorKey, Enabled: true, Status: "active",
			})
			if err := base.SaveOAuthToken(googleOAuthTestStoredGrant(t, sourceID, googleoauth.GmailReadonlyScope)); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			repo := &googleOAuthReuseRaceRepository{Repository: base}
			repo.onSourceRead = func(read int) {
				if test.cancelRead == "source" && read == 1 || test.cancelRead == "recheck" && read == 2 {
					cancel()
				}
			}
			repo.afterTokenRead = func() {
				if test.cancelRead == "token" {
					cancel()
				}
				if test.change != nil {
					test.change(base.sources[sourceID])
				}
			}
			if test.wantErr == recheckErr {
				repo.recheckErr = recheckErr
			}
			refreshes := 0
			access, err := NewService(repo, nil).(*service).forceGoogleAccessTokenRefreshWith(ctx, sourceID, gmailConnectorKey, "rejected-access", func(context.Context, string) (*googleoauth.Token, error) {
				refreshes++
				return nil, errors.New("replacement-token reuse must not call the provider")
			})
			if test.wantErr != nil || test.wantFailure {
				if err == nil || access != "" || test.wantErr != nil && !errors.Is(err, test.wantErr) {
					t.Fatalf("reuse access/error = %q/%v, want failure %v", access, err, test.wantErr)
				}
			} else if err != nil || access != "replacement-access" {
				t.Fatalf("reuse access/error = %q/%v", access, err)
			}
			if refreshes != 0 {
				t.Fatalf("provider refreshes = %d, want 0", refreshes)
			}
		})
	}
}

func TestGoogleConnectionHealthRejectsUnusableGrants(t *testing.T) {
	previous := config.AppConfig
	t.Cleanup(func() { config.AppConfig = previous })
	config.AppConfig.GoogleOAuthClientID = "client"
	config.AppConfig.GoogleOAuthClientSecret = "secret"
	config.AppConfig.GoogleOAuthRedirectURL = "https://example.test/callback"
	config.AppConfig.OAuthTokenEncryptionKey = "token-key"
	config.AppConfig.OAuthStateSigningKey = "state-key"
	tests := []struct {
		name   string
		change func(*models.ConnectedSource, *models.SourceOAuthToken)
		status string
	}{
		{name: "usable stored grant", status: "ready"},
		{name: "ownerless source", status: "configuration_required", change: func(s *models.ConnectedSource, _ *models.SourceOAuthToken) { s.OwnerIdentity = " " }},
		{name: "missing access credential", change: func(_ *models.ConnectedSource, t *models.SourceOAuthToken) { t.AccessToken = nil }},
		{name: "missing refresh credential", change: func(_ *models.ConnectedSource, t *models.SourceOAuthToken) { t.RefreshToken = nil }},
		{name: "wrong provider", change: func(_ *models.ConnectedSource, t *models.SourceOAuthToken) { t.Provider = "other" }},
		{name: "foreign source grant", change: func(_ *models.ConnectedSource, t *models.SourceOAuthToken) { t.SourceID = uuid.New() }},
		{name: "missing scope", change: func(_ *models.ConnectedSource, t *models.SourceOAuthToken) { t.Scope = "" }},
		{name: "wrong scope", change: func(_ *models.ConnectedSource, t *models.SourceOAuthToken) { t.Scope = googleoauth.DriveReadonlyScope }},
		{name: "broader scope", change: func(_ *models.ConnectedSource, t *models.SourceOAuthToken) {
			t.Scope += " " + googleoauth.DriveReadonlyScope
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source := models.ConnectedSource{ID: uuid.New(), OwnerIdentity: "alice@example.test", ConnectorKey: gmailConnectorKey, Enabled: true, Status: "active"}
			grant := googleOAuthTestStoredGrant(t, source.ID, googleoauth.GmailReadonlyScope)
			if test.change != nil {
				test.change(&source, grant)
			}
			health, err := NewService(newFakeSourceRepo(&source), nil).(*service).connectionHealthForSourceWithToken(source, grant)
			wantStatus := test.status
			if wantStatus == "" {
				wantStatus = "reconnect_required"
			}
			if err != nil || health.Status != wantStatus || health.Authorized != (wantStatus == "ready") || health.RequiresReconnect != (wantStatus == "reconnect_required") {
				t.Fatalf("health/error = %#v/%v, want %s", health, err, wantStatus)
			}
		})
	}
}
