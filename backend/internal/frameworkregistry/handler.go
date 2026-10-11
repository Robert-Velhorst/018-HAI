package frameworkregistry

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"automation-hub-backend/internal/identity"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type Handler struct {
	service *Service
}

const maxFrameworkRequestBytes = 64 * 1024

// selectionPreviewRequest contains only untrusted planning hints accepted from
// the browser. Trusted risk, approval, and identity state are derived by the
// authenticated task and approval services rather than asserted by a client.
type selectionPreviewRequest struct {
	TaskPlanID          string   `json:"taskPlanId,omitempty"`
	Request             string   `json:"request"`
	ProjectKey          string   `json:"projectKey,omitempty"`
	PursuitID           string   `json:"pursuitId,omitempty"`
	TaskType            string   `json:"taskType,omitempty"`
	Difficulty          int      `json:"difficulty,omitempty"`
	RequiredReasoning   string   `json:"requiredReasoning,omitempty"`
	SuccessCriteria     []string `json:"successCriteria,omitempty"`
	NeedsMemory         bool     `json:"needsMemory,omitempty"`
	NeedsTools          bool     `json:"needsTools,omitempty"`
	NeedsDocuments      bool     `json:"needsDocuments,omitempty"`
	NeedsWebAccess      bool     `json:"needsWebAccess,omitempty"`
	NeedsLocalExecution bool     `json:"needsLocalExecution,omitempty"`
	ExecuteRequested    bool     `json:"executeRequested,omitempty"`
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) Overview(c *gin.Context) {
	owner, ok := frameworkOwner(c)
	if !ok {
		return
	}
	result, err := h.service.Overview(owner)
	respondFramework(c, result, err, http.StatusOK)
}

func (h *Handler) List(c *gin.Context) {
	owner, ok := frameworkOwner(c)
	if !ok {
		return
	}
	result, err := h.service.List(owner)
	respondFramework(c, gin.H{"frameworks": result}, err, http.StatusOK)
}

// FamilyTaxonomy returns the immutable, versioned 55-family architecture
// contract used by the selector. Authentication and read permission are
// enforced by the production router before this handler runs.
func (h *Handler) FamilyTaxonomy(c *gin.Context) {
	if _, ok := frameworkOwner(c); !ok {
		return
	}
	if h == nil || h.service == nil {
		respondFramework(
			c,
			nil,
			errors.New("framework registry service is unavailable"),
			http.StatusOK,
		)
		return
	}
	taxonomy, err := h.service.FamilyTaxonomy()
	if err != nil {
		respondFramework(c, nil, err, http.StatusOK)
		return
	}

	etag := `"` + taxonomy.Digest + `"`
	c.Header("Cache-Control", "private, max-age=86400, immutable")
	c.Header("ETag", etag)
	if strings.TrimSpace(c.GetHeader("If-None-Match")) == etag {
		c.Status(http.StatusNotModified)
		return
	}
	c.JSON(http.StatusOK, taxonomy)
}

func (h *Handler) Get(c *gin.Context) {
	owner, ok := frameworkOwner(c)
	if !ok {
		return
	}
	result, err := h.service.Get(owner, c.Param("id"))
	respondFramework(c, result, err, http.StatusOK)
}

func (h *Handler) Select(c *gin.Context) {
	owner, ok := frameworkOwner(c)
	if !ok {
		return
	}
	var preview selectionPreviewRequest
	if !decodeFrameworkJSON(c, &preview) {
		return
	}
	request := SelectionRequest{
		OwnerIdentity:       owner,
		TaskPlanID:          preview.TaskPlanID,
		Request:             preview.Request,
		ProjectKey:          preview.ProjectKey,
		PursuitID:           preview.PursuitID,
		TaskType:            preview.TaskType,
		Difficulty:          preview.Difficulty,
		RequiredReasoning:   preview.RequiredReasoning,
		SuccessCriteria:     preview.SuccessCriteria,
		NeedsMemory:         preview.NeedsMemory,
		NeedsTools:          preview.NeedsTools,
		NeedsDocuments:      preview.NeedsDocuments,
		NeedsWebAccess:      preview.NeedsWebAccess,
		NeedsLocalExecution: preview.NeedsLocalExecution,
		ExecuteRequested:    preview.ExecuteRequested,
	}
	result, err := h.service.Select(request)
	respondFramework(c, result, err, http.StatusOK)
}

func (h *Handler) UpdatePreference(c *gin.Context) {
	owner, ok := frameworkOwner(c)
	if !ok {
		return
	}
	var patch PreferencePatch
	if !decodeFrameworkJSON(c, &patch) {
		return
	}
	result, err := h.service.UpdatePreference(owner, c.Param("id"), patch)
	respondFramework(c, result, err, http.StatusOK)
}

func (h *Handler) Selections(c *gin.Context) {
	owner, ok := frameworkOwner(c)
	if !ok {
		return
	}
	limit, ok := frameworkListLimit(
		c,
		"selection",
		defaultSelectionLimit,
		maxSelectionLimit,
	)
	if !ok {
		return
	}
	result, err := h.service.Selections(owner, limit)
	respondFramework(c, gin.H{"selections": result}, err, http.StatusOK)
}

func (h *Handler) Selection(c *gin.Context) {
	owner, ok := frameworkOwner(c)
	if !ok {
		return
	}
	result, err := h.service.Selection(c.Request.Context(), owner, c.Param("id"))
	respondFramework(c, result, err, http.StatusOK)
}

func (h *Handler) Constitution(c *gin.Context) {
	owner, ok := frameworkOwner(c)
	if !ok {
		return
	}
	result, source, err := h.service.ActiveConstitution(owner)
	if err != nil {
		respondFramework(c, nil, err, http.StatusOK)
		return
	}
	c.JSON(http.StatusOK, gin.H{"constitution": result, "source": source})
}

func (h *Handler) ConstitutionHistory(c *gin.Context) {
	owner, ok := frameworkOwner(c)
	if !ok {
		return
	}
	limit, ok := frameworkListLimit(
		c,
		"Constitution history",
		defaultHistoryLimit,
		maxHistoryLimit,
	)
	if !ok {
		return
	}
	result, err := h.service.ConstitutionHistory(owner, limit)
	respondFramework(c, result, err, http.StatusOK)
}

func (h *Handler) CreateConstitutionDraft(c *gin.Context) {
	owner, ok := frameworkOwner(c)
	if !ok {
		return
	}
	var request ConstitutionDraftRequest
	if !decodeFrameworkJSON(c, &request) {
		return
	}
	result, err := h.service.CreateConstitutionDraft(owner, request)
	respondFramework(c, result, err, http.StatusCreated)
}

func (h *Handler) ActivateConstitution(c *gin.Context) {
	owner, ok := frameworkOwner(c)
	if !ok {
		return
	}
	var request ActivateConstitutionRequest
	if !decodeFrameworkJSON(c, &request) {
		return
	}
	result, err := h.service.ActivateConstitution(owner, c.Param("id"), owner, request)
	respondFramework(c, result, err, http.StatusOK)
}

func frameworkOwner(c *gin.Context) (string, bool) {
	value, exists := c.Get(identity.ContextSubjectKey)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "an authenticated owner session is required for framework registry access"})
		return "", false
	}
	owner, ok := value.(string)
	owner = strings.TrimSpace(owner)
	if !ok || owner == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "an authenticated owner session is required for framework registry access"})
		return "", false
	}
	return owner, true
}

func decodeFrameworkJSON(c *gin.Context, target any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxFrameworkRequestBytes)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "framework registry request is too large"})
			return false
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid framework registry request"})
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "framework registry request must contain one JSON object"})
		return false
	}
	return true
}

func frameworkListLimit(
	c *gin.Context,
	resource string,
	defaultLimit int,
	maxLimit int,
) (int, bool) {
	values, exists := c.Request.URL.Query()["limit"]
	if !exists {
		return defaultLimit, true
	}
	if len(values) != 1 {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": resource + " limit must be provided exactly once",
		})
		return 0, false
	}
	raw := strings.TrimSpace(values[0])
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 1 || limit > maxLimit {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": fmt.Sprintf(
				"%s limit must be between 1 and %d",
				resource,
				maxLimit,
			),
		})
		return 0, false
	}
	return limit, true
}

func respondFramework(c *gin.Context, value any, err error, successStatus int) {
	if err == nil {
		c.JSON(successStatus, value)
		return
	}

	status, publicMessage := frameworkErrorResponse(err)
	if status == http.StatusInternalServerError || status == http.StatusConflict {
		errorID := uuid.NewString()
		internalDetail := compactRedactedText(err.Error(), maxApprovalNoteRunes)
		_ = c.Error(fmt.Errorf("framework registry error %s: %s", errorID, internalDetail))
		c.JSON(status, gin.H{
			"error":   publicMessage,
			"errorId": errorID,
		})
		return
	}
	c.JSON(status, gin.H{"error": publicMessage})
}

func frameworkErrorResponse(err error) (int, string) {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return http.StatusNotFound, "framework registry record not found"
	}
	message := strings.ToLower(strings.TrimSpace(err.Error()))
	switch {
	case strings.Contains(message, "not found"):
		return http.StatusNotFound, "framework registry record not found"
	case strings.Contains(message, "stale"),
		strings.Contains(message, "already exists"),
		strings.Contains(message, "lost its state precondition"),
		strings.Contains(message, "cannot be activated"):
		return http.StatusConflict, "framework registry state conflict"
	case isFrameworkValidationError(message):
		return http.StatusBadRequest, frameworkValidationPublicMessage(message)
	default:
		return http.StatusInternalServerError, "framework registry operation failed"
	}
}

func frameworkValidationPublicMessage(message string) string {
	switch {
	case strings.Contains(message, "is required"):
		return "a required framework registry field is missing"
	case strings.Contains(message, "exceeds "),
		strings.Contains(message, "at most "),
		strings.Contains(message, "between "):
		return "a framework registry value is outside its allowed bounds"
	case strings.Contains(message, "may only "),
		strings.Contains(message, "cannot modify "),
		strings.Contains(message, "conflicts with a protected rule"):
		return "the requested framework registry change is not allowed"
	default:
		return "framework registry request failed validation"
	}
}

func isFrameworkValidationError(message string) bool {
	for _, fragment := range []string{
		"is required",
		"invalid preference state",
		"invalid risk level",
		"invalid typed constitution rule",
		"must ",
		"may only ",
		"cannot modify ",
		"conflicts with a protected rule",
		"exceeds ",
		"at most ",
		"between ",
	} {
		if strings.Contains(message, fragment) {
			return true
		}
	}
	return false
}
