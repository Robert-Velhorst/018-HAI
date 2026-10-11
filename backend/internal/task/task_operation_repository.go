package task

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"time"

	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/safety"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const taskOperationMaximumErrorRunes = 1024

func normalizedTaskOperationContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func taskOperationMapKey(ownerIdentity, idempotencyKey string) string {
	return ownerIdentity + "\x00" + idempotencyKey
}

func normalizeTaskOperationIdentity(ownerIdentity, idempotencyKey, requestDigest, mode, leaseOwner string) (string, string, string, string, string, error) {
	ownerIdentity, err := normalizeTaskStateOwner(ownerIdentity)
	if err != nil {
		return "", "", "", "", "", err
	}
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if !validTaskOperationIdentifier(idempotencyKey, 120) {
		return "", "", "", "", "", fmt.Errorf("idempotency key must contain 1 to 120 safe identifier characters")
	}
	requestDigest = strings.ToLower(strings.TrimSpace(requestDigest))
	if len(requestDigest) != 64 || !allLowerHex(requestDigest) {
		return "", "", "", "", "", fmt.Errorf("task operation request digest is invalid")
	}
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode != "plan" && mode != "run" {
		return "", "", "", "", "", fmt.Errorf("task operation mode must be plan or run")
	}
	leaseOwner = strings.TrimSpace(leaseOwner)
	if !validTaskOperationIdentifier(leaseOwner, 120) {
		return "", "", "", "", "", fmt.Errorf("task operation lease owner is invalid")
	}
	return ownerIdentity, idempotencyKey, requestDigest, mode, leaseOwner, nil
}

func validTaskOperationIdentifier(value string, maximum int) bool {
	if value == "" || len([]rune(value)) > maximum {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || strings.ContainsRune("._:-", r) {
			continue
		}
		return false
	}
	return true
}

func allLowerHex(value string) bool {
	for _, r := range value {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

func normalizedTaskOperationTime(now time.Time) time.Time {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	return normalizeTaskStateTimestamp(now)
}

func taskOperationReviewReason(reason string) string {
	reason = strings.Join(strings.Fields(strings.TrimSpace(safety.RedactSecrets(reason))), " ")
	if reason == "" {
		reason = "task operation outcome could not be proven"
	}
	if len([]rune(reason)) > taskOperationMaximumErrorRunes {
		reason = string([]rune(reason)[:taskOperationMaximumErrorRunes])
	}
	return reason
}

func classifyExistingTaskOperation(row models.TaskOperationRecord, requestDigest, mode string, now time.Time, leaseDuration time.Duration) (TaskOperationClaim, bool, error) {
	if row.RequestDigest != requestDigest || row.Mode != mode {
		return TaskOperationClaim{}, false, ErrTaskStateConflict
	}
	switch row.Status {
	case "completed":
		return TaskOperationClaim{Operation: row, Disposition: TaskOperationReplay}, false, nil
	case "needs_review":
		return TaskOperationClaim{Operation: row, Disposition: TaskOperationNeedsReview}, false, nil
	case "canceled":
		return TaskOperationClaim{Operation: row, Disposition: TaskOperationCanceled}, false, nil
	case "running":
		if leaseDuration <= 0 {
			leaseDuration = 2 * time.Minute
		}
		if row.LeasedAt != nil && !row.LeasedAt.Before(now.Add(-leaseDuration)) {
			return TaskOperationClaim{Operation: row, Disposition: TaskOperationInProgress}, false, nil
		}
		return TaskOperationClaim{Operation: row, Disposition: TaskOperationNeedsReview}, true, nil
	default:
		return TaskOperationClaim{}, false, ErrTaskStateConflict
	}
}

func (r *MemoryTaskStateRepository) ClaimTaskOperation(ownerIdentity, idempotencyKey, requestDigest, mode, leaseOwner string, now time.Time, leaseDuration time.Duration) (TaskOperationClaim, error) {
	return r.ClaimTaskOperationContext(context.Background(), ownerIdentity, idempotencyKey, requestDigest, mode, leaseOwner, now, leaseDuration)
}

func (r *MemoryTaskStateRepository) ClaimTaskOperationContext(ctx context.Context, ownerIdentity, idempotencyKey, requestDigest, mode, leaseOwner string, now time.Time, leaseDuration time.Duration) (TaskOperationClaim, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return TaskOperationClaim{}, err
	}
	ownerIdentity, idempotencyKey, requestDigest, mode, leaseOwner, err := normalizeTaskOperationIdentity(ownerIdentity, idempotencyKey, requestDigest, mode, leaseOwner)
	if err != nil {
		return TaskOperationClaim{}, err
	}
	now = normalizedTaskOperationTime(now)
	key := taskOperationMapKey(ownerIdentity, idempotencyKey)
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return TaskOperationClaim{}, err
	}
	r.ensureInitialized()
	if existing, ok := r.operations[key]; ok {
		claim, expired, classifyErr := classifyExistingTaskOperation(existing, requestDigest, mode, now, leaseDuration)
		if classifyErr != nil {
			return TaskOperationClaim{}, classifyErr
		}
		if expired {
			existing.Status = "needs_review"
			existing.LastError = taskOperationReviewReason("prior task worker lease expired; execution outcome is unknown")
			existing.LeaseOwner = ""
			existing.LeasedAt = nil
			existing.UpdatedAt = now
			r.operations[key] = existing
			claim.Operation = existing
		}
		return claim, nil
	}
	leasedAt := now
	row := models.TaskOperationRecord{
		ID: uuid.New(), OwnerIdentity: ownerIdentity, IdempotencyKey: idempotencyKey,
		RequestDigest: requestDigest, Mode: mode, Status: "running", LeaseOwner: leaseOwner,
		LeaseGeneration: 1, LeasedAt: &leasedAt, CreatedAt: now, UpdatedAt: now,
	}
	r.operations[key] = row
	return TaskOperationClaim{Operation: row, Disposition: TaskOperationAcquired}, nil
}

func (r *MemoryTaskStateRepository) HeartbeatTaskOperation(ownerIdentity string, operationID uuid.UUID, leaseOwner string, leaseGeneration int64, now time.Time) (bool, error) {
	return r.HeartbeatTaskOperationContext(context.Background(), ownerIdentity, operationID, leaseOwner, leaseGeneration, now)
}

func (r *MemoryTaskStateRepository) HeartbeatTaskOperationContext(ctx context.Context, ownerIdentity string, operationID uuid.UUID, leaseOwner string, leaseGeneration int64, now time.Time) (bool, error) {
	return r.updateOwnedTaskOperationContext(ctx, ownerIdentity, operationID, leaseOwner, leaseGeneration, func(row *models.TaskOperationRecord) {
		now = normalizedTaskOperationTime(now)
		row.LeasedAt = &now
		row.UpdatedAt = now
	})
}

func (r *MemoryTaskStateRepository) CompleteTaskOperation(ownerIdentity string, operationID uuid.UUID, leaseOwner string, leaseGeneration int64, taskPlanID string, now time.Time) (bool, error) {
	return r.CompleteTaskOperationContext(context.Background(), ownerIdentity, operationID, leaseOwner, leaseGeneration, taskPlanID, now)
}

func (r *MemoryTaskStateRepository) CompleteTaskOperationContext(ctx context.Context, ownerIdentity string, operationID uuid.UUID, leaseOwner string, leaseGeneration int64, taskPlanID string, now time.Time) (bool, error) {
	taskPlanID = strings.TrimSpace(taskPlanID)
	if taskPlanID == "" || len([]rune(taskPlanID)) > 160 {
		return false, fmt.Errorf("task plan id must contain 1 to 160 characters")
	}
	return r.updateOwnedTaskOperationContext(ctx, ownerIdentity, operationID, leaseOwner, leaseGeneration, func(row *models.TaskOperationRecord) {
		now = normalizedTaskOperationTime(now)
		row.Status = "completed"
		row.TaskPlanID = taskPlanID
		row.LeaseOwner = ""
		row.LeasedAt = nil
		row.LastError = ""
		row.CompletedAt = &now
		row.UpdatedAt = now
	})
}

func (r *MemoryTaskStateRepository) MarkTaskOperationNeedsReview(ownerIdentity string, operationID uuid.UUID, leaseOwner string, leaseGeneration int64, reason string, now time.Time) (bool, error) {
	return r.MarkTaskOperationNeedsReviewContext(context.Background(), ownerIdentity, operationID, leaseOwner, leaseGeneration, reason, now)
}

func (r *MemoryTaskStateRepository) MarkTaskOperationNeedsReviewContext(ctx context.Context, ownerIdentity string, operationID uuid.UUID, leaseOwner string, leaseGeneration int64, reason string, now time.Time) (bool, error) {
	return r.updateOwnedTaskOperationContext(ctx, ownerIdentity, operationID, leaseOwner, leaseGeneration, func(row *models.TaskOperationRecord) {
		now = normalizedTaskOperationTime(now)
		row.Status = "needs_review"
		row.LeaseOwner = ""
		row.LeasedAt = nil
		row.LastError = taskOperationReviewReason(reason)
		row.UpdatedAt = now
	})
}

func (r *MemoryTaskStateRepository) CancelTaskOperation(ownerIdentity string, operationID uuid.UUID, leaseOwner string, leaseGeneration int64, reason string, now time.Time) (bool, error) {
	return r.CancelTaskOperationContext(context.Background(), ownerIdentity, operationID, leaseOwner, leaseGeneration, reason, now)
}

func (r *MemoryTaskStateRepository) CancelTaskOperationContext(ctx context.Context, ownerIdentity string, operationID uuid.UUID, leaseOwner string, leaseGeneration int64, reason string, now time.Time) (bool, error) {
	return r.updateOwnedTaskOperationContext(ctx, ownerIdentity, operationID, leaseOwner, leaseGeneration, func(row *models.TaskOperationRecord) {
		now = normalizedTaskOperationTime(now)
		row.Status = "canceled"
		row.LeaseOwner = ""
		row.LeasedAt = nil
		row.LastError = taskOperationReviewReason(reason)
		row.UpdatedAt = now
	})
}

func (r *MemoryTaskStateRepository) updateOwnedTaskOperation(ownerIdentity string, operationID uuid.UUID, leaseOwner string, leaseGeneration int64, mutate func(*models.TaskOperationRecord)) (bool, error) {
	return r.updateOwnedTaskOperationContext(context.Background(), ownerIdentity, operationID, leaseOwner, leaseGeneration, mutate)
}

func (r *MemoryTaskStateRepository) updateOwnedTaskOperationContext(ctx context.Context, ownerIdentity string, operationID uuid.UUID, leaseOwner string, leaseGeneration int64, mutate func(*models.TaskOperationRecord)) (bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	ownerIdentity, err := normalizeTaskStateOwner(ownerIdentity)
	if err != nil {
		return false, err
	}
	leaseOwner = strings.TrimSpace(leaseOwner)
	if operationID == uuid.Nil || !validTaskOperationIdentifier(leaseOwner, 120) || leaseGeneration < 1 {
		return false, fmt.Errorf("task operation lease identity is invalid")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return false, err
	}
	for key, row := range r.operations {
		if row.ID == operationID && row.OwnerIdentity == ownerIdentity && row.Status == "running" && row.LeaseOwner == leaseOwner && row.LeaseGeneration == leaseGeneration {
			mutate(&row)
			r.operations[key] = row
			return true, nil
		}
	}
	return false, nil
}

func (r *PostgresTaskStateRepository) ClaimTaskOperation(ownerIdentity, idempotencyKey, requestDigest, mode, leaseOwner string, now time.Time, leaseDuration time.Duration) (TaskOperationClaim, error) {
	return r.ClaimTaskOperationContext(context.Background(), ownerIdentity, idempotencyKey, requestDigest, mode, leaseOwner, now, leaseDuration)
}

func (r *PostgresTaskStateRepository) ClaimTaskOperationContext(ctx context.Context, ownerIdentity, idempotencyKey, requestDigest, mode, leaseOwner string, now time.Time, leaseDuration time.Duration) (TaskOperationClaim, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return TaskOperationClaim{}, err
	}
	ownerIdentity, idempotencyKey, requestDigest, mode, leaseOwner, err := normalizeTaskOperationIdentity(ownerIdentity, idempotencyKey, requestDigest, mode, leaseOwner)
	if err != nil {
		return TaskOperationClaim{}, err
	}
	now = normalizedTaskOperationTime(now)
	var claim TaskOperationClaim
	err = r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		lockDigest := fmt.Sprintf("%x", sha256.Sum256([]byte(ownerIdentity+"\x00"+idempotencyKey)))
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", lockDigest).Error; err != nil {
			return err
		}
		var row models.TaskOperationRecord
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("owner_identity = ? AND idempotency_key = ?", ownerIdentity, idempotencyKey).
			First(&row).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			leasedAt := now
			row = models.TaskOperationRecord{
				ID: uuid.New(), OwnerIdentity: ownerIdentity, IdempotencyKey: idempotencyKey,
				RequestDigest: requestDigest, Mode: mode, Status: "running", LeaseOwner: leaseOwner,
				LeaseGeneration: 1, LeasedAt: &leasedAt, CreatedAt: now, UpdatedAt: now,
			}
			if err := tx.Create(&row).Error; err != nil {
				return err
			}
			claim = TaskOperationClaim{Operation: row, Disposition: TaskOperationAcquired}
			return nil
		}
		if err != nil {
			return err
		}
		existingClaim, expired, err := classifyExistingTaskOperation(row, requestDigest, mode, now, leaseDuration)
		if err != nil {
			return err
		}
		if expired {
			row.Status = "needs_review"
			row.LastError = taskOperationReviewReason("prior task worker lease expired; execution outcome is unknown")
			row.LeaseOwner = ""
			row.LeasedAt = nil
			row.UpdatedAt = now
			if err := tx.Save(&row).Error; err != nil {
				return err
			}
			existingClaim.Operation = row
		}
		claim = existingClaim
		return nil
	})
	return claim, err
}

func (r *PostgresTaskStateRepository) HeartbeatTaskOperation(ownerIdentity string, operationID uuid.UUID, leaseOwner string, leaseGeneration int64, now time.Time) (bool, error) {
	return r.HeartbeatTaskOperationContext(context.Background(), ownerIdentity, operationID, leaseOwner, leaseGeneration, now)
}

func (r *PostgresTaskStateRepository) HeartbeatTaskOperationContext(ctx context.Context, ownerIdentity string, operationID uuid.UUID, leaseOwner string, leaseGeneration int64, now time.Time) (bool, error) {
	ctx = normalizedTaskOperationContext(ctx)
	now = normalizedTaskOperationTime(now)
	result := r.ownedTaskOperation(r.DB.WithContext(ctx), ownerIdentity, operationID, leaseOwner, leaseGeneration).Updates(map[string]any{"leased_at": now, "updated_at": now})
	return result.RowsAffected == 1, result.Error
}

func (r *PostgresTaskStateRepository) CompleteTaskOperation(ownerIdentity string, operationID uuid.UUID, leaseOwner string, leaseGeneration int64, taskPlanID string, now time.Time) (bool, error) {
	return r.CompleteTaskOperationContext(context.Background(), ownerIdentity, operationID, leaseOwner, leaseGeneration, taskPlanID, now)
}

func (r *PostgresTaskStateRepository) CompleteTaskOperationContext(ctx context.Context, ownerIdentity string, operationID uuid.UUID, leaseOwner string, leaseGeneration int64, taskPlanID string, now time.Time) (bool, error) {
	ctx = normalizedTaskOperationContext(ctx)
	taskPlanID = strings.TrimSpace(taskPlanID)
	if taskPlanID == "" || len([]rune(taskPlanID)) > 160 {
		return false, fmt.Errorf("task plan id must contain 1 to 160 characters")
	}
	now = normalizedTaskOperationTime(now)
	result := r.ownedTaskOperation(r.DB.WithContext(ctx), ownerIdentity, operationID, leaseOwner, leaseGeneration).Updates(map[string]any{
		"status": "completed", "task_plan_id": taskPlanID, "lease_owner": "", "leased_at": nil,
		"last_error": "", "completed_at": now, "updated_at": now,
	})
	return result.RowsAffected == 1, result.Error
}

func (r *PostgresTaskStateRepository) MarkTaskOperationNeedsReview(ownerIdentity string, operationID uuid.UUID, leaseOwner string, leaseGeneration int64, reason string, now time.Time) (bool, error) {
	return r.MarkTaskOperationNeedsReviewContext(context.Background(), ownerIdentity, operationID, leaseOwner, leaseGeneration, reason, now)
}

func (r *PostgresTaskStateRepository) MarkTaskOperationNeedsReviewContext(ctx context.Context, ownerIdentity string, operationID uuid.UUID, leaseOwner string, leaseGeneration int64, reason string, now time.Time) (bool, error) {
	ctx = normalizedTaskOperationContext(ctx)
	now = normalizedTaskOperationTime(now)
	result := r.ownedTaskOperation(r.DB.WithContext(ctx), ownerIdentity, operationID, leaseOwner, leaseGeneration).Updates(map[string]any{
		"status": "needs_review", "lease_owner": "", "leased_at": nil,
		"last_error": taskOperationReviewReason(reason), "updated_at": now,
	})
	return result.RowsAffected == 1, result.Error
}

func (r *PostgresTaskStateRepository) CancelTaskOperation(ownerIdentity string, operationID uuid.UUID, leaseOwner string, leaseGeneration int64, reason string, now time.Time) (bool, error) {
	return r.CancelTaskOperationContext(context.Background(), ownerIdentity, operationID, leaseOwner, leaseGeneration, reason, now)
}

func (r *PostgresTaskStateRepository) CancelTaskOperationContext(ctx context.Context, ownerIdentity string, operationID uuid.UUID, leaseOwner string, leaseGeneration int64, reason string, now time.Time) (bool, error) {
	ctx = normalizedTaskOperationContext(ctx)
	now = normalizedTaskOperationTime(now)
	result := r.ownedTaskOperation(r.DB.WithContext(ctx), ownerIdentity, operationID, leaseOwner, leaseGeneration).Updates(map[string]any{
		"status": "canceled", "lease_owner": "", "leased_at": nil,
		"last_error": taskOperationReviewReason(reason), "updated_at": now,
	})
	return result.RowsAffected == 1, result.Error
}

func (r *PostgresTaskStateRepository) ownedTaskOperation(db *gorm.DB, ownerIdentity string, operationID uuid.UUID, leaseOwner string, leaseGeneration int64) *gorm.DB {
	return db.Model(&models.TaskOperationRecord{}).Where(
		"id = ? AND owner_identity = ? AND status = 'running' AND lease_owner = ? AND lease_generation = ?",
		operationID, strings.TrimSpace(ownerIdentity), strings.TrimSpace(leaseOwner), leaseGeneration,
	)
}
