package source

import (
	"strings"
	"testing"

	"automation-hub-backend/internal/pgtestguard"
)

func TestTrelloPostgresDedicatedTargetGuardBeforeConnection(t *testing.T) {
	valid := "postgres://tester:synthetic-secret@127.0.0.1:5432/hai_migration_runner_test?sslmode=disable"
	for _, tc := range []struct {
		name, dsn string
		accepted  bool
	}{
		{"IPv4", valid, true},
		{"IPv6", strings.ReplaceAll(valid, "127.0.0.1", "[::1]"), true},
		{"hostname", strings.ReplaceAll(valid, "127.0.0.1", "localhost"), false},
		{"remote", strings.ReplaceAll(valid, "127.0.0.1", "192.0.2.10"), false},
		{"lookalike database", strings.ReplaceAll(valid, "hai_migration_runner_test", "hai_migration_runner_test_shadow"), false},
		{"unrelated test database", strings.ReplaceAll(valid, "hai_migration_runner_test", "unrelated_test"), false},
		{"socket", "host=/tmp dbname=hai_migration_runner_test user=tester", false},
		{"remote fallback", "host=127.0.0.1,192.0.2.10 dbname=hai_migration_runner_test user=tester", false},
		{"malformed", "postgres://tester:synthetic-secret@%", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := pgtestguard.ValidateDedicatedPostgresTestDSN(tc.dsn, "hai_migration_runner_test")
			if (err == nil) != tc.accepted {
				t.Fatalf("dedicated target acceptance=%t want=%t", err == nil, tc.accepted)
			}
			if err != nil && (strings.Contains(err.Error(), "synthetic-secret") || strings.Contains(err.Error(), "unrelated_test")) {
				t.Fatal("guard disclosed supplied configuration")
			}
		})
	}
}
