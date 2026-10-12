package models

import (
	"time"

	"github.com/google/uuid"
)

type SessionFamily struct {
	ID                 uuid.UUID `gorm:"type:uuid;primaryKey"`
	UserID             uuid.UUID `gorm:"type:uuid;not null;index"`
	User               User      `gorm:"foreignKey:UserID;constraint:OnUpdate:RESTRICT,OnDelete:RESTRICT" json:"-"`
	SessionVersion     int64     `gorm:"not null;check:ck_session_family_version,session_version >= 0"`
	CurrentGeneration  int64     `gorm:"not null;check:ck_session_family_generation,current_generation >= 0"`
	CurrentRefreshUUID uuid.UUID `gorm:"type:uuid;not null;uniqueIndex"`
	ExpiresAt          time.Time `gorm:"not null;index"`
	RevokedAt          *time.Time
	CreatedAt          time.Time
	UpdatedAt          time.Time
}
