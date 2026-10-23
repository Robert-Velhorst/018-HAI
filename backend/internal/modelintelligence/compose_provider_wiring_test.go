package modelintelligence

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestOllamaComposeConstructorReachesMaintenanceAndInference(t *testing.T) {
	const endpoint = "http://ollama-local:11434"
	const model = "compose-fixture-model"
	t.Setenv("OLLAMA_BASE_URL", endpoint)
	t.Setenv("OLLAMA_MODEL_IDS", model)
	t.Setenv(localInferenceAttestationEnv, "ollama/"+model)
	provider, ok := NewRegistryFromEnv().provider("ollama")
	if !ok {
		t.Fatal("Ollama provider was not registered")
	}
	remote := provider.(*remoteProvider)
	if !remote.configured() || !remote.endpointLocal {
		t.Fatalf("canonical Compose provider rejected by constructor: %s", remote.configurationDetail())
	}
	var probes, generations atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != "ollama-local:11434" {
			t.Error("connector changed the configured HTTP host")
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/models":
			probes.Add(1)
			_, _ = w.Write([]byte(`{"data":[{"id":"compose-fixture-model"}]}`))
		case "/v1/chat/completions":
			generations.Add(1)
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"category=general; summary=ok"}}],"usage":{"prompt_tokens":3,"completion_tokens":4}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	// Only the fixture dial is redirected to loopback; the constructor and HTTP
	// request keep the real Compose identity, without requiring Docker or DNS.
	transport := &http.Transport{DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		if address != "ollama-local:11434" {
			t.Errorf("unexpected configured dial target %q", address)
		}
		return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
	}}
	defer transport.CloseIdleConnections()
	remote.httpClient = newDirectHTTPClient(2 * time.Second)
	remote.httpClient.Transport = transport
	gate := &modelMaintenanceGateStub{}
	service := NewService(&Registry{providers: []Provider{provider}}).WithModelMaintenance(gate)
	probe, err := service.Probe(context.Background(), "ollama")
	if err != nil || probe.Status != ProviderActive {
		t.Fatalf("Compose probe status=%s error=%v", probe.Status, err)
	}
	decision, result, err := service.RunLane(context.Background(), LaneFastTriage, LaneInput{}, "classify this", "compose-wiring")
	if err != nil || !decision.Routable || result == nil || !result.OK {
		t.Fatalf("Compose inference: routable=%t result=%#v error=%v", decision.Routable, result, err)
	}
	if gate.calls != 1 || gate.endpoint != endpoint || gate.modelID != model || probes.Load() != 2 || generations.Load() != 1 {
		t.Fatalf("unexpected operational chain: maintenance=%#v probes=%d generations=%d", gate, probes.Load(), generations.Load())
	}
}

func TestOtherProviderConstructorsRejectOllamaComposeIdentity(t *testing.T) {
	t.Setenv("HAI_COMPOSE_PROVIDER_TEST_URL", "http://ollama-local:11434")
	for _, id := range []string{"lm-studio", "llama-cpp", "localai", "vllm"} {
		provider := newLocalRemoteProvider(id, id, "HAI_COMPOSE_PROVIDER_TEST_URL", "model", ArchLocalRuntimeUnknown, nil)
		if provider.configured() || provider.endpointLocal {
			t.Fatalf("%s constructor accepted another provider's internal service", id)
		}
		if _, _, applicable := provider.ModelMaintenanceIdentity(); applicable {
			t.Fatalf("%s supplied maintenance identity for another provider's service", id)
		}
	}
}
