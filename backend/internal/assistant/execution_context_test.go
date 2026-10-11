package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"automation-hub-backend/internal/identity"
	"automation-hub-backend/internal/task"
	"github.com/gin-gonic/gin"
)

func TestAssistantHTTPPreservesExecutionLifetime(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, cancelled := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		if cancelled {
			cancel()
		}
		engine := &fakeTaskEngine{}
		response := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(response)
		c.Request = httptest.NewRequest(http.MethodPost, "/assistant/command", strings.NewReader(`{"message":"Summarize my notes"}`)).WithContext(ctx)
		c.Set(identity.ContextSubjectKey, "alice")
		NewHandler(NewService(engine, nil)).Command(c)
		cancel()
		if cancelled {
			if response.Code != http.StatusRequestTimeout || engine.planCalls != 0 || engine.runCalls != 0 {
				t.Fatalf("cancelled chat started work: status=%d plan=%d run=%d", response.Code, engine.planCalls, engine.runCalls)
			}
		} else if response.Code != http.StatusOK || engine.lastRequest.ExecutionContext != ctx {
			t.Fatal("chat dropped its trusted execution context")
		}
	}
}

type cancellingCommandTask struct {
	*fakeTaskEngine
	cancel context.CancelFunc
}

func (e *cancellingCommandTask) Plan(request task.IntakeRequest) (*task.CompletionPlan, error) {
	plan, err := e.fakeTaskEngine.Plan(request)
	e.cancel()
	return plan, err
}

func (e *cancellingCommandTask) Run(request task.IntakeRequest) (*task.CompletionPlan, error) {
	plan, err := e.fakeTaskEngine.Run(request)
	e.cancel()
	return plan, err
}

func TestCancelledCommandRetainsTaskResultAndDoesNotStartCycle(t *testing.T) {
	for _, execute := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		engine := &cancellingCommandTask{fakeTaskEngine: &fakeTaskEngine{}, cancel: cancel}
		cycle := &fakeAgentCycleRunner{}
		svc := NewService(engine, cycle)
		result, err := svc.Command(CommandRequest{ExecutionContext: ctx, OwnerIdentity: "alice",
			Message: "Summarize my notes", ExecuteAllowed: execute, RunCycle: true})
		cancel()
		if !errors.Is(err, context.Canceled) || cycle.calls != 0 || engine.lastRequest.ExecutionContext != ctx ||
			result == nil || result.Plan == nil || result.Plan.ID != "task-1" || !result.ReviewRequired || len(svc.LogsForOwner("alice")) != 0 {
			t.Fatalf("interrupted chat discarded result, ran a cycle or reported completion: result=%#v err=%v cycles=%d", result, err, cycle.calls)
		}
	}
}

func TestCommandLifetimeIsNotSerializedOrIncludedInSourceIdentity(t *testing.T) {
	request := CommandRequest{Message: "Summarize my notes", OwnerIdentity: "alice"}
	before := assistantCommandSourceID(request.Message, request)
	request.ExecutionContext = context.Background()
	if before != assistantCommandSourceID(request.Message, request) {
		t.Fatal("caller lifetime changed source identity")
	}
	encoded, err := json.Marshal(request)
	if err != nil || strings.Contains(string(encoded), "ExecutionContext") || strings.Contains(string(encoded), "executionContext") {
		t.Fatalf("caller lifetime serialized: %s %v", encoded, err)
	}
	var decoded CommandRequest
	if err := json.Unmarshal([]byte(`{"message":"Notes","executionContext":{}}`), &decoded); err != nil || decoded.ExecutionContext != nil {
		t.Fatal("untrusted JSON set caller lifetime")
	}
}
