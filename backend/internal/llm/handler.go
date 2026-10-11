package llm

import (
	"automation-hub-backend/internal/apierror"
	"automation-hub-backend/internal/safety"
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

type Handler struct {
	service               *Service
	effectContextResolver func(*gin.Context) (EffectContext, error)
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func respondToRequestContextError(c *gin.Context, err error, timeoutMessage string) bool {
	switch {
	case errors.Is(err, context.Canceled):
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error":     "request was cancelled before completion",
			"retryable": true,
		})
		return true
	case errors.Is(err, context.DeadlineExceeded):
		c.JSON(http.StatusGatewayTimeout, gin.H{
			"error":     timeoutMessage,
			"retryable": true,
		})
		return true
	default:
		return false
	}
}

// NewHandlerWithEffectContext keeps identity and approval provenance out of
// public JSON. The composition root derives it from authenticated request
// state; without a resolver the service remains fail closed at the provider
// boundary.
func NewHandlerWithEffectContext(
	service *Service,
	resolver func(*gin.Context) (EffectContext, error),
) *Handler {
	return &Handler{
		service:               service,
		effectContextResolver: resolver,
	}
}

func DefaultHandler() (*Handler, error) {
	service, err := NewServiceFromEnv()
	if err != nil {
		return nil, err
	}
	return NewHandler(service), nil
}

func (h *Handler) Policy(c *gin.Context) {
	c.JSON(http.StatusOK, h.service.Policy())
}

func (h *Handler) ProviderProbes(c *gin.Context) {
	probes, err := h.service.ProbeAndRecordProvidersWithContext(c.Request.Context())
	if err != nil {
		if respondToRequestContextError(c, err, "provider probes did not finish before the request deadline") {
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": apierror.PublicMessage(err, "provider probes are unavailable")})
		return
	}
	c.JSON(http.StatusOK, probes)
}

func (h *Handler) ProviderProbeHistory(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "30"))
	probes, err := h.service.ProviderProbeHistoryWithContext(c.Request.Context(), limit)
	if err != nil {
		if respondToRequestContextError(c, err, "provider probe history did not load before the request deadline") {
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": apierror.PublicMessage(err, "provider probe history is unavailable")})
		return
	}
	c.JSON(http.StatusOK, probes)
}

func (h *Handler) ModelMaintenanceHistory(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "30"))
	records, err := h.service.ModelMaintenanceHistoryWithContext(c.Request.Context(), limit)
	if err != nil {
		if respondToRequestContextError(c, err, "model maintenance history did not load before the request deadline") {
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": apierror.PublicMessage(err, "model maintenance history is unavailable")})
		return
	}
	c.JSON(http.StatusOK, records)
}

func (h *Handler) RunDueModelMaintenance(c *gin.Context) {
	if safety.EmergencyStopActive() {
		c.JSON(http.StatusConflict, gin.H{
			"error":  safety.EmergencyStopReason(),
			"status": "blocked",
		})
		return
	}
	c.JSON(http.StatusOK, h.service.RunDueModelMaintenanceWithContext(c.Request.Context()))
}

func (h *Handler) GenerationHistory(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "30"))
	records, err := h.service.GenerationHistory(limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": apierror.PublicMessage(err, "generation history is unavailable")})
		return
	}
	c.JSON(http.StatusOK, records)
}

func (h *Handler) Route(c *gin.Context) {
	var request RouteRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid model-routing request"})
		return
	}
	decision, err := h.service.RouteWithContext(c.Request.Context(), request)
	if err != nil {
		if respondToRequestContextError(c, err, "model routing did not finish before the request deadline") {
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": apierror.PublicMessage(err, "model routing could not be completed")})
		return
	}
	c.JSON(http.StatusOK, decision)
}

func (h *Handler) Generate(c *gin.Context) {
	var request GenerateRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid model-generation request"})
		return
	}
	// Public API callers may request generation, but paid-model approval must
	// come from a server-side approval workflow, not a client-supplied flag.
	request.AllowPaidApproved = false
	request.CancellationContext = c.Request.Context()
	if h.effectContextResolver != nil {
		effectContext, err := h.effectContextResolver(c)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "an authorized effect context is required"})
			return
		}
		request.EffectContext = &effectContext
	}
	result, err := h.service.Generate(request)
	if err != nil {
		if respondToRequestContextError(c, err, "model generation did not finish before the request deadline") {
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": apierror.PublicMessage(err, "model generation could not be completed")})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *Handler) Logs(c *gin.Context) {
	c.JSON(http.StatusOK, h.service.Logs())
}
