//go:build integration

package migrations_test

import (
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"automation-hub-backend/internal/infra"
	"automation-hub-backend/migrations"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

const sourceManualSyncMigrationVersion = "pre/0087_source_manual_sync_jobs"

func TestSourceManualSyncMigration0087ApplyRollbackReapplyPreservesLegacySync(t *testing.T) {
	db := openIsolatedMigrationDatabase(t)
	files := sourceManualSync0087Files(t)
	createSourceManualSync0087Fixture(t, db)

	legacyID := uuid.New()
	legacySourceID := uuid.New()
	legacyCreatedAt := time.Date(2026, time.June, 1, 12, 30, 0, 0, time.UTC)
	if err := db.Exec(`
		INSERT INTO public.source_sync_jobs
			(id, source_id, mode, status, cursor_before, cursor_after, items_seen, message, created_at)
		VALUES (?, ?, 'scheduled', 'completed', 'cursor-before-0087', 'cursor-after-0087', 17, 'legacy completion summary', ?)`,
		legacyID, legacySourceID, legacyCreatedAt,
	).Error; err != nil {
		t.Fatalf("insert pre-0087 legacy sync row: %v", err)
	}

	applySourceManualSync0087(t, db, files)
	assertSourceManualSync0087Applied(t, db, true)
	assertLegacySourceSyncRow(t, db, legacyID, legacySourceID, legacyCreatedAt)

	if err := infra.RollbackMigration(db, files, "pre", sourceManualSyncMigrationVersion); err != nil {
		t.Fatalf("rollback clean 0087 migration: %v", err)
	}
	assertSourceManualSync0087Applied(t, db, false)
	assertLegacySourceSyncRow(t, db, legacyID, legacySourceID, legacyCreatedAt)

	applySourceManualSync0087(t, db, files)
	assertSourceManualSync0087Applied(t, db, true)
	assertLegacySourceSyncRow(t, db, legacyID, legacySourceID, legacyCreatedAt)
}

func TestSourceManualSyncMigration0087RollbackRefusesPersistedWork(t *testing.T) {
	tests := []struct {
		name         string
		insertSQL    string
		args         func() []any
		blockerQuery string
		wantReason   string
	}{
		{
			name: "manual async sync history",
			insertSQL: `INSERT INTO public.source_sync_jobs
				(id, source_id, mode, status, owner_identity, idempotency_key_hash, request_hash)
				VALUES (?, ?, 'manual_async_sync', 'completed', 'owner:test', ?, ?)`,
			args: func() []any {
				return []any{uuid.New(), uuid.New(), strings.Repeat("a", 64), strings.Repeat("b", 64)}
			},
			blockerQuery: "SELECT COUNT(*) FROM public.source_sync_jobs WHERE mode = 'manual_async_sync'",
			wantReason: "manual source sync history exists",
		},
		{
			name:         "durable source manual sync job",
			insertSQL:    "INSERT INTO public.durable_jobs (id, kind) VALUES (?, 'source.manual_sync')",
			args:         func() []any { return []any{uuid.New()} },
			blockerQuery: "SELECT COUNT(*) FROM public.durable_jobs WHERE kind = 'source.manual_sync'",
			wantReason:   "manual source sync durable queue records exist",
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			db := openIsolatedMigrationDatabase(t)
			files := sourceManualSync0087Files(t)
			createSourceManualSync0087Fixture(t, db)
			applySourceManualSync0087(t, db, files)

			if err := db.Exec(test.insertSQL, test.args()...).Error; err != nil {
				t.Fatalf("insert rollback-blocking record: %v", err)
			}

			err := infra.RollbackMigration(db, files, "pre", sourceManualSyncMigrationVersion)
			if err == nil || !strings.Contains(err.Error(), test.wantReason) {
				t.Fatalf("rollback error = %v, want it to contain %q", err, test.wantReason)
			}
			assertSourceManualSync0087Applied(t, db, true)
			assertRollbackBlockerStillPresent(t, db, test.blockerQuery)
		})
	}
}

func sourceManualSync0087Files(t *testing.T) fs.FS {
	t.Helper()
	files := fstest.MapFS{"pre": &fstest.MapFile{Mode: fs.ModeDir}}
	for _, suffix := range []string{".up.sql", ".down.sql"} {
		name := strings.TrimPrefix(sourceManualSyncMigrationVersion, "pre/") + suffix
		contents, err := migrations.Files.ReadFile("pre/" + name)
		if err != nil {
			t.Fatalf("read migration %s: %v", name, err)
		}
		files["pre/"+name] = &fstest.MapFile{Data: contents, Mode: 0o600}
	}
	return files
}

func createSourceManualSync0087Fixture(t *testing.T, db *gorm.DB) {
	t.Helper()
	// ApplyMigrations creates schema_migrations (version TEXT PRIMARY KEY,
	// applied_at TIMESTAMPTZ NOT NULL DEFAULT now()) and acquires its
	// transaction-scoped advisory lock. Migration 0087 needs source_id, mode,
	// and status on source_sync_jobs; rollback also reads durable_jobs.kind.
	// The other sync columns below are legacy data under preservation test.
	if err := db.Exec(`
		CREATE TABLE public.source_sync_jobs (
			id UUID PRIMARY KEY,
			source_id UUID NOT NULL,
			mode TEXT NOT NULL,
			status TEXT NOT NULL,
			cursor_before TEXT,
			cursor_after TEXT,
			items_seen BIGINT,
			message TEXT,
			created_at TIMESTAMPTZ
		);`).Error; err != nil {
		t.Fatalf("create minimal pre-0087 source sync table: %v", err)
	}
	if err := db.Exec(`CREATE TABLE public.durable_jobs (
			id UUID PRIMARY KEY,
			kind TEXT NOT NULL
		);`).Error; err != nil {
		t.Fatalf("create minimal pre-0087 durable job table: %v", err)
	}
}

func applySourceManualSync0087(t *testing.T, db *gorm.DB, files fs.FS) {
	t.Helper()
	count, err := infra.ApplyMigrations(db, files, "pre")
	if err != nil {
		t.Fatalf("apply migration 0087: %v", err)
	}
	if count != 1 {
		t.Fatalf("ApplyMigrations applied %d migrations, want exactly one", count)
	}
}

func assertSourceManualSync0087Applied(t *testing.T, db *gorm.DB, wantApplied bool) {
	t.Helper()
	var applied bool
	if err := db.Raw("SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = ?)", sourceManualSyncMigrationVersion).Row().Scan(&applied); err != nil {
		t.Fatalf("read migration ledger: %v", err)
	}
	if applied != wantApplied {
		t.Fatalf("migration ledger applied = %t, want %t", applied, wantApplied)
	}

	var addedColumns int64
	if err := db.Raw(`
		SELECT COUNT(*)
		FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = 'source_sync_jobs'
		  AND column_name IN ('owner_identity', 'idempotency_key_hash', 'request_hash', 'durable_job_id')`).Scan(&addedColumns).Error; err != nil {
		t.Fatalf("inspect migration metadata columns: %v", err)
	}
	wantColumns := int64(0)
	if wantApplied {
		wantColumns = 4
	}
	if addedColumns != wantColumns {
		t.Fatalf("migration metadata column count = %d, want %d", addedColumns, wantColumns)
	}

	var addedIndexes int64
	if err := db.Raw(`
		SELECT COUNT(*)
		FROM pg_indexes
		WHERE schemaname = 'public'
		  AND indexname IN (
			'ux_source_sync_jobs_owner_source_idempotency',
			'ux_source_sync_jobs_durable_job',
			'ux_source_sync_jobs_one_active_manual'
		  )`).Scan(&addedIndexes).Error; err != nil {
		t.Fatalf("inspect migration indexes: %v", err)
	}
	wantIndexes := int64(0)
	if wantApplied {
		wantIndexes = 3
	}
	if addedIndexes != wantIndexes {
		t.Fatalf("migration index count = %d, want %d", addedIndexes, wantIndexes)
	}
}

func assertLegacySourceSyncRow(t *testing.T, db *gorm.DB, id, sourceID uuid.UUID, createdAt time.Time) {
	t.Helper()
	var legacy struct {
		ID           uuid.UUID `gorm:"column:id"`
		SourceID     uuid.UUID `gorm:"column:source_id"`
		Mode         string    `gorm:"column:mode"`
		Status       string    `gorm:"column:status"`
		CursorBefore string    `gorm:"column:cursor_before"`
		CursorAfter  string    `gorm:"column:cursor_after"`
		ItemsSeen    int64     `gorm:"column:items_seen"`
		Message      string    `gorm:"column:message"`
		CreatedAt    time.Time `gorm:"column:created_at"`
	}
	if err := db.Raw(`
		SELECT id, source_id, mode, status, cursor_before, cursor_after, items_seen, message, created_at
		FROM public.source_sync_jobs WHERE id = ?`, id).Scan(&legacy).Error; err != nil {
		t.Fatalf("read legacy sync row: %v", err)
	}
	if legacy.ID != id || legacy.SourceID != sourceID || legacy.Mode != "scheduled" || legacy.Status != "completed" || legacy.CursorBefore != "cursor-before-0087" || legacy.CursorAfter != "cursor-after-0087" || legacy.ItemsSeen != 17 || legacy.Message != "legacy completion summary" || !legacy.CreatedAt.Equal(createdAt) {
		t.Fatalf("legacy sync row changed across migration lifecycle: %#v", legacy)
	}
}

func assertRollbackBlockerStillPresent(t *testing.T, db *gorm.DB, query string) {
	t.Helper()
	var count int64
	if err := db.Raw(query).Scan(&count).Error; err != nil {
		t.Fatalf("verify rollback blocker remains: %v", err)
	}
	if count != 1 {
		t.Fatalf("rollback blocker count = %d, want 1", count)
	}
}
