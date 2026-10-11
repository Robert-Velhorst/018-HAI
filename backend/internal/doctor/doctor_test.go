package doctor

import (
	"bytes"
	"strings"
	"testing"

	"automation-hub-backend/internal/config"
)

func healthyConfig() config.Configuration {
	return config.Configuration{
		BaseUrl:                 "/api",
		ServerPort:              ":80",
		DbHost:                  "postgres-automation",
		DbPort:                  5432,
		DbName:                  "automation",
		DbUser:                  "hai_runtime",
		DbPassword:              "0123456789abcdef0123456789abcdef",
		ImageMaxSize:            5 * 1024 * 1024,
		ImageSaveDir:            "images",
		Brokers:                 []string{"kafka1:9092", "kafka2:9093"},
		Topic:                   "automation-events",
		BackendAPIKey:           "0123456789abcdef0123456789abcdef",
		MemoryEngineKey:         "abcdef0123456789abcdef0123456789",
		JWTSecret:               "89abcdef0123456789abcdef01234567",
		ApprovalProofSigningKey: strings.Repeat("k", 32),
	}
}

func find(r Report, name string) (Check, bool) {
	for _, c := range r.Checks {
		if c.Name == name {
			return c, true
		}
	}
	return Check{}, false
}

func TestDiagnoseHealthyConfigHasNoFailures(t *testing.T) {
	r := Diagnose(healthyConfig())
	if r.HasFailures() {
		t.Fatalf("healthy config reported failures: %+v", r.Checks)
	}
	ok, warn, fail := r.Counts()
	if fail != 0 || warn != 0 {
		t.Fatalf("healthy config counts = %d ok / %d warn / %d fail, want no warn/fail", ok, warn, fail)
	}
	if r.ExitCode() != 0 {
		t.Fatalf("healthy exit code = %d, want 0", r.ExitCode())
	}
}

func TestDiagnoseFlagsMissingDatabaseAsFailure(t *testing.T) {
	cfg := healthyConfig()
	cfg.DbHost = ""
	cfg.DbName = "   "
	r := Diagnose(cfg)
	if !r.HasFailures() || r.ExitCode() != 1 {
		t.Fatalf("missing DB should fail; hasFailures=%v exit=%d", r.HasFailures(), r.ExitCode())
	}
	if c, _ := find(r, "database.host"); c.Severity != SeverityFail {
		t.Fatalf("database.host severity = %s, want fail", c.Severity)
	}
	if c, _ := find(r, "database.name"); c.Severity != SeverityFail {
		t.Fatalf("database.name severity = %s, want fail", c.Severity)
	}
}

func TestDiagnoseInvalidPortFails(t *testing.T) {
	cfg := healthyConfig()
	cfg.DbPort = 70000
	r := Diagnose(cfg)
	if c, _ := find(r, "database.port"); c.Severity != SeverityFail {
		t.Fatalf("out-of-range port severity = %s, want fail", c.Severity)
	}
}

func TestDiagnoseEmptySecretsWarnButDoNotFail(t *testing.T) {
	cfg := healthyConfig()
	cfg.RunMode = "demo"
	cfg.BackendAPIKey = ""
	cfg.MemoryEngineKey = ""
	cfg.DbPassword = ""
	r := Diagnose(cfg)
	if r.HasFailures() {
		t.Fatalf("empty secrets must warn, not fail: %+v", r.Checks)
	}
	for _, name := range []string{"security.backendApiKey", "security.memoryEncryptionKey", "database.password"} {
		if c, ok := find(r, name); !ok || c.Severity != SeverityWarn {
			t.Fatalf("%s severity = %s (found=%v), want warn", name, c.Severity, ok)
		}
	}
}

func TestDiagnoseEmptySecuritySecretsFailInProduction(t *testing.T) {
	cfg := healthyConfig()
	cfg.RunMode = "production"
	cfg.BackendAPIKey = ""
	cfg.JWTSecret = ""

	report := Diagnose(cfg)
	for _, name := range []string{"security.backendApiKey", "security.jwtSecret"} {
		if check, ok := find(report, name); !ok || check.Severity != SeverityFail {
			t.Fatalf("%s severity = %s (found=%v), want fail", name, check.Severity, ok)
		}
	}
}

func TestDiagnoseFailsPlaceholderDatabasePasswordInProduction(t *testing.T) {
	cfg := healthyConfig()
	cfg.RunMode = "production"
	cfg.DbPassword = "change-this-local-db-password"

	r := Diagnose(cfg)
	check, ok := find(r, "database.password")
	if !ok || check.Severity != SeverityFail {
		t.Fatalf("database.password severity = %s (found=%v), want fail", check.Severity, ok)
	}
	if !r.HasFailures() {
		t.Fatalf("a placeholder production database password must fail readiness: %+v", r.Checks)
	}
}

func TestDiagnoseRejectsDefaultDatabaseCredentialsInProduction(t *testing.T) {
	cfg := healthyConfig()
	cfg.RunMode = "production"
	cfg.DbUser = "postgres"
	cfg.DbPassword = "postgres"

	report := Diagnose(cfg)
	for _, name := range []string{"database.user", "database.password"} {
		check, ok := find(report, name)
		if !ok || check.Severity != SeverityFail {
			t.Fatalf("%s severity = %s (found=%v), want fail", name, check.Severity, ok)
		}
		if strings.TrimSpace(check.Detail) == "" {
			t.Fatalf("%s failure should include remediation guidance", name)
		}
	}
}

func TestDiagnoseRequiresStrongDatabasePasswordInProduction(t *testing.T) {
	for _, password := range []string{"", "short-enough-to-be-nonempty", strings.Repeat("x", 31)} {
		cfg := healthyConfig()
		cfg.RunMode = "production"
		cfg.DbPassword = password
		report := Diagnose(cfg)
		check, ok := find(report, "database.password")
		if !ok || check.Severity != SeverityFail {
			t.Fatalf("database.password for %q severity = %s (found=%v), want fail", password, check.Severity, ok)
		}
	}
}

func TestDiagnoseAllowsLocalPostgresDefaults(t *testing.T) {
	cfg := healthyConfig()
	cfg.RunMode = "demo"
	cfg.DbUser = "postgres"
	cfg.DbPassword = "postgres"

	report := Diagnose(cfg)
	for _, name := range []string{"database.user", "database.password"} {
		check, ok := find(report, name)
		if !ok || check.Severity != SeverityOK {
			t.Fatalf("%s severity = %s (found=%v), want ok", name, check.Severity, ok)
		}
	}
}

func TestDiagnoseMissingOrWeakApprovalProofKeyFails(t *testing.T) {
	for _, value := range []string{"", "too-short"} {
		cfg := healthyConfig()
		cfg.ApprovalProofSigningKey = value
		r := Diagnose(cfg)
		check, ok := find(r, "security.approvalProofSigningKey")
		if !ok || check.Severity != SeverityFail {
			t.Fatalf("approval proof key %q severity = %s (found=%v), want fail", value, check.Severity, ok)
		}
	}
}

func TestRenderReportsReadinessAndExitCode(t *testing.T) {
	var buf bytes.Buffer
	code := Render(&buf, Diagnose(healthyConfig()))
	if code != 0 {
		t.Fatalf("render exit code = %d, want 0", code)
	}
	out := buf.String()
	if !strings.Contains(out, "readiness: READY") {
		t.Fatalf("render output missing readiness line:\n%s", out)
	}

	buf.Reset()
	cfg := healthyConfig()
	cfg.DbHost = ""
	code = Render(&buf, Diagnose(cfg))
	if code != 1 || !strings.Contains(buf.String(), "NOT READY") {
		t.Fatalf("failing render code=%d, want 1 with NOT READY:\n%s", code, buf.String())
	}
}
