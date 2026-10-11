package task

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/safety"

	"github.com/google/uuid"
)

const (
	taskOperationLeaseDuration     = 2 * time.Minute
	taskOperationHeartbeatInterval = 20 * time.Second
)

type taskOperationFunc func(IntakeRequest) (*CompletionPlan, error)

func (s *service) withTaskOperation(request IntakeRequest, mode string, execute taskOperationFunc) (*CompletionPlan, error) {
	ctx := taskExecutionContext(request)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.stateRepository == nil {
		return nil, fmt.Errorf("task operation persistence is not configured")
	}
	request.IdempotencyKey = strings.TrimSpace(request.IdempotencyKey)
	if request.IdempotencyKey == "" {
		request.IdempotencyKey = uuid.NewString()
	}
	ownerIdentity := taskStateOwnerIdentity(request.OwnerIdentity)
	digest, err := ReviewRequestDigest(ownerIdentity, request)
	if err != nil {
		return nil, err
	}
	leaseOwner := "task-worker:" + uuid.NewString()
	admissionRepo, cancelAdmission, err := s.taskOperationStorage(ctx, request.ExecutionContext != nil)
	if err != nil {
		return nil, err
	}
	claim, err := admissionRepo.ClaimTaskOperation(
		ownerIdentity,
		request.IdempotencyKey,
		digest,
		mode,
		leaseOwner,
		time.Now().UTC(),
		taskOperationLeaseDuration,
	)
	cancelAdmission()
	if err != nil {
		return nil, err
	}
	switch claim.Disposition {
	case TaskOperationReplay:
		if strings.TrimSpace(claim.Operation.TaskPlanID) == "" {
			return nil, ErrTaskOperationNeedsReview
		}
		replayRepo, cancelReplay, scopeErr := s.taskOperationStorage(ctx, request.ExecutionContext != nil)
		if scopeErr != nil {
			return nil, scopeErr
		}
		plan, findErr := replayRepo.FindCompletionPlan(ownerIdentity, claim.Operation.TaskPlanID)
		cancelReplay()
		if findErr != nil {
			return plan, errors.Join(ErrTaskOperationNeedsReview, fmt.Errorf("read replayed task operation result: %w", findErr))
		}
		if plan == nil {
			return nil, ErrTaskOperationNeedsReview
		}
		return plan, ctx.Err()
	case TaskOperationInProgress:
		return nil, ErrTaskOperationInProgress
	case TaskOperationNeedsReview:
		if err := s.ensureTaskOperationReview(request, claim.Operation, claim.Operation.LastError); err != nil {
			return nil, fmt.Errorf("surface uncertain task operation for review: %w", err)
		}
		return nil, ErrTaskOperationNeedsReview
	case TaskOperationAcquired:
		// Continue below with the fenced claim.
	default:
		return nil, ErrTaskStateConflict
	}

	request.operationID = claim.Operation.ID.String()
	stopHeartbeat := s.startTaskOperationHeartbeat(claim, leaseOwner)
	defer stopHeartbeat()
	var plan *CompletionPlan
	executeErr := ctx.Err()
	if executeErr == nil {
		plan, executeErr = execute(request)
	}
	leaseLost := stopHeartbeat()
	terminalContext := ctx
	if ctx.Err() != nil || executeErr != nil || leaseLost || plan == nil {
		// Reconciliation outlives the caller, but never has an unlimited SQL wait.
		terminalContext = context.WithoutCancel(ctx)
	}
	terminalRepo, cancelTerminal, scopeErr := s.taskOperationStorage(terminalContext, request.ExecutionContext != nil)
	if scopeErr != nil {
		markTaskPlanUnconfirmed(plan)
		return plan, errors.Join(ErrTaskOperationNeedsReview, executeErr, ctx.Err(), scopeErr)
	}
	defer cancelTerminal()
	if cancelErr := ctx.Err(); cancelErr != nil {
		executeErr = errors.Join(ErrTaskOperationNeedsReview, executeErr, cancelErr)
		if plan != nil {
			plan.CompletionStatus = "review_required"
			plan.ValidationResult.Passed = false
			plan.ValidationResult.Status = "blocked"
			plan.ValidationResult.NextAction = "inspect cancellation and retained execution evidence before retrying"
			plan.RetryPolicy.RetryAvailable = false
			plan.ExecutionResult = cancelledTaskExecution(plan.ExecutionResult, plan, request, time.Now().UTC())
			// Cancellation cannot erase an acknowledged runtime receipt or usage.
			// This reconciliation write does not complete or release the operation.
			if err := s.addLogWithRepository(terminalRepo, *plan); err != nil {
				executeErr = errors.Join(executeErr, fmt.Errorf("retain cancelled task evidence: %w", err))
			}
		}
	}
	if executeErr != nil {
		markTaskPlanUnconfirmed(plan)
		executeErr = errors.Join(ErrTaskOperationNeedsReview, executeErr)
		reason := "task operation stopped before a durable result was confirmed: " + safety.RedactSecrets(executeErr.Error())
		marked, markErr := terminalRepo.MarkTaskOperationNeedsReview(
			ownerIdentity, claim.Operation.ID, leaseOwner, claim.Operation.LeaseGeneration, reason, time.Now().UTC(),
		)
		if markErr != nil {
			return plan, errors.Join(executeErr, fmt.Errorf("mark uncertain task operation: %w", markErr))
		}
		if marked {
			if reviewErr := s.ensureTaskOperationReviewWithRepository(terminalRepo, request, claim.Operation, reason); reviewErr != nil {
				return plan, errors.Join(executeErr, fmt.Errorf("create task operation review: %w", reviewErr))
			}
		}
		return plan, executeErr
	}
	if leaseLost || plan == nil {
		markTaskPlanUnconfirmed(plan)
		reason := "task operation lease was lost before its durable result could be fenced"
		marked, markErr := terminalRepo.MarkTaskOperationNeedsReview(
			ownerIdentity, claim.Operation.ID, leaseOwner, claim.Operation.LeaseGeneration,
			reason, time.Now().UTC(),
		)
		if markErr != nil {
			return plan, errors.Join(ErrTaskOperationNeedsReview, fmt.Errorf("mark lost task operation lease: %w", markErr))
		}
		if marked {
			if reviewErr := s.ensureTaskOperationReviewWithRepository(terminalRepo, request, claim.Operation, reason); reviewErr != nil {
				return plan, errors.Join(ErrTaskOperationNeedsReview, fmt.Errorf("create task operation review: %w", reviewErr))
			}
		}
		return plan, ErrTaskOperationNeedsReview
	}
	completed, completeErr := terminalRepo.CompleteTaskOperation(
		ownerIdentity,
		claim.Operation.ID,
		leaseOwner,
		claim.Operation.LeaseGeneration,
		plan.ID,
		time.Now().UTC(),
	)
	if completeErr != nil {
		markTaskPlanUnconfirmed(plan)
		reason := "task operation completion could not be confirmed: " + safety.RedactSecrets(completeErr.Error())
		marked, markErr := terminalRepo.MarkTaskOperationNeedsReview(
			ownerIdentity, claim.Operation.ID, leaseOwner, claim.Operation.LeaseGeneration, reason, time.Now().UTC(),
		)
		resultErr := errors.Join(ErrTaskOperationNeedsReview, completeErr, markErr)
		if marked && markErr == nil {
			resultErr = errors.Join(resultErr, s.ensureTaskOperationReviewWithRepository(terminalRepo, request, claim.Operation, reason))
		}
		return plan, resultErr
	}
	if !completed {
		markTaskPlanUnconfirmed(plan)
		return plan, ErrTaskOperationNeedsReview
	}
	// Return the authoritative persisted representation on the first delivery
	// as well as on replay. PostgreSQL normalizes timestamp precision and JSON
	// values, so returning the pre-storage object here would make the same
	// completed operation observably different on a later replay.
	durablePlan, findErr := terminalRepo.FindCompletionPlan(ownerIdentity, plan.ID)
	if findErr != nil {
		markTaskPlanUnconfirmed(plan)
		return plan, errors.Join(ErrTaskOperationNeedsReview, fmt.Errorf("read completed task operation result: %w", findErr))
	}
	if durablePlan == nil {
		markTaskPlanUnconfirmed(plan)
		return plan, ErrTaskOperationNeedsReview
	}
	// Cancellation cannot reverse an acknowledged completion, but it must not
	// disappear from the caller's result during the final readback boundary.
	return durablePlan, ctx.Err()
}

// An unconfirmed persistence outcome must retain evidence without promising
// completion or permission to repeat an already-entered effect.
func markTaskPlanUnconfirmed(plan *CompletionPlan) {
	if plan == nil {
		return
	}
	plan.CompletionStatus = "review_required"
	plan.ValidationResult.Passed = false
	plan.ValidationResult.Status = "blocked"
	plan.ValidationResult.NextAction = "inspect retained execution evidence and persistence outcome before retrying"
	plan.RetryPolicy.RetryAvailable = false
}

func (s *service) ensureTaskOperationReview(request IntakeRequest, operation models.TaskOperationRecord, reason string) error {
	repo, cancel, err := s.taskOperationStorage(context.WithoutCancel(taskExecutionContext(request)), request.ExecutionContext != nil)
	if err != nil {
		return err
	}
	defer cancel()
	return s.ensureTaskOperationReviewWithRepository(repo, request, operation, reason)
}

func (s *service) ensureTaskOperationReviewWithRepository(repo TaskStateRepository, request IntakeRequest, operation models.TaskOperationRecord, reason string) error {
	if repo == nil || operation.ID == uuid.Nil {
		return fmt.Errorf("task operation review persistence is unavailable")
	}
	ownerIdentity := taskStateOwnerIdentity(request.OwnerIdentity)
	request.OwnerIdentity = ownerIdentity
	request.IdempotencyKey = ""
	request.ExecuteAllowed = false
	request.HumanApproved = false
	request.ApprovalNote = ""
	request.ApprovalSourceID = ""
	request.operationID = ""
	request.reviewItemID = ""
	request.ExecutionContext = nil

	reviewID := uuid.NewSHA1(uuid.NameSpaceOID, []byte("hai-task-operation-review:"+operation.ID.String())).String()
	// Recovery surfaces an immutable pending request, not a refreshed approval.
	// In particular, legacy reviews must never acquire a snapshot on replay.
	if existing, existingErr := repo.FindReviewItem(ownerIdentity, reviewID); existingErr == nil {
		if existing == nil {
			return fmt.Errorf("task operation review lookup returned no durable item")
		}
		return nil
	} else if !errors.Is(existingErr, ErrTaskStateNotFound) {
		return fmt.Errorf("inspect existing task operation review: %w", existingErr)
	}
	priority := "normal"
	if operation.Mode == "run" {
		priority = "high"
	}
	reason = taskOperationReviewReason(reason)
	reviewReason := sanitizeTaskOperationalText(
		fmt.Sprintf(
			"Task operation %s (%s) has an uncertain outcome: %s. Inspect the audit evidence before approving a new attempt; approval creates a separate durable operation and never resumes or rewrites the prior attempt.",
			operation.ID, operation.Mode, reason,
		),
		taskStateMaximumReasonRunes,
	)
	reviewCreatedAt := operation.CreatedAt
	if reviewCreatedAt.IsZero() {
		reviewCreatedAt = time.Now().UTC()
	}
	_, err := s.addReviewItemWithRepositoryContext(context.WithoutCancel(taskExecutionContext(request)), repo, ReviewQueueItem{
		ID:        reviewID,
		TaskID:    "operation:" + operation.ID.String(),
		Request:   request,
		Reason:    reviewReason,
		Priority:  priority,
		Status:    "needs_review",
		CreatedAt: normalizedTaskOperationTime(reviewCreatedAt),
	})
	return err
}

// startTaskOperationHeartbeat maintains the direct synchronous operation lease
// while planning, model calls, controlled tools, and validation are running.
// The returned closure stops the heartbeat and reports whether ownership was
// lost. Completion still performs a generation-checked compare-and-set.
func (s *service) startTaskOperationHeartbeat(claim TaskOperationClaim, leaseOwner string) func() bool {
	return s.startTaskOperationHeartbeatWithInterval(claim, leaseOwner, taskOperationHeartbeatInterval)
}

func (s *service) startTaskOperationHeartbeatWithInterval(claim TaskOperationClaim, leaseOwner string, interval time.Duration) func() bool {
	heartbeatContext, cancelHeartbeat := context.WithCancel(context.Background())
	done := make(chan struct{})
	stopped := make(chan struct{})
	lost := make(chan struct{}, 1)
	go func() {
		defer close(stopped)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case now := <-ticker.C:
				repo, cancelWrite, err := s.taskOperationStorage(heartbeatContext, false)
				if err != nil {
					lost <- struct{}{}
					return
				}
				owned, err := repo.HeartbeatTaskOperation(
					claim.Operation.OwnerIdentity,
					claim.Operation.ID,
					leaseOwner,
					claim.Operation.LeaseGeneration,
					now.UTC(),
				)
				cancelWrite()
				if err != nil || !owned {
					select {
					case lost <- struct{}{}:
					default:
					}
					return
				}
			}
		}
	}()
	var stopOnce sync.Once
	var leaseLost bool
	return func() bool {
		stopOnce.Do(func() {
			close(done)
			cancelHeartbeat()
			<-stopped
			select {
			case <-lost:
				leaseLost = true
			default:
			}
		})
		return leaseLost
	}
}
