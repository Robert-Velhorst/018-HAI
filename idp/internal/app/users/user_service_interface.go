package users

import (
	"automation-hub-idp/internal/app/models"
	"automation-hub-idp/internal/app/utils"
	"github.com/google/uuid"
	"time"
)

type UserService interface {
	CreateUser(user models.User) (*models.User, error)
	GetUserByID(id uuid.UUID) (*models.User, error)
	GetUserByEmail(email string) (*models.User, error)
	GetUserByResetToken(token string) (*models.User, error)
	StorePasswordResetToken(id uuid.UUID, expectedSessionVersion int64, tokenDigest string, expiresAt time.Time) error
	UpdateUser(user models.User) (*models.User, error)
	CompleteSuccessfulLogin(expected models.User) (*models.User, error)
	DeleteUser(id uuid.UUID) error
	GetAllUsers(p *utils.Pagination) ([]*models.User, error)
	ConsumePasswordResetToken(id uuid.UUID, token, newPassword string) error
	ClearPasswordResetToken(id uuid.UUID, token string) error
}

type UserAccountUpdateService interface {
	UserService
	UpdateAccount(id uuid.UUID, expectedSessionVersion int64, currentPassword, email, newPassword string) (*models.User, error)
	UpdatePasswordWithCurrentPassword(id uuid.UUID, expectedSessionVersion int64, currentPassword, newPassword string) error
}

type AccountUpdateService interface {
	GetUserByID(id uuid.UUID) (*models.User, error)
	UpdateAccount(id uuid.UUID, expectedSessionVersion int64, currentPassword, email, newPassword string) (*models.User, error)
}
