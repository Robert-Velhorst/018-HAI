package automation

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"automation-hub-backend/internal/agentruntime"
	"automation-hub-backend/internal/events"
	"automation-hub-backend/internal/executionauth"
	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/safety"

	"github.com/google/uuid"
)

type allowingExecutionAuthorizer struct{}

func (allowingExecutionAuthorizer) AuthorizeAndConsume(
	_ context.Context,
	request executionauth.Request,
	_ string,
	_ string,
) (executionauth.Receipt, error) {
	return executionauth.Receipt{
		ID:            uuid.New(),
		Outcome:       executionauth.OutcomeAuthorized,
		OwnerIdentity: request.OwnerIdentity,
		Evidence: executionauth.DecisionEvidence{
			Constitution: executionauth.ConstitutionEvidence{
				Source: "test-constitution:v1",
			},
		},
	}, nil
}

type recordingExecutionAuthorizer struct {
	calls    atomic.Int32
	onCall   func()
	decision executionauth.Receipt
	err      error
	request  executionauth.Request
}

func (a *recordingExecutionAuthorizer) AuthorizeAndConsume(
	_ context.Context,
	request executionauth.Request,
	_ string,
	_ string,
) (executionauth.Receipt, error) {
	a.calls.Add(1)
	a.request = request
	if a.onCall != nil {
		a.onCall()
	}
	if a.err != nil {
		return executionauth.Receipt{}, a.err
	}
	receipt := a.decision
	if receipt.ID == uuid.Nil {
		receipt.ID = uuid.New()
	}
	if receipt.Outcome == "" {
		receipt.Outcome = executionauth.OutcomeAuthorized
	}
	if receipt.OwnerIdentity == "" {
		receipt.OwnerIdentity = request.OwnerIdentity
	}
	if receipt.Evidence.Constitution.Source == "" {
		receipt.Evidence.Constitution.Source = "test-constitution:v1"
	}
	return receipt, nil
}

type permissiveExecutionConstitution struct{}

func (permissiveExecutionConstitution) EvaluateExecutionPolicy(
	_ context.Context,
	_ string,
	_ []string,
	_ int,
) (executionauth.ConstitutionDecision, error) {
	return executionauth.ConstitutionDecision{
		ID:               "builtin-robert-constitution-v1",
		Version:          1,
		Source:           "builtin-robert-constitution-v1:v1",
		Digest:           strings.Repeat("c", 64),
		AuthorityCeiling: 10,
	}, nil
}

type exactTestApprovalResolver struct{}

func (exactTestApprovalResolver) Resolve(
	_ context.Context,
	owner string,
	sourceID string,
	bindingDigest string,
) (executionauth.ResolvedApproval, error) {
	decisionID := strings.TrimPrefix(sourceID, "task-review:")
	if strings.TrimSpace(owner) == "" ||
		decisionID == sourceID ||
		uuid.Validate(decisionID) != nil ||
		len(bindingDigest) != 64 {
		return executionauth.ResolvedApproval{}, executionauth.ErrNotFound
	}
	now := time.Now().UTC()
	return executionauth.ResolvedApproval{
		SourceID:       sourceID,
		DecisionID:     decisionID,
		DecisionDigest: strings.Repeat("d", 64),
		BindingDigest:  bindingDigest,
		ApprovedBy:     owner,
		ApprovedAt:     now.Add(-time.Second),
		ExpiresAt:      now.Add(5 * time.Minute),
	}, nil
}

// inMemoryTestExecutionAuthorizer exercises the runtime proof chain with a
// deterministic resolver. Production task-review approvals use the PostgreSQL
// transaction path; this test adapter only covers the in-memory unit fixture.
type inMemoryTestExecutionAuthorizer struct {
	service    *executionauth.Service
	repository *executionauth.MemoryRepository
}

func (a inMemoryTestExecutionAuthorizer) AuthorizeAndConsume(
	ctx context.Context,
	request executionauth.Request,
	consumer string,
	executionTarget string,
) (executionauth.Receipt, error) {
	if a.service == nil || a.repository == nil {
		return executionauth.Receipt{}, executionauth.ErrPolicyUnavailable
	}
	receipt, err := a.service.Authorize(ctx, request)
	if err != nil {
		return receipt, err
	}
	if receipt.Outcome != executionauth.OutcomeAuthorized {
		return receipt, executionauth.ErrNotAuthorized
	}
	err = a.repository.Consume(ctx, executionauth.Consumption{
		ReceiptID:       receipt.ID,
		OwnerIdentity:   receipt.OwnerIdentity,
		Consumer:        consumer,
		ExecutionTarget: executionTarget,
		ReceiptDigest:   receipt.DecisionDigest,
		ConsumedAt:      time.Now().UTC(),
	})
	return receipt, err
}

func newTestServiceWithAuthorizedRuntime(
	t *testing.T,
	repo Repository,
	publisher events.Publisher,
	adapter agentruntime.Adapter,
) Service {
	t.Helper()
	authorizationRepository := executionauth.NewMemoryRepository()
	finalEffects, err := executionauth.NewFinalEffectBridge(
		authorizationRepository,
		nil,
	)
	if err != nil {
		t.Fatalf("NewFinalEffectBridge: %v", err)
	}
	authorizationService, err := executionauth.NewService(
		authorizationRepository,
		permissiveExecutionConstitution{},
		nil,
		nil,
		exactTestApprovalResolver{},
		nil,
	)
	if err != nil {
		t.Fatalf("executionauth.NewService: %v", err)
	}
	authorizationService.WithEmergencyStopEvaluator(
		func() executionauth.EmergencyStopEvidence {
			return executionauth.EmergencyStopEvidence{Source: "automation-test"}
		},
	)
	registry := agentruntime.NewRegistryWithFinalEffectVerifier(
		finalEffects,
		adapter,
	)
	return NewServiceWithRuntimeRegistryApprovalProofsExecutionAuthorizationAndFinalEffects(
		repo,
		publisher,
		registry,
		newUnitTestApprovalProofService(),
		inMemoryTestExecutionAuthorizer{
			service:    authorizationService,
			repository: authorizationRepository,
		},
		finalEffects,
	)
}

func newTestService(repo Repository, publisher events.Publisher) Service {
	return newTestServiceWithRuntimeRegistry(
		repo,
		publisher,
		agentruntime.DefaultRegistry(),
	)
}

func newTestServiceWithRuntimeRegistry(
	repo Repository,
	publisher events.Publisher,
	registry *agentruntime.Registry,
) Service {
	return newTestServiceWithRuntimeRegistryAndApprovalProofs(
		repo,
		publisher,
		registry,
		newUnitTestApprovalProofService(),
	)
}

func newUnitTestApprovalProofService() ApprovalProofService {
	secret := []byte("0123456789abcdef0123456789abcdef")
	service, err := NewInMemoryApprovalProofService(secret, time.Now)
	if err != nil {
		panic(err)
	}
	return service
}

func newTestServiceWithRuntimeRegistryAndApprovalProofs(
	repo Repository,
	publisher events.Publisher,
	registry *agentruntime.Registry,
	proofs ApprovalProofService,
) Service {
	return NewServiceWithRuntimeRegistryApprovalProofsAndExecutionAuthorization(
		repo,
		publisher,
		registry,
		proofs,
		allowingExecutionAuthorizer{},
	)
}

func TestExternalLaunchFailsClosedWithoutUnifiedAuthorization(t *testing.T) {
	serverCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter,
		_ *http.Request,
	) {
		serverCalls++
		writer.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	repository := newFakeAutomationRepo(&models.Automation{
		ID:           uuid.New(),
		Name:         "read probe",
		LaunchType:   "api",
		LaunchTarget: "GET " + server.URL,
	})
	service := NewServiceWithRuntimeRegistryApprovalProofsAndExecutionAuthorization(
		repository,
		events.Publisher{},
		agentruntime.DefaultRegistry(),
		newUnitTestApprovalProofService(),
		nil,
	)

	request := approvedTaskLaunchRequest(t, service, repository.automation.ID, TaskLaunchRequest{OwnerIdentity: "alice"})
	result, err := service.LaunchTask(
		repository.automation.ID,
		request,
	)
	if err != nil {
		t.Fatalf("LaunchTask: %v", err)
	}
	if result.Status != "blocked" ||
		!strings.Contains(result.Message, "unified execution authorization service is unavailable") {
		t.Fatalf("result = %#v; want the request to reach and fail closed at unified execution authorization", result)
	}
	if serverCalls != 0 {
		t.Fatalf("network calls = %d, want 0", serverCalls)
	}
}

func TestAPILaunchValidatesTargetBeforeConsumingAuthorization(t *testing.T) {
	authorizer := &recordingExecutionAuthorizer{}
	service := &service{executionAuth: authorizer}
	result := service.executeAPILaunch(
		&models.Automation{
			ID:           uuid.New(),
			LaunchType:   "api",
			LaunchTarget: "POST relative-target",
		},
		TaskLaunchRequest{OwnerIdentity: "alice"},
		uuid.New(),
		time.Now().UTC(),
		nil,
	)
	if result.Status != "blocked" {
		t.Fatalf("result = %#v, want blocked", result)
	}
	if authorizer.calls.Load() != 0 {
		t.Fatalf("authorization calls = %d, want zero for invalid target", authorizer.calls.Load())
	}
}

func TestAPILaunchRechecksEmergencyStopAfterAuthorization(t *testing.T) {
	var stopped atomic.Bool
	restore := safety.SetEmergencyStopProvider(safety.EmergencyStopProviderFunc(
		func() (bool, string, error) {
			return stopped.Load(), "operator stopped execution", nil
		},
	))
	defer restore()

	var networkCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter,
		_ *http.Request,
	) {
		networkCalls.Add(1)
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	authorizer := &recordingExecutionAuthorizer{
		onCall: func() {
			stopped.Store(true)
		},
	}
	service := &service{executionAuth: authorizer}
	result := service.executeAPILaunch(
		&models.Automation{
			ID:                 uuid.New(),
			LaunchType:         "api",
			LaunchTarget:       "POST " + server.URL,
			ExpectedHTTPStatus: http.StatusNoContent,
		},
		TaskLaunchRequest{OwnerIdentity: "alice"},
		uuid.New(),
		time.Now().UTC(),
		nil,
	)
	if result.Status != "blocked" ||
		!strings.Contains(result.Message, "operator stopped execution") {
		t.Fatalf("result = %#v, want emergency-stop block", result)
	}
	if authorizer.calls.Load() != 1 {
		t.Fatalf("authorization calls = %d, want one", authorizer.calls.Load())
	}
	if networkCalls.Load() != 0 {
		t.Fatalf("network calls = %d, want zero after emergency stop", networkCalls.Load())
	}
}

func TestAPILaunchAuthorizesExactlyOnceBeforeNetworkEffect(t *testing.T) {
	var authorized atomic.Bool
	var networkCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter,
		_ *http.Request,
	) {
		if !authorized.Load() {
			t.Error("network effect occurred before authorization")
		}
		networkCalls.Add(1)
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	authorizer := &recordingExecutionAuthorizer{
		onCall: func() {
			authorized.Store(true)
		},
	}
	service := &service{executionAuth: authorizer}
	governance := executionauth.GovernanceEvidence{
		TaskPlanID:                       "plan-1",
		TaskPlanDigest:                   strings.Repeat("a", 64),
		FrameworkSelectionID:             "selection-1",
		FrameworkCatalogVersion:          "framework-catalog-v1",
		FrameworkCatalogDigest:           strings.Repeat("b", 64),
		FrameworkPreferenceDigest:        strings.Repeat("c", 64),
		FrameworkConstitutionDigest:      strings.Repeat("d", 64),
		FrameworkOperatingContractDigest: strings.Repeat("e", 64),
	}
	mandateID := "case-mandate-1"
	automation := &models.Automation{
		ID:                 uuid.New(),
		LaunchType:         "api",
		LaunchTarget:       "POST " + server.URL,
		ExpectedHTTPStatus: http.StatusNoContent,
	}
	result := service.executeAPILaunch(
		automation,
		launchBindingDispatchFixture(automation, TaskLaunchRequest{OwnerIdentity: "alice", MandateID: mandateID, Governance: governance}),
		uuid.New(),
		time.Now().UTC(),
		nil,
	)
	if result.Status != "completed" {
		t.Fatalf("result = %#v, want completed", result)
	}
	if authorizer.calls.Load() != 1 {
		t.Fatalf("authorization calls = %d, want one", authorizer.calls.Load())
	}
	if networkCalls.Load() != 1 {
		t.Fatalf("network calls = %d, want one", networkCalls.Load())
	}
	if authorizer.request.Governance.TaskPlanDigest != governance.TaskPlanDigest ||
		authorizer.request.Governance.FrameworkOperatingContractDigest !=
			governance.FrameworkOperatingContractDigest {
		t.Fatalf("authorization governance = %#v", authorizer.request.Governance)
	}
	if authorizer.request.MandateID != mandateID {
		t.Fatalf("authorization mandate = %q, want %q", authorizer.request.MandateID, mandateID)
	}
}

func TestApprovedGETAPILaunchUsesCaseApprovedAutonomy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	authorizer := &recordingExecutionAuthorizer{}
	service := &service{executionAuth: authorizer}
	automation := &models.Automation{
		ID: uuid.New(), LaunchType: "api", LaunchTarget: "GET " + server.URL,
		ExpectedHTTPStatus: http.StatusNoContent,
	}
	request := launchBindingDispatchFixture(automation, TaskLaunchRequest{
		OwnerIdentity: "alice", ApprovalSourceID: "workflow-decision:" + uuid.NewString(),
	})
	digest := request.ApprovalBindingDigest
	result := service.executeAPILaunch(
		automation,
		request,
		uuid.New(), time.Now().UTC(), nil,
	)
	if result.Status != "completed" {
		t.Fatalf("result = %#v, want completed", result)
	}
	if authorizer.request.RequestedAutonomy != 6 || authorizer.request.RequiredAuthority != 6 {
		t.Fatalf("approved read-only authorization = authority %d autonomy %d, want 6/6", authorizer.request.RequiredAuthority, authorizer.request.RequestedAutonomy)
	}
	if authorizer.request.ApprovalBindingDigest != digest || authorizer.request.ApprovalSourceID == "" {
		t.Fatalf("approved read-only authorization lost decision evidence: %#v", authorizer.request)
	}
}
