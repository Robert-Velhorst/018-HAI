package source

import (
	"errors"
	"strings"

	"automation-hub-backend/internal/models"
	"github.com/google/uuid"
)

type HistoryPageRequest struct {
	Limit  int
	Offset int
}

type SyncJobPage struct {
	Items   []models.SourceSyncJob `json:"items"`
	Total   int                    `json:"total"`
	Limit   int                    `json:"limit"`
	Offset  int                    `json:"offset"`
	HasMore bool                   `json:"hasMore"`
}

type AuditLogPage struct {
	Items   []models.SourceAuditLog `json:"items"`
	Total   int                     `json:"total"`
	Limit   int                     `json:"limit"`
	Offset  int                     `json:"offset"`
	HasMore bool                    `json:"hasMore"`
}

type PagedHistoryService interface {
	SyncJobsForOwnerPage(ownerIdentity string, sourceID *uuid.UUID, page HistoryPageRequest) (*SyncJobPage, error)
	ExtractionsForOwnerPage(ownerIdentity, projectKey string, includeArchived bool, page HistoryPageRequest) (*ExtractionPage, error)
	AuditLogsForOwnerPage(ownerIdentity string, sourceID *uuid.UUID, page HistoryPageRequest) (*AuditLogPage, error)
}

func (s *service) OverviewForOwner(ownerIdentity, projectKey string, includeArchived bool) (*SourceOverview, error) {
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	if ownerIdentity == "" {
		return nil, errors.New("source owner identity is required")
	}
	visible, err := s.visibleSourceIDs(ownerIdentity)
	if err != nil {
		return nil, err
	}
	repository, ok := s.repo.(sourceOverviewRepository)
	if !ok {
		return nil, errors.New("source overview repository is unavailable")
	}
	return repository.FindOverviewForSources(sourceIDsFromSet(visible), projectKey, includeArchived)
}

func (s *service) ExtractionsForOwnerPage(ownerIdentity, projectKey string, includeArchived bool, page HistoryPageRequest) (*ExtractionPage, error) {
	visible, err := s.ownerSourceIDs(ownerIdentity)
	if err != nil {
		return nil, err
	}
	limit, offset := normalizedHistoryPage(page)
	repository, ok := s.repo.(pagedSourceHistoryRepository)
	if !ok {
		return nil, errors.New("paged source history repository is unavailable")
	}
	items, total, err := repository.FindExtractionsPageForSources(sourceIDsFromSet(visible), strings.TrimSpace(projectKey), includeArchived, limit, offset)
	if err != nil {
		return nil, err
	}
	return &ExtractionPage{Items: items, TotalCount: int64(total), Total: total, Limit: limit, Offset: offset, HasMore: offset+len(items) < total}, nil
}

func (s *service) SyncJobsForOwnerPage(ownerIdentity string, sourceID *uuid.UUID, page HistoryPageRequest) (*SyncJobPage, error) {
	visible, err := s.ownerSourceIDs(ownerIdentity)
	if err != nil {
		return nil, err
	}
	if sourceID != nil {
		if !visible[*sourceID] {
			return &SyncJobPage{Items: []models.SourceSyncJob{}}, nil
		}
		visible = map[uuid.UUID]bool{*sourceID: true}
	}
	limit, offset := normalizedHistoryPage(page)
	repository, ok := s.repo.(pagedSourceHistoryRepository)
	if !ok {
		return nil, errors.New("paged source history repository is unavailable")
	}
	items, total, err := repository.FindSyncJobsPageForSources(sourceIDsFromSet(visible), limit, offset)
	if err != nil {
		return nil, err
	}
	return &SyncJobPage{Items: items, Total: total, Limit: limit, Offset: offset, HasMore: offset+len(items) < total}, nil
}

func (s *service) AuditLogsForOwnerPage(ownerIdentity string, sourceID *uuid.UUID, page HistoryPageRequest) (*AuditLogPage, error) {
	visible, err := s.ownerSourceIDs(ownerIdentity)
	if err != nil {
		return nil, err
	}
	if sourceID != nil {
		if !visible[*sourceID] {
			return &AuditLogPage{Items: []models.SourceAuditLog{}}, nil
		}
		visible = map[uuid.UUID]bool{*sourceID: true}
	}
	limit, offset := normalizedHistoryPage(page)
	repository, ok := s.repo.(pagedSourceHistoryRepository)
	if !ok {
		return nil, errors.New("paged source history repository is unavailable")
	}
	items, total, err := repository.FindAuditLogsPageForSources(sourceIDsFromSet(visible), limit, offset)
	if err != nil {
		return nil, err
	}
	return &AuditLogPage{Items: items, Total: total, Limit: limit, Offset: offset, HasMore: offset+len(items) < total}, nil
}

func (s *service) ownerSourceIDs(ownerIdentity string) (map[uuid.UUID]bool, error) {
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	if ownerIdentity == "" {
		return nil, errors.New("source owner identity is required")
	}
	return s.visibleSourceIDs(ownerIdentity)
}

func normalizedHistoryPage(page HistoryPageRequest) (int, int) {
	limit := page.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 250 {
		limit = 250
	}
	if page.Offset < 0 {
		page.Offset = 0
	}
	return limit, page.Offset
}

var _ PagedHistoryService = (*service)(nil)
