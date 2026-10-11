package source

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"automation-hub-backend/internal/durablejob"
	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/pgtestguard"
	"automation-hub-backend/internal/safety"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const manualWorkerPostgresDatabase = "hai_source_manual_worker_test"
const manualWorkerPostgresDSNEnv = "HAI_SOURCE_MANUAL_WORKER_POSTGRES_TEST_DSN"

// Keep the gate before even constructing a connection pool. In particular,
// never fall back to the application's or another integration test's DSN.
func validateManualWorkerPostgresTarget(dsn, optIn string) error {
	if optIn != "true" {
		return errors.New("HAI_ALLOW_DESTRUCTIVE_DATABASE_TESTS must be literal true")
	}
	return pgtestguard.ValidateDedicatedPostgresTestDSN(dsn, manualWorkerPostgresDatabase)
}

func TestManualWorkerPostgresTargetGuard(t *testing.T) {
	valid := "host=127.0.0.1 user=postgres dbname=" + manualWorkerPostgresDatabase + " sslmode=disable"
	for _, tc := range []struct {
		name, dsn, optIn string
		valid            bool
	}{
		{"literal IPv4", valid, "true", true},
		{"literal IPv6", strings.ReplaceAll(valid, "127.0.0.1", "::1"), "true", true},
		{"missing opt-in", valid, "", false},
		{"false opt-in", valid, "false", false},
		{"nonliteral opt-in", valid, " TRUE ", false},
		{"missing DSN", "", "true", false},
		{"application database", strings.ReplaceAll(valid, manualWorkerPostgresDatabase, "automation"), "true", false},
		{"lookalike database", strings.ReplaceAll(valid, manualWorkerPostgresDatabase, manualWorkerPostgresDatabase+"_shadow"), "true", false},
		{"hostname", strings.ReplaceAll(valid, "127.0.0.1", "localhost"), "true", false},
		{"remote IP", strings.ReplaceAll(valid, "127.0.0.1", "192.0.2.10"), "true", false},
		{"remote fallback", strings.ReplaceAll(valid, "127.0.0.1", "127.0.0.1,192.0.2.10"), "true", false},
		{"unix socket", strings.ReplaceAll(valid, "127.0.0.1", "/tmp"), "true", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateManualWorkerPostgresTarget(tc.dsn, tc.optIn); (err == nil) != tc.valid {
				t.Fatalf("target guard error=%v, want valid=%v", err, tc.valid)
			}
		})
	}
}

type manualWorkerPostgresFixture struct {
	dsn, schema string
	db          *gorm.DB
}

func openManualWorkerPostgresPool(t *testing.T, dsn, schema string) *gorm.DB {
	t.Helper()
	if err := validateManualWorkerPostgresTarget(dsn, os.Getenv("HAI_ALLOW_DESTRUCTIVE_DATABASE_TESTS")); err != nil {
		t.Fatalf("refusing manual worker PostgreSQL connection: %v", err)
	}
	config, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if schema != "" {
		config.RuntimeParams["search_path"] = schema + ",public"
	}
	config.ConnectTimeout = 5 * time.Second
	config.RuntimeParams["statement_timeout"] = "10000"
	pool := stdlib.OpenDB(*config)
	pool.SetMaxOpenConns(16)
	pool.SetMaxIdleConns(16)
	t.Cleanup(func() { _ = pool.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open guarded PostgreSQL pool: %v", err)
	}
	var actual string
	if err := db.Raw("SELECT current_database()").Scan(&actual).Error; err != nil || actual != manualWorkerPostgresDatabase {
		t.Fatalf("connected database=%q, error=%v; refusing schema work", actual, err)
	}
	return db
}

func newManualWorkerPostgresFixture(t *testing.T) *manualWorkerPostgresFixture {
	t.Helper()
	dsn := pgtestguard.RequireDedicatedPostgresTestDSN(t, manualWorkerPostgresDSNEnv, manualWorkerPostgresDatabase)
	root := openManualWorkerPostgresPool(t, dsn, "")
	schema := "manual_worker_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if err := root.Exec("CREATE SCHEMA " + schema).Error; err != nil {
		t.Fatalf("create uniquely owned schema: %v", err)
	}
	t.Cleanup(func() {
		if err := root.Exec("DROP SCHEMA " + schema + " CASCADE").Error; err != nil {
			t.Errorf("cleanup owned schema: %v", err)
		}
	})
	db := openManualWorkerPostgresPool(t, dsn, schema)
	// This is a targeted model fixture, not a claim of full migration acceptance.
	if err := db.Exec(`CREATE EXTENSION IF NOT EXISTS "uuid-ossp" WITH SCHEMA public`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.DurableJob{}, &models.ConnectedSource{}, &models.SourceSyncJob{},
		&models.SourceRawItem{}, &models.SourceExtraction{}, &models.SourceIndexEntry{}, &models.SourceAuditLog{}); err != nil {
		t.Fatalf("migrate owned model fixture: %v", err)
	}
	t.Logf("real PostgreSQL dedicated database=%s owned schema=%s", manualWorkerPostgresDatabase, schema)
	return &manualWorkerPostgresFixture{dsn: dsn, schema: schema, db: db}
}

func (f *manualWorkerPostgresFixture) reload(t *testing.T) *gorm.DB {
	t.Helper()
	return openManualWorkerPostgresPool(t, f.dsn, f.schema)
}

func manualWorkerPostgresJob(t *testing.T, db *gorm.DB, queue, kind, status string, runAt time.Time) models.DurableJob {
	t.Helper()
	job := models.DurableJob{ID: uuid.New(), Queue: queue, Kind: kind, Status: status,
		Payload: `{}`, RunAt: runAt, MaxAttempts: 3}
	if err := db.Create(&job).Error; err != nil {
		t.Fatal(err)
	}
	return manualWorkerPostgresFind(t, db, job.ID)
}

func manualWorkerPostgresFind(t *testing.T, db *gorm.DB, id uuid.UUID) models.DurableJob {
	t.Helper()
	var row models.DurableJob
	if err := db.First(&row, "id = ?", id).Error; err != nil {
		t.Fatalf("reload durable row: %v", err)
	}
	return row
}

func manualWorkerPostgresUnchanged(t *testing.T, db *gorm.DB, rows []models.DurableJob) {
	t.Helper()
	for _, before := range rows {
		after := manualWorkerPostgresFind(t, db, before.ID)
		if !reflect.DeepEqual(before, after) {
			t.Errorf("foreign/noneligible job %s changed: before=%+v after=%+v", before.ID, before, after)
		}
	}
}

func manualWorkerPostgresForeignRows(t *testing.T, db *gorm.DB, now time.Time, status string) []models.DurableJob {
	t.Helper()
	var rows []models.DurableJob
	for _, pair := range [][2]string{{"source", JobKindSync}, {"source", JobKindScan},
		{"source", "workflow.synthetic"}, {"workflow", "workflow.synthetic"}, {"other", JobKindManualSync}} {
		row := manualWorkerPostgresJob(t, db, pair[0], pair[1], status, now.Add(-time.Hour))
		if status == models.DurableJobRunning {
			locked := now.Add(-2 * durablejob.DefaultLease)
			if err := db.Model(&models.DurableJob{}).Where("id = ?", row.ID).Updates(map[string]any{
				"locked_by": "foreign-worker", "locked_at": locked, "attempts": 2, "lease_generation": 7,
			}).Error; err != nil {
				t.Fatal(err)
			}
		}
		rows = append(rows, manualWorkerPostgresFind(t, db, row.ID))
	}
	return rows
}

func TestManualWorkerPostgresClaimIsolation(t *testing.T) {
	f := newManualWorkerPostgresFixture(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	foreign := manualWorkerPostgresForeignRows(t, f.db, now, models.DurableJobPending)
	future := manualWorkerPostgresJob(t, f.db, "source", JobKindManualSync, models.DurableJobPending, now.Add(time.Hour))
	terminal := manualWorkerPostgresJob(t, f.db, "source", JobKindManualSync, models.DurableJobDead, now.Add(-time.Hour))
	manual := manualWorkerPostgresJob(t, f.db, "source", JobKindManualSync, models.DurableJobPending, now)
	repo := newManualWorkerRepository(f.db)
	if _, err := repo.ClaimDue("manual", "other", now, 100); err == nil {
		t.Fatal("claim accepted another queue")
	}
	claimed, err := repo.ClaimDue("manual-owner", "source", now, 100)
	if err != nil || len(claimed) != 1 || claimed[0].ID != manual.ID {
		t.Fatalf("claim=%+v err=%v; want only due manual job", claimed, err)
	}
	stored := manualWorkerPostgresFind(t, f.reload(t), manual.ID)
	if stored.Status != models.DurableJobRunning || stored.LockedBy != "manual-owner" ||
		stored.LockedAt == nil || !stored.LockedAt.Equal(now) || stored.LeaseGeneration != 1 || stored.Attempts != 0 {
		t.Fatalf("persisted claim=%+v", stored)
	}
	if again, err := repo.ClaimDue("second-owner", "source", now, 100); err != nil || len(again) != 0 {
		t.Fatalf("second claim=%+v err=%v; foreign jobs must remain unclaimed", again, err)
	}
	manualWorkerPostgresUnchanged(t, f.reload(t), append(foreign, future, terminal))
}

func TestManualWorkerPostgresConcurrentSingleOwner(t *testing.T) {
	f := newManualWorkerPostgresFixture(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	foreign := manualWorkerPostgresForeignRows(t, f.db, now, models.DurableJobPending)
	manual := manualWorkerPostgresJob(t, f.db, "source", JobKindManualSync, models.DurableJobPending, now)
	const contenders = 12
	start := make(chan struct{})
	type result struct {
		worker string
		jobs   []models.DurableJob
		err    error
	}
	results := make(chan result, contenders)
	for i := 0; i < contenders; i++ {
		go func(i int) {
			<-start
			worker := fmt.Sprintf("manual-contender-%d", i)
			jobs, err := newManualWorkerRepository(f.db).ClaimDue(worker, "source", now, 1)
			results <- result{worker: worker, jobs: jobs, err: err}
		}(i)
	}
	close(start)
	winner, owners := "", 0
	for i := 0; i < contenders; i++ {
		r := <-results
		if r.err != nil {
			t.Errorf("concurrent claim: %v", r.err)
		}
		for _, job := range r.jobs {
			owners++
			winner = r.worker
			if job.ID != manual.ID || job.LockedBy != winner || job.LeaseGeneration != 1 {
				t.Errorf("unexpected claim: %+v", job)
			}
		}
	}
	if owners != 1 {
		t.Fatalf("%d concurrent owners, want exactly one", owners)
	}
	stored := manualWorkerPostgresFind(t, f.reload(t), manual.ID)
	if stored.LockedBy != winner || stored.LeaseGeneration != 1 || stored.Status != models.DurableJobRunning {
		t.Fatalf("persisted single owner=%+v winner=%s", stored, winner)
	}
	manualWorkerPostgresUnchanged(t, f.reload(t), foreign)
	t.Logf("%d concurrent claims, exactly one persisted owner", contenders)
}

func TestManualWorkerPostgresSkipLocked(t *testing.T) {
	f := newManualWorkerPostgresFixture(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	first := manualWorkerPostgresJob(t, f.db, "source", JobKindManualSync, models.DurableJobPending, now.Add(-time.Minute))
	second := manualWorkerPostgresJob(t, f.db, "source", JobKindManualSync, models.DurableJobPending, now)
	tx := f.db.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	defer tx.Rollback()
	var locked models.DurableJob
	if err := tx.Raw("SELECT * FROM durable_jobs WHERE id = ? FOR UPDATE", first.ID).Scan(&locked).Error; err != nil {
		t.Fatal(err)
	}
	claimed, err := newManualWorkerRepository(f.db).ClaimDue("unblocked-owner", "source", now, 20)
	if err != nil || len(claimed) != 1 || claimed[0].ID != second.ID {
		t.Fatalf("skip locked claim=%+v err=%v", claimed, err)
	}
	if err := tx.Rollback().Error; err != nil {
		t.Fatal(err)
	}
	claimed, err = newManualWorkerRepository(f.db).ClaimDue("released-owner", "source", now, 20)
	if err != nil || len(claimed) != 1 || claimed[0].ID != first.ID {
		t.Fatalf("released row claim=%+v err=%v", claimed, err)
	}
}

func TestManualWorkerPostgresReapIsolationAndAttemptLimit(t *testing.T) {
	f := newManualWorkerPostgresFixture(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	foreign := manualWorkerPostgresForeignRows(t, f.db, now, models.DurableJobRunning)
	foreign = append(foreign, manualWorkerPostgresForeignRows(t, f.db, now, models.DurableJobPending)...)
	cutoff := now.Add(-durablejob.DefaultLease)
	expired := cutoff.Add(-time.Microsecond)
	fresh := now
	var recovered []models.DurableJob
	for _, tc := range []struct {
		name     string
		lockedAt *time.Time
		attempts int
		recover  bool
	}{
		{"expired retry", &expired, 0, true}, {"missing retry", nil, 1, true},
		{"expired final", &expired, 2, true}, {"missing final", nil, 2, true},
		{"fresh", &fresh, 1, false}, {"exact cutoff", &cutoff, 1, false},
	} {
		row := manualWorkerPostgresJob(t, f.db, "source", JobKindManualSync, models.DurableJobRunning, now)
		if err := f.db.Model(&models.DurableJob{}).Where("id = ?", row.ID).Updates(map[string]any{
			"locked_at": tc.lockedAt, "locked_by": tc.name, "attempts": tc.attempts, "lease_generation": 8,
		}).Error; err != nil {
			t.Fatal(err)
		}
		row = manualWorkerPostgresFind(t, f.db, row.ID)
		if tc.recover {
			recovered = append(recovered, row)
		} else {
			foreign = append(foreign, row)
		}
	}
	for _, status := range []string{models.DurableJobPending, models.DurableJobSucceeded, models.DurableJobDead} {
		row := manualWorkerPostgresJob(t, f.db, "source", JobKindManualSync, status, now)
		foreign = append(foreign, manualWorkerPostgresFind(t, f.db, row.ID))
	}
	repo := newManualWorkerRepository(f.db)
	count, err := repo.ReapExpiredLeases(now, durablejob.DefaultLease)
	if err != nil || count != len(recovered) {
		t.Fatalf("reaped=%d err=%v, want %d", count, err, len(recovered))
	}
	for _, before := range recovered {
		after := manualWorkerPostgresFind(t, f.reload(t), before.ID)
		want := models.DurableJobPending
		if before.Attempts+1 >= before.MaxAttempts {
			want = models.DurableJobDead
		}
		if after.Status != want || after.Attempts != before.Attempts+1 || after.LockedAt != nil || after.LockedBy != "" ||
			after.LeaseGeneration != before.LeaseGeneration || !strings.Contains(after.LastError, "outcome is unknown") {
			t.Errorf("reaped row=%+v, want %s attempts=%d", after, want, before.Attempts+1)
		}
		if want == models.DurableJobDead && (after.CompletedAt == nil || !after.CompletedAt.Equal(now)) {
			t.Errorf("final reaped job lacks durable completion time: %+v", after)
		}
		if want == models.DurableJobPending && after.CompletedAt != nil {
			t.Errorf("retry was incorrectly marked complete: %+v", after)
		}
		owned, err := repo.MarkSucceededWithAttempts(before.ID, before.LockedBy, before.LeaseGeneration, now, 1)
		if err != nil || owned {
			t.Errorf("stale pre-reap success write owned=%v err=%v", owned, err)
		}
	}
	if count, err := repo.ReapExpiredLeases(now, durablejob.DefaultLease); err != nil || count != 0 {
		t.Fatalf("second reap=%d err=%v, want no double attempt consumption", count, err)
	}
	manualWorkerPostgresUnchanged(t, f.reload(t), foreign)
}

type manualWorkerPostgresFailureExecutor struct {
	Service
	calls atomic.Int32
}

func (e *manualWorkerPostgresFailureExecutor) RunManualSyncJob(context.Context, manualSyncPayload, uuid.UUID, int, int) error {
	e.calls.Add(1)
	return errors.New("synthetic retryable failure; no provider invoked")
}

func TestManualWorkerPostgresDurableRetryLimit(t *testing.T) {
	f := newManualWorkerPostgresFixture(t)
	t.Cleanup(safety.SetEmergencyStopProvider(safety.EmergencyStopProviderFunc(func() (bool, string, error) { return false, "", nil })))
	now := time.Now().UTC().Truncate(time.Microsecond)
	foreign := manualWorkerPostgresForeignRows(t, f.db, now, models.DurableJobPending)
	foreign = append(foreign, manualWorkerPostgresForeignRows(t, f.db, now, models.DurableJobRunning)...)
	manual := manualWorkerPostgresJob(t, f.db, "source", JobKindManualSync, models.DurableJobPending, now.Add(-time.Second))
	executor := &manualWorkerPostgresFailureExecutor{Service: NewService(&GormRepository{DB: f.db}, nil)}
	for attempt := 1; attempt <= manual.MaxAttempts; attempt++ {
		// A fresh repository/runner on every attempt exercises persisted state.
		runner := newManualSourceRunner(newManualWorkerRepository(f.reload(t)), executor, func() bool { return true })
		before := time.Now().UTC()
		if processed, err := runner.RunOnce(context.Background()); err != nil || processed != 1 {
			t.Fatalf("attempt %d processed=%d err=%v", attempt, processed, err)
		}
		stored := manualWorkerPostgresFind(t, f.reload(t), manual.ID)
		want := models.DurableJobPending
		if attempt == manual.MaxAttempts {
			want = models.DurableJobDead
		}
		if stored.Status != want || stored.Attempts != attempt || stored.LeaseGeneration != int64(attempt) ||
			stored.LockedBy != "" || stored.LockedAt != nil || !strings.Contains(stored.LastError, "synthetic retryable failure") {
			t.Fatalf("attempt %d durable result=%+v", attempt, stored)
		}
		if want == models.DurableJobPending {
			if !stored.RunAt.After(before) || stored.CompletedAt != nil {
				t.Fatalf("retry lacks future backoff or is terminal: %+v", stored)
			}
			if n, err := runner.RunOnce(context.Background()); err != nil || n != 0 {
				t.Fatalf("retry ran before backoff: %d %v", n, err)
			}
			if err := f.db.Model(&models.DurableJob{}).Where("id = ?", manual.ID).Update("run_at", now.Add(-time.Second)).Error; err != nil {
				t.Fatal(err)
			}
		} else if stored.CompletedAt == nil {
			t.Fatal("dead-letter missing persisted completion time")
		}
	}
	runner := newManualSourceRunner(newManualWorkerRepository(f.reload(t)), executor, func() bool { return true })
	if n, err := runner.RunOnce(context.Background()); err != nil || n != 0 || executor.calls.Load() != int32(manual.MaxAttempts) {
		t.Fatalf("exhausted retry processed=%d err=%v executor calls=%d", n, err, executor.calls.Load())
	}
	var count int64
	if err := f.db.Model(&models.DurableJob{}).Count(&count).Error; err != nil || count != int64(len(foreign)+1) {
		t.Fatalf("unexpected scheduled/descendant work: count=%d err=%v", count, err)
	}
	manualWorkerPostgresUnchanged(t, f.reload(t), foreign)
}

func TestManualWorkerPostgresExecutionReload(t *testing.T) {
	f := newManualWorkerPostgresFixture(t)
	t.Cleanup(safety.SetEmergencyStopProvider(safety.EmergencyStopProviderFunc(func() (bool, string, error) { return false, "", nil })))
	root := t.TempDir()
	t.Setenv("CONNECTED_SOURCE_LOCAL_ROOT", root)
	const content = "Synthetic manual-worker durable evidence. No external account or provider."
	if err := os.WriteFile(filepath.Join(root, "evidence.md"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	src := models.ConnectedSource{ID: uuid.New(), OwnerIdentity: "synthetic-" + uuid.NewString(),
		ConnectorKey: "local-folder", Name: "Synthetic owned manual source", Category: "local_folder",
		Enabled: true, LocalOnly: true, Status: "active", SyncFrequency: "manual", SyncTarget: ".", DefaultProjectKey: "synthetic-captured"}
	if err := f.db.Create(&src).Error; err != nil {
		t.Fatal(err)
	}
	svc := NewService(&GormRepository{DB: f.db}, nil).(*service)
	svc.setManualOnlySyncWorkerReady(true)
	accepted, created, err := svc.SubmitManualSync(src.OwnerIdentity, src.ID, "synthetic-manual-"+uuid.NewString(), ManualSyncRequest{})
	if err != nil || !created || accepted == nil || accepted.Status != "queued" {
		t.Fatalf("durable submission=%+v created=%v err=%v", accepted, created, err)
	}
	jobID := uuid.MustParse(accepted.ID)
	history, queued, err := (&GormRepository{DB: f.reload(t)}).FindManualSyncJobForOwner(src.OwnerIdentity, jobID)
	if err != nil || history.DurableJobID == nil || *history.DurableJobID != queued.ID {
		t.Fatalf("reload accepted outbox/history: %v", err)
	}
	foreign := manualWorkerPostgresForeignRows(t, f.db, time.Now().UTC(), models.DurableJobPending)
	allowed := false
	runner := newManualSourceRunner(newManualWorkerRepository(f.db), svc, func() bool { return allowed })
	if n, err := runner.RunOnce(context.Background()); err != nil || n != 1 {
		t.Fatalf("paused execution processed=%d err=%v", n, err)
	}
	paused := manualWorkerPostgresFind(t, f.reload(t), queued.ID)
	if paused.Status != models.DurableJobPending || paused.Attempts != 0 || paused.CompletedAt != nil {
		t.Fatalf("paused durable state=%+v", paused)
	}
	var extracted int64
	if err := f.db.Model(&models.SourceExtraction{}).Count(&extracted).Error; err != nil || extracted != 0 {
		t.Fatalf("paused worker extracted=%d err=%v", extracted, err)
	}
	if err := f.db.Model(&models.DurableJob{}).Where("id = ?", queued.ID).Update("run_at", time.Now().UTC().Add(-time.Second)).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.Model(&models.ConnectedSource{}).Where("id = ?", src.ID).Update("default_project_key", "synthetic-newer").Error; err != nil {
		t.Fatal(err)
	}
	allowed = true
	if n, err := runner.RunOnce(context.Background()); err != nil || n != 1 {
		t.Fatalf("real local execution processed=%d err=%v", n, err)
	}
	// Close the execution pool; query through a newly constructed service/pool.
	pool, err := f.db.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := pool.Close(); err != nil {
		t.Fatal(err)
	}
	reopened := f.reload(t)
	repo := &GormRepository{DB: reopened}
	completed, durable, err := repo.FindManualSyncJobForOwner(src.OwnerIdentity, jobID)
	if err != nil {
		t.Fatal(err)
	}
	if completed.Status != "completed" || completed.CompletedAt == nil || completed.ItemsSeen != 1 || completed.ItemsAdded != 1 || completed.ItemsFailed != 0 ||
		durable.Status != models.DurableJobSucceeded || durable.Attempts != 1 || durable.CompletedAt == nil || durable.LockedAt != nil || durable.LockedBy != "" {
		t.Fatalf("reloaded completion history=%+v durable=%+v", completed, durable)
	}
	view, err := NewService(repo, nil).(*service).ManualSyncJobForOwner(src.OwnerIdentity, jobID)
	if err != nil || view.Status != "completed" || view.Attempt != 1 {
		t.Fatalf("fresh service durable view=%+v err=%v", view, err)
	}
	if _, _, err := repo.FindManualSyncJobForOwner("foreign-synthetic-owner", jobID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("foreign owner could reload manual intent: %v", err)
	}
	var extractions []models.SourceExtraction
	if err := reopened.Where("source_id = ?", src.ID).Find(&extractions).Error; err != nil || len(extractions) != 1 {
		t.Fatalf("reload extraction count=%d err=%v", len(extractions), err)
	}
	if extractions[0].Text != content || extractions[0].RawItemID == uuid.Nil || extractions[0].SourceURI == "" || extractions[0].ProjectKey != "synthetic-captured" {
		t.Fatalf("persisted extraction lost content/provenance/captured project: %+v", extractions[0])
	}
	reloadedSource, err := repo.FindSource(src.ID)
	if err != nil || reloadedSource.Cursor == "" || reloadedSource.Cursor != completed.CursorAfter ||
		reloadedSource.LastSyncedAt == nil || reloadedSource.DefaultProjectKey != "synthetic-newer" {
		t.Fatalf("reloaded source/cursor=%+v err=%v", reloadedSource, err)
	}
	var jobs int64
	if err := reopened.Model(&models.DurableJob{}).Count(&jobs).Error; err != nil || jobs != int64(len(foreign)+1) {
		t.Fatalf("manual execution produced scheduled/descendant jobs: count=%d err=%v", jobs, err)
	}
	manualWorkerPostgresUnchanged(t, reopened, foreign)
}
