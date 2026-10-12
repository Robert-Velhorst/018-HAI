package source

import (
	"strings"

	"automation-hub-backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	defaultRecentExtractionLimit = 20
	maxRecentExtractionLimit     = 500
)

// FindRecentExtractionsForSources returns a deterministic, bounded history
// window. Callers must pass source IDs already authorized for the requesting
// owner; the owner-scoped single-record lookup below is used for direct IDs.
func (r *GormRepository) FindRecentExtractionsForSources(
	sourceIDs []uuid.UUID,
	projectKey string,
	includeArchived bool,
	limit int,
) ([]models.SourceExtraction, error) {
	if len(sourceIDs) == 0 {
		return []models.SourceExtraction{}, nil
	}
	if limit <= 0 {
		limit = defaultRecentExtractionLimit
	}
	if limit > maxRecentExtractionLimit {
		limit = maxRecentExtractionLimit
	}
	query := r.DB.Model(&models.SourceExtraction{}).
		Where("source_id IN ?", sourceIDs)
	if projectKey = strings.TrimSpace(projectKey); projectKey != "" {
		query = query.Where("project_key = ?", projectKey)
	}
	if !includeArchived {
		query = query.Where("archived = ?", false)
	}
	var extractions []models.SourceExtraction
	err := query.Order("updated_at DESC, id DESC").Limit(limit).Find(&extractions).Error
	return extractions, err
}

// FindExtractionForOwner fails closed for a missing owner and scopes the
// record lookup through its parent source in the same database query.
func (r *GormRepository) FindExtractionForOwner(id uuid.UUID, ownerIdentity string) (*models.SourceExtraction, error) {
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	if id == uuid.Nil || ownerIdentity == "" {
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
