package infra

import (
	"strings"
	"testing"
	"testing/fstest"

	"automation-hub-backend/migrations"
)

func TestSplitSQLStatements(t *testing.T) {
	script := `-- a comment
DROP INDEX IF EXISTS idx_old;
CREATE UNIQUE INDEX IF NOT EXISTS idx_new ON t (a, b);

-- trailing comment only
`
	got := splitSQLStatements(script)
	if len(got) != 2 {
		t.Fatalf("expected 2 statements, got %d: %#v", len(got), got)
	}
	if got[0] != "DROP INDEX IF EXISTS idx_old" {
		t.Fatalf("statement[0] = %q", got[0])
	}
	if got[1] != "CREATE UNIQUE INDEX IF NOT EXISTS idx_new ON t (a, b)" {
		t.Fatalf("statement[1] = %q", got[1])
	}
}

func TestSplitSQLStatementsKeepsDollarQuotedBlocksIntact(t *testing.T) {
	// A guarded constraint uses DO $$ ... $$ and contains semicolons that must
	// NOT split the statement.
	script := `CREATE TABLE IF NOT EXISTS t (id int);
DO $$ BEGIN
  ALTER TABLE ONLY t ADD CONSTRAINT t_pkey PRIMARY KEY (id);
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;
CREATE INDEX IF NOT EXISTS i ON t (id);`
	got := splitSQLStatements(script)
	if len(got) != 3 {
		t.Fatalf("expected 3 statements, got %d: %#v", len(got), got)
	}
	if !strings.HasPrefix(got[1], "DO $$") || !strings.HasSuffix(got[1], "END $$") {
		t.Fatalf("dollar-quoted block was split: %q", got[1])
	}
	if !strings.Contains(got[1], "EXCEPTION WHEN duplicate_object") {
		t.Fatalf("block body lost its exception handler: %q", got[1])
	}
}

func TestSplitSQLStatementsKeepsStringsIdentifiersAndCommentsIntact(t *testing.T) {
	script := `INSERT INTO notes(value) VALUES ('left;right'), (E'can\'t; split'); -- ignored ;
SELECT "column;name";
/* outer ; /* nested ; */ still ; */ SELECT 3;`
	got := splitSQLStatements(script)
	if len(got) != 3 {
		t.Fatalf("expected 3 statements, got %d: %#v", len(got), got)
	}
	if got[0] != `INSERT INTO notes(value) VALUES ('left;right'), (E'can\'t; split')` {
		t.Fatalf("string literal was split or changed: %q", got[0])
	}
	if got[1] != `SELECT "column;name"` {
		t.Fatalf("quoted identifier was split or changed: %q", got[1])
	}
	if !strings.HasPrefix(got[2], "/* outer ; /* nested ; */ still ; */") ||
		!strings.HasSuffix(got[2], "SELECT 3") {
		t.Fatalf("nested block comment was split or changed: %q", got[2])
	}
}

func TestDollarTagAtRejectsInvalidTagStart(t *testing.T) {
	for _, script := range []string{"$1$", "$bad-tag$"} {
		if tag, width := dollarTagAt(script, 0); width != 0 || tag != "" {
			t.Errorf("dollarTagAt(%q) = (%q, %d), want no tag", script, tag, width)
		}
	}
	if tag, width := dollarTagAt("$valid_2$", 0); tag != "$valid_2$" || width != len(tag) {
		t.Fatalf("valid dollar tag = (%q, %d)", tag, width)
	}
}

func TestPendingMigrationsSkipsAppliedAndKeepsOrder(t *testing.T) {
	all := []Migration{
		{Version: "post/0001_a"},
		{Version: "post/0002_b"},
		{Version: "post/0003_c"},
	}
	pending := pendingMigrations(all, map[string]bool{"post/0001_a": true})
	if len(pending) != 2 || pending[0].Version != "post/0002_b" || pending[1].Version != "post/0003_c" {
		t.Fatalf("pending = %#v, want 0002 then 0003", pending)
	}
}

func TestMigrationChecksumBindsVersionAndBothScripts(t *testing.T) {
	base := Migration{
		Version: "pre/0001_example",
		UpSQL:   "CREATE TABLE example (id INT);",
		DownSQL: "DROP TABLE example;",
	}
	want := MigrationChecksum(base)
	if len(want) != 64 {
		t.Fatalf("checksum length = %d, want SHA-256 hex length 64", len(want))
	}
	for name, changed := range map[string]Migration{
		"version":  {Version: "pre/0002_example", UpSQL: base.UpSQL, DownSQL: base.DownSQL},
		"up SQL":   {Version: base.Version, UpSQL: base.UpSQL + " ", DownSQL: base.DownSQL},
		"down SQL": {Version: base.Version, UpSQL: base.UpSQL, DownSQL: base.DownSQL + " "},
	} {
		t.Run(name, func(t *testing.T) {
			if got := MigrationChecksum(changed); got == want {
				t.Fatal("changed migration content retained the original checksum")
			}
		})
	}
}

func TestValidateAppliedChecksumsFailsClosedForLegacyAndChangedSource(t *testing.T) {
	migration := Migration{
		Version: "pre/0001_example",
		UpSQL:   "CREATE TABLE example (id INT);",
		DownSQL: "DROP TABLE example;",
	}
	all := []Migration{migration}
	applied := map[string]bool{migration.Version: true}
	if err := validateAppliedChecksums(all, applied, map[string]string{}); err == nil || !strings.Contains(err.Error(), "no content checksum") {
		t.Fatalf("legacy checksum validation = %v, want fail-closed legacy error", err)
	}
	if err := validateAppliedChecksums(all, applied, map[string]string{migration.Version: MigrationChecksum(migration)}); err != nil {
		t.Fatalf("matching checksum rejected: %v", err)
	}
	changed := migration
	changed.UpSQL += " -- modified"
	if err := validateAppliedChecksums([]Migration{changed}, applied, map[string]string{migration.Version: MigrationChecksum(migration)}); err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("changed migration validation = %v, want mismatch", err)
	}
}

func TestValidateAppliedVersionsRejectsMissingVersionInCurrentPhase(t *testing.T) {
	all := []Migration{
		{Version: "pre/0001_known"},
		{Version: "post/0001_other_phase"},
	}
	applied := map[string]bool{
		"pre/0001_known":        true,
		"pre/0002_removed":      true,
		"post/0002_other_phase": true,
	}

	err := validateAppliedVersions(all, applied, "pre")
	if err == nil || !strings.Contains(err.Error(), "pre/0002_removed") {
		t.Fatalf("validateAppliedVersions error = %v, want missing pre migration to be reported", err)
	}
	if strings.Contains(err.Error(), "post/0002_other_phase") {
		t.Fatalf("validation for pre phase included a post migration: %v", err)
	}

	if err := validateAppliedVersions(all, map[string]bool{
		"pre/0001_known":        true,
		"post/0001_other_phase": true,
	}, "pre"); err != nil {
		t.Fatalf("known applied versions were rejected: %v", err)
	}
}

func TestValidateAppliedVersionsRequiresOrderedPrefix(t *testing.T) {
	all := []Migration{
		{Version: "pre/0001_first"},
		{Version: "pre/0002_second"},
		{Version: "pre/0003_third"},
	}
	for name, applied := range map[string]map[string]bool{
		"fresh ledger":    {},
		"first migration": {"pre/0001_first": true},
		"ordered prefix": {
			"pre/0001_first":  true,
			"pre/0002_second": true,
		},
		"complete chain": {
			"pre/0001_first":  true,
			"pre/0002_second": true,
			"pre/0003_third":  true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := validateAppliedVersions(all, applied, "pre"); err != nil {
				t.Fatalf("valid ledger rejected: %v", err)
			}
		})
	}

	for name, applied := range map[string]map[string]bool{
		"missing first": {"pre/0002_second": true},
		"gap before latest": {
			"pre/0001_first": true,
			"pre/0003_third": true,
		},
		"multiple successors after gap": {
			"pre/0002_second": true,
			"pre/0003_third":  true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			err := validateAppliedVersions(all, applied, "pre")
			if err == nil || !strings.Contains(err.Error(), "out-of-order applied pre versions") {
				t.Fatalf("validateAppliedVersions error = %v, want out-of-order ledger rejection", err)
			}
		})
	}
}

func TestPrePhaseRollbackRequiresKnownAndUnappliedPostPhase(t *testing.T) {
	fsys := fstest.MapFS{
		"pre/0001_base.up.sql":   &fstest.MapFile{Data: []byte("CREATE TABLE base_probe (id INTEGER PRIMARY KEY);")},
		"pre/0001_base.down.sql": &fstest.MapFile{Data: []byte("DROP TABLE base_probe;")},
		"post/0001_extension.up.sql": &fstest.MapFile{Data: []byte(
			"CREATE INDEX base_probe_idx ON base_probe (id);",
		)},
		"post/0001_extension.down.sql": &fstest.MapFile{Data: []byte("DROP INDEX base_probe_idx;")},
	}

	if err := validateNoLaterPhaseApplied(fsys, "pre", map[string]bool{}); err != nil {
		t.Fatalf("pre rollback with unapplied post phase: %v", err)
	}
	err := validateNoLaterPhaseApplied(fsys, "pre", map[string]bool{"post/0001_extension": true})
	if err == nil || !strings.Contains(err.Error(), "post-phase migrations first") {
		t.Fatalf("pre rollback with applied post phase = %v, want cross-phase order refusal", err)
	}

	unknownPost := fstest.MapFS{
		"pre/0001_base.up.sql":   &fstest.MapFile{Data: []byte("SELECT 1;")},
		"pre/0001_base.down.sql": &fstest.MapFile{Data: []byte("SELECT 1;")},
	}
	if err := validateNoLaterPhaseApplied(unknownPost, "pre", nil); err == nil ||
		!strings.Contains(err.Error(), "cannot verify post-phase migrations") {
		t.Fatalf("pre rollback without post migration sources = %v, want fail-closed error", err)
	}
}

func TestLoadMigrationsFromEmbeddedFiles(t *testing.T) {
	for _, tc := range []struct {
		dir         string
		wantVersion string
		wantDown    bool
	}{
		{"pre", "pre/0001_extensions", true},
		{"post", "post/0001_conversation_owner_identity", true},
	} {
		loaded, err := loadMigrations(migrations.Files, tc.dir)
		if err != nil {
			t.Fatalf("loadMigrations(%q): %v", tc.dir, err)
		}
		if len(loaded) == 0 {
			t.Fatalf("loadMigrations(%q) returned no migrations", tc.dir)
		}
		if loaded[0].Version != tc.wantVersion {
			t.Fatalf("first %q version = %q, want %q", tc.dir, loaded[0].Version, tc.wantVersion)
		}
		if loaded[0].UpSQL == "" {
			t.Fatalf("%q up SQL is empty", tc.wantVersion)
		}
		if tc.wantDown && loaded[0].DownSQL == "" {
			t.Fatalf("%q down SQL is empty; every up must have a down", tc.wantVersion)
		}
	}
}

func TestLegacyBaselineGuardsExistingPrimaryKeys(t *testing.T) {
	loaded, err := loadMigrations(migrations.Files, "pre")
	if err != nil {
		t.Fatalf("load pre migrations: %v", err)
	}

	var baseline string
	for _, migration := range loaded {
		if migration.Version == "pre/0002_baseline" {
			baseline = migration.UpSQL
			break
		}
	}
	if baseline == "" {
		t.Fatal("pre/0002_baseline migration not found")
	}
	if strings.Contains(baseline, ";;") {
		t.Fatal("baseline contains a duplicated statement terminator")
	}

	primaryKeyBlocks := 0
	for _, statement := range splitSQLStatements(baseline) {
		if !strings.Contains(statement, " PRIMARY KEY ") {
			continue
		}
		primaryKeyBlocks++
		if strings.Contains(statement, "WHEN invalid_table_definition THEN NULL") ||
			!strings.Contains(statement, "pg_get_constraintdef") ||
			!strings.Contains(statement, "RAISE;") {
			t.Fatalf("primary-key replay block lacks exact PostgreSQL catalog validation:\n%s", statement)
		}
	}
	if primaryKeyBlocks == 0 {
		t.Fatal("baseline contains no primary-key blocks to validate")
	}
}
