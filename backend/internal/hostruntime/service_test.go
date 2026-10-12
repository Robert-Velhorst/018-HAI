package hostruntime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"automation-hub-backend/internal/safety"

	"github.com/google/uuid"
)

func newTestService(repository Repository, options ...Option) *Service {
	service := NewService(repository, options...)
	service.executionIsolationAvailable = true
	return service
}

func TestStartIntentFreshRejectsFutureTimestamp(t *testing.T) {
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name string
		at   time.Time
		want bool
	}{
		{name: "current timestamp", at: now, want: true},
		{name: "inside freshness window", at: now.Add(-startIntentTimeout + time.Second), want: true},
		{name: "expired timestamp", at: now.Add(-startIntentTimeout), want: false},
		{name: "future timestamp", at: now.Add(time.Second), want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := startIntentFresh(Job{StartIntentAt: &tc.at}, now); got != tc.want {
				t.Fatalf("startIntentFresh(%s) = %v, want %v", tc.at, got, tc.want)
			}
		})
	}
}

func init() {
	safety.SetEmergencyStopProvider(&revisionedHostStop{})
}

func confirmAndAcknowledgeStart(t *testing.T, service *Service, leased *Lease) StartIntentBinding {
	t.Helper()
	if err := service.ConfirmLease(leased.WorkerID, leased.Job.ID, leased.Token); err != nil {
		t.Fatalf("ConfirmLease: %v", err)
	}
	binding := StartIntentBinding{IntentID: uuid.New(), ApprovalDigest: leased.ApprovalDigest, StopRevision: leased.StopRevision}
	if _, err := service.BeginStartIntent(context.Background(), leased.WorkerID, leased.Job.ID, leased.Token, binding); err != nil {
		t.Fatalf("BeginStartIntent: %v", err)
	}
	if err := service.AcknowledgeActualStart(context.Background(), leased.WorkerID, leased.Job.ID, leased.Token, binding); err != nil {
		t.Fatalf("AcknowledgeActualStart: %v", err)
	}
	return binding
}

type revisionedHostStop struct {
	engaged  bool
	revision uint64
}

func (s *revisionedHostStop) EmergencyStopStatus() (bool, string, error) {
	return s.engaged, "test stop", nil
}

func (s *revisionedHostStop) EmergencyStopRevision() (uint64, error) {
	return s.revision, nil
}

type contextBlockingCreateRepository struct {
	*memoryRepository
	entered chan struct{}
}

func (r *contextBlockingCreateRepository) Create(ctx context.Context, _ Job) (*Job, error) {
	close(r.entered)
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestProductionServiceBlocksExecutionWithoutVerifiedIsolation(t *testing.T) {
	repository := newMemoryRepository()
	service := NewService(repository)
	if available, reason := service.ExecutionAvailability(); available || !strings.Contains(reason, "operating-system sandbox") {
		t.Fatalf("execution availability = %v, %q; want blocked with sandbox reason", available, reason)
	}
	if _, err := service.Enqueue(ApprovedTask{
		OwnerIdentity: "robert@example.test", RuntimeID: "deepseek-harness", TaskID: "blocked-task",
		Prompt: "Inspect the approved workspace.", WorkspaceKey: "hai", Approved: true,
	}); !errors.Is(err, ErrExecutionIsolationUnavailable) {
		t.Fatalf("Enqueue error = %v; want isolation block", err)
	}
	if len(repository.jobs) != 0 {
		t.Fatalf("blocked enqueue persisted %d jobs", len(repository.jobs))
	}
	if lease, err := service.Lease("bridge-01", "deepseek-harness"); err != nil || lease != nil {
		t.Fatalf("Lease = %#v, %v; want no dispatched work", lease, err)
	}
	if err := service.ConfirmLease("bridge-01", uuid.New(), strings.Repeat("a", 32)); !errors.Is(err, ErrExecutionIsolationUnavailable) {
		t.Fatalf("ConfirmLease error = %v; want isolation block", err)
	}
}

func TestServiceLeasesOneApprovedJobAndRejectsStaleCompletion(t *testing.T) {
	now := time.Date(2026, 8, 24, 9, 0, 0, 0, time.UTC)
	repository := newMemoryRepository()
	service := newTestService(repository, WithClock(func() time.Time { return now }))

	job, err := service.Enqueue(ApprovedTask{
		OwnerIdentity: "robert@example.test",
		RuntimeID:     "deepseek-harness",
		TaskID:        "task-1",
		Prompt:        "Summarize the approved local workspace changes.",
		WorkspaceKey:  "hai",
		Approved:      true,
	})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if job.Status != StatusPending {
		t.Fatalf("new job status = %q, want %q", job.Status, StatusPending)
	}
	if want := now.Add(approvalValidityWindow); !job.ApprovalExpiresAt.Equal(want) {
		t.Fatalf("approval expiration = %s, want %s", job.ApprovalExpiresAt, want)
	}

	lease, err := service.Lease("bridge-01", "deepseek-harness")
	if err != nil {
		t.Fatalf("Lease: %v", err)
	}
	if lease == nil || lease.Job.ID != job.ID || lease.Token == "" {
		t.Fatalf("unexpected lease: %#v", lease)
	}
	if second, err := service.Lease("bridge-02", "deepseek-harness"); err != nil || second != nil {
		t.Fatalf("second lease = %#v, %v; want no job", second, err)
	}

	if _, err := service.Complete("bridge-01", job.ID, "wrong-token", Completion{ExitCode: 0, Output: "done"}); err == nil {
		t.Fatal("stale lease token completed host job")
	}
	confirmAndAcknowledgeStart(t, service, lease)
	completed, err := service.Complete("bridge-01", job.ID, lease.Token, Completion{ExitCode: 0, Output: "done"})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if completed.Status != StatusCompleted || completed.Output != "done" || completed.WorkerID != "bridge-01" {
		t.Fatalf("unexpected completed job: %#v", completed)
	}
}

func leasedStartProtocolFixture(t *testing.T, stop *revisionedHostStop) (*Service, *memoryRepository, *Lease) {
	t.Helper()
	if stop != nil {
		restore := safety.SetEmergencyStopProvider(stop)
		t.Cleanup(restore)
	}
	repository := newMemoryRepository()
	service := newTestService(repository)
	job, err := service.Enqueue(ApprovedTask{
		OwnerIdentity: "robert@example.test", RuntimeID: "deepseek-harness", TaskID: "start-protocol-fixture",
		Prompt: "Inspect the approved workspace.", WorkspaceKey: "hai", Approved: true,
	})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	lease, err := service.Lease("bridge-01", "deepseek-harness")
	if err != nil || lease == nil || lease.Job.ID != job.ID {
		t.Fatalf("Lease = %#v, %v", lease, err)
	}
	if err := service.ConfirmLease("bridge-01", job.ID, lease.Token); err != nil {
		t.Fatalf("ConfirmLease: %v", err)
	}
	return service, repository, lease
}

func TestStartIntentExactReplayIsIdempotentAndConflictingReplayIsQuarantined(t *testing.T) {
	service, repository, lease := leasedStartProtocolFixture(t, nil)
	binding := StartIntentBinding{IntentID: uuid.New(), ApprovalDigest: lease.ApprovalDigest, StopRevision: lease.StopRevision}
	first, err := service.BeginStartIntent(context.Background(), lease.WorkerID, lease.Job.ID, lease.Token, binding)
	if err != nil || first == nil {
		t.Fatalf("first start intent = %#v, %v", first, err)
	}
	replay, err := service.BeginStartIntent(context.Background(), lease.WorkerID, lease.Job.ID, lease.Token, binding)
	if err != nil || replay == nil || *replay != *first {
		t.Fatalf("exact start-intent replay = %#v, %v; want original durable receipt %#v", replay, err, first)
	}
	conflicting := binding
	conflicting.IntentID = uuid.New()
	if _, err := service.BeginStartIntent(context.Background(), lease.WorkerID, lease.Job.ID, lease.Token, conflicting); !errors.Is(err, ErrStartIntentReplay) {
		t.Fatalf("conflicting start-intent replay = %v, want ErrStartIntentReplay", err)
	}
	stored := repository.jobs[lease.Job.ID]
	if stored.Status != StatusNeedsReview || stored.ReviewReason != reviewReasonStartProtocol || stored.LeaseDigest != "" || stored.ActualStartAckAt != nil {
		t.Fatalf("conflicting replay was not quarantined: %#v", stored)
	}
	if err := service.AcknowledgeActualStart(context.Background(), lease.WorkerID, lease.Job.ID, lease.Token, binding); !errors.Is(err, ErrStartOutcomeReview) {
		t.Fatalf("acknowledgment after conflicting replay = %v, want review-only", err)
	}
	if next, err := service.Lease("bridge-02", "deepseek-harness"); err != nil || next != nil {
		t.Fatalf("quarantined start intent was re-leased: %#v, %v", next, err)
	}
}

func TestFutureStartIntentReplayIsQuarantined(t *testing.T) {
	service, repository, lease := leasedStartProtocolFixture(t, nil)
	binding := StartIntentBinding{IntentID: uuid.New(), ApprovalDigest: lease.ApprovalDigest, StopRevision: lease.StopRevision}
	if _, err := service.BeginStartIntent(context.Background(), lease.WorkerID, lease.Job.ID, lease.Token, binding); err != nil {
		t.Fatalf("BeginStartIntent: %v", err)
	}

	repository.mu.Lock()
	job := repository.jobs[lease.Job.ID]
	future := service.now().Add(time.Minute)
	job.StartIntentAt = &future
	repository.jobs[lease.Job.ID] = job
	repository.mu.Unlock()

	if _, err := service.BeginStartIntent(context.Background(), lease.WorkerID, lease.Job.ID, lease.Token, binding); !errors.Is(err, ErrStartOutcomeReview) {
		t.Fatalf("future start-intent replay = %v, want ErrStartOutcomeReview", err)
	}
	stored := repository.jobs[lease.Job.ID]
	if stored.Status != StatusNeedsReview || stored.ReviewReason != reviewReasonStartUnknown || stored.LeaseDigest != "" {
		t.Fatalf("future start intent was not quarantined: %#v", stored)
	}
}

func TestStartIntentAndCancellationRaceHasOnlyFailClosedDurableOutcomes(t *testing.T) {
	for _, intentFirst := range []bool{false, true} {
		service, repository, lease := leasedStartProtocolFixture(t, nil)
		binding := StartIntentBinding{IntentID: uuid.New(), ApprovalDigest: lease.ApprovalDigest, StopRevision: lease.StopRevision}
		start := make(chan struct{})
		intentReady := make(chan struct{})
		cancelReady := make(chan struct{})
		intentDone := make(chan error, 1)
		cancelDone := make(chan error, 1)
		revokedDone := make(chan bool, 1)
		go func() {
			<-start
			if !intentFirst {
				<-cancelReady
			}
			_, err := service.BeginStartIntent(context.Background(), lease.WorkerID, lease.Job.ID, lease.Token, binding)
			intentDone <- err
			close(intentReady)
		}()
		go func() {
			<-start
			if intentFirst {
				<-intentReady
			}
			_, revoked, err := service.CancelTask(context.Background(), lease.Job.OwnerIdentity, lease.Job.TaskID, lease.Job.ID)
			revokedDone <- revoked
			cancelDone <- err
			close(cancelReady)
		}()
		close(start)
		intentErr, cancelErr := <-intentDone, <-cancelDone
		revoked := <-revokedDone
		if cancelErr != nil {
			t.Fatalf("intentFirst=%v CancelTask: %v", intentFirst, cancelErr)
		}
		stored := repository.jobs[lease.Job.ID]
		if intentFirst {
			if intentErr != nil || revoked || stored.Status != StatusNeedsReview || stored.ReviewReason != reviewReasonStartUnknown || stored.LeaseDigest != "" {
				t.Fatalf("intent-first ordering was not quarantined: intent=%v revoked=%v row=%#v", intentErr, revoked, stored)
			}
			if err := service.AcknowledgeActualStart(context.Background(), lease.WorkerID, lease.Job.ID, lease.Token, binding); !errors.Is(err, ErrStartOutcomeReview) {
				t.Fatalf("late ack after intent-first stop = %v, want review-only", err)
			}
		} else if !errors.Is(intentErr, ErrStaleLease) || !revoked || stored.Status != StatusCancelled || stored.StartIntentID != nil || stored.LeaseDigest != "" {
			t.Fatalf("cancel-first ordering was not permanently revoked: intent=%v revoked=%v row=%#v", intentErr, revoked, stored)
		}
		if next, err := service.Lease("bridge-02", "deepseek-harness"); err != nil || next != nil {
			t.Fatalf("intent/cancel race result was re-leased: %#v, %v", next, err)
		}
	}
}

func TestStartAcknowledgmentWithStaleStopRevisionIsQuarantinedAndCannotComplete(t *testing.T) {
	stop := &revisionedHostStop{revision: 7}
	service, repository, lease := leasedStartProtocolFixture(t, stop)
	binding := StartIntentBinding{IntentID: uuid.New(), ApprovalDigest: lease.ApprovalDigest, StopRevision: lease.StopRevision}
	if _, err := service.BeginStartIntent(context.Background(), lease.WorkerID, lease.Job.ID, lease.Token, binding); err != nil {
		t.Fatalf("BeginStartIntent: %v", err)
	}
	stop.engaged = true
	stop.revision++
	if err := service.AcknowledgeActualStart(context.Background(), lease.WorkerID, lease.Job.ID, lease.Token, binding); !errors.Is(err, ErrStartOutcomeReview) {
		t.Fatalf("stale-stop acknowledgment = %v, want review-only", err)
	}
	if _, err := service.Complete(lease.WorkerID, lease.Job.ID, lease.Token, Completion{ExitCode: 0, Output: "must not be accepted"}); !errors.Is(err, ErrStaleLease) {
		t.Fatalf("completion after stale-stop quarantine = %v, want stale lease", err)
	}
	stored := repository.jobs[lease.Job.ID]
	if stored.Status != StatusNeedsReview || stored.ReviewReason != reviewReasonStartUnknown || stored.ActualStartAckAt != nil || stored.Output != "" || stored.CompletedAt != nil {
		t.Fatalf("stale-stop outcome was not quarantined without accepting completion: %#v", stored)
	}
}

func TestCompletionWithoutActualStartAcknowledgmentRequiresReview(t *testing.T) {
	service, repository, lease := leasedStartProtocolFixture(t, nil)
	if _, err := service.Complete(lease.WorkerID, lease.Job.ID, lease.Token, Completion{ExitCode: 0, Output: "unverified"}); !errors.Is(err, ErrStartOutcomeReview) {
		t.Fatalf("completion without start ack = %v, want ErrStartOutcomeReview", err)
	}
	stored := repository.jobs[lease.Job.ID]
	if stored.Status != StatusNeedsReview || stored.ReviewReason != reviewReasonStartProtocol || stored.Output != "" || stored.CompletedAt != nil {
		t.Fatalf("completion without acknowledgment was not rejected: %#v", stored)
	}
}

func TestServiceRejectsUnapprovedAndRedactsResult(t *testing.T) {
	service := newTestService(newMemoryRepository())
	if _, err := service.Enqueue(ApprovedTask{
		OwnerIdentity: "robert@example.test",
		RuntimeID:     "deepseek-harness",
		TaskID:        "task-1",
		Prompt:        "Inspect workspace.",
		WorkspaceKey:  "hai",
	}); err == nil {
		t.Fatal("unapproved job was accepted")
	}

	job, err := service.Enqueue(ApprovedTask{
		OwnerIdentity: "robert@example.test",
		RuntimeID:     "deepseek-harness",
		TaskID:        "task-2",
		Prompt:        "Inspect workspace.",
		WorkspaceKey:  "hai",
		Approved:      true,
	})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	lease, err := service.Lease("bridge-01", "deepseek-harness")
	if err != nil {
		t.Fatalf("Lease: %v", err)
	}
	confirmAndAcknowledgeStart(t, service, lease)
	completed, err := service.Complete("bridge-01", job.ID, lease.Token, Completion{
		ExitCode: 1,
		Output:   "Authorization: Bearer secret-value",
		Error:    strings.Repeat("x", 20_000),
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if strings.Contains(completed.Output, "secret-value") || len(completed.Error) > maxResultBytes {
		t.Fatalf("completion was not safely bounded/redacted: %#v", completed)
	}
}

func TestServiceCancellationBeforeLeaseMakesJobPermanentlyUnleaseable(t *testing.T) {
	now := time.Date(2026, 8, 24, 9, 0, 0, 0, time.UTC)
	repository := newMemoryRepository()
	service := newTestService(repository, WithClock(func() time.Time { return now }))
	job, err := service.Enqueue(ApprovedTask{
		OwnerIdentity: "robert@example.test", RuntimeID: "deepseek-harness", TaskID: "cancel-before-lease",
		Prompt: "Inspect the approved workspace.", WorkspaceKey: "hai", Approved: true,
	})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	cancelled, revoked, err := service.CancelTask(context.Background(), job.OwnerIdentity, job.TaskID, job.ID)
	if err != nil || !revoked || cancelled == nil || cancelled.Status != StatusCancelled {
		t.Fatalf("CancelTask = %#v, %v, %v; want durable cancellation", cancelled, revoked, err)
	}
	if cancelled.LeaseDigest != "" || cancelled.LeaseExpires != nil || !cancelled.UpdatedAt.Equal(now) {
		t.Fatalf("cancellation retained lease authority or lost its timestamp: %#v", cancelled)
	}
	if lease, err := service.Lease("bridge-01", "deepseek-harness"); err != nil || lease != nil {
		t.Fatalf("Lease after durable cancellation = %#v, %v; want unleaseable", lease, err)
	}
	if err := service.ConfirmLease("bridge-01", job.ID, "any-token"); !errors.Is(err, ErrStaleLease) {
		t.Fatalf("ConfirmLease after durable cancellation = %v, want stale lease", err)
	}
	// Repeating a stop is safe and confirms the already-durable terminal state.
	if again, confirmed, err := service.CancelTask(context.Background(), job.OwnerIdentity, job.TaskID, job.ID); err != nil || !confirmed || again.Status != StatusCancelled {
		t.Fatalf("idempotent CancelTask = %#v, %v, %v", again, confirmed, err)
	}
}

func TestServiceCancellationRevokesUnconfirmedLeaseButNeverConfirmedExecution(t *testing.T) {
	service := newTestService(newMemoryRepository())
	job, err := service.Enqueue(ApprovedTask{
		OwnerIdentity: "robert@example.test", RuntimeID: "deepseek-harness", TaskID: "cancel-unconfirmed-lease",
		Prompt: "Inspect the approved workspace.", WorkspaceKey: "hai", Approved: true,
	})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	lease, err := service.Lease("bridge-01", "deepseek-harness")
	if err != nil || lease == nil {
		t.Fatalf("Lease = %#v, %v", lease, err)
	}
	cancelled, revoked, err := service.CancelTask(context.Background(), job.OwnerIdentity, job.TaskID, job.ID)
	if err != nil || !revoked || cancelled.Status != StatusCancelled {
		t.Fatalf("CancelTask before confirmation = %#v, %v, %v; want revoked lease", cancelled, revoked, err)
	}
	if err := service.ConfirmLease("bridge-01", job.ID, lease.Token); !errors.Is(err, ErrStaleLease) {
		t.Fatalf("ConfirmLease after lease revocation = %v, want stale lease", err)
	}

	confirmedJob, err := service.Enqueue(ApprovedTask{
		OwnerIdentity: "robert@example.test", RuntimeID: "deepseek-harness", TaskID: "cancel-confirmed-lease",
		Prompt: "Inspect the approved workspace.", WorkspaceKey: "hai", Approved: true,
	})
	if err != nil {
		t.Fatalf("Enqueue confirmed job: %v", err)
	}
	confirmedLease, err := service.Lease("bridge-01", "deepseek-harness")
	if err != nil || confirmedLease == nil || confirmedLease.Job.ID != confirmedJob.ID {
		t.Fatalf("Lease confirmed job = %#v, %v", confirmedLease, err)
	}
	if err := service.ConfirmLease("bridge-01", confirmedJob.ID, confirmedLease.Token); err != nil {
		t.Fatalf("ConfirmLease: %v", err)
	}
	binding := StartIntentBinding{IntentID: uuid.New(), ApprovalDigest: confirmedLease.ApprovalDigest, StopRevision: confirmedLease.StopRevision}
	if _, err := service.BeginStartIntent(context.Background(), "bridge-01", confirmedJob.ID, confirmedLease.Token, binding); err != nil {
		t.Fatalf("BeginStartIntent: %v", err)
	}
	if err := service.AcknowledgeActualStart(context.Background(), "bridge-01", confirmedJob.ID, confirmedLease.Token, binding); err != nil {
		t.Fatalf("AcknowledgeActualStart: %v", err)
	}
	stored := service.repository.(*memoryRepository).jobs[confirmedJob.ID]
	if stored.ExecutionConfirmedAt == nil {
		t.Fatal("valid worker confirmation was not persisted")
	}
	unchanged, revoked, err := service.CancelTask(context.Background(), confirmedJob.OwnerIdentity, confirmedJob.TaskID, confirmedJob.ID)
	if err != nil || revoked || unchanged.Status != StatusLeased || unchanged.ExecutionConfirmedAt == nil || unchanged.CancelRequestedAt == nil || unchanged.LeaseDigest == "" {
		t.Fatalf("CancelTask after confirmation = %#v, %v, %v; want durable request and retained active lease", unchanged, revoked, err)
	}
	if err := service.ConfirmLease("bridge-01", confirmedJob.ID, confirmedLease.Token); !errors.Is(err, ErrCancellationRequested) {
		t.Fatalf("bridge heartbeat after Stop = %v, want cancellation request", err)
	}
	if _, err := service.Complete("bridge-01", confirmedJob.ID, confirmedLease.Token, Completion{ExitCode: 0}); !errors.Is(err, ErrCancellationRequested) {
		t.Fatalf("ordinary completion after Stop = %v, want cancellation acknowledgment required", err)
	}
	cancelled, err = service.Complete("bridge-01", confirmedJob.ID, confirmedLease.Token, Completion{
		ExitCode: -1, Error: "contained process tree stopped", CancellationRequested: true, TerminationVerified: true,
	})
	if err != nil || cancelled.Status != StatusCancelled || cancelled.CancelRequestedAt == nil || cancelled.CompletedAt == nil || cancelled.LeaseDigest != "" {
		t.Fatalf("verified cancellation acknowledgment = %#v, %v", cancelled, err)
	}

	uncertainJob, err := service.Enqueue(ApprovedTask{
		OwnerIdentity: "robert@example.test", RuntimeID: "deepseek-harness", TaskID: "cancel-unverified-tree",
		Prompt: "Inspect the approved workspace.", WorkspaceKey: "hai", Approved: true,
	})
	if err != nil {
		t.Fatalf("Enqueue uncertain job: %v", err)
	}
	uncertainLease, err := service.Lease("bridge-01", "deepseek-harness")
	if err != nil || uncertainLease == nil || uncertainLease.Job.ID != uncertainJob.ID {
		t.Fatalf("Lease uncertain job = %#v, %v", uncertainLease, err)
	}
	if err := service.ConfirmLease("bridge-01", uncertainJob.ID, uncertainLease.Token); err != nil {
		t.Fatalf("ConfirmLease uncertain job: %v", err)
	}
	confirmAndAcknowledgeStart(t, service, uncertainLease)
	if _, _, err := service.CancelTask(context.Background(), uncertainJob.OwnerIdentity, uncertainJob.TaskID, uncertainJob.ID); err != nil {
		t.Fatalf("CancelTask uncertain job: %v", err)
	}
	needsReview, err := service.Complete("bridge-01", uncertainJob.ID, uncertainLease.Token, Completion{
		ExitCode: -1, Error: "job termination uncertain", CancellationRequested: true,
	})
	if err != nil || needsReview.Status != StatusNeedsReview || needsReview.ReviewRequiredAt == nil || needsReview.ReviewReason != reviewReasonCancelUnverified || needsReview.CompletedAt != nil {
		t.Fatalf("uncertain cancellation acknowledgment = %#v, %v; want needs_review, not cancelled", needsReview, err)
	}
}

func TestServiceCancellationRequiresExactOwnerAndTaskBinding(t *testing.T) {
	service := newTestService(newMemoryRepository())
	job, err := service.Enqueue(ApprovedTask{
		OwnerIdentity: "robert@example.test", RuntimeID: "deepseek-harness", TaskID: "owner-bound-cancel",
		Prompt: "Inspect the approved workspace.", WorkspaceKey: "hai", Approved: true,
	})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if _, _, err := service.CancelTask(context.Background(), "other@example.test", job.TaskID, job.ID); !errors.Is(err, ErrJobNotFound) {
		t.Fatalf("cross-owner cancellation = %v, want not found", err)
	}
	if _, _, err := service.CancelTask(context.Background(), job.OwnerIdentity, "different-task", job.ID); !errors.Is(err, ErrJobNotFound) {
		t.Fatalf("cross-task cancellation = %v, want not found", err)
	}
	if lease, err := service.Lease("bridge-01", "deepseek-harness"); err != nil || lease == nil || lease.Job.ID != job.ID {
		t.Fatalf("wrong-owner attempts changed the job: lease=%#v err=%v", lease, err)
	}
}

func TestServiceCancelAndConfirmRaceHasOnlySafeDurableOutcomes(t *testing.T) {
	for attempt := 0; attempt < 40; attempt++ {
		repository := newMemoryRepository()
		service := newTestService(repository)
		job, err := service.Enqueue(ApprovedTask{
			OwnerIdentity: "robert@example.test", RuntimeID: "deepseek-harness", TaskID: fmt.Sprintf("cancel-confirm-race-%d", attempt),
			Prompt: "Inspect the approved workspace.", WorkspaceKey: "hai", Approved: true,
		})
		if err != nil {
			t.Fatalf("attempt %d Enqueue: %v", attempt, err)
		}
		lease, err := service.Lease("bridge-01", "deepseek-harness")
		if err != nil || lease == nil || lease.Job.ID != job.ID {
			t.Fatalf("attempt %d Lease = %#v, %v", attempt, lease, err)
		}

		start := make(chan struct{})
		type cancelOutcome struct {
			job     *Job
			revoked bool
			err     error
		}
		cancelDone := make(chan cancelOutcome, 1)
		confirmDone := make(chan error, 1)
		var workers sync.WaitGroup
		workers.Add(2)
		go func() {
			defer workers.Done()
			<-start
			cancelled, revoked, cancelErr := service.CancelTask(context.Background(), job.OwnerIdentity, job.TaskID, job.ID)
			cancelDone <- cancelOutcome{job: cancelled, revoked: revoked, err: cancelErr}
		}()
		go func() {
			defer workers.Done()
			<-start
			confirmDone <- service.ConfirmLease("bridge-01", job.ID, lease.Token)
		}()
		close(start)
		workers.Wait()
		cancelled := <-cancelDone
		confirmErr := <-confirmDone
		stored := repository.jobs[job.ID]
		if cancelled.err != nil {
			t.Fatalf("attempt %d cancellation error: %v", attempt, cancelled.err)
		}
		if stored.Status != StatusCancelled || !cancelled.revoked || stored.LeaseDigest != "" || stored.LeaseExpires != nil || stored.StartIntentID != nil {
			t.Fatalf("attempt %d pre-intent stop did not durably revoke the job: cancel=%#v confirm=%v row=%#v", attempt, cancelled, confirmErr, stored)
		}
		if confirmErr != nil && !errors.Is(confirmErr, ErrStaleLease) {
			t.Fatalf("attempt %d confirmation returned unexpected error after stop race: %v", attempt, confirmErr)
		}
	}
}

func TestServiceCancelAndCompleteRaceRequiresVerifiedStopAcknowledgment(t *testing.T) {
	for attempt := 0; attempt < 40; attempt++ {
		repository := newMemoryRepository()
		service := newTestService(repository)
		job, err := service.Enqueue(ApprovedTask{
			OwnerIdentity: "robert@example.test", RuntimeID: "deepseek-harness", TaskID: fmt.Sprintf("cancel-complete-race-%d", attempt),
			Prompt: "Inspect the approved workspace.", WorkspaceKey: "hai", Approved: true,
		})
		if err != nil {
			t.Fatalf("attempt %d Enqueue: %v", attempt, err)
		}
		lease, err := service.Lease("bridge-01", "deepseek-harness")
		if err != nil || lease == nil {
			t.Fatalf("attempt %d Lease = %#v, %v", attempt, lease, err)
		}
		confirmAndAcknowledgeStart(t, service, lease)

		start := make(chan struct{})
		cancelDone := make(chan error, 1)
		completeDone := make(chan error, 1)
		go func() {
			<-start
			_, _, cancelErr := service.CancelTask(context.Background(), job.OwnerIdentity, job.TaskID, job.ID)
			cancelDone <- cancelErr
		}()
		go func() {
			<-start
			_, completeErr := service.Complete("bridge-01", job.ID, lease.Token, Completion{ExitCode: 0, Output: "finished"})
			completeDone <- completeErr
		}()
		close(start)
		cancelErr, completeErr := <-cancelDone, <-completeDone
		if cancelErr != nil {
			t.Fatalf("attempt %d CancelTask: %v", attempt, cancelErr)
		}
		repository.mu.Lock()
		stored := repository.jobs[job.ID]
		repository.mu.Unlock()
		switch stored.Status {
		case StatusCompleted:
			if completeErr != nil || stored.CancelRequestedAt != nil || stored.ExitCode == nil || *stored.ExitCode != 0 {
				t.Fatalf("attempt %d completion won inconsistently: complete=%v job=%#v", attempt, completeErr, stored)
			}
		case StatusLeased:
			if !errors.Is(completeErr, ErrCancellationRequested) || stored.CancelRequestedAt == nil || stored.LeaseDigest == "" {
				t.Fatalf("attempt %d stop won without preserving active lease and rejecting ordinary completion: complete=%v job=%#v", attempt, completeErr, stored)
			}
			cancelled, err := service.Complete("bridge-01", job.ID, lease.Token, Completion{
				ExitCode: -1, Error: "verified process-tree stop", CancellationRequested: true, TerminationVerified: true,
			})
			if err != nil || cancelled.Status != StatusCancelled {
				t.Fatalf("attempt %d verified stop acknowledgment = %#v, %v", attempt, cancelled, err)
			}
		default:
			t.Fatalf("attempt %d race ended in unexpected status %q: %#v", attempt, stored.Status, stored)
		}
	}
}

func TestServiceExpiredLeaseMovesToReviewAndIsNeverReclaimed(t *testing.T) {
	now := time.Date(2026, 8, 24, 9, 0, 0, 0, time.UTC)
	repository := newMemoryRepository()
	service := newTestService(
		repository,
		WithClock(func() time.Time { return now }),
		WithLeaseDuration(5*time.Minute),
	)
	job, err := service.Enqueue(ApprovedTask{
		OwnerIdentity: "robert@example.test", RuntimeID: "deepseek-harness", TaskID: "task-reclaimed",
		Prompt: "Inspect the approved workspace.", WorkspaceKey: "hai", Approved: true,
	})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	first, err := service.Lease("bridge-old", "deepseek-harness")
	if err != nil || first == nil {
		t.Fatalf("first lease = %#v, %v", first, err)
	}
	now = now.Add(6 * time.Minute)
	second, err := service.Lease("bridge-new", "deepseek-harness")
	if err != nil || second != nil {
		t.Fatalf("lease after unknown prior execution = %#v, %v; want no replay", second, err)
	}
	if _, err := service.Complete("bridge-old", job.ID, first.Token, Completion{ExitCode: 0}); err != ErrStaleLease {
		t.Fatalf("old lease completion error = %v, want %v", err, ErrStaleLease)
	}
	stored := repository.jobs[job.ID]
	if stored.Status != StatusNeedsReview || stored.LeaseDigest != "" || stored.LeaseExpires != nil {
		t.Fatalf("expired lease was not moved to a token-invalidated review state: %#v", stored)
	}
	if stored.ReviewRequiredAt == nil || stored.ReviewReason != reviewReasonLeaseExpired || stored.WorkerID != "bridge-old" {
		t.Fatalf("lease intervention evidence or original worker was not preserved: %#v", stored)
	}
	if stored.TaskID != job.TaskID || stored.Prompt != job.Prompt || stored.ApprovalExpiresAt != job.ApprovalExpiresAt || !stored.CreatedAt.Equal(job.CreatedAt) {
		t.Fatalf("job/audit fields changed during lease intervention: %#v", stored)
	}
}

func TestServiceExpiresQueuedApprovalBeforeItCanBeLeased(t *testing.T) {
	now := time.Date(2026, 8, 24, 9, 0, 0, 0, time.UTC)
	repository := newMemoryRepository()
	service := newTestService(repository, WithClock(func() time.Time { return now }))
	job, err := service.Enqueue(ApprovedTask{
		OwnerIdentity: "robert@example.test", RuntimeID: "deepseek-harness", TaskID: "task-expired-approval",
		Prompt: "Inspect the approved workspace.", WorkspaceKey: "hai", Approved: true,
	})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	now = job.ApprovalExpiresAt
	lease, err := service.Lease("bridge-01", "deepseek-harness")
	if err != nil || lease != nil {
		t.Fatalf("Lease after approval expiry = %#v, %v; want no lease", lease, err)
	}
	stored := repository.jobs[job.ID]
	if stored.Status != StatusExpired || stored.ReviewRequiredAt == nil || stored.ReviewReason != reviewReasonApprovalExpired {
		t.Fatalf("expired queued approval lacks explicit intervention state: %#v", stored)
	}
	if stored.LeaseDigest != "" || stored.LeaseExpires != nil || !stored.CreatedAt.Equal(job.CreatedAt) || !stored.ApprovalExpiresAt.Equal(job.ApprovalExpiresAt) {
		t.Fatalf("expiration changed audit fields or retained a lease token: %#v", stored)
	}
}

func TestReviewRequiredUnreconciledIncludesOnlyUnprojectedReviewRecords(t *testing.T) {
	now := time.Date(2026, 8, 24, 9, 0, 0, 0, time.UTC)
	repository := newMemoryRepository()
	service := newTestService(repository, WithClock(func() time.Time { return now }))
	expired := Job{ID: uuid.New(), Status: StatusExpired, ReviewRequiredAt: timePointer(now.Add(-time.Minute))}
	needsReview := Job{ID: uuid.New(), Status: StatusNeedsReview, ReviewRequiredAt: timePointer(now)}
	completed := Job{ID: uuid.New(), Status: StatusCompleted, ReviewRequiredAt: timePointer(now)}
	missingReviewTime := Job{ID: uuid.New(), Status: StatusExpired}
	alreadyProjected := Job{ID: uuid.New(), Status: StatusNeedsReview, ReviewRequiredAt: timePointer(now), ReconciledAt: timePointer(now)}
	for _, job := range []Job{expired, needsReview, completed, missingReviewTime, alreadyProjected} {
		repository.jobs[job.ID] = job
	}

	jobs, err := service.ReviewRequiredUnreconciled(10)
	if err != nil || len(jobs) != 2 || jobs[0].ID != expired.ID || jobs[1].ID != needsReview.ID {
		t.Fatalf("ReviewRequiredUnreconciled = %#v, %v; want the two ordered review records", jobs, err)
	}
	if marked, err := service.MarkReconciled(expired.ID); err != nil || !marked {
		t.Fatalf("MarkReconciled(review) = %v, %v; want marked", marked, err)
	}
	if marked, err := service.MarkReconciled(expired.ID); err != nil || marked {
		t.Fatalf("repeated MarkReconciled(review) = %v, %v; want unchanged", marked, err)
	}
	if _, exists := repository.jobs[expired.ID]; !exists {
		t.Fatal("review projection removed the original host job")
	}
	jobs, err = service.ReviewRequiredUnreconciled(10)
	if err != nil || len(jobs) != 1 || jobs[0].ID != needsReview.ID {
		t.Fatalf("review list after marking = %#v, %v; want only the remaining unprojected record", jobs, err)
	}
}

func TestServiceDoesNotStartAJobAfterApprovalExpiresBetweenLeaseAndConfirmation(t *testing.T) {
	now := time.Date(2026, 8, 24, 9, 0, 0, 0, time.UTC)
	repository := newMemoryRepository()
	service := newTestService(repository, WithClock(func() time.Time { return now }), WithLeaseDuration(5*time.Minute))
	job, err := service.Enqueue(ApprovedTask{
		OwnerIdentity: "robert@example.test", RuntimeID: "deepseek-harness", TaskID: "task-expired-before-confirm",
		Prompt: "Inspect the approved workspace.", WorkspaceKey: "hai", Approved: true,
	})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	lease, err := service.Lease("bridge-01", "deepseek-harness")
	if err != nil || lease == nil {
		t.Fatalf("Lease = %#v, %v", lease, err)
	}
	queued := repository.jobs[job.ID]
	queued.ApprovalExpiresAt = now.Add(time.Minute)
	repository.jobs[job.ID] = queued
	now = now.Add(2 * time.Minute)
	if err := service.ConfirmLease("bridge-01", job.ID, lease.Token); !errors.Is(err, ErrStaleLease) {
		t.Fatalf("ConfirmLease after approval expiry = %v, want ErrStaleLease", err)
	}
	stored := repository.jobs[job.ID]
	if stored.Status != StatusNeedsReview || stored.LeaseDigest != "" || stored.LeaseExpires != nil {
		t.Fatalf("expired approval was not quarantined before host execution: %#v", stored)
	}
	if next, err := service.Lease("bridge-02", "deepseek-harness"); err != nil || next != nil {
		t.Fatalf("expired approval was replayed after failed confirmation: %#v, %v", next, err)
	}
}

func TestServiceStaleCompletionMovesLeaseToReviewAndCannotBeReplayed(t *testing.T) {
	now := time.Date(2026, 8, 24, 9, 0, 0, 0, time.UTC)
	repository := newMemoryRepository()
	service := newTestService(repository, WithClock(func() time.Time { return now }), WithLeaseDuration(5*time.Minute))
	job, err := service.Enqueue(ApprovedTask{
		OwnerIdentity: "robert@example.test", RuntimeID: "deepseek-harness", TaskID: "task-stale-completion",
		Prompt: "Inspect the approved workspace.", WorkspaceKey: "hai", Approved: true,
	})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	lease, err := service.Lease("bridge-01", "deepseek-harness")
	if err != nil || lease == nil {
		t.Fatalf("Lease = %#v, %v", lease, err)
	}
	confirmAndAcknowledgeStart(t, service, lease)
	now = now.Add(6 * time.Minute)
	if _, err := service.Complete("bridge-01", job.ID, lease.Token, Completion{ExitCode: 0, Output: "late result"}); !errors.Is(err, ErrStaleLease) {
		t.Fatalf("late completion error = %v, want ErrStaleLease", err)
	}
	stored := repository.jobs[job.ID]
	if stored.Status != StatusNeedsReview || stored.LeaseDigest != "" || stored.LeaseExpires != nil || stored.ReviewRequiredAt == nil {
		t.Fatalf("stale completion did not commit review transition and token invalidation: %#v", stored)
	}
	if stored.Output == "late result" || stored.CompletedAt != nil {
		t.Fatalf("late completion was accepted after lease expiry: %#v", stored)
	}
	if replay, err := service.Lease("bridge-02", "deepseek-harness"); err != nil || replay != nil {
		t.Fatalf("expired job replay after stale completion = %#v, %v", replay, err)
	}
}

func TestServiceEmergencyStopPreventsQueuedJobLease(t *testing.T) {
	service := newTestService(newMemoryRepository())
	if _, err := service.Enqueue(ApprovedTask{
		OwnerIdentity: "robert@example.test", RuntimeID: "deepseek-harness", TaskID: "task-stopped",
		Prompt: "Inspect the approved workspace.", WorkspaceKey: "hai", Approved: true,
	}); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	t.Setenv("HAI_EMERGENCY_STOP", "true")
	if lease, err := service.Lease("bridge-01", "deepseek-harness"); !errors.Is(err, ErrEmergencyStopped) || lease != nil {
		t.Fatalf("Lease while stopped = %#v, %v; want no lease and emergency-stop error", lease, err)
	}
	t.Setenv("HAI_EMERGENCY_STOP", "false")
	if lease, err := service.Lease("bridge-01", "deepseek-harness"); err != nil || lease == nil {
		t.Fatalf("Lease after stop cleared = %#v, %v; want queued job", lease, err)
	}
}

func TestStopClearInvalidatesQueuedAndLeasedUnstartedApprovals(t *testing.T) {
	stop := &revisionedHostStop{revision: 1}
	restore := safety.SetEmergencyStopProvider(stop)
	defer restore()
	repository := newMemoryRepository()
	service := newTestService(repository)

	queued, err := service.Enqueue(ApprovedTask{
		OwnerIdentity: "robert@example.test", RuntimeID: "deepseek-harness", TaskID: "queued-before-stop",
		Prompt: "Inspect the approved workspace.", WorkspaceKey: "hai", Approved: true,
	})
	if err != nil {
		t.Fatalf("enqueue pending job: %v", err)
	}
	stop.engaged, stop.revision = true, 2
	if lease, err := service.Lease("bridge-01", "deepseek-harness"); !errors.Is(err, ErrEmergencyStopped) || lease != nil {
		t.Fatalf("lease during stop = %#v, %v; want fail-closed stop", lease, err)
	}
	stop.engaged, stop.revision = false, 3
	if lease, err := service.Lease("bridge-01", "deepseek-harness"); err != nil || lease != nil {
		t.Fatalf("pre-stop queued approval after clear = %#v, %v; old approval must not start", lease, err)
	}
	if got := repository.jobs[queued.ID]; got.Status != StatusCancelled || got.ReviewReason != reviewReasonStopRevision {
		t.Fatalf("old pending job = %#v; want durable cancellation with stop-cycle reason", got)
	}

	leasedJob, err := service.Enqueue(ApprovedTask{
		OwnerIdentity: "robert@example.test", RuntimeID: "deepseek-harness", TaskID: "leased-before-stop",
		Prompt: "Inspect the approved workspace.", WorkspaceKey: "hai", Approved: true,
	})
	if err != nil {
		t.Fatalf("enqueue leased fixture: %v", err)
	}
	lease, err := service.Lease("bridge-01", "deepseek-harness")
	if err != nil || lease == nil || lease.Job.ID != leasedJob.ID {
		t.Fatalf("lease fixture = %#v, %v", lease, err)
	}
	stop.engaged, stop.revision = true, 4
	stop.engaged, stop.revision = false, 5
	if err := service.ConfirmLease("bridge-01", leasedJob.ID, lease.Token); !errors.Is(err, ErrStaleLease) {
		t.Fatalf("confirm pre-stop lease after clear = %v; want stale approval", err)
	}
	if got := repository.jobs[leasedJob.ID]; got.Status != StatusCancelled || got.ReviewReason != reviewReasonStopRevision {
		t.Fatalf("old unconfirmed lease = %#v; want durable cancellation with stop-cycle reason", got)
	}
}

func TestEnqueueContextBoundsRepositoryAdmission(t *testing.T) {
	repository := &contextBlockingCreateRepository{memoryRepository: newMemoryRepository(), entered: make(chan struct{})}
	service := newTestService(repository)
	ctx, cancel := context.WithTimeout(context.Background(), 75*time.Millisecond)
	defer cancel()
	_, err := service.EnqueueContext(ctx, ApprovedTask{
		OwnerIdentity: "robert@example.test", RuntimeID: "deepseek-harness", TaskID: "stalled-enqueue",
		Prompt: "Inspect the approved workspace.", WorkspaceKey: "hai", Approved: true,
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("EnqueueContext error = %v; want caller deadline", err)
	}
	select {
	case <-repository.entered:
	default:
		t.Fatal("repository did not receive the bounded enqueue attempt")
	}
}

func TestServiceFailsClosedBeforeEnqueueWhenSafetyControlIsUnavailable(t *testing.T) {
	t.Setenv("HAI_EMERGENCY_STOP", "false")
	t.Setenv("AUTONOMY_EMERGENCY_STOP", "false")
	t.Setenv("EMERGENCY_STOP", "false")
	restore := safety.SetEmergencyStopProvider(nil)
	defer restore()

	repository := newMemoryRepository()
	service := newTestService(repository)
	_, err := service.Enqueue(ApprovedTask{
		OwnerIdentity: "robert@example.test", RuntimeID: "deepseek-harness", TaskID: "task-unconfigured-stop",
		Prompt: "Inspect the approved workspace.", WorkspaceKey: "hai", Approved: true,
	})
	if !errors.Is(err, ErrEmergencyStopped) {
		t.Fatalf("Enqueue error = %v, want emergency-stop denial when safety control is unavailable", err)
	}
	if len(repository.jobs) != 0 {
		t.Fatalf("unavailable safety control persisted a host task: %#v", repository.jobs)
	}
}

func TestServiceConfirmLeaseChecksEmergencyStopAndTokenFreshness(t *testing.T) {
	service := newTestService(newMemoryRepository())
	job, err := service.Enqueue(ApprovedTask{
		OwnerIdentity: "robert@example.test", RuntimeID: "deepseek-harness", TaskID: "task-confirm",
		Prompt: "Inspect the approved workspace.", WorkspaceKey: "hai", Approved: true,
	})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	lease, err := service.Lease("bridge-01", "deepseek-harness")
	if err != nil || lease == nil {
		t.Fatalf("Lease = %#v, %v", lease, err)
	}
	if err := service.ConfirmLease("bridge-01", job.ID, "wrong-token"); !errors.Is(err, ErrStaleLease) {
		t.Fatalf("ConfirmLease wrong token = %v, want stale lease", err)
	}
	t.Setenv("HAI_EMERGENCY_STOP", "true")
	if err := service.ConfirmLease("bridge-01", job.ID, lease.Token); !errors.Is(err, ErrEmergencyStopped) {
		t.Fatalf("ConfirmLease while stopped = %v, want emergency stop", err)
	}
	t.Setenv("HAI_EMERGENCY_STOP", "false")
	if err := service.ConfirmLease("bridge-01", job.ID, lease.Token); err != nil {
		t.Fatalf("ConfirmLease valid lease: %v", err)
	}
}

func TestConfiguredLeaseOutlivesMaximumAllowedHarnessRun(t *testing.T) {
	t.Setenv("DEEPSEEK_HARNESS_TIMEOUT_SECONDS", "900")
	t.Setenv("HAI_HOST_RUNTIME_LEASE_SECONDS", "60")
	if got, want := configuredLeaseDuration(), 16*time.Minute; got != want {
		t.Fatalf("configuredLeaseDuration = %s, want %s", got, want)
	}

	t.Setenv("HAI_HOST_RUNTIME_LEASE_SECONDS", "1200")
	if got, want := configuredLeaseDuration(), 20*time.Minute; got != want {
		t.Fatalf("configuredLeaseDuration = %s, want %s", got, want)
	}
}
