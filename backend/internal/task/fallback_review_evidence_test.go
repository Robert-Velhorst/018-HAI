package task

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"automation-hub-backend/internal/frameworkregistry"
	"automation-hub-backend/internal/llm"
	"automation-hub-backend/internal/verification"

	"github.com/google/uuid"
)

type fallbackReviewGenerationHarness struct {
	llmService        *llm.Service
	generateCalls     atomic.Int64
	frameworkSelector FrameworkSelector
}

// This trusted unit fixture supplies health evidence, not participant authority.
// The real selector and evidence requirements remain in use.
type fallbackReviewHealthSelector struct {
	FrameworkSelector
	checkedAt time.Time
}

func (s fallbackReviewHealthSelector) PlanSelection(request frameworkregistry.SelectionRequest) (*frameworkregistry.SelectionDecision, error) {
	decision, err := s.FrameworkSelector.PlanSelection(request)
	if err != nil || decision == nil {
		return decision, err
	}
	for i := range decision.AgentCards {
		card := &decision.AgentCards[i]
		if card.ID == "hai_task_engine" && card.Verified && !card.Revoked {
			card.HealthStatus = "healthy"
			at := s.checkedAt
			card.LastVerifiedAt = &at
		}
	}
	return decision, nil
}

func newFallbackReviewGenerationHarness(t *testing.T) *fallbackReviewGenerationHarness {
	t.Helper()
	harness := &fallbackReviewGenerationHarness{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" && r.Method == http.MethodGet {
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.URL.Path != "/api/generate" {
			t.Errorf("unexpected fake provider path: %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		harness.generateCalls.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]string{"response": "generated answer"})
	}))
	t.Cleanup(server.Close)
	response, err := server.Client().Get(server.URL + "/health")
	if err != nil {
		t.Fatalf("probe owned synthetic health fixture: %v", err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("owned synthetic health fixture status: %d", response.StatusCode)
	}
	harness.frameworkSelector = fallbackReviewHealthSelector{FrameworkSelector: defaultFrameworkSelector(), checkedAt: time.Now().UTC()}
	models := []llm.Model{}
	for _, id := range []string{"fallback-review-primary", "fallback-review-secondary"} {
		models = append(models, llm.Model{
			ID: id, Name: id, Tier: llm.TierLocal, Enabled: true,
			Capabilities:  []string{"general", "coding", "planning", "summarization", "extraction", "verification"},
			MaxDifficulty: 5, MaxReasoning: "very_high",
		})
	}
	providers, err := json.Marshal([]llm.Provider{{
		ID: "ollama", Name: "Fake local provider", Enabled: true, Local: true, EndpointURL: server.URL, Models: models,
	}})
	if err != nil {
		t.Fatalf("marshal fake provider: %v", err)
	}
	t.Setenv("LLM_PROVIDERS_JSON", string(providers))
	t.Setenv("LLM_POLICY_JSON", "")
	t.Setenv("LLM_MODEL_MAINTENANCE_ENABLED", "false")
	llmService, err := llm.NewServiceFromEnv()
	if err != nil {
		t.Fatalf("NewServiceFromEnv: %v", err)
	}
	harness.llmService = llmService.WithFinalEffectAuthorization(
		llm.FinalEffectAuthorizerFunc(func(context.Context, llm.FinalEffectAuthorizationRequest) error { return nil }),
		llm.EmergencyStopEvaluatorFunc(func(context.Context) (llm.EmergencyStopState, error) {
			return llm.EmergencyStopState{}, nil
		}),
	)
	return harness
}

func TestFallbackReservationDenialsRetainPreviousExecutionEvidence(t *testing.T) {
	for _, denial := range []string{"invalid pursuit", "unavailable manager", "reservation denied"} {
		for _, evidence := range []string{"completed receipt", "uncertain receipt", "completed actions without receipt", "uncertain actions without receipt", "no previous execution"} {
			t.Run(denial+"/"+evidence, func(t *testing.T) {
				executor := &fakeToolExecutor{result: completedToolResult()}
				recorder := &fakePursuitAttemptRecorder{reserveErr: errors.New("remaining effort ceiling exhausted")}
				engine := &service{toolExecutor: executor, pursuitAttempts: recorder}
				plan := &CompletionPlan{
					ID: "fallback-reservation-test", OwnerIdentity: "alice", PursuitID: uuid.NewString(),
					Request: "Run local script tests", Intake: IntakeAnalysis{NeedsTools: true},
					RiskAssessment: RiskAssessment{Level: "low", AllowedNow: true},
				}
				prior := &ExecutionResult{
					ToolExecution: executor.result,
					Actions: []ExecutedAction{{
						Name: "automation.launch", Status: "completed", Input: executor.result.AutomationID,
						Output: executor.result.Output, StartedAt: executor.result.ExecutedAt, EndedAt: executor.result.ExecutedAt,
					}},
				}
				switch evidence {
				case "uncertain receipt":
					prior.ToolExecution.Status = "uncertain"
					prior.Actions[0].Status = "uncertain"
				case "completed actions without receipt":
					prior.ToolExecution = nil
				case "uncertain actions without receipt":
					prior.ToolExecution = nil
					prior.Actions[0].Status = "uncertain"
				case "no previous execution":
					prior = nil
				}
				plan.ExecutionResult = prior
				var priorActions []ExecutedAction
				if prior != nil {
					priorActions = append([]ExecutedAction{}, prior.Actions...)
				}
				wantReason := "pursuit resource reservation blocked execution: remaining effort ceiling exhausted"
				switch denial {
				case "invalid pursuit":
					plan.PursuitID = "invalid-pursuit"
					wantReason = "pursuit resource reservation received an invalid pursuit id"
				case "unavailable manager":
					engine.pursuitAttempts = nil
					wantReason = "pursuit resource reservation boundary is unavailable"
				}

				result := engine.executeWithPursuitReservation(plan, IntakeRequest{ExecuteAllowed: true}, 2)
				if result.BlockedReason != wantReason || result.VerificationStatus != verification.StatusNeedsReview {
					t.Fatalf("denial lost its exact review reason: %#v", result)
				}
				if executor.calls != 0 || len(recorder.settlements) != 0 {
					t.Fatalf("denied reservation dispatched or settled: calls=%d settlements=%v", executor.calls, recorder.settlements)
				}
				if len(result.Actions) != len(priorActions)+1 || result.Actions[len(priorActions)].Status != "blocked" {
					t.Fatalf("prior actions or current denial missing: %#v", result.Actions)
				}
				if prior == nil {
					if result.ToolExecution != nil {
						t.Fatal("denial invented a runtime receipt")
					}
					return
				}
				if result.ToolExecution != prior.ToolExecution || !reflect.DeepEqual(result.Actions[:len(priorActions)], priorActions) ||
					!reflect.DeepEqual(prior.Actions, priorActions) || !strings.Contains(result.Output, "prior execution evidence") {
					t.Fatalf("prior execution was lost, mutated, or represented as effect-free: %#v", result)
				}
				result.Actions[0].Status = "changed copy"
				if !reflect.DeepEqual(prior.Actions, priorActions) {
					t.Fatal("blocked result aliases the prior action slice")
				}
			})
		}
	}
}

type fallbackReviewReservationRecorder struct {
	fakePursuitAttemptRecorder
	denyFallback bool
}

func (r *fallbackReviewReservationRecorder) ReservePursuitTaskResources(pursuitID uuid.UUID, owner, operationID string, effort, cost int64) error {
	if err := r.fakePursuitAttemptRecorder.ReservePursuitTaskResources(pursuitID, owner, operationID, effort, cost); err != nil {
		return err
	}
	if r.denyFallback && len(r.reservations) == 2 {
		return errors.New("remaining effort ceiling exhausted")
	}
	return nil
}

func TestRunFallbackReservationFailureRetainsCompletedReceiptAndRequiresReview(t *testing.T) {
	for _, test := range []struct {
		name        string
		request     string
		maxAttempts int
	}{
		{"two attempts", "Run local script tests and verify the result", 2},
		{"three attempts", "Run local script tests and verify the routing architecture", 3},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("HAI_EMERGENCY_STOP", "false")
			harness := newFallbackReviewGenerationHarness(t)
			executor := &fakeToolExecutor{result: completedToolResult()}
			receipt := *executor.result
			recorder := &fallbackReviewReservationRecorder{denyFallback: true}
			verifier := &sequencedVerificationService{statuses: []string{verification.StatusNeedsReview}}
			engine := NewServiceWithEnginesAndPursuitAttempts(
				&fakeMemoryService{}, harness.llmService, nil, verifier, executor, recorder,
				harness.frameworkSelector,
			)
			plan, err := engine.Run(IntakeRequest{
				OwnerIdentity: "alice", PursuitID: uuid.NewString(), Request: test.request,
				ProjectKey: "018-HAI", AutomationID: receipt.AutomationID, ExecuteAllowed: true,
			})
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			assertFallbackReview(t, plan, test.maxAttempts, "pursuit resource reservation blocked execution: remaining effort ceiling exhausted")
			if executor.calls != 1 || verifier.calls != 1 || harness.generateCalls.Load() != 1 ||
				len(recorder.reservations) != 2 || len(recorder.settlements) != 1 {
				t.Fatalf("wrong fallback path: runtime=%d verification=%d generation=%d reservations=%v settlements=%v",
					executor.calls, verifier.calls, harness.generateCalls.Load(), recorder.reservations, recorder.settlements)
			}
			if recorder.settlements[0] != recorder.reservations[0]+":consumed" ||
				!strings.HasSuffix(recorder.reservations[1], ":attempt:2") {
				t.Fatalf("reservation/settlement lifecycle lost: %v %v", recorder.reservations, recorder.settlements)
			}
			if !reflect.DeepEqual(plan.ExecutionResult.ToolExecution, &receipt) ||
				!hasTaskAction(plan.ExecutionResult.Actions, "automation.launch", "completed") ||
				!hasTaskAction(plan.ExecutionResult.Actions, "verification.answer", "completed") ||
				!hasTaskAction(plan.ExecutionResult.Actions, "llm.generate", "completed") ||
				!strings.Contains(plan.ExecutionResult.Output, "prior execution evidence") {
				t.Fatalf("fallback reservation discarded completed evidence: %#v", plan.ExecutionResult)
			}
			if plan.FrameworkEvidencePreflight == nil || !plan.FrameworkEvidencePreflight.Passed ||
				len(executor.requests) != 1 || executor.requests[0].Governance.TaskPlanID != plan.ID ||
				executor.requests[0].Governance.FrameworkEvidencePreflightDigest == "" ||
				executor.requests[0].Governance.ResourceDecisionDigest == "" {
				t.Fatalf("fixture did not preserve the actual launch governance binding: %#v", executor.requests)
			}
			if len(recorder.attempts) != 2 || recorder.attempts[1].LaunchEventID != receipt.LaunchEventID {
				t.Fatalf("final pursuit attempt lost the launch receipt: %#v", recorder.attempts)
			}
		})
	}
}

func TestRunEmptyFallbackModelRequiresReviewWithoutAutomaticRetry(t *testing.T) {
	for _, test := range []struct {
		name        string
		request     string
		maxAttempts int
		needsTools  bool
	}{
		{"reasoning/two attempts", "Summarize project context for the dashboard", 2, false},
		{"reasoning/three attempts", "Explain the API architecture and compare routing options", 3, false},
		{"receipt/two attempts", "Run local script tests and verify the result", 2, true},
		{"receipt/three attempts", "Run local script tests and verify the routing architecture", 3, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("HAI_EMERGENCY_STOP", "false")
			harness := newFallbackReviewGenerationHarness(t)
			executor := &fakeToolExecutor{result: completedToolResult()}
			verifier := &sequencedVerificationService{statuses: []string{verification.StatusNeedsReview}}
			engine := NewServiceWithEngines(&fakeMemoryService{}, harness.llmService, nil, verifier, executor).(*service)
			engine.frameworkSelector = harness.frameworkSelector
			verifier.onAnswer = func(call int) {
				if call == 1 {
					engine.llmService = newTaskNoProviderLLMService(t)
				}
			}
			request := IntakeRequest{OwnerIdentity: "alice", Request: test.request, ProjectKey: "018-HAI"}
			wantReason := "no capable model was selected; configure an eligible model before retrying"
			wantCalls := 0
			if test.needsTools {
				request.ExecuteAllowed = true
				request.AutomationID = executor.result.AutomationID
				wantReason += "; the selected runtime has no verified read-only metadata"
				wantCalls = 1
			}
			plan, err := engine.Run(request)
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			assertFallbackReview(t, plan, test.maxAttempts, wantReason)
			if plan.ModelDecision.SelectedModelID != "" || plan.Intake.NeedsTools != test.needsTools ||
				executor.calls != wantCalls || verifier.calls != 1 || harness.generateCalls.Load() != 1 {
				t.Fatalf("fixture did not reach selected-to-empty fallback without another dispatch: plan=%#v runtime=%d verification=%d generation=%d",
					plan, executor.calls, verifier.calls, harness.generateCalls.Load())
			}
			if test.needsTools {
				if !reflect.DeepEqual(plan.ExecutionResult.ToolExecution, executor.result) ||
					!hasTaskAction(plan.ExecutionResult.Actions, "automation.launch", "completed") {
					t.Fatalf("model denial lost the prior completed receipt: %#v", plan.ExecutionResult)
				}
			} else if plan.ExecutionResult.ToolExecution != nil {
				t.Fatal("reasoning-only fallback invented a runtime receipt")
			}
		})
	}
}

func TestRunFallbackSettlementBlockerOverridesVerifiedOutput(t *testing.T) {
	t.Setenv("HAI_EMERGENCY_STOP", "false")
	harness := newFallbackReviewGenerationHarness(t)
	executor := &fakeToolExecutor{result: completedToolResult()}
	recorder := &fakePursuitAttemptRecorder{}
	verifier := &sequencedVerificationService{
		statuses: []string{verification.StatusNeedsReview, verification.StatusSourceSupported},
		onAnswer: func(call int) {
			if call == 2 {
				recorder.settleErr = errors.New("settlement could not be recorded")
			}
		},
	}
	engine := NewServiceWithEnginesAndPursuitAttempts(
		&fakeMemoryService{}, harness.llmService, nil, verifier, executor, recorder,
		harness.frameworkSelector,
	)
	plan, err := engine.Run(IntakeRequest{
		OwnerIdentity: "alice", PursuitID: uuid.NewString(), ProjectKey: "018-HAI",
		Request: "Run local script tests and verify the result", AutomationID: executor.result.AutomationID, ExecuteAllowed: true,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	assertFallbackReview(t, plan, 2, "execution completed but pursuit resource settlement requires review: settlement could not be recorded")
	if executor.calls != 1 || verifier.calls != 2 || len(recorder.reservations) != 2 || len(recorder.settlements) != 2 ||
		plan.ExecutionResult.ToolExecution == nil || plan.ExecutionResult.ToolExecution.LaunchEventID != executor.result.LaunchEventID ||
		!hasTaskAction(plan.ExecutionResult.Actions, "automation.launch", "reused") {
		t.Fatalf("settlement review repeated or lost the successful runtime: %#v", plan.ExecutionResult)
	}
}

func assertFallbackReview(t *testing.T, plan *CompletionPlan, maxAttempts int, reason string) {
	t.Helper()
	if plan == nil || plan.ExecutionResult == nil {
		t.Fatal("fallback returned no execution plan")
	}
	if plan.RetryPolicy.CurrentAttempt != 2 || plan.RetryPolicy.MaxAttempts != maxAttempts || plan.RetryPolicy.RetryAvailable ||
		plan.CompletionStatus != "review_required" || plan.ValidationResult.Passed || plan.ValidationResult.Status != "blocked" ||
		plan.ValidationResult.NextAction != "resolve the execution blocker before retrying" ||
		plan.ExecutionResult.BlockedReason != reason || plan.ExecutionResult.VerificationStatus != verification.StatusNeedsReview ||
		plan.ReviewQueueItem == nil || plan.ReviewQueueItem.Reason != reason {
		t.Fatalf("fallback blocker mismatch: status=%s attempt=%d max=%d retry=%t validation=%s passed=%t blocker=%q review=%#v",
			plan.CompletionStatus, plan.RetryPolicy.CurrentAttempt, plan.RetryPolicy.MaxAttempts, plan.RetryPolicy.RetryAvailable,
			plan.ValidationResult.Status, plan.ValidationResult.Passed, plan.ExecutionResult.BlockedReason, plan.ReviewQueueItem)
	}
	if len(plan.StoredMemoryIDs) != 0 || plan.ExecutionResult.CompletedAt.Before(plan.ExecutionResult.StartedAt) ||
		plan.ExecutionResult.CompletedAt.After(time.Now().UTC()) {
		t.Fatalf("blocked fallback stored accepted lessons or returned invalid timing: %#v", plan.ExecutionResult)
	}
}
