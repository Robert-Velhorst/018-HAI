package agentregistry

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/infra"
	"automation-hub-backend/internal/pgtestguard"
	"automation-hub-backend/migrations"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestPostgresRepositoryFailsClosedWithoutDatabase(t *testing.T) {
	repository := NewPostgresRepository(nil)
	agent := postgresTestAgent("owner@example.com", "agent")
	if _, err := repository.Create(context.Background(), agent); err == nil {
		t.Fatal("Create with nil database succeeded")
	}
	if _, err := repository.Get(context.Background(), agent.OwnerIdentity, agent.ID); err == nil {
		t.Fatal("Get with nil database succeeded")
	}
	if _, err := repository.CreateAssignment(context.Background(), postgresTestAssignment(agent)); err == nil {
		t.Fatal("CreateAssignment with nil database succeeded")
	}
}

func TestPostgresRepositoryRejectsMalformedRecordsBeforeSQL(t *testing.T) {
	repository := NewPostgresRepository(&gorm.DB{})
	agent := postgresTestAgent("owner@example.com", "agent")
	agent.Revision = 0
	if _, err := repository.Create(context.Background(), agent); err == nil {
		t.Fatal("Create accepted zero revision")
	}
	assignment := postgresTestAssignment(postgresTestAgent("owner@example.com", "agent"))
	assignment.GrantedAuthority = 11
	if _, err := repository.CreateAssignment(context.Background(), assignment); err == nil {
		t.Fatal("CreateAssignment accepted excessive authority")
	}
}

func TestPostgresRepositoryRoundTripOwnerIsolationCASAndImmutableLedgers(t *testing.T) {
	repository, db := agentRegistryPostgresRepository(t)
	ctx := context.Background()
	owner := "owner-" + uuid.NewString() + "@example.com"
	agent := postgresTestAgent(owner, "worker-"+uuid.NewString())

	created, err := repository.Create(ctx, agent)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !reflect.DeepEqual(created, agent) {
		t.Fatalf("created agent differs:\ngot  %#v\nwant %#v", created, agent)
	}
	stored, err := repository.Get(ctx, owner, agent.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !reflect.DeepEqual(stored, agent) {
		t.Fatalf("stored agent differs:\ngot  %#v\nwant %#v", stored, agent)
	}
	if _, err := repository.Get(ctx, "other-"+owner, agent.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-owner Get error = %v, want ErrNotFound", err)
	}
	list, err := repository.List(ctx, owner)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 || !reflect.DeepEqual(list[0], agent) {
		t.Fatalf("List = %#v, want one exact agent", list)
	}
	if _, err := repository.Create(ctx, agent); !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("duplicate Create error = %v, want ErrAlreadyExists", err)
	}

	updated := cloneAgent(agent)
	updated.Name = "Updated worker"
	updated.Revision = 2
	updated.UpdatedAt = updated.UpdatedAt.Add(time.Second)
	if _, err := repository.CompareAndSwap(ctx, updated, 0); !errors.Is(err, ErrConflict) {
		t.Fatalf("invalid expected revision error = %v, want ErrConflict", err)
	}
	saved, err := repository.CompareAndSwap(ctx, updated, 1)
	if err != nil {
		t.Fatalf("CompareAndSwap: %v", err)
	}
	if !reflect.DeepEqual(saved, updated) {
		t.Fatalf("saved agent differs:\ngot  %#v\nwant %#v", saved, updated)
	}
	stale := cloneAgent(updated)
	stale.Revision = 3
	stale.UpdatedAt = stale.UpdatedAt.Add(time.Second)
	if _, err := repository.CompareAndSwap(ctx, stale, 1); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale CompareAndSwap error = %v, want ErrConflict", err)
	}

	transitioned := cloneAgent(saved)
	transitioned.State = StateEnabled
	transitioned.Revision = saved.Revision + 1
	transitioned.UpdatedAt = saved.UpdatedAt.Add(time.Second)
	transition := Transition{
		From:       StateRegistered,
		To:         StateEnabled,
		Reason:     "operator enabled",
		OccurredAt: transitioned.UpdatedAt,
		Revision:   transitioned.Revision,
	}
	transitioned, err = repository.Transition(ctx, transitioned, saved.Revision, transition)
	if err != nil {
		t.Fatalf("Transition: %v", err)
	}
	transitions, err := repository.ListTransitions(ctx, owner, agent.ID)
	if err != nil {
		t.Fatalf("ListTransitions: %v", err)
	}
	if len(transitions) != 1 || !reflect.DeepEqual(transitions[0], transition) {
		t.Fatalf("transitions = %#v, want %#v", transitions, []Transition{transition})
	}
	if _, err := repository.Transition(ctx, transitioned, saved.Revision, transition); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate transition error = %v, want ErrConflict", err)
	}

	assignment := postgresTestAssignment(transitioned)
	reserved, err := repository.CreateAssignment(ctx, assignment)
	if err != nil {
		t.Fatalf("CreateAssignment: %v", err)
	}
	if reserved.Revision != transitioned.Revision+1 ||
		reserved.Availability.ActiveAssignments != transitioned.Availability.ActiveAssignments+1 {
		t.Fatalf("reserved agent = %#v, want revision and active assignment incremented", reserved)
	}
	gotAssignment, err := repository.GetAssignment(ctx, owner, assignment.ID)
	if err != nil {
		t.Fatalf("GetAssignment: %v", err)
	}
	if !reflect.DeepEqual(gotAssignment, assignment) {
		t.Fatalf("assignment differs:\ngot  %#v\nwant %#v", gotAssignment, assignment)
	}
	if _, err := repository.GetAssignment(ctx, "other-"+owner, assignment.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-owner assignment read error = %v, want ErrNotFound", err)
	}
	if _, err := repository.CreateAssignment(ctx, assignment); !errors.Is(err, ErrAssignmentExists) {
		t.Fatalf("duplicate assignment error = %v, want ErrAssignmentExists", err)
	}

	if err := db.Exec(`
		UPDATE public.agent_registry_transitions
		SET payload = '{}'::jsonb
		WHERE owner_identity = ? AND agent_id = ?`,
		owner,
		agent.ID,
	).Error; err == nil {
		t.Fatal("database allowed transition mutation")
	}
	if err := db.Exec(`
		DELETE FROM public.agent_registry_assignments
		WHERE owner_identity = ? AND id = ?`,
		owner,
		assignment.ID,
	).Error; err == nil {
		t.Fatal("database allowed assignment deletion")
	}

	outcomeAgent := cloneAgent(reserved)
	outcomeAgent.Availability.ActiveAssignments--
	outcomeAgent.Reliability.Successes++
	outcomeAgent.Reliability.MeanLatencyMs = 500
	outcomeAgent.Reliability.LastOutcomeAt = assignment.AssignedAt.Add(time.Second)
	outcomeAgent.Revision++
	outcomeAgent.UpdatedAt = assignment.AssignedAt.Add(time.Second)
	outcome := AssignmentOutcome{
		AssignmentID:  assignment.ID,
		OwnerIdentity: owner,
		AgentID:       agent.ID,
		Success:       true,
		Latency:       500 * time.Millisecond,
		RecordedAt:    outcomeAgent.UpdatedAt,
	}
	recordedAgent, err := repository.RecordAssignmentOutcome(
		ctx,
		outcome,
		outcomeAgent,
		reserved.Revision,
	)
	if err != nil {
		t.Fatalf("RecordAssignmentOutcome: %v", err)
	}
	if recordedAgent.Availability.ActiveAssignments != 0 {
		t.Fatalf("active assignments = %d, want 0", recordedAgent.Availability.ActiveAssignments)
	}
	if _, err := repository.RecordAssignmentOutcome(
		ctx,
		outcome,
		outcomeAgent,
		reserved.Revision,
	); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate outcome error = %v, want ErrConflict", err)
	}
	if err := db.Exec(`
		UPDATE public.agent_registry_assignment_outcomes
		SET payload = '{}'::jsonb
		WHERE owner_identity = ? AND assignment_id = ?`,
		owner,
		assignment.ID,
	).Error; err == nil {
		t.Fatal("database allowed assignment outcome mutation")
	}

	var revisionCount int64
	if err := db.Raw(`
		SELECT count(*)
		FROM public.agent_registry_revisions
		WHERE owner_identity = ? AND agent_id = ?`,
		owner,
		agent.ID,
	).Scan(&revisionCount).Error; err != nil {
		t.Fatalf("count agent revisions: %v", err)
	}
	if revisionCount != 5 {
		t.Fatalf("revision history count = %d, want 5", revisionCount)
	}
	if err := db.Exec(`
		DELETE FROM public.agent_registry_revisions
		WHERE owner_identity = ? AND agent_id = ? AND revision = 1`,
		owner,
		agent.ID,
	).Error; err == nil {
		t.Fatal("database allowed agent revision deletion")
	}

	invalidID := "invalid-" + uuid.NewString()
	if err := db.Exec(`
		INSERT INTO public.agent_registry_agents (
			owner_identity, id, revision, contract_version, agent_type,
			lifecycle_state, runtime_adapter_id, health_status,
			created_at, updated_at, payload
		) VALUES (?, ?, 1, 1, 'executor', 'registered', 'hermes',
			'healthy', now(), now(), '{}'::jsonb)`,
		owner,
		invalidID,
	).Error; err == nil {
		t.Fatal("database accepted an empty agent payload")
	}
}

func TestPostgresAgentRegistryMigrationCanReplayAgainstExistingSchema(t *testing.T) {
	_, db := agentRegistryPostgresRepository(t)
	before, err := infra.Status(db, migrations.Files, "pre")
	if err != nil {
		t.Fatalf("read migration status before replay: %v", err)
	}
	// Replay the DDL to prove its idempotence without forging a hole in the
	// migration ledger, which the runner deliberately rejects.
	ddl, err := migrations.Files.ReadFile("pre/0013_agent_registry.up.sql")
	if err != nil {
		t.Fatalf("read agent registry migration: %v", err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error { return tx.Exec(string(ddl)).Error }); err != nil {
		t.Fatalf("replay agent registry migration DDL: %v", err)
	}
	after, err := infra.Status(db, migrations.Files, "pre")
	if err != nil || !reflect.DeepEqual(before, after) || len(after.Pending) != 0 {
		t.Fatalf("DDL replay changed migration status: before=%+v after=%+v err=%v", before, after, err)
	}
	var triggerCount int64
	if err := db.Raw(`
		SELECT count(*)
		FROM pg_trigger
		WHERE NOT tgisinternal
		  AND tgname IN (
			'trg_agent_registry_agent_revision',
			'trg_agent_registry_revisions_immutable',
			'trg_agent_registry_transitions_immutable',
			'trg_agent_registry_assignments_immutable',
			'trg_agent_registry_outcomes_immutable'
		  )`).Scan(&triggerCount).Error; err != nil {
		t.Fatalf("count replayed registry triggers: %v", err)
	}
	if triggerCount != 5 {
		t.Fatalf("registry trigger count = %d, want 5", triggerCount)
	}
}

func agentRegistryPostgresRepository(t *testing.T) (*PostgresRepository, *gorm.DB) {
	t.Helper()
	dsn := pgtestguard.RequireDedicatedPostgresTestDSN(t, "HAI_AGENTREGISTRY_TEST_DATABASE_DSN", "hai_agentregistry_test")
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open Postgres: %v", err)
	}
	var databaseName string
	if err := db.Raw("SELECT current_database()").Scan(&databaseName).Error; err != nil {
		t.Fatalf("read connected database identity: %v", err)
	}
	if databaseName != "hai_agentregistry_test" {
		t.Fatalf("refusing migrations against unexpected database identity %q", databaseName)
	}
	if _, err := infra.ApplyMigrations(db, migrations.Files, "pre"); err != nil {
		t.Fatalf("apply pre migrations: %v", err)
	}
	return NewPostgresRepository(db), db
}

func postgresTestAgent(owner, id string) Agent {
	now := time.Now().UTC().Truncate(time.Microsecond).Add(-time.Minute)
	return Agent{
		ContractVersion: ContractVersion,
		ID:              id,
		OwnerIdentity:   owner,
		Name:            "Postgres worker",
		Type:            AgentTypeExecutor,
		Runtime: RuntimeAdapter{
			ID:              "hermes",
			Type:            "claw",
			ProtocolVersion: "1.2.0",
		},
		Capabilities: []CapabilityDeclaration{{
			ID:          "code",
			Version:     "2.1.0",
			Operations:  []string{"read", "write"},
			Description: "Controlled code work",
		}},
		AuthorityCeiling: 5,
		AutonomyCeiling:  4,
		ToolAllowlist:    []string{"editor", "git"},
		DataAllowlist:    []string{"project:hai"},
		FolderAllowlist:  []string{"C:/workspace/hai"},
		Health: HealthEvidence{
			Status:    HealthHealthy,
			Ready:     true,
			CheckedAt: now,
			FreshFor:  time.Hour,
		},
		State: StateRegistered,
		Availability: Availability{
			Available:     true,
			MaxConcurrent: 2,
		},
		Performance: PerformanceProfile{
			P95LatencyMs: 900,
			Locality:     LocalityLocal,
		},
		Reliability: ReliabilityEvidence{
			Successes:     4,
			Failures:      1,
			MeanLatencyMs: 450,
			LastOutcomeAt: now,
		},
		Revision:  1,
		CreatedAt: now,
		UpdatedAt: now,
	}
}

func postgresTestAssignment(agent Agent) Assignment {
	return Assignment{
		ID:               "assignment-" + strings.ReplaceAll(agent.ID, "-", ""),
		OwnerIdentity:    agent.OwnerIdentity,
		TaskID:           "task-1",
		AgentID:          agent.ID,
		AgentRevision:    agent.Revision,
		GrantedAuthority: 3,
		GrantedAutonomy:  2,
		Score:            0.75,
		Explanation: AssignmentExplanation{
			Eligible: true,
			Components: []ScoreComponent{{
				Name: "capability", Score: 1, Reason: "required capability is present",
			}},
			Constraints: []string{"local runtime required"},
		},
		RequestDigest: fmt.Sprintf("%064x", 42),
		AssignedAt:    agent.UpdatedAt,
	}
}
