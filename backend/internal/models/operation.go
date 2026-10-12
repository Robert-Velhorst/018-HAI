package models

import (
	"time"

	"github.com/google/uuid"
)

// Operation is the HAI Phase 2 Operation Ledger root aggregate (§7). Every unit
// of background work links here. JSON columns store canonical JSON strings.
type Operation struct {
	ID          uuid.UUID `gorm:"type:uuid;primary_key;default:uuid_generate_v4()" json:"id,omitempty"`
	OwnerUserID string    `gorm:"type:text;not null;index:idx_operations_owner_workspace_status" json:"ownerUserId"`
	WorkspaceID string    `gorm:"type:text;not null;default:'local';index:idx_operations_owner_workspace_status" json:"workspaceId"`

	Title       string `gorm:"type:text;not null" json:"title"`
	Description string `gorm:"type:text" json:"description,omitempty"`

	SourceType                  string     `gorm:"type:text;not null;index:idx_operations_source" json:"sourceType"`
	SourceID                    *uuid.UUID `gorm:"type:uuid;index:idx_operations_source" json:"sourceId,omitempty"`
	SourceURI                   string     `gorm:"type:text" json:"sourceUri,omitempty"`
	SourceReceivedAt            *time.Time `json:"sourceReceivedAt,omitempty"`
	SourceRevisionHash          string     `gorm:"type:text" json:"sourceRevisionHash,omitempty"`
	SourceProvider              string     `gorm:"type:text;not null;default:''" json:"sourceProvider,omitempty"`
	SourceAccount               string     `gorm:"type:text;not null;default:''" json:"sourceAccount,omitempty"`
	SourceExternalID            string     `gorm:"type:text;not null;default:''" json:"sourceExternalId,omitempty"`
	SourceIdentityHash          string     `gorm:"type:text;not null;default:''" json:"sourceIdentityHash,omitempty"`
	SourceObservationID         *uuid.UUID `gorm:"type:uuid" json:"sourceObservationId,omitempty"`
	SourceObservationGeneration int64      `gorm:"not null;default:0" json:"sourceObservationGeneration,omitempty"`

	ProjectKey    string     `gorm:"type:text;index" json:"projectKey,omitempty"`
	PursuitID     *uuid.UUID `gorm:"type:uuid;index" json:"pursuitId,omitempty"`
	WorkflowID    *uuid.UUID `gorm:"type:uuid;index" json:"workflowId,omitempty"`
	AccountFeedID *uuid.UUID `gorm:"type:uuid;index" json:"accountFeedId,omitempty"`

	OperationType     string     `gorm:"type:text;not null" json:"operationType"`
	Status            string     `gorm:"type:text;not null;index:idx_operations_owner_workspace_status" json:"status"`
	RiskLevel         string     `gorm:"type:text;not null;index" json:"riskLevel"`
	AutonomyLevel     string     `gorm:"type:text;not null" json:"autonomyLevel"`
	OwnerType         string     `gorm:"type:text;not null" json:"ownerType"`
	CurrentDecision   string     `gorm:"type:text;not null;default:'observe_only'" json:"currentDecision"`
	RequiresApproval  bool       `gorm:"not null;default:false;index" json:"requiresApproval"`
	ApprovalID        *uuid.UUID `gorm:"type:uuid" json:"approvalId,omitempty"`
	RecommendedAction string     `gorm:"type:text" json:"recommendedAction,omitempty"`

	EvidenceJSON           string `gorm:"type:jsonb;not null;default:'{}'" json:"evidence"`
	SourceEvidenceRawSHA256 string `gorm:"type:text;not null;default:''" json:"-"`
	WorldModelStateJSON string `gorm:"type:jsonb;not null;default:'{}'" json:"worldModelState"`

	RuntimeID          string `gorm:"type:text" json:"runtimeId,omitempty"`
	ModelProviderID    string `gorm:"type:text" json:"modelProviderId,omitempty"`
	ModelID            string `gorm:"type:text" json:"modelId,omitempty"`
	VerificationStatus string `gorm:"type:text;not null;default:'not_required'" json:"verificationStatus"`
	ResultSummary      string `gorm:"type:text" json:"resultSummary,omitempty"`
	LastError          string `gorm:"type:text" json:"lastError,omitempty"`

	DedupeKey    string     `gorm:"type:text;not null;index" json:"dedupeKey"`
	NextReviewAt *time.Time `gorm:"index" json:"nextReviewAt,omitempty"`

	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
	CompletedAt *time.Time `json:"completedAt,omitempty"`
	Version     int64      `gorm:"not null;default:1" json:"version"`
}

// OperationEvent is an immutable audit row for an Operation (§10.5).
type OperationEvent struct {
	ID           uuid.UUID `gorm:"type:uuid;primary_key;default:uuid_generate_v4()" json:"id,omitempty"`
	OperationID  uuid.UUID `gorm:"type:uuid;not null;index" json:"operationId"`
	EventType    string    `gorm:"type:text;not null" json:"eventType"`
	ActorType    string    `gorm:"type:text;not null" json:"actorType"`
	ActorID      string    `gorm:"type:text" json:"actorId,omitempty"`
	BeforeStatus string    `gorm:"type:text" json:"beforeStatus,omitempty"`
	AfterStatus  string    `gorm:"type:text" json:"afterStatus,omitempty"`
	Message      string    `gorm:"type:text" json:"message,omitempty"`
	PayloadJSON  string    `gorm:"type:jsonb;not null;default:'{}'" json:"payload"`
	CreatedAt    time.Time `json:"createdAt"`
}
