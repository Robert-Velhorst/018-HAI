package source

import (
	"net/http"
	"strconv"

	"automation-hub-backend/internal/apierror"
	"automation-hub-backend/internal/models"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func (h *Handler) auditLogHistoryPage(c *gin.Context, owner string, sourceID *uuid.UUID) {
	query := c.Request.URL.Query()
	offsets, limits := query["offset"], query["limit"]
	if len(offsets) != 1 || len(limits) > 1 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid audit history page"})
		return
	}
	offset, err := strconv.Atoi(offsets[0])
	if err != nil || offset < 0 || offset > 1000000 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid audit history offset"})
		return
	}
	limit := 8
	if len(limits) == 1 {
		limit, err = strconv.Atoi(limits[0])
		if err != nil || limit < 1 || limit > 250 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid audit history limit"})
			return
		}
	}
	reader, ok := h.service.(interface {
		AuditLogsForOwnerPage(string, *uuid.UUID, HistoryPageRequest) (*AuditLogPage, error)
	})
	if !ok {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "paged source audit history is unavailable"})
		return
	}
	page, err := reader.AuditLogsForOwnerPage(owner, sourceID, HistoryPageRequest{Limit: limit, Offset: offset})
	if err != nil || page == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": apierror.PublicMessage(err, "source audit history is unavailable")})
		return
	}
	if page.Items == nil {
		page.Items = []models.SourceAuditLog{}
	}
	c.Header("X-Total-Count", strconv.Itoa(page.Total))
	c.Header("X-Result-Limit", strconv.Itoa(page.Limit))
	c.Header("X-Result-Offset", strconv.Itoa(page.Offset))
	c.JSON(http.StatusOK, page)
}
