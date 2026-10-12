package source

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"automation-hub-backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestQueueTrelloWebhookReceiptPostgresReplayIsAtomic(t *testing.T) {
	db := openTrelloRepositoryPostgresTestDB(t)

	owner := "trello-webhook-test-" + uuid.NewString()
	source := newTrelloSource(uuid.New(), "abc123XY", "")
	source.OwnerIdentity = owner
	if err := db.Create(source).Error; err != nil {
		t.Fatalf("create test source: %v", err)
	}

	repository := &GormRepository{DB: db}
	jobIDs := make([]uuid.UUID, 0, 3)
	t.Cleanup(func() {
		if err := db.Transaction(func(tx *gorm.DB) error {
			if err := tx.Where("source_id = ?", source.ID).Delete(&models.TrelloWebhookReceipt{}).Error; err != nil {
				return err
			}
			if len(jobIDs) > 0 {
				if err := tx.Where("id IN ?", jobIDs).Delete(&models.DurableJob{}).Error; err != nil {
					return err
				}
			}
			return tx.Delete(&models.ConnectedSource{}, "id = ?", source.ID).Error
		}); err != nil {
			t.Errorf("clean up Trello webhook PostgreSQL fixture: %v", err)
		}
	})

	newReceiptAndJob := func(actionID, fingerprint string) (*models.TrelloWebhookReceipt, *models.DurableJob) {
		now := time.Now().UTC()
		receiptID, jobID := uuid.New(), uuid.New()
		payload, marshalErr := json.Marshal(trelloWebhookJobPayload{
			SourceID: source.ID.String(), ReceiptID: receiptID.String(), DurableJobID: jobID.String(),
		})
		if marshalErr != nil {
			t.Fatalf("marshal job payload: %v", marshalErr)
		}
		jobIDs = append(jobIDs, jobID)
		return &models.TrelloWebhookReceipt{
				ID: receiptID, SourceID: source.ID, ActionID: actionID, BoardID: "bbbbbbbbbbbbbbbbbbbbbbbb",
				ActionType: "updateCard", OccurredAt: now, Fingerprint: fingerprint, DurableJobID: jobID,
				Status: "queued", ReceivedAt: now, UpdatedAt: now,
			}, &models.DurableJob{
				ID: jobID, Queue: "source", Kind: JobKindTrelloWebhook, Payload: string(payload), Status: models.DurableJobPending,
				RunAt: now, MaxAttempts: syncMaxAttempts, CreatedAt: now, UpdatedAt: now,
			}
	}
	firstDigest := sha256.Sum256([]byte("first payload"))
	firstFingerprint := hex.EncodeToString(firstDigest[:])
	receipt, job := newReceiptAndJob("aaaaaaaaaaaaaaaaaaaaaaaa", firstFingerprint)
	created, err := repository.QueueTrelloWebhookReceipt(source, receipt, job)
	if err != nil || !created {
		t.Fatalf("first queue created=%v err=%v, want created", created, err)
	}
	if receipt.ReconciliationGeneration != 1 {
		t.Fatalf("first receipt generation = %d, want 1", receipt.ReconciliationGeneration)
	}

	duplicate, duplicateJob := newReceiptAndJob("aaaaaaaaaaaaaaaaaaaaaaaa", firstFingerprint)
	created, err = repository.QueueTrelloWebhookReceipt(source, duplicate, duplicateJob)
	if err != nil || created {
		t.Fatalf("duplicate queue created=%v err=%v, want idempotent no-op", created, err)
	}
	var receiptCount, jobCount int64
	if err := db.Model(&models.TrelloWebhookReceipt{}).Where("source_id = ?", source.ID).Count(&receiptCount).Error; err != nil {
		t.Fatalf("count receipts: %v", err)
	}
	if err := db.Model(&models.DurableJob{}).Where("id IN ?", jobIDs).Count(&jobCount).Error; err != nil {
		t.Fatalf("count durable jobs: %v", err)
	}
	if receiptCount != 1 || jobCount != 1 {
		t.Fatalf("after duplicate: receipts=%d durable jobs=%d, want one of each", receiptCount, jobCount)
	}

	otherDigest := sha256.Sum256([]byte("different signed payload"))
	conflicting, conflictingJob := newReceiptAndJob("aaaaaaaaaaaaaaaaaaaaaaaa", hex.EncodeToString(otherDigest[:]))
	created, err = repository.QueueTrelloWebhookReceipt(source, conflicting, conflictingJob)
	if !errors.Is(err, ErrTrelloWebhookReplayConflict) || created {
		t.Fatalf("conflicting replay created=%v err=%v, want replay conflict", created, err)
	}
	if err := db.Model(&models.DurableJob{}).Where("id IN ?", jobIDs).Count(&jobCount).Error; err != nil {
		t.Fatalf("count jobs after conflicting replay: %v", err)
	}
	if jobCount != 1 {
		t.Fatalf("conflicting replay left %d durable jobs, want only the original", jobCount)
	}
	var state models.TrelloWebhookReconciliationState
	if err := db.Where("source_id = ?", source.ID).First(&state).Error; err != nil {
		t.Fatalf("load reconciliation state: %v", err)
	}
	if state.RequestedGeneration != 1 {
		t.Fatalf("duplicate or conflicting delivery advanced requested generation to %d, want 1", state.RequestedGeneration)
	}
}

func TestTrelloWebhookReconciliationGenerationCoalescesAndPreservesNewEvents(t *testing.T) {
	db := openTrelloRepositoryPostgresTestDB(t)
	withTrelloPostgresRollback(t, db, func(tx *gorm.DB) error {
		owner := "trello-reconciliation-test-" + uuid.NewString()
		source := newTrelloSource(uuid.New(), "abc123XY", "")
		source.OwnerIdentity = owner
		if err := tx.Create(source).Error; err != nil {
			return err
		}
		boardID, err := trelloBoardID(source.SyncTarget)
		if err != nil {
			return err
		}
		priorSuccess := time.Now().UTC().Add(-time.Hour)
		if err := tx.Create(&models.TrelloSyncState{
			SourceID: source.ID, OwnerIdentity: owner, BoardID: boardID, Generation: 4,
			Phase: trelloPhaseIdle, CycleStartedAt: priorSuccess, LastSuccessfulAt: &priorSuccess,
		}).Error; err != nil {
			return err
		}
		repository := &GormRepository{DB: tx}
		queue := func(actionID string) (*models.TrelloWebhookReceipt, error) {
			now := time.Now().UTC()
			receiptID, durableID := uuid.New(), uuid.New()
			payload, err := json.Marshal(trelloWebhookJobPayload{SourceID: source.ID.String(), ReceiptID: receiptID.String(), DurableJobID: durableID.String()})
			if err != nil {
				return nil, err
			}
			receipt := &models.TrelloWebhookReceipt{
				ID: receiptID, SourceID: source.ID, ActionID: actionID, BoardID: "bbbbbbbbbbbbbbbbbbbbbbbb",
				ActionType: "updateCard", OccurredAt: now, Fingerprint: webhookFingerprint([]byte(actionID)),
				DurableJobID: durableID, Status: "queued", ReceivedAt: now, UpdatedAt: now,
			}
			job := &models.DurableJob{ID: durableID, Queue: "source", Kind: JobKindTrelloWebhook, Payload: string(payload),
				Status: models.DurableJobPending, RunAt: now, MaxAttempts: syncMaxAttempts, CreatedAt: now, UpdatedAt: now}
			created, err := repository.QueueTrelloWebhookReceipt(source, receipt, job)
			if err != nil {
				return nil, err
			}
			if !created {
				return nil, errors.New("new Trello action was not inserted")
			}
			return receipt, nil
		}
		first, err := queue("111111111111111111111111")
		if err != nil {
			return err
		}
		second, err := queue("222222222222222222222222")
		if err != nil {
			return err
		}
		if first.ReconciliationGeneration != 1 || second.ReconciliationGeneration != 2 {
			return fmt.Errorf("receipt generations = %d/%d, want 1/2", first.ReconciliationGeneration, second.ReconciliationGeneration)
		}
		var initialState models.TrelloWebhookReconciliationState
		if err := tx.Where("source_id = ?", source.ID).First(&initialState).Error; err != nil {
			return err
		}
		if initialState.RequiredTrelloGeneration != 5 {
			return fmt.Errorf("required Trello generation = %d, want 5 after existing generation 4", initialState.RequiredTrelloGeneration)
		}
		firstClaim, err := repository.ClaimTrelloWebhookSync(source.ID, first.ID, owner, time.Now().UTC())
		if err != nil {
			return err
		}
		if firstClaim.Generation != 2 || firstClaim.DispatchAttempt != 1 || firstClaim.Waiting {
			return fmt.Errorf("first claim = %#v, want fixed coalesced generation 2 attempt 1", firstClaim)
		}
		late, err := queue("333333333333333333333333")
		if err != nil {
			return err
		}
		lateClaim, err := repository.ClaimTrelloWebhookSync(source.ID, late.ID, owner, time.Now().UTC())
		if err != nil {
			return err
		}
		if !lateClaim.Waiting || lateClaim.Generation != 3 {
			return fmt.Errorf("event received during sync claim = %#v, want wait for generation 3", lateClaim)
		}
		firstSyncID := uuid.New()
		now := time.Now().UTC()
		if err := tx.Create(&models.SourceSyncJob{ID: firstSyncID, SourceID: source.ID, OwnerIdentity: owner,
			IdempotencyKeyHash: strings.Repeat("a", 64), RequestHash: strings.Repeat("a", 64),
			Mode: ModeManualAsyncSync, Status: "queued", CreatedAt: now, UpdatedAt: now}).Error; err != nil {
			return err
		}
		if err := repository.BindTrelloWebhookSyncJob(source.ID, 2, 1, firstSyncID, now); err != nil {
			return err
		}
		if err := tx.Model(&models.SourceSyncJob{}).Where("id = ?", firstSyncID).Update("status", "failed").Error; err != nil {
			return err
		}
		retry, err := repository.FailTrelloWebhookSyncAttempt(source.ID, 2, 1, 2, now.Add(time.Second))
		if err != nil {
			return err
		}
		if !retry {
			return errors.New("first failed dispatch unexpectedly exhausted its retry limit")
		}
		retryClaim, err := repository.ClaimTrelloWebhookSync(source.ID, first.ID, owner, now.Add(2*time.Second))
		if err != nil {
			return err
		}
		if retryClaim.Generation != 2 || retryClaim.DispatchAttempt != 2 || retryClaim.SyncJobID != nil {
			return fmt.Errorf("retry claim = %#v, want same generation and second dispatch", retryClaim)
		}
		secondSyncID := uuid.New()
		oldScanAt := now.Add(3 * time.Second)
		cursor := "trello:verified-generation-4"
		source.Cursor = cursor
		source.LastSyncedAt = &oldScanAt
		if err := tx.Model(&models.ConnectedSource{}).Where("id = ?", source.ID).
			Updates(map[string]any{"cursor": cursor, "last_synced_at": oldScanAt}).Error; err != nil {
			return err
		}
		if err := tx.Model(&models.TrelloSyncState{}).Where("source_id = ?", source.ID).
			Updates(map[string]any{"phase": trelloPhaseIdle, "logical_job_id": nil, "last_successful_at": oldScanAt}).Error; err != nil {
			return err
		}
		if err := tx.Create(&models.SourceSyncJob{ID: secondSyncID, SourceID: source.ID, OwnerIdentity: owner,
			IdempotencyKeyHash: strings.Repeat("b", 64), RequestHash: strings.Repeat("b", 64),
			Mode: ModeManualAsyncSync, Status: "completed", CursorAfter: cursor, ItemsFailed: 0,
			ProgressPhase: "complete", ProgressPages: 4, CompletedAt: &oldScanAt, CreatedAt: now, UpdatedAt: now}).Error; err != nil {
			return err
		}
		if err := repository.BindTrelloWebhookSyncJob(source.ID, 2, 2, secondSyncID, now); err != nil {
			return err
		}
		completion, err := repository.CompleteTrelloWebhookSyncBatch(source.ID, 2, 2, secondSyncID, 3, oldScanAt)
		if err != nil {
			return err
		}
		if completion.Completed || !completion.Retry {
			return fmt.Errorf("old checkpoint completion = %#v, want a fresh-scan retry", completion)
		}
		var state models.TrelloWebhookReconciliationState
		if err := tx.Where("source_id = ?", source.ID).First(&state).Error; err != nil {
			return err
		}
		if state.RequestedGeneration != 3 || state.CompletedGeneration != 0 || state.ActiveGeneration == nil ||
			*state.ActiveGeneration != 2 || state.DispatchAttempt != 3 || state.ActiveSyncJobID != nil ||
			state.ActiveRequiredTrelloGeneration == nil || *state.ActiveRequiredTrelloGeneration != 5 {
			return fmt.Errorf("state after old checkpoint completion = %#v, want generation 2 to retry at attempt 3 with required Trello generation 5", state)
		}
		thirdSyncID := uuid.New()
		freshScanAt := now.Add(4 * time.Second)
		if err := tx.Model(&models.TrelloSyncState{}).Where("source_id = ?", source.ID).
			Updates(map[string]any{"generation": 5, "phase": trelloPhaseIdle, "logical_job_id": nil, "last_successful_at": freshScanAt}).Error; err != nil {
			return err
		}
		if err := tx.Create(&models.SourceSyncJob{ID: thirdSyncID, SourceID: source.ID, OwnerIdentity: owner,
			IdempotencyKeyHash: strings.Repeat("c", 64), RequestHash: strings.Repeat("c", 64),
			Mode: ModeManualAsyncSync, Status: "completed", CursorAfter: cursor, ItemsFailed: 0,
			ProgressPhase: "complete", ProgressPages: 1, CompletedAt: &freshScanAt, CreatedAt: now, UpdatedAt: now}).Error; err != nil {
			return err
		}
		if err := repository.BindTrelloWebhookSyncJob(source.ID, 2, 3, thirdSyncID, freshScanAt); err != nil {
			return err
		}
		completion, err = repository.CompleteTrelloWebhookSyncBatch(source.ID, 2, 3, thirdSyncID, 3, freshScanAt)
		if err != nil {
			return err
		}
		if !completion.Completed || completion.Retry {
			return fmt.Errorf("fresh checkpoint completion = %#v, want completion", completion)
		}
		if err := tx.Where("source_id = ?", source.ID).First(&state).Error; err != nil {
			return err
		}
		if state.RequestedGeneration != 3 || state.CompletedGeneration != 2 || state.ActiveGeneration != nil {
			return fmt.Errorf("state after generation 2 completion = %#v, want requested 3 and completed 2 with no active claim", state)
		}
		thirdClaim, err := repository.ClaimTrelloWebhookSync(source.ID, late.ID, owner, now.Add(4*time.Second))
		if err != nil {
			return err
		}
		if thirdClaim.Generation != 3 || thirdClaim.DispatchAttempt != 1 || thirdClaim.Waiting {
			return fmt.Errorf("follow-up claim = %#v, want independent generation 3 attempt 1", thirdClaim)
		}
		var completedReceipts int64
		if err := tx.Model(&models.TrelloWebhookReceipt{}).Where("source_id = ? AND status = 'completed'", source.ID).Count(&completedReceipts).Error; err != nil {
			return err
		}
		if completedReceipts != 2 {
			return fmt.Errorf("completed receipt count = %d, want 2", completedReceipts)
		}
		return nil
	})
}

func TestTrelloWebhookTerminalFailureReleasesGenerationForNewEvents(t *testing.T) {
	db := openTrelloRepositoryPostgresTestDB(t)
	withTrelloPostgresRollback(t, db, func(tx *gorm.DB) error {
		owner := "trello-terminal-generation-test-" + uuid.NewString()
		source := newTrelloSource(uuid.New(), "abc123XY", "")
		source.OwnerIdentity = owner
		if err := tx.Create(source).Error; err != nil {
			return err
		}
		repository := &GormRepository{DB: tx}
		queue := func(actionID string) (*models.TrelloWebhookReceipt, error) {
			now := time.Now().UTC()
			receiptID, durableID := uuid.New(), uuid.New()
			payload, err := json.Marshal(trelloWebhookJobPayload{SourceID: source.ID.String(), ReceiptID: receiptID.String(), DurableJobID: durableID.String()})
			if err != nil {
				return nil, err
			}
			receipt := &models.TrelloWebhookReceipt{
				ID: receiptID, SourceID: source.ID, ActionID: actionID, BoardID: "bbbbbbbbbbbbbbbbbbbbbbbb",
				ActionType: "updateCard", OccurredAt: now, Fingerprint: webhookFingerprint([]byte(actionID)),
				DurableJobID: durableID, Status: "queued", ReceivedAt: now, UpdatedAt: now,
			}
			job := &models.DurableJob{ID: durableID, Queue: "source", Kind: JobKindTrelloWebhook, Payload: string(payload),
				Status: models.DurableJobPending, RunAt: now, MaxAttempts: syncMaxAttempts, CreatedAt: now, UpdatedAt: now}
			created, err := repository.QueueTrelloWebhookReceipt(source, receipt, job)
			if err != nil {
				return nil, err
			}
			if !created {
				return nil, errors.New("new Trello action was not inserted")
			}
			return receipt, nil
		}
		first, err := queue("111111111111111111111111")
		if err != nil {
			return err
		}
		second, err := queue("222222222222222222222222")
		if err != nil {
			return err
		}
		if first.ReconciliationGeneration != 1 || second.ReconciliationGeneration != 2 {
			return fmt.Errorf("terminal test receipt generations = %d/%d, want 1/2", first.ReconciliationGeneration, second.ReconciliationGeneration)
		}
		claim, err := repository.ClaimTrelloWebhookSync(source.ID, first.ID, owner, time.Now().UTC())
		if err != nil {
			return err
		}
		if claim.Generation != 2 || claim.DispatchAttempt != 1 || claim.Waiting {
			return fmt.Errorf("initial terminal-test claim = %#v, want coalesced generation 2", claim)
		}
		retry, err := repository.FailTrelloWebhookSyncAttempt(source.ID, claim.Generation, claim.DispatchAttempt, claim.DispatchAttempt, time.Now().UTC())
		if err != nil {
			return err
		}
		if retry {
			return errors.New("exhausted generation unexpectedly requested a retry")
		}
		var state models.TrelloWebhookReconciliationState
		if err := tx.Where("source_id = ?", source.ID).First(&state).Error; err != nil {
			return err
		}
		if state.ActiveGeneration != nil || state.ActiveRequiredTrelloGeneration != nil || state.ActiveSyncJobID != nil ||
			state.DispatchAttempt != 0 || state.FailedGeneration == nil || *state.FailedGeneration != 2 {
			return fmt.Errorf("state after terminal failure = %#v, want generation 2 failed and no active generation", state)
		}
		var failedReceipts int64
		if err := tx.Model(&models.TrelloWebhookReceipt{}).
			Where("source_id = ? AND reconciliation_generation <= ? AND status = 'failed'", source.ID, 2).
			Count(&failedReceipts).Error; err != nil {
			return err
		}
		if failedReceipts != 2 {
			return fmt.Errorf("terminally failed receipts = %d, want both coalesced receipts", failedReceipts)
		}
		third, err := queue("333333333333333333333333")
		if err != nil {
			return err
		}
		if third.ReconciliationGeneration != 3 {
			return fmt.Errorf("receipt after terminal failure generation = %d, want 3", third.ReconciliationGeneration)
		}
		next, err := repository.ClaimTrelloWebhookSync(source.ID, third.ID, owner, time.Now().UTC())
		if err != nil {
			return err
		}
		if next.Generation != 3 || next.DispatchAttempt != 1 || next.Waiting || next.PreviouslyFailed {
			return fmt.Errorf("claim after terminal failure = %#v, want generation 3 to start normally", next)
		}
		return nil
	})
}

func TestQueueTrelloWebhookReceiptPostgresAssignsConcurrentGenerationsExactlyOnce(t *testing.T) {
	db := openTrelloRepositoryPostgresTestDB(t)
	owner := "trello-generation-concurrency-" + uuid.NewString()
	source := newTrelloSource(uuid.New(), "abc123XY", "")
	source.OwnerIdentity = owner
	if err := db.Create(source).Error; err != nil {
		t.Fatalf("create test source: %v", err)
	}
	const count = 8
	jobIDs := make([]uuid.UUID, 0, count+1)
	t.Cleanup(func() {
		if err := db.Transaction(func(tx *gorm.DB) error {
			if err := tx.Where("source_id = ?", source.ID).Delete(&models.TrelloWebhookReceipt{}).Error; err != nil {
				return err
			}
			if len(jobIDs) > 0 {
				if err := tx.Where("id IN ?", jobIDs).Delete(&models.DurableJob{}).Error; err != nil {
					return err
				}
			}
			return tx.Delete(&models.ConnectedSource{}, "id = ?", source.ID).Error
		}); err != nil {
			t.Errorf("clean up concurrent Trello webhook fixtures: %v", err)
		}
	})

	repository := &GormRepository{DB: db}
	type result struct {
		generation int64
		jobID      uuid.UUID
		err        error
	}
	results := make(chan result, count)
	var workers sync.WaitGroup
	for index := 1; index <= count; index++ {
		index := index
		workers.Add(1)
		go func() {
			defer workers.Done()
			now := time.Now().UTC()
			receiptID, durableID := uuid.New(), uuid.New()
			actionID := fmt.Sprintf("%024x", index)
			payload, err := json.Marshal(trelloWebhookJobPayload{
				SourceID: source.ID.String(), ReceiptID: receiptID.String(), DurableJobID: durableID.String(),
			})
			if err != nil {
				results <- result{jobID: durableID, err: err}
				return
			}
			receipt := &models.TrelloWebhookReceipt{
				ID: receiptID, SourceID: source.ID, ActionID: actionID, BoardID: "bbbbbbbbbbbbbbbbbbbbbbbb",
				ActionType: "updateCard", OccurredAt: now, Fingerprint: webhookFingerprint([]byte(actionID)),
				DurableJobID: durableID, Status: "queued", ReceivedAt: now, UpdatedAt: now,
			}
			job := &models.DurableJob{
				ID: durableID, Queue: "source", Kind: JobKindTrelloWebhook, Payload: string(payload),
				Status: models.DurableJobPending, RunAt: now, MaxAttempts: syncMaxAttempts, CreatedAt: now, UpdatedAt: now,
			}
			created, err := repository.QueueTrelloWebhookReceipt(source, receipt, job)
			if err == nil && !created {
				err = errors.New("unique concurrent Trello action was not inserted")
			}
			results <- result{generation: receipt.ReconciliationGeneration, jobID: durableID, err: err}
		}()
	}
	workers.Wait()
	close(results)
	generations := make([]int, 0, count)
	for item := range results {
		jobIDs = append(jobIDs, item.jobID)
		if item.err != nil {
			t.Fatalf("concurrent webhook intake: %v", item.err)
		}
		generations = append(generations, int(item.generation))
	}
	sort.Ints(generations)
	for index, generation := range generations {
		if generation != index+1 {
			t.Fatalf("concurrent generation sequence = %v, want exact sequence 1..%d", generations, count)
		}
	}
	var state models.TrelloWebhookReconciliationState
	if err := db.Where("source_id = ?", source.ID).First(&state).Error; err != nil {
		t.Fatalf("load concurrent source reconciliation state: %v", err)
	}
	if state.RequestedGeneration != count || state.CompletedGeneration != 0 {
		t.Fatalf("concurrent reconciliation state = requested %d/completed %d, want %d/0", state.RequestedGeneration, state.CompletedGeneration, count)
	}
}
