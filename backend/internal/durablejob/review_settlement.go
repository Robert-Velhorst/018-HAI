package durablejob

import (
	"context"
	"crypto/sha256"
	"fmt"
	"time"

	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/safety"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Preparation is committed while the original occurrence is a claim barrier.
// Only an acknowledged preparation permits release in a second transaction.
// A lost first commit acknowledgement cannot expose its pending successor.
// A lost release acknowledgement still requires same-record reconciliation;
// the database may have committed the release based on confirmed preparation.
func (r *gormRepository) CompleteReviewRecurring(ctx context.Context, id uuid.UUID, worker string, generation int64, now time.Time, terminal string, attempts int, lastErr string, next *models.DurableJob) (bool, bool, error) {
	if ctx == nil || !r.reviewStoreAvailable() || id == uuid.Nil || worker == "" || generation <= 0 || attempts <= 0 ||
		next == nil || next.ReplayPolicy != models.DurableJobReviewUnknown ||
		(terminal != models.DurableJobSucceeded && terminal != models.DurableJobDead) ||
		(next.Status != "" && next.Status != models.DurableJobPending) || next.Attempts != 0 || next.LeaseGeneration != 0 || next.LockedBy != "" || next.LockedAt != nil || next.CompletedAt != nil {
		return false, false, ErrReviewPersistenceUnavailable
	}
	if err := ctx.Err(); err != nil {
		return false, false, err
	}
	normalizeJob(next)
	if next.ID == uuid.Nil {
		next.ID = uuid.New()
	}
	if next.ID == id {
		return false, false, ErrReviewPersistenceUnavailable
	}
	stamp := now.UTC().Truncate(time.Microsecond)
	safeLastErr := safety.RedactSecrets(lastErr)
	reason := "terminal outcome awaiting confirmation: " + terminal
	if safeLastErr != "" {
		reason += "; " + safeLastErr
	}
	lockKey := fmt.Sprintf("%x", sha256.Sum256([]byte(next.Queue+"\x00"+next.Kind)))
	db := r.db.WithContext(ctx)
	prepared, scheduled := false, false
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", lockKey).Error; err != nil {
			return err
		}
		result := tx.Model(&models.DurableJob{}).
			Where("id = ? AND status = ? AND locked_by = ? AND lease_generation = ? AND queue = ? AND kind = ? AND replay_policy = ?",
				id, models.DurableJobRunning, worker, generation, next.Queue, next.Kind, models.DurableJobReviewUnknown).
			Updates(map[string]any{"status": models.DurableJobSettling, "attempts": attempts, "last_error": reason, "completed_at": nil, "updated_at": stamp})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrJobSettlementUnconfirmed
		}
		prepared = true
		var active int64
		if err := tx.Model(&models.DurableJob{}).Where("queue = ? AND kind = ? AND id <> ? AND status IN ?", next.Queue, next.Kind, id, activeJobStatuses()).Count(&active).Error; err != nil {
			return err
		}
		if active > 0 {
			return nil
		}
		if err := tx.Create(next).Error; err != nil {
			return err
		}
		scheduled = true
		return nil
	})
	if err != nil || !prepared {
		return false, false, err
	}
	// Cancellation after preparation retains the barrier; it never guesses
	// that preparation failed or enables its successor to make progress.
	if err := ctx.Err(); err != nil {
		return false, false, err
	}
	err = db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", lockKey).Error; err != nil {
			return err
		}
		result := tx.Model(&models.DurableJob{}).
			Where("id = ? AND status = ? AND locked_by = ? AND lease_generation = ? AND queue = ? AND kind = ? AND replay_policy = ? AND attempts = ? AND updated_at = ? AND last_error = ? AND completed_at IS NULL",
				id, models.DurableJobSettling, worker, generation, next.Queue, next.Kind, models.DurableJobReviewUnknown, attempts, stamp, reason).
			Updates(map[string]any{"status": terminal, "completed_at": stamp, "updated_at": stamp, "last_error": safeLastErr, "locked_by": "", "locked_at": nil})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrJobSettlementUnconfirmed
		}
		return nil
	})
	if err != nil {
		return false, false, err
	}
	return true, scheduled, nil
}
