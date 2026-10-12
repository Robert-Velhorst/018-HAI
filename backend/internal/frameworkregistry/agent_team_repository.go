package frameworkregistry

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"automation-hub-backend/internal/agentcoordination"
)

var (
	ErrAgentTeamNotFound               = errors.New("agent team not found")
	ErrAgentTeamMessageNotFound        = errors.New("agent team coordination message not found")
	ErrAgentTeamRevisionConflict       = errors.New("agent team revision conflict")
	ErrAgentTeamIdempotencyConflict    = errors.New("agent team idempotency conflict")
	ErrAgentTeamAcknowledgmentTerminal = errors.New("agent team message acknowledgment is terminal")
)

// AgentTeamRepository persists lifecycle metadata and canonical coordination
// records. It is separate from both the framework preference repository and
// execution authorization stores.
type AgentTeamRepository interface {
	CreateTeam(owner string, team AgentTeamContract, event TeamLifecycleEvent) error
	UpdateTeam(owner string, team AgentTeamContract, expectedRevision uint64, event TeamLifecycleEvent) error
	GetTeam(owner, teamID, version string) (AgentTeamContract, error)
	ListTeams(owner string) ([]AgentTeamContract, error)
	ListTeamVersions(owner, teamID string) ([]AgentTeamContract, error)
	ListTeamEvents(owner, teamID, version string) ([]TeamLifecycleEvent, error)
	AppendCoordinationMessage(owner, teamID, version string, message agentcoordination.Message) (agentcoordination.Message, bool, error)
	GetCoordinationMessage(owner, teamID, version, messageID string) (agentcoordination.Message, error)
	ListCoordinationMessages(owner, teamID, version, correlationID string) ([]agentcoordination.Message, error)
	AppendMessageAcknowledgment(owner, teamID, version string, acknowledgment agentcoordination.Acknowledgment) (agentcoordination.Acknowledgment, bool, error)
	ListMessageAcknowledgments(owner, teamID, version, messageID string) ([]agentcoordination.Acknowledgment, error)
	RecordConsensusOutcome(owner string, outcome TeamConsensusOutcome, team AgentTeamContract, expectedRevision uint64, event TeamLifecycleEvent) (TeamConsensusOutcome, bool, error)
	ListConsensusOutcomes(owner, teamID, version string) ([]TeamConsensusOutcome, error)
}

// MemoryAgentTeamRepository provides deterministic owner-scoped repository
// semantics. Sharing one instance across service instances proves lifecycle
// durability independently of service object lifetime.
type MemoryAgentTeamRepository struct {
	mu                 sync.RWMutex
	teams              map[string]map[string]map[string]AgentTeamContract
	teamKeys           map[string]map[string]string
	events             map[string][]TeamLifecycleEvent
	messages           map[string][]agentcoordination.Message
	messageIdempotency map[string]agentcoordination.Message
	acknowledgments    map[string][]agentcoordination.Acknowledgment
	ackIdempotency     map[string]agentcoordination.Acknowledgment
	outcomes           map[string][]TeamConsensusOutcome
	outcomeIdempotency map[string]TeamConsensusOutcome
}

func NewMemoryAgentTeamRepository() *MemoryAgentTeamRepository {
	return &MemoryAgentTeamRepository{
		teams:              map[string]map[string]map[string]AgentTeamContract{},
		teamKeys:           map[string]map[string]string{},
		events:             map[string][]TeamLifecycleEvent{},
		messages:           map[string][]agentcoordination.Message{},
		messageIdempotency: map[string]agentcoordination.Message{},
		acknowledgments:    map[string][]agentcoordination.Acknowledgment{},
		ackIdempotency:     map[string]agentcoordination.Acknowledgment{},
		outcomes:           map[string][]TeamConsensusOutcome{},
		outcomeIdempotency: map[string]TeamConsensusOutcome{},
	}
}

func (r *MemoryAgentTeamRepository) CreateTeam(owner string, team AgentTeamContract, event TeamLifecycleEvent) error {
	owner, err := normalizeOwner(owner)
	if err != nil {
		return err
	}
	if err := validateStoredTeamAndEvent(team, event, 0, ""); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ensureInitialized()
	if r.teams[owner] == nil {
		r.teams[owner] = map[string]map[string]AgentTeamContract{}
	}
	if r.teamKeys[owner] == nil {
		r.teamKeys[owner] = map[string]string{}
	}
	if existingID, exists := r.teamKeys[owner][team.Key]; exists && existingID != team.ID {
		return fmt.Errorf("team key %s already belongs to another team", team.Key)
	}
	if r.teams[owner][team.ID] == nil {
		r.teams[owner][team.ID] = map[string]AgentTeamContract{}
	}
	if _, exists := r.teams[owner][team.ID][team.Version]; exists {
		return fmt.Errorf("team %s version %s already exists", team.ID, team.Version)
	}
	r.teams[owner][team.ID][team.Version] = cloneAgentTeam(team)
	r.teamKeys[owner][team.Key] = team.ID
	r.events[teamVersionKey(owner, team.ID, team.Version)] = []TeamLifecycleEvent{cloneTeamEvent(event)}
	return nil
}

func (r *MemoryAgentTeamRepository) UpdateTeam(owner string, team AgentTeamContract, expectedRevision uint64, event TeamLifecycleEvent) error {
	owner, err := normalizeOwner(owner)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ensureInitialized()
	versions := r.teams[owner][team.ID]
	current, exists := versions[team.Version]
	if !exists {
		return ErrAgentTeamNotFound
	}
	if current.Revision != expectedRevision {
		return fmt.Errorf("%w: expected %d, current %d", ErrAgentTeamRevisionConflict, expectedRevision, current.Revision)
	}
	events := r.events[teamVersionKey(owner, team.ID, team.Version)]
	previousDigest := ""
	if len(events) > 0 {
		previousDigest = events[len(events)-1].EventDigest
	}
	if err := validateStoredTeamAndEvent(team, event, expectedRevision, previousDigest); err != nil {
		return err
	}
	if current.Key != team.Key || current.ID != team.ID || current.Version != team.Version || !current.CreatedAt.Equal(team.CreatedAt) {
		return fmt.Errorf("team immutable identity fields cannot change")
	}
	versions[team.Version] = cloneAgentTeam(team)
	r.events[teamVersionKey(owner, team.ID, team.Version)] = append(events, cloneTeamEvent(event))
	return nil
}

func (r *MemoryAgentTeamRepository) GetTeam(owner, teamID, version string) (AgentTeamContract, error) {
	owner, err := normalizeOwner(owner)
	if err != nil {
		return AgentTeamContract{}, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	team, exists := r.teams[owner][strings.TrimSpace(teamID)][strings.TrimSpace(version)]
	if !exists {
		return AgentTeamContract{}, ErrAgentTeamNotFound
	}
	return cloneAgentTeam(team), nil
}

func (r *MemoryAgentTeamRepository) ListTeams(owner string) ([]AgentTeamContract, error) {
	owner, err := normalizeOwner(owner)
	if err != nil {
		return nil, err
	}
	r.mu.RLock()
	result := make([]AgentTeamContract, 0, len(r.teams[owner]))
	for _, versions := range r.teams[owner] {
		var latest *AgentTeamContract
		for _, team := range versions {
			candidate := cloneAgentTeam(team)
			if latest == nil || compareSemanticVersions(candidate.Version, latest.Version) > 0 {
				latest = &candidate
			}
		}
		if latest != nil {
			result = append(result, *latest)
		}
	}
	r.mu.RUnlock()
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].Key != result[j].Key {
			return result[i].Key < result[j].Key
		}
		return result[i].ID < result[j].ID
	})
	return result, nil
}

func (r *MemoryAgentTeamRepository) ListTeamVersions(owner, teamID string) ([]AgentTeamContract, error) {
	owner, err := normalizeOwner(owner)
	if err != nil {
		return nil, err
	}
	r.mu.RLock()
	versions := r.teams[owner][strings.TrimSpace(teamID)]
	result := make([]AgentTeamContract, 0, len(versions))
	for _, team := range versions {
		result = append(result, cloneAgentTeam(team))
	}
	r.mu.RUnlock()
	sort.SliceStable(result, func(i, j int) bool {
		return compareSemanticVersions(result[i].Version, result[j].Version) > 0
	})
	return result, nil
}

func (r *MemoryAgentTeamRepository) ListTeamEvents(owner, teamID, version string) ([]TeamLifecycleEvent, error) {
	owner, err := normalizeOwner(owner)
	if err != nil {
		return nil, err
	}
	r.mu.RLock()
	stored := r.events[teamVersionKey(owner, strings.TrimSpace(teamID), strings.TrimSpace(version))]
	result := make([]TeamLifecycleEvent, len(stored))
	for index := range stored {
		result[index] = cloneTeamEvent(stored[index])
	}
	r.mu.RUnlock()
	return result, nil
}

func (r *MemoryAgentTeamRepository) AppendCoordinationMessage(owner, teamID, version string, message agentcoordination.Message) (agentcoordination.Message, bool, error) {
	owner, err := normalizeOwner(owner)
	if err != nil {
		return agentcoordination.Message{}, false, err
	}
	teamID = strings.TrimSpace(teamID)
	version = strings.TrimSpace(version)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ensureInitialized()
	if _, exists := r.teams[owner][teamID][version]; !exists {
		return agentcoordination.Message{}, false, ErrAgentTeamNotFound
	}
	key := teamIdempotencyKey(owner, teamID, version, message.IdempotencyKey)
	if existing, exists := r.messageIdempotency[key]; exists {
		if existing.PayloadDigest != message.PayloadDigest {
			return agentcoordination.Message{}, false, ErrAgentTeamIdempotencyConflict
		}
		return cloneCoordinationMessage(existing), false, nil
	}
	streamKey := teamVersionKey(owner, teamID, version)
	for _, existing := range r.messages[streamKey] {
		if existing.ID == message.ID {
			return agentcoordination.Message{}, false, fmt.Errorf("coordination message %s already exists", message.ID)
		}
	}
	r.messageIdempotency[key] = cloneCoordinationMessage(message)
	r.messages[streamKey] = append(r.messages[streamKey], cloneCoordinationMessage(message))
	return cloneCoordinationMessage(message), true, nil
}

func (r *MemoryAgentTeamRepository) ListCoordinationMessages(owner, teamID, version, correlationID string) ([]agentcoordination.Message, error) {
	owner, err := normalizeOwner(owner)
	if err != nil {
		return nil, err
	}
	r.mu.RLock()
	stored := r.messages[teamVersionKey(owner, strings.TrimSpace(teamID), strings.TrimSpace(version))]
	result := make([]agentcoordination.Message, 0, len(stored))
	for _, message := range stored {
		if correlationID == "" || message.CorrelationID == strings.TrimSpace(correlationID) {
			result = append(result, cloneCoordinationMessage(message))
		}
	}
	r.mu.RUnlock()
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].CreatedAt.Equal(result[j].CreatedAt) {
			return result[i].ID < result[j].ID
		}
		return result[i].CreatedAt.Before(result[j].CreatedAt)
	})
	return result, nil
}

// ListCoordinationMessagesForTeams reads the requested team versions under one
// lock for overview views. The map is keyed by the owner-scoped team version
// key, so messages cannot be associated with another team's attention state.
func (r *MemoryAgentTeamRepository) ListCoordinationMessagesForTeams(owner string, teams []AgentTeamContract) (map[string][]agentcoordination.Message, error) {
	owner, err := normalizeOwner(owner)
	if err != nil {
		return nil, err
	}
	result := make(map[string][]agentcoordination.Message, len(teams))
	r.mu.RLock()
	for _, team := range teams {
		key := teamVersionKey(owner, team.ID, team.Version)
		if _, exists := result[key]; exists {
			continue
		}
		stored := r.messages[key]
		messages := make([]agentcoordination.Message, len(stored))
		for index := range stored {
			messages[index] = cloneCoordinationMessage(stored[index])
		}
		result[key] = messages
	}
	r.mu.RUnlock()
	for key := range result {
		sort.SliceStable(result[key], func(i, j int) bool {
			if result[key][i].CreatedAt.Equal(result[key][j].CreatedAt) {
				return result[key][i].ID < result[key][j].ID
			}
			return result[key][i].CreatedAt.Before(result[key][j].CreatedAt)
		})
	}
	return result, nil
}

func (r *MemoryAgentTeamRepository) GetCoordinationMessage(owner, teamID, version, messageID string) (agentcoordination.Message, error) {
	owner, err := normalizeOwner(owner)
	if err != nil {
		return agentcoordination.Message{}, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, message := range r.messages[teamVersionKey(owner, strings.TrimSpace(teamID), strings.TrimSpace(version))] {
		if message.ID == strings.TrimSpace(messageID) {
			return cloneCoordinationMessage(message), nil
		}
	}
	return agentcoordination.Message{}, ErrAgentTeamMessageNotFound
}

func (r *MemoryAgentTeamRepository) AppendMessageAcknowledgment(owner, teamID, version string, acknowledgment agentcoordination.Acknowledgment) (agentcoordination.Acknowledgment, bool, error) {
	owner, err := normalizeOwner(owner)
	if err != nil {
		return agentcoordination.Acknowledgment{}, false, err
	}
	teamID = strings.TrimSpace(teamID)
	version = strings.TrimSpace(version)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ensureInitialized()
	if _, exists := r.teams[owner][teamID][version]; !exists {
		return agentcoordination.Acknowledgment{}, false, ErrAgentTeamNotFound
	}
	key := teamIdempotencyKey(owner, teamID, version, acknowledgment.IdempotencyKey)
	if existing, exists := r.ackIdempotency[key]; exists {
		existingDigest, existingErr := agentcoordination.ComputeAcknowledgmentDigest(existing)
		requestedDigest, requestedErr := agentcoordination.ComputeAcknowledgmentDigest(acknowledgment)
		if existingErr != nil || requestedErr != nil || existingDigest != requestedDigest {
			return agentcoordination.Acknowledgment{}, false, ErrAgentTeamIdempotencyConflict
		}
		return cloneAcknowledgment(existing), false, nil
	}
	streamKey := teamMessageKey(owner, teamID, version, acknowledgment.MessageID)
	for _, existing := range r.acknowledgments[streamKey] {
		if existing.ID == acknowledgment.ID {
			return agentcoordination.Acknowledgment{}, false, fmt.Errorf("acknowledgment %s already exists", acknowledgment.ID)
		}
		if existing.Status == agentcoordination.AcknowledgmentAccepted || existing.Status == agentcoordination.AcknowledgmentRejected {
			return agentcoordination.Acknowledgment{}, false, ErrAgentTeamAcknowledgmentTerminal
		}
		if !acknowledgment.CreatedAt.After(existing.CreatedAt) {
			return agentcoordination.Acknowledgment{}, false, fmt.Errorf("acknowledgment creation time must advance")
		}
	}
	r.ackIdempotency[key] = cloneAcknowledgment(acknowledgment)
	r.acknowledgments[streamKey] = append(r.acknowledgments[streamKey], cloneAcknowledgment(acknowledgment))
	return cloneAcknowledgment(acknowledgment), true, nil
}

func (r *MemoryAgentTeamRepository) ListMessageAcknowledgments(owner, teamID, version, messageID string) ([]agentcoordination.Acknowledgment, error) {
	owner, err := normalizeOwner(owner)
	if err != nil {
		return nil, err
	}
	r.mu.RLock()
	stored := r.acknowledgments[teamMessageKey(owner, strings.TrimSpace(teamID), strings.TrimSpace(version), strings.TrimSpace(messageID))]
	result := make([]agentcoordination.Acknowledgment, len(stored))
	for index := range stored {
		result[index] = cloneAcknowledgment(stored[index])
	}
	r.mu.RUnlock()
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].CreatedAt.Equal(result[j].CreatedAt) {
			return result[i].ID < result[j].ID
		}
		return result[i].CreatedAt.Before(result[j].CreatedAt)
	})
	return result, nil
}

// ListMessageAcknowledgmentsForMessages reads a team-version's acknowledgments
// in one repository operation for attention views. It keeps the result keyed by
// message ID so callers never infer acknowledgement ownership across messages.
func (r *MemoryAgentTeamRepository) ListMessageAcknowledgmentsForMessages(owner, teamID, version string, messageIDs []string) (map[string][]agentcoordination.Acknowledgment, error) {
	owner, err := normalizeOwner(owner)
	if err != nil {
		return nil, err
	}
	teamID = strings.TrimSpace(teamID)
	version = strings.TrimSpace(version)
	result := make(map[string][]agentcoordination.Acknowledgment, len(messageIDs))
	r.mu.RLock()
	for _, messageID := range messageIDs {
		messageID = strings.TrimSpace(messageID)
		if messageID == "" {
			continue
		}
		stored := r.acknowledgments[teamMessageKey(owner, teamID, version, messageID)]
		acknowledgments := make([]agentcoordination.Acknowledgment, len(stored))
		for index := range stored {
			acknowledgments[index] = cloneAcknowledgment(stored[index])
		}
		result[messageID] = acknowledgments
	}
	r.mu.RUnlock()
	for messageID := range result {
		sort.SliceStable(result[messageID], func(i, j int) bool {
			if result[messageID][i].CreatedAt.Equal(result[messageID][j].CreatedAt) {
				return result[messageID][i].ID < result[messageID][j].ID
			}
			return result[messageID][i].CreatedAt.Before(result[messageID][j].CreatedAt)
		})
	}
	return result, nil
}

func (r *MemoryAgentTeamRepository) RecordConsensusOutcome(owner string, outcome TeamConsensusOutcome, team AgentTeamContract, expectedRevision uint64, event TeamLifecycleEvent) (TeamConsensusOutcome, bool, error) {
	owner, err := normalizeOwner(owner)
	if err != nil {
		return TeamConsensusOutcome{}, false, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ensureInitialized()
	if !outcome.AdvisoryOnly || outcome.GrantsExecutionAuthority || !outcome.ExecutionAuthorizationRequired || outcome.TeamID != team.ID || outcome.TeamVersion != team.Version {
		return TeamConsensusOutcome{}, false, fmt.Errorf("consensus outcome violates advisory team boundary")
	}
	current, exists := r.teams[owner][outcome.TeamID][outcome.TeamVersion]
	if !exists {
		return TeamConsensusOutcome{}, false, ErrAgentTeamNotFound
	}
	key := teamIdempotencyKey(owner, outcome.TeamID, outcome.TeamVersion, outcome.IdempotencyKey)
	if existing, exists := r.outcomeIdempotency[key]; exists {
		if existing.OutcomeDigest != outcome.OutcomeDigest {
			return TeamConsensusOutcome{}, false, ErrAgentTeamIdempotencyConflict
		}
		return cloneTeamOutcome(existing), false, nil
	}
	if current.Revision != expectedRevision {
		return TeamConsensusOutcome{}, false, fmt.Errorf("%w: expected %d, current %d", ErrAgentTeamRevisionConflict, expectedRevision, current.Revision)
	}
	events := r.events[teamVersionKey(owner, team.ID, team.Version)]
	previousDigest := ""
	if len(events) > 0 {
		previousDigest = events[len(events)-1].EventDigest
	}
	if err := validateStoredTeamAndEvent(team, event, expectedRevision, previousDigest); err != nil {
		return TeamConsensusOutcome{}, false, err
	}
	if current.Key != team.Key || current.ID != team.ID || current.Version != team.Version || !current.CreatedAt.Equal(team.CreatedAt) {
		return TeamConsensusOutcome{}, false, fmt.Errorf("team immutable identity fields cannot change")
	}
	expectedOutcomeDigest, err := teamConsensusOutcomeDigest(outcome)
	if err != nil || expectedOutcomeDigest != outcome.OutcomeDigest {
		return TeamConsensusOutcome{}, false, fmt.Errorf("consensus outcome digest is invalid")
	}
	streamKey := teamVersionKey(owner, outcome.TeamID, outcome.TeamVersion)
	r.outcomeIdempotency[key] = cloneTeamOutcome(outcome)
	r.outcomes[streamKey] = append(r.outcomes[streamKey], cloneTeamOutcome(outcome))
	r.teams[owner][team.ID][team.Version] = cloneAgentTeam(team)
	r.events[streamKey] = append(events, cloneTeamEvent(event))
	return cloneTeamOutcome(outcome), true, nil
}

func (r *MemoryAgentTeamRepository) ListConsensusOutcomes(owner, teamID, version string) ([]TeamConsensusOutcome, error) {
	owner, err := normalizeOwner(owner)
	if err != nil {
		return nil, err
	}
	r.mu.RLock()
	stored := r.outcomes[teamVersionKey(owner, strings.TrimSpace(teamID), strings.TrimSpace(version))]
	result := make([]TeamConsensusOutcome, len(stored))
	for index := range stored {
		result[index] = cloneTeamOutcome(stored[index])
	}
	r.mu.RUnlock()
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].RecordedAt.Equal(result[j].RecordedAt) {
			return result[i].ID < result[j].ID
		}
		return result[i].RecordedAt.Before(result[j].RecordedAt)
	})
	return result, nil
}

func (r *MemoryAgentTeamRepository) ensureInitialized() {
	if r.teams == nil {
		r.teams = map[string]map[string]map[string]AgentTeamContract{}
	}
	if r.teamKeys == nil {
		r.teamKeys = map[string]map[string]string{}
	}
	if r.events == nil {
		r.events = map[string][]TeamLifecycleEvent{}
	}
	if r.messages == nil {
		r.messages = map[string][]agentcoordination.Message{}
	}
	if r.messageIdempotency == nil {
		r.messageIdempotency = map[string]agentcoordination.Message{}
	}
	if r.acknowledgments == nil {
		r.acknowledgments = map[string][]agentcoordination.Acknowledgment{}
	}
	if r.ackIdempotency == nil {
		r.ackIdempotency = map[string]agentcoordination.Acknowledgment{}
	}
	if r.outcomes == nil {
		r.outcomes = map[string][]TeamConsensusOutcome{}
	}
	if r.outcomeIdempotency == nil {
		r.outcomeIdempotency = map[string]TeamConsensusOutcome{}
	}
}

func validateStoredTeamAndEvent(team AgentTeamContract, event TeamLifecycleEvent, expectedRevision uint64, previousDigest string) error {
	expectedContractDigest, err := agentTeamContractDigest(team)
	if err != nil || expectedContractDigest != team.ContractDigest {
		return fmt.Errorf("team contract digest is invalid")
	}
	if team.Revision != expectedRevision+1 || event.Revision != team.Revision || event.Sequence != expectedRevision+1 {
		return fmt.Errorf("team and event revision must advance exactly once")
	}
	if event.TeamID != team.ID || event.TeamVersion != team.Version || event.PreviousEventDigest != previousDigest {
		return fmt.Errorf("team lifecycle event identity or chain is invalid")
	}
	expectedEventDigest, err := teamLifecycleEventDigest(event)
	if err != nil || expectedEventDigest != event.EventDigest {
		return fmt.Errorf("team lifecycle event digest is invalid")
	}
	return nil
}

func teamVersionKey(owner, teamID, version string) string {
	return owner + "\x00" + teamID + "\x00" + version
}

func teamIdempotencyKey(owner, teamID, version, key string) string {
	return teamVersionKey(owner, teamID, version) + "\x00" + key
}

func teamMessageKey(owner, teamID, version, messageID string) string {
	return teamVersionKey(owner, teamID, version) + "\x00" + messageID
}

func cloneAgentTeam(value AgentTeamContract) AgentTeamContract   { return agentTeamJSONClone(value) }
func cloneTeamEvent(value TeamLifecycleEvent) TeamLifecycleEvent { return agentTeamJSONClone(value) }
func cloneCoordinationMessage(value agentcoordination.Message) agentcoordination.Message {
	return agentTeamJSONClone(value)
}
func cloneAcknowledgment(value agentcoordination.Acknowledgment) agentcoordination.Acknowledgment {
	return agentTeamJSONClone(value)
}
func cloneTeamOutcome(value TeamConsensusOutcome) TeamConsensusOutcome {
	return agentTeamJSONClone(value)
}

func agentTeamJSONClone[T any](value T) T {
	payload, err := json.Marshal(value)
	if err != nil {
		return value
	}
	var cloned T
	if err := json.Unmarshal(payload, &cloned); err != nil {
		return value
	}
	return cloned
}

var _ AgentTeamRepository = (*MemoryAgentTeamRepository)(nil)
