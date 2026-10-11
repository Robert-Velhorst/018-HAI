package source

import (
	"errors"
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
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatalf("begin transaction: %v", tx.Error)
	}
	t.Cleanup(func() { _ = tx.Rollback().Error })
	if err := tx.Exec(`
		CREATE TEMP TABLE connected_sources (
			id uuid PRIMARY KEY,
			owner_identity varchar(255)
		) ON COMMIT DROP;
		CREATE TEMP TABLE source_extractions (
			id uuid PRIMARY KEY,
			source_id uuid NOT NULL,
			project_key varchar(255),
			summary text,
			archived boolean DEFAULT false,
			updated_at timestamptz
		) ON COMMIT DROP
	`).Error; err != nil {
		t.Fatalf("create temporary source tables: %v", err)
	}

	aliceSource := uuid.New()
	bobSource := uuid.New()
	if err := tx.Exec(
		"INSERT INTO connected_sources (id, owner_identity) VALUES (?, ?), (?, ?)",
		aliceSource, "alice", bobSource, "bob",
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
	}

	repository := &GormRepository{DB: tx}
	items, err := repository.FindRecentExtractionsForSources(
		[]uuid.UUID{aliceSource}, "018-HAI", false, 2,
	)
	if err != nil {
		t.Fatalf("FindRecentExtractionsForSources: %v", err)
	}
	if len(items) != 2 || items[0].ID != aliceLatestHigh || items[1].ID != aliceLatestLow {
		t.Fatalf("bounded owner history = %#v", items)
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
