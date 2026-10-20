package memory

import (
	"testing"

	"automation-hub-backend/internal/models"
)

func memoryCreateForTest(service Service, owner string) func(CreateRequest) (*models.ContextMemory, error) {
	if owner == "" {
		return service.Create
	}
	return func(request CreateRequest) (*models.ContextMemory, error) {
		return service.(OwnerScopedService).CreateForOwner(owner, request)
	}
}

func TestMemoryCreateKeepsDistinctSourceURIs(t *testing.T) {
	const content = "The lawyer requested signed hearing documents and the original dated correspondence before the deadline."
	for _, owner := range []string{"", "alice"} {
		for _, similar := range []bool{false, true} {
			name := "legacy/exact"
			if owner != "" {
				name = "owner/exact"
			}
			if similar {
				name += "/similar"
			}
			t.Run(name, func(t *testing.T) {
				repository := newFakeRepository()
				create := memoryCreateForTest(NewService(repository), owner)
				first, err := create(CreateRequest{ProjectKey: "case-a", Kind: "note", Content: content, SourceURI: "https://records.example/first"})
				if err != nil {
					t.Fatal(err)
				}
				incoming := content
				if similar {
					incoming += " Tomorrow."
				}
				secondRequest := CreateRequest{ProjectKey: "case-a", Kind: "note", Content: incoming, SourceURI: "https://records.example/second"}
				second, err := create(secondRequest)
				if err != nil {
					t.Fatal(err)
				}
				if first.ID == second.ID || len(repository.memories) != 2 {
					t.Fatalf("distinct source records merged: first=%s second=%s count=%d", first.ID, second.ID, len(repository.memories))
				}
				if stored := repository.memories[first.ID]; stored.Content != content || stored.SourceURI != "https://records.example/first" {
					t.Fatalf("first source record changed: %#v", stored)
				}
				if stored := repository.memories[second.ID]; stored.Content != incoming || stored.SourceURI != secondRequest.SourceURI {
					t.Fatalf("second source record lost content or provenance: %#v", stored)
				}
				repeated, err := create(secondRequest)
				if err != nil {
					t.Fatal(err)
				}
				if repeated.ID != second.ID || len(repository.memories) != 2 {
					t.Fatal("same-source repeat did not deduplicate within its own lineage")
				}
			})
		}
	}
}

func TestLegacyExactMemoryDedupChecksCanonicalContent(t *testing.T) {
	repository := newFakeRepository()
	service := NewService(repository)
	first, err := service.Create(CreateRequest{ProjectKey: "case-a", Kind: "note", Content: "Unrelated cooking records and meal planning."})
	if err != nil {
		t.Fatal(err)
	}
	incoming := "Hearing preparation requires the original dated correspondence."
	first.ContentHash = hashContent("case-a", "note", incoming)
	repository.memories[first.ID] = *first
	second, err := service.Create(CreateRequest{ProjectKey: "case-a", Kind: "note", Content: incoming})
	if err != nil {
		t.Fatal(err)
	}
	if first.ID == second.ID || len(repository.memories) != 2 {
		t.Fatal("legacy hash fast path merged unrelated content")
	}
}

func TestMemoryDuplicatePreservesOrAcquiresMissingSourceURI(t *testing.T) {
	const source = "https://records.example/source"
	for _, owner := range []string{"", "alice"} {
		for _, sources := range [][2]string{{"", source}, {"", "  " + source + "  "}, {source, ""}, {source, "  " + source + "  "}} {
			repository := newFakeRepository()
			create := memoryCreateForTest(NewService(repository), owner)
			request := CreateRequest{ProjectKey: "case-a", Kind: "note", Content: "Keep the original dated correspondence.", SourceURI: sources[0]}
			first, err := create(request)
			if err != nil {
				t.Fatal(err)
			}
			request.SourceURI = sources[1]
			second, err := create(request)
			if err != nil {
				t.Fatal(err)
			}
			if second.ID != first.ID || len(repository.memories) != 1 || repository.memories[first.ID].SourceURI != source {
				t.Fatalf("duplicate lost or failed to acquire provenance: owner=%q sources=%q stored=%#v", owner, sources, repository.memories)
			}
		}
	}
}
