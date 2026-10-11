package repositories

import (
	"automation-hub-idp/internal/app/models"
	"automation-hub-idp/internal/app/repositories/irepository"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestGormUserRepositoryConsumesPasswordResetAtomically(t *testing.T) {
	dsn := os.Getenv("IDP_INTEGRATION_DATABASE_DSN")
	if dsn == "" {
		t.Skip("IDP_INTEGRATION_DATABASE_DSN is not configured")
	}

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	defer sqlDB.Close()

	tx := db.Begin()
	require.NoError(t, tx.Error)
	defer tx.Rollback()

	expiresAt := time.Now().UTC().Add(10 * time.Minute)
	user := models.User{
		ID:                 uuid.New(),
		Email:              "reset-" + uuid.NewString() + "@example.test",
		Password:           "old-password-hash",
		FirstAccess:        true,
		ResetPasswordToken: "single-use-token-" + uuid.NewString(),
		ResetTokenExpires:  &expiresAt,
		IsActive:           true,
	}
	require.NoError(t, tx.Create(&user).Error)

	repository := &GormUserRepository{DB: tx, logger: integrationTestLogger{}}
	require.NoError(t, repository.ConsumePasswordReset(user.ResetPasswordToken, "new-password-hash"))

	var stored models.User
	require.NoError(t, tx.First(&stored, "id = ?", user.ID).Error)
	require.Equal(t, "new-password-hash", stored.Password)
	require.False(t, stored.FirstAccess)
	require.Empty(t, stored.ResetPasswordToken)
	require.Nil(t, stored.ResetTokenExpires)

	err = repository.ConsumePasswordReset(user.ResetPasswordToken, "replayed-password-hash")
	require.ErrorIs(t, err, irepository.ErrInvalidResetToken)
	require.NoError(t, tx.First(&stored, "id = ?", user.ID).Error)
	require.Equal(t, "new-password-hash", stored.Password)
}

func TestGormUserRepositoryRejectsExpiredPasswordResetWithoutMutation(t *testing.T) {
	dsn := os.Getenv("IDP_INTEGRATION_DATABASE_DSN")
	if dsn == "" {
		t.Skip("IDP_INTEGRATION_DATABASE_DSN is not configured")
	}

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	defer sqlDB.Close()
	tx := db.Begin()
	require.NoError(t, tx.Error)
	defer tx.Rollback()

	expiredAt := time.Now().UTC().Add(-time.Minute)
	user := models.User{
		ID:                 uuid.New(),
		Email:              "expired-reset-" + uuid.NewString() + "@example.test",
		Password:           "old-password-hash",
		ResetPasswordToken: "expired-token-" + uuid.NewString(),
		ResetTokenExpires:  &expiredAt,
		IsActive:           true,
	}
	require.NoError(t, tx.Create(&user).Error)

	repository := &GormUserRepository{DB: tx, logger: integrationTestLogger{}}
	err = repository.ConsumePasswordReset(user.ResetPasswordToken, "new-password-hash")
	require.True(t, errors.Is(err, irepository.ErrInvalidResetToken))

	var stored models.User
	require.NoError(t, tx.First(&stored, "id = ?", user.ID).Error)
	require.Equal(t, "old-password-hash", stored.Password)
	require.Equal(t, user.ResetPasswordToken, stored.ResetPasswordToken)
}

func TestGormUserRepositoryRollbackDoesNotClearNewerPasswordReset(t *testing.T) {
	dsn := os.Getenv("IDP_INTEGRATION_DATABASE_DSN")
	if dsn == "" {
		t.Skip("IDP_INTEGRATION_DATABASE_DSN is not configured")
	}

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	defer sqlDB.Close()
	tx := db.Begin()
	require.NoError(t, tx.Error)
	defer tx.Rollback()

	user := models.User{
		ID:       uuid.New(),
		Email:    "reset-rollback-" + uuid.NewString() + "@example.test",
		Password: "old-password-hash",
		IsActive: true,
	}
	require.NoError(t, tx.Create(&user).Error)

	repository := &GormUserRepository{DB: tx, logger: integrationTestLogger{}}
	expiresAt := time.Now().UTC().Add(10 * time.Minute)
	require.NoError(t, repository.StorePasswordReset(user.ID, "newer-token", expiresAt))
	require.NoError(t, repository.ClearPasswordResetIfToken(user.ID, "older-failed-token"))

	var stored models.User
	require.NoError(t, tx.First(&stored, "id = ?", user.ID).Error)
	require.Equal(t, "newer-token", stored.ResetPasswordToken)
	require.NotNil(t, stored.ResetTokenExpires)

	require.NoError(t, repository.ClearPasswordResetIfToken(user.ID, "newer-token"))
	var cleared models.User
	require.NoError(t, tx.First(&cleared, "id = ?", user.ID).Error)
	require.Empty(t, cleared.ResetPasswordToken)
	require.Nil(t, cleared.ResetTokenExpires)
}

type integrationTestLogger struct{}

func (integrationTestLogger) Info(string, ...interface{})  {}
func (integrationTestLogger) Error(string, ...interface{}) {}
func (integrationTestLogger) Warn(string, ...interface{})  {}
func (integrationTestLogger) Debug(string, ...interface{}) {}
