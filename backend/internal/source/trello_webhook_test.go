package source

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/durablejob"
	"automation-hub-backend/internal/models"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const trelloWebhookTestBody = `{"action":{"id":"aaaaaaaaaaaaaaaaaaaaaaaa","type":"updateComment","date":"2026-09-27T10:30:00.000Z","idMemberCreator":"bbbbbbbbbbbbbbbbbbbbbbbb","data":{"text":"Updated source comment","board":{"id":"cccccccccccccccccccccccc","shortLink":"board123"},"card":{"id":"dddddddddddddddddddddddd","name":"Review the evidence","shortLink":"card1234","url":"https://trello.com/c/card1234/review-the-evidence"}}},"model":{"id":"cccccccccccccccccccccccc","shortLink":"board123"}}`

type trelloWebhookTestService struct {
	Service
	accepted  bool
	acceptErr error
	event     trelloWebhookEvent
	calls     int
}

type trelloWebhookAcceptTestRepo struct {
	*fakeSourceRepo
	receipt *models.TrelloWebhookReceipt
	job     *models.DurableJob
}

type trelloWebhookTerminalCleanupRepo struct {
	*fakeSourceRepo
	receipt                  *models.TrelloWebhookReceipt
	syncJob                  *models.SourceSyncJob
	durableJob               *models.DurableJob
	completionErr            error
	failCalls                int
	failedGeneration         int64
	failedDispatchAttempt    int
	failedDispatchMaxAttempt int
}

func (*trelloWebhookTerminalCleanupRepo) QueueTrelloWebhookReceipt(*models.ConnectedSource, *models.TrelloWebhookReceipt, *models.DurableJob) (bool, error) {
	return false, errors.New("not used by terminal cleanup test")
}

func (r *trelloWebhookTerminalCleanupRepo) FindTrelloWebhookReceipt(sourceID, receiptID uuid.UUID) (*models.TrelloWebhookReceipt, error) {
	if r.receipt == nil || r.receipt.ID != receiptID || r.receipt.SourceID != sourceID {
		return nil, errors.New("webhook receipt not found")
	}
	return r.receipt, nil
}

func (r *trelloWebhookTerminalCleanupRepo) ClaimTrelloWebhookSync(sourceID, receiptID uuid.UUID, _ string, _ time.Time) (*trelloWebhookSyncClaim, error) {
	if r.receipt == nil || r.receipt.ID != receiptID || r.receipt.SourceID != sourceID {
		return nil, errors.New("webhook receipt not found")
	}
	jobID := r.syncJob.ID
	return &trelloWebhookSyncClaim{Generation: r.receipt.ReconciliationGeneration, DispatchAttempt: 2, SyncJobID: &jobID}, nil
}

func (*trelloWebhookTerminalCleanupRepo) BindTrelloWebhookSyncJob(uuid.UUID, int64, int, uuid.UUID, time.Time) error {
	return errors.New("not used by terminal cleanup test")
}

func (r *trelloWebhookTerminalCleanupRepo) FailTrelloWebhookSyncAttempt(_ uuid.UUID, generation int64, attempt, maxAttempts int, _ time.Time) (bool, error) {
	r.failCalls++
	r.failedGeneration = generation
	r.failedDispatchAttempt = attempt
	r.failedDispatchMaxAttempt = maxAttempts
	return false, nil
}

func (r *trelloWebhookTerminalCleanupRepo) CompleteTrelloWebhookSyncBatch(uuid.UUID, int64, int, uuid.UUID, int, time.Time) (trelloWebhookSyncCompletion, error) {
	return trelloWebhookSyncCompletion{}, r.completionErr
}

func (r *trelloWebhookTerminalCleanupRepo) CompleteTrelloWebhookReceipt(sourceID, receiptID uuid.UUID, status string, now time.Time) error {
	if r.receipt == nil || r.receipt.ID != receiptID || r.receipt.SourceID != sourceID {
		return errors.New("webhook receipt not found")
	}
	r.receipt.Status = status
	r.receipt.CompletedAt = &now
	return nil
}

func (r *trelloWebhookTerminalCleanupRepo) FindManualSyncJobForOwner(owner string, id uuid.UUID) (*models.SourceSyncJob, *models.DurableJob, error) {
	if r.syncJob == nil || r.durableJob == nil || owner != r.syncJob.OwnerIdentity || id != r.syncJob.ID {
		return nil, nil, errors.New("manual sync job not found")
	}
	return r.syncJob, r.durableJob, nil
}

func (*trelloWebhookTerminalCleanupRepo) FindManualSyncJobByIdempotencyKey(string, uuid.UUID, string) (*models.SourceSyncJob, *models.DurableJob, bool, error) {
	return nil, nil, false, errors.New("not used by terminal cleanup test")
}

func (*trelloWebhookTerminalCleanupRepo) CreateManualSyncJob(*models.SourceSyncJob, *models.DurableJob) (*models.SourceSyncJob, *models.DurableJob, bool, error) {
	return nil, nil, false, errors.New("not used by terminal cleanup test")
}

func (*trelloWebhookTerminalCleanupRepo) FindManualSyncJob(uuid.UUID) (*models.SourceSyncJob, error) {
	return nil, errors.New("not used by terminal cleanup test")
}

func (*trelloWebhookTerminalCleanupRepo) StartManualSyncJob(uuid.UUID, uuid.UUID, time.Time) (*models.SourceSyncJob, error) {
	return nil, errors.New("not used by terminal cleanup test")
}

func (r *trelloWebhookAcceptTestRepo) QueueTrelloWebhookReceipt(_ *models.ConnectedSource, receipt *models.TrelloWebhookReceipt, job *models.DurableJob) (bool, error) {
	r.receipt = receipt
	r.job = job
	return true, nil
}

func (r *trelloWebhookAcceptTestRepo) FindTrelloWebhookReceipt(_, _ uuid.UUID) (*models.TrelloWebhookReceipt, error) {
	return r.receipt, nil
}

func (*trelloWebhookAcceptTestRepo) ClaimTrelloWebhookSync(uuid.UUID, uuid.UUID, string, time.Time) (*trelloWebhookSyncClaim, error) {
	return nil, errors.New("not used by Trello comment webhook fixture")
}

func (*trelloWebhookAcceptTestRepo) BindTrelloWebhookSyncJob(uuid.UUID, int64, int, uuid.UUID, time.Time) error {
	return errors.New("not used by Trello comment webhook fixture")
}

func (*trelloWebhookAcceptTestRepo) FailTrelloWebhookSyncAttempt(uuid.UUID, int64, int, int, time.Time) (bool, error) {
	return false, errors.New("not used by Trello comment webhook fixture")
}

func (*trelloWebhookAcceptTestRepo) CompleteTrelloWebhookSyncBatch(uuid.UUID, int64, int, uuid.UUID, int, time.Time) (trelloWebhookSyncCompletion, error) {
	return trelloWebhookSyncCompletion{}, errors.New("not used by Trello comment webhook fixture")
}

func (r *trelloWebhookAcceptTestRepo) CompleteTrelloWebhookReceipt(sourceID, receiptID uuid.UUID, status string, now time.Time) error {
	if r.receipt == nil || r.receipt.SourceID != sourceID || r.receipt.ID != receiptID {
		return errors.New("webhook receipt not found")
	}
	r.receipt.Status = status
	r.receipt.CompletedAt = &now
	return nil
}

func (s *trelloWebhookTestService) AcceptTrelloWebhook(_ context.Context, event trelloWebhookEvent) (bool, error) {
	s.calls++
	s.event = event
	return s.accepted, s.acceptErr
}

func configureTrelloWebhookTest(t *testing.T) string {
	t.Helper()
	configureTrelloTest(t, "https://api.trello.com")
	t.Setenv(trelloAPISecretEnv, "application-secret")
	callbackURL := "https://hai.example.test/api/v1/sources/webhooks/trello"
	t.Setenv("TRELLO_WEBHOOK_CALLBACK_URL", callbackURL)
	return callbackURL
}

func trelloWebhookTestSignature(body []byte, callbackURL, secret string) string {
	mac := hmac.New(sha1.New, []byte(secret))
	_, _ = mac.Write(body)
	_, _ = mac.Write([]byte(callbackURL))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func TestVerifyTrelloWebhookSignatureUsesApplicationSecretAndExactCallbackURL(t *testing.T) {
	callbackURL := configureTrelloWebhookTest(t)
	body := []byte(trelloWebhookTestBody)
	signature := trelloWebhookTestSignature(body, callbackURL, "application-secret")
	if !verifyTrelloWebhookSignature(signature, callbackURL, "application-secret", body) {
		t.Fatal("signature generated with Trello application secret was rejected")
	}
	if verifyTrelloWebhookSignature(signature, callbackURL, "test-key", body) {
		t.Fatal("Trello API key was incorrectly accepted as the webhook signing secret")
	}
	if verifyTrelloWebhookSignature(signature, callbackURL+"/", "application-secret", body) {
		t.Fatal("signature was accepted for a callback URL different from the registered URL")
	}
	if verifyTrelloWebhookSignature("", callbackURL, "application-secret", body) {
		t.Fatal("empty signature was accepted")
	}
}

func TestTrelloWebhookConfigurationRequiresExactHTTPSRouteAndSecret(t *testing.T) {
	configureTrelloWebhookTest(t)
	if _, _, err := trelloWebhookConfiguration(); err != nil {
		t.Fatalf("valid webhook configuration: %v", err)
	}
	for _, invalid := range []string{
		"http://hai.example.test" + trelloWebhookCallbackPath,
		"https://user@hai.example.test" + trelloWebhookCallbackPath,
		"https://hai.example.test" + trelloWebhookCallbackPath + "?board=secret",
		"https://hai.example.test/api/v1/sources/other",
		"https://hai.example.test/prefix" + trelloWebhookCallbackPath,
		"https://hai.example.test" + trelloWebhookCallbackPath + "/",
		"https://hai.example.test:8443" + trelloWebhookCallbackPath,
	} {
		t.Setenv("TRELLO_WEBHOOK_CALLBACK_URL", invalid)
		if _, _, err := trelloWebhookConfiguration(); err == nil {
			t.Errorf("callback URL %q unexpectedly accepted", invalid)
		}
	}
	t.Setenv("TRELLO_WEBHOOK_CALLBACK_URL", "https://hai.example.test"+trelloWebhookCallbackPath)
	t.Setenv(trelloAPISecretEnv, "")
	if _, _, err := trelloWebhookConfiguration(); err == nil {
		t.Fatal("missing application secret was accepted")
	}
}

func TestParseTrelloWebhookEventValidatesAndNormalizesEvidence(t *testing.T) {
	event, err := parseTrelloWebhookEvent([]byte(trelloWebhookTestBody))
	if err != nil {
		t.Fatalf("parse webhook event: %v", err)
	}
	if event.ActionID != "aaaaaaaaaaaaaaaaaaaaaaaa" || event.ActionType != "updateComment" || event.CardID != "dddddddddddddddddddddddd" {
		t.Fatalf("unexpected parsed event identity: %#v", event)
	}
	if len(event.BoardIDs) != 2 || event.BoardIDs[0] != "cccccccccccccccccccccccc" || event.BoardIDs[1] != "board123" {
		t.Fatalf("board identities = %v, want canonical ID and short link", event.BoardIDs)
	}
	if event.CardURL != "https://trello.com/c/card1234/review-the-evidence" || event.ActionText != "Updated source comment" || event.Fingerprint == "" {
		t.Fatalf("unexpected parsed comment evidence: %#v", event)
	}
	if event.OccurredAt.IsZero() {
		t.Fatal("event timestamp was not parsed")
	}
}

func TestAcceptTrelloCardWebhookBindsReceiptToActionBoard(t *testing.T) {
	configureTrelloTest(t, "https://api.trello.com")
	boardID := "cccccccccccccccccccccccc"
	cardID := "dddddddddddddddddddddddd"
	source := &models.ConnectedSource{
		ID: uuid.New(), OwnerIdentity: "alice", ConnectorKey: trelloConnectorKey,
		SyncTarget: boardID, Enabled: true, Status: "active",
	}
	repo := &trelloWebhookAcceptTestRepo{fakeSourceRepo: newFakeSourceRepo(source)}
	service := &service{repo: repo}
	service.setManualSyncWorkerReady(true)

	body := strings.Replace(
		trelloWebhookTestBody,
		`"model":{"id":"cccccccccccccccccccccccc","shortLink":"board123"}`,
		`"model":{"id":"dddddddddddddddddddddddd","shortLink":"card1234"}`,
		1,
	)
	event, err := parseTrelloWebhookEvent([]byte(body))
	if err != nil {
		t.Fatalf("parse card-scoped webhook: %v", err)
	}
	accepted, err := service.AcceptTrelloWebhook(t.Context(), event)
	if err != nil || !accepted {
		t.Fatalf("accept card-scoped webhook = %v, %v; want accepted", accepted, err)
	}
	if repo.receipt == nil {
		t.Fatal("webhook receipt was not queued")
	}
	if repo.receipt.BoardID != boardID {
		t.Fatalf("persisted receipt board ID = %q, want action board %q (subscribed card is %q)", repo.receipt.BoardID, boardID, cardID)
	}
	if !trelloWebhookSourceMatches(source, []string{repo.receipt.BoardID}, repo.fakeSourceRepo) {
		t.Fatalf("queued receipt board ID %q cannot be reconciled to its matched source", repo.receipt.BoardID)
	}
}

func TestTrelloWebhookCapturesCommentCreationEditAndDeletion(t *testing.T) {
	for _, actionType := range []string{"commentCard", "copyCommentCard", "updateComment", "deleteComment"} {
		t.Run(actionType, func(t *testing.T) {
			body := strings.Replace(trelloWebhookTestBody, `"type":"updateComment"`, `"type":"`+actionType+`"`, 1)
			event, err := parseTrelloWebhookEvent([]byte(body))
			if err != nil {
				t.Fatalf("parse %s callback: %v", actionType, err)
			}
			if !isTrelloWebhookCommentChange(event.ActionType) {
				t.Fatalf("%s was not routed to evidence capture", actionType)
			}
			receipt := &models.TrelloWebhookReceipt{
				ActionID: event.ActionID, ActionType: event.ActionType, CardID: event.CardID,
				CardName: event.CardName, CardURL: event.CardURL, ActionText: event.ActionText, OccurredAt: event.OccurredAt,
			}
			item := trelloWebhookCommentImportItem(receipt)
			wantState := "updated"
			if actionType == "commentCard" || actionType == "copyCommentCard" {
				wantState = "created"
			} else if actionType == "deleteComment" {
				wantState = "deleted"
			}
			if item.ItemType != "trello_comment_"+wantState || !strings.Contains(item.Title, wantState) {
				t.Fatalf("comment item type/title = %q/%q, want %q state", item.ItemType, item.Title, wantState)
			}
			if actionType == "updateComment" || actionType == "deleteComment" {
				if strings.Contains(item.Content, event.ActionText) {
					t.Fatalf("%s callback text was exposed as current comment evidence: %q", actionType, item.Content)
				}
			} else if !strings.Contains(item.Content, event.ActionText) {
				t.Fatalf("%s callback did not retain created comment text", actionType)
			}
		})
	}
}

func TestTrelloWebhookCommentCreationSharesResumableActionIdentity(t *testing.T) {
	for _, actionType := range []string{"commentCard", "copyCommentCard"} {
		t.Run(actionType, func(t *testing.T) {
			const actionID = "aaaaaaaaaaaaaaaaaaaaaaaa"
			receipt := &models.TrelloWebhookReceipt{
				ActionID: actionID, ActionType: actionType, CardID: "dddddddddddddddddddddddd",
				CardName: "Review the evidence", ActionText: "Please verify the source.",
				OccurredAt: time.Date(2026, 9, 27, 10, 30, 0, 0, time.UTC),
			}
			webhookItem := trelloWebhookCommentImportItem(receipt)
			if want := "trello:action:" + actionID; webhookItem.ExternalID != want {
				t.Fatalf("webhook external ID = %q, want canonical action ID %q", webhookItem.ExternalID, want)
			}
			scanItem := trelloCommentImportItem(trelloAction{
				ID: actionID, Type: actionType, Date: receipt.OccurredAt.Format(time.RFC3339),
				Data: trelloActionData{
					Text: receipt.ActionText,
					Card: trelloActionCard{ID: receipt.CardID, Name: receipt.CardName},
				},
			}, trelloBoard{Name: "Delivery board"}, "")
			if webhookItem.ExternalID != scanItem.ExternalID {
				t.Fatalf("webhook external ID = %q, scan external ID = %q; same Trello action must upsert one source item", webhookItem.ExternalID, scanItem.ExternalID)
			}
		})
	}
}

func TestTrelloCommentCreationWebhookUsesIndependentJobFromActiveBoardCheckpoint(t *testing.T) {
	boardJobID := uuid.New()
	receiptID := uuid.New()
	lastSynced := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	source := newTrelloSource(uuid.New(), "abc123XY", "board-cursor")
	source.OwnerIdentity = "alice"
	source.LastSyncedAt = &lastSynced
	receipt := &models.TrelloWebhookReceipt{
		ID: receiptID, SourceID: source.ID, ActionID: "aaaaaaaaaaaaaaaaaaaaaaaa", BoardID: "abc123XY",
		CardID: "dddddddddddddddddddddddd", CardName: "Review evidence", CardURL: "https://trello.com/c/card1234/review-evidence",
		ActionType: "commentCard", ActionText: "New evidence", OccurredAt: lastSynced.Add(time.Minute), Status: "queued",
	}
	repo := &trelloSliceFixtureRepo{
		fakeSourceRepo: newFakeSourceRepo(source), receipt: receipt,
		state: &models.TrelloSyncState{
			SourceID: source.ID, OwnerIdentity: source.OwnerIdentity, BoardID: "abc123XY",
			LogicalJobID: trelloUUIDPointer(boardJobID), Generation: 1, Phase: trelloPhaseBackfillCards,
			CycleStartedAt: lastSynced.Add(-time.Hour),
		},
	}
	svc := NewService(repo, &fakeSourceMemoryService{}).(*service)

	result, err := svc.SyncContext(t.Context(), source.ID, ImportRequest{
		Mode: ModeWebhookSync, Items: []ImportItem{trelloWebhookCommentImportItem(receipt)}, trelloWebhookReceiptID: receiptID,
	})
	if err != nil {
		t.Fatalf("SyncContext webhook comment: %v", err)
	}
	if result.Job.ID == boardJobID {
		t.Fatal("comment webhook reused the active board checkpoint job")
	}
	if repo.state.LogicalJobID == nil || *repo.state.LogicalJobID != boardJobID {
		t.Fatalf("board checkpoint job changed to %v, want %s", repo.state.LogicalJobID, boardJobID)
	}
	if result.Job.Status != "completed" {
		t.Fatalf("webhook sync job status = %q, want completed", result.Job.Status)
	}
	updated, err := repo.FindSource(source.ID)
	if err != nil {
		t.Fatalf("reload source: %v", err)
	}
	if updated.Cursor != "board-cursor" || updated.LastSyncedAt == nil || !updated.LastSyncedAt.Equal(lastSynced) {
		t.Fatalf("event-only webhook changed board checkpoint state: cursor=%q lastSync=%v", updated.Cursor, updated.LastSyncedAt)
	}
}

func TestTrelloCommentEditRefreshesOnlyAffectedCardAndRetainsWebhookHistory(t *testing.T) {
	const (
		cardID        = "dddddddddddddddddddddddd"
		otherCardID   = "eeeeeeeeeeeeeeeeeeeeeeee"
		editedID      = "222222222222222222222222"
		remainingID   = "111111111111111111111111"
		otherActionID = "333333333333333333333333"
	)
	currentActions := []trelloAction{
		trelloWebhookRefreshAction(editedID, cardID, "Current edited text from Trello", "2026-09-27T10:20:00.000Z"),
		trelloWebhookRefreshAction(remainingID, cardID, "Another current comment", "2026-09-27T10:10:00.000Z"),
	}
	server, methods, actionPaths := newTrelloWebhookRefreshServer(t, currentActions, nil)
	defer server.Close()
	configureTrelloTest(t, server.URL)

	service, repo, receipt, source, extractionIDs, originalRaw := seedTrelloWebhookRefresh(t, "updateComment", cardID, otherCardID, editedID, remainingID, otherActionID)
	previouslyDeleted, err := repo.FindExtraction(extractionIDs["trello:action:"+editedID])
	if err != nil {
		t.Fatalf("load prior comment extraction: %v", err)
	}
	previouslyDeleted.Archived = true
	previouslyDeleted.ContentType = trelloCommentDeletedItemType
	if _, err := repo.SaveExtraction(previouslyDeleted); err != nil {
		t.Fatalf("seed provider-deleted extraction marker: %v", err)
	}
	if err := service.RunTrelloWebhookJob(t.Context(), trelloWebhookJobPayload{
		SourceID: source.ID.String(), ReceiptID: receipt.ID.String(), DurableJobID: receipt.DurableJobID.String(),
	}, 1, 3); err != nil {
		t.Fatalf("process edit callback: %v", err)
	}
	if receipt.Status != "completed" || receipt.ActionText != "callback-only changed text" || receipt.Fingerprint != strings.Repeat("a", 64) {
		t.Fatalf("webhook audit receipt changed or did not complete: %#v", receipt)
	}
	if len(*actionPaths) != 1 || (*actionPaths)[0] != "/1/cards/"+cardID+"/actions" {
		t.Fatalf("comment actions were read outside the affected card: %v", *actionPaths)
	}
	for _, method := range *methods {
		if method != http.MethodGet {
			t.Fatalf("Trello refresh issued a non-read request: %s", method)
		}
	}
	updatedRaw, err := repo.FindRawItem(source.ID, "trello:action:"+editedID)
	if err != nil || !strings.Contains(updatedRaw.Content, "Current edited text from Trello") || strings.Contains(updatedRaw.Content, "callback-only changed text") {
		t.Fatalf("current comment source was not refreshed from Trello: raw=%#v err=%v", updatedRaw, err)
	}
	updatedExtraction, err := repo.FindExtraction(extractionIDs["trello:action:"+editedID])
	if err != nil || updatedExtraction.Archived || !strings.Contains(updatedExtraction.Text, "Current edited text from Trello") {
		t.Fatalf("edited comment extraction is not current: extraction=%#v err=%v", updatedExtraction, err)
	}
	cardRaw, err := repo.FindRawItem(source.ID, "trello:card:"+cardID)
	if err != nil || !strings.Contains(cardRaw.Content, "Current edited text from Trello") || !strings.Contains(cardRaw.Content, "Another current comment") || strings.Contains(cardRaw.Content, "callback-only changed text") {
		t.Fatalf("card snapshot does not reflect authoritative comments: raw=%#v err=%v", cardRaw, err)
	}
	otherRaw, err := repo.FindRawItem(source.ID, "trello:action:"+otherActionID)
	otherExtraction, extractionErr := repo.FindExtraction(extractionIDs["trello:action:"+otherActionID])
	if err != nil || extractionErr != nil || otherRaw.Content != originalRaw[otherRaw.ExternalID].Content || otherExtraction.Archived {
		t.Fatalf("unrelated card comment changed: raw=%#v extraction=%#v errors=%v/%v", otherRaw, otherExtraction, err, extractionErr)
	}
	if repo.hasAudit("source.trello_comment_reconciliation_required") || !repo.hasAudit("source.trello_comment_reconciled") {
		t.Fatal("edit should record successful authoritative reconciliation, not quarantine-required state")
	}
	updatedSource, err := repo.FindSource(source.ID)
	if err != nil || updatedSource.Cursor != "board-cursor" || updatedSource.LastSyncedAt == nil || !updatedSource.LastSyncedAt.Equal(source.LastSyncedAt.UTC()) {
		t.Fatalf("per-card refresh changed the board cursor/freshness: source=%#v err=%v", updatedSource, err)
	}
}

func TestTrelloCommentDeleteArchivesOnlyMissingActionAndKeepsRawHistory(t *testing.T) {
	const (
		cardID        = "dddddddddddddddddddddddd"
		otherCardID   = "eeeeeeeeeeeeeeeeeeeeeeee"
		deletedID     = "222222222222222222222222"
		remainingID   = "111111111111111111111111"
		otherActionID = "333333333333333333333333"
	)
	currentActions := []trelloAction{
		trelloWebhookRefreshAction(remainingID, cardID, "Still active on this card", "2026-09-27T10:10:00.000Z"),
	}
	server, _, actionPaths := newTrelloWebhookRefreshServer(t, currentActions, nil)
	defer server.Close()
	configureTrelloTest(t, server.URL)
	service, repo, receipt, source, extractionIDs, originalRaw := seedTrelloWebhookRefresh(t, "deleteComment", cardID, otherCardID, deletedID, remainingID, otherActionID)
	webhookCreatedRaw, err := repo.FindRawItem(source.ID, "trello:action:"+deletedID)
	if err != nil {
		t.Fatalf("load webhook-created comment raw item: %v", err)
	}
	webhookCreatedRaw.ItemType = trelloCommentCreatedItemType
	if _, err := repo.SaveRawItem(webhookCreatedRaw); err != nil {
		t.Fatalf("mark webhook-created comment raw item: %v", err)
	}
	webhookCreatedExtraction, err := repo.FindExtraction(extractionIDs["trello:action:"+deletedID])
	if err != nil {
		t.Fatalf("load webhook-created comment extraction: %v", err)
	}
	webhookCreatedExtraction.ContentType = trelloCommentCreatedItemType
	if _, err := repo.SaveExtraction(webhookCreatedExtraction); err != nil {
		t.Fatalf("mark webhook-created comment extraction: %v", err)
	}
	deletedRawBefore := originalRaw["trello:action:"+deletedID]
	deletedTextBefore := "Deleted comment text retained in audit history"
	if err := service.RunTrelloWebhookJob(t.Context(), trelloWebhookJobPayload{
		SourceID: source.ID.String(), ReceiptID: receipt.ID.String(), DurableJobID: receipt.DurableJobID.String(),
	}, 1, 3); err != nil {
		t.Fatalf("process delete callback: %v", err)
	}
	deleted, err := repo.FindExtraction(extractionIDs["trello:action:"+deletedID])
	if err != nil || !deleted.Archived || deleted.ContentType != trelloCommentDeletedItemType || !strings.Contains(deleted.Text, deletedTextBefore) {
		t.Fatalf("deleted comment was not archived with its text preserved: extraction=%#v err=%v", deleted, err)
	}
	deletedRaw, err := repo.FindRawItem(source.ID, "trello:action:"+deletedID)
	if err != nil || deletedRaw.Content != deletedRawBefore.Content {
		t.Fatalf("deleted comment raw source history changed: raw=%#v err=%v", deletedRaw, err)
	}
	remaining, err := repo.FindExtraction(extractionIDs["trello:action:"+remainingID])
	if err != nil || remaining.Archived || !strings.Contains(remaining.Text, "Still active on this card") {
		t.Fatalf("remaining same-card comment was changed: extraction=%#v err=%v", remaining, err)
	}
	cardRaw, err := repo.FindRawItem(source.ID, "trello:card:"+cardID)
	if err != nil || strings.Contains(cardRaw.Content, deletedTextBefore) || !strings.Contains(cardRaw.Content, "Still active on this card") {
		t.Fatalf("card snapshot retained deleted text or lost active text: raw=%#v err=%v", cardRaw, err)
	}
	otherRaw, err := repo.FindRawItem(source.ID, "trello:action:"+otherActionID)
	otherExtraction, extractionErr := repo.FindExtraction(extractionIDs["trello:action:"+otherActionID])
	if err != nil || extractionErr != nil || otherRaw.Content != originalRaw[otherRaw.ExternalID].Content || otherExtraction.Archived {
		t.Fatalf("unrelated card comment changed: raw=%#v extraction=%#v errors=%v/%v", otherRaw, otherExtraction, err, extractionErr)
	}
	if receipt.Status != "completed" || !repo.hasAudit("source.trello_comment_archived") || !repo.hasAudit("source.trello_comment_reconciled") {
		t.Fatalf("delete receipt/audit state is incomplete: status=%q archived=%v reconciled=%v", receipt.Status, repo.hasAudit("source.trello_comment_archived"), repo.hasAudit("source.trello_comment_reconciled"))
	}
	if len(*actionPaths) != 1 || (*actionPaths)[0] != "/1/cards/"+cardID+"/actions" {
		t.Fatalf("delete refresh requested actions outside the affected card: %v", *actionPaths)
	}
}

func TestTrelloCommentDeleteArchiveFailureDoesNotReportSyncCompleted(t *testing.T) {
	const (
		cardID        = "dddddddddddddddddddddddd"
		otherCardID   = "eeeeeeeeeeeeeeeeeeeeeeee"
		deletedID     = "222222222222222222222222"
		remainingID   = "111111111111111111111111"
		otherActionID = "333333333333333333333333"
	)
	server, _, _ := newTrelloWebhookRefreshServer(t, []trelloAction{
		trelloWebhookRefreshAction(remainingID, cardID, "Still active on this card", "2026-09-27T10:10:00.000Z"),
	}, nil)
	defer server.Close()
	configureTrelloTest(t, server.URL)
	service, repo, receipt, source, _, _ := seedTrelloWebhookRefresh(t, "deleteComment", cardID, otherCardID, deletedID, remainingID, otherActionID)
	service.repo = &trelloCommentArchiveFailureRepo{trelloWebhookAcceptTestRepo: repo}

	err := service.RunTrelloWebhookJob(t.Context(), trelloWebhookJobPayload{
		SourceID: source.ID.String(), ReceiptID: receipt.ID.String(), DurableJobID: receipt.DurableJobID.String(),
	}, 1, 3)
	if err == nil {
		t.Fatal("delete callback unexpectedly completed after its history archive failed")
	}
	if receipt.Status != "queued" {
		t.Fatalf("archive failure advanced webhook receipt to %q, want queued for retry", receipt.Status)
	}
	if len(repo.jobs) == 0 {
		t.Fatal("comment refresh did not retain a sync outcome")
	}
	job := repo.jobs[len(repo.jobs)-1]
	if job.Status != "partial_failure" || job.ItemsFailed == 0 || !strings.Contains(job.Message, "deletion archival failed") {
		t.Fatalf("sync job after archive failure = %#v, want an explicit partial failure", job)
	}
}

type trelloCommentArchiveFailureRepo struct {
	*trelloWebhookAcceptTestRepo
}

func (r *trelloCommentArchiveFailureRepo) SaveExtraction(extraction *models.SourceExtraction) (*models.SourceExtraction, error) {
	if extraction != nil && extraction.Archived && extraction.ContentType == trelloCommentDeletedItemType {
		return nil, errors.New("simulated archive persistence failure")
	}
	return r.trelloWebhookAcceptTestRepo.SaveExtraction(extraction)
}

func TestTrelloCommentCreationDefersWhenAnotherSyncOwnsSourceLease(t *testing.T) {
	const (
		cardID      = "dddddddddddddddddddddddd"
		otherCardID = "eeeeeeeeeeeeeeeeeeeeeeee"
	)
	configureTrelloTest(t, "https://api.trello.com")
	service, repo, receipt, source, _, _ := seedTrelloWebhookRefresh(t, "commentCard", cardID, otherCardID,
		"222222222222222222222222", "111111111111111111111111", "333333333333333333333333")
	repo.sourceLeaseAcquired = false
	err := service.RunTrelloWebhookJob(t.Context(), trelloWebhookJobPayload{
		SourceID: source.ID.String(), ReceiptID: receipt.ID.String(), DurableJobID: receipt.DurableJobID.String(),
	}, 3, 3)
	var deferred *durablejob.DeferredError
	if !errors.As(err, &deferred) {
		t.Fatalf("comment creation under active sync error = %v, want deferred", err)
	}
	if receipt.Status != "queued" {
		t.Fatalf("deferred comment receipt status = %q, want queued", receipt.Status)
	}
}

func TestTrelloWebhookTerminalWorkerFailureReleasesActiveGeneration(t *testing.T) {
	configureTrelloTest(t, "https://api.trello.com")
	source := newTrelloSource(uuid.New(), testTrelloCanonicalBoardID, "board-cursor")
	receipt := &models.TrelloWebhookReceipt{
		ID: uuid.New(), SourceID: source.ID, ActionID: "aaaaaaaaaaaaaaaaaaaaaaaa",
		BoardID: testTrelloCanonicalBoardID, CardID: "dddddddddddddddddddddddd",
		ActionType: "updateCard", ReconciliationGeneration: 7,
		DurableJobID: uuid.New(), Status: "queued",
	}
	syncJobID := uuid.New()
	durableJobID := uuid.New()
	completedAt := time.Now().UTC()
	repo := &trelloWebhookTerminalCleanupRepo{
		fakeSourceRepo: newFakeSourceRepo(source),
		receipt:        receipt,
		syncJob: &models.SourceSyncJob{
			ID: syncJobID, SourceID: source.ID, OwnerIdentity: source.OwnerIdentity,
			Mode: ModeManualAsyncSync, Status: "completed", CompletedAt: &completedAt,
		},
		durableJob:    &models.DurableJob{ID: durableJobID, Status: models.DurableJobSucceeded, Attempts: 1, MaxAttempts: 3},
		completionErr: errors.New("simulated reconciliation finalization failure"),
	}
	service := NewService(repo, nil).(*service)
	err := service.RunTrelloWebhookJob(t.Context(), trelloWebhookJobPayload{
		SourceID: source.ID.String(), ReceiptID: receipt.ID.String(), DurableJobID: receipt.DurableJobID.String(),
	}, 3, 3)
	if err == nil || !strings.Contains(err.Error(), "simulated reconciliation finalization failure") {
		t.Fatalf("terminal reconciliation error = %v, want the finalization error", err)
	}
	if repo.failCalls != 1 || repo.failedGeneration != receipt.ReconciliationGeneration ||
		repo.failedDispatchAttempt != 2 || repo.failedDispatchMaxAttempt != 2 {
		t.Fatalf("terminal cleanup call = %d generation=%d attempt=%d max=%d; want exactly one exhausted active-generation failure",
			repo.failCalls, repo.failedGeneration, repo.failedDispatchAttempt, repo.failedDispatchMaxAttempt)
	}
	if receipt.Status != "failed" {
		t.Fatalf("receipt status after terminal worker failure = %q, want failed", receipt.Status)
	}
}

func TestTrelloCommentRefreshFailsClosedBeforeMutation(t *testing.T) {
	const (
		cardID        = "dddddddddddddddddddddddd"
		otherCardID   = "eeeeeeeeeeeeeeeeeeeeeeee"
		commentID     = "222222222222222222222222"
		otherActionID = "333333333333333333333333"
	)
	foreignAction := trelloWebhookRefreshAction(commentID, otherCardID, "Foreign card text", "2026-09-27T10:20:00.000Z")
	for name, fixture := range map[string]struct {
		actions    []trelloAction
		actionCode int
	}{
		"foreign card action":    {actions: []trelloAction{foreignAction}},
		"incomplete action read": {actionCode: http.StatusInternalServerError},
	} {
		t.Run(name, func(t *testing.T) {
			server, _, _ := newTrelloWebhookRefreshServer(t, fixture.actions, func(path string) int {
				if strings.HasSuffix(path, "/actions") {
					return fixture.actionCode
				}
				return 0
			})
			defer server.Close()
			configureTrelloTest(t, server.URL)
			service, repo, receipt, source, extractionIDs, _ := seedTrelloWebhookRefresh(t, "deleteComment", cardID, otherCardID, commentID, "111111111111111111111111", otherActionID)
			if err := service.RunTrelloWebhookJob(t.Context(), trelloWebhookJobPayload{
				SourceID: source.ID.String(), ReceiptID: receipt.ID.String(), DurableJobID: receipt.DurableJobID.String(),
			}, 1, 3); err == nil {
				t.Fatal("unsafe or incomplete provider response unexpectedly completed")
			}
			if receipt.Status != "queued" {
				t.Fatalf("failed reconciliation completed the webhook receipt: %q", receipt.Status)
			}
			comment, err := repo.FindExtraction(extractionIDs["trello:action:"+commentID])
			if err != nil || comment.Archived || comment.ContentType != "trello_comment" {
				t.Fatalf("failed refresh mutated comment evidence: extraction=%#v err=%v", comment, err)
			}
		})
	}
}

func trelloWebhookRefreshAction(actionID, cardID, text, date string) trelloAction {
	return trelloAction{
		ID: actionID, Type: "commentCard", Date: date,
		IDMemberCreator: testTrelloAccountMemberID,
		Data:            trelloActionData{Text: text, Card: trelloActionCard{ID: cardID, Name: "Review the evidence", ShortLink: "card1234"}},
		MemberCreator:   trelloMember{ID: testTrelloAccountMemberID, FullName: "Alice"},
	}
}

func newTrelloWebhookRefreshServer(t *testing.T, actions []trelloAction, statusForPath func(string) int) (*httptest.Server, *[]string, *[]string) {
	t.Helper()
	methods := &[]string{}
	actionPaths := &[]string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*methods = append(*methods, r.Method)
		if r.Method != http.MethodGet {
			http.Error(w, "read-only test server", http.StatusMethodNotAllowed)
			return
		}
		if statusForPath != nil {
			if status := statusForPath(r.URL.Path); status != 0 {
				http.Error(w, "fixture provider failure", status)
				return
			}
		}
		var value any
		switch {
		case strings.HasPrefix(r.URL.Path, "/1/tokens/"):
			value = map[string]any{"idMember": testTrelloAccountMemberID, "permissions": []map[string]any{{"modelType": "board", "read": true, "write": false}}}
		case r.URL.Path == "/1/boards/"+testTrelloCanonicalBoardID:
			value = trelloBoard{ID: testTrelloCanonicalBoardID, Name: "Delivery board", ShortURL: "https://trello.com/b/abc123XY/test-board"}
		case r.URL.Path == "/1/cards/dddddddddddddddddddddddd":
			value = trelloCard{
				ID: "dddddddddddddddddddddddd", IDBoard: testTrelloCanonicalBoardID,
				IDList: "999999999999999999999999", Name: "Review the evidence", Desc: "Card description",
				ShortLink: "card1234", DateLastActivity: "2026-09-27T10:30:00.000Z",
			}
		case r.URL.Path == "/1/lists/999999999999999999999999":
			value = trelloList{ID: "999999999999999999999999", Name: "Doing"}
		case r.URL.Path == "/1/cards/dddddddddddddddddddddddd/actions":
			*actionPaths = append(*actionPaths, r.URL.Path)
			if r.URL.Query().Get("filter") != trelloCommentActionTypes || r.URL.Query().Get("limit") != strconv.Itoa(trelloActionPageSize) {
				http.Error(w, "missing comment action filter or bound", http.StatusBadRequest)
				return
			}
			value = actions
		default:
			http.Error(w, "unexpected Trello endpoint: "+r.URL.Path, http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(value); err != nil {
			t.Errorf("encode Trello fixture: %v", err)
		}
	}))
	t.Setenv("CONNECTED_SOURCE_HTTP_LOOPBACK_ADDRS", server.Listener.Addr().String())
	return server, methods, actionPaths
}

func seedTrelloWebhookRefresh(t *testing.T, actionType, cardID, otherCardID, primaryActionID, remainingActionID, otherActionID string) (*service, *trelloWebhookAcceptTestRepo, *models.TrelloWebhookReceipt, *models.ConnectedSource, map[string]uuid.UUID, map[string]models.SourceRawItem) {
	t.Helper()
	lastSynced := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	source := newTrelloSource(uuid.New(), testTrelloCanonicalBoardID, "board-cursor")
	source.LastSyncedAt = &lastSynced
	receipt := &models.TrelloWebhookReceipt{
		ID: uuid.New(), SourceID: source.ID, ActionID: "aaaaaaaaaaaaaaaaaaaaaaaa",
		BoardID: testTrelloCanonicalBoardID, CardID: cardID, CardName: "Review the evidence",
		ActionType: actionType, ActionText: "callback-only changed text", OccurredAt: lastSynced.Add(time.Hour),
		Fingerprint: strings.Repeat("a", 64), DurableJobID: uuid.New(), Status: "queued",
	}
	repo := &trelloWebhookAcceptTestRepo{fakeSourceRepo: newFakeSourceRepo(source), receipt: receipt}
	rawItems := []models.SourceRawItem{
		{ID: uuid.New(), SourceID: source.ID, ExternalID: "trello:card:" + cardID, ItemType: "trello_card", Title: "Review the evidence",
			Content:  "Trello card: Review the evidence\nBoard: Delivery board\nList: Doing\nCard description\n\nComments (2):\n- Old snapshot comment",
			Metadata: "source=trello;board=Delivery board;list=Doing;comments=2;readonly=true", SourceURI: "https://trello.com/c/card1234"},
		{ID: uuid.New(), SourceID: source.ID, ExternalID: "trello:action:" + primaryActionID, ItemType: "trello_comment", Title: "Primary comment",
			Content: "Deleted comment text retained in audit history", Metadata: "source=trello;action=" + primaryActionID + ";card=" + cardID + ";actionType=commentCard;readonly=true"},
		{ID: uuid.New(), SourceID: source.ID, ExternalID: "trello:action:" + remainingActionID, ItemType: "trello_comment", Title: "Remaining comment",
			Content: "Previous remaining comment content", Metadata: "source=trello;action=" + remainingActionID + ";card=" + cardID + ";actionType=commentCard;readonly=true"},
		{ID: uuid.New(), SourceID: source.ID, ExternalID: "trello:action:" + otherActionID, ItemType: "trello_comment", Title: "Other card comment",
			Content: "Other card content remains unchanged", Metadata: "source=trello;action=" + otherActionID + ";card=" + otherCardID + ";actionType=commentCard;readonly=true"},
	}
	extractionIDs := make(map[string]uuid.UUID, len(rawItems))
	originalRaw := make(map[string]models.SourceRawItem, len(rawItems))
	for _, fixture := range rawItems {
		raw := fixture
		if _, err := repo.SaveRawItem(&raw); err != nil {
			t.Fatalf("save raw fixture %s: %v", raw.ExternalID, err)
		}
		originalRaw[raw.ExternalID] = raw
		extraction := &models.SourceExtraction{
			ID: uuid.New(), SourceID: source.ID, RawItemID: raw.ID,
			ContentType: raw.ItemType, SourceLabel: raw.Title, Text: raw.Content,
		}
		if _, err := repo.SaveExtraction(extraction); err != nil {
			t.Fatalf("save extraction fixture %s: %v", raw.ExternalID, err)
		}
		extractionIDs[raw.ExternalID] = extraction.ID
	}
	return NewService(repo, nil).(*service), repo, receipt, source, extractionIDs, originalRaw
}

func TestParseTrelloWebhookEventRejectsMalformedAndOversizedEvidence(t *testing.T) {
	for _, body := range []string{
		"not-json",
		strings.Replace(trelloWebhookTestBody, `"id":"aaaaaaaaaaaaaaaaaaaaaaaa"`, `"id":"invalid"`, 1),
		strings.Replace(trelloWebhookTestBody, `"type":"updateComment"`, `"type":"../updateComment"`, 1),
		strings.Replace(trelloWebhookTestBody, `"date":"2026-09-27T10:30:00.000Z"`, `"date":"yesterday"`, 1),
		strings.ReplaceAll(strings.ReplaceAll(trelloWebhookTestBody, `"id":"cccccccccccccccccccccccc"`, `"id":"invalid"`), `"shortLink":"board123"`, `"shortLink":"invalid"`),
	} {
		if _, err := parseTrelloWebhookEvent([]byte(body)); err == nil {
			t.Errorf("malformed event unexpectedly accepted: %s", body)
		}
	}
	oversized := strings.Replace(trelloWebhookTestBody, "Updated source comment", strings.Repeat("x", trelloWebhookTextLimit+1), 1)
	if _, err := parseTrelloWebhookEvent([]byte(oversized)); err == nil {
		t.Fatal("oversized comment evidence was accepted")
	}
}

func TestTrelloWebhookBoardMatchingKeepsShortLinksCaseSensitive(t *testing.T) {
	if !trelloWebhookBoardIDMatches("boardAB1", "boardAB1") {
		t.Fatal("identical Trello short links did not match")
	}
	if trelloWebhookBoardIDMatches("boardAB1", "boardab1") {
		t.Fatal("different case-sensitive Trello short links unexpectedly matched")
	}
	if !trelloWebhookBoardIDMatches("ABCDEFABCDEFABCDEFABCDEF", "abcdefabcdefabcdefabcdef") {
		t.Fatal("hexadecimal Trello board IDs should match case-insensitively")
	}
	ids := uniqueTrelloWebhookBoardIDs("boardAB1", "boardab1", "ABCDEFABCDEFABCDEFABCDEF", "abcdefabcdefabcdefabcdef")
	if len(ids) != 3 {
		t.Fatalf("unique board IDs = %v, want two distinct short links and one canonical ID", ids)
	}
}

func TestTrelloWebhookHandlerVerifiesSignatureAndAcceptsWithoutBrowserSession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	callbackURL := configureTrelloWebhookTest(t)
	service := &trelloWebhookTestService{Service: NewService(newFakeSourceRepo(), nil), accepted: true}
	handler := NewHandler(service)
	router := gin.New()
	router.POST(trelloWebhookCallbackPath, handler.TrelloWebhook)

	request := httptest.NewRequest(http.MethodPost, trelloWebhookCallbackPath, strings.NewReader(trelloWebhookTestBody))
	request.Header.Set("X-Trello-Webhook", trelloWebhookTestSignature([]byte(trelloWebhookTestBody), callbackURL, "application-secret"))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusAccepted || !strings.Contains(response.Body.String(), `"accepted":true`) {
		t.Fatalf("valid signed webhook status/body = %d/%s", response.Code, response.Body.String())
	}
	if service.calls != 1 || service.event.ActionID != "aaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Fatalf("intake calls/event = %d/%#v", service.calls, service.event)
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", response.Header().Get("Cache-Control"))
	}

	badRequest := httptest.NewRequest(http.MethodPost, trelloWebhookCallbackPath, strings.NewReader(trelloWebhookTestBody))
	badRequest.Header.Set("X-Trello-Webhook", trelloWebhookTestSignature([]byte(trelloWebhookTestBody), callbackURL, "wrong-secret"))
	badResponse := httptest.NewRecorder()
	router.ServeHTTP(badResponse, badRequest)
	if badResponse.Code != http.StatusUnauthorized || service.calls != 1 {
		t.Fatalf("invalid signature status/calls = %d/%d, want 401 and no intake", badResponse.Code, service.calls)
	}

	service.acceptErr = ErrTrelloWebhookReplayConflict
	conflictingReplay := httptest.NewRequest(http.MethodPost, trelloWebhookCallbackPath, strings.NewReader(trelloWebhookTestBody))
	conflictingReplay.Header.Set("X-Trello-Webhook", trelloWebhookTestSignature([]byte(trelloWebhookTestBody), callbackURL, "application-secret"))
	conflictResponse := httptest.NewRecorder()
	router.ServeHTTP(conflictResponse, conflictingReplay)
	if conflictResponse.Code != http.StatusConflict || service.calls != 2 {
		t.Fatalf("conflicting replay status/intake calls = %d/%d, want 409 and a single verified intake", conflictResponse.Code, service.calls)
	}
}

func TestTrelloWebhookHandlerSupportsHeadAndBoundsRequestBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	configureTrelloWebhookTest(t)
	handler := NewHandler(NewService(newFakeSourceRepo(), nil))
	router := gin.New()
	router.HEAD(trelloWebhookCallbackPath, handler.TrelloWebhook)
	router.POST(trelloWebhookCallbackPath, handler.TrelloWebhook)

	headResponse := httptest.NewRecorder()
	router.ServeHTTP(headResponse, httptest.NewRequest(http.MethodHead, trelloWebhookCallbackPath, nil))
	if headResponse.Code != http.StatusOK || headResponse.Body.Len() != 0 {
		t.Fatalf("HEAD status/body = %d/%q", headResponse.Code, headResponse.Body.String())
	}

	oversized := strings.NewReader(strings.Repeat("x", trelloWebhookBodyLimit+1))
	postResponse := httptest.NewRecorder()
	router.ServeHTTP(postResponse, httptest.NewRequest(http.MethodPost, trelloWebhookCallbackPath, oversized))
	if postResponse.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized request status = %d, body=%s", postResponse.Code, postResponse.Body.String())
	}
}
