package hostruntime

import (
	"crypto/subtle"
	"errors"
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const maxCompletionRequestBytes = maxResultBytes*2 + 2048
const maxStartProtocolRequestBytes = 2048

type Config struct {
	Enabled  bool
	Token    string
	WorkerID string
}

func DefaultConfig() Config {
	return Config{
		Enabled:  strings.EqualFold(strings.TrimSpace(os.Getenv("HAI_HOST_RUNTIME_BRIDGE_ENABLED")), "true"),
		Token:    strings.TrimSpace(os.Getenv("HAI_HOST_RUNTIME_BRIDGE_TOKEN")),
		WorkerID: strings.TrimSpace(os.Getenv("HAI_HOST_RUNTIME_BRIDGE_WORKER_ID")),
	}
}

type Handler struct {
	service  *Service
	config   Config
	enabled  bool
	workerID string
}

func NewHandler(service *Service, config Config) *Handler {
	workerID := strings.TrimSpace(config.WorkerID)
	if workerID == "" {
		workerID = "windows-dsh"
	}
	return &Handler{
		service:  service,
		config:   config,
		enabled:  config.Enabled && len(strings.TrimSpace(config.Token)) >= 32,
		workerID: workerID,
	}
}

func (h *Handler) RegisterRoutes(routes gin.IRoutes) {
	routes.POST("/leases", h.Lease)
	routes.POST("/leases/:id/confirm", h.Confirm)
	routes.POST("/leases/:id/start-intent", h.BeginStartIntent)
	routes.POST("/leases/:id/start-ack", h.AcknowledgeActualStart)
	routes.POST("/leases/:id/start-unknown", h.ReportStartOutcomeUnknown)
	routes.POST("/leases/:id/complete", h.Complete)
}

func (h *Handler) Lease(c *gin.Context) {
	if !h.authorized(c) {
		return
	}
	lease, err := h.service.Lease(h.workerID, "deepseek-harness")
	if errors.Is(err, ErrEmergencyStopped) {
		// Keep the local worker alive and polling, but do not hand it any new
		// work until the operator clears the stop.
		c.Status(http.StatusNoContent)
		return
	}
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "host runtime lease is unavailable"})
		return
	}
	if lease == nil {
		c.Status(http.StatusNoContent)
		return
	}
	c.JSON(http.StatusOK, lease)
}

type completionRequest struct {
	LeaseToken            string `json:"leaseToken"`
	ExitCode              *int   `json:"exitCode"`
	Output                string `json:"output"`
	Error                 string `json:"error"`
	CancellationRequested bool   `json:"cancellationRequested"`
	TerminationVerified   bool   `json:"terminationVerified"`
}

type confirmRequest struct {
	LeaseToken string `json:"leaseToken"`
}

type startProtocolRequest struct {
	LeaseToken     string    `json:"leaseToken"`
	IntentID       uuid.UUID `json:"intentId"`
	ApprovalDigest string    `json:"approvalDigest"`
	StopRevision   uint64    `json:"stopRevision"`
}

func (h *Handler) BeginStartIntent(c *gin.Context) {
	if !h.authorized(c) {
		return
	}
	id, request, ok := h.parseStartProtocolRequest(c)
	if !ok {
		return
	}
	intent, err := h.service.BeginStartIntent(c.Request.Context(), h.workerID, id, request.LeaseToken, StartIntentBinding{
		IntentID: request.IntentID, ApprovalDigest: request.ApprovalDigest, StopRevision: request.StopRevision,
	})
	if err != nil {
		h.writeStartProtocolError(c, err)
		return
	}
	c.JSON(http.StatusOK, intent)
}

func (h *Handler) AcknowledgeActualStart(c *gin.Context) {
	if !h.authorized(c) {
		return
	}
	id, request, ok := h.parseStartProtocolRequest(c)
	if !ok {
		return
	}
	err := h.service.AcknowledgeActualStart(c.Request.Context(), h.workerID, id, request.LeaseToken, StartIntentBinding{
		IntentID: request.IntentID, ApprovalDigest: request.ApprovalDigest, StopRevision: request.StopRevision,
	})
	if err != nil {
		h.writeStartProtocolError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) ReportStartOutcomeUnknown(c *gin.Context) {
	if !h.authorized(c) {
		return
	}
	id, request, ok := h.parseStartProtocolRequest(c)
	if !ok {
		return
	}
	_, err := h.service.ReportStartOutcomeUnknown(c.Request.Context(), h.workerID, id, request.LeaseToken, StartIntentBinding{
		IntentID: request.IntentID, ApprovalDigest: request.ApprovalDigest, StopRevision: request.StopRevision,
	})
	if err != nil {
		h.writeStartProtocolError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) parseStartProtocolRequest(c *gin.Context) (uuid.UUID, startProtocolRequest, bool) {
	id, err := uuid.Parse(strings.TrimSpace(c.Param("id")))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "host runtime job id is invalid"})
		return uuid.Nil, startProtocolRequest{}, false
	}
	if c.Request.ContentLength > maxStartProtocolRequestBytes {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "host runtime start protocol request is too large"})
		return uuid.Nil, startProtocolRequest{}, false
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxStartProtocolRequestBytes)
	var request startProtocolRequest
	if err := c.ShouldBindJSON(&request); err != nil || strings.TrimSpace(request.LeaseToken) == "" || request.IntentID == uuid.Nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "host runtime start protocol request is invalid"})
		return uuid.Nil, startProtocolRequest{}, false
	}
	return id, request, true
}

func (h *Handler) writeStartProtocolError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrExecutionIsolationUnavailable):
		c.JSON(http.StatusLocked, gin.H{"code": "execution_isolation_unavailable", "error": "host runtime execution is blocked because operating-system isolation is unavailable"})
	case errors.Is(err, ErrEmergencyStopped):
		c.JSON(http.StatusLocked, gin.H{"code": "emergency_stopped", "error": "host runtime execution is blocked by emergency stop"})
	case errors.Is(err, ErrStopRevisionUnavailable):
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "stop_revision_unavailable", "error": "host runtime stop state is unavailable"})
	case errors.Is(err, ErrStartOutcomeReview):
		c.JSON(http.StatusConflict, gin.H{"code": "start_outcome_review", "error": "host runtime start outcome requires operator review and cannot be retried"})
	case errors.Is(err, ErrStartIntentReplay):
		c.JSON(http.StatusConflict, gin.H{"code": "start_intent_replay", "error": "host runtime start intent was already consumed"})
	case errors.Is(err, ErrCancellationRequested):
		c.JSON(http.StatusConflict, gin.H{"code": "cancellation_requested", "error": "host runtime cancellation was requested"})
	case errors.Is(err, ErrStaleLease):
		c.JSON(http.StatusConflict, gin.H{"code": "stale_lease", "error": "host runtime lease is no longer valid"})
	default:
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "host runtime start protocol is unavailable"})
	}
}

func (h *Handler) Confirm(c *gin.Context) {
	if !h.authorized(c) {
		return
	}
	id, err := uuid.Parse(strings.TrimSpace(c.Param("id")))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "host runtime job id is invalid"})
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 2048)
	var request confirmRequest
	if err := c.ShouldBindJSON(&request); err != nil || strings.TrimSpace(request.LeaseToken) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "host runtime confirmation is invalid"})
		return
	}
	if err := h.service.ConfirmLease(h.workerID, id, request.LeaseToken); err != nil {
		switch {
		case errors.Is(err, ErrExecutionIsolationUnavailable):
			c.JSON(http.StatusLocked, gin.H{"code": "execution_isolation_unavailable", "error": "host runtime execution is blocked because operating-system isolation is unavailable"})
		case errors.Is(err, ErrEmergencyStopped):
			c.JSON(http.StatusLocked, gin.H{"error": "host runtime execution is blocked by emergency stop"})
		case errors.Is(err, ErrStaleLease):
			c.JSON(http.StatusConflict, gin.H{"error": "host runtime lease is no longer valid"})
		case errors.Is(err, ErrCancellationRequested):
			c.JSON(http.StatusConflict, gin.H{"code": "cancellation_requested", "error": "host runtime cancellation was requested"})
		default:
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "host runtime confirmation is unavailable"})
		}
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) Complete(c *gin.Context) {
	if !h.authorized(c) {
		return
	}
	id, err := uuid.Parse(strings.TrimSpace(c.Param("id")))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "host runtime job id is invalid"})
		return
	}
	if c.Request.ContentLength > maxCompletionRequestBytes {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "host runtime completion is too large"})
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxCompletionRequestBytes)
	var request completionRequest
	if err := c.ShouldBindJSON(&request); err != nil || strings.TrimSpace(request.LeaseToken) == "" || request.ExitCode == nil || (request.TerminationVerified && !request.CancellationRequested) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "host runtime completion is invalid"})
		return
	}
	job, err := h.service.Complete(h.workerID, id, request.LeaseToken, Completion{
		ExitCode:              *request.ExitCode,
		Output:                request.Output,
		Error:                 request.Error,
		CancellationRequested: request.CancellationRequested,
		TerminationVerified:   request.TerminationVerified,
	})
	if errors.Is(err, ErrStaleLease) {
		c.JSON(http.StatusConflict, gin.H{"error": "host runtime lease is no longer valid"})
		return
	}
	if errors.Is(err, ErrCancellationRequested) {
		c.JSON(http.StatusConflict, gin.H{"code": "cancellation_requested", "error": "host runtime cancellation was requested"})
		return
	}
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "host runtime completion was rejected"})
		return
	}
	c.JSON(http.StatusOK, job)
}

func (h *Handler) authorized(c *gin.Context) bool {
	if h == nil || h.service == nil || !h.enabled {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "host runtime bridge is disabled"})
		return false
	}
	provided := bearerToken(c.GetHeader("Authorization"))
	expected := strings.TrimSpace(h.config.Token)
	if len(provided) != len(expected) || subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) != 1 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "host runtime bridge token is required"})
		return false
	}
	return true
}

func bearerToken(header string) string {
	header = strings.TrimSpace(header)
	if len(header) <= 7 || !strings.EqualFold(header[:7], "Bearer ") {
		return ""
	}
	return strings.TrimSpace(header[7:])
}
