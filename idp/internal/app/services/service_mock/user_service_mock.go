package service_mock

import (
	"automation-hub-idp/internal/app/models"
	"automation-hub-idp/internal/app/utils"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"time"
)

type MockUserService struct {
	mock.Mock
}

func (m *MockUserService) CreateUser(user models.User) (*models.User, error) {
	args := m.Called(user)
	return args.Get(0).(*models.User), args.Error(1)
}

func (m *MockUserService) GetUserByID(id uuid.UUID) (*models.User, error) {
	args := m.Called(id)
	return args.Get(0).(*models.User), args.Error(1)
}

func (m *MockUserService) GetUserByEmail(email string) (*models.User, error) {
	args := m.Called(email)
	return args.Get(0).(*models.User), args.Error(1)
}

func (m *MockUserService) GetUserByResetToken(token string) (*models.User, error) {
	args := m.Called(token)
	return args.Get(0).(*models.User), args.Error(1)
}

func (m *MockUserService) StorePasswordResetToken(id uuid.UUID, expectedSessionVersion int64, tokenDigest string, expiresAt time.Time) error {
	return m.Called(id, expectedSessionVersion, tokenDigest, expiresAt).Error(0)
}

func (m *MockUserService) CompleteSuccessfulLogin(expected models.User) (*models.User, error) {
	args := m.Called(expected)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.User), args.Error(1)
}

func (m *MockUserService) UpdateUser(user models.User) (*models.User, error) {
	args := m.Called(user)
	return args.Get(0).(*models.User), args.Error(1)
}

func (m *MockUserService) DeleteUser(id uuid.UUID) error {
	args := m.Called(id)
	return args.Error(0)
}

func (m *MockUserService) GetAllUsers(p *utils.Pagination) ([]*models.User, error) {
	args := m.Called(p)
	return args.Get(0).([]*models.User), args.Error(1)
}

func (m *MockUserService) ResetPassword(email string, opts ...utils.PasswordResetOptions) error {
	args := m.Called(email, opts)
	return args.Error(0)
}

func (m *MockUserService) UpdatePassword(id uuid.UUID, expectedSessionVersion int64, newPassword string) error {
	args := m.Called(id, expectedSessionVersion, newPassword)
	return args.Error(0)
}

func (m *MockUserService) ConsumePasswordResetToken(id uuid.UUID, token, newPassword string) error {
	args := m.Called(id, token, newPassword)
	return args.Error(0)
}

func (m *MockUserService) ClearPasswordResetToken(id uuid.UUID, token string) error {
	args := m.Called(id, token)
	return args.Error(0)
}

func (m *MockUserService) VerifyPasswordResetToken(token string) error {
	args := m.Called(token)
	return args.Error(0)
}

func (m *MockUserService) BlockUser(id uuid.UUID) error {
	args := m.Called(id)
	return args.Error(0)
}

func (m *MockUserService) UnblockUser(id uuid.UUID) error {
	args := m.Called(id)
	return args.Error(0)
}
