package workflow

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/identity"
	"automation-hub-backend/internal/models"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func TestWorkflowHandlerRedactsInternalServerErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const secret = "postgres://admin:super-secret@db.internal/hai?token=api-secret"
	service := &failingWorkflowHandlerService{
		Service: NewService(newFakeWorkflowRepo()),
		err:     errors.New("database query failed: " + secret),
	}
	handler := NewHandler(service)

	tests := []struct {
		name          string
		method        string
		path          string
		expectedError string
		invoke        func(*gin.Context)
	}{
		{
			name:          "items",
			method:        http.MethodGet,
			path:          "/workflow/items",
			expectedError: workflowItemsUnavailableMessage,
			invoke:        handler.Items,
		},
		{
			name:          "approval items",
			method:        http.MethodGet,
			path:          "/workflow/approval-items",
			expectedError: workflowApprovalsUnavailableMessage,
			invoke:        handler.ApprovalItems,
		},
		{
			name:          "dashboard",
			method:        http.MethodGet,
			path:          "/workflow/dashboard",
			expectedError: workflowDashboardUnavailableMessage,
			invoke:        handler.Dashboard,
		},
		{
			name:          "run due",
			method:        http.MethodPost,
			path:          "/workflow/run-due",
			expectedError: workflowRunFailedMessage,
			invoke:        handler.RunDue,
		},
		{
			name:          "recover stale claims",
			method:        http.MethodPost,
			path:          "/workflow/recover-stale-claims",
			expectedError: workflowRecoveryFailedMessage,
			invoke:        handler.RecoverStaleClaims,
		},
		{
			name:          "run due open loops",
			method:        http.MethodPost,
			path:          "/workflow/run-due-open-loops",
			expectedError: workflowOpenLoopRunFailedMessage,
			invoke:        handler.RunDueOpenLoops,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(test.method, test.path, bytes.NewBufferString(`{"limit":5}`))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			context, _ := gin.CreateTestContext(response)
			context.Request = request
			context.Set(identity.ContextSubjectKey, "verified-operator")

			test.invoke(context)

			if response.Code != http.StatusInternalServerError {
				t.Fatalf("status = %d, want 500: %s", response.Code, response.Body.String())
			}
			var payload struct {
				Error string `json:"error"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if payload.Error != test.expectedError {
				t.Fatalf("error = %q, want %q", payload.Error, test.expectedError)
			}
			for _, sensitive := range []string{secret, "super-secret", "api-secret", "db.internal"} {
				if strings.Contains(response.Body.String(), sensitive) {
					t.Fatalf("response exposed sensitive error fragment %q: %s", sensitive, response.Body.String())
				}
			}
		})
	}
}

func TestWorkflowHandlerKeepsUsefulBadRequestValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewHandler(NewService(newFakeWorkflowRepo()))
	request := httptest.NewRequest(http.MethodPost, "/workflow/intake", bytes.NewBufferString(`{"input":`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	context.Request = request
	context.Set(identity.ContextSubjectKey, "verified-operator")

	handler.Intake(context)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "unexpected EOF") {
		t.Fatalf("bad-request validation detail was lost: %s", response.Body.String())
	}
}

func TestWorkflowActionHandlersRejectMalformedChunkedBodiesBeforeServiceCalls(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name   string
		path   string
		invoke func(*Handler, *gin.Context)
		calls  func(*countingWorkflowHandlerService) int
	}{
		{name: "run due", path: "/workflow/run-due", invoke: func(handler *Handler, c *gin.Context) { handler.RunDue(c) }, calls: func(service *countingWorkflowHandlerService) int { return service.runDueCalls }},
		{name: "recover stale claims", path: "/workflow/recover-stale-claims", invoke: func(handler *Handler, c *gin.Context) { handler.RecoverStaleClaims(c) }, calls: func(service *countingWorkflowHandlerService) int { return service.recoverCalls }},
		{name: "run due open loops", path: "/workflow/run-due-open-loops", invoke: func(handler *Handler, c *gin.Context) { handler.RunDueOpenLoops(c) }, calls: func(service *countingWorkflowHandlerService) int { return service.openLoopCalls }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := &countingWorkflowHandlerService{Service: NewService(newFakeWorkflowRepo())}
			handler := NewHandler(service)
			request := httptest.NewRequest(http.MethodPost, test.path, bytes.NewBufferString(`{"limit":`))
			request.Header.Set("Content-Type", "application/json")
			request.ContentLength = -1
			response := httptest.NewRecorder()
			context, _ := gin.CreateTestContext(response)
			context.Request = request
			context.Set(identity.ContextSubjectKey, "verified-operator")

			test.invoke(handler, context)

			if response.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400: %s", response.Code, response.Body.String())
			}
			if got := test.calls(service); got != 0 {
				t.Fatalf("malformed request called service %d times", got)
			}
		})
	}
}

func TestReminderProposalHandlerIsBoundedOwnerScopedAndNonExecuting(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newFakeWorkflowRepo()
	service := NewService(repo)
	due := time.Now().UTC().Add(24 * time.Hour).Format(time.RFC3339)
	if _, err := service.Intake(IntakeRequest{
		OwnerIdentity: "verified-operator",
		Input:         "Calendar event: Review\nStart: " + due,
		SourceType:    "calendar",
		SourceID:      "review-1",
	}); err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(service)

	request := httptest.NewRequest(http.MethodGet, "/workflow/reminder-proposals?horizonHours=168&limit=10", nil)
	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	context.Request = request
	context.Set(identity.ContextSubjectKey, "verified-operator")
	handler.ReminderProposals(context)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"authority":"reminder_proposal_only"`) ||
		!strings.Contains(response.Body.String(), `"canExecute":false`) ||
		!strings.Contains(response.Body.String(), `"revalidationRequired":true`) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}

	invalidRequest := httptest.NewRequest(http.MethodGet, "/workflow/reminder-proposals?horizonHours=721", nil)
	invalidResponse := httptest.NewRecorder()
	invalidContext, _ := gin.CreateTestContext(invalidResponse)
	invalidContext.Request = invalidRequest
	invalidContext.Set(identity.ContextSubjectKey, "verified-operator")
	handler.ReminderProposals(invalidContext)
	if invalidResponse.Code != http.StatusBadRequest {
		t.Fatalf("invalid horizon status=%d body=%s", invalidResponse.Code, invalidResponse.Body.String())
	}
}

func TestResolveApprovalHandlerUsesVerifiedActor(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newFakeWorkflowRepo()
	service := NewService(repo)
	record, err := service.Intake(IntakeRequest{OwnerIdentity: "verified-operator", Input: "Draft and send a legal reply to the lawyer."})
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}

	handler := NewHandler(service)
	body, _ := json.Marshal(ApprovalResolutionRequest{
		Approved: true,
		Actor:    "forged-client-actor",
	})
	request := httptest.NewRequest(http.MethodPost, "/workflow/"+record.Item.ID.String()+"/approval", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	context.Params = gin.Params{{Key: "id", Value: record.Item.ID.String()}}
	context.Request = request
	context.Set(identity.ContextSubjectKey, "verified-operator")

	handler.ResolveApproval(context)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", response.Code, response.Body.String())
	}
	updated, err := service.Get(record.Item.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(updated.Transitions) == 0 || updated.Transitions[0].Actor != "verified-operator" {
		t.Fatalf("approval transition actor = %#v, want verified identity", updated.Transitions)
	}
	if updated.Transitions[0].Actor == "forged-client-actor" {
		t.Fatal("client actor label was used for approval provenance")
	}
}

func TestWorkflowRoutesRequireVerifiedOwner(t *testing.T) {
	gin.SetMode(gin.TestMode)

	unauthenticated := gin.New()
	unauthenticatedRoutes := unauthenticated.Group("/workflow")
	unauthenticatedRoutes.Use(RequireAuthenticatedOwner())
	unauthenticatedRoutes.GET("/overview", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	unauthenticatedRecorder := httptest.NewRecorder()
	unauthenticated.ServeHTTP(unauthenticatedRecorder, httptest.NewRequest(http.MethodGet, "/workflow/overview", nil))
	if unauthenticatedRecorder.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated workflow route status = %d, want %d: %s", unauthenticatedRecorder.Code, http.StatusUnauthorized, unauthenticatedRecorder.Body.String())
	}

	authenticated := gin.New()
	authenticated.Use(func(c *gin.Context) {
		c.Set(identity.ContextSubjectKey, "alice")
		c.Next()
	})
	authenticatedRoutes := authenticated.Group("/workflow")
	authenticatedRoutes.Use(RequireAuthenticatedOwner())
	authenticatedRoutes.GET("/overview", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	authenticatedRecorder := httptest.NewRecorder()
	authenticated.ServeHTTP(authenticatedRecorder, httptest.NewRequest(http.MethodGet, "/workflow/overview", nil))
	if authenticatedRecorder.Code != http.StatusNoContent {
		t.Fatalf("authenticated workflow route status = %d, want %d: %s", authenticatedRecorder.Code, http.StatusNoContent, authenticatedRecorder.Body.String())
	}
}

func TestTransitionHandlerCannotApproveWorkflow(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newFakeWorkflowRepo()
	service := NewService(repo)
	record, err := service.Intake(IntakeRequest{
		OwnerIdentity: "verified-operator",
		Input:         "Draft and send a legal reply to the lawyer.",
		ProjectKey:    "legal-case",
	})
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}
	if record.Item.CurrentState != StateNeedsApproval {
		t.Fatalf("initial state = %q, want needs approval", record.Item.CurrentState)
	}

	handler := NewHandler(service)
	body, _ := json.Marshal(TransitionRequest{
		TargetState: StateReady,
		Approved:    true,
		Message:     "forged approval through generic transition",
	})
	request := httptest.NewRequest(http.MethodPost, "/workflow/"+record.Item.ID.String()+"/transition", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	context.Params = gin.Params{{Key: "id", Value: record.Item.ID.String()}}
	context.Request = request
	context.Set(identity.ContextSubjectKey, "verified-operator")

	handler.Transition(context)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", response.Code, response.Body.String())
	}
	updated, err := service.Get(record.Item.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if updated.Item.ApprovalStatus == "approved" || updated.Item.CurrentState == StateReady {
		t.Fatalf("generic transition established approval: %#v", updated.Item)
	}
}

func TestWorkflowHandlerRejectsOwnerlessLegacyWorkflowMutation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newFakeWorkflowRepo()
	service := NewService(repo)
	record, err := service.Intake(IntakeRequest{Input: "Draft and send a legal reply to the lawyer."})
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}
	if record.Item.OwnerIdentity != "" {
		t.Fatalf("legacy workflow owner = %q, want empty", record.Item.OwnerIdentity)
	}

	handler := NewHandler(service)
	body, _ := json.Marshal(ApprovalResolutionRequest{Approved: true})
	request := httptest.NewRequest(http.MethodPost, "/workflow/"+record.Item.ID.String()+"/approval", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	context.Params = gin.Params{{Key: "id", Value: record.Item.ID.String()}}
	context.Request = request
	context.Set(identity.ContextSubjectKey, "alice")

	handler.ResolveApproval(context)

	if response.Code != http.StatusNotFound {
		t.Fatalf("ownerless workflow mutation status = %d, want 404: %s", response.Code, response.Body.String())
	}
	updated, err := service.Get(record.Item.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if updated.Item.ApprovalStatus == "approved" || updated.Item.CurrentState == StateReady {
		t.Fatalf("ownerless workflow was mutated through authenticated handler: %#v", updated.Item)
	}
}

func TestWorkflowHandlerRejectsCrossOwnerReadAndApproval(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newFakeWorkflowRepo()
	service := NewService(repo)
	record, err := service.Intake(IntakeRequest{
		OwnerIdentity: "bob",
		Input:         "Draft and send a legal reply to Bob's lawyer.",
	})
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}
	handler := NewHandler(service)

	getRequest := httptest.NewRequest(http.MethodGet, "/workflow/"+record.Item.ID.String(), nil)
	getResponse := httptest.NewRecorder()
	getContext, _ := gin.CreateTestContext(getResponse)
	getContext.Params = gin.Params{{Key: "id", Value: record.Item.ID.String()}}
	getContext.Request = getRequest
	getContext.Set(identity.ContextSubjectKey, "alice")
	handler.Get(getContext)
	if getResponse.Code != http.StatusNotFound {
		t.Fatalf("cross-owner get status = %d, want 404: %s", getResponse.Code, getResponse.Body.String())
	}

	body, _ := json.Marshal(ApprovalResolutionRequest{Approved: true})
	approvalRequest := httptest.NewRequest(http.MethodPost, "/workflow/"+record.Item.ID.String()+"/approval", bytes.NewReader(body))
	approvalRequest.Header.Set("Content-Type", "application/json")
	approvalResponse := httptest.NewRecorder()
	approvalContext, _ := gin.CreateTestContext(approvalResponse)
	approvalContext.Params = gin.Params{{Key: "id", Value: record.Item.ID.String()}}
	approvalContext.Request = approvalRequest
	approvalContext.Set(identity.ContextSubjectKey, "alice")
	handler.ResolveApproval(approvalContext)
	if approvalResponse.Code != http.StatusNotFound {
		t.Fatalf("cross-owner approval status = %d, want 404: %s", approvalResponse.Code, approvalResponse.Body.String())
	}
	updated, err := service.Get(record.Item.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if updated.Item.ApprovalStatus == "approved" {
		t.Fatalf("cross-owner approval mutated workflow: %#v", updated.Item)
	}
}

func TestRunDueHandlerRunsOnlyVerifiedOwnerWorkflow(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newFakeWorkflowRepo()
	runner := &fakeTaskRunner{result: &TaskRunResult{PlanID: "handler-owner-plan", CompletionStatus: "validated", VerificationStatus: "verified", Passed: true}}
	service := NewServiceWithTaskRunner(repo, runner)
	if _, err := service.Intake(IntakeRequest{OwnerIdentity: "alice", Input: "Create Alice's low-risk admin checklist."}); err != nil {
		t.Fatalf("alice Intake: %v", err)
	}
	bob, err := service.Intake(IntakeRequest{OwnerIdentity: "bob", Input: "Create Bob's low-risk admin checklist."})
	if err != nil {
		t.Fatalf("bob Intake: %v", err)
	}
	handler := NewHandler(service)
	request := httptest.NewRequest(http.MethodPost, "/workflow/run-due", bytes.NewBufferString(`{"limit":5}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	context.Request = request
	context.Set(identity.ContextSubjectKey, "alice")

	handler.RunDue(context)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", response.Code, response.Body.String())
	}
	if len(runner.requests) != 1 || runner.requests[0].OwnerIdentity != "alice" {
		t.Fatalf("handler executed wrong workflows: %#v", runner.requests)
	}
	if repo.items[bob.Item.ID].CurrentState != StateReady {
		t.Fatalf("handler executed Bob workflow for Alice: %#v", repo.items[bob.Item.ID])
	}
}

func TestRunDueHandlerHonorsCancelledRequestContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newFakeWorkflowRepo()
	runner := &fakeTaskRunner{result: &TaskRunResult{PlanID: "cancelled-handler-plan", CompletionStatus: "validated", VerificationStatus: "verified", Passed: true}}
	service := NewServiceWithTaskRunner(repo, runner)
	if _, err := service.Intake(IntakeRequest{OwnerIdentity: "alice", Input: "Create Alice's low-risk cancellation test checklist."}); err != nil {
		t.Fatalf("Intake: %v", err)
	}
	handler := NewHandler(service)
	requestContext, cancel := context.WithCancel(context.Background())
	cancel()
	request := httptest.NewRequest(http.MethodPost, "/workflow/run-due", bytes.NewBufferString(`{"limit":5}`)).WithContext(requestContext)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	ginContext, _ := gin.CreateTestContext(response)
	ginContext.Request = request
	ginContext.Set(identity.ContextSubjectKey, "alice")

	handler.RunDue(ginContext)

	if len(runner.requests) != 0 {
		t.Fatalf("cancelled handler started work: %#v", runner.requests)
	}
	if response.Body.Len() != 0 {
		t.Fatalf("cancelled handler wrote response body: %s", response.Body.String())
	}
}

func TestRunOneHandlerRunsOnlyTheSelectedVerifiedOwnerWorkflow(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newFakeWorkflowRepo()
	runner := &fakeTaskRunner{result: &TaskRunResult{PlanID: "handler-exact-plan", CompletionStatus: "validated", VerificationStatus: "verified", Passed: true}}
	service := NewServiceWithTaskRunner(repo, runner)
	selected, err := service.Intake(IntakeRequest{OwnerIdentity: "alice", Input: "Create Alice's selected low-risk admin checklist."})
	if err != nil {
		t.Fatalf("selected Intake: %v", err)
	}
	other, err := service.Intake(IntakeRequest{OwnerIdentity: "alice", Input: "Create Alice's other low-risk admin checklist."})
	if err != nil {
		t.Fatalf("other Intake: %v", err)
	}
	handler := NewHandler(service)
	request := httptest.NewRequest(http.MethodPost, "/workflow/"+selected.Item.ID.String()+"/run", bytes.NewReader(nil))
	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	context.Params = gin.Params{{Key: "id", Value: selected.Item.ID.String()}}
	context.Request = request
	context.Set(identity.ContextSubjectKey, "alice")

	handler.RunOne(context)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", response.Code, response.Body.String())
	}
	if len(runner.requests) != 1 || runner.requests[0].WorkflowID != selected.Item.ID.String() {
		t.Fatalf("handler executed wrong workflow: %#v", runner.requests)
	}
	if repo.items[other.Item.ID].CurrentState != StateReady {
		t.Fatalf("handler executed unselected workflow: %#v", repo.items[other.Item.ID])
	}

	foreignRequest := httptest.NewRequest(http.MethodPost, "/workflow/"+other.Item.ID.String()+"/run", bytes.NewReader(nil))
	foreignResponse := httptest.NewRecorder()
	foreignContext, _ := gin.CreateTestContext(foreignResponse)
	foreignContext.Params = gin.Params{{Key: "id", Value: other.Item.ID.String()}}
	foreignContext.Request = foreignRequest
	foreignContext.Set(identity.ContextSubjectKey, "bob")
	handler.RunOne(foreignContext)
	if foreignResponse.Code != http.StatusNotFound || len(runner.requests) != 1 {
		t.Fatalf("foreign exact run = %d %s, requests=%#v", foreignResponse.Code, foreignResponse.Body.String(), runner.requests)
	}
}

func TestIntakeHandlerRoutesLegacyRequestThroughPursuitGateway(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newFakeWorkflowRepo()
	service := NewService(repo)
	router := &capturingPursuitIntakeRouter{record: &WorkflowRecord{Item: models.WorkflowItem{ID: uuid.New(), Title: "Governed intake"}}}
	handler := NewHandlerWithPursuitIntakeRouter(service, router)
	body, _ := json.Marshal(IntakeRequest{Input: "Prepare the evidence bundle", ProjectKey: "vivare"})
	request := httptest.NewRequest(http.MethodPost, "/workflow/intake", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	context.Request = request
	context.Set(identity.ContextSubjectKey, "verified-operator")

	handler.Intake(context)

	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", response.Code, response.Body.String())
	}
	if router.calls != 1 || router.request.Actor != "verified-operator" {
		t.Fatalf("pursuit intake request = %#v", router.request)
	}
	if router.request.SourceType != "workflow_api" || router.request.SourceID == "" || router.request.SourceURI == "" {
		t.Fatalf("legacy request was not normalized with deterministic provenance: %#v", router.request)
	}
}

func TestIntakeHandlerReportsPursuitCandidatePending(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewHandlerWithPursuitIntakeRouter(NewService(newFakeWorkflowRepo()), &capturingPursuitIntakeRouter{
		err: candidatePendingHandlerError{pursuitID: "candidate-123", message: "review before workflow creation"},
	})
	body, _ := json.Marshal(IntakeRequest{Input: "Review this imported objective"})
	request := httptest.NewRequest(http.MethodPost, "/workflow/intake", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	context.Request = request
	context.Set(identity.ContextSubjectKey, "verified-operator")

	handler.Intake(context)

	if response.Code != http.StatusAccepted || !strings.Contains(response.Body.String(), "pursuit_candidate_pending") || !strings.Contains(response.Body.String(), "candidate-123") {
		t.Fatalf("candidate response = %d %s", response.Code, response.Body.String())
	}
}

type capturingPursuitIntakeRouter struct {
	calls   int
	request IntakeRequest
	record  *WorkflowRecord
	err     error
}

func (r *capturingPursuitIntakeRouter) RouteWorkflowIntake(request IntakeRequest) (*WorkflowRecord, error) {
	r.calls++
	r.request = request
	if r.err != nil {
		return nil, r.err
	}
	return r.record, nil
}

type candidatePendingHandlerError struct {
	pursuitID string
	message   string
}

func (e candidatePendingHandlerError) Error() string                  { return e.message }
func (e candidatePendingHandlerError) CandidatePending() bool         { return true }
func (e candidatePendingHandlerError) CandidatePursuitID() string     { return e.pursuitID }
func (e candidatePendingHandlerError) CandidateIntakeMessage() string { return e.message }

type failingWorkflowHandlerService struct {
	Service
	err error
}

type countingWorkflowHandlerService struct {
	Service
	runDueCalls   int
	recoverCalls  int
	openLoopCalls int
}

func (s *countingWorkflowHandlerService) RunDueForOwner(string, RunDueRequest) (*WorkflowRunSummary, error) {
	s.runDueCalls++
	return &WorkflowRunSummary{}, nil
}

func (s *countingWorkflowHandlerService) RecoverStaleClaimsForOwner(string, RunDueRequest) (*ClaimRecoverySummary, error) {
	s.recoverCalls++
	return &ClaimRecoverySummary{}, nil
}

func (s *countingWorkflowHandlerService) RunDueOpenLoopsForOwner(string, RunDueRequest) (*OpenLoopRunSummary, error) {
	s.openLoopCalls++
	return &OpenLoopRunSummary{}, nil
}

func (s *failingWorkflowHandlerService) ItemsForOwner(string, bool) ([]models.WorkflowItem, error) {
	return nil, s.err
}

func (s *failingWorkflowHandlerService) ApprovalItemsForOwner(string) ([]models.WorkflowItem, error) {
	return nil, s.err
}

func (s *failingWorkflowHandlerService) DashboardForOwner(string) (*WorkflowDashboard, error) {
	return nil, s.err
}

func (s *failingWorkflowHandlerService) RunDueForOwner(string, RunDueRequest) (*WorkflowRunSummary, error) {
	return nil, s.err
}

func (s *failingWorkflowHandlerService) RunOneForOwner(string, uuid.UUID) (*WorkflowRunResult, error) {
	return nil, s.err
}

func (s *failingWorkflowHandlerService) RecoverStaleClaimsForOwner(string, RunDueRequest) (*ClaimRecoverySummary, error) {
	return nil, s.err
}

func (s *failingWorkflowHandlerService) RunDueOpenLoopsForOwner(string, RunDueRequest) (*OpenLoopRunSummary, error) {
	return nil, s.err
}
