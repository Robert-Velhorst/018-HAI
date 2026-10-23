package openclawmaintenance

import (
	"strings"
	"testing"

	"automation-hub-backend/internal/pgtestguard"
	"github.com/jackc/pgx/v5"
)

const maintenanceTestDatabase = "hai_openclaw_maintenance_test"

func maintenanceTestDatabaseConfig(dsn string) (*pgx.ConnConfig, error) {
	if err := pgtestguard.ValidateDedicatedPostgresTestDSN(dsn, maintenanceTestDatabase); err != nil {
		return nil, err
	}
	return pgx.ParseConfig(dsn)
}

func TestMaintenancePostgresConfigurationIsDedicatedAndLoopback(t *testing.T) {
	for _, test := range []struct {
		name string
		dsn  string
		want bool
	}{
		{"keyword loopback", "host=127.0.0.1 user=test dbname=hai_openclaw_maintenance_test sslmode=disable", true},
		{"URI loopback", "postgres://test:fixture-password@127.0.0.1/hai_openclaw_maintenance_test?sslmode=disable", true},
		{"IPv6 loopback", "host=::1 user=test dbname=hai_openclaw_maintenance_test sslmode=disable", true},
		{"wrong database", "host=127.0.0.1 password=fixture-password dbname=hai_production sslmode=disable", false},
		{"remote host", "host=203.0.113.1 password=fixture-password dbname=hai_openclaw_maintenance_test sslmode=disable", false},
		{"hostname", "host=localhost password=fixture-password dbname=hai_openclaw_maintenance_test sslmode=disable", false},
		{"fallback", "host=127.0.0.1,203.0.113.1 password=fixture-password dbname=hai_openclaw_maintenance_test sslmode=disable", false},
		{"invalid", "postgres://test:fixture-password@[bad/hai_openclaw_maintenance_test", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			config, err := maintenanceTestDatabaseConfig(test.dsn)
			if (err == nil) != test.want || (err == nil && config == nil) {
				t.Fatalf("config accepted=%t want=%t err=%v", err == nil, test.want, err)
			}
			if err != nil && strings.Contains(err.Error(), "fixture-password") {
				t.Fatal("test configuration error exposed the DSN password")
			}
		})
	}
}
