package modelintelligence

import "testing"

func TestOnlyOllamaAcceptsCanonicalComposeInternalService(t *testing.T) {
	if err := validateLocalEndpointURLForProvider("http://ollama-local:11434", "ollama"); err != nil {
		t.Fatalf("canonical Compose-internal Ollama endpoint rejected: %v", err)
	}
	for _, providerID := range []string{"", "lm-studio", "llama-cpp", "localai", "vllm"} {
		if err := validateLocalEndpointURLForProvider("http://ollama-local:11434", providerID); err == nil {
			t.Fatalf("provider %q accepted Ollama's private service name", providerID)
		}
	}
	for _, endpoint := range []string{"http://ollama:11434", "https://ollama.example.test"} {
		if err := validateLocalEndpointURLForProvider(endpoint, "ollama"); err == nil {
			t.Fatalf("arbitrary Ollama hostname accepted: %q", endpoint)
		}
	}
}
