//go:build integration

package migrations_test

import (
	"io/fs"
	"testing"
	"testing/fstest"
	"time"

	"automation-hub-backend/internal/infra"
	"automation-hub-backend/migrations"

	"github.com/google/uuid"
)

func TestHostRuntimeExpiryMigrationBackfillsWithoutDiscardingAuditData(t *testing.T) {
	db := openIsolatedMigrationDatabase(t)
	if err := db.Exec(`
		CREATE TABLE public.host_runtime_jobs (
			id UUID PRIMARY KEY,
			owner_identity TEXT NOT NULL,
			runtime_id TEXT NOT NULL,
			task_id TEXT NOT NULL UNIQUE,
			prompt TEXT NOT NULL,
			workspace_key TEXT NOT NULL,
			status TEXT NOT NULL,
			worker_id TEXT NOT NULL DEFAULT '',
			lease_digest TEXT NOT NULL DEFAULT '',
			lease_expires TIMESTAMPTZ,
			output TEXT NOT NULL DEFAULT '',
			error TEXT NOT NULL DEFAULT '',
			exit_code INTEGER,
			created_at TIMESTAMPTZ NOT NULL,
			updated_at TIMESTAMPTZ NOT NULL,
			completed_at TIMESTAMPTZ,
			reconciled_at TIMESTAMPTZ,
			CONSTRAINT chk_host_runtime_jobs_status CHECK (status IN ('pending', 'leased', 'completed', 'cancelled'))
		)`).Error; err != nil {
		t.Fatalf("create pre-migration host-runtime schema: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Microsecond)
	insertLegacy := func(status, taskID, worker, digest string, createdAt time.Time, leaseExpires *time.Time, output, errorText string, exitCode *int, completedAt, reconciledAt *time.Time) uuid.UUID {
		t.Helper()
		id := uuid.New()
		if err := db.Exec(`INSERT INTO public.host_runtime_jobs
			(id, owner_identity, runtime_id, task_id, prompt, workspace_key, status, worker_id, lease_digest,
			 lease_expires, output, error, exit_code, created_at, updated_at, completed_at, reconciled_at)
			VALUES (?, 'owner:test', 'deepseek-harness', ?, 'prompt', 'workspace', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			id, taskID, status, worker, digest, leaseExpires, output, errorText, exitCode, createdAt, createdAt, completedAt, reconciledAt).Error; err != nil {
			t.Fatalf("insert legacy %s job: %v", status, err)
		}
		return id
	}

	freshCreated := now.Add(-time.Hour)
	freshID := insertLegacy("pending", "fresh-pending", "", "", freshCreated, nil, "", "", nil, nil, nil)
	expiredCreated := now.Add(-72 * time.Hour)
	expiredID := insertLegacy("pending", "expired-pending", "", "", expiredCreated, nil, "", "", nil, nil, nil)
	leaseExpiredCreated := now.Add(-2 * time.Hour)
	leaseExpiredAt := now.Add(-time.Minute)
	staleLeaseID := insertLegacy("leased", "stale-lease", "worker-stale", "secret-digest", leaseExpiredCreated, &leaseExpiredAt, "partial output", "partial error", nil, nil, nil)
	approvalExpiredCreated := now.Add(-72 * time.Hour)
	futureLeaseExpiry := now.Add(time.Hour)
	approvalExpiredLeaseID := insertLegacy("leased", "expired-approval-lease", "worker-expired", "old-digest", approvalExpiredCreated, &futureLeaseExpiry, "captured output", "captured error", nil, nil, nil)
	completedAt := now.Add(-47 * time.Hour)
	reconciledAt := now.Add(-46 * time.Hour)
	exitCode := 0
	completedID := insertLegacy("completed", "completed-audit", "worker-done", "", now.Add(-48*time.Hour), nil, "final output", "", &exitCode, &completedAt, &reconciledAt)

	up, err := migrations.Files.ReadFile("pre/0086_host_runtime_job_expiry_review.up.sql")
	if err != nil {
		t.Fatalf("read migration up: %v", err)
	}
	down, err := migrations.Files.ReadFile("pre/0086_host_runtime_job_expiry_review.down.sql")
	if err != nil {
		t.Fatalf("read migration down: %v", err)
	}
	files := fstest.MapFS{
		"pre": &fstest.MapFile{Mode: fs.ModeDir},
		"pre/0086_host_runtime_job_expiry_review.up.sql":   &fstest.MapFile{Data: up, Mode: 0o600},
		"pre/0086_host_runtime_job_expiry_review.down.sql": &fstest.MapFile{Data: down, Mode: 0o600},
	}
	if _, err := infra.ApplyMigrations(db, files, "pre"); err != nil {
		t.Fatalf("apply host-runtime expiry migration: %v", err)
	}

	var fresh struct {
		Status            string
		ApprovalExpiresAt time.Time `gorm:"column:approval_expires_at"`
		CreatedAt         time.Time `gorm:"column:created_at"`
	}
	if err := db.Raw("SELECT status, approval_expires_at, created_at FROM public.host_runtime_jobs WHERE id = ?", freshID).Scan(&fresh).Error; err != nil {
		t.Fatalf("read fresh job: %v", err)
	}
	if fresh.Status != "pending" || !fresh.ApprovalExpiresAt.Equal(fresh.CreatedAt.Add(24*time.Hour)) {
		t.Fatalf("fresh pending backfill = %#v", fresh)
	}

	var expired struct {
		Status           string
		LeaseDigest      string     `gorm:"column:lease_digest"`
		LeaseExpires     *time.Time `gorm:"column:lease_expires"`
		ReviewRequiredAt *time.Time `gorm:"column:review_required_at"`
		ReviewReason     string     `gorm:"column:review_reason"`
		CreatedAt        time.Time  `gorm:"column:created_at"`
	}
	if err := db.Raw("SELECT status, lease_digest, lease_expires, review_required_at, review_reason, created_at FROM public.host_runtime_jobs WHERE id = ?", expiredID).Scan(&expired).Error; err != nil {
		t.Fatalf("read expired approval: %v", err)
	}
	if expired.Status != "expired" || expired.LeaseDigest != "" || expired.LeaseExpires != nil || expired.ReviewRequiredAt == nil || expired.ReviewReason == "" || !expired.CreatedAt.Equal(expiredCreated) {
		t.Fatalf("expired pending backfill lost its explicit review/audit state: %#v", expired)
	}

	for _, id := range []uuid.UUID{staleLeaseID, approvalExpiredLeaseID} {
		var state struct {
			Status           string
			WorkerID         string     `gorm:"column:worker_id"`
			LeaseDigest      string     `gorm:"column:lease_digest"`
			LeaseExpires     *time.Time `gorm:"column:lease_expires"`
			ReviewRequiredAt *time.Time `gorm:"column:review_required_at"`
			ReviewReason     string     `gorm:"column:review_reason"`
			Output           string
			Error            string
		}
		if err := db.Raw("SELECT status, worker_id, lease_digest, lease_expires, review_required_at, review_reason, output, error FROM public.host_runtime_jobs WHERE id = ?", id).Scan(&state).Error; err != nil {
			t.Fatalf("read stale lease %s: %v", id, err)
		}
		if state.Status != "needs_review" || state.WorkerID == "" || state.LeaseDigest != "" || state.LeaseExpires != nil || state.ReviewRequiredAt == nil || state.ReviewReason == "" || state.Output == "" || state.Error == "" {
			t.Fatalf("stale lease %s was not quarantined with audit evidence preserved: %#v", id, state)
		}
	}

	var completed struct {
		Status       string
		Output       string
		ExitCode     *int       `gorm:"column:exit_code"`
		CompletedAt  *time.Time `gorm:"column:completed_at"`
		ReconciledAt *time.Time `gorm:"column:reconciled_at"`
	}
	if err := db.Raw("SELECT status, output, exit_code, completed_at, reconciled_at FROM public.host_runtime_jobs WHERE id = ?", completedID).Scan(&completed).Error; err != nil {
		t.Fatalf("read completed audit row: %v", err)
	}
	if completed.Status != "completed" || completed.Output != "final output" || completed.ExitCode == nil || *completed.ExitCode != 0 || completed.CompletedAt == nil || completed.ReconciledAt == nil {
		t.Fatalf("migration changed completed audit fields: %#v", completed)
	}

	if err := infra.RollbackMigration(db, files, "pre", "pre/0086_host_runtime_job_expiry_review"); err == nil {
		t.Fatal("unsafe rollback unexpectedly succeeded")
	}
	var rowCount int64
	if err := db.Table("public.host_runtime_jobs").Where("id = ?", staleLeaseID).Count(&rowCount).Error; err != nil || rowCount != 1 {
		t.Fatalf("rollback refusal changed host-runtime data: count=%d err=%v", rowCount, err)
	}
	var migrationRecorded bool
	if err := db.Raw("SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = ?)", "pre/0086_host_runtime_job_expiry_review").Row().Scan(&migrationRecorded); err != nil || !migrationRecorded {
		t.Fatalf("rollback refusal changed migration ledger: recorded=%t err=%v", migrationRecorded, err)
	}
}
