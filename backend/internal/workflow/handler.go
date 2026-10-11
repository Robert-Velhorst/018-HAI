package workflow

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"automation-hub-backend/internal/identity"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const (
	workflowItemsUnavailableMessage               = "workflow items are unavailable"
	workflowApprovalsUnavailableMessage           = "workflow approval items are unavailable"
	workflowDashboardUnavailableMessage           = "workflow dashboard is unavailable"
	workflowRunFailedMessage                      = "workflow run failed"
	workflowRecoveryFailedMessage                 = "workflow recovery failed"
	workflowOpenLoopRunFailedMessage              = "workflow follow-up run failed"
	workflowRemindersUnavailableMessage           = "workflow reminder proposals are unavailable"
	workflowReminderActivationsUnavailableMessage = "workflow reminder activation history is unavailable"
	workflowReminderDeliveriesUnavailableMessage  = "workflow reminder deliveries are unavailable"
)

type Handler struct {
	service             Service
	pursuitIntakeRouter PursuitIntakeRouter
}

// PursuitIntakeRouter keeps the legacy workflow endpoint compatible while
// allowing the canonical application to route new work through pursuits.
// Implementations return a workflow record only after the governed pursuit
// intake path has an accepted operational objective. CandidatePending errors
// are rendered as a deferred, reviewable response instead.
type PursuitIntakeRouter interface {
	RouteWorkflowIntake(request IntakeRequest) (*WorkflowRecord, error)
}

type candidatePendingIntakeError interface {
	error
	CandidatePending() bool
	CandidatePursuitID() string
	CandidateIntakeMessage() string
}

func DefaultHandler() *Handler {
	return &Handler{service: DefaultService()}
}

func NewHandler(service Service) *Handler {
	return &Handler{service: service}
}

func NewHandlerWithPursuitIntakeRouter(service Service, pursuitIntakeRouter PursuitIntakeRouter) *Handler {
	return &Handler{service: service, pursuitIntakeRouter: pursuitIntakeRouter}
}

// RequireAuthenticatedOwner protects workflow data and controls at the HTTP
// boundary. System schedulers call the service directly, but a browser or API
// caller must have a verified IDP subject before it can inspect or mutate a
// person's workflow, approvals, evidence, or follow-up state.
func RequireAuthenticatedOwner() gin.HandlerFunc {
	return func(c *gin.Context) {
		if verifiedWorkflowOwner(c) == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "an authenticated owner session is required for workflow access"})
			return
		}
		c.Next()
	}
}

func (h *Handler) Intake(c *gin.Context) {
	var request IntakeRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	request.Actor = verifiedWorkflowActor(c, "operator")
	request.OwnerIdentity = verifiedWorkflowOwner(c)
	request = normalizeWorkflowAPIIntake(request)
	var record *WorkflowRecord
	var err error
	if h.pursuitIntakeRouter != nil {
		record, err = h.pursuitIntakeRouter.RouteWorkflowIntake(request)
	} else {
		record, err = h.service.Intake(request)
	}
	if err != nil {
		if pending, ok := err.(candidatePendingIntakeError); ok && pending.CandidatePending() {
			c.JSON(http.StatusAccepted, gin.H{
				"status":    "pursuit_candidate_pending",
				"pursuitId": pending.CandidatePursuitID(),
				"message":   pending.CandidateIntakeMessage(),
			})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, record)
}

func normalizeWorkflowAPIIntake(request IntakeRequest) IntakeRequest {
	request.Trigger = firstNonEmpty(request.Trigger, "workflow_api_intake")
	if strings.TrimSpace(request.SourceType) == "" {
		request.SourceType = "workflow_api"
	}
	if strings.TrimSpace(request.SourceID) == "" && strings.TrimSpace(request.SourceURI) == "" {
		request.SourceID = workflowAPIIntakeSourceID(request)
		request.SourceURI = "workflow-api://intake/" + request.SourceID
		request.SourceLabel = firstNonEmpty(request.SourceLabel, "Direct workflow API intake")
	}
	return request
}

func workflowAPIIntakeSourceID(request IntakeRequest) string {
	value := strings.Join([]string{
		strings.ToLower(strings.Join(strings.Fields(request.Input), " ")),
		strings.ToLower(strings.TrimSpace(request.ProjectKey)),
		strings.ToLower(strings.TrimSpace(request.AutomationID)),
		request.CoordinationPlan.PlanID.String(),
		fmt.Sprintf("%d", request.CoordinationPlan.Revision),
		strings.ToLower(strings.TrimSpace(request.CoordinationPlan.Digest)),
		strings.TrimSpace(request.CoordinationPlan.NodeID),
	}, "\n")
	sum := sha256.Sum256([]byte(value))
	return fmt.Sprintf("workflow-api-%x", sum[:12])
}

func (h *Handler) Items(c *gin.Context) {
	includeArchived, _ := strconv.ParseBool(c.Query("includeArchived"))
	items, err := h.service.ItemsForOwner(verifiedWorkflowOwner(c), includeArchived)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": workflowItemsUnavailableMessage})
		return
	}
	c.JSON(http.StatusOK, items)
}

func (h *Handler) ApprovalItems(c *gin.Context) {
	items, err := h.service.ApprovalItemsForOwner(verifiedWorkflowOwner(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": workflowApprovalsUnavailableMessage})
		return
	}
	c.JSON(http.StatusOK, items)
}

func (h *Handler) Dashboard(c *gin.Context) {
	dashboard, err := h.service.DashboardForOwner(verifiedWorkflowOwner(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": workflowDashboardUnavailableMessage})
		return
	}
	c.JSON(http.StatusOK, dashboard)
}

func (h *Handler) ReminderProposals(c *gin.Context) {
	reminderService, ok := h.service.(ReminderProposalService)
	if !ok {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": workflowRemindersUnavailableMessage})
		return
	}
	horizonHours, err := strconv.Atoi(firstNonEmpty(c.Query("horizonHours"), "168"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "horizonHours must be an integer between 1 and 720"})
		return
	}
	limit, err := strconv.Atoi(firstNonEmpty(c.Query("limit"), "100"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "limit must be an integer between 1 and 200"})
		return
	}
	result, err := reminderService.ReminderProposalsForOwner(
		verifiedWorkflowOwner(c), time.Now().UTC(), horizonHours, limit,
	)
	if err != nil {
		if horizonHours < 1 || horizonHours > 720 || limit < 1 || limit > 200 {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": workflowRemindersUnavailableMessage})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *Handler) PrepareReminderActivation(c *gin.Context) {
	itemID, err := uuid.Parse(c.Param("itemId"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid reminder checklist item id"})
		return
	}
	var request ReminderActivationPrepareRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	service, ok := h.service.(ReminderActivationService)
	if !ok || service == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": workflowReminderActivationsUnavailableMessage})
		return
	}
	owner := verifiedWorkflowOwner(c)
	result, err := service.PrepareReminderActivationForOwner(owner, verifiedWorkflowActor(c, "operator"), itemID, request)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, result)
}

func (h *Handler) ReminderActivationHistory(c *gin.Context) {
	limit, err := boundedWorkflowQueryInt(c, "limit", 50, 1, 100)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	service, ok := h.service.(ReminderActivationService)
	if !ok || service == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": workflowReminderActivationsUnavailableMessage})
		return
	}
	result, err := service.ReminderActivationHistoryForOwner(verifiedWorkflowOwner(c), limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": workflowReminderActivationsUnavailableMessage})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *Handler) DecideReminderActivation(c *gin.Context) {
	requestID, err := uuid.Parse(c.Param("requestId"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid reminder activation request id"})
		return
	}
	var request ReminderActivationDecisionRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	service, ok := h.service.(ReminderActivationService)
	if !ok || service == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": workflowReminderActivationsUnavailableMessage})
		return
	}
	owner := verifiedWorkflowOwner(c)
	result, err := service.DecideReminderActivationForOwner(owner, verifiedWorkflowActor(c, "operator"), requestID, request)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, result)
}

func (h *Handler) ReminderActivationDecisionHistory(c *gin.Context) {
	requestID, err := uuid.Parse(c.Param("requestId"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid reminder activation request id"})
		return
	}
	limit, err := boundedWorkflowQueryInt(c, "limit", 50, 1, 100)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	service, ok := h.service.(ReminderActivationService)
	if !ok || service == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": workflowReminderActivationsUnavailableMessage})
		return
	}
	result, err := service.ReminderActivationDecisionHistoryForOwner(verifiedWorkflowOwner(c), requestID, limit)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *Handler) AuthorizeReminderDelivery(c *gin.Context) {
	requestID, err := uuid.Parse(c.Param("requestId"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid reminder activation request id"})
		return
	}
	var request ReminderDeliveryAuthorizeRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	service, ok := h.service.(ReminderDeliveryService)
	if !ok || service == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": workflowReminderDeliveriesUnavailableMessage})
		return
	}
	owner := verifiedWorkflowOwner(c)
	result, err := service.AuthorizeReminderDeliveryForOwner(owner, verifiedWorkflowActor(c, "operator"), requestID, request)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, result)
}

func (h *Handler) ReminderDeliveryHistory(c *gin.Context) {
	limit, err := boundedWorkflowQueryInt(c, "limit", 50, 1, 100)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	service, ok := h.service.(ReminderDeliveryService)
	if !ok || service == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": workflowReminderDeliveriesUnavailableMessage})
		return
	}
	result, err := service.ReminderDeliveryHistoryForOwner(verifiedWorkflowOwner(c), limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": workflowReminderDeliveriesUnavailableMessage})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *Handler) RunDueReminderDeliveries(c *gin.Context) {
	var request RunDueRequest
	if c.Request.ContentLength != 0 {
		if err := c.ShouldBindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
	}
	service, ok := h.service.(ReminderDeliveryService)
	if !ok || service == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": workflowReminderDeliveriesUnavailableMessage})
		return
	}
	result, err := service.RunDueReminderDeliveriesForOwner(verifiedWorkflowOwner(c), request)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

func boundedWorkflowQueryInt(c *gin.Context, name string, fallback, minimum, maximum int) (int, error) {
	value, err := strconv.Atoi(firstNonEmpty(c.Query(name), strconv.Itoa(fallback)))
	if err != nil || value < minimum || value > maximum {
		return 0, fmt.Errorf("%s must be an integer between %d and %d", name, minimum, maximum)
	}
	return value, nil
}

func (h *Handler) Get(c *gin.Context) {
	id, ok := parseWorkflowID(c)
	if !ok {
		return
	}
	record, err := h.service.GetForOwner(verifiedWorkflowOwner(c), id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, record)
}

func (h *Handler) Transition(c *gin.Context) {
	id, ok := parseWorkflowID(c)
	if !ok {
		return
	}
	if !h.ensureWorkflowMutable(c, id) {
		return
	}
	var request TransitionRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	// Generic state transitions cannot establish approval provenance.
	// Approval-required workflows must use ResolveApproval.
	request.Approved = false
	request.Actor = verifiedWorkflowActor(c, "operator")
	_, err := h.service.Transition(id, request)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	h.respondScopedWorkflow(c, id, http.StatusOK)
}

func (h *Handler) ResolveApproval(c *gin.Context) {
	id, ok := parseWorkflowID(c)
	if !ok {
		return
	}
	if !h.ensureWorkflowMutable(c, id) {
		return
	}
	var request ApprovalResolutionRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	request.Actor = verifiedWorkflowActor(c, "operator")
	_, err := h.service.ResolveApproval(id, request)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	h.respondScopedWorkflow(c, id, http.StatusOK)
}

func (h *Handler) ResolveInterruptedExecution(c *gin.Context) {
	id, ok := parseWorkflowID(c)
	if !ok {
		return
	}
	if !h.ensureWorkflowMutable(c, id) {
		return
	}
	var request InterruptedExecutionResolutionRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	request.Actor = verifiedWorkflowActor(c, "operator")
	_, err := h.service.ResolveInterruptedExecution(id, request)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	h.respondScopedWorkflow(c, id, http.StatusOK)
}

func (h *Handler) ResolveProposal(c *gin.Context) {
	id, ok := parseWorkflowID(c)
	if !ok {
		return
	}
	if !h.ensureWorkflowMutable(c, id) {
		return
	}
	proposalID, err := uuid.Parse(c.Param("proposalId"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid proposal id"})
		return
	}
	var request ProposalResolutionRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	request.Actor = verifiedWorkflowActor(c, "operator")
	_, err = h.service.ResolveProposal(id, proposalID, request)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	h.respondScopedWorkflow(c, id, http.StatusOK)
}

func (h *Handler) UpdateChecklistItem(c *gin.Context) {
	id, ok := parseWorkflowID(c)
	if !ok {
		return
	}
	if !h.ensureWorkflowMutable(c, id) {
		return
	}
	itemID, err := uuid.Parse(c.Param("itemId"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid checklist item id"})
		return
	}
	var request ChecklistUpdateRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	request.Actor = verifiedWorkflowActor(c, "operator")
	_, err = h.service.UpdateChecklistItem(id, itemID, request)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	h.respondScopedWorkflow(c, id, http.StatusOK)
}

// verifiedWorkflowActor ignores any actor label supplied in request JSON.
// The router writes the verified IDP JWT subject into the Gin context; when
// that identity path is absent in local development, audit records retain the
// explicit generic operator fallback instead of a forged user name.
func verifiedWorkflowActor(c *gin.Context, fallback string) string {
	if value, ok := c.Get(identity.ContextSubjectKey); ok {
		if subject, ok := value.(string); ok && strings.TrimSpace(subject) != "" {
			return strings.TrimSpace(subject)
		}
	}
	return fallback
}

func verifiedWorkflowOwner(c *gin.Context) string {
	if value, ok := c.Get(identity.ContextSubjectKey); ok {
		if subject, ok := value.(string); ok {
			return strings.TrimSpace(subject)
		}
	}
	return ""
}

// Ownerless legacy workflows remain available only to explicit internal
// maintenance calls. Authenticated API callers cannot inspect, adopt, or
// mutate them without a separate audited migration assigning an owner.
func (h *Handler) ensureWorkflowMutable(c *gin.Context, id uuid.UUID) bool {
	record, err := h.service.GetForOwner(verifiedWorkflowOwner(c), id)
	if err != nil || strings.TrimSpace(record.Item.OwnerIdentity) != verifiedWorkflowOwner(c) {
		c.JSON(http.StatusNotFound, gin.H{"error": "workflow not found"})
		return false
	}
	return true
}

func (h *Handler) respondScopedWorkflow(c *gin.Context, id uuid.UUID, status int) {
	record, err := h.service.GetForOwner(verifiedWorkflowOwner(c), id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "workflow not found"})
		return
	}
	c.JSON(status, record)
}

func (h *Handler) RunDue(c *gin.Context) {
	var request RunDueRequest
	if !bindOptionalWorkflowRunRequest(c, &request, "invalid workflow run request") {
		return
	}
	result, err := RunDueForOwnerWithContext(h.service, c.Request.Context(), verifiedWorkflowOwner(c), request)
	if err != nil {
		if requestContextEnded(err) {
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": workflowRunFailedMessage})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *Handler) RunOne(c *gin.Context) {
	id, ok := parseWorkflowID(c)
	if !ok {
		return
	}
	if !h.ensureWorkflowMutable(c, id) {
		return
	}
	result, err := RunOneForOwnerWithContext(h.service, c.Request.Context(), verifiedWorkflowOwner(c), id)
	if err != nil {
		if requestContextEnded(err) {
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": workflowRunFailedMessage})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *Handler) RecoverStaleClaims(c *gin.Context) {
	var request RunDueRequest
	if !bindOptionalWorkflowRunRequest(c, &request, "invalid workflow recovery request") {
		return
	}
	result, err := RecoverStaleClaimsForOwnerWithContext(h.service, c.Request.Context(), verifiedWorkflowOwner(c), request)
	if err != nil {
		if requestContextEnded(err) {
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": workflowRecoveryFailedMessage})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *Handler) RunDueOpenLoops(c *gin.Context) {
	var request RunDueRequest
	if !bindOptionalWorkflowRunRequest(c, &request, "invalid workflow follow-up request") {
		return
	}
	result, err := RunDueOpenLoopsForOwnerWithContext(h.service, c.Request.Context(), verifiedWorkflowOwner(c), request)
	if err != nil {
		if requestContextEnded(err) {
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": workflowOpenLoopRunFailedMessage})
		return
	}
	c.JSON(http.StatusOK, result)
}

func requestContextEnded(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

func bindOptionalWorkflowRunRequest(c *gin.Context, request *RunDueRequest, message string) bool {
	if c.Request.ContentLength == 0 {
		return true
	}
	if err := c.ShouldBindJSON(request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": message})
		return false
	}
	return true
}

func (h *Handler) Overview(c *gin.Context) {
	c.JSON(http.StatusOK, h.service.Overview())
}

func parseWorkflowID(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid workflow id"})
		return uuid.UUID{}, false
	}
	return id, true
}
