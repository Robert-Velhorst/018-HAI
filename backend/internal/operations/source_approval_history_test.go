package operations

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"automation-hub-backend/internal/models"
	"github.com/google/uuid"
)

func TestSourceApprovalReceiptConsumptionPaginatesAuditHistory(t *testing.T) {
	service, repo, op := sourceApprovalFixture(t)
	for i := 0; i < 125; i++ {
		old := time.Now().UTC().Add(-time.Duration(125-i) * time.Minute)
		if err := repo.AppendEvent(&models.OperationEvent{
			OperationID: op.ID,
			EventType:   "legacy_note",
			ActorType:   string(OwnerHAI),
			Message:     "historical operation event",
			PayloadJSON: "{}",
			CreatedAt:   old,
		}); err != nil {
			t.Fatalf("append historical event %d: %v", i, err)
		}
	}

	preview, err := service.PreviewSourceApproval(op)
	if err != nil {
		t.Fatalf("preview source approval: %v", err)
	}
	approved, receipt, err := service.ApproveSourceDerived(op, op.OwnerUserID, preview.Version, preview.RevisionDigest)
	if err != nil {
		t.Fatalf("approve source operation: %v", err)
	}
	claimed, err := service.ClaimOperation(context.Background(), op.OwnerUserID, op.WorkspaceID, op.ID, uuid.New(), time.Minute)
	if err != nil {
		t.Fatalf("claim approved operation: %v", err)
	}
	if claimed.Operation.Version != approved.Version {
		t.Fatalf("claim version=%d, approved version=%d", claimed.Operation.Version, approved.Version)
	}

	prepared := claimed.Operation
	prepared.RuntimeID = sourceApprovalSafeRuntimeID
	prepared.VerificationStatus = string(VerificationPending)
	running, err := service.ConsumeSourceApprovalClaimed(
		context.Background(), claimed.Claim, claimed.Operation, prepared, string(OwnerHAI), "worker",
	)
	if err != nil {
		t.Fatalf("consume source approval after paginating audit history: %v", err)
	}
	if err := service.ValidateConsumedSourceApproval(*running); err != nil {
		t.Fatalf("validate consumed approval: %v", err)
	}
	if _, err := service.ConsumeSourceApprovalClaimed(
		context.Background(), claimed.Claim, *approved, prepared, string(OwnerHAI), "worker-replay",
	); !errors.Is(err, ErrSourceApprovalReplayed) {
		t.Fatalf("replay error = %v, want ErrSourceApprovalReplayed", err)
	}

	events, err := service.listSourceApprovalEvents(op.ID)
	if err != nil {
		t.Fatalf("list source approval events: %v", err)
	}
	consumed := 0
	for _, event := range events {
		var payload sourceApprovalEventPayload
		if json.Unmarshal([]byte(event.PayloadJSON), &payload) == nil && payload.ConsumedReceipt == receipt.ID.String() {
			consumed++
			if event.BeforeStatus != string(StatusApproved) || event.AfterStatus != string(StatusRunning) || event.ActorID != "worker" {
				t.Fatalf("receipt consumption audit provenance = %#v", event)
			}
		}
	}
	if consumed != 1 {
		t.Fatalf("durable receipt consumption events=%d, want exactly one", consumed)
	}
}
