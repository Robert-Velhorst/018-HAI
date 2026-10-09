package runtimelab

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"automation-hub-backend/internal/agentruntime"
	"automation-hub-backend/internal/executionbroker"
)

// agentRuntimeBridge projects a canonical agent runtime into Runtime Lab's
// inspection contract. It intentionally exposes only bounded, non-secret
// readiness data and refuses every execution request.
type agentRuntimeBridge struct {
	registry *agentruntime.Registry
	id       string

	mu        sync.RWMutex
	lastProbe *ProbeResult
}

func newAgentRuntimeBridge(registry *agentruntime.Registry, runtimeID string) *agentRuntimeBridge {
	return &agentRuntimeBridge{registry: registry, id: strings.ToLower(strings.TrimSpace(runtimeID))}
}

func (b *agentRuntimeBridge) Info() RuntimeInfo {
	info, ok := b.runtimeInfo()
	if !ok {
		return RuntimeInfo{
			ID:          b.id,
			DisplayName: "Canonical agent runtime",
			Kind:        KindAgentRuntime,
			Description: "The canonical agent-runtime registry does not expose this runtime.",
		}
	}
	return RuntimeInfo{
		ID:          info.ID,
		DisplayName: info.Name,
		Kind:        KindAgentRuntime,
		Description: info.Name + " is governed by HAI's canonical agent-runtime registry; Runtime Lab provides read-only inspection only.",
	}
}

func (b *agentRuntimeBridge) Capabilities() []string {
	info, ok := b.runtimeInfo()
	if !ok {
		return []string{"declared:canonical_runtime_inspection"}
	}
	capabilities := append([]string{"declared:canonical_runtime_inspection"}, info.Capabilities...)
	return uniqueStrings(capabilities)
}

func (b *agentRuntimeBridge) SetupRequirements() []SetupRequirement {
	return []SetupRequirement{
		{Step: "Enable the canonical runtime", Detail: "Set OPENCLAW_AGENT_ENABLED=true in the canonical agent-runtime configuration."},
		{Step: "Configure read-only Gateway inspection", Detail: "Set OPENCLAW_GATEWAY_ENABLED=true and OPENCLAW_GATEWAY_URL to an allowlisted operator-managed Gateway."},
		{Step: "Constrain the target", Detail: "List the Gateway host in AGENT_RUNTIME_ALLOWED_HOSTS. Tokens remain in OPENCLAW_GATEWAY_TOKEN and are never exposed through Runtime Lab."},
		{Step: "Use the governed execution path", Detail: "Runtime Lab is inspection-only. Any task still requires the canonical HAI approval, authorization, evidence, and verification path."},
	}
}

func (b *agentRuntimeBridge) HealthCheck(ctx context.Context) Health {
	health, ok := b.runtimeHealth(ctx)
	if !ok {
		return Health{
			Status:            executionbroker.RuntimeNotConfigured,
			Detail:            "canonical agent runtime is not registered",
			Claim:             executionbroker.ClaimContractDefined,
			SetupRequirements: b.SetupRequirements(),
		}
	}
	status, claim := labStatusForCanonicalHealth(health.Status)
	result := Health{Status: status, Detail: health.Reason, Claim: claim}
	if status != executionbroker.RuntimeBlocked || claim != executionbroker.ClaimProbed {
		result.SetupRequirements = b.SetupRequirements()
	}
	return result
}

func (b *agentRuntimeBridge) Probe(ctx context.Context, now time.Time) ProbeResult {
	started := time.Now()
	result := ProbeResult{
		RuntimeID:      b.id,
		DiscoveryState: "failed",
		ReadinessLevel: ReadinessDeclared,
		Protocol:       "canonical-agent-runtime-health-v1",
		CheckedAt:      now,
	}
	health, ok := b.runtimeHealth(ctx)
	if !ok {
		result.Status = executionbroker.RuntimeNotConfigured
		result.Detail = "canonical agent runtime is not registered"
		result.DurationMs = time.Since(started).Milliseconds()
		return b.remember(result)
	}
	result.Status, _ = labStatusForCanonicalHealth(health.Status)
	result.Detail = health.Reason
	result.DurationMs = time.Since(started).Milliseconds()
	switch strings.ToLower(strings.TrimSpace(health.Status)) {
	case "available", "ready":
		result.Status = executionbroker.RuntimeBlocked
		result.DiscoveryState = "succeeded"
		result.ReadinessLevel = ReadinessAvailable
		result.RuntimeVersion = strings.TrimSpace(health.Version)
		result.ProtocolValid = health.GatewayProtocolValidated
		result.Authenticated = health.GatewayAuthenticated
		result.IdentityVerified = result.Authenticated && result.RuntimeVersion != ""
		result.EvidenceSchema = strings.TrimSpace(health.GatewayEvidenceSchema)
		result.EndpointSHA256 = strings.TrimSpace(health.GatewayEndpointSHA256)
		result.GatewayScope = strings.TrimSpace(health.GatewayScope)
		result.GatewayTaskLedger = gatewayTaskLedgerSummaryFromHealth(health.GatewayTaskLedger)
		result.GatewayCapabilityCatalog = gatewayCapabilityCatalogSummaryFromHealth(health.GatewayCapabilityCatalog)
		result.GatewayPreparedModelCatalog = gatewayPreparedModelCatalogSummaryFromHealth(health.GatewayPreparedModelCatalog)
		result.GatewayAgentRoster = gatewayAgentRosterSummaryFromHealth(health.GatewayAgentRoster)
		if result.ProtocolValid {
			result.Protocol = "openclaw-gateway-v4"
			expiresAt := now.Add(openClawDiscoveryEvidenceTTL)
			result.EvidenceExpiresAt = &expiresAt
			result.Detail = "canonical OpenClaw protocol discovery validated; Runtime Lab remains read-only and task execution stays governed by the canonical HAI runtime path"
		} else {
			result.Detail = "canonical OpenClaw liveness was observed, but no protocol challenge was validated; Runtime Lab remains read-only and task execution stays governed by the canonical HAI runtime path"
		}
	default:
		if result.Status == executionbroker.RuntimeBlocked {
			result.ReadinessLevel = ReadinessConfigured
		}
	}
	return b.remember(result)
}

func (b *agentRuntimeBridge) Execute(_ context.Context, _ map[string]any) (executionbroker.RuntimeResult, error) {
	message := "Runtime Lab is inspection-only; execute OpenClaw tasks through the canonical HAI runtime authorization path"
	return executionbroker.RuntimeResult{OK: false, Error: message}, fmt.Errorf("runtimelab: %s", message)
}

// Stop is deliberately a no-op. Runtime Lab never owns canonical task
// lifecycles, so it must not issue remote task cancellation requests.
func (*agentRuntimeBridge) Stop(context.Context) error { return nil }

func (b *agentRuntimeBridge) LastDiscovery() (ProbeResult, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.lastProbe == nil {
		return ProbeResult{}, false
	}
	result := *b.lastProbe
	result.Capabilities = append([]string(nil), b.lastProbe.Capabilities...)
	result.GatewayTaskLedger = cloneGatewayTaskLedgerSummary(b.lastProbe.GatewayTaskLedger)
	result.GatewayCapabilityCatalog = cloneGatewayCapabilityCatalogSummary(b.lastProbe.GatewayCapabilityCatalog)
	result.GatewayPreparedModelCatalog = cloneGatewayPreparedModelCatalogSummary(b.lastProbe.GatewayPreparedModelCatalog)
	result.GatewayAgentRoster = cloneGatewayAgentRosterSummary(b.lastProbe.GatewayAgentRoster)
	if b.lastProbe.EvidenceExpiresAt != nil {
		expiresAt := *b.lastProbe.EvidenceExpiresAt
		result.EvidenceExpiresAt = &expiresAt
	}
	return result, true
}

func (b *agentRuntimeBridge) remember(result ProbeResult) ProbeResult {
	b.mu.Lock()
	copyResult := result
	copyResult.Capabilities = append([]string(nil), result.Capabilities...)
	copyResult.GatewayTaskLedger = cloneGatewayTaskLedgerSummary(result.GatewayTaskLedger)
	copyResult.GatewayCapabilityCatalog = cloneGatewayCapabilityCatalogSummary(result.GatewayCapabilityCatalog)
	copyResult.GatewayPreparedModelCatalog = cloneGatewayPreparedModelCatalogSummary(result.GatewayPreparedModelCatalog)
	copyResult.GatewayAgentRoster = cloneGatewayAgentRosterSummary(result.GatewayAgentRoster)
	if result.EvidenceExpiresAt != nil {
		expiresAt := *result.EvidenceExpiresAt
		copyResult.EvidenceExpiresAt = &expiresAt
	}
	b.lastProbe = &copyResult
	b.mu.Unlock()
	return result
}

func gatewayTaskLedgerSummaryFromHealth(summary *agentruntime.GatewayTaskLedgerSummary) *GatewayTaskLedgerSummary {
	if summary == nil {
		return nil
	}
	return cloneGatewayTaskLedgerSummary(&GatewayTaskLedgerSummary{
		SampledTasks: summary.SampledTasks,
		StatusCounts: summary.StatusCounts,
		Truncated:    summary.Truncated,
	})
}

func cloneGatewayTaskLedgerSummary(summary *GatewayTaskLedgerSummary) *GatewayTaskLedgerSummary {
	if summary == nil {
		return nil
	}
	copySummary := *summary
	copySummary.StatusCounts = make(map[string]int, len(summary.StatusCounts))
	for status, count := range summary.StatusCounts {
		copySummary.StatusCounts[status] = count
	}
	return &copySummary
}

func gatewayCapabilityCatalogSummaryFromHealth(summary *agentruntime.GatewayCapabilityCatalogSummary) *GatewayCapabilityCatalogSummary {
	if summary == nil {
		return nil
	}
	return cloneGatewayCapabilityCatalogSummary(&GatewayCapabilityCatalogSummary{
		SampledSkills:      summary.SampledSkills,
		EligibleSkills:     summary.EligibleSkills,
		SampledCommands:    summary.SampledCommands,
		ToolCountsBySource: summary.ToolCountsBySource,
	})
}

func cloneGatewayCapabilityCatalogSummary(summary *GatewayCapabilityCatalogSummary) *GatewayCapabilityCatalogSummary {
	if summary == nil {
		return nil
	}
	copySummary := *summary
	copySummary.ToolCountsBySource = make(map[string]int, len(summary.ToolCountsBySource))
	for source, count := range summary.ToolCountsBySource {
		copySummary.ToolCountsBySource[source] = count
	}
	return &copySummary
}

func gatewayPreparedModelCatalogSummaryFromHealth(summary *agentruntime.GatewayPreparedModelCatalogSummary) *GatewayPreparedModelCatalogSummary {
	if summary == nil {
		return nil
	}
	return cloneGatewayPreparedModelCatalogSummary(&GatewayPreparedModelCatalogSummary{
		SampledModels:             summary.SampledModels,
		AvailableModels:           summary.AvailableModels,
		UnavailableModels:         summary.UnavailableModels,
		UnknownAvailabilityModels: summary.UnknownAvailabilityModels,
	})
}

func cloneGatewayPreparedModelCatalogSummary(summary *GatewayPreparedModelCatalogSummary) *GatewayPreparedModelCatalogSummary {
	if summary == nil {
		return nil
	}
	copySummary := *summary
	return &copySummary
}

func gatewayAgentRosterSummaryFromHealth(summary *agentruntime.GatewayAgentRosterSummary) *GatewayAgentRosterSummary {
	if summary == nil {
		return nil
	}
	return cloneGatewayAgentRosterSummary(&GatewayAgentRosterSummary{
		SampledAgents:    summary.SampledAgents,
		AgentCount:       summary.AgentCount,
		SystemCount:      summary.SystemCount,
		UnknownKindCount: summary.UnknownKindCount,
	})
}

func cloneGatewayAgentRosterSummary(summary *GatewayAgentRosterSummary) *GatewayAgentRosterSummary {
	if summary == nil {
		return nil
	}
	copySummary := *summary
	return &copySummary
}

func (b *agentRuntimeBridge) runtimeInfo() (agentruntime.Info, bool) {
	if b.registry == nil {
		return agentruntime.Info{}, false
	}
	for _, info := range b.registry.List() {
		if info.ID == b.id {
			return info, true
		}
	}
	return agentruntime.Info{}, false
}

func (b *agentRuntimeBridge) runtimeHealth(ctx context.Context) (agentruntime.Health, bool) {
	if b.registry == nil {
		return agentruntime.Health{}, false
	}
	return b.registry.HealthFor(ctx, b.id)
}

func labStatusForCanonicalHealth(status string) (executionbroker.RuntimeStatus, executionbroker.ClaimLevel) {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "available", "ready":
		return executionbroker.RuntimeBlocked, executionbroker.ClaimConfigured
	case "disabled":
		return executionbroker.RuntimeNotConfigured, executionbroker.ClaimContractDefined
	case "blocked":
		return executionbroker.RuntimeBlocked, executionbroker.ClaimConfigured
	default:
		return executionbroker.RuntimeUnavailable, executionbroker.ClaimConfigured
	}
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, found := seen[value]; found {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}
