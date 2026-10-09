package executionauth

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestPostgresWorkflowDecisionClaimIsOneUseAcrossConcurrentTaskAndProofIdentities(t *testing.T) {
	repository, db := executionAuthorizationPostgresRepository(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond).Add(-time.Minute)
	owner := "workflow-decision-claim-owner-" + uuid.NewString() + "@example.com"
	_, decisionID := createWorkflowDecisionFixture(t, db, owner, now)

	receipts := []Receipt{
		postgresTestReceipt(owner, OutcomeAuthorized, now.Add(2*time.Second)),
		postgresTestReceipt(owner, OutcomeAuthorized, now.Add(3*time.Second)),
	}
	bindReceiptApproval(&receipts[0], "workflow-decision:"+decisionID.String(), decisionID, owner, now)
	bindReceiptApproval(&receipts[1], "workflow-decision:"+decisionID.String(), decisionID, owner, now)
	receipts[1].TaskID = "replacement-task-" + uuid.NewString()
	receipts[1].IdempotencyKey = "replacement-proof-" + uuid.NewString()
	receipts[1].RequestDigest = postgresDigest(receipts[1].TaskID + "-request")
	receipts[1].DecisionDigest = postgresDigest(receipts[1].TaskID + "-decision")
	receipts[1].EffectDigest = postgresDigest(receipts[1].TaskID + "-effect")

	for index := range receipts {
		if _, created, err := repository.CreateOrGet(ctx, receipts[index]); err != nil || !created {
			t.Fatalf("create workflow-approved receipt %d = (%t, %v)", index, created, err)
		}
	}

	type result struct {
		receiptID uuid.UUID
		err       error
	}
	start := make(chan struct{})
	results := make(chan result, len(receipts))
	for index := range receipts {
		index := index
		go func() {
			consumption := postgresTestConsumption(receipts[index])
			consumption.Consumer = fmt.Sprintf("workflow-worker-%d", index)
			<-start
			results <- result{receiptID: receipts[index].ID, err: repository.Consume(ctx, consumption)}
		}()
	}
	close(start)

	winners := make([]uuid.UUID, 0, 1)
	claimed := 0
	for range receipts {
		attempt := <-results
		switch {
		case attempt.err == nil:
			winners = append(winners, attempt.receiptID)
		case errors.Is(attempt.err, ErrApprovalAlreadyClaimed):
			claimed++
		default:
			t.Fatalf("unexpected workflow approval consumption result: %v", attempt.err)
		}
	}
	if len(winners) != 1 || claimed != 1 {
		t.Fatalf("successful consumptions=%d approval-claimed=%d, want exactly one of each", len(winners), claimed)
	}

	var claimCount int64
	if err := db.Table("execution_authorization_workflow_decision_claims").
		Where("owner_identity = ? AND workflow_decision_id = ?", owner, decisionID).
		Count(&claimCount).Error; err != nil || claimCount != 1 {
		t.Fatalf("durable workflow decision claims=%d, err=%v; want exactly one", claimCount, err)
	}
	var claimedReceipt uuid.UUID
	if err := db.Table("execution_authorization_workflow_decision_claims").
		Select("receipt_id").
		Where("owner_identity = ? AND workflow_decision_id = ?", owner, decisionID).
		Row().Scan(&claimedReceipt); err != nil || claimedReceipt != winners[0] {
		t.Fatalf("claim receipt=%s err=%v, want winning receipt %s", claimedReceipt, err, winners[0])
	}

	for _, receipt := range receipts {
		_, err := repository.GetConsumption(ctx, owner, receipt.ID)
		if receipt.ID == winners[0] {
			if err != nil {
				t.Fatalf("winning receipt consumption missing: %v", err)
			}
			continue
		}
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("rejected replay persisted a consumption: %v", err)
		}
		if err := repository.Consume(ctx, postgresTestConsumption(receipt)); !errors.Is(err, ErrApprovalAlreadyClaimed) {
			t.Fatalf("replayed workflow decision error=%v, want ErrApprovalAlreadyClaimed", err)
		}
	}
}
