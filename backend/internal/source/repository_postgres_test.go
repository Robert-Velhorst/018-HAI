package source

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestGormRepositoryBoundsExtractionHistoryAndScopesLookupPostgres(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("HAI_TEST_DATABASE_DSN"))
	if dsn == "" {
		t.Skip("HAI_TEST_DATABASE_DSN not set; skipping PostgreSQL source-history query test")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open PostgreSQL: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get PostgreSQL pool: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatalf("begin transaction: %v", tx.Error)
	}
	t.Cleanup(func() { _ = tx.Rollback().Error })
	if err := tx.Exec(`
		CREATE TEMP TABLE source_sync_jobs (
			id uuid PRIMARY KEY,
			source_id uuid NOT NULL,
			owner_identity text,
			durable_job_id uuid,
			mode text NOT NULL,
			status text NOT NULL,
			message text DEFAULT '',
			completed_at timestamptz,
			created_at timestamptz NOT NULL
		) ON COMMIT DROP;
		CREATE TEMP TABLE durable_jobs (
			id uuid PRIMARY KEY,
			kind text NOT NULL,
			status text NOT NULL
		) ON COMMIT DROP;
		CREATE TEMP TABLE source_audit_logs (
			id uuid PRIMARY KEY,
			source_id uuid NOT NULL,
			action text DEFAULT 'source.read',
			message text DEFAULT '',
			created_at timestamptz NOT NULL
		) ON COMMIT DROP;
		CREATE TEMP TABLE connected_sources (
			id uuid PRIMARY KEY,
			owner_identity varchar(255),
			updated_at timestamptz DEFAULT now()
		) ON COMMIT DROP;
		CREATE TEMP TABLE source_extractions (
			id uuid PRIMARY KEY,
			source_id uuid NOT NULL,
			raw_item_id uuid DEFAULT '00000000-0000-0000-0000-000000000000',
			project_key varchar(255) DEFAULT '',
			content_type varchar(80) DEFAULT '',
			text text DEFAULT '',
			summary text DEFAULT '',
			entities text DEFAULT '',
			dates text DEFAULT '',
			tasks text DEFAULT '',
			decisions text DEFAULT '',
			follow_ups text DEFAULT '',
			source_uri varchar(1024) DEFAULT '',
			source_label varchar(512) DEFAULT '',
			content_hash varchar(64) DEFAULT '',
			sensitive boolean DEFAULT false,
			uncertain boolean DEFAULT false,
			archived boolean DEFAULT false,
			last_indexed_at timestamptz,
			created_at timestamptz NOT NULL DEFAULT now(),
			updated_at timestamptz NOT NULL DEFAULT now()
		) ON COMMIT DROP
	`).Error; err != nil {
		t.Fatalf("create temporary source tables: %v", err)
	}

	aliceSource := uuid.New()
	bobSource := uuid.New()
	legacySource := uuid.New()
	if err := tx.Exec(
		"INSERT INTO connected_sources (id, owner_identity) VALUES (?, ?), (?, ?), (?, NULL)",
		aliceSource, "alice", bobSource, "bob", legacySource,
	).Error; err != nil {
		t.Fatalf("seed sources: %v", err)
	}
	base := time.Date(2026, time.August, 15, 8, 0, 0, 0, time.UTC)
	aliceOldest := uuid.New()
	alicePrevious := uuid.New()
	aliceLatestLow := uuid.MustParse("10000000-0000-0000-0000-000000000001")
	aliceLatestHigh := uuid.MustParse("10000000-0000-0000-0000-000000000002")
	bobLatest := uuid.New()
	for _, row := range []struct {
		id        uuid.UUID
		sourceID  uuid.UUID
		summary   string
		updatedAt time.Time
	}{
		{aliceOldest, aliceSource, "Alice oldest", base},
		{alicePrevious, aliceSource, "Alice previous", base.Add(time.Minute)},
		{aliceLatestLow, aliceSource, "Alice latest low ID", base.Add(2 * time.Minute)},
		{aliceLatestHigh, aliceSource, "Alice latest high ID", base.Add(2 * time.Minute)},
		{bobLatest, bobSource, "Bob private", base.Add(3 * time.Minute)},
	} {
		if err := tx.Exec(
			"INSERT INTO source_extractions (id, source_id, project_key, summary, archived, updated_at) VALUES (?, ?, ?, ?, false, ?)",
			row.id, row.sourceID, "018-HAI", row.summary, row.updatedAt,
		).Error; err != nil {
			t.Fatalf("seed extraction: %v", err)
		}
		if err := tx.Exec("INSERT INTO source_audit_logs (id, source_id, created_at) VALUES (?, ?, ?)", row.id, row.sourceID, row.updatedAt).Error; err != nil {
			t.Fatalf("seed audit log: %v", err)
		}
	}

	repository := &GormRepository{DB: tx}
	durableID, manualID := uuid.New(), uuid.New()
	if err := tx.Exec("INSERT INTO durable_jobs (id, kind, status) VALUES (?, ?, ?)", durableID, JobKindManualSync, "running").Error; err != nil {
		t.Fatalf("seed durable job: %v", err)
	}
	for _, row := range []struct {
		id      uuid.UUID
		source  uuid.UUID
		owner   any
		mode    string
		durable any
		created time.Time
	}{
		{aliceLatestLow, aliceSource, nil, "incremental", nil, base.Add(2 * time.Minute)},
		{aliceLatestHigh, aliceSource, nil, "incremental", nil, base.Add(2 * time.Minute)},
		{manualID, aliceSource, "alice", ModeManualAsyncSync, durableID, base.Add(3 * time.Minute)},
		{uuid.New(), aliceSource, "bob", ModeManualAsyncSync, nil, base.Add(4 * time.Minute)},
		{uuid.New(), aliceSource, nil, ModeManualAsyncSync, nil, base.Add(5 * time.Minute)},
		{uuid.New(), bobSource, "alice", ModeManualAsyncSync, nil, base.Add(6 * time.Minute)},
		{uuid.New(), legacySource, "alice", ModeManualAsyncSync, nil, base.Add(7 * time.Minute)},
	} {
		if err := tx.Exec("INSERT INTO source_sync_jobs (id, source_id, owner_identity, mode, durable_job_id, status, created_at) VALUES (?, ?, ?, ?, ?, 'queued', ?)", row.id, row.source, row.owner, row.mode, row.durable, row.created).Error; err != nil {
			t.Fatalf("seed sync job: %v", err)
		}
	}
	syncItems, syncTotal, err := repository.FindSyncJobsPageForOwnerSources("alice", []uuid.UUID{aliceSource}, 1, 0)
	if err != nil || syncTotal != 3 || len(syncItems) != 1 || syncItems[0].ID != manualID || syncItems[0].Status != "running" {
		t.Fatalf("owner-filtered projected sync page = %#v total %d err %v", syncItems, syncTotal, err)
	}
	syncItems, syncTotal, err = repository.FindSyncJobsPageForOwnerSources("alice", []uuid.UUID{aliceSource}, 1, 1)
	if err != nil || syncTotal != 3 || len(syncItems) != 1 || syncItems[0].ID != aliceLatestHigh {
		t.Fatalf("owner-filtered offset sync page = %#v total %d err %v", syncItems, syncTotal, err)
	}
	syncItems, syncTotal, err = repository.FindSyncJobsPageForOwnerSources("", []uuid.UUID{aliceSource}, 6, 0)
	if err != nil || syncTotal != 0 || len(syncItems) != 0 {
		t.Fatalf("ownerless sync page = %#v total %d err %v", syncItems, syncTotal, err)
	}
	auditItems, auditTotal, err := repository.FindAuditLogsPageForSources([]uuid.UUID{aliceSource}, 1, 2)
	if err != nil || auditTotal != 4 || len(auditItems) != 1 || auditItems[0].ID != alicePrevious {
		t.Fatalf("offset audit page = items %#v total %d err %v", auditItems, auditTotal, err)
	}
	firstAuditItems, _, err := repository.FindAuditLogsPageForSources([]uuid.UUID{aliceSource}, 2, 0)
	if err != nil || len(firstAuditItems) != 2 || firstAuditItems[0].ID != aliceLatestHigh || firstAuditItems[1].ID != aliceLatestLow {
		t.Fatalf("stable audit ordering = %#v, err %v", firstAuditItems, err)
	}
	emptyAuditItems, emptyAuditTotal, err := repository.FindAuditLogsPageForSources(nil, 8, 0)
	if err != nil || emptyAuditTotal != 0 || len(emptyAuditItems) != 0 {
		t.Fatalf("ownerless audit page = %#v total %d err %v", emptyAuditItems, emptyAuditTotal, err)
	}
	// Exercise the real handler/service/repository path against PostgreSQL. The
	// subject is injected here; IDP token verification remains a separate gate.
	actualService := NewService(repository, nil)
	syncResponse := syncHistoryRequest(actualService, "alice", "offset=1&limit=1&owner=bob")
	var syncPage SyncJobPage
	if syncResponse.Code != http.StatusOK || json.Unmarshal(syncResponse.Body.Bytes(), &syncPage) != nil || syncPage.Total != 3 || len(syncPage.Items) != 1 || syncPage.Items[0].ID != aliceLatestHigh {
		t.Fatalf("PostgreSQL sync HTTP page status=%d body=%s", syncResponse.Code, syncResponse.Body.String())
	}
	bobResponse := syncHistoryRequest(actualService, "bob", "offset=0&limit=6&owner=alice")
	if bobResponse.Code != http.StatusOK || json.Unmarshal(bobResponse.Body.Bytes(), &syncPage) != nil || syncPage.Total != 0 || len(syncPage.Items) != 0 {
		t.Fatalf("PostgreSQL foreign-owner HTTP page status=%d body=%s", bobResponse.Code, bobResponse.Body.String())
	}
	auditResponse := auditHistoryRequest(actualService, "alice", "offset=1&limit=1&owner=bob")
	var auditPage AuditLogPage
	if auditResponse.Code != http.StatusOK || json.Unmarshal(auditResponse.Body.Bytes(), &auditPage) != nil || auditPage.Total != 4 || len(auditPage.Items) != 1 || auditPage.Items[0].ID != aliceLatestLow {
		t.Fatalf("PostgreSQL audit HTTP page status=%d body=%s", auditResponse.Code, auditResponse.Body.String())
	}
	items, err := repository.FindRecentExtractionsForSources(
		[]uuid.UUID{aliceSource}, "018-HAI", false, 2,
	)
	if err != nil {
		t.Fatalf("FindRecentExtractionsForSources: %v", err)
	}
	if len(items) != 2 || items[0].ID != aliceLatestHigh || items[1].ID != aliceLatestLow {
		t.Fatalf("bounded owner history = %#v", items)
	}
	pageItems, totalCount, err := repository.FindExtractionPageForSources(
		[]uuid.UUID{aliceSource}, "018-HAI", false, 2,
	)
	if err != nil {
		t.Fatalf("FindExtractionPageForSources: %v", err)
	}
	if totalCount != 4 || len(pageItems) != 2 || pageItems[0].ID != aliceLatestHigh || pageItems[1].ID != aliceLatestLow {
		t.Fatalf("bounded extraction page = items %#v total %d, want latest two of four Alice records", pageItems, totalCount)
	}
	olderItems, olderTotal, err := repository.FindExtractionsPageForSources(
		[]uuid.UUID{aliceSource}, "018-HAI", false, 1, 2,
	)
	if err != nil || olderTotal != 4 || len(olderItems) != 1 || olderItems[0].ID != alicePrevious {
		t.Fatalf("offset extraction page = items %#v total %d err %v, want Alice previous record", olderItems, olderTotal, err)
	}
	emptyItems, emptyTotal, err := repository.FindExtractionsPageForSources(nil, "018-HAI", false, 1, 0)
	if err != nil || emptyTotal != 0 || len(emptyItems) != 0 {
		t.Fatalf("ownerless offset page returned items %#v total %d err %v", emptyItems, emptyTotal, err)
	}

	resolved, err := repository.FindExtractionForOwner(aliceLatestHigh, "alice")
	if err != nil || resolved.ID != aliceLatestHigh {
		t.Fatalf("FindExtractionForOwner Alice = %#v, %v", resolved, err)
	}
	if _, err := repository.FindExtractionForOwner(bobLatest, "alice"); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("foreign extraction lookup error = %v, want record not found", err)
	}
	if _, err := repository.FindExtractionForOwner(aliceLatestHigh, ""); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("ownerless extraction lookup error = %v, want record not found", err)
	}
}
