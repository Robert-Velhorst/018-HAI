package source

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/durablejob"
	"automation-hub-backend/internal/models"

	"github.com/google/uuid"
)

// fakeJobRepo is an in-memory durablejob.Repository so the durable scheduler can
// be exercised without a database.
type fakeJobRepo struct {
	jobs map[uuid.UUID]*models.DurableJob
}

func newFakeJobRepo() *fakeJobRepo { return &fakeJobRepo{jobs: map[uuid.UUID]*models.DurableJob{}} }

func (f *fakeJobRepo) Enqueue(job *models.DurableJob) (*models.DurableJob, error) {
	if job.ID == uuid.Nil {
		job.ID = uuid.New()
	}
	if job.Status == "" {
		job.Status = models.DurableJobPending
	}
	stored := *job
	f.jobs[job.ID] = &stored
	return &stored, nil
}

func (f *fakeJobRepo) EnqueueIfNoActive(job *models.DurableJob) (bool, error) {
	if job.Queue == "" {
		job.Queue = "default"
	}
	for _, existing := range f.jobs {
		if existing.Queue == job.Queue && existing.Kind == job.Kind &&
			(existing.Status == models.DurableJobPending || existing.Status == models.DurableJobRunning) {
			return false, nil
		}
	}
	_, err := f.Enqueue(job)
	return err == nil, err
}

func (f *fakeJobRepo) EnqueueIfNoActiveMatchingPayload(job *models.DurableJob) (bool, error) {
	if job.Queue == "" {
		job.Queue = "default"
	}
	for _, existing := range f.jobs {
		if existing.Queue == job.Queue && existing.Kind == job.Kind && existing.Payload == job.Payload &&
			(existing.Status == models.DurableJobPending || existing.Status == models.DurableJobRunning) {
			return false, nil
		}
	}
	_, err := f.Enqueue(job)
	return err == nil, err
}

func (f *fakeJobRepo) CompleteRecurring(id uuid.UUID, workerID string, leaseGeneration int64, now time.Time, terminalStatus string, attempts int, lastErr string, next *models.DurableJob) (bool, bool, error) {
	job := f.jobs[id]
	if job == nil || job.Status != models.DurableJobRunning || job.LockedBy != workerID || job.LeaseGeneration != leaseGeneration {
		return false, false, nil
	}
	job.Status = terminalStatus
	job.CompletedAt = &now
	job.LockedBy = ""
	job.LockedAt = nil
	job.LastError = lastErr
	if terminalStatus == models.DurableJobDead {
		job.Attempts = attempts
	}
	for _, existing := range f.jobs {
		if existing.ID != id && existing.Queue == next.Queue && existing.Kind == next.Kind && (existing.Status == models.DurableJobPending || existing.Status == models.DurableJobRunning) {
			return true, false, nil
		}
	}
	_, err := f.Enqueue(next)
	return true, err == nil, err
}

func (f *fakeJobRepo) ClaimDue(workerID, queue string, now time.Time, limit int) ([]models.DurableJob, error) {
	if queue == "" {
		queue = "default"
	}
	claimed := []models.DurableJob{}
	for _, job := range f.jobs {
		if len(claimed) >= limit {
			break
		}
		if job.Queue != queue || job.Status != models.DurableJobPending || job.RunAt.After(now) {
			continue
		}
		job.Status = models.DurableJobRunning
		lockedAt := now
		job.LockedBy, job.LockedAt = workerID, &lockedAt
		job.LeaseGeneration++
		claimed = append(claimed, *job)
	}
	return claimed, nil
}

func (f *fakeJobRepo) MarkSucceeded(id uuid.UUID, workerID string, leaseGeneration int64, now time.Time) (bool, error) {
	job := f.jobs[id]
	if !sourceFakeLeaseOwned(job, workerID, leaseGeneration) {
		return false, nil
	}
	job.Status = models.DurableJobSucceeded
	job.CompletedAt = &now
	job.LockedBy, job.LockedAt = "", nil
	return true, nil
}

func (f *fakeJobRepo) MarkForRetry(id uuid.UUID, workerID string, leaseGeneration int64, runAt time.Time, attempts int, lastErr string) (bool, error) {
	job := f.jobs[id]
	if !sourceFakeLeaseOwned(job, workerID, leaseGeneration) {
		return false, nil
	}
	job.Status, job.RunAt, job.Attempts, job.LastError = models.DurableJobPending, runAt, attempts, lastErr
	job.LockedBy, job.LockedAt = "", nil
	return true, nil
}

func (f *fakeJobRepo) MarkDeferred(id uuid.UUID, workerID string, leaseGeneration int64, runAt time.Time, reason string) (bool, error) {
	job := f.jobs[id]
	if !sourceFakeLeaseOwned(job, workerID, leaseGeneration) {
		return false, nil
	}
	job.Status, job.RunAt, job.LastError = models.DurableJobPending, runAt, reason
	job.LockedBy, job.LockedAt = "", nil
	return true, nil
}

func (f *fakeJobRepo) MarkDead(id uuid.UUID, workerID string, leaseGeneration int64, now time.Time, attempts int, lastErr string) (bool, error) {
	job := f.jobs[id]
	if !sourceFakeLeaseOwned(job, workerID, leaseGeneration) {
		return false, nil
	}
	job.Status, job.Attempts, job.LastError = models.DurableJobDead, attempts, lastErr
	job.CompletedAt = &now
	job.LockedBy, job.LockedAt = "", nil
	return true, nil
}

func (f *fakeJobRepo) ExtendLease(id uuid.UUID, workerID string, leaseGeneration int64, now time.Time) (bool, error) {
	job := f.jobs[id]
	if !sourceFakeLeaseOwned(job, workerID, leaseGeneration) {
		return false, nil
	}
	job.LockedAt = &now
	return true, nil
}

func sourceFakeLeaseOwned(job *models.DurableJob, workerID string, leaseGeneration int64) bool {
	return job != nil &&
		job.Status == models.DurableJobRunning &&
		job.LockedBy == workerID &&
		job.LeaseGeneration == leaseGeneration
}

func (f *fakeJobRepo) ReapExpiredLeases(now time.Time, lease time.Duration) (int, error) {
	return f.ReapExpiredLeasesForQueue("source", now, lease)
}

func (f *fakeJobRepo) ReapExpiredLeasesForQueue(queue string, now time.Time, lease time.Duration) (int, error) {
	cutoff := now.Add(-lease)
	reaped := 0
	for _, job := range f.jobs {
		if job.Queue != queue || job.Status != models.DurableJobRunning || job.LockedAt == nil || !job.LockedAt.Before(cutoff) {
			continue
		}
		job.Status = models.DurableJobPending
		job.LockedBy, job.LockedAt = "", nil
		reaped++
	}
	return reaped, nil
}

func (f *fakeJobRepo) Find(id uuid.UUID) (*models.DurableJob, error) { return f.jobs[id], nil }

func (f *fakeJobRepo) FindLatestDeadByPayload(queue, kind, payload string) (*models.DurableJob, error) {
	var latest *models.DurableJob
	for _, job := range f.jobs {
		if job.Queue != queue || job.Kind != kind || job.Payload != payload || job.Status != models.DurableJobDead || job.CompletedAt == nil {
			continue
		}
		if latest == nil || job.CompletedAt.After(*latest.CompletedAt) {
			latest = job
		}
	}
	return latest, nil
}

func (f *fakeJobRepo) CountActiveByKind(kind string) (int64, error) {
	var count int64
	for _, job := range f.jobs {
		if job.Kind == kind && (job.Status == models.DurableJobPending || job.Status == models.DurableJobRunning) {
			count++
		}
	}
	return count, nil
}

func (f *fakeJobRepo) byKind(kind string) []models.DurableJob {
	found := []models.DurableJob{}
	for _, job := range f.jobs {
		if job.Kind == kind {
			found = append(found, *job)
		}
	}
	return found
}

// localFolderSource builds a due local-folder source backed by a temp directory.
func localFolderSource(t *testing.T, name string) (*models.ConnectedSource, string) {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(root+"/"+name, 0o755); err != nil {
		t.Fatalf("create fixture dir: %v", err)
	}
	writeTestFile(t, root+"/"+name+"/brief.md", "Follow up: prepare the delivery checklist by Friday.")
	t.Setenv("CONNECTED_SOURCE_LOCAL_ROOT", root)
	return &models.ConnectedSource{
		ID: uuid.New(), OwnerIdentity: "alice", ConnectorKey: "local-folder", Name: name,
		Category: "local_folder", Enabled: true, LocalOnly: true, Status: "active",
		SyncFrequency: "1m", SyncTarget: name,
	}, root
}

func TestDurableScanEnqueuesOneSyncPerDueSourceAndReschedulesItself(t *testing.T) {
	source, _ := localFolderSource(t, "alice")
	repo := newFakeSourceRepo(source)
	service := NewService(repo, &fakeSourceMemoryService{})

	jobs := newFakeJobRepo()
	runner := durablejob.NewRunner(jobs, durablejob.Options{WorkerID: "w1"})
	if err := RegisterDurableScheduling(runner, service, time.Minute); err != nil {
		t.Fatalf("RegisterDurableScheduling: %v", err)
	}
	// Startup must schedule exactly one scan job.
	if got := len(jobs.byKind(JobKindScan)); got != 1 {
		t.Fatalf("scan jobs after registration = %d, want 1", got)
	}

	// Run the scan.
	if _, err := runner.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	syncJobs := jobs.byKind(JobKindSync)
	if len(syncJobs) != 1 {
		t.Fatalf("sync jobs = %d, want 1 (one per due source)", len(syncJobs))
	}
	if syncJobs[0].MaxAttempts != syncMaxAttempts {
		t.Fatalf("sync MaxAttempts = %d, want %d", syncJobs[0].MaxAttempts, syncMaxAttempts)
	}
	// The scan must have re-scheduled itself for the next interval.
	pendingScans := 0
	for _, job := range jobs.byKind(JobKindScan) {
		if job.Status == models.DurableJobPending {
			pendingScans++
		}
	}
	if pendingScans != 1 {
		t.Fatalf("pending scan jobs = %d, want exactly 1 (self-rescheduled)", pendingScans)
	}
}

func TestDurableScanDoesNotDuplicateAnActiveSourceSync(t *testing.T) {
	source, _ := localFolderSource(t, "alice")
	repo := newFakeSourceRepo(source)
	service := NewService(repo, &fakeSourceMemoryService{})
	jobs := newFakeJobRepo()
	runner := durablejob.NewRunner(jobs, durablejob.Options{WorkerID: "w1"})

	// Invoke the scanner twice before the queued source job can run. A source
	// retry or a slow remote API must not build an unbounded duplicate backlog.
	work := scanWork(runner, service)
	if err := work(context.Background()); err != nil {
		t.Fatalf("first scan: %v", err)
	}
	if err := work(context.Background()); err != nil {
		t.Fatalf("second scan: %v", err)
	}
	if got := len(jobs.byKind(JobKindSync)); got != 1 {
		t.Fatalf("sync jobs after duplicate scan = %d, want 1", got)
	}
}

func TestDurableScanDoesNotDuplicateAnActiveSourceSyncAfterRename(t *testing.T) {
	source, _ := localFolderSource(t, "alice")
	repo := newFakeSourceRepo(source)
	service := NewService(repo, &fakeSourceMemoryService{})
	jobs := newFakeJobRepo()
	runner := durablejob.NewRunner(jobs, durablejob.Options{WorkerID: "w1"})
	work := scanWork(runner, service)

	if err := work(context.Background()); err != nil {
		t.Fatalf("first scan: %v", err)
	}
	repo.sources[source.ID].Name = "alice-renamed"
	if err := work(context.Background()); err != nil {
		t.Fatalf("scan after rename: %v", err)
	}
	if got := len(jobs.byKind(JobKindSync)); got != 1 {
		t.Fatalf("sync jobs after rename = %d, want 1", got)
	}
}

func TestDurableScanHonorsSourceIntervalAfterDeadLetter(t *testing.T) {
	source, _ := localFolderSource(t, "alice")
	repo := newFakeSourceRepo(source)
	service := NewService(repo, &fakeSourceMemoryService{})
	jobs := newFakeJobRepo()
	payload := `{"sourceId":"` + source.ID.String() + `"}`
	completedAt := time.Now().UTC()
	if _, err := jobs.Enqueue(&models.DurableJob{
		Queue: "source", Kind: JobKindSync, Payload: payload, Status: models.DurableJobDead,
		MaxAttempts: syncMaxAttempts, Attempts: syncMaxAttempts, CompletedAt: &completedAt,
	}); err != nil {
		t.Fatalf("seed dead source sync: %v", err)
	}

	runner := durablejob.NewRunner(jobs, durablejob.Options{WorkerID: "w1", Queue: "source"})
	if err := scanWork(runner, service)(context.Background()); err != nil {
		t.Fatalf("scan after dead-letter: %v", err)
	}
	if got := len(jobs.byKind(JobKindSync)); got != 1 {
		t.Fatalf("sync jobs before the next source interval = %d, want only the dead-lettered job", got)
	}

	completedAt = time.Now().UTC().Add(-2 * time.Minute)
	if err := scanWork(runner, service)(context.Background()); err != nil {
		t.Fatalf("scan after cooldown: %v", err)
	}
	if got := len(jobs.byKind(JobKindSync)); got != 2 {
		t.Fatalf("sync jobs after the source interval = %d, want one fresh scheduled cycle", got)
	}
}

func TestDurableSyncJobActuallySyncsTheSource(t *testing.T) {
	source, _ := localFolderSource(t, "alice")
	repo := newFakeSourceRepo(source)
	service := NewService(repo, &fakeSourceMemoryService{})

	jobs := newFakeJobRepo()
	runner := durablejob.NewRunner(jobs, durablejob.Options{WorkerID: "w1"})
	if err := RegisterDurableScheduling(runner, service, time.Minute); err != nil {
		t.Fatalf("RegisterDurableScheduling: %v", err)
	}
	// Cycle 1 runs the scan (enqueues the sync); cycle 2 runs the sync itself.
	for i := 0; i < 2; i++ {
		if _, err := runner.RunOnce(context.Background()); err != nil {
			t.Fatalf("RunOnce %d: %v", i, err)
		}
	}
	updated, err := repo.FindSource(source.ID)
	if err != nil {
		t.Fatalf("FindSource: %v", err)
	}
	if updated.LastSyncedAt == nil {
		t.Fatal("durable sync job did not actually sync the source")
	}
	for _, job := range jobs.byKind(JobKindSync) {
		if job.Status != models.DurableJobSucceeded {
			t.Fatalf("sync job status = %q (%s), want succeeded", job.Status, job.LastError)
		}
	}
}

func TestDurableSchedulerDoesNotProcessQueuedWorkWhenBackgroundIsStopped(t *testing.T) {
	source, _ := localFolderSource(t, "alice")
	repo := newFakeSourceRepo(source)
	service := NewService(repo, &fakeSourceMemoryService{})
	jobs := newFakeJobRepo()
	runner := durablejob.NewRunner(jobs, durablejob.Options{WorkerID: "w1"})

	if err := RegisterDurableScheduling(runner, service, time.Minute, func() bool { return false }); err != nil {
		t.Fatalf("RegisterDurableScheduling: %v", err)
	}
	if _, err := runner.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if got := len(jobs.byKind(JobKindSync)); got != 0 {
		t.Fatalf("queued sync jobs = %d, want none while background is stopped", got)
	}
	updated, err := repo.FindSource(source.ID)
	if err != nil {
		t.Fatalf("FindSource: %v", err)
	}
	if updated.LastSyncedAt != nil {
		t.Fatal("stopped background scheduler must not sync the source")
	}
}

func TestRegisteredDurableWorkerRecoversExtractionCorrectionAfterRestart(t *testing.T) {
	repo, extraction, firstService, _ := newExtractionCorrectionFixture(t, false)
	value := "recovered after restart"
	view := submitTestCorrection(t, firstService, extraction, ExtractionPatch{Summary: &value}, "scheduler-recovery-correction-key")
	correctionID, err := uuid.Parse(view.ID)
	if err != nil {
		t.Fatalf("parse correction ID: %v", err)
	}
	correction := repo.corrections[correctionID]
	if correction == nil {
		t.Fatal("submission did not persist correction intent")
	}
	job := repo.correctionJobs[correction.DurableJobID]
	if job == nil {
		t.Fatal("submission did not persist durable source job")
	}
	jobs := newFakeJobRepo()
	// The runner and correction repository share the durable record just as the
	// production repositories share the durable_jobs row.
	jobs.jobs[job.ID] = job
	firstRunner := durablejob.NewRunner(jobs, durablejob.Options{WorkerID: "before-restart", Queue: "source"})
	if err := RegisterDurableScheduling(firstRunner, firstService, time.Minute); err != nil {
		t.Fatalf("register before restart: %v", err)
	}

	// A new service and runner register the same persisted outbox after restart.
	restartedService := NewService(repo, firstService.memoryService)
	restartedRunner := durablejob.NewRunner(jobs, durablejob.Options{WorkerID: "after-restart", Queue: "source"})
	if err := RegisterDurableScheduling(restartedRunner, restartedService, time.Minute); err != nil {
		t.Fatalf("register after restart: %v", err)
	}
	if processed, err := restartedRunner.RunOnce(context.Background()); err != nil || processed == 0 {
		t.Fatalf("recovery pass processed=%d err=%v", processed, err)
	}
	storedJob := jobs.jobs[job.ID]
	if storedJob.Status != models.DurableJobSucceeded {
		t.Fatalf("recovered correction job status=%q error=%q, want succeeded", storedJob.Status, storedJob.LastError)
	}
	storedCorrection := repo.corrections[correctionID]
	if storedCorrection.Status != models.SourceExtractionCorrectionCompleted || storedCorrection.Phase != correctionPhaseCompleted {
		t.Fatalf("recovered correction state=%#v, want completed", storedCorrection)
	}
	if got := repo.extractions[extraction.ID].Summary; got != value {
		t.Fatalf("recovered source patch=%q, want %q", got, value)
	}
}

func TestRegisterDurableSchedulingIsSingletonAcrossRestarts(t *testing.T) {
	source, _ := localFolderSource(t, "alice")
	service := NewService(newFakeSourceRepo(source), &fakeSourceMemoryService{})
	jobs := newFakeJobRepo()

	// Three "process starts" against the same durable queue.
	for i := 0; i < 3; i++ {
		runner := durablejob.NewRunner(jobs, durablejob.Options{WorkerID: "w"})
		if err := RegisterDurableScheduling(runner, service, time.Minute); err != nil {
			t.Fatalf("restart %d: %v", i, err)
		}
	}
	if got := len(jobs.byKind(JobKindScan)); got != 1 {
		t.Fatalf("scan jobs after 3 restarts = %d, want 1 (must stay singleton)", got)
	}
}

func TestDurableSyncHandlerDeadLettersMalformedPayload(t *testing.T) {
	service := NewService(newFakeSourceRepo(), &fakeSourceMemoryService{})
	handler := syncHandler(service)

	if err := handler(context.Background(), durablejob.Job{Payload: "not-json"}); err == nil {
		t.Fatal("expected an error for a malformed payload")
	}
	if err := handler(context.Background(), durablejob.Job{Payload: `{"sourceId":"nope"}`}); err == nil {
		t.Fatal("expected an error for an invalid source id")
	}
}

func TestDurableSyncDefersBusySourceWithoutConsumingRetryAndResumes(t *testing.T) {
	src, _ := localFolderSource(t, "alice")
	repo := newFakeSourceRepo(src)
	svc := NewService(repo, &fakeSourceMemoryService{}).(*service)
	now := time.Now().UTC()
	jobs := newFakeJobRepo()
	runner := durablejob.NewRunner(jobs, durablejob.Options{
		WorkerID: "source-worker", Queue: "source", Now: func() time.Time { return now },
	})
	runner.Register(JobKindSync, syncHandler(svc))
	payload := `{"sourceId":"` + src.ID.String() + `"}`
	job, err := runner.Enqueue(JobKindSync, payload, now, syncMaxAttempts)
	if err != nil {
		t.Fatalf("enqueue sync: %v", err)
	}

	// Simulate another worker holding the source lease when this job is claimed.
	svc.beginSync(src.ID)
	if processed, err := runner.RunOnce(context.Background()); err != nil || processed != 1 {
		t.Fatalf("busy-source pass processed=%d err=%v, want one handled job", processed, err)
	}
	stored := jobs.jobs[job.ID]
	if stored.Status != models.DurableJobPending || stored.Attempts != 0 {
		t.Fatalf("busy-source job status/attempts=%q/%d, want pending/0", stored.Status, stored.Attempts)
	}
	if !stored.RunAt.Equal(now.Add(durablejob.DefaultDeferDelay)) {
		t.Fatalf("deferred runAt=%s, want %s", stored.RunAt, now.Add(durablejob.DefaultDeferDelay))
	}
	if stored.LockedBy != "" || stored.LockedAt != nil {
		t.Fatalf("deferred job retained lease owner/time: %q/%v", stored.LockedBy, stored.LockedAt)
	}

	// If the competing sync failed or was cancelled, the still-due source is
	// retried when the defer expires instead of waiting for the next scan.
	svc.endSync(src.ID)
	now = stored.RunAt
	if processed, err := runner.RunOnce(context.Background()); err != nil || processed != 1 {
		t.Fatalf("resumed-source pass processed=%d err=%v, want one handled job", processed, err)
	}
	stored = jobs.jobs[job.ID]
	if stored.Status != models.DurableJobSucceeded {
		t.Fatalf("resumed job status=%q error=%q, want succeeded", stored.Status, stored.LastError)
	}
	updated, err := repo.FindSource(src.ID)
	if err != nil {
		t.Fatalf("FindSource after resumed sync: %v", err)
	}
	if updated.LastSyncedAt == nil {
		t.Fatal("still-due source was not synced after the concurrent attempt ended")
	}
}

func TestDurableSyncDoesNotRepeatWhenConcurrentSyncMakesJobStale(t *testing.T) {
	src, _ := localFolderSource(t, "alice")
	repo := newFakeSourceRepo(src)
	svc := NewService(repo, &fakeSourceMemoryService{}).(*service)
	now := time.Now().UTC()
	jobs := newFakeJobRepo()
	runner := durablejob.NewRunner(jobs, durablejob.Options{
		WorkerID: "source-worker", Queue: "source", Now: func() time.Time { return now },
	})
	runner.Register(JobKindSync, syncHandler(svc))
	job, err := runner.Enqueue(JobKindSync, `{"sourceId":"`+src.ID.String()+`"}`, now, syncMaxAttempts)
	if err != nil {
		t.Fatalf("enqueue sync: %v", err)
	}
	svc.beginSync(src.ID)
	if _, err := runner.RunOnce(context.Background()); err != nil {
		t.Fatalf("busy-source pass: %v", err)
	}
	svc.endSync(src.ID)

	// Model the other worker committing a successful sync before this
	// deferred job runs. Its freshness makes the durable job obsolete.
	completedAt := time.Now().UTC()
	repo.sources[src.ID].LastSyncedAt = &completedAt
	now = jobs.jobs[job.ID].RunAt
	if processed, err := runner.RunOnce(context.Background()); err != nil || processed != 1 {
		t.Fatalf("stale-job pass processed=%d err=%v, want one handled job", processed, err)
	}
	if got := jobs.jobs[job.ID].Status; got != models.DurableJobSucceeded {
		t.Fatalf("stale sync job status=%q, want succeeded", got)
	}
	if len(repo.jobs) != 0 {
		t.Fatalf("stale queued work started %d source sync(s), want none", len(repo.jobs))
	}
}

type durableSyncFailureService struct {
	Service
	err    error
	dueErr error
}

func (s *durableSyncFailureService) Sync(sourceID uuid.UUID, request ImportRequest) (*SyncResult, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.Service.Sync(sourceID, request)
}

func (s *durableSyncFailureService) DueSources(now time.Time) ([]models.ConnectedSource, error) {
	if s.dueErr != nil {
		return nil, s.dueErr
	}
	return s.Service.DueSources(now)
}

func TestDurableDeferredSyncRetainsDueRecheckAfterTemporaryReadFailure(t *testing.T) {
	src, _ := localFolderSource(t, "alice")
	base := NewService(newFakeSourceRepo(src), &fakeSourceMemoryService{}).(*service)
	service := &durableSyncFailureService{Service: base}
	now := time.Now().UTC()
	jobs := newFakeJobRepo()
	runner := durablejob.NewRunner(jobs, durablejob.Options{
		WorkerID: "source-worker", Queue: "source", Now: func() time.Time { return now },
	})
	runner.Register(JobKindSync, syncHandler(service))
	job, err := runner.Enqueue(JobKindSync, `{"sourceId":"`+src.ID.String()+`"}`, now, syncMaxAttempts)
	if err != nil {
		t.Fatalf("enqueue sync: %v", err)
	}

	base.beginSync(src.ID)
	if _, err := runner.RunOnce(context.Background()); err != nil {
		t.Fatalf("initial busy-source pass: %v", err)
	}
	base.endSync(src.ID)
	service.dueErr = errors.New("temporary source-list failure")
	now = jobs.jobs[job.ID].RunAt
	if processed, err := runner.RunOnce(context.Background()); err != nil || processed != 1 {
		t.Fatalf("due-recheck failure pass processed=%d err=%v, want one handled job", processed, err)
	}
	stored := jobs.jobs[job.ID]
	if stored.Status != models.DurableJobPending || stored.Attempts != 1 {
		t.Fatalf("failed recheck status/attempts=%q/%d, want pending/1", stored.Status, stored.Attempts)
	}
	if !strings.HasPrefix(stored.LastError, "job deferred:") {
		t.Fatalf("failed recheck lost deferred intent marker: %q", stored.LastError)
	}
	service.dueErr = nil
	now = stored.RunAt
	if processed, err := runner.RunOnce(context.Background()); err != nil || processed != 1 {
		t.Fatalf("recovered due-recheck pass processed=%d err=%v, want one handled job", processed, err)
	}
	if got := jobs.jobs[job.ID].Status; got != models.DurableJobSucceeded {
		t.Fatalf("recovered deferred job status=%q, want succeeded", got)
	}
}

func TestDurableSyncFailureUsesRetryBackoffAndReleasesLease(t *testing.T) {
	src, _ := localFolderSource(t, "alice")
	service := &durableSyncFailureService{
		Service: NewService(newFakeSourceRepo(src), &fakeSourceMemoryService{}),
		err:     errors.New("temporary provider failure"),
	}
	now := time.Now().UTC()
	jobs := newFakeJobRepo()
	runner := durablejob.NewRunner(jobs, durablejob.Options{
		WorkerID: "source-worker", Queue: "source", Now: func() time.Time { return now },
	})
	runner.Register(JobKindSync, syncHandler(service))
	job, err := runner.Enqueue(JobKindSync, `{"sourceId":"`+src.ID.String()+`"}`, now, syncMaxAttempts)
	if err != nil {
		t.Fatalf("enqueue sync: %v", err)
	}
	if processed, err := runner.RunOnce(context.Background()); err != nil || processed != 1 {
		t.Fatalf("failed-sync pass processed=%d err=%v, want one handled job", processed, err)
	}
	stored := jobs.jobs[job.ID]
	if stored.Status != models.DurableJobPending || stored.Attempts != 1 {
		t.Fatalf("failed job status/attempts=%q/%d, want pending/1", stored.Status, stored.Attempts)
	}
	if !stored.RunAt.Equal(now.Add(time.Second)) {
		t.Fatalf("retry runAt=%s, want first backoff at %s", stored.RunAt, now.Add(time.Second))
	}
	if stored.LockedBy != "" || stored.LockedAt != nil {
		t.Fatalf("retry job retained lease owner/time: %q/%v", stored.LockedBy, stored.LockedAt)
	}
}

func TestDurableSyncRecoversAnExpiredLeaseAfterRestart(t *testing.T) {
	src, _ := localFolderSource(t, "alice")
	repo := newFakeSourceRepo(src)
	service := NewService(repo, &fakeSourceMemoryService{})
	now := time.Now().UTC()
	lockedAt := now.Add(-durablejob.DefaultLease - time.Second)
	job := &models.DurableJob{
		ID: uuid.New(), Queue: "source", Kind: JobKindSync,
		Payload: `{"sourceId":"` + src.ID.String() + `"}`,
		Status:  models.DurableJobRunning, RunAt: lockedAt,
		LockedBy: "crashed-worker", LockedAt: &lockedAt, LeaseGeneration: 4,
		MaxAttempts: syncMaxAttempts,
	}
	jobs := newFakeJobRepo()
	jobs.jobs[job.ID] = job
	runner := durablejob.NewRunner(jobs, durablejob.Options{
		WorkerID: "restarted-worker", Queue: "source", Now: func() time.Time { return now },
	})
	runner.Register(JobKindSync, syncHandler(service))
	if processed, err := runner.RunOnce(context.Background()); err != nil || processed != 1 {
		t.Fatalf("recovery pass processed=%d err=%v, want one handled job", processed, err)
	}
	stored := jobs.jobs[job.ID]
	if stored.Status != models.DurableJobSucceeded || stored.LeaseGeneration != 5 {
		t.Fatalf("recovered job status/generation=%q/%d, want succeeded/5", stored.Status, stored.LeaseGeneration)
	}
	if stored.LockedBy != "" || stored.LockedAt != nil {
		t.Fatalf("recovered job retained lease owner/time: %q/%v", stored.LockedBy, stored.LockedAt)
	}
	if len(repo.jobs) != 1 || repo.jobs[0].Status != "completed" {
		t.Fatalf("source sync history after recovery=%#v, want one completed sync", repo.jobs)
	}
}

func TestDurablePollIntervalUsesSafeBounds(t *testing.T) {
	for _, value := range []string{"", "1", "14", "3601", "invalid"} {
		t.Setenv("SOURCE_WORKER_POLL_SECONDS", value)
		if got := durablePollInterval(); got != defaultDurablePollInterval {
			t.Fatalf("durablePollInterval() with %q = %s, want default %s", value, got, defaultDurablePollInterval)
		}
	}
	t.Setenv("SOURCE_WORKER_POLL_SECONDS", "15")
	if got := durablePollInterval(); got != minDurablePollInterval {
		t.Fatalf("durablePollInterval() = %s, want %s", got, minDurablePollInterval)
	}
	t.Setenv("SOURCE_WORKER_POLL_SECONDS", "3600")
	if got := durablePollInterval(); got != maxDurablePollInterval {
		t.Fatalf("durablePollInterval() = %s, want %s", got, maxDurablePollInterval)
	}
}
