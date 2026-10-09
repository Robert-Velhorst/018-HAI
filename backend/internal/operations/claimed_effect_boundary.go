package operations

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"automation-hub-backend/internal/infra"
	"automation-hub-backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrSafeEffectUnsupported = errors.New("operations: repository does not support a locked safe effect boundary")
var ErrSafeEffectPoolCapacity = errors.New("operations: safe effect boundary requires at least two database connections")
var ErrInvalidSafeEffectAuthority = errors.New("operations: invalid, expired or nested safe effect authority")

const safeEffectTimeout = 5 * time.Second

// SafeEffectScope is copied from locked server state, never from a payload. It
// binds a bounded local effect to one running version and live worker claim.
type SafeEffectScope struct {
	OperationID                 uuid.UUID `json:"operationId"`
	Version                     int64     `json:"version"`
	OwnerUserID                 string    `json:"ownerUserId"`
	WorkspaceID                 string    `json:"workspaceId"`
	ClaimOwner                  uuid.UUID `json:"claimOwner"`
	ClaimGeneration             int64     `json:"claimGeneration"`
	SourceIdentityHash          string    `json:"sourceIdentityHash,omitempty"`
	SourceRevisionHash          string    `json:"sourceRevisionHash,omitempty"`
	SourceObservationID         string    `json:"sourceObservationId,omitempty"`
	SourceObservationGeneration int64     `json:"sourceObservationGeneration,omitempty"`
	SourceHeadGeneration        int64     `json:"sourceHeadGeneration,omitempty"`
	SourceConfigEpoch           int64     `json:"sourceConfigEpoch,omitempty"`
}

type safeEffectContextKey struct{}
type activeSafeEffect struct {
	scope     SafeEffectScope
	authority context.Context
	active    atomic.Bool
}

// ValidateExecutionContext rejects absent, expired and ended private authority.
// It does not grant execution: claim, policy and final-effect checks still apply.
func ValidateExecutionContext(ctx context.Context) error {
	return intakeContextError(ctx)
}

// BindExecutionContext retains private server cancellation across detached
// caller contexts. The returned cleanup must be called by the synchronous user.
func BindExecutionContext(ctx context.Context) (context.Context, func(), error) {
	return observedIntakeContext(ctx)
}

// CurrentSafeEffectScope returns a value copy only while the server boundary
// is active. Retaining the callback context cannot extend that authority.
func CurrentSafeEffectScope(ctx context.Context) (SafeEffectScope, bool) {
	if intakeContextError(ctx) != nil || safeEffectAuthorityError(ctx) != nil {
		return SafeEffectScope{}, false
	}
	return ctx.Value(safeEffectContextKey{}).(*activeSafeEffect).scope, true
}

func safeEffectAuthorityError(ctx context.Context) error {
	if err := observationContextError(ctx); err != nil {
		return err
	}
	state, ok := ctx.Value(safeEffectContextKey{}).(*activeSafeEffect)
	if !ok || state == nil || !state.active.Load() || observationContextError(state.authority) != nil {
		return ErrInvalidSafeEffectAuthority
	}
	// A busy scheduler can delay the cancellation timer. The server deadline is
	// still authoritative even while Context.Err has not observed that timer.
	deadline, bounded := state.authority.Deadline()
	if !bounded || !time.Now().Before(deadline) {
		return ErrInvalidSafeEffectAuthority
	}
	return nil
}

func safeEffectAdmissionError(ctx context.Context) error {
	if err := intakeContextError(ctx); err != nil {
		return err
	}
	// Nested calls would wait on their own held operation/claim lock. A retained
	// callback context must never mint a fresh effect scope after it has ended.
	if ctx.Value(safeEffectContextKey{}) != nil {
		return ErrInvalidSafeEffectAuthority
	}
	return nil
}

// SafeEffectRepository must retain the operation and claim locks until effect
// returns. Callbacks must be synchronous and must not reenter this repository.
// An error after callback entry never establishes absence of a filesystem effect.
type SafeEffectRepository interface {
	WithClaimedSafeEffect(context.Context, ExecutionClaim, models.Operation, func(context.Context) error) error
}

func (s *Service) WithClaimedSafeEffect(ctx context.Context, claim ExecutionClaim, expected models.Operation, effect func(context.Context) error) error {
	if err := safeEffectAdmissionError(ctx); err != nil {
		return err
	}
	if s == nil || effect == nil {
		return ErrSafeEffectUnsupported
	}
	repo, ok := s.repo.(SafeEffectRepository)
	if !ok {
		return ErrSafeEffectUnsupported
	}
	return repo.WithClaimedSafeEffect(ctx, claim, expected, effect)
}

func validateSafeEffectSnapshot(claim ExecutionClaim, current, expected models.Operation) error {
	if claim.OperationID == uuid.Nil || claim.Owner == uuid.Nil || claim.Generation <= 0 ||
		claim.OwnerUserID == "" || claim.WorkspaceID == "" ||
		current.ID != claim.OperationID || current.OwnerUserID != claim.OwnerUserID || current.WorkspaceID != claim.WorkspaceID ||
		expected.ID != current.ID || expected.OwnerUserID != current.OwnerUserID || expected.WorkspaceID != current.WorkspaceID {
		return ErrClaimLost
	}
	if current.Version <= 0 || expected.Version != current.Version {
		return ErrStaleOperation
	}
	// Live mode/block policy uses this content. A caller copy with a valid
	// version must not substitute different text or a different task category.
	if current.Title != expected.Title || current.Description != expected.Description || current.OperationType != expected.OperationType {
		return ErrStaleOperation
	}
	if err := validateSourceIdentityMutation(current, expected); err != nil {
		return err
	}
	// Legacy records are also revision-bound for this effect even though their
	// general edit path still permits changing the old opaque revision field.
	if expected.SourceRevisionHash != current.SourceRevisionHash {
		return ErrSourceIdentityImmutable
	}
	if current.Status != string(StatusRunning) || current.RuntimeID != "hai-local-safe-worker" ||
		current.VerificationStatus != string(VerificationPending) || current.RequiresApproval ||
		current.CurrentDecision != string(DecisionRunSafeLocalWorker) || current.RiskLevel != string(RiskLow) ||
		current.AutonomyLevel != string(AutonomyAuto) || current.OwnerType != string(OwnerHAI) {
		if !IsSourceDerived(current) || current.Status != string(StatusRunning) ||
			current.RuntimeID != sourceApprovalSafeRuntimeID || current.VerificationStatus != string(VerificationPending) ||
			!current.RequiresApproval || current.CurrentDecision != string(DecisionAskRobert) ||
			current.AutonomyLevel != string(AutonomyApproval) || current.OwnerType != string(OwnerRobert) {
			return ErrOperationNotClaimable
		}
	}
	return Validate(current)
}

func invokeClaimedSafeEffect(ctx context.Context, claim ExecutionClaim, current models.Operation, head *SourceHead, leaseDeadline time.Time, effect func(context.Context) error) error {
	if err := safeEffectAdmissionError(ctx); err != nil {
		return err
	}
	if !leaseDeadline.After(time.Now()) {
		return ErrClaimLost
	}
	ctx, cancel := context.WithDeadline(ctx, leaseDeadline)
	defer cancel()
	state := &activeSafeEffect{authority: ctx, scope: SafeEffectScope{
		OperationID: current.ID, Version: current.Version, OwnerUserID: current.OwnerUserID,
		WorkspaceID: current.WorkspaceID, ClaimOwner: claim.Owner, ClaimGeneration: claim.Generation,
		SourceIdentityHash: current.SourceIdentityHash, SourceRevisionHash: current.SourceRevisionHash,
	}}
	if current.SourceObservationID != nil {
		state.scope.SourceObservationID = current.SourceObservationID.String()
		state.scope.SourceObservationGeneration = current.SourceObservationGeneration
	}
	if head != nil {
		state.scope.SourceHeadGeneration = head.ObservationGeneration
		state.scope.SourceConfigEpoch = head.ConfigEpoch
	}
	state.active.Store(true)
	defer state.active.Store(false)
	if err := intakeContextError(ctx); err != nil {
		return err
	}
	err := effect(context.WithValue(ctx, safeEffectContextKey{}, state))
	if authorityErr := intakeContextError(ctx); authorityErr != nil {
		return errors.Join(err, authorityErr)
	}
	return err
}

func (r *MemoryRepository) WithClaimedSafeEffect(ctx context.Context, claim ExecutionClaim, expected models.Operation, effect func(context.Context) error) error {
	if err := safeEffectAdmissionError(ctx); err != nil {
		return err
	}
	if r == nil || effect == nil {
		return ErrSafeEffectUnsupported
	}
	ctx, finish, err := observedIntakeContext(ctx)
	if err != nil {
		return err
	}
	defer finish()
	ctx, cancel := context.WithTimeout(ctx, safeEffectTimeout)
	defer cancel()
	if err := r.lockObservationContext(ctx); err != nil {
		return err
	}
	defer r.mu.Unlock()
	if err := safeEffectAdmissionError(ctx); err != nil {
		return err
	}
	current, exists := r.ops[claim.OperationID]
	if !exists {
		return ErrClaimLost
	}
	if err := validateSafeEffectSnapshot(claim, current, expected); err != nil {
		return err
	}
	stored, exists := r.claims[claim.OperationID]
	if !exists || !matchesMemoryClaim(stored, claim) {
		return ErrClaimLost
	}
	var head *SourceHead
	if current.SourceIdentityHash != "" {
		accepted, ok := r.sourceHeads[sourceHeadKey{current.OwnerUserID, current.WorkspaceID, current.SourceIdentityHash}]
		if !ok || r.validateObservationReference(current) != nil {
			return ErrSourceHeadReconciliation
		}
		origin := r.sourceOrigins[sourceOriginKey{current.OwnerUserID, current.WorkspaceID, accepted.OriginID}]
		if err := validateExecutableHead(current, accepted, origin); err != nil {
			return err
		}
		if err := validateHeadObservation(accepted, r.observations[accepted.ObservationID], origin); err != nil {
			return err
		}
		head = &accepted
	}
	return invokeClaimedSafeEffect(ctx, claim, current, head, stored.expiresAt, effect)
}

func (r *GormRepository) WithClaimedSafeEffect(ctx context.Context, claim ExecutionClaim, expected models.Operation, effect func(context.Context) error) error {
	if err := safeEffectAdmissionError(ctx); err != nil {
		return err
	}
	if r == nil || r.DB == nil || effect == nil {
		return ErrSafeEffectUnsupported
	}
	ctx, finish, err := observedIntakeContext(ctx)
	if err != nil {
		return err
	}
	defer finish()
	ctx, cancel := context.WithTimeout(ctx, safeEffectTimeout)
	defer cancel()
	sqlDB, err := r.DB.DB()
	if err != nil {
		return fmt.Errorf("inspect safe effect database pool: %w", err)
	}
	// The locked transaction owns one connection; durable authorization must
	// commit independently so a later effect-boundary rollback cannot erase it.
	if maxOpen := sqlDB.Stats().MaxOpenConnections; maxOpen > 0 && maxOpen < 2 {
		return ErrSafeEffectPoolCapacity
	}
	return infra.WithPostgresExecutionConnections(ctx, r.DB, func(lockDB *gorm.DB, reservedCtx context.Context) error {
		ctx = reservedCtx
		return lockDB.Transaction(func(tx *gorm.DB) error {
			if err := safeEffectAdmissionError(ctx); err != nil {
				return err
			}
			// Publication and effect use origin -> head -> operation -> claim.
			// Other operation-only mutations never acquire origin/head afterward.
			if err := tx.Exec("SET LOCAL lock_timeout = '5s'").Error; err != nil {
				return fmt.Errorf("set safe effect lock timeout: %w", err)
			}
			var head *SourceHead
			var origin SourceOrigin
			var sourceErr error
			if expected.SourceIdentityHash != "" {
				if expected.AccountFeedID == nil || expected.SourceObservationID == nil {
					sourceErr = ErrSourceHeadReconciliation
				} else {
					origin, sourceErr = lockSourceOrigin(tx, claim.OwnerUserID, claim.WorkspaceID, *expected.AccountFeedID)
					if sourceErr != nil {
						return sourceErr
					}
					head, sourceErr = lockSourceHead(tx, claim.OwnerUserID, claim.WorkspaceID, expected.SourceIdentityHash)
					if sourceErr != nil {
						return sourceErr
					}
				}
			}
			var current models.Operation
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
				Where("id = ? AND owner_user_id = ? AND workspace_id = ?", claim.OperationID, claim.OwnerUserID, claim.WorkspaceID).
				First(&current).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return ErrClaimLost
				}
				return fmt.Errorf("lock operation at safe effect boundary: %w", err)
			}
			if err := validateSafeEffectSnapshot(claim, current, expected); err != nil {
				return err
			}
			if current.SourceIdentityHash != "" {
				if sourceErr != nil {
					return sourceErr
				}
				if head == nil {
					return ErrSourceHeadReconciliation
				}
				if err := validateGormObservationReference(tx, current); err != nil {
					return err
				}
				if err := validateExecutableHead(current, *head, origin); err != nil {
					return err
				}
				if err := validateGormHeadObservation(tx, *head, origin); err != nil {
					return err
				}
			}
			var stored executionClaimRow
			if err := tx.Raw(`SELECT claim_owner AS owner, generation, lease_expires_at AS expires_at
FROM public.operation_execution_claims WHERE operation_id = ?
AND claim_owner IS NOT NULL AND lease_expires_at IS NOT NULL FOR UPDATE`, claim.OperationID).
				Row().Scan(&stored.Owner, &stored.Generation, &stored.ExpiresAt); err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return ErrClaimLost
				}
				return fmt.Errorf("lock claim at safe effect boundary: %w", err)
			}
			if stored.Owner != claim.Owner || stored.Generation != claim.Generation {
				return ErrClaimLost
			}
			var dbNow time.Time
			clockReadStarted := time.Now()
			if err := tx.Raw("SELECT clock_timestamp()").Row().Scan(&dbNow); err != nil {
				return fmt.Errorf("read safe effect database time: %w", err)
			}
			// Anchor before the clock query so its latency cannot extend the lease.
			// Use the earlier wall-clock expiry too; clock skew can refuse work early,
			// but must never extend this authority beyond either conservative bound.
			deadline := clockReadStarted.Add(stored.ExpiresAt.Sub(dbNow))
			if stored.ExpiresAt.Before(deadline) {
				deadline = stored.ExpiresAt
			}
			if !deadline.After(time.Now()) {
				return ErrClaimLost
			}
			if err := infra.TightenPostgresExecutionDeadline(ctx, deadline); err != nil {
				return err
			}
			return invokeClaimedSafeEffect(ctx, claim, current, head, deadline, effect)
		})
	})
}
