package task

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"automation-hub-backend/internal/actionresolver"
	"automation-hub-backend/internal/automation"
	"automation-hub-backend/internal/autonomygate"
	"automation-hub-backend/internal/braincatalog"
	"automation-hub-backend/internal/executionauth"
	"automation-hub-backend/internal/frameworkevidence"
	"automation-hub-backend/internal/frameworkregistry"
	"automation-hub-backend/internal/lifeontology"
	"automation-hub-backend/internal/llm"
	"automation-hub-backend/internal/memory"
	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/plangraph"
	"automation-hub-backend/internal/resourceplanner"
	"automation-hub-backend/internal/safety"
	"automation-hub-backend/internal/source"
	"automation-hub-backend/internal/sourceevidence"
	"automation-hub-backend/internal/verification"

	"github.com/google/uuid"
)

var (
	ErrTaskLLMRouterNotConfigured = errors.New("task LLM router is not configured")
	ErrInvalidStandingMandateID   = errors.New("invalid standing mandate id")
)

type IntakeRequest struct {
	OwnerIdentity string `json:"-"`
	// IdempotencyKey identifies one caller intent. Reusing it with the same
	// request replays the durable result; reusing it for changed work fails.
	IdempotencyKey string `json:"idempotencyKey,omitempty"`
	PursuitID      string `json:"pursuitId,omitempty"`
	// WorkflowID is internal worker context. It prevents the workflow-owned
	// task run from being duplicated in the direct pursuit task-attempt ledger.
	WorkflowID string `json:"-"`
	// CoordinationPlan is exact advisory provenance. Resolving it never grants
	// execution authority; all normal approval and executionauth gates remain.
	CoordinationPlan plangraph.AcceptedRevisionReference `json:"coordinationPlan,omitempty"`
	Request          string                              `json:"request"`
	ProjectKey       string                              `json:"projectKey,omitempty"`
	AutomationID     string                              `json:"automationId,omitempty"`
	// MandateID selects a bounded standing mandate for the eventual controlled
	// effect. It never grants authority by itself; executionauth resolves it
	// against the authenticated owner and exact action.
	MandateID       string   `json:"mandateId,omitempty"`
	SuccessCriteria []string `json:"successCriteria,omitempty"`
	ExecuteAllowed  bool     `json:"executeAllowed,omitempty"`
	// ExecutionRequested preserves the caller's execution intent during a
	// side-effect-free preview. It is internal context only and never grants
	// execution authority.
	ExecutionRequested    bool                                    `json:"-"`
	HumanApproved         bool                                    `json:"humanApproved,omitempty"`
	ApprovalNote          string                                  `json:"approvalNote,omitempty"`
	ApprovalSourceID      string                                  `json:"-"`
	ApprovalBindingDigest string                                  `json:"-"`
	ApprovalActorIdentity string                                  `json:"-"`
	ApprovalApprovedAt    *time.Time                              `json:"-"`
	ObservedNeeds         []frameworkregistry.NeedStateAssessment `json:"-"`
	Capacity              *frameworkregistry.CapacitySnapshot     `json:"-"`
	AvailableAgents       []frameworkregistry.AgentCard           `json:"-"`
	CoordinationMode      string                                  `json:"-"`
	Deadline              *time.Time                              `json:"-"`
	operationID           string
	reviewItemID          string
	executionContext      context.Context
}

type IntakeAnalysis struct {
	TaskType            string   `json:"taskType"`
	RiskLevel           string   `json:"riskLevel"`
	Difficulty          int      `json:"difficulty"`
	RequiredReasoning   string   `json:"requiredReasoning"`
	SuccessCriteria     []string `json:"successCriteria"`
	NeedsMemory         bool     `json:"needsMemory"`
	NeedsTools          bool     `json:"needsTools"`
	NeedsDocuments      bool     `json:"needsDocuments"`
	NeedsWebAccess      bool     `json:"needsWebAccess"`
	NeedsLocalExecution bool     `json:"needsLocalExecution"`
	NeedsApproval       bool     `json:"needsApproval"`
	Reason              string   `json:"reason"`
}

// OperatingContextProvider supplies owner-scoped, source-backed personal state
// to the task planner. Browser requests cannot set these values directly; the
// provider is the trust boundary that resolves the latest reviewed records.
type OperatingContextProvider interface {
	LatestNeeds(ownerIdentity string, at time.Time) ([]frameworkregistry.NeedStateAssessment, error)
	LatestCapacity(ownerIdentity string, at time.Time) (*frameworkregistry.CapacitySnapshot, error)
}

type OperatingContextRecorder interface {
	RecordTaskDomains(
		ownerIdentity string,
		taskPlanID string,
		assignments []frameworkregistry.LifeDomainAssignment,
		selectionID string,
	) error
}

type ContextPlan struct {
	Strategy                 []string                         `json:"strategy"`
	UsedContext              []memory.RankedMemory            `json:"usedContext"`
	SourceContext            []source.RankedExtraction        `json:"sourceContext"`
	LifeContext              []lifeontology.ContextSuggestion `json:"lifeContext,omitempty"`
	SourceRefresh            *source.ScheduledSyncRun         `json:"sourceRefresh,omitempty"`
	SourceRefreshExplanation string                           `json:"sourceRefreshExplanation,omitempty"`
	LifeContextExplanation   string                           `json:"lifeContextExplanation,omitempty"`
	Explanation              string                           `json:"explanation"`
}

type ValidationPlan struct {
	Steps                          []string                    `json:"steps"`
	SuccessCriteria                []string                    `json:"successCriteria"`
	FrameworkEvidenceRequirements  []string                    `json:"frameworkEvidenceRequirements"`
	FrameworkCompletionCriteria    []string                    `json:"frameworkCompletionCriteria"`
	FrameworkAssuranceCriteria     []string                    `json:"frameworkAssuranceCriteria"`
	FrameworkEvidenceContracts     []FrameworkEvidenceContract `json:"frameworkEvidenceContracts"`
	DomainPackEvidenceRequirements []string                    `json:"domainPackEvidenceRequirements"`
	DomainPackSuccessCriteria      []string                    `json:"domainPackSuccessCriteria"`
	DomainPackValidators           []string                    `json:"domainPackValidators"`
	DomainPackMethodEvaluation     []string                    `json:"domainPackMethodEvaluation"`
	FailurePolicy                  string                      `json:"failurePolicy"`
	CompletionGate                 string                      `json:"completionGate"`
}

type ExecutionPlan struct {
	PlanningSeparatedFromExecution bool                                       `json:"planningSeparatedFromExecution"`
	ControlledExecutionMode        string                                     `json:"controlledExecutionMode"`
	ApprovalRequiredFor            []string                                   `json:"approvalRequiredFor"`
	AuditEvents                    []string                                   `json:"auditEvents"`
	CapacityConstraints            []string                                   `json:"capacityConstraints"`
	AgentCards                     []frameworkregistry.AgentCard              `json:"agentCards"`
	AgentTeams                     []frameworkregistry.AgentTeamContract      `json:"agentTeams,omitempty"`
	AgentTeamAuthorityBoundary     string                                     `json:"agentTeamAuthorityBoundary,omitempty"`
	Delegations                    []frameworkregistry.DelegationContract     `json:"delegations"`
	Communication                  frameworkregistry.CommunicationContract    `json:"communication"`
	Coordination                   frameworkregistry.CoordinationPlan         `json:"coordination"`
	ActionAutonomy                 []frameworkregistry.ActionAutonomyDecision `json:"actionAutonomy"`
	StopConditions                 []string                                   `json:"stopConditions"`
	OutcomeMonitoring              []string                                   `json:"outcomeMonitoring"`
	DomainPackLocalOnly            bool                                       `json:"domainPackLocalOnly"`
	DomainPackAuthorityBoundary    string                                     `json:"domainPackAuthorityBoundary,omitempty"`
	AdvisoryAgentCapabilities      []string                                   `json:"advisoryAgentCapabilities"`
}

type ToolRouteDecision struct {
	SelectedTools             []string                                `json:"selectedTools"`
	SkippedTools              []string                                `json:"skippedTools"`
	BlockedTools              []string                                `json:"blockedTools"`
	CatalogRecommendations    []braincatalog.Recommendation           `json:"catalogRecommendations,omitempty"`
	CapabilityRecommendations []braincatalog.CapabilityRecommendation `json:"capabilityRecommendations,omitempty"`
	Reason                    string                                  `json:"reason"`
}

type TaskStep struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	Purpose          string `json:"purpose"`
	Allowed          bool   `json:"allowed"`
	RequiresApproval bool   `json:"requiresApproval"`
	Status           string `json:"status"`
}

type RiskAssessment struct {
	Level                     string   `json:"level"`
	ApprovalRequired          bool     `json:"approvalRequired"`
	ApprovalGranted           bool     `json:"approvalGranted"`
	ApprovalSourceID          string   `json:"approvalSourceId,omitempty"`
	ApprovalActorIdentity     string   `json:"approvalActorIdentity,omitempty"`
	ActionResolution          string   `json:"actionResolution"`
	MissingParameters         []string `json:"missingParameters,omitempty"`
	FrameworkAutonomyCeiling  int      `json:"frameworkAutonomyCeiling,omitempty"`
	RequiredFrameworkAutonomy int      `json:"requiredFrameworkAutonomy,omitempty"`
	Reasons                   []string `json:"reasons"`
	AllowedNow                bool     `json:"allowedNow"`
}

type ValidationResult struct {
	Passed        bool                        `json:"passed"`
	Status        string                      `json:"status"`
	Checked       []string                    `json:"checked"`
	Failures      []string                    `json:"failures"`
	Criteria      []ValidationCriterionResult `json:"criteria"`
	NextAction    string                      `json:"nextAction"`
	AttemptNumber int                         `json:"attemptNumber"`
}

type RetryPolicy struct {
	MaxAttempts    int      `json:"maxAttempts"`
	EscalationPath []string `json:"escalationPath"`
	EscalateWhen   []string `json:"escalateWhen"`
	CurrentAttempt int      `json:"currentAttempt"`
	RetryAvailable bool     `json:"retryAvailable"`
}

type ReviewQueueItem struct {
	ID             string        `json:"id"`
	TaskID         string        `json:"taskId"`
	Request        IntakeRequest `json:"request"`
	Reason         string        `json:"reason"`
	Priority       string        `json:"priority"`
	Status         string        `json:"status"`
	Decision       string        `json:"decision,omitempty"`
	ResolutionNote string        `json:"resolutionNote,omitempty"`
	CreatedAt      time.Time     `json:"createdAt"`
	ResolvedAt     *time.Time    `json:"resolvedAt,omitempty"`
}

type ApprovalDecision struct {
	Approved     bool   `json:"approved"`
	Note         string `json:"note,omitempty"`
	Confirmation string `json:"confirmation,omitempty"`
}

const TaskOperationRetryConfirmation = "RETRY UNCERTAIN OPERATION"

type ReviewResolutionResult struct {
	Item              ReviewQueueItem `json:"item"`
	Plan              *CompletionPlan `json:"plan,omitempty"`
	LearningOutcomeID string          `json:"learningOutcomeId,omitempty"`
}

type TaskEvent struct {
	At      time.Time `json:"at"`
	Stage   string    `json:"stage"`
	Message string    `json:"message"`
}

type ExecutedAction struct {
	Name      string    `json:"name"`
	Status    string    `json:"status"`
	Input     string    `json:"input,omitempty"`
	Output    string    `json:"output,omitempty"`
	StartedAt time.Time `json:"startedAt"`
	EndedAt   time.Time `json:"endedAt"`
}

type ToolExecutionRequest struct {
	OwnerIdentity         string                           `json:"-"`
	TaskID                string                           `json:"-"`
	AutomationID          string                           `json:"automationId"`
	Task                  string                           `json:"task"`
	OriginalRequest       string                           `json:"-"`
	ProjectKey            string                           `json:"projectKey,omitempty"`
	MandateID             string                           `json:"-"`
	WorkflowID            string                           `json:"-"`
	ApprovalSourceID      string                           `json:"-"`
	ApprovalBindingDigest string                           `json:"-"`
	Governance            executionauth.GovernanceEvidence `json:"-"`
	approvalDecision      *automation.TaskApprovalDecisionRequest
	executionContext      context.Context
}

type ToolExecutionResult struct {
	AutomationID      string                              `json:"automationId"`
	LaunchEventID     string                              `json:"launchEventId,omitempty"`
	RuntimeType       string                              `json:"runtimeType,omitempty"`
	LaunchType        string                              `json:"launchType"`
	Target            string                              `json:"target,omitempty"`
	Status            string                              `json:"status"`
	Message           string                              `json:"message,omitempty"`
	Output            string                              `json:"output,omitempty"`
	RuntimeRouteTrace *models.AutomationRuntimeRouteTrace `json:"runtimeRouteTrace,omitempty"`
	ExitCode          int                                 `json:"exitCode"`
	DurationMs        int64                               `json:"durationMs"`
	RequiresApproval  bool                                `json:"requiresApproval"`
	AuditEvents       []string                            `json:"auditEvents"`
	ExecutedAt        time.Time                           `json:"executedAt"`
}

type ToolExecutor interface {
	Execute(request ToolExecutionRequest) (*ToolExecutionResult, error)
}

type FrameworkSelector interface {
	PlanSelection(request frameworkregistry.SelectionRequest) (*frameworkregistry.SelectionDecision, error)
}

// PursuitAttemptRecorder stores a compact audit projection for task work that
// is explicitly scoped to a pursuit. Retrieved context and generated output
// stay in the existing task and verification paths.
type PursuitAttemptRecorder interface {
	UpsertTaskAttempt(attempt models.PursuitTaskAttempt) error
}

// PursuitTaskGuard is an optional lifecycle boundary for a pursuit-scoped
// direct task plan or run. The task engine owns planning and execution, while
// the pursuit service owns whether a pursuit is active and eligible for work.
type PursuitTaskGuard interface {
	ValidatePursuitTaskAttempt(pursuitID uuid.UUID, ownerIdentity string) error
}

// PursuitResourceReservationManager is the atomic accounting boundary used
// only for pursuit-scoped execution attempts. Planning and preview never hold
// capacity, and every retry receives its own independently checked hold.
type PursuitResourceReservationManager interface {
	ReservePursuitTaskResources(pursuitID uuid.UUID, ownerIdentity, operationID string, effortMinutes, costMicros int64) error
	SettlePursuitTaskResources(pursuitID uuid.UUID, ownerIdentity, operationID, disposition string, actualEffortMinutes, actualCostMicros int64) error
}

type ExecutionResult struct {
	StartedAt          time.Time                  `json:"startedAt"`
	CompletedAt        time.Time                  `json:"completedAt"`
	Mode               string                     `json:"mode"`
	Output             string                     `json:"output"`
	VerificationStatus string                     `json:"verificationStatus"`
	Claims             []models.VerificationClaim `json:"claims"`
	EvidenceCount      int                        `json:"evidenceCount"`
	UnsupportedClaims  int                        `json:"unsupportedClaims"`
	LLMGeneration      *llm.GenerationResult      `json:"llmGeneration,omitempty"`
	ToolExecution      *ToolExecutionResult       `json:"toolExecution,omitempty"`
	Actions            []ExecutedAction           `json:"actions"`
	BlockedReason      string                     `json:"blockedReason,omitempty"`
}

type MemoryUpdateProposal struct {
	Kind       string   `json:"kind"`
	Content    string   `json:"content"`
	Tags       []string `json:"tags"`
	Reason     string   `json:"reason"`
	Confidence float64  `json:"confidence"`
}

type CompletionPlan struct {
	ID                         string                                    `json:"id"`
	OperationID                string                                    `json:"operationId"`
	IdempotencyKey             string                                    `json:"idempotencyKey"`
	OwnerIdentity              string                                    `json:"-"`
	ReviewItemID               string                                    `json:"reviewItemId,omitempty"`
	PursuitID                  string                                    `json:"pursuitId,omitempty"`
	CoordinationPlan           *plangraph.AcceptedRevisionBinding        `json:"coordinationPlan,omitempty"`
	CoordinationDraft          *plangraph.Plan                           `json:"coordinationDraft,omitempty"`
	CreatedAt                  time.Time                                 `json:"createdAt"`
	Request                    string                                    `json:"request"`
	ProjectKey                 string                                    `json:"projectKey,omitempty"`
	RealGoal                   string                                    `json:"realGoal"`
	Intake                     IntakeAnalysis                            `json:"intake"`
	ContextPlan                ContextPlan                               `json:"contextPlan"`
	MinimalityDecision         MinimalityDecision                        `json:"minimalityDecision"`
	FrameworkDecision          *frameworkregistry.SelectionDecision      `json:"frameworkDecision,omitempty"`
	DomainPackDecision         *DomainPackDecision                       `json:"domainPackDecision,omitempty"`
	CalendarCapacity           CalendarCapacityContext                   `json:"calendarCapacity"`
	ResourceDecision           *resourceplanner.Decision                 `json:"resourceDecision,omitempty"`
	ModelDecision              llm.RouteDecision                         `json:"modelDecision"`
	ToolDecision               ToolRouteDecision                         `json:"toolDecision"`
	Steps                      []TaskStep                                `json:"steps"`
	RiskAssessment             RiskAssessment                            `json:"riskAssessment"`
	ValidationPlan             ValidationPlan                            `json:"validationPlan"`
	FrameworkEvidencePreflight *FrameworkEvidencePreflightResult         `json:"frameworkEvidencePreflight,omitempty"`
	ValidationResult           ValidationResult                          `json:"validationResult"`
	ExecutionPlan              ExecutionPlan                             `json:"executionPlan"`
	ExecutionResult            *ExecutionResult                          `json:"executionResult,omitempty"`
	RetryPolicy                RetryPolicy                               `json:"retryPolicy"`
	ReviewQueueItem            *ReviewQueueItem                          `json:"reviewQueueItem,omitempty"`
	MemoryUpdateProposals      []MemoryUpdateProposal                    `json:"memoryUpdateProposals"`
	LessonsLearned             []MemoryUpdateProposal                    `json:"lessonsLearned"`
	StoredMemoryIDs            []string                                  `json:"storedMemoryIds"`
	LifeGraphProjection        *lifeontology.OperationalProjectionResult `json:"lifeGraphProjection,omitempty"`
	LifeGraphProjectionError   string                                    `json:"lifeGraphProjectionError,omitempty"`
	Events                     []TaskEvent                               `json:"events"`
	CompletionStatus           string                                    `json:"completionStatus"`
}

type CalendarCapacityContext struct {
	Status        string                        `json:"status"`
	WindowStart   time.Time                     `json:"windowStart"`
	WindowEnd     time.Time                     `json:"windowEnd"`
	BusyIntervals []source.CalendarBusyInterval `json:"busyIntervals"`
	Explanation   string                        `json:"explanation"`
}

type calendarCapacityProvider interface {
	CalendarBusyIntervalsForOwner(ownerIdentity string, start, end time.Time) ([]source.CalendarBusyInterval, error)
}

type Service interface {
	Plan(request IntakeRequest) (*CompletionPlan, error)
	Run(request IntakeRequest) (*CompletionPlan, error)
	Logs() []CompletionPlan
	ReviewQueue() []ReviewQueueItem
	ResolveReviewItem(id string, decision ApprovalDecision) (*ReviewResolutionResult, error)
}

// ContextService binds an interactive task run to its caller lifecycle while
// preserving the legacy Service contract used by durable workflow workers and
// narrow test doubles.
type ContextService interface {
	RunContext(context.Context, IntakeRequest) (*CompletionPlan, error)
}

type PlanningContextService interface {
	PlanContext(context.Context, IntakeRequest) (*CompletionPlan, error)
}

// OwnerScopedService is the authenticated view over task history and approvals.
// It is intentionally separate from Service so background workers can retain
// their system-level access without becoming an HTTP data-leak path.
type OwnerScopedService interface {
	LogsForOwner(ownerIdentity string) []CompletionPlan
	ReviewQueueForOwner(ownerIdentity string) []ReviewQueueItem
	ResolveReviewItemForOwner(ownerIdentity, id string, decision ApprovalDecision) (*ReviewResolutionResult, error)
}

// DurableOwnerScopedService exposes storage failures to authenticated HTTP
// handlers. The legacy slice-returning methods remain for internal workers and
// test doubles, but external reads must not turn a failed ledger into an empty
// and therefore misleading history.
type DurableOwnerScopedService interface {
	LogsForOwnerWithError(ownerIdentity string) ([]CompletionPlan, error)
	ReviewQueueForOwnerWithError(ownerIdentity string) ([]ReviewQueueItem, error)
}

// BoundedDurableOwnerScopedService lets HTTP readers request a smaller recent
// history without changing the wider internal worker contract.
type BoundedDurableOwnerScopedService interface {
	LogsForOwnerWithLimit(ownerIdentity string, limit int) ([]CompletionPlan, error)
}

// CompactDurableOwnerScopedService serves the immutable list projection used
// by dashboards without loading full execution plans and evidence payloads.
type CompactDurableOwnerScopedService interface {
	HistoryForOwnerWithLimit(ownerIdentity string, limit int) ([]CompletionPlanHistoryItem, error)
}

const internalTaskStateOwnerIdentity = "urn:hai:internal:task-system"

// PreviewService is the side-effect-free planning boundary used by reviewed
// local interoperability adapters. A preview may read owner-scoped context but
// must not refresh sources, persist task attempts, create review items, or
// execute work.
type PreviewService interface {
	Preview(request IntakeRequest) (*CompletionPlan, error)
}

type service struct {
	memoryService         memory.Service
	sourceService         source.Service
	verificationService   verification.Service
	llmService            *llm.Service
	toolExecutor          ToolExecutor
	pursuitAttempts       PursuitAttemptRecorder
	frameworkSelector     FrameworkSelector
	frameworkEvidence     frameworkevidence.Repository
	sourceEvidence        sourceevidence.Repository
	domainPackPlanner     DomainPackPlanner
	resourcePlanner       ResourcePlanner
	lifeOntology          LifeOntologyContextProvider
	lifeOntologyProjector LifeOntologyProjectionRecorder
	agentTeams            AgentTeamContextProvider
	stateRepository       TaskStateRepository
	operatingContext      OperatingContextProvider
	agentContext          AgentContextProvider
	controlledLearning    ControlledLearningRecorder
	acceptedPlanResolver  plangraph.AcceptedRevisionResolver
	coordinationProjector CoordinationPlanProjector
	mu                    sync.Mutex
	logs                  []CompletionPlan
	reviewQueue           []ReviewQueueItem
}

func NewService(memoryService memory.Service, llmService *llm.Service, sourceServices ...source.Service) Service {
	var sourceService source.Service
	if len(sourceServices) > 0 && sourceServices[0] != nil {
		sourceService = sourceServices[0]
	}
	return &service{
		memoryService:     memoryService,
		sourceService:     sourceService,
		llmService:        llmService,
		frameworkSelector: defaultFrameworkSelector(),
		frameworkEvidence: frameworkevidence.NewMemoryRepository(),
		domainPackPlanner: defaultDomainPackPlanner(),
		stateRepository:   NewMemoryTaskStateRepository(),
		logs:              []CompletionPlan{},
		reviewQueue:       []ReviewQueueItem{},
	}
}

func NewServiceWithEngines(memoryService memory.Service, llmService *llm.Service, sourceService source.Service, verificationService verification.Service, toolExecutors ...ToolExecutor) Service {
	var toolExecutor ToolExecutor
	if len(toolExecutors) > 0 {
		toolExecutor = toolExecutors[0]
	}
	return &service{
		memoryService:       memoryService,
		sourceService:       sourceService,
		verificationService: verificationService,
		llmService:          llmService,
		toolExecutor:        toolExecutor,
		frameworkSelector:   defaultFrameworkSelector(),
		frameworkEvidence:   frameworkevidence.NewMemoryRepository(),
		domainPackPlanner:   defaultDomainPackPlanner(),
		stateRepository:     NewMemoryTaskStateRepository(),
		logs:                []CompletionPlan{},
		reviewQueue:         []ReviewQueueItem{},
	}
}

func NewServiceWithEnginesAndPursuitAttempts(memoryService memory.Service, llmService *llm.Service, sourceService source.Service, verificationService verification.Service, toolExecutor ToolExecutor, pursuitAttempts PursuitAttemptRecorder, frameworkSelectors ...FrameworkSelector) Service {
	selector := defaultFrameworkSelector()
	if len(frameworkSelectors) > 0 && frameworkSelectors[0] != nil {
		selector = frameworkSelectors[0]
	}
	return NewServiceWithDependencies(
		memoryService,
		llmService,
		sourceService,
		verificationService,
		toolExecutor,
		pursuitAttempts,
		selector,
		NewMemoryTaskStateRepository(),
	)
}

func NewServiceWithDependencies(
	memoryService memory.Service,
	llmService *llm.Service,
	sourceService source.Service,
	verificationService verification.Service,
	toolExecutor ToolExecutor,
	pursuitAttempts PursuitAttemptRecorder,
	frameworkSelector FrameworkSelector,
	stateRepository TaskStateRepository,
	operatingContextProviders ...OperatingContextProvider,
) Service {
	if frameworkSelector == nil {
		frameworkSelector = defaultFrameworkSelector()
	}
	if stateRepository == nil {
		stateRepository = NewMemoryTaskStateRepository()
	}
	var operatingContext OperatingContextProvider
	var agentContext AgentContextProvider
	if len(operatingContextProviders) > 0 {
		operatingContext = operatingContextProviders[0]
		agentContext, _ = operatingContext.(AgentContextProvider)
	}
	return newServiceWithDependencies(
		memoryService,
		llmService,
		sourceService,
		verificationService,
		toolExecutor,
		pursuitAttempts,
		frameworkSelector,
		stateRepository,
		operatingContext,
		agentContext,
	)
}

// NewServiceWithDependenciesAndAgentContext keeps the existing operating
// context contract intact while allowing a durable agent registry to supply
// owner-scoped agent cards to framework selection.
func NewServiceWithDependenciesAndAgentContext(
	memoryService memory.Service,
	llmService *llm.Service,
	sourceService source.Service,
	verificationService verification.Service,
	toolExecutor ToolExecutor,
	pursuitAttempts PursuitAttemptRecorder,
	frameworkSelector FrameworkSelector,
	stateRepository TaskStateRepository,
	operatingContext OperatingContextProvider,
	agentContext AgentContextProvider,
) Service {
	if frameworkSelector == nil {
		frameworkSelector = defaultFrameworkSelector()
	}
	if stateRepository == nil {
		stateRepository = NewMemoryTaskStateRepository()
	}
	return newServiceWithDependencies(
		memoryService,
		llmService,
		sourceService,
		verificationService,
		toolExecutor,
		pursuitAttempts,
		frameworkSelector,
		stateRepository,
		operatingContext,
		agentContext,
	)
}

func newServiceWithDependencies(
	memoryService memory.Service,
	llmService *llm.Service,
	sourceService source.Service,
	verificationService verification.Service,
	toolExecutor ToolExecutor,
	pursuitAttempts PursuitAttemptRecorder,
	frameworkSelector FrameworkSelector,
	stateRepository TaskStateRepository,
	operatingContext OperatingContextProvider,
	agentContext AgentContextProvider,
) Service {
	return &service{
		memoryService:       memoryService,
		sourceService:       sourceService,
		verificationService: verificationService,
		llmService:          llmService,
		toolExecutor:        toolExecutor,
		pursuitAttempts:     pursuitAttempts,
		frameworkSelector:   frameworkSelector,
		frameworkEvidence:   frameworkevidence.NewMemoryRepository(),
		domainPackPlanner:   defaultDomainPackPlanner(),
		stateRepository:     stateRepository,
		operatingContext:    operatingContext,
		agentContext:        agentContext,
		logs:                []CompletionPlan{},
		reviewQueue:         []ReviewQueueItem{},
	}
}

func defaultFrameworkSelector() FrameworkSelector {
	service, err := frameworkregistry.NewService(nil)
	if err != nil {
		panic(fmt.Sprintf("initialize framework selector: %v", err))
	}
	return service
}

func DefaultService() (Service, error) {
	llmService, err := llm.NewServiceFromEnv()
	if err != nil {
		return nil, err
	}
	frameworkService, err := frameworkregistry.DefaultService()
	if err != nil {
		return nil, err
	}
	stateRepository, err := DefaultTaskStateRepository()
	if err != nil {
		return nil, err
	}
	frameworkEvidenceRepository, err := frameworkevidence.DefaultRepository()
	if err != nil {
		return nil, err
	}
	sourceEvidenceRepository, err := sourceevidence.DefaultRepository()
	if err != nil {
		return nil, err
	}
	result := NewServiceWithDependencies(
		memory.DefaultService(),
		llmService,
		source.DefaultService(),
		verification.DefaultService(),
		NewAutomationToolExecutor(automation.DefaultService()),
		nil,
		frameworkService,
		stateRepository,
	)
	result, err = WithFrameworkEvidenceRepository(result, frameworkEvidenceRepository)
	if err != nil {
		return nil, err
	}
	return WithSourceEvidenceRepository(result, sourceEvidenceRepository)
}

func (s *service) Plan(request IntakeRequest) (*CompletionPlan, error) {
	return s.PlanContext(context.Background(), request)
}

func (s *service) PlanContext(ctx context.Context, request IntakeRequest) (*CompletionPlan, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	request.executionContext = ctx
	return s.withTaskOperation(request, "plan", s.planOperation)
}

func (s *service) planOperation(request IntakeRequest) (*CompletionPlan, error) {
	request.ApprovalNote = sanitizeApprovalNote(request.ApprovalNote)
	binding, err := s.resolveCoordinationPlan(request)
	if err != nil {
		return nil, err
	}
	if err := s.validatePursuitAttemptRequest(request); err != nil {
		return nil, err
	}
	plan, err := s.buildPlan(request, false, true)
	if err != nil {
		return nil, err
	}
	plan.CoordinationPlan = binding
	if binding == nil {
		if err := s.projectCoordinationDraft(plan, request); err != nil {
			return nil, err
		}
	}
	if err := s.persistPursuitAttempt(plan, request, "plan", true); err != nil {
		return nil, err
	}
	s.projectDurableCompletionPlan(plan, request, "plan")
	if err := s.addLog(*plan); err != nil {
		return nil, err
	}
	return plan, nil
}

// Preview returns a planning draft without adding task logs, refreshing a
// connector, persisting a pursuit attempt, queueing approval, or executing an
// action. It is deliberately separate from Plan so an external protocol peer
// can never turn a request into durable operational work by accident.
func (s *service) Preview(request IntakeRequest) (*CompletionPlan, error) {
	binding, err := s.resolveCoordinationPlan(request)
	if err != nil {
		return nil, err
	}
	if err := s.validatePursuitAttemptRequest(request); err != nil {
		return nil, err
	}
	request.ExecuteAllowed = false
	request.HumanApproved = false
	request.ApprovalNote = ""
	plan, err := s.buildPlan(request, false, false)
	if plan != nil {
		plan.CoordinationPlan = binding
	}
	return plan, err
}

func (s *service) Run(request IntakeRequest) (*CompletionPlan, error) {
	return s.RunContext(context.Background(), request)
}

func (s *service) RunContext(ctx context.Context, request IntakeRequest) (*CompletionPlan, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	request.executionContext = ctx
	return s.withTaskOperation(request, "run", s.runOperation)
}

func (s *service) runOperation(request IntakeRequest) (*CompletionPlan, error) {
	request.ApprovalNote = sanitizeApprovalNote(request.ApprovalNote)
	binding, err := s.resolveCoordinationPlan(request)
	if err != nil {
		return nil, err
	}
	if err := s.validatePursuitAttemptRequest(request); err != nil {
		return nil, err
	}
	if safety.EmergencyStopActive() {
		request.ExecuteAllowed = false
		request.HumanApproved = false
		plan, err := s.buildPlan(request, false, true)
		if err != nil {
			return nil, err
		}
		plan.CoordinationPlan = binding
		reason := safety.EmergencyStopReason()
		started := time.Now().UTC()
		plan.ExecutionResult = &ExecutionResult{
			StartedAt:     started,
			CompletedAt:   started,
			Mode:          "blocked",
			Output:        "Execution was blocked by emergency stop.",
			BlockedReason: reason,
			Actions:       []ExecutedAction{executedAction("governance.emergency_stop", "blocked", plan.Request, reason, started)},
		}
		setTaskStepStatus(plan, "execute", "blocked")
		plan.ValidationResult.Passed = false
		plan.ValidationResult.Status = "blocked"
		plan.ValidationResult.Failures = append(plan.ValidationResult.Failures, reason)
		plan.ValidationResult.NextAction = "clear emergency stop before autonomous execution"
		plan.CompletionStatus = "review_required"
		plan.Events = append(plan.Events, event("governance", reason))
		if err := s.attachReviewItem(plan, reason, "high", request); err != nil {
			return nil, err
		}
		if err := s.persistPursuitAttempt(plan, request, "run", true); err != nil {
			return nil, err
		}
		s.projectDurableCompletionPlan(plan, request, "run")
		if err := s.addLog(*plan); err != nil {
			return nil, err
		}
		return plan, nil
	}
	plan, err := s.buildPlan(request, true, true)
	if err != nil {
		return nil, err
	}
	plan.CoordinationPlan = binding
	if err := s.persistPursuitAttempt(plan, request, "run", false); err != nil {
		return nil, err
	}
	if plan.RiskAssessment.AllowedNow {
		// Revalidate immediately before any effect. A replan between intake and
		// execution invalidates the prior accepted revision.
		binding, err = s.resolveCoordinationPlan(request)
		if err != nil {
			return nil, err
		}
		plan.CoordinationPlan = binding
		preflight := s.evaluateFrameworkEvidencePreflight(plan, request)
		plan.FrameworkEvidencePreflight = &preflight
		if preflight.Passed {
			if frameworkEvidenceDurabilityRequired(plan) {
				if persistErr := s.persistFrameworkEvidencePreflight(plan); persistErr != nil {
					plan.ExecutionResult = frameworkEvidencePersistenceBlockedExecution(plan, persistErr)
				} else {
					plan.ExecutionResult = s.executeWithPursuitReservation(plan, request, 1)
				}
			} else {
				plan.ExecutionResult = s.executeWithPursuitReservation(plan, request, 1)
			}
		} else {
			plan.ExecutionResult = frameworkEvidenceBlockedExecution(plan, preflight)
		}
		setExecutionStepStatus(plan)
	} else {
		setTaskStepStatus(plan, "execute", "blocked")
	}
	plan.ValidationResult = validatePlan(plan, 1)
	setValidationStepStatus(plan)
	plan.RetryPolicy.CurrentAttempt = 1
	plan.RetryPolicy.RetryAvailable = !plan.ValidationResult.Passed && plan.RetryPolicy.CurrentAttempt < plan.RetryPolicy.MaxAttempts
	s.recordGenerationValidation(plan)

	if !plan.RiskAssessment.AllowedNow {
		plan.CompletionStatus = "review_required"
		plan.ValidationResult.Passed = false
		plan.ValidationResult.Status = "blocked"
		plan.ValidationResult.NextAction = "human review required before execution"
		if err := s.attachReviewItem(plan, taskReviewReason(plan.RiskAssessment), plan.RiskAssessment.Level, request); err != nil {
			return nil, err
		}
	} else if plan.ExecutionResult != nil && plan.ExecutionResult.BlockedReason != "" {
		plan.CompletionStatus = "review_required"
		plan.ValidationResult.Passed = false
		plan.ValidationResult.Status = "blocked"
		plan.ValidationResult.NextAction = "resolve the execution blocker before retrying"
		plan.RetryPolicy.RetryAvailable = false
		if err := s.attachReviewItem(plan, plan.ExecutionResult.BlockedReason, plan.RiskAssessment.Level, request); err != nil {
			return nil, err
		}
	} else if plan.ValidationResult.Passed {
		plan.CompletionStatus = "validated"
		plan.ValidationResult.Status = "passed"
		plan.ValidationResult.NextAction = "mark task complete"
		plan.Events = append(plan.Events, event("validation", "execution result verified against success criteria"))
		plan.StoredMemoryIDs = s.storeLessons(plan)
		setMemoryStepStatus(plan)
	} else if plan.RetryPolicy.RetryAvailable {
		plan.Events = append(plan.Events, event("retry", "validation failed; retrying with fallback model route"))
		failed := false
		routeRequest := llm.RouteRequest{
			Task:              plan.Request,
			TaskType:          plan.Intake.TaskType,
			Difficulty:        plan.Intake.Difficulty,
			RequiredReasoning: plan.Intake.RequiredReasoning,
			ValidationPassed:  &failed,
			PreviousModelID:   plan.ModelDecision.SelectedModelID,
		}
		if s.llmService == nil {
			plan.Events = append(plan.Events, event("routing", "fallback model route skipped because the task LLM router is not configured"))
		} else if retryDecision, errRetry := s.llmService.Route(routeRequest); errRetry == nil {
			plan.ModelDecision = retryDecision
			plan.Events = append(plan.Events, event("routing", "fallback model route evaluated after validation failure"))
		}
		plan.ExecutionResult = s.executeWithPursuitReservation(plan, request, 2)
		setExecutionStepStatus(plan)
		plan.RetryPolicy.CurrentAttempt = 2
		plan.ValidationResult = validatePlan(plan, 2)
		setValidationStepStatus(plan)
		plan.RetryPolicy.RetryAvailable = !plan.ValidationResult.Passed && plan.RetryPolicy.CurrentAttempt < plan.RetryPolicy.MaxAttempts
		s.recordGenerationValidation(plan)
		if plan.ValidationResult.Passed {
			plan.CompletionStatus = "validated"
			plan.ValidationResult.Status = "passed"
			plan.ValidationResult.NextAction = "mark task complete"
			plan.Events = append(plan.Events, event("validation", "retry validated against success criteria"))
			plan.StoredMemoryIDs = s.storeLessons(plan)
			setMemoryStepStatus(plan)
		} else if plan.RetryPolicy.RetryAvailable {
			plan.CompletionStatus = "retry_needed"
		} else {
			plan.CompletionStatus = "review_required"
			if err := s.attachReviewItem(plan, "validation failed after retry", "medium", request); err != nil {
				return nil, err
			}
		}
	} else {
		plan.CompletionStatus = "review_required"
		if err := s.attachReviewItem(plan, "validation failed after available attempts", "medium", request); err != nil {
			return nil, err
		}
	}
	setMemoryStepStatus(plan)
	s.recordVerifiedLearningOutcome(plan)

	if err := s.persistPursuitAttempt(plan, request, "run", true); err != nil {
		return nil, err
	}
	s.projectDurableCompletionPlan(plan, request, "run")
	if err := s.addLog(*plan); err != nil {
		return nil, err
	}
	return plan, nil
}

func (s *service) validatePursuitAttemptRequest(request IntakeRequest) error {
	mandateID := strings.TrimSpace(request.MandateID)
	if mandateID != "" {
		parsedMandateID, err := uuid.Parse(mandateID)
		if err != nil || parsedMandateID == uuid.Nil {
			return ErrInvalidStandingMandateID
		}
	}
	pursuitID := strings.TrimSpace(request.PursuitID)
	if pursuitID == "" {
		return nil
	}
	parsedPursuitID, err := uuid.Parse(pursuitID)
	if err != nil {
		return fmt.Errorf("invalid pursuit id")
	}
	if s.pursuitAttempts == nil {
		return fmt.Errorf("pursuit task-attempt persistence is not configured")
	}
	if guard, ok := s.pursuitAttempts.(PursuitTaskGuard); ok {
		if err := guard.ValidatePursuitTaskAttempt(parsedPursuitID, request.OwnerIdentity); err != nil {
			return err
		}
	}
	return nil
}

func (s *service) persistPursuitAttempt(plan *CompletionPlan, request IntakeRequest, mode string, completed bool) error {
	if plan == nil || strings.TrimSpace(request.PursuitID) == "" || strings.TrimSpace(request.WorkflowID) != "" {
		return nil
	}
	pursuitID, err := uuid.Parse(strings.TrimSpace(request.PursuitID))
	if err != nil {
		return fmt.Errorf("invalid pursuit id")
	}
	if s.pursuitAttempts == nil {
		return fmt.Errorf("pursuit task-attempt persistence is not configured")
	}
	startedAt := plan.CreatedAt
	if plan.ExecutionResult != nil && !plan.ExecutionResult.StartedAt.IsZero() {
		startedAt = plan.ExecutionResult.StartedAt
	}
	status := "running"
	if mode == "plan" {
		status = "planned"
	}
	if completed {
		status = firstNonEmpty(plan.CompletionStatus, status)
	}
	var completedAt *time.Time
	if completed {
		when := time.Now().UTC()
		if plan.ExecutionResult != nil && !plan.ExecutionResult.CompletedAt.IsZero() {
			when = plan.ExecutionResult.CompletedAt
		}
		completedAt = &when
	}
	verificationStatus := strings.TrimSpace(plan.ValidationResult.Status)
	blockedReason := strings.Join(compactStrings(plan.ValidationResult.Failures, 3), "; ")
	if plan.ExecutionResult != nil {
		verificationStatus = firstNonEmpty(plan.ExecutionResult.VerificationStatus, verificationStatus)
		blockedReason = firstNonEmpty(plan.ExecutionResult.BlockedReason, blockedReason)
	}
	attempt := models.PursuitTaskAttempt{
		PursuitID:          pursuitID,
		TaskPlanID:         plan.ID,
		OwnerIdentity:      strings.TrimSpace(plan.OwnerIdentity),
		RequestSummary:     compactTaskRequest(plan.RealGoal),
		ProjectKey:         strings.TrimSpace(plan.ProjectKey),
		Mode:               mode,
		Status:             status,
		RiskLevel:          strings.TrimSpace(plan.RiskAssessment.Level),
		VerificationStatus: verificationStatus,
		AutomationID:       firstNonEmpty(request.AutomationID, planAutomationID(plan)),
		LaunchEventID:      planLaunchEventID(plan),
		BlockedReason:      compactTaskRequest(blockedReason),
		StartedAt:          &startedAt,
		CompletedAt:        completedAt,
	}
	if err := s.pursuitAttempts.UpsertTaskAttempt(attempt); err != nil {
		return fmt.Errorf("persist pursuit task attempt: %w", err)
	}
	return nil
}

func compactTaskRequest(value string) string {
	value = strings.Join(strings.Fields(strings.TrimSpace(safety.RedactSecrets(value))), " ")
	const limit = 500
	if len([]rune(value)) <= limit {
		return value
	}
	return string([]rune(value)[:limit]) + "..."
}

func compactStrings(values []string, limit int) []string {
	if limit <= 0 {
		return []string{}
	}
	result := make([]string, 0, limit)
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		result = append(result, value)
		if len(result) == limit {
			break
		}
	}
	return result
}

func planAutomationID(plan *CompletionPlan) string {
	if plan == nil || plan.ExecutionResult == nil || plan.ExecutionResult.ToolExecution == nil {
		return ""
	}
	return strings.TrimSpace(plan.ExecutionResult.ToolExecution.AutomationID)
}

func planLaunchEventID(plan *CompletionPlan) string {
	if plan == nil || plan.ExecutionResult == nil || plan.ExecutionResult.ToolExecution == nil {
		return ""
	}
	return strings.TrimSpace(plan.ExecutionResult.ToolExecution.LaunchEventID)
}

func (s *service) buildPlan(request IntakeRequest, runMode, allowSourceRefresh bool) (*CompletionPlan, error) {
	var err error
	request, err = s.loadOperatingContext(request)
	if err != nil {
		return nil, fmt.Errorf("load current operating context: %w", err)
	}
	intake := analyzeIntake(request)
	if s.llmService == nil {
		return nil, ErrTaskLLMRouterNotConfigured
	}
	planID := uuid.New().String()
	frameworkDecision, err := s.frameworkSelector.PlanSelection(frameworkregistry.SelectionRequest{
		OwnerIdentity:             request.OwnerIdentity,
		TaskPlanID:                planID,
		Request:                   request.Request,
		ProjectKey:                request.ProjectKey,
		PursuitID:                 request.PursuitID,
		TaskType:                  intake.TaskType,
		RiskLevel:                 intake.RiskLevel,
		Difficulty:                intake.Difficulty,
		RequiredReasoning:         intake.RequiredReasoning,
		SuccessCriteria:           intake.SuccessCriteria,
		NeedsMemory:               intake.NeedsMemory,
		NeedsTools:                intake.NeedsTools,
		NeedsDocuments:            intake.NeedsDocuments,
		NeedsWebAccess:            intake.NeedsWebAccess,
		NeedsLocalExecution:       intake.NeedsLocalExecution,
		NeedsApproval:             intake.NeedsApproval,
		ExecuteRequested:          request.ExecuteAllowed || request.ExecutionRequested,
		HumanApproved:             request.HumanApproved,
		ObservedNeeds:             request.ObservedNeeds,
		Capacity:                  request.Capacity,
		AvailableAgents:           request.AvailableAgents,
		PreferredCoordinationMode: request.CoordinationMode,
		Deadline:                  request.Deadline,
	})
	if err != nil {
		return nil, fmt.Errorf("select planning frameworks: %w", err)
	}
	domainPackPlanner := s.domainPackPlanner
	if domainPackPlanner == nil {
		domainPackPlanner = defaultDomainPackPlanner()
	}
	domainPackDecision, err := domainPackPlanner.PlanDomainPacks(DomainPackPlanningRequest{
		OwnerIdentity:       request.OwnerIdentity,
		Text:                strings.TrimSpace(request.Request + "\n" + request.ProjectKey),
		TaskType:            intake.TaskType,
		RiskLevel:           intake.RiskLevel,
		SuccessCriteria:     intake.SuccessCriteria,
		ExecuteRequested:    request.ExecuteAllowed,
		NeedsTools:          intake.NeedsTools,
		NeedsDocuments:      intake.NeedsDocuments,
		NeedsWebAccess:      intake.NeedsWebAccess,
		NeedsLocalExecution: intake.NeedsLocalExecution,
	})
	if err != nil {
		return nil, fmt.Errorf("plan advisory domain packs: %w", err)
	}
	if recorder, ok := s.operatingContext.(OperatingContextRecorder); ok {
		if err := recorder.RecordTaskDomains(
			request.OwnerIdentity,
			planID,
			frameworkDecision.LifeDomains,
			frameworkDecision.ID,
		); err != nil {
			return nil, fmt.Errorf("record task life-domain context: %w", err)
		}
	}
	var sourceRefresh *source.ScheduledSyncRun
	var sourceRefreshExplanation string
	if allowSourceRefresh {
		sourceRefresh, sourceRefreshExplanation, err = s.refreshSourcesForTask(request, intake)
		if err != nil {
			return nil, err
		}
	} else {
		sourceRefreshExplanation = "Source refresh is disabled for this planning preview."
	}
	contextResult, err := memory.RetrieveForOwnerContext(s.memoryService, taskExecutionContext(request), request.OwnerIdentity, memory.RetrieveRequest{
		Query:      request.Request,
		ProjectKey: request.ProjectKey,
		Limit:      8,
	})
	if err != nil {
		return nil, err
	}
	sourceContext, sourceExplanation, err := s.retrieveSourceContext(taskExecutionContext(request), request, intake)
	if err != nil {
		return nil, err
	}
	modelDecision, err := s.llmService.Route(llm.RouteRequest{
		Task:              request.Request,
		TaskType:          intake.TaskType,
		Difficulty:        intake.Difficulty,
		RequiredReasoning: intake.RequiredReasoning,
	})
	if err != nil {
		return nil, err
	}
	lifeContext, lifeContextExplanation, err := s.retrieveLifeOntologyContext(
		request,
		frameworkDecision,
		modelDecision,
	)
	if err != nil {
		return nil, fmt.Errorf("retrieve whole-life context: %w", err)
	}

	toolDecision := routeTools(intake, request)
	minimalityDecision := decideMinimality(request, intake)
	risk := assessRisk(intake, request)
	risk = applyFrameworkRisk(risk, frameworkDecision, intake, request)
	risk = applyDomainPackRisk(risk, domainPackDecision)
	selectedAgentTeams, agentTeamExplanation, err := s.selectTaskAgentTeams(request, risk)
	if err != nil {
		return nil, fmt.Errorf("select advisory agent-team context: %w", err)
	}
	steps := buildTaskSteps(intake, toolDecision, risk, minimalityDecision)
	createdAt := time.Now().UTC()
	calendarCapacity, err := s.calendarCapacityForTask(request.OwnerIdentity, createdAt, request.Deadline)
	if err != nil {
		return nil, fmt.Errorf("load calendar-backed capacity: %w", err)
	}
	resourcePlannerService := s.resourcePlanner
	if resourcePlannerService == nil {
		resourcePlannerService = defaultResourcePlanner()
	}
	paidAllowed, paidBudget, paidUsed := taskResourceBudget(s.llmService)
	resourceDecision, err := resourcePlannerService.PlanResources(ResourcePlanningRequest{
		OwnerIdentity:  request.OwnerIdentity,
		WorkspaceID:    firstNonEmpty(request.ProjectKey, request.PursuitID),
		PlanID:         planID,
		CreatedAt:      createdAt,
		Deadline:       request.Deadline,
		Difficulty:     intake.Difficulty,
		Steps:          steps,
		Risk:           risk,
		ModelDecision:  modelDecision,
		SelectedTools:  toolDecision.SelectedTools,
		Capacity:       request.Capacity,
		CalendarBusy:   calendarCapacity.BusyIntervals,
		PaidAllowed:    paidAllowed,
		PaidBudgetEUR:  paidBudget,
		PaidBudgetUsed: paidUsed,
		AutomatedExecution: boundedAutomatedExecution(
			intake,
			risk,
			request,
		),
	})
	if err != nil {
		return nil, fmt.Errorf("plan resource and time feasibility: %w", err)
	}
	risk = applyResourcePlanningRisk(risk, resourceDecision)
	steps = buildTaskSteps(intake, toolDecision, risk, minimalityDecision)
	validationPlan := buildValidationPlan(intake, minimalityDecision)
	validationPlan = applyFrameworkValidation(validationPlan, frameworkDecision)
	validationPlan = applyDomainPackValidation(validationPlan, domainPackDecision)
	memoryProposals := proposeMemoryUpdates(request, intake)
	plan := &CompletionPlan{
		ID:             planID,
		OperationID:    strings.TrimSpace(request.operationID),
		IdempotencyKey: strings.TrimSpace(request.IdempotencyKey),
		OwnerIdentity:  strings.TrimSpace(request.OwnerIdentity),
		ReviewItemID:   strings.TrimSpace(request.reviewItemID),
		PursuitID:      strings.TrimSpace(request.PursuitID),
		CreatedAt:      createdAt,
		Request:        request.Request,
		ProjectKey:     request.ProjectKey,
		RealGoal:       inferRealGoal(request, intake),
		Intake:         intake,
		ContextPlan: ContextPlan{
			Strategy: []string{
				"filter by project key when provided",
				"rank by keyword relevance, recency, confidence, and project match",
				"load only top relevant memories",
				"refresh due connected sources when the task likely depends on project, local, or document context",
				"check connected-source extractions before task planning only when the task depends on source context",
				"preserve source references on returned memories",
				"apply the selected framework context requirements without loading unrelated private context",
				"retrieve only source-backed whole-life entities from task-relevant domains",
			},
			UsedContext:              contextResult.UsedContext,
			SourceContext:            sourceContext,
			LifeContext:              lifeContext,
			SourceRefresh:            sourceRefresh,
			SourceRefreshExplanation: sourceRefreshExplanation,
			LifeContextExplanation:   lifeContextExplanation,
			Explanation:              strings.TrimSpace(contextResult.Explanation + " " + sourceRefreshExplanation + " " + sourceExplanation + " " + lifeContextExplanation),
		},
		MinimalityDecision:    minimalityDecision,
		FrameworkDecision:     frameworkDecision,
		DomainPackDecision:    domainPackDecision,
		CalendarCapacity:      calendarCapacity,
		ResourceDecision:      resourceDecision,
		ModelDecision:         modelDecision,
		ToolDecision:          toolDecision,
		Steps:                 steps,
		RiskAssessment:        risk,
		ValidationPlan:        validationPlan,
		ValidationResult:      initialValidationResult(validationPlan),
		ExecutionPlan:         applyAgentTeamExecution(applyResourcePlanningExecution(applyDomainPackExecution(applyFrameworkExecution(buildExecutionPlan(intake), frameworkDecision), domainPackDecision), resourceDecision), selectedAgentTeams),
		RetryPolicy:           buildRetryPolicy(intake),
		MemoryUpdateProposals: memoryProposals,
		LessonsLearned:        proposeLessons(request, intake, toolDecision),
		Events: []TaskEvent{
			event("intake", "request classified and real goal inferred"),
			event("framework-selection", frameworkSelectionSummary(frameworkDecision)),
			event("domain-pack-selection", domainPackSelectionSummary(domainPackDecision)),
			event("calendar-capacity", calendarCapacity.Explanation),
			event("resource-planning", resourcePlanningSummary(resourceDecision)),
			event("source-refresh", sourceRefreshExplanation),
			event("context", contextResult.Explanation),
			event("agent-team-selection", agentTeamExplanation),
			event("minimality", minimalityDecision.SelectedLevel+": "+minimalityDecision.Reason),
			event("routing", modelDecision.Reason),
			event("tool-routing", toolDecision.Reason),
			event("risk", strings.Join(risk.Reasons, "; ")),
		},
		CompletionStatus: "planned",
	}
	if request.HumanApproved {
		plan.Events = append(plan.Events, event("approval", "human approval recorded for the exact reviewed action"))
	}

	_ = runMode
	return plan, nil
}

func (s *service) calendarCapacityForTask(ownerIdentity string, start time.Time, deadline *time.Time) (CalendarCapacityContext, error) {
	start = start.UTC().Truncate(time.Minute)
	end := start.Add(7 * 24 * time.Hour)
	if deadline != nil && deadline.After(start) {
		end = deadline.UTC().Truncate(time.Minute)
	}
	if maximum := start.Add(31 * 24 * time.Hour); end.After(maximum) {
		end = maximum
	}
	result := CalendarCapacityContext{
		Status: "unavailable", WindowStart: start, WindowEnd: end,
		BusyIntervals: []source.CalendarBusyInterval{},
		Explanation:   "No owner-scoped Calendar capacity provider is available; the resource plan relies on explicit Life Ops capacity only.",
	}
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	if ownerIdentity == "" {
		result.Status = "not_applicable"
		result.Explanation = "Calendar capacity was not loaded for an unowned planning preview."
		return result, nil
	}
	provider, ok := s.sourceService.(calendarCapacityProvider)
	if !ok || provider == nil {
		return result, nil
	}
	intervals, err := provider.CalendarBusyIntervalsForOwner(ownerIdentity, start, end)
	if err != nil {
		return CalendarCapacityContext{}, err
	}
	result.Status = "source_backed"
	result.BusyIntervals = append([]source.CalendarBusyInterval(nil), intervals...)
	result.Explanation = fmt.Sprintf("Reserved %d owner-scoped read-only Google Calendar interval(s) between %s and %s; no Calendar write authority was granted.", len(intervals), start.Format(time.RFC3339), end.Format(time.RFC3339))
	return result, nil
}

func (s *service) loadOperatingContext(request IntakeRequest) (IntakeRequest, error) {
	if strings.TrimSpace(request.OwnerIdentity) == "" {
		return request, nil
	}
	now := time.Now().UTC()
	if s.operatingContext != nil && len(request.ObservedNeeds) == 0 {
		needs, err := s.operatingContext.LatestNeeds(request.OwnerIdentity, now)
		if err != nil {
			return request, fmt.Errorf("load needs state: %w", err)
		}
		request.ObservedNeeds = append([]frameworkregistry.NeedStateAssessment(nil), needs...)
	}
	if s.operatingContext != nil && request.Capacity == nil {
		capacity, err := s.operatingContext.LatestCapacity(request.OwnerIdentity, now)
		if err != nil {
			return request, fmt.Errorf("load capacity state: %w", err)
		}
		request.Capacity = capacity
	}
	if s.agentContext != nil && len(request.AvailableAgents) == 0 {
		agents, err := latestAgentsForTask(s.agentContext, request, now)
		if err != nil {
			return request, fmt.Errorf("load available agents: %w", err)
		}
		request.AvailableAgents = append([]frameworkregistry.AgentCard(nil), agents...)
	}
	return request, nil
}

func (s *service) Logs() []CompletionPlan {
	s.mu.Lock()
	defer s.mu.Unlock()

	copied := make([]CompletionPlan, 0, len(s.logs))
	for _, plan := range s.logs {
		copied = append(copied, sanitizeCompletionPlanApprovalData(plan))
	}
	return copied
}

func (s *service) LogsForOwner(ownerIdentity string) []CompletionPlan {
	logs, err := s.LogsForOwnerWithError(ownerIdentity)
	if err != nil {
		return nil
	}
	return logs
}

func (s *service) LogsForOwnerWithError(ownerIdentity string) ([]CompletionPlan, error) {
	return s.LogsForOwnerWithLimit(ownerIdentity, taskStateDefaultLimit)
}

func (s *service) LogsForOwnerWithLimit(ownerIdentity string, limit int) ([]CompletionPlan, error) {
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	if ownerIdentity == "" {
		return nil, fmt.Errorf("owner identity is required")
	}
	logs, err := s.stateRepository.ListCompletionPlans(ownerIdentity, normalizeTaskStateLimit(limit))
	if err != nil {
		return nil, err
	}
	for i := range logs {
		logs[i] = sanitizeCompletionPlanApprovalData(logs[i])
	}
	return logs, nil
}

func (s *service) HistoryForOwnerWithLimit(ownerIdentity string, limit int) ([]CompletionPlanHistoryItem, error) {
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	if ownerIdentity == "" {
		return nil, fmt.Errorf("owner identity is required")
	}
	limit = normalizeTaskStateLimit(limit)
	if compact, ok := s.stateRepository.(TaskHistoryRepository); ok {
		return compact.ListCompletionPlanHistory(ownerIdentity, limit)
	}
	logs, err := s.LogsForOwnerWithLimit(ownerIdentity, limit)
	if err != nil {
		return nil, err
	}
	return completionPlanHistory(logs), nil
}

func (s *service) ReviewQueue() []ReviewQueueItem {
	s.mu.Lock()
	defer s.mu.Unlock()

	copied := make([]ReviewQueueItem, 0, len(s.reviewQueue))
	for _, item := range s.reviewQueue {
		copied = append(copied, sanitizeReviewQueueItem(item))
	}
	return copied
}

func (s *service) ReviewQueueForOwner(ownerIdentity string) []ReviewQueueItem {
	items, err := s.ReviewQueueForOwnerWithError(ownerIdentity)
	if err != nil {
		return nil
	}
	return items
}

func (s *service) ReviewQueueForOwnerWithError(ownerIdentity string) ([]ReviewQueueItem, error) {
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	if ownerIdentity == "" {
		return nil, fmt.Errorf("owner identity is required")
	}
	var items []ReviewQueueItem
	var err error
	if pendingRepository, ok := s.stateRepository.(PendingReviewStateRepository); ok {
		items, err = pendingRepository.ListPendingReviewItems(ownerIdentity, taskStateDefaultLimit)
	} else {
		items, err = s.stateRepository.ListReviewItems(ownerIdentity, taskStateDefaultLimit)
		if err == nil {
			pending := items[:0]
			for _, item := range items {
				if item.Status == "open" || item.Status == "needs_review" {
					pending = append(pending, item)
				}
			}
			items = pending
		}
	}
	if err != nil {
		return nil, err
	}
	for i := range items {
		items[i] = sanitizeReviewQueueItem(items[i])
	}
	return items, nil
}

func (s *service) ResolveReviewItem(id string, decision ApprovalDecision) (*ReviewResolutionResult, error) {
	return s.resolveReviewItemForOwner("", id, decision)
}

func (s *service) ResolveReviewItemForOwner(ownerIdentity, id string, decision ApprovalDecision) (*ReviewResolutionResult, error) {
	return s.resolveReviewItemForOwner(ownerIdentity, id, decision)
}

func (s *service) resolveReviewItemForOwner(ownerIdentity, id string, decision ApprovalDecision) (*ReviewResolutionResult, error) {
	ownerIdentity, item, err := s.reviewItemForResolution(ownerIdentity, id)
	if err != nil {
		return nil, err
	}
	if item.Status != "open" && item.Status != "needs_review" {
		return nil, ErrTaskReviewAlreadyResolved
	}
	if decision.Approved && strings.HasPrefix(item.TaskID, "operation:") &&
		strings.TrimSpace(decision.Confirmation) != TaskOperationRetryConfirmation {
		return nil, ErrTaskOperationRetryConfirmation
	}
	now := time.Now().UTC()
	decisionName := "rejected"
	if decision.Approved {
		decisionName = "approved"
	}
	persisted, err := s.stateRepository.ResolveReviewItem(ownerIdentity, id, ReviewResolution{
		Decision:   decisionName,
		Note:       sanitizeApprovalNote(decision.Note),
		ResolvedAt: now,
	})
	if err != nil {
		return nil, err
	}
	item = persisted.Item
	s.updateReviewMirror(item)
	if !decision.Approved {
		return &ReviewResolutionResult{
			Item: sanitizeReviewQueueItem(item),
			LearningOutcomeID: s.recordHumanCorrection(
				ownerIdentity,
				item,
				persisted.Decision,
			),
		}, nil
	}

	approvedRequest := persisted.Item.Request
	approvedRequest.ExecuteAllowed = true
	approvedRequest.HumanApproved = true
	approvedRequest.ApprovalNote = persisted.Decision.ResolutionNote
	approvedRequest.ApprovalSourceID = persisted.Decision.ApprovalSourceID
	approvedRequest.reviewItemID = persisted.Item.ID

	plan, err := s.Run(approvedRequest)
	if err != nil {
		outcomeAt := time.Now().UTC()
		updated, outcomeErr := s.stateRepository.MarkReviewOutcome(ownerIdentity, id, ReviewOutcome{
			TaskPlanID: firstNonEmpty(item.TaskID, persisted.Decision.TaskPlanID),
			Status:     "needs_review",
			Reason:     "approved execution ended with an error; inspect the task audit before retrying",
			At:         outcomeAt,
		})
		if outcomeErr == nil {
			s.updateReviewMirror(*updated)
		}
		return nil, err
	}
	outcome := ReviewOutcome{
		TaskPlanID: plan.ID,
		Status:     "needs_review",
		Reason:     firstNonEmpty(reviewReasonFromPlan(plan), "approved task requires another review"),
		At:         time.Now().UTC(),
	}
	if plan.CompletionStatus == "validated" {
		outcome.Status = "completed"
		outcome.Reason = "approved task completed and passed validation"
	}
	updated, err := s.stateRepository.MarkReviewOutcome(ownerIdentity, id, outcome)
	if err != nil {
		return nil, err
	}
	item = *updated
	s.updateReviewMirror(item)
	plan.ReviewQueueItem = &item
	safePlan := sanitizeCompletionPlanApprovalData(*plan)
	return &ReviewResolutionResult{
		Item: sanitizeReviewQueueItem(item),
		Plan: &safePlan,
	}, nil
}

func (s *service) reviewItemForResolution(ownerIdentity, id string) (string, ReviewQueueItem, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return "", ReviewQueueItem{}, ErrTaskStateNotFound
	}
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	if ownerIdentity == "" {
		s.mu.Lock()
		for _, item := range s.reviewQueue {
			if item.ID == id {
				ownerIdentity = taskStateOwnerIdentity(item.Request.OwnerIdentity)
				break
			}
		}
		s.mu.Unlock()
	}
	if ownerIdentity == "" {
		return "", ReviewQueueItem{}, ErrTaskStateNotFound
	}
	item, err := s.stateRepository.FindReviewItem(ownerIdentity, id)
	if err != nil {
		return "", ReviewQueueItem{}, err
	}
	return ownerIdentity, *item, nil
}

func reviewReasonFromPlan(plan *CompletionPlan) string {
	if plan == nil {
		return ""
	}
	if plan.ReviewQueueItem != nil && strings.TrimSpace(plan.ReviewQueueItem.Reason) != "" {
		return plan.ReviewQueueItem.Reason
	}
	if plan.ExecutionResult != nil && strings.TrimSpace(plan.ExecutionResult.BlockedReason) != "" {
		return plan.ExecutionResult.BlockedReason
	}
	if len(plan.ValidationResult.Failures) > 0 {
		return strings.Join(plan.ValidationResult.Failures, "; ")
	}
	return ""
}

func (s *service) verifiedApprovalDecisionForExecution(
	plan *CompletionPlan,
	request IntakeRequest,
) (*automation.TaskApprovalDecisionRequest, error) {
	sourceID := strings.TrimSpace(request.ApprovalSourceID)
	sourceKind, err := validateExecutionApprovalSource(sourceID)
	if err != nil {
		return nil, err
	}
	if sourceKind == "workflow-decision" {
		workflowID, workflowErr := uuid.Parse(strings.TrimSpace(request.WorkflowID))
		if workflowErr != nil || workflowID == uuid.Nil {
			return nil, fmt.Errorf("workflow approval has no valid workflow binding")
		}
		if strings.TrimSpace(request.ApprovalActorIdentity) != strings.TrimSpace(request.OwnerIdentity) ||
			strings.TrimSpace(request.ApprovalActorIdentity) != strings.TrimSpace(plan.OwnerIdentity) {
			return nil, fmt.Errorf("workflow approval actor does not match the execution owner")
		}
		if request.ApprovalApprovedAt == nil {
			return nil, fmt.Errorf("workflow approval time is missing")
		}
		decision := &automation.TaskApprovalDecisionRequest{
			OwnerIdentity:         plan.OwnerIdentity,
			Task:                  plan.RealGoal,
			ProjectKey:            plan.ProjectKey,
			MandateID:             strings.TrimSpace(request.MandateID),
			ApprovalSourceID:      sourceID,
			ApprovalBindingDigest: strings.ToLower(strings.TrimSpace(request.ApprovalBindingDigest)),
			ApprovedAt:            request.ApprovalApprovedAt.UTC(),
		}
		if err := automation.ValidateTaskApprovalDecisionRequest(*decision, time.Now().UTC()); err != nil {
			return nil, fmt.Errorf("workflow approval is not valid for execution: %w", err)
		}
		if strings.TrimSpace(request.AutomationID) != "" && decision.ApprovalBindingDigest == "" {
			return nil, fmt.Errorf("workflow approval has no exact automation action binding")
		}
		return decision, nil
	}

	reviewID := strings.TrimPrefix(sourceID, "task-review:")
	if strings.TrimSpace(request.reviewItemID) == "" || request.reviewItemID != reviewID {
		return nil, fmt.Errorf("task approval source does not match the resolved review item")
	}
	ownerIdentity := taskStateOwnerIdentity(request.OwnerIdentity)
	if ownerIdentity != taskStateOwnerIdentity(plan.OwnerIdentity) {
		return nil, fmt.Errorf("task review owner does not match the execution owner")
	}
	item, err := s.stateRepository.FindReviewItem(ownerIdentity, reviewID)
	if err != nil {
		return nil, fmt.Errorf("task review decision is no longer present in the review store: %w", err)
	}
	approval, err := s.stateRepository.FindApprovedReviewDecision(ownerIdentity, reviewID)
	if err != nil {
		return nil, fmt.Errorf("task review decision is not currently approved: %w", err)
	}
	if item.Status != "approved" || item.Decision != "approved" || item.ResolvedAt == nil {
		return nil, fmt.Errorf("task review decision is not currently approved")
	}
	if item.Request.OwnerIdentity != ownerIdentity ||
		taskStateOwnerIdentity(plan.OwnerIdentity) != ownerIdentity {
		return nil, fmt.Errorf("task review owner does not match the execution owner")
	}
	if approval.ApprovalSourceID != sourceID ||
		approval.ReviewItemID != reviewID ||
		approval.ResolvedBy != ownerIdentity {
		return nil, fmt.Errorf("task review approval provenance does not match the execution request")
	}
	requestDigest, err := ReviewRequestDigest(ownerIdentity, request)
	if err != nil {
		return nil, fmt.Errorf("task review request cannot be verified: %w", err)
	}
	if requestDigest != approval.RequestDigest {
		return nil, fmt.Errorf("task review request no longer matches the approved action")
	}
	if strings.TrimSpace(item.Request.AutomationID) != strings.TrimSpace(request.AutomationID) {
		return nil, fmt.Errorf("task review automation does not match the execution target")
	}
	if strings.TrimSpace(item.Request.ProjectKey) != strings.TrimSpace(request.ProjectKey) ||
		strings.TrimSpace(plan.ProjectKey) != strings.TrimSpace(request.ProjectKey) {
		return nil, fmt.Errorf("task review project does not match the execution project")
	}
	if strings.TrimSpace(item.Request.Request) != strings.TrimSpace(request.Request) ||
		strings.TrimSpace(plan.Request) != strings.TrimSpace(request.Request) {
		return nil, fmt.Errorf("task review request does not match the execution request")
	}
	decision := &automation.TaskApprovalDecisionRequest{
		OwnerIdentity:         plan.OwnerIdentity,
		Task:                  plan.RealGoal,
		ProjectKey:            plan.ProjectKey,
		MandateID:             strings.TrimSpace(request.MandateID),
		ApprovalSourceID:      sourceID,
		ApprovalBindingDigest: approval.RequestDigest,
		ApprovedAt:            approval.ResolvedAt.UTC(),
	}
	if err := automation.ValidateTaskApprovalDecisionRequest(*decision, time.Now().UTC()); err != nil {
		return nil, fmt.Errorf("task review approval is not valid for execution: %w", err)
	}
	return decision, nil
}

func firstApprovalBindingDigest(
	decision *automation.TaskApprovalDecisionRequest,
) string {
	if decision == nil {
		return ""
	}
	return strings.TrimSpace(decision.ApprovalBindingDigest)
}

func (s *service) addLog(plan CompletionPlan) error {
	plan = sanitizeCompletionPlanApprovalData(plan)
	if err := s.stateRepository.AppendCompletionPlan(taskStateOwnerIdentity(plan.OwnerIdentity), plan); err != nil {
		return fmt.Errorf("persist task completion plan: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.logs = append([]CompletionPlan{plan}, s.logs...)
	if len(s.logs) > 50 {
		s.logs = s.logs[:50]
	}
	return nil
}

func (s *service) addReviewItem(item ReviewQueueItem) (ReviewQueueItem, error) {
	item = sanitizeReviewQueueItem(item)
	persisted, err := s.stateRepository.CreateReviewItem(taskStateOwnerIdentity(item.Request.OwnerIdentity), item)
	if err != nil {
		return ReviewQueueItem{}, fmt.Errorf("persist task review item: %w", err)
	}
	s.updateReviewMirror(*persisted)
	return sanitizeReviewQueueItem(*persisted), nil
}

func (s *service) attachReviewItem(plan *CompletionPlan, reason, risk string, request IntakeRequest) error {
	request.ApprovalNote = sanitizeApprovalNote(request.ApprovalNote)
	if request.reviewItemID == "" {
		item := newReviewItem(plan.ID, reason, risk, request)
		persisted, err := s.addReviewItem(item)
		if err != nil {
			return err
		}
		plan.ReviewQueueItem = &persisted
		return nil
	}

	ownerIdentity := taskStateOwnerIdentity(request.OwnerIdentity)
	item, err := s.stateRepository.FindReviewItem(ownerIdentity, request.reviewItemID)
	if err != nil {
		return fmt.Errorf("load approved task review item: %w", err)
	}
	item.TaskID = plan.ID
	item.Request = request
	item.Reason = sanitizeTaskOperationalText(reason, taskStateMaximumReasonRunes)
	item.Priority = "normal"
	if risk == "high" {
		item.Priority = "high"
	}
	item.Status = "needs_review"
	item.ResolvedAt = nil
	plan.ReviewQueueItem = item
	return nil
}

func (s *service) updateReviewMirror(item ReviewQueueItem) {
	item = sanitizeReviewQueueItem(item)
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.reviewQueue {
		if s.reviewQueue[i].ID == item.ID {
			s.reviewQueue[i] = item
			return
		}
	}
	s.reviewQueue = append([]ReviewQueueItem{item}, s.reviewQueue...)
	if len(s.reviewQueue) > 50 {
		s.reviewQueue = s.reviewQueue[:50]
	}
}

func taskStateOwnerIdentity(ownerIdentity string) string {
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	if ownerIdentity == "" {
		return internalTaskStateOwnerIdentity
	}
	return ownerIdentity
}

func (s *service) storeLessons(plan *CompletionPlan) []string {
	stored := []string{}
	if plan.ExecutionResult == nil || !verificationStatusAcceptsMemory(plan.ExecutionResult.VerificationStatus) {
		plan.Events = append(plan.Events, event("memory", "lesson storage skipped because execution was not verified"))
		return stored
	}
	for _, lesson := range plan.LessonsLearned {
		created, err := memory.CreateForOwner(s.memoryService, plan.OwnerIdentity, memory.CreateRequest{
			ProjectKey:  plan.ProjectKey,
			Kind:        lesson.Kind,
			Content:     lesson.Content,
			Summary:     compact(lesson.Content),
			Tags:        lesson.Tags,
			Confidence:  lesson.Confidence,
			SourceLabel: "task-success-engine",
		})
		if err == nil && created != nil {
			stored = append(stored, created.ID.String())
		}
	}
	if len(stored) > 0 {
		plan.Events = append(plan.Events, event("memory", "stored useful lessons for future tasks"))
	}
	return stored
}

func (s *service) executeAllowedSteps(plan *CompletionPlan, request IntakeRequest) *ExecutionResult {
	started := time.Now().UTC()
	executionContext := taskExecutionContext(request)
	result := &ExecutionResult{
		StartedAt:          started,
		Mode:               executionMode(plan, request),
		VerificationStatus: verification.StatusNeedsReview,
		Actions:            []ExecutedAction{},
	}
	if executionContext.Err() != nil {
		return stoppedTaskExecution(result, plan, executionContext.Err(), started)
	}

	if !plan.RiskAssessment.AllowedNow {
		result.CompletedAt = time.Now().UTC()
		result.BlockedReason = "risk gate blocked execution until approval is recorded"
		result.Output = "Execution was blocked before action because approval is required."
		result.Actions = append(result.Actions, executedAction("risk.approval_gate", "blocked", plan.Request, result.BlockedReason, started))
		plan.Events = append(plan.Events, event("execution", result.BlockedReason))
		return result
	}

	evidence := evidenceFromPlan(plan)
	if plan.Intake.NeedsTools || plan.Intake.NeedsLocalExecution {
		toolStarted := time.Now().UTC()
		toolResult := completedToolExecution(plan.ExecutionResult)
		if toolResult != nil {
			result.ToolExecution = toolResult
			result.Actions = append(result.Actions, executedAction(
				"automation.launch",
				"reused",
				toolResult.AutomationID,
				"reused successful runtime evidence without repeating external execution",
				toolStarted,
			))
			plan.Events = append(plan.Events, event("execution", "reused successful controlled-runtime evidence during validation retry"))
		} else {
			if s.toolExecutor == nil {
				return blockExecution(result, "controlled runtime executor is not configured", plan, toolStarted)
			}
			if strings.TrimSpace(request.AutomationID) == "" {
				return blockExecution(result, "task requires controlled runtime execution but no automationId was provided", plan, toolStarted)
			}
			if strings.TrimSpace(plan.OwnerIdentity) == "" {
				return blockExecution(
					result,
					"controlled runtime execution requires a verified owner identity",
					plan,
					toolStarted,
				)
			}
			governance, governanceErr := executionGovernanceEvidence(plan)
			if governanceErr != nil {
				return blockExecution(
					result,
					"task governance evidence could not be bound to execution: "+governanceErr.Error(),
					plan,
					toolStarted,
				)
			}
			approvalSourceID := ""
			var approvalDecision *automation.TaskApprovalDecisionRequest
			if request.HumanApproved {
				approvalSourceID = strings.TrimSpace(request.ApprovalSourceID)
				if approvalSourceID == "" {
					return blockExecution(result, "recorded human approval is missing its trusted review-item source", plan, toolStarted)
				}
				var approvalErr error
				approvalDecision, approvalErr = s.verifiedApprovalDecisionForExecution(plan, request)
				if approvalErr != nil {
					return blockExecution(result, "recorded human approval could not be verified: "+approvalErr.Error(), plan, toolStarted)
				}
			}
			executed, err := s.toolExecutor.Execute(ToolExecutionRequest{
				OwnerIdentity:    plan.OwnerIdentity,
				TaskID:           plan.ID,
				AutomationID:     request.AutomationID,
				Task:             plan.RealGoal,
				OriginalRequest:  request.Request,
				ProjectKey:       plan.ProjectKey,
				MandateID:        strings.TrimSpace(request.MandateID),
				WorkflowID:       request.WorkflowID,
				ApprovalSourceID: approvalSourceID,
				ApprovalBindingDigest: firstApprovalBindingDigest(
					approvalDecision,
				),
				Governance:       governance,
				approvalDecision: approvalDecision,
				executionContext: taskExecutionContext(request),
			})
			if err != nil {
				if executionContext.Err() != nil {
					return stoppedTaskExecution(result, plan, executionContext.Err(), toolStarted)
				}
				return blockExecution(result, "controlled runtime execution failed: "+err.Error(), plan, toolStarted)
			}
			if executed == nil {
				return blockExecution(result, "controlled runtime execution returned no result", plan, toolStarted)
			}
			result.ToolExecution = executed
			result.Actions = append(result.Actions, executedAction("automation.launch", executed.Status, executed.AutomationID, firstNonEmpty(executed.Output, executed.Message), toolStarted))
			plan.Events = append(plan.Events, event("execution", "controlled automation runtime returned status "+executed.Status))
			if executed.Status != "completed" {
				reason := firstNonEmpty(executed.Message, "controlled runtime did not complete successfully")
				return blockExecution(result, reason, plan, toolStarted)
			}
		}
		evidence = append(evidence, toolExecutionEvidence(result.ToolExecution))
	}
	result.EvidenceCount = len(evidence)
	result.Actions = append(result.Actions,
		executedAction("memory.retrieve", "completed", request.Request, countLabel(len(plan.ContextPlan.UsedContext), "memory item"), started),
		executedAction("source.search", "completed", request.Request, countLabel(len(plan.ContextPlan.SourceContext), "source extraction"), started),
		executedAction("life-ontology.retrieve", "completed", request.Request, countLabel(len(plan.ContextPlan.LifeContext), "whole-life record"), started),
	)
	if deterministicReadOnlyRuntimeCompleted(result.ToolExecution) {
		evidenceURI := "automation-launch://" + result.ToolExecution.LaunchEventID
		claimText := "The controlled read-only runtime completed successfully: " + result.ToolExecution.Message
		result.Output = claimText + ". Evidence: " + evidenceURI
		result.VerificationStatus = verification.StatusTestPassed
		result.Claims = []models.VerificationClaim{{
			ID:                 uuid.New(),
			ClaimText:          claimText,
			Status:             verification.StatusTestPassed,
			SourceRefs:         evidenceURI,
			SupportExplanation: "the immutable controlled-runtime launch record and deterministic HTTP status satisfy the bounded probe",
			Confidence:         1,
		}}
		result.UnsupportedClaims = 0
		result.CompletedAt = time.Now().UTC()
		result.Actions = append(result.Actions, executedAction(
			"verification.deterministic_runtime",
			"completed",
			request.Request,
			"read-only runtime postconditions passed with immutable launch evidence",
			time.Now().UTC(),
		))
		plan.Events = append(plan.Events, event(
			"verification",
			"read-only controlled-runtime result passed deterministic postcondition verification",
		))
		return result
	}
	draft := ""
	generateStarted := time.Now().UTC()
	if s.llmService != nil {
		context := generationContext(plan)
		if result.ToolExecution != nil {
			context = append(context, toolExecutionSnippet(result.ToolExecution))
		}
		effectContext := &llm.EffectContext{
			OwnerIdentity: plan.OwnerIdentity,
			ActorIdentity: "hai:task-engine",
			ActorKind:     "system",
			TaskID:        plan.ID,
			ProjectKey:    plan.ProjectKey,
		}
		if request.HumanApproved {
			approvalDecision, approvalErr := s.verifiedApprovalDecisionForExecution(
				plan,
				request,
			)
			if approvalErr != nil {
				plan.Events = append(
					plan.Events,
					event(
						"approval",
						"model effect approval could not be verified; only autonomous-safe local execution remains eligible",
					),
				)
			} else if approvalDecision != nil {
				effectContext.ApprovalSourceID = approvalDecision.ApprovalSourceID
				effectContext.ApprovalBindingDigest = approvalDecision.ApprovalBindingDigest
			}
		}
		generation, err := s.llmService.GenerateContext(taskExecutionContext(request), llm.GenerateRequest{
			Task:         plan.RealGoal,
			SystemPrompt: "Produce a concise draft answer using only the provided context. Do not invent facts; unsupported details will be rejected by verification." + minimalitySystemContract(plan.MinimalityDecision),
			Context:      context,
			RouteRequest: &llm.RouteRequest{
				Task:              plan.Request,
				TaskType:          plan.Intake.TaskType,
				Difficulty:        plan.Intake.Difficulty,
				RequiredReasoning: plan.Intake.RequiredReasoning,
			},
			RouteDecision: &plan.ModelDecision,
			EffectContext: effectContext,
			Temperature:   0.1,
			MaxTokens:     900,
			OperationID:   plan.ID + ":attempt:" + strconv.Itoa(maxInt(plan.RetryPolicy.CurrentAttempt+1, 1)),
			FallbackDepth: plan.RetryPolicy.CurrentAttempt,
		})
		if err == nil && generation != nil {
			result.LLMGeneration = generation
			if generation.Status == "completed" {
				draft = generation.Output
			}
			result.Actions = append(result.Actions, executedAction("llm.generate", generation.Status, plan.ModelDecision.SelectedModelID, generation.Reason, generateStarted))
			plan.Events = append(plan.Events, event("llm", "model generation "+generation.Status+": "+generation.Reason))
		} else if err != nil {
			result.Actions = append(result.Actions, executedAction("llm.generate", "failed", plan.ModelDecision.SelectedModelID, err.Error(), generateStarted))
			plan.Events = append(plan.Events, event("llm", "model generation failed; falling back to source-grounded evidence synthesis"))
		}
		if executionContext.Err() != nil {
			return stoppedTaskExecution(result, plan, executionContext.Err(), generateStarted)
		}
	}

	verifyStarted := time.Now().UTC()
	if s.verificationService == nil {
		result.Output, result.Claims, result.VerificationStatus = localGroundedResult(plan, evidence)
		result.UnsupportedClaims = unsupportedClaimCount(result.Claims)
		result.CompletedAt = time.Now().UTC()
		result.Actions = append(result.Actions, executedAction("verification.answer", "completed", request.Request, "used local evidence verifier", verifyStarted))
		plan.Events = append(plan.Events, event("execution", "produced grounded result from retrieved context"))
		return result
	}

	verificationResult, err := verification.AnswerWithContext(s.verificationService, executionContext, verification.AnswerRequest{
		OwnerIdentity:     plan.OwnerIdentity,
		Question:          verificationQuestion(plan),
		ProjectKey:        plan.ProjectKey,
		PursuitID:         plan.PursuitID,
		Mode:              result.Mode,
		DraftAnswer:       draft,
		ExternalEvidence:  evidence,
		IncludeSensitive:  false,
		HumanApproved:     plan.RiskAssessment.ApprovalGranted || !plan.RiskAssessment.ApprovalRequired,
		AllowMemoryUpdate: false,
	})
	if executionContext.Err() != nil {
		return stoppedTaskExecution(result, plan, executionContext.Err(), verifyStarted)
	}
	if err != nil {
		result.CompletedAt = time.Now().UTC()
		result.Output = "Verification engine failed before a grounded answer could be accepted: " + err.Error()
		result.VerificationStatus = verification.StatusNeedsReview
		result.BlockedReason = "verification engine unavailable"
		result.Actions = append(result.Actions, executedAction("verification.answer", "failed", request.Request, err.Error(), verifyStarted))
		plan.Events = append(plan.Events, event("verification", "verification engine failed; task requires review"))
		return result
	}

	result.Output = verificationResult.Run.Answer
	result.VerificationStatus = verificationResult.Run.Status
	result.Claims = verificationResult.Claims
	result.UnsupportedClaims = len(verificationResult.UnsupportedClaims)
	result.CompletedAt = time.Now().UTC()
	if strings.TrimSpace(plan.PursuitID) != "" && strings.TrimSpace(verificationResult.PursuitLinkError) != "" {
		result.VerificationStatus = verification.StatusNeedsReview
		result.BlockedReason = "verification evidence could not be linked to the pursuit"
		result.Actions = append(result.Actions, executedAction("pursuit.verification_link", "blocked", plan.PursuitID, result.BlockedReason, verifyStarted))
		plan.Events = append(plan.Events, event("verification", "verification evidence could not be linked to the pursuit; task requires review"))
		return result
	}
	result.Actions = append(result.Actions, executedAction("verification.answer", "completed", request.Request, verificationResult.Run.Status, verifyStarted))
	plan.Events = append(plan.Events, event("verification", "claims were checked against retrieved evidence before completion"))
	return result
}

func taskExecutionContext(request IntakeRequest) context.Context {
	if request.executionContext != nil {
		return request.executionContext
	}
	return context.Background()
}

func (s *service) recordGenerationValidation(plan *CompletionPlan) {
	if s.llmService == nil || plan == nil || plan.ExecutionResult == nil || plan.ExecutionResult.LLMGeneration == nil {
		return
	}
	generation := plan.ExecutionResult.LLMGeneration
	if strings.TrimSpace(generation.TelemetryID) == "" {
		return
	}
	status := "failed"
	method := "task_success_criteria_v1"
	verificationStatus := strings.TrimSpace(plan.ExecutionResult.VerificationStatus)
	if generation.Status == "completed" && plan.ValidationResult.Passed {
		switch verificationStatus {
		case verification.StatusVerified,
			verification.StatusSourceSupported,
			verification.StatusSchemaValidated,
			verification.StatusTestPassed,
			verification.StatusHumanApproved:
			status = verificationStatus
			method += "+verification_engine"
		default:
			status = verification.StatusSchemaValidated
		}
	} else {
		switch verificationStatus {
		case verification.StatusNeedsReview,
			verification.StatusUncertain,
			verification.StatusConflicting,
			verification.StatusUnsupported:
			status = verification.StatusNeedsReview
		}
	}
	if err := s.llmService.RecordGenerationValidation(generation.TelemetryID, status, method); err != nil {
		plan.Events = append(plan.Events, event("model-calibration", "validation outcome could not be attached to the routed generation: "+err.Error()))
		return
	}
	generation.ValidationStatus = status
	generation.ValidationMethod = method
	generation.CalibrationAudit = "recorded"
	plan.Events = append(plan.Events, event("model-calibration", "routed generation recorded as "+status+" by "+method))
}

func deterministicReadOnlyRuntimeCompleted(result *ToolExecutionResult) bool {
	if result == nil ||
		!strings.EqualFold(strings.TrimSpace(result.LaunchType), "api") ||
		!strings.EqualFold(strings.TrimSpace(result.Status), "completed") ||
		result.ExitCode < http.StatusOK || result.ExitCode >= http.StatusBadRequest ||
		result.ExecutedAt.IsZero() {
		return false
	}
	if id, err := uuid.Parse(strings.TrimSpace(result.AutomationID)); err != nil || id == uuid.Nil {
		return false
	}
	if id, err := uuid.Parse(strings.TrimSpace(result.LaunchEventID)); err != nil || id == uuid.Nil {
		return false
	}
	fields := strings.Fields(strings.TrimSpace(result.Message))
	if len(fields) == 0 || (fields[0] != http.MethodGet && fields[0] != http.MethodHead) {
		return false
	}
	return containsAuditFragment(result.AuditEvents, "unified execution authorization receipt") &&
		containsAuditFragment(result.AuditEvents, "api request executed") &&
		containsAuditFragment(result.AuditEvents, "response captured with bounded output")
}

func containsAuditFragment(events []string, fragment string) bool {
	for _, value := range events {
		if strings.Contains(strings.ToLower(value), strings.ToLower(fragment)) {
			return true
		}
	}
	return false
}

func completedToolExecution(previous *ExecutionResult) *ToolExecutionResult {
	if previous == nil || previous.ToolExecution == nil || previous.ToolExecution.Status != "completed" {
		return nil
	}
	copied := *previous.ToolExecution
	copied.Message = sanitizeTaskOperationalText(copied.Message, 2048)
	copied.Output = sanitizeTaskOperationalText(copied.Output, 8192)
	copied.AuditEvents = sanitizeTaskAuditEvents(previous.ToolExecution.AuditEvents)
	copied.RuntimeRouteTrace = copyAutomationRuntimeRouteTrace(previous.ToolExecution.RuntimeRouteTrace)
	return &copied
}

func blockExecution(result *ExecutionResult, reason string, plan *CompletionPlan, started time.Time) *ExecutionResult {
	reason = sanitizeTaskOperationalText(reason, 2048)
	result.CompletedAt = time.Now().UTC()
	result.Output = "Execution stopped before completion: " + reason
	result.VerificationStatus = verification.StatusNeedsReview
	result.BlockedReason = reason
	if len(result.Actions) == 0 || result.Actions[len(result.Actions)-1].Name != "automation.launch" {
		result.Actions = append(result.Actions, executedAction("automation.launch", "blocked", plan.Request, reason, started))
	}
	plan.Events = append(plan.Events, event("execution", reason))
	return result
}

func stoppedTaskExecution(result *ExecutionResult, plan *CompletionPlan, cause error, started time.Time) *ExecutionResult {
	reason := "task execution stopped because the caller canceled the operation"
	if errors.Is(cause, context.DeadlineExceeded) {
		reason = "task execution stopped because the caller deadline expired"
	}
	result.CompletedAt = time.Now().UTC()
	result.Output = reason
	result.VerificationStatus = verification.StatusNeedsReview
	result.BlockedReason = reason
	result.Actions = append(result.Actions, executedAction("task.cancelled", "stopped", plan.Request, reason, started))
	plan.Events = append(plan.Events, event("execution", reason))
	return result
}

func toolExecutionEvidence(result *ToolExecutionResult) verification.EvidenceInput {
	sourceID := result.AutomationID
	sourceURI := "automation://" + result.AutomationID
	if strings.TrimSpace(result.LaunchEventID) != "" {
		sourceID = result.LaunchEventID
		sourceURI = "automation-launch://" + result.LaunchEventID
	}
	return verification.EvidenceInput{
		SourceType:  "controlled_runtime",
		SourceID:    sourceID,
		SourceURI:   sourceURI,
		SourceLabel: firstNonEmpty(result.RuntimeType, result.LaunchType, "controlled automation runtime"),
		Snippet:     toolExecutionSnippet(result),
		Authority:   "deterministic_runtime",
		Primary:     true,
	}
}

func toolExecutionSnippet(result *ToolExecutionResult) string {
	if result == nil {
		return ""
	}
	snippet := compact(firstNonEmpty(result.Output, result.Message, "controlled runtime completed successfully"))
	if route := runtimeRouteTraceSnippet(result.RuntimeRouteTrace); route != "" {
		return compact(snippet + " | " + route)
	}
	return snippet
}

func runtimeRouteTraceSnippet(trace *models.AutomationRuntimeRouteTrace) string {
	if trace == nil {
		return ""
	}
	parts := []string{}
	if value := strings.TrimSpace(trace.RuntimeID); value != "" {
		parts = append(parts, "runtime="+value)
	}
	if value := strings.TrimSpace(trace.Intent); value != "" {
		parts = append(parts, "intent="+value)
	}
	if value := strings.TrimSpace(trace.ExecutionMode); value != "" {
		parts = append(parts, "mode="+value)
	}
	if value := strings.TrimSpace(trace.RiskLevel); value != "" {
		parts = append(parts, "risk="+value)
	}
	if value := compactRuntimeRouteTraceList("skills", trace.RecommendedSkills, 3); value != "" {
		parts = append(parts, value)
	}
	if value := compactRuntimeRouteTraceList("providers", trace.VisibleProviders, 2); value != "" {
		parts = append(parts, value)
	}
	if value := compactRuntimeRouteTraceList("tools", trace.VisibleTools, 2); value != "" {
		parts = append(parts, value)
	}
	if value := compactRuntimeRouteTraceList("maps", trace.RelevantMaps, 2); value != "" {
		parts = append(parts, value)
	}
	if value := compactRuntimeRouteTraceList("blocked", trace.BlockedSurfaces, 3); value != "" {
		parts = append(parts, value)
	}
	if len(parts) == 0 {
		return ""
	}
	return "route: " + strings.Join(parts, "; ")
}

func compactRuntimeRouteTraceList(label string, values []string, limit int) string {
	cleaned := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			cleaned = append(cleaned, trimmed)
		}
	}
	if len(cleaned) == 0 {
		return ""
	}
	if limit <= 0 || limit > len(cleaned) {
		limit = len(cleaned)
	}
	summary := strings.Join(cleaned[:limit], ", ")
	if len(cleaned) > limit {
		summary += fmt.Sprintf(" +%d", len(cleaned)-limit)
	}
	return label + "=" + summary
}

func copyAutomationRuntimeRouteTrace(trace *models.AutomationRuntimeRouteTrace) *models.AutomationRuntimeRouteTrace {
	if trace == nil {
		return nil
	}
	return &models.AutomationRuntimeRouteTrace{
		RuntimeID:           trace.RuntimeID,
		Intent:              trace.Intent,
		ExecutionMode:       trace.ExecutionMode,
		RiskLevel:           trace.RiskLevel,
		RecommendedSkills:   append([]string{}, trace.RecommendedSkills...),
		VisibleProviders:    append([]string{}, trace.VisibleProviders...),
		VisibleTools:        append([]string{}, trace.VisibleTools...),
		RelevantMaps:        append([]string{}, trace.RelevantMaps...),
		BlockedSurfaces:     append([]string{}, trace.BlockedSurfaces...),
		RequiredControls:    append([]string{}, trace.RequiredControls...),
		ValidationChecklist: append([]string{}, trace.ValidationChecklist...),
	}
}

func executionMode(plan *CompletionPlan, request IntakeRequest) string {
	if plan.Intake.NeedsApproval || plan.Intake.NeedsTools || request.ExecuteAllowed {
		return verification.ModeAction
	}
	return verification.ModeGrounded
}

func evidenceFromPlan(plan *CompletionPlan) []verification.EvidenceInput {
	evidence := []verification.EvidenceInput{}
	for _, ranked := range plan.ContextPlan.UsedContext {
		mem := ranked.Memory
		snippet := firstNonEmpty(mem.Summary, mem.Content)
		if strings.TrimSpace(snippet) == "" {
			continue
		}
		evidence = append(evidence, verification.EvidenceInput{
			SourceType:  "memory",
			SourceID:    mem.ID.String(),
			SourceURI:   mem.SourceURI,
			SourceLabel: firstNonEmpty(mem.SourceLabel, mem.Kind, "context memory"),
			Snippet:     snippet,
			Authority:   "local_memory",
			Primary:     true,
		})
	}
	for _, ranked := range plan.ContextPlan.SourceContext {
		extraction := ranked.Extraction
		snippet := firstNonEmpty(extraction.Summary, extraction.Text)
		if strings.TrimSpace(snippet) == "" {
			continue
		}
		evidence = append(evidence, verification.EvidenceInput{
			SourceType:  "connected_source",
			SourceID:    extraction.ID.String(),
			SourceURI:   extraction.SourceURI,
			SourceLabel: firstNonEmpty(extraction.SourceLabel, extraction.ContentType, "connected source"),
			Snippet:     snippet,
			Authority:   "connected_account",
			Primary:     true,
		})
	}
	for _, suggestion := range plan.ContextPlan.LifeContext {
		entity := suggestion.Entity
		snippet := firstNonEmpty(entity.Summary, entity.Name)
		if strings.TrimSpace(snippet) == "" {
			continue
		}
		for _, provenance := range entity.Provenance {
			evidence = append(evidence, verification.EvidenceInput{
				SourceType:  "life_ontology",
				SourceID:    firstNonEmpty(provenance.ReferenceID, entity.ID),
				SourceURI:   provenance.URI,
				SourceLabel: firstNonEmpty(provenance.Authority, string(entity.Type), "whole-life context"),
				Snippet:     snippet,
				Authority:   firstNonEmpty(provenance.Authority, "owner_scoped_context"),
				Primary:     true,
			})
		}
	}
	return evidence
}

func generationContext(plan *CompletionPlan) []string {
	context := []string{}
	for _, ranked := range plan.ContextPlan.UsedContext {
		snippet := firstNonEmpty(ranked.Memory.Summary, ranked.Memory.Content)
		if strings.TrimSpace(snippet) != "" {
			context = append(context, compact(snippet))
		}
	}
	for _, ranked := range plan.ContextPlan.SourceContext {
		snippet := firstNonEmpty(ranked.Extraction.Summary, ranked.Extraction.Text)
		if strings.TrimSpace(snippet) != "" {
			context = append(context, compact(snippet))
		}
	}
	for _, suggestion := range plan.ContextPlan.LifeContext {
		entity := suggestion.Entity
		snippet := firstNonEmpty(entity.Summary, entity.Name)
		if strings.TrimSpace(snippet) != "" {
			context = append(context, compact(snippet))
		}
	}
	return context
}

func localGroundedResult(plan *CompletionPlan, evidence []verification.EvidenceInput) (string, []models.VerificationClaim, string) {
	if len(evidence) == 0 {
		return "No grounded answer can be produced because no supporting context or source evidence was retrieved.", []models.VerificationClaim{
			{
				ID:                 uuid.New(),
				ClaimText:          "No supporting context or source evidence was retrieved.",
				Status:             verification.StatusNeedsReview,
				SupportExplanation: "task output requires evidence before it can be accepted",
				Confidence:         0.1,
				NeedsReview:        true,
			},
		}, verification.StatusNeedsReview
	}

	lines := []string{}
	claims := []models.VerificationClaim{}
	for _, item := range evidence {
		lines = append(lines, compact(item.Snippet))
		claims = append(claims, models.VerificationClaim{
			ID:                 uuid.New(),
			ClaimText:          compact(item.Snippet),
			Status:             verification.StatusSourceSupported,
			SourceRefs:         firstNonEmpty(item.SourceURI, item.SourceID, item.SourceLabel),
			SupportExplanation: "claim is directly derived from retrieved task context",
			Confidence:         0.7,
		})
		if len(lines) >= 5 {
			break
		}
	}
	return strings.Join(lines, ". "), claims, verification.StatusSourceSupported
}

func executedAction(name, status, input, output string, started time.Time) ExecutedAction {
	return ExecutedAction{
		Name:      name,
		Status:    status,
		Input:     compact(input),
		Output:    compact(output),
		StartedAt: started,
		EndedAt:   time.Now().UTC(),
	}
}

func countLabel(count int, label string) string {
	if count == 1 {
		return "1 " + label
	}
	return strconv.Itoa(count) + " " + label + "s"
}

func (s *service) refreshSourcesForTask(request IntakeRequest, intake IntakeAnalysis) (*source.ScheduledSyncRun, string, error) {
	if s.sourceService == nil {
		return nil, "Connected-source refresh is not configured.", nil
	}
	if !shouldRefreshSourcesForTask(request, intake) {
		return nil, "Connected-source refresh skipped because the task does not appear to need source-backed context.", nil
	}
	ownerIdentity := strings.TrimSpace(request.OwnerIdentity)
	if ownerIdentity == "" {
		return nil, "Connected-source refresh skipped for an unowned planning preview.", nil
	}
	ctx := taskExecutionContext(request)
	result, err := source.RunDueScheduledSyncsForOwnerWithContext(s.sourceService, ctx, time.Now().UTC(), ownerIdentity)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
			return nil, "", firstTaskContextError(ctx, err)
		}
		return nil, "Owner-scoped connected-source refresh failed before context retrieval: " + err.Error(), nil
	}
	return result, fmt.Sprintf("Owner-scoped connected-source preflight checked %d sources; %d due, %d completed, %d failed, %d skipped.", result.Checked, result.Due, result.Completed, result.Failed, result.Skipped), nil
}

func shouldRefreshSourcesForTask(request IntakeRequest, intake IntakeAnalysis) bool {
	return shouldUseConnectedSourcesForTask(request, intake)
}

func shouldUseConnectedSourcesForTask(request IntakeRequest, intake IntakeAnalysis) bool {
	text := strings.ToLower(request.Request + " " + request.ProjectKey)
	if intake.NeedsDocuments || intake.NeedsLocalExecution {
		return true
	}
	return containsAny(text, "source", "context", "project", "file", "folder", "document", "github", "email", "calendar", "trello", "board", "repo")
}

func unsupportedClaimCount(claims []models.VerificationClaim) int {
	count := 0
	for _, claim := range claims {
		if claim.NeedsReview || !verificationStatusAcceptsCompletion(claim.Status) {
			count++
		}
	}
	return count
}

func (s *service) retrieveSourceContext(ctx context.Context, request IntakeRequest, intake IntakeAnalysis) ([]source.RankedExtraction, string, error) {
	if s.sourceService == nil {
		return []source.RankedExtraction{}, "Connected-source retrieval is not configured.", nil
	}
	if !shouldUseConnectedSourcesForTask(request, intake) {
		return []source.RankedExtraction{}, "Connected-source retrieval skipped because the task does not depend on connected-source context.", nil
	}
	result, err := source.SearchWithContext(s.sourceService, ctx, source.SearchRequest{
		OwnerIdentity:        request.OwnerIdentity,
		Query:                request.Request,
		ProjectKey:           request.ProjectKey,
		Limit:                6,
		ExcludeConnectorKeys: source.ManualPlanningContextOnlyConnectorKeys(),
	})
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || (ctx != nil && ctx.Err() != nil) {
			return nil, "", firstTaskContextError(ctx, err)
		}
		return []source.RankedExtraction{}, "Connected-source retrieval failed or has no available index.", nil
	}
	return result.UsedContext, result.Explanation, nil
}

func firstTaskContextError(ctx context.Context, fallback error) error {
	if ctx != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	return fallback
}

func analyzeIntake(request IntakeRequest) IntakeAnalysis {
	text := strings.ToLower(request.Request)
	taskType := "general"
	difficulty := 2
	reasoning := "medium"
	risk := "low"
	reasons := []string{"default completion-first intake"}

	configuredAutomation := strings.TrimSpace(request.AutomationID) != ""
	needsTools := configuredAutomation || requiresControlledExecution(text)
	needsDocs := containsAny(text, "document", "pdf", "spreadsheet", "slides", "docx")
	needsWeb := containsAny(text, "latest", "current", "today", "web", "browse", "search")
	needsLocal := requiresControlledExecution(text) && containsWordOrPhrase(text, "local", "file", "files", "repo", "repository", "docker", "windows", "code", "build", "test", "tests", "script", "command", "commit", "push")

	if containsWordOrPhrase(
		text,
		"code", "coding", "bug", "api", "compile", "build", "test", "tests",
		"implement", "implementation", "refactor", "function", "package", "dependency",
		"library", "json", "parser", "endpoint", "repository", "golang", "go",
	) {
		taskType = "coding"
		difficulty = maxInt(difficulty, 3)
		reasoning = maxReasoning(reasoning, "medium")
		reasons = append(reasons, "coding/build terms detected")
	}
	if containsWordOrPhrase(
		text,
		"architecture", "blueprint", "multi-agent", "autonomous", "routing",
		"new service", "new module", "new runtime", "new adapter", "new connector",
	) {
		taskType = "architecture"
		difficulty = maxInt(difficulty, 4)
		reasoning = maxReasoning(reasoning, "high")
		reasons = append(reasons, "architecture terms detected")
	}
	if configuredAutomation && readOnlyAutomationIntent(text) {
		taskType = "automation"
		difficulty = 2
		reasoning = "low"
		reasons = append(reasons, "configured read-only automation detected")
	}
	risk = classifyTaskRisk(text, needsTools)
	if risk == "high" {
		difficulty = maxInt(difficulty, 4)
		reasoning = maxReasoning(reasoning, "high")
		reasons = append(reasons, "approval-sensitive terms detected")
	} else if risk == "medium" {
		difficulty = maxInt(difficulty, 3)
		reasons = append(reasons, "state-changing or externally consequential terms detected")
	}

	successCriteria := request.SuccessCriteria
	if len(successCriteria) == 0 {
		successCriteria = inferSuccessCriteria(taskType, needsTools)
	}

	return IntakeAnalysis{
		TaskType:            taskType,
		RiskLevel:           risk,
		Difficulty:          difficulty,
		RequiredReasoning:   reasoning,
		SuccessCriteria:     successCriteria,
		NeedsMemory:         true,
		NeedsTools:          needsTools,
		NeedsDocuments:      needsDocs,
		NeedsWebAccess:      needsWeb,
		NeedsLocalExecution: needsLocal,
		NeedsApproval:       risk != "low",
		Reason:              strings.Join(reasons, "; "),
	}
}

// classifyTaskRisk errs on the side of review for any task that can change
// external state. Read-only analysis and allowlisted build/test work stay low
// risk; sending, publication, legal/government, money, account, and deletion
// actions remain high risk even when the request is otherwise well formed.
func classifyTaskRisk(text string, needsTools bool) string {
	if containsWordOrPhrase(text,
		"delete", "financial", "payment", "pay", "spend", "bank", "legal", "lawyer",
		"government", "municipality", "insurer", "insurance", "account", "credential",
		"secret", "password", "publish", "public posting", "post publicly",
	) || (containsWordOrPhrase(text, "send") && containsWordOrPhrase(text, "email", "message", "reply")) {
		return "high"
	}
	if needsTools && containsWordOrPhrase(text,
		"deploy", "install", "commit", "push", "merge", "apply", "change", "modify",
		"move", "rename", "write", "create", "update", "call api", "invoke api",
	) {
		return "medium"
	}
	return "low"
}

func buildValidationPlan(intake IntakeAnalysis, minimality MinimalityDecision) ValidationPlan {
	steps := []string{
		"check every explicit success criterion",
		"verify required fields are present",
		"confirm context sources used are relevant",
	}
	if intake.TaskType == "coding" {
		steps = append(steps, "run applicable build and test commands")
	}
	if minimality.Applicable {
		steps = append(steps,
			"verify the implementation follows the selected YAGNI ladder rung",
			"reject new dependencies or abstractions without explicit evidence that simpler rungs are insufficient",
		)
	}
	if intake.NeedsWebAccess {
		steps = append(steps, "verify time-sensitive claims against current sources")
	}
	if intake.NeedsApproval {
		steps = append(steps, "pause before high-risk execution until human approval is recorded")
	}
	return ValidationPlan{
		Steps:                         steps,
		SuccessCriteria:               uniqueStrings(intake.SuccessCriteria),
		FrameworkEvidenceRequirements: []string{},
		FrameworkCompletionCriteria:   []string{},
		FrameworkAssuranceCriteria:    []string{},
		FrameworkEvidenceContracts:    []FrameworkEvidenceContract{},
		FailurePolicy:                 "retry with stronger context or model; escalate to human review if validation still fails",
		CompletionGate:                "task is complete only after validation passes against success criteria",
	}
}

func applyFrameworkValidation(plan ValidationPlan, decision *frameworkregistry.SelectionDecision) ValidationPlan {
	if decision == nil {
		return plan
	}
	assuranceCriteria := []string{}
	for _, selected := range decision.Selected {
		assuranceCriteria = append(assuranceCriteria, selected.EvaluationMethod...)
	}
	assuranceCriteria = uniqueStrings(assuranceCriteria)
	assuranceSet := make(map[string]struct{}, len(assuranceCriteria))
	for _, criterion := range assuranceCriteria {
		assuranceSet[strings.TrimSpace(criterion)] = struct{}{}
	}
	taskCriteriaSet := make(map[string]struct{}, len(plan.SuccessCriteria))
	for _, criterion := range plan.SuccessCriteria {
		taskCriteriaSet[strings.TrimSpace(criterion)] = struct{}{}
	}
	taskCompletionCriteria := make([]string, 0, len(decision.CompletionCriteria))
	for _, criterion := range decision.CompletionCriteria {
		criterion = strings.TrimSpace(criterion)
		if criterion == "" {
			continue
		}
		if _, duplicateTaskCriterion := taskCriteriaSet[criterion]; duplicateTaskCriterion {
			continue
		}
		if _, frameworkAssuranceCriterion := assuranceSet[criterion]; frameworkAssuranceCriterion {
			continue
		}
		taskCompletionCriteria = append(taskCompletionCriteria, criterion)
	}
	for _, requirement := range decision.EvidenceRequirements {
		plan.Steps = append(plan.Steps, "framework evidence: "+requirement)
	}
	plan.FrameworkEvidenceContracts = compileFrameworkEvidenceContracts(decision)
	for _, criterion := range taskCompletionCriteria {
		plan.Steps = append(plan.Steps, "framework completion: "+criterion)
	}
	for _, criterion := range assuranceCriteria {
		plan.Steps = append(plan.Steps, "framework assurance (not a per-task completion gate): "+criterion)
	}
	plan.Steps = uniqueStrings(plan.Steps)
	plan.FrameworkEvidenceRequirements = uniqueStrings(append(
		plan.FrameworkEvidenceRequirements,
		decision.EvidenceRequirements...,
	))
	plan.FrameworkCompletionCriteria = uniqueStrings(append(
		plan.FrameworkCompletionCriteria,
		taskCompletionCriteria...,
	))
	plan.FrameworkAssuranceCriteria = uniqueStrings(append(
		plan.FrameworkAssuranceCriteria,
		assuranceCriteria...,
	))
	plan.CompletionGate = "task is complete only after task success criteria, selected framework evidence, and framework completion criteria are verified"
	return plan
}

func buildExecutionPlan(intake IntakeAnalysis) ExecutionPlan {
	approval := []string{}
	if intake.NeedsApproval {
		approval = append(approval, "high-risk action")
	}
	if intake.NeedsLocalExecution {
		approval = append(approval, "destructive local execution")
	}
	return ExecutionPlan{
		PlanningSeparatedFromExecution: true,
		ControlledExecutionMode:        "plan_first_then_execute_with_validation",
		ApprovalRequiredFor:            approval,
		AuditEvents: []string{
			"intake classified",
			"context retrieved",
			"model selected",
			"execution attempted",
			"validation completed",
			"memory update proposed",
		},
	}
}

func applyFrameworkExecution(plan ExecutionPlan, decision *frameworkregistry.SelectionDecision) ExecutionPlan {
	if decision == nil {
		return plan
	}
	if decision.RequiresApproval {
		plan.ApprovalRequiredFor = append(plan.ApprovalRequiredFor, decision.ApprovalReasons...)
	}
	plan.ApprovalRequiredFor = uniqueStrings(plan.ApprovalRequiredFor)
	plan.CapacityConstraints = append([]string(nil), decision.Capacity.Constraints...)
	plan.AgentCards = append([]frameworkregistry.AgentCard(nil), decision.AgentCards...)
	plan.Delegations = append([]frameworkregistry.DelegationContract(nil), decision.Delegations...)
	plan.Communication = decision.Communication
	plan.Coordination = decision.Coordination
	plan.ActionAutonomy = append([]frameworkregistry.ActionAutonomyDecision(nil), decision.ActionAutonomy...)
	plan.StopConditions = append([]string(nil), decision.StopConditions...)
	plan.OutcomeMonitoring = append([]string(nil), decision.OutcomeMonitoring...)
	plan.AuditEvents = uniqueStrings(append(plan.AuditEvents,
		"framework combination selected",
		"framework authority ceiling evaluated",
		"human capacity and needs state evaluated",
		"agent cards and delegation authority evaluated",
		"coordination and typed communication contract evaluated",
		"per-action autonomy and stop conditions evaluated",
		"framework evidence and completion gates evaluated",
	))
	return plan
}

func routeTools(intake IntakeAnalysis, request IntakeRequest) ToolRouteDecision {
	selected := []string{"memory.retrieve", "llm.route", "validator.criteria"}
	skipped := []string{}
	blocked := []string{}
	reasons := []string{"selected tools needed for verified completion"}

	if intake.NeedsTools {
		selected = append(selected, "tool-router")
	}
	if strings.TrimSpace(request.AutomationID) != "" {
		selected = append(selected, "automation.launch")
		reasons = append(reasons, "the caller selected a controlled automation explicitly")
	}
	if intake.NeedsDocuments {
		selected = append(selected, "document-context-reader")
	} else {
		skipped = append(skipped, "document-context-reader: task does not require documents")
	}
	if intake.NeedsWebAccess {
		selected = append(selected, "web-verification")
	} else {
		skipped = append(skipped, "web-verification: task is not time-sensitive")
	}
	if intake.NeedsLocalExecution {
		selected = append(selected, "local-readonly-executor")
		blocked = append(blocked, "destructive-local-executor: approval required")
		reasons = append(reasons, "local execution limited to read-only or explicitly approved steps")
	}
	if intake.NeedsApproval {
		blocked = append(blocked, "public-posting", "email-sending", "financial-actions", "account-changes", "delete-actions")
		reasons = append(reasons, "high-risk tools blocked until human approval")
	}

	catalogRecommendations := braincatalog.Recommend(intake.TaskType, request.Request)
	capabilityRecommendations, capabilityRecommendationErr := braincatalog.RecommendForNeed(request.Request)
	if len(catalogRecommendations) > 0 || (capabilityRecommendationErr == nil && len(capabilityRecommendations.Recommendations) > 0) {
		reasons = append(reasons, "external agent capabilities are recommendations only until a reviewed adapter is configured")
		for _, recommendation := range catalogRecommendations {
			skipped, blocked = applyCatalogBoundary(recommendation.ID, recommendation.Status, skipped, blocked)
		}
		for _, recommendation := range capabilityRecommendations.Recommendations {
			skipped, blocked = applyCatalogBoundary(recommendation.ID, recommendation.Status, skipped, blocked)
		}
	}

	return ToolRouteDecision{
		SelectedTools:             uniqueStrings(selected),
		SkippedTools:              uniqueStrings(skipped),
		BlockedTools:              uniqueStrings(blocked),
		CatalogRecommendations:    catalogRecommendations,
		CapabilityRecommendations: capabilityRecommendations.Recommendations,
		Reason:                    strings.Join(reasons, "; "),
	}
}

func applyCatalogBoundary(id string, status braincatalog.Status, skipped, blocked []string) ([]string, []string) {
	switch status {
	case braincatalog.StatusIntegrated:
		skipped = append(skipped, "agent-catalog."+id+": integrated profile requires local configuration and live health")
	case braincatalog.StatusCandidate:
		skipped = append(skipped, "agent-catalog."+id+": operator-configured adapter required")
	case braincatalog.StatusCompatibility:
		blocked = append(blocked, "agent-catalog."+id+": compatibility bridge and approval required")
	default:
		blocked = append(blocked, "agent-catalog."+id+": "+string(status))
	}
	return skipped, blocked
}

func assessRisk(intake IntakeAnalysis, request IntakeRequest) RiskAssessment {
	reasons := []string{"read-only planning is allowed"}
	needsExplicitExecution := intake.NeedsTools || intake.NeedsLocalExecution
	// Approval is a property of the reviewed run, not only of the intake
	// classifier's first risk estimate. Later framework, domain, or resource
	// planning may add an approval requirement. Preserve the recorded decision
	// so those advisory gates can recognize it; the execution boundary still
	// verifies ApprovalSourceID against the durable review ledger before effects.
	approvalGranted := request.ExecuteAllowed && request.HumanApproved
	gateDecision := autonomygate.Decide(autonomygate.Signals{
		Confidence: 0.9,
		Risk:       intake.RiskLevel,
		Reversible: intake.RiskLevel != "high",
		Approved:   approvalGranted,
	})
	missingParameters := requiredExecutionParameters(intake, request)
	actionResolution := actionresolver.Resolve(actionresolver.Action{
		Description:   request.Request,
		Confidence:    0.9,
		Destructive:   intake.RiskLevel == "high",
		MissingParams: missingParameters,
	})
	if intake.NeedsApproval {
		reasons = append(reasons, "request risk classification requires explicit human approval before execution")
	}
	if approvalGranted {
		reasons = append(reasons, "human approval recorded for this run")
	}
	switch gateDecision {
	case autonomygate.Review:
		reasons = append(reasons, "autonomy gate routed the action to review")
	case autonomygate.Block:
		reasons = append(reasons, "autonomy gate blocked an unapproved irreversible high-risk action")
	}
	if actionResolution == actionresolver.Clarify {
		reasons = append(reasons, "action resolver requires clarification before execution: "+strings.Join(missingParameters, ", "))
	} else if actionResolution == actionresolver.Block {
		reasons = append(reasons, "action resolver blocked an ambiguous destructive action")
	}
	if intake.NeedsLocalExecution {
		reasons = append(reasons, "local execution is constrained to non-destructive steps")
	}
	if request.ExecuteAllowed && !intake.NeedsApproval {
		reasons = append(reasons, "caller allowed low-risk execution")
	}
	if !request.ExecuteAllowed && (intake.NeedsTools || intake.NeedsLocalExecution) {
		reasons = append(reasons, "execution not requested; plan remains non-executing")
	}
	allowedNow := gateDecision == autonomygate.Auto
	if actionResolution != actionresolver.Proceed {
		allowedNow = false
	}
	if needsExplicitExecution && !request.ExecuteAllowed {
		allowedNow = false
	}
	return RiskAssessment{
		Level:            intake.RiskLevel,
		ApprovalRequired: intake.NeedsApproval,
		ApprovalGranted:  approvalGranted,
		ApprovalSourceID: strings.TrimSpace(request.ApprovalSourceID),
		ApprovalActorIdentity: func() string {
			if !approvalGranted {
				return ""
			}
			return strings.TrimSpace(request.OwnerIdentity)
		}(),
		ActionResolution:  string(actionResolution),
		MissingParameters: missingParameters,
		Reasons:           reasons,
		AllowedNow:        allowedNow,
	}
}

func applyFrameworkRisk(
	risk RiskAssessment,
	decision *frameworkregistry.SelectionDecision,
	intake IntakeAnalysis,
	request IntakeRequest,
) RiskAssessment {
	if decision == nil {
		return risk
	}
	requiredAutonomy := requiredFrameworkAutonomy(intake, request)
	risk.FrameworkAutonomyCeiling = decision.MaximumAutonomyLevel
	risk.RequiredFrameworkAutonomy = requiredAutonomy
	risk.Reasons = append(risk.Reasons,
		fmt.Sprintf("chief-of-staff framework authority ceiling is level %d", decision.MaximumAutonomyLevel),
	)
	if decision.RequiresApproval {
		risk.ApprovalRequired = true
		risk.ApprovalGranted = request.ExecuteAllowed && request.HumanApproved
		risk.Reasons = append(risk.Reasons, decision.ApprovalReasons...)
		if !risk.ApprovalGranted {
			risk.AllowedNow = false
			risk.Reasons = append(risk.Reasons, "selected frameworks require approval before execution")
		}
	}
	if decision.MaximumAutonomyLevel < requiredAutonomy {
		risk.AllowedNow = false
		risk.Reasons = append(
			risk.Reasons,
			fmt.Sprintf(
				"selected framework ceiling level %d is below the level %d required for this action; re-plan with a suitable framework rather than treating approval as an authority override",
				decision.MaximumAutonomyLevel,
				requiredAutonomy,
			),
		)
	}
	if request.ExecuteAllowed && !boundedAutomatedExecution(intake, risk, request) &&
		(decision.Capacity.Status == "unavailable" || decision.Capacity.Status == "overloaded") {
		risk.AllowedNow = false
		risk.Reasons = append(
			risk.Reasons,
			"current human capacity is unavailable; execution must be rescheduled or explicitly re-planned without creating new operator commitments",
		)
	}
	if request.ExecuteAllowed {
		for _, delegation := range decision.Delegations {
			if delegation.State != "ready" {
				risk.AllowedNow = false
				risk.Reasons = append(
					risk.Reasons,
					"execution is blocked until every required delegated participant has a fresh verified agent card",
				)
				break
			}
		}
	}
	executionAction := "execute_reversible_low_risk_action"
	if request.HumanApproved || decision.RequiresApproval {
		executionAction = "execute_case_approved_action"
	}
	for _, action := range decision.ActionAutonomy {
		if !request.ExecuteAllowed || action.Action != executionAction {
			continue
		}
		if !action.Allowed {
			risk.AllowedNow = false
			risk.Reasons = append(risk.Reasons, "per-action autonomy contract blocks execution: "+action.Reason)
		}
	}
	risk.Reasons = uniqueStrings(risk.Reasons)
	return risk
}

func boundedAutomatedExecution(
	intake IntakeAnalysis,
	risk RiskAssessment,
	request IntakeRequest,
) bool {
	return request.ExecuteAllowed &&
		strings.TrimSpace(request.AutomationID) != "" &&
		intake.NeedsTools &&
		strings.EqualFold(risk.Level, "low") &&
		!risk.ApprovalRequired
}

func requiredFrameworkAutonomy(intake IntakeAnalysis, request IntakeRequest) int {
	if request.ExecuteAllowed && (intake.NeedsTools || intake.NeedsLocalExecution) {
		if request.HumanApproved {
			// Level 6 permits only the exact case-approved action. Approval can
			// authorize scope but cannot raise the framework authority ceiling.
			return 6
		}
		// Without case-specific approval, only level 8 permits automatic
		// execution, and only for reversible, low-risk, allowlisted actions.
		return 8
	}
	// Planning and simulation use level 4. Draft-only work remains a lower
	// capability inside this ceiling and does not imply permission to execute.
	return 4
}

func frameworkSelectionSummary(decision *frameworkregistry.SelectionDecision) string {
	if decision == nil || len(decision.Selected) == 0 {
		return "no framework selection was available"
	}
	ids := make([]string, 0, len(decision.Selected))
	for _, selected := range decision.Selected {
		ids = append(ids, selected.ID+"@"+selected.Version)
	}
	return fmt.Sprintf(
		"selected %s for domain %s with autonomy ceiling %d",
		strings.Join(ids, ", "),
		decision.LifeDomain,
		decision.MaximumAutonomyLevel,
	)
}

// requiredExecutionParameters checks deterministic execution prerequisites.
// It intentionally runs only when a caller requested execution, so planning
// can still explain a task before Robert chooses a runtime.
func requiredExecutionParameters(intake IntakeAnalysis, request IntakeRequest) []string {
	if !request.ExecuteAllowed || (!intake.NeedsTools && !intake.NeedsLocalExecution) {
		return nil
	}
	if strings.TrimSpace(request.AutomationID) == "" {
		return []string{"controlled automation"}
	}
	return nil
}

func taskReviewReason(risk RiskAssessment) string {
	if len(risk.MissingParameters) > 0 {
		return "missing required execution details: " + strings.Join(risk.MissingParameters, ", ")
	}
	if risk.ActionResolution == string(actionresolver.Block) {
		return "action resolver blocked an ambiguous destructive action"
	}
	if risk.ApprovalRequired && !risk.ApprovalGranted {
		return "approval required before task execution"
	}
	return "action clarification is required before task execution"
}

func buildTaskSteps(intake IntakeAnalysis, tools ToolRouteDecision, risk RiskAssessment, minimality MinimalityDecision) []TaskStep {
	steps := []TaskStep{
		{ID: "understand", Name: "Understand request", Purpose: "identify the user's real goal", Allowed: true, Status: "completed"},
		{ID: "criteria", Name: "Define success criteria", Purpose: "make completion measurable", Allowed: true, Status: "completed"},
		{ID: "framework", Name: "Select operating frameworks", Purpose: "choose the smallest capable and safe decision disciplines", Allowed: true, Status: "completed"},
		{ID: "minimality", Name: "Apply YAGNI gate", Purpose: "select the least complex capable implementation strategy", Allowed: minimality.Necessary, RequiresApproval: !minimality.Necessary, Status: taskStepPlanningStatus(minimality.Necessary)},
		{ID: "context", Name: "Gather context", Purpose: "retrieve only relevant memories and references", Allowed: true, Status: "completed"},
		{ID: "routing", Name: "Choose model and tools", Purpose: "select capable resources before optimizing cost", Allowed: true, Status: "completed"},
		{ID: "plan", Name: "Create plan", Purpose: "sequence safe actions and validation", Allowed: true, Status: "completed"},
		{ID: "risk", Name: "Check risk and approvals", Purpose: "block risky actions before execution", Allowed: true, Status: "completed"},
	}
	blockedHighRisk := len(tools.BlockedTools) > 0 && risk.ApprovalRequired && !risk.ApprovalGranted
	executionAllowed := risk.AllowedNow && !blockedHighRisk
	steps = append(steps,
		TaskStep{ID: "execute", Name: "Execute allowed steps", Purpose: "perform only approved or low-risk actions", Allowed: executionAllowed, RequiresApproval: !executionAllowed, Status: "planned"},
		TaskStep{ID: "verify", Name: "Verify result", Purpose: "validate output before completion", Allowed: true, Status: "planned"},
		TaskStep{ID: "memory", Name: "Update memory", Purpose: "store useful lessons without bloating context", Allowed: true, Status: "planned"},
	)
	return steps
}

func taskStepPlanningStatus(allowed bool) string {
	if allowed {
		return "completed"
	}
	return "blocked"
}

func buildRetryPolicy(intake IntakeAnalysis) RetryPolicy {
	maxAttempts := 2
	if intake.Difficulty >= 4 {
		maxAttempts = 3
	}
	return RetryPolicy{
		MaxAttempts: maxAttempts,
		EscalationPath: []string{
			"retry with stronger context",
			"retry with stronger free/local model",
			"queue human review",
		},
		EscalateWhen: []string{
			"validation fails",
			"required context is missing",
			"model or tool capability is insufficient",
			"approval is required",
		},
		CurrentAttempt: 0,
		RetryAvailable: true,
	}
}

func verificationStatusAcceptsCompletion(status string) bool {
	switch status {
	case verification.StatusVerified, verification.StatusSourceSupported, verification.StatusSchemaValidated, verification.StatusTestPassed, verification.StatusHumanApproved:
		return true
	default:
		return false
	}
}

func verificationStatusAcceptsMemory(status string) bool {
	switch status {
	case verification.StatusVerified, verification.StatusSourceSupported, verification.StatusTestPassed, verification.StatusHumanApproved:
		return true
	default:
		return false
	}
}

func proposeMemoryUpdates(request IntakeRequest, intake IntakeAnalysis) []MemoryUpdateProposal {
	proposals := []MemoryUpdateProposal{}
	if request.ProjectKey != "" {
		proposals = append(proposals, MemoryUpdateProposal{
			Kind:       "project",
			Content:    "Task planned for project " + request.ProjectKey + ": " + compact(request.Request),
			Tags:       []string{"task-plan", intake.TaskType},
			Reason:     "completed task plans can improve future project context",
			Confidence: 0.55,
		})
	}
	return proposals
}

func proposeLessons(request IntakeRequest, intake IntakeAnalysis, tools ToolRouteDecision) []MemoryUpdateProposal {
	lesson := MemoryUpdateProposal{
		Kind:       "procedural",
		Content:    "For " + intake.TaskType + " tasks, define success criteria, retrieve relevant context, route a capable model, use tools " + strings.Join(tools.SelectedTools, ", ") + ", validate before completion, and queue review when blocked.",
		Tags:       []string{"task-success-engine", intake.TaskType},
		Reason:     "successful task handling should improve future workflow selection",
		Confidence: 0.62,
	}
	if request.ProjectKey != "" {
		lesson.Tags = append(lesson.Tags, strings.ToLower(request.ProjectKey))
	}
	return []MemoryUpdateProposal{lesson}
}

func inferRealGoal(request IntakeRequest, intake IntakeAnalysis) string {
	clean := compact(request.Request)
	switch intake.TaskType {
	case "coding":
		return "Deliver a working code change and verify it against the requested behavior: " + clean
	case "architecture":
		return "Define an implementable architecture path with visible validation and safety gates: " + clean
	default:
		return "Complete and verify the requested outcome: " + clean
	}
}

// ExecutionTask returns the exact task identity used by the automation
// executor. Approval systems use it before execution so their action digest
// cannot drift from the task engine's eventual launch request.
func ExecutionTask(request IntakeRequest) string {
	return inferRealGoal(request, analyzeIntake(request))
}

func inferSuccessCriteria(taskType string, needsTools bool) []string {
	criteria := []string{
		"the user request is answered or implemented",
		"the result is validated before being marked complete",
		"the selected context and model choice are explained",
	}
	if taskType == "coding" || needsTools {
		criteria = append(criteria, "relevant checks or tests are run when available")
	}
	return criteria
}

func newReviewItem(taskID, reason, risk string, request IntakeRequest) ReviewQueueItem {
	priority := "normal"
	if risk == "high" {
		priority = "high"
	}
	request.ApprovalNote = sanitizeApprovalNote(request.ApprovalNote)
	return ReviewQueueItem{
		ID:        uuid.New().String(),
		TaskID:    taskID,
		Request:   request,
		Reason:    reason,
		Priority:  priority,
		Status:    "open",
		CreatedAt: time.Now().UTC(),
	}
}

func event(stage, message string) TaskEvent {
	if strings.TrimSpace(message) == "" {
		message = "completed"
	}
	return TaskEvent{
		At:      time.Now().UTC(),
		Stage:   stage,
		Message: sanitizeTaskOperationalText(message, 2048),
	}
}

func sanitizeApprovalNote(value string) string {
	return sanitizeTaskOperationalText(value, 512)
}

func sanitizeTaskOperationalText(value string, limit int) string {
	value = strings.Join(strings.Fields(strings.TrimSpace(safety.RedactSecrets(value))), " ")
	if limit <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	if limit <= 3 {
		return string(runes[:limit])
	}
	return string(runes[:limit-3]) + "..."
}

func sanitizeTaskAuditEvents(events []string) []string {
	if len(events) == 0 {
		return nil
	}
	const maxEvents = 64
	result := make([]string, 0, len(events))
	for _, value := range events {
		value = sanitizeTaskOperationalText(value, 512)
		if value != "" {
			result = append(result, value)
		}
		if len(result) == maxEvents {
			break
		}
	}
	return result
}

func sanitizeReviewQueueItem(item ReviewQueueItem) ReviewQueueItem {
	item.ResolutionNote = sanitizeApprovalNote(item.ResolutionNote)
	item.Request.ApprovalNote = sanitizeApprovalNote(item.Request.ApprovalNote)
	return item
}

func sanitizeCompletionPlanApprovalData(plan CompletionPlan) CompletionPlan {
	if plan.ReviewQueueItem != nil {
		item := sanitizeReviewQueueItem(*plan.ReviewQueueItem)
		plan.ReviewQueueItem = &item
	}
	if len(plan.Events) > 0 {
		events := append([]TaskEvent{}, plan.Events...)
		for i := range events {
			events[i].Message = sanitizeTaskOperationalText(events[i].Message, 2048)
		}
		plan.Events = events
	}
	if plan.ExecutionResult != nil {
		execution := *plan.ExecutionResult
		execution.Output = sanitizeTaskOperationalText(execution.Output, 8192)
		execution.BlockedReason = sanitizeTaskOperationalText(execution.BlockedReason, 2048)
		if execution.ToolExecution != nil {
			tool := *execution.ToolExecution
			tool.Message = sanitizeTaskOperationalText(tool.Message, 2048)
			tool.Output = sanitizeTaskOperationalText(tool.Output, 8192)
			tool.AuditEvents = sanitizeTaskAuditEvents(tool.AuditEvents)
			execution.ToolExecution = &tool
		}
		if len(execution.Actions) > 0 {
			actions := append([]ExecutedAction{}, execution.Actions...)
			for i := range actions {
				actions[i].Input = sanitizeTaskOperationalText(actions[i].Input, 512)
				actions[i].Output = sanitizeTaskOperationalText(actions[i].Output, 2048)
			}
			execution.Actions = actions
		}
		plan.ExecutionResult = &execution
	}
	return plan
}

func containsAny(value string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}

func requiresControlledExecution(value string) bool {
	if containsWordOrPhrase(value, "run", "execute", "deploy", "install", "launch", "invoke") {
		return true
	}
	action := containsWordOrPhrase(value,
		"add", "apply", "build", "call", "change", "commit", "create", "delete", "fix",
		"implement", "merge", "modify", "move", "post", "publish", "push", "rename",
		"send", "start", "update", "write",
	)
	target := containsWordOrPhrase(value,
		"account", "api", "build", "code", "command", "deployment", "docker", "email",
		"file", "files", "message", "post", "posting", "repo", "repository", "request",
		"script", "test", "tests",
	)
	return action && target
}

func readOnlyAutomationIntent(value string) bool {
	if !containsWordOrPhrase(value,
		"check", "health", "health check", "inspect", "probe", "readiness", "status", "verify",
	) {
		return false
	}
	return !containsWordOrPhrase(value,
		"add", "apply", "build", "change", "commit", "create", "delete", "deploy",
		"implement", "install", "merge", "modify", "move", "publish", "push", "refactor",
		"rename", "send", "start", "update", "write",
	)
}

func containsWordOrPhrase(value string, terms ...string) bool {
	normalized := " " + strings.Join(strings.FieldsFunc(strings.ToLower(value), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	}), " ") + " "
	for _, term := range terms {
		normalizedTerm := strings.Join(strings.Fields(strings.ToLower(term)), " ")
		if normalizedTerm != "" && strings.Contains(normalized, " "+normalizedTerm+" ") {
			return true
		}
	}
	return false
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}

var reasoningRank = map[string]int{"low": 1, "medium": 2, "high": 3, "very_high": 4}

func maxReasoning(left, right string) string {
	if reasoningRank[left] >= reasoningRank[right] {
		return left
	}
	return right
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	result := []string{}
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

func compact(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	if len(value) <= 180 {
		return value
	}
	return value[:177] + "..."
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}
