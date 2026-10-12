package migrations_test

import (
	"context"
	"testing"
	"time"

	"automation-hub-backend/internal/agentruntime"
	"automation-hub-backend/internal/openclawreconcile"
	"automation-hub-backend/migrations"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestOpenClawReceiptAlignmentPostgres(t *testing.T) {
	db := openIsolatedMigrationDatabase(t)
	if err := db.Exec(`CREATE EXTENSION IF NOT EXISTS "uuid-ossp"`).Error; err != nil {
		t.Fatal(err)
	}
	apply := func(name string) error {
		data, err := migrations.Files.ReadFile("pre/" + name + ".up.sql")
		if err != nil {
			t.Fatal(err)
		}
		return db.Transaction(func(tx *gorm.DB) error { return tx.Exec(string(data)).Error })
	}
	for _, name := range []string{"0069_openclaw_gateway_session_receipts", "0070_openclaw_gateway_session_review", "0074_openclaw_session_instance"} {
		if err := apply(name); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Exec(`CREATE TABLE public.open_claw_gateway_session_receipts (LIKE public.openclaw_gateway_session_receipts INCLUDING ALL)`).Error; err != nil {
		t.Fatal(err)
	}
	legacyID := uuid.New()
	legacyReference := "ocgw:v2:" + uuid.NewString()
	legacyRunID := "run-legacy"
	if err := db.Exec(`INSERT INTO open_claw_gateway_session_receipts
		(id, execution_reference, owner_identity, runtime_task_id, session_key, run_id, status, created_at)
		VALUES (?, ?, 'test-owner', 'task-legacy', 'agent:main:test', ?, 'admitted', ?)`,
		legacyID, legacyReference, legacyRunID, time.Now().UTC()).Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := apply("0075_openclaw_receipt_table_alignment"); err != nil {
			t.Fatal(err)
		}
	}
	if err := apply("0076_openclaw_usage_recovery"); err != nil {
		t.Fatal(err)
	}
	if err := apply("0077_openclaw_usage_snapshot"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"0081_openclaw_gateway_admission_intents", "0082_openclaw_gateway_cancellation_intents", "0089_openclaw_reconcile_fairness_and_cancellation_retry"} {
		if err := apply(name); err != nil {
			t.Fatal(err)
		}
	}
	repo := openclawreconcile.NewRepository(db)
	old, err := repo.FindOpenClawGatewayReceipt(legacyReference)
	if err != nil || old.RunID != legacyRunID || old.SessionID != "" {
		t.Fatalf("legacy receipt lost or fabricated: %#v %v", old, err)
	}
	newReceipt := agentruntime.OpenClawGatewayReceipt{
		ExecutionReference: "ocgw:v2:" + uuid.NewString(), OwnerIdentity: "test-owner", RuntimeTaskID: "task-new",
		SessionKey: "agent:main:test", SessionID: "instance-new", RunID: "run-new", RequestedModel: "ollama/qwen3:14b", CreatedAt: time.Now().UTC(),
	}
	newIntent := newReceipt
	newIntent.Status, newIntent.SessionKey, newIntent.RunID = "admitting", "", ""
	if err := repo.CreateOpenClawGatewayReceipt(context.Background(), newIntent); err != nil {
		t.Fatal(err)
	}
	if admitted, err := repo.MarkOpenClawGatewayReceiptAdmitted(context.Background(), newReceipt); err != nil || !admitted {
		t.Fatalf("mark receipt admitted: admitted=%v err=%v", admitted, err)
	}
	got, err := repo.FindOpenClawGatewayReceipt(newReceipt.ExecutionReference)
	if err != nil || got.SessionID != newReceipt.SessionID || got.RequestedModel != newReceipt.RequestedModel {
		t.Fatalf("new receipt not durable: %#v %v", got, err)
	}
	if err := db.Table("open_claw_gateway_session_receipts").Where("id = ?", legacyID).Update("run_id", "conflicting-run").Error; err != nil {
		t.Fatal(err)
	}
	if err := apply("0075_openclaw_receipt_table_alignment"); err == nil {
		t.Fatal("conflicting history was silently accepted")
	}
	old, err = repo.FindOpenClawGatewayReceipt(legacyReference)
	if err != nil || old.RunID != legacyRunID {
		t.Fatal("conflict overwrote canonical history")
	}
	var retained int64
	if err := db.Table("open_claw_gateway_session_receipts").Count(&retained).Error; err != nil || retained != 1 {
		t.Fatalf("legacy source was not preserved: %d %v", retained, err)
	}
}
