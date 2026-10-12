package infra

import (
	_ "embed"
	"errors"

	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

//go:embed session_authority_schema.sql
var sessionAuthoritySchema string

func migrateSessionAuthority(db *gorm.DB) error {
	if db == nil {
		return errors.New("durable session authority migration requires a database")
	}
	quiet := db.Session(&gorm.Session{Logger: db.Logger.LogMode(gormlogger.Silent)})
	if err := quiet.Transaction(func(tx *gorm.DB) error {
		return tx.Exec(sessionAuthoritySchema).Error
	}); err != nil {
		return errors.New("durable session authority schema migration failed")
	}
	return nil
}
