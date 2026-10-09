package memory

import (
	"errors"
	"strings"
	"testing"

	"automation-hub-backend/internal/models"

	"github.com/google/uuid"
)

type sourceLessonRepositoryStub struct {
	Repository
	ownerIdentity string
	sourceURI     string
	request       CreateRequest
	calls         int
}

func (r *sourceLessonRepositoryStub) UpsertSourceExtractionLesson(ownerIdentity, sourceURI string, request CreateRequest) (*models.ContextMemory, error) {
	r.calls++
	r.ownerIdentity = ownerIdentity
	r.sourceURI = sourceURI
	r.request = request
	extractionID, err := canonicalSourceExtractionID(sourceURI)
	if err != nil {
		return nil, err
	}
	return &models.ContextMemory{ID: uuid.New(), OwnerIdentity: ownerIdentity, SourceURI: request.SourceURI, SourceExtractionID: &extractionID, Kind: request.Kind, Content: request.Content}, nil
}

func TestPersistSourceExtractionLessonForOwnerUsesOnlyOptionalExactProvenanceCapability(t *testing.T) {
	repo := &sourceLessonRepositoryStub{}
	service := NewService(repo)
	owner := " owner-1 "
	sourceURI := "source-extraction://" + uuid.NewString()
	request := CreateRequest{
		OwnerIdentity: "forged-owner",
		SourceURI:     "https://example.invalid/unrelated",
		SourceLabel:   "Corrected source extraction",
		Kind:          "lesson",
		Content:       "Preserve this source-correction lesson exactly for its source.",
	}

	saved, err := PersistSourceExtractionLessonForOwner(service, owner, sourceURI, request)
	if err != nil {
		t.Fatalf("persist source lesson: %v", err)
	}
	if repo.calls != 1 || repo.ownerIdentity != "owner-1" || repo.sourceURI != sourceURI {
		t.Fatalf("optional store call=(%d, %q, %q), want one call with normalized owner and exact URI", repo.calls, repo.ownerIdentity, repo.sourceURI)
	}
	if saved.OwnerIdentity != "owner-1" || saved.SourceURI != request.SourceURI || saved.SourceExtractionID == nil || *saved.SourceExtractionID != mustParseSourceExtractionID(t, sourceURI) {
		t.Fatalf("saved owner/evidence/extraction=(%q, %q, %v), want authenticated owner, evidence URI, and parsed extraction ID", saved.OwnerIdentity, saved.SourceURI, saved.SourceExtractionID)
	}
	if repo.request.SourceURI != request.SourceURI || repo.request.OwnerIdentity != request.OwnerIdentity {
		t.Fatal("service adapter should pass the payload unchanged; repository capability owns persisted identity fields")
	}
	protected := request
	protected.Kind = "source_supported_fact"
	if _, err := PersistSourceExtractionLessonForOwner(service, owner, sourceURI, protected); err == nil {
		t.Fatal("source lesson writer accepted a caller-minted verified fact")
	}
	if repo.calls != 1 {
		t.Fatalf("protected memory request reached the source lesson repository; calls=%d", repo.calls)
	}
}

func TestPersistSourceExtractionLessonRejectsMissingOwnerAndNonCanonicalURI(t *testing.T) {
	repo := &sourceLessonRepositoryStub{}
	service := NewService(repo)
	validURI := "source-extraction://" + uuid.NewString()
	tests := []struct {
		name, owner, sourceURI string
	}{
		{name: "missing owner", owner: " ", sourceURI: validURI},
		{name: "non-UUID", owner: "owner-1", sourceURI: "source-extraction://not-a-uuid"},
		{name: "uppercase UUID is not canonical", owner: "owner-1", sourceURI: "source-extraction://" + strings.ToUpper(uuid.NewString())},
		{name: "external URI", owner: "owner-1", sourceURI: "https://example.invalid/source"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := PersistSourceExtractionLessonForOwner(service, test.owner, test.sourceURI, CreateRequest{Kind: "lesson", Content: "lesson"}); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
	if repo.calls != 0 {
		t.Fatalf("invalid requests reached repository %d times", repo.calls)
	}
	if _, err := PersistSourceExtractionLessonForOwner(service, "owner-1", validURI, CreateRequest{Kind: "lesson"}); err == nil {
		t.Fatal("expected empty lesson content to fail validation")
	}
	if repo.calls != 0 {
		t.Fatalf("invalid lesson content reached repository %d times", repo.calls)
	}
}

func TestPersistSourceExtractionLessonFailsWhenRepositoryLacksOptionalCapability(t *testing.T) {
	// newFakeRepository satisfies only the existing Repository interface.
	var existing Service = NewService(newFakeRepository())
	if _, err := PersistSourceExtractionLessonForOwner(existing, "owner-1", "source-extraction://"+uuid.NewString(), CreateRequest{Kind: "lesson", Content: "lesson"}); err == nil {
		t.Fatal("expected fail-closed error from native service without optional repository capability")
	}
}

func TestSourceExtractionLessonRequestRequiresKindAndContent(t *testing.T) {
	valid := CreateRequest{
		Kind: "correction_lesson", Content: "A verified correction lesson.",
		SourceURI: "https://example.invalid/source", SourceLabel: "Source record",
	}
	for _, request := range []CreateRequest{
		{Content: "lesson"},
		{Kind: "correction_lesson", Content: "Unreviewed correction claim."},
		{Kind: strings.Repeat("x", 51), Content: "lesson"},
		{Kind: "source_supported_fact", Content: "Unverified model claim."},
		{Kind: "correction_decision", Content: "Unreviewed correction claim."},
		{Kind: "lesson", Content: strings.Repeat("x", maxMemoryContentBytes+1)},
		{Kind: "lesson", Content: "lesson", SourceLabel: strings.Repeat("x", 256)},
		{Kind: "lesson", Content: "lesson", Tags: make([]string, maxMemoryTags+1)},
		{Kind: valid.Kind, Content: valid.Content, SourceLabel: valid.SourceLabel},
		{Kind: valid.Kind, Content: valid.Content, SourceURI: valid.SourceURI},
	} {
		if err := validateSourceExtractionLessonRequest(request); err == nil {
			t.Fatalf("expected invalid lesson request to fail: %#v", request)
		}
	}
}

func TestIndexPersistedSourceExtractionLessonIsBestEffortWithoutRewritingMemory(t *testing.T) {
	repo := newFakeRepository()
	indexer := &semanticMemoryStub{indexErr: errors.New("embedding service unavailable")}
	service := NewServiceWithSemantic(repo, indexer)
	persisted := &models.ContextMemory{
		ID:                 uuid.New(),
		OwnerIdentity:      "owner-1",
		SourceURI:          "https://example.invalid/original-evidence",
		SourceExtractionID: sourceExtractionIDPointer(uuid.New()),
		Kind:               "correction_lesson",
		Content:            "Already persisted in the worker transaction.",
	}

	IndexPersistedSourceExtractionLesson(service, persisted)

	if len(indexer.indexedMemoryIDs) != 1 || indexer.indexedMemoryIDs[0] != persisted.ID {
		t.Fatalf("semantic index calls = %v, want only persisted memory %s", indexer.indexedMemoryIDs, persisted.ID)
	}
	if len(repo.memories) != 0 {
		t.Fatalf("indexing wrote %d context-memory rows; it must only invoke semantic indexing", len(repo.memories))
	}
}

func mustParseSourceExtractionID(t *testing.T, sourceURI string) uuid.UUID {
	t.Helper()
	id, err := canonicalSourceExtractionID(sourceURI)
	if err != nil {
		t.Fatalf("parse canonical source extraction ID: %v", err)
	}
	return id
}

func sourceExtractionIDPointer(id uuid.UUID) *uuid.UUID {
	return &id
}

type serviceWithoutPersistedSourceLessonIndexer struct {
	Service
}

func TestIndexPersistedSourceExtractionLessonIsOptional(t *testing.T) {
	repo := newFakeRepository()
	var service Service = serviceWithoutPersistedSourceLessonIndexer{Service: NewService(repo)}
	persisted := &models.ContextMemory{ID: uuid.New()}

	IndexPersistedSourceExtractionLesson(service, persisted)
	IndexPersistedSourceExtractionLesson(nil, persisted)
	IndexPersistedSourceExtractionLesson(service, nil)

	if len(repo.memories) != 0 {
		t.Fatalf("optional indexing wrote %d context-memory rows", len(repo.memories))
	}
}
