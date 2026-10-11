package planningoptimizer

import (
	"automation-hub-backend/internal/apierror"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"automation-hub-backend/internal/identity"

	"github.com/gin-gonic/gin"
)

type Handler struct{ service *service }

func NewHandler(service *service) *Handler { return &Handler{service: service} }

func (h *Handler) Status(c *gin.Context) { c.JSON(http.StatusOK, h.service.Status()) }

func (h *Handler) Probe(c *gin.Context) {
	result, err := h.service.Probe(c.Request.Context())
	if errors.Is(err, ErrNotConfigured) {
		c.JSON(http.StatusConflict, result)
		return
	}
	if errors.Is(err, ErrUnavailable) {
		c.JSON(http.StatusServiceUnavailable, result)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *Handler) Propose(c *gin.Context) {
	var request Request
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64<<10)
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid scheduling proposal request"})
		return
	}
	run, err := h.service.Propose(c.Request.Context(), owner(c), request)
	if errors.Is(err, ErrNotConfigured) {
		c.JSON(http.StatusConflict, gin.H{"error": "planning optimizer is not configured", "run": run})
		return
	}
	if errors.Is(err, ErrUnavailable) {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "planning optimizer is unavailable"})
		return
	}
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": apierror.PublicMessage(err, "planning proposal request is invalid")})
		return
	}
	c.JSON(http.StatusCreated, run)
}

func (h *Handler) Runs(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "25"))
	runs, err := h.service.Runs(owner(c), limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not read planning proposal runs"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"runs": runs})
}

func owner(c *gin.Context) string {
	value, _ := c.Get(identity.ContextSubjectKey)
	owner, _ := value.(string)
	return strings.TrimSpace(owner)
}
