package modelintelligence

import (
	"context"
	"errors"
	"io"
	"math"
	"net/http"
	"strings"
	"testing"
	"time"
)

type healthInferenceProvider struct {
	*maintainedStaticProvider
	result InferenceResult
	err    error
}

func (p *healthInferenceProvider) Generate(context.Context, InferenceRequest, time.Time) (InferenceResult, error) {
	p.calls++
	return p.result, p.err
}

func healthTestProfile() ModelProfile {
	return ModelProfile{
		ProviderID: "health-test", ModelID: "local-model", DisplayName: "Health test",
		Local: true, EndpointLocal: true, BillingStatus: BillingUnmetered,
		Status: ProviderActive, Lanes: []RoutingLane{LaneFastTriage},
	}
}

func TestProductionHealthRejectsUnsuccessfulOrInconsistentInference(t *testing.T) {
	transportErr := errors.New("transport refused")
	for _, tc := range []struct {
		name   string
		mutate func(*InferenceResult)
		err    error
	}{
		{name: "zero result", mutate: func(r *InferenceResult) { *r = InferenceResult{} }},
		{name: "unsuccessful nil error", mutate: func(r *InferenceResult) { r.OK = false }},
		{name: "contradictory error field", mutate: func(r *InferenceResult) { r.Error = "outcome uncertain" }},
		{name: "wrong provider", mutate: func(r *InferenceResult) { r.ProviderID = "other-provider" }},
		{name: "wrong model", mutate: func(r *InferenceResult) { r.ModelID = "other-model" }},
		{name: "wrong lane", mutate: func(r *InferenceResult) { r.Lane = LaneDrafting }},
		{name: "empty output", mutate: func(r *InferenceResult) { r.Output = "" }},
		{name: "blank output", mutate: func(r *InferenceResult) { r.Output = " \n\t" }},
		{name: "negative duration", mutate: func(r *InferenceResult) { r.DurationMs = -1 }},
		{name: "negative throughput", mutate: func(r *InferenceResult) { r.TokensPerSecond = -1 }},
		{name: "nan throughput", mutate: func(r *InferenceResult) { r.TokensPerSecond = math.NaN() }},
		{name: "infinite throughput", mutate: func(r *InferenceResult) { r.TokensPerSecond = math.Inf(1) }},
		{name: "success plus error", err: transportErr},
	} {
		for _, action := range []string{"run_lane", "benchmark"} {
			t.Run(tc.name+"/"+action, func(t *testing.T) {
				profile := healthTestProfile()
				provider := &healthInferenceProvider{
					maintainedStaticProvider: &maintainedStaticProvider{
						profile: profile, endpoint: "http://127.0.0.1:11434", modelID: profile.ModelID,
					},
					result: InferenceResult{
						ProviderID: profile.ProviderID, ModelID: profile.ModelID, Lane: LaneFastTriage,
						OK: true, Output: "ok", InputUsageReported: true, OutputUsageReported: true,
						InputTokensActual: 2, OutputTokensActual: 1, DurationMs: 1, TokensPerSecond: 1000,
					},
					err: tc.err,
				}
				if tc.mutate != nil {
					tc.mutate(&provider.result)
				}
				service := NewService(&Registry{providers: []Provider{provider}}).WithModelMaintenance(&modelMaintenanceGateStub{})
				if action == "run_lane" {
					_, result, err := service.RunLane(context.Background(), LaneFastTriage, LaneInput{}, "classify", "health-operation")
					if err == nil || result != nil {
						t.Fatalf("invalid inference was accepted: result=%#v err=%v", result, err)
					}
					if tc.err != nil && !errors.Is(err, tc.err) {
						t.Fatal("adapter error identity was lost")
					}
				} else {
					result, err := service.Benchmark(context.Background(), profile.ProviderID, profile.ModelID)
					if err != nil || result.OK || result.ClaimLevel == ClaimBenchmarked || result.Detail == "" {
						t.Fatalf("invalid benchmark was accepted: result=%#v err=%v", result, err)
					}
				}
				rows := service.Telemetry()
				if provider.calls != 1 || len(rows) != 1 || rows[0].OK || rows[0].ProviderID != profile.ProviderID || rows[0].ModelID != profile.ModelID || rows[0].Lane != LaneFastTriage {
					t.Fatalf("failed attempt must be audited against selected route: calls=%d rows=%#v", provider.calls, rows)
				}
				if rows[0].ValidationStatus != ValidationUnvalidated || math.IsNaN(rows[0].TokensPerSecond) || math.IsInf(rows[0].TokensPerSecond, 0) || rows[0].TokensPerSecond < 0 || rows[0].DurationMs < 0 {
					t.Fatalf("failed attempt exposed invalid or accepted telemetry: %#v", rows[0])
				}
				observed, _ := service.Profile(profile.ProviderID, profile.ModelID)
				if observed.ObservedRuns != 1 || observed.ObservedFailures != 1 || observed.LastBenchmarkedAt != nil || observed.ClaimLevel == ClaimBenchmarked || observed.Status != ProviderFailed || len(service.Cache().Records()) != 0 {
					t.Fatalf("failed attempt promoted claims or cached output: %#v", observed)
				}
			})
		}
	}
}

func TestProductionHealthPreservesCheckTimeAndExpiresActiveEvidence(t *testing.T) {
	base := time.Now().UTC().Truncate(time.Second)
	checkedAt := base.Add(-time.Minute)
	profile := healthTestProfile()
	profile.Status = ProviderConfigured
	provider := fixedProbeProvider{
		staticProvider: staticProvider{profile: profile},
		result:         ProbeResult{ProviderID: profile.ProviderID, Status: ProviderActive, ModelsSeen: 1, CheckedAt: checkedAt},
	}
	registry := &Registry{providers: []Provider{provider}}
	service := NewService(registry)
	clock := base
	service.now = func() time.Time { return clock }
	result, err := service.Probe(context.Background(), profile.ProviderID)
	if err != nil || result.Status != ProviderActive || !result.CheckedAt.Equal(checkedAt) {
		t.Fatalf("recent probe=%#v err=%v", result, err)
	}
	observed, _ := service.Profile(profile.ProviderID, profile.ModelID)
	if observed.Status != ProviderActive || observed.LastProbedAt == nil || !observed.LastProbedAt.Equal(checkedAt) {
		t.Fatalf("probe time was fabricated or recent evidence was rejected: %#v", observed)
	}
	clock = checkedAt.Add(modelProbeMaxAge)
	observed, _ = service.Profile(profile.ProviderID, profile.ModelID)
	if observed.Status != ProviderConfigured || observed.LastProbedAt == nil || !observed.LastProbedAt.Equal(checkedAt) || service.Overview().ActiveModels != 0 {
		t.Fatalf("stale evidence remained active or lost provenance: %#v", observed)
	}
	registry.recordProbeStatus(profile.Key(), ProviderActive, time.Now().UTC().Add(-modelProbeMaxAge))
	if len(registry.ProfilesForLane(LaneFastTriage)) != 0 {
		t.Fatal("stale active evidence remains routable in the registry")
	}
	registry.recordProbeStatus(profile.Key(), ProviderUnavailable, time.Now().UTC().Add(-2*modelProbeMaxAge))
	observed, _ = registry.Profile(profile.ProviderID, profile.ModelID)
	if observed.Status != ProviderUnavailable {
		t.Fatalf("failure evidence was replaced by expiry: %#v", observed)
	}
}

func TestProductionHealthRejectsStaleAndInvalidProbeMetadata(t *testing.T) {
	base := time.Now().UTC()
	for _, tc := range []struct {
		name      string
		checkedAt time.Time
		models    int
		duration  int64
	}{
		{name: "stale success", checkedAt: base.Add(-modelProbeMaxAge), models: 1},
		{name: "future success", checkedAt: base.Add(time.Second), models: 1},
		{name: "negative model count", checkedAt: base, models: -1},
		{name: "negative duration", checkedAt: base, models: 1, duration: -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			profile := healthTestProfile()
			profile.Status = ProviderConfigured
			provider := fixedProbeProvider{
				staticProvider: staticProvider{profile: profile},
				result:         ProbeResult{ProviderID: profile.ProviderID, Status: ProviderActive, CheckedAt: tc.checkedAt, ModelsSeen: tc.models, DurationMs: tc.duration},
			}
			service := NewService(&Registry{providers: []Provider{provider}})
			service.now = func() time.Time { return base }
			result, err := service.Probe(context.Background(), profile.ProviderID)
			observed, _ := service.Profile(profile.ProviderID, profile.ModelID)
			if err != nil || result.Status != ProviderFailed || observed.Status != ProviderFailed || observed.ClaimLevel == ClaimProbed {
				t.Fatalf("invalid probe promoted health: result=%#v observed=%#v err=%v", result, observed, err)
			}
		})
	}
}

func TestProductionHealthOlderInferenceFailureCannotOverwriteNewerProbe(t *testing.T) {
	base := time.Now().UTC()
	profile := healthTestProfile()
	profile.Status = ProviderConfigured
	provider := fixedProbeProvider{
		staticProvider: staticProvider{profile: profile},
		result:         ProbeResult{ProviderID: profile.ProviderID, Status: ProviderActive, ModelsSeen: 1, CheckedAt: base},
	}
	service := NewService(&Registry{providers: []Provider{provider}})
	service.now = func() time.Time { return base }
	olderInference := service.startHealthCheck(profile.ProviderID)
	if _, err := service.Probe(nil, profile.ProviderID); err != nil {
		t.Fatalf("newer probe: %v", err)
	}
	service.recordInferenceFailure(profile, olderInference)
	observed, _ := service.Profile(profile.ProviderID, profile.ModelID)
	if observed.Status != ProviderActive || observed.LastProbedAt == nil || !observed.LastProbedAt.Equal(base) {
		t.Fatalf("older inference failure replaced a newer live probe: %#v", observed)
	}
	newerInference := service.startHealthCheck(profile.ProviderID)
	service.recordInferenceFailure(profile, newerInference)
	observed, _ = service.Profile(profile.ProviderID, profile.ModelID)
	if observed.Status != ProviderFailed || observed.LastProbedAt == nil || !observed.LastProbedAt.Equal(base) {
		t.Fatalf("newer inference failure retained active health or invented a probe: %#v", observed)
	}
	if _, err := service.Probe(nil, profile.ProviderID); err != nil {
		t.Fatalf("recovery probe: %v", err)
	}
	observed, _ = service.Profile(profile.ProviderID, profile.ModelID)
	if observed.Status != ProviderActive {
		t.Fatalf("newer successful probe did not recover failed health: %#v", observed)
	}
}

func TestProductionHealthNilDependenciesFailClosed(t *testing.T) {
	service := NewService(nil)
	if service.Overview().ActiveModels != 0 {
		t.Fatal("nil registry invented active models")
	}
	if _, err := service.Probe(nil, "missing"); err == nil {
		t.Fatal("nil registry invented provider health")
	}
	var provider *maintainedStaticProvider
	registry := &Registry{providers: []Provider{nil, provider, &testFastTriageProvider{}}}
	if len(registry.Profiles()) != 1 {
		t.Fatal("nil adapters were not excluded from the catalog")
	}
	if _, ok := registry.provider("missing"); ok {
		t.Fatal("nil adapter was available")
	}
	profile := healthTestProfile()
	if err := service.ensureModelMaintenance(nil, provider, profile); err == nil {
		t.Fatal("typed nil provider authorized maintenance")
	}
	realProvider := &maintainedStaticProvider{profile: profile, endpoint: "http://127.0.0.1:11434", modelID: profile.ModelID}
	var gate *modelMaintenanceGateStub
	service = NewService(&Registry{providers: []Provider{realProvider}}).WithModelMaintenance(gate)
	_, result, err := service.RunLane(nil, LaneFastTriage, LaneInput{}, "classify", "nil-gate")
	if err == nil || result != nil || realProvider.calls != 0 {
		t.Fatalf("typed nil maintenance gate authorized inference: result=%#v err=%v", result, err)
	}
}

func TestProductionHealthRedactsAdapterAndMaintenanceErrors(t *testing.T) {
	secretErr := errors.New("request failed: token=synthetic-health-secret")
	profile := healthTestProfile()
	provider := &healthInferenceProvider{
		maintainedStaticProvider: &maintainedStaticProvider{profile: profile, endpoint: "http://127.0.0.1:11434", modelID: profile.ModelID},
		err:                      secretErr,
	}
	service := NewService(&Registry{providers: []Provider{provider}}).WithModelMaintenance(&modelMaintenanceGateStub{})
	_, _, err := service.RunLane(nil, LaneFastTriage, LaneInput{}, "classify", "redaction")
	if err == nil || strings.Contains(err.Error(), "synthetic-health-secret") || !errors.Is(err, secretErr) {
		t.Fatal("adapter error was exposed or lost its identity")
	}
	benchmark, err := service.Benchmark(nil, profile.ProviderID, profile.ModelID)
	if err != nil || benchmark.OK || strings.Contains(benchmark.Detail, "synthetic-health-secret") || !strings.Contains(benchmark.Detail, "REDACTED") {
		t.Fatal("benchmark detail was not redacted")
	}
	service = NewService(&Registry{providers: []Provider{provider}}).WithModelMaintenance(&modelMaintenanceGateStub{err: secretErr})
	_, _, err = service.RunLane(nil, LaneFastTriage, LaneInput{}, "classify", "maintenance-redaction")
	if err == nil || strings.Contains(err.Error(), "synthetic-health-secret") || !errors.Is(err, secretErr) {
		t.Fatal("maintenance error was exposed or lost its identity")
	}
	probeProvider := fixedProbeProvider{
		staticProvider: staticProvider{profile: profile},
		result:         ProbeResult{ProviderID: profile.ProviderID, Status: ProviderFailed, Detail: secretErr.Error()},
	}
	service = NewService(&Registry{providers: []Provider{probeProvider}})
	probe, err := service.Probe(nil, profile.ProviderID)
	if err != nil || probe.Status != ProviderFailed || strings.Contains(probe.Detail, "synthetic-health-secret") || !strings.Contains(probe.Detail, "REDACTED") {
		t.Fatal("probe detail was not redacted")
	}
}

func TestProductionHealthRemoteAdapterRedactsTransportErrors(t *testing.T) {
	secretErr := errors.New("transport failed: token=synthetic-health-secret")
	provider := &remoteProvider{
		enabled: true, id: "remote-health", name: "Remote health", baseURL: "http://127.0.0.1:11434",
		modelID: "local-model", probePath: "/v1/models", genPath: "/v1/chat/completions",
		httpClient: &http.Client{Transport: roundTripper(func(*http.Request) (*http.Response, error) { return nil, secretErr })},
	}
	probe := provider.Probe(nil, time.Now().UTC())
	if probe.Status != ProviderUnavailable || strings.Contains(probe.Detail, "synthetic-health-secret") || !strings.Contains(probe.Detail, "REDACTED") {
		t.Fatal("direct adapter exposed probe transport secrets")
	}
	result, err := provider.Generate(nil, InferenceRequest{Lane: LaneFastTriage, Prompt: "classify", MaxInputTokens: 16, MaxOutputTokens: 16}, time.Now().UTC())
	if err == nil || result.OK || strings.Contains(result.Error, "synthetic-health-secret") || strings.Contains(err.Error(), "synthetic-health-secret") {
		t.Fatal("direct adapter exposed generation transport secrets")
	}
	provider.httpClient = &http.Client{Transport: roundTripper(func(request *http.Request) (*http.Response, error) {
		if request.Method == http.MethodGet {
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"data":[{"id":"local-model"}]}`))}, nil
		}
		return nil, secretErr
	})}
	result, err = provider.Generate(nil, InferenceRequest{Lane: LaneFastTriage, Prompt: "classify", MaxInputTokens: 16, MaxOutputTokens: 16}, time.Now().UTC())
	if err == nil || result.OK || strings.Contains(result.Error, "synthetic-health-secret") || strings.Contains(err.Error(), "synthetic-health-secret") || !errors.Is(err, secretErr) {
		t.Fatal("direct adapter exposed completion transport secrets or lost error identity")
	}
}

func TestProductionHealthPartialRemoteConfigurationFailsClosed(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*remoteProvider)
	}{
		{name: "nil client", mutate: func(p *remoteProvider) { p.httpClient = nil }},
		{name: "missing model", mutate: func(p *remoteProvider) { p.modelID = " " }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			provider := &remoteProvider{
				enabled: true, id: "partial-health", baseURL: "http://127.0.0.1:11434", modelID: "local-model",
				httpClient: &http.Client{Transport: roundTripper(func(*http.Request) (*http.Response, error) {
					t.Fatal("incomplete configuration reached transport")
					return nil, errors.New("unexpected transport call")
				})},
			}
			tc.mutate(provider)
			probe := provider.Probe(nil, time.Now().UTC())
			if probe.Status != ProviderNotConfigured || probe.Detail == "" {
				t.Fatalf("incomplete provider health=%#v", probe)
			}
			result, err := provider.Generate(nil, InferenceRequest{}, time.Now().UTC())
			if err == nil || result.OK || result.Error == "" {
				t.Fatalf("incomplete provider generated output: result=%#v err=%v", result, err)
			}
		})
	}
}
