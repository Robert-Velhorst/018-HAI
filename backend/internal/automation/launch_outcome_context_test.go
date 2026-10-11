package automation

import (
	"context"
	"errors"
	"testing"
	"time"

	"automation-hub-backend/internal/events"
	"automation-hub-backend/internal/models"
	"github.com/google/uuid"
)

type outcomeScopeProbe struct {
	ownedAdmissionProbe
	contexts                        []context.Context
	nilOutcomeScope                 bool
	outcomeSaveError                error
	cancelOutcome                   context.CancelFunc
	cancelAfterWrite                bool
	cancelAfterProjection           bool
	outcomeWrites, projectionWrites int
}
type outcomeScopeView struct {
	Repository
	root *outcomeScopeProbe
	ctx  context.Context
}

func (p *outcomeScopeProbe) WithAutomationRepositoryContext(ctx context.Context) (Repository, error) {
	p.contexts = append(p.contexts, ctx)
	if len(p.contexts) == 1 {
		return p.ownedAdmissionProbe.WithAutomationRepositoryContext(ctx)
	}
	if p.nilOutcomeScope {
		return nil, nil
	}
	child, cancel := context.WithCancel(ctx)
	p.cancelOutcome = cancel
	p.contexts[len(p.contexts)-1] = child
	return &outcomeScopeView{Repository: p.Repository, root: p, ctx: child}, nil
}
func (v *outcomeScopeView) SaveLaunchEvent(item *models.AutomationLaunchEvent) error {
	v.root.outcomeWrites++
	if v.root.outcomeSaveError != nil {
		return v.root.outcomeSaveError
	}
	err := v.Repository.SaveLaunchEvent(item)
	if v.root.cancelAfterWrite {
		v.root.cancelOutcome()
		return errors.Join(err, v.ctx.Err())
	}
	return err
}
func (v *outcomeScopeView) UpdateLaunchState(id uuid.UUID, at time.Time, reason *string) error {
	v.root.projectionWrites++
	err := v.Repository.UpdateLaunchState(id, at, reason)
	if v.root.cancelAfterProjection {
		v.root.cancelOutcome()
		return errors.Join(err, v.ctx.Err())
	}
	return err
}
func TestOwnedLaunchOutcomeUsesSeparateStorageScope(t *testing.T) {
	for _, mode := range []string{"valid", "nil_scope", "write_error", "cancel_write", "cancel_projection"} {
		t.Run(mode, func(t *testing.T) {
			type markerKey struct{}
			ctx := context.WithValue(context.Background(), markerKey{}, "trusted-marker")
			id := uuid.New()
			base := newFakeAutomationRepo(&models.Automation{ID: id, LaunchType: "browser_url", LaunchTarget: "https://example.invalid/preview"})
			p := &outcomeScopeProbe{ownedAdmissionProbe: ownedAdmissionProbe{launchConfigurationProbe: launchConfigurationProbe{Repository: base}}}
			p.nilOutcomeScope = mode == "nil_scope"
			p.cancelAfterWrite = mode == "cancel_write"
			p.cancelAfterProjection = mode == "cancel_projection"
			fault := errors.New("controlled outcome write failure")
			if mode == "write_error" {
				p.outcomeSaveError = fault
			}
			result, err := newTestService(p, events.Publisher{}).LaunchTask(id, TaskLaunchRequest{ExecutionContext: ctx, OwnerIdentity: "alice"})
			if len(p.contexts) != 2 || p.contexts[1] == p.contexts[0] {
				t.Fatal("outcome storage did not acquire a separate scope")
			}
			settlement := p.contexts[1]
			deadline, ok := settlement.Deadline()
			if !ok || time.Until(deadline) > 31*time.Second || settlement.Value(markerKey{}) != "trusted-marker" {
				t.Fatal("outcome storage lost bounded lifetime or trusted values")
			}
			if result == nil || result.LaunchEventID == uuid.Nil {
				t.Fatal("received result/reference was lost")
			}
			switch mode {
			case "valid":
				if err != nil || result.Status != "ready" || p.outcomeWrites != 1 || p.projectionWrites != 1 {
					t.Fatal("valid outcome was not stored through its scope")
				}
			case "nil_scope", "write_error", "cancel_write":
				if err == nil || result.Status != "indeterminate" || p.projectionWrites != 0 {
					t.Fatal("uncertain outcome was claimed or projected")
				}
				if mode == "write_error" && !errors.Is(err, fault) {
					t.Fatal("storage error identity was lost")
				}
				if mode == "cancel_write" && !errors.Is(err, context.Canceled) {
					t.Fatal("write cancellation was lost")
				}
			case "cancel_projection":
				if !errors.Is(err, context.Canceled) || result.Status != "ready" || len(base.launchEvents) != 1 {
					t.Fatal("projection cancellation lost persisted outcome")
				}
			}
		})
	}
}

func TestLaunchOutcomeScopeRetainsValuesAfterCallerCancellation(t *testing.T) {
	for _, mode := range []string{"cancelled", "expired", "nil"} {
		t.Run(mode, func(t *testing.T) {
			type key struct{}
			ctx := context.WithValue(context.Background(), key{}, "trusted")
			var cancel context.CancelFunc
			if mode == "expired" {
				ctx, cancel = context.WithDeadline(ctx, time.Now().Add(-time.Second))
			} else {
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			defer cancel()
			if mode == "nil" {
				ctx = nil
			}
			probe := &ownedAdmissionProbe{launchConfigurationProbe: launchConfigurationProbe{Repository: newFakeAutomationRepo(nil)}}
			svc := newTestService(probe, events.Publisher{}).(*service)
			repo, storageCtx, finish, err := svc.launchOutcomeRepository(ctx, true)
			defer finish()
			if mode == "nil" {
				if err == nil || repo != nil || probe.scopeCalls != 0 {
					t.Fatal("nil owner context silently fell back")
				}
				return
			}
			if err != nil || repo == nil || storageCtx.Err() != nil || storageCtx.Value(key{}) != "trusted" {
				t.Fatal("terminal storage inherited cancellation or lost trusted values")
			}
			at, ok := storageCtx.Deadline()
			if !ok || time.Until(at) <= 0 || time.Until(at) > 31*time.Second {
				t.Fatal("terminal storage is not independently bounded")
			}
			finish()
			if !errors.Is(storageCtx.Err(), context.Canceled) {
				t.Fatal("terminal storage context was not released")
			}
		})
	}
}
