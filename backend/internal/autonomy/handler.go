package autonomy

import (
	"automation-hub-backend/internal/apierror"
	"automation-hub-backend/internal/identity"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

type Handler struct {
	service Service
}

func NewHandler(service Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) Overview(c *gin.Context) {
	owner := strings.TrimSpace(c.GetString(identity.ContextSubjectKey))
	if owner == "" {
		err := apierror.New(apierror.CodeUnauthorized, "an authenticated owner is required for autonomy telemetry")
		c.AbortWithStatusJSON(err.HTTPStatus(), err.Envelope())
		return
	}
	result, err := h.service.OverviewForOwner(owner)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": apierror.PublicMessage(err, "autonomy overview is unavailable")})
		return
	}
	c.JSON(http.StatusOK, publicOverview(result))
}

func (h *Handler) Stress(c *gin.Context) {
	run, results, err := h.service.RunStressSuite()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": apierror.PublicMessage(err, "autonomy stress suite could not be completed")})
		return
	}
	c.JSON(http.StatusOK, gin.H{"run": run, "results": results})
}
