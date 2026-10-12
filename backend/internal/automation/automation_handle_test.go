package automation

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"automation-hub-backend/internal/identity"
	"automation-hub-backend/internal/models"

	"github.com/gin-gonic/gin"
)

type updateOnlyAutomationService struct{ Service }

func (updateOnlyAutomationService) Update(automation *models.Automation) (*models.Automation, error) {
	return automation, nil
}

func TestAutomationUpdateRejectsOversizedRequestBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.PATCH("/automation", NewHandler(nil).Update)

	request := httptest.NewRequest(
		http.MethodPatch,
		"/automation",
		bytes.NewBufferString(strings.Repeat("x", int(maxAutomationUpdateBodyBytes+1))),
	)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized update status = %d, want %d: %s", response.Code, http.StatusRequestEntityTooLarge, response.Body.String())
	}
}

func TestAutomationCreateRejectsOversizedMultipartRequestBeforeParsing(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/automation", NewHandler(nil).Create)

	request := httptest.NewRequest(
		http.MethodPost,
		"/automation",
		bytes.NewBufferString(strings.Repeat("x", 7<<20)),
	)
	request.Header.Set("Content-Type", "multipart/form-data; boundary=hai-test")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized create status = %d, want %d: %s", response.Code, http.StatusRequestEntityTooLarge, response.Body.String())
	}
}

func TestAutomationRoutesRequireVerifiedOperator(t *testing.T) {
	gin.SetMode(gin.TestMode)

	unauthenticated := gin.New()
	unauthenticatedRoutes := unauthenticated.Group("/automation")
	unauthenticatedRoutes.Use(RequireAuthenticatedOperator())
	unauthenticatedRoutes.GET("/", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	unauthenticatedRecorder := httptest.NewRecorder()
	unauthenticated.ServeHTTP(unauthenticatedRecorder, httptest.NewRequest(http.MethodGet, "/automation/", nil))
	if unauthenticatedRecorder.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated automation route status = %d, want %d: %s", unauthenticatedRecorder.Code, http.StatusUnauthorized, unauthenticatedRecorder.Body.String())
	}

	authenticated := gin.New()
	authenticated.Use(func(c *gin.Context) {
		c.Set(identity.ContextSubjectKey, "robert")
		c.Next()
	})
	authenticatedRoutes := authenticated.Group("/automation")
	authenticatedRoutes.Use(RequireAuthenticatedOperator())
	authenticatedRoutes.GET("/", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	authenticatedRecorder := httptest.NewRecorder()
	authenticated.ServeHTTP(authenticatedRecorder, httptest.NewRequest(http.MethodGet, "/automation/", nil))
	if authenticatedRecorder.Code != http.StatusNoContent {
		t.Fatalf("authenticated automation route status = %d, want %d: %s", authenticatedRecorder.Code, http.StatusNoContent, authenticatedRecorder.Body.String())
	}
}

func TestUpdateRejectsOversizedRequestBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.PATCH("/automation", NewHandler(updateOnlyAutomationService{}).Update)

	recorder := httptest.NewRecorder()
	body := bytes.Repeat([]byte("x"), maxAutomationUpdateBodyBytes+1)
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPatch, "/automation", bytes.NewReader(body)))

	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d: %s", recorder.Code, http.StatusRequestEntityTooLarge, recorder.Body.String())
	}
}

func TestCreateRejectsOversizedDeclaredMultipartBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/automation", NewHandler(updateOnlyAutomationService{}).Create)

	req := httptest.NewRequest(http.MethodPost, "/automation", bytes.NewReader([]byte("not-read")))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=test")
	req.ContentLength = maxAutomationCreateBodyBytes() + 1
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d: %s", recorder.Code, http.StatusRequestEntityTooLarge, recorder.Body.String())
	}
}
