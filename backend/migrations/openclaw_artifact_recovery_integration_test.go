package migrations_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/agentruntime"
	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/openclawreconcile"
	"automation-hub-backend/migrations"
	"github.com/google/uuid"
)

func TestOpenClawArtifactRecoveryPostgres(t *testing.T) {
	db := openIsolatedMigrationDatabase(t)
	if err := db.Exec(`CREATE EXTENSION IF NOT EXISTS "uuid-ossp"`).Error; err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"0069_openclaw_gateway_session_receipts", "0070_openclaw_gateway_session_review", "0071_openclaw_gateway_artifact_receipts", "0074_openclaw_session_instance", "0076_openclaw_usage_recovery", "0077_openclaw_usage_snapshot", "0078_openclaw_artifact_alignment", "0079_openclaw_artifact_recovery", "0081_openclaw_gateway_admission_intents", "0082_openclaw_gateway_cancellation_intents", "0089_openclaw_reconcile_fairness_and_cancellation_retry", "0098_openclaw_artifact_claim_leases"} {
		sql, err := migrations.Files.ReadFile("pre/" + name + ".up.sql")
		if err != nil {
			t.Fatal(err)
		}
		if err = db.Exec(string(sql)).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.AutoMigrate(&models.AutomationLaunchEvent{}); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	now := time.Now().UTC()
	repo := openclawreconcile.NewRepository(db)
	for _, scenario := range []string{"captured", "empty", "retry_limit", "lease_expiry"} {
		t.Run(scenario, func(t *testing.T) {
			r := agentruntime.OpenClawGatewayReceipt{ExecutionReference: "ocgw:v2:" + uuid.NewString(), RuntimeTaskID: "artifact-task", OwnerIdentity: "alice", SessionKey: "agent:main:test", RunID: uuid.NewString(), CreatedAt: now.Add(-time.Minute)}
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
			id := uuid.New()
			event := models.AutomationLaunchEvent{ID: uuid.New(), AutomationID: id, OwnerIdentity: r.OwnerIdentity, RuntimeType: "openclaw", RuntimeTaskID: r.RuntimeTaskID, ExecutionReference: r.ExecutionReference, EventKey: uuid.NewString(), Status: "completed", StartedAt: now, CompletedAt: now}
			if err := db.Create(&event).Error; err != nil {
				t.Fatal(err)
			}
			type claimed struct {
				claim *openclawreconcile.ArtifactClaim
				err   error
			}
			claims := make(chan claimed, 4)
			start := make(chan struct{})
			for i := 0; i < 4; i++ {
				go func() {
					<-start
					c, err := openclawreconcile.NewRepository(db).ClaimOpenClawArtifacts(ctx, now)
					claims <- claimed{c, err}
				}()
			}
			close(start)
			var claim *openclawreconcile.ArtifactClaim
			var err error
			for i := 0; i < 4; i++ {
				got := <-claims
				if got.err != nil {
					t.Fatal(got.err)
				}
				if got.claim != nil {
					if claim != nil {
						t.Fatal("concurrent workers received the same lease")
					}
					claim = got.claim
				}
			}
			if claim == nil || claim.Attempt != 1 || claim.Token == uuid.Nil {
				t.Fatalf("claim: %#v", claim)
			}
			activeView, err := repo.ArtifactsForEvent(ctx, "alice", event.ID)
			if err != nil || activeView.State != "collecting" || activeView.NextAttemptAt != nil {
				t.Fatalf("active artifact collection view: %#v %v", activeView, err)
			}
			if c, err := openclawreconcile.NewRepository(db).ClaimOpenClawArtifacts(ctx, now); err != nil || c != nil {
				t.Fatalf("duplicate active lease: %#v %v", c, err)
			}
			if scenario == "lease_expiry" {
				if err := db.Table("openclaw_gateway_artifact_collections").Where("execution_reference = ?", r.ExecutionReference).Update("claimed_at", now.Add(-4*time.Minute)).Error; err != nil {
					t.Fatal(err)
				}
				if ok, err := repo.CompleteOpenClawArtifacts(ctx, *claim, []agentruntime.GatewayArtifactDescriptor{}, id); err != nil || ok {
					t.Fatalf("expired lease accepted late completion: %t %v", ok, err)
				}
				expiredView, err := repo.ArtifactsForEvent(ctx, "alice", event.ID)
				if err != nil || expiredView.State != "pending" {
					t.Fatalf("expired lease view = %#v, %v; want pending", expiredView, err)
				}
				reclaimed, err := repo.ClaimOpenClawArtifacts(ctx, now.Add(4*time.Minute))
				if err != nil || reclaimed == nil || reclaimed.Attempt != 2 || reclaimed.Token == claim.Token {
					t.Fatalf("expired lease was not reclaimed with a fresh fence: %#v %v", reclaimed, err)
				}
				if ok, err := repo.CompleteOpenClawArtifacts(ctx, *claim, []agentruntime.GatewayArtifactDescriptor{}, id); err != nil || ok {
					t.Fatalf("replaced lease accepted late completion: %t %v", ok, err)
				}
				if _, err := repo.FailOpenClawArtifacts(ctx, *reclaimed); err != nil {
					t.Fatalf("release reclaimed test claim: %v", err)
				}
				return
			}
			if scenario == "retry_limit" {
				released, err := repo.FailOpenClawArtifacts(ctx, *claim)
				if err != nil || !released {
					t.Fatalf("release failed attempt: released=%v err=%v", released, err)
				}
				if ok, err := repo.CompleteOpenClawArtifacts(ctx, *claim, []agentruntime.GatewayArtifactDescriptor{}, id); err != nil || ok {
					t.Fatalf("released claim accepted a late completion: %t %v", ok, err)
				}
				if c, err := repo.ClaimOpenClawArtifacts(ctx, now); err != nil || c != nil {
					t.Fatalf("failed attempt bypassed backoff: %#v %v", c, err)
				}
				claim, err = repo.ClaimOpenClawArtifacts(ctx, now.Add(16*time.Minute))
				if err != nil || claim == nil || claim.Attempt != 2 || claim.Token == uuid.Nil {
					t.Fatalf("retry after failure: %#v %v", claim, err)
				}
				for attempt := 3; attempt <= 8; attempt++ {
					claim, err = repo.ClaimOpenClawArtifacts(ctx, now.Add(time.Duration(attempt)*48*time.Hour))
					if err != nil || claim == nil || claim.Attempt != attempt {
						t.Fatalf("retry %d: %#v %v", attempt, claim, err)
					}
				}
				if c, err := repo.ClaimOpenClawArtifacts(ctx, now.Add(50*24*time.Hour)); err != nil || c != nil {
					t.Fatal("retry ceiling failed")
				}
				return
			}
			descriptors := []agentruntime.GatewayArtifactDescriptor{}
			if scenario == "captured" {
				descriptors = append(descriptors, agentruntime.GatewayArtifactDescriptor{Digest: strings.Repeat("a", 64), Type: "file"})
			}
			stale := *claim
			stale.Attempt++
			if ok, err := repo.CompleteOpenClawArtifacts(ctx, stale, descriptors, id); err != nil || ok {
				t.Fatalf("stale lease accepted: %t %v", ok, err)
			}
			wrong := *claim
			wrong.Receipt.OwnerIdentity = "mallory"
			if ok, err := repo.CompleteOpenClawArtifacts(ctx, wrong, descriptors, id); err != nil || ok {
				t.Fatal("wrong owner accepted")
			}
			if ok, err := repo.CompleteOpenClawArtifacts(ctx, *claim, descriptors, uuid.New()); err == nil && ok {
				t.Fatal("wrong automation accepted")
			}
			// A failed audit insert must roll back both descriptors and collection state.
			if err := db.Exec(`ALTER TABLE automation_launch_events ADD CONSTRAINT test_reject_artifact_audit CHECK (launch_type <> 'agent_runtime_openclaw_artifacts') NOT VALID`).Error; err != nil {
				t.Fatal(err)
			}
			if ok, err := repo.CompleteOpenClawArtifacts(ctx, *claim, descriptors, id); err == nil || ok {
				t.Fatal("audit failure did not abort completion")
			}
			var count int64
			if err := db.Model(&models.OpenClawGatewayArtifactReceipt{}).Where("execution_reference = ?", r.ExecutionReference).Count(&count).Error; err != nil || count != 0 {
				t.Fatal("partial artifacts escaped transaction")
			}
			if err := db.Exec(`ALTER TABLE automation_launch_events DROP CONSTRAINT test_reject_artifact_audit`).Error; err != nil {
				t.Fatal(err)
			}
			if ok, err := repo.CompleteOpenClawArtifacts(ctx, *claim, descriptors, id); err != nil || !ok {
				t.Fatalf("complete: %t %v", ok, err)
			}
			if ok, err := repo.CompleteOpenClawArtifacts(ctx, *claim, descriptors, id); err != nil || ok {
				t.Fatalf("duplicate completion: %t %v", ok, err)
			}
			if c, err := repo.ClaimOpenClawArtifacts(ctx, now.Add(24*time.Hour)); err != nil || c != nil {
				t.Fatal("captured collection requeued")
			}
			if err := db.Model(&models.AutomationLaunchEvent{}).Where("execution_reference = ? AND launch_type = ?", r.ExecutionReference, "agent_runtime_openclaw_artifacts").Count(&count).Error; err != nil || count != 1 {
				t.Fatal("audit was not recorded once")
			}
			view, err := repo.ArtifactsForEvent(ctx, "alice", event.ID)
			if err != nil || view.State != "captured" || len(view.Items) != len(descriptors) {
				t.Fatalf("artifact view: %#v %v", view, err)
			}
			if _, err := repo.ArtifactsForEvent(ctx, "mallory", event.ID); err == nil {
				t.Fatal("another owner read artifacts")
			}
			if scenario == "captured" {
				binding, err := repo.ArtifactForEvent(ctx, "alice", event.ID, descriptors[0].Digest)
				if err != nil || binding.Receipt.RunID != r.RunID {
					t.Fatalf("download binding: %#v %v", binding, err)
				}
				if _, err := repo.ArtifactForEvent(ctx, "mallory", event.ID, descriptors[0].Digest); err == nil {
					t.Fatal("another owner obtained download binding")
				}
				if _, err := repo.ArtifactForEvent(ctx, "alice", event.ID, strings.Repeat("b", 64)); err == nil {
					t.Fatal("unknown artifact obtained download binding")
				}
				if err := repo.RecordArtifactDownload(ctx, *binding, strings.Repeat("b", 64), 2); err != nil {
					t.Fatal(err)
				}
				var audit models.AutomationLaunchEvent
				if err := db.Where("execution_reference = ? AND launch_type = ?", r.ExecutionReference, "agent_runtime_openclaw_artifact_download").Take(&audit).Error; err != nil || audit.Status != "observed" {
					t.Fatal("download audit missing")
				}
				if err := db.Model(&event).Update("runtime_task_id", "other-task").Error; err != nil {
					t.Fatal(err)
				}
				if _, err := repo.ArtifactForEvent(ctx, "alice", event.ID, descriptors[0].Digest); err == nil {
					t.Fatal("mismatched task obtained download binding")
				}
			}
		})
	}
}
