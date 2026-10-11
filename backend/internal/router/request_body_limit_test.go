package router

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestJSONRequestBodyLimitRejectsDeclaredOversizeBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(jsonRequestBodyLimitMiddleware(8))
	router.POST("/input", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	req := httptest.NewRequest(http.MethodPost, "/input", strings.NewReader(`{"payload":"too-large"}`))
	req.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)

	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestJSONRequestBodyLimitBoundsChunkedBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(jsonRequestBodyLimitMiddleware(8))
	router.POST("/input", func(c *gin.Context) {
		_, err := io.ReadAll(c.Request.Body)
		var maxBytesErr *http.MaxBytesError
		if !errors.As(err, &maxBytesErr) {
			c.Status(http.StatusInternalServerError)
			return
		}
		c.Status(http.StatusRequestEntityTooLarge)
	})

	req := httptest.NewRequest(http.MethodPost, "/input", strings.NewReader(`{"payload":"too-large"}`))
	req.ContentLength = -1
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)

	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestJSONRequestBodyLimitLeavesMultipartAndReadsUntouched(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(jsonRequestBodyLimitMiddleware(8))
	router.POST("/input", func(c *gin.Context) {
		body, err := io.ReadAll(c.Request.Body)
		if err != nil || string(body) != "multipart body" {
			c.Status(http.StatusInternalServerError)
			return
		}
		c.Status(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodPost, "/input", strings.NewReader("multipart body"))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=test")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)

	if response.Code != http.StatusNoContent {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}
