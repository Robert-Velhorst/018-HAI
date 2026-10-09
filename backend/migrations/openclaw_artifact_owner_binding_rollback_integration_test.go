package migrations_test

import (
	"bytes"
	"io/fs"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/infra"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

const openClawArtifactOwnerBindingMigration = "pre/0093_openclaw_artifact_owner_binding"

func TestOpenClawArtifactOwnerBindingRollbackRefusesRetainedArtifacts(t *testing.T) {
	db := openIsolatedMigrationDatabase(t)
	files := migrationFilesThrough(t, openClawArtifactOwnerBindingMigration)
	applyOpenClawArtifactOwnerBindingMigration(t, db, files)
	seedOpenClawRetainedArtifactForOwnerBindingRollback(t, db)

	err := infra.RollbackMigration(db, files, "pre", openClawArtifactOwnerBindingMigration)
	const wantError = "refusing to remove OpenClaw artifact owner-binding constraints and indexes while retained artifacts exist"
	if err == nil || !strings.Contains(err.Error(), wantError) {
		t.Fatalf("rollback error = %v, want it to contain %q", err, wantError)
	}
	assertOpenClawOwnerBindingRollbackState(t, db, 2, 2, true)
	var retainedCount int64
	if err := db.Table("public.openclaw_retained_artifacts").Count(&retainedCount).Error; err != nil || retainedCount != 1 {
		t.Fatalf("retained rows after refused rollback = %d, %v; want one preserved row", retainedCount, err)
	}
}

func TestOpenClawArtifactOwnerBindingRollbackAllowsEmptyRetentionTable(t *testing.T) {
	db := openIsolatedMigrationDatabase(t)
	files := migrationFilesThrough(t, openClawArtifactOwnerBindingMigration)
	applyOpenClawArtifactOwnerBindingMigration(t, db, files)

	if err := infra.RollbackMigration(db, files, "pre", openClawArtifactOwnerBindingMigration); err != nil {
		t.Fatalf("rollback with no retained artifacts: %v", err)
	}
	assertOpenClawOwnerBindingRollbackState(t, db, 0, 0, false)
}

func TestOpenClawArtifactOwnerBindingRollbackSerializesWithConcurrentRetain(t *testing.T) {
	db := openIsolatedMigrationDatabase(t)
	files := migrationFilesThrough(t, openClawArtifactOwnerBindingMigration)
	applyOpenClawArtifactOwnerBindingMigration(t, db, files)
	fixture := createOpenClawArtifactOwnerBindingFixture(t, db)

	writer := db.Begin()
	if writer.Error != nil {
		t.Fatalf("begin concurrent retention transaction: %v", writer.Error)
	}
	writerOpen := true
	defer func() {
		if writerOpen {
			_ = writer.Rollback().Error
		}
	}()
	if err := insertOpenClawRetainedArtifact(writer, fixture); err != nil {
		t.Fatalf("insert concurrent retained artifact: %v", err)
	}

	rollbackDone := make(chan error, 1)
	go func() {
		rollbackDone <- infra.RollbackMigration(db, files, "pre", openClawArtifactOwnerBindingMigration)
	}()
	deadline := time.Now().Add(10 * time.Second)
	blockedOnTable := false
	for time.Now().Before(deadline) {
		var waiting int64
		if err := db.Raw(`SELECT COUNT(*) FROM pg_locks AS l
			JOIN pg_class AS c ON c.oid = l.relation
			JOIN pg_namespace AS n ON n.oid = c.relnamespace
			WHERE n.nspname = 'public'
			  AND c.relname = 'openclaw_retained_artifacts'
			  AND l.mode = 'AccessExclusiveLock' AND NOT l.granted`).Scan(&waiting).Error; err != nil {
			_ = writer.Rollback().Error
			writerOpen = false
			<-rollbackDone
			t.Fatalf("inspect waiting owner-binding rollback lock: %v", err)
		}
		if waiting > 0 {
			blockedOnTable = true
			break
		}
		select {
		case err := <-rollbackDone:
			_ = writer.Rollback().Error
			writerOpen = false
			t.Fatalf("owner-binding rollback did not wait for concurrent retention: %v", err)
		default:
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !blockedOnTable {
		_ = writer.Rollback().Error
		writerOpen = false
		<-rollbackDone
		t.Fatal("owner-binding rollback never requested an exclusive lock while retention held a write transaction")
	}
	if err := writer.Commit().Error; err != nil {
		writerOpen = false
		t.Fatalf("commit concurrent retained artifact: %v", err)
	}
	writerOpen = false

	err := <-rollbackDone
	const wantError = "refusing to remove OpenClaw artifact owner-binding constraints and indexes while retained artifacts exist"
	if err == nil || !strings.Contains(err.Error(), wantError) {
		t.Fatalf("rollback error after concurrent retention committed = %v, want it to contain %q", err, wantError)
	}
	assertOpenClawOwnerBindingRollbackState(t, db, 2, 2, true)
	var retainedCount int64
	if err := db.Table("public.openclaw_retained_artifacts").Count(&retainedCount).Error; err != nil || retainedCount != 1 {
		t.Fatalf("concurrent retained artifact after refused rollback = %d, %v; want one preserved row", retainedCount, err)
	}
}

func applyOpenClawArtifactOwnerBindingMigration(t *testing.T, db *gorm.DB, files fs.FS) {
	t.Helper()
	count, err := infra.ApplyMigrations(db, files, "pre")
	if err != nil {
		t.Fatalf("apply migrations through 0093: %v", err)
	}
	if count == 0 {
		t.Fatal("migration chain through 0093 applied no migrations")
	}
	var applied int64
	if err := db.Table("schema_migrations").Where("version = ?", openClawArtifactOwnerBindingMigration).Count(&applied).Error; err != nil || applied != 1 {
		t.Fatalf("owner-binding migration ledger count = %d, %v; want one", applied, err)
	}
}

type openClawOwnerBindingFixture struct {
	owner              string
	executionReference string
	eventID            uuid.UUID
	digest             string
	contentSHA         string
	now                time.Time
}

func createOpenClawArtifactOwnerBindingFixture(t *testing.T, db *gorm.DB) openClawOwnerBindingFixture {
	t.Helper()
	owner := "owner-binding-rollback-" + uuid.NewString()
	executionReference := "ocgw:v2:" + uuid.NewString()
	taskID := "owner-binding-task-" + uuid.NewString()
	eventID := uuid.New()
	automationID := uuid.New()
	digest := strings.Repeat("d", 64)
	contentSHA := strings.Repeat("a", 64)
	now := time.Now().UTC().Truncate(time.Microsecond)

	if err := db.Exec(`INSERT INTO public.openclaw_gateway_session_receipts
		(execution_reference, owner_identity, runtime_task_id, session_key, run_id, status, terminal_status, terminal_at)
		VALUES (?, ?, ?, ?, ?, 'terminal', 'completed', ?)`,
		executionReference, owner, taskID, uuid.NewString(), uuid.NewString(), now,
	).Error; err != nil {
		t.Fatalf("insert gateway session receipt: %v", err)
	}
	if err := db.Exec(`INSERT INTO public.openclaw_gateway_artifact_receipts
		(execution_reference, artifact_digest, artifact_type, size_bytes)
		VALUES (?, ?, 'file', 1)`, executionReference, digest).Error; err != nil {
		t.Fatalf("insert gateway artifact receipt: %v", err)
	}
	if err := db.Exec(`INSERT INTO public.automation_launch_events
		(id, automation_id, owner_identity, runtime_type, runtime_task_id, execution_reference, launch_type, event_key, target, status, started_at, completed_at)
		VALUES (?, ?, ?, 'openclaw', ?, ?, 'agent_runtime_openclaw_terminal', ?, 'archive://rollback-test', 'observed', ?, ?)`,
		eventID, automationID, owner, taskID, executionReference, uuid.NewString(), now, now,
	).Error; err != nil {
		t.Fatalf("insert OpenClaw source event: %v", err)
	}
	return openClawOwnerBindingFixture{owner: owner, executionReference: executionReference, eventID: eventID, digest: digest, contentSHA: contentSHA, now: now}
}

func seedOpenClawRetainedArtifactForOwnerBindingRollback(t *testing.T, db *gorm.DB) {
	t.Helper()
	fixture := createOpenClawArtifactOwnerBindingFixture(t, db)
	if err := insertOpenClawRetainedArtifact(db, fixture); err != nil {
		t.Fatalf("insert retained artifact: %v", err)
	}
}

func insertOpenClawRetainedArtifact(tx *gorm.DB, fixture openClawOwnerBindingFixture) error {
	return tx.Exec(`INSERT INTO public.openclaw_retained_artifacts
		(execution_reference, artifact_digest, owner_identity, source_event_id, content_sha256, size_bytes, encrypted_content)
		VALUES (?, ?, ?, ?, ?, 1, ?)`,
		fixture.executionReference, fixture.digest, fixture.owner, fixture.eventID, fixture.contentSHA, bytes.Repeat([]byte{0xa5}, 29),
	).Error
}

func assertOpenClawOwnerBindingRollbackState(t *testing.T, db *gorm.DB, wantConstraints, wantIndexes int64, wantMigrationApplied bool) {
	t.Helper()
	var constraints int64
	if err := db.Raw(`SELECT COUNT(*) FROM pg_constraint
		WHERE conrelid = 'public.openclaw_retained_artifacts'::regclass
		  AND conname IN (
			'fk_openclaw_retained_artifacts_owner_event',
			'fk_openclaw_retained_artifacts_owner_receipt'
		  )`).Scan(&constraints).Error; err != nil {
		t.Fatalf("count owner-binding constraints: %v", err)
	}
	if constraints != wantConstraints {
		t.Fatalf("owner-binding constraints = %d, want %d", constraints, wantConstraints)
	}
	var indexes int64
	if err := db.Raw(`SELECT COUNT(*) FROM pg_indexes
		WHERE schemaname = 'public'
		  AND indexname IN (
			'uq_automation_launch_events_artifact_owner_binding',
			'uq_openclaw_session_receipts_artifact_owner_binding'
		  )`).Scan(&indexes).Error; err != nil {
		t.Fatalf("count owner-binding indexes: %v", err)
	}
	if indexes != wantIndexes {
		t.Fatalf("owner-binding indexes = %d, want %d", indexes, wantIndexes)
	}
	var applied int64
	if err := db.Table("schema_migrations").Where("version = ?", openClawArtifactOwnerBindingMigration).Count(&applied).Error; err != nil {
		t.Fatalf("count owner-binding migration ledger row: %v", err)
	}
	wantLedgerCount := int64(0)
	if wantMigrationApplied {
		wantLedgerCount = 1
	}
	if applied != wantLedgerCount {
		t.Fatalf("owner-binding migration ledger count = %d, want %d", applied, wantLedgerCount)
	}
}
