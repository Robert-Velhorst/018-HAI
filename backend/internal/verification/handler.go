package verification

import (
	"automation-hub-backend/internal/apierror"
	"automation-hub-backend/internal/identity"
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
	ownerIdentity := verifiedOwner(c)
	if ownerIdentity == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "authentication required"})
		return
	}
	var request AnswerRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid verification request"})
		return
	}
	if request.Question == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "question is required"})
		return
	}
	// This endpoint has no server-side approval-record validator. Caller-supplied
	// approval fields are discarded and high-risk claims stay gated.
	request.HumanApproved = false
	request.HumanApprovalReference = ""
	// Evidence authority is also server-owned provenance. The verification
	// service independently fails closed, and this strips HTTP assertions at
	// the boundary so a trusted resolver cannot accidentally consume them.
	for index := range request.ExternalEvidence {
		request.ExternalEvidence[index].Authority = ""
		request.ExternalEvidence[index].Official = false
		request.ExternalEvidence[index].Primary = false
	}
	request.OwnerIdentity = ownerIdentity
	result, err := h.service.Answer(request)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": apierror.PublicMessage(err, "verification could not be completed")})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *Handler) Runs(c *gin.Context) {
	ownerIdentity := verifiedOwner(c)
	if ownerIdentity == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "authentication required"})
		return
	}
	runs, err := h.service.RunsForOwner(ownerIdentity)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "verification runs are unavailable"})
		return
	}
	c.JSON(http.StatusOK, runs)
}

func (h *Handler) RunDetails(c *gin.Context) {
	ownerIdentity := verifiedOwner(c)
	if ownerIdentity == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "authentication required"})
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	result, err := h.service.RunDetailsForOwner(ownerIdentity, id)
	if err != nil {
		if errors.Is(err, ErrVerificationRunNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "verification run not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "verification run details are unavailable"})
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
