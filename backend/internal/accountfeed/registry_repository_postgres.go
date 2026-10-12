package accountfeed

import (
	"context"
	"errors"
	"fmt"
	"time"

	"automation-hub-backend/internal/infra"
	"automation-hub-backend/internal/operations"
	"automation-hub-backend/internal/privacyfilter"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type accountFeedRow struct {
	Feed          `gorm:"embedded"`
	LastAttemptAt *time.Time
	LastSuccessAt *time.Time
	LastItemsRead int
	SyncToken     *uuid.UUID `gorm:"type:uuid"`
	SyncStartedAt *time.Time
}

func (accountFeedRow) TableName() string { return "account_feeds" }

type accountFeedAuditRow struct {
	AuditEvent  `gorm:"embedded"`
	OwnerUserID string
	WorkspaceID string
}

func (accountFeedAuditRow) TableName() string { return "account_feed_audits" }

type GormRegistryRepository struct{ db *gorm.DB }

func NewGormRegistryRepository(db *gorm.DB) *GormRegistryRepository {
	return &GormRegistryRepository{db: db}
}

// NewPostgresRegistry is the production composition, never a volatile fallback.
// Startup probes the migrated schema without running private test migrations.
func NewPostgresRegistry(ctx context.Context, ops *operations.Service, privacy *privacyfilter.Service, opts FetchOptions) (*Registry, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	db, err := infra.GetDefaultDB()
	if err != nil {
		return nil, storageError(err)
	}
	var rows []accountFeedRow
	if err := db.WithContext(ctx).Select("id, owner_user_id, workspace_id, name, provider, account_label, source_type, path, url, project_key, operation_type, enabled, config_version, last_attempt_at, last_success_at, last_items_read, sync_token, sync_started_at").Limit(0).Find(&rows).Error; err != nil {
		return nil, storageError(err)
	}
	var audits []accountFeedAuditRow
	if err := db.WithContext(ctx).Select("id, feed_id, owner_user_id, workspace_id, event_type, message, created_at").Limit(0).Find(&audits).Error; err != nil {
		return nil, storageError(err)
	}
	var origins []operations.SourceOrigin
	if err := db.WithContext(ctx).Select("owner_user_id, workspace_id, origin_id, config_digest, config_epoch, registry_managed, enabled, registry_config_version").Limit(0).Find(&origins).Error; err != nil {
		return nil, storageError(err)
	}
	return NewRegistryWithRepository(NewGormRegistryRepository(db), ops, privacy, opts)
}

func storageError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrFeedNotFound
	}
	if errors.Is(err, ErrFeedNotFound) || errors.Is(err, ErrFeedSyncBusy) || errors.Is(err, ErrFeedInvalid) || errors.Is(err, ErrFeedDisabled) {
		return err
	}
	return fmt.Errorf("%w: %v", ErrFeedStorageUnavailable, err)
}

func (p *GormRegistryRepository) ready() error {
	if p == nil || p.db == nil {
		return ErrFeedStorageUnavailable
	}
	return nil
}

func scopedFeedDB(db *gorm.DB, scope FeedScope) *gorm.DB {
	return db.Where("owner_user_id = ? AND workspace_id = ?", scope.OwnerUserID, scope.WorkspaceID)
}

func (p *GormRegistryRepository) List(ctx context.Context, scope FeedScope) ([]FeedRecord, error) {
	if err := p.ready(); err != nil {
		return nil, err
	}
	if !scope.valid() {
		return nil, ErrFeedInvalid
	}
	var rows []accountFeedRow
	if err := scopedFeedDB(p.db.WithContext(ctx), scope).Order("name ASC, id ASC").Find(&rows).Error; err != nil {
		return nil, storageError(err)
	}
	out := make([]FeedRecord, 0, len(rows))
	for _, row := range rows {
		out = append(out, FeedRecord{Feed: row.Feed, LastAttemptAt: row.LastAttemptAt, LastSuccessAt: row.LastSuccessAt, LastItemsRead: row.LastItemsRead, SyncToken: row.SyncToken, SyncStartedAt: row.SyncStartedAt})
	}
	return out, nil
}

func (p *GormRegistryRepository) Get(ctx context.Context, scope FeedScope, id uuid.UUID) (FeedRecord, error) {
	if err := p.ready(); err != nil {
		return FeedRecord{}, err
	}
	if !scope.valid() || id == uuid.Nil {
		return FeedRecord{}, ErrFeedInvalid
	}
	var row accountFeedRow
	if err := scopedFeedDB(p.db.WithContext(ctx), scope).Where("id = ?", id).First(&row).Error; err != nil {
		return FeedRecord{}, storageError(err)
	}
	return FeedRecord{Feed: row.Feed, LastAttemptAt: row.LastAttemptAt, LastSuccessAt: row.LastSuccessAt, LastItemsRead: row.LastItemsRead, SyncToken: row.SyncToken, SyncStartedAt: row.SyncStartedAt}, nil
}

func (p *GormRegistryRepository) Register(ctx context.Context, feed Feed, event AuditEvent) (Feed, error) {
	if err := p.ready(); err != nil {
		return Feed{}, err
	}
	var stored Feed
	feed.ConfigVersion = 1
	err := p.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		row := accountFeedRow{Feed: feed}
		result := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "id"}}, DoNothing: true}).Create(&row)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			if err := scopedFeedDB(tx, feedScope(feed)).Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", feed.ID).First(&row).Error; err != nil {
				return err
			}
			if err := operations.PublishRegistrySourceConfiguration(tx, row.Feed.SourceObservationStart(), row.Feed.Enabled); err != nil {
				return err
			}
			stored = row.Feed
			return nil
		}
		if err := tx.Create(&accountFeedAuditRow{AuditEvent: event, OwnerUserID: feed.OwnerUserID, WorkspaceID: feed.WorkspaceID}).Error; err != nil {
			return err
		}
		if err := operations.PublishRegistrySourceConfiguration(tx, feed.SourceObservationStart(), feed.Enabled); err != nil {
			return err
		}
		stored = feed
		return nil
	})
	return stored, storageError(err)
}

func (p *GormRegistryRepository) Patch(ctx context.Context, scope FeedScope, id uuid.UUID, patch FeedPatch, event AuditEvent) (Feed, error) {
	if err := p.ready(); err != nil {
		return Feed{}, err
	}
	if !scope.valid() {
		return Feed{}, ErrFeedInvalid
	}
	var stored Feed
	err := p.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row accountFeedRow
		if err := scopedFeedDB(tx, scope).Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", id).First(&row).Error; err != nil {
			return err
		}
		if row.SyncToken != nil && (patch.Enabled == nil || *patch.Enabled) {
			return ErrFeedSyncBusy
		}
		feed, err := applyFeedPatch(row.Feed, patch)
		if err != nil {
			return err
		}
		if err := scopedFeedDB(tx.Model(&accountFeedRow{}), scope).Where("id = ?", id).Updates(map[string]any{"name": feed.Name, "enabled": feed.Enabled, "operation_type": feed.OperationType, "config_version": feed.ConfigVersion}).Error; err != nil {
			return err
		}
		if err := tx.Create(&accountFeedAuditRow{AuditEvent: event, OwnerUserID: scope.OwnerUserID, WorkspaceID: scope.WorkspaceID}).Error; err != nil {
			return err
		}
		if err := operations.PublishRegistrySourceConfiguration(tx, feed.SourceObservationStart(), feed.Enabled); err != nil {
			return err
		}
		stored = feed
		return nil
	})
	return stored, storageError(err)
}

func (p *GormRegistryRepository) Audit(ctx context.Context, scope FeedScope, id uuid.UUID) ([]AuditEvent, error) {
	if _, err := p.Get(ctx, scope, id); err != nil {
		return nil, err
	}
	var rows []accountFeedAuditRow
	if err := scopedFeedDB(p.db.WithContext(ctx), scope).Where("feed_id = ?", id).Order("created_at DESC, id DESC").Find(&rows).Error; err != nil {
		return nil, storageError(err)
	}
	out := make([]AuditEvent, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.AuditEvent)
	}
	return out, nil
}

func (p *GormRegistryRepository) BeginSync(ctx context.Context, scope FeedScope, id, token uuid.UUID, at time.Time) (Feed, error) {
	return p.beginSync(ctx, scope, id, token, at, false)
}

func (p *GormRegistryRepository) BeginEnabledSync(ctx context.Context, scope FeedScope, id, token uuid.UUID, at time.Time) (Feed, error) {
	return p.beginSync(ctx, scope, id, token, at, true)
}

func (p *GormRegistryRepository) beginSync(ctx context.Context, scope FeedScope, id, token uuid.UUID, at time.Time, enabledOnly bool) (Feed, error) {
	if err := p.ready(); err != nil {
		return Feed{}, err
	}
	if !scope.valid() || token == uuid.Nil {
		return Feed{}, ErrFeedInvalid
	}
	var feed Feed
	err := p.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row accountFeedRow
		if err := scopedFeedDB(tx, scope).Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", id).First(&row).Error; err != nil {
			return err
		}
		if !row.Feed.Enabled {
			return ErrFeedDisabled
		}
		if row.SyncToken != nil {
			return ErrFeedSyncBusy
		}
		if err := scopedFeedDB(tx.Model(&accountFeedRow{}), scope).Where("id = ? AND sync_token IS NULL", id).Updates(map[string]any{"sync_token": token, "sync_started_at": at.UTC(), "last_attempt_at": at.UTC()}).Error; err != nil {
			return err
		}
		event := newFeedAudit(id, "sync_started", "feed sync started", at)
		if err := tx.Create(&accountFeedAuditRow{AuditEvent: event, OwnerUserID: scope.OwnerUserID, WorkspaceID: scope.WorkspaceID}).Error; err != nil {
			return err
		}
		if err := operations.PublishRegistrySourceConfiguration(tx, row.Feed.SourceObservationStart(), row.Feed.Enabled); err != nil {
			return err
		}
		feed = row.Feed
		return nil
	})
	return feed, storageError(err)
}

func (p *GormRegistryRepository) FinishSync(ctx context.Context, scope FeedScope, id, token uuid.UUID, outcome SyncOutcome) error {
	if err := p.ready(); err != nil {
		return err
	}
	if !scope.valid() || token == uuid.Nil || outcome.ItemsRead < 0 {
		return ErrFeedInvalid
	}
	err := p.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		updates := map[string]any{"sync_token": nil, "sync_started_at": nil, "last_items_read": outcome.ItemsRead}
		if outcome.Event.EventType == "synced" {
			updates["last_success_at"] = outcome.Event.CreatedAt.UTC()
		}
		result := scopedFeedDB(tx.Model(&accountFeedRow{}), scope).Where("id = ? AND sync_token = ?", id, token).Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrFeedSyncBusy
		}
		return tx.Create(&accountFeedAuditRow{AuditEvent: outcome.Event, OwnerUserID: scope.OwnerUserID, WorkspaceID: scope.WorkspaceID}).Error
	})
	return storageError(err)
}
