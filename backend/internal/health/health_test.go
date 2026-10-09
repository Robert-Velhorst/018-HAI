package health

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"automation-hub-backend/internal/config"
)

func TestPostgresProbePropagatesCancellationBeforeOpening(t *testing.T) {
	// No database or DNS is contacted. Invalid pool configuration ensures that
	// cancellation, not a later constructor failure, is observed first.
	t.Setenv("DB_MAX_OPEN_CONNS", "0")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := PostgresProbe(config.Configuration{
		DbUser: "synthetic", DbPassword: "unused", DbName: "synthetic",
		DbHost: "must-not-resolve.invalid", DbPort: 5432,
	}).Run(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("probe ignored caller cancellation: %v", err)
	}
}

func TestLLMProviderProbeUsesConfiguredLocalAIEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			t.Fatalf("path = %q, want root probe", r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	clearLLMProviderEnv(t)
	t.Setenv("LOCALAI_BASE_URL", server.URL)

	if err := LLMProviderProbe().Run(context.Background()); err != nil {
		t.Fatalf("LocalAI health probe failed: %v", err)
	}
}

func TestLLMProviderProbeUsesConfiguredVLLMEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	clearLLMProviderEnv(t)
	t.Setenv("VLLM_BASE_URL", server.URL)

	if err := LLMProviderProbe().Run(context.Background()); err != nil {
		t.Fatalf("vLLM health probe failed: %v", err)
	}
}

func TestLLMProviderProbeUsesConfiguredMistralRSEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	clearLLMProviderEnv(t)
	t.Setenv("MISTRAL_RS_BASE_URL", server.URL)

	if err := LLMProviderProbe().Run(context.Background()); err != nil {
		t.Fatalf("mistral.rs health probe failed: %v", err)
	}
}

func TestLLMProviderProbeUsesConfiguredLMStudioEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	clearLLMProviderEnv(t)
	t.Setenv("LM_STUDIO_BASE_URL", server.URL)

	if err := LLMProviderProbe().Run(context.Background()); err != nil {
		t.Fatalf("LM Studio health probe failed: %v", err)
	}
}

func TestLLMProviderProbeUsesConfiguredSGLangEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	clearLLMProviderEnv(t)
	t.Setenv("SGLANG_BASE_URL", server.URL)

	if err := LLMProviderProbe().Run(context.Background()); err != nil {
		t.Fatalf("SGLang health probe failed: %v", err)
	}
}

func TestLLMProviderProbeUsesConfiguredDSparkEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	clearLLMProviderEnv(t)
	t.Setenv("DSPARK_BASE_URL", server.URL)
	t.Setenv("DSPARK_ENABLED", "true")

	if err := LLMProviderProbe().Run(context.Background()); err != nil {
		t.Fatalf("DSpark health probe failed: %v", err)
	}
}

func TestConfiguredLLMProviderEndpointIgnoresDisabledDSpark(t *testing.T) {
	clearLLMProviderEnv(t)
	t.Setenv("DSPARK_BASE_URL", "http://127.0.0.1:9100")

	endpoint, label := configuredLLMProviderEndpoint()
	if endpoint != "" || label != "" {
		t.Fatalf("disabled DSpark selected as provider: endpoint=%q label=%q", endpoint, label)
	}
}

func clearLLMProviderEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		"OLLAMA_BASE_URL", "LLAMA_CPP_BASE_URL", "LM_STUDIO_BASE_URL",
		"LOCALAI_BASE_URL", "VLLM_BASE_URL", "SGLANG_BASE_URL", "DSPARK_BASE_URL",
		"MISTRAL_RS_BASE_URL", "DSPARK_ENABLED", "LITELLM_ENABLED", "LITELLM_BASE_URL",
		"FREE_CLOUD_OPENAI_BASE_URL",
	} {
		t.Setenv(name, "")
	}
}
