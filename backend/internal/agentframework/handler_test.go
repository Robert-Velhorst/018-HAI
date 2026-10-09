package agentframework

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

type handlerServiceStub struct {
	response     *Response
	proposeErr   error
	proposeCalls int
}

func (s *handlerServiceStub) Status() Status { return Status{} }

func (s *handlerServiceStub) Probe(context.Context) (*ProbeResult, error) {
	return nil, nil
}

func (s *handlerServiceStub) Propose(context.Context, Request) (*Response, error) {
	s.proposeCalls++
	return s.response, s.proposeErr
}

func TestWriteContextErrorKeepsCancellationAndTimeoutTruthful(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantBody   bool
	}{
		{name: "client cancellation", err: context.Canceled, wantStatus: 200},
		{name: "request timeout", err: errors.Join(errors.New("runner request"), context.DeadlineExceeded), wantStatus: 504, wantBody: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			if !writeContextError(c, test.err) {
				t.Fatal("context failure was not handled")
			}
			if recorder.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d", recorder.Code, test.wantStatus)
			}
			if gotBody := recorder.Body.Len() > 0; gotBody != test.wantBody {
				t.Fatalf("response body present = %t, want %t", gotBody, test.wantBody)
			}
		})
	}
}

func TestProposeHandlerStopsImmediatelyForCancelledRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	request := httptest.NewRequest("POST", "/proposals", nil).WithContext(ctx)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = request

	NewHandler(nil).Propose(c)
	if recorder.Body.Len() != 0 {
		t.Fatalf("cancelled request wrote a response body: %s", recorder.Body.String())
	}
}

func TestProposeHandlerReturnsBoundedFailureForNilResultWithoutError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &handlerServiceStub{}
	request := httptest.NewRequest(http.MethodPost, "/proposals", strings.NewReader(`{"request":"Prepare a bounded plan"}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = request

	NewHandler(service).Propose(c)

	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusBadGateway, recorder.Body.String())
	}
	if recorder.Body.Len() > 512 || strings.Contains(recorder.Body.String(), "panic") {
		t.Fatalf("failure response is not bounded and generic: %s", recorder.Body.String())
	}
	if service.proposeCalls != 1 {
		t.Fatalf("Propose called %d times, want 1", service.proposeCalls)
	}
}

func TestProposeHandlerMapsOversizedBodyToRequestEntityTooLarge(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &handlerServiceStub{}
	body := `{"request":"` + strings.Repeat("x", 16<<10) + `"}`
	request := httptest.NewRequest(http.MethodPost, "/proposals", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = request

	NewHandler(service).Propose(c)

	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusRequestEntityTooLarge, recorder.Body.String())
	}
	if service.proposeCalls != 0 {
		t.Fatalf("oversized request reached service %d times, want 0", service.proposeCalls)
	}
}

func TestProposeHandlerRejectsOversizedSuffixesAndMultipleJSONValues(t *testing.T) {
	gin.SetMode(gin.TestMode)
	valid := `{"request":"Prepare a bounded plan"}`
	tests := []struct {
		name       string
		body       string
		wantStatus int
	}{
		{
			name:       "oversized trailing whitespace",
			body:       valid + strings.Repeat(" ", 16<<10),
			wantStatus: http.StatusRequestEntityTooLarge,
		},
		{
			name:       "oversized trailing junk",
			body:       valid + strings.Repeat("x", 16<<10),
			wantStatus: http.StatusRequestEntityTooLarge,
		},
		{
			name:       "duplicate JSON values",
			body:       valid + valid,
			wantStatus: http.StatusBadRequest,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := &handlerServiceStub{}
			request := httptest.NewRequest(http.MethodPost, "/proposals", strings.NewReader(test.body))
			request.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = request

			NewHandler(service).Propose(c)

			if recorder.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", recorder.Code, test.wantStatus, recorder.Body.String())
			}
			if service.proposeCalls != 0 {
				t.Fatalf("Propose called %d times for rejected JSON, want 0", service.proposeCalls)
			}
		})
	}
}

func TestProposeHandlerAcceptsTrailingWhitespaceAfterOneJSONValue(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &handlerServiceStub{response: &Response{Status: "proposed"}}
	request := httptest.NewRequest(http.MethodPost, "/proposals", strings.NewReader(`{"request":"Prepare a bounded plan"}`+" \t\r\n"))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = request

	NewHandler(service).Propose(c)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if service.proposeCalls != 1 {
		t.Fatalf("Propose called %d times, want 1", service.proposeCalls)
	}
}
