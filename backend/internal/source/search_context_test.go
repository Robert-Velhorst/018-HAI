package source

import (
	"context"
	"errors"
	"testing"
	"time"

	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/semantic"

	"github.com/google/uuid"
)

func TestSearchContextCancelsBlockedSemanticRetrieval(t *testing.T) {
	sourceID := uuid.New()
	repo := newFakeSourceRepo(&models.ConnectedSource{
		ID: sourceID, OwnerIdentity: "alice", Name: "Alice source", Enabled: true, Status: "active",
	})
	started := make(chan struct{})
	semanticService := &fakeSemanticService{search: func(ctx context.Context, _ semantic.SearchRequest) ([]semantic.Match, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	service := NewServiceWithWorkflowPursuitAndSemantic(repo, nil, nil, nil, semanticService)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		_, err := SearchWithContext(service, ctx, SearchRequest{OwnerIdentity: "alice", Query: "cancel me"})
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("semantic retrieval did not start")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("search error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("source search ignored caller cancellation")
	}
}
