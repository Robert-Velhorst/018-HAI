package frameworkregistry

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"automation-hub-backend/internal/agentcoordination"
	"automation-hub-backend/internal/safety"

	"github.com/google/uuid"
)

func normalizeAgentTeamContract(team AgentTeamContract, now time.Time) (AgentTeamContract, error) {
	if containsAgentTeamSecret(team) {
		return AgentTeamContract{}, fmt.Errorf("team contract contains secret material")
	}
	now = now.UTC()
	if _, err := uuid.Parse(strings.TrimSpace(team.ID)); err != nil {
		return AgentTeamContract{}, fmt.Errorf("team ID must be a UUID")
	}
	team.ID = strings.TrimSpace(team.ID)
	team.Key = normalizeIdentifier(team.Key)
	team.Version = strings.TrimSpace(team.Version)
	team.Name = compactContractText(team.Name)
	team.Purpose = compactContractText(team.Purpose)
	team.Status = normalizeIdentifier(team.Status)
	team.RiskCeiling = normalizeIdentifier(team.RiskCeiling)
	team.MaximumDelegatedRisk = normalizeIdentifier(team.MaximumDelegatedRisk)
	if team.Key == "" || team.Name == "" || team.Purpose == "" {
		return AgentTeamContract{}, fmt.Errorf("team key, name, and purpose are required")
	}
	if !catalogSemanticVersionPattern.MatchString(team.Version) {
		return AgentTeamContract{}, fmt.Errorf("team version must be semantic version x.y.z")
	}
	if team.Revision == 0 {
		return AgentTeamContract{}, fmt.Errorf("team revision must be positive")
	}
	if !containsExact([]string{AgentTeamDraft, AgentTeamActive, AgentTeamSuspended, AgentTeamRetired, AgentTeamRevoked}, team.Status) {
		return AgentTeamContract{}, fmt.Errorf("invalid team status %q", team.Status)
	}
	if team.AuthorityCeiling < 0 || team.AuthorityCeiling > 10 {
		return AgentTeamContract{}, fmt.Errorf("team authority ceiling must be between 0 and 10")
	}
	if _, ok := agentTeamRiskRank(team.RiskCeiling); !ok {
		return AgentTeamContract{}, fmt.Errorf("invalid team risk ceiling %q", team.RiskCeiling)
	}
	if team.MaximumDelegatedAuthority < 0 || team.MaximumDelegatedAuthority > team.AuthorityCeiling {
		return AgentTeamContract{}, fmt.Errorf("delegation authority ceiling exceeds the team ceiling")
	}
	if !agentTeamRiskAtOrBelow(team.MaximumDelegatedRisk, team.RiskCeiling) {
		return AgentTeamContract{}, fmt.Errorf("delegation risk ceiling exceeds the team ceiling")
	}
	if !team.AdvisoryOnly || team.GrantsExecutionAuthority || !team.ExecutionAuthorizationRequired {
		return AgentTeamContract{}, fmt.Errorf("team contracts must remain advisory and require separate execution authorization")
	}
	team.EvidenceRefs = redactedSortedUnique(team.EvidenceRefs)
	if len(team.EvidenceRefs) == 0 {
		return AgentTeamContract{}, fmt.Errorf("team evidence references are required")
	}

	capabilities, capabilityIDs, err := normalizeTeamCapabilities(team.Capabilities, team.AuthorityCeiling, team.RiskCeiling)
	if err != nil {
		return AgentTeamContract{}, err
	}
	team.Capabilities = capabilities
	roles, roleIDs, err := normalizeTeamRoles(team.Roles, capabilityIDs, team.AuthorityCeiling, team.RiskCeiling)
	if err != nil {
		return AgentTeamContract{}, err
	}
	team.Roles = roles
	team.CoordinationPolicy, err = normalizeCoordinationPolicy(team.CoordinationPolicy, team.AuthorityCeiling)
	if err != nil {
		return AgentTeamContract{}, err
	}
	team.Consensus, err = normalizeTeamConsensusPolicy(team.Consensus)
	if err != nil {
		return AgentTeamContract{}, err
	}

	members := make([]TeamMembership, 0, len(team.Members))
	memberIDs := map[string]struct{}{}
	liveAgents := map[string]struct{}{}
	for _, member := range team.Members {
		member, err = normalizeTeamMembership(member, roleIDs, capabilityIDs, team, now)
		if err != nil {
			return AgentTeamContract{}, err
		}
		if _, exists := memberIDs[member.ID]; exists {
			return AgentTeamContract{}, fmt.Errorf("duplicate team membership %s", member.ID)
		}
		memberIDs[member.ID] = struct{}{}
		if member.Status != TeamMemberLeft && member.Status != TeamMemberRevoked {
			if _, exists := liveAgents[member.AgentID]; exists {
				return AgentTeamContract{}, fmt.Errorf("agent %s has more than one live membership", member.AgentID)
			}
			liveAgents[member.AgentID] = struct{}{}
		}
		members = append(members, member)
	}
	sort.SliceStable(members, func(i, j int) bool { return members[i].ID < members[j].ID })
	team.Members = members

	team.Provenance.Source = compactContractText(team.Provenance.Source)
	team.Provenance.Reference = compactContractText(team.Provenance.Reference)
	team.Provenance.AuthoredBy = compactContractText(team.Provenance.AuthoredBy)
	team.Provenance.RegisteredBy = compactContractText(team.Provenance.RegisteredBy)
	if team.Provenance.Source == "" || team.Provenance.AuthoredBy == "" || team.Provenance.RegisteredBy == "" || team.Provenance.RegisteredAt.IsZero() {
		return AgentTeamContract{}, fmt.Errorf("team provenance source, author, registrar, and time are required")
	}
	team.Provenance.RegisteredAt = team.Provenance.RegisteredAt.UTC()
	evidenceDigest, err := agentTeamSHA256(struct {
		Source    string   `json:"source"`
		Reference string   `json:"reference"`
		Evidence  []string `json:"evidence"`
	}{team.Provenance.Source, team.Provenance.Reference, team.EvidenceRefs})
	if err != nil {
		return AgentTeamContract{}, err
	}
	if supplied := strings.ToLower(strings.TrimSpace(team.Provenance.EvidenceDigest)); supplied != "" && supplied != evidenceDigest {
		return AgentTeamContract{}, fmt.Errorf("team provenance evidence digest does not match")
	}
	team.Provenance.EvidenceDigest = evidenceDigest
	team.PreviousVersionDigest = strings.ToLower(strings.TrimSpace(team.PreviousVersionDigest))
	if team.PreviousVersionDigest != "" {
		if err := requireAgentTeamSHA256("previous version digest", team.PreviousVersionDigest); err != nil {
			return AgentTeamContract{}, err
		}
	}
	if team.CreatedAt.IsZero() || team.UpdatedAt.IsZero() {
		return AgentTeamContract{}, fmt.Errorf("team lifecycle timestamps are required")
	}
	team.CreatedAt = team.CreatedAt.UTC()
	team.UpdatedAt = team.UpdatedAt.UTC()
	if team.UpdatedAt.Before(team.CreatedAt) || team.CreatedAt.After(now.Add(5*time.Minute)) {
		return AgentTeamContract{}, fmt.Errorf("team lifecycle timestamps are invalid")
	}
	normalizeTeamLifecycleTimes(&team)
	if err := validateTeamStatusTimes(team); err != nil {
		return AgentTeamContract{}, err
	}
	expectedDigest, err := agentTeamContractDigest(team)
	if err != nil {
		return AgentTeamContract{}, err
	}
	if supplied := strings.ToLower(strings.TrimSpace(team.ContractDigest)); supplied != "" && supplied != expectedDigest {
		return AgentTeamContract{}, fmt.Errorf("team contract digest does not match its content")
	}
	team.ContractDigest = expectedDigest
	return team, nil
}

func normalizeTeamCapabilities(values []TeamCapabilityContract, authority int, risk string) ([]TeamCapabilityContract, map[string]TeamCapabilityContract, error) {
	if len(values) == 0 {
		return nil, nil, fmt.Errorf("at least one team capability is required")
	}
	result := make([]TeamCapabilityContract, 0, len(values))
	byID := map[string]TeamCapabilityContract{}
	for _, value := range values {
		value.ID = normalizeIdentifier(value.ID)
		value.Name = compactContractText(value.Name)
		value.Description = compactContractText(value.Description)
		value.InputSchema = compactContractText(value.InputSchema)
		value.OutputSchema = compactContractText(value.OutputSchema)
		value.EvidenceRequired = redactedSortedUnique(value.EvidenceRequired)
		value.ProhibitedActions = redactedSortedUnique(value.ProhibitedActions)
		value.RiskCeiling = normalizeIdentifier(value.RiskCeiling)
		if value.ID == "" || value.Name == "" || value.Description == "" || value.InputSchema == "" || value.OutputSchema == "" || len(value.EvidenceRequired) == 0 || len(value.ProhibitedActions) == 0 {
			return nil, nil, fmt.Errorf("team capability identity, description, and schemas are required")
		}
		if _, exists := byID[value.ID]; exists {
			return nil, nil, fmt.Errorf("duplicate team capability %s", value.ID)
		}
		if !value.AdvisoryOnly {
			return nil, nil, fmt.Errorf("team capability %s must remain advisory", value.ID)
		}
		if value.AuthorityCeiling < 0 || value.AuthorityCeiling > authority || !agentTeamRiskAtOrBelow(value.RiskCeiling, risk) {
			return nil, nil, fmt.Errorf("team capability %s exceeds team authority or risk ceiling", value.ID)
		}
		byID[value.ID] = value
		result = append(result, value)
	}
	sort.SliceStable(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, byID, nil
}

func normalizeTeamRoles(values []TeamRoleContract, capabilities map[string]TeamCapabilityContract, authority int, risk string) ([]TeamRoleContract, map[string]TeamRoleContract, error) {
	if len(values) == 0 {
		return nil, nil, fmt.Errorf("at least one team role is required")
	}
	result := make([]TeamRoleContract, 0, len(values))
	byID := map[string]TeamRoleContract{}
	for _, value := range values {
		value.ID = normalizeIdentifier(value.ID)
		value.Name = compactContractText(value.Name)
		value.Purpose = compactContractText(value.Purpose)
		value.CapabilityIDs = normalizedAgentTeamIdentifiers(value.CapabilityIDs)
		value.AllowedRecommendationTypes = normalizedAgentTeamIdentifiers(value.AllowedRecommendationTypes)
		value.ProhibitedActions = redactedSortedUnique(value.ProhibitedActions)
		value.EvidenceRequirements = redactedSortedUnique(value.EvidenceRequirements)
		value.RiskCeiling = normalizeIdentifier(value.RiskCeiling)
		if value.ID == "" || value.Name == "" || value.Purpose == "" || len(value.CapabilityIDs) == 0 || len(value.AllowedRecommendationTypes) == 0 || len(value.ProhibitedActions) == 0 || len(value.EvidenceRequirements) == 0 {
			return nil, nil, fmt.Errorf("team role identity, purpose, and capabilities are required")
		}
		if _, exists := byID[value.ID]; exists {
			return nil, nil, fmt.Errorf("duplicate team role %s", value.ID)
		}
		for _, capabilityID := range value.CapabilityIDs {
			if _, exists := capabilities[capabilityID]; !exists {
				return nil, nil, fmt.Errorf("team role %s references unknown capability %s", value.ID, capabilityID)
			}
		}
		if !value.AdvisoryOnly || value.AuthorityCeiling < 0 || value.AuthorityCeiling > authority || !agentTeamRiskAtOrBelow(value.RiskCeiling, risk) {
			return nil, nil, fmt.Errorf("team role %s violates advisory, authority, or risk boundaries", value.ID)
		}
		byID[value.ID] = value
		result = append(result, value)
	}
	sort.SliceStable(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, byID, nil
}

func normalizeTeamMembership(member TeamMembership, roles map[string]TeamRoleContract, capabilities map[string]TeamCapabilityContract, team AgentTeamContract, now time.Time) (TeamMembership, error) {
	if _, err := uuid.Parse(strings.TrimSpace(member.ID)); err != nil {
		return TeamMembership{}, fmt.Errorf("membership ID must be a UUID")
	}
	member.ID = strings.TrimSpace(member.ID)
	member.AgentID = normalizeIdentifier(member.AgentID)
	member.AgentVersion = strings.TrimSpace(member.AgentVersion)
	member.RoleIDs = normalizedAgentTeamIdentifiers(member.RoleIDs)
	member.CapabilityIDs = normalizedAgentTeamIdentifiers(member.CapabilityIDs)
	member.Status = normalizeIdentifier(member.Status)
	member.RiskCeiling = normalizeIdentifier(member.RiskCeiling)
	member.EvidenceRefs = redactedSortedUnique(member.EvidenceRefs)
	if member.AgentID == "" || !catalogSemanticVersionPattern.MatchString(member.AgentVersion) || len(member.RoleIDs) == 0 || len(member.CapabilityIDs) == 0 || len(member.EvidenceRefs) == 0 {
		return TeamMembership{}, fmt.Errorf("membership agent identity, semantic version, and role are required")
	}
	if !containsExact([]string{TeamMemberInvited, TeamMemberActive, TeamMemberSuspended, TeamMemberLeft, TeamMemberRevoked}, member.Status) {
		return TeamMembership{}, fmt.Errorf("invalid membership status %q", member.Status)
	}
	allowedCapabilities := map[string]struct{}{}
	maximumAuthority := team.AuthorityCeiling
	maximumRisk := team.RiskCeiling
	for _, roleID := range member.RoleIDs {
		role, exists := roles[roleID]
		if !exists {
			return TeamMembership{}, fmt.Errorf("membership references unknown role %s", roleID)
		}
		if role.AuthorityCeiling < maximumAuthority {
			maximumAuthority = role.AuthorityCeiling
		}
		if agentTeamRiskRankValue(role.RiskCeiling) < agentTeamRiskRankValue(maximumRisk) {
			maximumRisk = role.RiskCeiling
		}
		for _, capabilityID := range role.CapabilityIDs {
			allowedCapabilities[capabilityID] = struct{}{}
		}
	}
	for _, capabilityID := range member.CapabilityIDs {
		if _, exists := capabilities[capabilityID]; !exists {
			return TeamMembership{}, fmt.Errorf("membership references unknown capability %s", capabilityID)
		}
		if _, allowed := allowedCapabilities[capabilityID]; !allowed {
			return TeamMembership{}, fmt.Errorf("membership capability %s is not assigned by its roles", capabilityID)
		}
	}
	if member.AuthorityCeiling < 0 || member.AuthorityCeiling > maximumAuthority || !agentTeamRiskAtOrBelow(member.RiskCeiling, maximumRisk) {
		return TeamMembership{}, fmt.Errorf("membership exceeds role or team authority or risk ceiling")
	}
	if member.StatusChangedAt.IsZero() {
		return TeamMembership{}, fmt.Errorf("membership status change time is required")
	}
	member.StatusChangedAt = member.StatusChangedAt.UTC()
	member.JoinedAt = agentTeamUTCTimePointer(member.JoinedAt)
	member.RevokedAt = agentTeamUTCTimePointer(member.RevokedAt)
	member.RevocationReason = compactContractText(member.RevocationReason)
	if member.Status == TeamMemberActive && member.JoinedAt == nil {
		return TeamMembership{}, fmt.Errorf("active membership requires joined time")
	}
	if member.Status == TeamMemberRevoked && (member.RevokedAt == nil || member.RevocationReason == "") {
		return TeamMembership{}, fmt.Errorf("revoked membership requires time and reason")
	}
	if member.StatusChangedAt.After(now.Add(5 * time.Minute)) {
		return TeamMembership{}, fmt.Errorf("membership status change cannot be in the future")
	}
	digest, err := agentTeamSHA256(struct {
		AgentID      string   `json:"agentId"`
		AgentVersion string   `json:"agentVersion"`
		Roles        []string `json:"roles"`
		Capabilities []string `json:"capabilities"`
		Evidence     []string `json:"evidence"`
	}{member.AgentID, member.AgentVersion, member.RoleIDs, member.CapabilityIDs, member.EvidenceRefs})
	if err != nil {
		return TeamMembership{}, err
	}
	if supplied := strings.ToLower(strings.TrimSpace(member.ProvenanceDigest)); supplied != "" && supplied != digest {
		return TeamMembership{}, fmt.Errorf("membership provenance digest does not match")
	}
	member.ProvenanceDigest = digest
	return member, nil
}

func normalizeCoordinationPolicy(policy agentcoordination.ValidationPolicy, teamAuthority int) (agentcoordination.ValidationPolicy, error) {
	policy.SchemaVersion = strings.TrimSpace(policy.SchemaVersion)
	if policy.SchemaVersion == "" || policy.MaximumAuthority < 0 || policy.MaximumAuthority > teamAuthority || policy.MaximumMessageTTL <= 0 || policy.MaximumMessageTTL > 24*time.Hour || policy.MaximumPayloadBytes <= 0 {
		return agentcoordination.ValidationPolicy{}, fmt.Errorf("agent coordination policy has invalid schema, authority, TTL, or payload limits")
	}
	if len(policy.AllowedMessageTypes) == 0 || len(policy.AllowedConfidentiality) == 0 || len(policy.AllowedExecutionModes) == 0 {
		return agentcoordination.ValidationPolicy{}, fmt.Errorf("agent coordination policy allowlists are required")
	}
	if !policy.RequireProvenance || !policy.RequireDecisionEvidence || !policy.RequireRedaction {
		return agentcoordination.ValidationPolicy{}, fmt.Errorf("agent coordination policy must require provenance, decision evidence, and redaction")
	}
	sort.SliceStable(policy.AllowedMessageTypes, func(i, j int) bool { return policy.AllowedMessageTypes[i] < policy.AllowedMessageTypes[j] })
	sort.SliceStable(policy.AllowedConfidentiality, func(i, j int) bool { return policy.AllowedConfidentiality[i] < policy.AllowedConfidentiality[j] })
	sort.SliceStable(policy.AllowedExecutionModes, func(i, j int) bool { return policy.AllowedExecutionModes[i] < policy.AllowedExecutionModes[j] })
	return policy, nil
}

func normalizeTeamConsensusPolicy(policy TeamConsensusPolicy) (TeamConsensusPolicy, error) {
	policy.Mode = normalizeIdentifier(policy.Mode)
	policy.DecisionPayloadSchema = strings.TrimSpace(policy.DecisionPayloadSchema)
	policy.TieOutcome = normalizeIdentifier(policy.TieOutcome)
	if !containsExact([]string{TeamConsensusUnanimous, TeamConsensusMajority, TeamConsensusQuorum}, policy.Mode) || policy.DecisionPayloadSchema == "" {
		return TeamConsensusPolicy{}, fmt.Errorf("invalid consensus mode or decision payload schema")
	}
	if policy.Quorum <= 0 || policy.MinimumSupport <= 0 || policy.MinimumSupport > policy.Quorum {
		return TeamConsensusPolicy{}, fmt.Errorf("consensus quorum and minimum support are invalid")
	}
	if !containsExact([]string{TeamOutcomeConflicted, TeamOutcomeEscalated}, policy.TieOutcome) {
		return TeamConsensusPolicy{}, fmt.Errorf("consensus tie outcome must be conflicted or escalated")
	}
	return policy, nil
}

func validateTeamCoordinationMessage(team AgentTeamContract, message agentcoordination.Message, now time.Time) error {
	if team.Status != AgentTeamActive {
		return fmt.Errorf("team must be active to accept coordination messages")
	}
	sender, ok := activeMemberByAgentID(team, message.Sender.ID)
	if !ok {
		return fmt.Errorf("coordination message sender is not an active team member")
	}
	recipient, ok := activeMemberByAgentID(team, message.Recipient.ID)
	if !ok {
		return fmt.Errorf("coordination message recipient is not an active team member")
	}
	if message.AuthorityLevel > team.AuthorityCeiling || message.AuthorityLevel > sender.AuthorityCeiling || message.AuthorityLevel > recipient.AuthorityCeiling {
		return fmt.Errorf("coordination message exceeds team or membership authority ceiling")
	}
	if !memberHasRole(sender, message.Sender.Role) || !memberHasRole(recipient, message.Recipient.Role) {
		return fmt.Errorf("coordination message role does not match team membership")
	}
	if message.Type == agentcoordination.MessageTypeDecision && !teamMemberMayVote(team, sender.ID) {
		return fmt.Errorf("coordination decision sender does not hold a voting role")
	}
	return agentcoordination.ValidateMessage(team.CoordinationPolicy, message, now.UTC())
}

func evaluateTeamConsensus(team AgentTeamContract, messages []agentcoordination.Message, correlationID, idempotencyKey, issue string, now time.Time) (TeamConsensusOutcome, error) {
	if _, err := uuid.Parse(strings.TrimSpace(correlationID)); err != nil {
		return TeamConsensusOutcome{}, fmt.Errorf("consensus correlation ID must be a UUID")
	}
	if _, err := uuid.Parse(strings.TrimSpace(idempotencyKey)); err != nil {
		return TeamConsensusOutcome{}, fmt.Errorf("consensus idempotency key must be a UUID")
	}
	if containsAgentTeamSecret(issue) {
		return TeamConsensusOutcome{}, fmt.Errorf("consensus issue contains secret material")
	}
	issue = compactContractText(issue)
	if issue == "" {
		return TeamConsensusOutcome{}, fmt.Errorf("consensus issue is required")
	}
	type decisionPayload struct {
		Position       string `json:"position"`
		Recommendation string `json:"recommendation"`
	}
	seenAgents := map[string]struct{}{}
	decisionMessages := make([]agentcoordination.Message, 0)
	evidence := make([]string, 0)
	recommendations := make([]string, 0)
	support, oppose, abstain := 0, 0, 0
	for _, message := range messages {
		if message.CorrelationID != correlationID || message.Type != agentcoordination.MessageTypeDecision {
			continue
		}
		if err := validateTeamCoordinationMessage(team, message, now); err != nil {
			return TeamConsensusOutcome{}, err
		}
		if message.Payload.Schema != team.Consensus.DecisionPayloadSchema {
			return TeamConsensusOutcome{}, fmt.Errorf("decision payload schema does not match team consensus contract")
		}
		if _, exists := seenAgents[message.Sender.ID]; exists {
			return TeamConsensusOutcome{}, fmt.Errorf("agent %s submitted more than one decision", message.Sender.ID)
		}
		seenAgents[message.Sender.ID] = struct{}{}
		var payload decisionPayload
		if err := json.Unmarshal(message.Payload.Data, &payload); err != nil {
			return TeamConsensusOutcome{}, fmt.Errorf("decode decision payload: %w", err)
		}
		payload.Position = normalizeIdentifier(payload.Position)
		payload.Recommendation = compactContractText(payload.Recommendation)
		switch payload.Position {
		case TeamVoteSupport:
			support++
		case TeamVoteOppose:
			oppose++
		case TeamVoteAbstain:
			if !team.Consensus.AllowAbstention {
				return TeamConsensusOutcome{}, fmt.Errorf("consensus policy does not allow abstention")
			}
			abstain++
		default:
			return TeamConsensusOutcome{}, fmt.Errorf("invalid decision position %q", payload.Position)
		}
		if payload.Recommendation != "" {
			recommendations = append(recommendations, payload.Recommendation)
		}
		evidence = append(evidence, message.EvidenceRefs...)
		decisionMessages = append(decisionMessages, message)
	}
	if team.Consensus.RequireEvidence && len(redactedSortedUnique(evidence)) == 0 {
		return TeamConsensusOutcome{}, fmt.Errorf("consensus outcome requires evidence")
	}
	conflicts := agentcoordination.DetectConflicts(decisionMessages, nil, now.UTC())
	status := TeamOutcomeConflicted
	participation := support + oppose + abstain
	switch {
	case participation < team.Consensus.Quorum:
		status = TeamOutcomeInsufficient
	case len(conflicts) > 0 && team.Consensus.ConflictEscalationRequired:
		status = TeamOutcomeEscalated
	case len(conflicts) > 0:
		status = TeamOutcomeConflicted
	case team.Consensus.Mode == TeamConsensusUnanimous && oppose == 0 && support >= team.Consensus.MinimumSupport:
		status = TeamOutcomeReached
	case team.Consensus.Mode == TeamConsensusMajority && support > oppose && support >= team.Consensus.MinimumSupport:
		status = TeamOutcomeReached
	case team.Consensus.Mode == TeamConsensusQuorum && support >= team.Consensus.MinimumSupport:
		status = TeamOutcomeReached
	case support == oppose && support > 0:
		status = team.Consensus.TieOutcome
	case team.Consensus.ConflictEscalationRequired:
		status = TeamOutcomeEscalated
	}
	messageIDs := make([]string, 0, len(decisionMessages))
	for _, message := range decisionMessages {
		messageIDs = append(messageIDs, message.ID)
	}
	recommendations = sortedUnique(recommendations)
	recommendation := ""
	if status == TeamOutcomeReached && len(recommendations) == 1 {
		recommendation = recommendations[0]
	} else if status == TeamOutcomeReached {
		status = TeamOutcomeConflicted
	}
	outcome := TeamConsensusOutcome{
		ID:                             uuid.NewSHA1(uuid.NameSpaceOID, []byte(team.ID+"|"+team.Version+"|"+idempotencyKey)).String(),
		TeamID:                         team.ID,
		TeamVersion:                    team.Version,
		CorrelationID:                  correlationID,
		IdempotencyKey:                 idempotencyKey,
		Issue:                          issue,
		Mode:                           team.Consensus.Mode,
		Status:                         status,
		Recommendation:                 recommendation,
		DecisionMessageIDs:             sortedUnique(messageIDs),
		Conflicts:                      conflicts,
		SupportCount:                   support,
		OpposeCount:                    oppose,
		AbstainCount:                   abstain,
		EvidenceRefs:                   redactedSortedUnique(evidence),
		AdvisoryOnly:                   true,
		GrantsExecutionAuthority:       false,
		ExecutionAuthorizationRequired: true,
		RecordedAt:                     team.UpdatedAt.UTC(),
	}
	provenance, err := agentTeamSHA256(struct {
		TeamDigest string   `json:"teamDigest"`
		Messages   []string `json:"messages"`
		Evidence   []string `json:"evidence"`
	}{team.ContractDigest, outcome.DecisionMessageIDs, outcome.EvidenceRefs})
	if err != nil {
		return TeamConsensusOutcome{}, err
	}
	outcome.ProvenanceDigest = provenance
	outcome.OutcomeDigest, err = teamConsensusOutcomeDigest(outcome)
	return outcome, err
}

func agentTeamContractDigest(team AgentTeamContract) (string, error) {
	team.ContractDigest = ""
	return agentTeamSHA256(team)
}

func teamConsensusOutcomeDigest(value TeamConsensusOutcome) (string, error) {
	value.OutcomeDigest = ""
	return agentTeamSHA256(value)
}

func teamLifecycleEventDigest(event TeamLifecycleEvent) (string, error) {
	event.EventDigest = ""
	return agentTeamSHA256(event)
}

func agentTeamSHA256(value any) (string, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}

func requireAgentTeamSHA256(name, value string) error {
	decoded, err := hex.DecodeString(strings.ToLower(strings.TrimSpace(value)))
	if err != nil || len(decoded) != sha256.Size {
		return fmt.Errorf("%s must be a SHA-256 digest", name)
	}
	return nil
}

func normalizedAgentTeamIdentifiers(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value = normalizeIdentifier(value); value != "" {
			result = append(result, value)
		}
	}
	return sortedUnique(result)
}

func agentTeamRiskRank(value string) (int, bool) {
	switch normalizeIdentifier(value) {
	case TeamRiskLow:
		return 0, true
	case TeamRiskMedium:
		return 1, true
	case TeamRiskHigh:
		return 2, true
	case TeamRiskCritical:
		return 3, true
	default:
		return 0, false
	}
}

func agentTeamRiskRankValue(value string) int {
	rank, _ := agentTeamRiskRank(value)
	return rank
}

func agentTeamRiskAtOrBelow(value, ceiling string) bool {
	valueRank, valueOK := agentTeamRiskRank(value)
	ceilingRank, ceilingOK := agentTeamRiskRank(ceiling)
	return valueOK && ceilingOK && valueRank <= ceilingRank
}

func activeMemberByAgentID(team AgentTeamContract, agentID string) (TeamMembership, bool) {
	agentID = normalizeIdentifier(agentID)
	for _, member := range team.Members {
		if member.AgentID == agentID && member.Status == TeamMemberActive {
			return member, true
		}
	}
	return TeamMembership{}, false
}

func memberHasRole(member TeamMembership, role string) bool {
	return containsExact(member.RoleIDs, normalizeIdentifier(role))
}

func teamMemberMayVote(team AgentTeamContract, membershipID string) bool {
	for _, member := range team.Members {
		if member.ID != membershipID || member.Status != TeamMemberActive {
			continue
		}
		for _, roleID := range member.RoleIDs {
			for _, role := range team.Roles {
				if role.ID == roleID && role.MayVote {
					return true
				}
			}
		}
	}
	return false
}

func containsAgentTeamSecret(value any) bool {
	// These typed fields are identity/policy metadata, not credentials. Keep
	// their values in the scan without treating their schema labels as secrets.
	if team, ok := value.(AgentTeamContract); ok {
		type contract AgentTeamContract
		value = struct {
			contract
			Key                            string `json:"key,omitempty"`
			ExecutionAuthorizationRequired *bool  `json:"executionAuthorizationRequired,omitempty"`
			TeamIdentifier                 string `json:"teamIdentifier"`
			ExecutionApprovalRequired      bool   `json:"executionApprovalRequired"`
		}{contract: contract(team), TeamIdentifier: team.Key,
			ExecutionApprovalRequired: team.ExecutionAuthorizationRequired}
	}
	payload, err := json.Marshal(value)
	if err != nil {
		return true
	}
	text := string(payload)
	return safety.RedactSecrets(text) != text
}

func agentTeamUTCTimePointer(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	normalized := value.UTC()
	return &normalized
}

func normalizeTeamLifecycleTimes(team *AgentTeamContract) {
	team.ActivatedAt = agentTeamUTCTimePointer(team.ActivatedAt)
	team.SuspendedAt = agentTeamUTCTimePointer(team.SuspendedAt)
	team.RetiredAt = agentTeamUTCTimePointer(team.RetiredAt)
	team.RevokedAt = agentTeamUTCTimePointer(team.RevokedAt)
	team.RevocationReason = compactContractText(team.RevocationReason)
}

func validateTeamStatusTimes(team AgentTeamContract) error {
	switch team.Status {
	case AgentTeamActive:
		if team.ActivatedAt == nil {
			return fmt.Errorf("active team requires activation time")
		}
	case AgentTeamSuspended:
		if team.SuspendedAt == nil {
			return fmt.Errorf("suspended team requires suspension time")
		}
	case AgentTeamRetired:
		if team.RetiredAt == nil {
			return fmt.Errorf("retired team requires retirement time")
		}
	case AgentTeamRevoked:
		if team.RevokedAt == nil || team.RevocationReason == "" {
			return fmt.Errorf("revoked team requires time and reason")
		}
	}
	return nil
}
