package operations

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"automation-hub-backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrAtomicClaimsUnsupported = errors.New("operations: repository does not support durable claims")
	ErrClaimLost               = errors.New("operations: execution claim lost or expired")
	ErrOperationClaimed        = errors.New("operations: operation has an execution claim")
	ErrOperationNotClaimable   = errors.New("operations: operation is not eligible for safe execution")
	ErrStaleOperation          = errors.New("operations: stale operation version")
)

// ExecutionClaim is the fencing token for one worker's ownership of an
// Operation. The owner UUID identifies a worker process; Generation is
// monotonically advanced by PostgreSQL whenever ownership changes or ends.
type ExecutionClaim struct {
	OperationID uuid.UUID
	OwnerUserID string
	WorkspaceID string
	Owner       uuid.UUID
	Generation  int64
}

// ClaimedOperation couples the snapshot read under the atomic claim with its
// fencing token.
type ClaimedOperation struct {
	Operation models.Operation
	Claim     ExecutionClaim
}

// RecoveryResult reports both recovered work and executing rows that recovery
// deliberately left untouched because they have a live lease or no lease.
type RecoveryResult struct {
	ScannedRunning      int
	ScannedVerifying    int
	Recovered           int
	LiveRunning         int
	LiveVerifying       int
	UnleasedRunning     int
	UnleasedVerifying   int
	ExpiredClaimsRemain int
	Details             []string
}

// ClaimRepository is an optional extension to Repository. Workers fail closed
// when a repository cannot provide database-atomic claims and fenced writes.
type ClaimRepository interface {
	ClaimNext(context.Context, string, string, uuid.UUID, time.Duration) (*ClaimedOperation, error)
	ClaimOperation(context.Context, string, string, uuid.UUID, uuid.UUID, time.Duration) (*ClaimedOperation, error)
	RenewClaim(context.Context, ExecutionClaim, time.Duration) error
	TransitionClaimed(context.Context, ExecutionClaim, models.Operation, models.OperationEvent, bool) (*models.Operation, error)
	ReleaseClaim(context.Context, ExecutionClaim) error
	RecoverExpiredClaims(context.Context, string, string, int) (RecoveryResult, error)
}

var _ ClaimRepository = (*GormRepository)(nil)
var _ ClaimRepository = (*MemoryRepository)(nil)

type executionClaimRow struct {
	Owner      uuid.UUID
	Generation int64
	ExpiresAt  time.Time
}

func claimLeaseMicros(lease time.Duration) (int64, error) {
	if lease <= 0 || lease > 24*time.Hour {
		return 0, fmt.Errorf("operations: claim lease must be greater than zero and no more than 24 hours")
	}
	return int64((lease + time.Microsecond - 1) / time.Microsecond), nil
}

func (r *GormRepository) ClaimNext(ctx context.Context, ownerUserID, workspaceID string, workerID uuid.UUID, lease time.Duration) (*ClaimedOperation, error) {
	if err := intakeContextError(ctx); err != nil {
		return nil, err
	}
	if r == nil || r.DB == nil {
		return nil, ErrAtomicClaimsUnsupported
	}
	ctx, finish, err := observedIntakeContext(ctx)
	if err != nil {
		return nil, err
	}
	defer finish()
	leaseMicros, err := claimLeaseMicros(lease)
	if err != nil {
		return nil, err
	}
	if ownerUserID == "" || workspaceID == "" || workerID == uuid.Nil {
		return nil, fmt.Errorf("operations: owner, workspace, and worker identity are required for a claim")
	}
	var claimed *ClaimedOperation
	err = r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := intakeContextError(ctx); err != nil {
			return err
		}
		var op models.Operation
		query := tx.Model(&models.Operation{}).
			Where("owner_user_id = ? AND workspace_id = ?", ownerUserID, workspaceID).
			Where("status IN ?", []string{string(StatusDrafting), string(StatusNew), string(StatusClassified), string(StatusReady), string(StatusApproved)}).
			Where(`status <> ? OR EXISTS (
				SELECT 1 FROM public.operation_events AS approval
				WHERE approval.operation_id = operations.id
				  AND approval.event_type = 'status_change'
				  AND approval.after_status = 'approved'
				  AND approval.payload_json ? 'sourceApproval'
			)`, string(StatusApproved)).
			// A fresh exact-revision owner approval supersedes a prior self-hold.
			// Keep the review reminder for all other statuses, matching the memory
			// repository's safeOperationClaimable behavior.
			Where("(status = ? OR next_review_at IS NULL OR next_review_at <= clock_timestamp())", string(StatusApproved)).
			Where(claimSourceHeadPredicate).
			Where(`NOT EXISTS (
				SELECT 1 FROM public.operation_execution_claims AS active_claim
				WHERE active_claim.operation_id = operations.id
				  AND active_claim.claim_owner IS NOT NULL
				  AND active_claim.lease_expires_at > clock_timestamp()
			)`).
			Where(`status <> ? OR current_decision IN ? OR requires_approval = TRUE`,
				string(StatusClassified),
				[]string{string(DecisionRunSafeLocalWorker), string(DecisionCreateDraft), string(DecisionBlock)}).
			Order("created_at ASC, updated_at ASC, id ASC").
			Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Limit(1)
		if err := query.First(&op).Error; err != nil {
			if authorityErr := intakeContextError(ctx); authorityErr != nil {
				return authorityErr
			}
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return fmt.Errorf("select next claimable operation: %w", err)
		}

		if err := intakeContextError(ctx); err != nil {
			return err
		}

		var row executionClaimRow
		claimSQL := `
INSERT INTO public.operation_execution_claims
	(operation_id, claim_owner, generation, claimed_at, lease_expires_at)
VALUES (?, ?, 1, clock_timestamp(), clock_timestamp() + (? * INTERVAL '1 microsecond'))
ON CONFLICT (operation_id) DO UPDATE SET
	claim_owner = EXCLUDED.claim_owner,
	generation = public.operation_execution_claims.generation + 1,
	claimed_at = clock_timestamp(),
	lease_expires_at = clock_timestamp() + (? * INTERVAL '1 microsecond')
WHERE public.operation_execution_claims.claim_owner IS NULL
   OR public.operation_execution_claims.lease_expires_at <= clock_timestamp()
RETURNING claim_owner AS owner, generation, lease_expires_at AS expires_at`
		scanErr := tx.Raw(claimSQL, op.ID, workerID, leaseMicros, leaseMicros).
			Row().Scan(&row.Owner, &row.Generation, &row.ExpiresAt)
		if err := intakeContextError(ctx); err != nil {
			return err
		}
		if errors.Is(scanErr, sql.ErrNoRows) {
			return ErrClaimLost
		}
		if scanErr != nil {
			return fmt.Errorf("persist operation claim: %w", scanErr)
		}
		claimed = &ClaimedOperation{
			Operation: op,
			Claim: ExecutionClaim{
				OperationID: op.ID,
				OwnerUserID: ownerUserID,
				WorkspaceID: workspaceID,
				Owner:       row.Owner,
				Generation:  row.Generation,
			},
		}
		return intakeContextError(ctx)
	})
	if err == nil {
		err = intakeContextError(ctx)
	}
	if err != nil {
		return nil, err
	}
	return claimed, nil
}

// ClaimOperation atomically leases one explicitly selected safe-executable
// operation. The returned operation snapshot is read while holding the same
// row lock used to create the durable claim, so callers never execute a stale
// pre-claim snapshot.
func (r *GormRepository) ClaimOperation(ctx context.Context, ownerUserID, workspaceID string, operationID, workerID uuid.UUID, lease time.Duration) (*ClaimedOperation, error) {
	if err := intakeContextError(ctx); err != nil {
		return nil, err
	}
	if r == nil || r.DB == nil {
		return nil, ErrAtomicClaimsUnsupported
	}
	ctx, finish, err := observedIntakeContext(ctx)
	if err != nil {
		return nil, err
	}
	defer finish()
	leaseMicros, err := claimLeaseMicros(lease)
	if err != nil {
		return nil, err
	}
	if ownerUserID == "" || workspaceID == "" || operationID == uuid.Nil || workerID == uuid.Nil {
		return nil, fmt.Errorf("operations: owner, workspace, operation, and worker identity are required for a claim")
	}

	var claimed *ClaimedOperation
	err = r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := intakeContextError(ctx); err != nil {
			return err
		}
		var op models.Operation
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND owner_user_id = ? AND workspace_id = ?", operationID, ownerUserID, workspaceID).
			First(&op).Error; err != nil {
			if authorityErr := intakeContextError(ctx); authorityErr != nil {
				return authorityErr
			}
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return fmt.Errorf("lock explicitly selected operation: %w", err)
		}

		if err := intakeContextError(ctx); err != nil {
			return err
		}

		var dbNow time.Time
		if err := tx.Raw("SELECT clock_timestamp()").Row().Scan(&dbNow); err != nil {
			return fmt.Errorf("read database time before explicit operation claim: %w", err)
		}
		if err := intakeContextError(ctx); err != nil {
			return err
		}
		if !safeOperationClaimable(op, dbNow.UTC()) {
			return ErrOperationNotClaimable
		}
		if OperationStatus(op.Status) == StatusApproved {
			var eligible int64
			if err := tx.Raw(`SELECT count(*) FROM public.operation_events
WHERE operation_id = ? AND event_type = 'status_change'
  AND after_status = 'approved' AND payload_json ? 'sourceApproval'`, op.ID).Row().Scan(&eligible); err != nil {
				return fmt.Errorf("verify approved source operation receipt: %w", err)
			}
			if eligible != 1 {
				return ErrOperationNotClaimable
			}
		}
		if op.SourceIdentityHash != "" {
			var eligible int64
			if err := tx.Model(&models.Operation{}).Where("id = ? AND owner_user_id = ? AND workspace_id = ?", op.ID, op.OwnerUserID, op.WorkspaceID).
				Where(claimSourceHeadPredicate).Count(&eligible).Error; err != nil {
				return err
			}
			if eligible != 1 {
				return ErrSourceHeadSuperseded
			}
		}

		var row executionClaimRow
		scanErr := tx.Raw(`
INSERT INTO public.operation_execution_claims
	(operation_id, claim_owner, generation, claimed_at, lease_expires_at)
VALUES (?, ?, 1, clock_timestamp(), clock_timestamp() + (? * INTERVAL '1 microsecond'))
ON CONFLICT (operation_id) DO UPDATE SET
	claim_owner = EXCLUDED.claim_owner,
	generation = public.operation_execution_claims.generation + 1,
	claimed_at = clock_timestamp(),
	lease_expires_at = clock_timestamp() + (? * INTERVAL '1 microsecond')
WHERE public.operation_execution_claims.claim_owner IS NULL
   OR public.operation_execution_claims.lease_expires_at <= clock_timestamp()
RETURNING claim_owner AS owner, generation, lease_expires_at AS expires_at`,
			operationID, workerID, leaseMicros, leaseMicros).
			Row().Scan(&row.Owner, &row.Generation, &row.ExpiresAt)
		if err := intakeContextError(ctx); err != nil {
			return err
		}
		if errors.Is(scanErr, sql.ErrNoRows) {
			return ErrOperationClaimed
		}
		if scanErr != nil {
			return fmt.Errorf("persist explicit operation claim: %w", scanErr)
		}

		claimed = &ClaimedOperation{
			Operation: op,
			Claim: ExecutionClaim{
				OperationID: operationID,
				OwnerUserID: ownerUserID,
				WorkspaceID: workspaceID,
				Owner:       row.Owner,
				Generation:  row.Generation,
			},
		}
		return intakeContextError(ctx)
	})
	if err == nil {
		err = intakeContextError(ctx)
	}
	if err != nil {
		return nil, err
	}
	return claimed, nil
}

func safeOperationClaimable(op models.Operation, now time.Time) bool {
	switch OperationStatus(op.Status) {
	case StatusClassified, StatusReady, StatusFailed:
	case StatusApproved:
		// A fresh, exact-revision owner approval overrides a prior review
		// reminder. The durable receipt is verified by ClaimOperation before
		// a worker receives this claim; OwnerType is not authorization proof.
		return IsSourceDerived(op) && op.RequiresApproval
	default:
		return false
	}
	if op.NextReviewAt != nil && op.NextReviewAt.After(now) {
		return false
	}
	return CurrentDecision(op.CurrentDecision) == DecisionRunSafeLocalWorker &&
		!op.RequiresApproval &&
		RiskLevel(op.RiskLevel) == RiskLow &&
		AutonomyLevel(op.AutonomyLevel) == AutonomyAuto &&
		OwnerType(op.OwnerType) == OwnerHAI
}

func (r *GormRepository) RenewClaim(ctx context.Context, claim ExecutionClaim, lease time.Duration) error {
	if err := intakeContextError(ctx); err != nil {
		return err
	}
	if r == nil || r.DB == nil {
		return ErrAtomicClaimsUnsupported
	}
	ctx, finish, err := observedIntakeContext(ctx)
	if err != nil {
		return err
	}
	defer finish()
	leaseMicros, err := claimLeaseMicros(lease)
	if err != nil {
		return err
	}
	err = r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := intakeContextError(ctx); err != nil {
			return err
		}
		var operationID uuid.UUID
		err := tx.Raw(`
UPDATE public.operation_execution_claims
SET lease_expires_at = clock_timestamp() + (? * INTERVAL '1 microsecond')
WHERE operation_id = ? AND claim_owner = ? AND generation = ?
  AND lease_expires_at > clock_timestamp()
RETURNING operation_id`, leaseMicros, claim.OperationID, claim.Owner, claim.Generation).
			Row().Scan(&operationID)
		if authorityErr := intakeContextError(ctx); authorityErr != nil {
			return authorityErr
		}
		if errors.Is(err, sql.ErrNoRows) {
			return ErrClaimLost
		}
		if err != nil {
			return fmt.Errorf("renew operation claim: %w", err)
		}
		return intakeContextError(ctx)
	})
	if err == nil {
		err = intakeContextError(ctx)
	}
	return err
}

func (r *GormRepository) TransitionClaimed(ctx context.Context, claim ExecutionClaim, op models.Operation, event models.OperationEvent, release bool) (*models.Operation, error) {
	if err := intakeContextError(ctx); err != nil {
		return nil, err
	}
	if r == nil || r.DB == nil {
		return nil, ErrAtomicClaimsUnsupported
	}
	ctx, finish, err := observedIntakeContext(ctx)
	if err != nil {
		return nil, err
	}
	defer finish()
	if claim.OperationID != op.ID || claim.Owner == uuid.Nil || claim.Generation <= 0 || op.Version <= 1 {
		return nil, ErrClaimLost
	}
	expectedVersion := op.Version - 1
	err = r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := intakeContextError(ctx); err != nil {
			return err
		}
		var current models.Operation
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND owner_user_id = ? AND workspace_id = ?", claim.OperationID, claim.OwnerUserID, claim.WorkspaceID).
			First(&current).Error; err != nil {
			if authorityErr := intakeContextError(ctx); authorityErr != nil {
				return authorityErr
			}
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrClaimLost
			}
			return fmt.Errorf("lock operation before fenced transition: %w", err)
		}

		if err := intakeContextError(ctx); err != nil {
			return err
		}
		if current.Version != expectedVersion {
			return ErrStaleOperation
		}
		if current.ID != op.ID || current.OwnerUserID != op.OwnerUserID || current.WorkspaceID != op.WorkspaceID {
			return fmt.Errorf("operations: operation identity is immutable")
		}
		if err := validateMutationState(current, op, &event); err != nil {
			return err
		}
		if err := lockAndVerifyClaim(tx, claim); err != nil {
			return err
		}
		op.CreatedAt = current.CreatedAt
		if err := updateOperationCAS(tx, op, expectedVersion); err != nil {
			return err
		}
		if err := intakeContextError(ctx); err != nil {
			return err
		}
		event.OperationID = op.ID
		if err := tx.Create(&event).Error; err != nil {
			return fmt.Errorf("append operation transition event: %w", err)
		}
		if err := intakeContextError(ctx); err != nil {
			return err
		}
		if release {
			result := tx.Exec(`
UPDATE public.operation_execution_claims
SET generation = generation + 1, claim_owner = NULL, lease_expires_at = NULL
WHERE operation_id = ? AND claim_owner = ? AND generation = ?
  AND lease_expires_at > clock_timestamp()`, claim.OperationID, claim.Owner, claim.Generation)
			if err := intakeContextError(ctx); err != nil {
				return err
			}
			if result.Error != nil {
				return fmt.Errorf("release operation claim after transition: %w", result.Error)
			}
			if result.RowsAffected != 1 {
				return ErrClaimLost
			}
		}
		return intakeContextError(ctx)
	})
	if err == nil {
		err = intakeContextError(ctx)
	}
	if err != nil {
		return nil, err
	}
	return &op, nil
}

func (r *GormRepository) ReleaseClaim(ctx context.Context, claim ExecutionClaim) error {
	if err := intakeContextError(ctx); err != nil {
		return err
	}
	if r == nil || r.DB == nil {
		return ErrAtomicClaimsUnsupported
	}
	ctx, finish, err := observedIntakeContext(ctx)
	if err != nil {
		return err
	}
	defer finish()
	err = r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := intakeContextError(ctx); err != nil {
			return err
		}
		var op models.Operation
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND owner_user_id = ? AND workspace_id = ?", claim.OperationID, claim.OwnerUserID, claim.WorkspaceID).
			First(&op).Error; err != nil {
			if authorityErr := intakeContextError(ctx); authorityErr != nil {
				return authorityErr
			}
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrClaimLost
			}
			return fmt.Errorf("lock operation before claim release: %w", err)
		}

		if err := intakeContextError(ctx); err != nil {
			return err
		}
		if err := lockAndVerifyClaim(tx, claim); err != nil {
			return err
		}
		result := tx.Exec(`
UPDATE public.operation_execution_claims
SET generation = generation + 1, claim_owner = NULL, lease_expires_at = NULL
WHERE operation_id = ? AND claim_owner = ? AND generation = ?
  AND lease_expires_at > clock_timestamp()`, claim.OperationID, claim.Owner, claim.Generation)
		if err := intakeContextError(ctx); err != nil {
			return err
		}
		if result.Error != nil {
			return fmt.Errorf("release operation claim: %w", result.Error)
		}
		if result.RowsAffected != 1 {
			return ErrClaimLost
		}
		return intakeContextError(ctx)
	})
	if err == nil {
		err = intakeContextError(ctx)
	}
	return err
}

func (r *GormRepository) RecoverExpiredClaims(ctx context.Context, ownerUserID, workspaceID string, limit int) (RecoveryResult, error) {
	if err := intakeContextError(ctx); err != nil {
		return RecoveryResult{}, err
	}
	if r == nil || r.DB == nil {
		return RecoveryResult{}, ErrAtomicClaimsUnsupported
	}
	ctx, finish, err := observedIntakeContext(ctx)
	if err != nil {
		return RecoveryResult{}, err
	}
	defer finish()
	if limit <= 0 || limit > 200 {
		limit = 200
	}
	var result RecoveryResult
	err = r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := intakeContextError(ctx); err != nil {
			return err
		}
		var counts []struct {
			Status string
			Total  int
		}
		if err := tx.Raw(`
SELECT status, count(*) AS total
FROM public.operations
WHERE owner_user_id = ? AND workspace_id = ? AND status IN (?, ?)
GROUP BY status`, ownerUserID, workspaceID, string(StatusRunning), string(StatusVerifying)).
			Scan(&counts).Error; err != nil {
			return fmt.Errorf("count executing operations before recovery: %w", err)
		}
		if err := intakeContextError(ctx); err != nil {
			return err
		}
		for _, row := range counts {
			switch OperationStatus(row.Status) {
			case StatusRunning:
				result.ScannedRunning = row.Total
			case StatusVerifying:
				result.ScannedVerifying = row.Total
			}
		}

		var candidates []models.Operation
		if err := tx.Raw(`
SELECT operation.*
FROM public.operations AS operation
JOIN public.operation_execution_claims AS claim
  ON claim.operation_id = operation.id
WHERE operation.owner_user_id = ? AND operation.workspace_id = ?
  AND operation.status IN (?, ?)
  AND claim.claim_owner IS NOT NULL
  AND claim.lease_expires_at <= clock_timestamp()
ORDER BY operation.updated_at ASC, operation.id ASC
FOR UPDATE OF operation SKIP LOCKED
LIMIT ?`, ownerUserID, workspaceID, string(StatusRunning), string(StatusVerifying), limit).
			Scan(&candidates).Error; err != nil {
			return fmt.Errorf("select expired executing operation claims: %w", err)
		}

		if err := intakeContextError(ctx); err != nil {
			return err
		}
		for _, op := range candidates {
			if err := intakeContextError(ctx); err != nil {
				return err
			}
			var claim executionClaimRow
			scanErr := tx.Raw(`
SELECT claim_owner AS owner, generation, lease_expires_at AS expires_at
FROM public.operation_execution_claims
WHERE operation_id = ?
FOR UPDATE`, op.ID).Row().Scan(&claim.Owner, &claim.Generation, &claim.ExpiresAt)
			if err := intakeContextError(ctx); err != nil {
				return err
			}
			if errors.Is(scanErr, sql.ErrNoRows) {
				continue
			}
			if scanErr != nil {
				return fmt.Errorf("lock expired claim for operation %s: %w", op.ID, scanErr)
			}
			var now time.Time
			if err := tx.Raw("SELECT clock_timestamp()").Row().Scan(&now); err != nil {
				return fmt.Errorf("read database time during operation recovery: %w", err)
			}
			if err := intakeContextError(ctx); err != nil {
				return err
			}
			if claim.Owner == uuid.Nil || claim.Generation <= 0 || claim.ExpiresAt.After(now) {
				continue
			}

			to := StatusInterrupted
			message := "recovered after expired worker lease: side effect outcome is uncertain; no automatic replay"
			if OperationStatus(op.Status) == StatusVerifying {
				to = StatusAwaitingApproval
				message = "recovered after expired worker lease: verification is incomplete; human confirmation required; no automatic replay"
			}
			updated, event, err := ApplyTransition(op, to, "recovery", "", message, now.UTC())
			if err != nil {
				return fmt.Errorf("build recovery transition for operation %s: %w", op.ID, err)
			}
			if err := updateOperationCAS(tx, updated, op.Version); err != nil {
				return fmt.Errorf("persist recovery transition for operation %s: %w", op.ID, err)
			}
			if err := intakeContextError(ctx); err != nil {
				return err
			}
			if err := tx.Create(&event).Error; err != nil {
				return fmt.Errorf("append recovery event for operation %s: %w", op.ID, err)
			}
			if err := intakeContextError(ctx); err != nil {
				return err
			}
			fenced := tx.Exec(`
UPDATE public.operation_execution_claims
SET generation = generation + 1, claim_owner = NULL, lease_expires_at = NULL
WHERE operation_id = ? AND claim_owner = ? AND generation = ?
  AND lease_expires_at <= clock_timestamp()`, op.ID, claim.Owner, claim.Generation)
			if fenced.Error != nil {
				return fmt.Errorf("fence expired operation claim %s: %w", op.ID, fenced.Error)
			}
			if fenced.RowsAffected != 1 {
				return ErrClaimLost
			}
			if err := intakeContextError(ctx); err != nil {
				return err
			}
			result.Recovered++
			result.Details = append(result.Details, fmt.Sprintf("op %s: %s -> %s", op.ID, op.Status, to))
		}

		if err := intakeContextError(ctx); err != nil {
			return err
		}
		var remaining []struct {
			Status  string
			Total   int
			Live    int
			Expired int
		}
		if err := tx.Raw(`
SELECT operation.status, count(*) AS total,
       count(*) FILTER (WHERE claim.claim_owner IS NOT NULL
                         AND claim.lease_expires_at > clock_timestamp()) AS live,
       count(*) FILTER (WHERE claim.claim_owner IS NOT NULL
                         AND claim.lease_expires_at <= clock_timestamp()) AS expired
FROM public.operations AS operation
LEFT JOIN public.operation_execution_claims AS claim
  ON claim.operation_id = operation.id
WHERE operation.owner_user_id = ? AND operation.workspace_id = ?
  AND operation.status IN (?, ?)
GROUP BY operation.status`, ownerUserID, workspaceID, string(StatusRunning), string(StatusVerifying)).
			Scan(&remaining).Error; err != nil {
			return fmt.Errorf("count executing operations after recovery: %w", err)
		}
		if err := intakeContextError(ctx); err != nil {
			return err
		}
		for _, row := range remaining {
			unleased := row.Total - row.Live - row.Expired
			result.ExpiredClaimsRemain += row.Expired
			switch OperationStatus(row.Status) {
			case StatusRunning:
				result.LiveRunning = row.Live
				result.UnleasedRunning = unleased
			case StatusVerifying:
				result.LiveVerifying = row.Live
				result.UnleasedVerifying = unleased
			}
		}
		return intakeContextError(ctx)
	})
	if err == nil {
		err = intakeContextError(ctx)
	}
	if err != nil {
		return RecoveryResult{}, err
	}
	return result, nil
}

func lockAndVerifyClaim(tx *gorm.DB, claim ExecutionClaim) error {
	if tx == nil || tx.Statement == nil {
		return ErrAtomicClaimsUnsupported
	}
	if err := intakeContextError(tx.Statement.Context); err != nil {
		return err
	}
	var row executionClaimRow
	var ownerText sql.NullString
	err := tx.Raw(`
SELECT claim_owner::text, generation, lease_expires_at AS expires_at
FROM public.operation_execution_claims
WHERE operation_id = ?
FOR UPDATE`, claim.OperationID).Row().Scan(&ownerText, &row.Generation, &row.ExpiresAt)
	if err := intakeContextError(tx.Statement.Context); err != nil {
		return err
	}
	if errors.Is(err, sql.ErrNoRows) {
		return ErrClaimLost
	}
	if err != nil {
		return fmt.Errorf("read operation claim: %w", err)
	}
	if !ownerText.Valid {
		return ErrClaimLost
	}
	row.Owner, err = uuid.Parse(ownerText.String)
	if err != nil {
		return fmt.Errorf("parse operation claim owner: %w", err)
	}
	var now time.Time
	if err := tx.Raw("SELECT clock_timestamp()").Row().Scan(&now); err != nil {
		return fmt.Errorf("verify operation claim: %w", err)
	}
	if err := intakeContextError(tx.Statement.Context); err != nil {
		return err
	}
	if row.Owner != claim.Owner || row.Generation != claim.Generation || !row.ExpiresAt.After(now) {
		return ErrClaimLost
	}
	return nil
}

func updateOperationCAS(tx *gorm.DB, op models.Operation, expectedVersion int64) error {
	if tx == nil || tx.Statement == nil {
		return ErrAtomicClaimsUnsupported
	}
	if err := intakeContextError(tx.Statement.Context); err != nil {
		return err
	}
	result := tx.Model(&models.Operation{}).
		Where("id = ? AND owner_user_id = ? AND workspace_id = ? AND version = ?", op.ID, op.OwnerUserID, op.WorkspaceID, expectedVersion).
		Select("*").Omit("id", "created_at").Updates(&op)
	if err := intakeContextError(tx.Statement.Context); err != nil {
		return err
	}
	if result.Error != nil {
		return fmt.Errorf("update operation version: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return ErrStaleOperation
	}
	return nil
}

func recoveryReleaseStatus(status OperationStatus) bool {
	switch status {
	case StatusDraftReady, StatusAwaitingApproval, StatusBlocked, StatusFailed, StatusInterrupted, StatusCompleted, StatusWaitingExternal, StatusDismissed, StatusArchived:
		return true
	default:
		return false
	}
}
