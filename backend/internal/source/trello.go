package source

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
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
//     (see docs/provider-credential-checklist.md). The stored syncTarget is
//     limited to a board ID or Trello board URL without embedded credentials,
//     query parameters, or fragments; API keys and tokens are not persisted.
//   - Bounded. It reuses the shared host allowlist, blocked-address guard,
//     timeout, transport, and response-size cap that gate every outbound source
//     fetch, caps each sync at 90 HTTP attempts including retries, and bounds
//     all card/action pages to 10,000 records and 16 MiB of aggregate JSON.
//   - Complete-or-fail pagination. Cards are read in descending Mongo-ID pages;
//     comments are read separately from the board action endpoint, never nested
//     on a large card request. Incomplete pages cannot produce a new cursor.
//   - Reconciled full scan. Trello has no supported board-card change cursor
//     for this adapter, so every sync reads the complete paginated board and
//     comment history. The card cursor limits emitted active-card updates but
//     does not reduce provider reads; closed cards are reconciled every scan.
//   - State reconciliation. Closed cards and cards missing from the configured
//     board are emitted as lightweight tombstones even when their activity
//     timestamp did not advance. The source service applies them only to
//     existing records, preserving their evidence.
//   - Provenance + audit. Active cards become ImportItems carrying the card's
//     canonical shortUrl. Sync records the source and closure audit trail.
const (
	trelloConnectorKey            = "trello"
	trelloClosedCardItemType      = "trello_card_closed"
	trelloUnavailableCardItemType = "trello_card_unavailable"
	trelloDefaultBaseURL          = "https://api.trello.com"
	trelloCardPageSize            = 1000                    // Trello's documented maximum card page size.
	trelloActionPageSize          = 1000                    // Trello's documented maximum action page size.
	trelloMaxRecordsPerSync       = 10 * trelloCardPageSize // Shared by lists, cards, and comment actions.
	trelloMaxPageBytesPerSync     = 16 << 20                // Aggregate JSON for card and action pages.
	trelloMaxRequestsPerSync      = 90
	trelloAPIKeyEnv               = "TRELLO_API_KEY"
	trelloReadTokenEnv            = "TRELLO_READ_TOKEN"
	trelloOwnerIdentityEnv        = "TRELLO_ACCOUNT_OWNER_IDENTITY"
	trelloAccountMemberIDEnv      = "TRELLO_ACCOUNT_MEMBER_ID"
	trelloBaseURLEnv              = "TRELLO_API_BASE_URL"
	trelloCardFields              = "idBoard,name,desc,url,shortUrl,shortLink,start,due,dueComplete,dateLastActivity,idList,idMembers,labels,closed"
	trelloActionFields            = "id,date,data,idMemberCreator,type,memberCreator"
	trelloCommentActionTypes      = "commentCard,copyCommentCard"
	trelloAttachmentFields        = "name,url,mimeType,bytes,date,isUpload"
	trelloChecklistFields         = "name,pos"
	trelloCheckItemFields         = "name,state,due,pos"
	trelloCursorV1Prefix          = "trello:v1:"
	trelloCursorV2Prefix          = "trello:v2:"
	trelloCursorMaxLength         = 512
	trelloCardIDByteLength        = 12
	trelloRateLimitMaxRetries     = 2
	trelloRateLimitMaxWait        = 2 * time.Second
	trelloRateLimitBaseDelay      = 100 * time.Millisecond
	trelloTimeParseLayout         = time.RFC3339
)

// trelloIDPattern matches a raw Trello board id (24 hex) or 8-char shortLink.
var trelloIDPattern = regexp.MustCompile(`^[a-zA-Z0-9]{8}$|^[a-fA-F0-9]{24}$`)

var errTrelloRequestBudgetExhausted = errors.New("Trello sync request budget exhausted")

type trelloBoard struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	URL      string `json:"url"`
	ShortURL string `json:"shortUrl"`
}

type trelloTokenPermission struct {
	ModelType string `json:"modelType"`
	Read      bool   `json:"read"`
	Write     bool   `json:"write"`
}

type trelloTokenInfo struct {
	IDMember    string                  `json:"idMember"`
	Permissions []trelloTokenPermission `json:"permissions"`
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
	Text string           `json:"text"`
	Card trelloActionCard `json:"card"`
}

type trelloActionCard struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	ShortLink string `json:"shortLink"`
	URL       string `json:"url"`
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
	ID    string  `json:"id"`
	Name  string  `json:"name"`
	State string  `json:"state"`
	Due   string  `json:"due"`
	Pos   float64 `json:"pos"`
}

type trelloChecklist struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Pos        float64           `json:"pos"`
	CheckItems []trelloCheckItem `json:"checkItems"`
}

type trelloCard struct {
	ID               string             `json:"id"`
	IDBoard          string             `json:"idBoard"`
	Name             string             `json:"name"`
	Desc             string             `json:"desc"`
	URL              string             `json:"url"`
	ShortURL         string             `json:"shortUrl"`
	ShortLink        string             `json:"shortLink"`
	Start            string             `json:"start"`
	Due              string             `json:"due"`
	DueComplete      bool               `json:"dueComplete"`
	DateLastActivity string             `json:"dateLastActivity"`
	IDList           string             `json:"idList"`
	IDMembers        []string           `json:"idMembers"`
	Labels           []trelloLabel      `json:"labels"`
	Closed           bool               `json:"closed"`
	Actions          []trelloAction     `json:"actions"`
	Members          []trelloMember     `json:"members"`
	Attachments      []trelloAttachment `json:"attachments"`
	Checklists       []trelloChecklist  `json:"checklists"`
}

type trelloCursorV1 struct {
	LastActivity string   `json:"t"`
	CardIDs      []string `json:"i"`
}

// trelloConfigured reports whether least-privilege read credentials are present.
// It is used to keep the connector catalog honest: the adapter is implemented,
// but it cannot connect until an operator supplies the key and read-only token.
func trelloConfigured() bool {
	_, validMemberID := normalizedTrelloMongoID(os.Getenv(trelloAccountMemberIDEnv))
	return validTrelloHeaderCredential(os.Getenv(trelloAPIKeyEnv)) &&
		validTrelloHeaderCredential(os.Getenv(trelloReadTokenEnv)) &&
		strings.TrimSpace(os.Getenv(trelloOwnerIdentityEnv)) != "" && validMemberID
}

func validTrelloHeaderCredential(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

func trelloCredentialsBelongToOwner(ownerIdentity string) bool {
	configuredOwner := strings.TrimSpace(os.Getenv(trelloOwnerIdentityEnv))
	return configuredOwner != "" && ownerIdentity != "" &&
		strings.TrimSpace(ownerIdentity) == ownerIdentity && ownerIdentity == configuredOwner
}

// fetchTrelloSource pulls read-only card metadata from a live Trello board and
// returns import items plus the advanced cursor. It never writes to Trello.
func fetchTrelloSource(ctx context.Context, source *models.ConnectedSource) ([]ImportItem, string, error) {
	return fetchTrelloSourceSnapshot(ctx, source, nil, false)
}

func fetchTrelloSourceWithExistingItems(ctx context.Context, source *models.ConnectedSource, existingItems []models.SourceRawItem) ([]ImportItem, string, error) {
	return fetchTrelloSourceSnapshot(ctx, source, existingItems, true)
}

func fetchTrelloSourceSnapshot(ctx context.Context, source *models.ConnectedSource, existingItems []models.SourceRawItem, reconcileExistingSnapshot bool) ([]ImportItem, string, error) {
	if ctx == nil {
		return nil, "", errors.New("trello source context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, "", newProviderSyncError("Trello", 0, err)
	}
	if source == nil {
		return nil, "", fmt.Errorf("source is required")
	}
	if !trelloConfigured() {
		return nil, "", fmt.Errorf("trello connector is not configured; set %s, %s, %s, and a valid %s", trelloAPIKeyEnv, trelloReadTokenEnv, trelloOwnerIdentityEnv, trelloAccountMemberIDEnv)
	}
	if !trelloCredentialsBelongToOwner(source.OwnerIdentity) {
		return nil, "", fmt.Errorf("trello credentials are not assigned to this connected source owner")
	}
	cursorTime, _, hasCursor, err := parseTrelloCursor(source.Cursor)
	if err != nil {
		return nil, "", fmt.Errorf("parse trello cursor: %w", err)
	}
	key := strings.TrimSpace(os.Getenv(trelloAPIKeyEnv))
	token := strings.TrimSpace(os.Getenv(trelloReadTokenEnv))
	boardID, err := trelloBoardID(source.SyncTarget)
	if err != nil {
		return nil, "", err
	}
	base, err := trelloBaseURL()
	if err != nil {
		return nil, "", err
	}
	var syncBudget trelloSyncBudget
	client := &trelloAPIClient{base: base, key: key, token: token, syncBudget: &syncBudget}
	if err := validateTrelloReadOnlyToken(ctx, client); err != nil {
		return nil, "", err
	}

	var board trelloBoard
	if err := client.getJSON(ctx, "/1/boards/"+boardID, url.Values{"fields": {"name,url,shortUrl"}}, &board); err != nil {
		return nil, "", fmt.Errorf("fetch trello board: %w", err)
	}
	boardID, err = canonicalTrelloBoardIDForTarget(boardID, board)
	if err != nil {
		return nil, "", fmt.Errorf("verify trello board identity: %w", err)
	}

	lists, err := getTrelloJSONArray[trelloList](ctx, client, "/1/boards/"+boardID+"/lists", url.Values{"fields": {"name"}, "filter": {"all"}})
	if err != nil {
		return nil, "", fmt.Errorf("fetch trello lists: %w", err)
	}
	if err := syncBudget.addRecords(len(lists)); err != nil {
		return nil, "", fmt.Errorf("fetch trello lists: %w", err)
	}
	listNames := make(map[string]string, len(lists))
	for _, list := range lists {
		listNames[list.ID] = list.Name
	}

	cards, err := fetchTrelloBoardCards(ctx, client, boardID, &syncBudget)
	if err != nil {
		return nil, "", fmt.Errorf("fetch trello cards: %w", err)
	}
	cardByID := make(map[string]int, len(cards))
	cardIDsOnBoard := make(map[string]struct{}, len(cards))
	for index := range cards {
		cardIDsOnBoard[cards[index].ID] = struct{}{}
		if !cards[index].Closed {
			cardByID[cards[index].ID] = index
		}
	}
	if len(cardByID) > 0 {
		comments, err := fetchTrelloBoardComments(ctx, client, boardID, cardByID, &syncBudget)
		if err != nil {
			return nil, "", fmt.Errorf("fetch trello comments: %w", err)
		}
		for cardID, actions := range comments {
			cards[cardByID[cardID]].Actions = actions
		}
	}

	boardName := firstNonEmpty(strings.TrimSpace(board.Name), boardID)
	projectKey := strings.TrimSpace(source.DefaultProjectKey)
	knownItems := make(map[string]models.SourceRawItem, len(existingItems))
	for _, existing := range existingItems {
		if strings.HasPrefix(existing.ExternalID, "trello:card:") {
			knownItems[existing.ExternalID] = existing
		}
	}
	// Trello's cards endpoint cannot filter by last-activity, so compare the
	// complete paginated card set client-side. The timestamp is only a fast-path:
	// compare each card's stable content hash as well, because a board can change
	// while its ID-paginated pages are being read. This catches activity older
	// than a cursor advanced by a later page without re-indexing unchanged cards.
	latest := cursorTime
	items := make([]ImportItem, 0, len(cards))
	for _, card := range cards {
		activity, ok := parseTrelloTime(card.DateLastActivity)
		if !ok {
			return nil, "", fmt.Errorf("trello card %q has a missing or invalid dateLastActivity; refusing to advance the cursor", firstNonEmpty(strings.TrimSpace(card.Name), card.ID))
		}
		if latest.IsZero() || activity.After(latest) {
			latest = activity
		}
		if card.Closed {
			items = append(items, trelloClosedCardImportItem(card, boardName, listNames[card.IDList], projectKey))
		} else {
			item := trelloImportItem(card, boardName, listNames[card.IDList], projectKey)
			known, exists := knownItems[item.ExternalID]
			include := !hasCursor || !activity.Before(cursorTime)
			if reconcileExistingSnapshot && hasCursor {
				include = activity.After(cursorTime) || !exists || known.ItemType != "trello_card" || known.ContentHash != hashText(item.Title+"|"+item.Content)
			}
			if include {
				items = append(items, item)
			}
		}
	}
	for _, existing := range knownItems {
		if existing.ItemType != "trello_card" {
			continue
		}
		cardID := strings.TrimPrefix(existing.ExternalID, "trello:card:")
		if _, present := cardIDsOnBoard[cardID]; present {
			continue
		}
		items = append(items, ImportItem{
			ExternalID: existing.ExternalID,
			Title:      existing.Title,
			SourceURI:  existing.SourceURI,
			ItemType:   trelloUnavailableCardItemType,
			ProjectKey: existing.ProjectKey,
			Metadata:   "source=trello;state=not_on_configured_board;content_retained=true;readonly=true",
		})
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].ExternalID < items[j].ExternalID })

	nextCursor := source.Cursor
	if !latest.IsZero() {
		nextCursor, err = encodeTrelloCursor(latest, nil)
		if err != nil {
			return nil, "", fmt.Errorf("encode trello cursor: %w", err)
		}
	} else if strings.TrimSpace(nextCursor) == "" {
		// Persist a valid empty-board cursor so the generic sync layer does not
		// substitute its connector-agnostic timestamp cursor for Trello.
		nextCursor, err = encodeTrelloCursor(time.Time{}, nil)
		if err != nil {
			return nil, "", fmt.Errorf("encode empty trello cursor: %w", err)
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, "", newProviderSyncError("Trello", 0, err)
	}
	return items, nextCursor, nil
}

type trelloAPIClient struct {
	base         *url.URL
	key          string
	token        string
	requestsUsed int
	syncBudget   *trelloSyncBudget
	rateLimit    trelloRateLimitState
}

func (c *trelloAPIClient) getJSON(ctx context.Context, resourcePath string, query url.Values, out any) error {
	return trelloGetJSONContextWithRateLimitState(ctx, c.base, c.key, c.token, resourcePath, query, out, &c.requestsUsed, c.syncBudget, &c.rateLimit)
}

type trelloRateLimitState struct {
	retryAfter string
}

func (s *trelloRateLimitState) observe(headers http.Header) {
	if s == nil || s.retryAfter != "" {
		return
	}
	if retryAfter, exhausted := trelloRateLimitWindowRetryAfter(headers); exhausted {
		s.retryAfter = retryAfter
	}
}

type trelloSyncBudget struct {
	recordsRead int
	pageBytes   int64
}

func (b *trelloSyncBudget) addPageBytes(size int) error {
	if b == nil {
		return nil
	}
	if size < 0 || int64(size) > trelloMaxPageBytesPerSync-b.pageBytes {
		return trelloSyncLimitError(fmt.Sprintf("Trello sync exceeded its aggregate page JSON safety cap of %d bytes; review the board size before retrying", trelloMaxPageBytesPerSync))
	}
	b.pageBytes += int64(size)
	return nil
}

func trelloPageResponseReadLimit(maxResponseBytes int64, budget *trelloSyncBudget) (int64, int64) {
	if budget == nil {
		return maxResponseBytes, -1
	}
	remaining := trelloMaxPageBytesPerSync - budget.pageBytes
	if remaining < 0 {
		remaining = 0
	}
	if remaining < maxResponseBytes {
		return remaining, remaining
	}
	return maxResponseBytes, remaining
}

func (b *trelloSyncBudget) addRecords(count int) error {
	if b == nil {
		return nil
	}
	if count < 0 || count > trelloMaxRecordsPerSync-b.recordsRead {
		return trelloSyncLimitError(fmt.Sprintf("Trello sync exceeded its aggregate record safety cap of %d lists, cards, and comment actions; review the board size before retrying", trelloMaxRecordsPerSync))
	}
	b.recordsRead += count
	return nil
}

func trelloSyncLimitError(message string) error {
	retryable := false
	return newProviderSyncErrorWithOptions("Trello", http.StatusBadRequest, errors.New(message), providerSyncErrorOptions{
		RetryableOverride: &retryable,
		PublicMessage:     message,
	})
}

func fetchTrelloBoardCards(ctx context.Context, client *trelloAPIClient, boardID string, budget *trelloSyncBudget) ([]trelloCard, error) {
	expectedBoardID, validBoardID := normalizedTrelloMongoID(boardID)
	if !validBoardID {
		return nil, errors.New("Trello card pagination requires a verified canonical board ID")
	}
	baseQuery := url.Values{
		"fields":            {trelloCardFields},
		"filter":            {"all"},
		"sort":              {"-id"},
		"limit":             {strconv.Itoa(trelloCardPageSize)},
		"members":           {"true"},
		"member_fields":     {"fullName,username"},
		"attachments":       {"true"},
		"attachment_fields": {trelloAttachmentFields},
		"checklists":        {"all"},
		"checklist_fields":  {trelloChecklistFields},
		"checkItem_fields":  {trelloCheckItemFields},
	}

	cards := make([]trelloCard, 0)
	seen := make(map[string]struct{})
	before := ""
	for pageNumber := 0; ; pageNumber++ {
		query := cloneTrelloQuery(baseQuery)
		if before != "" {
			query.Set("before", before)
		}
		page, err := getTrelloJSONArray[trelloCard](ctx, client, "/1/boards/"+boardID+"/cards", query)
		if err != nil {
			return nil, fmt.Errorf("read card page %d: %w", pageNumber, err)
		}
		if len(page) > trelloCardPageSize {
			return nil, fmt.Errorf("card page %d exceeded the requested limit; refusing incomplete data", pageNumber)
		}
		if err := budget.addRecords(len(page)); err != nil {
			return nil, fmt.Errorf("card page %d: %w", pageNumber, err)
		}
		if len(page) == 0 {
			return cards, nil
		}

		previousID := ""
		newCards := 0
		for index, card := range page {
			card.ID = strings.TrimSpace(card.ID)
			idKey, validCardID := normalizedTrelloMongoID(card.ID)
			if !validCardID && (len(page) == trelloCardPageSize || before != "") {
				return nil, fmt.Errorf("card page %d contains an invalid Trello ID; refusing to advance pagination with an unstable source identity", pageNumber)
			}
			if card.ID == "" {
				return nil, fmt.Errorf("card page %d contains a card without an ID", pageNumber)
			}
			if validCardID {
				card.ID = idKey
				page[index].ID = idKey
			}
			cardBoardID, validCardBoardID := normalizedTrelloMongoID(card.IDBoard)
			if !validCardBoardID || cardBoardID != expectedBoardID {
				return nil, fmt.Errorf("card page %d contains a card without a matching board identity", pageNumber)
			}
			card.IDBoard = cardBoardID
			if previousID != "" {
				previousKey, previousValid := normalizedTrelloMongoID(previousID)
				if validCardID && previousValid && idKey >= previousKey {
					return nil, fmt.Errorf("card page %d is not strictly ordered by descending Mongo ID", pageNumber)
				}
			}
			previousID = card.ID

			beforeKey := ""
			if before != "" {
				var validBoundary bool
				beforeKey, validBoundary = normalizedTrelloMongoID(before)
				if !validBoundary {
					return nil, errors.New("card pagination boundary is not a valid Mongo ID")
				}
			}
			if validCardID && idKey == beforeKey && index == 0 {
				continue // Some Trello paging responses include the before boundary.
			}
			if _, duplicate := seen[card.ID]; duplicate {
				return nil, fmt.Errorf("card page %d repeated a non-boundary card; refusing potentially incomplete pagination", pageNumber)
			}
			if before != "" {
				if !validCardID || idKey >= beforeKey {
					return nil, fmt.Errorf("card page %d did not move strictly before its Mongo ID boundary", pageNumber)
				}
			}
			seen[card.ID] = struct{}{}
			cards = append(cards, card)
			newCards++
		}

		if len(page) < trelloCardPageSize {
			return cards, nil
		}
		if newCards == 0 {
			return nil, fmt.Errorf("card pagination made no progress at the Mongo ID boundary")
		}
		before = page[len(page)-1].ID
		if _, ok := normalizedTrelloMongoID(before); !ok {
			return nil, fmt.Errorf("full card page has no valid Mongo ID boundary")
		}
	}
}

func fetchTrelloBoardComments(ctx context.Context, client *trelloAPIClient, boardID string, cardIndexes map[string]int, budget *trelloSyncBudget) (map[string][]trelloAction, error) {
	baseQuery := url.Values{
		"filter":               {trelloCommentActionTypes},
		"fields":               {trelloActionFields},
		"limit":                {strconv.Itoa(trelloActionPageSize)},
		"memberCreator":        {"true"},
		"memberCreator_fields": {"fullName,username"},
	}
	comments := make(map[string][]trelloAction)
	seen := make(map[string]struct{})
	before := ""
	for pageNumber := 0; ; pageNumber++ {
		query := cloneTrelloQuery(baseQuery)
		if before != "" {
			query.Set("before", before)
		}
		page, err := getTrelloJSONArray[trelloAction](ctx, client, "/1/boards/"+boardID+"/actions", query)
		if err != nil {
			return nil, fmt.Errorf("read comment action page %d: %w", pageNumber, err)
		}
		if len(page) > trelloActionPageSize {
			return nil, fmt.Errorf("comment action page %d exceeded the requested limit; refusing incomplete data", pageNumber)
		}
		if err := budget.addRecords(len(page)); err != nil {
			return nil, fmt.Errorf("comment action page %d: %w", pageNumber, err)
		}
		if len(page) == 0 {
			return comments, nil
		}
		if err := validateTrelloActionPageOrder(page, before); err != nil {
			return nil, fmt.Errorf("comment action page %d: %w", pageNumber, err)
		}

		newActions := 0
		for index := range page {
			action := &page[index]
			action.ID = strings.TrimSpace(action.ID)
			actionID, validActionID := normalizedTrelloMongoID(action.ID)
			if !validActionID && (len(page) == trelloActionPageSize || before != "") {
				return nil, fmt.Errorf("comment action page %d contains an invalid Trello ID; refusing to advance pagination", pageNumber)
			}
			if action.ID == "" {
				return nil, fmt.Errorf("comment action page %d contains an action without an ID", pageNumber)
			}
			if validActionID {
				action.ID = actionID
			}
			beforeKey := ""
			if before != "" {
				var validBoundary bool
				beforeKey, validBoundary = normalizedTrelloMongoID(before)
				if !validBoundary {
					return nil, errors.New("comment action pagination boundary is not a valid Mongo ID")
				}
			}
			if validActionID && actionID == beforeKey && index == 0 {
				continue // De-duplicate an inclusive before boundary.
			}
			if _, duplicate := seen[action.ID]; duplicate {
				return nil, fmt.Errorf("comment action page %d repeated a non-boundary action; refusing potentially incomplete pagination", pageNumber)
			}
			if !isTrelloCommentAction(action.Type) {
				return nil, fmt.Errorf("comment action page %d returned an unexpected action type", pageNumber)
			}
			cardID := strings.TrimSpace(action.Data.Card.ID)
			normalizedCardID, validCardID := normalizedTrelloMongoID(cardID)
			if cardID == "" || (!validCardID && (len(page) == trelloActionPageSize || before != "")) {
				return nil, fmt.Errorf("comment action %q does not identify its card with a stable Trello ID", action.ID)
			}
			if validCardID {
				cardID = normalizedCardID
			}
			action.Data.Card.ID = cardID
			seen[action.ID] = struct{}{}
			newActions++
			if _, visible := cardIndexes[cardID]; visible {
				comments[cardID] = append(comments[cardID], *action)
			}
		}

		if len(page) < trelloActionPageSize {
			return comments, nil
		}
		if newActions == 0 {
			return nil, fmt.Errorf("comment action pagination made no progress at the Mongo ID boundary")
		}
		nextBefore := page[len(page)-1].ID
		if _, ok := normalizedTrelloMongoID(nextBefore); !ok {
			return nil, fmt.Errorf("full comment action page has no valid Mongo ID boundary")
		}
		if nextBefore == before {
			return nil, fmt.Errorf("comment action pagination did not move past its Mongo ID boundary")
		}
		before = nextBefore
	}
}

func cloneTrelloQuery(query url.Values) url.Values {
	clone := make(url.Values, len(query))
	for key, values := range query {
		clone[key] = append([]string(nil), values...)
	}
	return clone
}

func getTrelloJSONArray[T any](ctx context.Context, client *trelloAPIClient, resourcePath string, query url.Values) ([]T, error) {
	var raw json.RawMessage
	if err := client.getJSON(ctx, resourcePath, query, &raw); err != nil {
		return nil, err
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '[' {
		return nil, fmt.Errorf("Trello response for %s is not a JSON array", resourcePath)
	}
	var values []T
	if err := json.Unmarshal(trimmed, &values); err != nil {
		return nil, fmt.Errorf("decode Trello array for %s: %w", resourcePath, err)
	}
	return values, nil
}

func normalizedTrelloMongoID(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if len(value) != 24 {
		return "", false
	}
	if _, err := hex.DecodeString(value); err != nil {
		return "", false
	}
	return strings.ToLower(value), true
}

func validateTrelloActionPageOrder(page []trelloAction, before string) error {
	if len(page) == 0 {
		return nil
	}
	boundary := ""
	if strings.TrimSpace(before) != "" {
		var ok bool
		boundary, ok = normalizedTrelloMongoID(before)
		if !ok {
			return fmt.Errorf("pagination boundary is not a valid Mongo ID")
		}
	}
	ids := make([]string, len(page))
	allValid := true
	for i, action := range page {
		id, ok := normalizedTrelloMongoID(action.ID)
		if !ok {
			allValid = false
			break
		}
		ids[i] = id
	}
	// Legacy short-page fixtures can use synthetic IDs because they never form
	// or cross a provider cursor. Every paginated provider page must use stable
	// Trello Mongo IDs so its before boundary can be verified.
	if !allValid {
		if boundary != "" || len(page) >= trelloActionPageSize {
			return fmt.Errorf("paginated action page contains an invalid Trello ID")
		}
		return nil
	}
	previous := boundary
	for index, id := range ids {
		if boundary != "" && index == 0 && id == boundary {
			continue
		}
		if previous != "" && id >= previous {
			if boundary != "" && id >= boundary {
				return fmt.Errorf("page did not move strictly before its Mongo ID boundary")
			}
			return fmt.Errorf("action IDs are not strictly descending")
		}
		previous = id
	}
	return nil
}

// validateTrelloReadOnlyToken verifies the permission boundary reported by
// Trello before HAI reads a board. HAI itself never writes to Trello, but an
// over-privileged token must also be rejected instead of being silently used.
func validateTrelloReadOnlyToken(ctx context.Context, client *trelloAPIClient) error {
	var info trelloTokenInfo
	if err := client.getJSON(ctx, "/1/tokens/"+url.PathEscape(client.token), nil, &info); err != nil {
		return fmt.Errorf("verify Trello token permissions: %w", err)
	}
	expectedMemberID, expectedMemberIDValid := normalizedTrelloMongoID(os.Getenv(trelloAccountMemberIDEnv))
	actualMemberID, actualMemberIDValid := normalizedTrelloMongoID(info.IDMember)
	if !expectedMemberIDValid {
		return fmt.Errorf("%s must be configured with the expected Trello member ID", trelloAccountMemberIDEnv)
	}
	if !actualMemberIDValid || actualMemberID != expectedMemberID {
		return fmt.Errorf("Trello token belongs to a different account than the configured member ID")
	}
	if len(info.Permissions) == 0 {
		return fmt.Errorf("Trello token did not report any permissions; a least-privilege read-only token is required")
	}
	for _, permission := range info.Permissions {
		modelType := firstNonEmpty(strings.TrimSpace(permission.ModelType), "an unknown resource")
		if permission.Write {
			return fmt.Errorf("Trello token has write permission on %s; configure a least-privilege read-only token", modelType)
		}
		if !permission.Read {
			return fmt.Errorf("Trello token lacks read permission on %s", modelType)
		}
	}
	return nil
}

func trelloImportItem(card trelloCard, boardName, listName, projectKey string) ImportItem {
	list := firstNonEmpty(strings.TrimSpace(listName), "(unknown list)")
	cardID := strings.TrimSpace(card.ID)
	if normalized, valid := normalizedTrelloMongoID(cardID); valid {
		cardID = normalized
	}
	labels := trelloLabelNames(card.Labels)
	// Trello's public card route uses its shortLink, not the 24-character object ID.
	provenance := firstNonEmpty(
		trelloCardShortLinkURL(card.ShortLink),
		strings.TrimSpace(card.ShortURL),
		strings.TrimSpace(card.URL),
	)
	if provenance == "" {
		if _, isProviderID := normalizedTrelloMongoID(card.ID); !isProviderID {
			provenance = "https://trello.com/c/" + url.PathEscape(strings.TrimSpace(card.ID))
		}
	}
	lines := []string{
		"Trello card: " + card.Name,
		"Board: " + boardName,
		"List: " + list,
	}
	if start := strings.TrimSpace(card.Start); start != "" {
		lines = append(lines, "Start: "+start)
	}
	if strings.TrimSpace(card.Due) != "" {
		dueStatus := "incomplete"
		if card.DueComplete {
			dueStatus = "completed"
		}
		lines = append(lines, "Due: "+strings.TrimSpace(card.Due)+" ("+dueStatus+")")
	} else if card.DueComplete {
		lines = append(lines, "Due status: completed")
	}
	if members := trelloAssignedMemberNames(card); len(members) > 0 {
		lines = append(lines, "Assigned members: "+strings.Join(members, ", "))
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
		ExternalID: "trello:card:" + cardID,
		Title:      firstNonEmpty(strings.TrimSpace(card.Name), "(untitled card)"),
		Content:    strings.Join(lines, "\n"),
		SourceURI:  provenance,
		ItemType:   "trello_card",
		ProjectKey: projectKey,
		Metadata: fmt.Sprintf("source=trello;board=%s;list=%s;start=%s;due=%s;dueComplete=%t;labels=%s;dateLastActivity=%s;comments=%d;checklists=%d;attachments=%d;members=%d;attachmentContentFetched=false;readonly=true",
			boardName, list, strings.TrimSpace(card.Start), strings.TrimSpace(card.Due), card.DueComplete, labels, strings.TrimSpace(card.DateLastActivity), len(card.Actions), len(card.Checklists), len(card.Attachments), len(trelloAssignedMemberNames(card))),
	}
}

func trelloAssignedMemberNames(card trelloCard) []string {
	memberByID := make(map[string]string, len(card.Members))
	assigned := make([]string, 0, len(card.IDMembers)+len(card.Members))
	unresolvedIDs := make([]string, 0)
	for _, member := range card.Members {
		id := canonicalTrelloMemberID(member.ID)
		fullName := strings.TrimSpace(member.FullName)
		username := strings.TrimSpace(member.Username)
		name := firstNonEmpty(fullName, username)
		if fullName == "" && username != "" {
			name = "@" + username
		} else if fullName != "" && username != "" && !strings.EqualFold(fullName, username) {
			name = fullName + " (@" + username + ")"
		}
		if id == "" {
			if name != "" {
				assigned = append(assigned, name)
			}
			continue
		}
		if previous, exists := memberByID[id]; !exists || strings.ToLower(name) < strings.ToLower(previous) {
			memberByID[id] = name
		}
	}

	seenIDs := make(map[string]struct{}, len(card.IDMembers))
	for _, rawID := range card.IDMembers {
		id := canonicalTrelloMemberID(rawID)
		if id == "" {
			continue
		}
		if _, duplicate := seenIDs[id]; duplicate {
			continue
		}
		seenIDs[id] = struct{}{}
		if name := memberByID[id]; name != "" {
			assigned = append(assigned, name)
		} else {
			unresolvedIDs = append(unresolvedIDs, id)
		}
	}
	if len(card.IDMembers) == 0 {
		for id, name := range memberByID {
			if name != "" {
				assigned = append(assigned, name)
			} else {
				unresolvedIDs = append(unresolvedIDs, id)
			}
		}
	} else {
		for id, name := range memberByID {
			if _, represented := seenIDs[id]; !represented {
				if name != "" {
					assigned = append(assigned, name)
				} else {
					unresolvedIDs = append(unresolvedIDs, id)
				}
			}
		}
	}
	sort.Slice(assigned, func(i, j int) bool {
		left, right := strings.ToLower(assigned[i]), strings.ToLower(assigned[j])
		if left == right {
			return assigned[i] < assigned[j]
		}
		return left < right
	})
	sort.Strings(unresolvedIDs)
	assigned = append(assigned, unresolvedIDs...)
	return assigned
}

func canonicalTrelloMemberID(value string) string {
	value = strings.TrimSpace(value)
	if normalized, valid := normalizedTrelloMongoID(value); valid {
		return normalized
	}
	return value
}

func trelloCardShortLinkURL(value string) string {
	value = strings.TrimSpace(value)
	if len(value) != 8 || !trelloIDPattern.MatchString(value) {
		return ""
	}
	return "https://trello.com/c/" + url.PathEscape(value)
}

func trelloClosedCardImportItem(card trelloCard, boardName, listName, projectKey string) ImportItem {
	item := trelloImportItem(card, boardName, listName, projectKey)
	item.Content = ""
	item.ItemType = trelloClosedCardItemType
	item.Metadata = fmt.Sprintf("source=trello;state=closed;board=%s;list=%s;dateLastActivity=%s;readonly=true",
		boardName, firstNonEmpty(strings.TrimSpace(listName), "(unknown list)"), strings.TrimSpace(card.DateLastActivity))
	return item
}

func appendTrelloComments(lines []string, actions []trelloAction) []string {
	comments := make([]trelloAction, 0, len(actions))
	for _, action := range actions {
		if isTrelloCommentAction(action.Type) && strings.TrimSpace(action.Data.Text) != "" {
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
		if comment.Type == "copyCommentCard" {
			prefix += " (copied comment)"
		}
		lines = append(lines, prefix+": "+strings.TrimSpace(comment.Data.Text))
	}
	return lines
}

func isTrelloCommentAction(actionType string) bool {
	return actionType == "commentCard" || actionType == "copyCommentCard"
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
			if strings.EqualFold(strings.TrimSpace(item.State), "complete") {
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

func extractTrelloChecklistTasks(text string) []string {
	var checklistName string
	var tasks []string
	inChecklists := false
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "Checklists (") {
			inChecklists = true
			continue
		}
		if !inChecklists {
			continue
		}
		if strings.HasPrefix(trimmed, "Attachments (") {
			break
		}
		if strings.HasPrefix(line, "  - [ ] ") {
			item := strings.TrimSpace(strings.TrimPrefix(line, "  - [ ] "))
			if item != "" {
				if checklistName != "" {
					item = checklistName + ": " + item
				}
				tasks = append(tasks, item)
			}
			continue
		}
		if strings.HasPrefix(line, "  - [x] ") || strings.HasPrefix(line, "  - [X] ") {
			continue
		}
		if strings.HasPrefix(line, "- ") {
			checklistName = strings.TrimSpace(strings.TrimPrefix(line, "- "))
		}
	}
	return uniqueStrings(limitValues(tasks, 12))
}

func extractTrelloTasks(text string) []string {
	var prose []string
	inChecklists := false
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "Checklists (") {
			inChecklists = true
			continue
		}
		if inChecklists && strings.HasPrefix(trimmed, "Attachments (") {
			inChecklists = false
		}
		if !inChecklists {
			prose = append(prose, line)
		}
	}
	tasks := extractTasks(normalizeSpaces(strings.Join(prose, " ")))
	tasks = append(tasks, extractTrelloChecklistTasks(text)...)
	return uniqueStrings(limitValues(tasks, 12))
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
		if sourceURL := safeTrelloAttachmentURL(attachment.URL); sourceURL != "" {
			name += ": " + sourceURL
		}
		lines = append(lines, "- "+name)
	}
	return lines
}

func safeTrelloAttachmentURL(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
		return ""
	}
	parsed.RawQuery = ""
	parsed.ForceQuery = false
	parsed.Fragment = ""
	parsed.RawFragment = ""
	return parsed.String()
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

// trelloBoardID accepts a bare board id/shortLink or a canonical Trello board
// URL and returns only the board id used in API requests.
func trelloBoardID(syncTarget string) (string, error) {
	const invalidTarget = "trello syncTarget must be a bare board id or canonical HTTPS Trello board URL"
	raw := strings.TrimSpace(syncTarget)
	if raw == "" {
		return "", fmt.Errorf("%s", invalidTarget)
	}

	if !strings.Contains(raw, "://") {
		if strings.ContainsAny(raw, "/\\?#:") || !trelloIDPattern.MatchString(raw) {
			return "", fmt.Errorf("%s", invalidTarget)
		}
		return raw, nil
	}

	parsed, err := url.Parse(raw)
	if err != nil || !parsed.IsAbs() || !strings.EqualFold(parsed.Scheme, "https") || parsed.Opaque != "" || parsed.User != nil {
		return "", fmt.Errorf("%s", invalidTarget)
	}
	if parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || parsed.RawFragment != "" || strings.Contains(raw, "#") {
		return "", fmt.Errorf("%s", invalidTarget)
	}
	if !strings.EqualFold(parsed.Host, "trello.com") && !strings.EqualFold(parsed.Host, "www.trello.com") {
		return "", fmt.Errorf("%s", invalidTarget)
	}

	segments := strings.Split(parsed.Path, "/")
	if (len(segments) != 3 && len(segments) != 4) || segments[0] != "" || segments[1] != "b" {
		return "", fmt.Errorf("%s", invalidTarget)
	}
	for _, segment := range segments[1:] {
		if segment == "" || segment == "." || segment == ".." || strings.Contains(segment, "\\") {
			return "", fmt.Errorf("%s", invalidTarget)
		}
	}
	boardID := segments[2]
	escapedSegments := strings.Split(parsed.EscapedPath(), "/")
	if !trelloIDPattern.MatchString(boardID) || len(escapedSegments) != len(segments) || escapedSegments[2] != boardID {
		return "", fmt.Errorf("%s", invalidTarget)
	}
	return boardID, nil
}

func canonicalTrelloBoardIDForTarget(requestedBoardID string, board trelloBoard) (string, error) {
	canonicalBoardID, valid := normalizedTrelloMongoID(board.ID)
	if !valid {
		return "", errors.New("Trello board metadata omitted its canonical ID")
	}

	if requestedID, isCanonicalID := normalizedTrelloMongoID(requestedBoardID); isCanonicalID {
		if requestedID != canonicalBoardID {
			return "", errors.New("Trello board metadata does not match the configured board target")
		}
		return canonicalBoardID, nil
	}

	shortLink, err := trelloBoardID(board.ShortURL)
	if err != nil || shortLink != requestedBoardID {
		return "", errors.New("Trello board metadata does not match the configured board target")
	}
	return canonicalBoardID, nil
}

func trelloBaseURL() (*url.URL, error) {
	base := strings.TrimRight(firstNonEmpty(os.Getenv(trelloBaseURLEnv), trelloDefaultBaseURL), "/")
	parsed, err := url.Parse(base)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" ||
		parsed.Opaque != "" || parsed.User != nil || parsed.RawQuery != "" || parsed.ForceQuery ||
		parsed.Fragment != "" || parsed.RawFragment != "" {
		return nil, fmt.Errorf("%s must be an absolute HTTP(S) URL", trelloBaseURLEnv)
	}
	if parsed.Scheme != "https" && !trelloLoopbackHost(parsed.Hostname()) {
		return nil, fmt.Errorf("%s must use HTTPS except for loopback test endpoints", trelloBaseURLEnv)
	}
	if !sourceHTTPHostAllowed(parsed.Hostname()) || (sourceHTTPAddressBlocked(parsed.Hostname()) && !sourceHTTPLoopbackAddressAllowed(sourceHTTPURLAddress(parsed))) {
		return nil, fmt.Errorf("trello API host %s is not allowlisted; add api.trello.com to CONNECTED_SOURCE_HTTP_ALLOWED_HOSTS", parsed.Hostname())
	}
	host := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(parsed.Hostname())), ".")
	if host != "api.trello.com" && !trelloLoopbackHost(host) {
		return nil, errors.New("Trello credentials may only be sent to api.trello.com or a loopback test endpoint")
	}
	if host == "api.trello.com" && parsed.Port() != "" && parsed.Port() != "443" {
		return nil, errors.New("Trello credentials may only be sent to api.trello.com over HTTPS port 443")
	}
	return parsed, nil
}

func trelloLoopbackHost(host string) bool {
	host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// trelloGetJSON issues a single bounded, read-only GET and decodes the body into
// out. Credentials use Trello's documented OAuth Authorization header and are
// never included in returned error messages or query strings.
func trelloGetJSON(base *url.URL, key, token, resourcePath string, query url.Values, out any) error {
	return trelloGetJSONContext(context.Background(), base, key, token, resourcePath, query, out)
}

func trelloGetJSONContext(ctx context.Context, base *url.URL, key, token, resourcePath string, query url.Values, out any) error {
	return trelloGetJSONContextWithBudget(ctx, base, key, token, resourcePath, query, out, nil)
}

func trelloGetJSONContextWithBudget(ctx context.Context, base *url.URL, key, token, resourcePath string, query url.Values, out any, requestsUsed *int) error {
	return trelloGetJSONContextWithSyncBudget(ctx, base, key, token, resourcePath, query, out, requestsUsed, nil)
}

func trelloGetJSONContextWithSyncBudget(ctx context.Context, base *url.URL, key, token, resourcePath string, query url.Values, out any, requestsUsed *int, pageBudget *trelloSyncBudget) error {
	return trelloGetJSONContextWithRateLimitState(ctx, base, key, token, resourcePath, query, out, requestsUsed, pageBudget, nil)
}

func trelloGetJSONContextWithRateLimitState(ctx context.Context, base *url.URL, key, token, resourcePath string, query url.Values, out any, requestsUsed *int, pageBudget *trelloSyncBudget, rateLimit *trelloRateLimitState) error {
	if ctx == nil {
		return errors.New("trello request context is required")
	}
	safeResourcePath := trelloSafeResourcePath(resourcePath)
	target := *base
	// resourcePath may contain an already-escaped opaque path segment (the token
	// verification endpoint). URL.Path is decoded; retain the matching escaped
	// form in RawPath so serialization does not escape percent signs a second time.
	escapedPath := strings.TrimRight(base.EscapedPath(), "/") + resourcePath
	decodedPath, err := url.PathUnescape(escapedPath)
	if err != nil {
		return fmt.Errorf("build trello request for %s failed", safeResourcePath)
	}
	target.Path = decodedPath
	target.RawPath = escapedPath
	requestQuery := cloneTrelloQuery(query)
	requestQuery.Del("key")
	requestQuery.Del("token")
	target.RawQuery = requestQuery.Encode()
	client := &http.Client{
		Timeout:       sourceHTTPTimeout(),
		Transport:     trelloHTTPTransport(base),
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
	}
	var waited time.Duration
	for attempt := 0; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return newProviderSyncError("Trello", 0, err)
		}
		if rateLimit != nil && rateLimit.retryAfter != "" {
			retryable := true
			return newProviderSyncErrorWithOptions("Trello", http.StatusTooManyRequests, errors.New("provider rate-limit window is exhausted"), providerSyncErrorOptions{
				RetryableOverride: &retryable,
				RetryAfterHeader:  rateLimit.retryAfter,
			})
		}
		if requestsUsed != nil {
			if *requestsUsed >= trelloMaxRequestsPerSync {
				message := fmt.Sprintf("Trello sync exceeded its bounded request budget of %d; review the board size before retrying", trelloMaxRequestsPerSync)
				retryable := false
				return newProviderSyncErrorWithOptions("Trello", http.StatusBadRequest, fmt.Errorf("%s: %w", message, errTrelloRequestBudgetExhausted), providerSyncErrorOptions{
					RetryableOverride: &retryable,
					PublicMessage:     message,
				})
			}
			(*requestsUsed)++
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
		if err != nil {
			return fmt.Errorf("build trello request for %s failed", safeResourcePath)
		}
		request.Header.Set("Accept", "application/json")
		request.Header.Set("User-Agent", "HAI-connected-source")
		request.Header.Set("Authorization", trelloAuthorizationHeader(key, token))
		response, err := client.Do(request)
		if err != nil {
			// Do not surface target.String(): it carries the token.
			return newProviderSyncError("Trello", 0, err)
		}
		if response.StatusCode == http.StatusTooManyRequests {
			retryAfter := trelloRetryAfterWithRateLimitHeaders(response.Header.Get("Retry-After"), response.Header)
			// Use the same normalized delay for an immediate retry and durable
			// retry metadata. When Trello omits Retry-After, its safe 10-second
			// window exceeds our short in-process wait budget, so defer the job
			// instead of retrying after the generic 100/200ms backoff.
			delay, canRetry := trelloRateLimitRetryDelay(retryAfter, attempt, waited)
			_ = response.Body.Close() // The body may contain provider details; never surface or log it.
			if attempt >= trelloRateLimitMaxRetries {
				retryable := true
				return newProviderSyncErrorWithOptions("Trello", response.StatusCode, errors.New("bounded rate-limit retries exhausted"), providerSyncErrorOptions{
					RetryableOverride: &retryable,
					RetryAfterHeader:  retryAfter,
				})
			}
			if !canRetry {
				retryable := true
				return newProviderSyncErrorWithOptions("Trello", response.StatusCode, errors.New("bounded rate-limit wait exhausted"), providerSyncErrorOptions{
					RetryableOverride: &retryable,
					RetryAfterHeader:  retryAfter,
				})
			}
			if delay > 0 {
				timer := time.NewTimer(delay)
				select {
				case <-ctx.Done():
					timer.Stop()
					return newProviderSyncError("Trello", response.StatusCode, ctx.Err())
				case <-timer.C:
				}
				waited += delay
			}
			continue
		}
		if response.StatusCode == http.StatusUnauthorized {
			_ = response.Body.Close()
			return newProviderSyncErrorWithOptions("Trello", response.StatusCode, errors.New("configured credentials were rejected"), providerSyncErrorOptions{
				PublicMessage: "Trello rejected the configured API key or token; check the credentials and reconnect if needed",
			})
		}
		if response.StatusCode == http.StatusForbidden {
			_ = response.Body.Close()
			return newProviderSyncErrorWithOptions("Trello", response.StatusCode, errors.New("account lacks access to the requested resource"), providerSyncErrorOptions{
				PublicMessage: "Trello denied access to this board or resource; check token read permission and account membership",
			})
		}
		if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
			retryAfter := ""
			if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= http.StatusInternalServerError {
				retryAfter = strings.TrimSpace(response.Header.Get("Retry-After"))
			}
			_ = response.Body.Close()
			return newProviderSyncErrorWithOptions("Trello", response.StatusCode, errors.New("provider rejected request"), providerSyncErrorOptions{RetryAfterHeader: retryAfter})
		}
		responseLimit := sourceHTTPMaxBytes()
		readLimit, remainingPageBytes := trelloPageResponseReadLimit(responseLimit, pageBudget)
		body, err := io.ReadAll(io.LimitReader(response.Body, readLimit+1))
		closeErr := response.Body.Close()
		if err != nil {
			return newProviderSyncError("Trello", 0, err)
		}
		if closeErr != nil {
			return newProviderSyncError("Trello", 0, closeErr)
		}
		if int64(len(body)) > responseLimit {
			retryable := false
			return newProviderSyncErrorWithOptions("Trello", 0, errors.New("provider response exceeded the configured size limit"), providerSyncErrorOptions{RetryableOverride: &retryable})
		}
		if pageBudget != nil {
			if int64(len(body)) > remainingPageBytes {
				return trelloSyncLimitError(fmt.Sprintf("Trello sync exceeded its aggregate page JSON safety cap of %d bytes; review the board size before retrying", trelloMaxPageBytesPerSync))
			}
			if err := pageBudget.addPageBytes(len(body)); err != nil {
				return fmt.Errorf("read Trello page for %s: %w", safeResourcePath, err)
			}
		}
		if err := json.Unmarshal(body, out); err != nil {
			retryable := false
			return newProviderSyncErrorWithOptions("Trello", 0, errors.New("provider returned malformed JSON"), providerSyncErrorOptions{RetryableOverride: &retryable})
		}
		if rateLimit != nil {
			rateLimit.observe(response.Header)
		}
		return nil
	}
}

func trelloAuthorizationHeader(key, token string) string {
	escape := func(value string) string {
		value = strings.ReplaceAll(value, `\`, `\\`)
		return strings.ReplaceAll(value, `"`, `\"`)
	}
	return `OAuth oauth_consumer_key="` + escape(key) + `", oauth_token="` + escape(token) + `"`
}

func trelloRetryAfterHeader(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		// Trello documents 10-second API-key/token windows but does not
		// guarantee a Retry-After header. Defer to the durable retry queue for
		// one full window rather than polling the rate-limited endpoint.
		return "10"
	}
	if _, valid, _ := parseProviderRetryAfter(value, time.Now().UTC()); !valid {
		return "10"
	}
	return value
}

func trelloRetryAfterWithRateLimitHeaders(retryAfter string, headers http.Header) string {
	retryAfter = trelloRetryAfterHeader(retryAfter)
	now := time.Now().UTC()
	baseDelay, valid, _ := parseProviderRetryAfter(retryAfter, now)
	windowRetryAfter, exhausted := trelloRateLimitWindowRetryAfter(headers)
	if !exhausted {
		return retryAfter
	}
	windowDelay, validWindow, _ := parseProviderRetryAfter(windowRetryAfter, now)
	if validWindow && (!valid || windowDelay > baseDelay) {
		return windowRetryAfter
	}
	return retryAfter
}

// trelloRateLimitWindowRetryAfter converts Trello's documented per-token and
// per-key remaining/interval headers into a conservative durable retry delay.
// The API reports the interval but not a reset timestamp, so waiting one full
// interval is safer than guessing how much of the current window remains.
func trelloRateLimitWindowRetryAfter(headers http.Header) (string, bool) {
	var longestIntervalMS int64
	exhausted := false
	for _, scope := range []string{"api-token", "api-key"} {
		remainingHeader := "x-rate-limit-" + scope + "-remaining"
		remainingRaw := strings.TrimSpace(headers.Get(remainingHeader))
		if remainingRaw == "" {
			continue
		}
		remaining, err := strconv.ParseInt(remainingRaw, 10, 64)
		if err != nil || remaining > 0 {
			continue
		}
		exhausted = true
		intervalHeader := "x-rate-limit-" + scope + "-interval-ms"
		intervalMS, err := strconv.ParseInt(strings.TrimSpace(headers.Get(intervalHeader)), 10, 64)
		if err != nil || intervalMS <= 0 {
			intervalMS = 10_000 // Trello's documented standard key/token window.
		}
		if intervalMS > longestIntervalMS {
			longestIntervalMS = intervalMS
		}
	}
	if !exhausted {
		return "", false
	}
	seconds := longestIntervalMS / 1000
	if longestIntervalMS%1000 != 0 {
		seconds++
	}
	if seconds < 1 {
		seconds = 1
	}
	return strconv.FormatInt(seconds, 10), true
}

func trelloSafeResourcePath(resourcePath string) string {
	const tokenPathPrefix = "/1/tokens/"
	if strings.HasPrefix(resourcePath, tokenPathPrefix) {
		return tokenPathPrefix + "[redacted]"
	}
	return resourcePath
}

func trelloRateLimitRetryDelay(retryAfter string, retryIndex int, alreadyWaited time.Duration) (time.Duration, bool) {
	remaining := trelloRateLimitMaxWait - alreadyWaited
	if remaining < 0 {
		return 0, false
	}
	delay := trelloRateLimitBaseDelay << retryIndex
	if raw := strings.TrimSpace(retryAfter); raw != "" {
		if seconds, err := strconv.ParseInt(raw, 10, 64); err == nil {
			if seconds < 0 || seconds > int64(remaining/time.Second) {
				return 0, false
			}
			delay = time.Duration(seconds) * time.Second
		} else if retryAt, err := http.ParseTime(raw); err == nil {
			delay = time.Until(retryAt)
			if delay < 0 {
				delay = 0
			}
			if delay > remaining {
				return 0, false
			}
		}
	}
	return delay, delay <= remaining
}

// parseTrelloTime accepts either a Trello activity timestamp or a cursor.
func parseTrelloTime(value string) (time.Time, bool) {
	clean := strings.TrimSpace(value)
	if strings.HasPrefix(clean, "trello:") {
		activity, _, ok, err := parseTrelloCursor(clean)
		return activity, ok && err == nil
	}
	return parseTrelloActivityTime(clean)
}

func parseTrelloActivityTime(value string) (time.Time, bool) {
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

func encodeTrelloCursor(activity time.Time, _ map[string]struct{}) (string, error) {
	// A timestamp-only cursor is lossless for incremental processing because the
	// full board is fetched each time: replay every card at the high-water time,
	// then rely on stable external-ID upserts to avoid duplicate source records.
	// Its size is independent of the number of cards sharing that timestamp.
	cursor := trelloCursorV2Prefix
	if !activity.IsZero() {
		cursor += activity.UTC().Format(time.RFC3339Nano)
	}
	if len(cursor) > trelloCursorMaxLength {
		return "", fmt.Errorf("versioned cursor exceeds the %d-character persistence limit", trelloCursorMaxLength)
	}
	return cursor, nil
}

// parseTrelloCursor accepts legacy RFC3339 timestamps, version-one cursors
// containing timestamp/card IDs, and the bounded timestamp-only version two.
func parseTrelloCursor(value string) (time.Time, map[string]struct{}, bool, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, nil, false, nil
	}
	if !strings.HasPrefix(value, "trello:") {
		activity, ok := parseTrelloActivityTime(value)
		if ok {
			return activity, nil, true, nil
		}
		// Older generic source syncs stored `<RFC3339>:<item-count>` when an
		// adapter had no cursor yet. Accept that exact legacy shape for migration,
		// but never interpret an arbitrary non-empty value as a fresh source.
		separator := strings.LastIndexByte(value, ':')
		if separator > 0 {
			count, countErr := strconv.Atoi(value[separator+1:])
			legacyActivity, legacyOK := parseTrelloActivityTime(value[:separator])
			if countErr == nil && count >= 0 && legacyOK {
				return legacyActivity, nil, true, nil
			}
		}
		return time.Time{}, nil, false, fmt.Errorf("invalid legacy activity cursor")
	}
	if len(value) > trelloCursorMaxLength {
		return time.Time{}, nil, false, fmt.Errorf("versioned cursor exceeds the %d-character persistence limit", trelloCursorMaxLength)
	}
	if strings.HasPrefix(value, trelloCursorV2Prefix) {
		activityText := strings.TrimPrefix(value, trelloCursorV2Prefix)
		if activityText == "" {
			return time.Time{}, nil, true, nil
		}
		activity, ok := parseTrelloActivityTime(activityText)
		if !ok {
			return time.Time{}, nil, false, fmt.Errorf("version-two cursor has an invalid activity timestamp")
		}
		return activity, nil, true, nil
	}
	if !strings.HasPrefix(value, trelloCursorV1Prefix) {
		return time.Time{}, nil, false, fmt.Errorf("unsupported cursor version")
	}
	versioned := strings.TrimPrefix(value, trelloCursorV1Prefix)
	if strings.HasPrefix(versioned, "h|") {
		parts := strings.SplitN(strings.TrimPrefix(versioned, "h|"), "|", 2)
		if len(parts) != 2 {
			return time.Time{}, nil, false, fmt.Errorf("invalid compact versioned cursor")
		}
		activity, ok := parseTrelloActivityTime(parts[0])
		if !ok {
			return time.Time{}, nil, false, fmt.Errorf("versioned cursor has an invalid activity timestamp")
		}
		binaryIDs, err := base64.RawURLEncoding.DecodeString(parts[1])
		if err != nil {
			return time.Time{}, nil, false, fmt.Errorf("decode compact cursor IDs: %w", err)
		}
		if len(binaryIDs)%trelloCardIDByteLength != 0 {
			return time.Time{}, nil, false, fmt.Errorf("compact cursor contains an incomplete card ID")
		}
		ids := make(map[string]struct{}, len(binaryIDs)/trelloCardIDByteLength)
		for offset := 0; offset < len(binaryIDs); offset += trelloCardIDByteLength {
			ids[hex.EncodeToString(binaryIDs[offset:offset+trelloCardIDByteLength])] = struct{}{}
		}
		return activity, ids, true, nil
	}
	if !strings.HasPrefix(versioned, "j|") {
		return time.Time{}, nil, false, fmt.Errorf("invalid versioned cursor encoding")
	}
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(versioned, "j|"))
	if err != nil {
		return time.Time{}, nil, false, fmt.Errorf("decode versioned cursor: %w", err)
	}
	var cursor trelloCursorV1
	if err := json.Unmarshal(payload, &cursor); err != nil {
		return time.Time{}, nil, false, fmt.Errorf("decode versioned cursor payload: %w", err)
	}
	activity, ok := parseTrelloActivityTime(cursor.LastActivity)
	if !ok {
		return time.Time{}, nil, false, fmt.Errorf("versioned cursor has an invalid activity timestamp")
	}
	ids := make(map[string]struct{}, len(cursor.CardIDs))
	for _, id := range cursor.CardIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			return time.Time{}, nil, false, fmt.Errorf("versioned cursor contains an empty card ID")
		}
		ids[id] = struct{}{}
	}
	return activity, ids, true, nil
}
