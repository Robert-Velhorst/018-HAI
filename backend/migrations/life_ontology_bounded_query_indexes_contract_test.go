package migrations

import (
	"strings"
	"testing"
)

func TestLifeOntologyBoundedQueryIndexesAreReversible(t *testing.T) {
	upBytes, err := Files.ReadFile("pre/0063_life_ontology_bounded_query_indexes.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	downBytes, err := Files.ReadFile("pre/0063_life_ontology_bounded_query_indexes.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	up := strings.ToLower(string(upBytes))
	down := strings.ToLower(string(downBytes))

	for _, name := range []string{
		"idx_life_ontology_entities_owner_visibility_priority",
		"idx_life_ontology_entities_owner_review_priority",
		"idx_life_ontology_relations_owner_visibility",
	} {
		if strings.Count(up, "create index if not exists "+name) != 1 {
			t.Fatalf("up migration must create %s exactly once", name)
		}
		if strings.Count(down, "drop index if exists public."+name) != 1 {
			t.Fatalf("down migration must drop %s exactly once", name)
		}
	}
}
