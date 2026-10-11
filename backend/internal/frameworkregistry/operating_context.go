package frameworkregistry

import (
	"automation-hub-backend/internal/safety"
	"crypto/sha256"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	capacityFreshnessWindow = 24 * time.Hour
	agentFreshnessWindow    = 24 * time.Hour
)

type operatingContract struct {
	LifeDomains       []LifeDomainAssignment
	NeedsState        []NeedStateAssessment
	Capacity          CapacitySnapshot
	AgentCards        []AgentCard
	Delegations       []DelegationContract
	Communication     CommunicationContract
	Coordination      CoordinationPlan
	ActionAutonomy    []ActionAutonomyDecision
	StopConditions    []string
	OutcomeMonitoring []string
	ChiefOfStaff      ChiefOfStaffDecision
	Digest            string
}

func buildOperatingContract(
	request SelectionRequest,
	taskRiskLevel string,
	effectiveRiskCeiling string,
	lifeDomains []LifeDomainAssignment,
	requiredAgents []string,
	maximumAutonomy int,
	requiresApproval bool,
	approvalReasons []string,
	evidenceRequirements []string,
	completionCriteria []string,
	contextRequirements []string,
	now time.Time,
) (operatingContract, error) {
	needsState, err := buildNeedsState(request, lifeDomains)
	if err != nil {
		return operatingContract{}, err
	}
	capacity, err := normalizeCapacitySnapshot(request.Capacity, now)
	if err != nil {
		return operatingContract{}, err
	}
	agentCards, err := buildAgentCards(requiredAgents, request.AvailableAgents, maximumAutonomy, now)
	if err != nil {
		return operatingContract{}, err
	}
	coordination, err := buildCoordinationPlan(request, agentCards)
	if err != nil {
		return operatingContract{}, err
	}
	delegations := buildDelegationContracts(
		request,
		agentCards,
		capacity,
		maximumAutonomy,
		requiresApproval,
		evidenceRequirements,
		completionCriteria,
	)
	communication := buildCommunicationContract(request, maximumAutonomy)
	actionAutonomy := buildActionAutonomy(request, maximumAutonomy, requiresApproval)
	stopConditions := buildStopConditions(capacity, agentCards, coordination, requiresApproval)
	outcomeMonitoring := buildOutcomeMonitoring(request, completionCriteria)
	chiefOfStaff := buildChiefOfStaffDecision(
		request,
		lifeDomains,
		needsState,
		capacity,
		agentCards,
		coordination,
		actionAutonomy,
		requiresApproval,
		approvalReasons,
		completionCriteria,
		contextRequirements,
	)

	contract := operatingContract{
		LifeDomains:       lifeDomains,
		NeedsState:        needsState,
		Capacity:          capacity,
		AgentCards:        agentCards,
		Delegations:       delegations,
		Communication:     communication,
		Coordination:      coordination,
		ActionAutonomy:    actionAutonomy,
		StopConditions:    stopConditions,
		OutcomeMonitoring: outcomeMonitoring,
		ChiefOfStaff:      chiefOfStaff,
	}
	digest, err := canonicalSHA256(struct {
		TaskRiskLevel        string                   `json:"taskRiskLevel"`
		EffectiveRiskCeiling string                   `json:"effectiveRiskCeiling"`
		LifeDomains          []LifeDomainAssignment   `json:"lifeDomains"`
		NeedsState           []NeedStateAssessment    `json:"needsState"`
		Capacity             CapacitySnapshot         `json:"capacity"`
		AgentCards           []AgentCard              `json:"agentCards"`
		Delegations          []DelegationContract     `json:"delegations"`
		Communication        CommunicationContract    `json:"communication"`
		Coordination         CoordinationPlan         `json:"coordination"`
		ActionAutonomy       []ActionAutonomyDecision `json:"actionAutonomy"`
		StopConditions       []string                 `json:"stopConditions"`
		OutcomeMonitoring    []string                 `json:"outcomeMonitoring"`
		ChiefOfStaff         ChiefOfStaffDecision     `json:"chiefOfStaff"`
	}{
		TaskRiskLevel:        taskRiskLevel,
		EffectiveRiskCeiling: effectiveRiskCeiling,
		LifeDomains:          contract.LifeDomains,
		NeedsState:           contract.NeedsState,
		Capacity:             contract.Capacity,
		AgentCards:           contract.AgentCards,
		Delegations:          contract.Delegations,
		Communication:        contract.Communication,
		Coordination:         contract.Coordination,
		ActionAutonomy:       contract.ActionAutonomy,
		StopConditions:       contract.StopConditions,
		OutcomeMonitoring:    contract.OutcomeMonitoring,
		ChiefOfStaff:         contract.ChiefOfStaff,
	})
	if err != nil {
		return operatingContract{}, fmt.Errorf("digest operating contract: %w", err)
	}
	contract.Digest = digest
	return contract, nil
}

func classifyLifeDomains(text string) []LifeDomainAssignment {
	assignments := make([]LifeDomainAssignment, 0, len(domainRules))
	for _, rule := range domainRules {
		matched := make([]string, 0)
		for _, signal := range rule.signals {
			if domainSignalApplies(rule.name, signal, text) {
				matched = append(matched, signal)
			}
		}
		if len(matched) == 0 {
			continue
		}
		sort.Strings(matched)
		assignments = append(assignments, LifeDomainAssignment{
			ID:         rule.name,
			Need:       rule.need,
			Score:      len(matched),
			Confidence: math.Min(0.95, 0.55+(float64(len(matched))*0.1)),
			Signals:    matched,
			Source:     "deterministic_request_classification",
		})
	}
	if len(assignments) == 0 {
		return []LifeDomainAssignment{{
			ID:         "general_operations",
			Need:       "operator request or operational commitment",
			Score:      0,
			Confidence: 0.35,
			Signals:    []string{},
			Primary:    true,
			Source:     "deterministic_request_classification",
		}}
	}
	sort.SliceStable(assignments, func(i, j int) bool {
		if assignments[i].Score != assignments[j].Score {
			return assignments[i].Score > assignments[j].Score
		}
		return false
	})
	assignments[0].Primary = true
	return assignments
}

func domainSignalApplies(domainID, signal, text string) bool {
	if !containsPhrase(text, signal) {
		return false
	}
	if domainID != "health_wellbeing" || signal != "health" {
		return true
	}

	// "Health" is overloaded in an automation hub. Runtime, service, API, and
	// container health are operational signals, not evidence that the operator
	// has a personal-care need. Explicit personal or clinical context always
	// wins so real health requests retain the stricter care-source contract.
	if containsAnyPhrase(text, []string{
		"my health", "personal health", "physical health", "mental health",
		"health condition", "health concern", "health appointment", "health record",
		"doctor", "medical", "medication", "medicine", "therapy", "symptom",
		"patient", "wellbeing", "care plan",
	}) {
		return true
	}
	return !containsAnyPhrase(text, []string{
		"api", "automation", "backend", "container", "dashboard", "deployment",
		"endpoint", "frontend", "health check", "health probe", "health status",
		"healthcheck", "infrastructure", "liveness", "model provider", "readiness",
		"runtime", "service", "system", "worker",
	})
}

func buildNeedsState(request SelectionRequest, domains []LifeDomainAssignment) ([]NeedStateAssessment, error) {
	if len(request.ObservedNeeds) > 0 {
		result := make([]NeedStateAssessment, 0, len(request.ObservedNeeds))
		for _, observed := range request.ObservedNeeds {
			normalized, err := normalizeNeedState(observed)
			if err != nil {
				return nil, err
			}
			result = append(result, normalized)
		}
		sort.SliceStable(result, func(i, j int) bool {
			if result[i].Priority != result[j].Priority {
				return result[i].Priority > result[j].Priority
			}
			return result[i].ID < result[j].ID
		})
		return result, nil
	}

	result := make([]NeedStateAssessment, 0, len(domains))
	for _, domain := range domains {
		level := needsLevelForDomain(domain.ID)
		state := "active"
		priority := 50 + domain.Score*5
		if request.NeedsApproval || strings.EqualFold(strings.TrimSpace(request.RiskLevel), "high") {
			state = "attention_required"
			priority += 20
		}
		if priority > 100 {
			priority = 100
		}
		result = append(result, NeedStateAssessment{
			ID:          "derived-" + domain.ID,
			DomainID:    domain.ID,
			Level:       level,
			State:       state,
			Priority:    priority,
			Confidence:  domain.Confidence,
			Evidence:    append([]string{}, domain.Signals...),
			Source:      "derived_from_request_not_operator_confirmed",
			NeedsReview: true,
		})
	}
	return result, nil
}

func normalizeNeedState(value NeedStateAssessment) (NeedStateAssessment, error) {
	value.ID = normalizeIdentifier(safety.RedactSecrets(value.ID))
	value.DomainID = normalizeIdentifier(safety.RedactSecrets(value.DomainID))
	value.Level = normalizeIdentifier(safety.RedactSecrets(value.Level))
	value.State = normalizeIdentifier(safety.RedactSecrets(value.State))
	value.Source = compactContractText(value.Source)
	value.Evidence = redactedSortedUnique(value.Evidence)
	if value.ID == "" || value.Level == "" || value.State == "" || value.Source == "" {
		return NeedStateAssessment{}, fmt.Errorf("observed need requires id, level, state, and source")
	}
	if value.Priority < 0 || value.Priority > 100 {
		return NeedStateAssessment{}, fmt.Errorf("observed need priority must be between 0 and 100")
	}
	if value.Confidence < 0 || value.Confidence > 1 {
		return NeedStateAssessment{}, fmt.Errorf("observed need confidence must be between 0 and 1")
	}
	return value, nil
}

func needsLevelForDomain(domain string) string {
	switch domain {
	case "emergency_continuity", "safety_security", "home_assets", "financial", "digital_accounts":
		return "safety_and_stability"
	case "health_wellbeing":
		return "physiological_and_wellbeing"
	case "food_nutrition":
		return "physiological_and_nutrition"
	case "relationships_care", "family_household", "animals_dependants":
		return "belonging_and_care"
	case "work_venture":
		return "esteem_and_material_progress"
	case "learning_growth", "creativity_expression":
		return "competence_and_growth"
	case "travel_mobility":
		return "mobility_and_autonomy"
	case "personal_productivity":
		return "self_management"
	case "legal_government":
		return "rights_and_security"
	case "communication_correspondence", "community_civic":
		return "connection_and_participation"
	case "leisure_recreation":
		return "rest_and_recreation"
	case "meaning_values", "legacy_long_term":
		return "meaning_and_legacy"
	case "environment_sustainability":
		return "environment_and_stewardship"
	case "identity_roles":
		return "identity_and_autonomy"
	case "possessions_inventory":
		return "material_stability"
	default:
		return "operator_defined"
	}
}

func normalizeCapacitySnapshot(input *CapacitySnapshot, now time.Time) (CapacitySnapshot, error) {
	if input == nil {
		return CapacitySnapshot{
			Status:            "unknown",
			PlanningStepLimit: 5,
			Constraints: []string{
				"current human capacity was not provided",
				"keep the first plan bounded and ask before creating substantial new commitments",
			},
			Confidence:  0,
			Fresh:       false,
			NeedsReview: true,
		}, nil
	}
	result := *input
	result.Status = normalizeIdentifier(result.Status)
	result.SourceURI = strings.TrimSpace(safety.RedactSecrets(result.SourceURI))
	result.SourceLabel = strings.TrimSpace(safety.RedactSecrets(result.SourceLabel))
	result.Constraints = redactedSortedUnique(result.Constraints)
	if result.Status == "" {
		return CapacitySnapshot{}, fmt.Errorf("capacity status is required")
	}
	for label, value := range map[string]int{
		"energy":       result.Energy,
		"attention":    result.Attention,
		"current load": result.CurrentLoad,
	} {
		if value < 0 || value > 100 {
			return CapacitySnapshot{}, fmt.Errorf("capacity %s must be between 0 and 100", label)
		}
	}
	if result.TimeAvailableMinutes < 0 || result.ConcurrentWorkLimit < 0 {
		return CapacitySnapshot{}, fmt.Errorf("capacity time and concurrent work limit cannot be negative")
	}
	if result.Confidence < 0 || result.Confidence > 1 {
		return CapacitySnapshot{}, fmt.Errorf("capacity confidence must be between 0 and 1")
	}
	if result.CapturedAt == nil || strings.TrimSpace(result.SourceLabel) == "" {
		result.Fresh = false
		result.NeedsReview = true
		result.Constraints = sortedUnique(append(result.Constraints, "capacity lacks a timestamp or source label"))
	} else {
		captured := result.CapturedAt.UTC()
		result.CapturedAt = &captured
		if captured.After(now.UTC().Add(5 * time.Minute)) {
			return CapacitySnapshot{}, fmt.Errorf("capacity capture time cannot be in the future")
		}
		result.Fresh = now.UTC().Sub(captured) <= capacityFreshnessWindow
		if !result.Fresh {
			result.Status = "stale"
			result.NeedsReview = true
			result.Constraints = sortedUnique(append(result.Constraints, "capacity is older than 24 hours"))
		}
	}
	if result.PlanningStepLimit <= 0 {
		result.PlanningStepLimit = capacityStepLimit(result)
	}
	if result.PlanningStepLimit > 20 {
		return CapacitySnapshot{}, fmt.Errorf("capacity planning step limit cannot exceed 20")
	}
	return result, nil
}

func capacityStepLimit(capacity CapacitySnapshot) int {
	switch capacity.Status {
	case "unavailable":
		return 1
	case "constrained", "overloaded", "stale":
		return 3
	case "available":
		if capacity.TimeAvailableMinutes > 0 && capacity.TimeAvailableMinutes < 30 {
			return 3
		}
		return 8
	default:
		return 5
	}
}

func buildAgentCards(required []string, available []AgentCard, ceiling int, now time.Time) ([]AgentCard, error) {
	availableByRole := make(map[string]AgentCard, len(available))
	for _, card := range available {
		normalized, err := normalizeAvailableAgentCard(card, ceiling, now)
		if err != nil {
			return nil, err
		}
		keys := []string{normalized.ID, normalized.Role}
		for _, capability := range normalized.Capabilities {
			keys = append(keys, strings.SplitN(capability, "@", 2)[0])
		}
		keys = append(keys, normalized.DomainCompetence...)
		for _, key := range sortedUnique(keys) {
			key = normalizeIdentifier(key)
			if key == "" {
				continue
			}
			current, exists := availableByRole[key]
			if !exists || preferAgentCard(normalized, current) {
				availableByRole[key] = normalized
			}
		}
	}

	roles := sortedUnique(required)
	embeddedRoles := make([]string, 0, len(roles))
	embeddedDependencies := []string{"framework registry", "task state", "approval policy"}
	for _, role := range roles {
		key := normalizeIdentifier(role)
		if candidate, ok := availableByRole[key]; ok && candidate.Verified {
			continue
		}
		if dependency, ok := embeddedTaskEngineRoleDependency(key); ok {
			embeddedRoles = append(embeddedRoles, key)
			embeddedDependencies = append(embeddedDependencies, dependency)
		}
	}
	result := make([]AgentCard, 0, len(roles)+1)
	result = append(result, AgentCard{
		ID:                    "hai_task_engine",
		Name:                  "HAI task engine",
		Owner:                 "authenticated_owner_scope",
		Purpose:               "coordinate governed task planning, routing, validation, and audit",
		Role:                  "coordinator",
		Capabilities:          sortedUnique(append([]string{"classify", "plan", "route", "validate", "audit"}, embeddedRoles...)),
		DomainCompetence:      sortedUnique(append([]string{"cross_domain_operational_coordination"}, embeddedRoles...)),
		AllowedTools:          []string{"registered allowlisted tools"},
		RequiredPermissions:   []string{"owner-scoped task and framework read"},
		DataAccessBoundaries:  []string{"current authorized task context only"},
		CostProfile:           "local_control_plane_no_spend_authority",
		ModelRequirements:     []string{"none for deterministic contract construction"},
		ReliabilityHistory:    []string{"repository contract tests pass; production effectiveness is not inferred"},
		AllowedActions:        allowedActionsForAuthority(ceiling),
		ProhibitedActions:     protectedAgentProhibitions(),
		InputSchema:           "framework-selection-request-v5",
		OutputSchema:          "framework-selection-decision-v5",
		ExpectedEvidence:      []string{"framework selection and validation audit records"},
		EscalationRoute:       "owner-scoped review queue",
		Availability:          "local process",
		Version:               "selector-v5",
		Dependencies:          sortedUnique(embeddedDependencies),
		HealthStatus:          "available",
		EvaluationScore:       0,
		EvaluationScoreSource: "not calibrated against production outcomes",
		AuthorityCeiling:      ceiling,
		Status:                "available",
		Verified:              true,
		Revoked:               false,
		Provenance:            "embedded_canonical_go_engine",
	})
	for _, role := range roles {
		key := normalizeIdentifier(role)
		if card, ok := availableByRole[key]; ok && card.Verified {
			result = append(result, card)
			continue
		}
		if _, ok := embeddedTaskEngineRoleDependency(key); ok {
			continue
		}
		if card, ok := availableByRole[key]; ok {
			result = append(result, card)
			continue
		}
		result = append(result, AgentCard{
			ID:                    key,
			Name:                  humanizeIdentifier(key),
			Owner:                 "unassigned",
			Purpose:               "satisfy required specialist role " + key,
			Role:                  key,
			Capabilities:          []string{"required capability: " + key},
			DomainCompetence:      []string{key},
			AllowedTools:          []string{},
			RequiredPermissions:   []string{},
			DataAccessBoundaries:  []string{"no data access until assigned and verified"},
			CostProfile:           "unknown_no_spend_authorized",
			ModelRequirements:     []string{},
			ReliabilityHistory:    []string{"no verified runtime history"},
			AllowedActions:        []string{},
			ProhibitedActions:     protectedAgentProhibitions(),
			InputSchema:           "hai-agent-message-v1",
			OutputSchema:          "hai-agent-message-v1",
			ExpectedEvidence:      []string{"fresh runtime capability and health evidence"},
			EscalationRoute:       "hai_task_engine then owner-scoped review queue",
			Availability:          "unassigned",
			Version:               "unreported",
			Dependencies:          []string{},
			HealthStatus:          "unknown",
			EvaluationScore:       0,
			EvaluationScoreSource: "no verified evaluation",
			AuthorityCeiling:      ceiling,
			Status:                "required_unassigned",
			Verified:              false,
			Revoked:               false,
			Provenance:            "framework_requirement_not_runtime_evidence",
		})
	}
	return result, nil
}

// embeddedTaskEngineRoleDependency is an allowlist of role contracts that are
// implemented by deterministic modules in the canonical Go control plane. A
// catalog role not listed here must be backed by a fresh external/runtime agent
// card; it cannot be silently absorbed by the generic task engine.
func embeddedTaskEngineRoleDependency(role string) (string, bool) {
	dependencies := map[string]string{
		"approval_broker":         "execution approval service",
		"capacity_planner":        "resource planner",
		"chief_of_staff":          "task orchestration service",
		"constraint_checker":      "resource planner",
		"context_planner":         "context planning service",
		"critic":                  "task validation service",
		"data_steward":            "source governance service",
		"delegation_manager":      "framework delegation contracts",
		"evaluation_agent":        "outcome evaluation service",
		"evaluation_architect":    "evaluation registry",
		"evidence_agent":          "source evidence service",
		"execution_coordinator":   "execution authorization service",
		"extraction_agent":        "source extraction service",
		"governance_reviewer":     "framework evidence service",
		"intake_agent":            "task intake service",
		"interaction_coordinator": "task interaction service",
		"interoperability_broker": "MCP and A2A bridges",
		"knowledge_curator":       "memory consolidation service",
		"memory_steward":          "memory service",
		"model_router":            "LLM routing service",
		"observability_agent":     "operations and audit services",
		"ontology_steward":        "life ontology service",
		"opportunity_classifier":  "ambient proposal service",
		"planner":                 "task planning service",
		"platform_architect":      "runtime registry",
		"policy_guardian":         "autonomy policy service",
		"portfolio_planner":       "pursuit planning service",
		"privacy_guardian":        "privacy filter service",
		"privacy_reviewer":        "privacy filter service",
		"quality_reviewer":        "quality validation service",
		"reasoning_agent":         "task reasoning service",
		"recovery_coordinator":    "resilience service",
		"red_team_reviewer":       "agent threat-model checks",
		"reliability_monitor":     "durable job service",
		"reliability_reviewer":    "resilience service",
		"retrieval_agent":         "context retrieval service",
		"risk_reviewer":           "task risk service",
		"runtime_architect":       "runtime registry",
		"runtime_broker":          "execution broker",
		"safety_reviewer":         "safety policy service",
		"scheduler":               "durable job scheduler",
		"security_architect":      "execution security service",
		"security_guardian":       "execution security service",
		"security_reviewer":       "execution security service",
		"validation_critic":       "task validation service",
		"verification_critic":     "verification service",
		"workflow_designer":       "workflow service",
		"world_state_observer":    "operating context service",
	}
	dependency, ok := dependencies[normalizeIdentifier(role)]
	return dependency, ok
}

func preferAgentCard(candidate, current AgentCard) bool {
	if candidate.Verified != current.Verified {
		return candidate.Verified
	}
	if candidate.Revoked != current.Revoked {
		return !candidate.Revoked
	}
	if candidate.EvaluationScore != current.EvaluationScore {
		return candidate.EvaluationScore > current.EvaluationScore
	}
	return candidate.ID < current.ID
}

func normalizeAvailableAgentCard(card AgentCard, selectionCeiling int, now time.Time) (AgentCard, error) {
	card.ID = normalizeIdentifier(safety.RedactSecrets(card.ID))
	card.Owner = compactContractText(card.Owner)
	card.Purpose = compactContractText(card.Purpose)
	card.Role = normalizeIdentifier(safety.RedactSecrets(card.Role))
	card.Name = compactContractText(card.Name)
	card.Provenance = compactContractText(card.Provenance)
	card.Status = normalizeIdentifier(safety.RedactSecrets(card.Status))
	card.Capabilities = redactedSortedUnique(card.Capabilities)
	card.DomainCompetence = redactedSortedUnique(card.DomainCompetence)
	card.AllowedTools = redactedSortedUnique(card.AllowedTools)
	card.RequiredPermissions = redactedSortedUnique(card.RequiredPermissions)
	card.DataAccessBoundaries = redactedSortedUnique(card.DataAccessBoundaries)
	card.CostProfile = compactContractText(card.CostProfile)
	card.ModelRequirements = redactedSortedUnique(card.ModelRequirements)
	card.ReliabilityHistory = redactedSortedUnique(card.ReliabilityHistory)
	card.AllowedActions = redactedSortedUnique(card.AllowedActions)
	card.ProhibitedActions = redactedSortedUnique(append(card.ProhibitedActions, protectedAgentProhibitions()...))
	card.InputSchema = normalizeIdentifier(safety.RedactSecrets(card.InputSchema))
	card.OutputSchema = normalizeIdentifier(safety.RedactSecrets(card.OutputSchema))
	card.ExpectedEvidence = redactedSortedUnique(card.ExpectedEvidence)
	card.EscalationRoute = compactContractText(card.EscalationRoute)
	card.Availability = compactContractText(card.Availability)
	card.Version = compactContractText(card.Version)
	card.Dependencies = redactedSortedUnique(card.Dependencies)
	card.HealthStatus = normalizeIdentifier(safety.RedactSecrets(card.HealthStatus))
	card.EvaluationScoreSource = compactContractText(card.EvaluationScoreSource)
	card.RevocationReason = compactContractText(card.RevocationReason)
	if card.ID == "" || card.Name == "" || card.Role == "" || card.Provenance == "" {
		return AgentCard{}, fmt.Errorf("available agent card requires id, name, role, and provenance")
	}
	if card.Owner == "" {
		card.Owner = "authenticated_owner_scope"
	}
	if card.Purpose == "" {
		card.Purpose = "perform the assigned " + card.Role + " role inside the current task"
	}
	if len(card.DomainCompetence) == 0 {
		card.DomainCompetence = append([]string(nil), card.Capabilities...)
	}
	if len(card.DataAccessBoundaries) == 0 {
		card.DataAccessBoundaries = []string{"current authorized task context only"}
	}
	if card.CostProfile == "" {
		card.CostProfile = "unreported_no_spend_authorized"
	}
	if len(card.ReliabilityHistory) == 0 {
		card.ReliabilityHistory = []string{"no retained reliability history"}
	}
	if card.InputSchema == "" {
		card.InputSchema = "hai_agent_message_v1"
	}
	if card.OutputSchema == "" {
		card.OutputSchema = "hai_agent_message_v1"
	}
	if len(card.ExpectedEvidence) == 0 {
		card.ExpectedEvidence = []string{"task-specific evidence contract"}
	}
	if card.EscalationRoute == "" {
		card.EscalationRoute = "hai_task_engine then owner-scoped review queue"
	}
	if card.Availability == "" {
		card.Availability = card.Status
	}
	if card.Version == "" {
		card.Version = "unreported"
	}
	if card.HealthStatus == "" {
		card.HealthStatus = card.Status
	}
	if card.EvaluationScore < 0 || card.EvaluationScore > 1 {
		return AgentCard{}, fmt.Errorf("agent evaluation score must be between 0 and 1")
	}
	if card.EvaluationScoreSource == "" {
		card.EvaluationScoreSource = "unreported"
	}
	if card.Revoked && card.RevocationReason == "" {
		return AgentCard{}, fmt.Errorf("revoked agent card requires a revocation reason")
	}
	if card.AuthorityCeiling < 0 || card.AuthorityCeiling > 10 {
		return AgentCard{}, fmt.Errorf("agent authority ceiling must be between 0 and 10")
	}
	if card.AuthorityCeiling > selectionCeiling {
		card.AuthorityCeiling = selectionCeiling
	}
	card.Verified = false
	if card.LastVerifiedAt != nil {
		verifiedAt := card.LastVerifiedAt.UTC()
		card.LastVerifiedAt = &verifiedAt
		if verifiedAt.After(now.UTC().Add(5 * time.Minute)) {
			return AgentCard{}, fmt.Errorf("agent verification time cannot be in the future")
		}
		card.Verified = now.UTC().Sub(verifiedAt) <= agentFreshnessWindow &&
			card.Status == "available" &&
			card.HealthStatus == "available" &&
			!card.Revoked
	}
	if !card.Verified {
		card.Status = "unverified"
		if card.Revoked {
			card.Status = "revoked"
		}
		card.AllowedTools = []string{}
		card.AllowedActions = []string{}
	}
	return card, nil
}

func protectedAgentProhibitions() []string {
	return []string{
		"approve its own high-risk action",
		"change the Constitution, permissions, or safety policy",
		"grant itself or another agent additional authority",
		"expose credentials or unredacted secrets",
	}
}

func buildCoordinationPlan(request SelectionRequest, cards []AgentCard) (CoordinationPlan, error) {
	verified := make([]string, 0)
	for _, card := range cards {
		if card.Verified {
			verified = append(verified, card.ID)
		}
	}
	verified = sortedUnique(verified)
	mode := "single_engine"
	rationale := "The embedded HAI task engine is the only verified participant; specialist roles remain requirements, not live workers."
	if len(verified) >= 3 && request.Difficulty >= 4 {
		mode = "hierarchical"
		rationale = "A verified coordinator and multiple verified specialists are available for a difficult task."
	} else if len(verified) >= 2 {
		mode = "sequential"
		rationale = "Verified specialists can hand work off through an ordered, auditable sequence."
	}
	if preferred := normalizeIdentifier(request.PreferredCoordinationMode); preferred != "" {
		if !validCoordinationMode(preferred) {
			return CoordinationPlan{}, fmt.Errorf("unsupported coordination mode %q", preferred)
		}
		if preferred != "single_engine" && len(verified) < 2 {
			rationale = "Requested multi-agent coordination is unavailable because fewer than two participants have fresh verification."
		} else {
			mode = preferred
			rationale = "The trusted caller selected an available coordination mode."
		}
	}
	handoff := append([]string(nil), verified...)
	return CoordinationPlan{
		Mode:           mode,
		AllowedModes:   []string{"single_engine", "sequential", "hierarchical", "parallel_specialists", "debate_then_critic"},
		Coordinator:    "hai_task_engine",
		Participants:   verified,
		HandoffOrder:   handoff,
		ConsensusRule:  "No vote grants authority; conflicting or consequential conclusions route to evidence review or the operator.",
		EscalationRule: "Escalate on conflicting evidence, missing authority, unverified participants, failed validation, or a stop condition.",
		Rationale:      rationale,
	}, nil
}

func validCoordinationMode(value string) bool {
	switch value {
	case "single_engine", "sequential", "hierarchical", "parallel_specialists", "debate_then_critic":
		return true
	default:
		return false
	}
}

func buildDelegationContracts(
	request SelectionRequest,
	cards []AgentCard,
	capacity CapacitySnapshot,
	ceiling int,
	requiresApproval bool,
	evidence []string,
	completion []string,
) []DelegationContract {
	var deadline *time.Time
	deadlineStatus := "not_set"
	if request.Deadline != nil {
		value := request.Deadline.UTC()
		deadline = &value
		deadlineStatus = "scheduled"
	}
	constraints := redactedSortedUnique(append(
		append([]string(nil), capacity.Constraints...),
		"no financial expenditure is authorized by this delegation",
		"remain inside the registered tool, folder, network, and secret allowlists",
		"stop and escalate when evidence, authority, or required inputs are missing",
	))
	result := make([]DelegationContract, 0, len(cards))
	for _, card := range cards {
		state := "requires_assignment"
		if card.Verified {
			state = "ready"
		}
		authority := card.AuthorityCeiling
		if authority > ceiling {
			authority = ceiling
		}
		result = append(result, DelegationContract{
			ID:                 deterministicContractID(request.TaskPlanID, request.Request, card.ID),
			Delegator:          "chief_of_staff",
			Delegatee:          card.ID,
			Objective:          compactContractText(request.Request),
			AllowedActions:     append([]string(nil), card.AllowedActions...),
			ProhibitedActions:  append([]string(nil), card.ProhibitedActions...),
			BudgetLimitEUR:     0,
			BudgetPolicy:       "no_spend_authorized",
			Deadline:           deadline,
			DeadlineStatus:     deadlineStatus,
			Constraints:        append([]string(nil), constraints...),
			AuthorityCeiling:   authority,
			RequiresApproval:   requiresApproval,
			EvidenceRequired:   sortedUnique(evidence),
			CompletionCriteria: sortedUnique(completion),
			EscalationTriggers: []string{
				"required source or parameter is missing",
				"evidence conflicts or validation fails",
				"requested action exceeds the authority ceiling",
				"task encounters a destructive, financial, legal, public, or account-changing action",
			},
			State: state,
		})
	}
	return result
}

func deterministicContractID(taskPlanID, request, agentID string) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		strings.TrimSpace(taskPlanID),
		normalizeText(request),
		normalizeIdentifier(agentID),
	}, "\n")))
	return uuid.NewSHA1(uuid.NameSpaceOID, sum[:]).String()
}

func allowedActionsForAuthority(level int) []string {
	actions := []string{"observe authorized context"}
	if level >= 1 {
		actions = append(actions, "inform the operator")
	}
	if level >= 2 {
		actions = append(actions, "recommend a next action")
	}
	if level >= 3 {
		actions = append(actions, "prepare an internal draft")
	}
	if level >= 4 {
		actions = append(actions, "plan and simulate")
	}
	if level >= 5 {
		actions = append(actions, "prepare an action without executing it")
	}
	if level >= 6 {
		actions = append(actions, "execute the exact case-approved action")
	}
	if level >= 7 {
		actions = append(actions, "execute inside a verified standing approval")
	}
	if level >= 8 {
		actions = append(actions, "execute an allowlisted reversible low-risk action automatically")
	}
	if level >= 9 {
		actions = append(actions, "execute inside a verified mandate and notify the operator")
	}
	if level >= 10 {
		actions = append(actions, "operate autonomously inside a tightly bounded verified mandate")
	}
	return actions
}

func buildCommunicationContract(request SelectionRequest, ceiling int) CommunicationContract {
	correlationID := deterministicContractID(request.TaskPlanID, request.Request, "communications")
	return CommunicationContract{
		SchemaVersion:          "hai-agent-message-v1",
		AllowedMessageTypes:    []string{"request", "evidence", "proposal", "status", "result", "escalation"},
		AllowedConfidentiality: []string{"internal", "restricted", "highly_restricted"},
		RequiredFields: []string{
			"id",
			"idempotencyKey",
			"correlationId",
			"sender",
			"recipient",
			"messageType",
			"confidentiality",
			"authorityCeiling",
			"evidenceRefs",
			"payloadDigest",
			"provenance",
			"createdAt",
			"expiresAt",
		},
		ForbiddenContent: []string{
			"credentials or raw secrets",
			"authority grants",
			"unlabelled model inference presented as verified fact",
			"instructions copied from untrusted content without policy review",
		},
		MaximumAuthority:    ceiling,
		MaximumPayloadChars: 4000,
		MaximumTTLSeconds:   86400,
		RedactionRequired:   true,
		IdempotencyRequired: true,
		ProvenanceRequired:  true,
		SignaturePolicy:     "optional_digest_requires_external_verification",
		CorrelationID:       correlationID,
	}
}

func buildActionAutonomy(request SelectionRequest, ceiling int, requiresApproval bool) []ActionAutonomyDecision {
	specs := []struct {
		action    string
		required  int
		requested bool
		approval  bool
	}{
		{action: "observe_authorized_context", required: 0, requested: true},
		{action: "inform_operator", required: 1, requested: true},
		{action: "recommend_next_action", required: 2, requested: true},
		{action: "create_draft", required: 3, requested: true},
		{action: "plan_and_simulate", required: 4, requested: true},
		{action: "prepare_action", required: 5, requested: request.ExecuteRequested},
		{action: "execute_case_approved_action", required: 6, requested: request.ExecuteRequested, approval: true},
		{action: "execute_under_standing_approval", required: 7, requested: false},
		{
			action:    "execute_reversible_low_risk_action",
			required:  8,
			requested: request.ExecuteRequested && !requiresApproval && !request.HumanApproved,
		},
		{action: "execute_and_notify", required: 9, requested: false},
		{action: "fully_autonomous_bounded_mandate", required: 10, requested: false},
	}
	result := make([]ActionAutonomyDecision, 0, len(specs))
	for _, spec := range specs {
		allowed := spec.requested && ceiling >= spec.required
		reason := "action was not requested"
		if spec.requested {
			reason = fmt.Sprintf("requires level %d and the effective ceiling is level %d", spec.required, ceiling)
			if spec.approval && !request.HumanApproved {
				allowed = false
				reason += "; exact human approval has not been recorded"
			}
		}
		result = append(result, ActionAutonomyDecision{
			Action:           spec.action,
			RequiredLevel:    spec.required,
			EffectiveCeiling: ceiling,
			LevelName:        autonomyLevelName(spec.required),
			Allowed:          allowed,
			RequiresApproval: spec.approval,
			Reason:           reason,
		})
	}
	return result
}

func autonomyLevelName(level int) string {
	switch level {
	case 0:
		return "observe_only"
	case 1:
		return "inform"
	case 2:
		return "recommend"
	case 3:
		return "draft"
	case 4:
		return "plan_and_simulate"
	case 5:
		return "prepare_action"
	case 6:
		return "execute_after_case_specific_approval"
	case 7:
		return "execute_under_standing_approval"
	case 8:
		return "execute_reversible_low_risk_automatically"
	case 9:
		return "execute_and_notify"
	case 10:
		return "fully_autonomous_inside_bounded_mandate"
	default:
		return "unknown"
	}
}

func buildStopConditions(
	capacity CapacitySnapshot,
	cards []AgentCard,
	coordination CoordinationPlan,
	requiresApproval bool,
) []string {
	result := []string{
		"stop when authority, required evidence, or a required parameter is missing",
		"stop when validation fails after the bounded retry policy",
		"stop when evidence conflicts or an important claim remains unsupported",
		"stop when the emergency stop is active or a safety invariant is violated",
	}
	if capacity.Status == "unavailable" || capacity.Status == "overloaded" {
		result = append(result, "stop creation of new human commitments while current capacity is unavailable")
	}
	if requiresApproval {
		result = append(result, "stop before the exact approval-gated action until a valid action-bound approval exists")
	}
	if coordination.Mode != "single_engine" {
		for _, card := range cards {
			if card.ID != "hai_task_engine" && !card.Verified {
				result = append(result, "stop multi-agent execution until every assigned participant has fresh capability evidence")
				break
			}
		}
	}
	return sortedUnique(result)
}

func buildOutcomeMonitoring(request SelectionRequest, completion []string) []string {
	result := []string{
		"record state transitions, tool calls, approvals, evidence, validation, and final disposition",
		"keep unresolved work blocked or needs_review rather than complete",
		"schedule follow-up when completion depends on an external response or future check",
		"propose learning only from verified outcomes or operator-confirmed corrections",
	}
	for _, criterion := range completion {
		result = append(result, "verify: "+criterion)
	}
	if request.ExecuteRequested {
		result = append(result, "verify the external or runtime state after execution rather than trusting the command response")
	}
	return sortedUnique(result)
}

func buildChiefOfStaffDecision(
	request SelectionRequest,
	domains []LifeDomainAssignment,
	needs []NeedStateAssessment,
	capacity CapacitySnapshot,
	agents []AgentCard,
	coordination CoordinationPlan,
	autonomy []ActionAutonomyDecision,
	requiresApproval bool,
	approvalReasons []string,
	completion []string,
	context []string,
) ChiefOfStaffDecision {
	primaryDomain := "general_operations"
	if len(domains) > 0 {
		primaryDomain = domains[0].ID
	}
	need := "operator-defined outcome"
	if len(needs) > 0 {
		need = needs[0].Level + " / " + needs[0].State
	}
	actor := coordination.Coordinator
	if len(coordination.Participants) > 1 {
		actor = coordination.Coordinator + " coordinating " + strings.Join(coordination.Participants[1:], ", ")
	}
	mayProceed := "Planning, classification, and authorized context retrieval may proceed within the recorded ceiling."
	for _, action := range autonomy {
		if action.Action == "execute_case_approved_action" && action.Allowed {
			mayProceed = "The exact approved execution may proceed within tool, evidence, runtime, and stop-condition controls."
		}
		if action.Action == "execute_reversible_low_risk_action" && action.Allowed {
			mayProceed = "The reversible low-risk action may proceed automatically within the allowlist, runtime, and stop-condition controls."
		}
	}
	approval := "No additional approval gate was identified."
	if requiresApproval {
		approval = strings.Join(sortedUnique(approvalReasons), "; ")
	}
	return ChiefOfStaffDecision{
		NeedsAttention:  fmt.Sprintf("%s: %s", humanizeIdentifier(primaryDomain), compactContractText(request.Request)),
		WhyNow:          fmt.Sprintf("The request maps to %s and the highest current needs signal is %s.", humanizeIdentifier(primaryDomain), need),
		ContextNeeded:   strings.Join(firstStrings(sortedUnique(context), 5), "; "),
		WhoShouldAct:    actor,
		HowToProceed:    fmt.Sprintf("Use %s coordination, stay within a %d-step human-capacity plan, and follow the selected evidence and stop conditions.", coordination.Mode, capacity.PlanningStepLimit),
		MayProceedNow:   mayProceed,
		NeedsApproval:   approval,
		CompletionProof: strings.Join(firstStrings(sortedUnique(completion), 5), "; "),
	}
}

func normalizeIdentifier(value string) string {
	return strings.Trim(strings.ReplaceAll(normalizeText(value), " ", "_"), "_")
}

func humanizeIdentifier(value string) string {
	value = strings.ReplaceAll(strings.TrimSpace(value), "_", " ")
	if value == "" {
		return "Unknown"
	}
	return strings.ToUpper(value[:1]) + value[1:]
}

func compactContractText(value string) string {
	value = strings.Join(strings.Fields(strings.TrimSpace(safety.RedactSecrets(value))), " ")
	if len([]rune(value)) <= 240 {
		return value
	}
	return string([]rune(value)[:237]) + "..."
}

func redactedSortedUnique(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = compactContractText(value)
		if value != "" {
			result = append(result, value)
		}
	}
	return sortedUnique(result)
}

func firstStrings(values []string, limit int) []string {
	if len(values) <= limit {
		return values
	}
	return values[:limit]
}
