package temporalbridge

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/workflow"

	"github.com/google/uuid"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
)

type cancellationActivityRepository struct {
	Repository
	cancel        context.CancelFunc
	reads, writes int
}

func (r *cancellationActivityRepository) FindByID(id uuid.UUID) (*models.TemporalWorkflowRun, error) {
	r.reads++
	row, err := r.Repository.FindByID(id)
	r.cancel()
	return row, err
}

func (r *cancellationActivityRepository) FindByIDContext(_ context.Context, id uuid.UUID) (*models.TemporalWorkflowRun, error) {
	return r.FindByID(id)
}

func (r *cancellationActivityRepository) TransitionActivity(ctx context.Context, expected, next models.TemporalWorkflowRun) (bool, error) {
	r.writes++
	return r.Repository.TransitionActivity(ctx, expected, next)
}

func (r *cancellationActivityRepository) Update(row *models.TemporalWorkflowRun) (*models.TemporalWorkflowRun, error) {
	r.writes++
	return r.Repository.Update(row)
}

func activityBoundaryRow(t *testing.T, status string) (*memoryTemporalRepository, uuid.UUID) {
	t.Helper()
	repo := newMemoryTemporalRepository()
	id, at := uuid.New(), time.Now().UTC().Truncate(time.Microsecond)
	row := &models.TemporalWorkflowRun{ID: id, OwnerIdentity: "robert@example.test", TemporalWorkflowID: "hai-follow-up-" + id.String(),
		WorkflowType: followUpWorkflowType, Status: status, ScheduledFor: at, UpdatedAt: at, ResultJSON: "{}"}
	if status == "running" || status == "failed" {
		row.StartedAt = &at
	}
	if _, err := repo.Create(row); err != nil {
		t.Fatal(err)
	}
	return repo, id
}

func TestActivityBoundaryCancellationAfterLookupDoesNotStartProposals(t *testing.T) {
	base, id := activityBoundaryRow(t, "scheduled")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	repo := &cancellationActivityRepository{Repository: base, cancel: cancel}
	work := &countingTemporalWorkflowService{}
	a := &followUpActivity{repo: repo, workflows: work, now: time.Now}
	_, err := a.Run(ctx, FollowUpInput{RunID: id.String(), Limit: 7})
	if !errors.Is(err, context.Canceled) || work.calls != 0 || repo.writes != 0 {
		t.Fatalf("canceled activity mutated/executed: err=%v calls=%d writes=%d", err, work.calls, repo.writes)
	}
}

type heldActivityWorkflowService struct {
	workflow.Service
	calls            atomic.Int32
	entered, release chan struct{}
}

func (w *heldActivityWorkflowService) RunDueOpenLoopsForOwner(string, workflow.RunDueRequest) (*workflow.OpenLoopRunSummary, error) {
	if w.calls.Add(1) == 1 {
		close(w.entered)
		<-w.release
	}
	return &workflow.OpenLoopRunSummary{Checked: 1, Triggered: 1}, nil
}

func (w *heldActivityWorkflowService) RunDueOpenLoopsForOwnerContext(ctx context.Context, owner string, request workflow.RunDueRequest) (*workflow.OpenLoopRunSummary, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return w.RunDueOpenLoopsForOwner(owner, request)
}

func TestActivityBoundaryDuplicateWhileRunningDoesNotExecuteAgain(t *testing.T) {
	repo, id := activityBoundaryRow(t, "scheduled")
	work := &heldActivityWorkflowService{entered: make(chan struct{}), release: make(chan struct{})}
	a := &followUpActivity{repo: repo, workflows: work, now: time.Now}
	done := make(chan error, 1)
	var once sync.Once
	defer func() {
		once.Do(func() { close(work.release) })
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Error("owned test activity did not return")
		}
	}()
	go func() {
		_, err := a.Run(context.Background(), FollowUpInput{RunID: id.String(), Limit: 7})
		done <- err
	}()
	select {
	case <-work.entered:
	case <-time.After(time.Second):
		t.Fatal("first activity did not execute")
	}
	_, err := a.Run(context.Background(), FollowUpInput{RunID: id.String(), Limit: 7})
	if err == nil || work.calls.Load() != 1 {
		t.Errorf("duplicate activity executed: err=%v proposal calls=%d", err, work.calls.Load())
	}
	once.Do(func() { close(work.release) })
}

func TestActivityBoundaryInterruptedRunningRecordIsNotBlindlyRetried(t *testing.T) {
	for _, status := range []string{"running", "failed"} {
		t.Run(status, func(t *testing.T) {
			repo, id := activityBoundaryRow(t, status)
			work := &countingTemporalWorkflowService{}
			a := &followUpActivity{repo: repo, workflows: work, now: time.Now}
			_, err := a.Run(context.Background(), FollowUpInput{RunID: id.String(), Limit: 7})
			var application *temporal.ApplicationError
			if !errors.As(err, &application) || !application.NonRetryable() || work.calls != 0 {
				t.Fatalf("unreconciled activity repeated or auto-retriable: err=%v calls=%d", err, work.calls)
			}
		})
	}
}

type simultaneousReadActivityRepository struct {
	Repository
	arrived chan struct{}
	release chan struct{}
}

func (r *simultaneousReadActivityRepository) FindByIDContext(ctx context.Context, id uuid.UUID) (*models.TemporalWorkflowRun, error) {
	row, err := r.Repository.FindByIDContext(ctx, id)
	r.arrived <- struct{}{}
	select {
	case <-r.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return row, err
}

func TestActivityBoundarySimultaneousUnstartedReadsHaveOneWinner(t *testing.T) {
	base, id := activityBoundaryRow(t, "scheduled")
	repo := &simultaneousReadActivityRepository{Repository: base, arrived: make(chan struct{}, 2), release: make(chan struct{})}
	work := &heldActivityWorkflowService{entered: make(chan struct{}), release: make(chan struct{})}
	close(work.release)
	a := &followUpActivity{repo: repo, workflows: work, now: time.Now}
	done := make(chan error, 2)
	var once sync.Once
	pending := 2
	defer func() {
		once.Do(func() { close(repo.release) })
		for pending > 0 {
			select {
			case <-done:
				pending--
			case <-time.After(time.Second):
				t.Error("owned claim-race activity did not return")
				return
			}
		}
	}()
	for range 2 {
		go func() {
			_, err := a.Run(context.Background(), FollowUpInput{RunID: id.String(), Limit: 7})
			done <- err
		}()
	}
	for range 2 {
		select {
		case <-repo.arrived:
		case <-time.After(time.Second):
			t.Fatal("fixture did not hold both pre-claim reads")
		}
	}
	once.Do(func() { close(repo.release) })
	successes := 0
	for range 2 {
		select {
		case err := <-done:
			pending--
			if err == nil {
				successes++
			}
		case <-time.After(time.Second):
			t.Fatal("activity did not finish")
		}
	}
	if successes != 1 || work.calls.Load() != 1 {
		t.Fatalf("pre-claim read race executed more than once: successes=%d calls=%d", successes, work.calls.Load())
	}
}

type activityTransitionHookRepository struct {
	Repository
	hook       func(models.TemporalWorkflowRun)
	failStatus string
	conflict   bool
	contexts   []context.Context
}

func (r *activityTransitionHookRepository) TransitionActivity(ctx context.Context, expected, next models.TemporalWorkflowRun) (bool, error) {
	r.contexts = append(r.contexts, ctx)
	if next.Status == r.failStatus {
		if r.conflict {
			return false, nil
		}
		return false, errors.New("password=synthetic-private-storage-value")
	}
	changed, err := r.Repository.TransitionActivity(ctx, expected, next)
	if r.hook != nil {
		r.hook(next)
	}
	return changed, err
}

func TestActivityBoundaryCancellationAfterClaimSkipsEffectAndSettles(t *testing.T) {
	base, id := activityBoundaryRow(t, "scheduled")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	repo := &activityTransitionHookRepository{Repository: base, hook: func(next models.TemporalWorkflowRun) {
		if next.Status == "running" {
			cancel()
		}
	}}
	work := &countingTemporalWorkflowService{}
	a := &followUpActivity{repo: repo, workflows: work, now: time.Now}
	_, err := a.Run(ctx, FollowUpInput{RunID: id.String(), Limit: 7})
	row, findErr := base.FindByID(id)
	if !errors.Is(err, context.Canceled) || findErr != nil || work.calls != 0 || row.Status != "failed" || row.StartedAt == nil || row.CompletedAt != nil {
		t.Fatalf("post-claim cancellation executed/lost settlement: err=%v row=%+v calls=%d", err, row, work.calls)
	}
}

type resultActivityWorkflowService struct {
	workflow.Service
	calls int
	run   func() (*workflow.OpenLoopRunSummary, error)
}

func (w *resultActivityWorkflowService) RunDueOpenLoopsForOwner(string, workflow.RunDueRequest) (*workflow.OpenLoopRunSummary, error) {
	w.calls++
	return w.run()
}

func (w *resultActivityWorkflowService) RunDueOpenLoopsForOwnerContext(ctx context.Context, owner string, request workflow.RunDueRequest) (*workflow.OpenLoopRunSummary, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return w.RunDueOpenLoopsForOwner(owner, request)
}

func TestActivityBoundaryEffectAfterCancellationIsRecordedWithoutRetry(t *testing.T) {
	base, id := activityBoundaryRow(t, "scheduled")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	repo := &activityTransitionHookRepository{Repository: base}
	work := &resultActivityWorkflowService{run: func() (*workflow.OpenLoopRunSummary, error) {
		cancel()
		return &workflow.OpenLoopRunSummary{Checked: 1, Triggered: 1}, nil
	}}
	a := &followUpActivity{repo: repo, workflows: work, now: time.Now}
	result, err := a.Run(ctx, FollowUpInput{RunID: id.String(), Limit: 7})
	if err != nil || result.Triggered != 1 || work.calls != 1 || base.lastStatus() != "completed" || len(repo.contexts) != 2 {
		t.Fatalf("canceled response erased/repeated effect: %+v/%v calls=%d state=%s", result, err, work.calls, base.lastStatus())
	}
	if deadline, ok := repo.contexts[1].Deadline(); !ok || time.Until(deadline) > 3*time.Second {
		t.Error("settlement lacks its own bounded deadline")
	}
	_, err = a.Run(context.Background(), FollowUpInput{RunID: id.String(), Limit: 7})
	if err != nil || work.calls != 1 {
		t.Fatalf("completed replay repeated effect: %v calls=%d", err, work.calls)
	}
}

func TestActivityBoundaryUnconfirmedSettlementCannotSucceedOrRepeat(t *testing.T) {
	for _, conflict := range []bool{false, true} {
		base, id := activityBoundaryRow(t, "scheduled")
		repo := &activityTransitionHookRepository{Repository: base, failStatus: "completed", conflict: conflict}
		work := &countingTemporalWorkflowService{}
		a := &followUpActivity{repo: repo, workflows: work, now: time.Now}
		_, err := a.Run(context.Background(), FollowUpInput{RunID: id.String(), Limit: 7})
		var application *temporal.ApplicationError
		if !errors.As(err, &application) || !application.NonRetryable() || work.calls != 1 || base.lastStatus() != "running" {
			t.Fatalf("unconfirmed completion reported success/retry: %v calls=%d state=%s", err, work.calls, base.lastStatus())
		}
		_, err = a.Run(context.Background(), FollowUpInput{RunID: id.String(), Limit: 7})
		if err == nil || work.calls != 1 {
			t.Fatalf("uncertain run repeated: %v calls=%d", err, work.calls)
		}
	}
}

func TestActivityBoundaryMissingProposalResultFailsWithoutPanic(t *testing.T) {
	base, id := activityBoundaryRow(t, "scheduled")
	work := &resultActivityWorkflowService{run: func() (*workflow.OpenLoopRunSummary, error) { return nil, nil }}
	a := &followUpActivity{repo: base, workflows: work, now: time.Now}
	_, err := a.Run(context.Background(), FollowUpInput{RunID: id.String(), Limit: 7})
	if err == nil || base.lastStatus() != "failed" || work.calls != 1 {
		t.Fatalf("missing result succeeded: %v state=%s calls=%d", err, base.lastStatus(), work.calls)
	}
}

func TestActivityBoundarySDKDoesNotRetryAmbiguousCompletion(t *testing.T) {
	base, id := activityBoundaryRow(t, "scheduled")
	row, err := base.FindByID(id)
	if err != nil {
		t.Fatal(err)
	}
	repo := &activityTransitionHookRepository{Repository: base, failStatus: "completed"}
	work := &countingTemporalWorkflowService{}
	a := &followUpActivity{repo: repo, workflows: work, now: time.Now}
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.SetStartTime(row.ScheduledFor)
	env.RegisterActivity(a.Run)
	env.ExecuteWorkflow(GovernedFollowUpWorkflow, FollowUpInput{RunID: id.String(), RunAt: row.ScheduledFor, Limit: 7})
	err = env.GetWorkflowError()
	var application *temporal.ApplicationError
	if !env.IsWorkflowCompleted() || !errors.As(err, &application) || !application.NonRetryable() || work.calls != 1 || base.lastStatus() != "running" {
		t.Fatalf("SDK retry repeated an uncertain effect: err=%v calls=%d status=%s", err, work.calls, base.lastStatus())
	}
	if strings.Contains(err.Error(), "synthetic-private-storage-value") || strings.Contains(err.Error(), "password=") {
		t.Fatal("private repository failure entered the SDK failure payload")
	}
}

func TestActivityBoundaryLateSettlementPreservesChangedOrMissingRow(t *testing.T) {
	for _, mutation := range []string{"owner", "completed", "removed"} {
		t.Run(mutation, func(t *testing.T) {
			base, id := activityBoundaryRow(t, "scheduled")
			work := &resultActivityWorkflowService{run: func() (*workflow.OpenLoopRunSummary, error) {
				row, err := base.FindByID(id)
				if err != nil {
					t.Fatal(err)
				}
				switch mutation {
				case "owner":
					row.OwnerIdentity = "other@example.test"
				case "completed":
					row.Status, row.CompletedAt, row.ResultJSON = "completed", row.StartedAt, `{"checked":9}`
				case "removed":
					base.mu.Lock()
					delete(base.records, id)
					base.mu.Unlock()
					return &workflow.OpenLoopRunSummary{Checked: 1}, nil
				}
				if _, err := base.Update(row); err != nil {
					t.Fatal(err)
				}
				return &workflow.OpenLoopRunSummary{Checked: 1}, nil
			}}
			a := &followUpActivity{repo: base, workflows: work, now: time.Now}
			_, err := a.Run(context.Background(), FollowUpInput{RunID: id.String(), Limit: 7})
			var application *temporal.ApplicationError
			if !errors.As(err, &application) || !application.NonRetryable() || work.calls != 1 {
				t.Fatalf("changed run was treated as owned: %v calls=%d", err, work.calls)
			}
			row, readErr := base.FindByID(id)
			if mutation == "removed" {
				if readErr == nil || row != nil {
					t.Fatal("activity upserted a removed run")
				}
			} else if readErr != nil || (mutation == "owner" && row.OwnerIdentity != "other@example.test") || (mutation == "completed" && row.ResultJSON != `{"checked":9}`) {
				t.Fatalf("activity overwrote newer provenance/result: %+v/%v", row, readErr)
			}
		})
	}
}
