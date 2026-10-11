package repositories

import (
	"automation-hub-idp/internal/app/models"
	"automation-hub-idp/internal/app/repositories/irepository"
	"automation-hub-idp/internal/app/utils"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Logger interface {
	Info(message string, args ...interface{})
	Error(message string, args ...interface{})
	Warn(message string, args ...interface{})
	Debug(message string, args ...interface{})
} // TODO: 1. Create a new interface called Logger with the following methods: Info, Error, Warn, Debug

type GormUserRepository struct {
	DB     *gorm.DB
	logger Logger
}

func NewGormUserRepository(db *gorm.DB, logger Logger) irepository.UserRepository {
	return &GormUserRepository{
		DB:     db,
		logger: logger,
	}
}

func (r *GormUserRepository) logError(message string, args ...interface{}) {
	if r.logger != nil {
		r.logger.Error(message, args...)
	}
}

func (r *GormUserRepository) FindByID(id uuid.UUID) (*models.User, error) {
	var user models.User
	err := r.DB.First(&user, "id = ? AND is_active = ?", id, true).Error
	if err != nil {
		r.logError("Failed to fetch user by ID: %s", err)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, irepository.ErrUserNotFound
		}
		return nil, fmt.Errorf("find user by ID: %w", err)
	}
	return &user, nil
}

func (r *GormUserRepository) FindByEmail(email string) (*models.User, error) {
	var user models.User
	err := r.DB.First(&user, "email = ? AND is_active = ?", email, true).Error
	if err != nil {
		r.logError("Failed to fetch user by email: %s", err)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, irepository.ErrUserNotFound
		}
		return nil, fmt.Errorf("find user by email: %w", err)
	}
	return &user, nil
}

func (r *GormUserRepository) Create(user *models.User) (*models.User, error) {
	err := r.DB.Create(user).Error
	if err != nil {
		var postgresError *pgconn.PgError
		if errors.As(err, &postgresError) && postgresError.Code == "23505" {
			return nil, irepository.ErrDuplicateUser
		}
		r.logError("Failed to create user: %s", err)
		return nil, errors.New("failed to create user")
	}
	return user, nil
}

func (r *GormUserRepository) Update(user *models.User) (*models.User, error) {
	if user == nil || user.ID == uuid.Nil {
		return nil, errors.New("user update requires an ID")
	}
	result := r.DB.Model(&models.User{}).
		Where("id = ? AND session_version = ? AND reset_password_token = ? AND reset_token_expires IS NOT DISTINCT FROM ?", user.ID, user.SessionVersion, user.ResetPasswordToken, user.ResetTokenExpires).
		Updates(map[string]interface{}{
			"email": user.Email, "password": user.Password, "role": user.Role,
			"session_version": user.SessionVersion, "first_access": user.FirstAccess,
			"failed_attempts": user.FailedAttempts, "last_attempt": user.LastAttempt,
			"is_blocked": user.IsBlocked, "blocked_until": user.BlockedUntil,
			"reset_password_token": user.ResetPasswordToken, "reset_token_expires": user.ResetTokenExpires,
			"is_active": user.IsActive, "updated_at": time.Now().UTC(),
		})
	if result.Error != nil {
		r.logError("Failed to update user: %s", result.Error)
		return nil, errors.New("failed to update user")
	}
	if result.RowsAffected != 1 {
		return nil, irepository.ErrConcurrentUserUpdate
	}
	return user, nil
}

// Serialize with failed attempts; a verified snapshot grants no authority to
// clear a newer block or overwrite account/security fields.
func (r *GormUserRepository) CompleteSuccessfulLogin(expected models.User, now time.Time) (*models.User, error) {
	if expected.ID == uuid.Nil || now.IsZero() {
		return nil, errors.New("successful login requires an ID and time")
	}
	var current models.User
	err := r.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ?", expected.ID).First(&current).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return irepository.ErrUserNotFound
			}
			return fmt.Errorf("lock user for successful login: %w", err)
		}
		if !current.IsActive || current.Password != expected.Password || current.Email != expected.Email ||
			current.SessionVersion != expected.SessionVersion || current.ResetPasswordToken != expected.ResetPasswordToken ||
			!sameLoginResetExpiry(current.ResetTokenExpires, expected.ResetTokenExpires) ||
			(current.IsBlocked && (current.BlockedUntil == nil || now.Before(*current.BlockedUntil))) {
			return irepository.ErrConcurrentUserUpdate
		}
		result := tx.Model(&models.User{}).Where("id = ?", current.ID).
			Updates(map[string]interface{}{
				"failed_attempts": 0, "last_attempt": nil,
				"is_blocked": false, "blocked_until": nil, "updated_at": now.UTC(),
			})
		if result.Error != nil {
			return fmt.Errorf("reset successful-login state: %w", result.Error)
		}
		if result.RowsAffected != 1 {
			return irepository.ErrConcurrentUserUpdate
		}
		current.FailedAttempts = 0
		current.LastAttempt = nil
		current.IsBlocked = false
		current.BlockedUntil = nil
		current.UpdatedAt = now.UTC()
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &current, nil
}

func sameLoginResetExpiry(left, right *time.Time) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}

// RecordLoginFailure serializes failed-login updates on the user row so
// simultaneous requests cannot overwrite each other's counter or lockout.
func (r *GormUserRepository) RecordLoginFailure(id uuid.UUID, now time.Time, maxAttempts int, baseBlockDuration time.Duration) (*models.User, bool, error) {
	if id == uuid.Nil || maxAttempts < 1 || baseBlockDuration <= 0 {
		return nil, false, errors.New("invalid failed-login policy")
	}

	attemptAt := now.UTC()
	var updated models.User
	blockedNow := false
	err := r.DB.Transaction(func(tx *gorm.DB) error {
		var user models.User
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND is_active = ?", id, true).
			First(&user).Error
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return irepository.ErrUserNotFound
			}
			return fmt.Errorf("lock user for failed login: %w", err)
		}

		if user.IsBlocked {
			if user.BlockedUntil == nil || attemptAt.Before(*user.BlockedUntil) {
				updated = user
				return nil
			}
			user.FailedAttempts = 0
			user.IsBlocked = false
			user.BlockedUntil = nil
		}

		user.FailedAttempts++
		user.LastAttempt = &attemptAt
		blockedNow = !user.IsBlocked && user.FailedAttempts >= maxAttempts
		if user.FailedAttempts >= maxAttempts {
			blockedUntil := attemptAt.Add(loginBlockDuration(user.FailedAttempts, maxAttempts, baseBlockDuration))
			user.IsBlocked = true
			user.BlockedUntil = &blockedUntil
		}
		user.UpdatedAt = attemptAt

		result := tx.Model(&models.User{}).
			Where("id = ? AND is_active = ?", id, true).
			Updates(map[string]interface{}{
				"failed_attempts": user.FailedAttempts,
				"last_attempt":    attemptAt,
				"is_blocked":      user.IsBlocked,
				"blocked_until":   user.BlockedUntil,
				"updated_at":      attemptAt,
			})
		if result.Error != nil {
			return fmt.Errorf("update failed-login state: %w", result.Error)
		}
		if result.RowsAffected != 1 {
			return irepository.ErrUserNotFound
		}
		updated = user
		return nil
	})
	if err != nil {
		r.logError("Failed to atomically record login failure: %s", err)
		return nil, false, err
	}
	return &updated, blockedNow, nil
}

func loginBlockDuration(failedAttempts, maxAttempts int, baseBlockDuration time.Duration) time.Duration {
	duration := baseBlockDuration
	for remaining := failedAttempts - maxAttempts; remaining > 0; remaining-- {
		if duration > time.Duration(1<<63-1)/2 {
			return time.Duration(1<<63 - 1)
		}
		duration *= 2
	}
	return duration
}

func (r *GormUserRepository) Delete(id uuid.UUID) error {
	if id == uuid.Nil {
		return errors.New("user deletion requires an ID")
	}
	result := r.DB.Model(&models.User{}).
		Where("id = ? AND is_active = ?", id, true).
		Updates(map[string]interface{}{
			"is_active":            false,
			"session_version":      gorm.Expr("session_version + 1"),
			"reset_password_token": "",
			"reset_token_expires":  nil,
			"updated_at":           time.Now().UTC(),
		})
	if result.Error != nil {
		r.logError("Failed to soft delete user: %s", result.Error)
		return errors.New("failed to soft delete user")
	}
	if result.RowsAffected != 1 {
		return irepository.ErrUserNotFound
	}
	return nil
}

func (r *GormUserRepository) FindAll(p utils.Pagination) ([]*models.User, error) {
	var users []*models.User
	err := r.DB.Where("is_active = ?", true).Limit(p.Limit).Offset(p.Offset).Find(&users).Error
	if err != nil {
		r.logError("Failed to fetch all users: %s", err)
		return nil, errors.New("failed to fetch users")
	}
	return users, nil
}

func (r *GormUserRepository) FindByResetToken(token string) (*models.User, error) {
	if token == "" {
		return nil, irepository.ErrUserNotFound
	}
	tokenDigest := utils.PasswordResetTokenDigest(token)
	var user models.User
	err := r.DB.First(&user, "reset_password_token = ? AND is_active = ?", tokenDigest, true).Error
	if err != nil {
		r.logError("Failed to fetch user by password reset token: %s", err)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, irepository.ErrUserNotFound
		}
		return nil, fmt.Errorf("find user by reset token: %w", err)
	}
	return &user, nil
}

func (r *GormUserRepository) SetPasswordResetToken(id uuid.UUID, expectedSessionVersion int64, tokenDigest string, expiresAt, now time.Time) error {
	if id == uuid.Nil || len(tokenDigest) != 64 || !expiresAt.After(now) {
		return errors.New("invalid password reset token record")
	}
	result := r.DB.Model(&models.User{}).
		Where("id = ? AND is_active = ? AND session_version = ?", id, true, expectedSessionVersion).
		Updates(map[string]interface{}{
			"reset_password_token": tokenDigest,
			"reset_token_expires":  expiresAt,
			"updated_at":           now,
		})
	if result.Error != nil {
		r.logError("Failed to store password reset token: %s", result.Error)
		return fmt.Errorf("store password reset token: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return irepository.ErrConcurrentUserUpdate
	}
	return nil
}

func (r *GormUserRepository) UpdatePassword(id uuid.UUID, expectedSessionVersion int64, passwordHash string, now time.Time) error {
	if id == uuid.Nil || passwordHash == "" {
		return errors.New("password update requires an ID and hash")
	}
	result := r.DB.Model(&models.User{}).
		Where("id = ? AND is_active = ? AND session_version = ?", id, true, expectedSessionVersion).
		Updates(map[string]interface{}{
			"password":             passwordHash,
			"first_access":         false,
			"session_version":      gorm.Expr("session_version + 1"),
			"reset_password_token": "",
			"reset_token_expires":  nil,
			"updated_at":           now,
		})
	if result.Error != nil {
		r.logError("Failed to update password: %s", result.Error)
		return fmt.Errorf("update password: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return irepository.ErrConcurrentUserUpdate
	}
	return nil
}

// UpdateAccount atomically applies profile and password changes against the
// session version that authorized the request.
func (r *GormUserRepository) UpdateAccount(id uuid.UUID, expectedSessionVersion int64, email *string, passwordHash string, now time.Time) error {
	if id == uuid.Nil || (email == nil && passwordHash == "") {
		return errors.New("account update requires an ID and at least one change")
	}
	if email != nil && strings.TrimSpace(*email) == "" {
		return errors.New("account email must not be empty")
	}
	updates := map[string]interface{}{
		"session_version": gorm.Expr("session_version + 1"),
		"updated_at":      now.UTC(),
	}
	if email != nil {
		updates["email"] = strings.TrimSpace(*email)
	}
	if email != nil || passwordHash != "" {
		updates["reset_password_token"] = ""
		updates["reset_token_expires"] = nil
	}
	if passwordHash != "" {
		updates["password"] = passwordHash
		updates["first_access"] = false
	}
	result := r.DB.Model(&models.User{}).
		Where("id = ? AND is_active = ? AND session_version = ?", id, true, expectedSessionVersion).
		Updates(updates)
	if result.Error != nil {
		var postgresError *pgconn.PgError
		if errors.As(result.Error, &postgresError) && postgresError.Code == "23505" {
			return irepository.ErrDuplicateUser
		}
		if r.logger != nil {
			r.logError("Failed to update account: %s", result.Error)
		}
		return fmt.Errorf("update account: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return irepository.ErrConcurrentUserUpdate
	}
	return nil
}

// ConsumePasswordResetToken updates the password and clears the matching reset
// token in one conditional statement. The database update is the one-time-use
// boundary, including when multiple requests race to consume the same token.
func (r *GormUserRepository) ConsumePasswordResetToken(id uuid.UUID, token, passwordHash string, now time.Time) error {
	if token == "" {
		return irepository.ErrResetTokenNotConsumable
	}
	tokenDigest := utils.PasswordResetTokenDigest(token)
	result := r.DB.Model(&models.User{}).
		Where("id = ? AND reset_password_token = ? AND reset_token_expires > ? AND is_active = ?", id, tokenDigest, now, true).
		Updates(map[string]interface{}{
			"password":             passwordHash,
			"first_access":         false,
			"session_version":      gorm.Expr("session_version + 1"),
			"reset_password_token": "",
			"reset_token_expires":  nil,
			"updated_at":           now,
		})
	if result.Error != nil {
		r.logError("Failed to consume password reset token: %s", result.Error)
		return fmt.Errorf("consume password reset token: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return irepository.ErrResetTokenNotConsumable
	}
	return nil
}

// ClearPasswordResetToken only rolls back the token issued by the failed email
// attempt; it cannot erase a newer token issued concurrently.
func (r *GormUserRepository) ClearPasswordResetToken(id uuid.UUID, token string) error {
	if token == "" {
		return nil
	}
	tokenDigest := utils.PasswordResetTokenDigest(token)
	err := r.DB.Model(&models.User{}).
		Where("id = ? AND reset_password_token = ?", id, tokenDigest).
		Updates(map[string]interface{}{
			"reset_password_token": "",
			"reset_token_expires":  nil,
		}).Error
	if err != nil {
		r.logError("Failed to clear undelivered password reset token: %s", err)
		return fmt.Errorf("clear password reset token: %w", err)
	}
	return nil
}
