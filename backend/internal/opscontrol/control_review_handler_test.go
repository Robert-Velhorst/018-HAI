package opscontrol

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"automation-hub-backend/internal/task"

	"github.com/gin-gonic/gin"
)

func TestRequestResumeApprovalHandlerCreatesDurableReview(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := newTestService(t)
	service.WithControlReviewRepository(task.NewMemoryTaskStateRepository())
	if _, err := service.EngageEmergencyStop("test stop", service.owner); err != nil {
		t.Fatalf("engage emergency stop: %v", err)
	}
	handler := NewHandler(service)
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set("subject", service.owner) })
	router.POST("/resume-approval", handler.RequestResumeApproval)

	request := httptest.NewRequest(http.MethodPost, "/resume-approval", nil)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d: %s", recorder.Code, http.StatusCreated, recorder.Body.String())
	}
	pending := service.reviews.(task.PendingReviewStateRepository)
	items, err := pending.ListPendingReviewItems(service.owner, 10)
	if err != nil || len(items) != 1 {
		t.Fatalf("resume approval was not persisted: %s", recorder.Body.String())
	}
}

func TestPauseHandlerRejectsMalformedRequestWithoutEngagingEmergencyStop(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := newTestService(t)
	handler := NewHandler(service)
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set("subject", service.owner) })
	router.POST("/pause", handler.Pause)

	request := httptest.NewRequest(http.MethodPost, "/pause", strings.NewReader(`{"reason":`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d: %s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
	}
	if service.Control().EmergencyState().Engaged {
		t.Fatal("malformed request engaged the emergency stop")
	}
}

func TestApproveAndResumeHandlerConsumesPreparedReview(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := newTestService(t)
	service.WithExecutionAuthorizer(allowExactControlAuthorization(service.now))
	service.WithControlReviewRepository(task.NewMemoryTaskStateRepository())
	if _, err := service.EngageEmergencyStop("test stop", service.owner); err != nil {
		t.Fatalf("engage emergency stop: %v", err)
	}
	pending, err := service.RequestResumeApproval(t.Context(), service.owner)
	if err != nil {
		t.Fatalf("request approval: %v", err)
	}
	handler := NewHandler(service)
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set("subject", service.owner) })
	router.POST("/resume-approval/:id/approve-and-resume", handler.ApproveAndResume)

	request := httptest.NewRequest(
		http.MethodPost,
		"/resume-approval/"+pending.ReviewItemID+"/approve-and-resume",
		strings.NewReader(`{"confirmation":"RESUME BACKGROUND PROCESSING"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var response struct {
		EmergencyStop EmergencyStopState `json:"emergencyStop"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.EmergencyStop.Engaged {
		t.Fatal("approved review did not resume processing")
	}
}
