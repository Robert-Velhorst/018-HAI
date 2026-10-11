// Package durablejob provides the durable worker model: background jobs that
// are persisted, scheduled, retried with backoff, and recovered when the worker
// process dies. It replaces "in-process only" scheduling for work that must not
// be lost across a restart.
package durablejob

import (
	"crypto/sha256"
	"fmt"
	"time"

	"automation-hub-backend/internal/infra"
	"automation-hub-backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Repository is the persistence seam. The Gorm implementation below is used in
// production; tests use an in-memory fake so retry/lease logic is verifiable
// without a database.
type Repository interface {
	Enqueue(job *models.DurableJob) (*models.DurableJob, error)
	// EnqueueIfNoActive atomically creates a job only when no pending or
	// running job of the same kind exists.
	EnqueueIfNoActive(job *models.DurableJob) (bool, error)
	// EnqueueIfNoActiveMatchingPayload atomically creates a job only when no
	// pending or running job has the same queue, kind, and payload. It keeps
	// independent source jobs concurrent while preventing a periodic scanner
	// from repeatedly queuing the same source during a slow retry.
	EnqueueIfNoActiveMatchingPayload(job *models.DurableJob) (bool, error)
	// ClaimDue atomically leases up to limit jobs that are due at now.
	ClaimDue(workerID, queue string, now time.Time, limit int) ([]models.DurableJob, error)
	MarkSucceeded(id uuid.UUID, workerID string, leaseGeneration int64, now time.Time) (bool, error)
	// MarkForRetry returns a job to pending with a future RunAt.
	MarkForRetry(id uuid.UUID, workerID string, leaseGeneration int64, runAt time.Time, attempts int, lastErr string) (bool, error)
	// MarkDeferred returns a deliberately paused job to pending without
	// incrementing attempts. Policy gates use it to preserve work for resume.
	MarkDeferred(id uuid.UUID, workerID string, leaseGeneration int64, runAt time.Time, reason string) (bool, error)
	MarkDead(id uuid.UUID, workerID string, leaseGeneration int64, now time.Time, attempts int, lastErr string) (bool, error)
	// ExtendLease heartbeats a running job only while the caller still owns its
	// current lease generation.
	ExtendLease(id uuid.UUID, workerID string, leaseGeneration int64, now time.Time) (bool, error)
	// ReapExpiredLeases returns jobs whose worker died back to pending.
	ReapExpiredLeases(now time.Time, lease time.Duration) (int, error)
	Find(id uuid.UUID) (*models.DurableJob, error)
	// CountActiveByKind counts jobs of a kind that are still pending or running.
	// Used to keep recurring work singleton across restarts.
	CountActiveByKind(kind string) (int64, error)
}

type gormRepository struct{ db *gorm.DB }

// NewGormRepository returns the Postgres-backed repository.
func NewGormRepository(db *gorm.DB) Repository { return &gormRepository{db: db} }

// DefaultRepository builds the repository over the default database.
func DefaultRepository() (Repository, error) {
	db, err := infra.GetDefaultDB()
	if err != nil {
		return nil, err
	}
	return NewGormRepository(db), nil
}

func (r *gormRepository) Enqueue(job *models.DurableJob) (*models.DurableJob, error) {
	normalizeJob(job)
	if err := r.db.Create(job).Error; err != nil {
		return nil, err
	}
	return job, nil
}

func normalizeJob(job *models.DurableJob) {
	if job.Queue == "" {
		job.Queue = "default"
	}
	if job.Status == "" {
		job.Status = models.DurableJobPending
	}
	if job.MaxAttempts <= 0 {
		job.MaxAttempts = 5
	}
	if job.RunAt.IsZero() {
		job.RunAt = time.Now().UTC()
	}
	if job.Payload == "" {
		job.Payload = "{}"
	}
}

func (r *gormRepository) EnqueueIfNoActive(job *models.DurableJob) (bool, error) {
	return r.enqueueIfNoActive(job, false)
}

func (r *gormRepository) EnqueueIfNoActiveMatchingPayload(job *models.DurableJob) (bool, error) {
	return r.enqueueIfNoActive(job, true)
}

func (r *gormRepository) enqueueIfNoActive(job *models.DurableJob, matchPayload bool) (bool, error) {
	normalizeJob(job)
	created := false
	err := r.db.Transaction(func(tx *gorm.DB) error {
		// Serialize active-job creation without holding a table lock. PostgreSQL
		// text values cannot contain NUL bytes, so use a fixed-width digest
		// instead of an in-memory composite-key separator.
		lockMaterial := job.Queue + "\x00" + job.Kind
		if matchPayload {
			lockMaterial += "\x00" + job.Payload
		}
		lockKey := fmt.Sprintf("%x", sha256.Sum256([]byte(lockMaterial)))
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", lockKey).Error; err != nil {
			return err
		}
		query := tx.Model(&models.DurableJob{}).
			Where("queue = ? AND kind = ? AND status IN ?", job.Queue, job.Kind, []string{models.DurableJobPending, models.DurableJobRunning})
		if matchPayload {
			query = query.Where("payload = ?", job.Payload)
		}
		var count int64
		if err := query.Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return nil
		}
		if err := tx.Create(job).Error; err != nil {
			return err
		}
		created = true
		return nil
	})
	return created, err
}

// ClaimDue uses SELECT ... FOR UPDATE SKIP LOCKED so multiple workers can poll
// the same queue concurrently without ever claiming the same job twice.
func (r *gormRepository) ClaimDue(workerID, queue string, now time.Time, limit int) ([]models.DurableJob, error) {
	if limit <= 0 {
		limit = 10
	}
	if queue == "" {
		queue = "default"
	}
	claimed := []models.DurableJob{}
	err := r.db.Transaction(func(tx *gorm.DB) error {
		var candidates []models.DurableJob
		if err := tx.Raw(`
			SELECT * FROM durable_jobs
			WHERE status = ? AND queue = ? AND run_at <= ?
			ORDER BY run_at
			LIMIT ?
			FOR UPDATE SKIP LOCKED`,
			models.DurableJobPending, queue, now, limit).Scan(&candidates).Error; err != nil {
			return err
		}
		for i := range candidates {
			if err := tx.Model(&models.DurableJob{}).
				Where("id = ?", candidates[i].ID).
				Updates(map[string]any{
					"status":           models.DurableJobRunning,
					"locked_by":        workerID,
					"locked_at":        now,
					"lease_generation": gorm.Expr("lease_generation + 1"),
				}).Error; err != nil {
				return err
			}
			candidates[i].Status = models.DurableJobRunning
			candidates[i].LockedBy = workerID
			lockedAt := now
			candidates[i].LockedAt = &lockedAt
			candidates[i].LeaseGeneration++
		}
		claimed = candidates
		return nil
	})
	return claimed, err
}

func (r *gormRepository) MarkSucceeded(id uuid.UUID, workerID string, leaseGeneration int64, now time.Time) (bool, error) {
	result := r.ownedLease(id, workerID, leaseGeneration).Updates(map[string]any{
		"status":       models.DurableJobSucceeded,
		"completed_at": now,
		"locked_by":    "",
		"locked_at":    nil,
		"last_error":   "",
	})
	return result.RowsAffected == 1, result.Error
}

func (r *gormRepository) MarkForRetry(id uuid.UUID, workerID string, leaseGeneration int64, runAt time.Time, attempts int, lastErr string) (bool, error) {
	result := r.ownedLease(id, workerID, leaseGeneration).Updates(map[string]any{
		"status":     models.DurableJobPending,
		"run_at":     runAt,
		"attempts":   attempts,
		"last_error": lastErr,
		"locked_by":  "",
		"locked_at":  nil,
	})
	return result.RowsAffected == 1, result.Error
}

func (r *gormRepository) MarkDeferred(id uuid.UUID, workerID string, leaseGeneration int64, runAt time.Time, reason string) (bool, error) {
	result := r.ownedLease(id, workerID, leaseGeneration).Updates(map[string]any{
		"status":     models.DurableJobPending,
		"run_at":     runAt,
		"last_error": reason,
		"locked_by":  "",
		"locked_at":  nil,
	})
	return result.RowsAffected == 1, result.Error
}

func (r *gormRepository) MarkDead(id uuid.UUID, workerID string, leaseGeneration int64, now time.Time, attempts int, lastErr string) (bool, error) {
	result := r.ownedLease(id, workerID, leaseGeneration).Updates(map[string]any{
		"status":       models.DurableJobDead,
		"attempts":     attempts,
		"last_error":   lastErr,
		"completed_at": now,
		"locked_by":    "",
		"locked_at":    nil,
	})
	return result.RowsAffected == 1, result.Error
}

func (r *gormRepository) ExtendLease(id uuid.UUID, workerID string, leaseGeneration int64, now time.Time) (bool, error) {
	result := r.ownedLease(id, workerID, leaseGeneration).Update("locked_at", now)
	return result.RowsAffected == 1, result.Error
}

func (r *gormRepository) ownedLease(id uuid.UUID, workerID string, leaseGeneration int64) *gorm.DB {
	return r.db.Model(&models.DurableJob{}).
		Where(
			"id = ? AND status = ? AND locked_by = ? AND lease_generation = ?",
			id,
			models.DurableJobRunning,
			workerID,
			leaseGeneration,
		)
}

// ReapExpiredLeases recovers jobs whose worker died while holding the lease.
func (r *gormRepository) ReapExpiredLeases(now time.Time, lease time.Duration) (int, error) {
	cutoff := now.Add(-lease)
	result := r.db.Model(&models.DurableJob{}).
		Where("status = ? AND locked_at IS NOT NULL AND locked_at < ?", models.DurableJobRunning, cutoff).
		Updates(map[string]any{
			"status":    models.DurableJobPending,
			"locked_by": "",
			"locked_at": nil,
		})
	return int(result.RowsAffected), result.Error
}

func (r *gormRepository) CountActiveByKind(kind string) (int64, error) {
	var count int64
	err := r.db.Model(&models.DurableJob{}).
		Where("kind = ? AND status IN ?", kind, []string{models.DurableJobPending, models.DurableJobRunning}).
		Count(&count).Error
	return count, err
}

func (r *gormRepository) Find(id uuid.UUID) (*models.DurableJob, error) {
	var job models.DurableJob
	if err := r.db.First(&job, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &job, nil
}
