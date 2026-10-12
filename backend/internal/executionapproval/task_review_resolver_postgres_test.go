package executionapproval

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/infra"
	"automation-hub-backend/internal/task"
	"automation-hub-backend/migrations"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestTaskReviewResolverLocksApprovedItemInExecutionTransaction(t *testing.T) {
	db := taskReviewResolverPostgresDatabase(t)
	ctx := context.Background()
	owner := "task-review-lock-" + uuid.NewString() + "@example.com"
	itemID := uuid.New()
	decisionID := uuid.New()
	taskPlanID := "plan-" + itemID.String()
	sourceID := "task-review:" + itemID.String()
	request := task.IntakeRequest{OwnerIdentity: owner, Request: "Run the transaction-lock test"}
	requestDigest, err := task.ReviewRequestDigest(owner, request)
	if err != nil {
		t.Fatalf("digest task review request: %v", err)
	}
	requestJSON := `{"request":"Run the transaction-lock test"}`
	resolvedAt := time.Now().UTC().Truncate(time.Microsecond).Add(-time.Minute)
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`
			INSERT INTO public.task_review_items (
				id, owner_identity, original_task_plan_id,
				current_task_plan_id, request_digest, request_json,
				reason, priority, status, review_revision,
				created_at, updated_at
			) VALUES (?, ?, ?, ?, ?, ?::jsonb, ?, 'normal', 'needs_review', 1, ?, ?)`,
			itemID, owner, taskPlanID, taskPlanID, requestDigest, requestJSON, "approval lock test",
			resolvedAt, resolvedAt,
		).Error; err != nil {
			return err
		}
		if err := tx.Exec(`
			INSERT INTO public.task_review_decisions (
				id, review_item_id, review_revision, owner_identity,
				task_plan_id, decision, resolution_note, resolved_by,
				approval_source, approval_source_id, request_digest, resolved_at
			) VALUES (?, ?, 1, ?, ?, 'approved', ?, ?, 'task-review', ?, ?, ?)`,
			decisionID, itemID, owner, taskPlanID, "approved for transaction-lock test",
			owner, sourceID, requestDigest, resolvedAt,
		).Error; err != nil {
			return err
		}
		return tx.Exec(`
			UPDATE public.task_review_items
			SET status = 'approved', resolved_at = ?, updated_at = ?
			WHERE owner_identity = ? AND id = ?`,
			resolvedAt, resolvedAt, owner, itemID,
		).Error
	}); err != nil {
		t.Fatalf("create approved task review fixture: %v", err)
	}

	stateRepository := task.NewPostgresTaskStateRepository(db)
	resolver, err := NewTaskReviewResolver(stateRepository)
	if err != nil {
		t.Fatalf("create resolver: %v", err)
	}
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatalf("begin execution transaction: %v", tx.Error)
	}
	approval, err := resolver.ResolveInPostgresTransaction(ctx, tx, owner, sourceID, requestDigest)
	if err != nil {
		_ = tx.Rollback().Error
		t.Fatalf("resolve approval in execution transaction: %v", err)
	}
	if approval.DecisionID != decisionID.String() || approval.SourceID != sourceID {
		_ = tx.Rollback().Error
		t.Fatalf("resolved approval = %#v, want decision %s for %s", approval, decisionID, sourceID)
	}

	concurrent := db.Begin()
	if concurrent.Error != nil {
		_ = tx.Rollback().Error
		t.Fatalf("begin concurrent transaction: %v", concurrent.Error)
	}
	var lockedID uuid.UUID
	lockErr := concurrent.Raw(`
		SELECT id FROM public.task_review_items WHERE owner_identity = ? AND id = ? FOR UPDATE NOWAIT`,
		owner, itemID,
	).Scan(&lockedID).Error
	_ = concurrent.Rollback().Error
	if lockErr == nil {
		_ = tx.Rollback().Error
		t.Fatal("a second transaction acquired the approved-item lock before approval consumption committed")
	}
	if err := tx.Rollback().Error; err != nil {
		t.Fatalf("release execution transaction: %v", err)
	}

	if _, err := stateRepository.MarkReviewOutcome(owner, itemID.String(), task.ReviewOutcome{
		TaskPlanID: "retry-" + taskPlanID,
		Status:     "needs_review",
		Reason:     "approval no longer applies",
		At:         time.Now().UTC().Add(time.Minute),
	}); err != nil {
		t.Fatalf("invalidate approved review after releasing lock: %v", err)
	}
	if _, err := resolver.Resolve(ctx, owner, sourceID, requestDigest); !errors.Is(err, ErrApprovalUnavailable) {
		t.Fatalf("resolution after invalidation error = %v, want ErrApprovalUnavailable", err)
	}
}

func taskReviewResolverPostgresDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("HAI_TEST_DATABASE_DSN"))
	if dsn == "" {
		t.Skip("HAI_TEST_DATABASE_DSN not set; skipping PostgreSQL task-review lock test")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open PostgreSQL: %v", err)
	}
	if _, err := infra.ApplyMigrations(db, migrations.Files, "pre"); err != nil {
		t.Fatalf("apply pre migrations: %v", err)
	}
	return db
}
