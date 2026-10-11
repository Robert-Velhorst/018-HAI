package source

import (
	"automation-hub-backend/internal/infra"
	"automation-hub-backend/internal/models"
	"crypto/sha256"
	"errors"
	"strings"
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
	FindSourcesForOwner(ownerIdentity string, includeDisabled, includeLegacyOwnerless bool) ([]models.ConnectedSource, error)
	FindSource(id uuid.UUID) (*models.ConnectedSource, error)
	CreateSyncJob(job *models.SourceSyncJob) (*models.SourceSyncJob, error)
	UpdateSyncJob(job *models.SourceSyncJob) (*models.SourceSyncJob, error)
	FindSyncJobs(sourceID *uuid.UUID, limit int) ([]models.SourceSyncJob, error)
	FindSyncJobsForSources(sourceIDs []uuid.UUID, limit int) ([]models.SourceSyncJob, error)
	FindRawItem(sourceID uuid.UUID, externalID string) (*models.SourceRawItem, error)
	SaveRawItem(item *models.SourceRawItem) (*models.SourceRawItem, error)
	FindRawItems(sourceID uuid.UUID) ([]models.SourceRawItem, error)
	FindExtractionByRawItem(rawItemID uuid.UUID) (*models.SourceExtraction, error)
	SaveExtraction(extraction *models.SourceExtraction) (*models.SourceExtraction, error)
	FindExtractions(projectKey string, includeArchived bool) ([]models.SourceExtraction, error)
	FindExtractionsForSources(sourceIDs []uuid.UUID, projectKey string, includeArchived bool) ([]models.SourceExtraction, error)
	FindRecentExtractionsForSources(sourceIDs []uuid.UUID, projectKey string, includeArchived bool, limit int) ([]models.SourceExtraction, error)
	FindExtraction(id uuid.UUID) (*models.SourceExtraction, error)
	FindExtractionForOwner(id uuid.UUID, ownerIdentity string) (*models.SourceExtraction, error)
	DeleteExtractionForOwner(
		extraction *models.SourceExtraction,
		source *models.ConnectedSource,
		ownerIdentity string,
	) error
	SaveIndexEntry(entry *models.SourceIndexEntry) (*models.SourceIndexEntry, error)
	DeletePendingVectorIndex(extractionID uuid.UUID) error
	SaveAuditLog(log *models.SourceAuditLog) (*models.SourceAuditLog, error)
	FindAuditLogs(sourceID *uuid.UUID, limit int) ([]models.SourceAuditLog, error)
	FindAuditLogsForSources(sourceIDs []uuid.UUID, limit int) ([]models.SourceAuditLog, error)
	FindOverviewForSources(sourceIDs []uuid.UUID, projectKey string, includeArchived bool) (*SourceOverview, error)
	SaveOAuthState(state *models.SourceOAuthState) error
	ConsumeOAuthState(sourceID uuid.UUID, ownerIdentity, stateDigest string, consumedAt time.Time) error
	SaveOAuthToken(token *models.SourceOAuthToken) error
	FindOAuthToken(sourceID uuid.UUID) (*models.SourceOAuthToken, error)
}

type SourceOverview struct {
	ExtractionCount          int64             `json:"extractionCount"`
	SensitiveExtractionCount int64             `json:"sensitiveExtractionCount"`
	UncertainExtractionCount int64             `json:"uncertainExtractionCount"`
	FailedJobs               int64             `json:"failedJobs"`
	PendingJobs              int64             `json:"pendingJobs"`
	ExtractionCountsBySource map[string]int64  `json:"extractionCountsBySource"`
	LatestJobStatusBySource  map[string]string `json:"latestJobStatusBySource"`
}

func emptySourceOverview() *SourceOverview {
	return &SourceOverview{
		ExtractionCountsBySource: map[string]int64{},
		LatestJobStatusBySource:  map[string]string{},
	}
}

type GormRepository struct {
	DB *gorm.DB
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
	if source == nil {
		return nil, gorm.ErrRecordNotFound
	}
	var updated models.ConnectedSource
	err := r.DB.Transaction(func(tx *gorm.DB) error {
		var current models.ConnectedSource
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&current, "id = ?", source.ID).Error; err != nil {
			return err
		}
		if err := rejectRevokedSource(&current); err != nil {
			return err
		}
		if !current.UpdatedAt.Equal(source.UpdatedAt) {
			return ErrSourceChanged
		}
		// Ownership, connector identity, creation time, and revocation state are
		// repository-controlled invariants, never caller-updatable fields.
		source.OwnerIdentity = current.OwnerIdentity
		source.ConnectorKey = current.ConnectorKey
		source.CreatedAt = current.CreatedAt
		source.RevokedAt = current.RevokedAt
		if err := tx.Select("*").Save(source).Error; err != nil {
			return err
		}
		updated = *source
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &updated, nil
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
		if err := tx.Where("source_id = ?", expected.ID).
			Delete(&models.SourceOAuthState{}).Error; err != nil {
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
	query := r.DB.Order("updated_at desc")
	if !includeDisabled {
		query = query.Where("enabled = ? AND status <> ?", true, "revoked")
	}
	err := query.Find(&sources).Error
	return sources, err
}

func (r *GormRepository) FindSourcesForOwner(ownerIdentity string, includeDisabled, includeLegacyOwnerless bool) ([]models.ConnectedSource, error) {
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	if ownerIdentity == "" {
		return []models.ConnectedSource{}, nil
	}
	var sources []models.ConnectedSource
	query := r.DB.Where("owner_identity = ?", ownerIdentity).Order("updated_at desc")
	if includeLegacyOwnerless {
		query = r.DB.Where("(owner_identity = ? OR owner_identity = '' OR owner_identity IS NULL)", ownerIdentity).Order("updated_at desc")
	}
	if !includeDisabled {
		query = query.Where("enabled = ? AND status <> ?", true, "revoked")
	}
	if err := query.Find(&sources).Error; err != nil {
		return nil, err
	}
	return sources, nil
}

func (r *GormRepository) FindSource(id uuid.UUID) (*models.ConnectedSource, error) {
	var source models.ConnectedSource
	if err := r.DB.First(&source, "id = ?", id).Error; err != nil {
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

func (r *GormRepository) UpdateSyncJob(job *models.SourceSyncJob) (*models.SourceSyncJob, error) {
	if err := r.DB.Save(job).Error; err != nil {
		return nil, err
	}
	return job, nil
}

func (r *GormRepository) FindSyncJobs(sourceID *uuid.UUID, limit int) ([]models.SourceSyncJob, error) {
	var jobs []models.SourceSyncJob
	query := r.DB.Order("created_at desc")
	if sourceID != nil {
		query = query.Where("source_id = ?", *sourceID)
	}
	if limit > 0 {
		query = query.Limit(limit)
	}
	err := query.Find(&jobs).Error
	return jobs, err
}

func (r *GormRepository) FindSyncJobsForSources(sourceIDs []uuid.UUID, limit int) ([]models.SourceSyncJob, error) {
	if len(sourceIDs) == 0 {
		return []models.SourceSyncJob{}, nil
	}
	var jobs []models.SourceSyncJob
	query := r.DB.Where("source_id IN ?", sourceIDs).Order("created_at desc")
	if limit > 0 {
		query = query.Limit(limit)
	}
	return jobs, query.Find(&jobs).Error
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
	if err := r.DB.Save(extraction).Error; err != nil {
		return nil, err
	}
	return extraction, nil
}

func (r *GormRepository) FindExtractions(projectKey string, includeArchived bool) ([]models.SourceExtraction, error) {
	return r.findExtractions(nil, projectKey, includeArchived, 0)
}

func (r *GormRepository) FindExtractionsForSources(sourceIDs []uuid.UUID, projectKey string, includeArchived bool) ([]models.SourceExtraction, error) {
	if len(sourceIDs) == 0 {
		return []models.SourceExtraction{}, nil
	}
	return r.findExtractions(sourceIDs, projectKey, includeArchived, 0)
}

func (r *GormRepository) FindRecentExtractionsForSources(sourceIDs []uuid.UUID, projectKey string, includeArchived bool, limit int) ([]models.SourceExtraction, error) {
	if len(sourceIDs) == 0 {
		return []models.SourceExtraction{}, nil
	}
	return r.findExtractions(sourceIDs, projectKey, includeArchived, limit)
}

func (r *GormRepository) findExtractions(sourceIDs []uuid.UUID, projectKey string, includeArchived bool, limit int) ([]models.SourceExtraction, error) {
	var extractions []models.SourceExtraction
	query := r.DB.Order("updated_at desc").Order("id desc")
	if sourceIDs != nil {
		query = query.Where("source_id IN ?", sourceIDs)
	}
	if projectKey != "" {
		query = query.Where("project_key = ?", projectKey)
	}
	if !includeArchived {
		query = query.Where("archived = ?", false)
	}
	if limit > 0 {
		query = query.Limit(limit)
	}
	err := query.Find(&extractions).Error
	return extractions, err
}

func (r *GormRepository) FindExtraction(id uuid.UUID) (*models.SourceExtraction, error) {
	var extraction models.SourceExtraction
	if err := r.DB.First(&extraction, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &extraction, nil
}

func (r *GormRepository) FindExtractionForOwner(id uuid.UUID, ownerIdentity string) (*models.SourceExtraction, error) {
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	if ownerIdentity == "" {
		return nil, gorm.ErrRecordNotFound
	}
	var extraction models.SourceExtraction
	err := r.DB.Model(&models.SourceExtraction{}).
		Select("source_extractions.*").
		Joins("JOIN connected_sources ON connected_sources.id = source_extractions.source_id").
		Where("source_extractions.id = ? AND connected_sources.owner_identity = ?", id, ownerIdentity).
		First(&extraction).Error
	if err != nil {
		return nil, err
	}
	return &extraction, nil
}

func (r *GormRepository) DeleteExtractionForOwner(
	expected *models.SourceExtraction,
	expectedSource *models.ConnectedSource,
	ownerIdentity string,
) error {
	if expected == nil || expectedSource == nil ||
		expected.SourceID != expectedSource.ID {
		return gorm.ErrRecordNotFound
	}
	return r.DB.Transaction(func(tx *gorm.DB) error {
		var source models.ConnectedSource
		if err := tx.Where(
			"id = ? AND owner_identity = ? AND connector_key = ? AND updated_at = ?",
			expectedSource.ID,
			ownerIdentity,
			expectedSource.ConnectorKey,
			expectedSource.UpdatedAt,
		).First(&source).Error; err != nil {
			return err
		}
		if err := tx.Where("extraction_id = ?", expected.ID).
			Delete(&models.SourceIndexEntry{}).Error; err != nil {
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
		return nil
	})
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

func (r *GormRepository) FindAuditLogs(sourceID *uuid.UUID, limit int) ([]models.SourceAuditLog, error) {
	var logs []models.SourceAuditLog
	query := r.DB.Order("created_at desc")
	if sourceID != nil {
		query = query.Where("source_id = ?", *sourceID)
	}
	if limit > 0 {
		query = query.Limit(limit)
	}
	err := query.Find(&logs).Error
	return logs, err
}

func (r *GormRepository) FindAuditLogsForSources(sourceIDs []uuid.UUID, limit int) ([]models.SourceAuditLog, error) {
	if len(sourceIDs) == 0 {
		return []models.SourceAuditLog{}, nil
	}
	var logs []models.SourceAuditLog
	query := r.DB.Where("source_id IN ?", sourceIDs).Order("created_at desc")
	if limit > 0 {
		query = query.Limit(limit)
	}
	return logs, query.Find(&logs).Error
}

func (r *GormRepository) FindOverviewForSources(sourceIDs []uuid.UUID, projectKey string, includeArchived bool) (*SourceOverview, error) {
	overview := emptySourceOverview()
	if len(sourceIDs) == 0 {
		return overview, nil
	}

	type extractionAggregate struct {
		SourceID       uuid.UUID
		Total          int64
		SensitiveTotal int64
		UncertainTotal int64
	}
	var extractionRows []extractionAggregate
	extractions := r.DB.Model(&models.SourceExtraction{}).
		Select(`source_id,
			COUNT(*) AS total,
			SUM(CASE WHEN sensitive THEN 1 ELSE 0 END) AS sensitive_total,
			SUM(CASE WHEN uncertain THEN 1 ELSE 0 END) AS uncertain_total`).
		Where("source_id IN ?", sourceIDs)
	if strings.TrimSpace(projectKey) != "" {
		extractions = extractions.Where("project_key = ?", strings.TrimSpace(projectKey))
	}
	if !includeArchived {
		extractions = extractions.Where("archived = ?", false)
	}
	if err := extractions.Group("source_id").Scan(&extractionRows).Error; err != nil {
		return nil, err
	}
	for _, row := range extractionRows {
		overview.ExtractionCount += row.Total
		overview.SensitiveExtractionCount += row.SensitiveTotal
		overview.UncertainExtractionCount += row.UncertainTotal
		overview.ExtractionCountsBySource[row.SourceID.String()] = row.Total
	}

	type jobAggregate struct {
		FailedTotal  int64
		PendingTotal int64
	}
	var jobTotals jobAggregate
	if err := r.DB.Model(&models.SourceSyncJob{}).
		Select(`
			COALESCE(SUM(CASE WHEN status IN ('failed', 'partial_failure') THEN 1 ELSE 0 END), 0) AS failed_total,
			COALESCE(SUM(CASE WHEN status IN ('pending', 'running') THEN 1 ELSE 0 END), 0) AS pending_total`).
		Where("source_id IN ?", sourceIDs).
		Scan(&jobTotals).Error; err != nil {
		return nil, err
	}
	overview.FailedJobs = jobTotals.FailedTotal
	overview.PendingJobs = jobTotals.PendingTotal

	type latestJobStatus struct {
		SourceID uuid.UUID
		Status   string
	}
	var latestRows []latestJobStatus
	if err := r.DB.Model(&models.SourceSyncJob{}).
		Select("DISTINCT ON (source_id) source_id, status").
		Where("source_id IN ?", sourceIDs).
		Order("source_id, created_at DESC, id DESC").
		Scan(&latestRows).Error; err != nil {
		return nil, err
	}
	for _, row := range latestRows {
		overview.LatestJobStatusBySource[row.SourceID.String()] = row.Status
	}
	return overview, nil
}

// SaveOAuthState replaces any earlier pending attempt for the source. The
// source row lock serializes this with revocation and guarantees the state is
// issued only for the current owner of a non-revoked source.
func (r *GormRepository) SaveOAuthState(state *models.SourceOAuthState) error {
	if state == nil {
		return gorm.ErrRecordNotFound
	}
	ownerIdentity := strings.TrimSpace(state.OwnerIdentity)
	stateDigest := strings.TrimSpace(state.StateDigest)
	if state.SourceID == uuid.Nil || ownerIdentity == "" || !validOAuthStateDigest(stateDigest) ||
		!state.ExpiresAt.After(time.Now().UTC()) {
		return ErrOAuthStateInvalid
	}
	return r.DB.Transaction(func(tx *gorm.DB) error {
		var source models.ConnectedSource
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&source, "id = ?", state.SourceID).Error; err != nil {
			return err
		}
		if err := rejectRevokedSource(&source); err != nil {
			return err
		}
		if strings.TrimSpace(source.OwnerIdentity) != ownerIdentity {
			return gorm.ErrRecordNotFound
		}
		if err := tx.Where("source_id = ?", state.SourceID).Delete(&models.SourceOAuthState{}).Error; err != nil {
			return err
		}
		if state.ID == uuid.Nil {
			state.ID = uuid.New()
		}
		state.OwnerIdentity = ownerIdentity
		state.StateDigest = stateDigest
		return tx.Create(state).Error
	})
}

// ConsumeOAuthState atomically burns exactly one unexpired owner-bound state
// before the authorization code is exchanged. A replay, stale callback, wrong
// owner, or revoked source receives the same fail-closed error.
func (r *GormRepository) ConsumeOAuthState(
	sourceID uuid.UUID,
	ownerIdentity, stateDigest string,
	consumedAt time.Time,
) error {
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	stateDigest = strings.TrimSpace(stateDigest)
	if sourceID == uuid.Nil || ownerIdentity == "" || !validOAuthStateDigest(stateDigest) {
		return ErrOAuthStateInvalid
	}
	consumedAt = consumedAt.UTC()
	return r.DB.Transaction(func(tx *gorm.DB) error {
		var source models.ConnectedSource
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&source, "id = ?", sourceID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrOAuthStateInvalid
			}
			return err
		}
		if err := rejectRevokedSource(&source); err != nil {
			return ErrOAuthStateInvalid
		}
		if strings.TrimSpace(source.OwnerIdentity) != ownerIdentity {
			return ErrOAuthStateInvalid
		}
		result := tx.Model(&models.SourceOAuthState{}).
			Where(
				"source_id = ? AND owner_identity = ? AND state_digest = ? AND consumed_at IS NULL AND expires_at >= ?",
				sourceID,
				ownerIdentity,
				stateDigest,
				consumedAt,
			).
			Update(
				"consumed_at",
				gorm.Expr("CASE WHEN created_at > ? THEN created_at ELSE ? END", consumedAt, consumedAt),
			)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrOAuthStateInvalid
		}
		return nil
	})
}

func validOAuthStateDigest(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	for _, character := range value {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}

// SaveOAuthToken upserts the token for a source (one token set per source).
func (r *GormRepository) SaveOAuthToken(token *models.SourceOAuthToken) error {
	if token == nil {
		return gorm.ErrRecordNotFound
	}
	return r.DB.Transaction(func(tx *gorm.DB) error {
		var source models.ConnectedSource
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&source, "id = ?", token.SourceID).Error; err != nil {
			return err
		}
		if err := rejectRevokedSource(&source); err != nil {
			return err
		}
		var existing models.SourceOAuthToken
		err := tx.Where("source_id = ?", token.SourceID).First(&existing).Error
		if err == nil {
			token.ID = existing.ID
			token.CreatedAt = existing.CreatedAt
			return tx.Select("*").Save(token).Error
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if token.ID == uuid.Nil {
			token.ID = uuid.New()
		}
		return tx.Create(token).Error
	})
}

func (r *GormRepository) FindOAuthToken(sourceID uuid.UUID) (*models.SourceOAuthToken, error) {
	var token models.SourceOAuthToken
	if err := r.DB.Where("source_id = ?", sourceID).First(&token).Error; err != nil {
		return nil, err
	}
	return &token, nil
}
