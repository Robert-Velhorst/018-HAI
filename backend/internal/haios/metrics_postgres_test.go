package haios

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestOwnerMetricSnapshotIsScopedAndConsistent(t *testing.T) {
	db := openMetricTestDatabase(t)
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatalf("begin transaction: %v", tx.Error)
	}
	t.Cleanup(func() { _ = tx.Rollback().Error })

	createMetricTestTables(t, tx)
	seedMetricTestRows(t, tx)

	now := time.Now().UTC()
	snapshot, err := NewHandler(tx, nil).loadMetricSnapshot(context.Background(), " alice ", now)
	if err != nil {
		t.Fatalf("load owner metric snapshot: %v", err)
	}

	want := ownerMetricSnapshot{
		Automations:            2,
		UnhealthyAutomations:   1,
		OpenAutomationAlerts:   1,
		ConnectedSources:       1,
		SourceExtractions:      1,
		SourceReview:           1,
		WorkflowItems:          2,
		WorkflowApprovals:      1,
		WorkflowReview:         1,
		WorkflowProposalReview: 1,
		WorkflowQualityReview:  1,
		DueOpenLoops:           1,
		ContextMemories:        1,
		VerificationRuns:       1,
		VerificationReview:     1,
		AmbientProposals:       1,
		AmbientApprovalQueue:   1,
		AmbientLastScan:        "completed",
	}
	if snapshot != want {
		t.Fatalf("snapshot = %#v, want %#v", snapshot, want)
	}
	if total := snapshot.ReviewTotal(); total != 7 {
		t.Fatalf("review total = %d, want 7", total)
	}
}

func TestOwnerMetricSnapshotRejectsInvalidInputs(t *testing.T) {
	if _, err := NewHandler(nil, nil).loadMetricSnapshot(context.Background(), "alice", time.Now()); err == nil {
		t.Fatal("nil database should fail")
	}

	db := openMetricTestDatabase(t)
	if _, err := NewHandler(db, nil).loadMetricSnapshot(context.Background(), " ", time.Now()); err == nil {
		t.Fatal("empty owner should fail")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := NewHandler(db, nil).loadMetricSnapshot(ctx, "alice", time.Now()); err == nil {
		t.Fatal("cancelled context should fail")
	}
}

func openMetricTestDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("HAI_TEST_DATABASE_DSN"))
	if dsn == "" {
		t.Skip("HAI_TEST_DATABASE_DSN not set; skipping HAI OS Postgres integration test")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open Postgres test database: %v", err)
	}
	return db
}

func createMetricTestTables(t *testing.T, db *gorm.DB) {
	t.Helper()
	statements := []string{
		`CREATE TEMP TABLE automations (id text, status text) ON COMMIT DROP`,
		`CREATE TEMP TABLE automation_alerts (id text, status text) ON COMMIT DROP`,
		`CREATE TEMP TABLE connected_sources (id text, owner_identity text, status text) ON COMMIT DROP`,
		`CREATE TEMP TABLE source_extractions (id text, source_id text, archived boolean, uncertain boolean, sensitive boolean) ON COMMIT DROP`,
		`CREATE TEMP TABLE workflow_items (id text, owner_identity text, current_state text, approval_status text, archived boolean) ON COMMIT DROP`,
		`CREATE TEMP TABLE workflow_proposals (id text, workflow_id text, status text) ON COMMIT DROP`,
		`CREATE TEMP TABLE workflow_quality_gates (id text, workflow_id text, status text) ON COMMIT DROP`,
		`CREATE TEMP TABLE workflow_open_loops (id text, workflow_id text, status text, follow_up_at timestamptz) ON COMMIT DROP`,
		`CREATE TEMP TABLE context_memories (id text, owner_identity text, archived boolean) ON COMMIT DROP`,
		`CREATE TEMP TABLE verification_runs (id text, owner_identity text) ON COMMIT DROP`,
		`CREATE TEMP TABLE verification_claims (id text, run_id text, status text, needs_review boolean) ON COMMIT DROP`,
		`CREATE TEMP TABLE ambient_opportunities (id text, owner_identity text, source_type text, status text, requires_approval boolean) ON COMMIT DROP`,
		`CREATE TEMP TABLE ambient_scans (id text, owner_identity text, status text, started_at timestamptz) ON COMMIT DROP`,
	}
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("create metric test table: %v", err)
		}
	}
}

func seedMetricTestRows(t *testing.T, db *gorm.DB) {
	t.Helper()
	const seed = `
INSERT INTO automations (id, status) VALUES ('a1', 'healthy'), ('a2', 'warning');
INSERT INTO automation_alerts (id, status) VALUES ('alert-open', 'open'), ('alert-resolved', 'resolved');

INSERT INTO connected_sources (id, owner_identity, status) VALUES
	('source-alice', 'alice', 'active'),
	('source-alice-revoked', 'alice', 'revoked'),
	('source-bob', 'bob', 'active');
INSERT INTO source_extractions (id, source_id, archived, uncertain, sensitive) VALUES
	('extraction-alice', 'source-alice', false, true, false),
	('extraction-alice-archived', 'source-alice', true, true, true),
	('extraction-alice-revoked', 'source-alice-revoked', false, true, true),
	('extraction-bob', 'source-bob', false, true, true);

INSERT INTO workflow_items (id, owner_identity, current_state, approval_status, archived) VALUES
	('workflow-alice-approval', 'alice', 'needs_approval', 'pending', false),
	('workflow-alice-active', 'alice', 'ready', 'approved', false),
	('workflow-alice-archived', 'alice', 'blocked', 'pending', true),
	('workflow-bob', 'bob', 'blocked', 'pending', false);
INSERT INTO workflow_proposals (id, workflow_id, status) VALUES
	('proposal-alice', 'workflow-alice-active', 'open'),
	('proposal-archived', 'workflow-alice-archived', 'open'),
	('proposal-bob', 'workflow-bob', 'open');
INSERT INTO workflow_quality_gates (id, workflow_id, status) VALUES
	('quality-alice', 'workflow-alice-active', 'failed'),
	('quality-archived', 'workflow-alice-archived', 'needs_review'),
	('quality-bob', 'workflow-bob', 'failed');
INSERT INTO workflow_open_loops (id, workflow_id, status, follow_up_at) VALUES
	('loop-alice', 'workflow-alice-active', 'open', CURRENT_TIMESTAMP - INTERVAL '1 hour'),
	('loop-alice-future', 'workflow-alice-active', 'open', CURRENT_TIMESTAMP + INTERVAL '1 day'),
	('loop-archived', 'workflow-alice-archived', 'open', NULL),
	('loop-bob', 'workflow-bob', 'open', NULL);

INSERT INTO context_memories (id, owner_identity, archived) VALUES
	('memory-alice', 'alice', false), ('memory-alice-archived', 'alice', true), ('memory-bob', 'bob', false);
INSERT INTO verification_runs (id, owner_identity) VALUES ('run-alice', 'alice'), ('run-bob', 'bob');
INSERT INTO verification_claims (id, run_id, status, needs_review) VALUES
	('claim-alice', 'run-alice', 'uncertain', true), ('claim-bob', 'run-bob', 'unsupported', true);

INSERT INTO ambient_opportunities (id, owner_identity, source_type, status, requires_approval) VALUES
	('ambient-alice', 'alice', 'pursuit_stale', 'proposed', true),
	('ambient-not-prefix', 'alice', 'pursuitx', 'proposed', true),
	('ambient-bob', 'bob', 'pursuit_stale', 'proposed', true);
INSERT INTO ambient_scans (id, owner_identity, status, started_at) VALUES
	('scan-alice-old', 'alice', 'failed', CURRENT_TIMESTAMP - INTERVAL '1 day'),
	('scan-alice-new', 'alice', 'completed', CURRENT_TIMESTAMP),
	('scan-bob', 'bob', 'failed', CURRENT_TIMESTAMP + INTERVAL '1 hour');`
	if err := db.Exec(seed).Error; err != nil {
		t.Fatalf("seed metric test rows: %v", err)
	}
}
