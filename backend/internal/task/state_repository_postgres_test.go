//go:build integration

package task

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"automation-hub-backend/internal/infra"
	"automation-hub-backend/internal/models"
	"automation-hub-backend/migrations"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func taskStateLifecycleMigrations(t *testing.T) fstest.MapFS {
	t.Helper()
	files := fstest.MapFS{}
	for _, version := range []string{
		"0001_extensions",
		"0002_baseline",
		"0003_framework_registry",
		"0004_task_state_storage",
		"0005_framework_operating_contract",
		"0006_durable_job_fencing",
	} {
		for _, suffix := range []string{"up.sql", "down.sql"} {
			path := "pre/" + version + "." + suffix
			data, err := migrations.Files.ReadFile(path)
			if err != nil {
				t.Fatalf("read lifecycle migration %s: %v", path, err)
			}
			files[path] = &fstest.MapFile{Data: data}
		}
	}
	return files
}

func TestPostgresTaskStateRepositoryDurabilityOwnerScopeAndImmutability(t *testing.T) {
	db := openTaskStatePostgresTestDB(t)
	files := taskStateLifecycleMigrations(t)
	applied, err := infra.ApplyMigrations(db, files, "pre")
	if err != nil {
		t.Fatalf("apply pre migrations: %v", err)
	}
	if applied < 4 {
		t.Fatalf("applied %d pre migrations, want at least 4", applied)
	}
	if rerun, err := infra.ApplyMigrations(db, files, "pre"); err != nil || rerun != 0 {
		t.Fatalf("migration rerun = %d, %v; want 0, nil", rerun, err)
	}
	executeTaskStateMigration(t, db, "pre/0004_task_state_storage.up.sql")
	executeTaskStateMigration(t, db, "pre/0004_task_state_storage.up.sql")
	for _, triggerName := range []string{
		"trg_task_completion_plan_logs_immutable",
		"trg_task_completion_plan_logs_no_truncate",
		"trg_task_review_items_insert",
		"trg_task_review_items_provenance",
		"trg_task_review_items_transition",
		"trg_task_review_items_no_delete",
		"trg_task_review_items_no_truncate",
		"trg_task_review_decisions_binding",
		"trg_task_review_decisions_resolution_state",
		"trg_task_review_decisions_immutable",
		"trg_task_review_decisions_no_truncate",
	} {
		if count := taskStateTriggerCount(t, db, triggerName); count != 1 {
			t.Fatalf("trigger %s count = %d, want 1", triggerName, count)
		}
	}

	repo := NewPostgresTaskStateRepository(db)
	owner := "task-state-" + uuid.NewString()

	plan := taskStateTestPlan(uuid.NewString(), owner, time.Now().UTC())
	plan.Request = "Validate password=postgres-plan-secret"
	plan.CompletionStatus = "validated"
	plan.ValidationResult.Status = "verified"
	if err := repo.AppendCompletionPlan(owner, plan); err != nil {
		t.Fatalf("append completion plan: %v", err)
	}
	if err := repo.AppendCompletionPlan(owner, plan); err != nil {
		t.Fatalf("idempotent completion append: %v", err)
	}
	var completionRow models.TaskCompletionPlanLog
	if err := db.Where("owner_identity = ?", owner).First(&completionRow).Error; err != nil {
		t.Fatalf("read completion row: %v", err)
	}
	var completionCount int64
	if err := db.Model(&models.TaskCompletionPlanLog{}).
		Where("owner_identity = ? AND task_plan_id = ?", owner, plan.ID).
		Count(&completionCount).Error; err != nil {
		t.Fatalf("count idempotent completion rows: %v", err)
	}
	if completionCount != 1 {
		t.Fatalf("idempotent completion row count = %d, want 1", completionCount)
	}
	if strings.Contains(completionRow.PayloadJSON, "postgres-plan-secret") {
		t.Fatalf("completion row retained secret: %s", completionRow.PayloadJSON)
	}
	plans, err := repo.ListCompletionPlans(owner, 50)
	if err != nil || len(plans) != 1 || plans[0].ID != plan.ID {
		t.Fatalf("completion round trip = %#v, %v", plans, err)
	}
	foreignPlans, err := repo.ListCompletionPlans("foreign-"+owner, 50)
	if err != nil || len(foreignPlans) != 0 {
		t.Fatalf("foreign completion history = %#v, %v", foreignPlans, err)
	}
	expectTaskStatePostgresRejection(t, db, "completion update", func(db *gorm.DB) error {
		return db.Model(&models.TaskCompletionPlanLog{}).
			Where("id = ?", completionRow.ID).
			Update("completion_status", "tampered").Error
	})
	expectTaskStatePostgresRejection(t, db, "completion delete", func(db *gorm.DB) error {
		return db.Delete(&models.TaskCompletionPlanLog{}, "id = ?", completionRow.ID).Error
	})

	review := taskStateTestReviewItem(owner, plan.ID, time.Now().UTC())
	review.Request.Request = "Deploy with api_key=postgres-review-secret after approval"
	created, err := repo.CreateReviewItem(owner, review)
	if err != nil {
		t.Fatalf("create review item: %v", err)
	}
	if strings.Contains(created.Request.Request, "postgres-review-secret") {
		t.Fatalf("review round trip retained secret: %#v", created)
	}
	recreated, err := repo.CreateReviewItem(owner, review)
	if err != nil || recreated.ID != created.ID {
		t.Fatalf("idempotent review create = %#v, %v", recreated, err)
	}
	if _, err := repo.FindReviewItem("foreign-"+owner, review.ID); err != ErrTaskStateNotFound {
		t.Fatalf("foreign review lookup error = %v", err)
	}
	resolvedAt := time.Now().UTC().Add(time.Second)
	resolution, err := repo.ResolveReviewItem(owner, review.ID, ReviewResolution{
		Decision:   "approved",
		Note:       "Approved token=postgres-decision-secret",
		ResolvedAt: resolvedAt,
	})
	if err != nil {
		t.Fatalf("resolve review item: %v", err)
	}
	if resolution.Decision.ApprovalSourceID != "task-review:"+review.ID ||
		resolution.Decision.ReviewRevision != 1 ||
		resolution.Decision.ResolvedBy != owner ||
		strings.Contains(resolution.Decision.ResolutionNote, "postgres-decision-secret") {
		t.Fatalf("resolved decision provenance = %#v", resolution.Decision)
	}
	approved, err := repo.FindApprovedReviewDecision(owner, review.ID)
	if err != nil || approved.ID != resolution.Decision.ID {
		t.Fatalf("approved decision = %#v, %v", approved, err)
	}
	if replayed, err := repo.ResolveReviewItem(owner, review.ID, ReviewResolution{Decision: "approved"}); !errors.Is(err, ErrTaskReviewAlreadyResolved) || replayed != nil {
		t.Fatalf("replayed approval = %#v, %v; want nil, already resolved", replayed, err)
	}
	completed, err := repo.MarkReviewOutcome(owner, review.ID, ReviewOutcome{
		TaskPlanID: uuid.NewString(),
		Status:     "completed",
		At:         resolvedAt.Add(time.Second),
	})
	if err != nil || completed.Status != "completed" {
		t.Fatalf("complete approved review = %#v, %v", completed, err)
	}
	idempotentCompletion, err := repo.MarkReviewOutcome(owner, review.ID, ReviewOutcome{
		TaskPlanID: completed.TaskID,
		Status:     "completed",
	})
	if err != nil || idempotentCompletion.Status != "completed" {
		t.Fatalf("idempotent completion outcome = %#v, %v", idempotentCompletion, err)
	}

	reviewID, _ := uuid.Parse(review.ID)
	var reviewRow models.TaskReviewItemRecord
	if err := db.Where("id = ?", reviewID).First(&reviewRow).Error; err != nil {
		t.Fatalf("read review row: %v", err)
	}
	var decisionRow models.TaskReviewDecisionRecord
	if err := db.Where("review_item_id = ?", reviewID).First(&decisionRow).Error; err != nil {
		t.Fatalf("read decision row: %v", err)
	}
	expectTaskStatePostgresRejection(t, db, "review request provenance update", func(db *gorm.DB) error {
		return db.Model(&models.TaskReviewItemRecord{}).
			Where("id = ?", reviewID).
			Update("request_digest", strings.Repeat("0", 64)).Error
	})
	expectTaskStatePostgresRejection(t, db, "review item invalid state transition", func(db *gorm.DB) error {
		return db.Model(&models.TaskReviewItemRecord{}).
			Where("id = ?", reviewID).
			Update("status", "approved").Error
	})
	expectTaskStatePostgresRejection(t, db, "review item delete", func(db *gorm.DB) error {
		return db.Delete(&models.TaskReviewItemRecord{}, "id = ?", reviewID).Error
	})
	expectTaskStatePostgresRejection(t, db, "decision update", func(db *gorm.DB) error {
		return db.Model(&models.TaskReviewDecisionRecord{}).
			Where("id = ?", decisionRow.ID).
			Update("decision", "rejected").Error
	})
	expectTaskStatePostgresRejection(t, db, "decision delete", func(db *gorm.DB) error {
		return db.Delete(&models.TaskReviewDecisionRecord{}, "id = ?", decisionRow.ID).Error
	})
	expectTaskStatePostgresRejection(t, db, "cross-owner decision binding", func(db *gorm.DB) error {
		crossOwner := decisionRow
		crossOwner.ID = uuid.New()
		crossOwner.OwnerIdentity = "foreign-" + owner
		crossOwner.ResolvedAt = crossOwner.ResolvedAt.Add(time.Second)
		return db.Create(&crossOwner).Error
	})

	forgedReview := taskStateTestReviewItem(owner, uuid.NewString(), time.Now().UTC())
	forgedReviewRow, err := reviewItemToModel(owner, forgedReview)
	if err != nil {
		t.Fatalf("build forged review row: %v", err)
	}
	forgedReviewRow.RequestJSON = strings.TrimSuffix(forgedReviewRow.RequestJSON, "}") + `,"humanApproved":true}`
	if err := db.Create(&forgedReviewRow).Error; err != nil {
		t.Fatalf("insert structurally valid forged review row: %v", err)
	}
	if _, err := repo.FindReviewItem(owner, forgedReview.ID); err == nil || !strings.Contains(err.Error(), "transient approval state") {
		t.Fatalf("forged Postgres approval state error = %v", err)
	}

	forgedResolved := taskStateTestReviewItem(owner, uuid.NewString(), time.Now().UTC())
	forgedResolvedRow, err := reviewItemToModel(owner, forgedResolved)
	if err != nil {
		t.Fatalf("build forged resolved row: %v", err)
	}
	forgedResolvedRow.Status = "approved"
	forgedResolvedAt := forgedResolvedRow.CreatedAt.Add(time.Second)
	forgedResolvedRow.ResolvedAt = &forgedResolvedAt
	forgedResolvedRow.UpdatedAt = forgedResolvedAt
	expectTaskStatePostgresRejection(t, db, "review item inserted as already approved", func(db *gorm.DB) error {
		return db.Create(&forgedResolvedRow).Error
	})

	oversizedPayload := `{"value":"` + strings.Repeat("x", taskStateMaximumPayloadSize) + `"}`
	oversizedRow := models.TaskCompletionPlanLog{
		ID:                 uuid.New(),
		OwnerIdentity:      owner,
		TaskPlanID:         uuid.NewString(),
		CompletionStatus:   "planned",
		VerificationStatus: "not_run",
		PayloadJSON:        oversizedPayload,
		PayloadDigest:      strings.Repeat("0", 64),
		ProvenanceSource:   taskCompletionProvenance,
		CreatedAt:          time.Now().UTC(),
	}
	expectTaskStatePostgresRejection(t, db, "oversized completion payload", func(db *gorm.DB) error {
		return db.Create(&oversizedRow).Error
	})

	badPayload := `{"unexpected":true}`
	badDigest := sha256.Sum256([]byte(badPayload))
	badRow := models.TaskCompletionPlanLog{
		ID:                 uuid.New(),
		OwnerIdentity:      owner,
		TaskPlanID:         uuid.NewString(),
		CompletionStatus:   "planned",
		VerificationStatus: "not_run",
		PayloadJSON:        badPayload,
		PayloadDigest:      fmt.Sprintf("%x", badDigest[:]),
		ProvenanceSource:   taskCompletionProvenance,
		CreatedAt:          time.Now().UTC().Add(time.Minute),
	}
	if err := db.Create(&badRow).Error; err != nil {
		t.Fatalf("insert structurally malformed completion row: %v", err)
	}
	if _, err := repo.ListCompletionPlans(owner, 50); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("malformed Postgres payload error = %v", err)
	}

	testPostgresTaskStateConcurrentTransitions(t, db, repo, owner)

	if err := infra.RollbackMigration(
		db,
		files,
		"pre",
		"pre/0006_durable_job_fencing",
	); err != nil {
		t.Fatalf("rollback durable-job fencing migration before task state: %v", err)
	}
	if err := infra.RollbackMigration(
		db,
		files,
		"pre",
		"pre/0005_framework_operating_contract",
	); err != nil {
		t.Fatalf("rollback operating-contract migration before task state: %v", err)
	}
	if err := infra.RollbackMigration(
		db,
		files,
		"pre",
		"pre/0004_task_state_storage",
	); err != nil {
		t.Fatalf("rollback task-state migration: %v", err)
	}
	for _, relation := range []string{
		"task_completion_plan_logs",
		"task_review_items",
		"task_review_decisions",
	} {
		if taskStateRelationExists(t, db, relation) {
			t.Fatalf("%s survived rollback", relation)
		}
	}
	for _, functionName := range []string{
		"hai_reject_task_audit_mutation",
		"hai_reject_task_audit_truncate",
		"hai_enforce_task_review_item_insert",
		"hai_enforce_task_review_item_provenance",
		"hai_enforce_task_review_item_transition",
		"hai_enforce_task_review_decision_binding",
		"hai_require_task_review_resolution_state",
	} {
		if taskStateFunctionExists(t, db, functionName) {
			t.Fatalf("function %s survived rollback", functionName)
		}
	}
	if !taskStateRelationExists(t, db, "framework_preferences") {
		t.Fatal("task-state rollback removed the prior Framework Registry schema")
	}
	executeTaskStateMigration(t, db, "pre/0004_task_state_storage.down.sql")
	reapplied, err := infra.ApplyMigrations(db, files, "pre")
	if err != nil {
		t.Fatalf("reapply task-state migration: %v", err)
	}
	if reapplied != 3 || !taskStateRelationExists(t, db, "task_review_items") {
		t.Fatalf("task-state migration reapply = %d, relation=%t", reapplied, taskStateRelationExists(t, db, "task_review_items"))
	}
}

func openTaskStatePostgresTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("HAI_TEST_DATABASE_DSN"))
	if dsn == "" {
		t.Skip("HAI_TEST_DATABASE_DSN not set; skipping task-state Postgres integration test")
	}
	if !strings.EqualFold(strings.TrimSpace(os.Getenv("HAI_ALLOW_DESTRUCTIVE_DATABASE_TESTS")), "true") {
		t.Skip("HAI_ALLOW_DESTRUCTIVE_DATABASE_TESTS=true is required for task-state migration tests")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open Postgres: %v", err)
	}
	var databaseName string
	if err := db.Raw("SELECT current_database()").Scan(&databaseName).Error; err != nil {
		t.Fatalf("read current database: %v", err)
	}
	if !strings.HasSuffix(strings.ToLower(databaseName), "_test") {
		t.Fatalf("refusing destructive task-state test against database %q", databaseName)
	}
	if err := db.Exec("DROP SCHEMA public CASCADE; CREATE SCHEMA public;").Error; err != nil {
		t.Fatalf("reset task-state test schema: %v", err)
	}
	return db
}

func executeTaskStateMigration(t *testing.T, db *gorm.DB, path string) {
	t.Helper()
	sql, err := migrations.Files.ReadFile(path)
	if err != nil {
		t.Fatalf("read migration %s: %v", path, err)
	}
	if err := db.Exec(string(sql)).Error; err != nil {
		t.Fatalf("execute migration %s: %v", path, err)
	}
}

func taskStateRelationExists(t *testing.T, db *gorm.DB, relation string) bool {
	t.Helper()
	var exists bool
	if err := db.Raw("SELECT to_regclass(?) IS NOT NULL", "public."+relation).Scan(&exists).Error; err != nil {
		t.Fatalf("query relation %s: %v", relation, err)
	}
	return exists
}

func taskStateFunctionExists(t *testing.T, db *gorm.DB, functionName string) bool {
	t.Helper()
	var exists bool
	if err := db.Raw(`
		SELECT EXISTS (
			SELECT 1
			FROM pg_proc p
			JOIN pg_namespace n ON n.oid = p.pronamespace
			WHERE n.nspname = 'public' AND p.proname = ?
		)
	`, functionName).Scan(&exists).Error; err != nil {
		t.Fatalf("query function %s: %v", functionName, err)
	}
	return exists
}

func taskStateTriggerCount(t *testing.T, db *gorm.DB, triggerName string) int64 {
	t.Helper()
	var count int64
	if err := db.Raw(`
		SELECT count(*)
		FROM pg_trigger
		WHERE tgname = ? AND NOT tgisinternal
	`, triggerName).Scan(&count).Error; err != nil {
		t.Fatalf("query trigger %s: %v", triggerName, err)
	}
	return count
}

func testPostgresTaskStateConcurrentTransitions(
	t *testing.T,
	db *gorm.DB,
	repo *PostgresTaskStateRepository,
	owner string,
) {
	t.Helper()
	base := time.Now().UTC().Add(-time.Minute).Truncate(time.Microsecond)
	item := taskStateTestReviewItem(owner, uuid.NewString(), base)
	if _, err := repo.CreateReviewItem(owner, item); err != nil {
		t.Fatalf("create concurrent review: %v", err)
	}

	const workers = 12
	start := make(chan struct{})
	errorsByWorker := make(chan error, workers)
	var resolveWG sync.WaitGroup
	for index := 0; index < workers; index++ {
		resolveWG.Add(1)
		go func() {
			defer resolveWG.Done()
			<-start
			_, err := repo.ResolveReviewItem(owner, item.ID, ReviewResolution{
				Decision:   "approved",
				ResolvedAt: base.Add(time.Second),
			})
			errorsByWorker <- err
		}()
	}
	close(start)
	resolveWG.Wait()
	close(errorsByWorker)
	successes := 0
	for err := range errorsByWorker {
		if err == nil {
			successes++
			continue
		}
		if !errors.Is(err, ErrTaskReviewAlreadyResolved) {
			t.Fatalf("concurrent resolution error = %v", err)
		}
	}
	if successes != 1 {
		t.Fatalf("concurrent successful resolutions = %d, want exactly 1", successes)
	}
	itemID, _ := uuid.Parse(item.ID)
	var decisionCount int64
	if err := db.Model(&models.TaskReviewDecisionRecord{}).
		Where("review_item_id = ?", itemID).
		Count(&decisionCount).Error; err != nil {
		t.Fatalf("count concurrent decisions: %v", err)
	}
	if decisionCount != 1 {
		t.Fatalf("concurrent decision rows = %d, want 1", decisionCount)
	}
	if replayed, err := repo.ResolveReviewItem(owner, item.ID, ReviewResolution{Decision: "approved"}); !errors.Is(err, ErrTaskReviewAlreadyResolved) || replayed != nil {
		t.Fatalf("post-concurrency replay = %#v, %v; want nil, already resolved", replayed, err)
	}

	outcomeErrors := make(chan error, workers)
	var outcomeWG sync.WaitGroup
	for index := 0; index < workers; index++ {
		outcomeWG.Add(1)
		go func() {
			defer outcomeWG.Done()
			_, err := repo.MarkReviewOutcome(owner, item.ID, ReviewOutcome{
				TaskPlanID: "concurrent-completed-plan",
				Status:     "completed",
				At:         base.Add(2 * time.Second),
			})
			outcomeErrors <- err
		}()
	}
	outcomeWG.Wait()
	close(outcomeErrors)
	for err := range outcomeErrors {
		if err != nil {
			t.Fatalf("concurrent idempotent outcome: %v", err)
		}
	}
}

func expectTaskStatePostgresRejection(t *testing.T, db *gorm.DB, label string, operation func(*gorm.DB) error) {
	t.Helper()
	err := db.Transaction(func(nested *gorm.DB) error {
		return operation(nested)
	})
	if err == nil {
		t.Fatalf("Postgres accepted %s", label)
	}
}
