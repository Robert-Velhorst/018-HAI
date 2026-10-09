package workflow

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"automation-hub-backend/internal/identity"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type messageFollowUpService struct {
	Service
	result *OpenLoopRunSummary
}

func (s *messageFollowUpService) RunDueOpenLoopsForOwnerContext(context.Context, string, RunDueRequest) (*OpenLoopRunSummary, error) {
	return s.result, nil
}

func TestFollowUpHTTPRedactsMessagesWithoutMutatingWorkerResults(t *testing.T) {
	gin.SetMode(gin.TestMode)
	workflowID, loopID := uuid.New(), uuid.New()
	message := "storage failed password=private-worker-password"
	original := &OpenLoopRunSummary{
		Checked: 1, Skipped: 1,
		Results: []OpenLoopRunResult{{WorkflowID: workflowID, OpenLoopID: loopID, Status: "failed", State: "blocked", Message: message}},
	}
	response := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(response)
	c.Request = httptest.NewRequest(http.MethodPost, "/workflow/run-due-open-loops", strings.NewReader(`{"limit":5}`))
	c.Set(identity.ContextSubjectKey, "alice")
	NewHandler(&messageFollowUpService{result: original}).RunDueOpenLoops(c)
	if response.Code != http.StatusOK || strings.Contains(response.Body.String(), "private-worker-password") {
		t.Fatalf("unsafe worker response: status=%d", response.Code)
	}
	var public OpenLoopRunSummary
	if err := json.Unmarshal(response.Body.Bytes(), &public); err != nil {
		t.Fatal(err)
	}
	if len(public.Results) != 1 || public.Checked != 1 || public.Skipped != 1 {
		t.Fatal("redaction changed worker counts")
	}
	item := public.Results[0]
	if item.WorkflowID != workflowID || item.OpenLoopID != loopID || item.Status != "failed" || item.State != "blocked" || !strings.Contains(item.Message, "storage failed") {
		t.Fatal("redaction lost identity, state or recovery context")
	}
	if original.Results[0].Message != message {
		t.Fatal("HTTP projection mutated the service result")
	}
}

type messageReminderService struct {
	Service
	result *ReminderDeliveryRunSummary
}

func (s *messageReminderService) RunDueReminderDeliveriesForOwnerContext(context.Context, string, RunDueRequest) (*ReminderDeliveryRunSummary, error) {
	return s.result, nil
}

func TestReminderWorkerHTTPRedactsReasonWithoutChangingReceipts(t *testing.T) {
	gin.SetMode(gin.TestMode)
	id := uuid.New()
	reason := "sink failed password=private-reminder-password"
	original := &ReminderDeliveryRunSummary{
		Checked: 1, Retried: 1,
		Results: []ReminderDeliveryRunResult{{AuthorizationID: id, Status: "retry", Reason: reason}},
	}
	response := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(response)
	c.Request = httptest.NewRequest(http.MethodPost, "/workflow/run-due-reminder-deliveries", strings.NewReader(`{"limit":5}`))
	c.Set(identity.ContextSubjectKey, "alice")
	NewHandler(&messageReminderService{result: original}).RunDueReminderDeliveries(c)
	if response.Code != http.StatusOK || strings.Contains(response.Body.String(), "private-reminder-password") {
		t.Fatalf("unsafe reminder response: status=%d", response.Code)
	}
	var public ReminderDeliveryRunSummary
	if err := json.Unmarshal(response.Body.Bytes(), &public); err != nil {
		t.Fatal(err)
	}
	if len(public.Results) != 1 || public.Checked != 1 || public.Retried != 1 || public.Delivered != 0 {
		t.Fatal("redaction changed delivery counts")
	}
	if public.Results[0].AuthorizationID != id || public.Results[0].Status != "retry" || !strings.Contains(public.Results[0].Reason, "sink failed") {
		t.Fatal("redaction changed authorization, status or failure context")
	}
	if original.Results[0].Reason != reason {
		t.Fatal("HTTP projection mutated worker-owned reason")
	}
}

func TestWorkerHTTPPreservesEmptyResultShape(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, empty := range []bool{false, true} {
		t.Run(map[bool]string{false: "nil", true: "empty"}[empty], func(t *testing.T) {
			followUp := &OpenLoopRunSummary{}
			reminder := &ReminderDeliveryRunSummary{}
			if empty {
				followUp.Results = []OpenLoopRunResult{}
				reminder.Results = []ReminderDeliveryRunResult{}
			}
			for _, invoke := range []func(*gin.Context){
				NewHandler(&messageFollowUpService{result: followUp}).RunDueOpenLoops,
				NewHandler(&messageReminderService{result: reminder}).RunDueReminderDeliveries,
			} {
				response := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(response)
				c.Request = httptest.NewRequest(http.MethodPost, "/workflow/worker", strings.NewReader(`{"limit":5}`))
				c.Set(identity.ContextSubjectKey, "alice")
				invoke(c)
				var public map[string]json.RawMessage
				if err := json.Unmarshal(response.Body.Bytes(), &public); err != nil {
					t.Fatal(err)
				}
				want := "null"
				if empty {
					want = "[]"
				}
				if response.Code != http.StatusOK || string(public["results"]) != want {
					t.Fatalf("empty results changed: status=%d results=%s want=%s", response.Code, public["results"], want)
				}
			}
		})
	}
}

type messageExecutionService struct {
	Service
	result *WorkflowRunSummary
}

func (s *messageExecutionService) RunDueForOwnerContext(context.Context, string, RunDueRequest) (*WorkflowRunSummary, error) {
	return s.result, nil
}

func (s *messageExecutionService) RunOneForOwnerContext(context.Context, string, uuid.UUID) (*WorkflowRunResult, error) {
	return &s.result.Results[0], nil
}

type messageRecoveryService struct {
	Service
	result *ClaimRecoverySummary
}

func (s *messageRecoveryService) RecoverStaleClaimsForOwnerContext(context.Context, string, RunDueRequest) (*ClaimRecoverySummary, error) {
	return s.result, nil
}

func TestExecutionAndRecoveryHTTPPreserveStateWhileRedactingMessages(t *testing.T) {
	gin.SetMode(gin.TestMode)
	id := uuid.New()
	message := "audit write failed password=private-audit-password"
	execution := &WorkflowRunSummary{
		Checked: 1, Completed: 1,
		Results: []WorkflowRunResult{{WorkflowID: id, Status: "completed", State: "completed", Attempts: 2, VerificationStatus: "verified", ReviewRequired: true, Message: message}},
	}
	recovery := &ClaimRecoverySummary{
		Checked: 1, WorkflowsBlocked: 1,
		Results: []ClaimRecoveryResult{{WorkflowID: id, Type: "workflow", Status: "blocked", Message: message}},
	}
	for _, test := range []struct {
		name   string
		invoke func(*gin.Context)
		check  func([]byte) bool
	}{
		{"execution", NewHandler(&messageExecutionService{result: execution}).RunDue, func(body []byte) bool {
			var public WorkflowRunSummary
			return json.Unmarshal(body, &public) == nil && public.Checked == 1 && public.Completed == 1 && len(public.Results) == 1 &&
				public.Results[0].WorkflowID == id && public.Results[0].Status == "completed" && public.Results[0].State == "completed" &&
				public.Results[0].Attempts == 2 && public.Results[0].VerificationStatus == "verified" && public.Results[0].ReviewRequired
		}},
		{"recovery", NewHandler(&messageRecoveryService{result: recovery}).RecoverStaleClaims, func(body []byte) bool {
			var public ClaimRecoverySummary
			return json.Unmarshal(body, &public) == nil && public.Checked == 1 && public.WorkflowsBlocked == 1 && len(public.Results) == 1 &&
				public.Results[0].WorkflowID == id && public.Results[0].Type == "workflow" && public.Results[0].Status == "blocked"
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(response)
			c.Request = httptest.NewRequest(http.MethodPost, "/workflow/worker", strings.NewReader(`{"limit":5}`))
			c.Set(identity.ContextSubjectKey, "alice")
			test.invoke(c)
			body := response.Body.String()
			if response.Code != http.StatusOK || strings.Contains(body, "private-audit-password") || !strings.Contains(body, "audit write failed") || !test.check(response.Body.Bytes()) {
				t.Fatalf("worker projection changed safety/state: status=%d", response.Code)
			}
		})
	}
	if execution.Results[0].Message != message || recovery.Results[0].Message != message {
		t.Fatal("HTTP projection mutated worker audit messages")
	}
}
