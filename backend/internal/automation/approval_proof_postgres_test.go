package automation

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
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

func TestPostgresApprovalProofConsumptionSurvivesRestartAndIsAtomic(t *testing.T) {
	db := approvalProofPostgresDB(t)
	store := NewPostgresApprovalProofConsumptionStore(db)
	secret := []byte("0123456789abcdef0123456789abcdef")
	// Exercise the production clock's sub-microsecond precision. PostgreSQL
	// stores timestamps at microsecond precision, so proof digests must normalize
	// before they are signed and persisted.
	now := time.Now().UTC().Truncate(time.Microsecond).Add(789 * time.Nanosecond)
	issuer, err := NewApprovalProofService(secret, store, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	consumerAfterRestart, err := NewApprovalProofService(secret, NewPostgresApprovalProofConsumptionStore(db), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	request := ApprovalProofIssueRequest{
		OwnerIdentity:    "approval-owner-" + uuid.NewString(),
		AutomationID:     uuid.New(),
		ActionDigest:     approvalTestDigest("durable restart action"),
		Scope:            ApprovalScopeDocker,
		ApprovalSourceID: "task-review:" + uuid.NewString(),
	}
	proof, err := issuer.Issue(request)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	secondIssuer, err := NewApprovalProofService(secret, NewPostgresApprovalProofConsumptionStore(db), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := secondIssuer.Issue(request); !errors.Is(err, ErrApprovalProofAlreadyIssued) {
		t.Fatalf("second durable issuance error = %v, want ErrApprovalProofAlreadyIssued", err)
	}
	expected := approvalExpectation(request)
	if err := consumerAfterRestart.VerifyAndConsume(context.Background(), proof, expected); err != nil {
		t.Fatalf("restart consumer: %v", err)
	}
	apiReadRequest := request
	apiReadRequest.OwnerIdentity += "-api-read"
	apiReadRequest.AutomationID = uuid.New()
	apiReadRequest.ActionDigest = approvalTestDigest("durable API read approval")
	apiReadRequest.Scope = ApprovalScopeAPIRead
	apiReadRequest.ApprovalSourceID = "task-review:" + uuid.NewString()
	apiReadProof, err := issuer.Issue(apiReadRequest)
	if err != nil {
		t.Fatalf("issue API-read proof: %v", err)
	}
	if err := consumerAfterRestart.VerifyAndConsume(context.Background(), apiReadProof, approvalExpectation(apiReadRequest)); err != nil {
		t.Fatalf("consume API-read proof: %v", err)
	}
	secondRestart, err := NewApprovalProofService(secret, NewPostgresApprovalProofConsumptionStore(db), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	if err := secondRestart.VerifyAndConsume(context.Background(), proof, expected); !errors.Is(err, ErrApprovalProofConsumed) {
		t.Fatalf("restart replay error = %v, want ErrApprovalProofConsumed", err)
	}

	concurrentRequest := request
	concurrentRequest.AutomationID = uuid.New()
	concurrentRequest.ActionDigest = approvalTestDigest("durable concurrent action")
	concurrentRequest.ApprovalSourceID = "workflow-decision:" + uuid.NewString()
	concurrentProof, err := issuer.Issue(concurrentRequest)
	if err != nil {
		t.Fatal(err)
	}
	concurrentExpected := approvalExpectation(concurrentRequest)
	var successes atomic.Int32
	var replayBlocks atomic.Int32
	var unexpected atomic.Int32
	start := make(chan struct{})
	var wait sync.WaitGroup
	for range 24 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			service, createErr := NewApprovalProofService(secret, NewPostgresApprovalProofConsumptionStore(db), func() time.Time { return now })
			if createErr != nil {
				unexpected.Add(1)
				return
			}
			<-start
			err := service.VerifyAndConsume(context.Background(), concurrentProof, concurrentExpected)
			switch {
			case err == nil:
				successes.Add(1)
			case errors.Is(err, ErrApprovalProofConsumed):
				replayBlocks.Add(1)
			default:
				unexpected.Add(1)
			}
		}()
	}
	close(start)
	wait.Wait()
	if successes.Load() != 1 || replayBlocks.Load() != 23 || unexpected.Load() != 0 {
		t.Fatalf("concurrent results success=%d replay=%d unexpected=%d, want 1/23/0", successes.Load(), replayBlocks.Load(), unexpected.Load())
	}

	if err := db.Exec(`
		UPDATE public.automation_approval_proof_consumptions
		SET scope = 'automation.script.execute'
		WHERE owner_identity = ? AND proof_id = ?`,
		request.OwnerIdentity,
		proof.ID,
	).Error; err == nil {
		t.Fatal("database allowed approval proof consumption mutation")
	}
	if err := db.Exec(`
		DELETE FROM public.automation_approval_proof_consumptions
		WHERE owner_identity = ? AND proof_id = ?`,
		request.OwnerIdentity,
		proof.ID,
	).Error; err == nil {
		t.Fatal("database allowed approval proof consumption deletion")
	}
	if err := db.Exec(`
		UPDATE public.automation_approval_proof_issuances
		SET scope = 'automation.script.execute'
		WHERE owner_identity = ? AND approval_source_id = ?`,
		request.OwnerIdentity,
		request.ApprovalSourceID,
	).Error; err == nil {
		t.Fatal("database allowed approval proof issuance mutation")
	}
	if err := db.Exec(`
		DELETE FROM public.automation_approval_proof_issuances
		WHERE owner_identity = ? AND approval_source_id = ?`,
		request.OwnerIdentity,
		request.ApprovalSourceID,
	).Error; err == nil {
		t.Fatal("database allowed approval proof issuance deletion")
	}
}

func TestPostgresApprovalProofStoreRejectsMalformedRecordsBeforeSQL(t *testing.T) {
	store := NewPostgresApprovalProofConsumptionStore(&gorm.DB{})
	if err := store.Consume(context.Background(), ApprovalProofConsumption{}); err == nil {
		t.Fatal("malformed approval proof consumption was accepted")
	}
	if err := (*PostgresApprovalProofConsumptionStore)(nil).Consume(context.Background(), ApprovalProofConsumption{}); err == nil {
		t.Fatal("nil approval proof store was accepted")
	}
}

func approvalExpectation(request ApprovalProofIssueRequest) ApprovalProofExpectation {
	return ApprovalProofExpectation{
		OwnerIdentity:    request.OwnerIdentity,
		AutomationID:     request.AutomationID,
		ActionDigest:     request.ActionDigest,
		Scope:            request.Scope,
		ApprovalSourceID: request.ApprovalSourceID,
	}
}

func approvalProofPostgresDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := pgtestguard.RequireDedicatedPostgresTestDSN(t, "HAI_TEST_DATABASE_DSN", "hai_migration_runner_test")
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open Postgres: %v", err)
	}
	if _, err := infra.ApplyMigrations(db, migrations.Files, "pre"); err != nil {
		t.Fatalf("apply pre migrations: %v", err)
	}
	return db
}
