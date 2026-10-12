package migrations_test

import (
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"automation-hub-backend/internal/infra"
	"automation-hub-backend/migrations"
	"gorm.io/gorm"
)

const sourceCursorStorageMigration = "0104_source_sync_cursor_storage"

func TestSourceCursorStorageMigrationPreservesExistingAndLongCursors(t *testing.T) {
	db := openIsolatedMigrationDatabase(t)
	if err := db.Exec(`CREATE TABLE public.connected_sources (cursor character varying(512))`).Error; err != nil {
		t.Fatalf("create connected_sources fixture: %v", err)
	}
	if err := db.Exec(`CREATE TABLE public.source_sync_jobs (
		cursor_before character varying(512),
		cursor_after character varying(512)
	)`).Error; err != nil {
		t.Fatalf("create source_sync_jobs fixture: %v", err)
	}

	legacyCursor := strings.Repeat("c", 500)
	if err := db.Exec(`INSERT INTO public.connected_sources(cursor) VALUES (?)`, legacyCursor).Error; err != nil {
		t.Fatalf("insert legacy connected-source cursor: %v", err)
	}
	if err := db.Exec(`INSERT INTO public.source_sync_jobs(cursor_before, cursor_after) VALUES (?, ?)`, legacyCursor, legacyCursor).Error; err != nil {
		t.Fatalf("insert legacy sync-job cursors: %v", err)
	}

	files := cursorMigrationFiles(t)
	if count, err := infra.ApplyMigrations(db, files, "pre"); err != nil || count != 1 {
		t.Fatalf("apply cursor migration = %d, %v; want one applied migration", count, err)
	}
	assertSourceCursorColumnType(t, db, "connected_sources", "cursor", "text")
	assertSourceCursorColumnType(t, db, "source_sync_jobs", "cursor_before", "text")
	assertSourceCursorColumnType(t, db, "source_sync_jobs", "cursor_after", "text")

	var stored string
	if err := db.Raw(`SELECT cursor FROM public.connected_sources LIMIT 1`).Scan(&stored).Error; err != nil || stored != legacyCursor {
		t.Fatalf("legacy cursor after upgrade = %d characters, %v; want unchanged %d characters", len(stored), err, len(legacyCursor))
	}

	longCursor := strings.Repeat("n", 700)
	if err := db.Exec(`INSERT INTO public.connected_sources(cursor) VALUES (?)`, longCursor).Error; err != nil {
		t.Fatalf("insert long connected-source cursor: %v", err)
	}
	if err := db.Exec(`INSERT INTO public.source_sync_jobs(cursor_before, cursor_after) VALUES (?, ?)`, longCursor, longCursor).Error; err != nil {
		t.Fatalf("insert long sync-job cursors: %v", err)
	}
	version := "pre/" + sourceCursorStorageMigration
	if err := infra.RollbackMigration(db, files, "pre", version); err == nil || !strings.Contains(err.Error(), "longer cursor data exists") {
		t.Fatalf("rollback with long cursors = %v; want loss-prevention refusal", err)
	}
	assertSourceCursorColumnType(t, db, "connected_sources", "cursor", "text")

	if err := db.Exec(`DELETE FROM public.connected_sources WHERE length(cursor) > 512`).Error; err != nil {
		t.Fatalf("remove long test cursor: %v", err)
	}
	if err := db.Exec(`DELETE FROM public.source_sync_jobs WHERE length(cursor_before) > 512 OR length(cursor_after) > 512`).Error; err != nil {
		t.Fatalf("remove long test sync-job cursors: %v", err)
	}
	if err := infra.RollbackMigration(db, files, "pre", version); err != nil {
		t.Fatalf("rollback after long cursors are removed: %v", err)
	}
	assertSourceCursorColumnType(t, db, "connected_sources", "cursor", "character varying")
	assertSourceCursorColumnType(t, db, "source_sync_jobs", "cursor_before", "character varying")
	assertSourceCursorColumnType(t, db, "source_sync_jobs", "cursor_after", "character varying")
	var restored string
	if err := db.Raw(`SELECT cursor /* read after restoring varchar type */ FROM public.connected_sources LIMIT 1`).Scan(&restored).Error; err != nil || restored != legacyCursor {
		t.Fatalf("legacy cursor after rollback = %d characters, %v; want unchanged %d characters", len(restored), err, len(legacyCursor))
	}
}

func cursorMigrationFiles(t *testing.T) fs.FS {
	t.Helper()
	files := fstest.MapFS{"pre": &fstest.MapFile{Mode: fs.ModeDir}}
	for _, extension := range []string{"up.sql", "down.sql"} {
		name := sourceCursorStorageMigration + "." + extension
		data, err := fs.ReadFile(migrations.Files, "pre/"+name)
		if err != nil {
			t.Fatalf("read embedded cursor migration %s: %v", name, err)
		}
		files["pre/"+name] = &fstest.MapFile{Data: data, Mode: 0o600}
	}
	return files
}

func assertSourceCursorColumnType(t *testing.T, db *gorm.DB, table, column, want string) {
	t.Helper()
	var got string
	if err := db.Raw(`SELECT data_type FROM information_schema.columns WHERE table_schema = 'public' AND table_name = ? AND column_name = ?`, table, column).Scan(&got).Error; err != nil {
		t.Fatalf("read %s.%s data type: %v", table, column, err)
	}
	if got != want {
		t.Fatalf("%s.%s data type = %q, want %q", table, column, got, want)
	}
}
