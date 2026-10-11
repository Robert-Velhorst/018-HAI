package migrations

import (
	"strings"
	"testing"
)

func TestAmbientNeedDefaultsMigrationIsIdempotentAndRollbackSafe(t *testing.T) {
	upBytes, err := Files.ReadFile("pre/0062_ambient_need_defaults.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	downBytes, err := Files.ReadFile("pre/0062_ambient_need_defaults.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	up := strings.ToLower(string(upBytes))
	down := strings.ToLower(string(downBytes))

	if !strings.Contains(up, "on conflict (key) do nothing") {
		t.Fatal("ambient defaults must preserve an existing installation's configured defaults")
	}
	for _, key := range []string{"physiological", "safety", "belonging", "esteem", "growth"} {
		if strings.Count(up, "'"+key+"'") != 1 {
			t.Fatalf("up migration must seed %q exactly once", key)
		}
		if strings.Count(down, "'"+key+"'") != 1 {
			t.Fatalf("down migration must identify %q exactly once", key)
		}
	}
	if !strings.Contains(down, "where (id, key) in") {
		t.Fatal("rollback must remove only rows created with the migration's deterministic identities")
	}
}
