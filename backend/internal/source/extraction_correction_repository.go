package source

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"database/sql/driver"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"time"

	"automation-hub-backend/internal/memory"
	"automation-hub-backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

var errExtractionCorrectionLeaseLost = errors.New("source correction worker lease is no longer owned")

// extractionCorrectionWorkerFence is held on a dedicated PostgreSQL session
// for the full worker run. Deletion takes the matching transaction try-lock.
type extractionCorrectionWorkerFence interface {
	AcquireExtractionCorrectionSessionLock(context.Context, string, uuid.UUID) (release func(), acquired bool, err error)
}

type extractionCorrectionWorkerPoolCapacity interface {
	RequireExtractionCorrectionWorkerPoolCapacity() error
}

type extractionCorrectionLessonPersister interface {
	PersistExtractionCorrectionLesson(
		context.Context,
		uuid.UUID,
		uuid.UUID,
		string,
		int64,
		time.Time,
		string,
		string,
		memory.CreateRequest,
	) (*models.ContextMemory, error)
}

func extractionCorrectionWorkerLockKey(owner string, extractionID uuid.UUID) string {
	digest := sha256.Sum256([]byte(owner + "\x00" + extractionID.String()))
	return hex.EncodeToString(digest[:])
}

// RequireExtractionCorrectionWorkerPoolCapacity checks the full worker shape
// before it pins either advisory-lock connection. The worker needs those two
// sessions plus one connection for its durable state queries and writes.
func (r *GormRepository) RequireExtractionCorrectionWorkerPoolCapacity() error {
	if r == nil || r.DB == nil {
		return errors.New("source correction repository is unavailable")
	}
	sqlDB, err := r.DB.DB()
	if err != nil {
		return fmt.Errorf("inspect source correction worker connection pool: %w", err)
	}
	if maxOpen := sqlDB.Stats().MaxOpenConnections; maxOpen > 0 && maxOpen < 3 {
		return errors.New("source correction worker requires at least three database connections for the source lease, extraction fence, and work query")
	}
	return nil
}

// AcquireExtractionCorrectionSessionLock serializes correction workers and
// owner-scoped deletion. Callers must invoke release on every path after a
// successful acquisition. The dedicated connection is discarded if unlock
// cannot be confirmed, so an uncertain session lock cannot leak into the pool.
func (r *GormRepository) AcquireExtractionCorrectionSessionLock(
	ctx context.Context,
	ownerIdentity string,
	extractionID uuid.UUID,
) (release func(), acquired bool, err error) {
	if ctx == nil || ownerIdentity == "" || extractionID == uuid.Nil {
		return nil, false, errors.New("source correction worker lock request is invalid")
	}
	if r == nil || r.DB == nil || r.DB.Dialector == nil {
		return nil, false, errors.New("source correction repository is unavailable")
	}
	if r.DB.Dialector.Name() != "postgres" {
		return nil, false, errors.New("source correction worker requires PostgreSQL session advisory locks")
	}
	if err := r.RequireExtractionCorrectionWorkerPoolCapacity(); err != nil {
		return nil, false, err
	}
	sqlDB, err := r.DB.DB()
	if err != nil {
		return nil, false, fmt.Errorf("open source correction worker lock connection: %w", err)
	}
	conn, err := sqlDB.Conn(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("open source correction worker lock connection: %w", err)
	}
	if err := ctx.Err(); err != nil {
		_ = conn.Close()
		return nil, false, err
	}
	lockKey := extractionCorrectionWorkerLockKey(ownerIdentity, extractionID)
	var lockAcquired bool
	if err := conn.QueryRowContext(ctx, "SELECT pg_try_advisory_lock(hashtextextended($1, 0))", lockKey).Scan(&lockAcquired); err != nil {
		// A lost/cancelled response is ambiguous: PostgreSQL may have acquired
		// the lock. Never return that session to the pool.
		discardSQLConnection(conn)
		_ = conn.Close()
		return nil, false, fmt.Errorf("acquire source correction worker lock: %w", err)
	}
	if !lockAcquired {
		if err := conn.Close(); err != nil {
			return nil, false, fmt.Errorf("close unused source correction worker lock connection: %w", err)
		}
		return nil, false, nil
	}

	var once sync.Once
	return func() {
		once.Do(func() {
			unlockCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			var unlocked bool
			unlockErr := conn.QueryRowContext(
				unlockCtx,
				"SELECT pg_advisory_unlock(hashtextextended($1, 0))",
				lockKey,
			).Scan(&unlocked)
			cancel()
			if unlockErr != nil || !unlocked {
				// Returning this connection to the pool could leak a session lock to
				// an unrelated request. Force database/sql to discard it instead.
				discardSQLConnection(conn)
				if unlockErr != nil {
					r.DB.Logger.Error(context.Background(), "release source correction advisory lock failed: %v", unlockErr)
				} else {
					r.DB.Logger.Error(context.Background(), "source correction advisory lock was not held at release")
				}
			}
			if closeErr := conn.Close(); closeErr != nil {
				r.DB.Logger.Error(context.Background(), "close source correction advisory lock connection failed: %v", closeErr)
			}
		})
	}, true, nil
}

func discardSQLConnection(conn *sql.Conn) {
	if conn == nil {
		return
	}
	_ = conn.Raw(func(any) error { return driver.ErrBadConn })
}

// PersistExtractionCorrectionLesson persists one exact correction lesson only
// while the supplied durable-job lease, correction phase, and extraction
// revision still match. sourceURI is the canonical internal extraction key;
// request.SourceURI remains evidence provenance and is never replaced here.
func (r *GormRepository) PersistExtractionCorrectionLesson(
	ctx context.Context,
	correctionID uuid.UUID,
	durableJobID uuid.UUID,
	workerID string,
	generation int64,
	appliedRevision time.Time,
	ownerIdentity string,
	sourceURI string,
	request memory.CreateRequest,
) (*models.ContextMemory, error) {
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	workerID = strings.TrimSpace(workerID)
	if ctx == nil || correctionID == uuid.Nil || durableJobID == uuid.Nil || workerID == "" || generation <= 0 ||
		appliedRevision.IsZero() || ownerIdentity == "" || len(ownerIdentity) > 255 {
		return nil, errors.New("source correction lesson lease request is invalid")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r == nil || r.DB == nil || r.DB.Dialector == nil || r.DB.Dialector.Name() != "postgres" {
		return nil, errors.New("source correction lesson repository requires PostgreSQL")
	}
	if strings.TrimSpace(request.Kind) == "" || strings.TrimSpace(request.Content) == "" {
		return nil, errors.New("source correction lesson request is incomplete")
	}

	var persisted *models.ContextMemory
	err := r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var job models.DurableJob
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&job, "id = ?", durableJobID).Error; err != nil {
			return err
		}
		if job.Status != models.DurableJobRunning || job.LockedBy != workerID || job.LeaseGeneration != generation ||
			job.Queue != "source" || job.Kind != JobKindExtractionCorrection {
			return errExtractionCorrectionLeaseLost
		}

		var correction models.SourceExtractionCorrection
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			First(&correction, "id = ? AND durable_job_id = ?", correctionID, durableJobID).Error; err != nil {
			return err
		}
		if correction.Status != models.SourceExtractionCorrectionPending || correction.Phase != correctionPhaseGraphProjected ||
			correction.OwnerIdentity != ownerIdentity || correction.AppliedRevision == nil ||
			!correction.AppliedRevision.Equal(appliedRevision) {
			return ErrExtractionCorrectionNotFound
		}
		canonicalSourceURI := "source-extraction://" + correction.ExtractionID.String()
		if sourceURI != canonicalSourceURI {
			return errors.New("source correction lesson key does not match the extraction")
		}

		var before extractionCorrectionBeforeState
		if err := json.Unmarshal([]byte(correction.BeforeStateJSON), &before); err != nil ||
			before.ID != correction.ExtractionID || before.SourceID != correction.SourceID ||
			before.UpdatedAt.After(correction.ExpectedRevision) {
			return errors.New("source correction before-state is invalid")
		}

		var source models.ConnectedSource
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			First(&source, "id = ? AND owner_identity = ?", correction.SourceID, ownerIdentity).Error; err != nil {
			return err
		}
		var extraction models.SourceExtraction
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND source_id = ? AND archived = ? AND updated_at = ?",
				correction.ExtractionID, correction.SourceID, false, appliedRevision.UTC()).
			First(&extraction).Error; err != nil {
			return err
		}
		if !extraction.UpdatedAt.Equal(appliedRevision) {
			return errExtractionCorrectionLeaseLost
		}
		expectedRequest := extractionCorrectionMemoryRequest(&source, before.extraction(), &extraction)
		if !reflect.DeepEqual(expectedRequest, request) {
			return errors.New("source correction lesson no longer matches the persisted extraction")
		}
		auditMessage := memory.SourceExtractionCorrectionAuditMessage(extraction.ID, extraction.UpdatedAt, request.Content)
		var auditCount int64
		if err := tx.Model(&models.SourceAuditLog{}).
			Where("source_id = ? AND action = ? AND message = ? AND created_at >= ?",
				source.ID, "extraction.corrected", auditMessage, extraction.UpdatedAt).
			Count(&auditCount).Error; err != nil {
			return fmt.Errorf("verify source correction lesson audit: %w", err)
		}
		if auditCount == 0 {
			if err := tx.Create(&models.SourceAuditLog{
				ID: uuid.New(), SourceID: source.ID, Action: "extraction.corrected",
				Message: auditMessage, CreatedAt: extraction.UpdatedAt,
			}).Error; err != nil {
				return fmt.Errorf("record content-bound source correction audit: %w", err)
			}
		}

		var err error
		persisted, err = memory.PersistSourceExtractionLessonTx(tx, ownerIdentity, canonicalSourceURI, request)
		if err != nil {
			return err
		}
		if persisted == nil || persisted.SourceURI != request.SourceURI || persisted.SourceExtractionID == nil ||
			*persisted.SourceExtractionID != extraction.ID {
			return errors.New("source correction lesson persistence did not preserve evidence provenance")
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("persist source correction lesson: %w", err)
	}
	return persisted, nil
}

func (r *GormRepository) CreateExtractionCorrectionIntent(
	correction *models.SourceExtractionCorrection,
	job *models.DurableJob,
) (*models.SourceExtractionCorrection, *models.DurableJob, bool, error) {
	return r.createExtractionCorrectionIntent(correction, job, uuid.Nil)
}

func (r *GormRepository) CreateExtractionCorrectionRecoveryIntent(
	correction *models.SourceExtractionCorrection,
	job *models.DurableJob,
	priorCorrectionID uuid.UUID,
) (*models.SourceExtractionCorrection, *models.DurableJob, bool, error) {
	if priorCorrectionID == uuid.Nil {
		return nil, nil, false, ErrExtractionPatchConflict
	}
	return r.createExtractionCorrectionIntent(correction, job, priorCorrectionID)
}

func (r *GormRepository) createExtractionCorrectionIntent(
	correction *models.SourceExtractionCorrection,
	job *models.DurableJob,
	priorCorrectionID uuid.UUID,
) (*models.SourceExtractionCorrection, *models.DurableJob, bool, error) {
	if r == nil || r.DB == nil || correction == nil || job == nil {
		return nil, nil, false, errors.New("source correction repository is unavailable")
	}
	created := false
	var storedCorrection models.SourceExtractionCorrection
	var storedJob models.DurableJob
	quietDB := r.DB.Session(&gorm.Session{Logger: r.DB.Logger.LogMode(logger.Silent)})
	err := quietDB.Transaction(func(tx *gorm.DB) error {
		owner := correction.OwnerIdentity
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", extractionCorrectionWorkerLockKey(owner, correction.ExtractionID)).Error; err != nil {
			return err
		}

		var existing models.SourceExtractionCorrection
		lookupErr := tx.Where("owner_identity = ? AND extraction_id = ? AND idempotency_key_hash = ?", owner, correction.ExtractionID, correction.IdempotencyKeyHash).
			First(&existing).Error
		if lookupErr == nil {
			if existing.RequestHash != correction.RequestHash {
				return ErrExtractionCorrectionIdempotency
			}
			if err := tx.First(&storedJob, "id = ?", existing.DurableJobID).Error; err != nil {
				return err
			}
			storedCorrection = existing
			return nil
		}
		if !errors.Is(lookupErr, gorm.ErrRecordNotFound) {
			return lookupErr
		}

		if err := failClosedTerminalCorrectionRows(tx, owner, correction.ExtractionID); err != nil {
			return err
		}
		var active models.SourceExtractionCorrection
		activeErr := tx.Table("source_extraction_corrections AS correction").
			Select("correction.*").
			Joins("JOIN durable_jobs ON durable_jobs.id = correction.durable_job_id").
			Where("correction.owner_identity = ? AND correction.extraction_id = ? AND correction.status = ? AND durable_jobs.status IN ?",
				owner, correction.ExtractionID, models.SourceExtractionCorrectionPending,
				[]string{models.DurableJobPending, models.DurableJobRunning}).
			First(&active).Error
		if activeErr == nil {
			if active.RequestHash != correction.RequestHash {
				return ErrExtractionCorrectionActive
			}
			if err := tx.First(&storedJob, "id = ?", active.DurableJobID).Error; err != nil {
				return err
			}
			storedCorrection = active
			return nil
		}
		if !errors.Is(activeErr, gorm.ErrRecordNotFound) {
			return activeErr
		}

		var extraction models.SourceExtraction
		err := tx.Model(&models.SourceExtraction{}).
			Joins("JOIN connected_sources ON connected_sources.id = source_extractions.source_id").
			Clauses(clause.Locking{Strength: "UPDATE", Table: clause.Table{Name: "source_extractions"}}).
			Where("source_extractions.id = ? AND connected_sources.owner_identity = ?", correction.ExtractionID, owner).
			First(&extraction).Error
		if err != nil {
			return err
		}
		if extraction.Archived {
			return ErrArchivedExtractionPatch
		}
		if !extraction.UpdatedAt.Equal(correction.ExpectedRevision) {
			return ErrExtractionPatchConflict
		}
		if priorCorrectionID != uuid.Nil {
			if !correctionPatchMatchesExtraction(correction.PatchJSON, &extraction) {
				return ErrExtractionPatchConflict
			}
			if err := resolveCompletedExtractionCorrectionRecovery(tx, correction, &extraction, &storedCorrection, &storedJob); err != nil {
				return err
			}
			if storedCorrection.ID != uuid.Nil {
				return nil
			}
			var prior models.SourceExtractionCorrection
			priorErr := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
				Where("id = ? AND owner_identity = ? AND extraction_id = ? AND status = ? AND applied_revision = ? AND patch_json = ?::jsonb",
					priorCorrectionID, owner, correction.ExtractionID, models.SourceExtractionCorrectionFailed,
					extraction.UpdatedAt, correction.PatchJSON).
				First(&prior).Error
			if errors.Is(priorErr, gorm.ErrRecordNotFound) {
				return ErrExtractionPatchConflict
			}
			if priorErr != nil {
				return priorErr
			}
			var before extractionCorrectionBeforeState
			if err := json.Unmarshal([]byte(prior.BeforeStateJSON), &before); err != nil ||
				before.ID != extraction.ID || before.SourceID != extraction.SourceID ||
				!before.UpdatedAt.Equal(prior.ExpectedRevision) {
				return errors.New("source correction recovery checkpoint is invalid")
			}
			applied := extraction.UpdatedAt.UTC()
			correction.BeforeStateJSON = prior.BeforeStateJSON
			correction.RequiresRetraction = prior.RequiresRetraction
			correction.AppliedRevision = &applied
			correction.Phase = extractionCorrectionResumePhase(&prior)
		}
		correction.SourceID = extraction.SourceID
		correction.OwnerIdentity = owner
		correction.Status = models.SourceExtractionCorrectionPending
		if priorCorrectionID == uuid.Nil {
			correction.Phase = correctionPhaseIntentPersisted
		}
		correction.ErrorCode = ""
		correction.CompletedAt = nil
		correction.Attempts = 0
		correction.UpdatedAt = time.Now().UTC()
		if correction.CreatedAt.IsZero() {
			correction.CreatedAt = correction.UpdatedAt
		}
		job.Queue = "source"
		job.Kind = JobKindExtractionCorrection
		job.Status = models.DurableJobPending
		job.MaxAttempts = extractionCorrectionMaxAttempts
		job.Attempts = 0
		job.LockedBy = ""
		job.LockedAt = nil
		job.CompletedAt = nil
		job.LastError = ""
		if job.ID == uuid.Nil {
			job.ID = uuid.New()
		}
		if job.RunAt.IsZero() {
			job.RunAt = correction.CreatedAt
		}
		correction.DurableJobID = job.ID
		if err := tx.Create(job).Error; err != nil {
			return err
		}
		if err := tx.Create(correction).Error; err != nil {
			return err
		}
		storedCorrection = *correction
		storedJob = *job
		created = true
		return nil
	})
	if err != nil {
		// Resolve an ambiguous commit result by looking up the same idempotency
		// key before telling the caller whether the intent was saved.
		resolved, durable, found, lookupErr := r.FindExtractionCorrectionByIdempotency(
			correction.OwnerIdentity, correction.ExtractionID, correction.IdempotencyKeyHash,
		)
		if lookupErr != nil {
			return nil, nil, false, &ExtractionCorrectionPersistenceUnknownError{CorrectionID: correction.ID}
		}
		if found {
			if resolved.RequestHash != correction.RequestHash {
				return nil, nil, false, ErrExtractionCorrectionIdempotency
			}
			return resolved, durable, false, nil
		}
		return nil, nil, false, err
	}
	return &storedCorrection, &storedJob, created, nil
}

func resolveCompletedExtractionCorrectionRecovery(
	tx *gorm.DB,
	correction *models.SourceExtractionCorrection,
	extraction *models.SourceExtraction,
	storedCorrection *models.SourceExtractionCorrection,
	storedJob *models.DurableJob,
) error {
	var completed models.SourceExtractionCorrection
	err := tx.Where("owner_identity = ? AND extraction_id = ? AND status = ? AND applied_revision = ? AND patch_json = ?::jsonb",
		correction.OwnerIdentity, correction.ExtractionID, models.SourceExtractionCorrectionCompleted,
		extraction.UpdatedAt, correction.PatchJSON).
		Order("updated_at DESC, created_at DESC").First(&completed).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	var job models.DurableJob
	if err := tx.First(&job, "id = ?", completed.DurableJobID).Error; err != nil {
		return err
	}
	*storedCorrection = completed
	*storedJob = job
	return nil
}

func correctionPatchMatchesExtraction(patchJSON string, extraction *models.SourceExtraction) bool {
	var patch ExtractionPatch
	return json.Unmarshal([]byte(patchJSON), &patch) == nil && patch.validate() == nil && patch.matches(extraction)
}

func (r *GormRepository) FindLatestAppliedExtractionCorrection(
	owner string,
	extractionID uuid.UUID,
	patchJSON string,
	appliedRevision time.Time,
) (*models.SourceExtractionCorrection, *models.DurableJob, bool, error) {
	if r == nil || r.DB == nil || owner == "" || extractionID == uuid.Nil || patchJSON == "" || appliedRevision.IsZero() {
		return nil, nil, false, errors.New("source correction repository is unavailable")
	}
	var correction models.SourceExtractionCorrection
	var job models.DurableJob
	found := false
	quietDB := r.DB.Session(&gorm.Session{Logger: r.DB.Logger.LogMode(logger.Silent)})
	err := quietDB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", extractionCorrectionWorkerLockKey(owner, extractionID)).Error; err != nil {
			return err
		}
		if err := failClosedTerminalCorrectionRows(tx, owner, extractionID); err != nil {
			return err
		}
		err := tx.Where("owner_identity = ? AND extraction_id = ? AND status IN ? AND applied_revision = ? AND patch_json = ?::jsonb",
			owner, extractionID, []string{models.SourceExtractionCorrectionFailed, models.SourceExtractionCorrectionCompleted},
			appliedRevision.UTC(), patchJSON).
			Clauses(clause.OrderBy{Expression: clause.Expr{
				SQL:  "CASE WHEN status = ? THEN 0 ELSE 1 END ASC, updated_at DESC, created_at DESC, id DESC",
				Vars: []any{models.SourceExtractionCorrectionCompleted},
			}}).Take(&correction).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := tx.First(&job, "id = ?", correction.DurableJobID).Error; err != nil {
			return err
		}
		found = true
		return nil
	})
	if err != nil {
		return nil, nil, false, err
	}
	if !found {
		return nil, nil, false, nil
	}
	return &correction, &job, true, nil
}

func failClosedTerminalCorrectionRows(tx *gorm.DB, owner string, extractionID uuid.UUID) error {
	var stale []models.SourceExtractionCorrection
	if err := tx.Table("source_extraction_corrections AS correction").
		Select("correction.*").
		Joins("JOIN durable_jobs ON durable_jobs.id = correction.durable_job_id").
		Where("correction.owner_identity = ? AND correction.extraction_id = ? AND correction.status = ? AND durable_jobs.status NOT IN ?",
			owner, extractionID, models.SourceExtractionCorrectionPending,
			[]string{models.DurableJobPending, models.DurableJobRunning}).
		Find(&stale).Error; err != nil {
		return err
	}
	for _, correction := range stale {
		phase := correctionPhaseFailed
		if correction.AppliedRevision != nil {
			phase = extractionCorrectionResumePhase(&correction)
		}
		if err := tx.Model(&models.SourceExtractionCorrection{}).
			Where("id = ? AND status = ?", correction.ID, models.SourceExtractionCorrectionPending).
			Updates(map[string]any{
				"status":       models.SourceExtractionCorrectionFailed,
				"phase":        phase,
				"error_code":   "queue_terminal_unreconciled",
				"completed_at": gorm.Expr("clock_timestamp()"),
				"updated_at":   gorm.Expr("clock_timestamp()"),
			}).Error; err != nil {
			return err
		}
	}
	return nil
}

func (r *GormRepository) FindActiveExtractionCorrection(owner string, extractionID uuid.UUID, requestHash string) (*models.SourceExtractionCorrection, *models.DurableJob, bool, error) {
	var correction models.SourceExtractionCorrection
	err := r.DB.Table("source_extraction_corrections AS correction").
		Select("correction.*").
		Joins("JOIN durable_jobs ON durable_jobs.id = correction.durable_job_id").
		Where("correction.owner_identity = ? AND correction.extraction_id = ? AND correction.request_hash = ? AND correction.status = ? AND durable_jobs.status IN ?",
			owner, extractionID, requestHash, models.SourceExtractionCorrectionPending,
			[]string{models.DurableJobPending, models.DurableJobRunning}).
		Order("correction.created_at DESC").First(&correction).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil, false, nil
	}
	if err != nil {
		return nil, nil, false, err
	}
	job, err := r.jobForCorrection(correction.DurableJobID)
	return &correction, job, err == nil, err
}

func (r *GormRepository) FindExtractionCorrectionByIdempotency(owner string, extractionID uuid.UUID, keyHash string) (*models.SourceExtractionCorrection, *models.DurableJob, bool, error) {
	var correction models.SourceExtractionCorrection
	err := r.DB.Where("owner_identity = ? AND extraction_id = ? AND idempotency_key_hash = ?", owner, extractionID, keyHash).First(&correction).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil, false, nil
	}
	if err != nil {
		return nil, nil, false, err
	}
	job, err := r.jobForCorrection(correction.DurableJobID)
	return &correction, job, err == nil, err
}

func (r *GormRepository) FindExtractionCorrectionForOwner(owner string, id uuid.UUID) (*models.SourceExtractionCorrection, *models.DurableJob, error) {
	var correction models.SourceExtractionCorrection
	err := r.DB.Where("id = ? AND owner_identity = ?", id, owner).First(&correction).Error
	if err != nil {
		return nil, nil, err
	}
	job, err := r.jobForCorrection(correction.DurableJobID)
	return &correction, job, err
}

func (r *GormRepository) FindExtractionCorrectionForWorker(id, durableJobID uuid.UUID) (*models.SourceExtractionCorrection, *models.DurableJob, error) {
	var correction models.SourceExtractionCorrection
	err := r.DB.Where("id = ? AND durable_job_id = ?", id, durableJobID).First(&correction).Error
	if err != nil {
		return nil, nil, err
	}
	job, err := r.jobForCorrection(durableJobID)
	return &correction, job, err
}

func (r *GormRepository) jobForCorrection(id uuid.UUID) (*models.DurableJob, error) {
	var job models.DurableJob
	if err := r.DB.First(&job, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &job, nil
}

func (r *GormRepository) RecordExtractionCorrectionAttempt(id, durableJobID uuid.UUID, workerID string, generation int64, attempt int) error {
	return r.DB.Transaction(func(tx *gorm.DB) error {
		if err := requireCorrectionLease(tx, durableJobID, workerID, generation); err != nil {
			return err
		}
		result := tx.Model(&models.SourceExtractionCorrection{}).
			Where("id = ? AND durable_job_id = ? AND status = ?", id, durableJobID, models.SourceExtractionCorrectionPending).
			Updates(map[string]any{"attempts": gorm.Expr("GREATEST(attempts, ?)", attempt), "updated_at": gorm.Expr("clock_timestamp()")})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrExtractionCorrectionNotFound
		}
		return nil
	})
}

func (r *GormRepository) AdvanceExtractionCorrection(id, durableJobID uuid.UUID, workerID string, generation int64, from, to string) error {
	return r.DB.Transaction(func(tx *gorm.DB) error {
		if err := requireCorrectionLease(tx, durableJobID, workerID, generation); err != nil {
			return err
		}
		result := tx.Model(&models.SourceExtractionCorrection{}).
			Where("id = ? AND durable_job_id = ? AND status = ? AND phase = ?", id, durableJobID, models.SourceExtractionCorrectionPending, from).
			Updates(map[string]any{"phase": to, "updated_at": gorm.Expr("clock_timestamp()")})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 1 {
			return nil
		}
		var current models.SourceExtractionCorrection
		if err := tx.First(&current, "id = ? AND durable_job_id = ?", id, durableJobID).Error; err != nil {
			return err
		}
		if current.Phase == to {
			return nil
		}
		return fmt.Errorf("source correction phase changed concurrently")
	})
}

func (r *GormRepository) ApplyExtractionCorrection(id, durableJobID uuid.UUID, workerID string, generation int64) (*models.SourceExtractionCorrection, *models.SourceExtraction, bool, error) {
	var correction models.SourceExtractionCorrection
	var extraction models.SourceExtraction
	conflicted := false
	quietDB := r.DB.Session(&gorm.Session{Logger: r.DB.Logger.LogMode(logger.Silent)})
	err := quietDB.Transaction(func(tx *gorm.DB) error {
		if err := requireCorrectionLease(tx, durableJobID, workerID, generation); err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&correction, "id = ? AND durable_job_id = ?", id, durableJobID).Error; err != nil {
			return err
		}
		if correction.Status != models.SourceExtractionCorrectionPending {
			return nil
		}
		if correction.Phase == correctionPhasePatchApplied || correction.Phase == correctionPhaseWorkflowReconciled ||
			correction.Phase == correctionPhaseIndexUpdated || correction.Phase == correctionPhaseGraphProjected || correction.Phase == correctionPhaseMemoryRecorded {
			err := findCorrectionExtraction(tx, correction, &extraction)
			return err
		}
		if correction.Phase != correctionPhaseIntentPersisted && correction.Phase != correctionPhaseWorkflowRetracted {
			return fmt.Errorf("source correction is not ready to apply")
		}
		err := findCorrectionExtractionForUpdate(tx, correction, &extraction)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return persistCorrectionConflict(tx, &correction, correction.Phase == correctionPhaseWorkflowRetracted, "revision_conflict", &conflicted)
			}
			return err
		}
		if extraction.Archived || !extraction.UpdatedAt.Equal(correction.ExpectedRevision) {
			return persistCorrectionConflict(tx, &correction, correction.Phase == correctionPhaseWorkflowRetracted, "revision_conflict", &conflicted)
		}
		var patch ExtractionPatch
		if err := json.Unmarshal([]byte(correction.PatchJSON), &patch); err != nil {
			return err
		}
		if err := patch.validate(); err != nil {
			return err
		}
		updates := map[string]any{"updated_at": gorm.Expr("GREATEST(clock_timestamp(), updated_at + INTERVAL '1 microsecond')")}
		applyExtractionPatchUpdates(updates, patch)
		result := tx.Model(&models.SourceExtraction{}).
			Where(`source_extractions.id = ? AND source_extractions.updated_at = ? AND EXISTS (
				SELECT 1 FROM connected_sources
				WHERE connected_sources.id = source_extractions.source_id
				AND connected_sources.owner_identity = ?
			)`, correction.ExtractionID, correction.ExpectedRevision, correction.OwnerIdentity).
			Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return persistCorrectionConflict(tx, &correction, correction.Phase == correctionPhaseWorkflowRetracted, "revision_conflict", &conflicted)
		}
		if err := tx.First(&extraction, "id = ?", correction.ExtractionID).Error; err != nil {
			return err
		}
		applied := extraction.UpdatedAt.UTC()
		correction.AppliedRevision = &applied
		correction.Phase = correctionPhasePatchApplied
		correction.UpdatedAt = time.Now().UTC()
		if err := tx.Model(&models.SourceExtractionCorrection{}).
			Where("id = ? AND durable_job_id = ? AND status = ?", id, durableJobID, models.SourceExtractionCorrectionPending).
			Updates(map[string]any{"applied_revision": applied, "phase": correctionPhasePatchApplied, "updated_at": gorm.Expr("clock_timestamp()")}).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, nil, false, err
	}
	return &correction, &extraction, conflicted, nil
}

func findCorrectionExtraction(tx *gorm.DB, correction models.SourceExtractionCorrection, extraction *models.SourceExtraction) error {
	return tx.Model(&models.SourceExtraction{}).
		Joins("JOIN connected_sources ON connected_sources.id = source_extractions.source_id").
		Where("source_extractions.id = ? AND connected_sources.owner_identity = ?", correction.ExtractionID, correction.OwnerIdentity).
		First(extraction).Error
}

func findCorrectionExtractionForUpdate(tx *gorm.DB, correction models.SourceExtractionCorrection, extraction *models.SourceExtraction) error {
	return tx.Model(&models.SourceExtraction{}).
		Joins("JOIN connected_sources ON connected_sources.id = source_extractions.source_id").
		Clauses(clause.Locking{Strength: "UPDATE", Table: clause.Table{Name: "source_extractions"}}).
		Where("source_extractions.id = ? AND connected_sources.owner_identity = ?", correction.ExtractionID, correction.OwnerIdentity).
		First(extraction).Error
}

func persistCorrectionConflict(tx *gorm.DB, correction *models.SourceExtractionCorrection, compensate bool, code string, conflicted *bool) error {
	*conflicted = true
	if compensate {
		correction.Phase = correctionPhaseConflictReconcile
		return tx.Model(&models.SourceExtractionCorrection{}).
			Where("id = ? AND durable_job_id = ? AND status = ?", correction.ID, correction.DurableJobID, models.SourceExtractionCorrectionPending).
			Updates(map[string]any{"phase": correctionPhaseConflictReconcile, "error_code": code, "updated_at": gorm.Expr("clock_timestamp()")}).Error
	}
	correction.Status = models.SourceExtractionCorrectionConflict
	correction.Phase = correctionPhaseConflict
	correction.ErrorCode = code
	completed := time.Now().UTC()
	correction.CompletedAt = &completed
	return tx.Model(&models.SourceExtractionCorrection{}).
		Where("id = ? AND durable_job_id = ? AND status = ?", correction.ID, correction.DurableJobID, models.SourceExtractionCorrectionPending).
		Updates(map[string]any{"status": models.SourceExtractionCorrectionConflict, "phase": correctionPhaseConflict,
			"error_code": code, "completed_at": gorm.Expr("clock_timestamp()"), "updated_at": gorm.Expr("clock_timestamp()")}).Error
}

func (r *GormRepository) FinishExtractionCorrection(id, durableJobID uuid.UUID, workerID string, generation int64, status, phase, errorCode string) error {
	return r.DB.Transaction(func(tx *gorm.DB) error {
		if err := requireCorrectionLease(tx, durableJobID, workerID, generation); err != nil {
			return err
		}
		if status != models.SourceExtractionCorrectionCompleted && status != models.SourceExtractionCorrectionConflict && status != models.SourceExtractionCorrectionFailed {
			return errors.New("invalid source correction terminal status")
		}
		result := tx.Model(&models.SourceExtractionCorrection{}).
			Where("id = ? AND durable_job_id = ? AND status = ?", id, durableJobID, models.SourceExtractionCorrectionPending).
			Updates(map[string]any{"status": status, "phase": phase, "error_code": errorCode,
				"completed_at": gorm.Expr("clock_timestamp()"), "updated_at": gorm.Expr("clock_timestamp()")})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 1 {
			return nil
		}
		var current models.SourceExtractionCorrection
		if err := tx.First(&current, "id = ? AND durable_job_id = ?", id, durableJobID).Error; err != nil {
			return err
		}
		if current.Status == status && current.Phase == phase {
			return nil
		}
		return ErrExtractionCorrectionNotFound
	})
}

func requireCorrectionLease(tx *gorm.DB, jobID uuid.UUID, workerID string, generation int64) error {
	var job models.DurableJob
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", jobID).First(&job).Error; err != nil {
		return err
	}
	if job.Status != models.DurableJobRunning || job.LockedBy != workerID || job.LeaseGeneration != generation {
		return errExtractionCorrectionLeaseLost
	}
	return nil
}

func applyExtractionPatchUpdates(updates map[string]any, patch ExtractionPatch) {
	if patch.Text != nil {
		updates["text"] = *patch.Text
	}
	if patch.Summary != nil {
		updates["summary"] = *patch.Summary
	}
	if patch.ProjectKey != nil {
		updates["project_key"] = *patch.ProjectKey
	}
	if patch.Entities != nil {
		updates["entities"] = *patch.Entities
	}
	if patch.Dates != nil {
		updates["dates"] = *patch.Dates
	}
	if patch.Tasks != nil {
		updates["tasks"] = *patch.Tasks
	}
	if patch.Decisions != nil {
		updates["decisions"] = *patch.Decisions
	}
	if patch.FollowUps != nil {
		updates["follow_ups"] = *patch.FollowUps
	}
	if patch.Sensitive != nil {
		updates["sensitive"] = *patch.Sensitive
	}
	if patch.Uncertain != nil {
		updates["uncertain"] = *patch.Uncertain
	}
}
