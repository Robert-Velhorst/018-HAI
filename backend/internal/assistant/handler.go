package assistant

import (
	"automation-hub-backend/internal/identity"
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

// RequireAuthenticatedOwner keeps chat commands on the same identity boundary
// as the task, pursuit, and workflow engines they orchestrate. The service can
// still be called by controlled in-process workers, but HTTP must not create
// ownerless plans, logs, or execution attempts.
func RequireAuthenticatedOwner() gin.HandlerFunc {
	return func(c *gin.Context) {
		if verifiedActor(c, "") == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "an authenticated owner session is required for assistant access"})
			return
		}
		c.Next()
	}
}

func (h *Handler) Command(c *gin.Context) {
	if h.service == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "assistant service is not configured"})
		return
	}
	var request CommandRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ownerIdentity := verifiedActor(c, "")
	if ownerIdentity == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "an authenticated owner session is required for assistant access"})
		return
	}
	request.OwnerIdentity = ownerIdentity
	request.Actor = verifiedActor(c, "operator")
	if strings.TrimSpace(request.Message) == "" && !request.RunCycle {
		c.JSON(http.StatusBadRequest, gin.H{"error": "message is required"})
		return
	}
	result, err := h.service.CommandContext(c.Request.Context(), request)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return
		}
		if errors.Is(err, ErrInvalidStandingMandateID) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error(), "result": result})
		return
	}
	c.JSON(http.StatusOK, result)
}

func verifiedActor(c *gin.Context, fallback string) string {
	if value, ok := c.Get(identity.ContextSubjectKey); ok {
		if subject, ok := value.(string); ok && strings.TrimSpace(subject) != "" {
			return subject
		}
	}
	return fallback
}

func (h *Handler) Logs(c *gin.Context) {
	if h.service == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "assistant service is not configured"})
		return
	}
	ownerIdentity := verifiedActor(c, "")
	if ownerIdentity == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "an authenticated owner session is required for assistant access"})
		return
	}
	c.JSON(http.StatusOK, h.service.LogsForOwner(ownerIdentity))
}
