package browserverify

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"automation-hub-backend/internal/apierror"
	"automation-hub-backend/internal/identity"

	"github.com/gin-gonic/gin"
)

type Handler struct{ service *service }

func NewHandler(service *service) *Handler { return &Handler{service: service} }
func (h *Handler) Status(c *gin.Context)   { c.JSON(http.StatusOK, h.service.Status()) }
func (h *Handler) Profiles(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"profiles": h.service.Profiles()})
}
func (h *Handler) Run(c *gin.Context) {
	var request struct {
		WorkflowID string `json:"workflowId,omitempty"`
	}
	if err := c.ShouldBindJSON(&request); err != nil && !errors.Is(err, io.EOF) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "browser verification request is invalid"})
		return
	}
	run, err := h.service.RunWithWorkflow(c.Request.Context(), owner(c), c.Param("id"), request.WorkflowID)
	if errors.Is(err, ErrNotConfigured) {
		c.JSON(http.StatusConflict, gin.H{"error": "browser verification is not configured", "status": h.service.Status()})
		return
	}
	if errors.Is(err, ErrUnavailable) {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "browser verification is unavailable", "run": run})
		return
	}
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": apierror.PublicMessage(err, "browser verification request is invalid")})
		return
	}
	c.JSON(http.StatusCreated, run)
}
func (h *Handler) Runs(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "25"))
	runs, err := h.service.Runs(owner(c), limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not read browser verification runs"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"runs": runs})
}
func owner(c *gin.Context) string {
	value, _ := c.Get(identity.ContextSubjectKey)
	owner, _ := value.(string)
	return strings.TrimSpace(owner)
}
