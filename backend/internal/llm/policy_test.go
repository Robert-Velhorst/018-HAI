package llm

import (
	"automation-hub-backend/internal/models"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRouteSkipsWeakFreeModelForCodingTask(t *testing.T) {
	service := &Service{policy: testPolicyWithLocalEndpoints()}

	decision, err := service.Route(RouteRequest{Task: "Fix a Go API bug and explain the compile failure"})
	if err != nil {
		t.Fatalf("Route returned error: %v", err)
	}

	if decision.SelectedModelID == "phi3:mini" {
		t.Fatalf("selected weak model %q for coding task", decision.SelectedModelID)
	}
	if decision.Tier != TierLocal {
		t.Fatalf("selected tier %q, want %q", decision.Tier, TierLocal)
	}
	if decision.Classification.TaskType != "coding" {
		t.Fatalf("classified task as %q, want coding", decision.Classification.TaskType)
	}
}

func TestRouteIncludesTaskSpecificTokenAndCostEstimate(t *testing.T) {
	policy := Policy{
		DailyPaidBudgetEUR:             5,
		PaidCallsAllowed:               true,
		LocalModelsAllowed:             true,
		FreeCloudQuotaAllowed:          true,
		LocalFirst:                     true,
		RequireApprovalBeforePaidUsage: false,
		TierOrder:                      []string{TierLocal, TierFree, TierCheap, TierAcceptable, TierHigh, TierPremium, TierExpensive},
		Providers: []Provider{
			{
				ID:             "priced-test",
				Name:           "Priced test provider",
				Enabled:        true,
				Paid:           true,
				EndpointURL:    "http://localhost:9999",
				DailyBudgetEUR: 5,
				Models: []Model{
					{
						ID:                            "priced-coder",
						Name:                          "Priced coder",
						Tier:                          TierCheap,
						Capabilities:                  []string{"general", "coding"},
						MaxDifficulty:                 5,
						MaxReasoning:                  "high",
						InputCostPerMillionTokensEUR:  2,
						OutputCostPerMillionTokensEUR: 6,
						PricingSource:                 "test price sheet",
						Enabled:                       true,
					},
				},
			},
		},
	}
	service := &Service{policy: policy}

	decision, err := service.Route(RouteRequest{Task: "Fix a Go API bug and explain the compile failure"})
	if err != nil {
		t.Fatalf("Route returned error: %v", err)
	}

	if decision.SelectedModelID != "priced-coder" {
		t.Fatalf("selected model = %q, want priced-coder", decision.SelectedModelID)
	}
	if decision.EstimatedInputTokens == 0 || decision.EstimatedOutputTokens == 0 {
		t.Fatalf("missing token estimates: %#v", decision)
	}
	if decision.EstimatedCostEUR <= 0 {
		t.Fatalf("estimated cost = %f, want > 0", decision.EstimatedCostEUR)
	}
	if decision.PricingSource != "test price sheet" {
		t.Fatalf("pricing source = %q, want test price sheet", decision.PricingSource)
	}
	if !strings.Contains(decision.Reason, "input") || !strings.Contains(decision.Reason, "output") {
		t.Fatalf("reason should include token estimate, got %q", decision.Reason)
	}
}

func TestRouteMovesPastPreviousModelAfterValidationFailure(t *testing.T) {
	service := &Service{policy: testPolicyWithLocalEndpoints()}
	validationPassed := false

	decision, err := service.Route(RouteRequest{
		Task:             "Summarize and classify these short notes",
		ValidationPassed: &validationPassed,
		PreviousModelID:  "phi3:mini",
	})
	if err != nil {
		t.Fatalf("Route returned error: %v", err)
	}

	if decision.SelectedModelID == "phi3:mini" {
		t.Fatalf("selected previous failed model")
	}
	if len(decision.Skipped) == 0 {
		t.Fatalf("expected skipped models to explain validation fallback")
	}
}

func TestPaidProviderDisabledByDefault(t *testing.T) {
	service := &Service{policy: testPolicyWithLocalEndpoints()}

	decision, err := service.Route(RouteRequest{
		Task:              "Handle a legal financial medical decision with verification",
		Difficulty:        5,
		RequiredReasoning: "very_high",
	})
	if err != nil {
		t.Fatalf("Route returned error: %v", err)
	}

	if decision.RequiresApproval {
		t.Fatalf("default route should not select paid or expensive models")
	}
	for _, provider := range service.policy.Providers {
		if provider.ID == decision.SelectedProviderID && provider.Paid {
			t.Fatalf("default route selected paid provider %q", provider.ID)
		}
	}
}

func TestDefaultPolicyDoesNotExposeSyntheticProviderFillers(t *testing.T) {
	forbidden := map[string]bool{
		"cheap-provider":      true,
		"acceptable-provider": true,
		"high-provider":       true,
		"premium-provider":    true,
		"paid-provider":       true,
	}
	for _, provider := range defaultPolicy().Providers {
		if forbidden[provider.ID] {
			t.Fatalf("default policy exposes synthetic provider %q", provider.ID)
		}
		if strings.Contains(strings.ToLower(provider.Name), "placeholder") {
			t.Fatalf("default policy exposes placeholder provider name %q", provider.Name)
		}
	}
}

func TestRouteSkipsProvidersWithoutConfiguredEndpoints(t *testing.T) {
	service := &Service{policy: testPolicyWithoutEndpoints()}

	decision, err := service.Route(RouteRequest{Task: "Summarize this short note"})
	if err != nil {
		t.Fatalf("Route returned error: %v", err)
	}

	if decision.SelectedModelID != "" {
		t.Fatalf("selected %q even though no provider endpoint is configured", decision.SelectedModelID)
	}
	if len(decision.Skipped) == 0 {
		t.Fatalf("expected skipped models to explain missing provider endpoints")
	}
}

func TestPolicyMarksUnconfiguredProviders(t *testing.T) {
	service := &Service{policy: testPolicyWithoutEndpoints()}
	policy := service.Policy()

	if policy.Providers[0].Configured {
		t.Fatalf("ollama provider should not be configured without OLLAMA_BASE_URL")
	}
	if policy.Providers[0].ReadinessStatus != "not_configured" {
		t.Fatalf("readiness = %q, want not_configured", policy.Providers[0].ReadinessStatus)
	}
}

func TestOllamaComposeInternalEndpointIsLocalOnly(t *testing.T) {
	if !isLocalModelHostForProvider("ollama", "ollama-local") {
		t.Fatal("canonical Compose-internal Ollama endpoint was rejected")
	}
	for _, providerID := range []string{"lm-studio", "llama-cpp", "localai", "vllm", "dspark"} {
		if isLocalModelHostForProvider(providerID, "ollama-local") {
			t.Fatalf("provider %q accepted Ollama's private service name", providerID)
		}
	}
	for _, host := range []string{"ollama", "ollama.example.test", "models.internal"} {
		if isLocalModelHostForProvider("ollama", host) {
			t.Fatalf("arbitrary hostname accepted as local: %q", host)
		}
	}
}

func TestRouteBlocksLinkLocalProviderEndpointByDefault(t *testing.T) {
	policy := testPolicyWithoutEndpoints()
	policy.Providers[0].EndpointURL = "http://169.254.169.254"
	service := &Service{policy: annotatePolicyReadiness(policy)}

	decision, err := service.Route(RouteRequest{Task: "Summarize this short note"})
	if err != nil {
		t.Fatalf("Route returned error: %v", err)
	}
	if decision.SelectedModelID != "" {
		t.Fatalf("selected %q for blocked link-local provider", decision.SelectedModelID)
	}
	foundBlocked := false
	for _, skipped := range decision.Skipped {
		if skipped.ProviderID == "ollama" && skipped.Reason == "provider endpoint uses link-local, metadata, or unspecified address space" {
			foundBlocked = true
			break
		}
	}
	if !foundBlocked {
		t.Fatalf("expected link-local provider skip reason, got %#v", decision.Skipped)
	}
}

func TestRouteBlocksRemoteLlamaCPPProviderEndpoint(t *testing.T) {
	policy := testPolicyWithoutEndpoints()
	llamaIndex := providerIndex(t, policy, "llama-cpp")
	policy.Providers[llamaIndex].EndpointURL = "https://models.example.test"
	service := &Service{policy: annotatePolicyReadiness(policy)}

	decision, err := service.Route(RouteRequest{Task: "Plan a local offline workflow"})
	if err != nil {
		t.Fatalf("Route returned error: %v", err)
	}
	for _, skipped := range decision.Skipped {
		if skipped.ProviderID == "llama-cpp" && skipped.Reason == "llama.cpp endpoint must use localhost, loopback, or host.docker.internal" {
			return
		}
	}
	t.Fatalf("expected llama.cpp local-only boundary, got %#v", decision.Skipped)
}

func TestConfiguredOllamaModelsUseOnlyExplicitAllowlist(t *testing.T) {
	t.Setenv("OLLAMA_MODEL_IDS", "qwen2.5:7b, qwen2.5-coder:7b, qwen2.5:7b, custom/private:latest")
	models := configuredOllamaModels()
	if len(models) != 3 {
		t.Fatalf("models = %#v, want three deduplicated configured entries", models)
	}
	for _, model := range models {
		if !model.Enabled {
			t.Fatalf("configured Ollama model must be enabled: %#v", model)
		}
	}
	if models[0].ID != "qwen2.5:7b" || models[1].ID != "qwen2.5-coder:7b" || models[2].ID != "custom/private:latest" {
		t.Fatalf("configured Ollama model order = %#v", models)
	}
	if models[2].Name != "Configured Ollama local model" {
		t.Fatalf("unknown configured model must remain explicit: %#v", models[2])
	}
}

func TestConfiguredOllamaModelsUseOneSafeDefault(t *testing.T) {
	t.Setenv("OLLAMA_MODEL_IDS", "")
	models := configuredOllamaModels()
	if len(models) != 1 || models[0].ID != "phi3:mini" || !models[0].Enabled {
		t.Fatalf("default Ollama models = %#v", models)
	}
}

func TestLMStudioConfiguredModelIdentityIsSharedWithPolicy(t *testing.T) {
	t.Setenv("LM_STUDIO_MODEL_ID", "qwen3-local")
	policy := annotatePolicyReadiness(defaultPolicy())
	provider := policy.Providers[providerIndex(t, policy, "lm-studio")]
	if len(provider.Models) != 1 || provider.Models[0].ID != "qwen3-local" || !provider.Models[0].Enabled {
		t.Fatalf("LM Studio policy model = %#v", provider.Models)
	}
}

func TestDSparkRequiresExplicitLoopbackConfigurationAndSharesItsModelID(t *testing.T) {
	t.Setenv("DSPARK_ENABLED", "true")
	t.Setenv("DSPARK_BASE_URL", "https://models.example.test")
	t.Setenv("DSPARK_MODEL_ID", "qwen-dspark")
	policy := annotatePolicyReadiness(defaultPolicy())
	provider := policy.Providers[providerIndex(t, policy, "dspark")]
	if provider.Enabled || provider.Configured || provider.ReadinessStatus != "disabled" {
		t.Fatalf("remote DSpark must stay disabled: %#v", provider)
	}

	t.Setenv("DSPARK_BASE_URL", "http://127.0.0.1:9100")
	policy = annotatePolicyReadiness(defaultPolicy())
	provider = policy.Providers[providerIndex(t, policy, "dspark")]
	if !provider.Enabled || !provider.Configured || provider.Models[0].ID != "qwen-dspark" {
		t.Fatalf("loopback DSpark policy = %#v", provider)
	}
}

func TestRouteBlocksRemoteNamedLocalProviderEndpoints(t *testing.T) {
	for _, providerConfig := range []struct {
		providerID  string
		displayName string
	}{
		{providerID: "ollama", displayName: "Ollama"},
		{providerID: "lm-studio", displayName: "LM Studio"},
	} {
		t.Run(providerConfig.providerID, func(t *testing.T) {
			policy := testPolicyWithoutEndpoints()
			providerIndex := providerIndex(t, policy, providerConfig.providerID)
			policy.Providers[providerIndex].EndpointURL = "https://models.example.test"
			service := &Service{policy: annotatePolicyReadiness(policy)}

			decision, err := service.Route(RouteRequest{Task: "Plan a local offline workflow"})
			if err != nil {
				t.Fatalf("Route returned error: %v", err)
			}
			for _, skipped := range decision.Skipped {
				if skipped.ProviderID == providerConfig.providerID && skipped.Reason == providerConfig.displayName+" endpoint must use localhost, loopback, or host.docker.internal" {
					return
				}
			}
			t.Fatalf("expected %s local-only boundary, got %#v", providerConfig.providerID, decision.Skipped)
		})
	}
}

func TestRouteBlocksRemoteLocalAIProviderEndpoint(t *testing.T) {
	policy := testPolicyWithoutEndpoints()
	localAIIndex := providerIndex(t, policy, "localai")
	policy.Providers[localAIIndex].EndpointURL = "https://models.example.test"
	service := &Service{policy: annotatePolicyReadiness(policy)}

	decision, err := service.Route(RouteRequest{Task: "Plan a local offline workflow"})
	if err != nil {
		t.Fatalf("Route returned error: %v", err)
	}
	for _, skipped := range decision.Skipped {
		if skipped.ProviderID == "localai" && skipped.Reason == "LocalAI endpoint must use localhost, loopback, or host.docker.internal" {
			return
		}
	}
	t.Fatalf("expected LocalAI local-only boundary, got %#v", decision.Skipped)
}

func TestRouteBlocksRemoteVLLMProviderEndpoint(t *testing.T) {
	policy := testPolicyWithoutEndpoints()
	vllmIndex := providerIndex(t, policy, "vllm")
	policy.Providers[vllmIndex].EndpointURL = "https://models.example.test"
	service := &Service{policy: annotatePolicyReadiness(policy)}

	decision, err := service.Route(RouteRequest{Task: "Plan a local offline workflow"})
	if err != nil {
		t.Fatalf("Route returned error: %v", err)
	}
	for _, skipped := range decision.Skipped {
		if skipped.ProviderID == "vllm" && skipped.Reason == "vLLM endpoint must use localhost, loopback, or host.docker.internal" {
			return
		}
	}
	t.Fatalf("expected vLLM local-only boundary, got %#v", decision.Skipped)
}

func TestRouteBlocksRemoteSGLangProviderEndpoint(t *testing.T) {
	policy := testPolicyWithoutEndpoints()
	provider := providerIndex(t, policy, "sglang")
	policy.Providers[provider].EndpointURL = "https://models.example.test"
	service := &Service{policy: annotatePolicyReadiness(policy)}

	decision, err := service.Route(RouteRequest{Task: "Plan a local offline workflow"})
	if err != nil {
		t.Fatalf("Route returned error: %v", err)
	}
	for _, skipped := range decision.Skipped {
		if skipped.ProviderID == "sglang" && skipped.Reason == "SGLang endpoint must use localhost, loopback, or host.docker.internal" {
			return
		}
	}
	t.Fatalf("expected SGLang local-only boundary, got %#v", decision.Skipped)
}

func TestRouteBlocksRemoteDSparkProviderEndpoint(t *testing.T) {
	policy := testPolicyWithoutEndpoints()
	provider := providerIndex(t, policy, "dspark")
	policy.Providers[provider].Enabled = true
	policy.Providers[provider].EndpointURL = "https://models.example.test"
	service := &Service{policy: annotatePolicyReadiness(policy)}

	decision, err := service.Route(RouteRequest{Task: "Plan a local offline workflow"})
	if err != nil {
		t.Fatalf("Route returned error: %v", err)
	}
	for _, skipped := range decision.Skipped {
		if skipped.ProviderID == "dspark" && skipped.Reason == "DSpark endpoint must use localhost, loopback, or host.docker.internal" {
			return
		}
	}
	t.Fatalf("expected DSpark local-only boundary, got %#v", decision.Skipped)
}

func TestRouteBlocksRemoteLiteLLMGatewayEndpoint(t *testing.T) {
	policy := testPolicyWithoutEndpoints()
	liteLLMIndex := providerIndex(t, policy, "litellm")
	policy.Providers[liteLLMIndex].Enabled = true
	policy.Providers[liteLLMIndex].EndpointURL = "https://gateway.example.test"
	service := &Service{policy: annotatePolicyReadiness(policy)}

	decision, err := service.Route(RouteRequest{Task: "Draft an operational plan"})
	if err != nil {
		t.Fatalf("Route returned error: %v", err)
	}
	for _, skipped := range decision.Skipped {
		if skipped.ProviderID == "litellm" && skipped.Reason == "LiteLLM gateway endpoint must use localhost, loopback, or host.docker.internal" {
			return
		}
	}
	t.Fatalf("expected LiteLLM local-only boundary, got %#v", decision.Skipped)
}

func TestLocalModelsAllowedPolicyIsEnforced(t *testing.T) {
	policy := testPolicyWithLocalEndpoints()
	policy.LocalModelsAllowed = false
	service := &Service{policy: policy}

	decision, err := service.Route(RouteRequest{Task: "Summarize this short note"})
	if err != nil {
		t.Fatalf("Route returned error: %v", err)
	}

	if decision.SelectedModelID != "" {
		t.Fatalf("selected %q even though local models are disabled and no free cloud provider is enabled", decision.SelectedModelID)
	}
	if len(service.Logs()) != 1 {
		t.Fatalf("expected no-selection decision to be logged")
	}
}

func TestGenerateCallsOllamaEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/generate" {
			t.Fatalf("path = %s, want /api/generate", r.URL.Path)
		}
		var request map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if request["model"] != "phi3:mini" {
			t.Fatalf("model = %v, want phi3:mini", request["model"])
		}
		options, ok := request["options"].(map[string]interface{})
		if !ok || options["num_predict"] != float64(24) {
			t.Fatalf("options = %#v, want bounded num_predict=24", request["options"])
		}
		if options["temperature"] != float64(0) {
			t.Fatalf("options = %#v, want deterministic temperature=0", request["options"])
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"response": "grounded draft"})
	}))
	defer server.Close()

	policy := testPolicyWithoutEndpoints()
	policy.Providers[0].EndpointURL = server.URL
	service := withTrustedTestFinalEffects(t, &Service{policy: policy})

	result, err := service.Generate(withTrustedTestEffect(GenerateRequest{
		Task:      "Summarize this short note",
		MaxTokens: 24,
		RouteDecision: &RouteDecision{
			SelectedProviderID: "ollama",
			SelectedModelID:    "phi3:mini",
			SelectedModelName:  "Phi small local",
			Tier:               TierFree,
		},
	}))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if result.Status != "completed" {
		t.Fatalf("status = %q, want completed", result.Status)
	}
	if result.Output != "grounded draft" {
		t.Fatalf("output = %q, want grounded draft", result.Output)
	}
}

func TestGenerationOutputTokenLimitIsBounded(t *testing.T) {
	t.Setenv("LLM_MAX_OUTPUT_TOKENS", "128")
	for _, test := range []struct {
		requested int
		want      int
	}{{requested: 0, want: 128}, {requested: 16, want: 16}, {requested: 1000, want: 128}} {
		if got := boundedGenerationMaxTokens(test.requested); got != test.want {
			t.Fatalf("boundedGenerationMaxTokens(%d) = %d, want %d", test.requested, got, test.want)
		}
	}
}

func TestGenerateContextCancelsProviderWithoutChargingUsage(t *testing.T) {
	requestStarted := make(chan struct{})
	releaseProvider := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(requestStarted)
		<-releaseProvider
	}))
	defer func() {
		close(releaseProvider)
		server.CloseClientConnections()
		server.Close()
	}()

	policy := testPolicyWithoutEndpoints()
	policy.Providers[0].EndpointURL = server.URL
	history := &fakeGenerationHistoryRepository{}
	service := withTrustedTestFinalEffects(t, &Service{
		policy:            policy,
		usage:             map[string]UsageCounter{},
		generationHistory: history,
	})
	ctx, cancel := context.WithCancel(context.Background())
	resultCh := make(chan *GenerationResult, 1)
	errCh := make(chan error, 1)
	go func() {
		result, err := service.GenerateContext(ctx, withTrustedTestEffect(GenerateRequest{
			Task: "Summarize this short note",
			RouteDecision: &RouteDecision{
				SelectedProviderID: "ollama",
				SelectedModelID:    "phi3:mini",
				SelectedModelName:  "Phi small local",
				Tier:               TierLocal,
			},
		}))
		resultCh <- result
		errCh <- err
	}()

	select {
	case <-requestStarted:
	case <-time.After(time.Second):
		t.Fatal("provider request did not start")
	}
	cancel()

	var result *GenerationResult
	select {
	case result = <-resultCh:
	case <-time.After(time.Second):
		t.Fatal("GenerateContext did not return after caller cancellation")
	}
	if err := <-errCh; err != nil {
		t.Fatalf("GenerateContext: %v", err)
	}
	if result.Status != "stopped" || result.InputTokens != 0 || result.OutputTokens != 0 || result.EstimatedCostEUR != 0 {
		t.Fatalf("canceled generation = %#v, want stopped with zero usage", result)
	}
	if len(service.usage) != 0 {
		t.Fatalf("canceled generation updated usage counters: %#v", service.usage)
	}
	if len(history.records) != 1 || history.records[0].Status != "stopped" || history.records[0].InputTokens != 0 || history.records[0].OutputTokens != 0 {
		t.Fatalf("canceled generation history = %#v", history.records)
	}
}

func TestGenerateStrictLiveProbePolicyBlocksExplicitRouteDecision(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		_ = json.NewEncoder(w).Encode(map[string]string{"response": "must not be called"})
	}))
	defer server.Close()

	policy := testPolicyWithoutEndpoints()
	policy.Providers[0].EndpointURL = server.URL
	policy.RequireRecentLiveProviderProbe = true
	policy.ProviderProbeMaxAgeSeconds = 300
	service := &Service{policy: policy, probeHistory: &fakeProbeHistoryRepository{}}

	result, err := service.Generate(GenerateRequest{
		Task: "Summarize this short note",
		RouteDecision: &RouteDecision{
			SelectedProviderID: "ollama",
			SelectedModelID:    "phi3:mini",
			SelectedModelName:  "Phi small local",
			Tier:               TierLocal,
		},
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if called {
		t.Fatalf("explicit route decision bypassed strict live-probe policy")
	}
	if result.Status != "skipped" || !strings.Contains(result.Reason, "persisted live") {
		t.Fatalf("result = %#v, want strict readiness skip", result)
	}
}

func TestGenerateDoesNotFollowProviderRedirect(t *testing.T) {
	redirectCalled := false
	redirectTarget := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirectCalled = true
	}))
	defer redirectTarget.Close()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, redirectTarget.URL, http.StatusFound)
	}))
	defer server.Close()

	policy := testPolicyWithoutEndpoints()
	policy.Providers[0].EndpointURL = server.URL
	service := &Service{policy: policy}

	result, err := service.Generate(GenerateRequest{
		Task: "Summarize this short note",
		RouteDecision: &RouteDecision{
			SelectedProviderID: "ollama",
			SelectedModelID:    "phi3:mini",
			SelectedModelName:  "Phi small local",
			Tier:               TierFree,
		},
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if redirectCalled {
		t.Fatalf("redirect target was called; provider calls must not follow redirects")
	}
	if result.Status != "failed" {
		t.Fatalf("status = %q, want failed", result.Status)
	}
}

func TestProviderHTTPClientDoesNotUseEnvironmentProxy(t *testing.T) {
	client := noRedirectHTTPClient()
	transport, ok := client.Transport.(*http.Transport)
	if !ok || transport.Proxy != nil {
		t.Fatal("provider HTTP client must not inherit environment proxy settings")
	}
	if err := client.CheckRedirect(nil, nil); err != http.ErrUseLastResponse {
		t.Fatalf("redirect behavior = %v, want %v", err, http.ErrUseLastResponse)
	}
	if client != noRedirectHTTPClient() {
		t.Fatal("provider calls must reuse the bounded HTTP client")
	}
}

func TestProviderHTTPClientRejectsBlockedDNSResolution(t *testing.T) {
	t.Parallel()

	client := newProviderHTTPClient(func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("169.254.169.254")}}, nil
	})
	transport := client.Transport.(*http.Transport)
	_, err := transport.DialContext(context.Background(), "tcp", "models.example:443")
	if err == nil || !strings.Contains(err.Error(), "blocked address space") {
		t.Fatalf("DialContext error = %v, want blocked address rejection", err)
	}
}

func TestProbeProvidersChecksOllamaTags(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			t.Fatalf("path = %s, want /api/tags", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"models": []map[string]string{{"name": "phi3:mini"}},
		})
	}))
	defer server.Close()

	policy := testPolicyWithoutEndpoints()
	policy.Providers[0].EndpointURL = server.URL
	service := &Service{policy: policy}

	results := service.ProbeProviders()
	if results[0].Status != "live" {
		t.Fatalf("status = %q, want live: %s", results[0].Status, results[0].Reason)
	}
	if results[0].ModelsSeen != 1 {
		t.Fatalf("models seen = %d, want 1", results[0].ModelsSeen)
	}
}

func TestRouteIsReadOnlyAndGenerateAuthorizesDueOllamaRefresh(t *testing.T) {
	t.Setenv("LLM_MODEL_MAINTENANCE_ENABLED", "true")
	digest := "sha256:old"
	pulls := 0
	generations := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"models": []map[string]string{{"name": "phi3:mini", "digest": digest}}})
		case "/api/pull":
			var request map[string]interface{}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatalf("decode pull request: %v", err)
			}
			if request["name"] != "phi3:mini" || request["stream"] != false {
				t.Fatalf("pull request = %#v", request)
			}
			pulls++
			digest = "sha256:new"
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "success"})
		case "/api/generate":
			generations++
			_ = json.NewEncoder(w).Encode(map[string]string{"response": "authorized draft"})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	policy := testPolicyWithoutEndpoints()
	policy.Providers[0].EndpointURL = server.URL
	policy.Providers[0].Models = []Model{{ID: "phi3:mini", Name: "Phi", Tier: TierLocal, Capabilities: []string{"general", "extraction"}, MaxDifficulty: 5, MaxReasoning: "very_high", Enabled: true}}
	service := withTrustedTestFinalEffects(t, &Service{policy: policy, maintenanceHistory: &fakeModelMaintenanceRepository{}, maintenanceRunning: map[string]*sync.Mutex{}})

	decision, err := service.Route(RouteRequest{Task: "classify this"})
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	if decision.SelectedModelID != "phi3:mini" {
		t.Fatalf("selected model = %q", decision.SelectedModelID)
	}
	if pulls != 0 {
		t.Fatalf("routing performed a model pull before final-effect authorization: %d", pulls)
	}
	history, err := service.ModelMaintenanceHistory(10)
	if err != nil || len(history) != 0 {
		t.Fatalf("routing performed maintenance checks: history=%#v err=%v", history, err)
	}

	result, err := service.Generate(withTrustedTestEffect(GenerateRequest{
		Task:          "classify this",
		RouteDecision: &decision,
	}))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if result.Status != "completed" || pulls != 1 || generations != 1 {
		t.Fatalf("authorized generation=%#v pulls=%d generations=%d", result, pulls, generations)
	}
	history, err = service.ModelMaintenanceHistory(10)
	if err != nil || len(history) != 1 {
		t.Fatalf("history = %#v, err = %v", history, err)
	}
	if history[0].Status != "updated" || !history[0].UpdateApplied || history[0].CurrentDigest != "sha256:new" {
		t.Fatalf("maintenance = %#v", history[0])
	}

	_, err = service.Route(RouteRequest{Task: "classify this again"})
	if err != nil {
		t.Fatalf("second Route: %v", err)
	}
	if pulls != 1 {
		t.Fatalf("read-only route changed maintenance state; pulls = %d", pulls)
	}
}

func TestMaintenanceAuthorizationDoesNotReuseEvidenceAfterProviderEndpointChanges(t *testing.T) {
	t.Setenv("LLM_MODEL_MAINTENANCE_ENABLED", "true")
	firstPulls := 0
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"models": []map[string]string{{"name": "phi3:mini", "digest": "sha256:first"}}})
		case "/api/pull":
			firstPulls++
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "success"})
		default:
			t.Fatalf("unexpected first-runtime path: %s", r.URL.Path)
		}
	}))
	defer first.Close()

	secondPulls := 0
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"models": []map[string]string{{"name": "phi3:mini", "digest": "sha256:second"}}})
		case "/api/pull":
			secondPulls++
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "success"})
		default:
			t.Fatalf("unexpected second-runtime path: %s", r.URL.Path)
		}
	}))
	defer second.Close()

	policy := testPolicyWithoutEndpoints()
	policy.Providers[0].EndpointURL = first.URL
	policy.Providers[0].Models = []Model{{ID: "phi3:mini", Name: "Phi", Tier: TierLocal, Capabilities: []string{"general", "extraction"}, MaxDifficulty: 5, MaxReasoning: "very_high", Enabled: true}}
	history := &fakeModelMaintenanceRepository{}
	service := withTrustedTestFinalEffects(t, &Service{policy: policy, maintenanceHistory: history, maintenanceRunning: map[string]*sync.Mutex{}})

	if _, err := service.Route(RouteRequest{Task: "classify this"}); err != nil {
		t.Fatalf("first Route: %v", err)
	}
	if firstPulls != 0 || secondPulls != 0 {
		t.Fatalf("routing performed maintenance effects: first:%d second:%d", firstPulls, secondPulls)
	}
	firstRun := service.RunDueModelMaintenance()
	if firstRun.Failed != 0 {
		t.Fatalf("first authorized maintenance run = %#v", firstRun)
	}
	if firstPulls != 1 || secondPulls != 0 {
		t.Fatalf("first daily check pulls = first:%d second:%d", firstPulls, secondPulls)
	}

	service.policy.Providers[0].EndpointURL = second.URL
	if _, err := service.Route(RouteRequest{Task: "classify this after operator endpoint change"}); err != nil {
		t.Fatalf("second Route: %v", err)
	}
	if secondPulls != 0 {
		t.Fatalf("changed endpoint triggered a pull during routing: %d", secondPulls)
	}
	secondRun := service.RunDueModelMaintenance()
	if secondRun.Failed != 0 {
		t.Fatalf("second authorized maintenance run = %#v", secondRun)
	}
	if secondPulls != 1 {
		t.Fatalf("changed endpoint reused old maintenance evidence; second pulls = %d", secondPulls)
	}
	maintenance, err := service.ModelMaintenanceHistory(10)
	if err != nil || len(maintenance) != 2 {
		t.Fatalf("maintenance history = %#v, err=%v", maintenance, err)
	}
	if !maintenance[0].ConfigurationChanged {
		t.Fatalf("new endpoint maintenance result must disclose configuration change: %#v", maintenance[0])
	}
}

func TestGenerateAuthorizesMissingConfiguredOllamaModelInstallation(t *testing.T) {
	t.Setenv("LLM_MODEL_MAINTENANCE_ENABLED", "true")
	installed := false
	pulls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			models := []map[string]string{}
			if installed {
				models = append(models, map[string]string{"name": "phi3:mini", "digest": "sha256:installed"})
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"models": models})
		case "/api/pull":
			var request map[string]interface{}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatalf("decode pull request: %v", err)
			}
			if request["name"] != "phi3:mini" || request["stream"] != false {
				t.Fatalf("pull request = %#v", request)
			}
			pulls++
			installed = true
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "success"})
		case "/api/generate":
			_ = json.NewEncoder(w).Encode(map[string]string{"response": "installed model draft"})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	policy := testPolicyWithoutEndpoints()
	policy.Providers[0].EndpointURL = server.URL
	policy.Providers[0].Models = []Model{{ID: "phi3:mini", Name: "Phi", Tier: TierLocal, Capabilities: []string{"general", "extraction"}, MaxDifficulty: 5, MaxReasoning: "very_high", Enabled: true}}
	service := withTrustedTestFinalEffects(t, &Service{policy: policy, maintenanceHistory: &fakeModelMaintenanceRepository{}, maintenanceRunning: map[string]*sync.Mutex{}})

	decision, err := service.Route(RouteRequest{Task: "classify this"})
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	if decision.SelectedModelID != "phi3:mini" || pulls != 0 {
		t.Fatalf("decision=%#v pulls=%d", decision, pulls)
	}
	result, err := service.Generate(withTrustedTestEffect(GenerateRequest{
		Task:          "classify this",
		RouteDecision: &decision,
	}))
	if err != nil || result.Status != "completed" || pulls != 1 {
		t.Fatalf("authorized generation=%#v pulls=%d err=%v", result, pulls, err)
	}
	history, err := service.ModelMaintenanceHistory(10)
	if err != nil || len(history) != 1 {
		t.Fatalf("history=%#v err=%v", history, err)
	}
	if history[0].Status != "installed" || !history[0].UpdateApplied || history[0].CurrentDigest != "sha256:installed" {
		t.Fatalf("maintenance=%#v", history[0])
	}
}

func TestGenerateBlocksOllamaModelWhenAuthorizedRefreshCannotVerifyDigest(t *testing.T) {
	t.Setenv("LLM_MODEL_MAINTENANCE_ENABLED", "true")
	pulls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			// An incomplete tags response must never be treated as evidence that
			// the configured tag is current after the pull completes.
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"models": []map[string]string{{"name": "phi3:mini"}}})
		case "/api/pull":
			pulls++
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "success"})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	policy := testPolicyWithoutEndpoints()
	policy.Providers[0].EndpointURL = server.URL
	policy.Providers[0].Models = []Model{{ID: "phi3:mini", Name: "Phi", Tier: TierLocal, Capabilities: []string{"general", "extraction"}, MaxDifficulty: 5, MaxReasoning: "very_high", Enabled: true}}
	service := withTrustedTestFinalEffects(t, &Service{policy: policy, maintenanceHistory: &fakeModelMaintenanceRepository{}, maintenanceRunning: map[string]*sync.Mutex{}})

	decision, err := service.Route(RouteRequest{Task: "classify this"})
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	if decision.SelectedModelID != "phi3:mini" || pulls != 0 {
		t.Fatalf("decision=%#v pulls=%d", decision, pulls)
	}
	result, err := service.Generate(withTrustedTestEffect(GenerateRequest{
		Task:          "classify this",
		RouteDecision: &decision,
	}))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if result.Status != "skipped" || pulls != 1 || !strings.Contains(result.Reason, "no verifiable digest") {
		t.Fatalf("result=%#v pulls=%d", result, pulls)
	}
	history, err := service.ModelMaintenanceHistory(10)
	if err != nil || len(history) != 1 {
		t.Fatalf("history=%#v err=%v", history, err)
	}
	if history[0].Status != "failed" || !history[0].BlocksExecution || !strings.Contains(history[0].Reason, "no verifiable digest") {
		t.Fatalf("maintenance=%#v", history[0])
	}
}

func TestRouteDoesNotRunFailingMaintenanceOrPrematurelyFallback(t *testing.T) {
	t.Setenv("LLM_MODEL_MAINTENANCE_ENABLED", "true")
	maintenanceRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		maintenanceRequests++
		if r.URL.Path == "/api/tags" {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"models": []map[string]string{{"name": "phi3:mini", "digest": "sha256:old"}}})
			return
		}
		if r.URL.Path == "/api/pull" {
			http.Error(w, "registry unavailable", http.StatusBadGateway)
			return
		}
		t.Fatalf("unexpected path: %s", r.URL.Path)
	}))
	defer server.Close()
	freeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		maintenanceRequests++
		if r.URL.Path != "/v1/models" {
			t.Fatalf("free cloud maintenance path = %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"data": []map[string]string{
			{"id": "free-best-available"},
			{"id": "free-fast-classifier"},
			{"id": "free-coder"},
		}})
	}))
	defer freeServer.Close()

	policy := testPolicyWithoutEndpoints()
	policy.Providers[0].EndpointURL = server.URL
	policy.Providers[0].Models = []Model{{ID: "phi3:mini", Name: "Phi", Tier: TierLocal, Capabilities: []string{"general", "extraction"}, MaxDifficulty: 5, MaxReasoning: "very_high", Enabled: true}}
	t.Setenv("FREE_CLOUD_API_KEY", "test-free-key")
	providerIndex := providerIndex(t, policy, "free-cloud")
	policy.Providers[providerIndex].Enabled = true
	policy.Providers[providerIndex].EndpointURL = freeServer.URL
	policy.Providers[providerIndex].QuotaRemaining = 10
	service := &Service{policy: policy, maintenanceHistory: &fakeModelMaintenanceRepository{}, maintenanceRunning: map[string]*sync.Mutex{}}

	decision, err := service.Route(RouteRequest{Task: "classify this"})
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	if decision.SelectedProviderID != "ollama" {
		t.Fatalf("provider = %q, want read-only local selection; skipped=%#v", decision.SelectedProviderID, decision.Skipped)
	}
	if maintenanceRequests != 0 {
		t.Fatalf("Route performed %d maintenance network requests", maintenanceRequests)
	}
}

func TestGenerateReroutesWhenSuppliedDecisionBecomesBlockedByMaintenance(t *testing.T) {
	t.Setenv("LLM_MODEL_MAINTENANCE_ENABLED", "true")
	t.Setenv("FREE_CLOUD_API_KEY", "test-free-key")
	ollama := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"models": []map[string]string{{"name": "phi3:mini", "digest": "sha256:old"}}})
		case "/api/pull":
			http.Error(w, "registry unavailable", http.StatusBadGateway)
		default:
			t.Fatalf("unexpected Ollama path: %s", r.URL.Path)
		}
	}))
	defer ollama.Close()
	free := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"data": []map[string]string{{"id": "free-best-available"}}})
		case "/v1/chat/completions":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"choices": []map[string]interface{}{{"message": map[string]string{"content": "safe fallback draft"}}},
				"usage":   map[string]int{"prompt_tokens": 12, "completion_tokens": 4},
			})
		default:
			t.Fatalf("unexpected free provider path: %s", r.URL.Path)
		}
	}))
	defer free.Close()

	policy := testPolicyWithoutEndpoints()
	policy.Providers[0].EndpointURL = ollama.URL
	policy.Providers[0].Models = []Model{{ID: "phi3:mini", Name: "Phi", Tier: TierLocal, Capabilities: []string{"general"}, MaxDifficulty: 5, MaxReasoning: "very_high", Enabled: true}}
	freeIndex := providerIndex(t, policy, "free-cloud")
	policy.Providers[freeIndex].Enabled = true
	policy.Providers[freeIndex].EndpointURL = free.URL
	policy.Providers[freeIndex].QuotaRemaining = 10
	policy.Providers[freeIndex].Models = []Model{{ID: "free-best-available", Name: "Free fallback", Tier: TierFree, Capabilities: []string{"general"}, MaxDifficulty: 5, MaxReasoning: "very_high", Enabled: true}}
	service := withTrustedTestFinalEffects(t, &Service{policy: policy, maintenanceHistory: &fakeModelMaintenanceRepository{}, maintenanceRunning: map[string]*sync.Mutex{}})

	result, err := service.Generate(withTrustedTestEffect(GenerateRequest{
		Task:          "draft a short update",
		RouteDecision: &RouteDecision{SelectedProviderID: "ollama", SelectedModelID: "phi3:mini", SelectedModelName: "Phi", Tier: TierLocal},
	}))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if result.Status != "completed" || result.ProviderID != "free-cloud" || result.ModelID != "free-best-available" {
		t.Fatalf("result = %#v", result)
	}
	if result.Output != "safe fallback draft" || result.InputTokens != 12 || result.OutputTokens != 4 {
		t.Fatalf("fallback result = %#v", result)
	}
}

func TestRouteIsReadOnlyAndMaintenanceVerifiesExactFreeCloudModel(t *testing.T) {
	t.Setenv("LLM_MODEL_MAINTENANCE_ENABLED", "true")
	probes := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Fatalf("maintenance path = %s", r.URL.Path)
		}
		probes++
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"data": []map[string]string{{"id": "free-verified"}}})
	}))
	defer server.Close()

	policy := Policy{
		FreeCloudQuotaAllowed: true,
		TierOrder:             []string{TierFree},
		Providers: []Provider{{
			ID: "free-cloud", Name: "Free cloud", Enabled: true, EndpointURL: server.URL, QuotaRemaining: 5,
			Models: []Model{{ID: "free-verified", Name: "Verified free model", Tier: TierFree, Capabilities: []string{"general"}, MaxDifficulty: 5, MaxReasoning: "very_high", Enabled: true}},
		}},
	}
	service := withTrustedTestFinalEffects(t, &Service{policy: policy, maintenanceHistory: &fakeModelMaintenanceRepository{}, maintenanceRunning: map[string]*sync.Mutex{}})

	decision, err := service.Route(RouteRequest{Task: "plan this"})
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	if decision.SelectedModelID != "free-verified" {
		t.Fatalf("selected model = %q, skipped=%#v", decision.SelectedModelID, decision.Skipped)
	}
	history, err := service.ModelMaintenanceHistory(10)
	if err != nil || len(history) != 0 || probes != 0 {
		t.Fatalf("Route performed provider verification: history=%#v probes=%d err=%v", history, probes, err)
	}
	run := service.RunDueModelMaintenance()
	if run.Failed != 0 || run.Checked != 1 {
		t.Fatalf("maintenance run=%#v", run)
	}
	history, err = service.ModelMaintenanceHistory(10)
	if err != nil || len(history) != 1 || history[0].Status != "provider_managed" || history[0].BlocksExecution {
		t.Fatalf("maintenance history = %#v, err=%v", history, err)
	}
	if probes != 1 {
		t.Fatalf("probes = %d, want 1", probes)
	}

	_, err = service.Route(RouteRequest{Task: "plan this again"})
	if err != nil {
		t.Fatalf("second Route: %v", err)
	}
	if probes != 1 {
		t.Fatalf("daily cloud record was not reused; probes=%d", probes)
	}
	secondRun := service.RunDueModelMaintenance()
	if secondRun.Reused != 1 || probes != 1 {
		t.Fatalf("second maintenance run=%#v probes=%d", secondRun, probes)
	}
}

func TestMaintenanceFailureMakesLaterRoutesSkipUnverifiedIdentifier(t *testing.T) {
	t.Setenv("LLM_MODEL_MAINTENANCE_ENABLED", "true")
	probes := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		probes++
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"data": []map[string]string{{"id": "other-model"}}})
	}))
	defer server.Close()

	policy := Policy{
		FreeCloudQuotaAllowed: true,
		TierOrder:             []string{TierFree},
		Providers: []Provider{{
			ID: "free-cloud", Name: "Free cloud", Enabled: true, EndpointURL: server.URL, QuotaRemaining: 5,
			Models: []Model{{ID: "configured-model", Name: "Configured free model", Tier: TierFree, Capabilities: []string{"general"}, MaxDifficulty: 5, MaxReasoning: "very_high", Enabled: true}},
		}},
	}
	service := withTrustedTestFinalEffects(t, &Service{policy: policy, maintenanceHistory: &fakeModelMaintenanceRepository{}, maintenanceRunning: map[string]*sync.Mutex{}})

	decision, err := service.Route(RouteRequest{Task: "plan this"})
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	if decision.SelectedModelID != "configured-model" || probes != 0 {
		t.Fatalf("decision = %#v", decision)
	}
	run := service.RunDueModelMaintenance()
	if run.Failed != 1 || probes != 1 {
		t.Fatalf("maintenance run=%#v probes=%d", run, probes)
	}
	decision, err = service.Route(RouteRequest{Task: "plan this after failed verification"})
	if err != nil {
		t.Fatalf("second Route: %v", err)
	}
	if decision.SelectedModelID != "" || len(decision.Skipped) == 0 {
		t.Fatalf("decision after failed verification = %#v", decision)
	}
	history, err := service.ModelMaintenanceHistory(10)
	if err != nil || len(history) != 1 || history[0].Status != "failed" || !history[0].BlocksExecution {
		t.Fatalf("maintenance history = %#v, err=%v", history, err)
	}
}

func TestRunDueModelMaintenanceRefreshesEveryEnabledConfiguredLocalModel(t *testing.T) {
	t.Setenv("LLM_MODEL_MAINTENANCE_ENABLED", "true")
	pulls := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"models": []map[string]string{
				{"name": "phi3:mini", "digest": "sha256:phi"},
				{"name": "qwen2.5:7b", "digest": "sha256:qwen"},
			}})
		case "/api/pull":
			var request map[string]interface{}
			_ = json.NewDecoder(r.Body).Decode(&request)
			name, _ := request["name"].(string)
			pulls[name]++
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "success"})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	policy := testPolicyWithoutEndpoints()
	policy.Providers[0].EndpointURL = server.URL
	policy.Providers[0].Models = []Model{
		{ID: "phi3:mini", Name: "Phi", Tier: TierLocal, Enabled: true},
		{ID: "qwen2.5:7b", Name: "Qwen", Tier: TierLocal, Enabled: true},
		{ID: "disabled:local", Name: "Disabled", Tier: TierLocal, Enabled: false},
	}
	service := withTrustedTestFinalEffects(t, &Service{policy: policy, maintenanceHistory: &fakeModelMaintenanceRepository{}, maintenanceRunning: map[string]*sync.Mutex{}})

	run := service.RunDueModelMaintenance()
	if run.Eligible != 2 || run.Checked != 2 || run.Reused != 0 || run.Failed != 0 || len(run.Results) != 2 {
		t.Fatalf("run = %#v", run)
	}
	if pulls["phi3:mini"] != 1 || pulls["qwen2.5:7b"] != 1 || pulls["disabled:local"] != 0 {
		t.Fatalf("pulls = %#v", pulls)
	}
	second := service.RunDueModelMaintenance()
	if second.Checked != 0 || second.Reused != 2 || pulls["phi3:mini"] != 1 || pulls["qwen2.5:7b"] != 1 {
		t.Fatalf("daily cache was not reused: run=%#v pulls=%#v", second, pulls)
	}
}

func TestProbeProvidersDoesNotFollowRedirects(t *testing.T) {
	redirectCalled := false
	redirectTarget := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirectCalled = true
	}))
	defer redirectTarget.Close()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, redirectTarget.URL, http.StatusFound)
	}))
	defer server.Close()

	policy := testPolicyWithoutEndpoints()
	policy.Providers[0].EndpointURL = server.URL
	service := &Service{policy: policy}

	results := service.ProbeProviders()
	if redirectCalled {
		t.Fatalf("probe followed redirect target")
	}
	if results[0].Status != "failed" || !results[0].RequiresReview {
		t.Fatalf("probe status/review = %q/%v, want failed/review", results[0].Status, results[0].RequiresReview)
	}
}

func TestProbeAndRecordProvidersPersistsRedactedLastSuccess(t *testing.T) {
	healthy := true
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if healthy {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"models": []map[string]string{{"name": "phi3:mini"}},
			})
			return
		}
		http.Error(w, "token=super-secret-provider-token", http.StatusBadGateway)
	}))
	defer server.Close()

	repository := &fakeProbeHistoryRepository{}
	service := &Service{
		policy:       Policy{Providers: []Provider{{ID: "ollama", Name: "Ollama", Enabled: true, Local: true, EndpointURL: server.URL}}},
		probeHistory: repository,
	}

	first, err := service.ProbeAndRecordProviders()
	if err != nil {
		t.Fatalf("ProbeAndRecordProviders first run: %v", err)
	}
	if len(first) != 1 || !first[0].Live || first[0].LastSuccessfulAt == nil {
		t.Fatalf("first probe = %#v, want live persisted result with last success", first)
	}
	firstSuccess := *first[0].LastSuccessfulAt

	healthy = false
	second, err := service.ProbeAndRecordProviders()
	if err != nil {
		t.Fatalf("ProbeAndRecordProviders failed run: %v", err)
	}
	if len(second) != 1 || second[0].Live || second[0].LastSuccessfulAt == nil {
		t.Fatalf("second probe = %#v, want failed result retaining last success", second)
	}
	if !second[0].LastSuccessfulAt.Equal(firstSuccess) {
		t.Fatalf("last success = %s, want %s", second[0].LastSuccessfulAt, firstSuccess)
	}
	if strings.Contains(second[0].Reason, "super-secret-provider-token") {
		t.Fatalf("probe result leaked secret: %s", second[0].Reason)
	}
	if strings.Contains(repository.probes[1].Reason, "super-secret-provider-token") {
		t.Fatalf("persisted probe leaked secret: %#v", repository.probes[1])
	}

	history, err := service.ProviderProbeHistory(10)
	if err != nil {
		t.Fatalf("ProviderProbeHistory: %v", err)
	}
	if len(history) != 2 || history[0].Status != "failed" || history[1].Status != "live" {
		t.Fatalf("probe history = %#v, want newest failed then live", history)
	}
}

func TestRouteStrictLiveProbePolicyRequiresRecentLiveEvidence(t *testing.T) {
	policy := testPolicyWithoutEndpoints()
	policy.Providers[0].EndpointURL = "http://localhost:11434"
	policy.RequireRecentLiveProviderProbe = true
	policy.ProviderProbeMaxAgeSeconds = 300
	repository := &fakeProbeHistoryRepository{}
	service := &Service{policy: policy, probeHistory: repository}

	decision, err := service.Route(RouteRequest{Task: "Summarize this short note"})
	if err != nil {
		t.Fatalf("Route without probe: %v", err)
	}
	if decision.SelectedModelID != "" {
		t.Fatalf("selected %q without a persisted live probe", decision.SelectedModelID)
	}
	if !skippedReasonContains(decision.Skipped, "ollama", "has not passed a persisted live") {
		t.Fatalf("missing strict no-probe skip reason: %#v", decision.Skipped)
	}

	now := time.Now().UTC()
	repository.probes = append(repository.probes, models.LLMProviderProbe{ProviderID: "ollama", Live: true, CheckedAt: now})
	decision, err = service.Route(RouteRequest{Task: "Summarize this short note"})
	if err != nil {
		t.Fatalf("Route with live probe: %v", err)
	}
	if decision.SelectedModelID == "" {
		t.Fatalf("expected a route after a recent live probe: %#v", decision.Skipped)
	}

	repository.probes = append(repository.probes, models.LLMProviderProbe{ProviderID: "ollama", Live: false, CheckedAt: now.Add(time.Second)})
	decision, err = service.Route(RouteRequest{Task: "Summarize this short note"})
	if err != nil {
		t.Fatalf("Route after failed probe: %v", err)
	}
	if decision.SelectedModelID != "" || !skippedReasonContains(decision.Skipped, "ollama", "latest readiness check is not live") {
		t.Fatalf("failed latest probe must block route: %#v", decision)
	}

	repository.probes = []models.LLMProviderProbe{{ProviderID: "ollama", Live: true, CheckedAt: now.Add(-6 * time.Minute)}}
	decision, err = service.Route(RouteRequest{Task: "Summarize this short note"})
	if err != nil {
		t.Fatalf("Route after stale probe: %v", err)
	}
	if decision.SelectedModelID != "" || !skippedReasonContains(decision.Skipped, "ollama", "readiness check is stale") {
		t.Fatalf("stale live probe must block route: %#v", decision)
	}
}

func skippedReasonContains(skipped []SkippedModel, providerID, text string) bool {
	for _, item := range skipped {
		if item.ProviderID == providerID && strings.Contains(item.Reason, text) {
			return true
		}
	}
	return false
}

func TestGenerateRedactsProviderErrorBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "token=super-secret-token", http.StatusBadGateway)
	}))
	defer server.Close()

	policy := testPolicyWithoutEndpoints()
	policy.Providers[0].EndpointURL = server.URL
	service := &Service{policy: policy}

	result, err := service.Generate(GenerateRequest{
		Task: "Summarize this short note",
		RouteDecision: &RouteDecision{
			SelectedProviderID: "ollama",
			SelectedModelID:    "phi3:mini",
			SelectedModelName:  "Phi small local",
			Tier:               TierFree,
		},
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if strings.Contains(result.Reason, "super-secret-token") {
		t.Fatalf("provider error leaked secret: %s", result.Reason)
	}
}

func TestGenerateBlocksWhenEmergencyStopActive(t *testing.T) {
	t.Setenv("HAI_EMERGENCY_STOP", "true")
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		_ = json.NewEncoder(w).Encode(map[string]string{"response": "should not run"})
	}))
	defer server.Close()

	policy := testPolicyWithoutEndpoints()
	policy.Providers[0].EndpointURL = server.URL
	service := &Service{policy: policy}

	result, err := service.Generate(GenerateRequest{
		Task: "Summarize this short note",
		RouteDecision: &RouteDecision{
			SelectedProviderID: "ollama",
			SelectedModelID:    "phi3:mini",
			SelectedModelName:  "Phi small local",
			Tier:               TierFree,
		},
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if called {
		t.Fatalf("provider endpoint was called while emergency stop was active")
	}
	if result.Status != "blocked" {
		t.Fatalf("status = %q, want blocked", result.Status)
	}
}

func testPolicyWithLocalEndpoints() Policy {
	policy := testPolicyWithoutEndpoints()
	policy.Providers[0].EndpointURL = "http://localhost:11434"
	policy.Providers[1].EndpointURL = "http://localhost:1234"
	return annotatePolicyReadiness(policy)
}

func testPolicyWithoutEndpoints() Policy {
	policy := defaultPolicy()
	for index := range policy.Providers {
		policy.Providers[index].EndpointURL = ""
	}
	return annotatePolicyReadiness(policy)
}

func withTestPaidProvider(policy Policy, endpoint string) Policy {
	policy.Providers = append(policy.Providers, Provider{
		ID:          "test-paid-provider",
		Name:        "Test paid provider",
		Enabled:     true,
		Paid:        true,
		EndpointURL: endpoint,
		Models: []Model{{
			ID:               "test-paid-high-capability",
			Name:             "Test paid high capability model",
			Tier:             TierExpensive,
			Capabilities:     []string{"general", "coding", "planning", "verification", "extraction"},
			MaxDifficulty:    5,
			MaxReasoning:     "very_high",
			EstimatedCostEUR: 0.05,
			RequiresApproval: true,
			Enabled:          true,
		}},
	})
	return annotatePolicyReadiness(policy)
}

func providerIndex(t *testing.T, policy Policy, providerID string) int {
	t.Helper()
	for index, provider := range policy.Providers {
		if provider.ID == providerID {
			return index
		}
	}
	t.Fatalf("provider %q not found in policy", providerID)
	return -1
}

func TestGenerateCallsOpenAICompatibleEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("path = %s, want /v1/chat/completions", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []map[string]interface{}{
				{"message": map[string]string{"content": "local chat draft"}},
			},
		})
	}))
	defer server.Close()

	policy := testPolicyWithoutEndpoints()
	policy.Providers[1].EndpointURL = server.URL
	service := withTrustedTestFinalEffects(t, &Service{policy: policy})

	result, err := service.Generate(withTrustedTestEffect(GenerateRequest{
		Task: "Plan the work",
		RouteDecision: &RouteDecision{
			SelectedProviderID: "lm-studio",
			SelectedModelID:    "local-model",
			SelectedModelName:  "Configured LM Studio local model",
			Tier:               TierFree,
		},
	}))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if result.Status != "completed" {
		t.Fatalf("status = %q, want completed", result.Status)
	}
	if result.Output != "local chat draft" {
		t.Fatalf("output = %q, want local chat draft", result.Output)
	}
}

func TestLlamaCPPProviderProbesAndGeneratesThroughOpenAICompatibleAPI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"data": []map[string]string{{"id": "qwen3-gguf"}}})
		case "/v1/chat/completions":
			var request map[string]interface{}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatalf("decode request: %v", err)
			}
			if request["model"] != "qwen3-gguf" {
				t.Fatalf("model = %v, want qwen3-gguf", request["model"])
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"choices": []map[string]interface{}{{"message": map[string]string{"content": "local llama.cpp draft"}}}})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	policy := testPolicyWithoutEndpoints()
	llamaIndex := providerIndex(t, policy, "llama-cpp")
	policy.Providers[llamaIndex].EndpointURL = server.URL
	policy.Providers[llamaIndex].Models[0].ID = "qwen3-gguf"
	service := withTrustedTestFinalEffects(t, &Service{policy: policy})

	var probe ProviderProbeResult
	for _, result := range service.ProbeProviders() {
		if result.ProviderID == "llama-cpp" {
			probe = result
			break
		}
	}
	if probe.Status != "live" || probe.ModelsSeen != 1 {
		t.Fatalf("probe = %#v, want live llama.cpp provider with one model", probe)
	}

	result, err := service.Generate(withTrustedTestEffect(GenerateRequest{
		Task: "Draft a local answer",
		RouteDecision: &RouteDecision{
			SelectedProviderID: "llama-cpp",
			SelectedModelID:    "qwen3-gguf",
			SelectedModelName:  "Configured llama.cpp GGUF model",
			Tier:               TierLocal,
		},
	}))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if result.Status != "completed" || result.Output != "local llama.cpp draft" {
		t.Fatalf("generation result = %#v", result)
	}
}

func TestLocalAIProviderProbesAndGeneratesThroughOpenAICompatibleAPI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"data": []map[string]string{{"id": "qwen-localai"}}})
		case "/v1/chat/completions":
			var request map[string]interface{}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatalf("decode request: %v", err)
			}
			if request["model"] != "qwen-localai" {
				t.Fatalf("model = %v, want qwen-localai", request["model"])
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"choices": []map[string]interface{}{{"message": map[string]string{"content": "local LocalAI draft"}}}})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	policy := testPolicyWithoutEndpoints()
	localAIIndex := providerIndex(t, policy, "localai")
	policy.Providers[localAIIndex].EndpointURL = server.URL
	policy.Providers[localAIIndex].Models[0].ID = "qwen-localai"
	service := withTrustedTestFinalEffects(t, &Service{policy: policy})

	var probe ProviderProbeResult
	for _, result := range service.ProbeProviders() {
		if result.ProviderID == "localai" {
			probe = result
			break
		}
	}
	if probe.Status != "live" || probe.ModelsSeen != 1 {
		t.Fatalf("probe = %#v, want live LocalAI provider with one model", probe)
	}

	result, err := service.Generate(withTrustedTestEffect(GenerateRequest{
		Task: "Draft a local answer",
		RouteDecision: &RouteDecision{
			SelectedProviderID: "localai",
			SelectedModelID:    "qwen-localai",
			SelectedModelName:  "Configured LocalAI local model",
			Tier:               TierLocal,
		},
	}))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if result.Status != "completed" || result.Output != "local LocalAI draft" {
		t.Fatalf("generation result = %#v", result)
	}
}

func TestVLLMProviderProbesAndGeneratesThroughOpenAICompatibleAPI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"data": []map[string]string{{"id": "qwen-vllm"}}})
		case "/v1/chat/completions":
			var request map[string]interface{}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatalf("decode request: %v", err)
			}
			if request["model"] != "qwen-vllm" {
				t.Fatalf("model = %v, want qwen-vllm", request["model"])
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"choices": []map[string]interface{}{{"message": map[string]string{"content": "local vLLM draft"}}}})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	policy := testPolicyWithoutEndpoints()
	vllmIndex := providerIndex(t, policy, "vllm")
	policy.Providers[vllmIndex].EndpointURL = server.URL
	policy.Providers[vllmIndex].Models[0].ID = "qwen-vllm"
	service := withTrustedTestFinalEffects(t, &Service{policy: policy})

	var probe ProviderProbeResult
	for _, result := range service.ProbeProviders() {
		if result.ProviderID == "vllm" {
			probe = result
			break
		}
	}
	if probe.Status != "live" || probe.ModelsSeen != 1 {
		t.Fatalf("probe = %#v, want live vLLM provider with one model", probe)
	}

	result, err := service.Generate(withTrustedTestEffect(GenerateRequest{
		Task: "Draft a local answer",
		RouteDecision: &RouteDecision{
			SelectedProviderID: "vllm",
			SelectedModelID:    "qwen-vllm",
			SelectedModelName:  "Configured vLLM local model",
			Tier:               TierLocal,
		},
	}))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if result.Status != "completed" || result.Output != "local vLLM draft" {
		t.Fatalf("generation result = %#v", result)
	}
}

func TestSGLangProviderProbesAndGeneratesThroughOpenAICompatibleAPI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"data": []map[string]string{{"id": "qwen-sglang"}}})
		case "/v1/chat/completions":
			var request map[string]interface{}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatalf("decode request: %v", err)
			}
			if request["model"] != "qwen-sglang" {
				t.Fatalf("model = %v, want qwen-sglang", request["model"])
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"choices": []map[string]interface{}{{"message": map[string]string{"content": "local SGLang draft"}}}})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	policy := testPolicyWithoutEndpoints()
	provider := providerIndex(t, policy, "sglang")
	policy.Providers[provider].EndpointURL = server.URL
	policy.Providers[provider].Models[0].ID = "qwen-sglang"
	service := withTrustedTestFinalEffects(t, &Service{policy: policy, maintenanceHistory: &fakeModelMaintenanceRepository{}, maintenanceRunning: map[string]*sync.Mutex{}})

	var probe ProviderProbeResult
	for _, result := range service.ProbeProviders() {
		if result.ProviderID == "sglang" {
			probe = result
			break
		}
	}
	if probe.Status != "live" || probe.ModelsSeen != 1 {
		t.Fatalf("probe = %#v, want live SGLang provider with one model", probe)
	}

	result, err := service.Generate(withTrustedTestEffect(GenerateRequest{
		Task: "Draft a local answer",
		RouteDecision: &RouteDecision{
			SelectedProviderID: "sglang",
			SelectedModelID:    "qwen-sglang",
			SelectedModelName:  "Configured SGLang local model",
			Tier:               TierLocal,
		},
	}))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if result.Status != "completed" || result.Output != "local SGLang draft" {
		t.Fatalf("generation result = %#v", result)
	}
	history, err := service.ModelMaintenanceHistory(10)
	if err != nil || len(history) != 1 || history[0].ProviderID != "sglang" || history[0].ModelID != "qwen-sglang" || history[0].Status != "current" || history[0].BlocksExecution {
		t.Fatalf("SGLang daily maintenance history = %#v, err=%v", history, err)
	}
}

func TestMistralRSProviderProbesAndGeneratesThroughOpenAICompatibleAPI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"data": []map[string]string{{"id": "qwen-mistralrs"}}})
		case "/v1/chat/completions":
			var request map[string]interface{}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatalf("decode request: %v", err)
			}
			if request["model"] != "qwen-mistralrs" {
				t.Fatalf("model = %v, want qwen-mistralrs", request["model"])
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"choices": []map[string]interface{}{{"message": map[string]string{"content": "local mistral.rs draft"}}}})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	policy := testPolicyWithoutEndpoints()
	provider := providerIndex(t, policy, "mistral-rs")
	policy.Providers[provider].EndpointURL = server.URL
	policy.Providers[provider].Models[0].ID = "qwen-mistralrs"
	service := withTrustedTestFinalEffects(t, &Service{policy: policy})

	var probe ProviderProbeResult
	for _, result := range service.ProbeProviders() {
		if result.ProviderID == "mistral-rs" {
			probe = result
			break
		}
	}
	if probe.Status != "live" || probe.ModelsSeen != 1 {
		t.Fatalf("probe = %#v, want live mistral.rs provider with one model", probe)
	}

	result, err := service.Generate(withTrustedTestEffect(GenerateRequest{
		Task: "Draft a local answer",
		RouteDecision: &RouteDecision{
			SelectedProviderID: "mistral-rs",
			SelectedModelID:    "qwen-mistralrs",
			SelectedModelName:  "Configured mistral.rs local model",
			Tier:               TierLocal,
		},
	}))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if result.Status != "completed" || result.Output != "local mistral.rs draft" {
		t.Fatalf("generation result = %#v", result)
	}
}

func TestLiteLLMGatewayUsesVirtualKeyAndRequiresGenerationApproval(t *testing.T) {
	t.Setenv("LITELLM_API_KEY", "gateway-secret")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer gateway-secret" {
			t.Fatalf("authorization = %q", got)
		}
		switch r.URL.Path {
		case "/v1/models":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"data": []map[string]string{{"id": "local-qwen"}}})
		case "/v1/chat/completions":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"choices": []map[string]interface{}{{"message": map[string]string{"content": "reviewed local gateway draft"}}}})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	policy := testPolicyWithoutEndpoints()
	liteLLMIndex := providerIndex(t, policy, "litellm")
	policy.Providers[liteLLMIndex].Enabled = true
	policy.Providers[liteLLMIndex].EndpointURL = server.URL
	policy.Providers[liteLLMIndex].Models[0].ID = "local-qwen"
	service := withTrustedTestFinalEffects(t, &Service{policy: policy})

	var probe ProviderProbeResult
	for _, result := range service.ProbeProviders() {
		if result.ProviderID == "litellm" {
			probe = result
			break
		}
	}
	if probe.Status != "live" || probe.ModelsSeen != 1 {
		t.Fatalf("probe = %#v, want live LiteLLM gateway with one model", probe)
	}

	decision := &RouteDecision{SelectedProviderID: "litellm", SelectedModelID: "local-qwen", SelectedModelName: "Configured LiteLLM local model alias", Tier: TierLocal}
	blocked, err := service.Generate(GenerateRequest{Task: "Draft a local answer", RouteDecision: decision})
	if err != nil {
		t.Fatalf("Generate blocked: %v", err)
	}
	if blocked.Status != "blocked" {
		t.Fatalf("status = %q, want blocked before manual approval", blocked.Status)
	}

	completed, err := service.Generate(withTrustedTestEffect(GenerateRequest{Task: "Draft a local answer", RouteDecision: decision, AllowPaidApproved: true}))
	if err != nil {
		t.Fatalf("Generate approved: %v", err)
	}
	if completed.Status != "completed" || completed.Output != "reviewed local gateway draft" {
		t.Fatalf("generation result = %#v", completed)
	}
}

func TestLiteLLMGatewayGenerationRequiresLiveProbe(t *testing.T) {
	t.Setenv("LITELLM_API_KEY", "gateway-secret")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer gateway-secret" {
			t.Fatalf("authorization = %q", got)
		}
		if r.URL.Path != "/v1/models" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	policy := testPolicyWithoutEndpoints()
	liteLLMIndex := providerIndex(t, policy, "litellm")
	policy.Providers[liteLLMIndex].Enabled = true
	policy.Providers[liteLLMIndex].EndpointURL = server.URL
	service := withTrustedTestFinalEffects(t, &Service{policy: policy})

	result, err := service.Generate(withTrustedTestEffect(GenerateRequest{
		Task:              "Draft a local answer",
		AllowPaidApproved: true,
		RouteDecision:     &RouteDecision{SelectedProviderID: "litellm", SelectedModelID: "local-model", SelectedModelName: "Configured LiteLLM local model alias", Tier: TierLocal},
	}))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if result.Status != "skipped" || !strings.Contains(result.Reason, "live authenticated /v1/models probe") {
		t.Fatalf("generation result = %#v", result)
	}
}

func TestGenerateBlocksPaidWithoutApproval(t *testing.T) {
	policy := withTestPaidProvider(testPolicyWithoutEndpoints(), "http://example.invalid")
	policy.PaidCallsAllowed = true
	policy.DailyPaidBudgetEUR = 1
	service := &Service{policy: policy}

	result, err := service.Generate(GenerateRequest{
		Task: "Handle a difficult verification task",
		RouteDecision: &RouteDecision{
			SelectedProviderID: "test-paid-provider",
			SelectedModelID:    "test-paid-high-capability",
			SelectedModelName:  "Test paid high capability model",
			Tier:               TierExpensive,
		},
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if result.Status != "blocked" {
		t.Fatalf("status = %q, want blocked", result.Status)
	}
}

func TestDefaultPolicyIncludesOdysseusWhenConfigured(t *testing.T) {
	t.Setenv("ODYSSEUS_BASE_URL", "http://localhost:8080")
	t.Setenv("ODYSSEUS_API_TOKEN", "test-token")

	policy := defaultPolicy()
	odysseusIndex := providerIndex(t, policy, "odysseus")
	provider := policy.Providers[odysseusIndex]

	if provider.EndpointURL != "http://localhost:8080" {
		t.Fatalf("endpoint = %q, want configured Odysseus URL", provider.EndpointURL)
	}
	if provider.APIKeyEnv != "ODYSSEUS_API_TOKEN" {
		t.Fatalf("apiKeyEnv = %q, want ODYSSEUS_API_TOKEN", provider.APIKeyEnv)
	}
	if len(provider.Models) != 1 || !provider.Models[0].RequiresApproval {
		t.Fatalf("Odysseus workspace model must be approval-required: %#v", provider.Models)
	}
}

func TestProbeProvidersChecksOdysseusHealthWithOptionalToken(t *testing.T) {
	t.Setenv("ODYSSEUS_API_TOKEN", "test-token")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/health" {
			t.Fatalf("path = %s, want /api/health", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Fatalf("authorization = %q, want bearer token", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	defer server.Close()

	policy := testPolicyWithoutEndpoints()
	odysseusIndex := providerIndex(t, policy, "odysseus")
	policy.Providers[odysseusIndex].EndpointURL = server.URL
	policy.Providers[odysseusIndex].APIKeyEnv = "ODYSSEUS_API_TOKEN"
	service := &Service{policy: policy}

	results := service.ProbeProviders()
	var odysseusProbe ProviderProbeResult
	for _, result := range results {
		if result.ProviderID == "odysseus" {
			odysseusProbe = result
			break
		}
	}
	if odysseusProbe.ProviderID == "" {
		t.Fatalf("Odysseus probe not returned: %#v", results)
	}
	if odysseusProbe.Status != "live" {
		t.Fatalf("status = %q, want live: %s", odysseusProbe.Status, odysseusProbe.Reason)
	}
	if !strings.Contains(odysseusProbe.Reason, "execution remains approval-gated") {
		t.Fatalf("probe reason should document execution gate: %s", odysseusProbe.Reason)
	}
}

func TestGenerateBlocksOdysseusExecutionEvenWhenInternallyApproved(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "should not execute"})
	}))
	defer server.Close()

	policy := testPolicyWithoutEndpoints()
	odysseusIndex := providerIndex(t, policy, "odysseus")
	policy.Providers[odysseusIndex].EndpointURL = server.URL
	service := &Service{policy: policy}

	result, err := service.Generate(withTrustedTestEffect(GenerateRequest{
		Task:              "Let Odysseus run this autonomous agent task",
		AllowPaidApproved: true,
		RouteDecision: &RouteDecision{
			SelectedProviderID: "odysseus",
			SelectedModelID:    "odysseus-workspace-agent",
			SelectedModelName:  "Odysseus workspace agent",
			Tier:               TierFree,
		},
	}))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if called {
		t.Fatalf("Odysseus endpoint was called despite execution guard")
	}
	if result.Status != "blocked" {
		t.Fatalf("status = %q, want blocked", result.Status)
	}
	if !strings.Contains(result.Reason, "discovery and health probing only") {
		t.Fatalf("reason = %q, want discovery-only guard", result.Reason)
	}
}

func TestPolicyKeepsDualPathDisabledWithoutVerifiedInfrastructure(t *testing.T) {
	t.Setenv("LLM_KV_CACHE_LOAD_STRATEGY", "dual")
	t.Setenv("LLM_DUALPATH_INFRASTRUCTURE_VERIFIED", "false")
	service := &Service{policy: defaultPolicy()}

	infrastructure := service.Policy().InferenceInfrastructure
	if infrastructure.KVCacheLoadStrategy != "dual" {
		t.Fatalf("strategy = %q, want dual", infrastructure.KVCacheLoadStrategy)
	}
	if infrastructure.DualPathInfrastructureAvailable {
		t.Fatalf("expected DualPath to remain unavailable without verified infrastructure")
	}
}

func TestPolicyRejectsUnknownKVCacheStrategy(t *testing.T) {
	t.Setenv("LLM_KV_CACHE_LOAD_STRATEGY", "pretend-fast")
	service := &Service{policy: defaultPolicy()}

	if got := service.Policy().InferenceInfrastructure.KVCacheLoadStrategy; got != "disabled" {
		t.Fatalf("strategy = %q, want disabled", got)
	}
}

func TestDefaultPolicyIncludesNousPortalCatalog(t *testing.T) {
	t.Setenv("NOUS_PORTAL_BASE_URL", "https://portal.example.test/v1")
	t.Setenv("NOUS_PORTAL_API_KEY", "test-token")

	policy := defaultPolicy()
	nousIndex := providerIndex(t, policy, "nous-portal")
	provider := policy.Providers[nousIndex]

	if provider.EndpointURL != "https://portal.example.test/v1" {
		t.Fatalf("endpoint = %q, want configured Nous Portal URL", provider.EndpointURL)
	}
	if provider.APIKeyEnv != "NOUS_PORTAL_API_KEY" {
		t.Fatalf("apiKeyEnv = %q, want NOUS_PORTAL_API_KEY", provider.APIKeyEnv)
	}
	if !provider.Paid {
		t.Fatalf("Nous Portal should be marked paid/approval-gated")
	}
	if len(provider.Models) != 24 {
		t.Fatalf("models = %d, want 24: %#v", len(provider.Models), provider.Models)
	}

	wantTiers := map[string]string{
		"opus-4.8":                   TierExpensive,
		"gpt-5.5-pro":                TierExpensive,
		"gpt-5.5":                    TierPremium,
		"qwen3.7-max":                TierPremium,
		"gemini-3-pro-preview":       TierPremium,
		"hy3-preview":                TierPremium,
		"nemotron-3-super-120b-a12b": TierPremium,
		"sonnet-4.6":                 TierHigh,
		"deepseek-v4-pro":            TierHigh,
		"gemini-3.1-pro-preview":     TierHigh,
		"grok-4.3":                   TierHigh,
		"kimi-k2.7-code":             TierHigh,
		"minimax-m3":                 TierHigh,
		"glm-5.2":                    TierHigh,
		"mimo-v2.5-pro":              TierHigh,
		"haiku-4.5":                  TierAcceptable,
		"qwen3.7-plus":               TierAcceptable,
		"glm-5.1":                    TierAcceptable,
		"gpt-5.4-mini":               TierCheap,
		"gemini-3.5-flash":           TierCheap,
		"deepseek-v4-flash":          TierCheap,
		"qwen3.6-35b-a3b":            TierCheap,
		"step-3.7-flash":             TierCheap,
		"step-3.7-flash-free":        TierFree,
	}
	for modelID, tier := range wantTiers {
		model, ok := findModel(provider.Models, modelID)
		if !ok {
			t.Fatalf("model %q not found", modelID)
		}
		if model.Tier != tier {
			t.Fatalf("model %s tier = %q, want %q", modelID, model.Tier, tier)
		}
		if !model.RequiresApproval {
			t.Fatalf("model %s should require approval", modelID)
		}
		if modelID == "step-3.7-flash-free" {
			if model.InputCostPerMillionTokensEUR != 0 || model.OutputCostPerMillionTokensEUR != 0 {
				t.Fatalf("free model %s should have zero token pricing: %#v", modelID, model)
			}
			continue
		}
		if model.InputCostPerMillionTokensEUR <= 0 || model.OutputCostPerMillionTokensEUR <= 0 {
			t.Fatalf("model %s missing token pricing: %#v", modelID, model)
		}
		if !strings.Contains(model.PricingSource, "default estimate") {
			t.Fatalf("model %s pricing source = %q, want default estimate warning", modelID, model.PricingSource)
		}
	}
}

func TestDefaultPolicyIncludesMixtureAndOpenAICodexCatalogs(t *testing.T) {
	t.Setenv("MIXTURE_OF_AGENTS_BASE_URL", "https://moa.example.test/v1")
	t.Setenv("MIXTURE_OF_AGENTS_API_KEY", "test-token")
	t.Setenv("OPENAI_CODEX_BASE_URL", "https://codex.example.test/v1")
	t.Setenv("OPENAI_CODEX_API_KEY", "test-token")

	policy := defaultPolicy()
	mixture := policy.Providers[providerIndex(t, policy, "mixture-of-agents")]
	codex := policy.Providers[providerIndex(t, policy, "openai-codex")]

	if mixture.EndpointURL != "https://moa.example.test/v1" || mixture.APIKeyEnv != "MIXTURE_OF_AGENTS_API_KEY" {
		t.Fatalf("mixture provider env mapping is wrong: %#v", mixture)
	}
	if len(mixture.Models) != 1 || mixture.Models[0].ID != "moa-default" || mixture.Models[0].Tier != TierPremium {
		t.Fatalf("unexpected mixture catalog: %#v", mixture.Models)
	}
	if codex.EndpointURL != "https://codex.example.test/v1" || codex.APIKeyEnv != "OPENAI_CODEX_API_KEY" {
		t.Fatalf("codex provider env mapping is wrong: %#v", codex)
	}
	wantTiers := map[string]string{
		"gpt-5.5":             TierPremium,
		"gpt-5.4":             TierHigh,
		"gpt-5.4-mini":        TierCheap,
		"gpt-5.3-codex-spark": TierCheap,
	}
	for modelID, tier := range wantTiers {
		model, ok := findModel(codex.Models, modelID)
		if !ok {
			t.Fatalf("codex model %q not found", modelID)
		}
		if model.Tier != tier {
			t.Fatalf("codex model %s tier = %q, want %q", modelID, model.Tier, tier)
		}
		if !model.RequiresApproval {
			t.Fatalf("codex model %s should require approval", modelID)
		}
	}
}

func TestGenerateTracksModelLevelUsageAndTokenPrice(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []map[string]interface{}{
				{"message": map[string]string{"content": "priced draft response"}},
			},
			"usage": map[string]int{"prompt_tokens": 19, "completion_tokens": 7},
		})
	}))
	defer server.Close()

	policy := testPolicyWithoutEndpoints()
	policy.Providers[1].EndpointURL = server.URL
	policy.Providers[1].Models[0].InputCostPerMillionTokensEUR = 10
	policy.Providers[1].Models[0].OutputCostPerMillionTokensEUR = 20
	service := withTrustedTestFinalEffects(t, &Service{policy: policy})

	result, err := service.Generate(withTrustedTestEffect(GenerateRequest{
		Task: "Plan this work item",
		RouteDecision: &RouteDecision{
			SelectedProviderID: "lm-studio",
			SelectedModelID:    "local-model",
			SelectedModelName:  "Configured LM Studio local model",
			Tier:               TierLocal,
		},
	}))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if result.Status != "completed" {
		t.Fatalf("status = %q, want completed", result.Status)
	}
	if result.InputTokens != 19 || result.OutputTokens != 7 || result.UsageSource != "provider_reported" {
		t.Fatalf("generation usage = %#v, want provider-reported 19 input and 7 output tokens", result)
	}
	policyWithUsage := service.Policy()
	provider := policyWithUsage.Providers[1]
	model := provider.Models[0]
	if provider.InputTokensUsed == 0 || provider.OutputTokensUsed == 0 {
		t.Fatalf("provider usage not tracked: %#v", provider)
	}
	if model.InputTokensUsed != provider.InputTokensUsed || model.OutputTokensUsed != provider.OutputTokensUsed {
		t.Fatalf("model usage %#v did not match provider usage %#v", model, provider)
	}
	if model.BudgetUsedEUR <= 0 || provider.BudgetUsedEUR <= 0 || policyWithUsage.DailyBudgetUsedEUR <= 0 {
		t.Fatalf("priced usage not accumulated: provider=%#v model=%#v policy=%#v", provider, model, policyWithUsage)
	}
	if provider.InputTokensUsed != 19 || provider.OutputTokensUsed != 7 {
		t.Fatalf("provider exact usage = %#v, want 19 input and 7 output tokens", provider)
	}
}

func TestGenerateUsesOllamaReportedTokenCounts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/generate" {
			t.Fatalf("path = %q, want /api/generate", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"response":          "ollama draft",
			"prompt_eval_count": 23,
			"eval_count":        11,
		})
	}))
	defer server.Close()

	policy := testPolicyWithoutEndpoints()
	policy.Providers[0].EndpointURL = server.URL
	service := withTrustedTestFinalEffects(t, &Service{policy: policy})
	result, err := service.Generate(withTrustedTestEffect(GenerateRequest{
		Task: "Draft a local answer",
		RouteDecision: &RouteDecision{
			SelectedProviderID: "ollama",
			SelectedModelID:    "phi3:mini",
			SelectedModelName:  "Phi small local",
			Tier:               TierLocal,
		},
	}))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if result.Status != "completed" || result.InputTokens != 23 || result.OutputTokens != 11 || result.UsageSource != "provider_reported" {
		t.Fatalf("generation result = %#v, want Ollama-reported usage", result)
	}
}

func TestGeneratePersistsRedactedOperationalEvidence(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []map[string]interface{}{{"message": map[string]string{"content": "draft containing secret-value"}}},
			"usage":   map[string]int{"prompt_tokens": 13, "completion_tokens": 5},
		})
	}))
	defer server.Close()

	policy := testPolicyWithoutEndpoints()
	policy.Providers[1].EndpointURL = server.URL
	history := &fakeGenerationHistoryRepository{}
	service := withTrustedTestFinalEffects(t, &Service{policy: policy, generationHistory: history})
	result, err := service.Generate(withTrustedTestEffect(GenerateRequest{
		Task:          "Draft a safe local answer",
		RouteDecision: &RouteDecision{SelectedProviderID: "lm-studio", SelectedModelID: "local-model", SelectedModelName: "Configured LM Studio local model", Tier: TierLocal},
	}))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if result.AuditStatus != "recorded" || len(history.records) != 1 {
		t.Fatalf("audit status=%q records=%#v", result.AuditStatus, history.records)
	}
	record := history.records[0]
	if record.InputTokens != 13 || record.OutputTokens != 5 || record.UsageSource != "provider_reported" {
		t.Fatalf("record usage=%#v", record)
	}
	if strings.Contains(record.Reason, "secret-value") || strings.Contains(record.FallbackPathJSON, "secret-value") {
		t.Fatalf("generation record retained output content: %#v", record)
	}
	entries, err := service.GenerationHistory(10)
	if err != nil || len(entries) != 1 || entries[0].Output != "" || entries[0].AuditStatus != "recorded" {
		t.Fatalf("history=%#v err=%v", entries, err)
	}
}

func TestProviderUsageSourceLabelsPartialAndEstimatedCounts(t *testing.T) {
	tests := []struct {
		name  string
		usage providerUsage
		want  string
	}{
		{name: "both", usage: providerUsage{HasInput: true, HasOutput: true}, want: "provider_reported"},
		{name: "input only", usage: providerUsage{HasInput: true}, want: "provider_reported_partial"},
		{name: "none", usage: providerUsage{}, want: "estimated"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.usage.source(); got != test.want {
				t.Fatalf("source = %q, want %q", got, test.want)
			}
		})
	}
}

func findModel(models []Model, id string) (Model, bool) {
	for _, model := range models {
		if model.ID == id {
			return model, true
		}
	}
	return Model{}, false
}

type fakeProbeHistoryRepository struct {
	probes []models.LLMProviderProbe
}

type fakeModelMaintenanceRepository struct {
	records []models.LLMModelMaintenance
}

type fakeGenerationHistoryRepository struct {
	records []models.LLMGenerationRecord
	err     error
}

func (r *fakeGenerationHistoryRepository) RecordGeneration(record *models.LLMGenerationRecord) (*models.LLMGenerationRecord, error) {
	if r.err != nil {
		return nil, r.err
	}
	copy := *record
	if copy.LoggedAt.IsZero() {
		copy.LoggedAt = time.Now().UTC()
	}
	r.records = append(r.records, copy)
	return &copy, nil
}

func (r *fakeGenerationHistoryRepository) FindRecentGenerations(limit int) ([]models.LLMGenerationRecord, error) {
	if r.err != nil {
		return nil, r.err
	}
	if limit <= 0 || limit > len(r.records) {
		limit = len(r.records)
	}
	result := make([]models.LLMGenerationRecord, 0, limit)
	for index := len(r.records) - 1; index >= 0 && len(result) < limit; index-- {
		result = append(result, r.records[index])
	}
	return result, nil
}

func (r *fakeModelMaintenanceRepository) RecordModelMaintenance(record *models.LLMModelMaintenance) (*models.LLMModelMaintenance, error) {
	copy := *record
	if copy.CheckedAt.IsZero() {
		copy.CheckedAt = time.Now().UTC()
	}
	r.records = append(r.records, copy)
	return &copy, nil
}

func (r *fakeModelMaintenanceRepository) FindLatestModelMaintenance(providerID, modelID string) (*models.LLMModelMaintenance, error) {
	var latest *models.LLMModelMaintenance
	for index := range r.records {
		record := r.records[index]
		if record.ProviderID == providerID && record.ModelID == modelID && (latest == nil || record.CheckedAt.After(latest.CheckedAt)) {
			latest = &record
		}
	}
	return latest, nil
}

func (r *fakeModelMaintenanceRepository) FindRecentModelMaintenance(limit int) ([]models.LLMModelMaintenance, error) {
	if limit <= 0 || limit > len(r.records) {
		limit = len(r.records)
	}
	results := make([]models.LLMModelMaintenance, 0, limit)
	for index := len(r.records) - 1; index >= 0 && len(results) < limit; index-- {
		results = append(results, r.records[index])
	}
	return results, nil
}

func (r *fakeProbeHistoryRepository) RecordProviderProbe(probe *models.LLMProviderProbe) (*models.LLMProviderProbe, error) {
	copy := *probe
	if copy.Live {
		lastSuccess := copy.CheckedAt
		copy.LastSuccessfulAt = &lastSuccess
	} else {
		for index := len(r.probes) - 1; index >= 0; index-- {
			previous := r.probes[index]
			if previous.ProviderID == copy.ProviderID && previous.LastSuccessfulAt != nil {
				lastSuccess := *previous.LastSuccessfulAt
				copy.LastSuccessfulAt = &lastSuccess
				break
			}
		}
	}
	if copy.CheckedAt.IsZero() {
		copy.CheckedAt = time.Now().UTC()
	}
	r.probes = append(r.probes, copy)
	return &copy, nil
}

func (r *fakeProbeHistoryRepository) FindRecentProviderProbes(limit int) ([]models.LLMProviderProbe, error) {
	if limit <= 0 || limit > len(r.probes) {
		limit = len(r.probes)
	}
	result := make([]models.LLMProviderProbe, 0, limit)
	for index := len(r.probes) - 1; index >= 0 && len(result) < limit; index-- {
		result = append(result, r.probes[index])
	}
	return result, nil
}

func (r *fakeProbeHistoryRepository) FindLatestProviderProbe(providerID string) (*models.LLMProviderProbe, error) {
	var latest *models.LLMProviderProbe
	for index := range r.probes {
		probe := r.probes[index]
		if probe.ProviderID == providerID && (latest == nil || probe.CheckedAt.After(latest.CheckedAt)) {
			latest = &probe
		}
	}
	return latest, nil
}
