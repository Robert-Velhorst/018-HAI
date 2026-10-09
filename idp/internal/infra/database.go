package infra

import (
	"automation-hub-idp/internal/app/config"
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func NewPostgresDatabase(user, password, dbName, dbHost string, dbPort int) (*gorm.DB, error) {
	return NewPostgresDatabaseContext(context.Background(), user, password, dbName, dbHost, dbPort)
}

func NewPostgresDatabaseContext(ctx context.Context, user, password, dbName, dbHost string, dbPort int) (*gorm.DB, error) {
	if ctx == nil {
		return nil, errors.New("PostgreSQL open requires a context")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	dsn, err := postgresConnectionString(user, password, dbName, dbHost, dbPort)
	if err != nil {
		return nil, err
	}

	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, errors.New("invalid PostgreSQL connection configuration")
	}
	connector := idpBoundedConnector{Connector: stdlib.GetConnector(*cfg)}
	return initializeIDPPostgresPool(ctx, sql.OpenDB(connector))
}

type idpBoundedConnector struct{ driver.Connector }

func (c idpBoundedConnector) Connect(ctx context.Context) (driver.Conn, error) {
	connectCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return c.Connector.Connect(connectCtx)
}

func initializeIDPPostgresPool(ctx context.Context, pool *sql.DB) (database *gorm.DB, err error) {
	if pool == nil {
		return nil, errors.New("IDP PostgreSQL pool is unavailable")
	}
	defer func() {
		if database == nil {
			err = errors.Join(err, pool.Close())
		}
	}()
	if ctx == nil {
		return nil, errors.New("PostgreSQL open requires a context")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	pool.SetMaxOpenConns(8)
	pool.SetMaxIdleConns(2)
	pool.SetConnMaxIdleTime(2 * time.Minute)
	pool.SetConnMaxLifetime(30 * time.Minute)
	// Own the first ping so its deadline and failure cleanup are explicit.
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{
		DisableAutomaticPing: true,
		// SQL traces can contain account data, reset tokens and driver credentials.
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, err
	}
	openCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := pool.PingContext(openCtx); err != nil {
		return nil, err
	}
	if err := openCtx.Err(); err != nil {
		return nil, err
	}
	return db, nil
}

func postgresConnectionString(user, password, dbName, dbHost string, dbPort int) (string, error) {
	if strings.TrimSpace(user) == "" || strings.TrimSpace(dbName) == "" || strings.TrimSpace(dbHost) == "" || dbPort < 1 || dbPort > 65535 {
		return "", errors.New("PostgreSQL requires a user, database, host and port between 1 and 65535")
	}
	sslMode, err := idpPostgresSSLModeFromEnv(dbHost)
	if err != nil {
		return "", err
	}
	query := url.Values{
		"host": {dbHost}, "port": {strconv.Itoa(dbPort)}, "dbname": {dbName},
		"sslmode": {sslMode}, "timezone": {"UTC"}, "connect_timeout": {"5"},
	}
	connectionURL := url.URL{Scheme: "postgres", User: url.UserPassword(user, password), RawQuery: query.Encode()}
	dsn := connectionURL.String()
	if _, err := pgx.ParseConfig(dsn); err != nil {
		// Parser errors can contain the entire credential-bearing URL.
		return "", errors.New("invalid PostgreSQL connection configuration")
	}
	return dsn, nil
}

func idpPostgresSSLModeFromEnv(dbHost string) (string, error) {
	runMode := strings.ToLower(strings.TrimSpace(os.Getenv("RUN_MODE")))
	production := runMode == "production" || (runMode != "demo" && runMode != "test")
	sslMode := strings.ToLower(strings.TrimSpace(os.Getenv("DB_SSLMODE")))
	if sslMode == "" {
		if production {
			return "", errors.New("DB_SSLMODE must be verify-ca or verify-full in production")
		}
		return "disable", nil
	}
	switch sslMode {
	case "disable", "require", "verify-ca", "verify-full":
	default:
		return "", errors.New("DB_SSLMODE must be disable, require, verify-ca or verify-full")
	}
	if production && sslMode == "disable" &&
		strings.EqualFold(strings.TrimSpace(os.Getenv("HAI_LOCAL_COMPOSE_DATABASE")), "true") &&
		dbHost == "postgres-idp" {
		return sslMode, nil
	}
	if production && sslMode != "verify-ca" && sslMode != "verify-full" {
		return "", errors.New("DB_SSLMODE must be verify-ca or verify-full in production")
	}
	return sslMode, nil
}

func GetDefaultDB() (*gorm.DB, error) {
	return GetDefaultDBContext(context.Background())
}

func idpStartupContext(parent context.Context) (context.Context, context.CancelFunc, error) {
	if parent == nil {
		return nil, nil, errors.New("IDP database startup requires a context")
	}
	if err := parent.Err(); err != nil {
		return nil, nil, err
	}
	timeout := 5 * time.Minute
	if raw := strings.TrimSpace(os.Getenv("IDP_DB_STARTUP_TIMEOUT")); raw != "" {
		var err error
		timeout, err = time.ParseDuration(raw)
		if err != nil || timeout <= 0 {
			return nil, nil, errors.New("IDP_DB_STARTUP_TIMEOUT must be a positive duration with units")
		}
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	return ctx, cancel, nil
}

func GetDefaultDBContext(parent context.Context) (*gorm.DB, error) {
	ctx, cancel, err := idpStartupContext(parent)
	if err != nil {
		return nil, err
	}
	defer cancel()
	db, err := NewPostgresDatabaseContext(ctx, config.PostgresConfig.User, config.PostgresConfig.Password,
		config.PostgresConfig.DbName, config.PostgresConfig.DbHost, config.PostgresConfig.DbPort)
	if err != nil {
		return nil, err
	}
	if _, err := prepareDefaultDatabase(db.WithContext(ctx), RunMigrations, SeedDatabase); err != nil {
		return nil, err
	}
	// Retain a neutral runtime handle, not the cancelled startup context.
	return db, nil
}

func prepareDefaultDatabase(db *gorm.DB, migrate, seed func(*gorm.DB) error) (database *gorm.DB, err error) {
	if db == nil || db.Config == nil {
		return nil, errors.New("IDP startup database pool is unavailable")
	}
	pool, err := db.DB()
	if err != nil || pool == nil {
		return nil, errors.Join(errors.New("cannot acquire IDP startup database pool"), err)
	}
	// Ownership transfers to the caller only after every startup step succeeds.
	defer func() {
		if database == nil {
			err = errors.Join(err, pool.Close())
		}
	}()
	ctx := context.Background()
	if db.Statement != nil && db.Statement.Context != nil {
		ctx = db.Statement.Context
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := migrate(db); err != nil {
		return nil, err
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := seed(db); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	return db, nil
}
