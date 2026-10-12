//go:build integration

package hostruntime

import (
	"context"
	"errors"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestGormRepositoryExpiryAndUncertainLeaseTransitions(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("HAI_TEST_DATABASE_DSN"))
	if dsn == "" || !strings.EqualFold(strings.TrimSpace(os.Getenv("HAI_ALLOW_HOST_RUNTIME_TEMP_TABLE_TESTS")), "true") {
		t.Skip("set HAI_TEST_DATABASE_DSN and HAI_ALLOW_HOST_RUNTIME_TEMP_TABLE_TESTS=true for the isolated PostgreSQL temp-table test")
	}
	config, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse test Postgres DSN: %v", err)
	}
	if !isLoopbackHost(config.Host) {
		t.Fatalf("refusing host-runtime temp-table test against non-loopback Postgres host %q", config.Host)
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open test Postgres: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get test Postgres pool: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	sqlDB.SetMaxOpenConns(1)

	if err := db.Exec(`
			CREATE TEMP TABLE host_runtime_jobs (
				id uuid PRIMARY KEY,
				owner_identity text NOT NULL,
				runtime_id text NOT NULL,
				task_id text NOT NULL UNIQUE,
				prompt text NOT NULL,
				workspace_key text NOT NULL,
				approval_digest text NOT NULL DEFAULT '',
				stop_revision bigint NOT NULL DEFAULT 0,
				status text NOT NULL,
				approval_expires_at timestamptz NOT NULL,
				review_required_at timestamptz,
				review_reason text NOT NULL DEFAULT '',
				worker_id text NOT NULL DEFAULT '',
				lease_digest text NOT NULL DEFAULT '',
				lease_expires timestamptz,
				execution_confirmed_at timestamptz,
				start_intent_id uuid,
				start_intent_at timestamptz,
				start_intent_worker_id text NOT NULL DEFAULT '',
				start_intent_lease_digest text NOT NULL DEFAULT '',
				start_intent_approval_digest text NOT NULL DEFAULT '',
				start_intent_stop_revision bigint,
				actual_start_ack_at timestamptz,
				cancel_requested_at timestamptz,
				output text NOT NULL DEFAULT '',
				error text NOT NULL DEFAULT '',
				exit_code integer,
				created_at timestamptz NOT NULL,
				updated_at timestamptz NOT NULL,
				completed_at timestamptz,
				reconciled_at timestamptz,
				CONSTRAINT chk_host_runtime_jobs_status CHECK (status IN ('pending', 'leased', 'completed', 'cancelled', 'expired', 'needs_review')),
				CONSTRAINT chk_host_runtime_jobs_intervention_state CHECK (
					status NOT IN ('expired', 'needs_review') OR (
						review_required_at IS NOT NULL AND length(btrim(review_reason)) > 0
						AND lease_digest = '' AND lease_expires IS NULL
					)
				)
				,
				CONSTRAINT chk_host_runtime_jobs_start_intent_binding CHECK (
					(start_intent_id IS NULL AND start_intent_at IS NULL AND start_intent_worker_id = ''
					 AND start_intent_lease_digest = '' AND start_intent_approval_digest = ''
					 AND start_intent_stop_revision IS NULL AND actual_start_ack_at IS NULL)
					OR (start_intent_id IS NOT NULL AND start_intent_at IS NOT NULL
					 AND start_intent_worker_id <> '' AND start_intent_lease_digest <> ''
					 AND start_intent_approval_digest <> '' AND start_intent_stop_revision IS NOT NULL)
				),
				CONSTRAINT chk_host_runtime_jobs_start_ack_requires_intent CHECK (
					actual_start_ack_at IS NULL OR start_intent_id IS NOT NULL
				)
			) ON COMMIT PRESERVE ROWS`).Error; err != nil {
		t.Fatalf("create isolated temporary host-runtime table: %v", err)
	}
	repository := &gormRepository{db: db}
	now := time.Now().UTC().Truncate(time.Microsecond)
	newJob := func(taskID string, approvalExpiry time.Time, createdAt time.Time) Job {
		return Job{
			ID: uuid.New(), OwnerIdentity: "owner:test", RuntimeID: "deepseek-harness-" + taskID, TaskID: taskID,
			Prompt: "approved prompt", WorkspaceKey: "test-workspace", Status: StatusPending,
			ApprovalExpiresAt: approvalExpiry, CreatedAt: createdAt, UpdatedAt: createdAt,
		}
	}
	create := func(job Job) {
		t.Helper()
		if _, err := repository.Create(context.Background(), job); err != nil {
			t.Fatalf("create host-runtime fixture: %v", err)
		}
	}
	load := func(id uuid.UUID) Job {
		t.Helper()
		var job Job
		if err := db.First(&job, "id = ?", id).Error; err != nil {
			t.Fatalf("load host-runtime fixture %s: %v", id, err)
		}
		return job
	}
	acknowledgeStart := func(job Job, workerID, leaseDigest string, at time.Time) {
		t.Helper()
		stored := load(job.ID)
		binding := StartIntentBinding{IntentID: uuid.New(), ApprovalDigest: stored.ApprovalDigest, StopRevision: stored.StopRevision}
		gate := StartGate{Available: true, CurrentRevision: stored.StopRevision}
		if _, err := repository.BeginStartIntent(context.Background(), workerID, stored.ID, leaseDigest, binding, gate, at); err != nil {
			t.Fatalf("begin start intent for %s: %v", stored.TaskID, err)
		}
		if err := repository.AcknowledgeActualStart(context.Background(), workerID, stored.ID, leaseDigest, binding, gate, at.Add(time.Microsecond)); err != nil {
			t.Fatalf("acknowledge start for %s: %v", stored.TaskID, err)
		}
	}
	stalePending := newJob("stale-pending", now.Add(24*time.Hour), now)
	stalePending.StopRevision = 10
	create(stalePending)
	if lease, err := repository.Lease("worker-after-stop", stalePending.RuntimeID, now, now.Add(time.Minute), "digest-after-stop", 11); err != nil || lease != nil {
		t.Fatalf("pre-stop pending approval was leased after stop clear: lease=%#v err=%v", lease, err)
	}
	if stored := load(stalePending.ID); stored.Status != StatusCancelled || stored.ReviewReason != reviewReasonStopRevision {
		t.Fatalf("pre-stop pending approval state = %#v; want audited cancellation", stored)
	}
	staleConfirmed := newJob("stale-confirmation", now.Add(24*time.Hour), now)
	staleConfirmed.StopRevision = 12
	create(staleConfirmed)
	staleLease, err := repository.Lease("worker-stale-confirm", staleConfirmed.RuntimeID, now, now.Add(time.Minute), "digest-stale-confirm", 12)
	if err != nil || staleLease == nil {
		t.Fatalf("stop-bound confirmation fixture was not leased: lease=%#v err=%v", staleLease, err)
	}
	if err := repository.ConfirmLeaseContext(context.Background(), "worker-stale-confirm", staleConfirmed.ID, "digest-stale-confirm", now, 13); !errors.Is(err, ErrStaleLease) {
		t.Fatalf("pre-stop lease confirmation after clear = %v; want stale lease", err)
	}
	if stored := load(staleConfirmed.ID); stored.Status != StatusCancelled || stored.ReviewReason != reviewReasonStopRevision {
		t.Fatalf("pre-stop unconfirmed lease state = %#v; want audited cancellation", stored)
	}

	fresh := newJob("fresh", now.Add(24*time.Hour), now.Add(-time.Minute))
	create(fresh)
	lease, err := repository.Lease("worker-fresh", fresh.RuntimeID, now, now.Add(time.Minute), "digest-fresh", 0)
	if err != nil || lease == nil || lease.ID != fresh.ID || lease.Status != StatusLeased {
		t.Fatalf("fresh pending job was not leased: lease=%#v err=%v", lease, err)
	}

	expired := newJob("expired-pending", now, now.Add(-48*time.Hour))
	create(expired)
	if lease, err := repository.Lease("worker-expired", expired.RuntimeID, now, now.Add(time.Minute), "digest-expired", 0); err != nil || lease != nil {
		t.Fatalf("expired pending approval was leased: lease=%#v err=%v", lease, err)
	}
	stored := load(expired.ID)
	if stored.Status != StatusExpired || stored.ReviewRequiredAt == nil || stored.LeaseDigest != "" {
		t.Fatalf("expired pending approval was not durably marked expired: %#v", stored)
	}

	uncertain := newJob("uncertain-lease", now.Add(24*time.Hour), now.Add(-2*time.Hour))
	create(uncertain)
	uncertainLease, err := repository.Lease("worker-uncertain", uncertain.RuntimeID, now, now.Add(time.Minute), "digest-uncertain", 0)
	if err != nil || uncertainLease == nil || uncertainLease.ID != uncertain.ID {
		t.Fatalf("fresh uncertain-outcome fixture was not initially leased: lease=%#v err=%v", uncertainLease, err)
	}
	if replay, err := repository.Lease("worker-replay", uncertain.RuntimeID, now.Add(2*time.Minute), now.Add(3*time.Minute), "digest-replay", 0); err != nil || replay != nil {
		t.Fatalf("expired leased work was re-leased: lease=%#v err=%v", replay, err)
	}
	stored = load(uncertain.ID)
	if stored.Status != StatusNeedsReview || stored.LeaseDigest != "" || stored.LeaseExpires != nil || stored.WorkerID != "worker-uncertain" {
		t.Fatalf("expired lease did not preserve its worker and invalidate its token for review: %#v", stored)
	}
	reviewJobs, err := repository.ListReviewRequiredUnreconciled(10)
	if err != nil || len(reviewJobs) != 2 {
		t.Fatalf("list unprojected review records = %#v, %v; want expired and needs-review records", reviewJobs, err)
	}
	for _, reviewJob := range reviewJobs {
		if reviewJob.ID != expired.ID && reviewJob.ID != uncertain.ID {
			t.Fatalf("review list included unrelated job: %#v", reviewJob)
		}
		if marked, err := repository.MarkReconciled(reviewJob.ID, now.Add(3*time.Minute)); err != nil || !marked {
			t.Fatalf("mark review record %s reconciled = %v, %v", reviewJob.ID, marked, err)
		}
	}
	reviewJobs, err = repository.ListReviewRequiredUnreconciled(10)
	if err != nil || len(reviewJobs) != 0 {
		t.Fatalf("review records after projection = %#v, %v; want empty", reviewJobs, err)
	}
	if load(expired.ID).Status != StatusExpired || load(uncertain.ID).Status != StatusNeedsReview {
		t.Fatal("marking review projections changed source state")
	}

	late := newJob("late-completion", now.Add(24*time.Hour), now.Add(-time.Minute))
	late.Output, late.Error = "preserved output", "preserved error"
	create(late)
	lateLease, err := repository.Lease("worker-late", late.RuntimeID, now.Add(3*time.Minute), now.Add(4*time.Minute), "digest-late", 0)
	if err != nil || lateLease == nil || lateLease.ID != late.ID {
		t.Fatalf("late-completion fixture was not leased: lease=%#v err=%v", lateLease, err)
	}
	if _, err := repository.Complete("worker-late", late.ID, "digest-late", Completion{ExitCode: 0, Output: "late output"}, now.Add(5*time.Minute)); !errors.Is(err, ErrStaleLease) {
		t.Fatalf("late completion error = %v, want ErrStaleLease", err)
	}
	stored = load(late.ID)
	if stored.Status != StatusNeedsReview || stored.LeaseDigest != "" || stored.Output != "preserved output" || stored.Error != "preserved error" || stored.CompletedAt != nil {
		t.Fatalf("stale completion overwrote evidence or failed to move the job to review: %#v", stored)
	}

	cancelPending := newJob("cancel-pending", now.Add(24*time.Hour), now)
	create(cancelPending)
	cancelled, revoked, err := repository.CancelTask(context.Background(), cancelPending.OwnerIdentity, cancelPending.TaskID, cancelPending.ID, now)
	if err != nil || !revoked || cancelled.Status != StatusCancelled {
		t.Fatalf("cancel pending job = %#v, %v, %v; want durable cancellation", cancelled, revoked, err)
	}
	if lease, err := repository.Lease("worker-cancelled", cancelPending.RuntimeID, now, now.Add(time.Minute), "digest-cancelled", 0); err != nil || lease != nil {
		t.Fatalf("cancelled Postgres row was leased: lease=%#v err=%v", lease, err)
	}

	cancelLeased := newJob("cancel-unconfirmed-lease", now.Add(24*time.Hour), now)
	create(cancelLeased)
	unconfirmedLease, err := repository.Lease("worker-cancel-race", cancelLeased.RuntimeID, now, now.Add(time.Minute), "digest-cancel-race", 0)
	if err != nil || unconfirmedLease == nil || unconfirmedLease.ID != cancelLeased.ID {
		t.Fatalf("unconfirmed cancellation fixture was not leased: lease=%#v err=%v", unconfirmedLease, err)
	}
	cancelled, revoked, err = repository.CancelTask(context.Background(), cancelLeased.OwnerIdentity, cancelLeased.TaskID, cancelLeased.ID, now)
	if err != nil || !revoked || cancelled.Status != StatusCancelled || cancelled.LeaseDigest != "" || cancelled.LeaseExpires != nil {
		t.Fatalf("cancel leased-before-confirm job = %#v, %v, %v; want token-invalidating cancellation", cancelled, revoked, err)
	}
	if err := repository.ConfirmLeaseContext(context.Background(), "worker-cancel-race", cancelLeased.ID, "digest-cancel-race", now, 0); !errors.Is(err, ErrStaleLease) {
		t.Fatalf("worker confirmation after cancellation = %v, want stale lease", err)
	}

	confirmed := newJob("cancel-confirmed", now.Add(24*time.Hour), now)
	create(confirmed)
	confirmedLeaseDigest := digestToken("digest-confirmed")
	confirmedLease, err := repository.Lease("worker-confirmed", confirmed.RuntimeID, now, now.Add(time.Minute), confirmedLeaseDigest, 0)
	if err != nil || confirmedLease == nil || confirmedLease.ID != confirmed.ID {
		t.Fatalf("confirmed job lease = %#v, %v", confirmedLease, err)
	}
	if err := repository.ConfirmLeaseContext(context.Background(), "worker-confirmed", confirmed.ID, confirmedLeaseDigest, now, 0); err != nil {
		t.Fatalf("confirm before cancellation: %v", err)
	}
	acknowledgeStart(confirmed, "worker-confirmed", confirmedLeaseDigest, now)
	requested, revoked, err := repository.CancelTask(context.Background(), confirmed.OwnerIdentity, confirmed.TaskID, confirmed.ID, now.Add(time.Second))
	if err != nil || revoked || requested.Status != StatusLeased || requested.ExecutionConfirmedAt == nil || requested.CancelRequestedAt == nil || requested.LeaseDigest == "" {
		t.Fatalf("cancellation after worker confirmation = %#v, %v, %v; want durable request with active lease", requested, revoked, err)
	}
	if err := repository.ConfirmLeaseContext(context.Background(), "worker-confirmed", confirmed.ID, confirmedLeaseDigest, now.Add(2*time.Second), 0); !errors.Is(err, ErrCancellationRequested) {
		t.Fatalf("worker heartbeat after stop = %v, want cancellation requested", err)
	}
	if _, err := repository.Complete("worker-confirmed", confirmed.ID, confirmedLeaseDigest, Completion{ExitCode: 0}, now.Add(3*time.Second)); !errors.Is(err, ErrCancellationRequested) {
		t.Fatalf("ordinary completion after stop = %v, want explicit acknowledgment required", err)
	}
	cancelled, err = repository.Complete("worker-confirmed", confirmed.ID, confirmedLeaseDigest, Completion{
		ExitCode: -1, Error: "tree empty", CancellationRequested: true, TerminationVerified: true,
	}, now.Add(4*time.Second))
	if err != nil || cancelled.Status != StatusCancelled || cancelled.CancelRequestedAt == nil || cancelled.LeaseDigest != "" {
		t.Fatalf("verified termination acknowledgment = %#v, %v", cancelled, err)
	}

	confirmEdge := newJob("confirmation-after-approval-expiry", now.Add(7*time.Minute), now)
	create(confirmEdge)
	confirmLease, err := repository.Lease("worker-confirm-edge", confirmEdge.RuntimeID, now.Add(6*time.Minute), now.Add(16*time.Minute), "digest-confirm-edge", 0)
	if err != nil || confirmLease == nil || confirmLease.ID != confirmEdge.ID {
		t.Fatalf("confirmation-expiry fixture was not leased: lease=%#v err=%v", confirmLease, err)
	}
	if err := repository.ConfirmLeaseContext(context.Background(), "worker-confirm-edge", confirmEdge.ID, "digest-confirm-edge", now.Add(8*time.Minute), 0); !errors.Is(err, ErrStaleLease) {
		t.Fatalf("confirmation after approval expiry = %v, want ErrStaleLease", err)
	}
	stored = load(confirmEdge.ID)
	if stored.Status != StatusNeedsReview || stored.LeaseDigest != "" || stored.LeaseExpires != nil {
		t.Fatalf("approval expiry between lease and launch was not quarantined: %#v", stored)
	}
}

func isLoopbackHost(host string) bool {
	host = strings.Trim(strings.TrimSpace(host), "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
