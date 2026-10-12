package workflow

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"automation-hub-backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ProcessReminderDelivery serializes workers across processes and commits the
// internal signal and receipt together. Rollback (including panic/crash) leaves
// neither effect; a committed receipt excludes replay. No lease/schema is needed.
func (r *GormRepository) ProcessReminderDelivery(candidate reminderDeliveryCandidate, sink ReminderDeliverySink) (*ReminderDeliveryRunResult, error) {
	if r == nil || r.DB == nil || sink == nil || candidate.Authorization.ID == uuid.Nil || candidate.Authorization.OwnerIdentity == "" {
		return nil, fmt.Errorf("valid durable reminder delivery is required")
	}
	if err := reminderDeliveryRootDB(r); err != nil {
		return nil, err
	}
	parent := r.DB.Statement.Context
	if parent == nil {
		return nil, ErrReminderDeliveryContextUnavailable
	}
	if err := parent.Err(); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	var outcome *ReminderDeliveryRunResult
	err := r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var authorization models.WorkflowReminderDeliveryAuthorization
		err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("id = ? AND owner_identity = ?", candidate.Authorization.ID, candidate.Authorization.OwnerIdentity).
			First(&authorization).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if authorization.ReminderAt.After(time.Now().UTC()) {
			return nil
		}
		// Selection is only a hint: another worker may have committed since it.
		var attempts []models.WorkflowReminderDeliveryAttempt
		if err := tx.Where("authorization_id = ?", authorization.ID).Order("attempt_number ASC").Find(&attempts).Error; err != nil {
			return err
		}
		for index, attempt := range attempts {
			if attempt.AttemptNumber != index+1 {
				return fmt.Errorf("reminder delivery attempt ledger is not contiguous")
			}
			if attempt.Status != ReminderDeliveryStatusRetryableFailure {
				return nil
			}
		}
		if len(attempts) >= ReminderDeliveryMaxAttempts {
			return nil
		}
		locked := &reminderDeliveryLockedRepository{GormRepository: &GormRepository{DB: tx}}
		outcome, err = processReminderDeliveryContext(ctx, locked, reminderDeliveryCandidate{Authorization: authorization, AttemptCount: len(attempts)}, &reminderDeliveryTransactionSink{ctx: ctx, tx: tx, sink: sink})
		return err
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, err
	}
	return outcome, nil
}

func reminderDeliveryRootDB(r *GormRepository) error {
	if r == nil || r.DB == nil || r.DB.Config == nil || r.DB.Dialector == nil || r.DB.Dialector.Name() != "postgres" ||
		r.DB.Statement == nil || r.DB.Statement.ConnPool == nil || r.DB.DryRun || r.DB.DisableNestedTransaction {
		return fmt.Errorf("atomic reminder delivery requires a root PostgreSQL connection with savepoints enabled")
	}
	if _, nested := r.DB.Statement.ConnPool.(gorm.TxCommitter); nested {
		return fmt.Errorf("atomic reminder delivery requires a root PostgreSQL connection")
	}
	return nil
}

type reminderDeliveryLockedRepository struct{ *GormRepository }

func (r *reminderDeliveryLockedRepository) LoadReminderActivationRequestForOwner(owner string, id uuid.UUID) (*models.WorkflowReminderActivationRequest, *models.WorkflowReminderActivationDecision, error) {
	return loadReminderActivationRequest(r.DB, owner, id, true)
}

func (r *reminderDeliveryLockedRepository) LoadReminderActivationSourceForOwner(owner string, id uuid.UUID) (*WorkflowReminderCandidate, error) {
	return loadReminderActivationSource(r.DB, owner, id, true)
}

type reminderDeliveryTransactionSink struct {
	ctx  context.Context
	tx   *gorm.DB
	sink ReminderDeliverySink
}

func (s *reminderDeliveryTransactionSink) DeliverInternalReminder(_ context.Context, envelope ReminderDeliveryEnvelope) error {
	if s.ctx == nil {
		return ErrReminderDeliveryContextUnavailable
	}
	if err := s.ctx.Err(); err != nil {
		return err
	}
	sink, ok := s.sink.(TransactionalReminderDeliverySink)
	if !ok {
		return fmt.Errorf("durable reminder delivery requires a transactional internal sink")
	}
	// A failing sink may already have written rows or aborted PostgreSQL's
	// transaction. Roll it back to a savepoint before recording a bounded retry.
	return s.tx.Transaction(func(tx *gorm.DB) error {
		if err := s.ctx.Err(); err != nil {
			return err
		}
		err := sink.DeliverInternalReminderInTransaction(s.ctx, tx, envelope)
		if ctxErr := s.ctx.Err(); ctxErr != nil {
			return errors.Join(err, ctxErr)
		}
		if !envelope.Authorization.ExpiresAt.After(time.Now().UTC()) {
			return errReminderDeliveryExpired
		}
		return err
	})
}

func (r *GormRepository) FindOrCreateReminderDeliveryAuthorization(wanted *models.WorkflowReminderDeliveryAuthorization) (*models.WorkflowReminderDeliveryAuthorization, bool, error) {
	if r == nil || r.DB == nil || wanted == nil {
		return nil, false, fmt.Errorf("reminder delivery authorization is required")
	}
	stored := models.WorkflowReminderDeliveryAuthorization{}
	created := false
	err := r.DB.Transaction(func(tx *gorm.DB) error {
		activation, latest, err := loadReminderActivationRequest(tx, wanted.OwnerIdentity, wanted.ActivationRequestID, true)
		if err != nil {
			return err
		}
		if activation == nil || latest == nil || latest.ID != wanted.ActivationDecisionID || latest.Decision != ReminderActivationDecisionApproved ||
			activation.RecordDigest != wanted.ActivationRequestDigest || latest.RecordDigest != wanted.ActivationDecisionDigest {
			return fmt.Errorf("reminder delivery authorization lost its approval precondition")
		}
		source, err := loadReminderActivationSource(tx, wanted.OwnerIdentity, wanted.ChecklistItemID, true)
		if err != nil || source == nil {
			return firstReminderActivationError(err, "reminder delivery source is unavailable")
		}
		if digest, digestErr := reminderEvidenceDigest(*source); digestErr != nil || digest != wanted.ReminderDigest {
			return fmt.Errorf("reminder delivery source changed")
		}
		sourceBound := models.WorkflowReminderDeliveryAuthorization{}
		err = tx.Where(
			"owner_identity = ? AND activation_request_id = ? AND activation_decision_id = ? AND channel = ?",
			wanted.OwnerIdentity, wanted.ActivationRequestID, wanted.ActivationDecisionID, wanted.Channel,
		).First(&sourceBound).Error
		if err == nil {
			if sourceBound.IdempotencyKey != wanted.IdempotencyKey || sourceBound.RequestDigest != wanted.RequestDigest {
				return fmt.Errorf("approved reminder preparation already has a different delivery authorization")
			}
			stored = sourceBound
			return nil
		}
		if err != gorm.ErrRecordNotFound {
			return err
		}
		result := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "owner_identity"}, {Name: "idempotency_key"}}, DoNothing: true}).Create(wanted)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 1 {
			stored = *wanted
			created = true
			return nil
		}
		if err := tx.Where("owner_identity = ? AND idempotency_key = ?", wanted.OwnerIdentity, wanted.IdempotencyKey).First(&stored).Error; err != nil {
			return err
		}
		if stored.RequestDigest != wanted.RequestDigest ||
			stored.ActivationRequestID != wanted.ActivationRequestID ||
			stored.ActivationDecisionID != wanted.ActivationDecisionID ||
			stored.ReminderDigest != wanted.ReminderDigest ||
			stored.Channel != wanted.Channel {
			return fmt.Errorf("reminder delivery idempotency key is bound to different evidence")
		}
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	return &stored, created, nil
}

func (r *GormRepository) FindDueReminderDeliveryAuthorizations(owner string, now time.Time, limit, maxAttempts int) ([]reminderDeliveryCandidate, error) {
	if r == nil || r.DB == nil || limit < 1 || maxAttempts < 1 {
		return nil, fmt.Errorf("valid reminder delivery query is required")
	}
	type row struct {
		models.WorkflowReminderDeliveryAuthorization
		AttemptCount int `gorm:"column:attempt_count"`
	}
	rows := []row{}
	query := r.DB.Table("workflow_reminder_delivery_authorizations AS authz").
		Select("authz.*, COUNT(attempt.id) AS attempt_count").
		Joins("LEFT JOIN workflow_reminder_delivery_attempts AS attempt ON attempt.authorization_id = authz.id").
		Where("authz.reminder_at <= ?", now).
		Where("NOT EXISTS (SELECT 1 FROM workflow_reminder_delivery_attempts final_attempt WHERE final_attempt.authorization_id = authz.id AND final_attempt.status IN ?)", []string{ReminderDeliveryStatusDelivered, ReminderDeliveryStatusSuppressed, ReminderDeliveryStatusDeadLettered, ReminderDeliveryStatusExpired}).
		Group("authz.id").Having("COUNT(attempt.id) < ?", maxAttempts).
		Order("authz.reminder_at ASC, authz.id ASC").Limit(limit)
	if strings.TrimSpace(owner) != "" {
		query = query.Where("authz.owner_identity = ?", strings.TrimSpace(owner))
	}
	if err := query.Scan(&rows).Error; err != nil {
		return nil, err
	}
	result := make([]reminderDeliveryCandidate, 0, len(rows))
	for _, value := range rows {
		result = append(result, reminderDeliveryCandidate{Authorization: value.WorkflowReminderDeliveryAuthorization, AttemptCount: value.AttemptCount})
	}
	return result, nil
}

func (r *GormRepository) SaveReminderDeliveryAttempt(wanted *models.WorkflowReminderDeliveryAttempt) (*models.WorkflowReminderDeliveryAttempt, bool, error) {
	if r == nil || r.DB == nil || wanted == nil {
		return nil, false, fmt.Errorf("reminder delivery attempt is required")
	}
	stored := models.WorkflowReminderDeliveryAttempt{}
	result := r.DB.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "authorization_id"}, {Name: "attempt_number"}}, DoNothing: true}).Create(wanted)
	if result.Error != nil {
		return nil, false, result.Error
	}
	if result.RowsAffected == 1 {
		copy := *wanted
		return &copy, true, nil
	}
	if err := r.DB.Where("authorization_id = ? AND attempt_number = ?", wanted.AuthorizationID, wanted.AttemptNumber).First(&stored).Error; err != nil {
		return nil, false, err
	}
	if stored.Status != wanted.Status || stored.Reason != wanted.Reason ||
		stored.ReminderDigest != wanted.ReminderDigest ||
		stored.AuthorizationDigest != wanted.AuthorizationDigest ||
		stored.Authority != wanted.Authority {
		return nil, false, fmt.Errorf("reminder delivery attempt number is bound to different evidence")
	}
	return &stored, false, nil
}

func (r *GormRepository) ListReminderDeliveryAuthorizationsForOwner(owner string, limit int) ([]models.WorkflowReminderDeliveryAuthorization, error) {
	items := []models.WorkflowReminderDeliveryAuthorization{}
	err := r.DB.Where("owner_identity = ?", strings.TrimSpace(owner)).Order("authorized_at DESC, id DESC").Limit(limit).Find(&items).Error
	return items, err
}

func (r *GormRepository) ListReminderDeliveryAttemptsForOwner(owner string, limit int) ([]models.WorkflowReminderDeliveryAttempt, error) {
	items := []models.WorkflowReminderDeliveryAttempt{}
	err := r.DB.Where("owner_identity = ?", strings.TrimSpace(owner)).Order("attempted_at DESC, id DESC").Limit(limit).Find(&items).Error
	return items, err
}
