//go:build integration

package source

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"automation-hub-backend/internal/durablejob"
	"automation-hub-backend/internal/memory"
	"automation-hub-backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestExtractionCorrectionSessionFenceCoordinatesWithDeletion(t *testing.T) {
	db := openExtractionCorrectionFencePostgres(t)
	repository, sourceRecord, extraction, owner, cleanup := newPostgresExtractionCorrectionFixture(t, db)
	t.Cleanup(cleanup)
	correction, durableJob := createPostgresCorrectionOutboxFixture(
		t, db, extraction, owner, models.SourceExtractionCorrectionPending, models.DurableJobPending,
	)
	barrierRepository := &barrierExtractionCorrectionRepository{
		GormRepository: repository,
		acquired:       make(chan struct{}),
		continueWorker: make(chan struct{}),
	}
	workflowSpy := &extractionCorrectionWorkflowStub{}
	service := NewServiceWithWorkflow(barrierRepository, nil, workflowSpy).(*service)
	staleLease := *durableJob
	staleLease.Status = models.DurableJobRunning
	staleLease.LockedBy = "expired-correction-worker"
	staleLease.LeaseGeneration = 4
	lockedAt := time.Now().UTC()
	staleLease.LockedAt = &lockedAt
	workerDone := make(chan error, 1)
	go func() {
		workerDone <- service.runExtractionCorrection(context.Background(), staleLease, correction.ID)
	}()
	var releaseBarrier sync.Once
	continueWorker := func() { releaseBarrier.Do(func() { close(barrierRepository.continueWorker) }) }
	defer continueWorker()
	select {
	case <-barrierRepository.acquired:
	case err := <-workerDone:
		t.Fatalf("worker exited before acquiring session lock: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not reach the session-lock barrier")
	}
	if err := repository.DeleteExtractionForOwner(extraction, sourceRecord, owner); !errors.Is(err, ErrSyncInProgress) {
		t.Fatalf("deletion while stale handler holds source lease: %v, want ErrSyncInProgress", err)
	}
	assertPostgresCorrectionOutboxIntact(t, db, extraction.ID, correction, durableJob)
	continueWorker()
	select {
	case err := <-workerDone:
		if err != nil {
			t.Fatalf("expired handler after acquiring fence: %v, want clean lease-state stop", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not leave its session-lock barrier")
	}
	if workflowSpy.intakeCalls != 0 || workflowSpy.retractCalls != 0 {
		t.Fatalf("expired worker produced workflow effects: intake=%d retract=%d", workflowSpy.intakeCalls, workflowSpy.retractCalls)
	}

	if err := repository.DeleteExtractionForOwner(extraction, sourceRecord, owner); err != nil {
		t.Fatalf("deletion after stale worker released its session lock: %v", err)
	}
	var correctionCount, jobCount, extractionCount int64
	if err := db.Model(&models.SourceExtractionCorrection{}).Where("id = ?", correction.ID).Count(&correctionCount).Error; err != nil {
		t.Fatalf("count deleted correction: %v", err)
	}
	if err := db.Model(&models.DurableJob{}).Where("id = ?", correction.DurableJobID).Count(&jobCount).Error; err != nil {
		t.Fatalf("count deleted durable job: %v", err)
	}
	if err := db.Model(&models.SourceExtraction{}).Where("id = ?", extraction.ID).Count(&extractionCount).Error; err != nil {
		t.Fatalf("count deleted extraction: %v", err)
	}
	if correctionCount != 0 || jobCount != 0 || extractionCount != 0 {
		t.Fatalf("post-delete row counts correction/job/extraction=%d/%d/%d, want 0/0/0", correctionCount, jobCount, extractionCount)
	}
}

func TestExtractionCorrectionWorkerDefersWhileSourceSyncLeaseIsHeld(t *testing.T) {
	db := openExtractionCorrectionFencePostgres(t)
	repository, _, extraction, owner, cleanup := newPostgresExtractionCorrectionFixture(t, db)
	t.Cleanup(cleanup)
	correction, queuedJob := createPostgresCorrectionOutboxFixture(
		t, db, extraction, owner, models.SourceExtractionCorrectionPending, models.DurableJobPending,
	)
	syncRelease, acquired, err := repository.AcquireSourceSyncLease(context.Background(), extraction.SourceID)
	if err != nil || !acquired || syncRelease == nil {
		t.Fatalf("acquire source-sync lease barrier: acquired=%t release=%t err=%v", acquired, syncRelease != nil, err)
	}
	defer syncRelease()

	staleLease := *queuedJob
	staleLease.Status = models.DurableJobRunning
	staleLease.LockedBy = "sync-contention-worker"
	staleLease.LeaseGeneration = 3
	workflowSpy := &extractionCorrectionWorkflowStub{}
	service := NewServiceWithWorkflow(repository, nil, workflowSpy).(*service)
	workerErr := service.runExtractionCorrection(context.Background(), durablejob.Job(staleLease), correction.ID)
	var deferred *durablejob.DeferredError
	if !errors.As(workerErr, &deferred) {
		t.Fatalf("worker under active source sync = %v, want durable-job defer", workerErr)
	}
	if workflowSpy.intakeCalls != 0 || workflowSpy.retractCalls != 0 {
		t.Fatalf("worker produced effects while sync held the source lease: intake=%d retract=%d", workflowSpy.intakeCalls, workflowSpy.retractCalls)
	}
	assertPostgresCorrectionOutboxIntact(t, db, extraction.ID, correction, queuedJob)
}

func TestDeleteAndArchiveRefuseWhileSourceSyncLeaseIsHeld(t *testing.T) {
	db := openExtractionCorrectionFencePostgres(t)
	repository, sourceRecord, extraction, owner, cleanup := newPostgresExtractionCorrectionFixture(t, db)
	t.Cleanup(cleanup)
	correction, queuedJob := createPostgresCorrectionOutboxFixture(
		t, db, extraction, owner, models.SourceExtractionCorrectionPending, models.DurableJobPending,
	)
	syncRelease, acquired, err := repository.AcquireSourceSyncLease(context.Background(), sourceRecord.ID)
	if err != nil || !acquired || syncRelease == nil {
		t.Fatalf("acquire source-sync lease barrier: acquired=%t release=%t err=%v", acquired, syncRelease != nil, err)
	}
	defer syncRelease()

	callbackCalls := 0
	deleteErr := repository.DeleteExtractionForOwnerGuarded(extraction, sourceRecord, owner, func() error {
		callbackCalls++
		return nil
	})
	if !errors.Is(deleteErr, ErrSyncInProgress) || callbackCalls != 0 {
		t.Fatalf("guarded delete while sync active: err=%v callback calls=%d, want ErrSyncInProgress and no callback", deleteErr, callbackCalls)
	}
	workflowSpy := &extractionCorrectionWorkflowStub{}
	service := NewServiceWithWorkflow(repository, nil, workflowSpy).(*service)
	if _, archiveErr := service.ArchiveExtraction(extraction.ID, true); !errors.Is(archiveErr, ErrSyncInProgress) {
		t.Fatalf("archive while sync active = %v, want ErrSyncInProgress", archiveErr)
	}
	if workflowSpy.retractCalls != 0 {
		t.Fatalf("archive retracted workflow before acquiring source lease: calls=%d", workflowSpy.retractCalls)
	}
	var persisted models.SourceExtraction
	if err := db.First(&persisted, "id = ?", extraction.ID).Error; err != nil {
		t.Fatalf("source extraction disappeared after rejected mutations: %v", err)
	}
	if persisted.Archived {
		t.Fatal("source extraction was archived while sync held the lease")
	}
	assertPostgresCorrectionOutboxIntact(t, db, extraction.ID, correction, queuedJob)
}

func TestExtractionWorkerReleasesSourceLeaseWhenExtractionFenceIsBusy(t *testing.T) {
	db := openExtractionCorrectionFencePostgres(t)
	repository, sourceRecord, extraction, owner, cleanup := newPostgresExtractionCorrectionFixture(t, db)
	t.Cleanup(cleanup)
	correction, queuedJob := createPostgresCorrectionOutboxFixture(
		t, db, extraction, owner, models.SourceExtractionCorrectionPending, models.DurableJobPending,
	)
	extractionRelease, acquired, err := repository.AcquireExtractionCorrectionSessionLock(context.Background(), owner, extraction.ID)
	if err != nil || !acquired || extractionRelease == nil {
		t.Fatalf("acquire extraction-fence barrier: acquired=%t release=%t err=%v", acquired, extractionRelease != nil, err)
	}
	defer extractionRelease()
	staleLease := *queuedJob
	staleLease.Status = models.DurableJobRunning
	staleLease.LockedBy = "extraction-contention-worker"
	staleLease.LeaseGeneration = 5
	service := NewServiceWithWorkflow(repository, nil, &extractionCorrectionWorkflowStub{}).(*service)
	workerErr := service.runExtractionCorrection(context.Background(), durablejob.Job(staleLease), correction.ID)
	var deferred *durablejob.DeferredError
	if !errors.As(workerErr, &deferred) {
		t.Fatalf("worker under extraction fence = %v, want durable-job defer", workerErr)
	}
	sourceRelease, sourceAcquired, sourceErr := repository.AcquireSourceSyncLease(context.Background(), sourceRecord.ID)
	if sourceErr != nil || !sourceAcquired || sourceRelease == nil {
		t.Fatalf("source lease remained held after extraction-fence contention: acquired=%t release=%t err=%v", sourceAcquired, sourceRelease != nil, sourceErr)
	}
	sourceRelease()
}

func TestCorrectionWorkerPoolCapacityRequiresThreeConnections(t *testing.T) {
	db := openExtractionCorrectionFencePostgres(t)
	baseRepository := &GormRepository{DB: db}
	repository := &workerLockCountRepository{GormRepository: baseRepository}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get Postgres pool: %v", err)
	}
	previousMax := sqlDB.Stats().MaxOpenConnections
	t.Cleanup(func() { sqlDB.SetMaxOpenConns(previousMax) })
	_, _, extraction, owner, cleanup := newPostgresExtractionCorrectionFixture(t, db)
	t.Cleanup(cleanup)
	correction, job := createPostgresCorrectionOutboxFixture(
		t, db, extraction, owner, models.SourceExtractionCorrectionPending, models.DurableJobPending,
	)
	service := NewServiceWithWorkflow(repository, nil, &extractionCorrectionWorkflowStub{}).(*service)
	for _, maxOpen := range []int{1, 2} {
		sqlDB.SetMaxOpenConns(maxOpen)
		repository.sourceLeaseCalls = 0
		repository.extractionFenceCalls = 0
		started := time.Now()
		workerErr := service.runExtractionCorrection(context.Background(), durablejob.Job(*job), correction.ID)
		var classified *extractionCorrectionWorkerError
		if !errors.As(workerErr, &classified) || classified.retryable {
			t.Fatalf("worker with pool max-open=%d error = %v, want prompt permanent capacity error", maxOpen, workerErr)
		}
		if elapsed := time.Since(started); elapsed > time.Second {
			t.Fatalf("pool max-open=%d failed after %s, want prompt failure", maxOpen, elapsed)
		}
		if repository.sourceLeaseCalls != 0 || repository.extractionFenceCalls != 0 {
			t.Fatalf("pool max-open=%d acquired locks before capacity check: source=%d extraction=%d",
				maxOpen, repository.sourceLeaseCalls, repository.extractionFenceCalls)
		}
	}
	sqlDB.SetMaxOpenConns(3)
	repository.sourceLeaseCalls = 0
	repository.extractionFenceCalls = 0
	if err := service.runExtractionCorrection(context.Background(), durablejob.Job(*job), correction.ID); err != nil {
		t.Fatalf("pool max-open=3 should acquire both locks and reread durable state: %v", err)
	}
	if repository.sourceLeaseCalls != 1 || repository.extractionFenceCalls != 1 {
		t.Fatalf("pool max-open=3 lock acquisitions = source:%d extraction:%d, want one each",
			repository.sourceLeaseCalls, repository.extractionFenceCalls)
	}
}

func TestExtractionCorrectionWorkerStopsWhenDeletionCommittedBeforeRun(t *testing.T) {
	db := openExtractionCorrectionFencePostgres(t)
	repository, sourceRecord, extraction, owner, cleanup := newPostgresExtractionCorrectionFixture(t, db)
	t.Cleanup(cleanup)
	correction, job := createPostgresCorrectionOutboxFixture(
		t, db, extraction, owner, models.SourceExtractionCorrectionPending, models.DurableJobPending,
	)
	staleJob := *job
	staleJob.Status = models.DurableJobRunning
	staleJob.LockedBy = "stale-correction-worker"
	staleJob.LeaseGeneration = 7
	lockedAt := time.Now().UTC()
	staleJob.LockedAt = &lockedAt

	if err := repository.DeleteExtractionForOwner(extraction, sourceRecord, owner); err != nil {
		t.Fatalf("delete extraction before stale worker starts: %v", err)
	}
	workflowSpy := &extractionCorrectionWorkflowStub{}
	service := NewServiceWithWorkflow(repository, nil, workflowSpy).(*service)
	if err := service.runExtractionCorrection(context.Background(), durablejob.Job(staleJob), correction.ID); err != nil {
		t.Fatalf("stale worker after committed deletion: %v, want clean stop", err)
	}
	if workflowSpy.intakeCalls != 0 || workflowSpy.retractCalls != 0 {
		t.Fatalf("stale worker ran workflow effects after deletion: intake=%d retract=%d", workflowSpy.intakeCalls, workflowSpy.retractCalls)
	}
}

func TestExtractionCorrectionSessionFenceCoversMemoryPersistence(t *testing.T) {
	db := openExtractionCorrectionFencePostgres(t)
	repository, sourceRecord, extraction, owner, cleanup := newPostgresExtractionCorrectionFixture(t, db)
	t.Cleanup(cleanup)
	t.Cleanup(func() {
		if err := db.Where("source_extraction_id = ?", extraction.ID).Delete(&models.ContextMemory{}).Error; err != nil {
			t.Errorf("delete correction lesson fixture: %v", err)
		}
	})
	safeEvidenceURI := "https://evidence.example/messages/42"
	if err := db.Model(&models.SourceExtraction{}).Where("id = ?", extraction.ID).Updates(map[string]any{
		"source_uri": safeEvidenceURI, "source_label": "Evidence message 42", "sensitive": false, "uncertain": false,
	}).Error; err != nil {
		t.Fatalf("prepare safe evidence URI: %v", err)
	}
	if err := db.First(extraction, "id = ?", extraction.ID).Error; err != nil {
		t.Fatalf("reload evidence extraction: %v", err)
	}
	correction, durableJob := createPostgresCorrectionOutboxFixture(
		t, db, extraction, owner, models.SourceExtractionCorrectionPending, models.DurableJobPending,
	)

	beforeState, err := json.Marshal(extractionCorrectionBefore(extraction))
	if err != nil {
		t.Fatalf("marshal correction before-state: %v", err)
	}
	appliedRevision := extraction.UpdatedAt.Add(time.Second)
	if err := db.Model(&models.SourceExtraction{}).Where("id = ?", extraction.ID).Updates(map[string]any{
		"summary":    "corrected summary",
		"updated_at": appliedRevision,
	}).Error; err != nil {
		t.Fatalf("persist corrected extraction fixture: %v", err)
	}
	var correctedExtraction models.SourceExtraction
	if err := db.First(&correctedExtraction, "id = ?", extraction.ID).Error; err != nil {
		t.Fatalf("reload corrected extraction fixture: %v", err)
	}
	if err := db.Model(&models.SourceExtractionCorrection{}).Where("id = ?", correction.ID).Updates(map[string]any{
		"phase":             correctionPhaseGraphProjected,
		"before_state_json": string(beforeState),
		"applied_revision":  appliedRevision,
	}).Error; err != nil {
		t.Fatalf("prepare correction memory phase: %v", err)
	}
	correction.BeforeStateJSON = string(beforeState)
	lockedAt := time.Now().UTC()
	workerID := "memory-stage-worker-" + uuid.NewString()
	const leaseGeneration = int64(11)
	if err := db.Model(&models.DurableJob{}).Where("id = ?", durableJob.ID).Updates(map[string]any{
		"status":           models.DurableJobRunning,
		"locked_by":        workerID,
		"locked_at":        lockedAt,
		"lease_generation": leaseGeneration,
	}).Error; err != nil {
		t.Fatalf("lease correction job fixture: %v", err)
	}
	durableJob.Status = models.DurableJobRunning
	durableJob.LockedBy = workerID
	durableJob.LockedAt = &lockedAt
	durableJob.LeaseGeneration = leaseGeneration

	memoryBarrier := &blockingExtractionCorrectionLessonRepository{
		GormRepository: repository,
		reached:        make(chan struct{}),
		resume:         make(chan struct{}),
	}
	service := NewServiceWithWorkflow(memoryBarrier, nil, &extractionCorrectionWorkflowStub{}).(*service)
	workerDone := make(chan error, 1)
	go func() {
		workerDone <- service.runExtractionCorrection(context.Background(), durablejob.Job(*durableJob), correction.ID)
	}()
	var resumeOnce sync.Once
	resumeMemory := func() { resumeOnce.Do(func() { close(memoryBarrier.resume) }) }
	defer resumeMemory()
	select {
	case <-memoryBarrier.reached:
	case err := <-workerDone:
		t.Fatalf("worker exited before memory persistence barrier: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not reach the memory persistence barrier")
	}

	if err := repository.DeleteExtractionForOwner(&correctedExtraction, sourceRecord, owner); !errors.Is(err, ErrSyncInProgress) {
		t.Fatalf("deletion during memory persistence: %v, want ErrSyncInProgress while the correction worker holds the source lease", err)
	}
	assertPostgresCorrectionOutboxIntact(t, db, extraction.ID, correction, durableJob)
	resumeMemory()
	select {
	case err := <-workerDone:
		if err != nil {
			t.Fatalf("finish correction after memory persistence: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not finish after memory persistence resumed")
	}
	if err := db.Model(&models.DurableJob{}).Where("id = ?", durableJob.ID).Updates(map[string]any{
		"status":       models.DurableJobSucceeded,
		"locked_by":    "",
		"locked_at":    nil,
		"completed_at": time.Now().UTC(),
	}).Error; err != nil {
		t.Fatalf("settle completed durable job fixture: %v", err)
	}
	var saved models.ContextMemory
	if err := db.Where("owner_identity = ? AND source_extraction_id = ? AND kind = ?", owner, extraction.ID, "lesson").First(&saved).Error; err != nil {
		t.Fatalf("load persisted correction lesson: %v", err)
	}
	if saved.SourceURI != safeEvidenceURI || saved.SourceExtractionID == nil || *saved.SourceExtractionID != extraction.ID {
		t.Fatalf("stored evidence/extraction provenance = %q/%v, want %q/%s", saved.SourceURI, saved.SourceExtractionID, safeEvidenceURI, extraction.ID)
	}
	if err := repository.DeleteExtractionForOwner(&correctedExtraction, sourceRecord, owner); err != nil {
		t.Fatalf("deletion after memory-stage worker released its session lock: %v", err)
	}
}

func TestExtractionCorrectionLessonPersistenceIsIdempotentAndLeaseRevisionFenced(t *testing.T) {
	db := openExtractionCorrectionFencePostgres(t)
	repository, _, extraction, owner, cleanup := newPostgresExtractionCorrectionFixture(t, db)
	t.Cleanup(cleanup)
	t.Cleanup(func() {
		if err := db.Where("source_extraction_id = ?", extraction.ID).Delete(&models.ContextMemory{}).Error; err != nil {
			t.Errorf("delete correction lesson fixture: %v", err)
		}
	})
	safeEvidenceURI := "https://evidence.example/mail/thread/938"
	if err := db.Model(&models.SourceExtraction{}).Where("id = ?", extraction.ID).Updates(map[string]any{
		"source_uri": safeEvidenceURI, "source_label": "Mail thread 938", "sensitive": false, "uncertain": false,
	}).Error; err != nil {
		t.Fatalf("prepare safe evidence URI: %v", err)
	}
	if err := db.First(extraction, "id = ?", extraction.ID).Error; err != nil {
		t.Fatalf("reload evidence extraction: %v", err)
	}
	correction, job := createPostgresCorrectionOutboxFixture(
		t, db, extraction, owner, models.SourceExtractionCorrectionPending, models.DurableJobPending,
	)
	beforeState, err := json.Marshal(extractionCorrectionBefore(extraction))
	if err != nil {
		t.Fatalf("marshal correction before-state: %v", err)
	}
	appliedRevision := extraction.UpdatedAt.Add(time.Second).UTC()
	if err := db.Model(&models.SourceExtraction{}).Where("id = ?", extraction.ID).Updates(map[string]any{
		"summary": "corrected summary", "updated_at": appliedRevision,
	}).Error; err != nil {
		t.Fatalf("apply corrected extraction fixture: %v", err)
	}
	var current models.SourceExtraction
	if err := db.First(&current, "id = ?", extraction.ID).Error; err != nil {
		t.Fatalf("reload corrected extraction fixture: %v", err)
	}
	if err := db.Model(&models.SourceExtractionCorrection{}).Where("id = ?", correction.ID).Updates(map[string]any{
		"phase": correctionPhaseGraphProjected, "before_state_json": string(beforeState), "applied_revision": current.UpdatedAt,
	}).Error; err != nil {
		t.Fatalf("prepare graph-projected correction fixture: %v", err)
	}
	workerID := "lesson-fence-worker-" + uuid.NewString()
	const generation int64 = 19
	leaseCorrectionJob(t, db, job.ID, workerID, generation)
	request := extractionCorrectionMemoryRequest(sourceRecordByID(t, db, extraction.SourceID), extraction, &current)
	internalKey := "source-extraction://" + extraction.ID.String()

	if _, err := repository.PersistExtractionCorrectionLesson(context.Background(), correction.ID, job.ID, workerID,
		generation+1, current.UpdatedAt, owner, internalKey, request); !errors.Is(err, errExtractionCorrectionLeaseLost) {
		t.Fatalf("stale lease write error = %v, want lease lost", err)
	}
	if _, err := repository.PersistExtractionCorrectionLesson(context.Background(), correction.ID, job.ID, workerID,
		generation, current.UpdatedAt.Add(time.Second), owner, internalKey, request); !errors.Is(err, ErrExtractionCorrectionNotFound) {
		t.Fatalf("stale applied revision write error = %v, want correction-state rejection", err)
	}
	var absent int64
	if err := db.Model(&models.ContextMemory{}).Where("owner_identity = ? AND source_extraction_id = ?", owner, extraction.ID).Count(&absent).Error; err != nil || absent != 0 {
		t.Fatalf("stale attempts left %d lesson rows, err=%v; want none", absent, err)
	}

	first, err := repository.PersistExtractionCorrectionLesson(context.Background(), correction.ID, job.ID, workerID,
		generation, current.UpdatedAt, owner, internalKey, request)
	if err != nil {
		t.Fatalf("persist exact correction lesson: %v", err)
	}
	second, err := repository.PersistExtractionCorrectionLesson(context.Background(), correction.ID, job.ID, workerID,
		generation, current.UpdatedAt, owner, internalKey, request)
	if err != nil {
		t.Fatalf("retry exact correction lesson: %v", err)
	}
	if first.ID != second.ID || first.SourceURI != safeEvidenceURI || second.SourceURI != safeEvidenceURI ||
		first.SourceExtractionID == nil || second.SourceExtractionID == nil ||
		*first.SourceExtractionID != extraction.ID || *second.SourceExtractionID != extraction.ID {
		t.Fatalf("idempotent lesson provenance first=%#v second=%#v", first, second)
	}
	var count int64
	if err := db.Model(&models.ContextMemory{}).Where("owner_identity = ? AND source_extraction_id = ? AND kind = ?", owner, extraction.ID, "lesson").Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("exact lesson row count = %d, err=%v; want one", count, err)
	}
}

func sourceRecordByID(t *testing.T, db *gorm.DB, id uuid.UUID) *models.ConnectedSource {
	t.Helper()
	var source models.ConnectedSource
	if err := db.First(&source, "id = ?", id).Error; err != nil {
		t.Fatalf("load source record: %v", err)
	}
	return &source
}

func TestExtractionCorrectionSessionFenceReleaseAllowsDeletion(t *testing.T) {
	db := openExtractionCorrectionFencePostgres(t)
	repository, sourceRecord, extraction, owner, cleanup := newPostgresExtractionCorrectionFixture(t, db)
	t.Cleanup(cleanup)
	createPostgresCorrectionOutboxFixture(
		t, db, extraction, owner, models.SourceExtractionCorrectionPending, models.DurableJobPending,
	)

	ctx, cancel := context.WithCancel(context.Background())
	release, acquired, err := repository.AcquireExtractionCorrectionSessionLock(ctx, owner, extraction.ID)
	if err != nil || !acquired || release == nil {
		cancel()
		t.Fatalf("acquire worker session lock: acquired=%t release=%t err=%v", acquired, release != nil, err)
	}
	cancel()
	release()
	if err := repository.DeleteExtractionForOwner(extraction, sourceRecord, owner); err != nil {
		t.Fatalf("deletion after cancelled worker released its session lock: %v", err)
	}
}

func openExtractionCorrectionFencePostgres(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("HAI_TEST_DATABASE_DSN"))
	if dsn == "" {
		t.Skip("HAI_TEST_DATABASE_DSN not set; skipping PostgreSQL correction-worker fence integration test")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open Postgres test database: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get Postgres test database pool: %v", err)
	}
	sqlDB.SetMaxOpenConns(8)
	t.Cleanup(func() { _ = sqlDB.Close() })
	for _, model := range []any{&models.ConnectedSource{}, &models.SourceExtraction{}, &models.ContextMemory{}, &models.DurableJob{}, &models.SourceExtractionCorrection{}} {
		if !db.Migrator().HasTable(model) {
			t.Fatalf("Postgres test database is missing migrated table for %T", model)
		}
	}
	if !db.Migrator().HasColumn(&models.ContextMemory{}, "SourceExtractionID") {
		t.Fatal("Postgres test database is missing context-memory source extraction identity")
	}
	return db
}

var _ extractionCorrectionWorkerFence = (*GormRepository)(nil)
var _ extractionCorrectionWorkerFence = (*extractionCorrectionFakeRepository)(nil)

type barrierExtractionCorrectionRepository struct {
	*GormRepository
	acquired       chan struct{}
	continueWorker chan struct{}
}

type workerLockCountRepository struct {
	*GormRepository
	sourceLeaseCalls     int
	extractionFenceCalls int
}

func (r *workerLockCountRepository) AcquireSourceSyncLease(ctx context.Context, sourceID uuid.UUID) (func(), bool, error) {
	r.sourceLeaseCalls++
	return r.GormRepository.AcquireSourceSyncLease(ctx, sourceID)
}

func (r *workerLockCountRepository) AcquireExtractionCorrectionSessionLock(
	ctx context.Context,
	ownerIdentity string,
	extractionID uuid.UUID,
) (func(), bool, error) {
	r.extractionFenceCalls++
	return r.GormRepository.AcquireExtractionCorrectionSessionLock(ctx, ownerIdentity, extractionID)
}

type blockingExtractionCorrectionLessonRepository struct {
	*GormRepository
	reached chan struct{}
	resume  chan struct{}
}

func (r *blockingExtractionCorrectionLessonRepository) PersistExtractionCorrectionLesson(
	ctx context.Context,
	correctionID uuid.UUID,
	durableJobID uuid.UUID,
	workerID string,
	generation int64,
	appliedRevision time.Time,
	ownerIdentity string,
	sourceURI string,
	request memory.CreateRequest,
) (*models.ContextMemory, error) {
	close(r.reached)
	select {
	case <-r.resume:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return r.GormRepository.PersistExtractionCorrectionLesson(ctx, correctionID, durableJobID, workerID,
		generation, appliedRevision, ownerIdentity, sourceURI, request)
}

func (r *barrierExtractionCorrectionRepository) AcquireExtractionCorrectionSessionLock(
	ctx context.Context,
	ownerIdentity string,
	extractionID uuid.UUID,
) (func(), bool, error) {
	release, acquired, err := r.GormRepository.AcquireExtractionCorrectionSessionLock(ctx, ownerIdentity, extractionID)
	if err != nil || !acquired {
		return release, acquired, err
	}
	close(r.acquired)
	select {
	case <-r.continueWorker:
		return release, true, nil
	case <-ctx.Done():
		release()
		return nil, false, ctx.Err()
	}
}
