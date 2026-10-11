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
	"time"
)

type userServiceImpl struct {
	userRepo irepository.UserRepository
	logger   iservice.Logger
	hasher   utils.PasswordHasher
}

var (
	ErrUserAlreadyExists = errors.New("user already exists")
	ErrUserNotFound      = errors.New("user not found")
	ErrInvalidResetToken = errors.New("invalid or expired reset token")
)

func NewUserService(repo irepository.UserRepository, logger iservice.Logger, hasher utils.PasswordHasher) UserService {
	return &userServiceImpl{
		userRepo: repo,
		logger:   logger,
		hasher:   hasher,
	}
}

func GetDefaultUserService() (UserService, error) {
	logger, err := services.NewKafkaLogger(config.KafkaConfig.BrokersAddr, config.KafkaConfig.LoggerTopic)
	if err != nil {
		return nil, err
	}
	database, err := infra.GetDefaultDB()
	if err != nil {
		return nil, err
	}
	userRepository := repositories.NewGormUserRepository(database, logger)
	hasher := config.AuthenticationConfig.PasswordHasher
	return NewUserService(userRepository, logger, hasher), nil
}

func (s *userServiceImpl) CreateUser(user models.User) (*models.User, error) {
	existingUser, lookupErr := s.userRepo.FindByEmail(user.Email)
	if lookupErr == nil && existingUser != nil {
		s.logger.Error("User already exists with email: %s", user.Email)
		return nil, ErrUserAlreadyExists
	}
	if lookupErr != nil && !errors.Is(lookupErr, irepository.ErrUserNotFound) {
		s.logger.Error("Failed to check existing user with email: %s, %v", user.Email, lookupErr)
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

func (s *userServiceImpl) UpdateUser(user models.User) (*models.User, error) {
	currentUser, err := s.userRepo.FindByID(user.ID)
	if err != nil {
		s.logger.Error("Error fetching user with ID: %s, %v", user.ID, err)
		return nil, errors.New("error fetching user by ID")
	}

	if currentUser.Email != user.Email {
		existingUser, err := s.userRepo.FindByEmail(user.Email)
		if err == nil && existingUser.ID != user.ID {
			s.logger.Error("Email already exists: %s", user.Email)
			return nil, errors.New("email already exists")
		}
	}
	user.Password = currentUser.Password
	return s.userRepo.Update(&user)
}

func (s *userServiceImpl) DeleteUser(id uuid.UUID) error {
	err := s.userRepo.Delete(id)
	if err != nil {
		s.logger.Error("Error deleting user with ID: %s, %v", id, err)
		return errors.New("error deleting user")
	}
	return nil
}

func (s *userServiceImpl) UpdatePassword(id uuid.UUID, newPassword string) error {
	user, err := s.userRepo.FindByID(id)
	if err != nil {
		s.logger.Error("Error fetching user with ID: %s, %v", id, err)
		return errors.New("error fetching user")
	}

	user.Password, err = s.hasher.Hash(newPassword)
	if err != nil {
		s.logger.Error("Error hashing password for user with ID: %s, %v", id, err)
		return errors.New("error hashing password")
	}

	user.FirstAccess = false

	_, err = s.userRepo.Update(user)
	if err != nil {
		s.logger.Error("Error updating user with ID: %s, %v", id, err)
		return errors.New("error updating user")
	}
	return nil
}

func (s *userServiceImpl) CompletePasswordReset(token, newPassword string) error {
	passwordHash, err := s.hasher.Hash(newPassword)
	if err != nil {
		s.logger.Error("Error hashing password for password reset: %v", err)
		return errors.New("error hashing password")
	}
	if err := s.userRepo.ConsumePasswordReset(token, passwordHash); err != nil {
		if errors.Is(err, irepository.ErrInvalidResetToken) {
			return ErrInvalidResetToken
		}
		s.logger.Error("Error completing password reset: %v", err)
		return errors.New("error completing password reset")
	}
	return nil
}

func (s *userServiceImpl) StorePasswordReset(id uuid.UUID, token string, expiresAt time.Time) error {
	if err := s.userRepo.StorePasswordReset(id, token, expiresAt); err != nil {
		s.logger.Error("Error storing password reset for user with ID %s: %v", id, err)
		return errors.New("error storing password reset")
	}
	return nil
}

func (s *userServiceImpl) ClearPasswordResetIfToken(id uuid.UUID, token string) error {
	if err := s.userRepo.ClearPasswordResetIfToken(id, token); err != nil {
		s.logger.Error("Error clearing undelivered password reset for user with ID %s: %v", id, err)
		return errors.New("error clearing password reset")
	}
	return nil
}

func (s *userServiceImpl) GetUserByEmail(email string) (*models.User, error) {
	user, err := s.userRepo.FindByEmail(email)
	if err != nil {
		s.logger.Error("Failed to fetch user with email: %s, %v", email, err)
		if errors.Is(err, irepository.ErrUserNotFound) {
			return nil, ErrUserNotFound
		}
		return nil, errors.New("failed to fetch user")
	}

	if user == nil {
		s.logger.Error("User not found with email: %s", email)
		return nil, errors.New("user not found")
	}

	return user, nil
}

func (s *userServiceImpl) GetUserByResetToken(token string) (*models.User, error) {
	user, err := s.userRepo.FindByResetToken(token)
	if err != nil {
		s.logger.Error("Failed to fetch user with reset token: %s, %v", token, err)
		if errors.Is(err, irepository.ErrUserNotFound) {
			return nil, ErrUserNotFound
		}
		return nil, errors.New("failed to fetch user")
	}

	if user == nil {
		s.logger.Error("User not found with reset token: %s", token)
		return nil, errors.New("user not found")
	}

	return user, nil
}
