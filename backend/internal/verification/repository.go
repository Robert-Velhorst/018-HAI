package verification

import (
	"automation-hub-backend/internal/infra"
	"automation-hub-backend/internal/models"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type Repository interface {
	CreateRun(run *models.VerificationRun) (*models.VerificationRun, error)
	UpdateRun(run *models.VerificationRun) (*models.VerificationRun, error)
	CreateEvidence(evidence *models.VerificationEvidence) (*models.VerificationEvidence, error)
	CreateClaim(claim *models.VerificationClaim) (*models.VerificationClaim, error)
	CreateAuditLog(log *models.VerificationAuditLog) (*models.VerificationAuditLog, error)
	FindRuns() ([]models.VerificationRun, error)
	FindRunsForOwner(ownerIdentity string) ([]models.VerificationRun, error)
	FindClaims(runID uuid.UUID) ([]models.VerificationClaim, error)
	FindEvidence(runID uuid.UUID) ([]models.VerificationEvidence, error)
}

// AtomicRepository is implemented by durable stores that can make a
// verification finalisation all-or-nothing. Small in-memory repositories used
// by focused tests are intentionally not required to implement it.
type AtomicRepository interface {
	WithinTransaction(func(Repository) error) error
}

// OwnerScopedRunRepository permits direct, database-enforced lookup for
// authenticated inspection. The base interface remains unchanged for internal
// services and compact test repositories.
type OwnerScopedRunRepository interface {
	FindRunForOwner(ownerIdentity string, id uuid.UUID) (*models.VerificationRun, error)
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

func (r *GormRepository) CreateRun(run *models.VerificationRun) (*models.VerificationRun, error) {
	if err := r.DB.Create(run).Error; err != nil {
		return nil, err
	}
	return run, nil
}

func (r *GormRepository) UpdateRun(run *models.VerificationRun) (*models.VerificationRun, error) {
	if err := r.DB.Save(run).Error; err != nil {
		return nil, err
	}
	return run, nil
}

func (r *GormRepository) CreateEvidence(evidence *models.VerificationEvidence) (*models.VerificationEvidence, error) {
	if err := r.DB.Create(evidence).Error; err != nil {
		return nil, err
	}
	return evidence, nil
}

func (r *GormRepository) CreateClaim(claim *models.VerificationClaim) (*models.VerificationClaim, error) {
	if err := r.DB.Create(claim).Error; err != nil {
		return nil, err
	}
	return claim, nil
}

func (r *GormRepository) CreateAuditLog(log *models.VerificationAuditLog) (*models.VerificationAuditLog, error) {
	if err := r.DB.Create(log).Error; err != nil {
		return nil, err
	}
	return log, nil
}

func (r *GormRepository) WithinTransaction(action func(Repository) error) error {
	if action == nil {
		return nil
	}
	return r.DB.Transaction(func(transaction *gorm.DB) error {
		return action(&GormRepository{DB: transaction})
	})
}

func (r *GormRepository) FindRuns() ([]models.VerificationRun, error) {
	var runs []models.VerificationRun
	err := r.DB.Order("created_at desc").Find(&runs).Error
	return runs, err
}

// FindRunsForOwner returns only records owned by the authenticated identity.
// Legacy ownerless runs remain available through explicit in-process methods,
// not through authenticated reads where they could expose another operator's data.
func (r *GormRepository) FindRunsForOwner(ownerIdentity string) ([]models.VerificationRun, error) {
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	if ownerIdentity == "" {
		return nil, fmt.Errorf("authenticated owner identity is required")
	}
	var runs []models.VerificationRun
	err := r.DB.Where("owner_identity = ?", ownerIdentity).Order("created_at desc").Find(&runs).Error
	return runs, err
}

// FindRunForOwner enforces exact ownership for authenticated inspection.
func (r *GormRepository) FindRunForOwner(ownerIdentity string, id uuid.UUID) (*models.VerificationRun, error) {
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	if ownerIdentity == "" {
		return nil, fmt.Errorf("authenticated owner identity is required")
	}
	var run models.VerificationRun
	if err := r.DB.Where("id = ? AND owner_identity = ?", id, ownerIdentity).First(&run).Error; err != nil {
		return nil, err
	}
	return &run, nil
}

func (r *GormRepository) FindClaims(runID uuid.UUID) ([]models.VerificationClaim, error) {
	var claims []models.VerificationClaim
	err := r.DB.Where("run_id = ?", runID).Order("created_at asc").Find(&claims).Error
	return claims, err
}

func (r *GormRepository) FindEvidence(runID uuid.UUID) ([]models.VerificationEvidence, error) {
	var evidence []models.VerificationEvidence
	err := r.DB.Where("run_id = ?", runID).Order("quality_score desc").Find(&evidence).Error
	return evidence, err
}
