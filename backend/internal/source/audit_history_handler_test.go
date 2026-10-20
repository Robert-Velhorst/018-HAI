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

type auditHistoryService struct {
	Service
	owner string
	page  HistoryPageRequest
	calls int
	err   error
}

func (s *auditHistoryService) AuditLogsForOwnerPage(owner string, _ *uuid.UUID, page HistoryPageRequest) (*AuditLogPage, error) {
	s.calls++
	s.owner, s.page = owner, page
	if s.err != nil {
		return nil, s.err
	}
	return &AuditLogPage{Items: []models.SourceAuditLog{{ID: uuid.New()}}, Total: 20, Limit: page.Limit, Offset: page.Offset, HasMore: true}, nil
}

func auditHistoryRequest(service Service, owner, query string) *httptest.ResponseRecorder {
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set(identity.ContextSubjectKey, owner) })
	router.GET("/audit-logs", NewHandler(service).AuditLogs)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/audit-logs?"+query, nil))
	return response
}

func TestAuditHistoryHTTPUsesAuthenticatedOwnerAndExactOffset(t *testing.T) {
	reader := &auditHistoryService{Service: NewService(newFakeSourceRepo(), nil)}
	response := auditHistoryRequest(reader, "alice", "offset=8&limit=8&owner=bob")
	require.Equal(t, http.StatusOK, response.Code)
	var page AuditLogPage
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &page))
	require.Equal(t, "alice", reader.owner)
	require.Equal(t, HistoryPageRequest{Limit: 8, Offset: 8}, reader.page)
	require.Equal(t, 20, page.Total)
	require.Equal(t, 8, page.Offset)
	require.Equal(t, "8", response.Header().Get("X-Result-Offset"))
}

func TestAuditHistoryHTTPRejectsInvalidPagesAndMissingOwner(t *testing.T) {
	for _, query := range []string{"offset=", "offset=-1", "offset=x", "offset=1000001", "offset=0&offset=8", "offset=0&limit=0", "offset=0&limit=251", "offset=0&limit=8&limit=9"} {
		t.Run(query, func(t *testing.T) {
			reader := &auditHistoryService{Service: NewService(newFakeSourceRepo(), nil)}
			require.Equal(t, http.StatusBadRequest, auditHistoryRequest(reader, "alice", query).Code)
			require.Zero(t, reader.calls)
		})
	}
	reader := &auditHistoryService{Service: NewService(newFakeSourceRepo(), nil)}
	require.Equal(t, http.StatusForbidden, auditHistoryRequest(reader, "", "offset=0").Code)
	require.Zero(t, reader.calls)
}

func TestAuditHistoryHTTPKeepsLegacyArrayAndFailsClosed(t *testing.T) {
	base := NewService(newFakeSourceRepo(), nil)
	legacy := auditHistoryRequest(base, "alice", "")
	require.Equal(t, http.StatusOK, legacy.Code)
	var logs []models.SourceAuditLog
	require.NoError(t, json.Unmarshal(legacy.Body.Bytes(), &logs))
	missing := &struct{ Service }{Service: base}
	require.Equal(t, http.StatusServiceUnavailable, auditHistoryRequest(missing, "alice", "offset=0").Code)
	reader := &auditHistoryService{Service: base, err: errors.New("database failure with internal details")}
	response := auditHistoryRequest(reader, "alice", "offset=0")
	require.Equal(t, http.StatusInternalServerError, response.Code)
	require.NotContains(t, response.Body.String(), "internal details")
}

type auditHistoryScopeRepository struct {
	*extractionHistoryScopeRepository
	auditSourceIDs []uuid.UUID
}

func (r *auditHistoryScopeRepository) FindAuditLogsPageForSources(ids []uuid.UUID, _, _ int) ([]models.SourceAuditLog, int, error) {
	r.auditSourceIDs = append([]uuid.UUID(nil), ids...)
	return []models.SourceAuditLog{}, 0, nil
}

func TestAuditHistoryServiceExcludesForeignAndUnownedLegacySources(t *testing.T) {
	own, foreign, legacy := uuid.New(), uuid.New(), uuid.New()
	repo := &auditHistoryScopeRepository{extractionHistoryScopeRepository: &extractionHistoryScopeRepository{fakeSourceRepo: newFakeSourceRepo(
		&models.ConnectedSource{ID: own, OwnerIdentity: "alice"},
		&models.ConnectedSource{ID: foreign, OwnerIdentity: "bob"},
		&models.ConnectedSource{ID: legacy},
	)}}
	s := NewService(repo, nil).(*service)
	_, err := s.AuditLogsForOwnerPage("alice", nil, HistoryPageRequest{Limit: 8, Offset: 8})
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{own}, repo.auditSourceIDs)
	page, err := s.AuditLogsForOwnerPage("alice", &foreign, HistoryPageRequest{Limit: 8, Offset: 8})
	require.NoError(t, err)
	require.Empty(t, page.Items)
	require.Equal(t, 8, page.Offset)
	require.Equal(t, 8, page.Limit)
	_, err = s.AuditLogsForOwnerPage("", nil, HistoryPageRequest{})
	require.Error(t, err)
}
