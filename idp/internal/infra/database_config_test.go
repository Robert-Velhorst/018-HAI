package infra

import (
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestPostgresConnectionStringPreservesCredentialFields(t *testing.T) {
	t.Setenv("PGSERVICE", "")
	t.Setenv("RUN_MODE", "demo")
	t.Setenv("DB_SSLMODE", "")
	for _, password := range []string{
		"synthetic-simple", "synthetic password", "synthetic'quote", `synthetic\slash`,
		"synthetic host=203.0.113.1 user=other", "synthetic&sslmode=require#fragment",
		"synthetic\npassword", "synthetic+%25:@/=?",
	} {
		t.Run(password, func(t *testing.T) {
			dsn, err := postgresConnectionString("synthetic", password, "synthetic", "127.0.0.1", 5432)
			if err != nil {
				t.Fatal("valid credential fields rejected")
			}
			cfg, err := pgx.ParseConfig(dsn)
			if err != nil {
				t.Fatal("encoded credential fields could not be parsed")
			}
			if cfg.User != "synthetic" || cfg.Password != password || cfg.Database != "synthetic" || cfg.Host != "127.0.0.1" || cfg.Port != 5432 {
				t.Fatal("credential text altered the configured PostgreSQL fields")
			}
			if cfg.ConnectTimeout != 5*time.Second || cfg.TLSConfig != nil || cfg.RuntimeParams["timezone"] != "UTC" {
				t.Fatal("credential text altered timeout, TLS or timezone settings")
			}
		})
	}
}

func TestPostgresConnectionStringEncodesUserAndDatabaseNames(t *testing.T) {
	t.Setenv("PGSERVICE", "")
	t.Setenv("RUN_MODE", "demo")
	t.Setenv("DB_SSLMODE", "")
	dsn, err := postgresConnectionString("synthetic user", "synthetic-password", "synthetic database", "127.0.0.1", 5432)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil || cfg.User != "synthetic user" || cfg.Database != "synthetic database" {
		t.Fatal("user or database name was not preserved")
	}
}

func TestPostgresConnectionStringRejectsInvalidConfigurationWithoutSecrets(t *testing.T) {
	t.Setenv("RUN_MODE", "demo")
	t.Setenv("DB_SSLMODE", "")
	for _, tc := range []struct {
		user, database, host string
		port                 int
	}{
		{"", "synthetic", "127.0.0.1", 5432},
		{"synthetic", "", "127.0.0.1", 5432},
		{"synthetic", "synthetic", " ", 5432},
		{"synthetic", "synthetic", "127.0.0.1", 0},
		{"synthetic", "synthetic", "127.0.0.1", 65536},
	} {
		_, err := postgresConnectionString(tc.user, "synthetic-private-password", tc.database, tc.host, tc.port)
		if err == nil || strings.Contains(err.Error(), "synthetic-private-password") {
			t.Fatal("invalid database configuration was accepted or exposed a credential")
		}
		db, err := NewPostgresDatabase(tc.user, "synthetic-private-password", tc.database, tc.host, tc.port)
		if db != nil || err == nil || strings.Contains(err.Error(), "synthetic-private-password") {
			t.Fatal("database constructor did not reject invalid configuration safely before connecting")
		}
	}
}

func TestIDPPostgresSSLModeConfiguration(t *testing.T) {
	t.Setenv("PGSERVICE", "")
	for _, tc := range []struct {
		name, runMode, sslMode, host, localComposeMarker string
		wantErr, wantTLS                                 bool
	}{
		{"local compose production database", "production", "disable", "postgres-idp", "true", false, false},
		{"local compose requires marker", "production", "disable", "postgres-idp", "", true, false},
		{"local compose wrong marker", "production", "disable", "postgres-idp", "false", true, false},
		{"local compose rejects backend database", "production", "disable", "postgres-automation", "true", true, false},
		{"local compose marker rejects remote host", "production", "disable", "db.example.net", "true", true, false},
		{"local explicit non-production disable", "demo", "disable", "127.0.0.1", "", false, false},
		{"unset mode defaults to production", "", "", "db.example.net", "", true, false},
		{"unset mode rejects remote plaintext", "", "disable", "db.example.net", "", true, false},
		{"production requires mode", "production", "", "db.example.net", "", true, false},
		{"production rejects disable", "production", "disable", "db.example.net", "", true, false},
		{"production rejects unverified TLS", "production", "require", "db.example.net", "", true, false},
		{"production verify ca", "production", "verify-ca", "db.example.net", "", false, true},
		{"production verify full", "production", "verify-full", "db.example.net", "", false, true},
		{"unknown mode fails closed", "staging", "disable", "db.example.net", "", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("RUN_MODE", tc.runMode)
			t.Setenv("DB_SSLMODE", tc.sslMode)
			t.Setenv("HAI_LOCAL_COMPOSE_DATABASE", tc.localComposeMarker)
			dsn, err := postgresConnectionString("synthetic", "secret", "synthetic", tc.host, 5432)
			if (err != nil) != tc.wantErr {
				t.Fatalf("connection string error = %v", err)
			}
			if err == nil {
				cfg, parseErr := pgx.ParseConfig(dsn)
				if parseErr != nil || (cfg.TLSConfig != nil) != tc.wantTLS {
					t.Fatalf("TLS config = %v, parse error = %v", cfg.TLSConfig, parseErr)
				}
			}
		})
	}
}
