package memory

import (
	"database/sql"
	"strings"
	"testing"

	"automation-hub-backend/internal/models"

	_ "github.com/jackc/pgx/v5/stdlib"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestMemoryOwnerScopeAllowsLegacyRowsOnlyForConfiguredOwner(t *testing.T) {
	t.Setenv("HAI_LEGACY_DATA_OWNER_IDENTITY", "legacy-owner")
	sqlDB, err := sql.Open("pgx", "postgres://unused")
	if err != nil {
		t.Fatalf("open dry-run connection: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{
		DryRun:               true,
		DisableAutomaticPing: true,
	})
	if err != nil {
		t.Fatalf("open dry-run gorm: %v", err)
	}
	repository := &GormRepository{DB: db}

	operatorSQL := strings.ToLower(repository.scopeQuery(db, "operator", "", false).Find(&[]models.ContextMemory{}).Statement.SQL.String())
	if strings.Contains(operatorSQL, " is null") || strings.Contains(operatorSQL, " or ") {
		t.Fatalf("operator query includes ownerless rows: %s", operatorSQL)
	}
	legacySQL := strings.ToLower(repository.scopeQuery(db, "legacy-owner", "", false).Find(&[]models.ContextMemory{}).Statement.SQL.String())
	if !strings.Contains(legacySQL, " is null") || !strings.Contains(legacySQL, " or ") {
		t.Fatalf("migration-owner query excludes ownerless rows: %s", legacySQL)
	}
}
