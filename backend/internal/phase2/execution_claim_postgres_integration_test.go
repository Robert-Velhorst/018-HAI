//go:build integration

package phase2

import (
	"context"
	"errors"
	"net"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"automation-hub-backend/internal/infra"
	"automation-hub-backend/internal/operations"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestGormExplicitOperationClaimsAreAtomicAndFencedPostgres(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("HAI_TEST_DATABASE_DSN"))
	if dsn == "" {
		t.Skip("HAI_TEST_DATABASE_DSN not set; skipping Postgres integration test")
	}
	if !strings.EqualFold(strings.TrimSpace(os.Getenv("HAI_ALLOW_DESTRUCTIVE_DATABASE_TESTS")), "true") {
		t.Skip("HAI_ALLOW_DESTRUCTIVE_DATABASE_TESTS=true is required")
	}
	config, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse Postgres test DSN: %v", err)
	}
	if !isLoopbackClaimTestHost(config.Host) {
		t.Fatalf("refusing claim integration test against non-loopback host %q", config.Host)
	}
	if !strings.Contains(strings.ToLower(config.Database), "test") {
		t.Fatalf("refusing claim integration test against database %q without 'test' in its name", config.Database)
	}

	t.Setenv("DB_AUTOMIGRATE", "false")
	t.Setenv("HAI_SEMANTIC_RETRIEVAL_ENABLED", "false")
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open Postgres test database: %v", err)
	}
	if err := infra.RunMigrations(db); err != nil {
		t.Fatalf("apply production migrations to Postgres test database: %v", err)
	}
	if sqlDB, err := db.DB(); err != nil {
		t.Fatalf("get Postgres pool: %v", err)
	} else {
		sqlDB.SetMaxOpenConns(8)
	}

	firstService := operations.NewService(operations.NewGormRepository(db))
	secondService := operations.NewService(operations.NewGormRepository(db.Session(&gorm.Session{})))
	created, err := firstService.Ingest(operations.NewOperationInput{
		OwnerUserID: "explicit-claim-test-owner", WorkspaceID: "local",
		Title: "explicit target claim", OperationType: "safe_worker_test",
		SourceType: "integration_test", DedupeKey: "explicit-claim-" + uuid.NewString(),
	})
	if err != nil {
		t.Fatalf("create operation for explicit claim: %v", err)
	}
	operationID := created.Operation.ID
	t.Cleanup(func() {
		if err := db.Exec("DELETE FROM public.operation_events WHERE operation_id = ?", operationID).Error; err != nil {
			t.Errorf("delete operation events: %v", err)
		}
		if err := db.Exec("DELETE FROM public.operation_execution_claims WHERE operation_id = ?", operationID).Error; err != nil {
			t.Errorf("delete execution claim: %v", err)
		}
		if err := db.Exec("DELETE FROM public.operations WHERE id = ?", operationID).Error; err != nil {
			t.Errorf("delete test operation: %v", err)
		}
	})
	op := created.Operation
	op.CurrentDecision = string(operations.DecisionRunSafeLocalWorker)
	op.RiskLevel = string(operations.RiskLow)
	op.AutonomyLevel = string(operations.AutonomyAuto)
	op.OwnerType = string(operations.OwnerHAI)
	classified, err := firstService.Transition(op, operations.StatusClassified, "hai", "", "safe execution classified")
	if err != nil {
		t.Fatalf("classify operation: %v", err)
	}

	start := make(chan struct{})
	results := make(chan struct {
		claim *operations.ClaimedOperation
		err   error
	}, 2)
	var runners sync.WaitGroup
	for _, attempt := range []struct {
		service *operations.Service
		worker  uuid.UUID
	}{{firstService, uuid.New()}, {secondService, uuid.New()}} {
		runners.Add(1)
		go func(service *operations.Service, worker uuid.UUID) {
			defer runners.Done()
			<-start
			claim, claimErr := service.ClaimOperation(
				context.Background(), "explicit-claim-test-owner", "local", operationID, worker, time.Minute,
			)
			results <- struct {
				claim *operations.ClaimedOperation
				err   error
			}{claim: claim, err: claimErr}
		}(attempt.service, attempt.worker)
	}
	close(start)
	runners.Wait()
	close(results)

	var winner *operations.ClaimedOperation
	claimCount := 0
	for result := range results {
		if result.err == nil && result.claim != nil {
			winner = result.claim
			claimCount++
			continue
		}
		if !errors.Is(result.err, operations.ErrOperationClaimed) {
			t.Fatalf("competing exact target claim = %v, want ErrOperationClaimed", result.err)
		}
	}
	if claimCount != 1 || winner == nil || winner.Operation.ID != operationID {
		t.Fatalf("Postgres exact-claim winners = %d, winner=%#v", claimCount, winner)
	}
	if _, err := secondService.ClaimOperation(
		context.Background(), "different-owner", "local", operationID, uuid.New(), time.Minute,
	); !errors.Is(err, operations.ErrNotFound) {
		t.Fatalf("cross-owner exact claim = %v, want ErrNotFound", err)
	}
	if err := firstService.ReleaseClaim(context.Background(), winner.Claim); err != nil {
		t.Fatalf("release winning claim: %v", err)
	}

	current, err := secondService.ClaimOperation(
		context.Background(), "explicit-claim-test-owner", "local", operationID, uuid.New(), time.Minute,
	)
	if err != nil || current == nil {
		t.Fatalf("reclaim released operation: claim=%#v err=%v", current, err)
	}
	if current.Claim.Generation <= winner.Claim.Generation {
		t.Fatalf("claim generation failed to advance: old=%d new=%d", winner.Claim.Generation, current.Claim.Generation)
	}
	if _, err := firstService.TransitionClaimed(
		context.Background(), winner.Claim, winner.Operation, operations.StatusReady, "hai", "", "stale worker",
	); !errors.Is(err, operations.ErrClaimLost) {
		t.Fatalf("stale exact-claim transition = %v, want ErrClaimLost", err)
	}
	if _, err := secondService.TransitionClaimed(
		context.Background(), current.Claim, current.Operation, operations.StatusReady, "hai", "", "current worker",
	); err != nil {
		t.Fatalf("current exact-claim transition: %v", err)
	}
	stored, err := secondService.Get("explicit-claim-test-owner", "local", operationID)
	if err != nil || stored.Status != string(operations.StatusReady) || stored.Version != classified.Version+1 {
		t.Fatalf("operation after stale/current transitions = %#v, %v", stored, err)
	}
	if err := secondService.ReleaseClaim(context.Background(), current.Claim); err != nil {
		t.Fatalf("release current claim: %v", err)
	}
}

func isLoopbackClaimTestHost(host string) bool {
	host = strings.Trim(strings.TrimSpace(host), "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
