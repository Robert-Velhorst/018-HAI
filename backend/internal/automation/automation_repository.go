package automation

import (
	"automation-hub-backend/internal/events"
	"automation-hub-backend/internal/infra"
	"automation-hub-backend/internal/models"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type Repository interface {
	FindByID(id uuid.UUID) (*models.Automation, error)
	Create(automation *models.Automation) (*models.Automation, error)
	Update(automation *models.Automation) (*models.Automation, error)
	Delete(id uuid.UUID) error
	FindAll() ([]*models.Automation, error)
	MaxPosition() (int, error)
	GetByURLPath(urlPath string) (*models.Automation, error)
	Transaction(txFunc func(tx *gorm.DB) error) (err error)
	SaveHealthEvent(event *models.AutomationHealthEvent) error
	FindHealthEvents(automationID uuid.UUID, limit int) ([]models.AutomationHealthEvent, error)
	SaveLaunchIntent(event *models.AutomationLaunchEvent) error
	SaveLaunchEvent(event *models.AutomationLaunchEvent) error
	FindLaunchEvents(automationID uuid.UUID, limit int) ([]models.AutomationLaunchEvent, error)
	SaveApprovalDecision(record *ApprovalDecisionRecord) error
	FindApprovalDecision(sourceID string) (*ApprovalDecisionRecord, error)
}

// DurableEventRepository commits an automation mutation and its event-delivery
// intent in one database transaction. Production repositories must implement
// this interface; the narrower Repository remains useful for deterministic
// unit tests and non-database adapters.
type DurableEventRepository interface {
	CreateWithEvent(automation *models.Automation, event *events.AutomationEvent) (*models.Automation, error)
	UpdateWithEvent(automation *models.Automation, event *events.AutomationEvent) (*models.Automation, error)
	DeleteWithEvent(id uuid.UUID, event *events.AutomationEvent) error
}

type GormUserRepository struct {
	DB             *gorm.DB
	notifyDispatch func()
}

func NewGormUserRepository(db *gorm.DB) Repository {
	return &GormUserRepository{
		DB: db,
	}
}

func NewGormUserRepositoryWithNotifier(db *gorm.DB, notify func()) Repository {
	return &GormUserRepository{DB: db, notifyDispatch: notify}
}

func DefaultRepository() Repository {
	db, err := infra.GetDefaultDB()
	if err != nil {
		panic(err)
	}
	return NewGormUserRepository(db)
}

func (r *GormUserRepository) FindByID(id uuid.UUID) (*models.Automation, error) {
	var automation models.Automation
	err := r.DB.First(&automation, "id = ?", id).Error
	if err != nil {
		return nil, err
	}
	return &automation, nil
}

func (r *GormUserRepository) Create(automation *models.Automation) (*models.Automation, error) {
	err := r.DB.Create(automation).Error
	if err != nil {
		return nil, err
	}
	return automation, nil
}

func (r *GormUserRepository) CreateWithEvent(automation *models.Automation, event *events.AutomationEvent) (*models.Automation, error) {
	err := r.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(automation).Error; err != nil {
			return err
		}
		event.Automation = automation
		return events.EnqueueTx(tx, event)
	})
	if err != nil {
		return nil, err
	}
	r.notifyEventDispatch()
	return automation, nil
}

func (r *GormUserRepository) Update(automation *models.Automation) (*models.Automation, error) {
	err := r.DB.Save(automation).Error
	if err != nil {
		return nil, err
	}
	return automation, nil
}

func (r *GormUserRepository) UpdateWithEvent(automation *models.Automation, event *events.AutomationEvent) (*models.Automation, error) {
	err := r.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(automation).Error; err != nil {
			return err
		}
		event.Automation = automation
		return events.EnqueueTx(tx, event)
	})
	if err != nil {
		return nil, err
	}
	r.notifyEventDispatch()
	return automation, nil
}

func (r *GormUserRepository) Delete(id uuid.UUID) error {
	err := r.DB.Delete(&models.Automation{}, id).Error
	if err != nil {
		return err
	}
	return nil
}

func (r *GormUserRepository) DeleteWithEvent(id uuid.UUID, event *events.AutomationEvent) error {
	err := r.DB.Transaction(func(tx *gorm.DB) error {
		result := tx.Delete(&models.Automation{}, id)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return gorm.ErrRecordNotFound
		}
		return events.EnqueueTx(tx, event)
	})
	if err != nil {
		return err
	}
	r.notifyEventDispatch()
	return nil
}

func (r *GormUserRepository) notifyEventDispatch() {
	if r != nil && r.notifyDispatch != nil {
		r.notifyDispatch()
	}
}

func (r *GormUserRepository) FindAll() ([]*models.Automation, error) {
	var automations []*models.Automation
	err := r.DB.Order("position asc").Find(&automations).Error
	if err != nil {
		return nil, err
	}
	return automations, nil
}

func (r *GormUserRepository) Transaction(txFunc func(tx *gorm.DB) error) (err error) {
	tx := r.DB.Begin()
	if tx.Error != nil {
		return tx.Error
	}

	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
			err = fmt.Errorf("transaction panicked: %v", r)
		} else if err != nil {
			tx.Rollback()
		} else {
			err = tx.Commit().Error
		}
	}()

	err = txFunc(tx)
	return err
}

func (r *GormUserRepository) MaxPosition() (int, error) {
	var automation models.Automation
	err := r.DB.Order("position desc").First(&automation).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, nil
		}
		return 0, err
	}
	return automation.Position, nil
}

func (r *GormUserRepository) SaveHealthEvent(event *models.AutomationHealthEvent) error {
	return r.DB.Create(event).Error
}

func (r *GormUserRepository) FindHealthEvents(automationID uuid.UUID, limit int) ([]models.AutomationHealthEvent, error) {
	var events []models.AutomationHealthEvent
	if limit <= 0 {
		limit = 20
	}
	err := r.DB.
		Where("automation_id = ?", automationID).
		Order("checked_at desc").
		Limit(limit).
		Find(&events).Error
	if err != nil {
		return nil, err
	}
	return events, nil
}

func (r *GormUserRepository) SaveLaunchEvent(event *models.AutomationLaunchEvent) error {
	return r.DB.Create(event).Error
}

func (r *GormUserRepository) SaveLaunchIntent(event *models.AutomationLaunchEvent) error {
	if event == nil ||
		!strings.HasSuffix(strings.TrimSpace(event.LaunchType), "_intent") ||
		strings.TrimSpace(event.Status) != "pending" {
		return fmt.Errorf("launch intent must be an immutable pending intent event")
	}
	return r.DB.Create(event).Error
}

func (r *GormUserRepository) FindLaunchEvents(automationID uuid.UUID, limit int) ([]models.AutomationLaunchEvent, error) {
	var events []models.AutomationLaunchEvent
	if limit <= 0 {
		limit = 20
	}
	err := r.DB.
		Where("automation_id = ?", automationID).
		Where("launch_type <> ? AND launch_type NOT LIKE ?", "approval_decision", "%_intent").
		Order("started_at desc").
		Limit(limit).
		Find(&events).Error
	if err != nil {
		return nil, err
	}
	return events, nil
}

func (r *GormUserRepository) SaveApprovalDecision(record *ApprovalDecisionRecord) error {
	if err := validateApprovalDecisionRecord(record); err != nil {
		return err
	}
	kind, sourceUUID, err := approvalSourceKind(record.SourceID)
	if err != nil {
		return err
	}
	if kind != "task-review" {
		return fmt.Errorf("only task-review decisions can be registered through the automation service")
	}
	if existing, findErr := r.FindApprovalDecision(record.SourceID); findErr == nil {
		if sameApprovalDecision(existing, record) {
			return nil
		}
		return fmt.Errorf("approval decision conflicts with the recorded action binding")
	} else if !errors.Is(findErr, gorm.ErrRecordNotFound) {
		return findErr
	}
	event := &models.AutomationLaunchEvent{
		ID:            sourceUUID,
		AutomationID:  record.AutomationID,
		OwnerIdentity: record.OwnerIdentity,
		RuntimeType:   string(record.Scope),
		LaunchType:    "approval_decision",
		Target:        record.ActionDigest,
		Status:        "approved",
		Message:       "owner-scoped task review approved one exact automation action",
		AuditEvents: []string{
			"task review decision verified before registration",
			"approval action digest recorded",
		},
		StartedAt:   record.ApprovedAt.UTC(),
		CompletedAt: record.ApprovedAt.UTC(),
	}
	return r.DB.Create(event).Error
}

func (r *GormUserRepository) FindApprovalDecision(sourceID string) (*ApprovalDecisionRecord, error) {
	kind, sourceUUID, err := approvalSourceKind(sourceID)
	if err != nil {
		return nil, err
	}
	switch kind {
	case "task-review":
		var event models.AutomationLaunchEvent
		err := r.DB.
			Where(
				"id = ? AND launch_type = ? AND status = ?",
				sourceUUID,
				"approval_decision",
				"approved",
			).
			First(&event).Error
		if err != nil {
			return nil, err
		}
		record := &ApprovalDecisionRecord{
			SourceID:      sourceID,
			DecisionType:  kind,
			OwnerIdentity: event.OwnerIdentity,
			AutomationID:  event.AutomationID,
			ActionDigest:  event.Target,
			Scope:         ApprovalScope(event.RuntimeType),
			ApprovedAt:    event.StartedAt,
		}
		if err := validateApprovalDecisionRecord(record); err != nil {
			return nil, err
		}
		return record, nil
	case "workflow-decision":
		var row struct {
			WorkflowID    uuid.UUID
			OwnerIdentity string
			AutomationID  string
			RuleApplied   string
			CreatedAt     time.Time
		}
		err := r.DB.
			Table("workflow_decisions AS decisions").
			Select(
				"decisions.workflow_id, items.owner_identity, items.automation_id, decisions.rule_applied, decisions.created_at",
			).
			Joins("JOIN workflow_items AS items ON items.id = decisions.workflow_id").
			Where(
				"decisions.id = ? AND decisions.decision_type = ? AND decisions.decision = ? AND decisions.approved = ?",
				sourceUUID,
				"approval",
				"approved",
				true,
			).
			Where("items.requires_approval = ? AND items.approval_status = ?", true, "approved").
			Take(&row).Error
		if err != nil {
			return nil, err
		}
		automationID, err := uuid.Parse(row.AutomationID)
		if err != nil {
			return nil, fmt.Errorf("workflow approval decision has an invalid automation binding")
		}
		scope, digest, err := parseWorkflowApprovalBinding(row.RuleApplied)
		if err != nil {
			return nil, err
		}
		record := &ApprovalDecisionRecord{
			SourceID:      sourceID,
			DecisionType:  kind,
			OwnerIdentity: row.OwnerIdentity,
			AutomationID:  automationID,
			WorkflowID:    row.WorkflowID,
			ActionDigest:  digest,
			Scope:         scope,
			ApprovedAt:    row.CreatedAt,
		}
		if err := validateApprovalDecisionRecord(record); err != nil {
			return nil, err
		}
		return record, nil
	default:
		return nil, ErrApprovalDecisionMissing
	}
}

func (r *GormUserRepository) GetByURLPath(urlPath string) (*models.Automation, error) {
	var automation models.Automation
	err := r.DB.First(&automation, "url_path = ?", urlPath).Error
	if err != nil {
		return nil, err
	}
	return &automation, nil
}
