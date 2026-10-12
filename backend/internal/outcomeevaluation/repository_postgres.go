package outcomeevaluation

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"automation-hub-backend/internal/infra"

	"gorm.io/gorm"
)

// PostgresRepository persists immutable, owner-scoped outcome evidence.
// Schema creation belongs to the versioned migration chain; this repository
// deliberately never creates, repairs, or weakens its required constraints.
type PostgresRepository struct {
	DB     *gorm.DB
	limits HistoryLimits
}

func NewPostgresRepository(db *gorm.DB) *PostgresRepository {
	repository, _ := NewPostgresRepositoryWithLimits(db, defaultHistoryLimits)
	return repository
}

func NewPostgresRepositoryWithLimits(db *gorm.DB, limits HistoryLimits) (*PostgresRepository, error) {
	if err := validateHistoryLimits(limits); err != nil {
		return nil, err
	}
	return &PostgresRepository{DB: db, limits: limits}, nil
}

// DefaultRepository opens the configured durable database. It never falls
// back to process memory when database configuration or migrations fail.
func DefaultRepository() (Repository, error) {
	db, err := infra.GetDefaultDB()
	if err != nil {
		return nil, fmt.Errorf("open outcome evaluation database: %w", err)
	}
	return NewPostgresRepository(db), nil
}

func (r *PostgresRepository) SaveOutcome(ctx context.Context, ownerID, workspaceID string, record OutcomeRevision, expectedRevision int64, token WriteToken) (OutcomeRevision, bool, error) {
	ownerID, workspaceID, outcomeID, err := validatePostgresScope(ownerID, workspaceID, record.Outcome.ID)
	if err != nil {
		return OutcomeRevision{}, false, err
	}
	if err := r.ready(ctx); err != nil {
		return OutcomeRevision{}, false, err
	}
	if err := validateWriteToken(token); err != nil {
		return OutcomeRevision{}, false, err
	}
	if record.Outcome.Scope != (Scope{OwnerID: ownerID, WorkspaceID: workspaceID}) {
		return OutcomeRevision{}, false, ErrScopeViolation
	}
	if expectedRevision < 0 || record.Revision != expectedRevision+1 {
		return OutcomeRevision{}, false, ErrRevisionConflict
	}
	if err := VerifyOutcomeRevisionDigest(record); err != nil {
		return OutcomeRevision{}, false, err
	}
	payload, err := marshalPostgresRecord("outcome revision", record)
	if err != nil {
		return OutcomeRevision{}, false, err
	}

	var stored OutcomeRevision
	created := false
	err = r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if existing, digest, found, loadErr := loadOutcomeByIdempotency(tx, ownerID, workspaceID, outcomeID, token.Key); loadErr != nil {
			return loadErr
		} else if found {
			if digest != token.RequestDigest {
				return ErrIdempotencyConflict
			}
			stored = existing
			return nil
		}

		result := tx.Exec(`
			INSERT INTO public.outcome_evaluation_outcome_revisions (
				owner_identity, workspace_id, outcome_id, revision,
				idempotency_key, request_digest, audit_digest,
				recorded_at, payload
			)
			SELECT ?, ?, ?, ?, ?, ?, ?, ?, CAST(? AS jsonb)
			WHERE COALESCE((
				SELECT revision
				FROM public.outcome_evaluation_outcome_revisions
				WHERE owner_identity = ? AND workspace_id = ? AND outcome_id = ?
				ORDER BY revision DESC LIMIT 1
			), 0) = ?
			ON CONFLICT DO NOTHING`,
			ownerID, workspaceID, outcomeID, record.Revision,
			token.Key, token.RequestDigest, record.AuditDigest,
			record.RecordedAt.UTC(), string(payload),
			ownerID, workspaceID, outcomeID, expectedRevision,
		)
		if result.Error != nil {
			return fmt.Errorf("store outcome revision: %w", result.Error)
		}
		if result.RowsAffected == 1 {
			stored = record
			created = true
			return nil
		}
		if existing, digest, found, loadErr := loadOutcomeByIdempotency(tx, ownerID, workspaceID, outcomeID, token.Key); loadErr != nil {
			return loadErr
		} else if found {
			if digest != token.RequestDigest {
				return ErrIdempotencyConflict
			}
			stored = existing
			return nil
		}
		return ErrRevisionConflict
	})
	if err != nil {
		return OutcomeRevision{}, false, err
	}
	return stored, created, nil
}

func (r *PostgresRepository) GetOutcome(ctx context.Context, ownerID, workspaceID, outcomeID string) (OutcomeRevision, error) {
	ownerID, workspaceID, outcomeID, err := validatePostgresScope(ownerID, workspaceID, outcomeID)
	if err != nil {
		return OutcomeRevision{}, err
	}
	if err := r.ready(ctx); err != nil {
		return OutcomeRevision{}, err
	}
	row, err := loadLatestOutcomeRow(r.DB.WithContext(ctx), ownerID, workspaceID, outcomeID)
	if err != nil {
		return OutcomeRevision{}, err
	}
	return decodeOutcomeRow(row, ownerID, workspaceID, outcomeID)
}

// ResolveOutcomeRevision returns only an exact historical revision/digest
// match. The query deliberately has no latest-revision fallback.
func (r *PostgresRepository) ResolveOutcomeRevision(ctx context.Context, ownerID, workspaceID, outcomeID string, revision int64, auditDigest string) (OutcomeRevision, error) {
	ownerID, workspaceID, outcomeID, err := validatePostgresScope(ownerID, workspaceID, outcomeID)
	if err != nil {
		return OutcomeRevision{}, err
	}
	if err := validateOutcomeRevisionSelector(revision, auditDigest); err != nil {
		return OutcomeRevision{}, err
	}
	if err := r.ready(ctx); err != nil {
		return OutcomeRevision{}, err
	}
	row, err := loadExactOutcomeRow(r.DB.WithContext(ctx), ownerID, workspaceID, outcomeID, revision, auditDigest)
	if err != nil {
		return OutcomeRevision{}, err
	}
	record, err := decodeOutcomeRow(row, ownerID, workspaceID, outcomeID)
	if err != nil {
		return OutcomeRevision{}, err
	}
	if record.Revision != revision || !equalSHA256(record.AuditDigest, auditDigest) {
		return OutcomeRevision{}, ErrNotFound
	}
	return record, nil
}

func (r *PostgresRepository) ListOutcomeHistory(ctx context.Context, ownerID, workspaceID, outcomeID string) ([]OutcomeRevision, error) {
	ownerID, workspaceID, outcomeID, err := validatePostgresScope(ownerID, workspaceID, outcomeID)
	if err != nil {
		return nil, err
	}
	if err := r.ready(ctx); err != nil {
		return nil, err
	}
	var rows []postgresOutcomeRow
	if err := r.DB.WithContext(ctx).Raw(`
		SELECT * FROM (
			SELECT owner_identity, workspace_id, outcome_id, revision,
				idempotency_key, request_digest, audit_digest,
				recorded_at, payload::text AS payload
			FROM public.outcome_evaluation_outcome_revisions
			WHERE owner_identity = ? AND workspace_id = ? AND outcome_id = ?
			ORDER BY revision DESC LIMIT ?
		) recent ORDER BY revision ASC`, ownerID, workspaceID, outcomeID, r.limits.OutcomeRevisions,
	).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("list outcome revision history: %w", err)
	}
	if len(rows) == 0 {
		return nil, ErrNotFound
	}
	result := make([]OutcomeRevision, 0, len(rows))
	for _, row := range rows {
		record, decodeErr := decodeOutcomeRow(row, ownerID, workspaceID, outcomeID)
		if decodeErr != nil {
			return nil, decodeErr
		}
		result = append(result, record)
	}
	return result, nil
}

func (r *PostgresRepository) AppendEvaluation(ctx context.Context, ownerID, workspaceID, outcomeID string, record EvaluationRecord, token WriteToken) (EvaluationRecord, bool, error) {
	ownerID, workspaceID, outcomeID, err := validatePostgresScope(ownerID, workspaceID, outcomeID)
	if err != nil {
		return EvaluationRecord{}, false, err
	}
	if err := r.ready(ctx); err != nil {
		return EvaluationRecord{}, false, err
	}
	if err := validateWriteToken(token); err != nil {
		return EvaluationRecord{}, false, err
	}
	if err := verifyEvaluationRecordScope(record, ownerID, workspaceID, outcomeID); err != nil {
		return EvaluationRecord{}, false, err
	}
	if record.OutcomeRevision < 1 {
		return EvaluationRecord{}, false, ErrRevisionConflict
	}
	payload, err := marshalPostgresRecord("evaluation record", record)
	if err != nil {
		return EvaluationRecord{}, false, err
	}

	var stored EvaluationRecord
	created := false
	err = r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if token.OutcomeAuditDigest != "" {
			if selectorErr := validateOutcomeRevisionSelector(record.OutcomeRevision, token.OutcomeAuditDigest); selectorErr != nil {
				return selectorErr
			}
			row, loadErr := loadExactOutcomeRow(tx, ownerID, workspaceID, outcomeID, record.OutcomeRevision, token.OutcomeAuditDigest)
			if loadErr != nil {
				return loadErr
			}
			if _, decodeErr := decodeOutcomeRow(row, ownerID, workspaceID, outcomeID); decodeErr != nil {
				return decodeErr
			}
		}
		if existing, digest, found, loadErr := loadEvaluationByIdempotency(tx, ownerID, workspaceID, outcomeID, token.Key); loadErr != nil {
			return loadErr
		} else if found {
			if digest != token.RequestDigest {
				return ErrIdempotencyConflict
			}
			if token.OutcomeAuditDigest != "" {
				if err := verifyPinnedEvaluationResult(existing, record); err != nil {
					return err
				}
			}
			stored = existing
			return nil
		}
		if token.OutcomeAuditDigest == "" {
			current, found, loadErr := loadCurrentRevision(tx, ownerID, workspaceID, outcomeID)
			if loadErr != nil {
				return loadErr
			}
			if !found {
				return ErrNotFound
			}
			if current != record.OutcomeRevision {
				return ErrRevisionConflict
			}
		}
		result := tx.Exec(`
			INSERT INTO public.outcome_evaluation_evaluations (
				owner_identity, workspace_id, outcome_id, evaluation_id,
				outcome_revision, idempotency_key, request_digest,
				evaluation_audit_digest, record_digest, recorded_at, payload
			)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CAST(? AS jsonb))
			ON CONFLICT DO NOTHING`,
			ownerID, workspaceID, outcomeID, record.Evaluation.ID,
			record.OutcomeRevision, token.Key, token.RequestDigest,
			record.Evaluation.AuditDigest, record.RecordDigest,
			record.RecordedAt.UTC(), string(payload),
		)
		if result.Error != nil {
			return fmt.Errorf("append outcome evaluation: %w", result.Error)
		}
		if result.RowsAffected == 1 {
			stored = record
			created = true
			return nil
		}
		if existing, digest, found, loadErr := loadEvaluationByIdempotency(tx, ownerID, workspaceID, outcomeID, token.Key); loadErr != nil {
			return loadErr
		} else if found {
			if digest != token.RequestDigest {
				return ErrIdempotencyConflict
			}
			if token.OutcomeAuditDigest != "" {
				if err := verifyPinnedEvaluationResult(existing, record); err != nil {
					return err
				}
			}
			stored = existing
			return nil
		}
		return ErrIdempotencyConflict
	})
	if err != nil {
		return EvaluationRecord{}, false, err
	}
	return stored, created, nil
}

func (r *PostgresRepository) GetEvaluation(ctx context.Context, ownerID, workspaceID, outcomeID, evaluationID string) (EvaluationRecord, error) {
	ownerID, workspaceID, outcomeID, err := validatePostgresScope(ownerID, workspaceID, outcomeID)
	if err != nil {
		return EvaluationRecord{}, err
	}
	if err := validateText("evaluation id", strings.TrimSpace(evaluationID), maxIDRunes+64, true); err != nil {
		return EvaluationRecord{}, err
	}
	if err := r.ready(ctx); err != nil {
		return EvaluationRecord{}, err
	}
	row, err := loadEvaluationRow(r.DB.WithContext(ctx), ownerID, workspaceID, outcomeID, strings.TrimSpace(evaluationID))
	if err != nil {
		return EvaluationRecord{}, err
	}
	return decodeEvaluationRow(row, ownerID, workspaceID, outcomeID)
}

func (r *PostgresRepository) ListEvaluations(ctx context.Context, ownerID, workspaceID, outcomeID string) ([]EvaluationRecord, error) {
	ownerID, workspaceID, outcomeID, err := validatePostgresScope(ownerID, workspaceID, outcomeID)
	if err != nil {
		return nil, err
	}
	if err := r.ready(ctx); err != nil {
		return nil, err
	}
	if _, found, err := loadCurrentRevision(r.DB.WithContext(ctx), ownerID, workspaceID, outcomeID); err != nil {
		return nil, err
	} else if !found {
		return nil, ErrNotFound
	}
	var rows []postgresEvaluationRow
	if err := r.DB.WithContext(ctx).Raw(`
		SELECT * FROM (
			SELECT owner_identity, workspace_id, outcome_id, evaluation_id,
				outcome_revision, idempotency_key, request_digest,
				evaluation_audit_digest, record_digest, recorded_at,
				payload::text AS payload
			FROM public.outcome_evaluation_evaluations
			WHERE owner_identity = ? AND workspace_id = ? AND outcome_id = ?
			ORDER BY recorded_at DESC, evaluation_id DESC LIMIT ?
		) recent ORDER BY recorded_at ASC, evaluation_id ASC`,
		ownerID, workspaceID, outcomeID, r.limits.Evaluations,
	).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("list outcome evaluations: %w", err)
	}
	result := make([]EvaluationRecord, 0, len(rows))
	for _, row := range rows {
		record, decodeErr := decodeEvaluationRow(row, ownerID, workspaceID, outcomeID)
		if decodeErr != nil {
			return nil, decodeErr
		}
		result = append(result, record)
	}
	return result, nil
}

func (r *PostgresRepository) AppendCorrection(ctx context.Context, ownerID, workspaceID, outcomeID string, record CorrectionRecord, token WriteToken) (CorrectionRecord, bool, error) {
	ownerID, workspaceID, outcomeID, err := validatePostgresScope(ownerID, workspaceID, outcomeID)
	if err != nil {
		return CorrectionRecord{}, false, err
	}
	if err := r.ready(ctx); err != nil {
		return CorrectionRecord{}, false, err
	}
	if err := validateWriteToken(token); err != nil {
		return CorrectionRecord{}, false, err
	}
	if err := verifyCorrectionRecordScope(record, ownerID, workspaceID, outcomeID); err != nil {
		return CorrectionRecord{}, false, err
	}
	if record.OutcomeRevision < 1 {
		return CorrectionRecord{}, false, ErrRevisionConflict
	}
	payload, err := marshalPostgresRecord("correction record", record)
	if err != nil {
		return CorrectionRecord{}, false, err
	}

	var stored CorrectionRecord
	created := false
	err = r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if existing, digest, found, loadErr := loadCorrectionByIdempotency(tx, ownerID, workspaceID, outcomeID, token.Key); loadErr != nil {
			return loadErr
		} else if found {
			if digest != token.RequestDigest {
				return ErrIdempotencyConflict
			}
			stored = existing
			return nil
		}
		result := tx.Exec(`
			INSERT INTO public.outcome_evaluation_corrections (
				owner_identity, workspace_id, outcome_id, correction_id,
				observation_id, outcome_revision, idempotency_key,
				request_digest, audit_digest, recorded_at, payload
			)
			SELECT ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CAST(? AS jsonb)
			WHERE (
				SELECT revision
				FROM public.outcome_evaluation_outcome_revisions
				WHERE owner_identity = ? AND workspace_id = ? AND outcome_id = ?
				ORDER BY revision DESC LIMIT 1
			) = ?
			ON CONFLICT DO NOTHING`,
			ownerID, workspaceID, outcomeID, record.Correction.ID,
			record.Observation.ID, record.OutcomeRevision, token.Key,
			token.RequestDigest, record.AuditDigest, record.RecordedAt.UTC(), string(payload),
			ownerID, workspaceID, outcomeID, record.OutcomeRevision,
		)
		if result.Error != nil {
			return fmt.Errorf("append outcome correction: %w", result.Error)
		}
		if result.RowsAffected == 1 {
			stored = record
			created = true
			return nil
		}
		if existing, digest, found, loadErr := loadCorrectionByIdempotency(tx, ownerID, workspaceID, outcomeID, token.Key); loadErr != nil {
			return loadErr
		} else if found {
			if digest != token.RequestDigest {
				return ErrIdempotencyConflict
			}
			stored = existing
			return nil
		}
		current, found, loadErr := loadCurrentRevision(tx, ownerID, workspaceID, outcomeID)
		if loadErr != nil {
			return loadErr
		}
		if !found {
			return ErrNotFound
		}
		if current != record.OutcomeRevision {
			return ErrRevisionConflict
		}
		return ErrIdempotencyConflict
	})
	if err != nil {
		return CorrectionRecord{}, false, err
	}
	return stored, created, nil
}

func (r *PostgresRepository) ListCorrections(ctx context.Context, ownerID, workspaceID, outcomeID string) ([]CorrectionRecord, error) {
	ownerID, workspaceID, outcomeID, err := validatePostgresScope(ownerID, workspaceID, outcomeID)
	if err != nil {
		return nil, err
	}
	if err := r.ready(ctx); err != nil {
		return nil, err
	}
	if _, found, err := loadCurrentRevision(r.DB.WithContext(ctx), ownerID, workspaceID, outcomeID); err != nil {
		return nil, err
	} else if !found {
		return nil, ErrNotFound
	}
	var rows []postgresCorrectionRow
	if err := r.DB.WithContext(ctx).Raw(`
		SELECT * FROM (
			SELECT owner_identity, workspace_id, outcome_id, correction_id,
				observation_id, outcome_revision, idempotency_key,
				request_digest, audit_digest, recorded_at,
				payload::text AS payload
			FROM public.outcome_evaluation_corrections
			WHERE owner_identity = ? AND workspace_id = ? AND outcome_id = ?
			ORDER BY recorded_at DESC, correction_id DESC LIMIT ?
		) recent ORDER BY recorded_at ASC, correction_id ASC`,
		ownerID, workspaceID, outcomeID, r.limits.Corrections,
	).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("list outcome corrections: %w", err)
	}
	result := make([]CorrectionRecord, 0, len(rows))
	for _, row := range rows {
		record, decodeErr := decodeCorrectionRow(row, ownerID, workspaceID, outcomeID)
		if decodeErr != nil {
			return nil, decodeErr
		}
		result = append(result, record)
	}
	return result, nil
}

type postgresOutcomeRow struct {
	OwnerIdentity  string
	WorkspaceID    string
	OutcomeID      string
	Revision       int64
	IdempotencyKey string
	RequestDigest  string
	AuditDigest    string
	RecordedAt     time.Time
	Payload        string
}

type postgresEvaluationRow struct {
	OwnerIdentity         string
	WorkspaceID           string
	OutcomeID             string
	EvaluationID          string
	OutcomeRevision       int64
	IdempotencyKey        string
	RequestDigest         string
	EvaluationAuditDigest string
	RecordDigest          string
	RecordedAt            time.Time
	Payload               string
}

type postgresCorrectionRow struct {
	OwnerIdentity   string
	WorkspaceID     string
	OutcomeID       string
	CorrectionID    string
	ObservationID   string
	OutcomeRevision int64
	IdempotencyKey  string
	RequestDigest   string
	AuditDigest     string
	RecordedAt      time.Time
	Payload         string
}

func (r *PostgresRepository) ready(ctx context.Context) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if r == nil || r.DB == nil {
		return errorsUnavailable()
	}
	return validateHistoryLimits(r.limits)
}

func validatePostgresScope(ownerID, workspaceID, outcomeID string) (string, string, string, error) {
	return validateServiceScope(ownerID, workspaceID, outcomeID)
}

func loadLatestOutcomeRow(db *gorm.DB, ownerID, workspaceID, outcomeID string) (postgresOutcomeRow, error) {
	var row postgresOutcomeRow
	err := db.Raw(`
		SELECT owner_identity, workspace_id, outcome_id, revision,
			idempotency_key, request_digest, audit_digest,
			recorded_at, payload::text AS payload
		FROM public.outcome_evaluation_outcome_revisions
		WHERE owner_identity = ? AND workspace_id = ? AND outcome_id = ?
		ORDER BY revision DESC LIMIT 1`, ownerID, workspaceID, outcomeID,
	).Row().Scan(&row.OwnerIdentity, &row.WorkspaceID, &row.OutcomeID, &row.Revision,
		&row.IdempotencyKey, &row.RequestDigest, &row.AuditDigest,
		&row.RecordedAt, &row.Payload)
	if errors.Is(err, sql.ErrNoRows) {
		return postgresOutcomeRow{}, ErrNotFound
	}
	if err != nil {
		return postgresOutcomeRow{}, fmt.Errorf("read outcome revision: %w", err)
	}
	return row, nil
}

func loadExactOutcomeRow(db *gorm.DB, ownerID, workspaceID, outcomeID string, revision int64, auditDigest string) (postgresOutcomeRow, error) {
	var row postgresOutcomeRow
	err := db.Raw(`
		SELECT owner_identity, workspace_id, outcome_id, revision,
			idempotency_key, request_digest, audit_digest,
			recorded_at, payload::text AS payload
		FROM public.outcome_evaluation_outcome_revisions
		WHERE owner_identity = ? AND workspace_id = ? AND outcome_id = ?
			AND revision = ? AND audit_digest = ?`,
		ownerID, workspaceID, outcomeID, revision, auditDigest,
	).Row().Scan(&row.OwnerIdentity, &row.WorkspaceID, &row.OutcomeID, &row.Revision,
		&row.IdempotencyKey, &row.RequestDigest, &row.AuditDigest,
		&row.RecordedAt, &row.Payload)
	if errors.Is(err, sql.ErrNoRows) {
		return postgresOutcomeRow{}, ErrNotFound
	}
	if err != nil {
		return postgresOutcomeRow{}, fmt.Errorf("read exact outcome revision: %w", err)
	}
	return row, nil
}

func loadOutcomeByIdempotency(db *gorm.DB, ownerID, workspaceID, outcomeID, key string) (OutcomeRevision, string, bool, error) {
	var row postgresOutcomeRow
	err := db.Raw(`
		SELECT owner_identity, workspace_id, outcome_id, revision,
			idempotency_key, request_digest, audit_digest,
			recorded_at, payload::text AS payload
		FROM public.outcome_evaluation_outcome_revisions
		WHERE owner_identity = ? AND workspace_id = ? AND outcome_id = ?
			AND idempotency_key = ?`, ownerID, workspaceID, outcomeID, key,
	).Row().Scan(&row.OwnerIdentity, &row.WorkspaceID, &row.OutcomeID, &row.Revision,
		&row.IdempotencyKey, &row.RequestDigest, &row.AuditDigest,
		&row.RecordedAt, &row.Payload)
	if errors.Is(err, sql.ErrNoRows) {
		return OutcomeRevision{}, "", false, nil
	}
	if err != nil {
		return OutcomeRevision{}, "", false, fmt.Errorf("resolve outcome idempotency: %w", err)
	}
	record, err := decodeOutcomeRow(row, ownerID, workspaceID, outcomeID)
	return record, row.RequestDigest, err == nil, err
}

func loadEvaluationByIdempotency(db *gorm.DB, ownerID, workspaceID, outcomeID, key string) (EvaluationRecord, string, bool, error) {
	var row postgresEvaluationRow
	err := scanEvaluationRow(db.Raw(`
		SELECT owner_identity, workspace_id, outcome_id, evaluation_id,
			outcome_revision, idempotency_key, request_digest,
			evaluation_audit_digest, record_digest, recorded_at,
			payload::text AS payload
		FROM public.outcome_evaluation_evaluations
		WHERE owner_identity = ? AND workspace_id = ? AND outcome_id = ?
			AND idempotency_key = ?`, ownerID, workspaceID, outcomeID, key).Row(), &row)
	if errors.Is(err, sql.ErrNoRows) {
		return EvaluationRecord{}, "", false, nil
	}
	if err != nil {
		return EvaluationRecord{}, "", false, fmt.Errorf("resolve evaluation idempotency: %w", err)
	}
	record, err := decodeEvaluationRow(row, ownerID, workspaceID, outcomeID)
	return record, row.RequestDigest, err == nil, err
}

func loadCorrectionByIdempotency(db *gorm.DB, ownerID, workspaceID, outcomeID, key string) (CorrectionRecord, string, bool, error) {
	var row postgresCorrectionRow
	err := scanCorrectionRow(db.Raw(`
		SELECT owner_identity, workspace_id, outcome_id, correction_id,
			observation_id, outcome_revision, idempotency_key,
			request_digest, audit_digest, recorded_at,
			payload::text AS payload
		FROM public.outcome_evaluation_corrections
		WHERE owner_identity = ? AND workspace_id = ? AND outcome_id = ?
			AND idempotency_key = ?`, ownerID, workspaceID, outcomeID, key).Row(), &row)
	if errors.Is(err, sql.ErrNoRows) {
		return CorrectionRecord{}, "", false, nil
	}
	if err != nil {
		return CorrectionRecord{}, "", false, fmt.Errorf("resolve correction idempotency: %w", err)
	}
	record, err := decodeCorrectionRow(row, ownerID, workspaceID, outcomeID)
	return record, row.RequestDigest, err == nil, err
}

func loadEvaluationRow(db *gorm.DB, ownerID, workspaceID, outcomeID, evaluationID string) (postgresEvaluationRow, error) {
	var row postgresEvaluationRow
	err := scanEvaluationRow(db.Raw(`
		SELECT owner_identity, workspace_id, outcome_id, evaluation_id,
			outcome_revision, idempotency_key, request_digest,
			evaluation_audit_digest, record_digest, recorded_at,
			payload::text AS payload
		FROM public.outcome_evaluation_evaluations
		WHERE owner_identity = ? AND workspace_id = ? AND outcome_id = ?
			AND evaluation_id = ?`, ownerID, workspaceID, outcomeID, evaluationID).Row(), &row)
	if errors.Is(err, sql.ErrNoRows) {
		return postgresEvaluationRow{}, ErrNotFound
	}
	if err != nil {
		return postgresEvaluationRow{}, fmt.Errorf("read outcome evaluation: %w", err)
	}
	return row, nil
}

type postgresRowScanner interface{ Scan(...any) error }

func scanEvaluationRow(row postgresRowScanner, target *postgresEvaluationRow) error {
	return row.Scan(&target.OwnerIdentity, &target.WorkspaceID, &target.OutcomeID,
		&target.EvaluationID, &target.OutcomeRevision, &target.IdempotencyKey,
		&target.RequestDigest, &target.EvaluationAuditDigest, &target.RecordDigest,
		&target.RecordedAt, &target.Payload)
}

func scanCorrectionRow(row postgresRowScanner, target *postgresCorrectionRow) error {
	return row.Scan(&target.OwnerIdentity, &target.WorkspaceID, &target.OutcomeID,
		&target.CorrectionID, &target.ObservationID, &target.OutcomeRevision,
		&target.IdempotencyKey, &target.RequestDigest, &target.AuditDigest,
		&target.RecordedAt, &target.Payload)
}

func loadCurrentRevision(db *gorm.DB, ownerID, workspaceID, outcomeID string) (int64, bool, error) {
	var revision int64
	err := db.Raw(`
		SELECT revision
		FROM public.outcome_evaluation_outcome_revisions
		WHERE owner_identity = ? AND workspace_id = ? AND outcome_id = ?
		ORDER BY revision DESC LIMIT 1`, ownerID, workspaceID, outcomeID,
	).Row().Scan(&revision)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("read current outcome revision: %w", err)
	}
	return revision, true, nil
}

func decodeOutcomeRow(row postgresOutcomeRow, ownerID, workspaceID, outcomeID string) (OutcomeRevision, error) {
	var record OutcomeRevision
	if err := decodeStrictPostgresJSON(row.Payload, &record); err != nil {
		return OutcomeRevision{}, corrupt("decode outcome revision", err)
	}
	if err := validateStoredWriteMetadata(row.IdempotencyKey, row.RequestDigest); err != nil {
		return OutcomeRevision{}, corrupt("outcome revision write metadata", err)
	}
	if row.OwnerIdentity != ownerID || row.WorkspaceID != workspaceID || row.OutcomeID != outcomeID ||
		record.Outcome.Scope != (Scope{OwnerID: ownerID, WorkspaceID: workspaceID}) || record.Outcome.ID != outcomeID ||
		record.Revision < 1 || row.Revision != record.Revision || row.AuditDigest != record.AuditDigest ||
		!postgresOutcomeTimeEqual(row.RecordedAt, record.RecordedAt) {
		return OutcomeRevision{}, corrupt("outcome revision metadata mismatch", nil)
	}
	if err := VerifyOutcomeRevisionDigest(record); err != nil {
		return OutcomeRevision{}, corrupt("outcome revision digest", err)
	}
	return record, nil
}

func decodeEvaluationRow(row postgresEvaluationRow, ownerID, workspaceID, outcomeID string) (EvaluationRecord, error) {
	var record EvaluationRecord
	if err := decodeStrictPostgresJSON(row.Payload, &record); err != nil {
		return EvaluationRecord{}, corrupt("decode evaluation record", err)
	}
	if err := validateStoredWriteMetadata(row.IdempotencyKey, row.RequestDigest); err != nil {
		return EvaluationRecord{}, corrupt("evaluation write metadata", err)
	}
	if row.OwnerIdentity != ownerID || row.WorkspaceID != workspaceID || row.OutcomeID != outcomeID ||
		record.Evaluation.Scope != (Scope{OwnerID: ownerID, WorkspaceID: workspaceID}) || record.Evaluation.OutcomeID != outcomeID ||
		row.EvaluationID != record.Evaluation.ID || row.OutcomeRevision != record.OutcomeRevision ||
		row.EvaluationAuditDigest != record.Evaluation.AuditDigest || row.RecordDigest != record.RecordDigest ||
		!postgresOutcomeTimeEqual(row.RecordedAt, record.RecordedAt) {
		return EvaluationRecord{}, corrupt("evaluation record metadata mismatch", nil)
	}
	if err := VerifyEvaluationRecordDigest(record); err != nil {
		return EvaluationRecord{}, corrupt("evaluation record digest", err)
	}
	return record, nil
}

func decodeCorrectionRow(row postgresCorrectionRow, ownerID, workspaceID, outcomeID string) (CorrectionRecord, error) {
	var record CorrectionRecord
	if err := decodeStrictPostgresJSON(row.Payload, &record); err != nil {
		return CorrectionRecord{}, corrupt("decode correction record", err)
	}
	if err := validateStoredWriteMetadata(row.IdempotencyKey, row.RequestDigest); err != nil {
		return CorrectionRecord{}, corrupt("correction write metadata", err)
	}
	expectedScope := Scope{OwnerID: ownerID, WorkspaceID: workspaceID}
	if row.OwnerIdentity != ownerID || row.WorkspaceID != workspaceID || row.OutcomeID != outcomeID ||
		record.OutcomeID != outcomeID || record.Observation.Scope != expectedScope || record.Correction.Scope != expectedScope ||
		row.CorrectionID != record.Correction.ID || row.ObservationID != record.Observation.ID ||
		row.OutcomeRevision != record.OutcomeRevision || row.AuditDigest != record.AuditDigest ||
		!postgresOutcomeTimeEqual(row.RecordedAt, record.RecordedAt) {
		return CorrectionRecord{}, corrupt("correction record metadata mismatch", nil)
	}
	if err := VerifyCorrectionRecordDigest(record); err != nil {
		return CorrectionRecord{}, corrupt("correction record digest", err)
	}
	return record, nil
}

func marshalPostgresRecord(label string, value any) ([]byte, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode %s: %w", label, err)
	}
	return payload, nil
}

func decodeStrictPostgresJSON(payload string, target any) error {
	payload = strings.TrimSpace(payload)
	if payload == "" || payload == "null" {
		return fmt.Errorf("JSON object required")
	}
	decoder := json.NewDecoder(bytes.NewBufferString(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			err = fmt.Errorf("multiple JSON values")
		}
		return err
	}
	return nil
}

func corrupt(message string, cause error) error {
	if cause == nil {
		return fmt.Errorf("%w: %s", ErrIntegrityViolation, message)
	}
	return fmt.Errorf("%w: %s: %v", ErrIntegrityViolation, message, cause)
}

func validateStoredWriteMetadata(key, digest string) error {
	if key != strings.TrimSpace(key) {
		return fmt.Errorf("idempotency key is not canonical")
	}
	return validateWriteToken(WriteToken{Key: key, RequestDigest: digest})
}

func postgresOutcomeTimeEqual(left, right time.Time) bool {
	return left.UTC().Truncate(time.Microsecond).Equal(right.UTC().Truncate(time.Microsecond))
}

var _ Repository = (*PostgresRepository)(nil)
var _ OutcomeRevisionResolver = (*PostgresRepository)(nil)
