package durablejob

import (
	"strings"
	"testing"

	"automation-hub-backend/migrations"
)

func TestDurableJobFencingMigrationContract(t *testing.T) {
	up, err := migrations.Files.ReadFile("pre/0006_durable_job_fencing.up.sql")
	if err != nil {
		t.Fatalf("read fencing up migration: %v", err)
	}
	down, err := migrations.Files.ReadFile("pre/0006_durable_job_fencing.down.sql")
	if err != nil {
		t.Fatalf("read fencing down migration: %v", err)
	}
	for _, fragment := range []string{
		"lease_generation bigint",
		"DEFAULT 0 NOT NULL",
		"idx_durable_jobs_owned_lease",
		"locked_by",
	} {
		if !strings.Contains(string(up), fragment) {
			t.Errorf("fencing up migration is missing %q", fragment)
		}
	}
	for _, fragment := range []string{
		"DROP INDEX IF EXISTS",
		"DROP COLUMN IF EXISTS lease_generation",
	} {
		if !strings.Contains(string(down), fragment) {
			t.Errorf("fencing down migration is missing %q", fragment)
		}
	}
}

func TestDurableJobQueueIndexMigrationContract(t *testing.T) {
	up, err := migrations.Files.ReadFile("post/0003_durable_jobs_queue_index.up.sql")
	if err != nil {
		t.Fatalf("read queue-index up migration: %v", err)
	}
	down, err := migrations.Files.ReadFile("post/0003_durable_jobs_queue_index.down.sql")
	if err != nil {
		t.Fatalf("read queue-index down migration: %v", err)
	}
	if !strings.Contains(string(up), "(queue, status, run_at)") {
		t.Fatal("queue-index migration must scope claims by queue")
	}
	if !strings.Contains(string(down), "(status, run_at)") {
		t.Fatal("queue-index rollback must restore the previous index")
	}
}

func TestDurableJobQueueLeaseIndexMigrationContract(t *testing.T) {
	up, err := migrations.Files.ReadFile("post/0005_durable_jobs_queue_lease_index.up.sql")
	if err != nil {
		t.Fatalf("read queue-lease up migration: %v", err)
	}
	down, err := migrations.Files.ReadFile("post/0005_durable_jobs_queue_lease_index.down.sql")
	if err != nil {
		t.Fatalf("read queue-lease down migration: %v", err)
	}
	if !strings.Contains(string(up), "(queue, status, locked_at)") || !strings.Contains(string(up), "WHERE locked_at IS NOT NULL") {
		t.Fatal("queue-lease migration must index queue-scoped expired lease recovery")
	}
	if !strings.Contains(string(down), "idx_durable_jobs_queue_lease") {
		t.Fatal("queue-lease rollback must remove the queue-scoped lease index")
	}
}
