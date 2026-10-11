package phase2

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"automation-hub-backend/internal/background"
	"automation-hub-backend/internal/modelintelligence"
	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/operations"
	"automation-hub-backend/internal/privacyfilter"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// Handler serves the Phase 2 Background Operations API.
type Handler struct {
	m *Module
}

// NewHandler builds a handler over a module.
func NewHandler(m *Module) *Handler { return &Handler{m: m} }

// owner resolves the caller's operator identity from the verified JWT subject,
// falling back to the module's configured single operator.
func (h *Handler) owner(c *gin.Context) (string, string) {
	if sub, ok := c.Get("subject"); ok {
		if s, ok := sub.(string); ok && s != "" {
			return s, h.m.cfg.WorkspaceID
		}
	}
	return h.m.cfg.OwnerUserID, h.m.cfg.WorkspaceID
}

// ListOperations returns operations for the caller, optionally filtered.
func (h *Handler) ListOperations(c *gin.Context) {
	owner, workspace := h.owner(c)
	f := operations.Filter{OwnerUserID: owner, WorkspaceID: workspace}
	if s := c.Query("status"); s != "" {
		f.Status = operations.OperationStatus(s)
	}
	if r := c.Query("risk"); r != "" {
		f.RiskLevel = operations.RiskLevel(r)
	}
	if l := c.Query("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil {
			f.Limit = n
		}
	}
	ops, err := h.m.svc.List(f)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"operations": ops})
}

// Dashboard returns the Background Operations roll-up.
func (h *Handler) Dashboard(c *gin.Context) {
	owner, workspace := h.owner(c)
	d, err := h.m.svc.Dashboard(owner, workspace)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, d)
}

// GetOperation returns a single operation.
func (h *Handler) GetOperation(c *gin.Context) {
	owner, workspace := h.owner(c)
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	op, err := h.m.svc.Get(owner, workspace, id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, op)
}

// OperationEvents returns an operation's audit trail.
func (h *Handler) OperationEvents(c *gin.Context) {
	owner, workspace := h.owner(c)
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	if _, err := h.m.svc.Get(owner, workspace, id); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	events, err := h.m.svc.Events(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"events": events})
}

// Approve records a human approval, moving an awaiting-approval operation to
// approved. It does not fake execution: Phase 2A has no real runtime for
// high-risk/external work, so an approved operation waits for a future runtime.
func (h *Handler) Approve(c *gin.Context) {
	owner, workspace := h.owner(c)
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	op, err := h.m.svc.Get(owner, workspace, id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	op.OwnerType = string(operations.OwnerRobert)
	approved, err := h.m.svc.Transition(*op, operations.StatusApproved, string(operations.OwnerRobert), owner, "approved by operator")
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, approved)
}

// Reject dismisses an awaiting-approval operation (§10.19).
func (h *Handler) Reject(c *gin.Context) {
	owner, workspace := h.owner(c)
	op, ok := h.loadOp(c, owner, workspace)
	if !ok {
		return
	}
	dismissed, err := h.m.svc.Transition(*op, operations.StatusDismissed, string(operations.OwnerRobert), owner, "rejected by operator")
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, dismissed)
}

// Later postpones an awaiting-approval operation, holding it for a future review
// without changing its status (§10.19).
func (h *Handler) Later(c *gin.Context) {
	owner, workspace := h.owner(c)
	op, ok := h.loadOp(c, owner, workspace)
	if !ok {
		return
	}
	if op.Status != string(operations.StatusAwaitingApproval) {
		c.JSON(http.StatusConflict, gin.H{"error": "operation is not awaiting approval"})
		return
	}
	review := time.Now().UTC().Add(24 * time.Hour)
	op.NextReviewAt = &review
	saved, err := h.m.svc.Save(*op, "postponed", string(operations.OwnerRobert), "postponed by operator; will resurface for review")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, saved)
}

// BlockSimilar blocks this operation and registers a rule so future similar
// operations are auto-blocked (§10.19).
func (h *Handler) BlockSimilar(c *gin.Context) {
	owner, workspace := h.owner(c)
	op, ok := h.loadOp(c, owner, workspace)
	if !ok {
		return
	}
	reason := "operator blocked similar work to: " + op.Title
	h.m.blockRules.Add(BlockRule{
		OperationType: op.OperationType,
		Reason:        reason,
		SourceOpID:    op.ID.String(),
		CreatedAt:     time.Now().UTC(),
	})
	blocked, err := h.m.svc.Transition(*op, operations.StatusBlocked, string(operations.OwnerRobert), owner, reason)
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"operation": blocked, "rule": "future " + op.OperationType + " operations will be auto-blocked"})
}

// Approvals returns the approval-related audit events for an operation (§10.19).
func (h *Handler) Approvals(c *gin.Context) {
	owner, workspace := h.owner(c)
	op, ok := h.loadOp(c, owner, workspace)
	if !ok {
		return
	}
	events, err := h.m.svc.Events(op.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	approvals := make([]models.OperationEvent, 0, len(events))
	for _, e := range events {
		switch {
		case e.AfterStatus == string(operations.StatusAwaitingApproval),
			e.AfterStatus == string(operations.StatusApproved),
			e.AfterStatus == string(operations.StatusDismissed),
			e.AfterStatus == string(operations.StatusBlocked),
			e.EventType == "postponed":
			approvals = append(approvals, e)
		}
	}
	c.JSON(http.StatusOK, gin.H{"operationId": op.ID, "requiresApproval": op.RequiresApproval, "status": op.Status, "approvals": approvals})
}

// GenerateEvidencePack builds + stores an evidence pack for an operation (§10.18).
func (h *Handler) GenerateEvidencePack(c *gin.Context) {
	owner, ok := authenticatedOwner(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "authenticated owner identity required"})
		return
	}
	workspace := h.m.cfg.WorkspaceID
	repository, err := h.m.evidencePackRepository()
	if err != nil {
		writeEvidenceStorageError(c, err)
		return
	}
	op, ok := h.loadOp(c, owner, workspace)
	if !ok {
		return
	}
	events, err := h.m.svc.Events(op.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	scan := privacyfilter.Scan(op.Title+"\n"+op.Description, 280)
	var telemetry []modelintelligence.ModelRunTelemetry
	if h.m.modelInt != nil {
		for _, t := range h.m.modelInt.Telemetry() {
			if t.OperationID == op.ID.String() {
				telemetry = append(telemetry, t)
			}
		}
	}
	pack := buildEvidencePack(*op, events, scan, telemetry, time.Now().UTC())
	stored, err := repository.Create(c.Request.Context(), pack)
	if err != nil {
		writeEvidenceStorageError(c, err)
		return
	}
	c.JSON(http.StatusCreated, stored)
}

// GetEvidencePack returns a stored evidence pack (§10.19).
func (h *Handler) GetEvidencePack(c *gin.Context) {
	owner, ok := authenticatedOwner(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "authenticated owner identity required"})
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid evidence pack id"})
		return
	}
	repository, err := h.m.evidencePackRepository()
	if err != nil {
		writeEvidenceStorageError(c, err)
		return
	}
	pack, err := repository.Get(c.Request.Context(), owner, h.m.cfg.WorkspaceID, id)
	if errors.Is(err, ErrEvidencePackNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "evidence pack not found"})
		return
	}
	if err != nil {
		writeEvidenceStorageError(c, err)
		return
	}
	c.JSON(http.StatusOK, pack)
}

func writeEvidenceStorageError(c *gin.Context, err error) {
	status := http.StatusInternalServerError
	if errors.Is(err, ErrEvidencePackRepositoryUnavailable) {
		status = http.StatusServiceUnavailable
	}
	c.JSON(status, gin.H{"error": "durable evidence pack storage unavailable"})
}

func (h *Handler) loadOp(c *gin.Context, owner, workspace string) (*models.Operation, bool) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return nil, false
	}
	op, err := h.m.svc.Get(owner, workspace, id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return nil, false
	}
	return op, true
}

// RunOperation executes an operation via the local safe worker and verifies it.
// It refuses operations that are not safe-executable (returns 409), so no
// high-risk/external work is ever fake-completed.
func (h *Handler) RunOperation(c *gin.Context) {
	owner, workspace := h.owner(c)
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	op, err := h.m.svc.Get(owner, workspace, id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	outcome, err := background.ExecuteSafeOperation(c.Request.Context(), h.m.svc, h.m.broker, *op, time.Now().UTC())
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"operation": outcome.Operation, "verified": outcome.Verified, "failed": outcome.Failed})
}

// RunBackground triggers a caller-scoped background pass and returns its
// report. Unlike read-only endpoints, this execution path never falls back to
// the module's configured owner.
func (h *Handler) RunBackground(c *gin.Context) {
	owner, ok := authenticatedOwner(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "authenticated owner identity required"})
		return
	}
	rep, err := h.m.RunBackgroundForOwner(c.Request.Context(), owner)
	if err != nil {
		c.JSON(backgroundRunHTTPStatus(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, rep)
}

// backgroundRunHTTPStatus keeps a concurrent run distinguishable from an
// unexpected engine failure. Clients can safely offer a retry only for the
// former instead of presenting every failure as a conflict.
func backgroundRunHTTPStatus(err error) int {
	switch {
	case errors.Is(err, background.ErrBusy):
		return http.StatusConflict
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}

func authenticatedOwner(c *gin.Context) (string, bool) {
	sub, ok := c.Get("subject")
	if !ok {
		return "", false
	}
	owner, ok := sub.(string)
	if !ok {
		return "", false
	}
	owner = strings.TrimSpace(owner)
	return owner, owner != ""
}

// ListFeeds returns the configured account feeds.
func (h *Handler) ListFeeds(c *gin.Context) {
	type feedView struct {
		Name         string `json:"name"`
		Provider     string `json:"provider"`
		AccountLabel string `json:"accountLabel"`
		SourceType   string `json:"sourceType"`
		Enabled      bool   `json:"enabled"`
	}
	views := make([]feedView, 0, len(h.m.readers))
	for _, r := range h.m.readers {
		f := r.Feed()
		views = append(views, feedView{f.Name, f.Provider, f.AccountLabel, string(f.SourceType), f.Enabled})
	}
	c.JSON(http.StatusOK, gin.H{"feeds": views})
}
