package source

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"automation-hub-backend/internal/models"
)

// Trello live, read-only connector.
//
// This adapter connects to the real Trello REST API. It is deliberately narrow:
//
//   - Read-only. Every request is an HTTP GET. There is no code path that can
//     POST/PUT/DELETE to Trello, so a connected board can never be mutated.
//   - Least privilege. Credentials come from the environment, never from the
//     stored source, and the token is expected to carry only the `read` scope
//     (see docs/provider-credential-checklist.md). No board id, key, or token is
//     persisted on the ConnectedSource row.
//   - Bounded. It reuses the shared host allowlist, blocked-address guard,
//     timeout, transport, and response-size cap that gate every outbound source
//     fetch (see sourceHTTPHostAllowed / sourceHTTPTransport / sourceHTTPMaxBytes).
//   - Incremental. The cursor is the newest card `dateLastActivity` seen. On the
//     next sync only cards whose activity is strictly newer than the cursor are
//     ingested, so unchanged cards do not churn extractions.
//   - Provenance + audit. Each card becomes an ImportItem carrying the card's
//     canonical shortUrl, so every downstream extraction and workflow links back
//     to its source. Sync itself records the audit trail via the generic
//     pipeline in service.Sync.
const (
	trelloConnectorKey     = "trello"
	trelloDefaultBaseURL   = "https://api.trello.com"
	trelloCardFetchLimit   = 1000
	trelloAPIKeyEnv        = "TRELLO_API_KEY"
	trelloReadTokenEnv     = "TRELLO_READ_TOKEN"
	trelloBaseURLEnv       = "TRELLO_API_BASE_URL"
	trelloCardFields       = "name,desc,url,shortUrl,due,dateLastActivity,idList,labels,closed"
	trelloActionFields     = "date,data,idMemberCreator,type"
	trelloAttachmentFields = "name,url,mimeType,bytes,date,isUpload"
	trelloChecklistFields  = "name,pos"
	trelloCheckItemFields  = "name,state,due,dueComplete,pos"
	// Trello caps actions nested under the /boards/{id}/cards URL resource at
	// 300, even though the dedicated actions endpoint permits a larger page.
	trelloCommentLimit    = 300
	trelloTimeParseLayout = time.RFC3339
)

// trelloIDPattern matches a raw Trello board id (24 hex) or 8-char shortLink.
var trelloIDPattern = regexp.MustCompile(`^[a-zA-Z0-9]{8,32}$`)

type trelloBoard struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	URL      string `json:"url"`
	ShortURL string `json:"shortUrl"`
}

type trelloList struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type trelloLabel struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

type trelloMember struct {
	ID       string `json:"id"`
	FullName string `json:"fullName"`
	Username string `json:"username"`
}

type trelloActionData struct {
	Text string `json:"text"`
}

type trelloAction struct {
	ID              string           `json:"id"`
	Type            string           `json:"type"`
	Date            string           `json:"date"`
	IDMemberCreator string           `json:"idMemberCreator"`
	Data            trelloActionData `json:"data"`
	MemberCreator   trelloMember     `json:"memberCreator"`
}

type trelloAttachment struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	URL      string `json:"url"`
	MimeType string `json:"mimeType"`
	Bytes    int64  `json:"bytes"`
	Date     string `json:"date"`
	IsUpload bool   `json:"isUpload"`
}

type trelloCheckItem struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	State       string  `json:"state"`
	Due         string  `json:"due"`
	DueComplete bool    `json:"dueComplete"`
	Pos         float64 `json:"pos"`
}

type trelloChecklist struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Pos        float64           `json:"pos"`
	CheckItems []trelloCheckItem `json:"checkItems"`
}

type trelloCard struct {
	ID               string             `json:"id"`
	Name             string             `json:"name"`
	Desc             string             `json:"desc"`
	URL              string             `json:"url"`
	ShortURL         string             `json:"shortUrl"`
	Due              string             `json:"due"`
	DateLastActivity string             `json:"dateLastActivity"`
	IDList           string             `json:"idList"`
	Labels           []trelloLabel      `json:"labels"`
	Closed           bool               `json:"closed"`
	Actions          []trelloAction     `json:"actions"`
	Attachments      []trelloAttachment `json:"attachments"`
	Checklists       []trelloChecklist  `json:"checklists"`
}

// trelloConfigured reports whether least-privilege read credentials are present.
// It is used to keep the connector catalog honest: the adapter is implemented,
// but it cannot connect until an operator supplies the key and read-only token.
func trelloConfigured() bool {
	return strings.TrimSpace(os.Getenv(trelloAPIKeyEnv)) != "" && strings.TrimSpace(os.Getenv(trelloReadTokenEnv)) != ""
}

// fetchTrelloSource pulls read-only card metadata from a live Trello board and
// returns import items plus the advanced cursor. It never writes to Trello.
func fetchTrelloSource(ctx context.Context, source *models.ConnectedSource) ([]ImportItem, string, error) {
	if source == nil {
		return nil, "", fmt.Errorf("source is required")
	}
	key := strings.TrimSpace(os.Getenv(trelloAPIKeyEnv))
	token := strings.TrimSpace(os.Getenv(trelloReadTokenEnv))
	if key == "" || token == "" {
		return nil, "", fmt.Errorf("trello connector is not configured; set %s and a least-privilege read-only %s", trelloAPIKeyEnv, trelloReadTokenEnv)
	}
	boardID, err := trelloBoardID(source.SyncTarget)
	if err != nil {
		return nil, "", err
	}
	base, err := trelloBaseURL()
	if err != nil {
		return nil, "", err
	}

	var board trelloBoard
	if err := trelloGetJSON(ctx, base, key, token, "/1/boards/"+boardID, url.Values{"fields": {"name,url,shortUrl"}}, &board); err != nil {
		return nil, "", fmt.Errorf("fetch trello board: %w", err)
	}

	var lists []trelloList
	if err := trelloGetJSON(ctx, base, key, token, "/1/boards/"+boardID+"/lists", url.Values{"fields": {"name"}, "filter": {"open"}}, &lists); err != nil {
		return nil, "", fmt.Errorf("fetch trello lists: %w", err)
	}
	listNames := make(map[string]string, len(lists))
	for _, list := range lists {
		listNames[list.ID] = list.Name
	}

	var cards []trelloCard
	cardQuery := url.Values{
		"fields":                      {trelloCardFields},
		"filter":                      {"visible"},
		"limit":                       {fmt.Sprintf("%d", trelloCardFetchLimit)},
		"actions":                     {"commentCard"},
		"actions_limit":               {fmt.Sprintf("%d", trelloCommentLimit)},
		"action_fields":               {trelloActionFields},
		"action_memberCreator":        {"true"},
		"action_memberCreator_fields": {"fullName,username"},
		"attachments":                 {"true"},
		"attachment_fields":           {trelloAttachmentFields},
		"checklists":                  {"all"},
		"checklist_fields":            {trelloChecklistFields},
		"checkItem_fields":            {trelloCheckItemFields},
	}
	if err := trelloGetJSON(ctx, base, key, token, "/1/boards/"+boardID+"/cards", cardQuery, &cards); err != nil {
		return nil, "", fmt.Errorf("fetch trello cards: %w", err)
	}

	boardName := firstNonEmpty(strings.TrimSpace(board.Name), boardID)
	projectKey := firstNonEmpty(source.DefaultProjectKey, slugText(boardName))
	// Trello's cards endpoint cannot filter by last-activity, so we advance the
	// cursor by comparing dateLastActivity client-side. This keeps ingestion
	// incremental (unchanged cards are skipped) without over-claiming an
	// API-level since filter that Trello does not provide for activity.
	cursorTime, hasCursor := parseTrelloTime(source.Cursor)
	latest := cursorTime
	items := make([]ImportItem, 0, len(cards))
	for _, card := range cards {
		if card.Closed || strings.TrimSpace(card.ID) == "" {
			continue
		}
		activity, ok := parseTrelloTime(card.DateLastActivity)
		if ok && activity.After(latest) {
			latest = activity
		}
		if hasCursor && ok && !activity.After(cursorTime) {
			continue // unchanged since last sync
		}
		items = append(items, trelloImportItem(card, boardName, listNames[card.IDList], projectKey))
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].ExternalID < items[j].ExternalID })

	nextCursor := source.Cursor
	if !latest.IsZero() {
		nextCursor = latest.UTC().Format(time.RFC3339Nano)
	}
	return items, nextCursor, nil
}

func trelloImportItem(card trelloCard, boardName, listName, projectKey string) ImportItem {
	list := firstNonEmpty(strings.TrimSpace(listName), "(unknown list)")
	labels := trelloLabelNames(card.Labels)
	provenance := firstNonEmpty(strings.TrimSpace(card.ShortURL), strings.TrimSpace(card.URL))
	lines := []string{
		"Trello card: " + card.Name,
		"Board: " + boardName,
		"List: " + list,
	}
	if strings.TrimSpace(card.Due) != "" {
		lines = append(lines, "Due: "+strings.TrimSpace(card.Due))
	}
	if labels != "" {
		lines = append(lines, "Labels: "+labels)
	}
	if strings.TrimSpace(card.Desc) != "" {
		lines = append(lines, "", strings.TrimSpace(card.Desc))
	}
	lines = appendTrelloComments(lines, card.Actions)
	lines = appendTrelloChecklists(lines, card.Checklists)
	lines = appendTrelloAttachments(lines, card.Attachments)
	return ImportItem{
		ExternalID: "trello:card:" + card.ID,
		Title:      firstNonEmpty(strings.TrimSpace(card.Name), "(untitled card)"),
		Content:    strings.Join(lines, "\n"),
		SourceURI:  provenance,
		ItemType:   "trello_card",
		ProjectKey: projectKey,
		Metadata: fmt.Sprintf("source=trello;board=%s;list=%s;due=%s;labels=%s;dateLastActivity=%s;comments=%d;checklists=%d;attachments=%d;attachmentContentFetched=false;readonly=true",
			boardName, list, strings.TrimSpace(card.Due), labels, strings.TrimSpace(card.DateLastActivity), len(card.Actions), len(card.Checklists), len(card.Attachments)),
	}
}

func appendTrelloComments(lines []string, actions []trelloAction) []string {
	comments := make([]trelloAction, 0, len(actions))
	for _, action := range actions {
		if action.Type == "commentCard" && strings.TrimSpace(action.Data.Text) != "" {
			comments = append(comments, action)
		}
	}
	if len(comments) == 0 {
		return lines
	}
	sort.SliceStable(comments, func(i, j int) bool {
		left, leftOK := parseTrelloTime(comments[i].Date)
		right, rightOK := parseTrelloTime(comments[j].Date)
		if leftOK && rightOK {
			return left.Before(right)
		}
		return comments[i].Date < comments[j].Date
	})
	lines = append(lines, "", fmt.Sprintf("Comments (%d):", len(comments)))
	for _, comment := range comments {
		author := firstNonEmpty(strings.TrimSpace(comment.MemberCreator.FullName), strings.TrimSpace(comment.MemberCreator.Username), strings.TrimSpace(comment.IDMemberCreator), "unknown member")
		stamp := strings.TrimSpace(comment.Date)
		prefix := "- " + author
		if stamp != "" {
			prefix = "- [" + stamp + "] " + author
		}
		lines = append(lines, prefix+": "+strings.TrimSpace(comment.Data.Text))
	}
	return lines
}

func appendTrelloChecklists(lines []string, checklists []trelloChecklist) []string {
	if len(checklists) == 0 {
		return lines
	}
	sort.SliceStable(checklists, func(i, j int) bool { return checklists[i].Pos < checklists[j].Pos })
	lines = append(lines, "", fmt.Sprintf("Checklists (%d):", len(checklists)))
	for _, checklist := range checklists {
		name := firstNonEmpty(strings.TrimSpace(checklist.Name), "Checklist")
		lines = append(lines, "- "+name)
		items := append([]trelloCheckItem(nil), checklist.CheckItems...)
		sort.SliceStable(items, func(i, j int) bool { return items[i].Pos < items[j].Pos })
		for _, item := range items {
			marker := "[ ]"
			if strings.EqualFold(strings.TrimSpace(item.State), "complete") || item.DueComplete {
				marker = "[x]"
			}
			entry := "  - " + marker + " " + firstNonEmpty(strings.TrimSpace(item.Name), "(untitled item)")
			if due := strings.TrimSpace(item.Due); due != "" {
				entry += " (due " + due + ")"
			}
			lines = append(lines, entry)
		}
	}
	return lines
}

func appendTrelloAttachments(lines []string, attachments []trelloAttachment) []string {
	if len(attachments) == 0 {
		return lines
	}
	lines = append(lines, "", fmt.Sprintf("Attachments (%d; metadata only):", len(attachments)))
	for _, attachment := range attachments {
		name := firstNonEmpty(strings.TrimSpace(attachment.Name), "(unnamed attachment)")
		details := make([]string, 0, 2)
		if mimeType := strings.TrimSpace(attachment.MimeType); mimeType != "" {
			details = append(details, mimeType)
		}
		if attachment.Bytes > 0 {
			details = append(details, fmt.Sprintf("%d bytes", attachment.Bytes))
		}
		if len(details) > 0 {
			name += " (" + strings.Join(details, ", ") + ")"
		}
		if sourceURL := strings.TrimSpace(attachment.URL); sourceURL != "" {
			name += ": " + sourceURL
		}
		lines = append(lines, "- "+name)
	}
	return lines
}

func trelloLabelNames(labels []trelloLabel) string {
	names := make([]string, 0, len(labels))
	for _, label := range labels {
		if name := strings.TrimSpace(label.Name); name != "" {
			names = append(names, name)
		} else if color := strings.TrimSpace(label.Color); color != "" {
			names = append(names, color)
		}
	}
	return strings.Join(names, ", ")
}

// trelloBoardID accepts a bare board id/shortLink or a full trello.com board URL
// and returns the id segment. It never accepts embedded credentials.
func trelloBoardID(syncTarget string) (string, error) {
	raw := strings.TrimSpace(syncTarget)
	if raw == "" {
		return "", fmt.Errorf("trello syncTarget must be a board id or board URL")
	}
	if strings.Contains(raw, "://") {
		parsed, err := url.Parse(raw)
		if err != nil {
			return "", fmt.Errorf("trello syncTarget URL is invalid: %w", err)
		}
		if parsed.User != nil {
			return "", fmt.Errorf("trello credentials must not be embedded in syncTarget")
		}
		segments := strings.Split(strings.Trim(parsed.Path, "/"), "/")
		for i, segment := range segments {
			if segment == "b" && i+1 < len(segments) {
				raw = segments[i+1]
				break
			}
		}
	}
	if !trelloIDPattern.MatchString(raw) {
		return "", fmt.Errorf("trello board id %q is not a valid id or shortLink", raw)
	}
	return raw, nil
}

func trelloBaseURL() (*url.URL, error) {
	base := strings.TrimRight(firstNonEmpty(os.Getenv(trelloBaseURLEnv), trelloDefaultBaseURL), "/")
	parsed, err := url.Parse(base)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" {
		return nil, fmt.Errorf("%s must be an absolute HTTP(S) URL", trelloBaseURLEnv)
	}
	if !sourceHTTPHostAllowed(parsed.Hostname()) || sourceHTTPAddressBlocked(parsed.Hostname()) {
		return nil, fmt.Errorf("trello API host %s is not allowlisted; add api.trello.com to CONNECTED_SOURCE_HTTP_ALLOWED_HOSTS", parsed.Hostname())
	}
	return parsed, nil
}

// trelloGetJSON issues a single bounded, read-only GET and decodes the body into
// out. Credentials are attached as query parameters (Trello's auth scheme) and
// are never included in returned error messages.
func trelloGetJSON(ctx context.Context, base *url.URL, key, token, resourcePath string, query url.Values, out any) error {
	target := *base
	target.Path = strings.TrimRight(base.Path, "/") + resourcePath
	if query == nil {
		query = url.Values{}
	}
	query.Set("key", key)
	query.Set("token", token)
	target.RawQuery = query.Encode()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "HAI-connected-source")
	client := &http.Client{
		Timeout:       sourceHTTPTimeout(),
		Transport:     sourceHTTPTransport(),
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
	}
	response, err := client.Do(request)
	if err != nil {
		// Do not surface target.String(): it carries the token.
		if errContext := ctx.Err(); errContext != nil {
			return errContext
		}
		return fmt.Errorf("request to %s failed", resourcePath)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		return fmt.Errorf("trello rejected the read-only credentials (HTTP %d) for %s", response.StatusCode, resourcePath)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("trello returned HTTP %d for %s", response.StatusCode, resourcePath)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, sourceHTTPMaxBytes()+1))
	if err != nil {
		return err
	}
	if int64(len(body)) > sourceHTTPMaxBytes() {
		return fmt.Errorf("trello response for %s exceeds %d bytes", resourcePath, sourceHTTPMaxBytes())
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decode trello response for %s: %w", resourcePath, err)
	}
	return nil
}

func parseTrelloTime(value string) (time.Time, bool) {
	clean := strings.TrimSpace(value)
	if clean == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{time.RFC3339Nano, trelloTimeParseLayout} {
		if parsed, err := time.Parse(layout, clean); err == nil {
			return parsed.UTC(), true
		}
	}
	return time.Time{}, false
}
