package source

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"automation-hub-backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (r *GormRepository) QueueTrelloWebhookReceipt(source *models.ConnectedSource, receipt *models.TrelloWebhookReceipt, job *models.DurableJob) (bool, error) {
	if source == nil || receipt == nil || job == nil || source.ID == uuid.Nil || receipt.ID == uuid.Nil ||
		receipt.SourceID != source.ID || receipt.DurableJobID == uuid.Nil || receipt.DurableJobID != job.ID ||
		receipt.ActionID == "" || receipt.BoardID == "" || receipt.ActionType == "" ||
		!validTrelloFingerprint(receipt.Fingerprint) || job.Queue != "source" || job.Kind != JobKindTrelloWebhook {
		return false, errors.New("Trello webhook receipt and durable job identity are invalid")
	}
	var payload trelloWebhookJobPayload
	if err := json.Unmarshal([]byte(job.Payload), &payload); err != nil || payload.SourceID != source.ID.String() ||
		payload.ReceiptID != receipt.ID.String() || payload.DurableJobID != job.ID.String() {
		return false, errors.New("Trello webhook job payload does not match its receipt")
	}
	if id, valid := normalizedTrelloMongoID(receipt.ActionID); !valid || id != receipt.ActionID {
		return false, errors.New("Trello webhook action identity is invalid")
	}
	if !trelloIDPattern.MatchString(receipt.BoardID) {
		return false, errors.New("Trello webhook board identity is invalid")
	}
	if receipt.CardID != "" {
		if id, valid := normalizedTrelloMongoID(receipt.CardID); !valid || id != receipt.CardID {
			return false, errors.New("Trello webhook card identity is invalid")
		}
	}
	if len(receipt.ActionText) > trelloWebhookTextLimit || len(receipt.CardName) > 512 || len(receipt.CardURL) > 1024 {
		return false, errors.New("Trello webhook evidence exceeds its storage limit")
	}

	created := false
	err := r.DB.Transaction(func(tx *gorm.DB) error {
		var persisted models.ConnectedSource
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND owner_identity = ? AND connector_key = ?", source.ID, source.OwnerIdentity, trelloConnectorKey).
			First(&persisted).Error; err != nil {
			return err
		}
		if persisted.SyncTarget != source.SyncTarget || !trelloSourceCanSync(&persisted) {
			return ErrTrelloSyncSourceInactive
		}
		receipt.ReceivedAt = receipt.ReceivedAt.UTC()
		receipt.OccurredAt = receipt.OccurredAt.UTC()
		// The receipt references its durable job, so insert the job first. Both
		// writes are in this transaction and roll back together on any conflict.
		if err := tx.Create(job).Error; err != nil {
			return fmt.Errorf("persist Trello webhook worker job: %w", err)
		}
		result := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "source_id"}, {Name: "action_id"}},
			DoNothing: true,
		}).Create(receipt)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			if err := tx.Delete(&models.DurableJob{}, "id = ?", job.ID).Error; err != nil {
				return fmt.Errorf("remove duplicate Trello webhook worker job: %w", err)
			}
			var existing models.TrelloWebhookReceipt
			if err := tx.Where("source_id = ? AND action_id = ?", receipt.SourceID, receipt.ActionID).First(&existing).Error; err != nil {
				return err
			}
			if existing.Fingerprint != receipt.Fingerprint {
				return ErrTrelloWebhookReplayConflict
			}
			return nil
		}
		created = true
		if !isTrelloWebhookCommentChange(receipt.ActionType) {
			requiredTrelloGeneration := int64(1)
			var syncState models.TrelloSyncState
			syncStateErr := tx.Where("source_id = ?", source.ID).First(&syncState).Error
			if syncStateErr == nil {
				boardID, boardErr := trelloBoardID(persisted.SyncTarget)
				if boardErr != nil || syncState.OwnerIdentity != persisted.OwnerIdentity || syncState.BoardID != boardID || syncState.Generation < 0 {
					return ErrTrelloSyncBindingChanged
				}
				requiredTrelloGeneration = syncState.Generation + 1
				if requiredTrelloGeneration <= syncState.Generation {
					return errors.New("Trello sync generation is exhausted")
				}
			} else if !errors.Is(syncStateErr, gorm.ErrRecordNotFound) {
				return fmt.Errorf("load Trello sync generation for webhook receipt: %w", syncStateErr)
			}
			var state models.TrelloWebhookReconciliationState
			stateErr := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
				Where("source_id = ?", source.ID).First(&state).Error
			if errors.Is(stateErr, gorm.ErrRecordNotFound) {
				state = models.TrelloWebhookReconciliationState{
					SourceID: source.ID, OwnerIdentity: source.OwnerIdentity,
					RequestedGeneration:      1,
					RequiredTrelloGeneration: requiredTrelloGeneration,
					UpdatedAt:                receipt.ReceivedAt,
				}
				stateErr = tx.Create(&state).Error
			} else if stateErr == nil {
				if state.OwnerIdentity != source.OwnerIdentity {
					return errors.New("Trello reconciliation state owner does not match its connected source")
				}
				state.RequestedGeneration++
				if state.RequestedGeneration <= 0 {
					return errors.New("Trello webhook reconciliation generation is exhausted")
				}
				if requiredTrelloGeneration > state.RequiredTrelloGeneration {
					state.RequiredTrelloGeneration = requiredTrelloGeneration
				}
				state.UpdatedAt = receipt.ReceivedAt
				stateErr = tx.Model(&state).Select("requested_generation", "required_trello_generation", "updated_at").Updates(&state).Error
			}
			if stateErr != nil {
				return fmt.Errorf("advance Trello reconciliation generation: %w", stateErr)
			}
			receipt.ReconciliationGeneration = state.RequestedGeneration
			if err := tx.Model(&models.TrelloWebhookReceipt{}).
				Where("id = ? AND source_id = ?", receipt.ID, source.ID).
				Update("reconciliation_generation", receipt.ReconciliationGeneration).Error; err != nil {
				return fmt.Errorf("attach Trello reconciliation generation to receipt: %w", err)
			}
		}
		return nil
	})
	return created, err
}

func (r *GormRepository) FindTrelloWebhookReceipt(sourceID, receiptID uuid.UUID) (*models.TrelloWebhookReceipt, error) {
	if sourceID == uuid.Nil || receiptID == uuid.Nil {
		return nil, gorm.ErrRecordNotFound
	}
	var receipt models.TrelloWebhookReceipt
	if err := r.DB.Where("id = ? AND source_id = ?", receiptID, sourceID).First(&receipt).Error; err != nil {
		return nil, err
	}
	return &receipt, nil
}

// FindLatestTrelloWebhookReceipt returns the newest persisted callback receipt
// for health reporting. A receipt proves only that a signed callback was
// accepted and persisted; it does not prove current delivery or registration.
func (r *GormRepository) FindLatestTrelloWebhookReceipt(sourceID uuid.UUID) (*models.TrelloWebhookReceipt, error) {
	if sourceID == uuid.Nil {
		return nil, gorm.ErrRecordNotFound
	}
	var receipt models.TrelloWebhookReceipt
	if err := r.DB.Where("source_id = ?", sourceID).
		Order("received_at DESC").
		Order("id DESC").
		First(&receipt).Error; err != nil {
		return nil, err
	}
	return &receipt, nil
}

func (r *GormRepository) ClaimTrelloWebhookSync(sourceID, receiptID uuid.UUID, owner string, now time.Time) (*trelloWebhookSyncClaim, error) {
	if sourceID == uuid.Nil || receiptID == uuid.Nil || strings.TrimSpace(owner) == "" {
		return nil, errors.New("Trello webhook reconciliation identity is incomplete")
	}
	claim := &trelloWebhookSyncClaim{}
	err := r.DB.Transaction(func(tx *gorm.DB) error {
		var state models.TrelloWebhookReconciliationState
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("source_id = ? AND owner_identity = ?", sourceID, owner).First(&state).Error; err != nil {
			return err
		}
		var receipt models.TrelloWebhookReceipt
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND source_id = ?", receiptID, sourceID).First(&receipt).Error; err != nil {
			return err
		}
		if receipt.ReconciliationGeneration <= 0 || isTrelloWebhookCommentChange(receipt.ActionType) {
			return errors.New("Trello webhook receipt has no board reconciliation generation")
		}
		if receipt.ReconciliationGeneration > state.RequestedGeneration {
			return errors.New("Trello webhook receipt generation exceeds the requested source generation")
		}
		claim.Generation = receipt.ReconciliationGeneration
		if receipt.ReconciliationGeneration <= state.CompletedGeneration {
			claim.AlreadyComplete = true
			return nil
		}
		if state.ActiveGeneration == nil {
			if state.FailedGeneration != nil && state.RequestedGeneration <= *state.FailedGeneration {
				claim.PreviouslyFailed = true
				return nil
			}
			generation := state.RequestedGeneration
			state.ActiveGeneration = &generation
			requiredTrelloGeneration := state.RequiredTrelloGeneration
			if requiredTrelloGeneration <= 0 {
				return errors.New("Trello reconciliation has no required full-sync generation")
			}
			state.ActiveRequiredTrelloGeneration = &requiredTrelloGeneration
			state.ActiveSyncJobID = nil
			state.DispatchAttempt = 1
			state.FailedGeneration = nil
			state.UpdatedAt = now.UTC()
			if err := tx.Model(&state).Select("active_generation", "active_required_trello_generation", "active_sync_job_id", "dispatch_attempt", "failed_generation", "updated_at").Updates(&state).Error; err != nil {
				return err
			}
		} else if state.ActiveRequiredTrelloGeneration == nil || *state.ActiveRequiredTrelloGeneration <= 0 {
			return errors.New("active Trello reconciliation has no required full-sync generation")
		}
		if receipt.ReconciliationGeneration > *state.ActiveGeneration {
			claim.Waiting = true
			return nil
		}
		claim.Generation = *state.ActiveGeneration
		claim.DispatchAttempt = state.DispatchAttempt
		claim.SyncJobID = state.ActiveSyncJobID
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("claim Trello webhook sync generation: %w", err)
	}
	return claim, nil
}

func (r *GormRepository) BindTrelloWebhookSyncJob(sourceID uuid.UUID, generation int64, attempt int, syncJobID uuid.UUID, now time.Time) error {
	if sourceID == uuid.Nil || generation <= 0 || attempt <= 0 || syncJobID == uuid.Nil {
		return errors.New("Trello webhook sync job identity is incomplete")
	}
	return r.DB.Transaction(func(tx *gorm.DB) error {
		var state models.TrelloWebhookReconciliationState
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("source_id = ?", sourceID).First(&state).Error; err != nil {
			return err
		}
		if state.ActiveGeneration == nil || *state.ActiveGeneration != generation || state.DispatchAttempt != attempt {
			return errors.New("Trello webhook reconciliation generation changed before sync binding")
		}
		if state.ActiveSyncJobID != nil && *state.ActiveSyncJobID != syncJobID {
			return errors.New("Trello webhook reconciliation is already bound to a different sync job")
		}
		if state.ActiveSyncJobID == nil {
			state.ActiveSyncJobID = &syncJobID
			state.UpdatedAt = now.UTC()
			if err := tx.Model(&state).Select("active_sync_job_id", "updated_at").Updates(&state).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *GormRepository) FailTrelloWebhookSyncAttempt(sourceID uuid.UUID, generation int64, attempt, maxAttempts int, now time.Time) (bool, error) {
	if sourceID == uuid.Nil || generation <= 0 || attempt <= 0 || maxAttempts <= 0 {
		return false, errors.New("Trello webhook sync failure identity is incomplete")
	}
	retry := false
	err := r.DB.Transaction(func(tx *gorm.DB) error {
		var state models.TrelloWebhookReconciliationState
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("source_id = ?", sourceID).First(&state).Error; err != nil {
			return err
		}
		if state.ActiveGeneration == nil || *state.ActiveGeneration != generation || state.DispatchAttempt != attempt {
			return errors.New("Trello webhook reconciliation generation changed before failure recording")
		}
		state.ActiveSyncJobID = nil
		state.UpdatedAt = now.UTC()
		if attempt < maxAttempts {
			state.DispatchAttempt = attempt + 1
			retry = true
		} else {
			failedGeneration := generation
			state.FailedGeneration = &failedGeneration
			state.ActiveGeneration = nil
			state.DispatchAttempt = 0
		}
		if attempt >= maxAttempts {
			state.ActiveRequiredTrelloGeneration = nil
		}
		if err := tx.Model(&state).Select("active_generation", "active_required_trello_generation", "active_sync_job_id", "dispatch_attempt", "failed_generation", "updated_at").Updates(&state).Error; err != nil {
			return err
		}
		if attempt >= maxAttempts {
			return tx.Model(&models.TrelloWebhookReceipt{}).
				Where("source_id = ? AND reconciliation_generation <= ? AND status IN ? AND action_type NOT IN ?",
					sourceID, generation, []string{"queued", "dispatched"}, []string{"commentCard", "copyCommentCard", "updateComment", "deleteComment"}).
				Updates(map[string]any{"status": "failed", "completed_at": now.UTC(), "updated_at": now.UTC()}).Error
		}
		return nil
	})
	if err != nil {
		return false, fmt.Errorf("record Trello webhook sync failure: %w", err)
	}
	return retry, nil
}

type trelloWebhookSyncCompletion struct {
	Completed bool
	Retry     bool
}

func (r *GormRepository) CompleteTrelloWebhookSyncBatch(sourceID uuid.UUID, generation int64, attempt int, syncJobID uuid.UUID, maxAttempts int, now time.Time) (trelloWebhookSyncCompletion, error) {
	if sourceID == uuid.Nil || generation <= 0 || attempt <= 0 {
		return trelloWebhookSyncCompletion{}, errors.New("Trello webhook sync completion identity is incomplete")
	}
	if syncJobID == uuid.Nil || maxAttempts <= 0 {
		return trelloWebhookSyncCompletion{}, errors.New("Trello webhook completion requires an exact sync job and retry limit")
	}
	result := trelloWebhookSyncCompletion{}
	err := r.DB.Transaction(func(tx *gorm.DB) error {
		var source models.ConnectedSource
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND connector_key = ?", sourceID, trelloConnectorKey).First(&source).Error; err != nil {
			return err
		}
		boardID, err := trelloBoardID(source.SyncTarget)
		if err != nil {
			return ErrTrelloSyncBindingChanged
		}
		var syncState models.TrelloSyncState
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("source_id = ? AND owner_identity = ? AND board_id = ?", sourceID, source.OwnerIdentity, boardID).
			First(&syncState).Error; err != nil {
			return err
		}
		var state models.TrelloWebhookReconciliationState
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("source_id = ?", sourceID).First(&state).Error; err != nil {
			return err
		}
		if state.ActiveGeneration == nil || *state.ActiveGeneration != generation || state.DispatchAttempt != attempt ||
			state.ActiveSyncJobID == nil || *state.ActiveSyncJobID != syncJobID || state.ActiveRequiredTrelloGeneration == nil {
			return errors.New("Trello webhook reconciliation generation changed before completion")
		}
		var syncJob models.SourceSyncJob
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND source_id = ? AND owner_identity = ? AND mode = ?", syncJobID, sourceID, source.OwnerIdentity, ModeManualAsyncSync).
			First(&syncJob).Error; err != nil {
			return err
		}
		if syncJob.Status != "completed" || syncJob.ItemsFailed != 0 || syncJob.CompletedAt == nil ||
			syncJob.ProgressPhase != "complete" || syncJob.ProgressPages <= 0 ||
			syncState.Phase != trelloPhaseIdle || syncState.LogicalJobID != nil || syncState.LastSuccessfulAt == nil ||
			syncState.Generation <= 0 || strings.TrimSpace(source.Cursor) == "" || syncJob.CursorAfter != source.Cursor {
			return errors.New("Trello webhook sync job does not prove a fully committed board scan")
		}
		if syncState.Generation < *state.ActiveRequiredTrelloGeneration {
			state.ActiveSyncJobID = nil
			state.UpdatedAt = now.UTC()
			if attempt < maxAttempts {
				state.DispatchAttempt = attempt + 1
				result.Retry = true
			} else {
				failedGeneration := generation
				state.FailedGeneration = &failedGeneration
				state.ActiveGeneration = nil
				state.ActiveRequiredTrelloGeneration = nil
				state.DispatchAttempt = 0
			}
			if err := tx.Model(&state).Select("active_generation", "active_required_trello_generation", "active_sync_job_id", "dispatch_attempt", "failed_generation", "updated_at").Updates(&state).Error; err != nil {
				return err
			}
			if !result.Retry {
				return tx.Model(&models.TrelloWebhookReceipt{}).
					Where("source_id = ? AND reconciliation_generation <= ? AND status IN ? AND action_type NOT IN ?",
						sourceID, generation, []string{"queued", "dispatched"}, []string{"commentCard", "copyCommentCard", "updateComment", "deleteComment"}).
					Updates(map[string]any{"status": "failed", "completed_at": now.UTC(), "updated_at": now.UTC()}).Error
			}
			return nil
		}
		state.CompletedGeneration = generation
		state.ActiveGeneration = nil
		state.ActiveRequiredTrelloGeneration = nil
		state.ActiveSyncJobID = nil
		state.DispatchAttempt = 0
		state.FailedGeneration = nil
		state.UpdatedAt = now.UTC()
		if err := tx.Model(&state).Select("completed_generation", "active_generation", "active_required_trello_generation", "active_sync_job_id", "dispatch_attempt", "failed_generation", "updated_at").Updates(&state).Error; err != nil {
			return err
		}
		if err := tx.Model(&models.TrelloWebhookReceipt{}).
			Where("source_id = ? AND reconciliation_generation <= ? AND status IN ? AND action_type NOT IN ?",
				sourceID, generation, []string{"queued", "dispatched"}, []string{"commentCard", "copyCommentCard", "updateComment", "deleteComment"}).
			Updates(map[string]any{"status": "completed", "completed_at": now.UTC(), "updated_at": now.UTC()}).Error; err != nil {
			return err
		}
		result.Completed = true
		return nil
	})
	if err != nil {
		return trelloWebhookSyncCompletion{}, err
	}
	return result, nil
}

func (r *GormRepository) CompleteTrelloWebhookReceipt(sourceID, receiptID uuid.UUID, status string, now time.Time) error {
	if status != "completed" && status != "ignored" && status != "failed" {
		return errors.New("Trello webhook terminal status is invalid")
	}
	result := r.DB.Model(&models.TrelloWebhookReceipt{}).
		Where("id = ? AND source_id = ? AND status IN ?", receiptID, sourceID, []string{"queued", "dispatched"}).
		Updates(map[string]any{"status": status, "completed_at": now.UTC(), "updated_at": now.UTC()})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		var existing models.TrelloWebhookReceipt
		if err := r.DB.Select("status").Where("id = ? AND source_id = ?", receiptID, sourceID).First(&existing).Error; err != nil {
			return err
		}
		if existing.Status == status {
			return nil
		}
		return errors.New("Trello webhook receipt status changed before completion")
	}
	return nil
}
