package memory

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestGormRepositoryQueryForOwnerUsesDatabaseIsolationAndLiteralSearch(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("HAI_TEST_DATABASE_DSN"))
	if dsn == "" {
		t.Skip("HAI_TEST_DATABASE_DSN not set; skipping PostgreSQL memory query test")
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
		CREATE TEMP TABLE context_memories (
			id uuid PRIMARY KEY,
			owner_identity varchar(255),
			project_key varchar(255),
			kind varchar(50),
			content text NOT NULL,
			summary text,
			tags varchar(512),
			confidence numeric,
			source_uri varchar(1024),
			source_label varchar(255),
			content_hash varchar(64),
			archived boolean DEFAULT false,
			last_used_at timestamptz,
			created_at timestamptz,
			updated_at timestamptz
		) ON COMMIT DROP
	`).Error; err != nil {
		t.Fatalf("create temporary memory table: %v", err)
	}

	base := time.Date(2026, time.August, 14, 10, 0, 0, 0, time.UTC)
	rows := []models.ContextMemory{
		{ID: uuid.MustParse("00000000-0000-0000-0000-000000000001"), OwnerIdentity: "alice", ProjectKey: "hai", Kind: "preference", Content: "Local routing is 100%_safe and reviewed.", Tags: "llm, Routing ", Confidence: 0.8, CreatedAt: base, UpdatedAt: base.Add(time.Minute)},
		{ID: uuid.MustParse("00000000-0000-0000-0000-000000000002"), OwnerIdentity: "alice", ProjectKey: "hai", Kind: "preference", Content: "Local routing is 100%_safe but archived.", Tags: "routing", Confidence: 0.7, Archived: true, CreatedAt: base, UpdatedAt: base.Add(2 * time.Minute)},
		{ID: uuid.MustParse("00000000-0000-0000-0000-000000000003"), OwnerIdentity: "bob", ProjectKey: "hai", Kind: "preference", Content: "Local routing is 100%_safe and private.", Tags: "routing", Confidence: 0.6, CreatedAt: base, UpdatedAt: base.Add(3 * time.Minute)},
		{ID: uuid.MustParse("00000000-0000-0000-0000-000000000004"), OwnerIdentity: "alice", ProjectKey: "other", Kind: "preference", Content: "Local routing is 100%_safe elsewhere.", Tags: "routing", Confidence: 0.5, CreatedAt: base, UpdatedAt: base.Add(4 * time.Minute)},
	}
	if err := tx.Create(&rows).Error; err != nil {
		t.Fatalf("seed memories: %v", err)
	}

	repository := &GormRepository{DB: tx}
	result, err := repository.QueryForOwner(t.Context(), "alice", "hai", false, QueryParams{
		Search: "local 100%_safe", Kind: "PREFERENCE", Tag: "routing",
		Sort: "confidence", Order: "asc", Page: 1, PageSize: 10,
	})
	if err != nil {
		t.Fatalf("QueryForOwner: %v", err)
	}
	if result.Total != 1 || len(result.Items) != 1 || result.Items[0].OwnerIdentity != "alice" || result.Items[0].ID != rows[0].ID {
		t.Fatalf("owner query result = %#v", result)
	}

	withArchived, err := repository.QueryForOwner(t.Context(), "alice", "hai", true, QueryParams{
		Search: "100%_safe", Tag: "routing", Order: "asc", PageSize: 1, Page: 2,
	})
	if err != nil {
		t.Fatalf("QueryForOwner including archived: %v", err)
	}
	if withArchived.Total != 2 || withArchived.TotalPages != 2 || len(withArchived.Items) != 1 || !withArchived.Items[0].Archived {
		t.Fatalf("archived page = %#v", withArchived)
	}

	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := repository.QueryForOwner(cancelled, "alice", "hai", false, QueryParams{}); err == nil {
		t.Fatal("cancelled database query unexpectedly succeeded")
	}
}
