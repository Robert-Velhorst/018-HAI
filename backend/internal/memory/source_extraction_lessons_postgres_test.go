//go:build integration

package memory

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"automation-hub-backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type sourceLessonPostgresFixture struct {
	db     *gorm.DB
	admin  *gorm.DB
	sqlDB  *sql.DB
	conn   *sql.Conn
	schema string
}

func openSourceLessonPostgresFixture(t *testing.T) *sourceLessonPostgresFixture {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("HAI_TEST_DATABASE_DSN"))
	if dsn == "" {
		t.Skip("HAI_TEST_DATABASE_DSN not set; skipping source-extraction memory PostgreSQL tests")
	}
	admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open test PostgreSQL: %v", err)
	}
	sqlDB, err := admin.DB()
	if err != nil {
		t.Fatalf("get test PostgreSQL pool: %v", err)
	}
	sqlDB.SetMaxOpenConns(12)
	schema := "memory_lesson_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if err := admin.Exec("CREATE SCHEMA " + schema).Error; err != nil {
		_ = sqlDB.Close()
		t.Fatalf("create isolated test schema: %v", err)
	}
	db, conn, err := openSourceLessonPostgresSession(sqlDB, schema)
	if err != nil {
		_ = admin.Exec("DROP SCHEMA " + schema + " CASCADE").Error
		_ = sqlDB.Close()
		t.Fatalf("open isolated test session: %v", err)
	}
	fixture := &sourceLessonPostgresFixture{db: db, admin: admin, sqlDB: sqlDB, conn: conn, schema: schema}
	t.Cleanup(func() {
		_ = db.Exec("SET search_path TO public").Error
		_ = conn.Close()
		_ = admin.Exec("DROP SCHEMA " + schema + " CASCADE").Error
		_ = sqlDB.Close()
	})
	if err := fixture.createTables(); err != nil {
		t.Fatalf("create isolated test tables: %v", err)
	}
	return fixture
}

func openSourceLessonPostgresSession(sqlDB *sql.DB, schema string) (*gorm.DB, *sql.Conn, error) {
	conn, err := sqlDB.Conn(context.Background())
	if err != nil {
		return nil, nil, err
	}
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: conn}), &gorm.Config{})
	if err != nil {
		_ = conn.Close()
		return nil, nil, err
	}
	if err := db.Exec("SET search_path TO " + schema + ", public").Error; err != nil {
		_ = conn.Close()
		return nil, nil, err
	}
	return db, conn, nil
}

func (f *sourceLessonPostgresFixture) createTables() error {
	if err := f.db.Exec(`
		CREATE TABLE context_memories (
			id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
			owner_identity VARCHAR(255) NOT NULL DEFAULT '',
			project_key VARCHAR(255) NOT NULL DEFAULT '',
			kind VARCHAR(50) NOT NULL,
			content TEXT NOT NULL,
			summary TEXT NOT NULL DEFAULT '',
			tags VARCHAR(512) NOT NULL DEFAULT '',
			confidence DOUBLE PRECISION NOT NULL DEFAULT 0.7,
			source_uri VARCHAR(1024) NOT NULL DEFAULT '',
			source_extraction_id UUID NULL,
			source_label VARCHAR(255) NOT NULL DEFAULT '',
			content_hash VARCHAR(64) NOT NULL DEFAULT '',
			archived BOOLEAN NOT NULL DEFAULT FALSE,
			last_used_at TIMESTAMPTZ NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`).Error; err != nil {
		return err
	}
	if err := f.db.Exec(`CREATE UNIQUE INDEX ux_context_memories_owner_source_extraction_kind
		ON context_memories (owner_identity, source_extraction_id, kind)
		WHERE source_extraction_id IS NOT NULL`).Error; err != nil {
		return err
	}
	if err := f.db.AutoMigrate(
		&models.ConnectedSource{},
		&models.SourceRawItem{},
		&models.SourceExtraction{},
		&models.SourceExtractionCorrection{},
		&models.SourceAuditLog{},
		&models.VerificationRun{},
		&models.VerificationClaim{},
		&models.VerificationEvidence{},
		&models.VerificationAuditLog{},
	); err != nil {
		return err
	}
	return f.db.Exec(`CREATE TABLE semantic_memory_embeddings (memory_id UUID PRIMARY KEY, payload TEXT NOT NULL)`).Error
}

func (f *sourceLessonPostgresFixture) seedEvidence(owner, uri, projectKey string) (CreateRequest, error) {
	extractionID, err := canonicalSourceExtractionID(uri)
	if err != nil {
		return CreateRequest{}, err
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	sourceID, rawItemID := uuid.New(), uuid.New()
	evidenceURI := "https://example.invalid/evidence/" + extractionID.String()
	label := "Corrected source extraction"
	source := models.ConnectedSource{
		ID: sourceID, OwnerIdentity: owner, ConnectorKey: "test", Name: "Test source",
		Category: "test", Enabled: true, Status: "active", CreatedAt: now, UpdatedAt: now,
	}
	raw := models.SourceRawItem{
		ID: rawItemID, SourceID: sourceID, ExternalID: extractionID.String(), ProjectKey: projectKey,
		ItemType: "test", SourceURI: evidenceURI, Content: "A source record.", ContentHash: "source-hash",
		FetchedAt: now, CreatedAt: now, UpdatedAt: now,
	}
	extraction := models.SourceExtraction{
		ID: extractionID, SourceID: sourceID, RawItemID: rawItemID, ProjectKey: projectKey,
		ContentType: "test", Text: "A corrected source record.", SourceURI: evidenceURI,
		SourceLabel: label, ContentHash: "source-hash", CreatedAt: now, UpdatedAt: now,
	}
	appliedRevision := now
	correction := models.SourceExtractionCorrection{
		ID: uuid.New(), OwnerIdentity: owner, SourceID: sourceID, ExtractionID: extractionID,
		IdempotencyKeyHash: "idempotency-hash", RequestHash: "request-hash", ExpectedRevision: now,
		PatchJSON: "{}", BeforeStateJSON: "{}", Phase: "graph_projected",
		Status: models.SourceExtractionCorrectionPending, DurableJobID: uuid.New(),
		AppliedRevision: &appliedRevision, CreatedAt: now, UpdatedAt: now,
	}
	for _, value := range []any{&source, &raw, &extraction, &correction} {
		if err := f.db.Create(value).Error; err != nil {
			return CreateRequest{}, err
		}
	}
	request := CreateRequest{
		ProjectKey: projectKey, Kind: "lesson", Content: "A verified source correction lesson.",
		SourceURI: evidenceURI, SourceLabel: label,
	}
	if err := f.db.Create(&models.SourceAuditLog{
		ID: uuid.New(), SourceID: sourceID, Action: "extraction.corrected",
		Message: SourceExtractionCorrectionAuditMessage(extractionID, now, request.Content), CreatedAt: now,
	}).Error; err != nil {
		return CreateRequest{}, err
	}
	return request, nil
}

func TestPostgresManualCorrectionAuditBindsRevisionAndMemoryLockFollowsEvidenceLocks(t *testing.T) {
	f := openSourceLessonPostgresFixture(t)
	owner := "owner-a"
	uri := "source-extraction://" + uuid.NewString()
	extractionID := mustParseSourceExtractionID(t, uri)
	request, err := f.seedEvidence(owner, uri, "case-a")
	if err != nil {
		t.Fatalf("seed correction evidence: %v", err)
	}
	if err := f.db.Where("extraction_id = ?", extractionID).Delete(&models.SourceExtractionCorrection{}).Error; err != nil {
		t.Fatalf("remove durable worker correction: %v", err)
	}
	var extraction models.SourceExtraction
	if err := f.db.First(&extraction, "id = ?", extractionID).Error; err != nil {
		t.Fatalf("load corrected extraction: %v", err)
	}
	audit := models.SourceAuditLog{
		SourceID: extraction.SourceID, Action: "extraction.corrected",
		Message:   SourceExtractionCorrectionAuditMessage(extraction.ID, extraction.UpdatedAt, request.Content),
		CreatedAt: extraction.UpdatedAt.Add(time.Second),
	}
	if err := f.db.Create(&audit).Error; err != nil {
		t.Fatalf("seed exact revision audit: %v", err)
	}

	// Hold source/extraction rows and wait until PostgreSQL confirms that the
	// actual upsert is blocked on one of them before probing lock ordering.
	rowDB, rowConn, err := openSourceLessonPostgresSession(f.sqlDB, f.schema)
	if err != nil {
		t.Fatalf("open source-worker row-lock connection: %v", err)
	}
	defer rowConn.Close()
	rowTx := rowDB.Begin()
	if rowTx.Error != nil {
		t.Fatalf("begin source-worker transaction: %v", rowTx.Error)
	}
	defer rowTx.Rollback()
	var lockedID string
	if err := rowTx.Raw("SELECT id FROM connected_sources WHERE id = ? FOR UPDATE", extraction.SourceID).Scan(&lockedID).Error; err != nil || lockedID != extraction.SourceID.String() {
		t.Fatalf("lock source row: id=%s err=%v", lockedID, err)
	}
	if err := rowTx.Raw("SELECT id FROM source_extractions WHERE id = ? FOR UPDATE", extractionID).Scan(&lockedID).Error; err != nil || lockedID != extractionID.String() {
		t.Fatalf("lock extraction row: id=%s err=%v", lockedID, err)
	}

	memoryDB, memoryConn, err := openSourceLessonPostgresSession(f.sqlDB, f.schema)
	if err != nil {
		t.Fatalf("open independent memory transaction connection: %v", err)
	}
	defer memoryConn.Close()
	applicationName := "hai-memory-lock-" + strings.TrimPrefix(f.schema, "memory_lesson_")
	if err := memoryDB.Exec("SELECT set_config('application_name', ?, false)", applicationName).Error; err != nil {
		t.Fatalf("label the blocked upsert connection: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, persistErr := (&GormRepository{DB: memoryDB.WithContext(ctx)}).UpsertSourceExtractionLesson(owner, uri, request)
		done <- persistErr
	}()
	poll := time.NewTicker(10 * time.Millisecond)
	defer poll.Stop()
	for {
		var blockedOnLock bool
		if err := f.admin.Raw(`SELECT EXISTS (
			SELECT 1 FROM pg_stat_activity
			WHERE application_name = ? AND state = 'active' AND wait_event_type = 'Lock'
		)`, applicationName).Scan(&blockedOnLock).Error; err != nil {
			t.Fatalf("observe blocked upsert in PostgreSQL: %v", err)
		}
		if blockedOnLock {
			break
		}
		select {
		case err := <-done:
			t.Fatalf("upsert completed before the source row lock was released: %v", err)
		case <-ctx.Done():
			t.Fatal("PostgreSQL never observed the upsert waiting on the locked source evidence")
		case <-poll.C:
		}
	}
	probeDB, probeConn, err := openSourceLessonPostgresSession(f.sqlDB, f.schema)
	if err != nil {
		t.Fatalf("open memory-lock probe connection: %v", err)
	}
	defer probeConn.Close()
	probeTx := probeDB.Begin()
	if probeTx.Error != nil {
		t.Fatalf("begin memory-lock probe: %v", probeTx.Error)
	}
	var memoryLockAvailable bool
	if err := probeTx.Raw("SELECT pg_try_advisory_xact_lock(hashtextextended(?, 0))", sourceExtractionMemoryLockKey(owner, extractionID)).Scan(&memoryLockAvailable).Error; err != nil {
		_ = probeTx.Rollback().Error
		t.Fatalf("probe memory advisory lock while row locks are held: %v", err)
	}
	_ = probeTx.Rollback().Error
	if !memoryLockAvailable {
		t.Fatal("blocked upsert held the memory lock before acquiring source/extraction row locks")
	}
	if err := rowTx.Commit().Error; err != nil {
		t.Fatalf("release source-worker row locks: %v", err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("persist manual correction after worker row locks release: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("memory persistence remained blocked after source row locks were released")
	}

	var count int64
	if err := f.db.Model(&models.ContextMemory{}).
		Where("owner_identity = ? AND source_extraction_id = ? AND kind = ?", owner, extractionID, request.Kind).
		Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("exact manual correction lesson count=%d err=%v, want one", count, err)
	}
}

func TestPostgresDurableCorrectionIntentCannotAuthorizeDifferentLessonContent(t *testing.T) {
	f := openSourceLessonPostgresFixture(t)
	owner := "owner-a"
	uri := "source-extraction://" + uuid.NewString()
	extractionID := mustParseSourceExtractionID(t, uri)
	request, err := f.seedEvidence(owner, uri, "case-a")
	if err != nil {
		t.Fatalf("seed correction evidence: %v", err)
	}
	request.Content = "An arbitrary lesson not produced by this durable correction."
	if _, err := (&GormRepository{DB: f.db}).UpsertSourceExtractionLesson(owner, uri, request); err == nil {
		t.Fatal("a durable correction intent without a matching content-bound audit authorized arbitrary lesson content")
	}
	var count int64
	if err := f.db.Model(&models.ContextMemory{}).
		Where("owner_identity = ? AND source_extraction_id = ?", owner, extractionID).
		Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("mismatched lesson persisted %d row(s): err=%v", count, err)
	}
}

func TestPostgresUnrelatedExtractionAuditCannotAuthorizeCorrectionLesson(t *testing.T) {
	f := openSourceLessonPostgresFixture(t)
	owner := "owner-a"
	uri := "source-extraction://" + uuid.NewString()
	extractionID := mustParseSourceExtractionID(t, uri)
	request, err := f.seedEvidence(owner, uri, "case-a")
	if err != nil {
		t.Fatalf("seed correction evidence: %v", err)
	}
	if err := f.db.Where("extraction_id = ?", extractionID).Delete(&models.SourceExtractionCorrection{}).Error; err != nil {
		t.Fatalf("remove durable worker correction: %v", err)
	}
	var extraction models.SourceExtraction
	if err := f.db.First(&extraction, "id = ?", extractionID).Error; err != nil {
		t.Fatalf("load extraction: %v", err)
	}
	if err := f.db.Where("source_id = ? AND action = ? AND message = ?", extraction.SourceID, "extraction.corrected",
		SourceExtractionCorrectionAuditMessage(extraction.ID, extraction.UpdatedAt, request.Content)).
		Delete(&models.SourceAuditLog{}).Error; err != nil {
		t.Fatalf("remove matching audit fixture: %v", err)
	}
	audit := models.SourceAuditLog{
		SourceID: extraction.SourceID, Action: "extraction.corrected",
		Message:   SourceExtractionCorrectionAuditMessage(uuid.New(), extraction.UpdatedAt, request.Content),
		CreatedAt: extraction.UpdatedAt.Add(time.Second),
	}
	if err := f.db.Create(&audit).Error; err != nil {
		t.Fatalf("seed unrelated extraction audit: %v", err)
	}
	if _, err := (&GormRepository{DB: f.db}).UpsertSourceExtractionLesson(owner, uri, request); err == nil {
		t.Fatal("an audit for a different extraction authorized this lesson")
	}
	var count int64
	if err := f.db.Model(&models.ContextMemory{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("unrelated correction audit persisted %d lesson(s): err=%v", count, err)
	}
}

func TestPostgresCorrectionAuditCannotAuthorizeDifferentLessonContent(t *testing.T) {
	f := openSourceLessonPostgresFixture(t)
	owner := "owner-a"
	uri := "source-extraction://" + uuid.NewString()
	extractionID := mustParseSourceExtractionID(t, uri)
	request, err := f.seedEvidence(owner, uri, "case-a")
	if err != nil {
		t.Fatalf("seed correction evidence: %v", err)
	}
	if err := f.db.Where("extraction_id = ?", extractionID).Delete(&models.SourceExtractionCorrection{}).Error; err != nil {
		t.Fatalf("remove durable worker correction: %v", err)
	}
	var extraction models.SourceExtraction
	if err := f.db.First(&extraction, "id = ?", extractionID).Error; err != nil {
		t.Fatalf("load extraction: %v", err)
	}
	audit := models.SourceAuditLog{
		SourceID: extraction.SourceID, Action: "extraction.corrected",
		Message:   SourceExtractionCorrectionAuditMessage(extraction.ID, extraction.UpdatedAt, request.Content),
		CreatedAt: extraction.UpdatedAt.Add(time.Second),
	}
	if err := f.db.Create(&audit).Error; err != nil {
		t.Fatalf("seed exact-content correction audit: %v", err)
	}
	request.Content = "A different lesson not generated from this correction."
	if _, err := (&GormRepository{DB: f.db}).UpsertSourceExtractionLesson(owner, uri, request); err == nil {
		t.Fatal("a correction audit authorized different lesson content")
	}
	var count int64
	if err := f.db.Model(&models.ContextMemory{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("mismatched correction content persisted %d lesson(s): err=%v", count, err)
	}
}

func TestPostgresSourceExtractionLessonUpsertIsExactAndIdempotentUnderConcurrency(t *testing.T) {
	f := openSourceLessonPostgresFixture(t)
	owner := "owner-a"
	uri := "source-extraction://" + uuid.NewString()
	extractionID := mustParseSourceExtractionID(t, uri)
	request, err := f.seedEvidence(owner, uri, "case-a")
	if err != nil {
		t.Fatalf("seed exact correction evidence: %v", err)
	}
	var sourceExtraction models.SourceExtraction
	if err := f.db.First(&sourceExtraction, "id = ?", extractionID).Error; err != nil {
		t.Fatalf("load extraction for alternate lesson audit: %v", err)
	}
	const writers = 8
	var wg sync.WaitGroup
	errs := make(chan error, writers)
	for range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			db, conn, err := openSourceLessonPostgresSession(f.sqlDB, f.schema)
			if err != nil {
				errs <- err
				return
			}
			defer conn.Close()
			_, err = (&GormRepository{DB: db}).UpsertSourceExtractionLesson(owner, uri, request)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent exact-provenance upsert: %v", err)
		}
	}

	var canonicalCount int64
	if err := f.db.Model(&models.ContextMemory{}).
		Where("owner_identity = ? AND source_extraction_id = ? AND kind = ?", owner, extractionID, "lesson").Count(&canonicalCount).Error; err != nil {
		t.Fatalf("count canonical lessons: %v", err)
	}
	if canonicalCount != 1 {
		t.Fatalf("same owner/source/kind produced %d rows, want exactly one", canonicalCount)
	}
	var canonical models.ContextMemory
	if err := f.db.Where("owner_identity = ? AND source_extraction_id = ? AND kind = ?", owner, extractionID, "lesson").First(&canonical).Error; err != nil {
		t.Fatalf("load canonical lesson: %v", err)
	}
	if canonical.Content != request.Content {
		t.Fatalf("canonical content = %q, want exact request content %q", canonical.Content, request.Content)
	}
	if canonical.OwnerIdentity != owner || canonical.SourceURI != request.SourceURI || canonical.SourceExtractionID == nil || *canonical.SourceExtractionID != extractionID {
		t.Fatalf("persisted owner/evidence/extraction=(%q, %q, %v), want owner, original evidence URI, and parsed extraction ID", canonical.OwnerIdentity, canonical.SourceURI, canonical.SourceExtractionID)
	}
	conflicting := request
	conflicting.Content = "A different lesson must not silently replace the first one."
	if _, err := (&GormRepository{DB: f.db}).UpsertSourceExtractionLesson(owner, uri, conflicting); err == nil {
		t.Fatal("conflicting lesson silently overwrote the verified source lesson")
	}
	var afterConflict models.ContextMemory
	if err := f.db.Where("owner_identity = ? AND source_extraction_id = ? AND kind = ?", owner, extractionID, "lesson").First(&afterConflict).Error; err != nil {
		t.Fatalf("read lesson after rejected conflict: %v", err)
	}
	if afterConflict.Content != request.Content {
		t.Fatalf("rejected conflict changed lesson to %q", afterConflict.Content)
	}

	otherRequest := request
	otherRequest.Kind = "correction_lesson"
	otherRequest.Content = "A second verified source correction lesson."
	if err := f.db.Create(&models.SourceAuditLog{
		ID: uuid.New(), SourceID: sourceExtraction.SourceID, Action: "extraction.corrected",
		Message:   SourceExtractionCorrectionAuditMessage(extractionID, sourceExtraction.UpdatedAt, otherRequest.Content),
		CreatedAt: sourceExtraction.UpdatedAt,
	}).Error; err != nil {
		t.Fatalf("seed explicit alternate lesson audit: %v", err)
	}
	if _, err := (&GormRepository{DB: f.db}).UpsertSourceExtractionLesson(owner, uri, otherRequest); err != nil {
		t.Fatalf("persist alternate lesson kind for same source: %v", err)
	}
	otherURI := "source-extraction://" + uuid.NewString()
	otherRequest, err = f.seedEvidence(owner, otherURI, "case-a")
	if err != nil {
		t.Fatalf("seed second source evidence: %v", err)
	}
	if _, err := (&GormRepository{DB: f.db}).UpsertSourceExtractionLesson(owner, otherURI, otherRequest); err != nil {
		t.Fatalf("persist distinct source lesson: %v", err)
	}
	if _, err := (&GormRepository{DB: f.db}).UpsertSourceExtractionLesson("owner-b", uri, request); err == nil {
		t.Fatal("source lesson accepted an extraction owned by another user")
	}
	var total int64
	if err := f.db.Model(&models.ContextMemory{}).Count(&total).Error; err != nil {
		t.Fatalf("count all lessons: %v", err)
	}
	if total != 3 {
		t.Fatalf("exact owner/source/kind lessons collapsed or duplicated: got %d rows, want 3", total)
	}
}

func TestPostgresSourceExtractionLessonFailsClosedForMissingOrStaleCorrection(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*sourceLessonPostgresFixture, uuid.UUID)
	}{
		{
			name: "missing correction record and content-bound audit",
			mutate: func(f *sourceLessonPostgresFixture, extractionID uuid.UUID) {
				var extraction models.SourceExtraction
				if err := f.db.First(&extraction, "id = ?", extractionID).Error; err != nil {
					t.Fatalf("load extraction fixture: %v", err)
				}
				if err := f.db.Where("source_id = ? AND action = ? AND message LIKE ?", extraction.SourceID,
					"extraction.corrected", "%id="+extractionID.String()+"%").
					Delete(&models.SourceAuditLog{}).Error; err != nil {
					t.Fatalf("remove content-bound correction audit: %v", err)
				}
				if err := f.db.Where("extraction_id = ?", extractionID).Delete(&models.SourceExtractionCorrection{}).Error; err != nil {
					t.Fatalf("remove correction fixture: %v", err)
				}
			},
		},
		{
			name: "superseded extraction revision",
			mutate: func(f *sourceLessonPostgresFixture, extractionID uuid.UUID) {
				if err := f.db.Model(&models.SourceExtraction{}).Where("id = ?", extractionID).
					Update("updated_at", time.Now().UTC().Add(-time.Minute)).Error; err != nil {
					t.Fatalf("supersede extraction fixture: %v", err)
				}
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := openSourceLessonPostgresFixture(t)
			uri := "source-extraction://" + uuid.NewString()
			request, err := f.seedEvidence("owner-a", uri, "case-a")
			if err != nil {
				t.Fatalf("seed exact correction evidence: %v", err)
			}
			extractionID := mustParseSourceExtractionID(t, uri)
			test.mutate(f, extractionID)
			if _, err := (&GormRepository{DB: f.db}).UpsertSourceExtractionLesson("owner-a", uri, request); err == nil {
				t.Fatal("lesson promotion accepted missing or superseded correction evidence")
			}
			var count int64
			if err := f.db.Model(&models.ContextMemory{}).Count(&count).Error; err != nil || count != 0 {
				t.Fatalf("rejected promotion persisted records: count=%d err=%v", count, err)
			}
		})
	}
}

func TestPostgresDeleteSourceExtractionLessonsRemovesOnlyExactOwnerProvenanceAndEmbeddings(t *testing.T) {
	f := openSourceLessonPostgresFixture(t)
	uri := "source-extraction://" + uuid.NewString()
	extractionID := mustParseSourceExtractionID(t, uri)
	otherID := uuid.New()
	targets := []models.ContextMemory{
		{ID: uuid.New(), OwnerIdentity: "owner-a", SourceURI: "https://example.invalid/evidence/one", SourceExtractionID: sourceExtractionIDPointer(extractionID), Kind: "correction_lesson", Content: "lesson one"},
		{ID: uuid.New(), OwnerIdentity: "owner-a", SourceURI: "https://example.invalid/evidence/two", SourceExtractionID: sourceExtractionIDPointer(extractionID), Kind: "correction_decision", Content: "lesson two"},
	}
	preserved := []models.ContextMemory{
		{ID: uuid.New(), OwnerIdentity: "owner-b", SourceURI: "https://example.invalid/evidence/other-owner", SourceExtractionID: sourceExtractionIDPointer(extractionID), Kind: "correction_lesson", Content: "different owner"},
		{ID: uuid.New(), OwnerIdentity: "owner-a", SourceURI: "https://example.invalid/evidence/other-extraction", SourceExtractionID: sourceExtractionIDPointer(otherID), Kind: "correction_lesson", Content: "different source"},
		{ID: uuid.New(), OwnerIdentity: "owner-a", SourceURI: "https://example.invalid/evidence/one", Kind: "correction_lesson", Content: "no extraction identity"},
	}
	all := append(append([]models.ContextMemory{}, targets...), preserved...)
	for _, memory := range all {
		if err := f.db.Create(&memory).Error; err != nil {
			t.Fatalf("seed memory %s: %v", memory.ID, err)
		}
		if err := f.db.Exec("INSERT INTO semantic_memory_embeddings (memory_id, payload) VALUES (?, ?)", memory.ID, "vector").Error; err != nil {
			t.Fatalf("seed embedding %s: %v", memory.ID, err)
		}
	}

	deleted, err := DeleteSourceExtractionLessons(f.db, "owner-a", uri)
	if err != nil {
		t.Fatalf("delete exact source lessons: %v", err)
	}
	if deleted != int64(len(targets)) {
		t.Fatalf("deleted %d memory rows, want %d", deleted, len(targets))
	}
	for _, memory := range targets {
		var count int64
		if err := f.db.Model(&models.ContextMemory{}).Where("id = ?", memory.ID).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("target memory %s remains or lookup failed: count=%d err=%v", memory.ID, count, err)
		}
		if err := f.db.Table("semantic_memory_embeddings").Where("memory_id = ?", memory.ID).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("target embedding %s remains or lookup failed: count=%d err=%v", memory.ID, count, err)
		}
	}
	for _, memory := range preserved {
		var count int64
		if err := f.db.Model(&models.ContextMemory{}).Where("id = ?", memory.ID).Count(&count).Error; err != nil || count != 1 {
			t.Fatalf("nonmatching memory %s changed: count=%d err=%v", memory.ID, count, err)
		}
		if err := f.db.Table("semantic_memory_embeddings").Where("memory_id = ?", memory.ID).Count(&count).Error; err != nil || count != 1 {
			t.Fatalf("nonmatching embedding %s changed: count=%d err=%v", memory.ID, count, err)
		}
	}
}

func TestPostgresDeleteSourceExtractionLessonsTxParticipatesInCallerTransaction(t *testing.T) {
	f := openSourceLessonPostgresFixture(t)
	uri := "source-extraction://" + uuid.NewString()
	memory := models.ContextMemory{ID: uuid.New(), OwnerIdentity: "owner-a", SourceURI: "https://example.invalid/evidence/rollback", SourceExtractionID: sourceExtractionIDPointer(mustParseSourceExtractionID(t, uri)), Kind: "correction_lesson", Content: "lesson"}
	if err := f.db.Create(&memory).Error; err != nil {
		t.Fatalf("seed memory: %v", err)
	}
	if err := f.db.Exec("INSERT INTO semantic_memory_embeddings (memory_id, payload) VALUES (?, ?)", memory.ID, "vector").Error; err != nil {
		t.Fatalf("seed embedding: %v", err)
	}
	rollback := errors.New("test rollback")
	err := f.db.Transaction(func(tx *gorm.DB) error {
		if err := lockSourceExtractionMemoryKey(tx, "owner-a", uri); err != nil {
			return err
		}
		deleted, err := DeleteSourceExtractionLessonsTx(tx, "owner-a", uri)
		if err != nil {
			return err
		}
		if deleted != 1 {
			t.Fatalf("transaction helper deleted %d rows, want 1", deleted)
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("outer transaction error = %v, want rollback sentinel", err)
	}
	var count int64
	if err := f.db.Model(&models.ContextMemory{}).Where("id = ?", memory.ID).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("memory row was not restored by caller rollback: count=%d err=%v", count, err)
	}
	if err := f.db.Table("semantic_memory_embeddings").Where("memory_id = ?", memory.ID).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("embedding row was not restored by caller rollback: count=%d err=%v", count, err)
	}
}

func TestPostgresDeleteSourceExtractionLessonsSkipsMissingEmbeddingTable(t *testing.T) {
	f := openSourceLessonPostgresFixture(t)
	uri := "source-extraction://" + uuid.NewString()
	memory := models.ContextMemory{ID: uuid.New(), OwnerIdentity: "owner-a", SourceURI: "https://example.invalid/evidence/no-vector-table", SourceExtractionID: sourceExtractionIDPointer(mustParseSourceExtractionID(t, uri)), Kind: "correction_lesson", Content: "lesson"}
	if err := f.db.Create(&memory).Error; err != nil {
		t.Fatalf("seed memory: %v", err)
	}
	if err := f.db.Exec("DROP TABLE semantic_memory_embeddings").Error; err != nil {
		t.Fatalf("drop optional embedding table: %v", err)
	}
	deleted, err := DeleteSourceExtractionLessons(f.db, "owner-a", uri)
	if err != nil || deleted != 1 {
		t.Fatalf("cleanup without embedding table = (%d, %v), want (1, nil)", deleted, err)
	}
}

func TestPostgresDeleteSourceExtractionLessonsFailsClosedWhenEmbeddingCleanupFails(t *testing.T) {
	f := openSourceLessonPostgresFixture(t)
	uri := "source-extraction://" + uuid.NewString()
	memory := models.ContextMemory{ID: uuid.New(), OwnerIdentity: "owner-a", SourceURI: "https://example.invalid/evidence/vector-cleanup-failure", SourceExtractionID: sourceExtractionIDPointer(mustParseSourceExtractionID(t, uri)), Kind: "correction_lesson", Content: "lesson"}
	if err := f.db.Create(&memory).Error; err != nil {
		t.Fatalf("seed memory: %v", err)
	}
	if err := f.db.Exec("DROP TABLE semantic_memory_embeddings").Error; err != nil {
		t.Fatalf("replace embedding table: %v", err)
	}
	if err := f.db.Exec("CREATE TABLE semantic_memory_embeddings (unrecognized_memory_id UUID PRIMARY KEY)").Error; err != nil {
		t.Fatalf("create malformed embedding table: %v", err)
	}
	if _, err := DeleteSourceExtractionLessons(f.db, "owner-a", uri); err == nil {
		t.Fatal("expected vector cleanup failure")
	}
	var count int64
	if err := f.db.Model(&models.ContextMemory{}).Where("id = ?", memory.ID).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("memory row was deleted despite vector cleanup failure: count=%d err=%v", count, err)
	}
}
