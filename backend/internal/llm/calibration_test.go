package llm

import (
	"automation-hub-backend/internal/modelintelligence"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type fakeModelTelemetryRepository struct {
	rows []modelintelligence.ModelRunTelemetry
}

func (repo *fakeModelTelemetryRepository) Save(row modelintelligence.ModelRunTelemetry) error {
	for _, existing := range repo.rows {
		if existing.ID == row.ID {
			return fmt.Errorf("duplicate telemetry %s", row.ID)
		}
	}
	repo.rows = append(repo.rows, row)
	return nil
}

func (repo *fakeModelTelemetryRepository) LoadAll() ([]modelintelligence.ModelRunTelemetry, error) {
	return append([]modelintelligence.ModelRunTelemetry{}, repo.rows...), nil
}

func (repo *fakeModelTelemetryRepository) UpdateValidation(id string, status modelintelligence.ValidationStatus, method string) error {
	for index := range repo.rows {
		if repo.rows[index].ID == id {
			repo.rows[index].ValidationStatus = status
			repo.rows[index].ValidationMethod = method
			return nil
		}
	}
	return fmt.Errorf("telemetry %s not found", id)
}

func TestRouteUsesAcceptedOutcomeEvidenceBeforePrice(t *testing.T) {
	policy := calibrationTestPolicy("http://localhost:4101", "http://localhost:4102")
	repo := &fakeModelTelemetryRepository{}
	for index := 0; index < 5; index++ {
		repo.rows = append(repo.rows, modelintelligence.ModelRunTelemetry{
			ID: fmt.Sprintf("weak-%d", index), ProviderID: "cheap-local", ModelID: "cheap-model",
			Lane: modelintelligence.LaneDrafting, OK: true,
			ValidationStatus: modelintelligence.ValidationFailed, CreatedAt: time.Now().UTC(),
		})
	}
	repo.rows = append(repo.rows, modelintelligence.ModelRunTelemetry{
		ID: "capable-1", ProviderID: "capable-local", ModelID: "capable-model",
		Lane: modelintelligence.LaneDrafting, OK: true,
		ValidationStatus: modelintelligence.ValidationSourceSupported, CreatedAt: time.Now().UTC(),
	})
	service := (&Service{policy: policy}).WithModelTelemetryRepository(repo)

	decision, err := service.Route(RouteRequest{Task: "Draft a concise project update", Difficulty: 2, RequiredReasoning: "low"})
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	if decision.SelectedProviderID != "capable-local" || decision.SelectedModelID != "capable-model" {
		t.Fatalf("selected %s/%s, want validator-backed capable-local/capable-model", decision.SelectedProviderID, decision.SelectedModelID)
	}
	if decision.Calibration == nil || decision.Calibration.AcceptedOutputs != 1 {
		t.Fatalf("missing selected calibration evidence: %#v", decision.Calibration)
	}
	foundWeakSkip := false
	for _, skipped := range decision.Skipped {
		if skipped.ModelID == "cheap-model" && strings.Contains(skipped.Reason, "no validator-accepted") {
			foundWeakSkip = true
		}
	}
	if !foundWeakSkip {
		t.Fatalf("known weak model was not explained in skipped routes: %#v", decision.Skipped)
	}
}

func TestGenerateRecordsUnvalidatedRunThenTrustedValidation(t *testing.T) {
	disableModelMaintenanceForTest(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []map[string]interface{}{{"message": map[string]string{"content": "source-backed draft"}}},
			"usage":   map[string]int{"prompt_tokens": 12, "completion_tokens": 4},
		})
	}))
	defer server.Close()

	policy := calibrationTestPolicy(server.URL, server.URL)
	policy.Providers = []Provider{{
		ID: "lm-studio", Name: "LM Studio", Local: true, Enabled: true, EndpointURL: server.URL,
		Models: []Model{{ID: "local-model", Name: "Local model", Tier: TierLocal, Enabled: true, MaxDifficulty: 5, MaxReasoning: "high", Capabilities: []string{"general"}}},
	}}
	repo := &fakeModelTelemetryRepository{}
	history := &fakeGenerationHistoryRepository{}
	service := withTrustedTestFinalEffects(t, (&Service{policy: policy, generationHistory: history}).WithModelTelemetryRepository(repo))

	result, err := service.Generate(withTrustedTestEffect(GenerateRequest{
		Task: "Draft a project update", OperationID: "task-1:attempt:1",
		RouteDecision: &RouteDecision{
			SelectedProviderID: "lm-studio", SelectedModelID: "local-model", SelectedModelName: "Local model", Tier: TierLocal,
			Classification: TaskClassification{TaskType: "general", Difficulty: 2, RequiredReasoning: "low", RequiredCapabilities: []string{"general"}},
		},
	}))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if result.Status != "completed" || result.TelemetryID == "" || result.ValidationStatus != string(modelintelligence.ValidationUnvalidated) {
		t.Fatalf("generation result = %#v", result)
	}
	if len(repo.rows) != 1 || repo.rows[0].OperationID != "task-1:attempt:1" || repo.rows[0].ValidationStatus != modelintelligence.ValidationUnvalidated {
		t.Fatalf("initial telemetry = %#v", repo.rows)
	}
	if err := service.RecordGenerationValidation(result.TelemetryID, string(modelintelligence.ValidationSourceSupported), "task_success_criteria_v1+verification_engine"); err != nil {
		t.Fatalf("RecordGenerationValidation: %v", err)
	}
	if repo.rows[0].ValidationStatus != modelintelligence.ValidationSourceSupported {
		t.Fatalf("updated telemetry = %#v", repo.rows[0])
	}
	entries, err := service.GenerationHistory(10)
	if err != nil || len(entries) != 1 {
		t.Fatalf("GenerationHistory = %#v, %v", entries, err)
	}
	if entries[0].ValidationStatus != string(modelintelligence.ValidationSourceSupported) || entries[0].ValidationMethod == "" {
		t.Fatalf("generation history did not join calibration outcome: %#v", entries[0])
	}
}

func TestGenerateWithholdsCompletedOutputWhenCalibrationCannotPersist(t *testing.T) {
	disableModelMaintenanceForTest(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []map[string]interface{}{{"message": map[string]string{"content": "unrecorded result"}}},
			"usage":   map[string]int{"prompt_tokens": 12, "completion_tokens": 4},
		})
	}))
	defer server.Close()

	policy := calibrationTestPolicy(server.URL, server.URL)
	policy.Providers = []Provider{{
		ID: "lm-studio", Name: "LM Studio", Local: true, Enabled: true, EndpointURL: server.URL,
		Models: []Model{{ID: "local-model", Name: "Local model", Tier: TierLocal, Enabled: true, MaxDifficulty: 5, MaxReasoning: "high", Capabilities: []string{"general"}}},
	}}
	history := &fakeGenerationHistoryRepository{}
	telemetry := &failingModelTelemetryRepository{err: fmt.Errorf("durable telemetry unavailable")}
	service := withTrustedTestFinalEffects(t, (&Service{policy: policy, generationHistory: history}).WithModelTelemetryRepository(telemetry))

	result, err := service.Generate(withTrustedTestEffect(GenerateRequest{
		Task: "Draft a project update",
		RouteDecision: &RouteDecision{
			SelectedProviderID: "lm-studio", SelectedModelID: "local-model", SelectedModelName: "Local model", Tier: TierLocal,
			Classification: TaskClassification{TaskType: "general", Difficulty: 2, RequiredReasoning: "low", RequiredCapabilities: []string{"general"}},
		},
	}))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if result.Status != "failed" || result.Output != "" || result.CalibrationAudit != "record_failed" || !strings.Contains(result.Reason, "output was withheld") {
		t.Fatalf("result after telemetry failure = %#v, want failed with no output and explicit audit state", result)
	}
	if len(history.records) != 1 || history.records[0].Status != "failed" {
		t.Fatalf("generation audit after telemetry failure = %#v, want failed audit record", history.records)
	}
}

func TestGenerationTelemetryDoesNotLabelEstimatedTokensPerSecondObserved(t *testing.T) {
	repo := &fakeModelTelemetryRepository{}
	service := (&Service{}).WithModelTelemetryRepository(repo)
	for _, test := range []struct {
		generationID string
		usageSource  string
		wantTPS      float64
	}{
		{generationID: "estimated-run", usageSource: "estimated", wantTPS: 0},
		{generationID: "reported-run", usageSource: "provider_reported", wantTPS: 20},
	} {
		service.recordGeneration(&GenerationResult{
			GenerationID: test.generationID, ProviderID: "lm-studio", ModelID: "local-model",
			Status: "completed", InputTokens: 10, OutputTokens: 20, UsageSource: test.usageSource,
			DurationMs: 1000, LoggedAt: time.Now().UTC(),
		}, GenerateRequest{})
	}
	if len(repo.rows) != 2 || repo.rows[0].TokensPerSecond != 0 || repo.rows[1].TokensPerSecond != 20 {
		t.Fatalf("throughput telemetry = %#v, want estimated=0 and reported=20", repo.rows)
	}
}

type failingModelTelemetryRepository struct{ err error }

func (r *failingModelTelemetryRepository) Save(modelintelligence.ModelRunTelemetry) error {
	return r.err
}
func (*failingModelTelemetryRepository) LoadAll() ([]modelintelligence.ModelRunTelemetry, error) {
	return nil, nil
}
func (*failingModelTelemetryRepository) UpdateValidation(string, modelintelligence.ValidationStatus, string) error {
	return nil
}

func calibrationTestPolicy(cheapEndpoint, capableEndpoint string) Policy {
	return annotatePolicyReadiness(Policy{
		LocalModelsAllowed: true, FreeCloudQuotaAllowed: true, LocalFirst: true,
		TierOrder: []string{TierLocal, TierFree, TierCheap, TierAcceptable, TierHigh, TierPremium, TierExpensive},
		Providers: []Provider{
			{ID: "cheap-local", Name: "Cheap local", Local: true, Enabled: true, EndpointURL: cheapEndpoint, Models: []Model{{ID: "cheap-model", Name: "Cheap", Tier: TierLocal, Enabled: true, MaxDifficulty: 5, MaxReasoning: "high", Capabilities: []string{"general"}}}},
			{ID: "capable-local", Name: "Capable local", Local: true, Enabled: true, EndpointURL: capableEndpoint, Models: []Model{{ID: "capable-model", Name: "Capable", Tier: TierAcceptable, Enabled: true, MaxDifficulty: 5, MaxReasoning: "high", Capabilities: []string{"general"}}}},
		},
	})
}
