package models

import (
	"time"

	"github.com/google/uuid"
)

type WorkflowItem struct {
	ID uuid.UUID `gorm:"type:uuid;primary_key;default:uuid_generate_v4()" json:"id,omitempty"`
	// OwnerIdentity is the verified user whose intake created this workflow.
	// It is not client-controlled or exposed in workflow API payloads.
	OwnerIdentity             string     `gorm:"type:varchar(255);index" json:"-"`
	Title                     string     `gorm:"type:varchar(512);index;not null" json:"title"`
	Description               string     `gorm:"type:text" json:"description,omitempty"`
	ProjectKey                string     `gorm:"type:varchar(255);index" json:"projectKey,omitempty"`
	AutomationID              string     `gorm:"type:varchar(64);index" json:"automationId,omitempty"`
	MandateID                 *uuid.UUID `gorm:"type:uuid;index" json:"mandateId,omitempty"`
	CoordinationPlanID        *uuid.UUID `gorm:"type:uuid;index" json:"coordinationPlanId,omitempty"`
	CoordinationPlanRevision  uint64     `gorm:"not null;default:0" json:"coordinationPlanRevision,omitempty"`
	CoordinationPlanDigest    string     `gorm:"type:char(64);not null;default:''" json:"coordinationPlanDigest,omitempty"`
	CoordinationPlanNodeID    string     `gorm:"type:varchar(160);not null;default:''" json:"coordinationPlanNodeId,omitempty"`
	CoordinationDraftPlanID   *uuid.UUID `gorm:"type:uuid;index" json:"coordinationDraftPlanId,omitempty"`
	CoordinationDraftRevision uint64     `gorm:"not null;default:0" json:"coordinationDraftRevision,omitempty"`
	CoordinationDraftDigest   string     `gorm:"type:char(64);not null;default:''" json:"coordinationDraftDigest,omitempty"`
	CoordinationDraftNodeID   string     `gorm:"type:varchar(160);not null;default:''" json:"coordinationDraftNodeId,omitempty"`
	CurrentState              string     `gorm:"type:varchar(80);index;not null" json:"currentState"`
	TaskType                  string     `gorm:"type:varchar(80);index" json:"taskType"`
	RiskLevel                 string     `gorm:"type:varchar(80);index" json:"riskLevel"`
	PriorityScore             int        `gorm:"index" json:"priorityScore"`
	Confidence                float64    `json:"confidence"`
	AutonomyLevel             string     `gorm:"type:varchar(80);index" json:"autonomyLevel"`
	RequiresApproval          bool       `gorm:"index" json:"requiresApproval"`
	ApprovalStatus            string     `gorm:"type:varchar(50);index" json:"approvalStatus"`
	ApprovalReason            string     `gorm:"type:text" json:"approvalReason,omitempty"`
	BlockedReason             string     `gorm:"type:text" json:"blockedReason,omitempty"`
	NextAction                string     `gorm:"type:text" json:"nextAction,omitempty"`
	SourceType                string     `gorm:"type:varchar(80);index" json:"sourceType,omitempty"`
	SourceID                  string     `gorm:"type:varchar(120);index" json:"sourceId,omitempty"`
	SourceURI                 string     `gorm:"type:varchar(1024)" json:"sourceUri,omitempty"`
	SourceLabel               string     `gorm:"type:varchar(512)" json:"sourceLabel,omitempty"`
	SourceRevision            string     `gorm:"type:varchar(64);index" json:"sourceRevision,omitempty"`
	DueAt                     *time.Time `gorm:"index" json:"dueAt,omitempty"`
	RetryCount                int        `gorm:"default:0;index" json:"retryCount"`
	MaxRetries                int        `gorm:"default:2" json:"maxRetries"`
	NextRunAt                 *time.Time `gorm:"index" json:"nextRunAt,omitempty"`
	LastRunAt                 *time.Time `json:"lastRunAt,omitempty"`
	WorkerClaimID             string     `gorm:"type:varchar(64);index" json:"-"`
	WorkerLeaseUntil          *time.Time `gorm:"index" json:"workerLeaseUntil,omitempty"`
	CompletedAt               *time.Time `gorm:"index" json:"completedAt,omitempty"`
	VerificationStatus        string     `gorm:"type:varchar(80);index" json:"verificationStatus,omitempty"`
	RecoveryStatus            string     `gorm:"type:varchar(80);index" json:"recoveryStatus,omitempty"`
	RecoveryNote              string     `gorm:"type:text" json:"recoveryNote,omitempty"`
	LastTaskPlanID            string     `gorm:"type:varchar(120)" json:"lastTaskPlanId,omitempty"`
	LastWorkerError           string     `gorm:"type:text" json:"lastWorkerError,omitempty"`
	Archived                  bool       `gorm:"default:false;index" json:"archived"`
	CreatedAt                 time.Time  `json:"createdAt"`
	UpdatedAt                 time.Time  `json:"updatedAt"`
}

type WorkflowChecklistItem struct {
	ID               uuid.UUID  `gorm:"type:uuid;primary_key;default:uuid_generate_v4()" json:"id,omitempty"`
	WorkflowID       uuid.UUID  `gorm:"type:uuid;index;not null" json:"workflowId"`
	Label            string     `gorm:"type:text;not null" json:"label"`
	Status           string     `gorm:"type:varchar(50);index;not null" json:"status"`
	Position         int        `gorm:"index" json:"position"`
	RequiresApproval bool       `gorm:"index" json:"requiresApproval"`
	DueAt            *time.Time `gorm:"index" json:"dueAt,omitempty"`
	ReminderAt       *time.Time `gorm:"index" json:"reminderAt,omitempty"`
	CreatedAt        time.Time  `json:"createdAt"`
	UpdatedAt        time.Time  `json:"updatedAt"`
}

type WorkflowIntakeRecord struct {
	ID                uuid.UUID  `gorm:"type:uuid;primary_key;default:uuid_generate_v4()" json:"id,omitempty"`
	WorkflowID        uuid.UUID  `gorm:"type:uuid;index" json:"workflowId,omitempty"`
	SourceType        string     `gorm:"type:varchar(80);index" json:"sourceType,omitempty"`
	SourceID          string     `gorm:"type:varchar(120);index" json:"sourceId,omitempty"`
	SourceURI         string     `gorm:"type:varchar(1024);index" json:"sourceUri,omitempty"`
	SourceLabel       string     `gorm:"type:varchar(512)" json:"sourceLabel,omitempty"`
	ContentType       string     `gorm:"type:varchar(80);index" json:"contentType,omitempty"`
	Sender            string     `gorm:"type:varchar(255);index" json:"sender,omitempty"`
	ReceivedAt        *time.Time `gorm:"index" json:"receivedAt,omitempty"`
	RawContent        string     `gorm:"type:text" json:"rawContent,omitempty"`
	NormalizedSummary string     `gorm:"type:text" json:"normalizedSummary,omitempty"`
	DetectedEntities  string     `gorm:"type:text" json:"detectedEntities,omitempty"`
	PossibleProject   string     `gorm:"type:varchar(255);index" json:"possibleProject,omitempty"`
	Urgency           string     `gorm:"type:varchar(50);index" json:"urgency,omitempty"`
	CreatedAt         time.Time  `json:"createdAt"`
}

type WorkflowProjectMatch struct {
	ID             uuid.UUID `gorm:"type:uuid;primary_key;default:uuid_generate_v4()" json:"id,omitempty"`
	WorkflowID     uuid.UUID `gorm:"type:uuid;index;not null" json:"workflowId"`
	ProjectKey     string    `gorm:"type:varchar(255);index;not null" json:"projectKey"`
	MatchedBy      string    `gorm:"type:text" json:"matchedBy,omitempty"`
	Confidence     float64   `gorm:"index" json:"confidence"`
	TrelloCardRef  string    `gorm:"type:varchar(512)" json:"trelloCardRef,omitempty"`
	DriveFolderRef string    `gorm:"type:varchar(512)" json:"driveFolderRef,omitempty"`
	CreatedAt      time.Time `json:"createdAt"`
}

type WorkflowEvidenceClaim struct {
	ID          uuid.UUID `gorm:"type:uuid;primary_key;default:uuid_generate_v4()" json:"id,omitempty"`
	WorkflowID  uuid.UUID `gorm:"type:uuid;index;not null" json:"workflowId"`
	ClaimText   string    `gorm:"type:text;not null" json:"claimText"`
	SourceURI   string    `gorm:"type:varchar(1024);index" json:"sourceUri,omitempty"`
	SourceLabel string    `gorm:"type:varchar(512)" json:"sourceLabel,omitempty"`
	Reliability string    `gorm:"type:varchar(80);index" json:"reliability"`
	Status      string    `gorm:"type:varchar(80);index" json:"status"`
	NeedsReview bool      `gorm:"index" json:"needsReview"`
	CreatedAt   time.Time `json:"createdAt"`
}

type WorkflowOpenLoop struct {
	ID               uuid.UUID  `gorm:"type:uuid;primary_key;default:uuid_generate_v4()" json:"id,omitempty"`
	WorkflowID       uuid.UUID  `gorm:"type:uuid;index;not null" json:"workflowId"`
	ResponsibleParty string     `gorm:"type:varchar(120);index" json:"responsibleParty"`
	WaitingFor       string     `gorm:"type:text" json:"waitingFor"`
	NextAction       string     `gorm:"type:text" json:"nextAction"`
	FollowUpAt       *time.Time `gorm:"index" json:"followUpAt,omitempty"`
	Status           string     `gorm:"type:varchar(50);index" json:"status"`
	ClaimID          string     `gorm:"type:varchar(64);index" json:"-"`
	LeaseUntil       *time.Time `gorm:"index" json:"leaseUntil,omitempty"`
	CreatedAt        time.Time  `json:"createdAt"`
	UpdatedAt        time.Time  `json:"updatedAt"`
}

type WorkflowProposal struct {
	ID                uuid.UUID  `gorm:"type:uuid;primary_key;default:uuid_generate_v4()" json:"id,omitempty"`
	WorkflowID        uuid.UUID  `gorm:"type:uuid;index;not null" json:"workflowId"`
	RecommendedAction string     `gorm:"type:text;not null" json:"recommendedAction"`
	Options           string     `gorm:"type:text" json:"options,omitempty"`
	SelectedOption    string     `gorm:"type:text" json:"selectedOption,omitempty"`
	ResolutionNote    string     `gorm:"type:text" json:"resolutionNote,omitempty"`
	ResolvedBy        string     `gorm:"type:varchar(120)" json:"resolvedBy,omitempty"`
	ResolvedAt        *time.Time `gorm:"index" json:"resolvedAt,omitempty"`
	Status            string     `gorm:"type:varchar(50);index" json:"status"`
	CreatedAt         time.Time  `json:"createdAt"`
	UpdatedAt         time.Time  `json:"updatedAt"`
}

type WorkflowQualityGate struct {
	ID         uuid.UUID `gorm:"type:uuid;primary_key;default:uuid_generate_v4()" json:"id,omitempty"`
	WorkflowID uuid.UUID `gorm:"type:uuid;index;not null" json:"workflowId"`
	Gate       string    `gorm:"type:varchar(120);index;not null" json:"gate"`
	Status     string    `gorm:"type:varchar(50);index;not null" json:"status"`
	Reason     string    `gorm:"type:text" json:"reason,omitempty"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

type WorkflowRule struct {
	ID          uuid.UUID `gorm:"type:uuid;primary_key;default:uuid_generate_v4()" json:"id,omitempty"`
	RuleKey     string    `gorm:"type:varchar(120);uniqueIndex;not null" json:"ruleKey"`
	Name        string    `gorm:"type:varchar(255);not null" json:"name"`
	Description string    `gorm:"type:text" json:"description"`
	Category    string    `gorm:"type:varchar(80);index" json:"category"`
	Enabled     bool      `gorm:"default:true;index" json:"enabled"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type WorkflowTransition struct {
	ID         uuid.UUID `gorm:"type:uuid;primary_key;default:uuid_generate_v4()" json:"id,omitempty"`
	WorkflowID uuid.UUID `gorm:"type:uuid;index;not null" json:"workflowId"`
	FromState  string    `gorm:"type:varchar(80);index" json:"fromState,omitempty"`
	ToState    string    `gorm:"type:varchar(80);index;not null" json:"toState"`
	Trigger    string    `gorm:"type:varchar(255);index" json:"trigger,omitempty"`
	Actor      string    `gorm:"type:varchar(120)" json:"actor,omitempty"`
	Approved   bool      `gorm:"index" json:"approved"`
	Reason     string    `gorm:"type:text" json:"reason,omitempty"`
	CreatedAt  time.Time `json:"createdAt"`
}

type WorkflowSourceLink struct {
	ID           uuid.UUID `gorm:"type:uuid;primary_key;default:uuid_generate_v4()" json:"id,omitempty"`
	WorkflowID   uuid.UUID `gorm:"type:uuid;index;not null" json:"workflowId"`
	SourceType   string    `gorm:"type:varchar(80);index" json:"sourceType,omitempty"`
	SourceID     string    `gorm:"type:varchar(120);index" json:"sourceId,omitempty"`
	SourceURI    string    `gorm:"type:varchar(1024);index" json:"sourceUri,omitempty"`
	SourceLabel  string    `gorm:"type:varchar(512)" json:"sourceLabel,omitempty"`
	Relationship string    `gorm:"type:varchar(80);index" json:"relationship"`
	CreatedAt    time.Time `json:"createdAt"`
}

type WorkflowDecision struct {
	ID           uuid.UUID `gorm:"type:uuid;primary_key;default:uuid_generate_v4()" json:"id,omitempty"`
	WorkflowID   uuid.UUID `gorm:"type:uuid;index;not null" json:"workflowId"`
	DecisionType string    `gorm:"type:varchar(80);index;not null" json:"decisionType"`
	Decision     string    `gorm:"type:varchar(120);index;not null" json:"decision"`
	Reason       string    `gorm:"type:text" json:"reason,omitempty"`
	RuleApplied  string    `gorm:"type:text" json:"ruleApplied,omitempty"`
	Approved     bool      `gorm:"index" json:"approved"`
	Actor        string    `gorm:"type:varchar(120)" json:"actor,omitempty"`
	CreatedAt    time.Time `json:"createdAt"`
}

type WorkflowEvent struct {
	ID          uuid.UUID `gorm:"type:uuid;primary_key;default:uuid_generate_v4()" json:"id,omitempty"`
	WorkflowID  uuid.UUID `gorm:"type:uuid;index;not null" json:"workflowId"`
	EventType   string    `gorm:"type:varchar(80);index;not null" json:"eventType"`
	FromState   string    `gorm:"type:varchar(80);index" json:"fromState,omitempty"`
	ToState     string    `gorm:"type:varchar(80);index" json:"toState,omitempty"`
	Message     string    `gorm:"type:text" json:"message,omitempty"`
	Trigger     string    `gorm:"type:varchar(255)" json:"trigger,omitempty"`
	RuleApplied string    `gorm:"type:text" json:"ruleApplied,omitempty"`
	SourceURI   string    `gorm:"type:varchar(1024)" json:"sourceUri,omitempty"`
	Actor       string    `gorm:"type:varchar(120)" json:"actor,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
}

// WorkflowCompletionAttestation is the immutable evidence produced when a
// claimed workflow is accepted as complete. WorkflowItem remains a mutable
// projection; accounting and downstream effects must rely on this record.
type WorkflowCompletionAttestation struct {
	ID                    uuid.UUID `gorm:"type:uuid;primary_key;default:uuid_generate_v4()" json:"id,omitempty"`
	// The versioned migration owns the unique workflow_id constraint. Do not add
	// a generated GORM unique index here: DB_AUTOMIGRATE would otherwise try to
	// rename or drop a constraint that the migration deliberately names itself.
	WorkflowID            uuid.UUID `gorm:"type:uuid;not null" json:"workflowId"`
	OwnerIdentity         string    `gorm:"type:varchar(255);index;not null" json:"-"`
	TaskPlanID            string    `gorm:"type:varchar(120);not null" json:"taskPlanId"`
	CompletionStatus      string    `gorm:"type:varchar(40);not null" json:"completionStatus"`
	VerificationStatus    string    `gorm:"type:varchar(80);index;not null" json:"verificationStatus"`
	RuntimeID             string    `gorm:"type:varchar(256);not null;default:''" json:"runtimeId,omitempty"`
	RuntimeEvidenceURI    string    `gorm:"type:varchar(2048);not null;default:''" json:"runtimeEvidenceUri,omitempty"`
	RuntimeEvidenceDigest string    `gorm:"type:char(64);not null" json:"runtimeEvidenceDigest"`
	ResultDigest          string    `gorm:"type:char(64);not null" json:"resultDigest"`
	RecordDigest          string    `gorm:"type:char(64);not null" json:"recordDigest"`
	CompletedAt           time.Time `gorm:"not null;index" json:"completedAt"`
	CreatedAt             time.Time `gorm:"column:attested_at;not null;index;autoCreateTime" json:"createdAt"`
}

func (WorkflowCompletionAttestation) TableName() string {
	return "workflow_completion_attestations"
}
