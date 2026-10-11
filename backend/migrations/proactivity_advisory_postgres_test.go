package migrations_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/infra"
	"automation-hub-backend/internal/proactivity"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestProactivityAdvisoryMigrationLifecycle(t *testing.T) {
	db := openProactivityMigrationDatabase(t)
	const version = "pre/0020_proactivity_advisory"
	files := migrationFilesThrough(t, version)
	if _, err := infra.ApplyMigrations(db, files, "pre"); err != nil {
		t.Fatalf("apply pre migrations: %v", err)
	}
	assertProactivityTablesEmpty(t, db)

	if err := infra.RollbackMigration(db, files, "pre", version); err != nil {
		t.Fatalf("roll back empty proactivity advisory migration: %v", err)
	}
	if proactivityRelationExists(t, db, "proactivity_idempotency") {
		t.Fatal("proactivity_idempotency still exists after empty rollback")
	}
	if count, err := infra.ApplyMigrations(db, files, "pre"); err != nil || count != 1 {
		t.Fatalf("reapply proactivity migration = (%d, %v), want (1, nil)", count, err)
	}
	owner := "migration-proactivity-rollback-" + uuid.NewString()
	if err := db.Exec(`
		INSERT INTO public.proactivity_idempotency
			(owner_identity, idempotency_key, record_kind, payload_digest, recorded_at)
		VALUES (?, 'retained', 'policy', ?, now())`, owner, strings.Repeat("a", 64)).Error; err != nil {
		t.Fatalf("insert rollback-protection fixture: %v", err)
	}
	var appliedAt time.Time
	if err := db.Raw(`SELECT applied_at FROM public.schema_migrations WHERE version = ?`, version).Row().Scan(&appliedAt); err != nil {
		t.Fatalf("read applied migration timestamp: %v", err)
	}
	if err := infra.RollbackMigration(db, files, "pre", version); err == nil || !strings.Contains(err.Error(), "refusing to roll back non-empty proactivity advisory tables") {
		t.Fatalf("non-empty rollback error = %v, want retained-data refusal", err)
	}
	assertProactivityTablesEmptyExceptIdempotency(t, db)
	if got := proactivityRelationExists(t, db, "proactivity_idempotency"); !got {
		t.Fatal("refused rollback removed proactivity idempotency table")
	}
	var retained int64
	if err := db.Raw(`SELECT count(*) FROM public.proactivity_idempotency WHERE owner_identity = ? AND idempotency_key = 'retained'`, owner).Scan(&retained).Error; err != nil || retained != 1 {
		t.Fatalf("retained idempotency rows = %d, err = %v; want one preserved row", retained, err)
	}
	assertRollbackMigrationLedger(t, db, version, true)
	var afterRollback time.Time
	if err := db.Raw(`SELECT applied_at FROM public.schema_migrations WHERE version = ?`, version).Row().Scan(&afterRollback); err != nil || !afterRollback.Equal(appliedAt) {
		t.Fatalf("applied_at after refused rollback = %s, err = %v; want unchanged %s", afterRollback, err, appliedAt)
	}

	// The current service evaluates owner attention controls. Apply the later
	// schema in a separate database so rollback assertions above remain in
	// migration order and cannot be masked by later applied versions.
	db = openProactivityMigrationDatabase(t)
	currentFiles := migrationFilesThrough(t, "pre/0048_proactivity_attention_feedback")
	if _, err := infra.ApplyMigrations(db, currentFiles, "pre"); err != nil {
		t.Fatalf("apply current proactivity migrations: %v", err)
	}

	owner = "migration-proactivity-" + uuid.NewString()
	repository := proactivity.NewPostgresRepository(db)
	service := proactivity.NewService(repository)
	policy, created, err := service.RecordPolicy(context.Background(), owner, "policy-1", proactivity.DefaultPreferences(owner))
	if err != nil || !created || policy.OwnerIdentity != owner {
		t.Fatalf("record policy: created=%v owner=%q err=%v", created, policy.OwnerIdentity, err)
	}
	replayed, created, err := service.RecordPolicy(context.Background(), owner, "policy-1", proactivity.DefaultPreferences(owner))
	if err != nil || created || replayed.RecordedAt != policy.RecordedAt {
		t.Fatalf("replay policy: created=%v record=%#v err=%v", created, replayed, err)
	}
	if _, _, err := service.RecordPolicy(context.Background(), "other-owner", "policy-1", proactivity.DefaultPreferences("other-owner")); err != nil {
		t.Fatalf("owner-scoped idempotency rejected another owner: %v", err)
	}
	now := time.Now().UTC().Add(-time.Minute)
	signals, created, err := service.RecordSignals(context.Background(), owner, "signals-1", []proactivity.OpenLoopSignal{{
		ContractVersion: proactivity.ContractVersion,
		OwnerIdentity:   owner,
		ID:              "signal-1",
		OpenLoopKey:     "open-loop-1",
		Title:           "Review the open loop",
		Summary:         "A source-backed open loop requires an advisory decision.",
		Status:          proactivity.StatusOpen,
		Risk:            proactivity.RiskMedium,
		ObservedAt:      now,
		LastActivityAt:  now,
		StaleAfter:      24 * time.Hour,
		Impact:          0.8,
		Urgency:         0.7,
		Confidence:      0.9,
	}})
	if err != nil || !created || len(signals) != 1 {
		t.Fatalf("record signal batch: created=%v signals=%#v err=%v", created, signals, err)
	}
	decisionBatch, created, err := service.EvaluateStored(context.Background(), owner, proactivity.EvaluateStoredRequest{
		IdempotencyKey: "decisions-1",
		Now:            time.Now().UTC(),
	})
	if err != nil || !created || len(decisionBatch.Result.Decisions) != 1 {
		t.Fatalf("record decision batch: created=%v batch=%#v err=%v", created, decisionBatch, err)
	}
	decision := decisionBatch.Result.Decisions[0]
	if decision.ExecutionAuthorized || decision.DeliveryAuthorized || decision.AuthorityGranted {
		t.Fatalf("stored decision gained authority: %#v", decision)
	}

	if err := db.Exec(`UPDATE public.proactivity_policy_records SET recorded_at = now() WHERE owner_identity = ?`, owner).Error; err == nil || !strings.Contains(err.Error(), "append-only") {
		t.Fatalf("immutable update error = %v", err)
	}
	if err := db.Exec(`TRUNCATE public.proactivity_policy_records`).Error; err == nil || !strings.Contains(err.Error(), "append-only") {
		t.Fatalf("truncate protection error = %v", err)
	}
	if err := db.Exec(`
		INSERT INTO public.proactivity_idempotency
			(owner_identity, idempotency_key, record_kind, payload_digest, recorded_at)
		VALUES (?, 'unsafe-decision', 'decisions', ?, now())`, owner, strings.Repeat("a", 64)).Error; err != nil {
		t.Fatalf("insert unsafe idempotency fixture: %v", err)
	}
	unsafePayload := `{"contractVersion":1,"ownerIdentity":"` + owner + `","result":{"contractVersion":1,"ownerIdentity":"` + owner + `","decidedAt":"2026-08-01T12:00:00Z","timeZone":"UTC","interruptionsUsed":0,"interruptionsRemaining":0,"decisions":[{"contractVersion":1,"ownerIdentity":"` + owner + `","signalId":"signal-1","openLoopKey":"loop-1","outcome":"ambient","executionAuthorized":true,"deliveryAuthorized":false,"authorityGranted":false,"decidedAt":"2026-08-01T12:00:00Z"}]},"recordedAt":"2026-08-01T12:00:00Z"}`
	if err := db.Exec(`
		INSERT INTO public.proactivity_decision_batches
			(owner_identity, idempotency_key, record_kind, payload_digest, decision_count, recorded_at, payload)
		VALUES (?, 'unsafe-decision', 'decisions', ?, 1, '2026-08-01T12:00:00Z', CAST(? AS jsonb))`,
		owner, strings.Repeat("a", 64), unsafePayload).Error; err == nil {
		t.Fatal("authority-bearing decision batch was accepted")
	}

}

func assertProactivityTablesEmptyExceptIdempotency(t *testing.T, db *gorm.DB) {
	t.Helper()
	for _, table := range []string{
		"proactivity_decision_records", "proactivity_decision_batches",
		"proactivity_signal_records", "proactivity_signal_batches",
		"proactivity_policy_records",
	} {
		var count int64
		if err := db.Raw("SELECT count(*) FROM public." + table).Scan(&count).Error; err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if count != 0 {
			t.Fatalf("expected no rows in %s, got %d", table, count)
		}
	}
}

func TestProactivityAdvisoryMigrationRejectsIncompleteBatch(t *testing.T) {
	db := openProactivityMigrationDatabase(t)
	files := migrationFilesThrough(t, "pre/0020_proactivity_advisory")
	if _, err := infra.ApplyMigrations(db, files, "pre"); err != nil {
		t.Fatalf("apply pre migrations: %v", err)
	}
	assertProactivityTablesEmpty(t, db)

	owner := "migration-proactivity-batch-" + uuid.NewString()
	digest := strings.Repeat("b", 64)
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`
			INSERT INTO public.proactivity_idempotency
				(owner_identity, idempotency_key, record_kind, payload_digest, recorded_at)
			VALUES (?, 'signals-incomplete', 'signals', ?, '2026-08-01T12:00:00Z')`, owner, digest).Error; err != nil {
			return err
		}
		payload := `[{"contractVersion":1,"ownerIdentity":"` + owner + `","signal":{"contractVersion":1,"ownerIdentity":"` + owner + `","id":"signal-1"},"recordedAt":"2026-08-01T12:00:00Z"}]`
		return tx.Exec(`
			INSERT INTO public.proactivity_signal_batches
				(owner_identity, idempotency_key, record_kind, payload_digest, signal_count, recorded_at, payload)
			VALUES (?, 'signals-incomplete', 'signals', ?, 1, '2026-08-01T12:00:00Z', CAST(? AS jsonb))`, owner, digest, payload).Error
	})
	if err == nil || !strings.Contains(err.Error(), "child count is inconsistent") {
		t.Fatalf("incomplete batch commit error = %v", err)
	}
}

func openProactivityMigrationDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	return openIsolatedMigrationDatabase(t)
}

func assertProactivityTablesEmpty(t *testing.T, db *gorm.DB) {
	t.Helper()
	for _, table := range []string{
		"proactivity_decision_records", "proactivity_decision_batches",
		"proactivity_signal_records", "proactivity_signal_batches",
		"proactivity_policy_records", "proactivity_idempotency",
	} {
		var count int64
		if err := db.Raw("SELECT count(*) FROM public." + table).Scan(&count).Error; err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if count != 0 {
			t.Skipf("dedicated destructive test database required; %s contains %d records", table, count)
		}
	}
}

func proactivityRelationExists(t *testing.T, db *gorm.DB, relation string) bool {
	t.Helper()
	var exists bool
	if err := db.Raw(`SELECT to_regclass('public.' || ?) IS NOT NULL`, relation).Row().Scan(&exists); err != nil {
		t.Fatalf("check relation %s: %v", relation, err)
	}
	return exists
}
