package hostruntime

import (
	"automation-hub-backend/internal/infra"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type gormRepository struct{ db *gorm.DB }

func NewGormRepository(db *gorm.DB) Repository { return &gormRepository{db: db} }

func DefaultRepository() (Repository, error) {
	db, err := infra.GetDefaultDB()
	if err != nil {
		return nil, err
	}
	return NewGormRepository(db), nil
}

func (r *gormRepository) Create(ctx context.Context, job Job) (*Job, error) {
	if job.ApprovalExpiresAt.IsZero() {
		return nil, ErrInvalidTask
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if job.ID == uuid.Nil {
		job.ID = uuid.New()
	}
	if job.CreatedAt.IsZero() {
		job.CreatedAt = time.Now().UTC().Truncate(time.Microsecond)
	}
	if job.ApprovalDigest == "" {
		job.ApprovalDigest = approvalDigest(job)
	}
	if err := r.db.WithContext(ctx).Create(&job).Error; err != nil {
		return nil, err
	}
	return &job, nil
}

func (r *gormRepository) Lease(workerID, runtimeID string, now, expires time.Time, digest string, stopRevision uint64) (*Job, error) {
	var leased *Job
	err := r.db.Transaction(func(tx *gorm.DB) error {
		// A start intent without a timely bridge acknowledgment cannot be
		// replayed. Quarantine it before processing ordinary unstarted work.
		intentReview := map[string]any{
			"status": StatusNeedsReview, "lease_digest": "", "lease_expires": nil,
			"review_required_at": now, "review_reason": reviewReasonStartUnknown, "updated_at": now,
		}
		if err := tx.Model(&Job{}).
			Where("runtime_id = ? AND status = ? AND start_intent_id IS NOT NULL AND actual_start_ack_at IS NULL AND (start_intent_at <= ? OR lease_expires IS NULL OR lease_expires <= ? OR approval_expires_at <= ?)", runtimeID, StatusLeased, now.Add(-startIntentTimeout), now, now).
			Updates(intentReview).Error; err != nil {
			return err
		}
		if err := tx.Model(&Job{}).
			Where("runtime_id = ? AND status = ? AND start_intent_id IS NOT NULL AND actual_start_ack_at IS NULL AND stop_revision <> ?", runtimeID, StatusLeased, stopRevision).
			Updates(intentReview).Error; err != nil {
			return err
		}
		// A stop/clear cycle invalidates every approval stamped before it. This
		// transition is durable, so an offline worker cannot resume old work
		// merely because it missed the active-stop polling window.
		if err := tx.Model(&Job{}).
			Where("runtime_id = ? AND stop_revision <> ? AND (status = ? OR (status = ? AND start_intent_id IS NULL AND actual_start_ack_at IS NULL))", runtimeID, stopRevision, StatusPending, StatusLeased).
			Updates(map[string]any{
				"status":        StatusCancelled,
				"lease_digest":  "",
				"lease_expires": nil,
				"review_reason": reviewReasonStopRevision,
				"updated_at":    now,
			}).Error; err != nil {
			return err
		}
		// Expiration transitions and the claim happen under the same transaction.
		// A stale lease is never considered available work: its execution outcome
		// is unknown, so it must be reviewed instead of replayed.
		if err := tx.Model(&Job{}).
			Where("runtime_id = ? AND status = ? AND approval_expires_at <= ?", runtimeID, StatusPending, now).
			Updates(map[string]any{
				"status":             StatusExpired,
				"lease_digest":       "",
				"lease_expires":      nil,
				"review_required_at": now,
				"review_reason":      reviewReasonApprovalExpired,
				"updated_at":         now,
			}).Error; err != nil {
			return err
		}
		if err := tx.Model(&Job{}).
			Where("runtime_id = ? AND status = ? AND (lease_expires IS NULL OR lease_expires <= ? OR lease_digest = '' OR approval_expires_at <= ?)", runtimeID, StatusLeased, now, now).
			Updates(map[string]any{
				"status":             StatusNeedsReview,
				"lease_digest":       "",
				"lease_expires":      nil,
				"review_required_at": now,
				"review_reason":      reviewReasonLeaseExpired,
				"updated_at":         now,
			}).Error; err != nil {
			return err
		}

		var job Job
		err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("status = ? AND approval_expires_at > ? AND runtime_id = ? AND stop_revision = ?", StatusPending, now, runtimeID, stopRevision).
			Order("created_at").
			First(&job).Error
		if err != nil {
			if err == gorm.ErrRecordNotFound {
				return nil
			}
			return err
		}
		result := tx.Model(&Job{}).Where("id = ? AND status = ? AND approval_expires_at > ?", job.ID, StatusPending, now).Updates(map[string]any{
			"status":        StatusLeased,
			"worker_id":     workerID,
			"lease_digest":  digest,
			"lease_expires": expires,
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("host runtime job changed before lease")
		}
		job.Status, job.WorkerID, job.LeaseDigest = StatusLeased, workerID, digest
		job.LeaseExpires = &expires
		if job.ApprovalDigest == "" {
			job.ApprovalDigest = approvalDigest(job)
			if err := tx.Model(&Job{}).Where("id = ? AND approval_digest = ''", job.ID).Update("approval_digest", job.ApprovalDigest).Error; err != nil {
				return err
			}
		} else if job.ApprovalDigest != approvalDigest(job) {
			if err := tx.Model(&Job{}).Where("id = ? AND status = ?", job.ID, StatusLeased).Updates(map[string]any{
				"status": StatusNeedsReview, "lease_digest": "", "lease_expires": nil,
				"review_required_at": now, "review_reason": reviewReasonStartProtocol, "updated_at": now,
			}).Error; err != nil {
				return err
			}
			return nil
		}
		leased = &job
		return nil
	})
	return leased, err
}

func (r *gormRepository) ConfirmLeaseContext(ctx context.Context, workerID string, id uuid.UUID, digest string, now time.Time, stopRevision uint64) error {
	if ctx == nil {
		ctx = context.Background()
	}
	stale := false
	cancellationRequested := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		uncertainStart := tx.Model(&Job{}).
			Where("id = ? AND status = ? AND worker_id = ? AND lease_digest = ? AND stop_revision <> ? AND start_intent_id IS NOT NULL AND actual_start_ack_at IS NULL", id, StatusLeased, workerID, digest, stopRevision).
			Updates(map[string]any{
				"status": StatusNeedsReview, "lease_digest": "", "lease_expires": nil,
				"review_required_at": now, "review_reason": reviewReasonStartUnknown, "updated_at": now,
			})
		if uncertainStart.Error != nil {
			return uncertainStart.Error
		}
		if uncertainStart.RowsAffected > 0 {
			stale = true
			return nil
		}
		invalidated := tx.Model(&Job{}).
			Where("id = ? AND status = ? AND worker_id = ? AND lease_digest = ? AND stop_revision <> ? AND start_intent_id IS NULL AND actual_start_ack_at IS NULL", id, StatusLeased, workerID, digest, stopRevision).
			Updates(map[string]any{
				"status":        StatusCancelled,
				"lease_digest":  "",
				"lease_expires": nil,
				"review_reason": reviewReasonStopRevision,
				"updated_at":    now,
			})
		if invalidated.Error != nil {
			return invalidated.Error
		}
		if invalidated.RowsAffected > 0 {
			stale = true
			return nil
		}
		var stopChanged Job
		stopErr := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND status = ? AND worker_id = ? AND lease_digest = ? AND stop_revision <> ? AND actual_start_ack_at IS NOT NULL", id, StatusLeased, workerID, digest, stopRevision).
			First(&stopChanged).Error
		if stopErr != nil && !errors.Is(stopErr, gorm.ErrRecordNotFound) {
			return stopErr
		}
		if stopErr == nil {
			request := tx.Model(&Job{}).Where("id = ? AND status = ? AND worker_id = ? AND lease_digest = ? AND stop_revision <> ? AND actual_start_ack_at IS NOT NULL", id, StatusLeased, workerID, digest, stopRevision).
				Where("cancel_requested_at IS NULL").Updates(map[string]any{"cancel_requested_at": now, "updated_at": now})
			if request.Error != nil {
				return request.Error
			}
			cancellationRequested = true
			return nil
		}
		transition := tx.Model(&Job{}).
			Where("id = ? AND status = ? AND worker_id = ? AND lease_digest = ? AND (lease_expires IS NULL OR lease_expires <= ?)", id, StatusLeased, workerID, digest, now).
			Updates(map[string]any{
				"status":             StatusNeedsReview,
				"lease_digest":       "",
				"lease_expires":      nil,
				"review_required_at": now,
				"review_reason":      reviewReasonLeaseExpired,
				"updated_at":         now,
			})
		if transition.Error != nil {
			return transition.Error
		}
		if transition.RowsAffected > 0 {
			stale = true
			return nil
		}
		approvalExpired := tx.Model(&Job{}).
			Where("id = ? AND status = ? AND worker_id = ? AND lease_digest = ? AND approval_expires_at <= ?", id, StatusLeased, workerID, digest, now).
			Updates(map[string]any{
				"status":             StatusNeedsReview,
				"lease_digest":       "",
				"lease_expires":      nil,
				"review_required_at": now,
				"review_reason":      reviewReasonLeaseExpired,
				"updated_at":         now,
			})
		if approvalExpired.Error != nil {
			return approvalExpired.Error
		}
		if approvalExpired.RowsAffected > 0 {
			stale = true
			return nil
		}

		confirmation := tx.Model(&Job{}).
			Where("id = ? AND status = ? AND worker_id = ? AND lease_digest = ? AND lease_expires > ? AND approval_expires_at > ? AND stop_revision = ? AND execution_confirmed_at IS NULL AND cancel_requested_at IS NULL", id, StatusLeased, workerID, digest, now, now, stopRevision).
			Updates(map[string]any{"execution_confirmed_at": now, "updated_at": now})
		if confirmation.Error != nil {
			return confirmation.Error
		}
		if confirmation.RowsAffected == 1 {
			return nil
		}

		var job Job
		err := tx.Where("id = ? AND status = ? AND worker_id = ? AND lease_digest = ? AND lease_expires > ? AND approval_expires_at > ?", id, StatusLeased, workerID, digest, now, now).First(&job).Error
		if err == nil && job.CancelRequestedAt != nil {
			cancellationRequested = true
			return nil
		}
		if errors.Is(err, gorm.ErrRecordNotFound) || (err == nil && job.ExecutionConfirmedAt == nil) {
			stale = true
			return nil
		}
		return err
	})
	if err != nil {
		return err
	}
	if stale {
		return ErrStaleLease
	}
	if cancellationRequested {
		return ErrCancellationRequested
	}
	return nil
}

func (r *gormRepository) BeginStartIntent(ctx context.Context, workerID string, id uuid.UUID, leaseDigest string, binding StartIntentBinding, gate StartGate, now time.Time) (*StartIntent, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	var intent *StartIntent
	var outcome error
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var job Job
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&job, "id = ?", id).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			outcome = ErrStaleLease
			return nil
		} else if err != nil {
			return err
		}

		leaseMatches := job.Status == StatusLeased && job.WorkerID == workerID && job.LeaseDigest == leaseDigest
		if job.StartIntentID != nil {
			if !sameStartIntent(job, workerID, leaseDigest, binding) {
				if job.Status == StatusLeased && job.ActualStartAckAt == nil {
					if err := quarantineStartIntent(tx, job.ID, now, reviewReasonStartProtocol); err != nil {
						return err
					}
				}
				outcome = ErrStartIntentReplay
				return nil
			}
			if job.ActualStartAckAt != nil {
				outcome = ErrStartIntentReplay
				return nil
			}
			if !leaseMatches {
				outcome = ErrStaleLease
				return nil
			}
			if gateUnavailableOrUnsafe(gate, binding.StopRevision) {
				if err := quarantineStartIntent(tx, job.ID, now, reviewReasonStartUnknown); err != nil {
					return err
				}
				outcome = startGateError(gate)
				return nil
			}
			if !job.LeaseExpires.After(now) || !job.ApprovalExpiresAt.After(now) || job.CancelRequestedAt != nil || !startIntentFresh(job, now) {
				if err := quarantineStartIntent(tx, job.ID, now, reviewReasonStartUnknown); err != nil {
					return err
				}
				outcome = ErrStartOutcomeReview
				return nil
			}
			intent = startIntentReceipt(job)
			return nil
		}

		if !leaseMatches {
			outcome = ErrStaleLease
			return nil
		}
		if !gate.Available {
			outcome = ErrStopRevisionUnavailable
			return nil
		}
		if gate.Active {
			outcome = ErrEmergencyStopped
			return nil
		}
		if job.StopRevision != binding.StopRevision || gate.CurrentRevision != binding.StopRevision {
			if err := tx.Model(&Job{}).Where("id = ? AND status = ? AND worker_id = ? AND lease_digest = ? AND start_intent_id IS NULL", id, StatusLeased, workerID, leaseDigest).Updates(map[string]any{
				"status": StatusCancelled, "lease_digest": "", "lease_expires": nil,
				"review_reason": reviewReasonStopRevision, "updated_at": now,
			}).Error; err != nil {
				return err
			}
			outcome = ErrStaleLease
			return nil
		}
		if !job.LeaseExpires.After(now) || !job.ApprovalExpiresAt.After(now) {
			if err := quarantineStartIntent(tx, job.ID, now, reviewReasonLeaseExpired); err != nil {
				return err
			}
			outcome = ErrStaleLease
			return nil
		}
		if job.ExecutionConfirmedAt == nil {
			outcome = ErrStaleLease
			return nil
		}
		if job.CancelRequestedAt != nil {
			outcome = ErrCancellationRequested
			return nil
		}

		computedDigest := approvalDigest(job)
		if job.ApprovalDigest == "" {
			if err := tx.Model(&Job{}).Where("id = ? AND approval_digest = ''", id).Update("approval_digest", computedDigest).Error; err != nil {
				return err
			}
			job.ApprovalDigest = computedDigest
		} else if job.ApprovalDigest != computedDigest {
			if err := quarantineStartIntent(tx, job.ID, now, reviewReasonStartProtocol); err != nil {
				return err
			}
			outcome = ErrStartOutcomeReview
			return nil
		}
		if binding.ApprovalDigest != job.ApprovalDigest {
			if err := quarantineStartIntent(tx, job.ID, now, reviewReasonStartProtocol); err != nil {
				return err
			}
			outcome = ErrStartOutcomeReview
			return nil
		}

		created := now.UTC().Truncate(time.Microsecond)
		stopRevision := binding.StopRevision
		result := tx.Model(&Job{}).Where("id = ? AND status = ? AND worker_id = ? AND lease_digest = ? AND start_intent_id IS NULL AND actual_start_ack_at IS NULL AND cancel_requested_at IS NULL AND stop_revision = ?", id, StatusLeased, workerID, leaseDigest, stopRevision).Updates(map[string]any{
			"start_intent_id": binding.IntentID, "start_intent_at": created,
			"start_intent_worker_id": workerID, "start_intent_lease_digest": leaseDigest,
			"start_intent_approval_digest": binding.ApprovalDigest, "start_intent_stop_revision": stopRevision,
			"updated_at": created,
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			outcome = ErrStaleLease
			return nil
		}
		job.StartIntentID = &binding.IntentID
		job.StartIntentAt = &created
		job.StartIntentWorkerID = workerID
		job.StartIntentLeaseDigest = leaseDigest
		job.StartIntentApprovalDigest = binding.ApprovalDigest
		job.StartIntentStopRevision = &stopRevision
		job.UpdatedAt = created
		intent = startIntentReceipt(job)
		return nil
	})
	if err != nil {
		return nil, err
	}
	if outcome != nil {
		return nil, outcome
	}
	return intent, nil
}

func (r *gormRepository) AcknowledgeActualStart(ctx context.Context, workerID string, id uuid.UUID, leaseDigest string, binding StartIntentBinding, gate StartGate, now time.Time) error {
	if ctx == nil {
		ctx = context.Background()
	}
	var outcome error
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var job Job
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&job, "id = ?", id).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			outcome = ErrStaleLease
			return nil
		} else if err != nil {
			return err
		}
		if !sameStartIntent(job, workerID, leaseDigest, binding) {
			if job.Status == StatusLeased && job.WorkerID == workerID && job.LeaseDigest == leaseDigest && job.ActualStartAckAt == nil {
				if err := quarantineStartIntent(tx, id, now, reviewReasonStartProtocol); err != nil {
					return err
				}
				outcome = ErrStartOutcomeReview
				return nil
			}
			outcome = ErrStaleLease
			return nil
		}
		if job.ActualStartAckAt != nil {
			return nil
		}
		if job.Status != StatusLeased || job.WorkerID != workerID || job.LeaseDigest != leaseDigest {
			outcome = ErrStartOutcomeReview
			return nil
		}
		if gateUnavailableOrUnsafe(gate, binding.StopRevision) || !job.LeaseExpires.After(now) || !job.ApprovalExpiresAt.After(now) || job.CancelRequestedAt != nil || !startIntentFresh(job, now) {
			if err := quarantineStartIntent(tx, id, now, reviewReasonStartUnknown); err != nil {
				return err
			}
			outcome = ErrStartOutcomeReview
			return nil
		}
		if job.ApprovalDigest == "" || job.ApprovalDigest != binding.ApprovalDigest || job.ApprovalDigest != approvalDigest(job) {
			if err := quarantineStartIntent(tx, id, now, reviewReasonStartProtocol); err != nil {
				return err
			}
			outcome = ErrStartOutcomeReview
			return nil
		}
		acknowledged := now.UTC().Truncate(time.Microsecond)
		result := tx.Model(&Job{}).Where("id = ? AND status = ? AND worker_id = ? AND lease_digest = ? AND start_intent_id = ? AND actual_start_ack_at IS NULL AND start_intent_worker_id = ? AND start_intent_lease_digest = ? AND start_intent_approval_digest = ? AND start_intent_stop_revision = ? AND stop_revision = ? AND cancel_requested_at IS NULL AND lease_expires > ? AND approval_expires_at > ?", id, StatusLeased, workerID, leaseDigest, binding.IntentID, workerID, leaseDigest, binding.ApprovalDigest, binding.StopRevision, binding.StopRevision, now, now).
			Update("actual_start_ack_at", acknowledged)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			if err := quarantineStartIntent(tx, id, now, reviewReasonStartUnknown); err != nil {
				return err
			}
			outcome = ErrStartOutcomeReview
		}
		return nil
	})
	if err != nil {
		return err
	}
	return outcome
}

func (r *gormRepository) ReportStartOutcomeUnknown(ctx context.Context, workerID string, id uuid.UUID, leaseDigest string, binding StartIntentBinding, now time.Time) (bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	alreadyAcknowledged := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var job Job
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&job, "id = ?", id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrStaleLease
			}
			return err
		}
		if !sameStartIntent(job, workerID, leaseDigest, binding) {
			return ErrStaleLease
		}
		if job.ActualStartAckAt != nil {
			alreadyAcknowledged = true
			return nil
		}
		if job.Status == StatusLeased {
			return quarantineStartIntent(tx, id, now, reviewReasonStartUnknown)
		}
		if job.Status != StatusNeedsReview {
			return ErrStaleLease
		}
		return nil
	})
	return alreadyAcknowledged, err
}

func sameStartIntent(job Job, workerID, leaseDigest string, binding StartIntentBinding) bool {
	return job.StartIntentID != nil && *job.StartIntentID == binding.IntentID &&
		job.StartIntentWorkerID == workerID && job.StartIntentLeaseDigest == leaseDigest &&
		job.StartIntentApprovalDigest == binding.ApprovalDigest && job.ApprovalDigest == binding.ApprovalDigest &&
		job.StartIntentStopRevision != nil && *job.StartIntentStopRevision == binding.StopRevision
}

func startIntentReceipt(job Job) *StartIntent {
	if job.StartIntentID == nil || job.StartIntentAt == nil || job.StartIntentStopRevision == nil {
		return nil
	}
	return &StartIntent{
		JobID: job.ID, IntentID: *job.StartIntentID, WorkerID: job.StartIntentWorkerID,
		ApprovalDigest: job.StartIntentApprovalDigest, StopRevision: *job.StartIntentStopRevision,
		CreatedAt: *job.StartIntentAt,
	}
}

func gateUnavailableOrUnsafe(gate StartGate, expectedRevision uint64) bool {
	return !gate.Available || gate.Active || gate.CurrentRevision != expectedRevision
}

func startGateError(gate StartGate) error {
	if !gate.Available {
		return ErrStopRevisionUnavailable
	}
	if gate.Active {
		return ErrEmergencyStopped
	}
	return ErrStaleLease
}

func quarantineStartIntent(tx *gorm.DB, id uuid.UUID, now time.Time, reason string) error {
	return tx.Model(&Job{}).Where("id = ? AND status = ? AND actual_start_ack_at IS NULL", id, StatusLeased).Updates(map[string]any{
		"status": StatusNeedsReview, "lease_digest": "", "lease_expires": nil,
		"review_required_at": now, "review_reason": reason, "updated_at": now,
	}).Error
}

func (r *gormRepository) CancelTask(ctx context.Context, ownerIdentity, taskID string, jobID uuid.UUID, now time.Time) (*Job, bool, error) {
	if r == nil || r.db == nil || r.db.Config == nil || r.db.Statement == nil || r.db.DryRun || ctx == nil || !validIdentifier(ownerIdentity) || !validIdentifier(taskID) || now.IsZero() {
		return nil, false, ErrInvalidTask
	}
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	ctx, cancel := context.WithTimeout(ctx, AdmissionTimeout)
	defer cancel()
	var job *Job
	revoked := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		lookup := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("owner_identity = ? AND task_id = ?", ownerIdentity, taskID)
		if jobID != uuid.Nil {
			lookup = lookup.Where("id = ?", jobID)
		}
		var stored Job
		if err := lookup.First(&stored).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrJobNotFound
		} else if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}

		updates := map[string]any{"updated_at": now}
		switch {
		case stored.Status == StatusPending:
			updates["status"] = StatusCancelled
			updates["lease_digest"] = ""
			updates["lease_expires"] = nil
		case stored.Status == StatusLeased && stored.StartIntentID != nil && stored.ActualStartAckAt == nil:
			updates["status"] = StatusNeedsReview
			updates["lease_digest"] = ""
			updates["lease_expires"] = nil
			updates["review_required_at"] = now
			updates["review_reason"] = reviewReasonStartUnknown
			if stored.CancelRequestedAt == nil {
				updates["cancel_requested_at"] = now
			}
		case stored.Status == StatusLeased && stored.ActualStartAckAt != nil:
			if stored.CancelRequestedAt == nil {
				updates["cancel_requested_at"] = now
			}
		case stored.Status == StatusLeased:
			updates["status"] = StatusCancelled
			updates["lease_digest"] = ""
			updates["lease_expires"] = nil
		}
		jobIDToReload := stored.ID
		if err := tx.Model(&Job{}).Where("id = ?", jobIDToReload).Updates(updates).Error; err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		stored = Job{}
		if err := tx.First(&stored, "id = ?", jobIDToReload).Error; err != nil {
			return err
		}
		job = &stored
		revoked = stored.Status == StatusCancelled
		return ctx.Err()
	})
	if ctx.Err() != nil {
		return job, false, errors.Join(err, ctx.Err())
	}
	if err != nil {
		return nil, false, err
	}
	return job, revoked, nil
}

func (r *gormRepository) Complete(workerID string, id uuid.UUID, digest string, completion Completion, now time.Time) (*Job, error) {
	var completed *Job
	stale := false
	cancellationRequested := false
	invalidCancellationReport := false
	err := r.db.Transaction(func(tx *gorm.DB) error {
		validLease := func(query *gorm.DB) *gorm.DB {
			return query.Where("id = ? AND status = ? AND worker_id = ? AND lease_digest = ? AND lease_expires > ? AND actual_start_ack_at IS NOT NULL", id, StatusLeased, workerID, digest, now)
		}

		if completion.TerminationVerified && !completion.CancellationRequested {
			invalidCancellationReport = true
			return nil
		}

		var current Job
		currentErr := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND status = ? AND worker_id = ? AND lease_digest = ?", id, StatusLeased, workerID, digest).
			First(&current).Error
		if currentErr != nil && !errors.Is(currentErr, gorm.ErrRecordNotFound) {
			return currentErr
		}
		if currentErr == nil && current.ActualStartAckAt == nil {
			reason := reviewReasonStartProtocol
			if current.StartIntentID != nil {
				reason = reviewReasonStartUnknown
			}
			if err := quarantineStartIntent(tx, id, now, reason); err != nil {
				return err
			}
			stale = true
			return nil
		}

		if completion.CancellationRequested {
			var job Job
			err := validLease(tx).First(&job).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				transition := tx.Model(&Job{}).
					Where("id = ? AND status = ? AND worker_id = ? AND lease_digest = ? AND (lease_expires IS NULL OR lease_expires <= ?)", id, StatusLeased, workerID, digest, now).
					Updates(map[string]any{
						"status":             StatusNeedsReview,
						"lease_digest":       "",
						"lease_expires":      nil,
						"review_required_at": now,
						"review_reason":      reviewReasonLeaseExpired,
						"updated_at":         now,
					})
				if transition.Error != nil {
					return transition.Error
				}
				stale = true
				return nil
			}
			if err != nil {
				return err
			}
			if job.CancelRequestedAt == nil {
				invalidCancellationReport = true
				return nil
			}
			updates := map[string]any{
				"output":     completion.Output,
				"error":      completion.Error,
				"exit_code":  completion.ExitCode,
				"updated_at": now,
			}
			if completion.TerminationVerified {
				updates["status"] = StatusCancelled
				updates["lease_digest"] = ""
				updates["lease_expires"] = nil
				updates["completed_at"] = now
			} else {
				updates["status"] = StatusNeedsReview
				updates["lease_digest"] = ""
				updates["lease_expires"] = nil
				updates["review_required_at"] = now
				updates["review_reason"] = reviewReasonCancelUnverified
			}
			result := validLease(tx).Model(&Job{}).Where("cancel_requested_at IS NOT NULL").Updates(updates)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected == 1 {
				job = Job{}
				if err := tx.First(&job, "id = ?", id).Error; err != nil {
					return err
				}
				completed = &job
			} else {
				stale = true
			}
			return nil
		}

		result := tx.Model(&Job{}).
			Where("id = ? AND status = ? AND worker_id = ? AND lease_digest = ? AND lease_expires > ? AND execution_confirmed_at IS NOT NULL AND cancel_requested_at IS NULL", id, StatusLeased, workerID, digest, now).
			Updates(map[string]any{
				"status":        StatusCompleted,
				"lease_digest":  "",
				"lease_expires": nil,
				"output":        completion.Output,
				"error":         completion.Error,
				"exit_code":     completion.ExitCode,
				"completed_at":  now,
				"updated_at":    now,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 1 {
			var job Job
			if err := tx.First(&job, "id = ?", id).Error; err != nil {
				return err
			}
			completed = &job
			return nil
		}

		var active Job
		activeErr := validLease(tx).First(&active).Error
		if activeErr == nil && active.CancelRequestedAt != nil {
			cancellationRequested = true
			return nil
		}
		if activeErr != nil && !errors.Is(activeErr, gorm.ErrRecordNotFound) {
			return activeErr
		}

		transition := tx.Model(&Job{}).
			Where("id = ? AND status = ? AND worker_id = ? AND lease_digest = ? AND (lease_expires IS NULL OR lease_expires <= ?)", id, StatusLeased, workerID, digest, now).
			Updates(map[string]any{
				"status":             StatusNeedsReview,
				"lease_digest":       "",
				"lease_expires":      nil,
				"review_required_at": now,
				"review_reason":      reviewReasonLeaseExpired,
				"updated_at":         now,
			})
		if transition.Error != nil {
			return transition.Error
		}
		stale = true
		return nil
	})
	if err != nil {
		return nil, err
	}
	if invalidCancellationReport {
		return nil, ErrInvalidTask
	}
	if cancellationRequested {
		return nil, ErrCancellationRequested
	}
	if stale {
		return nil, ErrStaleLease
	}
	return completed, nil
}

func (r *gormRepository) ListCompletedUnreconciled(limit int) ([]Job, error) {
	if limit <= 0 {
		limit = 20
	}
	var jobs []Job
	err := r.db.
		Where("status = ? AND reconciled_at IS NULL", StatusCompleted).
		Order("completed_at ASC").
		Limit(limit).
		Find(&jobs).Error
	return jobs, err
}

func (r *gormRepository) ListCancelledUnreconciled(limit int) ([]Job, error) {
	if limit <= 0 {
		limit = 20
	}
	var jobs []Job
	err := r.db.
		Where("status = ? AND reconciled_at IS NULL", StatusCancelled).
		Order("completed_at ASC NULLS FIRST, updated_at ASC, id ASC").
		Limit(limit).
		Find(&jobs).Error
	return jobs, err
}

func (r *gormRepository) ListReviewRequiredUnreconciled(limit int) ([]Job, error) {
	if limit <= 0 {
		limit = 20
	}
	var jobs []Job
	err := r.db.
		Where("status IN ? AND review_required_at IS NOT NULL AND reconciled_at IS NULL", []string{StatusExpired, StatusNeedsReview}).
		Order("review_required_at ASC, id ASC").
		Limit(limit).
		Find(&jobs).Error
	return jobs, err
}

func (r *gormRepository) MarkReconciled(id uuid.UUID, at time.Time) (bool, error) {
	result := r.db.Model(&Job{}).
		Where("id = ? AND reconciled_at IS NULL AND (status IN ? OR (status IN ? AND review_required_at IS NOT NULL))", id, []string{StatusCompleted, StatusCancelled}, []string{StatusExpired, StatusNeedsReview}).
		Update("reconciled_at", at.UTC())
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected == 1, nil
}
