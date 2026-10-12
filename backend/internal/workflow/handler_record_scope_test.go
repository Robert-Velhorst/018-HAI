package workflow

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"automation-hub-backend/internal/identity"
	"automation-hub-backend/internal/models"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type scopedRecordHandlerService struct {
	Service
	record *WorkflowRecord
}

type scopedRunOneHandlerService struct {
	scopedRecordHandlerService
	result *WorkflowRunResult
}

func (s *scopedRunOneHandlerService) RunDueForOwnerContext(context.Context, string, RunDueRequest) (*WorkflowRunSummary, error) {
	return nil, nil
}

func (s *scopedRunOneHandlerService) RunOneForOwnerContext(context.Context, string, uuid.UUID) (*WorkflowRunResult, error) {
	return s.result, nil
}

func TestRunOneHTTPBindsResultToRequestedWorkflow(t *testing.T) {
	gin.SetMode(gin.TestMode)
	id := uuid.New()
	for _, test := range []struct {
		name   string
		result *WorkflowRunResult
		status int
	}{
		{"missing result", nil, http.StatusInternalServerError},
		{"missing identity", &WorkflowRunResult{Status: "completed", Message: "private-wrong-result"}, http.StatusInternalServerError},
		{"wrong identity", &WorkflowRunResult{WorkflowID: uuid.New(), Status: "completed", Message: "private-wrong-result"}, http.StatusInternalServerError},
		{"matching identity", &WorkflowRunResult{WorkflowID: id, Status: "completed", ReviewRequired: true, Message: "password=private-worker-password"}, http.StatusOK},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := &scopedRunOneHandlerService{
				scopedRecordHandlerService: scopedRecordHandlerService{record: &WorkflowRecord{Item: models.WorkflowItem{ID: id, OwnerIdentity: "alice"}}},
				result:                     test.result,
			}
			response := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(response)
			c.Request = httptest.NewRequest(http.MethodPost, "/workflow/items/"+id.String()+"/run", nil)
			c.Params = gin.Params{{Key: "id", Value: id.String()}}
			c.Set(identity.ContextSubjectKey, "alice")
			NewHandler(service).RunOne(c)
			body := response.Body.String()
			if response.Code != test.status || strings.Contains(body, "private-wrong-result") || strings.Contains(body, "private-worker-password") {
				t.Fatalf("unbound or unsafe execution response: status=%d want=%d", response.Code, test.status)
			}
			if test.status == http.StatusOK && (!strings.Contains(body, id.String()) || !strings.Contains(body, `"reviewRequired":true`)) {
				t.Fatal("matching result lost identity or review requirement")
			}
		})
	}
}

func (s *scopedRecordHandlerService) GetForOwner(string, uuid.UUID) (*WorkflowRecord, error) {
	return s.record, nil
}

func TestWorkflowRecordBoundariesRejectMissingOrUnboundReadback(t *testing.T) {
	gin.SetMode(gin.TestMode)
	id := uuid.New()
	for _, test := range []struct {
		name   string
		record *WorkflowRecord
	}{
		{"missing", nil},
		{"wrong owner", &WorkflowRecord{Item: models.WorkflowItem{ID: id, OwnerIdentity: "bob"}}},
		{"wrong id", &WorkflowRecord{Item: models.WorkflowItem{ID: uuid.New(), OwnerIdentity: "alice"}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			handler := NewHandler(&scopedRecordHandlerService{record: test.record})
			for _, readback := range []bool{false, true} {
				response := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(response)
				c.Request = httptest.NewRequest(http.MethodGet, "/workflow/items/"+id.String(), nil)
				c.Set(identity.ContextSubjectKey, "alice")
				if readback {
					handler.respondScopedWorkflow(c, id, http.StatusOK)
				} else if handler.ensureWorkflowMutable(c, id) {
					t.Fatal("unbound record authorized mutation")
				}
				if response.Code != http.StatusNotFound || !strings.Contains(response.Body.String(), "workflow not found") || strings.Contains(response.Body.String(), "bob") {
					t.Fatalf("unbound readback exposed or accepted: status=%d", response.Code)
				}
			}
		})
	}
	valid := &WorkflowRecord{Item: models.WorkflowItem{ID: id, OwnerIdentity: "alice"}}
	handler := NewHandler(&scopedRecordHandlerService{record: valid})
	response := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(response)
	c.Request = httptest.NewRequest(http.MethodGet, "/workflow/items/"+id.String(), nil)
	c.Set(identity.ContextSubjectKey, "alice")
	if !handler.ensureWorkflowMutable(c, id) {
		t.Fatal("matching owned record rejected")
	}
	handler.respondScopedWorkflow(c, id, http.StatusOK)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), id.String()) {
		t.Fatal("matching owned readback rejected")
	}
}
