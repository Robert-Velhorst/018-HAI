package automation

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/agentruntime"
	"automation-hub-backend/internal/events"
	"automation-hub-backend/internal/models"
	"github.com/google/uuid"
)

type ownedStopRoot struct {
	*fakeAutomationRepo
	mode                 string
	cancel               context.CancelFunc
	legacyReads, lookups int
	scopes               []context.Context
	outcomeContext       context.Context
}

func (r *ownedStopRoot) FindByID(id uuid.UUID) (*models.Automation, error) {
	r.legacyReads++
	return r.fakeAutomationRepo.FindByID(id)
}
func (r *ownedStopRoot) FindByIDContext(ctx context.Context, id uuid.UUID) (*models.Automation, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return r.fakeAutomationRepo.FindByID(id)
}
func (r *ownedStopRoot) WithAutomationRepositoryContext(ctx context.Context) (Repository, error) {
	if r.mode == "missing_scope" {
		return nil, errors.New("scope unavailable")
	}
	r.scopes = append(r.scopes, ctx)
	return &ownedStopView{Repository: r.fakeAutomationRepo, root: r, ctx: ctx}, nil
}

type ownedStopView struct {
	Repository
	root *ownedStopRoot
	ctx  context.Context
}

func (v *ownedStopView) FindOwnerActiveRuntimeLaunch(id uuid.UUID, runtime, owner string) (*models.AutomationLaunchEvent, error) {
	v.root.lookups++
	item, err := v.Repository.FindOwnerActiveRuntimeLaunch(id, runtime, owner)
	if v.root.mode == "cancel_lookup" {
		v.root.cancel()
	}
	return item, err
}
func (v *ownedStopView) SaveLaunchIntent(item *models.AutomationLaunchEvent) error {
	err := v.Repository.SaveLaunchIntent(item)
	if v.root.mode == "cancel_intent" {
		v.root.cancel()
	}
	return err
}
func (v *ownedStopView) SaveLaunchEvent(item *models.AutomationLaunchEvent) error {
	v.root.outcomeContext = v.ctx
	return v.Repository.SaveLaunchEvent(item)
}

type ownedStopAdapter struct {
	*fakeAgentRuntimeAdapter
	ctx    context.Context
	cancel context.CancelFunc
}

func (a *ownedStopAdapter) StopTaskWithReference(ctx context.Context, task, owner, reference string) agentruntime.StopResult {
	a.ctx = ctx
	if a.cancel != nil {
		a.cancel()
	}
	return a.fakeAgentRuntimeAdapter.StopTaskWithReference(ctx, task, owner, reference)
}

func TestOwnedRuntimeStopKeepsRequestAndStorageContexts(t *testing.T) {
	for _, mode := range []string{"valid", "nil_context", "cancel_before", "missing_scope", "cancel_lookup", "cancel_intent", "cancel_dispatch"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.WithValue(context.Background(), stopSummaryContextKey{}, "operator-snapshot"))
			defer cancel()
			id := uuid.New()
			base := newFakeAutomationRepo(&models.Automation{ID: id, LaunchType: "agent_runtime", RuntimeType: "openclaw"})
			base.launchEvents = []models.AutomationLaunchEvent{{ID: uuid.New(), AutomationID: id, LaunchType: "agent_runtime", RuntimeType: "openclaw", OwnerIdentity: "alice", RuntimeTaskID: "task-1", ExecutionReference: "ocgw:v2:run-1", Status: "running"}}
			root := &ownedStopRoot{fakeAutomationRepo: base, mode: mode, cancel: cancel}
			adapter := &ownedStopAdapter{fakeAgentRuntimeAdapter: &fakeAgentRuntimeAdapter{id: "openclaw", gatewayDelegation: true}}
			if mode == "cancel_dispatch" {
				adapter.cancel = cancel
			}
			if mode == "cancel_before" {
				cancel()
			}
			requestCtx := ctx
			if mode == "nil_context" {
				requestCtx = nil
			}
			result, err := newTestServiceWithRuntimeRegistry(root, events.Publisher{}, agentruntime.NewRegistry(adapter)).StopRuntimeTaskForOwnerContext(requestCtx, id, "alice")
			if root.legacyReads != 0 {
				t.Fatal("owned stop used legacy configuration read")
			}
			if mode == "valid" || mode == "cancel_dispatch" {
				if err != nil || result == nil || result.Status != "cancellation_requested" || adapter.stopCalls != 1 || adapter.ctx != ctx {
					t.Fatal("stop lost original execution context or valid receipt")
				}
				if len(root.scopes) != 2 || root.outcomeContext == nil || root.outcomeContext.Err() != context.Canceled { // scope closes after persistence
					t.Fatal("admission and outcome storage scopes were not separately closed")
				}
				for _, scope := range root.scopes {
					if scope.Value(stopSummaryContextKey{}) != "operator-snapshot" {
						t.Fatal("storage lost inherited values")
					}
					deadline, ok := scope.Deadline()
					if !ok || time.Until(deadline) > approvalRegistrationTimeout {
						t.Fatal("stop storage has no bounded deadline")
					}
				}
			} else {
				if err == nil || adapter.stopCalls != 0 {
					t.Fatal("unsafe stop reached runtime")
				}
				if mode == "cancel_intent" && (result == nil || result.Status != "indeterminate" || !strings.HasPrefix(result.EvidenceURI, "automation-launch://")) {
					t.Fatal("uncertain stop intent identity was lost")
				}
			}
		})
	}
}
