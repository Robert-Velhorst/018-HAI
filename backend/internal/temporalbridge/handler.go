package temporalbridge

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"automation-hub-backend/internal/apierror"
	"automation-hub-backend/internal/identity"

	"github.com/gin-gonic/gin"
)

type Handler struct{ service *Service }

const schedulePersistenceFailedMessage = "governed follow-up schedule could not be saved safely; inspect run history before retrying"

func NewHandler(service *Service) *Handler { return &Handler{service: service} }

func (h *Handler) Status(c *gin.Context) { c.JSON(http.StatusOK, h.service.Status()) }

func (h *Handler) StartWorker(c *gin.Context) {
	h.service.StartWorkerContext(c.Request.Context())
	status := h.service.Status()
	if status.WorkerStarting && status.WorkerError == "" {
		c.JSON(http.StatusAccepted, status)
		return
	}
	if status.Enabled && status.Configured && !status.WorkerStarted {
		c.JSON(http.StatusServiceUnavailable, status)
		return
	}
	c.JSON(http.StatusOK, status)
}

func (h *Handler) Runs(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "25"))
	runs, err := h.service.Runs(owner(c), limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not read durable workflow runs"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"runs": runs})
}

func (h *Handler) ScheduleFollowUp(c *gin.Context) {
	var request FollowUpRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid governed follow-up schedule"})
		return
	}
	run, err := h.service.ScheduleFollowUp(c.Request.Context(), owner(c), request)
	if errors.Is(err, ErrScheduleUncertain) {
		response := gin.H{"error": ErrScheduleUncertain.Error(), "retrySafe": false}
		if run != nil {
			response["runId"], response["temporalWorkflowId"] = run.ID, run.TemporalWorkflowID
		}
		c.JSON(http.StatusServiceUnavailable, response)
		return
	}
	if errors.Is(err, ErrNotConfigured) {
		c.JSON(http.StatusConflict, gin.H{"error": ErrNotConfigured.Error(), "status": h.service.Status()})
		return
	}
	if errors.Is(err, ErrUnavailable) {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": ErrUnavailable.Error(), "status": h.service.Status()})
		return
	}
	if errors.Is(err, ErrAuthorizationRequired) {
		c.JSON(http.StatusForbidden, gin.H{"error": ErrAuthorizationRequired.Error()})
		return
	}
	if errors.Is(err, ErrEmergencyStopActive) {
		c.JSON(http.StatusLocked, gin.H{"error": ErrEmergencyStopActive.Error()})
		return
	}
	if err != nil {
		status := http.StatusInternalServerError
		var public *apierror.Error
		if errors.As(err, &public) && public != nil {
			status = public.HTTPStatus()
		}
		c.JSON(status, gin.H{"error": apierror.PublicMessage(err, schedulePersistenceFailedMessage)})
		return
	}
	c.JSON(http.StatusCreated, run)
}

func owner(c *gin.Context) string {
	value, _ := c.Get(identity.ContextSubjectKey)
	owner, _ := value.(string)
	return strings.TrimSpace(owner)
}
