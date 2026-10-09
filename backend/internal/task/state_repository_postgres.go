package task

import (
	"errors"
	"fmt"
	"strings"

	"automation-hub-backend/internal/infra"
	"automation-hub-backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type PostgresTaskStateRepository struct {
	DB *gorm.DB
}

func NewPostgresTaskStateRepository(db *gorm.DB) *PostgresTaskStateRepository {
	return &PostgresTaskStateRepository{DB: db}
}

func DefaultTaskStateRepository() (TaskStateRepository, error) {
	db, err := infra.GetDefaultDB()
	if err != nil {
		return nil, err
	}
	return NewPostgresTaskStateRepository(db), nil
}

func (r *PostgresTaskStateRepository) AppendCompletionPlan(ownerIdentity string, plan CompletionPlan) error {
	row, err := completionPlanToModel(ownerIdentity, plan)
	if err != nil {
		return err
	}
	return r.DB.Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "owner_identity"},
			{Name: "task_plan_id"},
			{Name: "payload_digest"},
		},
		DoNothing: true,
	}).Create(&row).Error
}

func (r *PostgresTaskStateRepository) ListCompletionPlans(ownerIdentity string, limit int) ([]CompletionPlan, error) {
	ownerIdentity, err := normalizeTaskStateOwner(ownerIdentity)
	if err != nil {
		return nil, err
	}
	var rows []models.TaskCompletionPlanLog
	if err := r.DB.
		Where("owner_identity = ?", ownerIdentity).
		Order("created_at DESC, id DESC").
		Limit(normalizeTaskStateLimit(limit)).
		Find(&rows).Error; err != nil {
		return nil, err
	}
	result := make([]CompletionPlan, 0, len(rows))
	for _, row := range rows {
		plan, err := completionPlanFromModel(row)
		if err != nil {
			return nil, err
		}
		result = append(result, plan)
	}
	return result, nil
}

func (r *PostgresTaskStateRepository) FindCompletionPlan(ownerIdentity, taskPlanID string) (*CompletionPlan, error) {
	ownerIdentity, err := normalizeTaskStateOwner(ownerIdentity)
	if err != nil {
		return nil, err
	}
	taskPlanID = strings.TrimSpace(taskPlanID)
	if taskPlanID == "" {
		return nil, fmt.Errorf("task plan id is required")
	}
	var row models.TaskCompletionPlanLog
	err = r.DB.
		Where("owner_identity = ? AND task_plan_id = ?", ownerIdentity, taskPlanID).
		Order("created_at DESC, id DESC").
		First(&row).Error
	if err != nil {
		return nil, taskStateDatabaseError(err)
	}
	plan, err := completionPlanFromModel(row)
	if err != nil {
		return nil, err
	}
	return &plan, nil
}

func (r *PostgresTaskStateRepository) CreateReviewItem(ownerIdentity string, item ReviewQueueItem) (*ReviewQueueItem, error) {
	row, err := reviewItemToModel(ownerIdentity, item)
	if err != nil {
		return nil, err
	}
	createResult := r.DB.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "id"}},
		DoNothing: true,
	}).Create(&row)
	if createResult.Error != nil {
		return nil, createResult.Error
	}
	if createResult.RowsAffected == 0 {
		var existing models.TaskReviewItemRecord
		err := r.DB.
			Where("owner_identity = ? AND id = ?", row.OwnerIdentity, row.ID).
			First(&existing).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrTaskStateConflict
		}
		if err != nil {
			return nil, err
		}
		if !sameReviewItemCreateRecord(existing, row) {
			return nil, ErrTaskStateConflict
		}
		latest, err := r.latestDecision(existing.OwnerIdentity, existing.ID, "", 0)
		if err != nil {
			return nil, err
		}
		existingItem, err := reviewItemFromModel(existing, latest)
		if err != nil {
			return nil, err
		}
		return &existingItem, nil
	}
	result, err := reviewItemFromModel(row, nil)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func (r *PostgresTaskStateRepository) ListReviewItems(ownerIdentity string, limit int) ([]ReviewQueueItem, error) {
	return r.listReviewItems(ownerIdentity, limit, nil)
}

func (r *PostgresTaskStateRepository) ListPendingReviewItems(ownerIdentity string, limit int) ([]ReviewQueueItem, error) {
	return r.listReviewItems(ownerIdentity, limit, []string{"open", "needs_review"})
}

func (r *PostgresTaskStateRepository) listReviewItems(ownerIdentity string, limit int, statuses []string) ([]ReviewQueueItem, error) {
	ownerIdentity, err := normalizeTaskStateOwner(ownerIdentity)
	if err != nil {
		return nil, err
	}
	var rows []models.TaskReviewItemRecord
	query := r.DB.
		Where("owner_identity = ?", ownerIdentity).
		Order("created_at DESC, id DESC")
	if len(statuses) > 0 {
		query = query.Where("status IN ?", statuses)
	}
	if err := query.Limit(normalizeTaskStateLimit(limit)).Find(&rows).Error; err != nil {
		return nil, err
	}
	result := make([]ReviewQueueItem, 0, len(rows))
	for _, row := range rows {
		latest, err := r.latestDecision(row.OwnerIdentity, row.ID, "", 0)
		if err != nil {
			return nil, err
		}
		item, err := reviewItemFromModel(row, latest)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, nil
}

func (r *PostgresTaskStateRepository) FindReviewItem(ownerIdentity, reviewItemID string) (*ReviewQueueItem, error) {
	ownerIdentity, id, err := normalizeReviewLookup(ownerIdentity, reviewItemID)
	if err != nil {
		return nil, err
	}
	var row models.TaskReviewItemRecord
	if err := r.DB.
		Where("owner_identity = ? AND id = ?", ownerIdentity, id).
		First(&row).Error; err != nil {
		return nil, taskStateDatabaseError(err)
	}
	latest, err := r.latestDecision(ownerIdentity, id, "", 0)
	if err != nil {
		return nil, err
	}
	item, err := reviewItemFromModel(row, latest)
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *PostgresTaskStateRepository) ResolveReviewItem(ownerIdentity, reviewItemID string, resolution ReviewResolution) (*PersistedReviewResolution, error) {
	ownerIdentity, id, err := normalizeReviewLookup(ownerIdentity, reviewItemID)
	if err != nil {
		return nil, err
	}
	requestedDecision, err := normalizeReviewDecision(resolution.Decision)
	if err != nil {
		return nil, err
	}
	resolution.Decision = requestedDecision
	var storedItem models.TaskReviewItemRecord
	var storedDecision models.TaskReviewDecisionRecord
	err = r.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("owner_identity = ? AND id = ?", ownerIdentity, id).
			First(&storedItem).Error; err != nil {
			return taskStateDatabaseError(err)
		}
		latestStored, err := latestDecisionWithDB(tx, ownerIdentity, id, "", 0)
		if err != nil {
			return err
		}
		if _, err := reviewItemFromModel(storedItem, latestStored); err != nil {
			return err
		}
		if storedItem.Status != "open" && storedItem.Status != "needs_review" {
			return ErrTaskReviewAlreadyResolved
		}
		decision, err := newReviewDecisionModel(ownerIdentity, storedItem, resolution)
		if err != nil {
			return err
		}
		if err := tx.Create(&decision).Error; err != nil {
			return err
		}
		result := tx.Model(&models.TaskReviewItemRecord{}).
			Where("owner_identity = ? AND id = ? AND status IN ?", ownerIdentity, id, []string{"open", "needs_review"}).
			Updates(map[string]interface{}{
				"status":      decision.Decision,
				"resolved_at": decision.ResolvedAt,
				"updated_at":  decision.ResolvedAt,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrTaskReviewAlreadyResolved
		}
		storedItem.Status = decision.Decision
		storedItem.ResolvedAt = cloneTaskStateTime(&decision.ResolvedAt)
		storedItem.UpdatedAt = decision.ResolvedAt
		storedDecision = decision
		return nil
	})
	if err != nil {
		return nil, err
	}
	item, err := reviewItemFromModel(storedItem, &storedDecision)
	if err != nil {
		return nil, err
	}
	decision, err := reviewDecisionFromModel(storedDecision)
	if err != nil {
		return nil, err
	}
	return &PersistedReviewResolution{Item: item, Decision: decision}, nil
}

func (r *PostgresTaskStateRepository) MarkReviewOutcome(ownerIdentity, reviewItemID string, outcome ReviewOutcome) (*ReviewQueueItem, error) {
	ownerIdentity, id, err := normalizeReviewLookup(ownerIdentity, reviewItemID)
	if err != nil {
		return nil, err
	}
	normalized, err := normalizeReviewOutcome(outcome)
	if err != nil {
		return nil, err
	}
	var storedItem models.TaskReviewItemRecord
	var latest *models.TaskReviewDecisionRecord
	err = r.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("owner_identity = ? AND id = ?", ownerIdentity, id).
			First(&storedItem).Error; err != nil {
			return taskStateDatabaseError(err)
		}
		latestStored, err := latestDecisionWithDB(tx, ownerIdentity, id, "", 0)
		if err != nil {
			return err
		}
		if _, err := reviewItemFromModel(storedItem, latestStored); err != nil {
			return err
		}
		idempotent := storedItem.Status == normalized.Status &&
			storedItem.CurrentTaskPlanID == normalized.TaskPlanID
		if storedItem.Status != "approved" && !idempotent {
			return ErrTaskReviewInvalidTransition
		}
		activeRevision := storedItem.ReviewRevision
		if storedItem.Status == "needs_review" {
			activeRevision--
		}
		approved, err := latestDecisionWithDB(tx, ownerIdentity, id, "approved", activeRevision)
		if err != nil {
			return err
		}
		if approved == nil || approved.RequestDigest != storedItem.RequestDigest {
			return ErrTaskReviewBindingMismatch
		}
		latest = approved
		if idempotent {
			return nil
		}
		if normalized.At.Before(storedItem.UpdatedAt) {
			return fmt.Errorf("%w: outcome cannot predate the active approval", ErrTaskReviewInvalidTransition)
		}
		applyNormalizedReviewOutcome(&storedItem, normalized)
		result := tx.Model(&models.TaskReviewItemRecord{}).
			Where("owner_identity = ? AND id = ? AND status = ?", ownerIdentity, id, "approved").
			Updates(map[string]interface{}{
				"current_task_plan_id": storedItem.CurrentTaskPlanID,
				"status":               storedItem.Status,
				"reason":               storedItem.Reason,
				"review_revision":      storedItem.ReviewRevision,
				"resolved_at":          storedItem.ResolvedAt,
				"updated_at":           storedItem.UpdatedAt,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrTaskReviewInvalidTransition
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	item, err := reviewItemFromModel(storedItem, latest)
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *PostgresTaskStateRepository) ListReviewDecisions(ownerIdentity, reviewItemID string, limit int) ([]ReviewDecisionRecord, error) {
	ownerIdentity, id, err := normalizeReviewLookup(ownerIdentity, reviewItemID)
	if err != nil {
		return nil, err
	}
	var item models.TaskReviewItemRecord
	if err := r.DB.
		Where("owner_identity = ? AND id = ?", ownerIdentity, id).
		First(&item).Error; err != nil {
		return nil, taskStateDatabaseError(err)
	}
	latest, err := r.latestDecision(ownerIdentity, id, "", 0)
	if err != nil {
		return nil, err
	}
	if _, err := reviewItemFromModel(item, latest); err != nil {
		return nil, err
	}
	var rows []models.TaskReviewDecisionRecord
	if err := r.DB.
		Where("owner_identity = ? AND review_item_id = ?", ownerIdentity, id).
		Order("resolved_at DESC, id DESC").
		Limit(normalizeTaskStateLimit(limit)).
		Find(&rows).Error; err != nil {
		return nil, err
	}
	result := make([]ReviewDecisionRecord, 0, len(rows))
	for _, row := range rows {
		if err := validateReviewDecisionBinding(row, item); err != nil {
			return nil, err
		}
		decision, err := reviewDecisionFromModel(row)
		if err != nil {
			return nil, err
		}
		result = append(result, decision)
	}
	return result, nil
}

func (r *PostgresTaskStateRepository) FindApprovedReviewDecision(ownerIdentity, reviewItemID string) (*ReviewDecisionRecord, error) {
	ownerIdentity, id, err := normalizeReviewLookup(ownerIdentity, reviewItemID)
	if err != nil {
		return nil, err
	}
	var item models.TaskReviewItemRecord
	if err := r.DB.
		Where("owner_identity = ? AND id = ? AND status = ?", ownerIdentity, id, "approved").
		First(&item).Error; err != nil {
		return nil, taskStateDatabaseError(err)
	}
	latest, err := r.latestDecision(ownerIdentity, id, "approved", item.ReviewRevision)
	if err != nil {
		return nil, err
	}
	if latest == nil {
		return nil, ErrTaskStateNotFound
	}
	if latest.RequestDigest != item.RequestDigest {
		return nil, ErrTaskReviewBindingMismatch
	}
	if _, err := reviewItemFromModel(item, latest); err != nil {
		return nil, err
	}
	if err := validateReviewDecisionBinding(*latest, item); err != nil {
		return nil, err
	}
	decision, err := reviewDecisionFromModel(*latest)
	if err != nil {
		return nil, err
	}
	return &decision, nil
}

// FindApprovedReviewDecisionInPostgresTransaction locks the mutable review
// item and reads its immutable approval through the caller's execution
// transaction. Review resolution and outcome reconciliation take the same row
// lock, so either they complete first or this execution claim sees their
// committed state before consuming approval.
func (r *PostgresTaskStateRepository) FindApprovedReviewDecisionInPostgresTransaction(
	tx *gorm.DB,
	ownerIdentity string,
	reviewItemID string,
) (*ReviewDecisionRecord, error) {
	ownerIdentity, id, err := normalizeReviewLookup(ownerIdentity, reviewItemID)
	if err != nil {
		return nil, err
	}
	if !sameTaskStatePostgresTransaction(r.DB, tx) {
		return nil, fmt.Errorf("task review execution requires a transaction from the same PostgreSQL database")
	}
	var item models.TaskReviewItemRecord
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("owner_identity = ? AND id = ? AND status = ?", ownerIdentity, id, "approved").
		First(&item).Error; err != nil {
		return nil, taskStateDatabaseError(err)
	}
	latest, err := latestDecisionWithDB(tx, ownerIdentity, id, "approved", item.ReviewRevision)
	if err != nil {
		return nil, err
	}
	if latest == nil {
		return nil, ErrTaskStateNotFound
	}
	if latest.RequestDigest != item.RequestDigest {
		return nil, ErrTaskReviewBindingMismatch
	}
	if _, err := reviewItemFromModel(item, latest); err != nil {
		return nil, err
	}
	if err := validateReviewDecisionBinding(*latest, item); err != nil {
		return nil, err
	}
	decision, err := reviewDecisionFromModel(*latest)
	if err != nil {
		return nil, err
	}
	return &decision, nil
}

func sameTaskStatePostgresTransaction(database, tx *gorm.DB) bool {
	if database == nil || tx == nil || database.Dialector == nil || tx.Dialector == nil ||
		database.Dialector.Name() != "postgres" || tx.Dialector.Name() != "postgres" ||
		database.Error != nil || tx.Error != nil || database.Statement == nil || tx.Statement == nil {
		return false
	}
	if _, alreadyTransactional := database.Statement.ConnPool.(gorm.TxCommitter); alreadyTransactional {
		return false
	}
	if _, isTransaction := tx.Statement.ConnPool.(gorm.TxCommitter); !isTransaction {
		return false
	}
	return infra.PostgresExecutionPoolMatches(database, tx)
}

func (r *PostgresTaskStateRepository) latestDecision(
	ownerIdentity string,
	reviewItemID uuid.UUID,
	decision string,
	reviewRevision int,
) (*models.TaskReviewDecisionRecord, error) {
	return latestDecisionWithDB(r.DB, ownerIdentity, reviewItemID, decision, reviewRevision)
}

func latestDecisionWithDB(
	db *gorm.DB,
	ownerIdentity string,
	reviewItemID uuid.UUID,
	decision string,
	reviewRevision int,
) (*models.TaskReviewDecisionRecord, error) {
	query := db.Where("owner_identity = ? AND review_item_id = ?", ownerIdentity, reviewItemID)
	if decision != "" {
		query = query.Where("decision = ?", decision)
	}
	if reviewRevision > 0 {
		query = query.Where("review_revision = ?", reviewRevision)
	}
	var row models.TaskReviewDecisionRecord
	err := query.Order("resolved_at DESC, id DESC").First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func normalizeReviewLookup(ownerIdentity, reviewItemID string) (string, uuid.UUID, error) {
	ownerIdentity, err := normalizeTaskStateOwner(ownerIdentity)
	if err != nil {
		return "", uuid.Nil, err
	}
	id, err := uuid.Parse(strings.TrimSpace(reviewItemID))
	if err != nil || id == uuid.Nil {
		return "", uuid.Nil, ErrTaskStateNotFound
	}
	return ownerIdentity, id, nil
}

func taskStateDatabaseError(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrTaskStateNotFound
	}
	return err
}
