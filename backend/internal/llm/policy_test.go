package llm

import (
	"automation-hub-backend/internal/agentframework"
	"automation-hub-backend/internal/models"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
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
	for _, skipped := range decision.Skipped {
		if skipped.ProviderID == "paid-provider" && skipped.Reason == "" {
			t.Fatalf("paid provider skip should include a reason")
		}
	}
}

func TestDefaultPolicyDoesNotPresentCustomPaidProviderAsImplemented(t *testing.T) {
	policy := defaultPolicy()
	provider := policy.Providers[providerIndex(t, policy, "paid-provider")]
	if provider.Enabled || provider.Configured {
		t.Fatalf("custom paid provider must remain disabled and unconfigured by default: %#v", provider)
	}
	if strings.Contains(strings.ToLower(provider.Name), "placeholder") {
		t.Fatalf("custom paid provider name must not imply a placeholder integration: %q", provider.Name)
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

func TestOllamaCloudModelTagsAreUnknownAndPaidPolicyGated(t *testing.T) {
	for _, modelID := range []string{
		"qwen3-coder:480b-cloud",
		"QWEN3-CODER:480B-CLOUD",
		"qwen3-coder-480b-CLOUD",
	} {
		t.Run(modelID, func(t *testing.T) {
			t.Setenv("OLLAMA_MODEL_IDS", modelID)
			models := configuredOllamaModels()
			if len(models) != 1 {
				t.Fatalf("configured models = %#v, want one", models)
			}
			if models[0].Tier != TierUnknown || !models[0].RequiresApproval {
				t.Fatalf("cloud model classification = %#v, want unknown tier and approval", models[0])
			}

			policy := Policy{
				LocalModelsAllowed:    true,
				FreeCloudQuotaAllowed: true,
				LocalFirst:            true,
				TierOrder:             []string{TierLocal, TierFree, TierExpensive, TierUnknown},
				Providers: []Provider{{
					ID: "ollama", Name: "Ollama", Enabled: true, Local: true,
					EndpointURL: "http://127.0.0.1:11434", QuotaRemaining: -1,
					Models: models,
				}},
			}
			decision, err := (&Service{policy: policy}).Route(RouteRequest{Task: "draft a short note"})
			if err != nil {
				t.Fatalf("Route: %v", err)
			}
			if decision.SelectedModelID != "" {
				t.Fatalf("cloud model was selected under local/free policy: %#v", decision)
			}
			foundPaidPolicySkip := false
			for _, skipped := range decision.Skipped {
				if skipped.ProviderID == "ollama" && skipped.ModelID == modelID && skipped.Reason == "paid usage disabled by policy" {
					foundPaidPolicySkip = true
					break
				}
			}
			if !foundPaidPolicySkip {
				t.Fatalf("cloud model was not rejected by paid policy: %#v", decision.Skipped)
			}
		})
	}
}

func TestOllamaCloudModelCanRouteOnlyThroughPaidPolicyNotLocalPolicy(t *testing.T) {
	t.Setenv("OLLAMA_MODEL_IDS", "qwen3-coder:480b-cloud")
	models := configuredOllamaModels()
	policy := Policy{
		DailyPaidBudgetEUR:             1,
		PaidCallsAllowed:               true,
		LocalModelsAllowed:             false,
		FreeCloudQuotaAllowed:          false,
		RequireApprovalBeforePaidUsage: false,
		LocalFirst:                     true,
		TierOrder:                      []string{TierLocal, TierFree, TierExpensive, TierUnknown},
		Providers: []Provider{{
			ID: "ollama", Name: "Ollama", Enabled: true, Local: true,
			EndpointURL: "http://127.0.0.1:11434", QuotaRemaining: -1,
			Models: models,
		}},
	}

	decision, err := (&Service{policy: policy}).Route(RouteRequest{Task: "draft a short note"})
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	if decision.SelectedModelID != "qwen3-coder:480b-cloud" || decision.Tier != TierUnknown || !decision.RequiresApproval {
		t.Fatalf("cloud model did not retain unknown paid classification: %#v", decision)
	}
}

func TestOllamaCloudInferenceCannotUseApprovalToBypassPaidPolicy(t *testing.T) {
	for _, test := range []struct {
		name             string
		paidCallsAllowed bool
		dailyBudgetEUR   float64
	}{
		{name: "paid use disabled", paidCallsAllowed: false, dailyBudgetEUR: 1},
		{name: "no paid budget", paidCallsAllowed: true, dailyBudgetEUR: 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("OLLAMA_MODEL_IDS", "qwen3-coder:480b-cloud")
			var called atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called.Add(1)
				t.Errorf("provider endpoint must not be called when paid policy is blocked: %s", r.URL.Path)
			}))
			defer server.Close()

			policy := defaultPolicy()
			index := providerIndex(t, policy, "ollama")
			provider := policy.Providers[index]
			provider.EndpointURL = server.URL
			provider.Models = configuredOllamaModels()
			policy.Providers = []Provider{provider}
			policy.PaidCallsAllowed = test.paidCallsAllowed
			policy.DailyPaidBudgetEUR = test.dailyBudgetEUR
			policy = annotatePolicyReadiness(policy)
			service := withTrustedTestFinalEffects(t, &Service{policy: policy})

			result, err := service.Generate(withTrustedTestEffect(GenerateRequest{
				Task: "draft a short note",
				// A stale caller-supplied local classification must not override model policy.
				RouteDecision: &RouteDecision{
					SelectedProviderID: "ollama", SelectedModelID: "qwen3-coder:480b-cloud",
					SelectedModelName: "Cloud model", Tier: TierLocal,
				},
			}))
			if err != nil {
				t.Fatalf("Generate: %v", err)
			}
			if result.Status != "blocked" || !strings.Contains(result.Reason, "billing is unknown") {
				t.Fatalf("cloud inference result = %#v, want paid-policy block", result)
			}
			if called.Load() != 0 {
				t.Fatalf("provider endpoint received %d requests despite paid policy block", called.Load())
			}
		})
	}
}

func TestTrueLocalOllamaModelRemainsLocalWithPaidUsageDisabled(t *testing.T) {
	t.Setenv("OLLAMA_MODEL_IDS", "custom-qwen:7b,qwen3-coder:480b-cloud")
	policy := defaultPolicy()
	index := providerIndex(t, policy, "ollama")
	provider := policy.Providers[index]
	provider.EndpointURL = "http://127.0.0.1:11434"
	provider.Models = configuredOllamaModels()
	if len(provider.Models) != 2 {
		t.Fatalf("mixed Ollama model list = %#v, want a local and cloud model", provider.Models)
	}
	provider, model := applyOllamaModelPolicy(provider, provider.Models[0])
	if !provider.Local || provider.Paid || model.Tier != TierLocal || model.RequiresApproval {
		t.Fatalf("true local model policy changed: provider=%#v model=%#v", provider, model)
	}
	cloudProvider, cloudModel := applyOllamaModelPolicy(provider, provider.Models[1])
	if cloudProvider.Local || !cloudProvider.Paid || cloudModel.Tier != TierUnknown || !cloudModel.RequiresApproval {
		t.Fatalf("cloud model did not receive separate paid policy: provider=%#v model=%#v", cloudProvider, cloudModel)
	}

	policy.Providers = []Provider{provider}
	policy.LocalModelsAllowed = true
	policy.PaidCallsAllowed = false
	policy.DailyPaidBudgetEUR = 0
	policy = annotatePolicyReadiness(policy)
	decision, err := (&Service{policy: policy}).Route(RouteRequest{Task: "draft a short note"})
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	if decision.SelectedModelID != "custom-qwen:7b" || decision.Tier != TierLocal {
		t.Fatalf("true local model no longer routes under local policy: %#v", decision)
	}
	foundCloudPaidSkip := false
	for _, skipped := range decision.Skipped {
		if skipped.ModelID == "qwen3-coder:480b-cloud" && skipped.Reason == "paid usage disabled by policy" {
			foundCloudPaidSkip = true
			break
		}
	}
	if !foundCloudPaidSkip {
		t.Fatalf("mixed local/cloud allowlist did not keep cloud model behind paid policy: %#v", decision.Skipped)
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

func TestLocalProviderFlagCannotAuthorizeRemoteEndpoint(t *testing.T) {
	remote := providerRuntimeReadiness(Provider{
		ID: "custom-openai-compatible", Enabled: true, Local: true,
		EndpointURL: "https://models.example.test/v1",
	})
	if remote.configured || remote.status != "blocked_endpoint" {
		t.Fatalf("remote endpoint marked local readiness = %#v, want blocked_endpoint", remote)
	}

	local := providerRuntimeReadiness(Provider{
		ID: "custom-openai-compatible", Enabled: true, Local: true,
		EndpointURL: "http://127.0.0.1:11434/v1",
	})
	if !local.configured {
		t.Fatalf("loopback local endpoint readiness = %#v, want configured", local)
	}
}

func TestProviderReadinessRequiresTLSForRemoteAndRejectsEmbeddedCredentials(t *testing.T) {
	for _, test := range []struct {
		name       string
		endpoint   string
		wantStatus string
		wantReason string
	}{
		{
			name: "remote HTTP is blocked", endpoint: "http://models.example.test/v1",
			wantStatus: "blocked_endpoint", wantReason: "remote provider endpoints must use HTTPS",
		},
		{
			name: "URL userinfo is rejected", endpoint: "https://user:secret@models.example.test/v1",
			wantStatus: "invalid_endpoint", wantReason: "without embedded credentials",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			readiness := providerRuntimeReadiness(Provider{
				ID: "custom-openai-compatible", Enabled: true, EndpointURL: test.endpoint,
			})
			if readiness.configured || readiness.status != test.wantStatus || !strings.Contains(readiness.reason, test.wantReason) {
				t.Fatalf("provider readiness = %#v; want status %q and reason containing %q", readiness, test.wantStatus, test.wantReason)
			}
		})
	}

	localHTTP := providerRuntimeReadiness(Provider{
		ID: "custom-openai-compatible", Enabled: true, EndpointURL: "http://127.0.0.1:11434/v1",
	})
	if !localHTTP.configured {
		t.Fatalf("loopback HTTP provider readiness = %#v; local development endpoint should remain supported", localHTTP)
	}
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
	disableModelMaintenanceForTest(t)
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
		_ = json.NewEncoder(w).Encode(map[string]string{"response": "grounded draft"})
	}))
	defer server.Close()

	policy := testPolicyWithoutEndpoints()
	policy.Providers[0].EndpointURL = server.URL
	service := withTrustedTestFinalEffects(t, &Service{policy: policy})

	result, err := service.Generate(withTrustedTestEffect(GenerateRequest{
		Task: "Summarize this short note",
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

func TestGenerateStrictLiveProbePolicyBlocksExplicitRouteDecision(t *testing.T) {
	disableModelMaintenanceForTest(t)
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
	disableModelMaintenanceForTest(t)
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
	if client != noRedirectHTTPClient() {
		t.Fatal("provider HTTP client must reuse a shared transport pool")
	}
	if transport.MaxConnsPerHost <= 0 || transport.MaxIdleConns <= 0 || transport.MaxIdleConnsPerHost <= 0 || transport.IdleConnTimeout <= 0 {
		t.Fatal("provider HTTP transport must bound active and idle connections")
	}
	if err := client.CheckRedirect(nil, nil); err != http.ErrUseLastResponse {
		t.Fatalf("redirect behavior = %v, want %v", err, http.ErrUseLastResponse)
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

func TestProbeProvidersRejectsTruncatedResponseAsNotLive(t *testing.T) {
	for _, test := range []struct {
		name          string
		body          string
		contentLength string
		wantReason    string
	}{
		{
			name:          "transport read error",
			body:          `{"models":[{"name":"phi3:mini"}]}`,
			contentLength: "1024",
			wantReason:    "could not be read",
		},
		{
			name:       "clean but incomplete JSON",
			body:       `{"models":[{"name":`,
			wantReason: "invalid JSON",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if test.contentLength != "" {
					w.Header().Set("Content-Length", test.contentLength)
				}
				_, _ = w.Write([]byte(test.body))
			}))
			defer server.Close()

			policy := testPolicyWithoutEndpoints()
			policy.Providers[0].EndpointURL = server.URL
			results := (&Service{policy: policy}).ProbeProviders()
			if len(results) == 0 || results[0].Live || results[0].Status != "failed" {
				t.Fatalf("truncated probe response = %#v, want failed and not live", results)
			}
			if !strings.Contains(results[0].Reason, test.wantReason) {
				t.Fatalf("truncated probe reason = %q, want %q", results[0].Reason, test.wantReason)
			}
		})
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

func TestGenerateBlocksOllamaModelWhenPullOmitsSuccessStatus(t *testing.T) {
	t.Setenv("LLM_MODEL_MAINTENANCE_ENABLED", "true")
	pulls := 0
	generations := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"models": []map[string]string{{"name": "phi3:mini", "digest": "sha256:unchanged"}}})
		case "/api/pull":
			pulls++
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{}`))
		case "/api/generate":
			generations++
			_ = json.NewEncoder(w).Encode(map[string]string{"response": "must not execute"})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	policy := testPolicyWithoutEndpoints()
	policy.Providers[0].EndpointURL = server.URL
	policy.Providers[0].Models = []Model{{ID: "phi3:mini", Name: "Phi", Tier: TierLocal, Capabilities: []string{"general", "extraction"}, MaxDifficulty: 5, MaxReasoning: "very_high", Enabled: true}}
	history := &fakeModelMaintenanceRepository{}
	service := withTrustedTestFinalEffects(t, &Service{policy: policy, maintenanceHistory: history, maintenanceRunning: map[string]*sync.Mutex{}})

	decision, err := service.Route(RouteRequest{Task: "classify this"})
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	result, err := service.Generate(withTrustedTestEffect(GenerateRequest{Task: "classify this", RouteDecision: &decision}))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if result.Status != "skipped" || pulls != 1 || generations != 0 {
		t.Fatalf("result=%#v pulls=%d generations=%d; missing provider success must block generation", result, pulls, generations)
	}
	if len(history.records) != 1 || history.records[0].Status != "failed" || !history.records[0].BlocksExecution || !strings.Contains(history.records[0].Reason, "did not confirm success") {
		t.Fatalf("maintenance history = %#v; expected persisted fail-closed provider evidence", history.records)
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

func TestGenerateBlocksUnsupportedLocalRuntimeAfterOtherMaintenanceFailures(t *testing.T) {
	t.Setenv("LLM_MODEL_MAINTENANCE_ENABLED", "true")
	ollama := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			_ = json.NewEncoder(w).Encode(map[string]any{"models": []map[string]string{{"name": "a-model", "digest": "sha256:old"}}})
		case "/api/pull":
			http.Error(w, "registry unavailable", http.StatusBadGateway)
		default:
			t.Errorf("unexpected Ollama path %q", r.URL.Path)
		}
	}))
	defer ollama.Close()
	staleModel := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Errorf("unexpected second-model path %q", r.URL.Path)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]string{{"id": "different-model"}}})
	}))
	defer staleModel.Close()
	var generationCalls atomic.Int32
	availableModel := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]string{{"id": "c-model"}}})
		case "/v1/chat/completions":
			generationCalls.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []map[string]any{{"message": map[string]string{"content": "third model draft"}}},
				"usage":   map[string]int{"prompt_tokens": 9, "completion_tokens": 4},
			})
		default:
			t.Errorf("unexpected available-model path %q", r.URL.Path)
		}
	}))
	defer availableModel.Close()

	policy := Policy{
		LocalModelsAllowed: true,
		LocalFirst:         true,
		TierOrder:          []string{TierLocal},
		Providers: []Provider{
			{ID: "ollama", Name: "First model", Enabled: true, Local: true, EndpointURL: ollama.URL,
				Models: []Model{{ID: "a-model", Name: "First", Tier: TierLocal, Capabilities: []string{"general"}, MaxDifficulty: 5, MaxReasoning: "very_high", Enabled: true}}},
			{ID: "lm-studio", Name: "Second model", Enabled: true, Local: true, EndpointURL: staleModel.URL,
				Models: []Model{{ID: "b-model", Name: "Second", Tier: TierLocal, Capabilities: []string{"general"}, MaxDifficulty: 5, MaxReasoning: "very_high", Enabled: true}}},
			{ID: "localai", Name: "Third model", Enabled: true, Local: true, EndpointURL: availableModel.URL,
				Models: []Model{{ID: "c-model", Name: "Third", Tier: TierLocal, Capabilities: []string{"general"}, MaxDifficulty: 5, MaxReasoning: "very_high", Enabled: true}}},
		},
	}
	history := &fakeModelMaintenanceRepository{}
	service := withTrustedTestFinalEffects(t, &Service{policy: policy, maintenanceHistory: history, maintenanceRunning: map[string]*sync.Mutex{}})
	result, err := service.Generate(withTrustedTestEffect(GenerateRequest{
		Task: "draft a short update",
		RouteDecision: &RouteDecision{
			SelectedProviderID: "ollama",
			SelectedModelID:    "a-model",
			SelectedModelName:  "First",
			Tier:               TierLocal,
		},
	}))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if result.Status != "skipped" || generationCalls.Load() != 0 {
		t.Fatalf("result = %#v, generation calls=%d; unsupported local model must remain unused", result, generationCalls.Load())
	}
	if len(history.records) != 3 {
		t.Fatalf("maintenance records = %#v; want one result for each checked model", history.records)
	}
	if history.records[0].ProviderID != "ollama" || history.records[0].Status != "failed" || !history.records[0].BlocksExecution ||
		history.records[1].ProviderID != "lm-studio" || history.records[1].Status != "failed" || !history.records[1].BlocksExecution ||
		history.records[2].ProviderID != "localai" || history.records[2].Status != "operator_managed" || !history.records[2].BlocksExecution || !strings.Contains(history.records[2].Reason, "cannot verify its upstream version") {
		t.Fatalf("maintenance history did not isolate each model's outcome: %#v", history.records)
	}
}

func TestAgentFrameworkGateRejectsOperatorManagedLocalModel(t *testing.T) {
	t.Setenv("LLM_MODEL_MAINTENANCE_ENABLED", "true")
	var modelProbes, planningCalls atomic.Int32
	modelRuntime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Errorf("model runtime path = %q, want /v1/models", r.URL.Path)
			return
		}
		modelProbes.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]string{{"id": "qwen-local"}}})
	}))
	defer modelRuntime.Close()
	runner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "ok", "configured": true, "modelId": "qwen-local", "modelEndpoint": modelRuntime.URL + "/v1",
			})
		case "/v1/propose":
			planningCalls.Add(1)
			w.WriteHeader(http.StatusOK)
		default:
			t.Errorf("unexpected Agent Framework path %q", r.URL.Path)
		}
	}))
	defer runner.Close()

	policy := Policy{LocalModelsAllowed: true, Providers: []Provider{{
		ID: "localai", Name: "LocalAI", Enabled: true, Local: true, EndpointURL: modelRuntime.URL,
		Models: []Model{{ID: "qwen-local", Name: "Qwen local", Enabled: true}},
	}}}
	history := &fakeModelMaintenanceRepository{}
	canonical := &Service{policy: policy, maintenanceHistory: history, maintenanceRunning: map[string]*sync.Mutex{}}
	planner := agentframework.WithModelMaintenance(agentframework.NewService(true, runner.URL, 0, nil), canonical)
	result, err := planner.Propose(context.Background(), agentframework.Request{Request: "Prepare a bounded plan"})
	if err == nil || result != nil {
		t.Fatalf("proposal = %#v, error = %v; unsupported local model version must fail closed", result, err)
	}
	if modelProbes.Load() != 1 || planningCalls.Load() != 0 {
		t.Fatalf("model probes=%d planning calls=%d; want one check and no proposal", modelProbes.Load(), planningCalls.Load())
	}
	if len(history.records) != 1 || history.records[0].Status != "operator_managed" || !history.records[0].BlocksExecution {
		t.Fatalf("maintenance history = %#v; want one blocked operator-managed result", history.records)
	}
}

func TestRouteAvoidsProviderProbeAndMaintenanceChecksCloudCatalogReadOnly(t *testing.T) {
	t.Setenv("LLM_MODEL_MAINTENANCE_ENABLED", "true")
	var probes atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/models" || r.ContentLength != 0 {
			t.Errorf("provider request = %s %s content-length=%d; want bodyless GET /v1/models", r.Method, r.URL.Path, r.ContentLength)
			http.Error(w, "unexpected request", http.StatusMethodNotAllowed)
			return
		}
		probes.Add(1)
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
	if err != nil || len(history) != 0 || probes.Load() != 0 {
		t.Fatalf("Route performed provider verification: history=%#v probes=%d err=%v", history, probes.Load(), err)
	}
	run := service.RunDueModelMaintenance()
	if run.Failed != 0 || run.Checked != 1 || run.ProviderManaged != 1 {
		t.Fatalf("maintenance run=%#v", run)
	}
	history, err = service.ModelMaintenanceHistory(10)
	if err != nil || len(history) != 1 || history[0].Status != "provider_managed" || history[0].BlocksExecution {
		t.Fatalf("maintenance history = %#v, err=%v", history, err)
	}
	if probes.Load() != 1 || history[0].UpdateAttempted || history[0].UpdateApplied {
		t.Fatalf("cloud maintenance did not make exactly one read-only request or claimed an update: probes=%d result=%#v", probes.Load(), history[0])
	}

	_, err = service.Route(RouteRequest{Task: "plan this again"})
	if err != nil {
		t.Fatalf("second Route: %v", err)
	}
	if probes.Load() != 1 {
		t.Fatalf("routing unexpectedly contacted the provider-managed model; probes=%d", probes.Load())
	}
	secondRun := service.RunDueModelMaintenance()
	if secondRun.Reused != 1 || secondRun.Checked != 0 || probes.Load() != 1 {
		t.Fatalf("second maintenance run=%#v probes=%d", secondRun, probes.Load())
	}
}

func TestLocalMaintenanceFailureMakesLaterRoutesSkipUnverifiedIdentifier(t *testing.T) {
	t.Setenv("LLM_MODEL_MAINTENANCE_ENABLED", "true")
	probes := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		probes++
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"data": []map[string]string{{"id": "other-model"}}})
	}))
	defer server.Close()

	policy := Policy{
		LocalModelsAllowed: true,
		TierOrder:          []string{TierLocal},
		Providers: []Provider{{
			ID: "lm-studio", Name: "LM Studio", Enabled: true, Local: true, EndpointURL: server.URL,
			Models: []Model{{ID: "configured-model", Name: "Configured local model", Tier: TierLocal, Capabilities: []string{"general"}, MaxDifficulty: 5, MaxReasoning: "very_high", Enabled: true}},
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

func TestModelMaintenanceSkipsExternalWorkWhenPersistentLeaseIsHeld(t *testing.T) {
	t.Setenv("LLM_MODEL_MAINTENANCE_ENABLED", "true")
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		http.Error(w, "the competing process owns maintenance", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	policy := testPolicyWithoutEndpoints()
	policy.Providers[0].EndpointURL = server.URL
	policy.Providers[0].Models = []Model{{ID: "phi3:mini", Name: "Phi", Tier: TierLocal, Enabled: true}}
	history := &leasedModelMaintenanceRepository{
		fakeModelMaintenanceRepository: &fakeModelMaintenanceRepository{},
		acquired:                       false,
	}
	service := &Service{policy: policy, maintenanceHistory: history, maintenanceRunning: map[string]*sync.Mutex{}}

	result := service.ensureModelFresh(policy.Providers[0], policy.Providers[0].Models[0])
	if result.Status != "in_progress" || !result.BlocksExecution {
		t.Fatalf("maintenance result = %#v, want blocked in-progress result", result)
	}
	if calls != 0 {
		t.Fatalf("maintenance contacted the provider %d time(s) while another process held its lease", calls)
	}
	if len(history.records) != 0 {
		t.Fatalf("maintenance persisted %#v, want no misleading failure record", history.records)
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

func TestProbeAndRecordProvidersReportsCancellationInsteadOfPartialSuccess(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	repository := &cancellingContextProbeHistoryRepository{
		fakeProbeHistoryRepository: &fakeProbeHistoryRepository{},
		cancel:                     cancel,
	}
	service := &Service{
		policy:       Policy{Providers: []Provider{{ID: "test-provider", Name: "Test provider"}}},
		probeHistory: repository,
	}

	results, err := service.ProbeAndRecordProvidersWithContext(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("probe error = %v, want context cancellation", err)
	}
	if results != nil {
		t.Fatalf("cancelled probe returned partial success %#v", results)
	}
	if repository.recordCalls != 1 {
		t.Fatalf("context-aware history writes = %d, want 1", repository.recordCalls)
	}
}

func TestProbeAndRecordProvidersRejectsAlreadyCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	repository := &cancellingContextProbeHistoryRepository{
		fakeProbeHistoryRepository: &fakeProbeHistoryRepository{},
		cancel:                     cancel,
	}
	service := &Service{
		policy:       Policy{Providers: []Provider{{ID: "test-provider", Name: "Test provider"}}},
		probeHistory: repository,
	}

	results, err := service.ProbeAndRecordProvidersWithContext(ctx)
	if !errors.Is(err, context.Canceled) || results != nil {
		t.Fatalf("results/error = %#v/%v, want nil/context cancellation", results, err)
	}
	if repository.recordCalls != 0 {
		t.Fatalf("cancelled request reached history writer %d times", repository.recordCalls)
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

	for _, test := range []struct {
		checkedAt time.Time
		reason    string
	}{
		{checkedAt: time.Time{}, reason: "timestamp"},
		{checkedAt: now.Add(time.Minute), reason: "timestamp"},
		{checkedAt: now.Add(-5 * time.Minute), reason: "stale"},
	} {
		repository.probes = []models.LLMProviderProbe{{ProviderID: "ollama", Live: true, CheckedAt: test.checkedAt}}
		decision, err = service.Route(RouteRequest{Task: "Summarize this short note"})
		if err != nil {
			t.Fatalf("Route with provider-check timestamp %v: %v", test.checkedAt, err)
		}
		if decision.SelectedModelID != "" || !skippedReasonContains(decision.Skipped, "ollama", test.reason) {
			t.Fatalf("provider check with timestamp %v must not authorize routing: %#v", test.checkedAt, decision)
		}
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
	disableModelMaintenanceForTest(t)
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

func TestGenerateRequiresSuccessfulHTTPStatusAndDoesNotRetryProviderPosts(t *testing.T) {
	disableModelMaintenanceForTest(t)

	tests := []struct {
		name       string
		providerID string
		modelID    string
		status     int
		body       string
		wantPath   string
		wantSecret bool
	}{
		{
			name:       "Ollama redirect with success-shaped body",
			providerID: "ollama",
			modelID:    "phi3:mini",
			status:     http.StatusFound,
			body:       `{"response":"must not be accepted"}`,
			wantPath:   "/api/generate",
		},
		{
			name:       "OpenAI-compatible redirect with success-shaped body",
			providerID: "lm-studio",
			modelID:    "local-model",
			status:     http.StatusFound,
			body:       `{"choices":[{"message":{"content":"must not be accepted"}}]}`,
			wantPath:   "/v1/chat/completions",
		},
		{
			name:       "OpenAI-compatible transient failure is redacted and not retried",
			providerID: "lm-studio",
			modelID:    "local-model",
			status:     http.StatusServiceUnavailable,
			body:       `{"error":{"message":"authorization: Bearer super-secret-provider-token"}}`,
			wantPath:   "/v1/chat/completions",
			wantSecret: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var requests int
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				if r.URL.Path != test.wantPath {
					t.Errorf("request path = %q, want %q", r.URL.Path, test.wantPath)
				}
				w.Header().Set("Location", "https://provider-redirect.example.test/must-not-be-followed")
				w.WriteHeader(test.status)
				_, _ = io.WriteString(w, test.body)
			}))
			defer server.Close()

			policy := testPolicyWithoutEndpoints()
			index := providerIndex(t, policy, test.providerID)
			policy.Providers[index].EndpointURL = server.URL
			service := withTrustedTestFinalEffects(t, &Service{policy: policy})
			result, err := service.Generate(withTrustedTestEffect(GenerateRequest{
				Task: "Draft a short response",
				RouteDecision: &RouteDecision{
					SelectedProviderID: test.providerID,
					SelectedModelID:    test.modelID,
					Tier:               TierFree,
				},
			}))
			if err != nil {
				t.Fatalf("Generate: %v", err)
			}
			if result.Status != "failed" {
				t.Fatalf("status = %q, want failed for HTTP %d; result=%#v", result.Status, test.status, result)
			}
			if result.Output != "" {
				t.Fatalf("failed provider response exposed output %q", result.Output)
			}
			if !strings.Contains(result.Reason, fmt.Sprintf("HTTP %d", test.status)) {
				t.Fatalf("failure reason %q does not report HTTP %d", result.Reason, test.status)
			}
			if test.wantSecret && strings.Contains(result.Reason, "super-secret-provider-token") {
				t.Fatalf("provider error leaked a credential: %q", result.Reason)
			}
			if requests != 1 {
				t.Fatalf("provider POST was attempted %d times; want one non-retried attempt", requests)
			}
		})
	}
}

func TestGenerateCancelsAnInFlightProviderRequest(t *testing.T) {
	disableModelMaintenanceForTest(t)
	requestStarted := make(chan struct{}, 1)
	requestRelease := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(requestRelease) }) }
	defer release()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		requestStarted <- struct{}{}
		<-requestRelease
	}))
	defer server.Close()

	policy := testPolicyWithoutEndpoints()
	policy.Providers[1].EndpointURL = server.URL
	policy.Providers[1].Models[0].InputCostPerMillionTokensEUR = 3
	policy.Providers[1].Models[0].OutputCostPerMillionTokensEUR = 6
	history := &fakeGenerationHistoryRepository{}
	service := withTrustedTestFinalEffects(t, &Service{policy: policy, generationHistory: history})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	resultCh := make(chan *GenerationResult, 1)
	errCh := make(chan error, 1)
	go func() {
		result, err := service.Generate(withTrustedTestEffect(GenerateRequest{
			Task:                "draft a short note",
			CancellationContext: ctx,
			RouteDecision: &RouteDecision{
				SelectedProviderID: "lm-studio",
				SelectedModelID:    "local-model",
				SelectedModelName:  "Configured LM Studio local model",
				Tier:               TierFree,
			},
		}))
		resultCh <- result
		errCh <- err
	}()

	select {
	case <-requestStarted:
		cancel()
	case <-time.After(3 * time.Second):
		t.Fatal("provider generation request never started")
	}
	select {
	case result := <-resultCh:
		if err := <-errCh; err != nil {
			t.Fatalf("Generate returned error: %v", err)
		}
		release()
		if result == nil || result.Status != "cancelled" {
			t.Fatalf("cancelled generation result = %#v", result)
		}
		if result.ProviderID != "lm-studio" || result.ModelID != "local-model" || result.ModelName != "Configured LM Studio local model" {
			t.Fatalf("cancelled generation lost selected model identity: %#v", result)
		}
		if result.UsageSource != "estimated_uncertain" || result.InputTokens <= 0 || result.OutputTokens <= 0 || result.EstimatedCostEUR <= 0 {
			t.Fatalf("cancelled dispatched call did not expose uncertain estimated usage: %#v", result)
		}
		usage := service.Policy().Providers[1]
		if usage.InputTokensUsed != result.InputTokens || usage.OutputTokensUsed != result.OutputTokens || usage.BudgetUsedEUR != result.EstimatedCostEUR {
			t.Fatalf("cancelled dispatched call was not reflected in usage accounting: provider=%#v result=%#v", usage, result)
		}
		if len(history.records) != 1 || history.records[0].Status != "cancelled" || history.records[0].ProviderID != "lm-studio" || history.records[0].ModelID != "local-model" || history.records[0].ModelName != "Configured LM Studio local model" || history.records[0].UsageSource != "estimated_uncertain" || history.records[0].InputTokens != result.InputTokens || history.records[0].OutputTokens != result.OutputTokens {
			t.Fatalf("cancelled generation audit did not preserve selected model: %#v", history.records)
		}
	case <-time.After(3 * time.Second):
		release()
		t.Fatal("Generate did not stop after its request context was cancelled")
	}
}

func TestGenerateRejectsTruncatedResponsesWithoutRecordingUsage(t *testing.T) {
	disableModelMaintenanceForTest(t)
	for _, test := range []struct {
		name       string
		providerID string
		path       string
		body       string
	}{
		{
			name:       "ollama",
			providerID: "ollama",
			path:       "/api/generate",
			body:       `{"response":"partial draft","prompt_eval_count":11,"eval_count":4}`,
		},
		{
			name:       "openai-compatible",
			providerID: "lm-studio",
			path:       "/v1/chat/completions",
			body:       `{"choices":[{"message":{"content":"partial draft"}}],"usage":{"prompt_tokens":11,"completion_tokens":4}}`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != test.path {
					http.NotFound(w, r)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Content-Length", "2048")
				_, _ = w.Write([]byte(test.body))
			}))
			defer server.Close()

			policy := testPolicyWithoutEndpoints()
			providerIndex := providerIndex(t, policy, test.providerID)
			policy.Providers[providerIndex].EndpointURL = server.URL
			model := policy.Providers[providerIndex].Models[0]
			service := withTrustedTestFinalEffects(t, &Service{policy: policy})
			result, err := service.Generate(withTrustedTestEffect(GenerateRequest{
				Task: "draft a short note",
				RouteDecision: &RouteDecision{
					SelectedProviderID: test.providerID,
					SelectedModelID:    model.ID,
					SelectedModelName:  model.Name,
					Tier:               model.Tier,
				},
			}))
			if err != nil {
				t.Fatalf("Generate returned error: %v", err)
			}
			if result.Status != "failed" || !strings.Contains(result.Reason, "read ") {
				t.Fatalf("truncated generation result = %#v, want failed body read", result)
			}
			usage := service.Policy().Providers[providerIndex]
			if usage.InputTokensUsed != 0 || usage.OutputTokensUsed != 0 || usage.BudgetUsedEUR != 0 {
				t.Fatalf("truncated response recorded usage: %#v", usage)
			}
		})
	}
}

func TestAddUsageIfContextActiveDoesNotRecordAfterCancellation(t *testing.T) {
	service := &Service{usage: map[string]UsageCounter{}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if service.addUsageIfContextActive(ctx, "lm-studio", "local-model", 0.5, 12, 3) {
		t.Fatal("cancelled context was allowed to commit usage")
	}
	if got := service.usage["lm-studio"]; got != (UsageCounter{}) {
		t.Fatalf("provider usage after cancellation = %#v, want zero", got)
	}
}

type cancelWhenResponseBodyIsRead struct {
	reader   *strings.Reader
	cancel   context.CancelFunc
	canceled bool
}

func (r *cancelWhenResponseBodyIsRead) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	if n > 0 && !r.canceled {
		r.canceled = true
		r.cancel()
	}
	return n, err
}

func (*cancelWhenResponseBodyIsRead) Close() error { return nil }

type successfulProviderResponseCancellingTransport struct {
	cancel context.CancelFunc
}

func (t successfulProviderResponseCancellingTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	var body io.ReadCloser
	switch {
	case strings.HasSuffix(request.URL.Path, "/models"):
		body = io.NopCloser(strings.NewReader(`{"data":[{"id":"local-model"}]}`))
	case strings.HasSuffix(request.URL.Path, "/chat/completions"):
		body = &cancelWhenResponseBodyIsRead{reader: strings.NewReader(`{"choices":[{"message":{"content":"completed provider response"}}],"usage":{"prompt_tokens":11,"completion_tokens":4}}`), cancel: t.cancel}
	default:
		return nil, fmt.Errorf("unexpected provider endpoint %q", request.URL.Path)
	}
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: body, Request: request, ContentLength: -1}, nil
}

func TestGenerateAccountsSuccessfulProviderResponseWhenCancellationRacesBodyRead(t *testing.T) {
	t.Setenv("LLM_MODEL_MAINTENANCE_ENABLED", "false")
	policy := testPolicyWithoutEndpoints()
	index := providerIndex(t, policy, "lm-studio")
	policy.Providers[index].EndpointURL = "http://127.0.0.1:18080"
	policy.Providers[index].Models[0].InputCostPerMillionTokensEUR = 3
	policy.Providers[index].Models[0].OutputCostPerMillionTokensEUR = 6
	wantCost := estimateModelUsageCostEUR(policy.Providers[index].Models[0], 11, 4)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	originalTransport := providerHTTPClient.Transport
	providerHTTPClient.Transport = successfulProviderResponseCancellingTransport{cancel: cancel}
	defer func() { providerHTTPClient.Transport = originalTransport }()
	history := &fakeGenerationHistoryRepository{}
	telemetry := &fakeModelTelemetryRepository{}
	service := withTrustedTestFinalEffects(t, (&Service{policy: policy, generationHistory: history}).WithModelTelemetryRepository(telemetry))

	result, err := service.Generate(withTrustedTestEffect(GenerateRequest{
		Task:                "Draft a short answer",
		CancellationContext: ctx,
		RouteDecision:       &RouteDecision{SelectedProviderID: "lm-studio", SelectedModelID: "local-model", Tier: TierFree},
	}))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if result.Status != "cancelled" {
		t.Fatalf("generation result = %#v, want cancelled after successful response raced cancellation", result)
	}
	if result.InputTokens != 11 || result.OutputTokens != 4 || result.UsageSource != "provider_reported" || result.EstimatedCostEUR != wantCost {
		t.Fatalf("cancelled response lost provider-reported usage: %#v", result)
	}
	if result.GenerationID == "" || result.AuditStatus != "recorded" {
		t.Fatalf("response metadata does not identify the durable usage record: %#v", result)
	}
	encodedResult, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal response metadata: %v", err)
	}
	var responseMetadata GenerationResult
	if err := json.Unmarshal(encodedResult, &responseMetadata); err != nil {
		t.Fatalf("decode response metadata: %v", err)
	}
	if responseMetadata.Status != "cancelled" || responseMetadata.InputTokens != 11 || responseMetadata.OutputTokens != 4 || responseMetadata.EstimatedCostEUR != wantCost || responseMetadata.UsageSource != "provider_reported" || responseMetadata.GenerationID != result.GenerationID {
		t.Fatalf("serialized response metadata lost actual provider usage: %#v", responseMetadata)
	}
	service.mu.Lock()
	processUsage := service.usage["lm-studio"]
	service.mu.Unlock()
	if processUsage.InputTokensUsed != 11 || processUsage.OutputTokensUsed != 4 || processUsage.BudgetUsedEUR != wantCost {
		t.Fatalf("successful provider response was counted more than once in process usage: %#v", processUsage)
	}
	usage := service.Policy().Providers[index]
	if usage.InputTokensUsed != 11 || usage.OutputTokensUsed != 4 || usage.BudgetUsedEUR != wantCost {
		t.Fatalf("durable daily usage does not contain exactly one successful provider response: %#v", usage)
	}
	if len(history.records) != 1 || history.records[0].Status != "cancelled" || history.records[0].InputTokens != 11 || history.records[0].OutputTokens != 4 || history.records[0].EstimatedCostEUR != wantCost || history.records[0].UsageSource != "provider_reported" {
		t.Fatalf("durable generation record lost successful provider usage: %#v", history.records)
	}
	if len(telemetry.rows) != 1 || telemetry.rows[0].InputTokens != 11 || telemetry.rows[0].OutputTokens != 4 || telemetry.rows[0].EstimatedCostEUR != wantCost {
		t.Fatalf("model telemetry lost successful provider usage: %#v", telemetry.rows)
	}
}

func TestGenerateFallsBackFromNegativeProviderTokenCounts(t *testing.T) {
	disableModelMaintenanceForTest(t)
	for _, test := range []struct {
		name       string
		providerID string
		path       string
		body       string
	}{
		{
			name:       "ollama",
			providerID: "ollama",
			path:       "/api/generate",
			body:       `{"response":"ollama draft","prompt_eval_count":-12,"eval_count":-4}`,
		},
		{
			name:       "openai-compatible",
			providerID: "lm-studio",
			path:       "/v1/chat/completions",
			body:       `{"choices":[{"message":{"content":"openai compatible draft"}}],"usage":{"prompt_tokens":-12,"completion_tokens":-4}}`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != test.path {
					http.NotFound(w, r)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, test.body)
			}))
			defer server.Close()

			policy := testPolicyWithoutEndpoints()
			index := providerIndex(t, policy, test.providerID)
			policy.Providers[index].EndpointURL = server.URL
			model := policy.Providers[index].Models[0]
			service := withTrustedTestFinalEffects(t, &Service{policy: policy})
			result, err := service.Generate(withTrustedTestEffect(GenerateRequest{
				Task: "draft a short note",
				RouteDecision: &RouteDecision{
					SelectedProviderID: test.providerID,
					SelectedModelID:    model.ID,
					SelectedModelName:  model.Name,
					Tier:               model.Tier,
				},
			}))
			if err != nil {
				t.Fatalf("Generate: %v", err)
			}
			if result.Status != "completed" || result.InputTokens <= 0 || result.OutputTokens <= 0 || result.UsageSource != "estimated" {
				t.Fatalf("negative provider counts were not replaced by estimates: %#v", result)
			}
			usage := service.Policy().Providers[index]
			if usage.InputTokensUsed < 0 || usage.OutputTokensUsed < 0 {
				t.Fatalf("negative provider counts corrupted usage totals: %#v", usage)
			}
		})
	}
}

func TestDailyUsageIsDurableAndScopedToUTCDate(t *testing.T) {
	policy := testPolicyWithoutEndpoints()
	history := &fakeGenerationHistoryRepository{records: []models.LLMGenerationRecord{
		{ProviderID: "lm-studio", ModelID: "local-model", Status: "completed", EstimatedCostEUR: 0.2, InputTokens: 90, OutputTokens: 30, UsageSource: "provider_reported", LoggedAt: time.Date(2026, 9, 24, 23, 59, 59, 0, time.UTC)},
		{ProviderID: "lm-studio", ModelID: "local-model", Status: "cancelled", EstimatedCostEUR: 0.5, InputTokens: 17, OutputTokens: 8, UsageSource: "estimated_uncertain", LoggedAt: time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC)},
		{ProviderID: "lm-studio", ModelID: "local-model", Status: "failed", EstimatedCostEUR: 9, InputTokens: 99, OutputTokens: 99, UsageSource: "", LoggedAt: time.Date(2026, 9, 25, 1, 0, 0, 0, time.UTC)},
	}}
	now := func() time.Time { return time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC) }
	service := &Service{policy: policy, usage: map[string]UsageCounter{}, generationHistory: history, now: now}
	first := service.Policy()
	providerIndex := providerIndex(t, first, "lm-studio")
	provider := first.Providers[providerIndex]
	if first.UsageAccountingStatus != "durable" || first.UsageTimezone != "UTC" || first.UsagePeriodStart != time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC) {
		t.Fatalf("usage provenance = status %q timezone %q start %s", first.UsageAccountingStatus, first.UsageTimezone, first.UsagePeriodStart)
	}
	if provider.InputTokensUsed != 17 || provider.OutputTokensUsed != 8 || provider.BudgetUsedEUR != 0.5 {
		t.Fatalf("daily usage = %#v, want only current UTC-day accepted and estimated usage", provider)
	}

	// A new API service with the same durable repository must see the same day totals.
	restarted := &Service{policy: policy, usage: map[string]UsageCounter{}, generationHistory: history, now: now}
	restartedPolicy := restarted.Policy()
	if restartedPolicy.Providers[providerIndex].InputTokensUsed != 17 || restartedPolicy.Providers[providerIndex].OutputTokensUsed != 8 || restartedPolicy.DailyBudgetUsedEUR != 0.5 {
		t.Fatalf("usage did not survive service reconstruction: %#v", restartedPolicy)
	}

	restarted.now = func() time.Time { return time.Date(2026, 9, 26, 0, 0, 0, 0, time.UTC) }
	newDay := restarted.Policy()
	if newDay.UsageAccountingStatus != "durable" || newDay.Providers[providerIndex].InputTokensUsed != 0 || newDay.Providers[providerIndex].OutputTokensUsed != 0 || newDay.DailyBudgetUsedEUR != 0 {
		t.Fatalf("usage was not reset at the next UTC day: %#v", newDay)
	}
}

func TestUsageAccountingLabelsProcessFallbackAndRepositoryFailure(t *testing.T) {
	policy := testPolicyWithoutEndpoints()
	service := &Service{policy: policy, usage: map[string]UsageCounter{}, now: func() time.Time { return time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC) }}
	service.addUsage("lm-studio", "local-model", 0.25, 4, 2)
	if got := service.Policy().UsageAccountingStatus; got != "process_only" {
		t.Fatalf("usage status = %q, want process_only without durable history", got)
	}

	history := &fakeGenerationHistoryRepository{err: errors.New("database unavailable")}
	service.generationHistory = history
	loaded := service.Policy()
	if loaded.UsageAccountingStatus != "unavailable" {
		t.Fatalf("usage status = %q, want unavailable when durable aggregation fails", loaded.UsageAccountingStatus)
	}
	index := providerIndex(t, loaded, "lm-studio")
	if loaded.Providers[index].InputTokensUsed != 4 || loaded.Providers[index].OutputTokensUsed != 2 {
		t.Fatalf("unavailable durable query discarded process fallback without marking data: %#v", loaded.Providers[index])
	}
}

func TestLlamaCPPProviderProbesAndGeneratesThroughOpenAICompatibleAPI(t *testing.T) {
	disableModelMaintenanceForTest(t)
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
	disableModelMaintenanceForTest(t)
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
	disableModelMaintenanceForTest(t)
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

func TestSGLangProviderProbeDoesNotAdmitUnverifiableModelVersion(t *testing.T) {
	var generationCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"data": []map[string]string{{"id": "qwen-sglang"}}})
		case "/v1/chat/completions":
			generationCalls.Add(1)
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
	if result.Status != "skipped" || generationCalls.Load() != 0 {
		t.Fatalf("generation result = %#v, calls=%d; unsupported model version must not be used", result, generationCalls.Load())
	}
	history, err := service.ModelMaintenanceHistory(10)
	if err != nil || len(history) != 1 || history[0].ProviderID != "sglang" || history[0].ModelID != "qwen-sglang" || history[0].Status != "operator_managed" || !history[0].BlocksExecution || !strings.Contains(history[0].Reason, "cannot verify its upstream version") {
		t.Fatalf("SGLang daily maintenance history = %#v, err=%v", history, err)
	}
}

func TestMistralRSProviderProbesAndGeneratesThroughOpenAICompatibleAPI(t *testing.T) {
	disableModelMaintenanceForTest(t)
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
	disableModelMaintenanceForTest(t)
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
	disableModelMaintenanceForTest(t)
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
	policy := testPolicyWithoutEndpoints()
	paidIndex := providerIndex(t, policy, "paid-provider")
	policy.Providers[paidIndex].Enabled = true
	policy.Providers[paidIndex].EndpointURL = "http://example.invalid"
	policy.PaidCallsAllowed = true
	policy.DailyPaidBudgetEUR = 1
	service := &Service{policy: policy}

	result, err := service.Generate(GenerateRequest{
		Task: "Handle a difficult verification task",
		RouteDecision: &RouteDecision{
			SelectedProviderID: "paid-provider",
			SelectedModelID:    "paid-high-capability",
			SelectedModelName:  "Paid high capability model",
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

func TestProviderProbeTimeoutIsBounded(t *testing.T) {
	for _, test := range []struct {
		name  string
		value string
		want  time.Duration
	}{
		{name: "default", value: "", want: 5 * time.Second},
		{name: "minimum", value: "0", want: time.Second},
		{name: "maximum", value: "31", want: 30 * time.Second},
		{name: "malformed", value: "invalid", want: 5 * time.Second},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("LLM_PROVIDER_PROBE_TIMEOUT_SECONDS", test.value)
			if got := providerProbeTimeout(); got != test.want {
				t.Fatalf("provider probe timeout = %s; want %s", got, test.want)
			}
		})
	}
}

func TestOdysseusProbeUsesOneTimeoutAcrossFallbackHealthPaths(t *testing.T) {
	t.Setenv("LLM_PROVIDER_PROBE_TIMEOUT_SECONDS", "1")
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		<-r.Context().Done()
	}))
	defer server.Close()

	result := probeProviderWithContext(context.Background(), Provider{
		ID: "odysseus", Name: "Odysseus", Enabled: true, EndpointURL: server.URL,
	}, Policy{})
	if result.Status != "failed" || !strings.Contains(strings.ToLower(result.Reason), "timed out") {
		t.Fatalf("Odysseus timeout result = %#v; want bounded timeout", result)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("Odysseus fallback made %d requests after the total timeout; want one", got)
	}
}

func TestGenerateBlocksOdysseusExecutionEvenWhenInternallyApproved(t *testing.T) {
	disableModelMaintenanceForTest(t)
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
	disableModelMaintenanceForTest(t)
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
	disableModelMaintenanceForTest(t)
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
	disableModelMaintenanceForTest(t)
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

func TestPaidGenerationReservesDurableBudgetBeforeProviderDispatch(t *testing.T) {
	disableModelMaintenanceForTest(t)
	var providerCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		providerCalls.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]string{"content": "paid draft"}}},
			"usage":   map[string]int{"prompt_tokens": 30, "completion_tokens": 12},
		})
	}))
	defer server.Close()

	policy := testPolicyWithoutEndpoints()
	policy.PaidCallsAllowed = true
	policy.DailyPaidBudgetEUR = 1
	paidIndex := providerIndex(t, policy, "paid-provider")
	policy.Providers[paidIndex].Enabled = true
	policy.Providers[paidIndex].EndpointURL = server.URL
	policy.Providers[paidIndex].Models = []Model{{
		ID: "paid-budget-test", Name: "Paid budget test", Tier: TierCheap, Enabled: true,
		RequiresApproval: true, EstimatedCostEUR: 0.60, MaxDifficulty: 5, MaxReasoning: "very_high",
	}}
	history := &fakeGenerationHistoryRepository{}
	service := withTrustedTestFinalEffects(t, &Service{policy: policy, generationHistory: history})

	generate := func() *GenerationResult {
		result, err := service.Generate(withTrustedTestEffect(GenerateRequest{
			Task: "Draft a short approved response", MaxTokens: 100,
			RouteDecision: &RouteDecision{
				SelectedProviderID: "paid-provider", SelectedModelID: "paid-budget-test",
				SelectedModelName: "Paid budget test", Tier: TierCheap,
			},
		}))
		if err != nil {
			t.Fatalf("Generate: %v", err)
		}
		return result
	}

	first := generate()
	if first.Status != "completed" || first.AuditStatus != "recorded" || first.GenerationID == "" {
		t.Fatalf("first paid generation = %#v, want completed and durably recorded", first)
	}
	second := generate()
	if second.Status != "blocked" || !strings.Contains(second.Reason, ErrPaidBudgetExceeded.Error()) {
		t.Fatalf("second paid generation = %#v, want a daily budget block", second)
	}
	if got := providerCalls.Load(); got != 1 {
		t.Fatalf("provider calls = %d after exhausting the budget, want 1", got)
	}
	if len(history.records) != 2 || history.records[0].Status != "completed" || history.records[0].ID.String() != first.GenerationID {
		t.Fatalf("generation ledger = %#v; want one finalized reservation and one blocked audit row", history.records)
	}
}

func TestPaidGenerationFailsClosedWithoutDurableReservationStore(t *testing.T) {
	disableModelMaintenanceForTest(t)
	var providerCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		providerCalls.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []map[string]any{{"message": map[string]string{"content": "must not run"}}}})
	}))
	defer server.Close()

	policy := testPolicyWithoutEndpoints()
	policy.PaidCallsAllowed = true
	policy.DailyPaidBudgetEUR = 1
	paidIndex := providerIndex(t, policy, "paid-provider")
	policy.Providers[paidIndex].Enabled = true
	policy.Providers[paidIndex].EndpointURL = server.URL
	service := withTrustedTestFinalEffects(t, &Service{policy: policy})
	result, err := service.Generate(withTrustedTestEffect(GenerateRequest{
		Task: "Draft an approved response",
		RouteDecision: &RouteDecision{
			SelectedProviderID: "paid-provider", SelectedModelID: "paid-high-capability",
			SelectedModelName: "Paid high capability model", Tier: TierExpensive,
		},
	}))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if result.Status != "blocked" || !strings.Contains(result.Reason, ErrPaidBudgetUnavailable.Error()) {
		t.Fatalf("paid generation = %#v, want durable-accounting block", result)
	}
	if got := providerCalls.Load(); got != 0 {
		t.Fatalf("provider calls = %d without durable reservation storage, want 0", got)
	}
}

func TestGenerateWithholdsOutputWhenAuditHistoryCannotBeWritten(t *testing.T) {
	disableModelMaintenanceForTest(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]string{"content": "unrecorded draft"}}},
			"usage":   map[string]int{"prompt_tokens": 9, "completion_tokens": 4},
		})
	}))
	defer server.Close()

	policy := testPolicyWithoutEndpoints()
	policy.Providers[1].EndpointURL = server.URL
	history := &fakeGenerationHistoryRepository{err: errors.New("history store unavailable")}
	service := withTrustedTestFinalEffects(t, &Service{policy: policy, generationHistory: history})
	result, err := service.Generate(withTrustedTestEffect(GenerateRequest{
		Task: "Draft a local-only answer",
		RouteDecision: &RouteDecision{
			SelectedProviderID: "lm-studio", SelectedModelID: "local-model",
			SelectedModelName: "Configured LM Studio local model", Tier: TierLocal,
		},
	}))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if result.Status != "failed" || result.Output != "" || result.AuditStatus != "record_failed" || !strings.Contains(result.Reason, "output was withheld") {
		t.Fatalf("generation result = %#v; failed audit persistence must withhold the output", result)
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

type cancellingContextProbeHistoryRepository struct {
	*fakeProbeHistoryRepository
	cancel      context.CancelFunc
	recordCalls int
}

func (r *cancellingContextProbeHistoryRepository) RecordProviderProbeWithContext(ctx context.Context, _ *models.LLMProviderProbe) (*models.LLMProviderProbe, error) {
	r.recordCalls++
	r.cancel()
	return nil, ctx.Err()
}

func (r *cancellingContextProbeHistoryRepository) FindRecentProviderProbesWithContext(ctx context.Context, limit int) ([]models.LLMProviderProbe, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return r.fakeProbeHistoryRepository.FindRecentProviderProbes(limit)
}

func (r *cancellingContextProbeHistoryRepository) FindLatestProviderProbeWithContext(ctx context.Context, providerID string) (*models.LLMProviderProbe, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return r.fakeProbeHistoryRepository.FindLatestProviderProbe(providerID)
}

type fakeModelMaintenanceRepository struct {
	records              []models.LLMModelMaintenance
	mu                   sync.Mutex
	admissionMu          sync.Mutex
	admissionClaims      map[string]ModelMaintenanceAdmissionClaim
	admissionWriteErr    error
	admissionFinalizeErr error
}

type leasedModelMaintenanceRepository struct {
	*fakeModelMaintenanceRepository
	acquired bool
	err      error
	releases int
}

func (r *leasedModelMaintenanceRepository) AcquireModelMaintenanceLease(_ context.Context, _, _ string) (func(), bool, error) {
	return func() { r.releases++ }, r.acquired, r.err
}

type fakeGenerationHistoryRepository struct {
	records []models.LLMGenerationRecord
	err     error
}

func (r *fakeGenerationHistoryRepository) UsageBetween(start, end time.Time) ([]GenerationUsageAggregate, error) {
	if r.err != nil {
		return nil, r.err
	}
	byKey := map[string]GenerationUsageAggregate{}
	for _, record := range r.records {
		loggedAt := record.LoggedAt.UTC()
		if loggedAt.Before(start.UTC()) || !loggedAt.Before(end.UTC()) {
			continue
		}
		switch record.UsageSource {
		case "provider_reported", "provider_reported_partial", "estimated", "estimated_uncertain":
		default:
			continue
		}
		key := usageKey(record.ProviderID, record.ModelID)
		aggregate := byKey[key]
		aggregate.ProviderID = record.ProviderID
		aggregate.ModelID = record.ModelID
		aggregate.BudgetUsedEUR += record.EstimatedCostEUR
		aggregate.InputTokensUsed += record.InputTokens
		aggregate.OutputTokensUsed += record.OutputTokens
		byKey[key] = aggregate
	}
	result := make([]GenerationUsageAggregate, 0, len(byKey))
	for _, aggregate := range byKey {
		result = append(result, aggregate)
	}
	return result, nil
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

func (r *fakeGenerationHistoryRepository) ReservePaidGeneration(ctx context.Context, record *models.LLMGenerationRecord, dailyLimitEUR float64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if r.err != nil {
		return fmt.Errorf("reserve paid generation: %w", r.err)
	}
	start := utcDayStart(record.LoggedAt)
	usage, err := r.UsageBetween(start, start.Add(24*time.Hour))
	if err != nil {
		return err
	}
	var usedEUR float64
	for _, aggregate := range usage {
		usedEUR += aggregate.BudgetUsedEUR
	}
	if usedEUR+record.EstimatedCostEUR > dailyLimitEUR {
		return fmt.Errorf("%w: used %.6f EUR, requested %.6f EUR, limit %.6f EUR", ErrPaidBudgetExceeded, usedEUR, record.EstimatedCostEUR, dailyLimitEUR)
	}
	copy := *record
	r.records = append(r.records, copy)
	return nil
}

func (r *fakeGenerationHistoryRepository) FinalizePaidGeneration(record *models.LLMGenerationRecord) error {
	if r.err != nil {
		return r.err
	}
	for index := range r.records {
		if r.records[index].ID != record.ID {
			continue
		}
		loggedAt := r.records[index].LoggedAt
		r.records[index] = *record
		r.records[index].LoggedAt = loggedAt
		return nil
	}
	return errors.New("paid generation reservation not found")
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
	r.mu.Lock()
	r.records = append(r.records, copy)
	r.mu.Unlock()
	return &copy, nil
}

func (r *fakeModelMaintenanceRepository) RecordModelMaintenanceWithContext(ctx context.Context, record *models.LLMModelMaintenance) (*models.LLMModelMaintenance, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return r.RecordModelMaintenance(record)
}

func (r *fakeModelMaintenanceRepository) FindLatestModelMaintenance(providerID, modelID string) (*models.LLMModelMaintenance, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var latest *models.LLMModelMaintenance
	for index := range r.records {
		record := r.records[index]
		if record.ProviderID == providerID && record.ModelID == modelID && (latest == nil || record.CheckedAt.After(latest.CheckedAt)) {
			latest = &record
		}
	}
	return latest, nil
}

func (r *fakeModelMaintenanceRepository) FindLatestModelMaintenanceWithContext(ctx context.Context, providerID, modelID string) (*models.LLMModelMaintenance, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return r.FindLatestModelMaintenance(providerID, modelID)
}

func (r *fakeModelMaintenanceRepository) FindRecentModelMaintenance(limit int) ([]models.LLMModelMaintenance, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if limit <= 0 || limit > len(r.records) {
		limit = len(r.records)
	}
	results := make([]models.LLMModelMaintenance, 0, limit)
	for index := len(r.records) - 1; index >= 0 && len(results) < limit; index-- {
		results = append(results, r.records[index])
	}
	return results, nil
}

func (r *fakeModelMaintenanceRepository) FindRecentModelMaintenanceWithContext(ctx context.Context, limit int) ([]models.LLMModelMaintenance, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return r.FindRecentModelMaintenance(limit)
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
