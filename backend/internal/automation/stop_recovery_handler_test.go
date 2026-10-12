package automation

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"automation-hub-backend/internal/agentruntime"
	"automation-hub-backend/internal/identity"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type stopRecoveryProbe struct {
	Service
	result *agentruntime.StopResult
}

func (p *stopRecoveryProbe) StopRuntimeTaskForOwnerContext(context.Context, uuid.UUID, string) (*agentruntime.StopResult, error) {
	return p.result, errors.New("private-provider-payload")
}

func TestRuntimeStopErrorRetainsMinimalRecovery(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, mode := range []string{"valid", "nil", "empty_uri", "invalid_uri", "nil_event", "missing_task", "missing_runtime"} {
		t.Run(mode, func(t *testing.T) {
			id, eventID := uuid.New(), uuid.New()
			result := &agentruntime.StopResult{RuntimeID: "openclaw", TaskID: "task", Status: "indeterminate", EvidenceURI: "automation-launch://" + eventID.String(), Message: "private-provider-payload", AuditEvents: []string{"private-provider-payload"}}
			switch mode {
			case "nil":
				result = nil
			case "empty_uri":
				result.EvidenceURI = ""
			case "invalid_uri":
				result.EvidenceURI = "https://foreign-private.example/receipt"
			case "nil_event":
				result.EvidenceURI = "automation-launch://" + uuid.Nil.String()
			case "missing_task":
				result.TaskID = ""
			case "missing_runtime":
				result.RuntimeID = ""
			}
			response := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(response)
			c.Request = httptest.NewRequest(http.MethodPost, "/stop-runtime", nil)
			c.Params = gin.Params{{Key: "id", Value: id.String()}}
			c.Set(identity.ContextSubjectKey, "robert")
			NewHandler(&stopRecoveryProbe{result: result}).StopRuntimeTask(c)
			if response.Code != http.StatusInternalServerError {
				t.Fatalf("status=%d", response.Code)
			}
			var body map[string]any
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(response.Body.String(), "private") {
				t.Fatal("raw details leaked")
			}
			recovery, exists := body["recovery"].(map[string]any)
			if mode != "valid" {
				if exists {
					t.Fatal("malformed recovery published")
				}
				return
			}
			if !exists || recovery["automationId"] != id.String() || recovery["candidateStopEventId"] != eventID.String() || recovery["candidateEvidenceUri"] != result.EvidenceURI || recovery["retryAllowed"] != false || recovery["reconciliationRequired"] != true {
				t.Fatalf("missing safe recovery: %#v", body)
			}
			if len(recovery) != 6 {
				t.Fatalf("extra recovery payload: %#v", recovery)
			}
		})
	}
}
