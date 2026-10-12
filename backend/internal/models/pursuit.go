package models

import (
	"time"

	"github.com/google/uuid"
)

type Pursuit struct {
	ID                    uuid.UUID                 `gorm:"type:uuid;primary_key;default:uuid_generate_v4()" json:"id,omitempty"`
	OwnerIdentity         string                    `gorm:"type:varchar(255);index" json:"ownerIdentity,omitempty"`
	Title                 string                    `gorm:"type:varchar(512);index;not null" json:"title"`
	Description           string                    `gorm:"type:text" json:"description,omitempty"`
	WhyItMatters          string                    `gorm:"type:text" json:"whyItMatters,omitempty"`
	ProjectKey            string                    `gorm:"type:varchar(255);index" json:"projectKey,omitempty"`
	MandateID             *uuid.UUID                `gorm:"type:uuid;index" json:"mandateId,omitempty"`
	Domain                string                    `gorm:"type:varchar(120);index" json:"domain,omitempty"`
	DesiredOutcome        string                    `gorm:"type:text" json:"desiredOutcome,omitempty"`
	CurrentStateSummary   string                    `gorm:"type:text" json:"currentStateSummary,omitempty"`
	Status                string                    `gorm:"type:varchar(80);index;not null" json:"status"`
	PriorityScore         int                       `gorm:"index" json:"priorityScore"`
	RiskLevel             string                    `gorm:"type:varchar(80);index" json:"riskLevel"`
	Confidence            float64                   `json:"confidence"`
	AutonomyLevel         string                    `gorm:"type:varchar(80);index" json:"autonomyLevel"`
	NeedCategory          string                    `gorm:"type:varchar(120);index" json:"needCategory,omitempty"`
	SourceOfCreation      string                    `gorm:"type:varchar(120);index" json:"sourceOfCreation,omitempty"`
	NextRecommendedAction string                    `gorm:"type:text" json:"nextRecommendedAction,omitempty"`
	CompletionDefinition  string                    `gorm:"type:text" json:"completionDefinition,omitempty"`
	SuccessCriteria       []PursuitSuccessCriterion `gorm:"type:jsonb;serializer:json;not null;default:'[]'" json:"successCriteria"`
	StopConditions        []PursuitStopCondition    `gorm:"type:jsonb;serializer:json;not null;default:'[]'" json:"stopConditions"`
	Dependencies          []PursuitDependency       `gorm:"type:jsonb;serializer:json;not null;default:'[]'" json:"dependencies"`
	ResourceLimits        PursuitResourceLimits     `gorm:"type:jsonb;serializer:json;not null;default:'{}'" json:"resourceLimits"`
	CompletionState       string                    `gorm:"type:varchar(80);index" json:"completionState"`
	LastActivityAt        *time.Time                `gorm:"index" json:"lastActivityAt,omitempty"`
	NextReviewAt          *time.Time                `gorm:"index" json:"nextReviewAt,omitempty"`
	TargetAt              *time.Time                `gorm:"index" json:"targetAt,omitempty"`
	ReviewCadenceDays     int                       `gorm:"not null;default:0" json:"reviewCadenceDays"`
	Archived              bool                      `gorm:"default:false;index" json:"archived"`
	CreatedAt             time.Time                 `json:"createdAt"`
	UpdatedAt             time.Time                 `json:"updatedAt"`
}

// PursuitSuccessCriterion is an operator-reviewable acceptance test for a
// pursuit. Evidence links remain references; source content stays in the
// source and verification stores.
type PursuitSuccessCriterion struct {
	ID                 string `json:"id"`
	Description        string `json:"description"`
	Status             string `json:"status"`
	EvidenceRequired   bool   `json:"evidenceRequired"`
	EvidenceURI        string `json:"evidenceUri,omitempty"`
	VerificationStatus string `json:"verificationStatus,omitempty"`
	WaiverReason       string `json:"waiverReason,omitempty"`
}

// PursuitStopCondition defines when HAI must stop spending resources and ask
// for review. Triggered conditions block further planning until resolved.
type PursuitStopCondition struct {
	ID          string     `json:"id"`
	Description string     `json:"description"`
	Status      string     `json:"status"`
	Reason      string     `json:"reason,omitempty"`
	TriggeredAt *time.Time `json:"triggeredAt,omitempty"`
	ResolvedAt  *time.Time `json:"resolvedAt,omitempty"`
}

// PursuitDependency captures a prerequisite without inventing a second task
// graph. RelatedPursuitID is optional because external people, records, and
// deliveries can also be prerequisites.
type PursuitDependency struct {
	ID               string     `json:"id"`
	Label            string     `json:"label"`
	Status           string     `json:"status"`
	Owner            string     `json:"owner,omitempty"`
	RelatedPursuitID string     `json:"relatedPursuitId,omitempty"`
	DueAt            *time.Time `json:"dueAt,omitempty"`
	EvidenceURI      string     `json:"evidenceUri,omitempty"`
	Reason           string     `json:"reason,omitempty"`
}

// PursuitResourceLimits are ceilings, not targets. Zero means unspecified;
// paid execution remains governed independently by the global zero-budget
// policy and approval boundary.
type PursuitResourceLimits struct {
	MaxEffortHours       float64 `json:"maxEffortHours,omitempty"`
	MaxSpendEUR          float64 `json:"maxSpendEur,omitempty"`
	MaxParallelWorkflows int     `json:"maxParallelWorkflows,omitempty"`
	Notes                string  `json:"notes,omitempty"`
}

// PursuitResourceEvent is an immutable accounting record. Corrections and
// refunds are appended as new events so the original evidence remains
// auditable. Integer minutes and euro cents avoid floating-point decisions at
// the execution boundary.
type PursuitResourceEvent struct {
	ID             uuid.UUID `gorm:"type:uuid;primary_key;default:uuid_generate_v4()" json:"id,omitempty"`
	PursuitID      uuid.UUID `gorm:"type:uuid;index;not null" json:"pursuitId"`
	OwnerIdentity  string    `gorm:"type:varchar(255);index;not null" json:"-"`
	Kind           string    `gorm:"type:varchar(40);index;not null" json:"kind"`
	EffortMinutes  int64     `gorm:"not null;default:0" json:"effortMinutes"`
	AmountMinor    int64     `gorm:"not null;default:0" json:"amountMinor"`
	Currency       string    `gorm:"type:varchar(3);not null;default:''" json:"currency,omitempty"`
	Note           string    `gorm:"type:text" json:"note,omitempty"`
	EvidenceURI    string    `gorm:"type:varchar(2048)" json:"evidenceUri,omitempty"`
	Actor          string    `gorm:"type:varchar(255);not null" json:"actor"`
	IdempotencyKey string    `gorm:"type:varchar(120);not null" json:"idempotencyKey"`
	RecordDigest   string    `gorm:"type:char(64);not null" json:"recordDigest"`
	OccurredAt     time.Time `gorm:"not null;index" json:"occurredAt"`
	RecordedAt     time.Time `gorm:"not null;index;autoCreateTime" json:"recordedAt"`
}

func (PursuitResourceEvent) TableName() string { return "pursuit_resource_events" }

// PursuitResourceReservation is an immutable capacity hold created immediately
// before pursuit-scoped execution. A separate immutable settlement consumes or
// releases the hold; active holds therefore remain visible to concurrent work.
type PursuitResourceReservation struct {
	ID                     uuid.UUID `gorm:"type:uuid;primary_key;default:uuid_generate_v4()" json:"id,omitempty"`
	PursuitID              uuid.UUID `gorm:"type:uuid;index;not null" json:"pursuitId"`
	OwnerIdentity          string    `gorm:"type:varchar(255);index;not null" json:"-"`
	OperationID            string    `gorm:"type:varchar(160);not null" json:"operationId"`
	EstimatedEffortMinutes int64     `gorm:"not null;default:0" json:"estimatedEffortMinutes"`
	EstimatedCostMicros    int64     `gorm:"not null;default:0" json:"estimatedCostMicros"`
	Currency               string    `gorm:"type:varchar(3);not null;default:''" json:"currency,omitempty"`
	Reason                 string    `gorm:"type:text;not null;default:''" json:"reason,omitempty"`
	Actor                  string    `gorm:"type:varchar(255);not null" json:"actor"`
	RecordDigest           string    `gorm:"type:char(64);not null" json:"recordDigest"`
	ReservedAt             time.Time `gorm:"not null;index;autoCreateTime" json:"reservedAt"`
}

func (PursuitResourceReservation) TableName() string { return "pursuit_resource_reservations" }

// PursuitResourceReservationSettlement closes exactly one reservation. Actual
// usage is also appended to PursuitResourceEvent in the same transaction.
type PursuitResourceReservationSettlement struct {
	ID                  uuid.UUID `gorm:"type:uuid;primary_key;default:uuid_generate_v4()" json:"id,omitempty"`
	ReservationID       uuid.UUID `gorm:"type:uuid;uniqueIndex;not null" json:"reservationId"`
	PursuitID           uuid.UUID `gorm:"type:uuid;index;not null" json:"pursuitId"`
	OwnerIdentity       string    `gorm:"type:varchar(255);index;not null" json:"-"`
	Disposition         string    `gorm:"type:varchar(20);not null" json:"disposition"`
	ActualEffortMinutes int64     `gorm:"not null;default:0" json:"actualEffortMinutes"`
	ActualCostMicros    int64     `gorm:"not null;default:0" json:"actualCostMicros"`
	Currency            string    `gorm:"type:varchar(3);not null;default:''" json:"currency,omitempty"`
	EvidenceURI         string    `gorm:"type:varchar(2048);not null;default:''" json:"evidenceUri,omitempty"`
	Reason              string    `gorm:"type:text;not null;default:''" json:"reason,omitempty"`
	Actor               string    `gorm:"type:varchar(255);not null" json:"actor"`
	RecordDigest        string    `gorm:"type:char(64);not null" json:"recordDigest"`
	SettledAt           time.Time `gorm:"not null;index;autoCreateTime" json:"settledAt"`
}

func (PursuitResourceReservationSettlement) TableName() string {
	return "pursuit_resource_reservation_settlements"
}

// PursuitPortfolioWorkflowSettlementProof binds resource accounting to the
// exact immutable approval, authorization consumption, workflow completion,
// and measured usage that justified it. It grants no execution authority.
type PursuitPortfolioWorkflowSettlementProof struct {
	ID                          uuid.UUID `gorm:"type:uuid;primary_key;default:uuid_generate_v4()" json:"id,omitempty"`
	// The immutable proof ledger and its uniqueness constraints are owned by
	// pre/0044_workflow_completion_settlement_proofs. Keep generated GORM
	// unique indexes out of this model so an opt-in development AutoMigrate can
	// never rewrite the migration-owned schema.
	SettlementID                uuid.UUID `gorm:"type:uuid;not null" json:"settlementId"`
	SettlementDigest            string    `gorm:"type:char(64);not null" json:"settlementDigest"`
	ReservationID               uuid.UUID `gorm:"type:uuid;not null" json:"reservationId"`
	PursuitID                   uuid.UUID `gorm:"type:uuid;index;not null" json:"pursuitId"`
	OwnerIdentity               string    `gorm:"type:varchar(255);index;not null" json:"-"`
	ProposalItemID              uuid.UUID `gorm:"type:uuid;not null" json:"proposalItemId"`
	ProposalItemDigest          string    `gorm:"type:char(64);not null" json:"proposalItemDigest"`
	ApprovalDecisionID          uuid.UUID `gorm:"type:uuid;not null" json:"approvalDecisionId"`
	ApprovalDecisionDigest      string    `gorm:"type:char(64);not null" json:"approvalDecisionDigest"`
	AuthorizationReceiptID      uuid.UUID `gorm:"type:uuid;not null" json:"authorizationReceiptId"`
	AuthorizationReceiptDigest  string    `gorm:"type:char(64);not null" json:"authorizationReceiptDigest"`
	AuthorizationConsumptionKey string    `gorm:"column:authorization_consumption_digest;type:char(64);not null" json:"authorizationConsumptionDigest"`
	AuthorizationTarget         string    `gorm:"column:authorization_consumption_target;type:varchar(1024);not null" json:"authorizationTarget"`
	WorkflowID                  uuid.UUID `gorm:"type:uuid;not null" json:"workflowId"`
	CompletionAttestationID     uuid.UUID `gorm:"type:uuid;not null" json:"completionAttestationId"`
	CompletionAttestationDigest string    `gorm:"type:char(64);not null" json:"completionAttestationDigest"`
	ActualEffortMinutes         int64     `gorm:"not null;default:0" json:"actualEffortMinutes"`
	ActualCostMicros            int64     `gorm:"not null;default:0" json:"actualCostMicros"`
	Currency                    string    `gorm:"type:varchar(3);not null;default:''" json:"currency,omitempty"`
	RequestDigest               string    `gorm:"type:char(64);not null" json:"requestDigest"`
	RecordDigest                string    `gorm:"type:char(64);not null" json:"recordDigest"`
	Actor                       string    `gorm:"type:varchar(255);not null" json:"actor"`
	CreatedAt                   time.Time `gorm:"column:settled_at;not null;index" json:"createdAt"`
}

func (PursuitPortfolioWorkflowSettlementProof) TableName() string {
	return "pursuit_portfolio_workflow_settlement_proofs"
}

type PursuitLink struct {
	ID           uuid.UUID `gorm:"type:uuid;primary_key;default:uuid_generate_v4()" json:"id,omitempty"`
	PursuitID    uuid.UUID `gorm:"type:uuid;index;not null;uniqueIndex:pursuit_link_unique" json:"pursuitId"`
	LinkType     string    `gorm:"type:varchar(80);index;not null;uniqueIndex:pursuit_link_unique" json:"linkType"`
	LinkID       string    `gorm:"type:varchar(120);index;not null;uniqueIndex:pursuit_link_unique" json:"linkId"`
	Relationship string    `gorm:"type:varchar(80);index;not null;uniqueIndex:pursuit_link_unique" json:"relationship"`
	SourceURI    string    `gorm:"type:varchar(1024);index" json:"sourceUri,omitempty"`
	SourceLabel  string    `gorm:"type:varchar(512)" json:"sourceLabel,omitempty"`
	Confidence   float64   `json:"confidence"`
	CreatedAt    time.Time `json:"createdAt"`
}

type PursuitActivity struct {
	ID             uuid.UUID `gorm:"type:uuid;primary_key;default:uuid_generate_v4()" json:"id,omitempty"`
	PursuitID      uuid.UUID `gorm:"type:uuid;index;not null" json:"pursuitId"`
	EventType      string    `gorm:"type:varchar(80);index;not null" json:"eventType"`
	Message        string    `gorm:"type:text" json:"message,omitempty"`
	Actor          string    `gorm:"type:varchar(120)" json:"actor,omitempty"`
	SourceType     string    `gorm:"type:varchar(80);index" json:"sourceType,omitempty"`
	SourceID       string    `gorm:"type:varchar(120);index" json:"sourceId,omitempty"`
	SourceURI      string    `gorm:"type:varchar(1024)" json:"sourceUri,omitempty"`
	IdempotencyKey string    `gorm:"type:varchar(160);index" json:"-"`
	CreatedAt      time.Time `json:"createdAt"`
}

// PursuitTaskAttempt is a compact, durable projection of a direct task-engine
// plan or run. It intentionally excludes retrieved source context and model
// output; those remain in their existing protected stores. Workflow-owned task
// runs stay on WorkflowItem and are aggregated separately by the pursuit view.
type PursuitTaskAttempt struct {
	ID                 uuid.UUID  `gorm:"type:uuid;primary_key;default:uuid_generate_v4()" json:"id,omitempty"`
	PursuitID          uuid.UUID  `gorm:"type:uuid;index;not null" json:"pursuitId"`
	TaskPlanID         string     `gorm:"type:varchar(120);uniqueIndex;not null" json:"taskPlanId"`
	OwnerIdentity      string     `gorm:"type:varchar(255);index" json:"ownerIdentity,omitempty"`
	RequestSummary     string     `gorm:"type:text" json:"requestSummary,omitempty"`
	ProjectKey         string     `gorm:"type:varchar(255);index" json:"projectKey,omitempty"`
	Mode               string     `gorm:"type:varchar(40);index;not null" json:"mode"`
	Status             string     `gorm:"type:varchar(80);index;not null" json:"status"`
	RiskLevel          string     `gorm:"type:varchar(80);index" json:"riskLevel,omitempty"`
	VerificationStatus string     `gorm:"type:varchar(80);index" json:"verificationStatus,omitempty"`
	AutomationID       string     `gorm:"type:varchar(120);index" json:"automationId,omitempty"`
	LaunchEventID      string     `gorm:"type:varchar(120);index" json:"launchEventId,omitempty"`
	BlockedReason      string     `gorm:"type:text" json:"blockedReason,omitempty"`
	StartedAt          *time.Time `gorm:"index" json:"startedAt,omitempty"`
	CompletedAt        *time.Time `gorm:"index" json:"completedAt,omitempty"`
	CreatedAt          time.Time  `json:"createdAt"`
	UpdatedAt          time.Time  `json:"updatedAt"`
}

// PursuitPortfolioAllocation is the immutable owner acceptance of one
// deterministic portfolio-planning decision. It records acceptance only; it
// does not consume approvals or grant execution authority.
type PursuitPortfolioAllocation struct {
	ID                       uuid.UUID  `gorm:"type:uuid;primary_key;default:uuid_generate_v4()" json:"id,omitempty"`
	OwnerIdentity            string     `gorm:"type:varchar(255);not null;uniqueIndex:pursuit_portfolio_allocation_owner_plan" json:"-"`
	PlanID                   string     `gorm:"type:varchar(96);not null;uniqueIndex:pursuit_portfolio_allocation_owner_plan" json:"planId"`
	RequestDigest            string     `gorm:"type:char(64);not null" json:"requestDigest"`
	DecisionDigest           string     `gorm:"type:char(64);not null" json:"decisionDigest"`
	Status                   string     `gorm:"type:varchar(32);not null;index" json:"status"`
	DurationMode             string     `gorm:"type:varchar(16);not null" json:"durationMode"`
	HorizonStart             time.Time  `gorm:"not null" json:"horizonStart"`
	HorizonEnd               time.Time  `gorm:"not null" json:"horizonEnd"`
	Actor                    string     `gorm:"type:varchar(255);not null" json:"actor"`
	Confirmation             string     `gorm:"type:varchar(255);not null" json:"confirmation"`
	RecordDigest             string     `gorm:"type:char(64);not null" json:"recordDigest"`
	CoordinationPlanID       *uuid.UUID `gorm:"type:uuid;index" json:"coordinationPlanId,omitempty"`
	CoordinationPlanRevision uint64     `gorm:"type:bigint;not null;default:0" json:"coordinationPlanRevision,omitempty"`
	CoordinationPlanDigest   string     `gorm:"type:char(64);not null;default:''" json:"coordinationPlanDigest,omitempty"`
	CoordinationPlanNodeID   string     `gorm:"type:varchar(160);not null;default:''" json:"coordinationPlanNodeId,omitempty"`
	AcceptedAt               time.Time  `gorm:"not null;index" json:"acceptedAt"`
}

func (PursuitPortfolioAllocation) TableName() string {
	return "pursuit_portfolio_allocations"
}

// PursuitPortfolioAllocationItem binds an accepted schedule entry to its
// immutable resource hold. Approval flags remain informational gates and do
// not themselves satisfy or consume an approval.
type PursuitPortfolioAllocationItem struct {
	ID                  uuid.UUID `gorm:"type:uuid;primary_key;default:uuid_generate_v4()" json:"id,omitempty"`
	AllocationID        uuid.UUID `gorm:"type:uuid;not null;uniqueIndex:pursuit_portfolio_allocation_item_pursuit" json:"allocationId"`
	PursuitID           uuid.UUID `gorm:"type:uuid;not null;uniqueIndex:pursuit_portfolio_allocation_item_pursuit;index" json:"pursuitId"`
	OwnerIdentity       string    `gorm:"type:varchar(255);not null;index" json:"-"`
	ScheduledStart      time.Time `gorm:"not null" json:"scheduledStart"`
	ScheduledEnd        time.Time `gorm:"not null" json:"scheduledEnd"`
	DurationMinutes     int64     `gorm:"not null" json:"durationMinutes"`
	EstimatedCostMicros int64     `gorm:"not null;default:0" json:"estimatedCostMicros"`
	RequiresApproval    bool      `gorm:"not null;default:false" json:"requiresApproval"`
	ApprovalReasons     []string  `gorm:"type:jsonb;serializer:json;not null;default:'[]'" json:"approvalReasons"`
	ReservationID       uuid.UUID `gorm:"type:uuid;not null;uniqueIndex" json:"reservationId"`
	RecordDigest        string    `gorm:"type:char(64);not null" json:"recordDigest"`
	CreatedAt           time.Time `gorm:"not null;index" json:"createdAt"`
}

func (PursuitPortfolioAllocationItem) TableName() string {
	return "pursuit_portfolio_allocation_items"
}

// PursuitPortfolioExecutionProposal is an immutable snapshot of the work HAI
// could prepare from an accepted allocation. It is planning evidence only and
// never represents an approval, queue entry, or execution authorization.
type PursuitPortfolioExecutionProposal struct {
	ID                     uuid.UUID `gorm:"type:uuid;primary_key;default:uuid_generate_v4()" json:"id,omitempty"`
	AllocationID           uuid.UUID `gorm:"type:uuid;not null;uniqueIndex:pursuit_portfolio_execution_proposal_snapshot" json:"allocationId"`
	OwnerIdentity          string    `gorm:"type:varchar(255);not null;uniqueIndex:pursuit_portfolio_execution_proposal_snapshot" json:"-"`
	AllocationRecordDigest string    `gorm:"type:char(64);not null" json:"allocationRecordDigest"`
	SnapshotDigest         string    `gorm:"type:char(64);not null;uniqueIndex:pursuit_portfolio_execution_proposal_snapshot" json:"snapshotDigest"`
	Status                 string    `gorm:"type:varchar(40);not null;index" json:"status"`
	Actor                  string    `gorm:"type:varchar(255);not null" json:"actor"`
	Confirmation           string    `gorm:"type:varchar(255);not null" json:"confirmation"`
	Authority              string    `gorm:"type:varchar(32);not null" json:"authority"`
	RecordDigest           string    `gorm:"type:char(64);not null" json:"recordDigest"`
	PreparedAt             time.Time `gorm:"not null;index" json:"preparedAt"`
}

func (PursuitPortfolioExecutionProposal) TableName() string {
	return "pursuit_portfolio_execution_proposals"
}

// PursuitPortfolioExecutionProposalItem preserves the exact pursuit and
// policy state used to classify one proposed action. Approval and blocked
// reasons are evidence for later gates; they do not satisfy those gates.
type PursuitPortfolioExecutionProposalItem struct {
	ID                   uuid.UUID `gorm:"type:uuid;primary_key;default:uuid_generate_v4()" json:"id,omitempty"`
	ProposalID           uuid.UUID `gorm:"type:uuid;not null;uniqueIndex:pursuit_portfolio_execution_proposal_item_allocation_item" json:"proposalId"`
	AllocationItemID     uuid.UUID `gorm:"type:uuid;not null;uniqueIndex:pursuit_portfolio_execution_proposal_item_allocation_item" json:"allocationItemId"`
	PursuitID            uuid.UUID `gorm:"type:uuid;not null;index" json:"pursuitId"`
	ReservationID        uuid.UUID `gorm:"type:uuid;not null;index" json:"reservationId"`
	OwnerIdentity        string    `gorm:"type:varchar(255);not null;index" json:"-"`
	ActionSummary        string    `gorm:"type:text;not null" json:"actionSummary"`
	PursuitStatus        string    `gorm:"type:varchar(80);not null" json:"pursuitStatus"`
	RiskLevel            string    `gorm:"type:varchar(80);not null" json:"riskLevel"`
	AutonomyLevel        string    `gorm:"type:varchar(80);not null" json:"autonomyLevel"`
	Status               string    `gorm:"type:varchar(32);not null;index" json:"status"`
	RequiresApproval     bool      `gorm:"not null;default:false" json:"requiresApproval"`
	ApprovalReasons      []string  `gorm:"type:jsonb;serializer:json;not null;default:'[]'" json:"approvalReasons"`
	BlockedReasons       []string  `gorm:"type:jsonb;serializer:json;not null;default:'[]'" json:"blockedReasons"`
	AllocationItemDigest string    `gorm:"type:char(64);not null" json:"allocationItemDigest"`
	StateDigest          string    `gorm:"type:char(64);not null" json:"stateDigest"`
	RecordDigest         string    `gorm:"type:char(64);not null" json:"recordDigest"`
	PreparedAt           time.Time `gorm:"not null;index" json:"preparedAt"`
}

func (PursuitPortfolioExecutionProposalItem) TableName() string {
	return "pursuit_portfolio_execution_proposal_items"
}

// PursuitPortfolioExecutionProposalDecision is an append-only owner decision
// about one immutable proposal item. It is evidence for the later concrete
// effect authorization boundary; it never grants execution authority itself.
type PursuitPortfolioExecutionProposalDecision struct {
	ID                 uuid.UUID  `gorm:"type:uuid;primary_key;default:uuid_generate_v4()" json:"id,omitempty"`
	ProposalItemID     uuid.UUID  `gorm:"type:uuid;not null;index" json:"proposalItemId"`
	ProposalID         uuid.UUID  `gorm:"type:uuid;not null;index" json:"proposalId"`
	PursuitID          uuid.UUID  `gorm:"type:uuid;not null;index" json:"pursuitId"`
	OwnerIdentity      string     `gorm:"type:varchar(255);not null;index" json:"-"`
	Decision           string     `gorm:"type:varchar(40);not null;index" json:"decision"`
	Reason             string     `gorm:"type:text;not null" json:"reason"`
	Actor              string     `gorm:"type:varchar(255);not null" json:"actor"`
	Confirmation       string     `gorm:"type:varchar(255);not null" json:"confirmation"`
	ProposalItemDigest string     `gorm:"type:char(64);not null" json:"proposalItemDigest"`
	StateDigest        string     `gorm:"type:char(64);not null" json:"stateDigest"`
	Authority          string     `gorm:"type:varchar(40);not null" json:"authority"`
	RequestDigest      string     `gorm:"type:char(64);not null" json:"requestDigest"`
	RecordDigest       string     `gorm:"type:char(64);not null" json:"recordDigest"`
	PreviousDecisionID *uuid.UUID `gorm:"type:uuid" json:"previousDecisionId,omitempty"`
	DecidedAt          time.Time  `gorm:"not null;index" json:"decidedAt"`
	ExpiresAt          *time.Time `json:"expiresAt,omitempty"`
}

func (PursuitPortfolioExecutionProposalDecision) TableName() string {
	return "pursuit_portfolio_execution_proposal_decisions"
}

// PursuitPortfolioDispatchRun is the immutable request envelope for one
// explicitly selected, owner-confirmed portfolio dispatch. It records intent
// only; authority remains in the per-item approval and execution receipts.
type PursuitPortfolioDispatchRun struct {
	ID                  uuid.UUID   `gorm:"type:uuid;primary_key;default:uuid_generate_v4()" json:"id,omitempty"`
	ProposalID          uuid.UUID   `gorm:"type:uuid;not null;index" json:"proposalId"`
	OwnerIdentity       string      `gorm:"type:varchar(255);not null;uniqueIndex:pursuit_portfolio_dispatch_owner_request" json:"-"`
	ProposalDigest      string      `gorm:"type:char(64);not null" json:"proposalDigest"`
	SelectedItemIDs     []uuid.UUID `gorm:"type:jsonb;serializer:json;not null;default:'[]'" json:"selectedItemIds"`
	SelectedItemsDigest string      `gorm:"type:char(64);not null" json:"selectedItemsDigest"`
	RequestDigest       string      `gorm:"type:char(64);not null;uniqueIndex:pursuit_portfolio_dispatch_owner_request" json:"requestDigest"`
	Actor               string      `gorm:"type:varchar(255);not null" json:"actor"`
	Confirmation        string      `gorm:"type:varchar(255);not null" json:"confirmation"`
	RecordDigest        string      `gorm:"type:char(64);not null" json:"recordDigest"`
	RequestedAt         time.Time   `gorm:"not null;index" json:"requestedAt"`
}

func (PursuitPortfolioDispatchRun) TableName() string {
	return "pursuit_portfolio_dispatch_runs"
}

// PursuitPortfolioDispatchItemResult is an append-only observation of one
// item attempt. A failed attempt may be followed by another attempt, while a
// created/replayed workflow remains terminal for that exact dispatch run.
type PursuitPortfolioDispatchItemResult struct {
	ID                     uuid.UUID  `gorm:"type:uuid;primary_key;default:uuid_generate_v4()" json:"id,omitempty"`
	DispatchRunID          uuid.UUID  `gorm:"type:uuid;not null;uniqueIndex:pursuit_portfolio_dispatch_item_attempt" json:"dispatchRunId"`
	ProposalID             uuid.UUID  `gorm:"type:uuid;not null;index" json:"proposalId"`
	ProposalItemID         uuid.UUID  `gorm:"type:uuid;not null;uniqueIndex:pursuit_portfolio_dispatch_item_attempt;index" json:"proposalItemId"`
	OwnerIdentity          string     `gorm:"type:varchar(255);not null;index" json:"-"`
	AttemptNumber          int        `gorm:"not null;uniqueIndex:pursuit_portfolio_dispatch_item_attempt" json:"attemptNumber"`
	ProposalItemDigest     string     `gorm:"type:char(64);not null" json:"proposalItemDigest"`
	ApprovalDecisionID     *uuid.UUID `gorm:"type:uuid" json:"approvalDecisionId,omitempty"`
	ApprovalDecisionDigest string     `gorm:"type:char(64)" json:"approvalDecisionDigest,omitempty"`
	Outcome                string     `gorm:"type:varchar(40);not null;index" json:"outcome"`
	Message                string     `gorm:"type:text;not null" json:"message"`
	AuthorizationReceiptID *uuid.UUID `gorm:"type:uuid" json:"authorizationReceiptId,omitempty"`
	WorkflowID             *uuid.UUID `gorm:"type:uuid" json:"workflowId,omitempty"`
	WorkflowState          string     `gorm:"type:varchar(80)" json:"workflowState,omitempty"`
	Replayed               bool       `gorm:"not null;default:false" json:"replayed"`
	RecordDigest           string     `gorm:"type:char(64);not null" json:"recordDigest"`
	AttemptedAt            time.Time  `gorm:"not null;index" json:"attemptedAt"`
}

func (PursuitPortfolioDispatchItemResult) TableName() string {
	return "pursuit_portfolio_dispatch_item_results"
}
