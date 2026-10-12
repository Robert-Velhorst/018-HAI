package opscontrol

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/operations"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func TestPublicControlErrorDoesNotExposeUnexpectedPersistenceDetails(t *testing.T) {
	err := errors.New(`write failed: password=control-secret at C:\\private`)
	message := publicControlError(err)
	for _, forbidden := range []string{"password", "control-secret", "C:\\\\private"} {
		if strings.Contains(strings.ToLower(message), strings.ToLower(forbidden)) {
			t.Fatalf("message leaked %q: %s", forbidden, message)
		}
	}
	if message != "safety-control request could not be completed" {
		t.Fatalf("message = %q", message)
	}
}

func TestPublicControlErrorKeepsActionableSafetyStates(t *testing.T) {
	if got := publicControlError(ErrControlPersistence); got != "safety-control state could not be persisted" {
		t.Fatalf("persistence message = %q", got)
	}
	if got := publicControlError(ErrAutonomyModeStateChanged); got != "safety-control state changed; refresh and retry" {
		t.Fatalf("concurrent message = %q", got)
	}
}

func TestPublicControlReasonCodeDoesNotExposeAuthorizationDetails(t *testing.T) {
	err := controlAuthorizationFailureFor(
		"control.authorization.execution_denied",
		errors.New("provider rejected signature=super-secret"),
	)
	if got := publicControlReasonCode(err); got != "control.authorization.execution_denied" {
		t.Fatalf("reason code = %q", got)
	}
	if strings.Contains(publicControlReasonCode(err), "secret") {
		t.Fatal("reason code leaked internal authorization detail")
	}
}

func TestRecoveryHandlerReturnsUnavailableInsteadOfEmptySuccessOnRepositoryFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repositoryErr := errors.New("operation claim table unavailable")
	base := operations.NewMemoryRepository()
	repository := &failingRecoveryRepository{
		Repository:      base,
		ClaimRepository: base,
		err:             repositoryErr,
	}
	operationService := operations.NewService(repository)
	service := NewService(t.TempDir(), nil, operationService, "owner", "local")
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/background/recovery", nil)

	NewHandler(service).Recovery(ctx)
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("recovery HTTP status = %d, want %d; body=%s", recorder.Code, http.StatusServiceUnavailable, recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), repositoryErr.Error()) {
		t.Fatal("recovery response leaked repository details")
	}
	if strings.Contains(recorder.Body.String(), `"recovered":0`) {
		t.Fatal("repository failure was represented as an empty successful recovery report")
	}
}

type failingRecoveryRepository struct {
	operations.Repository
	operations.ClaimRepository
	err error
}

func (r *failingRecoveryRepository) RecoverExpiredClaims(context.Context, string, string, int) (operations.RecoveryResult, error) {
	return operations.RecoveryResult{}, r.err
}

func (r *failingRecoveryRepository) ClaimNext(context.Context, string, string, uuid.UUID, time.Duration) (*operations.ClaimedOperation, error) {
	return nil, r.err
}
