package workflow

import (
	"fmt"
	"strings"
	"time"

	"automation-hub-backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (r *GormRepository) LoadReminderActivationSourceForOwner(
	ownerIdentity string,
	checklistItemID uuid.UUID,
) (*WorkflowReminderCandidate, error) {
	if r == nil || r.DB == nil {
		return nil, fmt.Errorf("reminder activation repository is unavailable")
	}
	return loadReminderActivationSource(r.DB, ownerIdentity, checklistItemID, false)
}

func loadReminderActivationSource(
	db *gorm.DB,
	ownerIdentity string,
	checklistItemID uuid.UUID,
	lock bool,
) (*WorkflowReminderCandidate, error) {
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	if db == nil || ownerIdentity == "" || checklistItemID == uuid.Nil {
		return nil, fmt.Errorf("valid reminder activation source identity is required")
	}
	checklistQuery := db.Table("workflow_checklist_items AS reminder_checklist").
		Select("reminder_checklist.*").
		Joins("JOIN workflow_items AS reminder_workflow ON reminder_workflow.id = reminder_checklist.workflow_id").
		Where("reminder_checklist.id = ? AND reminder_workflow.owner_identity = ?", checklistItemID, ownerIdentity)
	if lock {
		checklistQuery = checklistQuery.Clauses(clause.Locking{
			Strength: "UPDATE",
			Table:    clause.Table{Name: "reminder_checklist"},
		})
	}
	var checklist models.WorkflowChecklistItem
	if err := checklistQuery.First(&checklist).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	workflowQuery := db.Where("id = ? AND owner_identity = ?", checklist.WorkflowID, ownerIdentity)
	if lock {
		workflowQuery = workflowQuery.Clauses(clause.Locking{Strength: "SHARE"})
	}
	var item models.WorkflowItem
	if err := workflowQuery.First(&item).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	if item.Archived || item.CurrentState == StateCompleted || item.CurrentState == StateArchived ||
		checklist.Status != "open" || checklist.ReminderAt == nil {
		return nil, nil
	}
	return &WorkflowReminderCandidate{Workflow: item, Reminder: checklist}, nil
}

func (r *GormRepository) FindOrCreateReminderActivationRequest(
	wanted *models.WorkflowReminderActivationRequest,
) (*models.WorkflowReminderActivationRequest, bool, error) {
	if r == nil || r.DB == nil || wanted == nil {
		return nil, false, fmt.Errorf("reminder activation request evidence is required")
	}
	if err := validateReminderActivationRequest(wanted); err != nil {
		return nil, false, err
	}
	stored := models.WorkflowReminderActivationRequest{}
	created := false
	err := r.DB.Transaction(func(tx *gorm.DB) error {
		source, err := loadReminderActivationSource(tx, wanted.OwnerIdentity, wanted.ChecklistItemID, true)
		if err != nil {
			return err
		}
		if source == nil {
			return fmt.Errorf("reminder changed or is unavailable")
		}
		if err := validateReminderActivationRequestSource(wanted, source); err != nil {
			return err
		}
		result := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "owner_identity"}, {Name: "idempotency_key"}},
			DoNothing: true,
		}).Create(wanted)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 1 {
			stored = *wanted
			created = true
			return nil
		}
		if err := tx.Where(
			"owner_identity = ? AND idempotency_key = ?", wanted.OwnerIdentity, wanted.IdempotencyKey,
		).First(&stored).Error; err != nil {
			return err
		}
		if stored.RequestDigest != wanted.RequestDigest || stored.ReminderDigest != wanted.ReminderDigest {
			return fmt.Errorf("reminder activation idempotency key is already bound to different evidence")
		}
		return validateReminderActivationRequest(&stored)
	})
	if err != nil {
		return nil, false, err
	}
	return &stored, created, nil
}

func validateReminderActivationRequestSource(
	request *models.WorkflowReminderActivationRequest,
	source *WorkflowReminderCandidate,
) error {
	if request == nil || source == nil || request.WorkflowID != source.Workflow.ID ||
		request.ChecklistItemID != source.Reminder.ID || request.OwnerIdentity != source.Workflow.OwnerIdentity ||
		request.WorkflowState != source.Workflow.CurrentState || request.ChecklistStatus != source.Reminder.Status ||
		source.Reminder.ReminderAt == nil || !request.ReminderAt.Equal(source.Reminder.ReminderAt.UTC()) ||
		!sameReminderTime(request.DueAt, source.Reminder.DueAt) {
		return fmt.Errorf("reminder activation request crossed its source boundary")
	}
	digest, err := reminderEvidenceDigest(*source)
	if err != nil || digest != request.ReminderDigest {
		return fmt.Errorf("reminder activation request source digest changed")
	}
	return nil
}

func sameReminderTime(left, right *time.Time) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(right.UTC())
}

func (r *GormRepository) ListReminderActivationRequestsForOwner(
	ownerIdentity string,
	limit int,
) ([]models.WorkflowReminderActivationRequest, error) {
	if r == nil || r.DB == nil || strings.TrimSpace(ownerIdentity) == "" || limit < 1 || limit > 100 {
		return nil, fmt.Errorf("valid reminder activation owner and history limit are required")
	}
	records := []models.WorkflowReminderActivationRequest{}
	if err := r.DB.Where("owner_identity = ?", strings.TrimSpace(ownerIdentity)).
		Order("requested_at DESC, id DESC").Limit(limit).Find(&records).Error; err != nil {
		return nil, err
	}
	return records, nil
}

func (r *GormRepository) LoadReminderActivationRequestForOwner(
	ownerIdentity string,
	requestID uuid.UUID,
) (*models.WorkflowReminderActivationRequest, *models.WorkflowReminderActivationDecision, error) {
	if r == nil || r.DB == nil {
		return nil, nil, fmt.Errorf("reminder activation repository is unavailable")
	}
	return loadReminderActivationRequest(r.DB, ownerIdentity, requestID, false)
}

func loadReminderActivationRequest(
	db *gorm.DB,
	ownerIdentity string,
	requestID uuid.UUID,
	lock bool,
) (*models.WorkflowReminderActivationRequest, *models.WorkflowReminderActivationDecision, error) {
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	if db == nil || ownerIdentity == "" || requestID == uuid.Nil {
		return nil, nil, fmt.Errorf("valid reminder activation request identity is required")
	}
	query := db.Where("id = ? AND owner_identity = ?", requestID, ownerIdentity)
	if lock {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var request models.WorkflowReminderActivationRequest
	if err := query.First(&request).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil, nil
		}
		return nil, nil, err
	}
	latestQuery := db.Where("activation_request_id = ? AND owner_identity = ?", requestID, ownerIdentity).
		Order("decided_at DESC, id DESC")
	if lock {
		latestQuery = latestQuery.Clauses(clause.Locking{Strength: "SHARE"})
	}
	var latest models.WorkflowReminderActivationDecision
	err := latestQuery.First(&latest).Error
	if err != nil && err != gorm.ErrRecordNotFound {
		return nil, nil, err
	}
	if err == gorm.ErrRecordNotFound {
		return &request, nil, nil
	}
	return &request, &latest, nil
}

func (r *GormRepository) SaveReminderActivationDecision(
	wanted *models.WorkflowReminderActivationDecision,
) (*models.WorkflowReminderActivationDecision, bool, error) {
	if r == nil || r.DB == nil || wanted == nil {
		return nil, false, fmt.Errorf("reminder activation decision evidence is required")
	}
	stored := models.WorkflowReminderActivationDecision{}
	created := false
	err := r.DB.Transaction(func(tx *gorm.DB) error {
		activation, latest, err := loadReminderActivationRequest(tx, wanted.OwnerIdentity, wanted.ActivationRequestID, true)
		if err != nil {
			return err
		}
		if activation == nil || activation.RecordDigest != wanted.ActivationRequestDigest {
			return fmt.Errorf("reminder activation request changed or is unavailable")
		}
		if err := validateReminderActivationRequest(activation); err != nil {
			return err
		}
		if wanted.Decision != ReminderActivationDecisionRevoked {
			if time.Now().UTC().After(activation.ExpiresAt) {
				return fmt.Errorf("reminder activation request expired")
			}
			source, err := loadReminderActivationSource(tx, wanted.OwnerIdentity, activation.ChecklistItemID, true)
			if err != nil {
				return err
			}
			if source == nil {
				return fmt.Errorf("reminder is no longer current")
			}
			if err := validateReminderActivationRequestSource(activation, source); err != nil {
				return err
			}
		}
		var replay models.WorkflowReminderActivationDecision
		replayErr := tx.Where(
			"owner_identity = ? AND activation_request_id = ? AND request_digest = ?",
			wanted.OwnerIdentity, wanted.ActivationRequestID, wanted.RequestDigest,
		).First(&replay).Error
		if replayErr == nil {
			stored = replay
			return validateReminderActivationDecision(activation, &stored)
		}
		if replayErr != gorm.ErrRecordNotFound {
			return replayErr
		}
		currentPrevious := uuid.Nil
		if latest != nil {
			currentPrevious = latest.ID
		}
		wantedPrevious := uuid.Nil
		if wanted.PreviousDecisionID != nil {
			wantedPrevious = *wanted.PreviousDecisionID
		}
		if currentPrevious != wantedPrevious {
			return fmt.Errorf("reminder activation decision chain changed; retry from current history")
		}
		if wanted.Decision == ReminderActivationDecisionRevoked &&
			(latest == nil || latest.Decision != ReminderActivationDecisionApproved) {
			return fmt.Errorf("only the latest approved reminder preparation can be revoked")
		}
		if err := validateReminderActivationDecision(activation, wanted); err != nil {
			return err
		}
		if err := tx.Create(wanted).Error; err != nil {
			return err
		}
		stored = *wanted
		created = true
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	return &stored, created, nil
}

func (r *GormRepository) ListReminderActivationDecisionsForOwner(
	ownerIdentity string,
	requestID uuid.UUID,
	limit int,
) ([]models.WorkflowReminderActivationDecision, error) {
	if r == nil || r.DB == nil || strings.TrimSpace(ownerIdentity) == "" ||
		requestID == uuid.Nil || limit < 1 || limit > 100 {
		return nil, fmt.Errorf("valid reminder activation decision history scope is required")
	}
	records := []models.WorkflowReminderActivationDecision{}
	if err := r.DB.Where(
		"owner_identity = ? AND activation_request_id = ?", strings.TrimSpace(ownerIdentity), requestID,
	).Order("decided_at DESC, id DESC").Limit(limit).Find(&records).Error; err != nil {
		return nil, err
	}
	return records, nil
}
