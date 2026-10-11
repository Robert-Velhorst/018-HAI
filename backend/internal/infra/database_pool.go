package infra

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type postgresPoolSettings struct {
	maxOpen, maxIdle            int
	maxIdleTime, maxLifetime    time.Duration
	connectTimeout, openTimeout time.Duration
}

func postgresPoolSettingsFromEnv() (postgresPoolSettings, error) {
	settings := postgresPoolSettings{8, 2, 2 * time.Minute, 30 * time.Minute, 5 * time.Second, 10 * time.Second}
	for _, entry := range []struct {
		name    string
		value   *int
		minimum int
	}{
		{"DB_MAX_OPEN_CONNS", &settings.maxOpen, 2},
		{"DB_MAX_IDLE_CONNS", &settings.maxIdle, 0},
	} {
		if raw := strings.TrimSpace(os.Getenv(entry.name)); raw != "" {
			value, err := strconv.Atoi(raw)
			if err != nil || value < entry.minimum {
				return postgresPoolSettings{}, fmt.Errorf("%s must be an integer >= %d", entry.name, entry.minimum)
			}
			*entry.value = value
		}
	}
	if settings.maxIdle > settings.maxOpen {
		return postgresPoolSettings{}, errors.New("DB_MAX_IDLE_CONNS must not exceed DB_MAX_OPEN_CONNS")
	}
	for _, entry := range []struct {
		name  string
		value *time.Duration
	}{
		{"DB_CONN_MAX_IDLE_TIME", &settings.maxIdleTime},
		{"DB_CONN_MAX_LIFETIME", &settings.maxLifetime},
		{"DB_CONNECT_TIMEOUT", &settings.connectTimeout},
		{"DB_OPEN_TIMEOUT", &settings.openTimeout},
	} {
		if raw := strings.TrimSpace(os.Getenv(entry.name)); raw != "" {
			value, err := time.ParseDuration(raw)
			if err != nil || value <= 0 {
				return postgresPoolSettings{}, fmt.Errorf("%s must be a positive duration, e.g. 5s or 2m", entry.name)
			}
			*entry.value = value
		}
	}
	return settings, nil
}

func postgresConnectionConfig(user, password, dbName, dbHost string, dbPort int, settings postgresPoolSettings) (*pgx.ConnConfig, error) {
	if strings.TrimSpace(user) == "" || strings.TrimSpace(dbName) == "" || strings.TrimSpace(dbHost) == "" || dbPort < 1 || dbPort > 65535 {
		return nil, errors.New("PostgreSQL requires a user, database, host and port between 1 and 65535")
	}
	sslMode, err := postgresSSLModeFromEnv(dbHost)
	if err != nil {
		return nil, err
	}
	// Encode each field, rather than allowing credential text to inject DSN options.
	query := url.Values{
		"host": {dbHost}, "port": {strconv.Itoa(dbPort)}, "dbname": {dbName},
		"sslmode": {sslMode}, "timezone": {"UTC"},
	}
	connectionURL := url.URL{Scheme: "postgres", User: url.UserPassword(user, password), RawQuery: query.Encode()}
	cfg, err := pgx.ParseConfig(connectionURL.String())
	if err != nil {
		// Parse errors include the original credential-bearing string.
		return nil, errors.New("invalid PostgreSQL connection configuration")
	}
	cfg.ConnectTimeout = settings.connectTimeout
	return cfg, nil
}

func postgresSSLModeFromEnv(dbHost string) (string, error) {
	mode := strings.ToLower(strings.TrimSpace(os.Getenv("RUN_MODE")))
	production := mode == "production" || (mode != "demo" && mode != "test")
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
		dbHost == "postgres-automation" {
		return sslMode, nil
	}
	if production && sslMode != "verify-ca" && sslMode != "verify-full" {
		return "", errors.New("DB_SSLMODE must be verify-ca or verify-full in production")
	}
	return sslMode, nil
}

// NewPostgresDatabaseContext opens a caller-owned pool. Both initial ping and
// future connections are bounded; the caller must close a successful pool.
// Cached GetDefaultDB instead retains its successful pool for process lifetime.
func NewPostgresDatabaseContext(ctx context.Context, user, password, dbName, dbHost string, dbPort int) (*gorm.DB, error) {
	if ctx == nil {
		return nil, errors.New("PostgreSQL open requires a context")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	settings, err := postgresPoolSettingsFromEnv()
	if err != nil {
		return nil, err
	}
	cfg, err := postgresConnectionConfig(user, password, dbName, dbHost, dbPort, settings)
	if err != nil {
		return nil, err
	}
	connector := postgresBoundedConnector{Connector: stdlib.GetConnector(*cfg), timeout: settings.connectTimeout}
	return initializePostgresPool(ctx, sql.OpenDB(connector), settings)
}

// Wrap the whole acquisition, including DNS and multi-host fallback. pgx's
// own ConnectTimeout applies per host, after address resolution.
type postgresBoundedConnector struct {
	driver.Connector
	timeout time.Duration
}

func (connector postgresBoundedConnector) Connect(ctx context.Context) (driver.Conn, error) {
	connectCtx, cancel := context.WithTimeout(ctx, connector.timeout)
	defer cancel()
	return connector.Connector.Connect(connectCtx)
}

func initializePostgresPool(ctx context.Context, pool *sql.DB, settings postgresPoolSettings) (*gorm.DB, error) {
	pool.SetMaxOpenConns(settings.maxOpen)
	pool.SetMaxIdleConns(settings.maxIdle)
	pool.SetConnMaxIdleTime(settings.maxIdleTime)
	pool.SetConnMaxLifetime(settings.maxLifetime)
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{DisableAutomaticPing: true})
	if err == nil {
		openCtx, cancel := context.WithTimeout(ctx, settings.openTimeout)
		err = pool.PingContext(openCtx)
		cancel()
	}
	if err != nil {
		return nil, errors.Join(fmt.Errorf("open PostgreSQL pool: %w", err), pool.Close())
	}
	return db, nil
}

func closePostgresDatabase(db *gorm.DB) error {
	if db == nil || db.Config == nil {
		return errors.New("cannot close uninitialized PostgreSQL pool")
	}
	pool, err := db.DB()
	if err != nil {
		return fmt.Errorf("acquire failed startup pool for cleanup: %w", err)
	}
	return pool.Close()
}
