package source

import (
	"context"
	"os"
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
	return true, nil
}

func (f *fakeJobRepo) MarkForRetry(id uuid.UUID, workerID string, leaseGeneration int64, runAt time.Time, attempts int, lastErr string) (bool, error) {
	job := f.jobs[id]
	if !sourceFakeLeaseOwned(job, workerID, leaseGeneration) {
		return false, nil
	}
	job.Status, job.RunAt, job.Attempts, job.LastError = models.DurableJobPending, runAt, attempts, lastErr
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
	return 0, nil
}

func (f *fakeJobRepo) Find(id uuid.UUID) (*models.DurableJob, error) { return f.jobs[id], nil }

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

func TestDurableSyncHandlerTreatsInProgressAsSuccess(t *testing.T) {
	src, _ := localFolderSource(t, "alice")
	repo := newFakeSourceRepo(src)
	svc := NewService(repo, &fakeSourceMemoryService{}).(*service)

	// Mark a sync as already running, as another worker would have.
	svc.beginSync(src.ID)
	defer svc.endSync(src.ID)

	payload := `{"sourceId":"` + src.ID.String() + `"}`
	if err := syncHandler(svc)(context.Background(), durablejob.Job{Payload: payload}); err != nil {
		t.Fatalf("in-progress sync should not fail the job (it would retry-storm): %v", err)
	}
}
