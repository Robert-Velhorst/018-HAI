package hostruntime

import (
	"strings"
	"testing"

	"automation-hub-backend/migrations"
)

func TestHostRuntimeJobMigrationCreatesLeasedJobLedger(t *testing.T) {
	up, err := migrations.Files.ReadFile("pre/0065_host_runtime_jobs.up.sql")
	if err != nil {
		t.Fatalf("read up migration: %v", err)
	}
	down, err := migrations.Files.ReadFile("pre/0065_host_runtime_jobs.down.sql")
	if err != nil {
		t.Fatalf("read down migration: %v", err)
	}
	for _, fragment := range []string{"CREATE TABLE IF NOT EXISTS host_runtime_jobs", "lease_digest", "lease_expires", "task_id", "reconciled_at", "idx_host_runtime_jobs_completed_unreconciled", "chk_host_runtime_jobs_status"} {
		if !strings.Contains(string(up), fragment) {
			t.Fatalf("up migration missing %q", fragment)
		}
	}
	downSQL := strings.ToLower(string(down))
	lock := strings.Index(downSQL, "lock table public.host_runtime_jobs in access exclusive mode")
	guard := strings.Index(downSQL, "if exists (select 1 from public.host_runtime_jobs)")
	drop := strings.Index(downSQL, "drop table if exists public.host_runtime_jobs")
	if lock < 0 || guard < 0 || drop < 0 || lock > guard || guard > drop ||
		!strings.Contains(downSQL, "rollback 0065_host_runtime_jobs refused") {
		t.Fatal("down migration must lock the table, refuse to drop persisted jobs, and only then remove an empty table")
	}
}

func TestHostRuntimeExpiryMigrationEnforcesBoundedApprovalAndReviewOnlyExpiredLeases(t *testing.T) {
	up, err := migrations.Files.ReadFile("pre/0086_host_runtime_job_expiry_review.up.sql")
	if err != nil {
		t.Fatalf("read expiry/review up migration: %v", err)
	}
	down, err := migrations.Files.ReadFile("pre/0086_host_runtime_job_expiry_review.down.sql")
	if err != nil {
		t.Fatalf("read expiry/review down migration: %v", err)
	}
	for _, fragment := range []string{
		"approval_expires_at",
		"created_at + INTERVAL '24 hours'",
		"status = 'expired'",
		"status = 'needs_review'",
		"lease_expires IS NULL",
		"lease_expires <= now()",
		"approval_expires_at <= now()",
		"lease_digest = ''",
		"review_required_at",
		"review_reason",
		"chk_host_runtime_jobs_intervention_state",
		"idx_host_runtime_jobs_fresh_pending",
		"idx_host_runtime_jobs_expired_lease",
	} {
		if !strings.Contains(string(up), fragment) {
			t.Errorf("up migration missing %q", fragment)
		}
	}
	for _, preservedField := range []string{"output", "error", "exit_code", "completed_at", "reconciled_at", "worker_id"} {
		if strings.Contains(string(up), strings.ToLower(preservedField)+" =") {
			t.Errorf("up migration must preserve existing audit field %q", preservedField)
		}
	}
	dropOldCheck := strings.Index(string(up), "DROP CONSTRAINT IF EXISTS chk_host_runtime_jobs_status")
	expireRows := strings.Index(string(up), "SET status = 'expired'")
	reviewRows := strings.Index(string(up), "SET status = 'needs_review'")
	addNewCheck := strings.Index(string(up), "ADD CONSTRAINT chk_host_runtime_jobs_status")
	if dropOldCheck < 0 || expireRows < 0 || reviewRows < 0 || addNewCheck < 0 ||
		dropOldCheck > expireRows || dropOldCheck > reviewRows || addNewCheck < expireRows || addNewCheck < reviewRows {
		t.Fatal("status constraint must be widened before, and validated after, existing rows enter intervention states")
	}
	if !strings.Contains(string(down), "rollback refused") || !strings.Contains(string(down), "USING ERRCODE = '55000'") {
		t.Fatal("down migration must fail closed without silently restoring unsafe lease behavior")
	}
	for _, destructive := range []string{"DROP TABLE", "DELETE FROM", "TRUNCATE", "DROP COLUMN"} {
		if strings.Contains(strings.ToUpper(string(down)), destructive) {
			t.Errorf("down migration must not discard host-runtime data; found %q", destructive)
		}
	}
}

func TestHostRuntimeCancellationFenceMigrationIsAdditiveAndRollbackFailsClosed(t *testing.T) {
	up, err := migrations.Files.ReadFile("pre/0092_host_runtime_execution_confirmation_fence.up.sql")
	if err != nil {
		t.Fatalf("read cancellation-fence up migration: %v", err)
	}
	down, err := migrations.Files.ReadFile("pre/0092_host_runtime_execution_confirmation_fence.down.sql")
	if err != nil {
		t.Fatalf("read cancellation-fence down migration: %v", err)
	}
	for _, fragment := range []string{
		"ADD COLUMN IF NOT EXISTS execution_confirmed_at TIMESTAMPTZ",
		"WHERE status = 'leased'",
		"COALESCE(execution_confirmed_at, updated_at, created_at, now())",
		"COMMENT ON COLUMN public.host_runtime_jobs.execution_confirmed_at",
	} {
		if !strings.Contains(string(up), fragment) {
			t.Errorf("up migration missing %q", fragment)
		}
	}
	for _, preservedField := range []string{"prompt", "output", "error", "exit_code", "worker_id", "lease_digest", "created_at", "reconciled_at"} {
		if strings.Contains(strings.ToLower(string(up)), preservedField+" =") {
			t.Errorf("up migration must preserve host-job field %q", preservedField)
		}
	}
	if !strings.Contains(strings.ToLower(string(down)), "rollback refused") || !strings.Contains(string(down), "USING ERRCODE = '55000'") {
		t.Fatal("down migration must refuse to remove the durable cancellation safety fence")
	}
	for _, destructive := range []string{"DROP TABLE", "DELETE FROM", "TRUNCATE", "DROP COLUMN", "UPDATE "} {
		if strings.Contains(strings.ToUpper(string(down)), destructive) {
			t.Errorf("down migration must preserve data and cancellation evidence; found %q", destructive)
		}
	}
}

func TestHostRuntimeCancellationRequestMigrationPreservesDurableStopEvidence(t *testing.T) {
	up, err := migrations.Files.ReadFile("pre/0094_host_runtime_cancellation_request.up.sql")
	if err != nil {
		t.Fatalf("read cancellation request up migration: %v", err)
	}
	down, err := migrations.Files.ReadFile("pre/0094_host_runtime_cancellation_request.down.sql")
	if err != nil {
		t.Fatalf("read cancellation request down migration: %v", err)
	}
	for _, fragment := range []string{
		"ADD COLUMN IF NOT EXISTS cancel_requested_at TIMESTAMPTZ",
		"COMMENT ON COLUMN public.host_runtime_jobs.cancel_requested_at",
		"verified contained-process termination",
	} {
		if !strings.Contains(string(up), fragment) {
			t.Errorf("up migration missing %q", fragment)
		}
	}
	if !strings.Contains(strings.ToLower(string(down)), "rollback refused") || !strings.Contains(string(down), "USING ERRCODE = '55000'") {
		t.Fatal("down migration must refuse to erase durable Stop-request evidence")
	}
	for _, destructive := range []string{"DROP TABLE", "DELETE FROM", "TRUNCATE", "DROP COLUMN", "UPDATE "} {
		if strings.Contains(strings.ToUpper(string(down)), destructive) {
			t.Errorf("down migration must preserve cancellation evidence; found %q", destructive)
		}
	}
}

func TestHostRuntimeStopRevisionMigrationInvalidatesPreStopApprovalsWithoutDestructiveRollback(t *testing.T) {
	up, err := migrations.Files.ReadFile("pre/0100_host_runtime_stop_revision.up.sql")
	if err != nil {
		t.Fatalf("read stop-revision up migration: %v", err)
	}
	down, err := migrations.Files.ReadFile("pre/0100_host_runtime_stop_revision.down.sql")
	if err != nil {
		t.Fatalf("read stop-revision down migration: %v", err)
	}
	for _, fragment := range []string{
		"ADD COLUMN IF NOT EXISTS stop_revision BIGINT NOT NULL DEFAULT 0",
		"COMMENT ON COLUMN public.host_runtime_jobs.stop_revision",
		"later revision invalidates unstarted work",
	} {
		if !strings.Contains(string(up), fragment) {
			t.Errorf("up migration missing %q", fragment)
		}
	}
	if !strings.Contains(strings.ToLower(string(down)), "rollback refused") || !strings.Contains(string(down), "USING ERRCODE = '55000'") {
		t.Fatal("down migration must refuse to remove stop-revision history")
	}
	for _, destructive := range []string{"DROP TABLE", "DELETE FROM", "TRUNCATE", "DROP COLUMN", "UPDATE "} {
		if strings.Contains(strings.ToUpper(string(down)), destructive) {
			t.Errorf("down migration must preserve host-job and approval history; found %q", destructive)
		}
	}
}

func TestHostRuntimeStartIntentMigrationQuarantinesLegacyLeasesAndRefusesRollback(t *testing.T) {
	up, err := migrations.Files.ReadFile("pre/0108_host_runtime_start_intent.up.sql")
	if err != nil {
		t.Fatalf("read start-intent up migration: %v", err)
	}
	down, err := migrations.Files.ReadFile("pre/0108_host_runtime_start_intent.down.sql")
	if err != nil {
		t.Fatalf("read start-intent down migration: %v", err)
	}
	for _, fragment := range []string{
		"ADD COLUMN IF NOT EXISTS approval_digest",
		"ADD COLUMN IF NOT EXISTS start_intent_id UUID",
		"ADD COLUMN IF NOT EXISTS start_intent_worker_id",
		"ADD COLUMN IF NOT EXISTS start_intent_lease_digest",
		"ADD COLUMN IF NOT EXISTS start_intent_approval_digest",
		"ADD COLUMN IF NOT EXISTS start_intent_stop_revision",
		"ADD COLUMN IF NOT EXISTS actual_start_ack_at",
		"WHERE status = 'leased'",
		"status = 'needs_review'",
		"chk_host_runtime_jobs_start_intent_binding",
		"chk_host_runtime_jobs_start_ack_requires_intent",
		"prevent_host_runtime_start_binding_rewrite",
		"start intent is immutable",
		"actual-start acknowledgment is immutable",
	} {
		if !strings.Contains(string(up), fragment) {
			t.Errorf("up migration missing %q", fragment)
		}
	}
	upSQL := strings.ToLower(string(up))
	for _, preservedField := range []string{"prompt", "output", "error", "exit_code", "worker_id", "created_at"} {
		if strings.Contains(upSQL, "set "+preservedField+" =") || strings.Contains(upSQL, ", "+preservedField+" =") {
			t.Errorf("up migration must preserve existing evidence field %q", preservedField)
		}
	}
	if !strings.Contains(strings.ToLower(string(down)), "rollback refused") || !strings.Contains(string(down), "USING ERRCODE = '55000'") {
		t.Fatal("down migration must refuse to erase durable start-intent safety evidence")
	}
	for _, destructive := range []string{"DROP TABLE", "DELETE FROM", "TRUNCATE", "DROP COLUMN", "UPDATE "} {
		if strings.Contains(strings.ToUpper(string(down)), destructive) {
			t.Errorf("down migration must preserve protocol evidence; found %q", destructive)
		}
	}
}
