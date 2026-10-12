package models

import (
	"time"

	"github.com/google/uuid"
)

// Receipts retain the canonical response and its original grace deadline.
// Redis restoration must not select a new winner or restart that deadline.
type RefreshRotationReceipt struct {
	PredecessorUUID       uuid.UUID     `gorm:"type:uuid;primaryKey"`
	FamilyID              uuid.UUID     `gorm:"type:uuid;not null;index"`
	Family                SessionFamily `gorm:"foreignKey:FamilyID;constraint:OnUpdate:RESTRICT,OnDelete:RESTRICT" json:"-"`
	PredecessorGeneration int64         `gorm:"not null;check:ck_rotation_predecessor_generation,predecessor_generation >= 0"`
	ReplacementGeneration int64         `gorm:"not null;check:ck_rotation_replacement_generation,replacement_generation = predecessor_generation + 1"`
	ReplacementUUID       uuid.UUID     `gorm:"type:uuid;not null;uniqueIndex"`
	EncryptedPair         string        `gorm:"type:text;not null;check:ck_rotation_pair_size,length(encrypted_pair) > 0 AND length(encrypted_pair) <= 32768" json:"-"`
	ConsumedAt            time.Time     `gorm:"not null"`
	ReplayUntil           time.Time     `gorm:"not null;index;check:ck_rotation_replay_deadline,replay_until > consumed_at"`
}
