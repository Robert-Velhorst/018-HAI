package accountfeed

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	ErrFeedNotFound           = errors.New("feed not found")
	ErrFeedSyncBusy           = errors.New("feed sync is running or interrupted; operator review is required before recovery")
	ErrFeedStorageUnavailable = errors.New("feed storage is unavailable")
	ErrFeedInvalid            = errors.New("invalid feed configuration or scope")
	ErrFeedDisabled           = errors.New("feed is disabled; enable it before syncing")
)

type FeedScope struct{ OwnerUserID, WorkspaceID string }

func (s FeedScope) valid() bool {
	return strings.TrimSpace(s.OwnerUserID) != "" && strings.TrimSpace(s.WorkspaceID) != ""
}

type FeedRecord struct {
	Feed                                        Feed
	LastAttemptAt, LastSuccessAt, SyncStartedAt *time.Time
	LastItemsRead                               int
	SyncToken                                   *uuid.UUID
}

type FeedPatch struct {
	Enabled             *bool
	Name, OperationType *string
}

type SyncOutcome struct {
	ItemsRead int
	Event     AuditEvent
}

// Mutations include their audit record in the same transaction. A sync token
// never expires automatically: a crashed writer must not race a later intake.
type RegistryRepository interface {
	List(context.Context, FeedScope) ([]FeedRecord, error)
	Get(context.Context, FeedScope, uuid.UUID) (FeedRecord, error)
	Register(context.Context, Feed, AuditEvent) (Feed, error)
	Patch(context.Context, FeedScope, uuid.UUID, FeedPatch, AuditEvent) (Feed, error)
	Audit(context.Context, FeedScope, uuid.UUID) ([]AuditEvent, error)
	BeginSync(context.Context, FeedScope, uuid.UUID, uuid.UUID, time.Time) (Feed, error)
	BeginEnabledSync(context.Context, FeedScope, uuid.UUID, uuid.UUID, time.Time) (Feed, error)
	FinishSync(context.Context, FeedScope, uuid.UUID, uuid.UUID, SyncOutcome) error
}

func applyFeedPatch(feed Feed, patch FeedPatch) (Feed, error) {
	before := feed
	if patch.Enabled != nil {
		feed.Enabled = *patch.Enabled
	}
	if patch.Name != nil {
		feed.Name = strings.TrimSpace(*patch.Name)
	}
	if patch.OperationType != nil {
		feed.OperationType = *patch.OperationType
	}
	if err := feed.Validate(); err != nil {
		return Feed{}, ErrFeedInvalid
	}
	if feed != before {
		if before.ConfigVersion <= 0 || before.ConfigVersion == math.MaxInt64 {
			return Feed{}, ErrFeedInvalid
		}
		feed.ConfigVersion++
	}
	return feed, nil
}

func feedScope(feed Feed) FeedScope {
	return FeedScope{OwnerUserID: feed.OwnerUserID, WorkspaceID: feed.WorkspaceID}
}

// ConfiguredFeedID makes bootstrap retryable without overwriting persisted
// operator choices. JSON encoding avoids delimiter collisions in scope/path.
func ConfiguredFeedID(owner, workspace, path string) uuid.UUID {
	if strings.TrimSpace(workspace) == "" {
		workspace = "local"
	}
	encoded, _ := json.Marshal([]string{"hai.account-feed.seed.v1", strings.TrimSpace(owner), strings.TrimSpace(workspace), strings.TrimSpace(path)})
	return uuid.NewSHA1(uuid.NameSpaceOID, encoded)
}

func newFeedAudit(id uuid.UUID, eventType, message string, at time.Time) AuditEvent {
	return AuditEvent{ID: uuid.NewString(), FeedID: id.String(), EventType: eventType, Message: message, CreatedAt: at.UTC()}
}
