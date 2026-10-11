package infra

import (
	"automation-hub-idp/internal/app/models"
	"automation-hub-idp/internal/app/utils"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
	"os"
	"strings"
)

func RunMigrations(db *gorm.DB) error {
	if err := db.AutoMigrate(&models.User{}); err != nil {
		return err
	}
	if err := migrateSessionAuthority(db); err != nil {
		return err
	}
	if err := migrateLegacyPasswordResetTokens(db); err != nil {
		return err
	}
	// Existing installations predate signed roles. Treat those accounts as
	// operators until the configured first-run owner is promoted during seeding;
	// never upgrade every historical account to owner implicitly.
	if err := db.Model(&models.User{}).
		Where("role = '' OR role IS NULL").
		Update("role", "operator").Error; err != nil {
		return err
	}
	return nil
}

// migrateLegacyPasswordResetTokens upgrades pre-digest bearer values in place.
// The value comparison makes this safe to run on every startup and prevents a
// stale migration read from overwriting a newly issued token.
func migrateLegacyPasswordResetTokens(db *gorm.DB) error {
	type resetTokenRecord struct {
		ID                 uuid.UUID
		ResetPasswordToken string
	}
	quietDB := db.Session(&gorm.Session{Logger: db.Logger.LogMode(gormlogger.Silent)})
	return quietDB.Transaction(func(tx *gorm.DB) error {
		var records []resetTokenRecord
		if err := tx.Model(&models.User{}).
			Select("id", "reset_password_token").
			Where("reset_password_token IS NOT NULL AND reset_password_token <> ''").
			Find(&records).Error; err != nil {
			return fmt.Errorf("load legacy password reset tokens: %w", err)
		}
		for _, record := range records {
			if utils.IsPasswordResetTokenDigest(record.ResetPasswordToken) {
				continue
			}
			digest := utils.PasswordResetTokenDigest(record.ResetPasswordToken)
			if err := tx.Model(&models.User{}).
				Where("id = ? AND reset_password_token = ?", record.ID, record.ResetPasswordToken).
				Update("reset_password_token", digest).Error; err != nil {
				return fmt.Errorf("upgrade legacy password reset token for user %s: %w", record.ID, err)
			}
		}
		return nil
	})
}

func SeedDatabase(db *gorm.DB) error {
	hasher := utils.DefaultBcryptHasher()
	defaultEmail := firstNonEmpty(os.Getenv("FIRST_RUN_ADMIN_EMAIL"), "noodzakelijkonline@gmail.com")
	var user models.User
	err := db.Where("Email = ?", defaultEmail).First(&user).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			defaultPassword, err := configuredFirstRunAdminPassword()
			if err != nil {
				return err
			}
			hashedPassword, err := hasher.Hash(defaultPassword)
			if err != nil {
				return err
			}
			adminUser := models.User{
				Email:       defaultEmail,
				Password:    hashedPassword,
				Role:        "owner",
				FirstAccess: false,
			}
			if err := db.Create(&adminUser).Error; err != nil {
				return err
			}
		} else {
			return err
		}
	} else if user.Role != "owner" {
		// FIRST_RUN_ADMIN_EMAIL remains the explicit ownership source after an
		// upgrade, so an existing local administrator receives a signed owner
		// role without promoting unrelated accounts.
		if err := db.Model(&user).Update("role", "owner").Error; err != nil {
			return err
		}
	}
	return nil
}

func configuredFirstRunAdminPassword() (string, error) {
	password := strings.TrimSpace(os.Getenv("FIRST_RUN_ADMIN_PASSWORD"))
	lowerPassword := strings.ToLower(password)
	if utils.ValidatePassword(password) != nil ||
		strings.Contains(lowerPassword, "change-this") ||
		strings.Contains(lowerPassword, "changeme") ||
		strings.Contains(lowerPassword, "placeholder") ||
		strings.Contains(lowerPassword, "example") {
		return "", errors.New("FIRST_RUN_ADMIN_PASSWORD must be a non-placeholder password with at least 12 characters and no more than 72 bytes before the first owner account can be created")
	}
	return password, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
