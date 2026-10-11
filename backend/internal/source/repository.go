package source

import (
	"automation-hub-backend/internal/infra"
	"automation-hub-backend/internal/memory"
	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/safety"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Repository interface {
	SaveConnector(connector *models.SourceConnector) (*models.SourceConnector, error)
	FindConnectors() ([]models.SourceConnector, error)
	CreateSource(source *models.ConnectedSource) (*models.ConnectedSource, error)
	UpdateSource(source *models.ConnectedSource) (*models.ConnectedSource, error)
	RevokeSource(source *models.ConnectedSource, ownerIdentity string, revokedAt time.Time) (*models.ConnectedSource, error)
	FindSources(includeDisabled bool) ([]models.ConnectedSource, error)
	FindSourcesVisibleToOwner(ownerIdentity string, includeDisabled bool) ([]models.ConnectedSource, error)
	FindSource(id uuid.UUID) (*models.ConnectedSource, error)
	FindMutableSourceForOwner(id uuid.UUID, ownerIdentity string) (*models.ConnectedSource, error)
	SetGoogleOAuthReconnectRequired(sourceID uuid.UUID, required bool) (bool, error)
	CreateSyncJob(job *models.SourceSyncJob) (*models.SourceSyncJob, error)
	UpdateSyncJob(job *models.SourceSyncJob) (*models.SourceSyncJob, error)
	FindSyncJobs(sourceID *uuid.UUID) ([]models.SourceSyncJob, error)
	FindSyncJobsForSources(sourceIDs []uuid.UUID, limit int) ([]models.SourceSyncJob, error)
	FindRawItem(sourceID uuid.UUID, externalID string) (*models.SourceRawItem, error)
	SaveRawItem(item *models.SourceRawItem) (*models.SourceRawItem, error)
	FindRawItems(sourceID uuid.UUID) ([]models.SourceRawItem, error)
	FindExtractionByRawItem(rawItemID uuid.UUID) (*models.SourceExtraction, error)
	SaveExtraction(extraction *models.SourceExtraction) (*models.SourceExtraction, error)
	FindExtractions(projectKey string, includeArchived bool) ([]models.SourceExtraction, error)
	FindExtractionsForSources(sourceIDs []uuid.UUID, projectKey string, includeArchived bool) ([]models.SourceExtraction, error)
	FindExtractionPageForSources(sourceIDs []uuid.UUID, projectKey string, includeArchived bool, limit int) ([]models.SourceExtraction, int64, error)
	FindExtraction(id uuid.UUID) (*models.SourceExtraction, error)
	FindMutableExtractionForOwner(id uuid.UUID, ownerIdentity string) (*models.SourceExtraction, error)
	DeleteExtractionForOwner(
		extraction *models.SourceExtraction,
		source *models.ConnectedSource,
		ownerIdentity string,
	) error
	SaveIndexEntry(entry *models.SourceIndexEntry) (*models.SourceIndexEntry, error)
	DeletePendingVectorIndex(extractionID uuid.UUID) error
	SaveAuditLog(log *models.SourceAuditLog) (*models.SourceAuditLog, error)
	FindAuditLogs(sourceID *uuid.UUID) ([]models.SourceAuditLog, error)
	FindAuditLogsForSources(sourceIDs []uuid.UUID, limit int) ([]models.SourceAuditLog, error)
	SaveOAuthToken(token *models.SourceOAuthToken) error
	FindOAuthToken(sourceID uuid.UUID) (*models.SourceOAuthToken, error)
	FindOAuthTokensForSources(sourceIDs []uuid.UUID) ([]models.SourceOAuthToken, error)
}

type guardedExtractionDeleteRepository interface {
	DeleteExtractionForOwnerGuarded(
		expected *models.SourceExtraction,
		expectedSource *models.ConnectedSource,
		ownerIdentity string,
		beforeCommit func() error,
	) error
}

type transactionalGuardedExtractionDeleteRepository interface {
	DeleteExtractionForOwnerGuardedInTransaction(
		expected *models.SourceExtraction,
		expectedSource *models.ConnectedSource,
		ownerIdentity string,
		beforeCommit func(*gorm.DB) (func(bool), error),
	) error
	SaveAuditLogInTransaction(tx *gorm.DB, log *models.SourceAuditLog) error
}

var (
	ErrExtractionCorrectionWorkerActive   = errors.New("source extraction has an active correction worker")
	ErrGuardedExtractionDeleteUnavailable = errors.New("repository does not support guarded extraction deletion")
)

type GormRepository struct {
	DB *gorm.DB
}

// AcquireSourceSyncLease prevents the same source from being synced by two
// backend processes at once. PostgreSQL releases this session-level advisory
// lock if the process or database connection dies, so an abandoned worker
// cannot permanently block a source after a restart.
func (r *GormRepository) AcquireSourceSyncLease(ctx context.Context, sourceID uuid.UUID) (func(), bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	db, err := r.DB.DB()
	if err != nil {
		return nil, false, err
	}
	if maxOpen := db.Stats().MaxOpenConnections; maxOpen > 0 && maxOpen < 2 {
		return nil, false, errors.New("source sync lease requires a database pool with at least two connections")
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, false, err
	}
	key := sourceSyncLeaseKey(sourceID)
	var acquired bool
	if err := conn.QueryRowContext(ctx, "SELECT pg_try_advisory_lock($1)", key).Scan(&acquired); err != nil {
		// The response may be lost after PostgreSQL acquired the session lock.
		// Never return an uncertain lock-bearing connection to the pool.
		discardSQLConnection(conn)
		_ = conn.Close()
		return nil, false, err
	}
	if !acquired {
		if err := conn.Close(); err != nil {
			return nil, false, err
		}
		return func() {}, false, nil
	}

	var once sync.Once
	release := func() {
		once.Do(func() {
			// The connection may already be gone after a database restart. Closing
			// it is still enough to ensure no session lock survives locally.
			releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			var released bool
			unlockErr := conn.QueryRowContext(releaseCtx, "SELECT pg_advisory_unlock($1)", key).Scan(&released)
			if unlockErr != nil || !released {
				// An uncertain unlock must not leak a session lock to an unrelated
				// request when this pinned connection returns to database/sql.
				discardSQLConnection(conn)
				if unlockErr != nil {
					r.DB.Logger.Error(context.Background(), "release source sync advisory lock failed: %v", unlockErr)
				} else {
					r.DB.Logger.Error(context.Background(), "source sync advisory lock was not held at release")
				}
			}
			if closeErr := conn.Close(); closeErr != nil {
				r.DB.Logger.Error(context.Background(), "close source sync advisory lock connection failed: %v", closeErr)
			}
		})
	}
	return release, true, nil
}

func sourceSyncLeaseKey(sourceID uuid.UUID) int64 {
	digest := sha256.Sum256(append([]byte("hai:source-sync:"), sourceID[:]...))
	return int64(binary.BigEndian.Uint64(digest[:8]))
}

func NewGormRepository(db *gorm.DB) Repository {
	return &GormRepository{DB: db}
}

func DefaultRepository() Repository {
	db, err := infra.GetDefaultDB()
	if err != nil {
		panic(err)
	}
	return NewGormRepository(db)
}

func (r *GormRepository) SaveConnector(connector *models.SourceConnector) (*models.SourceConnector, error) {
	var existing models.SourceConnector
	create := false
	if connector.ID == uuid.Nil {
		err := r.DB.Where("connector_key = ?", connector.ConnectorKey).First(&existing).Error
		if err == nil {
			connector.ID = existing.ID
			if connector.CreatedAt.IsZero() {
				connector.CreatedAt = existing.CreatedAt
			}
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		} else {
			connector.ID = uuid.New()
			create = true
		}
	} else if connector.CreatedAt.IsZero() {
		if err := r.DB.First(&existing, "id = ?", connector.ID).Error; err == nil {
			connector.CreatedAt = existing.CreatedAt
		} else if errors.Is(err, gorm.ErrRecordNotFound) {
			create = true
		} else {
			return nil, err
		}
	}
	if create {
		if err := r.DB.Create(connector).Error; err != nil {
			return nil, err
		}
		return connector, nil
	}
	if err := r.DB.Select("*").Save(connector).Error; err != nil {
		return nil, err
	}
	return connector, nil
}

func (r *GormRepository) FindConnectors() ([]models.SourceConnector, error) {
	var connectors []models.SourceConnector
	err := r.DB.Order("category asc, name asc").Find(&connectors).Error
	return connectors, err
}

func (r *GormRepository) CreateSource(source *models.ConnectedSource) (*models.ConnectedSource, error) {
	if err := r.DB.Create(source).Error; err != nil {
		return nil, err
	}
	return source, nil
}

func (r *GormRepository) UpdateSource(source *models.ConnectedSource) (*models.ConnectedSource, error) {
	if source == nil || source.ID == uuid.Nil || source.UpdatedAt.IsZero() {
		return nil, errors.New("source update requires a persisted source version")
	}
	if strings.EqualFold(strings.TrimSpace(source.Status), "revoked") || source.RevokedAt != nil {
		return nil, ErrSourceRevoked
	}
	result := r.DB.Model(&models.ConnectedSource{}).
		Where("id = ? AND owner_identity = ? AND connector_key = ? AND updated_at = ? AND revoked_at IS NULL AND lower(status) <> ?",
			source.ID, source.OwnerIdentity, source.ConnectorKey, source.UpdatedAt, "revoked").
		Updates(map[string]any{
			"name":                source.Name,
			"enabled":             source.Enabled,
			"local_only":          source.LocalOnly,
			"sync_frequency":      source.SyncFrequency,
			"sync_target":         source.SyncTarget,
			"default_project_key": source.DefaultProjectKey,
			"ingestion_modes":     source.IngestionModes,
			"permissions":         source.Permissions,
			"exclude_patterns":    source.ExcludePatterns,
			"cursor":              source.Cursor,
			"status":              source.Status,
			"last_synced_at":      source.LastSyncedAt,
			"updated_at":          gorm.Expr("GREATEST(clock_timestamp(), updated_at + INTERVAL '1 microsecond')"),
		})
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected != 1 {
		return nil, gorm.ErrRecordNotFound
	}
	var updated models.ConnectedSource
	if err := r.DB.First(&updated, "id = ?", source.ID).Error; err != nil {
		return nil, err
	}
	*source = updated
	return source, nil
}

// SetGoogleOAuthReconnectRequired changes only the source status. The filters
// ensure a background OAuth failure cannot revive a paused or revoked source.
func (r *GormRepository) SetGoogleOAuthReconnectRequired(sourceID uuid.UUID, required bool) (bool, error) {
	query := r.DB.Model(&models.ConnectedSource{}).
		Where("id = ? AND connector_key IN ?", sourceID, []string{gmailConnectorKey, driveConnectorKey, contactsConnectorKey, calendarConnectorKey}).
		Where("enabled = ? AND revoked_at IS NULL", true)
	status := "active"
	if required {
		status = "reconnect_required"
		query = query.Where("lower(status) NOT IN ?", []string{"paused", "revoked", "reconnect_required"})
	} else {
		query = query.Where("lower(status) = ?", "reconnect_required")
	}
	result := query.Updates(map[string]any{"status": status, "updated_at": time.Now().UTC()})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

// SetGoogleOAuthReconnectRequiredForToken records a refresh failure only while
// the exact access-token snapshot is still current. Source and token rows use
// the same lock order as token persistence and revocation.
func (r *GormRepository) SetGoogleOAuthReconnectRequiredForToken(
	ctx context.Context,
	expected *models.SourceOAuthToken,
) (bool, error) {
	if ctx == nil {
		return false, errors.New("Google OAuth refresh failure context is required")
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if r == nil || r.DB == nil || expected == nil || expected.SourceID == uuid.Nil ||
		expected.Provider != googleProvider || len(expected.AccessToken) == 0 && len(expected.RefreshToken) == 0 {
		return false, errors.New("Google OAuth failed-token identity is incomplete")
	}

	changed := false
	err := r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var source models.ConnectedSource
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND connector_key IN ?", expected.SourceID,
				[]string{gmailConnectorKey, driveConnectorKey, contactsConnectorKey, calendarConnectorKey}).
			First(&source).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if !source.Enabled || source.RevokedAt != nil ||
			strings.EqualFold(strings.TrimSpace(source.Status), "paused") ||
			strings.EqualFold(strings.TrimSpace(source.Status), "revoked") ||
			strings.EqualFold(strings.TrimSpace(source.Status), "reconnect_required") {
			return nil
		}

		tokenQuery := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("source_id = ? AND provider = ?", expected.SourceID, googleProvider)
		if expected.AccessToken == nil {
			tokenQuery = tokenQuery.Where("access_token IS NULL")
		} else {
			tokenQuery = tokenQuery.Where("access_token = ?", expected.AccessToken)
		}
		if expected.RefreshToken == nil {
			tokenQuery = tokenQuery.Where("refresh_token IS NULL")
		} else {
			tokenQuery = tokenQuery.Where("refresh_token = ?", expected.RefreshToken)
		}
		if expected.ID != uuid.Nil {
			tokenQuery = tokenQuery.Where("id = ?", expected.ID)
		}
		if !expected.UpdatedAt.IsZero() {
			tokenQuery = tokenQuery.Where("updated_at = ?", expected.UpdatedAt)
		}
		var current models.SourceOAuthToken
		err = tokenQuery.First(&current).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}

		result := tx.Model(&models.ConnectedSource{}).
			Where("id = ? AND enabled = ? AND revoked_at IS NULL AND lower(status) NOT IN ?",
				expected.SourceID, true, []string{"paused", "revoked", "reconnect_required"}).
			Updates(map[string]any{"status": "reconnect_required", "updated_at": time.Now().UTC()})
		if result.Error != nil {
			return result.Error
		}
		changed = result.RowsAffected == 1
		return ctx.Err()
	})
	if err != nil {
		return false, err
	}
	return changed, nil
}

func (r *GormRepository) RevokeSource(
	expected *models.ConnectedSource,
	ownerIdentity string,
	revokedAt time.Time,
) (*models.ConnectedSource, error) {
	if expected == nil {
		return nil, gorm.ErrRecordNotFound
	}
	var updated models.ConnectedSource
	err := r.DB.Transaction(func(tx *gorm.DB) error {
		var source models.ConnectedSource
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(
			"id = ? AND owner_identity = ? AND connector_key = ? AND default_project_key = ? AND updated_at = ?",
			expected.ID,
			ownerIdentity,
			expected.ConnectorKey,
			expected.DefaultProjectKey,
			expected.UpdatedAt,
		).First(&source).Error; err != nil {
			return err
		}
		if err := tx.Where("source_id = ?", expected.ID).
			Delete(&models.SourceOAuthToken{}).Error; err != nil {
			return err
		}
		result := tx.Model(&models.ConnectedSource{}).
			Where(
				"id = ? AND owner_identity = ? AND connector_key = ? AND default_project_key = ? AND updated_at = ?",
				expected.ID,
				ownerIdentity,
				expected.ConnectorKey,
				expected.DefaultProjectKey,
				expected.UpdatedAt,
			).
			Updates(map[string]any{
				"enabled":    false,
				"status":     "revoked",
				"revoked_at": revokedAt.UTC(),
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return gorm.ErrRecordNotFound
		}
		return tx.First(&updated, "id = ?", expected.ID).Error
	})
	if err != nil {
		return nil, err
	}
	return &updated, nil
}

func (r *GormRepository) FindSources(includeDisabled bool) ([]models.ConnectedSource, error) {
	var sources []models.ConnectedSource
	query := r.sourceQuery(includeDisabled)
	err := query.Find(&sources).Error
	return sources, err
}

// FindSourcesVisibleToOwner preserves read-only visibility of ownerless legacy
// sources while keeping other owners' records inside the database boundary.
func (r *GormRepository) FindSourcesVisibleToOwner(ownerIdentity string, includeDisabled bool) ([]models.ConnectedSource, error) {
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	if ownerIdentity == "" {
		return r.FindSources(includeDisabled)
	}
	var sources []models.ConnectedSource
	err := r.sourceQuery(includeDisabled).
		Where("owner_identity = ? OR owner_identity = '' OR owner_identity IS NULL", ownerIdentity).
		Find(&sources).Error
	return sources, err
}

func (r *GormRepository) sourceQuery(includeDisabled bool) *gorm.DB {
	query := r.DB.Order("updated_at desc")
	if !includeDisabled {
		// Paused and revoked sources remain available to their owner through the
		// explicit history view, but are not candidates for background work.
		// Filtering them at the database keeps large, deliberately paused source
		// inventories out of every scheduler sweep.
		query = query.Where("enabled = ? AND status NOT IN ?", true, []string{"paused", "revoked"})
	}
	return query
}

func (r *GormRepository) FindSource(id uuid.UUID) (*models.ConnectedSource, error) {
	var source models.ConnectedSource
	if err := r.DB.First(&source, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &source, nil
}

// FindMutableSourceForOwner is stricter than source visibility: ownerless
// legacy sources can remain readable during migration, but only their verified
// owner can alter configuration or trigger ingestion.
func (r *GormRepository) FindMutableSourceForOwner(id uuid.UUID, ownerIdentity string) (*models.ConnectedSource, error) {
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	if ownerIdentity == "" {
		return nil, gorm.ErrRecordNotFound
	}
	var source models.ConnectedSource
	if err := r.DB.Where("id = ? AND owner_identity = ?", id, ownerIdentity).First(&source).Error; err != nil {
		return nil, err
	}
	return &source, nil
}

func (r *GormRepository) CreateSyncJob(job *models.SourceSyncJob) (*models.SourceSyncJob, error) {
	if err := r.DB.Create(job).Error; err != nil {
		return nil, err
	}
	return job, nil
}

// CreateManualSyncJob persists the owner-bound history record and its durable
// queue entry together. A committed acceptance therefore always has work that
// survives the HTTP request and process lifetime.
func (r *GormRepository) CreateManualSyncJob(
	job *models.SourceSyncJob,
	durable *models.DurableJob,
) (*models.SourceSyncJob, *models.DurableJob, bool, error) {
	if job == nil || durable == nil || strings.TrimSpace(job.OwnerIdentity) == "" ||
		job.SourceID == uuid.Nil || job.IdempotencyKeyHash == "" || job.RequestHash == "" {
		return nil, nil, false, errors.New("manual source sync identity is incomplete")
	}
	if job.ID == uuid.Nil {
		job.ID = uuid.New()
	}
	if durable.ID == uuid.Nil {
		durable.ID = uuid.New()
	}
	job.DurableJobID = &durable.ID

	var existingJob models.SourceSyncJob
	var existingDurable models.DurableJob
	created := false
	err := r.DB.Transaction(func(tx *gorm.DB) error {
		var source models.ConnectedSource
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND owner_identity = ?", job.SourceID, job.OwnerIdentity).
			First(&source).Error; err != nil {
			return err
		}

		if err := tx.Where(
			"owner_identity = ? AND source_id = ? AND idempotency_key_hash = ? AND mode = ?",
			job.OwnerIdentity, job.SourceID, job.IdempotencyKeyHash, ModeManualAsyncSync,
		).First(&existingJob).Error; err == nil {
			if existingJob.RequestHash != job.RequestHash {
				return ErrManualSyncIdempotencyConflict
			}
			if existingJob.DurableJobID == nil {
				return errors.New("manual source sync is missing its durable queue entry")
			}
			if err := tx.Where("id = ? AND kind = ?", *existingJob.DurableJobID, JobKindManualSync).First(&existingDurable).Error; err != nil {
				return err
			}
			return nil
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if !source.Enabled || source.Status == "paused" || source.Status == "revoked" {
			return ErrManualSyncSourceDisabled
		}

		var active models.SourceSyncJob
		if err := tx.Model(&models.SourceSyncJob{}).
			Joins("LEFT JOIN durable_jobs AS sync_queue ON sync_queue.id = source_sync_jobs.durable_job_id AND sync_queue.kind = ?", JobKindManualSync).
			Where("source_sync_jobs.source_id = ? AND source_sync_jobs.mode = ? AND (source_sync_jobs.status IN ? OR sync_queue.status IN ?)",
				job.SourceID, ModeManualAsyncSync,
				[]string{"queued", "running"},
				[]string{models.DurableJobPending, models.DurableJobRunning},
			).
			First(&active).Error; err == nil {
			return ErrManualSyncAlreadyActive
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}

		if err := tx.Create(job).Error; err != nil {
			return err
		}
		if err := tx.Create(durable).Error; err != nil {
			return err
		}
		existingJob = *job
		existingDurable = *durable
		created = true
		return nil
	})
	if err == nil {
		return &existingJob, &existingDurable, created, nil
	}

	// A concurrent request can win the unique idempotency index between the
	// transaction's lookup and insert. Resolve only an identical retry; never
	// turn a conflicting payload or a different active request into success.
	if !errors.Is(err, ErrManualSyncAlreadyActive) && !errors.Is(err, ErrManualSyncIdempotencyConflict) {
		if retryJob, retryDurable, found, retryErr := r.findManualSyncByKey(
			job.OwnerIdentity, job.SourceID, job.IdempotencyKeyHash,
		); retryErr == nil && found {
			if retryJob.RequestHash != job.RequestHash {
				return nil, nil, false, ErrManualSyncIdempotencyConflict
			}
			return retryJob, retryDurable, false, nil
		}
	}
	return nil, nil, false, err
}

func (r *GormRepository) findManualSyncByKey(owner string, sourceID uuid.UUID, keyHash string) (*models.SourceSyncJob, *models.DurableJob, bool, error) {
	var job models.SourceSyncJob
	owner = strings.TrimSpace(owner)
	if owner == "" || sourceID == uuid.Nil || strings.TrimSpace(keyHash) == "" {
		return nil, nil, false, nil
	}
	err := r.DB.Table("source_sync_jobs AS sync_jobs").
		Select("sync_jobs.*").
		Joins("JOIN connected_sources AS sources ON sources.id = sync_jobs.source_id").
		Where("sync_jobs.owner_identity = ? AND sources.owner_identity = ? AND sync_jobs.source_id = ? AND sync_jobs.idempotency_key_hash = ? AND sync_jobs.mode = ?",
			owner, owner, sourceID, keyHash, ModeManualAsyncSync).
		First(&job).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil, false, nil
	}
	if err != nil {
		return nil, nil, false, err
	}
	if job.DurableJobID == nil {
		return nil, nil, false, errors.New("manual source sync is missing its durable queue entry")
	}
	var durable models.DurableJob
	if err := r.DB.Where("id = ? AND kind = ?", *job.DurableJobID, JobKindManualSync).First(&durable).Error; err != nil {
		return nil, nil, false, err
	}
	return &job, &durable, true, nil
}

func (r *GormRepository) FindManualSyncJobByIdempotencyKey(owner string, sourceID uuid.UUID, keyHash string) (*models.SourceSyncJob, *models.DurableJob, bool, error) {
	return r.findManualSyncByKey(owner, sourceID, keyHash)
}

func (r *GormRepository) FindManualSyncJobForOwner(owner string, id uuid.UUID) (*models.SourceSyncJob, *models.DurableJob, error) {
	owner = strings.TrimSpace(owner)
	if owner == "" || id == uuid.Nil {
		return nil, nil, gorm.ErrRecordNotFound
	}
	var job models.SourceSyncJob
	err := r.DB.Table("source_sync_jobs AS sync_jobs").
		Select("sync_jobs.*").
		Joins("JOIN connected_sources AS sources ON sources.id = sync_jobs.source_id").
		Where("sync_jobs.id = ? AND sync_jobs.mode = ? AND sync_jobs.owner_identity = ? AND sources.owner_identity = ?", id, ModeManualAsyncSync, owner, owner).
		First(&job).Error
	if err != nil {
		return nil, nil, err
	}
	if job.DurableJobID == nil {
		return nil, nil, gorm.ErrRecordNotFound
	}
	var durable models.DurableJob
	if err := r.DB.Where("id = ? AND kind = ?", *job.DurableJobID, JobKindManualSync).First(&durable).Error; err != nil {
		return nil, nil, err
	}
	return &job, &durable, nil
}

func (r *GormRepository) FindManualSyncJob(id uuid.UUID) (*models.SourceSyncJob, error) {
	var job models.SourceSyncJob
	if err := r.DB.Where("id = ? AND mode = ?", id, ModeManualAsyncSync).First(&job).Error; err != nil {
		return nil, err
	}
	return &job, nil
}

func (r *GormRepository) StartManualSyncJob(id, sourceID uuid.UUID, now time.Time) (*models.SourceSyncJob, error) {
	var job models.SourceSyncJob
	err := r.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND source_id = ? AND mode = ?", id, sourceID, ModeManualAsyncSync).
			First(&job).Error; err != nil {
			return err
		}
		if job.Status == "completed" {
			return nil
		}
		if job.Status == "failed" || job.Status == "cancelled" {
			return errors.New("manual source sync is already terminal")
		}
		var source models.ConnectedSource
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND owner_identity = ? AND enabled = ? AND status NOT IN ?", sourceID, job.OwnerIdentity, true, []string{"paused", "revoked"}).
			First(&source).Error; err != nil {
			return err
		}
		job.Status = "running"
		if job.StartedAt.IsZero() {
			job.StartedAt = now.UTC()
		}
		job.CompletedAt = nil
		if strings.TrimSpace(job.ProgressPhase) == "" {
			job.CursorBefore = source.Cursor
			job.CursorAfter = source.Cursor
			job.ItemsSeen = 0
			job.ItemsAdded = 0
			job.ItemsUpdated = 0
			job.ItemsFailed = 0
		}
		job.Message = "Sync is running; HAI will report the result here."
		return tx.Select("*").Save(&job).Error
	})
	if err != nil {
		return nil, err
	}
	return &job, nil
}

// CompleteSourceSync commits source cursor state and its history outcome in one
// transaction. A failed job write can therefore never leave a successful cursor
// advance behind.
func (r *GormRepository) CompleteSourceSync(source *models.ConnectedSource, job *models.SourceSyncJob) (*models.ConnectedSource, *models.SourceSyncJob, error) {
	if source == nil || job == nil || source.ID == uuid.Nil || job.ID == uuid.Nil || source.ID != job.SourceID {
		return nil, nil, errors.New("source sync completion identity is invalid")
	}
	job.Message = safety.RedactSecrets(job.Message)
	now := time.Now().UTC()
	err := r.DB.Transaction(func(tx *gorm.DB) error {
		sourceUpdates := map[string]any{
			"cursor":         source.Cursor,
			"last_synced_at": source.LastSyncedAt,
			"updated_at":     now,
		}
		if job.Mode != ModeManualAsyncSync {
			sourceUpdates["sync_target"] = source.SyncTarget
			sourceUpdates["default_project_key"] = source.DefaultProjectKey
		}
		update := tx.Model(&models.ConnectedSource{}).
			Where("id = ?", source.ID).
			Updates(sourceUpdates)
		if update.Error != nil {
			return update.Error
		}
		if update.RowsAffected != 1 {
			return gorm.ErrRecordNotFound
		}
		job.UpdatedAt = now
		result := tx.Model(&models.SourceSyncJob{}).
			Where("id = ? AND source_id = ?", job.ID, source.ID).
			Select("*").Updates(job)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return gorm.ErrRecordNotFound
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	source.UpdatedAt = now
	job.UpdatedAt = now
	return source, job, nil
}

func (r *GormRepository) UpdateSyncJob(job *models.SourceSyncJob) (*models.SourceSyncJob, error) {
	// Adapter failures can include arbitrary upstream diagnostics. Persist only
	// a redacted message; API responses and audit records use the same boundary.
	job.Message = safety.RedactSecrets(job.Message)
	if err := r.DB.Save(job).Error; err != nil {
		return nil, err
	}
	return job, nil
}

func (r *GormRepository) FindSyncJobs(sourceID *uuid.UUID) ([]models.SourceSyncJob, error) {
	var jobs []models.SourceSyncJob
	query := r.DB.Order("created_at desc")
	if sourceID != nil {
		query = query.Where("source_id = ?", *sourceID)
	}
	if err := query.Find(&jobs).Error; err != nil {
		return nil, err
	}
	if err := r.refreshManualSyncStatuses(jobs); err != nil {
		return nil, err
	}
	return jobs, nil
}

func (r *GormRepository) FindSyncJobsForSources(sourceIDs []uuid.UUID, limit int) ([]models.SourceSyncJob, error) {
	if len(sourceIDs) == 0 {
		return []models.SourceSyncJob{}, nil
	}
	var jobs []models.SourceSyncJob
	if err := r.DB.Where("source_id IN ?", sourceIDs).Order("created_at desc").Limit(boundedHistoryLimit(limit)).Find(&jobs).Error; err != nil {
		return nil, err
	}
	if err := r.refreshManualSyncStatuses(jobs); err != nil {
		return nil, err
	}
	return jobs, nil
}

func (r *GormRepository) refreshManualSyncStatuses(jobs []models.SourceSyncJob) error {
	ids := make([]uuid.UUID, 0, len(jobs))
	for _, job := range jobs {
		if job.Mode == ModeManualAsyncSync && job.DurableJobID != nil {
			ids = append(ids, *job.DurableJobID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	var durableJobs []models.DurableJob
	if err := r.DB.Where("id IN ? AND kind = ?", ids, JobKindManualSync).Find(&durableJobs).Error; err != nil {
		return err
	}
	byID := make(map[uuid.UUID]models.DurableJob, len(durableJobs))
	for _, durable := range durableJobs {
		byID[durable.ID] = durable
	}
	for index := range jobs {
		job := &jobs[index]
		if job.Mode != ModeManualAsyncSync || job.DurableJobID == nil {
			continue
		}
		durable, ok := byID[*job.DurableJobID]
		if !ok {
			if job.Status != "completed" && job.Status != "cancelled" {
				job.Status = "failed"
				job.Message = "The durable sync record is unavailable; review status before retrying."
			}
			continue
		}
		original := *job
		previousStatus := original.Status
		job.Status = manualSyncProjectedStatus(original.Status, durable.Status)
		job.Message = manualSyncProjectedMessage(&original, &durable, job.Status)
		if job.Status == "queued" || job.Status == "running" {
			job.CompletedAt = nil
		}
		if job.Status == "failed" && previousStatus != "failed" {
			switch durable.Status {
			case models.DurableJobDead:
				job.Message = "The background sync exhausted its retries; review status before starting another sync."
			case models.DurableJobSucceeded:
				job.Message = "The worker stopped without confirming a complete sync; the source checkpoint was not assumed to advance."
			}
		}
	}
	return nil
}

func (r *GormRepository) FindRawItem(sourceID uuid.UUID, externalID string) (*models.SourceRawItem, error) {
	var item models.SourceRawItem
	if err := r.DB.Where("source_id = ? AND external_id = ?", sourceID, externalID).First(&item).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *GormRepository) SaveRawItem(item *models.SourceRawItem) (*models.SourceRawItem, error) {
	if err := r.DB.Save(item).Error; err != nil {
		return nil, err
	}
	return item, nil
}

func (r *GormRepository) FindRawItems(sourceID uuid.UUID) ([]models.SourceRawItem, error) {
	var items []models.SourceRawItem
	err := r.DB.Where("source_id = ?", sourceID).Order("updated_at desc").Find(&items).Error
	return items, err
}

func (r *GormRepository) FindExtractionByRawItem(rawItemID uuid.UUID) (*models.SourceExtraction, error) {
	var extraction models.SourceExtraction
	if err := r.DB.Where("raw_item_id = ?", rawItemID).First(&extraction).Error; err != nil {
		return nil, err
	}
	return &extraction, nil
}

func (r *GormRepository) SaveExtraction(extraction *models.SourceExtraction) (*models.SourceExtraction, error) {
	if extraction == nil {
		return nil, errors.New("source extraction is required")
	}
	updates := clause.Assignments(map[string]any{
		"source_id":       gorm.Expr("EXCLUDED.source_id"),
		"raw_item_id":     gorm.Expr("EXCLUDED.raw_item_id"),
		"project_key":     gorm.Expr("EXCLUDED.project_key"),
		"content_type":    gorm.Expr("EXCLUDED.content_type"),
		"text":            gorm.Expr("EXCLUDED.text"),
		"summary":         gorm.Expr("EXCLUDED.summary"),
		"entities":        gorm.Expr("EXCLUDED.entities"),
		"dates":           gorm.Expr("EXCLUDED.dates"),
		"tasks":           gorm.Expr("EXCLUDED.tasks"),
		"decisions":       gorm.Expr("EXCLUDED.decisions"),
		"follow_ups":      gorm.Expr("EXCLUDED.follow_ups"),
		"source_uri":      gorm.Expr("EXCLUDED.source_uri"),
		"source_label":    gorm.Expr("EXCLUDED.source_label"),
		"content_hash":    gorm.Expr("EXCLUDED.content_hash"),
		"sensitive":       gorm.Expr("EXCLUDED.sensitive"),
		"uncertain":       gorm.Expr("EXCLUDED.uncertain"),
		"archived":        gorm.Expr("EXCLUDED.archived"),
		"last_indexed_at": gorm.Expr("EXCLUDED.last_indexed_at"),
		"updated_at":      gorm.Expr("GREATEST(clock_timestamp(), source_extractions.updated_at + INTERVAL '1 microsecond')"),
	})
	if err := r.DB.Clauses(
		clause.OnConflict{Columns: []clause.Column{{Name: "id"}}, DoUpdates: updates},
		clause.Returning{},
	).Create(extraction).Error; err != nil {
		return nil, err
	}
	// RETURNING reads PostgreSQL's canonical timestamptz precision. The conflict
	// update computes a monotone revision while holding PostgreSQL's row lock.
	return extraction, nil
}

func (r *GormRepository) FindExtractions(projectKey string, includeArchived bool) ([]models.SourceExtraction, error) {
	return r.findExtractions(nil, projectKey, includeArchived)
}

func (r *GormRepository) FindExtractionsForSources(sourceIDs []uuid.UUID, projectKey string, includeArchived bool) ([]models.SourceExtraction, error) {
	if len(sourceIDs) == 0 {
		return []models.SourceExtraction{}, nil
	}
	return r.findExtractions(sourceIDs, projectKey, includeArchived)
}

func (r *GormRepository) FindExtractionPageForSources(sourceIDs []uuid.UUID, projectKey string, includeArchived bool, limit int) ([]models.SourceExtraction, int64, error) {
	if len(sourceIDs) == 0 {
		return []models.SourceExtraction{}, 0, nil
	}
	query := r.extractionsQuery(sourceIDs, projectKey, includeArchived)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var extractions []models.SourceExtraction
	if err := query.Order("updated_at desc").Limit(limit).Find(&extractions).Error; err != nil {
		return nil, 0, err
	}
	return extractions, total, nil
}

func (r *GormRepository) findExtractions(sourceIDs []uuid.UUID, projectKey string, includeArchived bool) ([]models.SourceExtraction, error) {
	var extractions []models.SourceExtraction
	query := r.extractionsQuery(sourceIDs, projectKey, includeArchived).Order("updated_at desc")
	err := query.Find(&extractions).Error
	return extractions, err
}

func (r *GormRepository) extractionsQuery(sourceIDs []uuid.UUID, projectKey string, includeArchived bool) *gorm.DB {
	query := r.DB.Model(&models.SourceExtraction{})
	if sourceIDs != nil {
		query = query.Where("source_id IN ?", sourceIDs)
	}
	if projectKey != "" {
		query = query.Where("project_key = ?", projectKey)
	}
	if !includeArchived {
		query = query.Where("archived = ?", false)
	}
	return query
}

func (r *GormRepository) FindExtraction(id uuid.UUID) (*models.SourceExtraction, error) {
	var extraction models.SourceExtraction
	if err := r.DB.First(&extraction, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &extraction, nil
}

// FindMutableExtractionForOwner scopes an extraction mutation to its verified
// source owner without materializing the user's full source or extraction history.
func (r *GormRepository) FindMutableExtractionForOwner(id uuid.UUID, ownerIdentity string) (*models.SourceExtraction, error) {
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	if ownerIdentity == "" {
		return nil, gorm.ErrRecordNotFound
	}
	var extraction models.SourceExtraction
	err := r.DB.Model(&models.SourceExtraction{}).
		Joins("JOIN connected_sources ON connected_sources.id = source_extractions.source_id").
		Where("source_extractions.id = ? AND connected_sources.owner_identity = ?", id, ownerIdentity).
		First(&extraction).Error
	if err != nil {
		return nil, err
	}
	return &extraction, nil
}

// PatchExtractionForOwner updates only fields explicitly supplied by an
// authenticated owner. The updated_at predicate prevents a stale patch from
// overwriting concurrent review decisions or source corrections.
func (r *GormRepository) PatchExtractionForOwner(
	id uuid.UUID,
	ownerIdentity string,
	expectedUpdatedAt time.Time,
	patch ExtractionPatch,
) (*models.SourceExtraction, error) {
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	if ownerIdentity == "" || expectedUpdatedAt.IsZero() {
		return nil, gorm.ErrRecordNotFound
	}
	if err := patch.validate(); err != nil {
		return nil, err
	}
	updates := map[string]any{
		"updated_at": gorm.Expr("GREATEST(clock_timestamp(), updated_at + INTERVAL '1 microsecond')"),
	}
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
	var updated models.SourceExtraction
	err := r.DB.Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&models.SourceExtraction{}).
			Where(`source_extractions.id = ? AND source_extractions.updated_at = ? AND EXISTS (
				SELECT 1 FROM connected_sources
				WHERE connected_sources.id = source_extractions.source_id
				AND connected_sources.owner_identity = ?
			)`, id, expectedUpdatedAt, ownerIdentity).
			Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrExtractionPatchConflict
		}
		return tx.Where("id = ?", id).First(&updated).Error
	})
	if err != nil {
		return nil, err
	}
	return &updated, nil
}

func (r *GormRepository) DeleteExtractionForOwner(
	expected *models.SourceExtraction,
	expectedSource *models.ConnectedSource,
	ownerIdentity string,
) error {
	return r.DeleteExtractionForOwnerGuarded(expected, expectedSource, ownerIdentity, nil)
}

// DeleteExtractionForOwnerGuarded validates and locks the complete deletion
// target, applies every database deletion in one transaction, then invokes the
// caller's final-effect callback immediately before commit. The callback is
// never reached until the worker lease, revisions, correction outbox states,
// and all row mutations have succeeded; returning an error rolls the database
// transaction back.
func (r *GormRepository) DeleteExtractionForOwnerGuarded(
	expected *models.SourceExtraction,
	expectedSource *models.ConnectedSource,
	ownerIdentity string,
	beforeCommit func() error,
) error {
	return r.DeleteExtractionForOwnerGuardedInTransaction(
		expected,
		expectedSource,
		ownerIdentity,
		func(_ *gorm.DB) (func(bool), error) {
			if beforeCommit == nil {
				return nil, nil
			}
			return nil, beforeCommit()
		},
	)
}

// DeleteExtractionForOwnerGuardedInTransaction lets the final authorization
// and workflow state participate in the same transaction as source deletion.
// The finalizer returned by beforeCommit always runs after commit or rollback;
// it receives true only after a successful commit. Advisory projection code
// must check that result before touching external systems.
func (r *GormRepository) DeleteExtractionForOwnerGuardedInTransaction(
	expected *models.SourceExtraction,
	expectedSource *models.ConnectedSource,
	ownerIdentity string,
	beforeCommit func(*gorm.DB) (func(bool), error),
) error {
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	if expected == nil || expectedSource == nil ||
		expected.SourceID != expectedSource.ID || ownerIdentity == "" {
		return gorm.ErrRecordNotFound
	}
	var finalize func(bool)
	err := r.DB.Transaction(func(tx *gorm.DB) error {
		// Sync owns the source key for the full import. Deletion follows the same
		// source -> extraction order and fails fast while that sync lease exists.
		var sourceLockAcquired bool
		if err := tx.Raw(
			"SELECT pg_try_advisory_xact_lock(?)",
			sourceSyncLeaseKey(expectedSource.ID),
		).Scan(&sourceLockAcquired).Error; err != nil {
			return err
		}
		if !sourceLockAcquired {
			return ErrSyncInProgress
		}

		// Use the same owner/extraction key as correction submission and workers.
		// Deletion must fail fast while a worker holds its session-level lease;
		// taking a blocking transaction lock here could wait behind side effects.
		lockDigest := sha256.Sum256([]byte(ownerIdentity + "\x00" + expected.ID.String()))
		var lockAcquired bool
		if err := tx.Raw(
			"SELECT pg_try_advisory_xact_lock(hashtextextended(?, 0))",
			hex.EncodeToString(lockDigest[:]),
		).Scan(&lockAcquired).Error; err != nil {
			return err
		}
		if !lockAcquired {
			return ErrExtractionCorrectionWorkerActive
		}

		var source models.ConnectedSource
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(
			"id = ? AND owner_identity = ? AND connector_key = ? AND updated_at = ?",
			expectedSource.ID,
			ownerIdentity,
			expectedSource.ConnectorKey,
			expectedSource.UpdatedAt,
		).First(&source).Error; err != nil {
			return err
		}

		var extraction models.SourceExtraction
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(
			"id = ? AND source_id = ? AND project_key = ? AND raw_item_id = ? AND content_hash = ? AND source_uri = ? AND updated_at = ?",
			expected.ID,
			expected.SourceID,
			expected.ProjectKey,
			expected.RawItemID,
			expected.ContentHash,
			expected.SourceURI,
			expected.UpdatedAt,
		).First(&extraction).Error; err != nil {
			return err
		}

		// Inspect correction metadata without locking it first. Workers acquire
		// durable_jobs before correction rows; deletion follows the same order to
		// avoid a worker/deleter deadlock.
		var correctionRefs []struct {
			ID            uuid.UUID
			OwnerIdentity string
			SourceID      uuid.UUID
			DurableJobID  uuid.UUID
		}
		if err := tx.Model(&models.SourceExtractionCorrection{}).
			Select("id, owner_identity, source_id, durable_job_id").
			Where("extraction_id = ?", expected.ID).
			Order("id").
			Find(&correctionRefs).Error; err != nil {
			return err
		}
		jobIDs := make([]uuid.UUID, 0, len(correctionRefs))
		correctionIDs := make([]uuid.UUID, 0, len(correctionRefs))
		for _, correction := range correctionRefs {
			if correction.OwnerIdentity != ownerIdentity || correction.SourceID != expected.SourceID || correction.DurableJobID == uuid.Nil {
				return gorm.ErrRecordNotFound
			}
			correctionIDs = append(correctionIDs, correction.ID)
			jobIDs = append(jobIDs, correction.DurableJobID)
		}

		var jobs []models.DurableJob
		if len(jobIDs) > 0 {
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
				Where("id IN ? AND kind = ?", jobIDs, JobKindExtractionCorrection).
				Order("id").Find(&jobs).Error; err != nil {
				return err
			}
			if len(jobs) != len(jobIDs) {
				return errors.New("source correction queue rows are incomplete")
			}
		}

		var corrections []models.SourceExtractionCorrection
		if len(correctionRefs) > 0 {
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
				Where("id IN ? AND owner_identity = ? AND source_id = ? AND extraction_id = ?",
					correctionIDs, ownerIdentity, expected.SourceID, expected.ID).
				Order("id").Find(&corrections).Error; err != nil {
				return err
			}
			if len(corrections) != len(correctionRefs) {
				return errors.New("source correction rows changed during deletion")
			}
		}

		for _, correction := range corrections {
			switch correction.Status {
			case models.SourceExtractionCorrectionPending,
				models.SourceExtractionCorrectionCompleted,
				models.SourceExtractionCorrectionConflict,
				models.SourceExtractionCorrectionFailed:
			default:
				return errors.New("source correction has an unsupported status")
			}
		}
		for _, job := range jobs {
			switch job.Status {
			case models.DurableJobPending, models.DurableJobSucceeded, models.DurableJobDead:
			case models.DurableJobRunning:
				return ErrExtractionCorrectionWorkerActive
			default:
				return errors.New("source correction queue row has an unsupported status")
			}
		}

		// The correction row contains copied patch/before-state content and the
		// durable job carries an executable correction identifier. Both are
		// physically removed in this transaction; terminal history is not kept.
		if len(corrections) > 0 {
			result := tx.Where("id IN ? AND owner_identity = ? AND source_id = ? AND extraction_id = ?",
				correctionIDs, ownerIdentity, expected.SourceID, expected.ID).
				Delete(&models.SourceExtractionCorrection{})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != int64(len(corrections)) {
				return errors.New("source correction rows changed during deletion")
			}
			result = tx.Where("id IN ? AND kind = ?", jobIDs, JobKindExtractionCorrection).
				Delete(&models.DurableJob{})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != int64(len(jobs)) {
				return errors.New("source correction queue rows changed during deletion")
			}
		}
		if err := tx.Where("extraction_id = ? AND source_id = ?", expected.ID, expected.SourceID).
			Delete(&models.SourceIndexEntry{}).Error; err != nil {
			return err
		}
		// Lesson provenance is selected by extraction identity, not by a
		// caller-controlled external SourceURI. Keep it under this transaction
		// and worker fence, before removing the extraction row; any later failure
		// (including the final-effect callback) rolls the lessons back as well.
		canonicalSourceURI := "source-extraction://" + expected.ID.String()
		if _, err := memory.DeleteSourceExtractionLessonsTx(tx, ownerIdentity, canonicalSourceURI); err != nil {
			return err
		}
		result := tx.Where(
			"id = ? AND source_id = ? AND project_key = ? AND raw_item_id = ? AND content_hash = ? AND source_uri = ? AND updated_at = ?",
			expected.ID,
			expected.SourceID,
			expected.ProjectKey,
			expected.RawItemID,
			expected.ContentHash,
			expected.SourceURI,
			expected.UpdatedAt,
		).
			Delete(&models.SourceExtraction{})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return gorm.ErrRecordNotFound
		}
		if beforeCommit != nil {
			projection, err := beforeCommit(tx)
			if err != nil {
				return err
			}
			finalize = projection
		}
		return nil
	})
	if finalize != nil {
		finalize(err == nil)
	}
	if err != nil {
		return err
	}
	return nil
}

func (r *GormRepository) SaveAuditLogInTransaction(tx *gorm.DB, log *models.SourceAuditLog) error {
	if tx == nil || log == nil {
		return errors.New("source audit requires an active transaction and a record")
	}
	return tx.Create(log).Error
}

func (r *GormRepository) SaveIndexEntry(entry *models.SourceIndexEntry) (*models.SourceIndexEntry, error) {
	if entry.ID == uuid.Nil {
		var existing models.SourceIndexEntry
		err := r.DB.Where(
			"extraction_id = ? AND index_type = ?",
			entry.ExtractionID,
			entry.IndexType,
		).First(&existing).Error
		if err == nil {
			entry.ID = existing.ID
			entry.CreatedAt = existing.CreatedAt
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
	}
	if err := r.DB.Save(entry).Error; err != nil {
		return nil, err
	}
	return entry, nil
}

// DeletePendingVectorIndex removes the legacy placeholder produced before a
// real local embedding adapter is configured. It deliberately leaves any
// future non-placeholder vector records intact.
func (r *GormRepository) DeletePendingVectorIndex(extractionID uuid.UUID) error {
	return r.DB.
		Where("extraction_id = ? AND index_type = ? AND vector_ref LIKE ?", extractionID, "vector_ref", "local-vector-pending:%").
		Delete(&models.SourceIndexEntry{}).Error
}

func (r *GormRepository) SaveAuditLog(log *models.SourceAuditLog) (*models.SourceAuditLog, error) {
	if err := r.DB.Create(log).Error; err != nil {
		return nil, err
	}
	return log, nil
}

func (r *GormRepository) FindAuditLogs(sourceID *uuid.UUID) ([]models.SourceAuditLog, error) {
	var logs []models.SourceAuditLog
	query := r.DB.Order("created_at desc")
	if sourceID != nil {
		query = query.Where("source_id = ?", *sourceID)
	}
	err := query.Find(&logs).Error
	return logs, err
}

func (r *GormRepository) FindAuditLogsForSources(sourceIDs []uuid.UUID, limit int) ([]models.SourceAuditLog, error) {
	if len(sourceIDs) == 0 {
		return []models.SourceAuditLog{}, nil
	}
	var logs []models.SourceAuditLog
	err := r.DB.Where("source_id IN ?", sourceIDs).Order("created_at desc").Limit(boundedHistoryLimit(limit)).Find(&logs).Error
	return logs, err
}

func boundedHistoryLimit(limit int) int {
	if limit <= 0 {
		return 100
	}
	if limit > 250 {
		return 250
	}
	return limit
}

// SaveOAuthToken upserts the token for a source (one token set per source).
func (r *GormRepository) SaveOAuthToken(token *models.SourceOAuthToken) error {
	var existing models.SourceOAuthToken
	err := r.DB.Where("source_id = ?", token.SourceID).First(&existing).Error
	if err == nil {
		token.ID = existing.ID
		token.CreatedAt = existing.CreatedAt
		return r.DB.Select("*").Save(token).Error
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	if token.ID == uuid.Nil {
		token.ID = uuid.New()
	}
	return r.DB.Create(token).Error
}

func (r *GormRepository) FindOAuthToken(sourceID uuid.UUID) (*models.SourceOAuthToken, error) {
	var token models.SourceOAuthToken
	if err := r.DB.Where("source_id = ?", sourceID).First(&token).Error; err != nil {
		return nil, err
	}
	return &token, nil
}

func (r *GormRepository) FindOAuthTokensForSources(sourceIDs []uuid.UUID) ([]models.SourceOAuthToken, error) {
	if len(sourceIDs) == 0 {
		return []models.SourceOAuthToken{}, nil
	}
	var tokens []models.SourceOAuthToken
	if err := r.DB.Where("source_id IN ?", sourceIDs).Find(&tokens).Error; err != nil {
		return nil, err
	}
	return tokens, nil
}
