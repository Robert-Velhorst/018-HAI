package llm

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

const dueScanOllamaCloudModelID = "qwen3-coder:480b-cloud"

func TestDueModelMaintenanceSkipsOllamaCloudTagWithoutPaidApproval(t *testing.T) {
	t.Setenv("LLM_MODEL_MAINTENANCE_ENABLED", "true")
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		t.Errorf("provider request %s %s; cloud-tagged Ollama must be rejected before maintenance", r.Method, r.URL.Path)
		http.Error(w, "unexpected provider request", http.StatusInternalServerError)
	}))
	defer server.Close()

	history := &fakeModelMaintenanceRepository{}
	service := &Service{
		policy: Policy{
			LocalModelsAllowed: true,
			Providers: []Provider{{
				ID: "ollama", Name: "Ollama", Enabled: true, Local: true,
				EndpointURL: server.URL,
				Models:      []Model{{ID: dueScanOllamaCloudModelID, Name: "Cloud tag", Enabled: true, Tier: TierLocal}},
			}},
		},
		maintenanceHistory: history,
		maintenanceRunning: map[string]*sync.Mutex{},
	}

	for runNumber := 1; runNumber <= 2; runNumber++ {
		run := service.RunDueModelMaintenance()
		if run.Eligible != 0 || run.Checked != 0 || run.Failed != 0 || len(run.Results) != 0 {
			t.Fatalf("due run %d = %#v; unpaid cloud tag must not enter local maintenance", runNumber, run)
		}
	}

	if requests != 0 {
		t.Fatalf("provider received %d requests; want none", requests)
	}
	history.mu.Lock()
	recordCount := len(history.records)
	history.mu.Unlock()
	if recordCount != 0 {
		t.Fatalf("maintenance history contains %d records; cloud tags must not create a local failure/cooldown", recordCount)
	}
	history.admissionMu.Lock()
	claimCount := len(history.admissionClaims)
	history.admissionMu.Unlock()
	if claimCount != 0 {
		t.Fatalf("maintenance history contains %d local admission claims; cloud tags must not start local pull admission", claimCount)
	}
}

func TestDueModelMaintenanceTreatsApprovedOllamaCloudTagAsProviderManaged(t *testing.T) {
	t.Setenv("LLM_MODEL_MAINTENANCE_ENABLED", "true")
	requests := 0
	pulls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method == http.MethodPost && r.URL.Path == "/api/pull" {
			pulls++
			t.Errorf("cloud-tagged model triggered local pull")
			http.Error(w, "unexpected local pull", http.StatusInternalServerError)
			return
		}
		if r.Method != http.MethodGet || r.URL.Path != "/api/tags" {
			t.Errorf("provider request = %s %s; want read-only GET /api/tags", r.Method, r.URL.Path)
			http.Error(w, "unexpected provider request", http.StatusMethodNotAllowed)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"models": []map[string]string{{"name": dueScanOllamaCloudModelID, "digest": "sha256:provider-managed"}},
		})
	}))
	defer server.Close()

	history := &leasedModelMaintenanceRepository{
		fakeModelMaintenanceRepository: &fakeModelMaintenanceRepository{},
		acquired:                       true,
	}
	service := &Service{
		policy: Policy{
			LocalModelsAllowed:             false,
			PaidCallsAllowed:               true,
			DailyPaidBudgetEUR:             1,
			UsageAccountingStatus:          "durable",
			RequireApprovalBeforePaidUsage: false,
			Providers: []Provider{{
				ID: "ollama", Name: "Ollama", Enabled: true, Local: true,
				EndpointURL: server.URL,
				Models:      []Model{{ID: dueScanOllamaCloudModelID, Name: "Cloud tag", Enabled: true, Tier: TierLocal}},
			}},
		},
		maintenanceHistory: history,
		maintenanceRunning: map[string]*sync.Mutex{},
	}

	run := service.RunDueModelMaintenance()
	if run.Eligible != 1 || run.Checked != 1 || run.ProviderManaged != 1 || run.Updated != 0 || run.Failed != 0 || len(run.Results) != 1 {
		t.Fatalf("due run = %#v; want one provider-managed, read-only catalog check", run)
	}
	result := run.Results[0]
	if result.Status != "provider_managed" || result.BlocksExecution || result.UpdateAttempted || result.UpdateApplied {
		t.Fatalf("maintenance result = %#v; want non-blocking provider-managed result without update", result)
	}
	if requests != 1 || pulls != 0 {
		t.Fatalf("provider requests=%d pulls=%d; want one catalog GET and no pull", requests, pulls)
	}
	history.fakeModelMaintenanceRepository.mu.Lock()
	defer history.fakeModelMaintenanceRepository.mu.Unlock()
	if len(history.records) != 1 || history.records[0].Status != "provider_managed" {
		t.Fatalf("persisted maintenance records = %#v; want one provider-managed record and no local failure cooldown", history.records)
	}
}

func TestDueModelMaintenanceSkipsMisconfiguredMiniSWEOllamaCloudTag(t *testing.T) {
	t.Setenv("LLM_MODEL_MAINTENANCE_ENABLED", "true")
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		t.Errorf("provider request %s %s; isolated mini-SWE must not maintain a cloud-tagged model", r.Method, r.URL.Path)
		http.Error(w, "unexpected provider request", http.StatusInternalServerError)
	}))
	defer server.Close()

	history := &fakeModelMaintenanceRepository{}
	service := &Service{
		policy: Policy{
			LocalModelsAllowed: true,
			Providers: []Provider{{
				ID: miniSWEOllamaProviderID, Name: "mini-SWE isolated Ollama", Enabled: true, Local: true,
				EndpointURL: server.URL,
				Models:      []Model{{ID: dueScanOllamaCloudModelID, Name: "Cloud tag", Enabled: true, Tier: TierLocal}},
			}},
		},
		maintenanceHistory: history,
		maintenanceRunning: map[string]*sync.Mutex{},
	}

	for runNumber := 1; runNumber <= 2; runNumber++ {
		run := service.RunDueModelMaintenance()
		if run.Eligible != 0 || run.Checked != 0 || run.Failed != 0 || len(run.Results) != 0 {
			t.Fatalf("due run %d = %#v; isolated mini-SWE cloud tag must not enter local maintenance", runNumber, run)
		}
	}
	if requests != 0 {
		t.Fatalf("isolated mini-SWE endpoint received %d requests; want none", requests)
	}
	history.mu.Lock()
	recordCount := len(history.records)
	history.mu.Unlock()
	if recordCount != 0 {
		t.Fatalf("maintenance history contains %d records; mini-SWE cloud tag must not create a local failure/cooldown", recordCount)
	}
	history.admissionMu.Lock()
	claimCount := len(history.admissionClaims)
	history.admissionMu.Unlock()
	if claimCount != 0 {
		t.Fatalf("maintenance history contains %d local admission claims; mini-SWE cloud tag must not start local pull admission", claimCount)
	}
}
