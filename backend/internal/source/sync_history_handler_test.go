package source

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"automation-hub-backend/internal/identity"
	"automation-hub-backend/internal/models"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type syncHistoryHandlerService struct {
	Service
	owner   string
	request HistoryPageRequest
	calls   int
	err     error
}

func (s *syncHistoryHandlerService) SyncJobsForOwnerPage(owner string, _ *uuid.UUID, request HistoryPageRequest) (*SyncJobPage, error) {
	s.owner, s.request = owner, request
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	return &SyncJobPage{Items: []models.SourceSyncJob{}, Total: 0, Limit: request.Limit, Offset: request.Offset}, nil
}

func syncHistoryRequest(service Service, owner, query string) *httptest.ResponseRecorder {
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set(identity.ContextSubjectKey, owner) })
	router.GET("/sync-jobs", NewHandler(service).SyncJobs)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/sync-jobs?"+query, nil))
	return response
}

func TestSyncHistoryHTTPUsesAuthenticatedOwnerAndBounds(t *testing.T) {
	reader := &syncHistoryHandlerService{Service: NewService(newFakeSourceRepo(), nil)}
	response := syncHistoryRequest(reader, "alice", "offset=6&limit=6&owner=bob")
	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, "alice", reader.owner)
	require.Equal(t, HistoryPageRequest{Limit: 6, Offset: 6}, reader.request)
	var page SyncJobPage
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &page))
	require.Equal(t, 6, page.Offset)
	require.Equal(t, 6, page.Limit)
	require.Equal(t, "6", response.Header().Get("X-Result-Offset"))
	for _, query := range []string{"offset=", "offset=-1", "offset=x", "offset=1000001", "offset=0&offset=6", "offset=0&limit=0", "offset=0&limit=251", "offset=0&limit=6&limit=7"} {
		calls := reader.calls
		require.Equal(t, http.StatusBadRequest, syncHistoryRequest(reader, "alice", query).Code)
		require.Equal(t, calls, reader.calls)
	}
	calls := reader.calls
	require.Equal(t, http.StatusForbidden, syncHistoryRequest(reader, "", "offset=0").Code)
	require.Equal(t, calls, reader.calls)
}

func TestSyncHistoryHTTPRetainsLegacyShapeAndRedactsErrors(t *testing.T) {
	base := NewService(newFakeSourceRepo(), nil)
	legacy := syncHistoryRequest(base, "alice", "")
	require.Equal(t, http.StatusOK, legacy.Code)
	var jobs []models.SourceSyncJob
	require.NoError(t, json.Unmarshal(legacy.Body.Bytes(), &jobs))
	require.Equal(t, http.StatusServiceUnavailable, syncHistoryRequest(&struct{ Service }{base}, "alice", "offset=0").Code)
	reader := &syncHistoryHandlerService{Service: base, err: errors.New("private database diagnostic")}
	response := syncHistoryRequest(reader, "alice", "offset=0")
	require.Equal(t, http.StatusInternalServerError, response.Code)
	require.NotContains(t, response.Body.String(), "private database")
}

type syncHistoryScopeRepository struct {
	*extractionHistoryScopeRepository
	owner string
	ids   []uuid.UUID
}

func (r *syncHistoryScopeRepository) FindSyncJobsPageForOwnerSources(owner string, ids []uuid.UUID, _, _ int) ([]models.SourceSyncJob, int, error) {
	r.owner, r.ids = owner, append([]uuid.UUID(nil), ids...)
	return []models.SourceSyncJob{}, 0, nil
}

func TestSyncHistoryServiceRequiresOwnerScopedRepositoryAndExcludesLegacy(t *testing.T) {
	own, foreign, legacy := uuid.New(), uuid.New(), uuid.New()
	repo := &syncHistoryScopeRepository{extractionHistoryScopeRepository: &extractionHistoryScopeRepository{fakeSourceRepo: newFakeSourceRepo(
		&models.ConnectedSource{ID: own, OwnerIdentity: "alice"},
		&models.ConnectedSource{ID: foreign, OwnerIdentity: "bob"},
		&models.ConnectedSource{ID: legacy},
	)}}
	s := NewService(repo, nil).(*service)
	_, err := s.SyncJobsForOwnerPage(" alice ", nil, HistoryPageRequest{Limit: 6, Offset: 6})
	require.NoError(t, err)
	require.Equal(t, "alice", repo.owner)
	require.Equal(t, []uuid.UUID{own}, repo.ids)
	page, err := s.SyncJobsForOwnerPage("alice", &foreign, HistoryPageRequest{Limit: 6, Offset: 6})
	require.NoError(t, err)
	require.Empty(t, page.Items)
	require.Equal(t, 6, page.Offset)
	_, err = s.SyncJobsForOwnerPage("", nil, HistoryPageRequest{})
	require.Error(t, err)
	missing := NewService(repo.extractionHistoryScopeRepository, nil).(*service)
	_, err = missing.SyncJobsForOwnerPage("alice", nil, HistoryPageRequest{Limit: 6})
	require.ErrorContains(t, err, "unavailable")
}
