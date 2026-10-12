package migrations_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"automation-hub-backend/internal/agentruntime"
	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/openclawreconcile"
	"automation-hub-backend/migrations"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestOpenClawUsageRecoveryPostgres(t *testing.T) {
	db := openIsolatedMigrationDatabase(t)
	if err := db.Exec(`CREATE EXTENSION IF NOT EXISTS "uuid-ossp"`).Error; err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"0069_openclaw_gateway_session_receipts", "0070_openclaw_gateway_session_review", "0074_openclaw_session_instance", "0076_openclaw_usage_recovery", "0077_openclaw_usage_snapshot", "0081_openclaw_gateway_admission_intents", "0082_openclaw_gateway_cancellation_intents", "0089_openclaw_reconcile_fairness_and_cancellation_retry"} {
		data, err := migrations.Files.ReadFile("pre/" + name + ".up.sql")
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Exec(string(data)).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.AutoMigrate(&models.AutomationLaunchEvent{}); err != nil {
		t.Fatal(err)
	}
	repo := openclawreconcile.NewRepository(db)
	now := time.Now().UTC()
	ctx := context.Background()
	for _, scenario := range []string{"success", "legacy_summary", "retry_limit"} {
		r := agentruntime.OpenClawGatewayReceipt{ExecutionReference: "ocgw:v2:" + uuid.NewString(), RuntimeTaskID: "usage-task", OwnerIdentity: "alice", SessionKey: "agent:main:test", SessionID: uuid.NewString(), RunID: uuid.NewString(), CreatedAt: now.Add(-time.Minute)}
		intent := r
		intent.Status, intent.SessionKey, intent.RunID = "admitting", "", ""
		if err := repo.CreateOpenClawGatewayReceipt(ctx, intent); err != nil {
			t.Fatal(err)
		}
		if admitted, err := repo.MarkOpenClawGatewayReceiptAdmitted(ctx, r); err != nil || !admitted {
			t.Fatalf("mark receipt admitted: admitted=%v err=%v", admitted, err)
		}
		if _, err := repo.MarkOpenClawGatewayReceiptTerminal(r.ExecutionReference, "completed", now); err != nil {
			t.Fatal(err)
		}
		var legacyEventID uuid.UUID
		if scenario == "legacy_summary" {
			if err := db.Model(&models.OpenClawGatewaySessionReceipt{}).Where("execution_reference = ?", r.ExecutionReference).Updates(map[string]any{"usage_summary": "original usage audit", "usage_captured_at": now}).Error; err != nil {
				t.Fatal(err)
			}
			legacyEventID = uuid.New()
			if err := db.Create(&models.AutomationLaunchEvent{ID: legacyEventID, AutomationID: uuid.New(), OwnerIdentity: r.OwnerIdentity, ExecutionReference: r.ExecutionReference, EventKey: "openclaw-gateway-usage:" + r.ExecutionReference, Status: "observed", Message: "original usage audit", StartedAt: now, CompletedAt: now}).Error; err != nil {
				t.Fatal(err)
			}
		}
		claim, err := repo.ClaimOpenClawUsage(ctx, now)
		if err != nil || claim == nil || claim.Attempt != 1 {
			t.Fatalf("claim: %#v %v", claim, err)
		}
		if other, err := openclawreconcile.NewRepository(db).ClaimOpenClawUsage(ctx, now); err != nil || other != nil {
			t.Fatalf("lease allowed duplicate work: %#v %v", other, err)
		}
		if scenario != "retry_limit" {
			snapshot := &agentruntime.GatewaySessionUsageSnapshot{ExecutionReference: r.ExecutionReference, SessionKey: r.SessionKey, SessionID: r.SessionID, Source: "openclaw.sessions.usage", Scope: "session-instance", StartDate: r.CreatedAt.UTC().Format("2006-01-02"), EndDate: now.UTC().Format("2006-01-02"), ObservedAt: now, Totals: agentruntime.GatewayUsageTotals{Input: 12, Output: 4, TotalTokens: 16}}
			wrongInstance := *snapshot
			wrongInstance.SessionID = uuid.NewString()
			if ok, err := repo.CompleteOpenClawUsage(ctx, *claim, &wrongInstance, uuid.New()); err == nil || ok {
				t.Fatal("stored another session's usage")
			}
			staleClaim := *claim
			staleClaim.Attempt++
			if ok, err := repo.CompleteOpenClawUsage(ctx, staleClaim, snapshot, uuid.New()); err != nil || ok {
				t.Fatalf("accepted mismatched lease: %t %v", ok, err)
			}
			ok, err := repo.CompleteOpenClawUsage(ctx, *claim, snapshot, uuid.New())
			if err != nil || !ok {
				t.Fatalf("complete: %t %v", ok, err)
			}
			if ok, err = repo.CompleteOpenClawUsage(ctx, *claim, snapshot, uuid.New()); err != nil || ok {
				t.Fatalf("duplicate completion: %t %v", ok, err)
			}
			var count int64
			expectedEvents := int64(1)
			if legacyEventID != uuid.Nil {
				expectedEvents++
				var original models.AutomationLaunchEvent
				if err := db.First(&original, "id = ?", legacyEventID).Error; err != nil || original.Message != "original usage audit" {
					t.Fatalf("legacy audit lost: %v", err)
				}
			}
			if err := db.Model(&models.AutomationLaunchEvent{}).Where("execution_reference = ?", r.ExecutionReference).Count(&count).Error; err != nil || count != expectedEvents {
				t.Fatalf("usage event duplicated: %d %v", count, err)
			}
			var stored models.OpenClawGatewaySessionReceipt
			if err := db.Where("execution_reference = ?", r.ExecutionReference).First(&stored).Error; err != nil {
				t.Fatal(err)
			}
			if stored.UsageSnapshotJSON == nil {
				t.Fatal("typed usage snapshot was not stored")
			}
			var usageEvent models.AutomationLaunchEvent
			if err := db.Where("event_key = ?", "openclaw-gateway-usage-v2:"+r.ExecutionReference).First(&usageEvent).Error; err != nil {
				t.Fatal(err)
			}
			view, err := repo.UsageForEvent(ctx, r.OwnerIdentity, usageEvent.ID)
			if err != nil || view.State != "captured" || view.Snapshot.Totals.Input != 12 {
				t.Fatalf("owner usage read: %#v %v", view, err)
			}
			if _, err := repo.UsageForEvent(ctx, "mallory", usageEvent.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
				t.Fatalf("cross-owner access: %v", err)
			}
			if err := db.Model(&models.AutomationLaunchEvent{}).Where("id = ?", usageEvent.ID).Update("runtime_task_id", "unrelated-task").Error; err != nil {
				t.Fatal(err)
			}
			if _, err := repo.UsageForEvent(ctx, r.OwnerIdentity, usageEvent.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
				t.Fatalf("wrong task mapped to receipt: %v", err)
			}
			var decoded agentruntime.GatewaySessionUsageSnapshot
			if err := json.Unmarshal([]byte(*stored.UsageSnapshotJSON), &decoded); err != nil || decoded.Totals.Input != 12 || decoded.SessionID != "" {
				t.Fatalf("invalid private snapshot projection: %#v %v", decoded, err)
			}
		} else {
			for attempt := 2; attempt <= 8; attempt++ {
				now = now.Add(25 * time.Hour)
				claim, err = openclawreconcile.NewRepository(db).ClaimOpenClawUsage(ctx, now)
				if err != nil || claim == nil || claim.Attempt != attempt {
					t.Fatalf("retry %d: %#v %v", attempt, claim, err)
				}
			}
			if claim, err = repo.ClaimOpenClawUsage(ctx, now.Add(25*time.Hour)); err != nil || claim != nil {
				t.Fatalf("durable retry limit ignored: %#v %v", claim, err)
			}
		}
	}
}
