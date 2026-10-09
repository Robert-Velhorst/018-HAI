package models

import (
	"time"

	"github.com/google/uuid"
)

// OpenClawGatewayArtifactReceipt retains only one-way artifact metadata for a
// specific, owner-bound delegated execution. Original artifact identifiers,
// titles, download URLs, and content never enter this model.
type OpenClawGatewayArtifactReceipt struct {
	ID                 uuid.UUID `gorm:"type:uuid;primary_key;default:uuid_generate_v4()" json:"id,omitempty"`
	ExecutionReference string    `gorm:"type:varchar(64);not null;index" json:"-"`
	ArtifactDigest     string    `gorm:"type:varchar(64);not null" json:"artifactDigest"`
	ArtifactType       string    `gorm:"type:varchar(120);not null" json:"artifactType"`
	MIMEType           string    `gorm:"type:varchar(160);not null" json:"mimeType,omitempty"`
	SizeBytes          *int64    `json:"sizeBytes"`
	CreatedAt          time.Time `gorm:"index" json:"createdAt"`
}

func (OpenClawGatewayArtifactReceipt) TableName() string {
	return "openclaw_gateway_artifact_receipts"
}
