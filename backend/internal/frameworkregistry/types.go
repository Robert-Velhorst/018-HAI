package frameworkregistry

import "time"

const (
	StatusActive       = "active"
	StatusExperimental = "experimental"
	StatusDeprecated   = "deprecated"

	FrameworkVersionStaged  = "staged"
	FrameworkVersionActive  = "active"
	FrameworkVersionRetired = "retired"

	FrameworkLifecycleRegistered = "registered"
	FrameworkLifecycleActivated  = "activated"
	FrameworkLifecycleRetired    = "retired"
	FrameworkLifecycleRolledBack = "rolled_back"

	PreferenceDefault  = "default"
	PreferenceEnabled  = "enabled"
	PreferenceDisabled = "disabled"

	ConstitutionDraft      = "draft"
	ConstitutionActive     = "active"
	ConstitutionSuperseded = "superseded"
)

// Framework is the versioned, inspectable contract used by the chief-of-staff
// selector. It describes a decision discipline; it does not execute tools.
type Framework struct {
	ID                       string   `json:"id"`
	Version                  string   `json:"version"`
	Name                     string   `json:"name"`
	Family                   string   `json:"family"`
	Purpose                  string   `json:"purpose"`
	SuitableProblemTypes     []string `json:"suitableProblemTypes"`
	TriggerConditions        []string `json:"triggerConditions"`
	RequiredInputs           []string `json:"requiredInputs"`
	ProducedOutputs          []string `json:"producedOutputs"`
	RequiredAgents           []string `json:"requiredAgents"`
	WorkflowTemplate         []string `json:"workflowTemplate"`
	DecisionRules            []string `json:"decisionRules"`
	SafetyInvariants         []string `json:"safetyInvariants"`
	AuthorityRequirement     string   `json:"authorityRequirement"`
	MaximumAutonomyLevel     int      `json:"maximumAutonomyLevel"`
	RiskCeiling              string   `json:"riskCeiling"`
	EvidenceRequirements     []string `json:"evidenceRequirements"`
	EvaluationMethod         []string `json:"evaluationMethod"`
	ConflictsWith            []string `json:"conflictsWith"`
	UserSpecificAdaptations  []string `json:"userSpecificAdaptations"`
	CandidateImplementations []string `json:"candidateImplementations,omitempty"`
	Source                   string   `json:"source"`
	Provenance               string   `json:"provenance"`
	Status                   string   `json:"status"`
}

type Preference struct {
	FrameworkID          string    `json:"frameworkId"`
	State                string    `json:"state"`
	Pinned               bool      `json:"pinned"`
	MaximumAutonomyLevel *int      `json:"maximumAutonomyLevel,omitempty"`
	Adaptations          []string  `json:"adaptations"`
	UpdatedAt            time.Time `json:"updatedAt"`
}

type FrameworkView struct {
	Framework
	EffectiveStatus        string     `json:"effectiveStatus"`
	Enabled                bool       `json:"enabled"`
	Pinned                 bool       `json:"pinned"`
	EffectiveAutonomyLevel int        `json:"effectiveAutonomyLevel"`
	Adaptations            []string   `json:"adaptations"`
	PreferenceUpdatedAt    *time.Time `json:"preferenceUpdatedAt,omitempty"`
}

// FrameworkVersionProvenance identifies where an immutable framework version
// came from. ContentDigest is computed by the registry and cannot be supplied
// by callers.
type FrameworkVersionProvenance struct {
	Source        string    `json:"source"`
	Reference     string    `json:"reference,omitempty"`
	AuthoredBy    string    `json:"authoredBy"`
	ImportedBy    string    `json:"importedBy"`
	ImportedAt    time.Time `json:"importedAt"`
	ParentDigest  string    `json:"parentDigest,omitempty"`
	ContentDigest string    `json:"contentDigest"`
}

// FrameworkMigrationMetadata records the compatibility contract for moving
// from one framework version to another. It is copied into lifecycle events so
// activation history remains independently auditable.
type FrameworkMigrationMetadata struct {
	FromVersion        string   `json:"fromVersion"`
	Strategy           string   `json:"strategy"`
	ChangeSummary      string   `json:"changeSummary"`
	CompatibilityNotes string   `json:"compatibilityNotes"`
	MigrationSteps     []string `json:"migrationSteps"`
	ValidationCriteria []string `json:"validationCriteria"`
}

// FrameworkVersionRecord wraps immutable framework content with its current
// lifecycle projection. State transitions are preserved separately as
// hash-linked FrameworkLifecycleEvent records.
type FrameworkVersionRecord struct {
	Framework
	LifecycleState    string                     `json:"lifecycleState"`
	Supersedes        string                     `json:"supersedes,omitempty"`
	Migration         FrameworkMigrationMetadata `json:"migration"`
	VersionProvenance FrameworkVersionProvenance `json:"versionProvenance"`
	RegisteredAt      time.Time                  `json:"registeredAt"`
	ActivatedAt       *time.Time                 `json:"activatedAt,omitempty"`
	RetiredAt         *time.Time                 `json:"retiredAt,omitempty"`
}

type FrameworkVersionView struct {
	FrameworkVersionRecord
	Enabled                bool       `json:"enabled"`
	Pinned                 bool       `json:"pinned"`
	EffectiveAutonomyLevel int        `json:"effectiveAutonomyLevel"`
	Adaptations            []string   `json:"adaptations"`
	PreferenceUpdatedAt    *time.Time `json:"preferenceUpdatedAt,omitempty"`
}

// FrameworkLifecycleEvent is append-only and hash-linked. EventDigest covers
// the full event except EventDigest itself, including PreviousEventDigest.
type FrameworkLifecycleEvent struct {
	Sequence              uint64                     `json:"sequence"`
	ID                    string                     `json:"id"`
	FrameworkID           string                     `json:"frameworkId"`
	Version               string                     `json:"version"`
	Action                string                     `json:"action"`
	PreviousActiveVersion string                     `json:"previousActiveVersion,omitempty"`
	ActiveVersion         string                     `json:"activeVersion,omitempty"`
	Actor                 string                     `json:"actor"`
	Reason                string                     `json:"reason"`
	Migration             FrameworkMigrationMetadata `json:"migration"`
	Provenance            FrameworkVersionProvenance `json:"provenance"`
	OccurredAt            time.Time                  `json:"occurredAt"`
	PreviousEventDigest   string                     `json:"previousEventDigest,omitempty"`
	EventDigest           string                     `json:"eventDigest"`
}

type StageFrameworkVersionRequest struct {
	Framework  Framework                  `json:"framework"`
	Actor      string                     `json:"actor"`
	Supersedes string                     `json:"supersedes,omitempty"`
	Migration  FrameworkMigrationMetadata `json:"migration"`
	Provenance FrameworkVersionProvenance `json:"provenance"`
}

type ActivateFrameworkVersionRequest struct {
	Actor                 string `json:"actor"`
	Reason                string `json:"reason"`
	ExpectedActiveVersion string `json:"expectedActiveVersion"`
	ExpectedTargetDigest  string `json:"expectedTargetDigest"`
}

type RollbackFrameworkVersionRequest struct {
	Actor                 string `json:"actor"`
	Reason                string `json:"reason"`
	ExpectedActiveVersion string `json:"expectedActiveVersion"`
	ExpectedTargetDigest  string `json:"expectedTargetDigest"`
}

type RetireFrameworkVersionRequest struct {
	Actor                string `json:"actor"`
	Reason               string `json:"reason"`
	ExpectedTargetDigest string `json:"expectedTargetDigest"`
}

type PreferencePatch struct {
	Reason                string   `json:"reason,omitempty"`
	State                 string   `json:"state"`
	Pinned                *bool    `json:"pinned,omitempty"`
	MaximumAutonomyLevel  *int     `json:"maximumAutonomyLevel,omitempty"`
	ClearAutonomyOverride bool     `json:"clearAutonomyOverride,omitempty"`
	Adaptations           []string `json:"adaptations,omitempty"`
}

type PreferenceChangeEvent struct {
	Sequence            uint64      `json:"sequence"`
	ID                  string      `json:"id"`
	FrameworkID         string      `json:"frameworkId"`
	Actor               string      `json:"actor"`
	Reason              string      `json:"reason"`
	Before              *Preference `json:"before,omitempty"`
	After               Preference  `json:"after"`
	OccurredAt          time.Time   `json:"occurredAt"`
	PreviousEventDigest string      `json:"previousEventDigest,omitempty"`
	EventDigest         string      `json:"eventDigest"`
}

type SelectionRequest struct {
	OwnerIdentity       string   `json:"-"`
	TaskPlanID          string   `json:"taskPlanId,omitempty"`
	Request             string   `json:"request"`
	ProjectKey          string   `json:"projectKey,omitempty"`
	PursuitID           string   `json:"pursuitId,omitempty"`
	TaskType            string   `json:"taskType,omitempty"`
	RiskLevel           string   `json:"riskLevel,omitempty"`
	Difficulty          int      `json:"difficulty,omitempty"`
	RequiredReasoning   string   `json:"requiredReasoning,omitempty"`
	SuccessCriteria     []string `json:"successCriteria,omitempty"`
	NeedsMemory         bool     `json:"needsMemory,omitempty"`
	NeedsTools          bool     `json:"needsTools,omitempty"`
	NeedsDocuments      bool     `json:"needsDocuments,omitempty"`
	NeedsWebAccess      bool     `json:"needsWebAccess,omitempty"`
	NeedsLocalExecution bool     `json:"needsLocalExecution,omitempty"`
	NeedsApproval       bool     `json:"needsApproval,omitempty"`
	ExecuteRequested    bool     `json:"executeRequested,omitempty"`
	HumanApproved       bool     `json:"humanApproved,omitempty"`
	// The operating context fields are trusted in-process inputs. Browser
	// previews cannot assert human capacity, available agents, or observed
	// needs because those signals must retain their own provenance.
	ObservedNeeds             []NeedStateAssessment `json:"-"`
	Capacity                  *CapacitySnapshot     `json:"-"`
	AvailableAgents           []AgentCard           `json:"-"`
	PreferredCoordinationMode string                `json:"-"`
	Deadline                  *time.Time            `json:"-"`
}

type SelectedFramework struct {
	ID                   string   `json:"id"`
	Version              string   `json:"version"`
	Name                 string   `json:"name"`
	Family               string   `json:"family"`
	RiskCeiling          string   `json:"riskCeiling"`
	Score                float64  `json:"score"`
	Reasons              []string `json:"reasons"`
	MaximumAutonomyLevel int      `json:"maximumAutonomyLevel"`
	AuthorityRequirement string   `json:"authorityRequirement"`
	EvidenceRequirements []string `json:"evidenceRequirements"`
	EvaluationMethod     []string `json:"evaluationMethod"`
}

type FrameworkConflict struct {
	SelectedID string `json:"selectedId"`
	SkippedID  string `json:"skippedId"`
	Reason     string `json:"reason"`
}

// SelectionDecision is an auditable chief-of-staff recommendation. Authority
// is a ceiling and cannot grant execution permission by itself.
type SelectionDecision struct {
	ID                        string                   `json:"id"`
	TaskPlanID                string                   `json:"taskPlanId,omitempty"`
	CreatedAt                 time.Time                `json:"createdAt"`
	CatalogVersion            string                   `json:"catalogVersion"`
	CatalogDigest             string                   `json:"catalogDigest"`
	SelectorAlgorithmVersion  string                   `json:"selectorAlgorithmVersion"`
	TaskRiskLevel             string                   `json:"taskRiskLevel,omitempty"`
	EffectiveRiskCeiling      string                   `json:"effectiveRiskCeiling,omitempty"`
	EffectivePreferenceDigest string                   `json:"effectivePreferenceDigest"`
	ConstitutionDigest        string                   `json:"constitutionDigest"`
	LifeDomain                string                   `json:"lifeDomain"`
	NeedOrCommitment          string                   `json:"needOrCommitment"`
	Selected                  []SelectedFramework      `json:"selected"`
	Conflicts                 []FrameworkConflict      `json:"conflicts"`
	RequiredAgents            []string                 `json:"requiredAgents"`
	MaximumAutonomyLevel      int                      `json:"maximumAutonomyLevel"`
	AuthoritySummary          string                   `json:"authoritySummary"`
	RequiresApproval          bool                     `json:"requiresApproval"`
	ApprovalReasons           []string                 `json:"approvalReasons"`
	EvidenceRequirements      []string                 `json:"evidenceRequirements"`
	CompletionCriteria        []string                 `json:"completionCriteria"`
	LearningPlan              []string                 `json:"learningPlan"`
	ContextRequirements       []string                 `json:"contextRequirements"`
	SelectionReason           string                   `json:"selectionReason"`
	ConstitutionVersion       int                      `json:"constitutionVersion"`
	ConstitutionSource        string                   `json:"constitutionSource"`
	LifeDomains               []LifeDomainAssignment   `json:"lifeDomains"`
	NeedsState                []NeedStateAssessment    `json:"needsState"`
	Capacity                  CapacitySnapshot         `json:"capacity"`
	AgentCards                []AgentCard              `json:"agentCards"`
	Delegations               []DelegationContract     `json:"delegations"`
	Communication             CommunicationContract    `json:"communication"`
	Coordination              CoordinationPlan         `json:"coordination"`
	ActionAutonomy            []ActionAutonomyDecision `json:"actionAutonomy"`
	StopConditions            []string                 `json:"stopConditions"`
	OutcomeMonitoring         []string                 `json:"outcomeMonitoring"`
	ChiefOfStaff              ChiefOfStaffDecision     `json:"chiefOfStaff"`
	OperatingContractDigest   string                   `json:"operatingContractDigest,omitempty"`
}

type LifeDomainAssignment struct {
	ID         string   `json:"id"`
	Need       string   `json:"need"`
	Score      int      `json:"score"`
	Confidence float64  `json:"confidence"`
	Signals    []string `json:"signals"`
	Primary    bool     `json:"primary"`
	Source     string   `json:"source"`
}

type NeedStateAssessment struct {
	ID          string   `json:"id"`
	DomainID    string   `json:"domainId,omitempty"`
	Level       string   `json:"level"`
	State       string   `json:"state"`
	Priority    int      `json:"priority"`
	Confidence  float64  `json:"confidence"`
	Evidence    []string `json:"evidence"`
	Source      string   `json:"source"`
	NeedsReview bool     `json:"needsReview"`
}

type CapacitySnapshot struct {
	Status               string     `json:"status"`
	Energy               int        `json:"energy,omitempty"`
	Attention            int        `json:"attention,omitempty"`
	TimeAvailableMinutes int        `json:"timeAvailableMinutes,omitempty"`
	ConcurrentWorkLimit  int        `json:"concurrentWorkLimit,omitempty"`
	CurrentLoad          int        `json:"currentLoad,omitempty"`
	PlanningStepLimit    int        `json:"planningStepLimit"`
	Constraints          []string   `json:"constraints"`
	SourceURI            string     `json:"sourceUri,omitempty"`
	SourceLabel          string     `json:"sourceLabel,omitempty"`
	CapturedAt           *time.Time `json:"capturedAt,omitempty"`
	Confidence           float64    `json:"confidence"`
	Fresh                bool       `json:"fresh"`
	NeedsReview          bool       `json:"needsReview"`
}

type AgentCard struct {
	ID                    string     `json:"id"`
	Name                  string     `json:"name"`
	Owner                 string     `json:"owner"`
	Purpose               string     `json:"purpose"`
	Role                  string     `json:"role"`
	Capabilities          []string   `json:"capabilities"`
	DomainCompetence      []string   `json:"domainCompetence"`
	AllowedTools          []string   `json:"allowedTools"`
	RequiredPermissions   []string   `json:"requiredPermissions"`
	DataAccessBoundaries  []string   `json:"dataAccessBoundaries"`
	CostProfile           string     `json:"costProfile"`
	ModelRequirements     []string   `json:"modelRequirements"`
	ReliabilityHistory    []string   `json:"reliabilityHistory"`
	AllowedActions        []string   `json:"allowedActions"`
	ProhibitedActions     []string   `json:"prohibitedActions"`
	InputSchema           string     `json:"inputSchema"`
	OutputSchema          string     `json:"outputSchema"`
	ExpectedEvidence      []string   `json:"expectedEvidence"`
	EscalationRoute       string     `json:"escalationRoute"`
	Availability          string     `json:"availability"`
	Version               string     `json:"version"`
	Dependencies          []string   `json:"dependencies"`
	HealthStatus          string     `json:"healthStatus"`
	EvaluationScore       float64    `json:"evaluationScore"`
	EvaluationScoreSource string     `json:"evaluationScoreSource"`
	AuthorityCeiling      int        `json:"authorityCeiling"`
	Status                string     `json:"status"`
	Verified              bool       `json:"verified"`
	Revoked               bool       `json:"revoked"`
	RevocationReason      string     `json:"revocationReason,omitempty"`
	Provenance            string     `json:"provenance"`
	LastVerifiedAt        *time.Time `json:"lastVerifiedAt,omitempty"`
}

type DelegationContract struct {
	ID                 string     `json:"id"`
	Delegator          string     `json:"delegator"`
	Delegatee          string     `json:"delegatee"`
	Objective          string     `json:"objective"`
	AllowedActions     []string   `json:"allowedActions"`
	ProhibitedActions  []string   `json:"prohibitedActions"`
	BudgetLimitEUR     float64    `json:"budgetLimitEur"`
	BudgetPolicy       string     `json:"budgetPolicy"`
	Deadline           *time.Time `json:"deadline,omitempty"`
	DeadlineStatus     string     `json:"deadlineStatus"`
	Constraints        []string   `json:"constraints"`
	AuthorityCeiling   int        `json:"authorityCeiling"`
	RequiresApproval   bool       `json:"requiresApproval"`
	EvidenceRequired   []string   `json:"evidenceRequired"`
	CompletionCriteria []string   `json:"completionCriteria"`
	EscalationTriggers []string   `json:"escalationTriggers"`
	State              string     `json:"state"`
}

type CommunicationContract struct {
	SchemaVersion          string   `json:"schemaVersion"`
	AllowedMessageTypes    []string `json:"allowedMessageTypes"`
	AllowedConfidentiality []string `json:"allowedConfidentiality"`
	RequiredFields         []string `json:"requiredFields"`
	ForbiddenContent       []string `json:"forbiddenContent"`
	MaximumAuthority       int      `json:"maximumAuthority"`
	MaximumPayloadChars    int      `json:"maximumPayloadChars"`
	MaximumTTLSeconds      int      `json:"maximumTtlSeconds"`
	RedactionRequired      bool     `json:"redactionRequired"`
	IdempotencyRequired    bool     `json:"idempotencyRequired"`
	ProvenanceRequired     bool     `json:"provenanceRequired"`
	SignaturePolicy        string   `json:"signaturePolicy"`
	CorrelationID          string   `json:"correlationId"`
}

type AgentMessage struct {
	ID                string     `json:"id"`
	IdempotencyKey    string     `json:"idempotencyKey"`
	SchemaVersion     string     `json:"schemaVersion"`
	CorrelationID     string     `json:"correlationId"`
	Sender            string     `json:"sender"`
	Recipient         string     `json:"recipient"`
	MessageType       string     `json:"messageType"`
	Confidentiality   string     `json:"confidentiality"`
	AuthorityCeiling  int        `json:"authorityCeiling"`
	EvidenceRefs      []string   `json:"evidenceRefs"`
	PayloadSummary    string     `json:"payloadSummary"`
	PayloadDigest     string     `json:"payloadDigest"`
	Provenance        string     `json:"provenance"`
	SignatureDigest   string     `json:"signatureDigest,omitempty"`
	SignatureVerified bool       `json:"signatureVerified"`
	CreatedAt         time.Time  `json:"createdAt"`
	ExpiresAt         *time.Time `json:"expiresAt"`
}

type CoordinationPlan struct {
	Mode           string   `json:"mode"`
	AllowedModes   []string `json:"allowedModes"`
	Coordinator    string   `json:"coordinator"`
	Participants   []string `json:"participants"`
	HandoffOrder   []string `json:"handoffOrder"`
	ConsensusRule  string   `json:"consensusRule"`
	EscalationRule string   `json:"escalationRule"`
	Rationale      string   `json:"rationale"`
}

type ActionAutonomyDecision struct {
	Action           string `json:"action"`
	RequiredLevel    int    `json:"requiredLevel"`
	EffectiveCeiling int    `json:"effectiveCeiling"`
	LevelName        string `json:"levelName"`
	Allowed          bool   `json:"allowed"`
	RequiresApproval bool   `json:"requiresApproval"`
	Reason           string `json:"reason"`
}

type ChiefOfStaffDecision struct {
	NeedsAttention  string `json:"needsAttention"`
	WhyNow          string `json:"whyNow"`
	ContextNeeded   string `json:"contextNeeded"`
	WhoShouldAct    string `json:"whoShouldAct"`
	HowToProceed    string `json:"howToProceed"`
	MayProceedNow   string `json:"mayProceedNow"`
	NeedsApproval   string `json:"needsApproval"`
	CompletionProof string `json:"completionProof"`
}

type Constitution struct {
	ID                  string     `json:"id"`
	Version             int        `json:"version"`
	BaseVersion         int        `json:"baseVersion"`
	Status              string     `json:"status"`
	Values              []string   `json:"values"`
	Prohibitions        []string   `json:"prohibitions"`
	StandingPermissions []string   `json:"standingPermissions"`
	Preferences         []string   `json:"preferences"`
	RelationshipRules   []string   `json:"relationshipRules"`
	FinancialBoundaries []string   `json:"financialBoundaries"`
	CommunicationRules  []string   `json:"communicationRules"`
	EscalationRules     []string   `json:"escalationRules"`
	ProtectedRules      []string   `json:"protectedRules"`
	ChangeSummary       string     `json:"changeSummary,omitempty"`
	ApprovedBy          string     `json:"approvedBy,omitempty"`
	ApprovedAt          *time.Time `json:"approvedAt,omitempty"`
	CreatedAt           time.Time  `json:"createdAt"`
}

// ConstitutionHistoryEntry exposes immutable version metadata without
// returning the full governance contract or the private approval note.
type ConstitutionHistoryEntry struct {
	ID            string     `json:"id"`
	Version       int        `json:"version"`
	BaseVersion   int        `json:"baseVersion"`
	Status        string     `json:"status"`
	ChangeSummary string     `json:"changeSummary"`
	ApprovedBy    string     `json:"approvedBy,omitempty"`
	ApprovedAt    *time.Time `json:"approvedAt,omitempty"`
	CreatedAt     time.Time  `json:"createdAt"`
	Digest        string     `json:"digest"`
}

type ConstitutionHistoryPage struct {
	History   []ConstitutionHistoryEntry `json:"history"`
	Limit     int                        `json:"limit"`
	Truncated bool                       `json:"truncated"`
}

type ConstitutionDraftRequest struct {
	BaseVersion         int      `json:"baseVersion,omitempty"`
	Values              []string `json:"values"`
	Prohibitions        []string `json:"prohibitions"`
	StandingPermissions []string `json:"standingPermissions"`
	Preferences         []string `json:"preferences"`
	RelationshipRules   []string `json:"relationshipRules"`
	FinancialBoundaries []string `json:"financialBoundaries"`
	CommunicationRules  []string `json:"communicationRules"`
	EscalationRules     []string `json:"escalationRules"`
	ChangeSummary       string   `json:"changeSummary"`
}

type ActivateConstitutionRequest struct {
	Confirmation string `json:"confirmation"`
	ApprovalNote string `json:"approvalNote"`
}

type Overview struct {
	GeneratedAt         time.Time      `json:"generatedAt"`
	Total               int            `json:"total"`
	Enabled             int            `json:"enabled"`
	Experimental        int            `json:"experimental"`
	Deprecated          int            `json:"deprecated"`
	Pinned              int            `json:"pinned"`
	Families            map[string]int `json:"families"`
	ConstitutionVersion int            `json:"constitutionVersion"`
	ConstitutionSource  string         `json:"constitutionSource"`
	RecentSelections    int            `json:"recentSelections"`
	SelectionContract   []string       `json:"selectionContract"`
}
