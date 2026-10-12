package models

import (
	"time"

	"github.com/google/uuid"
)

// TrelloWebhookReceipt stores only the verified fields needed to deduplicate
// a callback, queue its read-only reconciliation, and preserve changed-comment
// evidence. The original provider payload and signature are never retained.
type TrelloWebhookReceipt struct {
	ID                       uuid.UUID  `gorm:"type:uuid;primaryKey" json:"-"`
	SourceID                 uuid.UUID  `gorm:"type:uuid;not null;uniqueIndex:ux_trello_webhook_source_action,priority:1" json:"-"`
	ActionID                 string     `gorm:"type:varchar(24);not null;uniqueIndex:ux_trello_webhook_source_action,priority:2" json:"-"`
	BoardID                  string     `gorm:"type:varchar(24);not null" json:"-"`
	CardID                   string     `gorm:"type:varchar(24);not null;default:''" json:"-"`
	CardName                 string     `gorm:"type:varchar(512);not null;default:''" json:"-"`
	CardURL                  string     `gorm:"type:varchar(1024);not null;default:''" json:"-"`
	ActionType               string     `gorm:"type:varchar(80);not null" json:"-"`
	ActionText               string     `gorm:"type:text;not null;default:''" json:"-"`
	ActorID                  string     `gorm:"type:varchar(24);not null;default:''" json:"-"`
	ReconciliationGeneration int64      `gorm:"not null;default:0" json:"-"`
	OccurredAt               time.Time  `gorm:"not null" json:"-"`
	Fingerprint              string     `gorm:"type:char(64);not null" json:"-"`
	DurableJobID             uuid.UUID  `gorm:"type:uuid;not null;uniqueIndex" json:"-"`
	SyncJobID                *uuid.UUID `gorm:"type:uuid" json:"-"`
	Status                   string     `gorm:"type:varchar(20);not null;default:'queued';index" json:"-"`
	ReceivedAt               time.Time  `gorm:"not null" json:"-"`
	CompletedAt              *time.Time `json:"-"`
	UpdatedAt                time.Time  `gorm:"not null" json:"-"`
}

func (TrelloWebhookReceipt) TableName() string { return "trello_webhook_receipts" }

// TrelloWebhookReconciliationState is a per-source durable watermark. A sync
// claims a fixed generation snapshot; receipts accepted during that sync get
// a higher generation and therefore require a later reconciliation.
type TrelloWebhookReconciliationState struct {
	SourceID                       uuid.UUID  `gorm:"type:uuid;primaryKey" json:"-"`
	OwnerIdentity                  string     `gorm:"type:varchar(255);not null" json:"-"`
	RequestedGeneration            int64      `gorm:"not null;default:0" json:"-"`
	CompletedGeneration            int64      `gorm:"not null;default:0" json:"-"`
	RequiredTrelloGeneration       int64      `gorm:"not null;default:1" json:"-"`
	ActiveGeneration               *int64     `json:"-"`
	ActiveRequiredTrelloGeneration *int64     `json:"-"`
	ActiveSyncJobID                *uuid.UUID `gorm:"type:uuid" json:"-"`
	DispatchAttempt                int        `gorm:"not null;default:0" json:"-"`
	FailedGeneration               *int64     `json:"-"`
	UpdatedAt                      time.Time  `gorm:"not null" json:"-"`
}

func (TrelloWebhookReconciliationState) TableName() string {
	return "trello_webhook_reconciliation_states"
}
