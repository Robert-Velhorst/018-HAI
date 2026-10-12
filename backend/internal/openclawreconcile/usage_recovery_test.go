package openclawreconcile

import (
	"context"
	"testing"
	"time"

	"automation-hub-backend/internal/agentruntime"
	"automation-hub-backend/internal/models"
	"github.com/google/uuid"
)

type usageTestRepository struct {
	*fakeReceiptRepository
	claim     *UsageClaim
	completed bool
}

func (r *usageTestRepository) ClaimOpenClawUsage(context.Context, time.Time) (*UsageClaim, error) {
	c := r.claim
	r.claim = nil
	return c, nil
}
func (r *usageTestRepository) CompleteOpenClawUsage(_ context.Context, _ UsageClaim, snapshot *agentruntime.GatewaySessionUsageSnapshot, _ uuid.UUID) (bool, error) {
	r.completed = snapshot != nil
	return r.completed, nil
}

type usageTestGateway struct {
	*fakeGatewayReconciler
	reads  int
	paused bool
}

func (g *usageTestGateway) OpenClawUsageRecoveryReady() bool { return !g.paused }

func (g *usageTestGateway) ReadOpenClawGatewayUsageSnapshot(context.Context, string, string) (*agentruntime.GatewaySessionUsageSnapshot, error) {
	g.reads++
	return &agentruntime.GatewaySessionUsageSnapshot{Source: "openclaw.sessions.usage"}, nil
}

func TestReconcileRecoversUsageWithoutRerunningTerminalWork(t *testing.T) {
	id := uuid.New()
	r := agentruntime.OpenClawGatewayReceipt{ExecutionReference: "ocgw:v2:" + uuid.NewString(), OwnerIdentity: "alice", RuntimeTaskID: "task", TerminalStatus: "completed", TerminalAt: time.Now().UTC()}
	repo := &usageTestRepository{fakeReceiptRepository: &fakeReceiptRepository{}, claim: &UsageClaim{Receipt: r, Attempt: 1}}
	ledger := &fakeAutomationLedger{launch: &models.AutomationLaunchEvent{AutomationID: id, OwnerIdentity: r.OwnerIdentity, RuntimeType: "openclaw", RuntimeTaskID: r.RuntimeTaskID, ExecutionReference: r.ExecutionReference}, automation: &models.Automation{ID: id}}
	gateway := &usageTestGateway{fakeGatewayReconciler: &fakeGatewayReconciler{}}
	count, err := NewService(repo, ledger, gateway).Reconcile(10)
	if err != nil || count != 0 || gateway.reads != 1 || !repo.completed {
		t.Fatalf("recovery was not wired: settled=%d reads=%d stored=%t err=%v", count, gateway.reads, repo.completed, err)
	}
	if ledger.automation.LastSuccessAt != nil || len(ledger.events) != 0 {
		t.Fatal("usage recovery fabricated a new terminal execution")
	}
}

func TestUsageRecoveryDoesNotConsumeAttemptsWhilePaused(t *testing.T) {
	repo := &usageTestRepository{fakeReceiptRepository: &fakeReceiptRepository{}, claim: &UsageClaim{Attempt: 1}}
	gateway := &usageTestGateway{fakeGatewayReconciler: &fakeGatewayReconciler{}, paused: true}
	_, err := NewService(repo, &fakeAutomationLedger{}, gateway).Reconcile(10)
	if err != nil || repo.claim == nil || gateway.reads != 0 {
		t.Fatalf("paused recovery consumed work: claim=%#v reads=%d err=%v", repo.claim, gateway.reads, err)
	}
}
