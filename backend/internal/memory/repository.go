package memory

import (
	"automation-hub-backend/internal/infra"
	"automation-hub-backend/internal/models"
	"context"
	"errors"

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

// OwnerScopedRepository lets authenticated memory operations apply their
// privacy boundary in SQL. The narrow base Repository remains available to
// trusted internal jobs and existing lightweight test repositories.
type OwnerScopedRepository interface {
	FindAllForOwner(ownerIdentity, projectKey string, includeArchived bool) ([]models.ContextMemory, error)
	FindByIDForOwner(ownerIdentity string, id uuid.UUID) (*models.ContextMemory, error)
}

type RecentOwnerScopedRepository interface {
	FindRecentForOwner(ownerIdentity, projectKey string, includeArchived bool, limit int) ([]models.ContextMemory, error)
}

// MemoryDeduplicationRepository provides a cross-instance write fence for
// deduplication. The callback must use only the supplied repository so its
// reads and writes share the lock transaction.
type MemoryDeduplicationRepository interface {
	WithMemoryDeduplicationLock(ctx context.Context, key string, write func(Repository) (*models.ContextMemory, error)) (*models.ContextMemory, error)
}

type GormRepository struct {
	DB *gorm.DB
}

func NewGormRepository(db *gorm.DB) Repository {
	return &GormRepository{DB: db}
}

func (r *GormRepository) WithMemoryDeduplicationLock(ctx context.Context, key string, write func(Repository) (*models.ContextMemory, error)) (*models.ContextMemory, error) {
	if r == nil || r.DB == nil || r.DB.Dialector == nil || write == nil {
		return nil, errors.New("memory deduplication repository is unavailable")
	}
	if r.DB.Dialector.Name() != "postgres" {
		return write(r)
	}
	var saved *models.ContextMemory
	err := r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", key).Error; err != nil {
			return err
		}
		var err error
		saved, err = write(&GormRepository{DB: tx})
		return err
	})
	if err != nil {
		return saved, err
	}
	return saved, nil
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

// FindAllForOwner excludes ownerless legacy records from authenticated reads.
// Those records require an explicit migration or trusted internal workflow;
// treating them as globally readable would expose personal context.
func (r *GormRepository) FindAllForOwner(ownerIdentity, projectKey string, includeArchived bool) ([]models.ContextMemory, error) {
	var memories []models.ContextMemory
	query := r.DB.Where("owner_identity = ?", ownerIdentity).Order("updated_at desc")
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

func (r *GormRepository) FindByIDForOwner(ownerIdentity string, id uuid.UUID) (*models.ContextMemory, error) {
	var memory models.ContextMemory
	if err := r.DB.Where("id = ? AND owner_identity = ?", id, ownerIdentity).First(&memory).Error; err != nil {
		return nil, err
	}
	return &memory, nil
}

func (r *GormRepository) FindRecentForOwner(ownerIdentity, projectKey string, includeArchived bool, limit int) ([]models.ContextMemory, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	var memories []models.ContextMemory
	query := r.DB.Where("owner_identity = ?", ownerIdentity).Order("updated_at desc").Limit(limit)
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
