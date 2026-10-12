package operations

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"automation-hub-backend/internal/infra"
	"automation-hub-backend/internal/models"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ErrNotFound is returned when an operation does not exist.
var ErrNotFound = errors.New("operations: not found")

// ErrDuplicateDedupeKey means another active operation already represents the
// same owner-scoped source revision. Callers must load that operation instead
// of treating the collision as a failed ingestion.
var ErrDuplicateDedupeKey = errors.New("operations: duplicate active dedupe key")

// ErrAtomicCreationUnsupported rejects creation without a durable audit pair.
var ErrAtomicCreationUnsupported = errors.New("operations: repository does not support atomic creation")

// ErrInvalidCreationEvent rejects mismatched or non-initial creation pairs.
var ErrInvalidCreationEvent = errors.New("operations: creation event must match a new operation")

// ErrAtomicMutationsUnsupported rejects unauditable service mutations.
var ErrAtomicMutationsUnsupported = errors.New("operations: repository does not support atomic mutations")

// ErrContextIntakeUnsupported rejects intake through a store that cannot carry
// request cancellation through lookup and both atomic write paths.
var ErrContextIntakeUnsupported = errors.New("operations: repository does not support contextual intake")

// ErrSourceIdentityMigrationRequired fences ambiguous historical source keys.
// Intake must not rekey or overwrite the existing operation automatically.
var ErrSourceIdentityMigrationRequired = errors.New("operations: source identity migration required")

// ErrInvalidRepositoryResult rejects ambiguous or incorrectly scoped results.
// A successful repository call is not permission to return another owner's row.
var ErrInvalidRepositoryResult = errors.New("operations: repository returned inconsistent scope or identity")

func validateIntakeLookup(op *models.Operation, found bool, owner, workspace, key string) error {
	if !found {
		if op != nil {
			return ErrInvalidRepositoryResult
		}
		return nil
	}
	if op == nil || op.ID == uuid.Nil || op.OwnerUserID != owner || op.WorkspaceID != workspace || op.DedupeKey != key ||
		op.Status == string(StatusArchived) || op.Status == string(StatusDismissed) {
		return ErrInvalidRepositoryResult
	}
	if validateOperationSourceIdentity(*op) != nil {
		return ErrInvalidRepositoryResult
	}
	return nil
}

func validateIntakeWriteResult(op *models.Operation, expected models.Operation) error {
	if err := validateIntakeLookup(op, true, expected.OwnerUserID, expected.WorkspaceID, expected.DedupeKey); err != nil {
		return err
	}
	if op.ID != expected.ID || op.Version != expected.Version {
		return ErrInvalidRepositoryResult
	}
	if !sameSourceIdentity(*op, expected) || !sameSourceObservation(*op, expected) || (expected.SourceIdentityHash != "" && op.SourceRevisionHash != expected.SourceRevisionHash) {
		return ErrInvalidRepositoryResult
	}
	return nil
}

// ErrInvalidMutationEvent rejects an audit row unrelated to the mutation.
var ErrInvalidMutationEvent = errors.New("operations: mutation event must match the operation and resulting status")

// Filter narrows a List query.
type Filter struct {
	OwnerUserID   string
	WorkspaceID   string
	Status        OperationStatus // optional
	RiskLevel     RiskLevel       // optional
	OperationType string          // optional
	RuntimeID     string          // optional
	Limit         int
	Offset        int
}

// Dashboard is the Background Operations dashboard roll-up (§24).
type Dashboard struct {
	CountsByStatus map[string]int     `json:"countsByStatus"`
	CountsByRisk   map[string]int     `json:"countsByRisk"`
	NeedsRobert    int                `json:"needsRobert"`
	DoneWhileAway  int                `json:"doneWhileAway"`
	Blocked        int                `json:"blocked"`
	Running        int                `json:"running"`
	Failed         int                `json:"failed"`
	Recent         []models.Operation `json:"recent"`
}

// Repository is the Operation Ledger persistence contract (§10.8).
type Repository interface {
	Create(op *models.Operation) (*models.Operation, error)
	Update(op *models.Operation) (*models.Operation, error)
	GetByID(ownerUserID, workspaceID string, id uuid.UUID) (*models.Operation, error)
	FindByDedupeKey(ownerUserID, workspaceID, dedupeKey string) (*models.Operation, bool, error)
	List(f Filter) ([]models.Operation, error)
	ListDue(ownerUserID, workspaceID string, limit int) ([]models.Operation, error)
	Dashboard(ownerUserID, workspaceID string) (Dashboard, error)
	AppendEvent(evt *models.OperationEvent) error
	ListEvents(operationID uuid.UUID, limit int) ([]models.OperationEvent, error)
}

// PaginatedEventRepository provides deterministic, bounded access to an
// operation's immutable audit history. Security-sensitive receipt validation
// must not rely on ListEvents' legacy first-page cap.
type PaginatedEventRepository interface {
	ListEventsPage(operationID uuid.UUID, offset, limit int) ([]models.OperationEvent, error)
}

// AtomicMutationRepository is an optional extension for unclaimed mutations.
// UpdateWithEvent must persist both rows or neither, retaining version, identity,
// and execution-claim guards. Transition/Save never fall back to separate writes.
type AtomicMutationRepository interface {
	UpdateWithEvent(op *models.Operation, event *models.OperationEvent) (*models.Operation, error)
}

// AtomicCreationRepository persists an initial operation and audit row together.
// Ingest never falls back to independent Create and AppendEvent calls.
type AtomicCreationRepository interface {
	CreateWithEvent(op *models.Operation, event *models.OperationEvent) (*models.Operation, error)
}

// ContextIntakeRepository is the optional, explicit IngestContext contract.
// Implementations must carry ctx through lookup and atomic operation/audit writes,
// preserving the creation, version, identity and execution-claim guards above.
// Nil/expired contexts and ended private observation scopes cannot access storage;
// detached contexts must still respect the original private server authority.
// Decorators must override these methods to intercept contextual intake; legacy
// overrides alone intercept only Ingest. No repository rebinding is performed.
type ContextIntakeRepository interface {
	FindByDedupeKeyContext(ctx context.Context, ownerUserID, workspaceID, dedupeKey string) (*models.Operation, bool, error)
	CreateWithEventContext(ctx context.Context, op *models.Operation, event *models.OperationEvent) (*models.Operation, error)
	UpdateWithEventContext(ctx context.Context, op *models.Operation, event *models.OperationEvent) (*models.Operation, error)
}

var _ ContextIntakeRepository = (*GormRepository)(nil)
var _ ContextIntakeRepository = (*MemoryRepository)(nil)
var _ PaginatedEventRepository = (*GormRepository)(nil)
var _ PaginatedEventRepository = (*MemoryRepository)(nil)

var _ AtomicCreationRepository = (*GormRepository)(nil)
var _ AtomicCreationRepository = (*MemoryRepository)(nil)

var _ AtomicMutationRepository = (*GormRepository)(nil)
var _ AtomicMutationRepository = (*MemoryRepository)(nil)

func validateCreationEvent(op *models.Operation, event *models.OperationEvent) error {
	if op == nil || op.ID == uuid.Nil || op.Version != 1 || op.Status != string(StatusNew) {
		return ErrInvalidCreationEvent
	}
	if err := Validate(*op); err != nil {
		return err
	}
	if strings.TrimSpace(op.OperationType) == "" || strings.TrimSpace(op.SourceType) == "" || !json.Valid([]byte(op.EvidenceJSON)) {
		return ErrInvalidCreationEvent
	}
	if event == nil || event.OperationID != op.ID || event.EventType != "created" ||
		event.ActorType != string(OwnerHAI) || event.BeforeStatus != "" || event.AfterStatus != op.Status ||
		!json.Valid([]byte(event.PayloadJSON)) {
		return ErrInvalidCreationEvent
	}
	return nil
}

func validateMutationEvent(op *models.Operation, event *models.OperationEvent) error {
	if op == nil || op.ID == uuid.Nil || op.Version <= 1 {
		return ErrStaleOperation
	}
	if event == nil || event.OperationID != op.ID || event.AfterStatus != op.Status {
		return ErrInvalidMutationEvent
	}
	return nil
}

// Version matching alone cannot authenticate a caller-supplied before-status.
// Check the locked record before any write, including non-transition saves.
func validateMutationState(current, op models.Operation, event *models.OperationEvent) error {
	if err := validateMutationEvent(&op, event); err != nil {
		return err
	}
	if err := validateSourceIdentityMutation(current, op); err != nil {
		return err
	}
	if event.EventType == "status_change" {
		if event.BeforeStatus != current.Status {
			return ErrStaleOperation
		}
		before := op
		before.Status = current.Status
		_, _, err := ApplyTransition(before, OperationStatus(op.Status), event.ActorType, event.ActorID, event.Message, event.CreatedAt)
		return err
	}
	if op.Status != current.Status || (event.BeforeStatus != "" && event.BeforeStatus != current.Status) {
		return ErrInvalidMutationEvent
	}
	return nil
}

// GormRepository is the Postgres-backed Repository.
type GormRepository struct{ DB *gorm.DB }

// NewGormRepository builds a repository over db.
func NewGormRepository(db *gorm.DB) *GormRepository { return &GormRepository{DB: db} }

func (r *GormRepository) FindByDedupeKeyContext(ctx context.Context, ownerUserID, workspaceID, dedupeKey string) (*models.Operation, bool, error) {
	if err := intakeContextError(ctx); err != nil {
		return nil, false, err
	}
	if r == nil || r.DB == nil {
		return nil, false, ErrContextIntakeUnsupported
	}
	bound := NewGormRepository(r.DB.WithContext(ctx))
	op, found, err := bound.FindByDedupeKey(ownerUserID, workspaceID, dedupeKey)
	if err == nil {
		err = intakeContextError(ctx)
	}
	if err != nil {
		return nil, false, err
	}
	return op, found, nil
}

func (r *GormRepository) CreateWithEventContext(ctx context.Context, op *models.Operation, event *models.OperationEvent) (*models.Operation, error) {
	if err := intakeContextError(ctx); err != nil {
		return nil, err
	}
	if r == nil || r.DB == nil {
		return nil, ErrContextIntakeUnsupported
	}
	bound := NewGormRepository(r.DB.WithContext(ctx))
	return bound.CreateWithEvent(op, event)
}

func (r *GormRepository) UpdateWithEventContext(ctx context.Context, op *models.Operation, event *models.OperationEvent) (*models.Operation, error) {
	if err := intakeContextError(ctx); err != nil {
		return nil, err
	}
	if r == nil || r.DB == nil {
		return nil, ErrContextIntakeUnsupported
	}
	bound := NewGormRepository(r.DB.WithContext(ctx))
	return bound.UpdateWithEvent(op, event)
}

// DefaultRepository builds a repository over the default DB.
func DefaultRepository() Repository {
	db, err := infra.GetDefaultDB()
	if err != nil {
		panic(err)
	}
	return NewGormRepository(db)
}

func (r *GormRepository) Create(op *models.Operation) (*models.Operation, error) {
	if op == nil {
		return nil, ErrInvalidRepositoryResult
	}
	if err := validateOperationSourceIdentity(*op); err != nil {
		return nil, err
	}
	if err := validateGormObservationReference(r.DB, *op); err != nil {
		return nil, err
	}
	if err := r.DB.Create(op).Error; err != nil {
		if isActiveDedupeConflict(err) {
			return nil, ErrDuplicateDedupeKey
		}
		return nil, err
	}
	return op, nil
}

func (r *GormRepository) CreateWithEvent(op *models.Operation, event *models.OperationEvent) (*models.Operation, error) {
	if err := validateCreationEvent(op, event); err != nil {
		return nil, err
	}
	created, audit := *op, *event
	err := r.DB.Transaction(func(tx *gorm.DB) error {
		if err := intakeContextError(tx.Statement.Context); err != nil {
			return err
		}
		if err := lockIntakeSourceAuthority(tx, created); err != nil {
			return err
		}
		if err := validateGormObservationReference(tx, created); err != nil {
			return err
		}
		if err := tx.Create(&created).Error; err != nil {
			if isActiveDedupeConflict(err) {
				return ErrDuplicateDedupeKey
			}
			return fmt.Errorf("create audited operation: %w", err)
		}
		if err := intakeContextError(tx.Statement.Context); err != nil {
			return err
		}
		if err := tx.Create(&audit).Error; err != nil {
			return fmt.Errorf("append operation creation event: %w", err)
		}
		return intakeContextError(tx.Statement.Context)
	})
	if err != nil {
		return nil, err
	}
	return &created, nil
}

func (r *GormRepository) Update(op *models.Operation) (*models.Operation, error) {
	return r.update(op, nil)
}

func (r *GormRepository) UpdateWithEvent(op *models.Operation, event *models.OperationEvent) (*models.Operation, error) {
	if err := validateMutationEvent(op, event); err != nil {
		return nil, err
	}
	return r.update(op, event)
}

func (r *GormRepository) update(op *models.Operation, event *models.OperationEvent) (*models.Operation, error) {
	if op == nil || op.ID == uuid.Nil || op.Version <= 1 {
		return nil, ErrStaleOperation
	}
	updated := *op
	err := r.DB.Transaction(func(tx *gorm.DB) error {
		if err := intakeContextError(tx.Statement.Context); err != nil {
			return err
		}
		if err := lockIntakeSourceAuthority(tx, updated); err != nil {
			return err
		}
		var current models.Operation
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ?", op.ID).First(&current).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return fmt.Errorf("lock operation before update: %w", err)
		}
		if current.Version != op.Version-1 {
			return ErrStaleOperation
		}
		if current.OwnerUserID != op.OwnerUserID || current.WorkspaceID != op.WorkspaceID {
			return fmt.Errorf("operations: owner and workspace are immutable")
		}
		if err := validateSourceIdentityMutation(current, *op); err != nil {
			return err
		}
		if event != nil {
			if err := validateMutationState(current, *op, event); err != nil {
				return err
			}
		}
		var claimOwner sql.NullString
		err := tx.Raw(`SELECT claim_owner::text FROM public.operation_execution_claims WHERE operation_id = ? FOR UPDATE`, op.ID).
			Row().Scan(&claimOwner)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("check operation execution claim before update: %w", err)
		}
		if err == nil && claimOwner.Valid && claimOwner.String != "" {
			return ErrOperationClaimed
		}
		updated.CreatedAt = current.CreatedAt
		if err := intakeContextError(tx.Statement.Context); err != nil {
			return err
		}
		if err := updateOperationCAS(tx, updated, current.Version); err != nil {
			return err
		}
		if event != nil {
			if err := intakeContextError(tx.Statement.Context); err != nil {
				return err
			}
			audit := *event
			if err := tx.Create(&audit).Error; err != nil {
				return fmt.Errorf("append operation mutation event: %w", err)
			}
		}
		return intakeContextError(tx.Statement.Context)
	})
	if err != nil {
		return nil, err
	}
	return &updated, nil
}

func (r *GormRepository) GetByID(ownerUserID, workspaceID string, id uuid.UUID) (*models.Operation, error) {
	var op models.Operation
	err := r.DB.Where("id = ? AND owner_user_id = ? AND workspace_id = ?", id, ownerUserID, workspaceID).First(&op).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &op, nil
}

func (r *GormRepository) FindByDedupeKey(ownerUserID, workspaceID, dedupeKey string) (*models.Operation, bool, error) {
	var op models.Operation
	err := r.DB.
		Where("owner_user_id = ? AND workspace_id = ? AND dedupe_key = ? AND status NOT IN ?", ownerUserID, workspaceID, dedupeKey, []string{string(StatusArchived), string(StatusDismissed)}).
		Order("created_at DESC").First(&op).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return &op, true, nil
}

func isActiveDedupeConflict(err error) bool {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		return postgresError.Code == "23505" && postgresError.ConstraintName == "uq_operations_owner_workspace_dedupe_active"
	}
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), `duplicate key value violates unique constraint "uq_operations_owner_workspace_dedupe_active"`)
}

func (r *GormRepository) List(f Filter) ([]models.Operation, error) {
	q := r.DB.Where("owner_user_id = ? AND workspace_id = ?", f.OwnerUserID, f.WorkspaceID)
	if f.Status != "" {
		q = q.Where("status = ?", string(f.Status))
	}
	if f.RiskLevel != "" {
		q = q.Where("risk_level = ?", string(f.RiskLevel))
	}
	if f.OperationType != "" {
		q = q.Where("operation_type = ?", f.OperationType)
	}
	if f.RuntimeID != "" {
		q = q.Where("runtime_id = ?", f.RuntimeID)
	}
	limit := f.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var ops []models.Operation
	if err := q.Order("updated_at DESC").Order("id ASC").Limit(limit).Offset(max(0, f.Offset)).Find(&ops).Error; err != nil {
		return nil, err
	}
	return ops, nil
}

// actionableStatuses are the statuses the background loop may still progress.
var actionableStatuses = []string{
	string(StatusNew), string(StatusClassified), string(StatusReady),
	string(StatusApproved), string(StatusVerifying), string(StatusWaitingExternal),
	string(StatusInterrupted), string(StatusFailed),
}

func (r *GormRepository) ListDue(ownerUserID, workspaceID string, limit int) ([]models.Operation, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var ops []models.Operation
	err := r.DB.
		Where("owner_user_id = ? AND workspace_id = ? AND status IN ?", ownerUserID, workspaceID, actionableStatuses).
		Where("next_review_at IS NULL OR next_review_at <= now()").
		Order("created_at ASC").Order("id ASC").Limit(limit).Find(&ops).Error
	if err != nil {
		return nil, err
	}
	return ops, nil
}

func (r *GormRepository) Dashboard(ownerUserID, workspaceID string) (Dashboard, error) {
	d := Dashboard{CountsByStatus: map[string]int{}, CountsByRisk: map[string]int{}}
	type row struct {
		Key string
		N   int
	}
	var byStatus []row
	if err := r.DB.Model(&models.Operation{}).
		Select("status as key, count(*) as n").
		Where("owner_user_id = ? AND workspace_id = ?", ownerUserID, workspaceID).
		Group("status").Scan(&byStatus).Error; err != nil {
		return d, err
	}
	for _, rrow := range byStatus {
		d.CountsByStatus[rrow.Key] = rrow.N
	}
	var byRisk []row
	if err := r.DB.Model(&models.Operation{}).
		Select("risk_level as key, count(*) as n").
		Where("owner_user_id = ? AND workspace_id = ?", ownerUserID, workspaceID).
		Group("risk_level").Scan(&byRisk).Error; err != nil {
		return d, err
	}
	for _, rrow := range byRisk {
		d.CountsByRisk[rrow.Key] = rrow.N
	}
	d.NeedsRobert = d.CountsByStatus[string(StatusAwaitingApproval)]
	d.DoneWhileAway = d.CountsByStatus[string(StatusCompleted)]
	d.Blocked = d.CountsByStatus[string(StatusBlocked)]
	d.Running = d.CountsByStatus[string(StatusRunning)]
	d.Failed = d.CountsByStatus[string(StatusFailed)]

	if err := r.DB.
		Where("owner_user_id = ? AND workspace_id = ?", ownerUserID, workspaceID).
		Order("updated_at DESC").Order("id ASC").Limit(20).Find(&d.Recent).Error; err != nil {
		return d, err
	}
	return d, nil
}

func (r *GormRepository) AppendEvent(evt *models.OperationEvent) error {
	return r.DB.Create(evt).Error
}

func (r *GormRepository) ListEvents(operationID uuid.UUID, limit int) ([]models.OperationEvent, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	var events []models.OperationEvent
	err := r.DB.Where("operation_id = ?", operationID).
		Order("created_at ASC").Limit(limit).Find(&events).Error
	if err != nil {
		return nil, err
	}
	return events, nil
}

// ListEventsPage returns one bounded audit page in a stable order.
func (r *GormRepository) ListEventsPage(operationID uuid.UUID, offset, limit int) ([]models.OperationEvent, error) {
	if r == nil || r.DB == nil {
		return nil, ErrNotFound
	}
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	var events []models.OperationEvent
	err := r.DB.Where("operation_id = ?", operationID).
		Order("created_at ASC").Order("id ASC").Limit(limit).Offset(max(0, offset)).Find(&events).Error
	if err != nil {
		return nil, err
	}
	return events, nil
}
