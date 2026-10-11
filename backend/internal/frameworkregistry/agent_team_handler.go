package frameworkregistry

import (
	"errors"
	"net/http"
	"strings"

	"automation-hub-backend/internal/agentcoordination"

	"github.com/gin-gonic/gin"
)

type AgentTeamHandler struct {
	service *AgentTeamService
}

func NewAgentTeamHandler(service *AgentTeamService) *AgentTeamHandler {
	return &AgentTeamHandler{service: service}
}

// AgentTeamRouteGuards keeps role and permission policy in the owning router
// package while making omission impossible. RecognizedRole must reject unknown
// verified role claims rather than silently treating them as viewers.
type AgentTeamRouteGuards struct {
	AuthenticatedOwner gin.HandlerFunc
	RecognizedRole     gin.HandlerFunc
	Read               gin.HandlerFunc
	Write              gin.HandlerFunc
	Govern             gin.HandlerFunc
}

// RegisterAgentTeamRoutes mounts below an existing /framework-registry group.
// It refuses incomplete security wiring and does not create an unguarded route.
func RegisterAgentTeamRoutes(parent *gin.RouterGroup, handler *AgentTeamHandler, guards AgentTeamRouteGuards) error {
	if parent == nil || handler == nil || handler.service == nil {
		return errors.New("agent team route group and service are required")
	}
	if guards.AuthenticatedOwner == nil || guards.RecognizedRole == nil || guards.Read == nil || guards.Write == nil || guards.Govern == nil {
		return errors.New("agent team routes require authentication, recognized-role, and permission guards")
	}
	routes := parent.Group("/teams")
	routes.Use(guards.AuthenticatedOwner, guards.RecognizedRole)
	{
		routes.GET("", guards.Read, handler.List)
		routes.GET("/message-attention", guards.Read, handler.MessageAttentionIndex)
		routes.POST("", guards.Govern, handler.Create)
		routes.POST("/guided", guards.Govern, handler.CreateGuided)
		routes.GET("/:id/versions", guards.Read, handler.ListVersions)
		routes.POST("/:id/versions", guards.Govern, handler.CreateVersion)
		routes.GET("/:id/versions/:version", guards.Read, handler.Get)
		routes.GET("/:id/versions/:version/events", guards.Read, handler.Events)
		routes.POST("/:id/versions/:version/activate", guards.Govern, handler.Activate)
		routes.POST("/:id/versions/:version/suspend", guards.Govern, handler.Suspend)
		routes.POST("/:id/versions/:version/retire", guards.Govern, handler.Retire)
		routes.POST("/:id/versions/:version/revoke", guards.Govern, handler.Revoke)
		routes.POST("/:id/versions/:version/members", guards.Govern, handler.AddMember)
		routes.POST("/:id/versions/:version/members/:memberId/status", guards.Govern, handler.ChangeMembership)
		routes.GET("/:id/versions/:version/decision-overview", guards.Read, handler.DecisionOverview)
		routes.GET("/:id/versions/:version/message-attention", guards.Read, handler.MessageAttention)
		routes.GET("/:id/versions/:version/messages", guards.Read, handler.Messages)
		routes.POST("/:id/versions/:version/messages", guards.Write, handler.StoreMessage)
		routes.POST("/:id/versions/:version/decision-messages", guards.Write, handler.CreateDecisionMessage)
		routes.GET("/:id/versions/:version/messages/:messageId/acknowledgments", guards.Read, handler.MessageAcknowledgments)
		routes.POST("/:id/versions/:version/messages/:messageId/acknowledgments", guards.Write, handler.AcknowledgeMessage)
		routes.POST("/:id/versions/:version/messages/:messageId/acknowledgments/guided", guards.Write, handler.CreateMessageAcknowledgment)
		routes.POST("/:id/versions/:version/delegations/assess", guards.Write, handler.AssessDelegation)
		routes.GET("/:id/versions/:version/consensus", guards.Read, handler.ConsensusOutcomes)
		routes.POST("/:id/versions/:version/consensus", guards.Govern, handler.RecordConsensus)
	}
	return nil
}

func (h *AgentTeamHandler) List(c *gin.Context) {
	owner, ok := frameworkOwner(c)
	if !ok {
		return
	}
	result, err := h.service.ListTeams(owner)
	respondAgentTeam(c, gin.H{"teams": result}, err, http.StatusOK)
}

func (h *AgentTeamHandler) Create(c *gin.Context) {
	owner, ok := frameworkOwner(c)
	if !ok {
		return
	}
	var request CreateAgentTeamRequest
	if !decodeFrameworkJSON(c, &request) {
		return
	}
	result, err := h.service.CreateTeam(owner, request)
	respondAgentTeam(c, result, err, http.StatusCreated)
}

func (h *AgentTeamHandler) CreateGuided(c *gin.Context) {
	owner, ok := frameworkOwner(c)
	if !ok {
		return
	}
	var request CreateGuidedAgentTeamRequest
	if !decodeFrameworkJSON(c, &request) {
		return
	}
	result, err := h.service.CreateGuidedTeam(owner, request)
	respondAgentTeam(c, result, err, http.StatusCreated)
}

func (h *AgentTeamHandler) Get(c *gin.Context) {
	owner, ok := frameworkOwner(c)
	if !ok {
		return
	}
	result, err := h.service.GetTeam(owner, c.Param("id"), c.Param("version"))
	respondAgentTeam(c, result, err, http.StatusOK)
}

func (h *AgentTeamHandler) ListVersions(c *gin.Context) {
	owner, ok := frameworkOwner(c)
	if !ok {
		return
	}
	result, err := h.service.ListTeamVersions(owner, c.Param("id"))
	respondAgentTeam(c, gin.H{"versions": result}, err, http.StatusOK)
}

func (h *AgentTeamHandler) CreateVersion(c *gin.Context) {
	owner, ok := frameworkOwner(c)
	if !ok {
		return
	}
	var request CreateAgentTeamVersionRequest
	if !decodeFrameworkJSON(c, &request) {
		return
	}
	result, err := h.service.CreateTeamVersion(owner, c.Param("id"), request)
	respondAgentTeam(c, result, err, http.StatusCreated)
}

func (h *AgentTeamHandler) Events(c *gin.Context) {
	owner, ok := frameworkOwner(c)
	if !ok {
		return
	}
	result, err := h.service.Events(owner, c.Param("id"), c.Param("version"))
	respondAgentTeam(c, gin.H{"events": result}, err, http.StatusOK)
}

func (h *AgentTeamHandler) Activate(c *gin.Context) {
	h.transition(c, h.service.ActivateTeam)
}

func (h *AgentTeamHandler) Suspend(c *gin.Context) {
	h.transition(c, h.service.SuspendTeam)
}

func (h *AgentTeamHandler) Retire(c *gin.Context) {
	h.transition(c, h.service.RetireTeam)
}

func (h *AgentTeamHandler) Revoke(c *gin.Context) {
	h.transition(c, h.service.RevokeTeam)
}

func (h *AgentTeamHandler) transition(c *gin.Context, transition func(string, string, string, TeamTransitionRequest) (*AgentTeamContract, error)) {
	owner, ok := frameworkOwner(c)
	if !ok {
		return
	}
	var request TeamTransitionRequest
	if !decodeFrameworkJSON(c, &request) {
		return
	}
	result, err := transition(owner, c.Param("id"), c.Param("version"), request)
	respondAgentTeam(c, result, err, http.StatusOK)
}

func (h *AgentTeamHandler) AddMember(c *gin.Context) {
	owner, ok := frameworkOwner(c)
	if !ok {
		return
	}
	var request AddTeamMemberRequest
	if !decodeFrameworkJSON(c, &request) {
		return
	}
	result, err := h.service.AddMember(owner, c.Param("id"), c.Param("version"), request)
	respondAgentTeam(c, result, err, http.StatusCreated)
}

func (h *AgentTeamHandler) ChangeMembership(c *gin.Context) {
	owner, ok := frameworkOwner(c)
	if !ok {
		return
	}
	var request ChangeTeamMembershipRequest
	if !decodeFrameworkJSON(c, &request) {
		return
	}
	result, err := h.service.ChangeMembership(owner, c.Param("id"), c.Param("version"), c.Param("memberId"), request)
	respondAgentTeam(c, result, err, http.StatusOK)
}

func (h *AgentTeamHandler) StoreMessage(c *gin.Context) {
	owner, ok := frameworkOwner(c)
	if !ok {
		return
	}
	var request agentcoordination.Message
	if !decodeFrameworkJSON(c, &request) {
		return
	}
	result, created, err := h.service.StoreCoordinationMessage(owner, c.Param("id"), c.Param("version"), request)
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	respondAgentTeam(c, result, err, status)
}

func (h *AgentTeamHandler) CreateDecisionMessage(c *gin.Context) {
	owner, ok := frameworkOwner(c)
	if !ok {
		return
	}
	var request CreateTeamDecisionMessageRequest
	if !decodeFrameworkJSON(c, &request) {
		return
	}
	result, created, err := h.service.CreateDecisionMessage(owner, c.Param("id"), c.Param("version"), request)
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	respondAgentTeam(c, result, err, status)
}

func (h *AgentTeamHandler) Messages(c *gin.Context) {
	owner, ok := frameworkOwner(c)
	if !ok {
		return
	}
	correlationID := strings.TrimSpace(c.Query("correlationId"))
	result, err := h.service.CoordinationMessages(owner, c.Param("id"), c.Param("version"), correlationID)
	respondAgentTeam(c, gin.H{"messages": result}, err, http.StatusOK)
}

func (h *AgentTeamHandler) AcknowledgeMessage(c *gin.Context) {
	owner, ok := frameworkOwner(c)
	if !ok {
		return
	}
	var request agentcoordination.Acknowledgment
	if !decodeFrameworkJSON(c, &request) {
		return
	}
	result, created, err := h.service.AcknowledgeCoordinationMessage(
		owner,
		c.Param("id"),
		c.Param("version"),
		c.Param("messageId"),
		request,
	)
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	respondAgentTeam(c, result, err, status)
}

func (h *AgentTeamHandler) CreateMessageAcknowledgment(c *gin.Context) {
	owner, ok := frameworkOwner(c)
	if !ok {
		return
	}
	var request CreateTeamAcknowledgmentRequest
	if !decodeFrameworkJSON(c, &request) {
		return
	}
	result, created, err := h.service.CreateMessageAcknowledgment(owner, c.Param("id"), c.Param("version"), c.Param("messageId"), request)
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	respondAgentTeam(c, result, err, status)
}

func (h *AgentTeamHandler) MessageAcknowledgments(c *gin.Context) {
	owner, ok := frameworkOwner(c)
	if !ok {
		return
	}
	result, err := h.service.MessageAcknowledgments(owner, c.Param("id"), c.Param("version"), c.Param("messageId"))
	respondAgentTeam(c, gin.H{"acknowledgments": result}, err, http.StatusOK)
}

func (h *AgentTeamHandler) MessageAttention(c *gin.Context) {
	owner, ok := frameworkOwner(c)
	if !ok {
		return
	}
	result, err := h.service.MessageAttention(owner, c.Param("id"), c.Param("version"))
	respondAgentTeam(c, result, err, http.StatusOK)
}

func (h *AgentTeamHandler) DecisionOverview(c *gin.Context) {
	owner, ok := frameworkOwner(c)
	if !ok {
		return
	}
	result, err := h.service.DecisionOverview(owner, c.Param("id"), c.Param("version"))
	respondAgentTeam(c, result, err, http.StatusOK)
}

func (h *AgentTeamHandler) MessageAttentionIndex(c *gin.Context) {
	owner, ok := frameworkOwner(c)
	if !ok {
		return
	}
	result, err := h.service.MessageAttentionIndex(owner)
	respondAgentTeam(c, result, err, http.StatusOK)
}

type assessTeamDelegationRequest struct {
	Risk       string                               `json:"risk"`
	Delegation agentcoordination.DelegationEnvelope `json:"delegation"`
}

func (h *AgentTeamHandler) AssessDelegation(c *gin.Context) {
	owner, ok := frameworkOwner(c)
	if !ok {
		return
	}
	var request assessTeamDelegationRequest
	if !decodeFrameworkJSON(c, &request) {
		return
	}
	result, err := h.service.AssessDelegation(owner, c.Param("id"), c.Param("version"), request.Risk, request.Delegation)
	respondAgentTeam(c, result, err, http.StatusOK)
}

type recordTeamConsensusRequest struct {
	CorrelationID  string `json:"correlationId"`
	IdempotencyKey string `json:"idempotencyKey"`
	Issue          string `json:"issue"`
}

func (h *AgentTeamHandler) RecordConsensus(c *gin.Context) {
	owner, ok := frameworkOwner(c)
	if !ok {
		return
	}
	var request recordTeamConsensusRequest
	if !decodeFrameworkJSON(c, &request) {
		return
	}
	result, created, err := h.service.RecordConsensus(owner, c.Param("id"), c.Param("version"), request.CorrelationID, request.IdempotencyKey, request.Issue)
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	respondAgentTeam(c, result, err, status)
}

func (h *AgentTeamHandler) ConsensusOutcomes(c *gin.Context) {
	owner, ok := frameworkOwner(c)
	if !ok {
		return
	}
	result, err := h.service.ConsensusOutcomes(owner, c.Param("id"), c.Param("version"))
	respondAgentTeam(c, gin.H{"outcomes": result}, err, http.StatusOK)
}

func respondAgentTeam(c *gin.Context, value any, err error, successStatus int) {
	if err == nil {
		c.JSON(successStatus, value)
		return
	}
	if errors.Is(err, ErrAgentTeamNotFound) || errors.Is(err, ErrAgentTeamMessageNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "agent team record not found"})
		return
	}
	if errors.Is(err, ErrAgentTeamRevisionConflict) || errors.Is(err, ErrAgentTeamIdempotencyConflict) || errors.Is(err, ErrAgentTeamAcknowledgmentTerminal) {
		c.JSON(http.StatusConflict, gin.H{"error": "agent team state conflict"})
		return
	}
	message := strings.ToLower(strings.TrimSpace(err.Error()))
	for _, fragment := range []string{
		"team lifecycle actor and reason are required",
		"contains secret material",
		"must be a uuid",
		"must be semantic version",
		"key, name, and purpose are required",
		"evidence references are required",
		"at least one team capability is required",
		"at least one team role is required",
		"coordination policy allowlists are required",
		"consensus issue is required",
		"acknowledgment does not match",
		"acknowledgment status is invalid",
		"acknowledgment creation time is invalid",
		"acknowledgment idempotency key",
		"acknowledgment id",
		"does not require acknowledgment",
		"deferred acknowledgment requires",
		"rejected acknowledgment requires",
		"accepted acknowledgment cannot",
		"decision sender and recipient",
		"decision sender does not hold",
		"decision position is invalid",
		"decision issue and recommendation",
		"decision evidence references",
		"decision expiry",
		"decision correlation ID",
		"decision idempotency key",
		"retry window",
	} {
		if strings.Contains(message, fragment) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid agent team request"})
			return
		}
	}
	for _, fragment := range []string{
		"only draft", "only active", "terminal team", "already revoked",
		"invalid membership transition", "below voting quorum", "cannot activate",
	} {
		if strings.Contains(message, fragment) {
			c.JSON(http.StatusConflict, gin.H{"error": "agent team lifecycle conflict"})
			return
		}
	}
	respondFramework(c, nil, err, successStatus)
}
