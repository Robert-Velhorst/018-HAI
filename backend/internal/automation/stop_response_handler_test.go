package automation

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"automation-hub-backend/internal/agentruntime"
	"automation-hub-backend/internal/identity"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type stopResponseProbe struct {
	Service
	result *agentruntime.StopResult
}

func (p *stopResponseProbe) StopRuntimeTaskForOwnerContext(context.Context, uuid.UUID, string) (*agentruntime.StopResult, error) {
	return p.result, nil
}

func TestRuntimeStopHandlerRejectsMalformedSuccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, mode := range []string{"valid", "nil", "empty_status", "unknown_status", "missing_runtime", "missing_task", "missing_evidence", "nil_event", "requested", "blocked"} {
		t.Run(mode, func(t *testing.T) {
			defer func() {
				if p := recover(); p != nil {
					t.Fatalf("handler panicked: %v", p)
				}
			}()
			result := &agentruntime.StopResult{RuntimeID: "openclaw", TaskID: "task", Status: "stopped", EvidenceURI: "automation-launch://" + uuid.NewString()}
			switch mode {
			case "nil":
				result = nil
			case "empty_status":
				result.Status = ""
			case "unknown_status":
				result.Status = "completed"
			case "missing_runtime":
				result.RuntimeID = ""
			case "missing_task":
				result.TaskID = ""
			case "missing_evidence":
				result.EvidenceURI = ""
			case "nil_event":
				result.EvidenceURI = "automation-launch://" + uuid.Nil.String()
			case "requested":
				result = &agentruntime.StopResult{Status: "cancellation_requested"}
			case "blocked":
				result = &agentruntime.StopResult{Status: "blocked"}
			}
			response := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(response)
			c.Request = httptest.NewRequest(http.MethodPost, "/stop-runtime", nil)
			c.Params = gin.Params{{Key: "id", Value: uuid.NewString()}}
			c.Set(identity.ContextSubjectKey, "robert")
			NewHandler(&stopResponseProbe{result: result}).StopRuntimeTask(c)
			want := http.StatusBadGateway
			if mode == "valid" || mode == "requested" {
				want = http.StatusOK
			}
			if mode == "blocked" {
				want = http.StatusBadRequest
			}
			if response.Code != want {
				t.Fatalf("status=%d want=%d body=%s", response.Code, want, response.Body.String())
			}
			if want == http.StatusBadGateway && !strings.Contains(response.Body.String(), "not confirmed") {
				t.Fatal("malformed success lacks uncertainty message")
			}
		})
	}
}
