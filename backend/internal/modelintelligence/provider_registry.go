package modelintelligence

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

// remoteProvider is a generic OpenAI-compatible provider configured from env.
// It is not_configured unless a valid base URL is
// set, is never active without a successful probe, and never executes actions.
type remoteProvider struct {
	enabled    bool
	id         string
	name       string
	baseURL    string
	apiKeyEnv  string
	probePath  string
	genPath    string
	modelID    string
	lanes      []RoutingLane
	arch       ArchitectureFamily
	local      bool
	configErr  string
	httpClient *http.Client
}

func newRemoteProvider(id, name, baseURLEnv, modelID string, arch ArchitectureFamily, lanes []RoutingLane) *remoteProvider {
	p := &remoteProvider{
		enabled:    true,
		id:         id,
		name:       name,
		baseURL:    strings.TrimSpace(os.Getenv(baseURLEnv)),
		probePath:  "/v1/models",
		genPath:    "/v1/chat/completions",
		modelID:    modelID,
		lanes:      lanes,
		arch:       arch,
		httpClient: newDirectHTTPClient(5 * time.Second),
	}
	if p.baseURL == "" {
		p.configErr = baseURLEnv + " not set"
	} else if err := validateEndpointURL(p.baseURL); err != nil {
		p.configErr = err.Error()
	}
	return p
}

// newGuardedLocalGatewayProvider registers an operator-hosted gateway without
// making it a cloud bypass. It needs an explicit enable flag, a loopback-only
// endpoint, and a separate gateway key before it can even be probed.
func newGuardedLocalGatewayProvider(id, name, enabledEnv, baseURLEnv, modelID, apiKeyEnv string, arch ArchitectureFamily, lanes []RoutingLane) *remoteProvider {
	p := newLocalRemoteProvider(id, name, baseURLEnv, modelID, arch, lanes)
	p.enabled = strings.EqualFold(strings.TrimSpace(os.Getenv(enabledEnv)), "true")
	p.apiKeyEnv = apiKeyEnv
	if !p.enabled {
		p.configErr = enabledEnv + " is false or missing"
		return p
	}
	if p.configErr == "" && strings.TrimSpace(os.Getenv(apiKeyEnv)) == "" {
		p.configErr = apiKeyEnv + " not set"
	}
	return p
}

func newLocalRemoteProvider(id, name, baseURLEnv, modelID string, arch ArchitectureFamily, lanes []RoutingLane) *remoteProvider {
	p := newRemoteProvider(id, name, baseURLEnv, modelID, arch, lanes)
	p.local = true
	if p.baseURL != "" {
		if err := validateLocalEndpointURLForProvider(p.baseURL, id); err != nil {
			p.configErr = err.Error()
		}
	}
	return p
}

func envOrDefault(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func firstConfiguredModelID(name, fallback string) string {
	for _, candidate := range strings.Split(strings.TrimSpace(os.Getenv(name)), ",") {
		if value := strings.TrimSpace(candidate); value != "" {
			return value
		}
	}
	return fallback
}

func (p *remoteProvider) ID() string          { return p.id }
func (p *remoteProvider) DisplayName() string { return p.name }
func (p *remoteProvider) configured() bool    { return p.enabled && p.baseURL != "" && p.configErr == "" }

// ModelMaintenanceIdentity exposes only the fixed local endpoint/model pair
// used for an actual inference call. It deliberately refuses external gateways
// and incomplete configuration, which keeps the maintenance gate local-only.
func (p *remoteProvider) ModelMaintenanceIdentity() (string, string, bool) {
	if !p.local || !p.configured() {
		return "", "", false
	}
	return p.baseURL, p.modelID, true
}

func (p *remoteProvider) bearerToken() string {
	if p.apiKeyEnv == "" {
		return ""
	}
	return strings.TrimSpace(os.Getenv(p.apiKeyEnv))
}

func (p *remoteProvider) status() ProviderStatus {
	if !p.configured() {
		return ProviderNotConfigured
	}
	return ProviderConfigured
}

func (p *remoteProvider) claim() ClaimLevel {
	if !p.configured() {
		return ClaimContractDefined
	}
	return ClaimConfigured
}

func (p *remoteProvider) Profiles() []ModelProfile {
	return []ModelProfile{{
		ProviderID:         p.id,
		ModelID:            p.modelID,
		DisplayName:        p.name,
		ArchitectureFamily: p.arch,
		Lanes:              p.lanes,
		Local:              p.local,
		Paid:               false,
		Status:             p.status(),
		ClaimLevel:         p.claim(),
	}}
}

func (p *remoteProvider) Probe(ctx context.Context, now time.Time) ProbeResult {
	if !p.configured() {
		return ProbeResult{ProviderID: p.id, Status: ProviderNotConfigured, Detail: p.configErr, CheckedAt: now}
	}
	return probeModelsEndpointWithBearer(ctx, p.httpClient, p.id, p.baseURL, p.probePath, p.bearerToken(), now)
}

func (p *remoteProvider) Generate(ctx context.Context, req InferenceRequest, now time.Time) (InferenceResult, error) {
	if !p.configured() {
		return InferenceResult{ProviderID: p.id, OK: false, Error: p.configErr}, fmt.Errorf("%s: %s", p.id, p.configErr)
	}
	probe := p.Probe(ctx, now)
	if probe.Status != ProviderActive {
		return InferenceResult{ProviderID: p.id, OK: false, Error: probe.Detail}, fmt.Errorf("%s: not active: %s", p.id, probe.Detail)
	}
	return chatCompletionWithBearer(ctx, p.httpClient, p.id, p.modelID, p.baseURL, p.genPath, p.bearerToken(), req)
}

// Registry holds all configured providers and their profiles (§10.17).
type Registry struct {
	providers []Provider
}

// NewRegistryFromEnv assembles the initial provider set:
//   - test-fast-triage, test-verifier (always active, deterministic, local)
//   - dspark (env, not_configured by default)
//   - ollama, lm-studio, llama.cpp, LocalAI, vLLM, SGLang, mistral.rs, LiteLLM, custom-openai-compatible (env, not_configured by default)
func NewRegistryFromEnv() *Registry {
	return &Registry{providers: []Provider{
		&testFastTriageProvider{},
		&testVerifierProvider{},
		NewDSparkProvider(DSparkConfigFromEnv()),
		newLocalRemoteProvider("ollama", "Ollama (loopback local server)", "OLLAMA_BASE_URL", firstConfiguredModelID("OLLAMA_MODEL_IDS", "phi3:mini"), ArchOllamaUnknown, []RoutingLane{LaneFastTriage, LaneDrafting}),
		newLocalRemoteProvider("lm-studio", "LM Studio (loopback local server)", "LM_STUDIO_BASE_URL", envOrDefault("LM_STUDIO_MODEL_ID", "local-model"), ArchLocalRuntimeUnknown, []RoutingLane{LaneFastTriage, LaneDrafting}),
		newLocalRemoteProvider("llama-cpp", "llama.cpp (local OpenAI-compatible)", "LLAMA_CPP_BASE_URL", envOrDefault("LLAMA_CPP_MODEL_ID", "local-model"), ArchLocalRuntimeUnknown, []RoutingLane{LaneFastTriage, LaneDrafting}),
		newLocalRemoteProvider("localai", "LocalAI (loopback OpenAI-compatible)", "LOCALAI_BASE_URL", envOrDefault("LOCALAI_MODEL_ID", "localai-default"), ArchLocalRuntimeUnknown, []RoutingLane{LaneFastTriage, LaneDrafting}),
		newLocalRemoteProvider("vllm", "vLLM (loopback OpenAI-compatible)", "VLLM_BASE_URL", envOrDefault("VLLM_MODEL_ID", "vllm-default"), ArchLocalRuntimeUnknown, []RoutingLane{LaneFastTriage, LaneDrafting, LaneParallelBatch}),
		newLocalRemoteProvider("sglang", "SGLang (loopback OpenAI-compatible)", "SGLANG_BASE_URL", envOrDefault("SGLANG_MODEL_ID", "sglang-default"), ArchLocalRuntimeUnknown, []RoutingLane{LaneFastTriage, LaneDrafting, LaneParallelBatch}),
		newLocalRemoteProvider("mistral-rs", "mistral.rs (loopback OpenAI-compatible)", "MISTRAL_RS_BASE_URL", envOrDefault("MISTRAL_RS_MODEL_ID", "mistralrs-default"), ArchLocalRuntimeUnknown, []RoutingLane{LaneFastTriage, LaneDrafting}),
		newGuardedLocalGatewayProvider("litellm", "LiteLLM (local-only gateway)", "LITELLM_ENABLED", "LITELLM_BASE_URL", envOrDefault("LITELLM_MODEL_ID", "local-model"), "LITELLM_API_KEY", ArchOpenAICompatibleUnknown, []RoutingLane{LaneFastTriage, LaneDrafting}),
		newRemoteProvider("custom-openai-compatible", "Custom OpenAI-compatible", "CUSTOM_OPENAI_BASE_URL", "custom-default", ArchOpenAICompatibleUnknown, []RoutingLane{LaneDrafting, LaneParallelBatch}),
	}}
}

// Providers returns all registered providers.
func (r *Registry) Providers() []Provider { return r.providers }

// Provider returns a provider by id.
func (r *Registry) Provider(id string) (Provider, bool) {
	for _, p := range r.providers {
		if p.ID() == id {
			return p, true
		}
	}
	return nil, false
}

// Profiles returns every model profile across all providers.
func (r *Registry) Profiles() []ModelProfile {
	var out []ModelProfile
	for _, p := range r.providers {
		out = append(out, p.Profiles()...)
	}
	return out
}

// Profile returns a specific profile by provider + model id.
func (r *Registry) Profile(providerID, modelID string) (ModelProfile, bool) {
	for _, prof := range r.Profiles() {
		if prof.ProviderID == providerID && prof.ModelID == modelID {
			return prof, true
		}
	}
	return ModelProfile{}, false
}

// ProfilesForLane returns the usable (active) profiles that serve a lane.
func (r *Registry) ProfilesForLane(lane RoutingLane) []ModelProfile {
	var out []ModelProfile
	for _, prof := range r.Profiles() {
		if prof.ServesLane(lane) && prof.Usable() {
			out = append(out, prof)
		}
	}
	return out
}
