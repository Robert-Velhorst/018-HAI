package source

import (
	"net/http"
	"strconv"

	"automation-hub-backend/internal/apierror"
	"automation-hub-backend/internal/models"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func (h *Handler) syncJobHistoryPage(c *gin.Context, owner string, sourceID *uuid.UUID) {
	request, ok := sourceHistoryPageRequest(c)
	if !ok {
		return
	}
	reader, ok := h.service.(interface {
		SyncJobsForOwnerPage(string, *uuid.UUID, HistoryPageRequest) (*SyncJobPage, error)
	})
	if !ok {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "paged source sync history is unavailable"})
		return
	}
	page, err := reader.SyncJobsForOwnerPage(owner, sourceID, request)
	if err != nil || page == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": apierror.PublicMessage(err, "source sync history is unavailable")})
		return
	}
	if page.Items == nil {
		page.Items = []models.SourceSyncJob{}
	}
	c.Header("X-Total-Count", strconv.Itoa(page.Total))
	c.Header("X-Result-Limit", strconv.Itoa(page.Limit))
	c.Header("X-Result-Offset", strconv.Itoa(page.Offset))
	c.JSON(http.StatusOK, page)
}
