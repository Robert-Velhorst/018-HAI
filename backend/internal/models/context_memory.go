package models

import (
	"github.com/google/uuid"
	"time"
)

type ContextMemory struct {
	ID uuid.UUID `gorm:"type:uuid;primary_key;default:uuid_generate_v4()" json:"id,omitempty"`
	// OwnerIdentity is set from the verified identity boundary and never accepted
	// from or returned to API clients.
	OwnerIdentity string  `gorm:"type:varchar(255);index" json:"-"`
	ProjectKey    string  `gorm:"type:varchar(255);index" json:"projectKey,omitempty"`
	Kind          string  `gorm:"type:varchar(50);index" json:"kind"`
	Content       string  `gorm:"type:text;not null" json:"content"`
	Summary       string  `gorm:"type:text" json:"summary,omitempty"`
	Tags          string  `gorm:"type:varchar(512)" json:"tags,omitempty"`
	Confidence    float64 `gorm:"default:0.7" json:"confidence"`
	SourceURI     string  `gorm:"type:varchar(1024)" json:"sourceUri,omitempty"`
	// SourceExtractionID is an internal provenance key, distinct from the evidence URI.
	SourceExtractionID *uuid.UUID `gorm:"type:uuid" json:"sourceExtractionId,omitempty"`
	SourceLabel        string     `gorm:"type:varchar(255)" json:"sourceLabel,omitempty"`
	ContentHash        string     `gorm:"type:varchar(64);index" json:"contentHash,omitempty"`
	Archived           bool       `gorm:"default:false;index" json:"archived"`
	LastUsedAt         *time.Time `json:"lastUsedAt,omitempty"`
	CreatedAt          time.Time  `json:"createdAt"`
	UpdatedAt          time.Time  `json:"updatedAt"`
}
