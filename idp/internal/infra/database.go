package infra

import (
	"automation-hub-idp/internal/app/config"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

const (
	defaultMaxOpenConnections = 8
	defaultMaxIdleConnections = 2
	defaultConnectionIdleTime = 5 * time.Minute
	defaultConnectionLifetime = 30 * time.Minute
)

type databaseIdentity struct {
	user     string
	password string
	name     string
	host     string
	port     int
}

type poolSettings struct {
	maxOpenConnections int
	maxIdleConnections int
	connectionIdleTime time.Duration
	connectionLifetime time.Duration
}

var defaultDatabaseState struct {
	sync.Mutex
	database *gorm.DB
	identity databaseIdentity
}

func NewPostgresDatabase(user, password, dbName, dbHost string, dbPort int) (*gorm.DB, error) {
	settings, err := loadPoolSettings(
		defaultMaxOpenConnections,
		defaultMaxIdleConnections,
		defaultConnectionIdleTime,
		defaultConnectionLifetime,
	)
	if err != nil {
		return nil, err
	}

	dsn := fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%d sslmode=disable TimeZone=UTC",
		dbHost, user, password, dbName, dbPort)

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		return nil, err
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("acquire postgres connection pool: %w", err)
	}
	sqlDB.SetMaxOpenConns(settings.maxOpenConnections)
	sqlDB.SetMaxIdleConns(settings.maxIdleConnections)
	sqlDB.SetConnMaxIdleTime(settings.connectionIdleTime)
	sqlDB.SetConnMaxLifetime(settings.connectionLifetime)

	return db, nil
}

func GetDefaultDB() (*gorm.DB, error) {
	identity := databaseIdentity{
		user:     config.PostgresConfig.User,
		password: config.PostgresConfig.Password,
		name:     config.PostgresConfig.DbName,
		host:     config.PostgresConfig.DbHost,
		port:     config.PostgresConfig.DbPort,
	}

	defaultDatabaseState.Lock()
	defer defaultDatabaseState.Unlock()
	if defaultDatabaseState.database != nil && defaultDatabaseState.identity == identity {
		return defaultDatabaseState.database, nil
	}

	db, err := NewPostgresDatabase(identity.user, identity.password, identity.name, identity.host, identity.port)
	if err != nil {
		return nil, err
	}

	if err := RunMigrations(db); err != nil {
		closeDatabase(db)
		return nil, err
	}

	if err := SeedDatabase(db); err != nil {
		closeDatabase(db)
		return nil, err
	}

	if defaultDatabaseState.database != nil {
		closeDatabase(defaultDatabaseState.database)
	}
	defaultDatabaseState.database = db
	defaultDatabaseState.identity = identity

	return db, nil
}

func loadPoolSettings(defaultMaxOpen, defaultMaxIdle int, defaultIdleTime, defaultLifetime time.Duration) (poolSettings, error) {
	maxOpen, err := positiveEnvInt("DB_MAX_OPEN_CONNS", defaultMaxOpen)
	if err != nil {
		return poolSettings{}, err
	}
	maxIdle, err := nonNegativeEnvInt("DB_MAX_IDLE_CONNS", defaultMaxIdle)
	if err != nil {
		return poolSettings{}, err
	}
	if maxIdle > maxOpen {
		return poolSettings{}, fmt.Errorf("DB_MAX_IDLE_CONNS (%d) cannot exceed DB_MAX_OPEN_CONNS (%d)", maxIdle, maxOpen)
	}
	idleTime, err := nonNegativeEnvDuration("DB_CONN_MAX_IDLE_TIME", defaultIdleTime)
	if err != nil {
		return poolSettings{}, err
	}
	lifetime, err := nonNegativeEnvDuration("DB_CONN_MAX_LIFETIME", defaultLifetime)
	if err != nil {
		return poolSettings{}, err
	}
	return poolSettings{
		maxOpenConnections: maxOpen,
		maxIdleConnections: maxIdle,
		connectionIdleTime: idleTime,
		connectionLifetime: lifetime,
	}, nil
}

func positiveEnvInt(name string, fallback int) (int, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer", name)
	}
	return value, nil
}

func nonNegativeEnvInt(name string, fallback int) (int, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 {
		return 0, fmt.Errorf("%s must be a non-negative integer", name)
	}
	return value, nil
}

func nonNegativeEnvDuration(name string, fallback time.Duration) (time.Duration, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := time.ParseDuration(raw)
	if err != nil || value < 0 {
		return 0, fmt.Errorf("%s must be a non-negative Go duration such as 5m or 30m", name)
	}
	return value, nil
}

func closeDatabase(db *gorm.DB) {
	if db == nil {
		return
	}
	if sqlDB, err := db.DB(); err == nil {
		_ = sqlDB.Close()
	}
}
