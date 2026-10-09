package automation

import (
	"automation-hub-backend/internal/infra"
	"automation-hub-backend/internal/models"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	errLaunchIntentAlreadyExists = errors.New("launch intent already exists")
	errLaunchEventKeyConflict    = errors.New("launch event key already has a different outcome")
)

type Repository interface {
	FindByID(id uuid.UUID) (*models.Automation, error)
	Create(automation *models.Automation) (*models.Automation, error)
	Update(automation *models.Automation) (*models.Automation, error)
	UpdateLaunchState(id uuid.UUID, startedAt time.Time, failureReason *string) error
	UpdateRuntimeStopFailure(id uuid.UUID, startedAt time.Time, reason string) error
	Delete(id uuid.UUID) error
	FindAll() ([]*models.Automation, error)
	MaxPosition() (int, error)
	GetByURLPath(urlPath string) (*models.Automation, error)
	Transaction(txFunc func(tx *gorm.DB) error) (err error)
	SaveHealthEvent(event *models.AutomationHealthEvent) error
	FindHealthEvents(automationID uuid.UUID, limit int) ([]models.AutomationHealthEvent, error)
	SaveLaunchIntent(event *models.AutomationLaunchEvent) error
	SaveLaunchEvent(event *models.AutomationLaunchEvent) error
	FindLaunchIntentByEventKey(eventKey string) (*models.AutomationLaunchEvent, error)
	FindLaunchOutcomeByIntentID(intentID uuid.UUID) (*models.AutomationLaunchEvent, error)
	FindLaunchEvents(automationID uuid.UUID, limit int) ([]models.AutomationLaunchEvent, error)
	FindOwnerLaunchEvents(automationID uuid.UUID, owner string, limit int) ([]models.AutomationLaunchEvent, error)
	FindOwnerActiveRuntimeLaunch(automationID uuid.UUID, runtimeID, owner string) (*models.AutomationLaunchEvent, error)
	FindPendingRuntimeLaunchIntent(automationID uuid.UUID, owner string) (*models.AutomationLaunchEvent, error)
	FindUnresolvedRuntimeStopIntent(automationID uuid.UUID, owner, taskID string) (*models.AutomationLaunchEvent, error)
	FindLaunchEventByExecutionReference(reference string) (*models.AutomationLaunchEvent, error)
	FindLaunchIntentByExecutionReference(reference string) (*models.AutomationLaunchEvent, error)
	SaveApprovalDecision(record *ApprovalDecisionRecord) error
	FindApprovalDecision(sourceID string) (*ApprovalDecisionRecord, error)
}

type GormUserRepository struct {
	DB *gorm.DB
}

func NewGormUserRepository(db *gorm.DB) Repository {
	return &GormUserRepository{
		DB: db,
	}
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

func (r *GormUserRepository) Update(automation *models.Automation) (*models.Automation, error) {
	err := r.DB.Save(automation).Error
	if err != nil {
		return nil, err
	}
	return automation, nil
}

func (r *GormUserRepository) UpdateRuntimeStopFailure(id uuid.UUID, startedAt time.Time, reason string) error {
	if id == uuid.Nil || startedAt.IsZero() {
		return fmt.Errorf("runtime stop failure summary requires automation ID and start time")
	}
	if r == nil || r.DB == nil || r.DB.Config == nil || r.DB.DryRun || r.DB.Statement == nil || r.DB.Statement.Context == nil {
		return fmt.Errorf("runtime stop failure summary requires available non-dry-run storage")
	}
	ctx, cancel := context.WithTimeout(r.DB.Statement.Context, approvalRegistrationTimeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return err
	}
	scoped := r.DB.WithContext(ctx)
	result := scoped.Model(&models.Automation{}).
		Where("id = ? AND (last_launch_at IS NULL OR last_launch_at <= ?)", id, startedAt.UTC()).
		Update("last_failure_reason", reason)
	if err := errors.Join(ctx.Err(), result.Error); err != nil {
		return err
	}
	if result.RowsAffected > 0 {
		return nil
	}
	// A newer launch supersedes this summary. A deleted automation does not.
	var existing models.Automation
	err := scoped.Select("id").First(&existing, "id = ?", id).Error
	return errors.Join(ctx.Err(), err)
}

func (r *GormUserRepository) UpdateLaunchState(id uuid.UUID, startedAt time.Time, failureReason *string) error {
	if id == uuid.Nil || startedAt.IsZero() {
		return fmt.Errorf("automation launch state requires an automation ID and start time")
	}

	updates := map[string]interface{}{"last_launch_at": startedAt.UTC()}
	if failureReason != nil {
		updates["last_failure_reason"] = *failureReason
	}
	result := r.DB.Model(&models.Automation{}).
		Where("id = ? AND (last_launch_at IS NULL OR last_launch_at <= ?)", id, startedAt.UTC()).
		Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected > 0 {
		return nil
	}

	// A zero-row update also means this launch is older than the projection
	// already stored. Distinguish that harmless stale write from a deleted row.
	var existing models.Automation
	return r.DB.Select("id").First(&existing, "id = ?", id).Error
}

func (r *GormUserRepository) Delete(id uuid.UUID) error {
	err := r.DB.Delete(&models.Automation{}, id).Error
	if err != nil {
		return err
	}
	return nil
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
	if event == nil {
		return fmt.Errorf("launch event is required")
	}
	if key := strings.TrimSpace(event.EventKey); key != "" {
		// The partial unique index introduced by migration 0067 is the authority
		// for event-key idempotency. Resolve a conflict to its durable row instead
		// of returning success for an event that was never inserted.
		result := r.DB.Clauses(clause.OnConflict{DoNothing: true}).Create(event)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected > 0 {
			return nil
		}

		var existing models.AutomationLaunchEvent
		if err := r.DB.Where("event_key = ?", event.EventKey).First(&existing).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errLaunchEventKeyConflict
			}
			return err
		}
		if !sameLaunchEventOutcome(&existing, event) {
			return errLaunchEventKeyConflict
		}
		event.ID = existing.ID
		return nil
	}
	return r.DB.Create(event).Error
}

func sameLaunchEventOutcome(existing, candidate *models.AutomationLaunchEvent) bool {
	if existing == nil || candidate == nil {
		return false
	}
	return existing.AutomationID == candidate.AutomationID &&
		existing.OwnerIdentity == candidate.OwnerIdentity &&
		existing.RuntimeType == candidate.RuntimeType &&
		existing.LaunchType == candidate.LaunchType &&
		existing.RuntimeTaskID == candidate.RuntimeTaskID &&
		existing.ExecutionReference == candidate.ExecutionReference &&
		existing.EventKey == candidate.EventKey &&
		existing.Target == candidate.Target &&
		existing.Status == candidate.Status &&
		existing.Message == candidate.Message &&
		existing.Output == candidate.Output &&
		existing.AuditLog == candidate.AuditLog &&
		existing.RuntimeRouteTraceLog == candidate.RuntimeRouteTraceLog &&
		existing.ExitCode == candidate.ExitCode &&
		existing.DurationMs == candidate.DurationMs &&
		existing.RequiresApproval == candidate.RequiresApproval &&
		samePersistedLaunchTime(existing.StartedAt, candidate.StartedAt) &&
		samePersistedLaunchTime(existing.CompletedAt, candidate.CompletedAt)
}

func samePersistedLaunchTime(existing, candidate time.Time) bool {
	return existing.Truncate(time.Microsecond).Equal(candidate.Truncate(time.Microsecond))
}

func (r *GormUserRepository) SaveLaunchIntent(event *models.AutomationLaunchEvent) error {
	if event == nil ||
		!strings.HasSuffix(strings.TrimSpace(event.LaunchType), "_intent") ||
		strings.TrimSpace(event.Status) != "pending" {
		return fmt.Errorf("launch intent must be an immutable pending intent event")
	}
	if strings.TrimSpace(event.EventKey) == "" {
		return r.DB.Create(event).Error
	}
	result := r.DB.Clauses(clause.OnConflict{DoNothing: true}).Create(event)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return errLaunchIntentAlreadyExists
	}
	return nil
}

func (r *GormUserRepository) FindLaunchIntentByEventKey(eventKey string) (*models.AutomationLaunchEvent, error) {
	eventKey = strings.TrimSpace(eventKey)
	if eventKey == "" {
		return nil, fmt.Errorf("launch intent event key is required")
	}
	var event models.AutomationLaunchEvent
	err := r.DB.Where("event_key = ?", eventKey).First(&event).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !strings.HasSuffix(strings.TrimSpace(event.LaunchType), "_intent") {
		return nil, fmt.Errorf("event key is already assigned to a non-intent launch event")
	}
	return &event, nil
}

func (r *GormUserRepository) FindLaunchOutcomeByIntentID(intentID uuid.UUID) (*models.AutomationLaunchEvent, error) {
	if intentID == uuid.Nil {
		return nil, fmt.Errorf("launch intent ID is required")
	}
	var outcome *models.AutomationLaunchEvent
	var event models.AutomationLaunchEvent
	err := r.DB.
		Where("event_key IN ?", []string{automationLaunchOutcomeEventKey(intentID), runtimeStopOutcomeEventKey(intentID)}).
		Order("completed_at DESC").
		First(&event).Error
	if err == nil {
		if strings.HasSuffix(strings.TrimSpace(event.LaunchType), "_intent") {
			return nil, fmt.Errorf("launch outcome key is assigned to an intent")
		}
		outcome = &event
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	var intent models.AutomationLaunchEvent
	err = r.DB.Where("id = ?", intentID).First(&intent).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return outcome, nil
	}
	if err != nil {
		return nil, err
	}
	if intent.LaunchType != "agent_runtime_intent" {
		return outcome, nil
	}
	reconciled, err := r.findReconciledRuntimeOutcome(&intent)
	if err != nil {
		return nil, err
	}
	if reconciled != nil {
		return reconciled, nil
	}
	return outcome, nil
}

func (r *GormUserRepository) findReconciledRuntimeOutcome(intent *models.AutomationLaunchEvent) (*models.AutomationLaunchEvent, error) {
	if intent == nil || intent.AutomationID == uuid.Nil || strings.TrimSpace(intent.OwnerIdentity) == "" ||
		strings.TrimSpace(intent.RuntimeType) == "" || strings.TrimSpace(intent.RuntimeTaskID) == "" {
		return nil, nil
	}
	var event models.AutomationLaunchEvent
	err := r.DB.
		Where("automation_id = ? AND owner_identity = ? AND LOWER(runtime_type) = ? AND runtime_task_id = ?",
			intent.AutomationID, strings.TrimSpace(intent.OwnerIdentity), strings.ToLower(strings.TrimSpace(intent.RuntimeType)), strings.TrimSpace(intent.RuntimeTaskID)).
		Where("launch_type IN ?", []string{"agent_runtime_openclaw_terminal", "agent_runtime_host_completion"}).
		Order("completed_at DESC").
		First(&event).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &event, nil
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

func (r *GormUserRepository) FindOwnerLaunchEvents(automationID uuid.UUID, owner string, limit int) ([]models.AutomationLaunchEvent, error) {
	if automationID == uuid.Nil || strings.TrimSpace(owner) == "" {
		return nil, fmt.Errorf("owner-bound launch history lookup requires automation and owner")
	}
	var events []models.AutomationLaunchEvent
	if limit <= 0 {
		limit = 20
	}
	err := r.DB.
		Where("automation_id = ? AND owner_identity = ?", automationID, strings.TrimSpace(owner)).
		Where("launch_type <> ? AND launch_type NOT LIKE ?", "approval_decision", "%_intent").
		Order("started_at desc").
		Limit(limit).
		Find(&events).Error
	if err != nil {
		return nil, err
	}
	return events, nil
}

func (r *GormUserRepository) FindOwnerActiveRuntimeLaunch(automationID uuid.UUID, runtimeID, owner string) (*models.AutomationLaunchEvent, error) {
	if automationID == uuid.Nil || strings.TrimSpace(owner) == "" || strings.TrimSpace(runtimeID) == "" {
		return nil, fmt.Errorf("owner-bound runtime launch lookup requires automation, runtime, and owner")
	}
	statuses := []string{"queued", "running"}
	if strings.EqualFold(strings.TrimSpace(runtimeID), "openclaw") {
		statuses = append(statuses, "indeterminate", "needs_review")
	}
	var event models.AutomationLaunchEvent
	err := r.DB.
		Where("automation_id = ? AND LOWER(runtime_type) = ? AND owner_identity = ? AND launch_type = ?", automationID, strings.ToLower(strings.TrimSpace(runtimeID)), strings.TrimSpace(owner), "agent_runtime").
		Where("status IN ?", statuses).
		Where("(status NOT IN ? OR execution_reference <> '')", []string{"indeterminate", "needs_review"}).
		Where(`NOT EXISTS (
			SELECT 1 FROM automation_launch_events AS terminal
			WHERE terminal.automation_id = automation_launch_events.automation_id
			  AND terminal.owner_identity = automation_launch_events.owner_identity
			  AND LOWER(terminal.runtime_type) = LOWER(automation_launch_events.runtime_type)
			  AND terminal.runtime_task_id = automation_launch_events.runtime_task_id
			  AND terminal.launch_type IN ('agent_runtime_openclaw_terminal', 'agent_runtime_host_completion')
		)`).
		Order("started_at DESC").
		First(&event).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &event, nil
}

func (r *GormUserRepository) FindPendingRuntimeLaunchIntent(automationID uuid.UUID, owner string) (*models.AutomationLaunchEvent, error) {
	if automationID == uuid.Nil || strings.TrimSpace(owner) == "" {
		return nil, fmt.Errorf("owner-bound pending launch lookup requires automation and owner")
	}
	var event models.AutomationLaunchEvent
	query := r.DB.Model(&models.AutomationLaunchEvent{}).
		Where("automation_id = ? AND owner_identity = ? AND launch_type = ? AND status = ?", automationID, strings.TrimSpace(owner), "agent_runtime_intent", "pending").
		// RuntimeTaskID is generated from the immutable launch-intent UUID and is
		// therefore the stable reconciliation key. Gateway references are optional
		// transport metadata and must not prevent CLI outcomes from closing intents.
		Where(`NOT EXISTS (
			SELECT 1 FROM automation_launch_events AS outcome
			WHERE outcome.automation_id = automation_launch_events.automation_id
			  AND outcome.owner_identity = automation_launch_events.owner_identity
			  AND LOWER(outcome.runtime_type) = LOWER(automation_launch_events.runtime_type)
			  AND outcome.launch_type = 'agent_runtime'
			  AND outcome.runtime_task_id = automation_launch_events.runtime_task_id
		)`).
		// Reconciliation can persist a terminal result before the ordinary launch
		// outcome. A matching terminal event is sufficient to close the intent.
		Where(`NOT EXISTS (
			SELECT 1 FROM automation_launch_events AS terminal
			WHERE terminal.automation_id = automation_launch_events.automation_id
			  AND terminal.owner_identity = automation_launch_events.owner_identity
			  AND LOWER(terminal.runtime_type) = LOWER(automation_launch_events.runtime_type)
			  AND terminal.runtime_task_id = automation_launch_events.runtime_task_id
			  AND terminal.launch_type IN ('agent_runtime_openclaw_terminal', 'agent_runtime_host_completion')
		)`).
		Order("started_at DESC")
	err := query.First(&event).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &event, nil
}

func (r *GormUserRepository) FindUnresolvedRuntimeStopIntent(automationID uuid.UUID, owner, taskID string) (*models.AutomationLaunchEvent, error) {
	if automationID == uuid.Nil || strings.TrimSpace(owner) == "" || strings.TrimSpace(taskID) == "" {
		return nil, fmt.Errorf("owner-bound runtime stop intent lookup requires automation, owner, and task")
	}
	var event models.AutomationLaunchEvent
	query := r.DB.Model(&models.AutomationLaunchEvent{}).
		Where("automation_id = ? AND owner_identity = ? AND runtime_task_id = ? AND launch_type = ? AND status = ?", automationID, strings.TrimSpace(owner), strings.TrimSpace(taskID), "agent_runtime_stop_intent", "pending").
		Where(`NOT EXISTS (
			SELECT 1 FROM automation_launch_events AS outcome
			WHERE outcome.automation_id = automation_launch_events.automation_id
			  AND outcome.owner_identity = automation_launch_events.owner_identity
			  AND outcome.runtime_task_id = automation_launch_events.runtime_task_id
			  AND outcome.launch_type = 'agent_runtime_stop'
			  AND outcome.event_key = 'runtime-stop-outcome:' || automation_launch_events.id::text
		)`).
		Order("started_at DESC")
	err := query.First(&event).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &event, nil
}

func (r *GormUserRepository) FindLaunchEventByExecutionReference(reference string) (*models.AutomationLaunchEvent, error) {
	reference = strings.TrimSpace(reference)
	if reference == "" {
		return nil, fmt.Errorf("execution reference is required")
	}
	var event models.AutomationLaunchEvent
	err := r.DB.
		Where("execution_reference = ?", reference).
		Where("launch_type = ?", "agent_runtime").
		Order("started_at DESC").
		First(&event).Error
	if err != nil {
		return nil, err
	}
	return &event, nil
}

func (r *GormUserRepository) FindLaunchIntentByExecutionReference(reference string) (*models.AutomationLaunchEvent, error) {
	reference = strings.TrimSpace(reference)
	if reference == "" {
		return nil, fmt.Errorf("execution reference is required")
	}
	var event models.AutomationLaunchEvent
	err := r.DB.
		Where("execution_reference = ? AND launch_type = ? AND status = ?", reference, "agent_runtime_intent", "pending").
		Order("started_at DESC").
		First(&event).Error
	if err != nil {
		return nil, err
	}
	return &event, nil
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
