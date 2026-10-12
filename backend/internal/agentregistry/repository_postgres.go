package agentregistry

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"automation-hub-backend/internal/infra"

	"gorm.io/gorm"
)

// PostgresRepository persists the current agent record and its immutable
// lifecycle and assignment ledgers in the canonical PostgreSQL database.
type PostgresRepository struct {
	DB *gorm.DB
}

var _ Repository = (*PostgresRepository)(nil)

func NewPostgresRepository(db *gorm.DB) *PostgresRepository {
	return &PostgresRepository{DB: db}
}

func DefaultRepository() (Repository, error) {
	db, err := infra.GetDefaultDB()
	if err != nil {
		return nil, fmt.Errorf("open agent registry database: %w", err)
	}
	return NewPostgresRepository(db), nil
}

func (r *PostgresRepository) Create(ctx context.Context, agent Agent) (Agent, error) {
	if err := r.ready(); err != nil {
		return Agent{}, err
	}
	if err := ValidateAgent(agent, time.Now().UTC()); err != nil {
		return Agent{}, fmt.Errorf("validate agent for persistence: %w", err)
	}
	payload, err := marshalRegistryPayload("agent", agent)
	if err != nil {
		return Agent{}, err
	}
	created := false
	err = r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Exec(`
			INSERT INTO public.agent_registry_agents (
				owner_identity, id, revision, contract_version, agent_type,
				lifecycle_state, runtime_adapter_id, health_status,
				created_at, updated_at, payload
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CAST(? AS jsonb))
			ON CONFLICT (owner_identity, id) DO NOTHING`,
			agent.OwnerIdentity,
			agent.ID,
			agent.Revision,
			agent.ContractVersion,
			string(agent.Type),
			string(agent.State),
			agent.Runtime.ID,
			string(agent.Health.Status),
			agent.CreatedAt.UTC(),
			agent.UpdatedAt.UTC(),
			string(payload),
		)
		if result.Error != nil {
			return fmt.Errorf("create agent: %w", result.Error)
		}
		if result.RowsAffected != 1 {
			return nil
		}
		if err := insertAgentRevision(tx, agent, payload); err != nil {
			return err
		}
		created = true
		return nil
	})
	if err != nil {
		return Agent{}, err
	}
	if !created {
		return Agent{}, ErrAlreadyExists
	}
	return cloneAgent(agent), nil
}

func (r *PostgresRepository) Get(ctx context.Context, owner, id string) (Agent, error) {
	if err := r.ready(); err != nil {
		return Agent{}, err
	}
	if err := validateLookup(owner, id); err != nil {
		return Agent{}, err
	}
	db, finish, err := infra.PostgresExecutionDB(ctx, r.DB)
	if err != nil {
		return Agent{}, err
	}
	defer finish()
	var payload []byte
	err = db.Raw(`
		SELECT payload
		FROM public.agent_registry_agents
		WHERE owner_identity = ? AND id = ?`,
		owner,
		id,
	).Row().Scan(&payload)
	if isRecordNotFound(err) {
		return Agent{}, ErrNotFound
	}
	if err != nil {
		return Agent{}, fmt.Errorf("get agent: %w", err)
	}
	return decodeAgent(payload)
}

func (r *PostgresRepository) List(ctx context.Context, owner string) ([]Agent, error) {
	if err := r.ready(); err != nil {
		return nil, err
	}
	if err := validateIdentity(owner); err != nil {
		return nil, err
	}
	rows, err := r.DB.WithContext(ctx).Raw(`
		SELECT payload
		FROM public.agent_registry_agents
		WHERE owner_identity = ?
		ORDER BY id ASC`,
		owner,
	).Rows()
	if err != nil {
		return nil, fmt.Errorf("list agents: %w", err)
	}
	defer rows.Close()

	result := make([]Agent, 0)
	for rows.Next() {
		var payload []byte
		if err := rows.Scan(&payload); err != nil {
			return nil, fmt.Errorf("scan agent: %w", err)
		}
		agent, err := decodeAgent(payload)
		if err != nil {
			return nil, err
		}
		result = append(result, agent)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate agents: %w", err)
	}
	return result, nil
}

func (r *PostgresRepository) CompareAndSwap(
	ctx context.Context,
	agent Agent,
	expected uint64,
) (Agent, error) {
	if err := r.ready(); err != nil {
		return Agent{}, err
	}
	if expected == 0 || agent.Revision != expected+1 {
		return Agent{}, ErrConflict
	}
	if err := ValidateAgent(agent, time.Now().UTC()); err != nil {
		return Agent{}, fmt.Errorf("validate agent for compare-and-swap: %w", err)
	}
	payload, err := marshalRegistryPayload("agent", agent)
	if err != nil {
		return Agent{}, err
	}
	updated := false
	err = r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		ok, err := updateAgentRow(tx, agent, expected, payload)
		if err != nil {
			return err
		}
		if !ok {
			return nil
		}
		if err := insertAgentRevision(tx, agent, payload); err != nil {
			return err
		}
		updated = true
		return nil
	})
	if err != nil {
		return Agent{}, err
	}
	if !updated {
		exists, err := r.agentExists(ctx, agent.OwnerIdentity, agent.ID)
		if err != nil {
			return Agent{}, err
		}
		if !exists {
			return Agent{}, ErrNotFound
		}
		return Agent{}, ErrConflict
	}
	return cloneAgent(agent), nil
}

func (r *PostgresRepository) Transition(
	ctx context.Context,
	agent Agent,
	expected uint64,
	transition Transition,
) (Agent, error) {
	if err := r.ready(); err != nil {
		return Agent{}, err
	}
	if expected == 0 || agent.Revision != expected+1 ||
		transition.Revision != agent.Revision ||
		transition.To != agent.State {
		return Agent{}, ErrConflict
	}
	if err := ValidateAgent(agent, time.Now().UTC()); err != nil {
		return Agent{}, fmt.Errorf("validate transitioned agent: %w", err)
	}
	if err := validateTransition(transition); err != nil {
		return Agent{}, err
	}
	agentPayload, err := marshalRegistryPayload("agent", agent)
	if err != nil {
		return Agent{}, err
	}
	transitionPayload, err := marshalRegistryPayload("transition", transition)
	if err != nil {
		return Agent{}, err
	}
	updated := false
	err = r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var currentState string
		err := tx.Raw(`
			SELECT lifecycle_state
			FROM public.agent_registry_agents
			WHERE owner_identity = ? AND id = ? AND revision = ?
			FOR UPDATE`,
			agent.OwnerIdentity,
			agent.ID,
			expected,
		).Row().Scan(&currentState)
		if isRecordNotFound(err) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("lock agent for transition: %w", err)
		}
		if currentState != string(transition.From) {
			return ErrConflict
		}
		ok, err := updateAgentRow(tx, agent, expected, agentPayload)
		if err != nil {
			return err
		}
		if !ok {
			return nil
		}
		if err := insertAgentRevision(tx, agent, agentPayload); err != nil {
			return err
		}
		result := tx.Exec(`
			INSERT INTO public.agent_registry_transitions (
				owner_identity, agent_id, revision, from_state, to_state,
				occurred_at, payload
			) VALUES (?, ?, ?, ?, ?, ?, CAST(? AS jsonb))`,
			agent.OwnerIdentity,
			agent.ID,
			transition.Revision,
			string(transition.From),
			string(transition.To),
			transition.OccurredAt.UTC(),
			string(transitionPayload),
		)
		if result.Error != nil {
			return fmt.Errorf("append agent transition: %w", result.Error)
		}
		updated = true
		return nil
	})
	if err != nil {
		return Agent{}, err
	}
	if !updated {
		exists, err := r.agentExists(ctx, agent.OwnerIdentity, agent.ID)
		if err != nil {
			return Agent{}, err
		}
		if !exists {
			return Agent{}, ErrNotFound
		}
		return Agent{}, ErrConflict
	}
	return cloneAgent(agent), nil
}

func (r *PostgresRepository) ListTransitions(
	ctx context.Context,
	owner, id string,
) ([]Transition, error) {
	if err := r.ready(); err != nil {
		return nil, err
	}
	if err := validateLookup(owner, id); err != nil {
		return nil, err
	}
	exists, err := r.agentExists(ctx, owner, id)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrNotFound
	}
	rows, err := r.DB.WithContext(ctx).Raw(`
		SELECT payload
		FROM public.agent_registry_transitions
		WHERE owner_identity = ? AND agent_id = ?
		ORDER BY revision ASC, sequence_id ASC`,
		owner,
		id,
	).Rows()
	if err != nil {
		return nil, fmt.Errorf("list agent transitions: %w", err)
	}
	defer rows.Close()

	result := make([]Transition, 0)
	for rows.Next() {
		var payload []byte
		if err := rows.Scan(&payload); err != nil {
			return nil, fmt.Errorf("scan agent transition: %w", err)
		}
		var transition Transition
		if err := unmarshalRegistryPayload("transition", payload, &transition); err != nil {
			return nil, err
		}
		if err := validateTransition(transition); err != nil {
			return nil, fmt.Errorf("stored transition is invalid: %w", err)
		}
		result = append(result, transition)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate agent transitions: %w", err)
	}
	return result, nil
}

func (r *PostgresRepository) CreateAssignment(
	ctx context.Context,
	assignment Assignment,
) (Agent, error) {
	if err := r.ready(); err != nil {
		return Agent{}, err
	}
	if err := validateAssignment(assignment); err != nil {
		return Agent{}, err
	}
	payload, err := marshalRegistryPayload("assignment", assignment)
	if err != nil {
		return Agent{}, err
	}
	var reserved Agent
	err = r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing bool
		if err := tx.Raw(`
			SELECT EXISTS (
				SELECT 1 FROM public.agent_registry_assignments
				WHERE owner_identity = ? AND id = ?
			)`,
			assignment.OwnerIdentity,
			assignment.ID,
		).Row().Scan(&existing); err != nil {
			return fmt.Errorf("check assignment existence: %w", err)
		}
		if existing {
			return ErrAssignmentExists
		}

		var agentPayload []byte
		err := tx.Raw(`
			SELECT payload
			FROM public.agent_registry_agents
			WHERE owner_identity = ? AND id = ?
			FOR UPDATE`,
			assignment.OwnerIdentity,
			assignment.AgentID,
		).Row().Scan(&agentPayload)
		if isRecordNotFound(err) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("lock agent for assignment: %w", err)
		}
		current, err := decodeAgent(agentPayload)
		if err != nil {
			return err
		}
		if current.Revision != assignment.AgentRevision ||
			current.Availability.ActiveAssignments >= current.Availability.MaxConcurrent {
			return ErrConflict
		}

		result := tx.Exec(`
			INSERT INTO public.agent_registry_assignments (
				owner_identity, id, task_id, agent_id, agent_revision,
				granted_authority, granted_autonomy, assigned_at, payload
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, CAST(? AS jsonb))`,
			assignment.OwnerIdentity,
			assignment.ID,
			assignment.TaskID,
			assignment.AgentID,
			assignment.AgentRevision,
			assignment.GrantedAuthority,
			assignment.GrantedAutonomy,
			assignment.AssignedAt.UTC(),
			string(payload),
		)
		if result.Error != nil {
			return fmt.Errorf("create agent assignment: %w", result.Error)
		}

		reserved = cloneAgent(current)
		reserved.Availability.ActiveAssignments++
		reserved.Revision++
		reserved.UpdatedAt = assignment.AssignedAt.UTC()
		if err := ValidateAgent(reserved, time.Now().UTC()); err != nil {
			return fmt.Errorf("validate reserved agent: %w", err)
		}
		reservedPayload, err := marshalRegistryPayload("agent", reserved)
		if err != nil {
			return err
		}
		ok, err := updateAgentRow(tx, reserved, current.Revision, reservedPayload)
		if err != nil {
			return err
		}
		if !ok {
			return ErrConflict
		}
		return insertAgentRevision(tx, reserved, reservedPayload)
	})
	if err != nil {
		return Agent{}, err
	}
	return cloneAgent(reserved), nil
}

func (r *PostgresRepository) GetAssignment(
	ctx context.Context,
	owner, id string,
) (Assignment, error) {
	if err := r.ready(); err != nil {
		return Assignment{}, err
	}
	if err := validateLookup(owner, id); err != nil {
		return Assignment{}, err
	}
	db, finish, err := infra.PostgresExecutionDB(ctx, r.DB)
	if err != nil {
		return Assignment{}, err
	}
	defer finish()
	var payload []byte
	err = db.Raw(`
		SELECT payload
		FROM public.agent_registry_assignments
		WHERE owner_identity = ? AND id = ?`,
		owner,
		id,
	).Row().Scan(&payload)
	if isRecordNotFound(err) {
		return Assignment{}, ErrNotFound
	}
	if err != nil {
		return Assignment{}, fmt.Errorf("get agent assignment: %w", err)
	}
	var assignment Assignment
	if err := unmarshalRegistryPayload("assignment", payload, &assignment); err != nil {
		return Assignment{}, err
	}
	if err := validateAssignment(assignment); err != nil {
		return Assignment{}, fmt.Errorf("stored assignment is invalid: %w", err)
	}
	return cloneAssignment(assignment), nil
}

func (r *PostgresRepository) RecordAssignmentOutcome(
	ctx context.Context,
	outcome AssignmentOutcome,
	updated Agent,
	expected uint64,
) (Agent, error) {
	if err := r.ready(); err != nil {
		return Agent{}, err
	}
	if err := validateAssignmentOutcome(outcome); err != nil {
		return Agent{}, err
	}
	if expected == 0 || updated.Revision != expected+1 ||
		updated.OwnerIdentity != outcome.OwnerIdentity ||
		updated.ID != outcome.AgentID {
		return Agent{}, ErrConflict
	}
	if err := ValidateAgent(updated, time.Now().UTC()); err != nil {
		return Agent{}, fmt.Errorf("validate agent after assignment outcome: %w", err)
	}
	outcomePayload, err := marshalRegistryPayload("assignment outcome", outcome)
	if err != nil {
		return Agent{}, err
	}
	agentPayload, err := marshalRegistryPayload("agent", updated)
	if err != nil {
		return Agent{}, err
	}

	recorded := false
	err = r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var assignmentAgentID string
		err := tx.Raw(`
			SELECT agent_id
			FROM public.agent_registry_assignments
			WHERE owner_identity = ? AND id = ?
			FOR UPDATE`,
			outcome.OwnerIdentity,
			outcome.AssignmentID,
		).Row().Scan(&assignmentAgentID)
		if isRecordNotFound(err) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("lock assignment for outcome: %w", err)
		}
		if assignmentAgentID != outcome.AgentID {
			return ErrConflict
		}

		result := tx.Exec(`
			INSERT INTO public.agent_registry_assignment_outcomes (
				owner_identity, assignment_id, agent_id,
				success, latency_ns, recorded_at, payload
			) VALUES (?, ?, ?, ?, ?, ?, CAST(? AS jsonb))
			ON CONFLICT (owner_identity, assignment_id) DO NOTHING`,
			outcome.OwnerIdentity,
			outcome.AssignmentID,
			outcome.AgentID,
			outcome.Success,
			outcome.Latency.Nanoseconds(),
			outcome.RecordedAt.UTC(),
			string(outcomePayload),
		)
		if result.Error != nil {
			return fmt.Errorf("record assignment outcome: %w", result.Error)
		}
		if result.RowsAffected != 1 {
			return ErrConflict
		}

		ok, err := updateAgentRow(tx, updated, expected, agentPayload)
		if err != nil {
			return err
		}
		if !ok {
			return ErrConflict
		}
		if err := insertAgentRevision(tx, updated, agentPayload); err != nil {
			return err
		}
		recorded = true
		return nil
	})
	if err != nil {
		return Agent{}, err
	}
	if !recorded {
		return Agent{}, ErrConflict
	}
	return cloneAgent(updated), nil
}

func (r *PostgresRepository) ready() error {
	if r == nil || r.DB == nil {
		return fmt.Errorf("agent registry database is required")
	}
	return nil
}

func updateAgentRow(
	tx *gorm.DB,
	agent Agent,
	expected uint64,
	payload []byte,
) (bool, error) {
	result := tx.Exec(`
		UPDATE public.agent_registry_agents
		SET revision = ?,
			contract_version = ?,
			agent_type = ?,
			lifecycle_state = ?,
			runtime_adapter_id = ?,
			health_status = ?,
			updated_at = ?,
			payload = CAST(? AS jsonb)
		WHERE owner_identity = ? AND id = ? AND revision = ?`,
		agent.Revision,
		agent.ContractVersion,
		string(agent.Type),
		string(agent.State),
		agent.Runtime.ID,
		string(agent.Health.Status),
		agent.UpdatedAt.UTC(),
		string(payload),
		agent.OwnerIdentity,
		agent.ID,
		expected,
	)
	if result.Error != nil {
		return false, fmt.Errorf("compare-and-swap agent: %w", result.Error)
	}
	return result.RowsAffected == 1, nil
}

func insertAgentRevision(tx *gorm.DB, agent Agent, payload []byte) error {
	result := tx.Exec(`
		INSERT INTO public.agent_registry_revisions (
			owner_identity, agent_id, revision, recorded_at, payload
		) VALUES (?, ?, ?, ?, CAST(? AS jsonb))`,
		agent.OwnerIdentity,
		agent.ID,
		agent.Revision,
		agent.UpdatedAt.UTC(),
		string(payload),
	)
	if result.Error != nil {
		return fmt.Errorf("record agent revision: %w", result.Error)
	}
	return nil
}

func (r *PostgresRepository) agentExists(ctx context.Context, owner, id string) (bool, error) {
	var exists bool
	err := r.DB.WithContext(ctx).Raw(`
		SELECT EXISTS (
			SELECT 1 FROM public.agent_registry_agents
			WHERE owner_identity = ? AND id = ?
		)`,
		owner,
		id,
	).Row().Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check agent existence: %w", err)
	}
	return exists, nil
}

func (r *PostgresRepository) assignmentExists(
	ctx context.Context,
	owner, id string,
) (bool, error) {
	var exists bool
	err := r.DB.WithContext(ctx).Raw(`
		SELECT EXISTS (
			SELECT 1 FROM public.agent_registry_assignments
			WHERE owner_identity = ? AND id = ?
		)`,
		owner,
		id,
	).Row().Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check assignment existence: %w", err)
	}
	return exists, nil
}

func decodeAgent(payload []byte) (Agent, error) {
	var agent Agent
	if err := unmarshalRegistryPayload("agent", payload, &agent); err != nil {
		return Agent{}, err
	}
	if err := ValidateAgent(agent, time.Now().UTC()); err != nil {
		return Agent{}, fmt.Errorf("stored agent is invalid: %w", err)
	}
	return cloneAgent(agent), nil
}

func validateLookup(owner, id string) error {
	if err := validateIdentity(owner); err != nil {
		return err
	}
	if err := validateIdentifier("record id", id); err != nil {
		return err
	}
	return nil
}

func validateTransition(transition Transition) error {
	if !validLifecycle(transition.From) || !validLifecycle(transition.To) {
		return fmt.Errorf("invalid agent transition state")
	}
	if transition.From == transition.To {
		return fmt.Errorf("agent transition must change state")
	}
	if err := validateText("transition reason", transition.Reason, true); err != nil {
		return err
	}
	if transition.OccurredAt.IsZero() ||
		transition.OccurredAt.After(time.Now().UTC().Add(time.Minute)) {
		return fmt.Errorf("invalid agent transition timestamp")
	}
	if transition.Revision < 2 {
		return fmt.Errorf("agent transition revision must be at least 2")
	}
	return nil
}

func validateAssignment(assignment Assignment) error {
	if err := validateIdentifier("assignment id", assignment.ID); err != nil {
		return err
	}
	if err := validateIdentity(assignment.OwnerIdentity); err != nil {
		return err
	}
	if err := validateIdentifier("assignment task id", assignment.TaskID); err != nil {
		return err
	}
	if err := validateIdentifier("assignment agent id", assignment.AgentID); err != nil {
		return err
	}
	if assignment.AgentRevision == 0 {
		return fmt.Errorf("assignment agent revision must be positive")
	}
	if assignment.GrantedAuthority < 0 || assignment.GrantedAuthority > 10 ||
		assignment.GrantedAutonomy < 0 || assignment.GrantedAutonomy > 10 {
		return fmt.Errorf("assignment authority and autonomy must be between 0 and 10")
	}
	if assignment.Score < 0 || assignment.Score > 1 {
		return fmt.Errorf("assignment score must be between 0 and 1")
	}
	if strings.TrimSpace(assignment.RequestDigest) == "" ||
		len(assignment.RequestDigest) > maxTextLength ||
		secretPattern.MatchString(assignment.RequestDigest) {
		return fmt.Errorf("invalid assignment request digest")
	}
	if assignment.AssignedAt.IsZero() ||
		assignment.AssignedAt.After(time.Now().UTC().Add(time.Minute)) {
		return fmt.Errorf("invalid assignment timestamp")
	}
	return nil
}

func validateAssignmentOutcome(outcome AssignmentOutcome) error {
	if err := validateIdentifier("assignment outcome id", outcome.AssignmentID); err != nil {
		return err
	}
	if err := validateIdentity(outcome.OwnerIdentity); err != nil {
		return err
	}
	if err := validateIdentifier("assignment outcome agent id", outcome.AgentID); err != nil {
		return err
	}
	if outcome.Latency < 0 {
		return fmt.Errorf("assignment outcome latency cannot be negative")
	}
	if outcome.RecordedAt.IsZero() ||
		outcome.RecordedAt.After(time.Now().UTC().Add(time.Minute)) {
		return fmt.Errorf("invalid assignment outcome timestamp")
	}
	return nil
}

func marshalRegistryPayload(kind string, value any) ([]byte, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("marshal %s payload: %w", kind, err)
	}
	if len(payload) == 0 || string(payload) == "null" {
		return nil, fmt.Errorf("%s payload is empty", kind)
	}
	return payload, nil
}

func unmarshalRegistryPayload(kind string, payload []byte, target any) error {
	if len(payload) == 0 {
		return fmt.Errorf("%s payload is empty", kind)
	}
	decoder := json.NewDecoder(strings.NewReader(string(payload)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode %s payload: %w", kind, err)
	}
	return nil
}

func isRecordNotFound(err error) bool {
	return errors.Is(err, gorm.ErrRecordNotFound) ||
		errors.Is(err, sql.ErrNoRows) ||
		strings.Contains(strings.ToLower(fmt.Sprint(err)), "no rows")
}
