package doctor

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"strings"
	"testing"

	"automation-hub-backend/internal/config"
)

var productionSecretFields = []struct {
	name   string
	envVar string
	set    func(*config.Configuration, string)
}{
	{"security.jwtSecret", "JWT_SECRET", func(cfg *config.Configuration, value string) { cfg.JWTSecret = value }},
	{"security.backendApiKey", "BACKEND_API_SHARED_KEY", func(cfg *config.Configuration, value string) { cfg.BackendAPIKey = value }},
	{"security.memoryEncryptionKey", "HAI_MEMORY_ENCRYPTION_KEY", func(cfg *config.Configuration, value string) { cfg.MemoryEngineKey = value }},
}

func TestDiagnoseProductionSecretMinimumBytes(t *testing.T) {
	var generated [32]byte
	if _, err := rand.Read(generated[:]); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name   string
		value  string
		want   Severity
		detail string
	}{
		{"empty", "", SeverityFail, "is empty"},
		{"whitespace", " \t\n\u2003", SeverityFail, "is empty"},
		{"one_byte", "a", SeverityFail, "must contain at least 32 bytes"},
		{"ascii_31_bytes", strings.Repeat("a", 31), SeverityFail, "must contain at least 32 bytes"},
		{"ascii_32_bytes", strings.Repeat("a", 32), SeverityOK, "set"},
		{"ascii_33_bytes", strings.Repeat("a", 33), SeverityOK, "set"},
		{"generated_hex_32_bytes", hex.EncodeToString(generated[:16]), SeverityOK, "set"},
		{"generated_hex_64_bytes", hex.EncodeToString(generated[:]), SeverityOK, "set"},
		{"padded_short", " \t" + strings.Repeat("a", 31) + "\u2003", SeverityFail, "must contain at least 32 bytes"},
		{"padded_valid", " \t" + strings.Repeat("a", 32) + "\u2003", SeverityOK, "set"},
		{"multibyte_31_bytes", strings.Repeat("\u00e9", 15) + "a", SeverityFail, "must contain at least 32 bytes"},
		{"multibyte_32_bytes", strings.Repeat("\u00e9", 16), SeverityOK, "set"},
		{"placeholder_short", "change-this", SeverityFail, "still holds a shipped placeholder value"},
		{"placeholder_long", "change-this-" + strings.Repeat("a", 32), SeverityFail, "still holds a shipped placeholder value"},
		{"placeholder_padded_uppercase", " \tCHANGE-THIS-" + strings.Repeat("a", 32) + "\n", SeverityFail, "still holds a shipped placeholder value"},
	}
	for _, field := range productionSecretFields {
		t.Run(field.envVar, func(t *testing.T) {
			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					cfg := healthyConfig()
					cfg.RunMode = "production"
					field.set(&cfg, tc.value)
					report := Diagnose(cfg)
					check, found := find(report, field.name)
					if !found || check.Severity != tc.want {
						t.Fatalf("%s severity = %s (found=%v), want %s", field.name, check.Severity, found, tc.want)
					}
					wantDetail := tc.detail
					if tc.want == SeverityFail {
						wantDetail = field.envVar + " " + wantDetail
					}
					if !strings.Contains(check.Detail, wantDetail) {
						t.Fatalf("detail = %q, want %q", check.Detail, wantDetail)
					}
					wantFailures := 0
					if tc.want == SeverityFail {
						wantFailures = 1
					}
					_, warn, fail := report.Counts()
					if warn != 0 || fail != wantFailures || report.ExitCode() != wantFailures || report.HasFailures() != (wantFailures != 0) {
						t.Fatalf("warn=%d fail=%d exit=%d hasFailures=%v, want warn=0 fail/exit=%d", warn, fail, report.ExitCode(), report.HasFailures(), wantFailures)
					}
				})
			}
		})
	}
}

func TestDiagnoseSecretRunModeSemantics(t *testing.T) {
	modes := []struct {
		name       string
		value      string
		production bool
	}{
		{"production", "production", true},
		{"default", "", true},
		{"prod_alias", "PROD", true},
		{"padded_production", " PRODUCTION ", true},
		{"unknown", "development", true},
		{"demo", "demo", false},
		{"test", "test", false},
		{"padded_demo", " DEMO ", false},
		{"mixed_case_test", "Test", false},
	}
	values := []struct {
		name  string
		value string
		dev   Severity
	}{
		{"empty", "", SeverityWarn},
		{"whitespace", " \t\u2003", SeverityWarn},
		{"short", "a", SeverityOK},
		{"placeholder", "change-this-" + strings.Repeat("a", 32), SeverityWarn},
	}
	for _, mode := range modes {
		t.Run(mode.name, func(t *testing.T) {
			for _, field := range productionSecretFields {
				t.Run(field.envVar, func(t *testing.T) {
					for _, value := range values {
						t.Run(value.name, func(t *testing.T) {
							cfg := healthyConfig()
							cfg.RunMode = mode.value
							field.set(&cfg, value.value)
							report := Diagnose(cfg)
							check, found := find(report, field.name)
							want := value.dev
							if mode.production {
								want = SeverityFail
							}
							if !found || check.Severity != want || report.HasFailures() != mode.production {
								t.Fatalf("%s severity=%s (found=%v), hasFailures=%v, want %s, hasFailures=%v", field.name, check.Severity, found, report.HasFailures(), want, mode.production)
							}
						})
					}
				})
			}
		})
	}
}

func TestRenderProductionSecretFailureDoesNotExposeValue(t *testing.T) {
	for _, field := range productionSecretFields {
		t.Run(field.envVar, func(t *testing.T) {
			for _, value := range []string{strings.Repeat("b", 31), "change-this-" + strings.Repeat("b", 32)} {
				cfg := healthyConfig()
				cfg.RunMode = "production"
				field.set(&cfg, value)
				var buf bytes.Buffer
				if code := Render(&buf, Diagnose(cfg)); code != 1 {
					t.Fatalf("render exit code = %d, want 1", code)
				}
				out := buf.String()
				if !strings.Contains(out, "readiness: NOT READY") || !strings.Contains(out, field.envVar) {
					t.Fatalf("render output missing failure or setting name:\n%s", out)
				}
				if strings.Contains(out, value) {
					t.Fatal("render output exposes secret value")
				}
			}
		})
	}
}
