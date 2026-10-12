package brain_skill_selection

import (
	"context"
	"fmt"
	"strconv"

	"gorm.io/gorm"
)

type PostgresRepository struct {
	db *gorm.DB
}

func NewPostgresRepository(db *gorm.DB) *PostgresRepository {
	return &PostgresRepository{db: db}
}

func (r *PostgresRepository) Append(ctx context.Context, event SelectionEvent) (SelectionEvent, error) {
	if r == nil || r.db == nil {
		return SelectionEvent{}, ErrRepositoryRequired
	}
	if event.ID != 0 {
		return SelectionEvent{}, fmt.Errorf("%w: event id is database-assigned", ErrInvalidInput)
	}
	if err := validateEvent(event); err != nil {
		return SelectionEvent{}, err
	}
	lockKey := strconv.Itoa(len(event.OwnerIdentity)) + ":" + event.OwnerIdentity + ":" + event.SkillID
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Serialize decisions for the same owner and skill. The event ID is
		// allocated while holding this transaction lock, so latest-state order
		// cannot invert when concurrent requests race.
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", lockKey).Error; err != nil {
			return fmt.Errorf("lock skill selection order: %w", err)
		}
		return tx.Create(&event).Error
	})
	if err != nil {
		return SelectionEvent{}, fmt.Errorf("append skill selection event: %w", err)
	}
	return event, nil
}

func (r *PostgresRepository) LatestForOwner(ctx context.Context, owner string) ([]SelectionEvent, error) {
	if r == nil || r.db == nil {
		return nil, ErrRepositoryRequired
	}
	if err := validateIdentity(owner); err != nil {
		return nil, fmt.Errorf("owner: %w", err)
	}
	var events []SelectionEvent
	err := r.db.WithContext(ctx).Raw(`
		SELECT DISTINCT ON (skill_id)
			id, owner_identity, skill_id, source_commit, source_sha256,
			guidance_sha256, catalog_fingerprint, actor_identity, enabled, decided_at
		FROM public.brain_skill_selection_events
		WHERE owner_identity = ?
		ORDER BY skill_id ASC, id DESC`, owner).Scan(&events).Error
	if err != nil {
		return nil, fmt.Errorf("load latest owner skill selections: %w", err)
	}
	return events, nil
}
