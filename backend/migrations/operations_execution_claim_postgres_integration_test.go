package migrations_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"automation-hub-backend/internal/infra"
	"automation-hub-backend/internal/operations"
	"automation-hub-backend/internal/opscontrol"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestOperationExecutionClaimsFenceReplicasAndRecoverOnlyExpiredLeases(t *testing.T) {
	t.Setenv("DB_AUTOMIGRATE", "false")
	t.Setenv("HAI_SEMANTIC_RETRIEVAL_ENABLED", "false")
	db := openIsolatedMigrationDatabase(t)
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get isolated Postgres pool: %v", err)
	}
	sqlDB.SetMaxOpenConns(8)
	if err := infra.RunMigrations(db); err != nil {
		t.Fatalf("apply production migrations to isolated Postgres: %v", err)
	}

	firstRepository := operations.NewGormRepository(db)
	secondRepository := operations.NewGormRepository(db.Session(&gorm.Session{}))
	firstService := operations.NewService(firstRepository)
	secondService := operations.NewService(secondRepository)
	created, err := firstService.Ingest(operations.NewOperationInput{
		OwnerUserID: "claim-test-owner", WorkspaceID: "local", Title: "claim race",
		OperationType: "internal", SourceType: "manual", DedupeKey: "claim-race-" + uuid.NewString(),
	})
	if err != nil {
		t.Fatalf("create operation for independent repositories: %v", err)
	}

	start := make(chan struct{})
	results := make(chan claimAttempt, 2)
	var runners sync.WaitGroup
	for _, attempt := range []struct {
		service *operations.Service
		worker  uuid.UUID
	}{
		{service: firstService, worker: uuid.New()},
		{service: secondService, worker: uuid.New()},
	} {
		runners.Add(1)
		go func(service *operations.Service, worker uuid.UUID) {
			defer runners.Done()
			<-start
			claim, claimErr := service.ClaimNext(context.Background(), "claim-test-owner", "local", worker, time.Minute)
			results <- claimAttempt{claim: claim, err: claimErr}
		}(attempt.service, attempt.worker)
	}
	close(start)
	runners.Wait()
	close(results)

	var winner *operations.ClaimedOperation
	claims := 0
	for result := range results {
		if result.err != nil {
			t.Fatalf("concurrent claim: %v", result.err)
		}
		if result.claim != nil {
			claims++
			winner = result.claim
		}
	}
	if claims != 1 || winner == nil || winner.Operation.ID != created.Operation.ID {
		t.Fatalf("independent repositories acquired %d claims; winner=%#v", claims, winner)
	}
	if _, err := secondService.Transition(winner.Operation, operations.StatusClassified, "legacy", "", "unfenced write"); !errors.Is(err, operations.ErrOperationClaimed) {
		t.Fatalf("unfenced transition during live claim = %v, want ErrOperationClaimed", err)
	}
	if duplicate, err := secondService.Ingest(operations.NewOperationInput{
		OwnerUserID: "claim-test-owner", WorkspaceID: "local", Title: "claim race",
		OperationType: "internal", SourceType: "manual", DedupeKey: created.Operation.DedupeKey,
	}); err != nil || duplicate.Created || duplicate.Operation.Version != winner.Operation.Version {
		t.Fatalf("unchanged duplicate ingestion while claimed = %#v, %v; want no-op without error", duplicate, err)
	}
	if _, err := secondService.Ingest(operations.NewOperationInput{
		OwnerUserID: "claim-test-owner", WorkspaceID: "local", Title: "claim race",
		OperationType: "internal", SourceType: "manual", DedupeKey: created.Operation.DedupeKey,
		EvidenceJSON: `{"revision":"changed"}`,
	}); !errors.Is(err, operations.ErrOperationClaimed) {
		t.Fatalf("changed evidence ingestion during live claim = %v, want ErrOperationClaimed", err)
	}

	if err := db.Exec(`UPDATE public.operation_execution_claims SET lease_expires_at = clock_timestamp() - INTERVAL '1 second' WHERE operation_id = ?`, created.Operation.ID).Error; err != nil {
		t.Fatalf("expire first claim for fencing test: %v", err)
	}
	newGeneration, err := secondService.ClaimNext(context.Background(), "claim-test-owner", "local", uuid.New(), time.Minute)
	if err != nil || newGeneration == nil {
		t.Fatalf("claim expired pending work: claim=%#v err=%v", newGeneration, err)
	}
	if newGeneration.Claim.Generation <= winner.Claim.Generation {
		t.Fatalf("claim generation advanced from %d to %d, want a larger generation", winner.Claim.Generation, newGeneration.Claim.Generation)
	}
	if _, err := firstService.TransitionClaimed(context.Background(), winner.Claim, winner.Operation, operations.StatusClassified, "hai", "", "stale worker"); !errors.Is(err, operations.ErrClaimLost) {
		t.Fatalf("stale generation finalization error = %v, want ErrClaimLost", err)
	}
	if _, err := secondService.TransitionClaimed(context.Background(), newGeneration.Claim, newGeneration.Operation, operations.StatusClassified, "hai", "", "current worker"); err != nil {
		t.Fatalf("current generation finalization: %v", err)
	}
	stored, err := firstService.Get("claim-test-owner", "local", created.Operation.ID)
	if err != nil || stored.Status != string(operations.StatusClassified) {
		t.Fatalf("operation after stale/current finalization = %#v, %v", stored, err)
	}

	live := createExecutingOperation(t, firstService, "live-running", operations.StatusRunning, time.Minute)
	expiredRunning := createExecutingOperation(t, firstService, "expired-running", operations.StatusRunning, time.Minute)
	expired := createExecutingOperation(t, firstService, "expired-verifying", operations.StatusVerifying, time.Minute)
	legacy, err := firstService.Ingest(operations.NewOperationInput{
		OwnerUserID: "claim-test-owner", WorkspaceID: "local", Title: "legacy unleased",
		OperationType: "internal", SourceType: "manual", DedupeKey: "legacy-" + uuid.NewString(),
	})
	if err != nil {
		t.Fatalf("create legacy operation: %v", err)
	}
	legacy.Operation.CurrentDecision = string(operations.DecisionRunSafeLocalWorker)
	classified, err := firstService.Transition(legacy.Operation, operations.StatusClassified, "hai", "", "legacy classified")
	if err != nil {
		t.Fatalf("classify legacy operation: %v", err)
	}
	ready, err := firstService.Transition(*classified, operations.StatusReady, "hai", "", "legacy ready")
	if err != nil {
		t.Fatalf("ready legacy operation: %v", err)
	}
	if _, err := firstService.Transition(*ready, operations.StatusRunning, "hai", "", "legacy running without claim"); err != nil {
		t.Fatalf("create unleased legacy running state: %v", err)
	}
	if err := db.Exec(`UPDATE public.operation_execution_claims SET lease_expires_at = clock_timestamp() - INTERVAL '1 second' WHERE operation_id = ?`, expired.Operation.ID).Error; err != nil {
		t.Fatalf("expire verifying claim: %v", err)
	}
	if err := db.Exec(`UPDATE public.operation_execution_claims SET lease_expires_at = clock_timestamp() - INTERVAL '1 second' WHERE operation_id = ?`, expiredRunning.Operation.ID).Error; err != nil {
		t.Fatalf("expire running claim: %v", err)
	}

	report, err := opscontrol.Recover(context.Background(), firstService, "claim-test-owner", "local", time.Now().UTC())
	if err != nil {
		t.Fatalf("recover expired execution claims: %v", err)
	}
	if report.Recovered != 2 || report.LiveRunning != 1 || report.UnleasedRunning != 1 {
		t.Fatalf("recovery report = %+v, want two recovered, one live, and one unleased operation", report)
	}
	liveStored, err := firstService.Get("claim-test-owner", "local", live.Operation.ID)
	if err != nil || liveStored.Status != string(operations.StatusRunning) {
		t.Fatalf("live operation was interrupted: %#v, %v", liveStored, err)
	}
	expiredRunningStored, err := firstService.Get("claim-test-owner", "local", expiredRunning.Operation.ID)
	if err != nil || expiredRunningStored.Status != string(operations.StatusInterrupted) {
		t.Fatalf("expired running operation recovery = %#v, %v", expiredRunningStored, err)
	}
	expiredStored, err := firstService.Get("claim-test-owner", "local", expired.Operation.ID)
	if err != nil || expiredStored.Status != string(operations.StatusAwaitingApproval) {
		t.Fatalf("expired verifying operation recovery = %#v, %v", expiredStored, err)
	}
	legacyStored, err := firstService.Get("claim-test-owner", "local", legacy.Operation.ID)
	if err != nil || legacyStored.Status != string(operations.StatusRunning) {
		t.Fatalf("unleased legacy operation was modified: %#v, %v", legacyStored, err)
	}
	if next, err := firstService.ClaimNext(context.Background(), "claim-test-owner", "local", uuid.New(), time.Minute); err != nil || next != nil {
		t.Fatalf("recovery made uncertain side effects executable again: claim=%#v err=%v", next, err)
	}

	if err := db.Exec(`DROP TABLE public.operation_execution_claims`).Error; err != nil {
		t.Fatalf("remove claim table to inject repository failure: %v", err)
	}
	if _, err := opscontrol.Recover(context.Background(), firstService, "claim-test-owner", "local", time.Now().UTC()); err == nil {
		t.Fatal("recovery returned an empty success report after the claim query failed")
	}
}

type claimAttempt struct {
	claim *operations.ClaimedOperation
	err   error
}

func createExecutingOperation(t *testing.T, service *operations.Service, key string, target operations.OperationStatus, lease time.Duration) *operations.ClaimedOperation {
	t.Helper()
	created, err := service.Ingest(operations.NewOperationInput{
		OwnerUserID: "claim-test-owner", WorkspaceID: "local", Title: key,
		OperationType: "internal", SourceType: "manual", DedupeKey: key + "-" + uuid.NewString(),
	})
	if err != nil {
		t.Fatalf("create %s operation: %v", key, err)
	}
	created.Operation.CurrentDecision = string(operations.DecisionRunSafeLocalWorker)
	created.Operation.RiskLevel = string(operations.RiskLow)
	created.Operation.AutonomyLevel = string(operations.AutonomyAuto)
	created.Operation.OwnerType = string(operations.OwnerHAI)
	created.Operation.VerificationStatus = string(operations.VerificationPending)
	claimed, err := service.ClaimNext(context.Background(), "claim-test-owner", "local", uuid.New(), lease)
	if err != nil || claimed == nil || claimed.Operation.ID != created.Operation.ID {
		t.Fatalf("claim %s: claim=%#v err=%v", key, claimed, err)
	}
	op := claimed.Operation
	op.CurrentDecision = created.Operation.CurrentDecision
	op.RiskLevel = created.Operation.RiskLevel
	op.AutonomyLevel = created.Operation.AutonomyLevel
	op.OwnerType = created.Operation.OwnerType
	op.VerificationStatus = created.Operation.VerificationStatus
	updated, err := service.TransitionClaimed(context.Background(), claimed.Claim, op, operations.StatusClassified, "hai", "", "classified")
	if err != nil {
		t.Fatalf("classify %s: %v", key, err)
	}
	updated, err = service.TransitionClaimed(context.Background(), claimed.Claim, *updated, operations.StatusReady, "hai", "", "ready")
	if err != nil {
		t.Fatalf("ready %s: %v", key, err)
	}
	updated, err = service.TransitionClaimed(context.Background(), claimed.Claim, *updated, operations.StatusRunning, "hai", "", "running")
	if err != nil {
		t.Fatalf("run %s: %v", key, err)
	}
	if target == operations.StatusVerifying {
		updated, err = service.TransitionClaimed(context.Background(), claimed.Claim, *updated, operations.StatusVerifying, "hai", "", "verifying")
		if err != nil {
			t.Fatalf("verify %s: %v", key, err)
		}
	}
	claimed.Operation = *updated
	return claimed
}
