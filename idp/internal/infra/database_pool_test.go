package infra

import (
	"strings"
	"testing"
	"time"
)

func TestLoadPoolSettingsUsesBoundedDefaults(t *testing.T) {
	clearPoolEnvironment(t)

	settings, err := loadPoolSettings(8, 2, 5*time.Minute, 30*time.Minute)
	if err != nil {
		t.Fatalf("loadPoolSettings: %v", err)
	}
	if settings.maxOpenConnections != 8 || settings.maxIdleConnections != 2 ||
		settings.connectionIdleTime != 5*time.Minute || settings.connectionLifetime != 30*time.Minute {
		t.Fatalf("settings = %#v", settings)
	}
}

func TestLoadPoolSettingsRejectsUnsafeValues(t *testing.T) {
	clearPoolEnvironment(t)
	t.Setenv("DB_MAX_OPEN_CONNS", "2")
	t.Setenv("DB_MAX_IDLE_CONNS", "3")

	_, err := loadPoolSettings(8, 2, 5*time.Minute, 30*time.Minute)
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
