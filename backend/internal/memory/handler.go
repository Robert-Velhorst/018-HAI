package memory

import (
	"automation-hub-backend/internal/identity"
	"automation-hub-backend/internal/models"
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type Handler struct {
	service Service
}

func NewHandler(service Service) *Handler {
	return &Handler{service: service}
}

func DefaultHandler() *Handler {
	return NewHandler(DefaultService())
}

func (h *Handler) Create(c *gin.Context) {
	var request CreateRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	memory, err := h.ownerService(c).CreateForOwner(memoryOwner(c), request)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, memory)
}

func (h *Handler) List(c *gin.Context) {
	includeArchived, _ := strconv.ParseBool(c.Query("includeArchived"))
	memories, err := h.ownerService(c).FindAllForOwner(memoryOwner(c), c.Query("projectKey"), includeArchived)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, memories)
}

func (h *Handler) Health(c *gin.Context) {
	report, err := HealthForOwner(h.service, memoryOwner(c), c.Query("projectKey"))
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "memory health review is unavailable"})
		return
	}
	c.JSON(http.StatusOK, report)
}

// Query lists memories with search, filtering, sorting, and pagination.
// It preserves the existing List endpoint unchanged and adds a richer,
// paginated envelope for clients that need to browse large memory sets.
func (h *Handler) Query(c *gin.Context) {
	includeArchived, _ := strconv.ParseBool(c.Query("includeArchived"))
	page, _ := strconv.Atoi(c.Query("page"))
	pageSize, _ := strconv.Atoi(c.Query("pageSize"))
	params := QueryParams{
		Search:   c.Query("q"),
		Kind:     c.Query("kind"),
		Tag:      c.Query("tag"),
		Sort:     c.Query("sort"),
		Order:    c.Query("order"),
		Page:     page,
		PageSize: pageSize,
	}
	var result PageResult
	var err error
	if queryService, ok := h.service.(OwnerQueryService); ok {
		result, err = queryService.QueryForOwner(c.Request.Context(), memoryOwner(c), c.Query("projectKey"), includeArchived, params)
	} else {
		var memories []models.ContextMemory
		memories, err = h.ownerService(c).FindAllForOwner(memoryOwner(c), c.Query("projectKey"), includeArchived)
		if err == nil {
			result = Query(memories, params)
		}
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "memory query is unavailable"})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *Handler) Get(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	memory, err := h.ownerService(c).FindByIDForOwner(memoryOwner(c), id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, memory)
}

func (h *Handler) Update(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	var request UpdateRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	memory, err := h.ownerService(c).UpdateForOwner(memoryOwner(c), id, request)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, memory)
}

func (h *Handler) Archive(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	memory, err := h.ownerService(c).ArchiveForOwner(memoryOwner(c), id, true)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, memory)
}

func (h *Handler) Restore(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	memory, err := h.ownerService(c).ArchiveForOwner(memoryOwner(c), id, false)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, memory)
}

func (h *Handler) Delete(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	if err := h.ownerService(c).DeleteForOwner(memoryOwner(c), id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) Retrieve(c *gin.Context) {
	var request RetrieveRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	result, err := RetrieveForOwnerContext(h.service, c.Request.Context(), memoryOwner(c), request)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *Handler) ReindexSemantic(c *gin.Context) {
	reindex, ok := h.service.(SemanticReindexService)
	if !ok {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "local semantic memory indexing is unavailable"})
		return
	}
	limit, _ := strconv.Atoi(c.Query("limit"))
	var result *SemanticReindexResult
	var err error
	if contextual, ok := h.service.(SemanticReindexContextService); ok {
		result, err = contextual.ReindexSemanticForOwnerContext(c.Request.Context(), memoryOwner(c), limit)
	} else {
		result, err = reindex.ReindexSemanticForOwner(memoryOwner(c), limit)
	}
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "local semantic memory indexing failed"})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *Handler) Export(c *gin.Context) {
	memories, err := h.ownerService(c).FindAllForOwner(memoryOwner(c), c.Query("projectKey"), true)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"format":   "018-hai-context-memory-v1",
		"memories": memories,
	})
}

func (h *Handler) ownerService(c *gin.Context) OwnerScopedService {
	scoped, ok := h.service.(OwnerScopedService)
	if !ok {
		panic("memory handler requires owner-scoped service")
	}
	return scoped
}

func memoryOwner(c *gin.Context) string {
	if value, ok := c.Get(identity.ContextSubjectKey); ok {
		if subject, ok := value.(string); ok {
			return strings.TrimSpace(subject)
		}
	}
	return ""
}

func parseID(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID format"})
		return uuid.UUID{}, false
	}
	return id, true
}
