package config

import (
	"strings"
	"testing"
)

func setValidPostgresEnvironment(t *testing.T) {
	t.Helper()
	t.Setenv(dbPort, "5432")
	t.Setenv(dbHost, "postgres-idp")
	t.Setenv(dbName, "idp")
	t.Setenv(userDb, "postgres")
	t.Setenv(passwordDb, strings.Repeat("a", 32))
	t.Setenv(runMode, "production")
}

func TestPostgresConfigRejectsInvalidPort(t *testing.T) {
	setValidPostgresEnvironment(t)
	t.Setenv(dbPort, "0")

	if _, err := newPostgresConfig(); err == nil || !strings.Contains(err.Error(), "Port 0 is not valid") {
		t.Fatalf("expected invalid port error, got %v", err)
	}
}

func TestPostgresConfigRequiresIdentityAndPassword(t *testing.T) {
	for _, test := range []struct {
		name string
		env  string
	}{
		{name: "user", env: userDb},
		{name: "password", env: passwordDb},
	} {
		t.Run(test.name, func(t *testing.T) {
			setValidPostgresEnvironment(t)
			t.Setenv(test.env, "")
			if _, err := newPostgresConfig(); err == nil {
				t.Fatalf("expected missing %s to fail", test.name)
			}
		})
	}
}

func TestPostgresConfigRejectsWeakProductionPassword(t *testing.T) {
	for _, password := range []string{
		"postgres",
		"short-password",
		"change-this-database-owner-password",
		"an-example-password-that-is-long",
	} {
		t.Run(password, func(t *testing.T) {
			setValidPostgresEnvironment(t)
			t.Setenv(passwordDb, password)
			if _, err := newPostgresConfig(); err == nil || !strings.Contains(err.Error(), "non-placeholder secret") {
				t.Fatalf("expected weak production password to fail, got %v", err)
			}
		})
	}
}

func TestPostgresConfigAcceptsGeneratedProductionPassword(t *testing.T) {
	setValidPostgresEnvironment(t)
	config, err := newPostgresConfig()
	if err != nil {
		t.Fatalf("expected generated password to pass: %v", err)
	}
	if config.User != "postgres" || len(config.Password) != 32 {
		t.Fatalf("unexpected config: %#v", config)
	}
}

func TestPostgresConfigAllowsExplicitNonProductionFixturePassword(t *testing.T) {
	setValidPostgresEnvironment(t)
	t.Setenv(runMode, "test")
	t.Setenv(passwordDb, "postgres")

	if _, err := newPostgresConfig(); err != nil {
		t.Fatalf("test fixture password should remain available outside production: %v", err)
	}
}
