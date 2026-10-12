// Package temporalbridge provides an opt-in, local Temporal bridge for one
// HAI-owned durable workflow. It never receives source content or external
// action payloads and only calls existing governed HAI services.
package temporalbridge

import (
	"automation-hub-backend/internal/infra"
	"automation-hub-backend/internal/models"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type Repository interface {
	Create(*models.TemporalWorkflowRun) (*models.TemporalWorkflowRun, error)
	Update(*models.TemporalWorkflowRun) (*models.TemporalWorkflowRun, error)
	TransitionSchedule(context.Context, models.TemporalWorkflowRun, string, string, string, time.Time) (bool, error)
	TransitionActivity(context.Context, models.TemporalWorkflowRun, models.TemporalWorkflowRun) (bool, error)
	FindByID(uuid.UUID) (*models.TemporalWorkflowRun, error)
	FindByIDContext(context.Context, uuid.UUID) (*models.TemporalWorkflowRun, error)
	FindForOwner(context.Context, string, string) (*models.TemporalWorkflowRun, error)
	ListForOwner(string, int) ([]models.TemporalWorkflowRun, error)
}

func (r *gormRepository) FindByID(id uuid.UUID) (*models.TemporalWorkflowRun, error) {
	return r.FindByIDContext(context.Background(), id)
}

func (r *gormRepository) FindByIDContext(ctx context.Context, id uuid.UUID) (*models.TemporalWorkflowRun, error) {
	if ctx == nil {
		return nil, errors.New("governed activity lookup requires a context")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var run models.TemporalWorkflowRun
	if err := r.db.WithContext(ctx).First(&run, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &run, nil
}

type gormRepository struct{ db *gorm.DB }

func NewGormRepository(db *gorm.DB) Repository { return &gormRepository{db: db} }

func DefaultRepository() Repository {
	db, err := infra.GetDefaultDB()
	if err != nil {
		panic(err)
	}
	return NewGormRepository(db)
}

func (r *gormRepository) Create(run *models.TemporalWorkflowRun) (*models.TemporalWorkflowRun, error) {
	if err := r.db.Create(run).Error; err != nil {
		return nil, err
	}
	return run, nil
}

func (r *gormRepository) Update(run *models.TemporalWorkflowRun) (*models.TemporalWorkflowRun, error) {
	if err := r.db.Save(run).Error; err != nil {
		return nil, err
	}
	return run, nil
}

// TransitionSchedule changes only scheduling metadata, never worker results.
// The state predicate prevents late SDK responses from regressing activity progress.
func (r *gormRepository) TransitionSchedule(ctx context.Context, expected models.TemporalWorkflowRun, from, to, summary string, at time.Time) (bool, error) {
	if ctx == nil || expected.ID == uuid.Nil || strings.TrimSpace(expected.OwnerIdentity) == "" ||
		expected.TemporalWorkflowID == "" || expected.WorkflowType != followUpWorkflowType ||
		!validScheduleTransition(from, to) {
		return false, errors.New("invalid governed scheduling transition")
	}
	result := r.db.WithContext(ctx).Model(&models.TemporalWorkflowRun{}).
		Where("id = ? AND owner_identity = ? AND temporal_workflow_id = ? AND workflow_type = ? AND scheduled_for = ? AND status = ? AND started_at IS NULL AND completed_at IS NULL",
			expected.ID, expected.OwnerIdentity, expected.TemporalWorkflowID, expected.WorkflowType, expected.ScheduledFor, from).
		Updates(map[string]any{"status": to, "summary": summary, "updated_at": at.UTC()})
	return result.RowsAffected == 1 && result.Error == nil, result.Error
}

func validScheduleTransition(from, to string) bool {
	return (from == "preparing" && (to == "dispatching" || to == "failed")) ||
		(from == "dispatching" && (to == "scheduled" || to == "schedule_uncertain" || to == "failed"))
}

// TransitionActivity claims an unstarted run or settles only that same claim.
// It never inserts a missing row, reclaims a running run, or rewrites identity.
func (r *gormRepository) TransitionActivity(ctx context.Context, expected, next models.TemporalWorkflowRun) (bool, error) {
	if ctx == nil || !validActivityTransition(expected, next) {
		return false, errors.New("invalid governed activity transition")
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	query := r.db.WithContext(ctx).Model(&models.TemporalWorkflowRun{}).
		Where("id = ? AND owner_identity = ? AND temporal_workflow_id = ? AND workflow_type = ? AND scheduled_for = ? AND status = ? AND updated_at = ? AND completed_at IS NULL",
			expected.ID, expected.OwnerIdentity, expected.TemporalWorkflowID, expected.WorkflowType, expected.ScheduledFor, expected.Status, expected.UpdatedAt)
	if expected.StartedAt == nil {
		query = query.Where("started_at IS NULL")
	} else {
		query = query.Where("started_at = ?", *expected.StartedAt)
	}
	result := query.Updates(map[string]any{
		"status": next.Status, "started_at": next.StartedAt, "completed_at": next.CompletedAt,
		"summary": next.Summary, "result_json": next.ResultJSON, "updated_at": next.UpdatedAt.UTC(),
	})
	return result.RowsAffected == 1 && result.Error == nil, result.Error
}

func validActivityTransition(expected, next models.TemporalWorkflowRun) bool {
	if expected.ID == uuid.Nil || strings.TrimSpace(expected.OwnerIdentity) == "" || expected.TemporalWorkflowID == "" ||
		expected.WorkflowType != followUpWorkflowType || expected.CompletedAt != nil || next.ID != expected.ID ||
		next.OwnerIdentity != expected.OwnerIdentity || next.TemporalWorkflowID != expected.TemporalWorkflowID ||
		next.WorkflowType != expected.WorkflowType || !next.ScheduledFor.Equal(expected.ScheduledFor) ||
		next.StartedAt == nil || next.StartedAt.IsZero() || next.UpdatedAt.IsZero() {
		return false
	}
	if expected.StartedAt == nil {
		return (expected.Status == "dispatching" || expected.Status == "scheduled" || expected.Status == "schedule_uncertain") &&
			next.Status == "running" && next.CompletedAt == nil
	}
	if expected.Status != "running" || !next.StartedAt.Equal(*expected.StartedAt) {
		return false
	}
	if next.Status == "failed" {
		return next.CompletedAt == nil
	}
	if next.Status != "completed" || next.CompletedAt == nil || next.CompletedAt.IsZero() || next.CompletedAt.Before(*next.StartedAt) {
		return false
	}
	var result *FollowUpResult
	return json.Unmarshal([]byte(next.ResultJSON), &result) == nil && result != nil
}

func (r *gormRepository) FindForOwner(ctx context.Context, ownerIdentity, workflowID string) (*models.TemporalWorkflowRun, error) {
	var run models.TemporalWorkflowRun
	if err := r.db.WithContext(ctx).Where("owner_identity = ? AND temporal_workflow_id = ?", ownerIdentity, workflowID).First(&run).Error; err != nil {
		return nil, err
	}
	return &run, nil
}

func (r *gormRepository) ListForOwner(ownerIdentity string, limit int) ([]models.TemporalWorkflowRun, error) {
	if limit <= 0 || limit > 100 {
		limit = 25
	}
	var runs []models.TemporalWorkflowRun
	err := r.db.Where("owner_identity = ?", ownerIdentity).Order("created_at DESC").Limit(limit).Find(&runs).Error
	return runs, err
}
