package accountfeed

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	defaultMaxContentBytes  = 200_000
	defaultMaxMetadataBytes = 16_000
	maxGenericFeedItems     = 10_000
	maxGenericFeedCursor    = 4_096
)

// GenericItem is one item in the generic feed response (§10.11).
type GenericItem struct {
	ExternalID   string         `json:"externalId"`
	ThreadID     string         `json:"threadId,omitempty"`
	Title        string         `json:"title"`
	Content      string         `json:"content"`
	SourceURI    string         `json:"sourceUri,omitempty"`
	ItemType     string         `json:"itemType"`
	Provider     string         `json:"provider"`
	AccountLabel string         `json:"accountLabel,omitempty"`
	ProjectKey   string         `json:"projectKey,omitempty"`
	ReceivedAt   *string        `json:"receivedAt,omitempty"`
	Metadata     map[string]any `json:"metadata,omitempty"`
	RawJSON      string         `json:"-"`
}

// GenericFeed is the generic feed response envelope (§10.11).
type GenericFeed struct {
	Cursor string        `json:"cursor,omitempty"`
	Items  []GenericItem `json:"items"`
}

// sourceURISecret flags secret-looking content that must not appear in a sourceUri.
var sourceURISecret = regexp.MustCompile(`(?i)(token|auth|api[_-]?key|secret|password|bearer)=`)

// Validate enforces the §10.11 item validation rules.
func (it GenericItem) Validate(maxContentBytes, maxMetadataBytes int) error {
	if strings.TrimSpace(it.ExternalID) == "" {
		return fmt.Errorf("accountfeed: item externalId required")
	}
	if strings.TrimSpace(it.Provider) == "" {
		return fmt.Errorf("accountfeed: item provider required")
	}
	if _, err := ParseProvider(it.Provider); err != nil {
		return err
	}
	if strings.TrimSpace(it.ItemType) == "" {
		return fmt.Errorf("accountfeed: item itemType required")
	}
	if _, err := ParseItemType(it.ItemType); err != nil {
		return err
	}
	if strings.TrimSpace(it.Title) == "" && strings.TrimSpace(it.Content) == "" {
		return fmt.Errorf("accountfeed: item %q requires title or content", it.ExternalID)
	}
	if it.ReceivedAt != nil {
		if _, err := time.Parse(time.RFC3339Nano, *it.ReceivedAt); err != nil {
			return fmt.Errorf("accountfeed: item receivedAt must be an RFC3339 timestamp with timezone")
		}
	}
	if maxContentBytes <= 0 {
		maxContentBytes = defaultMaxContentBytes
	}
	if len(it.Content) > maxContentBytes {
		return fmt.Errorf("accountfeed: item %q content exceeds %d bytes", it.ExternalID, maxContentBytes)
	}
	if sourceURISecret.MatchString(it.SourceURI) {
		return fmt.Errorf("accountfeed: item %q sourceUri must not contain secrets", it.ExternalID)
	}
	if maxMetadataBytes <= 0 {
		maxMetadataBytes = defaultMaxMetadataBytes
	}
	if it.Metadata != nil {
		raw, err := json.Marshal(it.Metadata)
		if err != nil {
			return fmt.Errorf("accountfeed: item metadata is not valid JSON")
		}
		if len(raw) > maxMetadataBytes {
			return fmt.Errorf("accountfeed: item %q metadata exceeds %d bytes", it.ExternalID, maxMetadataBytes)
		}
	}
	return nil
}

// ParseGenericFeed parses feed bytes as either the generic envelope
// {cursor, items:[...]} or a bare array [...]; each item is validated.
func ParseGenericFeed(data []byte, maxContentBytes, maxMetadataBytes int) (GenericFeed, error) {
	if len(data) > maxFeedBytes {
		return GenericFeed{}, fmt.Errorf("accountfeed: feed exceeds %d bytes", maxFeedBytes)
	}
	if !utf8.Valid(data) {
		return GenericFeed{}, fmt.Errorf("accountfeed: feed is not valid UTF-8 JSON")
	}
	trimmed := strings.TrimSpace(string(data))
	if err := rejectDuplicateJSONKeys(data); err != nil {
		return GenericFeed{}, err
	}
	if err := rejectAmbiguousTypedJSONKeys(data); err != nil {
		return GenericFeed{}, err
	}
	var feed GenericFeed
	if strings.HasPrefix(trimmed, "{") {
		if err := decodeItems([]byte(trimmed), &feed, true); err != nil {
			return GenericFeed{}, err
		}
	} else if strings.HasPrefix(trimmed, "[") {
		if err := decodeItems([]byte(trimmed), &feed, false); err != nil {
			return GenericFeed{}, err
		}
	} else {
		return GenericFeed{}, fmt.Errorf("accountfeed: feed must be a JSON object or array")
	}
	for i := range feed.Items {
		if err := feed.Items[i].Validate(maxContentBytes, maxMetadataBytes); err != nil {
			return GenericFeed{}, fmt.Errorf("item %d: %w", i, err)
		}
	}
	return feed, nil
}

// decodeItems decodes into feed, preserving each item's exact raw JSON.
func decodeItems(data []byte, feed *GenericFeed, envelope bool) error {
	if envelope {
		var raw struct {
			Cursor json.RawMessage `json:"cursor"`
			Items  []json.RawMessage `json:"items"`
		}
		if err := json.Unmarshal(data, &raw); err != nil {
			return fmt.Errorf("accountfeed: invalid feed envelope: %w", err)
		}
		if raw.Items == nil {
			return fmt.Errorf("accountfeed: feed envelope requires a non-null items array")
		}
		if len(raw.Cursor) == 0 {
			feed.Cursor = ""
		} else if cursor := bytes.TrimSpace(raw.Cursor); len(cursor) == 0 || cursor[0] != '"' {
			return fmt.Errorf("accountfeed: feed cursor must be a string")
		} else if err := json.Unmarshal(raw.Cursor, &feed.Cursor); err != nil {
			return fmt.Errorf("accountfeed: invalid feed cursor: %w", err)
		}
		if len(feed.Cursor) > maxGenericFeedCursor {
			return fmt.Errorf("accountfeed: feed cursor exceeds %d bytes", maxGenericFeedCursor)
		}
		return appendRawItems(feed, raw.Items)
	}
	var items []json.RawMessage
	if err := json.Unmarshal(data, &items); err != nil {
		return fmt.Errorf("accountfeed: invalid feed array: %w", err)
	}
	return appendRawItems(feed, items)
}

// encoding/json matches struct fields case-insensitively when no exact match
// exists. Reject aliases for fields in our typed objects so e.g. "items" and
// "ITEMS" cannot silently select one value. Arbitrary metadata object keys
// remain case-sensitive because they decode into maps, not structs.
func rejectAmbiguousTypedJSONKeys(data []byte) error {
	var root json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil {
		return fmt.Errorf("accountfeed: invalid JSON feed: %w", err)
	}
	trimmed := bytes.TrimSpace(root)
	var itemRaws []json.RawMessage
	switch {
	case len(trimmed) > 0 && trimmed[0] == '{':
		var envelope map[string]json.RawMessage
		if err := json.Unmarshal(trimmed, &envelope); err != nil {
			return fmt.Errorf("accountfeed: invalid feed envelope: %w", err)
		}
		if err := rejectTypedFieldAliases(envelope, "cursor", "items"); err != nil {
			return fmt.Errorf("accountfeed: %w", err)
		}
		for key, value := range envelope {
			if strings.EqualFold(key, "items") {
				if err := json.Unmarshal(value, &itemRaws); err != nil {
					return fmt.Errorf("accountfeed: invalid feed items: %w", err)
				}
				break
			}
		}
	case len(trimmed) > 0 && trimmed[0] == '[':
		if err := json.Unmarshal(trimmed, &itemRaws); err != nil {
			return fmt.Errorf("accountfeed: invalid feed array: %w", err)
		}
	default:
		return nil
	}
	for index, raw := range itemRaws {
		var item map[string]json.RawMessage
		if err := json.Unmarshal(raw, &item); err != nil {
			continue // The regular typed decoder reports the item-specific error.
		}
		if err := rejectTypedFieldAliases(item,
			"externalId", "threadId", "title", "content", "sourceUri", "itemType",
			"provider", "accountLabel", "projectKey", "receivedAt", "metadata",
		); err != nil {
			return fmt.Errorf("accountfeed: item %d: %w", index, err)
		}
	}
	return nil
}

func rejectTypedFieldAliases(fields map[string]json.RawMessage, names ...string) error {
	known := make(map[string]struct{}, len(names))
	for _, name := range names {
		known[name] = struct{}{}
	}
	seen := make(map[string]string, len(fields))
	for key := range fields {
		canonical := ""
		for name := range known {
			if strings.EqualFold(key, name) {
				canonical = name
				break
			}
		}
		if canonical == "" {
			continue
		}
		if previous, exists := seen[canonical]; exists {
			return fmt.Errorf("ambiguous field aliases %q and %q", previous, key)
		}
		seen[canonical] = key
	}
	return nil
}

func appendRawItems(feed *GenericFeed, raws []json.RawMessage) error {
	if len(raws) > maxGenericFeedItems {
		return fmt.Errorf("accountfeed: feed contains more than %d items", maxGenericFeedItems)
	}
	for i, raw := range raws {
		var it GenericItem
		if err := json.Unmarshal(raw, &it); err != nil {
			return fmt.Errorf("accountfeed: item %d: %w", i, err)
		}
		it.RawJSON = string(raw)
		feed.Items = append(feed.Items, it)
	}
	return nil
}

func rejectDuplicateJSONKeys(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := consumeJSONValue(decoder); err != nil {
		return fmt.Errorf("accountfeed: invalid JSON feed: %w", err)
	}
	if _, err := decoder.Token(); err != io.EOF {
		if err == nil {
			return fmt.Errorf("accountfeed: feed contains multiple JSON values")
		}
		return fmt.Errorf("accountfeed: invalid trailing JSON: %w", err)
	}
	return nil
}

func consumeJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return fmt.Errorf("object key is not a string")
			}
			if _, exists := seen[key]; exists {
				return fmt.Errorf("duplicate object key %q", key)
			}
			seen[key] = struct{}{}
			if err := consumeJSONValue(decoder); err != nil {
				return err
			}
		}
		_, err = decoder.Token()
		return err
	case '[':
		for decoder.More() {
			if err := consumeJSONValue(decoder); err != nil {
				return err
			}
		}
		_, err = decoder.Token()
		return err
	default:
		return fmt.Errorf("unexpected JSON delimiter %q", delim)
	}
}

// ToFeedItem converts a validated generic item to the normalized FeedItem used
// by ToOperationInput, deriving the operation type from the item type.
func (it GenericItem) ToFeedItem() FeedItem {
	body := it.Content
	var receivedAt *time.Time
	if it.ReceivedAt != nil {
		if parsed, err := time.Parse(time.RFC3339Nano, *it.ReceivedAt); err == nil {
			utc := parsed.UTC()
			receivedAt = &utc
		}
	}
	return FeedItem{
		ExternalID:    it.ExternalID,
		Title:         firstNonEmpty(it.Title, it.Content),
		Body:          body,
		OperationType: operationTypeForItem(it.ItemType),
		ReceivedAt:    receivedAt,
		Metadata:      it.Metadata,
		Provider:      it.Provider,
		AccountLabel:  it.AccountLabel,
		ProjectKey:    it.ProjectKey,
		RawJSON:       it.RawJSON,
	}
}

// operationTypeForItem maps an item type to a default operation type.
func operationTypeForItem(itemType string) string {
	switch ItemType(itemType) {
	case ItemEmail, ItemMessage, ItemChat:
		return "review_message"
	case ItemIssue, ItemPullRequest:
		return "review_code_item"
	case ItemCard:
		return "review_task_card"
	case ItemCalendarEvent:
		return "review_calendar_event"
	case ItemDocument, ItemFile:
		return "review_document"
	default:
		return "review_source_item"
	}
}
