//go:build integration

package migrations_test

import (
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/infra"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestWorkflowReminderActivationLedgerIsImmutableOwnerScopedAndLinearInPostgres(t *testing.T) {
	db := openIsolatedMigrationDatabase(t)
	files := migrationFilesThrough(t, "pre/0047_workflow_reminder_activation_decision_order")
	if _, err := infra.ApplyMigrations(db, files, "pre"); err != nil {
		t.Fatalf("apply migrations through reminder activation ledger: %v", err)
	}

	owner := "reminder-owner-" + uuid.NewString() + "@example.com"
	workflowID := uuid.New()
	checklistID := uuid.New()
	reminderAt := time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond)
	if err := insertReminderActivationSource(db, owner, workflowID, checklistID, reminderAt); err != nil {
		t.Fatalf("insert reminder source: %v", err)
	}

	requestID := uuid.New()
	requestDigest := strings.Repeat("a", 64)
	if err := insertReminderActivationRequest(
		db, requestID, owner, workflowID, checklistID, reminderAt,
		"ledger:test:one", strings.Repeat("b", 64), requestDigest,
	); err != nil {
		t.Fatalf("insert valid activation request: %v", err)
	}
	if err := insertReminderActivationRequest(
		db, uuid.New(), "foreign-"+owner, workflowID, checklistID, reminderAt,
		"ledger:test:foreign", strings.Repeat("c", 64), strings.Repeat("d", 64),
	); err == nil {
		t.Fatal("database accepted a foreign-owner reminder activation request")
	}
	if err := db.Exec(`UPDATE public.workflow_reminder_activation_requests SET workflow_state = 'blocked' WHERE id = ?`, requestID).Error; err == nil {
		t.Fatal("database allowed activation request mutation")
	}
	if err := db.Exec(`DELETE FROM public.workflow_reminder_activation_requests WHERE id = ?`, requestID).Error; err == nil {
		t.Fatal("database allowed activation request deletion")
	}
	if err := db.Exec(`TRUNCATE public.workflow_reminder_activation_requests`).Error; err == nil {
		t.Fatal("database allowed activation request truncation")
	}

	approvedID := uuid.New()
	approvedAt := time.Now().UTC().Truncate(time.Microsecond)
	if err := insertReminderActivationDecisionAt(
		db, approvedID, requestID, owner, "approved", nil,
		requestDigest, strings.Repeat("e", 64), strings.Repeat("f", 64), approvedAt,
	); err != nil {
		t.Fatalf("insert approved activation decision: %v", err)
	}
	if err := insertReminderActivationDecision(
		db, uuid.New(), requestID, owner, "rejected", nil,
		requestDigest, strings.Repeat("1", 64), strings.Repeat("2", 64),
	); err == nil {
		t.Fatal("database accepted a fork that did not extend the current decision tip")
	}
	foreignRequestID := uuid.New()
	if err := insertReminderActivationRequest(
		db, foreignRequestID, owner, workflowID, checklistID, reminderAt,
		"ledger:test:two", strings.Repeat("3", 64), strings.Repeat("4", 64),
	); err != nil {
		t.Fatalf("insert second activation request: %v", err)
	}
	if err := insertReminderActivationDecision(
		db, uuid.New(), foreignRequestID, owner, "rejected", &approvedID,
		strings.Repeat("4", 64), strings.Repeat("5", 64), strings.Repeat("6", 64),
	); err == nil {
		t.Fatal("database accepted a previous decision from another activation request")
	}

	revokedID := uuid.New()
	if err := insertReminderActivationDecisionAt(
		db, uuid.New(), requestID, owner, "revoked", &approvedID,
		requestDigest, strings.Repeat("7", 64), strings.Repeat("8", 64), approvedAt,
	); err == nil {
		t.Fatal("database accepted a decision timestamp that did not advance after the chain tip")
	}
	if err := insertReminderActivationDecisionAt(
		db, revokedID, requestID, owner, "revoked", &approvedID,
		requestDigest, strings.Repeat("a", 64), strings.Repeat("b", 64), approvedAt.Add(time.Microsecond),
	); err != nil {
		t.Fatalf("append valid revocation: %v", err)
	}
	if err := db.Exec(`UPDATE public.workflow_reminder_activation_decisions SET reason = 'changed' WHERE id = ?`, approvedID).Error; err == nil {
		t.Fatal("database allowed activation decision mutation")
	}
	if err := db.Exec(`DELETE FROM public.workflow_reminder_activation_decisions WHERE id = ?`, revokedID).Error; err == nil {
		t.Fatal("database allowed activation decision deletion")
	}
	if err := db.Exec(`TRUNCATE public.workflow_reminder_activation_decisions`).Error; err == nil {
		t.Fatal("database allowed activation decision truncation")
	}

	if err := infra.RollbackMigration(
		db, files, "pre", "pre/0047_workflow_reminder_activation_decision_order",
	); err != nil {
		t.Fatalf("rollback decision-order migration before ledger guard: %v", err)
	}
	if err := infra.RollbackMigration(
		db, files, "pre", "pre/0046_workflow_reminder_activation_ledger",
	); err == nil || !strings.Contains(err.Error(), "refusing to remove non-empty workflow reminder activation ledgers") {
		t.Fatalf("non-empty rollback error = %v, want immutable-ledger refusal", err)
	}
}

func insertReminderActivationSource(
	db *gorm.DB,
	owner string,
	workflowID, checklistID uuid.UUID,
	reminderAt time.Time,
) error {
	if err := db.Exec(`
		INSERT INTO public.workflow_items (
			id, owner_identity, title, current_state, task_type, risk_level,
			autonomy_level, requires_approval, approval_status, archived,
			created_at, updated_at
		) VALUES (?, ?, 'Review internal reminder', 'ready', 'administrative', 'low',
			'manual', true, 'pending', false, now(), now())`,
		workflowID, owner,
	).Error; err != nil {
		return err
	}
	return db.Exec(`
		INSERT INTO public.workflow_checklist_items (
			id, workflow_id, label, status, position, requires_approval,
			reminder_at, created_at, updated_at
		) VALUES (?, ?, 'Review reminder internally', 'open', 1, true, ?, now(), now())`,
		checklistID, workflowID, reminderAt,
	).Error
}

func insertReminderActivationRequest(
	db *gorm.DB,
	requestID uuid.UUID,
	owner string,
	workflowID, checklistID uuid.UUID,
	reminderAt time.Time,
	idempotencyKey, reminderDigest, recordDigest string,
) error {
	now := time.Now().UTC().Truncate(time.Microsecond)
	return db.Exec(`
		INSERT INTO public.workflow_reminder_activation_requests (
			id, owner_identity, workflow_id, checklist_item_id, activation_kind,
			workflow_state, checklist_status, reminder_at, reminder_digest,
			idempotency_key, authority, actor, confirmation, request_digest,
			record_digest, requested_at, expires_at
		) VALUES (?, ?, ?, ?, 'internal_notification', 'ready', 'open', ?, ?, ?,
			'reminder_activation_request_only', ?, 'PREPARE INTERNAL REMINDER ONLY', ?, ?, ?, ?)`,
		requestID, owner, workflowID, checklistID, reminderAt, reminderDigest,
		idempotencyKey, owner, strings.Repeat("9", 64), recordDigest, now, now.Add(15*time.Minute),
	).Error
}

func insertReminderActivationDecision(
	db *gorm.DB,
	decisionID, requestID uuid.UUID,
	owner, decision string,
	previousID *uuid.UUID,
	activationRequestDigest, requestDigest, recordDigest string,
) error {
	return insertReminderActivationDecisionAt(
		db, decisionID, requestID, owner, decision, previousID,
		activationRequestDigest, requestDigest, recordDigest,
		time.Now().UTC().Truncate(time.Microsecond),
	)
}

func insertReminderActivationDecisionAt(
	db *gorm.DB,
	decisionID, requestID uuid.UUID,
	owner, decision string,
	previousID *uuid.UUID,
	activationRequestDigest, requestDigest, recordDigest string,
	now time.Time,
) error {
	confirmation := map[string]string{
		"approved": "APPROVE INTERNAL REMINDER PREPARATION",
		"rejected": "REJECT INTERNAL REMINDER PREPARATION",
		"revoked":  "REVOKE INTERNAL REMINDER PREPARATION",
	}[decision]
	var expiresAt *time.Time
	if decision == "approved" {
		expires := now.Add(10 * time.Minute)
		expiresAt = &expires
	}
	return db.Exec(`
		INSERT INTO public.workflow_reminder_activation_decisions (
			id, activation_request_id, owner_identity, decision, reason, actor,
			confirmation, activation_request_digest, previous_decision_id,
			authority, request_digest, record_digest, decided_at, expires_at
		) VALUES (?, ?, ?, ?, 'Owner reviewed internal preparation.', ?, ?, ?, ?,
			'reminder_activation_decision_only', ?, ?, ?, ?)`,
		decisionID, requestID, owner, decision, owner, confirmation,
		activationRequestDigest, previousID, requestDigest, recordDigest, now, expiresAt,
	).Error
}
