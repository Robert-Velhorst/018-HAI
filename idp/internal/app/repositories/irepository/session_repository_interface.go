package irepository

import (
	"context"
	"errors"
	"time"

	"automation-hub-idp/internal/app/models"
	"github.com/google/uuid"
)

var (
	ErrSessionRevoked              = errors.New("durable session is no longer valid")
	ErrSessionUserUnavailable      = errors.New("session user is unavailable")
	ErrSessionAuthorityUnavailable = errors.New("durable session authority is unavailable")
)

type SessionIdentity struct {
	UserID         uuid.UUID
	FamilyID       uuid.UUID
	RefreshUUID    uuid.UUID
	SessionVersion int64
	ExpiresAt      time.Time
}

// Revoked/denied are committed outcomes, not transaction callback errors.
type SessionRotationResult struct {
	Receipt *models.RefreshRotationReceipt
	Revoked bool
}

type SessionRepository interface {
	Ready(context.Context) error
	CreateFamily(context.Context, models.User, SessionIdentity) (*models.User, error)
	Validate(context.Context, SessionIdentity) (*models.User, error)
	ValidateRotation(context.Context, SessionIdentity, uuid.UUID) (*models.User, error)
	Rotate(context.Context, SessionIdentity, uuid.UUID, string, time.Duration) (*SessionRotationResult, error)
	RevokeFamily(context.Context, SessionIdentity) error
	RevokeFamilyIfCurrent(context.Context, SessionIdentity, uuid.UUID) error
}
