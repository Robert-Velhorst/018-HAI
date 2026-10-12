package accountfeed

import (
	"context"
	"errors"
	"fmt"
	"time"

	"automation-hub-backend/internal/safety"
	"github.com/google/uuid"
)

var ErrIdentityPreviewUnsupported = errors.New("source identity inspection is available only for approved local JSON feeds; no provider was contacted")
var ErrIdentityPreviewChanged = errors.New("feed changed during source identity inspection; refresh feed status and inspect again")

const identityPreviewLimit = 100

type IdentityPreviewCounts struct {
	Unseen     int `json:"unseen"`
	Canonical  int `json:"canonical"`
	Historical int `json:"historical"`
	Coexisting int `json:"coexisting"`
}

type IdentityPreviewItem struct {
	ExternalID            string `json:"externalId"`
	Title                 string `json:"title"`
	State                 string `json:"state"`
	CanonicalOperationID  string `json:"canonicalOperationId,omitempty"`
	HistoricalOperationID string `json:"historicalOperationId,omitempty"`
}

type IdentityPreview struct {
	FeedID                      string                `json:"feedId"`
	ObservedAt                  time.Time             `json:"observedAt"`
	Scope                       string                `json:"scope"`
	HistoricalInventoryComplete bool                  `json:"historicalInventoryComplete"`
	ItemsObserved               int                   `json:"itemsObserved"`
	ItemsInspected              int                   `json:"itemsInspected"`
	Truncated                   bool                  `json:"truncated"`
	Counts                      IdentityPreviewCounts `json:"counts"`
	Items                       []IdentityPreviewItem `json:"items"`
}

// PreviewSourceIdentity observes a bounded local file and active ledger keys.
// It does not acquire a sync claim, call a provider, or write any ledger/audit row.
// The observations are not a transaction-wide snapshot or migration authority.
func (r *Registry) PreviewSourceIdentity(ctx context.Context, scope FeedScope, id uuid.UUID) (IdentityPreview, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if !scope.valid() {
		return IdentityPreview{}, ErrFeedInvalid
	}
	if err := ctx.Err(); err != nil {
		return IdentityPreview{}, err
	}
	record, err := r.repo.Get(ctx, scope, id)
	if err != nil {
		return IdentityPreview{}, err
	}
	feed := record.Feed
	if feed.OwnerUserID != scope.OwnerUserID || feed.WorkspaceID != scope.WorkspaceID || feed.ID != id {
		return IdentityPreview{}, ErrFeedStorageUnavailable
	}
	if feed.SourceType != SourceLocalJSONFile {
		return IdentityPreview{}, ErrIdentityPreviewUnsupported
	}
	if record.SyncToken != nil {
		return IdentityPreview{}, ErrFeedSyncBusy
	}
	if r.ops == nil {
		return IdentityPreview{}, ErrFeedStorageUnavailable
	}
	if feed.Validate() != nil {
		return IdentityPreview{}, ErrFeedInvalid
	}
	if _, err := ParseProvider(feed.Provider); err != nil {
		return IdentityPreview{}, ErrFeedInvalid
	}
	data, err := fetchFeedBytes(ctx, feed, r.opts)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return IdentityPreview{}, err
		}
		return IdentityPreview{}, fmt.Errorf("%w: %s", ErrFeedInvalid, publicSyncError(err))
	}
	parsed, err := ParseGenericFeed(data, r.maxC, r.maxM)
	if err != nil {
		return IdentityPreview{}, fmt.Errorf("%w: %s", ErrFeedInvalid, publicSyncError(err))
	}
	report := IdentityPreview{
		FeedID: id.String(), Scope: "current_local_feed_active_operations",
		ItemsObserved: len(parsed.Items), Items: make([]IdentityPreviewItem, 0, min(len(parsed.Items), identityPreviewLimit)),
		Truncated: len(parsed.Items) > identityPreviewLimit,
	}
	for _, item := range parsed.Items[:min(len(parsed.Items), identityPreviewLimit)] {
		in, err := feed.ToOperationInput(item.ToFeedItem())
		if err != nil {
			return IdentityPreview{}, ErrFeedInvalid
		}
		matches, err := r.ops.InspectSourceIdentity(ctx, in)
		if err != nil {
			return IdentityPreview{}, err
		}
		view := IdentityPreviewItem{ExternalID: boundedIdentityLabel(item.ExternalID, 256), Title: boundedIdentityLabel(in.Title, 160), State: "unseen"}
		if matches.Canonical != nil {
			view.CanonicalOperationID = matches.Canonical.ID.String()
		}
		if matches.Historical != nil {
			view.HistoricalOperationID = matches.Historical.ID.String()
		}
		switch {
		case matches.Canonical != nil && matches.Historical != nil:
			view.State = "coexisting"
			report.Counts.Coexisting++
		case matches.Canonical != nil:
			view.State = "canonical"
			report.Counts.Canonical++
		case matches.Historical != nil:
			view.State = "historical"
			report.Counts.Historical++
		default:
			report.Counts.Unseen++
		}
		report.Items = append(report.Items, view)
		report.ItemsInspected++
	}
	if err := ctx.Err(); err != nil {
		return IdentityPreview{}, err
	}
	// Reject observed configuration or sync changes without taking a writer
	// claim. This is a second observation, not an atomic ledger snapshot.
	latest, err := r.repo.Get(ctx, scope, id)
	if err != nil {
		return IdentityPreview{}, err
	}
	if latest.SyncToken != nil {
		return IdentityPreview{}, ErrFeedSyncBusy
	}
	if latest.Feed != feed || latest.LastItemsRead != record.LastItemsRead ||
		!samePreviewInstant(latest.LastAttemptAt, record.LastAttemptAt) ||
		!samePreviewInstant(latest.LastSuccessAt, record.LastSuccessAt) {
		return IdentityPreview{}, ErrIdentityPreviewChanged
	}
	if err := ctx.Err(); err != nil {
		return IdentityPreview{}, err
	}
	report.ObservedAt = r.now().UTC()
	return report, nil
}

func samePreviewInstant(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Equal(*b)
}

func boundedIdentityLabel(raw string, limit int) string {
	text := []rune(safety.RedactSecrets(raw))
	if len(text) > limit {
		return string(text[:limit]) + "..."
	}
	return string(text)
}
