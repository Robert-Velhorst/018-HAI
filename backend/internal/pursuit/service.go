package pursuit

import (
	"automation-hub-backend/internal/identity"
	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/plangraph"
	"automation-hub-backend/internal/safety"
	"automation-hub-backend/internal/workflow"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
)

const (
	StatusActive    = "active"
	StatusWaiting   = "waiting"
	StatusBlocked   = "blocked"
	StatusCompleted = "completed"
	StatusArchived  = "archived"

	CompletionOpen      = "open"
	CompletionCandidate = "candidate"
	CompletionVerified  = "verified"

	CriterionPending   = "pending"
	CriterionSatisfied = "satisfied"
	CriterionWaived    = "waived"

	StopMonitoring = "monitoring"
	StopTriggered  = "triggered"
	StopResolved   = "resolved"

	DependencyPending   = "pending"
	DependencySatisfied = "satisfied"
	DependencyBlocked   = "blocked"
	DependencyWaived    = "waived"

	LinkWorkflow           = "workflow"
	LinkMemory             = "memory"
	LinkAIConversation     = "ai_conversation"
	LinkSourceExtraction   = "source_extraction"
	LinkSourceItem         = "source_item"
	LinkVerification       = "verification"
	LinkAutomation         = "automation"
	LinkAgentRuntime       = "agent_runtime"
	LinkAmbientOpportunity = "ambient_opportunity"
	LinkAssistantCommand   = "assistant_command"
	LinkPursuit            = "pursuit"
)

const defaultAutoLinkMinimumScore = 0.45

// ErrLifecycleRouterRequired prevents a partially configured pursuit linker
// from creating workflow work before pursuit matching and candidate acceptance.
var ErrLifecycleRouterRequired = errors.New("pursuit lifecycle router is required when a pursuit linker is configured")

type CreateRequest struct {
	Actor                 string                           `json:"-"`
	OwnerIdentity         string                           `json:"ownerIdentity,omitempty"`
	Title                 string                           `json:"title"`
	Description           string                           `json:"description,omitempty"`
	WhyItMatters          string                           `json:"whyItMatters,omitempty"`
	ProjectKey            string                           `json:"projectKey,omitempty"`
	MandateID             string                           `json:"mandateId,omitempty"`
	Domain                string                           `json:"domain,omitempty"`
	DesiredOutcome        string                           `json:"desiredOutcome,omitempty"`
	CurrentStateSummary   string                           `json:"currentStateSummary,omitempty"`
	Status                string                           `json:"status,omitempty"`
	PriorityScore         int                              `json:"priorityScore,omitempty"`
	RiskLevel             string                           `json:"riskLevel,omitempty"`
	Confidence            float64                          `json:"confidence,omitempty"`
	AutonomyLevel         string                           `json:"autonomyLevel,omitempty"`
	NeedCategory          string                           `json:"needCategory,omitempty"`
	SourceOfCreation      string                           `json:"sourceOfCreation,omitempty"`
	NextRecommendedAction string                           `json:"nextRecommendedAction,omitempty"`
	CompletionDefinition  string                           `json:"completionDefinition,omitempty"`
	NextReviewAt          string                           `json:"nextReviewAt,omitempty"`
	SuccessCriteria       []models.PursuitSuccessCriterion `json:"successCriteria,omitempty"`
	StopConditions        []models.PursuitStopCondition    `json:"stopConditions,omitempty"`
	Dependencies          []models.PursuitDependency       `json:"dependencies,omitempty"`
	ResourceLimits        models.PursuitResourceLimits     `json:"resourceLimits,omitempty"`
	TargetAt              string                           `json:"targetAt,omitempty"`
	ReviewCadenceDays     int                              `json:"reviewCadenceDays,omitempty"`
}

type UpdateRequest struct {
	Title                 string                            `json:"title,omitempty"`
	Description           *string                           `json:"description,omitempty"`
	WhyItMatters          *string                           `json:"whyItMatters,omitempty"`
	ProjectKey            *string                           `json:"projectKey,omitempty"`
	Domain                *string                           `json:"domain,omitempty"`
	DesiredOutcome        *string                           `json:"desiredOutcome,omitempty"`
	CurrentStateSummary   *string                           `json:"currentStateSummary,omitempty"`
	Status                string                            `json:"status,omitempty"`
	PriorityScore         *int                              `json:"priorityScore,omitempty"`
	RiskLevel             string                            `json:"riskLevel,omitempty"`
	Confidence            *float64                          `json:"confidence,omitempty"`
	AutonomyLevel         string                            `json:"autonomyLevel,omitempty"`
	NeedCategory          *string                           `json:"needCategory,omitempty"`
	NextRecommendedAction *string                           `json:"nextRecommendedAction,omitempty"`
	CompletionDefinition  *string                           `json:"completionDefinition,omitempty"`
	CompletionState       string                            `json:"completionState,omitempty"`
	NextReviewAt          *string                           `json:"nextReviewAt,omitempty"`
	SuccessCriteria       *[]models.PursuitSuccessCriterion `json:"successCriteria,omitempty"`
	StopConditions        *[]models.PursuitStopCondition    `json:"stopConditions,omitempty"`
	Dependencies          *[]models.PursuitDependency       `json:"dependencies,omitempty"`
	ResourceLimits        *models.PursuitResourceLimits     `json:"resourceLimits,omitempty"`
	TargetAt              *string                           `json:"targetAt,omitempty"`
	ReviewCadenceDays     *int                              `json:"reviewCadenceDays,omitempty"`
	Archived              *bool                             `json:"archived,omitempty"`
	Actor                 string                            `json:"actor,omitempty"`
}

type ReviewRequest struct {
	Action       string `json:"action,omitempty"`
	Actor        string `json:"actor,omitempty"`
	Note         string `json:"note,omitempty"`
	NextReviewAt string `json:"nextReviewAt,omitempty"`
	SnoozeDays   int    `json:"snoozeDays,omitempty"`
}

type PlanRequest struct {
	Input            string                              `json:"input,omitempty"`
	Actor            string                              `json:"actor,omitempty"`
	RequiresReview   bool                                `json:"requiresReview,omitempty"`
	ReviewReason     string                              `json:"reviewReason,omitempty"`
	CoordinationPlan plangraph.AcceptedRevisionReference `json:"coordinationPlan,omitempty"`
}

type DecisionResolutionRequest struct {
	DecisionID    string `json:"decisionId"`
	DecisionType  string `json:"decisionType,omitempty"`
	Approved      bool   `json:"approved"`
	Reason        string `json:"reason,omitempty"`
	Note          string `json:"note,omitempty"`
	EvidenceURI   string `json:"evidenceUri,omitempty"`
	EvidenceLabel string `json:"evidenceLabel,omitempty"`
	Actor         string `json:"actor,omitempty"`
}

type LinkRequest struct {
	OwnerIdentity string  `json:"-"`
	LinkType      string  `json:"linkType"`
	LinkID        string  `json:"linkId"`
	Relationship  string  `json:"relationship,omitempty"`
	SourceURI     string  `json:"sourceUri,omitempty"`
	SourceLabel   string  `json:"sourceLabel,omitempty"`
	Confidence    float64 `json:"confidence,omitempty"`
	Actor         string  `json:"actor,omitempty"`
}

type MatchRequest struct {
	OwnerIdentity string `json:"-"`
	Input         string `json:"input,omitempty"`
	ProjectKey    string `json:"projectKey,omitempty"`
	SourceType    string `json:"sourceType,omitempty"`
	SourceID      string `json:"sourceId,omitempty"`
	SourceURI     string `json:"sourceUri,omitempty"`
	Limit         int    `json:"limit,omitempty"`
}

type MatchCandidate struct {
	Pursuit    models.Pursuit `json:"pursuit"`
	Score      float64        `json:"score"`
	Reasons    []string       `json:"reasons"`
	Confidence string         `json:"confidence"`
}

type AutoLinkWorkflowRequest struct {
	OwnerIdentity         string    `json:"-"`
	WorkflowID            uuid.UUID `json:"workflowId"`
	Input                 string    `json:"input,omitempty"`
	ProjectKey            string    `json:"projectKey,omitempty"`
	MandateID             string    `json:"mandateId,omitempty"`
	SourceType            string    `json:"sourceType,omitempty"`
	SourceID              string    `json:"sourceId,omitempty"`
	SourceURI             string    `json:"sourceUri,omitempty"`
	SourceLabel           string    `json:"sourceLabel,omitempty"`
	ExtractionID          string    `json:"extractionId,omitempty"`
	RawItemID             string    `json:"rawItemId,omitempty"`
	ConversationID        uuid.UUID `json:"conversationId,omitempty"`
	ConversationSourceURI string    `json:"conversationSourceUri,omitempty"`
	ConversationLabel     string    `json:"conversationLabel,omitempty"`
	Actor                 string    `json:"actor,omitempty"`
	MinimumScore          float64   `json:"minimumScore,omitempty"`
	AllowCreateCandidate  bool      `json:"allowCreateCandidate,omitempty"`
}

type AutoLinkMemoryRequest struct {
	OwnerIdentity         string    `json:"-"`
	MemoryID              uuid.UUID `json:"memoryId"`
	Input                 string    `json:"input,omitempty"`
	ProjectKey            string    `json:"projectKey,omitempty"`
	SourceURI             string    `json:"sourceUri,omitempty"`
	SourceLabel           string    `json:"sourceLabel,omitempty"`
	ConversationID        uuid.UUID `json:"conversationId,omitempty"`
	ConversationSourceURI string    `json:"conversationSourceUri,omitempty"`
	ConversationLabel     string    `json:"conversationLabel,omitempty"`
	Actor                 string    `json:"actor,omitempty"`
	MinimumScore          float64   `json:"minimumScore,omitempty"`
	AllowCreateCandidate  bool      `json:"allowCreateCandidate,omitempty"`
}

type AutoLinkResult struct {
	Linked    bool                 `json:"linked"`
	Created   bool                 `json:"created,omitempty"`
	PursuitID uuid.UUID            `json:"pursuitId,omitempty"`
	Score     float64              `json:"score"`
	Reasons   []string             `json:"reasons,omitempty"`
	Message   string               `json:"message,omitempty"`
	Links     []models.PursuitLink `json:"links,omitempty"`
}

type IntakeRequest struct {
	OwnerIdentity    string                              `json:"-"`
	Input            string                              `json:"input"`
	ProjectKey       string                              `json:"projectKey,omitempty"`
	AutomationID     string                              `json:"automationId,omitempty"`
	MandateID        string                              `json:"mandateId,omitempty"`
	SourceType       string                              `json:"sourceType,omitempty"`
	SourceID         string                              `json:"sourceId,omitempty"`
	RawItemID        string                              `json:"rawItemId,omitempty"`
	ExtractionID     string                              `json:"extractionId,omitempty"`
	SourceURI        string                              `json:"sourceUri,omitempty"`
	SourceLabel      string                              `json:"sourceLabel,omitempty"`
	ContentType      string                              `json:"contentType,omitempty"`
	Sender           string                              `json:"sender,omitempty"`
	ReceivedAt       string                              `json:"receivedAt,omitempty"`
	Trigger          string                              `json:"trigger,omitempty"`
	Actor            string                              `json:"actor,omitempty"`
	RequiresReview   bool                                `json:"requiresReview,omitempty"`
	ReviewReason     string                              `json:"reviewReason,omitempty"`
	CoordinationPlan plangraph.AcceptedRevisionReference `json:"coordinationPlan,omitempty"`
}

type RoutedIntakeResult struct {
	Mode             string           `json:"mode"`
	Matched          bool             `json:"matched"`
	CreatedCandidate bool             `json:"createdCandidate"`
	PursuitID        uuid.UUID        `json:"pursuitId,omitempty"`
	Score            float64          `json:"score,omitempty"`
	Reasons          []string         `json:"reasons,omitempty"`
	Message          string           `json:"message,omitempty"`
	Matches          []MatchCandidate `json:"matches,omitempty"`
	Detail           *PursuitDetail   `json:"detail,omitempty"`
	AutoLink         *AutoLinkResult  `json:"autoLink,omitempty"`
}

// CandidatePendingError reports a normal deferred intake outcome. It lets
// source and conversation importers preserve their candidate provenance
// without treating the absence of operational work as a failed import.
type CandidatePendingError struct {
	Result *RoutedIntakeResult
}

func (e *CandidatePendingError) Error() string {
	if e == nil || e.Result == nil {
		return "pursuit candidate is awaiting explicit acceptance"
	}
	return firstNonEmpty(e.Result.Message, "pursuit candidate is awaiting explicit acceptance")
}

// CandidatePending is intentionally package-neutral so the workflow package
// can return a truthful HTTP response without importing pursuit and creating
// an import cycle.
func (e *CandidatePendingError) CandidatePending() bool { return true }

func (e *CandidatePendingError) CandidatePursuitID() string {
	if e == nil || e.Result == nil || e.Result.PursuitID == uuid.Nil {
		return ""
	}
	return e.Result.PursuitID.String()
}

func (e *CandidatePendingError) CandidateIntakeMessage() string {
	if e == nil || e.Result == nil {
		return "pursuit candidate is awaiting explicit acceptance"
	}
	return firstNonEmpty(e.Result.Message, "pursuit candidate is awaiting explicit acceptance")
}

func IsCandidatePending(err error) (*RoutedIntakeResult, bool) {
	var pending *CandidatePendingError
	if !errors.As(err, &pending) {
		return nil, false
	}
	return pending.Result, true
}

// AmbientOpportunityRouteRequest describes an operator-accepted ambient
// proposal before it becomes workflow work. It keeps pursuit matching ahead of
// execution so an unaccepted candidate never acquires an orphan workflow.
type AmbientOpportunityRouteRequest struct {
	OwnerIdentity  string    `json:"-"`
	OpportunityID  uuid.UUID `json:"opportunityId"`
	Title          string    `json:"title"`
	Rationale      string    `json:"rationale,omitempty"`
	NextAction     string    `json:"nextAction"`
	ProjectKey     string    `json:"projectKey,omitempty"`
	SourceURI      string    `json:"sourceUri,omitempty"`
	RequiresReview bool      `json:"requiresReview,omitempty"`
	ReviewReason   string    `json:"reviewReason,omitempty"`
	Actor          string    `json:"actor,omitempty"`
}

type AmbientOpportunityRouteResult struct {
	Mode             string    `json:"mode"`
	PursuitID        uuid.UUID `json:"pursuitId"`
	WorkflowID       uuid.UUID `json:"workflowId,omitempty"`
	CreatedCandidate bool      `json:"createdCandidate"`
	Message          string    `json:"message,omitempty"`
}

type Dashboard struct {
	Counts               map[string]int64           `json:"counts"`
	DecisionQueue        []PursuitDashboardDecision `json:"decisionQueue"`
	NeedsRobert          []PursuitListItem          `json:"needsRobert"`
	VAReady              []PursuitListItem          `json:"vaReady"`
	SystemReady          []PursuitListItem          `json:"systemReady"`
	Blocked              []PursuitListItem          `json:"blocked"`
	Stale                []PursuitListItem          `json:"stale"`
	ReviewDue            []PursuitListItem          `json:"reviewDue"`
	PlanningNeeded       []PursuitListItem          `json:"planningNeeded"`
	RecentlyChanged      []PursuitListItem          `json:"recentlyChanged"`
	HighRisk             []PursuitListItem          `json:"highRisk"`
	CompletionCandidates []PursuitListItem          `json:"completionCandidates"`
}

// dashboardSummary keeps the queue and decision semantics intact while
// removing record fields that are only used by the exact pursuit detail view.
// The full dashboard remains the default API contract for existing clients.
func dashboardSummary(source *Dashboard) *Dashboard {
	if source == nil {
		return nil
	}
	result := *source
	result.DecisionQueue = make([]PursuitDashboardDecision, len(source.DecisionQueue))
	for index, decision := range source.DecisionQueue {
		decision.Pursuit = dashboardPursuitSummary(decision.Pursuit)
		decision.Decision = dashboardDecisionSummary(decision.Decision)
		result.DecisionQueue[index] = decision
	}
	result.NeedsRobert = dashboardListSummary(source.NeedsRobert, 1)
	result.VAReady = dashboardListSummary(source.VAReady, 1)
	result.SystemReady = dashboardListSummary(source.SystemReady, 1)
	result.Blocked = dashboardListSummary(source.Blocked, 1)
	result.Stale = dashboardListSummary(source.Stale, 1)
	result.ReviewDue = dashboardListSummary(source.ReviewDue, 1)
	result.PlanningNeeded = dashboardListSummary(source.PlanningNeeded, 1)
	result.RecentlyChanged = dashboardListSummary(source.RecentlyChanged, 1)
	result.HighRisk = dashboardListSummary(source.HighRisk, 1)
	result.CompletionCandidates = dashboardListSummary(source.CompletionCandidates, 1)
	return &result
}

func dashboardCounts(source *Dashboard) *Dashboard {
	if source == nil {
		return nil
	}
	counts := make(map[string]int64, len(source.Counts))
	for key, value := range source.Counts {
		counts[key] = value
	}
	return &Dashboard{
		Counts:               counts,
		DecisionQueue:        []PursuitDashboardDecision{},
		NeedsRobert:          []PursuitListItem{},
		VAReady:              []PursuitListItem{},
		SystemReady:          []PursuitListItem{},
		Blocked:              []PursuitListItem{},
		Stale:                []PursuitListItem{},
		ReviewDue:            []PursuitListItem{},
		PlanningNeeded:       []PursuitListItem{},
		RecentlyChanged:      []PursuitListItem{},
		HighRisk:             []PursuitListItem{},
		CompletionCandidates: []PursuitListItem{},
	}
}

func dashboardListSummary(source []PursuitListItem, limit int) []PursuitListItem {
	length := len(source)
	if limit > 0 && length > limit {
		length = limit
	}
	result := make([]PursuitListItem, length)
	for index, item := range source[:length] {
		item.Pursuit = dashboardPursuitSummary(item.Pursuit)
		result[index] = item
	}
	return result
}

func dashboardPursuitSummary(pursuit models.Pursuit) models.Pursuit {
	return models.Pursuit{
		ID:                    pursuit.ID,
		Title:                 pursuit.Title,
		WhyItMatters:          pursuit.WhyItMatters,
		ProjectKey:            pursuit.ProjectKey,
		Domain:                pursuit.Domain,
		CurrentStateSummary:   pursuit.CurrentStateSummary,
		Status:                pursuit.Status,
		PriorityScore:         pursuit.PriorityScore,
		RiskLevel:             pursuit.RiskLevel,
		Confidence:            pursuit.Confidence,
		AutonomyLevel:         pursuit.AutonomyLevel,
		NeedCategory:          pursuit.NeedCategory,
		NextRecommendedAction: pursuit.NextRecommendedAction,
		CompletionState:       pursuit.CompletionState,
		LastActivityAt:        pursuit.LastActivityAt,
		NextReviewAt:          pursuit.NextReviewAt,
		TargetAt:              pursuit.TargetAt,
		ReviewCadenceDays:     pursuit.ReviewCadenceDays,
		Archived:              pursuit.Archived,
		CreatedAt:             pursuit.CreatedAt,
		UpdatedAt:             pursuit.UpdatedAt,
	}
}

func dashboardDecisionSummary(decision PursuitDecision) PursuitDecision {
	return PursuitDecision{
		ID:               decision.ID,
		DecisionType:     decision.DecisionType,
		Status:           decision.Status,
		Recommended:      decision.Recommended,
		Reason:           decision.Reason,
		RiskLevel:        decision.RiskLevel,
		YesLabel:         decision.YesLabel,
		NoLabel:          decision.NoLabel,
		RequiresApproval: decision.RequiresApproval,
		Approved:         decision.Approved,
	}
}

type Brief struct {
	GeneratedAt          time.Time   `json:"generatedAt"`
	OperatingMode        string      `json:"operatingMode"`
	Summary              string      `json:"summary"`
	PrimaryAction        string      `json:"primaryAction"`
	NeedsRobert          int         `json:"needsRobert"`
	ReadyToMove          int         `json:"readyToMove"`
	Stuck                int         `json:"stuck"`
	ReviewDue            int         `json:"reviewDue"`
	PlanningNeeded       int         `json:"planningNeeded"`
	CompletionCandidates int         `json:"completionCandidates"`
	RecentlyChanged      int         `json:"recentlyChanged"`
	Cards                []BriefCard `json:"cards"`
}

// OperatingSnapshot is the compact pursuit projection consumed by the agent
// cycle. Both fields are derived from one dashboard build so a proactive pass
// does not expand every pursuit and its linked records twice.
type OperatingSnapshot struct {
	Brief     *Brief                     `json:"brief"`
	Decisions []PursuitDashboardDecision `json:"decisions"`
	Dashboard *Dashboard                 `json:"-"`
}

type BriefCard struct {
	Queue        string `json:"queue"`
	PursuitID    string `json:"pursuitId"`
	Title        string `json:"title"`
	Action       string `json:"action"`
	Context      string `json:"context"`
	RiskLevel    string `json:"riskLevel"`
	EvidenceLine string `json:"evidenceLine"`
	NeedsRobert  bool   `json:"needsRobert"`
}

type PursuitListItem struct {
	Pursuit                 models.Pursuit `json:"pursuit"`
	NeedsRobert             int            `json:"needsRobert"`
	Blocked                 int            `json:"blocked"`
	OpenLoops               int            `json:"openLoops"`
	DecisionCards           int            `json:"decisionCards"`
	LinkedEvidence          int            `json:"linkedEvidence"`
	TimelineItems           int            `json:"timelineItems"`
	CompletionCandidate     bool           `json:"completionCandidate"`
	CurrentState            string         `json:"currentState,omitempty"`
	WhatChanged             string         `json:"whatChanged,omitempty"`
	NextAction              string         `json:"nextAction,omitempty"`
	EffectiveLastActivityAt *time.Time     `json:"effectiveLastActivityAt,omitempty"`
	Stale                   bool           `json:"stale"`
	ReviewDue               bool           `json:"reviewDue"`
	PlanningNeeded          bool           `json:"planningNeeded"`
}

type PursuitDashboardDecision struct {
	Pursuit      models.Pursuit  `json:"pursuit"`
	Decision     PursuitDecision `json:"decision"`
	CurrentState string          `json:"currentState,omitempty"`
	NextAction   string          `json:"nextAction,omitempty"`
	Blocked      int             `json:"blocked"`
	EvidenceLine string          `json:"evidenceLine,omitempty"`
}

type PursuitDetail struct {
	Pursuit              models.Pursuit                 `json:"pursuit"`
	Links                []models.PursuitLink           `json:"links"`
	Activity             []models.PursuitActivity       `json:"activity"`
	Workflows            []models.WorkflowItem          `json:"workflows"`
	ChecklistItems       []models.WorkflowChecklistItem `json:"checklistItems"`
	OpenLoops            []models.WorkflowOpenLoop      `json:"openLoops"`
	Proposals            []models.WorkflowProposal      `json:"proposals"`
	QualityGates         []models.WorkflowQualityGate   `json:"qualityGates"`
	Decisions            []models.WorkflowDecision      `json:"decisions"`
	DecisionQueue        []PursuitDecision              `json:"decisionQueue"`
	Transitions          []models.WorkflowTransition    `json:"transitions"`
	SourceLinks          []models.WorkflowSourceLink    `json:"sourceLinks"`
	Events               []models.WorkflowEvent         `json:"events"`
	Timeline             []PursuitTimelineItem          `json:"timeline"`
	Evidence             []models.WorkflowEvidenceClaim `json:"evidence"`
	Memories             []models.ContextMemory         `json:"memories"`
	Conversations        []PursuitConversation          `json:"conversations"`
	AmbientOpportunities []PursuitAmbientOpportunity    `json:"ambientOpportunities"`
	TaskRuns             []PursuitTaskRun               `json:"taskRuns"`
	TaskAttempts         []models.PursuitTaskAttempt    `json:"taskAttempts"`
	VerificationRuns     []models.VerificationRun       `json:"verificationRuns"`
	VerificationClaims   []models.VerificationClaim     `json:"verificationClaims"`
	VerificationEvidence []models.VerificationEvidence  `json:"verificationEvidence"`
	Automations          []PursuitAutomation            `json:"automations"`
	RuntimeAttempts      []models.AutomationLaunchEvent `json:"runtimeAttempts"`
	SourceItems          []PursuitSourceItem            `json:"sourceItems"`
	SourceExtractions    []models.SourceExtraction      `json:"sourceExtractions"`
	NextActions          []PursuitAction                `json:"nextActions"`
	ActionQueues         PursuitActionQueues            `json:"actionQueues"`
	Blockers             []PursuitBlocker               `json:"blockers"`
	ApprovalItems        []models.WorkflowItem          `json:"approvalItems"`
	Summary              PursuitSummary                 `json:"summary"`
	OperationalDigest    PursuitOperationalDigest       `json:"operationalDigest"`
	ResourceUsage        PursuitResourceUsage           `json:"resourceUsage"`
}

// pursuitDetailRecords is the owner-filtered record set used to derive one
// pursuit detail. Keeping derivation separate from loading lets the dashboard
// bulk-load records without creating a second set of decision rules.
type pursuitDetailRecords struct {
	Links                []models.PursuitLink
	Activity             []models.PursuitActivity
	Workflows            []models.WorkflowItem
	ChecklistItems       []models.WorkflowChecklistItem
	OpenLoops            []models.WorkflowOpenLoop
	Proposals            []models.WorkflowProposal
	QualityGates         []models.WorkflowQualityGate
	Decisions            []models.WorkflowDecision
	Transitions          []models.WorkflowTransition
	SourceLinks          []models.WorkflowSourceLink
	Events               []models.WorkflowEvent
	Evidence             []models.WorkflowEvidenceClaim
	Memories             []models.ContextMemory
	Conversations        []models.AIConversationArchive
	AmbientOpportunities []models.AmbientOpportunity
	TaskAttempts         []models.PursuitTaskAttempt
	VerificationRuns     []models.VerificationRun
	VerificationClaims   []models.VerificationClaim
	VerificationEvidence []models.VerificationEvidence
	Automations          []models.Automation
	RuntimeAttempts      []models.AutomationLaunchEvent
	SourceItems          []models.SourceRawItem
	SourceExtractions    []models.SourceExtraction
	ResourceUsage        PursuitResourceUsage
}

// PursuitConversation exposes only archive metadata needed for provenance.
// It intentionally omits Preview and encrypted payload fields so a pursuit
// detail response cannot become a second conversation-content endpoint.
type PursuitConversation struct {
	ID            uuid.UUID  `json:"id"`
	Platform      string     `json:"platform"`
	ExternalID    string     `json:"externalId"`
	Title         string     `json:"title,omitempty"`
	SourceURI     string     `json:"sourceUri,omitempty"`
	Revision      int        `json:"revision"`
	MessageCount  int        `json:"messageCount"`
	CapturedAt    time.Time  `json:"capturedAt"`
	LastMessageAt *time.Time `json:"lastMessageAt,omitempty"`
	Archived      bool       `json:"archived"`
}

// PursuitAmbientOpportunity is the bounded proactive-planning projection for
// a pursuit. It deliberately omits owner identity and the raw evidence
// manifest; source evidence remains behind the existing source/evidence views.
type PursuitAmbientOpportunity struct {
	ID               uuid.UUID `json:"id"`
	NeedKey          string    `json:"needKey"`
	Title            string    `json:"title"`
	Rationale        string    `json:"rationale,omitempty"`
	NextAction       string    `json:"nextAction,omitempty"`
	SourceType       string    `json:"sourceType,omitempty"`
	SourceURI        string    `json:"sourceUri,omitempty"`
	PriorityScore    int       `json:"priorityScore"`
	Confidence       int       `json:"confidence"`
	Risk             int       `json:"risk"`
	RequiresApproval bool      `json:"requiresApproval"`
	Status           string    `json:"status"`
	LastSeenAt       time.Time `json:"lastSeenAt"`
	ResolutionNote   string    `json:"resolutionNote,omitempty"`
	CreatedAt        time.Time `json:"createdAt"`
	UpdatedAt        time.Time `json:"updatedAt"`
}

// PursuitDelegationPackage is a read-only handoff brief. It turns the
// existing pursuit context into bounded work for a VA without assigning a
// person, sending a message, or bypassing workflow approval controls.
type PursuitDelegationPackage struct {
	GeneratedAt              time.Time                   `json:"generatedAt"`
	Ready                    bool                        `json:"ready"`
	Status                   string                      `json:"status"`
	Reason                   string                      `json:"reason"`
	PursuitID                string                      `json:"pursuitId"`
	Title                    string                      `json:"title"`
	Objective                string                      `json:"objective"`
	WhyItMatters             string                      `json:"whyItMatters,omitempty"`
	CurrentState             string                      `json:"currentState"`
	CompletionDefinition     string                      `json:"completionDefinition,omitempty"`
	RiskLevel                string                      `json:"riskLevel"`
	WorkItems                []PursuitDelegationWorkItem `json:"workItems"`
	SourceContext            []PursuitDelegationSource   `json:"sourceContext"`
	AllowedActions           []string                    `json:"allowedActions"`
	BlockedActions           []string                    `json:"blockedActions"`
	EscalationRules          []string                    `json:"escalationRules"`
	DeliveryRequirements     []string                    `json:"deliveryRequirements"`
	OutstandingRobertActions []PursuitAction             `json:"outstandingRobertActions"`
}

type PursuitDelegationWorkItem struct {
	WorkflowID   string                           `json:"workflowId,omitempty"`
	Title        string                           `json:"title"`
	Instructions string                           `json:"instructions"`
	State        string                           `json:"state,omitempty"`
	DueAt        *time.Time                       `json:"dueAt,omitempty"`
	Checklist    []PursuitDelegationChecklistItem `json:"checklist"`
}

type PursuitDelegationChecklistItem struct {
	Label    string `json:"label"`
	Status   string `json:"status"`
	Required bool   `json:"required"`
}

type PursuitDelegationSource struct {
	WorkflowID   string `json:"workflowId,omitempty"`
	SourceType   string `json:"sourceType,omitempty"`
	SourceURI    string `json:"sourceUri"`
	SourceLabel  string `json:"sourceLabel,omitempty"`
	Relationship string `json:"relationship,omitempty"`
}

type PursuitEvidenceResolution struct {
	URI                  string                        `json:"uri"`
	Kind                 string                        `json:"kind"`
	Title                string                        `json:"title"`
	Summary              string                        `json:"summary,omitempty"`
	Status               string                        `json:"status,omitempty"`
	SourceType           string                        `json:"sourceType,omitempty"`
	SourceID             string                        `json:"sourceId,omitempty"`
	SourceLabel          string                        `json:"sourceLabel,omitempty"`
	WorkflowID           string                        `json:"workflowId,omitempty"`
	NeedsReview          bool                          `json:"needsReview"`
	RuntimeAttempt       *models.AutomationLaunchEvent `json:"runtimeAttempt,omitempty"`
	PursuitLink          *models.PursuitLink           `json:"pursuitLink,omitempty"`
	TimelineItem         *PursuitTimelineItem          `json:"timelineItem,omitempty"`
	WorkflowEvidence     *models.WorkflowEvidenceClaim `json:"workflowEvidence,omitempty"`
	Memory               *models.ContextMemory         `json:"memory,omitempty"`
	SourceItem           *PursuitSourceItem            `json:"sourceItem,omitempty"`
	SourceExtraction     *models.SourceExtraction      `json:"sourceExtraction,omitempty"`
	VerificationEvidence *models.VerificationEvidence  `json:"verificationEvidence,omitempty"`
	Activity             *models.PursuitActivity       `json:"activity,omitempty"`
}

type PursuitApprovalOverview struct {
	Pursuit       models.Pursuit        `json:"pursuit"`
	DecisionQueue []PursuitDecision     `json:"decisionQueue"`
	ApprovalItems []models.WorkflowItem `json:"approvalItems"`
	Actions       []PursuitAction       `json:"actions"`
	Blockers      []PursuitBlocker      `json:"blockers"`
	Summary       PursuitSummary        `json:"summary"`
	Counts        map[string]int        `json:"counts"`
}

type PursuitSourceItem struct {
	ID         uuid.UUID `json:"id"`
	SourceID   uuid.UUID `json:"sourceId"`
	ExternalID string    `json:"externalId"`
	ProjectKey string    `json:"projectKey,omitempty"`
	ItemType   string    `json:"itemType,omitempty"`
	Title      string    `json:"title,omitempty"`
	SourceURI  string    `json:"sourceUri,omitempty"`
	Metadata   string    `json:"metadata,omitempty"`
	FetchedAt  time.Time `json:"fetchedAt"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

type PursuitAutomation struct {
	ID                uuid.UUID  `json:"id"`
	Name              string     `json:"name"`
	RuntimeType       string     `json:"runtimeType,omitempty"`
	LaunchType        string     `json:"launchType,omitempty"`
	Status            string     `json:"status,omitempty"`
	LastLaunchAt      *time.Time `json:"lastLaunchAt,omitempty"`
	LastFailureReason string     `json:"lastFailureReason,omitempty"`
}

type PursuitTaskRun struct {
	WorkflowID         uuid.UUID  `json:"workflowId"`
	WorkflowTitle      string     `json:"workflowTitle"`
	TaskPlanID         string     `json:"taskPlanId,omitempty"`
	Status             string     `json:"status"`
	VerificationStatus string     `json:"verificationStatus,omitempty"`
	RetryCount         int        `json:"retryCount"`
	MaxRetries         int        `json:"maxRetries"`
	LastRunAt          *time.Time `json:"lastRunAt,omitempty"`
	NextRunAt          *time.Time `json:"nextRunAt,omitempty"`
	LastWorkerError    string     `json:"lastWorkerError,omitempty"`
	AutomationID       string     `json:"automationId,omitempty"`
	NeedsReview        bool       `json:"needsReview"`
}

type PursuitDecision struct {
	ID               string `json:"id"`
	WorkflowID       string `json:"workflowId,omitempty"`
	WorkflowTitle    string `json:"workflowTitle,omitempty"`
	DecisionType     string `json:"decisionType"`
	Status           string `json:"status"`
	Recommended      string `json:"recommended"`
	Reason           string `json:"reason"`
	RiskLevel        string `json:"riskLevel"`
	EvidenceURI      string `json:"evidenceUri,omitempty"`
	EvidenceLabel    string `json:"evidenceLabel,omitempty"`
	YesLabel         string `json:"yesLabel"`
	NoLabel          string `json:"noLabel"`
	YesConsequence   string `json:"yesConsequence"`
	NoConsequence    string `json:"noConsequence"`
	RequiresApproval bool   `json:"requiresApproval"`
	Approved         bool   `json:"approved"`
	Actor            string `json:"actor,omitempty"`
	CreatedAt        string `json:"createdAt,omitempty"`
}

type PursuitTimelineItem struct {
	ID            string    `json:"id"`
	Kind          string    `json:"kind"`
	Title         string    `json:"title"`
	Message       string    `json:"message,omitempty"`
	WorkflowID    string    `json:"workflowId,omitempty"`
	WorkflowTitle string    `json:"workflowTitle,omitempty"`
	Actor         string    `json:"actor,omitempty"`
	Status        string    `json:"status,omitempty"`
	RiskLevel     string    `json:"riskLevel,omitempty"`
	SourceURI     string    `json:"sourceUri,omitempty"`
	SourceLabel   string    `json:"sourceLabel,omitempty"`
	NeedsReview   bool      `json:"needsReview"`
	CreatedAt     time.Time `json:"createdAt"`
}

type PursuitAction struct {
	Label            string `json:"label"`
	Owner            string `json:"owner"`
	RiskLevel        string `json:"riskLevel"`
	RequiresApproval bool   `json:"requiresApproval"`
	Reason           string `json:"reason"`
	WorkflowID       string `json:"workflowId,omitempty"`
	YesLabel         string `json:"yesLabel,omitempty"`
	NoLabel          string `json:"noLabel,omitempty"`
}

type PursuitBlocker struct {
	Label      string     `json:"label"`
	Reason     string     `json:"reason"`
	Owner      string     `json:"owner"`
	WorkflowID string     `json:"workflowId,omitempty"`
	FollowUpAt *time.Time `json:"followUpAt,omitempty"`
}

type PursuitActionQueues struct {
	NeedsRobert []PursuitAction `json:"needsRobert"`
	VAReady     []PursuitAction `json:"vaReady"`
	SystemReady []PursuitAction `json:"systemReady"`
	Waiting     []PursuitAction `json:"waiting"`
}

type PursuitSummary struct {
	CurrentState              string  `json:"currentState"`
	WhatChanged               string  `json:"whatChanged"`
	NeedsRobert               int     `json:"needsRobert"`
	Blocked                   int     `json:"blocked"`
	OpenLoops                 int     `json:"openLoops"`
	RobertActions             int     `json:"robertActions"`
	VAReadyActions            int     `json:"vaReadyActions"`
	SystemReadyActions        int     `json:"systemReadyActions"`
	WaitingActions            int     `json:"waitingActions"`
	ReviewDue                 bool    `json:"reviewDue"`
	DecisionCards             int     `json:"decisionCards"`
	TimelineItems             int     `json:"timelineItems"`
	TaskRuns                  int     `json:"taskRuns"`
	LinkedEvidence            int     `json:"linkedEvidence"`
	VerificationRuns          int     `json:"verificationRuns"`
	RuntimeAttempts           int     `json:"runtimeAttempts"`
	QualityGatesNeedingReview int     `json:"qualityGatesNeedingReview"`
	Confidence                float64 `json:"confidence"`
	PlanningNeeded            bool    `json:"planningNeeded"`
	CompletionCandidate       bool    `json:"completionCandidate"`
	GoalContractReady         bool    `json:"goalContractReady"`
	CriteriaSatisfied         int     `json:"criteriaSatisfied"`
	CriteriaTotal             int     `json:"criteriaTotal"`
	TriggeredStopConditions   int     `json:"triggeredStopConditions"`
	OpenDependencies          int     `json:"openDependencies"`
	TargetOverdue             bool    `json:"targetOverdue"`
}

type PursuitOperationalDigest struct {
	PrimaryLane       string `json:"primaryLane"`
	Headline          string `json:"headline"`
	RecommendedAction string `json:"recommendedAction"`
	RobertLine        string `json:"robertLine"`
	DelegationLine    string `json:"delegationLine"`
	SystemLine        string `json:"systemLine"`
	WaitingLine       string `json:"waitingLine"`
	BlockerLine       string `json:"blockerLine"`
	EvidenceLine      string `json:"evidenceLine"`
	RuntimeLine       string `json:"runtimeLine"`
	SourceLine        string `json:"sourceLine"`
	VerificationLine  string `json:"verificationLine"`
	RiskLine          string `json:"riskLine"`
	NeedsRobert       int    `json:"needsRobert"`
	VAReady           int    `json:"vaReady"`
	SystemReady       int    `json:"systemReady"`
	Waiting           int    `json:"waiting"`
	Blocked           int    `json:"blocked"`
	Evidence          int    `json:"evidence"`
	RuntimeAttempts   int    `json:"runtimeAttempts"`
	VerificationRuns  int    `json:"verificationRuns"`
	OpenLoops         int    `json:"openLoops"`
}

type Service interface {
	Create(request CreateRequest) (*models.Pursuit, error)
	Update(id uuid.UUID, request UpdateRequest) (*models.Pursuit, error)
	UpdateForOwner(ownerIdentity string, id uuid.UUID, request UpdateRequest) (*models.Pursuit, error)
	Archive(id uuid.UUID, archived bool, actor string) (*models.Pursuit, error)
	ArchiveForOwner(ownerIdentity string, id uuid.UUID, archived bool, actor string) (*models.Pursuit, error)
	Reopen(id uuid.UUID, actor, note string) (*models.Pursuit, error)
	ReopenForOwner(ownerIdentity string, id uuid.UUID, actor, note string) (*models.Pursuit, error)
	List(includeArchived bool) ([]models.Pursuit, error)
	ListForOwner(ownerIdentity string, includeArchived bool) ([]models.Pursuit, error)
	UpsertTaskAttempt(attempt models.PursuitTaskAttempt) error
	Dashboard() (*Dashboard, error)
	DashboardForOwner(ownerIdentity string) (*Dashboard, error)
	Decisions() ([]PursuitDashboardDecision, error)
	DecisionsForOwner(ownerIdentity string) ([]PursuitDashboardDecision, error)
	Brief() (*Brief, error)
	BriefForOwner(ownerIdentity string) (*Brief, error)
	Detail(id uuid.UUID) (*PursuitDetail, error)
	DetailForOwner(ownerIdentity string, id uuid.UUID) (*PursuitDetail, error)
	ResolveEvidence(id uuid.UUID, uri string) (*PursuitEvidenceResolution, error)
	ResolveEvidenceForOwner(ownerIdentity string, id uuid.UUID, uri string) (*PursuitEvidenceResolution, error)
	Approvals(id uuid.UUID) (*PursuitApprovalOverview, error)
	ApprovalsForOwner(ownerIdentity string, id uuid.UUID) (*PursuitApprovalOverview, error)
	DelegationPackage(id uuid.UUID) (*PursuitDelegationPackage, error)
	DelegationPackageForOwner(ownerIdentity string, id uuid.UUID) (*PursuitDelegationPackage, error)
	Link(id uuid.UUID, request LinkRequest) (*models.PursuitLink, error)
	LinkVerification(pursuitID, verificationID uuid.UUID) error
	LinkVerificationForOwner(ownerIdentity string, pursuitID, verificationID uuid.UUID) error
	DeleteLink(id uuid.UUID, linkID uuid.UUID, actor string) error
	DeleteLinkForOwner(ownerIdentity string, id uuid.UUID, linkID uuid.UUID, actor string) error
	Match(request MatchRequest) ([]MatchCandidate, error)
	AutoLinkWorkflow(request AutoLinkWorkflowRequest) (*AutoLinkResult, error)
	AutoLinkMemory(request AutoLinkMemoryRequest) (*AutoLinkResult, error)
	RouteIntake(request IntakeRequest) (*RoutedIntakeResult, error)
	RouteAmbientOpportunity(request AmbientOpportunityRouteRequest) (*AmbientOpportunityRouteResult, error)
	RouteWorkflowIntake(request workflow.IntakeRequest) (*workflow.WorkflowRecord, error)
	Intake(id uuid.UUID, request IntakeRequest) (*PursuitDetail, error)
	IntakeForOwner(ownerIdentity string, id uuid.UUID, request IntakeRequest) (*PursuitDetail, error)
	Plan(id uuid.UUID, request PlanRequest) (*PursuitDetail, error)
	PlanForOwner(ownerIdentity string, id uuid.UUID, request PlanRequest) (*PursuitDetail, error)
	AcceptCandidate(id uuid.UUID, request PlanRequest) (*PursuitDetail, error)
	AcceptCandidateForOwner(ownerIdentity string, id uuid.UUID, request PlanRequest) (*PursuitDetail, error)
	ResolveDecision(id uuid.UUID, request DecisionResolutionRequest) (*PursuitDetail, error)
	ResolveDecisionForOwner(ownerIdentity string, id uuid.UUID, request DecisionResolutionRequest) (*PursuitDetail, error)
	RefreshSummary(id uuid.UUID, actor string) (*PursuitDetail, error)
	RefreshSummaryForOwner(ownerIdentity string, id uuid.UUID, actor string) (*PursuitDetail, error)
	Review(id uuid.UUID, request ReviewRequest) (*PursuitDetail, error)
	ReviewForOwner(ownerIdentity string, id uuid.UUID, request ReviewRequest) (*PursuitDetail, error)
	Activity(id uuid.UUID) ([]models.PursuitActivity, error)
	ActivityForOwner(ownerIdentity string, id uuid.UUID) ([]models.PursuitActivity, error)
	AppendResourceEvent(id uuid.UUID, request AppendPursuitResourceEventRequest) (*models.PursuitResourceEvent, error)
	AppendResourceEventForOwner(ownerIdentity string, id uuid.UUID, request AppendPursuitResourceEventRequest) (*models.PursuitResourceEvent, error)
	ResourceUsage(id uuid.UUID) (*PursuitResourceUsage, error)
	ResourceUsageForOwner(ownerIdentity string, id uuid.UUID) (*PursuitResourceUsage, error)
	ResourceEventsForOwner(ownerIdentity string, id uuid.UUID, limit int) ([]models.PursuitResourceEvent, error)
	ReleaseResourceReservationForOwner(ownerIdentity string, pursuitID, reservationID uuid.UUID, request ReleasePursuitResourceReservationRequest) (*PursuitResourceUsage, error)
	PlanPortfolioForOwner(ownerIdentity string, request PortfolioPlanningRequest) (*PortfolioPlanningResult, error)
}

type ownerScopedRepository interface {
	FindAllForOwner(ownerIdentity string, includeArchived bool) ([]models.Pursuit, error)
}

// ownerScopedLinkValidator protects direct links to records that carry private
// user context. System-wide operational records retain their existing runtime
// and approval controls.
type ownerScopedLinkValidator interface {
	LinkVisibleToOwner(ownerIdentity, linkType, linkID string) (handled bool, visible bool, err error)
}

type workflowIntakeService interface {
	Intake(request workflow.IntakeRequest) (*workflow.WorkflowRecord, error)
}

type workflowOwnerScopedRecordReader interface {
	GetForOwner(ownerIdentity string, id uuid.UUID) (*workflow.WorkflowRecord, error)
}

type service struct {
	repo                        Repository
	workflowService             workflowIntakeService
	portfolioCapacity           PortfolioCapacityReader
	portfolioCapacityNow        func() time.Time
	portfolioWorkflowAuthorizer portfolioWorkflowEffectAuthorizer
	portfolioWorkflowExecutor   portfolioWorkflowEffectExecutor
	controlledLearning          ControlledLearningRecorder
	portfolioCalibration        PortfolioCalibrationReader
	acceptedPlanResolver        plangraph.AcceptedRevisionResolver
}

func NewService(repo Repository, workflowService workflowIntakeService) Service {
	return &service{repo: repo, workflowService: workflowService}
}

func DefaultService() Service {
	return NewService(DefaultRepository(), workflow.DefaultService())
}

func (s *service) Create(request CreateRequest) (*models.Pursuit, error) {
	title := strings.TrimSpace(request.Title)
	if title == "" {
		return nil, fmt.Errorf("title is required")
	}
	mandateID, err := parseOptionalStandingMandateID(request.MandateID)
	if err != nil {
		return nil, err
	}
	successCriteria, stopConditions, dependencies, resourceLimits, targetAt, reviewCadenceDays, err := normalizePursuitGoalContract(
		request.SuccessCriteria,
		request.StopConditions,
		request.Dependencies,
		request.ResourceLimits,
		request.TargetAt,
		request.ReviewCadenceDays,
		firstNonEmpty(request.CompletionDefinition, request.DesiredOutcome, title),
		true,
	)
	if err != nil {
		return nil, err
	}
	contextText := title + " " + request.Description + " " + request.WhyItMatters + " " + request.DesiredOutcome
	riskLevel := conservativeRisk(request.RiskLevel, classifyRisk(contextText))
	autonomyLevel := conservativeAutonomy(request.AutonomyLevel, riskLevel)
	now := time.Now().UTC()
	nextReviewAt := parseOptionalTime(request.NextReviewAt)
	if nextReviewAt == nil && reviewCadenceDays > 0 {
		next := now.AddDate(0, 0, reviewCadenceDays)
		nextReviewAt = &next
	}
	pursuit := &models.Pursuit{
		OwnerIdentity:         strings.TrimSpace(request.OwnerIdentity),
		Title:                 title,
		Description:           strings.TrimSpace(request.Description),
		WhyItMatters:          strings.TrimSpace(request.WhyItMatters),
		ProjectKey:            strings.TrimSpace(request.ProjectKey),
		MandateID:             mandateID,
		Domain:                firstNonEmpty(request.Domain, classifyDomain(contextText)),
		DesiredOutcome:        strings.TrimSpace(request.DesiredOutcome),
		CurrentStateSummary:   strings.TrimSpace(request.CurrentStateSummary),
		Status:                firstNonEmpty(request.Status, StatusActive),
		PriorityScore:         clampInt(request.PriorityScore, 0, 100, 50),
		RiskLevel:             riskLevel,
		Confidence:            normalizeConfidence(request.Confidence, 0.7),
		AutonomyLevel:         autonomyLevel,
		NeedCategory:          firstNonEmpty(request.NeedCategory, classifyNeed(contextText)),
		SourceOfCreation:      firstNonEmpty(request.SourceOfCreation, "manual"),
		NextRecommendedAction: strings.TrimSpace(request.NextRecommendedAction),
		CompletionDefinition:  strings.TrimSpace(request.CompletionDefinition),
		SuccessCriteria:       successCriteria,
		StopConditions:        stopConditions,
		Dependencies:          dependencies,
		ResourceLimits:        resourceLimits,
		CompletionState:       CompletionOpen,
		LastActivityAt:        &now,
		NextReviewAt:          nextReviewAt,
		TargetAt:              targetAt,
		ReviewCadenceDays:     reviewCadenceDays,
	}
	if pursuit.CurrentStateSummary == "" {
		pursuit.CurrentStateSummary = "New pursuit created. HAI should gather context, link operational work, and propose the next concrete action."
	}
	if pursuit.NextRecommendedAction == "" {
		pursuit.NextRecommendedAction = "Define the first workflow item and evidence needed for this pursuit."
	}
	created, err := s.repo.Create(pursuit)
	if err != nil {
		return nil, err
	}
	_, _ = s.recordActivity(created.ID, "pursuit.created", "Pursuit created: "+created.Title, firstNonEmpty(request.Actor, "operator"), "", "", "")
	if policyWasNormalized(request.RiskLevel, request.AutonomyLevel, riskLevel, autonomyLevel) {
		_, _ = s.recordActivity(created.ID, "pursuit.safety_normalized", "Pursuit safety policy normalized from the goal context: "+riskLevel+" risk / "+autonomyLevel+" autonomy", firstNonEmpty(request.Actor, "operator"), "", "", "")
	}
	return created, nil
}

func (s *service) Update(id uuid.UUID, request UpdateRequest) (*models.Pursuit, error) {
	return s.UpdateForOwner("", id, request)
}

func (s *service) UpdateForOwner(ownerIdentity string, id uuid.UUID, request UpdateRequest) (*models.Pursuit, error) {
	pursuit, err := s.repo.FindByID(id)
	if err != nil {
		return nil, err
	}
	if !pursuitMutableBy(*pursuit, ownerIdentity) {
		return nil, fmt.Errorf("pursuit not found")
	}
	if updateAttemptsReopen(*pursuit, request) {
		return nil, fmt.Errorf("cannot reopen a closed pursuit through a generic update; use the explicit reopen action")
	}
	if requestsVerifiedCompletion(*pursuit, request) {
		if isPursuitCandidate(*pursuit) {
			return nil, fmt.Errorf("pursuit candidate must be accepted and planned before it can be marked complete")
		}
		if reason, err := s.completionActiveBlockerReasonForOwner(ownerIdentity, id); err != nil {
			return nil, err
		} else if reason != "" {
			return nil, fmt.Errorf("pursuit completion is blocked by unresolved operational work: %s", reason)
		}
		allowed, reason, err := s.completionEvidenceAvailableForOwner(ownerIdentity, id)
		if err != nil {
			return nil, err
		}
		if !allowed {
			return nil, fmt.Errorf("pursuit completion requires verified evidence, linked verification, or a verified completed workflow before it can be marked complete: %s", reason)
		}
		satisfyImplicitSuccessCriteria(pursuit)
	}
	priorRiskLevel := pursuit.RiskLevel
	priorAutonomyLevel := pursuit.AutonomyLevel
	if strings.TrimSpace(request.Title) != "" {
		pursuit.Title = strings.TrimSpace(request.Title)
	}
	assignString(request.Description, &pursuit.Description)
	assignString(request.WhyItMatters, &pursuit.WhyItMatters)
	assignString(request.ProjectKey, &pursuit.ProjectKey)
	assignString(request.Domain, &pursuit.Domain)
	assignString(request.DesiredOutcome, &pursuit.DesiredOutcome)
	assignString(request.CurrentStateSummary, &pursuit.CurrentStateSummary)
	assignString(request.NeedCategory, &pursuit.NeedCategory)
	assignString(request.NextRecommendedAction, &pursuit.NextRecommendedAction)
	assignString(request.CompletionDefinition, &pursuit.CompletionDefinition)
	if request.SuccessCriteria != nil {
		criteria, err := normalizeSuccessCriteria(*request.SuccessCriteria, "", false)
		if err != nil {
			return nil, err
		}
		pursuit.SuccessCriteria = criteria
	}
	if request.StopConditions != nil {
		conditions, err := normalizeStopConditions(*request.StopConditions, false)
		if err != nil {
			return nil, err
		}
		pursuit.StopConditions = conditions
	}
	if request.Dependencies != nil {
		dependencies, err := normalizeDependencies(*request.Dependencies)
		if err != nil {
			return nil, err
		}
		pursuit.Dependencies = dependencies
	}
	if request.ResourceLimits != nil {
		limits, err := normalizeResourceLimits(*request.ResourceLimits)
		if err != nil {
			return nil, err
		}
		pursuit.ResourceLimits = limits
	}
	if request.Status != "" {
		pursuit.Status = strings.TrimSpace(request.Status)
	}
	contextText := pursuit.Title + " " + pursuit.Description + " " + pursuit.WhyItMatters + " " + pursuit.DesiredOutcome
	pursuit.RiskLevel = conservativeRisk(firstNonEmpty(request.RiskLevel, pursuit.RiskLevel), classifyRisk(contextText))
	pursuit.AutonomyLevel = conservativeAutonomy(firstNonEmpty(request.AutonomyLevel, pursuit.AutonomyLevel), pursuit.RiskLevel)
	if request.CompletionState != "" {
		pursuit.CompletionState = strings.TrimSpace(request.CompletionState)
	}
	if strings.EqualFold(strings.TrimSpace(request.Status), StatusCompleted) && strings.TrimSpace(request.CompletionState) == "" {
		pursuit.CompletionState = CompletionVerified
	}
	if request.PriorityScore != nil {
		pursuit.PriorityScore = clampInt(*request.PriorityScore, 0, 100, pursuit.PriorityScore)
	}
	if request.Confidence != nil {
		pursuit.Confidence = normalizeConfidence(*request.Confidence, pursuit.Confidence)
	}
	if request.NextReviewAt != nil {
		pursuit.NextReviewAt = parseOptionalTime(*request.NextReviewAt)
	}
	if request.TargetAt != nil {
		targetAt, err := parseOptionalStrictTime(*request.TargetAt, "targetAt")
		if err != nil {
			return nil, err
		}
		pursuit.TargetAt = targetAt
	}
	if request.ReviewCadenceDays != nil {
		if *request.ReviewCadenceDays < 0 || *request.ReviewCadenceDays > 3650 {
			return nil, fmt.Errorf("reviewCadenceDays must be between 0 and 3650")
		}
		pursuit.ReviewCadenceDays = *request.ReviewCadenceDays
		if request.NextReviewAt == nil && *request.ReviewCadenceDays > 0 {
			next := time.Now().UTC().AddDate(0, 0, *request.ReviewCadenceDays)
			pursuit.NextReviewAt = &next
		}
	}
	if request.Archived != nil {
		pursuit.Archived = *request.Archived
		if *request.Archived {
			pursuit.Status = StatusArchived
		} else if pursuit.Status == StatusArchived {
			pursuit.Status = StatusActive
		}
	}
	now := time.Now().UTC()
	pursuit.LastActivityAt = &now
	updated, err := s.repo.Update(pursuit)
	if err != nil {
		return nil, err
	}
	_, _ = s.recordActivity(id, "pursuit.updated", "Pursuit details updated", firstNonEmpty(request.Actor, "operator"), "", "", "")
	if !strings.EqualFold(priorRiskLevel, updated.RiskLevel) || !strings.EqualFold(priorAutonomyLevel, updated.AutonomyLevel) {
		_, _ = s.recordActivity(id, "pursuit.safety_normalized", "Pursuit safety policy recalculated from the current goal context: "+updated.RiskLevel+" risk / "+updated.AutonomyLevel+" autonomy", firstNonEmpty(request.Actor, "operator"), "", "", "")
	}
	return updated, nil
}

func (s *service) Archive(id uuid.UUID, archived bool, actor string) (*models.Pursuit, error) {
	return s.ArchiveForOwner("", id, archived, actor)
}

func (s *service) ArchiveForOwner(ownerIdentity string, id uuid.UUID, archived bool, actor string) (*models.Pursuit, error) {
	if !archived {
		return s.ReopenForOwner(ownerIdentity, id, actor, "")
	}
	return s.UpdateForOwner(ownerIdentity, id, UpdateRequest{Archived: &archived, Actor: actor})
}

// Reopen is the explicit, auditable transition from a completed or archived
// pursuit back to active work. It never creates a workflow; subsequent intake
// still flows through the normal approval and verification controls.
func (s *service) Reopen(id uuid.UUID, actor, note string) (*models.Pursuit, error) {
	return s.ReopenForOwner("", id, actor, note)
}

func (s *service) ReopenForOwner(ownerIdentity string, id uuid.UUID, actor, note string) (*models.Pursuit, error) {
	pursuit, err := s.repo.FindByID(id)
	if err != nil {
		return nil, err
	}
	if !pursuitMutableBy(*pursuit, ownerIdentity) {
		return nil, fmt.Errorf("pursuit not found")
	}
	if !pursuitClosed(*pursuit) {
		return pursuit, nil
	}
	now := time.Now().UTC()
	pursuit.Archived = false
	pursuit.Status = StatusActive
	pursuit.CompletionState = CompletionOpen
	pursuit.LastActivityAt = &now
	pursuit.CurrentStateSummary = firstNonEmpty(strings.TrimSpace(note), "Pursuit reopened for further governed work. Review the prior evidence and define the next concrete action.")
	pursuit.NextRecommendedAction = "Review the previous closure evidence and add the next governed workflow item if more work is needed."
	updated, err := s.repo.Update(pursuit)
	if err != nil {
		return nil, err
	}
	_, _ = s.recordActivity(id, "pursuit.reopened", firstNonEmpty(strings.TrimSpace(note), "Pursuit reopened for further governed work."), firstNonEmpty(actor, "operator"), "", "", "")
	return updated, nil
}

func (s *service) List(includeArchived bool) ([]models.Pursuit, error) {
	return s.ListForOwner("", includeArchived)
}

func (s *service) Dashboard() (*Dashboard, error) {
	return s.DashboardForOwner("")
}

// ListForOwner scopes authenticated users to records they own. Only the
// explicitly configured migration owner can additionally inspect ownerless
// records created before identity-aware pursuit ownership existed.
func (s *service) ListForOwner(ownerIdentity string, includeArchived bool) ([]models.Pursuit, error) {
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	var (
		pursuits []models.Pursuit
		err      error
	)
	if scopedRepo, ok := s.repo.(ownerScopedRepository); ok && ownerIdentity != "" {
		pursuits, err = scopedRepo.FindAllForOwner(ownerIdentity, includeArchived)
	} else {
		pursuits, err = s.repo.FindAll(includeArchived)
	}
	if err != nil {
		return nil, err
	}
	visible := make([]models.Pursuit, 0, len(pursuits))
	for _, pursuit := range pursuits {
		if pursuitVisibleTo(pursuit, ownerIdentity) {
			visible = append(visible, pursuit)
		}
	}
	return visible, nil
}

func pursuitVisibleTo(pursuit models.Pursuit, ownerIdentity string) bool {
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	if ownerIdentity == "" {
		return true
	}
	recordOwner := strings.TrimSpace(pursuit.OwnerIdentity)
	return recordOwner == ownerIdentity || (recordOwner == "" && identity.CanReadLegacyOwnerlessData(ownerIdentity))
}

// pursuitMutableBy is intentionally stricter than read visibility. Ownerless
// legacy pursuits remain inspectable during local migration, but authenticated
// users must not adopt or change them. Empty owner identity is reserved for
// controlled in-process system work.
func pursuitMutableBy(pursuit models.Pursuit, ownerIdentity string) bool {
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	if ownerIdentity == "" {
		return true
	}
	return strings.TrimSpace(pursuit.OwnerIdentity) == ownerIdentity
}

func (s *service) DashboardForOwner(ownerIdentity string) (*Dashboard, error) {
	pursuits, err := s.ListForOwner(ownerIdentity, false)
	if err != nil {
		return nil, err
	}
	dashboard := &Dashboard{
		Counts: map[string]int64{
			"active": 0, "waiting": 0, "blocked": 0, "completed": 0, "needsRobert": 0, "decisionQueue": 0, "stale": 0, "reviewDue": 0, "planningNeeded": 0, "highRisk": 0, "completionCandidates": 0,
		},
		DecisionQueue:        []PursuitDashboardDecision{},
		NeedsRobert:          []PursuitListItem{},
		VAReady:              []PursuitListItem{},
		SystemReady:          []PursuitListItem{},
		Blocked:              []PursuitListItem{},
		Stale:                []PursuitListItem{},
		ReviewDue:            []PursuitListItem{},
		PlanningNeeded:       []PursuitListItem{},
		RecentlyChanged:      []PursuitListItem{},
		HighRisk:             []PursuitListItem{},
		CompletionCandidates: []PursuitListItem{},
	}
	bulkDetails, bulkAvailable, bulkErr := s.dashboardDetailsForOwner(ownerIdentity, pursuits)
	if bulkErr != nil {
		// A projection failure must not make the operator dashboard disappear.
		// The established single-pursuit loader remains the fail-safe path.
		bulkAvailable = false
	}
	for _, pursuit := range pursuits {
		if pursuitClosed(pursuit) {
			dashboard.Counts["completed"]++
			continue
		}
		var item PursuitListItem
		var detail *PursuitDetail
		var detailErr error
		if bulkAvailable {
			detail = bulkDetails[pursuit.ID]
			if detail == nil {
				detailErr = dashboardProjectionMissing(pursuit.ID)
			}
			item, detail, detailErr = pursuitListItemWithDetail(pursuit, detail, detailErr)
		} else {
			item, detail, detailErr = s.listItemWithDetailForOwner(ownerIdentity, pursuit)
		}
		if detailErr != nil {
			item = detailUnavailableListItem(pursuit)
		}
		switch dashboardStatusBucket(pursuit, item) {
		case StatusWaiting:
			dashboard.Counts["waiting"]++
		case StatusBlocked:
			dashboard.Counts["blocked"]++
		default:
			dashboard.Counts["active"]++
		}
		if item.NeedsRobert > 0 {
			dashboard.NeedsRobert = append(dashboard.NeedsRobert, item)
			dashboard.Counts["needsRobert"]++
		}
		if detail != nil {
			dashboard.DecisionQueue = append(dashboard.DecisionQueue, dashboardDecisionCards(item, detail)...)
		}
		if item.Blocked > 0 || pursuit.Status == StatusBlocked {
			dashboard.Blocked = append(dashboard.Blocked, item)
		}
		if item.Stale {
			dashboard.Stale = append(dashboard.Stale, item)
			dashboard.Counts["stale"]++
		}
		if item.ReviewDue {
			dashboard.ReviewDue = append(dashboard.ReviewDue, item)
			dashboard.Counts["reviewDue"]++
		}
		if item.PlanningNeeded {
			dashboard.PlanningNeeded = append(dashboard.PlanningNeeded, item)
			dashboard.Counts["planningNeeded"]++
		}
		if strings.EqualFold(pursuit.RiskLevel, "high") {
			dashboard.HighRisk = append(dashboard.HighRisk, item)
			dashboard.Counts["highRisk"]++
		}
		if item.CompletionCandidate {
			dashboard.CompletionCandidates = append(dashboard.CompletionCandidates, item)
			dashboard.Counts["completionCandidates"]++
		}
		if isVAReady(item) {
			dashboard.VAReady = append(dashboard.VAReady, item)
		}
		if isSystemReady(item) {
			dashboard.SystemReady = append(dashboard.SystemReady, item)
		}
		dashboard.RecentlyChanged = append(dashboard.RecentlyChanged, item)
	}
	sortDashboardDecisions(dashboard.DecisionQueue)
	sortListItemsByEffectiveActivity(dashboard.RecentlyChanged)
	dashboard.Counts["decisionQueue"] = int64(len(dashboard.DecisionQueue))
	limitDashboardDecisions(&dashboard.DecisionQueue, 12)
	limitListItems(&dashboard.NeedsRobert, 8)
	limitListItems(&dashboard.VAReady, 8)
	limitListItems(&dashboard.SystemReady, 8)
	limitListItems(&dashboard.Blocked, 8)
	limitListItems(&dashboard.Stale, 8)
	limitListItems(&dashboard.ReviewDue, 8)
	limitListItems(&dashboard.PlanningNeeded, 8)
	limitListItems(&dashboard.HighRisk, 8)
	limitListItems(&dashboard.CompletionCandidates, 8)
	limitListItems(&dashboard.RecentlyChanged, 12)
	return dashboard, nil
}

func (s *service) Decisions() ([]PursuitDashboardDecision, error) {
	return s.DecisionsForOwner("")
}

func (s *service) DecisionsForOwner(ownerIdentity string) ([]PursuitDashboardDecision, error) {
	snapshot, err := s.OperatingSnapshotForOwner(ownerIdentity)
	if err != nil {
		return nil, err
	}
	decisions := make([]PursuitDashboardDecision, len(snapshot.Decisions))
	copy(decisions, snapshot.Decisions)
	return decisions, nil
}

func dashboardStatusBucket(pursuit models.Pursuit, item PursuitListItem) string {
	if item.Blocked > 0 || pursuit.Status == StatusBlocked {
		return StatusBlocked
	}
	if pursuit.Status == StatusWaiting {
		return StatusWaiting
	}
	return StatusActive
}

func (s *service) Brief() (*Brief, error) {
	return s.BriefForOwner("")
}

func (s *service) BriefForOwner(ownerIdentity string) (*Brief, error) {
	snapshot, err := s.OperatingSnapshotForOwner(ownerIdentity)
	if err != nil {
		return nil, err
	}
	return snapshot.Brief, nil
}

func (s *service) OperatingSnapshot() (*OperatingSnapshot, error) {
	return s.OperatingSnapshotForOwner("")
}

func (s *service) OperatingSnapshotForOwner(ownerIdentity string) (*OperatingSnapshot, error) {
	dashboard, err := s.DashboardForOwner(ownerIdentity)
	if err != nil {
		return nil, err
	}
	decisions := make([]PursuitDashboardDecision, len(dashboard.DecisionQueue))
	copy(decisions, dashboard.DecisionQueue)
	return &OperatingSnapshot{
		Brief:     briefFromDashboard(dashboard),
		Decisions: decisions,
		Dashboard: dashboard,
	}, nil
}

func briefFromDashboard(dashboard *Dashboard) *Brief {
	brief := &Brief{
		GeneratedAt:          time.Now().UTC(),
		NeedsRobert:          len(dashboard.NeedsRobert),
		ReadyToMove:          len(dashboard.VAReady) + len(dashboard.SystemReady),
		Stuck:                len(dashboard.Blocked) + len(dashboard.Stale),
		ReviewDue:            len(dashboard.ReviewDue),
		PlanningNeeded:       len(dashboard.PlanningNeeded),
		CompletionCandidates: len(dashboard.CompletionCandidates),
		RecentlyChanged:      len(dashboard.RecentlyChanged),
	}
	brief.OperatingMode = briefOperatingMode(*brief)
	brief.Summary = briefSummary(*brief, dashboard)
	brief.PrimaryAction = briefPrimaryAction(*brief)
	brief.Cards = briefCards(dashboard, 6)
	return brief
}

// UpsertTaskAttempt records the durable, compact task-engine projection for a
// pursuit-scoped direct plan or run. It deliberately does not create a
// workflow: workflow-owned execution already persists on WorkflowItem and is
// aggregated separately in pursuit detail.
func (s *service) UpsertTaskAttempt(attempt models.PursuitTaskAttempt) error {
	if attempt.PursuitID == uuid.Nil || strings.TrimSpace(attempt.TaskPlanID) == "" {
		return fmt.Errorf("pursuit id and task plan id are required")
	}
	pursuit, err := s.taskAttemptPursuit(attempt.PursuitID, attempt.OwnerIdentity)
	if err != nil {
		return err
	}
	attempt.OwnerIdentity = firstNonEmpty(strings.TrimSpace(attempt.OwnerIdentity), pursuit.OwnerIdentity)
	attempt.RequestSummary = compactPursuitTaskSummary(attempt.RequestSummary)
	attempt.Mode = firstNonEmpty(strings.TrimSpace(attempt.Mode), "plan")
	attempt.Status = firstNonEmpty(strings.TrimSpace(attempt.Status), "planned")
	attempt.RiskLevel = firstNonEmpty(strings.TrimSpace(attempt.RiskLevel), pursuit.RiskLevel, "low")
	attempt.BlockedReason = compactPursuitTaskSummary(attempt.BlockedReason)
	stored, err := s.repo.UpsertTaskAttempt(&attempt)
	if err != nil {
		return err
	}
	if err := s.linkTaskAttemptRuntimeEvidence(*stored); err != nil {
		return err
	}
	eventType, message := pursuitTaskAttemptActivity(*stored)
	_, err = s.recordActivity(stored.PursuitID, eventType, message, firstNonEmpty(stored.OwnerIdentity, "task-engine"), "task_attempt", stored.TaskPlanID, "task://"+stored.TaskPlanID)
	return err
}

// ValidatePursuitTaskAttempt is used by the direct task engine before it
// builds a pursuit-scoped plan. It prevents an unaccepted candidate or closed
// pursuit from receiving task work through APIs that do not create workflows.
func (s *service) ValidatePursuitTaskAttempt(pursuitID uuid.UUID, ownerIdentity string) error {
	pursuit, err := s.taskAttemptPursuit(pursuitID, ownerIdentity)
	if err != nil {
		return err
	}
	detail, err := s.DetailForOwner(ownerIdentity, pursuitID)
	if err != nil {
		return err
	}
	if reason := s.newWorkBlockerReasonForOwner(ownerIdentity, *pursuit, detail.Workflows); reason != "" {
		return fmt.Errorf("pursuit cannot accept direct task work: %s", reason)
	}
	return nil
}

func (s *service) taskAttemptPursuit(pursuitID uuid.UUID, ownerIdentity string) (*models.Pursuit, error) {
	pursuit, err := s.repo.FindByID(pursuitID)
	if err != nil {
		return nil, err
	}
	if !pursuitMutableBy(*pursuit, ownerIdentity) {
		return nil, fmt.Errorf("pursuit not found")
	}
	if isPursuitCandidate(*pursuit) {
		return nil, fmt.Errorf("pursuit candidate must be accepted before direct task planning or execution")
	}
	if err := ensurePursuitOpen(*pursuit, "plan or execute direct task work for"); err != nil {
		return nil, err
	}
	return pursuit, nil
}

// linkTaskAttemptRuntimeEvidence adds only the exact persisted launch event to
// the pursuit. Linking the automation itself would make historical and future
// launches of that shared capability look like evidence for this task.
func (s *service) linkTaskAttemptRuntimeEvidence(attempt models.PursuitTaskAttempt) error {
	launchID, err := uuid.Parse(strings.TrimSpace(attempt.LaunchEventID))
	if err != nil {
		return nil
	}
	launches, err := s.repo.FindLinkedAutomationLaunches(nil, []uuid.UUID{launchID}, 1)
	if err != nil {
		return err
	}
	launches = runtimeAttemptsVisibleToOwner(attempt.OwnerIdentity, launches)
	if len(launches) != 1 || launches[0].ID != launchID {
		return fmt.Errorf("runtime launch evidence is not available")
	}
	launch := launches[0]
	if automationID, err := uuid.Parse(strings.TrimSpace(attempt.AutomationID)); err == nil && launch.AutomationID != uuid.Nil && launch.AutomationID != automationID {
		return fmt.Errorf("runtime launch evidence does not match the task automation")
	}
	_, err = s.Link(attempt.PursuitID, LinkRequest{
		OwnerIdentity: attempt.OwnerIdentity,
		LinkType:      LinkAgentRuntime,
		LinkID:        launchID.String(),
		Relationship:  "execution_attempt",
		SourceURI:     "automation-launch://" + launchID.String(),
		SourceLabel:   firstNonEmpty(strings.TrimSpace(launch.RuntimeType), "controlled runtime") + " task evidence",
		Confidence:    1,
		Actor:         "task-engine",
	})
	return err
}

func compactPursuitTaskSummary(value string) string {
	value = strings.Join(strings.Fields(strings.TrimSpace(safety.RedactSecrets(value))), " ")
	const limit = 500
	if len([]rune(value)) <= limit {
		return value
	}
	return string([]rune(value)[:limit]) + "..."
}

func pursuitTaskAttemptActivity(attempt models.PursuitTaskAttempt) (string, string) {
	mode := firstNonEmpty(attempt.Mode, "task")
	if attempt.CompletedAt == nil {
		return "pursuit.task_attempt_started", "Direct " + mode + " task attempt started."
	}
	if attempt.Status == "validated" {
		return "pursuit.task_attempt_validated", "Direct " + mode + " task attempt completed with verified output."
	}
	if strings.Contains(attempt.Status, "review") || strings.TrimSpace(attempt.BlockedReason) != "" {
		return "pursuit.task_attempt_review_required", "Direct " + mode + " task attempt requires review: " + firstNonEmpty(attempt.BlockedReason, attempt.Status)
	}
	return "pursuit.task_attempt_recorded", "Direct " + mode + " task attempt recorded with status " + attempt.Status + "."
}

func (s *service) Detail(id uuid.UUID) (*PursuitDetail, error) {
	return s.DetailForOwner("", id)
}

func (s *service) DetailForOwner(ownerIdentity string, id uuid.UUID) (*PursuitDetail, error) {
	pursuit, err := s.repo.FindByID(id)
	if err != nil {
		return nil, err
	}
	if !pursuitVisibleTo(*pursuit, ownerIdentity) {
		return nil, fmt.Errorf("pursuit not found")
	}
	links, err := s.repo.FindLinks(id)
	if err != nil {
		return nil, err
	}
	links, err = s.visibleLinksForOwner(ownerIdentity, links)
	if err != nil {
		return nil, err
	}
	activity, err := s.repo.FindActivities(id, 50)
	if err != nil {
		return nil, pursuitDetailLoadError("activity", err)
	}
	taskAttempts, err := s.repo.FindTaskAttempts(id, 20)
	if err != nil {
		return nil, pursuitDetailLoadError("task attempts", err)
	}
	taskAttempts = taskAttemptsVisibleToOwner(ownerIdentity, taskAttempts)
	if taskAttempts == nil {
		taskAttempts = []models.PursuitTaskAttempt{}
	}
	workflowIDs := linkUUIDs(links, LinkWorkflow)
	memoryIDs := linkUUIDs(links, LinkMemory)
	conversationIDs := linkUUIDs(links, LinkAIConversation)
	ambientOpportunityIDs := linkUUIDs(links, LinkAmbientOpportunity)
	sourceItemIDs := linkUUIDs(links, LinkSourceItem)
	extractionIDs := linkUUIDs(links, LinkSourceExtraction)
	verificationIDs := linkUUIDs(links, LinkVerification)
	linkedAutomationIDs := linkUUIDs(links, LinkAutomation)
	linkedRuntimeAttemptIDs := linkUUIDs(links, LinkAgentRuntime)
	workflows, err := s.repo.FindLinkedWorkflows(workflowIDs)
	if err != nil {
		return nil, pursuitDetailLoadError("linked workflows", err)
	}
	checklistItems, err := s.repo.FindLinkedChecklistItems(workflowIDs)
	if err != nil {
		return nil, pursuitDetailLoadError("linked checklist items", err)
	}
	automationIDs := uniqueUUIDs(append(linkedAutomationIDs, workflowAutomationIDs(workflows)...))
	openLoops, err := s.repo.FindLinkedOpenLoops(workflowIDs)
	if err != nil {
		return nil, pursuitDetailLoadError("linked open loops", err)
	}
	proposals, err := s.repo.FindLinkedProposals(workflowIDs)
	if err != nil {
		return nil, pursuitDetailLoadError("linked proposals", err)
	}
	qualityGates, err := s.repo.FindLinkedQualityGates(workflowIDs)
	if err != nil {
		return nil, pursuitDetailLoadError("linked quality gates", err)
	}
	decisions, err := s.repo.FindLinkedDecisions(workflowIDs)
	if err != nil {
		return nil, pursuitDetailLoadError("linked decisions", err)
	}
	transitions, err := s.repo.FindLinkedTransitions(workflowIDs)
	if err != nil {
		return nil, pursuitDetailLoadError("linked transitions", err)
	}
	sourceLinks, err := s.repo.FindLinkedSourceLinks(workflowIDs)
	if err != nil {
		return nil, pursuitDetailLoadError("linked source links", err)
	}
	events, err := s.repo.FindLinkedEvents(workflowIDs)
	if err != nil {
		return nil, pursuitDetailLoadError("linked events", err)
	}
	evidence, err := s.repo.FindLinkedEvidence(workflowIDs)
	if err != nil {
		return nil, pursuitDetailLoadError("linked evidence", err)
	}
	memories, err := s.repo.FindLinkedMemories(memoryIDs)
	if err != nil {
		return nil, pursuitDetailLoadError("linked memories", err)
	}
	conversations, err := s.repo.FindLinkedConversations(conversationIDs)
	if err != nil {
		return nil, pursuitDetailLoadError("linked conversations", err)
	}
	if conversations == nil {
		conversations = []models.AIConversationArchive{}
	}
	ambientOpportunities, err := s.repo.FindLinkedAmbientOpportunities(ambientOpportunityIDs)
	if err != nil {
		return nil, pursuitDetailLoadError("linked ambient opportunities", err)
	}
	if ambientOpportunities == nil {
		ambientOpportunities = []models.AmbientOpportunity{}
	}
	sourceItems, err := s.repo.FindLinkedSourceItems(sourceItemIDs)
	if err != nil {
		return nil, pursuitDetailLoadError("linked source items", err)
	}
	extractions, err := s.repo.FindLinkedExtractions(extractionIDs)
	if err != nil {
		return nil, pursuitDetailLoadError("linked source extractions", err)
	}
	verificationRuns, err := s.repo.FindLinkedVerificationRuns(verificationIDs)
	if err != nil {
		return nil, pursuitDetailLoadError("linked verification runs", err)
	}
	runIDs := verificationRunIDs(verificationRuns)
	verificationClaims, err := s.repo.FindLinkedVerificationClaims(runIDs)
	if err != nil {
		return nil, pursuitDetailLoadError("linked verification claims", err)
	}
	verificationEvidence, err := s.repo.FindLinkedVerificationEvidence(runIDs)
	if err != nil {
		return nil, pursuitDetailLoadError("linked verification evidence", err)
	}
	automations, err := s.repo.FindLinkedAutomations(automationIDs)
	if err != nil {
		return nil, pursuitDetailLoadError("linked automations", err)
	}
	runtimeAttempts, err := s.repo.FindLinkedAutomationLaunches(automationIDs, linkedRuntimeAttemptIDs, 20)
	if err != nil {
		return nil, pursuitDetailLoadError("linked runtime attempts", err)
	}
	runtimeAttempts = runtimeAttemptsVisibleToOwner(ownerIdentity, runtimeAttempts)
	if runtimeAttempts == nil {
		runtimeAttempts = []models.AutomationLaunchEvent{}
	}
	resourceUsage := s.resourceUsageForPursuit(ownerIdentity, *pursuit)
	return s.buildPursuitDetail(*pursuit, pursuitDetailRecords{
		Links: links, Activity: activity, Workflows: workflows,
		ChecklistItems: checklistItems, OpenLoops: openLoops, Proposals: proposals,
		QualityGates: qualityGates, Decisions: decisions, Transitions: transitions,
		SourceLinks: sourceLinks, Events: events, Evidence: evidence, Memories: memories,
		Conversations: conversations, AmbientOpportunities: ambientOpportunities,
		TaskAttempts: taskAttempts, VerificationRuns: verificationRuns,
		VerificationClaims: verificationClaims, VerificationEvidence: verificationEvidence,
		Automations: automations, RuntimeAttempts: runtimeAttempts, SourceItems: sourceItems,
		SourceExtractions: extractions, ResourceUsage: resourceUsage,
	}), nil
}

func (s *service) buildPursuitDetail(pursuit models.Pursuit, records pursuitDetailRecords) *PursuitDetail {
	resolvedDecisions := resolvedPursuitDecisions(records.Activity)
	sourceBlockers := sourceRetractionBlockers(records.Links, records.SourceExtractions)
	qualityReviewBlockers := qualityGateBlockers(records.QualityGates)
	contractBlockers := pursuitGoalContractBlockers(pursuit)
	contractBlockers = append(contractBlockers, pursuitResourceBlockers(pursuit, records.ResourceUsage)...)

	detail := &PursuitDetail{
		Pursuit: pursuit, Links: records.Links, Activity: records.Activity,
		Workflows: records.Workflows, ChecklistItems: records.ChecklistItems,
		OpenLoops: records.OpenLoops, Proposals: records.Proposals,
		QualityGates: records.QualityGates, Decisions: records.Decisions,
		Transitions: records.Transitions, SourceLinks: records.SourceLinks,
		Events: records.Events, Evidence: records.Evidence, Memories: records.Memories,
		Conversations:        compactConversations(records.Conversations),
		AmbientOpportunities: compactAmbientOpportunities(records.AmbientOpportunities),
		TaskRuns:             taskRunsFromWorkflows(records.Workflows), TaskAttempts: records.TaskAttempts,
		VerificationRuns: records.VerificationRuns, VerificationClaims: records.VerificationClaims,
		VerificationEvidence: records.VerificationEvidence,
		Automations:          compactAutomations(records.Automations), RuntimeAttempts: records.RuntimeAttempts,
		SourceItems: compactSourceItems(records.SourceItems), SourceExtractions: records.SourceExtractions,
		ResourceUsage: records.ResourceUsage,
	}
	detail.ApprovalItems = approvalWorkflows(records.Workflows)
	detail.DecisionQueue = decisionQueue(pursuit, records.Workflows, records.Proposals, records.Decisions, records.RuntimeAttempts, resolvedDecisions)
	detail.Timeline = pursuitTimeline(pursuit, records.Activity, records.Workflows, records.Transitions, records.SourceLinks, records.Decisions, records.Events, detail.TaskRuns, records.TaskAttempts, records.VerificationRuns, records.RuntimeAttempts)
	detail.Blockers = append(blockers(records.Workflows, records.OpenLoops), runtimeAttemptBlockers(records.RuntimeAttempts, records.Workflows, resolvedDecisions)...)
	detail.Blockers = append(detail.Blockers, sourceBlockers...)
	detail.Blockers = append(detail.Blockers, qualityReviewBlockers...)
	detail.Blockers = append(detail.Blockers, contractBlockers...)
	detail.NextActions = nextActions(pursuit, records.Workflows, records.OpenLoops, records.Proposals, records.RuntimeAttempts, resolvedDecisions, len(qualityReviewBlockers) > 0, len(contractBlockers) > 0)
	detail.ActionQueues = actionQueues(pursuit, detail.NextActions, detail.Blockers)
	detail.Summary = summarize(pursuit, records.Links, records.Workflows, records.OpenLoops, records.Evidence, records.Memories, detail.SourceItems, records.SourceExtractions, detail.TaskRuns, records.TaskAttempts, records.VerificationRuns, records.RuntimeAttempts, records.Activity, sourceBlockers, qualityReviewBlockers, contractBlockers)
	detail.Summary.QualityGatesNeedingReview = len(qualityReviewBlockers)
	if len(detail.Timeline) > 0 {
		detail.Summary.WhatChanged = timelineChangeSummary(detail.Timeline[0])
	}
	if pending := pendingDecisionCards(detail.DecisionQueue); pending > detail.Summary.NeedsRobert {
		detail.Summary.NeedsRobert = pending
	}
	if queueNeedsRobert := len(detail.ActionQueues.NeedsRobert); queueNeedsRobert > detail.Summary.NeedsRobert {
		detail.Summary.NeedsRobert = queueNeedsRobert
	}
	if pursuitNeedsRobert(pursuit, detail.NextActions) && detail.Summary.NeedsRobert == 0 {
		detail.Summary.NeedsRobert = 1
	}
	detail.Summary.DecisionCards = len(detail.DecisionQueue)
	detail.Summary.TimelineItems = len(detail.Timeline)
	detail.Summary.RobertActions = len(detail.ActionQueues.NeedsRobert)
	detail.Summary.VAReadyActions = len(detail.ActionQueues.VAReady)
	detail.Summary.SystemReadyActions = len(detail.ActionQueues.SystemReady)
	detail.Summary.WaitingActions = len(detail.ActionQueues.Waiting)
	detail.OperationalDigest = operationalDigest(pursuit, *detail)
	return detail
}

func (s *service) ResolveEvidence(id uuid.UUID, uri string) (*PursuitEvidenceResolution, error) {
	return s.ResolveEvidenceForOwner("", id, uri)
}

func (s *service) ResolveEvidenceForOwner(ownerIdentity string, id uuid.UUID, uri string) (*PursuitEvidenceResolution, error) {
	uri = strings.TrimSpace(uri)
	if uri == "" {
		return nil, fmt.Errorf("evidence uri is required")
	}
	detail, err := s.DetailForOwner(ownerIdentity, id)
	if err != nil {
		return nil, err
	}
	for _, attempt := range detail.RuntimeAttempts {
		evidenceURI := "automation-launch://" + attempt.ID.String()
		if !strings.EqualFold(uri, evidenceURI) {
			continue
		}
		copy := attempt
		return &PursuitEvidenceResolution{
			URI:            evidenceURI,
			Kind:           "runtime_attempt",
			Title:          runtimeTimelineTitle(attempt, runtimeAttemptLabel(attempt)),
			Summary:        firstNonEmpty(attempt.Message, attempt.Target, attempt.Output, "runtime attempt evidence"),
			Status:         attempt.Status,
			SourceType:     "automation_launch",
			SourceID:       attempt.ID.String(),
			SourceLabel:    runtimeAttemptLabel(attempt),
			NeedsReview:    runtimeAttemptNeedsReview(attempt),
			RuntimeAttempt: &copy,
		}, nil
	}
	for _, link := range detail.Links {
		if !strings.EqualFold(strings.TrimSpace(link.SourceURI), uri) {
			continue
		}
		copy := link
		return &PursuitEvidenceResolution{
			URI:         uri,
			Kind:        "pursuit_link",
			Title:       firstNonEmpty(link.SourceLabel, link.Relationship, link.LinkType),
			Summary:     fmt.Sprintf("Linked %s %s as %s.", link.LinkType, link.LinkID, link.Relationship),
			SourceType:  link.LinkType,
			SourceID:    link.LinkID,
			SourceLabel: link.SourceLabel,
			PursuitLink: &copy,
		}, nil
	}
	for _, item := range detail.Timeline {
		if !strings.EqualFold(strings.TrimSpace(item.SourceURI), uri) {
			continue
		}
		copy := item
		return &PursuitEvidenceResolution{
			URI:          uri,
			Kind:         "timeline_item",
			Title:        item.Title,
			Summary:      firstNonEmpty(item.Message, item.WorkflowTitle, item.Kind),
			Status:       item.Status,
			SourceType:   item.Kind,
			SourceID:     item.ID,
			SourceLabel:  item.SourceLabel,
			WorkflowID:   item.WorkflowID,
			NeedsReview:  item.NeedsReview,
			TimelineItem: &copy,
		}, nil
	}
	for _, claim := range detail.Evidence {
		if !strings.EqualFold(strings.TrimSpace(claim.SourceURI), uri) {
			continue
		}
		copy := claim
		return &PursuitEvidenceResolution{
			URI:              uri,
			Kind:             "workflow_evidence",
			Title:            claim.ClaimText,
			Summary:          firstNonEmpty(claim.SourceLabel, claim.Reliability, "workflow evidence claim"),
			Status:           claim.Status,
			SourceType:       "workflow_evidence",
			SourceID:         claim.ID.String(),
			SourceLabel:      claim.SourceLabel,
			WorkflowID:       claim.WorkflowID.String(),
			NeedsReview:      !acceptedCompletionStatus(claim.Status),
			WorkflowEvidence: &copy,
		}, nil
	}
	for _, memory := range detail.Memories {
		if !strings.EqualFold(strings.TrimSpace(memory.SourceURI), uri) {
			continue
		}
		copy := memory
		return &PursuitEvidenceResolution{
			URI:         uri,
			Kind:        "memory",
			Title:       firstNonEmpty(memory.Summary, memory.Kind, "memory"),
			Summary:     firstNonEmpty(memory.Summary, memory.Content),
			SourceType:  "memory",
			SourceID:    memory.ID.String(),
			SourceLabel: firstNonEmpty(memory.SourceLabel, memory.ProjectKey),
			Memory:      &copy,
		}, nil
	}
	for _, item := range detail.SourceItems {
		if !strings.EqualFold(strings.TrimSpace(item.SourceURI), uri) {
			continue
		}
		copy := item
		return &PursuitEvidenceResolution{
			URI:         uri,
			Kind:        "source_item",
			Title:       firstNonEmpty(item.Title, item.ExternalID, "source item"),
			Summary:     item.Metadata,
			SourceType:  item.ItemType,
			SourceID:    item.ID.String(),
			SourceLabel: item.Title,
			SourceItem:  &copy,
		}, nil
	}
	for _, extraction := range detail.SourceExtractions {
		if !strings.EqualFold(strings.TrimSpace(extraction.SourceURI), uri) {
			continue
		}
		copy := extraction
		return &PursuitEvidenceResolution{
			URI:              uri,
			Kind:             "source_extraction",
			Title:            firstNonEmpty(extraction.SourceLabel, extraction.Summary, "source extraction"),
			Summary:          firstNonEmpty(extraction.Summary, extraction.Text),
			Status:           sourceExtractionStatus(extraction),
			SourceType:       extraction.ContentType,
			SourceID:         extraction.ID.String(),
			SourceLabel:      extraction.SourceLabel,
			NeedsReview:      extraction.Archived,
			SourceExtraction: &copy,
		}, nil
	}
	for _, evidence := range detail.VerificationEvidence {
		if !strings.EqualFold(strings.TrimSpace(evidence.SourceURI), uri) {
			continue
		}
		copy := evidence
		return &PursuitEvidenceResolution{
			URI:                  uri,
			Kind:                 "verification_evidence",
			Title:                firstNonEmpty(evidence.SourceLabel, evidence.SourceID, "verification evidence"),
			Summary:              evidence.Snippet,
			SourceType:           evidence.SourceType,
			SourceID:             evidence.ID.String(),
			SourceLabel:          evidence.SourceLabel,
			VerificationEvidence: &copy,
		}, nil
	}
	for _, activity := range detail.Activity {
		if !strings.EqualFold(strings.TrimSpace(activity.SourceURI), uri) {
			continue
		}
		copy := activity
		return &PursuitEvidenceResolution{
			URI:         uri,
			Kind:        "activity",
			Title:       activity.EventType,
			Summary:     activity.Message,
			SourceType:  activity.SourceType,
			SourceID:    activity.SourceID,
			SourceLabel: activity.Actor,
			Activity:    &copy,
		}, nil
	}
	return nil, fmt.Errorf("evidence uri is not linked to this pursuit")
}

func (s *service) Approvals(id uuid.UUID) (*PursuitApprovalOverview, error) {
	return s.ApprovalsForOwner("", id)
}

func (s *service) ApprovalsForOwner(ownerIdentity string, id uuid.UUID) (*PursuitApprovalOverview, error) {
	detail, err := s.DetailForOwner(ownerIdentity, id)
	if err != nil {
		return nil, err
	}
	actions := approvalActions(detail.NextActions)
	overview := &PursuitApprovalOverview{
		Pursuit:       detail.Pursuit,
		DecisionQueue: detail.DecisionQueue,
		ApprovalItems: detail.ApprovalItems,
		Actions:       actions,
		Blockers:      detail.Blockers,
		Summary:       detail.Summary,
		Counts: map[string]int{
			"decisionQueue":    len(detail.DecisionQueue),
			"pendingDecisions": pendingDecisionCards(detail.DecisionQueue),
			"approvalItems":    len(detail.ApprovalItems),
			"actions":          len(actions),
			"blockers":         len(detail.Blockers),
			"needsRobert":      detail.Summary.NeedsRobert,
		},
	}
	return overview, nil
}

func (s *service) DelegationPackage(id uuid.UUID) (*PursuitDelegationPackage, error) {
	return s.DelegationPackageForOwner("", id)
}

func (s *service) DelegationPackageForOwner(ownerIdentity string, id uuid.UUID) (*PursuitDelegationPackage, error) {
	detail, err := s.DetailForOwner(ownerIdentity, id)
	if err != nil {
		return nil, err
	}
	return delegationPackage(*detail), nil
}

func delegationPackage(detail PursuitDetail) *PursuitDelegationPackage {
	pursuit := detail.Pursuit
	packageResult := &PursuitDelegationPackage{
		GeneratedAt:          time.Now().UTC(),
		PursuitID:            pursuit.ID.String(),
		Title:                pursuit.Title,
		Objective:            firstNonEmpty(pursuit.DesiredOutcome, pursuit.Description, pursuit.Title),
		WhyItMatters:         pursuit.WhyItMatters,
		CurrentState:         firstNonEmpty(detail.Summary.CurrentState, pursuit.CurrentStateSummary, "No current state has been recorded yet."),
		CompletionDefinition: pursuit.CompletionDefinition,
		RiskLevel:            firstNonEmpty(pursuit.RiskLevel, "medium"),
		AllowedActions: []string{
			"Review the linked source references and organize the requested evidence.",
			"Prepare drafts, checklists, summaries, and status updates inside the linked workflow.",
			"Record missing information, blockers, and questions for Robert in HAI.",
		},
		BlockedActions: []string{
			"Do not send external messages or make commitments on Robert's behalf.",
			"Do not make legal, government, financial, medical, account, public-posting, or destructive decisions.",
			"Do not delete or move source material outside the reviewed workflow path.",
		},
		DeliveryRequirements: []string{
			"Keep work inside the linked workflow and preserve source references.",
			"Mark only completed checklist steps that are supported by the linked evidence.",
			"Post a concise status update with completed work, remaining blockers, and questions for Robert.",
		},
		OutstandingRobertActions: append([]PursuitAction{}, detail.ActionQueues.NeedsRobert...),
	}

	if len(detail.ActionQueues.VAReady) == 0 {
		packageResult.Status = "not_ready"
		packageResult.Reason = "HAI found no bounded VA-ready action. Resolve Robert approvals, evidence gaps, or blockers first."
	} else {
		packageResult.Ready = true
		packageResult.Status = "ready"
		packageResult.Reason = "The package contains only VA-ready preparation work. It does not authorize external execution."
	}

	byWorkflow := make(map[string]models.WorkflowItem, len(detail.Workflows))
	for _, item := range detail.Workflows {
		byWorkflow[item.ID.String()] = item
	}
	checklists := make(map[string][]PursuitDelegationChecklistItem, len(detail.ChecklistItems))
	for _, item := range detail.ChecklistItems {
		key := item.WorkflowID.String()
		checklists[key] = append(checklists[key], PursuitDelegationChecklistItem{
			Label:    item.Label,
			Status:   item.Status,
			Required: item.RequiresApproval,
		})
	}
	seenWorkflows := map[string]bool{}
	for _, action := range detail.ActionQueues.VAReady {
		workflowID := strings.TrimSpace(action.WorkflowID)
		if workflowID == "" || seenWorkflows[workflowID] {
			continue
		}
		seenWorkflows[workflowID] = true
		workflowItem := byWorkflow[workflowID]
		packageResult.WorkItems = append(packageResult.WorkItems, PursuitDelegationWorkItem{
			WorkflowID:   workflowID,
			Title:        firstNonEmpty(workflowItem.Title, action.Label),
			Instructions: firstNonEmpty(action.Label, workflowItem.NextAction, workflowItem.Description),
			State:        workflowItem.CurrentState,
			DueAt:        workflowItem.DueAt,
			Checklist:    checklists[workflowID],
		})
	}
	if packageResult.Ready && len(packageResult.WorkItems) == 0 {
		packageResult.Ready = false
		packageResult.Status = "not_ready"
		packageResult.Reason = "VA-ready work was identified, but it is not linked to a governed workflow yet. Route it through pursuit planning first."
	}

	packageResult.SourceContext = delegationSources(detail, seenWorkflows)
	for _, blocker := range detail.Blockers {
		packageResult.EscalationRules = append(packageResult.EscalationRules, "Escalate to Robert: "+firstNonEmpty(blocker.Reason, blocker.Label, "a linked workflow is blocked"))
	}
	for _, action := range detail.ActionQueues.NeedsRobert {
		packageResult.EscalationRules = append(packageResult.EscalationRules, "Do not proceed past Robert's decision: "+action.Label)
	}
	for _, extraction := range detail.SourceExtractions {
		if extraction.Uncertain || extraction.Archived {
			packageResult.EscalationRules = append(packageResult.EscalationRules, "Do not rely on uncertain or archived extracted material without Robert review: "+firstNonEmpty(extraction.SourceLabel, extraction.SourceURI, extraction.ID.String()))
		}
	}
	packageResult.EscalationRules = uniqueStrings(packageResult.EscalationRules)
	return packageResult
}

func delegationSources(detail PursuitDetail, selectedWorkflows map[string]bool) []PursuitDelegationSource {
	result := []PursuitDelegationSource{}
	seen := map[string]bool{}
	add := func(workflowID, sourceType, uri, label, relationship string) {
		uri = strings.TrimSpace(uri)
		if uri == "" {
			return
		}
		key := workflowID + "|" + uri
		if seen[key] {
			return
		}
		seen[key] = true
		result = append(result, PursuitDelegationSource{WorkflowID: workflowID, SourceType: sourceType, SourceURI: uri, SourceLabel: label, Relationship: relationship})
	}
	for _, link := range detail.SourceLinks {
		workflowID := link.WorkflowID.String()
		if len(selectedWorkflows) > 0 && !selectedWorkflows[workflowID] {
			continue
		}
		add(workflowID, link.SourceType, link.SourceURI, link.SourceLabel, link.Relationship)
	}
	for _, link := range detail.Links {
		if link.LinkType == LinkSourceItem || link.LinkType == LinkSourceExtraction || link.LinkType == LinkMemory {
			add("", link.LinkType, link.SourceURI, link.SourceLabel, link.Relationship)
		}
	}
	return result
}

func (s *service) visibleLinksForOwner(ownerIdentity string, links []models.PursuitLink) ([]models.PursuitLink, error) {
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	if ownerIdentity == "" {
		return links, nil
	}
	validator, ok := s.repo.(ownerScopedLinkValidator)
	if !ok {
		return links, nil
	}
	visible := make([]models.PursuitLink, 0, len(links))
	for _, link := range links {
		handled, allowed, err := validator.LinkVisibleToOwner(ownerIdentity, link.LinkType, link.LinkID)
		if err != nil {
			return nil, err
		}
		if !handled || allowed {
			visible = append(visible, link)
		}
	}
	return visible, nil
}

func runtimeAttemptsVisibleToOwner(ownerIdentity string, attempts []models.AutomationLaunchEvent) []models.AutomationLaunchEvent {
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	if ownerIdentity == "" {
		return attempts
	}
	visible := make([]models.AutomationLaunchEvent, 0, len(attempts))
	for _, attempt := range attempts {
		if strings.TrimSpace(attempt.OwnerIdentity) == ownerIdentity {
			visible = append(visible, attempt)
		}
	}
	return visible
}

func taskAttemptsVisibleToOwner(ownerIdentity string, attempts []models.PursuitTaskAttempt) []models.PursuitTaskAttempt {
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	if ownerIdentity == "" {
		return attempts
	}
	visible := make([]models.PursuitTaskAttempt, 0, len(attempts))
	for _, attempt := range attempts {
		if strings.TrimSpace(attempt.OwnerIdentity) == ownerIdentity {
			visible = append(visible, attempt)
		}
	}
	return visible
}

func (s *service) Link(id uuid.UUID, request LinkRequest) (*models.PursuitLink, error) {
	pursuit, err := s.repo.FindByID(id)
	if err != nil {
		return nil, err
	}
	if !pursuitMutableBy(*pursuit, request.OwnerIdentity) {
		return nil, fmt.Errorf("pursuit not found")
	}
	linkType := strings.TrimSpace(request.LinkType)
	linkID := strings.TrimSpace(request.LinkID)
	if linkType == "" || linkID == "" {
		return nil, fmt.Errorf("linkType and linkId are required")
	}
	if linkType == LinkPursuit {
		linkedPursuitID, parseErr := uuid.Parse(linkID)
		if parseErr == nil && linkedPursuitID == id {
			return nil, fmt.Errorf("a pursuit cannot be linked to itself")
		}
	}
	if err := s.validateLinkOwnership(*pursuit, request.OwnerIdentity, linkType, linkID); err != nil {
		return nil, err
	}
	link := &models.PursuitLink{
		PursuitID:    id,
		LinkType:     linkType,
		LinkID:       linkID,
		Relationship: firstNonEmpty(request.Relationship, "related"),
		SourceURI:    strings.TrimSpace(request.SourceURI),
		SourceLabel:  strings.TrimSpace(request.SourceLabel),
		Confidence:   normalizeConfidence(request.Confidence, 0.7),
	}
	created, err := s.repo.CreateLink(link)
	if err != nil {
		return nil, err
	}
	if link.ID != uuid.Nil && created != nil && created.ID == link.ID {
		_, _ = s.recordActivity(id, "pursuit.linked", fmt.Sprintf("Linked %s %s", linkType, linkID), firstNonEmpty(request.Actor, "system"), linkType, linkID, request.SourceURI)
	}
	return created, nil
}

func (s *service) validateLinkOwnership(pursuit models.Pursuit, ownerIdentity, linkType, linkID string) error {
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	if ownerIdentity == "" {
		return nil
	}
	if owner := strings.TrimSpace(pursuit.OwnerIdentity); owner != "" && owner != ownerIdentity {
		return fmt.Errorf("pursuit is not visible to the authenticated owner")
	}
	validator, ok := s.repo.(ownerScopedLinkValidator)
	if !ok {
		return nil
	}
	handled, visible, err := validator.LinkVisibleToOwner(ownerIdentity, linkType, linkID)
	if err != nil {
		return err
	}
	if handled && !visible {
		return fmt.Errorf("linked %s record is not visible to the authenticated owner", linkType)
	}
	return nil
}

func (s *service) LinkVerification(pursuitID, verificationID uuid.UUID) error {
	return s.linkVerificationForOwner("", pursuitID, verificationID)
}

func (s *service) LinkVerificationForOwner(ownerIdentity string, pursuitID, verificationID uuid.UUID) error {
	if _, err := s.DetailForOwner(ownerIdentity, pursuitID); err != nil {
		return err
	}
	return s.linkVerificationForOwner(ownerIdentity, pursuitID, verificationID)
}

func (s *service) linkVerificationForOwner(ownerIdentity string, pursuitID, verificationID uuid.UUID) error {
	if pursuitID == uuid.Nil || verificationID == uuid.Nil {
		return fmt.Errorf("pursuit and verification ids are required")
	}
	if _, err := s.Link(pursuitID, LinkRequest{
		OwnerIdentity: ownerIdentity,
		LinkType:      LinkVerification,
		LinkID:        verificationID.String(),
		Relationship:  "verification_evidence",
		SourceURI:     "verification://" + verificationID.String(),
		SourceLabel:   "Source-grounded verification run",
		Confidence:    1,
		Actor:         "verification-engine",
	}); err != nil {
		return err
	}
	_, err := s.RefreshSummaryForOwner(ownerIdentity, pursuitID, "verification-engine")
	return err
}

func (s *service) DeleteLink(id uuid.UUID, linkID uuid.UUID, actor string) error {
	return s.DeleteLinkForOwner("", id, linkID, actor)
}

func (s *service) DeleteLinkForOwner(ownerIdentity string, id uuid.UUID, linkID uuid.UUID, actor string) error {
	pursuit, err := s.repo.FindByID(id)
	if err != nil {
		return err
	}
	if !pursuitMutableBy(*pursuit, ownerIdentity) {
		return fmt.Errorf("pursuit not found")
	}
	if err := s.repo.DeleteLink(id, linkID); err != nil {
		return err
	}
	_, _ = s.recordActivity(id, "pursuit.link_removed", "Removed pursuit link", firstNonEmpty(actor, "operator"), "", linkID.String(), "")
	return nil
}

func (s *service) Match(request MatchRequest) ([]MatchCandidate, error) {
	ownerIdentity := strings.TrimSpace(request.OwnerIdentity)
	pursuits, err := s.ListForOwner(ownerIdentity, false)
	if err != nil {
		return nil, err
	}
	if request.SourceType != "" && request.SourceID != "" {
		if link, err := s.repo.FindLinkForOwner(ownerIdentity, request.SourceType, request.SourceID); err == nil {
			if pursuit, err := s.repo.FindByID(link.PursuitID); err == nil && pursuitVisibleTo(*pursuit, ownerIdentity) {
				return []MatchCandidate{{Pursuit: *pursuit, Score: 0.98, Reasons: []string{"source is already linked to this pursuit"}, Confidence: "high"}}, nil
			}
		}
	}
	if sourceURI := strings.TrimSpace(request.SourceURI); sourceURI != "" {
		if link, err := s.repo.FindLinkBySourceURIForOwner(ownerIdentity, sourceURI); err == nil {
			if pursuit, err := s.repo.FindByID(link.PursuitID); err == nil && pursuitVisibleTo(*pursuit, ownerIdentity) {
				return []MatchCandidate{{Pursuit: *pursuit, Score: 0.97, Reasons: []string{"source URI is already linked to this pursuit"}, Confidence: "high"}}, nil
			}
		}
	}
	query := normalizeWords(request.Input + " " + request.ProjectKey + " " + request.SourceURI)
	result := []MatchCandidate{}
	for _, pursuit := range pursuits {
		score := 0.0
		reasons := []string{}
		if request.ProjectKey != "" && strings.EqualFold(request.ProjectKey, pursuit.ProjectKey) {
			score += 0.45
			reasons = append(reasons, "project key matches")
		}
		words := normalizeWords(pursuit.Title + " " + pursuit.Description + " " + pursuit.WhyItMatters + " " + pursuit.DesiredOutcome + " " + pursuit.ProjectKey)
		overlap := wordOverlap(query, words)
		if overlap > 0 {
			score += math.Min(0.45, overlap)
			reasons = append(reasons, "title/context overlap")
		}
		if score >= 0.18 {
			result = append(result, MatchCandidate{Pursuit: pursuit, Score: round(score), Reasons: reasons, Confidence: confidenceLabel(score)})
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Score > result[j].Score })
	limit := request.Limit
	if limit <= 0 || limit > 20 {
		limit = 5
	}
	if len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

func (s *service) AutoLinkWorkflow(request AutoLinkWorkflowRequest) (*AutoLinkResult, error) {
	if request.WorkflowID == uuid.Nil {
		return nil, fmt.Errorf("workflowId is required")
	}
	minimumScore := autoLinkMinimum(request.MinimumScore)
	matchSourceType := strings.TrimSpace(request.SourceType)
	matchSourceID := strings.TrimSpace(request.SourceID)
	if extractionID := strings.TrimSpace(request.ExtractionID); extractionID != "" {
		matchSourceType = LinkSourceExtraction
		matchSourceID = extractionID
	}
	matches, err := s.Match(MatchRequest{
		OwnerIdentity: request.OwnerIdentity,
		Input:         request.Input,
		ProjectKey:    request.ProjectKey,
		SourceType:    matchSourceType,
		SourceID:      matchSourceID,
		SourceURI:     request.SourceURI,
		Limit:         1,
	})
	if err != nil {
		return nil, err
	}
	if len(matches) == 0 {
		if request.AllowCreateCandidate && workflowCandidateAllowed(request) {
			return s.createWorkflowCandidate(request)
		}
		return &AutoLinkResult{Message: "no pursuit candidates matched"}, nil
	}
	match := matches[0]
	result := &AutoLinkResult{
		PursuitID: match.Pursuit.ID,
		Score:     match.Score,
		Reasons:   match.Reasons,
	}
	if match.Score < minimumScore {
		result.Message = fmt.Sprintf("best pursuit match %.2f is below auto-link threshold %.2f", match.Score, minimumScore)
		return result, nil
	}
	if pursuitClosed(match.Pursuit) {
		result.Message = "matched pursuit is closed; reopen it explicitly or create a new pursuit before linking operational work"
		return result, nil
	}
	if isPursuitCandidate(match.Pursuit) {
		// A source or memory producer may already have created this workflow
		// before it asks the pursuit layer to correlate it. Matching a reviewable
		// candidate must not turn that workflow into candidate-owned operational
		// work: Robert has not accepted the objective yet.
		result.Message = "matched pursuit candidate awaits explicit acceptance; workflow was not linked"
		_, _ = s.recordActivity(match.Pursuit.ID, "pursuit.candidate_workflow_link_deferred", "A source-derived workflow matched this reviewable candidate, but no operational link was created before explicit acceptance.", firstNonEmpty(request.Actor, "system"), LinkWorkflow, request.WorkflowID.String(), request.SourceURI)
		return result, nil
	}

	actor := firstNonEmpty(request.Actor, "system")
	links := []models.PursuitLink{}
	workflowLink, err := s.Link(match.Pursuit.ID, LinkRequest{
		OwnerIdentity: request.OwnerIdentity,
		LinkType:      LinkWorkflow,
		LinkID:        request.WorkflowID.String(),
		Relationship:  "operational_work",
		SourceURI:     request.SourceURI,
		SourceLabel:   request.SourceLabel,
		Confidence:    match.Score,
		Actor:         actor,
	})
	if err != nil {
		return nil, err
	}
	links = append(links, *workflowLink)
	if sourceLink, linked, err := s.linkExactSourceReference(match.Pursuit.ID, request.OwnerIdentity, request.SourceType, request.SourceID, request.SourceURI, request.SourceLabel, match.Score, actor); err != nil {
		return nil, err
	} else if linked {
		links = append(links, *sourceLink)
	}
	if conversationLink, linked, err := s.linkConversationReference(match.Pursuit.ID, request.OwnerIdentity, request.ConversationID, request.ConversationSourceURI, request.ConversationLabel, "conversation_context", match.Score, actor); err != nil {
		return nil, err
	} else if linked {
		links = append(links, *conversationLink)
	}
	if err := s.linkAssistantCommandReference(match.Pursuit.ID, request.OwnerIdentity, request.SourceType, request.SourceID, request.SourceURI, request.SourceLabel, actor); err != nil {
		return nil, err
	}
	if sourceItemLink, linked, err := s.linkOptionalUUID(match.Pursuit.ID, LinkSourceItem, request.RawItemID, "source_record", request, match.Score, actor); err != nil {
		return nil, err
	} else if linked {
		links = append(links, *sourceItemLink)
	}
	if extractionLink, linked, err := s.linkOptionalUUID(match.Pursuit.ID, LinkSourceExtraction, request.ExtractionID, "source_extraction", request, match.Score, actor); err != nil {
		return nil, err
	} else if linked {
		links = append(links, *extractionLink)
	}
	_, _ = s.recordActivity(match.Pursuit.ID, "pursuit.auto_linked", fmt.Sprintf("Source-derived workflow auto-linked to pursuit with %.2f confidence", match.Score), actor, LinkWorkflow, request.WorkflowID.String(), request.SourceURI)
	result.Linked = true
	result.Links = links
	result.Message = "source-derived workflow linked to pursuit"
	if _, err := s.RefreshSummaryForOwner(request.OwnerIdentity, match.Pursuit.ID, actor); err != nil {
		result.Message = "source-derived workflow linked to pursuit; summary refresh failed: " + err.Error()
	}
	return result, nil
}

func (s *service) AutoLinkMemory(request AutoLinkMemoryRequest) (*AutoLinkResult, error) {
	if request.MemoryID == uuid.Nil {
		return nil, fmt.Errorf("memoryId is required")
	}
	minimumScore := autoLinkMinimum(request.MinimumScore)
	matches, err := s.Match(MatchRequest{
		OwnerIdentity: request.OwnerIdentity,
		Input:         request.Input,
		ProjectKey:    request.ProjectKey,
		SourceURI:     request.SourceURI,
		Limit:         1,
	})
	if err != nil {
		return nil, err
	}
	if len(matches) == 0 {
		if request.AllowCreateCandidate && memoryCandidateAllowed(request) {
			return s.createMemoryCandidate(request)
		}
		return &AutoLinkResult{Message: "no pursuit candidates matched"}, nil
	}
	match := matches[0]
	result := &AutoLinkResult{
		PursuitID: match.Pursuit.ID,
		Score:     match.Score,
		Reasons:   match.Reasons,
	}
	if match.Score < minimumScore {
		result.Message = fmt.Sprintf("best pursuit match %.2f is below auto-link threshold %.2f", match.Score, minimumScore)
		return result, nil
	}
	actor := firstNonEmpty(request.Actor, "system")
	link, err := s.Link(match.Pursuit.ID, LinkRequest{
		OwnerIdentity: request.OwnerIdentity,
		LinkType:      LinkMemory,
		LinkID:        request.MemoryID.String(),
		Relationship:  "context_memory",
		SourceURI:     request.SourceURI,
		SourceLabel:   request.SourceLabel,
		Confidence:    match.Score,
		Actor:         actor,
	})
	if err != nil {
		return nil, err
	}
	links := []models.PursuitLink{*link}
	if conversationLink, linked, err := s.linkConversationReference(match.Pursuit.ID, request.OwnerIdentity, request.ConversationID, request.ConversationSourceURI, request.ConversationLabel, "conversation_context", match.Score, actor); err != nil {
		return nil, err
	} else if linked {
		links = append(links, *conversationLink)
	}
	_, _ = s.recordActivity(match.Pursuit.ID, "pursuit.memory_auto_linked", fmt.Sprintf("Context memory auto-linked to pursuit with %.2f confidence", match.Score), actor, LinkMemory, request.MemoryID.String(), request.SourceURI)
	result.Linked = true
	result.Links = links
	result.Message = "context memory linked to pursuit"
	if _, err := s.RefreshSummaryForOwner(request.OwnerIdentity, match.Pursuit.ID, actor); err != nil {
		result.Message = "context memory linked to pursuit; summary refresh failed: " + err.Error()
	}
	return result, nil
}

func (s *service) RouteIntake(request IntakeRequest) (*RoutedIntakeResult, error) {
	request.Input = strings.TrimSpace(request.Input)
	if request.Input == "" {
		return nil, fmt.Errorf("input is required")
	}
	if s.workflowService == nil {
		return nil, fmt.Errorf("workflow service is not configured")
	}
	normalizedMandateID, err := parseOptionalStandingMandateID(request.MandateID)
	if err != nil {
		return nil, err
	}
	request.MandateID = pursuitMandateIDString(normalizedMandateID)
	actor := firstNonEmpty(request.Actor, "operator")
	matches, err := s.Match(MatchRequest{
		OwnerIdentity: request.OwnerIdentity,
		Input:         request.Input,
		ProjectKey:    request.ProjectKey,
		SourceType:    request.SourceType,
		SourceID:      request.SourceID,
		SourceURI:     request.SourceURI,
		Limit:         3,
	})
	if err != nil {
		return nil, err
	}
	minimumScore := defaultAutoLinkMinimumScore
	if len(matches) > 0 && matches[0].Score >= minimumScore && !pursuitClosed(matches[0].Pursuit) {
		if isPursuitCandidate(matches[0].Pursuit) {
			if !samePursuitMandate(matches[0].Pursuit.MandateID, normalizedMandateID) {
				return nil, fmt.Errorf("matched pursuit candidate uses a different standing mandate; review the candidate before changing authority")
			}
			detail, err := s.DetailForOwner(request.OwnerIdentity, matches[0].Pursuit.ID)
			if err != nil {
				return nil, err
			}
			// A repeated source event may prove the candidate is relevant, but it
			// must not create new operational work until Robert explicitly accepts
			// the objective. Preserve the original candidate workflow and keep the
			// decision visible instead of silently promoting the candidate.
			_, _ = s.recordActivity(matches[0].Pursuit.ID, "pursuit.candidate_intake_reseen", fmt.Sprintf("Global intake matched an unaccepted pursuit candidate with %.2f confidence; no new work was created.", matches[0].Score), actor, firstNonEmpty(request.SourceType, "intake"), request.SourceID, request.SourceURI)
			return &RoutedIntakeResult{
				Mode:      "matched_candidate",
				Matched:   true,
				PursuitID: matches[0].Pursuit.ID,
				Score:     matches[0].Score,
				Reasons:   matches[0].Reasons,
				Message:   "intake matched an existing pursuit candidate awaiting explicit acceptance; no new operational work was created",
				Matches:   matches,
				Detail:    detail,
			}, nil
		}
		if _, err := s.IntakeForOwner(request.OwnerIdentity, matches[0].Pursuit.ID, request); err != nil {
			return nil, err
		}
		detail, err := s.DetailForOwner(request.OwnerIdentity, matches[0].Pursuit.ID)
		if err != nil {
			return nil, err
		}
		_, _ = s.recordActivity(matches[0].Pursuit.ID, "pursuit.routed_intake", fmt.Sprintf("Global intake routed into existing pursuit with %.2f confidence.", matches[0].Score), actor, firstNonEmpty(request.SourceType, "intake"), request.SourceID, request.SourceURI)
		return &RoutedIntakeResult{
			Mode:      "matched_existing",
			Matched:   true,
			PursuitID: matches[0].Pursuit.ID,
			Score:     matches[0].Score,
			Reasons:   matches[0].Reasons,
			Message:   "intake matched and created governed workflow under existing pursuit",
			Matches:   matches,
			Detail:    detail,
		}, nil
	}

	// Candidate-worthy unmatched input becomes a pursuit before workflow work
	// exists. This keeps sources, imported conversations, and direct API intake
	// from leaving behind a review-held but separately addressable workflow that
	// Robert never accepted as a real objective.
	candidateEligible := candidateHasEnoughSignal(request.Input, request.ProjectKey, request.SourceLabel, request.SourceURI)
	if candidateEligible {
		return s.createIntakeCandidate(request, matches)
	}
	reviewRequired := request.RequiresReview || candidateEligible || strings.EqualFold(classifyRisk(request.Input+" "+request.SourceLabel+" "+request.SourceType), "high")
	reviewReason := firstNonEmpty(request.ReviewReason, routedIntakeReviewReason(reviewRequired, request))
	record, err := s.workflowService.Intake(workflow.IntakeRequest{
		OwnerIdentity:  request.OwnerIdentity,
		Input:          request.Input,
		ProjectKey:     request.ProjectKey,
		AutomationID:   request.AutomationID,
		MandateID:      request.MandateID,
		SourceType:     request.SourceType,
		SourceID:       request.SourceID,
		SourceURI:      request.SourceURI,
		SourceLabel:    request.SourceLabel,
		ContentType:    request.ContentType,
		Sender:         request.Sender,
		ReceivedAt:     request.ReceivedAt,
		Trigger:        firstNonEmpty(request.Trigger, "pursuit_global_intake"),
		Actor:          actor,
		RequiresReview: reviewRequired,
		ReviewReason:   reviewReason,
	})
	if err != nil {
		return nil, err
	}
	if record == nil || record.Item.ID == uuid.Nil {
		return &RoutedIntakeResult{
			Mode:    "workflow_missing",
			Message: "workflow intake did not return a workflow record; no pursuit candidate was created",
			Matches: matches,
		}, nil
	}

	autoLinkRequest := AutoLinkWorkflowRequest{
		OwnerIdentity:         request.OwnerIdentity,
		WorkflowID:            record.Item.ID,
		Input:                 request.Input,
		ProjectKey:            request.ProjectKey,
		MandateID:             request.MandateID,
		SourceType:            request.SourceType,
		SourceID:              request.SourceID,
		SourceURI:             request.SourceURI,
		SourceLabel:           request.SourceLabel,
		ConversationID:        conversationIDFromAIChatSource(request.SourceType, request.SourceID),
		ConversationSourceURI: request.SourceURI,
		ConversationLabel:     request.SourceLabel,
		Actor:                 actor,
		AllowCreateCandidate:  candidateEligible,
	}
	autoLink, err := s.AutoLinkWorkflow(autoLinkRequest)
	if err != nil {
		return nil, err
	}
	if (autoLink == nil || !autoLink.Linked) && candidateEligible {
		autoLink, err = s.createWorkflowCandidate(autoLinkRequest)
		if err != nil {
			return nil, err
		}
	}
	result := &RoutedIntakeResult{
		Mode:     "candidate_created",
		Matches:  matches,
		AutoLink: autoLink,
		Message:  firstNonEmpty(autoLinkMessage(autoLink), "intake converted into governed workflow; no pursuit candidate was created"),
	}
	if autoLink != nil {
		result.Matched = autoLink.Linked && !autoLink.Created
		result.CreatedCandidate = autoLink.Created
		result.PursuitID = autoLink.PursuitID
		result.Score = autoLink.Score
		result.Reasons = autoLink.Reasons
		if autoLink.PursuitID != uuid.Nil {
			detail, err := s.DetailForOwner(request.OwnerIdentity, autoLink.PursuitID)
			if err != nil {
				return nil, err
			}
			result.Detail = detail
		}
		if autoLink.Created {
			result.Mode = "candidate_created"
		} else if autoLink.Linked {
			result.Mode = "matched_after_workflow"
		}
	}
	return result, nil
}

func (s *service) createIntakeCandidate(request IntakeRequest, matches []MatchCandidate) (*RoutedIntakeResult, error) {
	actor := firstNonEmpty(request.Actor, "system")
	sourceType := firstNonEmpty(strings.TrimSpace(request.SourceType), "intake")
	sourceLabel := firstNonEmpty(request.SourceLabel, request.SourceURI, sourceType+" intake")
	created, err := s.Create(CreateRequest{
		OwnerIdentity:         request.OwnerIdentity,
		Title:                 candidateTitle(sourceLabel, request.Input),
		Description:           candidateDescription("intake", request.Input, request.SourceURI, sourceLabel),
		ProjectKey:            strings.TrimSpace(request.ProjectKey),
		MandateID:             request.MandateID,
		DesiredOutcome:        "Turn this intake into a verified, governed outcome.",
		CurrentStateSummary:   "Created as a reviewable pursuit candidate before operational workflow work because no active pursuit matched the intake.",
		Status:                StatusWaiting,
		RiskLevel:             classifyRisk(request.Input + " " + sourceLabel + " " + sourceType),
		AutonomyLevel:         "suggest",
		SourceOfCreation:      sourceType + "_pursuit_candidate",
		NextRecommendedAction: "Review this pursuit candidate and accept it to create the first governed workflow plan.",
		CompletionDefinition:  "Robert accepts the candidate, the governed workflow path is completed, and completion evidence is verified.",
	})
	if err != nil {
		return nil, err
	}

	links := []models.PursuitLink{}
	if sourceLink, linked, err := s.linkExactSourceReference(created.ID, request.OwnerIdentity, request.SourceType, request.SourceID, request.SourceURI, request.SourceLabel, 1, actor); err != nil {
		return nil, err
	} else if linked {
		links = append(links, *sourceLink)
	}
	if conversationLink, linked, err := s.linkConversationReference(created.ID, request.OwnerIdentity, conversationIDFromAIChatSource(request.SourceType, request.SourceID), request.SourceURI, request.SourceLabel, "conversation_context", 1, actor); err != nil {
		return nil, err
	} else if linked {
		links = append(links, *conversationLink)
	}
	if err := s.linkAssistantCommandReference(created.ID, request.OwnerIdentity, request.SourceType, request.SourceID, request.SourceURI, request.SourceLabel, actor); err != nil {
		return nil, err
	}
	linkRequest := AutoLinkWorkflowRequest{
		OwnerIdentity: request.OwnerIdentity,
		RawItemID:     request.RawItemID,
		ExtractionID:  request.ExtractionID,
		SourceURI:     request.SourceURI,
		SourceLabel:   request.SourceLabel,
	}
	if sourceItemLink, linked, err := s.linkOptionalUUID(created.ID, LinkSourceItem, request.RawItemID, "candidate_source_record", linkRequest, 1, actor); err != nil {
		return nil, err
	} else if linked {
		links = append(links, *sourceItemLink)
	}
	if extractionLink, linked, err := s.linkOptionalUUID(created.ID, LinkSourceExtraction, request.ExtractionID, "candidate_source_extraction", linkRequest, 1, actor); err != nil {
		return nil, err
	} else if linked {
		links = append(links, *extractionLink)
	}
	_, _ = s.recordActivity(created.ID, "pursuit.candidate_created", "Created pursuit candidate from unmatched intake before workflow creation.", actor, sourceType, request.SourceID, request.SourceURI)

	result := &RoutedIntakeResult{
		Mode:             "candidate_created",
		CreatedCandidate: true,
		PursuitID:        created.ID,
		Score:            1,
		Reasons:          []string{"no active pursuit matched", "candidate created before workflow work"},
		Message:          "intake created a reviewable pursuit candidate; explicit acceptance is required before a workflow is created",
		Matches:          matches,
		AutoLink: &AutoLinkResult{
			Linked:    true,
			Created:   true,
			PursuitID: created.ID,
			Score:     1,
			Reasons:   []string{"no active pursuit matched", "candidate created before workflow work"},
			Message:   "pursuit candidate created before workflow work",
			Links:     links,
		},
	}
	detail, err := s.RefreshSummaryForOwner(request.OwnerIdentity, created.ID, actor)
	if err != nil {
		return nil, err
	}
	result.Detail = detail
	return result, nil
}

// RouteAmbientOpportunity handles an already accepted ambient proposal before
// a workflow exists. An existing active pursuit receives governed intake; a
// candidate match or no match remains a reviewable candidate with provenance
// instead of creating executable work that has no accepted pursuit owner.
func (s *service) RouteAmbientOpportunity(request AmbientOpportunityRouteRequest) (*AmbientOpportunityRouteResult, error) {
	if request.OpportunityID == uuid.Nil {
		return nil, fmt.Errorf("ambient opportunity id is required")
	}
	request.Title = strings.TrimSpace(request.Title)
	request.NextAction = strings.TrimSpace(request.NextAction)
	if request.Title == "" || request.NextAction == "" {
		return nil, fmt.Errorf("ambient opportunity title and next action are required")
	}
	ownerIdentity := strings.TrimSpace(request.OwnerIdentity)
	sourceID := request.OpportunityID.String()
	sourceURI := firstNonEmpty(strings.TrimSpace(request.SourceURI), "ambient://opportunities/"+sourceID)
	actor := firstNonEmpty(strings.TrimSpace(request.Actor), "ambient-engine")
	input := strings.TrimSpace(strings.Join([]string{request.Title, request.Rationale, request.NextAction}, "\n"))
	matches, err := s.Match(MatchRequest{
		OwnerIdentity: ownerIdentity,
		Input:         input,
		ProjectKey:    request.ProjectKey,
		SourceType:    LinkAmbientOpportunity,
		SourceID:      sourceID,
		SourceURI:     sourceURI,
		Limit:         1,
	})
	if err != nil {
		return nil, err
	}
	if len(matches) > 0 && matches[0].Score >= defaultAutoLinkMinimumScore && !pursuitClosed(matches[0].Pursuit) {
		match := matches[0]
		if isPursuitCandidate(match.Pursuit) {
			if err := s.linkAcceptedAmbientOpportunity(match.Pursuit.ID, ownerIdentity, sourceID, sourceURI, request.Title, match.Score, actor); err != nil {
				return nil, err
			}
			return &AmbientOpportunityRouteResult{
				Mode:      "matched_candidate",
				PursuitID: match.Pursuit.ID,
				Message:   "accepted ambient proposal was linked as candidate context; explicit pursuit acceptance is still required before workflow work",
			}, nil
		}
		detail, err := s.IntakeForOwner(ownerIdentity, match.Pursuit.ID, IntakeRequest{
			OwnerIdentity:  ownerIdentity,
			Input:          input,
			ProjectKey:     request.ProjectKey,
			SourceType:     LinkAmbientOpportunity,
			SourceID:       sourceID,
			SourceURI:      sourceURI,
			SourceLabel:    request.Title,
			ContentType:    "ambient_proposal",
			Trigger:        "ambient.accept",
			Actor:          actor,
			RequiresReview: request.RequiresReview,
			ReviewReason:   request.ReviewReason,
		})
		if err != nil {
			return nil, err
		}
		workflowID := workflowIDForSource(detail, LinkAmbientOpportunity, sourceID, sourceURI)
		if workflowID == uuid.Nil {
			return nil, fmt.Errorf("ambient pursuit intake did not identify its workflow record")
		}
		if err := s.linkAcceptedAmbientOpportunity(match.Pursuit.ID, ownerIdentity, sourceID, sourceURI, request.Title, match.Score, actor); err != nil {
			return nil, err
		}
		return &AmbientOpportunityRouteResult{
			Mode:       "matched_existing",
			PursuitID:  match.Pursuit.ID,
			WorkflowID: workflowID,
			Message:    "accepted ambient proposal created governed workflow work under the matched pursuit",
		}, nil
	}

	candidate, err := s.Create(CreateRequest{
		OwnerIdentity:         ownerIdentity,
		Title:                 request.Title,
		Description:           request.Rationale,
		WhyItMatters:          "An accepted ambient proposal needs an explicit pursuit decision before HAI creates operational work.",
		ProjectKey:            request.ProjectKey,
		DesiredOutcome:        request.NextAction,
		CurrentStateSummary:   "Ambient proposal accepted as context. This reviewable pursuit candidate has no workflow work until Robert explicitly accepts the objective.",
		SourceOfCreation:      "ambient_pursuit_candidate",
		NextRecommendedAction: "Review this ambient pursuit candidate and accept it to create the first governed workflow plan.",
		CompletionDefinition:  "Robert accepts the candidate, the governed workflow path is completed, and completion evidence is verified.",
		Actor:                 actor,
	})
	if err != nil {
		return nil, err
	}
	if err := s.linkAcceptedAmbientOpportunity(candidate.ID, ownerIdentity, sourceID, sourceURI, request.Title, 1, actor); err != nil {
		return nil, err
	}
	return &AmbientOpportunityRouteResult{
		Mode:             "candidate_created",
		PursuitID:        candidate.ID,
		CreatedCandidate: true,
		Message:          "accepted ambient proposal created a reviewable pursuit candidate; no workflow work was created before explicit candidate acceptance",
	}, nil
}

func (s *service) linkAcceptedAmbientOpportunity(pursuitID uuid.UUID, ownerIdentity, sourceID, sourceURI, title string, confidence float64, actor string) error {
	_, err := s.Link(pursuitID, LinkRequest{
		OwnerIdentity: ownerIdentity,
		LinkType:      LinkAmbientOpportunity,
		LinkID:        sourceID,
		Relationship:  "ambient_proposal_accepted",
		SourceURI:     sourceURI,
		SourceLabel:   title,
		Confidence:    normalizeConfidence(confidence, 0.7),
		Actor:         actor,
	})
	return err
}

func workflowIDForSource(detail *PursuitDetail, sourceType, sourceID, sourceURI string) uuid.UUID {
	if detail == nil {
		return uuid.Nil
	}
	for _, item := range detail.Workflows {
		if strings.EqualFold(strings.TrimSpace(item.SourceType), strings.TrimSpace(sourceType)) && item.SourceID == sourceID {
			return item.ID
		}
	}
	for _, item := range detail.Workflows {
		if sourceURI != "" && item.SourceURI == sourceURI {
			return item.ID
		}
	}
	return uuid.Nil
}

// RouteWorkflowIntake adapts the legacy workflow endpoint to the native
// pursuit intake path. It returns CandidatePendingError when the input needs
// explicit pursuit acceptance before any workflow record may exist.
func (s *service) RouteWorkflowIntake(request workflow.IntakeRequest) (*workflow.WorkflowRecord, error) {
	routed, err := s.RouteIntake(IntakeRequest{
		OwnerIdentity:  request.OwnerIdentity,
		Input:          request.Input,
		ProjectKey:     request.ProjectKey,
		AutomationID:   request.AutomationID,
		MandateID:      request.MandateID,
		SourceType:     request.SourceType,
		SourceID:       request.SourceID,
		RawItemID:      request.RawItemID,
		ExtractionID:   request.ExtractionID,
		SourceURI:      request.SourceURI,
		SourceLabel:    request.SourceLabel,
		ContentType:    request.ContentType,
		Sender:         request.Sender,
		ReceivedAt:     request.ReceivedAt,
		Trigger:        request.Trigger,
		Actor:          request.Actor,
		RequiresReview: request.RequiresReview,
		ReviewReason:   request.ReviewReason,
	})
	if err != nil {
		return nil, err
	}
	if routed != nil && (routed.CreatedCandidate || routed.Mode == "matched_candidate") {
		return nil, &CandidatePendingError{Result: routed}
	}
	reader, ok := s.workflowService.(workflowOwnerScopedRecordReader)
	if !ok {
		return nil, fmt.Errorf("workflow service does not support owner-scoped record retrieval")
	}
	workflowID := routedWorkflowID(routed, request)
	if workflowID == uuid.Nil {
		return nil, fmt.Errorf("pursuit intake did not identify the created workflow record")
	}
	return reader.GetForOwner(request.OwnerIdentity, workflowID)
}

func routedWorkflowID(routed *RoutedIntakeResult, request workflow.IntakeRequest) uuid.UUID {
	if routed == nil || routed.Detail == nil {
		return uuid.Nil
	}
	for _, item := range routed.Detail.Workflows {
		if strings.TrimSpace(request.SourceType) != "" && strings.TrimSpace(request.SourceID) != "" &&
			strings.EqualFold(item.SourceType, request.SourceType) && item.SourceID == request.SourceID {
			return item.ID
		}
	}
	for _, item := range routed.Detail.Workflows {
		if strings.TrimSpace(request.SourceURI) != "" && item.SourceURI == request.SourceURI {
			return item.ID
		}
	}
	return uuid.Nil
}

func routedIntakeReviewReason(reviewRequired bool, request IntakeRequest) string {
	if !reviewRequired {
		return ""
	}
	risk := classifyRisk(request.Input + " " + request.SourceLabel + " " + request.SourceType)
	if strings.EqualFold(risk, "high") {
		return "global pursuit intake classified this as high-risk; Robert approval is required before consequential execution"
	}
	return "global pursuit intake requires review before consequential execution"
}

// conversationIDFromAIChatSource accepts only the internal source-id format
// emitted by memory-engine imports: <conversation UUID>:<insight UUID>. The
// result is still checked by LinkVisibleToOwner before it can be persisted.
func conversationIDFromAIChatSource(sourceType, sourceID string) uuid.UUID {
	if !strings.EqualFold(strings.TrimSpace(sourceType), "ai_chat") {
		return uuid.Nil
	}
	conversationID, _, found := strings.Cut(strings.TrimSpace(sourceID), ":")
	if !found {
		return uuid.Nil
	}
	id, err := uuid.Parse(strings.TrimSpace(conversationID))
	if err != nil {
		return uuid.Nil
	}
	return id
}

func autoLinkMessage(result *AutoLinkResult) string {
	if result == nil {
		return ""
	}
	return result.Message
}

func (s *service) createWorkflowCandidate(request AutoLinkWorkflowRequest) (*AutoLinkResult, error) {
	actor := firstNonEmpty(request.Actor, "system")
	sourceLabel := firstNonEmpty(request.SourceLabel, request.SourceURI, request.SourceType, "source-derived work")
	created, err := s.Create(CreateRequest{
		OwnerIdentity:         request.OwnerIdentity,
		Title:                 candidateTitle(sourceLabel, request.Input),
		Description:           candidateDescription("workflow", request.Input, request.SourceURI, sourceLabel),
		ProjectKey:            strings.TrimSpace(request.ProjectKey),
		MandateID:             request.MandateID,
		DesiredOutcome:        "Turn this source-derived work into a verified, governed outcome.",
		CurrentStateSummary:   "Created as a reviewable pursuit candidate after no existing pursuit matched the source-derived workflow.",
		Status:                StatusWaiting,
		RiskLevel:             classifyRisk(request.Input + " " + sourceLabel),
		AutonomyLevel:         "suggest",
		SourceOfCreation:      firstNonEmpty(request.SourceType, "source") + "_pursuit_candidate",
		NextRecommendedAction: "Review this pursuit candidate, confirm it belongs in HAI, and decide the next concrete workflow step.",
		CompletionDefinition:  "Candidate is accepted, linked to the correct work, and closed only after verified completion or Robert archives it.",
	})
	if err != nil {
		return nil, err
	}
	// This compatibility path receives a workflow that was created before the
	// pursuit layer was asked to correlate it. Preserve source provenance for
	// review, but never attach that operational workflow to an unaccepted
	// candidate. The supported lifecycle router creates the candidate before any
	// workflow exists; legacy callers must not weaken that boundary.
	links := []models.PursuitLink{}
	if sourceLink, linked, err := s.linkExactSourceReference(created.ID, request.OwnerIdentity, request.SourceType, request.SourceID, request.SourceURI, request.SourceLabel, 1, actor); err != nil {
		return nil, err
	} else if linked {
		links = append(links, *sourceLink)
	}
	if conversationLink, linked, err := s.linkConversationReference(created.ID, request.OwnerIdentity, request.ConversationID, request.ConversationSourceURI, request.ConversationLabel, "conversation_context", 1, actor); err != nil {
		return nil, err
	} else if linked {
		links = append(links, *conversationLink)
	}
	if err := s.linkAssistantCommandReference(created.ID, request.OwnerIdentity, request.SourceType, request.SourceID, request.SourceURI, request.SourceLabel, actor); err != nil {
		return nil, err
	}
	if sourceItemLink, linked, err := s.linkOptionalUUID(created.ID, LinkSourceItem, request.RawItemID, "candidate_source_record", request, 1, actor); err != nil {
		return nil, err
	} else if linked {
		links = append(links, *sourceItemLink)
	}
	if extractionLink, linked, err := s.linkOptionalUUID(created.ID, LinkSourceExtraction, request.ExtractionID, "candidate_source_extraction", request, 1, actor); err != nil {
		return nil, err
	} else if linked {
		links = append(links, *extractionLink)
	}
	_, _ = s.recordActivity(created.ID, "pursuit.candidate_created", "Created pursuit candidate from unmatched source-derived workflow; no operational workflow was attached before acceptance.", actor, firstNonEmpty(request.SourceType, "workflow"), request.SourceID, request.SourceURI)
	if _, err := s.RefreshSummaryForOwner(request.OwnerIdentity, created.ID, actor); err != nil {
		return &AutoLinkResult{Created: true, PursuitID: created.ID, Score: 1, Reasons: []string{"no existing pursuit matched", "candidate created before operational workflow could be linked"}, Message: "pursuit candidate created without attaching pre-existing workflow work; summary refresh failed: " + err.Error(), Links: links}, nil
	}
	return &AutoLinkResult{Created: true, PursuitID: created.ID, Score: 1, Reasons: []string{"no existing pursuit matched", "candidate created before operational workflow could be linked"}, Message: "pursuit candidate created without attaching pre-existing workflow work", Links: links}, nil
}

func (s *service) createMemoryCandidate(request AutoLinkMemoryRequest) (*AutoLinkResult, error) {
	actor := firstNonEmpty(request.Actor, "system")
	sourceLabel := firstNonEmpty(request.SourceLabel, request.SourceURI, "memory insight")
	created, err := s.Create(CreateRequest{
		OwnerIdentity:         request.OwnerIdentity,
		Title:                 candidateTitle(sourceLabel, request.Input),
		Description:           candidateDescription("memory", request.Input, request.SourceURI, sourceLabel),
		ProjectKey:            strings.TrimSpace(request.ProjectKey),
		DesiredOutcome:        "Turn this memory-derived objective into a verified, governed outcome.",
		CurrentStateSummary:   "Created as a reviewable pursuit candidate after no existing pursuit matched the memory insight.",
		Status:                StatusWaiting,
		RiskLevel:             classifyRisk(request.Input + " " + sourceLabel),
		AutonomyLevel:         "suggest",
		SourceOfCreation:      "memory_pursuit_candidate",
		NextRecommendedAction: "Review this memory-derived pursuit candidate before turning it into active work.",
		CompletionDefinition:  "Candidate is accepted, linked to the correct work, and closed only after verified completion or Robert archives it.",
	})
	if err != nil {
		return nil, err
	}
	link, err := s.Link(created.ID, LinkRequest{
		OwnerIdentity: request.OwnerIdentity,
		LinkType:      LinkMemory,
		LinkID:        request.MemoryID.String(),
		Relationship:  "candidate_context_memory",
		SourceURI:     request.SourceURI,
		SourceLabel:   request.SourceLabel,
		Confidence:    1,
		Actor:         actor,
	})
	if err != nil {
		return nil, err
	}
	links := []models.PursuitLink{*link}
	if conversationLink, linked, err := s.linkConversationReference(created.ID, request.OwnerIdentity, request.ConversationID, request.ConversationSourceURI, request.ConversationLabel, "conversation_context", 1, actor); err != nil {
		return nil, err
	} else if linked {
		links = append(links, *conversationLink)
	}
	_, _ = s.recordActivity(created.ID, "pursuit.candidate_created", "Created pursuit candidate from memory insight because no existing pursuit matched.", actor, LinkMemory, request.MemoryID.String(), request.SourceURI)
	if _, err := s.RefreshSummaryForOwner(request.OwnerIdentity, created.ID, actor); err != nil {
		return &AutoLinkResult{Linked: true, Created: true, PursuitID: created.ID, Score: 1, Reasons: []string{"no existing pursuit matched", "candidate created from memory insight"}, Message: "pursuit candidate created; summary refresh failed: " + err.Error(), Links: links}, nil
	}
	return &AutoLinkResult{Linked: true, Created: true, PursuitID: created.ID, Score: 1, Reasons: []string{"no existing pursuit matched", "candidate created from memory insight"}, Message: "pursuit candidate created from memory insight", Links: links}, nil
}

func (s *service) linkConversationReference(pursuitID uuid.UUID, ownerIdentity string, conversationID uuid.UUID, sourceURI, sourceLabel, relationship string, confidence float64, actor string) (*models.PursuitLink, bool, error) {
	if conversationID == uuid.Nil {
		return nil, false, nil
	}
	link, err := s.Link(pursuitID, LinkRequest{
		OwnerIdentity: ownerIdentity,
		LinkType:      LinkAIConversation,
		LinkID:        conversationID.String(),
		Relationship:  firstNonEmpty(strings.TrimSpace(relationship), "conversation_context"),
		SourceURI:     strings.TrimSpace(sourceURI),
		SourceLabel:   firstNonEmpty(strings.TrimSpace(sourceLabel), "AI conversation archive"),
		Confidence:    confidence,
		Actor:         firstNonEmpty(actor, "memory-engine"),
	})
	if err != nil {
		return nil, false, err
	}
	_, _ = s.recordActivity(pursuitID, "pursuit.conversation_linked", "Encrypted AI conversation archive linked as source context.", firstNonEmpty(actor, "memory-engine"), LinkAIConversation, conversationID.String(), strings.TrimSpace(sourceURI))
	return link, true, nil
}

func workflowCandidateAllowed(request AutoLinkWorkflowRequest) bool {
	if request.WorkflowID == uuid.Nil {
		return false
	}
	return candidateHasEnoughSignal(request.Input, request.ProjectKey, request.SourceLabel, request.SourceURI)
}

func memoryCandidateAllowed(request AutoLinkMemoryRequest) bool {
	if request.MemoryID == uuid.Nil {
		return false
	}
	return candidateHasEnoughSignal(request.Input, request.ProjectKey, request.SourceLabel, request.SourceURI)
}

func candidateHasEnoughSignal(values ...string) bool {
	words := normalizeWords(strings.Join(values, " "))
	return len(words) >= 4
}

func candidateTitle(sourceLabel, input string) string {
	title := firstNonEmpty(sourceLabel, firstNonEmptyLine(input), "Review new pursuit candidate")
	return compactCandidateText(title, 90)
}

func candidateDescription(kind, input, sourceURI, sourceLabel string) string {
	parts := []string{"Candidate source: " + kind}
	if strings.TrimSpace(sourceLabel) != "" {
		parts = append(parts, "Source label: "+compactCandidateText(sourceLabel, 180))
	}
	if strings.TrimSpace(sourceURI) != "" {
		parts = append(parts, "Source URI: "+compactCandidateText(sourceURI, 260))
	}
	if strings.TrimSpace(input) != "" {
		parts = append(parts, "Extracted signal: "+compactCandidateText(input, 1000))
	}
	return strings.Join(parts, "\n")
}

func firstNonEmptyLine(value string) string {
	for _, line := range strings.Split(value, "\n") {
		if strings.TrimSpace(line) != "" {
			return strings.TrimSpace(line)
		}
	}
	return ""
}

func compactCandidateText(value string, limit int) string {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	if limit <= 0 || len(value) <= limit {
		return value
	}
	if limit <= 3 {
		return value[:limit]
	}
	return strings.TrimSpace(value[:limit-3]) + "..."
}

func runtimeLaunchIDFromEvidenceURI(value string) (uuid.UUID, bool) {
	raw := strings.TrimSpace(value)
	if raw == "" {
		return uuid.Nil, false
	}
	if !strings.HasPrefix(raw, "automation-launch://") {
		return uuid.Nil, false
	}
	raw = strings.TrimPrefix(raw, "automation-launch://")
	raw = strings.Trim(raw, "/")
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, false
	}
	return id, true
}

func (s *service) linkOptionalUUID(pursuitID uuid.UUID, linkType, rawID, relationship string, request AutoLinkWorkflowRequest, confidence float64, actor string) (*models.PursuitLink, bool, error) {
	id := strings.TrimSpace(rawID)
	if id == "" {
		return nil, false, nil
	}
	if _, err := uuid.Parse(id); err != nil {
		return nil, false, nil
	}
	link, err := s.Link(pursuitID, LinkRequest{
		OwnerIdentity: request.OwnerIdentity,
		LinkType:      linkType,
		LinkID:        id,
		Relationship:  relationship,
		SourceURI:     request.SourceURI,
		SourceLabel:   request.SourceLabel,
		Confidence:    confidence,
		Actor:         actor,
	})
	if err != nil {
		return nil, false, err
	}
	return link, true, nil
}

func autoLinkMinimum(value float64) float64 {
	if value <= 0 || value > 1 {
		return defaultAutoLinkMinimumScore
	}
	return value
}

func (s *service) Intake(id uuid.UUID, request IntakeRequest) (*PursuitDetail, error) {
	return s.IntakeForOwner("", id, request)
}

func (s *service) IntakeForOwner(ownerIdentity string, id uuid.UUID, request IntakeRequest) (*PursuitDetail, error) {
	pursuit, err := s.repo.FindByID(id)
	if err != nil {
		return nil, err
	}
	if !pursuitMutableBy(*pursuit, ownerIdentity) {
		return nil, fmt.Errorf("pursuit not found")
	}
	if err := ensurePursuitOpen(*pursuit, "add operational work to"); err != nil {
		return nil, err
	}
	if isPursuitCandidate(*pursuit) {
		return nil, fmt.Errorf("pursuit candidate must be accepted through the explicit plan action before adding operational work")
	}
	effectiveOwner := firstNonEmpty(pursuit.OwnerIdentity, ownerIdentity, request.OwnerIdentity)
	coordinationPlan, err := s.resolvePursuitCoordinationPlan(effectiveOwner, pursuit.ID, request.CoordinationPlan)
	if err != nil {
		return nil, err
	}
	detail, err := s.DetailForOwner(ownerIdentity, id)
	if err != nil {
		return nil, err
	}
	if reason := s.newWorkBlockerReasonForOwner(ownerIdentity, *pursuit, detail.Workflows); reason != "" {
		return nil, fmt.Errorf("pursuit cannot accept new operational work: %s", reason)
	}
	if strings.TrimSpace(request.Input) == "" {
		return nil, fmt.Errorf("input is required")
	}
	if s.workflowService == nil {
		return nil, fmt.Errorf("workflow service is not configured")
	}
	record, err := s.workflowService.Intake(workflow.IntakeRequest{
		OwnerIdentity:    effectiveOwner,
		Input:            request.Input,
		ProjectKey:       firstNonEmpty(request.ProjectKey, pursuit.ProjectKey),
		AutomationID:     request.AutomationID,
		MandateID:        request.MandateID,
		SourceType:       request.SourceType,
		SourceID:         request.SourceID,
		SourceURI:        request.SourceURI,
		SourceLabel:      request.SourceLabel,
		ContentType:      request.ContentType,
		Sender:           request.Sender,
		ReceivedAt:       request.ReceivedAt,
		Trigger:          firstNonEmpty(request.Trigger, "pursuit_intake"),
		Actor:            firstNonEmpty(request.Actor, "operator"),
		RequiresReview:   request.RequiresReview,
		ReviewReason:     request.ReviewReason,
		CoordinationPlan: coordinationReferenceFromBinding(coordinationPlan),
	})
	if err != nil {
		return nil, err
	}
	if record != nil {
		_, err = s.Link(id, LinkRequest{
			OwnerIdentity: effectiveOwner,
			LinkType:      LinkWorkflow,
			LinkID:        record.Item.ID.String(),
			Relationship:  "operational_work",
			SourceURI:     request.SourceURI,
			SourceLabel:   request.SourceLabel,
			Confidence:    0.9,
			Actor:         "system",
		})
		if err != nil {
			return nil, err
		}
	}
	if err := s.linkIntakeSourceReference(id, effectiveOwner, request); err != nil {
		return nil, err
	}
	if err := s.linkAssistantCommandReference(id, effectiveOwner, request.SourceType, request.SourceID, request.SourceURI, request.SourceLabel, firstNonEmpty(request.Actor, "system")); err != nil {
		return nil, err
	}
	if launchID, ok := runtimeLaunchIDFromEvidenceURI(request.SourceURI); ok {
		if _, err := s.Link(id, LinkRequest{
			OwnerIdentity: effectiveOwner,
			LinkType:      LinkAgentRuntime,
			LinkID:        launchID.String(),
			Relationship:  "execution_attempt",
			SourceURI:     request.SourceURI,
			SourceLabel:   firstNonEmpty(request.SourceLabel, "Runtime launch evidence"),
			Confidence:    0.95,
			Actor:         "system",
		}); err != nil {
			return nil, err
		}
	}
	if strings.EqualFold(request.SourceType, "pursuit_decision") && strings.TrimSpace(request.SourceID) != "" {
		_, _ = s.recordDecisionResolution(id, DecisionResolutionRequest{
			DecisionID:    request.SourceID,
			DecisionType:  request.ContentType,
			Approved:      true,
			Reason:        request.ReviewReason,
			Note:          "Decision approved and converted into governed workflow intake.",
			EvidenceURI:   request.SourceURI,
			EvidenceLabel: request.SourceLabel,
			Actor:         firstNonEmpty(request.Actor, "Robert"),
		})
	}
	return s.RefreshSummaryForOwner(ownerIdentity, id, "system")
}

func (s *service) linkIntakeSourceReference(id uuid.UUID, ownerIdentity string, request IntakeRequest) error {
	sourceID := strings.TrimSpace(request.SourceID)
	if sourceID == "" {
		return nil
	}
	linkType := ""
	relationship := ""
	switch strings.TrimSpace(request.SourceType) {
	case LinkSourceItem:
		linkType = LinkSourceItem
		relationship = "source_record"
	case LinkSourceExtraction:
		linkType = LinkSourceExtraction
		relationship = "source_extraction"
	default:
		return nil
	}
	if _, err := uuid.Parse(sourceID); err != nil {
		return nil
	}
	_, err := s.Link(id, LinkRequest{
		OwnerIdentity: ownerIdentity,
		LinkType:      linkType,
		LinkID:        sourceID,
		Relationship:  relationship,
		SourceURI:     request.SourceURI,
		SourceLabel:   request.SourceLabel,
		Confidence:    0.9,
		Actor:         "system",
	})
	return err
}

func (s *service) linkAssistantCommandReference(id uuid.UUID, ownerIdentity, sourceType, sourceID, sourceURI, sourceLabel, actor string) error {
	if !strings.EqualFold(strings.TrimSpace(sourceType), LinkAssistantCommand) || strings.TrimSpace(sourceID) == "" {
		return nil
	}
	_, err := s.Link(id, LinkRequest{
		OwnerIdentity: ownerIdentity,
		LinkType:      LinkAssistantCommand,
		LinkID:        strings.TrimSpace(sourceID),
		Relationship:  "command_origin",
		SourceURI:     strings.TrimSpace(sourceURI),
		SourceLabel:   firstNonEmpty(strings.TrimSpace(sourceLabel), "HAI chat command"),
		Confidence:    1,
		Actor:         firstNonEmpty(actor, "assistant"),
	})
	return err
}

// linkExactSourceReference preserves source identity for deterministic
// re-matching. This is distinct from the workflow link, which identifies the
// generated operational work rather than the input that produced it.
func (s *service) linkExactSourceReference(id uuid.UUID, ownerIdentity, sourceType, sourceID, sourceURI, sourceLabel string, confidence float64, actor string) (*models.PursuitLink, bool, error) {
	sourceType = strings.TrimSpace(sourceType)
	sourceID = strings.TrimSpace(sourceID)
	if sourceType == "" || sourceID == "" || strings.EqualFold(sourceType, LinkAssistantCommand) {
		return nil, false, nil
	}
	link, err := s.Link(id, LinkRequest{
		OwnerIdentity: ownerIdentity,
		LinkType:      sourceType,
		LinkID:        sourceID,
		Relationship:  "intake_origin",
		SourceURI:     strings.TrimSpace(sourceURI),
		SourceLabel:   strings.TrimSpace(sourceLabel),
		Confidence:    confidence,
		Actor:         firstNonEmpty(actor, "system"),
	})
	if err != nil {
		return nil, false, err
	}
	return link, true, nil
}

func (s *service) Plan(id uuid.UUID, request PlanRequest) (*PursuitDetail, error) {
	return s.PlanForOwner("", id, request)
}

func (s *service) PlanForOwner(ownerIdentity string, id uuid.UUID, request PlanRequest) (*PursuitDetail, error) {
	return s.planForOwner(ownerIdentity, id, request, false)
}

// AcceptCandidate is intentionally separate from generic planning. Callers
// must use an approval-gated boundary before invoking it; keeping this
// distinction in the service prevents future internal callers from silently
// turning a reviewable candidate into active operational work.
func (s *service) AcceptCandidate(id uuid.UUID, request PlanRequest) (*PursuitDetail, error) {
	return s.AcceptCandidateForOwner("", id, request)
}

func (s *service) AcceptCandidateForOwner(ownerIdentity string, id uuid.UUID, request PlanRequest) (*PursuitDetail, error) {
	return s.planForOwner(ownerIdentity, id, request, true)
}

func (s *service) planForOwner(ownerIdentity string, id uuid.UUID, request PlanRequest, acceptingCandidate bool) (*PursuitDetail, error) {
	pursuit, err := s.repo.FindByID(id)
	if err != nil {
		return nil, err
	}
	if !pursuitMutableBy(*pursuit, ownerIdentity) {
		return nil, fmt.Errorf("pursuit not found")
	}
	if err := ensurePursuitOpen(*pursuit, "plan"); err != nil {
		return nil, err
	}
	if isPursuitCandidate(*pursuit) && !acceptingCandidate {
		return nil, fmt.Errorf("pursuit candidate acceptance requires the explicit approval action")
	}
	if acceptingCandidate && !isPursuitCandidate(*pursuit) {
		return nil, fmt.Errorf("only an unaccepted pursuit candidate can use the candidate acceptance action")
	}
	effectiveOwner := firstNonEmpty(pursuit.OwnerIdentity, ownerIdentity)
	coordinationPlan, err := s.resolvePursuitCoordinationPlan(effectiveOwner, pursuit.ID, request.CoordinationPlan)
	if err != nil {
		return nil, err
	}
	existing, err := s.DetailForOwner(ownerIdentity, id)
	if err != nil {
		return nil, err
	}
	if !pursuitNeedsPlanning(*pursuit, len(existing.Workflows)) {
		if isPursuitCandidate(*pursuit) {
			if err := s.markPursuitCandidateAccepted(pursuit, firstNonEmpty(request.Actor, "Robert")); err != nil {
				return nil, err
			}
			return s.RefreshSummaryForOwner(ownerIdentity, id, firstNonEmpty(request.Actor, "pursuit-engine"))
		}
		return existing, nil
	}
	if reason := s.newWorkBlockerReasonForOwner(ownerIdentity, *pursuit, existing.Workflows); reason != "" {
		return nil, fmt.Errorf("pursuit planning is paused: %s", reason)
	}
	if s.workflowService == nil {
		return nil, fmt.Errorf("workflow service is not configured")
	}
	requiresReview := request.RequiresReview || strings.EqualFold(pursuit.RiskLevel, "high") || strings.EqualFold(pursuit.AutonomyLevel, "approve_before_execute")
	reviewReason := firstNonEmpty(request.ReviewReason, "first pursuit workflow plan should be reviewed before execution")
	if requiresReview && strings.EqualFold(pursuit.RiskLevel, "high") {
		reviewReason = firstNonEmpty(request.ReviewReason, "high-risk pursuit planning requires Robert approval before execution")
	}
	input := firstNonEmpty(request.Input, pursuitPlanInput(*pursuit))
	record, err := s.workflowService.Intake(workflow.IntakeRequest{
		OwnerIdentity:    effectiveOwner,
		Input:            input,
		ProjectKey:       pursuit.ProjectKey,
		MandateID:        pursuitMandateIDString(pursuit.MandateID),
		SourceType:       LinkPursuit,
		SourceID:         pursuit.ID.String(),
		SourceURI:        "pursuit://" + pursuit.ID.String(),
		SourceLabel:      pursuit.Title,
		ContentType:      "pursuit_plan",
		Trigger:          "pursuit_planning",
		Actor:            firstNonEmpty(request.Actor, "pursuit-engine"),
		RequiresReview:   requiresReview,
		ReviewReason:     reviewReason,
		CoordinationPlan: coordinationReferenceFromBinding(coordinationPlan),
	})
	if err != nil {
		return nil, err
	}
	if record != nil {
		if _, err := s.Link(id, LinkRequest{
			OwnerIdentity: effectiveOwner,
			LinkType:      LinkWorkflow,
			LinkID:        record.Item.ID.String(),
			Relationship:  "first_workflow_plan",
			SourceURI:     "pursuit://" + pursuit.ID.String(),
			SourceLabel:   pursuit.Title,
			Confidence:    0.95,
			Actor:         firstNonEmpty(request.Actor, "pursuit-engine"),
		}); err != nil {
			return nil, err
		}
		_, _ = s.recordActivity(id, "pursuit.planned", "Created first linked workflow plan: "+record.Item.Title, firstNonEmpty(request.Actor, "pursuit-engine"), LinkWorkflow, record.Item.ID.String(), "pursuit://"+pursuit.ID.String())
	}
	if isPursuitCandidate(*pursuit) {
		if err := s.markPursuitCandidateAccepted(pursuit, firstNonEmpty(request.Actor, "Robert")); err != nil {
			return nil, err
		}
	}
	return s.RefreshSummaryForOwner(ownerIdentity, id, firstNonEmpty(request.Actor, "pursuit-engine"))
}

func (s *service) ResolveDecision(id uuid.UUID, request DecisionResolutionRequest) (*PursuitDetail, error) {
	return s.ResolveDecisionForOwner("", id, request)
}

func (s *service) ResolveDecisionForOwner(ownerIdentity string, id uuid.UUID, request DecisionResolutionRequest) (*PursuitDetail, error) {
	pursuit, err := s.repo.FindByID(id)
	if err != nil {
		return nil, err
	}
	if !pursuitMutableBy(*pursuit, ownerIdentity) {
		return nil, fmt.Errorf("pursuit not found")
	}
	if err := ensurePursuitOpen(*pursuit, "resolve a decision for"); err != nil {
		return nil, err
	}
	if isPursuitCandidate(*pursuit) {
		return nil, fmt.Errorf("pursuit candidate must be accepted through the explicit approval action before resolving operational decisions")
	}
	if strings.TrimSpace(request.DecisionID) == "" {
		return nil, fmt.Errorf("decisionId is required")
	}
	if detail, err := s.DetailForOwner(ownerIdentity, id); err != nil {
		return nil, err
	} else if resolvedPursuitDecisions(detail.Activity)[strings.TrimSpace(request.DecisionID)] {
		// Decision requests can be retried by the UI or a client after a network
		// timeout. Returning the current detail avoids duplicating work or audit
		// records after the first governed resolution succeeded.
		return detail, nil
	}
	if request.Approved && strings.EqualFold(strings.TrimSpace(request.DecisionType), "pursuit_completion_review") {
		if err := s.completePursuitFromDecisionForOwner(ownerIdentity, id, request); err != nil {
			return nil, err
		}
	}
	if err := s.createApprovedDecisionWorkflowForOwner(ownerIdentity, id, request); err != nil {
		return nil, err
	}
	if _, err := s.recordDecisionResolution(id, request); err != nil {
		return nil, err
	}
	return s.RefreshSummaryForOwner(ownerIdentity, id, firstNonEmpty(request.Actor, "Robert"))
}

func (s *service) completePursuitFromDecision(id uuid.UUID, request DecisionResolutionRequest) error {
	return s.completePursuitFromDecisionForOwner("", id, request)
}

func (s *service) completePursuitFromDecisionForOwner(ownerIdentity string, id uuid.UUID, request DecisionResolutionRequest) error {
	expectedID := completionReviewDecisionID(id)
	if !strings.EqualFold(strings.TrimSpace(request.DecisionID), expectedID) {
		return fmt.Errorf("completion review decision does not belong to this pursuit")
	}
	pursuit, err := s.repo.FindByID(id)
	if err != nil {
		return err
	}
	if pursuitClosed(*pursuit) {
		return nil
	}
	if isPursuitCandidate(*pursuit) {
		return fmt.Errorf("pursuit candidate must be accepted and planned before completion review")
	}
	if reason, err := s.completionActiveBlockerReasonForOwner(ownerIdentity, id); err != nil {
		return err
	} else if reason != "" {
		return fmt.Errorf("pursuit completion is blocked by unresolved operational work: %s", reason)
	}
	allowed, reason, err := s.completionEvidenceAvailableForOwner(ownerIdentity, id)
	if err != nil {
		return err
	}
	if !allowed {
		return fmt.Errorf("pursuit completion requires verified evidence, linked verification, or a verified completed workflow before it can be marked complete: %s", reason)
	}
	now := time.Now().UTC()
	pursuit.Status = StatusCompleted
	pursuit.CompletionState = CompletionVerified
	pursuit.LastActivityAt = &now
	pursuit.CurrentStateSummary = firstNonEmpty(request.Note, "Marked complete after Robert approved the verified completion review.")
	pursuit.NextRecommendedAction = "No active next action; keep evidence available for audit or future review."
	if _, err := s.repo.Update(pursuit); err != nil {
		return err
	}
	_, _ = s.recordActivity(
		id,
		"pursuit.completed",
		completionDecisionMessage(request.Note),
		firstNonEmpty(request.Actor, "Robert"),
		"pursuit_decision:pursuit_completion_review:approved",
		strings.TrimSpace(request.DecisionID),
		request.EvidenceURI,
	)
	return nil
}

func completionDecisionMessage(note string) string {
	message := "Pursuit marked completed after verified evidence review."
	if strings.TrimSpace(note) != "" {
		message += " Note: " + strings.TrimSpace(note)
	}
	return message
}

func (s *service) createApprovedDecisionWorkflow(id uuid.UUID, request DecisionResolutionRequest) error {
	return s.createApprovedDecisionWorkflowForOwner("", id, request)
}

func (s *service) createApprovedDecisionWorkflowForOwner(ownerIdentity string, id uuid.UUID, request DecisionResolutionRequest) error {
	if !request.Approved {
		return nil
	}
	switch strings.ToLower(strings.TrimSpace(request.DecisionType)) {
	case "pursuit_next_action":
		return s.createNextActionWorkflowForOwner(ownerIdentity, id, request)
	case "runtime_attempt_review":
		return s.createRuntimeRecoveryWorkflowForOwner(ownerIdentity, id, request)
	default:
		return nil
	}
}

// createNextActionWorkflowForOwner is the only server-side path from a
// high-risk pursuit's Yes/No decision to operational work. It preserves the
// decision provenance and creates a separately approval-gated workflow rather
// than letting a client turn a dashboard action into direct execution.
func (s *service) createNextActionWorkflowForOwner(ownerIdentity string, id uuid.UUID, request DecisionResolutionRequest) error {
	if !strings.EqualFold(strings.TrimSpace(request.DecisionID), nextActionDecisionID(id)) {
		return fmt.Errorf("next-action decision does not belong to this pursuit")
	}
	if s.workflowService == nil {
		return fmt.Errorf("workflow service is not configured")
	}
	detail, err := s.DetailForOwner(ownerIdentity, id)
	if err != nil {
		return err
	}
	if !pendingDecision(detail.DecisionQueue, request.DecisionID, "pursuit_next_action") {
		return fmt.Errorf("next-action decision is not pending for this pursuit")
	}
	if !pursuitNeedsPlanning(detail.Pursuit, len(detail.Workflows)) {
		return nil
	}
	if reason := s.newWorkBlockerReasonForOwner(ownerIdentity, detail.Pursuit, detail.Workflows); reason != "" {
		return fmt.Errorf("approved pursuit action cannot create new work: %s", reason)
	}
	actor := firstNonEmpty(request.Actor, "Robert")
	sourceURI := firstNonEmpty(strings.TrimSpace(request.EvidenceURI), "pursuit://"+id.String())
	sourceLabel := firstNonEmpty(strings.TrimSpace(request.EvidenceLabel), detail.Pursuit.Title)
	record, err := s.workflowService.Intake(workflow.IntakeRequest{
		OwnerIdentity:  firstNonEmpty(detail.Pursuit.OwnerIdentity, ownerIdentity),
		Input:          pursuitPlanInput(detail.Pursuit),
		ProjectKey:     detail.Pursuit.ProjectKey,
		SourceType:     "pursuit_decision",
		SourceID:       strings.TrimSpace(request.DecisionID),
		SourceURI:      sourceURI,
		SourceLabel:    sourceLabel,
		ContentType:    "pursuit_next_action",
		Trigger:        "pursuit_decision_approved",
		Actor:          actor,
		RequiresReview: true,
		ReviewReason:   firstNonEmpty(request.Reason, "Robert approved creation of a governed workflow; consequential execution remains approval-gated"),
	})
	if err != nil {
		return err
	}
	if record == nil || record.Item.ID == uuid.Nil {
		return fmt.Errorf("workflow intake did not return a workflow record")
	}
	if _, err := s.Link(id, LinkRequest{
		OwnerIdentity: firstNonEmpty(detail.Pursuit.OwnerIdentity, ownerIdentity),
		LinkType:      LinkWorkflow,
		LinkID:        record.Item.ID.String(),
		Relationship:  "approved_next_action_workflow",
		SourceURI:     sourceURI,
		SourceLabel:   sourceLabel,
		Confidence:    1,
		Actor:         actor,
	}); err != nil {
		return err
	}
	_, _ = s.recordActivity(
		id,
		"pursuit.next_action_workflow_created",
		"Created governed workflow from Robert-approved next action: "+record.Item.Title,
		actor,
		LinkWorkflow,
		record.Item.ID.String(),
		sourceURI,
	)
	return nil
}

func pendingDecision(decisions []PursuitDecision, id, decisionType string) bool {
	for _, decision := range decisions {
		if strings.EqualFold(strings.TrimSpace(decision.ID), strings.TrimSpace(id)) &&
			strings.EqualFold(strings.TrimSpace(decision.DecisionType), strings.TrimSpace(decisionType)) &&
			strings.EqualFold(strings.TrimSpace(decision.Status), "pending") {
			return true
		}
	}
	return false
}

func (s *service) createRuntimeRecoveryWorkflow(id uuid.UUID, request DecisionResolutionRequest) error {
	return s.createRuntimeRecoveryWorkflowForOwner("", id, request)
}

func (s *service) createRuntimeRecoveryWorkflowForOwner(ownerIdentity string, id uuid.UUID, request DecisionResolutionRequest) error {
	if s.workflowService == nil {
		return fmt.Errorf("workflow service is not configured")
	}
	detail, err := s.DetailForOwner(ownerIdentity, id)
	if err != nil {
		return err
	}
	attempt, ok := runtimeAttemptForDecision(detail.RuntimeAttempts, request)
	if !ok {
		return fmt.Errorf("runtime attempt evidence is required before creating a recovery workflow")
	}
	sourceURI := "automation-launch://" + attempt.ID.String()
	if runtimeAttemptHasRecoveryWorkflow(attempt, detail.Workflows) {
		return nil
	}
	record, err := s.workflowService.Intake(workflow.IntakeRequest{
		OwnerIdentity:  firstNonEmpty(detail.Pursuit.OwnerIdentity, ownerIdentity),
		Input:          runtimeRecoveryWorkflowInput(detail.Pursuit, attempt, request),
		ProjectKey:     detail.Pursuit.ProjectKey,
		SourceType:     "pursuit_decision",
		SourceID:       strings.TrimSpace(request.DecisionID),
		SourceURI:      sourceURI,
		SourceLabel:    firstNonEmpty(request.EvidenceLabel, runtimeAttemptLabel(attempt)),
		ContentType:    "runtime_attempt_review",
		Trigger:        "pursuit_decision_approved",
		Actor:          firstNonEmpty(request.Actor, "Robert"),
		RequiresReview: true,
		ReviewReason:   firstNonEmpty(request.Reason, runtimeAttemptReason(attempt), "runtime attempt needs governed recovery before retry"),
	})
	if err != nil {
		return err
	}
	if record != nil {
		if _, err := s.Link(id, LinkRequest{
			OwnerIdentity: firstNonEmpty(detail.Pursuit.OwnerIdentity, ownerIdentity),
			LinkType:      LinkWorkflow,
			LinkID:        record.Item.ID.String(),
			Relationship:  "runtime_recovery_workflow",
			SourceURI:     sourceURI,
			SourceLabel:   firstNonEmpty(request.EvidenceLabel, runtimeAttemptLabel(attempt)),
			Confidence:    0.95,
			Actor:         firstNonEmpty(request.Actor, "pursuit-engine"),
		}); err != nil {
			return err
		}
		_, _ = s.recordActivity(
			id,
			"pursuit.runtime_recovery_workflow_created",
			"Created governed recovery workflow for runtime attempt: "+runtimeAttemptLabel(attempt),
			firstNonEmpty(request.Actor, "pursuit-engine"),
			LinkWorkflow,
			record.Item.ID.String(),
			sourceURI,
		)
	}
	return nil
}

func (s *service) RefreshSummary(id uuid.UUID, actor string) (*PursuitDetail, error) {
	return s.RefreshSummaryForOwner("", id, actor)
}

func (s *service) RefreshSummaryForOwner(ownerIdentity string, id uuid.UUID, actor string) (*PursuitDetail, error) {
	detail, err := s.DetailForOwner(ownerIdentity, id)
	if err != nil {
		return nil, err
	}
	if !pursuitMutableBy(detail.Pursuit, ownerIdentity) {
		return nil, fmt.Errorf("pursuit not found")
	}
	if pursuitClosed(detail.Pursuit) {
		return detail, nil
	}
	pursuit := detail.Pursuit
	pursuit.CurrentStateSummary = detail.Summary.CurrentState
	if len(detail.NextActions) > 0 {
		pursuit.NextRecommendedAction = detail.NextActions[0].Label
	}
	if detail.Summary.Blocked > 0 {
		pursuit.Status = StatusBlocked
	} else if detail.Summary.CompletionCandidate {
		pursuit.CompletionState = CompletionCandidate
	} else if len(detail.OpenLoops) > 0 {
		pursuit.Status = StatusWaiting
	} else if pursuit.Status == StatusBlocked || pursuit.Status == StatusWaiting {
		pursuit.Status = StatusActive
	}
	updated, err := s.repo.Update(&pursuit)
	if err != nil {
		return nil, err
	}
	_, _ = s.recordActivity(id, "pursuit.summary_refreshed", "Pursuit summary refreshed from linked operational records", firstNonEmpty(actor, "system"), "", "", "")
	detail.Pursuit = *updated
	return s.DetailForOwner(ownerIdentity, id)
}

func (s *service) Review(id uuid.UUID, request ReviewRequest) (*PursuitDetail, error) {
	return s.ReviewForOwner("", id, request)
}

func (s *service) ReviewForOwner(ownerIdentity string, id uuid.UUID, request ReviewRequest) (*PursuitDetail, error) {
	pursuit, err := s.repo.FindByID(id)
	if err != nil {
		return nil, err
	}
	if !pursuitMutableBy(*pursuit, ownerIdentity) {
		return nil, fmt.Errorf("pursuit not found")
	}
	now := time.Now().UTC()
	action := strings.ToLower(strings.TrimSpace(firstNonEmpty(request.Action, "complete")))
	actor := firstNonEmpty(request.Actor, "robert")
	note := strings.TrimSpace(request.Note)
	nextReviewAt := parseOptionalTime(request.NextReviewAt)

	switch action {
	case "complete", "reviewed", "done":
		if nextReviewAt == nil {
			days := clampInt(pursuit.ReviewCadenceDays, 1, 365, 7)
			next := now.Add(time.Duration(days) * 24 * time.Hour)
			nextReviewAt = &next
		}
		pursuit.NextReviewAt = nextReviewAt
		pursuit.LastActivityAt = &now
		pursuit.CurrentStateSummary = firstNonEmpty(note, fmt.Sprintf("Review completed; next review scheduled for %s.", pursuit.NextReviewAt.Format("2006-01-02")), pursuit.CurrentStateSummary)
		updated, err := s.repo.Update(pursuit)
		if err != nil {
			return nil, err
		}
		_, _ = s.recordActivity(id, "pursuit.reviewed", firstNonEmpty(note, fmt.Sprintf("Review completed; next review scheduled for %s", updated.NextReviewAt.Format("2006-01-02"))), actor, "", "", "")
	case "snooze", "postpone":
		if nextReviewAt == nil {
			days := clampInt(request.SnoozeDays, 1, 90, 3)
			next := now.Add(time.Duration(days) * 24 * time.Hour)
			nextReviewAt = &next
		}
		pursuit.NextReviewAt = nextReviewAt
		pursuit.LastActivityAt = &now
		pursuit.CurrentStateSummary = firstNonEmpty(note, fmt.Sprintf("Review snoozed until %s.", pursuit.NextReviewAt.Format("2006-01-02")), pursuit.CurrentStateSummary)
		updated, err := s.repo.Update(pursuit)
		if err != nil {
			return nil, err
		}
		_, _ = s.recordActivity(id, "pursuit.review_snoozed", firstNonEmpty(note, fmt.Sprintf("Review snoozed until %s", updated.NextReviewAt.Format("2006-01-02"))), actor, "", "", "")
	default:
		return nil, fmt.Errorf("unsupported pursuit review action %q", request.Action)
	}

	return s.DetailForOwner(ownerIdentity, id)
}

func (s *service) Activity(id uuid.UUID) ([]models.PursuitActivity, error) {
	return s.ActivityForOwner("", id)
}

// ActivityForOwner keeps the audit feed subject to the same ownership rule as
// pursuit detail. This is intentionally enforced by the service, not just the
// HTTP handler, because the feed contains source-derived operational history.
func (s *service) ActivityForOwner(ownerIdentity string, id uuid.UUID) ([]models.PursuitActivity, error) {
	if _, err := s.DetailForOwner(ownerIdentity, id); err != nil {
		return nil, err
	}
	return s.repo.FindActivities(id, 100)
}

func (s *service) listItem(pursuit models.Pursuit) (PursuitListItem, error) {
	item, _, err := s.listItemWithDetail(pursuit)
	return item, err
}

func (s *service) listItemWithDetail(pursuit models.Pursuit) (PursuitListItem, *PursuitDetail, error) {
	detail, err := s.Detail(pursuit.ID)
	return pursuitListItemWithDetail(pursuit, detail, err)
}

// listItemWithDetailForOwner keeps dashboard projections subject to the same
// linked-record visibility checks as the underlying pursuit detail endpoint.
// This protects owner dashboards from malformed links created before
// owner-aware link validation was introduced.
func (s *service) listItemWithDetailForOwner(ownerIdentity string, pursuit models.Pursuit) (PursuitListItem, *PursuitDetail, error) {
	detail, err := s.DetailForOwner(ownerIdentity, pursuit.ID)
	return pursuitListItemWithDetail(pursuit, detail, err)
}

func pursuitListItemWithDetail(pursuit models.Pursuit, detail *PursuitDetail, err error) (PursuitListItem, *PursuitDetail, error) {
	if err != nil {
		return PursuitListItem{Pursuit: pursuit, NextAction: pursuit.NextRecommendedAction, EffectiveLastActivityAt: pursuit.LastActivityAt, Stale: isStale(pursuit), ReviewDue: isReviewDue(pursuit), PlanningNeeded: pursuitNeedsPlanning(pursuit, 0)}, nil, err
	}
	effectiveActivityAt := effectivePursuitActivity(pursuit, detail)
	needsRobert := len(detail.ApprovalItems)
	if pursuitNeedsRobert(pursuit, detail.NextActions) {
		needsRobert++
	}
	if detail.Summary.NeedsRobert > needsRobert {
		needsRobert = detail.Summary.NeedsRobert
	}
	return PursuitListItem{
		Pursuit:                 pursuit,
		NeedsRobert:             needsRobert,
		Blocked:                 len(detail.Blockers),
		OpenLoops:               len(detail.OpenLoops),
		DecisionCards:           detail.Summary.DecisionCards,
		LinkedEvidence:          detail.Summary.LinkedEvidence,
		TimelineItems:           detail.Summary.TimelineItems,
		CompletionCandidate:     detail.Summary.CompletionCandidate,
		CurrentState:            detail.Summary.CurrentState,
		WhatChanged:             detail.Summary.WhatChanged,
		NextAction:              firstNonEmpty(pursuit.NextRecommendedAction, firstActionLabel(detail.NextActions)),
		EffectiveLastActivityAt: optionalTime(effectiveActivityAt),
		Stale:                   isStaleAt(effectiveActivityAt),
		ReviewDue:               isReviewDue(pursuit),
		PlanningNeeded:          detail.Summary.PlanningNeeded,
	}, detail, nil
}

func detailUnavailableListItem(pursuit models.Pursuit) PursuitListItem {
	return PursuitListItem{
		Pursuit:                 pursuit,
		NeedsRobert:             1,
		Blocked:                 1,
		CurrentState:            "Linked operational state is temporarily unavailable; do not advance this pursuit until it can be reviewed.",
		WhatChanged:             "HAI could not refresh the linked operational state.",
		NextAction:              "Retry the pursuit detail after checking HAI service health.",
		EffectiveLastActivityAt: pursuit.LastActivityAt,
		Stale:                   false,
		ReviewDue:               isReviewDue(pursuit),
		PlanningNeeded:          false,
	}
}

func pursuitDetailLoadError(component string, err error) error {
	return fmt.Errorf("load pursuit %s: %w", component, err)
}

// effectivePursuitActivity derives dashboard freshness from evidence that is
// already linked to a pursuit. It intentionally does not write the derived
// value back during a read: summary refreshes must not make stale work appear
// active, while real workflow, task, verification, source, and runtime work
// must keep a pursuit out of the stale queue.
func effectivePursuitActivity(pursuit models.Pursuit, detail *PursuitDetail) time.Time {
	// LastActivityAt is the semantic progress marker. UpdatedAt also changes
	// when HAI writes a derived summary, so it is a legacy fallback only when a
	// pursuit has never recorded an activity timestamp.
	latest := timeFromPointer(pursuit.LastActivityAt)
	if latest.IsZero() {
		latest = pursuit.UpdatedAt
	}
	if detail == nil {
		return latest
	}
	for _, item := range detail.Workflows {
		latest = latestTime(latest, item.UpdatedAt, timeFromPointer(item.LastRunAt), timeFromPointer(item.CompletedAt))
	}
	for _, item := range detail.OpenLoops {
		latest = latestTime(latest, item.UpdatedAt)
	}
	for _, item := range detail.TaskAttempts {
		latest = latestTime(latest, item.UpdatedAt, timeFromPointer(item.CompletedAt), timeFromPointer(item.StartedAt))
	}
	for _, item := range detail.VerificationRuns {
		latest = latestTime(latest, item.UpdatedAt, item.CreatedAt)
	}
	for _, item := range detail.RuntimeAttempts {
		latest = latestTime(latest, item.CompletedAt, item.StartedAt)
	}
	for _, item := range detail.SourceItems {
		latest = latestTime(latest, item.UpdatedAt, item.FetchedAt, item.CreatedAt)
	}
	for _, item := range detail.SourceExtractions {
		latest = latestTime(latest, item.UpdatedAt, item.CreatedAt)
	}
	for _, item := range detail.Activity {
		if !activityUpdatesFreshness(item.EventType) {
			continue
		}
		latest = latestTime(latest, item.CreatedAt)
	}
	return latest
}

func latestTime(current time.Time, values ...time.Time) time.Time {
	for _, value := range values {
		if value.After(current) {
			current = value
		}
	}
	return current
}

func optionalTime(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	copy := value
	return &copy
}

func (s *service) recordActivity(id uuid.UUID, eventType, message, actor, sourceType, sourceID, sourceURI string) (*models.PursuitActivity, error) {
	return s.recordActivityIdempotent(id, eventType, message, actor, sourceType, sourceID, sourceURI, "")
}

func (s *service) recordActivityIdempotent(
	id uuid.UUID,
	eventType, message, actor, sourceType, sourceID, sourceURI, idempotencyKey string,
) (*models.PursuitActivity, error) {
	activity, err := s.repo.CreateActivity(&models.PursuitActivity{
		PursuitID:      id,
		EventType:      eventType,
		Message:        strings.TrimSpace(message),
		Actor:          strings.TrimSpace(actor),
		SourceType:     strings.TrimSpace(sourceType),
		SourceID:       strings.TrimSpace(sourceID),
		SourceURI:      strings.TrimSpace(sourceURI),
		IdempotencyKey: strings.TrimSpace(idempotencyKey),
	})
	if err != nil {
		return activity, err
	}
	if activity == nil {
		return nil, fmt.Errorf("pursuit activity repository returned no activity")
	}

	pursuit, err := s.repo.FindByID(id)
	if err != nil {
		return activity, err
	}
	activityAt := activity.CreatedAt
	if activityAt.IsZero() {
		activityAt = time.Now().UTC()
	}
	if activityUpdatesFreshness(eventType) && (pursuit.LastActivityAt == nil || pursuit.LastActivityAt.Before(activityAt)) {
		pursuit.LastActivityAt = &activityAt
		if _, err := s.repo.Update(pursuit); err != nil {
			return activity, err
		}
	}
	return activity, nil
}

func activityUpdatesFreshness(eventType string) bool {
	return !strings.EqualFold(strings.TrimSpace(eventType), "pursuit.summary_refreshed")
}

func (s *service) recordDecisionResolution(id uuid.UUID, request DecisionResolutionRequest) (*models.PursuitActivity, error) {
	decisionID := strings.TrimSpace(request.DecisionID)
	if decisionID == "" {
		return nil, fmt.Errorf("decisionId is required")
	}
	outcome := "rejected"
	if request.Approved {
		outcome = "approved"
	}
	message := firstNonEmpty(request.Note, fmt.Sprintf("Robert %s pursuit decision %s.", outcome, decisionID))
	if strings.TrimSpace(request.Reason) != "" {
		message = message + " Reason: " + strings.TrimSpace(request.Reason)
	}
	if strings.TrimSpace(request.EvidenceLabel) != "" {
		message = message + " Evidence: " + strings.TrimSpace(request.EvidenceLabel)
	}
	return s.recordActivity(
		id,
		"pursuit.decision_resolved",
		message,
		firstNonEmpty(request.Actor, "Robert"),
		"pursuit_decision:"+strings.TrimSpace(request.DecisionType)+":"+outcome,
		decisionID,
		request.EvidenceURI,
	)
}

func approvalWorkflows(workflows []models.WorkflowItem) []models.WorkflowItem {
	result := []models.WorkflowItem{}
	for _, item := range workflows {
		if item.RequiresApproval && (item.ApprovalStatus == "" || item.ApprovalStatus == "pending") {
			result = append(result, item)
		}
	}
	return result
}

func openWorkflowProposals(proposals []models.WorkflowProposal) []models.WorkflowProposal {
	result := []models.WorkflowProposal{}
	for _, proposal := range proposals {
		if proposal.Status == "open" || proposal.Status == "pending" || proposal.Status == "" {
			result = append(result, proposal)
		}
	}
	return result
}

func unresolvedWorkflowDecisions(decisions []models.WorkflowDecision) []models.WorkflowDecision {
	result := []models.WorkflowDecision{}
	for _, decision := range decisions {
		status := decisionStatus(decision)
		if status == "needs_review" || status == "rejected" {
			result = append(result, decision)
		}
	}
	return result
}

func blockers(workflows []models.WorkflowItem, loops []models.WorkflowOpenLoop) []PursuitBlocker {
	result := []PursuitBlocker{}
	for _, item := range workflows {
		if item.CurrentState == workflow.StateBlocked || strings.TrimSpace(item.BlockedReason) != "" {
			result = append(result, PursuitBlocker{
				Label:      item.Title,
				Reason:     firstNonEmpty(item.BlockedReason, "workflow is blocked"),
				Owner:      "Robert or external party",
				WorkflowID: item.ID.String(),
			})
		}
	}
	now := time.Now().UTC()
	for _, loop := range loops {
		if loop.Status == "open" || loop.Status == "follow_up_due" || loop.FollowUpAt != nil && loop.FollowUpAt.Before(now) {
			result = append(result, PursuitBlocker{
				Label:      loop.WaitingFor,
				Reason:     loop.NextAction,
				Owner:      firstNonEmpty(loop.ResponsibleParty, "external"),
				WorkflowID: loop.WorkflowID.String(),
				FollowUpAt: loop.FollowUpAt,
			})
		}
	}
	return result
}

func sourceRetractionBlockers(links []models.PursuitLink, extractions []models.SourceExtraction) []PursuitBlocker {
	expected := map[uuid.UUID]models.PursuitLink{}
	for _, link := range links {
		if link.LinkType != LinkSourceExtraction {
			continue
		}
		if !sourceExtractionLinkRequiresEvidenceReview(link.Relationship) {
			continue
		}
		id, err := uuid.Parse(strings.TrimSpace(link.LinkID))
		if err != nil {
			continue
		}
		expected[id] = link
	}
	if len(expected) == 0 {
		return nil
	}
	found := map[uuid.UUID]models.SourceExtraction{}
	for _, extraction := range extractions {
		found[extraction.ID] = extraction
	}
	result := []PursuitBlocker{}
	for id, link := range expected {
		extraction, ok := found[id]
		if !ok {
			result = append(result, PursuitBlocker{
				Label:  firstNonEmpty(link.SourceLabel, "Missing source extraction"),
				Reason: "linked source extraction is missing; verify or remove the stale evidence link before relying on this pursuit",
				Owner:  "Robert or source owner",
			})
			continue
		}
		if extraction.Archived {
			result = append(result, PursuitBlocker{
				Label:  firstNonEmpty(extraction.SourceLabel, link.SourceLabel, extraction.Summary, "Archived source extraction"),
				Reason: "linked source extraction was archived and must be reviewed before relying on it as evidence",
				Owner:  "Robert or source owner",
			})
		}
	}
	sort.SliceStable(result, func(i, j int) bool {
		return result[i].Label < result[j].Label
	})
	return result
}

func qualityGateBlockers(gates []models.WorkflowQualityGate) []PursuitBlocker {
	result := []PursuitBlocker{}
	for _, gate := range gates {
		status := strings.ToLower(strings.TrimSpace(gate.Status))
		if status != "failed" && status != "needs_review" {
			continue
		}
		result = append(result, PursuitBlocker{
			Label:      "Quality gate: " + firstNonEmpty(gate.Gate, "workflow acceptance"),
			Reason:     firstNonEmpty(gate.Reason, "linked workflow quality gate requires review before the pursuit can move forward"),
			Owner:      "Robert or task owner",
			WorkflowID: gate.WorkflowID.String(),
		})
	}
	sort.SliceStable(result, func(i, j int) bool {
		return result[i].Label < result[j].Label
	})
	return result
}

func sourceExtractionStatus(extraction models.SourceExtraction) string {
	if extraction.Archived {
		return "archived"
	}
	if extraction.Uncertain {
		return "uncertain"
	}
	return "active"
}

func sourceExtractionLinkRequiresEvidenceReview(relationship string) bool {
	relationship = strings.ToLower(strings.TrimSpace(relationship))
	if relationship == "" {
		return true
	}
	if strings.Contains(relationship, "candidate") {
		return false
	}
	return strings.Contains(relationship, "evidence") ||
		strings.Contains(relationship, "source") ||
		strings.Contains(relationship, "provenance")
}

func nextActions(pursuit models.Pursuit, workflows []models.WorkflowItem, loops []models.WorkflowOpenLoop, proposals []models.WorkflowProposal, runtimeAttempts []models.AutomationLaunchEvent, resolvedDecisions map[string]bool, qualityGateNeedsReview bool, newWorkBlocked bool) []PursuitAction {
	actions := []PursuitAction{}
	if pursuitClosed(pursuit) {
		return actions
	}
	if isPursuitCandidate(pursuit) {
		actions = append(actions, PursuitAction{
			Label:            firstNonEmpty(pursuit.NextRecommendedAction, "Review this auto-created pursuit candidate and decide whether HAI should plan it."),
			Owner:            "Robert",
			RiskLevel:        firstNonEmpty(pursuit.RiskLevel, "low"),
			RequiresApproval: true,
			Reason:           "auto-created pursuit candidates must be accepted before HAI treats them as planned work",
			YesLabel:         "Accept and plan",
			NoLabel:          "Archive candidate",
		})
	}
	if !pursuitGoalContractReady(pursuit) {
		actions = append(actions, PursuitAction{
			Label:            "Define measurable success criteria and safe stop conditions",
			Owner:            "Robert",
			RiskLevel:        "low",
			RequiresApproval: false,
			Reason:           "the pursuit needs an explicit outcome contract before HAI can verify completion and control resource use",
			YesLabel:         "Define contract",
			NoLabel:          "Keep draft",
		})
	}
	if reason := triggeredStopConditionReason(pursuit); reason != "" {
		actions = append(actions, PursuitAction{
			Label:            "Review the triggered stop condition before continuing",
			Owner:            "Robert",
			RiskLevel:        firstNonEmpty(pursuit.RiskLevel, "medium"),
			RequiresApproval: true,
			Reason:           reason,
			YesLabel:         "Review stop",
			NoLabel:          "Keep paused",
		})
	}
	if isReviewDue(pursuit) {
		requiresApproval := strings.EqualFold(pursuit.RiskLevel, "high") || strings.EqualFold(pursuit.AutonomyLevel, "approve_before_execute")
		actions = append(actions, PursuitAction{
			Label:            firstNonEmpty(pursuit.NextRecommendedAction, "Review this pursuit and choose the next concrete action"),
			Owner:            reviewOwner(pursuit),
			RiskLevel:        pursuit.RiskLevel,
			RequiresApproval: requiresApproval,
			Reason:           "scheduled pursuit review is due",
			YesLabel:         "Review now",
			NoLabel:          "Snooze",
		})
	}
	for _, item := range workflows {
		if item.RequiresApproval && (item.ApprovalStatus == "" || item.ApprovalStatus == "pending") {
			actions = append(actions, PursuitAction{
				Label:            firstNonEmpty(item.NextAction, item.ApprovalReason, "Review and approve workflow: "+item.Title),
				Owner:            "Robert",
				RiskLevel:        firstNonEmpty(item.RiskLevel, pursuit.RiskLevel),
				RequiresApproval: true,
				Reason:           firstNonEmpty(item.ApprovalReason, "workflow policy requires human approval"),
				WorkflowID:       item.ID.String(),
				YesLabel:         "Approve",
				NoLabel:          "Reject",
			})
		}
	}
	for _, proposal := range openWorkflowProposals(proposals) {
		actions = append(actions, PursuitAction{
			Label:            proposal.RecommendedAction,
			Owner:            "Robert",
			RiskLevel:        pursuit.RiskLevel,
			RequiresApproval: true,
			Reason:           "open proposal needs a decision",
			WorkflowID:       proposal.WorkflowID.String(),
			YesLabel:         "Accept",
			NoLabel:          "Decline",
		})
	}
	now := time.Now().UTC()
	for _, loop := range loops {
		if loop.Status == "open" || loop.Status == "follow_up_due" || loop.FollowUpAt != nil && loop.FollowUpAt.Before(now) {
			actionRisk := followUpActionRisk(pursuit.RiskLevel, loop.NextAction)
			actions = append(actions, PursuitAction{
				Label:            firstNonEmpty(loop.NextAction, "Follow up on waiting state"),
				Owner:            "VA",
				RiskLevel:        actionRisk,
				RequiresApproval: actionRisk == "high",
				Reason:           "open loop is due or still waiting",
				WorkflowID:       loop.WorkflowID.String(),
				YesLabel:         "Prepare",
				NoLabel:          "Ignore for now",
			})
		}
	}
	for _, attempt := range runtimeAttempts {
		if runtimeAttemptRecoveredByWorkflow(attempt, workflows) {
			continue
		}
		if !runtimeAttemptNeedsReview(attempt) || resolvedDecisions[runtimeAttemptDecisionID(attempt)] {
			continue
		}
		actions = append(actions, PursuitAction{
			Label:            "Review failed runtime attempt before retrying",
			Owner:            "Robert",
			RiskLevel:        firstNonEmpty(pursuit.RiskLevel, "medium"),
			RequiresApproval: true,
			Reason:           runtimeAttemptReason(attempt),
			YesLabel:         "Create recovery workflow",
			NoLabel:          "Keep blocked",
		})
		break
	}
	if len(workflows) == 0 && !newWorkBlocked {
		actions = append(actions, PursuitAction{
			Label:            "Create the first workflow item for this pursuit",
			Owner:            "System",
			RiskLevel:        "low",
			RequiresApproval: false,
			Reason:           "pursuit has no linked operational work yet",
			YesLabel:         "Create workflow",
			NoLabel:          "Not now",
		})
	}
	if len(actions) == 0 && !qualityGateNeedsReview && workflowsReadyForCompletion(workflows) {
		actions = append(actions, PursuitAction{
			Label:            "Review verified evidence and mark pursuit complete",
			Owner:            "Robert",
			RiskLevel:        firstNonEmpty(pursuit.RiskLevel, "medium"),
			RequiresApproval: true,
			Reason:           "all linked workflows are complete and accepted verification evidence is available",
			YesLabel:         "Mark complete",
			NoLabel:          "Keep active",
		})
	}
	if len(actions) == 0 && !qualityGateNeedsReview && !newWorkBlocked {
		if resolvedDecisions["pursuit:"+pursuit.ID.String()+":next-action"] {
			return actions
		}
		actions = append(actions, PursuitAction{
			Label:            firstNonEmpty(pursuit.NextRecommendedAction, "Review this pursuit and confirm the next concrete action"),
			Owner:            "Robert",
			RiskLevel:        pursuit.RiskLevel,
			RequiresApproval: strings.EqualFold(pursuit.RiskLevel, "high"),
			Reason:           "no active blocker or approval was found",
			YesLabel:         "Confirm",
			NoLabel:          "Revise",
		})
	}
	return actions
}

// followUpActionRisk classifies the proposed step, not the subject matter of
// the entire pursuit. A legal or insurance pursuit can safely contain bounded
// clerical preparation, while sending, filing, spending, publishing, deleting,
// or changing accounts must still remain high-risk and approval-gated.
func followUpActionRisk(pursuitRisk, action string) string {
	lower := strings.ToLower(strings.TrimSpace(action))
	if containsActionPhrase(lower,
		"send", "submit", "file", "publish", "post", "sign", "accept", "agree",
		"pay", "spend", "transfer", "delete", "remove", "change account", "change setting",
		"escalate", "contact", "call", "message", "email") {
		return "high"
	}
	if containsActionPhrase(lower,
		"prepare", "organize", "collect", "list", "summarize", "classify", "catalog",
		"review", "compare", "extract", "attach", "draft", "research") {
		return "low"
	}
	if detected := classifyRisk(lower); detected != "low" {
		return detected
	}
	if normalizeRisk(pursuitRisk) == "high" {
		return "high"
	}
	return firstNonEmpty(normalizeRisk(pursuitRisk), "low")
}

func containsActionPhrase(text string, values ...string) bool {
	normalized := " " + strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			return unicode.ToLower(r)
		}
		return ' '
	}, text) + " "
	for _, value := range values {
		candidate := " " + strings.TrimSpace(strings.ToLower(value)) + " "
		if strings.Contains(normalized, candidate) {
			return true
		}
	}
	return false
}

func actionQueues(pursuit models.Pursuit, actions []PursuitAction, blockers []PursuitBlocker) PursuitActionQueues {
	queues := PursuitActionQueues{}
	seen := map[string]bool{}
	actionWorkflowIDs := map[string]bool{}
	add := func(action PursuitAction, lane string) {
		key := lane + "|" + action.Label + "|" + action.WorkflowID + "|" + action.Owner
		if seen[key] {
			return
		}
		seen[key] = true
		switch lane {
		case "robert":
			queues.NeedsRobert = append(queues.NeedsRobert, action)
		case "va":
			queues.VAReady = append(queues.VAReady, action)
		case "system":
			queues.SystemReady = append(queues.SystemReady, action)
		default:
			queues.Waiting = append(queues.Waiting, action)
		}
	}
	for _, action := range actions {
		if strings.TrimSpace(action.WorkflowID) != "" {
			actionWorkflowIDs[action.WorkflowID] = true
		}
		switch {
		case actionNeedsRobert(action):
			add(action, "robert")
		case actionIsSystemReady(action):
			add(action, "system")
		case actionIsVAReady(action):
			add(action, "va")
		default:
			add(action, "waiting")
		}
	}
	for _, blocker := range blockers {
		if strings.TrimSpace(blocker.WorkflowID) != "" && actionWorkflowIDs[blocker.WorkflowID] {
			continue
		}
		action := PursuitAction{
			Label:            firstNonEmpty(blocker.Reason, "Clear blocker: "+blocker.Label),
			Owner:            firstNonEmpty(blocker.Owner, "external"),
			RiskLevel:        pursuit.RiskLevel,
			RequiresApproval: strings.EqualFold(pursuit.RiskLevel, "high"),
			Reason:           "Blocked or waiting: " + firstNonEmpty(blocker.Label, blocker.Reason, "pursuit blocker"),
			WorkflowID:       blocker.WorkflowID,
			YesLabel:         "Prepare follow-up",
			NoLabel:          "Keep waiting",
		}
		if actionNeedsRobert(action) || strings.Contains(strings.ToLower(action.Owner), "robert") {
			add(action, "robert")
			continue
		}
		add(action, "waiting")
	}
	return queues
}

func approvalActions(actions []PursuitAction) []PursuitAction {
	result := []PursuitAction{}
	seen := map[string]bool{}
	for _, action := range actions {
		if !actionNeedsRobert(action) {
			continue
		}
		key := action.Label + "|" + action.WorkflowID + "|" + action.Owner
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, action)
	}
	return result
}

func actionNeedsRobert(action PursuitAction) bool {
	owner := strings.ToLower(strings.TrimSpace(action.Owner))
	return action.RequiresApproval || owner == "robert" || strings.Contains(owner, "robert")
}

func actionIsVAReady(action PursuitAction) bool {
	owner := strings.ToLower(strings.TrimSpace(action.Owner))
	risk := strings.ToLower(strings.TrimSpace(action.RiskLevel))
	return !action.RequiresApproval &&
		(risk == "" || risk == "low" || risk == "medium") &&
		(strings.Contains(owner, "va") || strings.Contains(owner, "assistant"))
}

func actionIsSystemReady(action PursuitAction) bool {
	owner := strings.ToLower(strings.TrimSpace(action.Owner))
	risk := strings.ToLower(strings.TrimSpace(action.RiskLevel))
	return !action.RequiresApproval &&
		(risk == "" || risk == "low") &&
		(strings.Contains(owner, "system") || strings.Contains(owner, "hai"))
}

func decisionQueue(pursuit models.Pursuit, workflows []models.WorkflowItem, proposals []models.WorkflowProposal, decisions []models.WorkflowDecision, runtimeAttempts []models.AutomationLaunchEvent, resolvedDecisions map[string]bool) []PursuitDecision {
	result := []PursuitDecision{}
	workflowsByID := map[uuid.UUID]models.WorkflowItem{}
	if pursuitClosed(pursuit) {
		return result
	}
	if isPursuitCandidate(pursuit) {
		result = append(result, PursuitDecision{
			ID:               "pursuit:" + pursuit.ID.String() + ":candidate-review",
			DecisionType:     "pursuit_candidate_review",
			Status:           "pending",
			Recommended:      firstNonEmpty(pursuit.NextRecommendedAction, "Accept this pursuit candidate and create the first governed workflow plan."),
			Reason:           "HAI created this candidate automatically because no existing pursuit matched the source or memory signal.",
			RiskLevel:        firstNonEmpty(pursuit.RiskLevel, "low"),
			EvidenceLabel:    firstNonEmpty(pursuit.SourceOfCreation, "auto-created pursuit candidate"),
			YesLabel:         "Accept and plan",
			NoLabel:          "Archive",
			YesConsequence:   "HAI converts the candidate into governed pursuit work and keeps approval gates for any risky action.",
			NoConsequence:    "The candidate is archived so it no longer clutters active Robert queues.",
			RequiresApproval: true,
			CreatedAt:        optionalRFC3339(pursuit.CreatedAt),
		})
	}
	for _, attempt := range runtimeAttempts {
		decisionID := runtimeAttemptDecisionID(attempt)
		if runtimeAttemptRecoveredByWorkflow(attempt, workflows) {
			continue
		}
		if !runtimeAttemptNeedsReview(attempt) || resolvedDecisions[decisionID] {
			continue
		}
		result = append(result, PursuitDecision{
			ID:               decisionID,
			DecisionType:     "runtime_attempt_review",
			Status:           "pending",
			Recommended:      "Create a governed recovery workflow before retrying this runtime attempt.",
			Reason:           runtimeAttemptReason(attempt),
			RiskLevel:        firstNonEmpty(runtimeAttemptRouteRisk(attempt), pursuit.RiskLevel, "medium"),
			EvidenceURI:      "automation-launch://" + attempt.ID.String(),
			EvidenceLabel:    runtimeAttemptLabel(attempt),
			YesLabel:         "Create recovery workflow",
			NoLabel:          "Keep blocked",
			YesConsequence:   "HAI creates a tracked recovery workflow; it does not retry uncontrolled runtime execution.",
			NoConsequence:    "The pursuit remains blocked until the runtime configuration or safety issue is reviewed.",
			RequiresApproval: true,
			CreatedAt:        optionalRFC3339(firstTime(attempt.CompletedAt, attempt.StartedAt)),
		})
		if len(result) >= 5 {
			break
		}
	}
	for _, item := range workflows {
		workflowsByID[item.ID] = item
		if item.RequiresApproval && (item.ApprovalStatus == "" || item.ApprovalStatus == "pending") {
			result = append(result, PursuitDecision{
				ID:               "workflow:" + item.ID.String() + ":approval",
				WorkflowID:       item.ID.String(),
				WorkflowTitle:    item.Title,
				DecisionType:     "approval",
				Status:           "pending",
				Recommended:      firstNonEmpty(item.NextAction, item.ApprovalReason, "Approve or reject workflow: "+item.Title),
				Reason:           firstNonEmpty(item.ApprovalReason, "workflow policy requires human approval"),
				RiskLevel:        firstNonEmpty(item.RiskLevel, pursuit.RiskLevel),
				EvidenceURI:      item.SourceURI,
				EvidenceLabel:    item.SourceLabel,
				YesLabel:         "Approve",
				NoLabel:          "Reject",
				YesConsequence:   "The workflow may move forward through the governed worker or automation path.",
				NoConsequence:    "The workflow remains blocked for revision or cancellation.",
				RequiresApproval: true,
			})
		}
	}
	for _, proposal := range openWorkflowProposals(proposals) {
		workflowTitle := ""
		if item, ok := workflowsByID[proposal.WorkflowID]; ok {
			workflowTitle = item.Title
		}
		result = append(result, PursuitDecision{
			ID:               "proposal:" + proposal.ID.String(),
			WorkflowID:       proposal.WorkflowID.String(),
			WorkflowTitle:    workflowTitle,
			DecisionType:     "proposal",
			Status:           "pending",
			Recommended:      proposal.RecommendedAction,
			Reason:           firstNonEmpty(proposal.Options, "open proposal needs a decision"),
			RiskLevel:        pursuit.RiskLevel,
			YesLabel:         "Accept",
			NoLabel:          "Decline",
			YesConsequence:   "HAI records the selected proposal and can advance the linked workflow.",
			NoConsequence:    "The proposal stays unresolved or returns for revision.",
			RequiresApproval: true,
			CreatedAt:        optionalRFC3339(proposal.CreatedAt),
		})
	}
	for _, decision := range decisions {
		workflowTitle := ""
		sourceURI := ""
		sourceLabel := ""
		riskLevel := pursuit.RiskLevel
		if item, ok := workflowsByID[decision.WorkflowID]; ok {
			workflowTitle = item.Title
			sourceURI = item.SourceURI
			sourceLabel = item.SourceLabel
			riskLevel = firstNonEmpty(item.RiskLevel, pursuit.RiskLevel)
		}
		result = append(result, PursuitDecision{
			ID:               decision.ID.String(),
			WorkflowID:       decision.WorkflowID.String(),
			WorkflowTitle:    workflowTitle,
			DecisionType:     decision.DecisionType,
			Status:           decisionStatus(decision),
			Recommended:      decisionRecommendation(decision),
			Reason:           firstNonEmpty(decision.Reason, decision.RuleApplied, "decision recorded in linked workflow"),
			RiskLevel:        riskLevel,
			EvidenceURI:      sourceURI,
			EvidenceLabel:    sourceLabel,
			YesLabel:         "Accept record",
			NoLabel:          "Review",
			YesConsequence:   "The audit record remains part of the pursuit history.",
			NoConsequence:    "Robert should inspect the linked workflow before relying on this decision.",
			RequiresApproval: false,
			Approved:         decision.Approved,
			Actor:            decision.Actor,
			CreatedAt:        optionalRFC3339(decision.CreatedAt),
		})
		if len(result) >= 24 {
			break
		}
	}
	if !hasPendingPursuitDecision(result) && !isPursuitCandidate(pursuit) && workflowsReadyForCompletion(workflows) {
		decisionID := completionReviewDecisionID(pursuit.ID)
		if resolvedDecisions[decisionID] {
			return result
		}
		result = append(result, PursuitDecision{
			ID:               decisionID,
			DecisionType:     "pursuit_completion_review",
			Status:           "pending",
			Recommended:      "Review verified evidence and mark this pursuit complete.",
			Reason:           "all linked workflows are complete and accepted verification evidence is available",
			RiskLevel:        firstNonEmpty(pursuit.RiskLevel, "medium"),
			YesLabel:         "Mark complete",
			NoLabel:          "Keep active",
			YesConsequence:   "HAI marks the pursuit completed and verified through the existing completion evidence guard.",
			NoConsequence:    "The pursuit stays active for more evidence, follow-up, or a better completion note.",
			RequiresApproval: true,
			CreatedAt:        optionalRFC3339(pursuit.UpdatedAt),
		})
	}
	if !hasPendingPursuitDecision(result) && (strings.EqualFold(pursuit.RiskLevel, "high") || strings.EqualFold(pursuit.AutonomyLevel, "approve_before_execute")) {
		decisionID := nextActionDecisionID(pursuit.ID)
		if resolvedDecisions[decisionID] {
			return result
		}
		result = append(result, PursuitDecision{
			ID:               decisionID,
			DecisionType:     "pursuit_next_action",
			Status:           "pending",
			Recommended:      firstNonEmpty(pursuit.NextRecommendedAction, "Define the first safe workflow item for this pursuit."),
			Reason:           "high-risk pursuit needs Robert approval before HAI can execute or delegate consequential work",
			RiskLevel:        firstNonEmpty(pursuit.RiskLevel, "high"),
			YesLabel:         "Create workflow",
			NoLabel:          "Revise goal",
			YesConsequence:   "HAI can create or prepare the first governed workflow with approval gates preserved.",
			NoConsequence:    "The pursuit remains active but waits for a safer objective or more context.",
			RequiresApproval: true,
			CreatedAt:        optionalRFC3339(pursuit.UpdatedAt),
		})
	}
	return result
}

func completionReviewDecisionID(id uuid.UUID) string {
	return "pursuit:" + id.String() + ":completion-review"
}

func nextActionDecisionID(id uuid.UUID) string {
	return "pursuit:" + id.String() + ":next-action"
}

func pendingDecisionCards(cards []PursuitDecision) int {
	count := 0
	for _, card := range cards {
		if strings.EqualFold(card.Status, "pending") || card.RequiresApproval {
			count++
		}
	}
	return count
}

// hasPendingPursuitDecision separates decision history from work that still
// needs an operator. Recorded or approved decisions remain visible in the
// audit timeline but must not hide a valid completion review.
func hasPendingPursuitDecision(cards []PursuitDecision) bool {
	for _, card := range cards {
		if strings.EqualFold(strings.TrimSpace(card.Status), "pending") || card.RequiresApproval {
			return true
		}
	}
	return false
}

func decisionStatus(decision models.WorkflowDecision) string {
	if strings.EqualFold(decision.DecisionType, "approval") {
		if decision.Approved || strings.EqualFold(decision.Decision, "approved") {
			return "approved"
		}
		if strings.EqualFold(decision.Decision, "rejected") {
			return "rejected"
		}
	}
	if strings.EqualFold(decision.Decision, "needs_review") || strings.EqualFold(decision.Decision, "blocked") {
		return "needs_review"
	}
	return "recorded"
}

func decisionRecommendation(decision models.WorkflowDecision) string {
	switch strings.ToLower(strings.TrimSpace(decision.DecisionType)) {
	case "approval_gate":
		if strings.EqualFold(decision.Decision, "required") {
			return "Approval gate was required before execution."
		}
		return "Approval gate allowed low-risk workflow progression."
	case "approval":
		return "Human approval decision recorded: " + decision.Decision + "."
	case "proposal":
		return "Proposal decision recorded: " + decision.Decision + "."
	case "worker_execution", "interrupted_execution":
		return "Worker execution decision recorded: " + decision.Decision + "."
	case "verification_completion":
		return "Verification/completion decision recorded: " + decision.Decision + "."
	default:
		return firstNonEmpty(decision.Reason, decision.DecisionType+": "+decision.Decision)
	}
}

func optionalRFC3339(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

func pursuitTimeline(
	pursuit models.Pursuit,
	activity []models.PursuitActivity,
	workflows []models.WorkflowItem,
	transitions []models.WorkflowTransition,
	sourceLinks []models.WorkflowSourceLink,
	decisions []models.WorkflowDecision,
	events []models.WorkflowEvent,
	taskRuns []PursuitTaskRun,
	taskAttempts []models.PursuitTaskAttempt,
	verificationRuns []models.VerificationRun,
	runtimeAttempts []models.AutomationLaunchEvent,
) []PursuitTimelineItem {
	items := []PursuitTimelineItem{}
	workflowTitles := map[uuid.UUID]string{}
	workflowRisks := map[uuid.UUID]string{}
	for _, item := range workflows {
		workflowTitles[item.ID] = item.Title
		workflowRisks[item.ID] = firstNonEmpty(item.RiskLevel, pursuit.RiskLevel)
	}
	for _, item := range activity {
		items = appendTimeline(items, PursuitTimelineItem{
			ID:          item.ID.String(),
			Kind:        "pursuit_activity",
			Title:       item.EventType,
			Message:     item.Message,
			Actor:       item.Actor,
			SourceURI:   item.SourceURI,
			SourceLabel: item.SourceType,
			Status:      "recorded",
			RiskLevel:   pursuit.RiskLevel,
			CreatedAt:   item.CreatedAt,
		})
	}
	for _, event := range events {
		items = appendTimeline(items, PursuitTimelineItem{
			ID:            event.ID.String(),
			Kind:          "workflow_event",
			Title:         firstNonEmpty(event.EventType, "workflow event"),
			Message:       firstNonEmpty(event.Message, event.RuleApplied),
			WorkflowID:    event.WorkflowID.String(),
			WorkflowTitle: workflowTitles[event.WorkflowID],
			Actor:         event.Actor,
			Status:        firstNonEmpty(event.ToState, event.EventType),
			RiskLevel:     workflowRisks[event.WorkflowID],
			SourceURI:     event.SourceURI,
			CreatedAt:     event.CreatedAt,
		})
	}
	for _, transition := range transitions {
		title := "State changed to " + transition.ToState
		if strings.TrimSpace(transition.FromState) != "" {
			title = "State changed: " + transition.FromState + " -> " + transition.ToState
		}
		items = appendTimeline(items, PursuitTimelineItem{
			ID:            transition.ID.String(),
			Kind:          "workflow_transition",
			Title:         title,
			Message:       firstNonEmpty(transition.Reason, transition.Trigger),
			WorkflowID:    transition.WorkflowID.String(),
			WorkflowTitle: workflowTitles[transition.WorkflowID],
			Actor:         transition.Actor,
			Status:        transition.ToState,
			RiskLevel:     workflowRisks[transition.WorkflowID],
			NeedsReview:   !transition.Approved && containsAny(strings.ToLower(transition.ToState), "approval", "blocked", "review"),
			CreatedAt:     transition.CreatedAt,
		})
	}
	for _, link := range sourceLinks {
		items = appendTimeline(items, PursuitTimelineItem{
			ID:            link.ID.String(),
			Kind:          "source_link",
			Title:         "Source linked: " + firstNonEmpty(link.Relationship, link.SourceType, "related"),
			Message:       firstNonEmpty(link.SourceLabel, link.SourceURI, link.SourceID),
			WorkflowID:    link.WorkflowID.String(),
			WorkflowTitle: workflowTitles[link.WorkflowID],
			Status:        link.Relationship,
			RiskLevel:     workflowRisks[link.WorkflowID],
			SourceURI:     link.SourceURI,
			SourceLabel:   link.SourceLabel,
			CreatedAt:     link.CreatedAt,
		})
	}
	for _, decision := range decisions {
		items = appendTimeline(items, PursuitTimelineItem{
			ID:            decision.ID.String(),
			Kind:          "workflow_decision",
			Title:         "Decision: " + firstNonEmpty(decision.DecisionType, "workflow") + " / " + decision.Decision,
			Message:       firstNonEmpty(decision.Reason, decision.RuleApplied),
			WorkflowID:    decision.WorkflowID.String(),
			WorkflowTitle: workflowTitles[decision.WorkflowID],
			Actor:         decision.Actor,
			Status:        decisionStatus(decision),
			RiskLevel:     workflowRisks[decision.WorkflowID],
			NeedsReview:   decisionStatus(decision) == "needs_review",
			CreatedAt:     decision.CreatedAt,
		})
	}
	for _, run := range taskRuns {
		when := timeFromPointer(run.LastRunAt)
		if when.IsZero() {
			continue
		}
		workflowID, _ := uuid.Parse(run.WorkflowID.String())
		items = appendTimeline(items, PursuitTimelineItem{
			ID:            "task-run:" + run.WorkflowID.String() + ":" + run.TaskPlanID,
			Kind:          "task_run",
			Title:         "Task run: " + run.Status,
			Message:       firstNonEmpty(run.LastWorkerError, run.VerificationStatus, "task engine attempted the workflow"),
			WorkflowID:    run.WorkflowID.String(),
			WorkflowTitle: firstNonEmpty(run.WorkflowTitle, workflowTitles[workflowID]),
			Status:        run.Status,
			RiskLevel:     workflowRisks[workflowID],
			NeedsReview:   run.NeedsReview,
			CreatedAt:     when,
		})
	}
	for _, attempt := range taskAttempts {
		when := timeFromPointer(attempt.CompletedAt)
		if when.IsZero() {
			when = timeFromPointer(attempt.StartedAt)
		}
		items = appendTimeline(items, PursuitTimelineItem{
			ID:          "task-attempt:" + attempt.TaskPlanID,
			Kind:        "task_attempt",
			Title:       "Direct task " + firstNonEmpty(attempt.Mode, "attempt") + ": " + firstNonEmpty(attempt.Status, "recorded"),
			Message:     firstNonEmpty(attempt.BlockedReason, attempt.RequestSummary, attempt.VerificationStatus),
			Status:      attempt.Status,
			RiskLevel:   firstNonEmpty(attempt.RiskLevel, pursuit.RiskLevel),
			SourceURI:   "task://" + attempt.TaskPlanID,
			SourceLabel: "direct task attempt",
			NeedsReview: strings.Contains(attempt.Status, "review") || strings.TrimSpace(attempt.BlockedReason) != "",
			CreatedAt:   when,
		})
	}
	for _, run := range verificationRuns {
		items = appendTimeline(items, PursuitTimelineItem{
			ID:          run.ID.String(),
			Kind:        "verification",
			Title:       "Verification: " + firstNonEmpty(run.Mode, "run"),
			Message:     firstNonEmpty(run.Question, run.Answer),
			Status:      run.Status,
			RiskLevel:   pursuit.RiskLevel,
			NeedsReview: !acceptedCompletionStatus(run.Status),
			CreatedAt:   run.CreatedAt,
		})
	}
	for _, attempt := range runtimeAttempts {
		when := firstTime(attempt.CompletedAt, attempt.StartedAt)
		label := firstNonEmpty(attempt.RuntimeType, attempt.LaunchType, "automation")
		sourceURI := "automation-launch://" + attempt.ID.String()
		items = appendTimeline(items, PursuitTimelineItem{
			ID:          attempt.ID.String(),
			Kind:        "runtime_attempt",
			Title:       runtimeTimelineTitle(attempt, label),
			Message:     firstNonEmpty(attempt.Message, attempt.Target, attempt.Output),
			Status:      attempt.Status,
			RiskLevel:   pursuit.RiskLevel,
			SourceURI:   sourceURI,
			SourceLabel: runtimeAttemptLabel(attempt),
			NeedsReview: runtimeAttemptNeedsReview(attempt),
			CreatedAt:   when,
		})
		for index, auditEvent := range boundedRuntimeAuditEvents(attempt.AuditEvents) {
			items = appendTimeline(items, PursuitTimelineItem{
				ID:          fmt.Sprintf("%s:audit:%d", attempt.ID.String(), index),
				Kind:        "runtime_audit",
				Title:       "Runtime audit: " + label,
				Message:     auditEvent,
				Status:      attempt.Status,
				RiskLevel:   pursuit.RiskLevel,
				SourceURI:   sourceURI,
				SourceLabel: runtimeAttemptLabel(attempt),
				NeedsReview: runtimeAttemptNeedsReview(attempt),
				CreatedAt:   when,
			})
		}
	}
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	if len(items) > 50 {
		return items[:50]
	}
	return items
}

func appendTimeline(items []PursuitTimelineItem, item PursuitTimelineItem) []PursuitTimelineItem {
	if item.CreatedAt.IsZero() {
		return items
	}
	if strings.TrimSpace(item.Title) == "" {
		item.Title = item.Kind
	}
	item.CreatedAt = item.CreatedAt.UTC()
	return append(items, item)
}

func boundedRuntimeAuditEvents(events []string) []string {
	const limit = 5
	capacity := len(events)
	if capacity > limit {
		capacity = limit
	}
	result := make([]string, 0, capacity)
	for _, event := range events {
		event = strings.TrimSpace(event)
		if event == "" {
			continue
		}
		result = append(result, event)
		if len(result) == limit {
			break
		}
	}
	return result
}

func runtimeTimelineTitle(attempt models.AutomationLaunchEvent, label string) string {
	if strings.EqualFold(strings.TrimSpace(attempt.LaunchType), "agent_runtime_stop") {
		return "Runtime stop: " + label
	}
	return "Runtime attempt: " + label
}

func timelineChangeSummary(item PursuitTimelineItem) string {
	if strings.TrimSpace(item.Message) == "" {
		return item.Title
	}
	return item.Title + ": " + item.Message
}

func timeFromPointer(value *time.Time) time.Time {
	if value == nil {
		return time.Time{}
	}
	return *value
}

func firstTime(values ...time.Time) time.Time {
	for _, value := range values {
		if !value.IsZero() {
			return value
		}
	}
	return time.Time{}
}

func summarize(pursuit models.Pursuit, links []models.PursuitLink, workflows []models.WorkflowItem, loops []models.WorkflowOpenLoop, evidence []models.WorkflowEvidenceClaim, memories []models.ContextMemory, sourceItems []PursuitSourceItem, extractions []models.SourceExtraction, taskRuns []PursuitTaskRun, taskAttempts []models.PursuitTaskAttempt, verificationRuns []models.VerificationRun, runtimeAttempts []models.AutomationLaunchEvent, activity []models.PursuitActivity, sourceBlockers []PursuitBlocker, qualityGateBlockers []PursuitBlocker, contractBlockers []PursuitBlocker) PursuitSummary {
	approvals := len(approvalWorkflows(workflows))
	needsRobert := approvals
	blocked := len(blockers(workflows, loops)) + len(runtimeAttemptBlockers(runtimeAttempts, workflows, resolvedPursuitDecisions(activity))) + len(sourceBlockers) + len(qualityGateBlockers) + len(contractBlockers)
	linkedEvidence := len(evidence) + len(memories) + len(sourceItems) + activeSourceExtractions(extractions) + acceptedVerificationRuns(verificationRuns) + completedRuntimeAttempts(runtimeAttempts) + acceptedWorkflowCompletionEvidence(workflows) + acceptedAmbientOpportunityLinks(links)
	completed := 0
	for _, item := range workflows {
		if item.CurrentState == workflow.StateCompleted {
			completed++
		}
	}
	state := fmt.Sprintf("%s has %d linked workflows, %d needing Robert, %d blockers, and %d open loops.", pursuit.Title, len(workflows), approvals, blocked, len(loops))
	if isPursuitCandidate(pursuit) {
		needsRobert++
		state = state + " This is an auto-created pursuit candidate and needs Robert acceptance before HAI treats it as planned work."
	}
	if !pursuitClosed(pursuit) && len(workflows) > 0 && completed == len(workflows) && approvals == 0 && blocked == 0 {
		state = "All linked workflows appear complete; pursuit completion needs evidence review or Robert confirmation."
		if isPursuitCandidate(pursuit) {
			state = state + " It is still a candidate until Robert accepts or archives it."
		}
	}
	planningNeeded := pursuitNeedsPlanning(pursuit, len(workflows))
	if planningNeeded {
		state = state + " No linked workflow exists yet; planning is needed before HAI can move it forward."
	}
	if len(sourceBlockers) > 0 {
		state = state + " Some linked source evidence was archived or is missing; review provenance before using it."
	}
	if len(qualityGateBlockers) > 0 {
		state = state + " A linked workflow quality gate needs review before this pursuit can move forward."
	}
	if len(contractBlockers) > 0 {
		state = state + " The pursuit outcome contract has an active deadline, dependency, or stop-condition blocker."
	}
	reviewDue := isReviewDue(pursuit)
	if reviewDue {
		state = state + " Scheduled pursuit review is due."
	}
	changed := "No recent activity recorded."
	if len(activity) > 0 {
		changed = activity[0].Message
	}
	criteriaSatisfied, triggeredStops, openDependencies := pursuitGoalContractCounts(pursuit)
	goalContractReady := pursuitGoalContractReady(pursuit)
	targetOverdue := pursuitTargetOverdue(pursuit)
	return PursuitSummary{
		CurrentState:            state,
		WhatChanged:             changed,
		NeedsRobert:             needsRobert,
		Blocked:                 blocked,
		OpenLoops:               len(loops),
		TaskRuns:                len(taskRuns) + len(taskAttempts),
		LinkedEvidence:          linkedEvidence,
		VerificationRuns:        len(verificationRuns),
		RuntimeAttempts:         len(runtimeAttempts),
		Confidence:              pursuit.Confidence,
		PlanningNeeded:          planningNeeded,
		ReviewDue:               reviewDue,
		CompletionCandidate:     !pursuitClosed(pursuit) && !isPursuitCandidate(pursuit) && len(workflows) > 0 && completed == len(workflows) && approvals == 0 && blocked == 0 && pursuitGoalContractCompletionBlocker(pursuit) == "",
		GoalContractReady:       goalContractReady,
		CriteriaSatisfied:       criteriaSatisfied,
		CriteriaTotal:           len(pursuit.SuccessCriteria),
		TriggeredStopConditions: triggeredStops,
		OpenDependencies:        openDependencies,
		TargetOverdue:           targetOverdue,
	}
}

func operationalDigest(pursuit models.Pursuit, detail PursuitDetail) PursuitOperationalDigest {
	summary := detail.Summary
	digest := PursuitOperationalDigest{
		NeedsRobert:      summary.NeedsRobert,
		VAReady:          len(detail.ActionQueues.VAReady),
		SystemReady:      len(detail.ActionQueues.SystemReady),
		Waiting:          len(detail.ActionQueues.Waiting),
		Blocked:          len(detail.Blockers),
		Evidence:         summary.LinkedEvidence,
		RuntimeAttempts:  len(detail.RuntimeAttempts),
		VerificationRuns: len(detail.VerificationRuns),
		OpenLoops:        len(detail.OpenLoops),
	}
	digest.PrimaryLane = primaryOperationalLane(pursuit, summary, detail.ActionQueues, detail.Blockers)
	digest.Headline = operationalHeadline(pursuit, digest.PrimaryLane, summary)
	digest.RecommendedAction = firstNonEmpty(primaryActionLabel(detail.ActionQueues.NeedsRobert), primaryActionLabel(detail.ActionQueues.SystemReady), primaryActionLabel(detail.ActionQueues.VAReady), primaryActionLabel(detail.ActionQueues.Waiting), pursuit.NextRecommendedAction, "Review pursuit context and define the next concrete action.")
	digest.RobertLine = fmt.Sprintf("%d Robert decision%s pending.", digest.NeedsRobert, pluralSuffix(digest.NeedsRobert))
	if action := primaryActionLabel(detail.ActionQueues.NeedsRobert); action != "" {
		digest.RobertLine = digest.RobertLine + " Next: " + action
	}
	digest.DelegationLine = fmt.Sprintf("%d VA-ready action%s.", digest.VAReady, pluralSuffix(digest.VAReady))
	if action := primaryActionLabel(detail.ActionQueues.VAReady); action != "" {
		digest.DelegationLine = digest.DelegationLine + " First: " + action
	}
	digest.SystemLine = fmt.Sprintf("%d system-ready action%s.", digest.SystemReady, pluralSuffix(digest.SystemReady))
	if action := primaryActionLabel(detail.ActionQueues.SystemReady); action != "" {
		digest.SystemLine = digest.SystemLine + " First: " + action
	}
	digest.WaitingLine = fmt.Sprintf("%d waiting action%s and %d open loop%s.", digest.Waiting, pluralSuffix(digest.Waiting), digest.OpenLoops, pluralSuffix(digest.OpenLoops))
	digest.BlockerLine = operationalBlockerLine(detail.Blockers)
	digest.EvidenceLine = operationalEvidenceLine(summary, detail)
	digest.RuntimeLine = operationalRuntimeLine(detail.RuntimeAttempts)
	digest.SourceLine = operationalSourceLine(detail)
	digest.VerificationLine = operationalVerificationLine(detail)
	digest.RiskLine = fmt.Sprintf("%s risk with %.0f%% confidence and %s autonomy.", firstNonEmpty(pursuit.RiskLevel, "unknown"), pursuit.Confidence*100, firstNonEmpty(pursuit.AutonomyLevel, "manual"))
	return digest
}

func primaryOperationalLane(pursuit models.Pursuit, summary PursuitSummary, queues PursuitActionQueues, blockers []PursuitBlocker) string {
	switch {
	case pursuitClosed(pursuit):
		return "completed"
	case len(queues.NeedsRobert) > 0 || summary.NeedsRobert > 0:
		return "needs_robert"
	case len(blockers) > 0 || summary.Blocked > 0:
		return "blocked"
	case summary.PlanningNeeded:
		return "planning_needed"
	case len(queues.SystemReady) > 0:
		return "system_ready"
	case len(queues.VAReady) > 0:
		return "va_ready"
	case len(queues.Waiting) > 0:
		return "waiting"
	default:
		return "monitor"
	}
}

func operationalHeadline(pursuit models.Pursuit, lane string, summary PursuitSummary) string {
	switch lane {
	case "completed":
		return "Pursuit is closed; keep evidence and audit history available."
	case "needs_robert":
		return "Robert has a concrete decision or approval to resolve before this can move safely."
	case "blocked":
		return "The pursuit is blocked or waiting; unblock before spending more execution effort."
	case "planning_needed":
		return "No linked workflow exists yet; create the first governed workflow plan."
	case "system_ready":
		return "HAI can prepare low-risk system work through the guarded task path."
	case "va_ready":
		return "This has delegation-ready work that can be packaged for a VA or assistant."
	case "waiting":
		return "The pursuit is waiting; monitor follow-up timing and source changes."
	default:
		return firstNonEmpty(summary.CurrentState, pursuit.CurrentStateSummary, "Pursuit is being monitored.")
	}
}

func primaryActionLabel(actions []PursuitAction) string {
	for _, action := range actions {
		if value := strings.TrimSpace(action.Label); value != "" {
			return value
		}
	}
	return ""
}

func operationalBlockerLine(blockers []PursuitBlocker) string {
	if len(blockers) == 0 {
		return "No active blocker detected from linked workflows, open loops, sources, or runtime attempts."
	}
	first := blockers[0]
	line := fmt.Sprintf("%d blocker%s. First: %s", len(blockers), pluralSuffix(len(blockers)), firstNonEmpty(first.Label, first.Reason, "unresolved blocker"))
	if reason := strings.TrimSpace(first.Reason); reason != "" {
		line = line + " - " + reason
	}
	if owner := strings.TrimSpace(first.Owner); owner != "" {
		line = line + " (owner: " + owner + ")"
	}
	return line
}

func operationalEvidenceLine(summary PursuitSummary, detail PursuitDetail) string {
	parts := []string{
		fmt.Sprintf("%d evidence item%s", summary.LinkedEvidence, pluralSuffix(summary.LinkedEvidence)),
		fmt.Sprintf("%d timeline item%s", summary.TimelineItems, pluralSuffix(summary.TimelineItems)),
	}
	if len(detail.Evidence) > 0 {
		parts = append(parts, "workflow evidence present")
	}
	if len(detail.Memories) > 0 {
		parts = append(parts, "memory context linked")
	}
	if len(detail.SourceItems) > 0 || activeSourceExtractions(detail.SourceExtractions) > 0 {
		parts = append(parts, "source provenance linked")
	}
	return strings.Join(parts, " / ")
}

func operationalRuntimeLine(attempts []models.AutomationLaunchEvent) string {
	if len(attempts) == 0 {
		return "No agent runtime attempts are linked to this pursuit."
	}
	latest := attempts[0]
	for _, attempt := range attempts[1:] {
		if firstTime(attempt.CompletedAt, attempt.StartedAt).After(firstTime(latest.CompletedAt, latest.StartedAt)) {
			latest = attempt
		}
	}
	line := fmt.Sprintf("%d runtime attempt%s. Latest: %s %s", len(attempts), pluralSuffix(len(attempts)), runtimeAttemptLabel(latest), firstNonEmpty(latest.Status, "unknown"))
	if route := runtimeAttemptRouteSummary(latest); route != "" {
		line = line + " / " + route
	}
	return line
}

func operationalSourceLine(detail PursuitDetail) string {
	sourceCount := len(detail.SourceLinks) + len(detail.SourceItems) + activeSourceExtractions(detail.SourceExtractions)
	if sourceCount == 0 {
		return "No connected-source provenance is linked yet."
	}
	return fmt.Sprintf("%d connected-source/provenance record%s linked.", sourceCount, pluralSuffix(sourceCount))
}

func operationalVerificationLine(detail PursuitDetail) string {
	accepted := acceptedVerificationRuns(detail.VerificationRuns)
	if len(detail.VerificationRuns) == 0 && len(detail.VerificationClaims) == 0 && len(detail.VerificationEvidence) == 0 {
		return "No verification run is linked yet; do not treat completion as proven."
	}
	return fmt.Sprintf("%d verification run%s, %d accepted, %d claim%s, %d evidence record%s.", len(detail.VerificationRuns), pluralSuffix(len(detail.VerificationRuns)), accepted, len(detail.VerificationClaims), pluralSuffix(len(detail.VerificationClaims)), len(detail.VerificationEvidence), pluralSuffix(len(detail.VerificationEvidence)))
}

func activeSourceExtractions(extractions []models.SourceExtraction) int {
	count := 0
	for _, extraction := range extractions {
		if !extraction.Archived {
			count++
		}
	}
	return count
}

func requestsVerifiedCompletion(pursuit models.Pursuit, request UpdateRequest) bool {
	status := strings.TrimSpace(request.Status)
	completionState := strings.TrimSpace(request.CompletionState)
	return (strings.EqualFold(status, StatusCompleted) && !strings.EqualFold(pursuit.Status, StatusCompleted)) ||
		(strings.EqualFold(completionState, CompletionVerified) && !strings.EqualFold(pursuit.CompletionState, CompletionVerified))
}

func (s *service) completionActiveBlockerReason(id uuid.UUID) (string, error) {
	return s.completionActiveBlockerReasonForOwner("", id)
}

func (s *service) completionActiveBlockerReasonForOwner(ownerIdentity string, id uuid.UUID) (string, error) {
	pursuit, err := s.repo.FindByID(id)
	if err != nil {
		return "", err
	}
	if !pursuitVisibleTo(*pursuit, ownerIdentity) {
		return "", fmt.Errorf("pursuit not found")
	}
	if reason := pursuitGoalContractCompletionBlocker(*pursuit); reason != "" {
		return reason, nil
	}
	links, err := s.repo.FindLinks(id)
	if err != nil {
		return "", err
	}
	links, err = s.visibleLinksForOwner(ownerIdentity, links)
	if err != nil {
		return "", err
	}
	workflowIDs := linkUUIDs(links, LinkWorkflow)
	workflows, err := s.repo.FindLinkedWorkflows(workflowIDs)
	if err != nil {
		return "", err
	}
	if approvals := approvalWorkflows(workflows); len(approvals) > 0 {
		return fmt.Sprintf("%d workflow approval%s still pending", len(approvals), pluralSuffix(len(approvals))), nil
	}
	openLoops, err := s.repo.FindLinkedOpenLoops(workflowIDs)
	if err != nil {
		return "", err
	}
	if active := blockers(workflows, openLoops); len(active) > 0 {
		return firstNonEmpty(active[0].Reason, active[0].Label, "linked workflow or open loop is still blocked"), nil
	}
	proposals, err := s.repo.FindLinkedProposals(workflowIDs)
	if err != nil {
		return "", err
	}
	if open := openWorkflowProposals(proposals); len(open) > 0 {
		return firstNonEmpty(open[0].RecommendedAction, open[0].Options, "open workflow proposal needs a decision"), nil
	}
	decisions, err := s.repo.FindLinkedDecisions(workflowIDs)
	if err != nil {
		return "", err
	}
	if unresolved := unresolvedWorkflowDecisions(decisions); len(unresolved) > 0 {
		return firstNonEmpty(unresolved[0].Reason, unresolved[0].RuleApplied, "linked workflow decision still needs review"), nil
	}
	extractions, err := s.repo.FindLinkedExtractions(linkUUIDs(links, LinkSourceExtraction))
	if err != nil {
		return "", err
	}
	if staleSources := sourceRetractionBlockers(links, extractions); len(staleSources) > 0 {
		return firstNonEmpty(staleSources[0].Reason, staleSources[0].Label, "linked source evidence needs review"), nil
	}
	runtimeAttemptIDs := linkUUIDs(links, LinkAgentRuntime)
	if len(runtimeAttemptIDs) > 0 {
		attempts, err := s.repo.FindLinkedAutomationLaunches(nil, runtimeAttemptIDs, len(runtimeAttemptIDs))
		if err != nil {
			return "", err
		}
		if runtimeBlockers := runtimeAttemptBlockers(attempts, workflows, map[string]bool{}); len(runtimeBlockers) > 0 {
			return firstNonEmpty(runtimeBlockers[0].Reason, runtimeBlockers[0].Label, "linked runtime attempt still needs review"), nil
		}
	}
	return "", nil
}

func (s *service) completionEvidenceAvailable(id uuid.UUID) (bool, string, error) {
	return s.completionEvidenceAvailableForOwner("", id)
}

func (s *service) completionEvidenceAvailableForOwner(ownerIdentity string, id uuid.UUID) (bool, string, error) {
	links, err := s.repo.FindLinks(id)
	if err != nil {
		return false, "", err
	}
	links, err = s.visibleLinksForOwner(ownerIdentity, links)
	if err != nil {
		return false, "", err
	}
	for _, link := range links {
		if link.LinkType == LinkVerification && strings.TrimSpace(link.LinkID) != "" {
			id, err := uuid.Parse(strings.TrimSpace(link.LinkID))
			if err != nil {
				if strings.TrimSpace(link.SourceURI) != "" && strings.EqualFold(strings.TrimSpace(link.Relationship), "completion_evidence") {
					return true, "external completion verification record has provenance", nil
				}
				continue
			}
			runs, err := s.repo.FindLinkedVerificationRuns([]uuid.UUID{id})
			if err != nil {
				return false, "", err
			}
			for _, run := range runs {
				if acceptedCompletionStatus(run.Status) {
					return true, "linked verification run has accepted status", nil
				}
			}
			if strings.TrimSpace(link.SourceURI) != "" && strings.EqualFold(strings.TrimSpace(link.Relationship), "completion_evidence") {
				return true, "external completion verification record has provenance", nil
			}
		}
	}
	runtimeEvidenceIDs := completionEvidenceRuntimeIDs(links)
	if len(runtimeEvidenceIDs) > 0 {
		attempts, err := s.repo.FindLinkedAutomationLaunches(nil, runtimeEvidenceIDs, len(runtimeEvidenceIDs))
		if err != nil {
			return false, "", err
		}
		for _, attempt := range attempts {
			if runtimeAttemptCompleted(attempt.Status) {
				return true, "linked agent-runtime attempt completed under HAI controls", nil
			}
		}
	}
	workflowIDs := linkUUIDs(links, LinkWorkflow)
	if len(workflowIDs) == 0 {
		return false, "no linked workflows or verification records", nil
	}
	evidence, err := s.repo.FindLinkedEvidence(workflowIDs)
	if err != nil {
		return false, "", err
	}
	for _, claim := range evidence {
		if acceptedCompletionStatus(claim.Status) && strings.TrimSpace(claim.SourceURI) != "" {
			return true, "linked workflow evidence is verified", nil
		}
	}
	workflows, err := s.repo.FindLinkedWorkflows(workflowIDs)
	if err != nil {
		return false, "", err
	}
	for _, item := range workflows {
		if item.CurrentState == workflow.StateCompleted && acceptedCompletionStatus(item.VerificationStatus) {
			return true, "linked workflow completed with accepted verification", nil
		}
	}
	return false, "linked workflows do not have accepted verification evidence", nil
}

func completionEvidenceRuntimeIDs(links []models.PursuitLink) []uuid.UUID {
	ids := []uuid.UUID{}
	for _, link := range links {
		if link.LinkType != LinkAgentRuntime {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(link.Relationship), "completion_evidence") {
			continue
		}
		id, err := uuid.Parse(strings.TrimSpace(link.LinkID))
		if err != nil {
			continue
		}
		ids = append(ids, id)
	}
	return uniqueUUIDs(ids)
}

func acceptedCompletionStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "verified", "source_supported", "test_passed", "human_approved":
		return true
	default:
		return false
	}
}

func compactSourceItems(items []models.SourceRawItem) []PursuitSourceItem {
	result := make([]PursuitSourceItem, 0, len(items))
	for _, item := range items {
		result = append(result, PursuitSourceItem{
			ID:         item.ID,
			SourceID:   item.SourceID,
			ExternalID: item.ExternalID,
			ProjectKey: item.ProjectKey,
			ItemType:   item.ItemType,
			Title:      item.Title,
			SourceURI:  item.SourceURI,
			Metadata:   item.Metadata,
			FetchedAt:  item.FetchedAt,
			CreatedAt:  item.CreatedAt,
			UpdatedAt:  item.UpdatedAt,
		})
	}
	return result
}

func compactConversations(items []models.AIConversationArchive) []PursuitConversation {
	result := make([]PursuitConversation, 0, len(items))
	for _, item := range items {
		result = append(result, PursuitConversation{
			ID:            item.ID,
			Platform:      item.Platform,
			ExternalID:    item.ExternalID,
			Title:         item.Title,
			SourceURI:     item.SourceURI,
			Revision:      item.Revision,
			MessageCount:  item.MessageCount,
			CapturedAt:    item.CapturedAt,
			LastMessageAt: item.LastMessageAt,
			Archived:      item.Archived,
		})
	}
	return result
}

func compactAmbientOpportunities(items []models.AmbientOpportunity) []PursuitAmbientOpportunity {
	result := make([]PursuitAmbientOpportunity, 0, len(items))
	for _, item := range items {
		result = append(result, PursuitAmbientOpportunity{
			ID:               item.ID,
			NeedKey:          item.NeedKey,
			Title:            item.Title,
			Rationale:        item.Rationale,
			NextAction:       item.NextAction,
			SourceType:       item.SourceType,
			SourceURI:        item.SourceURI,
			PriorityScore:    item.PriorityScore,
			Confidence:       item.Confidence,
			Risk:             item.Risk,
			RequiresApproval: item.RequiresApproval,
			Status:           item.Status,
			LastSeenAt:       item.LastSeenAt,
			ResolutionNote:   item.ResolutionNote,
			CreatedAt:        item.CreatedAt,
			UpdatedAt:        item.UpdatedAt,
		})
	}
	return result
}

func compactAutomations(items []models.Automation) []PursuitAutomation {
	result := make([]PursuitAutomation, 0, len(items))
	for _, item := range items {
		result = append(result, PursuitAutomation{
			ID:                item.ID,
			Name:              item.Name,
			RuntimeType:       item.RuntimeType,
			LaunchType:        item.LaunchType,
			Status:            item.Status,
			LastLaunchAt:      item.LastLaunchAt,
			LastFailureReason: item.LastFailureReason,
		})
	}
	return result
}

func taskRunsFromWorkflows(workflows []models.WorkflowItem) []PursuitTaskRun {
	result := []PursuitTaskRun{}
	for _, item := range workflows {
		if !workflowHasTaskRunEvidence(item) {
			continue
		}
		result = append(result, PursuitTaskRun{
			WorkflowID:         item.ID,
			WorkflowTitle:      item.Title,
			TaskPlanID:         item.LastTaskPlanID,
			Status:             taskRunStatus(item),
			VerificationStatus: item.VerificationStatus,
			RetryCount:         item.RetryCount,
			MaxRetries:         item.MaxRetries,
			LastRunAt:          item.LastRunAt,
			NextRunAt:          item.NextRunAt,
			LastWorkerError:    item.LastWorkerError,
			AutomationID:       item.AutomationID,
			NeedsReview:        taskRunNeedsReview(item),
		})
	}
	return result
}

func workflowHasTaskRunEvidence(item models.WorkflowItem) bool {
	return strings.TrimSpace(item.LastTaskPlanID) != "" ||
		item.LastRunAt != nil ||
		item.RetryCount > 0 ||
		strings.TrimSpace(item.VerificationStatus) != "" ||
		strings.TrimSpace(item.LastWorkerError) != ""
}

func taskRunStatus(item models.WorkflowItem) string {
	if strings.TrimSpace(item.LastWorkerError) != "" {
		return "blocked"
	}
	if item.CurrentState == workflow.StateCompleted {
		return "completed"
	}
	if item.NextRunAt != nil {
		return "retry_scheduled"
	}
	if strings.TrimSpace(item.LastTaskPlanID) != "" || item.LastRunAt != nil {
		return "ran"
	}
	return "not_started"
}

func taskRunNeedsReview(item models.WorkflowItem) bool {
	status := strings.ToLower(strings.TrimSpace(item.VerificationStatus))
	return strings.TrimSpace(item.LastWorkerError) != "" ||
		status == "needs_review" ||
		status == "unsupported" ||
		strings.Contains(status, "fail")
}

func verificationRunIDs(runs []models.VerificationRun) []uuid.UUID {
	result := make([]uuid.UUID, 0, len(runs))
	for _, run := range runs {
		result = append(result, run.ID)
	}
	return uniqueUUIDs(result)
}

func acceptedVerificationRuns(runs []models.VerificationRun) int {
	count := 0
	for _, run := range runs {
		if acceptedCompletionStatus(run.Status) {
			count++
		}
	}
	return count
}

func acceptedWorkflowCompletionEvidence(workflows []models.WorkflowItem) int {
	count := 0
	for _, item := range workflows {
		if item.CurrentState == workflow.StateCompleted && acceptedCompletionStatus(item.VerificationStatus) {
			count++
		}
	}
	return count
}

func acceptedAmbientOpportunityLinks(links []models.PursuitLink) int {
	count := 0
	for _, link := range links {
		if link.LinkType != LinkAmbientOpportunity {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(link.Relationship), "ambient_proposal_accepted") {
			continue
		}
		count++
	}
	return count
}

func workflowsReadyForCompletion(workflows []models.WorkflowItem) bool {
	if len(workflows) == 0 {
		return false
	}
	for _, item := range workflows {
		if item.CurrentState != workflow.StateCompleted || !acceptedCompletionStatus(item.VerificationStatus) {
			return false
		}
	}
	return true
}

func pursuitNeedsRobert(pursuit models.Pursuit, actions []PursuitAction) bool {
	if pursuitClosed(pursuit) {
		return false
	}
	if isPursuitCandidate(pursuit) {
		return true
	}
	if strings.EqualFold(pursuit.RiskLevel, "high") || strings.EqualFold(pursuit.AutonomyLevel, "approve_before_execute") {
		return true
	}
	for _, action := range actions {
		if action.RequiresApproval || strings.EqualFold(action.Owner, "Robert") {
			return true
		}
	}
	return false
}

func pursuitClosed(pursuit models.Pursuit) bool {
	return pursuit.Archived ||
		strings.EqualFold(pursuit.Status, StatusCompleted) ||
		strings.EqualFold(pursuit.CompletionState, CompletionVerified)
}

func ensurePursuitOpen(pursuit models.Pursuit, action string) error {
	if !pursuitClosed(pursuit) {
		return nil
	}
	return fmt.Errorf("cannot %s a closed pursuit; reopen it explicitly or create a new pursuit", action)
}

func updateAttemptsReopen(pursuit models.Pursuit, request UpdateRequest) bool {
	if !pursuitClosed(pursuit) {
		return false
	}
	if request.Archived != nil && !*request.Archived {
		return true
	}
	if status := strings.TrimSpace(request.Status); status != "" && !strings.EqualFold(status, pursuit.Status) {
		return true
	}
	if completion := strings.TrimSpace(request.CompletionState); completion != "" && !strings.EqualFold(completion, pursuit.CompletionState) {
		return true
	}
	return false
}

func isPursuitCandidate(pursuit models.Pursuit) bool {
	source := strings.ToLower(strings.TrimSpace(pursuit.SourceOfCreation))
	return source == "pursuit_candidate" || strings.Contains(source, "_pursuit_candidate")
}

// IsCandidate exposes the pursuit lifecycle boundary to orchestrators without
// letting them infer candidate state from storage fields themselves.
func IsCandidate(pursuit models.Pursuit) bool {
	return isPursuitCandidate(pursuit)
}

func (s *service) markPursuitCandidateAccepted(pursuit *models.Pursuit, actor string) error {
	if pursuit == nil || !isPursuitCandidate(*pursuit) {
		return nil
	}
	now := time.Now().UTC()
	pursuit.SourceOfCreation = acceptedCandidateSource(pursuit.SourceOfCreation)
	pursuit.Status = StatusActive
	pursuit.LastActivityAt = &now
	pursuit.CurrentStateSummary = firstNonEmpty(pursuit.CurrentStateSummary, "Pursuit candidate accepted by Robert and ready for governed planning.")
	if _, err := s.repo.Update(pursuit); err != nil {
		return err
	}
	_, _ = s.recordActivity(pursuit.ID, "pursuit.candidate_accepted", "Robert accepted the auto-created pursuit candidate for governed HAI handling.", firstNonEmpty(actor, "Robert"), "", "", "")
	return nil
}

func acceptedCandidateSource(source string) string {
	source = strings.TrimSpace(source)
	if source == "" {
		return "pursuit_intake"
	}
	lower := strings.ToLower(source)
	needle := "_pursuit_candidate"
	if idx := strings.Index(lower, needle); idx >= 0 {
		return source[:idx] + "_pursuit_intake" + source[idx+len(needle):]
	}
	if strings.EqualFold(source, "pursuit_candidate") {
		return "pursuit_intake"
	}
	return source + "_accepted"
}

func parseOptionalStandingMandateID(raw string) (*uuid.UUID, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	id, err := uuid.Parse(raw)
	if err != nil || id == uuid.Nil {
		return nil, fmt.Errorf("standing mandate id must be a UUID")
	}
	return &id, nil
}

func pursuitMandateIDString(value *uuid.UUID) string {
	if value == nil || *value == uuid.Nil {
		return ""
	}
	return value.String()
}

func samePursuitMandate(left, right *uuid.UUID) bool {
	return pursuitMandateIDString(left) == pursuitMandateIDString(right)
}

func isVAReady(item PursuitListItem) bool {
	if item.NeedsRobert > 0 || item.Blocked > 0 || strings.EqualFold(item.Pursuit.RiskLevel, "high") {
		return false
	}
	return strings.Contains(strings.ToLower(item.Pursuit.AutonomyLevel), "suggest")
}

func isSystemReady(item PursuitListItem) bool {
	if item.NeedsRobert > 0 || item.Blocked > 0 || !strings.EqualFold(item.Pursuit.RiskLevel, "low") {
		return false
	}
	autonomy := strings.ToLower(item.Pursuit.AutonomyLevel)
	return strings.Contains(autonomy, "autonomous_safe") || strings.Contains(autonomy, "autonomous_full_local_only")
}

func briefOperatingMode(brief Brief) string {
	switch {
	case brief.NeedsRobert > 0:
		return "needs_robert"
	case brief.PlanningNeeded > 0:
		return "planning_needed"
	case brief.ReviewDue > 0:
		return "review_due"
	case brief.Stuck > 0:
		return "stuck"
	case brief.ReadyToMove > 0:
		return "ready_to_move"
	default:
		return "calm"
	}
}

func briefSummary(brief Brief, dashboard *Dashboard) string {
	active := int64(0)
	if dashboard != nil && dashboard.Counts != nil {
		active = dashboard.Counts["active"] + dashboard.Counts["waiting"] + dashboard.Counts["blocked"]
	}
	if active == 0 {
		return "No active pursuits are registered. HAI is ready to create pursuits from source intake, memory imports, or manual goals."
	}
	parts := []string{fmt.Sprintf("%d active pursuits", active)}
	if brief.NeedsRobert > 0 {
		parts = append(parts, fmt.Sprintf("%d need Robert", brief.NeedsRobert))
	}
	if brief.PlanningNeeded > 0 {
		parts = append(parts, fmt.Sprintf("%d need a first plan", brief.PlanningNeeded))
	}
	if brief.ReviewDue > 0 {
		parts = append(parts, fmt.Sprintf("%d have review due", brief.ReviewDue))
	}
	if brief.ReadyToMove > 0 {
		parts = append(parts, fmt.Sprintf("%d can move without vague re-explaining", brief.ReadyToMove))
	}
	if brief.Stuck > 0 {
		parts = append(parts, fmt.Sprintf("%d are blocked or stale", brief.Stuck))
	}
	return strings.Join(parts, ", ") + "."
}

func briefPrimaryAction(brief Brief) string {
	switch brief.OperatingMode {
	case "needs_robert":
		return "Handle the Robert-only decisions first; approve, reject, or correct the proposed next action."
	case "planning_needed":
		return "Create first workflow plans for pursuits that have goals but no operational path."
	case "review_due":
		return "Review due pursuits and either mark them reviewed, snooze them, or convert them into next actions."
	case "stuck":
		return "Unblock stale or blocked pursuits by assigning the next follow-up, missing-input request, or review step."
	case "ready_to_move":
		return "Move VA-ready and system-ready work through the governed workflow layer."
	default:
		return "Keep scanning sources and memory for new pursuit candidates and unfinished work."
	}
}

func briefCards(dashboard *Dashboard, limit int) []BriefCard {
	if dashboard == nil || limit <= 0 {
		return []BriefCard{}
	}
	result := []BriefCard{}
	seen := map[uuid.UUID]bool{}
	add := func(queue string, items []PursuitListItem) {
		for _, item := range items {
			if len(result) >= limit {
				return
			}
			id := item.Pursuit.ID
			if id == uuid.Nil || seen[id] {
				continue
			}
			seen[id] = true
			result = append(result, briefCard(queue, item))
		}
	}
	add("Robert", dashboard.NeedsRobert)
	add("Plan", dashboard.PlanningNeeded)
	add("Review", dashboard.ReviewDue)
	add("Blocked", dashboard.Blocked)
	add("Stale", dashboard.Stale)
	add("VA-ready", dashboard.VAReady)
	add("System-ready", dashboard.SystemReady)
	add("Verify", dashboard.CompletionCandidates)
	add("Changed", dashboard.RecentlyChanged)
	return result
}

func briefCard(queue string, item PursuitListItem) BriefCard {
	return BriefCard{
		Queue:        queue,
		PursuitID:    item.Pursuit.ID.String(),
		Title:        item.Pursuit.Title,
		Action:       briefCardAction(queue, item),
		Context:      firstNonEmpty(item.WhatChanged, item.CurrentState, item.Pursuit.CurrentStateSummary, item.Pursuit.DesiredOutcome, "No operational movement recorded yet."),
		RiskLevel:    firstNonEmpty(item.Pursuit.RiskLevel, "medium"),
		EvidenceLine: briefEvidenceLine(item),
		NeedsRobert:  item.NeedsRobert > 0 || queue == "Robert",
	}
}

func briefCardAction(queue string, item PursuitListItem) string {
	if strings.TrimSpace(item.NextAction) != "" {
		return item.NextAction
	}
	if strings.TrimSpace(item.Pursuit.NextRecommendedAction) != "" {
		return item.Pursuit.NextRecommendedAction
	}
	switch queue {
	case "Plan":
		return "Create the first governed workflow plan."
	case "Review":
		return "Review the pursuit and choose whether to snooze, continue, or change direction."
	case "Blocked":
		return "Resolve the blocker or create a missing-information request."
	case "Stale":
		return "Create a follow-up so the pursuit starts moving again."
	case "Verify":
		return "Check evidence before marking the pursuit complete."
	default:
		return "Review the pursuit and choose the next concrete action."
	}
}

func briefEvidenceLine(item PursuitListItem) string {
	parts := []string{
		fmt.Sprintf("%d decision%s", item.DecisionCards, pluralSuffix(item.DecisionCards)),
		fmt.Sprintf("%d evidence", item.LinkedEvidence),
		fmt.Sprintf("%d timeline", item.TimelineItems),
	}
	if item.OpenLoops > 0 {
		parts = append(parts, fmt.Sprintf("%d open loop%s", item.OpenLoops, pluralSuffix(item.OpenLoops)))
	}
	return strings.Join(parts, " / ")
}

func dashboardDecisionCards(item PursuitListItem, detail *PursuitDetail) []PursuitDashboardDecision {
	if detail == nil {
		return nil
	}
	result := []PursuitDashboardDecision{}
	for _, decision := range detail.DecisionQueue {
		if !decisionNeedsRobert(decision) {
			continue
		}
		result = append(result, PursuitDashboardDecision{
			Pursuit:      detail.Pursuit,
			Decision:     decision,
			CurrentState: firstNonEmpty(item.CurrentState, detail.Summary.CurrentState, detail.Pursuit.CurrentStateSummary),
			NextAction:   firstNonEmpty(item.NextAction, detail.Pursuit.NextRecommendedAction, decision.Recommended),
			Blocked:      item.Blocked,
			EvidenceLine: briefEvidenceLine(item),
		})
	}
	return result
}

func decisionNeedsRobert(decision PursuitDecision) bool {
	return strings.EqualFold(decision.Status, "pending") || decision.RequiresApproval
}

func sortDashboardDecisions(items []PursuitDashboardDecision) {
	sort.SliceStable(items, func(i, j int) bool {
		left := items[i]
		right := items[j]
		if left.Decision.RequiresApproval != right.Decision.RequiresApproval {
			return left.Decision.RequiresApproval
		}
		if decisionRiskRank(left.Decision.RiskLevel, left.Pursuit.RiskLevel) != decisionRiskRank(right.Decision.RiskLevel, right.Pursuit.RiskLevel) {
			return decisionRiskRank(left.Decision.RiskLevel, left.Pursuit.RiskLevel) > decisionRiskRank(right.Decision.RiskLevel, right.Pursuit.RiskLevel)
		}
		if left.Blocked != right.Blocked {
			return left.Blocked > right.Blocked
		}
		return parseDecisionCardTime(left.Decision.CreatedAt).After(parseDecisionCardTime(right.Decision.CreatedAt))
	})
}

func decisionRiskRank(values ...string) int {
	risk := strings.ToLower(strings.TrimSpace(firstNonEmpty(values...)))
	switch risk {
	case "critical", "highest":
		return 5
	case "high":
		return 4
	case "medium":
		return 3
	case "low":
		return 2
	default:
		return 1
	}
}

func parseDecisionCardTime(value string) time.Time {
	parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(value))
	if err != nil {
		return time.Time{}
	}
	return parsed
}

func limitDashboardDecisions(items *[]PursuitDashboardDecision, limit int) {
	if len(*items) > limit {
		*items = (*items)[:limit]
	}
}

func pluralSuffix(count int) string {
	if count == 1 {
		return ""
	}
	return "s"
}

func linkUUIDs(links []models.PursuitLink, linkType string) []uuid.UUID {
	result := []uuid.UUID{}
	seen := map[uuid.UUID]bool{}
	for _, link := range links {
		if link.LinkType != linkType {
			continue
		}
		id, err := uuid.Parse(link.LinkID)
		if err != nil || seen[id] {
			continue
		}
		seen[id] = true
		result = append(result, id)
	}
	return result
}

func workflowAutomationIDs(workflows []models.WorkflowItem) []uuid.UUID {
	result := []uuid.UUID{}
	for _, item := range workflows {
		id, err := uuid.Parse(strings.TrimSpace(item.AutomationID))
		if err != nil {
			continue
		}
		result = append(result, id)
	}
	return uniqueUUIDs(result)
}

func uniqueUUIDs(ids []uuid.UUID) []uuid.UUID {
	result := []uuid.UUID{}
	seen := map[uuid.UUID]bool{}
	for _, id := range ids {
		if id == uuid.Nil || seen[id] {
			continue
		}
		seen[id] = true
		result = append(result, id)
	}
	return result
}

func completedRuntimeAttempts(attempts []models.AutomationLaunchEvent) int {
	count := 0
	for _, attempt := range attempts {
		if runtimeAttemptCompleted(attempt.Status) {
			count++
		}
	}
	return count
}

func runtimeAttemptCompleted(status string) bool {
	return strings.EqualFold(strings.TrimSpace(status), "completed")
}

func resolvedPursuitDecisions(activity []models.PursuitActivity) map[string]bool {
	result := map[string]bool{}
	for _, item := range activity {
		if !strings.EqualFold(item.EventType, "pursuit.decision_resolved") {
			continue
		}
		decisionID := strings.TrimSpace(item.SourceID)
		if decisionID == "" {
			continue
		}
		result[decisionID] = true
	}
	return result
}

func runtimeAttemptNeedsReview(attempt models.AutomationLaunchEvent) bool {
	status := strings.ToLower(strings.TrimSpace(attempt.Status))
	switch status {
	case "failed", "blocked", "error", "timeout", "timed_out", "cancelled", "canceled", "needs_review", "unsupported":
		return true
	case "completed", "ready":
		return false
	default:
		return attempt.ExitCode != 0
	}
}

func runtimeAttemptDecisionID(attempt models.AutomationLaunchEvent) string {
	return "runtime:" + attempt.ID.String() + ":review"
}

func runtimeAttemptForDecision(attempts []models.AutomationLaunchEvent, request DecisionResolutionRequest) (models.AutomationLaunchEvent, bool) {
	candidates := []uuid.UUID{}
	if id, ok := runtimeLaunchIDFromEvidenceURI(request.EvidenceURI); ok {
		candidates = append(candidates, id)
	}
	if id, ok := runtimeLaunchIDFromDecisionID(request.DecisionID); ok {
		candidates = append(candidates, id)
	}
	for _, attempt := range attempts {
		for _, candidate := range candidates {
			if attempt.ID == candidate {
				return attempt, true
			}
		}
	}
	if len(attempts) == 1 {
		return attempts[0], true
	}
	return models.AutomationLaunchEvent{}, false
}

func runtimeLaunchIDFromDecisionID(value string) (uuid.UUID, bool) {
	parts := strings.Split(strings.TrimSpace(value), ":")
	if len(parts) != 3 || !strings.EqualFold(parts[0], "runtime") || !strings.EqualFold(parts[2], "review") {
		return uuid.Nil, false
	}
	id, err := uuid.Parse(parts[1])
	if err != nil {
		return uuid.Nil, false
	}
	return id, true
}

func runtimeAttemptHasRecoveryWorkflow(attempt models.AutomationLaunchEvent, workflows []models.WorkflowItem) bool {
	sourceURI := "automation-launch://" + attempt.ID.String()
	for _, item := range workflows {
		if strings.EqualFold(strings.TrimSpace(item.SourceURI), sourceURI) {
			return true
		}
	}
	return false
}

func runtimeRecoveryWorkflowInput(pursuit models.Pursuit, attempt models.AutomationLaunchEvent, request DecisionResolutionRequest) string {
	parts := []string{
		"Create a governed recovery workflow for a blocked agent runtime attempt.",
		"Pursuit: " + pursuit.Title,
		"Desired outcome: " + firstNonEmpty(pursuit.DesiredOutcome, "restore safe controlled runtime progress without bypassing HAI policy"),
		"Runtime evidence: automation-launch://" + attempt.ID.String(),
		"Runtime: " + runtimeAttemptLabel(attempt),
		"Status: " + firstNonEmpty(attempt.Status, "unknown"),
		"Failure/review reason: " + firstNonEmpty(request.Reason, runtimeAttemptReason(attempt)),
	}
	if route := runtimeAttemptRouteSummary(attempt); route != "" {
		parts = append(parts, "Route trace: "+route)
	}
	if attempt.RuntimeRouteTrace != nil {
		if controls := compactRuntimeTraceList(attempt.RuntimeRouteTrace.RequiredControls, 6); controls != "" {
			parts = append(parts, "Required controls: "+controls)
		}
		if checks := compactRuntimeTraceList(attempt.RuntimeRouteTrace.ValidationChecklist, 6); checks != "" {
			parts = append(parts, "Validation checklist: "+checks)
		}
	}
	parts = append(parts,
		"Recovery rules:",
		"- Do not retry the runtime directly from this decision.",
		"- Keep external messages, public posting, account changes, destructive file changes, and broad host/browser control approval-gated.",
		"- Diagnose missing configuration, allowlist, workspace, token, safety, or adapter issues first.",
		"- Produce a concrete checklist with evidence requirements and a safe retry condition.",
		"- Verify the recovery result before treating the pursuit as unblocked.",
	)
	if strings.TrimSpace(request.Note) != "" {
		parts = append(parts, "Robert note: "+strings.TrimSpace(request.Note))
	}
	return strings.Join(parts, "\n")
}

func runtimeAttemptBlockers(attempts []models.AutomationLaunchEvent, workflows []models.WorkflowItem, resolvedDecisions map[string]bool) []PursuitBlocker {
	result := []PursuitBlocker{}
	for _, attempt := range attempts {
		if !runtimeAttemptNeedsReview(attempt) {
			continue
		}
		if runtimeAttemptRecoveredByWorkflow(attempt, workflows) {
			continue
		}
		owner := "Robert"
		if resolvedDecisions[runtimeAttemptDecisionID(attempt)] {
			owner = "System"
		}
		result = append(result, PursuitBlocker{
			Label:  "Runtime attempt needs review: " + runtimeAttemptLabel(attempt),
			Reason: runtimeAttemptReason(attempt),
			Owner:  owner,
		})
		if len(result) >= 5 {
			break
		}
	}
	return result
}

func runtimeAttemptRecoveredByWorkflow(attempt models.AutomationLaunchEvent, workflows []models.WorkflowItem) bool {
	sourceURI := "automation-launch://" + attempt.ID.String()
	for _, item := range workflows {
		if !strings.EqualFold(strings.TrimSpace(item.SourceURI), sourceURI) {
			continue
		}
		if item.CurrentState != workflow.StateCompleted {
			continue
		}
		if acceptedCompletionStatus(item.VerificationStatus) {
			return true
		}
		if item.RecoveryStatus == workflow.RecoveryCompletedAfterRetry || item.RecoveryStatus == workflow.RecoveryCompletionConfirmed {
			return true
		}
	}
	return false
}

func runtimeAttemptLabel(attempt models.AutomationLaunchEvent) string {
	return firstNonEmpty(attempt.RuntimeType, attempt.LaunchType, attempt.AutomationID.String(), "agent runtime")
}

func runtimeAttemptReason(attempt models.AutomationLaunchEvent) string {
	reason := firstNonEmpty(attempt.Message, attempt.Output, attempt.Target, "runtime attempt did not complete successfully")
	if route := runtimeAttemptRouteSummary(attempt); route != "" {
		reason = reason + " | " + route
	}
	if strings.TrimSpace(attempt.Status) != "" {
		return fmt.Sprintf("%s: %s", attempt.Status, reason)
	}
	return reason
}

func runtimeAttemptRouteRisk(attempt models.AutomationLaunchEvent) string {
	if attempt.RuntimeRouteTrace == nil {
		return ""
	}
	return strings.TrimSpace(attempt.RuntimeRouteTrace.RiskLevel)
}

func runtimeAttemptRouteSummary(attempt models.AutomationLaunchEvent) string {
	trace := attempt.RuntimeRouteTrace
	if trace == nil {
		return ""
	}
	parts := []string{}
	if value := strings.TrimSpace(trace.Intent); value != "" {
		parts = append(parts, "intent="+compactCandidateText(value, 90))
	}
	if value := strings.TrimSpace(trace.ExecutionMode); value != "" {
		parts = append(parts, "mode="+compactCandidateText(value, 90))
	}
	if value := compactRuntimeTraceList(trace.RecommendedSkills, 4); value != "" {
		parts = append(parts, "skills="+value)
	}
	if value := compactRuntimeTraceList(trace.VisibleTools, 4); value != "" {
		parts = append(parts, "tools="+value)
	}
	if value := compactRuntimeTraceList(trace.BlockedSurfaces, 4); value != "" {
		parts = append(parts, "blocked="+value)
	}
	if len(parts) == 0 {
		return ""
	}
	return "runtime route: " + strings.Join(parts, "; ")
}

func compactRuntimeTraceList(values []string, limit int) string {
	cleaned := []string{}
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if seen[key] {
			continue
		}
		seen[key] = true
		cleaned = append(cleaned, compactCandidateText(value, 60))
	}
	if len(cleaned) == 0 {
		return ""
	}
	if limit > 0 && len(cleaned) > limit {
		return strings.Join(cleaned[:limit], ", ") + fmt.Sprintf(" +%d more", len(cleaned)-limit)
	}
	return strings.Join(cleaned, ", ")
}

func limitListItems(items *[]PursuitListItem, limit int) {
	if len(*items) > limit {
		*items = (*items)[:limit]
	}
}

// sortListItemsByEffectiveActivity keeps the observational dashboard lane in
// the same order as the derived operational timestamp. Other queues retain
// their own policy ordering (risk, approvals, or work readiness).
func sortListItemsByEffectiveActivity(items []PursuitListItem) {
	sort.SliceStable(items, func(i, j int) bool {
		left := effectiveListItemActivity(items[i])
		right := effectiveListItemActivity(items[j])
		if !left.Equal(right) {
			return left.After(right)
		}
		if items[i].Pursuit.PriorityScore != items[j].Pursuit.PriorityScore {
			return items[i].Pursuit.PriorityScore > items[j].Pursuit.PriorityScore
		}
		if items[i].Pursuit.Title != items[j].Pursuit.Title {
			return items[i].Pursuit.Title < items[j].Pursuit.Title
		}
		return items[i].Pursuit.ID.String() < items[j].Pursuit.ID.String()
	})
}

func effectiveListItemActivity(item PursuitListItem) time.Time {
	return firstTime(
		timeFromPointer(item.EffectiveLastActivityAt),
		timeFromPointer(item.Pursuit.LastActivityAt),
		item.Pursuit.UpdatedAt,
	)
}

func isStale(pursuit models.Pursuit) bool {
	return isStaleAt(firstTime(timeFromPointer(pursuit.LastActivityAt), pursuit.UpdatedAt))
}

func isStaleAt(activityAt time.Time) bool {
	if activityAt.IsZero() {
		return true
	}
	return time.Since(activityAt) > 14*24*time.Hour
}

func isReviewDue(pursuit models.Pursuit) bool {
	return pursuit.NextReviewAt != nil && !pursuit.NextReviewAt.After(time.Now().UTC())
}

func pursuitGoalContractReady(pursuit models.Pursuit) bool {
	return len(pursuit.SuccessCriteria) > 0 && len(pursuit.StopConditions) > 0
}

func pursuitGoalContractCounts(pursuit models.Pursuit) (criteriaSatisfied, triggeredStops, openDependencies int) {
	for _, criterion := range pursuit.SuccessCriteria {
		if oneOf(strings.ToLower(strings.TrimSpace(criterion.Status)), CriterionSatisfied, CriterionWaived) {
			criteriaSatisfied++
		}
	}
	for _, condition := range pursuit.StopConditions {
		if strings.EqualFold(strings.TrimSpace(condition.Status), StopTriggered) {
			triggeredStops++
		}
	}
	for _, dependency := range pursuit.Dependencies {
		if !oneOf(strings.ToLower(strings.TrimSpace(dependency.Status)), DependencySatisfied, DependencyWaived) {
			openDependencies++
		}
	}
	return criteriaSatisfied, triggeredStops, openDependencies
}

func pursuitTargetOverdue(pursuit models.Pursuit) bool {
	return pursuit.TargetAt != nil && pursuit.TargetAt.Before(time.Now().UTC()) && !pursuitClosed(pursuit)
}

func triggeredStopConditionReason(pursuit models.Pursuit) string {
	for _, condition := range pursuit.StopConditions {
		if strings.EqualFold(strings.TrimSpace(condition.Status), StopTriggered) {
			return firstNonEmpty(condition.Reason, condition.Description, "a pursuit stop condition is active")
		}
	}
	return ""
}

func pursuitGoalContractCompletionBlocker(pursuit models.Pursuit) string {
	if len(pursuit.SuccessCriteria) == 0 {
		return "no measurable success criteria are defined"
	}
	if len(pursuit.StopConditions) == 0 {
		return "no resource stop conditions are defined"
	}
	pendingCriteria := 0
	for _, criterion := range pursuit.SuccessCriteria {
		if !oneOf(strings.ToLower(strings.TrimSpace(criterion.Status)), CriterionSatisfied, CriterionWaived) && !implicitSuccessCriterion(criterion) {
			pendingCriteria++
		}
	}
	if pendingCriteria > 0 {
		return fmt.Sprintf("%d success criterion%s remain unresolved", pendingCriteria, pluralSuffix(pendingCriteria))
	}
	if reason := triggeredStopConditionReason(pursuit); reason != "" {
		return "a stop condition is still triggered: " + reason
	}
	openDependencies := 0
	for _, dependency := range pursuit.Dependencies {
		if !oneOf(strings.ToLower(strings.TrimSpace(dependency.Status)), DependencySatisfied, DependencyWaived) {
			openDependencies++
		}
	}
	if openDependencies > 0 {
		return fmt.Sprintf("%d dependenc%s remain unresolved", openDependencies, map[bool]string{true: "y", false: "ies"}[openDependencies == 1])
	}
	return ""
}

func implicitSuccessCriterion(criterion models.PursuitSuccessCriterion) bool {
	return oneOf(strings.TrimSpace(criterion.ID), "default-success-criterion", "legacy-completion-definition")
}

func satisfyImplicitSuccessCriteria(pursuit *models.Pursuit) {
	if pursuit == nil {
		return
	}
	for index := range pursuit.SuccessCriteria {
		criterion := &pursuit.SuccessCriteria[index]
		if implicitSuccessCriterion(*criterion) && strings.EqualFold(strings.TrimSpace(criterion.Status), CriterionPending) {
			criterion.Status = CriterionSatisfied
			criterion.VerificationStatus = "human_approved"
		}
	}
}

func pursuitGoalContractBlockers(pursuit models.Pursuit) []PursuitBlocker {
	if pursuitClosed(pursuit) {
		return []PursuitBlocker{}
	}
	result := []PursuitBlocker{}
	if !pursuitGoalContractReady(pursuit) {
		result = append(result, PursuitBlocker{
			Label:  "Outcome contract incomplete",
			Reason: "Define at least one measurable success criterion and one stop condition.",
			Owner:  "Robert",
		})
	}
	for _, condition := range pursuit.StopConditions {
		if strings.EqualFold(strings.TrimSpace(condition.Status), StopTriggered) {
			result = append(result, PursuitBlocker{
				Label:  firstNonEmpty(condition.Description, "Triggered stop condition"),
				Reason: firstNonEmpty(condition.Reason, "HAI must pause resource use until Robert resolves this stop condition."),
				Owner:  "Robert",
			})
		}
	}
	for _, dependency := range pursuit.Dependencies {
		status := strings.ToLower(strings.TrimSpace(dependency.Status))
		if oneOf(status, DependencySatisfied, DependencyWaived) {
			continue
		}
		result = append(result, PursuitBlocker{
			Label:      firstNonEmpty(dependency.Label, "Unresolved pursuit dependency"),
			Reason:     firstNonEmpty(dependency.Reason, "This prerequisite must be satisfied or explicitly waived before completion."),
			Owner:      firstNonEmpty(dependency.Owner, "External or Robert"),
			FollowUpAt: dependency.DueAt,
		})
	}
	if pursuitTargetOverdue(pursuit) {
		result = append(result, PursuitBlocker{
			Label:      "Target date passed",
			Reason:     "Review the target, scope, dependencies, and next action before spending more effort.",
			Owner:      "Robert",
			FollowUpAt: pursuit.TargetAt,
		})
	}
	return result
}

func pursuitNeedsPlanning(pursuit models.Pursuit, workflowCount int) bool {
	if pursuit.Archived || strings.EqualFold(pursuit.Status, StatusCompleted) || strings.EqualFold(pursuit.CompletionState, CompletionVerified) {
		return false
	}
	return workflowCount == 0
}

func pursuitNewWorkBlockerReason(pursuit models.Pursuit, workflows []models.WorkflowItem) string {
	if reason := triggeredStopConditionReason(pursuit); reason != "" {
		return "triggered stop condition: " + reason
	}
	if pursuitTargetOverdue(pursuit) {
		return "the target date has passed; review or update the goal contract before spending more effort"
	}
	for _, dependency := range pursuit.Dependencies {
		if strings.EqualFold(strings.TrimSpace(dependency.Status), DependencyBlocked) {
			return "dependency is blocked: " + firstNonEmpty(dependency.Label, dependency.Reason, "unnamed dependency")
		}
	}
	limit := pursuit.ResourceLimits.MaxParallelWorkflows
	if limit <= 0 {
		return ""
	}
	active := 0
	for _, item := range workflows {
		if item.Archived || strings.EqualFold(item.CurrentState, workflow.StateArchived) || strings.EqualFold(item.CurrentState, workflow.StateCompleted) {
			continue
		}
		active++
	}
	if active >= limit {
		return fmt.Sprintf("parallel workflow ceiling reached (%d of %d active)", active, limit)
	}
	return ""
}

func pursuitPlanInput(pursuit models.Pursuit) string {
	parts := []string{
		"Create the first operational workflow for this pursuit.",
		"Goal: " + pursuit.Title,
	}
	if strings.TrimSpace(pursuit.DesiredOutcome) != "" {
		parts = append(parts, "Desired outcome: "+strings.TrimSpace(pursuit.DesiredOutcome))
	}
	if strings.TrimSpace(pursuit.WhyItMatters) != "" {
		parts = append(parts, "Why it matters: "+strings.TrimSpace(pursuit.WhyItMatters))
	}
	if strings.TrimSpace(pursuit.Description) != "" {
		parts = append(parts, "Context: "+strings.TrimSpace(pursuit.Description))
	}
	if strings.TrimSpace(pursuit.CurrentStateSummary) != "" {
		parts = append(parts, "Current state: "+strings.TrimSpace(pursuit.CurrentStateSummary))
	}
	if strings.TrimSpace(pursuit.NextRecommendedAction) != "" {
		parts = append(parts, "Recommended next action: "+strings.TrimSpace(pursuit.NextRecommendedAction))
	}
	if strings.TrimSpace(pursuit.CompletionDefinition) != "" {
		parts = append(parts, "Completion definition: "+strings.TrimSpace(pursuit.CompletionDefinition))
	}
	if len(pursuit.SuccessCriteria) > 0 {
		parts = append(parts, "Success criteria:")
		for _, criterion := range pursuit.SuccessCriteria {
			parts = append(parts, "- ["+firstNonEmpty(criterion.Status, CriterionPending)+"] "+criterion.Description)
		}
	}
	if len(pursuit.StopConditions) > 0 {
		parts = append(parts, "Stop conditions:")
		for _, condition := range pursuit.StopConditions {
			parts = append(parts, "- ["+firstNonEmpty(condition.Status, StopMonitoring)+"] "+condition.Description)
		}
	}
	if len(pursuit.Dependencies) > 0 {
		parts = append(parts, "Dependencies:")
		for _, dependency := range pursuit.Dependencies {
			parts = append(parts, "- ["+firstNonEmpty(dependency.Status, DependencyPending)+"] "+dependency.Label)
		}
	}
	if pursuit.TargetAt != nil {
		parts = append(parts, "Target date: "+pursuit.TargetAt.UTC().Format(time.RFC3339))
	}
	if pursuit.ResourceLimits.MaxEffortHours > 0 {
		parts = append(parts, fmt.Sprintf("Maximum effort: %.1f hours", pursuit.ResourceLimits.MaxEffortHours))
	}
	if pursuit.ResourceLimits.MaxSpendEUR > 0 {
		parts = append(parts, fmt.Sprintf("Maximum spend: EUR %.2f (still subject to global paid-use policy and approval)", pursuit.ResourceLimits.MaxSpendEUR))
	}
	if pursuit.ResourceLimits.MaxParallelWorkflows > 0 {
		parts = append(parts, fmt.Sprintf("Maximum parallel workflows: %d", pursuit.ResourceLimits.MaxParallelWorkflows))
	}
	if strings.TrimSpace(pursuit.RiskLevel) != "" {
		parts = append(parts, "Risk level: "+strings.TrimSpace(pursuit.RiskLevel))
	}
	if strings.TrimSpace(pursuit.AutonomyLevel) != "" {
		parts = append(parts, "Autonomy level: "+strings.TrimSpace(pursuit.AutonomyLevel))
	}
	parts = append(parts, "Output needed: concrete workflow checklist, evidence requirements, approval boundaries, and first safe next action.")
	return strings.Join(parts, "\n")
}

func reviewOwner(pursuit models.Pursuit) string {
	if strings.EqualFold(pursuit.RiskLevel, "high") || strings.EqualFold(pursuit.AutonomyLevel, "approve_before_execute") {
		return "Robert"
	}
	return "System or VA"
}

func firstActionLabel(actions []PursuitAction) string {
	if len(actions) == 0 {
		return ""
	}
	return actions[0].Label
}

func classifyDomain(text string) string {
	lower := strings.ToLower(text)
	switch {
	case containsAny(lower, "legal", "lawyer", "government", "municipality", "insurance", "claim", "dispute"):
		return "stability"
	case containsAny(lower, "automation", "github", "developer", "software", "code", "hai"):
		return "work"
	case containsAny(lower, "health", "doctor", "medical"):
		return "health"
	case containsAny(lower, "client", "invoice", "quote", "job"):
		return "business"
	default:
		return "operations"
	}
}

func classifyRisk(text string) string {
	lower := strings.ToLower(text)
	switch {
	case containsAny(lower, "legal", "lawyer", "government", "municipality", "insurance", "financial", "money", "publish", "public", "delete", "account"):
		return "high"
	case containsAny(lower, "client", "deadline", "contract", "message", "email"):
		return "medium"
	default:
		return "low"
	}
}

func classifyNeed(text string) string {
	lower := strings.ToLower(text)
	switch {
	case containsAny(lower, "housing", "legal", "government", "insurance", "money", "safety", "stability"):
		return "safety_and_stability"
	case containsAny(lower, "client", "work", "business", "automation", "developer"):
		return "work_and_capability"
	case containsAny(lower, "health", "doctor", "capacity"):
		return "health_and_capacity"
	default:
		return "operations"
	}
}

func defaultAutonomy(risk string) string {
	if risk == "high" {
		return "approve_before_execute"
	}
	if risk == "medium" {
		return "suggest"
	}
	return "autonomous_safe"
}

// conservativeRisk prevents a manually supplied label from downgrading risk
// detected from the pursuit's own goal, background, rationale, and desired outcome.
// A caller can always make a pursuit more conservative, never less so.
func conservativeRisk(requested, detected string) string {
	requested = normalizeRisk(requested)
	detected = normalizeRisk(detected)
	if detected == "" {
		detected = "medium"
	}
	if riskRank(requested) > riskRank(detected) {
		return requested
	}
	return detected
}

func normalizeRisk(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "low", "medium", "high":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return ""
	}
}

func riskRank(value string) int {
	switch normalizeRisk(value) {
	case "high":
		return 3
	case "medium":
		return 2
	case "low":
		return 1
	default:
		return 0
	}
}

// conservativeAutonomy keeps the pursuit label no more permissive than its
// risk class. It is a presentation and planning guard; the workflow/task
// engine remains the authority for any real execution.
func conservativeAutonomy(requested, risk string) string {
	requested = normalizeAutonomy(requested)
	switch normalizeRisk(risk) {
	case "high":
		switch requested {
		case "manual", "suggest", "approve_before_execute":
			return requested
		default:
			return "approve_before_execute"
		}
	case "medium":
		switch requested {
		case "manual", "suggest", "approve_before_execute":
			return requested
		default:
			return "suggest"
		}
	default:
		switch requested {
		case "manual", "suggest", "approve_before_execute", "autonomous_safe":
			return requested
		default:
			return "autonomous_safe"
		}
	}
}

func normalizeAutonomy(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "manual", "suggest", "approve_before_execute", "autonomous_safe", "autonomous_full_local_only":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return ""
	}
}

func policyWasNormalized(requestedRisk, requestedAutonomy, risk, autonomy string) bool {
	if strings.TrimSpace(requestedRisk) != "" && !strings.EqualFold(normalizeRisk(requestedRisk), risk) {
		return true
	}
	return strings.TrimSpace(requestedAutonomy) != "" && !strings.EqualFold(normalizeAutonomy(requestedAutonomy), autonomy)
}

func normalizeWords(text string) map[string]bool {
	result := map[string]bool{}
	for _, token := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9')
	}) {
		if len(token) < 3 {
			continue
		}
		result[token] = true
	}
	return result
}

func wordOverlap(left, right map[string]bool) float64 {
	if len(left) == 0 || len(right) == 0 {
		return 0
	}
	overlap := 0
	for word := range left {
		if right[word] {
			overlap++
		}
	}
	return float64(overlap) / math.Sqrt(float64(len(left)*len(right)))
}

func confidenceLabel(score float64) string {
	switch {
	case score >= 0.7:
		return "high"
	case score >= 0.35:
		return "medium"
	default:
		return "low"
	}
}

func round(value float64) float64 {
	return math.Round(value*100) / 100
}

func containsAny(text string, values ...string) bool {
	for _, value := range values {
		if strings.Contains(text, value) {
			return true
		}
	}
	return false
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func uniqueStrings(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}

func assignString(value *string, target *string) {
	if value != nil {
		*target = strings.TrimSpace(*value)
	}
}

func clampInt(value, minimum, maximum, fallback int) int {
	if value == 0 {
		return fallback
	}
	if value < minimum {
		return minimum
	}
	if value > maximum {
		return maximum
	}
	return value
}

func normalizeConfidence(value, fallback float64) float64 {
	if value <= 0 {
		return fallback
	}
	if value > 1 {
		return 1
	}
	return value
}

func normalizePursuitGoalContract(
	criteria []models.PursuitSuccessCriterion,
	conditions []models.PursuitStopCondition,
	dependencies []models.PursuitDependency,
	limits models.PursuitResourceLimits,
	targetAt string,
	reviewCadenceDays int,
	fallbackCriterion string,
	allowDefaults bool,
) ([]models.PursuitSuccessCriterion, []models.PursuitStopCondition, []models.PursuitDependency, models.PursuitResourceLimits, *time.Time, int, error) {
	normalizedCriteria, err := normalizeSuccessCriteria(criteria, fallbackCriterion, allowDefaults)
	if err != nil {
		return nil, nil, nil, models.PursuitResourceLimits{}, nil, 0, err
	}
	normalizedConditions, err := normalizeStopConditions(conditions, allowDefaults)
	if err != nil {
		return nil, nil, nil, models.PursuitResourceLimits{}, nil, 0, err
	}
	normalizedDependencies, err := normalizeDependencies(dependencies)
	if err != nil {
		return nil, nil, nil, models.PursuitResourceLimits{}, nil, 0, err
	}
	normalizedLimits, err := normalizeResourceLimits(limits)
	if err != nil {
		return nil, nil, nil, models.PursuitResourceLimits{}, nil, 0, err
	}
	target, err := parseOptionalStrictTime(targetAt, "targetAt")
	if err != nil {
		return nil, nil, nil, models.PursuitResourceLimits{}, nil, 0, err
	}
	if reviewCadenceDays < 0 || reviewCadenceDays > 3650 {
		return nil, nil, nil, models.PursuitResourceLimits{}, nil, 0, fmt.Errorf("reviewCadenceDays must be between 0 and 3650")
	}
	return normalizedCriteria, normalizedConditions, normalizedDependencies, normalizedLimits, target, reviewCadenceDays, nil
}

func normalizeSuccessCriteria(values []models.PursuitSuccessCriterion, fallback string, allowDefault bool) ([]models.PursuitSuccessCriterion, error) {
	if len(values) == 0 && allowDefault {
		values = []models.PursuitSuccessCriterion{{
			ID:               "default-success-criterion",
			Description:      firstNonEmpty(strings.TrimSpace(fallback), "The stated pursuit outcome is achieved and supported by accepted evidence."),
			Status:           CriterionPending,
			EvidenceRequired: true,
		}}
	}
	if len(values) == 0 {
		return nil, fmt.Errorf("at least one success criterion is required")
	}
	if len(values) > 25 {
		return nil, fmt.Errorf("success criteria cannot contain more than 25 items")
	}
	result := make([]models.PursuitSuccessCriterion, 0, len(values))
	seen := map[string]bool{}
	for index, value := range values {
		value.Description = strings.TrimSpace(value.Description)
		if value.Description == "" {
			return nil, fmt.Errorf("successCriteria[%d].description is required", index)
		}
		if len(value.Description) > 2000 {
			return nil, fmt.Errorf("successCriteria[%d].description is too long", index)
		}
		value.ID = normalizeContractItemID(value.ID)
		if seen[value.ID] {
			return nil, fmt.Errorf("successCriteria contains duplicate id %q", value.ID)
		}
		seen[value.ID] = true
		value.Status = strings.ToLower(strings.TrimSpace(value.Status))
		if value.Status == "" {
			value.Status = CriterionPending
		}
		if !oneOf(value.Status, CriterionPending, CriterionSatisfied, CriterionWaived) {
			return nil, fmt.Errorf("successCriteria[%d].status must be pending, satisfied, or waived", index)
		}
		value.EvidenceURI = strings.TrimSpace(value.EvidenceURI)
		value.VerificationStatus = strings.ToLower(strings.TrimSpace(value.VerificationStatus))
		value.WaiverReason = strings.TrimSpace(value.WaiverReason)
		if value.Status == CriterionSatisfied && value.EvidenceRequired && value.EvidenceURI == "" && !acceptedCriterionVerificationStatus(value.VerificationStatus) {
			return nil, fmt.Errorf("successCriteria[%d] requires evidence or an accepted verification status before it can be satisfied", index)
		}
		if value.Status == CriterionWaived && (value.WaiverReason == "" || value.VerificationStatus != "human_approved") {
			return nil, fmt.Errorf("successCriteria[%d] requires a waiver reason and human_approved verification status", index)
		}
		result = append(result, value)
	}
	return result, nil
}

func normalizeStopConditions(values []models.PursuitStopCondition, allowDefault bool) ([]models.PursuitStopCondition, error) {
	if len(values) == 0 && allowDefault {
		values = []models.PursuitStopCondition{{
			ID:          "default-safe-stop",
			Description: "Stop and request review when the pursuit no longer serves its outcome or exceeds an approved boundary.",
			Status:      StopMonitoring,
		}}
	}
	if len(values) == 0 {
		return nil, fmt.Errorf("at least one stop condition is required")
	}
	if len(values) > 25 {
		return nil, fmt.Errorf("stop conditions cannot contain more than 25 items")
	}
	result := make([]models.PursuitStopCondition, 0, len(values))
	seen := map[string]bool{}
	now := time.Now().UTC()
	for index, value := range values {
		value.Description = strings.TrimSpace(value.Description)
		if value.Description == "" {
			return nil, fmt.Errorf("stopConditions[%d].description is required", index)
		}
		value.ID = normalizeContractItemID(value.ID)
		if seen[value.ID] {
			return nil, fmt.Errorf("stopConditions contains duplicate id %q", value.ID)
		}
		seen[value.ID] = true
		value.Status = strings.ToLower(strings.TrimSpace(value.Status))
		if value.Status == "" {
			value.Status = StopMonitoring
		}
		if !oneOf(value.Status, StopMonitoring, StopTriggered, StopResolved) {
			return nil, fmt.Errorf("stopConditions[%d].status must be monitoring, triggered, or resolved", index)
		}
		value.Reason = strings.TrimSpace(value.Reason)
		if value.Status == StopTriggered && value.TriggeredAt == nil {
			triggeredAt := now
			value.TriggeredAt = &triggeredAt
		}
		if value.Status == StopResolved {
			if value.Reason == "" {
				return nil, fmt.Errorf("stopConditions[%d] requires a resolution reason", index)
			}
			if value.ResolvedAt == nil {
				resolvedAt := now
				value.ResolvedAt = &resolvedAt
			}
		}
		result = append(result, value)
	}
	return result, nil
}

func normalizeDependencies(values []models.PursuitDependency) ([]models.PursuitDependency, error) {
	if len(values) > 50 {
		return nil, fmt.Errorf("dependencies cannot contain more than 50 items")
	}
	result := make([]models.PursuitDependency, 0, len(values))
	seen := map[string]bool{}
	for index, value := range values {
		value.Label = strings.TrimSpace(value.Label)
		if value.Label == "" {
			return nil, fmt.Errorf("dependencies[%d].label is required", index)
		}
		value.ID = normalizeContractItemID(value.ID)
		if seen[value.ID] {
			return nil, fmt.Errorf("dependencies contains duplicate id %q", value.ID)
		}
		seen[value.ID] = true
		value.Status = strings.ToLower(strings.TrimSpace(value.Status))
		if value.Status == "" {
			value.Status = DependencyPending
		}
		if !oneOf(value.Status, DependencyPending, DependencySatisfied, DependencyBlocked, DependencyWaived) {
			return nil, fmt.Errorf("dependencies[%d].status must be pending, satisfied, blocked, or waived", index)
		}
		value.Owner = strings.TrimSpace(value.Owner)
		value.RelatedPursuitID = strings.TrimSpace(value.RelatedPursuitID)
		value.EvidenceURI = strings.TrimSpace(value.EvidenceURI)
		value.Reason = strings.TrimSpace(value.Reason)
		if value.RelatedPursuitID != "" {
			if _, err := uuid.Parse(value.RelatedPursuitID); err != nil {
				return nil, fmt.Errorf("dependencies[%d].relatedPursuitId must be a UUID", index)
			}
		}
		if value.Status == DependencySatisfied && value.EvidenceURI == "" && value.RelatedPursuitID == "" {
			return nil, fmt.Errorf("dependencies[%d] needs evidence or a related pursuit before it can be satisfied", index)
		}
		if value.Status == DependencyWaived && value.Reason == "" {
			return nil, fmt.Errorf("dependencies[%d] requires a waiver reason", index)
		}
		result = append(result, value)
	}
	return result, nil
}

func normalizeResourceLimits(value models.PursuitResourceLimits) (models.PursuitResourceLimits, error) {
	value.Notes = strings.TrimSpace(value.Notes)
	if math.IsNaN(value.MaxEffortHours) || math.IsInf(value.MaxEffortHours, 0) || value.MaxEffortHours < 0 || value.MaxEffortHours > 1_000_000 {
		return models.PursuitResourceLimits{}, fmt.Errorf("resourceLimits.maxEffortHours must be between 0 and 1000000")
	}
	if math.IsNaN(value.MaxSpendEUR) || math.IsInf(value.MaxSpendEUR, 0) || value.MaxSpendEUR < 0 || value.MaxSpendEUR > 1_000_000_000 {
		return models.PursuitResourceLimits{}, fmt.Errorf("resourceLimits.maxSpendEur must be between 0 and 1000000000")
	}
	if value.MaxParallelWorkflows < 0 || value.MaxParallelWorkflows > 1000 {
		return models.PursuitResourceLimits{}, fmt.Errorf("resourceLimits.maxParallelWorkflows must be between 0 and 1000")
	}
	if len(value.Notes) > 4000 {
		return models.PursuitResourceLimits{}, fmt.Errorf("resourceLimits.notes is too long")
	}
	return value, nil
}

func normalizeContractItemID(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 120 {
		return uuid.NewString()
	}
	return value
}

func acceptedCriterionVerificationStatus(status string) bool {
	return oneOf(strings.ToLower(strings.TrimSpace(status)), "verified", "source_supported", "schema_validated", "test_passed", "human_approved")
}

func oneOf(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}

func parseOptionalStrictTime(value, field string) (*time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			utc := parsed.UTC()
			return &utc, nil
		}
	}
	return nil, fmt.Errorf("%s must be an RFC3339 timestamp or YYYY-MM-DD date", field)
}

func parseOptionalTime(value string) *time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		utc := parsed.UTC()
		return &utc
	}
	if parsed, err := time.Parse("2006-01-02", value); err == nil {
		utc := parsed.UTC()
		return &utc
	}
	return nil
}
