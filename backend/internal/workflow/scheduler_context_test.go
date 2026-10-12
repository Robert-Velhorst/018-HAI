package workflow

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"automation-hub-backend/internal/durablejob"
	"automation-hub-backend/internal/lifecycle"
	"automation-hub-backend/internal/models"

	"github.com/google/uuid"
)

type contextualSchedulerProbe struct {
	calls        []string
	legacyCalls  int
	contextCalls int
	ctx          context.Context
	limit        int
	stop         func()
	boundary     string
}

func (s *contextualSchedulerProbe) RecoverStaleClaims(RunDueRequest) (*ClaimRecoverySummary, error) {
	s.calls = append(s.calls, "recover")
	if s.boundary == "recover" {
		s.stop()
	}
	return &ClaimRecoverySummary{}, nil
}

func (s *contextualSchedulerProbe) RecoverStaleClaimsContext(ctx context.Context, request RunDueRequest) (*ClaimRecoverySummary, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return s.RecoverStaleClaims(request)
}

func (s *contextualSchedulerProbe) RunDueOpenLoops(RunDueRequest) (*OpenLoopRunSummary, error) {
	s.calls = append(s.calls, "legacy-followup")
	s.legacyCalls++
	if s.boundary == "followup" {
		s.stop()
	}
	return &OpenLoopRunSummary{}, nil
}

func (s *contextualSchedulerProbe) RunDueOpenLoopsContext(ctx context.Context, request RunDueRequest) (*OpenLoopRunSummary, error) {
	s.calls = append(s.calls, "followup")
	s.contextCalls++
	s.ctx, s.limit = ctx, request.Limit
	if s.boundary == "followup" {
		s.stop()
	}
	return &OpenLoopRunSummary{Checked: 1}, ctx.Err()
}

func (s *contextualSchedulerProbe) RunDue(RunDueRequest) (*WorkflowRunSummary, error) {
	s.calls = append(s.calls, "task")
	return &WorkflowRunSummary{}, nil
}

func (s *contextualSchedulerProbe) RunDueContext(ctx context.Context, request RunDueRequest) (*WorkflowRunSummary, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return s.RunDue(request)
}

func TestSchedulerContextStopsAfterCancellation(t *testing.T) {
	t.Setenv("WORKFLOW_SCHEDULER_RUN_ON_STARTUP", "true")
	for _, boundary := range []string{"recover", "followup"} {
		t.Run(boundary, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			probe := &contextualSchedulerProbe{stop: cancel, boundary: boundary}
			scheduler := NewScheduler(probe, time.Minute, 7)
			scheduler.Start(ctx)
			want := []string{"recover"}
			if boundary == "followup" {
				want = append(want, "followup")
			}
			if !reflect.DeepEqual(probe.calls, want) || probe.legacyCalls != 0 || scheduler.running.Load() {
				t.Fatalf("canceled scheduler continued or lost context: calls=%v legacy=%d running=%v", probe.calls, probe.legacyCalls, scheduler.running.Load())
			}
			if boundary == "followup" && (probe.ctx != ctx || probe.limit != 7) {
				t.Fatal("ticker follow-up did not receive its context and limit")
			}
		})
	}
}

// Only queue-boundary behavior is modeled; the actual recurring registration,
// job invocation and lifecycle ownership are supplied by durablejob.Runner.
type schedulerContextQueue struct {
	durablejob.Repository
	job      *models.DurableJob
	claimed  bool
	marked   int
	deferred int
}

func (r *schedulerContextQueue) EnqueueIfNoActive(job *models.DurableJob) (bool, error) {
	copy := *job
	copy.ID = uuid.New()
	r.job = &copy
	return true, nil
}

func (*schedulerContextQueue) ReapExpiredLeases(time.Time, time.Duration) (int, error) { return 0, nil }
func (*schedulerContextQueue) ReapExpiredLeasesForQueue(string, time.Time, time.Duration) (int, error) {
	return 0, nil
}
func (r *schedulerContextQueue) ClaimDue(worker, queue string, _ time.Time, _ int) ([]models.DurableJob, error) {
	if r.claimed || r.job == nil {
		return nil, nil
	}
	r.claimed = true
	r.job.Status, r.job.LockedBy, r.job.LeaseGeneration = models.DurableJobRunning, worker, 1
	return []models.DurableJob{*r.job}, nil
}

func (*schedulerContextQueue) ExtendLease(uuid.UUID, string, int64, time.Time) (bool, error) {
	return true, nil
}
func (r *schedulerContextQueue) CompleteRecurring(uuid.UUID, string, int64, time.Time, string, int, string, *models.DurableJob) (bool, bool, error) {
	r.marked++
	return true, true, nil
}

func TestDurableSchedulerContextStopsBetweenStages(t *testing.T) {
	for _, boundary := range []string{"recover", "followup"} {
		t.Run(boundary, func(t *testing.T) {
			group := lifecycle.New(context.Background())
			ctx, cancel := context.WithCancel(group.Context())
			defer cancel()
			defer func() {
				join, stop := context.WithTimeout(context.Background(), time.Second)
				defer stop()
				if err := group.StopAndWait(join); err != nil {
					t.Error(err)
				}
			}()
			queue := &schedulerContextQueue{}
			probe := &contextualSchedulerProbe{stop: cancel, boundary: boundary}
			runner := durablejob.NewRunner(queue, durablejob.Options{Queue: "workflow", Batch: 1})
			if err := RegisterDurableScheduling(runner, probe, time.Minute, 7); err != nil {
				t.Fatal(err)
			}
			processed, err := runner.RunOnce(ctx)
			join, stop := context.WithTimeout(context.Background(), time.Second)
			defer stop()
			if joinErr := group.StopAndWait(join); joinErr != nil {
				t.Fatal(joinErr)
			}
			want := []string{"recover"}
			if boundary == "followup" {
				want = append(want, "followup")
			}
			if processed != 1 || !errors.Is(err, context.Canceled) || !reflect.DeepEqual(probe.calls, want) || probe.legacyCalls != 0 || queue.marked != 0 {
				t.Fatalf("durable handler ignored cancellation: processed=%d err=%v calls=%v legacy=%d marked=%d", processed, err, probe.calls, probe.legacyCalls, queue.marked)
			}
			if boundary == "followup" && (probe.ctx == nil || !errors.Is(probe.ctx.Err(), context.Canceled) || probe.limit != 7) {
				t.Fatal("durable handler discarded cancellation context or limit")
			}
		})
	}
}

func TestDurableSchedulerContextRechecksSafetyGateBeforeEachStage(t *testing.T) {
	var mu sync.Mutex
	allowed := true
	gate := func() bool { mu.Lock(); defer mu.Unlock(); return allowed }
	probe := &contextualSchedulerProbe{boundary: "recover", stop: func() { mu.Lock(); allowed = false; mu.Unlock() }}
	queue := &schedulerContextQueue{}
	runner := durablejob.NewRunner(queue, durablejob.Options{Queue: "workflow", Batch: 1})
	if err := RegisterDurableScheduling(runner, probe, time.Minute, 7, gate); err != nil {
		t.Fatal(err)
	}
	if _, err := runner.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(probe.calls, []string{"recover"}) || queue.marked != 0 || queue.deferred != 1 || queue.job.Status != models.DurableJobPending || queue.job.Attempts != 0 {
		t.Fatalf("paused safety gate advanced work or consumed a retry: calls=%v marked=%d deferred=%d", probe.calls, queue.marked, queue.deferred)
	}
}

func (r *schedulerContextQueue) MarkDeferred(uuid.UUID, string, int64, time.Time, string) (bool, error) {
	r.deferred++
	r.job.Status = models.DurableJobPending
	return true, nil
}

func (r *schedulerContextQueue) MarkForRetry(_ uuid.UUID, _ string, _ int64, _ time.Time, attempt int, reason string) (bool, error) {
	r.job.Status, r.job.Attempts, r.job.LastError = models.DurableJobPending, attempt, reason
	return true, nil
}

func TestDurableSchedulerContextRetainsRedactedRetryFailure(t *testing.T) {
	probe := &schedulerBoundaryProbe{
		contextualSchedulerProbe: &contextualSchedulerProbe{},
		failures:                 map[string]error{"recover": errors.New("token=must-not-be-stored")},
	}
	queue := &schedulerContextQueue{}
	runner := durablejob.NewRunner(queue, durablejob.Options{Queue: "workflow", Batch: 1})
	if err := RegisterDurableScheduling(runner, probe, time.Minute, 3); err != nil {
		t.Fatal(err)
	}
	if processed, err := runner.RunOnce(context.Background()); err != nil || processed != 1 {
		t.Fatalf("retry processing failed: processed=%d err=%v", processed, err)
	}
	if queue.marked != 0 || queue.job.Attempts != 1 || queue.job.Status != models.DurableJobPending || !strings.Contains(queue.job.LastError, "[REDACTED]") || strings.Contains(queue.job.LastError, "must-not-be-stored") {
		t.Fatal("durable sweep discarded its retry failure or retained a recognized credential")
	}
}

type schedulerBoundaryProbe struct {
	ReminderDeliveryService
	*contextualSchedulerProbe
	failures map[string]error
}

func (s *schedulerBoundaryProbe) RecoverStaleClaims(request RunDueRequest) (*ClaimRecoverySummary, error) {
	result, _ := s.contextualSchedulerProbe.RecoverStaleClaims(request)
	return result, s.failures["recover"]
}

func (s *schedulerBoundaryProbe) RecoverStaleClaimsContext(ctx context.Context, request RunDueRequest) (*ClaimRecoverySummary, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return s.RecoverStaleClaims(request)
}

func (s *schedulerBoundaryProbe) RunDueOpenLoopsContext(ctx context.Context, request RunDueRequest) (*OpenLoopRunSummary, error) {
	result, err := s.contextualSchedulerProbe.RunDueOpenLoopsContext(ctx, request)
	if err != nil {
		return result, err
	}
	return result, s.failures["followup"]
}

func (s *schedulerBoundaryProbe) RunDueReminderDeliveries(RunDueRequest) (*ReminderDeliveryRunSummary, error) {
	s.calls = append(s.calls, "reminder")
	if s.boundary == "reminder" {
		s.stop()
	}
	return &ReminderDeliveryRunSummary{}, s.failures["reminder"]
}

func (s *schedulerBoundaryProbe) RunDueReminderDeliveriesContext(ctx context.Context, request RunDueRequest) (*ReminderDeliveryRunSummary, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return s.RunDueReminderDeliveries(request)
}

func (s *schedulerBoundaryProbe) RunDue(request RunDueRequest) (*WorkflowRunSummary, error) {
	result, _ := s.contextualSchedulerProbe.RunDue(request)
	if s.boundary == "task" {
		s.stop()
	}
	return result, s.failures["task"]
}

func (s *schedulerBoundaryProbe) RunDueContext(ctx context.Context, request RunDueRequest) (*WorkflowRunSummary, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return s.RunDue(request)
}

func TestSchedulerSweepStopsOnReturnedCancellationAndUnavailableContext(t *testing.T) {
	for _, stage := range []string{"recover", "followup", "reminder"} {
		for _, failure := range []error{context.Canceled, context.DeadlineExceeded, ErrFollowUpContextUnavailable, ErrReminderDeliveryContextUnavailable, durablejob.Defer("paused")} {
			t.Run(stage+"/"+failure.Error(), func(t *testing.T) {
				probe := &schedulerBoundaryProbe{contextualSchedulerProbe: &contextualSchedulerProbe{}, failures: map[string]error{stage: failure}}
				err := runWorkflowSweep(context.Background(), probe, 3)
				want := []string{"recover"}
				if stage != "recover" {
					want = append(want, "followup")
				}
				if stage == "reminder" {
					want = append(want, "reminder")
				}
				if !errors.Is(err, failure) || !reflect.DeepEqual(probe.calls, want) {
					t.Fatalf("terminal stage failure allowed more work: err=%v calls=%v", err, probe.calls)
				}
			})
		}
	}
}

func TestSchedulerSweepJoinsOrdinaryFailuresWithoutLeakingSecrets(t *testing.T) {
	failures := map[string]error{
		"recover":  errors.New("recovery failed"),
		"followup": errors.New("token=must-not-be-stored"),
		"reminder": errors.New("reminder failed"),
		"task":     errors.New("task failed"),
	}
	probe := &schedulerBoundaryProbe{contextualSchedulerProbe: &contextualSchedulerProbe{}, failures: failures}
	err := runWorkflowSweep(context.Background(), probe, 3)
	if err == nil || strings.Contains(err.Error(), "must-not-be-stored") || !reflect.DeepEqual(probe.calls, []string{"recover", "followup", "reminder", "task"}) {
		t.Fatalf("aggregation, ordering or secret redaction failed: calls=%v", probe.calls)
	}
	for _, failure := range failures {
		if !errors.Is(err, failure) {
			t.Fatal("aggregation discarded original failure identity")
		}
	}
}

type legacyOnlySchedulerProbe struct{ ScheduledWorkflowService }

func (s *legacyOnlySchedulerProbe) RecoverStaleClaimsContext(ctx context.Context, request RunDueRequest) (*ClaimRecoverySummary, error) {
	return s.ScheduledWorkflowService.(ContextualClaimRecoveryBatchService).RecoverStaleClaimsContext(ctx, request)
}

func TestSchedulerSweepRefusesLegacyCapabilityBeforeAnyWork(t *testing.T) {
	probe := &contextualSchedulerProbe{}
	legacy := &legacyOnlySchedulerProbe{ScheduledWorkflowService: probe}
	queue := &schedulerContextQueue{}
	runner := durablejob.NewRunner(queue, durablejob.Options{Queue: "workflow"})
	if err := RegisterDurableScheduling(runner, legacy, time.Minute, 3); !errors.Is(err, ErrFollowUpContextUnavailable) || queue.job != nil {
		t.Fatal("unsupported adapter registered durable work")
	}
	if err := runWorkflowSweep(context.Background(), legacy, 3); !errors.Is(err, ErrFollowUpContextUnavailable) || len(probe.calls) != 0 {
		t.Fatal("legacy adapter performed work")
	}
	t.Setenv("WORKFLOW_OPEN_LOOP_SCHEDULER_ENABLED", "false")
	if err := runWorkflowSweep(context.Background(), legacy, 3); !errors.Is(err, ErrTaskExecutionContextUnavailable) || len(probe.calls) != 0 {
		t.Fatal("disabling follow-ups bypassed required contextual task execution")
	}
	withTasks := &taskOnlySchedulerProbe{legacy}
	if err := runWorkflowSweep(context.Background(), withTasks, 3); err != nil || !reflect.DeepEqual(probe.calls, []string{"recover", "task"}) {
		t.Fatal("disabled follow-up lost existing stage behavior")
	}
}

type taskOnlySchedulerProbe struct{ *legacyOnlySchedulerProbe }

func (s *taskOnlySchedulerProbe) RunDueContext(ctx context.Context, request RunDueRequest) (*WorkflowRunSummary, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return s.RunDue(request)
}

func TestSchedulerSweepStopsAfterReminderAndTaskCancellation(t *testing.T) {
	for _, stage := range []string{"reminder", "task"} {
		t.Run(stage, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			probe := &schedulerBoundaryProbe{contextualSchedulerProbe: &contextualSchedulerProbe{boundary: stage, stop: cancel}}
			err := runWorkflowSweep(ctx, probe, 3)
			want := []string{"recover", "followup", "reminder"}
			if stage == "task" {
				want = append(want, "task")
			}
			if !errors.Is(err, context.Canceled) || !reflect.DeepEqual(probe.calls, want) {
				t.Fatalf("late cancellation ignored: err=%v calls=%v", err, probe.calls)
			}
		})
	}
}

func TestSchedulerSweepRejectsInvalidContextBeforeStorage(t *testing.T) {
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	for _, ctx := range []context.Context{nil, canceled} {
		probe := &contextualSchedulerProbe{}
		if err := runWorkflowSweep(ctx, probe, 3); err == nil || len(probe.calls) != 0 {
			t.Fatal("invalid context reached sweep storage")
		}
	}
}
