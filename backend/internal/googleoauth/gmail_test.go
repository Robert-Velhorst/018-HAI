package googleoauth

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
)

// mockGmail serves the two Gmail endpoints the client uses, and asserts the
// bearer token is presented.
func mockGmail(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-access-token" {
			t.Errorf("Authorization = %q, want bearer test-access-token", got)
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/users/me/messages/"):
			id := strings.TrimPrefix(r.URL.Path, "/users/me/messages/")
			_, _ = w.Write([]byte(`{
				"id":"` + id + `",
				"snippet":"Hello from message ` + id + `",
				"internalDate":"1700000000000",
				"payload":{"headers":[
					{"name":"From","value":"alice@example.com"},
					{"name":"Subject","value":"Subject of ` + id + `"},
					{"name":"Date","value":"Tue, 14 Nov 2023 22:13:20 +0000"}
				]}
			}`))
		case strings.HasSuffix(r.URL.Path, "/users/me/messages"):
			_, _ = w.Write([]byte(`{"messages":[{"id":"m1"},{"id":"m2"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestGmailHistoryUsesNativeCursorAndDeduplicatesMessages(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/users/me/history" {
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get("startHistoryId") != "100" || !sameStringSet(r.URL.Query()["historyTypes"], []string{"messageAdded", "messageDeleted", "labelAdded", "labelRemoved"}) {
			t.Errorf("history query = %s", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(`{"historyId":"105","history":[{"id":"101","messagesAdded":[{"message":{"id":"m1"}},{"message":{"id":"m1"}},{"message":{"id":"m2"}}]}]}`))
	}))
	defer server.Close()

	page, err := (GmailClient{AccessToken: "token", BaseURL: server.URL}).ListHistoryPage(context.Background(), "100", "", 50)
	if err != nil || page.HistoryID != "105" || len(page.MessageIDs) != 2 || page.MessageIDs[0] != "m1" || page.MessageIDs[1] != "m2" {
		t.Fatalf("ListHistoryPage = %#v, %v", page, err)
	}
}

func TestGmailHistoryReturnsDeletionsAndLabelMutations(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"historyId":"205","history":[
			{"id":"201","messagesDeleted":[{"message":{"id":"deleted-1","threadId":"thread-1"}}]},
			{"id":"202","labelsAdded":[{"message":{"id":"message-2","threadId":"thread-2"},"labelIds":["STARRED","INBOX","STARRED"]},{"message":{"id":"message-2","threadId":"thread-2"},"labelIds":["INBOX","STARRED"]}]},
			{"id":"203","labelsRemoved":[{"message":{"id":"message-3"},"labelIds":["IMPORTANT"]}]}
		]}`))
	}))
	defer server.Close()

	page, err := (GmailClient{AccessToken: "token", BaseURL: server.URL}).ListHistoryPage(context.Background(), "200", "", 25)
	if err != nil {
		t.Fatalf("ListHistoryPage: %v", err)
	}
	if page.HistoryID != "205" || len(page.MessageIDs) != 0 || len(page.Changes) != 3 {
		t.Fatalf("history page = %#v", page)
	}
	want := []GmailHistoryChange{
		{HistoryID: "201", MessageID: "deleted-1", ThreadID: "thread-1", Type: "message_deleted"},
		{HistoryID: "202", MessageID: "message-2", ThreadID: "thread-2", Type: "labels_added", LabelIDs: []string{"INBOX", "STARRED"}},
		{HistoryID: "203", MessageID: "message-3", Type: "labels_removed", LabelIDs: []string{"IMPORTANT"}},
	}
	for i := range want {
		if got := page.Changes[i]; got.HistoryID != want[i].HistoryID || got.MessageID != want[i].MessageID ||
			got.ThreadID != want[i].ThreadID || got.Type != want[i].Type || strings.Join(got.LabelIDs, ",") != strings.Join(want[i].LabelIDs, ",") {
			t.Errorf("change[%d] = %#v, want %#v", i, got, want[i])
		}
	}
}

func TestGmailHistoryRejectsMutationWithoutEntryID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"historyId":"205","history":[{"messagesDeleted":[{"message":{"id":"m1"}}]}]}`))
	}))
	defer server.Close()

	_, err := (GmailClient{AccessToken: "token", BaseURL: server.URL}).ListHistoryPage(context.Background(), "200", "", 25)
	if err == nil || !strings.Contains(err.Error(), "no history id") {
		t.Fatalf("ListHistoryPage error = %v, want missing event history ID to fail closed", err)
	}
}

func sameStringSet(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	seen := make(map[string]bool, len(got))
	for _, value := range got {
		seen[value] = true
	}
	for _, value := range want {
		if !seen[value] {
			return false
		}
	}
	return true
}

func TestGmailExpiredHistoryRequiresFullSync(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"code":404}}`))
	}))
	defer server.Close()

	_, err := (GmailClient{AccessToken: "token", BaseURL: server.URL}).ListHistoryPage(context.Background(), "999", "", 50)
	if !errors.Is(err, ErrHistoryCursorExpired) {
		t.Fatalf("error = %v, want ErrHistoryCursorExpired", err)
	}
}

func TestGmailRejectsMalformedHistoryIDsBeforeRequest(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		_, _ = w.Write([]byte(`{"historyId":"not-numeric"}`))
	}))
	defer server.Close()

	client := GmailClient{AccessToken: "token", BaseURL: server.URL}
	if _, err := client.GetProfileHistoryID(context.Background()); err == nil {
		t.Fatal("malformed profile history ID was accepted")
	}
	if _, err := client.ListHistoryPage(context.Background(), "bad-cursor", "", 50); err == nil {
		t.Fatal("malformed history cursor was sent to the provider")
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want only profile request; invalid local cursor must fail before network", requests)
	}
}

func TestGmailProviderErrorPreservesRetryMetadataWithoutBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "23")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":"private provider response"}`))
	}))
	defer server.Close()

	_, err := (GmailClient{AccessToken: "token", BaseURL: server.URL}).GetProfileHistoryID(context.Background())
	var apiErr *ProviderAPIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusTooManyRequests || apiErr.RetryAfterHeader != "23" {
		t.Fatalf("error = %#v; want typed 429 with Retry-After metadata", err)
	}
	if strings.Contains(err.Error(), "private provider response") {
		t.Fatalf("provider body leaked through error: %v", err)
	}
}

func TestGmailMessageListRejectsMissingStableID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"messages":[{"id":"m1"},{}],"nextPageToken":"next"}`))
	}))
	defer server.Close()

	page, err := (GmailClient{AccessToken: "token", BaseURL: server.URL}).ListMessageIDsPage(context.Background(), 50, "", "")
	if err == nil || len(page.IDs) != 0 || page.NextPageToken != "" {
		t.Fatalf("page=%#v err=%v; malformed list entries must not be silently skipped", page, err)
	}
}

func TestGmailExtractsBodyAndBoundedTextAttachment(t *testing.T) {
	body := base64.RawURLEncoding.EncodeToString([]byte("Please prepare the evidence bundle."))
	attachment := base64.RawURLEncoding.EncodeToString([]byte("Decision: use the signed version."))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/users/me/messages/m1":
			_, _ = w.Write([]byte(`{"id":"m1","threadId":"t1","historyId":"101","payload":{"headers":[{"name":"From","value":"lawyer@example.com"},{"name":"To","value":"robert@example.com"},{"name":"Subject","value":"Evidence"}],"parts":[{"mimeType":"text/plain","body":{"data":"` + body + `"}},{"mimeType":"text/plain","filename":"decision.txt","body":{"attachmentId":"a1","size":33}}]}}`))
		case "/users/me/messages/m1/attachments/a1":
			_, _ = w.Write([]byte(`{"size":33,"data":"` + attachment + `"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	message, err := (GmailClient{AccessToken: "token", BaseURL: server.URL}).GetMessageMetadata(context.Background(), "m1")
	if err != nil || !strings.Contains(message.Body, "evidence bundle") || len(message.Attachments) != 1 || !message.Attachments[0].Fetched || !strings.Contains(message.Attachments[0].Content, "signed version") {
		t.Fatalf("message = %#v, err=%v", message, err)
	}
}

func TestGmailMarksTruncatedBodyAndPreservesUTF8(t *testing.T) {
	encodedBody := base64.RawURLEncoding.EncodeToString([]byte(strings.Repeat("🙂", 140000)))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/users/me/messages/m1" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"m1","payload":{"mimeType":"text/plain","body":{"data":"` + encodedBody + `"}}}`))
	}))
	defer server.Close()

	message, err := (GmailClient{AccessToken: "token", BaseURL: server.URL}).GetMessageMetadata(context.Background(), "m1")
	if err != nil {
		t.Fatalf("GetMessageMetadata: %v", err)
	}
	if !message.BodyTruncated || message.BodyLimitBytes != maxGmailBodyBytes {
		t.Fatalf("truncation metadata = truncated:%v limit:%d", message.BodyTruncated, message.BodyLimitBytes)
	}
	if len(message.Body) > maxGmailBodyBytes+3 || !utf8.ValidString(message.Body) {
		t.Fatalf("body length=%d validUTF8=%v; truncation must stay bounded and preserve UTF-8", len(message.Body), utf8.ValidString(message.Body))
	}
}

func TestGmailFlagsMalformedBodyEncodingAndDecodesDeclaredLegacyCharset(t *testing.T) {
	invalidUTF8 := base64.RawURLEncoding.EncodeToString([]byte{'o', 'k', 0xff, 'x'})
	latin1 := base64.RawURLEncoding.EncodeToString([]byte{'c', 'a', 'f', 0xe9})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var response string
		switch r.URL.Path {
		case "/users/me/messages/malformed":
			response = `{"id":"malformed","payload":{"mimeType":"text/plain","body":{"data":"` + invalidUTF8 + `"}}}`
		case "/users/me/messages/latin1":
			response = `{"id":"latin1","payload":{"mimeType":"text/plain; charset=ISO-8859-1","body":{"data":"` + latin1 + `"}}}`
		default:
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(response))
	}))
	defer server.Close()

	client := GmailClient{AccessToken: "token", BaseURL: server.URL}
	malformed, err := client.GetMessageMetadata(context.Background(), "malformed")
	if err != nil || !malformed.BodyEncodingWarning || !utf8.ValidString(malformed.Body) {
		t.Fatalf("malformed UTF-8 body = %#v, err=%v; replacement decoding must be reviewable", malformed, err)
	}
	decoded, err := client.GetMessageMetadata(context.Background(), "latin1")
	if err != nil || decoded.BodyEncodingWarning || decoded.Body != "café" {
		t.Fatalf("declared ISO-8859-1 body = %#v, err=%v; valid declared text must decode correctly", decoded, err)
	}
}

func TestGmailBoundsAttachmentRecordsAndTotalExtractedText(t *testing.T) {
	attachmentData := base64.RawURLEncoding.EncodeToString([]byte(strings.Repeat("a", maxGmailAttachmentBytes)))
	parts := make([]gmailMessagePart, 0, maxGmailAttachmentRecords+1)
	for index := 0; index < maxGmailAttachmentRecords+1; index++ {
		part := gmailMessagePart{MimeType: "text/plain", Filename: "attachment-" + strconv.Itoa(index) + ".txt"}
		part.Body.Size = maxGmailAttachmentBytes
		part.Body.Data = attachmentData
		parts = append(parts, part)
	}
	attachments := []GmailAttachment{}
	plain, html := []string{}, []string{}
	budget := maxGmailAttachmentContentTotalBytes
	omitted := 0
	bodyEncodingWarning := false
	if err := (GmailClient{}).collectPart(context.Background(), "m1", gmailMessagePart{Parts: parts}, &plain, &html, &attachments, &budget, &omitted, &bodyEncodingWarning); err != nil {
		t.Fatal(err)
	}
	if len(attachments) != maxGmailAttachmentRecords {
		t.Fatalf("attachment records = %d, want %d", len(attachments), maxGmailAttachmentRecords)
	}
	if omitted != 1 {
		t.Fatalf("omitted attachment count = %d, want 1", omitted)
	}
	extractedBytes := 0
	for _, attachment := range attachments {
		extractedBytes += len(attachment.Content)
	}
	if extractedBytes > maxGmailAttachmentContentTotalBytes {
		t.Fatalf("extracted attachment bytes = %d, exceeds %d", extractedBytes, maxGmailAttachmentContentTotalBytes)
	}
}

func TestGmailTextAttachmentWithMalformedUTF8IsMarkedForReview(t *testing.T) {
	data := base64.RawURLEncoding.EncodeToString([]byte{'o', 'k', 0xff})
	part := gmailMessagePart{MimeType: "text/plain", Filename: "note.txt"}
	part.Body.Size = 3
	part.Body.Data = data
	attachments := []GmailAttachment{}
	plain, html := []string{}, []string{}
	budget := maxGmailAttachmentContentTotalBytes
	omitted := 0
	bodyEncodingWarning := false
	if err := (GmailClient{}).collectPart(context.Background(), "m1", part, &plain, &html, &attachments, &budget, &omitted, &bodyEncodingWarning); err != nil {
		t.Fatal(err)
	}
	if len(attachments) != 1 || !attachments[0].Fetched || attachments[0].ContentStatus != "invalid_encoding" || !utf8.ValidString(attachments[0].Content) {
		t.Fatalf("malformed text attachment = %#v; content must be preserved as safe UTF-8 and review-marked", attachments)
	}
}

func TestGmailFetchRecentParsesHeadersAndSnippet(t *testing.T) {
	srv := mockGmail(t)
	defer srv.Close()

	client := GmailClient{AccessToken: "test-access-token", BaseURL: srv.URL}
	msgs, err := client.FetchRecent(context.Background(), 10, "")
	if err != nil {
		t.Fatalf("FetchRecent: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("got %d messages, want 2", len(msgs))
	}
	first := msgs[0]
	if first.ID != "m1" {
		t.Errorf("id = %q, want m1", first.ID)
	}
	if first.From != "alice@example.com" {
		t.Errorf("from = %q", first.From)
	}
	if first.Subject != "Subject of m1" {
		t.Errorf("subject = %q", first.Subject)
	}
	if first.Snippet == "" {
		t.Error("snippet should be populated")
	}
	if first.Date.IsZero() {
		t.Error("date should be parsed from internalDate")
	}
}

func TestGmailSurfacesExpiredToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"code":401}}`))
	}))
	defer srv.Close()

	client := GmailClient{AccessToken: "stale", BaseURL: srv.URL}
	_, err := client.ListRecentMessageIDs(context.Background(), 5, "")
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("expected a clear 401/expired error, got %v", err)
	}
}

func TestGmailRejectsResponseOverSafetyLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"payload":"` + strings.Repeat("x", maxGmailResponseBytes) + `"}`))
	}))
	defer server.Close()

	err := (GmailClient{AccessToken: "token", BaseURL: server.URL}).getJSON(context.Background(), "/users/me/profile", &struct{}{})
	if err == nil || !strings.Contains(err.Error(), "response exceeded") {
		t.Fatalf("getJSON error = %v, want explicit response safety limit", err)
	}
}

func TestGmailFetchRecentFailsPageOnTransientMessageError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/users/me/messages"):
			_, _ = w.Write([]byte(`{"messages":[{"id":"ok"},{"id":"bad"}]}`))
		case strings.HasSuffix(r.URL.Path, "/messages/ok"):
			_, _ = w.Write([]byte(`{"id":"ok","snippet":"fine","payload":{"headers":[]}}`))
		default:
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	client := GmailClient{AccessToken: "t", BaseURL: srv.URL}
	msgs, err := client.FetchRecent(context.Background(), 10, "")
	if err == nil || !strings.Contains(err.Error(), "HTTP 500") {
		t.Fatalf("FetchRecent = %#v, %v; want transient message error", msgs, err)
	}
	if len(msgs) != 0 {
		t.Fatalf("partial page escaped after transient message error: %+v", msgs)
	}
}

func TestGmailFetchRecentSkipsPermanentlyUnavailableMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/users/me/messages"):
			_, _ = w.Write([]byte(`{"messages":[{"id":"ok"},{"id":"gone"}]}`))
		case strings.HasSuffix(r.URL.Path, "/messages/ok"):
			_, _ = w.Write([]byte(`{"id":"ok","snippet":"fine","payload":{"headers":[]}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	client := GmailClient{AccessToken: "t", BaseURL: srv.URL}
	msgs, err := client.FetchRecent(context.Background(), 10, "")
	if err != nil {
		t.Fatalf("FetchRecent failed for a permanently unavailable message: %v", err)
	}
	if len(msgs) != 2 || msgs[0].ID != "ok" || msgs[1].ID != "gone" || !msgs[1].Unavailable {
		t.Fatalf("permanent disappearance must remain observable, got %+v", msgs)
	}
}

func TestGmailFetchRecentFailsOnTransientAttachmentError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/users/me/messages":
			_, _ = w.Write([]byte(`{"messages":[{"id":"m1"}]}`))
		case r.URL.Path == "/users/me/messages/m1":
			_, _ = w.Write([]byte(`{"id":"m1","payload":{"parts":[{"mimeType":"text/plain","filename":"note.txt","body":{"attachmentId":"a1","size":12}}]}}`))
		case r.URL.Path == "/users/me/messages/m1/attachments/a1":
			w.WriteHeader(http.StatusServiceUnavailable)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	messages, err := (GmailClient{AccessToken: "token", BaseURL: server.URL}).FetchRecent(context.Background(), 10, "")
	if err == nil || !strings.Contains(err.Error(), "503") || len(messages) != 0 {
		t.Fatalf("FetchRecent = %#v, %v; transient attachment failures must fail the full page", messages, err)
	}
}

func TestGmailPermanentAttachmentOutcomesAreExplicit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/users/me/messages/m1":
			_, _ = w.Write([]byte(`{"id":"m1","payload":{"parts":[{"mimeType":"application/pdf","filename":"evidence.pdf","body":{"size":42}},{"mimeType":"text/plain","filename":"gone.txt","body":{"attachmentId":"a1","size":12}}]}}`))
		case "/users/me/messages/m1/attachments/a1":
			w.WriteHeader(http.StatusNotFound)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	message, err := (GmailClient{AccessToken: "token", BaseURL: server.URL}).GetMessageMetadata(context.Background(), "m1")
	if err != nil {
		t.Fatalf("GetMessageMetadata: %v", err)
	}
	if len(message.Attachments) != 2 || message.Attachments[0].ContentStatus != "unsupported_mime" || message.Attachments[1].ContentStatus != "unavailable" {
		t.Fatalf("attachment outcomes were not explicit: %+v", message.Attachments)
	}
}

func TestGmailMalformedMessageBodyFailsHydration(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"m1","payload":{"mimeType":"text/plain","body":{"data":"%%%"}}}`))
	}))
	defer server.Close()

	if _, err := (GmailClient{AccessToken: "token", BaseURL: server.URL}).GetMessageMetadata(context.Background(), "m1"); err == nil {
		t.Fatal("malformed message body must fail hydration instead of silently importing incomplete content")
	}
}

func TestGmailOversizedMessageIsClassifiedWithoutRetryingPage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/users/me/messages/m1" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(strings.Repeat("x", maxGmailResponseBytes+1)))
	}))
	defer server.Close()

	message, err := (GmailClient{AccessToken: "token", BaseURL: server.URL}).GetMessageMetadata(context.Background(), "m1")
	if err != nil || message.ID != "m1" || message.ContentStatus != "size_limit" {
		t.Fatalf("oversized message = %+v, %v; want a stable-ID size-limit result", message, err)
	}
}

func TestGmailOversizedTransientErrorRemainsRetryable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(strings.Repeat("x", maxGmailResponseBytes+1)))
	}))
	defer server.Close()

	_, err := (GmailClient{AccessToken: "token", BaseURL: server.URL}).GetMessageMetadata(context.Background(), "m1")
	var apiErr *ProviderAPIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("oversized transient response error = %v; want provider HTTP 503, not a permanent size-limit result", err)
	}
}
