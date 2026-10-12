package source

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"automation-hub-backend/internal/models"

	"github.com/google/uuid"
)

const trelloSyncCapabilityLimitations = "Trello full scans are eventually consistent; cards changed mid-scan are picked up next run. Stale-card checks resume within the request cap. Comment edits/deletions require configured and registered signed HTTPS webhooks; attachment content is not downloaded."

const trelloCardExternalIDPrefix = "trello:card:"

type trelloSyncSlice struct {
	Items                        []ImportItem
	SeenCardExternalIDs          []string
	VerifiedBoardCardExternalIDs []string
	InventoryCheckedCardIDs      []string
	Current                      models.TrelloSyncState
	Next                         models.TrelloSyncState
	Page                         models.TrelloSyncPage
	Receipts                     []models.TrelloActionReceipt
	Complete                     bool
	FinalCursor                  string
}

func (s *service) prepareTrelloSyncSlice(ctx context.Context, source *models.ConnectedSource, job *models.SourceSyncJob) (*trelloSyncSlice, error) {
	repository, ok := s.repo.(trelloSyncStateRepository)
	if !ok {
		return nil, errors.New("durable Trello sync repository is unavailable; apply migration 0095")
	}
	current, err := repository.PrepareTrelloSyncState(source, job, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	boardID, err := trelloBoardID(source.SyncTarget)
	if err != nil {
		return nil, err
	}
	if current.BoardID != boardID || current.OwnerIdentity != source.OwnerIdentity {
		return nil, ErrTrelloSyncBindingChanged
	}
	if !trelloConfigured() {
		return nil, fmt.Errorf("trello connector is not configured; set %s, %s, %s, and a valid %s", trelloAPIKeyEnv, trelloReadTokenEnv, trelloOwnerIdentityEnv, trelloAccountMemberIDEnv)
	}
	if !trelloCredentialsBelongToOwner(source.OwnerIdentity) {
		return nil, errors.New("trello credentials are not assigned to this connected source owner")
	}
	base, err := trelloBaseURL()
	if err != nil {
		return nil, err
	}
	var budget trelloSyncBudget
	client := &trelloAPIClient{
		base: base, key: strings.TrimSpace(os.Getenv(trelloAPIKeyEnv)),
		token: strings.TrimSpace(os.Getenv(trelloReadTokenEnv)), syncBudget: &budget,
	}
	if err := validateTrelloReadOnlyToken(ctx, client); err != nil {
		return nil, err
	}
	var board trelloBoard
	if err := client.getJSON(ctx, "/1/boards/"+boardID, url.Values{"fields": {"id,name,url,shortUrl"}}, &board); err != nil {
		return nil, fmt.Errorf("fetch Trello board metadata: %w", err)
	}
	canonicalBoardID, err := canonicalTrelloBoardIDForTarget(boardID, board)
	if err != nil {
		return nil, fmt.Errorf("verify Trello board identity: %w; refusing card inventory reconciliation", err)
	}
	lists, err := getTrelloJSONArray[trelloList](ctx, client, "/1/boards/"+boardID+"/lists", url.Values{"fields": {"name"}, "filter": {"all"}})
	if err != nil {
		return nil, fmt.Errorf("fetch Trello board lists: %w", err)
	}
	if err := budget.addRecords(len(lists)); err != nil {
		return nil, fmt.Errorf("fetch Trello board lists: %w", err)
	}
	listNames := make(map[string]string, len(lists))
	for _, list := range lists {
		if strings.TrimSpace(list.ID) != "" {
			listNames[list.ID] = list.Name
		}
	}

	slice := &trelloSyncSlice{Current: *current, Next: *current}
	pageFingerprintPayload := any(nil)
	switch current.Phase {
	case trelloPhaseBackfillCards, trelloPhaseIncrementalCards:
		query := url.Values{
			"fields": {trelloCardFields}, "filter": {"all"}, "sort": {"-id"},
			"limit": {strconv.Itoa(trelloCardPageSize)}, "members": {"true"}, "member_fields": {"fullName,username"},
			"attachments":       {"true"},
			"attachment_fields": {trelloAttachmentFields}, "checklists": {"all"},
			"checklist_fields": {trelloChecklistFields}, "checkItem_fields": {trelloCheckItemFields},
		}
		if current.CardCursor != "" {
			query.Set("before", current.CardCursor)
		}
		cards, err := getTrelloJSONArray[trelloCard](ctx, client, "/1/boards/"+boardID+"/cards", query)
		if err != nil {
			return nil, fmt.Errorf("fetch Trello card page: %w", err)
		}
		if err := budget.addRecords(len(cards)); err != nil {
			return nil, err
		}
		if err := validateTrelloDurableCardPage(cards, current.CardCursor, trelloCardPageSize, canonicalBoardID); err != nil {
			return nil, err
		}
		for _, card := range cards {
			slice.SeenCardExternalIDs = append(slice.SeenCardExternalIDs, trelloCardExternalIDPrefix+strings.TrimSpace(card.ID))
			activity, valid := parseTrelloTime(card.DateLastActivity)
			if !valid {
				return nil, fmt.Errorf("Trello card %q has an invalid activity timestamp; refusing to checkpoint its page", firstNonEmpty(card.Name, card.ID))
			}
			slice.Next.MaxCardActivityAt = trelloMaxTimePointer(slice.Next.MaxCardActivityAt, activity)
			if card.Closed {
				slice.Items = append(slice.Items, trelloClosedCardImportItem(card, firstNonEmpty(board.Name, boardID), listNames[card.IDList], source.DefaultProjectKey))
			} else {
				slice.Items = append(slice.Items, trelloImportItem(card, firstNonEmpty(board.Name, boardID), listNames[card.IDList], source.DefaultProjectKey))
			}
		}
		pageAfter := current.CardCursor
		if len(cards) > 0 {
			pageAfter = strings.TrimSpace(cards[len(cards)-1].ID)
		}
		if len(cards) == trelloCardPageSize {
			if pageAfter == current.CardCursor {
				return nil, errors.New("Trello card pagination did not move its durable cursor")
			}
			slice.Next.CardCursor = pageAfter
		} else if current.Phase == trelloPhaseBackfillCards {
			slice.Next.Phase = trelloPhaseBackfillActions
			slice.Next.CardCursor = ""
			slice.Next.ActionCursor = ""
		} else {
			slice.Next.Phase = trelloPhaseReconcileInventory
			slice.Next.CardCursor = ""
			slice.Next.ActionCursor = ""
		}
		pageFingerprintPayload = cards
		slice.Page = trelloPageLedger(source.ID, job.ID, *current, slice.Next, len(cards), client.requestsUsed, budget.pageBytes, pageFingerprintPayload)
		slice.Page.CursorAfter = pageAfter
	case trelloPhaseBackfillActions, trelloPhaseCatchUpActions, trelloPhaseIncrementalActions:
		query := url.Values{
			"filter": {trelloCommentActionTypes}, "fields": {trelloActionFields},
			"limit": {strconv.Itoa(trelloActionPageSize)}, "memberCreator": {"true"},
			"memberCreator_fields": {"fullName,username"},
		}
		if current.ActionCursor != "" {
			query.Set("before", current.ActionCursor)
		}
		if current.Phase != trelloPhaseBackfillActions {
			since := current.ActionSince
			if since == nil && current.LastSuccessfulAt != nil {
				value := current.LastSuccessfulAt.Add(-trelloActionOverlap)
				since = &value
			}
			if since == nil {
				value := current.CycleStartedAt.Add(-trelloActionOverlap)
				since = &value
			}
			query.Set("since", since.UTC().Format(time.RFC3339))
		}
		actions, err := getTrelloJSONArray[trelloAction](ctx, client, "/1/boards/"+boardID+"/actions", query)
		if err != nil {
			return nil, fmt.Errorf("fetch Trello comment-action page: %w", err)
		}
		if len(actions) > trelloActionPageSize {
			return nil, errors.New("Trello action page exceeded the requested limit")
		}
		if err := budget.addRecords(len(actions)); err != nil {
			return nil, err
		}
		if err := validateTrelloActionPageOrder(actions, current.ActionCursor); err != nil {
			return nil, err
		}
		seen := make(map[string]struct{}, len(actions))
		for index := range actions {
			action := &actions[index]
			action.ID = strings.TrimSpace(action.ID)
			if normalized, valid := normalizedTrelloMongoID(action.ID); valid {
				action.ID = normalized
			}
			cardID, validCardID := normalizedTrelloMongoID(action.Data.Card.ID)
			if !isTrelloCommentAction(action.Type) || action.ID == "" || cardID == "" {
				return nil, errors.New("Trello comment-action page contains an incomplete action; refusing to checkpoint")
			}
			if _, valid := normalizedTrelloMongoID(action.ID); !valid {
				return nil, errors.New("Trello comment-action page contains an invalid action ID")
			}
			if !validCardID {
				return nil, errors.New("Trello comment-action page contains an invalid card ID")
			}
			action.Data.Card.ID = cardID
			if action.ID == current.ActionCursor && index == 0 {
				continue
			}
			if _, duplicate := seen[action.ID]; duplicate {
				return nil, errors.New("Trello comment-action page repeated a non-boundary action")
			}
			seen[action.ID] = struct{}{}
			occurredAt, valid := parseTrelloTime(action.Date)
			if !valid {
				return nil, fmt.Errorf("Trello action %s has an invalid timestamp", action.ID)
			}
			slice.Next.MaxCardActivityAt = trelloMaxTimePointer(slice.Next.MaxCardActivityAt, occurredAt)
			encoded, err := json.Marshal(action)
			if err != nil {
				return nil, fmt.Errorf("fingerprint Trello action %s: %w", action.ID, err)
			}
			digest := sha256.Sum256(encoded)
			receiptHash := hex.EncodeToString(digest[:])
			slice.Receipts = append(slice.Receipts, models.TrelloActionReceipt{
				SourceID: source.ID, ActionID: action.ID, CardID: cardID, OccurredAt: occurredAt, Fingerprint: receiptHash,
			})
			slice.Items = append(slice.Items, trelloCommentImportItem(*action, board, source.DefaultProjectKey))
		}
		pageAfter := current.ActionCursor
		if len(actions) > 0 {
			pageAfter = strings.TrimSpace(actions[len(actions)-1].ID)
		}
		if len(actions) == trelloActionPageSize {
			if pageAfter == current.ActionCursor {
				return nil, errors.New("Trello action pagination did not move its durable cursor")
			}
			slice.Next.ActionCursor = pageAfter
		} else if current.Phase == trelloPhaseBackfillActions {
			since := current.CycleStartedAt.Add(-trelloActionOverlap)
			slice.Next.Phase = trelloPhaseCatchUpActions
			slice.Next.ActionCursor = ""
			slice.Next.ActionSince = &since
		} else {
			slice.Next.Phase = trelloPhaseIncrementalCards
			slice.Next.ActionCursor = ""
		}
		pageFingerprintPayload = actions
		slice.Page = trelloPageLedger(source.ID, job.ID, *current, slice.Next, len(actions), client.requestsUsed, budget.pageBytes, pageFingerprintPayload)
		slice.Page.CursorAfter = pageAfter
	case trelloPhaseReconcileInventory:
		more, err := s.reconcileTrelloCardInventory(ctx, source, slice, client, canonicalBoardID)
		if err != nil {
			return nil, err
		}
		slice.Page = trelloPageLedger(source.ID, job.ID, *current, slice.Next, len(slice.InventoryCheckedCardIDs), client.requestsUsed, budget.pageBytes, slice.InventoryCheckedCardIDs)
		slice.Page.CursorAfter = slice.Next.CardCursor
		if !more {
			slice.Complete = true
			if err := finalizeTrelloSyncSlice(slice, time.Now().UTC()); err != nil {
				return nil, err
			}
		}
	default:
		return nil, fmt.Errorf("Trello sync checkpoint has unsupported phase %q", current.Phase)
	}

	slice.Next.PagesProcessed++
	// Progress counts provider records, not synthetic state observations added
	// during final inventory reconciliation.
	slice.Next.RecordsProcessed += int64(slice.Page.RecordCount)
	return slice, nil
}

func finalizeTrelloSyncSlice(slice *trelloSyncSlice, now time.Time) error {
	if slice == nil || now.IsZero() {
		return errors.New("Trello completion state is incomplete")
	}
	slice.Next.Phase = trelloPhaseIdle
	slice.Next.LogicalJobID = nil
	slice.Next.CardCursor = ""
	slice.Next.ActionCursor = ""
	slice.Next.LastSuccessfulAt = &now
	slice.Next.ActionSince = nil
	cursorTime := now
	if slice.Next.MaxCardActivityAt != nil && slice.Next.MaxCardActivityAt.After(cursorTime) {
		cursorTime = *slice.Next.MaxCardActivityAt
	}
	var err error
	slice.FinalCursor, err = encodeTrelloCursor(cursorTime, nil)
	if err != nil {
		return fmt.Errorf("encode completed Trello cursor: %w", err)
	}
	return nil
}

func (s *service) reconcileTrelloCardInventory(ctx context.Context, source *models.ConnectedSource, slice *trelloSyncSlice, client *trelloAPIClient, boardID string) (bool, error) {
	if source == nil || slice == nil || source.ID == uuid.Nil || slice.Current.Phase != trelloPhaseReconcileInventory {
		return false, errors.New("Trello inventory reconciliation state is incomplete")
	}
	if client == nil || strings.TrimSpace(boardID) == "" {
		return false, errors.New("Trello final inventory verification is unavailable")
	}
	repository, ok := s.repo.(trelloSyncStateRepository)
	if !ok {
		return false, errors.New("durable Trello card inventory repository is unavailable")
	}
	remainingRequests := trelloMaxRequestsPerSync - client.requestsUsed
	if remainingRequests < 1 {
		return true, nil
	}
	queryLimit := remainingRequests + 1
	if queryLimit > trelloMaxRecordsPerSync {
		queryLimit = trelloMaxRecordsPerSync
	}
	existing, err := repository.FindTrelloStaleCardsAfter(source.ID, slice.Current.CycleStartedAt, slice.Current.CardCursor, queryLimit)
	if err != nil {
		return false, fmt.Errorf("load stale Trello cards for final inventory reconciliation: %w", err)
	}
	more := len(existing) > remainingRequests
	if more {
		existing = existing[:remainingRequests]
	}
	sort.Slice(existing, func(i, j int) bool { return existing[i].ExternalID < existing[j].ExternalID })
	for _, raw := range existing {
		if !strings.HasPrefix(raw.ExternalID, trelloCardExternalIDPrefix) {
			return false, errors.New("Trello stale-card query returned a non-card identity")
		}
		cardID, validCardID := normalizedTrelloMongoID(strings.TrimPrefix(raw.ExternalID, trelloCardExternalIDPrefix))
		if !validCardID {
			return false, errors.New("stored Trello card has an invalid identity; refusing to build a provider request")
		}
		var card trelloCard
		lookupErr := client.getJSON(ctx, "/1/cards/"+cardID, url.Values{"fields": {"id,idBoard"}}, &card)
		onConfiguredBoard := false
		if lookupErr != nil {
			// Retries consume the same attempt budget as reads. Persist the
			// verified prefix and resume this unverified card in the next slice.
			if errors.Is(lookupErr, errTrelloRequestBudgetExhausted) {
				return true, nil
			}
			var providerErr *providerSyncError
			if !errors.As(lookupErr, &providerErr) || providerErr.upstreamStatus != http.StatusNotFound {
				return false, fmt.Errorf("verify Trello card availability before final inventory reconciliation: %w", lookupErr)
			}
		} else {
			verifiedCardID, validVerifiedCardID := normalizedTrelloMongoID(card.ID)
			verifiedBoardID, validVerifiedBoardID := normalizedTrelloMongoID(card.IDBoard)
			if !validVerifiedCardID || verifiedCardID != cardID || !validVerifiedBoardID {
				return false, errors.New("Trello card verification returned an incomplete or mismatched identity; refusing to checkpoint the inventory")
			}
			if verifiedBoardID == boardID {
				slice.VerifiedBoardCardExternalIDs = append(slice.VerifiedBoardCardExternalIDs, raw.ExternalID)
				onConfiguredBoard = true
			}
		}
		if !onConfiguredBoard {
			slice.Items = append(slice.Items, ImportItem{
				ExternalID: raw.ExternalID,
				Title:      raw.Title,
				SourceURI:  raw.SourceURI,
				ItemType:   trelloUnavailableCardItemType,
				ProjectKey: raw.ProjectKey,
				Metadata:   "source=trello;state=not_on_configured_board;content_retained=true;readonly=true",
			})
		}
		slice.InventoryCheckedCardIDs = append(slice.InventoryCheckedCardIDs, cardID)
		slice.Next.CardCursor = cardID
	}
	return more, nil
}

func trelloPageLedger(sourceID, jobID uuid.UUID, current, next models.TrelloSyncState, records, requests int, responseBytes int64, payload any) models.TrelloSyncPage {
	encoded, _ := json.Marshal(payload)
	digest := sha256.Sum256(encoded)
	cursorBefore := current.CardCursor
	cursorAfter := next.CardCursor
	if strings.Contains(current.Phase, "action") {
		cursorBefore = current.ActionCursor
		cursorAfter = next.ActionCursor
	}
	return models.TrelloSyncPage{
		SourceID: sourceID, LogicalJobID: jobID, Generation: current.Generation,
		Phase: current.Phase, CursorBefore: cursorBefore, CursorAfter: cursorAfter,
		RecordCount: records, RequestCount: requests, ResponseBytes: responseBytes,
		Fingerprint: hex.EncodeToString(digest[:]),
	}
}

func validateTrelloDurableCardPage(cards []trelloCard, before string, pageSize int, expectedBoardID string) error {
	if len(cards) > pageSize {
		return errors.New("Trello card page exceeded the requested limit")
	}
	expectedBoardID, validExpectedBoardID := normalizedTrelloMongoID(expectedBoardID)
	if !validExpectedBoardID {
		return errors.New("Trello card page requires a verified canonical board ID")
	}
	seen := make(map[string]struct{}, len(cards))
	previous := ""
	for index := range cards {
		id := strings.TrimSpace(cards[index].ID)
		key, valid := normalizedTrelloMongoID(id)
		if id == "" || !valid {
			return errors.New("Trello card page contains an invalid ID")
		}
		cardBoardID, validBoardID := normalizedTrelloMongoID(cards[index].IDBoard)
		if !validBoardID || cardBoardID != expectedBoardID {
			return errors.New("Trello card page contains a card from a different or unverified board")
		}
		cards[index].ID = key
		cards[index].IDBoard = cardBoardID
		id = key
		if previous != "" {
			previousKey, previousValid := normalizedTrelloMongoID(previous)
			if valid && previousValid && key >= previousKey {
				return errors.New("Trello card page is not strictly ordered by descending Mongo ID")
			}
		}
		previous = id
		if id == before && index == 0 {
			continue
		}
		if _, exists := seen[id]; exists {
			return errors.New("Trello card page repeated a non-boundary card")
		}
		if before != "" {
			beforeKey, beforeValid := normalizedTrelloMongoID(before)
			if !valid || !beforeValid || key >= beforeKey {
				return errors.New("Trello card page did not move strictly before its durable cursor")
			}
		}
		seen[id] = struct{}{}
	}
	return nil
}

func trelloCommentImportItem(action trelloAction, board trelloBoard, projectKey string) ImportItem {
	author := firstNonEmpty(strings.TrimSpace(action.MemberCreator.FullName), strings.TrimSpace(action.MemberCreator.Username), strings.TrimSpace(action.IDMemberCreator), "unknown member")
	cardID := strings.TrimSpace(action.Data.Card.ID)
	cardName := firstNonEmpty(strings.TrimSpace(action.Data.Card.Name), "Trello card")
	provenance := firstNonEmpty(strings.TrimSpace(action.Data.Card.ShortLink), "")
	if provenance != "" {
		provenance = "https://trello.com/c/" + url.PathEscape(provenance)
	} else {
		provenance = firstNonEmpty(strings.TrimSpace(action.Data.Card.URL), strings.TrimSpace(board.ShortURL), strings.TrimSpace(board.URL))
	}
	content := fmt.Sprintf("Trello comment on %s (card %s)\nAuthor: %s\nDate: %s\n\n%s",
		cardName, cardID, author, strings.TrimSpace(action.Date), strings.TrimSpace(action.Data.Text))
	return ImportItem{
		ExternalID: "trello:action:" + strings.TrimSpace(action.ID),
		Title:      firstNonEmpty(cardName+" comment", "Trello comment"), Content: content,
		SourceURI: provenance, ItemType: "trello_comment", ProjectKey: projectKey,
		Metadata: fmt.Sprintf("source=trello;action=%s;card=%s;actionType=%s;commentEditsAndDeletes=webhook_required;attachmentContentFetched=false;readonly=true",
			strings.TrimSpace(action.ID), cardID, strings.TrimSpace(action.Type)),
	}
}

func trelloMaxTimePointer(current *time.Time, candidate time.Time) *time.Time {
	if current == nil || candidate.After(*current) {
		value := candidate.UTC()
		return &value
	}
	return current
}

func trelloPageProgress(job *models.SourceSyncJob, next *models.TrelloSyncState, complete bool) {
	job.ProgressPages = next.PagesProcessed
	job.ProgressRecords = boundedSyncCount(int(next.RecordsProcessed))
	if complete {
		job.ProgressPhase = "complete"
		job.ProgressMessage = trelloSyncCapabilityLimitations
		return
	}
	job.ProgressPhase = next.Phase
	job.ProgressMessage = fmt.Sprintf("%s: %d committed page(s), %d source records. %s", strings.ReplaceAll(next.Phase, "_", " "), next.PagesProcessed, next.RecordsProcessed, trelloSyncCapabilityLimitations)
}
