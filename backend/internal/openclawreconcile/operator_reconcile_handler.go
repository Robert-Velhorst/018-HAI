package openclawreconcile

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type OperatorReconcileHandler struct {
	service *Service
}

func NewOperatorReconcileHandler(service *Service) *OperatorReconcileHandler {
	return &OperatorReconcileHandler{service: service}
}

func (h *OperatorReconcileHandler) Reconcile(c *gin.Context) {
	owner, eventID, ok := artifactRequest(c)
	if !ok {
		return
	}
	if h == nil || h.service == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "OpenClaw reconciliation is temporarily unavailable"})
		return
	}
	result, err := h.service.ReconcileOwnerEvent(c.Request.Context(), owner, eventID)
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "No owner-bound OpenClaw execution was found"})
		return
	case err != nil:
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "OpenClaw reconciliation is temporarily unavailable"})
		return
	case result == nil:
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "OpenClaw reconciliation is temporarily unavailable"})
		return
	}
	status := http.StatusAccepted
	if result.Status == "terminal" || result.Status == "not_admitted" {
		status = http.StatusOK
	}
	c.JSON(status, result)
}
