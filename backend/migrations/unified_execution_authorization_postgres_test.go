//go:build integration

package migrations_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/infra"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestUnifiedExecutionAuthorizationFreshApplyRollbackAndReapply(
	t *testing.T,
) {
	db := unifiedAuthorizationIntegrationDB(t)
	files := migrationFilesThrough(t, "pre/0017_controlled_learning_application")
	applied, err := infra.ApplyMigrations(db, files, "pre")
	if err != nil {
		t.Fatalf("fresh pre-migration apply: %v", err)
	}
	if applied == 0 {
		t.Fatal("fresh pre-migration apply recorded no migrations")
	}

	assertUnifiedAuthorizationSchema(t, db)
	assertGovernanceMigrationTailSchema(t, db, true)
	for _, version := range []string{
		"pre/0017_controlled_learning_application",
		"pre/0016_evidence_packs",
		"pre/0015_controlled_learning",
		"pre/0014_unified_execution_authorization",
	} {
		if err := infra.RollbackMigration(db, files, "pre", version); err != nil {
			t.Fatalf("roll back empty migration %s: %v", version, err)
		}
	}
	assertUnifiedAuthorizationRolledBack(t, db)
	assertGovernanceMigrationTailSchema(t, db, false)
	emptyReapplied, err := infra.ApplyMigrations(db, files, "pre")
	if err != nil || emptyReapplied != 4 {
		t.Fatalf("reapply empty migrations 0014 through 0017 = (%d, %v), want (4, nil)", emptyReapplied, err)
	}
	assertUnifiedAuthorizationSchema(t, db)
	assertGovernanceMigrationTailSchema(t, db, true)

	owner := "migration-owner-" + uuid.NewString() + "@example.com"
	now := time.Now().UTC().Truncate(time.Microsecond)
	taskReviewID, taskDecisionID := createTaskReviewProvenance(
		t,
		db,
		owner,
		now,
	)
	workflowID, workflowDecisionID := createWorkflowDecisionProvenance(
		t,
		db,
		owner,
		now,
	)

	taskSource := "task-review:" + taskReviewID.String()
	if err := insertApprovalReceipt(
		db,
		owner,
		taskSource,
		taskDecisionID.String(),
		&taskDecisionID,
		nil,
	); err != nil {
		t.Fatalf("insert exact task-review provenance: %v", err)
	}
	workflowSource := "workflow-decision:" + workflowDecisionID.String()
	if err := insertApprovalReceipt(
		db,
		owner,
		workflowSource,
		workflowDecisionID.String(),
		nil,
		&workflowDecisionID,
	); err != nil {
		t.Fatalf("insert exact workflow-decision provenance: %v", err)
	}

	assertApprovalReceiptRejected(t, db, "task source does not match source record",
		owner,
		"task-review:"+uuid.NewString(),
		taskDecisionID.String(),
		&taskDecisionID,
		nil,
	)
	assertApprovalReceiptRejected(t, db, "task decision belongs to another owner",
		"other-"+owner,
		taskSource,
		taskDecisionID.String(),
		&taskDecisionID,
		nil,
	)
	assertApprovalReceiptRejected(t, db, "workflow source does not match decision",
		owner,
		"workflow-decision:"+uuid.NewString(),
		workflowDecisionID.String(),
		nil,
		&workflowDecisionID,
	)
	assertApprovalReceiptRejected(t, db, "evidence decision id does not match column",
		owner,
		workflowSource,
		uuid.NewString(),
		nil,
		&workflowDecisionID,
	)
	if err := insertApprovalReceiptWithEvidence(
		db,
		owner,
		workflowSource,
		"workflow-decision:"+uuid.NewString(),
		workflowDecisionID.String(),
		nil,
		&workflowDecisionID,
	); err == nil {
		t.Error("database accepted evidence source id that differs from receipt source")
	}
	assertApprovalReceiptRejected(t, db, "approval references are mutually exclusive",
		owner,
		taskSource,
		taskDecisionID.String(),
		&taskDecisionID,
		&workflowDecisionID,
	)

	if err := infra.RollbackMigration(
		db,
		files,
		"pre",
		"pre/0015_controlled_learning",
	); err == nil || !strings.Contains(
		err.Error(),
		`later migration "pre/0017_controlled_learning_application" is applied`,
	) {
		t.Fatalf("out-of-order rollback error = %v, want latest-migration rejection", err)
	}

	for _, version := range []string{
		"pre/0017_controlled_learning_application",
		"pre/0016_evidence_packs",
		"pre/0015_controlled_learning",
	} {
		if err := infra.RollbackMigration(
			db,
			files,
			"pre",
			version,
		); err != nil {
			t.Fatalf("rollback %s: %v", version, err)
		}
	}

	assertGovernanceMigrationTailSchema(t, db, false)
	var appliedAtBefore time.Time
	if err := db.Raw(`SELECT applied_at FROM public.schema_migrations WHERE version = ?`, "pre/0014_unified_execution_authorization").Row().Scan(&appliedAtBefore); err != nil {
		t.Fatalf("read authorization migration timestamp before protected rollback: %v", err)
	}
	if err := infra.RollbackMigration(db, files, "pre", "pre/0014_unified_execution_authorization"); err == nil || !strings.Contains(err.Error(), "authorization or workflow decision data exists") {
		t.Fatalf("non-empty authorization rollback error = %v, want retained-data refusal", err)
	}
	for table, id := range map[string]uuid.UUID{
		"task_review_decisions": taskDecisionID,
		"workflow_items":        workflowID,
		"workflow_decisions":    workflowDecisionID,
	} {
		var count int
		if err := db.Raw(fmt.Sprintf("SELECT count(*) FROM public.%s WHERE id = ?", table), id).Row().Scan(&count); err != nil {
			t.Fatalf("read protected provenance %s: %v", table, err)
		}
		if count != 1 {
			t.Errorf("protected provenance %s count = %d, want 1", table, count)
		}
	}
	var receipts int64
	if err := db.Raw(`SELECT count(*) FROM public.execution_authorization_receipts`).Scan(&receipts).Error; err != nil || receipts != 2 {
		t.Fatalf("authorization receipts after refused rollback = %d, err = %v; want 2", receipts, err)
	}
	var appliedAtAfter time.Time
	if err := db.Raw(`SELECT applied_at FROM public.schema_migrations WHERE version = ?`, "pre/0014_unified_execution_authorization").Row().Scan(&appliedAtAfter); err != nil || !appliedAtAfter.Equal(appliedAtBefore) {
		t.Fatalf("authorization ledger timestamp after refused rollback = %s, err = %v; want unchanged %s", appliedAtAfter, err, appliedAtBefore)
	}

	reapplied, err := infra.ApplyMigrations(db, files, "pre")
	if err != nil {
		t.Fatalf("reapply migrations 0015 through 0017: %v", err)
	}
	if reapplied != 3 {
		t.Fatalf("reapplied migrations = %d, want 3", reapplied)
	}
	assertUnifiedAuthorizationSchema(t, db)
	assertGovernanceMigrationTailSchema(t, db, true)

	var reboundOwner string
	if err := db.Raw(`
		SELECT owner_identity
		FROM public.workflow_decisions
		WHERE id = ?`,
		workflowDecisionID,
	).Row().Scan(&reboundOwner); err != nil {
		t.Fatalf("read rebound workflow decision owner: %v", err)
	}
	if reboundOwner != owner {
		t.Fatalf("rebound workflow owner = %q, want %q", reboundOwner, owner)
	}
}

func assertGovernanceMigrationTailSchema(
	t *testing.T,
	db *gorm.DB,
	wantPresent bool,
) {
	t.Helper()
	for _, table := range []string{
		"controlled_learning_outcomes",
		"controlled_learning_proposals",
		"controlled_learning_applications",
		"controlled_learning_application_events",
		"evidence_packs",
	} {
		var relation *string
		query := fmt.Sprintf("SELECT to_regclass('public.%s')::text", table)
		if err := db.Raw(query).Row().Scan(&relation); err != nil {
			t.Fatalf("check migration table %s: %v", table, err)
		}
		present := relation != nil
		if present != wantPresent {
			t.Errorf("migration table %s present = %t, want %t", table, present, wantPresent)
		}
	}

	for _, constraint := range []string{
		"uq_operations_owner_workspace_id",
		"fk_controlled_learning_review_decision_application",
		"chk_controlled_learning_review_decision_application",
	} {
		var present bool
		if err := db.Raw(`
			SELECT EXISTS (
				SELECT 1
				FROM pg_constraint
				WHERE conname = ?
			)`,
			constraint,
		).Row().Scan(&present); err != nil {
			t.Fatalf("check migration constraint %s: %v", constraint, err)
		}
		if present != wantPresent {
			t.Errorf("migration constraint %s present = %t, want %t", constraint, present, wantPresent)
		}
	}
}

func unifiedAuthorizationIntegrationDB(t *testing.T) *gorm.DB {
	t.Helper()
	return openIsolatedMigrationDatabase(t)
}

func assertUnifiedAuthorizationSchema(t *testing.T, db *gorm.DB) {
	t.Helper()
	for _, column := range []string{
		"task_review_decision_id",
		"workflow_decision_id",
	} {
		var nullable string
		if err := db.Raw(`
			SELECT is_nullable
			FROM information_schema.columns
			WHERE table_schema = 'public'
			  AND table_name = 'execution_authorization_receipts'
			  AND column_name = ?`,
			column,
		).Row().Scan(&nullable); err != nil {
			t.Fatalf("read %s nullability: %v", column, err)
		}
		if nullable != "YES" {
			t.Errorf("%s nullable = %q, want YES", column, nullable)
		}
	}

	taskFK := constraintDefinition(
		t,
		db,
		"execution_authorization_receipts",
		"fk_execution_authorization_receipt_task_review",
	)
	requireNormalizedSQL(t, taskFK,
		"FOREIGN KEY (owner_identity, task_review_decision_id, approval_source_id) "+
			"REFERENCES task_review_decisions(owner_identity, id, approval_source_id) "+
			"ON UPDATE RESTRICT ON DELETE RESTRICT",
	)
	workflowFK := constraintDefinition(
		t,
		db,
		"execution_authorization_receipts",
		"fk_execution_authorization_receipt_workflow_decision",
	)
	requireNormalizedSQL(t, workflowFK,
		"FOREIGN KEY (owner_identity, workflow_decision_id) "+
			"REFERENCES workflow_decisions(owner_identity, id) "+
			"ON UPDATE RESTRICT ON DELETE RESTRICT",
	)
	approvalCheck := constraintDefinition(
		t,
		db,
		"execution_authorization_receipts",
		"chk_execution_authorization_receipt_approval_binding",
	)
	normalizedApprovalCheck := normalizeSQL(approvalCheck)
	for _, fragment := range []string{
		"task_review_decision_idisnotnull",
		"workflow_decision_idisnull",
		"'task-review:%'",
		"'workflow-decision:'",
		"workflow_decision_id",
	} {
		if !strings.Contains(normalizedApprovalCheck, fragment) {
			t.Errorf("approval binding check %q lacks %q", approvalCheck, fragment)
		}
	}
}

func assertUnifiedAuthorizationRolledBack(
	t *testing.T,
	db *gorm.DB,
) {
	t.Helper()
	var receiptTable *string
	if err := db.Raw(
		`SELECT to_regclass('public.execution_authorization_receipts')::text`,
	).Row().Scan(&receiptTable); err != nil {
		t.Fatalf("check receipt table after rollback: %v", err)
	}
	if receiptTable != nil {
		t.Fatalf("receipt table remains after rollback: %q", *receiptTable)
	}

	var workflowOwnerColumn bool
	if err := db.Raw(`
		SELECT EXISTS (
			SELECT 1
			FROM information_schema.columns
			WHERE table_schema = 'public'
			  AND table_name = 'workflow_decisions'
			  AND column_name = 'owner_identity'
		)`,
	).Row().Scan(&workflowOwnerColumn); err != nil {
		t.Fatalf("check workflow owner column after rollback: %v", err)
	}
	if workflowOwnerColumn {
		t.Fatal("workflow decision owner column remains after rollback")
	}

	var sourceConstraint bool
	if err := db.Raw(`
		SELECT EXISTS (
			SELECT 1
			FROM pg_constraint
			WHERE conname = 'uq_task_review_decision_owner_id_source'
		)`,
	).Row().Scan(&sourceConstraint); err != nil {
		t.Fatalf("check source constraint after rollback: %v", err)
	}
	if sourceConstraint {
		t.Fatal("task-review source constraint remains after rollback")
	}

	for _, table := range []string{"task_review_decisions", "workflow_items", "workflow_decisions"} {
		var count int
		query := fmt.Sprintf("SELECT count(*) FROM public.%s", table)
		if err := db.Raw(query).Row().Scan(&count); err != nil {
			t.Fatalf("check predecessor row %s: %v", table, err)
		}
		if count != 0 {
			t.Errorf("predecessor table %s rows = %d after empty rollback, want 0", table, count)
		}
	}
}

func createTaskReviewProvenance(
	t *testing.T,
	db *gorm.DB,
	owner string,
	now time.Time,
) (uuid.UUID, uuid.UUID) {
	t.Helper()
	reviewID := uuid.New()
	decisionID := uuid.New()
	planID := "plan-" + reviewID.String()
	requestDigest := unifiedAuthorizationDigest("review-" + reviewID.String())
	resolvedAt := now.Add(time.Second)
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`
			INSERT INTO public.task_review_items (
				id, owner_identity, original_task_plan_id,
				current_task_plan_id, request_digest, request_json,
				reason, priority, status, review_revision,
				created_at, updated_at
			) VALUES (?, ?, ?, ?, ?, '{}'::jsonb, ?, 'normal',
				'needs_review', 1, ?, ?)`,
			reviewID,
			owner,
			planID,
			planID,
			requestDigest,
			"migration provenance fixture",
			now,
			now,
		).Error; err != nil {
			return err
		}
		if err := tx.Exec(`
			INSERT INTO public.task_review_decisions (
				id, review_item_id, review_revision, owner_identity,
				task_plan_id, decision, resolution_note, resolved_by,
				approval_source, approval_source_id, request_digest,
				resolved_at
			) VALUES (?, ?, 1, ?, ?, 'approved', ?, ?,
				'task-review', ?, ?, ?)`,
			decisionID,
			reviewID,
			owner,
			planID,
			"approved exact migration fixture",
			owner,
			"task-review:"+reviewID.String(),
			requestDigest,
			resolvedAt,
		).Error; err != nil {
			return err
		}
		return tx.Exec(`
			UPDATE public.task_review_items
			SET status = 'approved', updated_at = ?, resolved_at = ?
			WHERE id = ?`,
			resolvedAt,
			resolvedAt,
			reviewID,
		).Error
	})
	if err != nil {
		t.Fatalf("create task-review provenance: %v", err)
	}
	return reviewID, decisionID
}

func createWorkflowDecisionProvenance(
	t *testing.T,
	db *gorm.DB,
	owner string,
	now time.Time,
) (uuid.UUID, uuid.UUID) {
	t.Helper()
	workflowID := uuid.New()
	decisionID := uuid.New()
	if err := db.Exec(`
		INSERT INTO public.workflow_items (
			id, owner_identity, title, current_state, created_at, updated_at
		) VALUES (?, ?, ?, 'awaiting_approval', ?, ?)`,
		workflowID,
		owner,
		"Migration approval fixture",
		now,
		now,
	).Error; err != nil {
		t.Fatalf("create workflow fixture: %v", err)
	}
	if err := db.Exec(`
		INSERT INTO public.workflow_decisions (
			id, workflow_id, decision_type, decision, reason,
			rule_applied, approved, actor, created_at
		) VALUES (?, ?, 'approval', 'approved', ?, ?, true, ?, ?)`,
		decisionID,
		workflowID,
		"approved exact migration fixture",
		"automation-action:test:"+unifiedAuthorizationDigest(decisionID.String()),
		owner,
		now,
	).Error; err != nil {
		t.Fatalf("create workflow decision provenance: %v", err)
	}
	return workflowID, decisionID
}

func insertApprovalReceipt(
	db *gorm.DB,
	owner string,
	sourceID string,
	evidenceDecisionID string,
	taskDecisionID *uuid.UUID,
	workflowDecisionID *uuid.UUID,
) error {
	return insertApprovalReceiptWithEvidence(
		db,
		owner,
		sourceID,
		sourceID,
		evidenceDecisionID,
		taskDecisionID,
		workflowDecisionID,
	)
}

func insertApprovalReceiptWithEvidence(
	db *gorm.DB,
	owner string,
	sourceID string,
	evidenceSourceID string,
	evidenceDecisionID string,
	taskDecisionID *uuid.UUID,
	workflowDecisionID *uuid.UUID,
) error {
	id := uuid.New()
	evidence, err := json.Marshal(map[string]any{
		"emergencyStop": map[string]any{"active": false, "source": "integration-test"},
		"constitution":  map[string]any{"source": "builtin-robert-constitution-v1"},
		"mandate":       map[string]any{},
		"agent":         map[string]any{},
		"approval": map[string]any{
			"sourceId":   evidenceSourceID,
			"decisionId": evidenceDecisionID,
		},
		"reasonCodes": []string{"integration.provenance"},
		"trace":       []string{"migration contract"},
	})
	if err != nil {
		return err
	}
	return db.Exec(`
		INSERT INTO public.execution_authorization_receipts (
			id, contract_version, owner_identity, idempotency_key,
			actor_identity, actor_kind, task_id, action, stage,
			resource_type, resource_id, project_key, runtime_id,
			approval_source_id, effect_digest, outcome, reason,
			request_digest, decision_digest, required_authority,
			requested_autonomy, effective_autonomy, risk, reversible,
			estimated_cost_eur, notification_required, evaluated_at,
			evidence_json, task_review_decision_id, workflow_decision_id
		) VALUES (
			?, 1, ?, ?, 'system:migration-test', 'system', ?,
			'migration.provenance.verify', 'execution',
			'migration-contract', '', '', '', ?, ?, 'authorized',
			'exact approval provenance verified', ?, ?, 1, 1, 1,
			'low', true, 0, false, ?, CAST(? AS jsonb), ?, ?
		)`,
		id,
		owner,
		"migration-"+id.String(),
		"task-"+id.String(),
		sourceID,
		unifiedAuthorizationDigest("effect-"+id.String()),
		unifiedAuthorizationDigest("request-"+id.String()),
		unifiedAuthorizationDigest("decision-"+id.String()),
		time.Now().UTC(),
		string(evidence),
		taskDecisionID,
		workflowDecisionID,
	).Error
}

func assertApprovalReceiptRejected(
	t *testing.T,
	db *gorm.DB,
	name string,
	owner string,
	sourceID string,
	evidenceDecisionID string,
	taskDecisionID *uuid.UUID,
	workflowDecisionID *uuid.UUID,
) {
	t.Helper()
	if err := insertApprovalReceipt(
		db,
		owner,
		sourceID,
		evidenceDecisionID,
		taskDecisionID,
		workflowDecisionID,
	); err == nil {
		t.Errorf("database accepted invalid approval provenance: %s", name)
	}
}

func constraintDefinition(
	t *testing.T,
	db *gorm.DB,
	table string,
	constraint string,
) string {
	t.Helper()
	var definition string
	if err := db.Raw(`
		SELECT pg_get_constraintdef(oid)
		FROM pg_constraint
		WHERE conrelid = (? || '.' || ?)::regclass
		  AND conname = ?`,
		"public",
		table,
		constraint,
	).Row().Scan(&definition); err != nil {
		t.Fatalf("read constraint %s: %v", constraint, err)
	}
	return definition
}

func requireNormalizedSQL(t *testing.T, actual string, expected string) {
	t.Helper()
	if normalizeSQL(actual) != normalizeSQL(expected) {
		t.Errorf("constraint definition = %q, want %q", actual, expected)
	}
}

func normalizeSQL(value string) string {
	return strings.ToLower(
		strings.Join(strings.Fields(strings.ReplaceAll(value, `"`, "")), ""),
	)
}

func unifiedAuthorizationDigest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
