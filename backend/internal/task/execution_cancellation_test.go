package task

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/automation"
	"automation-hub-backend/internal/identity"
	"automation-hub-backend/internal/llm"
	"automation-hub-backend/internal/verification"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func TestCancelledTaskHTTPDoesNotStartOperation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, mode := range []string{"plan", "run"} {
		t.Run(mode, func(t *testing.T) {
			service := &capturingTaskService{}
			handler := NewHandler(service)
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			response := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(response)
			c.Request = httptest.NewRequest(http.MethodPost, "/task/"+mode,
				strings.NewReader(`{"request":"Summarize my project"}`)).WithContext(ctx)
			c.Request.Header.Set("Content-Type", "application/json")
			c.Set(identity.ContextSubjectKey, "alice")
			if mode == "plan" {
				handler.Plan(c)
			} else {
				handler.Run(c)
			}
			if service.planRequest.Request != "" || service.runRequest.Request != "" || response.Code != http.StatusRequestTimeout {
				t.Fatalf("cancelled request started work: status=%d plan=%q run=%q", response.Code,
					service.planRequest.Request, service.runRequest.Request)
			}
		})
	}
}

func TestCancelledTaskReviewHTTPDoesNotRecordDecision(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := &capturingTaskService{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	response := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(response)
	c.Request = httptest.NewRequest(http.MethodPost, "/task/review-queue/item/resolve",
		strings.NewReader(`{"approved":true}`)).WithContext(ctx)
	c.Params = gin.Params{{Key: "id", Value: "item"}}
	c.Set(identity.ContextSubjectKey, "alice")
	NewHandler(svc).ResolveReviewItem(c)
	if svc.resolveOwner != "" || response.Code != http.StatusRequestTimeout {
		t.Fatalf("cancelled approval entered resolution: owner=%q status=%d", svc.resolveOwner, response.Code)
	}
}

func TestTaskExecutionContextIsTrustedAndDoesNotChangeIntentDigest(t *testing.T) {
	request := IntakeRequest{OwnerIdentity: "alice", Request: "Summarize project notes"}
	before, err := ReviewRequestDigest("alice", request)
	if err != nil {
		t.Fatal(err)
	}
	request.ExecutionContext = context.Background()
	after, err := ReviewRequestDigest("alice", request)
	if err != nil || before != after {
		t.Fatalf("lifetime changed intent digest: %v", err)
	}
	encoded, err := json.Marshal(request)
	if err != nil || strings.Contains(string(encoded), "ExecutionContext") || strings.Contains(string(encoded), "executionContext") {
		t.Fatalf("internal context serialized: %s %v", encoded, err)
	}
	var decoded IntakeRequest
	if err := json.Unmarshal([]byte(`{"request":"Notes","executionContext":{},"ExecutionContext":{}}`), &decoded); err != nil || decoded.ExecutionContext != nil {
		t.Fatalf("JSON could set caller context: %#v %v", decoded, err)
	}
}

func TestTaskHTTPForwardsTrustedLifetime(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	for _, mode := range []string{"plan", "run"} {
		svc := &capturingTaskService{}
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/task/"+mode, strings.NewReader(`{"request":"Notes","executionContext":{}}`)).WithContext(ctx)
		c.Request.Header.Set("Content-Type", "application/json")
		c.Set(identity.ContextSubjectKey, "alice")
		if mode == "plan" {
			NewHandler(svc).Plan(c)
		} else {
			NewHandler(svc).Run(c)
		}
		if svc.planRequest.ExecutionContext != ctx && svc.runRequest.ExecutionContext != ctx {
			t.Fatal("HTTP lifetime was dropped")
		}
	}
}

func TestTaskReviewHTTPForwardsLifetimeAndRefusesLegacyResolution(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	for _, legacy := range []bool{false, true} {
		svc := &capturingTaskService{}
		var exposed Service = svc
		if legacy {
			exposed = &struct {
				Service
				OwnerScopedService
			}{svc, svc}
		}
		response := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(response)
		c.Request = httptest.NewRequest(http.MethodPost, "/task/review-queue/item/resolve", strings.NewReader(`{"approved":false}`)).WithContext(ctx)
		c.Params = gin.Params{{Key: "id", Value: "item"}}
		c.Set(identity.ContextSubjectKey, "alice")
		NewHandler(exposed).ResolveReviewItem(c)
		if legacy {
			if response.Code != http.StatusServiceUnavailable || svc.resolveOwner != "" {
				t.Fatalf("context-free resolution admitted: %d owner=%q", response.Code, svc.resolveOwner)
			}
		} else if response.Code != http.StatusOK || svc.resolveContext != ctx || svc.resolveOwner != "alice" {
			t.Fatal("review request lost its trusted owner/lifetime")
		}
	}
}

type cancellationReviewRepository struct {
	TaskStateRepository
	afterDecision context.CancelFunc
	outcomeError  error
}

func (r *cancellationReviewRepository) WithTaskStateContext(ctx context.Context) (TaskStateRepository, error) {
	return r, ctx.Err()
}

func (r *cancellationReviewRepository) ResolveReviewItem(owner, id string, decision ReviewResolution) (*PersistedReviewResolution, error) {
	result, err := r.TaskStateRepository.ResolveReviewItem(owner, id, decision)
	if err == nil && r.afterDecision != nil {
		r.afterDecision()
	}
	return result, err
}

func (r *cancellationReviewRepository) MarkReviewOutcome(owner, id string, outcome ReviewOutcome) (*ReviewQueueItem, error) {
	if r.outcomeError != nil {
		return nil, r.outcomeError
	}
	return r.TaskStateRepository.MarkReviewOutcome(owner, id, outcome)
}

func TestApprovedTaskCancellationPreservesDecisionAndRuntimeEvidence(t *testing.T) {
	for _, boundary := range []string{"before_decision", "after_decision", "during_runtime", "outcome_failure"} {
		t.Run(boundary, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			memory := NewMemoryTaskStateRepository()
			repo := &cancellationReviewRepository{TaskStateRepository: memory}
			executor := &cancellingTaskExecutor{ctx: ctx, cancel: cancel, tool: completedToolResult()}
			svc := newDurableTaskTestService(t, repo, executor)
			plan, err := svc.Run(IntakeRequest{OwnerIdentity: "alice", Request: "Delete account data by running a local script",
				ProjectKey: "018-HAI", AutomationID: executor.tool.AutomationID, ExecuteAllowed: true})
			if err != nil || plan == nil || plan.ReviewQueueItem == nil || executor.calls != 0 {
				t.Fatalf("failed to prepare real approval boundary: plan=%#v err=%v calls=%d", plan, err, executor.calls)
			}
			id := plan.ReviewQueueItem.ID
			outcomeErr := errors.New("review outcome persistence failed")
			switch boundary {
			case "before_decision":
				cancel()
			case "after_decision":
				repo.afterDecision = cancel
			case "outcome_failure":
				repo.outcomeError = outcomeErr
			}
			result, executionErr := svc.(ContextualReviewService).ResolveReviewItemForOwnerContext(ctx, "alice", id, ApprovalDecision{Approved: true})
			if !errors.Is(executionErr, context.Canceled) {
				t.Fatalf("lost cancellation identity: result=%#v err=%v", result, executionErr)
			}
			if boundary != "before_decision" && !errors.Is(executionErr, ErrTaskOperationNeedsReview) {
				t.Fatalf("post-approval cancellation did not require reconciliation: %v", executionErr)
			}
			decisions, err := memory.ListReviewDecisions("alice", id, 10)
			if err != nil {
				t.Fatal(err)
			}
			stored, err := memory.FindReviewItem("alice", id)
			if err != nil || stored.Request.ExecutionContext != nil || stored.Status == "completed" {
				t.Fatalf("invalid stored cancellation: %#v err=%v", stored, err)
			}
			if boundary == "before_decision" {
				if result != nil || len(decisions) != 0 || executor.calls != 0 || stored.Status != "open" {
					t.Fatal("cancelled approval created authority or execution")
				}
				return
			}
			if len(decisions) != 1 || decisions[0].Decision != "approved" || result == nil {
				t.Fatal("acknowledged immutable approval was erased")
			}
			if boundary == "after_decision" {
				if executor.calls != 0 || stored.Status != "needs_review" {
					t.Fatal("post-approval cancellation entered runtime or claimed completion")
				}
				return
			}
			if executor.calls != 1 || result.Plan == nil || result.Plan.CompletionStatus != "review_required" ||
				result.Plan.ExecutionResult.ToolExecution.LaunchEventID != executor.tool.LaunchEventID || result.Plan.RetryPolicy.RetryAvailable {
				t.Fatal("cancelled reviewed execution discarded receipt or claimed success/retry")
			}
			if boundary == "outcome_failure" && stored.Status != "approved" {
				t.Fatal("failed outcome write was represented as acknowledged")
			}
			if boundary != "outcome_failure" && (stored.Status != "needs_review" || stored.TaskID != result.Plan.ID) {
				t.Fatal("review was not linked to the retained execution result")
			}
			if boundary == "outcome_failure" {
				if !errors.Is(executionErr, outcomeErr) {
					t.Fatalf("lost storage failure identity: %v", executionErr)
				}
			}
		})
	}
}

func TestAutomationExecutorForwardsCancellationAndDoesNotDispatchCancelledRequest(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	launcher := &fakeAutomationLauncher{result: &automation.LaunchResult{AutomationID: uuid.New(), Status: "failed"}}
	executor := NewAutomationToolExecutor(launcher)
	request := ToolExecutionRequest{ExecutionContext: ctx, OwnerIdentity: "alice", TaskID: "cancel-task", AutomationID: launcher.result.AutomationID.String()}
	_, _ = executor.Execute(request)
	if launcher.request.ExecutionContext != ctx || launcher.launchCalls != 1 {
		t.Fatal("runtime lifetime was dropped")
	}
	cancel()
	_, err := executor.Execute(request)
	if !errors.Is(err, context.Canceled) || !IsToolFailureBeforeDispatch(err) || launcher.launchCalls != 1 || launcher.issueCalls != 0 {
		t.Fatalf("cancelled request dispatched or lost safe-before-dispatch marker: %v calls=%d", err, launcher.launchCalls)
	}
}

type cancellingTaskExecutor struct {
	ctx    context.Context
	cancel context.CancelFunc
	calls  int
	tool   *ToolExecutionResult
}

func (e *cancellingTaskExecutor) Execute(request ToolExecutionRequest) (*ToolExecutionResult, error) {
	e.calls++
	if request.ExecutionContext != e.ctx {
		return nil, errors.New("task dropped execution context")
	}
	e.cancel()
	return e.tool, nil
}

func TestTaskCancellationRetainsRuntimeReceiptAndDoesNotReplayEffects(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	executor := &cancellingTaskExecutor{ctx: ctx, cancel: cancel, tool: completedToolResult()}
	repository := NewMemoryTaskStateRepository()
	svc := newDurableTaskTestService(t, repository, executor)
	request := IntakeRequest{ExecutionContext: ctx, OwnerIdentity: "alice", IdempotencyKey: "cancelled-runtime-once",
		Request: "Run local script tests for the project", AutomationID: executor.tool.AutomationID, ExecuteAllowed: true}
	plan, err := svc.Run(request)
	if !errors.Is(err, context.Canceled) || !errors.Is(err, ErrTaskOperationNeedsReview) || plan == nil || executor.calls != 1 {
		t.Fatalf("cancellation result: err=%v plan=%#v calls=%d", err, plan, executor.calls)
	}
	if plan.CompletionStatus != "review_required" || plan.ValidationResult.Passed || plan.RetryPolicy.RetryAvailable ||
		plan.ExecutionResult == nil || plan.ExecutionResult.ToolExecution == nil || plan.ExecutionResult.ToolExecution.LaunchEventID != executor.tool.LaunchEventID {
		t.Fatal("cancellation discarded receipt or claimed success/retry")
	}
	durable, err := repository.FindCompletionPlan("alice", plan.ID)
	if err != nil || durable.ExecutionResult.ToolExecution.LaunchEventID != executor.tool.LaunchEventID {
		t.Fatalf("receipt not retained: %v", err)
	}
	request.ExecutionContext = context.Background()
	if _, err := svc.Run(request); !errors.Is(err, ErrTaskOperationNeedsReview) || executor.calls != 1 {
		t.Fatalf("cancelled operation auto-replayed: %v calls=%d", err, executor.calls)
	}
	items, err := repository.ListReviewItems("alice", 50)
	if err != nil || len(items) != 1 || items[0].Request.ExecutionContext != nil {
		t.Fatalf("review lost or retains dead context: %v", err)
	}
}

func TestEnteredTaskModelCallReceivesCancellationWithoutVerificationFallback(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	providerCancelled := make(chan bool, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload any
		_ = json.NewDecoder(r.Body).Decode(&payload)
		cancel()
		select {
		case <-r.Context().Done():
			providerCancelled <- true
		case <-time.After(2 * time.Second):
			providerCancelled <- false
		}
	}))
	defer server.Close()
	providers, _ := json.Marshal([]llm.Provider{{ID: "ollama", Enabled: true, Local: true, EndpointURL: server.URL,
		Models: []llm.Model{{ID: "task-cancel-model", Tier: llm.TierLocal, Enabled: true, Capabilities: []string{"general", "planning", "summarization"}, MaxDifficulty: 5, MaxReasoning: "very_high"}}}})
	t.Setenv("LLM_PROVIDERS_JSON", string(providers))
	t.Setenv("LLM_POLICY_JSON", "")
	t.Setenv("LLM_MODEL_MAINTENANCE_ENABLED", "false")
	t.Setenv("OLLAMA_BASE_URL", "")
	model, err := llm.NewServiceFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	model = model.WithFinalEffectAuthorization(llm.FinalEffectAuthorizerFunc(func(context.Context, llm.FinalEffectAuthorizationRequest) error { return nil }),
		llm.EmergencyStopEvaluatorFunc(func(context.Context) (llm.EmergencyStopState, error) { return llm.EmergencyStopState{}, nil }))
	verifier := &sequencedVerificationService{}
	svc := &service{llmService: model, verificationService: verifier}
	plan := &CompletionPlan{ID: uuid.NewString(), OwnerIdentity: "alice", ProjectKey: "018-HAI", Request: "Summarize my notes", RealGoal: "Summarize my notes",
		RiskAssessment: RiskAssessment{Level: "low", AllowedNow: true}, ModelDecision: llm.RouteDecision{SelectedProviderID: "ollama", SelectedModelID: "task-cancel-model"}}
	result := svc.executeAllowedSteps(plan, IntakeRequest{ExecutionContext: ctx, OwnerIdentity: "alice", Request: plan.Request})
	if result.BlockedReason == "" || result.VerificationStatus != verification.StatusNeedsReview || verifier.calls != 0 {
		t.Fatalf("cancelled generation became verification fallback: result=%#v calls=%d", result, verifier.calls)
	}
	select {
	case cancelled := <-providerCancelled:
		if !cancelled {
			t.Fatal("HTTP model request did not receive cancellation")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("model provider was not entered")
	}
}
