package openclawreconcile

import (
	"context"
	"errors"
	"testing"
	"time"

	"automation-hub-backend/internal/agentruntime"
	"automation-hub-backend/internal/models"
	"github.com/google/uuid"
)

type artifactTestRepository struct {
	*fakeReceiptRepository
	claim     *ArtifactClaim
	completed bool
	failed    int
}

func (r *artifactTestRepository) ClaimOpenClawArtifacts(context.Context, time.Time) (*ArtifactClaim, error) {
	c := r.claim
	r.claim = nil
	return c, nil
}
func (r *artifactTestRepository) CompleteOpenClawArtifacts(_ context.Context, _ ArtifactClaim, _ []agentruntime.GatewayArtifactDescriptor, _ uuid.UUID) (bool, error) {
	r.completed = true
	return true, nil
}
func (r *artifactTestRepository) FailOpenClawArtifacts(_ context.Context, _ ArtifactClaim) (bool, error) {
	r.failed++
	return true, nil
}

type artifactTestGateway struct {
	*fakeGatewayReconciler
	reads  int
	paused bool
	fail   bool
}

func (g *artifactTestGateway) OpenClawArtifactRecoveryReady() bool { return !g.paused }
func (g *artifactTestGateway) ReadOpenClawGatewayArtifactDescriptors(context.Context, string, string) ([]agentruntime.GatewayArtifactDescriptor, error) {
	g.reads++
	if g.fail {
		return nil, errors.New("private gateway error")
	}
	return []agentruntime.GatewayArtifactDescriptor{}, nil
}

func TestArtifactRecoveryRetriesReadWithoutRerunningWork(t *testing.T) {
	for _, scenario := range []string{"empty", "failure", "paused", "wrong_owner"} {
		t.Run(scenario, func(t *testing.T) {
			id := uuid.New()
			receipt := agentruntime.OpenClawGatewayReceipt{ExecutionReference: "ocgw:v2:" + uuid.NewString(), OwnerIdentity: "alice", RuntimeTaskID: "task", TerminalStatus: "completed", TerminalAt: time.Now().UTC()}
			repo := &artifactTestRepository{fakeReceiptRepository: &fakeReceiptRepository{}, claim: &ArtifactClaim{Receipt: receipt, Attempt: 1, Token: uuid.New()}}
			ledger := &fakeAutomationLedger{launch: &models.AutomationLaunchEvent{AutomationID: id, OwnerIdentity: "alice", RuntimeType: "openclaw", RuntimeTaskID: receipt.RuntimeTaskID, ExecutionReference: receipt.ExecutionReference}, automation: &models.Automation{ID: id}}
			gateway := &artifactTestGateway{fakeGatewayReconciler: &fakeGatewayReconciler{}, paused: scenario == "paused", fail: scenario == "failure"}
			if scenario == "wrong_owner" {
				ledger.launch.OwnerIdentity = "mallory"
			}
			count, err := NewService(repo, ledger, gateway).Reconcile(10)
			if count != 0 || ledger.automation.LastSuccessAt != nil || len(ledger.events) != 0 {
				t.Fatal("metadata recovery fabricated terminal work")
			}
			switch scenario {
			case "empty":
				if err != nil || gateway.reads != 1 || !repo.completed {
					t.Fatalf("empty successful collection not settled: %v", err)
				}
			case "failure":
				if err == nil || repo.completed || repo.failed != 1 || gateway.reads != 1 {
					t.Fatal("failed collection was accepted or its claim was not released")
				}
			case "paused":
				if err != nil || repo.claim == nil || gateway.reads != 0 {
					t.Fatal("pause consumed an attempt")
				}
			case "wrong_owner":
				if err == nil || gateway.reads != 0 || repo.completed || repo.failed != 1 {
					t.Fatal("owner boundary bypassed")
				}
			}
		})
	}
}
