//go:build integration

package migrations_test

import (
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/infra"
	"automation-hub-backend/migrations"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestTrelloWebhookReconciliationGenerationMigrationBackfillAndRollback(t *testing.T) {
	db := openIsolatedMigrationDatabase(t)
	prior := migrationFilesThrough(t, "pre/0108_host_runtime_start_intent")
	if _, err := infra.ApplyMigrations(db, prior, "pre"); err != nil {
		t.Fatalf("apply migrations through 0108: %v", err)
	}

	ownerlessSourceID := uuid.New()
	if err := db.Exec(`
		INSERT INTO public.connected_sources (id, owner_identity, connector_key, name, category, enabled, status)
		VALUES (?, NULL, 'trello', 'ownerless migration fixture', 'project_board', true, 'active')`, ownerlessSourceID).Error; err != nil {
		t.Fatalf("create ownerless source fixture: %v", err)
	}
	firstReceiptID := insertTrelloWebhookMigrationFixture(t, db, ownerlessSourceID,
		"111111111111111111111111", "updateCard", "queued", time.Now().UTC().Add(-3*time.Minute))
	if _, err := infra.ApplyMigrations(db, migrations.Files, "pre"); err == nil ||
		!strings.Contains(err.Error(), "pending Trello board webhook receipts require a non-empty connected-source owner_identity") {
		t.Fatalf("ownerless open receipt migration error = %v, want clear preflight refusal", err)
	}
	if db.Migrator().HasColumn("trello_webhook_receipts", "reconciliation_generation") ||
		db.Migrator().HasTable("trello_webhook_reconciliation_states") {
		t.Fatal("failed preflight left part of migration 0109 applied")
	}
	var ledgerCount int64
	if err := db.Table("schema_migrations").Where("version = ?", "0109_trello_webhook_reconciliation_generations").Count(&ledgerCount).Error; err != nil {
		t.Fatalf("check migration ledger after preflight refusal: %v", err)
	}
	if ledgerCount != 0 {
		t.Fatal("failed preflight was recorded as an applied migration")
	}

	owner := "trello-migration-owner-" + uuid.NewString()
	if err := db.Exec("UPDATE public.connected_sources SET owner_identity = ? WHERE id = ?", owner, ownerlessSourceID).Error; err != nil {
		t.Fatalf("repair owner identity in isolated fixture: %v", err)
	}
	secondReceiptID := insertTrelloWebhookMigrationFixture(t, db, ownerlessSourceID,
		"222222222222222222222222", "updateCard", "dispatched", time.Now().UTC().Add(-2*time.Minute))
	commentReceiptID := insertTrelloWebhookMigrationFixture(t, db, ownerlessSourceID,
		"333333333333333333333333", "commentCard", "queued", time.Now().UTC().Add(-time.Minute))
	completedReceiptID := insertTrelloWebhookMigrationFixture(t, db, ownerlessSourceID,
		"444444444444444444444444", "updateCard", "completed", time.Now().UTC())
	boardID := "aaaaaaaaaaaaaaaaaaaaaaaa"
	syncTime := time.Now().UTC().Add(-time.Hour)
	if err := db.Exec(`
		INSERT INTO public.trello_sync_states (
			source_id, owner_identity, board_id, generation, phase, cycle_started_at, last_successful_at, updated_at
		) VALUES (?, ?, ?, 6, 'idle', ?, ?, ?)`, ownerlessSourceID, owner, boardID, syncTime, syncTime, syncTime).Error; err != nil {
		t.Fatalf("create prior Trello sync state: %v", err)
	}

	reconciliationMigration := migrationFilesThrough(t, "pre/0109_trello_webhook_reconciliation_generations")
	if applied, err := infra.ApplyMigrations(db, reconciliationMigration, "pre"); err != nil || applied != 1 {
		t.Fatalf("apply reconciliation migration = (%d, %v), want (1, nil)", applied, err)
	}
	if reapplied, err := infra.ApplyMigrations(db, reconciliationMigration, "pre"); err != nil || reapplied != 0 {
		t.Fatalf("reapply reconciliation migration = (%d, %v), want (0, nil)", reapplied, err)
	}
	var firstGeneration, secondGeneration, commentGeneration, completedGeneration int64
	for _, receipt := range []struct {
		id   uuid.UUID
		want *int64
	}{
		{firstReceiptID, &firstGeneration}, {secondReceiptID, &secondGeneration},
		{commentReceiptID, &commentGeneration}, {completedReceiptID, &completedGeneration},
	} {
		if err := db.Raw("SELECT reconciliation_generation FROM public.trello_webhook_receipts WHERE id = ?", receipt.id).Scan(receipt.want).Error; err != nil {
			t.Fatalf("read backfilled generation for %s: %v", receipt.id, err)
		}
	}
	if firstGeneration != 1 || secondGeneration != 2 || commentGeneration != 0 || completedGeneration != 0 {
		t.Fatalf("backfilled generations = board %d/%d, comment %d, completed %d; want board 1/2 and excluded rows 0/0",
			firstGeneration, secondGeneration, commentGeneration, completedGeneration)
	}
	var requested, completed, required int64
	var stateOwner string
	if err := db.Raw(`
		SELECT owner_identity, requested_generation, completed_generation, required_trello_generation
		FROM public.trello_webhook_reconciliation_states WHERE source_id = ?`, ownerlessSourceID).
		Row().Scan(&stateOwner, &requested, &completed, &required); err != nil {
		t.Fatalf("read reconciliation state backfill: %v", err)
	}
	if stateOwner != owner || requested != 2 || completed != 0 || required != 7 {
		t.Fatalf("reconciliation state owner/requested/completed/required = %q/%d/%d/%d; want %q/2/0/7",
			stateOwner, requested, completed, required, owner)
	}
	for _, invalidOwner := range []string{"", "  ", "\t\r\n"} {
		if err := db.Exec(`UPDATE public.trello_webhook_reconciliation_states
			SET owner_identity = ? WHERE source_id = ?`, invalidOwner, ownerlessSourceID).Error; err == nil {
			t.Fatalf("reconciliation state accepted blank owner identity %q", invalidOwner)
		} else if invalidOwner != "" && !strings.Contains(err.Error(), "ck_trello_reconciliation_owner_identity") {
			t.Fatalf("blank owner identity %q failed for unexpected reason: %v", invalidOwner, err)
		}
	}

	version := "pre/0109_trello_webhook_reconciliation_generations"
	if err := infra.RollbackMigration(db, reconciliationMigration, "pre", version); err == nil ||
		!strings.Contains(err.Error(), "rollback refused: Trello reconciliation generation history or pending work would be lost") {
		t.Fatalf("rollback with backfilled evidence error = %v, want data-loss refusal", err)
	}
	if !db.Migrator().HasColumn("trello_webhook_receipts", "reconciliation_generation") ||
		!db.Migrator().HasTable("trello_webhook_reconciliation_states") {
		t.Fatal("refused rollback removed reconciliation schema")
	}

	if err := db.Exec("DELETE FROM public.trello_webhook_receipts WHERE source_id = ?", ownerlessSourceID).Error; err != nil {
		t.Fatalf("remove isolated webhook receipts before empty rollback: %v", err)
	}
	if err := db.Exec("DELETE FROM public.trello_sync_states WHERE source_id = ?", ownerlessSourceID).Error; err != nil {
		t.Fatalf("remove isolated Trello sync state before empty rollback: %v", err)
	}
	if err := db.Exec("DELETE FROM public.connected_sources WHERE id = ?", ownerlessSourceID).Error; err != nil {
		t.Fatalf("remove isolated source before empty rollback: %v", err)
	}
	if err := infra.RollbackMigration(db, reconciliationMigration, "pre", version); err != nil {
		t.Fatalf("roll back empty reconciliation migration: %v", err)
	}
	if db.Migrator().HasColumn("trello_webhook_receipts", "reconciliation_generation") ||
		db.Migrator().HasTable("trello_webhook_reconciliation_states") {
		t.Fatal("empty rollback left reconciliation schema behind")
	}
}

func TestTrelloWebhookReconciliationGenerationMigrationLocksOutConcurrentPendingReceipt(t *testing.T) {
	db := openIsolatedMigrationDatabase(t)
	prior := migrationFilesThrough(t, "pre/0108_host_runtime_start_intent")
	if _, err := infra.ApplyMigrations(db, prior, "pre"); err != nil {
		t.Fatalf("apply migrations through 0108: %v", err)
	}

	sourceID := uuid.New()
	if err := db.Exec(`
		INSERT INTO public.connected_sources (id, owner_identity, connector_key, name, category, enabled, status)
		VALUES (?, NULL, 'trello', 'concurrent ownerless source fixture', 'project_board', true, 'active')`, sourceID).Error; err != nil {
		t.Fatalf("create ownerless concurrent source fixture: %v", err)
	}
	var sourceTableOID uint32
	if err := db.Raw("SELECT 'public.connected_sources'::regclass::oid").Scan(&sourceTableOID).Error; err != nil {
		t.Fatalf("resolve connected source table OID: %v", err)
	}

	// Match the production writer's source-first lock order. Start the migration
	// before inserting the receipt so an inverted table-lock order cannot pass.
	writer := db.Begin()
	if writer.Error != nil {
		t.Fatalf("begin concurrent receipt transaction: %v", writer.Error)
	}
	writerOpen := true
	defer func() {
		if writerOpen {
			_ = writer.Rollback().Error
		}
	}()
	if err := writer.Exec("SET LOCAL lock_timeout = '5s'").Error; err != nil {
		t.Fatalf("bound concurrent writer lock waits: %v", err)
	}
	var lockedSourceID uuid.UUID
	if err := writer.Raw("SELECT id FROM public.connected_sources WHERE id = ? FOR UPDATE", sourceID).
		Row().Scan(&lockedSourceID); err != nil {
		t.Fatalf("lock source row before receipt insert: %v", err)
	}

	type migrationResult struct {
		applied int
		err     error
	}
	done := make(chan migrationResult, 1)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		applied, err := infra.ApplyMigrations(db, migrations.Files, "pre")
		done <- migrationResult{applied: applied, err: err}
	}()
	defer func() {
		if writerOpen {
			_ = writer.Rollback().Error
			writerOpen = false
		}
		select {
		case <-finished:
		case <-time.After(10 * time.Second):
			t.Error("migration goroutine did not finish before isolated database cleanup")
		}
	}()

	locked, lockErr := waitForTrelloMigrationExclusiveLock(db, sourceTableOID, finished, 10*time.Second)
	if lockErr != nil {
		_ = writer.Rollback()
		writerOpen = false
		select {
		case <-done:
		case <-time.After(10 * time.Second):
		}
		t.Fatalf("observe migration's source-table lock request: %v", lockErr)
	}
	if !locked {
		_ = writer.Rollback()
		writerOpen = false
		select {
		case result := <-done:
			t.Fatalf("migration completed without waiting for the open receipt transaction: applied=%d err=%v", result.applied, result.err)
		case <-time.After(10 * time.Second):
			t.Fatal("migration neither requested the source-table lock nor completed")
		}
	}

	select {
	case result := <-done:
		_ = writer.Rollback()
		writerOpen = false
		t.Fatalf("migration did not wait for the receipt transaction: applied=%d err=%v", result.applied, result.err)
	default:
	}
	insertTrelloWebhookMigrationFixture(t, writer, sourceID,
		"555555555555555555555555", "updateCard", "queued", time.Now().UTC())
	if err := writer.Commit().Error; err != nil {
		writerOpen = false
		t.Fatalf("commit concurrent pending receipt: %v", err)
	}
	writerOpen = false

	select {
	case result := <-done:
		if result.err == nil || !strings.Contains(result.err.Error(), "pending Trello board webhook receipts require a non-empty connected-source owner_identity") {
			t.Fatalf("migration result after concurrent receipt commit = (applied %d, %v), want owner preflight refusal", result.applied, result.err)
		}
		if db.Migrator().HasColumn("trello_webhook_receipts", "reconciliation_generation") ||
			db.Migrator().HasTable("trello_webhook_reconciliation_states") {
			t.Fatal("concurrent owner preflight refusal left migration 0109 partially applied")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("migration did not finish after the concurrent receipt transaction committed")
	}
}

func waitForTrelloMigrationExclusiveLock(db *gorm.DB, tableOID uint32, finished <-chan struct{}, timeout time.Duration) (bool, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		select {
		case <-finished:
			return false, nil
		default:
		}
		var waiting bool
		if err := db.Raw(`
			SELECT EXISTS (
				SELECT 1 FROM pg_locks
				WHERE locktype = 'relation'
				  AND relation = ?
				  AND mode = 'AccessExclusiveLock'
				  AND NOT granted
			)`, tableOID).Row().Scan(&waiting); err != nil {
			return false, err
		}
		if waiting {
			return true, nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false, nil
}

func insertTrelloWebhookMigrationFixture(t *testing.T, db *gorm.DB, sourceID uuid.UUID, actionID, actionType, status string, receivedAt time.Time) uuid.UUID {
	t.Helper()
	jobID, receiptID := uuid.New(), uuid.New()
	if err := db.Exec(`
		INSERT INTO public.durable_jobs (id, queue, kind, payload, status, attempts, max_attempts, run_at, created_at, updated_at)
		VALUES (?, 'source', 'source.trello_webhook', '{}'::jsonb, 'pending', 0, 3, ?, ?, ?)`,
		jobID, receivedAt, receivedAt, receivedAt).Error; err != nil {
		t.Fatalf("create webhook durable job fixture: %v", err)
	}
	if err := db.Exec(`
		INSERT INTO public.trello_webhook_receipts (
			id, source_id, action_id, board_id, card_id, action_type, occurred_at, fingerprint,
			durable_job_id, status, received_at, updated_at
		) VALUES (?, ?, ?, 'bbbbbbbbbbbbbbbbbbbbbbbb', '', ?, ?, repeat('a', 64), ?, ?, ?, ?)`,
		receiptID, sourceID, actionID, actionType, receivedAt, jobID, status, receivedAt, receivedAt).Error; err != nil {
		t.Fatalf("create webhook receipt fixture: %v", err)
	}
	return receiptID
}
