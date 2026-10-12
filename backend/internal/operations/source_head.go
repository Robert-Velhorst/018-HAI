package operations

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"automation-hub-backend/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrSourceHeadUnsupported = errors.New("operations: repository does not support accepted source heads")
var ErrSourceHeadSuperseded = errors.New("operations: source observation or operation has been superseded")
var ErrSourceHeadReconciliation = errors.New("operations: source authority requires reconciliation")

type sourceHeadKey struct{ owner, workspace, identity string }
type sourceOriginKey struct {
	owner, workspace string
	origin           uuid.UUID
}
type sourceRevisionKey struct {
	head   sourceHeadKey
	digest string
}

type SourceOrigin struct {
	OwnerUserID, WorkspaceID string
	OriginID                 uuid.UUID
	ConfigDigest             string
	ConfigEpoch              int64
	RegistryManaged          bool
	Enabled                  bool
	ConfigVersion            int64 `gorm:"column:registry_config_version"`
	ChangedAt                time.Time
}

func (SourceOrigin) TableName() string { return "operation_source_origins" }

// A head is the latest accepted durable observation, not a claim about an
// unobserved provider change. Its original operation provenance stays immutable.
type SourceHead struct {
	OwnerUserID, WorkspaceID           string
	SourceIdentityHash                 string
	OriginID, OperationID              uuid.UUID
	RevisionHash                       string
	ObservationID                      uuid.UUID
	ObservationGeneration, ConfigEpoch int64
	State                              string
	UpdatedAt                          time.Time
}

func (SourceHead) TableName() string { return "operation_source_heads" }

func sameObservationRecord(a, b SourceObservation) bool {
	return a.ID == b.ID && a.OwnerUserID == b.OwnerUserID && a.WorkspaceID == b.WorkspaceID && a.OriginID == b.OriginID &&
		a.ConfigDigest == b.ConfigDigest && a.ConfigEpoch == b.ConfigEpoch && a.Generation == b.Generation && a.StartedAt.Equal(b.StartedAt)
}

type sourceHeadRevision struct {
	OwnerUserID, WorkspaceID, SourceIdentityHash, RevisionDigest string
	OperationID, ObservationID                                   uuid.UUID
}

func (sourceHeadRevision) TableName() string { return "operation_source_head_revisions" }

type SourceHeadRepository interface {
	PublishSourceHead(context.Context, models.Operation) error
}

func sourceRevisionDigest(revision string) string {
	digest := sha256.Sum256([]byte(revision))
	return hex.EncodeToString(digest[:])
}

func validateHeadPublication(op models.Operation, observation SourceObservation, origin SourceOrigin) error {
	if origin.RegistryManaged && !origin.Enabled {
		return ErrSourceHeadSuperseded
	}
	if validateOperationSourceIdentity(op) != nil || op.ID == uuid.Nil || op.SourceIdentityHash == "" ||
		op.SourceObservationID == nil || op.AccountFeedID == nil || *op.AccountFeedID != observation.OriginID ||
		op.OwnerUserID != observation.OwnerUserID || op.WorkspaceID != observation.WorkspaceID {
		return ErrSourceHeadReconciliation
	}
	if observation.ConfigEpoch <= 0 || observation.ConfigEpoch != origin.ConfigEpoch || observation.ConfigDigest != origin.ConfigDigest ||
		origin.OwnerUserID != observation.OwnerUserID || origin.WorkspaceID != observation.WorkspaceID || origin.OriginID != observation.OriginID {
		return ErrSourceHeadSuperseded
	}
	return nil
}

func nextSourceHead(current *SourceHead, op models.Operation, observation SourceObservation, seenRevision bool) (SourceHead, bool, error) {
	next := SourceHead{OwnerUserID: op.OwnerUserID, WorkspaceID: op.WorkspaceID, SourceIdentityHash: op.SourceIdentityHash,
		OriginID: observation.OriginID, OperationID: op.ID, RevisionHash: op.SourceRevisionHash,
		ObservationID: observation.ID, ObservationGeneration: observation.Generation, ConfigEpoch: observation.ConfigEpoch,
		State: "accepted", UpdatedAt: time.Now().UTC()}
	if current != nil {
		if current.OriginID != observation.OriginID {
			return SourceHead{}, false, ErrSourceHeadReconciliation
		}
		if observation.Generation < current.ObservationGeneration {
			return SourceHead{}, false, ErrSourceHeadSuperseded
		}
		if observation.Generation == current.ObservationGeneration {
			if current.ObservationID != observation.ID || current.OperationID != op.ID || current.RevisionHash != op.SourceRevisionHash || current.ConfigEpoch != observation.ConfigEpoch {
				return SourceHead{}, false, ErrSourceHeadReconciliation
			}
			return *current, false, nil
		}
		// An old consumed operation, archived/recreated dedupe row or A-B-A
		// recurrence cannot silently become a fresh execution opportunity.
		if current.State != "accepted" || (seenRevision && (current.OperationID != op.ID || current.RevisionHash != op.SourceRevisionHash)) {
			next.State = "reconciliation_required"
		}
	} else if seenRevision {
		next.State = "reconciliation_required"
	}
	return next, true, nil
}

func validatePublicationOrder(op models.Operation, observation SourceObservation, origin SourceOrigin, head *SourceHead) error {
	if err := validateHeadPublication(op, observation, origin); err != nil {
		return err
	}
	if head != nil {
		if head.OriginID != observation.OriginID {
			return ErrSourceHeadReconciliation
		}
		if head.ObservationGeneration > observation.Generation {
			return ErrSourceHeadSuperseded
		}
	}
	return nil
}

// Called before any observation-backed write, including duplicate evidence
// refresh. Publication later checks the committed operation again; creation
// and acceptance remain separate transactions and an unaccepted row is inert.
func lockIntakeSourceAuthority(tx *gorm.DB, op models.Operation) error {
	observation, active := CurrentSourceObservation(tx.Statement.Context)
	if !active {
		return nil
	}
	origin, err := lockSourceOrigin(tx, observation.OwnerUserID, observation.WorkspaceID, observation.OriginID)
	if err != nil {
		return err
	}
	head, err := lockSourceHead(tx, op.OwnerUserID, op.WorkspaceID, op.SourceIdentityHash)
	if err != nil {
		return err
	}
	return validatePublicationOrder(op, observation, origin, head)
}

func (r *MemoryRepository) validateIntakeSourceAuthority(ctx context.Context, op models.Operation) error {
	observation, active := CurrentSourceObservation(ctx)
	if !active {
		return nil
	}
	origin := r.sourceOrigins[sourceOriginKey{observation.OwnerUserID, observation.WorkspaceID, observation.OriginID}]
	var head *SourceHead
	if current, exists := r.sourceHeads[sourceHeadKey{op.OwnerUserID, op.WorkspaceID, op.SourceIdentityHash}]; exists {
		head = &current
	}
	return validatePublicationOrder(op, observation, origin, head)
}

func validateExecutableHead(op models.Operation, head SourceHead, origin SourceOrigin) error {
	if origin.RegistryManaged && !origin.Enabled {
		return ErrSourceHeadSuperseded
	}
	if head.State != "accepted" || op.SourceObservationID == nil || op.AccountFeedID == nil ||
		head.SourceIdentityHash != op.SourceIdentityHash || head.OwnerUserID != op.OwnerUserID || head.WorkspaceID != op.WorkspaceID ||
		head.OriginID != *op.AccountFeedID || head.ConfigEpoch <= 0 || head.ObservationGeneration <= 0 || head.ObservationID == uuid.Nil ||
		op.SourceObservationGeneration <= 0 {
		return ErrSourceHeadReconciliation
	}
	if head.OperationID != op.ID || head.RevisionHash != op.SourceRevisionHash || head.ConfigEpoch != origin.ConfigEpoch ||
		origin.OwnerUserID != op.OwnerUserID || origin.WorkspaceID != op.WorkspaceID || origin.OriginID != head.OriginID {
		return ErrSourceHeadSuperseded
	}
	return nil
}

func validateHeadObservation(head SourceHead, observation SourceObservation, origin SourceOrigin) error {
	if head.ObservationID != observation.ID || head.OwnerUserID != observation.OwnerUserID || head.WorkspaceID != observation.WorkspaceID ||
		head.OriginID != observation.OriginID || head.ObservationGeneration != observation.Generation || head.ConfigEpoch != observation.ConfigEpoch ||
		observation.ConfigDigest != origin.ConfigDigest {
		return ErrSourceHeadReconciliation
	}
	return nil
}

func validateGormHeadObservation(tx *gorm.DB, head SourceHead, origin SourceOrigin) error {
	var count int64
	if err := tx.Model(&SourceObservation{}).Where("id = ? AND owner_user_id = ? AND workspace_id = ? AND generation = ? AND origin_id = ? AND config_epoch = ? AND config_digest = ?",
		head.ObservationID, head.OwnerUserID, head.WorkspaceID, head.ObservationGeneration, head.OriginID, head.ConfigEpoch, origin.ConfigDigest).Count(&count).Error; err != nil {
		return err
	}
	if count != 1 {
		return ErrSourceHeadReconciliation
	}
	return nil
}

func headPublicationEvent(head SourceHead) models.OperationEvent {
	payload, _ := json.Marshal(map[string]any{"observationId": head.ObservationID, "generation": head.ObservationGeneration,
		"configEpoch": head.ConfigEpoch, "state": head.State})
	return models.OperationEvent{ID: uuid.New(), OperationID: head.OperationID, EventType: "source_head_published",
		ActorType: string(OwnerHAI), Message: "source authority recorded: " + head.State, PayloadJSON: string(payload), CreatedAt: head.UpdatedAt}
}

func (r *MemoryRepository) PublishSourceHead(ctx context.Context, op models.Operation) error {
	if err := safeEffectAdmissionError(ctx); err != nil {
		return err
	}
	observation, ok := CurrentSourceObservation(ctx)
	if !ok {
		return ErrInvalidSourceObservation
	}
	if r == nil {
		return ErrSourceHeadUnsupported
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
	storedObservation, ok := r.observations[observation.ID]
	if !ok || !sameObservationRecord(storedObservation, observation) {
		return ErrInvalidSourceObservation
	}
	origin := r.sourceOrigins[sourceOriginKey{op.OwnerUserID, op.WorkspaceID, observation.OriginID}]
	if err := validateHeadPublication(op, observation, origin); err != nil {
		return err
	}
	stored, ok := r.ops[op.ID]
	if !ok || !sameSourceIdentity(stored, op) || stored.SourceRevisionHash != op.SourceRevisionHash || !sameSourceObservation(stored, op) ||
		stored.OwnerUserID != op.OwnerUserID || stored.WorkspaceID != op.WorkspaceID || r.validateObservationReference(stored) != nil {
		return ErrSourceHeadReconciliation
	}
	key := sourceHeadKey{op.OwnerUserID, op.WorkspaceID, op.SourceIdentityHash}
	var current *SourceHead
	if head, exists := r.sourceHeads[key]; exists {
		current = &head
	}
	revisionKey := sourceRevisionKey{key, sourceRevisionDigest(op.SourceRevisionHash)}
	_, seen := r.sourceHeadRevisions[revisionKey]
	next, changed, err := nextSourceHead(current, stored, observation, seen)
	if err != nil {
		return err
	}
	if err := intakeContextError(ctx); err != nil {
		return err
	}
	if changed {
		if r.sourceHeads == nil {
			r.sourceHeads = make(map[sourceHeadKey]SourceHead)
		}
		if r.sourceHeadRevisions == nil {
			r.sourceHeadRevisions = make(map[sourceRevisionKey]sourceHeadRevision)
		}
		r.sourceHeads[key] = next
		if next.State == "accepted" && !seen {
			r.sourceHeadRevisions[revisionKey] = sourceHeadRevision{op.OwnerUserID, op.WorkspaceID, op.SourceIdentityHash, revisionKey.digest, op.ID, observation.ID}
		}
		r.events = append(r.events, headPublicationEvent(next))
	}
	if next.State != "accepted" {
		return ErrSourceHeadReconciliation
	}
	return nil
}

func lockSourceOrigin(tx *gorm.DB, owner, workspace string, originID uuid.UUID) (SourceOrigin, error) {
	var origin SourceOrigin
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("owner_user_id = ? AND workspace_id = ? AND origin_id = ?", owner, workspace, originID).First(&origin).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return SourceOrigin{}, ErrSourceHeadReconciliation
	}
	return origin, err
}

func lockSourceHead(tx *gorm.DB, owner, workspace, identity string) (*SourceHead, error) {
	var head SourceHead
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("owner_user_id = ? AND workspace_id = ? AND source_identity_hash = ?", owner, workspace, identity).First(&head).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &head, err
}

func validateSourceApprovalRevision(current, expected models.Operation, version int64, digest string) error {
	if current.ID != expected.ID || current.OwnerUserID != expected.OwnerUserID || current.WorkspaceID != expected.WorkspaceID ||
		current.Version != version || expected.Version != version || current.Status != string(StatusAwaitingApproval) ||
		!current.RequiresApproval || !IsSourceDerived(current) || current.SourceIdentityHash != expected.SourceIdentityHash {
		return ErrSourceApprovalStale
	}
	currentDigest, err := sourceOperationRevisionDigest(current)
	if err != nil {
		return err
	}
	expectedDigest, err := sourceOperationRevisionDigest(expected)
	if err != nil {
		return err
	}
	if !validSHA256(digest) || !strings.EqualFold(currentDigest, digest) || !strings.EqualFold(expectedDigest, digest) {
		return ErrSourceApprovalStale
	}
	return nil
}

func (r *MemoryRepository) validateSourceApprovalHeadLocked(op models.Operation) error {
	if op.SourceIdentityHash == "" {
		return nil
	}
	if op.SourceObservationID == nil || op.AccountFeedID == nil {
		return ErrSourceHeadReconciliation
	}
	head, ok := r.sourceHeads[sourceHeadKey{op.OwnerUserID, op.WorkspaceID, op.SourceIdentityHash}]
	if !ok {
		return ErrSourceHeadReconciliation
	}
	origin := r.sourceOrigins[sourceOriginKey{op.OwnerUserID, op.WorkspaceID, head.OriginID}]
	if err := validateExecutableHead(op, head, origin); err != nil {
		return err
	}
	observation, ok := r.observations[head.ObservationID]
	if !ok {
		return ErrSourceHeadReconciliation
	}
	return validateHeadObservation(head, observation, origin)
}

// The repository mutex is the memory implementation of the same origin ->
// head -> operation serialization used by the PostgreSQL row locks.
func (r *MemoryRepository) ValidateSourceApproval(ctx context.Context, expected models.Operation, version int64, digest string) error {
	if err := intakeContextError(ctx); err != nil {
		return err
	}
	if r == nil {
		return ErrSourceHeadUnsupported
	}
	if err := r.lockObservationContext(ctx); err != nil {
		return err
	}
	defer r.mu.Unlock()
	if err := intakeContextError(ctx); err != nil {
		return err
	}
	current, ok := r.ops[expected.ID]
	if !ok {
		return ErrNotFound
	}
	if err := validateSourceApprovalRevision(current, expected, version, digest); err != nil {
		return err
	}
	return r.validateSourceApprovalHeadLocked(current)
}

func (r *MemoryRepository) ApproveSourceDerived(ctx context.Context, expected, updated models.Operation, event models.OperationEvent, digest string) (*models.Operation, error) {
	if err := intakeContextError(ctx); err != nil {
		return nil, err
	}
	if r == nil {
		return nil, ErrSourceHeadUnsupported
	}
	if err := validateMutationEvent(&updated, &event); err != nil {
		return nil, err
	}
	if err := validateSourceApprovalReceiptEvent(expected, updated, event, digest); err != nil {
		return nil, err
	}
	if err := r.lockObservationContext(ctx); err != nil {
		return nil, err
	}
	defer r.mu.Unlock()
	if err := intakeContextError(ctx); err != nil {
		return nil, err
	}
	current, ok := r.ops[expected.ID]
	if !ok {
		return nil, ErrNotFound
	}
	if err := validateSourceApprovalRevision(current, expected, expected.Version, digest); err != nil {
		return nil, err
	}
	if err := r.validateSourceApprovalHeadLocked(current); err != nil {
		return nil, err
	}
	if err := validateSourceIdentityMutation(current, updated); err != nil {
		return nil, err
	}
	if err := validateMutationState(current, updated, &event); err != nil {
		return nil, err
	}
	if claim, ok := r.claims[updated.ID]; ok && claim.owner != uuid.Nil {
		return nil, ErrOperationClaimed
	}
	if event.ID == uuid.Nil {
		event.ID = uuid.New()
	}
	for _, existing := range r.events {
		if existing.ID == event.ID {
			return nil, errors.New("operations: duplicate mutation event ID")
		}
	}
	if err := intakeContextError(ctx); err != nil {
		return nil, err
	}
	updated.CreatedAt = current.CreatedAt
	r.ops[updated.ID] = cloneOperation(updated)
	r.events = append(r.events, event)
	saved := cloneOperation(updated)
	return &saved, nil
}

func (r *GormRepository) lockSourceApprovalOperation(tx *gorm.DB, expected models.Operation, digest string) (models.Operation, error) {
	var origin SourceOrigin
	var head *SourceHead
	if expected.SourceIdentityHash != "" {
		if expected.AccountFeedID == nil || expected.SourceObservationID == nil {
			return models.Operation{}, ErrSourceHeadReconciliation
		}
		var err error
		origin, err = lockSourceOrigin(tx, expected.OwnerUserID, expected.WorkspaceID, *expected.AccountFeedID)
		if err != nil {
			return models.Operation{}, err
		}
		head, err = lockSourceHead(tx, expected.OwnerUserID, expected.WorkspaceID, expected.SourceIdentityHash)
		if err != nil {
			return models.Operation{}, err
		}
	}
	var current models.Operation
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ? AND owner_user_id = ? AND workspace_id = ?", expected.ID, expected.OwnerUserID, expected.WorkspaceID).
		First(&current).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return models.Operation{}, ErrNotFound
		}
		return models.Operation{}, fmt.Errorf("lock operation for source approval: %w", err)
	}
	if expected.SourceIdentityHash != "" {
		if head == nil {
			return models.Operation{}, ErrSourceHeadReconciliation
		}
		if err := validateGormObservationReference(tx, current); err != nil {
			return models.Operation{}, err
		}
		if err := validateExecutableHead(current, *head, origin); err != nil {
			return models.Operation{}, err
		}
		if err := validateGormHeadObservation(tx, *head, origin); err != nil {
			return models.Operation{}, err
		}
	}
	if err := validateSourceApprovalRevision(current, expected, expected.Version, digest); err != nil {
		return models.Operation{}, err
	}
	return current, nil
}

func (r *GormRepository) ValidateSourceApproval(ctx context.Context, expected models.Operation, version int64, digest string) error {
	if r == nil || r.DB == nil {
		return ErrSourceHeadUnsupported
	}
	return r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SET LOCAL lock_timeout = '5s'").Error; err != nil {
			return err
		}
		current, err := r.lockSourceApprovalOperation(tx, expected, digest)
		if err != nil {
			return err
		}
		if current.Version != version {
			return ErrSourceApprovalStale
		}
		return intakeContextError(ctx)
	})
}

func (r *GormRepository) ApproveSourceDerived(ctx context.Context, expected, updated models.Operation, event models.OperationEvent, digest string) (*models.Operation, error) {
	if r == nil || r.DB == nil {
		return nil, ErrSourceHeadUnsupported
	}
	if err := validateMutationEvent(&updated, &event); err != nil {
		return nil, err
	}
	if err := validateSourceApprovalReceiptEvent(expected, updated, event, digest); err != nil {
		return nil, err
	}
	saved := updated
	err := r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SET LOCAL lock_timeout = '5s'").Error; err != nil {
			return err
		}
		current, err := r.lockSourceApprovalOperation(tx, expected, digest)
		if err != nil {
			return err
		}
		if err := validateSourceIdentityMutation(current, updated); err != nil {
			return err
		}
		if err := validateMutationState(current, updated, &event); err != nil {
			return err
		}
		var claimOwner sql.NullString
		err = tx.Raw(`SELECT claim_owner::text FROM public.operation_execution_claims WHERE operation_id = ? FOR UPDATE`, expected.ID).
			Row().Scan(&claimOwner)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("check operation execution claim before source approval: %w", err)
		}
		if err == nil && claimOwner.Valid && claimOwner.String != "" {
			return ErrOperationClaimed
		}
		saved.CreatedAt = current.CreatedAt
		if err := updateOperationCAS(tx, saved, current.Version); err != nil {
			return err
		}
		if err := intakeContextError(ctx); err != nil {
			return err
		}
		if err := tx.Create(&event).Error; err != nil {
			return fmt.Errorf("append source approval receipt event: %w", err)
		}
		return intakeContextError(ctx)
	})
	if err != nil {
		return nil, err
	}
	return &saved, nil
}

func (r *GormRepository) PublishSourceHead(ctx context.Context, op models.Operation) error {
	if err := safeEffectAdmissionError(ctx); err != nil {
		return err
	}
	observation, ok := CurrentSourceObservation(ctx)
	if !ok {
		return ErrInvalidSourceObservation
	}
	if r == nil || r.DB == nil {
		return ErrSourceHeadUnsupported
	}
	ctx, finish, err := observedIntakeContext(ctx)
	if err != nil {
		return err
	}
	defer finish()
	ctx, cancel := context.WithTimeout(ctx, safeEffectTimeout)
	defer cancel()
	var rejected bool
	err = r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SET LOCAL lock_timeout = '5s'").Error; err != nil {
			return err
		}
		origin, err := lockSourceOrigin(tx, op.OwnerUserID, op.WorkspaceID, observation.OriginID)
		if err != nil {
			return err
		}
		if err := validateHeadPublication(op, observation, origin); err != nil {
			return err
		}
		var persistedObservation SourceObservation
		if err := tx.Where("id = ?", observation.ID).First(&persistedObservation).Error; err != nil {
			return err
		}
		if !sameObservationRecord(persistedObservation, observation) {
			return ErrInvalidSourceObservation
		}
		head, err := lockSourceHead(tx, op.OwnerUserID, op.WorkspaceID, op.SourceIdentityHash)
		if err != nil {
			return err
		}
		var stored models.Operation
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND owner_user_id = ? AND workspace_id = ?", op.ID, op.OwnerUserID, op.WorkspaceID).First(&stored).Error; err != nil {
			return err
		}
		if !sameSourceIdentity(stored, op) || !sameSourceObservation(stored, op) || stored.SourceRevisionHash != op.SourceRevisionHash {
			return ErrSourceHeadReconciliation
		}
		if err := validateGormObservationReference(tx, stored); err != nil {
			return err
		}
		digest := sourceRevisionDigest(op.SourceRevisionHash)
		var seen int64
		if err := tx.Model(&sourceHeadRevision{}).Where("owner_user_id = ? AND workspace_id = ? AND source_identity_hash = ? AND revision_digest = ?", op.OwnerUserID, op.WorkspaceID, op.SourceIdentityHash, digest).Count(&seen).Error; err != nil {
			return err
		}
		next, changed, err := nextSourceHead(head, stored, observation, seen > 0)
		if err != nil {
			return err
		}
		if err := intakeContextError(ctx); err != nil {
			return err
		}
		if changed {
			if head == nil {
				if err := tx.Create(&next).Error; err != nil {
					return err
				}
			} else if err := tx.Model(&SourceHead{}).Where("owner_user_id = ? AND workspace_id = ? AND source_identity_hash = ?", op.OwnerUserID, op.WorkspaceID, op.SourceIdentityHash).Select("operation_id", "revision_hash", "observation_id", "observation_generation", "config_epoch", "state", "updated_at").Updates(next).Error; err != nil {
				return err
			}
			if next.State == "accepted" && seen == 0 {
				revision := sourceHeadRevision{op.OwnerUserID, op.WorkspaceID, op.SourceIdentityHash, digest, op.ID, observation.ID}
				if err := tx.Create(&revision).Error; err != nil {
					return err
				}
			}
			event := headPublicationEvent(next)
			if err := tx.Create(&event).Error; err != nil {
				return err
			}
		}
		rejected = next.State != "accepted"
		return intakeContextError(ctx)
	})
	if err != nil {
		return err
	}
	if rejected {
		return ErrSourceHeadReconciliation
	}
	return intakeContextError(ctx)
}

// Claim selection is only a preflight. The locked final boundary repeats this
// check and serializes it with publication; an ordinary MVCC read grants no effect.
const claimSourceHeadPredicate = `(operations.source_identity_hash = '' OR EXISTS (
SELECT 1 FROM public.operation_source_heads AS accepted_head
JOIN public.operation_source_origins AS accepted_origin
ON accepted_origin.owner_user_id = accepted_head.owner_user_id
AND accepted_origin.workspace_id = accepted_head.workspace_id
AND accepted_origin.origin_id = accepted_head.origin_id
WHERE accepted_head.owner_user_id = operations.owner_user_id
AND accepted_head.workspace_id = operations.workspace_id
AND accepted_head.source_identity_hash = operations.source_identity_hash
AND accepted_head.operation_id = operations.id
AND accepted_head.origin_id = operations.account_feed_id
AND accepted_head.revision_hash = operations.source_revision_hash
AND accepted_head.config_epoch = accepted_origin.config_epoch
AND (NOT accepted_origin.registry_managed OR accepted_origin.enabled)
AND accepted_head.state = 'accepted'
AND operations.source_observation_id IS NOT NULL))`

func (r *MemoryRepository) claimSourceHeadError(op models.Operation) error {
	if op.SourceIdentityHash == "" {
		return nil
	}
	head, ok := r.sourceHeads[sourceHeadKey{op.OwnerUserID, op.WorkspaceID, op.SourceIdentityHash}]
	if !ok || r.validateObservationReference(op) != nil {
		return ErrSourceHeadReconciliation
	}
	origin := r.sourceOrigins[sourceOriginKey{op.OwnerUserID, op.WorkspaceID, head.OriginID}]
	if err := validateExecutableHead(op, head, origin); err != nil {
		return err
	}
	return validateHeadObservation(head, r.observations[head.ObservationID], origin)
}
