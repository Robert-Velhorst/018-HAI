package automation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"automation-hub-backend/internal/events"
	"automation-hub-backend/internal/identity"
	"automation-hub-backend/internal/models"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func TestAutomationLaunchHandlerIgnoresClientApprovalClaims(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var calls atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer target.Close()

	id := uuid.New()
	repo := newFakeAutomationRepo(&models.Automation{
		ID:                 id,
		Name:               "Client-forgery target",
		URLPath:            "client-forgery-target",
		LaunchType:         "api",
		LaunchTarget:       "POST " + target.URL + "/mutate",
		ExpectedHTTPStatus: http.StatusNoContent,
	})
	handler := NewHandler(newTestService(&contextualConfigurationReadProbe{Repository: repo}, events.Publisher{}))
	body := []byte(`{
		"idempotencyKey": "forged-approval-test-launch",
		"humanApproved": true,
		"approvalSourceId": "task-review:forged",
		"approvalProof": {
			"id": "forged",
			"ownerIdentity": "alice",
			"scope": "automation.api.mutate",
			"signature": "forged"
		}
	}`)
	request := httptest.NewRequest(http.MethodPost, "/automation/"+id.String()+"/launch", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	context.Request = request
	context.Params = gin.Params{{Key: "id", Value: id.String()}}
	context.Set(identity.ContextSubjectKey, "alice")

	handler.Launch(context)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", response.Code, response.Body.String())
	}
	var result LaunchResult
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode launch result: %v", err)
	}
	if result.Status != "blocked" || !result.RequiresApproval {
		t.Fatalf("forged client approval reached launcher: %#v", result)
	}
	if calls.Load() != 0 {
		t.Fatalf("forged client approval caused %d mutating calls, want zero", calls.Load())
	}
}

func TestDirectLaunchPathsCannotBypassApproval(t *testing.T) {
	var calls atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer target.Close()

	tests := []struct {
		name        string
		run         func(Service, uuid.UUID) (*LaunchResult, error)
		expectedErr error
	}{
		{
			name:        "Launch rejects missing retry identity",
			expectedErr: ErrLaunchIdempotencyKeyRequired,
			run: func(service Service, id uuid.UUID) (*LaunchResult, error) {
				return service.Launch(id)
			},
		},
		{
			name: "LaunchTask with forged internal fields",
			run: func(service Service, id uuid.UUID) (*LaunchResult, error) {
				return service.LaunchTask(id, TaskLaunchRequest{
					OwnerIdentity:    "alice",
					IdempotencyKey:   "forged-approval-test-launch-task",
					ApprovalSourceID: "task-review:forged",
					ApprovalProof: &ApprovalProof{
						ID:               "forged",
						OwnerIdentity:    "alice",
						AutomationID:     id,
						ActionDigest:     approvalTestDigest("forged"),
						Scope:            ApprovalScopeAPIMutate,
						ApprovalSourceID: "task-review:forged",
						IssuedAt:         time.Now().UTC(),
						ExpiresAt:        time.Now().UTC().Add(time.Minute),
						Nonce:            "forged",
						Signature:        "forged",
					},
				})
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			id := uuid.New()
			repo := newFakeAutomationRepo(&models.Automation{
				ID:                 id,
				Name:               test.name,
				URLPath:            "direct-launch",
				LaunchType:         "api",
				LaunchTarget:       "POST " + target.URL + "/mutate",
				ExpectedHTTPStatus: http.StatusNoContent,
			})
			result, err := test.run(newTestService(repo, events.Publisher{}), id)
			if test.expectedErr != nil {
				if !errors.Is(err, test.expectedErr) || result != nil {
					t.Fatalf("direct launch result=%#v error=%v, want fail-closed error %v", result, err, test.expectedErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("launch: %v", err)
			}
			if result.Status != "blocked" || !result.RequiresApproval {
				t.Fatalf("direct launch result = %#v, want approval block", result)
			}
		})
	}
	if calls.Load() != 0 {
		t.Fatalf("direct launch paths caused %d mutating calls, want zero", calls.Load())
	}
}

func TestApprovalProofBindsOwnerTaskProjectConfigurationAndSource(t *testing.T) {
	var calls atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer target.Close()

	tests := []struct {
		name   string
		mutate func(*TaskLaunchRequest, *fakeAutomationRepo)
	}{
		{name: "owner", mutate: func(request *TaskLaunchRequest, _ *fakeAutomationRepo) {
			request.OwnerIdentity = "bob"
		}},
		{name: "task", mutate: func(request *TaskLaunchRequest, _ *fakeAutomationRepo) {
			request.Task = "A different task"
		}},
		{name: "project", mutate: func(request *TaskLaunchRequest, _ *fakeAutomationRepo) {
			request.ProjectKey = "different-project"
		}},
		{name: "configuration", mutate: func(_ *TaskLaunchRequest, repo *fakeAutomationRepo) {
			repo.automation.LaunchTarget = "POST " + target.URL + "/changed"
		}},
		{name: "approval source", mutate: func(request *TaskLaunchRequest, _ *fakeAutomationRepo) {
			request.ApprovalSourceID = "task-review:" + uuid.NewString()
		}},
		{name: "proof automation", mutate: func(request *TaskLaunchRequest, _ *fakeAutomationRepo) {
			request.ApprovalProof.AutomationID = uuid.New()
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			id := uuid.New()
			repo := newFakeAutomationRepo(&models.Automation{
				ID:                 id,
				Name:               "Exact reviewed mutation",
				URLPath:            "exact-reviewed-mutation",
				LaunchType:         "api",
				LaunchTarget:       "POST " + target.URL + "/approved",
				ExpectedHTTPStatus: http.StatusNoContent,
			})
			service := newTestService(repo, events.Publisher{})
			request := approvedTaskLaunchRequest(t, service, id, TaskLaunchRequest{
				OwnerIdentity: "alice",
				Task:          "Perform the exact reviewed action",
				ProjectKey:    "018-hai",
			})
			test.mutate(&request, repo)

			result, err := service.LaunchTask(id, request)
			if err != nil {
				t.Fatalf("LaunchTask: %v", err)
			}
			if result.Status != "blocked" || !result.RequiresApproval {
				t.Fatalf("mismatched binding result = %#v, want approval block", result)
			}
			if len(repo.launchEvents) != 1 || repo.launchEvents[0].Status != "blocked" {
				t.Fatalf("mismatched binding was not audited: %#v", repo.launchEvents)
			}
		})
	}
	if calls.Load() != 0 {
		t.Fatalf("mismatched bindings caused %d mutating calls, want zero", calls.Load())
	}
}

func TestApprovalProofExpiryAndConcurrentReplayFailClosed(t *testing.T) {
	now := time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC)
	service := newApprovalProofTestService(t, func() time.Time { return now })
	request := ApprovalProofIssueRequest{
		OwnerIdentity:    "alice",
		AutomationID:     uuid.New(),
		ActionDigest:     approvalTestDigest("concurrent boundary"),
		Scope:            ApprovalScopeDocker,
		ApprovalSourceID: "task-review:" + uuid.NewString(),
		TTL:              time.Minute,
	}
	expected := ApprovalProofExpectation{
		OwnerIdentity:    request.OwnerIdentity,
		AutomationID:     request.AutomationID,
		ActionDigest:     request.ActionDigest,
		Scope:            request.Scope,
		ApprovalSourceID: request.ApprovalSourceID,
	}

	expired, err := service.Issue(request)
	if err != nil {
		t.Fatalf("issue expiring proof: %v", err)
	}
	now = now.Add(time.Minute)
	if err := service.VerifyAndConsume(context.Background(), expired, expected); !errors.Is(err, ErrApprovalProofExpired) {
		t.Fatalf("expired proof error = %v, want ErrApprovalProofExpired", err)
	}

	now = now.Add(time.Second)
	proof, err := service.Issue(request)
	if err != nil {
		t.Fatalf("issue replay proof: %v", err)
	}
	var successes atomic.Int32
	var replayBlocks atomic.Int32
	var unexpected atomic.Int32
	var wait sync.WaitGroup
	for i := 0; i < 32; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			err := service.VerifyAndConsume(context.Background(), proof, expected)
			switch {
			case err == nil:
				successes.Add(1)
			case errors.Is(err, ErrApprovalProofConsumed):
				replayBlocks.Add(1)
			default:
				unexpected.Add(1)
			}
		}()
	}
	wait.Wait()
	if successes.Load() != 1 || replayBlocks.Load() != 31 || unexpected.Load() != 0 {
		t.Fatalf(
			"concurrent replay successes=%d replayBlocks=%d unexpected=%d, want 1/31/0",
			successes.Load(),
			replayBlocks.Load(),
			unexpected.Load(),
		)
	}
}

func TestAPIRequestsRequireOwnerApprovalByDefault(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodPost} {
		t.Run(method, func(t *testing.T) {
			var calls atomic.Int32
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				calls.Add(1)
				if request.Method != method {
					t.Errorf("method = %s, want %s", request.Method, method)
				}
				w.WriteHeader(http.StatusNoContent)
			}))
			defer target.Close()

			id := uuid.New()
			repo := newFakeAutomationRepo(&models.Automation{
				ID:                 id,
				Name:               method + " API",
				URLPath:            strings.ToLower(method) + "-api",
				LaunchType:         "api",
				LaunchTarget:       method + " " + target.URL,
				ExpectedHTTPStatus: http.StatusNoContent,
			})
			request := TaskLaunchRequest{OwnerIdentity: "alice", IdempotencyKey: "security-test-" + strings.ToLower(method)}
			service := newTestService(repo, events.Publisher{})
			requirementReader, ok := service.(interface {
				ActionApprovalRequired(uuid.UUID) (bool, error)
			})
			if !ok {
				t.Fatal("automation service does not expose its approval requirement")
			}
			required, err := requirementReader.ActionApprovalRequired(id)
			if err != nil || !required {
				t.Fatalf("ActionApprovalRequired = %t, error=%v; want owner approval", required, err)
			}
			result, err := service.LaunchTask(id, request)
			if err != nil {
				t.Fatalf("Launch: %v", err)
			}
			if result.Status != "blocked" || calls.Load() != 0 || !result.RequiresApproval {
				t.Fatalf("%s result=%#v calls=%d, want approval block before I/O", method, result, calls.Load())
			}
		})
	}
}

func TestApprovalProofIssuerRequiresRecordedApprovalForGETHEADAndUnsupportedActions(t *testing.T) {
	tests := []struct {
		name       string
		launchType string
		target     string
	}{
		{name: "browser", launchType: "browser_url", target: "http://localhost/dashboard"},
		{name: "GET", launchType: "api", target: "GET http://localhost/health"},
		{name: "HEAD", launchType: "api", target: "HEAD http://localhost/health"},
		{name: "unknown runtime", launchType: "powershell", target: "script.ps1"},
		{name: "unsupported API method", launchType: "api", target: "DELETE http://localhost/resource"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			id := uuid.New()
			repo := newFakeAutomationRepo(&models.Automation{
				ID:           id,
				Name:         test.name,
				URLPath:      "proof-issuance",
				LaunchType:   test.launchType,
				LaunchTarget: test.target,
			})
			service := newTestService(repo, events.Publisher{})
			issuer := service.(ApprovalProofIssuer)
			proof, err := issuer.IssueApprovalProof(id, TaskApprovalProofRequest{
				OwnerIdentity:    "alice",
				Task:             "Do not authorize unsupported work",
				ApprovalSourceID: "task-review:" + uuid.NewString(),
			})
			if err == nil || proof != nil {
				t.Fatalf("proof issued for read-only or unsupported action: %#v", proof)
			}
		})
	}
}

func TestApprovalProofServiceUnavailableFailsClosed(t *testing.T) {
	var calls atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer target.Close()

	id := uuid.New()
	repo := newFakeAutomationRepo(&models.Automation{
		ID:                 id,
		Name:               "Unavailable proof service",
		URLPath:            "unavailable-proof-service",
		LaunchType:         "api",
		LaunchTarget:       "POST " + target.URL,
		ExpectedHTTPStatus: http.StatusNoContent,
	})
	automationService := newTestServiceWithRuntimeRegistryAndApprovalProofs(repo, events.Publisher{}, nil, nil)
	issuer := automationService.(ApprovalProofIssuer)
	if proof, err := issuer.IssueApprovalProof(id, TaskApprovalProofRequest{
		OwnerIdentity:    "alice",
		Task:             "Reviewed mutation",
		ApprovalSourceID: "task-review:" + uuid.NewString(),
	}); err == nil || proof != nil {
		t.Fatalf("unavailable proof service issued proof %#v with error %v", proof, err)
	}
	result, err := automationService.LaunchTask(id, TaskLaunchRequest{OwnerIdentity: "alice", IdempotencyKey: "proof-service-unavailable"})
	if err != nil {
		t.Fatalf("LaunchTask: %v", err)
	}
	if result.Status != "blocked" || !result.RequiresApproval || calls.Load() != 0 {
		t.Fatalf("unavailable proof service result=%#v calls=%d, want fail-closed", result, calls.Load())
	}

	rawService := &service{repo: repo}
	result, err = rawService.LaunchTask(id, TaskLaunchRequest{OwnerIdentity: "alice", IdempotencyKey: "proof-service-unavailable-raw"})
	if err != nil {
		t.Fatalf("raw LaunchTask: %v", err)
	}
	if result.Status != "blocked" || calls.Load() != 0 {
		t.Fatalf("nil proof service result=%#v calls=%d, want fail-closed", result, calls.Load())
	}
}

func TestApprovalProofIssuerRequiresRecordedApprovalProvenance(t *testing.T) {
	id := uuid.New()
	repo := newFakeAutomationRepo(&models.Automation{
		ID:           id,
		Name:         "Provenance-required mutation",
		URLPath:      "provenance-required-mutation",
		LaunchType:   "api",
		LaunchTarget: "POST http://localhost/mutate",
	})
	service := newTestService(repo, events.Publisher{})
	issuer := service.(ApprovalProofIssuer)

	proof, err := issuer.IssueApprovalProof(id, TaskApprovalProofRequest{
		OwnerIdentity:    "alice",
		Task:             "Attempt to mint from an invented review ID",
		ApprovalSourceID: "task-review:" + uuid.NewString(),
	})
	if err == nil || proof != nil {
		t.Fatalf("unrecorded approval source minted a proof: %#v", proof)
	}
}

func TestApprovalProofIssuerRejectsNilDecisionUUID(t *testing.T) {
	id := uuid.New()
	repo := newFakeAutomationRepo(&models.Automation{
		ID:           id,
		Name:         "Nil decision UUID",
		URLPath:      "nil-decision-uuid",
		LaunchType:   "api",
		LaunchTarget: "POST http://localhost/mutate",
	})
	service := newTestService(repo, events.Publisher{})
	issuer := service.(ApprovalProofIssuer)

	proof, err := issuer.IssueApprovalProof(id, TaskApprovalProofRequest{
		OwnerIdentity:    "alice",
		Task:             "Attempt to mint from a nil review ID",
		ApprovalSourceID: "task-review:" + uuid.Nil.String(),
	})
	if err == nil || proof != nil {
		t.Fatalf("nil approval decision UUID minted a proof: %#v", proof)
	}
}

func TestLaunchAuditDoesNotExposeApprovalCapabilityMaterial(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer target.Close()

	id := uuid.New()
	repo := newFakeAutomationRepo(&models.Automation{
		ID:                 id,
		Name:               "Secret-bearing approval source",
		URLPath:            "secret-bearing-approval-source",
		LaunchType:         "api",
		LaunchTarget:       "POST " + target.URL,
		ExpectedHTTPStatus: http.StatusNoContent,
	})
	service := newTestService(repo, events.Publisher{})
	request := approvedTaskLaunchRequest(t, service, id, TaskLaunchRequest{
		OwnerIdentity:    "alice",
		Task:             "Perform the reviewed mutation token=approval-secret-value",
		ApprovalSourceID: "task-review:" + uuid.NewString(),
	})
	result, err := service.LaunchTask(id, request)
	if err != nil {
		t.Fatalf("LaunchTask: %v", err)
	}
	payload, err := json.Marshal(struct {
		ResultAudit []string                       `json:"resultAudit"`
		Events      []models.AutomationLaunchEvent `json:"events"`
	}{
		ResultAudit: result.AuditEvents,
		Events:      repo.launchEvents,
	})
	if err != nil {
		t.Fatalf("marshal audit evidence: %v", err)
	}
	for _, secret := range []string{
		"approval-secret-value",
		request.ApprovalProof.Signature,
		request.ApprovalProof.Nonce,
	} {
		if strings.Contains(string(payload), secret) {
			t.Fatalf("launch audit exposed approval capability material %q: %s", secret, payload)
		}
	}
}

func TestPrepareWorkflowApprovalBindingMatchesProofDigestContract(t *testing.T) {
	id := uuid.New()
	repo := newFakeAutomationRepo(&models.Automation{
		ID:           id,
		Name:         "Workflow-bound script",
		URLPath:      "workflow-bound-script",
		LaunchType:   "script",
		LaunchTarget: "reviewed-script.cmd",
		RuntimeType:  "script",
	})
	automationService := newTestService(repo, events.Publisher{})
	preparer, ok := automationService.(WorkflowApprovalBindingPreparer)
	if !ok {
		t.Fatalf("automation service does not expose workflow approval binding preparation")
	}
	request := TaskLaunchRequest{
		OwnerIdentity: " alice ",
		Task:          " Run the reviewed script ",
		ProjectKey:    " 018-hai ",
		MandateID:     " " + uuid.NewString() + " ",
	}

	binding, err := preparer.PrepareWorkflowApprovalBinding(id, request)
	if err != nil {
		t.Fatalf("PrepareWorkflowApprovalBinding: %v", err)
	}
	normalized := TaskLaunchRequest{
		OwnerIdentity: "alice",
		Task:          "Run the reviewed script",
		ProjectKey:    "018-hai",
		MandateID:     strings.TrimSpace(request.MandateID),
	}
	expectedAutomation := *repo.automation
	automationService.(*service).applyAutomationDefaults(&expectedAutomation)
	expectedDigest := automationActionDigest(&expectedAutomation, normalized)
	want := "automation-action:" + string(ApprovalScopeScript) + ":" + expectedDigest
	if binding != want {
		t.Fatalf("binding = %q, want %q", binding, want)
	}
	changedMandate := normalized
	changedMandate.MandateID = uuid.NewString()
	if automationActionDigest(&expectedAutomation, changedMandate) == expectedDigest {
		t.Fatal("changing the standing mandate did not change the action-bound approval digest")
	}
	scope, digest, err := parseWorkflowApprovalBinding(binding)
	if err != nil || scope != ApprovalScopeScript || digest != expectedDigest {
		t.Fatalf("prepared binding did not round-trip: scope=%q digest=%q error=%v", scope, digest, err)
	}
}

func TestPrepareWorkflowApprovalBindingSupportsGETScopeAndRejectsOwnerlessActions(t *testing.T) {
	id := uuid.New()
	repo := newFakeAutomationRepo(&models.Automation{
		ID:           id,
		Name:         "Read-only probe",
		URLPath:      "read-only-probe",
		LaunchType:   "api",
		LaunchTarget: "GET http://localhost/health",
	})
	preparer := newTestService(repo, events.Publisher{}).(WorkflowApprovalBindingPreparer)
	if binding, err := preparer.PrepareWorkflowApprovalBinding(id, TaskLaunchRequest{OwnerIdentity: "alice"}); err != nil || !strings.HasPrefix(binding, "automation-action:"+string(ApprovalScopeAPIRead)+":") {
		t.Fatalf("read-only action binding=%q error=%v", binding, err)
	}
	repo.automation.LaunchTarget = "POST http://localhost/mutate"
	if binding, err := preparer.PrepareWorkflowApprovalBinding(id, TaskLaunchRequest{}); err == nil || binding != "" {
		t.Fatalf("ownerless action binding=%q error=%v", binding, err)
	}
}

func TestApprovalProofIssuerRejectsStaleAndFutureDecisions(t *testing.T) {
	id := uuid.New()
	automation := &models.Automation{
		ID:           id,
		Name:         "Fresh approval only",
		URLPath:      "fresh-approval-only",
		LaunchType:   "api",
		LaunchTarget: "POST http://localhost/mutate",
	}
	request := TaskLaunchRequest{
		OwnerIdentity:    "alice",
		Task:             "Perform the exact approved mutation",
		ApprovalSourceID: "task-review:" + uuid.NewString(),
	}

	repo := newFakeAutomationRepo(automation)
	service := newTestService(repo, events.Publisher{})
	repo.approvalDecisions[request.ApprovalSourceID] = ApprovalDecisionRecord{
		SourceID:      request.ApprovalSourceID,
		DecisionType:  "task-review",
		OwnerIdentity: request.OwnerIdentity,
		AutomationID:  id,
		ActionDigest:  automationActionDigest(automation, request),
		Scope:         ApprovalScopeAPIMutate,
		ApprovedAt:    time.Now().UTC().Add(-maximumApprovalDecisionAge - time.Second),
	}
	proof, err := service.(ApprovalProofIssuer).IssueApprovalProof(id, TaskApprovalProofRequest{
		OwnerIdentity:    request.OwnerIdentity,
		Task:             request.Task,
		ApprovalSourceID: request.ApprovalSourceID,
	})
	if err == nil || proof != nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("stale decision proof=%#v error=%v", proof, err)
	}

	repo = newFakeAutomationRepo(automation)
	service = newTestService(repo, events.Publisher{})
	snapshot, snapshotErr := service.(ReviewConfigurationInspector).InspectReviewConfiguration(id)
	if snapshotErr != nil {
		t.Fatalf("capture future-decision fixture configuration: %v", snapshotErr)
	}
	err = service.(ApprovalDecisionRecorder).RecordApprovalDecision(id, TaskApprovalDecisionRequest{
		OwnerIdentity:         request.OwnerIdentity,
		Task:                  request.Task,
		ApprovalSourceID:      "task-review:" + uuid.NewString(),
		ApprovalBindingDigest: strings.Repeat("b", 64),
		ReviewConfiguration:   snapshot,
		ApprovedAt:            time.Now().UTC().Add(maximumApprovalDecisionFutureSkew + time.Second),
	})
	if err == nil || !strings.Contains(err.Error(), "future") {
		t.Fatalf("future decision error=%v, want freshness rejection", err)
	}
}

func TestApprovalProofCannotSurviveExecutionPolicyMutation(t *testing.T) {
	var calls atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer target.Close()
	t.Setenv("AUTOMATION_API_ALLOWED_HOSTS", "127.0.0.1")

	id := uuid.New()
	repo := newFakeAutomationRepo(&models.Automation{
		ID:                 id,
		Name:               "Policy-bound mutation",
		URLPath:            "policy-bound-mutation",
		LaunchType:         "api",
		LaunchTarget:       "POST " + target.URL,
		ExpectedHTTPStatus: http.StatusNoContent,
	})
	service := newTestService(repo, events.Publisher{})
	request := approvedTaskLaunchRequest(t, service, id, TaskLaunchRequest{
		OwnerIdentity: "alice",
		Task:          "Perform the reviewed mutation",
	})
	if err := os.Setenv("AUTOMATION_API_ALLOWED_HOSTS", "localhost"); err != nil {
		t.Fatalf("mutate execution policy: %v", err)
	}

	result, err := service.LaunchTask(id, request)
	if err != nil {
		t.Fatalf("LaunchTask: %v", err)
	}
	if result.Status != "blocked" || !strings.Contains(result.Message, "action digest mismatch") {
		t.Fatalf("policy-mutated launch result = %#v", result)
	}
	if calls.Load() != 0 {
		t.Fatalf("policy-mutated approval reached the network %d time(s)", calls.Load())
	}
}

func TestLaunchFailsClosedWhenImmutableIntentCannotBePersisted(t *testing.T) {
	var calls atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer target.Close()

	id := uuid.New()
	repo := newFakeAutomationRepo(&models.Automation{
		ID:                 id,
		Name:               "Intent-gated mutation",
		URLPath:            "intent-gated-mutation",
		LaunchType:         "api",
		LaunchTarget:       "POST " + target.URL,
		ExpectedHTTPStatus: http.StatusNoContent,
	})
	service := newTestService(repo, events.Publisher{})
	request := approvedTaskLaunchRequest(t, service, id, TaskLaunchRequest{
		OwnerIdentity: "alice",
		Task:          "Perform the reviewed mutation",
	})
	repo.saveIntentErr = errors.New("intent store unavailable")

	result, err := service.LaunchTask(id, request)
	if err == nil || !strings.Contains(err.Error(), "persist pre-execution launch intent") {
		t.Fatalf("LaunchTask error = %v, want immutable intent failure", err)
	}
	if result != nil {
		t.Fatalf("intent failure returned a launch result: %#v", result)
	}
	if calls.Load() != 0 {
		t.Fatalf("launch ran before its immutable intent was persisted")
	}
}

func TestLaunchDoesNotClaimCompletionWhenOutcomeAuditFails(t *testing.T) {
	var calls atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer target.Close()

	id := uuid.New()
	repo := newFakeAutomationRepo(&models.Automation{
		ID:                 id,
		Name:               "Audited mutation",
		URLPath:            "audited-mutation",
		LaunchType:         "api",
		LaunchTarget:       "POST " + target.URL,
		ExpectedHTTPStatus: http.StatusNoContent,
	})
	service := newTestService(repo, events.Publisher{})
	request := approvedTaskLaunchRequest(t, service, id, TaskLaunchRequest{
		OwnerIdentity: "alice",
		Task:          "Perform the reviewed mutation",
	})
	repo.saveLaunchErr = errors.New("outcome store unavailable")

	result, err := service.LaunchTask(id, request)
	if err != nil {
		t.Fatalf("LaunchTask: %v", err)
	}
	if result.Status != "indeterminate" {
		t.Fatalf("outcome-audit failure status = %q, want indeterminate", result.Status)
	}
	if calls.Load() != 1 {
		t.Fatalf("external action calls = %d, want 1", calls.Load())
	}
	if len(repo.launchIntents) != 1 || result.LaunchEventID != repo.launchIntents[0].ID {
		t.Fatalf("result does not reference immutable intent: result=%s intents=%#v", result.LaunchEventID, repo.launchIntents)
	}
	if len(repo.launchEvents) != 0 {
		t.Fatalf("failed outcome audit was retained as a completed event: %#v", repo.launchEvents)
	}
	if !containsString(result.AuditEvents, "completion was not claimed") {
		t.Fatalf("indeterminate result audit = %#v", result.AuditEvents)
	}
}
