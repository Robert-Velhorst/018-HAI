package infra

import (
	"strings"
	"testing"
	"time"

	"gorm.io/gorm/logger"
)

func TestLoadDatabaseLoggerConfigIsPrivateAndQuietByDefault(t *testing.T) {
	t.Setenv("DB_LOG_LEVEL", "")

	config, err := loadDatabaseLoggerConfig()
	if err != nil {
		t.Fatalf("loadDatabaseLoggerConfig: %v", err)
	}
	if config.LogLevel != logger.Error {
		t.Fatalf("log level = %v, want error", config.LogLevel)
	}
	if !config.ParameterizedQueries {
		t.Fatal("database logging must keep query values parameterized")
	}
	if !config.IgnoreRecordNotFoundError {
		t.Fatal("record-not-found noise should not be emitted")
	}
	if config.SlowThreshold != time.Second {
		t.Fatalf("slow threshold = %v, want 1s", config.SlowThreshold)
	}
}

func TestLoadDatabaseLoggerConfigRetainsParameterizationAtEveryLevel(t *testing.T) {
	tests := map[string]logger.LogLevel{
		"silent": logger.Silent,
		"error":  logger.Error,
		"warn":   logger.Warn,
		"info":   logger.Info,
	}
	for value, want := range tests {
		t.Run(value, func(t *testing.T) {
			t.Setenv("DB_LOG_LEVEL", value)
			config, err := loadDatabaseLoggerConfig()
			if err != nil {
				t.Fatalf("loadDatabaseLoggerConfig: %v", err)
			}
			if config.LogLevel != want || !config.ParameterizedQueries {
				t.Fatalf("config = %#v, want level %v with parameterization", config, want)
			}
		})
	}
}

func TestLoadDatabaseLoggerConfigRejectsUnknownLevel(t *testing.T) {
	t.Setenv("DB_LOG_LEVEL", "debug")
	_, err := loadDatabaseLoggerConfig()
	if err == nil || !strings.Contains(err.Error(), "silent, error, warn, or info") {
		t.Fatalf("error = %v", err)
	}
}

func TestLoadPoolSettingsUsesBoundedDefaults(t *testing.T) {
	clearPoolEnvironment(t)

	settings, err := loadPoolSettings(16, 4, 5*time.Minute, 30*time.Minute)
	if err != nil {
		t.Fatalf("loadPoolSettings: %v", err)
	}
	if settings.maxOpenConnections != 16 || settings.maxIdleConnections != 4 {
		t.Fatalf("connection settings = %#v", settings)
	}
	if settings.connectionIdleTime != 5*time.Minute || settings.connectionLifetime != 30*time.Minute {
		t.Fatalf("duration settings = %#v", settings)
	}
}

func TestLoadPoolSettingsUsesValidatedOverrides(t *testing.T) {
	clearPoolEnvironment(t)
	t.Setenv("DB_MAX_OPEN_CONNS", "9")
	t.Setenv("DB_MAX_IDLE_CONNS", "3")
	t.Setenv("DB_CONN_MAX_IDLE_TIME", "45s")
	t.Setenv("DB_CONN_MAX_LIFETIME", "12m")

	settings, err := loadPoolSettings(16, 4, 5*time.Minute, 30*time.Minute)
	if err != nil {
		t.Fatalf("loadPoolSettings: %v", err)
	}
	if settings.maxOpenConnections != 9 || settings.maxIdleConnections != 3 ||
		settings.connectionIdleTime != 45*time.Second || settings.connectionLifetime != 12*time.Minute {
		t.Fatalf("settings = %#v", settings)
	}
}

func TestLoadPoolSettingsRejectsUnsafeValues(t *testing.T) {
	tests := []struct {
		name  string
		env   string
		value string
		want  string
	}{
		{name: "zero open", env: "DB_MAX_OPEN_CONNS", value: "0", want: "positive integer"},
		{name: "negative idle", env: "DB_MAX_IDLE_CONNS", value: "-1", want: "non-negative integer"},
		{name: "bad idle duration", env: "DB_CONN_MAX_IDLE_TIME", value: "soon", want: "Go duration"},
		{name: "negative lifetime", env: "DB_CONN_MAX_LIFETIME", value: "-1s", want: "non-negative Go duration"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			clearPoolEnvironment(t)
			t.Setenv(test.env, test.value)
			_, err := loadPoolSettings(16, 4, 5*time.Minute, 30*time.Minute)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func TestLoadPoolSettingsRejectsIdleAboveOpen(t *testing.T) {
	clearPoolEnvironment(t)
	t.Setenv("DB_MAX_OPEN_CONNS", "2")
	t.Setenv("DB_MAX_IDLE_CONNS", "3")

	_, err := loadPoolSettings(16, 4, 5*time.Minute, 30*time.Minute)
	if err == nil || !strings.Contains(err.Error(), "cannot exceed") {
		t.Fatalf("error = %v", err)
	}
}

func clearPoolEnvironment(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		"DB_MAX_OPEN_CONNS",
		"DB_MAX_IDLE_CONNS",
		"DB_CONN_MAX_IDLE_TIME",
		"DB_CONN_MAX_LIFETIME",
	} {
		t.Setenv(name, "")
	}
}
