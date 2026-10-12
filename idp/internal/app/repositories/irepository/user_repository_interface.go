package irepository

import (
	"automation-hub-idp/internal/app/models"
	"automation-hub-idp/internal/app/utils"
	"errors"
	"github.com/google/uuid"
	"time"
)

var (
	ErrDuplicateUser           = errors.New("duplicate user")
	ErrUserNotFound            = errors.New("user not found")
	ErrConcurrentUserUpdate    = errors.New("user changed concurrently")
	ErrResetTokenNotConsumable = errors.New("password reset token is invalid, expired, or already consumed")
)

type UserRepository interface {
	FindByID(id uuid.UUID) (*models.User, error)
	FindByEmail(email string) (*models.User, error)
	Create(user *models.User) (*models.User, error)
	Update(user *models.User) (*models.User, error)
	CompleteSuccessfulLogin(expected models.User, now time.Time) (*models.User, error)
	Delete(id uuid.UUID) error
	FindAll(p utils.Pagination) ([]*models.User, error)
	FindByResetToken(token string) (*models.User, error)
	SetPasswordResetToken(id uuid.UUID, expectedSessionVersion int64, tokenDigest string, expiresAt, now time.Time) error
	UpdatePassword(id uuid.UUID, expectedSessionVersion int64, passwordHash string, now time.Time) error
	UpdateAccount(id uuid.UUID, expectedSessionVersion int64, email *string, passwordHash string, now time.Time) error
	ConsumePasswordResetToken(id uuid.UUID, token, passwordHash string, now time.Time) error
	ClearPasswordResetToken(id uuid.UUID, token string) error
}
