package memory

import (
	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/semantic"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestCreateDeduplicatesExactMemory(t *testing.T) {
	repo := newFakeRepository()
	service := NewService(repo)

	first, err := service.Create(CreateRequest{ProjectKey: "018-hai", Kind: "preference", Content: "Prefer local models before cloud models.", Tags: []string{"llm"}})
	if err != nil {
		t.Fatalf("Create first memory: %v", err)
	}
	second, err := service.Create(CreateRequest{ProjectKey: "018-hai", Kind: "preference", Content: "Prefer local models before cloud models.", Tags: []string{"routing"}})
	if err != nil {
		t.Fatalf("Create duplicate memory: %v", err)
	}

	if first.ID != second.ID {
		t.Fatalf("duplicate memory created new ID")
	}
	if len(repo.memories) != 1 {
		t.Fatalf("stored %d memories, want 1", len(repo.memories))
	}
}

func TestCreateExactDuplicateAvoidsRedundantWriteAndSemanticReindex(t *testing.T) {
	repo := newFakeRepository()
	semanticSpy := &semanticMemoryStub{}
	service := NewServiceWithSemantic(repo, semanticSpy)
	request := CreateRequest{
		ProjectKey:  "018-hai",
		Kind:        "procedural",
		Content:     "Inspect approval gates before retrying blocked workflows.",
		Summary:     "Inspect approval gates before workflow retries.",
		Tags:        []string{"agent-cycle", "workflow-retry"},
		Confidence:  0.74,
		SourceURI:   "agent-cycle://command-center",
		SourceLabel: "Owner-scoped agent cycle operational learning",
	}

	first, err := service.Create(request)
	if err != nil {
		t.Fatalf("Create first memory: %v", err)
	}
	if repo.updateCalls != 0 || len(semanticSpy.indexedMemoryIDs) != 1 {
		t.Fatalf("first create writes/indexes = %d/%d, want 0/1", repo.updateCalls, len(semanticSpy.indexedMemoryIDs))
	}

	second, err := service.Create(request)
	if err != nil {
		t.Fatalf("Create exact duplicate: %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("exact duplicate created a new memory: first=%s second=%s", first.ID, second.ID)
	}
	if repo.updateCalls != 0 {
		t.Fatalf("exact duplicate caused %d database update(s), want 0", repo.updateCalls)
	}
	if len(semanticSpy.indexedMemoryIDs) != 1 {
		t.Fatalf("exact duplicate caused %d semantic index writes, want only the initial write", len(semanticSpy.indexedMemoryIDs))
	}
}

func TestCreatePreservesDistinctSourceProvenanceAndReplacesSameSourceRevision(t *testing.T) {
	repo := newFakeRepository()
	service := NewService(repo)

	first, err := service.Create(CreateRequest{
		Kind:      "project",
		Content:   "The project deadline is Friday and Robert must review the filing.",
		SourceURI: "mail://message-1",
	})
	if err != nil {
		t.Fatalf("Create first source memory: %v", err)
	}
	second, err := service.Create(CreateRequest{
		Kind:      "project",
		Content:   "The project deadline is Friday and Robert must review the filing.",
		SourceURI: "document://letter-2",
	})
	if err != nil {
		t.Fatalf("Create second source memory: %v", err)
	}
	if first.ID == second.ID || len(repo.memories) != 2 {
		t.Fatalf("distinct source records were merged: first=%s second=%s records=%d", first.ID, second.ID, len(repo.memories))
	}

	revised, err := service.Create(CreateRequest{
		Kind:      "project",
		Content:   "The project deadline is Monday and Robert must review the corrected filing.",
		Summary:   "Corrected filing deadline is Monday.",
		SourceURI: "mail://message-1",
	})
	if err != nil {
		t.Fatalf("Create same-source revision: %v", err)
	}
	if revised.ID != first.ID || len(repo.memories) != 2 {
		t.Fatalf("same-source revision did not update in place: first=%s revised=%s records=%d", first.ID, revised.ID, len(repo.memories))
	}
	if revised.Content != "The project deadline is Monday and Robert must review the corrected filing." {
		t.Fatalf("same-source revision appended stale content: %q", revised.Content)
	}
	if revised.Summary != "Corrected filing deadline is Monday." {
		t.Fatalf("same-source revision summary = %q", revised.Summary)
	}
}

func TestRetrieveRanksRelevantProjectMemory(t *testing.T) {
	repo := newFakeRepository()
	service := NewService(repo)
	_, _ = service.Create(CreateRequest{ProjectKey: "018-hai", Kind: "project", Content: "The project uses Angular dashboard and Go backend.", Confidence: 0.9})
	_, _ = service.Create(CreateRequest{ProjectKey: "other", Kind: "project", Content: "Unrelated cooking notes.", Confidence: 1})

	result, err := service.Retrieve(RetrieveRequest{ProjectKey: "018-hai", Query: "Angular Go backend dashboard", Limit: 3})
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if len(result.UsedContext) != 1 {
		t.Fatalf("retrieved %d memories, want 1", len(result.UsedContext))
	}
	if result.UsedContext[0].Memory.ProjectKey != "018-hai" {
		t.Fatalf("retrieved wrong project memory")
	}
	if result.UsedContext[0].Memory.LastUsedAt == nil {
		t.Fatalf("expected LastUsedAt to be updated")
	}
}

func TestRetrieveThrottlesLastUsedWriteAmplification(t *testing.T) {
	repo := newFakeRepository()
	service := NewService(repo)
	_, err := service.Create(CreateRequest{
		ProjectKey: "018-hai",
		Kind:       "project",
		Content:    "The Angular dashboard uses the Go backend.",
		Confidence: 0.9,
	})
	if err != nil {
		t.Fatalf("Create memory: %v", err)
	}

	request := RetrieveRequest{ProjectKey: "018-hai", Query: "Angular Go backend dashboard", Limit: 3}
	first, err := service.Retrieve(request)
	if err != nil || len(first.UsedContext) != 1 || first.UsedContext[0].Memory.LastUsedAt == nil {
		t.Fatalf("first retrieve = %#v, err=%v", first, err)
	}
	updatesAfterFirstUse := repo.updateCalls
	if updatesAfterFirstUse != 1 {
		t.Fatalf("first retrieve caused %d updates, want 1", updatesAfterFirstUse)
	}

	second, err := service.Retrieve(request)
	if err != nil || len(second.UsedContext) != 1 {
		t.Fatalf("second retrieve = %#v, err=%v", second, err)
	}
	if repo.updateCalls != updatesAfterFirstUse {
		t.Fatalf("repeat retrieve caused %d additional last-used updates", repo.updateCalls-updatesAfterFirstUse)
	}
}

func TestRetrieveIncludesGlobalMemoryForProjectTask(t *testing.T) {
	repo := newFakeRepository()
	service := NewService(repo)
	_, _ = service.Create(CreateRequest{Kind: "preference", Content: "For lawyer follow-ups, use formal Dutch and attach evidence links before drafting.", Confidence: 0.85})
	_, _ = service.Create(CreateRequest{ProjectKey: "other", Kind: "project", Content: "Lawyer notes for an unrelated project should not load.", Confidence: 1})

	result, err := service.Retrieve(RetrieveRequest{ProjectKey: "vivare", Query: "lawyer follow-up formal Dutch evidence", Limit: 3})
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if len(result.UsedContext) != 1 {
		t.Fatalf("retrieved %d memories, want only global memory", len(result.UsedContext))
	}
	if result.UsedContext[0].Memory.ProjectKey != "" {
		t.Fatalf("retrieved project-scoped memory %q, want global memory", result.UsedContext[0].Memory.ProjectKey)
	}
	if result.UsedContext[0].Memory.LastUsedAt == nil {
		t.Fatalf("expected global memory LastUsedAt to be updated")
	}
}

func TestOwnerScopedMemorySeparatesDeduplicationRetrievalAndMutation(t *testing.T) {
	repo := newFakeRepository()
	service := NewService(repo)
	scoped, ok := service.(OwnerScopedService)
	if !ok {
		t.Fatal("native memory service does not implement OwnerScopedService")
	}
	request := CreateRequest{
		ProjectKey: "legal-case",
		Kind:       "preference",
		Content:    "Use formal Dutch when drafting the evidence reply.",
	}
	alice, err := scoped.CreateForOwner("alice", request)
	if err != nil {
		t.Fatalf("CreateForOwner alice: %v", err)
	}
	bob, err := scoped.CreateForOwner("bob", request)
	if err != nil {
		t.Fatalf("CreateForOwner bob: %v", err)
	}
	if alice.ID == bob.ID || alice.OwnerIdentity != "alice" || bob.OwnerIdentity != "bob" {
		t.Fatalf("owner-scoped creates merged or lost owner: alice=%#v bob=%#v", alice, bob)
	}

	aliceResult, err := scoped.RetrieveForOwner("alice", RetrieveRequest{ProjectKey: "legal-case", Query: "formal Dutch evidence", Limit: 10})
	if err != nil {
		t.Fatalf("RetrieveForOwner alice: %v", err)
	}
	if len(aliceResult.UsedContext) != 1 || aliceResult.UsedContext[0].Memory.ID != alice.ID {
		t.Fatalf("alice retrieve = %#v, want only Alice memory", aliceResult.UsedContext)
	}
	if _, err := scoped.UpdateForOwner("alice", bob.ID, UpdateRequest{Summary: "forged change"}); err == nil {
		t.Fatal("alice updated Bob's private memory")
	}
	if _, err := scoped.FindByIDForOwner("alice", bob.ID); err == nil {
		t.Fatal("alice read Bob's private memory by ID")
	}
}

func TestRetrieveUsesOwnerScopedLocalSemanticMemoryMatches(t *testing.T) {
	repo := newFakeRepository()
	semanticSpy := &semanticMemoryStub{}
	service := NewServiceWithSemantic(repo, semanticSpy)
	scoped := service.(OwnerScopedService)

	alice, err := scoped.CreateForOwner("alice", CreateRequest{ProjectKey: "vivare", Kind: "decision", Content: "A formal response is required."})
	if err != nil {
		t.Fatalf("CreateForOwner alice: %v", err)
	}
	bob, err := scoped.CreateForOwner("bob", CreateRequest{ProjectKey: "vivare", Kind: "decision", Content: "An unrelated private decision."})
	if err != nil {
		t.Fatalf("CreateForOwner bob: %v", err)
	}
	semanticSpy.matches = []semantic.MemoryMatch{
		{Memory: *alice, Similarity: 0.92},
		{Memory: *bob, Similarity: 0.99},
	}

	result, err := scoped.RetrieveForOwner("alice", RetrieveRequest{ProjectKey: "vivare", Query: "compose lawyer evidence response", Limit: 5})
	if err != nil {
		t.Fatalf("RetrieveForOwner: %v", err)
	}
	if len(semanticSpy.requests) != 1 || semanticSpy.requests[0].OwnerIdentity != "alice" || semanticSpy.requests[0].ProjectKey != "vivare" {
		t.Fatalf("semantic memory requests = %#v", semanticSpy.requests)
	}
	if len(result.UsedContext) != 1 || result.UsedContext[0].Memory.ID != alice.ID {
		t.Fatalf("semantic owner-scoped results = %#v", result.UsedContext)
	}
	if !strings.Contains(result.UsedContext[0].Explanation, "local semantic similarity") || !strings.Contains(result.Explanation, "pgvector") {
		t.Fatalf("semantic retrieval explanation = %q / %q", result.UsedContext[0].Explanation, result.Explanation)
	}
}

func TestRetrieveFallsBackToKeywordMemoryWhenSemanticSearchFails(t *testing.T) {
	repo := newFakeRepository()
	semanticSpy := &semanticMemoryStub{searchErr: errors.New("local endpoint unavailable")}
	service := NewServiceWithSemantic(repo, semanticSpy)
	_, err := service.Create(CreateRequest{ProjectKey: "018-hai", Kind: "preference", Content: "Use local models before free cloud providers."})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	result, err := service.Retrieve(RetrieveRequest{ProjectKey: "018-hai", Query: "local models", Limit: 3})
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if len(result.UsedContext) != 1 || !strings.Contains(result.Explanation, "keyword ranking was used") {
		t.Fatalf("keyword fallback result = %#v", result)
	}
}

func TestReindexSemanticOnlyIndexesVisibleOwnerMemories(t *testing.T) {
	repo := newFakeRepository()
	semanticSpy := &semanticMemoryStub{}
	service := NewServiceWithSemantic(repo, semanticSpy)
	scoped := service.(OwnerScopedService)
	_, _ = scoped.CreateForOwner("alice", CreateRequest{ProjectKey: "vivare", Kind: "project", Content: "Alice legal case memory"})
	_, _ = scoped.CreateForOwner("bob", CreateRequest{ProjectKey: "vivare", Kind: "project", Content: "Bob private case memory"})
	_, _ = service.Create(CreateRequest{Kind: "preference", Content: "Global local-first preference"})
	semanticSpy.indexedMemoryIDs = nil

	reindexer, ok := service.(SemanticReindexService)
	if !ok {
		t.Fatal("native memory service does not implement SemanticReindexService")
	}
	result, err := reindexer.ReindexSemanticForOwner("alice", 10)
	if err != nil {
		t.Fatalf("ReindexSemanticForOwner: %v", err)
	}
	if !result.Enabled || result.Attempted != 1 || result.Indexed != 1 || result.Failed != 0 || len(semanticSpy.indexedMemoryIDs) != 1 {
		t.Fatalf("semantic reindex result = %#v indexed=%#v", result, semanticSpy.indexedMemoryIDs)
	}

	for _, id := range semanticSpy.indexedMemoryIDs {
		memory, err := repo.FindByID(id)
		if err != nil || memory.OwnerIdentity == "bob" {
			t.Fatalf("reindex crossed owner boundary: id=%s memory=%#v err=%v", id, memory, err)
		}
	}
}

func TestReindexSemanticDoesNothingWhenLocalEmbeddingIsDisabled(t *testing.T) {
	service := NewService(newFakeRepository())
	reindexer := service.(SemanticReindexService)
	result, err := reindexer.ReindexSemanticForOwner("alice", 10)
	if err != nil {
		t.Fatalf("ReindexSemanticForOwner: %v", err)
	}
	if result.Enabled || result.Attempted != 0 || !strings.Contains(result.Explanation, "disabled") {
		t.Fatalf("disabled semantic reindex result = %#v", result)
	}
}

type semanticMemoryStub struct {
	matches          []semantic.MemoryMatch
	requests         []semantic.MemorySearchRequest
	indexedMemoryIDs []uuid.UUID
	searchErr        error
	searchMemory     func(context.Context, semantic.MemorySearchRequest) ([]semantic.MemoryMatch, error)
	indexMemory      func(context.Context, *models.ContextMemory) error
}

var _ semantic.Service = (*semanticMemoryStub)(nil)

func (s *semanticMemoryStub) Enabled() bool                                         { return true }
func (s *semanticMemoryStub) Reason() string                                        { return "test local semantic retrieval" }
func (s *semanticMemoryStub) Index(context.Context, *models.SourceExtraction) error { return nil }
func (s *semanticMemoryStub) Search(context.Context, semantic.SearchRequest) ([]semantic.Match, error) {
	return nil, nil
}
func (s *semanticMemoryStub) IndexMemory(ctx context.Context, memory *models.ContextMemory) error {
	if s.indexMemory != nil {
		return s.indexMemory(ctx, memory)
	}
	if memory != nil {
		s.indexedMemoryIDs = append(s.indexedMemoryIDs, memory.ID)
	}
	return nil
}
func (s *semanticMemoryStub) DeleteMemory(context.Context, uuid.UUID) error { return nil }
func (s *semanticMemoryStub) SearchMemory(ctx context.Context, request semantic.MemorySearchRequest) ([]semantic.MemoryMatch, error) {
	s.requests = append(s.requests, request)
	if s.searchMemory != nil {
		return s.searchMemory(ctx, request)
	}
	if s.searchErr != nil {
		return nil, s.searchErr
	}
	return s.matches, nil
}

func TestRetrieveContextCancelsSemanticMemorySearch(t *testing.T) {
	repo := newFakeRepository()
	started := make(chan struct{})
	semanticSpy := &semanticMemoryStub{searchMemory: func(ctx context.Context, _ semantic.MemorySearchRequest) ([]semantic.MemoryMatch, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	service := NewServiceWithSemantic(repo, semanticSpy)
	_, err := service.Create(CreateRequest{ProjectKey: "018-hai", Kind: "project", Content: "Cancelable local memory search."})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := RetrieveForOwnerContext(service, ctx, "alice", RetrieveRequest{Query: "local memory", ProjectKey: "018-hai"})
		done <- err
	}()
	<-started
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("RetrieveForOwnerContext error = %v, want context.Canceled", err)
	}
}

func TestReindexContextStopsBetweenSemanticWrites(t *testing.T) {
	repo := newFakeRepository()
	semanticSpy := &semanticMemoryStub{}
	service := NewServiceWithSemantic(repo, semanticSpy)
	scoped := service.(OwnerScopedService)
	_, _ = scoped.CreateForOwner("alice", CreateRequest{Kind: "project", Content: "First memory"})
	_, _ = scoped.CreateForOwner("alice", CreateRequest{Kind: "project", Content: "Second memory"})
	started := make(chan struct{})
	semanticSpy.indexMemory = func(ctx context.Context, _ *models.ContextMemory) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := service.(SemanticReindexContextService).ReindexSemanticForOwnerContext(ctx, "alice", 10)
		done <- err
	}()
	<-started
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("ReindexSemanticForOwnerContext error = %v, want context.Canceled", err)
	}
}

func TestOwnerScopedMemoryQuarantinesOwnerlessRecords(t *testing.T) {
	t.Setenv("HAI_LEGACY_DATA_OWNER_IDENTITY", "migration-owner")
	repo := newFakeRepository()
	service := NewService(repo)
	scoped := service.(OwnerScopedService)

	legacy, err := service.Create(CreateRequest{
		ProjectKey: "legal-case",
		Kind:       "preference",
		Content:    "Legacy personal preference without an owner.",
	})
	if err != nil {
		t.Fatalf("create ownerless memory: %v", err)
	}
	owned, err := scoped.CreateForOwner("alice", CreateRequest{
		ProjectKey: "legal-case",
		Kind:       "preference",
		Content:    "Alice's verified personal preference.",
	})
	if err != nil {
		t.Fatalf("create owned memory: %v", err)
	}

	memories, err := scoped.FindAllForOwner("alice", "legal-case", false)
	if err != nil {
		t.Fatalf("FindAllForOwner: %v", err)
	}
	if len(memories) != 1 || memories[0].ID != owned.ID {
		t.Fatalf("owner-scoped list = %#v, want only Alice's memory", memories)
	}
	if _, err := scoped.FindByIDForOwner("alice", legacy.ID); err == nil {
		t.Fatal("Alice read quarantined ownerless memory by ID")
	}
	result, err := scoped.RetrieveForOwner("alice", RetrieveRequest{
		ProjectKey: "legal-case",
		Query:      "personal preference",
		Limit:      10,
	})
	if err != nil {
		t.Fatalf("RetrieveForOwner: %v", err)
	}
	if len(result.UsedContext) != 1 || result.UsedContext[0].Memory.ID != owned.ID {
		t.Fatalf("owner-scoped retrieval = %#v, want only Alice's memory", result.UsedContext)
	}
	if _, err := scoped.UpdateForOwner("alice", legacy.ID, UpdateRequest{Summary: "claimed"}); err == nil {
		t.Fatal("Alice updated quarantined ownerless memory")
	}
	if err := scoped.DeleteForOwner("alice", legacy.ID); err == nil {
		t.Fatal("Alice deleted quarantined ownerless memory")
	}

	systemMemories, err := service.FindAll("legal-case", false)
	if err != nil {
		t.Fatalf("system FindAll: %v", err)
	}
	if len(systemMemories) != 2 {
		t.Fatalf("trusted unscoped list returned %d memories, want 2", len(systemMemories))
	}
}

func TestConfiguredLegacyOwnerCanReadButNotMutateOwnerlessMemory(t *testing.T) {
	t.Setenv("HAI_LEGACY_DATA_OWNER_IDENTITY", "alice")
	repo := newFakeRepository()
	service := NewService(repo)
	scoped := service.(OwnerScopedService)

	legacy, err := service.Create(CreateRequest{ProjectKey: "legal-case", Kind: "preference", Content: "Legacy owner preference."})
	if err != nil {
		t.Fatalf("create ownerless memory: %v", err)
	}
	owned, err := scoped.CreateForOwner("alice", CreateRequest{ProjectKey: "legal-case", Kind: "preference", Content: "Alice preference."})
	if err != nil {
		t.Fatalf("create owned memory: %v", err)
	}

	memories, err := scoped.FindAllForOwner("alice", "legal-case", false)
	if err != nil || len(memories) != 2 {
		t.Fatalf("migration-owner list = %#v, err=%v", memories, err)
	}
	if found, err := scoped.FindByIDForOwner("alice", legacy.ID); err != nil || found.ID != legacy.ID {
		t.Fatalf("migration owner could not read legacy memory: found=%#v err=%v", found, err)
	}
	if _, err := scoped.UpdateForOwner("alice", legacy.ID, UpdateRequest{Summary: "claimed"}); err == nil {
		t.Fatal("migration owner mutated ownerless memory")
	}
	if err := scoped.DeleteForOwner("alice", legacy.ID); err == nil {
		t.Fatal("migration owner deleted ownerless memory")
	}
	if _, err := scoped.FindByIDForOwner("bob", legacy.ID); err == nil {
		t.Fatal("non-migration owner read ownerless memory")
	}
	if found, err := scoped.FindByIDForOwner("alice", owned.ID); err != nil || found.ID != owned.ID {
		t.Fatalf("owned memory became unreadable: found=%#v err=%v", found, err)
	}
}

type fakeRepository struct {
	memories    map[uuid.UUID]models.ContextMemory
	updateCalls int
}

func newFakeRepository() *fakeRepository {
	return &fakeRepository{memories: map[uuid.UUID]models.ContextMemory{}}
}

func (r *fakeRepository) Create(memory *models.ContextMemory) (*models.ContextMemory, error) {
	if memory.ID == uuid.Nil {
		memory.ID = uuid.New()
	}
	now := time.Now().UTC()
	memory.CreatedAt = now
	memory.UpdatedAt = now
	r.memories[memory.ID] = *memory
	return memory, nil
}

func (r *fakeRepository) Update(memory *models.ContextMemory) (*models.ContextMemory, error) {
	r.updateCalls++
	memory.UpdatedAt = time.Now().UTC()
	r.memories[memory.ID] = *memory
	return memory, nil
}

func (r *fakeRepository) FindByID(id uuid.UUID) (*models.ContextMemory, error) {
	memory := r.memories[id]
	return &memory, nil
}

func (r *fakeRepository) FindAll(projectKey string, includeArchived bool) ([]models.ContextMemory, error) {
	memories := []models.ContextMemory{}
	for _, memory := range r.memories {
		if projectKey != "" && memory.ProjectKey != projectKey {
			continue
		}
		if !includeArchived && memory.Archived {
			continue
		}
		memories = append(memories, memory)
	}
	return memories, nil
}

func (r *fakeRepository) FindByHash(projectKey, kind, contentHash string) (*models.ContextMemory, error) {
	for _, memory := range r.memories {
		if memory.ProjectKey == projectKey && memory.Kind == kind && memory.ContentHash == contentHash && !memory.Archived {
			copyMemory := memory
			return &copyMemory, nil
		}
	}
	return nil, gorm.ErrRecordNotFound
}

func (r *fakeRepository) Delete(id uuid.UUID) error {
	delete(r.memories, id)
	return nil
}
