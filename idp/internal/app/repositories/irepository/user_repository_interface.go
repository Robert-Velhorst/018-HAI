package irepository

import (
	"automation-hub-idp/internal/app/models"
	"automation-hub-idp/internal/app/utils"
	"errors"
	"github.com/google/uuid"
	"time"
)

var (
	ErrDuplicateUser     = errors.New("duplicate user")
	ErrUserNotFound      = errors.New("user not found")
	ErrInvalidResetToken = errors.New("invalid or expired reset token")
)

type UserRepository interface {
	FindByID(id uuid.UUID) (*models.User, error)
	FindByEmail(email string) (*models.User, error)
	Create(user *models.User) (*models.User, error)
	Update(user *models.User) (*models.User, error)
	Delete(id uuid.UUID) error
	FindAll(p utils.Pagination) ([]*models.User, error)
	FindByResetToken(token string) (*models.User, error)
	StorePasswordReset(id uuid.UUID, token string, expiresAt time.Time) error
	ClearPasswordResetIfToken(id uuid.UUID, token string) error
	ConsumePasswordReset(token, passwordHash string) error
}
