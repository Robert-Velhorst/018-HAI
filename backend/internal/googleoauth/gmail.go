package googleoauth

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	xhtml "golang.org/x/net/html"
	"golang.org/x/net/html/charset"
)

// DefaultGmailBaseURL is the Gmail REST API root; tests override it.
const DefaultGmailBaseURL = "https://gmail.googleapis.com/gmail/v1"

const (
	maxGmailBodyBytes                   = 512 << 10
	maxGmailAttachmentBytes             = 512 << 10
	maxGmailAttachmentRecords           = 20
	maxGmailAttachmentContentTotalBytes = 1 << 20
	maxGmailResponseBytes               = 8 << 20
)

var (
	ErrHistoryCursorExpired  = errors.New("gmail history cursor expired")
	ErrMessageUnavailable    = errors.New("gmail message is no longer available")
	ErrAttachmentUnavailable = errors.New("gmail attachment is no longer available")
	ErrAttachmentUnsupported = errors.New("gmail attachment exceeds the extraction safety limit")
	ErrGmailResponseTooLarge = errors.New("gmail response exceeds the extraction safety limit")
)

// GmailClient reads a mailbox over the Gmail REST API with a bearer access
// token. Message and attachment content is bounded to protect memory and token
// budgets; non-text attachments remain metadata-only.
type GmailClient struct {
	AccessToken string
	BaseURL     string
	HTTPClient  *http.Client
}

func (g GmailClient) baseURL() string {
	if strings.TrimSpace(g.BaseURL) != "" {
		return strings.TrimRight(g.BaseURL, "/")
	}
	return DefaultGmailBaseURL
}

func (g GmailClient) httpClient() *http.Client {
	if g.HTTPClient != nil {
		return g.HTTPClient
	}
	return &http.Client{Timeout: 20 * time.Second}
}

// GmailMessage is the metadata this connector ingests for one message.
type GmailMessage struct {
	ID                 string
	ThreadID           string
	HistoryID          string
	From               string
	To                 string
	Subject            string
	Date               time.Time
	Snippet            string
	Body               string
	BodyTruncated      bool
	BodyLimitBytes     int64
	BodyEncodingWarning bool
	Attachments        []GmailAttachment
	AttachmentsOmitted int
	Unavailable        bool
	ContentStatus      string
	ContentLimitBytes  int64
}

type GmailAttachment struct {
	Filename      string
	MimeType      string
	Size          int64
	Content       string
	Fetched       bool
	ContentStatus string
}

type messageListResponse struct {
	Messages []struct {
		ID string `json:"id"`
	} `json:"messages"`
	NextPageToken string `json:"nextPageToken"`
}

type gmailMessagePart struct {
	PartID   string `json:"partId"`
	MimeType string `json:"mimeType"`
	Filename string `json:"filename"`
	Headers  []struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	} `json:"headers"`
	Body struct {
		AttachmentID string `json:"attachmentId"`
		Size         int64  `json:"size"`
		Data         string `json:"data"`
	} `json:"body"`
	Parts []gmailMessagePart `json:"parts"`
}

type messageResponse struct {
	ID           string           `json:"id"`
	ThreadID     string           `json:"threadId"`
	HistoryID    string           `json:"historyId"`
	Snippet      string           `json:"snippet"`
	InternalDate string           `json:"internalDate"`
	Payload      gmailMessagePart `json:"payload"`
}

type GmailMessageIDPage struct {
	IDs           []string
	NextPageToken string
}

type GmailHistoryPage struct {
	MessageIDs    []string
	Changes       []GmailHistoryChange
	NextPageToken string
	HistoryID     string
}

// GmailHistoryChange is an immutable provider history event. Message content
// is intentionally not hydrated for deletions or label mutations.
type GmailHistoryChange struct {
	HistoryID string
	MessageID string
	ThreadID  string
	Type      string
	LabelIDs  []string
}

func (g GmailClient) GetProfileHistoryID(ctx context.Context) (string, error) {
	var response struct {
		HistoryID string `json:"historyId"`
	}
	if err := g.getJSON(ctx, "/users/me/profile", &response); err != nil {
		return "", err
	}
	if strings.TrimSpace(response.HistoryID) == "" {
		return "", fmt.Errorf("gmail returned no profile historyId")
	}
	if !validGmailHistoryID(response.HistoryID) {
		return "", fmt.Errorf("gmail returned a malformed profile historyId")
	}
	return response.HistoryID, nil
}

func (g GmailClient) ListMessageIDsPage(ctx context.Context, maxResults int, query, pageToken string) (GmailMessageIDPage, error) {
	if maxResults <= 0 || maxResults > 500 {
		maxResults = 50
	}
	q := url.Values{}
	q.Set("maxResults", strconv.Itoa(maxResults))
	if trimmed := strings.TrimSpace(query); trimmed != "" {
		q.Set("q", trimmed)
	}
	if strings.TrimSpace(pageToken) != "" {
		q.Set("pageToken", pageToken)
	}
	var parsed messageListResponse
	if err := g.getJSON(ctx, "/users/me/messages?"+q.Encode(), &parsed); err != nil {
		return GmailMessageIDPage{}, err
	}
	page := GmailMessageIDPage{NextPageToken: parsed.NextPageToken, IDs: make([]string, 0, len(parsed.Messages))}
	for _, message := range parsed.Messages {
		id := strings.TrimSpace(message.ID)
		if id == "" {
			return GmailMessageIDPage{}, fmt.Errorf("gmail message list contains an entry without a stable message id")
		}
		page.IDs = append(page.IDs, id)
	}
	if page.NextPageToken != "" && page.NextPageToken == strings.TrimSpace(pageToken) {
		return GmailMessageIDPage{}, fmt.Errorf("gmail message list repeated its page token")
	}
	return page, nil
}

func (g GmailClient) ListHistoryPage(ctx context.Context, startHistoryID, pageToken string, maxResults int) (GmailHistoryPage, error) {
	startHistoryID = strings.TrimSpace(startHistoryID)
	if !validGmailHistoryID(startHistoryID) {
		return GmailHistoryPage{}, fmt.Errorf("gmail startHistoryId must be a decimal history ID")
	}
	if maxResults <= 0 || maxResults > 500 {
		maxResults = 100
	}
	q := url.Values{}
	q.Set("startHistoryId", startHistoryID)
	q["historyTypes"] = []string{"messageAdded", "messageDeleted", "labelAdded", "labelRemoved"}
	q.Set("maxResults", strconv.Itoa(maxResults))
	if strings.TrimSpace(pageToken) != "" {
		q.Set("pageToken", pageToken)
	}
	var response struct {
		History []struct {
			ID            string `json:"id"`
			MessagesAdded []struct {
				Message struct {
					ID       string `json:"id"`
					ThreadID string `json:"threadId"`
				} `json:"message"`
			} `json:"messagesAdded"`
			MessagesDeleted []struct {
				Message struct {
					ID       string `json:"id"`
					ThreadID string `json:"threadId"`
				} `json:"message"`
			} `json:"messagesDeleted"`
			LabelsAdded []struct {
				Message struct {
					ID       string `json:"id"`
					ThreadID string `json:"threadId"`
				} `json:"message"`
				LabelIDs []string `json:"labelIds"`
			} `json:"labelsAdded"`
			LabelsRemoved []struct {
				Message struct {
					ID       string `json:"id"`
					ThreadID string `json:"threadId"`
				} `json:"message"`
				LabelIDs []string `json:"labelIds"`
			} `json:"labelsRemoved"`
		} `json:"history"`
		NextPageToken string `json:"nextPageToken"`
		HistoryID     string `json:"historyId"`
	}
	if err := g.getJSON(ctx, "/users/me/history?"+q.Encode(), &response); err != nil {
		var apiErr *ProviderAPIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
			return GmailHistoryPage{}, fmt.Errorf("%w: %w", ErrHistoryCursorExpired, err)
		}
		return GmailHistoryPage{}, err
	}
	if strings.TrimSpace(response.HistoryID) == "" {
		return GmailHistoryPage{}, fmt.Errorf("gmail history response returned no historyId")
	}
	if response.NextPageToken != "" && response.NextPageToken == strings.TrimSpace(pageToken) {
		return GmailHistoryPage{}, fmt.Errorf("gmail history response repeated its page token")
	}
	seen := map[string]bool{}
	seenChanges := map[string]bool{}
	page := GmailHistoryPage{NextPageToken: response.NextPageToken, HistoryID: response.HistoryID}
	appendChange := func(historyID, messageID, threadID, changeType string, labelIDs []string) error {
		historyID = strings.TrimSpace(historyID)
		messageID = strings.TrimSpace(messageID)
		if messageID == "" {
			return fmt.Errorf("gmail history entry %s contains a %s event without a message id", historyID, changeType)
		}
		if historyID == "" {
			return fmt.Errorf("gmail history entry for message %s returned no history id", messageID)
		}
		labelIDs = normalizedGmailLabels(labelIDs)
		if (changeType == "labels_added" || changeType == "labels_removed") && len(labelIDs) == 0 {
			return fmt.Errorf("gmail history entry %s contains a %s event without label ids", historyID, changeType)
		}
		key, _ := json.Marshal(struct {
			HistoryID string
			MessageID string
			Type      string
			LabelIDs  []string
		}{historyID, messageID, changeType, labelIDs})
		if seenChanges[string(key)] {
			return nil
		}
		seenChanges[string(key)] = true
		page.Changes = append(page.Changes, GmailHistoryChange{
			HistoryID: historyID,
			MessageID: messageID,
			ThreadID:  strings.TrimSpace(threadID),
			Type:      changeType,
			LabelIDs:  labelIDs,
		})
		return nil
	}
	for _, history := range response.History {
		for _, added := range history.MessagesAdded {
			id := strings.TrimSpace(added.Message.ID)
			if id != "" && !seen[id] {
				seen[id] = true
				page.MessageIDs = append(page.MessageIDs, id)
			}
			if err := appendChange(history.ID, id, added.Message.ThreadID, "message_added", nil); err != nil {
				return GmailHistoryPage{}, err
			}
		}
		for _, deleted := range history.MessagesDeleted {
			if err := appendChange(history.ID, deleted.Message.ID, deleted.Message.ThreadID, "message_deleted", nil); err != nil {
				return GmailHistoryPage{}, err
			}
		}
		for _, added := range history.LabelsAdded {
			if err := appendChange(history.ID, added.Message.ID, added.Message.ThreadID, "labels_added", added.LabelIDs); err != nil {
				return GmailHistoryPage{}, err
			}
		}
		for _, removed := range history.LabelsRemoved {
			if err := appendChange(history.ID, removed.Message.ID, removed.Message.ThreadID, "labels_removed", removed.LabelIDs); err != nil {
				return GmailHistoryPage{}, err
			}
		}
	}
	return page, nil
}

func normalizedGmailLabels(labels []string) []string {
	seen := make(map[string]bool, len(labels))
	normalized := make([]string, 0, len(labels))
	for _, label := range labels {
		label = strings.TrimSpace(label)
		if label != "" && !seen[label] {
			seen[label] = true
			normalized = append(normalized, label)
		}
	}
	sort.Strings(normalized)
	return normalized
}

func validGmailHistoryID(value string) bool {
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

// ListRecentMessageIDs returns up to maxResults recent message IDs, newest
// first (Gmail's default order). A non-empty query is passed straight to
// Gmail's `q` search parameter, which is how incremental sync narrows the fetch
// to mail that arrived after the last cursor (e.g. "after:1750000000").
func (g GmailClient) ListRecentMessageIDs(ctx context.Context, maxResults int, query string) ([]string, error) {
	page, err := g.ListMessageIDsPage(ctx, maxResults, query, "")
	if err != nil {
		return nil, err
	}
	return page.IDs, nil
}

// GetMessageMetadata fetches one message's headers, bounded body text, and
// bounded textual attachments. The name is retained for API compatibility.
func (g GmailClient) GetMessageMetadata(ctx context.Context, id string) (GmailMessage, error) {
	path := "/users/me/messages/" + url.PathEscape(id) +
		"?format=full"
	var parsed messageResponse
	if err := g.getJSON(ctx, path, &parsed); err != nil {
		var apiErr *ProviderAPIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
			return GmailMessage{}, fmt.Errorf("%w: %w", ErrMessageUnavailable, err)
		}
		if errors.Is(err, ErrGmailResponseTooLarge) {
			return GmailMessage{ID: id, ContentStatus: "size_limit", ContentLimitBytes: maxGmailResponseBytes}, nil
		}
		return GmailMessage{}, err
	}
	msg := GmailMessage{ID: parsed.ID, ThreadID: parsed.ThreadID, HistoryID: parsed.HistoryID, Snippet: parsed.Snippet}
	for _, h := range parsed.Payload.Headers {
		switch strings.ToLower(h.Name) {
		case "from":
			msg.From = h.Value
		case "to":
			msg.To = h.Value
		case "subject":
			msg.Subject = h.Value
		}
	}
	if ms, err := strconv.ParseInt(parsed.InternalDate, 10, 64); err == nil && ms > 0 {
		msg.Date = time.UnixMilli(ms).UTC()
	}
	plain, htmlBody := []string{}, []string{}
	attachments := []GmailAttachment{}
	omittedAttachments := 0
	bodyEncodingWarning := false
	attachmentContentBudget := maxGmailAttachmentContentTotalBytes
	if err := g.collectPart(ctx, parsed.ID, parsed.Payload, &plain, &htmlBody, &attachments, &attachmentContentBudget, &omittedAttachments, &bodyEncodingWarning); err != nil {
		return GmailMessage{}, err
	}
	msg.Body = strings.TrimSpace(strings.Join(plain, "\n\n"))
	if msg.Body == "" {
		msg.Body = strings.TrimSpace(strings.Join(htmlBody, "\n\n"))
	}
	msg.Body, msg.BodyTruncated = truncateTextWithStatus(msg.Body, maxGmailBodyBytes)
	msg.BodyEncodingWarning = bodyEncodingWarning
	if msg.BodyTruncated {
		msg.BodyLimitBytes = maxGmailBodyBytes
	}
	msg.Attachments = attachments
	msg.AttachmentsOmitted = omittedAttachments
	return msg, nil
}

func (g GmailClient) FetchMessageIDs(ctx context.Context, ids []string) ([]GmailMessage, error) {
	out := make([]GmailMessage, 0, len(ids))
	for _, id := range ids {
		message, err := g.GetMessageMetadata(ctx, id)
		if errors.Is(err, ErrMessageUnavailable) {
			out = append(out, GmailMessage{ID: id, Unavailable: true})
			continue
		}
		if err != nil {
			return nil, err
		}
		out = append(out, message)
	}
	return out, nil
}

// FetchRecent lists and then hydrates up to maxResults recent messages matching
// query (empty fetches the newest overall). Messages that became unavailable
// after listing are retained as explicit placeholders; transient or malformed
// fetch failures fail the page so callers can retry without losing the cursor.
func (g GmailClient) FetchRecent(ctx context.Context, maxResults int, query string) ([]GmailMessage, error) {
	ids, err := g.ListRecentMessageIDs(ctx, maxResults, query)
	if err != nil {
		return nil, err
	}
	return g.FetchMessageIDs(ctx, ids)
}

func (g GmailClient) collectPart(ctx context.Context, messageID string, part gmailMessagePart, plain, htmlBody *[]string, attachments *[]GmailAttachment, attachmentContentBudget, omittedAttachments *int, bodyEncodingWarning *bool) error {
	if strings.TrimSpace(part.Filename) != "" {
		if len(*attachments) >= maxGmailAttachmentRecords {
			(*omittedAttachments)++
			return nil
		}
		attachment := GmailAttachment{Filename: part.Filename, MimeType: part.MimeType, Size: part.Body.Size, ContentStatus: "unsupported_mime"}
		if gmailTextMime(part.MimeType) {
			attachment.ContentStatus = "size_limit"
			if part.Body.Size <= maxGmailAttachmentBytes {
				attachment.ContentStatus = "budget_limit"
			}
			if part.Body.Size <= maxGmailAttachmentBytes && *attachmentContentBudget > 0 && part.Body.Size <= int64(*attachmentContentBudget) {
				data := part.Body.Data
				if data == "" && part.Body.AttachmentID != "" {
					var err error
					data, err = g.fetchAttachmentData(ctx, messageID, part.Body.AttachmentID)
					if errors.Is(err, ErrAttachmentUnavailable) {
						attachment.ContentStatus = "unavailable"
					} else if errors.Is(err, ErrAttachmentUnsupported) {
						attachment.ContentStatus = "size_limit"
					} else if err != nil {
						return fmt.Errorf("fetch Gmail attachment for message %s: %w", messageID, err)
					}
				}
				if attachment.ContentStatus != "unavailable" && attachment.ContentStatus != "size_limit" {
					if data == "" && part.Body.Size > 0 {
						return fmt.Errorf("Gmail returned no content for message %s attachment %s", messageID, part.Filename)
					}
					decoded, err := decodeGmailData(data)
					if err != nil {
						attachment.ContentStatus = "invalid_encoding"
					} else if len(decoded) > maxGmailAttachmentBytes || len(decoded) > *attachmentContentBudget {
						attachment.ContentStatus = "size_limit"
					} else {
						text, encodingWarning := decodeGmailText(decoded, part.MimeType)
						attachment.Content = strings.TrimSpace(text)
						attachment.Fetched = true
						attachment.ContentStatus = "fetched"
						if encodingWarning {
							attachment.ContentStatus = "invalid_encoding"
						} else if attachment.Content == "" {
							attachment.ContentStatus = "empty"
						}
						*attachmentContentBudget -= len(decoded)
					}
				}
			}
		}
		*attachments = append(*attachments, attachment)
		return nil
	}
	decoded, err := decodeGmailData(part.Body.Data)
	if err != nil {
		return fmt.Errorf("decode Gmail message %s body: %w", messageID, err)
	}
	if len(decoded) > 0 {
		text, encodingWarning := decodeGmailText(decoded, part.MimeType)
		if encodingWarning {
			*bodyEncodingWarning = true
		}
		text = strings.TrimSpace(text)
		switch strings.ToLower(strings.TrimSpace(strings.SplitN(part.MimeType, ";", 2)[0])) {
		case "text/plain":
			*plain = append(*plain, text)
		case "text/html":
			*htmlBody = append(*htmlBody, htmlToText(text))
		}
	}
	for _, child := range part.Parts {
		if err := g.collectPart(ctx, messageID, child, plain, htmlBody, attachments, attachmentContentBudget, omittedAttachments, bodyEncodingWarning); err != nil {
			return err
		}
	}
	return nil
}

func (g GmailClient) fetchAttachmentData(ctx context.Context, messageID, attachmentID string) (string, error) {
	var response struct {
		Data string `json:"data"`
		Size int64  `json:"size"`
	}
	path := "/users/me/messages/" + url.PathEscape(messageID) + "/attachments/" + url.PathEscape(attachmentID)
	if err := g.getJSON(ctx, path, &response); err != nil {
		var apiErr *ProviderAPIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
			return "", fmt.Errorf("%w: %w", ErrAttachmentUnavailable, err)
		}
		if errors.Is(err, ErrGmailResponseTooLarge) {
			return "", fmt.Errorf("%w: attachment response exceeded the safety limit", ErrAttachmentUnsupported)
		}
		return "", err
	}
	if response.Size > maxGmailAttachmentBytes {
		return "", fmt.Errorf("%w: gmail attachment exceeds the safety limit", ErrAttachmentUnsupported)
	}
	return response.Data, nil
}

func decodeGmailData(value string) ([]byte, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	return base64.RawURLEncoding.DecodeString(value)
}

func decodeGmailText(data []byte, contentType string) (string, bool) {
	_, parameters, err := mime.ParseMediaType(contentType)
	if err != nil {
		return strings.ToValidUTF8(string(data), "\uFFFD"), true
	}
	label := strings.TrimSpace(parameters["charset"])
	if label == "" || strings.EqualFold(label, "utf-8") || strings.EqualFold(label, "utf8") {
		if utf8.Valid(data) {
			return string(data), false
		}
		return strings.ToValidUTF8(string(data), "\uFFFD"), true
	}
	reader, err := charset.NewReaderLabel(label, bytes.NewReader(data))
	if err != nil {
		return strings.ToValidUTF8(string(data), "\uFFFD"), true
	}
	decoded, err := io.ReadAll(reader)
	if err != nil || !utf8.Valid(decoded) {
		return strings.ToValidUTF8(string(decoded), "\uFFFD"), true
	}
	return string(decoded), false
}

func gmailTextMime(mimeType string) bool {
	mimeType = strings.ToLower(strings.TrimSpace(strings.SplitN(mimeType, ";", 2)[0]))
	return strings.HasPrefix(mimeType, "text/") || mimeType == "application/json" || mimeType == "application/xml"
}

func htmlToText(value string) string {
	tokenizer := xhtml.NewTokenizer(strings.NewReader(value))
	var output bytes.Buffer
	skipDepth := 0
	for {
		tokenType := tokenizer.Next()
		if tokenType == xhtml.ErrorToken {
			break
		}
		token := tokenizer.Token()
		switch tokenType {
		case xhtml.StartTagToken:
			if token.Data == "script" || token.Data == "style" {
				skipDepth++
			}
		case xhtml.EndTagToken:
			if (token.Data == "script" || token.Data == "style") && skipDepth > 0 {
				skipDepth--
			}
			if skipDepth == 0 && (token.Data == "p" || token.Data == "div" || token.Data == "br" || token.Data == "li") {
				output.WriteByte('\n')
			}
		case xhtml.TextToken:
			if skipDepth == 0 {
				output.WriteString(token.Data)
				output.WriteByte(' ')
			}
		}
	}
	return strings.Join(strings.Fields(output.String()), " ")
}

func truncateText(value string, maxBytes int) string {
	truncated, _ := truncateTextWithStatus(value, maxBytes)
	return truncated
}

func truncateTextWithStatus(value string, maxBytes int) (string, bool) {
	value = strings.ToValidUTF8(value, "\uFFFD")
	if maxBytes <= 0 {
		return "", len(value) > 0
	}
	if len(value) <= maxBytes {
		return value, false
	}
	prefix := value[:maxBytes]
	for !utf8.ValidString(prefix) {
		prefix = prefix[:len(prefix)-1]
	}
	return prefix + "...", true
}

func (g GmailClient) getJSON(ctx context.Context, path string, target any) error {
	if !validGoogleEndpoint(g.baseURL(), "gmail.googleapis.com", "/gmail/v1") {
		return fmt.Errorf("gmail API endpoint must use HTTPS on Google's Gmail API host or a loopback test server")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, g.baseURL()+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+g.AccessToken)
	req.Header.Set("Accept", "application/json")

	resp, err := g.httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("gmail request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return newProviderAPIError("Gmail", resp, nil)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxGmailResponseBytes+1))
	if err != nil {
		return err
	}
	if len(body) > maxGmailResponseBytes {
		return fmt.Errorf("%w: gmail response exceeded the %d byte safety limit", ErrGmailResponseTooLarge, maxGmailResponseBytes)
	}
	if err := json.Unmarshal(body, target); err != nil {
		return fmt.Errorf("gmail returned unparseable JSON: %w", err)
	}
	return nil
}
