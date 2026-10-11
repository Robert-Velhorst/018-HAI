package operationalgraph

import (
	"automation-hub-backend/internal/identity"
	"fmt"
	"github.com/gin-gonic/gin"
	"net/http"
	"strconv"
	"strings"
)

const maxWriteBodyBytes = 32 * 1024

type Handler struct{ service *Service }

func NewHandler(service *Service) *Handler { return &Handler{service: service} }

func (h *Handler) Snapshot(c *gin.Context) {
	owner, ok := owner(c)
	if !ok {
		return
	}
	result, err := h.service.Snapshot(c.Request.Context(), owner)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}
func (h *Handler) Search(c *gin.Context) {
	owner, ok := owner(c)
	if !ok {
		return
	}
	result, err := h.service.Search(c.Request.Context(), owner, c.Query("q"), c.Query("layer"), c.Query("status"), queryInt(c, "limit", 40))
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}
func (h *Handler) Neighborhood(c *gin.Context) {
	owner, ok := owner(c)
	if !ok {
		return
	}
	result, err := h.service.Neighborhood(c.Request.Context(), owner, c.Param("id"), queryInt(c, "depth", 1), queryInt(c, "limit", 100))
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}
func (h *Handler) Path(c *gin.Context) {
	owner, ok := owner(c)
	if !ok {
		return
	}
	result, err := h.service.Path(c.Request.Context(), owner, c.Query("from"), c.Query("to"), queryInt(c, "maxHops", 12))
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}
func (h *Handler) AgentBoot(c *gin.Context) {
	owner, ok := owner(c)
	if !ok {
		return
	}
	result, err := h.service.AgentBoot(c.Request.Context(), owner, c.Param("id"))
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}
func (h *Handler) RecordMemory(c *gin.Context) {
	owner, ok := owner(c)
	if !ok {
		return
	}
	var request MemoryWriteRequest
	if !bindBounded(c, &request) {
		return
	}
	result, err := h.service.RecordMemory(owner, request)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, result)
}
func (h *Handler) RecordReport(c *gin.Context) {
	owner, ok := owner(c)
	if !ok {
		return
	}
	var request ReportWriteRequest
	if !bindBounded(c, &request) {
		return
	}
	result, err := h.service.RecordReport(c.Request.Context(), owner, request)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, result)
}
func (h *Handler) fail(c *gin.Context, err error) {
	status := http.StatusBadRequest
	if h.service.IsNotFound(err) {
		status = http.StatusNotFound
	}
	c.JSON(status, gin.H{"error": safeError(err)})
}

func owner(c *gin.Context) (string, bool) {
	value, exists := c.Get(identity.ContextSubjectKey)
	result, ok := value.(string)
	result = strings.TrimSpace(result)
	if !exists || !ok || result == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "an authenticated owner session is required for the operational graph"})
		return "", false
	}
	return result, true
}
func queryInt(c *gin.Context, key string, fallback int) int {
	value, err := strconv.Atoi(strings.TrimSpace(c.Query(key)))
	if err != nil {
		return fallback
	}
	return value
}
func bindBounded(c *gin.Context, target any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxWriteBodyBytes)
	if err := c.ShouldBindJSON(target); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "request body is invalid or exceeds the 32 KB limit"})
		return false
	}
	return true
}

type RouteGuards struct {
	AuthenticatedOwner gin.HandlerFunc
	RecognizedRole     gin.HandlerFunc
	Read               gin.HandlerFunc
	Write              gin.HandlerFunc
}

func RegisterRoutes(parent *gin.RouterGroup, handler *Handler, guards RouteGuards) error {
	if parent == nil || handler == nil || handler.service == nil {
		return fmt.Errorf("operational graph routes require a parent and handler")
	}
	if guards.AuthenticatedOwner == nil || guards.RecognizedRole == nil || guards.Read == nil || guards.Write == nil {
		return fmt.Errorf("operational graph routes require complete guards")
	}
	routes := parent.Group("/operational-graph")
	routes.Use(guards.AuthenticatedOwner, guards.RecognizedRole)
	routes.GET("/snapshot", guards.Read, handler.Snapshot)
	routes.GET("/search", guards.Read, handler.Search)
	routes.GET("/nodes/:id/neighborhood", guards.Read, handler.Neighborhood)
	routes.GET("/path", guards.Read, handler.Path)
	routes.GET("/agents/:id/boot", guards.Read, handler.AgentBoot)
	routes.POST("/memories", guards.Write, handler.RecordMemory)
	routes.POST("/reports", guards.Write, handler.RecordReport)
	return nil
}
