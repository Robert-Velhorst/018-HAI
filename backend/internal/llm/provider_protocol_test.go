package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestGenerationRejectsUnexpectedStreamingProtocolWithoutWaitingForEOF(t *testing.T) {
	disableModelMaintenanceForTest(t)
	t.Setenv("LLM_GENERATION_TIMEOUT_SECONDS", "30")

	tests := []struct {
		name       string
		providerID string
		modelID    string
		path       string
		keyEnv     string
	}{
		{name: "Ollama", providerID: "ollama", modelID: "phi3:mini", path: "/api/generate", keyEnv: "HAI_TEST_OLLAMA_KEY"},
		{name: "OpenAI-compatible", providerID: "lm-studio", modelID: "local-model", path: "/v1/chat/completions", keyEnv: "HAI_TEST_OPENAI_KEY"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			const credential = "provider-test-credential-do-not-log"
			t.Setenv(test.keyEnv, credential)
			requestStarted := make(chan struct{}, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != test.path {
					t.Errorf("request path = %q, want %q", r.URL.Path, test.path)
				}
				if got := r.Header.Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
					t.Errorf("request content type = %q, want application/json", got)
				}
				if got := r.Header.Get("Authorization"); got != "Bearer "+credential {
					t.Errorf("authorization = %q, want configured bearer credential", got)
				}
				var payload map[string]any
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Errorf("decode generation request: %v", err)
				}
				if test.providerID == "ollama" {
					if payload["stream"] != false {
						t.Errorf("Ollama stream = %#v, want false", payload["stream"])
					}
				} else if payload["stream"] == true {
					t.Error("OpenAI-compatible request opted into streaming")
				}

				w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
				w.WriteHeader(http.StatusOK)
				if flusher, ok := w.(http.Flusher); ok {
					flusher.Flush()
				}
				requestStarted <- struct{}{}
				_, _ = io.WriteString(w, "data: {\"token\":\""+credential+"\"}\n\n")
				if flusher, ok := w.(http.Flusher); ok {
					flusher.Flush()
				}
				<-r.Context().Done()
			}))
			defer server.Close()

			policy := testPolicyWithoutEndpoints()
			index := providerIndex(t, policy, test.providerID)
			policy.Providers[index].EndpointURL = server.URL
			policy.Providers[index].APIKeyEnv = test.keyEnv
			service := withTrustedTestFinalEffects(t, &Service{policy: policy})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			resultCh := make(chan *GenerationResult, 1)
			errCh := make(chan error, 1)
			go func() {
				result, err := service.Generate(withTrustedTestEffect(GenerateRequest{
					Task:                "Draft a response",
					CancellationContext: ctx,
					RouteDecision: &RouteDecision{
						SelectedProviderID: test.providerID,
						SelectedModelID:    test.modelID,
						Tier:               TierFree,
					},
				}))
				resultCh <- result
				errCh <- err
			}()

			select {
			case <-requestStarted:
			case <-time.After(3 * time.Second):
				cancel()
				t.Fatal("provider did not receive the simulated request")
			}

			select {
			case result := <-resultCh:
				if err := <-errCh; err != nil {
					t.Fatalf("Generate: %v", err)
				}
				if result.Status != "failed" || !strings.Contains(result.Reason, "streaming response") {
					t.Fatalf("stream response result = %#v; want prompt protocol failure", result)
				}
				if result.Output != "" || strings.Contains(result.Reason, credential) {
					t.Fatalf("stream response or credential escaped failure boundary: %#v", result)
				}
			case <-time.After(3 * time.Second):
				cancel()
				select {
				case <-resultCh:
				case <-time.After(3 * time.Second):
					t.Fatal("generation remained blocked after cancellation")
				}
				t.Fatal("client waited for a streaming response to end instead of rejecting its protocol")
			}
		})
	}
}

func TestOpenAICompatibleRequestCredentialIsRedactedFromProviderError(t *testing.T) {
	disableModelMaintenanceForTest(t)
	const credential = "provider-test-credential-do-not-log"
	t.Setenv("HAI_TEST_OPENAI_ERROR_KEY", credential)
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if got := r.Header.Get("Authorization"); got != "Bearer "+credential {
			t.Errorf("authorization = %q, want configured bearer credential", got)
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode generation request: %v", err)
		}
		if payload["stream"] == true {
			t.Error("OpenAI-compatible request opted into streaming")
		}
		http.Error(w, fmt.Sprintf(`{"error":{"message":"invalid token %s"}}`, credential), http.StatusBadGateway)
	}))
	defer server.Close()

	policy := testPolicyWithoutEndpoints()
	index := providerIndex(t, policy, "lm-studio")
	policy.Providers[index].EndpointURL = server.URL
	policy.Providers[index].APIKeyEnv = "HAI_TEST_OPENAI_ERROR_KEY"
	service := withTrustedTestFinalEffects(t, &Service{policy: policy})
	result, err := service.Generate(withTrustedTestEffect(GenerateRequest{
		Task: "Draft a response",
		RouteDecision: &RouteDecision{
			SelectedProviderID: "lm-studio",
			SelectedModelID:    "local-model",
			Tier:               TierFree,
		},
	}))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if result.Status != "failed" || result.Output != "" {
		t.Fatalf("provider error result = %#v; want failed with no output", result)
	}
	if strings.Contains(result.Reason, credential) {
		t.Fatalf("provider response disclosed the configured credential: %q", result.Reason)
	}
	if requests != 1 {
		t.Fatalf("provider POST count = %d, want exactly one attempt", requests)
	}
}

func TestOpenAICompatibleGenerationRedactsConfiguredCredentialFromOutput(t *testing.T) {
	disableModelMaintenanceForTest(t)
	const credential = "provider-test-credential-do-not-log"
	t.Setenv("HAI_TEST_OPENAI_OUTPUT_KEY", credential)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]string{"content": "quoted credential: " + credential}}},
			"usage":   map[string]int{"prompt_tokens": 8, "completion_tokens": 5},
		})
	}))
	defer server.Close()

	policy := testPolicyWithoutEndpoints()
	index := providerIndex(t, policy, "lm-studio")
	policy.Providers[index].EndpointURL = server.URL
	policy.Providers[index].APIKeyEnv = "HAI_TEST_OPENAI_OUTPUT_KEY"
	service := withTrustedTestFinalEffects(t, &Service{policy: policy})
	result, err := service.Generate(withTrustedTestEffect(GenerateRequest{
		Task: "Draft a response",
		RouteDecision: &RouteDecision{
			SelectedProviderID: "lm-studio",
			SelectedModelID:    "local-model",
			Tier:               TierFree,
		},
	}))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if result.Status != "completed" || strings.Contains(result.Output, credential) || !strings.Contains(result.Output, "[REDACTED]") {
		t.Fatalf("generation output did not redact the configured credential: %#v", result)
	}
}

func TestPaidGenerationTimeoutIsNotRetriedAndKeepsConservativeReservation(t *testing.T) {
	disableModelMaintenanceForTest(t)
	t.Setenv("LLM_GENERATION_TIMEOUT_SECONDS", "1")
	history := &observedPaidBudgetTestRepository{fakeGenerationHistoryRepository: &fakeGenerationHistoryRepository{}}
	var requests int
	requestStarted := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/v1/models" {
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{}})
			return
		}
		if r.Method != http.MethodPost || r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		requests++
		if !history.reserved.Load() {
			t.Error("provider was dispatched before the paid reservation was persisted")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		requestStarted <- struct{}{}
		<-r.Context().Done()
	}))
	defer server.Close()

	service, request := paidBudgetTestService(t, history, server.URL)
	resultCh := make(chan *GenerationResult, 1)
	errCh := make(chan error, 1)
	go func() {
		result, err := service.Generate(withTrustedTestEffect(request))
		resultCh <- result
		errCh <- err
	}()
	select {
	case <-requestStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("paid provider did not receive the simulated generation request")
	}
	select {
	case result := <-resultCh:
		if err := <-errCh; err != nil {
			t.Fatalf("Generate: %v", err)
		}
		if result.Status != "failed" || result.AuditStatus != "recorded" || result.Output != "" {
			t.Fatalf("timed-out paid generation = %#v; want failed and durably audited", result)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("provider generation exceeded its configured timeout")
	}
	if requests != 1 {
		t.Fatalf("provider POST attempts = %d, want exactly one", requests)
	}
	if len(history.records) != 1 || history.records[0].Status != "failed" ||
		history.records[0].UsageSource != "estimated_uncertain" || history.records[0].EstimatedCostEUR <= 0 {
		t.Fatalf("timed-out paid attempt did not retain its conservative reservation: %#v", history.records)
	}
}

func TestGenerationTimeoutIsBounded(t *testing.T) {
	for _, test := range []struct {
		name  string
		value string
		want  time.Duration
	}{
		{name: "default", value: "", want: 60 * time.Second},
		{name: "minimum", value: "0", want: time.Second},
		{name: "configured", value: "90", want: 90 * time.Second},
		{name: "maximum", value: "301", want: 5 * time.Minute},
		{name: "malformed", value: "1s", want: 60 * time.Second},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("LLM_GENERATION_TIMEOUT_SECONDS", test.value)
			if got := generationTimeout(); got != test.want {
				t.Fatalf("generation timeout = %s; want %s", got, test.want)
			}
		})
	}
}

func TestProviderReadinessBlocksNonLocalHTTPSPrivateIPDestinations(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
	}{
		{name: "localhost DNS name", endpoint: "https://localhost/v1"},
		{name: "localhost subdomain", endpoint: "https://provider.localhost/v1"},
		{name: "IPv4 loopback", endpoint: "https://127.0.0.1/v1"},
		{name: "IPv4 private", endpoint: "https://10.20.30.40/v1"},
		{name: "IPv6 loopback", endpoint: "https://[::1]/v1"},
		{name: "IPv6 unique local", endpoint: "https://[fd00::1]/v1"},
		{name: "scoped IPv6 link-local", endpoint: "https://[fe80::1%25eth0]/v1"},
		{name: "link-local remains blocked despite legacy override", endpoint: "https://169.254.169.254/v1"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.name == "link-local remains blocked despite legacy override" {
				t.Setenv("LLM_ALLOW_LINK_LOCAL_ENDPOINTS", "true")
			}
			readiness := providerRuntimeReadiness(Provider{
				ID: "custom-openai-compatible", Enabled: true, EndpointURL: test.endpoint,
			})
			if readiness.configured || readiness.status != "blocked_endpoint" {
				t.Fatalf("provider readiness = %#v; want blocked_endpoint", readiness)
			}
		})
	}

	public := providerRuntimeReadiness(Provider{
		ID: "custom-openai-compatible", Enabled: true, EndpointURL: "https://1.1.1.1/v1",
	})
	if !public.configured {
		t.Fatalf("public IP endpoint readiness = %#v; want configured", public)
	}

	local := providerRuntimeReadiness(Provider{
		ID: "custom-openai-compatible", Enabled: true, Local: true, EndpointURL: "https://127.0.0.1/v1",
	})
	if !local.configured {
		t.Fatalf("explicit local provider readiness = %#v; want configured", local)
	}
}

func TestGenerationRejectsEmptyProviderResponses(t *testing.T) {
	disableModelMaintenanceForTest(t)
	for _, test := range []struct {
		name       string
		providerID string
		modelID    string
		path       string
		keyEnv     string
		response   string
	}{
		{
			name: "Ollama response field missing", providerID: "ollama", modelID: "phi3:mini",
			path: "/api/generate", keyEnv: "HAI_TEST_OLLAMA_KEY", response: `{}`,
		},
		{
			name: "OpenAI-compatible message content empty", providerID: "lm-studio", modelID: "local-model",
			path: "/v1/chat/completions", keyEnv: "HAI_TEST_OPENAI_KEY",
			response: `{"choices":[{"message":{"content":"  "}}]}`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv(test.keyEnv, "protocol-test-credential")
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != test.path {
					t.Errorf("request path = %q, want %q", r.URL.Path, test.path)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, test.response)
			}))
			defer server.Close()

			policy := testPolicyWithoutEndpoints()
			index := providerIndex(t, policy, test.providerID)
			policy.Providers[index].EndpointURL = server.URL
			policy.Providers[index].APIKeyEnv = test.keyEnv
			service := withTrustedTestFinalEffects(t, &Service{policy: policy})
			result, err := service.Generate(withTrustedTestEffect(GenerateRequest{
				Task: "Draft a response",
				RouteDecision: &RouteDecision{
					SelectedProviderID: test.providerID,
					SelectedModelID:    test.modelID,
					Tier:               TierFree,
				},
			}))
			if err != nil {
				t.Fatalf("Generate: %v", err)
			}
			if result.Status != "failed" || result.Output != "" {
				t.Fatalf("empty provider response result = %#v; want failed with no output", result)
			}
		})
	}
}
