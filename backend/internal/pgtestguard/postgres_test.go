package pgtestguard

import "testing"

func TestDestructiveTestOptInRequiresExplicitTrue(t *testing.T) {
	for _, value := range []string{"true", " TRUE ", "True"} {
		if !destructiveTestOptedIn(value) {
			t.Errorf("destructiveTestOptedIn(%q) = false, want true", value)
		}
	}
	for _, value := range []string{"", "false", "1", "yes", "trueish"} {
		if destructiveTestOptedIn(value) {
			t.Errorf("destructiveTestOptedIn(%q) = true, want false", value)
		}
	}
}

func TestValidateDedicatedPostgresTestDSN(t *testing.T) {
	tests := []struct {
		name    string
		dsn     string
		db      string
		wantErr bool
	}{
		{
			name: "exact dedicated database on IPv4 loopback",
			dsn:  "postgres://tester:secret@127.0.0.1:55432/hai_resilience_test?sslmode=disable",
			db:   "hai_resilience_test",
		},
		{
			name: "exact dedicated database on IPv6 loopback",
			dsn:  "postgres://tester:secret@[::1]:55432/hai_resilience_test?sslmode=disable",
			db:   "hai_resilience_test",
		},
		{
			name: "TLS fallback on the same loopback host is allowed",
			dsn:  "postgres://tester:secret@127.0.0.1:55432/hai_resilience_test?sslmode=prefer",
			db:   "hai_resilience_test",
		},
		{
			name:    "database containing expected name is rejected",
			dsn:     "postgres://tester:secret@127.0.0.1:55432/hai_resilience_test_shadow?sslmode=disable",
			db:      "hai_resilience_test",
			wantErr: true,
		},
		{
			name:    "shared test database is rejected",
			dsn:     "postgres://tester:secret@127.0.0.1:55432/shared_test?sslmode=disable",
			db:      "hai_resilience_test",
			wantErr: true,
		},
		{
			name:    "remote IP is rejected",
			dsn:     "postgres://tester:secret@192.0.2.10:5432/hai_resilience_test?sslmode=disable",
			db:      "hai_resilience_test",
			wantErr: true,
		},
		{
			name:    "hostname is rejected even when named localhost",
			dsn:     "postgres://tester:secret@localhost:55432/hai_resilience_test?sslmode=disable",
			db:      "hai_resilience_test",
			wantErr: true,
		},
		{
			name:    "multi-host destination is rejected",
			dsn:     "host=127.0.0.1,192.0.2.10 port=5432 user=tester password=secret dbname=hai_resilience_test",
			db:      "hai_resilience_test",
			wantErr: true,
		},
		{
			name:    "empty expected database is rejected",
			dsn:     "postgres://tester:secret@127.0.0.1:55432/hai_resilience_test?sslmode=disable",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateDedicatedPostgresTestDSN(tt.dsn, tt.db)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateDedicatedPostgresTestDSN() error = %v, wantErr %t", err, tt.wantErr)
			}
		})
	}
}

func TestValidateDedicatedPostgresTestDSNRejectsMalformedDSN(t *testing.T) {
	if err := ValidateDedicatedPostgresTestDSN("not a postgres dsn", "hai_resilience_test"); err == nil {
		t.Fatal("malformed DSN was accepted")
	}
}
