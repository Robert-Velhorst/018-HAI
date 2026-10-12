// Package agentcoordination defines transport-neutral contracts for safe,
// traceable coordination between HAI agents. It does not execute delegated
// work, grant authority, or satisfy human approval requirements.
package agentcoordination

import (
	"context"
	"encoding/json"
	"time"
)

type MessageType string

const (
	MessageTypeRequest        MessageType = "request"
	MessageTypeProposal       MessageType = "proposal"
	MessageTypeEvidence       MessageType = "evidence"
	MessageTypeStatus         MessageType = "status"
	MessageTypeDecision       MessageType = "decision"
	MessageTypeAcknowledgment MessageType = "acknowledgment"
	MessageTypeEscalation     MessageType = "escalation"
	MessageTypeConflict       MessageType = "conflict"
)

type Confidentiality string

const (
	ConfidentialityInternal   Confidentiality = "internal"
	ConfidentialityRestricted Confidentiality = "restricted"
)

type AgentRef struct {
	ID               string `json:"id"`
	Role             string `json:"role"`
	AuthorityCeiling int    `json:"authorityCeiling"`
}

type MessagePayload struct {
	Schema  string          `json:"schema"`
	Subject string          `json:"subject"`
	Data    json.RawMessage `json:"data"`
}

// Message is a typed coordination envelope. AuthorityLevel describes the
// maximum authority needed to handle the message; receiving it never increases
// the recipient's authority.
type Message struct {
	ID                string          `json:"id"`
	IdempotencyKey    string          `json:"idempotencyKey"`
	CorrelationID     string          `json:"correlationId"`
	CausationID       string          `json:"causationId,omitempty"`
	SchemaVersion     string          `json:"schemaVersion"`
	Type              MessageType     `json:"type"`
	Sender            AgentRef        `json:"sender"`
	Recipient         AgentRef        `json:"recipient"`
	Confidentiality   Confidentiality `json:"confidentiality"`
	AuthorityLevel    int             `json:"authorityLevel"`
	Payload           MessagePayload  `json:"payload"`
	PayloadDigest     string          `json:"payloadDigest"`
	EvidenceRefs      []string        `json:"evidenceRefs,omitempty"`
	RequiresAck       bool            `json:"requiresAck"`
	CreatedAt         time.Time       `json:"createdAt"`
	ExpiresAt         time.Time       `json:"expiresAt"`
	HumanApprovalRef  string          `json:"humanApprovalRef,omitempty"`
	ProvenanceSummary string          `json:"provenanceSummary"`
}

type ExecutionMode string

const (
	ExecutionModePlanOnly       ExecutionMode = "plan_only"
	ExecutionModeRecommend      ExecutionMode = "recommend"
	ExecutionModeDraft          ExecutionMode = "draft"
	ExecutionModeExecuteLowRisk ExecutionMode = "execute_low_risk"
)

type ApprovalMode string

const (
	ApprovalNotRequired     ApprovalMode = "not_required"
	ApprovalBeforeExecution ApprovalMode = "approval_before_execution"
)

type ResourceAccess string

const (
	ResourceRead  ResourceAccess = "read"
	ResourceWrite ResourceAccess = "write"
)

type ResourceClaim struct {
	Resource  string         `json:"resource"`
	Access    ResourceAccess `json:"access"`
	Exclusive bool           `json:"exclusive"`
}

type DelegationStatus string

const (
	DelegationProposed   DelegationStatus = "proposed"
	DelegationAccepted   DelegationStatus = "accepted"
	DelegationRejected   DelegationStatus = "rejected"
	DelegationInProgress DelegationStatus = "in_progress"
	DelegationBlocked    DelegationStatus = "blocked"
	DelegationCompleted  DelegationStatus = "completed"
	DelegationCancelled  DelegationStatus = "cancelled"
	DelegationExpired    DelegationStatus = "expired"
	DelegationEscalated  DelegationStatus = "escalated"
)

// DelegationEnvelope bounds a task assignment. It cannot delegate approval
// authority and cannot authorize work above either agent's authority ceiling.
type DelegationEnvelope struct {
	ID                    string            `json:"id"`
	TaskID                string            `json:"taskId"`
	IdempotencyKey        string            `json:"idempotencyKey"`
	CorrelationID         string            `json:"correlationId"`
	Principal             AgentRef          `json:"principal"`
	Delegate              AgentRef          `json:"delegate"`
	Objective             string            `json:"objective"`
	SuccessCriteria       []string          `json:"successCriteria"`
	StopConditions        []string          `json:"stopConditions"`
	AllowedTools          []string          `json:"allowedTools,omitempty"`
	ProhibitedActions     []string          `json:"prohibitedActions"`
	ResourceClaims        []ResourceClaim   `json:"resourceClaims,omitempty"`
	ExecutionMode         ExecutionMode     `json:"executionMode"`
	ApprovalMode          ApprovalMode      `json:"approvalMode"`
	RequiredAuthority     int               `json:"requiredAuthority"`
	HumanApprovalRef      string            `json:"humanApprovalRef,omitempty"`
	EvidenceRefs          []string          `json:"evidenceRefs,omitempty"`
	Status                DelegationStatus  `json:"status"`
	CreatedAt             time.Time         `json:"createdAt"`
	DueAt                 time.Time         `json:"dueAt"`
	UpdatedAt             time.Time         `json:"updatedAt"`
	StatusReason          string            `json:"statusReason,omitempty"`
	CompletionEvidence    []string          `json:"completionEvidence,omitempty"`
	DelegationTransitions []DelegationEvent `json:"transitions,omitempty"`
}

type DelegationEvent struct {
	From      DelegationStatus `json:"from"`
	To        DelegationStatus `json:"to"`
	ActorID   string           `json:"actorId"`
	Reason    string           `json:"reason"`
	CreatedAt time.Time        `json:"createdAt"`
}

type AcknowledgmentStatus string

const (
	AcknowledgmentAccepted AcknowledgmentStatus = "accepted"
	AcknowledgmentRejected AcknowledgmentStatus = "rejected"
	AcknowledgmentDeferred AcknowledgmentStatus = "deferred"
)

type Acknowledgment struct {
	ID             string               `json:"id"`
	MessageID      string               `json:"messageId"`
	CorrelationID  string               `json:"correlationId"`
	RecipientID    string               `json:"recipientId"`
	Status         AcknowledgmentStatus `json:"status"`
	Reason         string               `json:"reason,omitempty"`
	CreatedAt      time.Time            `json:"createdAt"`
	RetryAfter     *time.Time           `json:"retryAfter,omitempty"`
	IdempotencyKey string               `json:"idempotencyKey"`
}

type DeliveryReceipt struct {
	MessageID     string    `json:"messageId"`
	CorrelationID string    `json:"correlationId"`
	TransportID   string    `json:"transportId"`
	AcceptedAt    time.Time `json:"acceptedAt"`
	Duplicate     bool      `json:"duplicate"`
}

// Transport accepts validated envelopes only. Implementations must not infer
// permission to execute a task from successful delivery. They should wrap
// ErrDeliveryNotAccepted only when non-acceptance is certain; other errors are
// treated as ambiguous and block automatic replay.
type Transport interface {
	Deliver(ctx context.Context, message Message) (DeliveryReceipt, error)
}

type DispatchClaimStatus string

const (
	DispatchClaimAcquired  DispatchClaimStatus = "acquired"
	DispatchClaimDuplicate DispatchClaimStatus = "duplicate"
	DispatchClaimConflict  DispatchClaimStatus = "conflict"
)

type DispatchClaim struct {
	Status  DispatchClaimStatus
	Receipt *DeliveryReceipt
}

// DispatchStore provides durable idempotency at the integration boundary.
// Begin must distinguish a retry of identical content from reuse of a key for
// different content. Abandon permits a retry after transport failure.
type DispatchStore interface {
	Begin(ctx context.Context, key, digest string, expiresAt time.Time) (DispatchClaim, error)
	Complete(ctx context.Context, key, digest string, receipt DeliveryReceipt) error
	Abandon(ctx context.Context, key, digest string) error
}
