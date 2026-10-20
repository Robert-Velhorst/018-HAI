//go:build integration

package memory

import (
	"testing"

	"automation-hub-backend/internal/models"
)

func TestPostgresMemoryDedupPreservesDistinctSources(t *testing.T) {
	fixture := openSourceLessonPostgresFixture(t)
	service := NewService(NewGormRepository(fixture.db))
	const original = "The lawyer requested signed hearing documents and the original dated correspondence before the deadline."
	for _, test := range []struct {
		name    string
		owner   string
		similar bool
	}{
		{name: "legacy_exact"},
		{name: "legacy_similar", similar: true},
		{name: "owner_exact", owner: "alice"},
		{name: "owner_similar", owner: "alice", similar: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			create := memoryCreateForTest(service, test.owner)
			project := "provenance-" + test.name
			first, err := create(CreateRequest{ProjectKey: project, Kind: "note", Content: original, SourceURI: "https://records.example/first"})
			if err != nil {
				t.Fatal(err)
			}
			incoming := original
			if test.similar {
				incoming += " Tomorrow."
			}
			secondRequest := CreateRequest{ProjectKey: project, Kind: "note", Content: incoming, SourceURI: "https://records.example/second"}
			second, err := create(secondRequest)
			if err != nil {
				t.Fatal(err)
			}
			if first.ID == second.ID {
				t.Fatal("transactional repository merged different source links")
			}
			if !test.similar && first.ContentHash != second.ContentHash {
				t.Fatal("exact fixture did not exercise equal content hashes")
			}
			repeated, err := create(secondRequest)
			if err != nil {
				t.Fatal(err)
			}
			if repeated.ID != second.ID {
				t.Fatal("same-source repeat created a third record")
			}
			var stored []models.ContextMemory
			if err := fixture.db.Where("project_key = ?", project).Find(&stored).Error; err != nil {
				t.Fatal(err)
			}
			if len(stored) != 2 {
				t.Fatalf("stored rows = %d, want two source lineages", len(stored))
			}
			for _, record := range stored {
				if record.OwnerIdentity != test.owner {
					t.Fatal("repository changed memory ownership")
				}
				switch record.ID {
				case first.ID:
					if record.SourceURI != "https://records.example/first" || record.Content != original {
						t.Fatal("first persisted source record changed")
					}
				case second.ID:
					if record.SourceURI != secondRequest.SourceURI || record.Content != incoming {
						t.Fatal("second persisted source record lost provenance")
					}
				default:
					t.Fatal("unexpected persisted memory")
				}
			}
		})
	}
}
