package router

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

type testReadiness struct{ err error }

func (r testReadiness) Readiness(context.Context) error { return r.err }

func TestReadinessHandlerReportsDependenciesWithoutLeakingErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name       string
		checker    serviceReadiness
		wantStatus int
		wantBody   string
	}{
		{name: "ready", checker: testReadiness{}, wantStatus: http.StatusOK, wantBody: `{"status":"ready"}`},
		{name: "unavailable", checker: testReadiness{err: errors.New("redis password must not be exposed")}, wantStatus: http.StatusServiceUnavailable, wantBody: `{"status":"not_ready"}`},
		{name: "missing checker", checker: nil, wantStatus: http.StatusServiceUnavailable, wantBody: `{"status":"not_ready"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := gin.New()
			router.GET("/readyz", readinessHandler(tt.checker))
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/readyz", nil))
			if recorder.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", recorder.Code, tt.wantStatus)
			}
			if strings.TrimSpace(recorder.Body.String()) != tt.wantBody {
				t.Fatalf("body = %q, want %q", recorder.Body.String(), tt.wantBody)
			}
		})
	}
}

func TestTrustedGatewayClientIPMiddlewareUsesForwardedIPOnlyFromGateway(t *testing.T) {
	gin.SetMode(gin.TestMode)
	lookup := func(context.Context) ([]net.IP, error) {
		return []net.IP{net.ParseIP("172.28.0.2")}, nil
	}
	newTestRouter := func() *gin.Engine {
		router := gin.New()
		if err := router.SetTrustedProxies(nil); err != nil {
			t.Fatal(err)
		}
		router.Use(newTrustedGatewayClientIPMiddleware(lookup))
		router.GET("/client-ip", func(c *gin.Context) { c.String(http.StatusOK, c.ClientIP()) })
		return router
	}

	t.Run("gateway forwarding the single overwritten client IP", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodGet, "/client-ip", nil)
		request.RemoteAddr = "172.28.0.2:8080"
		request.Header.Set("X-Forwarded-For", "198.51.100.24")
		request.Header.Set("X-Real-IP", "203.0.113.99")
		recorder := httptest.NewRecorder()
		newTestRouter().ServeHTTP(recorder, request)
		if recorder.Body.String() != "198.51.100.24" {
			t.Fatalf("trusted gateway client IP = %q, want 198.51.100.24", recorder.Body.String())
		}
	})

	t.Run("untrusted peer cannot spoof forwarding headers", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodGet, "/client-ip", nil)
		request.RemoteAddr = "172.28.0.3:8080"
		request.Header.Set("X-Forwarded-For", "198.51.100.24")
		request.Header.Set("X-Real-IP", "203.0.113.99")
		request.Header.Set("Forwarded", "for=198.51.100.24")
		recorder := httptest.NewRecorder()
		newTestRouter().ServeHTTP(recorder, request)
		if recorder.Body.String() != "172.28.0.3" {
			t.Fatalf("untrusted peer client IP = %q, want socket peer 172.28.0.3", recorder.Body.String())
		}
	})

	t.Run("gateway proxy-chain header has no trusted identity", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodGet, "/client-ip", nil)
		request.RemoteAddr = "172.28.0.2:8080"
		request.Header.Set("X-Forwarded-For", "198.51.100.24, 203.0.113.99")
		recorder := httptest.NewRecorder()
		newTestRouter().ServeHTTP(recorder, request)
		if recorder.Body.String() != "" {
			t.Fatalf("multi-value forwarded IP = %q, want no attributable identity", recorder.Body.String())
		}
	})

	t.Run("gateway duplicate forwarded headers have no trusted identity", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodGet, "/client-ip", nil)
		request.RemoteAddr = "172.28.0.2:8080"
		request.Header.Add("X-Forwarded-For", "198.51.100.24")
		request.Header.Add("X-Forwarded-For", "203.0.113.9")
		recorder := httptest.NewRecorder()
		newTestRouter().ServeHTTP(recorder, request)
		if recorder.Body.String() != "" {
			t.Fatalf("duplicate forwarded IP = %q, want no attributable identity", recorder.Body.String())
		}
	})

	t.Run("gateway missing forwarded client IP has no trusted identity", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodGet, "/client-ip", nil)
		request.RemoteAddr = "172.28.0.2:8080"
		recorder := httptest.NewRecorder()
		newTestRouter().ServeHTTP(recorder, request)
		if recorder.Body.String() != "" {
			t.Fatalf("missing forwarded IP = %q, want no attributable identity", recorder.Body.String())
		}
	})
}

func TestTrustedGatewayClientIPMiddlewareFailsClosedWhenGatewayCannotResolve(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	if err := router.SetTrustedProxies(nil); err != nil {
		t.Fatal(err)
	}
	router.Use(newTrustedGatewayClientIPMiddleware(func(context.Context) ([]net.IP, error) {
		return nil, net.ErrClosed
	}))
	router.GET("/client-ip", func(c *gin.Context) { c.String(http.StatusOK, c.ClientIP()) })

	request := httptest.NewRequest(http.MethodGet, "/client-ip", nil)
	request.RemoteAddr = "172.28.0.2:8080"
	request.Header.Set("X-Forwarded-For", "198.51.100.24")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Body.String() != "172.28.0.2" {
		t.Fatalf("client IP with gateway resolution failure = %q, want socket peer 172.28.0.2", recorder.Body.String())
	}
}

func TestAccessLogPathRedactsPasswordResetTokenAndQuery(t *testing.T) {
	got := accessLogPath("/api/v1/auth/confirm-password-reset/secret-token?debug=secret-query")
	if got != "/api/v1/auth/confirm-password-reset/:reset-token" {
		t.Fatalf("accessLogPath() = %q, want the redacted route pattern", got)
	}
}

func TestAccessLogPathDoesNotNeedResetBearerInRoute(t *testing.T) {
	got := accessLogPath("/api/v1/auth/confirm-password-reset?debug=secret-query")
	if got != "/api/v1/auth/confirm-password-reset" {
		t.Fatalf("accessLogPath() = %q, want request path without query", got)
	}
}

func TestAccessLogPathDropsQueryForAllRoutes(t *testing.T) {
	got := accessLogPath("/api/v1/auth/request-password-reset?email=private@example.test")
	if got != "/api/v1/auth/request-password-reset" {
		t.Fatalf("accessLogPath() = %q, want no query string", got)
	}
}

func TestAccessLogFormatterDoesNotEmitResetTokenOrErrorDetails(t *testing.T) {
	const token = "secret-reset-token"
	line := formatAccessLog(gin.LogFormatterParams{
		TimeStamp:    time.Date(2026, 9, 24, 0, 0, 0, 0, time.UTC),
		StatusCode:   400,
		Latency:      time.Millisecond,
		ClientIP:     "127.0.0.1",
		Method:       "POST",
		Path:         "/api/v1/auth/confirm-password-reset/" + token,
		ErrorMessage: "invalid reset token: " + token,
	})
	if strings.Contains(line, token) || !strings.Contains(line, ":reset-token") || !strings.Contains(line, "[redacted]") {
		t.Fatalf("access log leaked reset-token details: %q", line)
	}
}
