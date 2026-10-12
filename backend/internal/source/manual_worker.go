package source

import (
	"context"
	"errors"
	"log"
	"time"

	"automation-hub-backend/internal/durablejob"
	"automation-hub-backend/internal/infra"
	"automation-hub-backend/internal/lifecycle"
	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/safety"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

func (s *service) setManualOnlySyncWorkerReady(ready bool) {
	s.manualSyncWorkerMu.Lock()
	defer s.manualSyncWorkerMu.Unlock()
	s.manualOnlySyncWorkerReady.Store(ready)
}

// startDurableManualWorker never registers or produces scheduled source work.
// Missing policy wiring is an error, not permission to run explicit work.
func startDurableManualWorker(ctx context.Context, svc Service, allowed ...func() bool) error {
	if ctx == nil {
		return errors.New("manual source worker requires a lifecycle context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(allowed) == 0 || allowed[0] == nil {
		return errors.New("manual source worker requires the background safety gate")
	}
	s, ok := svc.(*service)
	if !ok || s == nil {
		return errors.New("manual source worker requires the durable source service")
	}
	s.setManualOnlySyncWorkerReady(false)
	db, err := infra.GetDefaultDB()
	if err != nil {
		return errors.New("manual source database is unavailable")
	}
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var probe []models.DurableJob
	if err := db.WithContext(probeCtx).Select("id").Limit(1).
		Where("queue = ? AND kind = ?", "source", JobKindManualSync).Find(&probe).Error; err != nil {
		return errors.New("manual source durable queue is unavailable")
	}
	runner := newManualSourceRunner(newManualWorkerRepository(db.WithContext(ctx)), svc, allowed[0])
	s.setManualOnlySyncWorkerReady(true)
	if !lifecycle.Go(ctx, "source-manual-worker", func() { runManualSourceWorker(ctx, s, runner, durablePollInterval()) }) {
		s.setManualOnlySyncWorkerReady(false)
		return context.Canceled
	}
	log.Printf("source manual worker started (manual jobs only; poll %s; background safety gate required)", durablePollInterval())
	return nil
}

func newManualSourceRunner(repo durablejob.Repository, svc Service, allowed func() bool) *durablejob.Runner {
	runner := durablejob.NewRunner(repo, durablejob.Options{Queue: "source", Batch: 1})
	gate := func() bool {
		return allowed != nil && allowed() && !safety.EvaluateEmergencyStopForExecution().Active
	}
	runner.Register(JobKindManualSync, manualSyncHandler(svc, gate))
	return runner
}

func runManualSourceWorker(ctx context.Context, s *service, runner *durablejob.Runner, interval time.Duration) {
	defer s.setManualOnlySyncWorkerReady(false)
	done := make(chan struct{})
	defer close(done)
	// Reject new work as soon as shutdown starts, even while a bounded sync
	// invocation is still unwinding. Already committed work remains durable.
	if !lifecycle.Go(ctx, "source-readiness-watcher", func() {
		select {
		case <-ctx.Done():
			s.setManualOnlySyncWorkerReady(false)
		case <-done:
		}
	}) {
		s.setManualOnlySyncWorkerReady(false)
	}
	runner.Start(ctx, interval)
}

// A kind-scoped claim/reaper is essential on the shared source queue: the
// generic runner would dead-letter kinds for which it has no handler. All
// other lease fencing, heartbeat and outcome writes reuse the durable runner.
type manualWorkerRepository struct {
	durablejob.Repository
	db *gorm.DB
}

func newManualWorkerRepository(db *gorm.DB) *manualWorkerRepository {
	return &manualWorkerRepository{Repository: durablejob.NewGormRepository(db), db: db}
}

func (r *manualWorkerRepository) MarkSucceededWithAttempts(id uuid.UUID, workerID string, generation int64, now time.Time, attempts int) (bool, error) {
	repo, ok := r.Repository.(durablejob.AttemptAwareSuccessRepository)
	if !ok {
		return false, errors.New("manual source queue cannot persist the successful attempt count")
	}
	return repo.MarkSucceededWithAttempts(id, workerID, generation, now, attempts)
}

const manualWorkerClaimSQL = `
	UPDATE durable_jobs
	SET status = ?, locked_by = ?, locked_at = ?, lease_generation = lease_generation + 1
	WHERE id IN (
		SELECT id FROM durable_jobs
		WHERE queue = ? AND kind = ? AND status = ? AND run_at <= ?
		ORDER BY run_at, id
		LIMIT 1 FOR UPDATE SKIP LOCKED
	)
	RETURNING *`

func (r *manualWorkerRepository) ClaimDue(workerID, queue string, now time.Time, limit int) ([]models.DurableJob, error) {
	if queue != "source" {
		return nil, errors.New("manual source worker cannot claim another queue")
	}
	var jobs []models.DurableJob
	ctx, cancel := context.WithTimeout(r.db.Statement.Context, 5*time.Second)
	defer cancel()
	err := r.db.WithContext(ctx).Raw(manualWorkerClaimSQL,
		models.DurableJobRunning, workerID, now, "source", JobKindManualSync, models.DurableJobPending, now,
	).Scan(&jobs).Error
	return jobs, err
}

func (r *manualWorkerRepository) ReapExpiredLeases(now time.Time, lease time.Duration) (int, error) {
	return r.ReapExpiredLeasesForQueue("source", now, lease)
}

func (r *manualWorkerRepository) ReapExpiredLeasesForQueue(queue string, now time.Time, lease time.Duration) (int, error) {
	if queue != "source" {
		return 0, errors.New("manual source worker cannot recover another queue")
	}
	ctx, cancel := context.WithTimeout(r.db.Statement.Context, 5*time.Second)
	defer cancel()
	result := manualWorkerRecovery(r.db.WithContext(ctx), queue, now, lease)
	return int(result.RowsAffected), result.Error
}

func manualWorkerRecovery(db *gorm.DB, queue string, now time.Time, lease time.Duration) *gorm.DB {
	// An expired invocation has an unknown outcome and consumes one attempt,
	// matching the durable repository's existing bounded recovery contract.
	return db.Model(&models.DurableJob{}).
		Where("queue = ? AND kind = ? AND status = ? AND (locked_at IS NULL OR locked_at < ?)",
			queue, JobKindManualSync, models.DurableJobRunning, now.Add(-lease)).
		Updates(map[string]any{
			"status":   gorm.Expr("CASE WHEN attempts + 1 >= max_attempts THEN ? ELSE ? END", models.DurableJobDead, models.DurableJobPending),
			"attempts": gorm.Expr("attempts + 1"),
			"last_error": gorm.Expr("CASE WHEN attempts + 1 >= max_attempts THEN ? ELSE ? END",
				"worker lease is missing or expired on the final allowed attempt; execution outcome is unknown",
				"worker lease is missing or expired; execution outcome is unknown"),
			"completed_at": gorm.Expr("CASE WHEN attempts + 1 >= max_attempts THEN ? ELSE completed_at END", now.UTC()),
			"locked_by":    "", "locked_at": nil,
		})
}
