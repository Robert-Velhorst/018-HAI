package opscontrol

import (
	"context"
	"errors"
	"testing"
	"time"

	"automation-hub-backend/internal/executionauth"
)

func TestResumeApprovalIsBoundToTheCurrentEmergencyStopRevision(t *testing.T) {
	service := newTestService(t)
	now := time.Now().UTC()
	service.now = func() time.Time { return now }
	service.WithExecutionAuthorizer(allowExactControlAuthorization(service.now))
	reviews := newMemoryControlReviews()
	service.WithControlReviewRepository(reviews)

	if _, err := service.EngageEmergencyStop("test stop", service.owner); err != nil {
		t.Fatalf("engage emergency stop: %v", err)
	}

	pending, err := service.RequestResumeApproval(context.Background(), service.owner)
	if err != nil {
		t.Fatalf("request resume approval: %v", err)
	}
	if pending.ApprovalSourceID == "" || pending.ApprovalBindingDigest == "" {
		t.Fatalf("approval request is not bound: %#v", pending)
	}

	// A second stop changes the protected state and must make the original
	// approval unusable, even if it is later approved.
	if _, err := service.EngageEmergencyStop("new stop revision", service.owner); err != nil {
		t.Fatalf("re-engage emergency stop: %v", err)
	}
	if err := reviews.ApproveControlReview(service.owner, pending.ReviewItemID, "", now); err != nil {
		t.Fatalf("approve durable review: %v", err)
	}

	_, err = service.ResumeWithApprovedReview(context.Background(), service.owner, pending.ReviewItemID)
	if err == nil {
		t.Fatal("stale resume approval unexpectedly cleared the newer emergency stop")
	}
	if !service.Control().EmergencyStop() {
		t.Fatal("stale resume approval cleared the emergency stop")
	}
}

func TestResumeWithApprovedReviewConsumesExactControlAuthorization(t *testing.T) {
	service := newTestService(t)
	now := time.Now().UTC()
	service.now = func() time.Time { return now }
	reviews := newMemoryControlReviews()
	service.WithControlReviewRepository(reviews)

	var request executionauth.Request
	service.WithExecutionAuthorizer(controlAuthorizerFunc(func(
		_ context.Context,
		captured executionauth.Request,
		_ string,
		_ string,
	) (executionauth.Receipt, error) {
		request = captured
		return exactControlReceipt(captured, now), nil
	}))
	if _, err := service.EngageEmergencyStop("test stop", service.owner); err != nil {
		t.Fatalf("engage emergency stop: %v", err)
	}
	pending, err := service.RequestResumeApproval(context.Background(), service.owner)
	if err != nil {
		t.Fatalf("request resume approval: %v", err)
	}
	if err := reviews.ApproveControlReview(service.owner, pending.ReviewItemID, "", now); err != nil {
		t.Fatalf("approve durable review: %v", err)
	}

	state, err := service.ResumeWithApprovedReview(context.Background(), service.owner, pending.ReviewItemID)
	if err != nil {
		t.Fatalf("resume with approved review: %v", err)
	}
	if state.Engaged {
		t.Fatal("approved review did not clear emergency stop")
	}
	if request.ApprovalSourceID != pending.ApprovalSourceID || request.ApprovalBindingDigest != pending.ApprovalBindingDigest {
		t.Fatalf("authorization request was not bound to the review: %#v", request)
	}
}

func TestApproveAndResumeIsIdempotentAfterTheExactOwnerReview(t *testing.T) {
	service := newTestService(t)
	service.WithExecutionAuthorizer(allowExactControlAuthorization(service.now))
	service.WithControlReviewRepository(newMemoryControlReviews())
	if _, err := service.EngageEmergencyStop("test stop", service.owner); err != nil {
		t.Fatalf("engage emergency stop: %v", err)
	}
	pending, err := service.RequestResumeApproval(context.Background(), service.owner)
	if err != nil {
		t.Fatalf("request resume approval: %v", err)
	}

	state, err := service.ApproveAndResume(context.Background(), service.owner, pending.ReviewItemID, "Owner reviewed current stop")
	if err != nil {
		t.Fatalf("approve and resume: %v", err)
	}
	if state.Engaged {
		t.Fatal("approved owner review did not clear emergency stop")
	}
	state, err = service.ApproveAndResume(context.Background(), service.owner, pending.ReviewItemID, "retry")
	if err != nil {
		t.Fatalf("idempotent retry after approved review: %v", err)
	}
	if state.Engaged {
		t.Fatal("idempotent retry re-engaged the emergency stop")
	}
}

func TestApproveAndResumeDoesNotApproveAnUnrelatedReview(t *testing.T) {
	service := newTestService(t)
	reviews := newMemoryControlReviews()
	service.WithControlReviewRepository(reviews)
	if _, err := service.EngageEmergencyStop("test stop", service.owner); err != nil {
		t.Fatalf("engage emergency stop: %v", err)
	}
	other, err := reviews.CreateControlReview(service.owner, ControlReviewItem{
		ID:            "11111111-1111-4111-8111-111111111111",
		TaskID:        "unrelated-task",
		OwnerIdentity: service.owner,
		Request:       "Unrelated review item",
		Reason:        "An unrelated owner review",
		Status:        "open",
		CreatedAt:     service.now().UTC(),
	})
	if err != nil {
		t.Fatalf("create unrelated review: %v", err)
	}
	_, err = service.ApproveAndResume(context.Background(), service.owner, other.ID, "approve")
	if !errors.Is(err, ErrAuthorizationMismatch) {
		t.Fatalf("ApproveAndResume error = %v, want ErrAuthorizationMismatch", err)
	}
	stored, err := reviews.FindControlReview(service.owner, other.ID)
	if err != nil {
		t.Fatalf("read unrelated review: %v", err)
	}
	if stored.Status != "open" {
		t.Fatalf("unrelated review status = %q, want open", stored.Status)
	}
	if !service.Control().EmergencyStop() {
		t.Fatal("unrelated approval cleared the emergency stop")
	}
}
