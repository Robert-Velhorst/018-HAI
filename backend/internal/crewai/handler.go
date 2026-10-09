package crewai

import (
	"context"
	"errors"
	"net/http"

	"automation-hub-backend/internal/agentguidance"
	"automation-hub-backend/internal/identity"

	"github.com/gin-gonic/gin"
)

type Handler struct {
	service          Service
	guidanceProvider agentguidance.Provider
}

func NewHandler(service Service) *Handler { return &Handler{service: service} }

func NewHandlerWithGuidance(service Service, provider agentguidance.Provider) *Handler {
	return &Handler{service: service, guidanceProvider: provider}
}
func (h *Handler) Status(c *gin.Context) { c.JSON(http.StatusOK, h.service.Status()) }

func (h *Handler) Probe(c *gin.Context) {
	result, err := h.service.Probe(c.Request.Context())
	if errors.Is(err, ErrNotConfigured) {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "local CrewAI planning runner is not configured"})
		return
	}
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "local CrewAI planning runner or its fixed local model could not be reached"})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *Handler) Propose(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16<<10)
	var request Request
	if c.ShouldBindJSON(&request) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid local CrewAI planning request"})
		return
	}
	owner, _ := c.Get(identity.ContextSubjectKey)
	ownerIdentity, _ := owner.(string)
	guidance, guidanceStatus := agentguidance.Resolve(c.Request.Context(), h.guidanceProvider, ownerIdentity, "planning", request.Request)
	var result *Response
	var err error
	if len(guidance) > 0 {
		if guided, ok := h.service.(interface {
			ProposeWithGuidance(context.Context, Request, []agentguidance.Item) (*Response, error)
		}); ok {
			result, err = guided.ProposeWithGuidance(c.Request.Context(), request, guidance)
			if errors.Is(err, agentguidance.ErrInvalidGuidance) {
				guidanceStatus = "invalid_selection"
				result, err = h.service.Propose(c.Request.Context(), request)
			}
		} else {
			guidanceStatus = "unsupported"
			result, err = h.service.Propose(c.Request.Context(), request)
		}
	} else {
		result, err = h.service.Propose(c.Request.Context(), request)
	}
	if errors.Is(err, ErrNotConfigured) {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "local CrewAI planning runner is not configured"})
		return
	}
	if errors.Is(err, ErrInvalidRequest) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "CrewAI accepts one short task request and up to eight short success criteria"})
		return
	}
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "local CrewAI planning runner could not return a bounded planning draft"})
		return
	}
	if result.GuidanceStatus == "" {
		result.GuidanceStatus = guidanceStatus
	}
	c.JSON(http.StatusOK, result)
}
