package modelintelligence

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
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
func (p staticProvider) Generate(context.Context, InferenceRequest, time.Time) (InferenceResult, error) {
	return InferenceResult{ProviderID: p.profile.ProviderID, ModelID: p.profile.ModelID, OK: true}, nil
}

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

func TestRegistryTruthfulProviderStates(t *testing.T) {
	s := newTestService()
	over := s.Overview()
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

func TestConfiguredExternalProvidersAreNeverMarkedLocal(t *testing.T) {
	t.Setenv("CUSTOM_OPENAI_BASE_URL", "https://api.example.test")
	registry := NewRegistryFromEnv()

	for _, providerID := range []string{"custom-openai-compatible"} {
		provider, ok := registry.Provider(providerID)
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
	t.Setenv("OLLAMA_MODEL_IDS", "qwen2.5:7b,qwen2.5-coder:7b")
	t.Setenv("LM_STUDIO_MODEL_ID", "qwen3-local")
	t.Setenv("DSPARK_MODEL_ID", "qwen3-dspark")
	registry := NewRegistryFromEnv()

	ollama, ok := registry.Provider("ollama")
	if !ok || ollama.Profiles()[0].ModelID != "qwen2.5:7b" {
		t.Fatalf("Ollama profile must use the first explicit configured tag: %#v", ollama)
	}
	lmStudio, ok := registry.Provider("lm-studio")
	if !ok || lmStudio.Profiles()[0].ModelID != "qwen3-local" {
		t.Fatalf("LM Studio profile must use its explicit configured model ID: %#v", lmStudio)
	}
	dspark, ok := registry.Provider("dspark")
	if !ok || dspark.Profiles()[0].ModelID != "qwen3-dspark" {
		t.Fatalf("DSpark profile must use its explicit configured model ID: %#v", dspark)
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
	service := NewService(&Registry{providers: []Provider{provider}})

	result, err := service.Benchmark(context.Background(), "external", "external-model")
	if err != nil || result.OK || called {
		t.Fatalf("result=%#v err=%v called=%v", result, err, called)
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
			provider, ok := NewRegistryFromEnv().Provider(providerConfig.providerID)
			if !ok {
				t.Fatalf("%s provider is not registered", providerConfig.providerID)
			}
			if probe := provider.Probe(context.Background(), time.Now().UTC()); probe.Status != ProviderNotConfigured {
				t.Fatalf("remote endpoint must stay unconfigured, got %#v", probe)
			}
		})
	}
}

func TestEndpointValidationRejectsCredentialOrQueryData(t *testing.T) {
	for _, raw := range []string{
		"https://operator:secret@example.test/v1",
		"https://example.test/v1?token=do-not-store",
		"https://example.test/v1#credential-data",
	} {
		if err := validateEndpointURL(raw); err == nil {
			t.Fatalf("validateEndpointURL(%q) unexpectedly accepted unsafe endpoint data", raw)
		}
	}
}

func TestLocalAIRegistryRequiresLoopbackEndpointAndUsesConfiguredModel(t *testing.T) {
	t.Setenv("LOCALAI_BASE_URL", "https://models.example.test")
	registry := NewRegistryFromEnv()
	provider, ok := registry.Provider("localai")
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
	provider, ok = registry.Provider("localai")
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
	provider, ok := registry.Provider("vllm")
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
	provider, ok = registry.Provider("vllm")
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
	provider, ok := registry.Provider("sglang")
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
	provider, ok = registry.Provider("sglang")
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
	provider, ok := registry.Provider("dspark")
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
	provider, ok = registry.Provider("dspark")
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
	provider, ok := registry.Provider("mistral-rs")
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
	provider, ok = registry.Provider("mistral-rs")
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
	provider, ok := registry.Provider("llama-cpp")
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
	provider, ok = registry.Provider("llama-cpp")
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
	provider, ok := registry.Provider("litellm")
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
	provider, ok = registry.Provider("litellm")
	if !ok {
		t.Fatal("LiteLLM provider is not registered after configuration")
	}
	profile := provider.Profiles()[0]
	if profile.ModelID != "local-qwen" || profile.Status != ProviderConfigured {
		t.Fatalf("profile = %#v, want configured local-qwen", profile)
	}
	if probe := provider.Probe(context.Background(), time.Now().UTC()); probe.Status != ProviderActive || probe.ModelsSeen != 1 {
		t.Fatalf("probe = %#v, want active LiteLLM provider", probe)
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
