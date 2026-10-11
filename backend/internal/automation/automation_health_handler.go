package automation

import (
	"automation-hub-backend/internal/executionauth"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"net/http"
)

func (h *Handler) HealthSummary(c *gin.Context) {
	summary, err := h.service.HealthSummary()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, summary)
}

func (h *Handler) Launch(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID format"})
		return
	}
	actor := verifiedAutomationActor(c)
	request := TaskLaunchRequest{}
	if c.Request.ContentLength != 0 {
		if err := c.ShouldBindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid launch request"})
			return
		}
	}
	// Authority-bearing identity and approval fields remain server-owned. The
	// optional mandateId is only a reference to owner-scoped policy evaluated
	// by executionauth; it is never accepted as proof of authorization.
	request.OwnerIdentity = actor
	request.ActorIdentity = actor
	request.ActorKind = executionauth.ActorHuman
	request.ExecutionContext = c.Request.Context()
	request.ApprovalSourceID = ""
	request.ApprovalBindingDigest = ""
	request.ApprovalProof = nil
	result, err := h.service.LaunchTask(id, request)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *Handler) StopRuntimeTask(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID format"})
		return
	}
	result, err := h.service.StopRuntimeTaskForOwner(id, verifiedAutomationActor(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	status := http.StatusOK
	if result.Status == "blocked" {
		status = http.StatusBadRequest
	}
	c.JSON(status, result)
}

func (h *Handler) RunHealthCheck(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID format"})
		return
	}
	result, err := h.service.RunHealthCheck(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *Handler) HealthCheck(c *gin.Context) {
	h.RunHealthCheck(c)
}

func (h *Handler) Diagnostics(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID format"})
		return
	}
	result, err := h.service.Diagnostics(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}
