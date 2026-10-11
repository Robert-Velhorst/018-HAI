package verification

import (
	"database/sql"
	"strings"
	"testing"

	"automation-hub-backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestVerificationRunsForOwnerQueryExcludesOwnerlessLegacyRows(t *testing.T) {
	t.Setenv("HAI_LEGACY_DATA_OWNER_IDENTITY", "legacy-owner")
	sqlDB, err := sql.Open("pgx", "postgres://unused")
	if err != nil {
		t.Fatalf("open dry-run connection: %v", err)
	}
	defer sqlDB.Close()
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{
		DryRun:               true,
		DisableAutomaticPing: true,
	})
	if err != nil {
		t.Fatalf("open dry-run gorm: %v", err)
	}

	var runs []models.VerificationRun
	statement := verificationRunsForOwnerQuery(db, "alice").Find(&runs).Statement
	query := strings.ToLower(statement.SQL.String())
	if !strings.Contains(query, "owner_identity = $1") {
		t.Fatalf("authenticated query lacks exact owner scope: %s", query)
	}
	if strings.Contains(query, "owner_identity is null") || strings.Contains(query, "owner_identity = ''") || strings.Contains(query, " or ") {
		t.Fatalf("authenticated query exposes ownerless legacy rows: %s", query)
	}
	if len(statement.Vars) != 1 || statement.Vars[0] != "alice" {
		t.Fatalf("authenticated query vars = %#v, want exact owner", statement.Vars)
	}

	statement = verificationRunsForOwnerQuery(db, "legacy-owner").Find(&runs).Statement
	query = strings.ToLower(statement.SQL.String())
	if !strings.Contains(query, "owner_identity is null") || !strings.Contains(query, " or ") {
		t.Fatalf("configured migration owner cannot inspect ownerless legacy rows: %s", query)
	}

	statement = verificationRunsForOwnerQuery(db, "").Find(&runs).Statement
	if strings.Contains(strings.ToLower(statement.SQL.String()), "owner_identity") {
		t.Fatalf("explicit internal/global query was unexpectedly owner-filtered: %s", statement.SQL.String())
	}
}

func TestFindRunForOwnerUsesOwnerAndIDPredicates(t *testing.T) {
	t.Setenv("HAI_LEGACY_DATA_OWNER_IDENTITY", "legacy-owner")
	sqlDB, err := sql.Open("pgx", "postgres://unused")
	if err != nil {
		t.Fatalf("open dry-run connection: %v", err)
	}
	defer sqlDB.Close()
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{
		DryRun:               true,
		DisableAutomaticPing: true,
	})
	if err != nil {
		t.Fatalf("open dry-run gorm: %v", err)
	}

	id := uuid.New()
	var run models.VerificationRun
	statement := verificationRunsForOwnerQuery(db, "alice").Where("id = ?", id).First(&run).Statement
	query := strings.ToLower(statement.SQL.String())
	if !strings.Contains(query, "owner_identity = $1") || !strings.Contains(query, "id = $2") || !strings.Contains(query, "limit $3") {
		t.Fatalf("single-run query is not bounded and owner-scoped: %s", query)
	}
	if len(statement.Vars) != 3 || statement.Vars[0] != "alice" || statement.Vars[1] != id || statement.Vars[2] != 1 {
		t.Fatalf("single-run query vars = %#v", statement.Vars)
	}
}

func TestVerificationRunHistoryIsBounded(t *testing.T) {
	sqlDB, err := sql.Open("pgx", "postgres://unused")
	if err != nil {
		t.Fatalf("open dry-run connection: %v", err)
	}
	defer sqlDB.Close()
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{
		DryRun:               true,
		DisableAutomaticPing: true,
	})
	if err != nil {
		t.Fatalf("open dry-run gorm: %v", err)
	}

	var runs []models.VerificationRun
	statement := verificationRunsForOwnerQuery(db, "alice").
		Limit(verificationRunHistoryLimit).
		Find(&runs).Statement
	query := strings.ToLower(statement.SQL.String())
	if !strings.Contains(query, "limit $2") {
		t.Fatalf("verification run history query is unbounded: %s", query)
	}
	if len(statement.Vars) != 2 || statement.Vars[1] != verificationRunHistoryLimit {
		t.Fatalf("verification history query vars = %#v", statement.Vars)
	}
}
