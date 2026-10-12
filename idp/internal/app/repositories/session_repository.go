package repositories

import (
	"context"
	"errors"
	"fmt"
	"time"

	"automation-hub-idp/internal/app/models"
	"automation-hub-idp/internal/app/repositories/irepository"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	gormlogger "gorm.io/gorm/logger"
)

type GormSessionRepository struct{ DB *gorm.DB }

const sessionUserColumns = "id, email, password, role, session_version, first_access, failed_attempts, last_attempt, is_blocked, blocked_until, reset_password_token, reset_token_expires, is_active, created_at, updated_at"
const sessionFamilyColumns = "id, user_id, session_version, current_generation, current_refresh_uuid, expires_at, revoked_at, created_at, updated_at"
const sessionReceiptColumns = "predecessor_uuid, family_id, predecessor_generation, replacement_generation, replacement_uuid, encrypted_pair, consumed_at, replay_until"

func NewGormSessionRepository(db *gorm.DB) irepository.SessionRepository {
	if db != nil {
		// Rotation ciphertext is a credential; never emit parameterized SQL logs.
		db = db.Session(&gorm.Session{Logger: db.Logger.LogMode(gormlogger.Silent)})
	}
	return &GormSessionRepository{DB: db}
}

func (r *GormSessionRepository) database(ctx context.Context) (*gorm.DB, error) {
	if r == nil || r.DB == nil || ctx == nil {
		return nil, irepository.ErrSessionAuthorityUnavailable
	}
	return r.DB.WithContext(ctx), nil
}

func (r *GormSessionRepository) Ready(ctx context.Context) error {
	db, err := r.database(ctx)
	if err != nil {
		return err
	}
	// Ping alone would not detect a missing or incompatible authority schema.
	if err := db.Model(&models.User{}).Select(sessionUserColumns).Limit(0).Find(&[]models.User{}).Error; err != nil {
		return irepository.ErrSessionAuthorityUnavailable
	}
	if err := db.Model(&models.SessionFamily{}).Select(sessionFamilyColumns).Limit(0).Find(&[]models.SessionFamily{}).Error; err != nil {
		return irepository.ErrSessionAuthorityUnavailable
	}
	if err := db.Model(&models.RefreshRotationReceipt{}).Select(sessionReceiptColumns).Limit(0).Find(&[]models.RefreshRotationReceipt{}).Error; err != nil {
		return irepository.ErrSessionAuthorityUnavailable
	}
	return nil
}

func validSessionIdentity(id irepository.SessionIdentity) bool {
	return id.UserID != uuid.Nil && id.FamilyID != uuid.Nil && id.RefreshUUID != uuid.Nil && id.SessionVersion >= 0 && !id.ExpiresAt.IsZero()
}

func sessionDatabaseTime(tx *gorm.DB) (time.Time, error) {
	var now time.Time
	if err := tx.Raw("SELECT clock_timestamp()").Scan(&now).Error; err != nil {
		return time.Time{}, err
	}
	return now.UTC(), nil
}

// Every operation that locks both rows uses user -> family, including logout.
func lockSessionUser(tx *gorm.DB, id uuid.UUID) (*models.User, error) {
	var user models.User
	if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Select(sessionUserColumns).Where("id = ?", id).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, irepository.ErrSessionUserUnavailable
		}
		return nil, err
	}
	return &user, nil
}

func availableSessionUser(user *models.User, version int64, now time.Time) error {
	if !user.IsActive || (user.IsBlocked && (user.BlockedUntil == nil || now.Before(*user.BlockedUntil))) {
		return irepository.ErrSessionUserUnavailable
	}
	if user.SessionVersion != version {
		return irepository.ErrSessionRevoked
	}
	return nil
}

func lockSessionFamily(tx *gorm.DB, id irepository.SessionIdentity, strength string) (*models.SessionFamily, error) {
	var family models.SessionFamily
	if err := tx.Clauses(clause.Locking{Strength: strength}).Select(sessionFamilyColumns).Where("id = ?", id.FamilyID).First(&family).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, irepository.ErrSessionRevoked
		}
		return nil, err
	}
	if family.UserID != id.UserID || family.SessionVersion != id.SessionVersion || family.CurrentGeneration < 0 || family.CurrentRefreshUUID == uuid.Nil || !family.ExpiresAt.Equal(id.ExpiresAt) {
		return nil, irepository.ErrSessionRevoked
	}
	return &family, nil
}

func (r *GormSessionRepository) CreateFamily(ctx context.Context, expected models.User, id irepository.SessionIdentity) (*models.User, error) {
	db, err := r.database(ctx)
	if err != nil {
		return nil, err
	}
	if !validSessionIdentity(id) || expected.ID != id.UserID || expected.SessionVersion != id.SessionVersion || id.FamilyID != id.RefreshUUID {
		return nil, irepository.ErrSessionRevoked
	}
	var current *models.User
	err = db.Transaction(func(tx *gorm.DB) error {
		var err error
		current, err = lockSessionUser(tx, id.UserID)
		if err != nil {
			return err
		}
		now, err := sessionDatabaseTime(tx)
		if err != nil {
			return err
		}
		if err := availableSessionUser(current, id.SessionVersion, now); err != nil {
			return err
		}
		if !id.ExpiresAt.After(now) || current.Password != expected.Password || current.Email != expected.Email || current.ResetPasswordToken != expected.ResetPasswordToken || !sameLoginResetExpiry(current.ResetTokenExpires, expected.ResetTokenExpires) {
			return irepository.ErrSessionRevoked
		}
		family := models.SessionFamily{ID: id.FamilyID, UserID: id.UserID, SessionVersion: id.SessionVersion, CurrentRefreshUUID: id.RefreshUUID, ExpiresAt: id.ExpiresAt.UTC(), CreatedAt: now, UpdatedAt: now}
		// Insert only. Never upsert/recreate an existing or revoked family.
		return tx.Create(&family).Error
	})
	if err != nil {
		return nil, fmt.Errorf("create durable session: %w", err)
	}
	return current, nil
}

func (r *GormSessionRepository) Validate(ctx context.Context, id irepository.SessionIdentity) (*models.User, error) {
	return r.validate(ctx, id, uuid.Nil)
}

func (r *GormSessionRepository) ValidateRotation(ctx context.Context, id irepository.SessionIdentity, predecessor uuid.UUID) (*models.User, error) {
	if predecessor == uuid.Nil {
		return nil, irepository.ErrSessionRevoked
	}
	return r.validate(ctx, id, predecessor)
}

func (r *GormSessionRepository) validate(ctx context.Context, id irepository.SessionIdentity, predecessor uuid.UUID) (*models.User, error) {
	db, err := r.database(ctx)
	if err != nil {
		return nil, err
	}
	if !validSessionIdentity(id) {
		return nil, irepository.ErrSessionRevoked
	}
	var user models.User
	// One statement observes account and family at the same committed snapshot.
	// This is the authorization point; it does not cancel earlier in-flight work.
	query := db.Model(&models.User{}).
		Select("users.id, users.email, users.password, users.role, users.session_version, users.first_access, users.failed_attempts, users.last_attempt, users.is_blocked, users.blocked_until, users.reset_password_token, users.reset_token_expires, users.is_active, users.created_at, users.updated_at").
		Joins("JOIN session_families AS sf ON sf.user_id = users.id").
		Where("users.id = ? AND users.session_version = ? AND users.is_active = TRUE", id.UserID, id.SessionVersion).
		Where("(NOT users.is_blocked OR (users.blocked_until IS NOT NULL AND users.blocked_until <= clock_timestamp()))").
		Where("sf.id = ? AND sf.session_version = ? AND sf.current_generation >= 0 AND sf.current_refresh_uuid = ? AND sf.expires_at = ? AND sf.expires_at > clock_timestamp() AND sf.revoked_at IS NULL", id.FamilyID, id.SessionVersion, id.RefreshUUID, id.ExpiresAt)
	if predecessor != uuid.Nil {
		query = query.Joins("JOIN refresh_rotation_receipts AS rr ON rr.family_id = sf.id").
			Where("rr.predecessor_uuid = ? AND rr.replacement_uuid = sf.current_refresh_uuid AND rr.replacement_generation = sf.current_generation AND rr.replay_until > clock_timestamp()", predecessor)
	}
	err = query.First(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, irepository.ErrSessionRevoked
	}
	if err != nil {
		return nil, fmt.Errorf("validate durable session: %w", err)
	}
	return &user, nil
}

func (r *GormSessionRepository) Rotate(ctx context.Context, id irepository.SessionIdentity, replacement uuid.UUID, encryptedPair string, grace time.Duration) (*irepository.SessionRotationResult, error) {
	db, err := r.database(ctx)
	if err != nil {
		return nil, err
	}
	if !validSessionIdentity(id) || replacement == uuid.Nil || replacement == id.RefreshUUID || encryptedPair == "" || len(encryptedPair) > 32768 || grace <= 0 || grace > 5*time.Second {
		return nil, irepository.ErrSessionRevoked
	}
	result := &irepository.SessionRotationResult{}
	err = db.Transaction(func(tx *gorm.DB) error {
		user, err := lockSessionUser(tx, id.UserID)
		if err != nil {
			return err
		}
		family, err := lockSessionFamily(tx, id, "UPDATE")
		if err != nil {
			return err
		}
		now, err := sessionDatabaseTime(tx)
		if err != nil {
			return err
		}
		if err := availableSessionUser(user, id.SessionVersion, now); err != nil {
			return err
		}
		if family.RevokedAt != nil || !family.ExpiresAt.After(now) {
			result.Revoked = true
			return nil
		}
		if family.CurrentRefreshUUID != id.RefreshUUID {
			var receipt models.RefreshRotationReceipt
			err := tx.Select(sessionReceiptColumns).Where("predecessor_uuid = ? AND family_id = ?", id.RefreshUUID, id.FamilyID).First(&receipt).Error
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			if err == nil && now.Before(receipt.ReplayUntil) {
				if receipt.ReplacementUUID == family.CurrentRefreshUUID && receipt.ReplacementGeneration == family.CurrentGeneration {
					result.Receipt = &receipt
				}
				return nil // A consumed child inside grace denies without revoking its descendant.
			}
			result.Revoked = true
			return tx.Model(family).Updates(map[string]interface{}{"revoked_at": now, "updated_at": now}).Error
		}
		if family.CurrentGeneration == int64(1<<63-1) {
			return irepository.ErrSessionRevoked
		}
		deadline := now.Add(grace)
		if family.ExpiresAt.Before(deadline) {
			deadline = family.ExpiresAt
		}
		receipt := models.RefreshRotationReceipt{PredecessorUUID: id.RefreshUUID, FamilyID: id.FamilyID, PredecessorGeneration: family.CurrentGeneration, ReplacementGeneration: family.CurrentGeneration + 1, ReplacementUUID: replacement, EncryptedPair: encryptedPair, ConsumedAt: now, ReplayUntil: deadline}
		if err := tx.Create(&receipt).Error; err != nil {
			return err
		}
		if err := tx.Model(family).Updates(map[string]interface{}{"current_generation": receipt.ReplacementGeneration, "current_refresh_uuid": replacement, "updated_at": now}).Error; err != nil {
			return err
		}
		result.Receipt = &receipt
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("rotate durable session: %w", err)
	}
	return result, nil
}

func (r *GormSessionRepository) RevokeFamily(ctx context.Context, id irepository.SessionIdentity) error {
	return r.revoke(ctx, id, uuid.Nil)
}

func (r *GormSessionRepository) RevokeFamilyIfCurrent(ctx context.Context, id irepository.SessionIdentity, current uuid.UUID) error {
	if current == uuid.Nil {
		return irepository.ErrSessionRevoked
	}
	return r.revoke(ctx, id, current)
}

func (r *GormSessionRepository) revoke(ctx context.Context, id irepository.SessionIdentity, current uuid.UUID) error {
	db, err := r.database(ctx)
	if err != nil {
		return err
	}
	if !validSessionIdentity(id) {
		return irepository.ErrSessionRevoked
	}
	return db.Transaction(func(tx *gorm.DB) error {
		if _, err := lockSessionUser(tx, id.UserID); err != nil {
			return err
		}
		family, err := lockSessionFamily(tx, id, "UPDATE")
		if err != nil {
			return err
		}
		// Revocation does not require an active user/current generation: logout
		// must still work after a block/reset and from a pre-rotation access JWT.
		if family.RevokedAt != nil || (current != uuid.Nil && family.CurrentRefreshUUID != current) {
			return nil
		}
		now, err := sessionDatabaseTime(tx)
		if err != nil {
			return err
		}
		return tx.Model(family).Updates(map[string]interface{}{"revoked_at": now, "updated_at": now}).Error
	})
}

var _ irepository.SessionRepository = (*GormSessionRepository)(nil)
