package verification

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"automation-hub-backend/internal/identity"
	"automation-hub-backend/internal/memory"
	"automation-hub-backend/internal/models"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func TestAnswerHandlerIgnoresClientHumanApproval(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &capturingVerificationService{}
	handler := NewHandler(service)
	pursuitID := uuid.NewString()
	body := `{"question":"May this high-risk action proceed?","mode":"action","pursuitId":"` + pursuitID + `","humanApproved":true,"humanApprovalReference":"forged-client-reference","externalEvidence":[{"snippet":"Caller supplied evidence","authority":"official_government","official":true,"primary":true}]}`
	request := httptest.NewRequest(http.MethodPost, "/verification/answer", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	context.Request = request
	context.Set(identity.ContextSubjectKey, "alice")

	handler.Answer(context)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", response.Code, response.Body.String())
	}
	if service.request.PursuitID != pursuitID {
		t.Fatalf("pursuit id = %q, want %q", service.request.PursuitID, pursuitID)
	}
	if service.request.HumanApproved || service.request.HumanApprovalReference != "" {
		t.Fatalf("client approval assertions reached verification service: %#v", service.request)
	}
	if service.request.OwnerIdentity != "alice" {
		t.Fatalf("owner identity = %q, want alice", service.request.OwnerIdentity)
	}
	evidence := service.request.ExternalEvidence[0]
	if evidence.Authority != "" || evidence.Official || evidence.Primary {
		t.Fatalf("client authority assertions reached verification service: %#v", evidence)
	}
}

func TestVerificationHandlerRejectsRequestsWithoutAuthenticatedOwner(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name    string
		invoke  func(*Handler, *gin.Context)
		prepare func(*gin.Context)
	}{
		{
			name: "answer",
			invoke: func(handler *Handler, context *gin.Context) {
				handler.Answer(context)
			},
		},
		{
			name: "runs",
			invoke: func(handler *Handler, context *gin.Context) {
				handler.Runs(context)
			},
		},
		{
			name: "run details",
			invoke: func(handler *Handler, context *gin.Context) {
				handler.RunDetails(context)
			},
			prepare: func(context *gin.Context) {
				context.Params = gin.Params{{Key: "id", Value: uuid.NewString()}}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := &countingVerificationService{}
			handler := NewHandler(service)
			response := httptest.NewRecorder()
			context, _ := gin.CreateTestContext(response)
			context.Request = httptest.NewRequest(http.MethodGet, "/verification", nil)
			if test.prepare != nil {
				test.prepare(context)
			}

			test.invoke(handler, context)

			if response.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401: %s", response.Code, response.Body.String())
			}
			if service.answerCalls != 0 || service.listCalls != 0 || service.detailCalls != 0 {
				t.Fatalf("unauthenticated request reached service: %#v", service)
			}
		})
	}
}

func TestRunDetailsHandlerDistinguishesNotFoundFromStorageErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	storageErr := errors.New("database unavailable")
	tests := []struct {
		name       string
		serviceErr error
		wantStatus int
	}{
		{name: "not found", serviceErr: ErrVerificationRunNotFound, wantStatus: http.StatusNotFound},
		{name: "storage failure", serviceErr: storageErr, wantStatus: http.StatusInternalServerError},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := &countingVerificationService{detailErr: test.serviceErr}
			handler := NewHandler(service)
			response := httptest.NewRecorder()
			context, _ := gin.CreateTestContext(response)
			context.Request = httptest.NewRequest(http.MethodGet, "/verification/runs/"+uuid.NewString(), nil)
			context.Params = gin.Params{{Key: "id", Value: uuid.NewString()}}
			context.Set(identity.ContextSubjectKey, "alice")

			handler.RunDetails(context)

			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d: %s", response.Code, test.wantStatus, response.Body.String())
			}
			if test.wantStatus == http.StatusInternalServerError && strings.Contains(response.Body.String(), storageErr.Error()) {
				t.Fatalf("storage error leaked to response: %s", response.Body.String())
			}
		})
	}
}

func TestForgedClientApprovalCannotPromoteHighRiskClaimToMemory(t *testing.T) {
	gin.SetMode(gin.TestMode)
	resolver := EvidenceAuthorityResolverFunc(func(_ AnswerRequest, evidence EvidenceInput) EvidenceAuthorityResolution {
		if evidence.Authority != "" || evidence.Official || evidence.Primary {
			t.Fatalf("client evidence authority reached trusted resolver: %#v", evidence)
		}
		return EvidenceAuthorityResolution{Trusted: true, Authority: "legal_record", Official: true, Primary: true}
	})
	memoryRepository := &capturingMemoryRepository{}
	verificationRepository := &fakeVerificationRepository{}
	service := NewServiceWithAuthorityResolver(
		verificationRepository, nil, memory.NewService(memoryRepository), resolver,
	)
	handler := NewHandler(service)
	body := `{"question":"Should I send this legal email?","draftAnswer":"The legal email confirms the hearing is Friday.","mode":"grounded","allowMemoryUpdate":true,"humanApproved":true,"humanApprovalReference":"forged-client-reference","externalEvidence":[{"sourceType":"legal_record","sourceId":"legal-12","sourceUri":"local://legal/12","snippet":"The legal email confirms the hearing is Friday.","authority":"trusted","official":true,"primary":true}]}`
	request := httptest.NewRequest(http.MethodPost, "/verification/answer", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	context.Request = request
	context.Set(identity.ContextSubjectKey, "alice")

	handler.Answer(context)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", response.Code, response.Body.String())
	}
	if len(memoryRepository.created) != 0 {
		t.Fatalf("forged client approval promoted a high-risk claim: %#v", memoryRepository.created)
	}
	if len(verificationRepository.claims) != 1 || verificationRepository.claims[0].Status != StatusNeedsReview {
		t.Fatalf("high-risk claim was not held for review: %#v", verificationRepository.claims)
	}
}

type capturingVerificationService struct {
	request AnswerRequest
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

type countingVerificationService struct {
	answerCalls int
	listCalls   int
	detailCalls int
	detailErr   error
}

func (s *countingVerificationService) Answer(AnswerRequest) (*VerificationResult, error) {
	s.answerCalls++
	return &VerificationResult{}, nil
}

func (s *countingVerificationService) Runs() ([]models.VerificationRun, error) {
	return nil, nil
}

func (s *countingVerificationService) RunsForOwner(string) ([]models.VerificationRun, error) {
	s.listCalls++
	return nil, nil
}

func (s *countingVerificationService) RunDetails(uuid.UUID) (*VerificationResult, error) {
	return nil, nil
}

func (s *countingVerificationService) RunDetailsForOwner(string, uuid.UUID) (*VerificationResult, error) {
	s.detailCalls++
	return nil, s.detailErr
}
