package workflow

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"automation-hub-backend/internal/identity"
	"automation-hub-backend/internal/safety"

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
	workflowAuditPersistenceFailedMessage         = "workflow audit history could not be saved; inspect the workflow before retrying"
	workflowPersistenceFailedMessage              = "workflow state could not be saved safely; inspect the workflow before retrying"
)

func respondWorkflowMutationError(c *gin.Context, err error) {
	var auditErr *WorkflowAuditPersistenceError
	if errors.As(err, &auditErr) {
		c.JSON(http.StatusInternalServerError, gin.H{"error": workflowAuditPersistenceFailedMessage})
		return
	}
	var persistenceErr *WorkflowPersistenceError
	if errors.As(err, &persistenceErr) {
		c.JSON(http.StatusInternalServerError, gin.H{"error": workflowPersistenceFailedMessage})
		return
	}
	if errors.Is(err, ErrWorkflowIntakeIncomplete) {
		c.JSON(http.StatusConflict, gin.H{"error": ErrWorkflowIntakeIncomplete.Error()})
		return
	}
	if errors.Is(err, ErrWorkflowIntakeConcurrentChange) {
		c.JSON(http.StatusConflict, gin.H{"error": ErrWorkflowIntakeConcurrentChange.Error()})
		return
	}
	c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
}

type Handler struct {
	service             Service
	pursuitIntakeRouter PursuitIntakeRouter
	projectDossier      ProjectDossierService
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

func NewHandlerWithPursuitIntakeRouterAndProjectDossierService(service Service, pursuitIntakeRouter PursuitIntakeRouter, dossierService ProjectDossierService) *Handler {
	return &Handler{service: service, pursuitIntakeRouter: pursuitIntakeRouter, projectDossier: dossierService}
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
		respondWorkflowMutationError(c, err)
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
	owner := verifiedWorkflowOwner(c)
	if owner == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "an authenticated owner session is required for workflow access"})
		return
	}
	var request *RunDueRequest
	decoder := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 4096))
	if err := decoder.Decode(&request); err != nil || request == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "a valid reminder delivery request is required"})
		return
	}
	// Reject additional payloads and oversized whitespace before admitting work.
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF || request.Limit < 0 || request.Limit > 100 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "a valid reminder delivery request is required"})
		return
	}
	if h == nil || h.service == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": workflowReminderDeliveriesUnavailableMessage})
		return
	}
	service, ok := h.service.(ContextualReminderDeliveryService)
	if !ok {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": workflowReminderDeliveriesUnavailableMessage})
		return
	}
	result, err := service.RunDueReminderDeliveriesForOwnerContext(c.Request.Context(), owner, *request)
	if err != nil || result == nil {
		status := http.StatusInternalServerError
		if errors.Is(err, ErrReminderDeliveryContextUnavailable) {
			status = http.StatusServiceUnavailable
		}
		c.JSON(status, gin.H{"error": workflowReminderDeliveriesUnavailableMessage})
		return
	}
	public := *result
	public.Results = slices.Clone(result.Results)
	for i := range public.Results {
		public.Results[i].Reason = safety.RedactSecrets(public.Results[i].Reason)
	}
	c.JSON(http.StatusOK, public)
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
		respondWorkflowMutationError(c, err)
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
		respondWorkflowMutationError(c, err)
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
		respondWorkflowMutationError(c, err)
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
		respondWorkflowMutationError(c, err)
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
	if err != nil || record == nil || record.Item.ID != id || strings.TrimSpace(record.Item.OwnerIdentity) != verifiedWorkflowOwner(c) {
		c.JSON(http.StatusNotFound, gin.H{"error": "workflow not found"})
		return false
	}
	return true
}

func (h *Handler) respondScopedWorkflow(c *gin.Context, id uuid.UUID, status int) {
	record, err := h.service.GetForOwner(verifiedWorkflowOwner(c), id)
	if err != nil || record == nil || record.Item.ID != id || strings.TrimSpace(record.Item.OwnerIdentity) != verifiedWorkflowOwner(c) {
		c.JSON(http.StatusNotFound, gin.H{"error": "workflow not found"})
		return
	}
	c.JSON(status, record)
}

func (h *Handler) RunDue(c *gin.Context) {
	owner := verifiedWorkflowOwner(c)
	if owner == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "an authenticated owner session is required for workflow access"})
		return
	}
	var request *RunDueRequest
	decoder := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 4096))
	if err := decoder.Decode(&request); err != nil || request == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "a valid workflow execution request with limit 0 to 50 is required"})
		return
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF || request.Limit < 0 || request.Limit > 50 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "a valid workflow execution request with limit 0 to 50 is required"})
		return
	}
	executor, ok := h.service.(ContextualWorkflowExecutionService)
	if !ok {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "cancellation-aware workflow execution is unavailable"})
		return
	}
	result, err := executor.RunDueForOwnerContext(c.Request.Context(), owner, *request)
	if err != nil || result == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": workflowRunFailedMessage})
		return
	}
	public := *result
	public.Results = slices.Clone(result.Results)
	for i := range public.Results {
		public.Results[i].Message = safety.RedactSecrets(public.Results[i].Message)
	}
	c.JSON(http.StatusOK, public)
}

func (h *Handler) RunOne(c *gin.Context) {
	owner := verifiedWorkflowOwner(c)
	if owner == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "an authenticated owner session is required for workflow access"})
		return
	}
	id, ok := parseWorkflowID(c)
	if !ok {
		return
	}
	if !h.ensureWorkflowMutable(c, id) {
		return
	}
	executor, ok := h.service.(ContextualWorkflowExecutionService)
	if !ok {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "cancellation-aware workflow execution is unavailable"})
		return
	}
	result, err := executor.RunOneForOwnerContext(c.Request.Context(), owner, id)
	if err != nil || result == nil || result.WorkflowID != id {
		c.JSON(http.StatusInternalServerError, gin.H{"error": workflowRunFailedMessage})
		return
	}
	public := *result
	public.Message = safety.RedactSecrets(public.Message)
	c.JSON(http.StatusOK, public)
}

func (h *Handler) RecoverStaleClaims(c *gin.Context) {
	owner := verifiedWorkflowOwner(c)
	if owner == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "an authenticated owner session is required for workflow access"})
		return
	}
	var request *RunDueRequest
	decoder := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 4096))
	if err := decoder.Decode(&request); err != nil || request == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "a valid claim recovery request is required"})
		return
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF || request.Limit < 0 || request.Limit > 50 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "a valid claim recovery request is required"})
		return
	}
	if h == nil || h.service == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": workflowRecoveryFailedMessage})
		return
	}
	recovery, ok := h.service.(ContextualClaimRecoveryService)
	if !ok {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": workflowRecoveryFailedMessage})
		return
	}
	result, err := recovery.RecoverStaleClaimsForOwnerContext(c.Request.Context(), owner, *request)
	if err != nil || result == nil {
		if errors.Is(err, ErrClaimRecoveryContextUnavailable) {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": workflowRecoveryFailedMessage})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": workflowRecoveryFailedMessage})
		return
	}
	public := *result
	public.Results = slices.Clone(result.Results)
	for i := range public.Results {
		public.Results[i].Message = safety.RedactSecrets(public.Results[i].Message)
	}
	c.JSON(http.StatusOK, public)
}

func (h *Handler) RunDueOpenLoops(c *gin.Context) {
	owner := verifiedWorkflowOwner(c)
	if owner == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "an authenticated owner session is required for workflow access"})
		return
	}
	var request *RunDueRequest
	// Decode an optional object directly: the installed Gin validator panics
	// when asked to validate a pointer made nil by a JSON null body.
	decoder := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 4096))
	if err := decoder.Decode(&request); err != nil || request == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "a valid follow-up request is required"})
		return
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF || request.Limit < 0 || request.Limit > 50 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "a valid follow-up request with limit 0 to 50 is required"})
		return
	}
	if h == nil || h.service == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": ErrFollowUpContextUnavailable.Error()})
		return
	}
	workflows, ok := h.service.(ContextualFollowUpService)
	if !ok {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": ErrFollowUpContextUnavailable.Error()})
		return
	}
	result, err := workflows.RunDueOpenLoopsForOwnerContext(c.Request.Context(), owner, *request)
	if err != nil || result == nil {
		if errors.Is(err, ErrFollowUpContextUnavailable) {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": ErrFollowUpContextUnavailable.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": workflowOpenLoopRunFailedMessage})
		return
	}
	public := *result
	public.Results = slices.Clone(result.Results)
	for i := range public.Results {
		public.Results[i].Message = safety.RedactSecrets(public.Results[i].Message)
	}
	c.JSON(http.StatusOK, public)
}

func (h *Handler) ProjectDossier(c *gin.Context) {
	ownerIdentity := verifiedWorkflowOwner(c)
	if ownerIdentity == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "an authenticated owner session is required for workflow access"})
		return
	}
	projectKey, ok := c.GetQuery("projectKey")
	if !ok || strings.TrimSpace(projectKey) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": ErrProjectDossierProjectKeyRequired.Error()})
		return
	}
	if h == nil || h.projectDossier == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "project dossier is temporarily unavailable"})
		return
	}
	dossier, err := h.projectDossier.ProjectDossierForOwner(ownerIdentity, projectKey)
	if err != nil {
		switch {
		case errors.Is(err, ErrProjectDossierOwnerRequired):
			c.JSON(http.StatusUnauthorized, gin.H{"error": "an authenticated owner session is required for workflow access"})
		case errors.Is(err, ErrProjectDossierProjectKeyRequired):
			c.JSON(http.StatusBadRequest, gin.H{"error": ErrProjectDossierProjectKeyRequired.Error()})
		default:
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "project dossier is temporarily unavailable"})
		}
		return
	}
	if dossier == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "project dossier is temporarily unavailable"})
		return
	}
	c.JSON(http.StatusOK, dossier)
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
