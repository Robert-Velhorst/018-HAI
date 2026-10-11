package workflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
	"unicode"

	"automation-hub-backend/internal/autonomy"
	"automation-hub-backend/internal/memory"
	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/plangraph"
	"automation-hub-backend/internal/safety"

	"github.com/google/uuid"
)

const (
	StateNewInput           = "new_input"
	StateClassified         = "classified"
	StateLinked             = "linked"
	StateChecklistGenerated = "checklist_generated"
	StateWaitingInput       = "waiting_external_input"
	StateNeedsApproval      = "needs_approval"
	StateReady              = "ready"
	StateInProgress         = "in_progress"
	StateCompleted          = "completed"
	StateArchived           = "archived"
	StateBlocked            = "blocked"

	RecoveryNeedsReview         = "needs_review"
	RecoveryRetryConfirmed      = "retry_confirmed"
	RecoveryCompletionConfirmed = "completion_confirmed"
	RecoveryCompletedAfterRetry = "completed_after_retry"

	frameworkSelectionDecisionType = "framework_selection"
	frameworkSelectionEventType    = "workflow.framework_selection"
)

type IntakeRequest struct {
	OwnerIdentity            string                              `json:"-"`
	Input                    string                              `json:"input"`
	ProjectKey               string                              `json:"projectKey,omitempty"`
	AutomationID             string                              `json:"automationId,omitempty"`
	MandateID                string                              `json:"mandateId,omitempty"`
	SourceType               string                              `json:"sourceType,omitempty"`
	SourceID                 string                              `json:"sourceId,omitempty"`
	RawItemID                string                              `json:"rawItemId,omitempty"`
	ExtractionID             string                              `json:"extractionId,omitempty"`
	SourceURI                string                              `json:"sourceUri,omitempty"`
	SourceLabel              string                              `json:"sourceLabel,omitempty"`
	ContentType              string                              `json:"contentType,omitempty"`
	Sender                   string                              `json:"sender,omitempty"`
	ReceivedAt               string                              `json:"receivedAt,omitempty"`
	Trigger                  string                              `json:"trigger,omitempty"`
	Actor                    string                              `json:"actor,omitempty"`
	RequiresReview           bool                                `json:"requiresReview,omitempty"`
	ReviewReason             string                              `json:"reviewReason,omitempty"`
	CoordinationPlan         plangraph.AcceptedRevisionReference `json:"coordinationPlan,omitempty"`
	resolvedCoordinationPlan *plangraph.AcceptedRevisionBinding
}

type TransitionRequest struct {
	TargetState string `json:"targetState"`
	Message     string `json:"message,omitempty"`
	Approved    bool   `json:"approved,omitempty"`
	Actor       string `json:"actor,omitempty"`
}

type ChecklistUpdateRequest struct {
	Status string `json:"status"`
	Note   string `json:"note,omitempty"`
	Actor  string `json:"actor,omitempty"`
}

type ApprovalResolutionRequest struct {
	Approved bool   `json:"approved"`
	Note     string `json:"note,omitempty"`
	Actor    string `json:"actor,omitempty"`
}

type InterruptedExecutionResolutionRequest struct {
	Decision      string `json:"decision"`
	Note          string `json:"note"`
	EvidenceURI   string `json:"evidenceUri,omitempty"`
	EvidenceLabel string `json:"evidenceLabel,omitempty"`
	Actor         string `json:"actor,omitempty"`
}

type RunDueRequest struct {
	Limit int `json:"limit,omitempty"`
}

type ProposalResolutionRequest struct {
	Status         string `json:"status,omitempty"`
	Approved       bool   `json:"approved,omitempty"`
	SelectedOption string `json:"selectedOption,omitempty"`
	Note           string `json:"note,omitempty"`
	Actor          string `json:"actor,omitempty"`
}

type TaskRunRequest struct {
	OwnerIdentity         string                              `json:"-"`
	PursuitID             string                              `json:"pursuitId,omitempty"`
	WorkflowID            string                              `json:"workflowId"`
	Request               string                              `json:"request"`
	ProjectKey            string                              `json:"projectKey,omitempty"`
	AutomationID          string                              `json:"automationId,omitempty"`
	MandateID             string                              `json:"-"`
	RiskLevel             string                              `json:"riskLevel,omitempty"`
	HumanApproved         bool                                `json:"humanApproved"`
	ApprovalNote          string                              `json:"approvalNote,omitempty"`
	ApprovalSourceID      string                              `json:"-"`
	ApprovalBindingDigest string                              `json:"-"`
	ApprovalActorIdentity string                              `json:"-"`
	ApprovalApprovedAt    *time.Time                          `json:"-"`
	Deadline              *time.Time                          `json:"-"`
	CoordinationPlan      plangraph.AcceptedRevisionReference `json:"-"`
}

type FrameworkSelectionProvenance struct {
	SelectionDecisionID       string `json:"selectionDecisionId"`
	TaskPlanID                string `json:"taskPlanId"`
	CatalogVersion            string `json:"catalogVersion"`
	CatalogDigest             string `json:"catalogDigest"`
	SelectorAlgorithmVersion  string `json:"selectorAlgorithmVersion"`
	TaskRiskLevel             string `json:"taskRiskLevel,omitempty"`
	EffectiveRiskCeiling      string `json:"effectiveRiskCeiling,omitempty"`
	MaximumAutonomyLevel      *int   `json:"maximumAutonomyLevel,omitempty"`
	RequiresApproval          *bool  `json:"requiresApproval,omitempty"`
	EffectivePreferenceDigest string `json:"effectivePreferenceDigest"`
	ConstitutionVersion       int    `json:"constitutionVersion"`
	ConstitutionDigest        string `json:"constitutionDigest"`
	ConstitutionSource        string `json:"constitutionSource"`
	OperatingContractDigest   string `json:"operatingContractDigest,omitempty"`
}

func (p FrameworkSelectionProvenance) Validate(taskPlanID string) error {
	required := []struct {
		label string
		value string
	}{
		{label: "selection decision id", value: p.SelectionDecisionID},
		{label: "task plan id", value: p.TaskPlanID},
		{label: "catalog version", value: p.CatalogVersion},
		{label: "selector algorithm version", value: p.SelectorAlgorithmVersion},
		{label: "constitution source", value: p.ConstitutionSource},
	}
	for _, field := range required {
		if strings.TrimSpace(field.value) == "" {
			return fmt.Errorf("%s is required", field.label)
		}
	}
	if _, err := uuid.Parse(strings.TrimSpace(p.SelectionDecisionID)); err != nil {
		return fmt.Errorf("selection decision id must be a UUID: %w", err)
	}
	if expected := strings.TrimSpace(taskPlanID); expected != "" && strings.TrimSpace(p.TaskPlanID) != expected {
		return fmt.Errorf("framework selection task plan %q does not match execution plan %q", p.TaskPlanID, expected)
	}
	digests := []struct {
		label string
		value string
	}{
		{label: "catalog digest", value: p.CatalogDigest},
		{label: "effective preference digest", value: p.EffectivePreferenceDigest},
		{label: "constitution digest", value: p.ConstitutionDigest},
	}
	for _, digest := range digests {
		value := strings.TrimSpace(digest.value)
		if len(value) != sha256.Size*2 {
			return fmt.Errorf("%s must be a SHA-256 digest", digest.label)
		}
		if _, err := hex.DecodeString(value); err != nil {
			return fmt.Errorf("%s must be a SHA-256 digest: %w", digest.label, err)
		}
	}
	if value := strings.TrimSpace(p.OperatingContractDigest); value != "" {
		if len(value) != sha256.Size*2 {
			return fmt.Errorf("operating contract digest must be a SHA-256 digest")
		}
		if _, err := hex.DecodeString(value); err != nil {
			return fmt.Errorf("operating contract digest must be a SHA-256 digest: %w", err)
		}
	}
	if p.ConstitutionVersion < 1 {
		return fmt.Errorf("constitution version must be positive")
	}
	if strings.EqualFold(strings.TrimSpace(p.SelectorAlgorithmVersion), "selector-v5") {
		taskRisk, taskRank, err := frameworkRiskRank(p.TaskRiskLevel)
		if err != nil {
			return fmt.Errorf("selector-v5 task risk level: %w", err)
		}
		ceiling, ceilingRank, err := frameworkRiskRank(p.EffectiveRiskCeiling)
		if err != nil {
			return fmt.Errorf("selector-v5 effective risk ceiling: %w", err)
		}
		if ceilingRank < taskRank {
			return fmt.Errorf("selector-v5 effective risk ceiling %q is below task risk %q", ceiling, taskRisk)
		}
		if p.MaximumAutonomyLevel == nil || p.RequiresApproval == nil {
			return fmt.Errorf("selector-v5 autonomy and approval contracts are required")
		}
		if *p.MaximumAutonomyLevel < 0 || *p.MaximumAutonomyLevel > 10 {
			return fmt.Errorf("selector-v5 maximum autonomy level must be between 0 and 10")
		}
	} else if p.MaximumAutonomyLevel != nil || p.RequiresApproval != nil {
		return fmt.Errorf("legacy framework selection cannot assert a selector-v5 execution contract")
	}
	return nil
}

func frameworkRiskRank(value string) (string, int, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	switch normalized {
	case "low":
		return normalized, 1, nil
	case "medium":
		return normalized, 2, nil
	case "high":
		return normalized, 3, nil
	default:
		return "", 0, fmt.Errorf("must be one of low, medium, or high")
	}
}

type TaskRunResult struct {
	PlanID                 string                              `json:"planId,omitempty"`
	CompletionStatus       string                              `json:"completionStatus"`
	VerificationStatus     string                              `json:"verificationStatus"`
	Output                 string                              `json:"output,omitempty"`
	FailureReason          string                              `json:"failureReason,omitempty"`
	RuntimeEvidenceURI     string                              `json:"runtimeEvidenceUri,omitempty"`
	RuntimeEvidenceLabel   string                              `json:"runtimeEvidenceLabel,omitempty"`
	RuntimeRouteTrace      *models.AutomationRuntimeRouteTrace `json:"runtimeRouteTrace,omitempty"`
	Passed                 bool                                `json:"passed"`
	ReviewRequired         bool                                `json:"reviewRequired"`
	ApprovalRequired       bool                                `json:"approvalRequired"`
	ExternalActionExecuted bool                                `json:"externalActionExecuted"`
	FrameworkSelection     *FrameworkSelectionProvenance       `json:"frameworkSelection,omitempty"`
}

type TaskRunner interface {
	RunWorkflowTask(request TaskRunRequest) (*TaskRunResult, error)
}

type WorkflowApprovalBindingRequest struct {
	OwnerIdentity string
	WorkflowID    string
	AutomationID  string
	MandateID     string
	Request       string
	ProjectKey    string
}

type ApprovalBindingPreparer interface {
	PrepareWorkflowApprovalBinding(request WorkflowApprovalBindingRequest) (string, error)
}

type AutomationSelectionRequest struct {
	OwnerIdentity string
	TaskType      string
	Request       string
	ProjectKey    string
}

type AutomationCandidate struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	RuntimeType string `json:"runtimeType,omitempty"`
	LaunchType  string `json:"launchType,omitempty"`
	Score       int    `json:"score"`
	Reason      string `json:"reason"`
}

type AutomationSelector interface {
	SelectWorkflowAutomations(request AutomationSelectionRequest) ([]AutomationCandidate, error)
}

type WorkflowRunResult struct {
	WorkflowID         uuid.UUID                     `json:"workflowId"`
	Status             string                        `json:"status"`
	State              string                        `json:"state"`
	Attempts           int                           `json:"attempts"`
	VerificationStatus string                        `json:"verificationStatus,omitempty"`
	NextRunAt          *time.Time                    `json:"nextRunAt,omitempty"`
	Message            string                        `json:"message,omitempty"`
	FrameworkSelection *FrameworkSelectionProvenance `json:"frameworkSelection,omitempty"`
}

type WorkflowRunSummary struct {
	Checked   int                 `json:"checked"`
	Completed int                 `json:"completed"`
	Retried   int                 `json:"retried"`
	Blocked   int                 `json:"blocked"`
	Skipped   int                 `json:"skipped"`
	Results   []WorkflowRunResult `json:"results"`
}

type OpenLoopRunResult struct {
	WorkflowID uuid.UUID `json:"workflowId"`
	OpenLoopID uuid.UUID `json:"openLoopId"`
	Status     string    `json:"status"`
	State      string    `json:"state,omitempty"`
	Message    string    `json:"message,omitempty"`
}

type OpenLoopRunSummary struct {
	Checked   int                 `json:"checked"`
	Triggered int                 `json:"triggered"`
	Resolved  int                 `json:"resolved"`
	Skipped   int                 `json:"skipped"`
	Results   []OpenLoopRunResult `json:"results"`
}

type ClaimRecoveryResult struct {
	WorkflowID uuid.UUID `json:"workflowId"`
	OpenLoopID uuid.UUID `json:"openLoopId,omitempty"`
	Type       string    `json:"type"`
	Status     string    `json:"status"`
	Message    string    `json:"message"`
}

type ClaimRecoverySummary struct {
	Checked           int                   `json:"checked"`
	WorkflowsBlocked  int                   `json:"workflowsBlocked"`
	OpenLoopsReopened int                   `json:"openLoopsReopened"`
	Skipped           int                   `json:"skipped"`
	Results           []ClaimRecoveryResult `json:"results"`
}

type WorkflowRecord struct {
	Item                models.WorkflowItem            `json:"item"`
	Checklist           []models.WorkflowChecklistItem `json:"checklist"`
	Intake              []models.WorkflowIntakeRecord  `json:"intake"`
	Matches             []models.WorkflowProjectMatch  `json:"matches"`
	Pursuits            []WorkflowPursuitContext       `json:"pursuits"`
	Evidence            []models.WorkflowEvidenceClaim `json:"evidence"`
	OpenLoops           []models.WorkflowOpenLoop      `json:"openLoops"`
	Proposals           []models.WorkflowProposal      `json:"proposals"`
	QualityGates        []models.WorkflowQualityGate   `json:"qualityGates"`
	Transitions         []models.WorkflowTransition    `json:"transitions"`
	SourceLinks         []models.WorkflowSourceLink    `json:"sourceLinks"`
	Decisions           []models.WorkflowDecision      `json:"decisions"`
	Events              []models.WorkflowEvent         `json:"events"`
	FrameworkSelections []FrameworkSelectionProvenance `json:"frameworkSelections"`
}

type WorkflowPursuitContext struct {
	ID                    uuid.UUID `json:"id"`
	OwnerIdentity         string    `json:"-"`
	Title                 string    `json:"title"`
	Status                string    `json:"status"`
	RiskLevel             string    `json:"riskLevel"`
	PriorityScore         int       `json:"priorityScore"`
	Confidence            float64   `json:"confidence"`
	AutonomyLevel         string    `json:"autonomyLevel"`
	NeedCategory          string    `json:"needCategory,omitempty"`
	WhyItMatters          string    `json:"whyItMatters,omitempty"`
	DesiredOutcome        string    `json:"desiredOutcome,omitempty"`
	CurrentStateSummary   string    `json:"currentStateSummary,omitempty"`
	NextRecommendedAction string    `json:"nextRecommendedAction,omitempty"`
	CompletionDefinition  string    `json:"completionDefinition,omitempty"`
	CompletionState       string    `json:"completionState,omitempty"`
	LinkID                uuid.UUID `json:"linkId"`
	Relationship          string    `json:"relationship"`
	SourceURI             string    `json:"sourceUri,omitempty"`
	SourceLabel           string    `json:"sourceLabel,omitempty"`
	LinkConfidence        float64   `json:"linkConfidence"`
}

type EngineCapability struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Status      string   `json:"status"`
	Implemented []string `json:"implemented"`
	Next        []string `json:"next"`
}

type Overview struct {
	Capabilities []EngineCapability    `json:"capabilities"`
	States       []string              `json:"states"`
	SafetyRules  []string              `json:"safetyRules"`
	Rules        []models.WorkflowRule `json:"rules"`
}

type WorkflowDashboard struct {
	Counts                 map[string]int64          `json:"counts"`
	ApprovalItems          []models.WorkflowItem     `json:"approvalItems"`
	BlockedItems           []models.WorkflowItem     `json:"blockedItems"`
	ReadyItems             []models.WorkflowItem     `json:"readyItems"`
	HighRiskItems          []models.WorkflowItem     `json:"highRiskItems"`
	ItemsWithoutNextAction []models.WorkflowItem     `json:"itemsWithoutNextAction"`
	DueOpenLoops           []models.WorkflowOpenLoop `json:"dueOpenLoops"`
	Rules                  []models.WorkflowRule     `json:"rules"`
}

type Service interface {
	Intake(request IntakeRequest) (*WorkflowRecord, error)
	Items(includeArchived bool) ([]models.WorkflowItem, error)
	ItemsForOwner(ownerIdentity string, includeArchived bool) ([]models.WorkflowItem, error)
	ApprovalItems() ([]models.WorkflowItem, error)
	ApprovalItemsForOwner(ownerIdentity string) ([]models.WorkflowItem, error)
	Dashboard() (*WorkflowDashboard, error)
	DashboardForOwner(ownerIdentity string) (*WorkflowDashboard, error)
	Get(id uuid.UUID) (*WorkflowRecord, error)
	GetForOwner(ownerIdentity string, id uuid.UUID) (*WorkflowRecord, error)
	Transition(id uuid.UUID, request TransitionRequest) (*WorkflowRecord, error)
	ResolveApproval(id uuid.UUID, request ApprovalResolutionRequest) (*WorkflowRecord, error)
	ResolveInterruptedExecution(id uuid.UUID, request InterruptedExecutionResolutionRequest) (*WorkflowRecord, error)
	ResolveProposal(id uuid.UUID, proposalID uuid.UUID, request ProposalResolutionRequest) (*WorkflowRecord, error)
	UpdateChecklistItem(id uuid.UUID, itemID uuid.UUID, request ChecklistUpdateRequest) (*WorkflowRecord, error)
	RetractSource(sourceType, sourceID, reason string) error
	RecoverStaleClaims(request RunDueRequest) (*ClaimRecoverySummary, error)
	RecoverStaleClaimsForOwner(ownerIdentity string, request RunDueRequest) (*ClaimRecoverySummary, error)
	RunDue(request RunDueRequest) (*WorkflowRunSummary, error)
	RunDueForOwner(ownerIdentity string, request RunDueRequest) (*WorkflowRunSummary, error)
	RunOneForOwner(ownerIdentity string, id uuid.UUID) (*WorkflowRunResult, error)
	RunDueOpenLoops(request RunDueRequest) (*OpenLoopRunSummary, error)
	RunDueOpenLoopsForOwner(ownerIdentity string, request RunDueRequest) (*OpenLoopRunSummary, error)
	Overview() Overview
}

// ContextOwnerBatchService is an optional boundary for interactive owner-scoped
// batches. Durable schedulers keep using the background-context Service methods,
// while HTTP and agent-cycle callers can stop before another atomic work unit is
// claimed after their request is cancelled.
type ContextOwnerBatchService interface {
	RecoverStaleClaimsForOwnerContext(context.Context, string, RunDueRequest) (*ClaimRecoverySummary, error)
	RunDueForOwnerContext(context.Context, string, RunDueRequest) (*WorkflowRunSummary, error)
	RunDueOpenLoopsForOwnerContext(context.Context, string, RunDueRequest) (*OpenLoopRunSummary, error)
}

type ContextOwnerSingleRunService interface {
	RunOneForOwnerContext(context.Context, string, uuid.UUID) (*WorkflowRunResult, error)
}

func RecoverStaleClaimsForOwnerWithContext(service interface {
	RecoverStaleClaimsForOwner(string, RunDueRequest) (*ClaimRecoverySummary, error)
}, ctx context.Context, ownerIdentity string, request RunDueRequest) (*ClaimRecoverySummary, error) {
	ctx = normalizeBatchContext(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if cancellable, ok := service.(ContextOwnerBatchService); ok {
		return cancellable.RecoverStaleClaimsForOwnerContext(ctx, ownerIdentity, request)
	}
	return service.RecoverStaleClaimsForOwner(ownerIdentity, request)
}

func RunDueForOwnerWithContext(service interface {
	RunDueForOwner(string, RunDueRequest) (*WorkflowRunSummary, error)
}, ctx context.Context, ownerIdentity string, request RunDueRequest) (*WorkflowRunSummary, error) {
	ctx = normalizeBatchContext(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if cancellable, ok := service.(ContextOwnerBatchService); ok {
		return cancellable.RunDueForOwnerContext(ctx, ownerIdentity, request)
	}
	return service.RunDueForOwner(ownerIdentity, request)
}

func RunDueOpenLoopsForOwnerWithContext(service interface {
	RunDueOpenLoopsForOwner(string, RunDueRequest) (*OpenLoopRunSummary, error)
}, ctx context.Context, ownerIdentity string, request RunDueRequest) (*OpenLoopRunSummary, error) {
	ctx = normalizeBatchContext(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if cancellable, ok := service.(ContextOwnerBatchService); ok {
		return cancellable.RunDueOpenLoopsForOwnerContext(ctx, ownerIdentity, request)
	}
	return service.RunDueOpenLoopsForOwner(ownerIdentity, request)
}

func RunOneForOwnerWithContext(service interface {
	RunOneForOwner(string, uuid.UUID) (*WorkflowRunResult, error)
}, ctx context.Context, ownerIdentity string, id uuid.UUID) (*WorkflowRunResult, error) {
	ctx = normalizeBatchContext(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if cancellable, ok := service.(ContextOwnerSingleRunService); ok {
		return cancellable.RunOneForOwnerContext(ctx, ownerIdentity, id)
	}
	return service.RunOneForOwner(ownerIdentity, id)
}

func normalizeBatchContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

// AuthorizedEffectRecoveryIntake is a narrow internal recovery boundary for
// an effect whose exact authorization receipt has already been consumed. It
// preserves historical plan provenance but does not grant workflow execution.
type AuthorizedEffectRecoveryIntake interface {
	IntakeAuthorizedEffectRecovery(request IntakeRequest) (*WorkflowRecord, error)
}

type service struct {
	repo                  Repository
	taskRunner            TaskRunner
	memoryService         memory.Service
	controlledLearning    ControlledLearningRecorder
	lifeOntologyProjector LifeOntologyProjector
	acceptedPlanResolver  plangraph.AcceptedRevisionResolver
	coordinationProjector CoordinationPlanProjector
	reminderDeliverySink  ReminderDeliverySink
}

func NewService(repo Repository, memoryServices ...memory.Service) Service {
	return &service{repo: repo, memoryService: firstMemoryService(memoryServices...)}
}

func NewServiceWithTaskRunner(repo Repository, taskRunner TaskRunner, memoryServices ...memory.Service) Service {
	return &service{repo: repo, taskRunner: taskRunner, memoryService: firstMemoryService(memoryServices...)}
}

func NewServiceWithMemory(repo Repository, memoryService memory.Service) Service {
	return &service{repo: repo, memoryService: memoryService}
}

func DefaultService() Service {
	return NewService(DefaultRepository(), memory.DefaultService())
}

func firstMemoryService(services ...memory.Service) memory.Service {
	for _, service := range services {
		if service != nil {
			return service
		}
	}
	return nil
}

func (s *service) approvalDecisionRule(item *models.WorkflowItem) (string, error) {
	if item == nil {
		return "", fmt.Errorf("workflow approval requires an item")
	}
	if strings.TrimSpace(item.AutomationID) == "" {
		return "manual approval gate", nil
	}
	preparer, ok := s.taskRunner.(ApprovalBindingPreparer)
	if !ok || preparer == nil {
		return "", fmt.Errorf("workflow task runner cannot prepare an exact automation approval binding")
	}
	binding, err := preparer.PrepareWorkflowApprovalBinding(WorkflowApprovalBindingRequest{
		OwnerIdentity: strings.TrimSpace(item.OwnerIdentity),
		WorkflowID:    item.ID.String(),
		AutomationID:  strings.TrimSpace(item.AutomationID),
		MandateID:     uuidPointerString(item.MandateID),
		Request:       strings.TrimSpace(item.Description),
		ProjectKey:    strings.TrimSpace(item.ProjectKey),
	})
	if err != nil {
		return "", fmt.Errorf("prepare exact automation approval binding: %w", err)
	}
	if !strings.HasPrefix(strings.TrimSpace(binding), "automation-action:") {
		return "", fmt.Errorf("workflow task runner returned an invalid automation approval binding")
	}
	return strings.TrimSpace(binding), nil
}

func (s *service) Intake(request IntakeRequest) (*WorkflowRecord, error) {
	input := strings.TrimSpace(request.Input)
	if input == "" {
		return nil, fmt.Errorf("input is required")
	}
	var err error
	coordinationBinding := request.resolvedCoordinationPlan
	if coordinationBinding == nil {
		coordinationBinding, err = s.resolveAcceptedCoordinationPlan(request.OwnerIdentity, request.CoordinationPlan)
		if err != nil {
			return nil, err
		}
	}
	if coordinationBinding != nil && coordinationBinding.CanExecute {
		return nil, fmt.Errorf("accepted coordination plan violated the advisory-only invariant")
	}
	var mandateID *uuid.UUID
	if rawMandateID := strings.TrimSpace(request.MandateID); rawMandateID != "" {
		parsedMandateID, parseErr := uuid.Parse(rawMandateID)
		if parseErr != nil || parsedMandateID == uuid.Nil {
			return nil, fmt.Errorf("standing mandate id must be a UUID")
		}
		mandateID = &parsedMandateID
	}
	sourceType := strings.TrimSpace(request.SourceType)
	sourceID := strings.TrimSpace(request.SourceID)
	sourceRevision := workflowSourceRevision(request, input)
	var existing *models.WorkflowItem
	var dedupeRule string
	if sourceType != "" && sourceID != "" {
		existing, err = s.repo.FindActiveItemBySourceIdentityForOwner(strings.TrimSpace(request.OwnerIdentity), sourceType, sourceID)
		if err != nil {
			return nil, err
		}
		dedupeRule = "source identity deduplication"
	} else if sourceURI := strings.TrimSpace(request.SourceURI); sourceURI != "" {
		existing, err = s.repo.FindActiveItemBySourceURIForOwner(strings.TrimSpace(request.OwnerIdentity), sourceURI)
		if err != nil {
			return nil, err
		}
		dedupeRule = "source URI deduplication"
	}
	if existing != nil {
		if existing.SourceRevision == sourceRevision {
			s.ensureWorkflowCoordinationDraft(existing, firstNonEmpty(request.Actor, "engine"))
			s.audit(existing.ID, "workflow.intake_deduped", "", existing.CurrentState, "existing workflow reused for unchanged source revision", request.Trigger, dedupeRule, request.SourceURI, firstNonEmpty(request.Actor, "engine"))
			return s.Get(existing.ID)
		}
		if err := s.supersedeSourceWorkflow(existing, request, sourceRevision); err != nil {
			return nil, err
		}
	}
	analysis := analyzeInput(request)
	if request.RequiresReview {
		analysis.requiresApproval = true
		analysis.approvalReason = firstNonEmpty(strings.TrimSpace(request.ReviewReason), "connected-source extraction requires human review")
		analysis.autonomyLevel = "approve_before_execute"
		analysis.initialState = StateNeedsApproval
		analysis.nextAction = "review and confirm the connected-source extraction before execution"
		analysis.confidence = math.Min(analysis.confidence, 0.49)
		if analysis.riskLevel == "low" {
			analysis.riskLevel = "medium"
		}
		analysis.ruleApplied += "; explicit connected-source review gate"
	}
	selectionCandidates := []AutomationCandidate{}
	selectionRequired := false
	automationSelectionReason := ""
	if strings.TrimSpace(request.AutomationID) == "" && workflowNeedsAutomation(input, analysis) {
		if selector, ok := s.taskRunner.(AutomationSelector); ok && selector != nil {
			candidates, selectionErr := selector.SelectWorkflowAutomations(AutomationSelectionRequest{
				OwnerIdentity: strings.TrimSpace(request.OwnerIdentity),
				TaskType:      analysis.taskType,
				Request:       input,
				ProjectKey:    firstNonEmpty(request.ProjectKey, analysis.projectKey),
			})
			switch {
			case selectionErr != nil:
				selectionRequired = true
				automationSelectionReason = "automation selection failed: " + selectionErr.Error()
			case len(candidates) == 1:
				request.AutomationID = candidates[0].ID
				selectionCandidates = candidates
				automationSelectionReason = candidates[0].Reason
			default:
				selectionRequired = true
				selectionCandidates = candidates
				if len(candidates) == 0 {
					automationSelectionReason = "no suitable configured automation was found"
				} else {
					automationSelectionReason = "multiple suitable automations require an operator choice"
				}
			}
		}
	}
	if selectionRequired {
		analysis.requiresApproval = true
		analysis.approvalReason = automationSelectionReason
		analysis.autonomyLevel = "approve_before_execute"
		analysis.initialState = StateNeedsApproval
		analysis.nextAction = "select the exact automation before controlled execution"
		analysis.ruleApplied += "; exact automation selection required before approval"
	}
	projectKey := firstNonEmpty(request.ProjectKey, analysis.projectKey)
	item := &models.WorkflowItem{
		OwnerIdentity:    strings.TrimSpace(request.OwnerIdentity),
		Title:            analysis.title,
		Description:      input,
		ProjectKey:       projectKey,
		AutomationID:     strings.TrimSpace(request.AutomationID),
		MandateID:        mandateID,
		CurrentState:     analysis.initialState,
		TaskType:         analysis.taskType,
		RiskLevel:        analysis.riskLevel,
		PriorityScore:    analysis.priority,
		Confidence:       analysis.confidence,
		AutonomyLevel:    analysis.autonomyLevel,
		RequiresApproval: analysis.requiresApproval,
		ApprovalStatus:   approvalStatus(analysis.requiresApproval),
		ApprovalReason:   analysis.approvalReason,
		BlockedReason:    analysis.blockedReason,
		NextAction:       analysis.nextAction,
		SourceType:       sourceType,
		SourceID:         sourceID,
		SourceURI:        strings.TrimSpace(request.SourceURI),
		SourceLabel:      strings.TrimSpace(request.SourceLabel),
		SourceRevision:   sourceRevision,
		DueAt:            analysis.dueAt,
		MaxRetries:       maxRetriesForAnalysis(analysis),
	}
	applyWorkflowCoordinationBinding(item, coordinationBinding)
	createdNew := true
	var created *models.WorkflowItem
	if sourceType != "" && sourceID != "" {
		if idempotent, ok := s.repo.(idempotentWorkflowItemRepository); ok {
			created, createdNew, err = idempotent.CreateItemIdempotent(item)
		} else {
			created, err = s.repo.CreateItem(item)
		}
	} else {
		created, err = s.repo.CreateItem(item)
	}
	if err != nil {
		return nil, err
	}
	if !createdNew {
		s.audit(created.ID, "workflow.intake_deduped", "", created.CurrentState, "existing workflow reused after concurrent receipt-bound intake", request.Trigger, "source identity unique boundary", request.SourceURI, firstNonEmpty(request.Actor, "engine"))
		return s.Get(created.ID)
	}
	intakeRecord, _ := s.repo.SaveIntakeRecord(&models.WorkflowIntakeRecord{
		WorkflowID:        created.ID,
		SourceType:        strings.TrimSpace(request.SourceType),
		SourceID:          strings.TrimSpace(request.SourceID),
		SourceURI:         strings.TrimSpace(request.SourceURI),
		SourceLabel:       strings.TrimSpace(request.SourceLabel),
		ContentType:       firstNonEmpty(request.ContentType, analysis.taskType),
		Sender:            strings.TrimSpace(request.Sender),
		ReceivedAt:        parseOptionalTime(request.ReceivedAt),
		RawContent:        input,
		NormalizedSummary: compact(input, 420),
		DetectedEntities:  strings.Join(analysis.entities, ","),
		PossibleProject:   projectKey,
		Urgency:           urgencyForPriority(analysis.priority),
	})
	for index, checklist := range checklistForAnalysis(analysis) {
		_, _ = s.repo.CreateChecklistItem(&models.WorkflowChecklistItem{
			WorkflowID:       created.ID,
			Label:            checklist.label,
			Status:           "open",
			Position:         index + 1,
			RequiresApproval: checklist.requiresApproval,
		})
	}
	if analysis.dueAt != nil {
		_, _ = s.repo.CreateChecklistItem(&models.WorkflowChecklistItem{
			WorkflowID: created.ID,
			Label:      "Follow up or check before detected deadline",
			Status:     "open",
			Position:   900,
			DueAt:      analysis.dueAt,
			ReminderAt: reminderBefore(*analysis.dueAt),
		})
	}
	s.ensureWorkflowCoordinationDraft(created, firstNonEmpty(request.Actor, "engine"))
	s.applyMemoryContext(created.ID, input, projectKey, request.OwnerIdentity, firstNonEmpty(request.Actor, "engine"))
	if created.SourceURI != "" || created.SourceLabel != "" {
		s.linkSource(created.ID, created.SourceType, request.SourceID, created.SourceURI, created.SourceLabel, "origin")
		s.decide(created.ID, "source_link", "linked", "source provenance captured for workflow", "source link created at intake", false, firstNonEmpty(request.Actor, "engine"))
	}
	if projectKey != "" {
		_, _ = s.repo.CreateProjectMatch(&models.WorkflowProjectMatch{
			WorkflowID:     created.ID,
			ProjectKey:     projectKey,
			MatchedBy:      strings.Join(analysis.matchReasons, ", "),
			Confidence:     analysis.projectConfidence,
			TrelloCardRef:  analysis.trelloRef,
			DriveFolderRef: analysis.driveRef,
		})
		s.decide(created.ID, "project_match", projectKey, "workflow linked to project context", strings.Join(analysis.matchReasons, ", "), false, "engine")
	}
	for _, claim := range evidenceClaimsForInput(created.ID, input, request) {
		_, _ = s.repo.CreateEvidenceClaim(&claim)
	}
	if loop := openLoopForAnalysis(created.ID, analysis); loop != nil {
		_, _ = s.repo.CreateOpenLoop(loop)
		s.decide(created.ID, "open_loop", "created", loop.WaitingFor, "follow-up/open-loop detection", false, "engine")
	}
	if proposal := proposalForAnalysis(created.ID, analysis); proposal != nil {
		_, _ = s.repo.CreateProposal(proposal)
	}
	if selectionRequired {
		_, _ = s.repo.CreateProposal(automationSelectionProposal(created.ID, selectionCandidates))
	}
	for _, gate := range qualityGatesForAnalysis(created.ID, analysis) {
		_, _ = s.repo.CreateQualityGate(&gate)
	}
	s.recordTransition(created.ID, StateNewInput, created.CurrentState, request.Trigger, firstNonEmpty(request.Actor, "engine"), false, "input classified and workflow state initialized")
	s.decide(created.ID, "classification", analysis.taskType, "input classified as "+analysis.taskType, analysis.ruleApplied, false, "engine")
	s.decide(created.ID, "priority", fmt.Sprintf("%d", analysis.priority), "priority assigned from risk, deadline, and task type", "priority engine", false, "engine")
	if analysis.requiresApproval {
		s.decide(created.ID, "approval_gate", "required", analysis.approvalReason, "approval rule engine", false, "engine")
	} else {
		s.decide(created.ID, "approval_gate", "not_required", "low-risk workflow can enter worker queue", "approval rule engine", false, "engine")
	}
	if strings.TrimSpace(created.AutomationID) != "" {
		s.decide(created.ID, "automation_selection", created.AutomationID, automationSelectionReason, "conservative automation capability match", false, "engine")
	} else if selectionRequired {
		s.decide(created.ID, "automation_selection", "needs_review", automationSelectionReason, "conservative automation capability match", false, "engine")
	}
	if analysis.blockedReason != "" {
		s.decide(created.ID, "missing_info", "blocked", analysis.blockedReason, "missing information detection", false, "engine")
	}
	if analysis.dueAt != nil {
		s.decide(created.ID, "deadline_reminder", "created", "check reminder created from detected deadline", "deadline detection", false, "engine")
	}
	if intakeRecord != nil {
		s.audit(created.ID, "workflow.intake_normalized", "", "", "input normalized from "+firstNonEmpty(request.SourceType, "manual"), request.Trigger, "universal intake engine", request.SourceURI, firstNonEmpty(request.Actor, "engine"))
	}
	s.audit(created.ID, "workflow.intake", "", created.CurrentState, "input classified and workflow state initialized", request.Trigger, analysis.ruleApplied, request.SourceURI, firstNonEmpty(request.Actor, "engine"))
	return s.Get(created.ID)
}

func (s *service) IntakeAuthorizedEffectRecovery(request IntakeRequest) (*WorkflowRecord, error) {
	ownerIdentity := strings.TrimSpace(request.OwnerIdentity)
	sourceID := strings.TrimSpace(request.SourceID)
	if ownerIdentity == "" || sourceID == "" || strings.TrimSpace(request.SourceType) == "" ||
		!request.RequiresReview || request.CoordinationPlan.IsZero() {
		return nil, fmt.Errorf("authorized effect recovery requires owner, receipt source, review gate, and coordination plan")
	}
	if _, err := uuid.Parse(sourceID); err != nil {
		return nil, fmt.Errorf("authorized effect recovery source id must be a receipt UUID")
	}
	expectedURI := "hai://execution-authorization-receipts/" + sourceID
	if strings.TrimSpace(request.SourceURI) != expectedURI {
		return nil, fmt.Errorf("authorized effect recovery source URI does not match its receipt")
	}
	historyResolver, ok := s.acceptedPlanResolver.(plangraph.AcceptedRevisionHistoryResolver)
	if !ok || historyResolver == nil {
		return nil, fmt.Errorf("historical coordination plan validation is unavailable")
	}
	binding, err := historyResolver.ResolveAcceptedRevision(context.Background(), ownerIdentity, request.CoordinationPlan)
	if err != nil {
		return nil, fmt.Errorf("validate historical accepted coordination plan: %w", err)
	}
	if binding == nil || binding.CanExecute {
		return nil, fmt.Errorf("historical coordination plan violated the advisory-only invariant")
	}
	request.resolvedCoordinationPlan = binding
	return s.Intake(request)
}

func (s *service) applyMemoryContext(workflowID uuid.UUID, input, projectKey, ownerIdentity, actor string) {
	if s.memoryService == nil {
		return
	}
	result, err := memory.RetrieveForOwner(s.memoryService, ownerIdentity, memory.RetrieveRequest{
		Query:      input,
		ProjectKey: projectKey,
		Limit:      3,
	})
	if err != nil {
		s.audit(workflowID, "workflow.memory_context_failed", "", "", err.Error(), "memory_retrieval", "context planning layer", "", actor)
		return
	}
	applied := 0
	summaries := []string{}
	for _, ranked := range result.UsedContext {
		mem := ranked.Memory
		if !workflowMemoryUseful(mem) {
			continue
		}
		lesson := workflowMemoryLessonText(mem)
		if lesson == "" {
			continue
		}
		applied++
		_, _ = s.repo.CreateChecklistItem(&models.WorkflowChecklistItem{
			WorkflowID: workflowID,
			Label:      "Apply learned context: " + lesson,
			Status:     "open",
			Position:   800 + applied,
		})
		s.linkSource(
			workflowID,
			"memory",
			mem.ID.String(),
			workflowMemorySourceURI(mem),
			firstNonEmpty(mem.SourceLabel, mem.Summary, "Context memory"),
			"planning_context",
		)
		summaries = append(summaries, lesson)
	}
	if applied == 0 {
		return
	}
	summary := compactWorkflowText(strings.Join(summaries, "; "), 420)
	s.decide(
		workflowID,
		"memory_context",
		"applied",
		fmt.Sprintf("applied %d relevant memory record(s): %s", applied, summary),
		firstNonEmpty(result.Explanation, "context planning layer"),
		false,
		actor,
	)
	s.audit(
		workflowID,
		"workflow.memory_context",
		"",
		"",
		fmt.Sprintf("applied %d relevant memory record(s) to workflow planning", applied),
		"memory_retrieval",
		summary,
		"",
		actor,
	)
}

func (s *service) supersedeSourceWorkflow(item *models.WorkflowItem, request IntakeRequest, sourceRevision string) error {
	if item.CurrentState == StateInProgress || item.WorkerClaimID != "" {
		return fmt.Errorf("source workflow is currently in progress and cannot be superseded until execution review is complete")
	}
	from := item.CurrentState
	item.CurrentState = StateArchived
	item.Archived = true
	item.NextAction = "superseded by revised source content"
	item.NextRunAt = nil
	item.WorkerClaimID = ""
	item.WorkerLeaseUntil = nil
	if _, err := s.repo.UpdateItem(item); err != nil {
		return err
	}
	actor := firstNonEmpty(request.Actor, "engine")
	reason := "source content or review requirements changed; prior workflow and approval scope were superseded"
	s.recordTransition(item.ID, from, StateArchived, "source_revision", actor, false, reason)
	s.decide(item.ID, "source_revision", "superseded", reason, "immutable source workflow revisions", false, actor)
	s.audit(item.ID, "workflow.source_superseded", from, StateArchived, reason, "source_revision", compact(sourceRevision, 16), request.SourceURI, actor)
	return nil
}

func (s *service) Items(includeArchived bool) ([]models.WorkflowItem, error) {
	return s.ItemsForOwner("", includeArchived)
}

func (s *service) ItemsForOwner(ownerIdentity string, includeArchived bool) ([]models.WorkflowItem, error) {
	items, err := s.repo.FindItems(includeArchived)
	if err != nil {
		return nil, err
	}
	return visibleWorkflowItems(ownerIdentity, items), nil
}

func (s *service) ApprovalItems() ([]models.WorkflowItem, error) {
	return s.ApprovalItemsForOwner("")
}

func (s *service) ApprovalItemsForOwner(ownerIdentity string) ([]models.WorkflowItem, error) {
	items, err := s.repo.FindApprovalItems()
	if err != nil {
		return nil, err
	}
	return visibleWorkflowItems(ownerIdentity, items), nil
}

func (s *service) RetractSource(sourceType, sourceID, reason string) error {
	sourceType = strings.TrimSpace(sourceType)
	sourceID = strings.TrimSpace(sourceID)
	if sourceType == "" || sourceID == "" {
		return fmt.Errorf("source type and source id are required")
	}
	item, err := s.repo.FindActiveItemBySourceIdentity(sourceType, sourceID)
	if err != nil || item == nil {
		return err
	}
	reason = firstNonEmpty(strings.TrimSpace(reason), "source record was retracted")
	if item.CurrentState == StateInProgress {
		return fmt.Errorf("source workflow is currently in progress and requires interruption review before retraction")
	}
	if item.CurrentState == StateCompleted || item.CurrentState == StateArchived {
		s.audit(item.ID, "workflow.source_retracted_after_completion", item.CurrentState, item.CurrentState, reason, "source_retraction", "completed workflow retained for audit", item.SourceURI, "source-worker")
		return nil
	}
	from := item.CurrentState
	item.CurrentState = StateBlocked
	item.BlockedReason = reason
	item.NextAction = "review the retracted source record before any further execution"
	item.NextRunAt = nil
	item.WorkerClaimID = ""
	item.WorkerLeaseUntil = nil
	item.VerificationStatus = "needs_review"
	if _, err := s.repo.UpdateItem(item); err != nil {
		return err
	}
	s.recordTransition(item.ID, from, StateBlocked, "source_retraction", "source-worker", false, reason)
	s.decide(item.ID, "source_retraction", "blocked", reason, "source-derived work must stop when its evidence is retracted", false, "source-worker")
	s.audit(item.ID, "workflow.source_retracted", from, StateBlocked, reason, "source_retraction", "source identity retraction", item.SourceURI, "source-worker")
	return nil
}

func (s *service) Dashboard() (*WorkflowDashboard, error) {
	return s.DashboardForOwner("")
}

func (s *service) DashboardForOwner(ownerIdentity string) (*WorkflowDashboard, error) {
	items, err := s.ItemsForOwner(ownerIdentity, false)
	if err != nil {
		return nil, err
	}
	approvalItems, err := s.ApprovalItemsForOwner(ownerIdentity)
	if err != nil {
		return nil, err
	}
	workflowIDs := workflowIDSet(items)
	now := time.Now().UTC()
	openLoops, err := s.repo.FindDashboardOpenLoops(now)
	if err != nil {
		return nil, err
	}
	openLoops = visibleWorkflowOpenLoops(workflowIDs, openLoops)
	expiredWorkflowClaims, err := s.repo.FindExpiredWorkflowClaims(now, 50)
	if err != nil {
		return nil, err
	}
	expiredWorkflowClaims = visibleWorkflowItemsByID(workflowIDs, expiredWorkflowClaims)
	expiredOpenLoopClaims, err := s.repo.FindExpiredOpenLoopClaims(now, 50)
	if err != nil {
		return nil, err
	}
	expiredOpenLoopClaims = visibleWorkflowOpenLoops(workflowIDs, expiredOpenLoopClaims)
	rules, err := s.repo.FindRules()
	if err != nil {
		return nil, err
	}
	if len(rules) == 0 {
		rules = defaultWorkflowRules()
	}
	dashboard := &WorkflowDashboard{
		Counts: map[string]int64{
			"total":                  int64(len(items)),
			"approvals":              int64(len(approvalItems)),
			"blocked":                0,
			"ready":                  0,
			"highRisk":               0,
			"itemsWithoutNextAction": 0,
			"dueOpenLoops":           int64(len(openLoops)),
			"expiredWorkflowClaims":  int64(len(expiredWorkflowClaims)),
			"expiredOpenLoopClaims":  int64(len(expiredOpenLoopClaims)),
			"interruptedReview":      0,
		},
		ApprovalItems:          append([]models.WorkflowItem{}, approvalItems...),
		BlockedItems:           []models.WorkflowItem{},
		ReadyItems:             []models.WorkflowItem{},
		HighRiskItems:          []models.WorkflowItem{},
		ItemsWithoutNextAction: []models.WorkflowItem{},
		DueOpenLoops:           append([]models.WorkflowOpenLoop{}, openLoops...),
		Rules:                  append([]models.WorkflowRule{}, rules...),
	}
	for _, item := range items {
		switch item.CurrentState {
		case StateBlocked:
			dashboard.BlockedItems = append(dashboard.BlockedItems, item)
			dashboard.Counts["blocked"]++
		case StateReady:
			dashboard.ReadyItems = append(dashboard.ReadyItems, item)
			dashboard.Counts["ready"]++
		}
		if item.RiskLevel == "high" {
			dashboard.HighRiskItems = append(dashboard.HighRiskItems, item)
			dashboard.Counts["highRisk"]++
		}
		if item.RecoveryStatus == RecoveryNeedsReview {
			dashboard.Counts["interruptedReview"]++
		}
		if strings.TrimSpace(item.NextAction) == "" && item.CurrentState != StateArchived {
			dashboard.ItemsWithoutNextAction = append(dashboard.ItemsWithoutNextAction, item)
			dashboard.Counts["itemsWithoutNextAction"]++
		}
	}
	SortItems(dashboard.ApprovalItems)
	SortItems(dashboard.BlockedItems)
	SortItems(dashboard.ReadyItems)
	SortItems(dashboard.HighRiskItems)
	SortItems(dashboard.ItemsWithoutNextAction)
	dashboard.ApprovalItems = limitWorkflowItems(dashboard.ApprovalItems, 25)
	dashboard.BlockedItems = limitWorkflowItems(dashboard.BlockedItems, 25)
	dashboard.ReadyItems = limitWorkflowItems(dashboard.ReadyItems, 25)
	dashboard.HighRiskItems = limitWorkflowItems(dashboard.HighRiskItems, 25)
	dashboard.ItemsWithoutNextAction = limitWorkflowItems(dashboard.ItemsWithoutNextAction, 25)
	return dashboard, nil
}

func (s *service) Get(id uuid.UUID) (*WorkflowRecord, error) {
	return s.GetForOwner("", id)
}

func (s *service) GetForOwner(ownerIdentity string, id uuid.UUID) (*WorkflowRecord, error) {
	record, err := s.get(id)
	if err != nil {
		return nil, err
	}
	if !workflowVisibleTo(record.Item, ownerIdentity) {
		return nil, fmt.Errorf("workflow not found")
	}
	record.Pursuits = visibleWorkflowPursuits(ownerIdentity, record.Pursuits)
	return record, nil
}

// AttachBrowserVerification links one completed, owner-authorized local
// browser check as a quality signal. A passing route check proves only that the
// named local page met its configured navigation expectation: it cannot verify
// facts, update memory, execute work, or transition the workflow to complete.
func (s *service) AttachBrowserVerification(ownerIdentity, workflowID, runID, profileID, status, finalPath, pageTitle, summary string) error {
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	if ownerIdentity == "" {
		return fmt.Errorf("owner identity is required")
	}
	workflowID = strings.TrimSpace(workflowID)
	runID = strings.TrimSpace(runID)
	parsedWorkflowID, err := uuid.Parse(workflowID)
	if err != nil {
		return fmt.Errorf("workflow id is invalid")
	}
	if _, err := uuid.Parse(runID); err != nil {
		return fmt.Errorf("browser verification run id is invalid")
	}
	record, err := s.GetForOwner(ownerIdentity, parsedWorkflowID)
	if err != nil {
		return err
	}
	status = strings.ToLower(strings.TrimSpace(status))
	if status != "passed" && status != "failed" {
		return fmt.Errorf("browser verification must be completed before it can be linked")
	}
	uri := "browser-verification://run/" + runID
	links, err := s.repo.FindSourceLinks(record.Item.ID)
	if err != nil {
		return fmt.Errorf("load workflow source links: %w", err)
	}
	linked := false
	for _, link := range links {
		if link.SourceURI == uri && link.Relationship == "read_only_browser_verification" {
			linked = true
			break
		}
	}
	label := firstNonEmpty(strings.TrimSpace(profileID), "Local browser verification")
	if !linked {
		if _, err := s.repo.CreateSourceLink(&models.WorkflowSourceLink{
			WorkflowID: record.Item.ID, SourceType: "browser_verification", SourceID: runID,
			SourceURI: uri, SourceLabel: label, Relationship: "read_only_browser_verification",
		}); err != nil {
			return fmt.Errorf("store browser verification source link: %w", err)
		}
	}
	reason := "read-only local browser verification " + status + ": " + firstNonEmpty(strings.TrimSpace(summary), "no summary returned")
	if strings.TrimSpace(finalPath) != "" {
		reason += " (path " + strings.TrimSpace(finalPath) + ")"
	}
	if strings.TrimSpace(pageTitle) != "" {
		reason += " (title " + strings.TrimSpace(pageTitle) + ")"
	}
	if err := s.requireQualityGate(record.Item.ID, "local browser verification", status, reason); err != nil {
		return err
	}
	s.audit(record.Item.ID, "workflow.browser_verification_linked", record.Item.CurrentState, record.Item.CurrentState, reason, "read_only_browser_verification", status, uri, "browser_verifier")
	return nil
}

// AttachSecretScan links a redacted aggregate Gitleaks result to an
// owner-authorized workflow. It records only the reviewed snapshot identifier,
// aggregate counts, and an opaque result digest. A scan is a review signal, not
// source evidence or completion proof, so it cannot move workflow state or
// authorize execution.
func (s *service) AttachSecretScan(ownerIdentity, workflowID, workspaceID, resultDigest string, findingCount, affectedFiles int) error {
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	if ownerIdentity == "" {
		return fmt.Errorf("owner identity is required")
	}
	workflowID = strings.TrimSpace(workflowID)
	workspaceID = strings.TrimSpace(workspaceID)
	resultDigest = strings.TrimSpace(resultDigest)
	parsedWorkflowID, err := uuid.Parse(workflowID)
	if err != nil {
		return fmt.Errorf("workflow id is invalid")
	}
	if !validWorkflowScanWorkspace(workspaceID) || len(resultDigest) != sha256.Size*2 || findingCount < 0 || affectedFiles < 0 || affectedFiles > findingCount {
		return fmt.Errorf("aggregate secret scan result is invalid")
	}
	if _, err := hex.DecodeString(resultDigest); err != nil {
		return fmt.Errorf("aggregate secret scan result is invalid")
	}
	record, err := s.GetForOwner(ownerIdentity, parsedWorkflowID)
	if err != nil {
		return err
	}
	uri := "gitleaks://scan/" + workspaceID + "/" + resultDigest
	links, err := s.repo.FindSourceLinks(record.Item.ID)
	if err != nil {
		return fmt.Errorf("load workflow source links: %w", err)
	}
	linked := false
	for _, link := range links {
		if link.SourceURI == uri && link.Relationship == "aggregate_secret_scan" {
			linked = true
			break
		}
	}
	if !linked {
		if _, err := s.repo.CreateSourceLink(&models.WorkflowSourceLink{
			WorkflowID: record.Item.ID, SourceType: "gitleaks_scan", SourceID: resultDigest,
			SourceURI: uri, SourceLabel: workspaceID, Relationship: "aggregate_secret_scan",
		}); err != nil {
			return fmt.Errorf("store secret scan source link: %w", err)
		}
	}
	decision := "passed"
	reason := fmt.Sprintf("redacted aggregate secret scan found no findings in reviewed snapshot %s", workspaceID)
	if findingCount > 0 {
		decision = "needs_review"
		reason = fmt.Sprintf("redacted aggregate secret scan found %d finding(s) across %d affected file(s) in reviewed snapshot %s", findingCount, affectedFiles, workspaceID)
	}
	s.decide(record.Item.ID, "aggregate_secret_scan", decision, reason, "read_only_security_scan", false, "gitleaks")
	s.audit(record.Item.ID, "workflow.secret_scan_linked", record.Item.CurrentState, record.Item.CurrentState, reason, "aggregate_secret_scan", decision, uri, "gitleaks")
	return nil
}

// AttachSBOMInventory links a redacted aggregate Syft result to an owner-
// authorized workflow. It provides review context only: package and ecosystem
// counts cannot establish a dependency's safety, change workflow state, or
// authorize execution.
func (s *service) AttachSBOMInventory(ownerIdentity, workflowID, workspaceID, resultDigest string, packageCount, ecosystemCount int) error {
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	if ownerIdentity == "" {
		return fmt.Errorf("owner identity is required")
	}
	workflowID = strings.TrimSpace(workflowID)
	workspaceID = strings.TrimSpace(workspaceID)
	resultDigest = strings.TrimSpace(resultDigest)
	parsedWorkflowID, err := uuid.Parse(workflowID)
	if err != nil {
		return fmt.Errorf("workflow id is invalid")
	}
	if !validWorkflowScanWorkspace(workspaceID) || len(resultDigest) != sha256.Size*2 || packageCount < 0 || ecosystemCount < 0 || (packageCount > 0 && ecosystemCount == 0) {
		return fmt.Errorf("aggregate SBOM inventory result is invalid")
	}
	if _, err := hex.DecodeString(resultDigest); err != nil {
		return fmt.Errorf("aggregate SBOM inventory result is invalid")
	}
	record, err := s.GetForOwner(ownerIdentity, parsedWorkflowID)
	if err != nil {
		return err
	}
	uri := "syft://inventory/" + workspaceID + "/" + resultDigest
	links, err := s.repo.FindSourceLinks(record.Item.ID)
	if err != nil {
		return fmt.Errorf("load workflow source links: %w", err)
	}
	linked := false
	for _, link := range links {
		if link.SourceURI == uri && link.Relationship == "aggregate_sbom_inventory" {
			linked = true
			break
		}
	}
	if !linked {
		if _, err := s.repo.CreateSourceLink(&models.WorkflowSourceLink{
			WorkflowID: record.Item.ID, SourceType: "syft_inventory", SourceID: resultDigest,
			SourceURI: uri, SourceLabel: workspaceID, Relationship: "aggregate_sbom_inventory",
		}); err != nil {
			return fmt.Errorf("store SBOM inventory source link: %w", err)
		}
	}
	reason := fmt.Sprintf("redacted aggregate SBOM inventory recorded %d package(s) across %d ecosystem(s) in reviewed snapshot %s; review in the original workspace before making dependency decisions", packageCount, ecosystemCount, workspaceID)
	s.decide(record.Item.ID, "aggregate_sbom_inventory", "needs_review", reason, "read_only_software_inventory", false, "syft")
	s.audit(record.Item.ID, "workflow.sbom_inventory_linked", record.Item.CurrentState, record.Item.CurrentState, reason, "aggregate_sbom_inventory", "needs_review", uri, "syft")
	return nil
}

// AttachMiniSWEPatchProposal records only an opaque disposable patch proposal
// reference. The generated diff remains response-only at the mini-SWE boundary;
// this workflow link is a review signal and cannot apply code, change state, or
// satisfy a technical completion gate.
func (s *service) AttachMiniSWEPatchProposal(ownerIdentity, workflowID, proposalID, workspaceID, diffDigest string, changedFiles int) error {
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	if ownerIdentity == "" {
		return fmt.Errorf("owner identity is required")
	}
	workflowID = strings.TrimSpace(workflowID)
	proposalID = strings.TrimSpace(proposalID)
	workspaceID = strings.TrimSpace(workspaceID)
	diffDigest = strings.TrimSpace(diffDigest)
	parsedWorkflowID, err := uuid.Parse(workflowID)
	if err != nil {
		return fmt.Errorf("workflow id is invalid")
	}
	if _, err := uuid.Parse(proposalID); err != nil {
		return fmt.Errorf("patch proposal id is invalid")
	}
	if !validWorkflowScanWorkspace(workspaceID) || len(diffDigest) != sha256.Size*2 || changedFiles < 0 || changedFiles > 2000 {
		return fmt.Errorf("patch proposal result is invalid")
	}
	if _, err := hex.DecodeString(diffDigest); err != nil {
		return fmt.Errorf("patch proposal result is invalid")
	}
	record, err := s.GetForOwner(ownerIdentity, parsedWorkflowID)
	if err != nil {
		return err
	}
	uri := "mini-swe://proposal/" + proposalID + "/" + diffDigest
	links, err := s.repo.FindSourceLinks(record.Item.ID)
	if err != nil {
		return fmt.Errorf("load workflow source links: %w", err)
	}
	linked := false
	for _, link := range links {
		if link.SourceURI == uri && link.Relationship == "review_only_patch_proposal" {
			linked = true
			break
		}
	}
	if !linked {
		if _, err := s.repo.CreateSourceLink(&models.WorkflowSourceLink{
			WorkflowID: record.Item.ID, SourceType: "mini_swe_patch_proposal", SourceID: proposalID,
			SourceURI: uri, SourceLabel: workspaceID, Relationship: "review_only_patch_proposal",
		}); err != nil {
			return fmt.Errorf("store patch proposal source link: %w", err)
		}
	}
	reason := fmt.Sprintf("isolated mini-SWE patch proposal returned an opaque diff digest with %d changed file(s); review the response-only diff before any independent apply or test", changedFiles)
	s.decide(record.Item.ID, "mini_swe_patch_proposal", "needs_review", reason, "review_only_patch_proposal", false, "mini-swe")
	s.audit(record.Item.ID, "workflow.mini_swe_patch_linked", record.Item.CurrentState, record.Item.CurrentState, reason, "review_only_patch_proposal", "needs_review", uri, "mini-swe")
	return nil
}

func validWorkflowScanWorkspace(value string) bool {
	if len(value) == 0 || len(value) > 64 {
		return false
	}
	for _, character := range value {
		if (character >= 'A' && character <= 'Z') || (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9') || character == '_' || character == '-' {
			continue
		}
		return false
	}
	first := value[0]
	if first == '_' || first == '-' {
		return false
	}
	return true
}

func (s *service) get(id uuid.UUID) (*WorkflowRecord, error) {
	item, err := s.repo.FindItem(id)
	if err != nil {
		return nil, err
	}
	checklist, err := s.repo.FindChecklist(id)
	if err != nil {
		return nil, err
	}
	intake, err := s.repo.FindIntakeRecords(id)
	if err != nil {
		return nil, err
	}
	matches, err := s.repo.FindProjectMatches(id)
	if err != nil {
		return nil, err
	}
	pursuits, err := s.repo.FindLinkedPursuits(id)
	if err != nil {
		return nil, err
	}
	evidence, err := s.repo.FindEvidenceClaims(id)
	if err != nil {
		return nil, err
	}
	openLoops, err := s.repo.FindOpenLoops(id)
	if err != nil {
		return nil, err
	}
	proposals, err := s.repo.FindProposals(id)
	if err != nil {
		return nil, err
	}
	qualityGates, err := s.repo.FindQualityGates(id)
	if err != nil {
		return nil, err
	}
	events, err := s.repo.FindEvents(id)
	if err != nil {
		return nil, err
	}
	transitions, err := s.repo.FindTransitions(id)
	if err != nil {
		return nil, err
	}
	sourceLinks, err := s.repo.FindSourceLinks(id)
	if err != nil {
		return nil, err
	}
	decisions, err := s.repo.FindDecisions(id)
	if err != nil {
		return nil, err
	}
	return &WorkflowRecord{
		Item:                *item,
		Checklist:           checklist,
		Intake:              intake,
		Matches:             matches,
		Pursuits:            pursuits,
		Evidence:            evidence,
		OpenLoops:           openLoops,
		Proposals:           proposals,
		QualityGates:        qualityGates,
		Transitions:         transitions,
		SourceLinks:         sourceLinks,
		Decisions:           decisions,
		Events:              events,
		FrameworkSelections: frameworkSelectionsFromDecisions(decisions),
	}, nil
}

func workflowVisibleTo(item models.WorkflowItem, ownerIdentity string) bool {
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	if ownerIdentity == "" {
		return true
	}
	return strings.TrimSpace(item.OwnerIdentity) == ownerIdentity
}

func visibleWorkflowItems(ownerIdentity string, items []models.WorkflowItem) []models.WorkflowItem {
	visible := make([]models.WorkflowItem, 0, len(items))
	for _, item := range items {
		if workflowVisibleTo(item, ownerIdentity) {
			visible = append(visible, item)
		}
	}
	return visible
}

func workflowIDSet(items []models.WorkflowItem) map[uuid.UUID]struct{} {
	ids := make(map[uuid.UUID]struct{}, len(items))
	for _, item := range items {
		if item.ID != uuid.Nil {
			ids[item.ID] = struct{}{}
		}
	}
	return ids
}

func visibleWorkflowItemsByID(ids map[uuid.UUID]struct{}, items []models.WorkflowItem) []models.WorkflowItem {
	visible := make([]models.WorkflowItem, 0, len(items))
	for _, item := range items {
		if _, ok := ids[item.ID]; ok {
			visible = append(visible, item)
		}
	}
	return visible
}

func visibleWorkflowOpenLoops(ids map[uuid.UUID]struct{}, loops []models.WorkflowOpenLoop) []models.WorkflowOpenLoop {
	visible := make([]models.WorkflowOpenLoop, 0, len(loops))
	for _, loop := range loops {
		if _, ok := ids[loop.WorkflowID]; ok {
			visible = append(visible, loop)
		}
	}
	return visible
}

func visibleWorkflowPursuits(ownerIdentity string, pursuits []WorkflowPursuitContext) []WorkflowPursuitContext {
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	if ownerIdentity == "" {
		return pursuits
	}
	visible := make([]WorkflowPursuitContext, 0, len(pursuits))
	for _, pursuit := range pursuits {
		owner := strings.TrimSpace(pursuit.OwnerIdentity)
		if owner == "" || owner == ownerIdentity {
			visible = append(visible, pursuit)
		}
	}
	return visible
}

func (s *service) Transition(id uuid.UUID, request TransitionRequest) (*WorkflowRecord, error) {
	item, err := s.repo.FindItem(id)
	if err != nil {
		return nil, err
	}
	target := strings.TrimSpace(request.TargetState)
	if target == "" {
		return nil, fmt.Errorf("targetState is required")
	}
	if target == StateCompleted {
		return nil, fmt.Errorf("manual completion is not allowed; completion requires verified worker output or interrupted-execution resolution")
	}
	if item.RecoveryStatus == RecoveryNeedsReview && target != StateBlocked {
		return nil, fmt.Errorf("interrupted execution must be resolved before changing workflow state")
	}
	if target == StateReady && item.RequiresApproval && item.ApprovalStatus != "approved" && !request.Approved {
		return nil, fmt.Errorf("approval is required before workflow can become ready")
	}
	if !transitionAllowed(item.CurrentState, target, request.Approved) {
		return nil, fmt.Errorf("transition from %s to %s is not allowed", item.CurrentState, target)
	}
	decisionRule := "manual approval gate"
	if request.Approved {
		decisionRule, err = s.approvalDecisionRule(item)
		if err != nil {
			return nil, err
		}
	}
	from := item.CurrentState
	item.CurrentState = target
	if target == StateNeedsApproval {
		item.RequiresApproval = true
		item.ApprovalStatus = "pending"
		item.ApprovalReason = firstNonEmpty(item.ApprovalReason, "manual review requested")
	}
	if target == StateReady && item.RequiresApproval && request.Approved {
		item.BlockedReason = ""
		item.NextAction = "execute approved workflow steps"
		item.ApprovalStatus = "approved"
	}
	if target == StateBlocked {
		item.BlockedReason = firstNonEmpty(request.Message, "workflow blocked")
		item.NextAction = "resolve blocker before continuing"
	}
	if target == StateCompleted {
		item.NextAction = "write completion summary and archive when reviewed"
		now := time.Now().UTC()
		item.CompletedAt = &now
	}
	if target == StateArchived {
		item.Archived = true
	}
	updated, err := s.repo.UpdateItem(item)
	if err != nil {
		return nil, err
	}
	s.recordTransition(updated.ID, from, target, "manual_transition", firstNonEmpty(request.Actor, "operator"), request.Approved, request.Message)
	if request.Approved {
		s.decide(updated.ID, "approval", "approved", firstNonEmpty(request.Message, "human approval recorded"), decisionRule, true, firstNonEmpty(request.Actor, "operator"))
	}
	s.audit(updated.ID, "workflow.transition", from, target, request.Message, "manual_transition", approvalRule(request.Approved), updated.SourceURI, firstNonEmpty(request.Actor, "operator"))
	return s.Get(updated.ID)
}

func (s *service) ResolveApproval(id uuid.UUID, request ApprovalResolutionRequest) (*WorkflowRecord, error) {
	item, err := s.repo.FindItem(id)
	if err != nil {
		return nil, err
	}
	if item.RecoveryStatus == RecoveryNeedsReview {
		return nil, fmt.Errorf("interrupted execution must be resolved before approval can continue")
	}
	if request.Approved {
		proposals, proposalErr := s.repo.FindProposals(id)
		if proposalErr != nil {
			return nil, proposalErr
		}
		for index := range proposals {
			if proposals[index].Status == "open" && isAutomationSelectionProposal(&proposals[index]) {
				return nil, fmt.Errorf("select the exact automation candidate before approving controlled execution")
			}
		}
		record, err := s.Transition(id, TransitionRequest{
			TargetState: StateReady,
			Message:     firstNonEmpty(request.Note, "workflow approved for controlled execution"),
			Approved:    true,
			Actor:       firstNonEmpty(request.Actor, "operator"),
		})
		if err != nil {
			return nil, err
		}
		s.rememberCorrection(&record.Item, "approval_approved", request.Note, firstNonEmpty(request.Actor, "operator"))
		return record, nil
	}
	from := item.CurrentState
	item.CurrentState = StateBlocked
	item.ApprovalStatus = "rejected"
	item.BlockedReason = firstNonEmpty(request.Note, "approval rejected")
	item.NextAction = "review rejection reason before continuing"
	updated, err := s.repo.UpdateItem(item)
	if err != nil {
		return nil, err
	}
	s.recordTransition(updated.ID, from, StateBlocked, "approval_resolution", firstNonEmpty(request.Actor, "operator"), false, updated.BlockedReason)
	s.decide(updated.ID, "approval", "rejected", updated.BlockedReason, "manual approval gate", false, firstNonEmpty(request.Actor, "operator"))
	s.audit(updated.ID, "workflow.approval", from, StateBlocked, updated.BlockedReason, "approval_resolution", "human approval rejected", updated.SourceURI, firstNonEmpty(request.Actor, "operator"))
	s.rememberCorrection(updated, "approval_rejected", updated.BlockedReason, firstNonEmpty(request.Actor, "operator"))
	return s.Get(updated.ID)
}

func (s *service) ResolveInterruptedExecution(id uuid.UUID, request InterruptedExecutionResolutionRequest) (*WorkflowRecord, error) {
	item, err := s.repo.FindItem(id)
	if err != nil {
		return nil, err
	}
	if item.CurrentState != StateBlocked || item.RecoveryStatus != RecoveryNeedsReview {
		return nil, fmt.Errorf("workflow does not have an interrupted execution awaiting review")
	}

	decision := strings.ToLower(strings.TrimSpace(request.Decision))
	note := strings.TrimSpace(request.Note)
	actor := firstNonEmpty(request.Actor, "operator")
	if note == "" {
		return nil, fmt.Errorf("note is required to resolve interrupted execution")
	}

	from := item.CurrentState
	switch decision {
	case "retry":
		item.RecoveryStatus = RecoveryRetryConfirmed
		item.RecoveryNote = note
		item.BlockedReason = ""
		item.LastWorkerError = ""
		item.CompletedAt = nil
		item.VerificationStatus = ""
		item.NextRunAt = nil
		if item.MaxRetries <= item.RetryCount {
			item.MaxRetries = item.RetryCount + 1
		}
		if item.RequiresApproval {
			item.CurrentState = StateNeedsApproval
			item.ApprovalStatus = "pending"
			item.ApprovalReason = "interrupted high-risk execution requires fresh approval before retry"
			item.NextAction = "approve controlled retry after confirming prior side effects did not occur"
		} else {
			item.CurrentState = StateReady
			item.NextAction = "retry interrupted workflow after operator side-effect review"
		}
	case "confirm_completed":
		evidenceURI := strings.TrimSpace(request.EvidenceURI)
		if evidenceURI == "" {
			return nil, fmt.Errorf("evidenceUri is required to confirm interrupted execution completed")
		}
		evidenceLabel := firstNonEmpty(request.EvidenceLabel, "Interrupted execution completion evidence")
		if _, err := s.repo.CreateSourceLink(&models.WorkflowSourceLink{
			WorkflowID:   item.ID,
			SourceType:   "recovery_evidence",
			SourceURI:    evidenceURI,
			SourceLabel:  evidenceLabel,
			Relationship: "completion_evidence",
		}); err != nil {
			return nil, fmt.Errorf("store completion evidence link: %w", err)
		}
		if _, err := s.repo.CreateEvidenceClaim(&models.WorkflowEvidenceClaim{
			WorkflowID:  item.ID,
			ClaimText:   note,
			SourceURI:   evidenceURI,
			SourceLabel: evidenceLabel,
			Reliability: "operator_attestation",
			Status:      "human_approved",
			NeedsReview: false,
		}); err != nil {
			return nil, fmt.Errorf("store completion evidence claim: %w", err)
		}
		if err := s.requireQualityGate(item.ID, "verification before completion", "passed", "operator confirmed interrupted execution outcome with linked evidence"); err != nil {
			return nil, err
		}
		now := time.Now().UTC()
		item.CurrentState = StateCompleted
		item.CompletedAt = &now
		item.RecoveryStatus = RecoveryCompletionConfirmed
		item.RecoveryNote = note
		item.VerificationStatus = "human_approved"
		item.BlockedReason = ""
		item.LastWorkerError = ""
		item.NextRunAt = nil
		item.NextAction = "review completion summary and archive when appropriate"
	case "keep_blocked":
		item.RecoveryNote = note
		item.BlockedReason = "interrupted execution remains blocked after operator review"
		item.NextAction = note
	default:
		return nil, fmt.Errorf("decision must be retry, confirm_completed, or keep_blocked")
	}

	updated, err := s.repo.UpdateItem(item)
	if err != nil {
		return nil, err
	}
	approved := decision == "confirm_completed"
	s.recordTransition(updated.ID, from, updated.CurrentState, "interrupted_execution_resolution", actor, approved, note)
	s.decide(updated.ID, "interrupted_execution", decision, note, "unknown external side effects require explicit operator resolution", approved, actor)
	s.audit(updated.ID, "workflow.interruption_resolved", from, updated.CurrentState, note, "interrupted_execution_resolution", decision, firstNonEmpty(request.EvidenceURI, updated.SourceURI), actor)
	s.rememberCorrection(updated, "interruption_"+decision, note, actor)
	if decision == "confirm_completed" {
		s.markChecklistProgress(updated.ID, "Verify completion before closing")
	}
	return s.Get(updated.ID)
}

func (s *service) ResolveProposal(id uuid.UUID, proposalID uuid.UUID, request ProposalResolutionRequest) (*WorkflowRecord, error) {
	item, err := s.repo.FindItem(id)
	if err != nil {
		return nil, err
	}
	proposals, err := s.repo.FindProposals(id)
	if err != nil {
		return nil, err
	}
	var proposal *models.WorkflowProposal
	for index := range proposals {
		if proposals[index].ID == proposalID {
			proposal = &proposals[index]
			break
		}
	}
	if proposal == nil {
		return nil, fmt.Errorf("proposal not found")
	}
	if proposal.Status != "open" {
		return nil, fmt.Errorf("proposal is already resolved")
	}
	if item.Archived || item.CurrentState == StateArchived || item.CurrentState == StateCompleted {
		return nil, fmt.Errorf("closed workflows cannot resolve proposals")
	}
	if item.RecoveryStatus == RecoveryNeedsReview {
		return nil, fmt.Errorf("interrupted execution must be resolved before proposals can change workflow state")
	}

	status, err := normalizeProposalStatus(request)
	if err != nil {
		return nil, err
	}
	if status == "approved" && isAutomationSelectionProposal(proposal) {
		automationID, selectionErr := selectedAutomationID(proposal.Options, request.SelectedOption)
		if selectionErr != nil {
			return nil, selectionErr
		}
		item.AutomationID = automationID
		item.NextAction = "approve the exact automation action and queue controlled execution"
		if item.RequiresApproval {
			if _, bindingErr := s.approvalDecisionRule(item); bindingErr != nil {
				return nil, fmt.Errorf("selected automation cannot be bound to approval: %w", bindingErr)
			}
		}
		if _, err := s.repo.UpdateItem(item); err != nil {
			return nil, err
		}
		s.decide(id, "automation_selection", automationID, "operator selected the exact automation candidate", "runtime-selection proposal", true, firstNonEmpty(request.Actor, "operator"))
	}
	now := time.Now().UTC()
	proposal.Status = status
	proposal.SelectedOption = strings.TrimSpace(request.SelectedOption)
	proposal.ResolutionNote = strings.TrimSpace(request.Note)
	proposal.ResolvedBy = firstNonEmpty(request.Actor, "operator")
	proposal.ResolvedAt = &now
	if _, err := s.repo.UpdateProposal(proposal); err != nil {
		return nil, err
	}
	s.decide(id, "proposal", status, firstNonEmpty(request.Note, proposal.RecommendedAction), "proposal/yes-no engine", status == "approved", firstNonEmpty(request.Actor, "operator"))
	s.audit(id, "workflow.proposal", "", item.CurrentState, "proposal "+status+": "+proposal.RecommendedAction, "proposal_resolution", "proposal/yes-no engine", item.SourceURI, firstNonEmpty(request.Actor, "operator"))

	switch status {
	case "approved":
		if item.CurrentState == StateNeedsApproval || item.RequiresApproval {
			return s.ResolveApproval(id, ApprovalResolutionRequest{
				Approved: true,
				Note:     firstNonEmpty(request.Note, proposal.RecommendedAction),
				Actor:    firstNonEmpty(request.Actor, "operator"),
			})
		}
		if item.CurrentState == StateBlocked || item.CurrentState == StateWaitingInput {
			from := item.CurrentState
			item.CurrentState = StateReady
			item.BlockedReason = ""
			item.NextAction = "execute approved proposal through workflow worker"
			if _, err := s.repo.UpdateItem(item); err != nil {
				return nil, err
			}
			s.recordTransition(item.ID, from, StateReady, "proposal_resolution", firstNonEmpty(request.Actor, "operator"), true, firstNonEmpty(request.Note, "proposal approved"))
			s.audit(item.ID, "workflow.transition", from, StateReady, "proposal approved and workflow made ready", "proposal_resolution", "proposal approved", item.SourceURI, firstNonEmpty(request.Actor, "operator"))
		}
	case "changes_requested":
		from := item.CurrentState
		item.CurrentState = StateWaitingInput
		item.NextAction = firstNonEmpty(request.Note, "apply requested proposal changes")
		item.BlockedReason = ""
		if _, err := s.repo.UpdateItem(item); err != nil {
			return nil, err
		}
		s.recordTransition(item.ID, from, StateWaitingInput, "proposal_resolution", firstNonEmpty(request.Actor, "operator"), false, item.NextAction)
		s.rememberCorrection(item, "proposal_changes_requested", firstNonEmpty(request.Note, request.SelectedOption), firstNonEmpty(request.Actor, "operator"))
	case "rejected":
		from := item.CurrentState
		item.CurrentState = StateBlocked
		item.ApprovalStatus = "rejected"
		item.BlockedReason = firstNonEmpty(request.Note, "proposal rejected")
		item.NextAction = "review rejected proposal before continuing"
		if _, err := s.repo.UpdateItem(item); err != nil {
			return nil, err
		}
		s.recordTransition(item.ID, from, StateBlocked, "proposal_resolution", firstNonEmpty(request.Actor, "operator"), false, item.BlockedReason)
		s.rememberCorrection(item, "proposal_rejected", item.BlockedReason, firstNonEmpty(request.Actor, "operator"))
	}
	return s.Get(id)
}

func (s *service) rememberCorrection(item *models.WorkflowItem, signal, note, actor string) {
	if item == nil || !feedbackNoteUseful(signal, note) {
		return
	}
	s.recordControlledCorrection(item, signal, note, actor)
	if s.memoryService == nil {
		return
	}
	note = strings.TrimSpace(note)
	sourceURI := firstNonEmpty(item.SourceURI, "workflow://"+item.ID.String())
	sourceLabel := firstNonEmpty(item.SourceLabel, "Workflow feedback: "+item.Title)
	content := feedbackLessonContent(*item, signal, note)
	_, err := memory.CreateForOwner(s.memoryService, item.OwnerIdentity, memory.CreateRequest{
		ProjectKey:  item.ProjectKey,
		Kind:        "lesson",
		Content:     content,
		Summary:     feedbackLessonSummary(*item, signal, note),
		Tags:        feedbackLessonTags(*item, signal),
		Confidence:  feedbackLessonConfidence(signal),
		SourceURI:   sourceURI,
		SourceLabel: sourceLabel,
	})
	if err != nil {
		s.audit(item.ID, "workflow.feedback_memory_failed", item.CurrentState, item.CurrentState, err.Error(), signal, "learning feedback memory", sourceURI, actor)
		return
	}
	s.audit(item.ID, "workflow.feedback_memory", item.CurrentState, item.CurrentState, "stored reviewable correction lesson", signal, "learning feedback memory", sourceURI, actor)
}

func (s *service) UpdateChecklistItem(id uuid.UUID, itemID uuid.UUID, request ChecklistUpdateRequest) (*WorkflowRecord, error) {
	checklist, err := s.repo.FindChecklist(id)
	if err != nil {
		return nil, err
	}
	actor := firstNonEmpty(request.Actor, "operator")
	for _, item := range checklist {
		if item.ID != itemID {
			continue
		}
		status := firstNonEmpty(request.Status, "done")
		if status != "open" && status != "done" && status != "blocked" {
			return nil, fmt.Errorf("unsupported checklist status")
		}
		item.Status = status
		if _, err := s.repo.UpdateChecklistItem(&item); err != nil {
			return nil, err
		}
		note := strings.TrimSpace(request.Note)
		message := "checklist item marked " + status + ": " + item.Label
		if note != "" {
			message += " | " + note
		}
		s.audit(id, "workflow.checklist", "", "", message, "checklist_update", "checklist progress tracked", "", actor)
		if note != "" || status == "blocked" {
			if workflowItem, err := s.repo.FindItem(id); err == nil {
				s.rememberCorrection(workflowItem, "checklist_"+status, firstNonEmpty(note, message), actor)
			}
		}
		return s.Get(id)
	}
	return nil, fmt.Errorf("checklist item not found")
}

func (s *service) RecoverStaleClaims(request RunDueRequest) (*ClaimRecoverySummary, error) {
	return s.RecoverStaleClaimsForOwner("", request)
}

func (s *service) RecoverStaleClaimsForOwner(ownerIdentity string, request RunDueRequest) (*ClaimRecoverySummary, error) {
	return s.RecoverStaleClaimsForOwnerContext(context.Background(), ownerIdentity, request)
}

func (s *service) RecoverStaleClaimsForOwnerContext(ctx context.Context, ownerIdentity string, request RunDueRequest) (*ClaimRecoverySummary, error) {
	ctx = normalizeBatchContext(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	now := time.Now().UTC()
	limit := normalizeRunLimit(request.Limit)
	items, err := s.repo.FindExpiredWorkflowClaimsForOwner(ownerIdentity, now, limit)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	loops, err := s.repo.FindExpiredOpenLoopClaimsForOwner(ownerIdentity, now, limit)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	summary := &ClaimRecoverySummary{
		Checked: len(items) + len(loops),
		Results: []ClaimRecoveryResult{},
	}
	for _, item := range items {
		if err := ctx.Err(); err != nil {
			return summary, err
		}
		recovered, changed, recoverErr := s.repo.RecoverExpiredWorkflowClaim(item, now)
		if recoverErr != nil {
			summary.Skipped++
			summary.Results = append(summary.Results, ClaimRecoveryResult{
				WorkflowID: item.ID,
				Type:       "workflow",
				Status:     "skipped",
				Message:    recoverErr.Error(),
			})
			continue
		}
		if !changed || recovered == nil {
			summary.Skipped++
			summary.Results = append(summary.Results, ClaimRecoveryResult{
				WorkflowID: item.ID,
				Type:       "workflow",
				Status:     "skipped",
				Message:    "workflow claim was renewed or recovered by another worker",
			})
			continue
		}
		summary.WorkflowsBlocked++
		message := "expired workflow claim moved to review because execution outcome is unknown"
		summary.Results = append(summary.Results, ClaimRecoveryResult{
			WorkflowID: recovered.ID,
			Type:       "workflow",
			Status:     "blocked",
			Message:    message,
		})
		s.recordTransition(recovered.ID, StateInProgress, StateBlocked, "worker_lease_expired", "workflow-recovery", recovered.ApprovalStatus == "approved", message)
		s.decide(recovered.ID, "worker_recovery", "blocked", message, "unknown external side effects require human review", false, "workflow-recovery")
		s.audit(recovered.ID, "workflow.worker_recovered", StateInProgress, StateBlocked, message, "worker_lease_expired", "claim lease recovery", recovered.SourceURI, "workflow-recovery")
	}
	for _, loop := range loops {
		if err := ctx.Err(); err != nil {
			return summary, err
		}
		recovered, changed, recoverErr := s.repo.RecoverExpiredOpenLoopClaim(loop, now)
		if recoverErr != nil {
			summary.Skipped++
			summary.Results = append(summary.Results, ClaimRecoveryResult{
				WorkflowID: loop.WorkflowID,
				OpenLoopID: loop.ID,
				Type:       "open_loop",
				Status:     "skipped",
				Message:    recoverErr.Error(),
			})
			continue
		}
		if !changed || recovered == nil {
			summary.Skipped++
			summary.Results = append(summary.Results, ClaimRecoveryResult{
				WorkflowID: loop.WorkflowID,
				OpenLoopID: loop.ID,
				Type:       "open_loop",
				Status:     "skipped",
				Message:    "open-loop claim was recovered by another worker",
			})
			continue
		}
		summary.OpenLoopsReopened++
		message := "expired idempotent follow-up claim reopened for retry"
		summary.Results = append(summary.Results, ClaimRecoveryResult{
			WorkflowID: recovered.WorkflowID,
			OpenLoopID: recovered.ID,
			Type:       "open_loop",
			Status:     "reopened",
			Message:    message,
		})
		s.decide(recovered.WorkflowID, "open_loop_recovery", "reopened", message, "idempotent follow-up artifacts", false, "workflow-recovery")
		s.audit(recovered.WorkflowID, "workflow.open_loop_recovered", "", "", message, "open_loop_lease_expired", "claim lease recovery", "", "workflow-recovery")
	}
	return summary, nil
}

func (s *service) RunDue(request RunDueRequest) (*WorkflowRunSummary, error) {
	return s.RunDueForOwner("", request)
}

func (s *service) RunDueForOwner(ownerIdentity string, request RunDueRequest) (*WorkflowRunSummary, error) {
	return s.RunDueForOwnerContext(context.Background(), ownerIdentity, request)
}

func (s *service) RunDueForOwnerContext(ctx context.Context, ownerIdentity string, request RunDueRequest) (*WorkflowRunSummary, error) {
	ctx = normalizeBatchContext(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	items, err := s.repo.FindRunnableItemsForOwner(ownerIdentity, time.Now().UTC(), normalizeRunLimit(request.Limit))
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	summary := &WorkflowRunSummary{
		Checked: len(items),
		Results: []WorkflowRunResult{},
	}
	if safety.EmergencyStopActive() {
		reason := safety.EmergencyStopReason()
		for _, item := range items {
			summary.Blocked++
			summary.Results = append(summary.Results, WorkflowRunResult{WorkflowID: item.ID, Status: "blocked", State: item.CurrentState, Attempts: item.RetryCount, Message: reason})
			s.decide(item.ID, "worker_execution", "blocked", reason, "emergency stop", false, "workflow-worker")
			s.audit(item.ID, "workflow.worker_blocked", item.CurrentState, item.CurrentState, reason, "emergency_stop", "emergency stop", item.SourceURI, "workflow-worker")
		}
		return summary, nil
	}
	for _, item := range items {
		if err := ctx.Err(); err != nil {
			return summary, err
		}
		result := s.claimAndRunWorkflow(ownerIdentity, item)
		summary.Results = append(summary.Results, result)
		switch result.Status {
		case "completed":
			summary.Completed++
		case "retry_scheduled":
			summary.Retried++
		case "blocked":
			summary.Blocked++
		default:
			summary.Skipped++
		}
	}
	return summary, nil
}

// RunOneForOwner claims and runs exactly one owner-scoped workflow. It is the
// precise operator command used after review; all concrete task/runtime effects
// still pass through the task runner's authorization and verification gates.
func (s *service) RunOneForOwner(ownerIdentity string, id uuid.UUID) (*WorkflowRunResult, error) {
	return s.RunOneForOwnerContext(context.Background(), ownerIdentity, id)
}

func (s *service) RunOneForOwnerContext(ctx context.Context, ownerIdentity string, id uuid.UUID) (*WorkflowRunResult, error) {
	ctx = normalizeBatchContext(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	item, err := s.repo.FindItem(id)
	if err != nil || item == nil || strings.TrimSpace(item.OwnerIdentity) != ownerIdentity {
		return nil, fmt.Errorf("workflow not found")
	}
	if safety.EmergencyStopActive() {
		reason := safety.EmergencyStopReason()
		s.decide(item.ID, "worker_execution", "blocked", reason, "emergency stop", false, "workflow-worker")
		s.audit(item.ID, "workflow.worker_blocked", item.CurrentState, item.CurrentState, reason, "emergency_stop", "emergency stop", item.SourceURI, "workflow-worker")
		return &WorkflowRunResult{WorkflowID: item.ID, Status: "blocked", State: item.CurrentState, Attempts: item.RetryCount, Message: reason}, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	result := s.claimAndRunWorkflow(ownerIdentity, *item)
	return &result, nil
}

func (s *service) claimAndRunWorkflow(ownerIdentity string, item models.WorkflowItem) WorkflowRunResult {
	claimedAt := time.Now().UTC()
	claimID := uuid.NewString()
	claimed, acquired, err := s.repo.ClaimRunnableItemForOwner(
		ownerIdentity,
		item.ID,
		claimID,
		claimedAt,
		claimedAt.Add(claimLeaseDuration()),
	)
	if err != nil {
		return WorkflowRunResult{
			WorkflowID: item.ID,
			Status:     "blocked",
			State:      item.CurrentState,
			Attempts:   item.RetryCount,
			Message:    "failed to claim runnable workflow: " + err.Error(),
		}
	}
	if !acquired || claimed == nil {
		return WorkflowRunResult{
			WorkflowID: item.ID,
			Status:     "skipped",
			State:      item.CurrentState,
			Attempts:   item.RetryCount,
			Message:    "workflow was already claimed, is not approved and ready, or is not due",
		}
	}
	s.recordTransition(claimed.ID, StateReady, StateInProgress, "worker_claim", "workflow-worker", claimed.ApprovalStatus == "approved", "worker atomically claimed runnable workflow")
	s.audit(claimed.ID, "workflow.worker_started", StateReady, StateInProgress, "worker atomically claimed runnable workflow", "worker_claim", "single-consumer execution claim", claimed.SourceURI, "workflow-worker")
	return s.runWorkflowItem(*claimed, claimID)
}

func (s *service) RunDueOpenLoops(request RunDueRequest) (*OpenLoopRunSummary, error) {
	return s.RunDueOpenLoopsForOwner("", request)
}

func (s *service) RunDueOpenLoopsForOwner(ownerIdentity string, request RunDueRequest) (*OpenLoopRunSummary, error) {
	return s.RunDueOpenLoopsForOwnerContext(context.Background(), ownerIdentity, request)
}

func (s *service) RunDueOpenLoopsForOwnerContext(ctx context.Context, ownerIdentity string, request RunDueRequest) (*OpenLoopRunSummary, error) {
	ctx = normalizeBatchContext(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	loops, err := s.repo.FindDashboardOpenLoopsForOwner(ownerIdentity, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	limit := normalizeRunLimit(request.Limit)
	if limit < len(loops) {
		loops = loops[:limit]
	}
	summary := &OpenLoopRunSummary{
		Checked: len(loops),
		Results: []OpenLoopRunResult{},
	}
	if safety.EmergencyStopActive() {
		reason := safety.EmergencyStopReason()
		for _, loop := range loops {
			summary.Skipped++
			summary.Results = append(summary.Results, OpenLoopRunResult{WorkflowID: loop.WorkflowID, OpenLoopID: loop.ID, Status: "skipped", Message: reason})
			s.decide(loop.WorkflowID, "open_loop", "blocked", reason, "emergency stop", false, "workflow-followup")
			s.audit(loop.WorkflowID, "workflow.open_loop_blocked", "", "", reason, "emergency_stop", "emergency stop", "", "workflow-followup")
		}
		return summary, nil
	}
	for _, loop := range loops {
		if err := ctx.Err(); err != nil {
			return summary, err
		}
		claimedAt := time.Now().UTC()
		claimID := uuid.NewString()
		claimed, acquired, claimErr := s.repo.ClaimDueOpenLoopForOwner(ownerIdentity, loop.ID, claimID, claimedAt, claimedAt.Add(claimLeaseDuration()))
		if claimErr != nil {
			summary.Skipped++
			summary.Results = append(summary.Results, OpenLoopRunResult{
				WorkflowID: loop.WorkflowID,
				OpenLoopID: loop.ID,
				Status:     "skipped",
				Message:    "failed to claim due open loop: " + claimErr.Error(),
			})
			continue
		}
		if !acquired || claimed == nil {
			summary.Skipped++
			summary.Results = append(summary.Results, OpenLoopRunResult{
				WorkflowID: loop.WorkflowID,
				OpenLoopID: loop.ID,
				Status:     "skipped",
				Message:    "open loop was already claimed or is no longer due",
			})
			continue
		}
		result := s.runOpenLoop(*claimed, claimID)
		if result.Status == "skipped" {
			claimed.Status = "open"
			claimed.ClaimID = ""
			claimed.LeaseUntil = nil
			if _, owned, releaseErr := s.repo.UpdateClaimedOpenLoop(claimed, claimID); releaseErr != nil {
				result.Message = firstNonEmpty(result.Message, "follow-up processing failed") + "; failed to release open-loop claim: " + releaseErr.Error()
			} else if !owned {
				result.Message = firstNonEmpty(result.Message, "follow-up processing failed") + "; open-loop claim was already lost"
			}
		}
		summary.Results = append(summary.Results, result)
		switch result.Status {
		case "triggered":
			summary.Triggered++
		case "resolved":
			summary.Resolved++
		default:
			summary.Skipped++
		}
	}
	return summary, nil
}

func (s *service) runOpenLoop(loop models.WorkflowOpenLoop, claimID string) OpenLoopRunResult {
	if loop.Status != "processing" {
		return OpenLoopRunResult{WorkflowID: loop.WorkflowID, OpenLoopID: loop.ID, Status: "skipped", Message: "open loop does not hold an active processing claim"}
	}
	item, err := s.repo.FindItem(loop.WorkflowID)
	if err != nil {
		return OpenLoopRunResult{WorkflowID: loop.WorkflowID, OpenLoopID: loop.ID, Status: "skipped", Message: "workflow item not found"}
	}
	if item.Archived || item.CurrentState == StateArchived || item.CurrentState == StateCompleted {
		loop.Status = "resolved"
		loop.ClaimID = ""
		loop.LeaseUntil = nil
		if _, owned, err := s.repo.UpdateClaimedOpenLoop(&loop, claimID); err != nil {
			return OpenLoopRunResult{WorkflowID: item.ID, OpenLoopID: loop.ID, Status: "skipped", State: item.CurrentState, Message: "failed to resolve closed workflow open loop: " + err.Error()}
		} else if !owned {
			return OpenLoopRunResult{WorkflowID: item.ID, OpenLoopID: loop.ID, Status: "skipped", State: item.CurrentState, Message: "open-loop claim was lost before resolution"}
		}
		s.decide(item.ID, "open_loop", "resolved", "workflow already completed or archived", "follow-up engine", false, "workflow-followup")
		s.audit(item.ID, "workflow.open_loop", item.CurrentState, item.CurrentState, "open loop resolved because workflow is closed", "followup_worker", "follow-up engine", item.SourceURI, "workflow-followup")
		return OpenLoopRunResult{WorkflowID: item.ID, OpenLoopID: loop.ID, Status: "resolved", State: item.CurrentState, Message: "workflow already closed"}
	}

	checklistLabel := "Resolve due open loop: " + compact(loop.WaitingFor, 160)
	checklist, err := s.repo.FindChecklist(item.ID)
	if err != nil {
		return OpenLoopRunResult{WorkflowID: item.ID, OpenLoopID: loop.ID, Status: "skipped", State: item.CurrentState, Message: "failed to inspect existing follow-up checklist steps: " + err.Error()}
	}
	if !hasChecklistLabel(checklist, checklistLabel) {
		if _, err := s.repo.CreateChecklistItem(&models.WorkflowChecklistItem{
			WorkflowID:       item.ID,
			Label:            checklistLabel,
			Status:           "open",
			Position:         950,
			RequiresApproval: item.RequiresApproval || item.RiskLevel == "high" || loop.ResponsibleParty == "Robert",
			DueAt:            loop.FollowUpAt,
		}); err != nil {
			return OpenLoopRunResult{WorkflowID: item.ID, OpenLoopID: loop.ID, Status: "skipped", State: item.CurrentState, Message: "failed to create follow-up checklist step: " + err.Error()}
		}
	}
	recommendedAction := "Follow-up due: " + firstNonEmpty(loop.NextAction, loop.WaitingFor)
	proposals, err := s.repo.FindProposals(item.ID)
	if err != nil {
		return OpenLoopRunResult{WorkflowID: item.ID, OpenLoopID: loop.ID, Status: "skipped", State: item.CurrentState, Message: "failed to inspect existing follow-up proposals: " + err.Error()}
	}
	if !hasProposalAction(proposals, recommendedAction) {
		if _, err := s.repo.CreateProposal(&models.WorkflowProposal{
			WorkflowID:        item.ID,
			RecommendedAction: recommendedAction,
			Options:           strings.Join(followUpOptions(item, loop), "\n"),
			Status:            "open",
		}); err != nil {
			return OpenLoopRunResult{WorkflowID: item.ID, OpenLoopID: loop.ID, Status: "skipped", State: item.CurrentState, Message: "failed to create follow-up proposal: " + err.Error()}
		}
	}

	from := item.CurrentState
	if item.CurrentState == StateBlocked {
		item.NextAction = "review due follow-up proposal when blocker is cleared"
		item.BlockedReason = firstNonEmpty(item.BlockedReason, "due follow-up is blocked")
	} else if item.RiskLevel == "high" || item.RequiresApproval || loop.ResponsibleParty == "Robert" {
		item.CurrentState = StateNeedsApproval
		item.RequiresApproval = true
		item.ApprovalStatus = "pending"
		item.ApprovalReason = firstNonEmpty(item.ApprovalReason, "due open loop requires Robert decision")
		item.NextAction = "review due follow-up proposal"
	} else if item.CurrentState == StateWaitingInput {
		item.CurrentState = StateReady
		item.BlockedReason = ""
		item.NextAction = "execute due follow-up through workflow worker"
	} else {
		item.NextAction = "execute due follow-up through workflow worker"
	}
	owned, renewErr := s.repo.RenewOpenLoopClaim(loop.ID, claimID, time.Now().UTC().Add(claimLeaseDuration()))
	if renewErr != nil {
		return OpenLoopRunResult{WorkflowID: item.ID, OpenLoopID: loop.ID, Status: "skipped", State: from, Message: "failed to confirm open-loop claim: " + renewErr.Error()}
	}
	if !owned {
		return OpenLoopRunResult{WorkflowID: item.ID, OpenLoopID: loop.ID, Status: "skipped", State: from, Message: "open-loop claim was lost before workflow update"}
	}
	if _, err := s.repo.UpdateItem(item); err != nil {
		return OpenLoopRunResult{WorkflowID: item.ID, OpenLoopID: loop.ID, Status: "skipped", State: from, Message: "failed to update workflow after follow-up trigger: " + err.Error()}
	}
	if from != item.CurrentState {
		s.recordTransition(item.ID, from, item.CurrentState, "followup_worker", "workflow-followup", false, "due open loop triggered next action")
	}
	loop.Status = "triggered"
	loop.ClaimID = ""
	loop.LeaseUntil = nil
	if _, owned, err := s.repo.UpdateClaimedOpenLoop(&loop, claimID); err != nil {
		return OpenLoopRunResult{WorkflowID: item.ID, OpenLoopID: loop.ID, Status: "skipped", State: item.CurrentState, Message: "failed to mark open loop triggered: " + err.Error()}
	} else if !owned {
		return OpenLoopRunResult{WorkflowID: item.ID, OpenLoopID: loop.ID, Status: "skipped", State: item.CurrentState, Message: "open-loop claim was lost before trigger completion"}
	}
	s.decide(item.ID, "open_loop", "triggered", loop.WaitingFor, "follow-up engine", false, "workflow-followup")
	s.audit(item.ID, "workflow.open_loop", from, item.CurrentState, "due open loop created proposal and checklist step", "followup_worker", "follow-up engine", item.SourceURI, "workflow-followup")
	return OpenLoopRunResult{WorkflowID: item.ID, OpenLoopID: loop.ID, Status: "triggered", State: item.CurrentState, Message: "proposal and checklist step created"}
}

func (s *service) runWorkflowItem(item models.WorkflowItem, claimID string) WorkflowRunResult {
	if item.MaxRetries <= 0 {
		item.MaxRetries = 2
	}
	if _, err := s.resolveWorkflowItemCoordinationPlan(item); err != nil {
		return s.handleRunReviewRequired(&item, claimID, "accepted coordination plan is no longer current: "+err.Error(), "needs_review")
	}
	if item.RequiresApproval && item.ApprovalStatus != "approved" {
		message := "approval is required before worker execution"
		from := item.CurrentState
		item.CurrentState = StateNeedsApproval
		item.BlockedReason = ""
		item.NextAction = "review and approve workflow before execution"
		item.ApprovalStatus = "pending"
		item.LastWorkerError = message
		updated, owned, err := s.repo.UpdateClaimedItem(&item, claimID)
		if err != nil {
			return WorkflowRunResult{WorkflowID: item.ID, Status: "blocked", State: from, Attempts: item.RetryCount, Message: err.Error()}
		}
		if !owned || updated == nil {
			return WorkflowRunResult{WorkflowID: item.ID, Status: "blocked", State: StateBlocked, Attempts: item.RetryCount, Message: "worker claim was lost before approval guard could be persisted"}
		}
		s.recordTransition(updated.ID, from, StateNeedsApproval, "worker_guard", "workflow-worker", false, message)
		s.decide(updated.ID, "worker_execution", "blocked", message, "approval guard after claim", false, "workflow-worker")
		s.audit(updated.ID, "workflow.worker_blocked", from, StateNeedsApproval, message, "worker_guard", "approval guard after claim", updated.SourceURI, "workflow-worker")
		return WorkflowRunResult{WorkflowID: item.ID, Status: "blocked", State: StateNeedsApproval, Attempts: item.RetryCount, Message: message}
	}
	if s.taskRunner == nil {
		message := "task runner is not configured"
		from := item.CurrentState
		item.CurrentState = StateBlocked
		item.BlockedReason = message
		item.NextAction = "configure task runner adapter before worker execution"
		item.LastWorkerError = message
		updated, owned, err := s.repo.UpdateClaimedItem(&item, claimID)
		if err != nil {
			return WorkflowRunResult{WorkflowID: item.ID, Status: "blocked", State: from, Attempts: item.RetryCount, Message: err.Error()}
		}
		if !owned || updated == nil {
			return WorkflowRunResult{WorkflowID: item.ID, Status: "blocked", State: StateBlocked, Attempts: item.RetryCount, Message: "worker claim was lost before missing-runner state could be persisted"}
		}
		s.recordTransition(updated.ID, from, StateBlocked, "worker", "workflow-worker", false, message)
		s.decide(updated.ID, "worker_execution", "blocked", message, "task runner dependency check", false, "workflow-worker")
		s.audit(updated.ID, "workflow.worker", from, StateBlocked, message, "worker", "task runner missing", updated.SourceURI, "workflow-worker")
		return WorkflowRunResult{WorkflowID: item.ID, Status: "blocked", State: StateBlocked, Attempts: item.RetryCount, Message: message}
	}
	observedAt := time.Now().UTC()
	if item.LastRunAt != nil {
		observedAt = *item.LastRunAt
	}
	actionDecision := autonomy.ValidateAction(autonomy.ActionEnvelope{
		InterfaceType:    autonomy.InterfaceSkillCall,
		ActionType:       "run_workflow_task",
		RequiresApproval: item.RequiresApproval,
		ApprovalRecorded: !item.RequiresApproval || item.ApprovalStatus == "approved",
		ObservationTime:  observedAt,
		StaleAfter:       observedAt.Add(worldStateTTL()),
	}, time.Now().UTC())
	if actionDecision != "allowed" {
		return s.handleRunReviewRequired(&item, claimID, "autonomy policy blocked action: "+actionDecision, "needs_review")
	}

	pursuitID, pursuitErr := s.workflowTaskPursuitID(item)
	if pursuitErr != nil {
		return s.handleRunReviewRequired(&item, claimID, "linked pursuit context could not be resolved before task execution: "+pursuitErr.Error(), "needs_review")
	}
	humanApproved := item.ApprovalStatus == "approved"
	approvalProof := workflowApprovalProof{}
	if humanApproved {
		var approvalErr error
		approvalProof, approvalErr = s.workflowApprovalProof(item)
		if approvalErr != nil {
			return s.handleRunApprovalRequired(
				&item,
				claimID,
				"approved workflow is missing durable approval provenance: "+approvalErr.Error(),
				"needs_review",
			)
		}
	}
	runResult, err := s.runTaskWithLease(item.ID, claimID, TaskRunRequest{
		OwnerIdentity:         item.OwnerIdentity,
		PursuitID:             pursuitID,
		WorkflowID:            item.ID.String(),
		Request:               item.Description,
		ProjectKey:            item.ProjectKey,
		AutomationID:          item.AutomationID,
		MandateID:             uuidPointerString(item.MandateID),
		RiskLevel:             item.RiskLevel,
		HumanApproved:         humanApproved,
		ApprovalNote:          item.ApprovalReason,
		ApprovalSourceID:      approvalProof.SourceID,
		ApprovalBindingDigest: approvalProof.BindingDigest,
		ApprovalActorIdentity: approvalProof.ActorIdentity,
		ApprovalApprovedAt:    approvalProof.ApprovedAt,
		Deadline:              item.DueAt,
		CoordinationPlan:      workflowCoordinationReference(item),
	})
	if err != nil {
		return s.handleRunFailure(&item, claimID, "task engine failed: "+err.Error(), "")
	}
	if runResult == nil {
		return s.handleRunFailure(&item, claimID, "task engine returned no result", "")
	}
	item.LastTaskPlanID = runResult.PlanID
	item.VerificationStatus = runResult.VerificationStatus
	if err := s.storeTaskFrameworkSelection(item.ID, runResult); err != nil {
		return s.handleRunReviewRequired(&item, claimID, "framework selection provenance could not be stored: "+err.Error(), "needs_review")
	}
	if time.Now().UTC().After(observedAt.Add(worldStateTTL())) {
		result := s.handleRunReviewRequired(
			&item,
			claimID,
			"world state expired during execution; re-observe source and external side effects before accepting completion",
			"needs_review",
		)
		result.FrameworkSelection = runResult.FrameworkSelection
		return result
	}
	if err := s.storeTaskRuntimeEvidence(item.ID, runResult); err != nil {
		result := s.handleRunReviewRequired(&item, claimID, "runtime evidence could not be stored: "+err.Error(), "needs_review")
		result.FrameworkSelection = runResult.FrameworkSelection
		return result
	}
	if runResult.ReviewRequired {
		reason := firstNonEmpty(runResult.FailureReason, "task engine requires human review")
		if runResult.ApprovalRequired {
			result := s.handleRunApprovalRequired(&item, claimID, reason, runResult.VerificationStatus)
			result.FrameworkSelection = runResult.FrameworkSelection
			return result
		}
		result := s.handleRunReviewRequired(&item, claimID, reason, runResult.VerificationStatus)
		result.FrameworkSelection = runResult.FrameworkSelection
		return result
	}
	gateResult := s.evaluateQualityGates(item, runResult)
	if runResult.Passed && !runResult.ReviewRequired && gateResult.Passed {
		if _, err := s.resolveWorkflowItemCoordinationPlan(item); err != nil {
			result := s.handleRunReviewRequired(&item, claimID, "coordination plan changed before completion attestation: "+err.Error(), "needs_review")
			result.FrameworkSelection = runResult.FrameworkSelection
			return result
		}
		completed := time.Now().UTC()
		item.CurrentState = StateCompleted
		if item.RecoveryStatus == RecoveryRetryConfirmed {
			item.RecoveryStatus = RecoveryCompletedAfterRetry
		}
		item.CompletedAt = &completed
		item.NextRunAt = nil
		item.LastWorkerError = ""
		item.NextAction = "write completion summary and archive when reviewed"
		attestation, err := newWorkflowCompletionAttestation(item, runResult, completed)
		if err != nil {
			return WorkflowRunResult{WorkflowID: item.ID, Status: "blocked", State: StateInProgress, Attempts: item.RetryCount, VerificationStatus: item.VerificationStatus, Message: err.Error(), FrameworkSelection: runResult.FrameworkSelection}
		}
		if _, owned, err := s.repo.CompleteClaimedItem(&item, claimID, attestation); err != nil {
			return WorkflowRunResult{WorkflowID: item.ID, Status: "blocked", State: StateInProgress, Attempts: item.RetryCount, VerificationStatus: item.VerificationStatus, Message: err.Error(), FrameworkSelection: runResult.FrameworkSelection}
		} else if !owned {
			return WorkflowRunResult{WorkflowID: item.ID, Status: "blocked", State: StateBlocked, Attempts: item.RetryCount, VerificationStatus: item.VerificationStatus, Message: "worker claim was lost before completion could be persisted", FrameworkSelection: runResult.FrameworkSelection}
		}
		s.recordTransition(item.ID, StateInProgress, StateCompleted, "worker", "workflow-worker", item.ApprovalStatus == "approved", "task engine result verified workflow completion")
		s.decide(item.ID, "verification_completion", "completed", "verification accepted task engine result", firstNonEmpty(runResult.VerificationStatus, "validation passed"), item.ApprovalStatus == "approved", "workflow-worker")
		s.audit(item.ID, "workflow.worker_completed", StateInProgress, StateCompleted, firstNonEmpty(runResult.Output, "task engine result verified workflow completion"), "worker", "verification accepted completion", item.SourceURI, "workflow-worker")
		s.markChecklistProgress(item.ID, "Verify completion before closing")
		return WorkflowRunResult{WorkflowID: item.ID, Status: "completed", State: StateCompleted, Attempts: item.RetryCount, VerificationStatus: item.VerificationStatus, Message: "verified completion", FrameworkSelection: runResult.FrameworkSelection}
	}
	reason := firstNonEmpty(runResult.FailureReason, "task engine validation did not pass")
	if !gateResult.Passed {
		reason = firstNonEmpty(strings.Join(gateResult.Failures, "; "), reason)
	}
	if runResult.ExternalActionExecuted {
		reason = "controlled runtime action executed, but completion validation failed; review evidence before any retry: " + reason
		result := s.handleRunReviewRequired(&item, claimID, reason, runResult.VerificationStatus)
		result.FrameworkSelection = runResult.FrameworkSelection
		return result
	}
	result := s.handleRunFailure(&item, claimID, reason, runResult.VerificationStatus)
	result.FrameworkSelection = runResult.FrameworkSelection
	return result
}

func uuidPointerString(value *uuid.UUID) string {
	if value == nil || *value == uuid.Nil {
		return ""
	}
	return value.String()
}

// workflowTaskPursuitID only attributes a workflow worker run when exactly
// one pursuit link belongs to the same owner. Zero owner-matching links retain
// the existing standalone workflow semantics. Multiple matches are ambiguous
// and must be reviewed instead of executing without pursuit governance.
func (s *service) workflowTaskPursuitID(item models.WorkflowItem) (string, error) {
	linked, err := s.repo.FindLinkedPursuits(item.ID)
	if err != nil {
		return "", err
	}
	ownerIdentity := strings.TrimSpace(item.OwnerIdentity)
	matched := uuid.Nil
	for _, pursuit := range linked {
		if pursuit.ID == uuid.Nil || strings.TrimSpace(pursuit.OwnerIdentity) != ownerIdentity {
			continue
		}
		if matched != uuid.Nil {
			return "", fmt.Errorf("workflow has multiple pursuits for owner %q; select exactly one pursuit before automatic execution", ownerIdentity)
		}
		matched = pursuit.ID
	}
	if matched == uuid.Nil {
		return "", nil
	}
	return matched.String(), nil
}

type workflowApprovalProof struct {
	SourceID      string
	BindingDigest string
	ActorIdentity string
	ApprovedAt    *time.Time
}

func (s *service) workflowApprovalProof(item models.WorkflowItem) (workflowApprovalProof, error) {
	decisions, err := s.repo.FindDecisions(item.ID)
	if err != nil {
		return workflowApprovalProof{}, err
	}
	for _, decision := range decisions {
		if !strings.EqualFold(strings.TrimSpace(decision.DecisionType), "approval") {
			continue
		}
		if decision.ID == uuid.Nil ||
			!decision.Approved ||
			!strings.EqualFold(strings.TrimSpace(decision.Decision), "approved") {
			return workflowApprovalProof{}, fmt.Errorf("latest workflow approval decision is not an approval")
		}
		if strings.TrimSpace(decision.Actor) == "" || strings.TrimSpace(decision.Actor) != strings.TrimSpace(item.OwnerIdentity) {
			return workflowApprovalProof{}, fmt.Errorf("latest workflow approval decision actor does not match the workflow owner")
		}
		bindingDigest := ""
		if strings.TrimSpace(item.AutomationID) != "" {
			var bindingErr error
			bindingDigest, bindingErr = workflowApprovalBindingDigest(decision.RuleApplied)
			if bindingErr != nil {
				return workflowApprovalProof{}, bindingErr
			}
		}
		approvedAt := decision.CreatedAt.UTC()
		if approvedAt.IsZero() {
			return workflowApprovalProof{}, fmt.Errorf("latest workflow approval decision has no approval time")
		}
		return workflowApprovalProof{
			SourceID:      "workflow-decision:" + decision.ID.String(),
			BindingDigest: bindingDigest,
			ActorIdentity: strings.TrimSpace(decision.Actor),
			ApprovedAt:    &approvedAt,
		}, nil
	}
	return workflowApprovalProof{}, fmt.Errorf("no approved workflow decision record exists")
}

func workflowApprovalBindingDigest(binding string) (string, error) {
	parts := strings.Split(strings.TrimSpace(binding), ":")
	if len(parts) != 3 || parts[0] != "automation-action" || strings.TrimSpace(parts[1]) == "" {
		return "", fmt.Errorf("latest workflow approval decision has no exact automation action binding")
	}
	digest := strings.ToLower(strings.TrimSpace(parts[2]))
	decoded, err := hex.DecodeString(digest)
	if err != nil || len(decoded) != sha256.Size {
		return "", fmt.Errorf("latest workflow approval decision has an invalid automation action binding")
	}
	return digest, nil
}

func normalizeFrameworkSelection(selection FrameworkSelectionProvenance) FrameworkSelectionProvenance {
	selection.SelectionDecisionID = strings.TrimSpace(selection.SelectionDecisionID)
	selection.TaskPlanID = strings.TrimSpace(selection.TaskPlanID)
	selection.CatalogVersion = strings.TrimSpace(selection.CatalogVersion)
	selection.CatalogDigest = strings.ToLower(strings.TrimSpace(selection.CatalogDigest))
	selection.SelectorAlgorithmVersion = strings.TrimSpace(selection.SelectorAlgorithmVersion)
	selection.TaskRiskLevel = strings.ToLower(strings.TrimSpace(selection.TaskRiskLevel))
	selection.EffectiveRiskCeiling = strings.ToLower(strings.TrimSpace(selection.EffectiveRiskCeiling))
	selection.EffectivePreferenceDigest = strings.ToLower(strings.TrimSpace(selection.EffectivePreferenceDigest))
	selection.ConstitutionDigest = strings.ToLower(strings.TrimSpace(selection.ConstitutionDigest))
	selection.ConstitutionSource = strings.TrimSpace(selection.ConstitutionSource)
	selection.OperatingContractDigest = strings.ToLower(strings.TrimSpace(selection.OperatingContractDigest))
	return selection
}

func frameworkSelectionRule(selection FrameworkSelectionProvenance) string {
	rule := fmt.Sprintf(
		"catalog=%s selector=%s constitution_version=%d constitution_source=%s operating_contract=%s",
		selection.CatalogVersion,
		selection.SelectorAlgorithmVersion,
		selection.ConstitutionVersion,
		selection.ConstitutionSource,
		selection.OperatingContractDigest,
	)
	if strings.EqualFold(selection.SelectorAlgorithmVersion, "selector-v5") {
		rule += fmt.Sprintf(" task_risk=%s effective_risk_ceiling=%s", selection.TaskRiskLevel, selection.EffectiveRiskCeiling)
	}
	return rule
}

func decodeFrameworkSelectionDecision(decision models.WorkflowDecision) (FrameworkSelectionProvenance, error) {
	payload := strings.TrimSpace(decision.Reason)
	if payload == "" && strings.HasPrefix(strings.TrimSpace(decision.RuleApplied), "{") {
		payload = strings.TrimSpace(decision.RuleApplied)
	}
	if payload == "" {
		return FrameworkSelectionProvenance{}, fmt.Errorf("framework selection decision payload is empty")
	}
	var selection FrameworkSelectionProvenance
	if err := json.Unmarshal([]byte(payload), &selection); err != nil {
		return FrameworkSelectionProvenance{}, fmt.Errorf("decode framework selection decision: %w", err)
	}
	selection = normalizeFrameworkSelection(selection)
	if strings.TrimSpace(decision.Decision) != "" &&
		selection.SelectionDecisionID != strings.TrimSpace(decision.Decision) {
		return FrameworkSelectionProvenance{}, fmt.Errorf("framework selection decision identity does not match its payload")
	}
	if err := selection.Validate(selection.TaskPlanID); err != nil {
		return FrameworkSelectionProvenance{}, err
	}
	return selection, nil
}

func frameworkSelectionsFromDecisions(decisions []models.WorkflowDecision) []FrameworkSelectionProvenance {
	selections := make([]FrameworkSelectionProvenance, 0)
	seen := make(map[string]struct{})
	for _, decision := range decisions {
		if decision.DecisionType != frameworkSelectionDecisionType {
			continue
		}
		selection, err := decodeFrameworkSelectionDecision(decision)
		if err != nil {
			continue
		}
		if _, ok := seen[selection.SelectionDecisionID]; ok {
			continue
		}
		seen[selection.SelectionDecisionID] = struct{}{}
		selections = append(selections, selection)
	}
	return selections
}

func (s *service) storeTaskFrameworkSelection(workflowID uuid.UUID, result *TaskRunResult) error {
	if result == nil {
		return fmt.Errorf("task engine returned no framework selection result")
	}
	if result.FrameworkSelection == nil {
		return fmt.Errorf("task plan %q has no framework selection provenance", strings.TrimSpace(result.PlanID))
	}
	selection := normalizeFrameworkSelection(*result.FrameworkSelection)
	if err := selection.Validate(strings.TrimSpace(result.PlanID)); err != nil {
		return err
	}
	payload, err := json.Marshal(selection)
	if err != nil {
		return fmt.Errorf("encode framework selection provenance: %w", err)
	}
	payloadText := string(payload)
	rule := frameworkSelectionRule(selection)
	sourceURI := "framework-selection://" + selection.SelectionDecisionID

	decisions, err := s.repo.FindDecisions(workflowID)
	if err != nil {
		return err
	}
	decisionExists := false
	for _, decision := range decisions {
		if decision.DecisionType != frameworkSelectionDecisionType ||
			strings.TrimSpace(decision.Decision) != selection.SelectionDecisionID {
			continue
		}
		existing, decodeErr := decodeFrameworkSelectionDecision(decision)
		if decodeErr != nil || existing != selection {
			return fmt.Errorf("framework selection decision conflicts with existing provenance")
		}
		decisionExists = true
		break
	}
	if !decisionExists {
		if _, err := s.repo.CreateDecision(&models.WorkflowDecision{
			WorkflowID:   workflowID,
			DecisionType: frameworkSelectionDecisionType,
			Decision:     selection.SelectionDecisionID,
			Reason:       payloadText,
			RuleApplied:  rule,
			Approved:     true,
			Actor:        "workflow-worker",
		}); err != nil {
			return fmt.Errorf("store framework selection decision: %w", err)
		}
	}

	events, err := s.repo.FindEvents(workflowID)
	if err != nil {
		return err
	}
	eventExists := false
	for _, event := range events {
		if event.EventType != frameworkSelectionEventType ||
			strings.TrimSpace(event.SourceURI) != sourceURI {
			continue
		}
		if strings.TrimSpace(event.Message) != payloadText ||
			strings.TrimSpace(event.RuleApplied) != rule {
			return fmt.Errorf("framework selection event conflicts with existing provenance")
		}
		eventExists = true
		break
	}
	if !eventExists {
		if _, err := s.repo.CreateEvent(&models.WorkflowEvent{
			WorkflowID:  workflowID,
			EventType:   frameworkSelectionEventType,
			Message:     payloadText,
			Trigger:     "task_engine_framework_selection",
			RuleApplied: rule,
			SourceURI:   sourceURI,
			Actor:       "workflow-worker",
		}); err != nil {
			return fmt.Errorf("store framework selection event: %w", err)
		}
	}
	*result.FrameworkSelection = selection
	return nil
}

func (s *service) storeTaskRuntimeEvidence(workflowID uuid.UUID, result *TaskRunResult) error {
	if result == nil || strings.TrimSpace(result.RuntimeEvidenceURI) == "" {
		return nil
	}
	sourceURI := strings.TrimSpace(result.RuntimeEvidenceURI)
	existing, err := s.repo.FindEvidenceClaims(workflowID)
	if err != nil {
		return err
	}
	for _, claim := range existing {
		if strings.EqualFold(strings.TrimSpace(claim.SourceURI), sourceURI) {
			return nil
		}
	}
	status := firstNonEmpty(result.VerificationStatus, "needs_review")
	claimText := firstNonEmpty(result.RuntimeEvidenceLabel, "Controlled runtime execution evidence")
	if result.Output != "" {
		claimText = claimText + ": " + compact(result.Output, 240)
	}
	if routeSummary := runtimeRouteTraceEvidenceSummary(result.RuntimeRouteTrace); routeSummary != "" {
		claimText = claimText + " | " + routeSummary
	}
	_, err = s.repo.CreateEvidenceClaim(&models.WorkflowEvidenceClaim{
		WorkflowID:  workflowID,
		ClaimText:   claimText,
		SourceURI:   sourceURI,
		SourceLabel: firstNonEmpty(result.RuntimeEvidenceLabel, "Controlled runtime launch"),
		Reliability: "controlled_runtime",
		Status:      status,
	})
	return err
}

func runtimeRouteTraceEvidenceSummary(trace *models.AutomationRuntimeRouteTrace) string {
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
	if value := compactTraceList("skills", trace.RecommendedSkills, 3); value != "" {
		parts = append(parts, value)
	}
	if value := compactTraceList("maps", trace.RelevantMaps, 2); value != "" {
		parts = append(parts, value)
	}
	if value := compactTraceList("blocked", trace.BlockedSurfaces, 3); value != "" {
		parts = append(parts, value)
	}
	if len(parts) == 0 {
		return ""
	}
	return "route: " + strings.Join(parts, "; ")
}

func compactTraceList(label string, values []string, limit int) string {
	cleaned := []string{}
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

func (s *service) runTaskSafely(request TaskRunRequest) (result *TaskRunResult, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			result = nil
			err = fmt.Errorf("task runner panic recovered: %v", recovered)
		}
	}()
	return s.taskRunner.RunWorkflowTask(request)
}

func (s *service) runTaskWithLease(itemID uuid.UUID, claimID string, request TaskRunRequest) (*TaskRunResult, error) {
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		interval := claimLeaseDuration() / 3
		if interval < 15*time.Second {
			interval = 15 * time.Second
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				leaseUntil := time.Now().UTC().Add(claimLeaseDuration())
				owned, err := s.repo.RenewRunnableItemClaim(itemID, claimID, leaseUntil)
				if err != nil || !owned {
					return
				}
			}
		}
	}()
	result, err := s.runTaskSafely(request)
	close(stop)
	<-done
	if err != nil {
		return nil, err
	}
	owned, renewErr := s.repo.RenewRunnableItemClaim(itemID, claimID, time.Now().UTC().Add(claimLeaseDuration()))
	if renewErr != nil {
		return nil, fmt.Errorf("failed to confirm worker claim after task execution: %w", renewErr)
	}
	if !owned {
		return nil, fmt.Errorf("worker claim was lost during task execution")
	}
	return result, nil
}

func (s *service) handleRunFailure(item *models.WorkflowItem, claimID, reason, verificationStatus string) WorkflowRunResult {
	if verificationStatus == "" || containsAny(strings.ToLower(verificationStatus), "fail", "needs_review", "unsupported", "blocked") {
		s.markQualityGate(item.ID, "verification before completion", "failed", firstNonEmpty(reason, "worker validation failed"))
	}
	item.RetryCount++
	item.VerificationStatus = verificationStatus
	item.LastWorkerError = reason
	attempts := item.RetryCount
	if attempts < item.MaxRetries {
		next := time.Now().UTC().Add(retryBackoff(attempts))
		item.CurrentState = StateReady
		item.NextRunAt = &next
		item.NextAction = "retry scheduled after worker validation failure"
		if _, owned, err := s.repo.UpdateClaimedItem(item, claimID); err != nil {
			return WorkflowRunResult{WorkflowID: item.ID, Status: "blocked", State: StateInProgress, Attempts: attempts, VerificationStatus: verificationStatus, Message: err.Error()}
		} else if !owned {
			return WorkflowRunResult{WorkflowID: item.ID, Status: "blocked", State: StateBlocked, Attempts: attempts, VerificationStatus: verificationStatus, Message: "worker claim was lost before retry could be scheduled"}
		}
		s.recordTransition(item.ID, StateInProgress, StateReady, "worker_retry", "workflow-worker", item.ApprovalStatus == "approved", reason)
		s.decide(item.ID, "retry", "scheduled", reason, fmt.Sprintf("retry %d of %d", attempts, item.MaxRetries), false, "workflow-worker")
		s.audit(item.ID, "workflow.worker_retry", StateInProgress, StateReady, reason, "worker_retry", "retry scheduled with durable counter", item.SourceURI, "workflow-worker")
		return WorkflowRunResult{WorkflowID: item.ID, Status: "retry_scheduled", State: StateReady, Attempts: attempts, VerificationStatus: verificationStatus, NextRunAt: item.NextRunAt, Message: reason}
	}
	item.CurrentState = StateBlocked
	item.BlockedReason = reason
	item.NextAction = "human review required after retry limit"
	item.NextRunAt = nil
	if _, owned, err := s.repo.UpdateClaimedItem(item, claimID); err != nil {
		return WorkflowRunResult{WorkflowID: item.ID, Status: "blocked", State: StateInProgress, Attempts: attempts, VerificationStatus: verificationStatus, Message: err.Error()}
	} else if !owned {
		return WorkflowRunResult{WorkflowID: item.ID, Status: "blocked", State: StateBlocked, Attempts: attempts, VerificationStatus: verificationStatus, Message: "worker claim was lost before failure could be persisted"}
	}
	s.recordTransition(item.ID, StateInProgress, StateBlocked, "worker_retry_exhausted", "workflow-worker", item.ApprovalStatus == "approved", reason)
	s.decide(item.ID, "retry", "exhausted", reason, fmt.Sprintf("retry limit reached at %d attempts", attempts), false, "workflow-worker")
	s.audit(item.ID, "workflow.worker_blocked", StateInProgress, StateBlocked, reason, "worker_retry_exhausted", "retry limit reached", item.SourceURI, "workflow-worker")
	return WorkflowRunResult{WorkflowID: item.ID, Status: "blocked", State: StateBlocked, Attempts: attempts, VerificationStatus: verificationStatus, Message: reason}
}

type qualityGateRunResult struct {
	Passed   bool
	Failures []string
}

func (s *service) handleRunApprovalRequired(item *models.WorkflowItem, claimID, reason, verificationStatus string) WorkflowRunResult {
	item.CurrentState = StateNeedsApproval
	item.RequiresApproval = true
	item.ApprovalStatus = "pending"
	item.ApprovalReason = firstNonEmpty(reason, "task execution requires explicit human approval")
	item.BlockedReason = ""
	item.NextAction = "review the exact proposed action and approve before execution"
	item.NextRunAt = nil
	item.LastWorkerError = item.ApprovalReason
	item.VerificationStatus = verificationStatus
	s.markQualityGate(item.ID, "human approval", "needs_review", item.ApprovalReason)
	updated, owned, err := s.repo.UpdateClaimedItem(item, claimID)
	if err != nil {
		return WorkflowRunResult{
			WorkflowID:         item.ID,
			Status:             "blocked",
			State:              StateInProgress,
			Attempts:           item.RetryCount,
			VerificationStatus: verificationStatus,
			Message:            err.Error(),
		}
	}
	if !owned || updated == nil {
		return WorkflowRunResult{
			WorkflowID:         item.ID,
			Status:             "blocked",
			State:              StateBlocked,
			Attempts:           item.RetryCount,
			VerificationStatus: verificationStatus,
			Message:            "worker claim was lost before approval-required state could be persisted",
		}
	}
	s.recordTransition(item.ID, StateInProgress, StateNeedsApproval, "worker_approval_required", "workflow-worker", false, item.ApprovalReason)
	s.decide(item.ID, "worker_execution", "needs_approval", item.ApprovalReason, "exact action requires a durable human approval decision", false, "workflow-worker")
	s.audit(item.ID, "workflow.worker_approval_required", StateInProgress, StateNeedsApproval, item.ApprovalReason, "worker_approval_required", "approval gate blocks execution", item.SourceURI, "workflow-worker")
	return WorkflowRunResult{
		WorkflowID:         item.ID,
		Status:             "blocked",
		State:              StateNeedsApproval,
		Attempts:           item.RetryCount,
		VerificationStatus: verificationStatus,
		Message:            item.ApprovalReason,
	}
}

func (s *service) handleRunReviewRequired(item *models.WorkflowItem, claimID, reason, verificationStatus string) WorkflowRunResult {
	item.RetryCount++
	item.CurrentState = StateBlocked
	item.BlockedReason = firstNonEmpty(reason, "task engine requires human review")
	item.NextAction = "review task execution evidence and resolve the blocker before retrying"
	item.NextRunAt = nil
	item.LastWorkerError = item.BlockedReason
	item.VerificationStatus = verificationStatus
	s.markQualityGate(item.ID, "verification before completion", "needs_review", item.BlockedReason)
	updated, owned, err := s.repo.UpdateClaimedItem(item, claimID)
	if err != nil {
		return WorkflowRunResult{WorkflowID: item.ID, Status: "blocked", State: StateInProgress, Attempts: item.RetryCount, VerificationStatus: verificationStatus, Message: err.Error()}
	}
	if !owned || updated == nil {
		return WorkflowRunResult{WorkflowID: item.ID, Status: "blocked", State: StateBlocked, Attempts: item.RetryCount, VerificationStatus: verificationStatus, Message: "worker claim was lost before review-required state could be persisted"}
	}
	s.recordTransition(item.ID, StateInProgress, StateBlocked, "worker_review_required", "workflow-worker", item.ApprovalStatus == "approved", item.BlockedReason)
	s.decide(item.ID, "worker_execution", "needs_review", item.BlockedReason, "task engine explicitly required human review", false, "workflow-worker")
	s.audit(item.ID, "workflow.worker_review_required", StateInProgress, StateBlocked, item.BlockedReason, "worker_review_required", "non-retryable review gate", item.SourceURI, "workflow-worker")
	return WorkflowRunResult{WorkflowID: item.ID, Status: "blocked", State: StateBlocked, Attempts: item.RetryCount, VerificationStatus: verificationStatus, Message: item.BlockedReason}
}

func (s *service) evaluateQualityGates(item models.WorkflowItem, runResult *TaskRunResult) qualityGateRunResult {
	result := qualityGateRunResult{Passed: true}
	gates, err := s.repo.FindQualityGates(item.ID)
	if err != nil {
		return result
	}
	sourceLinks, _ := s.repo.FindSourceLinks(item.ID)
	evidence, _ := s.repo.FindEvidenceClaims(item.ID)
	githubEvidence := collectGitHubQualityEvidence(item, sourceLinks, evidence)

	for _, gate := range gates {
		status := "passed"
		reason := "quality gate satisfied"
		mandatory := false
		switch strings.ToLower(gate.Gate) {
		case "source provenance":
			if item.SourceURI == "" && len(sourceLinks) == 0 {
				status = "needs_review"
				reason = "no source link is attached; acceptable for manual low-risk intake but not source-grounded work"
			}
		case "verification before completion":
			mandatory = true
			if !runResult.Passed || runResult.ReviewRequired {
				status = "failed"
				reason = firstNonEmpty(runResult.FailureReason, "task result was not verified")
			} else {
				reason = firstNonEmpty(runResult.VerificationStatus, "task validation passed")
			}
		case "human approval":
			mandatory = item.RequiresApproval
			if item.RequiresApproval && item.ApprovalStatus != "approved" {
				status = "needs_review"
				reason = "human approval has not been recorded"
			} else {
				reason = "approval not required or already recorded"
			}
		case "evidence-linked claims":
			mandatory = len(evidence) > 0
			if hasEvidenceNeedingReview(evidence) {
				status = "needs_review"
				reason = "one or more extracted claims lack usable source support"
			} else if len(evidence) == 0 {
				status = "needs_review"
				reason = "no extracted claims were found for this workflow"
			} else {
				reason = "all extracted evidence claims are source-linked"
			}
		case "github commit exists":
			mandatory = item.TaskType == "technical"
			if !githubEvidence.Commit {
				status = "needs_review"
				reason = "no source-linked GitHub commit record is attached"
			} else {
				reason = "source-linked GitHub commit record is attached"
			}
		case "tests or build evidence":
			mandatory = item.TaskType == "technical"
			if !githubEvidence.WorkflowSuccess {
				status = "needs_review"
				reason = "no source-linked successful GitHub Actions run is attached"
			} else {
				reason = "source-linked successful GitHub Actions run is attached"
			}
		case "readme/setup updated":
			mandatory = item.TaskType == "technical"
			if !githubEvidence.DocsChanged {
				status = "needs_review"
				reason = "no source-linked GitHub evidence identifies README, setup, or documentation changes"
			} else {
				reason = "source-linked GitHub evidence identifies README, setup, or documentation changes"
			}
		case "windows 11 operational path":
			mandatory = item.TaskType == "technical"
			if !githubEvidence.WindowsValidation {
				status = "needs_review"
				reason = "no source-linked controlled-runtime evidence confirms the Windows 11 operational path"
			} else {
				reason = "source-linked controlled-runtime evidence confirms the Windows 11 operational path"
			}
		}
		gate.Status = status
		gate.Reason = reason
		_, _ = s.repo.UpdateQualityGate(&gate)
		if mandatory && status != "passed" {
			result.Passed = false
			result.Failures = append(result.Failures, gate.Gate+": "+reason)
		}
	}
	if result.Passed {
		s.decide(item.ID, "quality_gates", "passed", "mandatory quality gates passed", "completion engine", item.ApprovalStatus == "approved", "workflow-worker")
	} else {
		s.decide(item.ID, "quality_gates", "needs_review", strings.Join(result.Failures, "; "), "completion engine", false, "workflow-worker")
	}
	return result
}

type githubQualityEvidence struct {
	Commit            bool
	WorkflowSuccess   bool
	DocsChanged       bool
	WindowsValidation bool
}

// collectGitHubQualityEvidence keeps technical completion grounded in durable
// source records. Worker prose is intentionally excluded: a model or runtime
// saying that a commit or build exists is not proof that it does.
func collectGitHubQualityEvidence(item models.WorkflowItem, links []models.WorkflowSourceLink, claims []models.WorkflowEvidenceClaim) githubQualityEvidence {
	evidence := githubQualityEvidence{}
	type sourceDescriptor struct {
		uri   string
		label string
	}
	sources := []sourceDescriptor{{uri: item.SourceURI, label: strings.Join([]string{item.SourceLabel, item.Title, item.Description}, " ")}}
	uris := []string{item.SourceURI}
	for _, link := range links {
		uris = append(uris, link.SourceURI)
		sources = append(sources, sourceDescriptor{uri: link.SourceURI, label: link.SourceLabel})
	}
	for _, claim := range claims {
		uris = append(uris, claim.SourceURI)
		sources = append(sources, sourceDescriptor{uri: claim.SourceURI, label: strings.Join([]string{claim.ClaimText, claim.SourceLabel}, " ")})
	}
	for _, uri := range uris {
		if isGitHubCommitURI(uri) {
			evidence.Commit = true
		}
	}
	for _, source := range sources {
		if isGitHubURI(source.uri) && containsAny(strings.ToLower(source.label), "readme", "setup", "documentation", "docs") {
			evidence.DocsChanged = true
		}
	}
	for _, claim := range claims {
		text := strings.ToLower(strings.Join([]string{claim.ClaimText, claim.SourceLabel}, " "))
		if isGitHubActionsURI(claim.SourceURI) && githubWorkflowSucceeded(text) {
			evidence.WorkflowSuccess = true
		}
		if claim.Reliability == "controlled_runtime" && containsAny(text, "windows 11", "docker compose", "windows") && containsAny(text, "passed", "validated", "completed", "success") {
			evidence.WindowsValidation = true
		}
	}
	return evidence
}

func isGitHubURI(uri string) bool {
	uri = strings.ToLower(strings.TrimSpace(uri))
	return strings.Contains(uri, "://github.com/") || strings.Contains(uri, "://api.github.com/")
}

func isGitHubCommitURI(uri string) bool {
	return isGitHubURI(uri) && strings.Contains(strings.ToLower(uri), "/commit/")
}

func isGitHubActionsURI(uri string) bool {
	return isGitHubURI(uri) && strings.Contains(strings.ToLower(uri), "/actions/runs/")
}

func githubWorkflowSucceeded(text string) bool {
	if containsAny(text, "failure", "failed", "cancelled", "canceled", "timed_out", "action_required") {
		return false
	}
	return containsAny(text, "success", "successful", "passed", "completed")
}

func (s *service) markQualityGate(workflowID uuid.UUID, gateName, status, reason string) {
	gates, err := s.repo.FindQualityGates(workflowID)
	if err != nil {
		return
	}
	for _, gate := range gates {
		if !strings.EqualFold(gate.Gate, gateName) {
			continue
		}
		gate.Status = status
		gate.Reason = reason
		_, _ = s.repo.UpdateQualityGate(&gate)
		return
	}
}

func (s *service) requireQualityGate(workflowID uuid.UUID, gateName, status, reason string) error {
	gates, err := s.repo.FindQualityGates(workflowID)
	if err != nil {
		return fmt.Errorf("load quality gates: %w", err)
	}
	for _, gate := range gates {
		if !strings.EqualFold(gate.Gate, gateName) {
			continue
		}
		gate.Status = status
		gate.Reason = reason
		if _, err := s.repo.UpdateQualityGate(&gate); err != nil {
			return fmt.Errorf("update quality gate: %w", err)
		}
		return nil
	}
	if _, err := s.repo.CreateQualityGate(&models.WorkflowQualityGate{
		WorkflowID: workflowID,
		Gate:       gateName,
		Status:     status,
		Reason:     reason,
	}); err != nil {
		return fmt.Errorf("create quality gate: %w", err)
	}
	return nil
}

func (s *service) Overview() Overview {
	rules, err := s.repo.FindRules()
	if err != nil || len(rules) == 0 {
		rules = defaultWorkflowRules()
	}
	return Overview{
		States: []string{StateNewInput, StateClassified, StateLinked, StateChecklistGenerated, StateWaitingInput, StateNeedsApproval, StateReady, StateInProgress, StateCompleted, StateArchived, StateBlocked},
		SafetyRules: []string{
			"legal, government, insurance, lawyer, financial, account-change, deletion, and public-posting workflows require approval",
			"low-risk administrative checklist generation may run automatically",
			"workflow worker retries are capped and failed items are blocked for review",
			"interrupted execution cannot retry or complete until an operator resolves unknown side effects",
			"blocked workflows must record a reason and next action",
			"completion requires checklist and verification evidence before archive",
		},
		Capabilities: engineCapabilities(),
		Rules:        rules,
	}
}

func (s *service) audit(workflowID uuid.UUID, eventType, from, to, message, trigger, rule, sourceURI, actor string) {
	_, _ = s.repo.CreateEvent(&models.WorkflowEvent{
		WorkflowID:  workflowID,
		EventType:   eventType,
		FromState:   from,
		ToState:     to,
		Message:     message,
		Trigger:     trigger,
		RuleApplied: rule,
		SourceURI:   sourceURI,
		Actor:       actor,
	})
}

func (s *service) recordTransition(workflowID uuid.UUID, from, to, trigger, actor string, approved bool, reason string) {
	_, err := s.repo.CreateTransition(&models.WorkflowTransition{
		WorkflowID: workflowID,
		FromState:  from,
		ToState:    to,
		Trigger:    trigger,
		Actor:      actor,
		Approved:   approved,
		Reason:     reason,
	})
	if err != nil {
		return
	}
	if err := s.projectWorkflowToLifeGraph(context.Background(), workflowID); err != nil {
		_, _ = s.repo.CreateEvent(&models.WorkflowEvent{
			WorkflowID:  workflowID,
			EventType:   "workflow.life_graph_projection_failed",
			FromState:   from,
			ToState:     to,
			Message:     compactWorkflowProjectionText(safety.RedactSecrets(err.Error()), 300),
			Trigger:     trigger,
			RuleApplied: "operational life graph is advisory and cannot roll back durable workflow state",
			Actor:       "workflow-engine",
		})
	} else if s.lifeOntologyProjector != nil {
		_, _ = s.repo.CreateEvent(&models.WorkflowEvent{
			WorkflowID:  workflowID,
			EventType:   "workflow.life_graph_projected",
			FromState:   from,
			ToState:     to,
			Message:     "durable workflow state projected into the advisory owner-scoped life graph",
			Trigger:     trigger,
			RuleApplied: "graph presence grants no approval or execution authority",
			Actor:       "workflow-engine",
		})
	}
}

func (s *service) linkSource(workflowID uuid.UUID, sourceType, sourceID, sourceURI, sourceLabel, relationship string) {
	_, _ = s.repo.CreateSourceLink(&models.WorkflowSourceLink{
		WorkflowID:   workflowID,
		SourceType:   sourceType,
		SourceID:     sourceID,
		SourceURI:    sourceURI,
		SourceLabel:  sourceLabel,
		Relationship: firstNonEmpty(relationship, "related"),
	})
}

func (s *service) decide(workflowID uuid.UUID, decisionType, decision, reason, rule string, approved bool, actor string) {
	_, _ = s.repo.CreateDecision(&models.WorkflowDecision{
		WorkflowID:   workflowID,
		DecisionType: decisionType,
		Decision:     decision,
		Reason:       reason,
		RuleApplied:  rule,
		Approved:     approved,
		Actor:        actor,
	})
}

func (s *service) markChecklistProgress(workflowID uuid.UUID, contains string) {
	checklist, err := s.repo.FindChecklist(workflowID)
	if err != nil {
		return
	}
	needle := strings.ToLower(contains)
	for _, item := range checklist {
		if item.Status == "done" || !strings.Contains(strings.ToLower(item.Label), needle) {
			continue
		}
		item.Status = "done"
		_, _ = s.repo.UpdateChecklistItem(&item)
		s.audit(workflowID, "workflow.checklist", "", "", "checklist item marked done: "+item.Label, "worker_completion", "verification completed", "", "workflow-worker")
		return
	}
}

type inputAnalysis struct {
	title             string
	taskType          string
	projectKey        string
	projectConfidence float64
	matchReasons      []string
	trelloRef         string
	driveRef          string
	riskLevel         string
	priority          int
	confidence        float64
	autonomyLevel     string
	requiresApproval  bool
	approvalReason    string
	blockedReason     string
	nextAction        string
	initialState      string
	dueAt             *time.Time
	entities          []string
	ruleApplied       string
}

type checklistTemplate struct {
	label            string
	requiresApproval bool
}

func analyzeInput(request IntakeRequest) inputAnalysis {
	text := strings.ToLower(request.Input)
	taskType := classifyType(text)
	risk := riskLevel(text, taskType)
	requiresApproval, approvalReason := approvalNeed(text, taskType)
	priority := priorityScore(text, taskType, risk)
	title := compactTitle(request.Input)
	dueAt := detectDueDate(request.Input)
	projectKey, projectConfidence, matchReasons, trelloRef, driveRef := matchProject(request, text, taskType)
	entities := extractEntities(request.Input)
	state := StateReady
	next := "execute allowed low-risk steps through workflow worker"
	blocked := ""
	autonomy := "autonomous_safe"
	if requiresApproval {
		state = StateNeedsApproval
		next = "wait for Robert approval before execution"
		autonomy = "approve_before_execute"
	}
	if containsAny(text, "missing", "unknown", "need access", "login credentials", "cannot access") {
		state = StateBlocked
		blocked = "missing information or access"
		next = "ask one clear question or request access"
	}
	confidence := 0.72
	if containsAny(text, "maybe", "possibly", "unclear") {
		confidence = 0.48
		state = StateWaitingInput
		next = "request clarification before execution"
	}
	return inputAnalysis{
		title:             title,
		taskType:          taskType,
		projectKey:        projectKey,
		projectConfidence: projectConfidence,
		matchReasons:      matchReasons,
		trelloRef:         trelloRef,
		driveRef:          driveRef,
		riskLevel:         risk,
		priority:          priority,
		confidence:        confidence,
		autonomyLevel:     autonomy,
		requiresApproval:  requiresApproval,
		approvalReason:    approvalReason,
		blockedReason:     blocked,
		nextAction:        next,
		initialState:      state,
		dueAt:             dueAt,
		entities:          entities,
		ruleApplied:       "workflow suggestions applied: state machine, trigger handling, adapters, memory context, decision rules, AI reasoning, checklist, priority, escalation, audit, approvals, workers, feedback, safety",
	}
}

func classifyType(text string) string {
	switch {
	case containsAny(text, "lawyer", "legal", "government", "insurance", "court", "hearing", "vivare"):
		return "legal"
	case containsAny(text, "invoice", "payment", "quote", "bank", "tax", "financial"):
		return "financial"
	case containsAny(text, "trello", "card", "checklist", "board"):
		return "project_board"
	case containsAny(text, "medium", "article", "publish", "post", "blog"):
		return "publishing"
	case containsAny(text, "github", "repo", "code", "build", "test", "docker"):
		return "technical"
	case containsAny(text, "calendar", "appointment", "meeting", "deadline"):
		return "scheduling"
	default:
		return "administrative"
	}
}

func riskLevel(text, taskType string) string {
	if taskType == "legal" || taskType == "financial" || containsAny(text, "delete", "publish", "send email", "public posting", "account change") {
		return "high"
	}
	if taskType == "technical" || taskType == "publishing" {
		return "medium"
	}
	return "low"
}

func approvalNeed(text, taskType string) (bool, string) {
	if taskType == "legal" {
		return true, "legal/government/insurance/lawyer workflow"
	}
	if taskType == "financial" {
		return true, "financial commitment or payment workflow"
	}
	if containsAny(text, "publish", "public posting", "send email", "delete", "account change", "government") {
		return true, "sensitive external or destructive action"
	}
	return false, ""
}

func priorityScore(text, taskType, risk string) int {
	score := 35
	if risk == "high" {
		score += 35
	}
	if risk == "medium" {
		score += 15
	}
	if containsAny(text, "today", "tomorrow", "urgent", "deadline", "hearing") {
		score += 25
	}
	if taskType == "legal" || taskType == "financial" {
		score += 10
	}
	return minInt(score, 100)
}

func detectDueDate(text string) *time.Time {
	now := time.Now().UTC()
	lower := strings.ToLower(text)
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(strings.ToLower(trimmed), "start:") {
			continue
		}
		if parsed, ok := parseWorkflowDate(strings.TrimSpace(trimmed[len("start:"):])); ok {
			return &parsed
		}
	}
	if strings.Contains(lower, "tomorrow") {
		due := now.Add(24 * time.Hour)
		return &due
	}
	if strings.Contains(lower, "today") || strings.Contains(lower, "urgent") {
		due := now
		return &due
	}
	for _, token := range strings.Fields(text) {
		if parsed, ok := parseWorkflowDate(strings.Trim(token, " ,.;()[]")); ok {
			return &parsed
		}
	}
	return nil
}

func parseWorkflowDate(value string) (time.Time, bool) {
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02"} {
		parsed, err := time.Parse(layout, strings.TrimSpace(value))
		if err == nil {
			return parsed.UTC(), true
		}
	}
	return time.Time{}, false
}

func matchProject(request IntakeRequest, text, taskType string) (string, float64, []string, string, string) {
	if strings.TrimSpace(request.ProjectKey) != "" {
		return strings.TrimSpace(request.ProjectKey), 0.95, []string{"explicit project key"}, "", driveRefForProject(request.ProjectKey)
	}
	switch {
	case containsAny(text, "vivare", "hearing", "heat pump", "housing association"):
		return "Vivare dispute", 0.88, []string{"keyword: vivare", "legal/dispute terms"}, "Vivare - hearing preparation", "Legal/Vivare"
	case containsAny(text, "asr", "burglary", "claim", "policy number", "damage number"):
		return "ASR burglary claim", 0.84, []string{"insurance claim terms"}, "ASR - claim documents", "Insurance/ASR"
	case containsAny(text, "sharet"):
		return "ShareT development", 0.82, []string{"project name: ShareT"}, "ShareT - development", "Projects/ShareT"
	case containsAny(text, "laro"):
		return "LARO development", 0.8, []string{"project name: LARO"}, "LARO - development", "Projects/LARO"
	case taskType == "publishing" || containsAny(text, "medium", "blog", "article"):
		return "Medium publishing", 0.7, []string{"publishing workflow terms"}, "Medium - draft pipeline", "Content/Medium"
	case taskType == "technical" && containsAny(text, "github", "developer", "feature", "branch", "commit"):
		return "Software development", 0.68, []string{"software/developer terms"}, "Development - review queue", "Projects/Software"
	default:
		return "", 0, []string{"no confident project match"}, "", ""
	}
}

func evidenceClaimsForInput(workflowID uuid.UUID, input string, request IntakeRequest) []models.WorkflowEvidenceClaim {
	lower := strings.ToLower(input)
	claims := []models.WorkflowEvidenceClaim{}
	if !containsAny(lower, "said", "claims", "sent", "received", "approved", "rejected", "deadline", "hearing", "invoice", "contract") {
		return claims
	}
	for _, sentence := range splitSentences(input) {
		if !containsAny(strings.ToLower(sentence), "said", "claims", "sent", "received", "approved", "rejected", "deadline", "hearing", "invoice", "contract") {
			continue
		}
		claims = append(claims, models.WorkflowEvidenceClaim{
			WorkflowID:  workflowID,
			ClaimText:   compact(sentence, 360),
			SourceURI:   request.SourceURI,
			SourceLabel: request.SourceLabel,
			Reliability: reliabilityForSource(request.SourceType),
			Status:      "source_linked",
			NeedsReview: request.SourceURI == "",
		})
		if len(claims) >= 8 {
			break
		}
	}
	return claims
}

func openLoopForAnalysis(workflowID uuid.UUID, analysis inputAnalysis) *models.WorkflowOpenLoop {
	text := strings.ToLower(analysis.title + " " + analysis.nextAction + " " + analysis.blockedReason)
	responsible := "Robert"
	waitingFor := ""
	next := analysis.nextAction
	switch {
	case analysis.initialState == StateNeedsApproval:
		responsible = "Robert"
		waitingFor = "approval decision"
		next = "approve, reject, or request changes"
	case analysis.initialState == StateBlocked:
		responsible = "Robert"
		waitingFor = firstNonEmpty(analysis.blockedReason, "missing information")
		next = "provide missing information or access"
	case containsAny(text, "lawyer", "client", "municipality", "insurer", "vivare", "waiting"):
		responsible = "external"
		waitingFor = "external reply or document"
		next = "draft follow-up if no response arrives"
	default:
		return nil
	}
	followUp := time.Now().UTC().Add(5 * 24 * time.Hour)
	if analysis.dueAt != nil {
		followUp = analysis.dueAt.Add(-48 * time.Hour)
		if followUp.Before(time.Now().UTC()) {
			followUp = time.Now().UTC().Add(24 * time.Hour)
		}
	}
	return &models.WorkflowOpenLoop{
		WorkflowID:       workflowID,
		ResponsibleParty: responsible,
		WaitingFor:       waitingFor,
		NextAction:       next,
		FollowUpAt:       &followUp,
		Status:           "open",
	}
}

func proposalForAnalysis(workflowID uuid.UUID, analysis inputAnalysis) *models.WorkflowProposal {
	action := analysis.nextAction
	options := []string{"Approve recommended action", "Request changes", "Add evidence/context", "Block this workflow"}
	if analysis.taskType == "technical" {
		options = []string{"Accept as ready for worker", "Request technical plan first", "Ask for tests/docs", "Block until GitHub evidence exists"}
	}
	if analysis.taskType == "publishing" {
		options = []string{"Approve draft-only workflow", "Make tone safer", "Add evidence links", "Do not publish"}
	}
	return &models.WorkflowProposal{
		WorkflowID:        workflowID,
		RecommendedAction: action,
		Options:           strings.Join(options, "\n"),
		Status:            "open",
	}
}

const automationSelectionProposalAction = "Select an automation for controlled execution"

func automationSelectionProposal(workflowID uuid.UUID, candidates []AutomationCandidate) *models.WorkflowProposal {
	options := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if _, err := uuid.Parse(strings.TrimSpace(candidate.ID)); err != nil {
			continue
		}
		label := firstNonEmpty(candidate.Name, candidate.RuntimeType, "Automation")
		options = append(options, fmt.Sprintf(
			"Use %s [automation:%s] - %s",
			label,
			strings.TrimSpace(candidate.ID),
			firstNonEmpty(candidate.Reason, "matched configured automation"),
		))
	}
	if len(options) == 0 {
		options = append(options, "Configure a suitable automation before approving execution")
	}
	return &models.WorkflowProposal{
		WorkflowID:        workflowID,
		RecommendedAction: automationSelectionProposalAction,
		Options:           strings.Join(options, "\n"),
		Status:            "open",
	}
}

func isAutomationSelectionProposal(proposal *models.WorkflowProposal) bool {
	return proposal != nil && strings.TrimSpace(proposal.RecommendedAction) == automationSelectionProposalAction
}

func selectedAutomationID(options string, selectedOption string) (string, error) {
	selectedOption = strings.TrimSpace(selectedOption)
	if selectedOption == "" {
		return "", fmt.Errorf("select one automation before approving controlled execution")
	}
	matched := false
	for _, option := range strings.Split(options, "\n") {
		if strings.TrimSpace(option) == selectedOption {
			matched = true
			break
		}
	}
	if !matched {
		return "", fmt.Errorf("selected automation is not one of the reviewed candidates")
	}
	const marker = "[automation:"
	start := strings.Index(selectedOption, marker)
	if start < 0 {
		return "", fmt.Errorf("configure a suitable automation before approving controlled execution")
	}
	start += len(marker)
	end := strings.Index(selectedOption[start:], "]")
	if end < 0 {
		return "", fmt.Errorf("selected automation candidate is malformed")
	}
	automationID := strings.TrimSpace(selectedOption[start : start+end])
	parsed, err := uuid.Parse(automationID)
	if err != nil || parsed == uuid.Nil {
		return "", fmt.Errorf("selected automation candidate has an invalid id")
	}
	return parsed.String(), nil
}

func workflowNeedsAutomation(input string, analysis inputAnalysis) bool {
	if workflowContainsWordOrPhrase(input, "run", "execute", "deploy", "install", "launch", "invoke") {
		return true
	}
	action := workflowContainsWordOrPhrase(input,
		"add", "apply", "build", "call", "change", "commit", "create", "delete", "fix",
		"implement", "merge", "modify", "move", "post", "publish", "push", "rename",
		"send", "start", "update", "write",
	)
	target := workflowContainsWordOrPhrase(input,
		"account", "api", "build", "code", "command", "deployment", "docker", "email",
		"file", "files", "message", "post", "posting", "repo", "repository", "request",
		"script", "test", "tests",
	)
	return action && target
}

func workflowContainsWordOrPhrase(value string, terms ...string) bool {
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

func qualityGatesForAnalysis(workflowID uuid.UUID, analysis inputAnalysis) []models.WorkflowQualityGate {
	gates := []models.WorkflowQualityGate{
		{WorkflowID: workflowID, Gate: "source provenance", Status: "pending", Reason: "workflow must retain source links"},
		{WorkflowID: workflowID, Gate: "verification before completion", Status: "pending", Reason: "completion requires task/verification result"},
	}
	if analysis.taskType == "technical" {
		for _, gate := range []string{"GitHub commit exists", "tests or build evidence", "README/setup updated", "Windows 11 operational path"} {
			gates = append(gates, models.WorkflowQualityGate{WorkflowID: workflowID, Gate: gate, Status: "pending", Reason: "developer/GitHub quality gate"})
		}
	}
	if analysis.taskType == "legal" || analysis.taskType == "financial" || analysis.taskType == "publishing" {
		gates = append(gates, models.WorkflowQualityGate{WorkflowID: workflowID, Gate: "human approval", Status: "pending", Reason: "risk/autonomy rule"})
		gates = append(gates, models.WorkflowQualityGate{WorkflowID: workflowID, Gate: "evidence-linked claims", Status: "pending", Reason: "factual claims need provenance"})
	}
	return gates
}

func checklistForAnalysis(analysis inputAnalysis) []checklistTemplate {
	base := []checklistTemplate{
		{label: "Review original source and provenance"},
		{label: "Link workflow to the correct project or case"},
		{label: "Check missing information and blockers"},
		{label: "Confirm priority and deadline"},
	}
	switch analysis.taskType {
	case "publishing":
		base = append(base,
			checklistTemplate{label: "Extract core story and target reader"},
			checklistTemplate{label: "Draft article structure"},
			checklistTemplate{label: "Create unpublished draft"},
			checklistTemplate{label: "Add tags and completion summary"},
			checklistTemplate{label: "Publish only after approval", requiresApproval: true},
		)
	case "legal":
		base = append(base,
			checklistTemplate{label: "Extract legal request, deadline, and evidence references"},
			checklistTemplate{label: "Prepare formal Dutch draft"},
			checklistTemplate{label: "Attach source-supported evidence"},
			checklistTemplate{label: "Request Robert approval before sending", requiresApproval: true},
		)
	case "technical":
		base = append(base,
			checklistTemplate{label: "Inspect repository context"},
			checklistTemplate{label: "Implement scoped code change"},
			checklistTemplate{label: "Run tests/build checks"},
			checklistTemplate{label: "Write completion summary"},
		)
	default:
		base = append(base,
			checklistTemplate{label: "Generate next action"},
			checklistTemplate{label: "Execute allowed administrative step"},
			checklistTemplate{label: "Verify completion before closing"},
		)
	}
	if analysis.requiresApproval {
		base = append(base, checklistTemplate{label: "Record approval decision before external/destructive action", requiresApproval: true})
	}
	return base
}

func transitionAllowed(from, to string, approved bool) bool {
	if from == to {
		return true
	}
	allowed := map[string][]string{
		StateNewInput:           {StateClassified, StateBlocked},
		StateClassified:         {StateLinked, StateChecklistGenerated, StateNeedsApproval, StateBlocked},
		StateLinked:             {StateChecklistGenerated, StateWaitingInput, StateNeedsApproval, StateBlocked},
		StateChecklistGenerated: {StateReady, StateNeedsApproval, StateWaitingInput, StateBlocked},
		StateWaitingInput:       {StateReady, StateChecklistGenerated, StateBlocked},
		StateNeedsApproval:      {StateReady, StateBlocked},
		StateReady:              {StateInProgress, StateBlocked},
		StateInProgress:         {StateCompleted, StateBlocked, StateWaitingInput},
		StateCompleted:          {StateArchived},
		StateBlocked:            {StateWaitingInput, StateReady, StateNeedsApproval, StateArchived},
	}
	if from == StateNeedsApproval && to == StateReady && !approved {
		return false
	}
	for _, candidate := range allowed[from] {
		if candidate == to {
			return true
		}
	}
	return false
}

func normalizeProposalStatus(request ProposalResolutionRequest) (string, error) {
	status := strings.ToLower(strings.TrimSpace(request.Status))
	switch status {
	case "approved", "rejected", "changes_requested":
		return status, nil
	case "":
	default:
		return "", fmt.Errorf("unsupported proposal status")
	}
	if request.Approved {
		return "approved", nil
	}
	decisionText := strings.ToLower(request.SelectedOption + " " + request.Note)
	if strings.Contains(decisionText, "reject") || strings.Contains(decisionText, "block") || strings.Contains(decisionText, "do not") {
		return "rejected", nil
	}
	return "changes_requested", nil
}

func followUpOptions(item *models.WorkflowItem, loop models.WorkflowOpenLoop) []string {
	if item.RiskLevel == "high" || item.RequiresApproval || loop.ResponsibleParty == "Robert" {
		return []string{"Approve follow-up draft", "Request safer wording", "Add evidence/context first", "Keep blocked"}
	}
	return []string{"Run follow-up automatically", "Draft only", "Wait longer", "Close open loop"}
}

func hasChecklistLabel(items []models.WorkflowChecklistItem, label string) bool {
	for _, item := range items {
		if item.Label == label {
			return true
		}
	}
	return false
}

func hasProposalAction(proposals []models.WorkflowProposal, action string) bool {
	for _, proposal := range proposals {
		if proposal.RecommendedAction == action {
			return true
		}
	}
	return false
}

func hasEvidenceNeedingReview(evidence []models.WorkflowEvidenceClaim) bool {
	for _, claim := range evidence {
		if claim.NeedsReview || claim.SourceURI == "" || claim.Status == "unsupported" || claim.Status == "needs_review" {
			return true
		}
	}
	return false
}

func keywordGate(value string, keywords []string, missingReason string) (string, string) {
	for _, keyword := range keywords {
		if strings.Contains(value, keyword) {
			return "passed", "task output mentions " + keyword
		}
	}
	return "needs_review", missingReason
}

func defaultWorkflowRules() []models.WorkflowRule {
	return []models.WorkflowRule{
		{RuleKey: "approval.legal_external", Name: "Legal and government communication is draft-only", Description: "Legal, government, insurance, housing association, and lawyer messages must be drafted and held for Robert approval before sending.", Category: "approval", Enabled: true},
		{RuleKey: "approval.public_posting", Name: "Public posting requires evidence and approval", Description: "Public accountability posts, Medium publishing, social posts, and public claims are prepared as drafts only until evidence is linked and Robert approves.", Category: "approval", Enabled: true},
		{RuleKey: "approval.financial_limit_25", Name: "Financial commitments over 25 EUR need approval", Description: "Payments, paid provider usage, purchases, refunds, quotes, contracts, and commitments over 25 EUR cannot execute automatically.", Category: "approval", Enabled: true},
		{RuleKey: "safety.no_permanent_delete", Name: "Never delete evidence permanently", Description: "Legal, financial, source, and project files may be archived or marked duplicate, but permanent deletion requires explicit human approval.", Category: "safety", Enabled: true},
		{RuleKey: "safety.account_changes", Name: "Account changes require approval", Description: "Password, permission, profile, connector, posting, or account-setting changes must be approval-gated.", Category: "safety", Enabled: true},
		{RuleKey: "workflow.checklist_required", Name: "Execution workflows receive checklists", Description: "Every actionable workflow item gets a concrete checklist before worker execution or completion.", Category: "workflow", Enabled: true},
		{RuleKey: "workflow.blocked_has_reason", Name: "Blocked workflows need owner, reason, and next action", Description: "Blocked and waiting workflows must record the responsible party, blocker, next action, and follow-up date where possible.", Category: "workflow", Enabled: true},
		{RuleKey: "workflow.external_followup", Name: "External waiting creates follow-up", Description: "Items waiting for a lawyer, municipality, client, insurer, freelancer, developer, or VA get an open loop with a follow-up date.", Category: "workflow", Enabled: true},
		{RuleKey: "workflow.retry_limits", Name: "Worker retries are durable and capped", Description: "Failed worker attempts are counted, retried with backoff, and blocked for human review after the retry limit.", Category: "workflow", Enabled: true},
		{RuleKey: "verification.before_done", Name: "Completion requires verification", Description: "A workflow can only complete through the worker when checklist progress and task verification support completion.", Category: "verification", Enabled: true},
		{RuleKey: "verification.claims_need_sources", Name: "Important factual claims need sources", Description: "Evidence claims are linked to their source where possible and marked for review when unsupported.", Category: "verification", Enabled: true},
		{RuleKey: "developer.github_quality_gate", Name: "Developer completion requires GitHub evidence", Description: "Developer claims of completion require branch/commit/build/test/readme evidence before acceptance.", Category: "developer", Enabled: true},
		{RuleKey: "content.medium_draft_only", Name: "Medium articles are draft-only", Description: "Article workflows may draft, format, and attach a draft link, but publishing remains approval-gated.", Category: "content", Enabled: true},
		{RuleKey: "learning.corrections_feed_memory", Name: "Corrections become future rules or memory", Description: "Rejected drafts, project corrections, and tone changes should become reviewable lessons instead of unbounded raw memory.", Category: "learning", Enabled: true},
	}
}

func normalizeRunLimit(limit int) int {
	if limit <= 0 {
		return 10
	}
	if limit > 50 {
		return 50
	}
	return limit
}

func limitWorkflowItems(items []models.WorkflowItem, limit int) []models.WorkflowItem {
	if limit <= 0 || len(items) <= limit {
		return items
	}
	return items[:limit]
}

func engineCapabilities() []EngineCapability {
	return []EngineCapability{
		{ID: "state-machine", Name: "Workflow state machine", Status: "implemented", Implemented: []string{"persistent workflow states", "validated transitions", "blocked/waiting/completed/archive states"}, Next: []string{"per-project custom states"}},
		{ID: "event-triggers", Name: "Event-driven trigger logic", Status: "implemented", Implemented: []string{"intake trigger field", "audit trigger log", "connected-source extraction creates workflow candidates", "stable source-record deduplication", "source retraction blocks stale work"}, Next: []string{"webhook workers per connector"}},
		{ID: "adapters", Name: "Integration adapter layer", Status: "partial", Implemented: []string{"adapter capability names", "source-local-folder path", "allowlisted incremental JSON source bridge", "read-only Gmail, Drive, Contacts, Calendar, Trello, and GitHub adapters", "task-engine runner adapter"}, Next: []string{"retained sandbox acceptance for every configured account", "provider webhooks and scoped write adapters"}},
		{ID: "context-memory", Name: "Context and memory layer", Status: "implemented", Implemented: []string{"project key", "separate source links", "memory/task/source retrieval"}, Next: []string{"project dossier projection"}},
		{ID: "decision-rules", Name: "Autonomous decision rules", Status: "implemented", Implemented: []string{"separate decision records", "approval rules", "autonomy levels", "blocked reasons", "next action"}, Next: []string{"configurable per-contact rules"}},
		{ID: "ai-reasoning", Name: "AI reasoning layer", Status: "partial", Implemented: []string{"deterministic classification fallback", "task type/risk/priority extraction"}, Next: []string{"LLM structured extractor with schema validation"}},
		{ID: "checklists", Name: "Checklist generation", Status: "implemented", Implemented: []string{"type-specific checklist templates", "approval-marked checklist steps"}, Next: []string{"learned checklist templates"}},
		{ID: "priority", Name: "Priority engine", Status: "implemented", Implemented: []string{"deadline/risk/type scoring", "priority-sorted inbox"}, Next: []string{"waiting-time and client importance scoring"}},
		{ID: "exceptions", Name: "Exception and escalation logic", Status: "implemented", Implemented: []string{"blocked state", "missing-info detection", "durable retry limits", "structured unknown-outcome recovery review"}, Next: []string{"operator notification channels"}},
		{ID: "audit", Name: "Audit trail and traceability", Status: "implemented", Implemented: []string{"workflow events", "separate transitions", "decision records", "source links"}, Next: []string{"cross-module trace IDs"}},
		{ID: "approval-gates", Name: "Human approval gates", Status: "implemented", Implemented: []string{"approval queue", "approve/reject buttons", "approval-only transitions", "approval checklist steps"}, Next: []string{"per-action approval scopes"}},
		{ID: "worker-queue", Name: "Worker/queue system", Status: "implemented", Implemented: []string{"durable retry counters", "ready/in-progress/completed/blocked lifecycle", "controlled automation execution adapter", "background scheduler", "owned renewable claims", "non-idempotent retry guard"}, Next: []string{"multi-node queue metrics"}},
		{ID: "feedback", Name: "Feedback loop", Status: "partial", Implemented: []string{"checklist correction events", "resolution notes"}, Next: []string{"store rejected draft/tone preferences into memory"}},
		{ID: "safety", Name: "Safety boundaries", Status: "implemented", Implemented: []string{"never-send/publish/delete/spend without approval rules", "approval reason surfaced", "uncertain and sensitive source extractions require review", "in-progress source work cannot be silently retracted"}, Next: []string{"policy editor"}},
		{ID: "universal-intake", Name: "Universal intake engine", Status: "implemented", Implemented: []string{"manual/source intake request", "source id/type/content/sender metadata", "normalized intake records"}, Next: []string{"connector webhooks and voice/screenshot intake"}},
		{ID: "project-matching", Name: "Project matching engine", Status: "implemented", Implemented: []string{"project match records", "keyword/project heuristics", "trello and drive reference hints"}, Next: []string{"semantic matching against connected-source index"}},
		{ID: "context-builder", Name: "Context builder engine", Status: "partial", Implemented: []string{"project key", "source provenance", "memory/source modules available"}, Next: []string{"project dossier projection with people, deadlines, documents, and open questions"}},
		{ID: "action-planner", Name: "Action planner engine", Status: "partial", Implemented: []string{"next action selection", "task-engine worker adapter", "proposal records"}, Next: []string{"multi-step executable plans per workflow"}},
		{ID: "checklist-compiler", Name: "Checklist compiler engine", Status: "implemented", Implemented: []string{"task-type checklist templates", "approval-marked checklist steps", "deadline reminder steps"}, Next: []string{"per-project editable templates"}},
		{ID: "autonomy-levels", Name: "Autonomy level engine", Status: "implemented", Implemented: []string{"approve_before_execute", "autonomous_safe", "blocked/waiting handling"}, Next: []string{"per-source/per-contact autonomy settings"}},
		{ID: "risk-scoring", Name: "Risk scoring engine", Status: "implemented", Implemented: []string{"legal/financial/public/destructive risk scoring", "approval reason surfaced"}, Next: []string{"weighted project/client/irreversibility risk model"}},
		{ID: "evidence-linking", Name: "Evidence and source linking engine", Status: "implemented", Implemented: []string{"source link table", "evidence claim table", "unsupported claim review flag"}, Next: []string{"claim-source precision checks against extracted snippets"}},
		{ID: "deadline-detection", Name: "Deadline detection engine", Status: "partial", Implemented: []string{"today/tomorrow/urgent detection", "RFC3339 and ISO calendar start parsing", "due dates", "check reminder checklist items"}, Next: []string{"locale-aware date parser for letters and PDFs"}},
		{ID: "follow-up", Name: "Follow-up engine", Status: "implemented", Implemented: []string{"open loop records", "follow-up date", "dashboard due-open-loop queue", "due follow-up worker creates proposals and checklist steps"}, Next: []string{"calendar reminder adapter and message draft generation"}},
		{ID: "waiting-state", Name: "Waiting-state engine", Status: "implemented", Implemented: []string{"blocked/waiting states", "responsible party", "waiting-for reason"}, Next: []string{"automatic Trello On-Hold transitions"}},
		{ID: "delegation", Name: "Delegation engine", Status: "partial", Implemented: []string{"proposal/options records", "checklist output suitable for VA/developer handoff"}, Next: []string{"dedicated delegation package templates"}},
		{ID: "proposal", Name: "Proposal yes-no engine", Status: "implemented", Implemented: []string{"recommended action records", "option sets by task type", "approval/change/rejection resolution updates workflow state"}, Next: []string{"proposal editing with custom option text"}},
		{ID: "communication-drafting", Name: "Communication drafting engine", Status: "partial", Implemented: []string{"task type and approval gates", "formal legal/publishing/developer workflow hints"}, Next: []string{"recipient-specific tone templates and draft adapters"}},
		{ID: "document-ingestion", Name: "Document ingestion engine", Status: "partial", Implemented: []string{"allowlisted local folder sync", "text extraction for readable files", "source provenance"}, Next: []string{"OCR, file renaming, folder movement, PDF extraction"}},
		{ID: "duplicate-version", Name: "Duplicate and version control engine", Status: "partial", Implemented: []string{"stable source-identity deduplication", "immutable workflow revision hashes", "changed source revisions supersede stale workflows and approvals", "source item cursor/hash support"}, Next: []string{"near-duplicate and final-vs-draft detection"}},
		{ID: "case-timeline", Name: "Case timeline engine", Status: "partial", Implemented: []string{"timestamped intake/events/transitions/claims"}, Next: []string{"project timeline API grouped by evidence"}},
		{ID: "contradiction-detection", Name: "Contradiction detection engine", Status: "implemented", Implemented: []string{"verification module has conflict statuses", "evidence claims can be reviewed", "deterministic cross-source scans require separate source records, a shared concrete topic, and opposite lifecycle assertions", "conflicts preserve both source references and remain human-review signals"}, Next: []string{"typed entity/date/value contradiction extraction for operator-reviewed evidence"}},
		{ID: "developer-github", Name: "Developer/GitHub engine", Status: "implemented", Implemented: []string{"read-only GitHub source adapter for repository, issue, pull request, branch, commit, and Actions-run records", "technical task classification", "source-linked commit and successful Actions evidence gates", "worker prose cannot self-certify GitHub completion"}, Next: []string{"read-only branch comparison and repository acceptance reports"}},
		{ID: "software-quality-gate", Name: "Software quality gate engine", Status: "implemented", Implemented: []string{"test/build/readme/windows setup gates created for technical workflows", "mandatory technical gates require source-linked GitHub and controlled-runtime evidence before completion"}, Next: []string{"automated repository acceptance reports"}},
		{ID: "public-accountability", Name: "Public accountability engine", Status: "partial", Implemented: []string{"public-post approval gate", "evidence claim records", "risk-gated publishing flow"}, Next: []string{"safer wording reviewer and source-backed timeline builder"}},
		{ID: "medium-publishing", Name: "Medium/blog publishing engine", Status: "partial", Implemented: []string{"publishing task type", "draft-only rule", "article checklist"}, Next: []string{"Medium draft adapter and image prompt workflow"}},
		{ID: "client-operations", Name: "Client job operations engine", Status: "partial", Implemented: []string{"administrative workflow path", "deadline/priority/checklist support"}, Next: []string{"quote, travel, materials, and invoice templates"}},
		{ID: "calendar-availability", Name: "Calendar and availability engine", Status: "partial", Implemented: []string{"read-only Google Calendar adapter", "bounded upcoming-event preparation proposals", "source-backed due dates and check reminders", "review-gated cancellation retraction", "stable overlap detection with stale-conflict retraction"}, Next: []string{"travel-time checks", "approved reminder write adapter"}},
		{ID: "negotiation-support", Name: "Negotiation support engine", Status: "planned", Implemented: []string{"proposal record foundation"}, Next: []string{"preferred/fallback/boundary proposal generator"}},
		{ID: "admin-monitoring", Name: "Admin monitoring dashboard engine", Status: "implemented", Implemented: []string{"dashboard endpoint", "approvals, blocked, ready, high-risk, due open loops, missing next action", "connected-source sync job history", "failed source sync review workflows"}, Next: []string{"operator notification channel"}},
		{ID: "error-recovery", Name: "Error recovery engine", Status: "implemented", Implemented: []string{"retry backoff", "blocked after retry limit", "expired lease recovery", "operator-confirmed retry", "evidence-backed interrupted completion", "idempotent follow-up replay"}, Next: []string{"connector-specific recovery playbooks"}},
		{ID: "learning-corrections", Name: "Learning-from-corrections engine", Status: "partial", Implemented: []string{"approval/rejection notes", "checklist update audit", "rule for corrections feeding memory"}, Next: []string{"reviewable memory lessons from corrections"}},
		{ID: "rules-library", Name: "Rules library engine", Status: "implemented", Implemented: []string{"persistent editable rule table", "default 14-rule safety/workflow library"}, Next: []string{"dashboard rule editor"}},
		{ID: "multi-agent-workers", Name: "Multi-agent worker engine", Status: "partial", Implemented: []string{"single orchestrated task runner adapter", "capability separation by module"}, Next: []string{"specialized worker registry"}},
		{ID: "next-best-action", Name: "Next best action engine", Status: "implemented", Implemented: []string{"next action field on every intake", "dashboard flags missing next actions"}, Next: []string{"project-level next-best-action rollup"}},
		{ID: "completion", Name: "Completion engine", Status: "implemented", Implemented: []string{"verification-gated completion", "controlled runtime evidence gate", "manual-completion bypass prevention", "evidence-backed interruption resolution", "quality-gate validation", "completion timestamp", "archive state"}, Next: []string{"completion summary generator and archive package"}},
	}
}

func workflowSourceRevision(request IntakeRequest, input string) string {
	if strings.TrimSpace(request.SourceType) == "" &&
		strings.TrimSpace(request.SourceID) == "" &&
		strings.TrimSpace(request.SourceURI) == "" {
		return ""
	}
	canonical := strings.Join([]string{
		strings.TrimSpace(input),
		strings.TrimSpace(request.ProjectKey),
		strings.TrimSpace(request.AutomationID),
		strings.TrimSpace(request.SourceType),
		strings.TrimSpace(request.SourceID),
		strings.TrimSpace(request.SourceURI),
		strings.TrimSpace(request.SourceLabel),
		strings.TrimSpace(request.ContentType),
		strings.TrimSpace(request.Sender),
		strings.TrimSpace(request.ReceivedAt),
		fmt.Sprintf("%t", request.RequiresReview),
		strings.TrimSpace(request.ReviewReason),
		request.CoordinationPlan.PlanID.String(),
		fmt.Sprintf("%d", request.CoordinationPlan.Revision),
		strings.ToLower(strings.TrimSpace(request.CoordinationPlan.Digest)),
		strings.TrimSpace(request.CoordinationPlan.NodeID),
	}, "\x1f")
	sum := sha256.Sum256([]byte(canonical))
	return fmt.Sprintf("%x", sum)
}

func compactTitle(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	if len(value) <= 90 {
		return value
	}
	return value[:87] + "..."
}

func compact(value string, limit int) string {
	value = strings.Join(strings.Fields(value), " ")
	if limit <= 3 || len(value) <= limit {
		return value
	}
	return value[:limit-3] + "..."
}

func parseOptionalTime(value string) *time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02"} {
		parsed, err := time.Parse(layout, value)
		if err == nil {
			return &parsed
		}
	}
	return nil
}

func urgencyForPriority(priority int) string {
	switch {
	case priority >= 85:
		return "high"
	case priority >= 60:
		return "medium"
	default:
		return "normal"
	}
}

func driveRefForProject(projectKey string) string {
	clean := strings.ReplaceAll(strings.TrimSpace(projectKey), "\\", "/")
	if clean == "" {
		return ""
	}
	return "Projects/" + clean
}

func reliabilityForSource(sourceType string) string {
	switch strings.ToLower(strings.TrimSpace(sourceType)) {
	case "email", "cloud_document", "local_folder", "github", "calendar":
		return "direct_source"
	case "":
		return "unlinked"
	default:
		return "connected_source"
	}
}

func splitSentences(value string) []string {
	value = strings.NewReplacer("\n", ". ", ";", ".").Replace(value)
	result := []string{}
	for _, part := range strings.Split(value, ".") {
		part = strings.TrimSpace(part)
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}

func extractEntities(value string) []string {
	result := []string{}
	for _, word := range strings.Fields(value) {
		word = strings.Trim(word, ".,;:()[]")
		if len(word) > 2 && word[:1] == strings.ToUpper(word[:1]) {
			result = append(result, word)
		}
	}
	if len(result) > 20 {
		return uniqueStrings(result[:20])
	}
	return uniqueStrings(result)
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

func containsAny(value string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
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

func minInt(left, right int) int {
	return int(math.Min(float64(left), float64(right)))
}

func approvalStatus(requiresApproval bool) string {
	if requiresApproval {
		return "pending"
	}
	return "not_required"
}

func maxRetriesForAnalysis(analysis inputAnalysis) int {
	if analysis.riskLevel == "high" || analysis.confidence < 0.6 {
		return 1
	}
	if analysis.taskType == "technical" {
		return 3
	}
	return 2
}

func reminderBefore(due time.Time) *time.Time {
	reminder := due.Add(-24 * time.Hour)
	now := time.Now().UTC()
	if reminder.Before(now) {
		reminder = now
	}
	return &reminder
}

func retryBackoff(attempt int) time.Duration {
	if attempt <= 1 {
		return 15 * time.Minute
	}
	return time.Duration(attempt*attempt) * 15 * time.Minute
}

func approvalRule(approved bool) string {
	if approved {
		return "human approval recorded"
	}
	return "standard state transition"
}

func feedbackNoteUseful(signal, note string) bool {
	note = strings.TrimSpace(note)
	if len([]rune(note)) < 12 {
		return false
	}
	lower := strings.ToLower(note)
	generic := map[string]bool{
		"approval rejected":                true,
		"proposal rejected":                true,
		"apply requested proposal changes": true,
		"changes requested":                true,
		"rejected":                         true,
		"not approved":                     true,
	}
	if generic[lower] {
		return false
	}
	if strings.TrimSpace(signal) == "approval_approved" && !feedbackLearningCue(lower) {
		return false
	}
	return strings.TrimSpace(signal) != ""
}

func feedbackLearningCue(lowerNote string) bool {
	for _, cue := range []string{
		"always",
		"avoid",
		"exclude",
		"future",
		"going forward",
		"include",
		"keep",
		"learn",
		"never",
		"next time",
		"prefer",
		"similar",
		"tone",
		"use",
	} {
		if strings.Contains(lowerNote, cue) {
			return true
		}
	}
	return false
}

func feedbackLessonContent(item models.WorkflowItem, signal, note string) string {
	parts := []string{
		"Robert gave HAI workflow feedback.",
		"Signal: " + strings.TrimSpace(signal) + ".",
	}
	if item.ProjectKey != "" {
		parts = append(parts, "Project: "+item.ProjectKey+".")
	}
	if item.RiskLevel != "" {
		parts = append(parts, "Risk level: "+item.RiskLevel+".")
	}
	if item.TaskType != "" {
		parts = append(parts, "Task type: "+item.TaskType+".")
	}
	if item.Title != "" {
		parts = append(parts, "Workflow: "+item.Title+".")
	}
	parts = append(parts,
		"Correction: "+strings.TrimSpace(note)+".",
		"Future behavior: apply this correction to similar project, source, recipient, tone, checklist, proposal, or approval decisions. If the correction conflicts with verified source evidence or a newer Robert instruction, ask for review instead of acting from memory.",
	)
	return strings.Join(parts, " ")
}

func feedbackLessonSummary(item models.WorkflowItem, signal, note string) string {
	prefix := "Learn from " + strings.ReplaceAll(strings.TrimSpace(signal), "_", " ")
	if item.ProjectKey != "" {
		prefix += " for " + item.ProjectKey
	}
	return compactWorkflowText(prefix+": "+strings.TrimSpace(note), 240)
}

func feedbackLessonTags(item models.WorkflowItem, signal string) []string {
	tags := []string{"workflow-feedback", "correction", strings.TrimSpace(signal)}
	for _, value := range []string{item.ProjectKey, item.RiskLevel, item.TaskType, item.AutonomyLevel, item.SourceType} {
		value = strings.TrimSpace(value)
		if value != "" {
			tags = append(tags, value)
		}
	}
	return tags
}

func feedbackLessonConfidence(signal string) float64 {
	switch strings.TrimSpace(signal) {
	case "proposal_changes_requested":
		return 0.76
	case "approval_rejected", "proposal_rejected":
		return 0.82
	case "approval_approved":
		return 0.66
	case "interruption_retry", "interruption_keep_blocked", "interruption_confirm_completed":
		return 0.78
	case "checklist_blocked":
		return 0.74
	default:
		return 0.72
	}
}

func workflowMemoryUseful(memory models.ContextMemory) bool {
	if memory.Archived || memory.Confidence < 0.45 {
		return false
	}
	if strings.TrimSpace(firstNonEmpty(memory.Summary, memory.Content)) == "" {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(memory.Kind)) {
	case "lesson", "preference", "procedural":
		return true
	default:
		return false
	}
}

func workflowMemoryLessonText(memory models.ContextMemory) string {
	text := strings.TrimSpace(firstNonEmpty(memory.Summary, memory.Content))
	if text == "" {
		return ""
	}
	return compactWorkflowText(text, 180)
}

func workflowMemorySourceURI(memory models.ContextMemory) string {
	if uri := strings.TrimSpace(memory.SourceURI); uri != "" {
		return uri
	}
	if memory.ID != uuid.Nil {
		return "memory://" + memory.ID.String()
	}
	return "memory://context"
}

func compactWorkflowText(value string, limit int) string {
	value = strings.Join(strings.Fields(value), " ")
	if limit <= 0 || len(value) <= limit {
		return value
	}
	if limit <= 3 {
		return value[:limit]
	}
	return value[:limit-3] + "..."
}

func SortItems(items []models.WorkflowItem) {
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].PriorityScore > items[j].PriorityScore
	})
}
