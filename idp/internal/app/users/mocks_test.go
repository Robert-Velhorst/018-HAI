package users

import (
	"automation-hub-idp/internal/app/models"
	"automation-hub-idp/internal/app/utils"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"time"
)

type MockLogger struct {
	mock.Mock
}

func (m *MockLogger) Info(message string, args ...interface{}) {
	m.Called(message, args)
}

func (m *MockLogger) Error(format string, args ...interface{}) {
	_ = m.Called(format, args)
}

func (m *MockLogger) Warn(message string, args ...interface{}) {
	m.Called(message, args)
}

func (m *MockLogger) Debug(message string, args ...interface{}) {
	m.Called(message, args)
}

type MockUserRepository struct {
	mock.Mock
}

func (m *MockUserRepository) FindByID(id uuid.UUID) (*models.User, error) {
	args := m.Called(id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.User), args.Error(1)
}

func (m *MockUserRepository) FindByEmail(email string) (*models.User, error) {
	args := m.Called(email)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.User), args.Error(1)
}

func (m *MockUserRepository) Create(user *models.User) (*models.User, error) {
	args := m.Called(user)
	return args.Get(0).(*models.User), args.Error(1)
}

func (m *MockUserRepository) CompleteSuccessfulLogin(expected models.User, now time.Time) (*models.User, error) {
	args := m.Called(expected, now)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.User), args.Error(1)
}

func (m *MockUserRepository) Update(user *models.User) (*models.User, error) {
	args := m.Called(user)
	if fn, ok := args.Get(0).(func(mock.Arguments) (*models.User, error)); ok {
		return fn(args)
	}
	return args.Get(0).(*models.User), args.Error(1)
}

func (m *MockUserRepository) Delete(id uuid.UUID) error {
	args := m.Called(id)
	return args.Error(0)
}

func (m *MockUserRepository) FindAll(p utils.Pagination) ([]*models.User, error) {
	args := m.Called(p)
	return args.Get(0).([]*models.User), args.Error(1)
}

func (m *MockUserRepository) FindByResetToken(token string) (*models.User, error) {
	args := m.Called(token)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.User), args.Error(1)
}

func (m *MockUserRepository) SetPasswordResetToken(id uuid.UUID, expectedSessionVersion int64, tokenDigest string, expiresAt, now time.Time) error {
	return m.Called(id, expectedSessionVersion, tokenDigest, expiresAt, now).Error(0)
}

func (m *MockUserRepository) UpdatePassword(id uuid.UUID, expectedSessionVersion int64, passwordHash string, now time.Time) error {
	return m.Called(id, expectedSessionVersion, passwordHash, now).Error(0)
}

func (m *MockUserRepository) UpdateAccount(id uuid.UUID, expectedSessionVersion int64, email *string, passwordHash string, now time.Time) error {
	return m.Called(id, expectedSessionVersion, email, passwordHash, now).Error(0)
}

func (m *MockUserRepository) ConsumePasswordResetToken(id uuid.UUID, token, passwordHash string, now time.Time) error {
	return m.Called(id, token, passwordHash, now).Error(0)
}

func (m *MockUserRepository) ClearPasswordResetToken(id uuid.UUID, token string) error {
	return m.Called(id, token).Error(0)
}

type MockPasswordHasher struct {
	mock.Mock
}

func (m *MockPasswordHasher) Hash(password string) (string, error) {
	args := m.Called(password)
	return args.String(0), args.Error(1)
}

func (m *MockPasswordHasher) Compare(hashedPassword, password string) error {
	args := m.Called(hashedPassword, password)
	return args.Error(0)
}
