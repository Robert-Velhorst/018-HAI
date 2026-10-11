package source

import (
	"context"
	"errors"
	"testing"

	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/semantic"

	"github.com/google/uuid"
)

func TestSyncContextPassesCancellationToOptionalSemanticIndexing(t *testing.T) {
	sourceID := uuid.New()
	repo := newFakeSourceRepo(&models.ConnectedSource{
		ID: sourceID, ConnectorKey: "email", Name: "Mailbox", Category: "email", Enabled: true, LocalOnly: true, Status: "active",
	})
	ctx, cancel := context.WithCancel(context.Background())
	semanticService := &cancellingSemanticService{cancel: cancel}
	service := NewServiceWithWorkflowPursuitAndSemantic(repo, nil, nil, nil, semanticService)

	result, err := service.(ContextSyncService).SyncContext(ctx, sourceID, ImportRequest{
		Mode: ModeManualImport,
		Items: []ImportItem{{ExternalID: "message-1", Title: "Message", Content: "Capture this bounded source record."}},
	})
	if err != nil {
		t.Fatalf("SyncContext: %v", err)
	}
	if result == nil || result.Job.Status != "completed" {
		t.Fatalf("sync result = %#v, want completed result despite optional semantic indexing failure", result)
	}
	if !errors.Is(semanticService.observed, context.Canceled) {
		t.Fatalf("semantic index context error = %v, want context.Canceled", semanticService.observed)
	}
}

type cancellingSemanticService struct {
	cancel   context.CancelFunc
	observed error
}

func (s *cancellingSemanticService) Enabled() bool { return true }
func (s *cancellingSemanticService) Reason() string { return "test semantic service" }
func (s *cancellingSemanticService) Index(ctx context.Context, _ *models.SourceExtraction) error {
	s.cancel()
	s.observed = ctx.Err()
	return s.observed
}
func (s *cancellingSemanticService) Search(context.Context, semantic.SearchRequest) ([]semantic.Match, error) {
	return nil, nil
}
func (s *cancellingSemanticService) IndexMemory(context.Context, *models.ContextMemory) error { return nil }
func (s *cancellingSemanticService) DeleteMemory(context.Context, uuid.UUID) error { return nil }
func (s *cancellingSemanticService) SearchMemory(context.Context, semantic.MemorySearchRequest) ([]semantic.MemoryMatch, error) {
	return nil, nil
}
