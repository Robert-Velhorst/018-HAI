package workflow

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"automation-hub-backend/internal/identity"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type workflowMutationFailureService struct {
	Service
	err error
}

func (s *workflowMutationFailureService) Transition(uuid.UUID, TransitionRequest) (*WorkflowRecord, error) {
	return nil, s.err
}

func (s *workflowMutationFailureService) ResolveApproval(uuid.UUID, ApprovalResolutionRequest) (*WorkflowRecord, error) {
	return nil, s.err
}

func (s *workflowMutationFailureService) ResolveInterruptedExecution(uuid.UUID, InterruptedExecutionResolutionRequest) (*WorkflowRecord, error) {
	return nil, s.err
}

func (s *workflowMutationFailureService) ResolveProposal(uuid.UUID, uuid.UUID, ProposalResolutionRequest) (*WorkflowRecord, error) {
	return nil, s.err
}

func TestWorkflowMutationHandlersReturn500OnlyForAuditPersistenceErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	baseService := NewService(newFakeWorkflowRepo())
	record, err := baseService.Intake(IntakeRequest{
		OwnerIdentity: "alice",
		Input:         "Create an internal low-risk checklist.",
	})
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}
	proposalID := record.Proposals[0].ID

	tests := []struct {
		name   string
		body   string
		invoke func(*Handler, *gin.Context)
	}{
		{name: "transition", body: `{"targetState":"blocked"}`, invoke: (*Handler).Transition},
		{name: "approval", body: `{"approved":true}`, invoke: (*Handler).ResolveApproval},
		{name: "interrupted execution", body: `{"decision":"keep_blocked","note":"review"}`, invoke: (*Handler).ResolveInterruptedExecution},
		{name: "proposal", body: `{"status":"approved"}`, invoke: (*Handler).ResolveProposal},
	}

	for _, test := range tests {
		for _, failure := range []struct {
			name string
			err  error
			want int
		}{
			{
				name: "audit persistence",
				err: &WorkflowAuditPersistenceError{
					Operation: "test audit",
					Err:       errors.New("database connection includes secret=do-not-expose"),
				},
				want: http.StatusInternalServerError,
			},
			{
				name: "validation or conflict",
				err:  errors.New("workflow changed while applying transition; reload before trying again"),
				want: http.StatusBadRequest,
			},
		} {
			t.Run(test.name+"/"+failure.name, func(t *testing.T) {
				wrapped := &workflowMutationFailureService{Service: baseService, err: failure.err}
				handler := NewHandler(wrapped)
				request := httptest.NewRequest(http.MethodPost, "/workflow/"+record.Item.ID.String()+"/action", strings.NewReader(test.body))
				request.Header.Set("Content-Type", "application/json")
				response := httptest.NewRecorder()
				context, _ := gin.CreateTestContext(response)
				context.Params = gin.Params{
					{Key: "id", Value: record.Item.ID.String()},
					{Key: "proposalId", Value: proposalID.String()},
				}
				context.Request = request
				context.Set(identity.ContextSubjectKey, "alice")

				test.invoke(handler, context)

				if response.Code != failure.want {
					t.Fatalf("status = %d, want %d: %s", response.Code, failure.want, response.Body.String())
				}
				if failure.want == http.StatusInternalServerError {
					if !strings.Contains(response.Body.String(), workflowAuditPersistenceFailedMessage) {
						t.Fatalf("500 body = %s, want safe audit recovery message", response.Body.String())
					}
					if strings.Contains(response.Body.String(), "do-not-expose") {
						t.Fatalf("500 body leaked persistence detail: %s", response.Body.String())
					}
				} else if !strings.Contains(response.Body.String(), "workflow changed while applying transition") {
					t.Fatalf("400 body lost validation/conflict detail: %s", response.Body.String())
				}
			})
		}
	}
}

func TestWorkflowIncompleteIntakeReturnsConflict(t *testing.T) {
	gin.SetMode(gin.TestMode)
	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	respondWorkflowMutationError(context, ErrWorkflowIntakeIncomplete)

	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusConflict)
	}
	if !strings.Contains(response.Body.String(), ErrWorkflowIntakeIncomplete.Error()) {
		t.Fatalf("response = %s, want incomplete-intake recovery guidance", response.Body.String())
	}
}

func TestWorkflowPersistenceErrorsReturnSanitized500(t *testing.T) {
	gin.SetMode(gin.TestMode)
	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	respondWorkflowMutationError(context, &WorkflowPersistenceError{
		Operation: "test persistence",
		Err:       errors.New("database connection includes secret=do-not-expose"),
	})

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
	}
	if !strings.Contains(response.Body.String(), workflowPersistenceFailedMessage) || strings.Contains(response.Body.String(), "do-not-expose") {
		t.Fatalf("response = %s, want a sanitized persistence failure", response.Body.String())
	}
}

func TestCompletionChecklistFailureReturnsSanitizedReviewGuidance(t *testing.T) {
	gin.SetMode(gin.TestMode)
	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	cause := errors.New("checklist database includes secret=do-not-expose")
	err := workflowAuditPersistenceFailure("completion checklist", cause)
	respondWorkflowMutationError(context, err)
	if response.Code != http.StatusInternalServerError || !errors.Is(err, cause) {
		t.Fatalf("status=%d error=%v; want sanitized storage failure preserving cause", response.Code, err)
	}
	if !strings.Contains(response.Body.String(), workflowAuditPersistenceFailedMessage) || strings.Contains(response.Body.String(), "do-not-expose") {
		t.Fatalf("response=%s; want review guidance without storage details", response.Body.String())
	}
}

func TestWorkflowIntakeConcurrentChangeReturnsConflict(t *testing.T) {
	gin.SetMode(gin.TestMode)
	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	respondWorkflowMutationError(context, ErrWorkflowIntakeConcurrentChange)

	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusConflict)
	}
	if !strings.Contains(response.Body.String(), ErrWorkflowIntakeConcurrentChange.Error()) {
		t.Fatalf("response = %s, want reload guidance", response.Body.String())
	}
}
