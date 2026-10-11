package source

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"automation-hub-backend/internal/durablejob"
	"automation-hub-backend/internal/models"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	trelloWebhookCallbackPath     = "/api/v1/sources/webhooks/trello"
	trelloWebhookBodyLimit        = 128 << 10
	trelloWebhookTextLimit        = 32 << 10
	trelloWebhookReceiptFreshness = 24 * time.Hour
	trelloAPISecretEnv            = "TRELLO_API_SECRET"
	JobKindTrelloWebhook          = "source.trello_webhook"
)

var (
	ErrTrelloWebhookSourceMissing  = errors.New("Trello webhook does not match an active connected board")
	ErrTrelloWebhookAmbiguous      = errors.New("Trello webhook board maps to more than one connected source")
	ErrTrelloWebhookNotReady       = errors.New("Trello webhook worker is unavailable")
	ErrTrelloWebhookReplayConflict = errors.New("Trello webhook action identity was reused with different content")
)

type trelloWebhookEvent struct {
	BoardIDs    []string
	ActionID    string
	ActionType  string
	CardID      string
	CardName    string
	CardURL     string
	ActionText  string
	ActorID     string
	OccurredAt  time.Time
	Fingerprint string
}

type trelloWebhookPayload struct {
	Action struct {
		ID              string `json:"id"`
		Type            string `json:"type"`
		Date            string `json:"date"`
		IDMemberCreator string `json:"idMemberCreator"`
		Data            struct {
			Text  string `json:"text"`
			Board struct {
				ID        string `json:"id"`
				ShortLink string `json:"shortLink"`
			} `json:"board"`
			Card struct {
				ID        string `json:"id"`
				Name      string `json:"name"`
				ShortLink string `json:"shortLink"`
				URL       string `json:"url"`
			} `json:"card"`
		} `json:"data"`
	} `json:"action"`
	Model struct {
		ID        string `json:"id"`
		ShortLink string `json:"shortLink"`
	} `json:"model"`
}

type trelloWebhookIntakeService interface {
	AcceptTrelloWebhook(context.Context, trelloWebhookEvent) (bool, error)
}

type trelloWebhookRepository interface {
	QueueTrelloWebhookReceipt(source *models.ConnectedSource, receipt *models.TrelloWebhookReceipt, job *models.DurableJob) (bool, error)
	FindTrelloWebhookReceipt(sourceID, receiptID uuid.UUID) (*models.TrelloWebhookReceipt, error)
	ClaimTrelloWebhookSync(sourceID, receiptID uuid.UUID, owner string, now time.Time) (*trelloWebhookSyncClaim, error)
	BindTrelloWebhookSyncJob(sourceID uuid.UUID, generation int64, attempt int, syncJobID uuid.UUID, now time.Time) error
	FailTrelloWebhookSyncAttempt(sourceID uuid.UUID, generation int64, attempt, maxAttempts int, now time.Time) (bool, error)
	CompleteTrelloWebhookSyncBatch(sourceID uuid.UUID, generation int64, attempt int, syncJobID uuid.UUID, maxAttempts int, now time.Time) (trelloWebhookSyncCompletion, error)
	CompleteTrelloWebhookReceipt(sourceID, receiptID uuid.UUID, status string, now time.Time) error
}

type trelloWebhookSyncClaim struct {
	Generation       int64
	DispatchAttempt  int
	SyncJobID        *uuid.UUID
	AlreadyComplete  bool
	Waiting          bool
	PreviouslyFailed bool
}

type trelloWebhookJobPayload struct {
	SourceID     string `json:"sourceId"`
	ReceiptID    string `json:"receiptId"`
	DurableJobID string `json:"durableJobId"`
}

type trelloWebhookExecutionService interface {
	RunTrelloWebhookJob(context.Context, trelloWebhookJobPayload, int, int) error
}

func trelloWebhookConfiguration() (callbackURL, signingSecret string, err error) {
	callbackURL = strings.TrimSpace(os.Getenv("TRELLO_WEBHOOK_CALLBACK_URL"))
	signingSecret = strings.TrimSpace(os.Getenv(trelloAPISecretEnv))
	parsed, parseErr := url.Parse(callbackURL)
	if callbackURL == "" || parseErr != nil || !parsed.IsAbs() || !strings.EqualFold(parsed.Scheme, "https") ||
		parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" ||
		parsed.RawFragment != "" || parsed.Opaque != "" || (parsed.Port() != "" && parsed.Port() != "443") ||
		parsed.Path != trelloWebhookCallbackPath {
		return "", "", errors.New("Trello webhook callback URL must be the public HTTPS callback endpoint")
	}
	if signingSecret == "" {
		return "", "", errors.New("Trello webhook signing key is not configured")
	}
	return callbackURL, signingSecret, nil
}

func verifyTrelloWebhookSignature(signature, callbackURL, signingSecret string, body []byte) bool {
	if strings.TrimSpace(signature) == "" || callbackURL == "" || signingSecret == "" {
		return false
	}
	mac := hmac.New(sha1.New, []byte(signingSecret))
	_, _ = mac.Write(body)
	_, _ = mac.Write([]byte(callbackURL))
	expected := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(strings.TrimSpace(signature)), []byte(expected))
}

func parseTrelloWebhookEvent(body []byte) (trelloWebhookEvent, error) {
	var payload trelloWebhookPayload
	if len(body) == 0 || json.Unmarshal(body, &payload) != nil {
		return trelloWebhookEvent{}, errors.New("Trello webhook body is not valid JSON")
	}
	actionID, valid := normalizedTrelloMongoID(payload.Action.ID)
	if !valid {
		return trelloWebhookEvent{}, errors.New("Trello webhook action ID is invalid")
	}
	actionType := strings.TrimSpace(payload.Action.Type)
	if actionType == "" || len(actionType) > 80 || !validTrelloWebhookActionType(actionType) {
		return trelloWebhookEvent{}, errors.New("Trello webhook action type is invalid")
	}
	occurredAt, valid := parseTrelloTime(payload.Action.Date)
	if !valid {
		return trelloWebhookEvent{}, errors.New("Trello webhook timestamp is invalid")
	}
	boardIDs := uniqueTrelloWebhookBoardIDs(payload.Model.ID, payload.Model.ShortLink, payload.Action.Data.Board.ID, payload.Action.Data.Board.ShortLink)
	if len(boardIDs) == 0 {
		return trelloWebhookEvent{}, errors.New("Trello webhook board identity is missing")
	}
	cardID := ""
	if strings.TrimSpace(payload.Action.Data.Card.ID) != "" {
		cardID, valid = normalizedTrelloMongoID(payload.Action.Data.Card.ID)
		if !valid {
			return trelloWebhookEvent{}, errors.New("Trello webhook card ID is invalid")
		}
	}
	if isTrelloWebhookCommentChange(actionType) && cardID == "" {
		return trelloWebhookEvent{}, errors.New("Trello comment webhook is missing its card identity")
	}
	if len(payload.Action.Data.Text) > trelloWebhookTextLimit {
		return trelloWebhookEvent{}, errors.New("Trello webhook comment exceeds the configured evidence limit")
	}
	actorID := ""
	if rawActor := strings.TrimSpace(payload.Action.IDMemberCreator); rawActor != "" {
		if normalized, ok := normalizedTrelloMongoID(rawActor); ok {
			actorID = normalized
		}
	}
	return trelloWebhookEvent{
		BoardIDs: boardIDs, ActionID: actionID, ActionType: actionType, CardID: cardID,
		CardName: compact(strings.TrimSpace(payload.Action.Data.Card.Name), 512),
		CardURL:  validatedTrelloCardURL(payload.Action.Data.Card.URL), ActionText: payload.Action.Data.Text,
		ActorID: actorID, OccurredAt: occurredAt, Fingerprint: webhookFingerprint(body),
	}, nil
}

func uniqueTrelloWebhookBoardIDs(values ...string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if !trelloIDPattern.MatchString(value) {
			continue
		}
		if len(value) == 24 {
			value = strings.ToLower(value)
		}
		key := value
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, value)
	}
	return result
}

func validTrelloWebhookActionType(value string) bool {
	for index, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (index > 0 && r >= '0' && r <= '9') {
			continue
		}
		return false
	}
	return len(value) > 0
}

func isTrelloWebhookCommentChange(actionType string) bool {
	switch actionType {
	case "commentCard", "copyCommentCard", "updateComment", "deleteComment":
		return true
	default:
		return false
	}
}

func isTrelloWebhookCommentCreation(actionType string) bool {
	return actionType == "commentCard" || actionType == "copyCommentCard"
}

func validatedTrelloCardURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if len(raw) > 1024 {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil || !strings.EqualFold(parsed.Scheme, "https") || parsed.User != nil ||
		(!strings.EqualFold(parsed.Host, "trello.com") && !strings.EqualFold(parsed.Host, "www.trello.com")) ||
		!strings.HasPrefix(parsed.Path, "/c/") || parsed.RawQuery != "" || parsed.Fragment != "" {
		return ""
	}
	return parsed.String()
}

func webhookFingerprint(body []byte) string {
	digest := sha256.Sum256(body)
	return hex.EncodeToString(digest[:])
}

func (h *Handler) TrelloWebhook(c *gin.Context) {
	callbackURL, signingSecret, err := trelloWebhookConfiguration()
	if err != nil || !trelloConfigured() {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Trello webhook intake is not configured"})
		return
	}
	if c.Request.Method == http.MethodHead {
		c.Status(http.StatusOK)
		return
	}
	if c.Request.Method != http.MethodPost {
		c.Header("Allow", "HEAD, POST")
		c.Status(http.StatusMethodNotAllowed)
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, trelloWebhookBodyLimit)
	body, readErr := io.ReadAll(c.Request.Body)
	if readErr != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(readErr, &maxBytesErr) {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "Trello webhook payload exceeds the size limit"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": "Trello webhook payload could not be read"})
		return
	}
	if !verifyTrelloWebhookSignature(c.GetHeader("X-Trello-Webhook"), callbackURL, signingSecret, body) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Trello webhook signature is invalid"})
		return
	}
	event, err := parseTrelloWebhookEvent(body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	intake, ok := h.service.(trelloWebhookIntakeService)
	if !ok {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Trello webhook intake is unavailable"})
		return
	}
	accepted, err := intake.AcceptTrelloWebhook(c.Request.Context(), event)
	if err != nil {
		switch {
		case errors.Is(err, ErrTrelloWebhookSourceMissing), errors.Is(err, gorm.ErrRecordNotFound):
			c.Status(http.StatusGone)
		case errors.Is(err, ErrTrelloWebhookReplayConflict):
			c.JSON(http.StatusConflict, gin.H{"error": "Trello webhook action replay conflicts with previously accepted content"})
		case errors.Is(err, ErrTrelloWebhookAmbiguous):
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Trello webhook board mapping requires operator review"})
		case errors.Is(err, ErrTrelloWebhookNotReady):
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Trello webhook worker is not ready"})
		default:
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Trello webhook could not be durably accepted"})
		}
		return
	}
	if !accepted {
		c.Status(http.StatusAccepted)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusAccepted, gin.H{"accepted": true})
}

func (s *service) AcceptTrelloWebhook(ctx context.Context, event trelloWebhookEvent) (bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if !s.manualSyncWorkerAvailable() {
		return false, ErrTrelloWebhookNotReady
	}
	owner := strings.TrimSpace(os.Getenv(trelloOwnerIdentityEnv))
	if owner == "" || !trelloConfigured() {
		return false, errors.New("Trello webhook owner binding is not configured")
	}
	if len(event.BoardIDs) == 0 || event.ActionID == "" || event.ActionType == "" || event.Fingerprint == "" {
		return false, errors.New("Trello webhook event is incomplete")
	}
	sources, err := s.repo.FindSources(true)
	if err != nil {
		return false, fmt.Errorf("load connected Trello sources: %w", err)
	}
	var matched *models.ConnectedSource
	for index := range sources {
		source := &sources[index]
		if source.ConnectorKey != trelloConnectorKey || source.OwnerIdentity != owner {
			continue
		}
		if !trelloWebhookSourceMatches(source, event.BoardIDs, s.repo) {
			continue
		}
		if !trelloSourceCanSync(source) {
			return false, nil
		}
		if matched != nil {
			return false, ErrTrelloWebhookAmbiguous
		}
		copy := *source
		matched = &copy
	}
	if matched == nil {
		return false, ErrTrelloWebhookSourceMissing
	}
	boardID, err := trelloBoardID(matched.SyncTarget)
	if err != nil {
		return false, fmt.Errorf("matched Trello source has an invalid board binding: %w", err)
	}
	repository, ok := s.repo.(trelloWebhookRepository)
	if !ok {
		return false, errors.New("durable Trello webhook repository is unavailable")
	}
	now := time.Now().UTC()
	receiptID, err := uuid.NewRandom()
	if err != nil {
		return false, fmt.Errorf("allocate Trello webhook receipt: %w", err)
	}
	jobID, err := uuid.NewRandom()
	if err != nil {
		return false, fmt.Errorf("allocate Trello webhook job: %w", err)
	}
	payload, err := json.Marshal(trelloWebhookJobPayload{
		SourceID: matched.ID.String(), ReceiptID: receiptID.String(), DurableJobID: jobID.String(),
	})
	if err != nil {
		return false, fmt.Errorf("encode Trello webhook job: %w", err)
	}
	receipt := &models.TrelloWebhookReceipt{
		ID: receiptID, SourceID: matched.ID, ActionID: event.ActionID, BoardID: boardID,
		CardID: event.CardID, CardName: event.CardName, CardURL: event.CardURL,
		ActionType: event.ActionType, ActionText: event.ActionText, ActorID: event.ActorID,
		OccurredAt: event.OccurredAt.UTC(), Fingerprint: event.Fingerprint, DurableJobID: jobID,
		Status: "queued", ReceivedAt: now,
	}
	job := &models.DurableJob{
		ID: jobID, Queue: "source", Kind: JobKindTrelloWebhook, Payload: string(payload),
		Status: models.DurableJobPending, RunAt: now, MaxAttempts: syncMaxAttempts, CreatedAt: now, UpdatedAt: now,
	}
	return repository.QueueTrelloWebhookReceipt(matched, receipt, job)
}

func trelloWebhookSourceMatches(source *models.ConnectedSource, boardIDs []string, repository Repository) bool {
	if source == nil {
		return false
	}
	identifiers := []string{}
	if id, err := trelloBoardID(source.SyncTarget); err == nil {
		identifiers = append(identifiers, id)
	}
	if stateRepository, ok := repository.(trelloSyncStateRepository); ok {
		if state, err := stateRepository.FindTrelloSyncState(source.ID); err == nil && state != nil {
			identifiers = append(identifiers, state.BoardID)
		}
	}
	for _, candidate := range boardIDs {
		for _, identifier := range identifiers {
			if trelloWebhookBoardIDMatches(candidate, identifier) {
				return true
			}
		}
	}
	return false
}

func trelloWebhookBoardIDMatches(candidate, configured string) bool {
	candidateID, candidateIsID := normalizedTrelloMongoID(candidate)
	configuredID, configuredIsID := normalizedTrelloMongoID(configured)
	if candidateIsID && configuredIsID {
		return candidateID == configuredID
	}
	return candidate == configured
}

func (s *service) RunTrelloWebhookJob(ctx context.Context, payload trelloWebhookJobPayload, attempt, maxAttempts int) (runErr error) {
	sourceID, err := uuid.Parse(strings.TrimSpace(payload.SourceID))
	if err != nil || sourceID == uuid.Nil {
		return errors.New("Trello webhook queue payload has an invalid source ID")
	}
	receiptID, err := uuid.Parse(strings.TrimSpace(payload.ReceiptID))
	if err != nil || receiptID == uuid.Nil {
		return errors.New("Trello webhook queue payload has an invalid receipt ID")
	}
	repository, ok := s.repo.(trelloWebhookRepository)
	if !ok {
		return errors.New("durable Trello webhook repository is unavailable")
	}
	receipt, err := repository.FindTrelloWebhookReceipt(sourceID, receiptID)
	if err != nil {
		return err
	}
	durableJobID, err := uuid.Parse(strings.TrimSpace(payload.DurableJobID))
	if err != nil || durableJobID == uuid.Nil || receipt.DurableJobID != durableJobID {
		return errors.New("Trello webhook receipt does not belong to this durable job")
	}
	if attempt < 1 {
		attempt = 1
	}
	if maxAttempts < attempt {
		maxAttempts = attempt
	}
	claimedGeneration := int64(0)
	claimedDispatchAttempt := 0
	reconciliationSettled := false
	defer func() {
		if runErr == nil || attempt < maxAttempts {
			return
		}
		var deferred *durablejob.DeferredError
		if errors.As(runErr, &deferred) {
			return
		}
		if !reconciliationSettled && claimedGeneration > 0 && claimedDispatchAttempt > 0 {
			_, cleanupErr := repository.FailTrelloWebhookSyncAttempt(
				sourceID, claimedGeneration, claimedDispatchAttempt, claimedDispatchAttempt, time.Now().UTC(),
			)
			if cleanupErr != nil {
				runErr = durablejob.Defer("Trello webhook reconciliation cleanup is waiting for database recovery")
				return
			}
			reconciliationSettled = true
		}
		if completeErr := repository.CompleteTrelloWebhookReceipt(sourceID, receiptID, "failed", time.Now().UTC()); completeErr != nil {
			runErr = errors.Join(runErr, fmt.Errorf("record terminal Trello webhook failure: %w", completeErr))
		}
	}()
	if receipt.Status == "completed" || receipt.Status == "ignored" || receipt.Status == "failed" {
		return nil
	}
	source, err := s.repo.FindSource(sourceID)
	if err != nil {
		return err
	}
	if source.ConnectorKey != trelloConnectorKey || source.OwnerIdentity != strings.TrimSpace(os.Getenv(trelloOwnerIdentityEnv)) ||
		!trelloWebhookSourceMatches(source, []string{receipt.BoardID}, s.repo) {
		if err := repository.CompleteTrelloWebhookReceipt(sourceID, receiptID, "failed", time.Now().UTC()); err != nil {
			return fmt.Errorf("record rejected Trello webhook binding: %w", err)
		}
		return nil
	}
	if !trelloSourceCanSync(source) {
		if !isTrelloWebhookCommentChange(receipt.ActionType) && receipt.ReconciliationGeneration > 0 {
			return durablejob.Defer("Trello webhook reconciliation is waiting for its connected source to be enabled")
		}
		if err := repository.CompleteTrelloWebhookReceipt(sourceID, receiptID, "ignored", time.Now().UTC()); err != nil {
			return fmt.Errorf("record ignored Trello webhook for inactive source: %w", err)
		}
		return nil
	}
	if !isTrelloWebhookCommentChange(receipt.ActionType) {
		claim, claimErr := repository.ClaimTrelloWebhookSync(sourceID, receiptID, source.OwnerIdentity, time.Now().UTC())
		if claimErr != nil {
			return fmt.Errorf("claim Trello webhook reconciliation generation: %w", claimErr)
		}
		if claim.AlreadyComplete {
			return repository.CompleteTrelloWebhookReceipt(sourceID, receiptID, "completed", time.Now().UTC())
		}
		if claim.PreviouslyFailed {
			reconciliationSettled = true
			if err := repository.CompleteTrelloWebhookReceipt(sourceID, receiptID, "failed", time.Now().UTC()); err != nil {
				return fmt.Errorf("record previously exhausted Trello reconciliation: %w", err)
			}
			return nil
		}
		if claim.Waiting {
			return durablejob.Defer("Trello webhook is queued for the next board reconciliation generation")
		}
		claimedGeneration = claim.Generation
		claimedDispatchAttempt = claim.DispatchAttempt
		syncJobID := uuid.Nil
		if claim.SyncJobID != nil {
			syncJobID = *claim.SyncJobID
		} else {
			key := fmt.Sprintf("trello-webhook:%s:%d:%d", sourceID, claim.Generation, claim.DispatchAttempt)
			view, _, submitErr := s.SubmitManualSync(source.OwnerIdentity, sourceID, key, ManualSyncRequest{})
			if errors.Is(submitErr, ErrManualSyncAlreadyActive) {
				return durablejob.Defer("Trello webhook is waiting for the active board sync")
			}
			if errors.Is(submitErr, ErrManualSyncSourceDisabled) {
				return durablejob.Defer("Trello webhook reconciliation is waiting for its connected source to be enabled")
			}
			if submitErr != nil {
				if errors.Is(submitErr, ErrManualSyncWorkerUnavailable) {
					return durablejob.Defer("Trello webhook worker is temporarily unavailable")
				}
				return fmt.Errorf("queue Trello webhook reconciliation: %w", submitErr)
			}
			syncJobID, err = uuid.Parse(view.ID)
			if err != nil || syncJobID == uuid.Nil {
				return errors.New("Trello webhook reconciliation returned an invalid sync job ID")
			}
			if err := repository.BindTrelloWebhookSyncJob(sourceID, claim.Generation, claim.DispatchAttempt, syncJobID, time.Now().UTC()); err != nil {
				return fmt.Errorf("bind Trello webhook to reconciliation sync: %w", err)
			}
		}
		view, statusErr := s.ManualSyncJobForOwner(source.OwnerIdentity, syncJobID)
		if statusErr != nil {
			return fmt.Errorf("check Trello webhook sync status: %w", statusErr)
		}
		switch view.Status {
		case "queued", "running":
			return durablejob.Defer("Trello webhook reconciliation is still running")
		case "failed", "cancelled":
			retry, failErr := repository.FailTrelloWebhookSyncAttempt(sourceID, claim.Generation, claim.DispatchAttempt, syncMaxAttempts, time.Now().UTC())
			if failErr != nil {
				return fmt.Errorf("record failed Trello reconciliation attempt: %w", failErr)
			}
			if retry {
				return durablejob.Defer("Trello board sync failed; retrying the reconciliation generation")
			}
			reconciliationSettled = true
			if err := repository.CompleteTrelloWebhookReceipt(sourceID, receiptID, "failed", time.Now().UTC()); err != nil {
				return fmt.Errorf("record exhausted Trello webhook reconciliation: %w", err)
			}
			return nil
		case "completed":
			completion, err := repository.CompleteTrelloWebhookSyncBatch(sourceID, claim.Generation, claim.DispatchAttempt, syncJobID, syncMaxAttempts, time.Now().UTC())
			if err != nil {
				return fmt.Errorf("complete Trello webhook reconciliation generation: %w", err)
			}
			if completion.Retry {
				return durablejob.Defer("Trello sync resumed an older checkpoint; starting a fresh board scan for this webhook")
			}
			if !completion.Completed {
				reconciliationSettled = true
				if err := repository.CompleteTrelloWebhookReceipt(sourceID, receiptID, "failed", time.Now().UTC()); err != nil {
					return fmt.Errorf("record exhausted Trello fresh-scan attempts: %w", err)
				}
				return nil
			}
			reconciliationSettled = true
		default:
			return fmt.Errorf("Trello webhook sync returned unsupported status %q", view.Status)
		}
	}
	if isTrelloWebhookCommentCreation(receipt.ActionType) {
		item := trelloWebhookCommentImportItem(receipt)
		result, syncErr := s.syncContext(ctx, sourceID, ImportRequest{
			Mode: ModeWebhookSync, Items: []ImportItem{item}, trelloWebhookReceiptID: receiptID,
		}, uuid.Nil)
		if errors.Is(syncErr, ErrSyncInProgress) || errors.Is(syncErr, ErrTrelloSyncAlreadyActive) {
			return durablejob.Defer("Trello comment import is waiting for the active source or board sync")
		}
		if syncErr != nil {
			return fmt.Errorf("import Trello comment change: %w", syncErr)
		}
		if result == nil || result.Job.Status != "completed" {
			return errors.New("Trello comment change import did not complete")
		}
	} else if receipt.ActionType == "updateComment" || receipt.ActionType == "deleteComment" {
		result, syncErr := s.syncContext(ctx, sourceID, ImportRequest{
			Mode: ModeWebhookSync, trelloWebhookReceiptID: receiptID, trelloWebhookReconciliation: true,
		}, uuid.Nil)
		if errors.Is(syncErr, ErrSyncInProgress) || errors.Is(syncErr, ErrTrelloSyncAlreadyActive) {
			return durablejob.Defer("Trello comment refresh is waiting for the active source or board sync")
		}
		if syncErr != nil {
			return fmt.Errorf("refresh Trello comment state for affected card: %w", syncErr)
		}
		if result == nil || result.Job.Status != "completed" {
			return errors.New("Trello comment refresh did not complete")
		}
	}
	return repository.CompleteTrelloWebhookReceipt(sourceID, receiptID, "completed", time.Now().UTC())
}

func trelloWebhookMetadataValue(metadata, key string) string {
	for _, field := range strings.Split(metadata, ";") {
		name, value, ok := strings.Cut(field, "=")
		if ok && name == key {
			return value
		}
	}
	return ""
}

func trelloWebhookCommentImportItem(receipt *models.TrelloWebhookReceipt) ImportItem {
	verb, state := "updated", "updated"
	externalID := "trello:webhook-action:" + receipt.ActionID
	switch receipt.ActionType {
	case "commentCard", "copyCommentCard":
		verb, state = "created", "created"
		// These creation actions are also returned by the resumable action scan.
		// Share its identity so webhook-first and scan-first delivery upsert once.
		externalID = "trello:action:" + receipt.ActionID
	case "deleteComment":
		verb, state = "deleted", "deleted"
	}
	content := fmt.Sprintf("Trello comment %s.\nAction: %s\nOccurred at: %s", verb, receipt.ActionType, receipt.OccurredAt.UTC().Format(time.RFC3339))
	if receipt.ActorID != "" {
		content += "\nActor ID: " + receipt.ActorID
	}
	if isTrelloWebhookCommentCreation(receipt.ActionType) && receipt.ActionText != "" {
		content += "\nComment text supplied by Trello: " + receipt.ActionText
	} else if receipt.ActionType == "updateComment" || receipt.ActionType == "deleteComment" {
		content += "\nThis callback cannot identify the original imported comment; its text is retained only in the webhook receipt pending reconciliation."
	}
	metadata := fmt.Sprintf("source=trello;action=%s;actionId=%s;actionType=%s;board=%s;card=%s;readonly=true;webhookVerified=true",
		receipt.ActionID, receipt.ActionID, receipt.ActionType, receipt.BoardID, receipt.CardID)
	return ImportItem{
		ExternalID: externalID,
		Title:      "Trello comment " + verb + " on " + firstNonEmpty(receipt.CardName, receipt.CardID),
		Content:    content, SourceURI: validatedTrelloCardURL(receipt.CardURL),
		ItemType:   "trello_comment_" + state,
		ProjectKey: "", Metadata: metadata,
	}
}

func trelloWebhookHandler(service Service, allowed ...func() bool) durablejob.Handler {
	backgroundAllowed := schedulerBackgroundGate(allowed)
	return func(ctx context.Context, job durablejob.Job) error {
		if backgroundAllowed != nil && !backgroundAllowed() {
			return durablejob.Defer("background processing is paused by safety policy")
		}
		var payload trelloWebhookJobPayload
		if err := json.Unmarshal([]byte(job.Payload), &payload); err != nil {
			return fmt.Errorf("decode Trello webhook payload: %w", err)
		}
		executor, ok := service.(trelloWebhookExecutionService)
		if !ok {
			return errors.New("Trello webhook worker is unavailable")
		}
		return executor.RunTrelloWebhookJob(ctx, payload, job.Attempts+1, job.MaxAttempts)
	}
}

func isTrelloWebhookReceiptItem(source *models.ConnectedSource, request ImportRequest, repo Repository) bool {
	if source == nil || source.ConnectorKey != trelloConnectorKey || request.trelloWebhookReceiptID == uuid.Nil || len(request.Items) != 1 {
		return false
	}
	repository, ok := repo.(trelloWebhookRepository)
	if !ok {
		return false
	}
	receipt, err := repository.FindTrelloWebhookReceipt(source.ID, request.trelloWebhookReceiptID)
	if err != nil || receipt.Status == "ignored" || receipt.Status == "failed" || !isTrelloWebhookCommentCreation(receipt.ActionType) {
		return false
	}
	expected := trelloWebhookCommentImportItem(receipt)
	actual := request.Items[0]
	return actual.ExternalID == expected.ExternalID && actual.Title == expected.Title && actual.Content == expected.Content &&
		actual.SourceURI == expected.SourceURI && actual.ItemType == expected.ItemType && actual.ProjectKey == expected.ProjectKey &&
		actual.Metadata == expected.Metadata
}
