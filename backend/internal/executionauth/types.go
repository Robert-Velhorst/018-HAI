// Package executionauth is the single policy boundary before HAI performs a
// tool, runtime, filesystem, network, communication, or other external effect.
package executionauth

import (
	"context"
	"encoding/json"
	"time"

	"automation-hub-backend/internal/agentregistry"
	"automation-hub-backend/internal/lifeontology"
	"automation-hub-backend/internal/standingmandate"

	"github.com/google/uuid"
)

const ContractVersion = 1

type Outcome string

const (
	OutcomeAuthorized       Outcome = "authorized"
	OutcomeRequiresApproval Outcome = "requires_approval"
	OutcomeDenied           Outcome = "denied"
)

type ActorKind string

const (
	ActorSystem ActorKind = "system"
	ActorAgent  ActorKind = "agent"
	ActorHuman  ActorKind = "human"
)

type Stage string

const (
	StageDataAccess          Stage = "data_access"
	StageToolUse             Stage = "tool_use"
	StageExpenditure         Stage = "expenditure"
	StageCommunication       Stage = "communication"
	StageCommitment          Stage = "commitment"
	StageExecution           Stage = "execution"
	StagePublication         Stage = "publication"
	StageDeletion            Stage = "deletion"
	StagePrivilegeEscalation Stage = "privilege_escalation"
	StageSelfModification    Stage = "self_modification"
)

type RiskLevel string

const (
	RiskLow      RiskLevel = "low"
	RiskMedium   RiskLevel = "medium"
	RiskHigh     RiskLevel = "high"
	RiskCritical RiskLevel = "critical"
)

// Request is an exact, bounded execution proposal. ApprovalSourceID references
// server-side evidence; callers cannot submit an approval fact directly.
type Request struct {
	OwnerIdentity         string              `json:"ownerIdentity"`
	IdempotencyKey        string              `json:"idempotencyKey"`
	ActorIdentity         string              `json:"actorIdentity"`
	ActorKind             ActorKind           `json:"actorKind"`
	TaskID                string              `json:"taskId"`
	Action                string              `json:"action"`
	Stage                 Stage               `json:"stage"`
	ResourceType          string              `json:"resourceType"`
	ResourceID            string              `json:"resourceId,omitempty"`
	ProjectKey            string              `json:"projectKey,omitempty"`
	Domain                string              `json:"domain,omitempty"`
	ToolID                string              `json:"toolId,omitempty"`
	RuntimeID             string              `json:"runtimeId,omitempty"`
	DataScopes            []string            `json:"dataScopes,omitempty"`
	FolderPaths           []string            `json:"folderPaths,omitempty"`
	RequiredAuthority     int                 `json:"requiredAuthority"`
	RequestedAutonomy     int                 `json:"requestedAutonomy"`
	Risk                  RiskLevel           `json:"risk"`
	Reversible            bool                `json:"reversible"`
	EstimatedCostEUR      float64             `json:"estimatedCostEur"`
	MandateID             string              `json:"mandateId,omitempty"`
	AgentID               string              `json:"agentId,omitempty"`
	AssignmentID          string              `json:"assignmentId,omitempty"`
	ApprovalSourceID      string              `json:"approvalSourceId,omitempty"`
	ApprovalBindingDigest string              `json:"approvalBindingDigest,omitempty"`
	EffectDigest          string              `json:"effectDigest"`
	Facts                 map[string]string   `json:"facts,omitempty"`
	SourceReferences      []string            `json:"sourceReferences,omitempty"`
	Governance            *GovernanceEvidence `json:"governance,omitempty"`
	RequestedAt           time.Time           `json:"requestedAt,omitempty"`
}

type ConstitutionDecision struct {
	ID                           string   `json:"id"`
	Version                      int      `json:"version"`
	Source                       string   `json:"source"`
	Digest                       string   `json:"digest"`
	AuthorityCeiling             int      `json:"authorityCeiling"`
	DeniedCapabilities           []string `json:"deniedCapabilities,omitempty"`
	ApprovalRequiredCapabilities []string `json:"approvalRequiredCapabilities,omitempty"`
}

type ResolvedApproval struct {
	SourceID       string    `json:"sourceId"`
	DecisionID     string    `json:"decisionId"`
	DecisionDigest string    `json:"decisionDigest"`
	BindingDigest  string    `json:"bindingDigest"`
	ApprovedBy     string    `json:"approvedBy"`
	ApproverRoles  []string  `json:"approverRoles,omitempty"`
	ApprovedAt     time.Time `json:"approvedAt"`
	ExpiresAt      time.Time `json:"expiresAt"`
}

type EmergencyStopEvidence struct {
	Active bool   `json:"active"`
	Source string `json:"source"`
	Reason string `json:"reason,omitempty"`
}

type ConstitutionEvidence struct {
	ID                           string   `json:"id"`
	Version                      int      `json:"version"`
	Source                       string   `json:"source"`
	Digest                       string   `json:"digest"`
	RequestedCapabilities        []string `json:"requestedCapabilities"`
	DeniedCapabilities           []string `json:"deniedCapabilities,omitempty"`
	ApprovalRequiredCapabilities []string `json:"approvalRequiredCapabilities,omitempty"`
	AuthorityCeiling             int      `json:"authorityCeiling"`
}

type MandateEvidence struct {
	ID             string `json:"id,omitempty"`
	Revision       uint64 `json:"revision,omitempty"`
	RequestDigest  string `json:"requestDigest,omitempty"`
	MandateDigest  string `json:"mandateDigest,omitempty"`
	DecisionID     string `json:"decisionId,omitempty"`
	DecisionDigest string `json:"decisionDigest,omitempty"`
	Outcome        string `json:"outcome,omitempty"`
}

type AgentEvidence struct {
	AgentID          string `json:"agentId,omitempty"`
	AgentRevision    uint64 `json:"agentRevision,omitempty"`
	AssignmentID     string `json:"assignmentId,omitempty"`
	GrantedAuthority int    `json:"grantedAuthority,omitempty"`
	GrantedAutonomy  int    `json:"grantedAutonomy,omitempty"`
	RuntimeID        string `json:"runtimeId,omitempty"`
}

type SystemWorkloadEvidence struct {
	PolicyID      string `json:"policyId,omitempty"`
	ActorIdentity string `json:"actorIdentity,omitempty"`
	Matched       bool   `json:"matched"`
}

type ApprovalEvidence struct {
	SourceID       string    `json:"sourceId,omitempty"`
	DecisionID     string    `json:"decisionId,omitempty"`
	DecisionDigest string    `json:"decisionDigest,omitempty"`
	ApprovedBy     string    `json:"approvedBy,omitempty"`
	ApprovedAt     time.Time `json:"approvedAt,omitempty"`
	ExpiresAt      time.Time `json:"expiresAt,omitempty"`
}

// FrameworkSelectionSnapshot is the immutable subset of a registry decision
// that execution authorization must resolve independently. Callers may carry
// governance evidence, but they cannot grant themselves a higher ceiling or
// remove an approval requirement by changing that evidence.
type FrameworkSelectionSnapshot struct {
	SelectionID              string
	TaskPlanID               string
	CatalogVersion           string
	SelectorAlgorithmVersion string
	TaskRiskLevel            RiskLevel
	EffectiveRiskCeiling     RiskLevel
	MaximumAutonomyLevel     int
	RequiresApproval         bool
	CatalogDigest            string
	PreferenceDigest         string
	ConstitutionDigest       string
	OperatingContractDigest  string
}

type FrameworkSelectionResolver interface {
	ResolveFrameworkSelection(
		context.Context,
		string,
		string,
	) (FrameworkSelectionSnapshot, error)
}

type FrameworkSelectionVerificationEvidence struct {
	SelectionID              string `json:"selectionId,omitempty"`
	SelectorAlgorithmVersion string `json:"selectorAlgorithmVersion,omitempty"`
	OwnerScoped              bool   `json:"ownerScoped"`
	Verified                 bool   `json:"verified"`
}

// FrameworkEvidencePreflightSnapshot is the immutable, owner-scoped result
// execution authorization must resolve independently before trusting a
// selector-v5 framework evidence preflight asserted by a caller.
type FrameworkEvidencePreflightSnapshot struct {
	OwnerIdentity        string
	TaskPlanID           string
	FrameworkSelectionID string
	PreflightDigest      string
	Status               string
	AssertionsJSON       json.RawMessage
}

type FrameworkEvidencePreflightResolver interface {
	ResolveFrameworkEvidencePreflight(
		context.Context,
		string,
		string,
		string,
		string,
	) (FrameworkEvidencePreflightSnapshot, error)
}

type FrameworkEvidencePreflightVerificationEvidence struct {
	Digest               string `json:"digest,omitempty"`
	OwnerScoped          bool   `json:"ownerScoped"`
	Verified             bool   `json:"verified"`
	SourceClaimsVerified int    `json:"sourceClaimsVerified,omitempty"`
	SourceClaimsDigest   string `json:"sourceClaimsDigest,omitempty"`
}

// GovernanceEvidence binds an execution decision to the server-generated
// planning and framework records that informed it. These records are evidence,
// never authority: Constitution, mandate, assignment, approval, and emergency
// stop evaluation remain independent mandatory policy boundaries.
type GovernanceEvidence struct {
	TaskPlanID                        string    `json:"taskPlanId,omitempty"`
	TaskPlanDigest                    string    `json:"taskPlanDigest,omitempty"`
	FrameworkEvidencePreflightDigest  string    `json:"frameworkEvidencePreflightDigest,omitempty"`
	FrameworkSelectionID              string    `json:"frameworkSelectionId,omitempty"`
	FrameworkCatalogVersion           string    `json:"frameworkCatalogVersion,omitempty"`
	FrameworkSelectorAlgorithmVersion string    `json:"frameworkSelectorAlgorithmVersion,omitempty"`
	FrameworkTaskRiskLevel            RiskLevel `json:"frameworkTaskRiskLevel,omitempty"`
	FrameworkEffectiveRiskCeiling     RiskLevel `json:"frameworkEffectiveRiskCeiling,omitempty"`
	FrameworkMaximumAutonomyLevel     *int      `json:"frameworkMaximumAutonomyLevel,omitempty"`
	FrameworkRequiresApproval         *bool     `json:"frameworkRequiresApproval,omitempty"`
	FrameworkCatalogDigest            string    `json:"frameworkCatalogDigest,omitempty"`
	FrameworkPreferenceDigest         string    `json:"frameworkPreferenceDigest,omitempty"`
	FrameworkConstitutionDigest       string    `json:"frameworkConstitutionDigest,omitempty"`
	FrameworkOperatingContractDigest  string    `json:"frameworkOperatingContractDigest,omitempty"`
	DomainPackDecisionID              string    `json:"domainPackDecisionId,omitempty"`
	DomainPackCatalogVersion          string    `json:"domainPackCatalogVersion,omitempty"`
	DomainPackCatalogDigest           string    `json:"domainPackCatalogDigest,omitempty"`
	DomainPackDecisionDigest          string    `json:"domainPackDecisionDigest,omitempty"`
	ResourceDecisionDigest            string    `json:"resourceDecisionDigest,omitempty"`
	ResourceFeasibility               string    `json:"resourceFeasibility,omitempty"`
	EvidenceReferences                []string  `json:"evidenceReferences,omitempty"`
}

type DecisionEvidence struct {
	EmergencyStop              EmergencyStopEvidence                          `json:"emergencyStop"`
	SystemWorkload             SystemWorkloadEvidence                         `json:"systemWorkload"`
	Constitution               ConstitutionEvidence                           `json:"constitution"`
	Mandate                    MandateEvidence                                `json:"mandate"`
	Agent                      AgentEvidence                                  `json:"agent"`
	Approval                   ApprovalEvidence                               `json:"approval"`
	FrameworkSelection         FrameworkSelectionVerificationEvidence         `json:"frameworkSelection"`
	FrameworkEvidencePreflight FrameworkEvidencePreflightVerificationEvidence `json:"frameworkEvidencePreflight"`
	Governance                 GovernanceEvidence                             `json:"governance"`
	ReasonCodes                []string                                       `json:"reasonCodes"`
	Trace                      []string                                       `json:"trace"`
}

// Receipt is immutable decision evidence. An authorized receipt is not itself
// reusable authority; a caller must atomically consume it immediately before
// the side effect.
type Receipt struct {
	ID                         uuid.UUID                                 `json:"id"`
	ContractVersion            int                                       `json:"contractVersion"`
	OwnerIdentity              string                                    `json:"ownerIdentity"`
	IdempotencyKey             string                                    `json:"idempotencyKey"`
	ActorIdentity              string                                    `json:"actorIdentity"`
	ActorKind                  ActorKind                                 `json:"actorKind"`
	TaskID                     string                                    `json:"taskId"`
	Action                     string                                    `json:"action"`
	Stage                      Stage                                     `json:"stage"`
	ResourceType               string                                    `json:"resourceType"`
	ResourceID                 string                                    `json:"resourceId,omitempty"`
	ProjectKey                 string                                    `json:"projectKey,omitempty"`
	Domain                     string                                    `json:"domain,omitempty"`
	RuntimeID                  string                                    `json:"runtimeId,omitempty"`
	ApprovalSourceID           string                                    `json:"approvalSourceId,omitempty"`
	EffectDigest               string                                    `json:"effectDigest"`
	Outcome                    Outcome                                   `json:"outcome"`
	Reason                     string                                    `json:"reason"`
	RequestDigest              string                                    `json:"requestDigest"`
	DecisionDigest             string                                    `json:"decisionDigest"`
	RequiredAuthority          int                                       `json:"requiredAuthority"`
	RequestedAutonomy          int                                       `json:"requestedAutonomy"`
	EffectiveAutonomy          int                                       `json:"effectiveAutonomy"`
	Risk                       RiskLevel                                 `json:"risk"`
	Reversible                 bool                                      `json:"reversible"`
	EstimatedCostEUR           float64                                   `json:"estimatedCostEur"`
	NotificationRequired       bool                                      `json:"notificationRequired"`
	EvaluatedAt                time.Time                                 `json:"evaluatedAt"`
	Evidence                   DecisionEvidence                          `json:"evidence"`
	LifeGraphProjection        *lifeontology.OperationalProjectionResult `json:"lifeGraphProjection,omitempty"`
	LifeGraphProjectionWarning string                                    `json:"lifeGraphProjectionWarning,omitempty"`
}

type Consumption struct {
	ReceiptID       uuid.UUID `json:"receiptId"`
	OwnerIdentity   string    `json:"ownerIdentity"`
	Consumer        string    `json:"consumer"`
	ExecutionTarget string    `json:"executionTarget"`
	ReceiptDigest   string    `json:"receiptDigest"`
	ConsumedAt      time.Time `json:"consumedAt"`
}

// FinalEffectExercise is an append-only record that the consumed
// authorization was presented at one exact runtime boundary. It is not a new
// authorization and cannot be exercised more than once.
type FinalEffectExercise struct {
	ReceiptID                  uuid.UUID `json:"receiptId"`
	OwnerIdentity              string    `json:"ownerIdentity"`
	RuntimeID                  string    `json:"runtimeId"`
	TaskID                     string    `json:"taskId"`
	Action                     string    `json:"action"`
	ResourceType               string    `json:"resourceType"`
	ResourceID                 string    `json:"resourceId"`
	ProjectKey                 string    `json:"projectKey,omitempty"`
	ApprovalSourceID           string    `json:"approvalSourceId,omitempty"`
	EffectDigest               string    `json:"effectDigest"`
	AuthorizationRequestDigest string    `json:"authorizationRequestDigest"`
	DecisionDigest             string    `json:"decisionDigest"`
	RuntimeRequestDigest       string    `json:"runtimeRequestDigest"`
	ConsumptionTarget          string    `json:"consumptionTarget"`
	ExercisedAt                time.Time `json:"exercisedAt"`
}

type ConstitutionEvaluator interface {
	EvaluateExecutionPolicy(context.Context, string, []string, int) (ConstitutionDecision, error)
}

type MandateAuthorizer interface {
	Authorize(context.Context, uuid.UUID, standingmandate.ActionRequest) (*standingmandate.AuthorizationDecision, error)
	Get(context.Context, string, uuid.UUID) (*standingmandate.StandingMandate, error)
	GetAuthorizationSnapshot(context.Context, string, uuid.UUID) (standingmandate.AuthorizationSnapshot, error)
	GetDecision(context.Context, string, uuid.UUID) (*standingmandate.AuthorizationDecision, error)
}

type AgentAuthorityResolver interface {
	Get(context.Context, string, string) (agentregistry.Agent, error)
	GetAssignment(context.Context, string, string) (agentregistry.Assignment, error)
}

type ApprovalResolver interface {
	Resolve(
		context.Context,
		string,
		string,
		string,
	) (ResolvedApproval, error)
}

type Repository interface {
	CreateOrGet(context.Context, Receipt) (Receipt, bool, error)
	Get(context.Context, string, uuid.UUID) (Receipt, error)
	List(context.Context, string, int) ([]Receipt, error)
	Consume(context.Context, Consumption) error
	GetConsumption(context.Context, string, uuid.UUID) (Consumption, error)
	ExerciseFinalEffect(context.Context, FinalEffectExercise) error
	GetFinalEffectExercise(context.Context, string, uuid.UUID) (FinalEffectExercise, error)
}
