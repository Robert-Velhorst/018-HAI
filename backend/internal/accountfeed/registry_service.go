package accountfeed

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"automation-hub-backend/internal/operations"
	"automation-hub-backend/internal/privacyfilter"

	"github.com/google/uuid"
)

// AuditEvent is an immutable audit row for a feed (§10.19 feed audit).
type AuditEvent struct {
	ID        string    `json:"id"`
	FeedID    string    `json:"feedId"`
	EventType string    `json:"eventType"`
	Message   string    `json:"message"`
	CreatedAt time.Time `json:"createdAt"`
}

// SyncReport summarizes one feed sync.
type SyncReport struct {
	FeedID            string   `json:"feedId"`
	ItemsRead         int      `json:"itemsRead"`
	OperationsCreated int      `json:"operationsCreated"`
	OperationsRefresh int      `json:"operationsRefreshed"`
	PrivacyFlagged    int      `json:"privacyFlagged"`
	Cursor            string   `json:"cursor,omitempty"`
	Errors            []string `json:"errors,omitempty"`
}

// FeedHealth is a feed's truthful health for the dashboard.
type FeedHealth struct {
	Feed             Feed             `json:"feed"`
	ConnectionStatus ConnectionStatus `json:"connectionStatus"`
	LastSyncedAt     *time.Time       `json:"lastSyncedAt,omitempty"`
	LastItemsRead    int              `json:"lastItemsRead"`
}

// Registry is the account feed registry (§14). It stores feed configs, syncs
// them (fetch → privacy scan → dedupe-ingest into the Operation Ledger), and
// records an audit trail. It never fakes provider access.
type Registry struct {
	mu      sync.Mutex
	feeds   map[uuid.UUID]Feed
	audits  map[uuid.UUID][]AuditEvent
	lastRun map[uuid.UUID]*time.Time
	lastN   map[uuid.UUID]int
	syncing map[uuid.UUID]bool
	seq     int

	ops     *operations.Service
	privacy *privacyfilter.Service
	opts    FetchOptions
	maxC    int
	maxM    int
	now     func() time.Time
}

// NewRegistry builds a feed registry.
func NewRegistry(ops *operations.Service, privacy *privacyfilter.Service, opts FetchOptions) *Registry {
	return &Registry{
		feeds:   map[uuid.UUID]Feed{},
		audits:  map[uuid.UUID][]AuditEvent{},
		lastRun: map[uuid.UUID]*time.Time{},
		lastN:   map[uuid.UUID]int{},
		syncing: map[uuid.UUID]bool{},
		ops:     ops,
		privacy: privacy,
		opts:    opts,
		maxC:    defaultMaxContentBytes,
		maxM:    defaultMaxMetadataBytes,
		now:     time.Now,
	}
}

// Register validates and stores a feed, returning it with an assigned id.
func (r *Registry) Register(feed Feed) (Feed, error) {
	if feed.WorkspaceID == "" {
		feed.WorkspaceID = "local"
	}
	if err := feed.Validate(); err != nil {
		return Feed{}, err
	}
	// The registry requires a valid, supported provider contract (§14).
	if _, err := ParseProvider(feed.Provider); err != nil {
		return Feed{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if feed.ID == uuid.Nil {
		feed.ID = uuid.New()
	}
	r.feeds[feed.ID] = feed
	r.appendAudit(feed.ID, "registered", "feed registered: "+feed.Name)
	return feed, nil
}

// List returns all registered feeds.
func (r *Registry) List() []Feed {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Feed, 0, len(r.feeds))
	for _, f := range r.feeds {
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Get returns a feed by id.
func (r *Registry) Get(id uuid.UUID) (Feed, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	f, ok := r.feeds[id]
	return f, ok
}

// Patch updates mutable feed fields (enabled, name, operationType).
func (r *Registry) Patch(id uuid.UUID, enabled *bool, name, operationType *string) (Feed, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	f, ok := r.feeds[id]
	if !ok {
		return Feed{}, false
	}
	if enabled != nil {
		f.Enabled = *enabled
	}
	if name != nil && *name != "" {
		f.Name = *name
	}
	if operationType != nil {
		f.OperationType = *operationType
	}
	r.feeds[id] = f
	r.appendAudit(id, "updated", "feed updated")
	return f, true
}

// Health returns truthful health for every feed.
func (r *Registry) Health() []FeedHealth {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]FeedHealth, 0, len(r.feeds))
	for id, f := range r.feeds {
		status := ConnAvailable
		if p, err := ParseProvider(f.Provider); err == nil {
			if b, ok := Bridge(p); ok {
				status = b.ConnectionStatus()
			}
		}
		out = append(out, FeedHealth{Feed: f, ConnectionStatus: status, LastSyncedAt: r.lastRun[id], LastItemsRead: r.lastN[id]})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Feed.Name < out[j].Feed.Name })
	return out
}

// Sync fetches a feed, privacy-scans each item, and ingests operations.
func (r *Registry) Sync(ctx context.Context, id uuid.UUID) (SyncReport, bool) {
	r.mu.Lock()
	feed, ok := r.feeds[id]
	r.mu.Unlock()
	if !ok {
		return SyncReport{}, false
	}
	return r.syncFeed(ctx, feed), true
}

// SyncDue syncs all enabled feeds.
func (r *Registry) SyncDue(ctx context.Context) []SyncReport {
	return r.syncDue(ctx, "")
}

// SyncDueForOwner syncs only enabled feeds belonging to the given owner.
func (r *Registry) SyncDueForOwner(ctx context.Context, ownerUserID string) []SyncReport {
	return r.syncDue(ctx, ownerUserID)
}

func (r *Registry) syncDue(ctx context.Context, ownerUserID string) []SyncReport {
	r.mu.Lock()
	var due []Feed
	for _, f := range r.feeds {
		if f.Enabled && (ownerUserID == "" || f.OwnerUserID == ownerUserID) {
			due = append(due, f)
		}
	}
	r.mu.Unlock()
	reports := make([]SyncReport, 0, len(due))
	for _, f := range due {
		reports = append(reports, r.syncFeed(ctx, f))
	}
	return reports
}

func (r *Registry) syncFeed(ctx context.Context, feed Feed) SyncReport {
	if !r.beginSync(feed.ID) {
		return SyncReport{FeedID: feed.ID.String(), Errors: []string{"sync already in progress"}}
	}
	defer r.endSync(feed.ID)

	rep := SyncReport{FeedID: feed.ID.String()}
	data, err := fetchFeedBytes(ctx, feed, r.opts)
	if err != nil {
		rep.Errors = append(rep.Errors, err.Error())
		r.recordSync(feed.ID, 0, "sync_failed", err.Error())
		return rep
	}
	parsed, err := ParseGenericFeed(data, r.maxC, r.maxM)
	if err != nil {
		rep.Errors = append(rep.Errors, err.Error())
		r.recordSync(feed.ID, 0, "sync_failed", err.Error())
		return rep
	}
	rep.Cursor = parsed.Cursor
	for _, item := range parsed.Items {
		rep.ItemsRead++
		// Privacy filter runs before storage/model use (§14 Fetcher -> Privacy).
		if r.privacy != nil {
			scan := r.privacy.Scan(item.Content, item.ExternalID, "", 280)
			if !scan.Result.SafeForCloudModel || scan.Result.PrivacyRiskLevel == privacyfilter.RiskHigh || scan.Result.PrivacyRiskLevel == privacyfilter.RiskCritical {
				rep.PrivacyFlagged++
			}
		}
		in, err := feed.ToOperationInput(item.ToFeedItem())
		if err != nil {
			rep.Errors = append(rep.Errors, fmt.Sprintf("item %s: %v", item.ExternalID, err))
			continue
		}
		res, err := r.ops.Ingest(in)
		if err != nil {
			rep.Errors = append(rep.Errors, fmt.Sprintf("ingest %s: %v", item.ExternalID, err))
			continue
		}
		if res.Created {
			rep.OperationsCreated++
		} else {
			rep.OperationsRefresh++
		}
	}
	r.recordSync(feed.ID, rep.ItemsRead, "synced", fmt.Sprintf("read %d items, %d new operations, %d privacy-flagged", rep.ItemsRead, rep.OperationsCreated, rep.PrivacyFlagged))
	return rep
}

func (r *Registry) beginSync(id uuid.UUID) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.syncing[id] {
		return false
	}
	r.syncing[id] = true
	return true
}

func (r *Registry) endSync(id uuid.UUID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.syncing, id)
}

// Audit returns a feed's audit trail (newest first).
func (r *Registry) Audit(id uuid.UUID) []AuditEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	events := r.audits[id]
	out := make([]AuditEvent, len(events))
	for i := range events {
		out[i] = events[len(events)-1-i]
	}
	return out
}

func (r *Registry) recordSync(id uuid.UUID, items int, eventType, msg string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now().UTC()
	r.lastRun[id] = &now
	r.lastN[id] = items
	r.appendAudit(id, eventType, msg)
}

// appendAudit must be called with r.mu held.
func (r *Registry) appendAudit(id uuid.UUID, eventType, msg string) {
	r.seq++
	r.audits[id] = append(r.audits[id], AuditEvent{
		ID:        fmt.Sprintf("afa-%d", r.seq),
		FeedID:    id.String(),
		EventType: eventType,
		Message:   msg,
		CreatedAt: r.now().UTC(),
	})
}
