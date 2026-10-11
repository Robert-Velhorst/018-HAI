package memoryengine

import (
	"automation-hub-backend/internal/apierror"
	"automation-hub-backend/internal/identity"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type Handler struct {
	service Service
}

func NewHandler(service Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) Import(c *gin.Context) {
	var request ImportRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid memory import request"})
		return
	}
	request.OwnerIdentity = verifiedOwner(c)
	result, err := h.service.Import(request)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": apierror.PublicMessage(err, "memory import could not be completed")})
		return
	}
	status := http.StatusCreated
	if result.Deduplicated {
		status = http.StatusOK
	}
	c.JSON(status, result)
}

func (h *Handler) Conversations(c *gin.Context) {
	limit, _ := strconv.Atoi(c.Query("limit"))
	result, err := h.service.ConversationsForOwner(verifiedOwner(c), limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "memory conversations are unavailable"})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *Handler) Conversation(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	result, err := h.service.ConversationForOwner(verifiedOwner(c), id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": apierror.PublicMessage(err, "conversation not found")})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *Handler) DeleteConversation(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	if err := h.service.DeleteConversationForOwner(verifiedOwner(c), id); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "conversation not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "conversation could not be deleted"})
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) Insights(c *gin.Context) {
	limit, _ := strconv.Atoi(c.Query("limit"))
	var needsReview *bool
	if raw := c.Query("needsReview"); raw != "" {
		value, err := strconv.ParseBool(raw)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid needsReview"})
			return
		}
		needsReview = &value
	}
	result, err := h.service.InsightsForOwner(verifiedOwner(c), c.Query("kind"), c.Query("projectKey"), needsReview, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "memory insights are unavailable"})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *Handler) Dashboard(c *gin.Context) {
	result, err := h.service.DashboardForOwner(verifiedOwner(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "memory dashboard is unavailable"})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *Handler) Search(c *gin.Context) {
	var request struct {
		Query      string `json:"query"`
		ProjectKey string `json:"projectKey,omitempty"`
		Limit      int    `json:"limit,omitempty"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid memory search request"})
		return
	}
	result, err := h.service.SearchForOwner(verifiedOwner(c), request.Query, request.ProjectKey, request.Limit)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": apierror.PublicMessage(err, "memory search could not be completed")})
		return
	}
	c.JSON(http.StatusOK, result)
}

func parseID(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return uuid.Nil, false
	}
	return id, true
}

// verifiedOwner is populated by the gateway after validating the session. It
// deliberately ignores any owner field supplied in browser JSON.
func verifiedOwner(c *gin.Context) string {
	if value, ok := c.Get(identity.ContextSubjectKey); ok {
		if subject, ok := value.(string); ok {
			return strings.TrimSpace(subject)
		}
	}
	return ""
}
