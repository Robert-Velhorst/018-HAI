package verification

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"automation-hub-backend/internal/identity"
	"automation-hub-backend/internal/models"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func TestAnswerHandlerIgnoresClientHumanApproval(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &capturingVerificationService{}
	handler := NewHandler(service)
	pursuitID := uuid.NewString()
	body, _ := json.Marshal(AnswerRequest{
		Question:      "May this high-risk action proceed?",
		Mode:          ModeAction,
		PursuitID:     pursuitID,
		HumanApproved: true,
		ExternalEvidence: []EvidenceInput{{
			Snippet:   "Caller supplied evidence",
			Authority: "official_government",
			Official:  true,
			Primary:   true,
		}},
	})
	request := httptest.NewRequest(http.MethodPost, "/verification/answer", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	context.Request = request
	context.Set(identity.ContextSubjectKey, "alice")

	handler.Answer(context)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", response.Code, response.Body.String())
	}
	if service.request.HumanApproved {
		t.Fatalf("client approval reached verification service")
	}
	if service.request.PursuitID != pursuitID {
		t.Fatalf("pursuit id = %q, want %q", service.request.PursuitID, pursuitID)
	}
	if service.request.OwnerIdentity != "alice" {
		t.Fatalf("owner identity = %q, want alice", service.request.OwnerIdentity)
	}
	if service.answerContext != request.Context() {
		t.Fatal("verification handler did not pass the HTTP request context")
	}
	evidence := service.request.ExternalEvidence[0]
	if evidence.Authority != "" || evidence.Official || evidence.Primary {
		t.Fatalf("client authority assertions reached verification service: %#v", evidence)
	}
}

type capturingVerificationService struct {
	request       AnswerRequest
	answerContext context.Context
}

func (s *capturingVerificationService) AnswerContext(ctx context.Context, request AnswerRequest) (*VerificationResult, error) {
	s.answerContext = ctx
	return s.Answer(request)
}

func (s *capturingVerificationService) Answer(request AnswerRequest) (*VerificationResult, error) {
	s.request = request
	return &VerificationResult{}, nil
}

func (s *capturingVerificationService) Runs() ([]models.VerificationRun, error) {
	return nil, nil
}

func (s *capturingVerificationService) RunsForOwner(string) ([]models.VerificationRun, error) {
	return nil, nil
}

func (s *capturingVerificationService) RunDetails(id uuid.UUID) (*VerificationResult, error) {
	return nil, nil
}

func (s *capturingVerificationService) RunDetailsForOwner(string, uuid.UUID) (*VerificationResult, error) {
	return nil, nil
}
