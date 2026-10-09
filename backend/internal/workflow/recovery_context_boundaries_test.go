package workflow

import (
	"context"
	"errors"
	"testing"
	"time"

	"automation-hub-backend/internal/durablejob"
	"automation-hub-backend/internal/models"
	"github.com/google/uuid"
)

// Controlled acknowledgements exercise the real service checkpoints, not SQL atomicity.
type recoveryBoundaryRepository struct {
	Repository
	boundary string
	stop     context.CancelFunc
	calls    []string
	owners   []string
	contexts []context.Context
	items    []models.WorkflowItem
	loops    []models.WorkflowOpenLoop
}

type recoveryBoundaryView struct {
	*recoveryBoundaryRepository
	ctx context.Context
}

func (r *recoveryBoundaryRepository) withClaimRecoveryContext(ctx context.Context) (Repository, error) {
	return &recoveryBoundaryView{recoveryBoundaryRepository: r, ctx: ctx}, nil
}

func (r *recoveryBoundaryView) FindExpiredWorkflowClaimsForOwner(owner string, _ time.Time, _ int) ([]models.WorkflowItem, error) {
	r.calls = append(r.calls, "workflows")
	r.owners = append(r.owners, owner)
	r.contexts = append(r.contexts, r.ctx)
	if r.boundary == "workflows" {
		r.stop()
	}
	return r.items, nil
}

func (r *recoveryBoundaryView) FindExpiredOpenLoopClaimsForOwner(owner string, _ time.Time, _ int) ([]models.WorkflowOpenLoop, error) {
	r.calls = append(r.calls, "loops")
	r.owners = append(r.owners, owner)
	r.contexts = append(r.contexts, r.ctx)
	if r.boundary == "loops" {
		r.stop()
	}
	return r.loops, nil
}

func (r *recoveryBoundaryView) RecoverExpiredWorkflowClaimAtomic(item models.WorkflowItem, _ time.Time) (*models.WorkflowItem, bool, error) {
	r.calls = append(r.calls, "recover-workflow")
	r.contexts = append(r.contexts, r.ctx)
	if r.boundary == "unconfirmed" {
		return nil, false, errors.New("token=private-storage-error")
	}
	if r.boundary == "missing-outcome" {
		return nil, true, nil
	}
	if r.boundary == "confirmed" {
		r.stop()
	}
	item.CurrentState = StateBlocked
	return &item, true, nil
}

func (r *recoveryBoundaryView) RecoverExpiredOpenLoopClaimAtomic(owner string, loop models.WorkflowOpenLoop, _ time.Time) (*models.WorkflowOpenLoop, bool, error) {
	r.calls = append(r.calls, "recover-loop")
	r.owners = append(r.owners, owner)
	r.contexts = append(r.contexts, r.ctx)
	if r.boundary == "confirmed-loop" {
		r.stop()
	}
	loop.Status = "open"
	return &loop, true, nil
}

func TestRecoveryContextStopsBetweenStepsAndRetainsOnlyAcknowledgedCounts(t *testing.T) {
	for _, boundary := range []string{"workflows", "loops", "confirmed", "confirmed-loop", "unconfirmed", "missing-outcome", "complete"} {
		t.Run(boundary, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.WithValue(context.Background(), reminderStopContextKey{}, "owned-recovery-service"))
			defer cancel()
			r := &recoveryBoundaryRepository{boundary: boundary, stop: cancel}
			for i := 0; i < 2; i++ {
				r.items = append(r.items, models.WorkflowItem{ID: uuid.New(), OwnerIdentity: "alice"})
				r.loops = append(r.loops, models.WorkflowOpenLoop{ID: uuid.New(), WorkflowID: r.items[i].ID})
			}
			result, err := (&service{repo: r}).RecoverStaleClaimsForOwnerContext(ctx, " alice ", RunDueRequest{Limit: 3})
			wantCalls, wantWorkflows, wantLoops := 6, 2, 2
			if boundary == "workflows" {
				wantCalls, wantWorkflows, wantLoops = 1, 0, 0
			}
			if boundary == "loops" {
				wantCalls, wantWorkflows, wantLoops = 2, 0, 0
			}
			if boundary == "confirmed" {
				wantCalls, wantWorkflows, wantLoops = 3, 1, 0
			}
			if boundary == "confirmed-loop" {
				wantCalls, wantWorkflows, wantLoops = 5, 2, 1
			}
			if boundary == "unconfirmed" || boundary == "missing-outcome" {
				wantCalls, wantWorkflows, wantLoops = 3, 0, 0
			}
			if len(r.calls) != wantCalls {
				t.Fatalf("continued after boundary: %v", r.calls)
			}
			if boundary == "complete" {
				if err != nil {
					t.Fatal(err)
				}
			} else if boundary == "unconfirmed" || boundary == "missing-outcome" {
				if !errors.Is(err, ErrClaimRecoveryOutcomeUnconfirmed) {
					t.Fatal("uncertainty lost its stopping identity")
				}
			} else if !errors.Is(err, context.Canceled) {
				t.Fatal("cancellation lost its identity")
			}
			if result == nil {
				if wantWorkflows != 0 || wantLoops != 0 {
					t.Fatal("lost acknowledged partial outcome")
				}
			} else if result.WorkflowsBlocked != wantWorkflows || result.OpenLoopsReopened != wantLoops || result.Skipped != 0 || len(result.Results) != wantWorkflows+wantLoops {
				t.Fatalf("invented/lost confirmed result: %+v", result)
			}
			for _, owner := range r.owners {
				if owner != "alice" {
					t.Fatal("owner HTTP capability entered system-wide recovery")
				}
			}
			for _, received := range r.contexts {
				if received != ctx {
					t.Fatal("service detached caller context")
				}
			}
		})
	}
}

func TestRecoveryContextRefusesInvalidEntryWithoutStorageCalls(t *testing.T) {
	for _, scenario := range []string{"nil-context", "canceled", "blank-owner", "missing-storage-capability", "nil-service"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			r := &recoveryBoundaryRepository{}
			s, owner := &service{repo: r}, "alice"
			if scenario == "nil-context" {
				ctx = nil
			}
			if scenario == "canceled" {
				cancel()
			}
			if scenario == "blank-owner" {
				owner = " "
			}
			if scenario == "missing-storage-capability" {
				s.repo = newFakeWorkflowRepo()
			}
			if scenario == "nil-service" {
				s = nil
			}
			if result, err := s.RecoverStaleClaimsForOwnerContext(ctx, owner, RunDueRequest{}); err == nil || result != nil || len(r.calls) != 0 {
				t.Fatal("unsafe recovery entry reached storage")
			}
		})
	}
}

type legacyRecoverySchedulerProbe struct{ ScheduledWorkflowService }
type nilRecoverySchedulerProbe struct{ *contextualSchedulerProbe }

func (*nilRecoverySchedulerProbe) RecoverStaleClaimsContext(context.Context, RunDueRequest) (*ClaimRecoverySummary, error) {
	return nil, nil
}

func TestRecoveryContextSchedulerRefusesMissingCapabilityAndUnconfirmedOutcome(t *testing.T) {
	legacy := &legacyRecoverySchedulerProbe{ScheduledWorkflowService: &contextualSchedulerProbe{}}
	queue := &schedulerContextQueue{}
	runner := durablejob.NewRunner(queue, durablejob.Options{Queue: "workflow", Batch: 1})
	if err := RegisterDurableScheduling(runner, legacy, time.Minute, 3); !errors.Is(err, ErrClaimRecoveryContextUnavailable) || queue.job != nil {
		t.Fatal("unsupported recovery registered a job")
	}
	if err := runWorkflowSweep(context.Background(), legacy, 3); !errors.Is(err, ErrClaimRecoveryContextUnavailable) {
		t.Fatal("unsupported recovery entered a sweep")
	}
	probe := &nilRecoverySchedulerProbe{contextualSchedulerProbe: &contextualSchedulerProbe{}}
	if err := runWorkflowSweep(context.Background(), probe, 3); !errors.Is(err, ErrClaimRecoveryOutcomeUnconfirmed) || len(probe.calls) != 0 {
		t.Fatal("missing recovery result allowed later work")
	}
}
