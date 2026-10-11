package migrations

import (
	"strings"
	"testing"
)

func TestContextMemorySourceExtractionIdentityMigrationPreservesEvidenceURI(t *testing.T) {
	up, err := Files.ReadFile("pre/0090_context_memory_source_extraction_identity.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	down, err := Files.ReadFile("pre/0090_context_memory_source_extraction_identity.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	upSQL := strings.ToLower(string(up))
	downSQL := strings.ToLower(string(down))
	for _, required := range []string{
		"add column source_extraction_id uuid",
		"set source_extraction_id = substring(",
		"where source_uri ~ '^source-extraction://",
		"group by owner_identity, source_extraction_id, kind",
		"create unique index ux_context_memories_owner_source_extraction_kind",
		"on public.context_memories (owner_identity, source_extraction_id, kind)",
		"where source_extraction_id is not null",
	} {
		if !strings.Contains(upSQL, required) {
			t.Errorf("source-extraction identity migration is missing %q", required)
		}
	}
	if strings.Contains(upSQL, "set source_uri") || strings.Contains(upSQL, "source_uri =") {
		t.Fatal("identity migration must not overwrite original evidence SourceURI values")
	}
	for _, required := range []string{
		"drop index if exists public.ux_context_memories_owner_source_extraction_kind",
		"drop column source_extraction_id",
	} {
		if !strings.Contains(downSQL, required) {
			t.Errorf("source-extraction identity rollback is missing %q", required)
		}
	}
	if strings.Contains(upSQL, "source_extraction_id uuid not null") {
		t.Fatal("SourceExtractionID must remain nullable for ordinary memory records")
	}
}
