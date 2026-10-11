package ambient

import (
	"automation-hub-backend/internal/infra"
	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/safety"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Repository interface {
	EnsureNeeds(needs []models.AmbientNeed) error
	Needs() ([]models.AmbientNeed, error)
	NeedOverridesForOwner(ownerIdentity string) ([]models.AmbientNeedOverride, error)
	FindNeedOverride(ownerIdentity, needKey string) (*models.AmbientNeedOverride, error)
	SaveNeedOverride(override *models.AmbientNeedOverride) (*models.AmbientNeedOverride, error)
	FindOpportunity(id uuid.UUID) (*models.AmbientOpportunity, error)
	FindOpportunityByFingerprint(fingerprint string) (*models.AmbientOpportunity, error)
	SaveOpportunity(opportunity *models.AmbientOpportunity) (*models.AmbientOpportunity, error)
	Opportunities(status string, limit int) ([]models.AmbientOpportunity, error)
	OpportunitiesForOwner(ownerIdentity, status string, limit int) ([]models.AmbientOpportunity, error)
	CreateScan(scan *models.AmbientScan) (*models.AmbientScan, error)
	UpdateScan(scan *models.AmbientScan) (*models.AmbientScan, error)
	Scans(limit int) ([]models.AmbientScan, error)
	ScansForOwner(ownerIdentity string, limit int) ([]models.AmbientScan, error)
	PruneScans(keep int) error
}

type GormRepository struct {
	db *gorm.DB
}

func NewGormRepository(db *gorm.DB) Repository {
	return &GormRepository{db: db}
}

func DefaultRepository() Repository {
	db, err := infra.GetDefaultDB()
	if err != nil {
		panic(err)
	}
	return NewGormRepository(db)
}

func (r *GormRepository) EnsureNeeds(needs []models.AmbientNeed) error {
	for index := range needs {
		if err := r.db.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "key"}},
			DoNothing: true,
		}).Create(&needs[index]).Error; err != nil {
			return err
		}
	}
	return nil
}

func (r *GormRepository) Needs() ([]models.AmbientNeed, error) {
	var needs []models.AmbientNeed
	err := r.db.Order("priority_weight desc, key asc").Find(&needs).Error
	return needs, err
}

func (r *GormRepository) NeedOverridesForOwner(ownerIdentity string) ([]models.AmbientNeedOverride, error) {
	var overrides []models.AmbientNeedOverride
	err := r.db.Where("owner_identity = ?", ownerIdentity).Order("need_key asc").Find(&overrides).Error
	return overrides, err
}

func (r *GormRepository) FindNeedOverride(ownerIdentity, needKey string) (*models.AmbientNeedOverride, error) {
	var override models.AmbientNeedOverride
	err := r.db.Where("owner_identity = ? AND need_key = ?", ownerIdentity, needKey).First(&override).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &override, nil
}

func (r *GormRepository) SaveNeedOverride(override *models.AmbientNeedOverride) (*models.AmbientNeedOverride, error) {
	if err := r.db.Save(override).Error; err != nil {
		return nil, err
	}
	return override, nil
}

func (r *GormRepository) FindOpportunity(id uuid.UUID) (*models.AmbientOpportunity, error) {
	var item models.AmbientOpportunity
	if err := r.db.First(&item, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *GormRepository) FindOpportunityByFingerprint(fingerprint string) (*models.AmbientOpportunity, error) {
	var item models.AmbientOpportunity
	err := r.db.Where("fingerprint = ?", fingerprint).First(&item).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *GormRepository) SaveOpportunity(item *models.AmbientOpportunity) (*models.AmbientOpportunity, error) {
	if item.ID == uuid.Nil {
		if err := r.db.Create(item).Error; err != nil {
			return nil, err
		}
		return item, nil
	}
	if err := r.db.Save(item).Error; err != nil {
		return nil, err
	}
	return item, nil
}

func (r *GormRepository) Opportunities(status string, limit int) ([]models.AmbientOpportunity, error) {
	return r.opportunities("", status, limit)
}

func (r *GormRepository) OpportunitiesForOwner(ownerIdentity, status string, limit int) ([]models.AmbientOpportunity, error) {
	return r.opportunities(ownerIdentity, status, limit)
}

func (r *GormRepository) opportunities(ownerIdentity, status string, limit int) ([]models.AmbientOpportunity, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var items []models.AmbientOpportunity
	query := r.db.Order("priority_score desc, last_seen_at desc").Limit(limit)
	if ownerIdentity != "" {
		query = query.Where("owner_identity = ?", ownerIdentity)
	}
	if status != "" {
		query = query.Where("status = ?", status)
	}
	err := query.Find(&items).Error
	return items, err
}

func (r *GormRepository) CreateScan(scan *models.AmbientScan) (*models.AmbientScan, error) {
	if scan == nil {
		return nil, ErrScanOutcomeUnconfirmed
	}
	scan.StartedAt = scan.StartedAt.UTC().Truncate(time.Microsecond)
	if err := r.db.Create(scan).Error; err != nil {
		return nil, err
	}
	return scan, nil
}

func (r *GormRepository) UpdateScan(scan *models.AmbientScan) (*models.AmbientScan, error) {
	if scan == nil || scan.ID == uuid.Nil || scan.StartedAt.IsZero() || scan.CompletedAt == nil ||
		(scan.Status != "completed" && scan.Status != "failed") {
		return nil, ErrScanOutcomeUnconfirmed
	}
	if r == nil || r.db == nil || r.db.Config == nil || r.db.Statement == nil {
		return nil, ErrScanContextUnavailable
	}
	// A terminal update may acknowledge only this still-running scan; no Save
	// fallback may recreate deleted rows or overwrite a newer terminal outcome.
	result := r.db.Model(&models.AmbientScan{}).
		Where("id = ? AND owner_identity = ? AND started_at = ? AND status = ?", scan.ID, scan.OwnerIdentity, scan.StartedAt.UTC().Truncate(time.Microsecond), "running").
		Updates(map[string]interface{}{
			"status": scan.Status, "completed_at": scan.CompletedAt,
			"items_examined": scan.ItemsExamined, "opportunities_found": scan.OpportunitiesFound,
			"created": scan.Created, "updated": scan.Updated, "deduplicated": scan.Deduplicated,
			"advanced": scan.Advanced, "filtered": scan.Filtered, "skipped": scan.Skipped, "blocked": scan.Blocked,
			"manifest_bytes": scan.ManifestBytes, "deduplicated_bytes": scan.DeduplicatedBytes,
			"error_message": safety.RedactSecrets(scan.ErrorMessage), "updated_at": time.Now().UTC(),
		})
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected != 1 {
		return nil, ErrScanOutcomeUnconfirmed
	}
	return scan, nil
}

func (r *GormRepository) Scans(limit int) ([]models.AmbientScan, error) {
	return r.scans("", limit)
}

func (r *GormRepository) ScansForOwner(ownerIdentity string, limit int) ([]models.AmbientScan, error) {
	return r.scans(ownerIdentity, limit)
}

func (r *GormRepository) scans(ownerIdentity string, limit int) ([]models.AmbientScan, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	var scans []models.AmbientScan
	query := r.db.Order("started_at desc").Limit(limit)
	if ownerIdentity != "" {
		query = query.Where("owner_identity = ?", ownerIdentity)
	}
	err := query.Find(&scans).Error
	return scans, err
}

func (r *GormRepository) PruneScans(keep int) error {
	return r.pruneScans("", keep)
}

type ownerScanRetentionRepository interface{ PruneScansForOwner(string, int) error }

func (r *GormRepository) PruneScansForOwner(owner string, keep int) error {
	owner = strings.TrimSpace(owner)
	if owner == "" {
		return ErrScanContextUnavailable
	}
	return r.pruneScans(owner, keep)
}

func (r *GormRepository) pruneScans(owner string, keep int) error {
	if keep < 10 {
		keep = 10
	}
	var cutoff models.AmbientScan
	query := r.db.Where("status IN ?", []string{"completed", "failed"})
	if owner != "" {
		query = query.Where("owner_identity = ?", owner)
	}
	err := query.Order("started_at desc, id desc").Offset(keep - 1).Limit(1).Take(&cutoff).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	query = r.db.Where("status IN ?", []string{"completed", "failed"})
	if owner != "" {
		query = query.Where("owner_identity = ?", owner)
	}
	return query.
		Where("started_at < ? OR (started_at = ? AND id < ?)", cutoff.StartedAt, cutoff.StartedAt, cutoff.ID).
		Delete(&models.AmbientScan{}).Error
}
