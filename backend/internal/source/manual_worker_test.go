package source

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/durablejob"
	"automation-hub-backend/internal/identity"
	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/safety"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestManualWorkerReadinessAcceptsExplicitWorkWithoutEnablingWebhookWorker(t *testing.T) {
	src, _ := localFolderSource(t, "alice")
	repo := &manualSyncSubmitTestRepo{
		fakeSourceRepo: newFakeSourceRepo(src),
		jobs:           map[string]models.SourceSyncJob{},
		durable:        map[uuid.UUID]models.DurableJob{},
	}
	svc := NewService(repo, nil).(*service)
	// This is the readiness contract needed when only the manual runner starts.
	svc.setManualOnlySyncWorkerReady(true)
	view, created, err := svc.SubmitManualSync("alice", src.ID, "manual-only-explicit-request", ManualSyncRequest{})
	if err != nil || !created || view.Status != "queued" {
		t.Fatalf("manual-only submit = (%#v, %v, %v), want queued", view, created, err)
	}
	if svc.manualSyncWorkerAvailable() {
		t.Fatal("manual-only readiness incorrectly enables the webhook worker")
	}
	if _, _, err := svc.SubmitManualSync("bob", src.ID, "manual-only-cross-owner-request", ManualSyncRequest{}); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("foreign submit = %v, want not found", err)
	}
	repo.sources[src.ID].Status = "paused"
	if _, _, err := svc.SubmitManualSync("alice", src.ID, "manual-only-paused-source-request", ManualSyncRequest{}); !errors.Is(err, ErrManualSyncSourceDisabled) {
		t.Fatalf("paused-source submit = %v, want source disabled", err)
	}
	svc.setManualOnlySyncWorkerReady(false)
	if _, _, err := svc.SubmitManualSync("alice", src.ID, "manual-only-after-shutdown", ManualSyncRequest{}); !errors.Is(err, ErrManualSyncWorkerUnavailable) {
		t.Fatalf("submit after shutdown = %v, want unavailable", err)
	}
	retry, created, err := svc.SubmitManualSync("alice", src.ID, "manual-only-explicit-request", ManualSyncRequest{})
	if err != nil || created || retry.ID != view.ID || len(repo.jobs) != 1 || len(repo.durable) != 1 {
		t.Fatalf("idempotent retry after shutdown = (%#v, %v, %v), want original record only", retry, created, err)
	}
}

func TestManualWorkerShutdownClearsOnlyItsReadiness(t *testing.T) {
	svc := NewService(newFakeSourceRepo(), nil).(*service)
	svc.setManualOnlySyncWorkerReady(true)
	svc.setManualSyncWorkerReady(true)
	runner := newManualSourceRunner(newFakeJobRepo(), svc, func() bool { return false })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	runManualSourceWorker(ctx, svc, runner, time.Hour)
	if svc.manualOnlySyncWorkerReady.Load() || !svc.manualSyncWorkerAvailable() {
		t.Fatal("manual shutdown retained its readiness or disabled the full source worker")
	}
}

type manualWorkerFailingExecutor struct{ Service }

func (*manualWorkerFailingExecutor) RunManualSyncJob(context.Context, manualSyncPayload, uuid.UUID, int, int) error {
	return errors.New("temporary sync failure")
}

type manualWorkerAttemptAwareRepo struct{ *fakeJobRepo }

func (r *manualWorkerAttemptAwareRepo) MarkSucceededWithAttempts(id uuid.UUID, workerID string, generation int64, now time.Time, attempts int) (bool, error) {
	owned, err := r.MarkSucceeded(id, workerID, generation, now)
	if owned && err == nil {
		r.jobs[id].Attempts = attempts
	}
	return owned, err
}

func TestManualWorkerRepositoryPreservesSuccessfulAttemptTelemetry(t *testing.T) {
	queue := &manualWorkerAttemptAwareRepo{newFakeJobRepo()}
	now := time.Now().UTC()
	job, err := queue.Enqueue(&models.DurableJob{Queue: "source", Kind: JobKindManualSync, RunAt: now})
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := queue.ClaimDue("manual-worker", "source", now, 1)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claim = (%#v, %v)", claimed, err)
	}
	repo := &manualWorkerRepository{Repository: queue}
	if owned, err := repo.MarkSucceededWithAttempts(job.ID, "manual-worker", claimed[0].LeaseGeneration, now, 1); err != nil || !owned {
		t.Fatalf("complete = (%v, %v)", owned, err)
	}
	if queue.jobs[job.ID].Attempts != 1 || queue.jobs[job.ID].Status != models.DurableJobSucceeded {
		t.Fatalf("successful attempt telemetry = %#v", queue.jobs[job.ID])
	}
}

func TestManualWorkerUsesBoundedDurableRetries(t *testing.T) {
	t.Cleanup(safety.SetEmergencyStopProvider(safety.EmergencyStopProviderFunc(func() (bool, string, error) {
		return false, "", nil
	})))
	queue := newFakeJobRepo()
	job, err := queue.Enqueue(&models.DurableJob{Queue: "source", Kind: JobKindManualSync,
		Payload: `{}`, RunAt: time.Now().Add(-time.Second), MaxAttempts: syncMaxAttempts})
	if err != nil {
		t.Fatal(err)
	}
	runner := newManualSourceRunner(queue, &manualWorkerFailingExecutor{NewService(newFakeSourceRepo(), nil)}, func() bool { return true })
	for attempt := 1; attempt <= syncMaxAttempts; attempt++ {
		stored := queue.jobs[job.ID]
		stored.RunAt = time.Now().Add(-time.Second)
		if processed, err := runner.RunOnce(context.Background()); err != nil || processed != 1 {
			t.Fatalf("attempt %d pass = (%d, %v)", attempt, processed, err)
		}
		want := models.DurableJobPending
		if attempt == syncMaxAttempts {
			want = models.DurableJobDead
		}
		if stored.Attempts != attempt || stored.Status != want || stored.LockedBy != "" || stored.LockedAt != nil {
			t.Fatalf("attempt %d: %#v", attempt, stored)
		}
	}
	if processed, err := runner.RunOnce(context.Background()); err != nil || processed != 0 || len(queue.jobs) != 1 {
		t.Fatalf("exhausted pass = (%d, %v), jobs=%d; want no repeat or scan", processed, err, len(queue.jobs))
	}
}

func TestManualWorkerRequiresPolicyAndContextBeforeDatabaseAccess(t *testing.T) {
	svc := NewService(newFakeSourceRepo(), nil)
	if err := startDurableManualWorker(context.Background(), svc); err == nil {
		t.Fatal("worker accepted missing safety gate")
	}
	if err := startDurableManualWorker(context.Background(), svc, nil); err == nil {
		t.Fatal("worker accepted nil safety gate")
	}
	if err := startDurableManualWorker(nil, svc, func() bool { return true }); err == nil {
		t.Fatal("worker accepted nil lifecycle context")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := startDurableManualWorker(ctx, svc, func() bool { return true }); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled startup = %v, want context cancellation", err)
	}
}

func TestManualWorkerPausedDefersThenCompletesExistingLocalSource(t *testing.T) {
	t.Cleanup(safety.SetEmergencyStopProvider(safety.EmergencyStopProviderFunc(func() (bool, string, error) {
		return false, "", nil
	})))
	src, _ := localFolderSource(t, "alice")
	repo := &manualSyncSubmitTestRepo{
		fakeSourceRepo: newFakeSourceRepo(src),
		jobs:           map[string]models.SourceSyncJob{}, durable: map[uuid.UUID]models.DurableJob{},
	}
	svc := NewService(repo, nil).(*service)
	svc.setManualOnlySyncWorkerReady(true)
	accepted, created, err := svc.SubmitManualSync("alice", src.ID, "manual-only-paused-local-request", ManualSyncRequest{})
	if err != nil || !created {
		t.Fatalf("submit = (%#v, %v, %v)", accepted, created, err)
	}
	history := onlyManualSyncTestRecord(t, repo)
	queue := newFakeJobRepo()
	durable := repo.durable[*history.DurableJobID]
	queue.jobs[durable.ID] = &durable
	allowed := false
	runner := newManualSourceRunner(queue, svc, func() bool { return allowed })
	if len(queue.jobs) != 1 || len(queue.byKind(JobKindScan)) != 0 {
		t.Fatal("manual runner registered scheduled work")
	}
	if processed, err := runner.RunOnce(context.Background()); err != nil || processed != 1 {
		t.Fatalf("paused pass = (%d, %v)", processed, err)
	}
	stored := queue.jobs[durable.ID]
	if stored.Status != models.DurableJobPending || stored.Attempts != 0 || len(repo.extractions) != 0 {
		t.Fatalf("paused job = %#v; extractions=%d, want pending/0/no extraction", stored, len(repo.extractions))
	}
	if view := manualSyncView(&history, stored); view.Status != "queued" || view.CompletedAt != nil {
		t.Fatalf("paused projection = %#v, want truthful nonterminal status", view)
	}
	// The same durable intent is resumed, not replaced, once policy allows it.
	allowed = true
	stored.RunAt = time.Now().UTC().Add(-time.Second)
	if processed, err := runner.RunOnce(context.Background()); err != nil || processed != 1 {
		t.Fatalf("resumed pass = (%d, %v)", processed, err)
	}
	completed := onlyManualSyncTestRecord(t, repo)
	if stored.Status != models.DurableJobSucceeded || completed.Status != "completed" || completed.CompletedAt == nil || len(repo.extractions) != 1 {
		t.Fatalf("local completion: durable=%#v history=%#v extractions=%d", stored, completed, len(repo.extractions))
	}
	if completed.ID != history.ID || len(queue.jobs) != 1 {
		t.Fatal("resume replaced the durable intent or scheduled additional work")
	}
}

func TestManualWorkerEmergencyStopAndMissingGateDeferWithoutExecution(t *testing.T) {
	for _, test := range []struct {
		name     string
		gate     func() bool
		provider safety.EmergencyStopProvider
	}{
		{"missing gate", nil, safety.EmergencyStopProviderFunc(func() (bool, string, error) { return false, "", nil })},
		{"stop engaged", func() bool { return true }, safety.EmergencyStopProviderFunc(func() (bool, string, error) { return true, "stop", nil })},
		{"stop unreadable", func() bool { return true }, safety.EmergencyStopProviderFunc(func() (bool, string, error) { return false, "", errors.New("unavailable") })},
		{"stop unconfigured", func() bool { return true }, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Cleanup(safety.SetEmergencyStopProvider(test.provider))
			queue := newFakeJobRepo()
			// Invalid payload would fail if the gate let execution reach decoding.
			job, err := queue.Enqueue(&models.DurableJob{Queue: "source", Kind: JobKindManualSync,
				Payload: "not-json", RunAt: time.Now().Add(-time.Second), MaxAttempts: syncMaxAttempts})
			if err != nil {
				t.Fatal(err)
			}
			runner := newManualSourceRunner(queue, NewService(newFakeSourceRepo(), nil), test.gate)
			if processed, err := runner.RunOnce(context.Background()); err != nil || processed != 1 {
				t.Fatalf("blocked pass = (%d, %v)", processed, err)
			}
			stored := queue.jobs[job.ID]
			if stored.Status != models.DurableJobPending || stored.Attempts != 0 || !strings.Contains(stored.LastError, "paused by safety policy") {
				t.Fatalf("blocked job = %#v, want deferred without attempt", stored)
			}
		})
	}
}

func TestManualWorkerUnavailableHTTPHasNoQueueWrites(t *testing.T) {
	gin.SetMode(gin.TestMode)
	src, _ := localFolderSource(t, "alice")
	repo := &manualSyncSubmitTestRepo{fakeSourceRepo: newFakeSourceRepo(src),
		jobs: map[string]models.SourceSyncJob{}, durable: map[uuid.UUID]models.DurableJob{}}
	svc := NewService(repo, nil).(*service)
	t.Setenv("SOURCE_SCHEDULER_ENABLED", "false")
	t.Setenv("SOURCE_MANUAL_WORKER_ENABLED", "false")
	StartScheduler(context.Background(), svc, func() bool { return false })
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set(identity.ContextSubjectKey, "alice") })
	router.POST("/sources/:id/sync-jobs", NewHandler(svc).SubmitManualSync)
	request := httptest.NewRequest(http.MethodPost, "/sources/"+src.ID.String()+"/sync-jobs", strings.NewReader(`{}`))
	request.Header.Set("Idempotency-Key", "manual-only-unavailable-http")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable || len(repo.jobs) != 0 || len(repo.durable) != 0 || len(repo.extractions) != 0 {
		t.Fatalf("disabled submit: status=%d body=%s history=%d queue=%d extractions=%d", response.Code, response.Body.String(), len(repo.jobs), len(repo.durable), len(repo.extractions))
	}
}

func TestManualWorkerSQLClaimsAndRecoversOnlyManualJobs(t *testing.T) {
	db, err := gorm.Open(postgres.New(postgres.Config{DSN: "host=127.0.0.1 user=unused dbname=unused", PreferSimpleProtocol: true}), &gorm.Config{
		DryRun: true, DisableAutomaticPing: true, SkipDefaultTransaction: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	claim := db.Raw(manualWorkerClaimSQL, models.DurableJobRunning, "manual-worker", now, "source", JobKindManualSync, models.DurableJobPending, now)
	for _, fragment := range []string{"queue = $4 AND kind = $5 AND status = $6", "LIMIT 1 FOR UPDATE SKIP LOCKED", "lease_generation = lease_generation + 1", "RETURNING *"} {
		if !strings.Contains(claim.Statement.SQL.String(), fragment) {
			t.Fatalf("manual claim SQL missing %q: %s", fragment, claim.Statement.SQL.String())
		}
	}
	if claim.Statement.Vars[3] != "source" || claim.Statement.Vars[4] != JobKindManualSync {
		t.Fatalf("manual claim bindings = %#v", claim.Statement.Vars)
	}
	recovery := manualWorkerRecovery(db, "source", now, durablejob.DefaultLease)
	if recovery.Error != nil {
		t.Fatal(recovery.Error)
	}
	sql := recovery.Statement.SQL.String()
	for _, fragment := range []string{"queue =", "kind =", "locked_at IS NULL OR locked_at <", "attempts + 1 >= max_attempts", "CASE WHEN", "attempts + 1"} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("manual recovery SQL missing %q: %s", fragment, sql)
		}
	}
	foundKind, foundQueue, foundCutoff := false, false, false
	for _, value := range recovery.Statement.Vars {
		foundKind = foundKind || value == JobKindManualSync
		foundQueue = foundQueue || value == "source"
		foundCutoff = foundCutoff || value == now.Add(-durablejob.DefaultLease)
	}
	if !foundKind || !foundQueue || !foundCutoff {
		t.Fatalf("recovery bindings = %#v", recovery.Statement.Vars)
	}
	if _, err := newManualWorkerRepository(db).ClaimDue("manual", "other", now, 10); err == nil {
		t.Fatal("manual worker accepted a different queue")
	}
}

func TestManualWorkerStartupIsIndependentAndOptIn(t *testing.T) {
	t.Setenv("SOURCE_SCHEDULER_ENABLED", "false")
	t.Setenv("SOURCE_SCHEDULER_DURABLE", "false")
	for _, value := range []string{"", "false", "invalid", "true"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("SOURCE_MANUAL_WORKER_ENABLED", value)
			calls := 0
			gate := func() bool { return false }
			err := startScheduler(
				context.Background(), nil,
				func(context.Context, Service, time.Duration, ...func() bool) error {
					t.Fatal("durable scheduler started while scheduling is disabled")
					return nil
				},
				func(context.Context, Service, time.Duration, ...func() bool) bool {
					t.Fatal("volatile scheduler started while scheduling is disabled")
					return false
				},
				func(ctx context.Context, svc Service, allowed ...func() bool) error {
					calls++
					if len(allowed) != 1 || allowed[0]() {
						t.Fatal("startup did not preserve the paused background gate")
					}
					return nil
				},
				gate,
			)
			if err != nil {
				t.Fatalf("startScheduler() error = %v", err)
			}
			want := 0
			if value == "true" {
				want = 1
			}
			if calls != want {
				t.Fatalf("manual starts with flag %q = %d, want %d", value, calls, want)
			}
		})
	}
}
