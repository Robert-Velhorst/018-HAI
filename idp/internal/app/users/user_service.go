package users

import (
	"automation-hub-idp/internal/app/config"
	"automation-hub-idp/internal/app/models"
	"automation-hub-idp/internal/app/repositories"
	"automation-hub-idp/internal/app/repositories/irepository"
	"automation-hub-idp/internal/app/services"
	"automation-hub-idp/internal/app/services/iservice"
	"automation-hub-idp/internal/app/utils"
	"automation-hub-idp/internal/infra"
	"errors"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"net/mail"
	"strings"
	"time"
)

type userServiceImpl struct {
	userRepo       irepository.UserRepository
	logger         iservice.Logger
	hasher         utils.PasswordHasher
	ownedResources *services.OwnedResources
}

func (s *userServiceImpl) Close() error {
	if s == nil {
		return nil
	}
	return s.ownedResources.Close()
}

var (
	ErrUserAlreadyExists         = errors.New("user already exists")
	ErrUserNotFound              = errors.New("user not found")
	ErrInvalidPasswordResetToken = errors.New("invalid password reset token")
	ErrCurrentPasswordRequired   = errors.New("current password is required")
	ErrInvalidCurrentPassword    = errors.New("current password is incorrect")
	ErrInvalidEmail              = errors.New("enter a valid email address")
	ErrConcurrentUserUpdate      = irepository.ErrConcurrentUserUpdate
)

func (s *userServiceImpl) logError(message string, args ...interface{}) {
	if s.logger != nil {
		s.logger.Error(message, args...)
	}
}

func NewUserService(repo irepository.UserRepository, logger iservice.Logger, hasher utils.PasswordHasher) UserAccountUpdateService {
	return &userServiceImpl{
		userRepo: repo,
		logger:   logger,
		hasher:   hasher,
	}
}

func GetDefaultUserService() (UserAccountUpdateService, error) {
	database, err := infra.GetDefaultDB()
	if err != nil {
		return nil, err
	}
	userService, err := NewDefaultUserServiceWithDatabase(database)
	if err != nil {
		if pool, poolErr := database.DB(); poolErr == nil {
			err = errors.Join(err, pool.Close())
		}
	}
	return userService, err
}

// NewDefaultUserServiceWithDatabase borrows the caller-owned initialized database.
func NewDefaultUserServiceWithDatabase(database *gorm.DB) (result UserAccountUpdateService, err error) {
	owned := &services.OwnedResources{}
	defer func() {
		if result == nil {
			err = errors.Join(err, owned.Close())
		}
	}()
	if database == nil || database.Config == nil {
		return nil, errors.New("user database is required")
	}
	if _, err := database.DB(); err != nil {
		return nil, err
	}
	logger, err := services.DefaultLogger()
	if err != nil {
		return nil, err
	}
	owned.Add(logger)
	userRepository := repositories.NewGormUserRepository(database, logger)
	hasher := config.AuthenticationConfig.PasswordHasher
	return &userServiceImpl{userRepo: userRepository, logger: logger, hasher: hasher, ownedResources: owned}, nil
}

func (s *userServiceImpl) CreateUser(user models.User) (*models.User, error) {
	existingUser, lookupErr := s.userRepo.FindByEmail(user.Email)
	if lookupErr == nil && existingUser != nil {
		s.logError("User already exists with email: %s", user.Email)
		return nil, ErrUserAlreadyExists
	}
	if lookupErr != nil && !errors.Is(lookupErr, irepository.ErrUserNotFound) {
		s.logError("Failed to check existing user with email: %s, %v", user.Email, lookupErr)
		return nil, errors.New("failed to check existing user")
	}

	createdUser, err := s.userRepo.Create(&user)
	if errors.Is(err, irepository.ErrDuplicateUser) {
		return nil, ErrUserAlreadyExists
	}
	return createdUser, err
}

func (s *userServiceImpl) GetUserByID(id uuid.UUID) (*models.User, error) {
	return s.userRepo.FindByID(id)
}

func (s *userServiceImpl) GetAllUsers(p *utils.Pagination) ([]*models.User, error) {
	if p == nil {
		defaultPagination := utils.DefaultPagination()
		p = &defaultPagination
	}

	return s.userRepo.FindAll(*p)
}

func (s *userServiceImpl) CompleteSuccessfulLogin(expected models.User) (*models.User, error) {
	user, err := s.userRepo.CompleteSuccessfulLogin(expected, time.Now().UTC())
	if err != nil {
		s.logError("Failed to complete successful login for user %s: %v", expected.ID, err)
		return nil, err
	}
	if user == nil {
		return nil, errors.New("successful login returned no account")
	}
	return user, nil
}

func (s *userServiceImpl) UpdateUser(user models.User) (*models.User, error) {
	currentUser, err := s.userRepo.FindByID(user.ID)
	if err != nil || currentUser == nil {
		s.logError("Error fetching user with ID: %s, %v", user.ID, err)
		return nil, errors.New("error fetching user by ID")
	}
	if user.SessionVersion != currentUser.SessionVersion || user.ResetPasswordToken != currentUser.ResetPasswordToken ||
		!sameResetTokenExpiry(user.ResetTokenExpires, currentUser.ResetTokenExpires) {
		return nil, ErrConcurrentUserUpdate
	}

	// Login and registration use lowercase email addresses. Keep this legacy
	// account-update path canonical too, or a mixed-case update can lock out
	// the user on their next login.
	user.Email = strings.ToLower(strings.TrimSpace(user.Email))
	if currentUser.Email != user.Email {
		existingUser, err := s.userRepo.FindByEmail(user.Email)
		if err == nil && existingUser.ID != user.ID {
			s.logError("Email already exists: %s", user.Email)
			return nil, errors.New("email already exists")
		}
	}
	user.Password = currentUser.Password
	user.SessionVersion = currentUser.SessionVersion
	user.ResetPasswordToken = currentUser.ResetPasswordToken
	user.ResetTokenExpires = currentUser.ResetTokenExpires
	return s.userRepo.Update(&user)
}

func sameResetTokenExpiry(left, right *time.Time) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}

func (s *userServiceImpl) DeleteUser(id uuid.UUID) error {
	err := s.userRepo.Delete(id)
	if err != nil {
		s.logError("Error deleting user with ID: %s, %v", id, err)
		return errors.New("error deleting user")
	}
	return nil
}

func (s *userServiceImpl) UpdatePasswordWithCurrentPassword(id uuid.UUID, expectedSessionVersion int64, currentPassword, newPassword string) error {
	if currentPassword == "" {
		return ErrCurrentPasswordRequired
	}
	if err := utils.ValidatePassword(newPassword); err != nil {
		return err
	}

	user, err := s.userRepo.FindByID(id)
	if err != nil || user == nil {
		if s.logger != nil {
			s.logger.Error("Error fetching user before password update: %v", err)
		}
		return errors.New("error verifying current password")
	}
	if err := s.verifyCurrentPassword(user, currentPassword); err != nil {
		return err
	}

	return s.persistPasswordUpdate(id, expectedSessionVersion, newPassword)
}

func (s *userServiceImpl) UpdateAccount(id uuid.UUID, expectedSessionVersion int64, currentPassword, email, newPassword string) (*models.User, error) {
	if id == uuid.Nil {
		return nil, errors.New("account update requires a user ID")
	}
	if newPassword != "" {
		if err := utils.ValidatePassword(newPassword); err != nil {
			return nil, err
		}
		if currentPassword == "" {
			return nil, ErrCurrentPasswordRequired
		}
	}

	current, err := s.userRepo.FindByID(id)
	if err != nil || current == nil {
		if s.logger != nil {
			s.logger.Error("Error fetching user before account update: %v", err)
		}
		return nil, errors.New("error fetching user")
	}
	if current.SessionVersion != expectedSessionVersion {
		return nil, ErrConcurrentUserUpdate
	}

	// Authentication lookup uses lowercase email addresses; persist account
	// changes in the same canonical form so a case-only update cannot lock out
	// the user after the next login.
	email = strings.ToLower(strings.TrimSpace(email))
	var emailUpdate *string
	if email != "" && email != current.Email {
		emailUpdate = &email
	}
	if emailUpdate != nil {
		parsedEmail, err := mail.ParseAddress(*emailUpdate)
		if err != nil || parsedEmail.Address != *emailUpdate {
			return nil, ErrInvalidEmail
		}
	}
	if emailUpdate == nil && newPassword == "" {
		return current, nil
	}
	if currentPassword == "" {
		return nil, ErrCurrentPasswordRequired
	}
	if err := s.verifyCurrentPassword(current, currentPassword); err != nil {
		return nil, err
	}

	var passwordHash string
	if newPassword != "" {
		passwordHash, err = s.hasher.Hash(newPassword)
		if err != nil {
			if s.logger != nil {
				s.logger.Error("Error hashing password for user with ID: %s, %v", id, err)
			}
			return nil, errors.New("error hashing password")
		}
	}

	now := time.Now().UTC()
	if err := s.userRepo.UpdateAccount(id, expectedSessionVersion, emailUpdate, passwordHash, now); err != nil {
		if errors.Is(err, irepository.ErrConcurrentUserUpdate) {
			return nil, ErrConcurrentUserUpdate
		}
		if errors.Is(err, irepository.ErrDuplicateUser) {
			return nil, ErrUserAlreadyExists
		}
		if s.logger != nil {
			s.logger.Error("Error updating account for user with ID: %s, %v", id, err)
		}
		return nil, errors.New("error updating account")
	}

	updated := *current
	if emailUpdate != nil {
		updated.Email = *emailUpdate
	}
	if emailUpdate != nil || passwordHash != "" {
		updated.ResetPasswordToken = ""
		updated.ResetTokenExpires = nil
	}
	if passwordHash != "" {
		updated.Password = passwordHash
		updated.FirstAccess = false
	}
	updated.SessionVersion++
	updated.UpdatedAt = now
	return &updated, nil
}

func (s *userServiceImpl) verifyCurrentPassword(user *models.User, currentPassword string) error {
	if err := s.hasher.Compare(user.Password, currentPassword); err != nil {
		if errors.Is(err, utils.ErrPasswordMismatch) {
			return ErrInvalidCurrentPassword
		}
		if s.logger != nil {
			s.logger.Error("Error verifying current password for user with ID: %s, %v", user.ID, err)
		}
		return errors.New("error verifying current password")
	}
	return nil
}

func (s *userServiceImpl) persistPasswordUpdate(id uuid.UUID, expectedSessionVersion int64, newPassword string) error {
	passwordHash, err := s.hasher.Hash(newPassword)
	if err != nil {
		if s.logger != nil {
			s.logger.Error("Error hashing password for user with ID: %s, %v", id, err)
		}
		return errors.New("error hashing password")
	}
	if err := s.userRepo.UpdatePassword(id, expectedSessionVersion, passwordHash, time.Now().UTC()); err != nil {
		if errors.Is(err, irepository.ErrConcurrentUserUpdate) {
			return ErrConcurrentUserUpdate
		}
		if s.logger != nil {
			s.logger.Error("Error updating user with ID: %s, %v", id, err)
		}
		return errors.New("error updating user")
	}
	return nil
}

func (s *userServiceImpl) StorePasswordResetToken(id uuid.UUID, expectedSessionVersion int64, tokenDigest string, expiresAt time.Time) error {
	if id == uuid.Nil || !utils.IsPasswordResetTokenDigest(tokenDigest) || !expiresAt.After(time.Now().UTC()) {
		return errors.New("invalid password reset token record")
	}
	if err := s.userRepo.SetPasswordResetToken(id, expectedSessionVersion, tokenDigest, expiresAt.UTC(), time.Now().UTC()); err != nil {
		if errors.Is(err, irepository.ErrConcurrentUserUpdate) {
			return ErrConcurrentUserUpdate
		}
		s.logError("Error storing password reset token")
		return errors.New("error storing password reset token")
	}
	return nil
}

func (s *userServiceImpl) GetUserByEmail(email string) (*models.User, error) {
	user, err := s.userRepo.FindByEmail(email)
	if err != nil {
		s.logError("Failed to fetch user by email: %v", err)
		if errors.Is(err, irepository.ErrUserNotFound) {
			return nil, ErrUserNotFound
		}
		return nil, errors.New("failed to fetch user")
	}

	if user == nil {
		s.logError("User not found during email lookup")
		return nil, errors.New("user not found")
	}

	return user, nil
}

func (s *userServiceImpl) GetUserByResetToken(token string) (*models.User, error) {
	user, err := s.userRepo.FindByResetToken(token)
	if err != nil {
		s.logError("Failed to fetch user with password reset token")
		if errors.Is(err, irepository.ErrUserNotFound) {
			return nil, ErrUserNotFound
		}
		return nil, errors.New("failed to fetch user")
	}

	if user == nil {
		s.logError("User not found with password reset token")
		return nil, errors.New("user not found")
	}

	return user, nil
}

func (s *userServiceImpl) ConsumePasswordResetToken(id uuid.UUID, token, newPassword string) error {
	if err := utils.ValidatePassword(newPassword); err != nil {
		return err
	}
	passwordHash, err := s.hasher.Hash(newPassword)
	if err != nil {
		s.logError("Error hashing password during password reset")
		return errors.New("error hashing password")
	}
	if err := s.userRepo.ConsumePasswordResetToken(id, token, passwordHash, time.Now().UTC()); err != nil {
		if errors.Is(err, irepository.ErrResetTokenNotConsumable) {
			return ErrInvalidPasswordResetToken
		}
		s.logError("Error consuming password reset token")
		return errors.New("error consuming password reset token")
	}
	return nil
}

func (s *userServiceImpl) ClearPasswordResetToken(id uuid.UUID, token string) error {
	if err := s.userRepo.ClearPasswordResetToken(id, token); err != nil {
		s.logError("Error clearing undelivered password reset token")
		return errors.New("error clearing password reset token")
	}
	return nil
}
