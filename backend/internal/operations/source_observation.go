package operations

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync/atomic"
	"time"

	"automation-hub-backend/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrInvalidSourceObservation = errors.New("operations: invalid or expired source observation")
var ErrSourceObservationUnsupported = errors.New("operations: repository does not support durable source observations")

// Observation order is minted before reading, never inferred from completion
// time or user-provided timestamps. This is not yet a latest-source-head fence.
type SourceObservationStart struct {
	OwnerUserID, WorkspaceID string
	OriginID                 uuid.UUID
	ConfigDigest             string
	RegistryManaged          bool
	ConfigVersion            int64
}

type SourceObservation struct {
	ID                       uuid.UUID
	OwnerUserID, WorkspaceID string
	OriginID                 uuid.UUID
	ConfigDigest             string
	Generation               int64
	ConfigEpoch              int64
	StartedAt                time.Time
}

func (SourceObservation) TableName() string { return "operation_source_observations" }

type SourceObservationRepository interface {
	BeginSourceObservation(context.Context, SourceObservationStart) (SourceObservation, error)
}

type sourceObservationScope struct{ owner, workspace string }
type sourceObservationContextKey struct{}
type activeSourceObservation struct {
	observation SourceObservation
	authority   context.Context
	active      atomic.Bool
}

func observationContextError(ctx context.Context) error {
	if ctx == nil {
		return ErrInvalidSourceObservation
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if deadline, ok := ctx.Deadline(); ok && !time.Now().Before(deadline) {
		return context.DeadlineExceeded
	}
	return nil
}

func intakeContextError(ctx context.Context) error {
	if err := observationContextError(ctx); err != nil {
		return err
	}
	if ctx.Value(sourceObservationContextKey{}) != nil {
		if _, ok := CurrentSourceObservation(ctx); !ok {
			return ErrInvalidSourceObservation
		}
	}
	if ctx.Value(safeEffectContextKey{}) != nil {
		return safeEffectAuthorityError(ctx)
	}
	return nil
}

func validateObservationStart(start SourceObservationStart) error {
	if start.OriginID == uuid.Nil || !validSourceIdentityPart(start.OwnerUserID, 1024) ||
		!validSourceIdentityPart(start.WorkspaceID, 1024) || start.OwnerUserID != strings.TrimSpace(start.OwnerUserID) ||
		start.WorkspaceID != strings.TrimSpace(start.WorkspaceID) || len(start.ConfigDigest) != 64 ||
		start.ConfigDigest != strings.ToLower(start.ConfigDigest) {
		return ErrInvalidSourceObservation
	}
	if _, err := hex.DecodeString(start.ConfigDigest); err != nil {
		return ErrInvalidSourceObservation
	}
	return nil
}

func CurrentSourceObservation(ctx context.Context) (SourceObservation, bool) {
	if observationContextError(ctx) != nil {
		return SourceObservation{}, false
	}
	state, ok := ctx.Value(sourceObservationContextKey{}).(*activeSourceObservation)
	if !ok || state == nil || !state.active.Load() || observationContextError(state.authority) != nil {
		return SourceObservation{}, false
	}
	return state.observation, true
}

// WithSourceObservation commits the ticket before invoking the synchronous
// reader. It holds no persistence lock while a file or provider is being read.
func (s *Service) WithSourceObservation(ctx context.Context, start SourceObservationStart, read func(context.Context) error) error {
	if err := safeEffectAdmissionError(ctx); err != nil {
		return err
	}
	if err := validateObservationStart(start); err != nil {
		return err
	}
	if s == nil || read == nil {
		return ErrSourceObservationUnsupported
	}
	repo, ok := s.repo.(SourceObservationRepository)
	if !ok {
		return ErrSourceObservationUnsupported
	}
	if _, ok := s.repo.(ContextIntakeRepository); !ok {
		return ErrContextIntakeUnsupported
	}
	if ctx.Value(sourceObservationContextKey{}) != nil {
		return ErrInvalidSourceObservation
	}
	observation, err := repo.BeginSourceObservation(ctx, start)
	if err != nil {
		return err
	}
	if observation.ID == uuid.Nil || observation.Generation <= 0 || observation.ConfigEpoch <= 0 || observation.StartedAt.IsZero() ||
		observation.OwnerUserID != start.OwnerUserID || observation.WorkspaceID != start.WorkspaceID ||
		observation.OriginID != start.OriginID || observation.ConfigDigest != start.ConfigDigest {
		return ErrInvalidRepositoryResult
	}
	if err := safeEffectAdmissionError(ctx); err != nil {
		return err
	}
	authority, cancel := context.WithCancel(ctx)
	defer cancel()
	state := &activeSourceObservation{observation: observation, authority: authority}
	state.active.Store(true)
	defer state.active.Store(false)
	err = read(context.WithValue(authority, sourceObservationContextKey{}, state))
	return errors.Join(err, observationContextError(authority))
}

func bindSourceObservation(ctx context.Context, op *models.Operation) error {
	if ctx.Value(sourceObservationContextKey{}) == nil {
		return nil
	}
	observation, ok := CurrentSourceObservation(ctx)
	if !ok {
		return ErrInvalidSourceObservation
	}
	if op.SourceIdentityHash == "" || op.OwnerUserID != observation.OwnerUserID || op.WorkspaceID != observation.WorkspaceID ||
		op.AccountFeedID == nil || *op.AccountFeedID != observation.OriginID {
		return ErrInvalidSourceObservation
	}
	id := observation.ID
	op.SourceObservationID = &id
	op.SourceObservationGeneration = observation.Generation
	return nil
}

func observedIntakeContext(ctx context.Context) (context.Context, func(), error) {
	if err := intakeContextError(ctx); err != nil {
		return nil, nil, err
	}
	var authorities []context.Context
	if state, ok := ctx.Value(sourceObservationContextKey{}).(*activeSourceObservation); ok {
		authorities = append(authorities, state.authority)
	}
	if state, ok := ctx.Value(safeEffectContextKey{}).(*activeSafeEffect); ok {
		authorities = append(authorities, state.authority)
	}
	if len(authorities) == 0 {
		return ctx, func() {}, nil
	}
	// Retain every private scope and caller value while restoring cancellation
	// from all original authorities. Binding only the observation would strip a
	// nested effect scope or allow its shorter lease to be detached.
	bound, cancel := context.WithCancel(ctx)
	cleanups := []func(){cancel}
	for _, authority := range authorities {
		stop := context.AfterFunc(authority, cancel)
		cleanups = append(cleanups, func() { stop() })
		if deadline, ok := authority.Deadline(); ok {
			var cancelDeadline context.CancelFunc
			bound, cancelDeadline = context.WithDeadline(bound, deadline)
			cleanups = append(cleanups, cancelDeadline)
		}
	}
	finish := func() {
		for _, cleanup := range cleanups {
			cleanup()
		}
	}
	if err := intakeContextError(bound); err != nil {
		finish()
		return nil, nil, err
	}
	return bound, finish, nil
}

func (r *GormRepository) BeginSourceObservation(ctx context.Context, start SourceObservationStart) (SourceObservation, error) {
	if err := safeEffectAdmissionError(ctx); err != nil {
		return SourceObservation{}, err
	}
	if err := validateObservationStart(start); err != nil {
		return SourceObservation{}, err
	}
	if r == nil || r.DB == nil {
		return SourceObservation{}, ErrSourceObservationUnsupported
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var observation SourceObservation
	err := r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SET LOCAL lock_timeout = '5s'").Error; err != nil {
			return err
		}
		var epoch int64
		var origin SourceOrigin
		originErr := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("owner_user_id = ? AND workspace_id = ? AND origin_id = ?", start.OwnerUserID, start.WorkspaceID, start.OriginID).First(&origin).Error
		if originErr != nil && !errors.Is(originErr, gorm.ErrRecordNotFound) {
			return originErr
		}
		if originErr == nil && origin.RegistryManaged {
			if err := validateManagedSourceObservation(origin, start); err != nil {
				return err
			}
			epoch = origin.ConfigEpoch
		} else {
			if start.RegistryManaged {
				return ErrSourceHeadSuperseded
			}
			if err := tx.Raw(`INSERT INTO public.operation_source_origins (owner_user_id, workspace_id, origin_id, config_digest, config_epoch)
VALUES (?, ?, ?, ?, 1) ON CONFLICT (owner_user_id, workspace_id, origin_id) DO UPDATE
SET config_digest = EXCLUDED.config_digest,
config_epoch = operation_source_origins.config_epoch + CASE WHEN operation_source_origins.config_digest <> EXCLUDED.config_digest THEN 1 ELSE 0 END,
changed_at = CASE WHEN operation_source_origins.config_digest <> EXCLUDED.config_digest THEN clock_timestamp() ELSE operation_source_origins.changed_at END
WHERE NOT operation_source_origins.registry_managed AND
(operation_source_origins.config_digest = EXCLUDED.config_digest OR operation_source_origins.config_epoch < ?)
RETURNING config_epoch`, start.OwnerUserID, start.WorkspaceID, start.OriginID, start.ConfigDigest, int64(math.MaxInt64)).Row().Scan(&epoch); err != nil {
				return err
			}
		}
		var generation int64
		err := tx.Raw(`INSERT INTO public.operation_source_observation_clocks (owner_user_id, workspace_id, generation)
VALUES (?, ?, 1) ON CONFLICT (owner_user_id, workspace_id) DO UPDATE
SET generation = operation_source_observation_clocks.generation + 1
WHERE operation_source_observation_clocks.generation < ? RETURNING generation`, start.OwnerUserID, start.WorkspaceID, int64(math.MaxInt64)).Row().Scan(&generation)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrInvalidSourceObservation
		}
		if err != nil {
			return fmt.Errorf("allocate source observation: %w", err)
		}
		observation = SourceObservation{ID: uuid.New(), OwnerUserID: start.OwnerUserID, WorkspaceID: start.WorkspaceID,
			OriginID: start.OriginID, ConfigDigest: start.ConfigDigest, Generation: generation, ConfigEpoch: epoch}
		return tx.Raw(`INSERT INTO public.operation_source_observations
(id, owner_user_id, workspace_id, origin_id, config_digest, generation, config_epoch)
VALUES (?, ?, ?, ?, ?, ?, ?) RETURNING started_at`, observation.ID, start.OwnerUserID, start.WorkspaceID,
			start.OriginID, start.ConfigDigest, generation, epoch).Row().Scan(&observation.StartedAt)
	})
	if err != nil {
		return SourceObservation{}, err
	}
	return observation, nil
}

func (r *MemoryRepository) lockObservationContext(ctx context.Context) error {
	if err := intakeContextError(ctx); err != nil {
		return err
	}
	if r.mu.TryLock() {
		if err := intakeContextError(ctx); err != nil {
			r.mu.Unlock()
			return err
		}
		return nil
	}
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := intakeContextError(ctx); err != nil {
				return err
			}
			if r.mu.TryLock() {
				if err := intakeContextError(ctx); err != nil {
					r.mu.Unlock()
					return err
				}
				return nil
			}
		}
	}
}

func (r *MemoryRepository) BeginSourceObservation(ctx context.Context, start SourceObservationStart) (SourceObservation, error) {
	if err := safeEffectAdmissionError(ctx); err != nil {
		return SourceObservation{}, err
	}
	if err := validateObservationStart(start); err != nil {
		return SourceObservation{}, err
	}
	if r == nil {
		return SourceObservation{}, ErrSourceObservationUnsupported
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := r.lockObservationContext(ctx); err != nil {
		return SourceObservation{}, err
	}
	defer r.mu.Unlock()
	scope := sourceObservationScope{start.OwnerUserID, start.WorkspaceID}
	if r.observationClocks[scope] == math.MaxInt64 {
		return SourceObservation{}, ErrInvalidSourceObservation
	}
	originKey := sourceOriginKey{start.OwnerUserID, start.WorkspaceID, start.OriginID}
	origin, exists := r.sourceOrigins[originKey]
	if (exists && origin.RegistryManaged) || start.RegistryManaged {
		if err := validateManagedSourceObservation(origin, start); err != nil {
			return SourceObservation{}, err
		}
	}
	if !exists {
		origin = SourceOrigin{OwnerUserID: start.OwnerUserID, WorkspaceID: start.WorkspaceID, OriginID: start.OriginID, ConfigEpoch: 1}
	}
	if exists && origin.ConfigDigest != start.ConfigDigest {
		if origin.ConfigEpoch == math.MaxInt64 {
			return SourceObservation{}, ErrInvalidSourceObservation
		}
		origin.ConfigEpoch++
	}
	if origin.ConfigDigest != start.ConfigDigest {
		origin.ChangedAt = time.Now().UTC()
	}
	origin.ConfigDigest = start.ConfigDigest
	observation := SourceObservation{ID: uuid.New(), OwnerUserID: start.OwnerUserID, WorkspaceID: start.WorkspaceID,
		OriginID: start.OriginID, ConfigDigest: start.ConfigDigest, Generation: r.observationClocks[scope] + 1, ConfigEpoch: origin.ConfigEpoch, StartedAt: time.Now().UTC()}
	if r.observationClocks == nil {
		r.observationClocks = make(map[sourceObservationScope]int64)
	}
	if r.observations == nil {
		r.observations = make(map[uuid.UUID]SourceObservation)
	}
	if err := intakeContextError(ctx); err != nil {
		return SourceObservation{}, err
	}
	if r.sourceOrigins == nil {
		r.sourceOrigins = make(map[sourceOriginKey]SourceOrigin)
	}
	r.sourceOrigins[originKey] = origin
	r.observationClocks[scope] = observation.Generation
	r.observations[observation.ID] = observation
	return observation, nil
}

// Called under the memory store lock; mirrors durable observation provenance.
func (r *MemoryRepository) validateObservationReference(op models.Operation) error {
	if op.SourceObservationID == nil {
		return nil
	}
	observation, ok := r.observations[*op.SourceObservationID]
	if !ok || observation.OwnerUserID != op.OwnerUserID || observation.WorkspaceID != op.WorkspaceID ||
		observation.Generation != op.SourceObservationGeneration || op.AccountFeedID == nil ||
		*op.AccountFeedID != observation.OriginID {
		return ErrInvalidSourceObservation
	}
	return nil
}

func validateGormObservationReference(db *gorm.DB, op models.Operation) error {
	if op.SourceObservationID == nil {
		return nil
	}
	if op.AccountFeedID == nil {
		return ErrInvalidSourceObservation
	}
	var count int64
	if err := db.Model(&SourceObservation{}).Where("id = ? AND owner_user_id = ? AND workspace_id = ? AND generation = ? AND origin_id = ?",
		*op.SourceObservationID, op.OwnerUserID, op.WorkspaceID, op.SourceObservationGeneration, *op.AccountFeedID).Count(&count).Error; err != nil {
		return err
	}
	if count != 1 {
		return ErrInvalidSourceObservation
	}
	return nil
}

// Value snapshots must not leak writable pointers back into the stored record.
func cloneOperation(op models.Operation) models.Operation {
	op.SourceID = clonePointer(op.SourceID)
	op.SourceReceivedAt = clonePointer(op.SourceReceivedAt)
	op.SourceObservationID = clonePointer(op.SourceObservationID)
	op.PursuitID = clonePointer(op.PursuitID)
	op.WorkflowID = clonePointer(op.WorkflowID)
	op.AccountFeedID = clonePointer(op.AccountFeedID)
	op.ApprovalID = clonePointer(op.ApprovalID)
	op.NextReviewAt = clonePointer(op.NextReviewAt)
	op.CompletedAt = clonePointer(op.CompletedAt)
	return op
}

func clonePointer[T any](ptr *T) *T {
	if ptr == nil {
		return nil
	}
	value := *ptr
	return &value
}

func sameSourceObservation(a, b models.Operation) bool {
	if a.SourceObservationGeneration != b.SourceObservationGeneration {
		return false
	}
	if a.SourceObservationID == nil || b.SourceObservationID == nil {
		return a.SourceObservationID == nil && b.SourceObservationID == nil
	}
	return *a.SourceObservationID == *b.SourceObservationID
}
