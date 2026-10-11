package opscontrol

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"automation-hub-backend/internal/identity"

	"github.com/gin-gonic/gin"
)

type emergencyStopFanoutFunc func(context.Context) (int, int, int, int, error)

func (f emergencyStopFanoutFunc) FanOutEmergencyStop(ctx context.Context) (int, int, int, int, error) {
	return f(ctx)
}

func TestPausePersistsStopBeforeFanoutAndReportsPartialDelivery(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := newTestService(t)
	called := false
	service.WithOpenClawEmergencyStopFanout(emergencyStopFanoutFunc(func(context.Context) (int, int, int, int, error) {
		called = true
		if !service.Control().EmergencyStop() {
			t.Fatal("OpenClaw fan-out ran before the persisted emergency stop was engaged")
		}
		return 2, 1, 1, 0, errors.New("one delivery was not confirmed")
	}))

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(identity.ContextSubjectKey, "local-operator")
		c.Next()
	})
	router.POST("/pause", NewHandler(service).Pause)
	request := httptest.NewRequest(http.MethodPost, "/pause", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if !called || response.Code != http.StatusMultiStatus || !service.Control().EmergencyStop() {
		t.Fatalf("Pause must retain the stop and return partial delivery: called=%v status=%d state=%#v body=%s", called, response.Code, service.Control().EmergencyState(), response.Body.String())
	}
	var body struct {
		EmergencyStop EmergencyStopState         `json:"emergencyStop"`
		Cancellation  EmergencyStopFanoutSummary `json:"openClawCancellation"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode Pause response: %v", err)
	}
	if !body.EmergencyStop.Engaged || body.Cancellation.Status != "partial" || body.Cancellation.Targeted != 2 ||
		body.Cancellation.Acknowledged != 1 || body.Cancellation.AwaitingIdentity != 1 || body.Cancellation.AcknowledgmentIsTerminal {
		t.Fatalf("Pause response overstated cancellation or terminal status: %#v", body)
	}
}

func TestVerificationDoesNotCancelLiveOpenClawRuns(t *testing.T) {
	service := newTestService(t)
	service.SetBackgroundRunner(func(ctx context.Context) (int, error) {
		if !service.Control().EmergencyStop() {
			t.Fatal("verification runner must observe the temporary local stop")
		}
		return 0, nil
	})
	fanoutCalls := 0
	service.WithOpenClawEmergencyStopFanout(emergencyStopFanoutFunc(func(context.Context) (int, int, int, int, error) {
		fanoutCalls++
		return 1, 1, 0, 0, nil
	}))

	result, err := service.VerifyEmergencyStop(context.Background())
	if err != nil {
		t.Fatalf("verify emergency stop: %v", err)
	}
	if !result.Halted || fanoutCalls != 0 || service.Control().EmergencyStop() {
		t.Fatalf("verification must be local-only and restore prior state: result=%#v fanoutCalls=%d emergency=%v", result, fanoutCalls, service.Control().EmergencyStop())
	}

	_, summary, err := service.EngageEmergencyStopWithFanout(context.Background(), "operator request", "operator")
	if err != nil {
		t.Fatalf("explicit emergency stop fan-out: %v", err)
	}
	if fanoutCalls != 1 || summary.Targeted != 1 || !service.Control().EmergencyStop() {
		t.Fatalf("explicit stop must retain its cancellation path: fanoutCalls=%d summary=%#v emergency=%v", fanoutCalls, summary, service.Control().EmergencyStop())
	}
}
