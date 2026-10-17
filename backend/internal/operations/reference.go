package operations

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// SyncReference mirrors a source document, not an executable task. A stable
// source identity maps to one terminal ledger row, including after restart.
// Tombstones replace the current content; they do not erase historical backups.
func (s *Service) SyncReference(ctx context.Context, in NewOperationInput) (IngestResult, error) {
	writer, ok := s.repo.(interface {
		SyncReference(context.Context, NewOperationInput, time.Time) (IngestResult, error)
	})
	if !ok {
		return IngestResult{}, fmt.Errorf("operations: reference persistence unavailable")
	}
	return writer.SyncReference(ctx, in, s.now().UTC())
}

func referenceID(in NewOperationInput) uuid.UUID {
	// Source observations are immutable. Each producer change therefore gets
	// its own terminal ledger row instead of mutating evidence from an older read.
	identity, _ := json.Marshal([]any{in.OwnerUserID, in.WorkspaceID, in.DedupeKey, in.SourceSequence})
	return uuid.NewSHA1(uuid.NameSpaceURL, identity)
}

// SyncReference serializes matching source identities across HAI processes and
// commits each snapshot atomically on the existing PostgreSQL repository.
func (r *GormRepository) SyncReference(ctx context.Context, in NewOperationInput, now time.Time) (IngestResult, error) {
	var result IngestResult
	id := referenceID(in)
	err := r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockReference(tx, id); err != nil {
			return err
		}
		var err error
		result, err = syncReference(tx.Statement.Context, NewGormRepository(tx), in, now)
		return err
	})
	return result, err
}

func (r *MemoryRepository) SyncReference(ctx context.Context, in NewOperationInput, now time.Time) (IngestResult, error) {
	r.referenceMu.Lock()
	defer r.referenceMu.Unlock()
	if err := ctx.Err(); err != nil {
		return IngestResult{}, err
	}
	return syncReference(ctx, r, in, now)
}

func syncReference(ctx context.Context, repo Repository, in NewOperationInput, now time.Time) (IngestResult, error) {
	if in.SourceType != "autoposter_api" || in.OperationType != "read_only_reference" || in.SourceReceivedAt == nil || in.SourceSequence == 0 {
		return IngestResult{}, fmt.Errorf("operations: invalid reference input")
	}
	op, err := NewOperation(in, now)
	if err != nil {
		return IngestResult{}, err
	}
	op.ID = referenceID(in)
	if err := bindSourceObservation(ctx, &op); err != nil {
		return IngestResult{}, err
	}
	if db, ok := repo.(*GormRepository); ok {
		if err := lockIntakeSourceAuthority(db.DB, op); err != nil {
			return IngestResult{}, err
		}
		if err := validateGormObservationReference(db.DB, op); err != nil {
			return IngestResult{}, err
		}
	} else if memory, ok := repo.(*MemoryRepository); ok && op.SourceObservationID != nil {
		memory.mu.Lock()
		validationErr := errors.Join(memory.validateIntakeSourceAuthority(ctx, op), memory.validateObservationReference(op))
		memory.mu.Unlock()
		if validationErr != nil {
			return IngestResult{}, validationErr
		}
	}
	op.Status = string(StatusArchived) // Terminal: excluded from background execution.
	state := struct {
		Sequence uint64 `json:"source_change_id,string"`
	}{Sequence: in.SourceSequence}
	encodedState, _ := json.Marshal(state)
	op.WorldModelStateJSON = string(encodedState)
	existing, err := repo.GetByID(op.OwnerUserID, op.WorkspaceID, op.ID)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return IngestResult{}, err
	}
	created := errors.Is(err, ErrNotFound)
	if !created {
		if existing.SourceType != op.SourceType || existing.OperationType != op.OperationType || existing.Status != op.Status {
			return IngestResult{}, fmt.Errorf("operations: reference identity conflicts with a non-reference operation")
		}
		// Timestamps can collide or move backwards; the producer's immutable
		// decimal change ID, not wall-clock time, establishes replay order.
		if err := json.Unmarshal([]byte(existing.WorldModelStateJSON), &state); err != nil || state.Sequence == 0 {
			return IngestResult{}, fmt.Errorf("operations: reference change sequence unavailable")
		}
		if state.Sequence >= in.SourceSequence {
			return IngestResult{Operation: *existing}, nil
		}
		op.CreatedAt = existing.CreatedAt
		op.Version = existing.Version + 1
	}
	var saved = &op
	if created {
		saved, err = repo.Create(&op)
	} else {
		saved, err = repo.Update(&op)
	}
	if err != nil {
		return IngestResult{}, err
	}
	return IngestResult{Operation: *saved, Created: created}, nil
}
