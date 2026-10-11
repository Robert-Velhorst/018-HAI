package llm

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"automation-hub-backend/internal/safety"

	"github.com/gin-gonic/gin"
)

type Handler struct {
	service               *Service
	effectContextResolver func(*gin.Context) (EffectContext, error)
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
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
	probes, err := h.service.ProbeAndRecordProviders()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, probes)
}

func (h *Handler) ProviderProbeHistory(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "30"))
	probes, err := h.service.ProviderProbeHistory(limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, probes)
}

func (h *Handler) ModelMaintenanceHistory(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "30"))
	records, err := h.service.ModelMaintenanceHistory(limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
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
	run, err := h.service.RunDueModelMaintenanceContext(c.Request.Context())
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "model maintenance failed"})
		return
	}
	c.JSON(http.StatusOK, run)
}

func (h *Handler) GenerationHistory(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "30"))
	records, err := h.service.GenerationHistory(limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, records)
}

func (h *Handler) Route(c *gin.Context) {
	var request RouteRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	decision, err := h.service.Route(request)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, decision)
}

func (h *Handler) Generate(c *gin.Context) {
	var request GenerateRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	// Public API callers may request generation, but paid-model approval must
	// come from a server-side approval workflow, not a client-supplied flag.
	request.AllowPaidApproved = false
	if h.effectContextResolver != nil {
		effectContext, err := h.effectContextResolver(c)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
			return
		}
		request.EffectContext = &effectContext
	}
	result, err := h.service.GenerateContext(c.Request.Context(), request)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *Handler) Logs(c *gin.Context) {
	c.JSON(http.StatusOK, h.service.Logs())
}
