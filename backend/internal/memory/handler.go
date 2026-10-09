package memory

import (
	"automation-hub-backend/internal/apierror"
	"automation-hub-backend/internal/identity"
	"automation-hub-backend/internal/models"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	if !bindBoundedMemoryJSON(c, &request, "invalid memory request") {
		return
	}
	owner, ok := requireHandlerOwner(c)
	if !ok {
		return
	}
	if isReviewManagedMemoryKind(request.Kind) {
		c.JSON(http.StatusForbidden, gin.H{"error": "this memory type can only be written by its review workflow"})
		return
	}
	memory, err := h.ownerService(c).CreateForOwner(owner, request)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": apierror.PublicMessage(err, "memory could not be created")})
		return
	}
	c.JSON(http.StatusCreated, memoryResponse(memory))
}

func (h *Handler) List(c *gin.Context) {
	owner, ok := requireHandlerOwner(c)
	if !ok {
		return
	}
	includeArchived, _ := strconv.ParseBool(c.Query("includeArchived"))
	limit, _ := strconv.Atoi(c.Query("limit"))
	var memories []models.ContextMemory
	var err error
	if limit > 0 {
		memories, err = RecentForOwner(h.service, owner, c.Query("projectKey"), includeArchived, limit)
	} else {
		memories, err = h.ownerService(c).FindAllForOwner(owner, c.Query("projectKey"), includeArchived)
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "memories are unavailable"})
		return
	}
	c.JSON(http.StatusOK, memoryListResponse(memories))
}

func RecentForOwner(service Service, ownerIdentity, projectKey string, includeArchived bool, limit int) ([]models.ContextMemory, error) {
	ownerIdentity, err := requireOwnerIdentity(ownerIdentity)
	if err != nil {
		return nil, err
	}
	recent, ok := service.(RecentMemoryService)
	if !ok {
		return nil, fmt.Errorf("bounded recent memory listing is unavailable")
	}
	return recent.RecentForOwner(ownerIdentity, projectKey, includeArchived, limit)
}

func (h *Handler) Health(c *gin.Context) {
	owner, ok := requireHandlerOwner(c)
	if !ok {
		return
	}
	report, err := HealthForOwner(h.service, owner, c.Query("projectKey"))
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
	owner, ok := requireHandlerOwner(c)
	if !ok {
		return
	}
	includeArchived, _ := strconv.ParseBool(c.Query("includeArchived"))
	memories, err := h.ownerService(c).FindAllForOwner(owner, c.Query("projectKey"), includeArchived)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "memories are unavailable"})
		return
	}
	page, _ := strconv.Atoi(c.Query("page"))
	pageSize, _ := strconv.Atoi(c.Query("pageSize"))
	result := Query(memories, QueryParams{
		Search:   c.Query("q"),
		Kind:     c.Query("kind"),
		Tag:      c.Query("tag"),
		Sort:     c.Query("sort"),
		Order:    c.Query("order"),
		Page:     page,
		PageSize: pageSize,
	})
	result.Items = memoryListResponse(result.Items)
	c.JSON(http.StatusOK, result)
}

func (h *Handler) Get(c *gin.Context) {
	owner, ok := requireHandlerOwner(c)
	if !ok {
		return
	}
	id, ok := parseID(c)
	if !ok {
		return
	}
	memory, err := h.ownerService(c).FindByIDForOwner(owner, id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": apierror.PublicMessage(err, "memory not found")})
		return
	}
	c.JSON(http.StatusOK, memoryResponse(memory))
}

func (h *Handler) Update(c *gin.Context) {
	owner, ok := requireHandlerOwner(c)
	if !ok {
		return
	}
	id, ok := parseID(c)
	if !ok {
		return
	}
	var request UpdateRequest
	if !bindBoundedMemoryJSON(c, &request, "invalid memory update request") {
		return
	}
	memory, err := h.ownerService(c).UpdateForOwner(owner, id, request)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": apierror.PublicMessage(err, "memory could not be updated")})
		return
	}
	c.JSON(http.StatusOK, memoryResponse(memory))
}

func (h *Handler) Archive(c *gin.Context) {
	owner, ok := requireHandlerOwner(c)
	if !ok {
		return
	}
	id, ok := parseID(c)
	if !ok {
		return
	}
	memory, err := h.ownerService(c).ArchiveForOwner(owner, id, true)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": apierror.PublicMessage(err, "memory could not be archived")})
		return
	}
	c.JSON(http.StatusOK, memoryResponse(memory))
}

func (h *Handler) Restore(c *gin.Context) {
	owner, ok := requireHandlerOwner(c)
	if !ok {
		return
	}
	id, ok := parseID(c)
	if !ok {
		return
	}
	memory, err := h.ownerService(c).ArchiveForOwner(owner, id, false)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": apierror.PublicMessage(err, "memory could not be restored")})
		return
	}
	c.JSON(http.StatusOK, memoryResponse(memory))
}

func (h *Handler) Delete(c *gin.Context) {
	owner, ok := requireHandlerOwner(c)
	if !ok {
		return
	}
	id, ok := parseID(c)
	if !ok {
		return
	}
	if err := h.ownerService(c).DeleteForOwner(owner, id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "memory could not be deleted"})
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) Retrieve(c *gin.Context) {
	owner, ok := requireHandlerOwner(c)
	if !ok {
		return
	}
	var request RetrieveRequest
	if !bindBoundedMemoryJSON(c, &request, "invalid memory retrieval request") {
		return
	}
	result, err := h.ownerService(c).RetrieveForOwner(owner, request)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": apierror.PublicMessage(err, "memory retrieval is unavailable")})
		return
	}
	c.JSON(http.StatusOK, retrieveResultResponse(result))
}

func (h *Handler) ReindexSemantic(c *gin.Context) {
	owner, ok := requireHandlerOwner(c)
	if !ok {
		return
	}
	reindex, ok := h.service.(SemanticReindexService)
	if !ok {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "local semantic memory indexing is unavailable"})
		return
	}
	limit, _ := strconv.Atoi(c.Query("limit"))
	result, err := reindex.ReindexSemanticForOwner(owner, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "local semantic memory indexing failed"})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *Handler) Export(c *gin.Context) {
	owner, ok := requireHandlerOwner(c)
	if !ok {
		return
	}
	memories, err := h.ownerService(c).FindAllForOwner(owner, c.Query("projectKey"), true)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "memory export is unavailable"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"format":   "018-hai-context-memory-v1",
		"memories": memoryListResponse(memories),
	})
}

// SourceExtractionID is internal lineage metadata; clients receive the source
// URI and label instead. Clone response values so redaction never changes the
// persisted or in-process memory record.
func memoryResponse(memory *models.ContextMemory) *models.ContextMemory {
	if memory == nil {
		return nil
	}
	response := *memory
	response.SourceExtractionID = nil
	return &response
}

func memoryListResponse(memories []models.ContextMemory) []models.ContextMemory {
	response := make([]models.ContextMemory, len(memories))
	for index := range memories {
		response[index] = *memoryResponse(&memories[index])
	}
	return response
}

func retrieveResultResponse(result *RetrieveResult) *RetrieveResult {
	if result == nil {
		return nil
	}
	response := *result
	response.UsedContext = append([]RankedMemory(nil), result.UsedContext...)
	for index := range response.UsedContext {
		response.UsedContext[index].Memory.SourceExtractionID = nil
	}
	return &response
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

func requireHandlerOwner(c *gin.Context) (string, bool) {
	ownerIdentity, err := requireOwnerIdentity(memoryOwner(c))
	if err != nil {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "authenticated owner identity is required"})
		return "", false
	}
	return ownerIdentity, true
}

func parseID(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID format"})
		return uuid.UUID{}, false
	}
	return id, true
}

func bindBoundedMemoryJSON(c *gin.Context, destination any, invalidMessage string) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxMemoryRequestBytes)
	payload, err := io.ReadAll(c.Request.Body)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "memory request exceeds size limit"})
			return false
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": invalidMessage})
		return false
	}
	if err := json.Unmarshal(payload, destination); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": invalidMessage})
		return false
	}
	return true
}
