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
	UpdateUser(user models.User) (*models.User, error)
	DeleteUser(id uuid.UUID) error
	GetAllUsers(p *utils.Pagination) ([]*models.User, error)
	UpdatePassword(id uuid.UUID, newPassword string) error
	StorePasswordReset(id uuid.UUID, token string, expiresAt time.Time) error
	ClearPasswordResetIfToken(id uuid.UUID, token string) error
	CompletePasswordReset(token, newPassword string) error
}
