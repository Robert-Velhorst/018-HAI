package users

import (
	"automation-hub-idp/internal/app/models"
	"automation-hub-idp/internal/app/repositories/irepository"
	"automation-hub-idp/internal/app/utils"
	"errors"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"testing"
)

func TestCreateUser_Success(t *testing.T) {
	// Arrange
	mockRepo := new(MockUserRepository)
	mockLogger := new(MockLogger)
	hasher := new(MockPasswordHasher)
	email := "test@example.com"
	user := models.User{Email: email, Password: "test123"}

	mockError := irepository.ErrUserNotFound
	mockRepo.On("FindByEmail", email).Return(nil, mockError)

	expectedUser := user
	mockRepo.On("Create", &expectedUser).Return(&expectedUser, nil)

	service := NewUserService(mockRepo, mockLogger, hasher)

	// Act
	result, err := service.CreateUser(user)

	// Assert
	assert.Nil(t, err)
	assert.Equal(t, user.Password, result.Password)
	assert.Equal(t, user.Email, result.Email)
	mockRepo.AssertExpectations(t)
}

func TestCreateUser_RejectsDuplicateEmail(t *testing.T) {
	mockRepo := new(MockUserRepository)
	mockLogger := new(MockLogger)
	user := models.User{Email: "existing@example.com", Password: "hashed"}
	existingUser := user
	mockRepo.On("FindByEmail", user.Email).Return(&existingUser, nil)
	mockLogger.On("Error", "User already exists with email: %s", mock.Anything).Return()

	service := NewUserService(mockRepo, mockLogger, nil)
	result, err := service.CreateUser(user)

	assert.Nil(t, result)
	assert.ErrorIs(t, err, ErrUserAlreadyExists)
	mockRepo.AssertNotCalled(t, "Create", mock.Anything)
	mockRepo.AssertExpectations(t)
	mockLogger.AssertExpectations(t)
}

func TestCreateUser_MapsDuplicateRepositoryError(t *testing.T) {
	mockRepo := new(MockUserRepository)
	user := models.User{Email: "existing@example.com", Password: "hashed"}
	mockRepo.On("FindByEmail", user.Email).Return(nil, irepository.ErrUserNotFound)
	mockRepo.On("Create", mock.AnythingOfType("*models.User")).Return((*models.User)(nil), irepository.ErrDuplicateUser)

	service := NewUserService(mockRepo, nil, nil)
	result, err := service.CreateUser(user)

	assert.Nil(t, result)
	assert.ErrorIs(t, err, ErrUserAlreadyExists)
	mockRepo.AssertExpectations(t)
}

func TestCreateUserFailsClosedWhenDuplicateLookupFails(t *testing.T) {
	mockRepo := new(MockUserRepository)
	mockLogger := new(MockLogger)
	user := models.User{Email: "operator@example.com", Password: "hashed"}
	lookupErr := errors.New("database unavailable")
	mockRepo.On("FindByEmail", user.Email).Return((*models.User)(nil), lookupErr)
	mockLogger.On(
		"Error",
		"Failed to check existing user with email: %s, %v",
		mock.MatchedBy(func(args []interface{}) bool {
			return len(args) == 2 && args[0] == user.Email && args[1] == lookupErr
		}),
	).Return()

	service := NewUserService(mockRepo, mockLogger, nil)
	result, err := service.CreateUser(user)

	assert.Nil(t, result)
	assert.EqualError(t, err, "failed to check existing user")
	mockRepo.AssertNotCalled(t, "Create", mock.Anything)
	mockRepo.AssertExpectations(t)
	mockLogger.AssertExpectations(t)
}

func TestGetUserByID(t *testing.T) {
	// Arrange
	mockRepo := new(MockUserRepository)
	mockLogger := new(MockLogger)
	hasher := new(MockPasswordHasher)
	id := uuid.New()
	user := models.User{ID: id, Email: "test@example.com"}

	mockRepo.On("FindByID", id).Return(&user, nil)

	service := NewUserService(mockRepo, mockLogger, hasher)

	// Act
	result, err := service.GetUserByID(id)

	// Assert
	assert.Nil(t, err)
	assert.Equal(t, &user, result)
	mockRepo.AssertExpectations(t)
}

func TestGetAllUsers_WithDefaultPagination(t *testing.T) {
	// Arrange
	mockRepo := new(MockUserRepository)
	users := []*models.User{
		{Email: "test1@example.com"},
		{Email: "test2@example.com"},
	}

	defaultPagination := utils.DefaultPagination()
	mockRepo.On("FindAll", defaultPagination).Return(users, nil)

	service := NewUserService(mockRepo, nil, nil)

	// Act
	result, err := service.GetAllUsers(nil)

	// Assert
	assert.Nil(t, err)
	assert.Equal(t, users, result)
	mockRepo.AssertExpectations(t)
}

func TestUpdateUser_Success(t *testing.T) {
	// Arrange
	mockRepo := new(MockUserRepository)
	userID := uuid.New()
	existingUser := &models.User{ID: userID, Email: "existing@example.com", Password: "hashedPassword"}
	newUser := models.User{ID: userID, Email: "new@example.com", Password: "hashedPassword"}

	mockRepo.On("FindByID", userID).Return(existingUser, nil)
	mockRepo.On("FindByEmail", "new@example.com").Return(nil, errors.New("not found"))
	updatedUser := newUser
	updatedUser.Password = "hashedPassword"
	mockRepo.On("Update", &updatedUser).Return(&updatedUser, nil)

	service := NewUserService(mockRepo, nil, nil)

	// Act
	result, err := service.UpdateUser(newUser)

	// Assert
	assert.Nil(t, err)
	assert.Equal(t, &newUser, result)
	assert.Equal(t, "hashedPassword", result.Password)
	mockRepo.AssertExpectations(t)
}

func TestUpdateUser_PasswordNotChanged(t *testing.T) {
	// Arrange
	mockRepo := new(MockUserRepository)
	userID := uuid.New()
	existingPassword := "hashedPassword"
	existingUser := &models.User{ID: userID, Email: "existing@example.com", Password: existingPassword}
	newUser := models.User{ID: userID, Email: "new@example.com", Password: "newHashedPassword"}

	mockRepo.On("FindByID", userID).Return(existingUser, nil)
	mockRepo.On("FindByEmail", "new@example.com").Return(nil, errors.New("not found"))
	var updatedUserToReturn *models.User

	mockRepo.On("Update", mock.AnythingOfType("*models.User")).Run(func(args mock.Arguments) {
		updatedUser := args.Get(0).(*models.User)
		assert.Equal(t, existingPassword, updatedUser.Password)
		updatedUserToReturn = updatedUser
	}).Return(func(args mock.Arguments) (*models.User, error) {
		return updatedUserToReturn, nil
	})
	mockLogger := new(MockLogger)
	hasher := new(MockPasswordHasher)

	hasher.On("Hash", mock.AnythingOfType("string")).Return(existingPassword, nil)
	mockLogger.On("Error", "Error deleting user with ID: %s, %v", mock.MatchedBy(func(args []interface{}) bool {
		// You can add further conditions to verify the contents of the slice if necessary.
		return true
	})).Return()

	service := NewUserService(mockRepo, mockLogger, hasher)

	// Act
	result, err := service.UpdateUser(newUser)

	// Assert
	assert.Nil(t, err)
	newUser.Password = existingPassword
	assert.Equal(t, &newUser, result)

	assert.Equal(t, existingPassword, result.Password) // This ensures the password did not change
	mockRepo.AssertExpectations(t)
}

func TestDeleteUser_Success(t *testing.T) {
	// Arrange
	mockRepo := new(MockUserRepository)
	mockLogger := new(MockLogger)
	id := uuid.New()

	mockRepo.On("Delete", id).Return(nil)

	service := NewUserService(mockRepo, mockLogger, nil)

	// Act
	err := service.DeleteUser(id)

	// Assert
	assert.Nil(t, err)
	mockRepo.AssertExpectations(t)
}

func TestGetUserByEmail_Success(t *testing.T) {
	// Arrange
	mockRepo := new(MockUserRepository)
	email := "test@example.com"

	user := &models.User{
		Email: email,
	}

	mockRepo.On("FindByEmail", email).Return(user, nil)

	service := NewUserService(mockRepo, nil, nil)

	// Act
	result, err := service.GetUserByEmail(email)

	// Assert
	assert.Nil(t, err)
	assert.Equal(t, email, result.Email)
	mockRepo.AssertExpectations(t)
}

func TestGetUserByEmail_RepoError(t *testing.T) {
	// Arrange
	mockRepo := new(MockUserRepository)
	mockLogger := new(MockLogger)
	email := "test@example.com"

	mockRepo.On("FindByEmail", email).Return(nil, errors.New("database error"))
	mockLogger.On("Error", mock.Anything, mock.Anything).Return()
	service := NewUserService(mockRepo, mockLogger, nil)

	// Act
	result, err := service.GetUserByEmail(email)

	// Assert
	assert.Nil(t, result)
	assert.NotNil(t, err)
	assert.Equal(t, "failed to fetch user", err.Error())
	mockRepo.AssertExpectations(t)
}

func TestGetUserByEmailPreservesNotFoundClassification(t *testing.T) {
	mockRepo := new(MockUserRepository)
	mockLogger := new(MockLogger)
	email := "missing@example.com"
	mockRepo.On("FindByEmail", email).Return((*models.User)(nil), irepository.ErrUserNotFound)
	mockLogger.On("Error", mock.Anything, mock.Anything).Return()
	service := NewUserService(mockRepo, mockLogger, nil)

	result, err := service.GetUserByEmail(email)

	assert.Nil(t, result)
	assert.ErrorIs(t, err, ErrUserNotFound)
	mockRepo.AssertExpectations(t)
	mockLogger.AssertExpectations(t)
}

func TestCompletePasswordResetHashesAndAtomicallyConsumesToken(t *testing.T) {
	mockRepo := new(MockUserRepository)
	hasher := new(MockPasswordHasher)
	hasher.On("Hash", "new-password").Return("new-password-hash", nil)
	mockRepo.On("ConsumePasswordReset", "one-time-token", "new-password-hash").Return(nil)
	service := NewUserService(mockRepo, nil, hasher)

	err := service.CompletePasswordReset("one-time-token", "new-password")

	assert.NoError(t, err)
	hasher.AssertExpectations(t)
	mockRepo.AssertExpectations(t)
}

func TestCompletePasswordResetPreservesInvalidTokenClassification(t *testing.T) {
	mockRepo := new(MockUserRepository)
	hasher := new(MockPasswordHasher)
	hasher.On("Hash", "new-password").Return("new-password-hash", nil)
	mockRepo.On("ConsumePasswordReset", "expired-token", "new-password-hash").Return(irepository.ErrInvalidResetToken)
	service := NewUserService(mockRepo, nil, hasher)

	err := service.CompletePasswordReset("expired-token", "new-password")

	assert.ErrorIs(t, err, ErrInvalidResetToken)
	hasher.AssertExpectations(t)
	mockRepo.AssertExpectations(t)
}
