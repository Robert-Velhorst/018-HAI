package llm

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"automation-hub-backend/internal/models"

	"github.com/google/uuid"
)

func TestReservePaidInferenceBudgetUsesLargerOfTokenAndConfiguredEstimates(t *testing.T) {
	history := &fakeGenerationHistoryRepository{}
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.FixedZone("UTC+2", 2*60*60))
	service := &Service{
		policy:            Policy{PaidCallsAllowed: true, DailyPaidBudgetEUR: 10},
		generationHistory: history,
		now:               func() time.Time { return now },
	}
	model := Model{
		ID: "paid-model", Name: "Paid model", Tier: TierExpensive,
		InputCostPerMillionTokensEUR: 3, OutputCostPerMillionTokensEUR: 7,
		EstimatedCostEUR: 0.25,
	}
	request := GenerateRequest{Task: "Write a short response", MaxTokens: 64}

	id, reservedEUR, inputTokens, outputTokens, err := service.reservePaidInferenceBudget(
		context.Background(), Provider{ID: "paid-provider"}, model, request, &RouteDecision{},
	)
	if err != nil {
		t.Fatalf("reserve paid inference budget: %v", err)
	}
	if id == uuid.Nil || len(history.records) != 1 {
		t.Fatalf("reservation id=%s, records=%d; expected one durable reservation", id, len(history.records))
	}
	record := history.records[0]
	tokenEstimate := estimateModelUsageCostEUR(model, inputTokens, outputTokens)
	wantEstimate := model.EstimatedCostEUR
	if tokenEstimate > wantEstimate {
		wantEstimate = tokenEstimate
	}
	if reservedEUR != wantEstimate || record.EstimatedCostEUR != wantEstimate {
		t.Fatalf("reserved estimate=%v, persisted=%v; want conservative maximum %v", reservedEUR, record.EstimatedCostEUR, wantEstimate)
	}
	if record.Status != paidReservationStatus || record.UsageSource != "estimated_uncertain" ||
		record.LoggedAt.Location() != time.UTC || record.InputTokens != inputTokens || record.OutputTokens != outputTokens {
		t.Fatalf("persisted reservation metadata = %#v", record)
	}
}

func TestReservePaidInferenceBudgetFailsBeforePersistenceForInvalidPricingOrCancelledContext(t *testing.T) {
	for _, test := range []struct {
		name   string
		model  Model
		cancel bool
		want   error
	}{
		{name: "negative input price", model: Model{InputCostPerMillionTokensEUR: -1, EstimatedCostEUR: 0.1}, want: ErrPaidPricingUnavailable},
		{name: "non-finite output price", model: Model{OutputCostPerMillionTokensEUR: math.Inf(1), EstimatedCostEUR: 0.1}, want: ErrPaidPricingUnavailable},
		{name: "cancelled before reserve", model: Model{EstimatedCostEUR: 0.1}, cancel: true, want: context.Canceled},
	} {
		t.Run(test.name, func(t *testing.T) {
			history := &fakeGenerationHistoryRepository{}
			service := &Service{
				policy:            Policy{PaidCallsAllowed: true, DailyPaidBudgetEUR: 1},
				generationHistory: history,
			}
			ctx := context.Background()
			if test.cancel {
				cancelled, cancel := context.WithCancel(ctx)
				cancel()
				ctx = cancelled
			}
			_, _, _, _, err := service.reservePaidInferenceBudget(ctx, Provider{ID: "paid-provider"}, test.model, GenerateRequest{Task: "test"}, nil)
			if !errors.Is(err, test.want) {
				t.Fatalf("reserve error = %v; want %v", err, test.want)
			}
			if len(history.records) != 0 {
				t.Fatalf("persisted %d reservations before rejection", len(history.records))
			}
		})
	}
}

func TestCollapsePaidFinalizationEventsKeepsOneAuditableGeneration(t *testing.T) {
	id := uuid.New()
	loggedAt := time.Date(2026, 9, 28, 23, 59, 59, 0, time.UTC)
	originalFallback := `[{"providerId":"fallback-a"}]`
	markers, err := json.Marshal([]string{
		paidReservationMarker + id.String(),
		paidTerminalStatusMarker + "completed",
	})
	if err != nil {
		t.Fatal(err)
	}
	rows := []models.LLMGenerationRecord{
		{
			ID: id, ProviderID: "provider", ModelID: "model", ModelName: "Model", Tier: TierExpensive,
			Status: paidReservationStatus, Reason: "pending", EstimatedCostEUR: 0.6,
			InputTokens: 100, OutputTokens: 64, UsageSource: "estimated_uncertain",
			FallbackPathJSON: originalFallback, LoggedAt: loggedAt,
		},
		{
			ID: uuid.New(), ProviderID: "provider", ModelID: "model", ModelName: "Model", Tier: TierExpensive,
			Status: paidFinalizationStatus, Reason: "provider completed", EstimatedCostEUR: 0.4,
			InputTokens: 42, OutputTokens: 17, UsageSource: "provider_reported", DurationMs: 80,
			FallbackPathJSON: string(markers), LoggedAt: loggedAt, CreatedAt: loggedAt.Add(time.Second),
		},
	}

	got := collapsePaidFinalizationEvents(rows)
	if len(got) != 1 {
		t.Fatalf("collapsed history has %d records, want one: %#v", len(got), got)
	}
	if got[0].ID != id || got[0].Status != "completed" || got[0].EstimatedCostEUR != 0.4 ||
		got[0].InputTokens != 42 || got[0].OutputTokens != 17 || got[0].UsageSource != "provider_reported" ||
		got[0].LoggedAt != loggedAt || got[0].FallbackPathJSON != originalFallback {
		t.Fatalf("collapsed generation does not preserve original identity/day and actual settlement: %#v", got[0])
	}
}

func TestPaidProviderFailureFinalizesConservativeReservationAfterDispatch(t *testing.T) {
	disableModelMaintenanceForTest(t)
	history := &observedPaidBudgetTestRepository{fakeGenerationHistoryRepository: &fakeGenerationHistoryRepository{}}
	var providerCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		providerCalls.Add(1)
		if !history.reserved.Load() {
			t.Error("provider was dispatched before the paid reservation was persisted")
		}
		http.Error(w, "provider unavailable", http.StatusBadGateway)
	}))
	defer server.Close()

	service, request := paidBudgetTestService(t, history, server.URL)
	result, err := service.Generate(withTrustedTestEffect(request))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if result.Status != "failed" || result.AuditStatus != "recorded" || result.Output != "" {
		t.Fatalf("provider failure result = %#v; want failed, audited, empty output", result)
	}
	if providerCalls.Load() != 1 || len(history.records) != 1 {
		t.Fatalf("provider calls=%d, ledger rows=%d; want one dispatched and finalized paid call", providerCalls.Load(), len(history.records))
	}
	row := history.records[0]
	if row.Status != "failed" || row.UsageSource != "estimated_uncertain" || row.EstimatedCostEUR <= 0 {
		t.Fatalf("failed provider call was not conservatively accounted: %#v", row)
	}
}

func TestPaidGenerationWithholdsOutputWhenSettlementAuditFails(t *testing.T) {
	disableModelMaintenanceForTest(t)
	history := &observedPaidBudgetTestRepository{
		fakeGenerationHistoryRepository: &fakeGenerationHistoryRepository{},
		finalizeErr:                     errors.New("append-only audit store unavailable"),
	}
	var providerCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		providerCalls.Add(1)
		if !history.reserved.Load() {
			t.Error("provider was dispatched before the paid reservation was persisted")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]string{"content": "unrecorded paid draft"}}},
			"usage":   map[string]int{"prompt_tokens": 10, "completion_tokens": 4},
		})
	}))
	defer server.Close()

	service, request := paidBudgetTestService(t, history, server.URL)
	result, err := service.Generate(withTrustedTestEffect(request))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if providerCalls.Load() != 1 || result.Status != "failed" || result.Output != "" || result.AuditStatus != "record_failed" {
		t.Fatalf("paid result after settlement audit failure = %#v, provider calls=%d; output must be withheld", result, providerCalls.Load())
	}
	if len(history.records) != 1 || history.records[0].Status != paidReservationStatus ||
		history.records[0].UsageSource != "estimated_uncertain" || history.records[0].EstimatedCostEUR <= 0 {
		t.Fatalf("failed settlement did not leave the durable reservation counted: %#v", history.records)
	}
}

type observedPaidBudgetTestRepository struct {
	*fakeGenerationHistoryRepository
	reserved    atomic.Bool
	finalizeErr error
}

func (r *observedPaidBudgetTestRepository) ReservePaidGeneration(ctx context.Context, record *models.LLMGenerationRecord, dailyLimitEUR float64) error {
	if err := r.fakeGenerationHistoryRepository.ReservePaidGeneration(ctx, record, dailyLimitEUR); err != nil {
		return err
	}
	r.reserved.Store(true)
	return nil
}

func (r *observedPaidBudgetTestRepository) FinalizePaidGeneration(record *models.LLMGenerationRecord) error {
	if r.finalizeErr != nil {
		return r.finalizeErr
	}
	return r.fakeGenerationHistoryRepository.FinalizePaidGeneration(record)
}

func paidBudgetTestService(t *testing.T, history GenerationHistoryRepository, endpoint string) (*Service, GenerateRequest) {
	t.Helper()
	policy := testPolicyWithoutEndpoints()
	policy.PaidCallsAllowed = true
	policy.DailyPaidBudgetEUR = 1
	index := providerIndex(t, policy, "paid-provider")
	policy.Providers[index].Enabled = true
	policy.Providers[index].EndpointURL = endpoint
	policy.Providers[index].Models = []Model{{
		ID: "paid-budget-test", Name: "Paid budget test", Tier: TierCheap, Enabled: true,
		RequiresApproval: true, EstimatedCostEUR: 0.6, MaxDifficulty: 5, MaxReasoning: "very_high",
	}}
	service := withTrustedTestFinalEffects(t, &Service{policy: policy, generationHistory: history})
	request := GenerateRequest{
		Task: "Draft a short approved response", MaxTokens: 100,
		RouteDecision: &RouteDecision{
			SelectedProviderID: "paid-provider", SelectedModelID: "paid-budget-test",
			SelectedModelName: "Paid budget test", Tier: TierCheap,
		},
	}
	return service, request
}
