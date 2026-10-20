package source

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"automation-hub-backend/internal/identity"
	"automation-hub-backend/internal/models"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type extractionHistoryHandlerService struct {
	Service
	owner   string
	request HistoryPageRequest
	calls   int
}

type extractionHistoryScopeRepository struct {
	*fakeSourceRepo
	sourceIDs []uuid.UUID
}

func (r *extractionHistoryScopeRepository) FindExtractionsPageForSources(ids []uuid.UUID, _ string, _ bool, _, _ int) ([]models.SourceExtraction, int, error) {
	r.sourceIDs = append([]uuid.UUID(nil), ids...)
	return []models.SourceExtraction{}, 0, nil
}

func (r *extractionHistoryScopeRepository) FindSyncJobsPageForSources([]uuid.UUID, int, int) ([]models.SourceSyncJob, int, error) {
	panic("unexpected sync-job paging in extraction test")
}

func (r *extractionHistoryScopeRepository) FindAuditLogsPageForSources([]uuid.UUID, int, int) ([]models.SourceAuditLog, int, error) {
	panic("unexpected audit paging in extraction test")
}

func (s *extractionHistoryHandlerService) ExtractionsForOwnerPage(owner, project string, archived bool, request HistoryPageRequest) (*ExtractionPage, error) {
	s.owner = owner
	s.request = request
	s.calls++
	return &ExtractionPage{Items: []models.SourceExtraction{{ID: uuid.MustParse("10000000-0000-0000-0000-000000000002")}}, TotalCount: 3, Total: 3, Limit: 1, Offset: request.Offset, HasMore: true}, nil
}

func TestExtractionHistoryHTTPUsesOwnerScopedPaging(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &extractionHistoryHandlerService{Service: NewService(newFakeSourceRepo(), nil)}
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set(identity.ContextSubjectKey, "alice") })
	router.GET("/extractions", NewHandler(service).Extractions)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/extractions?offset=1&limit=1", nil))
	require.Equal(t, http.StatusOK, response.Code)
	var page ExtractionPage
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &page))
	require.Equal(t, "alice", service.owner)
	require.Equal(t, HistoryPageRequest{Limit: 1, Offset: 1}, service.request)
	require.Equal(t, 3, page.Total)
	require.Equal(t, int64(3), page.TotalCount)
	require.Equal(t, 1, page.Offset)
	require.True(t, page.HasMore)
	require.Len(t, page.Items, 1)
	require.Equal(t, "1", response.Header().Get("X-Result-Offset"))
}

func TestExtractionHistoryHTTPRejectsInvalidOffsetsBeforePaging(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, query := range []string{"offset=-1", "offset=", "offset=invalid", "offset=1000001", "offset=1&offset=2"} {
		t.Run(query, func(t *testing.T) {
			service := &extractionHistoryHandlerService{Service: NewService(newFakeSourceRepo(), nil)}
			router := gin.New()
			router.Use(func(c *gin.Context) { c.Set(identity.ContextSubjectKey, "alice") })
			router.GET("/extractions", NewHandler(service).Extractions)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/extractions?"+query, nil))
			require.Equal(t, http.StatusBadRequest, response.Code)
			require.Zero(t, service.calls)
		})
	}
}

func TestExtractionHistoryScopeExcludesUnownedLegacySources(t *testing.T) {
	ownID, otherID, legacyID := uuid.New(), uuid.New(), uuid.New()
	repo := &extractionHistoryScopeRepository{fakeSourceRepo: newFakeSourceRepo(
		&models.ConnectedSource{ID: ownID, OwnerIdentity: "alice"},
		&models.ConnectedSource{ID: otherID, OwnerIdentity: "bob"},
		&models.ConnectedSource{ID: legacyID},
	)}
	service := NewService(repo, nil).(*service)
	ids, err := service.extractionSourceIDsForOwner(" alice ")
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{ownID}, ids)
	_, err = service.ExtractionsForOwnerPage("alice", "project", false, HistoryPageRequest{Limit: 8, Offset: 8})
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{ownID}, repo.sourceIDs, "paged repository must receive only owned sources")
	_, err = service.extractionSourceIDsForOwner(" ")
	require.Error(t, err)
}
