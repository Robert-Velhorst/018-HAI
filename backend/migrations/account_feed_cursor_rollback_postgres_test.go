package migrations_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/infra"
	"automation-hub-backend/internal/pgtestguard"
	"github.com/google/uuid"
)

// Rollback must refuse an active claim even before it has produced a cursor.
func TestAccountFeedCursorRollbackRefusesActiveSyncClaim(t *testing.T) {
	pgtestguard.RequireDedicatedPostgresTestDSN(t, "HAI_TEST_DATABASE_DSN", "hai_migration_runner_test")
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()

	db := openIsolatedMigrationDatabase(t).WithContext(ctx)
	files := migrationFilesThrough(t, "pre/0128_account_feed_sync_cursor")
	if _, err := infra.ApplyMigrations(db, files, "pre"); err != nil {
		t.Fatalf("apply migrations through account-feed cursor: %v", err)
	}

	feedID := uuid.New()
	claimToken := uuid.New()
	if err := db.Exec(`
		INSERT INTO public.account_feeds (
			id, owner_user_id, workspace_id, name, provider, source_type, enabled,
			sync_token, sync_started_at
		) VALUES (?, 'owner-a', 'workspace-a', 'active claim', 'generic_json_feed',
			'local_json_file', true, ?, now())`, feedID, claimToken).Error; err != nil {
		t.Fatalf("seed active account-feed claim: %v", err)
	}

	err := infra.RollbackMigration(db, files, "pre", "pre/0128_account_feed_sync_cursor")
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "rollback refused: account feed cursor or active sync claim exists") {
		t.Fatalf("rollback error=%v; want active-sync refusal", err)
	}

	var cursor string
	var storedToken uuid.UUID
	if err := db.Raw("SELECT sync_cursor, sync_token FROM public.account_feeds WHERE id = ?", feedID).Row().Scan(&cursor, &storedToken); err != nil {
		t.Fatalf("read feed state after refused rollback: %v", err)
	}
	if cursor != "" || storedToken != claimToken {
		t.Fatalf("refused rollback changed feed state: cursor=%q token=%s", cursor, storedToken)
	}
}
