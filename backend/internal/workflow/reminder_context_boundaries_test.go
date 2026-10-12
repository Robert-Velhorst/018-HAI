package workflow

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"automation-hub-backend/internal/durablejob"
	"automation-hub-backend/internal/models"
	"github.com/google/uuid"
)

// Offline probes check service checkpoints, not real PostgreSQL atomicity.
type reminderContextRepositoryProbe struct {
	*reminderDeliveryFakeRepo
	ctx      context.Context
	stop     func()
	boundary string
	calls    []string
	owner    string
	contexts []context.Context
}

type reminderContextScopedRepository struct {
	*reminderContextRepositoryProbe
	ctx context.Context
}

func (r *reminderContextRepositoryProbe) withReminderDeliveryContext(ctx context.Context) (Repository, error) {
	return &reminderContextScopedRepository{reminderContextRepositoryProbe: r, ctx: ctx}, nil
}

func (r *reminderContextScopedRepository) FindDueReminderDeliveryAuthorizations(owner string, now time.Time, limit, maximum int) ([]reminderDeliveryCandidate, error) {
	r.calls = append(r.calls, "due")
	r.owner = owner
	r.contexts = append(r.contexts, r.ctx)
	items, err := r.reminderDeliveryFakeRepo.FindDueReminderDeliveryAuthorizations(owner, now, limit, maximum)
	if r.boundary == "due" {
		r.stop()
	}
	return items, err
}

func (r *reminderContextScopedRepository) ProcessReminderDelivery(candidate reminderDeliveryCandidate, sink ReminderDeliverySink) (*ReminderDeliveryRunResult, error) {
	r.calls = append(r.calls, "process")
	r.contexts = append(r.contexts, r.ctx)
	result, err := processReminderDeliveryContext(r.ctx, r.reminderContextRepositoryProbe, candidate, sink)
	if r.boundary == "confirmed" {
		r.stop()
	}
	return result, err
}

func (r *reminderContextRepositoryProbe) LoadReminderActivationRequestForOwner(owner string, id uuid.UUID) (*models.WorkflowReminderActivationRequest, *models.WorkflowReminderActivationDecision, error) {
	r.calls = append(r.calls, "activation")
	a, d, err := r.reminderDeliveryFakeRepo.LoadReminderActivationRequestForOwner(owner, id)
	if r.boundary == "activation" {
		r.stop()
	}
	return a, d, err
}

func (r *reminderContextRepositoryProbe) LoadReminderActivationSourceForOwner(owner string, id uuid.UUID) (*WorkflowReminderCandidate, error) {
	r.calls = append(r.calls, "source")
	s, err := r.reminderDeliveryFakeRepo.LoadReminderActivationSourceForOwner(owner, id)
	if r.boundary == "source" {
		r.stop()
	}
	return s, err
}

func (r *reminderContextRepositoryProbe) SaveReminderDeliveryAttempt(a *models.WorkflowReminderDeliveryAttempt) (*models.WorkflowReminderDeliveryAttempt, bool, error) {
	r.calls = append(r.calls, "receipt")
	return r.reminderDeliveryFakeRepo.SaveReminderDeliveryAttempt(a)
}

type reminderContextSinkProbe struct {
	ctx   context.Context
	calls int
	stop  func()
}

func (s *reminderContextSinkProbe) DeliverInternalReminder(ctx context.Context, _ ReminderDeliveryEnvelope) error {
	s.ctx, s.calls = ctx, s.calls+1
	if s.stop != nil {
		s.stop()
	}
	return nil
}

func TestReminderContextStopsBetweenStorageStepsAndRetainsConfirmedResults(t *testing.T) {
	for _, boundary := range []string{"due", "activation", "source", "sink", "confirmed", "complete"} {
		t.Run(boundary, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			repo := &reminderContextRepositoryProbe{reminderDeliveryFakeRepo: newReminderDeliveryFakeRepo(), stop: cancel, boundary: boundary}
			for i := 0; i < 2; i++ {
				newHistoricalReminderFixture(t, time.Now().UTC().Add(-3*time.Minute)).seed(repo.reminderDeliveryFakeRepo)
			}
			newHistoricalReminderFixtureForOwner(t, "bob", time.Now().UTC().Add(-3*time.Minute)).seed(repo.reminderDeliveryFakeRepo)
			sink := &reminderContextSinkProbe{}
			if boundary == "sink" {
				sink.stop = cancel
			}
			engine := &service{repo: repo, reminderDeliverySink: sink}
			result, err := engine.RunDueReminderDeliveriesForOwnerContext(ctx, " alice ", RunDueRequest{Limit: 10})
			want := []string{"due"}
			count := 0
			switch boundary {
			case "activation":
				want = append(want, "process", "activation")
			case "source", "sink":
				want = append(want, "process", "activation", "source")
			case "confirmed":
				want = append(want, "process", "activation", "source", "receipt")
				count = 1
			case "complete":
				want = append(want, "process", "activation", "source", "receipt", "process", "activation", "source", "receipt")
				count = 2
			}
			if (count == 0 && result != nil) || (count > 0 && (result == nil || result.Checked != count || result.Delivered != count || len(result.Results) != count)) || !reflect.DeepEqual(repo.calls, want) {
				t.Fatalf("unexpected confirmed work: summary=%+v calls=%v err=%v", result, repo.calls, err)
			}
			if boundary == "complete" {
				if err != nil {
					t.Fatal(err)
				}
			} else if !errors.Is(err, context.Canceled) {
				t.Fatal("stop lost cancellation identity")
			}
			if repo.ctx != nil || repo.owner != "alice" {
				t.Fatal("service mutated root context or lost owner scope")
			}
			for _, got := range repo.contexts {
				if got != ctx {
					t.Fatal("service discarded its context")
				}
			}
			if sink.calls != 0 && sink.ctx != ctx {
				t.Fatal("internal sink lost its context")
			}
			receipts := 0
			for _, attempts := range repo.attempts {
				receipts += len(attempts)
			}
			if receipts != count {
				t.Fatal("canceled delivery consumed an attempt or escaped owner scope")
			}
		})
	}
}

func TestReminderContextRejectsLegacyMemoryAndCanceledRequests(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	probe := &reminderContextRepositoryProbe{reminderDeliveryFakeRepo: newReminderDeliveryFakeRepo()}
	engine := &service{repo: probe, reminderDeliverySink: &reminderContextSinkProbe{}}
	for _, c := range []context.Context{nil, ctx} {
		if result, err := engine.RunDueReminderDeliveriesContext(c, RunDueRequest{}); result != nil || err == nil {
			t.Fatal("invalid context entered service")
		}
	}
	if result, err := engine.RunDueReminderDeliveriesForOwnerContext(context.Background(), " ", RunDueRequest{}); result != nil || err == nil {
		t.Fatal("blank owner entered batch path")
	}
	if len(probe.calls) != 0 {
		t.Fatal("invalid request started storage")
	}
	legacy := &service{repo: newReminderDeliveryFakeRepo(), reminderDeliverySink: &reminderContextSinkProbe{}}
	if result, err := legacy.RunDueReminderDeliveriesContext(context.Background(), RunDueRequest{}); result != nil || !errors.Is(err, ErrReminderDeliveryContextUnavailable) {
		t.Fatal("legacy fixture silently bypassed contextual atomic contract")
	}
}

type legacyReminderContextSchedulerProbe struct {
	*contextualSchedulerProbe
	ReminderDeliveryService
}

type nilReminderContextSchedulerProbe struct{ *schedulerBoundaryProbe }

func (*nilReminderContextSchedulerProbe) RunDueReminderDeliveriesContext(context.Context, RunDueRequest) (*ReminderDeliveryRunSummary, error) {
	return nil, nil
}

func TestReminderContextSchedulerRejectsMissingOutcome(t *testing.T) {
	t.Setenv("WORKFLOW_OPEN_LOOP_SCHEDULER_ENABLED", "true")
	t.Setenv("WORKFLOW_REMINDER_DELIVERY_ENABLED", "true")
	probe := &nilReminderContextSchedulerProbe{schedulerBoundaryProbe: &schedulerBoundaryProbe{contextualSchedulerProbe: &contextualSchedulerProbe{}}}
	if err := runWorkflowSweep(context.Background(), probe, 3); !errors.Is(err, ErrReminderDeliveryContextUnavailable) || !reflect.DeepEqual(probe.calls, []string{"recover", "followup"}) {
		t.Fatal("missing reminder outcome allowed task execution")
	}
}

func TestReminderContextSchedulerRefusesLegacyBeforeAdmission(t *testing.T) {
	t.Setenv("WORKFLOW_OPEN_LOOP_SCHEDULER_ENABLED", "true")
	t.Setenv("WORKFLOW_REMINDER_DELIVERY_ENABLED", "true")
	probe := &contextualSchedulerProbe{}
	legacy := &legacyReminderContextSchedulerProbe{contextualSchedulerProbe: probe}
	queue := &schedulerContextQueue{}
	runner := durablejob.NewRunner(queue, durablejob.Options{Queue: "workflow"})
	if err := RegisterDurableScheduling(runner, legacy, time.Minute, 3); !errors.Is(err, ErrReminderDeliveryContextUnavailable) || queue.job != nil {
		t.Fatal("legacy reminders admitted recurring work")
	}
	if err := runWorkflowSweep(context.Background(), legacy, 3); !errors.Is(err, ErrReminderDeliveryContextUnavailable) || len(probe.calls) != 0 {
		t.Fatal("legacy reminders allowed earlier recovery mutation")
	}
	t.Setenv("WORKFLOW_REMINDER_DELIVERY_ENABLED", "false")
	if err := runWorkflowSweep(context.Background(), legacy, 3); err != nil || !reflect.DeepEqual(probe.calls, []string{"recover", "followup", "task"}) {
		t.Fatal("disabled reminders changed other stages")
	}
}
