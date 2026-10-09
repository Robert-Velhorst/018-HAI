package pgtestguard

import (
	"fmt"
	"net"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

const destructiveOptInEnv = "HAI_ALLOW_DESTRUCTIVE_DATABASE_TESTS"

// RequireDedicatedPostgresTestDSN skips unless a test-specific DSN and the
// explicit destructive-test opt-in are both present. Configured but unsafe
// values fail before callers can open a database connection.
func RequireDedicatedPostgresTestDSN(t testing.TB, dsnEnv, expectedDatabase string) string {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv(dsnEnv))
	if dsn == "" {
		t.Skipf("%s is not set; skipping destructive PostgreSQL test", dsnEnv)
	}
	if !destructiveTestOptedIn(os.Getenv(destructiveOptInEnv)) {
		t.Skipf("%s=true is required; skipping destructive PostgreSQL test", destructiveOptInEnv)
	}
	if err := ValidateDedicatedPostgresTestDSN(dsn, expectedDatabase); err != nil {
		t.Fatalf("refusing destructive PostgreSQL test: %v", err)
	}
	return dsn
}

func destructiveTestOptedIn(value string) bool {
	return strings.EqualFold(strings.TrimSpace(value), "true")
}

// ValidateDedicatedPostgresTestDSN enforces a literal loopback host and an
// exact database name. Hostnames and multi-host failover are rejected because
// their destination cannot be guaranteed by a local-only test guard.
func ValidateDedicatedPostgresTestDSN(dsn, expectedDatabase string) error {
	if strings.TrimSpace(dsn) == "" {
		return fmt.Errorf("test database DSN is empty")
	}
	if strings.TrimSpace(expectedDatabase) == "" {
		return fmt.Errorf("expected dedicated database identity is empty")
	}
	config, err := pgx.ParseConfig(dsn)
	if err != nil {
		return fmt.Errorf("test database DSN is invalid")
	}
	if config.Database != expectedDatabase {
		return fmt.Errorf("database identity does not exactly match the dedicated test target")
	}
	if !isLoopbackIP(config.Host) {
		return fmt.Errorf("host must be a literal loopback IP address")
	}
	for _, fallback := range config.Fallbacks {
		if fallback.Host != config.Host || !isLoopbackIP(fallback.Host) {
			return fmt.Errorf("multi-host PostgreSQL destinations are not allowed")
		}
	}
	return nil
}

func isLoopbackIP(value string) bool {
	ip := net.ParseIP(value)
	return ip != nil && ip.IsLoopback()
}
