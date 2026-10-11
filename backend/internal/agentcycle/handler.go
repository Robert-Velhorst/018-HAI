package agentcycle

import (
	"automation-hub-backend/internal/identity"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) Run(c *gin.Context) {
	if h.service == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "agent cycle service is not configured"})
		return
	}
	ownerIdentity := verifiedOwner(c)
	if ownerIdentity == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "an authenticated owner session is required to refresh an operating brief"})
		return
	}
	var request RunRequest
	_ = c.ShouldBindJSON(&request)
	request.OwnerIdentity = ownerIdentity
	result := h.service.Run(request)
	result = publicRunResult(result)
	status := http.StatusOK
	if result.Status == "failed" {
		status = http.StatusServiceUnavailable
	}
	c.JSON(status, result)
}

// publicRunResult keeps detailed diagnostics inside the worker/audit path.
// Downstream storage, connector, and provider errors can contain credentials,
// URLs, or filesystem paths, so the browser receives only a stable phase-level
// failure description.
func publicRunResult(result *RunResult) *RunResult {
	if result == nil {
		return nil
	}
	copyResult := *result
	copyResult.Errors = append([]PhaseError(nil), result.Errors...)
	for index := range copyResult.Errors {
		phase := strings.TrimSpace(copyResult.Errors[index].Phase)
		if phase == "" {
			phase = "agent cycle"
		}
		copyResult.Errors[index].Message = phase + " could not be completed"
	}
	copyResult.Steps = append([]WorkerStep(nil), result.Steps...)
	for index := range copyResult.Steps {
		if copyResult.Steps[index].Status == "failed" {
			name := strings.TrimSpace(copyResult.Steps[index].Name)
			if name == "" {
				name = "agent-cycle phase"
			}
			copyResult.Steps[index].Summary = name + " could not be completed"
		}
	}
	return &copyResult
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
