package lmeval

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

type stubService struct{ Service }

func (s stubService) Status() Status                                { return Status{Provider: "LM Evaluation Harness local runner"} }
func (s stubService) Probe(_ context.Context) (*ProbeResult, error) { return nil, ErrNotConfigured }
func (s stubService) Run(_ context.Context) (*Result, error)        { return nil, ErrNotConfigured }

func TestHandlerDoesNotAcceptArbitraryModelOrSuiteParameters(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/run", NewHandler(stubService{}).Run)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/run?model=external&suite=anything", nil))
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("run endpoint must ignore caller-selected model or suite: %d", recorder.Code)
	}
	recorder = httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/run", strings.NewReader(`{"model":"external"}`))
	request.ContentLength = -1
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("run endpoint must reject caller-supplied run payloads: %d", recorder.Code)
	}
}
