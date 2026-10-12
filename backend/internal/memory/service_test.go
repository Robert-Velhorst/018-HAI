package memory

import (
	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/semantic"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

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

func TestGenericMemoryWritesCannotMintOrRewriteReviewManagedLessons(t *testing.T) {
	repo := newFakeRepository()
	service := newVerifiedFactTestService(repo)

	for _, kind := range []string{"source", "source_supported_fact", "correction_lesson"} {
		if _, err := service.Create(CreateRequest{Kind: kind, Content: "Unverified model claim."}); err == nil {
			t.Fatalf("generic unscoped Create accepted review-managed kind %q", kind)
		}
	}
	if len(repo.memories) != 0 {
		t.Fatalf("rejected generic writes left %d memories, want none", len(repo.memories))
	}

	// Generic owner-scoped writes cannot label arbitrary content as verified.
	scoped := service.(OwnerScopedService)
	for _, kind := range []string{"source", "source_supported_fact", "correction_lesson"} {
		if _, err := scoped.CreateForOwner("alice", CreateRequest{Kind: kind, Content: "Caller-minted review-managed memory"}); err == nil {
			t.Fatalf("generic owner-scoped CreateForOwner accepted review-managed kind %q", kind)
		}
	}

	// This exercises the internal writer's structural checks only. Referential
	// owner/run/claim/evidence validation is covered at the verification-service boundary.
	proof := VerifiedFactProvenance{RunID: uuid.New(), ClaimID: uuid.New(), EvidenceID: uuid.New(), SourceURI: "https://records.example/review/12"}
	fact, err := CreateVerifiedFactForOwner(service, "alice", CreateRequest{
		Kind: "source_supported_fact", Content: "The review is scheduled for Friday.",
		SourceURI: proof.SourceURI, SourceLabel: "verification-run:" + proof.RunID.String(), Confidence: 0.91,
	}, proof)
	if err != nil {
		t.Fatalf("owner-scoped verified write: %v", err)
	}
	if _, err := scoped.UpdateForOwner("alice", fact.ID, UpdateRequest{Content: "An unverified replacement claim."}); err == nil {
		t.Fatal("generic update changed a review-managed fact")
	}
	if _, err := scoped.UpdateForOwner("alice", fact.ID, UpdateRequest{SourceURI: "https://unrelated.example"}); err == nil {
		t.Fatal("generic update changed review-managed evidence provenance")
	}
	for _, invalid := range []VerifiedFactProvenance{
		{},
		{RunID: proof.RunID, ClaimID: proof.ClaimID, EvidenceID: proof.EvidenceID, SourceURI: "https://records.example/other"},
	} {
		request := CreateRequest{Kind: "source_supported_fact", Content: "Another claim", SourceURI: proof.SourceURI, SourceLabel: "verification-run:" + proof.RunID.String(), Confidence: 0.9}
		if _, err := CreateVerifiedFactForOwner(service, "alice", request, invalid); err == nil {
			t.Fatalf("verified-fact writer accepted invalid provenance %#v", invalid)
		}
	}
	stored, err := scoped.FindByIDForOwner("alice", fact.ID)
	if err != nil || stored.Content != fact.Content || stored.SourceURI != fact.SourceURI {
		t.Fatalf("rejected update changed the stored fact: stored=%#v err=%v", stored, err)
	}

	note, err := scoped.CreateForOwner("alice", CreateRequest{Kind: "note", Content: "ordinary note"})
	if err != nil {
		t.Fatalf("create ordinary note: %v", err)
	}
	if _, err := scoped.UpdateForOwner("alice", note.ID, UpdateRequest{Kind: "correction_lesson"}); err == nil {
		t.Fatal("generic update relabeled an ordinary note as a correction lesson")
	}
}

func TestVerifiedFactsDeduplicateOnlyWithinSameSourceProvenance(t *testing.T) {
	repo := newFakeRepository()
	service := newVerifiedFactTestService(repo)
	content := "The review is scheduled for Friday."
	createFact := func(proof VerifiedFactProvenance, factContent string) *models.ContextMemory {
		t.Helper()
		created, err := CreateVerifiedFactForOwner(service, "alice", CreateRequest{
			Kind: "source_supported_fact", Content: factContent, SourceURI: proof.SourceURI,
			SourceLabel: "verification-run:" + proof.RunID.String(), Confidence: 0.9,
		}, proof)
		if err != nil {
			t.Fatalf("create verified fact for run %s: %v", proof.RunID, err)
		}
		return created
	}

	firstProof := VerifiedFactProvenance{RunID: uuid.New(), ClaimID: uuid.New(), EvidenceID: uuid.New(), SourceURI: "https://records.example/first"}
	secondProof := VerifiedFactProvenance{RunID: uuid.New(), ClaimID: uuid.New(), EvidenceID: uuid.New(), SourceURI: "https://records.example/second"}
	nearDuplicateProof := VerifiedFactProvenance{RunID: uuid.New(), ClaimID: uuid.New(), EvidenceID: uuid.New(), SourceURI: "https://records.example/third"}
	first := createFact(firstProof, content)
	second := createFact(secondProof, content)
	nearDuplicate := createFact(nearDuplicateProof, content+" now")
	retry := createFact(firstProof, content)

	if first.ID == second.ID || first.ID == nearDuplicate.ID || second.ID == nearDuplicate.ID {
		t.Fatalf("facts with distinct provenance or content were merged: first=%s second=%s near=%s", first.ID, second.ID, nearDuplicate.ID)
	}
	if retry.ID != first.ID {
		t.Fatalf("retry for identical run/source/content got ID %s, want idempotent existing ID %s", retry.ID, first.ID)
	}
	if len(repo.memories) != 3 {
		t.Fatalf("stored facts = %d, want 3 independently traceable facts", len(repo.memories))
	}

	for _, want := range []*models.ContextMemory{
		{ID: first.ID, SourceURI: firstProof.SourceURI, SourceLabel: "verification-run:" + firstProof.RunID.String(), Content: content},
		{ID: second.ID, SourceURI: "https://records.example/second", Content: content},
		{ID: nearDuplicate.ID, SourceURI: "https://records.example/third", Content: content + " now"},
	} {
		stored, err := service.FindByID(want.ID)
		if err != nil {
			t.Fatalf("read fact %s: %v", want.ID, err)
		}
		if stored.OwnerIdentity != "alice" || stored.SourceURI != want.SourceURI || stored.Content != want.Content {
			t.Fatalf("fact %s lost owner/source/content provenance: got %#v want %#v", want.ID, stored, want)
		}
		if want.SourceLabel != "" && stored.SourceLabel != want.SourceLabel {
			t.Fatalf("fact %s run provenance = %q, want %q", want.ID, stored.SourceLabel, want.SourceLabel)
		}
	}
}

func TestVerifiedFactPromotionFailsClosedWithoutDurableValidation(t *testing.T) {
	repo := newFakeRepository()
	service := NewService(repo)
	proof := VerifiedFactProvenance{
		RunID: uuid.New(), ClaimID: uuid.New(), EvidenceID: uuid.New(), SourceURI: "https://records.example/review/12",
	}
	_, err := CreateVerifiedFactForOwner(service, "alice", CreateRequest{
		Kind: "source_supported_fact", Content: "The review is scheduled for Friday.",
		SourceURI: proof.SourceURI, SourceLabel: "verification-run:" + proof.RunID.String(), Confidence: 0.91,
	}, proof)
	if err == nil || !strings.Contains(err.Error(), "durable verified-fact provenance validation is unavailable") {
		t.Fatalf("promotion without durable evidence validation error = %v", err)
	}
	if len(repo.memories) != 0 {
		t.Fatalf("unvalidated promotion persisted %d memory rows", len(repo.memories))
	}
}

func TestOwnerScopedCreateUsesAuthenticatedOwnerInsteadOfRequestOwner(t *testing.T) {
	service := NewService(newFakeRepository()).(OwnerScopedService)
	created, err := service.CreateForOwner("alice", CreateRequest{
		OwnerIdentity: "bob", Kind: "note", Content: "Owner comes from the authenticated call boundary.",
	})
	if err != nil {
		t.Fatalf("owner-scoped create: %v", err)
	}
	if created.OwnerIdentity != "alice" {
		t.Fatalf("stored owner = %q, want authenticated owner alice", created.OwnerIdentity)
	}
}

func TestFallbackRepositoryQuarantinesWhitespaceAlteredOwner(t *testing.T) {
	repo := newFakeRepository()
	id := uuid.New()
	repo.memories[id] = models.ContextMemory{ID: id, OwnerIdentity: " alice ", Kind: "note", Content: "private legacy record"}
	service := NewService(repo).(OwnerScopedService)

	memories, err := service.FindAllForOwner("alice", "", true)
	if err != nil || len(memories) != 0 {
		t.Fatalf("owner-scoped list exposed padded owner: memories=%#v err=%v", memories, err)
	}
	if _, err := service.FindByIDForOwner("alice", id); err == nil {
		t.Fatal("owner-scoped ID lookup exposed a whitespace-altered owner")
	}
	if _, err := service.UpdateForOwner("alice", id, UpdateRequest{Summary: "claimed"}); err == nil {
		t.Fatal("owner-scoped update accepted a whitespace-altered owner")
	}
	if err := service.DeleteForOwner("alice", id); err == nil {
		t.Fatal("owner-scoped delete accepted a whitespace-altered owner")
	}
	if repo.memories[id].Content != "private legacy record" {
		t.Fatal("rejected owner operations changed the legacy record")
	}
}

func TestMemoryWritesEnforceStorageLimitsBeforePersistence(t *testing.T) {
	tests := []struct {
		name    string
		request CreateRequest
		owner   string
	}{
		{name: "content bytes", request: CreateRequest{Kind: "note", Content: strings.Repeat("x", maxMemoryContentBytes+1)}, owner: "alice"},
		{name: "kind bytes", request: CreateRequest{Kind: strings.Repeat("k", 51), Content: "note"}, owner: "alice"},
		{name: "project bytes", request: CreateRequest{ProjectKey: strings.Repeat("p", 256), Kind: "note", Content: "note"}, owner: "alice"},
		{name: "summary input bytes", request: CreateRequest{Kind: "note", Content: "note", Summary: strings.Repeat("s", maxMemorySummaryInputBytes+1)}, owner: "alice"},
		{name: "too many tags", request: CreateRequest{Kind: "note", Content: "note", Tags: make([]string, maxMemoryTags+1)}, owner: "alice"},
		{name: "tag storage bytes", request: CreateRequest{Kind: "note", Content: "note", Tags: []string{strings.Repeat("t", 513)}}, owner: "alice"},
		{name: "source URI bytes", request: CreateRequest{Kind: "note", Content: "note", SourceURI: strings.Repeat("u", 1025)}, owner: "alice"},
		{name: "source label bytes", request: CreateRequest{Kind: "note", Content: "note", SourceLabel: strings.Repeat("l", 256)}, owner: "alice"},
		{name: "owner bytes", request: CreateRequest{Kind: "note", Content: "note"}, owner: strings.Repeat("o", 256)},
		{name: "invalid UTF-8", request: CreateRequest{Kind: "note", Content: string([]byte{0xff})}, owner: "alice"},
		{name: "NUL text", request: CreateRequest{Kind: "note", Content: "invalid\x00text"}, owner: "alice"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repo := newFakeRepository()
			service := NewService(repo).(OwnerScopedService)
			if _, err := service.CreateForOwner(test.owner, test.request); err == nil {
				t.Fatal("oversized or invalid memory request was accepted")
			}
			if len(repo.memories) != 0 {
				t.Fatalf("invalid request persisted %d memory records", len(repo.memories))
			}
		})
	}

	repo := newFakeRepository()
	service := NewService(repo).(OwnerScopedService)
	created, err := service.CreateForOwner("alice", CreateRequest{Kind: "note", Content: strings.Repeat("x", maxMemoryContentBytes)})
	if err != nil {
		t.Fatalf("content at configured byte limit was rejected: %v", err)
	}
	if created == nil || len(created.Content) != maxMemoryContentBytes {
		t.Fatalf("content at configured byte limit was changed: created=%#v", created)
	}
	if _, err := service.UpdateForOwner("alice", created.ID, UpdateRequest{Content: strings.Repeat("x", maxMemoryContentBytes+1)}); err == nil {
		t.Fatal("oversized update was accepted")
	}
	stored, err := service.FindByIDForOwner("alice", created.ID)
	if err != nil || len(stored.Content) != maxMemoryContentBytes {
		t.Fatalf("rejected update changed persisted content: length=%d err=%v", len(stored.Content), err)
	}
}

func TestExactMemoryDeduplicationChecksCanonicalContentAfterHashMatch(t *testing.T) {
	repo := newFakeRepository()
	service := NewService(repo).(OwnerScopedService)
	first, err := service.CreateForOwner("alice", CreateRequest{ProjectKey: "case-a", Kind: "note", Content: "Unrelated cooking records and meal planning."})
	if err != nil {
		t.Fatalf("seed memory: %v", err)
	}
	secondContent := "Hearing preparation requires the original dated correspondence."
	first.ContentHash = hashContent("case-a", "note", secondContent)
	repo.memories[first.ID] = *first
	second, err := service.CreateForOwner("alice", CreateRequest{ProjectKey: "case-a", Kind: "note", Content: secondContent})
	if err != nil {
		t.Fatalf("create colliding-hash memory: %v", err)
	}
	if first.ID == second.ID || len(repo.memories) != 2 {
		t.Fatalf("hash collision merged distinct memory content: first=%s second=%s count=%d", first.ID, second.ID, len(repo.memories))
	}
}

func TestConcurrentMemoryCreatesDeduplicateWithinOwnerProjectKind(t *testing.T) {
	repo := newFakeRepository()
	service := NewService(repo).(OwnerScopedService)
	const writers = 32
	start := make(chan struct{})
	results := make(chan uuid.UUID, writers)
	errorsFound := make(chan error, writers)
	var group sync.WaitGroup
	for index := 0; index < writers; index++ {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			created, err := service.CreateForOwner("alice", CreateRequest{ProjectKey: "vivare", Kind: "decision", Content: "Collect dated evidence before drafting the reply."})
			if err != nil {
				errorsFound <- err
				return
			}
			results <- created.ID
		}()
	}
	close(start)
	group.Wait()
	close(results)
	close(errorsFound)
	for err := range errorsFound {
		t.Fatalf("concurrent create failed: %v", err)
	}
	var canonicalID uuid.UUID
	for id := range results {
		if canonicalID == uuid.Nil {
			canonicalID = id
			continue
		}
		if id != canonicalID {
			t.Fatalf("concurrent duplicate received a second ID: %s != %s", id, canonicalID)
		}
	}
	if len(repo.memories) != 1 {
		t.Fatalf("concurrent duplicate create persisted %d rows, want 1", len(repo.memories))
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

func TestMemoryTextCompactionPreservesUTF8AtTruncationBoundary(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		compact  func(string) string
		maxBytes int
	}{
		{
			name:     "summary",
			input:    strings.Repeat("a", 356) + "€" + strings.Repeat("b", 8),
			compact:  compactSummary,
			maxBytes: 360,
		},
		{
			name:     "merged content",
			input:    strings.Repeat("a", 1796) + "€" + strings.Repeat("b", 8),
			compact:  compactLongText,
			maxBytes: 1800,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.compact(test.input)
			if !utf8.ValidString(got) {
				t.Fatalf("compacted text is invalid UTF-8: %q", got)
			}
			if len(got) > test.maxBytes {
				t.Fatalf("compacted text has %d bytes, want at most %d", len(got), test.maxBytes)
			}
		})
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

func TestOwnerScopedOperationsRejectMissingOwner(t *testing.T) {
	repo := newFakeRepository()
	service := NewService(repo)
	scoped := service.(OwnerScopedService)
	alice, err := scoped.CreateForOwner("alice", CreateRequest{
		ProjectKey: "legal-case", Kind: "note", Content: "Alice private memory.",
	})
	if err != nil {
		t.Fatalf("CreateForOwner alice: %v", err)
	}
	if _, err := scoped.CreateForOwner("bob", CreateRequest{
		ProjectKey: "legal-case", Kind: "note", Content: "Bob private memory.",
	}); err != nil {
		t.Fatalf("CreateForOwner bob: %v", err)
	}

	if _, err := scoped.CreateForOwner(" ", CreateRequest{Content: "Must not become global."}); err == nil {
		t.Fatal("CreateForOwner accepted an empty owner")
	}
	if _, err := scoped.FindAllForOwner("", "", true); err == nil {
		t.Fatal("FindAllForOwner returned an unscoped memory list")
	}
	if _, err := service.(RecentMemoryService).RecentForOwner("\t ", "", true, 10); err == nil {
		t.Fatal("RecentForOwner accepted a whitespace-only owner")
	}
	if _, err := scoped.FindByIDForOwner("", alice.ID); err == nil {
		t.Fatal("FindByIDForOwner accepted an empty owner")
	}
	if _, err := scoped.RetrieveForOwner("", RetrieveRequest{Query: "private memory"}); err == nil {
		t.Fatal("RetrieveForOwner returned unscoped context")
	}
	if _, err := scoped.UpdateForOwner("", alice.ID, UpdateRequest{Content: "Forged update."}); err == nil {
		t.Fatal("UpdateForOwner accepted an empty owner")
	}
	if _, err := scoped.ArchiveForOwner("", alice.ID, true); err == nil {
		t.Fatal("ArchiveForOwner accepted an empty owner")
	}
	if err := scoped.DeleteForOwner("", alice.ID); err == nil {
		t.Fatal("DeleteForOwner accepted an empty owner")
	}
	if _, err := service.(MemoryHealthService).MemoryHealthForOwner("", ""); err == nil {
		t.Fatal("MemoryHealthForOwner returned an unscoped report")
	}
	if _, err := service.(SemanticReindexService).ReindexSemanticForOwner("", 10); err == nil {
		t.Fatal("ReindexSemanticForOwner accepted an empty owner")
	}

	if _, err := HealthForOwner(service, "", ""); err == nil {
		t.Fatal("HealthForOwner helper returned an unscoped report")
	}
	if _, err := RecentForOwner(service, "", "", true, 10); err == nil {
		t.Fatal("RecentForOwner helper returned an unscoped list")
	}

	storedAlice, err := service.FindByID(alice.ID)
	if err != nil {
		t.Fatalf("verify Alice record remains: %v", err)
	}
	if storedAlice.OwnerIdentity != "alice" || storedAlice.Content != "Alice private memory." || storedAlice.Archived {
		t.Fatalf("rejected ownerless mutation changed Alice's record: %#v", storedAlice)
	}
	if len(repo.memories) != 2 {
		t.Fatalf("records after rejected ownerless operations = %d, want 2", len(repo.memories))
	}
}

func TestTrustedInternalOwnerHelpersRetainUnscopedCompatibility(t *testing.T) {
	service := NewService(newFakeRepository())
	created, err := CreateForOwner(service, " ", CreateRequest{Content: "Trusted system workflow context"})
	if err != nil {
		t.Fatalf("CreateForOwner internal fallback: %v", err)
	}
	if created.OwnerIdentity != "" {
		t.Fatalf("internal fallback owner = %q, want unscoped system memory", created.OwnerIdentity)
	}
	result, err := RetrieveForOwner(service, "", RetrieveRequest{Query: "trusted system workflow context"})
	if err != nil {
		t.Fatalf("RetrieveForOwner internal fallback: %v", err)
	}
	for _, item := range result.UsedContext {
		if item.Memory.ID == created.ID {
			return
		}
	}
	t.Fatalf("trusted unscoped retrieval omitted the created memory: %#v", result.UsedContext)
}

func TestDeleteFailsClosedWhenSemanticIndexDeletionFails(t *testing.T) {
	for _, ownerScoped := range []bool{false, true} {
		name := "unscoped"
		if ownerScoped {
			name = "owner-scoped"
		}
		t.Run(name, func(t *testing.T) {
			repo := newFakeRepository()
			semanticIndex := &semanticMemoryStub{deleteErr: errors.New("index unavailable")}
			service := NewServiceWithSemantic(repo, semanticIndex)
			var memory *models.ContextMemory
			var err error
			if ownerScoped {
				memory, err = service.(OwnerScopedService).CreateForOwner("alice", CreateRequest{Content: "Private record"})
			} else {
				memory, err = service.Create(CreateRequest{Content: "Internal record"})
			}
			if err != nil {
				t.Fatalf("create memory: %v", err)
			}

			if ownerScoped {
				err = service.(OwnerScopedService).DeleteForOwner("alice", memory.ID)
			} else {
				err = service.Delete(memory.ID)
			}
			if err == nil || !strings.Contains(err.Error(), "SQL memory was preserved") {
				t.Fatalf("delete error = %v, want fail-closed index error", err)
			}
			stored, findErr := repo.FindByID(memory.ID)
			if findErr != nil || stored.ID != memory.ID || len(repo.memories) != 1 {
				t.Fatalf("SQL memory was not preserved after index failure: stored=%#v err=%v", stored, findErr)
			}
			if len(semanticIndex.deletedMemoryIDs) != 1 || semanticIndex.deletedMemoryIDs[0] != memory.ID {
				t.Fatalf("semantic deletion calls = %#v", semanticIndex.deletedMemoryIDs)
			}

			semanticIndex.deleteErr = nil
			if ownerScoped {
				err = service.(OwnerScopedService).DeleteForOwner("alice", memory.ID)
			} else {
				err = service.Delete(memory.ID)
			}
			if err != nil || len(repo.memories) != 0 {
				t.Fatalf("retry delete: err=%v remaining=%d", err, len(repo.memories))
			}
		})
	}
}

func TestDisabledSemanticServiceCleansIndexDuringArchiveAndDelete(t *testing.T) {
	repo := newFakeRepository()
	semanticIndex := &semanticMemoryStub{}
	service := NewServiceWithSemantic(repo, semanticIndex)
	ownerScoped := service.(OwnerScopedService)
	memory, err := ownerScoped.CreateForOwner("alice", CreateRequest{Content: "May have a retained vector"})
	if err != nil {
		t.Fatalf("create memory: %v", err)
	}
	if len(semanticIndex.indexedMemoryIDs) != 1 || semanticIndex.indexedMemoryIDs[0] != memory.ID {
		t.Fatalf("setup did not index the memory: %#v", semanticIndex.indexedMemoryIDs)
	}

	semanticIndex.disabled = true
	archived, err := ownerScoped.ArchiveForOwner("alice", memory.ID, true)
	if err != nil || archived == nil || !archived.Archived {
		t.Fatalf("archive with retrieval disabled: memory=%#v err=%v", archived, err)
	}
	result, err := ownerScoped.RetrieveForOwner("alice", RetrieveRequest{Query: "retained vector"})
	if err != nil {
		t.Fatalf("retrieve while semantic service is disabled: %v", err)
	}
	if len(result.UsedContext) != 0 {
		t.Fatalf("archived memory remained retrievable while semantic service was disabled: %#v", result.UsedContext)
	}
	if len(semanticIndex.deletedMemoryIDs) != 1 || semanticIndex.deletedMemoryIDs[0] != memory.ID {
		t.Fatalf("disabled adapter was not asked to clean the archived vector: %#v", semanticIndex.deletedMemoryIDs)
	}

	if err := ownerScoped.DeleteForOwner("alice", memory.ID); err != nil {
		t.Fatalf("delete with retrieval disabled: %v", err)
	}
	if len(repo.memories) != 0 {
		t.Fatalf("SQL memories remaining after delete = %d, want 0", len(repo.memories))
	}
	if len(semanticIndex.deletedMemoryIDs) != 2 || semanticIndex.deletedMemoryIDs[1] != memory.ID {
		t.Fatalf("disabled adapter was not asked to clean the deleted vector: %#v", semanticIndex.deletedMemoryIDs)
	}
	result, err = ownerScoped.RetrieveForOwner("alice", RetrieveRequest{Query: "retained vector"})
	if err != nil || len(result.UsedContext) != 0 {
		t.Fatalf("deleted memory remained retrievable: result=%#v err=%v", result, err)
	}
}

func TestDisabledSemanticCleanupFailurePreservesMemoryForRetry(t *testing.T) {
	for _, operation := range []string{"archive", "delete"} {
		t.Run(operation, func(t *testing.T) {
			repo := newFakeRepository()
			semanticIndex := &semanticMemoryStub{}
			service := NewServiceWithSemantic(repo, semanticIndex).(OwnerScopedService)
			memory, err := service.CreateForOwner("alice", CreateRequest{Content: "Sensitive memory"})
			if err != nil {
				t.Fatalf("create memory: %v", err)
			}
			semanticIndex.disabled = true
			semanticIndex.deleteErr = errors.New("cleanup database unavailable")

			if operation == "archive" {
				if _, err := service.ArchiveForOwner("alice", memory.ID, true); err == nil {
					t.Fatal("archive succeeded without confirming disabled-index cleanup")
				}
			} else if err := service.DeleteForOwner("alice", memory.ID); err == nil {
				t.Fatal("delete succeeded without confirming disabled-index cleanup")
			}
			stored, err := repo.FindByID(memory.ID)
			if err != nil || stored.Archived || len(repo.memories) != 1 {
				t.Fatalf("SQL memory changed after cleanup failure: stored=%#v err=%v", stored, err)
			}

			semanticIndex.deleteErr = nil
			if operation == "archive" {
				archived, err := service.ArchiveForOwner("alice", memory.ID, true)
				if err != nil || archived == nil || !archived.Archived {
					t.Fatalf("retry archive: memory=%#v err=%v", archived, err)
				}
			} else if err := service.DeleteForOwner("alice", memory.ID); err != nil || len(repo.memories) != 0 {
				t.Fatalf("retry delete: err=%v remaining=%d", err, len(repo.memories))
			}
		})
	}
}

func TestUpdateRemovesStaleVectorBeforeSavingAndReportsReindexFailure(t *testing.T) {
	repo := newFakeRepository()
	semanticIndex := &semanticMemoryStub{}
	service := NewServiceWithSemantic(repo, semanticIndex).(OwnerScopedService)
	created, err := service.CreateForOwner("alice", CreateRequest{Kind: "note", Content: "Original memory content."})
	if err != nil {
		t.Fatalf("create memory: %v", err)
	}
	if len(semanticIndex.indexedMemoryIDs) != 1 {
		t.Fatalf("initial semantic indexing calls = %#v, want one", semanticIndex.indexedMemoryIDs)
	}

	semanticIndex.deleteErr = errors.New("index cleanup unavailable")
	if _, err := service.UpdateForOwner("alice", created.ID, UpdateRequest{Content: "Corrected memory content."}); err == nil || !strings.Contains(err.Error(), "semantic index deletion failed") {
		t.Fatalf("update did not fail closed when stale vector cleanup failed: %v", err)
	}
	stored, err := service.FindByIDForOwner("alice", created.ID)
	if err != nil || stored.Content != "Original memory content." {
		t.Fatalf("SQL memory changed despite failed vector cleanup: stored=%#v err=%v", stored, err)
	}

	semanticIndex.deleteErr = nil
	semanticIndex.indexErr = errors.New("embedding unavailable")
	updated, err := service.UpdateForOwner("alice", created.ID, UpdateRequest{Content: "Corrected memory content."})
	if err == nil || !strings.Contains(err.Error(), "keyword retrieval remains available") || updated == nil || updated.Content != "Corrected memory content." {
		t.Fatalf("index refresh failure was not surfaced with persisted SQL state: updated=%#v err=%v", updated, err)
	}
	if len(semanticIndex.deletedMemoryIDs) != 2 || semanticIndex.deletedMemoryIDs[1] != created.ID {
		t.Fatalf("update did not remove the prior vector before changing content: %#v", semanticIndex.deletedMemoryIDs)
	}
	stored, err = service.FindByIDForOwner("alice", created.ID)
	if err != nil || stored.Content != "Corrected memory content." {
		t.Fatalf("SQL content after index failure = %#v err=%v", stored, err)
	}

	semanticIndex.indexErr = nil
	semanticReindex, ok := service.(SemanticReindexService)
	if !ok {
		t.Fatal("memory service does not expose owner-scoped semantic reindexing")
	}
	result, err := semanticReindex.ReindexSemanticForOwner("alice", 10)
	if err != nil || result.Attempted != 1 || result.Indexed != 1 || result.Failed != 0 {
		t.Fatalf("semantic reindex did not repair the missing vector: result=%#v err=%v", result, err)
	}
}

func TestOwnerScopedDeleteAndArchiveCheckOwnerBeforeTouchingSemanticIndex(t *testing.T) {
	repo := newFakeRepository()
	semanticIndex := &semanticMemoryStub{}
	service := NewServiceWithSemantic(repo, semanticIndex).(OwnerScopedService)
	memory, err := service.CreateForOwner("alice", CreateRequest{Content: "Alice only"})
	if err != nil {
		t.Fatalf("create memory: %v", err)
	}

	if err := service.DeleteForOwner("bob", memory.ID); err == nil {
		t.Fatal("owner-scoped delete accepted another owner's memory")
	}
	if _, err := service.ArchiveForOwner("bob", memory.ID, true); err == nil {
		t.Fatal("owner-scoped archive accepted another owner's memory")
	}
	if len(semanticIndex.deletedMemoryIDs) != 0 {
		t.Fatalf("unauthorized operations touched semantic index: %#v", semanticIndex.deletedMemoryIDs)
	}
	stored, err := repo.FindByID(memory.ID)
	if err != nil || stored.Archived || stored.OwnerIdentity != "alice" {
		t.Fatalf("unauthorized operation changed SQL record: %#v err=%v", stored, err)
	}
}

func TestArchiveFailsClosedWhenSemanticIndexDeletionFails(t *testing.T) {
	for _, route := range []string{"unscoped archive", "owner-scoped archive", "owner-scoped update"} {
		t.Run(route, func(t *testing.T) {
			repo := newFakeRepository()
			semanticIndex := &semanticMemoryStub{deleteErr: errors.New("index unavailable")}
			service := NewServiceWithSemantic(repo, semanticIndex)
			memory, err := service.(OwnerScopedService).CreateForOwner("alice", CreateRequest{Content: "Archive candidate"})
			if err != nil {
				t.Fatalf("create memory: %v", err)
			}

			switch route {
			case "unscoped archive":
				_, err = service.Archive(memory.ID, true)
			case "owner-scoped archive":
				_, err = service.(OwnerScopedService).ArchiveForOwner("alice", memory.ID, true)
			default:
				archived := true
				_, err = service.(OwnerScopedService).UpdateForOwner("alice", memory.ID, UpdateRequest{Archived: &archived})
			}
			if err == nil || !strings.Contains(err.Error(), "semantic index deletion failed") {
				t.Fatalf("archive error = %v, want fail-closed index error", err)
			}
			stored, findErr := repo.FindByID(memory.ID)
			if findErr != nil || stored.Archived || len(repo.memories) != 1 {
				t.Fatalf("SQL memory changed despite index failure: stored=%#v err=%v", stored, findErr)
			}
			if len(semanticIndex.deletedMemoryIDs) != 1 || semanticIndex.deletedMemoryIDs[0] != memory.ID {
				t.Fatalf("semantic deletion calls = %#v", semanticIndex.deletedMemoryIDs)
			}

			semanticIndex.deleteErr = nil
			var saved *models.ContextMemory
			switch route {
			case "unscoped archive":
				saved, err = service.Archive(memory.ID, true)
			case "owner-scoped archive":
				saved, err = service.(OwnerScopedService).ArchiveForOwner("alice", memory.ID, true)
			default:
				archived := true
				saved, err = service.(OwnerScopedService).UpdateForOwner("alice", memory.ID, UpdateRequest{Archived: &archived})
			}
			if err != nil || saved == nil || !saved.Archived {
				t.Fatalf("retry archive: saved=%#v err=%v", saved, err)
			}
		})
	}
}

func TestDeleteAndArchiveReportSQLFailuresAfterVectorDeletion(t *testing.T) {
	t.Run("delete remains retryable after SQL failure", func(t *testing.T) {
		repo := newFakeRepository()
		semanticIndex := &semanticMemoryStub{}
		service := NewServiceWithSemantic(repo, semanticIndex)
		memory, err := service.(OwnerScopedService).CreateForOwner("alice", CreateRequest{Content: "Delete candidate"})
		if err != nil {
			t.Fatalf("create memory: %v", err)
		}
		repo.deleteErr = errors.New("database unavailable")
		if err := service.(OwnerScopedService).DeleteForOwner("alice", memory.ID); err == nil || !strings.Contains(err.Error(), "semantic index entry was deleted") {
			t.Fatalf("delete error = %v, want explicit partial-state error", err)
		}
		if _, err := repo.FindByID(memory.ID); err != nil || len(repo.memories) != 1 {
			t.Fatalf("SQL record should remain available for safe retry: err=%v", err)
		}
		if len(semanticIndex.deletedMemoryIDs) != 1 {
			t.Fatalf("vector delete calls = %#v", semanticIndex.deletedMemoryIDs)
		}
		if err := service.(OwnerScopedService).DeleteForOwner("alice", memory.ID); err != nil || len(repo.memories) != 0 {
			t.Fatalf("retry delete: err=%v remaining=%d", err, len(repo.memories))
		}
	})

	t.Run("archive remains retryable after SQL failure", func(t *testing.T) {
		repo := newFakeRepository()
		semanticIndex := &semanticMemoryStub{}
		service := NewServiceWithSemantic(repo, semanticIndex)
		memory, err := service.(OwnerScopedService).CreateForOwner("alice", CreateRequest{Content: "Archive candidate"})
		if err != nil {
			t.Fatalf("create memory: %v", err)
		}
		repo.updateErr = errors.New("database unavailable")
		if _, err := service.(OwnerScopedService).ArchiveForOwner("alice", memory.ID, true); err == nil || !strings.Contains(err.Error(), "SQL archive update failed") {
			t.Fatalf("archive error = %v, want explicit partial-state error", err)
		}
		stored, err := repo.FindByID(memory.ID)
		if err != nil || stored.Archived || len(semanticIndex.deletedMemoryIDs) != 1 {
			t.Fatalf("expected unarchived SQL record and removed vector: stored=%#v err=%v vectorDeletes=%#v", stored, err, semanticIndex.deletedMemoryIDs)
		}
		saved, err := service.(OwnerScopedService).ArchiveForOwner("alice", memory.ID, true)
		if err != nil || saved == nil || !saved.Archived {
			t.Fatalf("retry archive: saved=%#v err=%v", saved, err)
		}
	})
}

func TestUnarchiveReportsSemanticIndexFailureAndCanBeRetried(t *testing.T) {
	repo := newFakeRepository()
	semanticIndex := &semanticMemoryStub{}
	service := NewServiceWithSemantic(repo, semanticIndex).(OwnerScopedService)
	memory, err := service.CreateForOwner("alice", CreateRequest{Content: "Unarchive candidate"})
	if err != nil {
		t.Fatalf("create memory: %v", err)
	}
	if _, err := service.ArchiveForOwner("alice", memory.ID, true); err != nil {
		t.Fatalf("archive memory: %v", err)
	}
	semanticIndex.indexErr = errors.New("embedding service unavailable")
	saved, err := service.ArchiveForOwner("alice", memory.ID, false)
	if err == nil || !strings.Contains(err.Error(), "unarchived in SQL but semantic index rebuild failed") {
		t.Fatalf("unarchive error = %v, want explicit partial-state error", err)
	}
	if saved == nil || saved.Archived {
		t.Fatalf("unarchive must return its persisted SQL state with the error: %#v", saved)
	}
	stored, findErr := repo.FindByID(memory.ID)
	if findErr != nil || stored.Archived {
		t.Fatalf("SQL unarchive state = %#v err=%v", stored, findErr)
	}

	semanticIndex.indexErr = nil
	if saved, err = service.ArchiveForOwner("alice", memory.ID, false); err != nil || saved == nil || saved.Archived {
		t.Fatalf("retry unarchive: saved=%#v err=%v", saved, err)
	}
	if len(semanticIndex.indexedMemoryIDs) < 2 {
		t.Fatalf("unarchive retry did not retry semantic indexing: %#v", semanticIndex.indexedMemoryIDs)
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
	deletedMemoryIDs []uuid.UUID
	searchErr        error
	indexErr         error
	deleteErr        error
	disabled         bool
}

var _ semantic.Service = (*semanticMemoryStub)(nil)

func (s *semanticMemoryStub) Enabled() bool                                         { return !s.disabled }
func (s *semanticMemoryStub) Reason() string                                        { return "test local semantic retrieval" }
func (s *semanticMemoryStub) Index(context.Context, *models.SourceExtraction) error { return nil }
func (s *semanticMemoryStub) Search(context.Context, semantic.SearchRequest) ([]semantic.Match, error) {
	return nil, nil
}
func (s *semanticMemoryStub) IndexMemory(_ context.Context, memory *models.ContextMemory) error {
	if memory != nil {
		s.indexedMemoryIDs = append(s.indexedMemoryIDs, memory.ID)
	}
	return s.indexErr
}
func (s *semanticMemoryStub) DeleteMemory(_ context.Context, id uuid.UUID) error {
	s.deletedMemoryIDs = append(s.deletedMemoryIDs, id)
	if s.deleteErr != nil {
		return s.deleteErr
	}
	return nil
}
func (s *semanticMemoryStub) SearchMemory(_ context.Context, request semantic.MemorySearchRequest) ([]semantic.MemoryMatch, error) {
	s.requests = append(s.requests, request)
	if s.searchErr != nil {
		return nil, s.searchErr
	}
	return s.matches, nil
}

func TestOwnerScopedMemoryQuarantinesOwnerlessRecords(t *testing.T) {
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

func TestOwnerScopedMemoryUsesRepositoryBoundaryWhenAvailable(t *testing.T) {
	repo := newOwnerScopedFakeRepository()
	service := NewService(repo)
	scoped := service.(OwnerScopedService)

	alice, err := scoped.CreateForOwner("alice", CreateRequest{
		ProjectKey: "vivare", Kind: "decision", Content: "Collect the source-linked evidence first.",
	})
	if err != nil {
		t.Fatalf("CreateForOwner: %v", err)
	}
	if repo.findAllForOwnerCalls != 1 || repo.unscopedFindAllCalls != 0 {
		t.Fatalf("create did not use owner query: scoped=%d unscoped=%d", repo.findAllForOwnerCalls, repo.unscopedFindAllCalls)
	}

	if _, err := scoped.FindAllForOwner("alice", "vivare", false); err != nil {
		t.Fatalf("FindAllForOwner: %v", err)
	}
	if repo.findAllForOwnerCalls != 2 || repo.unscopedFindAllCalls != 0 {
		t.Fatalf("list did not use owner query: scoped=%d unscoped=%d", repo.findAllForOwnerCalls, repo.unscopedFindAllCalls)
	}

	if _, err := scoped.FindByIDForOwner("alice", alice.ID); err != nil {
		t.Fatalf("FindByIDForOwner: %v", err)
	}
	if repo.findByIDForOwnerCalls != 1 || repo.unscopedFindByIDCalls != 0 {
		t.Fatalf("lookup did not use owner query: scoped=%d unscoped=%d", repo.findByIDForOwnerCalls, repo.unscopedFindByIDCalls)
	}
}

type fakeRepository struct {
	memories  map[uuid.UUID]models.ContextMemory
	updateErr error
	deleteErr error
}

// This adapter exercises memory-write behavior in unit tests only. Production
// promotion requires GormRepository's durable provenance transaction.
type verifiedFactTestRepository struct {
	Repository
}

func (r *verifiedFactTestRepository) WithVerifiedFactPromotion(
	ctx context.Context,
	ownerIdentity string,
	request CreateRequest,
	provenance VerifiedFactProvenance,
	promote func(Repository) (*models.ContextMemory, error),
) (*models.ContextMemory, error) {
	if ctx == nil || promote == nil {
		return nil, errors.New("test promotion context and callback are required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if _, err := requireOwnerIdentity(ownerIdentity); err != nil {
		return nil, err
	}
	if err := validateVerifiedFactRequest(request, provenance); err != nil {
		return nil, err
	}
	return promote(r.Repository)
}

func newVerifiedFactTestService(repository Repository) Service {
	return NewService(&verifiedFactTestRepository{Repository: repository})
}

type ownerScopedFakeRepository struct {
	*fakeRepository
	findAllForOwnerCalls  int
	findByIDForOwnerCalls int
	unscopedFindAllCalls  int
	unscopedFindByIDCalls int
}

func newOwnerScopedFakeRepository() *ownerScopedFakeRepository {
	return &ownerScopedFakeRepository{fakeRepository: newFakeRepository()}
}

func (r *ownerScopedFakeRepository) FindAll(projectKey string, includeArchived bool) ([]models.ContextMemory, error) {
	r.unscopedFindAllCalls++
	return r.fakeRepository.FindAll(projectKey, includeArchived)
}

func (r *ownerScopedFakeRepository) FindByID(id uuid.UUID) (*models.ContextMemory, error) {
	r.unscopedFindByIDCalls++
	return r.fakeRepository.FindByID(id)
}

func (r *ownerScopedFakeRepository) FindAllForOwner(ownerIdentity, projectKey string, includeArchived bool) ([]models.ContextMemory, error) {
	r.findAllForOwnerCalls++
	all, err := r.fakeRepository.FindAll(projectKey, includeArchived)
	if err != nil {
		return nil, err
	}
	return filterReadableMemories(all, ownerIdentity), nil
}

func (r *ownerScopedFakeRepository) FindByIDForOwner(ownerIdentity string, id uuid.UUID) (*models.ContextMemory, error) {
	r.findByIDForOwnerCalls++
	memory, err := r.fakeRepository.FindByID(id)
	if err != nil || !readableByOwner(memory, ownerIdentity) {
		return nil, gorm.ErrRecordNotFound
	}
	return memory, nil
}

func (r *ownerScopedFakeRepository) FindRecentForOwner(ownerIdentity, projectKey string, includeArchived bool, limit int) ([]models.ContextMemory, error) {
	all, err := r.fakeRepository.FindAll(projectKey, includeArchived)
	if err != nil {
		return nil, err
	}
	visible := filterReadableMemories(all, ownerIdentity)
	if len(visible) > limit {
		return visible[:limit], nil
	}
	return visible, nil
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
	if r.updateErr != nil {
		err := r.updateErr
		r.updateErr = nil
		return nil, err
	}
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
	if r.deleteErr != nil {
		err := r.deleteErr
		r.deleteErr = nil
		return err
	}
	delete(r.memories, id)
	return nil
}
