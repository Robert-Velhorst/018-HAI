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

type ownedAdmissionProbe struct {
	launchConfigurationProbe
	scopeContext                         context.Context
	scopeCalls, scopeSaves, scopeLookups int
	nilScope                             bool
	afterSave                            context.CancelFunc
	saveError                            error
	scopeOutcomes                        int
	afterOutcome                         context.CancelFunc
	mismatchedOutcomeOwner               bool
}
type ownedAdmissionView struct {
	Repository
	root *ownedAdmissionProbe
	ctx  context.Context
}

func (r *ownedAdmissionProbe) WithAutomationRepositoryContext(ctx context.Context) (Repository, error) {
	r.scopeContext = ctx
	r.scopeCalls++
	if r.nilScope {
		return nil, nil
	}
	return &ownedAdmissionView{Repository: r.Repository, root: r, ctx: ctx}, nil
}
func (v *ownedAdmissionView) SaveLaunchIntent(item *models.AutomationLaunchEvent) error {
	v.root.scopeSaves++
	if v.root.saveError != nil {
		return v.root.saveError
	}
	err := v.Repository.SaveLaunchIntent(item)
	if v.root.afterSave != nil {
		v.root.afterSave()
	}
	return err
}
func (v *ownedAdmissionView) FindLaunchIntentByEventKey(key string) (*models.AutomationLaunchEvent, error) {
	v.root.scopeLookups++
	item, err := v.Repository.FindLaunchIntentByEventKey(key)
	if v.root.afterLookup != nil {
		v.root.afterLookup()
	}
	return item, err
}

func (v *ownedAdmissionView) FindLaunchOutcomeByIntentID(id uuid.UUID) (*models.AutomationLaunchEvent, error) {
	v.root.scopeOutcomes++
	item, err := v.Repository.FindLaunchOutcomeByIntentID(id)
	if item != nil && v.root.mismatchedOutcomeOwner {
		item.OwnerIdentity = "another-owner"
	}
	if v.root.afterOutcome != nil {
		v.root.afterOutcome()
	}
	return item, err
}

func TestOwnedLaunchReplayUsesAdmissionRepository(t *testing.T) {
	for _, mode := range []string{"valid", "cancel_outcome", "cancel_mismatched_owner", "mismatched_owner"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			id := uuid.New()
			base := newFakeAutomationRepo(&models.Automation{ID: id, LaunchType: "api", LaunchTarget: "POST invalid"})
			repo := &ownedAdmissionProbe{launchConfigurationProbe: launchConfigurationProbe{Repository: base}}
			svc := newTestService(repo, events.Publisher{})
			request := TaskLaunchRequest{ExecutionContext: ctx, OwnerIdentity: "alice", Task: "Do not dispatch without approval", IdempotencyKey: uuid.NewString()}
			first, err := svc.LaunchTask(id, request)
			if err != nil || first == nil || first.Status != "blocked" {
				t.Fatalf("replay fixture failed: %v", err)
			}
			if mode == "cancel_outcome" || mode == "cancel_mismatched_owner" {
				repo.afterOutcome = cancel
			}
			if mode == "cancel_mismatched_owner" || mode == "mismatched_owner" {
				repo.mismatchedOutcomeOwner = true
			}
			second, err := svc.LaunchTask(id, request)
			if repo.scopeOutcomes != 1 || repo.scopeSaves != 1 || len(base.launchEvents) != 1 {
				t.Fatal("replay used unscoped lookup or dispatched another attempt")
			}
			if mode == "mismatched_owner" {
				if second != nil || err != ErrLaunchIdempotencyConflict {
					t.Fatal("ordinary ownership conflict lost its original error identity")
				}
			} else if mode == "cancel_mismatched_owner" {
				if second != nil || !errors.Is(err, context.Canceled) || !errors.Is(err, ErrLaunchIdempotencyConflict) {
					t.Fatal("cancelled replay leaked another owner's receipt")
				}
			} else if mode == "cancel_outcome" {
				if !errors.Is(err, context.Canceled) || second == nil || second.LaunchEventID != first.LaunchEventID || second.Status != first.Status {
					t.Fatal("cancelled replay lost the bound durable receipt or cancellation")
				}
			} else if err != nil || second == nil || second.LaunchEventID != first.LaunchEventID {
				t.Fatal("scoped replay lost durable receipt")
			}
		})
	}
}

func TestLaunchHandlerRefusesMissingAdmissionScope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	id := uuid.New()
	repo := &ownedAdmissionProbe{launchConfigurationProbe: launchConfigurationProbe{Repository: newFakeAutomationRepo(&models.Automation{ID: id, LaunchType: "browser_url", LaunchTarget: "https://example.invalid/preview"})}}
	handler := NewHandler(newTestService(&configOnlyAdmissionRepo{Repository: repo, reader: repo}, events.Publisher{}))
	response := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(response)
	c.Request = httptest.NewRequest(http.MethodPost, "/automation/"+id.String()+"/launch", nil)
	c.Params = gin.Params{{Key: "id", Value: id.String()}}
	c.Set(identity.ContextSubjectKey, "alice")
	handler.Launch(c)
	if response.Code != http.StatusServiceUnavailable || repo.scopeSaves != 0 || repo.saves != 0 {
		t.Fatal("missing admission scope was not refused before intent storage")
	}
}

type configOnlyAdmissionRepo struct {
	Repository
	reader *ownedAdmissionProbe
}

func (r *configOnlyAdmissionRepo) FindByIDContext(ctx context.Context, id uuid.UUID) (*models.Automation, error) {
	return r.reader.FindByIDContext(ctx, id)
}

func TestOwnedLaunchIntentStorageUsesAdmissionScope(t *testing.T) {
	for _, boundary := range []string{"valid", "cancel_lookup", "cancel_save", "save_error", "legacy_scope", "nil_scope"} {
		t.Run(boundary, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.WithValue(context.Background(), configurationContextKey{}, "admission"))
			defer cancel()
			id := uuid.New()
			config := &models.Automation{ID: id, Name: "read-only admission", LaunchType: "browser_url", LaunchTarget: "https://example.invalid/preview"}
			if boundary == "cancel_lookup" {
				config.LaunchType = "script"
				config.LaunchTarget = "missing-" + uuid.NewString() + ".ps1"
			}
			base := newFakeAutomationRepo(config)
			repo := &ownedAdmissionProbe{launchConfigurationProbe: launchConfigurationProbe{Repository: base}}
			var repository Repository = repo
			cause := errors.New("controlled intent storage error")
			switch boundary {
			case "cancel_lookup":
				repo.afterLookup = cancel
			case "cancel_save":
				repo.afterSave = cancel
			case "save_error":
				repo.saveError = cause
			case "legacy_scope":
				repository = &configOnlyAdmissionRepo{Repository: repo, reader: repo}
			case "nil_scope":
				repo.nilScope = true
			}
			result, err := newTestService(repository, events.Publisher{}).LaunchTask(id, TaskLaunchRequest{ExecutionContext: ctx, OwnerIdentity: "alice", Task: "Preview configuration", IdempotencyKey: uuid.NewString()})
			if boundary == "valid" {
				if err != nil || result == nil || result.Status != "ready" || repo.scopeSaves != 1 || repo.scopeCalls != 2 {
					t.Fatalf("owned admission did not use scope: %v", err)
				}
				if repo.scopeContext.Value(configurationContextKey{}) != "admission" {
					t.Fatal("admission lost caller context")
				}
				if _, ok := repo.scopeContext.Deadline(); !ok {
					t.Fatal("admission has no deadline")
				}
			} else {
				if err == nil || len(base.launchEvents) != 0 {
					t.Fatal("uncertain intent storage continued execution")
				}
				if boundary == "cancel_save" || boundary == "save_error" {
					if result == nil || result.Status != "indeterminate" || result.LaunchEventID == uuid.Nil {
						t.Fatal("uncertain intent storage discarded candidate identity")
					}
				} else if result != nil {
					t.Fatal("failed admission invented launch evidence")
				}
			}
			if ctx.Err() != nil && !errors.Is(err, context.Canceled) {
				t.Fatal("admission lost cancellation")
			}
			if boundary == "save_error" && !errors.Is(err, cause) {
				t.Fatal("admission lost write error")
			}
			if boundary == "cancel_lookup" && (repo.scopeLookups != 1 || repo.scopeSaves != 0) {
				t.Fatal("cancelled lookup entered intent write")
			}
			if boundary == "cancel_save" && len(base.launchIntents) != 1 {
				t.Fatal("cancelled acknowledgement removed stored intent")
			}
			if repo.saves != 0 || repo.lookups != 0 {
				t.Fatal("owned admission used unscoped repository")
			}
		})
	}
}
