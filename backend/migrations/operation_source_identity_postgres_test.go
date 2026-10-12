package migrations_test

import (
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/infra"
	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/operations"
	"automation-hub-backend/migrations"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Opt-in only: the shared fixture validates every host and creates a fresh,
// explicitly owned database. Compilation or a skip is not PostgreSQL acceptance.
func TestOperationSourceIdentityPostgresUpgradeImmutabilityAndRollback(t *testing.T) {
	db := openIsolatedMigrationDatabase(t)
	const version = "pre/0111_operation_source_identity"
	prior := migrationFilesThrough(t, "pre/0110_account_feed_registry")
	if _, err := infra.ApplyMigrations(db, prior, "pre"); err != nil {
		t.Fatalf("apply predecessor migrations: %v", err)
	}
	legacyID, auditID := uuid.New(), uuid.New()
	legacyURI := "gmail:account:with:colons:external:with:colons"
	legacyRevision := "opaque old revision"
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`INSERT INTO public.operations
 (id, owner_user_id, workspace_id, title, source_type, source_uri,
  source_revision_hash, operation_type, status, risk_level, autonomy_level,
  owner_type, dedupe_key, version)
 VALUES (?, 'source-test-owner', 'local', 'historical', 'manual', ?,
 'opaque old revision', 'internal', 'new', 'low', 'observe', 'hai', ?, 1)`,
			legacyID, legacyURI, uuid.NewString()).Error; err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO public.operation_events
 (id, operation_id, event_type, actor_type, after_status, message, payload_json)
 VALUES (?, ?, 'created', 'hai', 'new', 'preserve history', '{}')`, auditID, legacyID).Error
	}); err != nil {
		t.Fatal(err)
	}
	files := migrationFilesThrough(t, version)
	if n, err := infra.ApplyMigrations(db, files, "pre"); err != nil || n != 1 {
		t.Fatalf("apply identity migration: %d %v", n, err)
	}
	assertLegacy := func(t *testing.T) {
		t.Helper()
		var row models.Operation
		if err := db.First(&row, "id = ?", legacyID).Error; err != nil || row.SourceURI != legacyURI ||
			row.SourceRevisionHash != legacyRevision || row.SourceProvider != "" ||
			row.SourceAccount != "" || row.SourceExternalID != "" || row.SourceIdentityHash != "" {
			t.Fatalf("historical identity inferred/changed: %+v %v", row, err)
		}
		var events int64
		if err := db.Model(&models.OperationEvent{}).Where("id = ? AND operation_id = ?", auditID, legacyID).Count(&events).Error; err != nil || events != 1 {
			t.Fatalf("historical event changed: %d %v", events, err)
		}
		var event models.OperationEvent
		if err := db.First(&event, "id = ?", auditID).Error; err != nil || event.OperationID != legacyID ||
			event.EventType != "created" || event.ActorType != "hai" || event.AfterStatus != "new" ||
			event.Message != "preserve history" || event.PayloadJSON != "{}" {
			t.Fatalf("historical audit fields changed: %+v %v", event, err)
		}
	}
	assertLegacy(t)
	if err := infra.RollbackMigration(db, files, "pre", version); err != nil {
		t.Fatalf("empty-identity rollback: %v", err)
	}
	assertLegacy(t)
	if n, err := infra.ApplyMigrations(db, files, "pre"); err != nil || n != 1 {
		t.Fatalf("reapply identity migration: %d %v", n, err)
	}
	// Keep 0111 genuinely latest: these writes name only its stage's columns.
	insertStageOperation := func(op models.Operation, eventID uuid.UUID) error {
		return db.Transaction(func(tx *gorm.DB) error {
			if err := tx.Exec(`INSERT INTO public.operations
 (id, owner_user_id, workspace_id, title, source_type, source_uri,
  source_provider, source_account, source_external_id, source_identity_hash,
  source_revision_hash, operation_type, status, risk_level, autonomy_level,
  owner_type, dedupe_key, version, evidence_json)
 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, '{}'::jsonb)`,
				op.ID, op.OwnerUserID, op.WorkspaceID, op.Title, op.SourceType, op.SourceURI,
				op.SourceProvider, op.SourceAccount, op.SourceExternalID, op.SourceIdentityHash,
				op.SourceRevisionHash, op.OperationType, op.Status, op.RiskLevel, op.AutonomyLevel,
				op.OwnerType, op.DedupeKey, op.Version).Error; err != nil {
				return err
			}
			return tx.Exec(`INSERT INTO public.operation_events
 (id, operation_id, event_type, actor_type, after_status, message, payload_json)
 VALUES (?, ?, 'created', 'hai', 'new', 'operation created from manual', '{}')`, eventID, op.ID).Error
		})
	}
	digest, err := operations.SourceIdentityDigest("gmail", "account:with:colons", "external:with:colons")
	if err != nil {
		t.Fatal(err)
	}
	first := models.Operation{
		ID: uuid.New(), OwnerUserID: "source-test-owner", WorkspaceID: "local", Title: "identified",
		SourceType: "manual", SourceURI: legacyURI, SourceProvider: "gmail",
		SourceAccount: "account:with:colons", SourceExternalID: "external:with:colons",
		SourceIdentityHash: digest, SourceRevisionHash: "opaque revision one", OperationType: "internal",
		Status: "new", RiskLevel: "low", AutonomyLevel: "observe", OwnerType: "hai",
		DedupeKey: uuid.NewString(), Version: 1,
	}
	second := first
	second.ID, second.DedupeKey, second.SourceRevisionHash = uuid.New(), uuid.NewString(), "opaque revision two"
	stageRows := []models.Operation{first, second}
	stageEvents := []uuid.UUID{uuid.New(), uuid.New()}
	for i, op := range stageRows {
		if err := insertStageOperation(op, stageEvents[i]); err != nil {
			t.Fatalf("stage-shaped identified revision insert: %v", err)
		}
	}
	assertStageRetained := func(t *testing.T) {
		t.Helper()
		for i, want := range stageRows {
			var row models.Operation
			if err := db.First(&row, "id = ?", want.ID).Error; err != nil ||
				row.OwnerUserID != want.OwnerUserID || row.WorkspaceID != want.WorkspaceID ||
				row.Title != want.Title || row.OperationType != want.OperationType || row.Status != want.Status ||
				row.RiskLevel != want.RiskLevel || row.AutonomyLevel != want.AutonomyLevel || row.OwnerType != want.OwnerType ||
				row.SourceType != want.SourceType || row.SourceURI != want.SourceURI ||
				row.SourceProvider != want.SourceProvider || row.SourceAccount != want.SourceAccount ||
				row.SourceExternalID != want.SourceExternalID || row.SourceIdentityHash != want.SourceIdentityHash ||
				row.SourceRevisionHash != want.SourceRevisionHash || row.DedupeKey != want.DedupeKey || row.Version != 1 ||
				row.SourceObservationID != nil || row.SourceObservationGeneration != 0 || row.AccountFeedID != nil {
				t.Fatalf("stage provenance changed: %+v %v", row, err)
			}
			var event models.OperationEvent
			if err := db.First(&event, "id = ?", stageEvents[i]).Error; err != nil || event.OperationID != want.ID ||
				event.EventType != "created" || event.ActorType != "hai" || event.AfterStatus != "new" ||
				event.Message != "operation created from manual" || event.PayloadJSON != "{}" {
				t.Fatalf("stage audit changed: %+v %v", event, err)
			}
		}
	}
	assertStageRetained(t)
	alternateDigest, err := operations.SourceIdentityDigest(first.SourceProvider, first.SourceAccount, "another external record")
	if err != nil {
		t.Fatal(err)
	}
	for _, mutation := range []struct{ field, value string }{
		{"source_provider", "another-provider"}, {"source_account", "another-account"},
		{"source_external_id", "another external record"}, {"source_identity_hash", alternateDigest},
		{"source_revision_hash", "another opaque revision"},
	} {
		assertSourceObservationSQLState(t, db.Exec("UPDATE public.operations SET "+mutation.field+" = ? WHERE id = ?",
			mutation.value, first.ID).Error, "23000")
	}
	legacyDigest, err := operations.SourceIdentityDigest("gmail", "primary", "legacy")
	if err != nil {
		t.Fatal(err)
	}
	assertSourceObservationSQLState(t, db.Exec(`UPDATE public.operations SET source_provider = 'gmail', source_account = 'primary',
 source_external_id = 'legacy', source_identity_hash = ? WHERE id = ?`, legacyDigest, legacyID).Error, "23000")
	if err := db.Exec(`UPDATE public.operations SET source_revision_hash = ? WHERE id = ?`, "legacy edit still allowed", legacyID).Error; err != nil {
		t.Fatalf("unidentified legacy revision compatibility: %v", err)
	}
	legacyRevision = "legacy edit still allowed"
	invalid := first
	invalid.ID, invalid.DedupeKey = uuid.New(), uuid.NewString()
	invalid.SourceAccount = ""
	assertSourceObservationSQLState(t, insertStageOperation(invalid, uuid.New()), "23514")
	invalid.SourceAccount, invalid.SourceRevisionHash = first.SourceAccount, ""
	assertSourceObservationSQLState(t, insertStageOperation(invalid, uuid.New()), "23514")
	if err := db.Exec(`UPDATE public.operations SET updated_at = ? WHERE id = ?`, time.Now().UTC(), first.ID).Error; err != nil {
		t.Fatalf("ordinary update blocked: %v", err)
	}
	err = infra.RollbackMigration(db, files, "pre", version)
	assertSourceObservationSQLState(t, err, "55000")
	if !strings.Contains(err.Error(), "rollback refused") {
		t.Fatalf("populated identity rollback must refuse: %v", err)
	}
	var count int64
	if err := db.Model(&models.Operation{}).Count(&count).Error; err != nil || count != 3 {
		t.Fatalf("refused rollback changed operations: %d %v", count, err)
	}
	var events int64
	if err := db.Model(&models.OperationEvent{}).Count(&events).Error; err != nil || events != 3 {
		t.Fatalf("refused inserts/rollback changed audit history: %d %v", events, err)
	}
	assertLegacy(t)
	assertStageRetained(t)
	var applied int64
	if err := db.Table("schema_migrations").Where("version = ?", version).Count(&applied).Error; err != nil || applied != 1 {
		t.Fatalf("refused rollback changed migration ledger: %d %v", applied, err)
	}

	t.Run("current writer after full schema upgrade", func(t *testing.T) {
		if _, err := infra.ApplyMigrations(db, migrations.Files, "pre"); err != nil {
			t.Fatalf("upgrade full pre schema: %v", err)
		}
		if _, err := infra.ApplyMigrations(db, migrations.Files, "post"); err != nil {
			t.Fatalf("upgrade full post schema: %v", err)
		}
		in := operations.NewOperationInput{
			OwnerUserID: "source-test-owner", WorkspaceID: "local", Title: "current unobserved identified intake",
			OperationType: "internal", SourceType: "manual", SourceURI: "manual:current-unobserved",
			DedupeKey: uuid.NewString(), SourceProvider: "manual", SourceAccount: "current-writer",
			SourceExternalID: "distinct-unobserved-record", SourceRevisionHash: "current opaque revision", EvidenceJSON: `{}`,
		}
		wantDigest, err := operations.SourceIdentityDigest(in.SourceProvider, in.SourceAccount, in.SourceExternalID)
		if err != nil {
			t.Fatal(err)
		}
		created, err := operations.NewService(operations.NewGormRepository(db)).IngestContext(t.Context(), in)
		if err != nil || !created.Created || created.Operation.ID == uuid.Nil || created.Operation.SourceIdentityHash != wantDigest {
			t.Fatalf("current identified intake: %+v %v", created, err)
		}
		var row models.Operation
		if err := db.First(&row, "id = ?", created.Operation.ID).Error; err != nil ||
			row.OwnerUserID != in.OwnerUserID || row.WorkspaceID != in.WorkspaceID || row.Title != in.Title ||
			row.SourceType != in.SourceType || row.SourceURI != in.SourceURI || row.SourceProvider != in.SourceProvider ||
			row.SourceAccount != in.SourceAccount || row.SourceExternalID != in.SourceExternalID ||
			row.SourceIdentityHash != wantDigest || row.SourceRevisionHash != in.SourceRevisionHash ||
			row.DedupeKey != in.DedupeKey || row.Version != 1 || row.SourceObservationID != nil ||
			row.SourceObservationGeneration != 0 || row.AccountFeedID != nil {
			t.Fatalf("current unobserved provenance not persisted faithfully: %+v %v", row, err)
		}
		var audits []models.OperationEvent
		if err := db.Where("operation_id = ?", row.ID).Find(&audits).Error; err != nil || len(audits) != 1 {
			t.Fatalf("current intake audit: %+v %v", audits, err)
		}
		if event := audits[0]; event.ID == uuid.Nil || event.EventType != "created" || event.ActorType != "hai" ||
			event.AfterStatus != "new" || event.Message != "operation created from manual" || event.PayloadJSON != "{}" {
			t.Fatalf("current intake audit fields: %+v", event)
		}
		assertLegacy(t)
		assertStageRetained(t)
		if err := db.Model(&models.Operation{}).Count(&count).Error; err != nil || count != 4 {
			t.Fatalf("full upgrade/current intake changed operation history: %d %v", count, err)
		}
		if err := db.Model(&models.OperationEvent{}).Count(&events).Error; err != nil || events != 4 {
			t.Fatalf("full upgrade/current intake changed audit history: %d %v", events, err)
		}
		for _, table := range []string{"operation_source_observation_clocks", "operation_source_observations", "operation_source_origins", "operation_source_heads"} {
			var authorityRows int64
			if err := db.Table("public." + table).Count(&authorityRows).Error; err != nil || authorityRows != 0 {
				t.Fatalf("unobserved intake manufactured %s authority: %d %v", table, authorityRows, err)
			}
		}
	})
}
