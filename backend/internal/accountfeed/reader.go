package accountfeed

import (
	"context"
	"encoding/json"
	"fmt"

	"automation-hub-backend/internal/pathsafety"
)

// Reader obtains the current raw items for a feed.
type Reader interface {
	Feed() Feed
	Read(ctx context.Context) ([]FeedItem, error)
}

// LocalFileReader reads feed items from a JSON file confined to a feeds
// directory (Phase 2A). The file must contain a JSON array of item objects.
type LocalFileReader struct {
	feed    Feed
	rootDir string
}

// NewLocalFileReader builds a reader for feed, confining feed.Path inside
// rootDir. It validates the feed configuration up front.
func NewLocalFileReader(feed Feed, rootDir string) (*LocalFileReader, error) {
	if feed.SourceType != SourceLocalJSONFile {
		return nil, fmt.Errorf("accountfeed: LocalFileReader requires %s, got %q", SourceLocalJSONFile, feed.SourceType)
	}
	if err := feed.Validate(); err != nil {
		return nil, err
	}
	if !pathsafety.IsSafeRelative(feed.Path) {
		return nil, fmt.Errorf("accountfeed: unsafe feed path %q", feed.Path)
	}
	return &LocalFileReader{feed: feed, rootDir: rootDir}, nil
}

// Feed returns the reader's feed configuration.
func (r *LocalFileReader) Feed() Feed { return r.feed }

// Read loads and validates the feed's items, preserving each item's exact raw
// JSON for evidence.
func (r *LocalFileReader) Read(ctx context.Context) ([]FeedItem, error) {
	full, err := pathsafety.SafeJoin(r.rootDir, r.feed.Path)
	if err != nil {
		return nil, fmt.Errorf("accountfeed: %w", err)
	}
	data, err := readBoundedLocalFeedFile(ctx, full)
	if err != nil {
		return nil, fmt.Errorf("accountfeed: read feed file: %w", err)
	}
	var raws []json.RawMessage
	if err := json.Unmarshal(data, &raws); err != nil {
		return nil, fmt.Errorf("accountfeed: feed file must be a JSON array: %w", err)
	}
	items := make([]FeedItem, 0, len(raws))
	for i, raw := range raws {
		var it FeedItem
		if err := json.Unmarshal(raw, &it); err != nil {
			return nil, fmt.Errorf("accountfeed: item %d: %w", i, err)
		}
		it.RawJSON = string(raw)
		if err := it.Validate(); err != nil {
			return nil, fmt.Errorf("accountfeed: item %d: %w", i, err)
		}
		items = append(items, it)
	}
	return items, nil
}
