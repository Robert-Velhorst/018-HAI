package task

import (
	"context"
	"fmt"
	"strings"
	"time"

	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/safety"

	"github.com/google/uuid"
)

const (
	taskOperationLeaseDuration     = 2 * time.Minute
	taskOperationHeartbeatInterval = 20 * time.Second
	taskOperationWriteTimeout      = 10 * time.Second
)

type taskOperationFunc func(IntakeRequest) (*CompletionPlan, error)

func claimTaskOperationContext(
	ctx context.Context,
	repository TaskStateRepository,
	ownerIdentity, idempotencyKey, requestDigest, mode, leaseOwner string,
	now time.Time,
	leaseDuration time.Duration,
) (TaskOperationClaim, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return TaskOperationClaim{}, err
	}
	if contextual, ok := repository.(ContextTaskOperationClaimer); ok {
		return contextual.ClaimTaskOperationContext(
			ctx, ownerIdentity, idempotencyKey, requestDigest, mode, leaseOwner, now, leaseDuration,
		)
	}
	return repository.ClaimTaskOperation(
		ownerIdentity, idempotencyKey, requestDigest, mode, leaseOwner, now, leaseDuration,
	)
}

func heartbeatTaskOperationContext(
	ctx context.Context,
	repository TaskStateRepository,
	ownerIdentity string,
	operationID uuid.UUID,
	leaseOwner string,
	leaseGeneration int64,
	now time.Time,
) (bool, error) {
	if contextual, ok := repository.(ContextTaskOperationHeartbeater); ok {
		return contextual.HeartbeatTaskOperationContext(ctx, ownerIdentity, operationID, leaseOwner, leaseGeneration, now)
	}
	return repository.HeartbeatTaskOperation(ownerIdentity, operationID, leaseOwner, leaseGeneration, now)
}

func completeTaskOperationContext(
	ctx context.Context,
	repository TaskStateRepository,
	ownerIdentity string,
	operationID uuid.UUID,
	leaseOwner string,
	leaseGeneration int64,
	taskPlanID string,
	now time.Time,
) (bool, error) {
	if contextual, ok := repository.(ContextTaskOperationCompleter); ok {
		return contextual.CompleteTaskOperationContext(ctx, ownerIdentity, operationID, leaseOwner, leaseGeneration, taskPlanID, now)
	}
	return repository.CompleteTaskOperation(ownerIdentity, operationID, leaseOwner, leaseGeneration, taskPlanID, now)
}

func markTaskOperationNeedsReviewContext(
	ctx context.Context,
	repository TaskStateRepository,
	ownerIdentity string,
	operationID uuid.UUID,
	leaseOwner string,
	leaseGeneration int64,
	reason string,
	now time.Time,
) (bool, error) {
	if contextual, ok := repository.(ContextTaskOperationReviewer); ok {
		return contextual.MarkTaskOperationNeedsReviewContext(ctx, ownerIdentity, operationID, leaseOwner, leaseGeneration, reason, now)
	}
	return repository.MarkTaskOperationNeedsReview(ownerIdentity, operationID, leaseOwner, leaseGeneration, reason, now)
}

func cancelTaskOperationContext(
	ctx context.Context,
	repository TaskStateRepository,
	ownerIdentity string,
	operationID uuid.UUID,
	leaseOwner string,
	leaseGeneration int64,
	reason string,
	now time.Time,
) (bool, error) {
	if contextual, ok := repository.(ContextTaskOperationCanceler); ok {
		return contextual.CancelTaskOperationContext(ctx, ownerIdentity, operationID, leaseOwner, leaseGeneration, reason, now)
	}
	if canceler, ok := repository.(TaskOperationCanceler); ok {
		return canceler.CancelTaskOperation(ownerIdentity, operationID, leaseOwner, leaseGeneration, reason, now)
	}
	return false, fmt.Errorf("task operation cancellation persistence is not configured")
}

func findCompletionPlanContext(ctx context.Context, repository TaskStateRepository, ownerIdentity, taskPlanID string) (*CompletionPlan, error) {
	if contextual, ok := repository.(ContextCompletionPlanFinder); ok {
		return contextual.FindCompletionPlanContext(ctx, ownerIdentity, taskPlanID)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	plan, err := repository.FindCompletionPlan(ownerIdentity, taskPlanID)
	if contextErr := ctx.Err(); contextErr != nil {
		return nil, contextErr
	}
	return plan, err
}

func taskOperationWriteContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), taskOperationWriteTimeout)
}

func (s *service) withTaskOperation(request IntakeRequest, mode string, execute taskOperationFunc) (*CompletionPlan, error) {
	if s.stateRepository == nil {
		return nil, fmt.Errorf("task operation persistence is not configured")
	}
	request.IdempotencyKey = strings.TrimSpace(request.IdempotencyKey)
	if request.IdempotencyKey == "" {
		request.IdempotencyKey = uuid.NewString()
	}
	ownerIdentity := taskStateOwnerIdentity(request.OwnerIdentity)
	requestContext := taskExecutionContext(request)
	if err := requestContext.Err(); err != nil {
		return nil, err
	}
	digest, err := ReviewRequestDigest(ownerIdentity, request)
	if err != nil {
		return nil, err
	}
	leaseOwner := "task-worker:" + uuid.NewString()
	claim, err := claimTaskOperationContext(
		requestContext,
		s.stateRepository,
		ownerIdentity,
		request.IdempotencyKey,
		digest,
		mode,
		leaseOwner,
		time.Now().UTC(),
		taskOperationLeaseDuration,
	)
	if err != nil {
		return nil, err
	}
	if contextErr := requestContext.Err(); contextErr != nil {
		if claim.Disposition == TaskOperationAcquired {
			writeContext, cancel := taskOperationWriteContext()
			canceled, cancelErr := cancelTaskOperationContext(
				writeContext,
				s.stateRepository,
				ownerIdentity,
				claim.Operation.ID,
				leaseOwner,
				claim.Operation.LeaseGeneration,
				taskOperationCancellationReason(contextErr),
				time.Now().UTC(),
			)
			cancel()
			if cancelErr != nil {
				return nil, fmt.Errorf("%w: persist pre-execution cancellation: %v", contextErr, cancelErr)
			}
			if !canceled {
				return nil, fmt.Errorf("%w: durable task claim ownership was lost", contextErr)
			}
		}
		return nil, contextErr
	}
	switch claim.Disposition {
	case TaskOperationReplay:
		if strings.TrimSpace(claim.Operation.TaskPlanID) == "" {
			return nil, ErrTaskOperationNeedsReview
		}
		plan, findErr := findCompletionPlanContext(requestContext, s.stateRepository, ownerIdentity, claim.Operation.TaskPlanID)
		if findErr != nil {
			return nil, fmt.Errorf("%w: completed task operation has no durable result", ErrTaskOperationNeedsReview)
		}
		return plan, nil
	case TaskOperationInProgress:
		return nil, ErrTaskOperationInProgress
	case TaskOperationNeedsReview:
		if err := s.ensureTaskOperationReview(request, claim.Operation, claim.Operation.LastError); err != nil {
			return nil, fmt.Errorf("surface uncertain task operation for review: %w", err)
		}
		return nil, ErrTaskOperationNeedsReview
	case TaskOperationCanceled:
		return nil, ErrTaskOperationCanceled
	case TaskOperationAcquired:
		// Continue below with the fenced claim.
	default:
		return nil, ErrTaskStateConflict
	}

	request.operationID = claim.Operation.ID.String()
	stopHeartbeat := s.startTaskOperationHeartbeat(claim, leaseOwner)
	plan, executeErr := execute(request)
	leaseLost := stopHeartbeat()
	if executeErr != nil {
		reason := "task operation stopped before a durable result was confirmed: " + safety.RedactSecrets(executeErr.Error())
		writeContext, cancel := taskOperationWriteContext()
		marked, markErr := markTaskOperationNeedsReviewContext(
			writeContext,
			s.stateRepository,
			ownerIdentity, claim.Operation.ID, leaseOwner, claim.Operation.LeaseGeneration, reason, time.Now().UTC(),
		)
		cancel()
		if markErr != nil {
			return nil, fmt.Errorf("%w: mark uncertain task operation: %v", executeErr, markErr)
		}
		if marked {
			if reviewErr := s.ensureTaskOperationReview(request, claim.Operation, reason); reviewErr != nil {
				return nil, fmt.Errorf("%w: create task operation review: %v", executeErr, reviewErr)
			}
		}
		return nil, executeErr
	}
	if leaseLost || plan == nil {
		reason := "task operation lease was lost before its durable result could be fenced"
		writeContext, cancel := taskOperationWriteContext()
		marked, markErr := markTaskOperationNeedsReviewContext(
			writeContext,
			s.stateRepository,
			ownerIdentity, claim.Operation.ID, leaseOwner, claim.Operation.LeaseGeneration,
			reason, time.Now().UTC(),
		)
		cancel()
		if markErr != nil {
			return nil, fmt.Errorf("%w: mark lost task operation lease: %v", ErrTaskOperationNeedsReview, markErr)
		}
		if marked {
			if reviewErr := s.ensureTaskOperationReview(request, claim.Operation, reason); reviewErr != nil {
				return nil, fmt.Errorf("%w: create task operation review: %v", ErrTaskOperationNeedsReview, reviewErr)
			}
		}
		return nil, ErrTaskOperationNeedsReview
	}
	writeContext, cancelWrite := taskOperationWriteContext()
	completed, completeErr := completeTaskOperationContext(
		writeContext,
		s.stateRepository,
		ownerIdentity,
		claim.Operation.ID,
		leaseOwner,
		claim.Operation.LeaseGeneration,
		plan.ID,
		time.Now().UTC(),
	)
	cancelWrite()
	if completeErr != nil {
		reason := "task operation completion could not be confirmed: " + safety.RedactSecrets(completeErr.Error())
		writeContext, cancel := taskOperationWriteContext()
		marked, _ := markTaskOperationNeedsReviewContext(
			writeContext,
			s.stateRepository,
			ownerIdentity, claim.Operation.ID, leaseOwner, claim.Operation.LeaseGeneration, reason, time.Now().UTC(),
		)
		cancel()
		if marked {
			_ = s.ensureTaskOperationReview(request, claim.Operation, reason)
		}
		return nil, completeErr
	}
	if !completed {
		return nil, ErrTaskOperationNeedsReview
	}
	// Return the authoritative persisted representation on the first delivery
	// as well as on replay. PostgreSQL normalizes timestamp precision and JSON
	// values, so returning the pre-storage object here would make the same
	// completed operation observably different on a later replay.
	readContext, cancelRead := taskOperationWriteContext()
	durablePlan, findErr := findCompletionPlanContext(readContext, s.stateRepository, ownerIdentity, plan.ID)
	cancelRead()
	if findErr != nil {
		return nil, fmt.Errorf("%w: completed task operation has no durable result", ErrTaskOperationNeedsReview)
	}
	return durablePlan, nil
}

func taskOperationCancellationReason(err error) string {
	if err == context.DeadlineExceeded {
		return "task operation deadline expired before planning or execution began"
	}
	return "task operation was canceled before planning or execution began"
}

func (s *service) ensureTaskOperationReview(request IntakeRequest, operation models.TaskOperationRecord, reason string) error {
	if s.stateRepository == nil || operation.ID == uuid.Nil {
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

	reviewID := uuid.NewSHA1(uuid.NameSpaceOID, []byte("hai-task-operation-review:"+operation.ID.String())).String()
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
	_, err := s.stateRepository.CreateReviewItem(ownerIdentity, ReviewQueueItem{
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
	done := make(chan struct{})
	stopped := make(chan struct{})
	lost := make(chan struct{}, 1)
	go func() {
		defer close(stopped)
		ticker := time.NewTicker(taskOperationHeartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case now := <-ticker.C:
				writeContext, cancel := taskOperationWriteContext()
				owned, err := heartbeatTaskOperationContext(
					writeContext,
					s.stateRepository,
					claim.Operation.OwnerIdentity,
					claim.Operation.ID,
					leaseOwner,
					claim.Operation.LeaseGeneration,
					now.UTC(),
				)
				cancel()
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
	return func() bool {
		close(done)
		<-stopped
		select {
		case <-lost:
			return true
		default:
			return false
		}
	}
}
