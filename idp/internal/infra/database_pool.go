package infra

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type databasePoolSettings struct {
	maxOpenConnections int
	maxIdleConnections int
	connectionIdleTime time.Duration
	connectionLifetime time.Duration
}

func loadPoolSettings(defaultMaxOpen, defaultMaxIdle int, defaultIdleTime, defaultLifetime time.Duration) (databasePoolSettings, error) {
	settings := databasePoolSettings{
		maxOpenConnections: defaultMaxOpen,
		maxIdleConnections: defaultMaxIdle,
		connectionIdleTime: defaultIdleTime,
		connectionLifetime: defaultLifetime,
	}
	var err error
	if settings.maxOpenConnections, err = envPoolInt("DB_MAX_OPEN_CONNS", defaultMaxOpen); err != nil {
		return databasePoolSettings{}, err
	}
	if settings.maxIdleConnections, err = envPoolInt("DB_MAX_IDLE_CONNS", defaultMaxIdle); err != nil {
		return databasePoolSettings{}, err
	}
	if settings.connectionIdleTime, err = envPoolDuration("DB_CONN_MAX_IDLE_TIME", defaultIdleTime); err != nil {
		return databasePoolSettings{}, err
	}
	if settings.connectionLifetime, err = envPoolDuration("DB_CONN_MAX_LIFETIME", defaultLifetime); err != nil {
		return databasePoolSettings{}, err
	}
	if settings.maxOpenConnections < 1 {
		return databasePoolSettings{}, errors.New("DB_MAX_OPEN_CONNS must be at least 1")
	}
	if settings.maxIdleConnections < 0 {
		return databasePoolSettings{}, errors.New("DB_MAX_IDLE_CONNS cannot be negative")
	}
	if settings.maxIdleConnections > settings.maxOpenConnections {
		return databasePoolSettings{}, errors.New("DB_MAX_IDLE_CONNS cannot exceed DB_MAX_OPEN_CONNS")
	}
	if settings.connectionIdleTime < 0 || settings.connectionLifetime < 0 {
		return databasePoolSettings{}, errors.New("database connection durations cannot be negative")
	}
	return settings, nil
}

func envPoolInt(name string, fallback int) (int, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer", name)
	}
	return value, nil
}

func envPoolDuration(name string, fallback time.Duration) (time.Duration, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("%s must be a valid duration", name)
	}
	return value, nil
}
