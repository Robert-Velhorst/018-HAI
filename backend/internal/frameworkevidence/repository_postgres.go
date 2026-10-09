package frameworkevidence

import (
	"context"
	"fmt"
	"time"

	"automation-hub-backend/internal/infra"

	"gorm.io/gorm"
)

type GormRepository struct {
	db *gorm.DB
}

var _ Repository = (*GormRepository)(nil)

type postgresRecord struct {
	ContractVersion      int       `gorm:"column:contract_version"`
	OwnerIdentity        string    `gorm:"column:owner_identity"`
	TaskPlanID           string    `gorm:"column:task_plan_id"`
	FrameworkSelectionID string    `gorm:"column:framework_selection_id"`
	PreflightDigest      string    `gorm:"column:preflight_digest"`
	Status               string    `gorm:"column:status"`
	AssertionsJSON       []byte    `gorm:"column:assertions_json"`
	EvaluatedAt          time.Time `gorm:"column:evaluated_at"`
	CreatedAt            time.Time `gorm:"column:created_at"`
}

func NewGormRepository(db *gorm.DB) *GormRepository {
	return &GormRepository{db: db}
}

// DefaultRepository opens the configured canonical database and applies the
// versioned migration chain. It never falls back to volatile storage.
func DefaultRepository() (Repository, error) {
	db, err := infra.GetDefaultDB()
	if err != nil {
		return nil, fmt.Errorf("open framework evidence preflight database: %w", err)
	}
	return NewGormRepository(db), nil
}

func (repository *GormRepository) Store(ctx context.Context, record Record) error {
	if err := repository.ready(); err != nil {
		return err
	}
	if err := contextError(ctx); err != nil {
		return err
	}
	normalized, err := normalizeRecord(record)
	if err != nil {
		return err
	}
	createdAt := time.Now().UTC().Truncate(time.Microsecond)
	result := repository.db.WithContext(ctx).Exec(`
		INSERT INTO public.framework_evidence_preflights (
			contract_version, owner_identity, task_plan_id,
			framework_selection_id, preflight_digest, status,
			assertions_json, evaluated_at, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (
			owner_identity, task_plan_id, framework_selection_id, preflight_digest
		) DO NOTHING`,
		normalized.ContractVersion,
		normalized.OwnerIdentity,
		normalized.TaskPlanID,
		normalized.FrameworkSelectionID,
		normalized.PreflightDigest,
		normalized.Status,
		[]byte(normalized.AssertionsJSON),
		normalized.EvaluatedAt,
		createdAt,
	)
	if result.Error != nil {
		return fmt.Errorf("store framework evidence preflight: %w", result.Error)
	}
	if result.RowsAffected == 1 {
		return nil
	}

	existing, err := repository.Resolve(
		ctx,
		normalized.OwnerIdentity,
		normalized.TaskPlanID,
		normalized.FrameworkSelectionID,
		normalized.PreflightDigest,
	)
	if err != nil {
		return fmt.Errorf("resolve framework evidence preflight replay: %w", err)
	}
	if !sameSemanticRecord(existing, normalized) {
		return ErrConflict
	}
	return nil
}

func (repository *GormRepository) Resolve(
	ctx context.Context,
	ownerIdentity string,
	taskPlanID string,
	frameworkSelectionID string,
	preflightDigest string,
) (Record, error) {
	if err := repository.ready(); err != nil {
		return Record{}, err
	}
	if err := contextError(ctx); err != nil {
		return Record{}, err
	}
	ownerIdentity, taskPlanID, frameworkSelectionID, preflightDigest, err := normalizeLookup(
		ownerIdentity,
		taskPlanID,
		frameworkSelectionID,
		preflightDigest,
	)
	if err != nil {
		return Record{}, err
	}
	db, cleanup, err := infra.PostgresExecutionDB(ctx, repository.db)
	if err != nil {
		return Record{}, err
	}
	defer cleanup()

	var row postgresRecord
	query := db.Raw(`
		SELECT contract_version, owner_identity, task_plan_id,
			framework_selection_id, preflight_digest, status,
			assertions_json, evaluated_at, created_at
		FROM public.framework_evidence_preflights
		WHERE owner_identity = ?
		  AND task_plan_id = ?
		  AND framework_selection_id = ?
		  AND preflight_digest = ?`,
		ownerIdentity,
		taskPlanID,
		frameworkSelectionID,
		preflightDigest,
	).Scan(&row)
	if query.Error != nil {
		return Record{}, fmt.Errorf("resolve framework evidence preflight: %w", query.Error)
	}
	if query.RowsAffected != 1 {
		return Record{}, ErrNotFound
	}
	record := recordFromPostgres(row)
	normalized, err := normalizeRecord(record)
	if err != nil {
		return Record{}, fmt.Errorf("%w: persisted record failed validation: %v", ErrConflict, err)
	}
	if normalized.OwnerIdentity != ownerIdentity ||
		normalized.TaskPlanID != taskPlanID ||
		normalized.FrameworkSelectionID != frameworkSelectionID ||
		normalized.PreflightDigest != preflightDigest {
		return Record{}, fmt.Errorf("%w: persisted record escaped owner-scoped tuple", ErrConflict)
	}
	return cloneRecord(normalized), nil
}

func (repository *GormRepository) ready() error {
	if repository == nil || repository.db == nil {
		return ErrRepositoryUnavailable
	}
	return nil
}

func recordFromPostgres(row postgresRecord) Record {
	return Record{
		ContractVersion:      row.ContractVersion,
		OwnerIdentity:        row.OwnerIdentity,
		TaskPlanID:           row.TaskPlanID,
		FrameworkSelectionID: row.FrameworkSelectionID,
		PreflightDigest:      row.PreflightDigest,
		Status:               row.Status,
		AssertionsJSON:       cloneBytes(row.AssertionsJSON),
		EvaluatedAt:          row.EvaluatedAt.UTC(),
		CreatedAt:            row.CreatedAt.UTC(),
	}
}
