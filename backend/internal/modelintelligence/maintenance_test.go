package modelintelligence

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type modelMaintenanceGateStub struct {
	endpoint string
	modelID  string
	calls    int
	err      error
}

func (s *modelMaintenanceGateStub) EnsureConfiguredLocalModel(endpointURL, modelID string) error {
	s.calls++
	s.endpoint = endpointURL
	s.modelID = modelID
	return s.err
}

func (s *modelMaintenanceGateStub) EnsureConfiguredLocalModelWithContext(ctx context.Context, endpointURL, modelID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.EnsureConfiguredLocalModel(endpointURL, modelID)
}

type legacyOnlyModelMaintenanceGateStub struct{ calls int }

func (s *legacyOnlyModelMaintenanceGateStub) EnsureConfiguredLocalModel(string, string) error {
	s.calls++
	return nil
}

type contextModelMaintenanceGateStub struct {
	started chan struct{}
	once    sync.Once
}

func (*contextModelMaintenanceGateStub) EnsureConfiguredLocalModel(string, string) error {
	return errors.New("legacy non-context maintenance method was used")
}

func (s *contextModelMaintenanceGateStub) EnsureConfiguredLocalModelWithContext(ctx context.Context, _, _ string) error {
	s.once.Do(func() { close(s.started) })
	<-ctx.Done()
	return ctx.Err()
}

type maintainedStaticProvider struct {
	profile     ModelProfile
	endpoint    string
	modelID     string
	calls       int
	lastRequest InferenceRequest
	probeStatus ProviderStatus
}

type cancelAfterGenerateProvider struct {
	*maintainedStaticProvider
	cancel context.CancelFunc
}

func (p *cancelAfterGenerateProvider) Generate(ctx context.Context, req InferenceRequest, now time.Time) (InferenceResult, error) {
	result, err := p.maintainedStaticProvider.Generate(ctx, req, now)
	p.cancel()
	return result, err
}

type unmaintainedStaticProvider struct {
	profile ModelProfile
	calls   int
}

func (p *unmaintainedStaticProvider) ID() string          { return p.profile.ProviderID }
func (p *unmaintainedStaticProvider) DisplayName() string { return p.profile.DisplayName }
func (p *unmaintainedStaticProvider) Profiles() []ModelProfile {
	return []ModelProfile{p.profile}
}
func (p *unmaintainedStaticProvider) Probe(context.Context, time.Time) ProbeResult {
	return ProbeResult{ProviderID: p.profile.ProviderID, Status: ProviderActive}
}
func (p *unmaintainedStaticProvider) Generate(_ context.Context, req InferenceRequest, _ time.Time) (InferenceResult, error) {
	p.calls++
	output := "category=general; summary=ok"
	if estimateTokens(output) > req.MaxOutputTokens {
		output = "ok"
	}
	return InferenceResult{
		ProviderID: p.profile.ProviderID, ModelID: p.profile.ModelID, Lane: req.Lane,
		Output: output, InputTokensActual: estimateTokens(req.Prompt), OutputTokensActual: estimateTokens(output),
		InputUsageReported: true, OutputUsageReported: true, OK: true,
	}, nil
}

func (p *maintainedStaticProvider) ID() string          { return p.profile.ProviderID }
func (p *maintainedStaticProvider) DisplayName() string { return p.profile.DisplayName }
func (p *maintainedStaticProvider) Profiles() []ModelProfile {
	return []ModelProfile{p.profile}
}
func (p *maintainedStaticProvider) Probe(context.Context, time.Time) ProbeResult {
	status := p.probeStatus
	if status == "" {
		status = ProviderActive
	}
	return ProbeResult{ProviderID: p.profile.ProviderID, Status: status}
}
func (p *maintainedStaticProvider) Generate(_ context.Context, req InferenceRequest, _ time.Time) (InferenceResult, error) {
	p.calls++
	p.lastRequest = req
	output := "category=general; summary=ok"
	if len(output) > req.MaxOutputTokens {
		output = "ok"
	}
	return InferenceResult{
		ProviderID: p.profile.ProviderID, ModelID: p.profile.ModelID, Lane: req.Lane,
		Output: output, InputTokensActual: estimateTokens(req.Prompt), OutputTokensActual: estimateTokens(output),
		InputUsageReported: true, OutputUsageReported: true, OK: true,
	}, nil
}
func (p *maintainedStaticProvider) ModelMaintenanceIdentity() (string, string, bool) {
	return p.endpoint, p.modelID, true
}

func TestLocalModelIntelligenceCallsRequireCanonicalMaintenance(t *testing.T) {
	provider := &maintainedStaticProvider{
		profile:  ModelProfile{ProviderID: "local-test", ModelID: "qwen-local", DisplayName: "Local test", Local: true, BillingStatus: BillingUnmetered, EndpointLocal: true, Status: ProviderActive, Lanes: []RoutingLane{LaneFastTriage}},
		endpoint: "http://127.0.0.1:11434",
		modelID:  "qwen-local",
	}
	gate := &modelMaintenanceGateStub{err: errors.New("model refresh failed")}
	service := NewService(&Registry{providers: []Provider{provider}}).WithModelMaintenance(gate)

	decision, result, err := service.RunLane(context.Background(), LaneFastTriage, LaneInput{}, "classify this", "operation-1")
	if err == nil || result != nil || !decision.Routable || provider.calls != 0 {
		t.Fatalf("maintenance failure must prevent inference: decision=%#v result=%#v err=%v calls=%d", decision, result, err, provider.calls)
	}
	if gate.endpoint != provider.endpoint || gate.modelID != provider.modelID {
		t.Fatalf("gate received endpoint=%q model=%q", gate.endpoint, gate.modelID)
	}

	benchmark, err := service.Benchmark(context.Background(), provider.profile.ProviderID, provider.profile.ModelID)
	if err != nil || benchmark.OK || provider.calls != 0 || !strings.Contains(benchmark.Detail, "daily model maintenance") {
		t.Fatalf("benchmark must report maintenance block without inference: %#v err=%v calls=%d", benchmark, err, provider.calls)
	}

	gate.err = nil
	_, result, err = service.RunLane(context.Background(), LaneFastTriage, LaneInput{}, "classify this", "operation-2")
	if err != nil || result == nil || !result.OK || provider.calls != 1 {
		t.Fatalf("maintained local inference = %#v err=%v calls=%d", result, err, provider.calls)
	}
}

func TestMaintenanceGateReceivesRequestCancellationBeforeInference(t *testing.T) {
	for _, action := range []string{"run_lane", "benchmark"} {
		t.Run(action, func(t *testing.T) {
			provider := &maintainedStaticProvider{
				profile:  ModelProfile{ProviderID: "local-test", ModelID: "qwen-local", DisplayName: "Local test", Local: true, BillingStatus: BillingUnmetered, EndpointLocal: true, Status: ProviderActive, Lanes: []RoutingLane{LaneFastTriage}},
				endpoint: "http://127.0.0.1:11434", modelID: "qwen-local",
			}
			gate := &contextModelMaintenanceGateStub{started: make(chan struct{})}
			service := NewService(&Registry{providers: []Provider{provider}}).WithModelMaintenance(gate)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() {
				if action == "run_lane" {
					_, _, err := service.RunLane(ctx, LaneFastTriage, LaneInput{}, "classify this", "operation-cancelled")
					done <- err
					return
				}
				_, err := service.Benchmark(ctx, provider.profile.ProviderID, provider.profile.ModelID)
				done <- err
			}()
			select {
			case <-gate.started:
			case <-time.After(time.Second):
				t.Fatal("context-aware maintenance gate was not reached")
			}
			cancel()
			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("cancelled %s error = %v, want context.Canceled", action, err)
				}
			case <-time.After(time.Second):
				t.Fatalf("%s did not stop after request cancellation", action)
			}
			if provider.calls != 0 {
				t.Fatalf("provider inference ran %d time(s) after maintenance cancellation", provider.calls)
			}
		})
	}
}

func TestCancelledGenerationResultIsDiscardedBeforeTelemetry(t *testing.T) {
	for _, action := range []string{"run_lane", "benchmark"} {
		t.Run(action, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			provider := &cancelAfterGenerateProvider{
				maintainedStaticProvider: &maintainedStaticProvider{
					profile: ModelProfile{
						ProviderID: "local-test", ModelID: "qwen-local", DisplayName: "Local test",
						Local: true, LocalInferenceOperatorAttested: true, BillingStatus: BillingUnmetered,
						EndpointLocal: true, Status: ProviderActive, Lanes: []RoutingLane{LaneFastTriage},
					},
					endpoint: "http://127.0.0.1:11434", modelID: "qwen-local",
				},
				cancel: cancel,
			}
			service := NewService(&Registry{providers: []Provider{provider}}).WithModelMaintenance(&modelMaintenanceGateStub{})
			if action == "run_lane" {
				_, result, err := service.RunLane(ctx, LaneFastTriage, LaneInput{}, "classify this", "cancelled-generation")
				if !errors.Is(err, context.Canceled) || result != nil {
					t.Fatalf("cancelled RunLane accepted result=%#v err=%v", result, err)
				}
			} else {
				result, err := service.Benchmark(ctx, provider.profile.ProviderID, provider.profile.ModelID)
				if !errors.Is(err, context.Canceled) || result.OK {
					t.Fatalf("cancelled benchmark accepted result=%#v err=%v", result, err)
				}
			}
			if got := len(service.Telemetry()); got != 0 {
				t.Fatalf("canceled generation recorded %d telemetry rows", got)
			}
		})
	}
}

func TestMaintainedLocalModelFailsClosedWhenGateIsNotWired(t *testing.T) {
	provider := &maintainedStaticProvider{
		profile:  ModelProfile{ProviderID: "local-test", ModelID: "qwen-local", DisplayName: "Local test", Local: true, BillingStatus: BillingUnmetered, EndpointLocal: true, Status: ProviderActive, Lanes: []RoutingLane{LaneFastTriage}},
		endpoint: "http://127.0.0.1:11434",
		modelID:  "qwen-local",
	}
	service := NewService(&Registry{providers: []Provider{provider}})
	_, result, err := service.RunLane(context.Background(), LaneFastTriage, LaneInput{}, "classify this", "operation-1")
	if err == nil || result != nil || provider.calls != 0 || !strings.Contains(err.Error(), "maintenance gate is unavailable") {
		t.Fatalf("unwired maintenance gate must fail closed: result=%#v err=%v calls=%d", result, err, provider.calls)
	}
}

func TestApprovalRequiredMaintenanceGateBlocksLocalInference(t *testing.T) {
	provider := &maintainedStaticProvider{
		profile:  ModelProfile{ProviderID: "local-paid-test", ModelID: "qwen-local", DisplayName: "Paid local runtime", Local: true, BillingStatus: BillingUnmetered, EndpointLocal: true, Status: ProviderActive, Lanes: []RoutingLane{LaneFastTriage}},
		endpoint: "http://127.0.0.1:11434",
		modelID:  "qwen-local",
	}
	gate := &modelMaintenanceGateStub{err: errors.New("approval_required: explicit paid-use approval is required")}
	service := NewService(&Registry{providers: []Provider{provider}}).WithModelMaintenance(gate)

	_, result, err := service.RunLane(context.Background(), LaneFastTriage, LaneInput{}, "classify this", "operation-approval-required")
	if err == nil || result != nil || gate.calls != 1 || provider.calls != 0 || !strings.Contains(err.Error(), "approval_required") {
		t.Fatalf("approval-required maintenance must block before local inference: result=%#v err=%v gateCalls=%d providerCalls=%d", result, err, gate.calls, provider.calls)
	}
}

func TestPaidModelIntelligenceProfilesNeverReachProviderGeneration(t *testing.T) {
	for _, action := range []string{"run_lane", "benchmark"} {
		t.Run(action, func(t *testing.T) {
			provider := &maintainedStaticProvider{
				profile: ModelProfile{
					ProviderID: "local-paid-test", ModelID: "qwen-local", DisplayName: "Paid local runtime",
					Local: true, Paid: paidValue(BillingPaid), BillingStatus: BillingPaid, EndpointLocal: true, Status: ProviderActive, Lanes: []RoutingLane{LaneFastTriage},
				},
				endpoint: "http://127.0.0.1:11434", modelID: "qwen-local",
			}
			gate := &modelMaintenanceGateStub{} // Maintenance success is not paid-use authorization.
			service := NewService(&Registry{providers: []Provider{provider}}).WithModelMaintenance(gate)

			if action == "run_lane" {
				_, result, err := service.RunLane(context.Background(), LaneFastTriage, LaneInput{}, "classify this", "operation-paid")
				if err == nil || result != nil || !strings.Contains(err.Error(), "paid model") {
					t.Fatalf("paid RunLane result=%#v err=%v; want pre-generation paid-model refusal", result, err)
				}
			} else {
				result, err := service.Benchmark(context.Background(), provider.profile.ProviderID, provider.profile.ModelID)
				if err != nil || result.OK || !strings.Contains(result.Detail, "paid model") {
					t.Fatalf("paid Benchmark result=%#v err=%v; want pre-generation paid-model refusal", result, err)
				}
			}
			if provider.calls != 0 || gate.calls != 0 {
				t.Fatalf("paid profile reached maintenance or Generate: providerCalls=%d maintenanceCalls=%d", provider.calls, gate.calls)
			}
		})
	}
}

func TestZeroModelIntelligenceTokenBudgetNeverReachesProviderGeneration(t *testing.T) {
	for _, limit := range []string{"input", "output"} {
		for _, action := range []string{"run_lane", "benchmark"} {
			t.Run(limit+"_"+action, func(t *testing.T) {
				provider := &maintainedStaticProvider{
					profile: ModelProfile{
						ProviderID: "local-test", ModelID: "qwen-local", DisplayName: "Free local runtime",
						Local: true, Paid: paidValue(BillingUnmetered), BillingStatus: BillingUnmetered, EndpointLocal: true, Status: ProviderActive, Lanes: []RoutingLane{LaneFastTriage},
					},
					endpoint: "http://127.0.0.1:11434", modelID: "qwen-local",
				}
				gate := &modelMaintenanceGateStub{}
				service := NewService(&Registry{providers: []Provider{provider}}).WithModelMaintenance(gate)
				budget := service.TokenBudgetDefaults()
				if limit == "input" {
					budget.MaximumInputTokens = 0
				} else {
					budget.MaximumOutputTokens = 0
				}
				// Simulate an invalid/legacy persisted configuration. The public setter
				// rejects zero maxima, but generation must remain fail-closed if one is
				// nevertheless loaded into service state.
				service.mu.Lock()
				service.budgetDefaults = budget
				service.mu.Unlock()

				if action == "run_lane" {
					_, result, err := service.RunLane(context.Background(), LaneFastTriage, LaneInput{}, "classify this", "operation-zero-budget")
					if err == nil || result != nil || !strings.Contains(err.Error(), "token budget is zero") {
						t.Fatalf("zero-budget RunLane result=%#v err=%v; want pre-generation refusal", result, err)
					}
				} else {
					result, err := service.Benchmark(context.Background(), provider.profile.ProviderID, provider.profile.ModelID)
					if err != nil || result.OK || !strings.Contains(result.Detail, "token budget is zero") {
						t.Fatalf("zero-budget Benchmark result=%#v err=%v; want pre-generation refusal", result, err)
					}
				}
				if provider.calls != 0 || gate.calls != 0 {
					t.Fatalf("zero budget reached maintenance or Generate: providerCalls=%d maintenanceCalls=%d", provider.calls, gate.calls)
				}
			})
		}
	}
}

func TestModelIntelligenceCallsRespectConfiguredTokenLimits(t *testing.T) {
	for _, action := range []string{"run_lane", "benchmark"} {
		t.Run(action, func(t *testing.T) {
			provider := &maintainedStaticProvider{
				profile: ModelProfile{
					ProviderID: "local-test", ModelID: "qwen-local", DisplayName: "Free local runtime",
					Local: true, Paid: paidValue(BillingUnmetered), BillingStatus: BillingUnmetered, EndpointLocal: true, Status: ProviderActive, Lanes: []RoutingLane{LaneFastTriage},
				},
				endpoint: "http://127.0.0.1:11434", modelID: "qwen-local",
			}
			gate := &modelMaintenanceGateStub{}
			service := NewService(&Registry{providers: []Provider{provider}}).WithModelMaintenance(gate)
			budget := service.TokenBudgetDefaults()
			budget.MaximumInputTokens = 16
			budget.MaximumOutputTokens = 8
			if _, err := service.SetTokenBudgetDefaults(budget); err != nil {
				t.Fatalf("SetTokenBudgetDefaults: %v", err)
			}

			if action == "run_lane" {
				_, result, err := service.RunLane(context.Background(), LaneFastTriage, LaneInput{}, "classify this", "operation-bounded-budget")
				if err != nil || result == nil {
					t.Fatalf("bounded RunLane result=%#v err=%v", result, err)
				}
			} else {
				result, err := service.Benchmark(context.Background(), provider.profile.ProviderID, provider.profile.ModelID)
				if err != nil || !result.OK {
					t.Fatalf("bounded Benchmark result=%#v err=%v", result, err)
				}
			}
			if provider.calls != 1 || gate.calls != 1 {
				t.Fatalf("free local generation calls=%d maintenance calls=%d; want one each", provider.calls, gate.calls)
			}
			if provider.lastRequest.MaxOutputTokens != 8 {
				t.Fatalf("provider output limit = %d, want configured budget 8", provider.lastRequest.MaxOutputTokens)
			}
		})
	}
}

func TestInputOverBudgetNeverReachesProviderGeneration(t *testing.T) {
	for _, action := range []string{"run_lane", "benchmark"} {
		t.Run(action, func(t *testing.T) {
			provider := &maintainedStaticProvider{
				profile: ModelProfile{
					ProviderID: "local-test", ModelID: "qwen-local", DisplayName: "Free local runtime",
					Local: true, Paid: paidValue(BillingUnmetered), BillingStatus: BillingUnmetered, EndpointLocal: true, Status: ProviderActive, Lanes: []RoutingLane{LaneFastTriage},
				},
				endpoint: "http://127.0.0.1:11434", modelID: "qwen-local",
			}
			gate := &modelMaintenanceGateStub{}
			service := NewService(&Registry{providers: []Provider{provider}}).WithModelMaintenance(gate)
			budget := service.TokenBudgetDefaults()
			budget.MaximumInputTokens = 1
			if _, err := service.SetTokenBudgetDefaults(budget); err != nil {
				t.Fatalf("SetTokenBudgetDefaults: %v", err)
			}

			if action == "run_lane" {
				_, result, err := service.RunLane(context.Background(), LaneFastTriage, LaneInput{}, "classify this", "operation-input-budget")
				if err == nil || result != nil || !strings.Contains(err.Error(), "configured token maximum") {
					t.Fatalf("over-budget RunLane result=%#v err=%v; want pre-generation refusal", result, err)
				}
			} else {
				result, err := service.Benchmark(context.Background(), provider.profile.ProviderID, provider.profile.ModelID)
				if err != nil || result.OK || !strings.Contains(result.Detail, "configured token maximum") {
					t.Fatalf("over-budget Benchmark result=%#v err=%v; want pre-generation refusal", result, err)
				}
			}
			if provider.calls != 0 || gate.calls != 0 {
				t.Fatalf("over-budget input reached maintenance or Generate: providerCalls=%d maintenanceCalls=%d", provider.calls, gate.calls)
			}
		})
	}
}

func TestLegacyMaintenanceGateCannotAuthorizeLocalInference(t *testing.T) {
	provider := &maintainedStaticProvider{
		profile:  ModelProfile{ProviderID: "local-test", ModelID: "qwen-local", DisplayName: "Local test", Local: true, BillingStatus: BillingUnmetered, EndpointLocal: true, Status: ProviderActive, Lanes: []RoutingLane{LaneFastTriage}},
		endpoint: "http://127.0.0.1:11434",
		modelID:  "qwen-local",
	}
	gate := &legacyOnlyModelMaintenanceGateStub{}
	service := NewService(&Registry{providers: []Provider{provider}}).WithModelMaintenance(gate)

	_, result, err := service.RunLane(context.Background(), LaneFastTriage, LaneInput{}, "classify this", "operation-legacy-gate")
	if err == nil || result != nil || gate.calls != 0 || provider.calls != 0 || !strings.Contains(err.Error(), "does not support request cancellation") {
		t.Fatalf("legacy maintenance gate must be rejected before update or inference: result=%#v err=%v gateCalls=%d providerCalls=%d", result, err, gate.calls, provider.calls)
	}
}

func TestLocalModelWithoutMaintenanceIdentityFailsClosed(t *testing.T) {
	provider := &unmaintainedStaticProvider{profile: ModelProfile{
		ProviderID: "local-unmaintained", ModelID: "qwen-local", DisplayName: "Local unmaintained",
		Local: true, BillingStatus: BillingUnmetered, EndpointLocal: true, Status: ProviderActive, Lanes: []RoutingLane{LaneFastTriage},
	}}
	gate := &modelMaintenanceGateStub{}
	service := NewService(&Registry{providers: []Provider{provider}}).WithModelMaintenance(gate)

	_, result, err := service.RunLane(context.Background(), LaneFastTriage, LaneInput{}, "classify this", "operation-1")
	if err == nil || result != nil || provider.calls != 0 || gate.calls != 0 || !strings.Contains(err.Error(), "maintenance identity is unavailable") {
		t.Fatalf("local provider without a maintenance identity must be blocked before inference: result=%#v err=%v providerCalls=%d gateCalls=%d", result, err, provider.calls, gate.calls)
	}
}

func TestCloudModelMaintenanceDoesNotRequireOrInvokeLocalUpdater(t *testing.T) {
	provider := &unmaintainedStaticProvider{profile: ModelProfile{
		ProviderID: "cloud-test", ModelID: "provider-managed-v1", DisplayName: "Provider-managed model",
		Local: false, EndpointLocal: false, BillingStatus: BillingPaid, Status: ProviderActive,
	}}
	gate := &modelMaintenanceGateStub{err: errors.New("local updater must not be called for cloud models")}
	service := NewService(&Registry{providers: []Provider{provider}}).WithModelMaintenance(gate)

	err := service.ensureModelMaintenance(context.Background(), provider, provider.profile)
	if err != nil {
		t.Fatalf("provider-managed cloud model must not require a local artifact updater: %v", err)
	}
	if gate.calls != 0 {
		t.Fatalf("local model maintenance gate was called %d times for a cloud model", gate.calls)
	}
}

func TestLocalModelMaintenanceIdentityMustMatchSelectedModel(t *testing.T) {
	provider := &maintainedStaticProvider{
		profile:  ModelProfile{ProviderID: "local-test", ModelID: "qwen-selected", DisplayName: "Local test", Local: true, BillingStatus: BillingUnmetered, EndpointLocal: true, Status: ProviderActive, Lanes: []RoutingLane{LaneFastTriage}},
		endpoint: "http://127.0.0.1:11434",
		modelID:  "qwen-maintained",
	}
	gate := &modelMaintenanceGateStub{}
	service := NewService(&Registry{providers: []Provider{provider}}).WithModelMaintenance(gate)

	_, result, err := service.RunLane(context.Background(), LaneFastTriage, LaneInput{}, "classify this", "operation-1")
	if err == nil || result != nil || provider.calls != 0 || gate.calls != 0 || !strings.Contains(err.Error(), "does not match selected model") {
		t.Fatalf("maintenance for a different model must not admit inference: result=%#v err=%v providerCalls=%d gateCalls=%d", result, err, provider.calls, gate.calls)
	}
}

func TestDeterministicLocalProvidersRemainExemptFromModelMaintenance(t *testing.T) {
	gate := &modelMaintenanceGateStub{err: errors.New("maintenance should not run for deterministic inference")}
	service := NewService(NewRegistryFromEnv()).WithModelMaintenance(gate)
	for _, model := range []struct{ providerID, modelID string }{
		{ProviderTestFastTriage, "triage-rules-v1"},
		{ProviderTestVerifier, "verifier-rules-v1"},
	} {
		result, err := service.Benchmark(context.Background(), model.providerID, model.modelID)
		if err != nil || !result.OK {
			t.Fatalf("deterministic benchmark %s/%s should remain available: result=%#v err=%v", model.providerID, model.modelID, result, err)
		}
	}
	if gate.calls != 0 {
		t.Fatalf("deterministic inference called the model maintenance gate %d times", gate.calls)
	}
}

func TestProviderProbeControlsCatalogAndMaintenanceEligibility(t *testing.T) {
	provider := &maintainedStaticProvider{
		profile: ModelProfile{
			ProviderID: "local-test", ModelID: "qwen-local", DisplayName: "Local test",
			Local: true, BillingStatus: BillingUnmetered, EndpointLocal: true, Status: ProviderConfigured, Lanes: []RoutingLane{LaneFastTriage},
		},
		endpoint: "http://127.0.0.1:11434", modelID: "qwen-local",
	}
	gate := &modelMaintenanceGateStub{}
	service := NewService(&Registry{providers: []Provider{provider}}).WithModelMaintenance(gate)

	if got, ok := service.Profile(provider.profile.ProviderID, provider.profile.ModelID); !ok || got.Status != ProviderConfigured {
		t.Fatalf("initial catalog profile = %#v, found=%v; want configured", got, ok)
	}
	if _, _, err := service.RunLane(context.Background(), LaneFastTriage, LaneInput{}, "before probe", "operation-1"); err != nil || provider.calls != 0 || gate.calls != 0 {
		t.Fatalf("unprobed provider should not run: err=%v providerCalls=%d gateCalls=%d", err, provider.calls, gate.calls)
	}

	probe, err := service.Probe(context.Background(), provider.profile.ProviderID)
	if err != nil || probe.Status != ProviderActive {
		t.Fatalf("probe = %#v, err=%v; want active", probe, err)
	}
	if got, ok := service.Profile(provider.profile.ProviderID, provider.profile.ModelID); !ok || got.Status != ProviderActive {
		t.Fatalf("catalog profile after successful probe = %#v, found=%v; want active", got, ok)
	}
	if _, result, err := service.RunLane(context.Background(), LaneFastTriage, LaneInput{}, "after probe", "operation-2"); err != nil || result == nil || gate.calls != 1 || provider.calls != 1 {
		t.Fatalf("probed provider should pass through maintenance before inference: result=%#v err=%v gateCalls=%d providerCalls=%d", result, err, gate.calls, provider.calls)
	}

	provider.probeStatus = ProviderUnavailable
	probe, err = service.Probe(context.Background(), provider.profile.ProviderID)
	if err != nil || probe.Status != ProviderUnavailable {
		t.Fatalf("failed health probe = %#v, err=%v; want unavailable", probe, err)
	}
	if got, ok := service.Profile(provider.profile.ProviderID, provider.profile.ModelID); !ok || got.Status != ProviderUnavailable {
		t.Fatalf("catalog profile after failed probe = %#v, found=%v; want unavailable", got, ok)
	}
	if _, result, err := service.RunLane(context.Background(), LaneFastTriage, LaneInput{}, "after failure", "operation-3"); err != nil || result != nil || gate.calls != 1 || provider.calls != 1 {
		t.Fatalf("unavailable provider must fail closed before maintenance/inference: result=%#v err=%v gateCalls=%d providerCalls=%d", result, err, gate.calls, provider.calls)
	}
}

func TestConfiguredModelProbeMustMatchBeforeMaintenanceOrInference(t *testing.T) {
	for _, tc := range []struct {
		name         string
		listedModel  string
		wantStatus   ProviderStatus
		wantRoutable bool
		wantCalls    int
	}{
		{name: "configured model listed", listedModel: "qwen-local", wantStatus: ProviderActive, wantRoutable: true, wantCalls: 1},
		{name: "only a different model listed", listedModel: "other-model", wantStatus: ProviderUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			generationCalls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/v1/models":
					_, _ = fmt.Fprintf(w, `{"data":[{"id":%q}]}`, tc.listedModel)
				case "/v1/chat/completions":
					generationCalls++
					_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"category=general; summary=ok"}}],"usage":{"prompt_tokens":3,"completion_tokens":4}}`))
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()

			provider := &remoteProvider{
				enabled: true, id: "ollama", name: "Ollama", baseURL: server.URL,
				probePath: "/v1/models", genPath: "/v1/chat/completions", modelID: "qwen-local",
				lanes: []RoutingLane{LaneFastTriage}, local: true, localInferenceOperatorAttested: true,
				billingStatus: BillingUnmetered, endpointLocal: true,
				httpClient: newDirectHTTPClient(time.Second),
			}
			gate := &modelMaintenanceGateStub{}
			service := NewService(&Registry{providers: []Provider{provider}}).WithModelMaintenance(gate)

			probe, err := service.Probe(context.Background(), provider.ID())
			if err != nil || probe.Status != tc.wantStatus {
				t.Fatalf("probe = %#v, err=%v; want status %q", probe, err, tc.wantStatus)
			}
			decision, result, err := service.RunLane(context.Background(), LaneFastTriage, LaneInput{}, "classify this", "operation-1")
			if err != nil {
				t.Fatalf("RunLane: %v", err)
			}
			if decision.Routable != tc.wantRoutable || (result != nil) != tc.wantRoutable {
				t.Fatalf("decision=%#v result=%#v; routable=%v", decision, result, tc.wantRoutable)
			}
			if gate.calls != tc.wantCalls || generationCalls != tc.wantCalls {
				t.Fatalf("maintenance calls=%d generation calls=%d; want %d each", gate.calls, generationCalls, tc.wantCalls)
			}
		})
	}
}
