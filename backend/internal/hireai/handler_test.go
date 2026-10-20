package hireai

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestSearchHandlerRejectsWrongContentTypeAndUnknownFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.POST("/jobs/search", NewHandler(NewService(Config{}, nil)).Search)

	wrongType := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/jobs/search", strings.NewReader(`{}`))
	request.Header.Set("Content-Type", "application/jsonp")
	engine.ServeHTTP(wrongType, request)
	if wrongType.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("wrong content type status = %d", wrongType.Code)
	}

	unknownField := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "/jobs/search", strings.NewReader(`{"filters":{"remoteOnly":false}}`))
	request.Header.Set("Content-Type", "application/json; charset=utf-8")
	engine.ServeHTTP(unknownField, request)
	if unknownField.Code != http.StatusBadRequest {
		t.Fatalf("unknown field status = %d", unknownField.Code)
	}
}

func TestSearchHandlerEnforcesRequestSizeAndNoStore(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.POST("/jobs/search", NewHandler(NewService(Config{}, nil)).Search)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/jobs/search", strings.NewReader(`{"filters":{"query":"`+strings.Repeat("x", maxRequest)+`"}}`))
	request.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized body status = %d", recorder.Code)
	}
	if recorder.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("cache control = %q", recorder.Header().Get("Cache-Control"))
	}
}
