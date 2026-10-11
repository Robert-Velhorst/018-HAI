package memory

import (
	"context"

	"automation-hub-backend/internal/infra"
	"automation-hub-backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type Repository interface {
	Create(memory *models.ContextMemory) (*models.ContextMemory, error)
	Update(memory *models.ContextMemory) (*models.ContextMemory, error)
	FindByID(id uuid.UUID) (*models.ContextMemory, error)
	FindAll(projectKey string, includeArchived bool) ([]models.ContextMemory, error)
	FindByHash(projectKey, kind, contentHash string) (*models.ContextMemory, error)
	Delete(id uuid.UUID) error
}

// OwnerScopedRepository lets the production repository apply the authenticated
// owner boundary inside the database. Service fallbacks preserve compatibility
// for trusted in-memory workers, but authenticated paths must never load other
// owners' rows merely to discard them in Go.
type OwnerScopedRepository interface {
	FindAllForOwner(ownerIdentity, projectKey string, includeArchived bool) ([]models.ContextMemory, error)
}

// OwnerQueryRepository performs bounded browse/search work in persistence.
// Context cancellation stops obsolete HTTP searches during navigation or a
// newer query, and PageResult preserves the public API contract.
type OwnerQueryRepository interface {
	QueryForOwner(context.Context, string, string, bool, QueryParams) (PageResult, error)
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

func (r *GormRepository) Create(memory *models.ContextMemory) (*models.ContextMemory, error) {
	if err := r.DB.Create(memory).Error; err != nil {
		return nil, err
	}
	return memory, nil
}

func (r *GormRepository) Update(memory *models.ContextMemory) (*models.ContextMemory, error) {
	if err := r.DB.Save(memory).Error; err != nil {
		return nil, err
	}
	return memory, nil
}

func (r *GormRepository) FindByID(id uuid.UUID) (*models.ContextMemory, error) {
	var memory models.ContextMemory
	if err := r.DB.First(&memory, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &memory, nil
}

func (r *GormRepository) FindAll(projectKey string, includeArchived bool) ([]models.ContextMemory, error) {
	var memories []models.ContextMemory
	query := r.DB.Order("updated_at desc")
	if projectKey != "" {
		query = query.Where("project_key = ?", projectKey)
	}
	if !includeArchived {
		query = query.Where("archived = ?", false)
	}
	if err := query.Find(&memories).Error; err != nil {
		return nil, err
	}
	return memories, nil
}

func (r *GormRepository) FindAllForOwner(ownerIdentity, projectKey string, includeArchived bool) ([]models.ContextMemory, error) {
	var memories []models.ContextMemory
	query := r.scopeQuery(r.DB, ownerIdentity, projectKey, includeArchived).Order("updated_at DESC, created_at DESC, id DESC")
	if err := query.Find(&memories).Error; err != nil {
		return nil, err
	}
	return memories, nil
}

func (r *GormRepository) FindByHash(projectKey, kind, contentHash string) (*models.ContextMemory, error) {
	var memory models.ContextMemory
	err := r.DB.
		Where("project_key = ? AND kind = ? AND content_hash = ? AND archived = ?", projectKey, kind, contentHash, false).
		First(&memory).Error
	if err != nil {
		return nil, err
	}
	return &memory, nil
}

func (r *GormRepository) Delete(id uuid.UUID) error {
	return r.DB.Delete(&models.ContextMemory{}, id).Error
}
