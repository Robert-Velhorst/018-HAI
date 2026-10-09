package phase2

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"automation-hub-backend/internal/apierror"
	"automation-hub-backend/internal/background"
	"automation-hub-backend/internal/executionbroker"
	"automation-hub-backend/internal/identity"
	"automation-hub-backend/internal/modelintelligence"
	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/operations"
	"automation-hub-backend/internal/privacyfilter"
	"automation-hub-backend/internal/rbac"
	"automation-hub-backend/internal/safety"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// Handler serves the Phase 2 Background Operations API.
type Handler struct {
	m *Module
}

// OperationsOverview keeps the operations page's initial ledger reads in one
// owner-scoped response. Feed state remains a separate connector-registry read
// because it has different lifecycle and authorization ownership.
type OperationsOverview struct {
	Dashboard  operations.Dashboard `json:"dashboard"`
	Operations []models.Operation   `json:"operations"`
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
	f := operationFilter(c, owner, workspace)
	ops, err := h.m.svc.List(f)
	if err != nil {
		writeServerError(c, err, "background operations are unavailable")
		return
	}
	c.JSON(http.StatusOK, gin.H{"operations": ops})
}

// Overview returns the operation-ledger roll-up and the current filtered list
// together, avoiding a duplicate owner-scoped request during page startup.
func (h *Handler) Overview(c *gin.Context) {
	owner, workspace := h.owner(c)
	dashboard, err := h.m.svc.Dashboard(owner, workspace)
	if err != nil {
		writeServerError(c, err, "background operations overview is unavailable")
		return
	}
	operationsList, err := h.m.svc.List(operationFilter(c, owner, workspace))
	if err != nil {
		writeServerError(c, err, "background operations are unavailable")
		return
	}
	c.JSON(http.StatusOK, OperationsOverview{Dashboard: dashboard, Operations: operationsList})
}

func operationFilter(c *gin.Context, owner, workspace string) operations.Filter {
	f := operations.Filter{OwnerUserID: owner, WorkspaceID: workspace}
	if status := c.Query("status"); status != "" {
		f.Status = operations.OperationStatus(status)
	}
	if risk := c.Query("risk"); risk != "" {
		f.RiskLevel = operations.RiskLevel(risk)
	}
	if limit := c.Query("limit"); limit != "" {
		if parsed, err := strconv.Atoi(limit); err == nil {
			f.Limit = parsed
		}
	}
	return f
}

// Dashboard returns the Background Operations roll-up.
func (h *Handler) Dashboard(c *gin.Context) {
	owner, workspace := h.owner(c)
	d, err := h.m.svc.Dashboard(owner, workspace)
	if err != nil {
		writeServerError(c, err, "background operations dashboard is unavailable")
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
		writeOperationLookupError(c, err)
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
		writeOperationLookupError(c, err)
		return
	}
	events, err := h.m.svc.Events(id)
	if err != nil {
		writeServerError(c, err, "operation audit history is unavailable")
		return
	}
	c.JSON(http.StatusOK, gin.H{"events": events})
}

// Approve records a human approval, moving an awaiting-approval operation to
// approved. It does not fake execution: Phase 2A has no real runtime for
// high-risk/external work, so an approved operation waits for a future runtime.
func (h *Handler) Approve(c *gin.Context) {
	owner, ok := authenticatedOwner(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "authenticated owner identity required"})
		return
	}
	workspace := h.m.cfg.WorkspaceID
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	op, configuredWorkerScope, ok := h.loadApprovalOperation(c, owner, workspace, id)
	if !ok {
		return
	}
	var request struct {
		ExpectedVersion int64  `json:"expectedVersion"`
		RevisionDigest  string `json:"revisionDigest"`
	}
	if c.Request.ContentLength != 0 {
		if err := c.ShouldBindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "approval revision binding is invalid"})
			return
		}
	}
	if operations.IsSourceDerived(*op) {
		if request.ExpectedVersion <= 0 || strings.TrimSpace(request.RevisionDigest) == "" {
			c.JSON(http.StatusPreconditionRequired, gin.H{"error": "review the current operation revision before approving it"})
			return
		}
		var approved *models.Operation
		var receipt operations.SourceApprovalReceipt
		if configuredWorkerScope {
			approved, receipt, err = h.m.svc.ApproveSourceDerivedAsOwnerContext(c.Request.Context(), *op, owner, request.ExpectedVersion, request.RevisionDigest)
		} else {
			approved, receipt, err = h.m.svc.ApproveSourceDerivedContext(c.Request.Context(), *op, owner, request.ExpectedVersion, request.RevisionDigest)
		}
		if err != nil {
			status := http.StatusConflict
			if errors.Is(err, operations.ErrSourceApprovalRequired) {
				status = http.StatusForbidden
			} else if errors.Is(err, operations.ErrSourceApprovalBinding) {
				status = http.StatusPreconditionRequired
			}
			c.JSON(status, gin.H{"error": apierror.PublicMessage(err, "source-derived operation approval was not recorded")})
			return
		}
		response, err := json.Marshal(approved)
		if err != nil {
			writeServerError(c, err, "approval receipt was recorded but its response could not be encoded")
			return
		}
		var body map[string]json.RawMessage
		if err := json.Unmarshal(response, &body); err != nil {
			writeServerError(c, err, "approval receipt was recorded but its response could not be encoded")
			return
		}
		body["approvalReceipt"], err = json.Marshal(receipt)
		if err != nil {
			writeServerError(c, err, "approval receipt was recorded but its response could not be encoded")
			return
		}
		c.JSON(http.StatusOK, body)
		return
	}
	op.OwnerType = string(operations.OwnerRobert)
	approved, err := h.m.svc.Transition(*op, operations.StatusApproved, string(operations.OwnerRobert), owner, "approved by operator")
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": apierror.PublicMessage(err, "safe execution could not be completed")})
		return
	}
	c.JSON(http.StatusOK, approved)
}

// ApprovalPreview returns the revision binding an owner should inspect before
// approving source-derived work. Non-source operations retain their existing
// approval flow and do not need a source receipt.
func (h *Handler) ApprovalPreview(c *gin.Context) {
	owner, ok := authenticatedOwner(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "authenticated owner identity required"})
		return
	}
	workspace := h.m.cfg.WorkspaceID
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	op, _, ok := h.loadApprovalOperation(c, owner, workspace, id)
	if !ok {
		return
	}
	preview, err := h.m.svc.PreviewSourceApprovalContext(c.Request.Context(), *op)
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": apierror.PublicMessage(err, "source operation is not awaiting review")})
		return
	}
	c.JSON(http.StatusOK, gin.H{"operation": op, "revision": preview})
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
		writeServerError(c, err, "operation could not be postponed")
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
		writeServerError(c, err, "operation approval history is unavailable")
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
		writeServerError(c, err, "operation audit history is unavailable")
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
		writeOperationLookupError(c, err)
		return nil, false
	}
	return op, true
}

// loadApprovalOperation permits installation-owner access to the configured
// background worker scope when its stable ID differs from the verified JWT
// principal. Other roles remain restricted to their own operation scope.
func (h *Handler) loadApprovalOperation(c *gin.Context, principal, workspace string, id uuid.UUID) (*models.Operation, bool, bool) {
	op, err := h.m.svc.Get(principal, workspace, id)
	if err == nil {
		return op, false, true
	}
	if !errors.Is(err, operations.ErrNotFound) {
		writeOperationLookupError(c, err)
		return nil, false, false
	}
	roleValue, _ := c.Get(identity.ContextRoleKey)
	role, _ := roleValue.(string)
	configuredOwner := strings.TrimSpace(h.m.cfg.OwnerUserID)
	if rbac.Role(strings.ToLower(strings.TrimSpace(role))) != rbac.RoleOwner || configuredOwner == "" || configuredOwner == principal {
		writeOperationLookupError(c, err)
		return nil, false, false
	}
	op, err = h.m.svc.Get(configuredOwner, workspace, id)
	if err != nil {
		writeOperationLookupError(c, err)
		return nil, false, false
	}
	return op, true, true
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
	claimed, err := h.m.svc.ClaimOperation(
		c.Request.Context(), owner, workspace, id, uuid.New(), 5*time.Minute,
	)
	if err != nil {
		switch {
		case errors.Is(err, operations.ErrNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": "operation not found"})
		case errors.Is(err, operations.ErrOperationClaimed):
			c.JSON(http.StatusConflict, gin.H{"error": "operation is already being executed"})
		case errors.Is(err, operations.ErrOperationNotClaimable):
			c.JSON(http.StatusConflict, gin.H{"error": "operation is not eligible for safe execution"})
		default:
			writeServerError(c, err, "safe execution claim could not be acquired")
		}
		return
	}
	if !h.m.SafeOperationExecutionAllowed(claimed.Operation) {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(c.Request.Context()), 5*time.Second)
		releaseErr := h.m.svc.ReleaseClaim(cleanupCtx, claimed.Claim)
		cancel()
		if releaseErr != nil && !errors.Is(releaseErr, operations.ErrClaimLost) {
			writeServerError(c, releaseErr, "safe execution claim could not be released")
			return
		}
		c.JSON(http.StatusConflict, gin.H{"error": "safe execution is blocked by the current runtime policy"})
		return
	}
	outcome, err := background.ExecuteSafeOperationClaimed(
		c.Request.Context(), h.m.svc, h.m.broker, claimed.Operation, claimed.Claim, time.Now().UTC(),
		func(op models.Operation) bool {
			return h.m.SafeOperationEffectPolicyAllows(op)
		},
	)
	if err != nil {
		// Once an execution receipt exists, leave nonterminal claims fenced for
		// expiry recovery. A recorded interruption already releases its claim.
		if outcome.Receipt == nil && !outcome.Verified && !outcome.Interrupted && !errors.Is(err, background.ErrSafeOutcomeUncertain) {
			cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(c.Request.Context()), 5*time.Second)
			releaseErr := h.m.svc.ReleaseClaim(cleanupCtx, claimed.Claim)
			cancel()
			if releaseErr != nil && !errors.Is(releaseErr, operations.ErrClaimLost) {
				err = errors.Join(err, releaseErr)
			}
		}
		c.JSON(http.StatusConflict, safeOperationRunResponse(outcome, err))
		return
	}
	c.JSON(http.StatusOK, safeOperationRunResponse(outcome, nil))
}

func safeOperationRunResponse(outcome background.SafeOutcome, err error) gin.H {
	verified := err == nil && outcome.Verified && !outcome.Interrupted && !outcome.Failed &&
		outcome.Operation != nil && outcome.Operation.Status == string(operations.StatusCompleted) &&
		outcome.Operation.VerificationStatus == string(operations.VerificationPassed)
	reconciliation := outcome.Interrupted || errors.Is(err, background.ErrSafeOutcomeUncertain) ||
		((outcome.Receipt != nil || outcome.Verified) && err != nil) || (err == nil && !verified && !outcome.Failed)
	response := gin.H{
		"operation": publicSafeOperationSnapshot(outcome.Operation),
		"verified":  verified, "failed": outcome.Failed && !reconciliation,
		"interrupted": outcome.Interrupted, "reconciliationRequired": reconciliation,
		// These are repository transition flags, not proof of durable storage.
		"outcomeRecorded":   outcome.Interrupted || outcome.Failed || verified,
		"operationSnapshot": outcome.Operation != nil,
	}
	if outcome.Operation != nil {
		response["operationId"] = outcome.Operation.ID.String()
	}
	if outcome.Receipt != nil {
		response["receipt"] = executionbroker.PublicExecutionResult(*outcome.Receipt)
	}
	if err != nil {
		response["error"] = "safe execution could not be completed"
		if reconciliation {
			response["error"] = "safe execution requires reconciliation; retained receipt does not confirm completion"
		}
	}
	return response
}

func publicSafeOperationSnapshot(operation *models.Operation) json.RawMessage {
	if operation == nil {
		return nil
	}
	snapshot := *operation
	// Do not echo raw execution/storage errors through the operation snapshot.
	if snapshot.LastError != "" {
		snapshot.LastError = "safe worker execution did not complete; review the operation"
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return nil
	}
	redacted := safety.RedactSecrets(string(raw))
	if !json.Valid([]byte(redacted)) {
		// Oversize redaction may intentionally discard the full payload. Keep
		// the real correlation/state fields without exposing that payload.
		minimal, _ := json.Marshal(gin.H{
			"id": snapshot.ID, "status": safety.RedactSecrets(snapshot.Status),
			"verificationStatus": safety.RedactSecrets(snapshot.VerificationStatus),
		})
		return minimal
	}
	return json.RawMessage(redacted)
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
		status := backgroundRunHTTPStatus(err)
		if status >= http.StatusInternalServerError {
			writeServerError(c, err, "background pass could not be completed")
			return
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, rep)
}

func writeServerError(c *gin.Context, err error, fallback string) {
	c.JSON(http.StatusInternalServerError, gin.H{"error": apierror.PublicMessage(err, fallback)})
}

func writeOperationLookupError(c *gin.Context, err error) {
	if errors.Is(err, operations.ErrNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "operation not found"})
		return
	}
	writeServerError(c, err, "operation details are unavailable")
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
