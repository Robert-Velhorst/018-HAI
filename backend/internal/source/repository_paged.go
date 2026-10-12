package source

import (
	"strings"

	"automation-hub-backend/internal/models"
	"github.com/google/uuid"
)

type pagedSourceHistoryRepository interface {
	FindSyncJobsPageForSources(sourceIDs []uuid.UUID, limit, offset int) ([]models.SourceSyncJob, int, error)
	FindExtractionsPageForSources(sourceIDs []uuid.UUID, projectKey string, includeArchived bool, limit, offset int) ([]models.SourceExtraction, int, error)
	FindAuditLogsPageForSources(sourceIDs []uuid.UUID, limit, offset int) ([]models.SourceAuditLog, int, error)
}

type sourceOverviewRepository interface {
	FindOverviewForSources(sourceIDs []uuid.UUID, projectKey string, includeArchived bool) (*SourceOverview, error)
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

func (r *GormRepository) FindSyncJobsPageForSources(sourceIDs []uuid.UUID, limit, offset int) ([]models.SourceSyncJob, int, error) {
	if len(sourceIDs) == 0 {
		return []models.SourceSyncJob{}, 0, nil
	}
	query := r.DB.Model(&models.SourceSyncJob{}).Where("source_id IN ?", sourceIDs)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var items []models.SourceSyncJob
	err := query.Order("created_at DESC").Order("id DESC").Limit(limit).Offset(offset).Find(&items).Error
	return items, int(total), err
}

func (r *GormRepository) FindExtractionsPageForSources(sourceIDs []uuid.UUID, projectKey string, includeArchived bool, limit, offset int) ([]models.SourceExtraction, int, error) {
	if len(sourceIDs) == 0 {
		return []models.SourceExtraction{}, 0, nil
	}
	query := r.DB.Model(&models.SourceExtraction{}).Where("source_id IN ?", sourceIDs)
	if projectKey = strings.TrimSpace(projectKey); projectKey != "" {
		query = query.Where("project_key = ?", projectKey)
	}
	if !includeArchived {
		query = query.Where("archived = ?", false)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var items []models.SourceExtraction
	err := query.Order("updated_at DESC").Order("id DESC").Limit(limit).Offset(offset).Find(&items).Error
	return items, int(total), err
}

func (r *GormRepository) FindAuditLogsPageForSources(sourceIDs []uuid.UUID, limit, offset int) ([]models.SourceAuditLog, int, error) {
	if len(sourceIDs) == 0 {
		return []models.SourceAuditLog{}, 0, nil
	}
	query := r.DB.Model(&models.SourceAuditLog{}).Where("source_id IN ?", sourceIDs)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var items []models.SourceAuditLog
	err := query.Order("created_at DESC").Order("id DESC").Limit(limit).Offset(offset).Find(&items).Error
	return items, int(total), err
}

func (r *GormRepository) FindOverviewForSources(sourceIDs []uuid.UUID, projectKey string, includeArchived bool) (*SourceOverview, error) {
	overview := &SourceOverview{
		ExtractionCountsBySource: map[string]int64{},
		LatestJobStatusBySource:  map[string]string{},
	}
	if len(sourceIDs) == 0 {
		return overview, nil
	}
	type extractionAggregate struct {
		SourceID       uuid.UUID
		Total          int64
		SensitiveTotal int64
		UncertainTotal int64
	}
	query := r.DB.Model(&models.SourceExtraction{}).
		Select("source_id, COUNT(*) AS total, COALESCE(SUM(CASE WHEN sensitive THEN 1 ELSE 0 END), 0) AS sensitive_total, COALESCE(SUM(CASE WHEN uncertain THEN 1 ELSE 0 END), 0) AS uncertain_total").
		Where("source_id IN ?", sourceIDs)
	if projectKey = strings.TrimSpace(projectKey); projectKey != "" {
		query = query.Where("project_key = ?", projectKey)
	}
	if !includeArchived {
		query = query.Where("archived = ?", false)
	}
	var extractionRows []extractionAggregate
	if err := query.Group("source_id").Scan(&extractionRows).Error; err != nil {
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
	var totals jobAggregate
	if err := r.DB.Model(&models.SourceSyncJob{}).
		Select("COALESCE(SUM(CASE WHEN status IN ('failed', 'partial_failure') THEN 1 ELSE 0 END), 0) AS failed_total, COALESCE(SUM(CASE WHEN status IN ('pending', 'running') THEN 1 ELSE 0 END), 0) AS pending_total").
		Where("source_id IN ?", sourceIDs).Scan(&totals).Error; err != nil {
		return nil, err
	}
	overview.FailedJobs, overview.PendingJobs = totals.FailedTotal, totals.PendingTotal
	type latestJob struct {
		SourceID uuid.UUID
		Status   string
	}
	var latest []latestJob
	err := r.DB.Raw("SELECT source_id, status FROM (SELECT source_id, status, ROW_NUMBER() OVER (PARTITION BY source_id ORDER BY created_at DESC, id DESC) AS row_number FROM source_sync_jobs WHERE source_id IN ?) AS ranked WHERE row_number = 1", sourceIDs).Scan(&latest).Error
	if err != nil {
		return nil, err
	}
	for _, row := range latest {
		overview.LatestJobStatusBySource[row.SourceID.String()] = row.Status
	}
	return overview, nil
}

var _ pagedSourceHistoryRepository = (*GormRepository)(nil)
var _ sourceOverviewRepository = (*GormRepository)(nil)
