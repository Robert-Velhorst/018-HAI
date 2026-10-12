package source

import (
	"crypto/sha256"
	"encoding/hex"
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
	"gorm.io/gorm/logger"
)

func TestManualSyncPostgresConcurrentIdempotencyAndRetryActiveUniqueness(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("HAI_TEST_DATABASE_DSN"))
	if dsn == "" {
		t.Skip("HAI_TEST_DATABASE_DSN not set; skipping Postgres manual-sync integration test")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open Postgres test database: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get Postgres test connection: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if !db.Migrator().HasTable(&models.ConnectedSource{}) || !db.Migrator().HasTable(&models.SourceSyncJob{}) || !db.Migrator().HasTable(&models.DurableJob{}) {
		t.Fatal("Postgres test database is missing migrated connected-source or durable-job tables")
	}
	for _, column := range []string{"owner_identity", "idempotency_key_hash", "request_hash", "durable_job_id"} {
		if !db.Migrator().HasColumn(&models.SourceSyncJob{}, column) {
			t.Fatalf("Postgres test database is missing source_sync_jobs.%s; apply manual-sync migration first", column)
		}
	}

	repository := &GormRepository{DB: db}
	owner := "manual-sync-test-" + uuid.NewString()
	source := &models.ConnectedSource{
		ID: uuid.New(), OwnerIdentity: owner, ConnectorKey: "local-folder", Name: "Manual sync integration source",
		Category: "local_folder", Enabled: true, LocalOnly: true, Status: "active", SyncFrequency: "manual", DefaultProjectKey: "project-current",
	}
	if err := db.Create(source).Error; err != nil {
		t.Fatalf("create test source: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Transaction(func(tx *gorm.DB) error {
			var queueIDs []uuid.UUID
			if err := tx.Model(&models.SourceSyncJob{}).Where("source_id = ?", source.ID).Pluck("durable_job_id", &queueIDs).Error; err != nil {
				return err
			}
			if err := tx.Where("source_id = ?", source.ID).Delete(&models.SourceSyncJob{}).Error; err != nil {
				return err
			}
			if len(queueIDs) > 0 {
				if err := tx.Where("id IN ? AND kind = ?", queueIDs, JobKindManualSync).Delete(&models.DurableJob{}).Error; err != nil {
					return err
				}
			}
			return tx.Delete(&models.ConnectedSource{}, "id = ?", source.ID).Error
		})
	})

	service := NewService(repository, nil).(*service)
	service.setManualSyncWorkerReady(true)
	key := "concurrent-manual-sync-" + uuid.NewString()
	const submitters = 12
	start := make(chan struct{})
	type submitResult struct {
		view    *ManualSyncJobView
		created bool
		err     error
	}
	results := make(chan submitResult, submitters)
	var wait sync.WaitGroup
	for index := 0; index < submitters; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			view, created, err := service.SubmitManualSync(owner, source.ID, key, ManualSyncRequest{})
			results <- submitResult{view: view, created: created, err: err}
		}()
	}
	close(start)
	wait.Wait()
	close(results)
	var acceptedID string
	createdCount := 0
	for result := range results {
		if result.err != nil {
			t.Fatalf("concurrent SubmitManualSync: %v", result.err)
		}
		if result.view == nil {
			t.Fatal("concurrent SubmitManualSync returned a nil job")
		}
		if acceptedID == "" {
			acceptedID = result.view.ID
		} else if result.view.ID != acceptedID {
			t.Fatalf("concurrent requests returned different jobs %s and %s", acceptedID, result.view.ID)
		}
		if result.created {
			createdCount++
		}
	}
	if createdCount != 1 {
		t.Fatalf("concurrent requests created %d durable jobs, want exactly one", createdCount)
	}
	var historyCount int64
	if err := db.Model(&models.SourceSyncJob{}).Where("source_id = ? AND owner_identity = ?", source.ID, owner).Count(&historyCount).Error; err != nil {
		t.Fatalf("count created history rows: %v", err)
	}
	if historyCount != 1 {
		t.Fatalf("concurrent history row count = %d, want one", historyCount)
	}

	jobID, err := uuid.Parse(acceptedID)
	if err != nil {
		t.Fatalf("parse accepted job id: %v", err)
	}
	job, durable, err := repository.FindManualSyncJobForOwner(owner, jobID)
	if err != nil {
		t.Fatalf("load accepted owner-bound job: %v", err)
	}
	keyDigest := sha256.Sum256([]byte(key))
	if _, _, found, err := repository.FindManualSyncJobByIdempotencyKey("another-owner", source.ID, hex.EncodeToString(keyDigest[:])); err != nil || found {
		t.Fatalf("foreign idempotency lookup = found:%v err:%v, want no result", found, err)
	}
	if _, _, err := service.SubmitManualSync(owner, source.ID, key, ManualSyncRequest{ProjectKey: "different-payload"}); !errors.Is(err, ErrManualSyncIdempotencyConflict) {
		t.Fatalf("mismatched key payload error = %v, want idempotency conflict", err)
	}

	if err := db.Model(&models.SourceSyncJob{}).Where("id = ?", job.ID).Update("status", "failed").Error; err != nil {
		t.Fatalf("simulate failed attempt with retryable queue state: %v", err)
	}
	if err := db.Model(&models.DurableJob{}).Where("id = ?", durable.ID).Update("status", models.DurableJobPending).Error; err != nil {
		t.Fatalf("set durable retry to pending: %v", err)
	}
	if _, _, err := service.SubmitManualSync(owner, source.ID, "manual-sync-another-active-key", ManualSyncRequest{}); !errors.Is(err, ErrManualSyncAlreadyActive) {
		t.Fatalf("submission while prior durable retry is pending = %v, want active-job conflict", err)
	}
	direct, err := service.ManualSyncJobForOwner(owner, job.ID)
	if err != nil || direct.Status != "queued" {
		t.Fatalf("direct pending-retry projection = %#v err=%v, want queued", direct, err)
	}
	history, err := repository.FindSyncJobs(&source.ID)
	if err != nil || len(history) != 1 || history[0].Status != "queued" {
		t.Fatalf("history pending-retry projection = %#v err=%v, want queued", history, err)
	}

	if err := db.Model(&models.DurableJob{}).Where("id = ?", durable.ID).Update("status", models.DurableJobRunning).Error; err != nil {
		t.Fatalf("set durable retry to running: %v", err)
	}
	direct, err = service.ManualSyncJobForOwner(owner, job.ID)
	if err != nil || direct.Status != "running" {
		t.Fatalf("direct running-retry projection = %#v err=%v, want running", direct, err)
	}
	history, err = repository.FindSyncJobs(&source.ID)
	if err != nil || len(history) != 1 || history[0].Status != "running" {
		t.Fatalf("history running-retry projection = %#v err=%v, want running", history, err)
	}

	if err := db.Model(&models.ConnectedSource{}).Where("id = ?", source.ID).Update("default_project_key", "project-newer").Error; err != nil {
		t.Fatalf("set newer source project default: %v", err)
	}
	staleSourceSnapshot := *source
	staleSourceSnapshot.DefaultProjectKey = "project-captured-by-old-sync"
	staleSourceSnapshot.Cursor = "cursor-after-manual-sync"
	completion := *job
	completion.Status = "completed"
	completion.CursorAfter = staleSourceSnapshot.Cursor
	completedAt := time.Now().UTC()
	completion.CompletedAt = &completedAt
	if _, _, err := repository.CompleteSourceSync(&staleSourceSnapshot, &completion); err != nil {
		t.Fatalf("complete queued sync from stale source snapshot: %v", err)
	}
	var reloadedSource models.ConnectedSource
	if err := db.First(&reloadedSource, "id = ?", source.ID).Error; err != nil {
		t.Fatalf("reload source after completion: %v", err)
	}
	if reloadedSource.DefaultProjectKey != "project-newer" {
		t.Fatalf("source default after stale completion = %q, want project-newer", reloadedSource.DefaultProjectKey)
	}
}
