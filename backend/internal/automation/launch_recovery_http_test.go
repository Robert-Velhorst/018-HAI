package automation

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"automation-hub-backend/internal/events"
	"automation-hub-backend/internal/identity"
	"automation-hub-backend/internal/models"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func TestLaunchHTTPPreservesRecoveryReference(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, mode := range []string{"uncertain_intent", "cancelled_replay"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			id, key := uuid.New(), uuid.NewString()
			base := newFakeAutomationRepo(&models.Automation{ID: id, LaunchType: "api", LaunchTarget: "POST invalid"})
			repo := &ownedAdmissionProbe{launchConfigurationProbe: launchConfigurationProbe{Repository: base}}
			svc := newTestService(repo, events.Publisher{})
			if mode == "cancelled_replay" {
				first, err := svc.LaunchTask(id, TaskLaunchRequest{ExecutionContext: ctx, OwnerIdentity: "alice", IdempotencyKey: key})
				if err != nil || first == nil || first.Status != "blocked" {
					t.Fatal("replay fixture did not produce a blocked receipt")
				}
				repo.afterOutcome = cancel
			} else {
				repo.afterSave = cancel
			}
			response := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(response)
			c.Request = httptest.NewRequest(http.MethodPost, "/automation/"+id.String()+"/launch", nil).WithContext(ctx)
			c.Request.Header.Set("Idempotency-Key", key)
			c.Params = gin.Params{{Key: "id", Value: id.String()}}
			c.Set(identity.ContextSubjectKey, "alice")
			NewHandler(svc).Launch(c)
			if response.Code != http.StatusInternalServerError {
				t.Fatalf("partial result was reported as success: %d", response.Code)
			}
			var body struct {
				Error    string `json:"error"`
				Recovery struct {
					AutomationID           uuid.UUID `json:"automationId"`
					LaunchEventID          uuid.UUID `json:"launchEventId"`
					ReconciliationRequired bool      `json:"reconciliationRequired"`
					RetryAllowed           bool      `json:"retryAllowed"`
				} `json:"recovery"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body.Error == "" || body.Recovery.AutomationID != id || body.Recovery.LaunchEventID == uuid.Nil || !body.Recovery.ReconciliationRequired || body.Recovery.RetryAllowed {
				t.Fatal("error response lost safe recovery identity or allowed retry")
			}
			if strings.Contains(response.Body.String(), "POST invalid") || repo.scopeSaves != 1 {
				t.Fatal("error response leaked target or created another attempt")
			}
		})
	}
}

func TestPublicLaunchRecoveryUsesOnlySafeReferences(t *testing.T) {
	id := uuid.New()
	for _, result := range []*LaunchResult{nil, {AutomationID: uuid.New(), LaunchEventID: uuid.New()}, {AutomationID: id}} {
		if publicLaunchRecovery(id, result) != nil {
			t.Fatal("invalid or unrelated result exposed a recovery reference")
		}
	}
	result := &LaunchResult{AutomationID: id, LaunchEventID: uuid.New(), Status: "token=private", Output: "private-output", Target: "private-target", Message: "private-message", ExecutionReference: "private-runtime"}
	encoded, err := json.Marshal(publicLaunchRecovery(id, result))
	if err != nil || strings.Contains(string(encoded), "private") || !strings.Contains(string(encoded), `"reportedStatus":"unknown"`) {
		t.Fatal("recovery response copied untrusted or sensitive result fields")
	}
}
