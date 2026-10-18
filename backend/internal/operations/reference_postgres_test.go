//go:build integration

package operations

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sync"
	"testing"

	"automation-hub-backend/internal/infra"
	"automation-hub-backend/migrations"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Requires an explicitly disposable test database, never an installed HAI DB.
func TestAutoposterReferencePostgres(t *testing.T) {
	if os.Getenv("AUTOPOSTER_TEST_DISPOSABLE") != "1" || os.Getenv("HAI_TEST_DATABASE_DSN") == "" {
		t.Skip("disposable PostgreSQL test database not enabled")
	}
	db, err := gorm.Open(postgres.Open(os.Getenv("HAI_TEST_DATABASE_DSN")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal("could not open disposable PostgreSQL")
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	if _, err := infra.ApplyMigrations(db, migrations.Files, "pre"); err != nil {
		t.Fatal(err)
	}
	svc := NewService(NewGormRepository(db))
	exerciseReferenceCheckpoints(t, func() *Service { return NewService(NewGormRepository(db)) })
	exerciseReferenceControls(t, func() *Service { return NewService(NewGormRepository(db)) })
	initial := exerciseReferenceStore(t, svc)
	// Independent service/repository instances use the DB lock, not a Go mutex.
	var wg sync.WaitGroup
	for sequence := uint64(4); sequence <= 30; sequence++ {
		wg.Add(1)
		go func(seq uint64) {
			defer wg.Done()
			in := initial
			in.SourceSequence = seq
			if _, err := NewService(NewGormRepository(db)).SyncReference(context.Background(), in); err != nil {
				t.Error(err)
			}
		}(sequence)
	}
	wg.Wait()
	rows, err := svc.List(Filter{OwnerUserID: initial.OwnerUserID, WorkspaceID: "local"})
	if err != nil || len(rows) != 1 {
		t.Fatalf("reference read failed: %v", err)
	}
	var state map[string]string
	if err := json.Unmarshal([]byte(rows[0].WorldModelStateJSON), &state); err != nil || state["source_change_id"] != "30" {
		t.Fatal("latest sequence was not retained")
	}
	t.Run("checkpoint_database_failure_and_migration_reversal", func(t *testing.T) {
		// Everything below, including DDL and migration records, is rolled back.
		// This test database is explicitly disposable; no installed DB is used.
		tx := db.Begin()
		if tx.Error != nil {
			t.Fatal(tx.Error)
		}
		defer tx.Rollback()
		svc := NewService(NewGormRepository(tx))
		key := ReferenceCheckpointKey{OwnerUserID: "migration-" + uuid.NewString(), WorkspaceID: "local", FeedID: uuid.New()}
		if _, err := svc.EnsureReferenceSource(context.Background(), key, true); err != nil {
			t.Fatal(err)
		}
		in := referenceInput(1)
		in.OwnerUserID, in.AccountFeedID = key.OwnerUserID, &key.FeedID
		in.DedupeKey = key.FeedID.String() + ":listing:1"
		if err := tx.Exec("ALTER TABLE public.account_feed_reference_checkpoints ADD CONSTRAINT test_reject_cursor CHECK (cursor <> 'fail_cursor')").Error; err != nil {
			t.Fatal(err)
		}
		if results, err := svc.SyncReferencePage(context.Background(), key, 1, ReferenceCheckpoint{}, "fail_cursor", []NewOperationInput{in}); err == nil || len(results) != 0 {
			t.Fatal("checkpoint DB constraint failure accepted")
		}
		cp, err := svc.LoadReferenceCheckpoint(context.Background(), key)
		rows, rowErr := svc.List(Filter{OwnerUserID: key.OwnerUserID, WorkspaceID: "local"})
		if err != nil || rowErr != nil || cp != (ReferenceCheckpoint{}) || len(rows) != 0 {
			t.Fatal("checkpoint failure did not roll back the preceding reference write")
		}
		if _, err := svc.SyncReferencePage(context.Background(), key, 1, ReferenceCheckpoint{}, "MQ", []NewOperationInput{in}); err != nil {
			t.Fatal(err)
		}
		if err := infra.RollbackMigration(tx, migrations.Files, "pre", "pre/0132_account_feed_reference_controls"); err != nil {
			t.Fatal(err)
		}
		if err := infra.RollbackMigration(tx, migrations.Files, "pre", "pre/0131_account_feed_reference_checkpoints"); err != nil {
			t.Fatal(err)
		}
		// Use a savepoint: a missing table aborts its PostgreSQL transaction.
		missingTable := errors.New("expected missing checkpoint table")
		if err := tx.Transaction(func(probe *gorm.DB) error {
			if _, err := NewService(NewGormRepository(probe)).LoadReferenceCheckpoint(context.Background(), key); err == nil {
				t.Error("missing checkpoint table silently fell back to in-memory progress")
			}
			return missingTable
		}); !errors.Is(err, missingTable) {
			t.Fatal(err)
		}
		rows, rowErr = svc.List(Filter{OwnerUserID: key.OwnerUserID, WorkspaceID: "local"})
		if rowErr != nil || len(rows) != 1 {
			t.Fatal("checkpoint rollback removed source references")
		}
		if count, err := infra.ApplyMigrations(tx, migrations.Files, "pre"); err != nil || count != 2 {
			t.Fatalf("reapply checkpoint migration: count=%d, error=%v", count, err)
		}
		if _, err := svc.EnsureReferenceSource(context.Background(), key, true); err != nil {
			t.Fatal(err)
		}
		if results, err := svc.SyncReferencePage(context.Background(), key, 1, ReferenceCheckpoint{}, "MQ", []NewOperationInput{in}); err != nil || len(results) != 1 || results[0].Created {
			t.Fatal("migration reapply/replay duplicated a retained reference")
		}
	})
}
