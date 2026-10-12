package router

import (
	"context"
	"strings"
	"testing"

	"automation-hub-backend/internal/config"
	"automation-hub-backend/internal/doctor"
)

func TestInvalidMetricsConfigRejectedBeforeSharedDatabaseAcquisition(t *testing.T) {
	previous := config.AppConfig
	t.Cleanup(func() { config.AppConfig = previous })
	config.AppConfig = config.Configuration{
		ServerPort: ":80", BaseUrl: "/api", RunMode: "demo",
		DbHost: "must-not-resolve.invalid", DbPort: 5432, DbName: "synthetic", DbUser: "synthetic",
		ImageSaveDir: "images", ApprovalProofSigningKey: strings.Repeat("a", 32),
	}
	if doctor.Diagnose(config.AppConfig).HasFailures() {
		t.Fatal("fixture does not reach metrics validation")
	}
	t.Setenv("HAI_PROMETHEUS_ENABLED", "true")
	t.Setenv("HAI_PROMETHEUS_TOKEN", "")
	// The acquisition guard would fail without opening a connection if reached.
	// The returned metrics error proves validation wins that ordering contest.
	t.Setenv("DB_STARTUP_TIMEOUT", "invalid-synthetic-duration")
	if err := initializeWithContext(context.Background()); err == nil || !strings.Contains(err.Error(), "HAI_PROMETHEUS_TOKEN") {
		t.Fatalf("startup bypassed metrics validation: %v", err)
	}
}
