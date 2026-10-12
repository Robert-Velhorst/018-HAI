package source

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"

	"automation-hub-backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	trelloCommentCreatedItemType = "trello_comment_created"
	trelloCommentDeletedItemType = "trello_comment_deleted"
)

func isTrelloCommentSourceItemType(value string) bool {
	return value == "trello_comment" || value == trelloCommentCreatedItemType
}

type trelloWebhookCommentRefresh struct {
	Receipt         *models.TrelloWebhookReceipt
	CardID          string
	ActiveActionIDs map[string]struct{}
	Items           []ImportItem
}

type trelloWebhookCommentArchiveCandidate struct {
	raw        models.SourceRawItem
	extraction *models.SourceExtraction
	actionID   string
}

func validateTrelloWebhookCommentRefresh(source *models.ConnectedSource, receipt *models.TrelloWebhookReceipt, repo Repository) error {
	if source == nil || receipt == nil || source.ID == uuid.Nil || receipt.ID == uuid.Nil || receipt.SourceID != source.ID {
		return errors.New("source and receipt identity do not match")
	}
	if receipt.ActionType != "updateComment" && receipt.ActionType != "deleteComment" {
		return errors.New("only comment edit and delete callbacks can request a per-card refresh")
	}
	if _, valid := normalizedTrelloMongoID(receipt.ActionID); !valid {
		return errors.New("webhook action identity is invalid")
	}
	if _, valid := normalizedTrelloMongoID(receipt.CardID); !valid {
		return errors.New("webhook card identity is invalid")
	}
	if !trelloWebhookSourceMatches(source, []string{receipt.BoardID}, repo) {
		return errors.New("webhook board does not match the connected source")
	}
	if !trelloCredentialsBelongToOwner(source.OwnerIdentity) {
		return errors.New("Trello credentials are not assigned to this connected source owner")
	}
	return nil
}

// fetchTrelloWebhookCommentRefresh reads one card and its complete current
// comment-action set. It must run inside syncContext's source lease, and returns
// no items unless every identity and page has been verified.
func (s *service) fetchTrelloWebhookCommentRefresh(ctx context.Context, source *models.ConnectedSource, receipt *models.TrelloWebhookReceipt) (trelloWebhookCommentRefresh, error) {
	if err := validateTrelloWebhookCommentRefresh(source, receipt, s.repo); err != nil {
		return trelloWebhookCommentRefresh{}, err
	}
	cardID, _ := normalizedTrelloMongoID(receipt.CardID)
	if !trelloConfigured() {
		return trelloWebhookCommentRefresh{}, fmt.Errorf("Trello connector is not configured; set %s, %s, %s, and a valid %s", trelloAPIKeyEnv, trelloReadTokenEnv, trelloOwnerIdentityEnv, trelloAccountMemberIDEnv)
	}
	boardTarget, err := trelloBoardID(source.SyncTarget)
	if err != nil {
		return trelloWebhookCommentRefresh{}, err
	}
	base, err := trelloBaseURL()
	if err != nil {
		return trelloWebhookCommentRefresh{}, err
	}
	var budget trelloSyncBudget
	client := &trelloAPIClient{
		base: base, key: strings.TrimSpace(os.Getenv(trelloAPIKeyEnv)),
		token: strings.TrimSpace(os.Getenv(trelloReadTokenEnv)), syncBudget: &budget,
	}
	if err := validateTrelloReadOnlyToken(ctx, client); err != nil {
		return trelloWebhookCommentRefresh{}, err
	}
	var board trelloBoard
	if err := client.getJSON(ctx, "/1/boards/"+boardTarget, url.Values{"fields": {"id,name,url,shortUrl"}}, &board); err != nil {
		return trelloWebhookCommentRefresh{}, fmt.Errorf("fetch Trello board metadata for comment refresh: %w", err)
	}
	canonicalBoardID, err := canonicalTrelloBoardIDForTarget(boardTarget, board)
	if err != nil {
		return trelloWebhookCommentRefresh{}, fmt.Errorf("verify Trello board identity for comment refresh: %w", err)
	}
	if !trelloWebhookBoardIDMatches(receipt.BoardID, boardTarget) && !trelloWebhookBoardIDMatches(receipt.BoardID, canonicalBoardID) {
		return trelloWebhookCommentRefresh{}, errors.New("webhook board does not resolve to the configured canonical board")
	}

	cardQuery := url.Values{
		"fields":            {trelloCardFields},
		"members":           {"true"},
		"member_fields":     {"fullName,username"},
		"attachments":       {"true"},
		"attachment_fields": {trelloAttachmentFields},
		"checklists":        {"all"},
		"checklist_fields":  {trelloChecklistFields},
		"checkItem_fields":  {trelloCheckItemFields},
	}
	var card trelloCard
	if err := client.getJSON(ctx, "/1/cards/"+cardID, cardQuery, &card); err != nil {
		return trelloWebhookCommentRefresh{}, fmt.Errorf("fetch Trello card for comment refresh: %w", err)
	}
	verifiedCardID, validCardID := normalizedTrelloMongoID(card.ID)
	verifiedCardBoardID, validCardBoardID := normalizedTrelloMongoID(card.IDBoard)
	if !validCardID || verifiedCardID != cardID || !validCardBoardID || verifiedCardBoardID != canonicalBoardID {
		return trelloWebhookCommentRefresh{}, errors.New("Trello card response did not match the webhook card and configured board")
	}
	listID, validListID := normalizedTrelloMongoID(card.IDList)
	if !validListID {
		return trelloWebhookCommentRefresh{}, errors.New("Trello card response omitted a valid list identity")
	}
	card.ID = verifiedCardID
	card.IDBoard = verifiedCardBoardID
	card.IDList = listID
	if err := budget.addRecords(1); err != nil {
		return trelloWebhookCommentRefresh{}, err
	}
	var list trelloList
	if err := client.getJSON(ctx, "/1/lists/"+listID, url.Values{"fields": {"id,name"}}, &list); err != nil {
		return trelloWebhookCommentRefresh{}, fmt.Errorf("fetch Trello card list for comment refresh: %w", err)
	}
	verifiedListID, validVerifiedListID := normalizedTrelloMongoID(list.ID)
	if !validVerifiedListID || verifiedListID != listID {
		return trelloWebhookCommentRefresh{}, errors.New("Trello list response did not match the card list identity")
	}
	if err := budget.addRecords(1); err != nil {
		return trelloWebhookCommentRefresh{}, err
	}

	actions, err := fetchTrelloCardCommentActions(ctx, client, cardID, &budget)
	if err != nil {
		return trelloWebhookCommentRefresh{}, err
	}
	card.Actions = actions
	items := make([]ImportItem, 0, len(actions)+1)
	activeActionIDs := make(map[string]struct{}, len(actions))
	for _, action := range actions {
		activeActionIDs[action.ID] = struct{}{}
		items = append(items, trelloCommentImportItem(action, board, source.DefaultProjectKey))
	}
	if card.Closed {
		items = append(items, trelloClosedCardImportItem(card, firstNonEmpty(board.Name, canonicalBoardID), list.Name, source.DefaultProjectKey))
	} else {
		items = append(items, trelloImportItem(card, firstNonEmpty(board.Name, canonicalBoardID), list.Name, source.DefaultProjectKey))
	}
	return trelloWebhookCommentRefresh{
		Receipt: receipt, CardID: cardID, ActiveActionIDs: activeActionIDs, Items: items,
	}, nil
}

func fetchTrelloCardCommentActions(ctx context.Context, client *trelloAPIClient, cardID string, budget *trelloSyncBudget) ([]trelloAction, error) {
	baseQuery := url.Values{
		"filter":               {trelloCommentActionTypes},
		"fields":               {trelloActionFields},
		"limit":                {strconv.Itoa(trelloActionPageSize)},
		"memberCreator":        {"true"},
		"memberCreator_fields": {"fullName,username"},
	}
	actions := make([]trelloAction, 0)
	seen := make(map[string]struct{})
	before := ""
	for pageNumber := 0; ; pageNumber++ {
		query := cloneTrelloQuery(baseQuery)
		if before != "" {
			query.Set("before", before)
		}
		page, err := getTrelloJSONArray[trelloAction](ctx, client, "/1/cards/"+cardID+"/actions", query)
		if err != nil {
			return nil, fmt.Errorf("read Trello card comment-action page %d: %w", pageNumber, err)
		}
		if len(page) > trelloActionPageSize {
			return nil, fmt.Errorf("Trello card comment-action page %d exceeded the requested limit", pageNumber)
		}
		if err := budget.addRecords(len(page)); err != nil {
			return nil, fmt.Errorf("Trello card comment-action page %d: %w", pageNumber, err)
		}
		if err := validateTrelloActionPageOrder(page, before); err != nil {
			return nil, fmt.Errorf("validate Trello card comment-action page %d: %w", pageNumber, err)
		}
		if len(page) == 0 {
			return actions, nil
		}
		newActions := 0
		for index := range page {
			action := &page[index]
			actionID, validActionID := normalizedTrelloMongoID(action.ID)
			if !validActionID {
				return nil, fmt.Errorf("Trello card comment-action page %d contains an invalid action identity", pageNumber)
			}
			if before != "" && index == 0 && actionID == before {
				continue
			}
			if _, duplicate := seen[actionID]; duplicate {
				return nil, fmt.Errorf("Trello card comment-action page %d repeated a non-boundary action", pageNumber)
			}
			if !isTrelloCommentAction(action.Type) {
				return nil, fmt.Errorf("Trello card comment-action page %d returned an unsupported action type", pageNumber)
			}
			actualCardID, validActionCardID := normalizedTrelloMongoID(action.Data.Card.ID)
			if !validActionCardID || actualCardID != cardID {
				return nil, fmt.Errorf("Trello card comment-action %s did not belong to the requested card", actionID)
			}
			if _, validDate := parseTrelloTime(action.Date); !validDate {
				return nil, fmt.Errorf("Trello card comment-action %s has an invalid date", actionID)
			}
			action.ID = actionID
			action.Data.Card.ID = actualCardID
			seen[actionID] = struct{}{}
			actions = append(actions, *action)
			newActions++
		}
		if len(page) < trelloActionPageSize {
			return actions, nil
		}
		if newActions == 0 {
			return nil, errors.New("Trello card comment-action pagination made no progress")
		}
		nextBefore, validBoundary := normalizedTrelloMongoID(page[len(page)-1].ID)
		if !validBoundary || nextBefore == before {
			return nil, errors.New("Trello card comment-action pagination has no stable next boundary")
		}
		before = nextBefore
	}
}

func (s *service) archiveMissingTrelloWebhookComments(ctx context.Context, source *models.ConnectedSource, refresh *trelloWebhookCommentRefresh) error {
	if source == nil || refresh == nil || refresh.Receipt == nil || refresh.CardID == "" {
		return errors.New("Trello comment refresh identity is incomplete")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	rawItems, err := s.repo.FindRawItems(source.ID)
	if err != nil {
		return fmt.Errorf("load current Trello comment records: %w", err)
	}
	candidates := make([]trelloWebhookCommentArchiveCandidate, 0)
	for _, raw := range rawItems {
		if raw.SourceID != source.ID || raw.ID == uuid.Nil || !isTrelloCommentSourceItemType(raw.ItemType) {
			continue
		}
		cardID, validCardID := normalizedTrelloMongoID(trelloWebhookMetadataValue(raw.Metadata, "card"))
		if !validCardID || cardID != refresh.CardID {
			continue
		}
		actionID, validActionID := normalizedTrelloMongoID(strings.TrimPrefix(raw.ExternalID, "trello:action:"))
		metadataActionID, validMetadataActionID := normalizedTrelloMongoID(trelloWebhookMetadataValue(raw.Metadata, "action"))
		// Earlier verified creation callbacks stored the type in action and the
		// stable ID in actionId. Accept only that exact legacy webhook shape.
		if !validMetadataActionID && raw.ItemType == trelloCommentCreatedItemType &&
			trelloWebhookMetadataValue(raw.Metadata, "webhookVerified") == "true" &&
			isTrelloWebhookCommentCreation(trelloWebhookMetadataValue(raw.Metadata, "action")) {
			metadataActionID, validMetadataActionID = normalizedTrelloMongoID(trelloWebhookMetadataValue(raw.Metadata, "actionId"))
		}
		if !strings.HasPrefix(raw.ExternalID, "trello:action:") || !validActionID || !validMetadataActionID || actionID != metadataActionID {
			return errors.New("a stored Trello comment for the affected card has an invalid source action identity; no comments were archived")
		}
		if _, active := refresh.ActiveActionIDs[actionID]; active {
			continue
		}
		extraction, lookupErr := s.repo.FindExtractionByRawItem(raw.ID)
		if errors.Is(lookupErr, gorm.ErrRecordNotFound) {
			continue
		}
		if lookupErr != nil {
			return fmt.Errorf("load Trello comment extraction %s: %w", raw.ID, lookupErr)
		}
		if extraction == nil || extraction.ID == uuid.Nil || extraction.SourceID != source.ID || extraction.RawItemID != raw.ID {
			return errors.New("a Trello comment extraction does not match its source record; no comments were archived")
		}
		candidates = append(candidates, trelloWebhookCommentArchiveCandidate{raw: raw, extraction: extraction, actionID: actionID})
	}

	for _, item := range candidates {
		if err := ctx.Err(); err != nil {
			return err
		}
		release, lockErr := s.acquireSourceExtractionLocks(ctx, source.ID, source.OwnerIdentity, item.extraction.ID)
		if lockErr != nil {
			return fmt.Errorf("lock Trello comment extraction %s: %w", item.extraction.ID, lockErr)
		}
		archiveErr := s.archiveTrelloCommentForProviderDeletion(source, refresh, item)
		release()
		if archiveErr != nil {
			return archiveErr
		}
	}
	return nil
}

func (s *service) archiveTrelloCommentForProviderDeletion(source *models.ConnectedSource, refresh *trelloWebhookCommentRefresh, item trelloWebhookCommentArchiveCandidate) error {
	extraction, err := s.repo.FindExtraction(item.extraction.ID)
	if err != nil {
		return fmt.Errorf("reload Trello comment extraction %s: %w", item.extraction.ID, err)
	}
	if extraction.SourceID != source.ID || extraction.RawItemID != item.raw.ID {
		return errors.New("Trello comment extraction changed source identity before archive")
	}
	if extraction.Archived {
		return nil
	}
	if !isTrelloCommentSourceItemType(extraction.ContentType) {
		return errors.New("Trello comment extraction changed type before provider deletion archive")
	}
	reason := "Trello's authoritative card action list no longer contains this comment; preserve its source text as archived history"
	if err := s.retractWorkflowForExtraction(extraction, reason); err != nil {
		return fmt.Errorf("retract workflows for deleted Trello comment %s: %w", item.actionID, err)
	}
	extraction.ContentType = trelloCommentDeletedItemType
	extraction.Archived = true
	if _, err := s.repo.SaveExtraction(extraction); err != nil {
		return fmt.Errorf("archive deleted Trello comment %s: %w", item.actionID, err)
	}
	s.audit(source.ID, "source.trello_comment_archived", fmt.Sprintf(
		"card_id=%s; action_id=%s; webhook_action_id=%s; receipt_id=%s; reason=provider_action_absent; raw_retained=true",
		refresh.CardID, item.actionID, refresh.Receipt.ActionID, refresh.Receipt.ID,
	))
	return nil
}
