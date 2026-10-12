package workflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
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

	sourceRetractionQuarantinePrefix = "source retraction quarantine: "

	frameworkSelectionDecisionType = "framework_selection"
	frameworkSelectionEventType    = "workflow.framework_selection"
)

var ErrWorkflowSourceRetracted = errors.New("workflow source was retracted")
var ErrWorkflowIntakeIncomplete = errors.New("workflow source intake is incomplete; inspect its state before retrying")
var ErrWorkflowIntakeConcurrentChange = errors.New("workflow changed during intake; reload before retrying")

type WorkflowPersistenceError struct {
	Operation string
	Err       error
}

func (e *WorkflowPersistenceError) Error() string {
	if e == nil {
		return "workflow state could not be safely saved"
	}
	if strings.TrimSpace(e.Operation) == "" {
		return fmt.Sprintf("workflow state could not be safely saved: %v", e.Err)
	}
	return fmt.Sprintf("workflow state could not be safely saved during %s: %v", e.Operation, e.Err)
}

func (e *WorkflowPersistenceError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func workflowPersistenceFailure(operation string, err error) error {
	if err == nil {
		return nil
	}
	return &WorkflowPersistenceError{Operation: operation, Err: err}
}

// WorkflowAuditPersistenceError marks a failed durable audit write. Callers
// must not continue with later workflow state changes after receiving it.
type WorkflowAuditPersistenceError struct {
	Operation string
	Err       error
}

func (e *WorkflowAuditPersistenceError) Error() string {
	if e == nil {
		return "workflow audit history is incomplete"
	}
	if strings.TrimSpace(e.Operation) == "" {
		return fmt.Sprintf("workflow audit history is incomplete: %v", e.Err)
	}
	return fmt.Sprintf("workflow audit history is incomplete during %s: %v", e.Operation, e.Err)
}

func (e *WorkflowAuditPersistenceError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func workflowAuditPersistenceFailure(operation string, err error) error {
	if err == nil {
		return nil
	}
	return &WorkflowAuditPersistenceError{Operation: operation, Err: err}
}

type IntakeRequest struct {
	OwnerIdentity            string                              `json:"-"`
	Input                    string                              `json:"input"`
	SuccessCriteria          []string                            `json:"successCriteria,omitempty"`
	ProjectKey               string                              `json:"projectKey,omitempty"`
	ProjectKeyHint           string                              `json:"projectKeyHint,omitempty"`
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
	Decision                 string `json:"decision"`
	Note                     string `json:"note"`
	EvidenceURI              string `json:"evidenceUri,omitempty"`
	EvidenceLabel            string `json:"evidenceLabel,omitempty"`
	PriorExecutionReconciled bool   `json:"priorExecutionReconciled,omitempty"`
	Actor                    string `json:"actor,omitempty"`
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
	ExecutionContext      context.Context                     `json:"-"`
	OwnerIdentity         string                              `json:"-"`
	PursuitID             string                              `json:"pursuitId,omitempty"`
	WorkflowID            string                              `json:"workflowId"`
	Request               string                              `json:"request"`
	SuccessCriteria       []string                            `json:"successCriteria,omitempty"`
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
	PlanID                    string                              `json:"planId,omitempty"`
	CompletionStatus          string                              `json:"completionStatus"`
	VerificationStatus        string                              `json:"verificationStatus"`
	Output                    string                              `json:"output,omitempty"`
	FailureReason             string                              `json:"failureReason,omitempty"`
	RuntimeEvidenceURI        string                              `json:"runtimeEvidenceUri,omitempty"`
	RuntimeEvidenceLabel      string                              `json:"runtimeEvidenceLabel,omitempty"`
	RuntimeRouteTrace         *models.AutomationRuntimeRouteTrace `json:"runtimeRouteTrace,omitempty"`
	Passed                    bool                                `json:"passed"`
	ReviewRequired            bool                                `json:"reviewRequired"`
	ApprovalRequired          bool                                `json:"approvalRequired"`
	ExternalActionExecuted    bool                                `json:"externalActionExecuted"`
	ExecutionOutcomeUncertain bool                                `json:"executionOutcomeUncertain"`
	FrameworkSelection        *FrameworkSelectionProvenance       `json:"frameworkSelection,omitempty"`
}

type safeNoSideEffectTaskError struct {
	err error
}

func (e *safeNoSideEffectTaskError) Error() string {
	if e == nil || e.err == nil {
		return "task failed before external execution"
	}
	return e.err.Error()
}

func (e *safeNoSideEffectTaskError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.err
}

// MarkTaskFailureSafeNoSideEffect marks a failure known to happen before any
// external side effect can begin. Unmarked runner errors require human review.
func MarkTaskFailureSafeNoSideEffect(err error) error {
	if err == nil {
		return nil
	}
	return &safeNoSideEffectTaskError{err: err}
}

// IsTaskFailureSafeNoSideEffect reports whether err carries the explicit
// pre-execution safety marker.
func IsTaskFailureSafeNoSideEffect(err error) bool {
	marked := false
	for err != nil {
		if _, joined := err.(interface{ Unwrap() []error }); joined {
			return false
		}
		if safeErr, ok := err.(*safeNoSideEffectTaskError); ok {
			if safeErr == nil || safeErr.err == nil {
				return false
			}
			marked = true
		}
		err = errors.Unwrap(err)
	}
	return marked
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
	ReviewRequired     bool                          `json:"reviewRequired"`
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

// AuthorizedEffectRecoveryIntake is a narrow internal recovery boundary for
// an effect whose exact authorization receipt has already been consumed. It
// preserves historical plan provenance but does not grant workflow execution.
type AuthorizedEffectRecoveryIntake interface {
	IntakeAuthorizedEffectRecovery(request IntakeRequest) (*WorkflowRecord, error)
}

type service struct {
	repo                          Repository
	taskRunner                    TaskRunner
	memoryService                 memory.Service
	githubQualityEvidenceResolver GitHubQualityEvidenceResolver
	controlledLearning            ControlledLearningRecorder
	lifeOntologyProjector         LifeOntologyProjector
	acceptedPlanResolver          plangraph.AcceptedRevisionResolver
	coordinationProjector         CoordinationPlanProjector
	reminderDeliverySink          ReminderDeliverySink
	activeTaskRuns                sync.Map
}

func NewService(repo Repository, memoryServices ...memory.Service) Service {
	return &service{repo: repo, memoryService: firstMemoryService(memoryServices...)}
}

func NewServiceWithTaskRunner(repo Repository, taskRunner TaskRunner, memoryServices ...memory.Service) Service {
	return &service{repo: repo, taskRunner: taskRunner, memoryService: firstMemoryService(memoryServices...)}
}

func NewServiceWithTaskRunnerAndGitHubQualityEvidenceResolver(repo Repository, taskRunner TaskRunner, resolver GitHubQualityEvidenceResolver, memoryServices ...memory.Service) Service {
	return &service{
		repo:                          repo,
		taskRunner:                    taskRunner,
		memoryService:                 firstMemoryService(memoryServices...),
		githubQualityEvidenceResolver: resolver,
	}
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
	successCriteria, err := normalizeWorkflowSuccessCriteria(request.SuccessCriteria)
	if err != nil {
		return nil, err
	}
	request.SuccessCriteria = successCriteria
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
	_ = s.ensureDefaultRules()
	sourceType := strings.TrimSpace(request.SourceType)
	sourceID := strings.TrimSpace(request.SourceID)
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
				selectionCandidates = candidates
				automationSelectionReason = candidates[0].Reason
				if workflowExplicitlyNamesAutomationSelection(input) {
					selectionRequired = true
				} else {
					request.AutomationID = candidates[0].ID
				}
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
		if analysis.requiresApproval {
			analysis.approvalReason = firstNonEmpty(analysis.approvalReason, "approval required") + "; exact automation selection: " + automationSelectionReason
		} else {
			// Choosing between safe runtimes is a routing decision, not a human
			// approval for the action itself. Keep it in the existing decision queue
			// until a runtime is chosen, then remove the temporary gate.
			analysis.requiresApproval = true
			analysis.approvalReason = automationSelectionOnlyApprovalPrefix + automationSelectionReason
		}
		analysis.autonomyLevel = "approve_before_execute"
		analysis.initialState = StateNeedsApproval
		analysis.nextAction = "select the exact automation before controlled execution"
		analysis.ruleApplied += "; exact automation selection required before controlled execution"
	}
	selectionFingerprint, _ := json.Marshal(struct {
		Required   bool
		Reason     string
		Candidates []AutomationCandidate
	}{Required: selectionRequired, Reason: automationSelectionReason, Candidates: selectionCandidates})
	sourceRevision := workflowSourceRevision(request, input, string(selectionFingerprint))
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
			if existing.CurrentState == StateNewInput {
				return nil, fmt.Errorf("%w: workflow %s", ErrWorkflowIntakeIncomplete, existing.ID)
			}
			if err := s.ensureWorkflowCoordinationDraft(existing, firstNonEmpty(request.Actor, "engine")); err != nil {
				return nil, err
			}
			if err := s.audit(existing.ID, "workflow.intake_deduped", "", existing.CurrentState, "existing workflow reused for unchanged source revision", request.Trigger, dedupeRule, request.SourceURI, firstNonEmpty(request.Actor, "engine")); err != nil {
				return nil, workflowAuditPersistenceFailure("deduplicated intake event", err)
			}
			return s.Get(existing.ID)
		}
		if err := s.supersedeSourceWorkflow(existing, request, sourceRevision); err != nil {
			return nil, err
		}
	}
	projectKey := firstNonEmpty(request.ProjectKey, analysis.projectKey)
	item := &models.WorkflowItem{
		OwnerIdentity:    strings.TrimSpace(request.OwnerIdentity),
		Title:            analysis.title,
		Description:      input,
		SuccessCriteria:  append([]string(nil), request.SuccessCriteria...),
		ProjectKey:       projectKey,
		AutomationID:     strings.TrimSpace(request.AutomationID),
		MandateID:        mandateID,
		CurrentState:     StateNewInput,
		TaskType:         analysis.taskType,
		RiskLevel:        analysis.riskLevel,
		PriorityScore:    analysis.priority,
		Confidence:       analysis.confidence,
		AutonomyLevel:    analysis.autonomyLevel,
		RequiresApproval: analysis.requiresApproval,
		ApprovalStatus:   approvalStatus(analysis.requiresApproval),
		ApprovalReason:   analysis.approvalReason,
		NextAction:       "persist required quality gates before scheduling workflow",
		SourceType:       sourceType,
		SourceID:         sourceID,
		SourceURI:        strings.TrimSpace(request.SourceURI),
		SourceLabel:      strings.TrimSpace(request.SourceLabel),
		SourceRevision:   sourceRevision,
		DueAt:            analysis.dueAt,
		MaxRetries:       maxRetriesForAnalysis(analysis),
	}
	applyWorkflowCoordinationBinding(item, coordinationBinding)
	created, createdNew, err := s.repo.CreateItemIdempotent(item)
	if err != nil {
		return nil, err
	}
	if !createdNew {
		if created.CurrentState == StateNewInput {
			return nil, fmt.Errorf("%w: workflow %s", ErrWorkflowIntakeIncomplete, created.ID)
		}
		if err := s.ensureWorkflowCoordinationDraft(created, firstNonEmpty(request.Actor, "engine")); err != nil {
			return nil, err
		}
		if err := s.audit(created.ID, "workflow.intake_deduped", "", created.CurrentState, "existing workflow reused after concurrent receipt-bound intake", request.Trigger, "source identity unique boundary", request.SourceURI, firstNonEmpty(request.Actor, "engine")); err != nil {
			return nil, workflowAuditPersistenceFailure("concurrent deduplicated intake event", err)
		}
		return s.Get(created.ID)
	}
	intakeDecisions := make([]models.WorkflowDecision, 0, 4)
	intakeRecord, intakeErr := s.repo.SaveIntakeRecord(&models.WorkflowIntakeRecord{
		WorkflowID:        created.ID,
		SourceType:        created.SourceType,
		SourceID:          created.SourceID,
		SourceURI:         created.SourceURI,
		SourceLabel:       created.SourceLabel,
		ContentType:       firstNonEmpty(request.ContentType, analysis.taskType),
		Sender:            strings.TrimSpace(request.Sender),
		ReceivedAt:        parseOptionalTime(request.ReceivedAt),
		RawContent:        input,
		NormalizedSummary: compact(input, 420),
		DetectedEntities:  strings.Join(analysis.entities, ","),
		PossibleProject:   firstNonEmpty(projectKey, request.ProjectKey, request.ProjectKeyHint),
		Urgency:           urgencyForPriority(analysis.priority),
	})
	if workflowHasSourceProvenance(*created) {
		if intakeErr != nil {
			return nil, s.blockUnqueuedWorkflowForSourceProvenanceFailure(created, fmt.Errorf("persist source intake record: %w", intakeErr))
		}
		if intakeRecord == nil || intakeRecord.ID == uuid.Nil || intakeRecord.WorkflowID != created.ID ||
			intakeRecord.SourceType != created.SourceType || intakeRecord.SourceID != created.SourceID ||
			intakeRecord.SourceURI != created.SourceURI || intakeRecord.SourceLabel != created.SourceLabel ||
			intakeRecord.RawContent != input {
			return nil, s.blockUnqueuedWorkflowForSourceProvenanceFailure(created, fmt.Errorf("source intake record was not returned with matching durable provenance"))
		}
	}
	for index, checklist := range checklistForAnalysis(analysis) {
		checklistItem := &models.WorkflowChecklistItem{
			WorkflowID:       created.ID,
			Label:            checklist.label,
			Status:           "open",
			Position:         index + 1,
			RequiresApproval: checklist.requiresApproval,
		}
		persisted, checklistErr := s.repo.CreateChecklistItem(checklistItem)
		if checklistErr == nil && (persisted == nil || persisted.ID == uuid.Nil || persisted.WorkflowID != created.ID ||
			persisted.Label != checklistItem.Label || persisted.Status != checklistItem.Status ||
			persisted.Position != checklistItem.Position || persisted.RequiresApproval != checklistItem.RequiresApproval) {
			checklistErr = fmt.Errorf("repository did not confirm the required checklist item")
		}
		if checklistErr != nil {
			return nil, s.blockUnqueuedWorkflowForChecklistFailure(created, checklistErr)
		}
	}
	if analysis.dueAt != nil {
		reminder, reminderErr := s.repo.CreateChecklistItem(&models.WorkflowChecklistItem{
			WorkflowID: created.ID,
			Label:      "Follow up or check before detected deadline",
			Status:     "open",
			Position:   900,
			DueAt:      analysis.dueAt,
			ReminderAt: reminderBefore(*analysis.dueAt),
		})
		if reminderErr == nil && (reminder == nil || reminder.ID == uuid.Nil || reminder.WorkflowID != created.ID || reminder.DueAt == nil || reminder.ReminderAt == nil) {
			reminderErr = fmt.Errorf("repository did not confirm a persisted deadline reminder")
		}
		if reminderErr != nil {
			return nil, s.blockUnqueuedWorkflowForDeadlineReminderFailure(created, reminderErr)
		}
	}
	if err := s.applyMemoryContext(created.ID, input, projectKey, request.OwnerIdentity, firstNonEmpty(request.Actor, "engine")); err != nil {
		return nil, workflowPersistenceFailure("workflow intake learned context", s.blockUnqueuedWorkflowForIntakeContextFailure(created, err))
	}
	if workflowHasSourceProvenance(*created) {
		link, linkErr := s.repo.CreateSourceLink(&models.WorkflowSourceLink{
			WorkflowID:   created.ID,
			SourceType:   created.SourceType,
			SourceID:     created.SourceID,
			SourceURI:    created.SourceURI,
			SourceLabel:  created.SourceLabel,
			Relationship: "origin",
		})
		if linkErr != nil {
			return nil, s.blockUnqueuedWorkflowForSourceProvenanceFailure(created, fmt.Errorf("persist origin source link: %w", linkErr))
		}
		if link == nil || link.ID == uuid.Nil || link.WorkflowID != created.ID ||
			link.SourceType != created.SourceType || link.SourceID != created.SourceID ||
			link.SourceURI != created.SourceURI || link.SourceLabel != created.SourceLabel || link.Relationship != "origin" {
			return nil, s.blockUnqueuedWorkflowForSourceProvenanceFailure(created, fmt.Errorf("origin source link was not returned with matching durable provenance"))
		}
		intakeDecisions = append(intakeDecisions, models.WorkflowDecision{
			WorkflowID: created.ID, DecisionType: "source_link", Decision: "linked",
			Reason: "source provenance captured for workflow", RuleApplied: "source link created at intake",
			Actor: firstNonEmpty(request.Actor, "engine"),
		})
	}
	if projectKey != "" {
		match := &models.WorkflowProjectMatch{
			WorkflowID:     created.ID,
			ProjectKey:     projectKey,
			MatchedBy:      strings.Join(analysis.matchReasons, ", "),
			Confidence:     analysis.projectConfidence,
			TrelloCardRef:  analysis.trelloRef,
			DriveFolderRef: analysis.driveRef,
		}
		persisted, matchErr := s.repo.CreateProjectMatch(match)
		if matchErr != nil || persisted == nil || persisted.ID == uuid.Nil || persisted.WorkflowID != created.ID || persisted.ProjectKey != projectKey {
			if matchErr == nil {
				matchErr = fmt.Errorf("repository did not confirm the project match")
			}
			return nil, workflowPersistenceFailure("workflow intake project match", s.blockUnqueuedWorkflowForIntakeContextFailure(created, matchErr))
		}
		intakeDecisions = append(intakeDecisions, models.WorkflowDecision{
			WorkflowID: created.ID, DecisionType: "project_match", Decision: projectKey,
			Reason: "workflow linked to project context", RuleApplied: strings.Join(analysis.matchReasons, ", "), Actor: "engine",
		})
	} else if strings.TrimSpace(request.ProjectKeyHint) != "" {
		intakeDecisions = append(intakeDecisions, models.WorkflowDecision{
			WorkflowID: created.ID, DecisionType: "project_match", Decision: "unverified",
			Reason:      "source project key was retained only as a hint; no project was matched from the input",
			RuleApplied: strings.Join(analysis.matchReasons, ", "), Actor: firstNonEmpty(request.Actor, "engine"),
		})
	}
	for _, claim := range evidenceClaimsForInput(created.ID, input, request) {
		if evidenceClaimsRequired(analysis.taskType) {
			if err := s.persistRequiredEvidenceClaim(&claim); err != nil {
				return nil, s.blockUnqueuedWorkflowForEvidenceFailure(created, err)
			}
		} else {
			_, _ = s.repo.CreateEvidenceClaim(&claim)
		}
	}
	if loop := openLoopForAnalysis(created.ID, analysis); loop != nil {
		persisted, loopErr := s.repo.CreateOpenLoop(loop)
		if loopErr != nil || persisted == nil || persisted.ID == uuid.Nil || persisted.WorkflowID != created.ID {
			if loopErr == nil {
				loopErr = fmt.Errorf("repository did not confirm the detected follow-up")
			}
			return nil, workflowPersistenceFailure("workflow intake follow-up", s.blockUnqueuedWorkflowForIntakeContextFailure(created, loopErr))
		}
		intakeDecisions = append(intakeDecisions, models.WorkflowDecision{
			WorkflowID: created.ID, DecisionType: "open_loop", Decision: "created",
			Reason: loop.WaitingFor, RuleApplied: "follow-up/open-loop detection", Actor: "engine",
		})
	}
	if proposal := proposalForAnalysis(created.ID, analysis); proposal != nil {
		if created.RequiresApproval || selectionRequired {
			if err := s.persistRequiredProposal(proposal); err != nil {
				return nil, s.blockUnqueuedWorkflowForProposalFailure(created, err)
			}
		} else {
			_, _ = s.repo.CreateProposal(proposal)
		}
	} else if created.RequiresApproval {
		return nil, s.blockUnqueuedWorkflowForProposalFailure(created, fmt.Errorf("approval-required workflow has no proposal"))
	}
	if selectionRequired {
		if err := s.persistRequiredProposal(automationSelectionProposal(created.ID, selectionCandidates)); err != nil {
			return nil, s.blockUnqueuedWorkflowForProposalFailure(created, err)
		}
	}
	if err := s.persistRequiredQualityGates(created.ID, analysis.taskType); err != nil {
		return nil, s.blockUnqueuedWorkflowForQualityGateFailure(
			created,
			"required workflow quality gates could not be durably established",
			err,
		)
	}
	activated := *created
	activated.CurrentState = analysis.initialState
	activated.BlockedReason = analysis.blockedReason
	activated.NextAction = analysis.nextAction
	coordinationProjectionAttempted := false
	var coordinationProjectionErr error
	if s.coordinationProjector != nil && activated.CoordinationPlanID == nil && activated.CoordinationDraftPlanID == nil {
		coordinationProjectionAttempted = true
		checklist, checklistErr := s.repo.FindChecklist(created.ID)
		if checklistErr != nil {
			coordinationProjectionErr = fmt.Errorf("load workflow checklist for coordination draft: %w", checklistErr)
		} else {
			var draft *plangraph.Plan
			draft, coordinationProjectionErr = s.projectWorkflowCoordinationDraft(&activated, checklist)
			if coordinationProjectionErr == nil {
				applyWorkflowCoordinationDraftBinding(&activated, draft)
			}
		}
		if coordinationProjectionErr != nil && activated.CurrentState == StateReady {
			activated.CurrentState = StateBlocked
			activated.BlockedReason = coordinationProjectionFailurePrefix + compact(coordinationProjectionErr.Error(), 420)
			activated.NextAction = "restore workflow plan projection before execution"
		}
	}
	actor := firstNonEmpty(request.Actor, "engine")
	transition := models.WorkflowTransition{
		WorkflowID: created.ID,
		FromState:  StateNewInput,
		ToState:    activated.CurrentState,
		Trigger:    request.Trigger,
		Actor:      actor,
		Reason:     "input classified and workflow state initialized",
	}
	decision := func(decisionType, value, reason, rule, decisionActor string) models.WorkflowDecision {
		return models.WorkflowDecision{
			WorkflowID:   created.ID,
			DecisionType: decisionType,
			Decision:     value,
			Reason:       reason,
			RuleApplied:  rule,
			Actor:        decisionActor,
		}
	}
	decisions := append([]models.WorkflowDecision(nil), intakeDecisions...)
	decisions = append(decisions,
		decision("classification", analysis.taskType, "input classified as "+analysis.taskType, analysis.ruleApplied, "engine"),
		decision("priority", fmt.Sprintf("%d", analysis.priority), "priority assigned from risk, deadline, and task type", "priority engine", "engine"),
	)
	if analysis.requiresApproval {
		decisions = append(decisions, decision("approval_gate", "required", analysis.approvalReason, "approval rule engine", "engine"))
	} else {
		decisions = append(decisions, decision("approval_gate", "not_required", "low-risk workflow can enter worker queue", "approval rule engine", "engine"))
	}
	if strings.TrimSpace(created.AutomationID) != "" {
		decisions = append(decisions, decision("automation_selection", created.AutomationID, automationSelectionReason, "conservative automation capability match", "engine"))
	} else if selectionRequired {
		decisions = append(decisions, decision("automation_selection", "needs_review", automationSelectionReason, "conservative automation capability match", "engine"))
	}
	if analysis.blockedReason != "" {
		decisions = append(decisions, decision("missing_info", "blocked", analysis.blockedReason, "missing information detection", "engine"))
	}
	if analysis.dueAt != nil {
		decisions = append(decisions, decision("deadline_reminder", "created", "check reminder created from detected deadline", "deadline detection", "engine"))
	}
	if coordinationProjectionAttempted {
		if coordinationProjectionErr == nil && activated.CoordinationDraftPlanID != nil {
			decisions = append(decisions, decision("coordination_plan", "drafted", "immutable advisory coordination draft linked before workflow activation", "plan graph projection", actor))
		} else {
			decisions = append(decisions, decision("coordination_plan", "unavailable", "workflow execution remains gated until an advisory coordination draft is durable", "plan graph projection", actor))
		}
	}
	events := make([]models.WorkflowEvent, 0, 4)
	if intakeRecord != nil {
		events = append(events, models.WorkflowEvent{
			WorkflowID:  created.ID,
			EventType:   "workflow.intake_normalized",
			Message:     "input normalized from " + firstNonEmpty(request.SourceType, "manual"),
			Trigger:     request.Trigger,
			RuleApplied: "universal intake engine",
			SourceURI:   request.SourceURI,
			Actor:       actor,
		})
	}
	events = append(events, models.WorkflowEvent{
		WorkflowID:  created.ID,
		EventType:   "workflow.intake",
		ToState:     activated.CurrentState,
		Message:     "input classified and workflow state initialized",
		Trigger:     request.Trigger,
		RuleApplied: analysis.ruleApplied,
		SourceURI:   request.SourceURI,
		Actor:       actor,
	})
	if coordinationProjectionAttempted {
		if coordinationProjectionErr == nil && activated.CoordinationDraftPlanID != nil {
			events = append(events, models.WorkflowEvent{
				WorkflowID: created.ID, EventType: "workflow.coordination_draft_projected",
				ToState: activated.CurrentState, Message: workflowCoordinationDraftProjectedMessage(*activated.CoordinationDraftPlanID),
				Trigger: request.Trigger, RuleApplied: "plan graph projection", Actor: actor,
			})
		} else {
			events = append(events, models.WorkflowEvent{
				WorkflowID: created.ID, EventType: "workflow.coordination_draft_failed",
				FromState: analysis.initialState, ToState: activated.CurrentState,
				Message: "workflow retained; execution requires a durable advisory coordination draft",
				Trigger: request.Trigger, RuleApplied: "plan graph projection", Actor: actor,
			})
		}
	}
	updated, changed, err := s.repo.CommitWorkflowIntake(WorkflowIntakeFinalization{
		Expected:   created,
		Updated:    &activated,
		Transition: transition,
		Decisions:  decisions,
		Events:     events,
	})
	if err != nil {
		blockErr := s.blockUnqueuedWorkflowForQualityGateFailure(
			created,
			"workflow could not be safely activated after intake finalization failed",
			err,
		)
		return nil, workflowPersistenceFailure("workflow intake finalization", errors.Join(err, blockErr))
	}
	if !changed || updated == nil {
		if current, findErr := s.repo.FindItem(created.ID); findErr == nil && current != nil {
			if quarantineErr := s.ensureWorkflowSourceNotRetracted(current); quarantineErr != nil {
				return nil, quarantineErr
			}
		}
		return nil, ErrWorkflowIntakeConcurrentChange
	}
	created = updated
	s.projectWorkflowTransition(created.ID, StateNewInput, created.CurrentState, request.Trigger)
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

func (s *service) applyMemoryContext(workflowID uuid.UUID, input, projectKey, ownerIdentity, actor string) error {
	if s.memoryService == nil {
		return nil
	}
	result, err := memory.RetrieveForOwner(s.memoryService, ownerIdentity, memory.RetrieveRequest{
		Query:      input,
		ProjectKey: projectKey,
		Limit:      3,
	})
	if err != nil {
		return s.audit(workflowID, "workflow.memory_context_failed", "", "", err.Error(), "memory_retrieval", "context planning layer", "", actor)
	}
	if result == nil {
		return fmt.Errorf("memory retrieval returned no context result")
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
		checklist, err := s.repo.CreateChecklistItem(&models.WorkflowChecklistItem{
			WorkflowID: workflowID,
			Label:      "Apply learned context: " + lesson,
			Status:     "open",
			Position:   801 + applied,
		})
		if err != nil {
			return fmt.Errorf("persist learned-context checklist: %w", err)
		}
		if checklist == nil || checklist.ID == uuid.Nil || checklist.WorkflowID != workflowID || checklist.Label != "Apply learned context: "+lesson || checklist.Status != "open" {
			return fmt.Errorf("repository did not confirm learned-context checklist")
		}
		if err := s.linkSource(
			workflowID,
			"memory",
			mem.ID.String(),
			workflowMemorySourceURI(mem),
			firstNonEmpty(mem.SourceLabel, mem.Summary, "Context memory"),
			"planning_context",
		); err != nil {
			return fmt.Errorf("persist learned-context provenance: %w", err)
		}
		applied++
		summaries = append(summaries, lesson)
	}
	if applied == 0 {
		return nil
	}
	summary := compactWorkflowText(strings.Join(summaries, "; "), 420)
	if err := s.decide(
		workflowID,
		"memory_context",
		"applied",
		fmt.Sprintf("applied %d relevant memory record(s): %s", applied, summary),
		firstNonEmpty(result.Explanation, "context planning layer"),
		false,
		actor,
	); err != nil {
		return fmt.Errorf("persist learned-context decision: %w", err)
	}
	return s.audit(
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
	if item == nil {
		return fmt.Errorf("%w: workflow item is required", ErrWorkflowSourceSupersessionConflict)
	}
	if item.CurrentState == StateInProgress || item.WorkerClaimID != "" {
		return fmt.Errorf("%w: source workflow is currently in progress and cannot be superseded until execution review is complete", ErrWorkflowSourceSupersessionConflict)
	}
	superseder, supported := s.repo.(interface {
		SupersedeSourceWorkflowCAS(SourceWorkflowSupersession) (bool, error)
	})
	if !supported {
		return fmt.Errorf("%w: repository does not support conditional source supersession", ErrWorkflowSourceSupersessionConflict)
	}
	from := item.CurrentState
	superseded, err := superseder.SupersedeSourceWorkflowCAS(SourceWorkflowSupersession{
		ID:                     item.ID,
		OwnerIdentity:          item.OwnerIdentity,
		SourceType:             item.SourceType,
		SourceID:               item.SourceID,
		ExpectedSourceRevision: item.SourceRevision,
		ExpectedCurrentState:   from,
	})
	if err != nil {
		return fmt.Errorf("conditionally supersede source workflow: %w", err)
	}
	if !superseded {
		return fmt.Errorf("%w: workflow changed, was claimed, or is no longer active; replacement workflow was not created", ErrWorkflowSourceSupersessionConflict)
	}
	item.CurrentState = StateArchived
	item.Archived = true
	item.NextAction = "superseded by revised source content"
	item.NextRunAt = nil
	item.WorkerClaimID = ""
	item.WorkerLeaseUntil = nil
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
		if err := s.audit(item.ID, "workflow.source_retracted_after_completion", item.CurrentState, item.CurrentState, reason, "source_retraction", "completed workflow retained for audit", item.SourceURI, "source-worker"); err != nil {
			return fmt.Errorf("completed workflow was retained, but source-retraction audit could not be persisted: %w", err)
		}
		return nil
	}
	from := item.CurrentState
	expected := *item
	item.CurrentState = StateBlocked
	item.BlockedReason = reason
	item.NextAction = "review the retracted source record before any further execution"
	item.NextRunAt = nil
	item.WorkerClaimID = ""
	item.WorkerLeaseUntil = nil
	item.VerificationStatus = "needs_review"
	quarantineRetractedWorkflow(item, reason)
	updated, changed, err := s.repo.UpdateWorkflowItemCAS(&expected, item)
	if err != nil {
		return err
	}
	if !changed || updated == nil {
		return fmt.Errorf("source workflow changed while retraction was being applied; reload and retry")
	}
	var auditErrors []error
	if err := s.recordTransition(updated.ID, from, StateBlocked, "source_retraction", "source-worker", false, reason); err != nil {
		auditErrors = append(auditErrors, fmt.Errorf("persist source-retraction transition: %w", err))
	}
	if err := s.decide(updated.ID, "source_retraction", "blocked", reason, "source-derived work must stop when its evidence is retracted", false, "source-worker"); err != nil {
		auditErrors = append(auditErrors, fmt.Errorf("persist source-retraction decision: %w", err))
	}
	if err := s.audit(updated.ID, "workflow.source_retracted", from, StateBlocked, reason, "source_retraction", "source identity retraction", updated.SourceURI, "source-worker"); err != nil {
		auditErrors = append(auditErrors, fmt.Errorf("persist source-retraction audit event: %w", err))
	}
	if len(auditErrors) > 0 {
		return fmt.Errorf("source workflow was quarantined, but its audit history is incomplete: %w", workflowAuditPersistenceFailure("source retraction", errors.Join(auditErrors...)))
	}
	return nil
}

func quarantineRetractedWorkflow(item *models.WorkflowItem, reason string) {
	if item == nil {
		return
	}
	item.RequiresApproval = true
	item.ApprovalStatus = "pending"
	item.ApprovalReason = sourceRetractionQuarantinePrefix + firstNonEmpty(strings.TrimSpace(reason), "source record was retracted")
	item.AutonomyLevel = "approve_before_execute"
	item.NextAction = "restore a valid source revision and create a new workflow before execution"
}

func (s *service) ensureWorkflowSourceNotRetracted(item *models.WorkflowItem) error {
	if item == nil {
		return fmt.Errorf("workflow item is required")
	}
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(item.ApprovalReason)), sourceRetractionQuarantinePrefix) {
		return fmt.Errorf("%w: restore a valid source revision and create a new workflow before execution", ErrWorkflowSourceRetracted)
	}
	if !workflowHasSourceProvenance(*item) {
		return nil
	}
	events, err := s.repo.FindEvents(item.ID)
	if err != nil {
		return fmt.Errorf("check workflow source-retraction status: %w", err)
	}
	for _, event := range events {
		if event.EventType == "workflow.source_retracted" {
			return fmt.Errorf("%w: restore a valid source revision and create a new workflow before execution", ErrWorkflowSourceRetracted)
		}
	}
	return nil
}

func (s *service) Dashboard() (*WorkflowDashboard, error) {
	return s.DashboardForOwner("")
}

func (s *service) DashboardForOwner(ownerIdentity string) (*WorkflowDashboard, error) {
	_ = s.ensureDefaultRules()
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
	if request.Approved {
		return nil, fmt.Errorf("approval must be resolved through the pending approval action")
	}
	target := strings.TrimSpace(request.TargetState)
	if target == "" {
		return nil, fmt.Errorf("targetState is required")
	}
	if target == StateCompleted {
		return nil, fmt.Errorf("manual completion is not allowed; completion requires verified worker output or interrupted-execution resolution")
	}
	if target != StateBlocked && target != StateArchived {
		if err := s.ensureWorkflowSourceNotRetracted(item); err != nil {
			return nil, err
		}
	}
	if item.RecoveryStatus == RecoveryNeedsReview && target != StateBlocked {
		return nil, fmt.Errorf("interrupted execution must be resolved before changing workflow state")
	}
	if target == StateReady && item.RequiresApproval && item.ApprovalStatus != "approved" {
		return nil, fmt.Errorf("approval is required before workflow can become ready")
	}
	if target == StateReady && s.coordinationProjector != nil && item.CoordinationPlanID == nil {
		if err := s.ensureWorkflowCoordinationDraft(item, firstNonEmpty(request.Actor, "operator")); err != nil {
			return nil, err
		}
		item, err = s.repo.FindItem(id)
		if err != nil {
			return nil, err
		}
		if item.CurrentState != target {
			return nil, fmt.Errorf("workflow state changed while binding the coordination draft; reload before trying again")
		}
	}
	if !transitionAllowed(item.CurrentState, target, request.Approved) {
		return nil, fmt.Errorf("transition from %s to %s is not allowed", item.CurrentState, target)
	}
	expected := *item
	from := item.CurrentState
	item.CurrentState = target
	if target == StateNeedsApproval {
		item.RequiresApproval = true
		item.ApprovalStatus = "pending"
		item.ApprovalReason = firstNonEmpty(item.ApprovalReason, "manual review requested")
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
	actor := firstNonEmpty(request.Actor, "operator")
	transition := models.WorkflowTransition{
		WorkflowID: expected.ID, FromState: from, ToState: target,
		Trigger: "manual_transition", Actor: actor, Approved: false, Reason: request.Message,
	}
	event := models.WorkflowEvent{
		WorkflowID: expected.ID, EventType: "workflow.transition", FromState: from, ToState: target,
		Message: request.Message, Trigger: "manual_transition", RuleApplied: approvalRule(false),
		SourceURI: item.SourceURI, Actor: actor,
	}
	updated, changed, err := s.repo.CommitManualWorkflowTransition(WorkflowTransitionFinalization{
		Expected: &expected, Updated: item, Transition: transition, Event: event,
	})
	if err != nil {
		return nil, err
	}
	if !changed || updated == nil {
		return nil, fmt.Errorf("workflow changed while applying transition; reload before trying again")
	}
	s.projectWorkflowTransition(updated.ID, from, target, "manual_transition")
	return s.Get(updated.ID)
}

func (s *service) ResolveApproval(id uuid.UUID, request ApprovalResolutionRequest) (*WorkflowRecord, error) {
	item, err := s.repo.FindItem(id)
	if err != nil {
		return nil, err
	}
	if item.Archived || item.CurrentState == StateArchived || item.CurrentState == StateCompleted {
		return nil, fmt.Errorf("workflow approval is not pending: workflow is completed or archived")
	}
	if item.RecoveryStatus == RecoveryNeedsReview {
		return nil, fmt.Errorf("interrupted execution must be resolved before approval can continue")
	}
	if request.Approved {
		if err := s.ensureWorkflowSourceNotRetracted(item); err != nil {
			return nil, err
		}
	}
	if !item.RequiresApproval || item.ApprovalStatus != "pending" || item.CurrentState != StateNeedsApproval {
		return nil, fmt.Errorf("workflow approval is not pending")
	}
	if request.Approved && s.coordinationProjector != nil && item.CoordinationPlanID == nil {
		if err := s.ensureWorkflowCoordinationDraft(item, firstNonEmpty(request.Actor, "operator")); err != nil {
			return nil, err
		}
		item, err = s.repo.FindItem(id)
		if err != nil {
			return nil, err
		}
		if !item.RequiresApproval || item.ApprovalStatus != "pending" || item.CurrentState != StateNeedsApproval {
			return nil, fmt.Errorf("workflow approval changed while binding the coordination draft")
		}
	}
	actor := firstNonEmpty(request.Actor, "operator")
	message := strings.TrimSpace(request.Note)
	decisionRule := "manual approval gate"
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
		decisionRule, err = s.approvalDecisionRule(item)
		if err != nil {
			return nil, err
		}
		message = firstNonEmpty(message, "workflow approved for controlled execution")
	} else {
		message = firstNonEmpty(message, "approval rejected")
	}
	updated, resolved, err := s.repo.ResolvePendingApproval(id, ApprovalResolutionMutation{
		Approved:        request.Approved,
		RejectionReason: message,
		Actor:           actor,
		DecisionRule:    decisionRule,
	})
	if err != nil {
		return nil, workflowAuditPersistenceFailure("approval transition, decision, or event", err)
	}
	if !resolved || updated == nil {
		return nil, fmt.Errorf("workflow approval is no longer pending")
	}
	from := item.CurrentState
	to := updated.CurrentState
	s.projectWorkflowTransition(updated.ID, from, to, "approval_resolution")
	if request.Approved {
		s.rememberCorrection(updated, "approval_approved", request.Note, actor)
	} else {
		s.rememberCorrection(updated, "approval_rejected", updated.BlockedReason, actor)
	}
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
	if decision == "retry" {
		if err := s.ensureWorkflowSourceNotRetracted(item); err != nil {
			return nil, err
		}
	}
	note := strings.TrimSpace(request.Note)
	actor := firstNonEmpty(request.Actor, "operator")
	if note == "" {
		return nil, fmt.Errorf("note is required to resolve interrupted execution")
	}
	if decision == "retry" || decision == "confirm_completed" {
		if !request.PriorExecutionReconciled {
			return nil, fmt.Errorf("resolution requires explicit confirmation that the prior execution terminated and its external outcome was reconciled")
		}
		if strings.TrimSpace(request.EvidenceURI) == "" {
			return nil, fmt.Errorf("resolution requires a source link documenting the prior execution reconciliation")
		}
		if _, active := s.activeTaskRuns.Load(id); active {
			return nil, fmt.Errorf("prior workflow task execution is still active in this process; resolution remains blocked until it terminates")
		}
	}

	from := item.CurrentState
	expected := *item
	var sourceLink *models.WorkflowSourceLink
	var evidenceClaim *models.WorkflowEvidenceClaim
	var qualityGate *models.WorkflowQualityGate
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
		evidenceLabel := firstNonEmpty(request.EvidenceLabel, "Prior execution reconciliation evidence")
		sourceLink = &models.WorkflowSourceLink{
			WorkflowID: item.ID, SourceType: "recovery_evidence",
			SourceURI: strings.TrimSpace(request.EvidenceURI), SourceLabel: evidenceLabel,
			Relationship: "execution_reconciliation",
		}
		evidenceClaim = &models.WorkflowEvidenceClaim{
			WorkflowID: item.ID,
			ClaimText:  "Operator attests the prior execution terminated and its external outcome was reconciled before retry: " + note,
			SourceURI:  strings.TrimSpace(request.EvidenceURI), SourceLabel: evidenceLabel,
			Reliability: "operator_attestation", Status: "human_approved", NeedsReview: false,
		}
	case "confirm_completed":
		evidenceURI := strings.TrimSpace(request.EvidenceURI)
		if evidenceURI == "" {
			return nil, fmt.Errorf("evidenceUri is required to confirm interrupted execution completed")
		}
		if evidenceURI == strings.TrimSpace(item.SourceURI) {
			return nil, fmt.Errorf("completion evidence must be separate from the workflow source")
		}
		evidenceLabel := firstNonEmpty(request.EvidenceLabel, "Interrupted execution completion evidence")
		sourceLink = &models.WorkflowSourceLink{
			WorkflowID: item.ID, SourceType: "recovery_evidence", SourceURI: evidenceURI,
			SourceLabel: evidenceLabel, Relationship: "completion_evidence",
		}
		evidenceClaim = &models.WorkflowEvidenceClaim{
			WorkflowID: item.ID, ClaimText: note, SourceURI: evidenceURI,
			SourceLabel: evidenceLabel, Reliability: "operator_attestation",
			Status: "human_approved", NeedsReview: false,
		}
		qualityGate = &models.WorkflowQualityGate{
			WorkflowID: item.ID, Gate: "verification before completion", Status: "passed",
			Reason: "operator confirmed interrupted execution outcome with linked evidence",
		}
		now := time.Now().UTC()
		item.CurrentState = StateCompleted
		item.CompletedAt = &now
		item.RecoveryStatus = RecoveryCompletionConfirmed
		item.RecoveryNote = note
		item.VerificationStatus = "human_approved"
		item.RequiresApproval = false
		item.ApprovalStatus = approvalStatus(false)
		item.ApprovalReason = ""
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

	updated, resolved, err := s.repo.ResolveInterruptedExecutionCAS(&expected, item, sourceLink, evidenceClaim, qualityGate)
	if err != nil {
		return nil, err
	}
	if !resolved || updated == nil {
		return nil, fmt.Errorf("workflow interruption resolution was superseded; reload before deciding again")
	}
	approved := decision == "confirm_completed"
	var auditErrors []error
	if err := s.recordTransition(updated.ID, from, updated.CurrentState, "interrupted_execution_resolution", actor, approved, note); err != nil {
		auditErrors = append(auditErrors, fmt.Errorf("persist interruption-resolution transition: %w", err))
	}
	if err := s.decide(updated.ID, "interrupted_execution", decision, note, "unknown external side effects require explicit operator resolution", approved, actor); err != nil {
		auditErrors = append(auditErrors, fmt.Errorf("persist interruption-resolution decision: %w", err))
	}
	if err := s.audit(updated.ID, "workflow.interruption_resolved", from, updated.CurrentState, note, "interrupted_execution_resolution", decision, firstNonEmpty(request.EvidenceURI, updated.SourceURI), actor); err != nil {
		auditErrors = append(auditErrors, fmt.Errorf("persist interruption-resolution audit event: %w", err))
	}
	if len(auditErrors) > 0 {
		return nil, fmt.Errorf("interrupted workflow state was updated, but its audit history is incomplete: %w", workflowAuditPersistenceFailure("interruption resolution", errors.Join(auditErrors...)))
	}
	if decision == "confirm_completed" {
		if err := s.markChecklistProgress(updated.ID, "Verify completion before closing"); err != nil {
			return nil, fmt.Errorf("interrupted workflow completion was persisted, but checklist progress requires review: %w", workflowAuditPersistenceFailure("completion checklist", err))
		}
	}
	s.rememberCorrection(updated, "interruption_"+decision, note, actor)
	return s.Get(updated.ID)
}

func (s *service) ResolveProposal(id uuid.UUID, proposalID uuid.UUID, request ProposalResolutionRequest) (*WorkflowRecord, error) {
	if repository, ok := s.repo.(*GormRepository); ok && repository != nil && repository.DB != nil &&
		repository.DB.Dialector != nil && repository.DB.Dialector.Name() == "postgres" {
		return s.resolveProposalPostgres(repository, id, proposalID, request)
	}
	return s.resolveProposalWithGenericRepository(id, proposalID, request)
}

func (s *service) resolveProposalPostgres(
	repository *GormRepository,
	workflowID uuid.UUID,
	proposalID uuid.UUID,
	request ProposalResolutionRequest,
) (*WorkflowRecord, error) {
	status, err := normalizeProposalStatus(request)
	if err != nil {
		return nil, err
	}
	actor := firstNonEmpty(request.Actor, "operator")
	note := strings.TrimSpace(request.Note)
	type postCommitEffects struct {
		transition bool
		from       string
		to         string
		trigger    string
		correction string
		note       string
		actor      string
	}
	var effects postCommitEffects

	updated, resolved, err := repository.resolveProposalAtomically(
		workflowID,
		proposalID,
		func(txRepository *GormRepository, item *models.WorkflowItem, proposal *models.WorkflowProposal) (*atomicProposalResolution, error) {
			if item.Archived || item.CurrentState == StateArchived || item.CurrentState == StateCompleted {
				return nil, fmt.Errorf("closed workflows cannot resolve proposals")
			}
			if item.RecoveryStatus == RecoveryNeedsReview {
				return nil, fmt.Errorf("interrupted execution must be resolved before proposals can change workflow state")
			}

			transactionalService := &service{repo: txRepository}
			if status != "rejected" {
				if err := transactionalService.ensureWorkflowSourceNotRetracted(item); err != nil {
					return nil, err
				}
			}

			original := *item
			updatedItem := *item
			itemChanged := false
			selectionOnly := status == "approved" && isAutomationSelectionProposal(proposal) && isAutomationSelectionOnlyApproval(item)
			decisions := make([]models.WorkflowDecision, 0, 3)
			transitions := make([]models.WorkflowTransition, 0, 1)
			events := make([]models.WorkflowEvent, 0, 2)
			transitionTrigger := ""
			correctionSignal := ""
			correctionNote := ""

			var selectionDecisionRule string
			if status == "approved" && isAutomationSelectionProposal(proposal) {
				automationID, selectionErr := selectedAutomationID(proposal.Options, request.SelectedOption)
				if selectionErr != nil {
					return nil, selectionErr
				}
				updatedItem.AutomationID = automationID
				if selectionOnly {
					updatedItem.RequiresApproval = false
					updatedItem.ApprovalStatus = approvalStatus(false)
					updatedItem.ApprovalReason = ""
					updatedItem.AutonomyLevel = "autonomous_safe"
					updatedItem.NextAction = "run the selected low-risk automation through workflow worker"
				} else {
					updatedItem.NextAction = "approve the exact automation action and queue controlled execution"
				}
				if updatedItem.RequiresApproval {
					selectionDecisionRule, err = s.approvalDecisionRule(&updatedItem)
					if err != nil {
						return nil, fmt.Errorf("selected automation cannot be bound to approval: %w", err)
					}
				}
				itemChanged = true
				decisions = append(decisions, models.WorkflowDecision{
					WorkflowID: workflowID, DecisionType: "automation_selection", Decision: automationID,
					Reason: "operator selected the exact automation candidate", RuleApplied: "runtime-selection proposal",
					Approved: true, Actor: actor,
				})
			}

			proposalReason := firstNonEmpty(note, proposal.RecommendedAction)
			decisions = append(decisions, models.WorkflowDecision{
				WorkflowID: workflowID, DecisionType: "proposal", Decision: status,
				Reason: proposalReason, RuleApplied: "proposal/yes-no engine",
				Approved: status == "approved", Actor: actor,
			})

			switch status {
			case "approved":
				if updatedItem.RequiresApproval {
					if updatedItem.ApprovalStatus != "pending" || updatedItem.CurrentState != StateNeedsApproval {
						return nil, fmt.Errorf("workflow approval is not pending")
					}
					proposals, proposalErr := txRepository.FindProposals(workflowID)
					if proposalErr != nil {
						return nil, proposalErr
					}
					for index := range proposals {
						if proposals[index].ID != proposal.ID && proposals[index].Status == "open" && isAutomationSelectionProposal(&proposals[index]) {
							return nil, fmt.Errorf("select the exact automation candidate before approving controlled execution")
						}
					}
					decisionRule := selectionDecisionRule
					if decisionRule == "" {
						decisionRule, err = s.approvalDecisionRule(&updatedItem)
						if err != nil {
							return nil, err
						}
					}
					approvalNote := firstNonEmpty(note, proposal.RecommendedAction)
					from := updatedItem.CurrentState
					updatedItem.CurrentState = StateReady
					updatedItem.ApprovalStatus = "approved"
					updatedItem.BlockedReason = ""
					updatedItem.NextAction = "execute approved workflow steps"
					itemChanged = true
					transitions = append(transitions, models.WorkflowTransition{
						WorkflowID: workflowID, FromState: from, ToState: StateReady,
						Trigger: "approval_resolution", Actor: actor, Approved: true, Reason: approvalNote,
					})
					decisions = append(decisions, models.WorkflowDecision{
						WorkflowID: workflowID, DecisionType: "approval", Decision: "approved",
						Reason: approvalNote, RuleApplied: decisionRule, Approved: true, Actor: actor,
					})
					events = append(events, models.WorkflowEvent{
						WorkflowID: workflowID, EventType: "workflow.approval", FromState: from, ToState: StateReady,
						Message: approvalNote, Trigger: "approval_resolution", RuleApplied: decisionRule,
						SourceURI: item.SourceURI, Actor: actor,
					})
					transitionTrigger = "approval_resolution"
					correctionSignal = "approval_approved"
					correctionNote = note
				} else if updatedItem.CurrentState == StateNeedsApproval || updatedItem.CurrentState == StateBlocked || updatedItem.CurrentState == StateWaitingInput {
					from := updatedItem.CurrentState
					updatedItem.CurrentState = StateReady
					updatedItem.BlockedReason = ""
					if selectionOnly {
						updatedItem.NextAction = "run the selected low-risk automation through workflow worker"
					} else {
						updatedItem.NextAction = "execute approved proposal through workflow worker"
					}
					itemChanged = true
					reason := firstNonEmpty(note, "proposal approved")
					transitions = append(transitions, models.WorkflowTransition{
						WorkflowID: workflowID, FromState: from, ToState: StateReady,
						Trigger: "proposal_resolution", Actor: actor, Approved: !selectionOnly, Reason: reason,
					})
					rule := "proposal approved"
					if selectionOnly {
						rule = "low-risk automation selection completed without action approval"
					}
					events = append(events, models.WorkflowEvent{
						WorkflowID: workflowID, EventType: "workflow.transition", FromState: from, ToState: StateReady,
						Message: "proposal approved and workflow made ready", Trigger: "proposal_resolution",
						RuleApplied: rule, SourceURI: item.SourceURI, Actor: actor,
					})
					transitionTrigger = "proposal_resolution"
				}
			case "changes_requested":
				from := updatedItem.CurrentState
				updatedItem.CurrentState = StateWaitingInput
				updatedItem.NextAction = firstNonEmpty(note, "apply requested proposal changes")
				updatedItem.BlockedReason = ""
				itemChanged = true
				transitions = append(transitions, models.WorkflowTransition{
					WorkflowID: workflowID, FromState: from, ToState: StateWaitingInput,
					Trigger: "proposal_resolution", Actor: actor, Reason: updatedItem.NextAction,
				})
				events = append(events, models.WorkflowEvent{
					WorkflowID: workflowID, EventType: "workflow.transition", FromState: from, ToState: StateWaitingInput,
					Message: updatedItem.NextAction, Trigger: "proposal_resolution",
					RuleApplied: "proposal changes requested", SourceURI: item.SourceURI, Actor: actor,
				})
				transitionTrigger = "proposal_resolution"
				correctionSignal = "proposal_changes_requested"
				correctionNote = firstNonEmpty(note, request.SelectedOption)
			case "rejected":
				from := updatedItem.CurrentState
				updatedItem.CurrentState = StateBlocked
				updatedItem.ApprovalStatus = "rejected"
				updatedItem.BlockedReason = firstNonEmpty(note, "proposal rejected")
				updatedItem.NextAction = "review rejected proposal before continuing"
				itemChanged = true
				transitions = append(transitions, models.WorkflowTransition{
					WorkflowID: workflowID, FromState: from, ToState: StateBlocked,
					Trigger: "proposal_resolution", Actor: actor, Reason: updatedItem.BlockedReason,
				})
				events = append(events, models.WorkflowEvent{
					WorkflowID: workflowID, EventType: "workflow.transition", FromState: from, ToState: StateBlocked,
					Message: updatedItem.BlockedReason, Trigger: "proposal_resolution",
					RuleApplied: "proposal rejected", SourceURI: item.SourceURI, Actor: actor,
				})
				transitionTrigger = "proposal_resolution"
				correctionSignal = "proposal_rejected"
				correctionNote = updatedItem.BlockedReason
			}

			events = append(events, models.WorkflowEvent{
				WorkflowID: workflowID, EventType: "workflow.proposal",
				FromState: original.CurrentState, ToState: updatedItem.CurrentState,
				Message: "proposal " + status + ": " + proposal.RecommendedAction,
				Trigger: "proposal_resolution", RuleApplied: "proposal/yes-no engine",
				SourceURI: item.SourceURI, Actor: actor,
			})

			var changedItem *models.WorkflowItem
			if itemChanged {
				changedItem = &updatedItem
			}
			resolution := &atomicProposalResolution{
				Status: status, SelectedOption: strings.TrimSpace(request.SelectedOption),
				ResolutionNote: note, ResolvedBy: actor, ResolvedAt: time.Now().UTC(),
				UpdatedItem: changedItem, Transitions: transitions, Decisions: decisions, Events: events,
			}
			effects = postCommitEffects{
				transition: len(transitions) > 0, from: original.CurrentState,
				to: updatedItem.CurrentState, trigger: transitionTrigger,
				correction: correctionSignal, note: correctionNote, actor: actor,
			}
			return resolution, nil
		},
	)
	if err != nil {
		return nil, err
	}
	if !resolved || updated == nil {
		return nil, errWorkflowProposalConflict
	}

	if effects.transition {
		s.projectWorkflowTransition(workflowID, effects.from, effects.to, effects.trigger)
	}
	if effects.correction != "" {
		s.rememberCorrection(updated, effects.correction, effects.note, effects.actor)
	}
	return s.Get(workflowID)
}

func (s *service) resolveProposalWithGenericRepository(id uuid.UUID, proposalID uuid.UUID, request ProposalResolutionRequest) (*WorkflowRecord, error) {
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
	if status != "rejected" {
		if err := s.ensureWorkflowSourceNotRetracted(item); err != nil {
			return nil, err
		}
	}
	selectionOnly := status == "approved" && isAutomationSelectionProposal(proposal) && isAutomationSelectionOnlyApproval(item)
	if status == "approved" && isAutomationSelectionProposal(proposal) {
		automationID, selectionErr := selectedAutomationID(proposal.Options, request.SelectedOption)
		if selectionErr != nil {
			return nil, selectionErr
		}
		expected := *item
		item.AutomationID = automationID
		if selectionOnly {
			item.RequiresApproval = false
			item.ApprovalStatus = approvalStatus(false)
			item.ApprovalReason = ""
			item.AutonomyLevel = "autonomous_safe"
			item.NextAction = "run the selected low-risk automation through workflow worker"
		} else {
			item.NextAction = "approve the exact automation action and queue controlled execution"
		}
		if item.RequiresApproval {
			if _, bindingErr := s.approvalDecisionRule(item); bindingErr != nil {
				return nil, fmt.Errorf("selected automation cannot be bound to approval: %w", bindingErr)
			}
		}
		updated, changed, err := s.repo.UpdateWorkflowItemCAS(&expected, item)
		if err != nil {
			return nil, err
		}
		if !changed || updated == nil {
			return nil, fmt.Errorf("workflow changed while resolving proposal; reload before deciding again")
		}
		item = updated
		if err := s.decide(id, "automation_selection", automationID, "operator selected the exact automation candidate", "runtime-selection proposal", true, firstNonEmpty(request.Actor, "operator")); err != nil {
			return nil, workflowAuditPersistenceFailure("automation selection decision", err)
		}
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
	if err := s.decide(id, "proposal", status, firstNonEmpty(request.Note, proposal.RecommendedAction), "proposal/yes-no engine", status == "approved", firstNonEmpty(request.Actor, "operator")); err != nil {
		return nil, workflowAuditPersistenceFailure("proposal decision", err)
	}
	if err := s.audit(id, "workflow.proposal", "", item.CurrentState, "proposal "+status+": "+proposal.RecommendedAction, "proposal_resolution", "proposal/yes-no engine", item.SourceURI, firstNonEmpty(request.Actor, "operator")); err != nil {
		return nil, workflowAuditPersistenceFailure("proposal event", err)
	}

	switch status {
	case "approved":
		if item.RequiresApproval {
			return s.ResolveApproval(id, ApprovalResolutionRequest{
				Approved: true,
				Note:     firstNonEmpty(request.Note, proposal.RecommendedAction),
				Actor:    firstNonEmpty(request.Actor, "operator"),
			})
		}
		if item.CurrentState == StateNeedsApproval || item.CurrentState == StateBlocked || item.CurrentState == StateWaitingInput {
			expected := *item
			from := item.CurrentState
			item.CurrentState = StateReady
			item.BlockedReason = ""
			if selectionOnly {
				item.NextAction = "run the selected low-risk automation through workflow worker"
			} else {
				item.NextAction = "execute approved proposal through workflow worker"
			}
			updated, changed, err := s.repo.UpdateWorkflowItemCAS(&expected, item)
			if err != nil {
				return nil, err
			}
			if !changed || updated == nil {
				return nil, fmt.Errorf("workflow changed while resolving proposal; reload before deciding again")
			}
			item = updated
			if err := s.recordTransition(item.ID, from, StateReady, "proposal_resolution", firstNonEmpty(request.Actor, "operator"), !selectionOnly, firstNonEmpty(request.Note, "proposal approved")); err != nil {
				return nil, workflowAuditPersistenceFailure("proposal transition record", err)
			}
			rule := "proposal approved"
			if selectionOnly {
				rule = "low-risk automation selection completed without action approval"
			}
			if err := s.audit(item.ID, "workflow.transition", from, StateReady, "proposal approved and workflow made ready", "proposal_resolution", rule, item.SourceURI, firstNonEmpty(request.Actor, "operator")); err != nil {
				return nil, workflowAuditPersistenceFailure("proposal transition event", err)
			}
		}
	case "changes_requested":
		expected := *item
		from := item.CurrentState
		item.CurrentState = StateWaitingInput
		item.NextAction = firstNonEmpty(request.Note, "apply requested proposal changes")
		item.BlockedReason = ""
		updated, changed, err := s.repo.UpdateWorkflowItemCAS(&expected, item)
		if err != nil {
			return nil, err
		}
		if !changed || updated == nil {
			return nil, fmt.Errorf("workflow changed while resolving proposal; reload before deciding again")
		}
		item = updated
		if err := s.recordTransition(item.ID, from, StateWaitingInput, "proposal_resolution", firstNonEmpty(request.Actor, "operator"), false, item.NextAction); err != nil {
			return nil, workflowAuditPersistenceFailure("proposal transition record", err)
		}
		s.rememberCorrection(item, "proposal_changes_requested", firstNonEmpty(request.Note, request.SelectedOption), firstNonEmpty(request.Actor, "operator"))
	case "rejected":
		expected := *item
		from := item.CurrentState
		item.CurrentState = StateBlocked
		item.ApprovalStatus = "rejected"
		item.BlockedReason = firstNonEmpty(request.Note, "proposal rejected")
		item.NextAction = "review rejected proposal before continuing"
		updated, changed, err := s.repo.UpdateWorkflowItemCAS(&expected, item)
		if err != nil {
			return nil, err
		}
		if !changed || updated == nil {
			return nil, fmt.Errorf("workflow changed while resolving proposal; reload before deciding again")
		}
		item = updated
		if err := s.recordTransition(item.ID, from, StateBlocked, "proposal_resolution", firstNonEmpty(request.Actor, "operator"), false, item.BlockedReason); err != nil {
			return nil, workflowAuditPersistenceFailure("proposal transition record", err)
		}
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
		expected := item
		item.Status = status
		note := strings.TrimSpace(request.Note)
		message := "checklist item marked " + status + ": " + item.Label
		if note != "" {
			message += " | " + note
		}
		_, committed, err := s.repo.CommitChecklistUpdate(&expected, &item, models.WorkflowEvent{
			WorkflowID: id, EventType: "workflow.checklist", Message: message,
			Trigger: "checklist_update", RuleApplied: "checklist progress tracked", Actor: actor,
		})
		if err != nil {
			return nil, err
		}
		if !committed {
			return nil, fmt.Errorf("checklist changed while updating; reload before deciding again")
		}
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
	if s == nil {
		return nil, ErrClaimRecoveryContextUnavailable
	}
	if _, contextual := s.repo.(contextualClaimRecoveryRepository); contextual {
		return s.runContextualClaimRecovery(context.Background(), strings.TrimSpace(ownerIdentity), request)
	}
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	now := time.Now().UTC()
	limit := normalizeRunLimit(request.Limit)
	items, err := s.repo.FindExpiredWorkflowClaimsForOwner(ownerIdentity, now, limit)
	if err != nil {
		return nil, err
	}
	loops, err := s.repo.FindExpiredOpenLoopClaimsForOwner(ownerIdentity, now, limit)
	if err != nil {
		return nil, err
	}
	summary := &ClaimRecoverySummary{
		Checked: len(items) + len(loops),
		Results: []ClaimRecoveryResult{},
	}
	for _, item := range items {
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
	return s.runDueForOwnerContext(nil, ownerIdentity, request)
}

func (s *service) runDueForOwnerContext(ctx context.Context, ownerIdentity string, request RunDueRequest) (*WorkflowRunSummary, error) {
	lifetime := workflowExecutionContext(ctx)
	if err := lifetime.Err(); err != nil {
		return nil, err
	}
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	items, err := s.repo.FindRunnableItemsForOwner(ownerIdentity, time.Now().UTC(), normalizeRunLimit(request.Limit))
	if err != nil {
		return nil, err
	}
	summary := &WorkflowRunSummary{
		Checked: len(items),
		Results: []WorkflowRunResult{},
	}
	if decision := safety.EvaluateEmergencyStopForExecution(); decision.Active {
		reason := decision.Reason
		for _, item := range items {
			summary.Blocked++
			summary.Results = append(summary.Results, WorkflowRunResult{WorkflowID: item.ID, Status: "blocked", State: item.CurrentState, Attempts: item.RetryCount, Message: reason})
			s.decide(item.ID, "worker_execution", "blocked", reason, "emergency stop", false, "workflow-worker")
			s.audit(item.ID, "workflow.worker_blocked", item.CurrentState, item.CurrentState, reason, "emergency_stop", "emergency stop", item.SourceURI, "workflow-worker")
		}
		return summary, nil
	}
	for _, item := range items {
		if err := lifetime.Err(); err != nil {
			return summary, err
		}
		result := s.claimAndRunWorkflow(ctx, ownerIdentity, item)
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
		if err := lifetime.Err(); err != nil {
			return summary, err
		}
	}
	return summary, nil
}

// RunOneForOwner claims and runs exactly one owner-scoped workflow. It is the
// precise operator command used after review; all concrete task/runtime effects
// still pass through the task runner's authorization and verification gates.
func (s *service) RunOneForOwner(ownerIdentity string, id uuid.UUID) (*WorkflowRunResult, error) {
	return s.runOneForOwnerContext(nil, ownerIdentity, id)
}

func (s *service) runOneForOwnerContext(ctx context.Context, ownerIdentity string, id uuid.UUID) (*WorkflowRunResult, error) {
	if err := workflowExecutionContext(ctx).Err(); err != nil {
		return nil, err
	}
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	item, err := s.repo.FindItem(id)
	if err != nil || item == nil || strings.TrimSpace(item.OwnerIdentity) != ownerIdentity {
		return nil, fmt.Errorf("workflow not found")
	}
	if decision := safety.EvaluateEmergencyStopForExecution(); decision.Active {
		reason := decision.Reason
		s.decide(item.ID, "worker_execution", "blocked", reason, "emergency stop", false, "workflow-worker")
		s.audit(item.ID, "workflow.worker_blocked", item.CurrentState, item.CurrentState, reason, "emergency_stop", "emergency stop", item.SourceURI, "workflow-worker")
		return &WorkflowRunResult{WorkflowID: item.ID, Status: "blocked", State: item.CurrentState, Attempts: item.RetryCount, Message: reason}, nil
	}
	result := s.claimAndRunWorkflow(ctx, ownerIdentity, *item)
	return &result, workflowExecutionContext(ctx).Err()
}

func (s *service) claimAndRunWorkflow(ctx context.Context, ownerIdentity string, item models.WorkflowItem) WorkflowRunResult {
	if err := workflowExecutionContext(ctx).Err(); err != nil {
		return WorkflowRunResult{WorkflowID: item.ID, Status: "blocked", State: item.CurrentState, Message: "workflow request cancelled before claim"}
	}
	if err := s.ensureWorkflowSourceNotRetracted(&item); err != nil {
		return WorkflowRunResult{
			WorkflowID: item.ID, Status: "blocked", State: item.CurrentState,
			Attempts: item.RetryCount, Message: err.Error(),
		}
	}
	if s.coordinationProjector != nil && item.CoordinationPlanID == nil {
		if err := s.ensureWorkflowCoordinationDraft(&item, "workflow-worker"); err != nil {
			return WorkflowRunResult{
				WorkflowID: item.ID, Status: "blocked", State: item.CurrentState,
				Attempts: item.RetryCount, Message: "workflow cannot be claimed until its coordination draft is durable",
			}
		}
		current, err := s.repo.FindItem(item.ID)
		if err != nil || current == nil {
			return WorkflowRunResult{
				WorkflowID: item.ID, Status: "blocked", State: item.CurrentState,
				Attempts: item.RetryCount, Message: "workflow changed while verifying its coordination draft",
			}
		}
		item = *current
		if item.CurrentState != StateReady || (item.RequiresApproval && item.ApprovalStatus != "approved") {
			return WorkflowRunResult{
				WorkflowID: item.ID, Status: "skipped", State: item.CurrentState,
				Attempts: item.RetryCount, Message: "workflow is no longer runnable after coordination-plan verification",
			}
		}
	}
	if _, active := s.activeTaskRuns.Load(item.ID); active {
		return WorkflowRunResult{
			WorkflowID: item.ID, Status: "blocked", State: item.CurrentState,
			Attempts: item.RetryCount, Message: "an earlier task runner is still active for this workflow",
		}
	}
	claimedAt := time.Now().UTC()
	if err := workflowExecutionContext(ctx).Err(); err != nil {
		return WorkflowRunResult{WorkflowID: item.ID, Status: "blocked", State: item.CurrentState, Message: "workflow request cancelled before claim"}
	}
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
	var auditErrors []error
	if err := s.recordTransition(claimed.ID, StateReady, StateInProgress, "worker_claim", "workflow-worker", claimed.ApprovalStatus == "approved", "worker atomically claimed runnable workflow"); err != nil {
		auditErrors = append(auditErrors, fmt.Errorf("persist worker-claim transition: %w", err))
	}
	if err := s.audit(claimed.ID, "workflow.worker_started", StateReady, StateInProgress, "worker atomically claimed runnable workflow", "worker_claim", "single-consumer execution claim", claimed.SourceURI, "workflow-worker"); err != nil {
		auditErrors = append(auditErrors, fmt.Errorf("persist worker-start event: %w", err))
	}
	if len(auditErrors) > 0 {
		failure := fmt.Errorf("worker start audit history is incomplete; task execution was not started: %w", errors.Join(auditErrors...))
		claimed.CurrentState = StateBlocked
		claimed.BlockedReason = "worker start audit history is incomplete; review before execution"
		claimed.NextAction = "repair audit storage and review this workflow before retrying"
		claimed.NextRunAt = nil
		claimed.LastWorkerError = failure.Error()
		claimed.VerificationStatus = "needs_review"
		updated, owned, releaseErr := s.repo.UpdateClaimedItem(claimed, claimID)
		if releaseErr != nil {
			failure = errors.Join(failure, fmt.Errorf("release worker claim: %w", releaseErr))
		} else if !owned || updated == nil {
			failure = errors.Join(failure, fmt.Errorf("worker claim was lost before the blocked state could be persisted"))
		}
		state := StateInProgress
		if updated != nil {
			state = updated.CurrentState
		}
		return WorkflowRunResult{
			WorkflowID: claimed.ID, Status: "blocked", State: state,
			Attempts: claimed.RetryCount, VerificationStatus: "needs_review", Message: failure.Error(),
		}
	}
	return s.runWorkflowItem(ctx, *claimed, claimID)
}

func (s *service) RunDueOpenLoops(request RunDueRequest) (*OpenLoopRunSummary, error) {
	return s.RunDueOpenLoopsForOwner("", request)
}

func (s *service) RunDueOpenLoopsForOwner(ownerIdentity string, request RunDueRequest) (*OpenLoopRunSummary, error) {
	return s.runDueOpenLoopsForOwnerContext(context.Background(), ownerIdentity, request)
}

func (s *service) runDueOpenLoopsForOwnerContext(ctx context.Context, ownerIdentity string, request RunDueRequest) (*OpenLoopRunSummary, error) {
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
	if decision := safety.EvaluateEmergencyStopForExecution(); decision.Active {
		reason := decision.Reason
		for _, loop := range loops {
			if err := ctx.Err(); err != nil {
				return summary, err
			}
			summary.Skipped++
			summary.Results = append(summary.Results, OpenLoopRunResult{WorkflowID: loop.WorkflowID, OpenLoopID: loop.ID, Status: "skipped", Message: reason})
			s.decide(loop.WorkflowID, "open_loop", "blocked", reason, "emergency stop", false, "workflow-followup")
			if err := ctx.Err(); err != nil {
				return summary, err
			}
			s.audit(loop.WorkflowID, "workflow.open_loop_blocked", "", "", reason, "emergency_stop", "emergency stop", "", "workflow-followup")
		}
		if err := ctx.Err(); err != nil {
			return summary, err
		}
		return summary, nil
	}
	if _, ok := s.repo.(transactionalFollowUpRepository); !ok {
		for _, loop := range loops {
			summary.Skipped++
			summary.Results = append(summary.Results, OpenLoopRunResult{
				WorkflowID: loop.WorkflowID, OpenLoopID: loop.ID, Status: "skipped",
				Message: "durable transactional follow-up projection is unavailable; open loop was not claimed",
			})
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
		if err := ctx.Err(); err != nil {
			// A claim acknowledgement can be lost. Do not start projection or
			// guess that releasing the claim is safe after cancellation.
			return summary, err
		}
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
		result := s.runOpenLoopForOwnerContext(ctx, ownerIdentity, *claimed, claimID)
		if result.Status == "skipped" && ctx.Err() == nil {
			if followups, ok := s.repo.(transactionalFollowUpRepository); ok {
				if owned, releaseErr := followups.releaseDueOpenLoopClaim(ownerIdentity, loop.WorkflowID, loop.ID, claimID); releaseErr != nil {
					result.Message += "; failed to release open-loop claim: " + releaseErr.Error()
				} else if !owned {
					result.Message += "; open-loop claim was already lost or expired"
				}
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
		if err := ctx.Err(); err != nil {
			return summary, err
		}
	}
	return summary, nil
}

func (s *service) runOpenLoop(loop models.WorkflowOpenLoop, claimID string) OpenLoopRunResult {
	return s.runOpenLoopForOwner("", loop, claimID)
}

func (s *service) runOpenLoopForOwner(owner string, loop models.WorkflowOpenLoop, claimID string) OpenLoopRunResult {
	return s.runOpenLoopForOwnerContext(context.Background(), owner, loop, claimID)
}

func (s *service) runOpenLoopForOwnerContext(ctx context.Context, owner string, loop models.WorkflowOpenLoop, claimID string) OpenLoopRunResult {
	result := OpenLoopRunResult{WorkflowID: loop.WorkflowID, OpenLoopID: loop.ID, Status: "skipped"}
	if err := ctx.Err(); err != nil {
		result.Message = "follow-up canceled before projection; inspect any outstanding claim before recovery"
		return result
	}
	followups, ok := s.repo.(transactionalFollowUpRepository)
	if !ok {
		result.Message = "durable transactional follow-up projection is unavailable; no follow-up artifacts were written"
		return result
	}
	committed, err := followups.commitDueOpenLoop(owner, loop.WorkflowID, loop.ID, claimID)
	if err != nil {
		result.Message = "follow-up projection could not be confirmed: " + err.Error()
		return result
	}
	if committed == nil || committed.Item == nil {
		result.Message = "follow-up projection did not return a durable commit receipt"
		return result
	}
	result.Status, result.State = committed.Status, committed.Item.CurrentState
	result.Message = "proposal and checklist step durably projected; no task action was executed"
	if committed.Status == "resolved" {
		result.Message = "workflow already closed; open loop durably resolved"
	}
	if committed.Replayed {
		result.Message = "previous follow-up commit confirmed; workflow state and review decisions were not reapplied"
	} else if committed.FromState != committed.Item.CurrentState {
		s.projectWorkflowTransitionContext(ctx, committed.Item.ID, committed.FromState, committed.Item.CurrentState, "followup_worker")
	}
	return result
}

func (s *service) runWorkflowItem(ctx context.Context, item models.WorkflowItem, claimID string) WorkflowRunResult {
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
	if err := s.verifyWorkflowSourceProvenance(item); err != nil {
		return s.handleRunReviewRequired(&item, claimID, "source provenance is unavailable before task execution: "+err.Error(), "needs_review")
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
	if _, err := s.findRequiredQualityGates(item.ID, item.TaskType); err != nil {
		return s.handleRunReviewRequired(
			&item,
			claimID,
			"required workflow quality gates are unavailable before task execution: "+err.Error(),
			"needs_review",
		)
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
	leaseOwned, leaseErr := s.repo.RenewRunnableItemClaim(
		item.ID,
		claimID,
		time.Now().UTC().Add(claimLeaseDuration()),
	)
	if leaseErr != nil || !leaseOwned {
		reason := "worker claim could not be confirmed immediately before task start; no task action was initiated"
		if leaseErr != nil {
			reason += ": " + leaseErr.Error()
		}
		return s.handleRunReviewRequired(&item, claimID, reason, "needs_review")
	}
	if workflowExecutionContext(ctx).Err() != nil {
		return s.handleRunReviewRequired(&item, claimID, "workflow cancelled before task dispatch; review before a new attempt", "needs_review")
	}
	runResult, err := s.runTaskWithLease(item.ID, claimID, TaskRunRequest{
		ExecutionContext:      ctx,
		OwnerIdentity:         item.OwnerIdentity,
		PursuitID:             pursuitID,
		WorkflowID:            item.ID.String(),
		Request:               item.Description,
		SuccessCriteria:       append([]string(nil), item.SuccessCriteria...),
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
	if cancelErr := workflowExecutionContext(ctx).Err(); cancelErr != nil {
		err = errors.Join(err, cancelErr)
	}
	var taskLeaseErr *taskLeaseConfirmationError
	explicitUncertainty := runResult != nil && runResult.ExecutionOutcomeUncertain
	outcomeUncertain := explicitUncertainty || (err != nil && (errors.As(err, &taskLeaseErr) ||
		!IsTaskFailureSafeNoSideEffect(err) ||
		(runResult != nil && runResult.ExternalActionExecuted)))
	// A returned uncertainty signal overrides success, approval, and safe-error
	// markers: none establishes that a dispatched action had no side effects.
	if outcomeUncertain {
		evidenceFailures := s.persistUncertainTaskRunEvidence(&item, runResult)
		reason := "task engine reports its outcome is uncertain; human review is required before any retry"
		if err != nil {
			reason = "task engine failed and its outcome is uncertain; human review is required before any retry: " + err.Error()
			if errors.As(err, &taskLeaseErr) {
				reason = "worker lease could not be confirmed after task execution; the outcome is uncertain and must be reviewed before any retry: " + err.Error()
			}
		}
		if explicitUncertainty && strings.TrimSpace(runResult.FailureReason) != "" {
			reason += "; task failure: " + safety.RedactSecrets(runResult.FailureReason)
		}
		if runResult != nil && runResult.ExternalActionExecuted {
			reason += "; task result reports an external action was executed"
		}
		if len(evidenceFailures) > 0 {
			reason += "; evidence persistence failed: " + strings.Join(evidenceFailures, "; ")
		}
		verificationStatus := "needs_review"
		if !explicitUncertainty && runResult != nil && strings.TrimSpace(runResult.VerificationStatus) != "" {
			verificationStatus = runResult.VerificationStatus
		}
		blocked := s.handleRunExecutionReviewRequired(&item, claimID, safety.RedactSecrets(reason), verificationStatus)
		if runResult != nil {
			blocked.FrameworkSelection = runResult.FrameworkSelection
		}
		return blocked
	}
	if err != nil {
		verificationStatus := ""
		if runResult != nil {
			verificationStatus = runResult.VerificationStatus
		}
		return s.handleRunFailure(&item, claimID, "task engine failed before side effects; safe retry is permitted: "+err.Error(), verificationStatus)
	}
	if runResult == nil {
		return s.handleRunExecutionReviewRequired(&item, claimID, "task engine returned no result; execution outcome cannot be established", "needs_review")
	}
	item.LastTaskPlanID = runResult.PlanID
	item.VerificationStatus = runResult.VerificationStatus
	if err := s.storeTaskFrameworkSelection(item.ID, runResult); err != nil {
		return s.handleRunTaskReviewRequired(&item, claimID, runResult, "framework selection provenance could not be stored: "+err.Error(), "needs_review")
	}
	if time.Now().UTC().After(observedAt.Add(worldStateTTL())) {
		result := s.handleRunTaskReviewRequired(&item, claimID, runResult,
			"world state expired during execution; re-observe source and external side effects before accepting completion", "needs_review")
		result.FrameworkSelection = runResult.FrameworkSelection
		return result
	}
	if err := s.storeTaskRuntimeEvidence(item.ID, runResult); err != nil {
		result := s.handleRunTaskReviewRequired(&item, claimID, runResult, "runtime evidence could not be stored: "+err.Error(), "needs_review")
		result.FrameworkSelection = runResult.FrameworkSelection
		return result
	}
	if runResult.ReviewRequired || runResult.ApprovalRequired {
		defaultReason := "task engine requires human review"
		if runResult.ApprovalRequired {
			defaultReason = "task engine requires explicit human approval"
		}
		reason := firstNonEmpty(runResult.FailureReason, defaultReason)
		if runResult.ApprovalRequired {
			if runResult.ExternalActionExecuted {
				result := s.handleRunExecutionReviewRequired(&item, claimID, reason, runResult.VerificationStatus)
				result.FrameworkSelection = runResult.FrameworkSelection
				return result
			}
			result := s.handleRunApprovalRequired(&item, claimID, reason, runResult.VerificationStatus)
			result.FrameworkSelection = runResult.FrameworkSelection
			return result
		}
		result := s.handleRunTaskReviewRequired(&item, claimID, runResult, reason, runResult.VerificationStatus)
		result.FrameworkSelection = runResult.FrameworkSelection
		return result
	}
	gateResult := s.evaluateQualityGates(item, runResult)
	if workflowExecutionContext(ctx).Err() != nil {
		return s.handleRunTaskReviewRequired(&item, claimID, runResult, "workflow cancelled during completion verification; review retained evidence before retrying", "needs_review")
	}
	if gateResult.ReviewRequired {
		reason := firstNonEmpty(strings.Join(gateResult.Failures, "; "), "quality gate evaluation could not be durably completed")
		if runResult.ExternalActionExecuted {
			reason = "controlled runtime action executed, but completion validation failed; review evidence before any retry: " + reason
		}
		result := s.handleRunTaskReviewRequired(&item, claimID, runResult, reason, "needs_review")
		result.FrameworkSelection = runResult.FrameworkSelection
		return result
	}
	if runResult.Passed && !runResult.ReviewRequired && gateResult.Passed &&
		!acceptsWorkflowCompletionVerification(runResult.VerificationStatus) {
		reason := fmt.Sprintf(
			"task execution passed with verification status %q, which is not eligible for completion attestation; review the evidence before closing this workflow",
			strings.TrimSpace(runResult.VerificationStatus),
		)
		result := s.handleRunTaskReviewRequired(&item, claimID, runResult, reason, runResult.VerificationStatus)
		result.FrameworkSelection = runResult.FrameworkSelection
		return result
	}
	if runResult.Passed && !runResult.ReviewRequired && gateResult.Passed {
		if _, err := s.resolveWorkflowItemCoordinationPlan(item); err != nil {
			result := s.handleRunTaskReviewRequired(&item, claimID, runResult, "coordination plan changed before completion attestation: "+err.Error(), "needs_review")
			result.FrameworkSelection = runResult.FrameworkSelection
			return result
		}
		if workflowExecutionContext(ctx).Err() != nil {
			return s.handleRunTaskReviewRequired(&item, claimID, runResult, "workflow cancelled before completion attestation; review retained evidence before retrying", "needs_review")
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
			if runResult.ExternalActionExecuted {
				return s.handleRunExecutionReviewRequired(&item, claimID, "completion attestation could not be created after external execution: "+err.Error(), "needs_review")
			}
			return WorkflowRunResult{WorkflowID: item.ID, Status: "blocked", State: StateInProgress, Attempts: item.RetryCount, VerificationStatus: item.VerificationStatus, Message: err.Error(), FrameworkSelection: runResult.FrameworkSelection}
		}
		if _, owned, err := s.repo.CompleteClaimedItem(&item, claimID, attestation); err != nil {
			return WorkflowRunResult{WorkflowID: item.ID, Status: "blocked", State: StateInProgress, Attempts: item.RetryCount, VerificationStatus: item.VerificationStatus, Message: err.Error(), FrameworkSelection: runResult.FrameworkSelection}
		} else if !owned {
			return WorkflowRunResult{WorkflowID: item.ID, Status: "blocked", State: StateBlocked, Attempts: item.RetryCount, VerificationStatus: item.VerificationStatus, Message: "worker claim was lost before completion could be persisted", FrameworkSelection: runResult.FrameworkSelection}
		}
		var auditErrors []error
		if err := s.recordTransition(item.ID, StateInProgress, StateCompleted, "worker", "workflow-worker", item.ApprovalStatus == "approved", "task engine result verified workflow completion"); err != nil {
			auditErrors = append(auditErrors, fmt.Errorf("persist worker-completion transition: %w", err))
		}
		if err := s.decide(item.ID, "verification_completion", "completed", "verification accepted task engine result", firstNonEmpty(runResult.VerificationStatus, "validation passed"), item.ApprovalStatus == "approved", "workflow-worker"); err != nil {
			auditErrors = append(auditErrors, fmt.Errorf("persist completion decision: %w", err))
		}
		if err := s.audit(item.ID, "workflow.worker_completed", StateInProgress, StateCompleted, firstNonEmpty(runResult.Output, "task engine result verified workflow completion"), "worker", "verification accepted completion", item.SourceURI, "workflow-worker"); err != nil {
			auditErrors = append(auditErrors, fmt.Errorf("persist worker-completion event: %w", err))
		}
		if err := s.markChecklistProgress(item.ID, "Verify completion before closing"); err != nil {
			auditErrors = append(auditErrors, fmt.Errorf("persist completion checklist: %w", err))
		}
		message := "verified completion"
		reviewRequired := false
		if len(auditErrors) > 0 {
			reviewRequired = true
			message = fmt.Sprintf("verified completion, but audit history is incomplete: %v", errors.Join(auditErrors...))
		}
		return WorkflowRunResult{WorkflowID: item.ID, Status: "completed", State: StateCompleted, Attempts: item.RetryCount, VerificationStatus: item.VerificationStatus, Message: message, ReviewRequired: reviewRequired, FrameworkSelection: runResult.FrameworkSelection}
	}
	reason := firstNonEmpty(runResult.FailureReason, "task engine validation did not pass")
	if !gateResult.Passed {
		reason = firstNonEmpty(strings.Join(gateResult.Failures, "; "), reason)
	}
	if runResult.ExternalActionExecuted {
		reason = "controlled runtime action executed, but completion validation failed; review evidence before any retry: " + reason
		result := s.handleRunExecutionReviewRequired(&item, claimID, reason, runResult.VerificationStatus)
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
		NeedsReview: result.ReviewRequired || result.ExecutionOutcomeUncertain,
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
	if request.ExecutionContext != nil {
		if err := request.ExecutionContext.Err(); err != nil {
			return nil, MarkTaskFailureSafeNoSideEffect(err)
		}
		runner, ok := s.taskRunner.(ContextualTaskRunner)
		if !ok {
			return nil, MarkTaskFailureSafeNoSideEffect(ErrTaskExecutionContextUnavailable)
		}
		return runner.RunWorkflowTaskContext(request.ExecutionContext, request)
	}
	return s.taskRunner.RunWorkflowTask(request)
}

type taskLeaseConfirmationError struct {
	reason string
	cause  error
}

func (e *taskLeaseConfirmationError) Error() string { return e.reason }

func (e *taskLeaseConfirmationError) Unwrap() error { return e.cause }

func (s *service) persistUncertainTaskRunEvidence(item *models.WorkflowItem, result *TaskRunResult) []string {
	if item == nil || result == nil {
		return nil
	}
	if planID := strings.TrimSpace(result.PlanID); planID != "" {
		item.LastTaskPlanID = planID
	}
	item.VerificationStatus = "needs_review"

	failures := make([]string, 0, 2)
	if result.FrameworkSelection != nil {
		if err := s.storeTaskFrameworkSelection(item.ID, result); err != nil {
			failures = append(failures, "framework selection: "+err.Error())
		}
	}
	if strings.TrimSpace(result.RuntimeEvidenceURI) != "" {
		// Exclude free-form task output; preserve only the launch reference,
		// label, and bounded route summary while the outcome is uncertain.
		safeEvidence := *result
		safeEvidence.Output = ""
		safeEvidence.VerificationStatus = "needs_review"
		safeEvidence.ReviewRequired = true
		if err := s.storeTaskRuntimeEvidence(item.ID, &safeEvidence); err != nil {
			failures = append(failures, "runtime evidence: "+err.Error())
		}
	} else if routeSummary := runtimeRouteTraceEvidenceSummary(result.RuntimeRouteTrace); routeSummary != "" {
		events, err := s.repo.FindEvents(item.ID)
		if err != nil {
			failures = append(failures, "runtime route trace lookup: "+err.Error())
		} else {
			found := false
			for _, event := range events {
				if event.EventType == "workflow.runtime_route_trace" && event.Message == routeSummary {
					found = true
					break
				}
			}
			if !found {
				_, err = s.repo.CreateEvent(&models.WorkflowEvent{
					WorkflowID:  item.ID,
					EventType:   "workflow.runtime_route_trace",
					Message:     routeSummary,
					Trigger:     "uncertain_task_outcome",
					RuleApplied: "runtime route trace retained without immutable evidence URI",
					Actor:       "workflow-worker",
				})
				if err != nil {
					failures = append(failures, "runtime route trace: "+err.Error())
				}
			}
		}
	}
	return failures
}

func (s *service) runTaskWithLease(itemID uuid.UUID, claimID string, request TaskRunRequest) (*TaskRunResult, error) {
	if _, active := s.activeTaskRuns.LoadOrStore(itemID, claimID); active {
		return nil, fmt.Errorf("an earlier task runner is still active for this workflow")
	}
	defer s.activeTaskRuns.Delete(itemID)
	stop := make(chan struct{})
	done := make(chan struct{})
	renewalFailure := make(chan error, 1)
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
				if err != nil {
					renewalFailure <- fmt.Errorf("failed to renew worker claim during task execution: %w", err)
					return
				}
				if !owned {
					renewalFailure <- fmt.Errorf("worker claim was lost during task execution")
					return
				}
			}
		}
	}()
	result, err := s.runTaskSafely(request)
	close(stop)
	<-done
	owned, renewErr := s.repo.RenewRunnableItemClaim(itemID, claimID, time.Now().UTC().Add(claimLeaseDuration()))
	var priorRenewalErr error
	select {
	case priorRenewalErr = <-renewalFailure:
	default:
	}
	if !owned {
		if _, releaseErr := s.repo.ReleaseInterruptedExecutionClaim(itemID, claimID); releaseErr != nil {
			renewErr = errors.Join(renewErr, fmt.Errorf("failed to release completed runner's interrupted-execution fence: %w", releaseErr))
		}
	}
	if priorRenewalErr != nil {
		reason := "worker claim could not be continuously confirmed during task execution: " + priorRenewalErr.Error()
		if err != nil {
			reason += "; task runner also returned: " + err.Error()
		}
		return result, &taskLeaseConfirmationError{
			reason: reason,
			cause:  errors.Join(err, priorRenewalErr, renewErr),
		}
	}
	if renewErr != nil {
		reason := "failed to confirm worker claim after task execution: " + renewErr.Error()
		if err != nil {
			reason += "; task runner also returned: " + err.Error()
		}
		return result, &taskLeaseConfirmationError{
			reason: reason,
			cause:  errors.Join(err, renewErr),
		}
	}
	if !owned {
		reason := "worker claim was lost during task execution"
		if err != nil {
			reason += "; task runner also returned: " + err.Error()
		}
		return result, &taskLeaseConfirmationError{
			reason: reason,
			cause:  err,
		}
	}
	if err != nil {
		return result, err
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
	Passed         bool
	ReviewRequired bool
	Failures       []string
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
			ReviewRequired:     true,
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
			ReviewRequired:     true,
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
		ReviewRequired:     true,
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
		return WorkflowRunResult{WorkflowID: item.ID, Status: "blocked", State: StateInProgress, Attempts: item.RetryCount, VerificationStatus: verificationStatus, Message: err.Error(), ReviewRequired: true}
	}
	if !owned || updated == nil {
		return WorkflowRunResult{WorkflowID: item.ID, Status: "blocked", State: StateBlocked, Attempts: item.RetryCount, VerificationStatus: verificationStatus, Message: "worker claim was lost before review-required state could be persisted", ReviewRequired: true}
	}
	s.recordTransition(item.ID, StateInProgress, StateBlocked, "worker_review_required", "workflow-worker", item.ApprovalStatus == "approved", item.BlockedReason)
	s.decide(item.ID, "worker_execution", "needs_review", item.BlockedReason, "task engine explicitly required human review", false, "workflow-worker")
	s.audit(item.ID, "workflow.worker_review_required", StateInProgress, StateBlocked, item.BlockedReason, "worker_review_required", "non-retryable review gate", item.SourceURI, "workflow-worker")
	return WorkflowRunResult{WorkflowID: item.ID, Status: "blocked", State: StateBlocked, Attempts: item.RetryCount, VerificationStatus: verificationStatus, Message: item.BlockedReason, ReviewRequired: true}
}

func (s *service) handleRunExecutionReviewRequired(item *models.WorkflowItem, claimID, reason, verificationStatus string) WorkflowRunResult {
	item.RecoveryStatus = RecoveryNeedsReview
	item.RecoveryNote = "execution termination and external outcome must be reconciled before another attempt"
	return s.handleRunReviewRequired(item, claimID, reason, verificationStatus)
}

func (s *service) handleRunTaskReviewRequired(item *models.WorkflowItem, claimID string, runResult *TaskRunResult, reason, verificationStatus string) WorkflowRunResult {
	if runResult != nil && (runResult.ExternalActionExecuted || runResult.ExecutionOutcomeUncertain) {
		if runResult.ExecutionOutcomeUncertain {
			reason = safety.RedactSecrets(reason)
			verificationStatus = "needs_review"
		}
		return s.handleRunExecutionReviewRequired(item, claimID, reason, verificationStatus)
	}
	return s.handleRunReviewRequired(item, claimID, reason, verificationStatus)
}

func (s *service) evaluateQualityGates(item models.WorkflowItem, runResult *TaskRunResult) qualityGateRunResult {
	result := qualityGateRunResult{Passed: true}
	gates, err := s.findRequiredQualityGates(item.ID, item.TaskType)
	if err != nil {
		result.Passed = false
		result.ReviewRequired = true
		result.Failures = append(result.Failures, "required workflow quality gates could not be loaded: "+err.Error())
		return result
	}
	sourceLinks, err := s.repo.FindSourceLinks(item.ID)
	if err != nil {
		result.Passed = false
		result.ReviewRequired = true
		result.Failures = append(result.Failures, "workflow source links could not be loaded: "+err.Error())
		return result
	}
	evidence, err := s.repo.FindEvidenceClaims(item.ID)
	if err != nil {
		result.Passed = false
		result.ReviewRequired = true
		result.Failures = append(result.Failures, "workflow evidence claims could not be loaded: "+err.Error())
		return result
	}
	githubEvidence := GitHubQualityEvidence{}
	githubEvidenceFailure := "owner-scoped connected GitHub evidence is unavailable"
	if s.githubQualityEvidenceResolver != nil {
		resolved, resolveErr := s.githubQualityEvidenceResolver.ResolveGitHubQualityEvidence(
			item.OwnerIdentity,
			githubQualityEvidenceReferences(item, sourceLinks, evidence),
		)
		if resolveErr == nil {
			githubEvidence = resolved
			githubEvidenceFailure = ""
		} else {
			githubEvidenceFailure = "owner-scoped GitHub evidence could not be verified"
		}
	}

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
			mandatory = evidenceClaimsRequired(item.TaskType)
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
				reason = firstNonEmpty(githubEvidenceFailure, "no verified GitHub commit record is attached")
			} else {
				reason = "verified GitHub commit record is attached to an owner-authorized connected source"
			}
		case "tests or build evidence":
			mandatory = item.TaskType == "technical"
			if !githubEvidence.WorkflowSuccess {
				status = "needs_review"
				reason = firstNonEmpty(githubEvidenceFailure, "no successful GitHub Actions run matches the single verified commit explicitly linked to this workflow")
			} else {
				reason = "verified completed-success GitHub Actions run matches the single verified commit explicitly linked to this workflow"
			}
		case "readme/setup updated":
			mandatory = item.TaskType == "technical"
			if !githubEvidence.DocsChanged {
				status = "needs_review"
				reason = firstNonEmpty(githubEvidenceFailure, "no merged documentation change matches the single verified commit explicitly linked to this workflow")
			} else {
				reason = "verified merged GitHub documentation change matches the single verified commit explicitly linked to this workflow"
			}
		case "windows 11 operational path":
			mandatory = item.TaskType == "technical"
			status = "needs_review"
			reason = "no structured, platform-attested Windows 11 validation record is available"
		}
		gate.Status = status
		gate.Reason = reason
		updated, updateErr := s.repo.UpdateQualityGate(&gate)
		if updateErr != nil || updated == nil || updated.ID != gate.ID || updated.WorkflowID != item.ID || !strings.EqualFold(strings.TrimSpace(updated.Status), status) {
			result.Passed = false
			result.ReviewRequired = true
			failure := "quality gate status could not be durably persisted"
			if updateErr != nil {
				failure += ": " + updateErr.Error()
			} else if updated == nil {
				failure += ": repository returned no quality gate"
			} else {
				failure += ": repository returned a mismatched quality gate"
			}
			result.Failures = append(result.Failures, gate.Gate+": "+failure)
		}
		if mandatory && status != "passed" {
			result.Passed = false
			result.ReviewRequired = true
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

func (s *service) persistRequiredQualityGates(workflowID uuid.UUID, taskType string) error {
	required := qualityGatesForAnalysis(workflowID, inputAnalysis{taskType: taskType})
	for index := range required {
		created, err := s.repo.CreateQualityGate(&required[index])
		if err != nil {
			return fmt.Errorf("create required quality gate %q: %w", required[index].Gate, err)
		}
		if created == nil || created.ID == uuid.Nil || created.WorkflowID != workflowID || !strings.EqualFold(strings.TrimSpace(created.Gate), required[index].Gate) {
			return fmt.Errorf("create required quality gate %q returned no matching durable record", required[index].Gate)
		}
	}
	_, err := s.findRequiredQualityGates(workflowID, taskType)
	if err != nil {
		return fmt.Errorf("verify required quality gates after creation: %w", err)
	}
	return nil
}

func workflowHasSourceProvenance(item models.WorkflowItem) bool {
	return strings.TrimSpace(item.SourceType) != "" || strings.TrimSpace(item.SourceID) != "" ||
		strings.TrimSpace(item.SourceURI) != "" || strings.TrimSpace(item.SourceLabel) != ""
}

func (s *service) verifyWorkflowSourceProvenance(item models.WorkflowItem) error {
	if !workflowHasSourceProvenance(item) {
		return nil
	}
	intakeRecords, err := s.repo.FindIntakeRecords(item.ID)
	if err != nil {
		return fmt.Errorf("load source intake record: %w", err)
	}
	intakeRecordFound := false
	for _, record := range intakeRecords {
		if record.ID != uuid.Nil && record.WorkflowID == item.ID && record.SourceType == item.SourceType &&
			record.SourceID == item.SourceID && record.SourceURI == item.SourceURI && record.SourceLabel == item.SourceLabel &&
			record.RawContent == item.Description {
			intakeRecordFound = true
			break
		}
	}
	if !intakeRecordFound {
		return fmt.Errorf("matching source intake record is missing")
	}
	sourceLinks, err := s.repo.FindSourceLinks(item.ID)
	if err != nil {
		return fmt.Errorf("load source links: %w", err)
	}
	for _, link := range sourceLinks {
		if link.ID != uuid.Nil && link.WorkflowID == item.ID && link.Relationship == "origin" &&
			link.SourceType == item.SourceType && link.SourceID == item.SourceID &&
			link.SourceURI == item.SourceURI && link.SourceLabel == item.SourceLabel {
			return nil
		}
	}
	return fmt.Errorf("matching origin source link is missing")
}

func (s *service) blockUnqueuedWorkflowForSourceProvenanceFailure(item *models.WorkflowItem, cause error) error {
	return s.blockUnqueuedWorkflow(item, cause,
		"source provenance persistence failed",
		"required source provenance could not be durably established",
		"restore source provenance storage before scheduling this workflow",
		"source_provenance_persistence", "workflow.source_provenance_blocked",
		"workflow cannot be scheduled without durable origin provenance")
}

func (s *service) blockUnqueuedWorkflowForDeadlineReminderFailure(item *models.WorkflowItem, cause error) error {
	return s.blockUnqueuedWorkflow(item, cause,
		"workflow deadline reminder persistence failed",
		"required deadline reminder could not be durably created",
		"restore workflow reminder storage before scheduling this workflow",
		"deadline_reminder_persistence", "workflow.deadline_reminder_blocked",
		"workflow cannot be scheduled without a durable deadline reminder")
}

func (s *service) blockUnqueuedWorkflowForIntakeContextFailure(item *models.WorkflowItem, cause error) error {
	return s.blockUnqueuedWorkflow(item, cause,
		"workflow intake context persistence failed",
		"required project or follow-up context could not be durably established",
		"restore workflow context storage before scheduling this workflow",
		"intake_context_persistence", "workflow.intake_context_blocked",
		"workflow cannot be scheduled without required project or follow-up context")
}

func (s *service) blockUnqueuedWorkflowForChecklistFailure(item *models.WorkflowItem, cause error) error {
	return s.blockUnqueuedWorkflow(item, cause,
		"workflow checklist persistence failed",
		"required checklist could not be durably established",
		"restore checklist storage before scheduling this workflow",
		"checklist_persistence", "workflow.checklist_persistence_blocked",
		"workflow cannot be scheduled without durable required checklist items")
}

func (s *service) blockUnqueuedWorkflowForEvidenceFailure(item *models.WorkflowItem, cause error) error {
	return s.blockUnqueuedWorkflow(item, cause,
		"workflow evidence persistence failed",
		"required factual evidence could not be durably established",
		"restore evidence storage and review source-backed claims before scheduling this workflow",
		"evidence_persistence", "workflow.evidence_persistence_blocked",
		"workflow cannot be queued without durable required factual claims")
}

func (s *service) persistRequiredEvidenceClaim(claim *models.WorkflowEvidenceClaim) error {
	if claim == nil || claim.WorkflowID == uuid.Nil || strings.TrimSpace(claim.ClaimText) == "" {
		return fmt.Errorf("a workflow-bound factual evidence claim is required")
	}
	persisted, err := s.repo.CreateEvidenceClaim(claim)
	if err != nil {
		return fmt.Errorf("create evidence claim: %w", err)
	}
	if persisted == nil || persisted.ID == uuid.Nil || persisted.WorkflowID != claim.WorkflowID ||
		persisted.ClaimText != claim.ClaimText || persisted.SourceURI != claim.SourceURI ||
		persisted.SourceLabel != claim.SourceLabel || persisted.Reliability != claim.Reliability ||
		persisted.Status != claim.Status || persisted.NeedsReview != claim.NeedsReview {
		return fmt.Errorf("repository did not confirm the required workflow evidence claim")
	}
	return nil
}

func (s *service) persistRequiredProposal(proposal *models.WorkflowProposal) error {
	if proposal == nil || proposal.ID != uuid.Nil {
		return fmt.Errorf("a new workflow proposal is required")
	}
	persisted, err := s.repo.CreateProposal(proposal)
	if err != nil {
		return fmt.Errorf("create approval proposal: %w", err)
	}
	if persisted == nil || persisted.ID == uuid.Nil || persisted.WorkflowID != proposal.WorkflowID ||
		persisted.RecommendedAction != proposal.RecommendedAction || persisted.Options != proposal.Options || persisted.Status != "open" {
		return fmt.Errorf("repository did not confirm the required workflow proposal")
	}
	return nil
}

func (s *service) blockUnqueuedWorkflowForProposalFailure(item *models.WorkflowItem, cause error) error {
	return s.blockUnqueuedWorkflow(item, cause,
		"workflow proposal persistence failed",
		"required workflow proposal could not be durably established",
		"restore approval-proposal storage before scheduling this workflow",
		"proposal_persistence", "workflow.proposal_persistence_blocked",
		"workflow cannot be queued without its required approval proposal")
}

func (s *service) findRequiredQualityGates(workflowID uuid.UUID, taskType string) ([]models.WorkflowQualityGate, error) {
	gates, err := s.repo.FindQualityGates(workflowID)
	if err != nil {
		return nil, fmt.Errorf("load quality gates: %w", err)
	}
	found := make(map[string]bool, len(gates))
	for _, gate := range gates {
		if gate.ID == uuid.Nil || gate.WorkflowID != workflowID || strings.TrimSpace(gate.Gate) == "" {
			return nil, fmt.Errorf("quality gate query returned an invalid record")
		}
		found[strings.ToLower(strings.TrimSpace(gate.Gate))] = true
	}
	for _, required := range qualityGatesForAnalysis(workflowID, inputAnalysis{taskType: taskType}) {
		name := strings.ToLower(strings.TrimSpace(required.Gate))
		if !found[name] {
			return nil, fmt.Errorf("required quality gate %q is missing", required.Gate)
		}
	}
	return gates, nil
}

func (s *service) blockUnqueuedWorkflowForQualityGateFailure(item *models.WorkflowItem, reason string, cause error) error {
	return s.blockUnqueuedWorkflow(item, cause,
		reason,
		reason,
		"review quality-gate persistence before scheduling workflow",
		"quality_gate_persistence", "workflow.quality_gate_persistence_blocked",
		"workflow cannot be queued without durable required gates")
}

func (s *service) blockUnqueuedWorkflow(
	item *models.WorkflowItem,
	cause error,
	failureMessage, blockedMessage, nextAction, trigger, eventType, rule string,
) error {
	if item == nil || item.ID == uuid.Nil {
		if cause == nil {
			cause = errors.New("intake persistence failed")
		}
		return fmt.Errorf("%s: %w; workflow item is unavailable for safe blocking", failureMessage, cause)
	}
	if cause == nil {
		cause = errors.New("intake persistence failed")
	}
	blocked := *item
	from := blocked.CurrentState
	blocked.CurrentState = StateBlocked
	blocked.BlockedReason = blockedMessage + ": " + cause.Error()
	blocked.LastWorkerError = blocked.BlockedReason
	blocked.NextAction = nextAction
	blocked.NextRunAt = nil

	updated, changed, err := s.repo.UpdateWorkflowItemCAS(item, &blocked)
	if err != nil {
		return fmt.Errorf("%s: %w; failed to persist blocked workflow state: %v", failureMessage, cause, err)
	}
	if !changed || updated == nil || updated.ID != item.ID || updated.CurrentState != StateBlocked {
		current, findErr := s.repo.FindItem(item.ID)
		if findErr == nil && current != nil {
			if quarantineErr := s.ensureWorkflowSourceNotRetracted(current); quarantineErr != nil {
				return quarantineErr
			}
		}
		return fmt.Errorf("%s: %w; workflow changed concurrently and was not overwritten", failureMessage, cause)
	}

	var auditErrors []error
	if err := s.recordTransition(item.ID, from, StateBlocked, trigger, "workflow-intake", false, blocked.BlockedReason); err != nil {
		auditErrors = append(auditErrors, fmt.Errorf("persist intake failure transition: %w", err))
	}
	if err := s.audit(item.ID, eventType, from, StateBlocked, blocked.BlockedReason, trigger, rule, item.SourceURI, "workflow-intake"); err != nil {
		auditErrors = append(auditErrors, fmt.Errorf("persist intake failure event: %w", err))
	}
	if len(auditErrors) != 0 {
		return fmt.Errorf("%s: %w; %w", failureMessage, cause, workflowAuditPersistenceFailure("intake failure", errors.Join(auditErrors...)))
	}
	return fmt.Errorf("%s: %w", failureMessage, cause)
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
	rules := s.ensureDefaultRules()
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

func (s *service) ensureDefaultRules() []models.WorkflowRule {
	for _, rule := range defaultWorkflowRules() {
		_, _ = s.repo.SaveRule(&rule)
	}
	rules, err := s.repo.FindRules()
	if err != nil {
		return defaultWorkflowRules()
	}
	return rules
}

func (s *service) audit(workflowID uuid.UUID, eventType, from, to, message, trigger, rule, sourceURI, actor string) error {
	_, err := s.repo.CreateEvent(&models.WorkflowEvent{
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
	return err
}

func (s *service) recordTransition(workflowID uuid.UUID, from, to, trigger, actor string, approved bool, reason string) error {
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
		return err
	}
	s.projectWorkflowTransition(workflowID, from, to, trigger)
	return nil
}

func (s *service) projectWorkflowTransition(workflowID uuid.UUID, from, to, trigger string) {
	s.projectWorkflowTransitionContext(context.Background(), workflowID, from, to, trigger)
}

func (s *service) projectWorkflowTransitionContext(ctx context.Context, workflowID uuid.UUID, from, to, trigger string) {
	if ctx.Err() != nil {
		return
	}
	err := s.projectWorkflowToLifeGraph(ctx, workflowID)
	if ctx.Err() != nil {
		return
	}
	if err != nil {
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

func (s *service) linkSource(workflowID uuid.UUID, sourceType, sourceID, sourceURI, sourceLabel, relationship string) error {
	expectedRelationship := firstNonEmpty(relationship, "related")
	link, err := s.repo.CreateSourceLink(&models.WorkflowSourceLink{
		WorkflowID:   workflowID,
		SourceType:   sourceType,
		SourceID:     sourceID,
		SourceURI:    sourceURI,
		SourceLabel:  sourceLabel,
		Relationship: expectedRelationship,
	})
	if err != nil {
		return err
	}
	if link == nil || link.ID == uuid.Nil || link.WorkflowID != workflowID || link.SourceType != sourceType ||
		link.SourceID != sourceID || link.SourceURI != sourceURI || link.SourceLabel != sourceLabel || link.Relationship != expectedRelationship {
		return fmt.Errorf("repository did not confirm matching source provenance")
	}
	return nil
}

func (s *service) decide(workflowID uuid.UUID, decisionType, decision, reason, rule string, approved bool, actor string) error {
	_, err := s.repo.CreateDecision(&models.WorkflowDecision{
		WorkflowID:   workflowID,
		DecisionType: decisionType,
		Decision:     decision,
		Reason:       reason,
		RuleApplied:  rule,
		Approved:     approved,
		Actor:        actor,
	})
	return err
}

func (s *service) markChecklistProgress(workflowID uuid.UUID, contains string) error {
	checklist, err := s.repo.FindChecklist(workflowID)
	if err != nil {
		return fmt.Errorf("read completion checklist: %w", err)
	}
	needle := strings.ToLower(contains)
	for _, item := range checklist {
		if item.Status == "done" || !strings.Contains(strings.ToLower(item.Label), needle) {
			continue
		}
		expected := item
		item.Status = "done"
		_, committed, err := s.repo.CommitChecklistUpdate(&expected, &item, models.WorkflowEvent{
			WorkflowID: workflowID, EventType: "workflow.checklist", Message: "checklist item marked done: " + item.Label,
			Trigger: "worker_completion", RuleApplied: "verification completed", Actor: "workflow-worker",
		})
		if err != nil {
			return fmt.Errorf("commit completion checklist: %w", err)
		}
		if !committed {
			return fmt.Errorf("completion checklist changed concurrently; review its current revision")
		}
		return nil
	}
	return nil
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

const (
	automationSelectionProposalAction     = "Select an automation for controlled execution"
	automationSelectionOnlyApprovalPrefix = "automation selection only: "
)

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

func isAutomationSelectionOnlyApproval(item *models.WorkflowItem) bool {
	return item != nil && strings.HasPrefix(
		strings.ToLower(strings.TrimSpace(item.ApprovalReason)),
		automationSelectionOnlyApprovalPrefix,
	)
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

func workflowExplicitlyNamesAutomationSelection(input string) bool {
	return workflowContainsWordOrPhrase(input,
		"run the selected", "execute the selected", "launch the selected", "invoke the selected",
		"selected runtime", "selected automation", "choose the runtime", "choose an automation", "select the exact",
	)
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
	if evidenceClaimsRequired(analysis.taskType) {
		gates = append(gates, models.WorkflowQualityGate{WorkflowID: workflowID, Gate: "human approval", Status: "pending", Reason: "risk/autonomy rule"})
		gates = append(gates, models.WorkflowQualityGate{WorkflowID: workflowID, Gate: "evidence-linked claims", Status: "pending", Reason: "factual claims need provenance"})
	}
	return gates
}

func evidenceClaimsRequired(taskType string) bool {
	switch strings.ToLower(strings.TrimSpace(taskType)) {
	case "legal", "financial", "publishing":
		return true
	default:
		return false
	}
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
		status := strings.ToLower(strings.TrimSpace(claim.Status))
		if claim.NeedsReview || strings.TrimSpace(claim.SourceURI) == "" {
			return true
		}
		switch status {
		case "source_linked", "verified", "human_approved":
		default:
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
		{RuleKey: "developer.github_quality_gate", Name: "Developer completion requires GitHub evidence", Description: "Developer claims require owner-scoped, source-linked commit evidence and concrete build, test, and documentation evidence where available; unsupported branch or platform claims remain in human review. Worker prose and labels alone are not proof.", Category: "developer", Enabled: true},
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
		{ID: "event-triggers", Name: "Event-driven trigger logic", Status: "partial", Implemented: []string{"intake trigger field", "audit trigger log", "connected-source extraction creates workflow candidates", "stable source-record deduplication", "source retraction blocks stale work", "Trello signed callbacks are signature-verified, durable, idempotent, and source-bound; board events enqueue read-only reconciliation and comment changes become source-linked evidence"}, Next: []string{"webhook workers for other connectors (not implemented)"}},
		{ID: "adapters", Name: "Integration adapter layer", Status: "partial", Implemented: []string{"adapter capability names", "source-local-folder path", "allowlisted incremental JSON source bridge", "read-only Gmail, Drive, Contacts, Calendar, and Trello adapters", "read-only Trello signed webhook intake and durable read-only board reconciliation", "read-only GitHub REST sync for repository, issue, pull request, commit, and Actions-run records; branch records are not imported", "task-engine runner adapter"}, Next: []string{"retained sandbox acceptance for every configured account", "GitHub webhook delivery (not implemented)", "dedicated GitHub historical backfill mode (not implemented)", "scoped write adapters"}},
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
		{ID: "universal-intake", Name: "Universal intake engine", Status: "partial", Implemented: []string{"manual/source intake request", "source id/type/content/sender metadata", "normalized intake records", "Trello signed webhook callbacks enter the durable source-processing path; comment evidence is linked to its board source"}, Next: []string{"connector webhook intake for other providers (not implemented)", "voice/screenshot intake"}},
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
		{ID: "developer-github", Name: "Developer/GitHub engine", Status: "implemented", Implemented: []string{"read-only GitHub REST sync imports repository, issue, pull request, commit, and Actions-run records; branch records are not imported", "technical task classification", "source-linked commit and successful Actions evidence gates", "worker prose cannot self-certify GitHub completion"}, Next: []string{"read-only branch comparison and repository acceptance reports (not implemented; no branch-record import)"}},
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

func workflowSourceRevision(request IntakeRequest, input string, selectionContext ...string) string {
	if strings.TrimSpace(request.SourceType) == "" &&
		strings.TrimSpace(request.SourceID) == "" &&
		strings.TrimSpace(request.SourceURI) == "" {
		return ""
	}
	canonical := strings.Join([]string{
		strings.TrimSpace(input),
		strings.TrimSpace(request.ProjectKey),
		strings.TrimSpace(request.ProjectKeyHint),
		strings.TrimSpace(request.AutomationID),
		strings.TrimSpace(request.SourceType),
		strings.TrimSpace(request.SourceID),
		strings.TrimSpace(request.SourceURI),
		strings.TrimSpace(request.SourceLabel),
		strings.TrimSpace(request.ContentType),
		strings.TrimSpace(request.Sender),
		strings.TrimSpace(request.ReceivedAt),
		strings.TrimSpace(request.MandateID),
		fmt.Sprintf("%t", request.RequiresReview),
		strings.TrimSpace(request.ReviewReason),
		strings.Join(request.SuccessCriteria, "\x1d"),
		request.CoordinationPlan.PlanID.String(),
		fmt.Sprintf("%d", request.CoordinationPlan.Revision),
		strings.ToLower(strings.TrimSpace(request.CoordinationPlan.Digest)),
		strings.TrimSpace(request.CoordinationPlan.NodeID),
		strings.Join(selectionContext, "\x1e"),
	}, "\x1f")
	sum := sha256.Sum256([]byte(canonical))
	return fmt.Sprintf("%x", sum)
}

func normalizeWorkflowSuccessCriteria(criteria []string) ([]string, error) {
	if len(criteria) > 50 {
		return nil, fmt.Errorf("success criteria may contain at most 50 items")
	}
	result := make([]string, len(criteria))
	for index, criterion := range criteria {
		criterion = strings.TrimSpace(criterion)
		if criterion == "" {
			return nil, fmt.Errorf("success criterion %d is empty", index+1)
		}
		if len([]rune(criterion)) > 1000 {
			return nil, fmt.Errorf("success criterion %d exceeds 1000 characters", index+1)
		}
		result[index] = criterion
	}
	return result, nil
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
