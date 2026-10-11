package lifeops

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"automation-hub-backend/internal/identity"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

const maxLifeOpsRequestBytes = 128 * 1024

type Handler struct {
	service *Service
}

// LifeOverview is the bounded initial payload for the whole-life module. It
// keeps all values owner-scoped and represents an absent capacity snapshot as
// null rather than asking the client to treat a 404 as normal page state.
type LifeOverview struct {
	Domains  []LifeDomain      `json:"domains"`
	Needs    []NeedObservation `json:"needs"`
	Capacity *CapacitySnapshot `json:"capacity"`
	Goals    []GoalNode        `json:"goals"`
	Forest   []GoalTreeNode    `json:"forest"`
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) Domains(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"domains": h.service.Domains()})
}

// Overview returns the initial whole-life workspace context in one response.
// It shares the goal list with the forest builder so the initial screen does
// not read the same owner-scoped goals twice.
func (h *Handler) Overview(c *gin.Context) {
	owner, ok := lifeOpsOwner(c)
	if !ok {
		return
	}
	needs, err := h.service.Needs(owner, "", 100)
	if err != nil {
		respondLifeOps(c, nil, err, http.StatusOK)
		return
	}
	capacity, err := h.service.LatestCapacity(owner)
	if err != nil && !errors.Is(err, ErrNotFound) {
		respondLifeOps(c, nil, err, http.StatusOK)
		return
	}
	goals, err := h.service.Goals(owner)
	if err != nil {
		respondLifeOps(c, nil, err, http.StatusOK)
		return
	}
	c.JSON(http.StatusOK, LifeOverview{
		Domains:  h.service.Domains(),
		Needs:    needs,
		Capacity: capacity,
		Goals:    goals,
		Forest:   GoalForestFromGoals(goals),
	})
}

func (h *Handler) LinkEntity(c *gin.Context) {
	owner, ok := lifeOpsOwner(c)
	if !ok {
		return
	}
	var request LinkEntityRequest
	if !decodeLifeOpsJSON(c, &request) {
		return
	}
	request.OwnerIdentity = owner
	result, err := h.service.LinkEntity(request)
	respondLifeOps(c, result, err, http.StatusCreated)
}

func (h *Handler) EntityDomains(c *gin.Context) {
	owner, ok := lifeOpsOwner(c)
	if !ok {
		return
	}
	result, err := h.service.EntityDomains(owner, c.Param("entityType"), c.Param("entityId"))
	respondLifeOps(c, gin.H{"links": result}, err, http.StatusOK)
}

func (h *Handler) RecordNeed(c *gin.Context) {
	owner, ok := lifeOpsOwner(c)
	if !ok {
		return
	}
	var request RecordNeedRequest
	if !decodeLifeOpsJSON(c, &request) {
		return
	}
	request.OwnerIdentity = owner
	result, err := h.service.RecordNeed(request)
	respondLifeOps(c, result, err, http.StatusCreated)
}

func (h *Handler) Needs(c *gin.Context) {
	owner, ok := lifeOpsOwner(c)
	if !ok {
		return
	}
	limit, err := parseLifeOpsLimit(c.Query("limit"))
	if err != nil {
		respondLifeOps(c, nil, err, http.StatusOK)
		return
	}
	result, err := h.service.Needs(owner, DomainID(c.Query("domainId")), limit)
	respondLifeOps(c, gin.H{"observations": result}, err, http.StatusOK)
}

func (h *Handler) RecordCapacity(c *gin.Context) {
	owner, ok := lifeOpsOwner(c)
	if !ok {
		return
	}
	var request RecordCapacityRequest
	if !decodeLifeOpsJSON(c, &request) {
		return
	}
	request.OwnerIdentity = owner
	result, err := h.service.RecordCapacity(request)
	respondLifeOps(c, result, err, http.StatusCreated)
}

func (h *Handler) CapacityHistory(c *gin.Context) {
	owner, ok := lifeOpsOwner(c)
	if !ok {
		return
	}
	limit, err := parseLifeOpsLimit(c.Query("limit"))
	if err != nil {
		respondLifeOps(c, nil, err, http.StatusOK)
		return
	}
	result, err := h.service.CapacityHistory(owner, limit)
	respondLifeOps(c, gin.H{"snapshots": result}, err, http.StatusOK)
}

func (h *Handler) LatestCapacity(c *gin.Context) {
	owner, ok := lifeOpsOwner(c)
	if !ok {
		return
	}
	result, err := h.service.LatestCapacity(owner)
	respondLifeOps(c, result, err, http.StatusOK)
}

func (h *Handler) CreateGoal(c *gin.Context) {
	owner, ok := lifeOpsOwner(c)
	if !ok {
		return
	}
	var request CreateGoalRequest
	if !decodeLifeOpsJSON(c, &request) {
		return
	}
	request.OwnerIdentity = owner
	result, err := h.service.CreateGoal(request)
	respondLifeOps(c, result, err, http.StatusCreated)
}

func (h *Handler) Goals(c *gin.Context) {
	owner, ok := lifeOpsOwner(c)
	if !ok {
		return
	}
	result, err := h.service.Goals(owner)
	respondLifeOps(c, gin.H{"goals": result}, err, http.StatusOK)
}

func (h *Handler) GoalForest(c *gin.Context) {
	owner, ok := lifeOpsOwner(c)
	if !ok {
		return
	}
	result, err := h.service.GoalForest(owner)
	respondLifeOps(c, gin.H{"forest": result}, err, http.StatusOK)
}

func (h *Handler) Goal(c *gin.Context) {
	owner, id, ok := lifeOpsOwnerAndUUID(c)
	if !ok {
		return
	}
	result, err := h.service.Goal(owner, id)
	respondLifeOps(c, result, err, http.StatusOK)
}

func (h *Handler) UpdateGoal(c *gin.Context) {
	owner, id, ok := lifeOpsOwnerAndUUID(c)
	if !ok {
		return
	}
	var request UpdateGoalRequest
	if !decodeLifeOpsJSON(c, &request) {
		return
	}
	result, err := h.service.UpdateGoal(owner, id, request)
	respondLifeOps(c, result, err, http.StatusOK)
}

func (h *Handler) AssessPriority(c *gin.Context) {
	owner, ok := lifeOpsOwner(c)
	if !ok {
		return
	}
	var request PriorityAssessmentRequest
	if !decodeLifeOpsJSON(c, &request) {
		return
	}
	request.OwnerIdentity = owner
	result, err := h.service.AssessPriority(request)
	respondLifeOps(c, result, err, http.StatusOK)
}

func (h *Handler) PriorityHistory(c *gin.Context) {
	owner, ok := lifeOpsOwner(c)
	if !ok {
		return
	}
	limit, err := parseLifeOpsLimit(c.Query("limit"))
	if err != nil {
		respondLifeOps(c, nil, err, http.StatusOK)
		return
	}
	result, err := h.service.PriorityHistory(
		owner,
		c.Query("entityType"),
		c.Query("entityId"),
		limit,
	)
	respondLifeOps(c, gin.H{"assessments": result}, err, http.StatusOK)
}

func lifeOpsOwner(c *gin.Context) (string, bool) {
	value, exists := c.Get(identity.ContextSubjectKey)
	owner, ok := value.(string)
	owner = strings.TrimSpace(owner)
	if !exists || !ok || owner == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "an authenticated owner session is required for whole-life context"})
		return "", false
	}
	return owner, true
}

func lifeOpsOwnerAndUUID(c *gin.Context) (string, uuid.UUID, bool) {
	owner, ok := lifeOpsOwner(c)
	if !ok {
		return "", uuid.Nil, false
	}
	id, err := uuid.Parse(strings.TrimSpace(c.Param("id")))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "a valid goal id is required"})
		return "", uuid.Nil, false
	}
	return owner, id, true
}

func decodeLifeOpsJSON(c *gin.Context, target any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxLifeOpsRequestBytes)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "whole-life context request is too large"})
			return false
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid whole-life context request"})
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "whole-life context request must contain one JSON object"})
		return false
	}
	return true
}

func parseLifeOpsLimit(raw string) (int, error) {
	if strings.TrimSpace(raw) == "" {
		return 100, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("limit must be a number between 1 and 500")
	}
	if limit < 1 || limit > 500 {
		return 0, fmt.Errorf("limit must be between 1 and 500")
	}
	return limit, nil
}

func respondLifeOps(c *gin.Context, value any, err error, successStatus int) {
	if err == nil {
		c.JSON(successStatus, value)
		return
	}
	switch {
	case errors.Is(err, ErrNotFound), errors.Is(err, gorm.ErrRecordNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "whole-life context record not found"})
	case isLifeOpsConflict(err):
		c.JSON(http.StatusConflict, gin.H{"error": "whole-life context state conflict"})
	case isLifeOpsValidationError(err):
		c.JSON(http.StatusBadRequest, gin.H{"error": compactLifeOpsValidation(err.Error())})
	default:
		errorID := uuid.NewString()
		_ = c.Error(fmt.Errorf("whole-life context error %s: %w", errorID, err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "whole-life context operation failed",
			"errorId": errorID,
		})
	}
}

func isLifeOpsConflict(err error) bool {
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "cycle") ||
		strings.Contains(message, "parent") ||
		strings.Contains(message, "already exists")
}

func isLifeOpsValidationError(err error) bool {
	message := strings.ToLower(err.Error())
	for _, fragment := range []string{
		"is required", "must be", "must contain", "must have", "must not",
		"between", "unknown life domain", "invalid", "cannot",
	} {
		if strings.Contains(message, fragment) {
			return true
		}
	}
	return false
}

func compactLifeOpsValidation(message string) string {
	message = strings.TrimSpace(message)
	if len(message) > 300 {
		return "whole-life context request failed validation"
	}
	return message
}
