package llm

import (
	"automation-hub-backend/internal/models"
	"testing"
	"time"
)

func TestModelMaintenanceSchedulerDefaultsAndExplicitDisable(t *testing.T) {
	t.Setenv("LLM_MODEL_MAINTENANCE_SCHEDULER_ENABLED", "")
	if !modelMaintenanceSchedulerEnabled() {
		t.Fatal("scheduler should be enabled by default")
	}

	t.Setenv("LLM_MODEL_MAINTENANCE_SCHEDULER_ENABLED", "false")
	if modelMaintenanceSchedulerEnabled() {
		t.Fatal("scheduler should honor explicit disable")
	}
}

func TestServiceReportsWhetherAnyModelMaintenanceWorkIsEligible(t *testing.T) {
	policy := testPolicyWithoutEndpoints()
	service := &Service{policy: policy}
	if service.hasEligibleModelMaintenanceWork() {
		t.Fatal("an unconfigured model endpoint must not start maintenance work")
	}

	policy.Providers[0].EndpointURL = "http://127.0.0.1:11434"
	service.policy = policy
	if !service.hasEligibleModelMaintenanceWork() {
		t.Fatal("a configured enabled local model should allow maintenance work")
	}
}

func TestModelMaintenanceSchedulerIntervalIsBounded(t *testing.T) {
	t.Setenv(maintenanceSchedulerIntervalEnv, "")
	if interval := modelMaintenanceSchedulerInterval(); interval != time.Hour {
		t.Fatalf("default interval = %s, want 1h", interval)
	}

	for _, value := range []string{"1", "30", "99999"} {
		t.Setenv(maintenanceSchedulerIntervalEnv, value)
		if interval := modelMaintenanceSchedulerInterval(); interval != time.Hour {
			t.Fatalf("scheduler interval for %q = %s, want fixed 1h", value, interval)
		}
	}
}

func TestModelMaintenanceIntervalPreventsPerRequestRefreshLoops(t *testing.T) {
	t.Setenv("LLM_MODEL_MAINTENANCE_INTERVAL_HOURS", "")
	if interval := modelMaintenanceInterval(); interval != 24*time.Hour {
		t.Fatalf("default interval = %s, want 24h", interval)
	}

	for _, value := range []string{"0", "-5", "1", "12", "9999"} {
		t.Setenv("LLM_MODEL_MAINTENANCE_INTERVAL_HOURS", value)
		if interval := modelMaintenanceInterval(); interval != 24*time.Hour {
			t.Fatalf("interval for %q = %s, want fixed 24h", value, interval)
		}
	}
}

func TestModelMaintenanceTimeoutIsBounded(t *testing.T) {
	t.Setenv("LLM_MODEL_MAINTENANCE_TIMEOUT_SECONDS", "")
	if timeout := modelMaintenanceTimeout(); timeout != 15*time.Minute {
		t.Fatalf("default timeout = %s, want 15m", timeout)
	}

	t.Setenv("LLM_MODEL_MAINTENANCE_TIMEOUT_SECONDS", "0")
	if timeout := modelMaintenanceTimeout(); timeout != 30*time.Second {
		t.Fatalf("zero timeout = %s, want 30s minimum", timeout)
	}

	t.Setenv("LLM_MODEL_MAINTENANCE_TIMEOUT_SECONDS", "99999")
	if timeout := modelMaintenanceTimeout(); timeout != time.Hour {
		t.Fatalf("large timeout = %s, want 1h maximum", timeout)
	}
}

func TestModelMaintenanceResultReportsTheNextDailyCheck(t *testing.T) {
	t.Setenv("LLM_MODEL_MAINTENANCE_INTERVAL_HOURS", "9999")
	checkedAt := time.Date(2026, time.July, 21, 9, 0, 0, 0, time.UTC)
	result := modelMaintenanceResult(models.LLMModelMaintenance{
		ProviderID: "ollama",
		ProviderName: "Ollama",
		ModelID: "qwen2.5:7b",
		ModelName: "Qwen",
		Status: "current",
		CheckedAt: checkedAt,
	})
	if result.NextCheckDueAt == nil || !result.NextCheckDueAt.Equal(checkedAt.Add(24*time.Hour)) {
		t.Fatalf("next check due = %v, want %v", result.NextCheckDueAt, checkedAt.Add(24*time.Hour))
	}
}

func TestMaintenanceStillFreshRejectsFutureAndExpiredRecords(t *testing.T) {
	now := time.Date(2026, time.July, 21, 12, 0, 0, 0, time.UTC)
	interval := 24 * time.Hour
	if !maintenanceStillFresh(now.Add(-23*time.Hour), now, interval) {
		t.Fatal("recent maintenance record should be reusable")
	}
	if maintenanceStillFresh(now.Add(-24*time.Hour), now, interval) {
		t.Fatal("record at the daily boundary must be refreshed")
	}
	if maintenanceStillFresh(now.Add(time.Minute), now, interval) {
		t.Fatal("future-dated maintenance record must not suppress a real check")
	}
}

func TestModelMaintenanceFingerprintChangesWithTheCheckedRuntimeConfiguration(t *testing.T) {
	provider := Provider{ID: "ollama", EndpointURL: "http://host.docker.internal:11434", Local: true}
	model := Model{ID: "qwen2.5:7b"}
	baseline := modelMaintenanceFingerprint(provider, model)
	if baseline == "" || len(baseline) != 64 {
		t.Fatalf("maintenance fingerprint = %q, want a SHA-256 hex digest", baseline)
	}
	if baseline == modelMaintenanceFingerprint(Provider{ID: "ollama", EndpointURL: "http://host.docker.internal:11435", Local: true}, model) {
		t.Fatal("endpoint change must invalidate a daily maintenance record")
	}
	if baseline == modelMaintenanceFingerprint(Provider{ID: "ollama", EndpointURL: "http://host.docker.internal:11434", Local: false}, model) {
		t.Fatal("provider locality change must invalidate a daily maintenance record")
	}
	if baseline == modelMaintenanceFingerprint(provider, Model{ID: "qwen2.5:14b"}) {
		t.Fatal("model change must invalidate a daily maintenance record")
	}
}

func TestMaintenanceFailureRecordsRetryBeforeTheNextDailyCycle(t *testing.T) {
	t.Setenv("LLM_MODEL_MAINTENANCE_FAILURE_RETRY_MINUTES", "5")
	now := time.Date(2026, time.July, 21, 12, 0, 0, 0, time.UTC)
	failed := models.LLMModelMaintenance{Status: "failed", BlocksExecution: true, CheckedAt: now.Add(-4 * time.Minute)}
	if !maintenanceRecordReusable(failed, now, 24*time.Hour) {
		t.Fatal("a recent failure should observe the bounded retry cooldown")
	}
	if maintenanceRecordReusable(models.LLMModelMaintenance{Status: "failed", BlocksExecution: true, CheckedAt: now.Add(-5 * time.Minute)}, now, 24*time.Hour) {
		t.Fatal("a failure at the retry boundary must be checked again")
	}

	result := modelMaintenanceResult(failed)
	if result.NextCheckDueAt == nil || !result.NextCheckDueAt.Equal(now.Add(time.Minute)) {
		t.Fatalf("failure retry due = %v, want %v", result.NextCheckDueAt, now.Add(time.Minute))
	}
}

func TestModelMaintenanceFailureRetryIntervalIsBounded(t *testing.T) {
	t.Setenv("LLM_MODEL_MAINTENANCE_FAILURE_RETRY_MINUTES", "0")
	if interval := modelMaintenanceFailureRetryInterval(); interval != time.Minute {
		t.Fatalf("minimum retry interval = %s, want 1m", interval)
	}
	t.Setenv("LLM_MODEL_MAINTENANCE_FAILURE_RETRY_MINUTES", "999")
	if interval := modelMaintenanceFailureRetryInterval(); interval != time.Hour {
		t.Fatalf("maximum retry interval = %s, want 1h", interval)
	}
}

func TestMaintenanceModelIDAllowsRegistryNamespacesWithoutAcceptingURLs(t *testing.T) {
	if !validMaintenanceModelID("library/qwen2.5:7b") {
		t.Fatal("registry namespace should be valid")
	}
	for _, value := range []string{"", "../qwen", "https://example.test/model", "qwen?tag=latest", "qwen\nnext"} {
		if validMaintenanceModelID(value) {
			t.Fatalf("invalid model ID accepted: %q", value)
		}
	}
}

func TestMaintenanceEndpointKeyOnlyNormalizesTheOpenAICompatibilitySuffix(t *testing.T) {
	for _, endpoint := range []string{"http://host.docker.internal:11434", "http://host.docker.internal:11434/v1/"} {
		got, err := maintenanceEndpointKey(endpoint)
		if err != nil || got != "http://host.docker.internal:11434" {
			t.Fatalf("endpoint key for %q = %q, %v", endpoint, got, err)
		}
	}
	for _, endpoint := range []string{"https://user@example.test", "https://example.test/v1/other", "https://example.test/?token=secret"} {
		if _, err := maintenanceEndpointKey(endpoint); err == nil {
			t.Fatalf("unsafe endpoint accepted: %q", endpoint)
		}
	}
}

func TestLocalMaintenanceEndpointKeyRejectsPublicHosts(t *testing.T) {
	for _, endpoint := range []string{"https://example.test/v1", "http://192.168.1.20:11434"} {
		if _, err := localMaintenanceEndpointKey(endpoint); err == nil {
			t.Fatalf("non-local planning endpoint accepted: %q", endpoint)
		}
	}
	for _, endpoint := range []string{"http://127.0.0.1:11434/v1", "http://host.docker.internal:11434"} {
		if _, err := localMaintenanceEndpointKey(endpoint); err != nil {
			t.Fatalf("local planning endpoint rejected: %q: %v", endpoint, err)
		}
	}
}

func TestMiniSWEOllamaEndpointIsExactAndCannotAliasAnotherLocalModelServer(t *testing.T) {
	if !isMiniSWEOllamaEndpoint("http://ollama-miniswe:11434/") {
		t.Fatal("isolated mini-SWE Ollama endpoint was rejected")
	}
	for _, endpoint := range []string{"http://host.docker.internal:11434", "http://ollama:11434", "https://ollama-miniswe:11434"} {
		if isMiniSWEOllamaEndpoint(endpoint) {
			t.Fatalf("non-isolated endpoint accepted: %q", endpoint)
		}
	}
}
