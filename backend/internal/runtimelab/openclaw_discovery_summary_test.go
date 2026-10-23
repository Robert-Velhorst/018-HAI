package runtimelab

import (
	"testing"
	"time"
)

func TestOpenClawDiscoveryRejectsOverflowedAggregateCounts(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	now := time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)
	base := ProbeResult{
		RuntimeID: "openclaw", Protocol: "openclaw-gateway-v4", ProtocolValid: true,
		Authenticated: true, GatewayScope: "operator.read", EvidenceSchema: openClawDiscoverySchema,
		EndpointSHA256: "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
		CheckedAt:      now,
	}
	tests := []struct {
		name  string
		apply func(*ProbeResult)
	}{
		{"tasks", func(p *ProbeResult) {
			p.GatewayTaskLedger = &GatewayTaskLedgerSummary{SampledTasks: 1, StatusCounts: map[string]int{"running": maxInt, "queued": maxInt, "completed": 3}}
		}},
		{"tools", func(p *ProbeResult) {
			p.GatewayCapabilityCatalog = &GatewayCapabilityCatalogSummary{ToolCountsBySource: map[string]int{"core": maxInt, "plugin": maxInt}}
		}},
		{"models", func(p *ProbeResult) {
			p.GatewayPreparedModelCatalog = &GatewayPreparedModelCatalogSummary{SampledModels: 1, AvailableModels: maxInt, UnavailableModels: maxInt, UnknownAvailabilityModels: 3}
		}},
		{"agents", func(p *ProbeResult) {
			p.GatewayAgentRoster = &GatewayAgentRosterSummary{SampledAgents: 1, AgentCount: maxInt, SystemCount: maxInt, UnknownKindCount: 3}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			probe := base
			tt.apply(&probe)
			evidence, err := newOpenClawDiscoveryEvidence(probe, now)
			if err == nil {
				t.Error("malformed aggregate must be rejected before persistence")
			} else {
				// Reproduce an already stored, correctly hashed but malformed payload.
				evidence = openClawDiscoveryEvidence{
					SchemaRevision: openClawDiscoverySchema, RuntimeID: "openclaw", Protocol: base.Protocol,
					ProtocolValid: true, Authenticated: true, GatewayScope: "operator.read", EndpointSHA256: base.EndpointSHA256,
					CheckedAt: now, ExpiresAt: now.Add(openClawDiscoveryEvidenceTTL),
					GatewayTaskLedger: probe.GatewayTaskLedger, GatewayCapabilityCatalog: probe.GatewayCapabilityCatalog,
					GatewayPreparedModelCatalog: probe.GatewayPreparedModelCatalog, GatewayAgentRoster: probe.GatewayAgentRoster,
				}
			}
			evidence.EvidenceSHA256 = openClawDiscoveryEvidenceSHA256(evidence)
			if validOpenClawDiscoveryEvidence(evidence, now) {
				t.Error("correctly hashed overflowed aggregate must not be restored")
			}
		})
	}
}

func TestOpenClawDiscoveryAggregateCountBoundaries(t *testing.T) {
	for _, count := range []int{0, 1, 500} {
		if !validGatewayCapabilityCatalogSummary(&GatewayCapabilityCatalogSummary{SampledSkills: count, EligibleSkills: count, SampledCommands: count, ToolCountsBySource: map[string]int{"core": count, "plugin": 0}}) {
			t.Errorf("valid capability count rejected: %d", count)
		}
		if !validGatewayPreparedModelCatalogSummary(&GatewayPreparedModelCatalogSummary{SampledModels: count, AvailableModels: count}) {
			t.Errorf("valid model count rejected: %d", count)
		}
		if !validGatewayAgentRosterSummary(&GatewayAgentRosterSummary{SampledAgents: count, AgentCount: count}) {
			t.Errorf("valid agent count rejected: %d", count)
		}
	}
	for _, count := range []int{0, 1, 50} {
		if !validGatewayTaskLedgerSummary(&GatewayTaskLedgerSummary{SampledTasks: count, StatusCounts: map[string]int{"running": count}}) {
			t.Errorf("valid task count rejected: %d", count)
		}
	}
	if validGatewayCapabilityCatalogSummary(&GatewayCapabilityCatalogSummary{ToolCountsBySource: map[string]int{"core": 300, "plugin": 201}}) {
		t.Error("combined tool count above 500 must be rejected")
	}
}
