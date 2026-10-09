package task

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"automation-hub-backend/internal/agentregistry"
	"automation-hub-backend/internal/frameworkregistry"
)

// AgentContextProvider supplies owner-scoped, registry-backed agent cards to
// framework selection. Implementations must return an error rather than a
// partial result when registry state cannot be trusted.
type AgentContextProvider interface {
	LatestAgents(ownerIdentity string, at time.Time) ([]frameworkregistry.AgentCard, error)
}

type agentListRepository interface {
	List(context.Context, string) ([]agentregistry.Agent, error)
}

type registryAgentContextProvider struct {
	repository agentListRepository
}

func NewAgentContextProvider(repository agentListRepository) (AgentContextProvider, error) {
	if repository == nil {
		return nil, fmt.Errorf("agent repository is required")
	}
	return &registryAgentContextProvider{repository: repository}, nil
}

func (p *registryAgentContextProvider) LatestAgents(ownerIdentity string, at time.Time) ([]frameworkregistry.AgentCard, error) {
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	if ownerIdentity == "" {
		return nil, fmt.Errorf("owner identity is required")
	}
	at = at.UTC()
	if at.IsZero() {
		return nil, fmt.Errorf("agent context time is required")
	}
	agents, err := p.repository.List(context.Background(), ownerIdentity)
	if err != nil {
		return nil, fmt.Errorf("list owner-scoped agents: %w", err)
	}
	cards := make([]frameworkregistry.AgentCard, 0, len(agents))
	for _, agent := range agents {
		if strings.TrimSpace(agent.OwnerIdentity) != ownerIdentity {
			return nil, fmt.Errorf("agent %q escaped owner scope", agent.ID)
		}
		if err := agentregistry.ValidateAgent(agent, at); err != nil {
			return nil, fmt.Errorf("validate agent %q revision %d: %w", agent.ID, agent.Revision, err)
		}
		cards = append(cards, mapRegistryAgentCard(agent, at))
	}
	sort.Slice(cards, func(i, j int) bool { return cards[i].ID < cards[j].ID })
	return cards, nil
}

func mapRegistryAgentCard(agent agentregistry.Agent, at time.Time) frameworkregistry.AgentCard {
	capabilities, operations := registryCapabilities(agent.Capabilities)
	// Registry health is caller-editable configuration, not a server probe.
	declaredReady := agent.State == agentregistry.StateEnabled &&
		agent.Availability.Available &&
		agent.Availability.MaxConcurrent > 0 &&
		agent.Availability.ActiveAssignments >= 0 &&
		agent.Availability.ActiveAssignments < agent.Availability.MaxConcurrent &&
		agent.Health.Ready &&
		agent.Health.Status == agentregistry.HealthHealthy &&
		healthEvidenceFresh(agent.Health, at)

	status := "configured_unavailable"
	healthStatus := "declared_" + string(agent.Health.Status)
	availability := registryAvailability(agent) + "; declaration_ready=" + strconv.FormatBool(declaredReady)
	if declaredReady {
		status = "configured_unverified"
	}

	revoked := agent.State == agentregistry.StateDisabled ||
		agent.State == agentregistry.StateQuarantined
	revocationReason := ""
	if revoked {
		revocationReason = "agent lifecycle state is " + string(agent.State)
		if reason := strings.TrimSpace(agent.Health.Reason); reason != "" {
			revocationReason += ": " + reason
		}
	}

	authorityCeiling := agent.AuthorityCeiling
	if agent.AutonomyCeiling < authorityCeiling {
		authorityCeiling = agent.AutonomyCeiling
	}
	dataBoundaries := prefixBoundaries("data", agent.DataAllowlist)
	dataBoundaries = append(dataBoundaries, prefixBoundaries("folder", agent.FolderAllowlist)...)
	if len(dataBoundaries) == 0 {
		dataBoundaries = []string{"no data or folder access declared"}
	}

	reliability := agent.Reliability.Score()
	return frameworkregistry.AgentCard{
		ID:                    agent.ID,
		Name:                  agent.Name,
		Owner:                 agent.OwnerIdentity,
		Purpose:               "configured " + string(agent.Type) + " role through " + agent.Runtime.ID + "; trusted server capability probe required",
		Role:                  string(agent.Type),
		Capabilities:          capabilities,
		DomainCompetence:      capabilityIDs(agent.Capabilities),
		AllowedTools:          append([]string(nil), agent.ToolAllowlist...),
		RequiredPermissions:   []string{"runtime:" + agent.Runtime.Type, "adapter:" + agent.Runtime.ID},
		DataAccessBoundaries:  dataBoundaries,
		CostProfile:           registryCostProfile(agent.Performance),
		ModelRequirements:     []string{"runtime protocol " + agent.Runtime.ProtocolVersion},
		ReliabilityHistory:    registryReliabilityHistory(agent.Reliability),
		AllowedActions:        operations,
		ProhibitedActions:     registryLifecycleProhibitions(agent),
		InputSchema:           "hai_agent_message_v1",
		OutputSchema:          "hai_agent_message_v1",
		ExpectedEvidence:      []string{"trusted server capability probe", "task execution outcome evidence"},
		EscalationRoute:       "hai_task_engine then owner-scoped review queue",
		Availability:          availability,
		Version:               fmt.Sprintf("contract-v%d/revision-%d", agent.ContractVersion, agent.Revision),
		Dependencies:          []string{agent.Runtime.ID, agent.Runtime.Type},
		HealthStatus:          healthStatus,
		EvaluationScore:       reliability,
		EvaluationScoreSource: "agent registry success/failure beta prior",
		AuthorityCeiling:      authorityCeiling,
		Status:                status,
		Verified:              false,
		Revoked:               revoked,
		RevocationReason:      revocationReason,
		Provenance: fmt.Sprintf(
			"agent_registry_declaration:%s revision=%d updated=%s declared_health_checked_at=%s",
			agent.ID,
			agent.Revision,
			agent.UpdatedAt.UTC().Format(time.RFC3339),
			agent.Health.CheckedAt.UTC().Format(time.RFC3339Nano),
		),
		LastVerifiedAt: nil,
	}
}

func healthEvidenceFresh(health agentregistry.HealthEvidence, at time.Time) bool {
	if health.CheckedAt.IsZero() || health.FreshFor <= 0 {
		return false
	}
	checkedAt := health.CheckedAt.UTC()
	return !checkedAt.After(at.Add(time.Minute)) && !at.After(checkedAt.Add(health.FreshFor))
}

func registryCapabilities(values []agentregistry.CapabilityDeclaration) ([]string, []string) {
	capabilities := make([]string, 0, len(values))
	operations := make([]string, 0)
	for _, capability := range values {
		id := strings.TrimSpace(capability.ID)
		if id == "" {
			continue
		}
		version := strings.TrimSpace(capability.Version)
		if version == "" {
			capabilities = append(capabilities, id)
		} else {
			capabilities = append(capabilities, id+"@"+version)
		}
		for _, operation := range capability.Operations {
			operation = strings.TrimSpace(operation)
			if operation != "" {
				operations = append(operations, id+"."+operation)
			}
		}
	}
	return sortedStrings(capabilities), sortedStrings(operations)
}

func capabilityIDs(values []agentregistry.CapabilityDeclaration) []string {
	result := make([]string, 0, len(values))
	for _, capability := range values {
		if id := strings.TrimSpace(capability.ID); id != "" {
			result = append(result, id)
		}
	}
	return sortedStrings(result)
}

func prefixBoundaries(prefix string, values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			result = append(result, prefix+":"+value)
		}
	}
	return sortedStrings(result)
}

func registryCostProfile(profile agentregistry.PerformanceProfile) string {
	return "locality=" + string(profile.Locality) +
		"; estimated_cost_eur=" + strconv.FormatFloat(profile.EstimatedCostEUR, 'f', 6, 64) +
		"; p95_latency_ms=" + strconv.FormatInt(profile.P95LatencyMs, 10)
}

func registryAvailability(agent agentregistry.Agent) string {
	return "state=" + string(agent.State) +
		"; available=" + strconv.FormatBool(agent.Availability.Available) +
		"; assignments=" + strconv.Itoa(agent.Availability.ActiveAssignments) +
		"/" + strconv.Itoa(agent.Availability.MaxConcurrent)
}

func registryReliabilityHistory(evidence agentregistry.ReliabilityEvidence) []string {
	result := []string{
		"successes=" + strconv.FormatUint(evidence.Successes, 10),
		"failures=" + strconv.FormatUint(evidence.Failures, 10),
		"consecutive_failures=" + strconv.FormatUint(evidence.ConsecutiveFailures, 10),
		"mean_latency_ms=" + strconv.FormatFloat(evidence.MeanLatencyMs, 'f', 2, 64),
	}
	if !evidence.LastOutcomeAt.IsZero() {
		result = append(result, "last_outcome_at="+evidence.LastOutcomeAt.UTC().Format(time.RFC3339))
	}
	return result
}

func registryLifecycleProhibitions(agent agentregistry.Agent) []string {
	switch agent.State {
	case agentregistry.StateDraining:
		return []string{"accept new assignments while draining"}
	case agentregistry.StateDisabled, agentregistry.StateQuarantined:
		return []string{"perform any task action while " + string(agent.State)}
	default:
		return nil
	}
}

func sortedStrings(values []string) []string {
	result := append([]string(nil), values...)
	sort.Strings(result)
	return result
}
