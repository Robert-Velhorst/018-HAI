package authentication

import (
	"errors"
	"net"
	"net/url"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

const idpAuthTestDatabase = "hai_idp_auth_test"

// Validate both the URL and pgx's effective targets before any database opens.
// Query overrides, PG* defaults and TLS fallback targets must not escape the gate.
func validateIDPAuthTestDatabaseURL(dsn, optIn string) error {
	if optIn != "true" {
		return errors.New("IDP database tests require explicit destructive-test opt-in")
	}
	parsed, err := url.Parse(dsn)
	if err != nil || parsed == nil || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") ||
		parsed.Path != "/"+idpAuthTestDatabase || parsed.Fragment != "" {
		return errors.New("IDP database tests require a PostgreSQL URL for the dedicated test database")
	}
	ip := net.ParseIP(parsed.Hostname())
	if ip == nil || !ip.IsLoopback() {
		return errors.New("IDP database tests require a literal loopback host")
	}
	config, err := pgx.ParseConfig(dsn)
	if err != nil {
		return errors.New("IDP test database URL is invalid")
	}
	effectiveIP := net.ParseIP(config.Host)
	if config.Database != idpAuthTestDatabase || effectiveIP == nil || !effectiveIP.IsLoopback() || config.Host != parsed.Hostname() {
		return errors.New("IDP effective database target does not match the dedicated loopback URL")
	}
	for _, fallback := range config.Fallbacks {
		fallbackIP := net.ParseIP(fallback.Host)
		if fallbackIP == nil || !fallbackIP.IsLoopback() || fallback.Host != config.Host {
			return errors.New("IDP database tests do not allow alternate fallback hosts")
		}
	}
	return nil
}

func TestIDPAuthDatabaseGuardRejectsUnsafeTargetsWithoutConnecting(t *testing.T) {
	for _, key := range []string{"PGHOST", "PGPORT", "PGDATABASE", "PGUSER", "PGPASSWORD", "PGPASSFILE", "PGSERVICE", "PGSERVICEFILE", "PGSSLMODE", "PGSSLROOTCERT", "PGSSLCERT", "PGSSLKEY", "PGSSLPASSWORD", "PGCONNECT_TIMEOUT", "PGTARGETSESSIONATTRS", "PGAPPNAME"} {
		t.Setenv(key, "")
	}
	for _, tc := range []struct {
		name, dsn, optIn string
		allowed          bool
	}{
		{"IPv4", "postgres://127.0.0.1/hai_idp_auth_test", "true", true},
		{"IPv6", "postgresql://[::1]/hai_idp_auth_test", "true", true},
		{"missing opt-in", "postgres://127.0.0.1/hai_idp_auth_test", "", false},
		{"nonliteral opt-in", "postgres://127.0.0.1/hai_idp_auth_test", "1", false},
		{"wrong database", "postgres://127.0.0.1/postgres", "true", false},
		{"hostname", "postgres://localhost/hai_idp_auth_test", "true", false},
		{"remote", "postgres://192.0.2.1/hai_idp_auth_test", "true", false},
		{"Unix socket", "postgres:///hai_idp_auth_test?host=/tmp", "true", false},
		{"host override", "postgres://127.0.0.1/hai_idp_auth_test?host=192.0.2.1", "true", false},
		{"database override", "postgres://127.0.0.1/hai_idp_auth_test?dbname=postgres", "true", false},
		{"remote fallback", "postgres://127.0.0.1/hai_idp_auth_test?host=127.0.0.1,192.0.2.1", "true", false},
		{"local fallback", "postgres://127.0.0.1/hai_idp_auth_test?host=127.0.0.1,127.0.0.2", "true", false},
		{"keyword DSN", "host=127.0.0.1 dbname=hai_idp_auth_test", "true", false},
		{"invalid URL", "postgres://%zz/hai_idp_auth_test", "true", false},
		{"missing database", "postgres://127.0.0.1/", "true", false},
		{"missing host", "postgres:///hai_idp_auth_test", "true", false},
		{"invalid port", "postgres://127.0.0.1:bad/hai_idp_auth_test", "true", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validateIDPAuthTestDatabaseURL(tc.dsn, tc.optIn)
			if tc.allowed {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}
