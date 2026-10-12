package source

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"automation-hub-backend/internal/durablejob"
	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/safety"

	"github.com/google/uuid"
)

const (
	ModeManualAsyncSync  = "manual_async_sync"
	JobKindManualSync    = "source.manual_sync"
	manualSyncKeyMin     = 16
	manualSyncKeyMax     = 128
	manualSyncProjectMax = 255
	manualSyncMessageMax = 280
)

var (
	ErrManualSyncIdempotencyConflict = errors.New("idempotency key was already used for a different manual source sync request")
	ErrManualSyncAlreadyActive       = errors.New("a manual source sync is already queued or running")
	ErrManualSyncInvalidRequest      = errors.New("manual source sync request is invalid")
	ErrManualSyncSourceDisabled      = errors.New("source is not enabled for sync")
	ErrManualSyncWorkerUnavailable   = errors.New("durable manual source sync worker is unavailable")
)

type ManualSyncRequest struct {
	ProjectKey string `json:"projectKey,omitempty"`
}

// ManualSyncJobView is intentionally a bounded public projection. It excludes
// the durable payload, cursor values, owner identity, provider response, and
// low-level error text.
type ManualSyncJobView struct {
	ID              string     `json:"id"`
	SourceID        string     `json:"sourceId"`
	Mode            string     `json:"mode"`
	Status          string     `json:"status"`
	ItemsSeen       int        `json:"itemsSeen"`
	ItemsAdded      int        `json:"itemsAdded"`
	ItemsUpdated    int        `json:"itemsUpdated"`
	ItemsFailed     int        `json:"itemsFailed"`
	ProgressPhase   string     `json:"progressPhase,omitempty"`
	ProgressPages   int        `json:"progressPages,omitempty"`
	ProgressRecords int        `json:"progressRecords,omitempty"`
	ProgressMessage string     `json:"progressMessage,omitempty"`
	Message         string     `json:"message,omitempty"`
	CreatedAt       time.Time  `json:"createdAt"`
	StartedAt       *time.Time `json:"startedAt,omitempty"`
	CompletedAt     *time.Time `json:"completedAt,omitempty"`
	Attempt         int        `json:"attempt"`
	MaxAttempts     int        `json:"maxAttempts"`
	NextAttemptAt   *time.Time `json:"nextAttemptAt,omitempty"`
}

type manualSyncPayload struct {
	SourceID           string `json:"sourceId"`
	SyncJobID          string `json:"syncJobId"`
	ProjectKey         string `json:"projectKey,omitempty"`
	ProjectKeyOverride bool   `json:"projectKeyOverride,omitempty"`
}

type manualSyncRepository interface {
	FindManualSyncJobByIdempotencyKey(owner string, sourceID uuid.UUID, keyHash string) (*models.SourceSyncJob, *models.DurableJob, bool, error)
	CreateManualSyncJob(job *models.SourceSyncJob, durable *models.DurableJob) (*models.SourceSyncJob, *models.DurableJob, bool, error)
	FindManualSyncJobForOwner(owner string, id uuid.UUID) (*models.SourceSyncJob, *models.DurableJob, error)
	FindManualSyncJob(id uuid.UUID) (*models.SourceSyncJob, error)
	StartManualSyncJob(id, sourceID uuid.UUID, now time.Time) (*models.SourceSyncJob, error)
}

type sourceSyncCompletionRepository interface {
	CompleteSourceSync(source *models.ConnectedSource, job *models.SourceSyncJob) (*models.ConnectedSource, *models.SourceSyncJob, error)
}

type ManualSyncSubmissionService interface {
	SubmitManualSync(owner string, sourceID uuid.UUID, idempotencyKey string, request ManualSyncRequest) (*ManualSyncJobView, bool, error)
	ManualSyncJobForOwner(owner string, id uuid.UUID) (*ManualSyncJobView, error)
}

type manualSyncExecutionService interface {
	RunManualSyncJob(ctx context.Context, payload manualSyncPayload, durableJobID uuid.UUID, attempt, maxAttempts int) error
}

type manualSyncWorkerReadiness interface {
	manualSyncWorkerAvailable() bool
}

func (s *service) SubmitManualSync(owner string, sourceID uuid.UUID, idempotencyKey string, request ManualSyncRequest) (*ManualSyncJobView, bool, error) {
	owner = strings.TrimSpace(owner)
	key := strings.TrimSpace(idempotencyKey)
	request.ProjectKey = strings.TrimSpace(request.ProjectKey)
	if owner == "" || len(owner) > 255 || sourceID == uuid.Nil || !validManualSyncIdempotencyKey(key) || len(request.ProjectKey) > manualSyncProjectMax {
		return nil, false, ErrManualSyncInvalidRequest
	}
	requestBytes, err := json.Marshal(struct {
		SourceID   string `json:"sourceId"`
		ProjectKey string `json:"projectKey,omitempty"`
	}{SourceID: sourceID.String(), ProjectKey: request.ProjectKey})
	if err != nil {
		return nil, false, err
	}
	keyDigest := sha256.Sum256([]byte(key))
	requestDigest := sha256.Sum256(requestBytes)
	repository, ok := s.repo.(manualSyncRepository)
	if !ok {
		return nil, false, errors.New("durable manual source sync is unavailable")
	}
	keyHash := hex.EncodeToString(keyDigest[:])
	requestHash := hex.EncodeToString(requestDigest[:])
	lookupRetry := func() (*ManualSyncJobView, bool, error) {
		stored, durableStored, found, lookupErr := repository.FindManualSyncJobByIdempotencyKey(owner, sourceID, keyHash)
		if lookupErr != nil || !found {
			return nil, found, lookupErr
		}
		if stored.RequestHash != requestHash {
			return nil, true, ErrManualSyncIdempotencyConflict
		}
		return manualSyncView(stored, durableStored), true, nil
	}
	if view, found, lookupErr := lookupRetry(); lookupErr != nil {
		return nil, false, lookupErr
	} else if found {
		return view, false, nil
	}

	// Hold a read lock through persistence so readiness changes have a clear
	// ordering with acceptance: once the worker is marked unavailable, no new
	// request can be accepted. Existing idempotent retries were resolved above.
	s.manualSyncWorkerMu.RLock()
	if !s.manualSyncWorkerReady.Load() && !s.manualOnlySyncWorkerReady.Load() {
		s.manualSyncWorkerMu.RUnlock()
		if view, found, lookupErr := lookupRetry(); lookupErr != nil {
			return nil, false, lookupErr
		} else if found {
			return view, false, nil
		}
		return nil, false, ErrManualSyncWorkerUnavailable
	}
	defer s.manualSyncWorkerMu.RUnlock()

	source, err := s.repo.FindMutableSourceForOwner(sourceID, owner)
	if err != nil {
		return nil, false, err
	}
	if !source.Enabled || source.Status == "paused" || source.Status == "revoked" {
		if view, found, lookupErr := lookupRetry(); lookupErr != nil {
			return nil, false, lookupErr
		} else if found {
			return view, false, nil
		}
		return nil, false, ErrManualSyncSourceDisabled
	}
	effectiveProjectKey := firstNonEmpty(request.ProjectKey, source.DefaultProjectKey)
	jobID := uuid.New()
	queueID := uuid.New()
	now := time.Now().UTC()
	payload, err := json.Marshal(manualSyncPayload{
		SourceID: sourceID.String(), SyncJobID: jobID.String(), ProjectKey: effectiveProjectKey,
		ProjectKeyOverride: request.ProjectKey != "",
	})
	if err != nil {
		return nil, false, err
	}
	manualJob := &models.SourceSyncJob{
		ID: jobID, SourceID: sourceID, OwnerIdentity: owner,
		IdempotencyKeyHash: keyHash, RequestHash: requestHash,
		Mode: ModeManualAsyncSync, Status: "queued", CursorBefore: source.Cursor, CursorAfter: source.Cursor,
		Message: "Queued for background sync; this page can be closed safely.", CreatedAt: now, UpdatedAt: now,
	}
	durable := &models.DurableJob{
		ID: queueID, Queue: "source", Kind: JobKindManualSync, Payload: string(payload),
		Status: models.DurableJobPending, RunAt: now, MaxAttempts: syncMaxAttempts, CreatedAt: now, UpdatedAt: now,
	}
	stored, durableStored, created, err := repository.CreateManualSyncJob(manualJob, durable)
	if err != nil {
		return nil, false, err
	}
	return manualSyncView(stored, durableStored), created, nil
}

func (s *service) ManualSyncJobForOwner(owner string, id uuid.UUID) (*ManualSyncJobView, error) {
	repository, ok := s.repo.(manualSyncRepository)
	if !ok {
		return nil, errors.New("durable manual source sync is unavailable")
	}
	job, durable, err := repository.FindManualSyncJobForOwner(strings.TrimSpace(owner), id)
	if err != nil {
		return nil, err
	}
	return manualSyncView(job, durable), nil
}

func (s *service) RunManualSyncJob(ctx context.Context, payload manualSyncPayload, durableJobID uuid.UUID, attempt, maxAttempts int) error {
	sourceID, err := uuid.Parse(strings.TrimSpace(payload.SourceID))
	if err != nil || sourceID == uuid.Nil {
		return errors.New("manual source sync queue payload has an invalid source id")
	}
	syncJobID, err := uuid.Parse(strings.TrimSpace(payload.SyncJobID))
	if err != nil || syncJobID == uuid.Nil || durableJobID == uuid.Nil {
		return errors.New("manual source sync queue payload has an invalid job identity")
	}
	repository, ok := s.repo.(manualSyncRepository)
	if !ok {
		return errors.New("durable manual source sync repository is unavailable")
	}
	job, err := repository.FindManualSyncJob(syncJobID)
	if err != nil {
		return err
	}
	if job.SourceID != sourceID || job.DurableJobID == nil || *job.DurableJobID != durableJobID || job.Mode != ModeManualAsyncSync {
		return errors.New("manual source sync queue identity does not match its owner-bound record")
	}
	if job.Status == "completed" {
		return nil
	}
	if job.Status == "cancelled" {
		return nil
	}
	if attempt <= 0 {
		attempt = 1
	}
	if maxAttempts <= 0 {
		maxAttempts = syncMaxAttempts
	}
	request := ImportRequest{
		Mode: ModeIncrementalSync, ProjectKey: strings.TrimSpace(payload.ProjectKey),
		manualProjectKeyOverride: payload.ProjectKeyOverride,
	}
	result, syncErr := s.syncContext(ctx, sourceID, request, syncJobID)
	if errors.Is(syncErr, ErrSyncInProgress) || errors.Is(syncErr, ErrTrelloSyncAlreadyActive) {
		return durablejob.Defer("manual source sync is waiting for the active source or Trello board sync")
	}
	if errors.Is(syncErr, ErrSourceSyncUnavailable) {
		source, sourceErr := s.repo.FindSource(sourceID)
		if sourceErr == nil && source.ConnectorKey == trelloConnectorKey && !trelloSourceCanSync(source) {
			return durablejob.Defer("manual Trello sync is waiting for its connected source to be enabled")
		}
	}
	if syncErr == nil && result != nil && result.Job.Status == "completed" && result.Job.ItemsFailed == 0 {
		return nil
	}
	if syncErr == nil && result != nil && result.Job.Status == "running" {
		return durablejob.Defer("Trello sync has more durable pages to process")
	}
	if syncErr == nil && result != nil {
		syncErr = fmt.Errorf("manual source sync finished with status %s", result.Job.Status)
	}
	if syncErr == nil {
		syncErr = errors.New("manual source sync returned no completion result")
	}
	failure, classifiedFailure := classifySyncFailure(syncErr)
	message := sourceOperationalFailureMessage(syncErr)
	if classifiedFailure {
		message = failure.message
	}
	if !classifiedFailure && result != nil && strings.TrimSpace(result.Job.Message) != "" {
		message = compact(safety.RedactSecrets(result.Job.Message), manualSyncMessageMax)
	}
	job, err = repository.FindManualSyncJob(syncJobID)
	if err != nil {
		return fmt.Errorf("reload manual source sync record after failure: %w", err)
	}
	job.CursorAfter = job.CursorBefore
	job.Message = message
	if attempt >= maxAttempts || (classifiedFailure && !failure.retryable) {
		job.Status = "failed"
		completedAt := time.Now().UTC()
		job.CompletedAt = &completedAt
	} else {
		job.Status = "queued"
		job.CompletedAt = nil
	}
	if _, persistErr := s.repo.UpdateSyncJob(job); persistErr != nil {
		return fmt.Errorf("manual source sync failed and history could not be updated: %w", persistErr)
	}
	return syncErr
}

func validManualSyncIdempotencyKey(key string) bool {
	if len(key) < manualSyncKeyMin || len(key) > manualSyncKeyMax {
		return false
	}
	for _, value := range key {
		if value < 0x21 || value > 0x7e {
			return false
		}
	}
	return true
}

func manualSyncView(job *models.SourceSyncJob, durable *models.DurableJob) *ManualSyncJobView {
	if job == nil || durable == nil {
		return nil
	}
	status := manualSyncProjectedStatus(job.Status, durable.Status)
	var startedAt *time.Time
	if !job.StartedAt.IsZero() {
		value := job.StartedAt.UTC()
		startedAt = &value
	}
	completedAt := job.CompletedAt
	if status == "queued" || status == "running" {
		completedAt = nil
	}
	var nextAttemptAt *time.Time
	if status == "queued" && !durable.RunAt.IsZero() && durable.RunAt.After(time.Now().UTC()) {
		value := durable.RunAt.UTC()
		nextAttemptAt = &value
	}
	return &ManualSyncJobView{
		ID: job.ID.String(), SourceID: job.SourceID.String(), Mode: job.Mode, Status: status,
		ItemsSeen: boundedSyncCount(job.ItemsSeen), ItemsAdded: boundedSyncCount(job.ItemsAdded),
		ItemsUpdated: boundedSyncCount(job.ItemsUpdated), ItemsFailed: boundedSyncCount(job.ItemsFailed),
		ProgressPhase: job.ProgressPhase, ProgressPages: boundedSyncCount(job.ProgressPages),
		ProgressRecords: boundedSyncCount(job.ProgressRecords), ProgressMessage: compact(safety.RedactSecrets(job.ProgressMessage), manualSyncMessageMax),
		Message: compact(safety.RedactSecrets(manualSyncProjectedMessage(job, durable, status)), manualSyncMessageMax), CreatedAt: job.CreatedAt.UTC(),
		StartedAt: startedAt, CompletedAt: completedAt, Attempt: boundedSyncCount(durable.Attempts),
		MaxAttempts: boundedSyncCount(durable.MaxAttempts), NextAttemptAt: nextAttemptAt,
	}
}

func manualSyncProjectedStatus(historyStatus, durableStatus string) string {
	if historyStatus == "completed" {
		return "completed"
	}
	switch durableStatus {
	case models.DurableJobRunning:
		return "running"
	case models.DurableJobPending:
		return "queued"
	}
	if historyStatus == "cancelled" {
		return "cancelled"
	}
	if durableStatus == models.DurableJobDead || durableStatus == models.DurableJobSucceeded || historyStatus == "failed" || historyStatus == "partial_failure" {
		return "failed"
	}
	if historyStatus == "running" {
		return "running"
	}
	return "queued"
}

func manualSyncProjectedMessage(job *models.SourceSyncJob, durable *models.DurableJob, projectedStatus string) string {
	if job == nil || durable == nil {
		return ""
	}
	if job.Status == "failed" || job.Status == "partial_failure" {
		switch projectedStatus {
		case "running":
			return "A sync attempt failed; the background worker is retrying this job."
		case "queued":
			return "A sync attempt failed; this job is queued for retry."
		}
	}
	if job.Status == "running" && projectedStatus == "queued" {
		return "The worker released this job for a retry."
	}
	return job.Message
}

func boundedSyncCount(value int) int {
	if value < 0 {
		return 0
	}
	if value > 1_000_000_000 {
		return 1_000_000_000
	}
	return value
}
