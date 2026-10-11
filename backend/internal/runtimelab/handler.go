package runtimelab

import (
	"net/http"
	"strings"

	"automation-hub-backend/internal/apierror"
	"automation-hub-backend/internal/identity"

	"github.com/gin-gonic/gin"
)

// Handler serves the Runtime Lab API (§10.19).
type Handler struct {
	svc *Service
}

// NewHandler builds a handler over a service.
func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) Overview(c *gin.Context) {
	if !h.requireConfiguredOwner(c) {
		return
	}
	c.JSON(http.StatusOK, gin.H{"runtimes": h.svc.Overview(c.Request.Context())})
}

func (h *Handler) FeatureParity(c *gin.Context) {
	if !h.requireConfiguredOwner(c) {
		return
	}
	overview, err := h.svc.FeatureParity()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": apierror.PublicMessage(err, "runtime feature parity is unavailable")})
		return
	}
	c.JSON(http.StatusOK, overview)
}

func (h *Handler) RuntimeFeatureParity(c *gin.Context) {
	if !h.requireConfiguredOwner(c) {
		return
	}
	inventory, ok, err := h.svc.RuntimeFeatureParity(c.Param("runtimeId"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": apierror.PublicMessage(err, "runtime parity inventory is unavailable")})
		return
	}
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "runtime parity inventory not found"})
		return
	}
	c.JSON(http.StatusOK, inventory)
}

func (h *Handler) Capabilities(c *gin.Context) {
	if !h.requireConfiguredOwner(c) {
		return
	}
	overview, err := h.svc.CapabilityCards(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": apierror.PublicMessage(err, "runtime capabilities are unavailable")})
		return
	}
	c.JSON(http.StatusOK, overview)
}

func (h *Handler) Probe(c *gin.Context) {
	if !h.requireConfiguredOwner(c) {
		return
	}
	res, ok := h.svc.Probe(c.Request.Context(), c.Param("runtimeId"))
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "runtime not found"})
		return
	}
	c.JSON(http.StatusOK, res)
}

func (h *Handler) SelfTest(c *gin.Context) {
	if !h.requireConfiguredOwner(c) {
		return
	}
	attempt, ok := h.svc.SelfTest(c.Request.Context(), c.Param("runtimeId"))
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "runtime not found"})
		return
	}
	c.JSON(http.StatusOK, attempt)
}

func (h *Handler) Attempts(c *gin.Context) {
	if !h.requireConfiguredOwner(c) {
		return
	}
	c.JSON(http.StatusOK, gin.H{"attempts": h.svc.Attempts(c.Param("runtimeId"))})
}

// HTTP callers cannot use the configured operator's service as their deputy.
// Authentication and route permissions remain the router's responsibility.
func (h *Handler) requireConfiguredOwner(c *gin.Context) bool {
	value, exists := c.Get(identity.ContextSubjectKey)
	owner, valid := value.(string)
	if !exists || !valid || owner == "" || owner != strings.TrimSpace(owner) {
		err := apierror.New(apierror.CodeUnauthorized, "an authenticated owner session is required for this operation")
		c.AbortWithStatusJSON(err.HTTPStatus(), err.Envelope())
		return false
	}
	if h == nil || h.svc == nil || h.svc.owner == "" || h.svc.owner != strings.TrimSpace(h.svc.owner) {
		err := apierror.New(apierror.CodeUnavailable, "runtime lab owner scope is unavailable")
		c.AbortWithStatusJSON(err.HTTPStatus(), err.Envelope())
		return false
	}
	if owner != h.svc.owner {
		err := apierror.New(apierror.CodeForbidden, "runtime lab is restricted to its configured owner")
		c.AbortWithStatusJSON(err.HTTPStatus(), err.Envelope())
		return false
	}
	return true
}
