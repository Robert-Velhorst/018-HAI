package operations

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"automation-hub-backend/internal/models"

	"github.com/google/uuid"
)

// Service orchestrates the Operation Ledger over the repository + domain rules.
type Service struct {
	repo Repository
	now  func() time.Time
}

// NewService builds a service over repo.
func NewService(repo Repository) *Service {
	return &Service{repo: repo, now: time.Now}
}

// DefaultService builds a service over the default repository.
func DefaultService() *Service { return NewService(DefaultRepository()) }

// IngestResult is the outcome of Ingest.
type IngestResult struct {
	Operation models.Operation
	Created   bool
}

// Ingest creates an Operation for a source item, or — if an active Operation
// already exists for the same dedupe key — refreshes it instead of creating a
// duplicate (§10.9 step 7-9).
func (s *Service) Ingest(in NewOperationInput) (IngestResult, error) {
	return s.ingest(context.Background(), nil, in)
}

// IngestContext carries request cancellation through the entire intake path.
// Unlike Ingest, it requires explicit contextual lookup and atomic write support;
// absent/expired contexts and ended private scopes are refused, not replaced
// with background authority. Context-free stores are not treated as cancellable.
func (s *Service) IngestContext(ctx context.Context, in NewOperationInput) (IngestResult, error) {
	if err := intakeContextError(ctx); err != nil {
		return IngestResult{}, err
	}
	ctx, finish, err := observedIntakeContext(ctx)
	if err != nil {
		return IngestResult{}, err
	}
	defer finish()
	repository, ok := s.repo.(ContextIntakeRepository)
	if !ok {
		return IngestResult{}, ErrContextIntakeUnsupported
	}
	_, observed := CurrentSourceObservation(ctx)
	var publisher SourceHeadRepository
	if observed {
		publisher, ok = s.repo.(SourceHeadRepository)
		if !ok {
			return IngestResult{}, ErrSourceHeadUnsupported
		}
	}
	result, err := s.ingest(ctx, repository, in)
	if err != nil {
		return IngestResult{}, err
	}
	if err := intakeContextError(ctx); err != nil {
		return IngestResult{}, err
	}
	if observed {
		if err := publisher.PublishSourceHead(ctx, result.Operation); err != nil {
			return IngestResult{}, err
		}
	}
	return result, nil
}

func (s *Service) ingest(ctx context.Context, contextual ContextIntakeRepository, in NewOperationInput) (IngestResult, error) {
	now := s.now().UTC()
	in.OwnerUserID = strings.TrimSpace(in.OwnerUserID)
	in.WorkspaceID = firstNonEmpty(strings.TrimSpace(in.WorkspaceID), "local")
	op, err := NewOperation(in, now)
	if err != nil {
		return IngestResult{}, err
	}
	if err := bindSourceObservation(ctx, &op); err != nil {
		return IngestResult{}, err
	}
	if !json.Valid([]byte(op.EvidenceJSON)) {
		return IngestResult{}, fmt.Errorf("operation: evidence must be valid JSON")
	}
	lookup := func(key string) (*models.Operation, bool, error) {
		if err := intakeContextError(ctx); err != nil {
			return nil, false, err
		}
		var existing *models.Operation
		var found bool
		var err error
		if contextual != nil {
			existing, found, err = contextual.FindByDedupeKeyContext(ctx, in.OwnerUserID, in.WorkspaceID, key)
		} else {
			existing, found, err = s.repo.FindByDedupeKey(in.OwnerUserID, in.WorkspaceID, key)
		}
		if err == nil {
			err = intakeContextError(ctx)
		}
		if err == nil {
			err = validateIntakeLookup(existing, found, in.OwnerUserID, in.WorkspaceID, key)
		}
		if err == nil && found && key == in.DedupeKey {
			err = validateIntakeSourceMatch(*existing, op)
		}
		return existing, found, err
	}
	if existing, found, err := lookup(in.DedupeKey); err != nil {
		return IngestResult{}, err
	} else if found {
		return s.refreshEvidence(ctx, contextual, in, *existing)
	}
	if in.LegacyDedupeKey != "" && in.LegacyDedupeKey != in.DedupeKey {
		if _, found, err := lookup(in.LegacyDedupeKey); err != nil {
			return IngestResult{}, err
		} else if found {
			return IngestResult{}, ErrSourceIdentityMigrationRequired
		}
	}
	if err := intakeContextError(ctx); err != nil {
		return IngestResult{}, err
	}
	var create func(*models.Operation, *models.OperationEvent) (*models.Operation, error)
	if contextual != nil {
		create = func(op *models.Operation, event *models.OperationEvent) (*models.Operation, error) {
			return contextual.CreateWithEventContext(ctx, op, event)
		}
	} else {
		repository, ok := s.repo.(AtomicCreationRepository)
		if !ok {
			return IngestResult{}, ErrAtomicCreationUnsupported
		}
		create = repository.CreateWithEvent
	}
	op.ID = uuid.New()
	expectedCreated := op
	if err := intakeContextError(ctx); err != nil {
		return IngestResult{}, err
	}
	created, err := create(&op, &models.OperationEvent{
		OperationID: op.ID,
		EventType:   "created",
		ActorType:   string(OwnerHAI),
		AfterStatus: op.Status,
		Message:     "operation created from " + op.SourceType,
		PayloadJSON: "{}",
		CreatedAt:   now,
	})
	if err != nil {
		if errors.Is(err, ErrDuplicateDedupeKey) {
			existing, found, lookupErr := lookup(in.DedupeKey)
			if lookupErr != nil {
				return IngestResult{}, lookupErr
			}
			if found {
				return s.refreshEvidence(ctx, contextual, in, *existing)
			}
			if err := intakeContextError(ctx); err != nil {
				return IngestResult{}, err
			}
		}
		return IngestResult{}, err
	}
	if err := validateIntakeWriteResult(created, expectedCreated); err != nil {
		return IngestResult{}, err
	}
	return IngestResult{Operation: *created, Created: true}, nil
}

// Both duplicate paths share the audit and claim fence, including creation races.
func (s *Service) refreshEvidence(ctx context.Context, contextual ContextIntakeRepository, in NewOperationInput, existing models.Operation) (IngestResult, error) {
	if err := intakeContextError(ctx); err != nil {
		return IngestResult{}, err
	}
	if evidence := strings.TrimSpace(in.EvidenceJSON); evidence != "" && evidence != "{}" && evidence != existing.EvidenceJSON {
		existing.EvidenceJSON = in.EvidenceJSON
		updated, err := s.save(ctx, contextual, existing, "source_evidence_refreshed", string(OwnerHAI), "source evidence refreshed")
		if err != nil {
			return IngestResult{}, err
		}
		// save increments the version before handing the pair to persistence.
		expected := existing
		expected.Version++
		if err := validateIntakeWriteResult(updated, expected); err != nil {
			return IngestResult{}, err
		}
		return IngestResult{Operation: *updated}, nil
	}
	return IngestResult{Operation: existing}, nil
}

// Get returns an operation scoped to owner/workspace.
func (s *Service) Get(ownerUserID, workspaceID string, id uuid.UUID) (*models.Operation, error) {
	return s.repo.GetByID(ownerUserID, workspaceID, id)
}

// List returns operations for a filter.
func (s *Service) List(f Filter) ([]models.Operation, error) { return s.repo.List(f) }

// ListDue returns operations the background loop may still progress.
func (s *Service) ListDue(ownerUserID, workspaceID string, limit int) ([]models.Operation, error) {
	return s.repo.ListDue(ownerUserID, workspaceID, limit)
}

// Dashboard returns the Background Operations roll-up.
func (s *Service) Dashboard(ownerUserID, workspaceID string) (Dashboard, error) {
	return s.repo.Dashboard(ownerUserID, workspaceID)
}

// Events returns an operation's audit trail.
func (s *Service) Events(operationID uuid.UUID) ([]models.OperationEvent, error) {
	return s.repo.ListEvents(operationID, 0)
}

// Transition moves an operation to a new status (validated) and audits it.
func (s *Service) Transition(op models.Operation, to OperationStatus, actorType, actorID, message string) (*models.Operation, error) {
	updated, evt, err := ApplyTransition(op, to, actorType, actorID, message, s.now().UTC())
	if err != nil {
		return nil, err
	}
	repository, err := s.atomicMutationRepository()
	if err != nil {
		return nil, err
	}
	return repository.UpdateWithEvent(&updated, &evt)
}

// Save persists a mutated operation (e.g. after a policy decision or execution
// result) and appends a domain event.
func (s *Service) Save(op models.Operation, eventType, actorType, message string) (*models.Operation, error) {
	return s.save(context.Background(), nil, op, eventType, actorType, message)
}

func (s *Service) save(ctx context.Context, contextual ContextIntakeRepository, op models.Operation, eventType, actorType, message string) (*models.Operation, error) {
	now := s.now().UTC()
	op.UpdatedAt = now
	op.Version++
	var update func(*models.Operation, *models.OperationEvent) (*models.Operation, error)
	if contextual != nil {
		update = func(op *models.Operation, event *models.OperationEvent) (*models.Operation, error) {
			return contextual.UpdateWithEventContext(ctx, op, event)
		}
	} else {
		repository, err := s.atomicMutationRepository()
		if err != nil {
			return nil, err
		}
		update = repository.UpdateWithEvent
	}
	if err := intakeContextError(ctx); err != nil {
		return nil, err
	}
	return update(&op, &models.OperationEvent{
		OperationID: op.ID,
		EventType:   eventType,
		ActorType:   actorType,
		AfterStatus: op.Status,
		Message:     message,
		PayloadJSON: "{}",
		CreatedAt:   now,
	})
}

func (s *Service) atomicMutationRepository() (AtomicMutationRepository, error) {
	repository, ok := s.repo.(AtomicMutationRepository)
	if !ok {
		return nil, ErrAtomicMutationsUnsupported
	}
	return repository, nil
}

func (s *Service) claimRepository() (ClaimRepository, error) {
	if s == nil {
		return nil, ErrAtomicClaimsUnsupported
	}
	repository, ok := s.repo.(ClaimRepository)
	if !ok {
		return nil, ErrAtomicClaimsUnsupported
	}
	return repository, nil
}

// ClaimNext atomically leases the oldest eligible operation. A repository
// without durable claim support is rejected rather than falling back to List.
func (s *Service) ClaimNext(ctx context.Context, ownerUserID, workspaceID string, workerID uuid.UUID, lease time.Duration) (*ClaimedOperation, error) {
	if err := intakeContextError(ctx); err != nil {
		return nil, err
	}
	repository, err := s.claimRepository()
	if err != nil {
		return nil, err
	}
	return repository.ClaimNext(ctx, ownerUserID, workspaceID, workerID, lease)
}

// ClaimOperation atomically claims an explicitly selected safe-executable
// operation, scoped to its owner and workspace, and returns the locked current
// snapshot with its fencing token.
func (s *Service) ClaimOperation(ctx context.Context, ownerUserID, workspaceID string, operationID, workerID uuid.UUID, lease time.Duration) (*ClaimedOperation, error) {
	if err := intakeContextError(ctx); err != nil {
		return nil, err
	}
	repository, err := s.claimRepository()
	if err != nil {
		return nil, err
	}
	return repository.ClaimOperation(ctx, ownerUserID, workspaceID, operationID, workerID, lease)
}

// RenewClaim extends a still-live claim without changing its generation.
func (s *Service) RenewClaim(ctx context.Context, claim ExecutionClaim, lease time.Duration) error {
	if err := intakeContextError(ctx); err != nil {
		return err
	}
	repository, err := s.claimRepository()
	if err != nil {
		return err
	}
	return repository.RenewClaim(ctx, claim, lease)
}

// TransitionClaimed atomically fences the operation update, audit event, and
// optional claim release against the exact owner and generation.
func (s *Service) TransitionClaimed(ctx context.Context, claim ExecutionClaim, op models.Operation, to OperationStatus, actorType, actorID, message string) (*models.Operation, error) {
	if err := intakeContextError(ctx); err != nil {
		return nil, err
	}
	if s == nil {
		return nil, ErrAtomicClaimsUnsupported
	}
	updated, event, err := ApplyTransition(op, to, actorType, actorID, message, s.now().UTC())
	if err != nil {
		return nil, err
	}
	repository, err := s.claimRepository()
	if err != nil {
		return nil, err
	}
	return repository.TransitionClaimed(ctx, claim, updated, event, recoveryReleaseStatus(to))
}

// ReleaseClaim relinquishes ownership when routing concludes without another
// operation-state transition.
func (s *Service) ReleaseClaim(ctx context.Context, claim ExecutionClaim) error {
	if err := intakeContextError(ctx); err != nil {
		return err
	}
	repository, err := s.claimRepository()
	if err != nil {
		return err
	}
	return repository.ReleaseClaim(ctx, claim)
}

// RecoverExpiredClaims reviews only expired claimed execution. Unclaimed
// Running/Verifying rows are reported for manual inspection and never guessed
// safe to interrupt.
func (s *Service) RecoverExpiredClaims(ctx context.Context, ownerUserID, workspaceID string, limit int) (RecoveryResult, error) {
	if err := intakeContextError(ctx); err != nil {
		return RecoveryResult{}, err
	}
	repository, err := s.claimRepository()
	if err != nil {
		return RecoveryResult{}, err
	}
	return repository.RecoverExpiredClaims(ctx, ownerUserID, workspaceID, limit)
}
