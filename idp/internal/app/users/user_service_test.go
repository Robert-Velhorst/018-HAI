package users

import (
	"automation-hub-idp/internal/app/models"
	"automation-hub-idp/internal/app/repositories/irepository"
	"automation-hub-idp/internal/app/utils"
	"errors"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"strings"
	"testing"
	"time"
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

func TestUpdateUserNormalizesEmailForLoginCompatibility(t *testing.T) {
	repo := new(MockUserRepository)
	userID := uuid.New()
	current := &models.User{ID: userID, Email: "old@example.com", Password: "hashed-password"}
	repo.On("FindByID", userID).Return(current, nil).Once()
	repo.On("FindByEmail", "new.user@example.com").Return(nil, errors.New("not found")).Once()
	repo.On("Update", mock.MatchedBy(func(user *models.User) bool {
		return user.Email == "new.user@example.com" && user.Password == "hashed-password"
	})).Return(&models.User{ID: userID, Email: "new.user@example.com", Password: "hashed-password"}, nil).Once()
	service := NewUserService(repo, nil, nil)

	updated, err := service.UpdateUser(models.User{
		ID: userID, Email: " New.User@Example.com ", SessionVersion: current.SessionVersion,
	})

	assert.NoError(t, err)
	assert.Equal(t, "new.user@example.com", updated.Email)
	repo.AssertExpectations(t)
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

func TestUpdateUserRejectsStaleSessionInsteadOfRefreshingItsConcurrencyToken(t *testing.T) {
	repo := new(MockUserRepository)
	userID := uuid.New()
	repo.On("FindByID", userID).Return(&models.User{ID: userID, Email: "current@example.com", SessionVersion: 5}, nil).Once()
	service := NewUserService(repo, nil, nil)
	stale := models.User{ID: userID, Email: "stale@example.com", SessionVersion: 4}

	_, err := service.UpdateUser(stale)

	assert.ErrorIs(t, err, ErrConcurrentUserUpdate)
	repo.AssertNotCalled(t, "Update", mock.Anything)
	repo.AssertExpectations(t)
}

func TestUpdateUserDoesNotPanicWhenLoggingIsUnavailable(t *testing.T) {
	t.Run("lookup failure", func(t *testing.T) {
		repo := new(MockUserRepository)
		userID := uuid.New()
		repo.On("FindByID", userID).Return((*models.User)(nil), errors.New("database unavailable")).Once()
		service := NewUserService(repo, nil, nil)

		_, err := service.UpdateUser(models.User{ID: userID})

		assert.EqualError(t, err, "error fetching user by ID")
		repo.AssertExpectations(t)
	})

	t.Run("duplicate email", func(t *testing.T) {
		repo := new(MockUserRepository)
		userID := uuid.New()
		current := &models.User{ID: userID, Email: "old@example.com"}
		duplicate := &models.User{ID: uuid.New(), Email: "taken@example.com"}
		repo.On("FindByID", userID).Return(current, nil).Once()
		repo.On("FindByEmail", duplicate.Email).Return(duplicate, nil).Once()
		service := NewUserService(repo, nil, nil)

		_, err := service.UpdateUser(models.User{ID: userID, Email: duplicate.Email})

		assert.EqualError(t, err, "email already exists")
		repo.AssertExpectations(t)
	})
}

func TestUpdatePasswordRejectsPasswordsOutsidePolicyBeforeHashing(t *testing.T) {
	tests := []struct {
		name     string
		password string
		wantErr  error
	}{
		{name: "too short", password: "weak-pass", wantErr: utils.ErrPasswordTooShort},
		{name: "too long for bcrypt", password: strings.Repeat("a", utils.MaximumPasswordBytes+1), wantErr: utils.ErrPasswordTooLong},
	}

	operations := []struct {
		name string
		run  func(UserAccountUpdateService, uuid.UUID, string) error
	}{
		{name: "authenticated password change", run: func(service UserAccountUpdateService, id uuid.UUID, password string) error {
			return service.UpdatePasswordWithCurrentPassword(id, 0, "current-password", password)
		}},
		{name: "password reset consumption", run: func(service UserAccountUpdateService, id uuid.UUID, password string) error {
			return service.ConsumePasswordResetToken(id, "reset-token", password)
		}},
	}

	for _, operation := range operations {
		for _, test := range tests {
			t.Run(operation.name+"/"+test.name, func(t *testing.T) {
				repo := new(MockUserRepository)
				hasher := new(MockPasswordHasher)
				id := uuid.New()
				hasher.On("Hash", test.password).Return("hashed-password", nil).Maybe()
				repo.On("UpdatePassword", id, int64(0), "hashed-password", mock.AnythingOfType("time.Time")).Return(nil).Maybe()
				repo.On("ConsumePasswordResetToken", id, "reset-token", "hashed-password", mock.AnythingOfType("time.Time")).Return(nil).Maybe()
				service := NewUserService(repo, nil, hasher)

				err := operation.run(service, id, test.password)

				assert.ErrorIs(t, err, test.wantErr)
				hasher.AssertNotCalled(t, "Hash", test.password)
				repo.AssertNotCalled(t, "UpdatePassword", id, int64(0), mock.Anything, mock.Anything)
				repo.AssertNotCalled(t, "ConsumePasswordResetToken", id, "reset-token", mock.Anything, mock.Anything)
			})
		}
	}
}

func TestUpdatePasswordWithCurrentPassword(t *testing.T) {
	tests := []struct {
		name                string
		currentPassword     string
		compareErr          error
		wantErr             error
		wantMessage         string
		wantPasswordPersist bool
	}{
		{name: "correct current password", currentPassword: "correct-current-password", wantPasswordPersist: true},
		{name: "wrong current password", currentPassword: "wrong-current-password", compareErr: utils.ErrPasswordMismatch, wantErr: ErrInvalidCurrentPassword},
		{name: "stored hash verification failure", currentPassword: "current-password", compareErr: errors.New("malformed stored hash"), wantMessage: "error verifying current password"},
		{name: "missing current password", wantErr: ErrCurrentPasswordRequired},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repo := new(MockUserRepository)
			hasher := new(MockPasswordHasher)
			userID := uuid.New()
			const sessionVersion = int64(7)
			service := NewUserService(repo, nil, hasher)

			if test.currentPassword != "" {
				repo.On("FindByID", userID).Return(&models.User{ID: userID, Password: "existing-hash"}, nil).Once()
				hasher.On("Compare", "existing-hash", test.currentPassword).Return(test.compareErr).Once()
			}
			if test.wantPasswordPersist {
				hasher.On("Hash", "new-strong-password").Return("new-password-hash", nil).Once()
				repo.On("UpdatePassword", userID, sessionVersion, "new-password-hash", mock.AnythingOfType("time.Time")).Return(nil).Once()
			}

			err := service.UpdatePasswordWithCurrentPassword(userID, sessionVersion, test.currentPassword, "new-strong-password")

			if test.wantErr != nil {
				assert.ErrorIs(t, err, test.wantErr)
			} else if test.wantMessage != "" {
				assert.EqualError(t, err, test.wantMessage)
			} else {
				assert.NoError(t, err)
			}
			if !test.wantPasswordPersist {
				hasher.AssertNotCalled(t, "Hash", "new-strong-password")
				repo.AssertNotCalled(t, "UpdatePassword", userID, sessionVersion, mock.Anything, mock.Anything)
			}
			repo.AssertExpectations(t)
			hasher.AssertExpectations(t)
		})
	}
}

func TestUpdateAccountKeepsOmittedEmailAndCommitsPasswordAsOneUpdate(t *testing.T) {
	repo := new(MockUserRepository)
	hasher := new(MockPasswordHasher)
	userID := uuid.New()
	expiresAt := time.Now().Add(time.Hour)
	current := &models.User{
		ID: userID, Email: "owner@example.com", Password: "old-hash", SessionVersion: 4,
		ResetPasswordToken: "pending-reset", ResetTokenExpires: &expiresAt,
	}
	repo.On("FindByID", userID).Return(current, nil).Once()
	hasher.On("Compare", "old-hash", "current-password").Return(nil).Once()
	hasher.On("Hash", "new-strong-password").Return("new-hash", nil).Once()
	repo.On("UpdateAccount", userID, int64(4), (*string)(nil), "new-hash", mock.AnythingOfType("time.Time")).Return(nil).Once()
	service := NewUserService(repo, nil, hasher)

	updated, err := service.UpdateAccount(userID, 4, "current-password", "", "new-strong-password")

	assert.NoError(t, err)
	assert.Equal(t, "owner@example.com", updated.Email)
	assert.Equal(t, "new-hash", updated.Password)
	assert.Equal(t, int64(5), updated.SessionVersion)
	assert.Empty(t, updated.ResetPasswordToken)
	assert.Nil(t, updated.ResetTokenExpires)
	repo.AssertExpectations(t)
	hasher.AssertExpectations(t)
}

func TestUpdateAccountAppliesEmailAndPasswordInOneConditionalWrite(t *testing.T) {
	repo := new(MockUserRepository)
	hasher := new(MockPasswordHasher)
	userID := uuid.New()
	current := &models.User{ID: userID, Email: "old@example.com", Password: "old-hash", SessionVersion: 2}
	repo.On("FindByID", userID).Return(current, nil).Once()
	hasher.On("Compare", "old-hash", "current-password").Return(nil).Once()
	hasher.On("Hash", "new-strong-password").Return("new-hash", nil).Once()
	repo.On("UpdateAccount", userID, int64(2), mock.MatchedBy(func(email *string) bool { return email != nil && *email == "new@example.com" }), "new-hash", mock.AnythingOfType("time.Time")).Return(nil).Once()
	service := NewUserService(repo, nil, hasher)

	updated, err := service.UpdateAccount(userID, 2, "current-password", " new@example.com ", "new-strong-password")

	assert.NoError(t, err)
	assert.Equal(t, "new@example.com", updated.Email)
	assert.Equal(t, int64(3), updated.SessionVersion)
	repo.AssertExpectations(t)
	hasher.AssertExpectations(t)
}

func TestUpdateAccountNormalizesEmailForLoginCompatibility(t *testing.T) {
	repo := new(MockUserRepository)
	hasher := new(MockPasswordHasher)
	userID := uuid.New()
	current := &models.User{ID: userID, Email: "old@example.com", Password: "old-hash", SessionVersion: 2}
	repo.On("FindByID", userID).Return(current, nil).Once()
	hasher.On("Compare", "old-hash", "current-password").Return(nil).Once()
	repo.On("UpdateAccount", userID, int64(2), mock.MatchedBy(func(email *string) bool {
		return email != nil && *email == "new.user@example.com"
	}), "", mock.AnythingOfType("time.Time")).Return(nil).Once()
	service := NewUserService(repo, nil, hasher)

	updated, err := service.UpdateAccount(userID, 2, "current-password", " New.User@Example.com ", "")

	assert.NoError(t, err)
	assert.Equal(t, "new.user@example.com", updated.Email)
	assert.Equal(t, int64(3), updated.SessionVersion)
	repo.AssertExpectations(t)
	hasher.AssertExpectations(t)
}

func TestUpdateAccountRequiresCurrentPasswordForEmailChange(t *testing.T) {
	repo := new(MockUserRepository)
	hasher := new(MockPasswordHasher)
	userID := uuid.New()
	current := &models.User{ID: userID, Email: "old@example.com", Password: "old-hash", SessionVersion: 2}
	repo.On("FindByID", userID).Return(current, nil).Once()
	service := NewUserService(repo, nil, hasher)

	_, err := service.UpdateAccount(userID, 2, "", "new@example.com", "")

	assert.ErrorIs(t, err, ErrCurrentPasswordRequired)
	hasher.AssertNotCalled(t, "Compare", mock.Anything, mock.Anything)
	repo.AssertNotCalled(t, "UpdateAccount", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	repo.AssertExpectations(t)
}

func TestUpdateAccountRejectsInvalidEmailBeforeReauthentication(t *testing.T) {
	repo := new(MockUserRepository)
	hasher := new(MockPasswordHasher)
	userID := uuid.New()
	repo.On("FindByID", userID).Return(&models.User{ID: userID, Email: "old@example.com", SessionVersion: 2}, nil).Once()
	service := NewUserService(repo, nil, hasher)

	_, err := service.UpdateAccount(userID, 2, "", "not-an-email", "")

	assert.ErrorIs(t, err, ErrInvalidEmail)
	hasher.AssertNotCalled(t, "Compare", mock.Anything, mock.Anything)
	repo.AssertNotCalled(t, "UpdateAccount", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	repo.AssertExpectations(t)
}

func TestUpdateAccountRequiresCurrentPasswordOnlyForActualChanges(t *testing.T) {
	repo := new(MockUserRepository)
	hasher := new(MockPasswordHasher)
	userID := uuid.New()
	current := &models.User{ID: userID, Email: "same@example.com", Password: "old-hash", SessionVersion: 2}
	repo.On("FindByID", userID).Return(current, nil).Once()
	service := NewUserService(repo, nil, hasher)

	updated, err := service.UpdateAccount(userID, 2, "", " same@example.com ", "")

	assert.NoError(t, err)
	assert.Same(t, current, updated)
	hasher.AssertNotCalled(t, "Compare", mock.Anything, mock.Anything)
	repo.AssertNotCalled(t, "UpdateAccount", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	repo.AssertExpectations(t)
}

func TestUpdateAccountVerifiesCurrentPasswordForEmailOnlyChange(t *testing.T) {
	repo := new(MockUserRepository)
	hasher := new(MockPasswordHasher)
	userID := uuid.New()
	expiresAt := time.Now().Add(time.Hour)
	current := &models.User{
		ID: userID, Email: "old@example.com", Password: "old-hash", SessionVersion: 2,
		ResetPasswordToken: "pending-token", ResetTokenExpires: &expiresAt,
	}
	repo.On("FindByID", userID).Return(current, nil).Once()
	hasher.On("Compare", "old-hash", "current-password").Return(nil).Once()
	repo.On("UpdateAccount", userID, int64(2), mock.MatchedBy(func(email *string) bool { return email != nil && *email == "new@example.com" }), "", mock.AnythingOfType("time.Time")).Return(nil).Once()
	service := NewUserService(repo, nil, hasher)

	updated, err := service.UpdateAccount(userID, 2, "current-password", " new@example.com ", "")

	assert.NoError(t, err)
	assert.Equal(t, "new@example.com", updated.Email)
	assert.Equal(t, int64(3), updated.SessionVersion)
	assert.Empty(t, updated.ResetPasswordToken)
	assert.Nil(t, updated.ResetTokenExpires)
	repo.AssertExpectations(t)
	hasher.AssertExpectations(t)
}

func TestUpdateAccountRejectsIncorrectCurrentPasswordBeforeEmailChange(t *testing.T) {
	repo := new(MockUserRepository)
	hasher := new(MockPasswordHasher)
	userID := uuid.New()
	current := &models.User{ID: userID, Email: "old@example.com", Password: "old-hash", SessionVersion: 2}
	repo.On("FindByID", userID).Return(current, nil).Once()
	hasher.On("Compare", "old-hash", "incorrect").Return(utils.ErrPasswordMismatch).Once()
	service := NewUserService(repo, nil, hasher)

	_, err := service.UpdateAccount(userID, 2, "incorrect", "new@example.com", "")

	assert.ErrorIs(t, err, ErrInvalidCurrentPassword)
	repo.AssertNotCalled(t, "UpdateAccount", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	repo.AssertExpectations(t)
	hasher.AssertExpectations(t)
}

func TestUpdateAccountRejectsStaleSessionBeforePasswordVerification(t *testing.T) {
	repo := new(MockUserRepository)
	hasher := new(MockPasswordHasher)
	userID := uuid.New()
	repo.On("FindByID", userID).Return(&models.User{ID: userID, SessionVersion: 5}, nil).Once()
	service := NewUserService(repo, nil, hasher)

	_, err := service.UpdateAccount(userID, 4, "current-password", "new@example.com", "new-strong-password")

	assert.ErrorIs(t, err, ErrConcurrentUserUpdate)
	hasher.AssertNotCalled(t, "Compare", mock.Anything, mock.Anything)
	hasher.AssertNotCalled(t, "Hash", mock.Anything)
	repo.AssertNotCalled(t, "UpdateAccount", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	repo.AssertExpectations(t)
}

func TestUpdateAccountMapsDuplicateEmailWithoutApplyingPartialChanges(t *testing.T) {
	repo := new(MockUserRepository)
	hasher := new(MockPasswordHasher)
	userID := uuid.New()
	current := &models.User{ID: userID, Email: "old@example.com", Password: "old-hash", SessionVersion: 2}
	repo.On("FindByID", userID).Return(current, nil).Once()
	hasher.On("Compare", "old-hash", "current-password").Return(nil).Once()
	hasher.On("Hash", "new-strong-password").Return("new-hash", nil).Once()
	repo.On("UpdateAccount", userID, int64(2), mock.MatchedBy(func(email *string) bool { return email != nil && *email == "taken@example.com" }), "new-hash", mock.AnythingOfType("time.Time")).Return(irepository.ErrDuplicateUser).Once()
	service := NewUserService(repo, nil, hasher)

	_, err := service.UpdateAccount(userID, 2, "current-password", "taken@example.com", "new-strong-password")

	assert.ErrorIs(t, err, ErrUserAlreadyExists)
	repo.AssertExpectations(t)
	hasher.AssertExpectations(t)
}

func TestStorePasswordResetTokenRequiresObservedSessionVersion(t *testing.T) {
	t.Run("stores against matching version", func(t *testing.T) {
		repo := new(MockUserRepository)
		userID := uuid.New()
		digest := utils.PasswordResetTokenDigest("reset-token")
		expiresAt := time.Now().Add(time.Hour)
		repo.On("SetPasswordResetToken", userID, int64(7), digest, expiresAt.UTC(), mock.AnythingOfType("time.Time")).Return(nil).Once()
		service := NewUserService(repo, nil, nil)

		err := service.StorePasswordResetToken(userID, 7, digest, expiresAt)

		assert.NoError(t, err)
		repo.AssertExpectations(t)
	})

	t.Run("rejects a stale version", func(t *testing.T) {
		repo := new(MockUserRepository)
		userID := uuid.New()
		digest := utils.PasswordResetTokenDigest("reset-token")
		expiresAt := time.Now().Add(time.Hour)
		repo.On("SetPasswordResetToken", userID, int64(6), digest, expiresAt.UTC(), mock.AnythingOfType("time.Time")).Return(irepository.ErrConcurrentUserUpdate).Once()
		service := NewUserService(repo, nil, nil)

		err := service.StorePasswordResetToken(userID, 6, digest, expiresAt)

		assert.ErrorIs(t, err, ErrConcurrentUserUpdate)
		repo.AssertExpectations(t)
	})
}

func TestStorePasswordResetTokenRejectsMalformedDigestBeforePersistence(t *testing.T) {
	repo := new(MockUserRepository)
	userID := uuid.New()
	service := NewUserService(repo, nil, nil)

	err := service.StorePasswordResetToken(userID, 3, strings.Repeat("z", 64), time.Now().Add(time.Hour))

	assert.EqualError(t, err, "invalid password reset token record")
	repo.AssertNotCalled(t, "SetPasswordResetToken", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

func TestConsumePasswordResetTokenDoesNotRequireCurrentPassword(t *testing.T) {
	repo := new(MockUserRepository)
	hasher := new(MockPasswordHasher)
	userID := uuid.New()
	hasher.On("Hash", "new-password-for-reset").Return("reset-password-hash", nil).Once()
	repo.On("ConsumePasswordResetToken", userID, "valid-reset-token", "reset-password-hash", mock.AnythingOfType("time.Time")).Return(nil).Once()
	service := NewUserService(repo, nil, hasher)

	err := service.ConsumePasswordResetToken(userID, "valid-reset-token", "new-password-for-reset")

	assert.NoError(t, err)
	hasher.AssertExpectations(t)
	repo.AssertExpectations(t)
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
