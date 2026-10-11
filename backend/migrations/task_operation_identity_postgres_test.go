//go:build integration

package migrations_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"automation-hub-backend/internal/infra"
	"automation-hub-backend/internal/task"
	"automation-hub-backend/migrations"
	"gorm.io/gorm"
)

func TestTaskOperationIdentityConcurrencyAndFencingInPostgres(t *testing.T) {
	db := openIsolatedMigrationDatabase(t)
	files := migrationFilesThrough(t, "pre/0037_task_operation_identity")
	if _, err := infra.ApplyMigrations(db, files, "pre"); err != nil {
		t.Fatalf("apply task operation identity migration: %v", err)
	}
	repository := task.NewPostgresTaskStateRepository(db)
	now := time.Now().UTC().Truncate(time.Microsecond)
	digest := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

	start := make(chan struct{})
	claims := make(chan task.TaskOperationClaim, 2)
	errorsFound := make(chan error, 2)
	var workers sync.WaitGroup
	for _, worker := range []string{"worker:one", "worker:two"} {
		workers.Add(1)
		go func(worker string) {
			defer workers.Done()
			<-start
			claim, err := repository.ClaimTaskOperation("alice", "event:postgres-1", digest, "run", worker, now, time.Minute)
			if err != nil {
				errorsFound <- err
				return
			}
			claims <- claim
		}(worker)
	}
	close(start)
	workers.Wait()
	close(claims)
	close(errorsFound)
	for err := range errorsFound {
		t.Fatalf("concurrent claim: %v", err)
	}
	counts := map[string]int{}
	var acquired task.TaskOperationClaim
	for claim := range claims {
		counts[claim.Disposition]++
		if claim.Disposition == task.TaskOperationAcquired {
			acquired = claim
		}
	}
	if counts[task.TaskOperationAcquired] != 1 || counts[task.TaskOperationInProgress] != 1 {
		t.Fatalf("claim dispositions = %#v", counts)
	}
	if _, err := repository.ClaimTaskOperation("alice", "event:postgres-1", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", "run", "worker:three", now, time.Minute); !errors.Is(err, task.ErrTaskStateConflict) {
		t.Fatalf("changed request error = %v, want conflict", err)
	}

	completed, err := repository.CompleteTaskOperation(
		"alice", acquired.Operation.ID, acquired.Operation.LeaseOwner,
		acquired.Operation.LeaseGeneration, "plan-postgres-1", now.Add(time.Second),
	)
	if err != nil || !completed {
		t.Fatalf("complete operation = (%v, %v)", completed, err)
	}
	replay, err := repository.ClaimTaskOperation("alice", "event:postgres-1", digest, "run", "worker:four", now.Add(2*time.Second), time.Minute)
	if err != nil || replay.Disposition != task.TaskOperationReplay || replay.Operation.TaskPlanID != "plan-postgres-1" {
		t.Fatalf("replay = (%#v, %v)", replay, err)
	}

	assertTaskOperationMutationRejected(t, db, "UPDATE task_operations SET request_digest = repeat('c', 64) WHERE id = ?", acquired.Operation.ID)
	assertTaskOperationMutationRejected(t, db, "UPDATE task_operations SET status = 'needs_review', task_plan_id = '', completed_at = NULL, last_error = 'changed' WHERE id = ?", acquired.Operation.ID)
	assertTaskOperationMutationRejected(t, db, "DELETE FROM task_operations WHERE id = ?", acquired.Operation.ID)
	assertTaskOperationMutationRejected(t, db, "TRUNCATE task_operations")
	if err := infra.RollbackMigration(db, migrations.Files, "pre", "pre/0037_task_operation_identity"); err == nil {
		t.Fatal("rollback discarded non-empty task operation audit state")
	}
}

func TestTaskOperationCancellationIsTerminalAndRollbackSafeInPostgres(t *testing.T) {
	db := openIsolatedMigrationDatabase(t)
	files := migrationFilesThrough(t, "pre/0067_task_operation_cancellation")
	if _, err := infra.ApplyMigrations(db, files, "pre"); err != nil {
		t.Fatalf("apply task operation cancellation migration: %v", err)
	}
	repository := task.NewPostgresTaskStateRepository(db)
	now := time.Now().UTC().Truncate(time.Microsecond)
	digest := "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
	claim, err := repository.ClaimTaskOperation(
		"alice", "event:postgres-canceled", digest, "plan", "worker:one", now, time.Minute,
	)
	if err != nil || claim.Disposition != task.TaskOperationAcquired {
		t.Fatalf("claim cancellable operation = (%#v, %v)", claim, err)
	}
	canceled, err := repository.CancelTaskOperation(
		"alice",
		claim.Operation.ID,
		claim.Operation.LeaseOwner,
		claim.Operation.LeaseGeneration,
		"caller canceled before task execution began",
		now.Add(time.Second),
	)
	if err != nil || !canceled {
		t.Fatalf("cancel operation = (%v, %v)", canceled, err)
	}
	replay, err := repository.ClaimTaskOperation(
		"alice", "event:postgres-canceled", digest, "plan", "worker:two", now.Add(2*time.Second), time.Minute,
	)
	if err != nil || replay.Disposition != task.TaskOperationCanceled || replay.Operation.Status != "canceled" {
		t.Fatalf("canceled replay = (%#v, %v)", replay, err)
	}
	assertTaskOperationMutationRejected(
		t,
		db,
		"UPDATE task_operations SET status = 'needs_review', last_error = 'rewritten' WHERE id = ?",
		claim.Operation.ID,
	)
	if err := infra.RollbackMigration(
		db,
		migrations.Files,
		"pre",
		"pre/0067_task_operation_cancellation",
	); err == nil || !strings.Contains(err.Error(), "canceled audit state") {
		t.Fatalf("rollback cancellation migration error = %v, want audit-state refusal", err)
	}
}

func TestTaskOperationClaimHonorsContextWhileWaitingForPostgresLock(t *testing.T) {
	db := openIsolatedMigrationDatabase(t)
	files := migrationFilesThrough(t, "pre/0067_task_operation_cancellation")
	if _, err := infra.ApplyMigrations(db, files, "pre"); err != nil {
		t.Fatalf("apply task operation cancellation migration: %v", err)
	}
	owner := "alice"
	key := "event:postgres-context"
	lockDigest := fmt.Sprintf("%x", sha256.Sum256([]byte(owner+"\x00"+key)))
	lockTx := db.Begin()
	if lockTx.Error != nil {
		t.Fatal(lockTx.Error)
	}
	t.Cleanup(func() { _ = lockTx.Rollback().Error })
	if err := lockTx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", lockDigest).Error; err != nil {
		t.Fatalf("hold task operation advisory lock: %v", err)
	}

	repository := task.NewPostgresTaskStateRepository(db)
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	_, err := repository.ClaimTaskOperationContext(
		ctx,
		owner,
		key,
		"eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
		"plan",
		"worker:context",
		time.Now().UTC(),
		time.Minute,
	)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("blocked PostgreSQL claim error = %v, want deadline exceeded", err)
	}
	if err := lockTx.Rollback().Error; err != nil {
		t.Fatalf("release task operation advisory lock: %v", err)
	}
	var count int64
	if err := db.Table("task_operations").Where("owner_identity = ? AND idempotency_key = ?", owner, key).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("canceled claim persisted %d task operation rows", count)
	}
}

func assertTaskOperationMutationRejected(t *testing.T, db *gorm.DB, query string, args ...interface{}) {
	t.Helper()
	err := db.Exec(query, args...).Error
	if err == nil || (!strings.Contains(err.Error(), "task operation") && !strings.Contains(err.Error(), "terminal task operation")) {
		t.Fatalf("task operation mutation error = %v, want durable-state rejection", err)
	}
}
