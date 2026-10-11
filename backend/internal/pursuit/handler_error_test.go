package pursuit

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"automation-hub-backend/internal/identity"
	"automation-hub-backend/internal/models"

	"github.com/gin-gonic/gin"
)

type failingPursuitListService struct {
	Service
	err error
}

func (s failingPursuitListService) ListForOwner(string, bool) ([]models.Pursuit, error) {
	return nil, s.err
}

func TestListDoesNotExposeUnexpectedRepositoryErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewHandler(failingPursuitListService{err: errors.New(`postgres password=not-for-http at C:\\private`)})
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(identity.ContextSubjectKey, "alice")
		c.Next()
	})
	router.GET("/pursuits", handler.List)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/pursuits", nil))
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusInternalServerError, recorder.Body.String())
	}
	for _, forbidden := range []string{"password", "not-for-http", "C:\\\\private"} {
		if strings.Contains(strings.ToLower(recorder.Body.String()), strings.ToLower(forbidden)) {
			t.Fatalf("response leaked %q: %s", forbidden, recorder.Body.String())
		}
	}
	if !strings.Contains(recorder.Body.String(), "pursuits are unavailable") {
		t.Fatalf("response lacks stable error: %s", recorder.Body.String())
	}
}
