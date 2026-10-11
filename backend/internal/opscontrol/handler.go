package opscontrol

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// Handler serves the always-on runtime control API (§30/§31): background status,
// pause/resume (emergency stop), mode, recovery, and readiness.
type Handler struct {
	svc *Service
}

// NewHandler builds a handler over a service.
func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) actor(c *gin.Context) (string, bool) {
	if sub, ok := c.Get("subject"); ok {
		if s, ok := sub.(string); ok && s != "" {
			return s, true
		}
	}
	return "", false
}

// Status returns the background/runtime status.
func (h *Handler) Status(c *gin.Context) {
	c.JSON(http.StatusOK, h.svc.Status(c.Request.Context()))
}

type pauseRequest struct {
	Reason string `json:"reason"`
}

// Pause engages the emergency stop (halts background processing).
func (h *Handler) Pause(c *gin.Context) {
	var req pauseRequest
	_ = c.ShouldBindJSON(&req)
	actor, _ := h.actor(c)
	state, err := h.svc.EngageEmergencyStop(req.Reason, actor)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error":         "failed to persist emergency-stop state; execution remains blocked",
			"emergencyStop": h.svc.Control().EmergencyState(),
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{"emergencyStop": state})
}

type controlAuthorizationRequest struct {
	IdempotencyKey        string `json:"idempotencyKey"`
	TaskID                string `json:"taskId"`
	ApprovalSourceID      string `json:"approvalSourceId"`
	ApprovalBindingDigest string `json:"approvalBindingDigest"`
}

func (h *Handler) controlAuthorization(
	c *gin.Context,
	req controlAuthorizationRequest,
) ControlAuthorization {
	actor, _ := h.actor(c)
	return ControlAuthorization{
		ActorIdentity:         actor,
		IdempotencyKey:        req.IdempotencyKey,
		TaskID:                req.TaskID,
		ApprovalSourceID:      req.ApprovalSourceID,
		ApprovalBindingDigest: req.ApprovalBindingDigest,
	}
}

// Resume disengages the emergency stop.
func (h *Handler) Resume(c *gin.Context) {
	var req controlAuthorizationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "authorization request is required"})
		return
	}
	state, err := h.svc.DisengageEmergencyStop(
		c.Request.Context(),
		h.controlAuthorization(c, req),
	)
	if err != nil {
		c.JSON(controlErrorStatus(err), gin.H{
			"error":         err.Error(),
			"emergencyStop": h.svc.Control().EmergencyState(),
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{"emergencyStop": state})
}

type prepareControlApprovalRequest struct {
	Action     string `json:"action"`
	TargetMode string `json:"targetMode,omitempty"`
}

// PrepareControlApproval creates a short-lived, exact-bound request from
// current server state. It does not approve or execute anything.
func (h *Handler) PrepareControlApproval(c *gin.Context) {
	actor, ok := h.actor(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": ErrUnauthenticated.Error()})
		return
	}
	var req prepareControlApprovalRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "control approval request is required"})
		return
	}
	prepared, err := h.svc.PrepareControlApproval(
		c.Request.Context(),
		actor,
		req.Action,
		req.TargetMode,
	)
	if err != nil {
		c.JSON(controlErrorStatus(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, prepared)
}

type decideControlApprovalRequest struct {
	Decision string `json:"decision"`
	Reason   string `json:"reason,omitempty"`
}

// DecideControlApproval appends the owner's decision. An approved response
// contains references for the separate execution call, never a client-minted
// approval assertion.
func (h *Handler) DecideControlApproval(c *gin.Context) {
	actor, ok := h.actor(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": ErrUnauthenticated.Error()})
		return
	}
	requestID, err := uuid.Parse(c.Param("id"))
	if err != nil || requestID == uuid.Nil || c.Param("id") != requestID.String() {
		c.JSON(http.StatusBadRequest, gin.H{"error": "control approval request id is invalid"})
		return
	}
	var req decideControlApprovalRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "control approval decision is required"})
		return
	}
	decision, err := h.svc.DecideControlApproval(
		c.Request.Context(),
		actor,
		requestID,
		req.Decision,
		req.Reason,
	)
	if err != nil {
		c.JSON(controlErrorStatus(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, decision)
}

type modeRequest struct {
	Mode string `json:"mode"`
	controlAuthorizationRequest
}

// SetMode updates the background autonomy mode.
func (h *Handler) SetMode(c *gin.Context) {
	var req modeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	mode, err := h.svc.SetMode(
		c.Request.Context(),
		req.Mode,
		h.controlAuthorization(c, req.controlAuthorizationRequest),
	)
	if err != nil {
		c.JSON(controlErrorStatus(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"mode": mode})
}

func controlErrorStatus(err error) int {
	switch {
	case errors.Is(err, ErrUnauthenticated):
		return http.StatusUnauthorized
	case errors.Is(err, ErrAuthorizationDenied),
		errors.Is(err, ErrAuthorizationMismatch):
		return http.StatusForbidden
	case errors.Is(err, ErrAuthorizationUnavailable):
		return http.StatusServiceUnavailable
	case errors.Is(err, ErrControlApprovalUnavailable):
		return http.StatusServiceUnavailable
	case errors.Is(err, ErrControlApprovalNotFound):
		return http.StatusNotFound
	case errors.Is(err, ErrControlPersistence):
		return http.StatusServiceUnavailable
	case errors.Is(err, ErrEmergencyStopStateChanged),
		errors.Is(err, ErrAutonomyModeStateChanged),
		errors.Is(err, ErrControlApprovalExpired),
		errors.Is(err, ErrControlApprovalDecided),
		errors.Is(err, ErrControlApprovalStale),
		errors.Is(err, ErrControlChangeNotRequired),
		errors.Is(err, ErrControlApprovalNotRequired):
		return http.StatusConflict
	default:
		return http.StatusBadRequest
	}
}

// Readiness returns the Windows-runtime readiness checklist.
func (h *Handler) Readiness(c *gin.Context) {
	c.JSON(http.StatusOK, h.svc.Readiness(c.Request.Context()))
}

// Recovery runs a crash/reboot recovery pass.
func (h *Handler) Recovery(c *gin.Context) {
	c.JSON(http.StatusOK, h.svc.Recover(c.Request.Context()))
}

// VerifyEmergencyStop proves the emergency stop halts background processing.
func (h *Handler) VerifyEmergencyStop(c *gin.Context) {
	v, err := h.svc.VerifyEmergencyStop(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
		return
	}
	code := http.StatusOK
	if !v.Halted {
		code = http.StatusInternalServerError // a non-halting emergency stop is a hard failure
	}
	c.JSON(code, v)
}
