package braincatalog

import (
	"context"
	"errors"
	"net/http"
	"testing"
)

type catalogRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn catalogRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func blockingCatalogClient(started chan<- struct{}) *http.Client {
	return &http.Client{Transport: catalogRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		started <- struct{}{}
		<-request.Context().Done()
		return nil, request.Context().Err()
	})}
}

func TestUpstreamReviewStopsWithCallerContext(t *testing.T) {
	started := make(chan struct{}, 1)
	reviewer := NewUpstreamReviewer(blockingCatalogClient(started)).(UpstreamContextReviewer)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := reviewer.ReviewContext(ctx, Entry{ID: "hai", Name: "HAI", UpstreamURL: "https://github.com/example/hai"})
		done <- err
	}()
	<-started
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("ReviewContext error = %v, want context.Canceled", err)
	}
}

func TestCollectionReviewStopsWithCallerContext(t *testing.T) {
	started := make(chan struct{}, 1)
	reviewer := NewOSSInsightCollectionReviewer(blockingCatalogClient(started)).(OSSInsightCollectionContextReviewer)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := reviewer.ReviewCollectionsContext(ctx)
		done <- err
	}()
	<-started
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("ReviewCollectionsContext error = %v, want context.Canceled", err)
	}
}

func TestRepositoryDiscoveryStopsWithCallerContext(t *testing.T) {
	started := make(chan struct{}, 1)
	scout := NewOSSInsightRepositoryScout(blockingCatalogClient(started)).(OSSInsightRepositoryContextScout)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := scout.DiscoverRepositoriesContext(ctx)
		done <- err
	}()
	<-started
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("DiscoverRepositoriesContext error = %v, want context.Canceled", err)
	}
}
