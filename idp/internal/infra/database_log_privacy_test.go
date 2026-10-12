package infra

import (
	"bytes"
	"context"
	"database/sql"
	"log"
	"strings"
	"testing"

	"gorm.io/gorm/logger"
)

func TestIDPDatabaseDoesNotLogSensitiveSQLValues(t *testing.T) {
	var output bytes.Buffer
	previous := logger.Default
	logger.Default = logger.New(log.New(&output, "", 0), logger.Config{LogLevel: logger.Error})
	t.Cleanup(func() { logger.Default = previous })
	pool := sql.OpenDB(&startupTestConnector{})
	t.Cleanup(func() { _ = pool.Close() })
	db, err := initializeIDPPostgresPool(context.Background(), pool)
	if err != nil {
		t.Fatal(err)
	}
	secret := "synthetic-password-reset-token"
	// The synthetic driver rejects SQL; GORM's default error trace logs the query.
	result := db.Exec("SELECT ?", secret)
	if result.Error == nil {
		t.Fatal("fixture did not produce a SQL failure")
	}
	if strings.Contains(output.String(), secret) {
		t.Fatal("database error trace exposed a sensitive SQL parameter")
	}
	if output.Len() != 0 {
		t.Fatalf("unexpected raw database trace: %q", output.String())
	}
}
