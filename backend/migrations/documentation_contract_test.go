package migrations

import (
	"fmt"
	"io/fs"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

func migrationGuide(t *testing.T) string {
	t.Helper()
	content, err := os.ReadFile("../../docs/migrations.md")
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}

func TestMigrationDocumentationReferencesShippedPairs(t *testing.T) {
	guide := migrationGuide(t)
	references := regexp.MustCompile(`\b(?:pre|post)/[0-9]{4}_[a-z0-9_]+`).FindAllString(guide, -1)
	if len(references) == 0 {
		t.Fatal("migration guide contains no phase-qualified references")
	}
	seen := make(map[string]bool)
	for _, reference := range references {
		if seen[reference] {
			continue
		}
		seen[reference] = true
		for _, direction := range []string{"up", "down"} {
			if _, err := fs.Stat(Files, reference+"."+direction+".sql"); err != nil {
				t.Errorf("documented migration %s has no shipped %s script: %v", reference, direction, err)
			}
		}
	}
}

func TestMigrationDocumentationTracksEmbeddedHeads(t *testing.T) {
	guide := migrationGuide(t)
	for _, phase := range []string{"pre", "post"} {
		entries, err := fs.ReadDir(Files, phase)
		if err != nil {
			t.Fatal(err)
		}
		var versions []string
		for _, entry := range entries {
			if strings.HasSuffix(entry.Name(), ".up.sql") {
				versions = append(versions, strings.TrimSuffix(entry.Name(), ".up.sql"))
			}
		}
		sort.Strings(versions)
		if len(versions) == 0 {
			t.Fatalf("no embedded %s migrations", phase)
		}
		row := fmt.Sprintf("| %s | %d | `%s/%s` |", phase, len(versions), phase, versions[len(versions)-1])
		if !strings.Contains(guide, row) {
			t.Errorf("migration guide snapshot is stale; review and include %s", row)
		}
	}
}
