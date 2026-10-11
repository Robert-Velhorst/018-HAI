package operations

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"automation-hub-backend/internal/models"

	"github.com/google/uuid"
)

// MemoryRepository is an in-process Repository implementation. It is a real
// port implementation (not a mock) used by the background loop's tests and by
// the DB-free smoke path; production uses GormRepository.
type MemoryRepository struct {
	mu                  sync.Mutex
	ops                 map[uuid.UUID]models.Operation
	events              []models.OperationEvent
	claims              map[uuid.UUID]memoryExecutionClaim
	observations        map[uuid.UUID]SourceObservation
	observationClocks   map[sourceObservationScope]int64
	sourceOrigins       map[sourceOriginKey]SourceOrigin
	sourceHeads         map[sourceHeadKey]SourceHead
	sourceHeadRevisions map[sourceRevisionKey]sourceHeadRevision
}

// NewMemoryRepository builds an empty in-memory repository.
func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{ops: map[uuid.UUID]models.Operation{}, claims: map[uuid.UUID]memoryExecutionClaim{}}
}

func (r *MemoryRepository) Create(op *models.Operation) (*models.Operation, error) {
	if op == nil {
		return nil, ErrInvalidRepositoryResult
	}
	if err := validateOperationSourceIdentity(*op); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.validateObservationReference(*op); err != nil {
		return nil, err
	}
	for _, existing := range r.ops {
		if existing.OwnerUserID == op.OwnerUserID && existing.WorkspaceID == op.WorkspaceID &&
			existing.DedupeKey == op.DedupeKey && existing.Status != string(StatusArchived) &&
			existing.Status != string(StatusDismissed) {
			return nil, ErrDuplicateDedupeKey
		}
	}
	if op.ID == uuid.Nil {
		op.ID = uuid.New()
	}
	if _, exists := r.ops[op.ID]; exists {
		return nil, errors.New("operations: duplicate operation ID")
	}
	r.ops[op.ID] = cloneOperation(*op)
	cp := cloneOperation(r.ops[op.ID])
	return &cp, nil
}

func (r *MemoryRepository) CreateWithEvent(op *models.Operation, event *models.OperationEvent) (*models.Operation, error) {
	return r.CreateWithEventContext(context.Background(), op, event)
}

func (r *MemoryRepository) CreateWithEventContext(ctx context.Context, op *models.Operation, event *models.OperationEvent) (*models.Operation, error) {
	if err := intakeContextError(ctx); err != nil {
		return nil, err
	}
	if err := validateCreationEvent(op, event); err != nil {
		return nil, err
	}
	created, audit := cloneOperation(*op), *event
	if err := r.lockObservationContext(ctx); err != nil {
		return nil, err
	}
	defer r.mu.Unlock()
	if err := intakeContextError(ctx); err != nil {
		return nil, err
	}
	if err := r.validateObservationReference(created); err != nil {
		return nil, err
	}
	if err := r.validateIntakeSourceAuthority(ctx, created); err != nil {
		return nil, err
	}
	if _, exists := r.ops[created.ID]; exists {
		return nil, errors.New("operations: duplicate operation ID")
	}
	for _, existing := range r.ops {
		if existing.OwnerUserID == created.OwnerUserID && existing.WorkspaceID == created.WorkspaceID &&
			existing.DedupeKey == created.DedupeKey && existing.Status != string(StatusArchived) &&
			existing.Status != string(StatusDismissed) {
			return nil, ErrDuplicateDedupeKey
		}
	}
	if audit.ID == uuid.Nil {
		audit.ID = uuid.New()
	}
	for _, existing := range r.events {
		if existing.ID == audit.ID {
			return nil, errors.New("operations: duplicate creation event ID")
		}
	}
	if err := intakeContextError(ctx); err != nil {
		return nil, err
	}
	r.ops[created.ID] = cloneOperation(created)
	r.events = append(r.events, audit)
	return &created, nil
}

func (r *MemoryRepository) Update(op *models.Operation) (*models.Operation, error) {
	return r.update(op, nil)
}

func (r *MemoryRepository) UpdateWithEvent(op *models.Operation, event *models.OperationEvent) (*models.Operation, error) {
	if err := validateMutationEvent(op, event); err != nil {
		return nil, err
	}
	return r.update(op, event)
}

func (r *MemoryRepository) update(op *models.Operation, event *models.OperationEvent) (*models.Operation, error) {
	return r.updateContext(context.Background(), op, event)
}

func (r *MemoryRepository) UpdateWithEventContext(ctx context.Context, op *models.Operation, event *models.OperationEvent) (*models.Operation, error) {
	if err := intakeContextError(ctx); err != nil {
		return nil, err
	}
	if err := validateMutationEvent(op, event); err != nil {
		return nil, err
	}
	return r.updateContext(ctx, op, event)
}

func (r *MemoryRepository) updateContext(ctx context.Context, op *models.Operation, event *models.OperationEvent) (*models.Operation, error) {
	if err := intakeContextError(ctx); err != nil {
		return nil, err
	}
	if op == nil || op.ID == uuid.Nil || op.Version <= 1 {
		return nil, ErrStaleOperation
	}
	if err := r.lockObservationContext(ctx); err != nil {
		return nil, err
	}
	defer r.mu.Unlock()
	if err := intakeContextError(ctx); err != nil {
		return nil, err
	}
	current, ok := r.ops[op.ID]
	if !ok {
		return nil, ErrNotFound
	}
	if current.Version != op.Version-1 {
		return nil, ErrStaleOperation
	}
	if current.OwnerUserID != op.OwnerUserID || current.WorkspaceID != op.WorkspaceID {
		return nil, errors.New("operations: owner and workspace are immutable")
	}
	if err := validateSourceIdentityMutation(current, *op); err != nil {
		return nil, err
	}
	if err := r.validateIntakeSourceAuthority(ctx, *op); err != nil {
		return nil, err
	}
	if event != nil {
		if err := validateMutationState(current, *op, event); err != nil {
			return nil, err
		}
	}
	if claim, ok := r.claims[op.ID]; ok && claim.owner != uuid.Nil {
		return nil, ErrOperationClaimed
	}
	if event != nil {
		audit := *event
		if audit.ID == uuid.Nil {
			audit.ID = uuid.New()
		}
		for _, existing := range r.events {
			if existing.ID == audit.ID {
				return nil, errors.New("operations: duplicate mutation event ID")
			}
		}
		if err := intakeContextError(ctx); err != nil {
			return nil, err
		}
		r.events = append(r.events, audit)
	} else if err := intakeContextError(ctx); err != nil {
		return nil, err
	}
	updated := cloneOperation(*op)
	updated.CreatedAt = current.CreatedAt
	r.ops[op.ID] = cloneOperation(updated)
	cp := cloneOperation(r.ops[op.ID])
	return &cp, nil
}

func (r *MemoryRepository) GetByID(ownerUserID, workspaceID string, id uuid.UUID) (*models.Operation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	op, ok := r.ops[id]
	if !ok || op.OwnerUserID != ownerUserID || op.WorkspaceID != workspaceID {
		return nil, ErrNotFound
	}
	cp := cloneOperation(op)
	return &cp, nil
}

func (r *MemoryRepository) FindByDedupeKey(ownerUserID, workspaceID, dedupeKey string) (*models.Operation, bool, error) {
	return r.FindByDedupeKeyContext(context.Background(), ownerUserID, workspaceID, dedupeKey)
}

func (r *MemoryRepository) FindByDedupeKeyContext(ctx context.Context, ownerUserID, workspaceID, dedupeKey string) (*models.Operation, bool, error) {
	if err := intakeContextError(ctx); err != nil {
		return nil, false, err
	}
	if err := r.lockObservationContext(ctx); err != nil {
		return nil, false, err
	}
	defer r.mu.Unlock()
	if err := intakeContextError(ctx); err != nil {
		return nil, false, err
	}
	var found *models.Operation
	for _, op := range r.ops {
		if op.OwnerUserID != ownerUserID || op.WorkspaceID != workspaceID || op.DedupeKey != dedupeKey {
			continue
		}
		if op.Status == string(StatusArchived) || op.Status == string(StatusDismissed) {
			continue
		}
		if found == nil || op.CreatedAt.After(found.CreatedAt) {
			cp := cloneOperation(op)
			found = &cp
		}
	}
	if err := intakeContextError(ctx); err != nil {
		return nil, false, err
	}
	if found == nil {
		return nil, false, nil
	}
	return found, true, nil
}

func (r *MemoryRepository) List(f Filter) ([]models.Operation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []models.Operation
	for _, op := range r.ops {
		if op.OwnerUserID != f.OwnerUserID || op.WorkspaceID != f.WorkspaceID {
			continue
		}
		if f.Status != "" && op.Status != string(f.Status) {
			continue
		}
		if f.RiskLevel != "" && op.RiskLevel != string(f.RiskLevel) {
			continue
		}
		if f.OperationType != "" && op.OperationType != f.OperationType {
			continue
		}
		if f.RuntimeID != "" && op.RuntimeID != f.RuntimeID {
			continue
		}
		out = append(out, cloneOperation(op))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].UpdatedAt.Equal(out[j].UpdatedAt) {
			return out[i].ID.String() < out[j].ID.String()
		}
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	limit := f.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	out = applyWindow(out, f.Offset, limit)
	return out, nil
}

func (r *MemoryRepository) ListDue(ownerUserID, workspaceID string, limit int) ([]models.Operation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	actionable := map[string]bool{}
	for _, s := range actionableStatuses {
		actionable[s] = true
	}
	var out []models.Operation
	now := time.Now().UTC()
	for _, op := range r.ops {
		if op.OwnerUserID != ownerUserID || op.WorkspaceID != workspaceID {
			continue
		}
		if !actionable[op.Status] {
			continue
		}
		if op.NextReviewAt != nil && op.NextReviewAt.After(now) {
			continue
		}
		out = append(out, cloneOperation(op))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID.String() < out[j].ID.String()
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	out = applyWindow(out, 0, limit)
	return out, nil
}

func (r *MemoryRepository) Dashboard(ownerUserID, workspaceID string) (Dashboard, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	d := Dashboard{CountsByStatus: map[string]int{}, CountsByRisk: map[string]int{}}
	var all []models.Operation
	for _, op := range r.ops {
		if op.OwnerUserID != ownerUserID || op.WorkspaceID != workspaceID {
			continue
		}
		d.CountsByStatus[op.Status]++
		d.CountsByRisk[op.RiskLevel]++
		all = append(all, cloneOperation(op))
	}
	d.NeedsRobert = d.CountsByStatus[string(StatusAwaitingApproval)]
	d.DoneWhileAway = d.CountsByStatus[string(StatusCompleted)]
	d.Blocked = d.CountsByStatus[string(StatusBlocked)]
	d.Running = d.CountsByStatus[string(StatusRunning)]
	d.Failed = d.CountsByStatus[string(StatusFailed)]
	sort.Slice(all, func(i, j int) bool {
		if all[i].UpdatedAt.Equal(all[j].UpdatedAt) {
			return all[i].ID.String() < all[j].ID.String()
		}
		return all[i].UpdatedAt.After(all[j].UpdatedAt)
	})
	d.Recent = applyWindow(all, 0, 20)
	return d, nil
}

func (r *MemoryRepository) AppendEvent(evt *models.OperationEvent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if evt.ID == uuid.Nil {
		evt.ID = uuid.New()
	}
	r.events = append(r.events, *evt)
	return nil
}

func (r *MemoryRepository) ListEvents(operationID uuid.UUID, limit int) ([]models.OperationEvent, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []models.OperationEvent
	for _, e := range r.events {
		if e.OperationID == operationID {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	return applyWindow(out, 0, limit), nil
}

// ListEventsPage mirrors the production repository's bounded, deterministic
// pagination contract for audit-receipt verification.
func (r *MemoryRepository) ListEventsPage(operationID uuid.UUID, offset, limit int) ([]models.OperationEvent, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []models.OperationEvent
	for _, event := range r.events {
		if event.OperationID == operationID {
			out = append(out, event)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID.String() < out[j].ID.String()
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	return applyWindow(out, max(0, offset), limit), nil
}

func applyWindow[T any](items []T, offset, limit int) []T {
	offset = max(0, offset)
	if offset >= len(items) {
		return nil
	}
	items = items[offset:]
	if len(items) > limit {
		items = items[:limit]
	}
	return items
}
