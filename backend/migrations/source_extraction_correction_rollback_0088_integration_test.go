//go:build integration

package migrations_test

import (
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"automation-hub-backend/internal/infra"
	"automation-hub-backend/migrations"

	"github.com/google/uuid"
)

const sourceExtractionCorrectionMigrationVersion = "pre/0088_source_extraction_correction_outbox"

func TestSourceExtractionCorrectionMigration0088RollbackPreservesIntentAndOutbox(t *testing.T) {
	db := openIsolatedMigrationDatabase(t)
	files := sourceExtractionCorrection0088Files(t)
	if err := db.Exec(`CREATE EXTENSION IF NOT EXISTS "uuid-ossp"`).Error; err != nil {
		t.Fatalf("create UUID extension prerequisite: %v", err)
	}
	if err := db.Exec(`CREATE TABLE public.durable_jobs (id UUID PRIMARY KEY, kind TEXT NOT NULL)`).Error; err != nil {
		t.Fatalf("create minimal durable-job fixture: %v", err)
	}
	count, err := infra.ApplyMigrations(db, files, "pre")
	if err != nil {
		t.Fatalf("apply 0088 migration: %v", err)
	}
	if count != 1 {
		t.Fatalf("ApplyMigrations applied %d migration(s), want exactly 1", count)
	}

	jobID, correctionID := uuid.New(), uuid.New()
	if err := db.Exec(`INSERT INTO public.durable_jobs (id, kind) VALUES (?, 'source.extraction_correction')`, jobID).Error; err != nil {
		t.Fatalf("insert persisted correction outbox job: %v", err)
	}
	if err := db.Exec(`
		INSERT INTO public.source_extraction_corrections
			(id, owner_identity, source_id, extraction_id, idempotency_key_hash, request_hash,
			 expected_revision, patch_json, before_state_json, phase, status, durable_job_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?::jsonb, ?::jsonb, 'intent_persisted', 'pending', ?)`,
		correctionID, "owner:rollback-regression", uuid.New(), uuid.New(), strings.Repeat("a", 64),
		strings.Repeat("b", 64), time.Now().UTC(), `{"summary":"corrected"}`, `{}`, jobID,
	).Error; err != nil {
		t.Fatalf("insert persisted correction intent: %v", err)
	}

	err = infra.RollbackMigration(db, files, "pre", sourceExtractionCorrectionMigrationVersion)
	if err == nil || !strings.Contains(err.Error(), "source extraction correction history exists") {
		t.Fatalf("rollback error = %v, want populated-history refusal", err)
	}
	if !db.Migrator().HasTable("source_extraction_corrections") {
		t.Fatal("rollback removed the correction table despite persisted work")
	}
	var correctionRows, outboxRows int64
	if err := db.Table("source_extraction_corrections").Where("id = ?", correctionID).Count(&correctionRows).Error; err != nil {
		t.Fatalf("check preserved correction intent: %v", err)
	}
	if err := db.Table("durable_jobs").Where("id = ?", jobID).Count(&outboxRows).Error; err != nil {
		t.Fatalf("check preserved outbox job: %v", err)
	}
	if correctionRows != 1 || outboxRows != 1 {
		t.Fatalf("rollback preservation counts: correction=%d job=%d, want 1/1", correctionRows, outboxRows)
	}
	var migrationApplied bool
	if err := db.Raw("SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = ?)", sourceExtractionCorrectionMigrationVersion).Row().Scan(&migrationApplied); err != nil {
		t.Fatalf("check migration ledger after refused rollback: %v", err)
	}
	if !migrationApplied {
		t.Fatal("refused rollback removed the 0088 migration ledger entry")
	}
}

func sourceExtractionCorrection0088Files(t *testing.T) fs.FS {
	t.Helper()
	files := fstest.MapFS{"pre": &fstest.MapFile{Mode: fs.ModeDir}}
	name := strings.TrimPrefix(sourceExtractionCorrectionMigrationVersion, "pre/")
	for _, suffix := range []string{".up.sql", ".down.sql"} {
		contents, err := migrations.Files.ReadFile("pre/" + name + suffix)
		if err != nil {
			t.Fatalf("read 0088 migration %s: %v", suffix, err)
		}
		files["pre/"+name+suffix] = &fstest.MapFile{Data: contents, Mode: 0o600}
	}
	return files
}
