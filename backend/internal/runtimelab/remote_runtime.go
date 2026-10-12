package runtimelab

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"automation-hub-backend/internal/executionbroker"
)

const (
	runtimeLabAllowedHostsEnv     = "RUNTIME_LAB_ALLOWED_HOSTS"
	defaultRuntimeLabAllowedHosts = "localhost,127.0.0.1,::1,host.docker.internal,hermes,openclaw,odysseus,openhands"
	maxRuntimeDiscoveryBytes      = 64 * 1024
)

type remoteProtocolProfile struct {
	name       string
	healthPath string
	authEnv    string
}

func protocolProfile(runtimeID string) remoteProtocolProfile {
	switch runtimeID {
	case "openclaw":
		return remoteProtocolProfile{name: "openclaw-http-health-v1", healthPath: "/health"}
	case "hermes":
		return remoteProtocolProfile{name: "hermes-api-server-v1", healthPath: "/health", authEnv: "HERMES_API_SERVER_KEY"}
	case "odysseus":
		return remoteProtocolProfile{name: "odysseus-http-api-v1", healthPath: "/api/health"}
	default:
		return remoteProtocolProfile{name: "unverified-http-health", healthPath: "/health"}
	}
}

// remoteRuntime is a fail-closed adapter for an operator-managed external
// runtime. Discovery and execution authority are deliberately separate: even
// a schema-valid health response keeps execution blocked.
type remoteRuntime struct {
	id         string
	name       string
	baseURLEnv string
	healthPath string
	baseURL    string
	configErr  string
	profile    remoteProtocolProfile
	httpClient *http.Client

	mu        sync.RWMutex
	lastProbe *ProbeResult
}

func newRemoteRuntime(id, name, baseURLEnv string) *remoteRuntime {
	profile := protocolProfile(id)
	r := &remoteRuntime{
		id:         id,
		name:       name,
		baseURLEnv: baseURLEnv,
		healthPath: envDefault(strings.ToUpper(id)+"_HEALTH_PATH", profile.healthPath),
		baseURL:    strings.TrimSpace(os.Getenv(baseURLEnv)),
		profile:    profile,
		httpClient: &http.Client{
			Timeout: 5 * time.Second,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
	if r.baseURL == "" {
		r.configErr = baseURLEnv + " not set"
	} else if err := validateURL(r.baseURL); err != nil {
		r.configErr = err.Error()
	}
	return r
}

func (r *remoteRuntime) configured() bool { return r.baseURL != "" && r.configErr == "" }

func (r *remoteRuntime) Info() RuntimeInfo {
	return RuntimeInfo{
		ID:          r.id,
		DisplayName: r.name,
		Kind:        KindAgentRuntime,
		Description: r.name + " is an external agent runtime governed by HAI; discovery never grants task or host authority.",
	}
}

func (r *remoteRuntime) Capabilities() []string {
	return []string{"declared:agent_task_execution", "declared:protocol_discovery"}
}

func (r *remoteRuntime) SetupRequirements() []SetupRequirement {
	return []SetupRequirement{
		{Step: "Provision the runtime", Detail: "Install/run " + r.name + " yourself; HAI never downloads or installs third-party runtimes automatically."},
		{Step: "Configure the endpoint", Detail: "Set " + r.baseURLEnv + " to a reachable base URL whose host is explicitly listed in " + runtimeLabAllowedHostsEnv + "."},
		{Step: "Pass protocol discovery", Detail: "POST /runtime-lab/" + r.id + "/probe must validate the reviewed " + r.profile.name + " response; an HTTP 200 alone is insufficient."},
		{Step: "Authorize a bounded integration", Detail: "Discovery is read-only. A separate HAI operation, approval, evidence, and verification contract is required before any task execution."},
	}
}

func (r *remoteRuntime) HealthCheck(_ context.Context) Health {
	if !r.configured() {
		return Health{Status: executionbroker.RuntimeNotConfigured, Detail: r.configErr, Claim: executionbroker.ClaimContractDefined, SetupRequirements: r.SetupRequirements()}
	}
	if last, ok := r.LastDiscovery(); ok && last.ProtocolValid {
		return Health{
			Status:            executionbroker.RuntimeBlocked,
			Detail:            fmt.Sprintf("%s discovery %s; execution remains blocked", last.ReadinessLevel, last.DiscoveryState),
			Claim:             executionbroker.ClaimProbed,
			SetupRequirements: r.SetupRequirements()[3:],
		}
	}
	return Health{
		Status:            executionbroker.RuntimeBlocked,
		Detail:            "endpoint configured; protocol discovery required; execution is disabled",
		Claim:             executionbroker.ClaimConfigured,
		SetupRequirements: r.SetupRequirements()[2:],
	}
}

// LastDiscovery returns a copy of the latest in-process discovery result. HAI
// intentionally forgets this evidence on restart until a durable ledger-backed
// discovery record is introduced; it never restores readiness from a claim.
func (r *remoteRuntime) LastDiscovery() (ProbeResult, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.lastProbe == nil {
		return ProbeResult{}, false
	}
	result := *r.lastProbe
	result.Capabilities = append([]string(nil), r.lastProbe.Capabilities...)
	return result, true
}

func (r *remoteRuntime) remember(result ProbeResult) ProbeResult {
	r.mu.Lock()
	copyResult := result
	copyResult.Capabilities = append([]string(nil), result.Capabilities...)
	r.lastProbe = &copyResult
	r.mu.Unlock()
	return result
}

func (r *remoteRuntime) Probe(ctx context.Context, now time.Time) ProbeResult {
	if !r.configured() {
		return r.remember(ProbeResult{
			RuntimeID: r.id, Status: executionbroker.RuntimeNotConfigured,
			DiscoveryState: "not_configured", ReadinessLevel: ReadinessDeclared,
			Detail: r.configErr, CheckedAt: now,
		})
	}
	result := r.probeProtocol(ctx, now)
	return r.remember(result)
}

func (r *remoteRuntime) probeProtocol(ctx context.Context, now time.Time) ProbeResult {
	start := time.Now()
	base := ProbeResult{
		RuntimeID: r.id, Status: executionbroker.RuntimeBlocked,
		DiscoveryState: "failed", ReadinessLevel: ReadinessConfigured,
		Protocol: r.profile.name, CheckedAt: now,
	}
	health, rawHealth, status, err := r.getJSON(ctx, r.healthPath, "")
	base.DurationMs = time.Since(start).Milliseconds()
	if err != nil {
		base.Status = executionbroker.RuntimeUnavailable
		base.Detail = "runtime discovery could not reach or validate the configured endpoint; review the local runtime configuration"
		return base
	}
	if status != http.StatusOK {
		base.Status = executionbroker.RuntimeUnavailable
		base.Detail = fmt.Sprintf("health HTTP %d", status)
		return base
	}

	switch r.id {
	case "openclaw":
		if boolField(health, "ok") != true || stringField(health, "status") != "live" {
			base.Detail = "OpenClaw health response did not match {ok:true,status:live}"
			return base
		}
		base.DiscoveryState = "succeeded"
		base.ReadinessLevel = ReadinessAvailable
		base.ProtocolValid = true
		base.Detail = "OpenClaw health contract validated; response does not carry a cryptographic runtime identity; execution remains blocked"
		base.EvidenceSHA256 = digestEvidence(rawHealth)
		return base

	case "hermes":
		if stringField(health, "status") != "ok" || stringField(health, "platform") != "hermes-agent" || stringField(health, "version") == "" {
			base.Detail = "Hermes health response did not match the reviewed platform/version contract"
			return base
		}
		base.DiscoveryState = "succeeded"
		base.ReadinessLevel = ReadinessHealthChecked
		base.ProtocolValid = true
		base.IdentityVerified = true
		base.RuntimeVersion = stringField(health, "version")
		base.Detail = "Hermes identity and liveness validated; execution remains blocked"
		evidence := [][]byte{rawHealth}
		if token := strings.TrimSpace(os.Getenv(r.profile.authEnv)); token != "" {
			capability, rawCapability, capabilityStatus, capabilityErr := r.getJSON(ctx, "/v1/capabilities", token)
			base.DurationMs = time.Since(start).Milliseconds()
			if capabilityErr != nil || capabilityStatus != http.StatusOK || stringField(capability, "object") != "hermes.api_server.capabilities" || stringField(capability, "platform") != "hermes-agent" {
				base.DiscoveryState = "partial"
				base.Detail = "Hermes liveness validated, but authenticated capability discovery failed; execution remains blocked"
			} else {
				base.Authenticated = true
				base.Capabilities = enabledCapabilityNames(capability["features"])
				base.Detail = "Hermes identity, liveness, and authenticated capability contract validated; execution remains blocked"
				evidence = append(evidence, rawCapability)
			}
		}
		base.EvidenceSHA256 = digestEvidence(evidence...)
		return base

	case "odysseus":
		if stringField(health, "status") != "healthy" {
			base.Detail = "Odysseus liveness response did not match {status:healthy}"
			return base
		}
		if _, err := time.Parse(time.RFC3339Nano, stringField(health, "timestamp")); err != nil {
			base.Detail = "Odysseus liveness response did not contain an RFC3339 timestamp"
			return base
		}
		version, rawVersion, versionStatus, versionErr := r.getJSON(ctx, "/api/version", "")
		base.DurationMs = time.Since(start).Milliseconds()
		if versionErr != nil || versionStatus != http.StatusOK || stringField(version, "version") == "" {
			base.Detail = "Odysseus liveness passed but version discovery failed"
			return base
		}
		base.DiscoveryState = "succeeded"
		base.ReadinessLevel = ReadinessAvailable
		base.ProtocolValid = true
		base.RuntimeVersion = stringField(version, "version")
		base.Detail = "Odysseus liveness/version contract validated; admin readiness remains unprobed and execution remains blocked"
		base.EvidenceSHA256 = digestEvidence(rawHealth, rawVersion)
		return base

	default:
		base.DiscoveryState = "reachable_unverified"
		base.Detail = "endpoint returned HTTP 200, but no reviewed runtime identity schema is registered; execution remains blocked"
		base.EvidenceSHA256 = digestEvidence(rawHealth)
		return base
	}
}

func (r *remoteRuntime) getJSON(ctx context.Context, path, bearer string) (map[string]any, []byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(r.baseURL, "/")+path, nil)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("build discovery request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := r.httpClient.Do(req)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("runtime discovery unavailable: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxRuntimeDiscoveryBytes+1))
	if err != nil {
		return nil, nil, resp.StatusCode, fmt.Errorf("read discovery response: %w", err)
	}
	if len(raw) > maxRuntimeDiscoveryBytes {
		return nil, nil, resp.StatusCode, fmt.Errorf("discovery response exceeds %d bytes", maxRuntimeDiscoveryBytes)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, raw, resp.StatusCode, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var payload map[string]any
	if err := decoder.Decode(&payload); err != nil {
		return nil, raw, resp.StatusCode, fmt.Errorf("discovery response is not a JSON object: %w", err)
	}
	if payload == nil {
		return nil, raw, resp.StatusCode, fmt.Errorf("discovery response is empty")
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, raw, resp.StatusCode, fmt.Errorf("discovery response contains trailing JSON")
	}
	return payload, raw, resp.StatusCode, nil
}

func stringField(payload map[string]any, key string) string {
	value, _ := payload[key].(string)
	return strings.TrimSpace(value)
}

func boolField(payload map[string]any, key string) bool {
	value, _ := payload[key].(bool)
	return value
}

func enabledCapabilityNames(value any) []string {
	features, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	names := make([]string, 0, len(features))
	for name, enabled := range features {
		if flag, ok := enabled.(bool); ok && flag {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	if len(names) > 64 {
		names = names[:64]
	}
	return names
}

func digestEvidence(parts ...[]byte) string {
	h := sha256.New()
	for _, part := range parts {
		_, _ = h.Write(part)
		_, _ = h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func (r *remoteRuntime) Execute(_ context.Context, _ map[string]any) (executionbroker.RuntimeResult, error) {
	msg := r.name + " execution is not enabled: discovery is not execution authority; complete a HAI-governed operation, approval, evidence, and verification integration first"
	return executionbroker.RuntimeResult{OK: false, Error: msg}, fmt.Errorf("runtimelab: %s", msg)
}

func (r *remoteRuntime) Stop(context.Context) error { return nil }

func validateURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("URL scheme must be http/https, got %q", u.Scheme)
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("URL must not contain credentials, query, or fragment")
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("URL host is empty")
	}
	if !runtimeLabAllowedHosts()[strings.ToLower(host)] {
		return fmt.Errorf("URL host %q is not listed in %s", host, runtimeLabAllowedHostsEnv)
	}
	return nil
}

func runtimeLabAllowedHosts() map[string]bool {
	raw := strings.TrimSpace(os.Getenv(runtimeLabAllowedHostsEnv))
	if raw == "" {
		raw = defaultRuntimeLabAllowedHosts
	}
	allowed := map[string]bool{}
	for _, host := range strings.Split(raw, ",") {
		host = strings.ToLower(strings.TrimSpace(host))
		if host != "" {
			allowed[host] = true
		}
	}
	return allowed
}

func envDefault(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}
