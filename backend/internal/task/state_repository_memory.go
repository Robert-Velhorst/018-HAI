package task

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"automation-hub-backend/internal/models"

	"github.com/google/uuid"
)

// MemoryTaskStateRepository mirrors the PostgreSQL contract for deterministic
// service tests. It stores encoded snapshots rather than caller-owned structs,
// so malformed JSON and accidental mutation are caught by the same decoders.
type MemoryTaskStateRepository struct {
	mu          sync.RWMutex
	operations  map[string]models.TaskOperationRecord
	completions []models.TaskCompletionPlanLog
	reviews     map[uuid.UUID]models.TaskReviewItemRecord
	decisions   []models.TaskReviewDecisionRecord
}

func NewMemoryTaskStateRepository() *MemoryTaskStateRepository {
	return &MemoryTaskStateRepository{
		operations:  map[string]models.TaskOperationRecord{},
		completions: []models.TaskCompletionPlanLog{},
		reviews:     map[uuid.UUID]models.TaskReviewItemRecord{},
		decisions:   []models.TaskReviewDecisionRecord{},
	}
}

func (r *MemoryTaskStateRepository) AppendCompletionPlan(ownerIdentity string, plan CompletionPlan) error {
	row, err := completionPlanToModel(ownerIdentity, plan)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ensureInitialized()
	for _, existing := range r.completions {
		if existing.OwnerIdentity == row.OwnerIdentity &&
			existing.TaskPlanID == row.TaskPlanID &&
			existing.PayloadDigest == row.PayloadDigest {
			return nil
		}
	}
	r.completions = append(r.completions, row)
	return nil
}

func (r *MemoryTaskStateRepository) ListCompletionPlans(ownerIdentity string, limit int) ([]CompletionPlan, error) {
	ownerIdentity, err := normalizeTaskStateOwner(ownerIdentity)
	if err != nil {
		return nil, err
	}
	limit = normalizeTaskStateLimit(limit)

	r.mu.RLock()
	rows := make([]models.TaskCompletionPlanLog, 0)
	for _, row := range r.completions {
		if row.OwnerIdentity == ownerIdentity {
			rows = append(rows, row)
		}
	}
	r.mu.RUnlock()
	sortCompletionRows(rows)
	if len(rows) > limit {
		rows = rows[:limit]
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

func (r *MemoryTaskStateRepository) ListCompletionPlanHistory(ownerIdentity string, limit int) ([]CompletionPlanHistoryItem, error) {
	plans, err := r.ListCompletionPlans(ownerIdentity, limit)
	if err != nil {
		return nil, err
	}
	return completionPlanHistory(plans), nil
}

func (r *MemoryTaskStateRepository) FindCompletionPlan(ownerIdentity, taskPlanID string) (*CompletionPlan, error) {
	return r.FindCompletionPlanContext(context.Background(), ownerIdentity, taskPlanID)
}

func (r *MemoryTaskStateRepository) FindCompletionPlanContext(ctx context.Context, ownerIdentity, taskPlanID string) (*CompletionPlan, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ownerIdentity, err := normalizeTaskStateOwner(ownerIdentity)
	if err != nil {
		return nil, err
	}
	taskPlanID = strings.TrimSpace(taskPlanID)
	if taskPlanID == "" {
		return nil, fmt.Errorf("task plan id is required")
	}

	r.mu.RLock()
	if err := ctx.Err(); err != nil {
		r.mu.RUnlock()
		return nil, err
	}
	rows := make([]models.TaskCompletionPlanLog, 0)
	for _, row := range r.completions {
		if row.OwnerIdentity == ownerIdentity && row.TaskPlanID == taskPlanID {
			rows = append(rows, row)
		}
	}
	r.mu.RUnlock()
	if len(rows) == 0 {
		return nil, ErrTaskStateNotFound
	}
	sortCompletionRows(rows)
	plan, err := completionPlanFromModel(rows[0])
	if err != nil {
		return nil, err
	}
	return &plan, nil
}

func (r *MemoryTaskStateRepository) CreateReviewItem(ownerIdentity string, item ReviewQueueItem) (*ReviewQueueItem, error) {
	row, err := reviewItemToModel(ownerIdentity, item)
	if err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ensureInitialized()
	if existing, exists := r.reviews[row.ID]; exists {
		if !sameReviewItemCreateRecord(existing, row) {
			return nil, ErrTaskStateConflict
		}
		latest := latestDecisionFromRows(r.decisions, existing.OwnerIdentity, existing.ID, "", 0)
		result, err := reviewItemFromModel(existing, latest)
		if err != nil {
			return nil, err
		}
		return &result, nil
	}
	r.reviews[row.ID] = row
	result, err := reviewItemFromModel(row, nil)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func (r *MemoryTaskStateRepository) ListReviewItems(ownerIdentity string, limit int) ([]ReviewQueueItem, error) {
	return r.listReviewItems(ownerIdentity, limit, false)
}

func (r *MemoryTaskStateRepository) ListPendingReviewItems(ownerIdentity string, limit int) ([]ReviewQueueItem, error) {
	return r.listReviewItems(ownerIdentity, limit, true)
}

func (r *MemoryTaskStateRepository) listReviewItems(ownerIdentity string, limit int, pendingOnly bool) ([]ReviewQueueItem, error) {
	ownerIdentity, err := normalizeTaskStateOwner(ownerIdentity)
	if err != nil {
		return nil, err
	}
	limit = normalizeTaskStateLimit(limit)

	r.mu.RLock()
	rows := make([]models.TaskReviewItemRecord, 0)
	decisions := append([]models.TaskReviewDecisionRecord(nil), r.decisions...)
	for _, row := range r.reviews {
		if row.OwnerIdentity == ownerIdentity && (!pendingOnly || row.Status == "open" || row.Status == "needs_review") {
			rows = append(rows, row)
		}
	}
	r.mu.RUnlock()
	sortReviewRows(rows)
	if len(rows) > limit {
		rows = rows[:limit]
	}
	result := make([]ReviewQueueItem, 0, len(rows))
	for _, row := range rows {
		latest := latestDecisionFromRows(decisions, row.OwnerIdentity, row.ID, "", 0)
		item, err := reviewItemFromModel(row, latest)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, nil
}

func (r *MemoryTaskStateRepository) FindReviewItem(ownerIdentity, reviewItemID string) (*ReviewQueueItem, error) {
	ownerIdentity, err := normalizeTaskStateOwner(ownerIdentity)
	if err != nil {
		return nil, err
	}
	id, err := uuid.Parse(strings.TrimSpace(reviewItemID))
	if err != nil || id == uuid.Nil {
		return nil, ErrTaskStateNotFound
	}
	r.mu.RLock()
	row, exists := r.reviews[id]
	decisions := append([]models.TaskReviewDecisionRecord(nil), r.decisions...)
	r.mu.RUnlock()
	if !exists || row.OwnerIdentity != ownerIdentity {
		return nil, ErrTaskStateNotFound
	}
	latest := latestDecisionFromRows(decisions, ownerIdentity, id, "", 0)
	item, err := reviewItemFromModel(row, latest)
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *MemoryTaskStateRepository) ResolveReviewItem(ownerIdentity, reviewItemID string, resolution ReviewResolution) (*PersistedReviewResolution, error) {
	ownerIdentity, err := normalizeTaskStateOwner(ownerIdentity)
	if err != nil {
		return nil, err
	}
	id, err := uuid.Parse(strings.TrimSpace(reviewItemID))
	if err != nil || id == uuid.Nil {
		return nil, ErrTaskStateNotFound
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ensureInitialized()
	row, exists := r.reviews[id]
	if !exists || row.OwnerIdentity != ownerIdentity {
		return nil, ErrTaskStateNotFound
	}
	latestStored := latestDecisionFromRows(r.decisions, ownerIdentity, id, "", 0)
	if _, err := reviewItemFromModel(row, latestStored); err != nil {
		return nil, err
	}
	requestedDecision, err := normalizeReviewDecision(resolution.Decision)
	if err != nil {
		return nil, err
	}
	if row.Status != "open" && row.Status != "needs_review" {
		return nil, ErrTaskReviewAlreadyResolved
	}
	resolution.Decision = requestedDecision
	decisionRow, err := newReviewDecisionModel(ownerIdentity, row, resolution)
	if err != nil {
		return nil, err
	}
	row.Status = decisionRow.Decision
	row.ResolvedAt = cloneTaskStateTime(&decisionRow.ResolvedAt)
	row.UpdatedAt = decisionRow.ResolvedAt
	r.reviews[id] = row
	r.decisions = append(r.decisions, decisionRow)

	item, err := reviewItemFromModel(row, &decisionRow)
	if err != nil {
		return nil, err
	}
	decision, err := reviewDecisionFromModel(decisionRow)
	if err != nil {
		return nil, err
	}
	return &PersistedReviewResolution{Item: item, Decision: decision}, nil
}

func (r *MemoryTaskStateRepository) MarkReviewOutcome(ownerIdentity, reviewItemID string, outcome ReviewOutcome) (*ReviewQueueItem, error) {
	ownerIdentity, err := normalizeTaskStateOwner(ownerIdentity)
	if err != nil {
		return nil, err
	}
	id, err := uuid.Parse(strings.TrimSpace(reviewItemID))
	if err != nil || id == uuid.Nil {
		return nil, ErrTaskStateNotFound
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ensureInitialized()
	row, exists := r.reviews[id]
	if !exists || row.OwnerIdentity != ownerIdentity {
		return nil, ErrTaskStateNotFound
	}
	latestStored := latestDecisionFromRows(r.decisions, ownerIdentity, id, "", 0)
	if _, err := reviewItemFromModel(row, latestStored); err != nil {
		return nil, err
	}
	normalized, err := normalizeReviewOutcome(outcome)
	if err != nil {
		return nil, err
	}
	idempotent := row.Status == normalized.Status &&
		row.CurrentTaskPlanID == normalized.TaskPlanID
	if row.Status != "approved" && !idempotent {
		return nil, ErrTaskReviewInvalidTransition
	}
	activeRevision := row.ReviewRevision
	if row.Status == "needs_review" {
		activeRevision--
	}
	latestApproved := latestDecisionFromRows(
		r.decisions,
		ownerIdentity,
		id,
		"approved",
		activeRevision,
	)
	if latestApproved == nil || latestApproved.RequestDigest != row.RequestDigest {
		return nil, ErrTaskReviewBindingMismatch
	}
	if idempotent {
		latest := latestDecisionFromRows(r.decisions, ownerIdentity, id, "", 0)
		item, err := reviewItemFromModel(row, latest)
		if err != nil {
			return nil, err
		}
		return &item, nil
	}
	if normalized.At.Before(row.UpdatedAt) {
		return nil, fmt.Errorf("%w: outcome cannot predate the active approval", ErrTaskReviewInvalidTransition)
	}
	applyNormalizedReviewOutcome(&row, normalized)
	r.reviews[id] = row
	latest := latestDecisionFromRows(r.decisions, ownerIdentity, id, "", 0)
	item, err := reviewItemFromModel(row, latest)
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *MemoryTaskStateRepository) ListReviewDecisions(ownerIdentity, reviewItemID string, limit int) ([]ReviewDecisionRecord, error) {
	ownerIdentity, err := normalizeTaskStateOwner(ownerIdentity)
	if err != nil {
		return nil, err
	}
	id, err := uuid.Parse(strings.TrimSpace(reviewItemID))
	if err != nil || id == uuid.Nil {
		return nil, ErrTaskStateNotFound
	}
	limit = normalizeTaskStateLimit(limit)
	r.mu.RLock()
	item, exists := r.reviews[id]
	rows := make([]models.TaskReviewDecisionRecord, 0)
	for _, row := range r.decisions {
		if row.OwnerIdentity == ownerIdentity && row.ReviewItemID == id {
			rows = append(rows, row)
		}
	}
	r.mu.RUnlock()
	if !exists || item.OwnerIdentity != ownerIdentity {
		return nil, ErrTaskStateNotFound
	}
	latest := latestDecisionFromRows(rows, ownerIdentity, id, "", 0)
	if _, err := reviewItemFromModel(item, latest); err != nil {
		return nil, err
	}
	sortDecisionRows(rows)
	if len(rows) > limit {
		rows = rows[:limit]
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

func (r *MemoryTaskStateRepository) FindApprovedReviewDecision(ownerIdentity, reviewItemID string) (*ReviewDecisionRecord, error) {
	ownerIdentity, err := normalizeTaskStateOwner(ownerIdentity)
	if err != nil {
		return nil, err
	}
	id, err := uuid.Parse(strings.TrimSpace(reviewItemID))
	if err != nil || id == uuid.Nil {
		return nil, ErrTaskStateNotFound
	}
	r.mu.RLock()
	item, exists := r.reviews[id]
	latest := latestDecisionFromRows(
		r.decisions,
		ownerIdentity,
		id,
		"approved",
		item.ReviewRevision,
	)
	r.mu.RUnlock()
	if !exists || item.OwnerIdentity != ownerIdentity || item.Status != "approved" || latest == nil {
		return nil, ErrTaskStateNotFound
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

func (r *MemoryTaskStateRepository) ensureInitialized() {
	if r.operations == nil {
		r.operations = map[string]models.TaskOperationRecord{}
	}
	if r.completions == nil {
		r.completions = []models.TaskCompletionPlanLog{}
	}
	if r.reviews == nil {
		r.reviews = map[uuid.UUID]models.TaskReviewItemRecord{}
	}
	if r.decisions == nil {
		r.decisions = []models.TaskReviewDecisionRecord{}
	}
}

type normalizedReviewOutcomeValue struct {
	TaskPlanID string
	Status     string
	Reason     string
	At         time.Time
}

func normalizeReviewOutcome(outcome ReviewOutcome) (normalizedReviewOutcomeValue, error) {
	taskPlanID := strings.TrimSpace(outcome.TaskPlanID)
	if taskPlanID == "" || len([]rune(taskPlanID)) > 160 {
		return normalizedReviewOutcomeValue{}, fmt.Errorf("outcome task plan id must contain 1 to 160 characters")
	}
	status := strings.ToLower(strings.TrimSpace(outcome.Status))
	if status != "completed" && status != "needs_review" {
		return normalizedReviewOutcomeValue{}, fmt.Errorf("%w: outcome must be completed or needs_review", ErrTaskReviewInvalidTransition)
	}
	at := outcome.At
	if at.IsZero() {
		at = time.Now().UTC()
	}
	at = normalizeTaskStateTimestamp(at)
	reason := sanitizeTaskOperationalText(outcome.Reason, 4096)
	if reason == "" {
		if status == "completed" {
			reason = "approved task completed"
		} else {
			reason = "approved task requires another review"
		}
	}
	return normalizedReviewOutcomeValue{
		TaskPlanID: taskPlanID,
		Status:     status,
		Reason:     reason,
		At:         at,
	}, nil
}

func applyNormalizedReviewOutcome(row *models.TaskReviewItemRecord, outcome normalizedReviewOutcomeValue) {
	row.CurrentTaskPlanID = outcome.TaskPlanID
	row.Status = outcome.Status
	row.Reason = outcome.Reason
	row.UpdatedAt = outcome.At
	if outcome.Status == "completed" {
		row.ResolvedAt = cloneTaskStateTime(&outcome.At)
	} else {
		row.ReviewRevision++
		row.ResolvedAt = nil
	}
}

func sortCompletionRows(rows []models.TaskCompletionPlanLog) {
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].CreatedAt.Equal(rows[j].CreatedAt) {
			return rows[i].ID.String() > rows[j].ID.String()
		}
		return rows[i].CreatedAt.After(rows[j].CreatedAt)
	})
}

func sortReviewRows(rows []models.TaskReviewItemRecord) {
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].CreatedAt.Equal(rows[j].CreatedAt) {
			return rows[i].ID.String() > rows[j].ID.String()
		}
		return rows[i].CreatedAt.After(rows[j].CreatedAt)
	})
}

func sortDecisionRows(rows []models.TaskReviewDecisionRecord) {
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].ResolvedAt.Equal(rows[j].ResolvedAt) {
			return rows[i].ID.String() > rows[j].ID.String()
		}
		return rows[i].ResolvedAt.After(rows[j].ResolvedAt)
	})
}

func latestDecisionFromRows(
	rows []models.TaskReviewDecisionRecord,
	ownerIdentity string,
	reviewItemID uuid.UUID,
	decision string,
	reviewRevision int,
) *models.TaskReviewDecisionRecord {
	matches := make([]models.TaskReviewDecisionRecord, 0)
	for _, row := range rows {
		if row.OwnerIdentity != ownerIdentity || row.ReviewItemID != reviewItemID {
			continue
		}
		if decision != "" && row.Decision != decision {
			continue
		}
		if reviewRevision > 0 && row.ReviewRevision != reviewRevision {
			continue
		}
		matches = append(matches, row)
	}
	if len(matches) == 0 {
		return nil
	}
	sortDecisionRows(matches)
	latest := matches[0]
	return &latest
}

func normalizeReviewDecision(value string) (string, error) {
	decision := strings.ToLower(strings.TrimSpace(value))
	if decision != "approved" && decision != "rejected" {
		return "", fmt.Errorf("review decision must be approved or rejected")
	}
	return decision, nil
}

func sameReviewItemCreateRecord(left, right models.TaskReviewItemRecord) bool {
	return left.ID == right.ID &&
		left.OwnerIdentity == right.OwnerIdentity &&
		left.OriginalTaskPlanID == right.OriginalTaskPlanID &&
		left.CurrentTaskPlanID == right.CurrentTaskPlanID &&
		left.RequestDigest == right.RequestDigest &&
		left.Reason == right.Reason &&
		left.Priority == right.Priority &&
		left.Status == right.Status &&
		left.ReviewRevision == right.ReviewRevision &&
		left.CreatedAt.Equal(right.CreatedAt)
}
