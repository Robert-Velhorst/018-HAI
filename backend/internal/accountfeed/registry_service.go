package accountfeed

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"automation-hub-backend/internal/operations"
	"automation-hub-backend/internal/pathsafety"
	"automation-hub-backend/internal/privacyfilter"
	"github.com/google/uuid"
)

type AuditEvent struct {
	ID        string    `json:"id"`
	FeedID    string    `json:"feedId"`
	EventType string    `json:"eventType"`
	Message   string    `json:"message"`
	CreatedAt time.Time `json:"createdAt"`
}

type SyncReport struct {
	FeedID            string   `json:"feedId"`
	ItemsRead         int      `json:"itemsRead"`
	OperationsCreated int      `json:"operationsCreated"`
	OperationsRefresh int      `json:"operationsRefreshed"`
	PrivacyFlagged    int      `json:"privacyFlagged"`
	Cursor            string   `json:"cursor,omitempty"`
	Errors            []string `json:"errors,omitempty"`
	Recorded          bool     `json:"recorded"`
	ReadCompleted     bool     `json:"readCompleted"`
}

type FeedHealth struct {
	Feed             Feed             `json:"feed"`
	ConnectionStatus ConnectionStatus `json:"connectionStatus"`
	LastSyncedAt     *time.Time       `json:"lastSyncedAt,omitempty"`
	LastAttemptAt    *time.Time       `json:"lastAttemptAt,omitempty"`
	LastItemsRead    int              `json:"lastItemsRead"`
	SyncState        string           `json:"syncState"`
	SyncStartedAt    *time.Time       `json:"syncStartedAt,omitempty"`
}

type Registry struct {
	repo       RegistryRepository
	ops        *operations.Service
	privacy    *privacyfilter.Service
	opts       FetchOptions
	maxC, maxM int
	now        func() time.Time
}

// NewRegistry is the explicitly volatile test composition. Production uses
// NewPostgresRegistry and the error-returning, request-context API below.
func NewRegistry(ops *operations.Service, privacy *privacyfilter.Service, opts FetchOptions) *Registry {
	registry, _ := NewRegistryWithRepository(NewMemoryRegistryRepository(), ops, privacy, opts)
	return registry
}

func NewRegistryWithRepository(repo RegistryRepository, ops *operations.Service, privacy *privacyfilter.Service, opts FetchOptions) (*Registry, error) {
	if repo == nil {
		return nil, ErrFeedStorageUnavailable
	}
	if binder, ok := repo.(interface {
		BindSourceAuthority(*operations.Service) error
	}); ok {
		if err := binder.BindSourceAuthority(ops); err != nil {
			return nil, err
		}
	}
	return &Registry{repo: repo, ops: ops, privacy: privacy, opts: opts, maxC: defaultMaxContentBytes, maxM: defaultMaxMetadataBytes, now: time.Now}, nil
}

func (r *Registry) RegisterContext(ctx context.Context, feed Feed) (Feed, error) {
	feed.Name = strings.TrimSpace(feed.Name)
	feed.OwnerUserID = strings.TrimSpace(feed.OwnerUserID)
	feed.WorkspaceID = strings.TrimSpace(feed.WorkspaceID)
	if feed.WorkspaceID == "" {
		feed.WorkspaceID = "local"
	}
	if err := feed.Validate(); err != nil {
		return Feed{}, fmt.Errorf("%w: %v", ErrFeedInvalid, err)
	}
	if _, err := ParseProvider(feed.Provider); err != nil {
		return Feed{}, ErrFeedInvalid
	}
	if feed.ID == uuid.Nil {
		feed.ID = uuid.New()
	}
	return r.repo.Register(ctx, feed, newFeedAudit(feed.ID, "registered", "feed registered", r.now()))
}

func (r *Registry) ListContext(ctx context.Context, scope FeedScope) ([]FeedHealth, error) {
	if !scope.valid() {
		return nil, ErrFeedInvalid
	}
	rows, err := r.repo.List(ctx, scope)
	if err != nil {
		return nil, err
	}
	return healthRows(rows), nil
}

func healthRows(rows []FeedRecord) []FeedHealth {
	out := make([]FeedHealth, 0, len(rows))
	for _, row := range rows {
		status := ConnContractOnly
		if provider, err := ParseProvider(row.Feed.Provider); err == nil {
			if bridge, ok := Bridge(provider); ok {
				status = bridge.ConnectionStatus()
			}
		}
		state := "idle"
		if row.SyncToken != nil {
			state = "running_or_interrupted"
		}
		out = append(out, FeedHealth{Feed: row.Feed, ConnectionStatus: status, LastSyncedAt: row.LastSuccessAt, LastAttemptAt: row.LastAttemptAt, LastItemsRead: row.LastItemsRead, SyncState: state, SyncStartedAt: row.SyncStartedAt})
	}
	return out
}

func (r *Registry) GetContext(ctx context.Context, scope FeedScope, id uuid.UUID) (Feed, error) {
	if !scope.valid() {
		return Feed{}, ErrFeedInvalid
	}
	record, err := r.repo.Get(ctx, scope, id)
	return record.Feed, err
}

func (r *Registry) PatchContext(ctx context.Context, scope FeedScope, id uuid.UUID, patch FeedPatch) (Feed, error) {
	if !scope.valid() {
		return Feed{}, ErrFeedInvalid
	}
	return r.repo.Patch(ctx, scope, id, patch, newFeedAudit(id, "updated", "feed updated", r.now()))
}

func (r *Registry) AuditContext(ctx context.Context, scope FeedScope, id uuid.UUID) ([]AuditEvent, error) {
	if !scope.valid() {
		return nil, ErrFeedInvalid
	}
	return r.repo.Audit(ctx, scope, id)
}

// SyncContext commits a durable claim before any observation or intake. A lost
// completion write leaves that claim blocked and clears the returned cursor.
func (r *Registry) SyncContext(ctx context.Context, scope FeedScope, id uuid.UUID) (SyncReport, error) {
	return r.syncContext(ctx, scope, id, false)
}

func (r *Registry) syncContext(ctx context.Context, scope FeedScope, id uuid.UUID, enabledOnly bool) (rep SyncReport, resultErr error) {
	rep.FeedID = id.String()
	if !scope.valid() {
		return rep, ErrFeedInvalid
	}
	token := uuid.New()
	var feed Feed
	var err error
	if enabledOnly {
		feed, err = r.repo.BeginEnabledSync(ctx, scope, id, token, r.now().UTC())
	} else {
		feed, err = r.repo.BeginSync(ctx, scope, id, token, r.now().UTC())
	}
	if err != nil {
		if ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			// Begin may have committed before reporting cancellation. This only
			// settles our exact token; it cannot authorize reads or clear another attempt.
			finishCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			finishErr := r.repo.FinishSync(finishCtx, scope, id, token, SyncOutcome{
				Event: newFeedAudit(id, "sync_failed", "feed sync cancelled before reading", r.now()),
			})
			cancel()
			if finishErr == nil {
				rep.Recorded = true
				rep.Errors = append(rep.Errors, "feed sync was cancelled before reading")
			} else if !errors.Is(finishErr, ErrFeedSyncBusy) && !errors.Is(finishErr, ErrFeedNotFound) {
				rep.Errors = append(rep.Errors, "feed sync result could not be recorded; operator review is required")
			}
		}
		if errors.Is(err, ErrFeedSyncBusy) {
			rep.Errors = append(rep.Errors, ErrFeedSyncBusy.Error())
		}
		return rep, err
	}
	eventType := "sync_failed"
	defer func() {
		// Finish the owned attempt even after an HTTP disconnect. This bounded
		// cleanup is not authorization to continue processing a cancelled request.
		finishCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		message := fmt.Sprintf("read %d items, %d new operations, %d existing operations, %d rejected, %d privacy-flagged", rep.ItemsRead, rep.OperationsCreated, rep.OperationsRefresh, len(rep.Errors), rep.PrivacyFlagged)
		outcome := SyncOutcome{ItemsRead: rep.ItemsRead, Event: newFeedAudit(id, eventType, message, r.now())}
		if err := r.repo.FinishSync(finishCtx, scope, id, token, outcome); err != nil {
			rep.Cursor = ""
			rep.Errors = append(rep.Errors, "feed sync result could not be recorded; operator review is required")
			resultErr = storageError(err)
			return
		}
		rep.Recorded = true
	}()
	if err := feed.Validate(); err != nil {
		rep.Errors = append(rep.Errors, "stored feed configuration requires operator review")
		return rep, nil
	}
	if _, err := ParseProvider(feed.Provider); err != nil {
		rep.Errors = append(rep.Errors, "stored feed provider requires operator review")
		return rep, nil
	}
	if r.ops == nil {
		rep.Errors = append(rep.Errors, "source observation storage is unavailable; nothing was read")
		return rep, nil
	}
	err = r.ops.WithSourceObservation(ctx, feed.SourceObservationStart(), func(observed context.Context) error {
		r.ingestObservedFeed(observed, feed, &rep)
		return nil
	})
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			rep.Errors = append(rep.Errors, "feed sync was cancelled")
		} else {
			rep.Errors = append(rep.Errors, "source observation could not be recorded; nothing further was read")
		}
	}
	eventType = "synced"
	if len(rep.Errors) > 0 {
		eventType = "sync_failed"
		if rep.OperationsCreated+rep.OperationsRefresh > 0 {
			eventType = "sync_partial"
		}
		rep.Cursor = ""
	}
	return rep, nil
}

func (r *Registry) ingestObservedFeed(ctx context.Context, feed Feed, rep *SyncReport) {
	data, err := fetchFeedBytes(ctx, feed, r.opts)
	if err != nil {
		rep.Errors = append(rep.Errors, publicSyncError(err))
		return
	}
	parsed, err := ParseGenericFeed(data, r.maxC, r.maxM)
	if err != nil {
		rep.Errors = append(rep.Errors, publicSyncError(err))
		return
	}
	rep.Cursor = parsed.Cursor
	rep.ReadCompleted = true
	for _, item := range parsed.Items {
		if ctx.Err() != nil {
			rep.Errors = append(rep.Errors, "feed sync was cancelled")
			break
		}
		rep.ItemsRead++
		if r.privacy != nil {
			scan := r.privacy.Scan(item.Content, item.ExternalID, "", 280)
			if !scan.Result.SafeForCloudModel || scan.Result.PrivacyRiskLevel == privacyfilter.RiskHigh || scan.Result.PrivacyRiskLevel == privacyfilter.RiskCritical {
				rep.PrivacyFlagged++
			}
		}
		in, err := feed.ToOperationInput(item.ToFeedItem())
		if err != nil {
			rep.Errors = append(rep.Errors, "a feed item was rejected during bounded validation")
			continue
		}
		if r.ops == nil {
			rep.Errors = append(rep.Errors, "a feed item could not be recorded")
			continue
		}
		res, err := r.ops.IngestContext(ctx, in)
		if err != nil {
			switch {
			case errors.Is(err, operations.ErrSourceIdentityMigrationRequired):
				rep.Errors = append(rep.Errors, "a historical feed identity needs operator reconciliation; existing work was preserved")
			case errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded):
				rep.Errors = append(rep.Errors, "feed sync was cancelled")
			default:
				rep.Errors = append(rep.Errors, "a feed item could not be recorded")
			}
			continue
		}
		if res.Created {
			rep.OperationsCreated++
		} else {
			rep.OperationsRefresh++
		}
	}
	if ctx.Err() != nil && len(rep.Errors) == 0 {
		rep.Errors = append(rep.Errors, "feed sync was cancelled")
	}
}

func (r *Registry) SyncDueContext(ctx context.Context, scope FeedScope) ([]SyncReport, error) {
	if !scope.valid() {
		return nil, ErrFeedInvalid
	}
	rows, err := r.repo.List(ctx, scope)
	if err != nil {
		return nil, err
	}
	reports := make([]SyncReport, 0, len(rows))
	for _, row := range rows {
		if !row.Feed.Enabled {
			continue
		}
		if ctx.Err() != nil {
			return reports, ctx.Err()
		}
		rep, err := r.syncContext(ctx, scope, row.Feed.ID, true)
		if errors.Is(err, ErrFeedDisabled) {
			continue
		}
		reports = append(reports, rep)
		if err != nil && !errors.Is(err, ErrFeedSyncBusy) {
			return reports, err
		}
	}
	return reports, nil
}

func publicSyncError(err error) string {
	if errors.Is(err, pathsafety.ErrUnsafePath) || errors.Is(err, pathsafety.ErrPathLink) || errors.Is(err, pathsafety.ErrPathSubstituted) {
		return "feed path is not approved"
	}
	message := strings.ToLower(strings.TrimSpace(err.Error()))
	switch {
	case strings.Contains(message, "http feeds are disabled"):
		return "HTTP feed sync is disabled by policy"
	case strings.Contains(message, "unsafe feed path"):
		return "feed path is not approved"
	case strings.Contains(message, "exceeds"):
		return "feed content exceeds the configured size limit"
	case strings.Contains(message, "must be a json") || strings.Contains(message, "invalid character"):
		return "feed content is not valid JSON"
	default:
		return "feed sync failed; inspect local operator diagnostics"
	}
}

// The compatibility helpers below are intentionally restricted to the volatile
// test composition. They cannot swallow production storage failures as empty
// lists, missing feeds or successful writes.
func (r *Registry) memoryRepository() *MemoryRegistryRepository {
	repo, ok := r.repo.(*MemoryRegistryRepository)
	if !ok {
		panic("production account feeds require context/error-returning methods")
	}
	return repo
}
func (r *Registry) Register(feed Feed) (Feed, error) {
	return r.RegisterContext(context.Background(), feed)
}
func (r *Registry) List() []Feed {
	rows, _ := r.memoryRepository().List(context.Background(), FeedScope{})
	out := make([]Feed, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.Feed)
	}
	return out
}
func (r *Registry) Get(id uuid.UUID) (Feed, bool) {
	row, err := r.memoryRepository().Get(context.Background(), FeedScope{}, id)
	return row.Feed, err == nil
}
func (r *Registry) GetForOwner(id uuid.UUID, owner string) (Feed, bool) {
	row, err := r.memoryRepository().Get(context.Background(), FeedScope{OwnerUserID: owner}, id)
	return row.Feed, err == nil
}
func (r *Registry) Patch(id uuid.UUID, enabled *bool, name, operationType *string) (Feed, bool) {
	feed, ok := r.Get(id)
	if !ok {
		return Feed{}, false
	}
	result, err := r.PatchContext(context.Background(), feedScope(feed), id, FeedPatch{enabled, name, operationType})
	return result, err == nil
}
func (r *Registry) PatchForOwner(id uuid.UUID, owner string, enabled *bool, name, operationType *string) (Feed, bool) {
	feed, ok := r.GetForOwner(id, owner)
	if !ok {
		return Feed{}, false
	}
	result, err := r.PatchContext(context.Background(), feedScope(feed), id, FeedPatch{enabled, name, operationType})
	return result, err == nil
}
func (r *Registry) Health() []FeedHealth {
	rows, _ := r.memoryRepository().List(context.Background(), FeedScope{})
	return healthRows(rows)
}
func (r *Registry) HealthForOwner(owner string) []FeedHealth {
	rows, _ := r.memoryRepository().List(context.Background(), FeedScope{OwnerUserID: owner})
	return healthRows(rows)
}
func (r *Registry) Sync(ctx context.Context, id uuid.UUID) (SyncReport, bool) {
	feed, ok := r.Get(id)
	if !ok {
		return SyncReport{}, false
	}
	rep, err := r.SyncContext(ctx, feedScope(feed), id)
	return rep, !errors.Is(err, ErrFeedNotFound)
}
func (r *Registry) SyncForOwner(ctx context.Context, id uuid.UUID, owner string) (SyncReport, bool) {
	feed, ok := r.GetForOwner(id, owner)
	if !ok {
		return SyncReport{}, false
	}
	rep, err := r.SyncContext(ctx, feedScope(feed), id)
	return rep, !errors.Is(err, ErrFeedNotFound)
}
func (r *Registry) SyncDue(ctx context.Context) []SyncReport { return r.syncDueLegacy(ctx, "") }
func (r *Registry) SyncDueForOwner(ctx context.Context, owner string) []SyncReport {
	return r.syncDueLegacy(ctx, owner)
}
func (r *Registry) syncDueLegacy(ctx context.Context, owner string) []SyncReport {
	rows, _ := r.memoryRepository().List(ctx, FeedScope{OwnerUserID: owner})
	reports := make([]SyncReport, 0, len(rows))
	for _, row := range rows {
		if row.Feed.Enabled {
			rep, _ := r.SyncContext(ctx, feedScope(row.Feed), row.Feed.ID)
			reports = append(reports, rep)
		}
	}
	return reports
}
func (r *Registry) Audit(id uuid.UUID) []AuditEvent {
	events, _ := r.memoryRepository().Audit(context.Background(), FeedScope{}, id)
	return events
}
func (r *Registry) AuditForOwner(id uuid.UUID, owner string) ([]AuditEvent, bool) {
	events, err := r.memoryRepository().Audit(context.Background(), FeedScope{OwnerUserID: owner}, id)
	return events, err == nil
}
