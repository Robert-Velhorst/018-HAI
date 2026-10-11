package frameworkregistry

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"automation-hub-backend/internal/agentcoordination"
	"automation-hub-backend/internal/infra"

	"gorm.io/gorm"
)

// PostgresAgentTeamRepository persists owner-scoped team lifecycle state.
// Schema creation is intentionally left to the versioned migrations.
type PostgresAgentTeamRepository struct {
	DB *gorm.DB
}

func NewPostgresAgentTeamRepository(db *gorm.DB) *PostgresAgentTeamRepository {
	return &PostgresAgentTeamRepository{DB: db}
}

// DefaultAgentTeamRepository opens the configured database and applies the
// project migration chain. It never falls back to process memory.
func DefaultAgentTeamRepository() (AgentTeamRepository, error) {
	db, err := infra.GetDefaultDB()
	if err != nil {
		return nil, err
	}
	return NewPostgresAgentTeamRepository(db), nil
}

func (r *PostgresAgentTeamRepository) CreateTeam(owner string, team AgentTeamContract, event TeamLifecycleEvent) error {
	owner, err := normalizeOwner(owner)
	if err != nil {
		return err
	}
	if err := r.ready(); err != nil {
		return err
	}
	if err := validatePostgresAgentTeam(team); err != nil {
		return err
	}
	if err := validateStoredTeamAndEvent(team, event, 0, ""); err != nil {
		return err
	}
	teamPayload, err := marshalAgentTeamRecord("team contract", team)
	if err != nil {
		return err
	}
	eventPayload, err := marshalAgentTeamRecord("team lifecycle event", event)
	if err != nil {
		return err
	}

	return r.DB.Transaction(func(tx *gorm.DB) error {
		identityInsert := tx.Exec(`
			INSERT INTO public.agent_teams (
				owner_identity, team_id, team_key, created_at
			) VALUES (?, ?, ?, ?)
			ON CONFLICT DO NOTHING`,
			owner, team.ID, team.Key, team.CreatedAt.UTC(),
		)
		if identityInsert.Error != nil {
			return fmt.Errorf("create agent team identity: %w", identityInsert.Error)
		}
		var identities []postgresAgentTeamIdentity
		if err := tx.Raw(`
			SELECT owner_identity, team_id::text AS team_id, team_key
			FROM public.agent_teams
			WHERE owner_identity = ? AND (team_id = ? OR team_key = ?)`,
			owner, team.ID, team.Key,
		).Scan(&identities).Error; err != nil {
			return fmt.Errorf("verify agent team identity: %w", err)
		}
		if len(identities) != 1 || identities[0].OwnerIdentity != owner ||
			identities[0].TeamID != team.ID || identities[0].TeamKey != team.Key {
			return fmt.Errorf("agent team identity conflicts with an existing owner-scoped team")
		}

		result := tx.Exec(`
			INSERT INTO public.agent_team_contracts (
				owner_identity, team_id, team_key, team_version, revision,
				team_status, contract_digest, previous_version_digest,
				created_at, updated_at, payload
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CAST(? AS jsonb))
			ON CONFLICT (owner_identity, team_id, team_version) DO NOTHING`,
			owner, team.ID, team.Key, team.Version, team.Revision,
			team.Status, team.ContractDigest, team.PreviousVersionDigest,
			team.CreatedAt.UTC(), team.UpdatedAt.UTC(), string(teamPayload),
		)
		if result.Error != nil {
			return fmt.Errorf("create agent team contract: %w", result.Error)
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("agent team %s version %s already exists", team.ID, team.Version)
		}
		return insertPostgresTeamEvent(tx, owner, event, eventPayload)
	})
}

func (r *PostgresAgentTeamRepository) UpdateTeam(owner string, team AgentTeamContract, expectedRevision uint64, event TeamLifecycleEvent) error {
	owner, err := normalizeOwner(owner)
	if err != nil {
		return err
	}
	if err := r.ready(); err != nil {
		return err
	}
	if err := validatePostgresAgentTeam(team); err != nil {
		return err
	}
	teamPayload, err := marshalAgentTeamRecord("team contract", team)
	if err != nil {
		return err
	}
	eventPayload, err := marshalAgentTeamRecord("team lifecycle event", event)
	if err != nil {
		return err
	}

	return r.DB.Transaction(func(tx *gorm.DB) error {
		current, err := loadPostgresTeam(tx, owner, team.ID, team.Version, true)
		if err != nil {
			return err
		}
		if current.Revision != expectedRevision {
			return fmt.Errorf("%w: expected %d, current %d", ErrAgentTeamRevisionConflict, expectedRevision, current.Revision)
		}
		if current.ID != team.ID || current.Key != team.Key || current.Version != team.Version ||
			!current.CreatedAt.Equal(team.CreatedAt) {
			return fmt.Errorf("team immutable identity fields cannot change")
		}
		events, err := loadPostgresTeamEvents(tx, owner, team.ID, team.Version)
		if err != nil {
			return err
		}
		previousDigest := ""
		if len(events) > 0 {
			previousDigest = events[len(events)-1].EventDigest
		}
		if err := validateStoredTeamAndEvent(team, event, expectedRevision, previousDigest); err != nil {
			return err
		}

		result := tx.Exec(`
			UPDATE public.agent_team_contracts
			SET revision = ?, team_status = ?, contract_digest = ?,
				previous_version_digest = ?, updated_at = ?, payload = CAST(? AS jsonb)
			WHERE owner_identity = ? AND team_id = ? AND team_version = ?
				AND revision = ?`,
			team.Revision, team.Status, team.ContractDigest,
			team.PreviousVersionDigest, team.UpdatedAt.UTC(), string(teamPayload),
			owner, team.ID, team.Version, expectedRevision,
		)
		if result.Error != nil {
			return fmt.Errorf("advance agent team contract: %w", result.Error)
		}
		if result.RowsAffected != 1 {
			return ErrAgentTeamRevisionConflict
		}
		return insertPostgresTeamEvent(tx, owner, event, eventPayload)
	})
}

func (r *PostgresAgentTeamRepository) GetTeam(owner, teamID, version string) (AgentTeamContract, error) {
	owner, err := normalizeOwner(owner)
	if err != nil {
		return AgentTeamContract{}, err
	}
	if err := r.ready(); err != nil {
		return AgentTeamContract{}, err
	}
	return loadPostgresTeam(r.DB, owner, strings.TrimSpace(teamID), strings.TrimSpace(version), false)
}

func (r *PostgresAgentTeamRepository) ListTeams(owner string) ([]AgentTeamContract, error) {
	owner, err := normalizeOwner(owner)
	if err != nil {
		return nil, err
	}
	if err := r.ready(); err != nil {
		return nil, err
	}
	rows, err := queryPostgresTeamRows(r.DB, `
		SELECT owner_identity, team_id::text AS team_id, team_key, team_version,
			revision, team_status, contract_digest, previous_version_digest,
			created_at, updated_at, payload::text AS payload
		FROM public.agent_team_contracts
		WHERE owner_identity = ?`, owner)
	if err != nil {
		return nil, err
	}
	latest := make(map[string]AgentTeamContract)
	for _, row := range rows {
		team, err := decodePostgresAgentTeamRow(row, owner)
		if err != nil {
			return nil, err
		}
		current, exists := latest[team.ID]
		if !exists || compareSemanticVersions(team.Version, current.Version) > 0 {
			latest[team.ID] = team
		}
	}
	result := make([]AgentTeamContract, 0, len(latest))
	for _, team := range latest {
		result = append(result, team)
	}
	sortAgentTeams(result, false)
	return result, nil
}

func (r *PostgresAgentTeamRepository) ListTeamVersions(owner, teamID string) ([]AgentTeamContract, error) {
	owner, err := normalizeOwner(owner)
	if err != nil {
		return nil, err
	}
	if err := r.ready(); err != nil {
		return nil, err
	}
	rows, err := queryPostgresTeamRows(r.DB, `
		SELECT owner_identity, team_id::text AS team_id, team_key, team_version,
			revision, team_status, contract_digest, previous_version_digest,
			created_at, updated_at, payload::text AS payload
		FROM public.agent_team_contracts
		WHERE owner_identity = ? AND team_id = ?`, owner, strings.TrimSpace(teamID))
	if err != nil {
		return nil, err
	}
	result := make([]AgentTeamContract, 0, len(rows))
	for _, row := range rows {
		team, err := decodePostgresAgentTeamRow(row, owner)
		if err != nil {
			return nil, err
		}
		result = append(result, team)
	}
	sortAgentTeams(result, true)
	return result, nil
}

func (r *PostgresAgentTeamRepository) ListTeamEvents(owner, teamID, version string) ([]TeamLifecycleEvent, error) {
	owner, err := normalizeOwner(owner)
	if err != nil {
		return nil, err
	}
	if err := r.ready(); err != nil {
		return nil, err
	}
	return loadPostgresTeamEvents(r.DB, owner, strings.TrimSpace(teamID), strings.TrimSpace(version))
}

func (r *PostgresAgentTeamRepository) AppendCoordinationMessage(owner, teamID, version string, message agentcoordination.Message) (agentcoordination.Message, bool, error) {
	owner, err := normalizeOwner(owner)
	if err != nil {
		return agentcoordination.Message{}, false, err
	}
	if err := r.ready(); err != nil {
		return agentcoordination.Message{}, false, err
	}
	teamID = strings.TrimSpace(teamID)
	version = strings.TrimSpace(version)
	payload, err := marshalAgentTeamRecord("coordination message", message)
	if err != nil {
		return agentcoordination.Message{}, false, err
	}
	expectedDigest, err := agentcoordination.ComputeMessageDigest(message)
	if err != nil || expectedDigest != strings.ToLower(strings.TrimSpace(message.PayloadDigest)) {
		return agentcoordination.Message{}, false, fmt.Errorf("coordination message digest is invalid")
	}

	var stored agentcoordination.Message
	created := false
	err = r.DB.Transaction(func(tx *gorm.DB) error {
		if _, err := loadPostgresTeam(tx, owner, teamID, version, false); err != nil {
			return err
		}
		result := tx.Exec(`
			INSERT INTO public.agent_team_coordination_messages (
				owner_identity, team_id, team_version, message_id,
				idempotency_key, correlation_id, payload_digest, created_at, payload
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, CAST(? AS jsonb))
			ON CONFLICT DO NOTHING`,
			owner, teamID, version, message.ID, message.IdempotencyKey,
			message.CorrelationID, message.PayloadDigest, message.CreatedAt.UTC(), string(payload),
		)
		if result.Error != nil {
			return fmt.Errorf("append agent team coordination message: %w", result.Error)
		}
		if result.RowsAffected == 1 {
			stored = cloneCoordinationMessage(message)
			created = true
			return nil
		}
		existing, found, err := loadPostgresCoordinationMessageByIdempotency(tx, owner, teamID, version, message.IdempotencyKey)
		if err != nil {
			return err
		}
		if !found || existing.PayloadDigest != message.PayloadDigest {
			return ErrAgentTeamIdempotencyConflict
		}
		stored = existing
		return nil
	})
	if err != nil {
		return agentcoordination.Message{}, false, err
	}
	return stored, created, nil
}

func (r *PostgresAgentTeamRepository) ListCoordinationMessages(owner, teamID, version, correlationID string) ([]agentcoordination.Message, error) {
	owner, err := normalizeOwner(owner)
	if err != nil {
		return nil, err
	}
	if err := r.ready(); err != nil {
		return nil, err
	}
	query := `
		SELECT owner_identity, team_id::text AS team_id, team_version,
			message_id::text AS message_id, idempotency_key::text AS idempotency_key,
			correlation_id::text AS correlation_id, payload_digest, created_at,
			payload::text AS payload
		FROM public.agent_team_coordination_messages
		WHERE owner_identity = ? AND team_id = ? AND team_version = ?`
	args := []any{owner, strings.TrimSpace(teamID), strings.TrimSpace(version)}
	if correlationID != "" {
		query += " AND correlation_id = ?"
		args = append(args, strings.TrimSpace(correlationID))
	}
	query += " ORDER BY created_at ASC, message_id ASC"
	var rows []postgresCoordinationMessageRow
	if err := r.DB.Raw(query, args...).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("list agent team coordination messages: %w", err)
	}
	result := make([]agentcoordination.Message, 0, len(rows))
	for _, row := range rows {
		message, err := decodePostgresCoordinationMessage(row, owner, strings.TrimSpace(teamID), strings.TrimSpace(version))
		if err != nil {
			return nil, err
		}
		result = append(result, message)
	}
	return result, nil
}

// ListCoordinationMessagesForTeams keeps overview reads bounded to one query.
// It restricts the query to the requested team IDs and then verifies the exact
// version in Go before decoding the signed record.
func (r *PostgresAgentTeamRepository) ListCoordinationMessagesForTeams(owner string, teams []AgentTeamContract) (map[string][]agentcoordination.Message, error) {
	owner, err := normalizeOwner(owner)
	if err != nil {
		return nil, err
	}
	if err := r.ready(); err != nil {
		return nil, err
	}
	result := make(map[string][]agentcoordination.Message, len(teams))
	requested := make(map[string]struct{}, len(teams))
	teamIDs := make([]string, 0, len(teams))
	seenTeamIDs := map[string]struct{}{}
	for _, team := range teams {
		teamID := strings.TrimSpace(team.ID)
		version := strings.TrimSpace(team.Version)
		if teamID == "" || version == "" {
			continue
		}
		key := teamVersionKey(owner, teamID, version)
		if _, exists := requested[key]; exists {
			continue
		}
		requested[key] = struct{}{}
		result[key] = []agentcoordination.Message{}
		if _, exists := seenTeamIDs[teamID]; !exists {
			seenTeamIDs[teamID] = struct{}{}
			teamIDs = append(teamIDs, teamID)
		}
	}
	if len(teamIDs) == 0 {
		return result, nil
	}
	var rows []postgresCoordinationMessageRow
	if err := r.DB.Raw(`
		SELECT owner_identity, team_id::text AS team_id, team_version,
			message_id::text AS message_id, idempotency_key::text AS idempotency_key,
			correlation_id::text AS correlation_id, payload_digest, created_at,
			payload::text AS payload
		FROM public.agent_team_coordination_messages
		WHERE owner_identity = ? AND team_id::text IN ?
		ORDER BY team_id ASC, team_version ASC, created_at ASC, message_id ASC`, owner, teamIDs).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("list agent team coordination messages for teams: %w", err)
	}
	for _, row := range rows {
		key := teamVersionKey(owner, row.TeamID, row.TeamVersion)
		if _, requested := requested[key]; !requested {
			continue
		}
		message, err := decodePostgresCoordinationMessage(row, owner, row.TeamID, row.TeamVersion)
		if err != nil {
			return nil, err
		}
		result[key] = append(result[key], message)
	}
	return result, nil
}

func (r *PostgresAgentTeamRepository) GetCoordinationMessage(owner, teamID, version, messageID string) (agentcoordination.Message, error) {
	owner, err := normalizeOwner(owner)
	if err != nil {
		return agentcoordination.Message{}, err
	}
	if err := r.ready(); err != nil {
		return agentcoordination.Message{}, err
	}
	message, found, err := loadPostgresCoordinationMessage(r.DB, owner, strings.TrimSpace(teamID), strings.TrimSpace(version), strings.TrimSpace(messageID))
	if err != nil {
		return agentcoordination.Message{}, err
	}
	if !found {
		return agentcoordination.Message{}, ErrAgentTeamMessageNotFound
	}
	return message, nil
}

func (r *PostgresAgentTeamRepository) AppendMessageAcknowledgment(owner, teamID, version string, acknowledgment agentcoordination.Acknowledgment) (agentcoordination.Acknowledgment, bool, error) {
	owner, err := normalizeOwner(owner)
	if err != nil {
		return agentcoordination.Acknowledgment{}, false, err
	}
	if err := r.ready(); err != nil {
		return agentcoordination.Acknowledgment{}, false, err
	}
	teamID = strings.TrimSpace(teamID)
	version = strings.TrimSpace(version)
	payload, err := marshalAgentTeamRecord("message acknowledgment", acknowledgment)
	if err != nil {
		return agentcoordination.Acknowledgment{}, false, err
	}
	digest, err := agentcoordination.ComputeAcknowledgmentDigest(acknowledgment)
	if err != nil {
		return agentcoordination.Acknowledgment{}, false, fmt.Errorf("compute acknowledgment digest: %w", err)
	}

	var stored agentcoordination.Acknowledgment
	created := false
	err = r.DB.Transaction(func(tx *gorm.DB) error {
		if _, err := loadPostgresTeam(tx, owner, teamID, version, false); err != nil {
			return err
		}
		message, found, err := loadPostgresCoordinationMessage(tx, owner, teamID, version, acknowledgment.MessageID)
		if err != nil {
			return err
		}
		if !found {
			return ErrAgentTeamMessageNotFound
		}
		validationTime := time.Now().UTC()
		if acknowledgment.CreatedAt.After(validationTime) {
			validationTime = acknowledgment.CreatedAt.UTC()
		}
		if err := agentcoordination.ValidateAcknowledgment(message, acknowledgment, validationTime); err != nil {
			return err
		}
		existing, found, err := loadPostgresAcknowledgmentByIdempotency(tx, owner, teamID, version, acknowledgment.IdempotencyKey)
		if err != nil {
			return err
		}
		if found {
			existingDigest, digestErr := agentcoordination.ComputeAcknowledgmentDigest(existing)
			if digestErr != nil || existingDigest != digest {
				return ErrAgentTeamIdempotencyConflict
			}
			stored = existing
			return nil
		}
		previous, err := listPostgresMessageAcknowledgments(tx, owner, teamID, version, acknowledgment.MessageID)
		if err != nil {
			return err
		}
		for _, item := range previous {
			if item.Status == agentcoordination.AcknowledgmentAccepted || item.Status == agentcoordination.AcknowledgmentRejected {
				return ErrAgentTeamAcknowledgmentTerminal
			}
			if !acknowledgment.CreatedAt.After(item.CreatedAt) {
				return fmt.Errorf("acknowledgment creation time must advance")
			}
		}
		result := tx.Exec(`
			INSERT INTO public.agent_team_message_acknowledgments (
				owner_identity, team_id, team_version, acknowledgment_id,
				message_id, correlation_id, recipient_id, status,
				idempotency_key, acknowledgment_digest, created_at, retry_after, payload
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CAST(? AS jsonb))`,
			owner, teamID, version, acknowledgment.ID, acknowledgment.MessageID,
			acknowledgment.CorrelationID, acknowledgment.RecipientID, acknowledgment.Status,
			acknowledgment.IdempotencyKey, digest, acknowledgment.CreatedAt.UTC(),
			acknowledgment.RetryAfter, string(payload),
		)
		if result.Error != nil {
			return fmt.Errorf("append agent team message acknowledgment: %w", result.Error)
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("append agent team message acknowledgment affected no rows")
		}
		stored = cloneAcknowledgment(acknowledgment)
		created = true
		return nil
	})
	if err != nil {
		return agentcoordination.Acknowledgment{}, false, err
	}
	return stored, created, nil
}

func (r *PostgresAgentTeamRepository) ListMessageAcknowledgments(owner, teamID, version, messageID string) ([]agentcoordination.Acknowledgment, error) {
	owner, err := normalizeOwner(owner)
	if err != nil {
		return nil, err
	}
	if err := r.ready(); err != nil {
		return nil, err
	}
	return listPostgresMessageAcknowledgments(r.DB, owner, strings.TrimSpace(teamID), strings.TrimSpace(version), strings.TrimSpace(messageID))
}

// ListMessageAcknowledgmentsForMessages avoids a query per message when HAI
// derives attention for one team/version.
func (r *PostgresAgentTeamRepository) ListMessageAcknowledgmentsForMessages(owner, teamID, version string, messageIDs []string) (map[string][]agentcoordination.Acknowledgment, error) {
	owner, err := normalizeOwner(owner)
	if err != nil {
		return nil, err
	}
	if err := r.ready(); err != nil {
		return nil, err
	}
	teamID = strings.TrimSpace(teamID)
	version = strings.TrimSpace(version)
	ids := make([]string, 0, len(messageIDs))
	result := make(map[string][]agentcoordination.Acknowledgment, len(messageIDs))
	seen := map[string]struct{}{}
	for _, messageID := range messageIDs {
		messageID = strings.TrimSpace(messageID)
		if messageID == "" {
			continue
		}
		if _, exists := seen[messageID]; exists {
			continue
		}
		seen[messageID] = struct{}{}
		ids = append(ids, messageID)
		result[messageID] = []agentcoordination.Acknowledgment{}
	}
	if len(ids) == 0 {
		return result, nil
	}
	rows, err := queryPostgresMessageAcknowledgments(r.DB, `
		SELECT owner_identity, team_id::text AS team_id, team_version,
			acknowledgment_id::text AS acknowledgment_id, message_id::text AS message_id,
			correlation_id::text AS correlation_id, recipient_id, status,
			idempotency_key::text AS idempotency_key, acknowledgment_digest,
			created_at, retry_after, payload::text AS payload
		FROM public.agent_team_message_acknowledgments
		WHERE owner_identity = ? AND team_id = ? AND team_version = ?
			AND message_id::text IN ?
		ORDER BY message_id ASC, created_at ASC, acknowledgment_id ASC`, owner, teamID, version, ids)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		acknowledgment, err := decodePostgresMessageAcknowledgment(row, owner, teamID, version)
		if err != nil {
			return nil, err
		}
		result[acknowledgment.MessageID] = append(result[acknowledgment.MessageID], acknowledgment)
	}
	return result, nil
}

func (r *PostgresAgentTeamRepository) RecordConsensusOutcome(owner string, outcome TeamConsensusOutcome, team AgentTeamContract, expectedRevision uint64, event TeamLifecycleEvent) (TeamConsensusOutcome, bool, error) {
	owner, err := normalizeOwner(owner)
	if err != nil {
		return TeamConsensusOutcome{}, false, err
	}
	if err := r.ready(); err != nil {
		return TeamConsensusOutcome{}, false, err
	}
	if err := validatePostgresAgentTeam(team); err != nil {
		return TeamConsensusOutcome{}, false, err
	}
	if err := validatePostgresConsensusOutcome(outcome, team.ID, team.Version); err != nil {
		return TeamConsensusOutcome{}, false, err
	}
	teamPayload, err := marshalAgentTeamRecord("team contract", team)
	if err != nil {
		return TeamConsensusOutcome{}, false, err
	}
	outcomePayload, err := marshalAgentTeamRecord("team consensus outcome", outcome)
	if err != nil {
		return TeamConsensusOutcome{}, false, err
	}
	eventPayload, err := marshalAgentTeamRecord("team lifecycle event", event)
	if err != nil {
		return TeamConsensusOutcome{}, false, err
	}

	var stored TeamConsensusOutcome
	created := false
	err = r.DB.Transaction(func(tx *gorm.DB) error {
		current, err := loadPostgresTeam(tx, owner, team.ID, team.Version, true)
		if err != nil {
			return err
		}
		existing, found, err := loadPostgresConsensusByIdempotency(tx, owner, team.ID, team.Version, outcome.IdempotencyKey)
		if err != nil {
			return err
		}
		if found {
			if existing.OutcomeDigest != outcome.OutcomeDigest {
				return ErrAgentTeamIdempotencyConflict
			}
			stored = existing
			return nil
		}
		if current.Revision != expectedRevision {
			return fmt.Errorf("%w: expected %d, current %d", ErrAgentTeamRevisionConflict, expectedRevision, current.Revision)
		}
		if current.ID != team.ID || current.Key != team.Key || current.Version != team.Version ||
			!current.CreatedAt.Equal(team.CreatedAt) {
			return fmt.Errorf("team immutable identity fields cannot change")
		}
		events, err := loadPostgresTeamEvents(tx, owner, team.ID, team.Version)
		if err != nil {
			return err
		}
		previousDigest := ""
		if len(events) > 0 {
			previousDigest = events[len(events)-1].EventDigest
		}
		if err := validateStoredTeamAndEvent(team, event, expectedRevision, previousDigest); err != nil {
			return err
		}

		update := tx.Exec(`
			UPDATE public.agent_team_contracts
			SET revision = ?, team_status = ?, contract_digest = ?,
				previous_version_digest = ?, updated_at = ?, payload = CAST(? AS jsonb)
			WHERE owner_identity = ? AND team_id = ? AND team_version = ?
				AND revision = ?`,
			team.Revision, team.Status, team.ContractDigest,
			team.PreviousVersionDigest, team.UpdatedAt.UTC(), string(teamPayload),
			owner, team.ID, team.Version, expectedRevision,
		)
		if update.Error != nil {
			return fmt.Errorf("advance agent team for consensus: %w", update.Error)
		}
		if update.RowsAffected != 1 {
			return ErrAgentTeamRevisionConflict
		}
		insert := tx.Exec(`
			INSERT INTO public.agent_team_consensus_outcomes (
				owner_identity, team_id, team_version, outcome_id,
				idempotency_key, correlation_id, team_revision,
				outcome_status, outcome_digest, recorded_at, payload
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CAST(? AS jsonb))`,
			owner, team.ID, team.Version, outcome.ID, outcome.IdempotencyKey,
			outcome.CorrelationID, team.Revision, outcome.Status,
			outcome.OutcomeDigest, outcome.RecordedAt.UTC(), string(outcomePayload),
		)
		if insert.Error != nil {
			return fmt.Errorf("record agent team consensus outcome: %w", insert.Error)
		}
		if err := insertPostgresTeamEvent(tx, owner, event, eventPayload); err != nil {
			return err
		}
		stored = cloneTeamOutcome(outcome)
		created = true
		return nil
	})
	if err != nil {
		return TeamConsensusOutcome{}, false, err
	}
	return stored, created, nil
}

func (r *PostgresAgentTeamRepository) ListConsensusOutcomes(owner, teamID, version string) ([]TeamConsensusOutcome, error) {
	owner, err := normalizeOwner(owner)
	if err != nil {
		return nil, err
	}
	if err := r.ready(); err != nil {
		return nil, err
	}
	teamID = strings.TrimSpace(teamID)
	version = strings.TrimSpace(version)
	var rows []postgresConsensusOutcomeRow
	if err := r.DB.Raw(`
		SELECT owner_identity, team_id::text AS team_id, team_version,
			outcome_id::text AS outcome_id, idempotency_key::text AS idempotency_key,
			correlation_id::text AS correlation_id, team_revision,
			outcome_status, outcome_digest, recorded_at, payload::text AS payload
		FROM public.agent_team_consensus_outcomes
		WHERE owner_identity = ? AND team_id = ? AND team_version = ?
		ORDER BY recorded_at ASC, outcome_id ASC`, owner, teamID, version,
	).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("list agent team consensus outcomes: %w", err)
	}
	result := make([]TeamConsensusOutcome, 0, len(rows))
	for _, row := range rows {
		outcome, err := decodePostgresConsensusOutcome(row, owner, teamID, version)
		if err != nil {
			return nil, err
		}
		result = append(result, outcome)
	}
	return result, nil
}

func (r *PostgresAgentTeamRepository) ready() error {
	if r == nil || r.DB == nil {
		return fmt.Errorf("agent team Postgres database is required")
	}
	return nil
}

type postgresAgentTeamIdentity struct {
	OwnerIdentity string
	TeamID        string
	TeamKey       string
}

type postgresAgentTeamRow struct {
	OwnerIdentity         string
	TeamID                string
	TeamKey               string
	TeamVersion           string
	Revision              int64
	TeamStatus            string
	ContractDigest        string
	PreviousVersionDigest string
	CreatedAt             time.Time
	UpdatedAt             time.Time
	Payload               string
}

type postgresTeamEventRow struct {
	OwnerIdentity       string
	TeamID              string
	TeamVersion         string
	Sequence            int64
	EventID             string
	Revision            int64
	EventType           string
	EventDigest         string
	PreviousEventDigest string
	OccurredAt          time.Time
	Payload             string
}

type postgresCoordinationMessageRow struct {
	OwnerIdentity  string
	TeamID         string
	TeamVersion    string
	MessageID      string
	IdempotencyKey string
	CorrelationID  string
	PayloadDigest  string
	CreatedAt      time.Time
	Payload        string
}

type postgresMessageAcknowledgmentRow struct {
	OwnerIdentity        string
	TeamID               string
	TeamVersion          string
	AcknowledgmentID     string
	MessageID            string
	CorrelationID        string
	RecipientID          string
	Status               string
	IdempotencyKey       string
	AcknowledgmentDigest string
	CreatedAt            time.Time
	RetryAfter           *time.Time
	Payload              string
}

type postgresConsensusOutcomeRow struct {
	OwnerIdentity  string
	TeamID         string
	TeamVersion    string
	OutcomeID      string
	IdempotencyKey string
	CorrelationID  string
	TeamRevision   int64
	OutcomeStatus  string
	OutcomeDigest  string
	RecordedAt     time.Time
	Payload        string
}

func loadPostgresTeam(db *gorm.DB, owner, teamID, version string, lock bool) (AgentTeamContract, error) {
	query := `
		SELECT owner_identity, team_id::text AS team_id, team_key, team_version,
			revision, team_status, contract_digest, previous_version_digest,
			created_at, updated_at, payload::text AS payload
		FROM public.agent_team_contracts
		WHERE owner_identity = ? AND team_id = ? AND team_version = ?`
	if lock {
		query += " FOR UPDATE"
	}
	rows, err := queryPostgresTeamRows(db, query, owner, teamID, version)
	if err != nil {
		return AgentTeamContract{}, err
	}
	if len(rows) == 0 {
		return AgentTeamContract{}, ErrAgentTeamNotFound
	}
	if len(rows) != 1 {
		return AgentTeamContract{}, fmt.Errorf("agent team lookup returned duplicate rows")
	}
	return decodePostgresAgentTeamRow(rows[0], owner)
}

func queryPostgresTeamRows(db *gorm.DB, query string, args ...any) ([]postgresAgentTeamRow, error) {
	var rows []postgresAgentTeamRow
	if err := db.Raw(query, args...).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("query agent team contracts: %w", err)
	}
	return rows, nil
}

func decodePostgresAgentTeamRow(row postgresAgentTeamRow, owner string) (AgentTeamContract, error) {
	var team AgentTeamContract
	if err := decodeStrictAgentTeamJSON("team contract", row.Payload, &team); err != nil {
		return AgentTeamContract{}, err
	}
	if row.Revision <= 0 || row.OwnerIdentity != owner || team.ID != row.TeamID ||
		team.Key != row.TeamKey || team.Version != row.TeamVersion ||
		team.Revision != uint64(row.Revision) || team.Status != row.TeamStatus ||
		team.ContractDigest != row.ContractDigest ||
		team.PreviousVersionDigest != row.PreviousVersionDigest ||
		!postgresTimesEqual(team.CreatedAt, row.CreatedAt) ||
		!postgresTimesEqual(team.UpdatedAt, row.UpdatedAt) {
		return AgentTeamContract{}, fmt.Errorf("stored team contract metadata is inconsistent")
	}
	if err := validatePostgresAgentTeam(team); err != nil {
		return AgentTeamContract{}, fmt.Errorf("stored team contract is invalid: %w", err)
	}
	return team, nil
}

func validatePostgresAgentTeam(team AgentTeamContract) error {
	normalized, err := normalizeAgentTeamContract(team, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("validate team contract: %w", err)
	}
	if normalized.ContractDigest != team.ContractDigest {
		return fmt.Errorf("team contract digest is invalid")
	}
	return nil
}

func insertPostgresTeamEvent(tx *gorm.DB, owner string, event TeamLifecycleEvent, payload []byte) error {
	result := tx.Exec(`
		INSERT INTO public.agent_team_lifecycle_events (
			owner_identity, team_id, team_version, sequence, event_id,
			revision, event_type, event_digest, previous_event_digest,
			occurred_at, payload
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CAST(? AS jsonb))`,
		owner, event.TeamID, event.TeamVersion, event.Sequence, event.ID,
		event.Revision, event.Type, event.EventDigest, event.PreviousEventDigest,
		event.OccurredAt.UTC(), string(payload),
	)
	if result.Error != nil {
		return fmt.Errorf("append agent team lifecycle event: %w", result.Error)
	}
	return nil
}

func loadPostgresTeamEvents(db *gorm.DB, owner, teamID, version string) ([]TeamLifecycleEvent, error) {
	var rows []postgresTeamEventRow
	if err := db.Raw(`
		SELECT owner_identity, team_id::text AS team_id, team_version, sequence,
			event_id::text AS event_id, revision, event_type, event_digest,
			previous_event_digest, occurred_at, payload::text AS payload
		FROM public.agent_team_lifecycle_events
		WHERE owner_identity = ? AND team_id = ? AND team_version = ?
		ORDER BY sequence ASC`, owner, teamID, version,
	).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("list agent team lifecycle events: %w", err)
	}
	result := make([]TeamLifecycleEvent, 0, len(rows))
	previousDigest := ""
	for index, row := range rows {
		var event TeamLifecycleEvent
		if err := decodeStrictAgentTeamJSON("team lifecycle event", row.Payload, &event); err != nil {
			return nil, err
		}
		expectedSequence := uint64(index + 1)
		expectedDigest, digestErr := teamLifecycleEventDigest(event)
		if row.OwnerIdentity != owner || row.TeamID != teamID || row.TeamVersion != version ||
			row.Sequence <= 0 || row.Revision <= 0 || event.Sequence != uint64(row.Sequence) ||
			event.Sequence != expectedSequence || event.Revision != uint64(row.Revision) ||
			event.Revision != event.Sequence || event.ID != row.EventID ||
			event.TeamID != row.TeamID || event.TeamVersion != row.TeamVersion ||
			event.Type != row.EventType || event.EventDigest != row.EventDigest ||
			event.PreviousEventDigest != row.PreviousEventDigest ||
			event.PreviousEventDigest != previousDigest ||
			!postgresTimesEqual(event.OccurredAt, row.OccurredAt) ||
			digestErr != nil || expectedDigest != event.EventDigest {
			return nil, fmt.Errorf("stored team lifecycle event chain is invalid")
		}
		previousDigest = event.EventDigest
		result = append(result, event)
	}
	return result, nil
}

func loadPostgresCoordinationMessageByIdempotency(db *gorm.DB, owner, teamID, version, key string) (agentcoordination.Message, bool, error) {
	var rows []postgresCoordinationMessageRow
	if err := db.Raw(`
		SELECT owner_identity, team_id::text AS team_id, team_version,
			message_id::text AS message_id, idempotency_key::text AS idempotency_key,
			correlation_id::text AS correlation_id, payload_digest, created_at,
			payload::text AS payload
		FROM public.agent_team_coordination_messages
		WHERE owner_identity = ? AND team_id = ? AND team_version = ?
			AND idempotency_key = ?`, owner, teamID, version, key,
	).Scan(&rows).Error; err != nil {
		return agentcoordination.Message{}, false, fmt.Errorf("load coordination message idempotency record: %w", err)
	}
	if len(rows) == 0 {
		return agentcoordination.Message{}, false, nil
	}
	if len(rows) != 1 {
		return agentcoordination.Message{}, false, fmt.Errorf("duplicate coordination message idempotency records")
	}
	message, err := decodePostgresCoordinationMessage(rows[0], owner, teamID, version)
	return message, true, err
}

func loadPostgresCoordinationMessage(db *gorm.DB, owner, teamID, version, messageID string) (agentcoordination.Message, bool, error) {
	var rows []postgresCoordinationMessageRow
	if err := db.Raw(`
		SELECT owner_identity, team_id::text AS team_id, team_version,
			message_id::text AS message_id, idempotency_key::text AS idempotency_key,
			correlation_id::text AS correlation_id, payload_digest, created_at,
			payload::text AS payload
		FROM public.agent_team_coordination_messages
		WHERE owner_identity = ? AND team_id = ? AND team_version = ?
			AND message_id = ?`, owner, teamID, version, messageID,
	).Scan(&rows).Error; err != nil {
		return agentcoordination.Message{}, false, fmt.Errorf("load coordination message: %w", err)
	}
	if len(rows) == 0 {
		return agentcoordination.Message{}, false, nil
	}
	if len(rows) != 1 {
		return agentcoordination.Message{}, false, fmt.Errorf("duplicate coordination message records")
	}
	message, err := decodePostgresCoordinationMessage(rows[0], owner, teamID, version)
	return message, true, err
}

func decodePostgresCoordinationMessage(row postgresCoordinationMessageRow, owner, teamID, version string) (agentcoordination.Message, error) {
	var message agentcoordination.Message
	if err := decodeStrictAgentTeamJSON("coordination message", row.Payload, &message); err != nil {
		return agentcoordination.Message{}, err
	}
	expectedDigest, err := agentcoordination.ComputeMessageDigest(message)
	if err != nil || row.OwnerIdentity != owner || row.TeamID != teamID || row.TeamVersion != version ||
		message.ID != row.MessageID || message.IdempotencyKey != row.IdempotencyKey ||
		message.CorrelationID != row.CorrelationID || message.PayloadDigest != row.PayloadDigest ||
		!postgresTimesEqual(message.CreatedAt, row.CreatedAt) ||
		!strings.EqualFold(expectedDigest, strings.TrimSpace(message.PayloadDigest)) {
		return agentcoordination.Message{}, fmt.Errorf("stored coordination message is invalid")
	}
	return message, nil
}

func loadPostgresAcknowledgmentByIdempotency(db *gorm.DB, owner, teamID, version, key string) (agentcoordination.Acknowledgment, bool, error) {
	rows, err := queryPostgresMessageAcknowledgments(db, `
		SELECT owner_identity, team_id::text AS team_id, team_version,
			acknowledgment_id::text AS acknowledgment_id, message_id::text AS message_id,
			correlation_id::text AS correlation_id, recipient_id, status,
			idempotency_key::text AS idempotency_key, acknowledgment_digest,
			created_at, retry_after, payload::text AS payload
		FROM public.agent_team_message_acknowledgments
		WHERE owner_identity = ? AND team_id = ? AND team_version = ?
			AND idempotency_key = ?`, owner, teamID, version, key)
	if err != nil {
		return agentcoordination.Acknowledgment{}, false, err
	}
	if len(rows) == 0 {
		return agentcoordination.Acknowledgment{}, false, nil
	}
	if len(rows) != 1 {
		return agentcoordination.Acknowledgment{}, false, fmt.Errorf("duplicate message acknowledgment idempotency records")
	}
	acknowledgment, err := decodePostgresMessageAcknowledgment(rows[0], owner, teamID, version)
	return acknowledgment, true, err
}

func listPostgresMessageAcknowledgments(db *gorm.DB, owner, teamID, version, messageID string) ([]agentcoordination.Acknowledgment, error) {
	rows, err := queryPostgresMessageAcknowledgments(db, `
		SELECT owner_identity, team_id::text AS team_id, team_version,
			acknowledgment_id::text AS acknowledgment_id, message_id::text AS message_id,
			correlation_id::text AS correlation_id, recipient_id, status,
			idempotency_key::text AS idempotency_key, acknowledgment_digest,
			created_at, retry_after, payload::text AS payload
		FROM public.agent_team_message_acknowledgments
		WHERE owner_identity = ? AND team_id = ? AND team_version = ?
			AND message_id = ?
		ORDER BY created_at ASC, acknowledgment_id ASC`, owner, teamID, version, messageID)
	if err != nil {
		return nil, err
	}
	result := make([]agentcoordination.Acknowledgment, 0, len(rows))
	for _, row := range rows {
		acknowledgment, err := decodePostgresMessageAcknowledgment(row, owner, teamID, version)
		if err != nil {
			return nil, err
		}
		result = append(result, acknowledgment)
	}
	return result, nil
}

func queryPostgresMessageAcknowledgments(db *gorm.DB, query string, args ...any) ([]postgresMessageAcknowledgmentRow, error) {
	var rows []postgresMessageAcknowledgmentRow
	if err := db.Raw(query, args...).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("query agent team message acknowledgments: %w", err)
	}
	return rows, nil
}

func decodePostgresMessageAcknowledgment(row postgresMessageAcknowledgmentRow, owner, teamID, version string) (agentcoordination.Acknowledgment, error) {
	var acknowledgment agentcoordination.Acknowledgment
	if err := decodeStrictAgentTeamJSON("message acknowledgment", row.Payload, &acknowledgment); err != nil {
		return agentcoordination.Acknowledgment{}, err
	}
	digest, err := agentcoordination.ComputeAcknowledgmentDigest(acknowledgment)
	if err != nil || row.OwnerIdentity != owner || row.TeamID != teamID || row.TeamVersion != version ||
		acknowledgment.ID != row.AcknowledgmentID || acknowledgment.MessageID != row.MessageID ||
		acknowledgment.CorrelationID != row.CorrelationID || acknowledgment.RecipientID != row.RecipientID ||
		string(acknowledgment.Status) != row.Status || acknowledgment.IdempotencyKey != row.IdempotencyKey ||
		digest != row.AcknowledgmentDigest || !postgresTimesEqual(acknowledgment.CreatedAt, row.CreatedAt) ||
		!postgresOptionalTimesEqual(acknowledgment.RetryAfter, row.RetryAfter) {
		return agentcoordination.Acknowledgment{}, fmt.Errorf("stored message acknowledgment is invalid")
	}
	return acknowledgment, nil
}

func postgresOptionalTimesEqual(left, right *time.Time) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return postgresTimesEqual(*left, *right)
}

func loadPostgresConsensusByIdempotency(db *gorm.DB, owner, teamID, version, key string) (TeamConsensusOutcome, bool, error) {
	var rows []postgresConsensusOutcomeRow
	if err := db.Raw(`
		SELECT owner_identity, team_id::text AS team_id, team_version,
			outcome_id::text AS outcome_id, idempotency_key::text AS idempotency_key,
			correlation_id::text AS correlation_id, team_revision,
			outcome_status, outcome_digest, recorded_at, payload::text AS payload
		FROM public.agent_team_consensus_outcomes
		WHERE owner_identity = ? AND team_id = ? AND team_version = ?
			AND idempotency_key = ?`, owner, teamID, version, key,
	).Scan(&rows).Error; err != nil {
		return TeamConsensusOutcome{}, false, fmt.Errorf("load consensus idempotency record: %w", err)
	}
	if len(rows) == 0 {
		return TeamConsensusOutcome{}, false, nil
	}
	if len(rows) != 1 {
		return TeamConsensusOutcome{}, false, fmt.Errorf("duplicate consensus idempotency records")
	}
	outcome, err := decodePostgresConsensusOutcome(rows[0], owner, teamID, version)
	return outcome, true, err
}

func decodePostgresConsensusOutcome(row postgresConsensusOutcomeRow, owner, teamID, version string) (TeamConsensusOutcome, error) {
	var outcome TeamConsensusOutcome
	if err := decodeStrictAgentTeamJSON("team consensus outcome", row.Payload, &outcome); err != nil {
		return TeamConsensusOutcome{}, err
	}
	if row.TeamRevision <= 1 || row.OwnerIdentity != owner || row.TeamID != teamID || row.TeamVersion != version ||
		outcome.ID != row.OutcomeID || outcome.TeamID != row.TeamID || outcome.TeamVersion != row.TeamVersion ||
		outcome.IdempotencyKey != row.IdempotencyKey || outcome.CorrelationID != row.CorrelationID ||
		outcome.Status != row.OutcomeStatus || outcome.OutcomeDigest != row.OutcomeDigest ||
		!postgresTimesEqual(outcome.RecordedAt, row.RecordedAt) {
		return TeamConsensusOutcome{}, fmt.Errorf("stored team consensus outcome metadata is inconsistent")
	}
	if err := validatePostgresConsensusOutcome(outcome, teamID, version); err != nil {
		return TeamConsensusOutcome{}, fmt.Errorf("stored team consensus outcome is invalid: %w", err)
	}
	return outcome, nil
}

func validatePostgresConsensusOutcome(outcome TeamConsensusOutcome, teamID, version string) error {
	if !outcome.AdvisoryOnly || outcome.GrantsExecutionAuthority || !outcome.ExecutionAuthorizationRequired ||
		outcome.TeamID != teamID || outcome.TeamVersion != version {
		return fmt.Errorf("consensus outcome violates advisory team boundary")
	}
	expected, err := teamConsensusOutcomeDigest(outcome)
	if err != nil || expected != outcome.OutcomeDigest {
		return fmt.Errorf("consensus outcome digest is invalid")
	}
	return nil
}

func marshalAgentTeamRecord(name string, value any) ([]byte, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode %s: %w", name, err)
	}
	if len(payload) < 2 || payload[0] != '{' || !json.Valid(payload) {
		return nil, fmt.Errorf("encode %s: JSON object required", name)
	}
	return payload, nil
}

func decodeStrictAgentTeamJSON(name, payload string, target any) error {
	payload = strings.TrimSpace(payload)
	if payload == "" || payload == "null" {
		return fmt.Errorf("decode %s: JSON object required", name)
	}
	decoder := json.NewDecoder(strings.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode %s: %w", name, err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			err = fmt.Errorf("multiple JSON values")
		}
		return fmt.Errorf("decode %s: %w", name, err)
	}
	return nil
}

func postgresTimesEqual(left, right time.Time) bool {
	difference := left.UTC().Sub(right.UTC())
	if difference < 0 {
		difference = -difference
	}
	return difference <= time.Microsecond
}

func sortAgentTeams(teams []AgentTeamContract, versions bool) {
	if versions {
		sort.SliceStable(teams, func(i, j int) bool {
			return compareSemanticVersions(teams[i].Version, teams[j].Version) > 0
		})
		return
	}
	sort.SliceStable(teams, func(i, j int) bool {
		if teams[i].Key != teams[j].Key {
			return teams[i].Key < teams[j].Key
		}
		return teams[i].ID < teams[j].ID
	})
}

var _ AgentTeamRepository = (*PostgresAgentTeamRepository)(nil)
