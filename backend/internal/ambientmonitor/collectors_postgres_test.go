package ambientmonitor

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestPostgresFixedCollectorsSourceRows(t *testing.T) {
	db := openAmbientMonitorPostgresTestDatabase(t)
	now := time.Date(2026, time.September, 30, 12, 0, 0, 0, time.UTC)
	scope := Scope{OwnerID: "collector-" + uuid.NewString(), WorkspaceID: "collector-fixture"}
	foreignOwner := "foreign-" + uuid.NewString()
	expected := map[SourceKind]sourceSnapshot{
		SourceWorkflowOpenLoopCount:           {Count: 260, Records: make([]sourceSnapshotRecord, 0, 256)},
		SourceWorkflowVerifiedCompletionCount: {Count: 260, Records: make([]sourceSnapshotRecord, 0, 256)},
		SourceOverdueCommitmentCount:          {Count: 260, Records: make([]sourceSnapshotRecord, 0, 256)},
	}
	seed := db.WithContext(t.Context()).Begin()
	if seed.Error != nil {
		t.Fatal(seed.Error)
	}
	t.Cleanup(func() { _ = seed.Rollback().Error })
	for i := 0; i < 260; i++ {
		due := now.Add(time.Duration(i-260) * time.Minute)
		sourceAt := now.Add(-time.Hour)
		loop := insertCollectorOpenLoop(t, seed, scope.OwnerID, "pending", false, "open", &due, sourceAt)
		verification := "verified"
		if i%2 == 1 {
			verification = "test_passed"
		}
		completion := insertCollectorCompletion(t, seed, scope.OwnerID, verification, now.Add(-time.Duration(i)*time.Minute), i)
		key := fmt.Sprintf("commitment-%03d", i)
		insertCollectorCommitment(t, seed, scope.OwnerID, key, 1, "active", &due, sourceAt)
		statuses := []string{"proposed", "active", "waiting", "breached", "disputed"}
		commitment := insertCollectorCommitment(t, seed, scope.OwnerID, key, 2, statuses[i%len(statuses)], &due, sourceAt)
		if i < 256 {
			for kind, row := range map[SourceKind]sourceSnapshotRecord{
				SourceWorkflowOpenLoopCount: loop, SourceWorkflowVerifiedCompletionCount: completion,
				SourceOverdueCommitmentCount: commitment,
			} {
				value := expected[kind]
				value.Records = append(value.Records, row)
				expected[kind] = value
			}
		}
	}
	// Qualifying boundary/null rows are outside the bounded snapshot but count.
	insertCollectorOpenLoop(t, seed, scope.OwnerID, "pending", false, "open", nil, now)
	insertCollectorOpenLoop(t, seed, scope.OwnerID, "pending", false, "open", &now, now)
	value := expected[SourceWorkflowOpenLoopCount]
	value.Count += 2
	expected[SourceWorkflowOpenLoopCount] = value
	future := now.Add(time.Minute)
	past := now.Add(-time.Minute)
	for _, row := range []struct {
		owner, state, status string
		archived             bool
		due                  *time.Time
	}{
		{foreignOwner, "pending", "open", false, &past},
		{scope.OwnerID, "completed", "open", false, &past},
		{scope.OwnerID, "archived", "open", false, &past},
		{scope.OwnerID, "pending", "open", true, &past},
		{scope.OwnerID, "pending", "closed", false, &past},
		{scope.OwnerID, "pending", "open", false, &future},
	} {
		insertCollectorOpenLoop(t, seed, row.owner, row.state, row.archived, row.status, row.due, now)
	}
	insertCollectorCompletion(t, seed, foreignOwner, "verified", now, 300)
	insertCollectorCommitment(t, seed, foreignOwner, "foreign", 1, "active", &past, now)
	for _, row := range []struct {
		key, status string
		due         *time.Time
	}{
		{"fulfilled", "fulfilled", &past}, {"cancelled", "cancelled", &past},
		{"future", "active", &future}, {"boundary", "active", &now}, {"no-due", "active", nil},
	} {
		insertCollectorCommitment(t, seed, scope.OwnerID, row.key, 1, "active", &past, now)
		insertCollectorCommitment(t, seed, scope.OwnerID, row.key, 2, row.status, row.due, now)
	}
	if err := seed.Commit().Error; err != nil {
		t.Fatal(err)
	}

	readDB := db.WithContext(t.Context()).Begin(&sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if readDB.Error != nil {
		t.Fatal(readDB.Error)
	}
	t.Cleanup(func() { _ = readDB.Rollback().Error })
	var readOnly string
	if err := readDB.Raw("SHOW transaction_read_only").Scan(&readOnly).Error; err != nil || readOnly != "on" {
		t.Fatalf("collector transaction read-only = %q, %v", readOnly, err)
	}
	collector, err := NewGormCollector(readDB, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	reader := &gormCollectorSourceReader{db: readDB}
	for _, kind := range []SourceKind{SourceWorkflowOpenLoopCount, SourceWorkflowVerifiedCompletionCount, SourceOverdueCommitmentCount} {
		t.Run(string(kind), func(t *testing.T) {
			want := expected[kind]
			var snapshot sourceSnapshot
			var err error
			switch kind {
			case SourceWorkflowOpenLoopCount:
				snapshot, err = reader.workflowOpenLoops(t.Context(), scope.OwnerID, now)
			case SourceWorkflowVerifiedCompletionCount:
				snapshot, err = reader.workflowVerifiedCompletions(t.Context(), scope.OwnerID)
			case SourceOverdueCommitmentCount:
				snapshot, err = reader.overdueCommitments(t.Context(), scope.OwnerID, now)
			}
			if err != nil || !reflect.DeepEqual(snapshot, want) {
				t.Fatalf("source rows differ from fixture: err=%v count=%d/%d rows=%d/%d", err, snapshot.Count, want.Count, len(snapshot.Records), len(want.Records))
			}
			wantDigest, err := sourceSnapshotDigest(scope, kind, now, want)
			if err != nil {
				t.Fatal(err)
			}
			target := MonitorTarget{Scope: scope, SourceKind: kind}
			first, err := collector.Collect(t.Context(), target)
			if err != nil || first.Value != float64(want.Count) || first.SourceDigest != wantDigest || !first.ObservedAt.Equal(now) {
				t.Fatalf("real collector = %+v, %v; want count=%d digest=%s", first, err, want.Count, wantDigest)
			}
			second, err := collector.Collect(t.Context(), target)
			if err != nil || first != second {
				t.Fatalf("unchanged source digest replay = %+v, %v", second, err)
			}
			target.Scope.WorkspaceID = "different-workspace"
			other, err := collector.Collect(t.Context(), target)
			if err != nil || other.Value != first.Value || other.SourceDigest == first.SourceDigest {
				t.Fatalf("workspace digest binding = %+v, %v", other, err)
			}
			target.Scope = Scope{OwnerID: "empty-" + uuid.NewString(), WorkspaceID: scope.WorkspaceID}
			empty, err := collector.Collect(t.Context(), target)
			emptyDigest, digestErr := sourceSnapshotDigest(target.Scope, kind, now, sourceSnapshot{Records: []sourceSnapshotRecord{}})
			if err != nil || digestErr != nil || empty.Value != 0 || empty.SourceDigest != emptyDigest {
				t.Fatalf("empty owner collector = %+v, %v; digest error=%v", empty, err, digestErr)
			}
			t.Logf("real source acceptance: count=%d snapshotRows=%d digest=%s readOnly=%s", want.Count, len(want.Records), first.SourceDigest, readOnly)
		})
	}
}

func collectorFixtureExec(t *testing.T, db *gorm.DB, query string, args ...any) {
	t.Helper()
	if err := db.Exec(query, args...).Error; err != nil {
		t.Fatalf("seed synthetic collector fixture: %v", err)
	}
}

func insertCollectorOpenLoop(t *testing.T, db *gorm.DB, owner, state string, archived bool, status string, due *time.Time, sourceAt time.Time) sourceSnapshotRecord {
	t.Helper()
	workflowID, loopID := uuid.NewString(), uuid.NewString()
	collectorFixtureExec(t, db, `INSERT INTO workflow_items (id, owner_identity, title, current_state, archived, updated_at) VALUES (?, ?, 'collector fixture', ?, ?, ?)`, workflowID, owner, state, archived, sourceAt)
	collectorFixtureExec(t, db, `INSERT INTO workflow_open_loops (id, workflow_id, status, follow_up_at, updated_at) VALUES (?, ?, ?, ?, ?)`, loopID, workflowID, status, due, sourceAt)
	return sourceSnapshotRecord{RecordID: loopID, ParentID: workflowID, State: status, DueAt: due, SourceAt: sourceAt}
}

func insertCollectorCompletion(t *testing.T, db *gorm.DB, owner, verification string, at time.Time, index int) sourceSnapshotRecord {
	t.Helper()
	workflowID, attestationID := uuid.NewString(), uuid.NewString()
	digest := fmt.Sprintf("%064x", index+1)
	collectorFixtureExec(t, db, `INSERT INTO workflow_items (id, owner_identity, title, current_state, archived, last_task_plan_id, verification_status, completed_at) VALUES (?, ?, 'collector fixture', 'completed', false, 'fixture-plan', ?, ?)`, workflowID, owner, verification, at)
	collectorFixtureExec(t, db, `INSERT INTO workflow_completion_attestations
		(id, workflow_id, owner_identity, task_plan_id, completion_status, verification_status, runtime_id, runtime_evidence_uri, runtime_evidence_digest, result_digest, record_digest, completed_at, attested_at)
		VALUES (?, ?, ?, 'fixture-plan', 'completed', ?, 'fixture-runtime', 'fixture://collector', ?, ?, ?, ?, ?)`, attestationID, workflowID, owner, verification, digest, digest, digest, at, at)
	return sourceSnapshotRecord{RecordID: attestationID, ParentID: workflowID, State: verification, RecordDigest: digest, SourceAt: at}
}

func insertCollectorCommitment(t *testing.T, db *gorm.DB, owner, key string, revision int64, status string, due *time.Time, at time.Time) sourceSnapshotRecord {
	t.Helper()
	digest := strings.Repeat("a", 64)
	idempotencyKey := fmt.Sprintf("%s-%d", key, revision)
	payload := map[string]any{
		"contractVersion": "life-ledger.v1", "ownerIdentity": owner, "commitmentKey": key,
		"revision": revision, "idempotencyKey": idempotencyKey, "requestDigest": digest,
		"recordDigest": digest, "localOnly": true, "status": status, "projectKey": "fixture-project",
	}
	if due != nil {
		payload["dueAt"] = due.UTC().Format(time.RFC3339Nano)
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	collectorFixtureExec(t, db, `INSERT INTO life_ledger_commitment_revisions
		(owner_identity, commitment_key, revision, idempotency_key, request_digest, record_digest, observed_at, recorded_at, payload)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?::jsonb)`, owner, key, revision, idempotencyKey, digest, digest, at, at, string(encoded))
	return sourceSnapshotRecord{RecordID: key, ParentID: "fixture-project", State: status, RecordDigest: digest, Revision: revision, DueAt: due, SourceAt: at}
}
