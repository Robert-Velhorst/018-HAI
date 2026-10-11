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

func TestReviewReplayPolicyMigrationPreservesAuthority(t *testing.T) {
	up, err := migrations.Files.ReadFile("pre/0115_durable_job_replay_policy.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	down, err := migrations.Files.ReadFile("pre/0115_durable_job_replay_policy.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{
		"replay_policy text NOT NULL DEFAULT 'at_least_once'",
		"ALTER COLUMN replay_policy SET NOT NULL",
		"CHECK (replay_policy IN ('at_least_once', 'review_unknown'))",
		"WHERE kind = 'ambient.scan' AND status IN ('pending', 'running', 'needs_review')",
		"ON public.durable_jobs (queue, kind) WHERE status IN ('needs_review', 'settling')",
	} {
		if !strings.Contains(string(up), fragment) {
			t.Errorf("missing replay authority: %q", fragment)
		}
	}
	for _, fragment := range []string{"LOCK TABLE public.durable_jobs IN ACCESS EXCLUSIVE MODE", "replay_policy IS DISTINCT FROM 'at_least_once' OR status IN ('needs_review', 'settling')", "RAISE EXCEPTION", "ERRCODE = '55000'"} {
		if !strings.Contains(string(down), fragment) {
			t.Errorf("unsafe rollback: missing %q", fragment)
		}
	}
	if strings.Index(string(down), "LOCK TABLE") > strings.Index(string(down), "IF EXISTS") ||
		strings.Index(string(down), "RAISE EXCEPTION") > strings.Index(string(down), "DROP INDEX") ||
		strings.Contains(strings.ToUpper(string(down)), "DELETE FROM") {
		t.Fatal("rollback must lock and refuse before removing review authority; no row deletion")
	}
}
