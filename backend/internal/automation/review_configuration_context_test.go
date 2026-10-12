package automation

import (
	"context"
	"errors"
	"testing"

	"automation-hub-backend/internal/events"
	"automation-hub-backend/internal/models"
	"github.com/google/uuid"
)

type contextualSnapshotInspector interface {
	InspectReviewConfigurationContext(context.Context, uuid.UUID) (*ReviewConfigurationSnapshot, error)
}

type contextualConfigurationReadProbe struct {
	Repository
	ctx       context.Context
	calls     int
	afterRead context.CancelFunc
}

func (r *contextualConfigurationReadProbe) WithAutomationRepositoryContext(ctx context.Context) (Repository, error) {
	return r, ctx.Err()
}

func (r *contextualConfigurationReadProbe) FindByIDContext(ctx context.Context, id uuid.UUID) (*models.Automation, error) {
	r.ctx = ctx
	r.calls++
	item, err := r.Repository.FindByID(id)
	if r.afterRead != nil {
		r.afterRead()
	}
	return item, err
}

func TestReviewConfigurationInspectionOwnsRepositoryContext(t *testing.T) {
	for _, boundary := range []string{"valid", "cancel_before", "cancel_after", "legacy"} {
		t.Run(boundary, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.WithValue(context.Background(), "snapshot-test", "owned"))
			defer cancel()
			id := uuid.New()
			repo := &contextualConfigurationReadProbe{Repository: newFakeAutomationRepo(&models.Automation{ID: id, Name: "local script", LaunchType: "script", LaunchTarget: "script.ps1"})}
			var repository Repository = repo
			if boundary == "legacy" {
				repository = repo.Repository
			}
			svc := newTestService(repository, events.Publisher{})
			inspector, ok := svc.(contextualSnapshotInspector)
			if !ok {
				t.Fatal("canonical service has no contextual review inspector")
			}
			if boundary == "cancel_before" {
				cancel()
			}
			if boundary == "cancel_after" {
				repo.afterRead = cancel
			}
			snapshot, err := inspector.InspectReviewConfigurationContext(ctx, id)
			if boundary == "valid" {
				if err != nil || snapshot == nil || repo.calls != 1 {
					t.Fatalf("snapshot read failed: %v", err)
				}
				if repo.ctx.Value("snapshot-test") != "owned" {
					t.Fatal("repository lost caller context")
				}
				if _, ok := repo.ctx.Deadline(); !ok {
					t.Fatal("repository inspection has no deadline")
				}
			} else if err == nil || snapshot != nil {
				t.Fatal("unsafe inspection returned evidence")
			}
			if ctx.Err() != nil && !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation lost: %v", err)
			}
			if (boundary == "cancel_before" || boundary == "legacy") && repo.calls != 0 {
				t.Fatal("configuration read entered without authority")
			}
		})
	}
}
