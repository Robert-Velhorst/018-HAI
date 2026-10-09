//go:build integration

package source

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/safety"
	"automation-hub-backend/internal/workflow"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestExtractionCorrectionPostgresIntentAndRevisionTransactions(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("HAI_TEST_DATABASE_DSN"))
	if dsn == "" {
		t.Skip("HAI_TEST_DATABASE_DSN not set; skipping PostgreSQL source correction integration test")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open Postgres test database: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get Postgres connection: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	for _, model := range []any{
		&models.ConnectedSource{}, &models.SourceExtraction{}, &models.SourceIndexEntry{}, &models.ContextMemory{},
		&models.DurableJob{}, &models.SourceExtractionCorrection{}, &models.WorkflowItem{},
		&models.WorkflowTransition{}, &models.WorkflowDecision{}, &models.WorkflowEvent{},
	} {
		if !db.Migrator().HasTable(model) {
			t.Fatalf("Postgres test database is missing migrated table for %T", model)
		}
	}

	t.Run("atomic intent and stale revision conflict", func(t *testing.T) {
		repository, sourceRecord, extraction, owner, cleanup := newPostgresExtractionCorrectionFixture(t, db)
		t.Cleanup(cleanup)
		value := "operator correction"
		patch := ExtractionPatch{Summary: &value}
		service := NewService(repository, nil).(*service)
		first, err := service.SubmitExtractionCorrection(owner, extraction.ID, extraction.UpdatedAt, patch, "postgres-correction-key-0001")
		if err != nil {
			t.Fatalf("submit correction: %v", err)
		}
		if !first.IntentPersisted || first.PatchSaved || !first.RecoveryPending || first.ID == "" {
			t.Fatalf("initial correction state = %#v", first)
		}
		var correctionCount, jobCount int64
		if err := db.Model(&models.SourceExtractionCorrection{}).Where("owner_identity = ? AND extraction_id = ?", owner, extraction.ID).Count(&correctionCount).Error; err != nil {
			t.Fatalf("count correction intents: %v", err)
		}
		if err := db.Model(&models.DurableJob{}).Where("id = ?", firstCorrectionJobID(t, db, first.ID)).Count(&jobCount).Error; err != nil {
			t.Fatalf("count correction jobs: %v", err)
		}
		if correctionCount != 1 || jobCount != 1 {
			t.Fatalf("intent/job counts = %d/%d, want exactly one paired outbox item", correctionCount, jobCount)
		}
		retry, err := service.SubmitExtractionCorrection(owner, extraction.ID, extraction.UpdatedAt, patch, "postgres-correction-key-0001")
		if err != nil || retry.ID != first.ID {
			t.Fatalf("idempotent submit id=%q err=%v, want existing intent %q", retry.ID, err, first.ID)
		}
		if err := db.Model(&models.SourceExtraction{}).Where("id = ?", extraction.ID).Update("summary", "newer external value").Error; err != nil {
			t.Fatalf("simulate concurrent extraction update: %v", err)
		}
		newRevision := extraction.UpdatedAt.Add(time.Second)
		if err := db.Model(&models.SourceExtraction{}).Where("id = ?", extraction.ID).Update("updated_at", newRevision).Error; err != nil {
			t.Fatalf("set newer extraction revision: %v", err)
		}
		correctionID, _ := uuid.Parse(first.ID)
		jobID := firstCorrectionJobID(t, db, first.ID)
		leaseCorrectionJob(t, db, jobID, "correction-integration-worker", 1)
		_, _, conflicted, err := repository.ApplyExtractionCorrection(correctionID, jobID, "correction-integration-worker", 1)
		if err != nil || !conflicted {
			t.Fatalf("stale correction apply conflicted=%t err=%v, want persisted conflict", conflicted, err)
		}
		var saved models.SourceExtractionCorrection
		if err := db.First(&saved, "id = ?", correctionID).Error; err != nil {
			t.Fatalf("load stale correction outcome: %v", err)
		}
		var after models.SourceExtraction
		if err := db.First(&after, "id = ?", extraction.ID).Error; err != nil {
			t.Fatalf("load extraction after stale apply: %v", err)
		}
		if saved.Status != models.SourceExtractionCorrectionConflict || saved.Phase != correctionPhaseConflict || saved.AppliedRevision != nil {
			t.Fatalf("stale correction record = %#v", saved)
		}
		if after.Summary != "newer external value" || !after.UpdatedAt.Equal(newRevision) {
			t.Fatalf("stale correction overwrote concurrent source state: %#v", after)
		}
		_ = sourceRecord
	})

	t.Run("patch and revision checkpoint are atomic and lease fenced", func(t *testing.T) {
		repository, _, extraction, owner, cleanup := newPostgresExtractionCorrectionFixture(t, db)
		t.Cleanup(cleanup)
		value := "saved through durable correction"
		service := NewService(repository, nil).(*service)
		view, err := service.SubmitExtractionCorrection(owner, extraction.ID, extraction.UpdatedAt, ExtractionPatch{Summary: &value}, "postgres-correction-key-0002")
		if err != nil {
			t.Fatalf("submit correction: %v", err)
		}
		correctionID, _ := uuid.Parse(view.ID)
		jobID := firstCorrectionJobID(t, db, view.ID)
		if _, _, _, err := repository.ApplyExtractionCorrection(correctionID, jobID, "wrong-worker", 1); !errors.Is(err, errExtractionCorrectionLeaseLost) {
			t.Fatalf("unowned apply error = %v, want lease lost", err)
		}
		var unchanged models.SourceExtraction
		if err := db.First(&unchanged, "id = ?", extraction.ID).Error; err != nil {
			t.Fatalf("read after rejected lease: %v", err)
		}
		if unchanged.Summary != "before" {
			t.Fatalf("unowned worker changed source: %#v", unchanged)
		}
		leaseCorrectionJob(t, db, jobID, "correction-integration-worker", 1)
		correction, updated, conflicted, err := repository.ApplyExtractionCorrection(correctionID, jobID, "correction-integration-worker", 1)
		if err != nil || conflicted || correction == nil || updated == nil {
			t.Fatalf("valid apply correction=%#v extraction=%#v conflicted=%t err=%v", correction, updated, conflicted, err)
		}
		if updated.Summary != value || correction.Phase != correctionPhasePatchApplied || correction.AppliedRevision == nil || !correction.AppliedRevision.Equal(updated.UpdatedAt) {
			t.Fatalf("patch/checkpoint state is inconsistent: correction=%#v extraction=%#v", correction, updated)
		}
		var persistedCorrection models.SourceExtractionCorrection
		var persistedExtraction models.SourceExtraction
		if err := db.First(&persistedCorrection, "id = ?", correctionID).Error; err != nil {
			t.Fatalf("reload correction checkpoint: %v", err)
		}
		if err := db.First(&persistedExtraction, "id = ?", extraction.ID).Error; err != nil {
			t.Fatalf("reload applied extraction: %v", err)
		}
		if persistedExtraction.Summary != value || persistedCorrection.Phase != correctionPhasePatchApplied || persistedCorrection.AppliedRevision == nil || !persistedCorrection.AppliedRevision.Equal(persistedExtraction.UpdatedAt) {
			t.Fatalf("persisted atomic patch/checkpoint mismatch: correction=%#v extraction=%#v", persistedCorrection, persistedExtraction)
		}
	})

	t.Run("SaveExtraction returns persisted monotone revisions", func(t *testing.T) {
		repository, _, extraction, _, cleanup := newPostgresExtractionCorrectionFixture(t, db)
		t.Cleanup(cleanup)
		baseRevision := time.Now().UTC().Add(24 * time.Hour).Truncate(time.Microsecond)
		if err := db.Model(&models.SourceExtraction{}).Where("id = ?", extraction.ID).Update("updated_at", baseRevision).Error; err != nil {
			t.Fatalf("set deterministic future base revision: %v", err)
		}
		if err := db.First(extraction, "id = ?", extraction.ID).Error; err != nil {
			t.Fatalf("reload base extraction: %v", err)
		}
		repository.DB = db.Session(&gorm.Session{NowFunc: func() time.Time { return baseRevision.Add(-time.Hour) }})
		previous := extraction.UpdatedAt
		for i := 0; i < 8; i++ {
			candidate := *extraction
			candidate.Summary = fmt.Sprintf("rapid persisted write %d", i)
			saved, err := repository.SaveExtraction(&candidate)
			if err != nil {
				t.Fatalf("rapid write %d: %v", i, err)
			}
			var persisted models.SourceExtraction
			if err := db.First(&persisted, "id = ?", extraction.ID).Error; err != nil {
				t.Fatalf("reload after rapid write %d: %v", i, err)
			}
			if !saved.UpdatedAt.Equal(persisted.UpdatedAt) || saved.UpdatedAt.Nanosecond()%1000 != 0 {
				t.Fatalf("rapid write %d returned non-persisted precision: returned=%s persisted=%s", i, saved.UpdatedAt.Format(time.RFC3339Nano), persisted.UpdatedAt.Format(time.RFC3339Nano))
			}
			if !saved.UpdatedAt.After(previous) || saved.UpdatedAt.Sub(previous) < time.Microsecond {
				t.Fatalf("rapid write %d revision did not advance by at least one microsecond: previous=%s next=%s", i, previous.Format(time.RFC3339Nano), saved.UpdatedAt.Format(time.RFC3339Nano))
			}
			previous = saved.UpdatedAt
			*extraction = persisted
		}

		const concurrentWrites = 12
		var wait sync.WaitGroup
		revisions := make(chan time.Time, concurrentWrites)
		errs := make(chan error, concurrentWrites)
		for i := 0; i < concurrentWrites; i++ {
			wait.Add(1)
			go func(index int) {
				defer wait.Done()
				candidate := *extraction
				candidate.Summary = fmt.Sprintf("concurrent persisted write %d", index)
				saved, err := repository.SaveExtraction(&candidate)
				if err != nil {
					errs <- err
					return
				}
				revisions <- saved.UpdatedAt
			}(i)
		}
		wait.Wait()
		close(revisions)
		close(errs)
		for err := range errs {
			t.Errorf("concurrent SaveExtraction: %v", err)
		}
		seen := make(map[int64]struct{}, concurrentWrites)
		var latest time.Time
		for revision := range revisions {
			if revision.Nanosecond()%1000 != 0 || !revision.After(previous) {
				t.Errorf("concurrent write returned invalid/non-advancing revision %s after %s", revision.Format(time.RFC3339Nano), previous.Format(time.RFC3339Nano))
			}
			if _, duplicate := seen[revision.UnixMicro()]; duplicate {
				t.Errorf("concurrent writes reused revision %s", revision.Format(time.RFC3339Nano))
			}
			seen[revision.UnixMicro()] = struct{}{}
			if revision.After(latest) {
				latest = revision
			}
		}
		if len(seen) != concurrentWrites {
			t.Fatalf("unique concurrent revisions=%d, want %d", len(seen), concurrentWrites)
		}
		var persisted models.SourceExtraction
		if err := db.First(&persisted, "id = ?", extraction.ID).Error; err != nil {
			t.Fatalf("reload after concurrent writes: %v", err)
		}
		if !persisted.UpdatedAt.Equal(latest) {
			t.Fatalf("latest returned concurrent revision=%s, persisted=%s", latest.Format(time.RFC3339Nano), persisted.UpdatedAt.Format(time.RFC3339Nano))
		}

		createdID := uuid.New()
		t.Cleanup(func() {
			if err := db.Delete(&models.SourceExtraction{}, "id = ?", createdID).Error; err != nil {
				t.Errorf("remove created extraction fixture: %v", err)
			}
		})
		created, err := repository.SaveExtraction(&models.SourceExtraction{
			ID: createdID, SourceID: extraction.SourceID, RawItemID: uuid.New(), Summary: "created through SaveExtraction",
		})
		if err != nil {
			t.Fatalf("create extraction: %v", err)
		}
		if created.ID != createdID {
			t.Fatalf("SaveExtraction returned ID %s, want %s", created.ID, createdID)
		}
		persisted = models.SourceExtraction{}
		if err := db.First(&persisted, "id = ?", createdID).Error; err != nil {
			t.Fatalf("reload created extraction %s: %v", createdID, err)
		}
		if !created.UpdatedAt.Equal(persisted.UpdatedAt) || created.UpdatedAt.Nanosecond()%1000 != 0 {
			t.Fatalf("SaveExtraction create path returned non-canonical revision: returned=%s persisted=%s", created.UpdatedAt.Format(time.RFC3339Nano), persisted.UpdatedAt.Format(time.RFC3339Nano))
		}
	})

	t.Run("deletion removes only exact owner-source lessons", func(t *testing.T) {
		repository, sourceRecord, extraction, owner, cleanup := newPostgresExtractionCorrectionFixture(t, db)
		t.Cleanup(cleanup)
		lessons := seedPostgresSourceLessonFixture(t, db, extraction, owner)

		if err := repository.DeleteExtractionForOwner(extraction, sourceRecord, owner); err != nil {
			t.Fatalf("owner-scoped extraction delete: %v", err)
		}
		assertPostgresSourceLessons(t, db, lessons, false)
	})

	t.Run("deletion removes correction history and outbox payloads for every status", func(t *testing.T) {
		for _, test := range []struct {
			name             string
			correctionStatus string
			jobStatus        string
		}{
			{name: "pending job", correctionStatus: models.SourceExtractionCorrectionPending, jobStatus: models.DurableJobPending},
			{name: "completed job", correctionStatus: models.SourceExtractionCorrectionCompleted, jobStatus: models.DurableJobSucceeded},
			{name: "conflict job", correctionStatus: models.SourceExtractionCorrectionConflict, jobStatus: models.DurableJobDead},
			{name: "failed job", correctionStatus: models.SourceExtractionCorrectionFailed, jobStatus: models.DurableJobDead},
		} {
			t.Run(test.name, func(t *testing.T) {
				repository, sourceRecord, extraction, owner, cleanup := newPostgresExtractionCorrectionFixture(t, db)
				t.Cleanup(cleanup)
				correction, job := createPostgresCorrectionOutboxFixture(t, db, extraction, owner, test.correctionStatus, test.jobStatus)
				callbackCalls := 0
				if err := repository.DeleteExtractionForOwnerGuarded(extraction, sourceRecord, owner, func() error {
					callbackCalls++
					return nil
				}); err != nil {
					t.Fatalf("owner-scoped extraction delete: %v", err)
				}
				if callbackCalls != 1 {
					t.Fatalf("final-effect callback calls=%d, want exactly one", callbackCalls)
				}
				var correctionCount, jobCount int64
				if err := db.Model(&models.SourceExtractionCorrection{}).Where("id = ?", correction.ID).Count(&correctionCount).Error; err != nil {
					t.Fatalf("check correction deletion: %v", err)
				}
				if err := db.Model(&models.DurableJob{}).Where("id = ?", job.ID).Count(&jobCount).Error; err != nil {
					t.Fatalf("check durable payload deletion: %v", err)
				}
				if correctionCount != 0 || jobCount != 0 {
					t.Fatalf("deleted extraction left correction/outbox rows: %d/%d", correctionCount, jobCount)
				}
				var deleted models.SourceExtraction
				if err := db.First(&deleted, "id = ?", extraction.ID).Error; !errors.Is(err, gorm.ErrRecordNotFound) {
					t.Fatalf("extraction still exists or delete failed: err=%v", err)
				}
			})
		}
	})

	t.Run("deletion refuses a running correction worker", func(t *testing.T) {
		repository, sourceRecord, extraction, owner, cleanup := newPostgresExtractionCorrectionFixture(t, db)
		t.Cleanup(cleanup)
		correction, job := createPostgresCorrectionOutboxFixture(t, db, extraction, owner, models.SourceExtractionCorrectionPending, models.DurableJobRunning)
		callbackInvoked := false
		err := repository.DeleteExtractionForOwnerGuarded(extraction, sourceRecord, owner, func() error {
			callbackInvoked = true
			return nil
		})
		if !errors.Is(err, ErrExtractionCorrectionWorkerActive) {
			t.Fatalf("running worker deletion error=%v, want ErrExtractionCorrectionWorkerActive", err)
		}
		if callbackInvoked {
			t.Fatal("pre-commit callback ran while a correction job was active")
		}
		assertPostgresCorrectionOutboxIntact(t, db, extraction.ID, correction, job)
	})

	t.Run("deletion refuses while a worker holds the extraction session lock", func(t *testing.T) {
		repository, sourceRecord, extraction, owner, cleanup := newPostgresExtractionCorrectionFixture(t, db)
		t.Cleanup(cleanup)
		correction, job := createPostgresCorrectionOutboxFixture(t, db, extraction, owner, models.SourceExtractionCorrectionPending, models.DurableJobPending)
		sqlDB, err := db.DB()
		if err != nil {
			t.Fatalf("get Postgres connection pool: %v", err)
		}
		workerConn, err := sqlDB.Conn(context.Background())
		if err != nil {
			t.Fatalf("reserve worker connection: %v", err)
		}
		defer workerConn.Close()
		lockDigest := sha256.Sum256([]byte(owner + "\x00" + extraction.ID.String()))
		lockKey := hex.EncodeToString(lockDigest[:])
		var lockAcquired bool
		if err := workerConn.QueryRowContext(context.Background(),
			"SELECT pg_try_advisory_lock(hashtextextended($1, 0))", lockKey,
		).Scan(&lockAcquired); err != nil {
			t.Fatalf("acquire worker session lock: %v", err)
		}
		if !lockAcquired {
			t.Fatal("worker session lock was not acquired")
		}
		defer func() {
			var unlocked bool
			if err := workerConn.QueryRowContext(context.Background(),
				"SELECT pg_advisory_unlock(hashtextextended($1, 0))", lockKey,
			).Scan(&unlocked); err != nil || !unlocked {
				t.Errorf("release worker session lock: unlocked=%v err=%v", unlocked, err)
			}
		}()

		callbackInvoked := false
		startedAt := time.Now()
		err = repository.DeleteExtractionForOwnerGuarded(extraction, sourceRecord, owner, func() error {
			callbackInvoked = true
			return nil
		})
		if !errors.Is(err, ErrExtractionCorrectionWorkerActive) {
			t.Fatalf("session-locked deletion error=%v, want ErrExtractionCorrectionWorkerActive", err)
		}
		if callbackInvoked {
			t.Fatal("pre-commit callback ran while the correction worker held its session lock")
		}
		if elapsed := time.Since(startedAt); elapsed > time.Second {
			t.Fatalf("session-locked deletion took %s; expected fail-fast behavior", elapsed)
		}
		assertPostgresCorrectionOutboxIntact(t, db, extraction.ID, correction, job)
	})

	t.Run("deletion rolls correction cleanup back when extraction revision is stale", func(t *testing.T) {
		repository, sourceRecord, extraction, owner, cleanup := newPostgresExtractionCorrectionFixture(t, db)
		t.Cleanup(cleanup)
		correction, job := createPostgresCorrectionOutboxFixture(t, db, extraction, owner, models.SourceExtractionCorrectionPending, models.DurableJobPending)
		newerRevision := extraction.UpdatedAt.Add(time.Hour)
		if err := db.Model(&models.SourceExtraction{}).Where("id = ?", extraction.ID).Update("updated_at", newerRevision).Error; err != nil {
			t.Fatalf("advance persisted extraction revision: %v", err)
		}
		callbackInvoked := false
		if err := repository.DeleteExtractionForOwnerGuarded(extraction, sourceRecord, owner, func() error {
			callbackInvoked = true
			return nil
		}); !errors.Is(err, gorm.ErrRecordNotFound) {
			t.Fatalf("stale extraction delete error=%v, want not found", err)
		}
		if callbackInvoked {
			t.Fatal("pre-commit callback ran for a stale extraction revision")
		}
		assertPostgresCorrectionOutboxIntact(t, db, extraction.ID, correction, job)
	})

	t.Run("pre-commit callback failure rolls back all deletion rows", func(t *testing.T) {
		repository, sourceRecord, extraction, owner, cleanup := newPostgresExtractionCorrectionFixture(t, db)
		t.Cleanup(cleanup)
		correction, job := createPostgresCorrectionOutboxFixture(t, db, extraction, owner, models.SourceExtractionCorrectionPending, models.DurableJobPending)
		lessons := seedPostgresSourceLessonFixture(t, db, extraction, owner)
		indexEntry := &models.SourceIndexEntry{
			ID: uuid.New(), SourceID: sourceRecord.ID, ExtractionID: extraction.ID,
			ProjectKey: extraction.ProjectKey, IndexType: "keyword", Keywords: "rollback test",
		}
		if err := db.Create(indexEntry).Error; err != nil {
			t.Fatalf("create source index fixture: %v", err)
		}
		if err := db.First(indexEntry, "id = ?", indexEntry.ID).Error; err != nil {
			t.Fatalf("reload persisted source index fixture: %v", err)
		}
		t.Cleanup(func() {
			if err := db.Delete(&models.SourceIndexEntry{}, "id = ?", indexEntry.ID).Error; err != nil {
				t.Errorf("remove source index fixture: %v", err)
			}
		})

		callbackErr := errors.New("final effect rejected")
		callbackCalls := 0
		err := repository.DeleteExtractionForOwnerGuarded(extraction, sourceRecord, owner, func() error {
			callbackCalls++
			return callbackErr
		})
		if !errors.Is(err, callbackErr) {
			t.Fatalf("guarded delete error=%v, want callback error", err)
		}
		if callbackCalls != 1 {
			t.Fatalf("final-effect callback calls=%d, want exactly one", callbackCalls)
		}
		assertPostgresCorrectionOutboxIntact(t, db, extraction.ID, correction, job)
		assertPostgresSourceLessons(t, db, lessons, true)
		var persistedExtraction models.SourceExtraction
		if err := db.First(&persistedExtraction, "id = ?", extraction.ID).Error; err != nil {
			t.Fatalf("callback error did not roll extraction deletion back: %v", err)
		}
		var persistedIndex models.SourceIndexEntry
		if err := db.First(&persistedIndex, "id = ?", indexEntry.ID).Error; err != nil {
			t.Fatalf("callback error did not roll index deletion back: %v", err)
		}
	})

	t.Run("authorized service leaves state unchanged when a worker holds the session lock", func(t *testing.T) {
		repository, sourceRecord, extraction, owner, cleanup := newPostgresExtractionCorrectionFixture(t, db)
		t.Cleanup(cleanup)
		correction, job := createPostgresCorrectionOutboxFixture(t, db, extraction, owner, models.SourceExtractionCorrectionPending, models.DurableJobPending)
		lessons := seedPostgresSourceLessonFixture(t, db, extraction, owner)
		indexEntry := &models.SourceIndexEntry{
			ID: uuid.New(), SourceID: sourceRecord.ID, ExtractionID: extraction.ID,
			ProjectKey: extraction.ProjectKey, IndexType: "keyword", Keywords: "preserve on blocked delete",
		}
		if err := db.Create(indexEntry).Error; err != nil {
			t.Fatalf("create source index fixture: %v", err)
		}
		if err := db.First(indexEntry, "id = ?", indexEntry.ID).Error; err != nil {
			t.Fatalf("reload persisted source index fixture: %v", err)
		}
		t.Cleanup(func() {
			if err := db.Delete(&models.SourceIndexEntry{}, "id = ?", indexEntry.ID).Error; err != nil {
				t.Errorf("remove source index fixture: %v", err)
			}
		})

		workflowItem := &models.WorkflowItem{
			ID: uuid.New(), OwnerIdentity: owner, Title: "Source-derived follow-up",
			Description:  "Preserve workflow while the correction worker owns the extraction.",
			CurrentState: workflow.StateReady, TaskType: "general", RiskLevel: "medium",
			AutonomyLevel: "assist_only", SourceType: sourceRecord.Category,
			SourceID: extraction.ID.String(),
		}
		if err := db.Create(workflowItem).Error; err != nil {
			t.Fatalf("create source workflow fixture: %v", err)
		}
		if err := db.First(workflowItem, "id = ?", workflowItem.ID).Error; err != nil {
			t.Fatalf("reload persisted source workflow fixture: %v", err)
		}
		t.Cleanup(func() {
			for _, model := range []any{
				&models.WorkflowTransition{}, &models.WorkflowDecision{}, &models.WorkflowEvent{},
			} {
				if err := db.Where("workflow_id = ?", workflowItem.ID).Delete(model).Error; err != nil {
					t.Errorf("remove workflow history fixture for %T: %v", model, err)
				}
			}
			if err := db.Delete(&models.WorkflowItem{}, "id = ?", workflowItem.ID).Error; err != nil {
				t.Errorf("remove workflow fixture: %v", err)
			}
		})

		sqlDB, err := db.DB()
		if err != nil {
			t.Fatalf("get Postgres connection pool: %v", err)
		}
		workerConn, err := sqlDB.Conn(context.Background())
		if err != nil {
			t.Fatalf("reserve worker connection: %v", err)
		}
		defer workerConn.Close()
		lockDigest := sha256.Sum256([]byte(owner + "\x00" + extraction.ID.String()))
		lockKey := hex.EncodeToString(lockDigest[:])
		var lockAcquired bool
		if err := workerConn.QueryRowContext(context.Background(),
			"SELECT pg_try_advisory_lock(hashtextextended($1, 0))", lockKey,
		).Scan(&lockAcquired); err != nil {
			t.Fatalf("acquire worker session lock: %v", err)
		}
		if !lockAcquired {
			t.Fatal("worker session lock was not acquired")
		}
		defer func() {
			var unlocked bool
			if err := workerConn.QueryRowContext(context.Background(),
				"SELECT pg_advisory_unlock(hashtextextended($1, 0))", lockKey,
			).Scan(&unlocked); err != nil || !unlocked {
				t.Errorf("release worker session lock: unlocked=%v err=%v", unlocked, err)
			}
		}()

		authorizer := &recordingSourceAuthorizer{}
		workflowService := workflow.NewService(workflow.NewGormRepository(db))
		service := configuredSourceEffectService(
			NewServiceWithWorkflow(repository, nil, workflowService),
			authorizer,
			clearSourceEmergencyStop,
		)
		err = service.DeleteExtractionAuthorized(context.Background(), extraction.ID, testSourceAuthorization(owner))
		if !errors.Is(err, ErrExtractionCorrectionWorkerActive) {
			t.Fatalf("authorized deletion error=%v, want ErrExtractionCorrectionWorkerActive", err)
		}
		if authorizer.calls != 0 {
			t.Fatalf("authorization consumption calls=%d, want 0 while worker lock is held", authorizer.calls)
		}

		assertPostgresCorrectionOutboxIntact(t, db, extraction.ID, correction, job)
		assertPostgresSourceLessons(t, db, lessons, true)
		var sourceAfter models.ConnectedSource
		if err := db.First(&sourceAfter, "id = ?", sourceRecord.ID).Error; err != nil {
			t.Fatalf("source changed or disappeared after rejected delete: %v", err)
		}
		if sourceAfter.OwnerIdentity != sourceRecord.OwnerIdentity || sourceAfter.ConnectorKey != sourceRecord.ConnectorKey ||
			sourceAfter.Status != sourceRecord.Status || !sourceAfter.UpdatedAt.Equal(sourceRecord.UpdatedAt) {
			t.Fatalf("source changed after rejected delete: before=%#v after=%#v", sourceRecord, sourceAfter)
		}
		var extractionAfter models.SourceExtraction
		if err := db.First(&extractionAfter, "id = ?", extraction.ID).Error; err != nil {
			t.Fatalf("extraction changed or disappeared after rejected delete: %v", err)
		}
		if extractionAfter.SourceID != extraction.SourceID || extractionAfter.ProjectKey != extraction.ProjectKey ||
			extractionAfter.RawItemID != extraction.RawItemID || extractionAfter.ContentHash != extraction.ContentHash ||
			extractionAfter.Summary != extraction.Summary || !extractionAfter.UpdatedAt.Equal(extraction.UpdatedAt) {
			t.Fatalf("extraction changed after rejected delete: before=%#v after=%#v", extraction, extractionAfter)
		}
		var indexAfter models.SourceIndexEntry
		if err := db.First(&indexAfter, "id = ?", indexEntry.ID).Error; err != nil {
			t.Fatalf("index entry changed or disappeared after rejected delete: %v", err)
		}
		if indexAfter.ID != indexEntry.ID || indexAfter.SourceID != indexEntry.SourceID ||
			indexAfter.ExtractionID != indexEntry.ExtractionID || indexAfter.ProjectKey != indexEntry.ProjectKey ||
			indexAfter.IndexType != indexEntry.IndexType || indexAfter.Keywords != indexEntry.Keywords ||
			indexAfter.VectorRef != indexEntry.VectorRef || !indexAfter.CreatedAt.Equal(indexEntry.CreatedAt) ||
			!indexAfter.UpdatedAt.Equal(indexEntry.UpdatedAt) {
			t.Fatalf("index entry changed after rejected delete: before=%#v after=%#v", indexEntry, indexAfter)
		}
		var workflowAfter models.WorkflowItem
		if err := db.First(&workflowAfter, "id = ?", workflowItem.ID).Error; err != nil {
			t.Fatalf("workflow changed or disappeared after rejected delete: %v", err)
		}
		if workflowAfter.CurrentState != workflowItem.CurrentState || workflowAfter.BlockedReason != workflowItem.BlockedReason ||
			workflowAfter.NextAction != workflowItem.NextAction || !workflowAfter.UpdatedAt.Equal(workflowItem.UpdatedAt) {
			t.Fatalf("workflow changed after rejected delete: before=%#v after=%#v", workflowItem, workflowAfter)
		}
		for _, model := range []any{
			&models.WorkflowTransition{}, &models.WorkflowDecision{}, &models.WorkflowEvent{},
		} {
			var count int64
			if err := db.Model(model).Where("workflow_id = ?", workflowItem.ID).Count(&count).Error; err != nil {
				t.Fatalf("count workflow history %T: %v", model, err)
			}
			if count != 0 {
				t.Fatalf("rejected delete wrote %d rows to workflow history %T", count, model)
			}
		}
		var deleteAuditCount int64
		if err := db.Model(&models.SourceAuditLog{}).
			Where("source_id = ? AND action = ?", sourceRecord.ID, "extraction.deleted").
			Count(&deleteAuditCount).Error; err != nil {
			t.Fatalf("count source deletion audit logs: %v", err)
		}
		if deleteAuditCount != 0 {
			t.Fatalf("rejected delete wrote %d deletion audit logs", deleteAuditCount)
		}
	})

	t.Run("deletion fails closed on correction rows owned by another identity", func(t *testing.T) {
		repository, sourceRecord, extraction, owner, cleanup := newPostgresExtractionCorrectionFixture(t, db)
		t.Cleanup(cleanup)
		foreignOwner := "different-owner-" + uuid.NewString()
		correction, job := createPostgresCorrectionOutboxFixture(t, db, extraction, foreignOwner, models.SourceExtractionCorrectionFailed, models.DurableJobDead)
		t.Cleanup(func() {
			if err := db.Where("id = ?", correction.ID).Delete(&models.SourceExtractionCorrection{}).Error; err != nil {
				t.Errorf("remove foreign-owner correction fixture: %v", err)
			}
			if err := db.Where("id = ?", job.ID).Delete(&models.DurableJob{}).Error; err != nil {
				t.Errorf("remove foreign-owner durable job fixture: %v", err)
			}
		})
		if err := repository.DeleteExtractionForOwner(extraction, sourceRecord, owner); err == nil {
			t.Fatal("deletion succeeded despite a correction row with mismatched owner scope")
		}
		assertPostgresCorrectionOutboxIntact(t, db, extraction.ID, correction, job)
	})
}

func TestDeleteExtractionRefusesWhileSourceSyncLeaseIsHeld(t *testing.T) {
	db := openExtractionCorrectionFencePostgres(t)
	repository, sourceRecord, extraction, owner, cleanup := newPostgresExtractionCorrectionFixture(t, db)
	t.Cleanup(cleanup)
	syncRelease, acquired, err := repository.AcquireSourceSyncLease(context.Background(), sourceRecord.ID)
	if err != nil || !acquired || syncRelease == nil {
		t.Fatalf("acquire source-sync lease barrier: acquired=%t release=%t err=%v", acquired, syncRelease != nil, err)
	}
	defer syncRelease()

	authorizer := &recordingSourceAuthorizer{}
	workflowSpy := &extractionCorrectionWorkflowStub{}
	service := configuredSourceEffectService(
		NewServiceWithWorkflow(repository, nil, workflowSpy),
		authorizer,
		clearSourceEmergencyStop,
	)
	started := time.Now()
	err = service.DeleteExtractionAuthorized(context.Background(), extraction.ID, testSourceAuthorization(owner))
	if !errors.Is(err, ErrSyncInProgress) {
		t.Fatalf("authorized deletion while sync holds source lease = %v, want ErrSyncInProgress", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("delete contention took %s, want fail-fast behavior", elapsed)
	}
	if authorizer.calls != 0 || workflowSpy.retractCalls != 0 {
		t.Fatalf("delete callback effects while source lease is busy: authorization=%d workflow retractions=%d, want 0/0",
			authorizer.calls, workflowSpy.retractCalls)
	}
	var persisted models.SourceExtraction
	if err := db.First(&persisted, "id = ?", extraction.ID).Error; err != nil {
		t.Fatalf("extraction changed or disappeared after rejected delete: %v", err)
	}
	var persistedSource models.ConnectedSource
	if err := db.First(&persistedSource, "id = ?", sourceRecord.ID).Error; err != nil {
		t.Fatalf("source changed or disappeared after rejected delete: %v", err)
	}
}

func TestDeleteExtractionAuthorizedAtomicallyRetractsWorkflowAndLessons(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("HAI_TEST_DATABASE_DSN"))
	if dsn == "" {
		t.Skip("HAI_TEST_DATABASE_DSN not set; skipping PostgreSQL source deletion transaction test")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open Postgres test database: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get Postgres connection pool: %v", err)
	}
	sqlDB.SetMaxOpenConns(8)
	t.Cleanup(func() { _ = sqlDB.Close() })
	for _, model := range []any{
		&models.ConnectedSource{}, &models.SourceExtraction{}, &models.SourceIndexEntry{}, &models.ContextMemory{},
		&models.SourceAuditLog{},
		&models.WorkflowItem{}, &models.WorkflowTransition{}, &models.WorkflowDecision{}, &models.WorkflowEvent{},
	} {
		if !db.Migrator().HasTable(model) {
			t.Fatalf("Postgres test database is missing migrated table for %T", model)
		}
	}
	if !db.Migrator().HasColumn(&models.ContextMemory{}, "SourceExtractionID") {
		t.Fatal("Postgres test database is missing context-memory source extraction identity")
	}

	repository, sourceRecord, extraction, owner, cleanup := newPostgresExtractionCorrectionFixture(t, db)
	t.Cleanup(cleanup)
	lessons := seedPostgresSourceLessonFixture(t, db, extraction, owner)
	if err := db.Create(&models.SourceIndexEntry{
		ID: uuid.New(), SourceID: sourceRecord.ID, ExtractionID: extraction.ID,
		IndexType: "keyword", Keywords: "atomic deletion test",
	}).Error; err != nil {
		t.Fatalf("create extraction index fixture: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Where("extraction_id = ?", extraction.ID).Delete(&models.SourceIndexEntry{}).Error; err != nil {
			t.Errorf("remove extraction index fixture: %v", err)
		}
	})
	workflowRepository := workflow.NewGormRepository(db)
	workflowEngine := workflow.NewService(workflowRepository)
	workflowItem, err := workflowRepository.CreateItem(&models.WorkflowItem{
		ID: uuid.New(), OwnerIdentity: owner, Title: "Atomic source deletion integration",
		CurrentState: workflow.StateReady, TaskType: "administrative", RiskLevel: "low", AutonomyLevel: "manual",
		SourceType: workflowSourceType(sourceRecord), SourceID: extraction.ID.String(),
	})
	if err != nil {
		t.Fatalf("create source-derived workflow fixture: %v", err)
	}
	t.Cleanup(func() {
		for _, model := range []any{&models.WorkflowEvent{}, &models.WorkflowDecision{}, &models.WorkflowTransition{}} {
			if err := db.Where("workflow_id = ?", workflowItem.ID).Delete(model).Error; err != nil {
				t.Errorf("remove workflow retraction fixture: %v", err)
			}
		}
		if err := db.Delete(&models.WorkflowItem{}, "id = ?", workflowItem.ID).Error; err != nil {
			t.Errorf("remove workflow item fixture: %v", err)
		}
	})

	stopChecks := 0
	authorizer := &recordingSourceAuthorizer{}
	stopAfterWorkflowMutation := func() safety.EmergencyStopDecision {
		stopChecks++
		return safety.EmergencyStopDecision{
			Active: stopChecks >= 4,
			Reason: "integration test stop",
			Source: "test",
		}
	}
	service := configuredSourceEffectService(
		NewServiceWithWorkflow(repository, nil, workflowEngine),
		authorizer,
		stopAfterWorkflowMutation,
	)
	auth := testSourceAuthorization(owner)
	err = service.DeleteExtractionAuthorized(context.Background(), extraction.ID, auth)
	if !errors.Is(err, ErrSourceEmergencyStopActive) {
		t.Fatalf("late-stop deletion error = %v, want emergency-stop rollback", err)
	}
	if authorizer.calls != 1 || stopChecks != 4 {
		t.Fatalf("late-stop checks/authorization calls=%d/%d, want 4/1", stopChecks, authorizer.calls)
	}
	if _, err := repository.FindExtraction(extraction.ID); err != nil {
		t.Fatalf("late stop removed source extraction: %v", err)
	}
	assertPostgresSourceLessons(t, db, lessons, true)
	var indexCount int64
	if err := db.Model(&models.SourceIndexEntry{}).Where("extraction_id = ?", extraction.ID).Count(&indexCount).Error; err != nil || indexCount != 1 {
		t.Fatalf("index rows after rollback=%d err=%v, want original row", indexCount, err)
	}
	persistedWorkflow, err := workflowRepository.FindItem(workflowItem.ID)
	if err != nil || persistedWorkflow.CurrentState != workflow.StateReady {
		t.Fatalf("workflow after source transaction rollback=%#v err=%v", persistedWorkflow, err)
	}
	transitions, err := workflowRepository.FindTransitions(workflowItem.ID)
	if err != nil || len(transitions) != 0 {
		t.Fatalf("workflow transitions after rollback=%d err=%v", len(transitions), err)
	}
	decisions, err := workflowRepository.FindDecisions(workflowItem.ID)
	if err != nil || len(decisions) != 0 {
		t.Fatalf("workflow decisions after rollback=%d err=%v", len(decisions), err)
	}
	events, err := workflowRepository.FindEvents(workflowItem.ID)
	if err != nil || len(events) != 0 {
		t.Fatalf("workflow events after rollback=%d err=%v", len(events), err)
	}
	var auditCount int64
	if err := db.Model(&models.SourceAuditLog{}).
		Where("source_id = ? AND action = ?", sourceRecord.ID, "extraction.deleted").Count(&auditCount).Error; err != nil || auditCount != 0 {
		t.Fatalf("deletion audit rows after rollback=%d err=%v, want none", auditCount, err)
	}

	committingAuthorizer := &recordingSourceAuthorizer{}
	committingService := configuredSourceEffectService(
		NewServiceWithWorkflow(repository, nil, workflowEngine),
		committingAuthorizer,
		clearSourceEmergencyStop,
	)
	auth.IdempotencyKey = "source-delete-retry-0002"
	if err := committingService.DeleteExtractionAuthorized(context.Background(), extraction.ID, auth); err != nil {
		t.Fatalf("authorized source deletion: %v", err)
	}
	if committingAuthorizer.calls != 1 {
		t.Fatalf("successful deletion authorization calls=%d, want 1", committingAuthorizer.calls)
	}
	if _, err := repository.FindExtraction(extraction.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("committed source extraction lookup error=%v, want not found", err)
	}
	assertPostgresSourceLessons(t, db, lessons, false)
	if err := db.Model(&models.SourceIndexEntry{}).Where("extraction_id = ?", extraction.ID).Count(&indexCount).Error; err != nil || indexCount != 0 {
		t.Fatalf("index rows after commit=%d err=%v, want none", indexCount, err)
	}
	persistedWorkflow, err = workflowRepository.FindItem(workflowItem.ID)
	if err != nil || persistedWorkflow.CurrentState != workflow.StateBlocked || persistedWorkflow.VerificationStatus != "needs_review" {
		t.Fatalf("workflow after committed source deletion=%#v err=%v", persistedWorkflow, err)
	}
	transitions, err = workflowRepository.FindTransitions(workflowItem.ID)
	if err != nil || len(transitions) != 1 || transitions[0].Trigger != "source_retraction" {
		t.Fatalf("committed workflow transitions=%#v err=%v", transitions, err)
	}
	decisions, err = workflowRepository.FindDecisions(workflowItem.ID)
	if err != nil || len(decisions) != 1 || decisions[0].DecisionType != "source_retraction" {
		t.Fatalf("committed workflow decisions=%#v err=%v", decisions, err)
	}
	events, err = workflowRepository.FindEvents(workflowItem.ID)
	if err != nil || len(events) != 1 || events[0].EventType != "workflow.source_retracted" || events[0].Actor != auth.ActorIdentity {
		t.Fatalf("committed workflow events=%#v err=%v", events, err)
	}
	var deletionAudit models.SourceAuditLog
	if err := db.Where("source_id = ? AND action = ?", sourceRecord.ID, "extraction.deleted").First(&deletionAudit).Error; err != nil {
		t.Fatalf("read committed extraction deletion audit: %v", err)
	}
	if !strings.Contains(deletionAudit.Message, auth.ActorIdentity) || !strings.Contains(deletionAudit.Message, extraction.ID.String()) {
		t.Fatalf("deletion audit message=%q, want authenticated actor and extraction identity", deletionAudit.Message)
	}
}

func TestExtractionCorrectionPostgresPrefersCompletedTerminalRow(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("HAI_TEST_DATABASE_DSN"))
	if dsn == "" {
		t.Skip("HAI_TEST_DATABASE_DSN not set; skipping PostgreSQL source correction integration test")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open Postgres test database: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get Postgres connection: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	repository, _, extraction, owner, cleanup := newPostgresExtractionCorrectionFixture(t, db)
	t.Cleanup(cleanup)

	correctedSummary := "applied correction"
	patchJSON, err := json.Marshal(ExtractionPatch{Summary: &correctedSummary})
	if err != nil {
		t.Fatalf("encode patch fixture: %v", err)
	}
	timestamp := time.Now().UTC().Truncate(time.Microsecond)
	appliedRevision := extraction.UpdatedAt.UTC()
	failedCorrectionID, completedCorrectionID := uuid.New(), uuid.New()
	failedJobID, completedJobID := uuid.New(), uuid.New()
	jobs := []models.DurableJob{
		{ID: failedJobID, Queue: "source", Kind: JobKindExtractionCorrection, Payload: "{}", Status: models.DurableJobDead, RunAt: timestamp, CreatedAt: timestamp, UpdatedAt: timestamp},
		{ID: completedJobID, Queue: "source", Kind: JobKindExtractionCorrection, Payload: "{}", Status: models.DurableJobSucceeded, RunAt: timestamp, CreatedAt: timestamp, UpdatedAt: timestamp},
	}
	if err := db.Create(&jobs).Error; err != nil {
		t.Fatalf("create terminal correction jobs: %v", err)
	}
	corrections := []models.SourceExtractionCorrection{
		{
			ID: failedCorrectionID, OwnerIdentity: owner, SourceID: extraction.SourceID, ExtractionID: extraction.ID,
			IdempotencyKeyHash: strings.Repeat("a", 64), RequestHash: strings.Repeat("b", 64),
			ExpectedRevision: appliedRevision, PatchJSON: string(patchJSON), BeforeStateJSON: "{}",
			Phase: correctionPhasePatchApplied, Status: models.SourceExtractionCorrectionFailed, DurableJobID: failedJobID,
			MaxAttempts: 5, AppliedRevision: &appliedRevision, CreatedAt: timestamp, UpdatedAt: timestamp, CompletedAt: &timestamp,
		},
		{
			ID: completedCorrectionID, OwnerIdentity: owner, SourceID: extraction.SourceID, ExtractionID: extraction.ID,
			IdempotencyKeyHash: strings.Repeat("c", 64), RequestHash: strings.Repeat("d", 64),
			ExpectedRevision: appliedRevision, PatchJSON: string(patchJSON), BeforeStateJSON: "{}",
			Phase: correctionPhaseCompleted, Status: models.SourceExtractionCorrectionCompleted, DurableJobID: completedJobID,
			MaxAttempts: 5, AppliedRevision: &appliedRevision, CreatedAt: timestamp, UpdatedAt: timestamp, CompletedAt: &timestamp,
		},
	}
	if err := db.Create(&corrections).Error; err != nil {
		t.Fatalf("create terminal correction rows: %v", err)
	}

	got, gotJob, found, err := repository.FindLatestAppliedExtractionCorrection(owner, extraction.ID, string(patchJSON), appliedRevision)
	if err != nil || !found || got == nil || gotJob == nil || got.ID != completedCorrectionID || gotJob.ID != completedJobID {
		t.Fatalf("terminal correction selection = correction %#v, job %#v, found %t, err %v; want completed row despite equal timestamps",
			got, gotJob, found, err)
	}
}

func newPostgresExtractionCorrectionFixture(t *testing.T, db *gorm.DB) (*GormRepository, *models.ConnectedSource, *models.SourceExtraction, string, func()) {
	t.Helper()
	owner := "source-correction-integration-" + uuid.NewString()
	sourceRecord := &models.ConnectedSource{
		ID: uuid.New(), OwnerIdentity: owner, ConnectorKey: "local-folder", Name: "correction integration source",
		Category: "local_folder", Enabled: true, LocalOnly: true, Status: "active",
	}
	if err := db.Create(sourceRecord).Error; err != nil {
		t.Fatalf("create source fixture: %v", err)
	}
	var persistedSource models.ConnectedSource
	if err := db.First(&persistedSource, "id = ?", sourceRecord.ID).Error; err != nil {
		t.Fatalf("reload persisted source fixture: %v", err)
	}
	sourceRecord = &persistedSource
	extraction := &models.SourceExtraction{
		ID: uuid.New(), SourceID: sourceRecord.ID, RawItemID: uuid.New(), Summary: "before",
		Tasks: "existing task", Sensitive: true, Uncertain: true,
	}
	now := time.Now().UTC()
	rawItem := &models.SourceRawItem{
		ID: extraction.RawItemID, SourceID: sourceRecord.ID, ExternalID: extraction.ID.String(),
		ItemType: "email", Content: "source correction integration evidence", FetchedAt: now,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(rawItem).Error; err != nil {
		t.Fatalf("create raw source evidence fixture: %v", err)
	}
	if err := db.Create(extraction).Error; err != nil {
		t.Fatalf("create extraction fixture: %v", err)
	}
	var persistedExtraction models.SourceExtraction
	if err := db.First(&persistedExtraction, "id = ?", extraction.ID).Error; err != nil {
		t.Fatalf("reload persisted extraction fixture: %v", err)
	}
	extraction = &persistedExtraction
	jobIDs := make([]uuid.UUID, 0, 2)
	cleanup := func() {
		if err := db.Where("owner_identity = ? AND extraction_id = ?", owner, extraction.ID).Delete(&models.SourceExtractionCorrection{}).Error; err != nil {
			t.Errorf("delete correction fixtures: %v", err)
		}
		if len(jobIDs) > 0 {
			if err := db.Where("id IN ?", jobIDs).Delete(&models.DurableJob{}).Error; err != nil {
				t.Errorf("delete correction-job fixtures: %v", err)
			}
		}
		if err := db.Delete(&models.SourceExtraction{}, "id = ?", extraction.ID).Error; err != nil {
			t.Errorf("delete extraction fixture: %v", err)
		}
		if err := db.Delete(&models.SourceRawItem{}, "id = ?", rawItem.ID).Error; err != nil {
			t.Errorf("delete raw source evidence fixture: %v", err)
		}
		if err := db.Delete(&models.ConnectedSource{}, "id = ?", sourceRecord.ID).Error; err != nil {
			t.Errorf("delete source fixture: %v", err)
		}
	}
	return &GormRepository{DB: db}, sourceRecord, extraction, owner, func() {
		var corrections []models.SourceExtractionCorrection
		if err := db.Where("owner_identity = ? AND extraction_id = ?", owner, extraction.ID).Find(&corrections).Error; err == nil {
			for _, correction := range corrections {
				jobIDs = append(jobIDs, correction.DurableJobID)
			}
		}
		cleanup()
	}
}

type postgresSourceLessonRecord struct {
	id                 uuid.UUID
	ownerIdentity      string
	sourceExtractionID uuid.UUID
	sourceURI          string
	kind               string
	deleted            bool
}

func seedPostgresSourceLessonFixture(
	t *testing.T,
	db *gorm.DB,
	extraction *models.SourceExtraction,
	owner string,
) []postgresSourceLessonRecord {
	t.Helper()
	externalURI := "https://trello.com/c/shared-source-card"
	otherExtractionID := uuid.New()
	otherOwner := "unrelated-owner-" + uuid.NewString()
	lessons := []postgresSourceLessonRecord{
		{id: uuid.New(), ownerIdentity: owner, sourceExtractionID: extraction.ID, sourceURI: externalURI, kind: "correction_lesson", deleted: true},
		{id: uuid.New(), ownerIdentity: owner, sourceExtractionID: extraction.ID, sourceURI: "https://example.test/second-source-reference", kind: "review_note", deleted: true},
		{id: uuid.New(), ownerIdentity: owner, sourceExtractionID: otherExtractionID, sourceURI: externalURI, kind: "correction_lesson"},
		{id: uuid.New(), ownerIdentity: otherOwner, sourceExtractionID: extraction.ID, sourceURI: externalURI, kind: "correction_lesson"},
	}
	for _, lesson := range lessons {
		if err := db.Exec(`
			INSERT INTO context_memories
				(id, owner_identity, kind, content, source_uri, source_extraction_id, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, NOW(), NOW())`,
			lesson.id, lesson.ownerIdentity, lesson.kind, "source lesson fixture", lesson.sourceURI, lesson.sourceExtractionID,
		).Error; err != nil {
			t.Fatalf("seed source lesson %s: %v", lesson.id, err)
		}
	}
	ids := make([]uuid.UUID, 0, len(lessons))
	for _, lesson := range lessons {
		ids = append(ids, lesson.id)
	}
	t.Cleanup(func() {
		if err := db.Where("id IN ?", ids).Delete(&models.ContextMemory{}).Error; err != nil {
			t.Errorf("remove source lesson fixtures: %v", err)
		}
	})
	return lessons
}

func assertPostgresSourceLessons(
	t *testing.T,
	db *gorm.DB,
	lessons []postgresSourceLessonRecord,
	allPresent bool,
) {
	t.Helper()
	for _, lesson := range lessons {
		wantCount := int64(1)
		if lesson.deleted && !allPresent {
			wantCount = 0
		}
		var count int64
		if err := db.Table("context_memories").Where(
			"id = ? AND owner_identity = ? AND source_extraction_id = ? AND source_uri = ?",
			lesson.id, lesson.ownerIdentity, lesson.sourceExtractionID, lesson.sourceURI,
		).Count(&count).Error; err != nil {
			t.Fatalf("count source lesson %s: %v", lesson.id, err)
		}
		if count != wantCount {
			t.Errorf("source lesson id=%s owner=%q extraction=%s uri=%q count=%d, want %d",
				lesson.id, lesson.ownerIdentity, lesson.sourceExtractionID, lesson.sourceURI, count, wantCount)
		}
	}
}

func createPostgresCorrectionOutboxFixture(
	t *testing.T,
	db *gorm.DB,
	extraction *models.SourceExtraction,
	owner, correctionStatus, jobStatus string,
) (*models.SourceExtractionCorrection, *models.DurableJob) {
	t.Helper()
	correctionID, jobID := uuid.New(), uuid.New()
	marker := "sensitive-correction-fixture-" + uuid.NewString()
	phase := correctionPhaseIntentPersisted
	switch correctionStatus {
	case models.SourceExtractionCorrectionCompleted:
		phase = correctionPhaseCompleted
	case models.SourceExtractionCorrectionConflict:
		phase = correctionPhaseConflict
	case models.SourceExtractionCorrectionFailed:
		phase = correctionPhaseFailed
	case models.SourceExtractionCorrectionPending:
	default:
		t.Fatalf("unsupported correction fixture status %q", correctionStatus)
	}
	correction := &models.SourceExtractionCorrection{
		ID: correctionID, OwnerIdentity: owner, SourceID: extraction.SourceID, ExtractionID: extraction.ID,
		IdempotencyKeyHash: uuid.NewString(), RequestHash: uuid.NewString(), ExpectedRevision: extraction.UpdatedAt,
		PatchJSON: `{"summary":"` + marker + `"}`, BeforeStateJSON: `{"summary":"before-` + marker + `"}`,
		Phase: phase, Status: correctionStatus, DurableJobID: jobID, Attempts: 1, MaxAttempts: 5,
	}
	job := &models.DurableJob{
		ID: jobID, Queue: "source", Kind: JobKindExtractionCorrection,
		Payload: `{"correctionId":"` + correctionID.String() + `"}`, Status: jobStatus,
		RunAt: time.Now().UTC(), MaxAttempts: 5,
	}
	if err := db.Create(job).Error; err != nil {
		t.Fatalf("create correction durable job fixture: %v", err)
	}
	if err := db.Create(correction).Error; err != nil {
		t.Fatalf("create correction fixture: %v", err)
	}
	return correction, job
}

func assertPostgresCorrectionOutboxIntact(
	t *testing.T,
	db *gorm.DB,
	extractionID uuid.UUID,
	wantCorrection *models.SourceExtractionCorrection,
	wantJob *models.DurableJob,
) {
	t.Helper()
	var correction models.SourceExtractionCorrection
	if err := db.First(&correction, "id = ? AND extraction_id = ?", wantCorrection.ID, extractionID).Error; err != nil {
		t.Fatalf("correction row was not preserved: %v", err)
	}
	if canonicalFixtureJSON(t, correction.PatchJSON) != canonicalFixtureJSON(t, wantCorrection.PatchJSON) ||
		canonicalFixtureJSON(t, correction.BeforeStateJSON) != canonicalFixtureJSON(t, wantCorrection.BeforeStateJSON) {
		t.Fatalf("correction payload changed despite aborted delete: %#v", correction)
	}
	var job models.DurableJob
	if err := db.First(&job, "id = ?", wantJob.ID).Error; err != nil {
		t.Fatalf("durable job was not preserved: %v", err)
	}
	if canonicalFixtureJSON(t, job.Payload) != canonicalFixtureJSON(t, wantJob.Payload) || job.Status != wantJob.Status {
		t.Fatalf("durable job changed despite aborted delete: %#v", job)
	}
}

func canonicalFixtureJSON(t *testing.T, value string) string {
	t.Helper()
	var decoded any
	if err := json.Unmarshal([]byte(value), &decoded); err != nil {
		t.Fatalf("decode JSON fixture %q: %v", value, err)
	}
	encoded, err := json.Marshal(decoded)
	if err != nil {
		t.Fatalf("encode JSON fixture: %v", err)
	}
	return string(bytes.TrimSpace(encoded))
}

func firstCorrectionJobID(t *testing.T, db *gorm.DB, correctionID string) uuid.UUID {
	t.Helper()
	id, err := uuid.Parse(correctionID)
	if err != nil {
		t.Fatalf("parse correction ID: %v", err)
	}
	var correction models.SourceExtractionCorrection
	if err := db.First(&correction, "id = ?", id).Error; err != nil {
		t.Fatalf("load correction intent: %v", err)
	}
	return correction.DurableJobID
}

func leaseCorrectionJob(t *testing.T, db *gorm.DB, id uuid.UUID, worker string, generation int64) {
	t.Helper()
	if err := db.Model(&models.DurableJob{}).Where("id = ?", id).Updates(map[string]any{
		"status": models.DurableJobRunning, "locked_by": worker, "lease_generation": generation,
	}).Error; err != nil {
		t.Fatalf("lease correction job fixture: %v", err)
	}
}
