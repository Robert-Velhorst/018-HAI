package runtimelab

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"automation-hub-backend/internal/executionbroker"
)

// RuntimeReadinessLevel is the evidence ladder required by the external
// runtime integration brief. Levels are never inferred from a higher-sounding
// product claim; HAI advances only with retained evidence for that level.
type RuntimeReadinessLevel string

const (
	ReadinessDeclared          RuntimeReadinessLevel = "declared"
	ReadinessConfigured        RuntimeReadinessLevel = "configured"
	ReadinessAvailable         RuntimeReadinessLevel = "available"
	ReadinessHealthChecked     RuntimeReadinessLevel = "health_checked"
	ReadinessSelfTested        RuntimeReadinessLevel = "self_tested"
	ReadinessIntegrationTested RuntimeReadinessLevel = "integration_tested"
	ReadinessDemonstrated      RuntimeReadinessLevel = "demonstrated"
	ReadinessProductionReady   RuntimeReadinessLevel = "production_ready"
)

// RuntimeCapabilityCard projects one upstream behavior into HAI's capability
// vocabulary. A card is a contract and readiness record, not permission.
type RuntimeCapabilityCard struct {
	ID                       string                        `json:"id"`
	RuntimeID                string                        `json:"runtimeId"`
	Name                     string                        `json:"name"`
	Purpose                  string                        `json:"purpose"`
	InputSchema              map[string]any                `json:"inputSchema"`
	OutputSchema             map[string]any                `json:"outputSchema"`
	AuthenticationState      string                        `json:"authenticationState"`
	Availability             executionbroker.RuntimeStatus `json:"availability"`
	RuntimeLocation          string                        `json:"runtimeLocation"`
	RequiredAuthority        []string                      `json:"requiredAuthority"`
	RiskLevel                string                        `json:"riskLevel"`
	ExpectedCostEURMax       float64                       `json:"expectedCostEurMax"`
	CostPolicy               string                        `json:"costPolicy"`
	ContextCost              string                        `json:"contextCost"`
	TimeoutSeconds           int                           `json:"timeoutSeconds"`
	RetryBehaviour           string                        `json:"retryBehaviour"`
	Reversibility            string                        `json:"reversibility"`
	ApprovalRequirements     []string                      `json:"approvalRequirements"`
	VerificationMethod       string                        `json:"verificationMethod"`
	EvidenceReturned         []string                      `json:"evidenceReturned"`
	ReadinessLevel           RuntimeReadinessLevel         `json:"readinessLevel"`
	ReadinessReason          string                        `json:"readinessReason"`
	CanInvoke                bool                          `json:"canInvoke"`
	CanExecuteExternalEffect bool                          `json:"canExecuteExternalEffect"`
	LatestDiscovery          *ProbeResult                  `json:"latestDiscovery,omitempty"`
	SourceFeatureIDs         []string                      `json:"sourceFeatureIds"`
}

// RuntimeCapabilityOverview is the non-secret capability projection.
type RuntimeCapabilityOverview struct {
	Cards      []RuntimeCapabilityCard `json:"cards"`
	Counts     map[string]int          `json:"counts"`
	Authority  string                  `json:"authority"`
	SafetyNote string                  `json:"safetyNote"`
}

type capabilityTemplate struct {
	card RuntimeCapabilityCard
}

func runtimeCapabilityTemplates() []capabilityTemplate {
	readOnlyInput := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties":           map[string]any{},
	}
	readOnlyOutput := map[string]any{
		"type":     "object",
		"required": []string{"runtimeId", "status", "checkedAt"},
		"properties": map[string]any{
			"runtimeId": map[string]any{"type": "string"},
			"status":    map[string]any{"type": "string"},
			"checkedAt": map[string]any{"type": "string", "format": "date-time"},
		},
	}
	delegatedInput := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"operationId", "task", "successCriteria", "contextRefs", "authorizationReceiptId"},
		"properties": map[string]any{
			"operationId":            map[string]any{"type": "string", "format": "uuid"},
			"task":                   map[string]any{"type": "string", "maxLength": 8000},
			"successCriteria":        map[string]any{"type": "array", "maxItems": 20, "items": map[string]any{"type": "string", "maxLength": 1000}},
			"contextRefs":            map[string]any{"type": "array", "maxItems": 50, "items": map[string]any{"type": "string"}},
			"authorizationReceiptId": map[string]any{"type": "string", "format": "uuid"},
		},
	}
	delegatedOutput := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"operationId", "status", "artifacts", "evidence", "runtimeReceipt"},
		"properties": map[string]any{
			"operationId":    map[string]any{"type": "string", "format": "uuid"},
			"status":         map[string]any{"enum": []string{"completed", "blocked", "failed", "cancelled", "unknown"}},
			"artifacts":      map[string]any{"type": "array", "maxItems": 50},
			"evidence":       map[string]any{"type": "array", "maxItems": 100},
			"runtimeReceipt": map[string]any{"type": "object"},
		},
	}
	return []capabilityTemplate{
		{card: discoveryCard("openclaw.gateway.discovery", "openclaw", "Inspect OpenClaw Gateway readiness", readOnlyInput, readOnlyOutput, "openclaw-gateway", "openclaw-security")},
		{card: discoveryCard("openclaw.tasks.discovery", "openclaw", "Inspect OpenClaw task-state availability", readOnlyInput, map[string]any{
			"type": "object", "required": []string{"sampledTasks", "statusCounts", "truncated"}, "properties": map[string]any{"sampledTasks": map[string]any{"type": "integer"}, "statusCounts": map[string]any{"type": "object"}, "truncated": map[string]any{"type": "boolean"}},
		}, "openclaw-gateway", "openclaw-resilience", "openclaw-security")},
		{card: discoveryCard("openclaw.capabilities.discovery", "openclaw", "Inspect OpenClaw skill and tool availability", readOnlyInput, map[string]any{
			"type": "object", "required": []string{"sampledSkills", "eligibleSkills", "sampledCommands", "toolCountsBySource"}, "properties": map[string]any{"sampledSkills": map[string]any{"type": "integer"}, "eligibleSkills": map[string]any{"type": "integer"}, "sampledCommands": map[string]any{"type": "integer"}, "toolCountsBySource": map[string]any{"type": "object"}},
		}, "openclaw-capabilities", "openclaw-security")},
		{card: discoveryCard("openclaw.models.discovery", "openclaw", "Inspect prepared OpenClaw model availability", readOnlyInput, map[string]any{
			"type": "object", "required": []string{"sampledModels", "availableModels", "unavailableModels", "unknownAvailabilityModels"}, "properties": map[string]any{"sampledModels": map[string]any{"type": "integer"}, "availableModels": map[string]any{"type": "integer"}, "unavailableModels": map[string]any{"type": "integer"}, "unknownAvailabilityModels": map[string]any{"type": "integer"}},
		}, "openclaw-models", "openclaw-security")},
		{card: discoveryCard("openclaw.agents.discovery", "openclaw", "Inspect OpenClaw agent roster availability", readOnlyInput, map[string]any{
			"type": "object", "required": []string{"sampledAgents", "agentCount", "systemCount", "unknownKindCount"}, "properties": map[string]any{"sampledAgents": map[string]any{"type": "integer"}, "agentCount": map[string]any{"type": "integer"}, "systemCount": map[string]any{"type": "integer"}, "unknownKindCount": map[string]any{"type": "integer"}},
		}, "openclaw-gateway", "openclaw-multi-agent", "openclaw-security")},
		{card: delegationCard("openclaw.agent.delegate", "openclaw", "Delegate one bounded task to OpenClaw", delegatedInput, delegatedOutput, "openclaw-gateway", "openclaw-multi-agent", "openclaw-resilience")},
		{card: discoveryCard("hermes.gateway.discovery", "hermes", "Inspect Hermes gateway readiness", readOnlyInput, readOnlyOutput, "hermes-protocol", "hermes-security")},
		{card: delegationCard("hermes.agent.delegate", "hermes", "Delegate one bounded task to Hermes", delegatedInput, delegatedOutput, "hermes-protocol", "hermes-multi-agent", "hermes-resilience")},
		{card: discoveryCard("hermes.skills.discovery", "hermes", "Inspect reviewed Hermes skill metadata", readOnlyInput, map[string]any{
			"type": "object", "required": []string{"skills"}, "properties": map[string]any{"skills": map[string]any{"type": "array", "maxItems": 500}},
		}, "hermes-capabilities")},
		{card: discoveryCard("odysseus.service.discovery", "odysseus", "Inspect a separate Odysseus service", readOnlyInput, readOnlyOutput, "odysseus-runtime", "odysseus-observability")},
		{card: delegationCard("odysseus.research.delegate", "odysseus", "Delegate one bounded research task to a separate Odysseus service", delegatedInput, delegatedOutput, "odysseus-runtime", "odysseus-planning", "odysseus-resilience")},
	}
}

func discoveryCard(id, runtimeID, name string, input, output map[string]any, sourceFeatures ...string) RuntimeCapabilityCard {
	return RuntimeCapabilityCard{
		ID: id, RuntimeID: runtimeID, Name: name,
		Purpose:     "Read non-secret runtime version, protocol, and declared capability metadata without invoking a tool.",
		InputSchema: input, OutputSchema: output, RuntimeLocation: "operator_managed_local_service",
		// Probe is an audited POST, so its HAI route requires write permission even though the upstream read has no execution effect.
		RequiredAuthority: []string{"authenticated_owner", "write", "runtime.read"}, RiskLevel: "low",
		ExpectedCostEURMax: 0, CostPolicy: "No model or paid provider call is allowed.", ContextCost: "none",
		TimeoutSeconds: 5, RetryBehaviour: "One manual retry; redirects and non-allowlisted hosts fail closed.",
		Reversibility: "read_only", ApprovalRequirements: []string{"No execution approval; endpoint configuration remains owner-managed."},
		VerificationMethod: "Validate protocol/version response against the pinned schema and reviewed runtime identity.",
		EvidenceReturned:   []string{"runtime identity", "protocol version", "checked timestamp", "bounded declared capabilities"},
		SourceFeatureIDs:   sourceFeatures,
	}
}

func delegationCard(id, runtimeID, name string, input, output map[string]any, sourceFeatures ...string) RuntimeCapabilityCard {
	return RuntimeCapabilityCard{
		ID: id, RuntimeID: runtimeID, Name: name,
		Purpose:     "Execute one bounded, correlated subtask while HAI retains planning, policy, approval, audit, and completion authority.",
		InputSchema: input, OutputSchema: output, RuntimeLocation: "operator_managed_local_service",
		RequiredAuthority: []string{"authenticated_owner", "runtime.execute", "exact_execution_authorization_receipt"}, RiskLevel: "high",
		ExpectedCostEURMax: 0, CostPolicy: "Paid usage remains disabled; the runtime must accept HAI's exact model and budget constraint.", ContextCost: "bounded_by_task_context_budget",
		TimeoutSeconds: 300, RetryBehaviour: "No automatic effect retry; ambiguous outcomes go to review and require receipt reconciliation.",
		Reversibility: "capability_specific", ApprovalRequirements: []string{"Current policy approval", "exact effect authorization", "fresh source and plan digests"},
		VerificationMethod: "Independent HAI verifier checks returned artifacts and external postconditions before completion.",
		EvidenceReturned:   []string{"runtime request ID", "runtime receipt", "bounded artifacts", "tool lifecycle", "source references", "terminal status"},
		SourceFeatureIDs:   sourceFeatures,
	}
}

// CapabilityCards returns HAI-native cards with dynamic configuration state.
// No card currently grants external execution authority.
func (s *Service) CapabilityCards(ctx context.Context) (RuntimeCapabilityOverview, error) {
	parity, err := s.FeatureParity()
	if err != nil {
		return RuntimeCapabilityOverview{}, err
	}
	featureIDs := map[string]map[string]bool{}
	for _, inventory := range parity.Inventories {
		featureIDs[inventory.RuntimeID] = map[string]bool{}
		for _, item := range inventory.Features {
			featureIDs[inventory.RuntimeID][item.ID] = true
		}
	}
	cards := make([]RuntimeCapabilityCard, 0)
	counts := map[string]int{}
	readOnlyDiscoveryAvailable := false
	for _, template := range runtimeCapabilityTemplates() {
		card := template.card
		for _, featureID := range card.SourceFeatureIDs {
			if !featureIDs[card.RuntimeID][featureID] {
				return RuntimeCapabilityOverview{}, fmt.Errorf("runtimelab: capability %s references unknown feature %s", card.ID, featureID)
			}
		}
		adapter, ok := s.reg.Adapter(card.RuntimeID)
		if !ok {
			return RuntimeCapabilityOverview{}, fmt.Errorf("runtimelab: capability %s references unknown runtime %s", card.ID, card.RuntimeID)
		}
		health := adapter.HealthCheck(ctx)
		card.Availability = health.Status
		card.AuthenticationState = "not_configured"
		card.ReadinessLevel = ReadinessDeclared
		card.ReadinessReason = "Source-reviewed contract only; no protocol-specific capability handshake has passed."
		if health.Claim == executionbroker.ClaimConfigured || health.Claim == executionbroker.ClaimProbed {
			card.AuthenticationState = "operator_configured_unverified"
			card.ReadinessLevel = ReadinessConfigured
			card.ReadinessReason = "Endpoint is configured, but capability identity and protocol have not been verified."
		}
		if discovery, found := s.durableDiscovery(card.RuntimeID); found {
			copyDiscovery := discovery
			card.LatestDiscovery = &copyDiscovery
			if discovery.ProtocolValid && s.discoveryIsCurrent(card.RuntimeID, discovery) && cardDiscoveryAvailable(card.ID, discovery) {
				card.ReadinessLevel = discovery.ReadinessLevel
				card.ReadinessReason = discovery.Detail
				card.CanInvoke = true
				readOnlyDiscoveryAvailable = true
				if discovery.Authenticated {
					card.AuthenticationState = "authenticated_read_only_discovery"
				} else if discovery.IdentityVerified {
					card.AuthenticationState = "identity_verified_liveness_only"
				} else {
					card.AuthenticationState = "protocol_validated_identity_unverified"
				}
			}
		} else if reader, ok := adapter.(interface {
			LastDiscovery() (ProbeResult, bool)
		}); ok {
			if discovery, found := reader.LastDiscovery(); found {
				copyDiscovery := discovery
				card.LatestDiscovery = &copyDiscovery
				if discovery.ProtocolValid && s.discoveryIsCurrent(card.RuntimeID, discovery) && cardDiscoveryAvailable(card.ID, discovery) {
					card.ReadinessLevel = discovery.ReadinessLevel
					card.ReadinessReason = discovery.Detail
					card.CanInvoke = true
					readOnlyDiscoveryAvailable = true
					if discovery.Authenticated {
						card.AuthenticationState = "authenticated_read_only_discovery"
					} else if discovery.IdentityVerified {
						card.AuthenticationState = "identity_verified_liveness_only"
					} else {
						card.AuthenticationState = "protocol_validated_identity_unverified"
					}
				}
			}
		}
		card.CanExecuteExternalEffect = false
		counts[string(card.ReadinessLevel)]++
		cards = append(cards, card)
	}
	sort.Slice(cards, func(i, j int) bool { return strings.Compare(cards[i].ID, cards[j].ID) < 0 })
	return RuntimeCapabilityOverview{
		Cards: cards, Counts: counts,
		Authority: func() string {
			if readOnlyDiscoveryAvailable {
				return "read_only_discovery_only"
			}
			return "contract_only"
		}(),
		SafetyNote: "Only reviewed read-only discovery may become invocable. No discovery result grants model, tool, host, or external-effect authority.",
	}, nil
}

func (s *Service) discoveryIsCurrent(runtimeID string, discovery ProbeResult) bool {
	now := s.now().UTC()
	if discovery.CheckedAt.IsZero() || discovery.CheckedAt.After(now) {
		return false
	}
	if normalizeRuntimeID(runtimeID) != "openclaw" {
		age := now.Sub(discovery.CheckedAt.UTC())
		return age >= 0 && age < runtimeDiscoveryFreshnessTTL
	}
	return discovery.EvidenceExpiresAt != nil && discovery.EvidenceExpiresAt.After(now)
}

func (s *Service) durableDiscovery(runtimeID string) (ProbeResult, bool) {
	if normalizeRuntimeID(runtimeID) != "openclaw" {
		return ProbeResult{}, false
	}
	return s.durableOpenClawDiscovery()
}

func cardDiscoveryAvailable(cardID string, discovery ProbeResult) bool {
	switch cardID {
	case "openclaw.gateway.discovery", "hermes.gateway.discovery", "odysseus.service.discovery":
		return true
	case "openclaw.capabilities.discovery":
		return openClawAuthenticatedRead(discovery) && discovery.GatewayCapabilityCatalog != nil && validGatewayCapabilityCatalogSummary(discovery.GatewayCapabilityCatalog)
	case "openclaw.models.discovery":
		return openClawAuthenticatedRead(discovery) && discovery.GatewayPreparedModelCatalog != nil && validGatewayPreparedModelCatalogSummary(discovery.GatewayPreparedModelCatalog)
	case "openclaw.tasks.discovery":
		return openClawAuthenticatedRead(discovery) && discovery.GatewayTaskLedger != nil && validGatewayTaskLedgerSummary(discovery.GatewayTaskLedger)
	case "openclaw.agents.discovery":
		return openClawAuthenticatedRead(discovery) && discovery.GatewayAgentRoster != nil && validGatewayAgentRosterSummary(discovery.GatewayAgentRoster)
	default:
		return false
	}
}

func openClawAuthenticatedRead(discovery ProbeResult) bool {
	return discovery.Authenticated && strings.TrimSpace(discovery.GatewayScope) == "operator.read"
}
