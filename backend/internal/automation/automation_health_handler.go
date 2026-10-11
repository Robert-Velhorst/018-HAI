package automation

import (
	"automation-hub-backend/internal/agentruntime"
	"automation-hub-backend/internal/apierror"
	"automation-hub-backend/internal/executionauth"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"net/http"
)

func (h *Handler) HealthSummary(c *gin.Context) {
	summary, err := h.service.HealthSummary()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": apierror.PublicMessage(err, "automation health summary is unavailable")})
		return
	}
	c.JSON(http.StatusOK, summary)
}

func (h *Handler) Launch(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID format"})
		return
	}
	actor := verifiedAutomationActor(c)
	if actor == "" {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "an authenticated operator session is required for automation access"})
		return
	}
	request, err := readLaunchRequest(c)
	if err != nil {
		status := http.StatusBadRequest
		var sizeError *http.MaxBytesError
		if errors.As(err, &sizeError) {
			status = http.StatusRequestEntityTooLarge
		}
		c.JSON(status, gin.H{"error": "launch request must be one JSON object within 64 KiB"})
		return
	}
	request.IdempotencyKey = strings.TrimSpace(request.IdempotencyKey)
	headerKey := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	if compatibilityKey := strings.TrimSpace(c.GetHeader("X-HAI-Idempotency-Key")); compatibilityKey != "" {
		if headerKey != "" && headerKey != compatibilityKey {
			c.JSON(http.StatusBadRequest, gin.H{"error": "conflicting idempotency headers"})
			return
		}
		headerKey = compatibilityKey
	}
	if headerKey != "" && request.IdempotencyKey != "" && headerKey != request.IdempotencyKey {
		c.JSON(http.StatusBadRequest, gin.H{"error": "conflicting idempotency key values"})
		return
	}
	if headerKey != "" {
		request.IdempotencyKey = headerKey
	}
	// Authority-bearing identity and approval fields remain server-owned. The
	// optional mandateId is only a reference to owner-scoped policy evaluated
	// by executionauth; it is never accepted as proof of authorization.
	request.OwnerIdentity = actor
	request.ActorIdentity = actor
	request.ActorKind = executionauth.ActorHuman
	request.ExecutionContext = c.Request.Context()
	request.ApprovalSourceID = ""
	request.ApprovalBindingDigest = ""
	request.ApprovalProof = nil
	result, err := h.service.LaunchTask(id, request)
	if err != nil {
		status := http.StatusInternalServerError
		switch {
		case errors.Is(err, ErrLaunchConfigurationContextUnavailable), errors.Is(err, ErrLaunchStorageContextUnavailable):
			status = http.StatusServiceUnavailable
		case errors.Is(err, ErrLaunchIdempotencyKeyRequired):
			status = http.StatusPreconditionRequired
		case errors.Is(err, ErrLaunchIdempotencyKeyInvalid):
			status = http.StatusBadRequest
		case errors.Is(err, ErrLaunchIdempotencyConflict):
			status = http.StatusConflict
		}
		body := gin.H{"error": apierror.PublicMessage(err, "automation launch could not be completed")}
		if actor != "" && !errors.Is(err, ErrLaunchIdempotencyConflict) {
			if recovery := publicLaunchRecovery(id, result); recovery != nil {
				body["recovery"] = recovery
			}
		}
		c.JSON(status, body)
		return
	}
	c.JSON(http.StatusOK, result)
}

const maxLaunchRequestBytes = 64 * 1024

func readLaunchRequest(c *gin.Context) (TaskLaunchRequest, error) {
	if c.Request.ContentLength > maxLaunchRequestBytes {
		return TaskLaunchRequest{}, &http.MaxBytesError{Limit: maxLaunchRequestBytes}
	}
	if c.Request.Body == nil {
		return TaskLaunchRequest{}, nil
	}
	decoder := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, maxLaunchRequestBytes))
	var request *TaskLaunchRequest
	err := decoder.Decode(&request)
	if err == io.EOF {
		return TaskLaunchRequest{}, nil
	}
	if err != nil {
		return TaskLaunchRequest{}, err
	}
	if request == nil {
		return TaskLaunchRequest{}, errors.New("null launch request")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			err = errors.New("multiple launch request values")
		}
		return TaskLaunchRequest{}, err
	}
	return *request, nil
}

// Recovery references are evidence, not success or permission to dispatch again.
func publicLaunchRecovery(id uuid.UUID, result *LaunchResult) gin.H {
	if result == nil || id == uuid.Nil || result.AutomationID != id || result.LaunchEventID == uuid.Nil {
		return nil
	}
	status := strings.ToLower(strings.TrimSpace(result.Status))
	switch status {
	case "ready", "running", "waiting", "blocked", "needs_approval", "failed", "completed", "indeterminate":
	default:
		status = "unknown"
	}
	return gin.H{
		"automationId": id, "launchEventId": result.LaunchEventID,
		"reportedStatus": status, "reconciliationRequired": true, "retryAllowed": false,
	}
}

func (h *Handler) StopRuntimeTask(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID format"})
		return
	}
	actor := verifiedAutomationActor(c)
	if actor == "" {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "an authenticated operator session is required for automation access"})
		return
	}
	result, err := h.service.StopRuntimeTaskForOwnerContext(c.Request.Context(), id, actor)
	if err != nil {
		body := gin.H{"error": apierror.PublicMessage(err, "automation runtime could not be stopped")}
		if recovery := publicStopRecovery(id, result); recovery != nil {
			body["recovery"] = recovery
		}
		c.JSON(http.StatusInternalServerError, body)
		return
	}
	valid := false
	if result != nil {
		switch result.Status {
		case "cancellation_requested", "stopping", "blocked", "failed", "indeterminate":
			valid = true
		case "stopped", "cancelled":
			valid = publicStopRecovery(id, result) != nil
		}
	}
	if !valid {
		body := gin.H{"error": "runtime stop not confirmed: the service returned an incomplete or unsupported response"}
		if recovery := publicStopRecovery(id, result); recovery != nil {
			body["recovery"] = recovery
		}
		c.JSON(http.StatusBadGateway, body)
		return
	}
	status := http.StatusOK
	if result.Status == "blocked" {
		status = http.StatusBadRequest
	}
	c.JSON(status, result)
}

// A candidate audit ID may come from an uncertain write; it is not a receipt
// proving persistence, termination or permission to repeat the stop.
func publicStopRecovery(id uuid.UUID, result *agentruntime.StopResult) gin.H {
	if id == uuid.Nil || result == nil || !isSupportedAgentRuntime(strings.ToLower(strings.TrimSpace(result.RuntimeID))) || strings.TrimSpace(result.TaskID) == "" {
		return nil
	}
	const prefix = "automation-launch://"
	if !strings.HasPrefix(result.EvidenceURI, prefix) {
		return nil
	}
	value := strings.TrimPrefix(result.EvidenceURI, prefix)
	eventID, err := uuid.Parse(value)
	if err != nil || eventID == uuid.Nil || strings.ToLower(value) != eventID.String() {
		return nil
	}
	status := strings.ToLower(strings.TrimSpace(result.Status))
	switch status {
	case "stopped", "cancelled", "cancellation_requested", "stopping", "blocked", "failed", "indeterminate":
	default:
		status = "unknown"
	}
	return gin.H{"automationId": id, "candidateStopEventId": eventID,
		"candidateEvidenceUri": prefix + eventID.String(), "reportedStatus": status,
		"reconciliationRequired": true, "retryAllowed": false}
}

func (h *Handler) RunHealthCheck(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID format"})
		return
	}
	result, err := h.service.RunHealthCheck(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": apierror.PublicMessage(err, "automation health check could not be completed")})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *Handler) HealthCheck(c *gin.Context) {
	h.RunHealthCheck(c)
}

func (h *Handler) Diagnostics(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID format"})
		return
	}
	actor := verifiedAutomationActor(c)
	if actor == "" {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "an authenticated operator session is required for automation access"})
		return
	}
	result, err := h.service.DiagnosticsForOwner(id, actor)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": apierror.PublicMessage(err, "automation diagnostics are unavailable")})
		return
	}
	c.JSON(http.StatusOK, publicDiagnostics(result))
}
