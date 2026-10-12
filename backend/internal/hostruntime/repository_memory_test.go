package hostruntime

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
)

type memoryRepository struct {
	mu   sync.Mutex
	jobs map[uuid.UUID]Job
}

func newMemoryRepository() *memoryRepository { return &memoryRepository{jobs: map[uuid.UUID]Job{}} }

func (r *memoryRepository) Create(ctx context.Context, job Job) (*Job, error) {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
	if job.ApprovalExpiresAt.IsZero() {
		return nil, ErrInvalidTask
	}
	if job.ID == uuid.Nil {
		job.ID = uuid.New()
	}
	if job.ApprovalDigest == "" {
		job.ApprovalDigest = approvalDigest(job)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.jobs[job.ID] = job
	return copyJob(job), nil
}

func (r *memoryRepository) Lease(workerID, runtimeID string, now, expires time.Time, digest string, stopRevision uint64) (*Job, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, job := range r.jobs {
		if job.RuntimeID != runtimeID || job.StopRevision == stopRevision {
			continue
		}
		switch {
		case job.Status == StatusLeased && job.StartIntentID != nil && job.ActualStartAckAt == nil:
			r.markNeedsReviewReason(&job, now, reviewReasonStartUnknown)
		case job.Status == StatusLeased && job.ActualStartAckAt != nil:
			if job.CancelRequestedAt == nil {
				job.CancelRequestedAt = timePointer(now)
			}
			job.UpdatedAt = now
		case job.Status == StatusPending || job.Status == StatusLeased:
			job.Status = StatusCancelled
			job.LeaseDigest = ""
			job.LeaseExpires = nil
			job.ReviewReason = reviewReasonStopRevision
			job.UpdatedAt = now
		default:
			continue
		}
		r.jobs[id] = job
	}
	for id, job := range r.jobs {
		if job.RuntimeID != runtimeID {
			continue
		}
		if job.Status == StatusPending && !job.ApprovalExpiresAt.After(now) {
			job.Status = StatusExpired
			job.LeaseDigest = ""
			job.LeaseExpires = nil
			job.ReviewRequiredAt = timePointer(now)
			job.ReviewReason = reviewReasonApprovalExpired
			job.UpdatedAt = now
			r.jobs[id] = job
			continue
		}
		if job.Status == StatusLeased && job.StartIntentID != nil && job.ActualStartAckAt == nil &&
			((job.StartIntentAt != nil && !job.StartIntentAt.After(now.Add(-startIntentTimeout))) || job.LeaseExpires == nil || !job.LeaseExpires.After(now) || !job.ApprovalExpiresAt.After(now)) {
			r.markNeedsReviewReason(&job, now, reviewReasonStartUnknown)
			r.jobs[id] = job
			continue
		}
		if job.Status == StatusLeased && (job.LeaseExpires == nil || !job.LeaseExpires.After(now) || job.LeaseDigest == "" || !job.ApprovalExpiresAt.After(now)) {
			job.Status = StatusNeedsReview
			job.LeaseDigest = ""
			job.LeaseExpires = nil
			job.ReviewRequiredAt = timePointer(now)
			job.ReviewReason = reviewReasonLeaseExpired
			job.UpdatedAt = now
			r.jobs[id] = job
		}
		if job.Status != StatusPending || !job.ApprovalExpiresAt.After(now) {
			continue
		}
		job.Status, job.WorkerID, job.LeaseDigest = StatusLeased, workerID, digest
		if job.ApprovalDigest == "" {
			job.ApprovalDigest = approvalDigest(job)
		} else if job.ApprovalDigest != approvalDigest(job) {
			r.markNeedsReviewReason(&job, now, reviewReasonStartProtocol)
			r.jobs[id] = job
			continue
		}
		job.LeaseExpires = &expires
		job.UpdatedAt = now
		r.jobs[id] = job
		return copyJob(job), nil
	}
	return nil, nil
}

func (r *memoryRepository) ConfirmLeaseContext(ctx context.Context, workerID string, id uuid.UUID, digest string, now time.Time, stopRevision uint64) error {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	job, ok := r.jobs[id]
	if !ok || job.Status != StatusLeased || job.WorkerID != workerID || job.LeaseDigest != digest {
		return ErrStaleLease
	}
	if job.StopRevision != stopRevision {
		if job.StartIntentID != nil && job.ActualStartAckAt == nil {
			r.markNeedsReviewReason(&job, now, reviewReasonStartUnknown)
		} else if job.ActualStartAckAt != nil {
			if job.CancelRequestedAt == nil {
				job.CancelRequestedAt = timePointer(now)
			}
			job.UpdatedAt = now
		} else {
			job.Status = StatusCancelled
			job.LeaseDigest = ""
			job.LeaseExpires = nil
			job.ReviewReason = reviewReasonStopRevision
			job.UpdatedAt = now
		}
		r.jobs[id] = job
		if job.ActualStartAckAt != nil {
			return ErrCancellationRequested
		}
		return ErrStaleLease
	}
	if job.LeaseExpires == nil || !job.LeaseExpires.After(now) || !job.ApprovalExpiresAt.After(now) {
		r.markNeedsReview(&job, now)
		r.jobs[id] = job
		return ErrStaleLease
	}
	if job.CancelRequestedAt != nil {
		return ErrCancellationRequested
	}
	if job.ExecutionConfirmedAt == nil {
		job.ExecutionConfirmedAt = timePointer(now)
		job.UpdatedAt = now
		r.jobs[id] = job
	}
	return nil
}

func (r *memoryRepository) BeginStartIntent(ctx context.Context, workerID string, id uuid.UUID, leaseDigest string, binding StartIntentBinding, gate StartGate, now time.Time) (*StartIntent, error) {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	job, ok := r.jobs[id]
	if !ok {
		return nil, ErrStaleLease
	}
	leaseMatches := job.Status == StatusLeased && job.WorkerID == workerID && job.LeaseDigest == leaseDigest
	if job.StartIntentID != nil {
		if !sameStartIntent(job, workerID, leaseDigest, binding) {
			if job.Status == StatusLeased && job.ActualStartAckAt == nil {
				r.markNeedsReviewReason(&job, now, reviewReasonStartProtocol)
				r.jobs[id] = job
			}
			return nil, ErrStartIntentReplay
		}
		if job.ActualStartAckAt != nil {
			return nil, ErrStartIntentReplay
		}
		if !leaseMatches {
			return nil, ErrStaleLease
		}
		if gateUnavailableOrUnsafe(gate, binding.StopRevision) || job.LeaseExpires == nil || !job.LeaseExpires.After(now) || !job.ApprovalExpiresAt.After(now) || job.CancelRequestedAt != nil || !startIntentFresh(job, now) {
			r.markNeedsReviewReason(&job, now, reviewReasonStartUnknown)
			r.jobs[id] = job
			return nil, ErrStartOutcomeReview
		}
		return startIntentReceipt(job), nil
	}
	if !leaseMatches {
		return nil, ErrStaleLease
	}
	if !gate.Available {
		return nil, ErrStopRevisionUnavailable
	}
	if gate.Active {
		return nil, ErrEmergencyStopped
	}
	if job.StopRevision != binding.StopRevision || gate.CurrentRevision != binding.StopRevision {
		job.Status, job.LeaseDigest, job.LeaseExpires = StatusCancelled, "", nil
		job.ReviewReason, job.UpdatedAt = reviewReasonStopRevision, now
		r.jobs[id] = job
		return nil, ErrStaleLease
	}
	if job.LeaseExpires == nil || !job.LeaseExpires.After(now) || !job.ApprovalExpiresAt.After(now) {
		r.markNeedsReviewReason(&job, now, reviewReasonLeaseExpired)
		r.jobs[id] = job
		return nil, ErrStaleLease
	}
	if job.ExecutionConfirmedAt == nil {
		return nil, ErrStaleLease
	}
	if job.CancelRequestedAt != nil {
		return nil, ErrCancellationRequested
	}
	computed := approvalDigest(job)
	if job.ApprovalDigest == "" {
		job.ApprovalDigest = computed
	}
	if job.ApprovalDigest != computed || binding.ApprovalDigest != job.ApprovalDigest {
		r.markNeedsReviewReason(&job, now, reviewReasonStartProtocol)
		r.jobs[id] = job
		return nil, ErrStartOutcomeReview
	}
	intentAt := now.UTC().Truncate(time.Microsecond)
	stopRevision := binding.StopRevision
	job.StartIntentID = &binding.IntentID
	job.StartIntentAt = &intentAt
	job.StartIntentWorkerID = workerID
	job.StartIntentLeaseDigest = leaseDigest
	job.StartIntentApprovalDigest = binding.ApprovalDigest
	job.StartIntentStopRevision = &stopRevision
	job.UpdatedAt = intentAt
	r.jobs[id] = job
	return startIntentReceipt(job), nil
}

func (r *memoryRepository) AcknowledgeActualStart(ctx context.Context, workerID string, id uuid.UUID, leaseDigest string, binding StartIntentBinding, gate StartGate, now time.Time) error {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	job, ok := r.jobs[id]
	if !ok {
		return ErrStaleLease
	}
	if !sameStartIntent(job, workerID, leaseDigest, binding) {
		if job.Status == StatusLeased && job.WorkerID == workerID && job.LeaseDigest == leaseDigest && job.ActualStartAckAt == nil {
			r.markNeedsReviewReason(&job, now, reviewReasonStartProtocol)
			r.jobs[id] = job
			return ErrStartOutcomeReview
		}
		return ErrStaleLease
	}
	if job.ActualStartAckAt != nil {
		return nil
	}
	if job.Status != StatusLeased || job.WorkerID != workerID || job.LeaseDigest != leaseDigest ||
		gateUnavailableOrUnsafe(gate, binding.StopRevision) || job.LeaseExpires == nil || !job.LeaseExpires.After(now) ||
		!job.ApprovalExpiresAt.After(now) || job.CancelRequestedAt != nil || !startIntentFresh(job, now) || job.ApprovalDigest != approvalDigest(job) {
		r.markNeedsReviewReason(&job, now, reviewReasonStartUnknown)
		r.jobs[id] = job
		return ErrStartOutcomeReview
	}
	ackAt := now.UTC().Truncate(time.Microsecond)
	job.ActualStartAckAt = &ackAt
	job.UpdatedAt = ackAt
	r.jobs[id] = job
	return nil
}

func (r *memoryRepository) ReportStartOutcomeUnknown(ctx context.Context, workerID string, id uuid.UUID, leaseDigest string, binding StartIntentBinding, now time.Time) (bool, error) {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return false, err
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	job, ok := r.jobs[id]
	if !ok || !sameStartIntent(job, workerID, leaseDigest, binding) {
		return false, ErrStaleLease
	}
	if job.ActualStartAckAt != nil {
		return true, nil
	}
	if job.Status == StatusLeased {
		r.markNeedsReviewReason(&job, now, reviewReasonStartUnknown)
		r.jobs[id] = job
		return false, nil
	}
	if job.Status == StatusNeedsReview {
		return false, nil
	}
	return false, ErrStaleLease
}

func (r *memoryRepository) CancelTask(ctx context.Context, ownerIdentity, taskID string, jobID uuid.UUID, now time.Time) (*Job, bool, error) {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return nil, false, err
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, job := range r.jobs {
		if job.OwnerIdentity != ownerIdentity || job.TaskID != taskID || (jobID != uuid.Nil && id != jobID) {
			continue
		}
		switch {
		case job.Status == StatusPending:
			job.Status = StatusCancelled
			job.LeaseDigest = ""
			job.LeaseExpires = nil
			job.UpdatedAt = now
			r.jobs[id] = job
		case job.Status == StatusLeased && job.StartIntentID != nil && job.ActualStartAckAt == nil:
			r.markNeedsReviewReason(&job, now, reviewReasonStartUnknown)
			if job.CancelRequestedAt == nil {
				job.CancelRequestedAt = timePointer(now)
			}
			r.jobs[id] = job
		case job.Status == StatusLeased && job.ActualStartAckAt != nil:
			if job.CancelRequestedAt == nil {
				job.CancelRequestedAt = timePointer(now)
				job.UpdatedAt = now
				r.jobs[id] = job
			}
		case job.Status == StatusLeased:
			job.Status = StatusCancelled
			job.LeaseDigest = ""
			job.LeaseExpires = nil
			job.UpdatedAt = now
			r.jobs[id] = job
		}
		return copyJob(job), job.Status == StatusCancelled, nil
	}
	return nil, false, ErrJobNotFound
}

func (r *memoryRepository) Complete(workerID string, id uuid.UUID, digest string, completion Completion, now time.Time) (*Job, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	job, ok := r.jobs[id]
	if !ok || job.Status != StatusLeased || job.WorkerID != workerID || job.LeaseDigest != digest {
		return nil, ErrStaleLease
	}
	if job.LeaseExpires == nil || !job.LeaseExpires.After(now) {
		r.markNeedsReview(&job, now)
		r.jobs[id] = job
		return nil, ErrStaleLease
	}
	if job.ActualStartAckAt == nil {
		reason := reviewReasonStartProtocol
		if job.StartIntentID != nil {
			reason = reviewReasonStartUnknown
		}
		r.markNeedsReviewReason(&job, now, reason)
		r.jobs[id] = job
		return nil, ErrStartOutcomeReview
	}
	if completion.TerminationVerified && !completion.CancellationRequested {
		return nil, ErrInvalidTask
	}
	if completion.CancellationRequested {
		if job.CancelRequestedAt == nil {
			return nil, ErrInvalidTask
		}
		job.Output, job.Error, job.ExitCode = completion.Output, completion.Error, &completion.ExitCode
		if completion.TerminationVerified {
			job.Status, job.CompletedAt = StatusCancelled, &now
			job.LeaseDigest, job.LeaseExpires = "", nil
		} else {
			job.Status = StatusNeedsReview
			job.LeaseDigest, job.LeaseExpires = "", nil
			job.ReviewRequiredAt = timePointer(now)
			job.ReviewReason = reviewReasonCancelUnverified
		}
		job.UpdatedAt = now
		r.jobs[id] = job
		return copyJob(job), nil
	}
	if job.CancelRequestedAt != nil {
		return nil, ErrCancellationRequested
	}
	job.Status, job.Output, job.Error = StatusCompleted, completion.Output, completion.Error
	job.ExitCode, job.CompletedAt = &completion.ExitCode, &now
	job.LeaseDigest, job.LeaseExpires = "", nil
	job.UpdatedAt = now
	r.jobs[id] = job
	return copyJob(job), nil
}

func (r *memoryRepository) markNeedsReview(job *Job, now time.Time) {
	r.markNeedsReviewReason(job, now, reviewReasonLeaseExpired)
}

func (r *memoryRepository) markNeedsReviewReason(job *Job, now time.Time, reason string) {
	job.Status = StatusNeedsReview
	job.LeaseDigest = ""
	job.LeaseExpires = nil
	job.ReviewRequiredAt = timePointer(now)
	job.ReviewReason = reason
	job.UpdatedAt = now
}

func timePointer(value time.Time) *time.Time { return &value }

func (r *memoryRepository) ListCompletedUnreconciled(limit int) ([]Job, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	jobs := make([]Job, 0)
	for _, job := range r.jobs {
		if job.Status == StatusCompleted && job.ReconciledAt == nil {
			jobs = append(jobs, job)
			if limit > 0 && len(jobs) >= limit {
				break
			}
		}
	}
	return jobs, nil
}

func (r *memoryRepository) ListCancelledUnreconciled(limit int) ([]Job, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	jobs := make([]Job, 0)
	for _, job := range r.jobs {
		if job.Status == StatusCancelled && job.ReconciledAt == nil {
			jobs = append(jobs, job)
		}
	}
	sort.Slice(jobs, func(i, j int) bool {
		if jobs[i].CompletedAt != nil && jobs[j].CompletedAt != nil && !jobs[i].CompletedAt.Equal(*jobs[j].CompletedAt) {
			return jobs[i].CompletedAt.Before(*jobs[j].CompletedAt)
		}
		if jobs[i].UpdatedAt.Equal(jobs[j].UpdatedAt) {
			return jobs[i].ID.String() < jobs[j].ID.String()
		}
		return jobs[i].UpdatedAt.Before(jobs[j].UpdatedAt)
	})
	if limit <= 0 {
		limit = 20
	}
	if len(jobs) > limit {
		jobs = jobs[:limit]
	}
	return jobs, nil
}

func (r *memoryRepository) ListReviewRequiredUnreconciled(limit int) ([]Job, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	jobs := make([]Job, 0)
	for _, job := range r.jobs {
		if (job.Status == StatusExpired || job.Status == StatusNeedsReview) && job.ReviewRequiredAt != nil && job.ReconciledAt == nil {
			jobs = append(jobs, job)
		}
	}
	sort.Slice(jobs, func(i, j int) bool {
		if jobs[i].ReviewRequiredAt.Equal(*jobs[j].ReviewRequiredAt) {
			return jobs[i].ID.String() < jobs[j].ID.String()
		}
		return jobs[i].ReviewRequiredAt.Before(*jobs[j].ReviewRequiredAt)
	})
	if limit <= 0 {
		limit = 20
	}
	if len(jobs) > limit {
		jobs = jobs[:limit]
	}
	return jobs, nil
}

func (r *memoryRepository) MarkReconciled(id uuid.UUID, at time.Time) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	job, ok := r.jobs[id]
	if !ok || job.ReconciledAt != nil || !reconcilableStatus(job) {
		return false, nil
	}
	job.ReconciledAt = &at
	r.jobs[id] = job
	return true, nil
}

func reconcilableStatus(job Job) bool {
	return job.Status == StatusCompleted || job.Status == StatusCancelled ||
		((job.Status == StatusExpired || job.Status == StatusNeedsReview) && job.ReviewRequiredAt != nil)
}

func copyJob(job Job) *Job { return &job }
