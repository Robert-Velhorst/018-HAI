//go:build integration

package migrations_test

import (
	"io/fs"
	"testing"
	"testing/fstest"

	"automation-hub-backend/internal/infra"
	"automation-hub-backend/migrations"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

const contextMemorySourceExtractionIdentity0090 = "pre/0090_context_memory_source_extraction_identity"

func TestContextMemorySourceExtractionIdentity0090ApplyAndRefusesPopulatedRollback(t *testing.T) {
	db := openIsolatedMigrationDatabase(t)
	files := contextMemorySourceExtractionIdentity0090Files(t)
	createContextMemorySourceExtractionIdentity0090Fixture(t, db)

	ownerASourceID := uuid.New()
	ownerASecondSourceID := uuid.New()
	ownerBSourceID := uuid.New()
	emptyOwnerSourceID := uuid.New()
	insertIdentitySource(t, db, ownerASourceID, "owner-a")
	insertIdentitySource(t, db, ownerASecondSourceID, "owner-a")
	insertIdentitySource(t, db, ownerBSourceID, "owner-b")
	insertIdentitySource(t, db, emptyOwnerSourceID, "")

	uniqueExtractionID := uuid.New()
	uniqueURI := "https://example.invalid/evidence/CaseSensitive%2Fletter-42.pdf?ref=Exact"
	insertIdentityExtraction(t, db, uniqueExtractionID, ownerASourceID, uniqueURI)

	duplicateExtractionID := uuid.New()
	duplicateURI := "https://example.invalid/evidence/duplicate-lessons.pdf"
	insertIdentityExtraction(t, db, duplicateExtractionID, ownerASourceID, duplicateURI)

	ambiguousURI := "https://example.invalid/evidence/ambiguous.pdf"
	insertIdentityExtraction(t, db, uuid.New(), ownerASourceID, ambiguousURI)
	insertIdentityExtraction(t, db, uuid.New(), ownerASecondSourceID, ambiguousURI)

	ownerMismatchURI := "https://example.invalid/evidence/other-owner.pdf"
	insertIdentityExtraction(t, db, uuid.New(), ownerBSourceID, ownerMismatchURI)
	emptyOwnerURI := "https://example.invalid/evidence/empty-owner.pdf"
	insertIdentityExtraction(t, db, uuid.New(), emptyOwnerSourceID, emptyOwnerURI)

	canonicalExtractionID := uuid.New()
	canonicalURI := "source-extraction://" + canonicalExtractionID.String()
	insertIdentityExtraction(t, db, canonicalExtractionID, ownerASourceID, canonicalURI)

	uniqueMemoryID := uuid.New()
	ambiguousMemoryID := uuid.New()
	ownerMismatchMemoryID := uuid.New()
	emptyOwnerMemoryID := uuid.New()
	duplicateMemoryOneID := uuid.New()
	duplicateMemoryTwoID := uuid.New()
	canonicalMemoryID := uuid.New()
	nearTagMemoryID := uuid.New()
	wrongKindMemoryID := uuid.New()
	byteMismatchMemoryID := uuid.New()
	ordinaryMemoryID := uuid.New()
	for _, row := range []struct {
		id        uuid.UUID
		ownerName string
		kind      string
		tags      string
		sourceURI string
	}{
		{uniqueMemoryID, "owner-a", "lesson", "connected-source,source-correction,correction,email", uniqueURI},
		{ambiguousMemoryID, "owner-a", "lesson", "source-correction", ambiguousURI},
		{ownerMismatchMemoryID, "owner-a", "lesson", "source-correction", ownerMismatchURI},
		{emptyOwnerMemoryID, "", "lesson", "source-correction", emptyOwnerURI},
		{duplicateMemoryOneID, "owner-a", "lesson", "source-correction", duplicateURI},
		{duplicateMemoryTwoID, "owner-a", "lesson", "connected-source,source-correction,correction", duplicateURI},
		{canonicalMemoryID, "owner-a", "lesson", "", canonicalURI},
		{nearTagMemoryID, "owner-a", "lesson", "not-source-correction", uniqueURI},
		{wrongKindMemoryID, "owner-a", "preference", "source-correction", uniqueURI},
		{byteMismatchMemoryID, "owner-a", "lesson", "source-correction", uniqueURI + " "},
		{ordinaryMemoryID, "owner-a", "preference", "", "https://example.invalid/evidence/ordinary.pdf"},
	} {
		insertIdentityMemory(t, db, row.id, row.ownerName, row.kind, row.tags, row.sourceURI)
	}

	if applied, err := infra.ApplyMigrations(db, files, "pre"); err != nil || applied != 1 {
		t.Fatalf("apply 0090 = (%d, %v), want (1, nil)", applied, err)
	}
	assertExtractionIdentityRow(t, db, uniqueMemoryID, uniqueExtractionID.String(), uniqueURI)
	assertExtractionIdentityRow(t, db, ambiguousMemoryID, "", ambiguousURI)
	assertExtractionIdentityRow(t, db, ownerMismatchMemoryID, "", ownerMismatchURI)
	assertExtractionIdentityRow(t, db, emptyOwnerMemoryID, "", emptyOwnerURI)
	assertExtractionIdentityRow(t, db, duplicateMemoryOneID, "", duplicateURI)
	assertExtractionIdentityRow(t, db, duplicateMemoryTwoID, "", duplicateURI)
	assertExtractionIdentityRow(t, db, canonicalMemoryID, canonicalExtractionID.String(), canonicalURI)
	assertExtractionIdentityRow(t, db, nearTagMemoryID, "", uniqueURI)
	assertExtractionIdentityRow(t, db, wrongKindMemoryID, "", uniqueURI)
	assertExtractionIdentityRow(t, db, byteMismatchMemoryID, "", uniqueURI+" ")
	assertExtractionIdentityRow(t, db, ordinaryMemoryID, "", "https://example.invalid/evidence/ordinary.pdf")

	duplicateErr := db.Exec(`
		INSERT INTO public.context_memories (id, owner_identity, kind, tags, source_uri, source_extraction_id)
		VALUES (?, ?, ?, ?, ?, ?)`,
		uuid.New(), "owner-a", "lesson", "source-correction", "https://example.invalid/duplicate.pdf", uniqueExtractionID,
	).Error
	if duplicateErr == nil {
		t.Fatal("unique owner/extraction/kind index accepted a duplicate identity")
	}

	if err := infra.RollbackMigration(db, files, "pre", contextMemorySourceExtractionIdentity0090); err == nil {
		t.Fatal("rollback 0090 succeeded while populated provenance exists")
	}
	assertExtractionIdentitySchema(t, db, true)
	assertExtractionIdentityRow(t, db, uniqueMemoryID, uniqueExtractionID.String(), uniqueURI)
	assertExtractionIdentityRow(t, db, canonicalMemoryID, canonicalExtractionID.String(), canonicalURI)
	assertSourceURIUnchanged(t, db, ambiguousMemoryID, ambiguousURI)
	assertSourceURIUnchanged(t, db, ownerMismatchMemoryID, ownerMismatchURI)
	assertSourceURIUnchanged(t, db, emptyOwnerMemoryID, emptyOwnerURI)
	assertSourceURIUnchanged(t, db, duplicateMemoryOneID, duplicateURI)
	assertSourceURIUnchanged(t, db, duplicateMemoryTwoID, duplicateURI)
	assertSourceURIUnchanged(t, db, ordinaryMemoryID, "https://example.invalid/evidence/ordinary.pdf")
}

func contextMemorySourceExtractionIdentity0090Files(t *testing.T) fs.FS {
	t.Helper()
	files := fstest.MapFS{"pre": &fstest.MapFile{Mode: fs.ModeDir}}
	for _, suffix := range []string{".up.sql", ".down.sql"} {
		name := contextMemorySourceExtractionIdentity0090 + suffix
		contents, err := migrations.Files.ReadFile(name)
		if err != nil {
			t.Fatalf("read migration %s: %v", name, err)
		}
		files[name] = &fstest.MapFile{Data: contents, Mode: 0o600}
	}
	return files
}

func createContextMemorySourceExtractionIdentity0090Fixture(t *testing.T, db *gorm.DB) {
	t.Helper()
	for _, statement := range []string{
		`CREATE TABLE public.connected_sources (
			id uuid PRIMARY KEY,
			owner_identity varchar(255)
		)`,
		`CREATE TABLE public.source_extractions (
			id uuid PRIMARY KEY,
			source_id uuid NOT NULL,
			source_uri varchar(1024)
		)`,
		`CREATE TABLE public.context_memories (
			id uuid PRIMARY KEY,
			owner_identity varchar(255),
			kind varchar(50),
			tags varchar(512),
			source_uri varchar(1024)
		)`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("create pre-0090 fixture table: %v", err)
		}
	}
}

func insertIdentitySource(t *testing.T, db *gorm.DB, id uuid.UUID, owner string) {
	t.Helper()
	if err := db.Exec(`INSERT INTO public.connected_sources (id, owner_identity) VALUES (?, ?)`, id, owner).Error; err != nil {
		t.Fatalf("insert connected source %s: %v", id, err)
	}
}

func insertIdentityExtraction(t *testing.T, db *gorm.DB, id, sourceID uuid.UUID, sourceURI string) {
	t.Helper()
	if err := db.Exec(`INSERT INTO public.source_extractions (id, source_id, source_uri) VALUES (?, ?, ?)`, id, sourceID, sourceURI).Error; err != nil {
		t.Fatalf("insert source extraction %s: %v", id, err)
	}
}

func insertIdentityMemory(t *testing.T, db *gorm.DB, id uuid.UUID, owner, kind, tags, sourceURI string) {
	t.Helper()
	if err := db.Exec(`
		INSERT INTO public.context_memories (id, owner_identity, kind, tags, source_uri)
		VALUES (?, ?, ?, ?, ?)`, id, owner, kind, tags, sourceURI,
	).Error; err != nil {
		t.Fatalf("insert pre-0090 context memory %s: %v", id, err)
	}
}

func assertExtractionIdentityRow(t *testing.T, db *gorm.DB, id uuid.UUID, wantExtractionID, wantSourceURI string) {
	t.Helper()
	var gotExtractionID, gotSourceURI string
	if err := db.Raw(`
		SELECT COALESCE(source_extraction_id::text, ''), source_uri
		FROM public.context_memories WHERE id = ?`, id,
	).Row().Scan(&gotExtractionID, &gotSourceURI); err != nil {
		t.Fatalf("read migrated memory row %s: %v", id, err)
	}
	if gotExtractionID != wantExtractionID || gotSourceURI != wantSourceURI {
		t.Fatalf("migrated row %s extraction/evidence=(%q, %q), want (%q, %q)", id, gotExtractionID, gotSourceURI, wantExtractionID, wantSourceURI)
	}
}

func assertExtractionIdentitySchema(t *testing.T, db *gorm.DB, wantPresent bool) {
	t.Helper()
	var columnPresent bool
	if err := db.Raw(`
		SELECT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_schema = 'public' AND table_name = 'context_memories'
			  AND column_name = 'source_extraction_id'
		)`).Scan(&columnPresent).Error; err != nil {
		t.Fatalf("check source extraction column: %v", err)
	}
	var indexPresent bool
	if err := db.Raw("SELECT to_regclass('public.ux_context_memories_owner_source_extraction_kind') IS NOT NULL").Scan(&indexPresent).Error; err != nil {
		t.Fatalf("check source extraction unique index: %v", err)
	}
	if columnPresent != wantPresent || indexPresent != wantPresent {
		t.Fatalf("source extraction schema column/index=(%t, %t), want both %t", columnPresent, indexPresent, wantPresent)
	}
}

func assertSourceURIUnchanged(t *testing.T, db *gorm.DB, id uuid.UUID, want string) {
	t.Helper()
	var got string
	if err := db.Raw("SELECT source_uri FROM public.context_memories WHERE id = ?", id).Scan(&got).Error; err != nil {
		t.Fatalf("read source URI after rollback for %s: %v", id, err)
	}
	if got != want {
		t.Fatalf("source URI after rollback for %s = %q, want unchanged %q", id, got, want)
	}
}
