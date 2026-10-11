package pursuit

import (
	"automation-hub-backend/internal/executionauth"
	"automation-hub-backend/internal/identity"
	"automation-hub-backend/internal/rbac"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type Handler struct {
	service Service
}

func NewHandler(service Service) *Handler {
	return &Handler{service: service}
}

func DefaultHandler() *Handler {
	return NewHandler(DefaultService())
}

// RequireAuthenticatedOwner protects the personal pursuit API boundary. The
// service still supports ownerless calls for controlled in-process workers,
// but browser and API traffic must carry a verified IDP principal before it
// can read or mutate a person's pursuits.
func RequireAuthenticatedOwner() gin.HandlerFunc {
	return func(c *gin.Context) {
		if pursuitOwner(c) == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "an authenticated owner session is required for pursuit access"})
			return
		}
		c.Next()
	}
}

func (h *Handler) Create(c *gin.Context) {
	var request CreateRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	request.OwnerIdentity = verifiedActor(c, "")
	request.Actor = verifiedActor(c, "operator")
	record, err := h.service.Create(request)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, record)
}

func (h *Handler) List(c *gin.Context) {
	viewMode := strings.ToLower(strings.TrimSpace(c.Query("view")))
	if viewMode != "" && viewMode != "full" && viewMode != "summary" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "pursuit list view must be full or summary"})
		return
	}
	includeArchived, _ := strconv.ParseBool(c.Query("includeArchived"))
	records, err := h.service.ListForOwner(pursuitOwner(c), includeArchived)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if viewMode == "summary" {
		for index := range records {
			records[index] = dashboardPursuitSummary(records[index])
		}
	}
	c.JSON(http.StatusOK, records)
}

func (h *Handler) Dashboard(c *gin.Context) {
	viewMode := strings.TrimSpace(c.Query("view"))
	if viewMode != "" && viewMode != "full" && viewMode != "summary" && viewMode != "counts" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "pursuit dashboard view must be full, summary, or counts"})
		return
	}
	record, err := h.service.DashboardForOwner(pursuitOwner(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if viewMode == "summary" {
		record = dashboardSummary(record)
	} else if viewMode == "counts" {
		record = dashboardCounts(record)
	}
	c.JSON(http.StatusOK, record)
}

func (h *Handler) Brief(c *gin.Context) {
	record, err := h.service.BriefForOwner(pursuitOwner(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, record)
}

func (h *Handler) Decisions(c *gin.Context) {
	decisions, err := h.service.DecisionsForOwner(pursuitOwner(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, decisions)
}

func (h *Handler) PlanPortfolio(c *gin.Context) {
	var request PortfolioPlanningRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	result, err := h.service.PlanPortfolioForOwner(pursuitOwner(c), request)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *Handler) AcceptPortfolioAllocation(c *gin.Context) {
	if !pursuitApprovalAllowed(c) {
		c.JSON(http.StatusForbidden, gin.H{"error": "approval permission is required to accept a portfolio allocation"})
		return
	}
	acceptor, ok := h.service.(interface {
		AcceptPortfolioAllocationForOwner(string, string, PortfolioAllocationAcceptanceRequest) (*PortfolioAllocationAcceptanceResult, error)
	})
	if !ok {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "portfolio allocation acceptance is unavailable"})
		return
	}
	var request PortfolioAllocationAcceptanceRequest
	if !decodePursuitJSON(c, &request, 2*1024*1024, "portfolio allocation") {
		return
	}
	result, err := acceptor.AcceptPortfolioAllocationForOwner(
		pursuitOwner(c), verifiedActor(c, "operator"), request,
	)
	if err != nil {
		message := strings.ToLower(err.Error())
		status := http.StatusBadRequest
		if strings.Contains(message, "changed during acceptance") || strings.Contains(message, "already used") {
			status = http.StatusConflict
		} else if strings.Contains(message, "storage is unavailable") {
			status = http.StatusServiceUnavailable
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	status := http.StatusCreated
	if result.Replayed {
		status = http.StatusOK
	}
	c.JSON(status, result)
}

func (h *Handler) PortfolioAllocationHistory(c *gin.Context) {
	history, ok := h.service.(interface {
		PortfolioAllocationHistoryForOwner(string, int) ([]PortfolioAllocationAcceptanceResult, error)
	})
	if !ok {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "portfolio allocation history is unavailable"})
		return
	}
	limit := 25
	if raw := strings.TrimSpace(c.Query("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 100 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "portfolio allocation history limit must be between 1 and 100"})
			return
		}
		limit = parsed
	}
	result, err := history.PortfolioAllocationHistoryForOwner(pursuitOwner(c), limit)
	if err != nil {
		status := http.StatusInternalServerError
		if strings.Contains(strings.ToLower(err.Error()), "limit") {
			status = http.StatusBadRequest
		} else if strings.Contains(strings.ToLower(err.Error()), "storage is unavailable") {
			status = http.StatusServiceUnavailable
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *Handler) PreparePortfolioExecutionProposals(c *gin.Context) {
	if !pursuitApprovalAllowed(c) {
		c.JSON(http.StatusForbidden, gin.H{"error": "approval permission is required to prepare portfolio execution proposals"})
		return
	}
	allocationID, err := uuid.Parse(strings.TrimSpace(c.Param("allocationId")))
	if err != nil || allocationID == uuid.Nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "a valid portfolio allocation id is required"})
		return
	}
	preparer, ok := h.service.(interface {
		PreparePortfolioExecutionProposalsForOwner(string, string, uuid.UUID, PortfolioExecutionProposalRequest) (*PortfolioExecutionProposalResult, error)
	})
	if !ok {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "portfolio execution proposal preparation is unavailable"})
		return
	}
	var request PortfolioExecutionProposalRequest
	if !decodePursuitJSON(c, &request, 64*1024, "portfolio execution proposal") {
		return
	}
	result, err := preparer.PreparePortfolioExecutionProposalsForOwner(
		pursuitOwner(c), verifiedActor(c, "operator"), allocationID, request,
	)
	if err != nil {
		message := strings.ToLower(err.Error())
		status := http.StatusBadRequest
		if strings.Contains(message, "unavailable to this owner") {
			status = http.StatusNotFound
		} else if strings.Contains(message, "changed") || strings.Contains(message, "different") {
			status = http.StatusConflict
		} else if strings.Contains(message, "storage is unavailable") {
			status = http.StatusServiceUnavailable
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	status := http.StatusCreated
	if result.Replayed {
		status = http.StatusOK
	}
	c.JSON(status, result)
}

func (h *Handler) PortfolioExecutionProposalHistory(c *gin.Context) {
	rawIDs := strings.Split(strings.TrimSpace(c.Query("allocationIds")), ",")
	if len(rawIDs) == 0 || len(rawIDs) > 20 || (len(rawIDs) == 1 && strings.TrimSpace(rawIDs[0]) == "") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "between 1 and 20 portfolio allocation ids are required"})
		return
	}
	allocationIDs := make([]uuid.UUID, 0, len(rawIDs))
	seen := make(map[uuid.UUID]struct{}, len(rawIDs))
	for _, rawID := range rawIDs {
		allocationID, err := uuid.Parse(strings.TrimSpace(rawID))
		if err != nil || allocationID == uuid.Nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "portfolio allocation ids must be valid UUIDs"})
			return
		}
		if _, duplicate := seen[allocationID]; duplicate {
			c.JSON(http.StatusBadRequest, gin.H{"error": "portfolio allocation ids must be unique"})
			return
		}
		seen[allocationID] = struct{}{}
		allocationIDs = append(allocationIDs, allocationID)
	}
	reader, ok := h.service.(interface {
		PortfolioExecutionProposalHistoryForOwner(string, []uuid.UUID) ([]PortfolioExecutionProposalResult, error)
	})
	if !ok {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "portfolio execution proposal history is unavailable"})
		return
	}
	results, err := reader.PortfolioExecutionProposalHistoryForOwner(pursuitOwner(c), allocationIDs)
	if err != nil {
		status := http.StatusBadRequest
		message := strings.ToLower(err.Error())
		if strings.Contains(message, "history is unavailable") || strings.Contains(message, "repository is unavailable") {
			status = http.StatusServiceUnavailable
		} else if strings.Contains(message, "crossed") || strings.Contains(message, "digest") || strings.Contains(message, "invalid") {
			status = http.StatusConflict
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, results)
}

func (h *Handler) PortfolioDispatchCoordination(c *gin.Context) {
	proposalID, err := uuid.Parse(strings.TrimSpace(c.Param("proposalId")))
	if err != nil || proposalID == uuid.Nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "a valid portfolio execution proposal id is required"})
		return
	}
	reader, ok := h.service.(interface {
		PortfolioDispatchCoordinationForOwner(context.Context, string, uuid.UUID) (*PortfolioCoordinationResult, error)
	})
	if !ok {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "portfolio dispatch coordination is unavailable"})
		return
	}
	result, err := reader.PortfolioDispatchCoordinationForOwner(c.Request.Context(), pursuitOwner(c), proposalID)
	if err != nil {
		message := strings.ToLower(err.Error())
		status := http.StatusBadRequest
		if strings.Contains(message, "unavailable to this owner") {
			status = http.StatusNotFound
		} else if strings.Contains(message, "storage is unavailable") {
			status = http.StatusServiceUnavailable
		} else if strings.Contains(message, "changed") || strings.Contains(message, "invalid") {
			status = http.StatusConflict
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *Handler) PortfolioDispatchCoordinationBatch(c *gin.Context) {
	rawIDs := strings.Split(strings.TrimSpace(c.Query("proposalIds")), ",")
	if len(rawIDs) == 0 || len(rawIDs) > PortfolioDispatchMaxItems || (len(rawIDs) == 1 && strings.TrimSpace(rawIDs[0]) == "") {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("portfolio coordination requires between 1 and %d proposal ids", PortfolioDispatchMaxItems)})
		return
	}
	proposalIDs := make([]uuid.UUID, 0, len(rawIDs))
	seen := make(map[uuid.UUID]struct{}, len(rawIDs))
	for _, rawID := range rawIDs {
		proposalID, err := uuid.Parse(strings.TrimSpace(rawID))
		if err != nil || proposalID == uuid.Nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "portfolio coordination proposal ids must be valid UUIDs"})
			return
		}
		if _, duplicate := seen[proposalID]; duplicate {
			c.JSON(http.StatusBadRequest, gin.H{"error": "portfolio coordination proposal ids must be unique"})
			return
		}
		seen[proposalID] = struct{}{}
		proposalIDs = append(proposalIDs, proposalID)
	}
	reader, ok := h.service.(interface {
		PortfolioDispatchCoordinationBatchForOwner(context.Context, string, []uuid.UUID) ([]PortfolioCoordinationResult, error)
	})
	if !ok {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "portfolio dispatch coordination is unavailable"})
		return
	}
	results, err := reader.PortfolioDispatchCoordinationBatchForOwner(c.Request.Context(), pursuitOwner(c), proposalIDs)
	if err != nil {
		message := strings.ToLower(err.Error())
		status := http.StatusBadRequest
		if strings.Contains(message, "unavailable to this owner") {
			status = http.StatusNotFound
		} else if strings.Contains(message, "storage is unavailable") || strings.Contains(message, "repository is unavailable") {
			status = http.StatusServiceUnavailable
		} else if strings.Contains(message, "crossed") || strings.Contains(message, "changed") || strings.Contains(message, "invalid") {
			status = http.StatusConflict
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, results)
}

func (h *Handler) DispatchPortfolioWorkflows(c *gin.Context) {
	if !pursuitApprovalAllowed(c) {
		c.JSON(http.StatusForbidden, gin.H{"error": "owner approval permission is required to dispatch approved portfolio workflows"})
		return
	}
	proposalID, err := uuid.Parse(strings.TrimSpace(c.Param("proposalId")))
	if err != nil || proposalID == uuid.Nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "a valid portfolio execution proposal id is required"})
		return
	}
	dispatcher, ok := h.service.(interface {
		DispatchPortfolioWorkflowsForOwner(context.Context, string, string, uuid.UUID, PortfolioDispatchRequest) (*PortfolioDispatchResult, error)
	})
	if !ok {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "portfolio workflow dispatch is unavailable"})
		return
	}
	var request PortfolioDispatchRequest
	if !decodePursuitJSON(c, &request, 64*1024, "portfolio workflow dispatch") {
		return
	}
	result, err := dispatcher.DispatchPortfolioWorkflowsForOwner(
		c.Request.Context(), pursuitOwner(c), verifiedActor(c, "operator"), proposalID, request,
	)
	if err != nil {
		message := strings.ToLower(err.Error())
		status := http.StatusBadRequest
		switch {
		case strings.Contains(message, "unavailable to this owner"):
			status = http.StatusNotFound
		case strings.Contains(message, "storage is unavailable"),
			strings.Contains(message, "authorization is unavailable"),
			strings.Contains(message, "execution is unavailable"),
			strings.Contains(message, "intake is unavailable"):
			status = http.StatusServiceUnavailable
		case strings.Contains(message, "changed"), strings.Contains(message, "different immutable"):
			status = http.StatusConflict
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	status := http.StatusCreated
	if result.Resumed {
		status = http.StatusOK
	}
	c.JSON(status, result)
}

func (h *Handler) DecidePortfolioExecutionProposalItem(c *gin.Context) {
	if !pursuitApprovalAllowed(c) {
		c.JSON(http.StatusForbidden, gin.H{"error": "approval permission is required to decide portfolio execution proposal items"})
		return
	}
	itemID, err := uuid.Parse(strings.TrimSpace(c.Param("itemId")))
	if err != nil || itemID == uuid.Nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "a valid portfolio execution proposal item id is required"})
		return
	}
	decider, ok := h.service.(interface {
		DecidePortfolioExecutionProposalItemForOwner(string, string, uuid.UUID, PortfolioExecutionProposalDecisionRequest) (*PortfolioExecutionProposalDecisionResult, error)
	})
	if !ok {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "portfolio execution proposal decisions are unavailable"})
		return
	}
	var request PortfolioExecutionProposalDecisionRequest
	if !decodePursuitJSON(c, &request, 64*1024, "portfolio execution proposal decision") {
		return
	}
	result, err := decider.DecidePortfolioExecutionProposalItemForOwner(
		pursuitOwner(c), verifiedActor(c, "operator"), itemID, request,
	)
	if err != nil {
		message := strings.ToLower(err.Error())
		status := http.StatusBadRequest
		if strings.Contains(message, "unavailable to this owner") {
			status = http.StatusNotFound
		} else if strings.Contains(message, "changed") || strings.Contains(message, "chain") {
			status = http.StatusConflict
		} else if strings.Contains(message, "storage is unavailable") {
			status = http.StatusServiceUnavailable
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	status := http.StatusCreated
	if result.Replayed {
		status = http.StatusOK
	}
	c.JSON(status, result)
}

func (h *Handler) AuthorizePortfolioWorkflowEffect(c *gin.Context) {
	if !pursuitApprovalAllowed(c) {
		c.JSON(http.StatusForbidden, gin.H{"error": "owner approval permission is required to authorize a portfolio workflow effect"})
		return
	}
	itemID, err := uuid.Parse(strings.TrimSpace(c.Param("itemId")))
	if err != nil || itemID == uuid.Nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "a valid portfolio execution proposal item id is required"})
		return
	}
	authorizer, ok := h.service.(interface {
		AuthorizePortfolioWorkflowEffectForOwner(context.Context, string, string, uuid.UUID, PortfolioWorkflowEffectAuthorizationRequest) (*PortfolioWorkflowEffectAuthorizationResult, error)
	})
	if !ok {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "portfolio workflow effect authorization is unavailable"})
		return
	}
	var request PortfolioWorkflowEffectAuthorizationRequest
	if !decodePursuitJSON(c, &request, 16*1024, "portfolio workflow effect authorization") {
		return
	}
	result, err := authorizer.AuthorizePortfolioWorkflowEffectForOwner(
		c.Request.Context(), pursuitOwner(c), verifiedActor(c, "operator"), itemID, request,
	)
	if err != nil {
		message := strings.ToLower(err.Error())
		status := http.StatusBadRequest
		switch {
		case errors.Is(err, ErrPortfolioWorkflowApprovalUnavailable), strings.Contains(message, "unavailable to this owner"):
			status = http.StatusNotFound
		case errors.Is(err, ErrPortfolioWorkflowApprovalStale),
			errors.Is(err, ErrPortfolioWorkflowBindingMismatch),
			strings.Contains(message, "changed"):
			status = http.StatusConflict
		case strings.Contains(message, "storage is unavailable"),
			strings.Contains(message, "authorization is unavailable"):
			status = http.StatusServiceUnavailable
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *Handler) ExecutePortfolioWorkflowEffect(c *gin.Context) {
	if !pursuitApprovalAllowed(c) {
		c.JSON(http.StatusForbidden, gin.H{"error": "owner approval permission is required to create an approved portfolio workflow"})
		return
	}
	itemID, err := uuid.Parse(strings.TrimSpace(c.Param("itemId")))
	if err != nil || itemID == uuid.Nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "a valid portfolio execution proposal item id is required"})
		return
	}
	executor, ok := h.service.(interface {
		ExecutePortfolioWorkflowEffectForOwner(context.Context, string, string, uuid.UUID, PortfolioWorkflowEffectExecutionRequest) (*PortfolioWorkflowEffectExecutionResult, error)
	})
	if !ok {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "portfolio workflow execution is unavailable"})
		return
	}
	var request PortfolioWorkflowEffectExecutionRequest
	if !decodePursuitJSON(c, &request, 16*1024, "portfolio workflow effect execution") {
		return
	}
	result, err := executor.ExecutePortfolioWorkflowEffectForOwner(
		c.Request.Context(), pursuitOwner(c), verifiedActor(c, "operator"), itemID, request,
	)
	if err != nil {
		message := strings.ToLower(err.Error())
		status := http.StatusBadRequest
		switch {
		case errors.Is(err, ErrPortfolioWorkflowApprovalUnavailable),
			errors.Is(err, executionauth.ErrNotFound),
			strings.Contains(message, "unavailable to this owner"):
			status = http.StatusNotFound
		case errors.Is(err, ErrPortfolioWorkflowApprovalStale),
			errors.Is(err, ErrPortfolioWorkflowBindingMismatch),
			errors.Is(err, executionauth.ErrAuthorizationChanged),
			errors.Is(err, executionauth.ErrFinalEffectMismatch),
			strings.Contains(message, "changed"),
			strings.Contains(message, "different effect"):
			status = http.StatusConflict
		case strings.Contains(message, "storage is unavailable"),
			strings.Contains(message, "execution is unavailable"),
			strings.Contains(message, "intake is unavailable"):
			status = http.StatusServiceUnavailable
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	status := http.StatusCreated
	if result.Replayed {
		status = http.StatusOK
	}
	c.JSON(status, result)
}

func (h *Handler) SettlePortfolioWorkflow(c *gin.Context) {
	if !pursuitApprovalAllowed(c) {
		c.JSON(http.StatusForbidden, gin.H{"error": "owner approval permission is required to settle verified portfolio work"})
		return
	}
	itemID, err := uuid.Parse(strings.TrimSpace(c.Param("itemId")))
	if err != nil || itemID == uuid.Nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "a valid portfolio execution proposal item id is required"})
		return
	}
	settler, ok := h.service.(interface {
		SettlePortfolioWorkflowForOwner(context.Context, string, string, uuid.UUID, PortfolioWorkflowSettlementRequest) (*PortfolioWorkflowSettlementResult, error)
	})
	if !ok {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "verified portfolio workflow settlement is unavailable"})
		return
	}
	var request PortfolioWorkflowSettlementRequest
	if !decodePursuitJSON(c, &request, 16*1024, "verified portfolio workflow settlement") {
		return
	}
	result, err := settler.SettlePortfolioWorkflowForOwner(
		c.Request.Context(), pursuitOwner(c), verifiedActor(c, "operator"), itemID, request,
	)
	if err != nil {
		message := strings.ToLower(err.Error())
		status := http.StatusBadRequest
		switch {
		case strings.Contains(message, "storage is unavailable"),
			strings.Contains(message, "verification is unavailable"),
			strings.Contains(message, "ledger is unavailable"):
			status = http.StatusServiceUnavailable
		case errors.Is(err, executionauth.ErrNotFound), strings.Contains(message, "unavailable to this owner"),
			strings.Contains(message, "reservation is unavailable"):
			status = http.StatusNotFound
		case errors.Is(err, executionauth.ErrFinalEffectMismatch), strings.Contains(message, "changed"),
			strings.Contains(message, "different"), strings.Contains(message, "mismatch"),
			strings.Contains(message, "not backed by verified completion"):
			status = http.StatusConflict
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	status := http.StatusCreated
	if result.Replayed {
		status = http.StatusOK
	}
	c.JSON(status, result)
}

func (h *Handler) PortfolioExecutionProposalDecisionHistory(c *gin.Context) {
	itemID, err := uuid.Parse(strings.TrimSpace(c.Param("itemId")))
	if err != nil || itemID == uuid.Nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "a valid portfolio execution proposal item id is required"})
		return
	}
	limit := 50
	if raw := strings.TrimSpace(c.Query("limit")); raw != "" {
		parsed, parseErr := strconv.Atoi(raw)
		if parseErr != nil || parsed < 1 || parsed > 100 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "limit must be between 1 and 100"})
			return
		}
		limit = parsed
	}
	reader, ok := h.service.(interface {
		PortfolioExecutionProposalDecisionHistoryForOwner(string, uuid.UUID, int) (*PortfolioExecutionProposalDecisionHistoryResult, error)
	})
	if !ok {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "portfolio execution proposal decision history is unavailable"})
		return
	}
	result, err := reader.PortfolioExecutionProposalDecisionHistoryForOwner(pursuitOwner(c), itemID, limit)
	if err != nil {
		status := http.StatusBadRequest
		if strings.Contains(strings.ToLower(err.Error()), "unavailable to this owner") {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *Handler) Get(c *gin.Context) {
	id, ok := parsePursuitID(c)
	if !ok {
		return
	}
	record, err := h.service.DetailForOwner(pursuitOwner(c), id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, record)
}

func (h *Handler) ResolveEvidence(c *gin.Context) {
	id, ok := parsePursuitID(c)
	if !ok {
		return
	}
	if !h.ensurePursuitVisible(c, id) {
		return
	}
	uri := c.Query("uri")
	record, err := h.service.ResolveEvidenceForOwner(pursuitOwner(c), id, uri)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, record)
}

func (h *Handler) ResourceUsage(c *gin.Context) {
	id, ok := parsePursuitID(c)
	if !ok {
		return
	}
	record, err := h.service.ResourceUsageForOwner(pursuitOwner(c), id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "pursuit not found"})
		return
	}
	c.JSON(http.StatusOK, record)
}

func (h *Handler) ResourceEvents(c *gin.Context) {
	id, ok := parsePursuitID(c)
	if !ok {
		return
	}
	limit := 100
	if raw := strings.TrimSpace(c.Query("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 500 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "limit must be between 1 and 500"})
			return
		}
		limit = parsed
	}
	records, err := h.service.ResourceEventsForOwner(pursuitOwner(c), id, limit)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "pursuit resource ledger is unavailable"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"events": records})
}

func (h *Handler) AppendResourceEvent(c *gin.Context) {
	id, ok := parsePursuitID(c)
	if !ok {
		return
	}
	if !h.ensurePursuitMutable(c, id) {
		return
	}
	var request AppendPursuitResourceEventRequest
	if !decodePursuitResourceRequest(c, &request) {
		return
	}
	request.Actor = verifiedActor(c, "operator")
	record, err := h.service.AppendResourceEventForOwner(pursuitOwner(c), id, request)
	if err != nil {
		status := http.StatusBadRequest
		if strings.Contains(strings.ToLower(err.Error()), "idempotency key") {
			status = http.StatusConflict
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, record)
}

func (h *Handler) ReleaseResourceReservation(c *gin.Context) {
	pursuitID, ok := parsePursuitID(c)
	if !ok {
		return
	}
	if !h.ensurePursuitMutable(c, pursuitID) {
		return
	}
	reservationID, err := uuid.Parse(strings.TrimSpace(c.Param("reservationId")))
	if err != nil || reservationID == uuid.Nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid resource reservation id"})
		return
	}
	var request ReleasePursuitResourceReservationRequest
	if !decodePursuitResourceRequest(c, &request) {
		return
	}
	request.Actor = verifiedActor(c, "operator")
	record, err := h.service.ReleaseResourceReservationForOwner(
		pursuitOwner(c), pursuitID, reservationID, request,
	)
	if err != nil {
		status := http.StatusBadRequest
		if strings.Contains(strings.ToLower(err.Error()), "already settled") {
			status = http.StatusConflict
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, record)
}

func (h *Handler) Update(c *gin.Context) {
	id, ok := parsePursuitID(c)
	if !ok {
		return
	}
	if !h.ensurePursuitMutable(c, id) {
		return
	}
	var request UpdateRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	request.Actor = verifiedActor(c, "operator")
	record, err := h.service.UpdateForOwner(pursuitOwner(c), id, request)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, record)
}

func (h *Handler) Archive(c *gin.Context) {
	id, ok := parsePursuitID(c)
	if !ok {
		return
	}
	if !h.ensurePursuitMutable(c, id) {
		return
	}
	var request struct {
		Archived *bool  `json:"archived"`
		Actor    string `json:"actor,omitempty"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if request.Archived == nil || !*request.Archived {
		c.JSON(http.StatusBadRequest, gin.H{"error": "archive requests must set archived=true; use the explicit reopen action to reactivate a pursuit"})
		return
	}
	request.Actor = verifiedActor(c, "operator")
	record, err := h.service.ArchiveForOwner(pursuitOwner(c), id, true, request.Actor)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, record)
}

func (h *Handler) Reopen(c *gin.Context) {
	id, ok := parsePursuitID(c)
	if !ok {
		return
	}
	if !h.ensurePursuitMutable(c, id) {
		return
	}
	var request struct {
		Note string `json:"note,omitempty"`
	}
	if c.Request.ContentLength != 0 {
		if err := c.ShouldBindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid pursuit reopen request"})
			return
		}
	}
	record, err := h.service.ReopenForOwner(pursuitOwner(c), id, verifiedActor(c, "operator"), request.Note)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, record)
}

func (h *Handler) Link(c *gin.Context) {
	id, ok := parsePursuitID(c)
	if !ok {
		return
	}
	if !h.ensurePursuitMutable(c, id) {
		return
	}
	var request LinkRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	request.OwnerIdentity = pursuitOwner(c)
	request.Actor = verifiedActor(c, "operator")
	record, err := h.service.Link(id, request)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, record)
}

func (h *Handler) DeleteLink(c *gin.Context) {
	id, ok := parsePursuitID(c)
	if !ok {
		return
	}
	if !h.ensurePursuitMutable(c, id) {
		return
	}
	linkID, err := uuid.Parse(c.Param("linkId"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid link id"})
		return
	}
	if err := h.service.DeleteLinkForOwner(pursuitOwner(c), id, linkID, verifiedActor(c, "operator")); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) Match(c *gin.Context) {
	var request MatchRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	request.OwnerIdentity = pursuitOwner(c)
	result, err := h.service.Match(request)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *Handler) RouteIntake(c *gin.Context) {
	var request IntakeRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	request.OwnerIdentity = pursuitOwner(c)
	request.Actor = verifiedActor(c, "operator")
	result, err := h.service.RouteIntake(request)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, result)
}

func (h *Handler) Intake(c *gin.Context) {
	id, ok := parsePursuitID(c)
	if !ok {
		return
	}
	if !h.ensurePursuitMutable(c, id) {
		return
	}
	var request IntakeRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	request.OwnerIdentity = pursuitOwner(c)
	request.Actor = verifiedActor(c, "operator")
	_, err := h.service.IntakeForOwner(pursuitOwner(c), id, request)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	h.respondScopedDetail(c, id, http.StatusCreated)
}

func (h *Handler) Plan(c *gin.Context) {
	id, ok := parsePursuitID(c)
	if !ok {
		return
	}
	if !h.ensurePursuitMutable(c, id) {
		return
	}
	var request PlanRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if detail, err := h.service.DetailForOwner(pursuitOwner(c), id); err != nil || detail == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "pursuit not found"})
		return
	} else if isPursuitCandidate(detail.Pursuit) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "pursuit candidate acceptance requires the explicit approval action"})
		return
	}
	request.Actor = verifiedActor(c, "operator")
	_, err := h.service.PlanForOwner(pursuitOwner(c), id, request)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	h.respondScopedDetail(c, id, http.StatusCreated)
}

// AcceptCandidate is deliberately separate from generic planning. Accepting an
// auto-created pursuit candidate is an auditable approval decision, so its
// route requires approval capability before it may create or unlock work.
func (h *Handler) AcceptCandidate(c *gin.Context) {
	id, ok := parsePursuitID(c)
	if !ok {
		return
	}
	if !pursuitApprovalAllowed(c) {
		c.JSON(http.StatusForbidden, gin.H{"error": "approval permission is required to accept a pursuit candidate"})
		return
	}
	if !h.ensurePursuitMutable(c, id) {
		return
	}
	var request PlanRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	detail, err := h.service.DetailForOwner(pursuitOwner(c), id)
	if err != nil || detail == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "pursuit not found"})
		return
	}
	if !isPursuitCandidate(detail.Pursuit) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "only an unaccepted pursuit candidate can use the candidate acceptance action"})
		return
	}
	request.Actor = verifiedActor(c, "operator")
	if _, err := h.service.AcceptCandidateForOwner(pursuitOwner(c), id, request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	h.respondScopedDetail(c, id, http.StatusCreated)
}

func (h *Handler) ResolveDecision(c *gin.Context) {
	id, ok := parsePursuitID(c)
	if !ok {
		return
	}
	if !h.ensurePursuitMutable(c, id) {
		return
	}
	// Decision resolution can approve a next action, mark verified completion,
	// or create a governed recovery workflow. Keep the approval boundary here
	// as well as in route registration so alternate Gin wiring cannot weaken it.
	if !pursuitApprovalAllowed(c) {
		c.JSON(http.StatusForbidden, gin.H{"error": "approval permission is required to resolve a pursuit decision"})
		return
	}
	var request DecisionResolutionRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	request.Actor = verifiedActor(c, "operator")
	_, err := h.service.ResolveDecisionForOwner(pursuitOwner(c), id, request)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	h.respondScopedDetail(c, id, http.StatusOK)
}

func (h *Handler) RefreshSummary(c *gin.Context) {
	id, ok := parsePursuitID(c)
	if !ok {
		return
	}
	if !h.ensurePursuitMutable(c, id) {
		return
	}
	var request struct {
		Actor string `json:"actor,omitempty"`
	}
	if c.Request.ContentLength != 0 {
		if err := c.ShouldBindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid pursuit summary request"})
			return
		}
	}
	request.Actor = verifiedActor(c, "system")
	_, err := h.service.RefreshSummaryForOwner(pursuitOwner(c), id, request.Actor)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	h.respondScopedDetail(c, id, http.StatusOK)
}

func (h *Handler) Review(c *gin.Context) {
	id, ok := parsePursuitID(c)
	if !ok {
		return
	}
	if !h.ensurePursuitMutable(c, id) {
		return
	}
	var request ReviewRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	request.Actor = verifiedActor(c, "operator")
	_, err := h.service.ReviewForOwner(pursuitOwner(c), id, request)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	h.respondScopedDetail(c, id, http.StatusOK)
}

func (h *Handler) Activity(c *gin.Context) {
	id, ok := parsePursuitID(c)
	if !ok {
		return
	}
	records, err := h.service.ActivityForOwner(pursuitOwner(c), id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "pursuit not found"})
		return
	}
	c.JSON(http.StatusOK, records)
}

func (h *Handler) NextActions(c *gin.Context) {
	id, ok := parsePursuitID(c)
	if !ok {
		return
	}
	record, err := h.service.DetailForOwner(pursuitOwner(c), id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, record.NextActions)
}

func (h *Handler) Blockers(c *gin.Context) {
	id, ok := parsePursuitID(c)
	if !ok {
		return
	}
	record, err := h.service.DetailForOwner(pursuitOwner(c), id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, record.Blockers)
}

func (h *Handler) Approvals(c *gin.Context) {
	id, ok := parsePursuitID(c)
	if !ok {
		return
	}
	if !h.ensurePursuitVisible(c, id) {
		return
	}
	record, err := h.service.ApprovalsForOwner(pursuitOwner(c), id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, record)
}

func (h *Handler) DelegationPackage(c *gin.Context) {
	id, ok := parsePursuitID(c)
	if !ok {
		return
	}
	record, err := h.service.DelegationPackageForOwner(pursuitOwner(c), id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "pursuit not found"})
		return
	}
	c.JSON(http.StatusOK, record)
}

func parsePursuitID(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid pursuit id"})
		return uuid.UUID{}, false
	}
	return id, true
}

func (h *Handler) ensurePursuitVisible(c *gin.Context, id uuid.UUID) bool {
	if _, err := h.service.DetailForOwner(pursuitOwner(c), id); err != nil {
		// Keep an inaccessible record indistinguishable from one that does not
		// exist so UUID probing does not disclose another user's work.
		c.JSON(http.StatusNotFound, gin.H{"error": "pursuit not found"})
		return false
	}
	return true
}

func (h *Handler) ensurePursuitMutable(c *gin.Context, id uuid.UUID) bool {
	record, err := h.service.DetailForOwner(pursuitOwner(c), id)
	if err != nil || !pursuitMutableBy(record.Pursuit, pursuitOwner(c)) {
		c.JSON(http.StatusNotFound, gin.H{"error": "pursuit not found"})
		return false
	}
	return true
}

func (h *Handler) respondScopedDetail(c *gin.Context, id uuid.UUID, status int) {
	record, err := h.service.DetailForOwner(pursuitOwner(c), id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "pursuit not found"})
		return
	}
	c.JSON(status, record)
}

func pursuitOwner(c *gin.Context) string {
	return verifiedActor(c, "")
}

func pursuitApprovalAllowed(c *gin.Context) bool {
	value, _ := c.Get(identity.ContextRoleKey)
	role, _ := value.(string)
	role = strings.ToLower(strings.TrimSpace(role))
	return rbac.Can(rbac.Role(role), rbac.PermApprove)
}

// verifiedActor deliberately ignores client-provided actor labels. When HAI is
// running behind its local IDP gateway, the backend has independently verified
// the signed session token and stores its subject in the Gin context. Local
// development without that identity path is recorded honestly as an operator,
// never as a user-supplied name such as "Robert".
func verifiedActor(c *gin.Context, fallback string) string {
	if value, ok := c.Get(identity.ContextSubjectKey); ok {
		if subject, ok := value.(string); ok && subject != "" {
			return subject
		}
	}
	return fallback
}

func decodePursuitResourceRequest(c *gin.Context, target any) bool {
	return decodePursuitJSON(c, target, 32*1024, "pursuit resource")
}

func decodePursuitJSON(c *gin.Context, target any, maximumBytes int64, label string) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maximumBytes)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": label + " request is too large"})
		} else {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid " + label + " request"})
		}
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		c.JSON(http.StatusBadRequest, gin.H{"error": label + " request must contain one JSON object"})
		return false
	}
	return true
}
