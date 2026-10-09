package modelintelligence

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

// memTelemetryRepo is an in-memory TelemetryRepository for durability tests.
type memTelemetryRepo struct{ rows []ModelRunTelemetry }

func (m *memTelemetryRepo) Save(t ModelRunTelemetry) error { m.rows = append(m.rows, t); return nil }
func (m *memTelemetryRepo) LoadAll() ([]ModelRunTelemetry, error) {
	out := make([]ModelRunTelemetry, len(m.rows))
	copy(out, m.rows)
	return out, nil
}
func (m *memTelemetryRepo) UpdateValidation(id string, status ValidationStatus, method string) error {
	for index := range m.rows {
		if m.rows[index].ID == id {
			m.rows[index].ValidationStatus = status
			m.rows[index].ValidationMethod = method
			return nil
		}
	}
	return fmt.Errorf("telemetry %s not found", id)
}

type failingTelemetryRepo struct {
	memTelemetryRepo
	saveErr error
	loadErr error
}

func (r *failingTelemetryRepo) Save(row ModelRunTelemetry) error {
	if r.saveErr != nil {
		return r.saveErr
	}
	return r.memTelemetryRepo.Save(row)
}

func (r *failingTelemetryRepo) LoadAll() ([]ModelRunTelemetry, error) {
	if r.loadErr != nil {
		return nil, r.loadErr
	}
	return r.memTelemetryRepo.LoadAll()
}

func TestTelemetryIsDurableAcrossRestart(t *testing.T) {
	repo := &memTelemetryRepo{}

	// First "process": a service persisting to the repo records a triage call.
	s1 := NewService(NewRegistryFromEnv()).WithTelemetryRepository(repo)
	s1.now = func() time.Time { return time.Date(2026, 7, 11, 0, 0, 0, 0, time.UTC) }
	if _, err := s1.Triage(context.Background(), "review_invoice", "Pay invoice", "pay the rent invoice", true, false, "op-1"); err != nil {
		t.Fatalf("triage: %v", err)
	}
	if len(s1.Telemetry()) == 0 {
		t.Fatalf("first service must record telemetry")
	}
	if len(repo.rows) == 0 {
		t.Fatalf("telemetry must be persisted to the durable repository")
	}
	if repo.rows[0].ValidationStatus != ValidationSchemaValidated || repo.rows[0].ValidationMethod != "triage_schema_v1" {
		t.Fatalf("triage validation evidence must be durable: %#v", repo.rows[0])
	}

	// Second "process" (restart): a fresh service seeded from the same repo must
	// already have the prior telemetry — proving durability.
	s2 := NewService(NewRegistryFromEnv()).WithTelemetryRepository(repo)
	if len(s2.Telemetry()) != len(repo.rows) {
		t.Fatalf("telemetry must survive restart: got %d, persisted %d", len(s2.Telemetry()), len(repo.rows))
	}
	if len(s2.LaneWinners()) == 0 {
		t.Fatalf("seeded telemetry must produce lane winners after restart")
	}
	profiles := s2.Profiles()
	foundObserved := false
	for _, profile := range profiles {
		if profile.ProviderID == ProviderTestFastTriage && profile.ObservedRuns > 0 {
			foundObserved = true
		}
	}
	if !foundObserved {
		t.Fatal("durable telemetry must rebuild observed profile metrics after restart")
	}
}

func TestCalibrationRefreshesRowsWrittenByAnotherEngine(t *testing.T) {
	repo := &memTelemetryRepo{}
	service := NewService(NewRegistryFromEnv()).WithTelemetryRepository(repo)
	repo.rows = append(repo.rows, ModelRunTelemetry{
		ID: "llm-generation:external", ProviderID: ProviderTestFastTriage,
		ModelID: "triage-rules-v1", Lane: LaneFastTriage, OK: true,
		ValidationStatus: ValidationUnvalidated, CreatedAt: time.Now().UTC(),
	})
	if got := service.Calibration().UnvalidatedRuns; got != 1 {
		t.Fatalf("calibration did not refresh external durable row: got %d", got)
	}
	if err := repo.UpdateValidation("llm-generation:external", ValidationSourceSupported, "task_success_criteria_v1"); err != nil {
		t.Fatal(err)
	}
	summary := service.Calibration()
	if summary.AcceptedOutputs != 1 || summary.UnvalidatedRuns != 0 {
		t.Fatalf("calibration did not refresh external validation: %#v", summary)
	}
}

func TestTelemetryStorePreservesCallerAssignedID(t *testing.T) {
	store := NewTelemetryStore()
	recorded := store.Record(ModelRunTelemetry{ID: "external-id", ValidationStatus: ValidationUnvalidated})
	if recorded.ID != "external-id" {
		t.Fatalf("record id = %q, want caller-assigned id", recorded.ID)
	}
}

func TestTelemetryStoreNormalizesMissingUsageSourceToEstimate(t *testing.T) {
	store := NewTelemetryStore()
	got := store.Record(ModelRunTelemetry{ID: "legacy-row"})
	if got.UsageSource != TokenUsageEstimated {
		t.Fatalf("missing usage source = %q, want estimated", got.UsageSource)
	}
}

func TestProductionStyleServiceBlocksModelCallsWithoutDurableTelemetry(t *testing.T) {
	provider := &maintainedStaticProvider{
		profile: ModelProfile{
			ProviderID: "local-test", ModelID: "qwen-local", DisplayName: "Local model",
			Lanes: []RoutingLane{LaneFastTriage}, Local: true, EndpointLocal: true,
			LocalInferenceOperatorAttested: true, BillingStatus: BillingUnmetered,
			Status: ProviderActive,
		},
		endpoint: "http://127.0.0.1:11434", modelID: "qwen-local",
	}
	service := NewService(&Registry{providers: []Provider{provider}})
	service.requireDurableTelemetry = true
	service.WithModelMaintenance(&modelMaintenanceGateStub{})
	_, result, err := service.RunLane(context.Background(), LaneFastTriage, LaneInput{SafeForCloud: true}, "classify this", "op-1")
	if err == nil || !strings.Contains(err.Error(), "durable model-run history is memory_only") || result != nil || provider.calls != 0 {
		t.Fatalf("result=%#v err=%v provider calls=%d; inference must stop before generation", result, err, provider.calls)
	}
	if got := service.Overview().TelemetryPersistence.State; got != TelemetryPersistenceMemoryOnly {
		t.Fatalf("persistence state = %q, want memory_only", got)
	}
}

func TestTelemetrySaveFailureWithholdsModelOutput(t *testing.T) {
	provider := &maintainedStaticProvider{
		profile: ModelProfile{
			ProviderID: "local-test", ModelID: "qwen-local", DisplayName: "Local model",
			Lanes: []RoutingLane{LaneFastTriage}, Local: true, EndpointLocal: true,
			LocalInferenceOperatorAttested: true, BillingStatus: BillingUnmetered,
			Status: ProviderActive,
		},
		endpoint: "http://127.0.0.1:11434", modelID: "qwen-local",
	}
	repo := &failingTelemetryRepo{saveErr: fmt.Errorf("database credentials must not leak")}
	service := NewService(&Registry{providers: []Provider{provider}}).
		WithTelemetryRepository(repo).
		WithModelMaintenance(&modelMaintenanceGateStub{})
	_, result, err := service.RunLane(context.Background(), LaneFastTriage, LaneInput{SafeForCloud: true}, "classify this", "op-2")
	if err == nil || !strings.Contains(err.Error(), "history could not be saved") || strings.Contains(err.Error(), "credentials") || result != nil {
		t.Fatalf("result=%#v err=%v; output must be withheld with sanitized persistence error", result, err)
	}
	if rows := service.Telemetry(); len(rows) != 0 {
		t.Fatalf("failed telemetry row was exposed as recorded: %#v", rows)
	}
	status := service.TelemetryPersistence()
	if status.State != TelemetryPersistenceDegraded || !strings.Contains(status.Message, "output was withheld") || strings.Contains(status.Message, "credentials") {
		t.Fatalf("unexpected persistence state: %#v", status)
	}
}

func TestTelemetryLoadFailureReportsDegradedPersistence(t *testing.T) {
	service := NewService(NewRegistryFromEnv()).WithTelemetryRepository(&failingTelemetryRepo{loadErr: fmt.Errorf("private db error")})
	status := service.Overview().TelemetryPersistence
	if status.State != TelemetryPersistenceDegraded || !strings.Contains(status.Message, "could not be refreshed") || strings.Contains(status.Message, "private") {
		t.Fatalf("unexpected persistence state: %#v", status)
	}
}
