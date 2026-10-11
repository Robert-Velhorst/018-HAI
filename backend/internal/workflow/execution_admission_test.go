package workflow

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"automation-hub-backend/internal/identity"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type admissionExecutionProbe struct {
	Service
	calls int
}

func (p *admissionExecutionProbe) RunDueForOwnerContext(context.Context, string, RunDueRequest) (*WorkflowRunSummary, error) {
	p.calls++
	return &WorkflowRunSummary{}, nil
}

func (p *admissionExecutionProbe) RunOneForOwnerContext(context.Context, string, uuid.UUID) (*WorkflowRunResult, error) {
	p.calls++
	return &WorkflowRunResult{}, nil
}

func TestWorkflowRunDueRejectsInvalidEnvelopeBeforeExecution(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, body := range []string{"null", `{} {"limit":1}`, `{} true`,
		`{"limit":51}`, `{"limit":-1}`, `{"limit":1,"ignored":"` + strings.Repeat("x", 4096) + `"}`} {
		t.Run(body[:min(len(body), 50)], func(t *testing.T) {
			svc := &admissionExecutionProbe{}
			response := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(response)
			c.Request = httptest.NewRequest(http.MethodPost, "/workflow/run-due", strings.NewReader(body))
			c.Set(identity.ContextSubjectKey, "alice")
			NewHandler(svc).RunDue(c)
			if response.Code != http.StatusBadRequest || svc.calls != 0 {
				t.Fatalf("invalid envelope started work: status=%d calls=%d", response.Code, svc.calls)
			}
		})
	}
}
