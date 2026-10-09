package task

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/models"
	"github.com/google/uuid"
)

type taskTransitionStorage struct {
	*MemoryTaskStateRepository
	cancel   context.CancelFunc
	boundary string
	claim    TaskOperationClaim
	plan     *CompletionPlan
	readErr  error
}

func (r *taskTransitionStorage) WithTaskStateContext(ctx context.Context) (TaskStateRepository, error) {
	return r, ctx.Err()
}

func (r *taskTransitionStorage) ClaimTaskOperation(owner, key, digest, mode, lease string, now time.Time, duration time.Duration) (TaskOperationClaim, error) {
	if strings.HasPrefix(r.boundary, "replay") {
		return TaskOperationClaim{Operation: models.TaskOperationRecord{TaskPlanID: r.plan.ID}, Disposition: TaskOperationReplay}, nil
	}
	claim, err := r.MemoryTaskStateRepository.ClaimTaskOperation(owner, key, digest, mode, lease, now, duration)
	r.claim = claim
	if err == nil && r.boundary == "after_claim" {
		r.cancel()
	}
	return claim, err
}

func (r *taskTransitionStorage) CompleteTaskOperation(owner string, id uuid.UUID, lease string, generation int64, planID string, now time.Time) (bool, error) {
	return r.MemoryTaskStateRepository.CompleteTaskOperation(owner, id, lease, generation, planID, now)
}

func (r *taskTransitionStorage) FindCompletionPlan(owner, id string) (*CompletionPlan, error) {
	switch r.boundary {
	case "replay_empty":
		return nil, nil
	case "replay_error":
		return nil, r.readErr
	case "replay_cancel", "readback_cancel":
		r.cancel()
		return r.plan, nil
	}
	return r.MemoryTaskStateRepository.FindCompletionPlan(owner, id)
}

func TestTaskOperationTransitionCancellationAndEmptyReplay(t *testing.T) {
	for _, boundary := range []string{"after_claim", "replay_empty", "replay_error", "replay_cancel", "readback_cancel"} {
		t.Run(boundary, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			plan := &CompletionPlan{ID: uuid.NewString(), OwnerIdentity: "alice"}
			readErr := errors.New("controlled readback failure")
			repo := &taskTransitionStorage{MemoryTaskStateRepository: NewMemoryTaskStateRepository(), cancel: cancel, boundary: boundary, plan: plan, readErr: readErr}
			svc := &service{stateRepository: repo}
			executed := 0
			returned, err := svc.withTaskOperation(IntakeRequest{ExecutionContext: ctx, OwnerIdentity: "alice", Request: "Read notes", IdempotencyKey: "transition-" + boundary}, "plan", func(IntakeRequest) (*CompletionPlan, error) {
				executed++
				return plan, nil
			})
			if boundary == "replay_empty" {
				if returned != nil || !errors.Is(err, ErrTaskOperationNeedsReview) || executed != 0 {
					t.Fatalf("empty replay was acknowledged: plan=%v err=%v calls=%d", returned, err, executed)
				}
				return
			}
			if boundary == "replay_error" {
				if !errors.Is(err, readErr) || !errors.Is(err, ErrTaskOperationNeedsReview) || executed != 0 {
					t.Fatalf("replay lost storage error or dispatched work: %v calls=%d", err, executed)
				}
				return
			}
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("transition lost cancellation identity: %v", err)
			}
			if boundary == "after_claim" {
				if executed != 0 || !errors.Is(err, ErrTaskOperationNeedsReview) {
					t.Fatalf("cancelled acquired claim dispatched work: calls=%d err=%v", executed, err)
				}
				items, listErr := repo.ListReviewItems("alice", 10)
				if listErr != nil || len(items) != 1 || items[0].Request.ExecutionContext != nil {
					t.Fatal("cancelled claimed operation was not durably surfaced without dead context")
				}
				return
			}
			if returned != plan || (boundary == "replay_cancel" && executed != 0) || (boundary == "readback_cancel" && executed != 1) {
				t.Fatal("cancellation erased returned evidence or replayed effects")
			}
			if boundary == "readback_cancel" {
				digest, digestErr := ReviewRequestDigest("alice", IntakeRequest{OwnerIdentity: "alice", Request: "Read notes", IdempotencyKey: "transition-" + boundary})
				if digestErr != nil {
					t.Fatal(digestErr)
				}
				claim, claimErr := repo.MemoryTaskStateRepository.ClaimTaskOperation("alice", "transition-"+boundary, digest, "plan", "another-worker", time.Now(), time.Minute)
				if claimErr != nil || claim.Disposition != TaskOperationReplay {
					t.Fatalf("caller cancellation reversed acknowledged completion: %#v %v", claim, claimErr)
				}
			}
		})
	}
}
