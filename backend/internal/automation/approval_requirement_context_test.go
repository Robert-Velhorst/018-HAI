package automation

import (
	"context"
	"errors"
	"testing"

	"automation-hub-backend/internal/events"
	"automation-hub-backend/internal/models"
	"github.com/google/uuid"
)

type contextualRequirementInspector interface {
	ActionApprovalRequiredContext(context.Context, uuid.UUID) (bool, error)
}

type requirementConfigurationProbe struct {
	contextualConfigurationReadProbe
	substitute bool
	item       *models.Automation
	readError  error
}

func (r *requirementConfigurationProbe) FindByIDContext(ctx context.Context, id uuid.UUID) (*models.Automation, error) {
	item, err := r.contextualConfigurationReadProbe.FindByIDContext(ctx, id)
	if r.readError != nil {
		return nil, r.readError
	}
	if r.substitute {
		return r.item, nil
	}
	return item, err
}

func TestApprovalRequirementOwnsRepositoryContext(t *testing.T) {
	for _, boundary := range []string{"valid", "cancel_before", "cancel_after", "legacy", "missing_record", "wrong_record", "nil_record", "read_error"} {
		t.Run(boundary, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.WithValue(context.Background(), "requirement-test", "owned"))
			defer cancel()
			id := uuid.New()
			base := newFakeAutomationRepo(&models.Automation{ID: id, Name: "local script", LaunchType: "script", LaunchTarget: "script.ps1"})
			repo := &requirementConfigurationProbe{contextualConfigurationReadProbe: contextualConfigurationReadProbe{Repository: base}}
			if boundary == "wrong_record" {
				repo.substitute = true
				repo.item = &models.Automation{ID: uuid.New(), LaunchType: "script", LaunchTarget: "script.ps1"}
			}
			if boundary == "nil_record" {
				repo.substitute = true
			}
			readError := errors.New("controlled requirement storage error")
			if boundary == "read_error" {
				repo.readError = readError
			}
			var repository Repository = repo
			if boundary == "legacy" {
				repository = base
			}
			if boundary == "cancel_before" {
				cancel()
			}
			if boundary == "cancel_after" {
				repo.afterRead = cancel
			}
			svc := newTestService(repository, events.Publisher{})
			inspector, ok := svc.(contextualRequirementInspector)
			if !ok {
				t.Fatal("canonical service has no contextual requirement inspector")
			}
			readID := id
			if boundary == "missing_record" {
				readID = uuid.New()
			}
			required, err := inspector.ActionApprovalRequiredContext(ctx, readID)
			if boundary == "valid" {
				if err != nil || !required || repo.calls != 1 {
					t.Fatalf("requirement read failed: %v", err)
				}
				if repo.ctx.Value("requirement-test") != "owned" {
					t.Fatal("repository lost caller context")
				}
				if _, ok := repo.ctx.Deadline(); !ok {
					t.Fatal("requirement read has no deadline")
				}
			} else if err == nil {
				t.Fatal("uncertain requirement returned authority")
			}
			if ctx.Err() != nil && !errors.Is(err, context.Canceled) {
				t.Fatal("requirement lost cancellation")
			}
			if boundary == "read_error" && !errors.Is(err, readError) {
				t.Fatal("requirement lost storage error identity")
			}
			if (boundary == "cancel_before" || boundary == "legacy") && repo.calls != 0 {
				t.Fatal("read entered without authority")
			}
		})
	}
}
