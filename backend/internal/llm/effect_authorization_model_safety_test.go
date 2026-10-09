package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func modelPullSafetyEffectContext() *EffectContext {
	return &EffectContext{
		OwnerIdentity:         "owner:model-pull-safety-test",
		ActorIdentity:         "actor:model-pull-safety-test",
		ActorKind:             "system",
		TaskID:                "task:model-pull-safety-test",
		ProjectKey:            "project:model-pull-safety-test",
		ApprovalSourceID:      "approval:model-pull-safety-test",
		ApprovalBindingDigest: strings.Repeat("b", 64),
	}
}

func TestFinalModelPullAuthorizationMatchesImplementedProviderCapabilities(t *testing.T) {
	tests := []struct {
		name        string
		provider    Provider
		modelID     string
		wantAllowed bool
		wantError   string
	}{
		{
			name:        "normal Ollama local model",
			provider:    Provider{ID: "ollama", Local: true},
			modelID:     "qwen2.5:7b",
			wantAllowed: true,
		},
		{
			name:        "isolated Ollama local model",
			provider:    Provider{ID: miniSWEOllamaProviderID, Local: true},
			modelID:     "qwen2.5:7b",
			wantAllowed: true,
		},
		{
			name:      "OpenAI-compatible local runtime has no installer",
			provider:  Provider{ID: "lm-studio", Local: true},
			modelID:   "qwen2.5:7b",
			wantError: "not implemented",
		},
		{
			name:      "llama.cpp has no installer",
			provider:  Provider{ID: "llama-cpp", Local: true},
			modelID:   "qwen2.5:7b",
			wantError: "not implemented",
		},
		{
			name:      "LocalAI has no installer",
			provider:  Provider{ID: "localai", Local: true},
			modelID:   "qwen2.5:7b",
			wantError: "not implemented",
		},
		{
			name:      "vLLM has no installer",
			provider:  Provider{ID: "vllm", Local: true},
			modelID:   "qwen2.5:7b",
			wantError: "not implemented",
		},
		{
			name:      "SGLang has no installer",
			provider:  Provider{ID: "sglang", Local: true},
			modelID:   "qwen2.5:7b",
			wantError: "not implemented",
		},
		{
			name:      "mistral.rs has no installer",
			provider:  Provider{ID: "mistral-rs", Local: true},
			modelID:   "qwen2.5:7b",
			wantError: "not implemented",
		},
		{
			name:      "DSpark has no installer",
			provider:  Provider{ID: "dspark", Local: true},
			modelID:   "qwen2.5:7b",
			wantError: "not implemented",
		},
		{
			name:      "LiteLLM has no installer",
			provider:  Provider{ID: "litellm", Local: true},
			modelID:   "qwen2.5:7b",
			wantError: "not implemented",
		},
		{
			name:      "custom OpenAI-compatible runtime has no installer",
			provider:  Provider{ID: "custom-openai-compatible", Local: true},
			modelID:   "qwen2.5:7b",
			wantError: "not implemented",
		},
		{
			name:      "Odysseus workspace health is not a model installer",
			provider:  Provider{ID: "odysseus", Local: true},
			modelID:   "odysseus-workspace-agent",
			wantError: "not implemented",
		},
		{
			name:      "remote Ollama provider cannot install a local model",
			provider:  Provider{ID: "ollama", Local: false},
			modelID:   "qwen2.5:7b",
			wantError: "require a local",
		},
		{
			name:      "Ollama cloud model is not a local install artifact",
			provider:  Provider{ID: "ollama", Local: true},
			modelID:   "qwen3-coder:480b-cloud",
			wantError: "cloud-hosted",
		},
		{
			name:      "isolated Ollama cloud model is not a local install artifact",
			provider:  Provider{ID: miniSWEOllamaProviderID, Local: true},
			modelID:   "gpt-oss:120b-cloud",
			wantError: "cloud-hosted",
		},
		{
			name:      "Ollama cloud suffix is case insensitive",
			provider:  Provider{ID: "ollama", Local: true},
			modelID:   "qwen3-coder:latest-CLOUD",
			wantError: "cloud-hosted",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request, err := buildFinalEffectAuthorizationRequest(
				EffectOperationModelPull,
				modelPullSafetyEffectContext(),
				test.provider,
				Model{ID: test.modelID, Tier: TierLocal},
				"http://127.0.0.1:11434",
				0,
				nil,
				[]byte(`{"name":"`+test.modelID+`"}`),
				"test-configuration",
			)
			if test.wantAllowed {
				if err != nil {
					t.Fatalf("implemented local pull was rejected: %v", err)
				}
				if request.Operation != EffectOperationModelPull || request.ProviderID != test.provider.ID || request.ModelID != test.modelID {
					t.Fatalf("authorization request lost its provider/model binding: %#v", request)
				}
				return
			}
			if err == nil {
				t.Fatalf("unsupported model-pull capability was authorized: %#v", request)
			}
			if !strings.Contains(strings.ToLower(err.Error()), test.wantError) {
				t.Fatalf("error = %q, want it to explain %q", err, test.wantError)
			}
		})
	}
}

func TestOllamaCloudModelRefreshNeverCallsPullOrFinalAuthorizer(t *testing.T) {
	pulls := 0
	authorizations := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/tags":
			_ = json.NewEncoder(w).Encode(map[string]any{"models": []any{}})
		case "/api/pull":
			pulls++
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "success"})
		default:
			t.Fatalf("unexpected Ollama path: %s", request.URL.Path)
		}
	}))
	defer server.Close()

	service := (&Service{}).WithFinalEffectAuthorization(
		FinalEffectAuthorizerFunc(func(context.Context, FinalEffectAuthorizationRequest) error {
			authorizations++
			return nil
		}),
		EmergencyStopEvaluatorFunc(func(context.Context) (EmergencyStopState, error) {
			return EmergencyStopState{}, nil
		}),
	)
	provider := Provider{ID: "ollama", Name: "Ollama", EndpointURL: server.URL, Enabled: true, Local: true}
	model := Model{ID: "qwen3-coder:480b-cloud", Name: "Configured Ollama local model", Enabled: true, Tier: TierLocal}

	result := service.refreshOllamaModel(context.Background(), provider, model, "test-fingerprint", false, modelPullSafetyEffectContext())
	if result.Status != "failed" || !result.BlocksExecution || result.UpdateAttempted || result.UpdateApplied {
		t.Fatalf("cloud model was not safely blocked: %#v", result)
	}
	if !strings.Contains(strings.ToLower(result.Reason), "cloud") {
		t.Fatalf("block reason does not explain cloud model restriction: %q", result.Reason)
	}
	if pulls != 0 || authorizations != 0 {
		t.Fatalf("cloud model reached a pull effect: pulls=%d authorizations=%d", pulls, authorizations)
	}
}
