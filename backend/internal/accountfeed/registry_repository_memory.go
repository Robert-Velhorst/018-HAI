package accountfeed

import (
	"context"
	"sort"
	"sync"
	"time"

	"automation-hub-backend/internal/operations"

	"github.com/google/uuid"
)

// MemoryRegistryRepository is for isolated tests, not a production fallback.
type MemoryRegistryRepository struct {
	mu        sync.Mutex
	feeds     map[uuid.UUID]FeedRecord
	audits    map[uuid.UUID][]AuditEvent
	authority *operations.Service
}

func NewMemoryRegistryRepository() *MemoryRegistryRepository {
	return &MemoryRegistryRepository{feeds: make(map[uuid.UUID]FeedRecord), audits: make(map[uuid.UUID][]AuditEvent)}
}

func (m *MemoryRegistryRepository) BindSourceAuthority(ops *operations.Service) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if ops == nil {
		return nil // A read-only facade must not detach existing authority.
	}
	if m.authority == nil && len(m.feeds) != 0 {
		return ErrFeedStorageUnavailable // Existing rows require explicit enrollment, not late authority attachment.
	}
	if m.authority != nil && !m.authority.SharesSourceConfigurationStore(ops) {
		return ErrFeedStorageUnavailable
	}
	m.authority = ops
	return nil
}

// Called with the registry mutex held. The operation authority is published
// under its own lock across the registry commit, without reverse lock reads.
func (m *MemoryRegistryRepository) commitSourceConfiguration(ctx context.Context, feed Feed, commit func() error) error {
	if m.authority == nil {
		return commit()
	}
	return m.authority.WithRegistrySourceConfiguration(ctx, feed.SourceObservationStart(), feed.Enabled, commit)
}

func memoryVisible(scope FeedScope, feed Feed) bool {
	return (scope.OwnerUserID == "" || scope.OwnerUserID == feed.OwnerUserID) && (scope.WorkspaceID == "" || scope.WorkspaceID == feed.WorkspaceID)
}

func copyFeedRecord(record FeedRecord) FeedRecord {
	copyTime := func(value *time.Time) *time.Time {
		if value == nil {
			return nil
		}
		clone := *value
		return &clone
	}
	record.LastAttemptAt = copyTime(record.LastAttemptAt)
	record.LastSuccessAt = copyTime(record.LastSuccessAt)
	record.SyncStartedAt = copyTime(record.SyncStartedAt)
	if record.SyncToken != nil {
		token := *record.SyncToken
		record.SyncToken = &token
	}
	return record
}

func (m *MemoryRegistryRepository) lockContext(ctx context.Context) error {
	bound, finish, err := operations.BindExecutionContext(ctx)
	if err != nil {
		return err
	}
	defer finish()
	bound, cancel := context.WithTimeout(bound, 5*time.Second)
	defer cancel()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		if err := operations.ValidateExecutionContext(bound); err != nil {
			return err
		}
		if m.mu.TryLock() {
			if err := operations.ValidateExecutionContext(bound); err != nil {
				m.mu.Unlock()
				return err
			}
			return nil
		}
		select {
		case <-bound.Done():
			return bound.Err()
		case <-ticker.C:
		}
	}
}

func (m *MemoryRegistryRepository) List(ctx context.Context, scope FeedScope) ([]FeedRecord, error) {
	if err := m.lockContext(ctx); err != nil {
		return nil, err
	}
	defer m.mu.Unlock()
	rows := make([]FeedRecord, 0)
	for _, row := range m.feeds {
		if memoryVisible(scope, row.Feed) {
			rows = append(rows, copyFeedRecord(row))
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Feed.Name == rows[j].Feed.Name {
			return rows[i].Feed.ID.String() < rows[j].Feed.ID.String()
		}
		return rows[i].Feed.Name < rows[j].Feed.Name
	})
	return rows, nil
}

func (m *MemoryRegistryRepository) Get(ctx context.Context, scope FeedScope, id uuid.UUID) (FeedRecord, error) {
	if err := m.lockContext(ctx); err != nil {
		return FeedRecord{}, err
	}
	defer m.mu.Unlock()
	row, ok := m.feeds[id]
	if !ok || !memoryVisible(scope, row.Feed) {
		return FeedRecord{}, ErrFeedNotFound
	}
	return copyFeedRecord(row), nil
}

func (m *MemoryRegistryRepository) Register(ctx context.Context, feed Feed, event AuditEvent) (Feed, error) {
	if err := m.lockContext(ctx); err != nil {
		return Feed{}, err
	}
	defer m.mu.Unlock()
	if row, exists := m.feeds[feed.ID]; exists {
		if feedScope(row.Feed) != feedScope(feed) {
			return Feed{}, ErrFeedNotFound
		}
		if err := m.commitSourceConfiguration(ctx, row.Feed, func() error { return nil }); err != nil {
			return Feed{}, err
		}
		return row.Feed, nil
	}
	feed.ConfigVersion = 1
	err := m.commitSourceConfiguration(ctx, feed, func() error {
		m.feeds[feed.ID] = FeedRecord{Feed: feed}
		m.audits[feed.ID] = append(m.audits[feed.ID], event)
		return nil
	})
	if err != nil {
		return Feed{}, err
	}
	return feed, nil
}

func (m *MemoryRegistryRepository) Patch(ctx context.Context, scope FeedScope, id uuid.UUID, patch FeedPatch, event AuditEvent) (Feed, error) {
	if err := m.lockContext(ctx); err != nil {
		return Feed{}, err
	}
	defer m.mu.Unlock()
	row, ok := m.feeds[id]
	if !ok || !memoryVisible(scope, row.Feed) {
		return Feed{}, ErrFeedNotFound
	}
	if row.SyncToken != nil && (patch.Enabled == nil || *patch.Enabled) {
		return Feed{}, ErrFeedSyncBusy
	}
	feed, err := applyFeedPatch(row.Feed, patch)
	if err != nil {
		return Feed{}, err
	}
	if err := m.commitSourceConfiguration(ctx, feed, func() error {
		row.Feed = feed
		m.feeds[id] = row
		m.audits[id] = append(m.audits[id], event)
		return nil
	}); err != nil {
		return Feed{}, err
	}
	return feed, nil
}

func (m *MemoryRegistryRepository) Audit(ctx context.Context, scope FeedScope, id uuid.UUID) ([]AuditEvent, error) {
	if err := m.lockContext(ctx); err != nil {
		return nil, err
	}
	defer m.mu.Unlock()
	row, ok := m.feeds[id]
	if !ok || !memoryVisible(scope, row.Feed) {
		return nil, ErrFeedNotFound
	}
	events := m.audits[id]
	out := make([]AuditEvent, len(events))
	for i := range events {
		out[i] = events[len(events)-1-i]
	}
	return out, nil
}

func (m *MemoryRegistryRepository) BeginSync(ctx context.Context, scope FeedScope, id, token uuid.UUID, at time.Time) (Feed, error) {
	return m.beginSync(ctx, scope, id, token, at, false)
}

func (m *MemoryRegistryRepository) BeginEnabledSync(ctx context.Context, scope FeedScope, id, token uuid.UUID, at time.Time) (Feed, error) {
	return m.beginSync(ctx, scope, id, token, at, true)
}

func (m *MemoryRegistryRepository) beginSync(ctx context.Context, scope FeedScope, id, token uuid.UUID, at time.Time, enabledOnly bool) (Feed, error) {
	if token == uuid.Nil {
		return Feed{}, ErrFeedInvalid
	}
	if err := m.lockContext(ctx); err != nil {
		return Feed{}, err
	}
	defer m.mu.Unlock()
	row, ok := m.feeds[id]
	if !ok || !memoryVisible(scope, row.Feed) {
		return Feed{}, ErrFeedNotFound
	}
	if !row.Feed.Enabled {
		return Feed{}, ErrFeedDisabled
	}
	if row.SyncToken != nil {
		return Feed{}, ErrFeedSyncBusy
	}
	at = at.UTC()
	row.SyncToken = &token
	row.SyncStartedAt = &at
	row.LastAttemptAt = &at
	if err := m.commitSourceConfiguration(ctx, row.Feed, func() error {
		m.feeds[id] = row
		m.audits[id] = append(m.audits[id], newFeedAudit(id, "sync_started", "feed sync started", at))
		return nil
	}); err != nil {
		return Feed{}, err
	}
	return row.Feed, nil
}

func (m *MemoryRegistryRepository) FinishSync(ctx context.Context, scope FeedScope, id, token uuid.UUID, outcome SyncOutcome) error {
	if outcome.ItemsRead < 0 || token == uuid.Nil {
		return ErrFeedInvalid
	}
	if err := m.lockContext(ctx); err != nil {
		return err
	}
	defer m.mu.Unlock()
	row, ok := m.feeds[id]
	if !ok || !memoryVisible(scope, row.Feed) {
		return ErrFeedNotFound
	}
	if row.SyncToken == nil || *row.SyncToken != token {
		return ErrFeedSyncBusy
	}
	row.SyncToken = nil
	row.SyncStartedAt = nil
	row.LastItemsRead = outcome.ItemsRead
	if outcome.Event.EventType == "synced" {
		at := outcome.Event.CreatedAt.UTC()
		row.LastSuccessAt = &at
	}
	m.feeds[id] = row
	m.audits[id] = append(m.audits[id], outcome.Event)
	return nil
}
