package task

import (
	"automation-hub-backend/internal/identity"
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type Handler struct {
	service Service
}

const (
	defaultTaskLogLimit = 10
	maximumTaskLogLimit = 50
)

func NewHandler(service Service) *Handler {
	return &Handler{service: service}
}

func DefaultHandler() (*Handler, error) {
	service, err := DefaultService()
	if err != nil {
		return nil, err
	}
	return NewHandler(service), nil
}

func (h *Handler) Plan(c *gin.Context) {
	var request IntakeRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if request.Request == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "request is required"})
		return
	}
	ownerIdentity, ok := requireTaskOwner(c)
	if !ok {
		return
	}
	request.ExecuteAllowed = false
	request.OwnerIdentity = ownerIdentity
	if !bindTaskIdempotencyKey(c, &request) {
		return
	}
	c.Header("Idempotency-Key", request.IdempotencyKey)
	request.HumanApproved = false
	request.ApprovalNote = ""
	var plan *CompletionPlan
	var err error
	if contextual, ok := h.service.(PlanningContextService); ok {
		plan, err = contextual.PlanContext(c.Request.Context(), request)
	} else {
		plan, err = h.service.Plan(request)
	}
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return
		}
		writeTaskOperationError(c, err, "task plan could not be created")
		return
	}
	c.JSON(http.StatusOK, plan)
}

func (h *Handler) Run(c *gin.Context) {
	var request IntakeRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if request.Request == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "request is required"})
		return
	}
	ownerIdentity, ok := requireTaskOwner(c)
	if !ok {
		return
	}
	request.ExecuteAllowed = true
	request.OwnerIdentity = ownerIdentity
	if !bindTaskIdempotencyKey(c, &request) {
		return
	}
	c.Header("Idempotency-Key", request.IdempotencyKey)
	request.HumanApproved = false
	request.ApprovalNote = ""
	var plan *CompletionPlan
	var err error
	if contextService, ok := h.service.(ContextService); ok {
		plan, err = contextService.RunContext(c.Request.Context(), request)
	} else {
		plan, err = h.service.Run(request)
	}
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return
		}
		writeTaskOperationError(c, err, "task run could not be completed")
		return
	}
	c.JSON(http.StatusOK, plan)
}

func bindTaskIdempotencyKey(c *gin.Context, request *IntakeRequest) bool {
	headerKey := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	bodyKey := strings.TrimSpace(request.IdempotencyKey)
	if headerKey != "" && bodyKey != "" && headerKey != bodyKey {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Idempotency-Key header does not match request idempotencyKey"})
		return false
	}
	request.IdempotencyKey = headerKey
	if request.IdempotencyKey == "" {
		request.IdempotencyKey = bodyKey
	}
	if request.IdempotencyKey == "" {
		request.IdempotencyKey = uuid.NewString()
	}
	if !validTaskOperationIdentifier(request.IdempotencyKey, 120) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "idempotency key must contain 1 to 120 safe identifier characters"})
		return false
	}
	return true
}

func writeTaskOperationError(c *gin.Context, err error, fallback string) {
	switch {
	case errors.Is(err, ErrInvalidStandingMandateID):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	case errors.Is(err, ErrTaskStateConflict):
		c.JSON(http.StatusConflict, gin.H{"error": "idempotency key was already used for different task input"})
	case errors.Is(err, ErrTaskOperationInProgress):
		c.Header("Retry-After", "5")
		c.JSON(http.StatusConflict, gin.H{"error": "task operation is already in progress"})
	case errors.Is(err, ErrTaskOperationNeedsReview):
		c.JSON(http.StatusConflict, gin.H{"error": "task operation outcome requires review before retry"})
	case errors.Is(err, ErrTaskOperationCanceled):
		c.JSON(http.StatusConflict, gin.H{"error": "task operation was canceled before execution; retry with a new idempotency key"})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": fallback})
	}
}

func verifiedTaskOwner(c *gin.Context) string {
	if value, ok := c.Get(identity.ContextSubjectKey); ok {
		if subject, ok := value.(string); ok {
			return strings.TrimSpace(subject)
		}
	}
	return ""
}

// requireTaskOwner keeps HTTP operator requests separate from in-process
// system workers. Task plans, review queues, and resolution decisions can
// contain source-derived private context, so they must never fall back to an
// ownerless/global view when the identity boundary is unavailable.
func requireTaskOwner(c *gin.Context) (string, bool) {
	ownerIdentity := verifiedTaskOwner(c)
	if ownerIdentity == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "an authenticated owner session is required for task operations"})
		return "", false
	}
	return ownerIdentity, true
}

func (h *Handler) Logs(c *gin.Context) {
	ownerIdentity, ok := requireTaskOwner(c)
	if !ok {
		return
	}
	limit, ok := taskLogLimit(c)
	if !ok {
		return
	}
	if compact, ok := h.service.(CompactDurableOwnerScopedService); ok {
		logs, err := compact.HistoryForOwnerWithLimit(ownerIdentity, limit)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "task history is temporarily unavailable"})
			return
		}
		c.JSON(http.StatusOK, logs)
		return
	}
	if bounded, ok := h.service.(BoundedDurableOwnerScopedService); ok {
		logs, err := bounded.LogsForOwnerWithLimit(ownerIdentity, limit)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "task history is temporarily unavailable"})
			return
		}
		c.JSON(http.StatusOK, completionPlanHistory(logs))
		return
	}
	if durable, ok := h.service.(DurableOwnerScopedService); ok {
		logs, err := durable.LogsForOwnerWithError(ownerIdentity)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "task history is temporarily unavailable"})
			return
		}
		c.JSON(http.StatusOK, completionPlanHistory(recentCompletionPlans(logs, limit)))
		return
	}
	scoped, ok := h.service.(OwnerScopedService)
	if !ok {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "owner-scoped task history is unavailable"})
		return
	}
	c.JSON(http.StatusOK, completionPlanHistory(recentCompletionPlans(scoped.LogsForOwner(ownerIdentity), limit)))
}

func taskLogLimit(c *gin.Context) (int, bool) {
	values, exists := c.Request.URL.Query()["limit"]
	if !exists {
		return defaultTaskLogLimit, true
	}
	if len(values) != 1 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "task history limit must be provided exactly once"})
		return 0, false
	}
	limit, err := strconv.Atoi(strings.TrimSpace(values[0]))
	if err != nil || limit < 1 || limit > maximumTaskLogLimit {
		c.JSON(http.StatusBadRequest, gin.H{"error": "task history limit must be between 1 and 50"})
		return 0, false
	}
	return limit, true
}

func recentCompletionPlans(logs []CompletionPlan, limit int) []CompletionPlan {
	if len(logs) <= limit {
		return logs
	}
	return logs[:limit]
}

func completionPlanHistory(logs []CompletionPlan) []CompletionPlanHistoryItem {
	items := make([]CompletionPlanHistoryItem, 0, len(logs))
	for _, plan := range logs {
		items = append(items, CompletionPlanHistoryItem{
			ID:         plan.ID,
			CreatedAt:  plan.CreatedAt,
			Request:    plan.Request,
			ProjectKey: plan.ProjectKey,
			Intake: CompletionPlanHistoryIntake{
				TaskType:        plan.Intake.TaskType,
				SuccessCriteria: append([]string(nil), plan.Intake.SuccessCriteria...),
			},
			CompletionStatus: plan.CompletionStatus,
		})
	}
	return items
}

func (h *Handler) ReviewQueue(c *gin.Context) {
	ownerIdentity, ok := requireTaskOwner(c)
	if !ok {
		return
	}
	if durable, ok := h.service.(DurableOwnerScopedService); ok {
		items, err := durable.ReviewQueueForOwnerWithError(ownerIdentity)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "task review queue is temporarily unavailable"})
			return
		}
		c.JSON(http.StatusOK, items)
		return
	}
	scoped, ok := h.service.(OwnerScopedService)
	if !ok {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "owner-scoped task review is unavailable"})
		return
	}
	c.JSON(http.StatusOK, scoped.ReviewQueueForOwner(ownerIdentity))
}

func (h *Handler) ResolveReviewItem(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "review item id is required"})
		return
	}
	var decision ApprovalDecision
	if err := c.ShouldBindJSON(&decision); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ownerIdentity, ok := requireTaskOwner(c)
	if !ok {
		return
	}
	scoped, ok := h.service.(OwnerScopedService)
	if !ok {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "owner-scoped task review is unavailable"})
		return
	}
	result, err := scoped.ResolveReviewItemForOwner(ownerIdentity, id, decision)
	if err != nil {
		switch {
		case errors.Is(err, ErrTaskStateNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": "review item not found"})
		case errors.Is(err, ErrTaskOperationRetryConfirmation):
			c.JSON(http.StatusBadRequest, gin.H{
				"error":        "uncertain operation retry requires explicit confirmation",
				"confirmation": TaskOperationRetryConfirmation,
			})
		case errors.Is(err, ErrTaskReviewAlreadyResolved),
			errors.Is(err, ErrTaskStateConflict),
			errors.Is(err, ErrTaskReviewInvalidTransition):
			c.JSON(http.StatusConflict, gin.H{"error": "review item can no longer be resolved from its current state"})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "review decision could not be completed"})
		}
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *Handler) ReconcileApprovedReviews(c *gin.Context) {
	ownerIdentity, ok := requireTaskOwner(c)
	if !ok {
		return
	}
	var request ApprovedReviewReconciliationRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	reconciler, ok := h.service.(ReviewReconciliationService)
	if !ok {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "task review reconciliation is unavailable"})
		return
	}
	result, err := reconciler.ReconcileApprovedReviewsForOwner(ownerIdentity, request)
	if err != nil {
		if strings.Contains(err.Error(), "confirmation") || strings.Contains(err.Error(), "olderThanMinutes") {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "task review reconciliation could not be completed"})
		return
	}
	c.JSON(http.StatusOK, result)
}
