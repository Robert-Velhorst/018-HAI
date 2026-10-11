package models

import (
	"time"

	"github.com/google/uuid"
)

const (
	SourceExtractionCorrectionPending   = "pending"
	SourceExtractionCorrectionCompleted = "completed"
	SourceExtractionCorrectionConflict  = "conflict"
	SourceExtractionCorrectionFailed    = "failed"
)

// SourceExtractionCorrection is a durable, owner-scoped correction intent.
// PatchJSON and BeforeStateJSON are private persistence fields and must never
// be exposed through logs or API responses.
type SourceExtractionCorrection struct {
	ID                 uuid.UUID  `gorm:"type:uuid;primaryKey;default:uuid_generate_v4()" json:"-"`
	OwnerIdentity      string     `gorm:"type:varchar(255);not null" json:"-"`
	SourceID           uuid.UUID  `gorm:"type:uuid;not null" json:"-"`
	ExtractionID       uuid.UUID  `gorm:"type:uuid;not null" json:"-"`
	IdempotencyKeyHash string     `gorm:"type:varchar(64);not null" json:"-"`
	RequestHash        string     `gorm:"type:varchar(64);not null" json:"-"`
	ExpectedRevision   time.Time  `gorm:"type:timestamptz;not null" json:"-"`
	PatchJSON          string     `gorm:"type:jsonb;not null" json:"-"`
	BeforeStateJSON    string     `gorm:"type:jsonb;not null" json:"-"`
	RequiresRetraction bool       `gorm:"not null;default:false" json:"-"`
	Phase              string     `gorm:"type:varchar(40);not null" json:"-"`
	Status             string     `gorm:"type:varchar(24);not null" json:"-"`
	ErrorCode          string     `gorm:"type:varchar(64);not null;default:''" json:"-"`
	DurableJobID       uuid.UUID  `gorm:"type:uuid;not null" json:"-"`
	Attempts           int        `gorm:"not null;default:0" json:"-"`
	MaxAttempts        int        `gorm:"not null;default:5" json:"-"`
	AppliedRevision    *time.Time `gorm:"type:timestamptz" json:"-"`
	CreatedAt          time.Time  `json:"-"`
	UpdatedAt          time.Time  `json:"-"`
	CompletedAt        *time.Time `json:"-"`
}

func (SourceExtractionCorrection) TableName() string { return "source_extraction_corrections" }
