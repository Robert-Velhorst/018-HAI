package router

import (
	"testing"
	"time"

	"automation-hub-backend/internal/opscontrol"
	"automation-hub-backend/internal/task"
)

func TestOpsControlTaskReviewRepositoryPersistsAndOwnerScopesReviews(t *testing.T) {
	ledger := task.NewMemoryTaskStateRepository()
	repository := newOpsControlTaskReviewRepository(ledger)
	created, err := repository.CreateControlReview("owner-1", opscontrol.ControlReviewItem{
		ID:              "11111111-1111-4111-8111-111111111111",
		TaskID:          "opscontrol:resume:binding",
		OwnerIdentity:   "owner-1",
		Request:         "Resume background processing",
		ProjectKey:      "runtime-control",
		SuccessCriteria: []string{"use the current revision"},
		Reason:          "Explicit owner review is required.",
		Status:          "open",
		CreatedAt:       time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("create review: %v", err)
	}
	if created.OwnerIdentity != "owner-1" || created.TaskID != "opscontrol:resume:binding" {
		t.Fatalf("created review lost its owner or immutable task binding: %#v", created)
	}
	if _, err := repository.FindControlReview("other-owner", created.ID); err == nil {
		t.Fatal("cross-owner review lookup unexpectedly succeeded")
	}
	if _, err := repository.CreateControlReview("other-owner", opscontrol.ControlReviewItem{
		ID:            "22222222-2222-4222-8222-222222222222",
		OwnerIdentity: "owner-1",
		Status:        "open",
	}); err == nil {
		t.Fatal("review with mismatched owner identity unexpectedly persisted")
	}
	if err := repository.ApproveControlReview("owner-1", created.ID, "explicit review", time.Now().UTC()); err != nil {
		t.Fatalf("approve review: %v", err)
	}
	decision, err := repository.FindApprovedControlReview("owner-1", created.ID)
	if err != nil || decision == nil || decision.ResolvedBy != "owner-1" {
		t.Fatalf("approved decision = %#v, %v", decision, err)
	}
	pending, err := repository.ListPendingControlReviews("owner-1", 10)
	if err != nil || len(pending) != 0 {
		t.Fatalf("pending reviews = %#v, %v", pending, err)
	}
}
