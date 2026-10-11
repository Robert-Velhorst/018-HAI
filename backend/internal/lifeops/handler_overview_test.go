package lifeops

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"automation-hub-backend/internal/identity"

	"github.com/gin-gonic/gin"
)

func TestOverviewReturnsAnOwnerScopedEmptyCapacityState(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(identity.ContextSubjectKey, "owner-a")
		c.Next()
	})
	router.GET("/life/overview", NewHandler(NewService(nil)).Overview)

	request := httptest.NewRequest(http.MethodGet, "/life/overview", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("overview status = %d body %s", response.Code, response.Body.String())
	}

	var overview LifeOverview
	if err := json.Unmarshal(response.Body.Bytes(), &overview); err != nil {
		t.Fatal(err)
	}
	if len(overview.Domains) != 24 {
		t.Fatalf("domains = %d, want 24", len(overview.Domains))
	}
	if overview.Capacity != nil {
		t.Fatalf("capacity = %#v, want nil when no snapshot exists", overview.Capacity)
	}
	if len(overview.Needs) != 0 || len(overview.Goals) != 0 || len(overview.Forest) != 0 {
		t.Fatalf("unexpected empty overview content: %#v", overview)
	}
}
