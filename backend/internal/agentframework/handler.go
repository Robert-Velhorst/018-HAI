package agentframework

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
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
	if c.Request.Context().Err() != nil {
		return
	}
	result, err := h.service.Probe(c.Request.Context())
	if writeContextError(c, err) {
		return
	}
	if errors.Is(err, ErrNotConfigured) {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "local Agent Framework planning runner is not configured"})
		return
	}
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "local Agent Framework planning runner or its fixed local model could not be reached"})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *Handler) Propose(c *gin.Context) {
	if c.Request.Context().Err() != nil {
		return
	}
	var request Request
	if err := bindProposeJSON(c, &request); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "local Agent Framework planning request exceeds the 16 KiB limit"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid local Agent Framework planning request"})
		return
	}
	if c.Request.Context().Err() != nil {
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
	if writeContextError(c, err) {
		return
	}
	if errors.Is(err, ErrNotConfigured) {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "local Agent Framework planning runner is not configured"})
		return
	}
	if errors.Is(err, ErrInvalidRequest) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Agent Framework accepts one short task request and up to eight short success criteria"})
		return
	}
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "local Agent Framework planning runner could not return a bounded planning draft"})
		return
	}
	if result == nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "local Agent Framework planning runner could not return a bounded planning draft"})
		return
	}
	if result.GuidanceStatus == "" {
		result.GuidanceStatus = guidanceStatus
	}
	c.JSON(http.StatusOK, result)
}

func bindProposeJSON(c *gin.Context, destination any) error {
	const maxBodyBytes = 16 << 10
	body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, maxBodyBytes))
	if err != nil {
		return err
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(body))
	if proposeJSONHasTrailingValue(body) {
		return errors.New("request body must contain exactly one JSON value")
	}
	return c.ShouldBindJSON(destination)
}

func proposeJSONHasTrailingValue(body []byte) bool {
	decoder := json.NewDecoder(bytes.NewReader(body))
	var first json.RawMessage
	if err := decoder.Decode(&first); err != nil {
		return false
	}
	var trailing json.RawMessage
	return decoder.Decode(&trailing) != io.EOF
}

func writeContextError(c *gin.Context, err error) bool {
	if errors.Is(err, context.Canceled) {
		return true
	}
	if errors.Is(err, context.DeadlineExceeded) {
		c.JSON(http.StatusGatewayTimeout, gin.H{"error": "local Agent Framework request timed out"})
		return true
	}
	return false
}
