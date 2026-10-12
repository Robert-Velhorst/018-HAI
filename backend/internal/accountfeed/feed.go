// Package accountfeed is a generic, provider-agnostic JSON feed source (§10.11).
// A feed is a named local source of raw items (e.g. exported inbox/task JSON)
// that are normalized into operations.NewOperationInput for the Operation
// Ledger. Phase 2A ships a local JSON-file reader only — no live provider
// integrations are claimed. New providers implement the Reader interface.
package accountfeed

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"automation-hub-backend/internal/idempotency"
	"automation-hub-backend/internal/operations"

	"github.com/google/uuid"
)

// SourceType identifies how a feed's raw items are obtained.
type SourceType string

const (
	// SourceLocalJSONFile reads items from a local JSON file (Phase 2A).
	SourceLocalJSONFile SourceType = "local_json_file"
	// SourceHTTPJSONFeed reads items from an HTTP JSON feed URL (only if enabled).
	SourceHTTPJSONFeed SourceType = "http_json_feed"
)

// Feed is a registered account feed configuration.
type Feed struct {
	ID            uuid.UUID  `json:"id"`
	Name          string     `json:"name"`
	Provider      string     `json:"provider"`     // one of the supported provider contracts
	AccountLabel  string     `json:"accountLabel"` // which account this feed represents
	SourceType    SourceType `json:"sourceType"`
	Path          string     `json:"path,omitempty"` // for SourceLocalJSONFile, the item filename
	URL           string     `json:"url,omitempty"`  // for SourceHTTPJSONFeed
	WorkspaceID   string     `json:"workspaceId"`
	OwnerUserID   string     `json:"ownerUserId"`
	ProjectKey    string     `json:"projectKey"`
	OperationType string     `json:"operationType"` // default op type for items lacking one
	Enabled       bool       `json:"enabled"`
	ConfigVersion int64      `json:"configVersion"`
}

// Bind the pre-read ticket to the exact configuration without exposing paths
// or provider URLs in its public provenance. Item overrides cannot mint tickets.
func (f Feed) SourceObservationStart() operations.SourceObservationStart {
	// Public marshaling redacts credentials; authority must hash the private copy.
	type sourceFeedConfiguration Feed
	encoded, _ := json.Marshal(struct {
		Version int                     `json:"version"`
		Feed    sourceFeedConfiguration `json:"feed"`
	}{1, sourceFeedConfiguration(f)})
	digest := sha256.Sum256(encoded)
	return operations.SourceObservationStart{
		OwnerUserID: strings.TrimSpace(f.OwnerUserID), WorkspaceID: firstNonEmpty(strings.TrimSpace(f.WorkspaceID), "local"),
		OriginID: f.ID, ConfigDigest: hex.EncodeToString(digest[:]), RegistryManaged: true, ConfigVersion: f.ConfigVersion,
	}
}

// Validate checks a feed is well-formed before it is used.
func (f Feed) Validate() error {
	if strings.TrimSpace(f.Name) == "" {
		return fmt.Errorf("accountfeed: name required")
	}
	if strings.TrimSpace(f.OwnerUserID) == "" {
		return fmt.Errorf("accountfeed: ownerUserId required")
	}
	switch f.SourceType {
	case SourceLocalJSONFile:
		if strings.TrimSpace(f.Path) == "" {
			return fmt.Errorf("accountfeed: path required for %s", f.SourceType)
		}
	case SourceHTTPJSONFeed:
		if strings.TrimSpace(f.URL) == "" {
			return fmt.Errorf("accountfeed: url required for %s", f.SourceType)
		}
		if err := validateFeedURL(f.URL); err != nil {
			return err
		}
	default:
		return fmt.Errorf("accountfeed: unsupported sourceType %q", f.SourceType)
	}
	return nil
}

// FeedItem is a single normalized item from a feed's raw JSON.
type FeedItem struct {
	ExternalID    string         `json:"externalId"`
	Title         string         `json:"title"`
	Body          string         `json:"body"`
	OperationType string         `json:"operationType"`
	ReceivedAt    *time.Time     `json:"receivedAt"`
	Metadata      map[string]any `json:"metadata"`
	Provider      string         `json:"provider,omitempty"`
	AccountLabel  string         `json:"accountLabel,omitempty"`
	ProjectKey    string         `json:"projectKey,omitempty"`
	// RawJSON is the exact original item text, preserved for evidence/audit.
	RawJSON string `json:"-"`
}

// Validate checks an item carries the minimum identifying content.
func (it FeedItem) Validate() error {
	if strings.TrimSpace(it.ExternalID) == "" {
		return fmt.Errorf("accountfeed: item externalId required")
	}
	if strings.TrimSpace(it.Title) == "" {
		return fmt.Errorf("accountfeed: item title required")
	}
	return nil
}

// ToOperationInput normalizes a feed item into an Operation creation input,
// computing a stable source revision hash and dedupe key so repeated syncs of
// the same item do not create duplicate Operations (§10.9).
func (f Feed) ToOperationInput(it FeedItem) (operations.NewOperationInput, error) {
	if err := it.Validate(); err != nil {
		return operations.NewOperationInput{}, err
	}
	metaCanonical, err := idempotency.CanonicalJSONString(it.Metadata)
	if err != nil {
		return operations.NewOperationInput{}, err
	}
	legacyRevision := idempotency.SourceRevisionHash(it.Body, metaCanonical)
	legacyDedupe := idempotency.FeedItemDedupeKey(f.Provider, f.AccountLabel, it.ExternalID, legacyRevision)

	opType := firstNonEmpty(it.OperationType, f.OperationType, "review_source_item")
	evidence := firstNonEmpty(strings.TrimSpace(it.RawJSON), "{}")
	provider := f.Provider
	if it.Provider != "" {
		if _, err := ParseProvider(it.Provider); err != nil {
			return operations.NewOperationInput{}, err
		}
		provider = it.Provider
	}
	accountLabel := firstNonEmpty(it.AccountLabel, f.AccountLabel, "feed:"+f.ID.String())
	if _, err := operations.SourceIdentityDigest(provider, accountLabel, it.ExternalID); err != nil {
		return operations.NewOperationInput{}, err
	}
	var receivedAt *time.Time
	if it.ReceivedAt != nil {
		utc := it.ReceivedAt.UTC()
		receivedAt = &utc
	}
	// One identity contract across readers: optional item fields cannot select
	// the historical algorithm or bypass its operator-reconciliation fence.
	semantic, err := idempotency.CanonicalJSONString(struct {
		Title, Body, OperationType string
		ReceivedAt                 *time.Time
	}{it.Title, it.Body, opType, receivedAt})
	if err != nil {
		return operations.NewOperationInput{}, err
	}
	revHash := idempotency.SourceRevisionHash(semantic, metaCanonical)
	dedupe := idempotency.StructuredFeedItemDedupeKey(provider, accountLabel, it.ExternalID, revHash)

	return operations.NewOperationInput{
		OwnerUserID:        f.OwnerUserID,
		WorkspaceID:        firstNonEmpty(f.WorkspaceID, "local"),
		Title:              it.Title,
		Description:        it.Body,
		OperationType:      opType,
		SourceType:         string(f.SourceType),
		SourceURI:          provider + ":" + accountLabel + ":" + it.ExternalID,
		SourceReceivedAt:   receivedAt,
		SourceRevisionHash: revHash,
		SourceProvider:     provider,
		SourceAccount:      accountLabel,
		SourceExternalID:   it.ExternalID,
		ProjectKey:         firstNonEmpty(it.ProjectKey, f.ProjectKey),
		AccountFeedID:      &f.ID,
		DedupeKey:          dedupe,
		LegacyDedupeKey:    legacyDedupe,
		EvidenceJSON:       evidence,
	}, nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
