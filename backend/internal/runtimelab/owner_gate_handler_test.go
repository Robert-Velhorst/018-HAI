package runtimelab

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/apierror"
	"automation-hub-backend/internal/executionbroker"
	"automation-hub-backend/internal/identity"
	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/operations"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const runtimeLabHandlerOwner = "configured-owner"
const runtimeLabHandlerWorkspace = "configured-workspace"

type runtimeLabOwnerEndpoint struct {
	name    string
	method  string
	path    string
	handler gin.HandlerFunc
}

func runtimeLabOwnerEndpoints(h *Handler) []runtimeLabOwnerEndpoint {
	return []runtimeLabOwnerEndpoint{
		{"overview", http.MethodGet, "/runtime-lab/overview", h.Overview},
		{"feature parity", http.MethodGet, "/runtime-lab/feature-parity", h.FeatureParity},
		{"runtime feature parity", http.MethodGet, "/runtime-lab/:runtimeId/feature-parity", h.RuntimeFeatureParity},
		{"capabilities", http.MethodGet, "/runtime-lab/capabilities", h.Capabilities},
		{"probe", http.MethodPost, "/runtime-lab/:runtimeId/probe", h.Probe},
		{"self-test", http.MethodPost, "/runtime-lab/:runtimeId/self-test", h.SelfTest},
		{"attempts", http.MethodGet, "/runtime-lab/:runtimeId/attempts", h.Attempts},
	}
}

type runtimeLabHandlerLedgerSpy struct {
	*operations.MemoryRepository
	reads  int
	writes int
}

func (r *runtimeLabHandlerLedgerSpy) FindByDedupeKey(owner, workspace, key string) (*models.Operation, bool, error) {
	r.reads++
	return r.MemoryRepository.FindByDedupeKey(owner, workspace, key)
}

func (r *runtimeLabHandlerLedgerSpy) CreateWithEvent(op *models.Operation, event *models.OperationEvent) (*models.Operation, error) {
	r.writes++
	return r.MemoryRepository.CreateWithEvent(op, event)
}

func (r *runtimeLabHandlerLedgerSpy) UpdateWithEvent(op *models.Operation, event *models.OperationEvent) (*models.Operation, error) {
	r.writes++
	return r.MemoryRepository.UpdateWithEvent(op, event)
}

func (r *runtimeLabHandlerLedgerSpy) List(filter operations.Filter) ([]models.Operation, error) {
	r.reads++
	return r.MemoryRepository.List(filter)
}

func (r *runtimeLabHandlerLedgerSpy) ListEvents(id uuid.UUID, limit int) ([]models.OperationEvent, error) {
	r.reads++
	return r.MemoryRepository.ListEvents(id, limit)
}

type runtimeLabHandlerAdapterSpy struct {
	Adapter
	id    string
	calls int
}

func (a *runtimeLabHandlerAdapterSpy) Info() RuntimeInfo {
	a.calls++
	return RuntimeInfo{ID: a.id, DisplayName: a.id, Kind: KindAgentRuntime}
}

func (a *runtimeLabHandlerAdapterSpy) HealthCheck(context.Context) Health {
	a.calls++
	return Health{Status: executionbroker.RuntimeNotConfigured, Detail: "owner-only runtime detail"}
}

func (a *runtimeLabHandlerAdapterSpy) Capabilities() []string {
	a.calls++
	return []string{"owner-only capability"}
}

func (a *runtimeLabHandlerAdapterSpy) SetupRequirements() []SetupRequirement {
	a.calls++
	return nil
}

func (a *runtimeLabHandlerAdapterSpy) Probe(_ context.Context, now time.Time) ProbeResult {
	a.calls++
	return ProbeResult{RuntimeID: a.id, Status: executionbroker.RuntimeNotConfigured, CheckedAt: now}
}

type runtimeLabHandlerFixture struct {
	handler    *Handler
	ledger     *runtimeLabHandlerLedgerSpy
	adapters   []*runtimeLabHandlerAdapterSpy
	seed       models.Operation
	clockCalls int
}

func newRuntimeLabHandlerFixture(t *testing.T) *runtimeLabHandlerFixture {
	t.Helper()
	base := operations.NewMemoryRepository()
	seed, err := operations.NewService(base).Ingest(operations.NewOperationInput{
		OwnerUserID: runtimeLabHandlerOwner, WorkspaceID: runtimeLabHandlerWorkspace,
		Title: "owner-only ledger detail", OperationType: "review_source_item",
		SourceType: "runtime_lab", DedupeKey: "owner-gate-seed",
	})
	if err != nil {
		t.Fatal(err)
	}
	fixture := &runtimeLabHandlerFixture{
		ledger: &runtimeLabHandlerLedgerSpy{MemoryRepository: base},
		seed:   seed.Operation,
	}
	svc := NewService(executionbroker.NewBroker(""), operations.NewService(fixture.ledger), runtimeLabHandlerOwner, runtimeLabHandlerWorkspace)
	svc.reg = &Registry{}
	for _, id := range []string{executionbroker.LocalSafeWorkerID, "openclaw", "hermes", "odysseus", "openhands"} {
		adapter := &runtimeLabHandlerAdapterSpy{Adapter: newBrowserContract(), id: id}
		fixture.adapters = append(fixture.adapters, adapter)
		svc.reg.adapters = append(svc.reg.adapters, adapter)
	}
	svc.now = func() time.Time {
		fixture.clockCalls++
		return time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	}
	svc.WithSafeExecutionPolicy(func(string, string, string) bool { return true })
	svc.attempts = []RuntimeAttempt{{
		ID: "owner-private-attempt", RuntimeID: executionbroker.LocalSafeWorkerID,
		Status: AttemptSetupRequired, Detail: "owner-only attempt detail",
	}}
	svc.seq = 73
	fixture.handler = NewHandler(svc)
	return fixture
}

func runtimeLabOwnerRequest(endpoint runtimeLabOwnerEndpoint, subject any, present bool, runtimeID string) (*httptest.ResponseRecorder, int) {
	router := gin.New()
	router.Use(func(c *gin.Context) {
		if present {
			c.Set(identity.ContextSubjectKey, subject)
		}
		// Even an administrative role does not grant another operator's scope.
		c.Set(identity.ContextRoleKey, "admin")
		c.Next()
	})
	followingCalls := 0
	router.Handle(endpoint.method, endpoint.path, endpoint.handler, func(c *gin.Context) {
		followingCalls++
	})
	path := strings.ReplaceAll(endpoint.path, ":runtimeId", runtimeID)
	request := httptest.NewRequest(endpoint.method, path+"?ownerIdentity="+runtimeLabHandlerOwner,
		strings.NewReader(`{"ownerIdentity":"configured-owner","workspaceId":"configured-workspace"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Owner-Identity", runtimeLabHandlerOwner)
	request.Header.Set("X-Subject", runtimeLabHandlerOwner)
	request.Header.Set("Authorization", "Bearer caller-supplied-owner-claim")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response, followingCalls
}

func TestRuntimeLabHandlersRejectForeignAndAnonymousSubjectsBeforeServiceAccess(t *testing.T) {
	cases := []struct {
		name    string
		subject any
		present bool
		status  int
		code    apierror.Code
	}{
		{"foreign subject", "foreign-owner", true, http.StatusForbidden, apierror.CodeForbidden},
		{"case-distinct subject", "Configured-owner", true, http.StatusForbidden, apierror.CodeForbidden},
		{"anonymous", nil, false, http.StatusUnauthorized, apierror.CodeUnauthorized},
		{"nil subject", nil, true, http.StatusUnauthorized, apierror.CodeUnauthorized},
		{"empty subject", "", true, http.StatusUnauthorized, apierror.CodeUnauthorized},
		{"whitespace subject", " \t\n", true, http.StatusUnauthorized, apierror.CodeUnauthorized},
		{"padded owner alias", " " + runtimeLabHandlerOwner + " ", true, http.StatusUnauthorized, apierror.CodeUnauthorized},
		{"non-string subject", 17, true, http.StatusUnauthorized, apierror.CodeUnauthorized},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			for _, runtimeID := range []string{executionbroker.LocalSafeWorkerID, "missing-runtime"} {
				fixture := newRuntimeLabHandlerFixture(t)
				for _, endpoint := range runtimeLabOwnerEndpoints(fixture.handler) {
					t.Run(runtimeID+"/"+endpoint.name, func(t *testing.T) {
						base := fixture.ledger.MemoryRepository
						filter := operations.Filter{OwnerUserID: runtimeLabHandlerOwner, WorkspaceID: runtimeLabHandlerWorkspace, Limit: 200}
						beforeOperations, err := base.List(filter)
						if err != nil {
							t.Fatal(err)
						}
						beforeEvents, err := base.ListEvents(fixture.seed.ID, 500)
						if err != nil {
							t.Fatal(err)
						}
						beforeAttempts := append([]RuntimeAttempt(nil), fixture.handler.svc.attempts...)
						beforeSeq := fixture.handler.svc.seq
						response, followingCalls := runtimeLabOwnerRequest(endpoint, test.subject, test.present, runtimeID)
						if response.Code != test.status {
							t.Fatalf("status = %d, want %d; body=%s", response.Code, test.status, response.Body.String())
						}
						var envelope apierror.Envelope
						if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
							t.Fatal(err)
						}
						if envelope.Error == nil || envelope.Error.Code != test.code || envelope.Error.Message == "" {
							t.Fatalf("unexpected authorization envelope: %+v", envelope)
						}
						for _, secret := range []string{runtimeLabHandlerOwner, runtimeLabHandlerWorkspace, "owner-only", "owner-private-attempt"} {
							if strings.Contains(response.Body.String(), secret) {
								t.Errorf("rejection exposed configured-owner information %q", secret)
							}
						}
						if followingCalls != 0 {
							t.Error("rejected request did not abort the remaining handler chain")
						}
						if fixture.ledger.reads != 0 || fixture.ledger.writes != 0 || fixture.clockCalls != 0 {
							t.Fatalf("rejection accessed service state: reads=%d writes=%d clock=%d", fixture.ledger.reads, fixture.ledger.writes, fixture.clockCalls)
						}
						for _, adapter := range fixture.adapters {
							if adapter.calls != 0 {
								t.Errorf("rejection accessed adapter %s %d times", adapter.id, adapter.calls)
							}
						}
						afterOperations, err := base.List(filter)
						if err != nil {
							t.Fatal(err)
						}
						afterEvents, err := base.ListEvents(fixture.seed.ID, 500)
						if err != nil {
							t.Fatal(err)
						}
						if !reflect.DeepEqual(beforeOperations, afterOperations) || !reflect.DeepEqual(beforeEvents, afterEvents) ||
							!reflect.DeepEqual(beforeAttempts, fixture.handler.svc.attempts) || beforeSeq != fixture.handler.svc.seq {
							t.Fatal("rejected request changed ledger, audit, or runtime-attempt state")
						}
					})
				}
			}
		})
	}
}

func TestRuntimeLabHandlersAllowExactConfiguredOwner(t *testing.T) {
	for _, name := range []string{"overview", "feature parity", "runtime feature parity", "capabilities", "probe", "self-test", "attempts"} {
		t.Run(name, func(t *testing.T) {
			fixture := newRuntimeLabHandlerFixture(t)
			for _, endpoint := range runtimeLabOwnerEndpoints(fixture.handler) {
				if endpoint.name != name {
					continue
				}
				response, followingCalls := runtimeLabOwnerRequest(endpoint, runtimeLabHandlerOwner, true, "hermes")
				if response.Code != http.StatusOK {
					t.Fatalf("configured-owner status = %d; body=%s", response.Code, response.Body.String())
				}
				if followingCalls != 1 {
					t.Fatal("configured-owner request did not complete its handler chain")
				}
				var body map[string]json.RawMessage
				if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
					t.Fatal(err)
				}
				if len(body) == 0 || body["error"] != nil {
					t.Fatalf("configured owner did not receive the normal response: %s", response.Body.String())
				}
				if name == "self-test" && (len(fixture.handler.svc.attempts) != 2 ||
					fixture.handler.svc.attempts[1].RuntimeID != "hermes") {
					t.Fatal("configured-owner self-test did not record its setup-only attempt")
				}
			}
		})
	}
}

func TestRuntimeLabHandlersFailClosedWithoutConfiguredOwner(t *testing.T) {
	cases := []struct {
		name    string
		handler *Handler
	}{
		{"nil handler", nil},
		{"nil service", NewHandler(nil)},
		{"empty owner", NewHandler(&Service{})},
		{"whitespace owner", NewHandler(&Service{owner: " \t"})},
		{"padded owner", NewHandler(&Service{owner: " " + runtimeLabHandlerOwner + " "})},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			for _, endpoint := range runtimeLabOwnerEndpoints(test.handler) {
				t.Run(endpoint.name, func(t *testing.T) {
					response, followingCalls := runtimeLabOwnerRequest(endpoint, runtimeLabHandlerOwner, true, executionbroker.LocalSafeWorkerID)
					if response.Code != http.StatusServiceUnavailable || followingCalls != 0 {
						t.Fatalf("unconfigured owner status=%d following=%d; body=%s", response.Code, followingCalls, response.Body.String())
					}
					var envelope apierror.Envelope
					if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
						t.Fatal(err)
					}
					if envelope.Error == nil || envelope.Error.Code != apierror.CodeUnavailable {
						t.Fatalf("unexpected unavailable envelope: %+v", envelope)
					}
					if strings.Contains(response.Body.String(), runtimeLabHandlerOwner) {
						t.Fatal("unavailable scope exposed configured owner")
					}
				})
			}
		})
	}
}
