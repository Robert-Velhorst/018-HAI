package ambientmonitor

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestPostgresCollectorsMatchOwnerScopedCanonicalRecords(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("HAI_AMBIENT_MONITOR_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("HAI_AMBIENT_MONITOR_POSTGRES_TEST_DSN is not configured")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open Postgres collector database: %v", err)
	}
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatalf("begin collector transaction: %v", tx.Error)
	}
	t.Cleanup(func() { _ = tx.Rollback().Error })

	createCollectorTestTables(t, tx)
	now := time.Date(2026, time.August, 14, 10, 0, 0, 0, time.UTC)
	seedCollectorTestRows(t, tx, now)
	collector, err := NewGormCollector(tx, func() time.Time { return now })
	if err != nil {
		t.Fatalf("create Postgres collector: %v", err)
	}

	for _, kind := range []SourceKind{
		SourceWorkflowOpenLoopCount,
		SourceWorkflowVerifiedCompletionCount,
		SourceOverdueCommitmentCount,
	} {
		t.Run(string(kind), func(t *testing.T) {
			target := collectorTarget(kind)
			first, err := collector.Collect(context.Background(), target)
			if err != nil {
				t.Fatalf("collect %s: %v", kind, err)
			}
			second, err := collector.Collect(context.Background(), target)
			if err != nil {
				t.Fatalf("replay collect %s: %v", kind, err)
			}
			if first.Value != 1 || second.Value != 1 {
				t.Fatalf("%s values = %v and %v, want 1", kind, first.Value, second.Value)
			}
			if len(first.SourceDigest) != 64 || first.SourceDigest != second.SourceDigest {
				t.Fatalf("%s digests are not deterministic: %q and %q", kind, first.SourceDigest, second.SourceDigest)
			}
			if !first.ObservedAt.Equal(now) || !second.ObservedAt.Equal(now) {
				t.Fatalf("%s observation times = %s and %s, want %s", kind, first.ObservedAt, second.ObservedAt, now)
			}
		})
	}
}

func TestPostgresCollectorSnapshotUsesReadOnlyRepeatableRead(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("HAI_AMBIENT_MONITOR_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("HAI_AMBIENT_MONITOR_POSTGRES_TEST_DSN is not configured")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open Postgres collector database: %v", err)
	}

	var isolation string
	var readOnly string
	err = withCollectorSnapshot(t.Context(), db, func(tx *gorm.DB) error {
		if err := tx.Raw(`SHOW transaction_isolation`).Row().Scan(&isolation); err != nil {
			return err
		}
		return tx.Raw(`SHOW transaction_read_only`).Row().Scan(&readOnly)
	})
	if err != nil {
		t.Fatalf("inspect collector snapshot transaction: %v", err)
	}
	if isolation != "repeatable read" || readOnly != "on" {
		t.Fatalf("collector snapshot transaction = isolation %q read-only %q", isolation, readOnly)
	}
}

func createCollectorTestTables(t *testing.T, db *gorm.DB) {
	t.Helper()
	statements := []string{
		`CREATE TEMP TABLE workflow_items (id uuid, owner_identity text, archived boolean, current_state text) ON COMMIT DROP`,
		`CREATE TEMP TABLE workflow_open_loops (id uuid, workflow_id uuid, status text, follow_up_at timestamptz, updated_at timestamptz) ON COMMIT DROP`,
		`CREATE TEMP TABLE workflow_completion_attestations (id uuid, workflow_id uuid, owner_identity text, completion_status text, verification_status text, record_digest text, completed_at timestamptz) ON COMMIT DROP`,
		`CREATE TEMP TABLE life_ledger_commitment_revisions (owner_identity text, commitment_key text, revision bigint, record_digest text, payload jsonb, recorded_at timestamptz) ON COMMIT DROP`,
	}
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("create collector test table: %v", err)
		}
	}
}

func seedCollectorTestRows(t *testing.T, db *gorm.DB, now time.Time) {
	t.Helper()
	const digestA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const digestB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	const digestC = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	type seedStatement struct {
		query string
		args  []interface{}
	}
	statements := []seedStatement{
		{query: `INSERT INTO workflow_items (id, owner_identity, archived, current_state) VALUES
	('10000000-0000-0000-0000-000000000001', 'owner-a', false, 'active'),
	('10000000-0000-0000-0000-000000000002', 'owner-a', true, 'active'),
	('10000000-0000-0000-0000-000000000003', 'owner-a', false, 'completed'),
	('10000000-0000-0000-0000-000000000004', 'owner-b', false, 'active')`},
		{query: `INSERT INTO workflow_open_loops (id, workflow_id, status, follow_up_at, updated_at) VALUES
	('20000000-0000-0000-0000-000000000001', '10000000-0000-0000-0000-000000000001', 'open', @past, @past),
	('20000000-0000-0000-0000-000000000002', '10000000-0000-0000-0000-000000000001', 'open', @future, @past),
	('20000000-0000-0000-0000-000000000003', '10000000-0000-0000-0000-000000000002', 'open', NULL, @past),
	('20000000-0000-0000-0000-000000000004', '10000000-0000-0000-0000-000000000003', 'open', NULL, @past),
	('20000000-0000-0000-0000-000000000005', '10000000-0000-0000-0000-000000000004', 'open', NULL, @past)`, args: []interface{}{
			sql.Named("past", now.Add(-time.Hour)), sql.Named("future", now.Add(time.Hour)),
		}},
		{query: `INSERT INTO workflow_completion_attestations (id, workflow_id, owner_identity, completion_status, verification_status, record_digest, completed_at) VALUES
	('30000000-0000-0000-0000-000000000001', '10000000-0000-0000-0000-000000000001', 'owner-a', 'completed', 'verified', @digest_a, @past),
	('30000000-0000-0000-0000-000000000002', '10000000-0000-0000-0000-000000000001', 'owner-a', 'completed', 'uncertain', @digest_b, @past),
	('30000000-0000-0000-0000-000000000003', '10000000-0000-0000-0000-000000000004', 'owner-b', 'completed', 'test_passed', @digest_c, @past)`, args: []interface{}{
			sql.Named("digest_a", digestA), sql.Named("digest_b", digestB), sql.Named("digest_c", digestC), sql.Named("past", now.Add(-time.Hour)),
		}},
		{query: `INSERT INTO life_ledger_commitment_revisions (owner_identity, commitment_key, revision, record_digest, payload, recorded_at) VALUES
	('owner-a', 'commitment-overdue', 1, @digest_a, jsonb_build_object('status','active','dueAt',CAST(@past_text AS text),'projectKey','project-a'), @past),
	('owner-a', 'commitment-future', 1, @digest_b, jsonb_build_object('status','active','dueAt',CAST(@future_text AS text)), @past),
	('owner-a', 'commitment-terminal', 1, @digest_c, jsonb_build_object('status','fulfilled','dueAt',CAST(@past_text AS text)), @past),
	('owner-a', 'commitment-latest-terminal', 1, @digest_a, jsonb_build_object('status','active','dueAt',CAST(@past_text AS text)), @past),
	('owner-a', 'commitment-latest-terminal', 2, @digest_b, jsonb_build_object('status','fulfilled','dueAt',CAST(@past_text AS text)), @future),
	('owner-b', 'commitment-bob', 1, @digest_c, jsonb_build_object('status','active','dueAt',CAST(@past_text AS text)), @past)`, args: []interface{}{
			sql.Named("digest_a", digestA), sql.Named("digest_b", digestB), sql.Named("digest_c", digestC),
			sql.Named("past", now.Add(-time.Hour)), sql.Named("future", now.Add(time.Hour)),
			sql.Named("past_text", now.Add(-time.Hour).Format(time.RFC3339Nano)), sql.Named("future_text", now.Add(time.Hour).Format(time.RFC3339Nano)),
		}},
	}
	for _, statement := range statements {
		if err := db.Exec(statement.query, statement.args...).Error; err != nil {
			t.Fatalf("seed collector test rows: %v", err)
		}
	}
}
