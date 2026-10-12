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

const (
	openClawSessionReviewMigration = "pre/0070_openclaw_gateway_session_review"
	trelloResumableSyncMigration   = "pre/0095_trello_resumable_sync"
	openClawArtifactClaimMigration = "pre/0098_openclaw_artifact_claim_leases"
	accountFeedRegistryMigration   = "pre/0110_account_feed_registry"
)

func TestAccountFeedRegistryRollbackIsEmptyOnly(t *testing.T) {
	down := rollbackSQLWithoutLineComments(readRawRollbackMigration(t, accountFeedRegistryMigration))
	lockAt := strings.Index(down, "lock table public.account_feed_audits, public.account_feeds")
	preflightAt := strings.Index(down, "do $$")
	guardEnd := -1
	if preflightAt >= 0 {
		if delimiter := strings.Index(down[preflightAt:], "$$;"); delimiter >= 0 {
			guardEnd = preflightAt + delimiter
		}
	}
	dropAt := strings.Index(down, "drop table if exists public.account_feed_audits")
	if lockAt < 0 || preflightAt < lockAt || guardEnd < preflightAt || dropAt < guardEnd {
		t.Fatalf("account-feed rollback must lock, preflight, then drop; positions lock=%d preflight=%d guardEnd=%d drop=%d", lockAt, preflightAt, guardEnd, dropAt)
	}
	lock := down[lockAt:preflightAt]
	if !strings.Contains(lock, "access exclusive mode") {
		t.Fatal("account-feed rollback must hold ACCESS EXCLUSIVE locks through its preflight and DDL")
	}
	guard := down[preflightAt:guardEnd]
	for _, fragment := range []string{
		"from public.account_feed_audits limit 1",
		"from public.account_feeds limit 1",
		"rollback refused: account feed configuration or audit history exists",
		"using errcode = '55000'",
	} {
		if !strings.Contains(guard, fragment) {
			t.Errorf("account-feed rollback preflight is missing %q", fragment)
		}
	}
	if strings.Contains(strings.ToUpper(down), " CASCADE") {
		t.Fatal("account-feed rollback must not cascade through dependent data")
	}
}

func TestGuardedRollbackSourcesLockBeforePreflightAndDDL(t *testing.T) {
	tests := []struct {
		name       string
		migration  string
		lockTables []string
		guard      []string
		firstDDL   string
		ddlTargets []string
	}{
		{
			name:       "0070 retained review metadata",
			migration:  openClawSessionReviewMigration,
			lockTables: []string{"public.openclaw_gateway_session_receipts"},
			guard:      []string{"status = 'needs_review'", "review_reason is distinct from ''", "review_at is not null", "retained review metadata exists"},
			firstDDL:   "drop index if exists public.idx_openclaw_gateway_session_receipts_review",
			ddlTargets: []string{"alter table public.openclaw_gateway_session_receipts"},
		},
		{
			name:      "0095 Trello durable state",
			migration: trelloResumableSyncMigration,
			lockTables: []string{
				"public.trello_sync_states", "public.trello_sync_pages",
				"public.trello_action_receipts", "public.source_sync_jobs",
			},
			guard: []string{
				"from public.trello_sync_states", "from public.trello_sync_pages",
				"from public.trello_action_receipts", "progress_phase is not null",
				"progress_pages is distinct from 0", "progress_records is distinct from 0",
				"progress_message is not null", "sync state, action receipts, or progress data exists",
			},
			firstDDL: "drop table if exists public.trello_action_receipts",
		},
		{
			name:       "0098 active artifact claims",
			migration:  openClawArtifactClaimMigration,
			lockTables: []string{"public.openclaw_gateway_artifact_collections"},
			guard: []string{
				"claim_token is not null", "claimed_at is not null",
				"claim tokens are retained",
			},
			firstDDL: "drop index if exists public.idx_openclaw_artifact_collections_claimed",
			ddlTargets: []string{
				"drop index if exists public.idx_openclaw_artifact_collections_claim_token",
				"alter table public.openclaw_gateway_artifact_collections",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			down := readRollbackMigration(t, test.migration)
			lockAt := strings.Index(down, "lock table")
			preflightAt := strings.Index(down, "do $$")
			guardEnd := strings.Index(down, "end $$;")
			ddlAt := strings.Index(down, test.firstDDL)
			if lockAt < 0 || preflightAt < lockAt || guardEnd < preflightAt || ddlAt < guardEnd {
				t.Fatalf("rollback ordering must be lock, preflight, then DDL; positions lock=%d preflight=%d guardEnd=%d ddl=%d", lockAt, preflightAt, guardEnd, ddlAt)
			}
			lock := down[lockAt:preflightAt]
			if !strings.Contains(lock, "access exclusive mode") {
				t.Fatal("rollback must hold ACCESS EXCLUSIVE table locks through its transaction")
			}
			for _, table := range test.lockTables {
				if !strings.Contains(lock, table) {
					t.Errorf("rollback does not lock %s before its preflight", table)
				}
			}
			guard := down[preflightAt:guardEnd]
			for _, fragment := range test.guard {
				if !strings.Contains(guard, fragment) {
					t.Errorf("rollback preflight is missing %q", fragment)
				}
			}
			for _, target := range test.ddlTargets {
				if !strings.Contains(down, target) {
					t.Errorf("rollback DDL target is not schema-qualified: missing %q", target)
				}
			}
			if strings.Contains(strings.ToUpper(down), " CASCADE") {
				t.Fatal("rollback must not cascade through protected records")
			}
		})
	}

	trelloDown := readRollbackMigration(t, trelloResumableSyncMigration)
	if strings.Contains(trelloDown, "drop index if exists public.ux_source_raw_items_source_external") {
		t.Fatal("0095 rollback must preserve the potentially pre-existing shared source-item unique index")
	}
}

func TestOpenClawSessionReviewRollbackPreservesTerminalReviewMetadata(t *testing.T) {
	t.Run("retained metadata blocks rollback", func(t *testing.T) {
		db := openIsolatedMigrationDatabase(t)
		execRollbackFixture(t, db, `
			CREATE TABLE public.openclaw_gateway_session_receipts (
				status text NOT NULL CHECK (status IN ('admitted', 'needs_review', 'terminal')),
				review_reason text NOT NULL DEFAULT '',
				review_at timestamptz
			)`)
		execRollbackFixture(t, db, `CREATE INDEX idx_openclaw_gateway_session_receipts_review
			ON public.openclaw_gateway_session_receipts (review_at ASC)
			WHERE status = 'needs_review'`)
		files := applyRollbackFixtureMigration(t, db, openClawSessionReviewMigration)
		execRollbackFixture(t, db, `INSERT INTO public.openclaw_gateway_session_receipts
			(status, review_reason, review_at) VALUES ('terminal', 'reviewed and resolved', now())`)

		err := infra.RollbackMigration(db, files, "pre", openClawSessionReviewMigration)
		if err == nil || !strings.Contains(strings.ToLower(err.Error()), "retained review metadata exists") {
			t.Fatalf("rollback error = %v, want retained-review-metadata refusal", err)
		}
		assertRollbackColumn(t, db, "openclaw_gateway_session_receipts", "review_reason", true)
		assertRollbackColumn(t, db, "openclaw_gateway_session_receipts", "review_at", true)
		assertRollbackRelation(t, db, "idx_openclaw_gateway_session_receipts_review", true)
		assertRollbackMigrationLedger(t, db, openClawSessionReviewMigration, true)
		var reason string
		if err := db.Raw(`SELECT review_reason FROM public.openclaw_gateway_session_receipts WHERE status = 'terminal'`).Row().Scan(&reason); err != nil || reason != "reviewed and resolved" {
			t.Fatalf("retained terminal review reason = %q, err=%v", reason, err)
		}
	})

	t.Run("empty rollback removes review schema", func(t *testing.T) {
		db := openIsolatedMigrationDatabase(t)
		execRollbackFixture(t, db, `CREATE TABLE public.openclaw_gateway_session_receipts (
			status text NOT NULL CHECK (status IN ('admitted', 'needs_review', 'terminal')),
			review_reason text NOT NULL DEFAULT '', review_at timestamptz)`)
		execRollbackFixture(t, db, `CREATE INDEX idx_openclaw_gateway_session_receipts_review
			ON public.openclaw_gateway_session_receipts (review_at ASC) WHERE status = 'needs_review'`)
		files := applyRollbackFixtureMigration(t, db, openClawSessionReviewMigration)
		if err := infra.RollbackMigration(db, files, "pre", openClawSessionReviewMigration); err != nil {
			t.Fatalf("rollback empty review migration: %v", err)
		}
		assertRollbackColumn(t, db, "openclaw_gateway_session_receipts", "review_reason", false)
		assertRollbackColumn(t, db, "openclaw_gateway_session_receipts", "review_at", false)
		assertRollbackMigrationLedger(t, db, openClawSessionReviewMigration, false)
	})
}

func TestTrelloResumableRollbackRefusesPersistedStateAndPreservesSharedIndex(t *testing.T) {
	blockers := []struct {
		name   string
		insert string
	}{
		{name: "sync state", insert: `INSERT INTO public.trello_sync_states DEFAULT VALUES`},
		{name: "sync page", insert: `INSERT INTO public.trello_sync_pages DEFAULT VALUES`},
		{name: "action receipt", insert: `INSERT INTO public.trello_action_receipts DEFAULT VALUES`},
		{name: "progress data", insert: `INSERT INTO public.source_sync_jobs (progress_phase) VALUES ('cards')`},
	}
	for _, blocker := range blockers {
		t.Run(blocker.name, func(t *testing.T) {
			db := openIsolatedMigrationDatabase(t)
			createTrelloRollbackFixture(t, db)
			files := applyRollbackFixtureMigration(t, db, trelloResumableSyncMigration)
			execRollbackFixture(t, db, blocker.insert)

			err := infra.RollbackMigration(db, files, "pre", trelloResumableSyncMigration)
			if err == nil || !strings.Contains(strings.ToLower(err.Error()), "sync state, action receipts, or progress data exists") {
				t.Fatalf("rollback error = %v, want persisted Trello-state refusal", err)
			}
			for _, table := range []string{"trello_sync_states", "trello_sync_pages", "trello_action_receipts"} {
				assertRollbackRelation(t, db, table, true)
			}
			assertRollbackColumn(t, db, "source_sync_jobs", "progress_phase", true)
			assertRollbackRelation(t, db, "ux_source_raw_items_source_external", true)
			assertRollbackMigrationLedger(t, db, trelloResumableSyncMigration, true)
		})
	}

	t.Run("empty rollback retains shared index", func(t *testing.T) {
		db := openIsolatedMigrationDatabase(t)
		createTrelloRollbackFixture(t, db)
		files := applyRollbackFixtureMigration(t, db, trelloResumableSyncMigration)
		if err := infra.RollbackMigration(db, files, "pre", trelloResumableSyncMigration); err != nil {
			t.Fatalf("rollback empty Trello migration: %v", err)
		}
		for _, table := range []string{"trello_sync_states", "trello_sync_pages", "trello_action_receipts"} {
			assertRollbackRelation(t, db, table, false)
		}
		assertRollbackColumn(t, db, "source_sync_jobs", "progress_phase", false)
		assertRollbackRelation(t, db, "ux_source_raw_items_source_external", true)
		assertRollbackMigrationLedger(t, db, trelloResumableSyncMigration, false)
	})
}

func TestOpenClawArtifactClaimRollbackRefusesRetainedTokens(t *testing.T) {
	t.Run("claim token blocks rollback", func(t *testing.T) {
		db := openIsolatedMigrationDatabase(t)
		createArtifactClaimRollbackFixture(t, db)
		files := applyRollbackFixtureMigration(t, db, openClawArtifactClaimMigration)
		execRollbackFixture(t, db, `INSERT INTO public.openclaw_gateway_artifact_collections
			(execution_reference, claimed_at, claim_token) VALUES
			('claim-regression', now(), '00000000-0000-0000-0000-000000000001')`)

		err := infra.RollbackMigration(db, files, "pre", openClawArtifactClaimMigration)
		if err == nil || !strings.Contains(strings.ToLower(err.Error()), "claim tokens are retained") {
			t.Fatalf("rollback error = %v, want retained-claim refusal", err)
		}
		assertRollbackColumn(t, db, "openclaw_gateway_artifact_collections", "claim_token", true)
		assertRollbackColumn(t, db, "openclaw_gateway_artifact_collections", "claimed_at", true)
		assertRollbackRelation(t, db, "idx_openclaw_artifact_collections_claim_token", true)
		assertRollbackMigrationLedger(t, db, openClawArtifactClaimMigration, true)
	})

	t.Run("empty rollback removes claim schema", func(t *testing.T) {
		db := openIsolatedMigrationDatabase(t)
		createArtifactClaimRollbackFixture(t, db)
		files := applyRollbackFixtureMigration(t, db, openClawArtifactClaimMigration)
		if err := infra.RollbackMigration(db, files, "pre", openClawArtifactClaimMigration); err != nil {
			t.Fatalf("rollback empty artifact claim migration: %v", err)
		}
		assertRollbackColumn(t, db, "openclaw_gateway_artifact_collections", "claim_token", false)
		assertRollbackColumn(t, db, "openclaw_gateway_artifact_collections", "claimed_at", false)
		assertRollbackRelation(t, db, "idx_openclaw_artifact_collections_claim_token", false)
		assertRollbackMigrationLedger(t, db, openClawArtifactClaimMigration, false)
	})
}

func createTrelloRollbackFixture(t *testing.T, db *gorm.DB) {
	t.Helper()
	execRollbackFixture(t, db, `CREATE TABLE public.source_sync_jobs (
		id bigint GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
		progress_phase varchar(40), progress_pages integer NOT NULL DEFAULT 0,
		progress_records integer NOT NULL DEFAULT 0, progress_message varchar(512))`)
	execRollbackFixture(t, db, `CREATE TABLE public.source_raw_items (source_id uuid NOT NULL, external_id text NOT NULL)`)
	execRollbackFixture(t, db, `CREATE UNIQUE INDEX ux_source_raw_items_source_external
		ON public.source_raw_items (source_id, external_id)`)
	execRollbackFixture(t, db, `CREATE TABLE public.trello_sync_states (marker integer)`)
	execRollbackFixture(t, db, `CREATE TABLE public.trello_sync_pages (marker integer)`)
	execRollbackFixture(t, db, `CREATE TABLE public.trello_action_receipts (marker integer)`)
}

func createArtifactClaimRollbackFixture(t *testing.T, db *gorm.DB) {
	t.Helper()
	execRollbackFixture(t, db, `CREATE TABLE public.openclaw_gateway_artifact_collections (
		execution_reference varchar(64) PRIMARY KEY,
		attempts integer NOT NULL DEFAULT 0,
		next_attempt_at timestamptz,
		captured_at timestamptz,
		descriptor_count integer,
		claimed_at timestamptz,
		claim_token uuid,
		CONSTRAINT chk_openclaw_artifact_claim_pair CHECK ((claimed_at IS NULL) = (claim_token IS NULL)))`)
	execRollbackFixture(t, db, `CREATE INDEX idx_openclaw_artifact_collections_claimed
		ON public.openclaw_gateway_artifact_collections (claimed_at)
		WHERE claimed_at IS NOT NULL AND captured_at IS NULL AND attempts < 8`)
	execRollbackFixture(t, db, `CREATE UNIQUE INDEX idx_openclaw_artifact_collections_claim_token
		ON public.openclaw_gateway_artifact_collections (claim_token) WHERE claim_token IS NOT NULL`)
}

func applyRollbackFixtureMigration(t *testing.T, db *gorm.DB, version string) fs.FS {
	t.Helper()
	name := strings.TrimPrefix(version, "pre/")
	down, err := migrations.Files.ReadFile("pre/" + name + ".down.sql")
	if err != nil {
		t.Fatalf("read rollback migration %s: %v", version, err)
	}
	files := fstest.MapFS{
		"pre":                       &fstest.MapFile{Mode: fs.ModeDir},
		"pre/" + name + ".up.sql":   {Data: []byte("SELECT 1;"), Mode: 0o600},
		"pre/" + name + ".down.sql": {Data: down, Mode: 0o600},
	}
	if count, err := infra.ApplyMigrations(db, files, "pre"); err != nil || count != 1 {
		t.Fatalf("record isolated rollback fixture migration: applied=%d err=%v", count, err)
	}
	return files
}

func execRollbackFixture(t *testing.T, db *gorm.DB, sql string, args ...any) {
	t.Helper()
	if err := db.Exec(sql, args...).Error; err != nil {
		t.Fatalf("prepare isolated rollback fixture: %v", err)
	}
}

func assertRollbackColumn(t *testing.T, db *gorm.DB, table, column string, want bool) {
	t.Helper()
	var exists bool
	if err := db.Raw(`SELECT EXISTS (
		SELECT 1 FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = ? AND column_name = ?
	)`, table, column).Row().Scan(&exists); err != nil {
		t.Fatalf("check column %s.%s: %v", table, column, err)
	}
	if exists != want {
		t.Fatalf("column %s.%s exists=%t, want %t", table, column, exists, want)
	}
}

func assertRollbackRelation(t *testing.T, db *gorm.DB, relation string, want bool) {
	t.Helper()
	var exists bool
	if err := db.Raw(`SELECT to_regclass(?) IS NOT NULL`, "public."+relation).Row().Scan(&exists); err != nil {
		t.Fatalf("check relation %s: %v", relation, err)
	}
	if exists != want {
		t.Fatalf("relation %s exists=%t, want %t", relation, exists, want)
	}
}

func assertRollbackMigrationLedger(t *testing.T, db *gorm.DB, version string, want bool) {
	t.Helper()
	var applied bool
	if err := db.Raw(`SELECT EXISTS (SELECT 1 FROM public.schema_migrations WHERE version = ?)`, version).Row().Scan(&applied); err != nil {
		t.Fatalf("check schema migration ledger for %s: %v", version, err)
	}
	if applied != want {
		t.Fatalf("migration %s applied=%t, want %t", version, applied, want)
	}
}

func readRollbackMigration(t *testing.T, version string) string {
	t.Helper()
	return strings.Join(strings.Fields(strings.ToLower(readRawRollbackMigration(t, version))), " ")
}

func readRawRollbackMigration(t *testing.T, version string) string {
	t.Helper()
	name := strings.TrimPrefix(version, "pre/")
	data, err := migrations.Files.ReadFile("pre/" + name + ".down.sql")
	if err != nil {
		t.Fatalf("read rollback migration %s: %v", version, err)
	}
	return strings.ToLower(strings.ReplaceAll(string(data), "\r\n", "\n"))
}

func rollbackSQLWithoutLineComments(sql string) string {
	var lines []string
	for _, line := range strings.Split(sql, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "--") {
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "\n")
}
