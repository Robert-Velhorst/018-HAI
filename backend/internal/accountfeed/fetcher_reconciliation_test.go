package accountfeed

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

type feedBoundaryReader func([]byte) (int, error)

func (read feedBoundaryReader) Read(buffer []byte) (int, error) { return read(buffer) }

func TestReconciledFeedReadHonorsCancellationAtEOF(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reader := feedBoundaryReader(func(buffer []byte) (int, error) {
		count := copy(buffer, "[]")
		cancel()
		return count, io.EOF
	})
	if body, err := readBoundedFeedBytes(ctx, reader); !errors.Is(err, context.Canceled) || body != nil {
		t.Fatalf("canceled EOF returned successful data: body=%q err=%v", body, err)
	}
}

func TestReconciledFeedReadRejectsMissingOrCancelledContextBeforeReading(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, candidate := range []context.Context{nil, ctx} {
		reads := 0
		reader := feedBoundaryReader(func([]byte) (int, error) { reads++; return 0, io.EOF })
		if body, err := readBoundedFeedBytes(candidate, reader); err == nil || body != nil || reads != 0 {
			t.Fatalf("invalid context consumed data: body=%q err=%v reads=%d", body, err, reads)
		}
	}
}

func TestReconciledFeedReadRetainsExactSizeBoundary(t *testing.T) {
	for _, size := range []int{maxFeedBytes - 1, maxFeedBytes, maxFeedBytes + 1} {
		body, err := readBoundedFeedBytes(context.Background(), strings.NewReader(strings.Repeat("x", size)))
		if size <= maxFeedBytes {
			if err != nil || len(body) != size {
				t.Fatalf("allowed size %d rejected: bytes=%d err=%v", size, len(body), err)
			}
		} else if err == nil || body != nil {
			t.Fatal("oversized body was accepted")
		}
	}
}

func TestFeedFetcherRejectsMissingContextWithoutPanicking(t *testing.T) {
	defer func() {
		if failure := recover(); failure != nil {
			t.Errorf("missing fetch context caused a panic: %T", failure)
		}
	}()
	if body, err := fetchFeedBytes(nil, Feed{SourceType: SourceHTTPJSONFeed}, FetchOptions{}); err == nil || body != nil {
		t.Fatal("missing fetch context must return an error without data")
	}
}

func TestDisabledHTTPFeedErrorDoesNotRevealItsPrivateURL(t *testing.T) {
	feed := Feed{SourceType: SourceHTTPJSONFeed, URL: "https://operator@example.invalid/private-feed-marker"}
	body, err := fetchFeedBytes(context.Background(), feed, FetchOptions{})
	if err == nil || body != nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("expected disabled feed rejection, got body=%q err=%v", body, err)
	}
	if strings.Contains(err.Error(), feed.URL) || strings.Contains(err.Error(), "operator") || strings.Contains(err.Error(), "private-feed-marker") {
		t.Fatal("disabled feed error exposed private configuration")
	}
}
