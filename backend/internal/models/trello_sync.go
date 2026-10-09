package models

import (
	"time"

	"github.com/google/uuid"
)

// TrelloSyncState is the durable checkpoint for one connected Trello board.
// The owner and board binding prevent a saved cursor from being reused after a
// source is reassigned or pointed at a different board.
type TrelloSyncState struct {
	SourceID          uuid.UUID  `gorm:"type:uuid;primaryKey" json:"sourceId"`
	OwnerIdentity     string     `gorm:"type:varchar(255);not null" json:"-"`
	BoardID           string     `gorm:"type:varchar(32);not null" json:"-"`
	LogicalJobID      *uuid.UUID `gorm:"type:uuid;index" json:"-"`
	Generation        int64      `gorm:"not null;default:1" json:"-"`
	Phase             string     `gorm:"type:varchar(40);not null" json:"-"`
	CardCursor        string     `gorm:"type:varchar(24);not null;default:''" json:"-"`
	ActionCursor      string     `gorm:"type:varchar(24);not null;default:''" json:"-"`
	ActionSince       *time.Time `json:"-"`
	CycleStartedAt    time.Time  `gorm:"not null" json:"-"`
	LastSuccessfulAt  *time.Time `json:"-"`
	MaxCardActivityAt *time.Time `json:"-"`
	PagesProcessed    int        `gorm:"not null;default:0" json:"-"`
	RecordsProcessed  int64      `gorm:"not null;default:0" json:"-"`
	UpdatedAt         time.Time  `json:"-"`
}

// TrelloSyncPage records a committed provider page. The cursor pair and page
// hash make retry replay observable without retaining duplicate provider JSON.
type TrelloSyncPage struct {
	ID            uuid.UUID `gorm:"type:uuid;primaryKey" json:"-"`
	SourceID      uuid.UUID `gorm:"type:uuid;not null;uniqueIndex:ux_trello_sync_pages_identity,priority:1" json:"-"`
	LogicalJobID  uuid.UUID `gorm:"type:uuid;not null;uniqueIndex:ux_trello_sync_pages_identity,priority:2" json:"-"`
	Generation    int64     `gorm:"not null;uniqueIndex:ux_trello_sync_pages_identity,priority:3" json:"-"`
	Phase         string    `gorm:"type:varchar(40);not null;uniqueIndex:ux_trello_sync_pages_identity,priority:4" json:"-"`
	CursorBefore  string    `gorm:"type:varchar(24);not null;default:'';uniqueIndex:ux_trello_sync_pages_identity,priority:5" json:"-"`
	CursorAfter   string    `gorm:"type:varchar(24);not null;default:''" json:"-"`
	RecordCount   int       `gorm:"not null;default:0" json:"-"`
	RequestCount  int       `gorm:"not null;default:0" json:"-"`
	ResponseBytes int64     `gorm:"not null;default:0" json:"-"`
	Fingerprint   string    `gorm:"type:char(64);not null" json:"-"`
	CommittedAt   time.Time `gorm:"not null" json:"-"`
}

// TrelloActionReceipt gives comment-action imports a stable, owner-scoped
// idempotency key without storing a second copy of the provider payload.
type TrelloActionReceipt struct {
	SourceID    uuid.UUID `gorm:"type:uuid;primaryKey" json:"-"`
	ActionID    string    `gorm:"type:varchar(255);primaryKey" json:"-"`
	CardID      string    `gorm:"type:varchar(255);not null" json:"-"`
	OccurredAt  time.Time `gorm:"not null" json:"-"`
	Fingerprint string    `gorm:"type:char(64);not null" json:"-"`
	CreatedAt   time.Time `json:"-"`
}
