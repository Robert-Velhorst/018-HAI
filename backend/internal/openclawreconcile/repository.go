package openclawreconcile

import (
	"context"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"automation-hub-backend/internal/agentruntime"
	"automation-hub-backend/internal/infra"
	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/safety"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Repository retains only the private identifiers required to reconcile a
// Gateway-admitted session. It is not an evidence/archive store for Gateway
// transcripts, tool calls, or model output.
type Repository struct {
	db *gorm.DB
}

func DefaultRepository() (*Repository, error) {
	db, err := infra.GetDefaultDB()
	if err != nil {
		return nil, err
	}
	return &Repository{db: db}, nil
}

func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) CreateOpenClawGatewayReceipt(_ context.Context, receipt agentruntime.OpenClawGatewayReceipt) error {
	if r == nil || r.db == nil {
		return fmt.Errorf("OpenClaw gateway receipt repository is unavailable")
	}
	if !validAdmissionIntent(receipt) {
		return fmt.Errorf("invalid OpenClaw gateway admission intent")
	}
	entry := models.OpenClawGatewaySessionReceipt{
		ExecutionReference: receipt.ExecutionReference,
		OwnerIdentity:      receipt.OwnerIdentity,
		RuntimeTaskID:      receipt.RuntimeTaskID,
		SessionKey:         "",
		SessionID:          "",
		RequestedModel:     receipt.RequestedModel,
		RunID:              "",
		Status:             "admitting",
		CreatedAt:          receipt.CreatedAt.UTC(),
	}
	return r.db.Create(&entry).Error
}

func (r *Repository) MarkOpenClawGatewayReceiptAdmitted(_ context.Context, receipt agentruntime.OpenClawGatewayReceipt) (bool, error) {
	if r == nil || r.db == nil {
		return false, fmt.Errorf("OpenClaw gateway receipt repository is unavailable")
	}
	if !validReceipt(receipt) {
		return false, fmt.Errorf("invalid admitted OpenClaw gateway receipt")
	}
	updated := r.db.Model(&models.OpenClawGatewaySessionReceipt{}).
		Where("execution_reference = ? AND owner_identity = ? AND runtime_task_id = ? AND status = ? AND reconciled_at IS NULL", receipt.ExecutionReference, receipt.OwnerIdentity, receipt.RuntimeTaskID, "admitting").
		Updates(map[string]any{
			"session_key":     receipt.SessionKey,
			"session_id":      receipt.SessionID,
			"requested_model": receipt.RequestedModel,
			"run_id":          receipt.RunID,
			"status":          "admitted",
		})
	if updated.Error != nil {
		return false, updated.Error
	}
	return updated.RowsAffected == 1, nil
}

func (r *Repository) MarkOpenClawGatewayReceiptNotAdmitted(_ context.Context, reference, owner, taskID string) (bool, error) {
	if r == nil || r.db == nil {
		return false, fmt.Errorf("OpenClaw gateway receipt repository is unavailable")
	}
	if !validOpenClawGatewayReceiptReference(reference) || strings.TrimSpace(owner) == "" || strings.TrimSpace(taskID) == "" {
		return false, fmt.Errorf("invalid OpenClaw gateway admission binding")
	}
	now := time.Now().UTC()
	updated := r.db.Model(&models.OpenClawGatewaySessionReceipt{}).
		Where("execution_reference = ? AND owner_identity = ? AND runtime_task_id = ? AND status = ? AND reconciled_at IS NULL", reference, owner, taskID, "admitting").
		Updates(map[string]any{
			"status":                            "not_admitted",
			"reconciled_at":                     now,
			"cancellation_next_attempt_at":      gorm.Expr("NULL"),
			"cancellation_delivery_lease_until": gorm.Expr("NULL"),
			"cancellation_delivery_token":       "",
			"cancellation_status": gorm.Expr(
				"CASE WHEN cancellation_intent_id <> '' THEN ? ELSE cancellation_status END",
				"not_required",
			),
			"cancellation_message": gorm.Expr(
				"CASE WHEN cancellation_intent_id <> '' THEN ? ELSE cancellation_message END",
				"Gateway explicitly reported no admitted run; no cancellation was required",
			),
		})
	if updated.Error != nil {
		return false, updated.Error
	}
	return updated.RowsAffected == 1, nil
}

func (r *Repository) FindOpenClawGatewayReceipt(reference string) (agentruntime.OpenClawGatewayReceipt, error) {
	if r == nil || r.db == nil {
		return agentruntime.OpenClawGatewayReceipt{}, fmt.Errorf("OpenClaw gateway receipt repository is unavailable")
	}
	var entry models.OpenClawGatewaySessionReceipt
	if err := r.db.Where("execution_reference = ?", strings.TrimSpace(reference)).First(&entry).Error; err != nil {
		return agentruntime.OpenClawGatewayReceipt{}, err
	}
	return receiptFromModel(entry), nil
}

func (r *Repository) FindOpenClawGatewayReceiptForEvent(ctx context.Context, owner string, eventID uuid.UUID) (agentruntime.OpenClawGatewayReceipt, error) {
	if r == nil || r.db == nil || strings.TrimSpace(owner) == "" || eventID == uuid.Nil {
		return agentruntime.OpenClawGatewayReceipt{}, fmt.Errorf("owner-bound OpenClaw receipt lookup is unavailable")
	}
	var entry models.OpenClawGatewaySessionReceipt
	err := r.db.WithContext(ctx).Table("openclaw_gateway_session_receipts AS receipt").Select("receipt.*").
		Joins("JOIN automation_launch_events AS event ON event.execution_reference = receipt.execution_reference AND event.runtime_task_id = receipt.runtime_task_id AND event.owner_identity = receipt.owner_identity").
		Where("event.id = ? AND event.runtime_type = 'openclaw' AND event.owner_identity = ? AND receipt.owner_identity = ?", eventID, strings.TrimSpace(owner), strings.TrimSpace(owner)).
		Take(&entry).Error
	if err != nil {
		return agentruntime.OpenClawGatewayReceipt{}, err
	}
	return receiptFromModel(entry), nil
}

func (r *Repository) ListUnreconciledOpenClawGatewayReceipts(limit int) ([]agentruntime.OpenClawGatewayReceipt, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("OpenClaw gateway receipt repository is unavailable")
	}
	if limit < 1 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	var entries []models.OpenClawGatewaySessionReceipt
	if err := r.db.
		Where("status IN ? AND reconciled_at IS NULL", []string{"admitting", "admitted", "needs_review"}).
		Order("created_at ASC").
		Limit(limit).
		Find(&entries).Error; err != nil {
		return nil, err
	}
	receipts := make([]agentruntime.OpenClawGatewayReceipt, 0, len(entries))
	for _, entry := range entries {
		receipts = append(receipts, receiptFromModel(entry))
	}
	return receipts, nil
}

// ClaimNextUnreconciledOpenClawGatewayReceiptPage reserves the next bounded
// keyset page in a persisted scan cycle. A fixed high-water mark prevents a
// continuously growing tail from extending the current cycle indefinitely.
// The cursor-row lock serializes reservations across processes; a process
// crash only delays the abandoned page until the cycle wraps.
func (r *Repository) ClaimNextUnreconciledOpenClawGatewayReceiptPage(limit int) ([]agentruntime.OpenClawGatewayReceipt, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("OpenClaw gateway receipt repository is unavailable")
	}
	limit = boundedReceiptPage(limit)
	var claimed []models.OpenClawGatewaySessionReceipt
	err := r.db.Transaction(func(tx *gorm.DB) error {
		cursor := models.OpenClawGatewayReconcileCursor{Name: "gateway-sessions"}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&cursor).Error; err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("name = ?", cursor.Name).Take(&cursor).Error; err != nil {
			return err
		}

		if cursor.CycleHighCreatedAt == nil {
			high, err := latestUnreconciledReceipt(tx)
			if err != nil {
				return err
			}
			if high == nil {
				return nil
			}
			cursor.CycleHighCreatedAt = &high.CreatedAt
			cursor.CycleHighReference = high.ExecutionReference
		}

		page, err := queryUnreconciledReceiptPage(tx, limit, cursor.AfterCreatedAt, cursor.AfterExecutionReference, cursor.CycleHighCreatedAt, cursor.CycleHighReference)
		if err != nil {
			return err
		}
		if len(page) == 0 {
			// The remaining members of the old cycle were settled while workers
			// were processing it. Begin a new bounded cycle from current state.
			cursor.AfterCreatedAt = nil
			cursor.AfterExecutionReference = ""
			cursor.CycleHighCreatedAt = nil
			cursor.CycleHighReference = ""
			high, err := latestUnreconciledReceipt(tx)
			if err != nil {
				return err
			}
			if high == nil {
				return tx.Model(&models.OpenClawGatewayReconcileCursor{}).Where("name = ?", cursor.Name).Updates(map[string]any{
					"after_created_at":          gorm.Expr("NULL"),
					"after_execution_reference": "",
					"cycle_high_created_at":     gorm.Expr("NULL"),
					"cycle_high_reference":      "",
					"updated_at":                time.Now().UTC(),
				}).Error
			}
			cursor.CycleHighCreatedAt = &high.CreatedAt
			cursor.CycleHighReference = high.ExecutionReference
			page, err = queryUnreconciledReceiptPage(tx, limit, nil, "", cursor.CycleHighCreatedAt, cursor.CycleHighReference)
			if err != nil {
				return err
			}
		}
		if len(page) == 0 {
			return nil
		}
		last := page[len(page)-1]
		cursor.AfterCreatedAt = &last.CreatedAt
		cursor.AfterExecutionReference = last.ExecutionReference
		claimed = page
		return tx.Model(&models.OpenClawGatewayReconcileCursor{}).Where("name = ?", cursor.Name).Updates(map[string]any{
			"after_created_at":          cursor.AfterCreatedAt,
			"after_execution_reference": cursor.AfterExecutionReference,
			"cycle_high_created_at":     cursor.CycleHighCreatedAt,
			"cycle_high_reference":      cursor.CycleHighReference,
			"updated_at":                time.Now().UTC(),
		}).Error
	})
	if err != nil {
		return nil, err
	}
	return receiptsFromModels(claimed), nil
}

func latestUnreconciledReceipt(tx *gorm.DB) (*models.OpenClawGatewaySessionReceipt, error) {
	var latest models.OpenClawGatewaySessionReceipt
	err := tx.Where("status IN ? AND reconciled_at IS NULL", []string{"admitting", "admitted", "needs_review"}).
		Order("created_at DESC, execution_reference DESC").Take(&latest).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &latest, nil
}

func queryUnreconciledReceiptPage(
	tx *gorm.DB,
	limit int,
	afterCreatedAt *time.Time,
	afterReference string,
	highCreatedAt *time.Time,
	highReference string,
) ([]models.OpenClawGatewaySessionReceipt, error) {
	query := tx.Model(&models.OpenClawGatewaySessionReceipt{}).
		Where("status IN ? AND reconciled_at IS NULL", []string{"admitting", "admitted", "needs_review"})
	if afterCreatedAt != nil {
		query = query.Where("(created_at, execution_reference) > (?, ?)", afterCreatedAt.UTC(), strings.TrimSpace(afterReference))
	}
	if highCreatedAt != nil {
		query = query.Where("(created_at, execution_reference) <= (?, ?)", highCreatedAt.UTC(), strings.TrimSpace(highReference))
	}
	var entries []models.OpenClawGatewaySessionReceipt
	err := query.Order("created_at ASC, execution_reference ASC").Limit(limit).Find(&entries).Error
	return entries, err
}

func (r *Repository) ListUnreconciledOpenClawGatewayReceiptsAfter(limit int, afterCreatedAt time.Time, afterReference string) ([]agentruntime.OpenClawGatewayReceipt, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("OpenClaw gateway receipt repository is unavailable")
	}
	limit = boundedReceiptPage(limit)
	query := r.db.Model(&models.OpenClawGatewaySessionReceipt{}).
		Where("status IN ? AND reconciled_at IS NULL", []string{"admitting", "admitted", "needs_review"})
	if !afterCreatedAt.IsZero() {
		query = query.Where("(created_at > ? OR (created_at = ? AND execution_reference > ?))", afterCreatedAt.UTC(), afterCreatedAt.UTC(), strings.TrimSpace(afterReference))
	}
	var entries []models.OpenClawGatewaySessionReceipt
	if err := query.Order("created_at ASC, execution_reference ASC").Limit(limit).Find(&entries).Error; err != nil {
		return nil, err
	}
	return receiptsFromModels(entries), nil
}

func (r *Repository) ListOpenClawGatewayCancellationReceipts(limit int, afterCreatedAt time.Time, afterReference string) ([]agentruntime.OpenClawGatewayReceipt, error) {
	return r.listOpenClawGatewayCancellationReceipts(limit, time.Now().UTC(), afterCreatedAt, afterReference)
}

// ListDueOpenClawGatewayCancellationReceipts returns one bounded batch of
// currently due work. Scheduled passes never walk every page in one run.
func (r *Repository) ListDueOpenClawGatewayCancellationReceipts(limit int, now time.Time) ([]agentruntime.OpenClawGatewayReceipt, error) {
	return r.listOpenClawGatewayCancellationReceipts(limit, now, time.Time{}, "")
}

func (r *Repository) listOpenClawGatewayCancellationReceipts(limit int, now, afterCreatedAt time.Time, afterReference string) ([]agentruntime.OpenClawGatewayReceipt, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("OpenClaw gateway receipt repository is unavailable")
	}
	limit = boundedReceiptPage(limit)
	if now.IsZero() {
		now = time.Now().UTC()
	}
	query := r.db.Model(&models.OpenClawGatewaySessionReceipt{}).
		Where("cancellation_intent_id <> '' AND status IN ? AND reconciled_at IS NULL", []string{"admitting", "admitted", "needs_review"}).
		Where("(cancellation_delivery_lease_until IS NULL OR cancellation_delivery_lease_until <= ?)", now.UTC()).
		Where("((cancellation_status IN ? AND (cancellation_next_attempt_at IS NULL OR cancellation_next_attempt_at <= ?)) OR (cancellation_status = ? AND session_key <> '' AND run_id <> '' AND (cancellation_next_attempt_at IS NULL OR cancellation_next_attempt_at <= ?)))", []string{"requested", "delivery_failed"}, now.UTC(), "awaiting_identity", now.UTC())
	if !afterCreatedAt.IsZero() {
		query = query.Where("(created_at > ? OR (created_at = ? AND execution_reference > ?))", afterCreatedAt.UTC(), afterCreatedAt.UTC(), strings.TrimSpace(afterReference))
	}
	var entries []models.OpenClawGatewaySessionReceipt
	if err := query.Order("created_at ASC, execution_reference ASC").Limit(limit).Find(&entries).Error; err != nil {
		return nil, err
	}
	return receiptsFromModels(entries), nil
}

func (r *Repository) EnsureOpenClawGatewayCancellationIntent(_ context.Context, requested agentruntime.OpenClawGatewayReceipt, requestedAt time.Time) (agentruntime.OpenClawGatewayReceipt, error) {
	if r == nil || r.db == nil {
		return agentruntime.OpenClawGatewayReceipt{}, fmt.Errorf("OpenClaw gateway receipt repository is unavailable")
	}
	if !validCancellationBinding(requested) {
		return agentruntime.OpenClawGatewayReceipt{}, fmt.Errorf("invalid owner-bound OpenClaw cancellation binding")
	}
	if requestedAt.IsZero() {
		requestedAt = time.Now().UTC()
	}
	var result models.OpenClawGatewaySessionReceipt
	err := r.db.Transaction(func(tx *gorm.DB) error {
		var entry models.OpenClawGatewaySessionReceipt
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("execution_reference = ?", requested.ExecutionReference).Take(&entry).Error; err != nil {
			return err
		}
		if entry.OwnerIdentity != requested.OwnerIdentity || entry.RuntimeTaskID != requested.RuntimeTaskID {
			return fmt.Errorf("OpenClaw cancellation receipt owner/task binding changed")
		}
		if (requested.SessionKey != "" && requested.SessionKey != entry.SessionKey) ||
			(requested.RunID != "" && requested.RunID != entry.RunID) ||
			(requested.SessionID != "" && requested.SessionID != entry.SessionID) {
			return fmt.Errorf("OpenClaw cancellation receipt session/run binding changed")
		}
		if entry.ReconciledAt != nil || entry.Status == "terminal" || entry.Status == "not_admitted" {
			result = entry
			return nil
		}
		if entry.Status != "admitting" && entry.Status != "admitted" && entry.Status != "needs_review" {
			return fmt.Errorf("OpenClaw cancellation receipt is not unresolved")
		}
		if entry.CancellationIntentID == "" {
			entry.CancellationIntentID = cancellationIntentID(entry.ExecutionReference)
			entry.CancellationAt = &requestedAt
		}
		if entry.CancellationStatus != "acknowledged" && entry.CancellationStatus != "review_required" {
			if !agentruntime.ValidOpenClawGatewayRunID(entry.RunID) || strings.TrimSpace(entry.SessionKey) == "" {
				entry.CancellationStatus = "awaiting_identity"
				entry.CancellationMessage = "cancellation intent is durable; exact session key and run ID are not yet available"
			} else if entry.CancellationStatus == "" || entry.CancellationStatus == "awaiting_identity" || entry.CancellationStatus == "not_required" {
				entry.CancellationStatus = "requested"
				entry.CancellationMessage = "exact-run cancellation requested; terminal state remains unverified"
			}
		}
		if err := tx.Model(&models.OpenClawGatewaySessionReceipt{}).
			Where("id = ?", entry.ID).
			Updates(map[string]any{
				"cancellation_intent_id": entry.CancellationIntentID,
				"cancellation_status":    entry.CancellationStatus,
				"cancellation_message":   entry.CancellationMessage,
				"cancellation_at":        entry.CancellationAt,
			}).Error; err != nil {
			return err
		}
		result = entry
		return nil
	})
	if err != nil {
		return agentruntime.OpenClawGatewayReceipt{}, err
	}
	return receiptFromModel(result), nil
}

// ClaimOpenClawGatewayCancellationAttempt atomically reserves one exact-run
// delivery. Automatic callers honor backoff and the durable retry ceiling;
// explicit operator retries may bypass backoff and retry an escalated intent,
// but never bypass an active lease or change its owner/task/run binding.
func (r *Repository) ClaimOpenClawGatewayCancellationAttempt(
	ctx context.Context,
	expected agentruntime.OpenClawGatewayReceipt,
	attemptedAt time.Time,
	manual bool,
) (agentruntime.OpenClawGatewayReceipt, string, bool, error) {
	if r == nil || r.db == nil {
		return agentruntime.OpenClawGatewayReceipt{}, "", false, fmt.Errorf("OpenClaw gateway receipt repository is unavailable")
	}
	if !validCancellationBinding(expected) || expected.CancellationIntentID == "" {
		return agentruntime.OpenClawGatewayReceipt{}, "", false, fmt.Errorf("invalid owner-bound OpenClaw cancellation attempt binding")
	}
	if attemptedAt.IsZero() {
		attemptedAt = time.Now().UTC()
	}
	attemptedAt = attemptedAt.UTC()
	var claimed models.OpenClawGatewaySessionReceipt
	token := uuid.NewString()
	reserved := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var entry models.OpenClawGatewaySessionReceipt
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("execution_reference = ?", expected.ExecutionReference).Take(&entry).Error; err != nil {
			return err
		}
		if entry.OwnerIdentity != expected.OwnerIdentity || entry.RuntimeTaskID != expected.RuntimeTaskID ||
			entry.CancellationIntentID != expected.CancellationIntentID ||
			entry.SessionKey != expected.SessionKey || entry.RunID != expected.RunID ||
			(entry.SessionID != "" && entry.SessionID != expected.SessionID) {
			return fmt.Errorf("OpenClaw cancellation receipt owner/task/session/run binding changed")
		}
		if entry.ReconciledAt != nil || (entry.Status != "admitting" && entry.Status != "admitted" && entry.Status != "needs_review") ||
			entry.CancellationStatus == "acknowledged" || entry.CancellationStatus == "not_required" {
			return nil
		}
		if entry.CancellationDeliveryLeaseUntil != nil && entry.CancellationDeliveryLeaseUntil.After(attemptedAt) {
			return nil
		}
		if !agentruntime.ValidOpenClawGatewayRunID(entry.RunID) || strings.TrimSpace(entry.SessionKey) == "" {
			return nil
		}

		if !manual {
			if entry.CancellationStatus == "review_required" {
				return nil
			}
			if entry.CancellationNextAttemptAt != nil && entry.CancellationNextAttemptAt.After(attemptedAt) {
				return nil
			}
			if entry.CancellationAutomaticAttempts >= maxAutomaticCancellationAttempts {
				entry.CancellationStatus = "review_required"
				entry.CancellationMessage = "automatic cancellation attempts exhausted; operator review and an explicit retry are required"
				entry.CancellationReviewAt = &attemptedAt
				entry.CancellationNextAttemptAt = nil
				if err := tx.Model(&models.OpenClawGatewaySessionReceipt{}).Where("id = ?", entry.ID).Updates(map[string]any{
					"cancellation_status":               entry.CancellationStatus,
					"cancellation_message":              entry.CancellationMessage,
					"cancellation_review_at":            entry.CancellationReviewAt,
					"cancellation_next_attempt_at":      gorm.Expr("NULL"),
					"cancellation_delivery_lease_until": gorm.Expr("NULL"),
					"cancellation_delivery_token":       "",
				}).Error; err != nil {
					return err
				}
				return nil
			}
			if entry.CancellationStatus != "requested" && entry.CancellationStatus != "delivery_failed" && entry.CancellationStatus != "awaiting_identity" {
				return nil
			}
		} else if entry.CancellationStatus != "requested" && entry.CancellationStatus != "delivery_failed" &&
			entry.CancellationStatus != "awaiting_identity" && entry.CancellationStatus != "review_required" {
			return nil
		}

		entry.CancellationAttempts++
		if !manual {
			entry.CancellationAutomaticAttempts++
		}
		entry.CancellationTriedAt = &attemptedAt
		leaseUntil := attemptedAt.Add(cancellationDeliveryLeaseDuration)
		entry.CancellationDeliveryLeaseUntil = &leaseUntil
		entry.CancellationDeliveryToken = token
		entry.CancellationNextAttemptAt = nil
		if entry.CancellationStatus == "awaiting_identity" {
			entry.CancellationStatus = "requested"
		}
		if err := tx.Model(&models.OpenClawGatewaySessionReceipt{}).Where("id = ?", entry.ID).Updates(map[string]any{
			"cancellation_status":               entry.CancellationStatus,
			"cancellation_attempts":             entry.CancellationAttempts,
			"cancellation_automatic_attempts":   entry.CancellationAutomaticAttempts,
			"cancellation_tried_at":             entry.CancellationTriedAt,
			"cancellation_next_attempt_at":      gorm.Expr("NULL"),
			"cancellation_delivery_lease_until": entry.CancellationDeliveryLeaseUntil,
			"cancellation_delivery_token":       entry.CancellationDeliveryToken,
		}).Error; err != nil {
			return err
		}
		claimed = entry
		reserved = true
		return nil
	})
	if err != nil {
		return agentruntime.OpenClawGatewayReceipt{}, "", false, err
	}
	if !reserved {
		return agentruntime.OpenClawGatewayReceipt{}, "", false, nil
	}
	return receiptFromModel(claimed), token, true, nil
}

func (r *Repository) RecordOpenClawGatewayCancellationOutcome(ctx context.Context, expected agentruntime.OpenClawGatewayReceipt, outcome, message string, attemptedAt time.Time) error {
	if r == nil || r.db == nil {
		return fmt.Errorf("OpenClaw gateway receipt repository is unavailable")
	}
	if !validCancellationBinding(expected) || (outcome != "awaiting_identity" && outcome != "not_required") {
		return fmt.Errorf("invalid OpenClaw cancellation outcome binding")
	}
	message = boundedCancellationMessage(message)
	if attemptedAt.IsZero() {
		attemptedAt = time.Now().UTC()
	}
	query := r.db.WithContext(ctx).Model(&models.OpenClawGatewaySessionReceipt{}).
		Where("execution_reference = ? AND owner_identity = ? AND runtime_task_id = ? AND cancellation_intent_id <> ''", expected.ExecutionReference, expected.OwnerIdentity, expected.RuntimeTaskID)
	if outcome == "not_required" {
		query = query.Where("status = ? AND reconciled_at IS NOT NULL", "not_admitted")
	} else {
		query = query.Where("status IN ? AND reconciled_at IS NULL", []string{"admitting", "admitted", "needs_review"}).
			Where("cancellation_status NOT IN ?", []string{"acknowledged", "review_required"}).
			Where("session_key = ? AND run_id = ?", expected.SessionKey, expected.RunID)
	}
	updated := query.Updates(map[string]any{"cancellation_status": outcome, "cancellation_message": message})
	if updated.Error != nil {
		return updated.Error
	}
	if updated.RowsAffected != 1 && outcome != "not_required" {
		return fmt.Errorf("OpenClaw cancellation receipt binding changed before outcome persistence")
	}
	return nil
}

// RecordOpenClawGatewayCancellationAttemptOutcome finalizes only the currently
// leased delivery. A stale worker cannot overwrite a newer claim because each
// claim replaces the persisted token. Acknowledgment is monotonic and never
// changes the receipt's terminal status.
func (r *Repository) RecordOpenClawGatewayCancellationAttemptOutcome(
	ctx context.Context,
	expected agentruntime.OpenClawGatewayReceipt,
	token, outcome, message string,
	attemptedAt time.Time,
) error {
	if r == nil || r.db == nil {
		return fmt.Errorf("OpenClaw gateway receipt repository is unavailable")
	}
	if !validCancellationBinding(expected) || strings.TrimSpace(token) == "" || (outcome != "acknowledged" && outcome != "delivery_failed") {
		return fmt.Errorf("invalid OpenClaw cancellation attempt outcome")
	}
	if attemptedAt.IsZero() {
		attemptedAt = time.Now().UTC()
	}
	attemptedAt = attemptedAt.UTC()
	message = boundedCancellationMessage(message)
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var entry models.OpenClawGatewaySessionReceipt
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("execution_reference = ?", expected.ExecutionReference).Take(&entry).Error; err != nil {
			return err
		}
		if entry.OwnerIdentity != expected.OwnerIdentity || entry.RuntimeTaskID != expected.RuntimeTaskID ||
			entry.CancellationIntentID != expected.CancellationIntentID || entry.SessionKey != expected.SessionKey ||
			entry.SessionID != expected.SessionID || entry.RunID != expected.RunID {
			return fmt.Errorf("OpenClaw cancellation receipt owner/task/session/run binding changed")
		}
		if entry.CancellationStatus == "acknowledged" {
			return nil
		}
		if entry.CancellationDeliveryToken != token || entry.CancellationDeliveryToken == "" {
			return fmt.Errorf("OpenClaw cancellation attempt lease was superseded")
		}
		updates := map[string]any{
			"cancellation_delivery_lease_until": gorm.Expr("NULL"),
			"cancellation_delivery_token":       "",
			"cancellation_next_attempt_at":      gorm.Expr("NULL"),
			"cancellation_message":              message,
		}
		if outcome == "acknowledged" {
			updates["cancellation_status"] = "acknowledged"
		} else if entry.CancellationAutomaticAttempts >= maxAutomaticCancellationAttempts {
			updates["cancellation_status"] = "review_required"
			updates["cancellation_review_at"] = attemptedAt
			updates["cancellation_message"] = "automatic cancellation attempts exhausted; operator review and an explicit retry are required"
		} else {
			updates["cancellation_status"] = "delivery_failed"
			delayAttempt := entry.CancellationAutomaticAttempts
			if delayAttempt < 1 {
				delayAttempt = 1
			}
			nextAttemptAt := attemptedAt.Add(cancellationRetryDelay(delayAttempt))
			updates["cancellation_next_attempt_at"] = nextAttemptAt
		}
		return tx.Model(&models.OpenClawGatewaySessionReceipt{}).Where("id = ?", entry.ID).Updates(updates).Error
	})
}

func boundedReceiptPage(limit int) int {
	if limit < 1 {
		return 20
	}
	if limit > 100 {
		return 100
	}
	return limit
}

func receiptsFromModels(entries []models.OpenClawGatewaySessionReceipt) []agentruntime.OpenClawGatewayReceipt {
	receipts := make([]agentruntime.OpenClawGatewayReceipt, 0, len(entries))
	for _, entry := range entries {
		receipts = append(receipts, receiptFromModel(entry))
	}
	return receipts
}

func (r *Repository) MarkOpenClawGatewayReceiptTerminal(reference, status string, finishedAt time.Time) (bool, error) {
	if r == nil || r.db == nil {
		return false, fmt.Errorf("OpenClaw gateway receipt repository is unavailable")
	}
	if status != "completed" && status != "failed" {
		return false, fmt.Errorf("invalid OpenClaw gateway terminal status")
	}
	if finishedAt.IsZero() {
		finishedAt = time.Now().UTC()
	}
	updated := r.db.Model(&models.OpenClawGatewaySessionReceipt{}).
		Where("execution_reference = ? AND status IN ? AND reconciled_at IS NULL", strings.TrimSpace(reference), []string{"admitted", "needs_review"}).
		Updates(map[string]any{
			"status":                            "terminal",
			"terminal_status":                   status,
			"terminal_at":                       finishedAt.UTC(),
			"reconciled_at":                     time.Now().UTC(),
			"cancellation_next_attempt_at":      gorm.Expr("NULL"),
			"cancellation_delivery_lease_until": gorm.Expr("NULL"),
			"cancellation_delivery_token":       "",
		})
	if updated.Error != nil {
		return false, updated.Error
	}
	return updated.RowsAffected == 1, nil
}

func (r *Repository) MarkOpenClawGatewayReceiptNeedsReview(reference, reason string, reviewedAt time.Time) (bool, error) {
	if r == nil || r.db == nil {
		return false, fmt.Errorf("OpenClaw gateway receipt repository is unavailable")
	}
	reason = strings.TrimSpace(reason)
	if reason == "" || len(reason) > 1024 {
		return false, fmt.Errorf("invalid OpenClaw gateway review reason")
	}
	if reviewedAt.IsZero() {
		reviewedAt = time.Now().UTC()
	}
	updated := r.db.Model(&models.OpenClawGatewaySessionReceipt{}).
		Where("execution_reference = ? AND status IN ? AND reconciled_at IS NULL", strings.TrimSpace(reference), []string{"admitting", "admitted", "needs_review"}).
		Updates(map[string]any{
			"status":        "needs_review",
			"review_reason": reason,
			"review_at":     reviewedAt.UTC(),
		})
	if updated.Error != nil {
		return false, updated.Error
	}
	return updated.RowsAffected == 1, nil
}

// CreateOpenClawGatewayArtifactDescriptors stores only descriptors derived
// from the Gateway list response. The caller never receives a path to persist
// original artifact IDs, names, remote URLs, or contents.
func (r *Repository) CreateOpenClawGatewayArtifactDescriptors(ctx context.Context, reference string, descriptors []agentruntime.GatewayArtifactDescriptor) error {
	if r == nil || r.db == nil {
		return fmt.Errorf("OpenClaw gateway receipt repository is unavailable")
	}
	reference = strings.TrimSpace(reference)
	if !validOpenClawGatewayReceiptReference(reference) || len(descriptors) == 0 || len(descriptors) > 20 {
		return fmt.Errorf("invalid OpenClaw gateway artifact descriptors")
	}
	entries := make([]models.OpenClawGatewayArtifactReceipt, 0, len(descriptors))
	for _, descriptor := range descriptors {
		if !validArtifactDescriptor(descriptor) {
			return fmt.Errorf("invalid OpenClaw gateway artifact descriptor")
		}
		entries = append(entries, models.OpenClawGatewayArtifactReceipt{
			ExecutionReference: reference,
			ArtifactDigest:     strings.ToLower(strings.TrimSpace(descriptor.Digest)),
			ArtifactType:       strings.TrimSpace(descriptor.Type),
			MIMEType:           strings.TrimSpace(descriptor.MIMEType),
			SizeBytes:          descriptor.SizeBytes,
		})
	}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "execution_reference"}, {Name: "artifact_digest"}},
		DoNothing: true,
	}).Create(&entries).Error
}

func receiptFromModel(entry models.OpenClawGatewaySessionReceipt) agentruntime.OpenClawGatewayReceipt {
	terminalAt := time.Time{}
	if entry.TerminalAt != nil {
		terminalAt = entry.TerminalAt.UTC()
	}
	return agentruntime.OpenClawGatewayReceipt{
		ExecutionReference:   entry.ExecutionReference,
		OwnerIdentity:        entry.OwnerIdentity,
		RuntimeTaskID:        entry.RuntimeTaskID,
		Status:               entry.Status,
		SessionKey:           entry.SessionKey,
		SessionID:            entry.SessionID,
		RequestedModel:       entry.RequestedModel,
		RunID:                entry.RunID,
		CreatedAt:            entry.CreatedAt.UTC(),
		TerminalStatus:       entry.TerminalStatus,
		TerminalAt:           terminalAt,
		CancellationIntentID: entry.CancellationIntentID,
		CancellationStatus:   entry.CancellationStatus,
		CancellationMessage:  entry.CancellationMessage,
		CancellationAttempts: entry.CancellationAttempts,
		CancellationAt:       timeValue(entry.CancellationAt),
		CancellationTriedAt:  timeValue(entry.CancellationTriedAt),
	}
}

func timeValue(value *time.Time) time.Time {
	if value == nil {
		return time.Time{}
	}
	return value.UTC()
}

func validCancellationBinding(receipt agentruntime.OpenClawGatewayReceipt) bool {
	return validOpenClawGatewayReceiptReference(receipt.ExecutionReference) &&
		strings.TrimSpace(receipt.OwnerIdentity) != "" && strings.TrimSpace(receipt.RuntimeTaskID) != ""
}

func cancellationIntentID(reference string) string {
	return "ocancel:v1:" + strings.TrimPrefix(strings.TrimSpace(reference), "ocgw:v2:")
}

func boundedCancellationMessage(message string) string {
	message = strings.TrimSpace(message)
	if message == "" {
		return "OpenClaw cancellation outcome recorded; terminal state remains unverified"
	}
	message = strings.TrimSpace(safety.RedactSecrets(message))
	if len(message) > 512 {
		message = message[:512]
	}
	return message
}

func validReceipt(receipt agentruntime.OpenClawGatewayReceipt) bool {
	reference := strings.TrimSpace(receipt.ExecutionReference)
	if !strings.HasPrefix(reference, "ocgw:v2:") || len(reference) > 64 {
		return false
	}
	if _, err := uuid.Parse(strings.TrimPrefix(reference, "ocgw:v2:")); err != nil {
		return false
	}
	return strings.TrimSpace(receipt.OwnerIdentity) != "" && len(receipt.OwnerIdentity) <= 255 &&
		strings.TrimSpace(receipt.RuntimeTaskID) != "" && len(receipt.RuntimeTaskID) <= 120 &&
		strings.TrimSpace(receipt.SessionKey) != "" &&
		strings.TrimSpace(receipt.RunID) != "" &&
		!receipt.CreatedAt.IsZero()
}

func validAdmissionIntent(receipt agentruntime.OpenClawGatewayReceipt) bool {
	reference := strings.TrimSpace(receipt.ExecutionReference)
	if !validOpenClawGatewayReceiptReference(reference) || len(reference) > 64 ||
		strings.TrimSpace(receipt.OwnerIdentity) == "" || len(receipt.OwnerIdentity) > 255 ||
		strings.TrimSpace(receipt.RuntimeTaskID) == "" || len(receipt.RuntimeTaskID) > 120 ||
		receipt.Status != "admitting" || receipt.CreatedAt.IsZero() ||
		strings.TrimSpace(receipt.SessionKey) != "" || strings.TrimSpace(receipt.RunID) != "" {
		return false
	}
	return true
}

func validOpenClawGatewayReceiptReference(reference string) bool {
	reference = strings.TrimSpace(reference)
	if !strings.HasPrefix(reference, "ocgw:v2:") || len(reference) > 64 {
		return false
	}
	_, err := uuid.Parse(strings.TrimPrefix(reference, "ocgw:v2:"))
	return err == nil
}

func validArtifactDescriptor(descriptor agentruntime.GatewayArtifactDescriptor) bool {
	digest := strings.TrimSpace(descriptor.Digest)
	if len(digest) != 64 || (descriptor.SizeBytes != nil && *descriptor.SizeBytes < 0) || strings.TrimSpace(descriptor.Type) == "" || len(strings.TrimSpace(descriptor.Type)) > 120 || len(strings.TrimSpace(descriptor.MIMEType)) > 160 {
		return false
	}
	decoded, err := hex.DecodeString(digest)
	return err == nil && len(decoded) == 32
}
