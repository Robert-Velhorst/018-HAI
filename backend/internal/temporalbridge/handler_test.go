package temporalbridge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"automation-hub-backend/internal/identity"
	"automation-hub-backend/internal/models"

	"github.com/gin-gonic/gin"
	"go.temporal.io/sdk/client"
)

type failingCreateTemporalRepository struct {
	Repository
	err error
}

func (r failingCreateTemporalRepository) Create(*models.TemporalWorkflowRun) (*models.TemporalWorkflowRun, error) {
	return nil, r.err
}

func TestScheduleHandlerDoesNotExposeStorageErrorsOrMisclassifyThem(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name    string
		err     error
		status  int
		message string
	}{
		{"storage error", errors.New("database password=synthetic-password at C:/private-synthetic-store"), http.StatusInternalServerError, "governed follow-up schedule could not be saved safely; inspect run history before retrying"},
		{"empty storage response", nil, http.StatusInternalServerError, "governed follow-up schedule could not be saved safely; inspect run history before retrying"},
		{"wrapped unavailable", fmt.Errorf("%w: database password=synthetic-password", ErrUnavailable), http.StatusServiceUnavailable, ErrUnavailable.Error()},
		{"wrapped authorization", fmt.Errorf("%w: credential=synthetic-credential", ErrAuthorizationRequired), http.StatusForbidden, ErrAuthorizationRequired.Error()},
		{"wrapped emergency stop", fmt.Errorf("%w: private_key=synthetic-key", ErrEmergencyStopActive), http.StatusLocked, ErrEmergencyStopActive.Error()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if recovered := recover(); recovered != nil {
					t.Errorf("schedule handler panicked on an unconfirmed storage result: %v", recovered)
				}
			}()
			authorizer := &recordingFinalEffectAuthorizer{}
			harness := newScheduleSecurityHarness(t, authorizer)
			harness.service.repo = failingCreateTemporalRepository{Repository: harness.repo, err: tc.err}
			request := FollowUpRequest{RunAt: harness.now.Add(time.Hour), Limit: 5}
			encoded, err := json.Marshal(request)
			if err != nil {
				t.Fatal(err)
			}
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Set(identity.ContextSubjectKey, "robert@example.test")
			c.Request = httptest.NewRequest(http.MethodPost, "/temporal/follow-ups", bytes.NewReader(encoded))
			c.Request.Header.Set("Content-Type", "application/json")
			NewHandler(harness.service).ScheduleFollowUp(c)
			var response struct {
				Error string `json:"error"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if rec.Code != tc.status || response.Error != tc.message || strings.Contains(rec.Body.String(), "synthetic-") {
				t.Errorf("unsafe or misclassified scheduling error: %d %s", rec.Code, rec.Body.String())
			}
			if authorizer.calls != 0 || harness.scheduler.calls != 0 {
				t.Fatal("storage failure reached authorization or scheduling")
			}
		})
	}
}

func TestScheduleHandlerPreservesActionableRequestValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	harness := newScheduleSecurityHarness(t, &recordingFinalEffectAuthorizer{})
	for _, tc := range []struct {
		body    string
		message string
	}{
		{`{}`, "runAt is required"},
		{fmt.Sprintf(`{"runAt":%q,"limit":51}`, harness.now.Add(time.Hour).Format(time.RFC3339)), "limit must be between 1 and 50"},
	} {
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Set(identity.ContextSubjectKey, "robert@example.test")
		c.Request = httptest.NewRequest(http.MethodPost, "/temporal/follow-ups", strings.NewReader(tc.body))
		c.Request.Header.Set("Content-Type", "application/json")
		NewHandler(harness.service).ScheduleFollowUp(c)
		var response struct {
			Error string `json:"error"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if rec.Code != http.StatusBadRequest || response.Error != tc.message {
			t.Errorf("request validation context lost: %d %s", rec.Code, rec.Body.String())
		}
	}
}

func TestStatusHandlerFiltersConfiguredCredentialsWithoutChangingServiceConfig(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := NewService(nil, nil, false, "https://synthetic-user:synthetic-password@example.invalid", "passphrase=synthetic-namespace", "private_key=synthetic-queue")
	service.workerErr = "Cookie: session=synthetic-session; csrf=synthetic-csrf"
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	NewHandler(service).Status(c)
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "synthetic-") {
		t.Fatalf("status exposed credentials: %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(service.config.namespace, "synthetic-namespace") || !strings.Contains(service.workerErr, "synthetic-session") {
		t.Fatal("public status sanitization changed internal configuration")
	}
}

func TestScheduleHandlerUncertaintyReturnsInspectableIdentityWithoutUnsafeRetry(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, afterAcceptance := range []bool{false, true} {
		h := newScheduleSecurityHarness(t, &recordingFinalEffectAuthorizer{})
		if afterAcceptance {
			h.service.repo = uncertainScheduleWriteRepository{Repository: h.repo, failTo: "scheduled"}
		} else {
			h.scheduler.err = fmt.Errorf("%w: password=synthetic-provider-secret", context.DeadlineExceeded)
		}
		encoded, err := json.Marshal(FollowUpRequest{RunAt: h.now.Add(time.Hour)})
		if err != nil {
			t.Fatal(err)
		}
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Set(identity.ContextSubjectKey, "robert@example.test")
		c.Request = httptest.NewRequest(http.MethodPost, "/temporal/follow-ups", bytes.NewReader(encoded))
		c.Request.Header.Set("Content-Type", "application/json")
		NewHandler(h.service).ScheduleFollowUp(c)
		var response struct {
			Error      string `json:"error"`
			RetrySafe  *bool  `json:"retrySafe"`
			RunID      string `json:"runId"`
			WorkflowID string `json:"temporalWorkflowId"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if rec.Code != http.StatusServiceUnavailable || response.Error != ErrScheduleUncertain.Error() ||
			response.RetrySafe == nil || *response.RetrySafe || response.RunID != h.scheduler.input.RunID ||
			response.WorkflowID != h.scheduler.options.ID || strings.Contains(rec.Body.String(), "synthetic-") || h.scheduler.calls != 1 {
			t.Fatalf("unsafe/opaque uncertainty response: %d %s calls=%d", rec.Code, rec.Body.String(), h.scheduler.calls)
		}
	}
}

func TestScheduleHandlerFailedDenialWriteIsNotReportedAsDurableRejection(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := newScheduleSecurityHarness(t, &recordingFinalEffectAuthorizer{err: errors.New("denied")})
	h.service.repo = failedScheduleWriteRepository{h.repo}
	encoded, err := json.Marshal(FollowUpRequest{RunAt: h.now.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Set(identity.ContextSubjectKey, "robert@example.test")
	c.Request = httptest.NewRequest(http.MethodPost, "/temporal/follow-ups", bytes.NewReader(encoded))
	c.Request.Header.Set("Content-Type", "application/json")
	NewHandler(h.service).ScheduleFollowUp(c)
	if rec.Code != http.StatusInternalServerError || !strings.Contains(rec.Body.String(), schedulePersistenceFailedMessage) ||
		strings.Contains(rec.Body.String(), "synthetic-") || h.scheduler.calls != 0 || h.repo.lastStatus() != "preparing" {
		t.Fatalf("denial storage failure was obscured: %d %s status=%q", rec.Code, rec.Body.String(), h.repo.lastStatus())
	}
}

func TestWorkerStartHandlerReturnsPendingStateInsteadOfQueuingAnotherAttempt(t *testing.T) {
	gin.SetMode(gin.TestMode)
	s, entered, _, done, cleanup := heldDialStartup(t)
	defer cleanup()
	go func() { defer close(done); s.StartWorker() }()
	waitForStartupBoundary(t, entered, "first dial")
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/temporal/worker/start", nil)
	NewHandler(s).StartWorker(c)
	var state Status
	if err := json.Unmarshal(rec.Body.Bytes(), &state); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusAccepted || !state.WorkerStarting || state.WorkerStarted {
		t.Fatalf("pending startup was opaque or incorrectly ready: %d %s", rec.Code, rec.Body.String())
	}
}

func TestWorkerStartHandlerPropagatesRequestCancellation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := newStartupBoundaryHarness()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered := make(chan struct{})
	var once sync.Once
	h.service.dial = func(ctx context.Context, _ client.Options) (client.Client, error) {
		once.Do(func() { close(entered) })
		cancel()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Second):
			return nil, errors.New("handler ignored request cancellation")
		}
	}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/temporal/worker/start", nil).WithContext(ctx)
	NewHandler(h.service).StartWorker(c)
	if rec.Code != http.StatusServiceUnavailable || h.factories.Load() != 0 || h.worker.starts.Load() != 0 {
		t.Fatalf("canceled HTTP start was accepted: %d %s", rec.Code, rec.Body.String())
	}
	select {
	case <-entered:
	default:
		t.Fatal("handler did not exercise the controlled SDK dial")
	}
}
