package automation

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"automation-hub-backend/internal/events"
	"automation-hub-backend/internal/identity"
	"automation-hub-backend/internal/models"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type launchConfigurationProbe struct {
	Repository
	ctx                                context.Context
	calls, legacyReads, saves, lookups int
	afterRead, afterLookup             context.CancelFunc
	readError                          error
	substitute                         bool
	item                               *models.Automation
}

func (r *launchConfigurationProbe) WithAutomationRepositoryContext(ctx context.Context) (Repository, error) {
	return r, ctx.Err()
}

func TestLaunchHandlerRefusesMissingContextualConfiguration(t *testing.T) {
	gin.SetMode(gin.TestMode)
	id := uuid.New()
	repo := &launchConfigurationProbe{Repository: newFakeAutomationRepo(&models.Automation{ID: id, LaunchType: "browser_url", LaunchTarget: "https://example.invalid/preview"})}
	handler := NewHandler(newTestService(struct{ Repository }{repo}, events.Publisher{}))
	response := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(response)
	c.Request = httptest.NewRequest(http.MethodPost, "/automation/"+id.String()+"/launch", nil)
	c.Params = gin.Params{{Key: "id", Value: id.String()}}
	c.Set(identity.ContextSubjectKey, "alice")
	handler.Launch(c)
	if response.Code != http.StatusServiceUnavailable || repo.legacyReads != 0 || repo.saves != 0 {
		t.Fatalf("missing contextual reader was not refused before storage: status=%d", response.Code)
	}
}

func (r *launchConfigurationProbe) FindByID(id uuid.UUID) (*models.Automation, error) {
	r.legacyReads++
	return r.Repository.FindByID(id)
}
func (r *launchConfigurationProbe) FindByIDContext(ctx context.Context, id uuid.UUID) (*models.Automation, error) {
	r.ctx = ctx
	r.calls++
	item, err := r.Repository.FindByID(id)
	if r.afterRead != nil {
		r.afterRead()
	}
	if r.readError != nil {
		return nil, r.readError
	}
	if r.substitute {
		return r.item, nil
	}
	return item, err
}
func (r *launchConfigurationProbe) FindLaunchIntentByEventKey(key string) (*models.AutomationLaunchEvent, error) {
	r.lookups++
	item, err := r.Repository.FindLaunchIntentByEventKey(key)
	if r.afterLookup != nil {
		r.afterLookup()
	}
	return item, err
}
func (r *launchConfigurationProbe) SaveLaunchIntent(item *models.AutomationLaunchEvent) error {
	r.saves++
	return r.Repository.SaveLaunchIntent(item)
}

func TestOwnedLaunchConfigurationReadStopsBeforeIntent(t *testing.T) {
	for _, boundary := range []string{"valid", "cancel_before", "cancel_after_read", "read_error", "nil_record", "wrong_record", "legacy", "cancel_lookup"} {
		t.Run(boundary, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.WithValue(context.Background(), configurationContextKey{}, "launch-config"))
			defer cancel()
			id := uuid.New()
			configuration := &models.Automation{ID: id, Name: "read-only preview", LaunchType: "browser_url", LaunchTarget: "https://example.invalid/preview"}
			if boundary == "cancel_lookup" {
				configuration.LaunchType = "script"
				configuration.LaunchTarget = "missing-" + uuid.NewString() + ".ps1"
			}
			repo := &launchConfigurationProbe{Repository: newFakeAutomationRepo(configuration)}
			var repository Repository = repo
			cause := errors.New("controlled launch configuration error")
			switch boundary {
			case "cancel_before":
				cancel()
			case "cancel_after_read":
				repo.afterRead = cancel
			case "cancel_lookup":
				repo.afterLookup = cancel
			case "read_error":
				repo.readError = cause
			case "nil_record":
				repo.substitute = true
			case "wrong_record":
				repo.substitute = true
				copy := *configuration
				copy.ID = uuid.New()
				repo.item = &copy
			case "legacy":
				repository = struct{ Repository }{repo}
			}
			svc := newTestService(repository, events.Publisher{})
			result, err := svc.LaunchTask(id, TaskLaunchRequest{ExecutionContext: ctx, OwnerIdentity: "alice", Task: "Preview configuration", IdempotencyKey: uuid.NewString()})
			if boundary == "valid" {
				if err != nil || result == nil || result.Status != "ready" || repo.calls != 1 || repo.saves != 1 {
					t.Fatalf("read-only owned launch failed: %v", err)
				}
				if repo.ctx.Value(configurationContextKey{}) != "launch-config" {
					t.Fatal("configuration lost caller context")
				}
				if _, ok := repo.ctx.Deadline(); !ok {
					t.Fatal("configuration has no deadline")
				}
			} else if err == nil || result != nil || repo.saves != 0 {
				t.Fatalf("unsafe configuration entered launch intent: %v saves=%d", err, repo.saves)
			}
			if repo.legacyReads != 0 {
				t.Fatal("owned launch used legacy configuration reader")
			}
			if ctx.Err() != nil && !errors.Is(err, context.Canceled) {
				t.Fatal("owned configuration lost cancellation")
			}
			if boundary == "read_error" && !errors.Is(err, cause) {
				t.Fatal("configuration lost storage error")
			}
			if (boundary == "cancel_before" || boundary == "legacy") && repo.calls != 0 {
				t.Fatal("configuration entered without capability")
			}
			if boundary == "cancel_lookup" && repo.lookups != 1 {
				t.Fatal("cancellation fixture did not reach idempotency read")
			}
		})
	}
}
