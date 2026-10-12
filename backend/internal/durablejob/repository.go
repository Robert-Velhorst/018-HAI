// Package durablejob provides the durable worker model: background jobs that
// are persisted, scheduled, retried with backoff, and recovered when the worker
// process dies. It replaces "in-process only" scheduling for work that must not
// be lost across a restart.
package durablejob

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"sort"
	"time"

	"automation-hub-backend/internal/infra"
	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/safety"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Repository is the persistence seam. The Gorm implementation below is used in
// production; tests use an in-memory fake so retry/lease logic is verifiable
// without a database.
type Repository interface {
	Enqueue(job *models.DurableJob) (*models.DurableJob, error)
	// EnqueueIfNoActive atomically creates a job only when no pending or
	// running, review-held or settling job of the same kind exists.
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
	// CompleteRecurring terminalizes the owned current occurrence and creates
	// its replacement occurrence in one transaction. It returns whether the
	// current lease was still owned and whether it created the replacement.
	CompleteRecurring(id uuid.UUID, workerID string, leaseGeneration int64, now time.Time, terminalStatus string, attempts int, lastErr string, next *models.DurableJob) (bool, bool, error)
	// ExtendLease heartbeats a running job only while the caller still owns its
	// current lease generation.
	ExtendLease(id uuid.UUID, workerID string, leaseGeneration int64, now time.Time) (bool, error)
	// ReapExpiredLeases retries replay-safe work and holds uncertain work for review.
	ReapExpiredLeases(now time.Time, lease time.Duration) (int, error)
	Find(id uuid.UUID) (*models.DurableJob, error)
	// FindLatestDeadByPayload returns the most recently exhausted job for one
	// stable work item, allowing recurring scanners to honour a cooldown before
	// creating a fresh retry cycle.
	FindLatestDeadByPayload(queue, kind, payload string) (*models.DurableJob, error)
	// CountActiveByKind includes pending/running and review/preparation holds.
	// Used to keep recurring work singleton across restarts.
	CountActiveByKind(kind string) (int64, error)
}

// AttemptAwareSuccessRepository persists the invocation count when a job
// succeeds. It is an optional extension so existing Repository implementations
// remain source-compatible.
type AttemptAwareSuccessRepository interface {
	MarkSucceededWithAttempts(id uuid.UUID, workerID string, leaseGeneration int64, now time.Time, attempts int) (bool, error)
}

// ManualReviewRepository holds an owned occurrence without making it claimable
// or creating a successor. Existing adapters must explicitly implement it.
type ManualReviewRepository interface {
	MarkForReview(context.Context, uuid.UUID, string, int64, time.Time, int, string) (bool, error)
}

// ReviewSchedulingRepository persists the policy before exposing a recurring
// handler, and upgrades existing active occurrences under the singleton lock.
type ReviewSchedulingRepository interface {
	EnqueueReviewIfNoActive(*models.DurableJob) (bool, error)
}

type ReviewRecurringRepository interface {
	// CompleteReviewRecurring requires confirmed preparation before releasing
	// a review-sensitive successor. It never clears uncertainty automatically.
	CompleteReviewRecurring(context.Context, uuid.UUID, string, int64, time.Time, string, int, string, *models.DurableJob) (bool, bool, error)
}

func activeJobStatuses() []string {
	return []string{models.DurableJobPending, models.DurableJobRunning, models.DurableJobNeedsReview, models.DurableJobSettling}
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
	if job.ReplayPolicy == "" {
		job.ReplayPolicy = models.DurableJobReplayAtLeastOnce
	}
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
	return r.enqueueIfNoActive(job, false, false)
}

func (r *gormRepository) EnqueueIfNoActiveMatchingPayload(job *models.DurableJob) (bool, error) {
	return r.enqueueIfNoActive(job, true, false)
}

func (r *gormRepository) EnqueueReviewIfNoActive(job *models.DurableJob) (bool, error) {
	if !r.reviewStoreAvailable() || job == nil || job.ReplayPolicy != models.DurableJobReviewUnknown {
		return false, ErrReviewPersistenceUnavailable
	}
	return r.enqueueIfNoActive(job, false, true)
}

func (r *gormRepository) reviewStoreAvailable() bool {
	if r == nil || r.db == nil || r.db.Config == nil || r.db.Statement == nil || r.db.Statement.ConnPool == nil ||
		r.db.DryRun || r.db.Dialector == nil || r.db.Dialector.Name() != "postgres" {
		return false
	}
	_, transaction := r.db.Statement.ConnPool.(gorm.TxCommitter)
	return !transaction
}

func (r *gormRepository) enqueueIfNoActive(job *models.DurableJob, matchPayload, reviewPolicy bool) (bool, error) {
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
		if reviewPolicy {
			if err := tx.Model(&models.DurableJob{}).
				Where("queue = ? AND kind = ? AND status IN ?", job.Queue, job.Kind, activeJobStatuses()).
				Update("replay_policy", models.DurableJobReviewUnknown).Error; err != nil {
				return err
			}
		}
		query := tx.Model(&models.DurableJob{}).
			Where("queue = ? AND kind = ? AND status IN ?", job.Queue, job.Kind, activeJobStatuses())
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
			SELECT candidate.* FROM durable_jobs AS candidate
			WHERE candidate.status = ? AND candidate.queue = ? AND candidate.run_at <= ?
			AND NOT EXISTS (
				SELECT 1 FROM durable_jobs AS held
				WHERE held.queue = candidate.queue AND held.kind = candidate.kind
				AND held.status IN ?
			)
			ORDER BY candidate.run_at
			LIMIT ?
			FOR UPDATE SKIP LOCKED`,
			models.DurableJobPending, queue, now, []string{models.DurableJobNeedsReview, models.DurableJobSettling}, limit).Scan(&candidates).Error; err != nil {
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
	return r.markSucceeded(id, workerID, leaseGeneration, now, nil)
}

func (r *gormRepository) MarkSucceededWithAttempts(id uuid.UUID, workerID string, leaseGeneration int64, now time.Time, attempts int) (bool, error) {
	return r.markSucceeded(id, workerID, leaseGeneration, now, &attempts)
}

func (r *gormRepository) markSucceeded(id uuid.UUID, workerID string, leaseGeneration int64, now time.Time, attempts *int) (bool, error) {
	updates := map[string]any{
		"status":       models.DurableJobSucceeded,
		"completed_at": now,
		"locked_by":    "",
		"locked_at":    nil,
		"last_error":   "",
	}
	if attempts != nil {
		updates["attempts"] = *attempts
	}
	result := r.ownedLease(id, workerID, leaseGeneration).Updates(updates)
	return result.RowsAffected == 1, result.Error
}

func (r *gormRepository) MarkForRetry(id uuid.UUID, workerID string, leaseGeneration int64, runAt time.Time, attempts int, lastErr string) (bool, error) {
	result := r.ownedLease(id, workerID, leaseGeneration).Updates(map[string]any{
		"status":     models.DurableJobPending,
		"run_at":     runAt,
		"attempts":   attempts,
		"last_error": safety.RedactSecrets(lastErr),
		"locked_by":  "",
		"locked_at":  nil,
	})
	return result.RowsAffected == 1, result.Error
}

func (r *gormRepository) MarkDeferred(id uuid.UUID, workerID string, leaseGeneration int64, runAt time.Time, reason string) (bool, error) {
	result := r.ownedLease(id, workerID, leaseGeneration).Updates(map[string]any{
		"status":     models.DurableJobPending,
		"run_at":     runAt,
		"last_error": safety.RedactSecrets(reason),
		"locked_by":  "",
		"locked_at":  nil,
	})
	return result.RowsAffected == 1, result.Error
}

func (r *gormRepository) MarkDead(id uuid.UUID, workerID string, leaseGeneration int64, now time.Time, attempts int, lastErr string) (bool, error) {
	result := r.ownedLease(id, workerID, leaseGeneration).Updates(map[string]any{
		"status":       models.DurableJobDead,
		"attempts":     attempts,
		"last_error":   safety.RedactSecrets(lastErr),
		"completed_at": now,
		"locked_by":    "",
		"locked_at":    nil,
	})
	return result.RowsAffected == 1, result.Error
}

func (r *gormRepository) MarkForReview(ctx context.Context, id uuid.UUID, workerID string, generation int64, now time.Time, attempts int, reason string) (bool, error) {
	if ctx == nil || !r.reviewStoreAvailable() ||
		id == uuid.Nil || workerID == "" || generation <= 0 {
		return false, ErrReviewPersistenceUnavailable
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	result := r.db.WithContext(ctx).Model(&models.DurableJob{}).
		Where("id = ? AND status = ? AND locked_by = ? AND lease_generation = ?", id, models.DurableJobRunning, workerID, generation).
		Updates(map[string]any{
			"status": models.DurableJobNeedsReview, "attempts": attempts,
			"last_error": safety.RedactSecrets(reason), "completed_at": nil,
			"locked_by": "", "locked_at": nil, "updated_at": now.UTC(),
		})
	return result.RowsAffected == 1, result.Error
}

func (r *gormRepository) CompleteRecurring(id uuid.UUID, workerID string, leaseGeneration int64, now time.Time, terminalStatus string, attempts int, lastErr string, next *models.DurableJob) (bool, bool, error) {
	if terminalStatus != models.DurableJobSucceeded && terminalStatus != models.DurableJobDead {
		return false, false, fmt.Errorf("recurring terminal status %q is invalid", terminalStatus)
	}
	if next == nil {
		return false, false, fmt.Errorf("recurring replacement job is required")
	}
	if next.ReplayPolicy == models.DurableJobReviewUnknown {
		return false, false, ErrReviewPersistenceUnavailable
	}
	normalizeJob(next)
	owned, scheduled := false, false
	err := r.db.Transaction(func(tx *gorm.DB) error {
		lockMaterial := next.Queue + "\x00" + next.Kind
		lockKey := fmt.Sprintf("%x", sha256.Sum256([]byte(lockMaterial)))
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", lockKey).Error; err != nil {
			return err
		}

		updates := map[string]any{
			"status":       terminalStatus,
			"attempts":     attempts,
			"completed_at": now.UTC(),
			"locked_by":    "",
			"locked_at":    nil,
			"last_error":   safety.RedactSecrets(lastErr),
		}
		// A stale caller must not settle a newly upgraded review-policy row
		// through the ordinary completion path or replace another queue/kind.
		result := tx.Model(&models.DurableJob{}).
			Where("id = ? AND status = ? AND locked_by = ? AND lease_generation = ? AND queue = ? AND kind = ? AND replay_policy = ?",
				id, models.DurableJobRunning, workerID, leaseGeneration, next.Queue, next.Kind, models.DurableJobReplayAtLeastOnce).
			Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return nil
		}
		owned = true

		var active int64
		if err := tx.Model(&models.DurableJob{}).
			Where("queue = ? AND kind = ? AND id <> ? AND status IN ?", next.Queue, next.Kind, id, activeJobStatuses()).
			Count(&active).Error; err != nil {
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
	if err != nil {
		return false, false, err
	}
	return owned, scheduled, nil
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
	return r.reapExpiredLeases("", now, lease, nil)
}

// ReapExpiredLeasesForQueue limits recovery to the queue whose lease duration
// the caller is configured to enforce. Runners may use different lease lengths.
func (r *gormRepository) ReapExpiredLeasesForQueue(queue string, now time.Time, lease time.Duration) (int, error) {
	if queue == "" {
		queue = "default"
	}
	return r.reapExpiredLeases(queue, now, lease, nil)
}

// ReapExpiredLeasesWithRecurring commits exhausted occurrences and their next
// schedules together. The singleton lock matches CompleteRecurring and startup
// scheduling, so concurrent recovery cannot create duplicate successors.
func (r *gormRepository) ReapExpiredLeasesWithRecurring(queue string, now time.Time, lease time.Duration, schedules map[string]recurringSchedule) (int, error) {
	if queue == "" {
		queue = "default"
	}
	return r.reapExpiredLeases(queue, now, lease, schedules)
}

func (r *gormRepository) reapExpiredLeases(queue string, now time.Time, lease time.Duration, schedules map[string]recurringSchedule) (int, error) {
	cutoff := now.Add(-lease)
	const recoveryReason = "worker lease is missing or expired; execution outcome is unknown"
	const exhaustedReason = "worker lease is missing or expired on the final allowed attempt; execution outcome is unknown"
	reaped := int64(0)
	err := r.db.Transaction(func(tx *gorm.DB) error {
		kinds := make([]string, 0, len(schedules))
		for kind := range schedules {
			kinds = append(kinds, kind)
		}
		sort.Strings(kinds)
		for _, kind := range kinds {
			lockKey := fmt.Sprintf("%x", sha256.Sum256([]byte(queue+"\x00"+kind)))
			if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", lockKey).Error; err != nil {
				return err
			}
		}
		expired := func() *gorm.DB {
			query := tx.Model(&models.DurableJob{}).
				Where("status = ? AND (locked_at IS NULL OR locked_at < ?)", models.DurableJobRunning, cutoff)
			if queue != "" {
				query = query.Where("queue = ?", queue)
			}
			return query
		}
		// Only explicitly replay-safe jobs may return to the runnable queue.
		// Persisted policy makes this safe even without a registered handler or
		// after a failed review write/process crash. Unknown policies fail closed.
		held := expired().Where("replay_policy <> ? OR replay_policy IS NULL", models.DurableJobReplayAtLeastOnce).Updates(map[string]any{
			"status": models.DurableJobNeedsReview, "attempts": gorm.Expr("attempts + 1"),
			"last_error": recoveryReason, "completed_at": nil, "locked_by": "", "locked_at": nil,
		})
		if held.Error != nil {
			return held.Error
		}
		reaped += held.RowsAffected
		pending := expired().Where("replay_policy = ? AND attempts + 1 < max_attempts", models.DurableJobReplayAtLeastOnce).Updates(map[string]any{
			"status":     models.DurableJobPending,
			"attempts":   gorm.Expr("attempts + 1"),
			"last_error": recoveryReason,
			"locked_by":  "",
			"locked_at":  nil,
		})
		if pending.Error != nil {
			return pending.Error
		}
		reaped += pending.RowsAffected
		var exhausted []models.DurableJob
		dead := expired().Model(&exhausted).Clauses(clause.Returning{}).Where("replay_policy = ? AND attempts + 1 >= max_attempts", models.DurableJobReplayAtLeastOnce).Updates(map[string]any{
			"status":       models.DurableJobDead,
			"attempts":     gorm.Expr("attempts + 1"),
			"last_error":   exhaustedReason,
			"completed_at": now.UTC(),
			"locked_by":    "",
			"locked_at":    nil,
		})
		if dead.Error != nil {
			return dead.Error
		}
		reaped += dead.RowsAffected
		for _, job := range exhausted {
			schedule, recurring := schedules[job.Kind]
			if !recurring {
				continue
			}
			var active int64
			if err := tx.Model(&models.DurableJob{}).Where("queue = ? AND kind = ? AND status IN ?", queue, job.Kind, activeJobStatuses()).Count(&active).Error; err != nil {
				return err
			}
			if active == 0 {
				next := &models.DurableJob{Queue: queue, Kind: job.Kind, Payload: schedule.payload, RunAt: now.UTC().Add(schedule.interval), MaxAttempts: schedule.maxAttempts, ReplayPolicy: schedule.replayPolicy}
				normalizeJob(next)
				if err := tx.Create(next).Error; err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return int(reaped), err
}

func (r *gormRepository) CountActiveByKind(kind string) (int64, error) {
	var count int64
	err := r.db.Model(&models.DurableJob{}).
		Where("kind = ? AND status IN ?", kind, activeJobStatuses()).
		Count(&count).Error
	return count, err
}

func (r *gormRepository) FindLatestDeadByPayload(queue, kind, payload string) (*models.DurableJob, error) {
	var job models.DurableJob
	err := r.db.Where("queue = ? AND kind = ? AND payload = ? AND status = ?", queue, kind, payload, models.DurableJobDead).
		Order("completed_at DESC, created_at DESC").First(&job).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &job, nil
}

func (r *gormRepository) Find(id uuid.UUID) (*models.DurableJob, error) {
	var job models.DurableJob
	if err := r.db.First(&job, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &job, nil
}
