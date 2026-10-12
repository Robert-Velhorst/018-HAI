package llm

import (
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"automation-hub-backend/internal/models"
)

func TestPaidProviderMaintenancePolicyAndBudgetRetry(t *testing.T) {
	t.Setenv("LLM_MODEL_MAINTENANCE_ENABLED", "true")

	tests := []struct {
		name         string
		policy       Policy
		wantEligible int
		wantStatus   string
		wantRetry    time.Duration
		wantRequests int32
		wantUpdate   bool
	}{
		{
			name:         "paid calls disabled skips provider",
			policy:       Policy{PaidCallsAllowed: false, DailyPaidBudgetEUR: 10},
			wantEligible: 0,
			wantRequests: 0,
		},
		{
			name:         "approval required is not probed",
			policy:       Policy{PaidCallsAllowed: true, DailyPaidBudgetEUR: 10, RequireApprovalBeforePaidUsage: true},
			wantEligible: 1,
			wantStatus:   "approval_required",
			wantRetry:    24 * time.Hour,
			wantRequests: 0,
		},
		{
			name:         "exhausted budget uses short retry",
			policy:       Policy{PaidCallsAllowed: true, DailyPaidBudgetEUR: 10, DailyBudgetUsedEUR: 10},
			wantEligible: 1,
			wantStatus:   "failed",
			wantRetry:    modelMaintenanceFailureRetryInterval(),
			wantRequests: 0,
		},
		{
			name:         "unavailable usage accounting uses short retry",
			policy:       Policy{PaidCallsAllowed: true, DailyPaidBudgetEUR: 10, UsageAccountingStatus: "unavailable"},
			wantEligible: 1,
			wantStatus:   "failed",
			wantRetry:    modelMaintenanceFailureRetryInterval(),
			wantRequests: 0,
		},
		{
			name:         "process-only usage accounting cannot authorize paid probe",
			policy:       Policy{PaidCallsAllowed: true, DailyPaidBudgetEUR: 10, UsageAccountingStatus: "process_only"},
			wantEligible: 1,
			wantStatus:   "failed",
			wantRetry:    modelMaintenanceFailureRetryInterval(),
			wantRequests: 0,
		},
		{
			name:         "unknown usage accounting cannot authorize paid probe",
			policy:       Policy{PaidCallsAllowed: true, DailyPaidBudgetEUR: 10, UsageAccountingStatus: "future-state"},
			wantEligible: 1,
			wantStatus:   "failed",
			wantRetry:    modelMaintenanceFailureRetryInterval(),
			wantRequests: 0,
		},
		{
			name:         "permitted paid catalog check is read only",
			policy:       Policy{PaidCallsAllowed: true, DailyPaidBudgetEUR: 10, UsageAccountingStatus: "durable"},
			wantEligible: 1,
			wantStatus:   "provider_managed",
			wantRetry:    24 * time.Hour,
			wantRequests: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if r.Method != http.MethodGet || r.URL.Path != "/v1/models" {
					t.Errorf("paid provider request = %s %s; want read-only GET /v1/models", r.Method, r.URL.Path)
					http.Error(w, "unexpected request", http.StatusMethodNotAllowed)
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]string{{"id": "configured-model"}}})
			}))
			defer server.Close()

			provider := Provider{
				ID: "paid-test", Name: "Paid test provider", Enabled: true, Paid: true,
				EndpointURL: server.URL,
				Models:      []Model{{ID: "configured-model", Name: "Configured model", Enabled: true}},
			}
			policy := tt.policy
			policy.Providers = []Provider{provider}
			history := &fakeModelMaintenanceRepository{}
			service := &Service{policy: policy, maintenanceHistory: history}

			run := service.RunDueModelMaintenance()
			if run.Eligible != tt.wantEligible {
				t.Fatalf("eligible providers = %d, want %d; run=%#v", run.Eligible, tt.wantEligible, run)
			}
			if got := requests.Load(); got != tt.wantRequests {
				t.Fatalf("paid provider requests = %d, want %d", got, tt.wantRequests)
			}
			if tt.wantEligible == 0 {
				if len(run.Results) != 0 {
					t.Fatalf("maintenance results = %#v, want none for a policy-denied provider", run.Results)
				}
				return
			}
			if len(run.Results) != 1 || run.Results[0].Status != tt.wantStatus || !run.Results[0].BlocksExecution && tt.wantStatus != "provider_managed" {
				t.Fatalf("maintenance result = %#v, want status %q with policy-appropriate execution gate", run.Results, tt.wantStatus)
			}
			result := run.Results[0]
			if result.UpdateAttempted || result.UpdateApplied {
				t.Fatalf("paid provider maintenance claimed an update: %#v", result)
			}
			if result.NextCheckDueAt == nil || !result.NextCheckDueAt.Equal(result.CheckedAt.Add(tt.wantRetry)) {
				t.Fatalf("next check = %v, checked at = %s, want retry after %s", result.NextCheckDueAt, result.CheckedAt, tt.wantRetry)
			}

			history.mu.Lock()
			if len(history.records) != 1 {
				history.mu.Unlock()
				t.Fatalf("persisted maintenance results = %d, want one", len(history.records))
			}
			record := history.records[0]
			history.mu.Unlock()
			if record.Status != tt.wantStatus || record.CheckedAt != result.CheckedAt {
				t.Fatalf("persisted maintenance record = %#v, result = %#v", record, result)
			}
			if tt.wantRetry == modelMaintenanceFailureRetryInterval() {
				if !maintenanceRecordReusable(record, result.CheckedAt.Add(tt.wantRetry-time.Nanosecond), modelMaintenanceInterval()) {
					t.Fatal("budget-blocked result was not reused during its bounded retry window")
				}
				if maintenanceRecordReusable(record, result.CheckedAt.Add(tt.wantRetry), modelMaintenanceInterval()) {
					t.Fatal("budget-blocked result suppressed a retry at the bounded retry deadline")
				}
			}
		})
	}
}

func TestPaidProviderProbeBudgetRequiresDurableFiniteNonnegativeAccounting(t *testing.T) {
	tests := []struct {
		name   string
		policy Policy
		want   bool
	}{
		{name: "durable budget remains", policy: Policy{DailyPaidBudgetEUR: 10, DailyBudgetUsedEUR: 9.99, UsageAccountingStatus: "durable"}, want: true},
		{name: "exactly exhausted", policy: Policy{DailyPaidBudgetEUR: 10, DailyBudgetUsedEUR: 10, UsageAccountingStatus: "durable"}},
		{name: "process only", policy: Policy{DailyPaidBudgetEUR: 10, UsageAccountingStatus: "process_only"}},
		{name: "unavailable", policy: Policy{DailyPaidBudgetEUR: 10, UsageAccountingStatus: "unavailable"}},
		{name: "missing accounting state", policy: Policy{DailyPaidBudgetEUR: 10}},
		{name: "negative usage", policy: Policy{DailyPaidBudgetEUR: 10, DailyBudgetUsedEUR: -1, UsageAccountingStatus: "durable"}},
		{name: "NaN limit", policy: Policy{DailyPaidBudgetEUR: math.NaN(), UsageAccountingStatus: "durable"}},
		{name: "infinite usage", policy: Policy{DailyPaidBudgetEUR: 10, DailyBudgetUsedEUR: math.Inf(1), UsageAccountingStatus: "durable"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := paidProviderProbeBudgetAvailable(tt.policy); got != tt.want {
				t.Fatalf("paidProviderProbeBudgetAvailable(%#v) = %t, want %t", tt.policy, got, tt.want)
			}
		})
	}
}

func TestMaintenanceResultProvenanceRequiresVerifiedInstalledDigest(t *testing.T) {
	checkedAt := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	record := models.LLMModelMaintenance{
		ProviderID: "ollama", ModelID: "qwen2.5:7b", Status: "updated",
		PreviousDigest: "sha256:before", CurrentDigest: "sha256:after",
		UpdateAttempted: true, UpdateApplied: true, CheckedAt: checkedAt,
	}
	result := modelMaintenanceResult(record)
	if result.Status != "updated" || result.PreviousDigest != "sha256:before" || result.CurrentDigest != "sha256:after" || !result.UpdateAttempted || !result.UpdateApplied || result.BlocksExecution {
		t.Fatalf("verified digest provenance was not preserved: %#v", result)
	}

	for _, status := range []string{"failed", "cancelled", "operator_managed"} {
		blocked := modelMaintenanceResult(models.LLMModelMaintenance{
			ProviderID: "ollama", ModelID: "qwen2.5:7b", Status: status,
			PreviousDigest: "sha256:before", CurrentDigest: "sha256:observed-unverified",
			UpdateAttempted: true, CheckedAt: checkedAt,
		})
		if !blocked.BlocksExecution {
			t.Fatalf("%s result with observed but unverified digest did not block use: %#v", status, blocked)
		}
	}
}
