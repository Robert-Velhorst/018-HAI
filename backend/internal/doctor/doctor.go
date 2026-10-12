// Package doctor provides a self-diagnostic ("doctor") report over the loaded
// configuration. The diagnosis is a pure function so it can be unit-tested
// without environment variables, a database, or the HTTP server, and it is
// wired to the `doctor` subcommand of the backend binary.
package doctor

import (
	"fmt"
	"io"
	"strings"

	"automation-hub-backend/internal/config"
	"automation-hub-backend/internal/demomode"
)

// Severity classifies a single diagnostic check.
type Severity string

const (
	// SeverityOK means the setting is present and safe.
	SeverityOK Severity = "ok"
	// SeverityWarn means the setting is usable but risky or non-production.
	SeverityWarn Severity = "warn"
	// SeverityFail means the setting is missing/invalid and will break startup
	// or a core capability.
	SeverityFail Severity = "fail"
)

// Check is one diagnostic result.
type Check struct {
	Name     string   `json:"name"`
	Severity Severity `json:"severity"`
	Detail   string   `json:"detail"`
}

// Report is the full diagnostic result.
type Report struct {
	Checks []Check `json:"checks"`
}

// Counts returns the number of ok/warn/fail checks.
func (r Report) Counts() (ok, warn, fail int) {
	for _, c := range r.Checks {
		switch c.Severity {
		case SeverityOK:
			ok++
		case SeverityWarn:
			warn++
		case SeverityFail:
			fail++
		}
	}
	return ok, warn, fail
}

// HasFailures reports whether any check failed.
func (r Report) HasFailures() bool {
	_, _, fail := r.Counts()
	return fail > 0
}

// ExitCode returns 1 when any check failed, else 0, for CLI use.
func (r Report) ExitCode() int {
	if r.HasFailures() {
		return 1
	}
	return 0
}

// Diagnose inspects a loaded configuration and returns a readiness report.
// It performs no I/O: it only reasons about configuration values, so failures
// are deterministic and safe to assert in tests.
func Diagnose(cfg config.Configuration) Report {
	checks := make([]Check, 0, 12)
	add := func(name string, sev Severity, detail string) {
		checks = append(checks, Check{Name: name, Severity: sev, Detail: detail})
	}

	// Server / gateway.
	if trimmedPort(cfg.ServerPort) == "" {
		add("server.port", SeverityFail, "SERVER_PORT resolved to an empty listen address")
	} else {
		add("server.port", SeverityOK, "listen "+cfg.ServerPort)
	}
	if strings.TrimSpace(cfg.BaseUrl) == "" {
		add("server.baseUrl", SeverityWarn, "BASE_URL is empty; routes will mount at the root")
	} else {
		add("server.baseUrl", SeverityOK, cfg.BaseUrl)
	}

	// Database.
	if strings.TrimSpace(cfg.DbHost) == "" {
		add("database.host", SeverityFail, "DB_HOST is empty")
	} else {
		add("database.host", SeverityOK, cfg.DbHost)
	}
	if cfg.DbPort <= 0 || cfg.DbPort > 65535 {
		add("database.port", SeverityFail, fmt.Sprintf("DB_PORT %d is out of range 1-65535", cfg.DbPort))
	} else {
		add("database.port", SeverityOK, fmt.Sprintf("%d", cfg.DbPort))
	}
	if strings.TrimSpace(cfg.DbName) == "" {
		add("database.name", SeverityFail, "DB_NAME is empty")
	} else {
		add("database.name", SeverityOK, cfg.DbName)
	}
	if strings.TrimSpace(cfg.DbUser) == "" {
		add("database.user", SeverityFail, "DB_USER is empty")
	} else {
		add("database.user", SeverityOK, cfg.DbUser)
	}
	production := demomode.Parse(cfg.RunMode).IsProduction()
	switch {
	case strings.TrimSpace(cfg.DbPassword) == "":
		add("database.password", SeverityWarn, "DB_PASSWORD is empty; acceptable only with local trust authentication")
	case strings.EqualFold(strings.TrimSpace(cfg.DbPassword), "postgres"):
		severity := SeverityWarn
		if production {
			severity = SeverityFail
		}
		add("database.password", severity, "DB_PASSWORD still holds the shipped default credential; generate a real database password")
	case IsPlaceholderSecret(cfg.DbPassword):
		severity := SeverityWarn
		if production {
			severity = SeverityFail
		}
		add("database.password", severity, "DB_PASSWORD still holds a shipped placeholder value; generate a real database password")
	default:
		add("database.password", SeverityOK, "set")
	}

	// Security-sensitive keys. A shipped placeholder is not a secret: treating
	// "change-this-..." as OK is how a default credential reaches production,
	// so it is reported as loudly as an empty one.
	secretCheck := func(name, envVar, value, emptyDetail string) {
		switch {
		case strings.TrimSpace(value) == "":
			add(name, SeverityWarn, emptyDetail)
		case IsPlaceholderSecret(value):
			severity := SeverityWarn
			if production {
				severity = SeverityFail
			}
			add(name, severity, envVar+" still holds a shipped placeholder value; generate a real secret (openssl rand -hex 32)")
		default:
			add(name, SeverityOK, "set")
		}
	}
	secretCheck("security.backendApiKey", "BACKEND_API_SHARED_KEY", cfg.BackendAPIKey,
		"BACKEND_API_SHARED_KEY is empty; the API is unauthenticated and must stay on a trusted local network only")
	secretCheck("security.memoryEncryptionKey", "HAI_MEMORY_ENCRYPTION_KEY", cfg.MemoryEngineKey,
		"HAI_MEMORY_ENCRYPTION_KEY is empty; memory-engine falls back to the backend API key")
	secretCheck("security.jwtSecret", "JWT_SECRET", cfg.JWTSecret,
		"JWT_SECRET is empty; issued tokens cannot be verified")
	approvalProofKey := strings.TrimSpace(cfg.ApprovalProofSigningKey)
	switch {
	case approvalProofKey == "":
		add("security.approvalProofSigningKey", SeverityFail,
			"HAI_APPROVAL_PROOF_SIGNING_KEY is empty; controlled automation approval capabilities cannot be issued")
	case len([]byte(approvalProofKey)) < 32:
		add("security.approvalProofSigningKey", SeverityFail,
			"HAI_APPROVAL_PROOF_SIGNING_KEY must contain at least 32 bytes")
	case IsPlaceholderSecret(approvalProofKey):
		severity := SeverityWarn
		if production {
			severity = SeverityFail
		}
		add("security.approvalProofSigningKey", severity,
			"HAI_APPROVAL_PROOF_SIGNING_KEY still holds a shipped placeholder value; generate a real secret (openssl rand -hex 32)")
	default:
		add("security.approvalProofSigningKey", SeverityOK, "set")
	}

	// Event bus.
	if countNonEmpty(cfg.Brokers) == 0 {
		add("kafka.brokers", SeverityWarn, "KAFKA_BROKERS is empty; event publishing is disabled")
	} else {
		add("kafka.brokers", SeverityOK, fmt.Sprintf("%d broker(s)", countNonEmpty(cfg.Brokers)))
	}
	if strings.TrimSpace(cfg.Topic) == "" {
		add("kafka.topic", SeverityWarn, "KAFKA_TOPIC is empty")
	} else {
		add("kafka.topic", SeverityOK, cfg.Topic)
	}

	// Media storage.
	if strings.TrimSpace(cfg.ImageSaveDir) == "" {
		add("media.saveDir", SeverityFail, "IMAGE_SAVE_DIR is empty; uploads cannot be stored")
	} else {
		add("media.saveDir", SeverityOK, cfg.ImageSaveDir)
	}
	if cfg.ImageMaxSize <= 0 {
		add("media.maxSize", SeverityWarn, "IMAGE_MAX_SIZE_IN_MB resolved to a non-positive limit")
	} else {
		add("media.maxSize", SeverityOK, fmt.Sprintf("%d bytes", cfg.ImageMaxSize))
	}

	// Run mode: production performs real side effects; demo/test are labelled and
	// side-effect free, which is a warning if seen where production is expected.
	if mode := demomode.Parse(cfg.RunMode); mode.IsProduction() {
		add("runtime.mode", SeverityOK, "production")
	} else {
		add("runtime.mode", SeverityWarn, "running in "+string(mode)+" mode; real side effects are disabled")
	}

	return Report{Checks: checks}
}

// Render writes a human-readable diagnostic report to w and returns the report's
// exit code, so a caller can `os.Exit(doctor.Render(os.Stdout, report))`.
func Render(w io.Writer, r Report) int {
	ok, warn, fail := r.Counts()
	fmt.Fprintln(w, "HAI backend doctor")
	fmt.Fprintln(w, "==================")
	for _, c := range r.Checks {
		fmt.Fprintf(w, "[%-4s] %-28s %s\n", strings.ToUpper(string(c.Severity)), c.Name, c.Detail)
	}
	fmt.Fprintf(w, "\n%d ok, %d warn, %d fail\n", ok, warn, fail)
	if fail > 0 {
		fmt.Fprintln(w, "readiness: NOT READY (resolve failures above)")
	} else if warn > 0 {
		fmt.Fprintln(w, "readiness: READY WITH WARNINGS")
	} else {
		fmt.Fprintln(w, "readiness: READY")
	}
	return r.ExitCode()
}

// placeholderSecretMarkers are substrings that only ever appear in a value that
// was copied from an example file and never replaced.
var placeholderSecretMarkers = []string{
	"change-this",
	"changeme",
	"change-me",
	"replace-me",
	"replace-this",
	"your-secret",
	"example",
	"placeholder",
	"insert-",
	"todo",
	"xxx",
}

// IsPlaceholderSecret reports whether value is a shipped example credential
// rather than a real one. It is deliberately substring-based: the failure mode
// worth catching is an operator who copied .env.example and booted it.
func IsPlaceholderSecret(value string) bool {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if normalized == "" {
		return false
	}
	for _, marker := range placeholderSecretMarkers {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	return false
}

func trimmedPort(port string) string {
	return strings.TrimSpace(strings.TrimPrefix(port, ":"))
}

func countNonEmpty(values []string) int {
	n := 0
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			n++
		}
	}
	return n
}
