package executionauth

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"automation-hub-backend/internal/lifeontology"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type transactionAwareTestApprovalResolver struct {
	approval         ResolvedApproval
	plainCalls       atomic.Int32
	transactionCalls atomic.Int32
}

func (r *transactionAwareTestApprovalResolver) Resolve(
	_ context.Context,
	_ string,
	_ string,
	_ string,
) (ResolvedApproval, error) {
	r.plainCalls.Add(1)
	return r.approval, nil
}

func (r *transactionAwareTestApprovalResolver) ResolveInPostgresTransaction(
	_ context.Context,
	tx *gorm.DB,
	ownerIdentity string,
	sourceID string,
	bindingDigest string,
) (ResolvedApproval, error) {
	if tx == nil || tx.Statement == nil {
		return ResolvedApproval{}, ErrPostgresTransactionRequired
	}
	if ownerIdentity != r.approval.ApprovedBy ||
		sourceID != r.approval.SourceID ||
		bindingDigest != r.approval.BindingDigest {
		return ResolvedApproval{}, ErrAuthorizationChanged
	}
	r.transactionCalls.Add(1)
	return r.approval, nil
}

func TestAuthorizeAndConsumeInTransactionRejectsNonPostgresRepository(t *testing.T) {
	service := newTestService(t, NewMemoryRepository(), permissiveConstitution(), nil, nil)
	_, projection, err := service.AuthorizeAndConsumeInTransaction(
		context.Background(), nil, baseRequest("transactional-rejection"), "test", "target",
	)
	if !errors.Is(err, ErrPostgresTransactionRequired) {
		t.Fatalf("error = %v, want ErrPostgresTransactionRequired", err)
	}
	if projection != nil {
		t.Fatal("rejected request returned a post-commit projection")
	}
}

func TestAuthorizeAndConsumeFailsClosedForTaskReviewWithoutPostgres(t *testing.T) {
	repository := NewMemoryRepository()
	service := newTestService(t, repository, permissiveConstitution(), nil, nil)
	request := baseRequest("task-review-non-postgres")
	request.ApprovalSourceID = "task-review:" + uuid.NewString()
	request.ApprovalBindingDigest = postgresDigest("binding")

	if _, err := service.AuthorizeAndConsume(
		context.Background(), request, "test-consumer", "test-target",
	); !errors.Is(err, ErrPostgresTransactionRequired) {
		t.Fatalf("task-review execution error = %v, want ErrPostgresTransactionRequired", err)
	}
	receipts, err := repository.List(context.Background(), request.OwnerIdentity, 10)
	if err != nil || len(receipts) != 0 {
		t.Fatalf("non-transactional path persisted receipts=%d err=%v, want none", len(receipts), err)
	}
}

func TestPostgresAuthorizeAndConsumeInCallerTransaction(t *testing.T) {
	repository, db := executionAuthorizationPostgresRepository(t)
	owner := "executionauth-transaction-" + uuid.NewString()
	t.Cleanup(func() {
		_ = db.Exec("DELETE FROM public.execution_authorization_consumptions WHERE owner_identity = ?", owner).Error
		_ = db.Exec("DELETE FROM public.execution_authorization_receipts WHERE owner_identity = ?", owner).Error
	})

	service := newTestService(t, repository, permissiveConstitution(), nil, nil)
	var projectionCalls atomic.Int32
	if _, err := service.WithLifeOntologyProjection(authorizationProjectionFunc(func(
		_ context.Context,
		_ lifeontology.OperationalProjectionRequest,
	) (lifeontology.OperationalProjectionResult, error) {
		projectionCalls.Add(1)
		return lifeontology.OperationalProjectionResult{AdvisoryOnly: true}, nil
	})); err != nil {
		t.Fatal(err)
	}

	request := baseRequest("transactional-" + uuid.NewString())
	request.OwnerIdentity = owner
	request.ActorIdentity = owner
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatalf("begin transaction: %v", tx.Error)
	}
	receipt, project, err := service.AuthorizeAndConsumeInTransaction(
		context.Background(), tx, request, "test-consumer", "workspace:"+request.ResourceID,
	)
	if err != nil {
		_ = tx.Rollback().Error
		t.Fatalf("AuthorizeAndConsumeInTransaction: %v", err)
	}
	if receipt.Outcome != OutcomeAuthorized || receipt.LifeGraphProjection != nil || project == nil {
		t.Fatalf("receipt=%#v projection=%v; want authorized receipt with deferred projection", receipt, project != nil)
	}
	if projectionCalls.Load() != 0 {
		t.Fatal("life-graph projection ran before transaction commit")
	}
	if _, err := repository.Get(context.Background(), owner, receipt.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("uncommitted receipt visible through root repository: err=%v", err)
	}
	if _, err := repository.GetConsumption(context.Background(), owner, receipt.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("uncommitted consumption visible through root repository: err=%v", err)
	}
	if err := tx.Commit().Error; err != nil {
		t.Fatalf("commit transaction: %v", err)
	}

	stored, err := repository.Get(context.Background(), owner, receipt.ID)
	if err != nil || stored.DecisionDigest != receipt.DecisionDigest {
		t.Fatalf("committed receipt=%#v err=%v", stored, err)
	}
	consumption, err := repository.GetConsumption(context.Background(), owner, receipt.ID)
	if err != nil || consumption.ExecutionTarget != "workspace:"+request.ResourceID {
		t.Fatalf("committed consumption=%#v err=%v", consumption, err)
	}
	projected := project(context.Background())
	if projected.LifeGraphProjection == nil || projectionCalls.Load() != 1 {
		t.Fatalf("post-commit projection=%#v calls=%d", projected.LifeGraphProjection, projectionCalls.Load())
	}

	rollbackRequest := baseRequest("transactional-rollback-" + uuid.NewString())
	rollbackRequest.OwnerIdentity = owner
	rollbackRequest.ActorIdentity = owner
	rollbackTx := db.Begin()
	if rollbackTx.Error != nil {
		t.Fatalf("begin rollback transaction: %v", rollbackTx.Error)
	}
	rolledBackReceipt, rollbackProjection, err := service.AuthorizeAndConsumeInTransaction(
		context.Background(), rollbackTx, rollbackRequest, "test-consumer", "workspace:"+rollbackRequest.ResourceID,
	)
	if err != nil {
		_ = rollbackTx.Rollback().Error
		t.Fatalf("AuthorizeAndConsumeInTransaction before rollback: %v", err)
	}
	if rollbackProjection == nil {
		_ = rollbackTx.Rollback().Error
		t.Fatal("rollback transaction did not return its deferred projection")
	}
	if err := rollbackTx.Rollback().Error; err != nil {
		t.Fatalf("rollback authorization transaction: %v", err)
	}
	if _, err := repository.Get(context.Background(), owner, rolledBackReceipt.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("rolled-back receipt lookup error=%v, want ErrNotFound", err)
	}
	if _, err := repository.GetConsumption(context.Background(), owner, rolledBackReceipt.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("rolled-back consumption lookup error=%v, want ErrNotFound", err)
	}
	if projectionCalls.Load() != 1 {
		t.Fatalf("post-commit projection ran for rolled-back transaction; calls=%d", projectionCalls.Load())
	}
}

func TestPostgresAuthorizeAndConsumeClaimsTaskReviewApprovalInTransaction(t *testing.T) {
	repository, db := executionAuthorizationPostgresRepository(t)
	ctx := context.Background()
	owner := "executionauth-task-review-" + uuid.NewString()
	decisionID, sourceID := createTaskReviewDecisionFixture(t, db, owner, fixedNow().Add(-time.Minute))
	decisionDigest := postgresDigest("task-review-approval-" + decisionID.String())
	bindingDigest := postgresDigest("task-review-" + strings.TrimPrefix(sourceID, "task-review:"))
	resolver := &transactionAwareTestApprovalResolver{approval: ResolvedApproval{
		SourceID: sourceID, DecisionID: decisionID.String(),
		DecisionDigest: decisionDigest, BindingDigest: bindingDigest,
		ApprovedBy: owner, ApprovedAt: fixedNow().Add(-time.Minute),
		ExpiresAt: fixedNow().Add(10 * time.Minute),
	}}
	service := newTestService(t, repository, permissiveConstitution(), resolver, nil)
	request := baseRequest("task-review-transaction-" + uuid.NewString())
	request.OwnerIdentity = owner
	request.ActorIdentity = owner
	request.ApprovalSourceID = sourceID
	request.ApprovalBindingDigest = bindingDigest

	first, err := service.AuthorizeAndConsume(ctx, request, "task-worker", "workspace:"+request.ResourceID)
	if err != nil || first.Outcome != OutcomeAuthorized {
		t.Fatalf("first task-review execution = (%#v, %v), want authorized", first, err)
	}
	if resolver.plainCalls.Load() != 0 || resolver.transactionCalls.Load() != 2 {
		t.Fatalf("approval resolution used plain=%d transactional=%d, want 0 and 2", resolver.plainCalls.Load(), resolver.transactionCalls.Load())
	}
	if _, err := repository.GetConsumption(ctx, owner, first.ID); err != nil {
		t.Fatalf("first approval consumption was not committed: %v", err)
	}

	request.IdempotencyKey += "-second-attempt"
	second, err := service.AuthorizeAndConsume(ctx, request, "task-worker", "workspace:"+request.ResourceID)
	if !errors.Is(err, ErrApprovalAlreadyClaimed) {
		t.Fatalf("second task-review execution error = %v, want ErrApprovalAlreadyClaimed", err)
	}
	if resolver.plainCalls.Load() != 0 || resolver.transactionCalls.Load() != 4 {
		t.Fatalf("second approval resolution used plain=%d transactional=%d, want 0 and 4", resolver.plainCalls.Load(), resolver.transactionCalls.Load())
	}
	if _, err := repository.GetConsumption(ctx, owner, first.ID); err != nil {
		t.Fatalf("first approval consumption was lost after rejected retry: %v", err)
	}
	if second.ID != uuid.Nil {
		if _, err := repository.Get(ctx, owner, second.ID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("failed second attempt left a receipt outside its rolled-back transaction: %v", err)
		}
	}
}
