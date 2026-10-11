package frameworkregistry

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"automation-hub-backend/internal/agentcoordination"
	"automation-hub-backend/internal/infra"
	"automation-hub-backend/migrations"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestPostgresAgentTeamRepositoryRequiresDatabase(t *testing.T) {
	t.Parallel()

	repository := NewPostgresAgentTeamRepository(nil)
	if _, err := repository.GetTeam("owner", uuid.NewString(), "1.0.0"); err == nil ||
		!strings.Contains(err.Error(), "database is required") {
		t.Fatalf("nil database error = %v", err)
	}
}

func TestPostgresAgentTeamDecoderRejectsMalformedAndTamperedJSON(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC().Truncate(time.Second)
	service := newAgentTeamService(NewMemoryAgentTeamRepository(), func() time.Time { return now }, deterministicTeamIDs("postgres-decoder"))
	team, err := service.CreateTeam("owner", testTeamRequest(now))
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(team)
	if err != nil {
		t.Fatal(err)
	}
	row := postgresAgentTeamRow{
		OwnerIdentity:         "owner",
		TeamID:                team.ID,
		TeamKey:               team.Key,
		TeamVersion:           team.Version,
		Revision:              int64(team.Revision),
		TeamStatus:            team.Status,
		ContractDigest:        team.ContractDigest,
		PreviousVersionDigest: team.PreviousVersionDigest,
		CreatedAt:             team.CreatedAt,
		UpdatedAt:             team.UpdatedAt,
		Payload:               string(payload),
	}
	if _, err := decodePostgresAgentTeamRow(row, "owner"); err != nil {
		t.Fatalf("valid row rejected: %v", err)
	}

	malformed := row
	malformed.Payload = `{"id":`
	if _, err := decodePostgresAgentTeamRow(malformed, "owner"); err == nil {
		t.Fatal("malformed JSON was accepted")
	}

	var object map[string]any
	if err := json.Unmarshal(payload, &object); err != nil {
		t.Fatal(err)
	}
	object["name"] = "tampered without recomputing the contract digest"
	tamperedPayload, err := json.Marshal(object)
	if err != nil {
		t.Fatal(err)
	}
	tampered := row
	tampered.Payload = string(tamperedPayload)
	if _, err := decodePostgresAgentTeamRow(tampered, "owner"); err == nil {
		t.Fatal("tampered team payload was accepted")
	}

	wrongOwner := row
	wrongOwner.OwnerIdentity = "other-owner"
	if _, err := decodePostgresAgentTeamRow(wrongOwner, "owner"); err == nil {
		t.Fatal("owner-mismatched team row was accepted")
	}
}

func TestPostgresAgentTeamRepositoryDurableLifecycleAndRaces(t *testing.T) {
	repository, db := openAgentTeamPostgresRepository(t)
	now := time.Now().UTC().Truncate(time.Second)
	owner := "agent-team-test-" + uuid.NewString()
	service := newAgentTeamService(repository, func() time.Time { return now }, uuid.NewString)
	team := createActiveTestTeam(t, service, owner, now)

	restarted := NewPostgresAgentTeamRepository(db)
	stored, err := restarted.GetTeam(owner, team.ID, team.Version)
	if err != nil || stored.ContractDigest != team.ContractDigest {
		t.Fatalf("durable team = %#v, err %v", stored, err)
	}
	if _, err := restarted.GetTeam("other-owner", team.ID, team.Version); !errors.Is(err, ErrAgentTeamNotFound) {
		t.Fatalf("cross-owner lookup error = %v", err)
	}
	events, err := restarted.ListTeamEvents(owner, team.ID, team.Version)
	if err != nil || len(events) != int(team.Revision) {
		t.Fatalf("events = %d, revision = %d, err %v", len(events), team.Revision, err)
	}
	for index := 1; index < len(events); index++ {
		if events[index].PreviousEventDigest != events[index-1].EventDigest {
			t.Fatalf("event %d is not hash-linked", index)
		}
	}

	raceCorrelation := uuid.NewString()
	raceMessage := decisionMessage(t, team, now, raceCorrelation, team.Members[0], team.Members[1], TeamVoteSupport, "race-safe plan", "postgres-race")
	raceMessage.RequiresAck = true
	raceMessage.PayloadDigest, err = agentcoordination.ComputeMessageDigest(raceMessage)
	if err != nil {
		t.Fatal(err)
	}
	raceMessage.PayloadDigest = strings.ToUpper(raceMessage.PayloadDigest)
	type appendResult struct {
		created bool
		err     error
	}
	results := make(chan appendResult, 2)
	var wait sync.WaitGroup
	for range 2 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, created, err := restarted.AppendCoordinationMessage(owner, team.ID, team.Version, raceMessage)
			results <- appendResult{created: created, err: err}
		}()
	}
	wait.Wait()
	close(results)
	createdCount := 0
	for result := range results {
		if result.err != nil {
			t.Fatalf("same-payload idempotency race: %v", result.err)
		}
		if result.created {
			createdCount++
		}
	}
	if createdCount != 1 {
		t.Fatalf("same-payload race created %d records, want 1", createdCount)
	}
	batchedMessages, err := restarted.ListCoordinationMessagesForTeams(owner, []AgentTeamContract{*team, *team})
	if err != nil {
		t.Fatalf("batch durable messages: %v", err)
	}
	storedMessages := batchedMessages[teamVersionKey(owner, team.ID, team.Version)]
	if len(batchedMessages) != 1 || len(storedMessages) != 1 || storedMessages[0].ID != raceMessage.ID {
		t.Fatalf("batched durable messages = %#v", batchedMessages)
	}

	retryAfter := now.Add(5 * time.Minute)
	deferredAck := agentcoordination.Acknowledgment{
		ID:             uuid.NewString(),
		MessageID:      raceMessage.ID,
		CorrelationID:  raceMessage.CorrelationID,
		RecipientID:    raceMessage.Recipient.ID,
		Status:         agentcoordination.AcknowledgmentDeferred,
		Reason:         "Waiting for the referenced evidence.",
		CreatedAt:      now.Add(time.Minute),
		RetryAfter:     &retryAfter,
		IdempotencyKey: uuid.NewString(),
	}
	if _, created, err := restarted.AppendMessageAcknowledgment(owner, team.ID, team.Version, deferredAck); err != nil || !created {
		t.Fatalf("persist deferred acknowledgment: created=%v err=%v", created, err)
	}
	if _, created, err := NewPostgresAgentTeamRepository(db).AppendMessageAcknowledgment(owner, team.ID, team.Version, deferredAck); err != nil || created {
		t.Fatalf("replay deferred acknowledgment: created=%v err=%v", created, err)
	}
	acceptedAck := agentcoordination.Acknowledgment{
		ID:             uuid.NewString(),
		MessageID:      raceMessage.ID,
		CorrelationID:  raceMessage.CorrelationID,
		RecipientID:    raceMessage.Recipient.ID,
		Status:         agentcoordination.AcknowledgmentAccepted,
		CreatedAt:      now.Add(2 * time.Minute),
		IdempotencyKey: uuid.NewString(),
	}
	if _, created, err := restarted.AppendMessageAcknowledgment(owner, team.ID, team.Version, acceptedAck); err != nil || !created {
		t.Fatalf("persist terminal acknowledgment: created=%v err=%v", created, err)
	}
	acknowledgments, err := NewPostgresAgentTeamRepository(db).ListMessageAcknowledgments(owner, team.ID, team.Version, raceMessage.ID)
	if err != nil || len(acknowledgments) != 2 || acknowledgments[1].Status != agentcoordination.AcknowledgmentAccepted {
		t.Fatalf("durable acknowledgments = %#v, err %v", acknowledgments, err)
	}
	batchedAcknowledgments, err := NewPostgresAgentTeamRepository(db).ListMessageAcknowledgmentsForMessages(
		owner,
		team.ID,
		team.Version,
		[]string{raceMessage.ID, "not-a-stored-message", raceMessage.ID, "  "},
	)
	if err != nil {
		t.Fatalf("batch durable acknowledgments: %v", err)
	}
	if len(batchedAcknowledgments) != 2 {
		t.Fatalf("batched acknowledgment keys = %#v, want race and missing messages", batchedAcknowledgments)
	}
	if got := batchedAcknowledgments[raceMessage.ID]; len(got) != 2 || got[1].Status != agentcoordination.AcknowledgmentAccepted {
		t.Fatalf("batched durable acknowledgments for race message = %#v", got)
	}
	if missing, exists := batchedAcknowledgments["not-a-stored-message"]; !exists || len(missing) != 0 {
		t.Fatalf("batched missing-message acknowledgment state = %#v, exists=%v", missing, exists)
	}
	if result := db.Exec(`UPDATE public.agent_team_message_acknowledgments SET status = 'rejected' WHERE owner_identity = ? AND acknowledgment_id = ?`, owner, acceptedAck.ID); result.Error == nil {
		t.Fatal("append-only acknowledgment row accepted an update")
	}

	conflicting := raceMessage
	conflicting.ID = uuid.NewString()
	conflicting.Payload.Data = json.RawMessage(`{"position":"oppose","recommendation":"different plan"}`)
	conflicting.PayloadDigest, err = agentcoordination.ComputeMessageDigest(conflicting)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := restarted.AppendCoordinationMessage(owner, team.ID, team.Version, conflicting); !errors.Is(err, ErrAgentTeamIdempotencyConflict) {
		t.Fatalf("conflicting idempotency replay error = %v", err)
	}

	correlationID := uuid.NewString()
	first := decisionMessage(t, team, now, correlationID, team.Members[0], team.Members[1], TeamVoteSupport, "bounded plan", "postgres-consensus-a")
	second := decisionMessage(t, team, now, correlationID, team.Members[1], team.Members[0], TeamVoteSupport, "bounded plan", "postgres-consensus-b")
	for _, message := range []agentcoordination.Message{first, second} {
		if _, created, err := service.StoreCoordinationMessage(owner, team.ID, team.Version, message); err != nil || !created {
			t.Fatalf("store consensus message: created=%v err=%v", created, err)
		}
	}
	outcome, created, err := service.RecordConsensus(owner, team.ID, team.Version, correlationID, uuid.NewString(), "Choose the bounded plan")
	if err != nil || !created {
		t.Fatalf("record consensus: created=%v outcome=%#v err=%v", created, outcome, err)
	}
	current, err := restarted.GetTeam(owner, team.ID, team.Version)
	if err != nil || current.Revision != team.Revision+1 {
		t.Fatalf("consensus revision = %d, err %v", current.Revision, err)
	}
	replayed, created, err := restarted.RecordConsensusOutcome(owner, *outcome, current, 0, TeamLifecycleEvent{})
	if err != nil || created || replayed.OutcomeDigest != outcome.OutcomeDigest {
		t.Fatalf("consensus replay: created=%v outcome=%#v err=%v", created, replayed, err)
	}

	conflictingOutcome := *outcome
	conflictingOutcome.Issue = "A conflicting replay"
	conflictingOutcome.OutcomeDigest, err = teamConsensusOutcomeDigest(conflictingOutcome)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := restarted.RecordConsensusOutcome(owner, conflictingOutcome, current, current.Revision, TeamLifecycleEvent{}); !errors.Is(err, ErrAgentTeamIdempotencyConflict) {
		t.Fatalf("consensus idempotency conflict error = %v", err)
	}

	staleOutcome := *outcome
	staleOutcome.ID = uuid.NewString()
	staleOutcome.IdempotencyKey = uuid.NewString()
	staleOutcome.Issue = "A stale competing outcome"
	staleOutcome.OutcomeDigest, err = teamConsensusOutcomeDigest(staleOutcome)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := restarted.RecordConsensusOutcome(owner, staleOutcome, current, team.Revision, TeamLifecycleEvent{}); !errors.Is(err, ErrAgentTeamRevisionConflict) {
		t.Fatalf("stale consensus revision error = %v", err)
	}

	outcomes, err := NewPostgresAgentTeamRepository(db).ListConsensusOutcomes(owner, team.ID, team.Version)
	if err != nil || len(outcomes) != 1 || outcomes[0].OutcomeDigest != outcome.OutcomeDigest {
		t.Fatalf("durable outcomes = %#v, err %v", outcomes, err)
	}
}

func openAgentTeamPostgresRepository(t *testing.T) (*PostgresAgentTeamRepository, *gorm.DB) {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("HAI_TEST_DATABASE_DSN"))
	if dsn == "" {
		t.Skip("HAI_TEST_DATABASE_DSN not set; skipping agent-team Postgres integration test")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open Postgres: %v", err)
	}
	if _, err := infra.ApplyMigrations(db, migrations.Files, "pre"); err != nil {
		t.Fatalf("apply pre migrations: %v", err)
	}
	return NewPostgresAgentTeamRepository(db), db
}
