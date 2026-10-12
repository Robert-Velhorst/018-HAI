package openclawreconcile

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"automation-hub-backend/internal/identity"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func TestArtifactRetainReturnsUnavailableWhenArchiveIsNotConfigured(t *testing.T) {
	gin.SetMode(gin.TestMode)
	reader := &artifactViewFake{}
	downloader := &artifactDownloaderFake{}
	handler := NewArtifactHandler(reader, downloader)
	engine := gin.New()
	engine.Use(func(c *gin.Context) { c.Set(identity.ContextSubjectKey, "alice") })
	engine.POST("/events/:eventId/artifacts/:digest/retain", handler.Retain)

	eventID := uuid.NewString()
	digest := strings.Repeat("a", 64)
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/events/"+eventID+"/artifacts/"+digest+"/retain", nil))

	if response.Code != http.StatusServiceUnavailable || !strings.Contains(response.Body.String(), "Encrypted artifact retention is not configured") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if reader.calls != 0 || downloader.calls != 0 {
		t.Fatalf("retention lookup or download started without an archive: reader=%d downloader=%d", reader.calls, downloader.calls)
	}
}
