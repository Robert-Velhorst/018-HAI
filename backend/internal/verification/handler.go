package verification

import (
	"automation-hub-backend/internal/identity"
	"context"
	"errors"
	"net/http"
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

func (h *Handler) Answer(c *gin.Context) {
	var request AnswerRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if request.Question == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "question is required"})
		return
	}
	// Approval provenance must come from a server-side approval workflow,
	// never from a caller asserting approval in request JSON.
	request.HumanApproved = false
	// Evidence authority is also server-owned provenance. The verification
	// service independently fails closed, and this strips HTTP assertions at
	// the boundary so a trusted resolver cannot accidentally consume them.
	for index := range request.ExternalEvidence {
		request.ExternalEvidence[index].Authority = ""
		request.ExternalEvidence[index].Official = false
		request.ExternalEvidence[index].Primary = false
	}
	request.OwnerIdentity = verifiedOwner(c)
	result, err := AnswerWithContext(h.service, c.Request.Context(), request)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *Handler) Runs(c *gin.Context) {
	runs, err := h.service.RunsForOwner(verifiedOwner(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, runs)
}

func (h *Handler) RunDetails(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	result, err := h.service.RunDetailsForOwner(verifiedOwner(c), id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "verification run not found"})
		return
	}
	c.JSON(http.StatusOK, result)
}

func verifiedOwner(c *gin.Context) string {
	value, ok := c.Get(identity.ContextSubjectKey)
	if !ok {
		return ""
	}
	owner, ok := value.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(owner)
}
