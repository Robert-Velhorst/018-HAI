package router

import (
	"bytes"
	"errors"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestPrivateRecoveryDoesNotEmitPanicHeadersOrCookies(t *testing.T) {
	previousMode := gin.Mode()
	t.Cleanup(func() { gin.SetMode(previousMode) })
	for _, mode := range []string{gin.DebugMode, gin.ReleaseMode} {
		t.Run(mode, func(t *testing.T) {
			gin.SetMode(mode)
			var output bytes.Buffer
			previousWriter := log.Writer()
			previousErrorWriter := gin.DefaultErrorWriter
			log.SetOutput(&output)
			gin.DefaultErrorWriter = &output
			t.Cleanup(func() { log.SetOutput(previousWriter); gin.DefaultErrorWriter = previousErrorWriter })
			engine := gin.New()
			engine.Use(privateRecovery())
			engine.GET("/panic", func(*gin.Context) { panic("synthetic-panic-secret") })
			request := httptest.NewRequest(http.MethodGet, "/panic?token=synthetic-query-secret", nil)
			request.Header.Set("Cookie", "session=synthetic-cookie-secret")
			request.Header.Set("X-Private-Header", "synthetic-header-secret")
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, request)
			if response.Code != http.StatusInternalServerError || strings.TrimSpace(response.Body.String()) != `{"error":"internal server error"}` {
				t.Fatalf("unexpected response: %d %q", response.Code, response.Body.String())
			}
			if !strings.Contains(output.String(), "HAI identity request panic recovered") {
				t.Fatal("safe panic diagnostic missing")
			}
			if strings.Contains(output.String(), "synthetic-") || strings.Contains(response.Body.String(), "synthetic-") {
				t.Fatal("private panic/request information escaped")
			}
		})
	}
}

func TestAccessLogSuppressesErrorPayloadsOutsideResetRoutes(t *testing.T) {
	for _, path := range []string{"/api/v1/auth/login", "/api/v1/user/", "/readyz"} {
		line := formatAccessLog(gin.LogFormatterParams{Path: path, StatusCode: 500, ErrorMessage: "password=synthetic-log-secret"})
		if strings.Contains(line, "synthetic-log-secret") || !strings.Contains(line, "[redacted]") {
			t.Fatal("access log leaked error detail")
		}
		if !strings.Contains(line, path) {
			t.Fatal("non-sensitive route omitted")
		}
	}
}

func TestPrivateRecoveryBrokenConnectionDoesNotLeakError(t *testing.T) {
	previousMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(previousMode) })
	var output bytes.Buffer
	engine := gin.New()
	engine.Use(gin.LoggerWithConfig(gin.LoggerConfig{Output: &output, Formatter: formatAccessLog}), privateRecovery())
	engine.GET("/broken", func(*gin.Context) {
		panic(&net.OpError{Op: "write", Err: &os.SyscallError{Syscall: "write", Err: errors.New("broken pipe synthetic-connection-secret")}})
	})
	engine.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/broken", nil))
	if strings.Contains(output.String(), "synthetic-connection-secret") || !strings.Contains(output.String(), "[redacted]") {
		t.Fatal("broken connection error leaked via access log")
	}
}
