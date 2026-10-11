package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHealthHandlerReportsConsumerReadiness(t *testing.T) {
	for _, tc := range []struct {
		name       string
		ready      bool
		wantStatus int
		wantBody   string
	}{
		{name: "ready", ready: true, wantStatus: http.StatusOK, wantBody: `"status":"ok"`},
		{name: "not ready", ready: false, wantStatus: http.StatusServiceUnavailable, wantBody: `"status":"not_ready"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
			response := httptest.NewRecorder()

			healthHandler(func() bool { return tc.ready }).ServeHTTP(response, request)

			if response.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", response.Code, tc.wantStatus)
			}
			if !strings.Contains(response.Body.String(), tc.wantBody) {
				t.Fatalf("body = %q, want %s", response.Body.String(), tc.wantBody)
			}
			if contentType := response.Header().Get("Content-Type"); contentType != "application/json" {
				t.Fatalf("Content-Type = %q, want application/json", contentType)
			}
		})
	}
}
