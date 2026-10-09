package modelintelligence

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type roundTripper func(*http.Request) (*http.Response, error)

func (f roundTripper) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

type staticProvider struct{ profile ModelProfile }

func (p staticProvider) ID() string               { return p.profile.ProviderID }
func (p staticProvider) DisplayName() string      { return p.profile.DisplayName }
func (p staticProvider) Profiles() []ModelProfile { return []ModelProfile{p.profile} }
func (p staticProvider) Probe(context.Context, time.Time) ProbeResult {
	return ProbeResult{ProviderID: p.profile.ProviderID, Status: p.profile.Status}
}
func (p staticProvider) Generate(_ context.Context, req InferenceRequest, _ time.Time) (InferenceResult, error) {
	return InferenceResult{
		ProviderID: p.profile.ProviderID, ModelID: p.profile.ModelID, Lane: req.Lane,
		Output: "ok", InputTokensActual: estimateTokens(req.Prompt), OutputTokensActual: 1,
		InputUsageReported: true, OutputUsageReported: true, OK: true,
	}, nil
}

type blockingBenchmarkProvider struct {
	profile ModelProfile
	started chan struct{}
	release chan struct{}
	once    sync.Once
	mu      sync.Mutex
	calls   int
}

func (p *blockingBenchmarkProvider) ID() string               { return p.profile.ProviderID }
func (p *blockingBenchmarkProvider) DisplayName() string      { return p.profile.DisplayName }
func (p *blockingBenchmarkProvider) Profiles() []ModelProfile { return []ModelProfile{p.profile} }
func (p *blockingBenchmarkProvider) Probe(context.Context, time.Time) ProbeResult {
	return ProbeResult{ProviderID: p.ID(), Status: ProviderActive}
}
func (p *blockingBenchmarkProvider) ModelMaintenanceIdentity() (string, string, bool) {
	return "http://127.0.0.1:11434", p.profile.ModelID, true
}
func (p *blockingBenchmarkProvider) Generate(ctx context.Context, req InferenceRequest, _ time.Time) (InferenceResult, error) {
	p.mu.Lock()
	p.calls++
	p.mu.Unlock()
	p.once.Do(func() { close(p.started) })
	select {
	case <-ctx.Done():
		return InferenceResult{}, ctx.Err()
	case <-p.release:
	}
	return InferenceResult{
		ProviderID: p.ID(), ModelID: p.profile.ModelID, Lane: req.Lane,
		Output: "ok", InputTokensActual: estimateTokens(req.Prompt), OutputTokensActual: 1,
		InputUsageReported: true, OutputUsageReported: true, OK: true,
	}, nil
}

type cancelOnProbeProvider struct {
	staticProvider
	started chan struct{}
}

func (p *cancelOnProbeProvider) Probe(ctx context.Context, now time.Time) ProbeResult {
	close(p.started)
	<-ctx.Done()
	return ProbeResult{ProviderID: p.profile.ProviderID, Status: ProviderUnavailable, CheckedAt: now}
}

type overlappingProbeProvider struct {
	staticProvider
	firstStarted chan struct{}
	releaseFirst chan struct{}
	mu           sync.Mutex
	calls        int
	releaseOnce  sync.Once
}

func (p *overlappingProbeProvider) releaseFirstProbe() {
	p.releaseOnce.Do(func() { close(p.releaseFirst) })
}

func (p *overlappingProbeProvider) Probe(ctx context.Context, now time.Time) ProbeResult {
	p.mu.Lock()
	p.calls++
	call := p.calls
	p.mu.Unlock()
	if call == 1 {
		close(p.firstStarted)
		select {
		case <-p.releaseFirst:
			return ProbeResult{ProviderID: p.profile.ProviderID, Status: ProviderActive, CheckedAt: now}
		case <-ctx.Done():
			return ProbeResult{ProviderID: p.profile.ProviderID, Status: ProviderFailed, CheckedAt: now}
		}
	}
	return ProbeResult{ProviderID: p.profile.ProviderID, Status: ProviderUnavailable, CheckedAt: now}
}

type fixedProbeProvider struct {
	staticProvider
	result ProbeResult
}

func (p fixedProbeProvider) Probe(context.Context, time.Time) ProbeResult { return p.result }

func newTestService() *Service {
	// Fixed clock keeps telemetry timestamps deterministic.
	s := NewService(NewRegistryFromEnv())
	s.now = func() time.Time { return time.Date(2026, 7, 11, 0, 0, 0, 0, time.UTC) }
	return s
}

func TestDirectModelHTTPClientDoesNotUseEnvironmentProxyOrRedirects(t *testing.T) {
	client := newDirectHTTPClient(time.Second)
	transport, ok := client.Transport.(*http.Transport)
	if !ok || transport.Proxy != nil {
		t.Fatal("model intelligence client must not inherit environment proxy settings")
	}
	if err := client.CheckRedirect(nil, nil); err != http.ErrUseLastResponse {
		t.Fatalf("redirect behavior = %v, want %v", err, http.ErrUseLastResponse)
	}
}

func TestModelsProbeRejectsMalformedProviderResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("not-json"))
	}))
	defer server.Close()

	probe := probeModelsEndpoint(context.Background(), server.Client(), "test", server.URL, "/models", "configured-model", time.Now().UTC())
	if probe.Status != ProviderFailed || probe.Detail != "invalid models response" {
		t.Fatalf("probe = %#v, want malformed provider response to fail", probe)
	}
}

func TestChatCompletionRejectsOversizedProviderResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[],"padding":"` + strings.Repeat("x", maxChatCompletionBytes) + `"}`))
	}))
	defer server.Close()

	_, err := chatCompletion(context.Background(), server.Client(), "test", "local-model", server.URL, "/chat", InferenceRequest{Prompt: "hello", Lane: LaneFastTriage})
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("chat completion error = %v, want oversized response rejection", err)
	}
}

func TestChatCompletionRejectsHTTPFailureAndEmptyAssistantText(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
		wantError  string
	}{
		{name: "provider error", statusCode: http.StatusServiceUnavailable, body: `{"error":"offline"}`, wantError: "HTTP 503"},
		{name: "no choices", statusCode: http.StatusOK, body: `{"choices":[]}`, wantError: "no assistant text"},
		{name: "empty text", statusCode: http.StatusOK, body: `{"choices":[{"message":{"content":" "}}]}`, wantError: "no assistant text"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(test.statusCode)
				_, _ = w.Write([]byte(test.body))
			}))
			defer server.Close()

			result, err := chatCompletion(context.Background(), server.Client(), "provider", "model", server.URL, "/chat", InferenceRequest{Prompt: "hello", MaxOutputTokens: 16})
			if err == nil || !strings.Contains(err.Error(), test.wantError) || result.OK {
				t.Fatalf("result=%#v err=%v, want failure containing %q", result, err, test.wantError)
			}
		})
	}
}

func TestChatCompletionPreservesProviderReportedTokenUsage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"This is a longer answer."}}],"usage":{"prompt_tokens":7,"completion_tokens":3}}`))
	}))
	defer server.Close()

	request := InferenceRequest{Prompt: "hello", MaxInputTokens: 16, MaxOutputTokens: 3}
	result, err := chatCompletion(context.Background(), server.Client(), "provider", "model", server.URL, "/chat", request)
	if err != nil {
		t.Fatalf("chat completion: %v", err)
	}
	if !result.InputUsageReported || !result.OutputUsageReported || result.InputTokensActual != 7 || result.OutputTokensActual != 3 {
		t.Fatalf("provider usage = %#v; want exact input 7 and output 3", result)
	}
	if err := validateInferenceResult(request, &result); err != nil {
		t.Fatalf("reported token limits should accept output bytes longer than output token count: %v", err)
	}
	if result.TokensPerSecond <= 0 {
		t.Fatalf("reported output usage should drive measured token throughput: %#v", result)
	}
}

func TestNonDeterministicInferenceRequiresProviderReportedOutputUsage(t *testing.T) {
	request := InferenceRequest{Prompt: "hello", MaxInputTokens: 16, MaxOutputTokens: 3, RequireReportedOutputUsage: true}
	result := InferenceResult{Output: "answer"}
	if err := validateInferenceResult(request, &result); err == nil || !strings.Contains(err.Error(), "did not report output token usage") {
		t.Fatalf("missing provider usage error = %v, want output to be withheld", err)
	}
}

func TestProviderCannotReportZeroOutputTokensForNonEmptyText(t *testing.T) {
	request := InferenceRequest{Prompt: "hello", MaxInputTokens: 16, MaxOutputTokens: 3}
	result := InferenceResult{Output: "answer", OutputUsageReported: true, OutputTokensActual: 0}
	if err := validateInferenceResult(request, &result); err == nil || !strings.Contains(err.Error(), "zero output tokens") {
		t.Fatalf("zero-token result error = %v, want non-empty output rejected", err)
	}
}

func TestPaidProjectionPreservesUnknownBillingAsNull(t *testing.T) {
	for _, test := range []struct {
		status BillingStatus
		want   any
	}{
		{status: BillingUnknown, want: nil},
		{status: BillingUnmetered, want: false},
		{status: BillingPaid, want: true},
	} {
		profile := ModelProfile{BillingStatus: test.status, Paid: paidValue(test.status)}
		encoded, err := json.Marshal(profile)
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]any
		if err := json.Unmarshal(encoded, &fields); err != nil {
			t.Fatal(err)
		}
		if fields["paid"] != test.want {
			t.Errorf("billing %q serialized paid=%#v, want %#v", test.status, fields["paid"], test.want)
		}
	}
}

func TestChatCompletionPreservesPartialProviderUsage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"answer"}}],"usage":{"prompt_tokens":7}}`))
	}))
	defer server.Close()

	result, err := chatCompletion(context.Background(), server.Client(), "provider", "model", server.URL, "/chat", InferenceRequest{Prompt: "hello", MaxInputTokens: 16, MaxOutputTokens: 3})
	if err != nil {
		t.Fatalf("chat completion: %v", err)
	}
	if !result.InputUsageReported || result.OutputUsageReported || result.InputTokensActual != 7 || result.OutputTokensEstimate <= 0 {
		t.Fatalf("partial usage = %#v; want provider input plus estimated output", result)
	}
}

func TestInvalidProviderTokenUsageIsRejectedAndStoredAsEstimate(t *testing.T) {
	request := InferenceRequest{Prompt: "hello", MaxInputTokens: 16, MaxOutputTokens: 3}
	result := InferenceResult{
		ProviderID: "provider", ModelID: "model", Output: "answer", OK: true,
		InputTokensActual: -1, InputUsageReported: true,
	}
	if err := validateInferenceResult(request, &result); err == nil || !strings.Contains(err.Error(), "negative input") {
		t.Fatalf("invalid reported usage error = %v", err)
	}
	service := NewService(NewRegistryFromEnv())
	service.recordTelemetry(result, LaneDrafting, "invalid-usage", false, false, ValidationUnvalidated, "", 0, 0)
	rows := service.Telemetry()
	if len(rows) != 1 || rows[0].UsageSource != TokenUsageProviderReportInvalid || rows[0].InputTokens < 0 || rows[0].OutputTokens < 0 {
		t.Fatalf("invalid provider usage telemetry = %#v", rows)
	}
}

func TestRegistryTruthfulProviderStates(t *testing.T) {
	s := newTestService()
	over := s.Overview()
	if over.Calibration.TotalRuns != over.TelemetryRuns {
		t.Fatalf("overview calibration runs = %d, telemetry runs = %d", over.Calibration.TotalRuns, over.TelemetryRuns)
	}
	byID := map[string]ProviderSummary{}
	for _, p := range over.Providers {
		byID[p.ID] = p
	}
	// Test providers are active (local deterministic); remote providers with no
	// env config must be not_configured — never fabricated as active.
	if byID[ProviderTestFastTriage].Status != ProviderActive {
		t.Fatalf("test-fast-triage must be active, got %s", byID[ProviderTestFastTriage].Status)
	}
	for _, id := range []string{"dspark", "ollama", "lm-studio", "llama-cpp", "localai", "vllm", "sglang", "mistral-rs", "litellm", "custom-openai-compatible"} {
		if byID[id].Status != ProviderNotConfigured {
			t.Fatalf("%s must be not_configured without env config, got %s", id, byID[id].Status)
		}
	}
}

func TestOverviewSeparatesDeterministicRulesFromActiveModels(t *testing.T) {
	overview := NewService(NewRegistryFromEnv()).Overview()
	if overview.DeterministicProfiles != 2 {
		t.Fatalf("deterministic profiles = %d, want the two built-in rules providers", overview.DeterministicProfiles)
	}
	if overview.ActiveModels != 0 {
		t.Fatalf("active model profiles = %d, want deterministic rules excluded before any real provider probe", overview.ActiveModels)
	}
	if overview.TelemetryPersistence.State != TelemetryPersistenceMemoryOnly {
		t.Fatalf("new in-memory service persistence = %q, want explicit memory-only state", overview.TelemetryPersistence.State)
	}
}

func TestCancelledProviderProbeDoesNotOverwriteKnownStatus(t *testing.T) {
	provider := &cancelOnProbeProvider{
		staticProvider: staticProvider{profile: ModelProfile{
			ProviderID: "cancel-probe", ModelID: "local-model", Status: ProviderConfigured, Local: true,
		}},
		started: make(chan struct{}),
	}
	service := NewService(&Registry{providers: []Provider{provider}})
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := service.Probe(ctx, provider.ID())
		result <- err
	}()
	<-provider.started
	cancel()
	if err := <-result; err != context.Canceled {
		t.Fatalf("cancelled probe error = %v, want %v", err, context.Canceled)
	}
	profile, ok := service.Profile(provider.ID(), provider.profile.ModelID)
	if !ok || profile.Status != ProviderConfigured {
		t.Fatalf("cancelled probe replaced configured status: profile=%#v, ok=%v", profile, ok)
	}
	if profile.LastProbedAt != nil {
		t.Fatalf("cancelled probe recorded a probe timestamp: %v", profile.LastProbedAt)
	}
}

func TestOlderProbeCompletionCannotOverwriteNewerProviderStatus(t *testing.T) {
	provider := &overlappingProbeProvider{
		staticProvider: staticProvider{profile: ModelProfile{
			ProviderID: "overlapping-probe", ModelID: "local-model", Status: ProviderConfigured,
		}},
		firstStarted: make(chan struct{}),
		releaseFirst: make(chan struct{}),
	}
	defer provider.releaseFirstProbe()
	service := NewService(&Registry{providers: []Provider{provider}})
	base := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	var clockMu sync.Mutex
	var clockCalls int
	service.now = func() time.Time {
		clockMu.Lock()
		defer clockMu.Unlock()
		value := base.Add(time.Duration(clockCalls) * time.Second)
		clockCalls++
		return value
	}

	firstDone := make(chan error, 1)
	go func() {
		_, err := service.Probe(context.Background(), provider.ID())
		firstDone <- err
	}()
	<-provider.firstStarted

	newer, err := service.Probe(context.Background(), provider.ID())
	if err != nil || newer.Status != ProviderUnavailable {
		t.Fatalf("newer probe = %#v, err=%v; want unavailable", newer, err)
	}
	provider.releaseFirstProbe()
	if err := <-firstDone; err != nil {
		t.Fatalf("older probe: %v", err)
	}

	profile, ok := service.Profile(provider.ID(), provider.profile.ModelID)
	if !ok || profile.Status != ProviderUnavailable {
		t.Fatalf("older positive result overwrote latest status: profile=%#v, found=%v", profile, ok)
	}
	wantLastProbe := base.Add(time.Second)
	if profile.LastProbedAt == nil || !profile.LastProbedAt.Equal(wantLastProbe) {
		t.Fatalf("last probe time = %v, want %v", profile.LastProbedAt, wantLastProbe)
	}
}

func TestProbeRejectsInconsistentProviderStatusMetadata(t *testing.T) {
	for _, tc := range []struct {
		name   string
		result ProbeResult
	}{
		{name: "wrong provider", result: ProbeResult{ProviderID: "another-provider", Status: ProviderActive}},
		{name: "unknown status", result: ProbeResult{ProviderID: "reported-probe", Status: ProviderStatus("ready")}},
		{name: "unfinished status", result: ProbeResult{ProviderID: "reported-probe", Status: ProviderProbing}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			provider := fixedProbeProvider{
				staticProvider: staticProvider{profile: ModelProfile{
					ProviderID: "reported-probe", ModelID: "local-model", Status: ProviderConfigured,
				}},
				result: tc.result,
			}
			service := NewService(&Registry{providers: []Provider{provider}})
			got, err := service.Probe(context.Background(), provider.ID())
			if err != nil {
				t.Fatalf("Probe: %v", err)
			}
			if got.ProviderID != provider.ID() || got.Status != ProviderFailed || got.Detail != "provider returned inconsistent probe metadata" || got.CheckedAt.IsZero() {
				t.Fatalf("normalized probe result = %#v", got)
			}
			profile, ok := service.Profile(provider.ID(), provider.profile.ModelID)
			if !ok || profile.Status != ProviderFailed {
				t.Fatalf("catalog accepted inconsistent probe result: profile=%#v, found=%v", profile, ok)
			}
		})
	}
}

func TestConfiguredExternalProvidersAreNeverMarkedLocal(t *testing.T) {
	t.Setenv("CUSTOM_OPENAI_BASE_URL", "https://api.example.test")
	registry := NewRegistryFromEnv()

	for _, providerID := range []string{"custom-openai-compatible"} {
		provider, ok := registry.provider(providerID)
		if !ok {
			t.Fatalf("provider %q not found", providerID)
		}
		profiles := provider.Profiles()
		if len(profiles) != 1 || profiles[0].Local {
			t.Fatalf("provider %q profile must be external: %#v", providerID, profiles)
		}
	}
}

func TestLocalProviderProfilesUseExplicitConfiguredModelIDs(t *testing.T) {
	t.Setenv("OLLAMA_BASE_URL", "http://127.0.0.1:11434")
	t.Setenv("OLLAMA_MODEL_IDS", "qwen2.5:7b,qwen2.5-coder:7b")
	t.Setenv("LM_STUDIO_MODEL_ID", "qwen3-local")
	t.Setenv("DSPARK_MODEL_ID", "qwen3-dspark")
	registry := NewRegistryFromEnv()

	ollama, ok := registry.provider("ollama")
	if !ok || ollama.Profiles()[0].ModelID != "qwen2.5:7b" {
		t.Fatalf("Ollama profile must use the first explicit configured tag: %#v", ollama)
	}
	lmStudio, ok := registry.provider("lm-studio")
	if !ok || lmStudio.Profiles()[0].ModelID != "qwen3-local" {
		t.Fatalf("LM Studio profile must use its explicit configured model ID: %#v", lmStudio)
	}
	dspark, ok := registry.provider("dspark")
	if !ok || dspark.Profiles()[0].ModelID != "qwen3-dspark" {
		t.Fatalf("DSpark profile must use its explicit configured model ID: %#v", dspark)
	}
	ollamaProfile := ollama.Profiles()[0]
	if !ollamaProfile.EndpointLocal || ollamaProfile.Local || ollamaProfile.LocalInferenceOperatorAttested || ollamaProfile.BillingStatus != BillingUnknown {
		t.Fatalf("local endpoint alone must not imply local or unmetered inference: %#v", ollamaProfile)
	}
}

func TestLocalInferenceRequiresExactOperatorAttestation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"id":"qwen-local"}]}`))
	}))
	defer server.Close()
	t.Setenv("OLLAMA_BASE_URL", server.URL)
	t.Setenv("OLLAMA_MODEL_IDS", "qwen-local,other-model")
	t.Setenv(localInferenceAttestationEnv, "ollama/other-model,ollama/*,litellm/qwen-local")

	getOllama := func() ModelProfile {
		provider, ok := NewRegistryFromEnv().provider("ollama")
		if !ok {
			t.Fatal("Ollama provider missing from registry")
		}
		return provider.Profiles()[0]
	}
	profile := getOllama()
	if !profile.EndpointLocal || profile.Local || profile.LocalInferenceOperatorAttested || profile.BillingStatus != BillingUnknown {
		t.Fatalf("non-exact attestation must not authorize inference: %#v", profile)
	}

	t.Setenv(localInferenceAttestationEnv, "ollama/qwen-local")
	profile = getOllama()
	if !profile.Local || !profile.EndpointLocal || !profile.LocalInferenceOperatorAttested || profile.BillingStatus != BillingUnmetered {
		t.Fatalf("exact operator attestation should be represented explicitly: %#v", profile)
	}
}

func TestOllamaCloudModelCannotBeAttestedAsLocal(t *testing.T) {
	t.Setenv(localInferenceAttestationEnv, "ollama/qwen3-coder:480b-cloud,ollama/deepseek-v3:cloud,ollama/local-model")
	for _, modelID := range []string{"qwen3-coder:480b-cloud", "deepseek-v3:cloud"} {
		if localInferenceOperatorAttested("ollama", modelID) {
			t.Errorf("cloud-hosted Ollama model %q must not be treated as local", modelID)
		}
	}
	if !localInferenceOperatorAttested("ollama", "local-model") {
		t.Fatal("exact attestation for a local Ollama model should remain supported")
	}
}

func TestRouterPrefersConfiguredModelOverDeterministicFallback(t *testing.T) {
	deterministic := &testFastTriageProvider{}
	configured := staticProvider{profile: ModelProfile{
		ProviderID: "ollama", ModelID: "qwen-local", DisplayName: "Configured local model",
		Lanes: []RoutingLane{LaneFastTriage}, Local: true,
		BillingStatus: BillingUnmetered, EndpointLocal: true,
		Status: ProviderActive, ClaimLevel: ClaimConfigured,
	}}
	router := NewRouter(&Registry{providers: []Provider{deterministic, configured}})
	decision := router.Route(LaneFastTriage, LaneInput{SafeForCloud: false}, time.Now().UTC())
	if !decision.Routable || decision.ProviderID != "ollama" || decision.ModelID != "qwen-local" {
		t.Fatalf("route = %#v; configured local model should precede deterministic fallback", decision)
	}
}

func TestExternalModelIntelligenceBenchmarkDoesNotReachProvider(t *testing.T) {
	called := false
	provider := &remoteProvider{
		enabled: true, id: "external", name: "External", baseURL: "https://api.example.test",
		modelID: "external-model", lanes: []RoutingLane{LaneFastTriage}, local: false,
		httpClient: &http.Client{Transport: roundTripper(func(*http.Request) (*http.Response, error) {
			called = true
			return nil, nil
		})},
	}
	gate := &modelMaintenanceGateStub{}
	service := NewService(&Registry{providers: []Provider{provider}}).WithModelMaintenance(gate)

	result, err := service.Benchmark(context.Background(), "external", "external-model")
	if err != nil || result.OK || called || gate.calls != 0 {
		t.Fatalf("result=%#v err=%v providerCalled=%v localMaintenanceCalls=%d", result, err, called, gate.calls)
	}
	if !strings.Contains(result.Detail, "canonical LLM policy router") {
		t.Fatalf("detail = %q", result.Detail)
	}
}

func TestModelIntelligenceRouterRejectsActiveExternalModels(t *testing.T) {
	router := NewRouter(&Registry{providers: []Provider{staticProvider{profile: ModelProfile{
		ProviderID: "external", ModelID: "external-model", DisplayName: "External", Local: false,
		Status: ProviderActive, Lanes: []RoutingLane{LaneFastTriage},
	}}}})

	decision := router.Route(LaneFastTriage, LaneInput{SafeForCloud: true}, time.Now())
	if decision.Routable || !strings.Contains(decision.Reason, "canonical LLM policy router") {
		t.Fatalf("decision = %#v", decision)
	}
	if len(decision.Fallbacks) != 1 || !strings.Contains(decision.Fallbacks[0], "external model intelligence execution is disabled") {
		t.Fatalf("fallbacks = %#v", decision.Fallbacks)
	}
}

func TestNamedLocalProviderProfilesRejectRemoteEndpoints(t *testing.T) {
	for _, providerConfig := range []struct {
		providerID string
		envName    string
	}{
		{providerID: "ollama", envName: "OLLAMA_BASE_URL"},
		{providerID: "lm-studio", envName: "LM_STUDIO_BASE_URL"},
		{providerID: "llama-cpp", envName: "LLAMA_CPP_BASE_URL"},
		{providerID: "localai", envName: "LOCALAI_BASE_URL"},
		{providerID: "vllm", envName: "VLLM_BASE_URL"},
		{providerID: "sglang", envName: "SGLANG_BASE_URL"},
		{providerID: "mistral-rs", envName: "MISTRAL_RS_BASE_URL"},
	} {
		t.Run(providerConfig.providerID, func(t *testing.T) {
			t.Setenv(providerConfig.envName, "https://models.example.test")
			provider, ok := NewRegistryFromEnv().provider(providerConfig.providerID)
			if !ok {
				t.Fatalf("%s provider is not registered", providerConfig.providerID)
			}
			if probe := provider.Probe(context.Background(), time.Now().UTC()); probe.Status != ProviderNotConfigured {
				t.Fatalf("remote endpoint must stay unconfigured, got %#v", probe)
			}
		})
	}
}

func TestLocalAIRegistryRequiresLoopbackEndpointAndUsesConfiguredModel(t *testing.T) {
	t.Setenv("LOCALAI_BASE_URL", "https://models.example.test")
	registry := NewRegistryFromEnv()
	provider, ok := registry.provider("localai")
	if !ok {
		t.Fatal("LocalAI provider is not registered")
	}
	if probe := provider.Probe(context.Background(), time.Now().UTC()); probe.Status != ProviderNotConfigured {
		t.Fatalf("remote LocalAI endpoint must stay unconfigured, got %#v", probe)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Fatalf("path = %s, want /v1/models", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"qwen-localai"}]}`))
	}))
	defer server.Close()
	t.Setenv("LOCALAI_BASE_URL", server.URL)
	t.Setenv("LOCALAI_MODEL_ID", "qwen-localai")
	registry = NewRegistryFromEnv()
	provider, ok = registry.provider("localai")
	if !ok {
		t.Fatal("LocalAI provider is not registered after configuration")
	}
	profile := provider.Profiles()[0]
	if profile.ModelID != "qwen-localai" || profile.Status != ProviderConfigured {
		t.Fatalf("profile = %#v, want configured qwen-localai", profile)
	}
	if probe := provider.Probe(context.Background(), time.Now().UTC()); probe.Status != ProviderActive || probe.ModelsSeen != 1 {
		t.Fatalf("probe = %#v, want active LocalAI provider", probe)
	}
}

func TestVLLMRegistryRequiresLoopbackEndpointAndUsesConfiguredModel(t *testing.T) {
	t.Setenv("VLLM_BASE_URL", "https://models.example.test")
	registry := NewRegistryFromEnv()
	provider, ok := registry.provider("vllm")
	if !ok {
		t.Fatal("vLLM provider is not registered")
	}
	if probe := provider.Probe(context.Background(), time.Now().UTC()); probe.Status != ProviderNotConfigured {
		t.Fatalf("remote vLLM endpoint must stay unconfigured, got %#v", probe)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Fatalf("path = %s, want /v1/models", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"qwen-vllm"}]}`))
	}))
	defer server.Close()
	t.Setenv("VLLM_BASE_URL", server.URL)
	t.Setenv("VLLM_MODEL_ID", "qwen-vllm")
	registry = NewRegistryFromEnv()
	provider, ok = registry.provider("vllm")
	if !ok {
		t.Fatal("vLLM provider is not registered after configuration")
	}
	profile := provider.Profiles()[0]
	if profile.ModelID != "qwen-vllm" || profile.Status != ProviderConfigured {
		t.Fatalf("profile = %#v, want configured qwen-vllm", profile)
	}
	if probe := provider.Probe(context.Background(), time.Now().UTC()); probe.Status != ProviderActive || probe.ModelsSeen != 1 {
		t.Fatalf("probe = %#v, want active vLLM provider", probe)
	}
}

func TestSGLangRegistryRequiresLoopbackEndpointAndUsesConfiguredModel(t *testing.T) {
	t.Setenv("SGLANG_BASE_URL", "https://models.example.test")
	registry := NewRegistryFromEnv()
	provider, ok := registry.provider("sglang")
	if !ok {
		t.Fatal("SGLang provider is not registered")
	}
	if probe := provider.Probe(context.Background(), time.Now().UTC()); probe.Status != ProviderNotConfigured {
		t.Fatalf("remote SGLang endpoint must stay unconfigured, got %#v", probe)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Fatalf("path = %s, want /v1/models", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"qwen-sglang"}]}`))
	}))
	defer server.Close()
	t.Setenv("SGLANG_BASE_URL", server.URL)
	t.Setenv("SGLANG_MODEL_ID", "qwen-sglang")
	registry = NewRegistryFromEnv()
	provider, ok = registry.provider("sglang")
	if !ok {
		t.Fatal("SGLang provider is not registered after configuration")
	}
	profile := provider.Profiles()[0]
	if profile.ModelID != "qwen-sglang" || profile.Status != ProviderConfigured {
		t.Fatalf("profile = %#v, want configured qwen-sglang", profile)
	}
	if probe := provider.Probe(context.Background(), time.Now().UTC()); probe.Status != ProviderActive || probe.ModelsSeen != 1 {
		t.Fatalf("probe = %#v, want active SGLang provider", probe)
	}
}

func TestDSparkRegistryRequiresExplicitLoopbackAndUsesConfiguredModel(t *testing.T) {
	t.Setenv("DSPARK_ENABLED", "true")
	t.Setenv("DSPARK_BASE_URL", "https://models.example.test")
	registry := NewRegistryFromEnv()
	provider, ok := registry.provider("dspark")
	if !ok {
		t.Fatal("DSpark provider is not registered")
	}
	if probe := provider.Probe(context.Background(), time.Now().UTC()); probe.Status != ProviderNotConfigured {
		t.Fatalf("remote DSpark endpoint must stay unconfigured, got %#v", probe)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Fatalf("path = %s, want /v1/models", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"qwen-dspark"}]}`))
	}))
	defer server.Close()
	t.Setenv("DSPARK_BASE_URL", server.URL)
	t.Setenv("DSPARK_MODEL_ID", "qwen-dspark")
	registry = NewRegistryFromEnv()
	provider, ok = registry.provider("dspark")
	if !ok {
		t.Fatal("DSpark provider is not registered after configuration")
	}
	profile := provider.Profiles()[0]
	if profile.ModelID != "qwen-dspark" || profile.Status != ProviderConfigured {
		t.Fatalf("profile = %#v, want configured qwen-dspark", profile)
	}
	maintained, ok := provider.(MaintainedLocalProvider)
	if !ok {
		t.Fatal("DSpark must expose its canonical maintenance identity")
	}
	endpoint, modelID, applicable := maintained.ModelMaintenanceIdentity()
	if !applicable || endpoint != server.URL || modelID != "qwen-dspark" {
		t.Fatalf("maintenance identity = endpoint=%q model=%q applicable=%v", endpoint, modelID, applicable)
	}
	if probe := provider.Probe(context.Background(), time.Now().UTC()); probe.Status != ProviderActive || probe.ModelsSeen != 1 {
		t.Fatalf("probe = %#v, want active DSpark provider", probe)
	}
}

func TestMistralRSRegistryRequiresLoopbackEndpointAndUsesConfiguredModel(t *testing.T) {
	t.Setenv("MISTRAL_RS_BASE_URL", "https://models.example.test")
	registry := NewRegistryFromEnv()
	provider, ok := registry.provider("mistral-rs")
	if !ok {
		t.Fatal("mistral.rs provider is not registered")
	}
	if probe := provider.Probe(context.Background(), time.Now().UTC()); probe.Status != ProviderNotConfigured {
		t.Fatalf("remote mistral.rs endpoint must stay unconfigured, got %#v", probe)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Fatalf("path = %s, want /v1/models", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"qwen-mistralrs"}]}`))
	}))
	defer server.Close()
	t.Setenv("MISTRAL_RS_BASE_URL", server.URL)
	t.Setenv("MISTRAL_RS_MODEL_ID", "qwen-mistralrs")
	registry = NewRegistryFromEnv()
	provider, ok = registry.provider("mistral-rs")
	if !ok {
		t.Fatal("mistral.rs provider is not registered after configuration")
	}
	profile := provider.Profiles()[0]
	if profile.ModelID != "qwen-mistralrs" || profile.Status != ProviderConfigured {
		t.Fatalf("profile = %#v, want configured qwen-mistralrs", profile)
	}
	if probe := provider.Probe(context.Background(), time.Now().UTC()); probe.Status != ProviderActive || probe.ModelsSeen != 1 {
		t.Fatalf("probe = %#v, want active mistral.rs provider", probe)
	}
}

func TestLlamaCPPRegistryRequiresLocalEndpointAndUsesConfiguredModel(t *testing.T) {
	t.Setenv("LLAMA_CPP_BASE_URL", "https://models.example.test")
	registry := NewRegistryFromEnv()
	provider, ok := registry.provider("llama-cpp")
	if !ok {
		t.Fatal("llama.cpp provider is not registered")
	}
	if probe := provider.Probe(context.Background(), time.Now().UTC()); probe.Status != ProviderNotConfigured {
		t.Fatalf("remote llama.cpp endpoint must stay unconfigured, got %#v", probe)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Fatalf("path = %s, want /v1/models", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"qwen3-gguf"}]}`))
	}))
	defer server.Close()
	t.Setenv("LLAMA_CPP_BASE_URL", server.URL)
	t.Setenv("LLAMA_CPP_MODEL_ID", "qwen3-gguf")
	registry = NewRegistryFromEnv()
	provider, ok = registry.provider("llama-cpp")
	if !ok {
		t.Fatal("llama.cpp provider is not registered after configuration")
	}
	profile := provider.Profiles()[0]
	if profile.ModelID != "qwen3-gguf" || profile.Status != ProviderConfigured {
		t.Fatalf("profile = %#v, want configured qwen3-gguf", profile)
	}
	probe := provider.Probe(context.Background(), time.Now().UTC())
	if probe.Status != ProviderActive || probe.ModelsSeen != 1 {
		t.Fatalf("probe = %#v, want active llama.cpp provider", probe)
	}
}

func TestLiteLLMRegistryRequiresExplicitLocalAuthenticatedGateway(t *testing.T) {
	t.Setenv("LITELLM_ENABLED", "true")
	t.Setenv("LITELLM_BASE_URL", "https://models.example.test")
	t.Setenv("LITELLM_MODEL_ID", "local-qwen")
	t.Setenv("LITELLM_API_KEY", "gateway-secret")
	registry := NewRegistryFromEnv()
	provider, ok := registry.provider("litellm")
	if !ok {
		t.Fatal("LiteLLM provider is not registered")
	}
	if probe := provider.Probe(context.Background(), time.Now().UTC()); probe.Status != ProviderNotConfigured {
		t.Fatalf("remote LiteLLM endpoint must stay unconfigured, got %#v", probe)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Fatalf("path = %s, want /v1/models", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer gateway-secret" {
			t.Fatalf("authorization = %q", got)
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"local-qwen"}]}`))
	}))
	defer server.Close()
	t.Setenv("LITELLM_BASE_URL", server.URL)
	registry = NewRegistryFromEnv()
	provider, ok = registry.provider("litellm")
	if !ok {
		t.Fatal("LiteLLM provider is not registered after configuration")
	}
	profile := provider.Profiles()[0]
	if profile.ModelID != "local-qwen" || profile.Status != ProviderConfigured {
		t.Fatalf("profile = %#v, want configured local-qwen", profile)
	}
	if !profile.EndpointLocal || profile.Local || profile.BillingStatus != BillingUnknown {
		t.Fatalf("loopback gateway must not imply local inference or unmetered billing: %#v", profile)
	}
	if probe := provider.Probe(context.Background(), time.Now().UTC()); probe.Status != ProviderActive || probe.ModelsSeen != 1 {
		t.Fatalf("probe = %#v, want active LiteLLM provider", probe)
	}
}

func TestLiteLLMUnknownUpstreamBillingBlocksGenerationBeforeNetworkCall(t *testing.T) {
	var generationCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/chat/completions" {
			generationCalls++
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"local-model"}]}`))
	}))
	defer server.Close()
	t.Setenv("LITELLM_ENABLED", "true")
	t.Setenv("LITELLM_BASE_URL", server.URL)
	t.Setenv("LITELLM_MODEL_ID", "local-model")
	t.Setenv("LITELLM_API_KEY", "gateway-secret")

	service := NewService(NewRegistryFromEnv())
	result, err := service.Benchmark(context.Background(), "litellm", "local-model")
	if err != nil || result.OK || !strings.Contains(result.Detail, "unknown billing") {
		t.Fatalf("unknown-billing gateway benchmark = %#v, err=%v", result, err)
	}
	if generationCalls != 0 {
		t.Fatalf("unknown-billing gateway reached generation %d time(s)", generationCalls)
	}
}

func TestFastTriageLaneAffectsBehavior(t *testing.T) {
	s := newTestService()
	res, err := s.Triage(context.Background(), "review_invoice", "Pay invoice", "Please pay the rent invoice", true, false, "op-1")
	if err != nil {
		t.Fatalf("triage: %v", err)
	}
	if !res.Routed {
		t.Fatalf("fast-triage lane must route to an active model")
	}
	if res.Category != "financial" {
		t.Fatalf("expected financial category, got %q", res.Category)
	}
	if res.ProviderID != ProviderTestFastTriage {
		t.Fatalf("expected the triage provider, got %q", res.ProviderID)
	}
	// The call must have produced real telemetry.
	if len(s.Telemetry()) == 0 {
		t.Fatalf("triage must record telemetry")
	}
}

func TestPrivacyLaneRestrictsCloud(t *testing.T) {
	s := newTestService()
	// All 2B providers are local, so a privacy-restricted route still succeeds
	// on a local model; the decision must record the cloud restriction.
	dec := s.router.Route(LaneFastTriage, LaneInput{SafeForCloud: false}, s.now())
	if !dec.CloudRestricted {
		t.Fatalf("route must record cloud restriction when content is not safe for cloud")
	}
	if dec.Routable && !dec.Local {
		t.Fatalf("privacy-restricted route must select a local model")
	}
}

func TestBenchmarkRecordsClaimAndTelemetry(t *testing.T) {
	s := newTestService()
	res, err := s.Benchmark(context.Background(), ProviderTestFastTriage, "triage-rules-v1")
	if err != nil {
		t.Fatalf("benchmark: %v", err)
	}
	if !res.OK || res.ClaimLevel != ClaimBenchmarked {
		t.Fatalf("benchmark must promote to benchmarked, got ok=%v claim=%s", res.OK, res.ClaimLevel)
	}
	// A not-configured provider benchmarks truthfully: attempted, not usable, no promotion.
	res2, err := s.Benchmark(context.Background(), "dspark", "dspark-default")
	if err != nil {
		t.Fatalf("benchmark dspark: %v", err)
	}
	if res2.OK {
		t.Fatalf("dspark must not benchmark OK when not configured")
	}
	if res2.ClaimLevel == ClaimBenchmarked {
		t.Fatalf("not-configured provider must not be promoted to benchmarked")
	}
}

func TestConcurrentBenchmarkForSameModelIsRejectedWithoutDuplicateInference(t *testing.T) {
	provider := &blockingBenchmarkProvider{
		started: make(chan struct{}), release: make(chan struct{}),
		profile: ModelProfile{ProviderID: "local-test", ModelID: "qwen-local", DisplayName: "Local model", Local: true, EndpointLocal: true, BillingStatus: BillingUnmetered, Status: ProviderActive, Lanes: []RoutingLane{LaneFastTriage}},
	}
	service := NewService(&Registry{providers: []Provider{provider}}).WithModelMaintenance(&modelMaintenanceGateStub{})
	firstDone := make(chan BenchmarkResult, 1)
	go func() {
		result, _ := service.Benchmark(context.Background(), provider.profile.ProviderID, provider.profile.ModelID)
		firstDone <- result
	}()
	<-provider.started
	second, err := service.Benchmark(context.Background(), provider.profile.ProviderID, provider.profile.ModelID)
	if err != nil || second.OK || !strings.Contains(second.Detail, "already running") {
		t.Fatalf("duplicate benchmark = %#v, err=%v", second, err)
	}
	close(provider.release)
	first := <-firstDone
	if !first.OK || provider.calls != 1 {
		t.Fatalf("first benchmark=%#v; generation calls=%d, want exactly one", first, provider.calls)
	}
}

func TestLaneWinnersOnlyFromObservedRuns(t *testing.T) {
	s := newTestService()
	if len(s.LaneWinners()) != 0 {
		t.Fatalf("no telemetry yet -> no lane winners")
	}
	_, _ = s.Triage(context.Background(), "note", "Organize notes", "cleanup", true, false, "op-2")
	winners := s.LaneWinners()
	if len(winners) == 0 {
		t.Fatalf("a routed run must yield a lane winner")
	}
}

func TestBudgetDefaultsConservativeAndValidated(t *testing.T) {
	s := newTestService()
	b := s.TokenBudgetDefaults()
	if b.ContextStrategy != ContextEvidenceOnly || b.MaximumReasoning != EffortLow {
		t.Fatalf("defaults must be conservative (evidence_only/low), got %s/%s", b.ContextStrategy, b.MaximumReasoning)
	}
	bad := b
	bad.ContextStrategy = "everything"
	if _, err := s.SetTokenBudgetDefaults(bad); err == nil {
		t.Fatalf("invalid context strategy must be rejected")
	}
}

func TestDSparkURLValidation(t *testing.T) {
	bad := []string{"ftp://x", "http://169.254.169.254/v1", "http://0.0.0.0/", "http://metadata.google.internal/"}
	for _, u := range bad {
		if err := validateEndpointURL(u); err == nil {
			t.Fatalf("URL %q must be rejected", u)
		}
	}
	for _, u := range []string{"http://localhost:1234", "http://127.0.0.1:8080", "https://api.example.com"} {
		if err := validateEndpointURL(u); err != nil {
			t.Fatalf("URL %q must be allowed: %v", u, err)
		}
	}
}

func TestCacheReuseBoundaries(t *testing.T) {
	c := NewCache()
	now := time.Now()
	c.Store(CacheDeterministicResult, "p1", "out", "revA", false, true, false, now)
	// Unverified output must not be reused for a high-risk action.
	if _, ok := c.Get(CacheLookup{CacheType: CacheDeterministicResult, Prompt: "p1", ForHighRiskAction: true, SafeForCloud: true}); ok {
		t.Fatalf("unverified output must not be reused for high-risk actions")
	}
	// Changed source revision must invalidate reuse.
	if _, ok := c.Get(CacheLookup{CacheType: CacheDeterministicResult, Prompt: "p1", SourceRevisionHash: "revB", SafeForCloud: true}); ok {
		t.Fatalf("changed source revision must not be reused")
	}
	// Same revision, low-risk, safe -> reusable.
	if _, ok := c.Get(CacheLookup{CacheType: CacheDeterministicResult, Prompt: "p1", SourceRevisionHash: "revA", SafeForCloud: true}); !ok {
		t.Fatalf("matching, low-risk, safe lookup should hit")
	}
}

func TestQualityScoreRewardsVerifiedWork(t *testing.T) {
	low := ComputeQualityScore(QualityInputs{VerifiedCompletions: 1, TokensUsed: 100000, HumanRepairs: 5})
	high := ComputeQualityScore(QualityInputs{VerifiedCompletions: 10, TokensUsed: 1000})
	if high.Score <= low.Score {
		t.Fatalf("more verified work at lower cost must score higher: high=%f low=%f", high.Score, low.Score)
	}
}
