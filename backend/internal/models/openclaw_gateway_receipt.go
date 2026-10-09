package models

import (
	"time"

	"github.com/google/uuid"
)

// OpenClawGatewaySessionReceipt stores the private identifiers that HAI needs
// to reconcile a specifically approved Gateway session after a backend restart.
// Its session and run IDs are intentionally omitted from JSON responses.
type OpenClawGatewaySessionReceipt struct {
	ID                             uuid.UUID  `gorm:"type:uuid;primary_key;default:uuid_generate_v4()" json:"id,omitempty"`
	ExecutionReference             string     `gorm:"type:varchar(64);uniqueIndex" json:"executionReference"`
	OwnerIdentity                  string     `gorm:"type:varchar(255);index" json:"-"`
	RuntimeTaskID                  string     `gorm:"type:varchar(120);index" json:"-"`
	SessionKey                     string     `gorm:"type:text;not null" json:"-"`
	SessionID                      string     `gorm:"type:varchar(256);default:''" json:"-"`
	RequestedModel                 string     `gorm:"type:varchar(255);default:''" json:"-"`
	UsageSummary                   string     `gorm:"type:text;not null;default:''" json:"-"`
	UsageSnapshotJSON              *string    `gorm:"type:jsonb" json:"-"`
	UsageAttempts                  int        `gorm:"not null;default:0" json:"-"`
	UsageNextAttemptAt             *time.Time `json:"-"`
	UsageCapturedAt                *time.Time `json:"-"`
	RunID                          string     `gorm:"type:varchar(256);not null" json:"-"`
	Status                         string     `gorm:"type:varchar(30);index;not null" json:"status"`
	TerminalStatus                 string     `gorm:"type:varchar(30)" json:"terminalStatus,omitempty"`
	CancellationIntentID           string     `gorm:"type:varchar(64);not null;default:''" json:"-"`
	CancellationStatus             string     `gorm:"type:varchar(30);not null;default:'';index" json:"-"`
	CancellationMessage            string     `gorm:"type:varchar(512);not null;default:''" json:"-"`
	CancellationAttempts           int        `gorm:"not null;default:0" json:"-"`
	CancellationAutomaticAttempts  int        `gorm:"not null;default:0" json:"-"`
	CancellationAt                 *time.Time `json:"-"`
	CancellationTriedAt            *time.Time `json:"-"`
	CancellationNextAttemptAt      *time.Time `json:"-"`
	CancellationDeliveryLeaseUntil *time.Time `json:"-"`
	CancellationDeliveryToken      string     `gorm:"type:varchar(36);not null;default:''" json:"-"`
	CancellationReviewAt           *time.Time `json:"-"`
	CreatedAt                      time.Time  `gorm:"index" json:"createdAt"`
	TerminalAt                     *time.Time `json:"terminalAt,omitempty"`
	ReconciledAt                   *time.Time `json:"reconciledAt,omitempty"`
	ReviewReason                   string     `gorm:"type:text" json:"-"`
	ReviewAt                       *time.Time `json:"reviewAt,omitempty"`
}

func (OpenClawGatewaySessionReceipt) TableName() string {
	return "openclaw_gateway_session_receipts"
}

// OpenClawGatewayReconcileCursor stores the keyset and high-water mark for a
// single bounded scan cycle. The row lock serializes page reservations across
// workers; the cursor survives process restarts.
type OpenClawGatewayReconcileCursor struct {
	Name                    string     `gorm:"type:varchar(40);primaryKey"`
	AfterCreatedAt          *time.Time `gorm:"type:timestamptz"`
	AfterExecutionReference string     `gorm:"type:varchar(64);not null;default:''"`
	CycleHighCreatedAt      *time.Time `gorm:"type:timestamptz"`
	CycleHighReference      string     `gorm:"type:varchar(64);not null;default:''"`
	UpdatedAt               time.Time  `gorm:"not null;default:now()"`
}

func (OpenClawGatewayReconcileCursor) TableName() string {
	return "openclaw_gateway_reconcile_cursors"
}
