package operations

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ReferenceCheckpointKey binds progress to a source feed and its local owner.
// The feed identity also includes the configured remote endpoint and owner.
type ReferenceCheckpointKey struct {
	OwnerUserID string    `gorm:"primaryKey"`
	WorkspaceID string    `gorm:"primaryKey"`
	FeedID      uuid.UUID `gorm:"type:uuid;primaryKey"`
}

// ReferenceCheckpoint is an opaque producer cursor plus the last ordered change.
// Its zero value means that no page has been committed for this source yet.
type ReferenceCheckpoint struct {
	Cursor   string
	Sequence uint64
}

type referenceCheckpointRow struct {
	ReferenceCheckpointKey `gorm:"embedded"`
	ReferenceCheckpoint    `gorm:"embedded"`
	UpdatedAt              time.Time
}

func (referenceCheckpointRow) TableName() string { return "public.account_feed_reference_checkpoints" }

var ErrReferenceCheckpointConflict = errors.New("operations: reference checkpoint changed; retry sync")
var referenceCursorPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

func (key ReferenceCheckpointKey) validate() error {
	if key.FeedID == uuid.Nil || key.OwnerUserID == "" || key.WorkspaceID == "" || len(key.OwnerUserID) > 255 || len(key.WorkspaceID) > 255 {
		return fmt.Errorf("operations: invalid reference checkpoint binding")
	}
	return nil
}

func (cp ReferenceCheckpoint) validate() error {
	if cp == (ReferenceCheckpoint{}) {
		return nil
	}
	if !referenceCursorPattern.MatchString(cp.Cursor) || cp.Sequence == 0 || cp.Sequence > math.MaxInt64 {
		return fmt.Errorf("operations: invalid reference checkpoint")
	}
	return nil
}

type referencePageRepository interface {
	LoadReferenceCheckpoint(context.Context, ReferenceCheckpointKey) (ReferenceCheckpoint, error)
	SyncReferencePage(context.Context, ReferenceCheckpointKey, uint64, ReferenceCheckpoint, string, []NewOperationInput, time.Time) ([]IngestResult, error)
}

func (s *Service) LoadReferenceCheckpoint(ctx context.Context, key ReferenceCheckpointKey) (ReferenceCheckpoint, error) {
	if err := key.validate(); err != nil {
		return ReferenceCheckpoint{}, err
	}
	repo, ok := s.repo.(referencePageRepository)
	if !ok {
		return ReferenceCheckpoint{}, fmt.Errorf("operations: reference checkpoint persistence unavailable")
	}
	return repo.LoadReferenceCheckpoint(ctx, key)
}

// SyncReferencePage atomically applies a validated bounded page and advances its
// checkpoint only if the stored cursor/sequence still match the caller's read.
// Network requests must finish before entering this transaction.
func (s *Service) SyncReferencePage(ctx context.Context, key ReferenceCheckpointKey, controlRevision uint64, expected ReferenceCheckpoint, next string, inputs []NewOperationInput) ([]IngestResult, error) {
	if err := validateReferencePage(key, expected, next, inputs); err != nil {
		return nil, err
	}
	repo, ok := s.repo.(referencePageRepository)
	if !ok {
		return nil, fmt.Errorf("operations: reference checkpoint persistence unavailable")
	}
	return repo.SyncReferencePage(ctx, key, controlRevision, expected, next, inputs, s.now().UTC())
}

func validateReferencePage(key ReferenceCheckpointKey, expected ReferenceCheckpoint, next string, inputs []NewOperationInput) error {
	if err := key.validate(); err != nil {
		return err
	}
	if err := expected.validate(); err != nil {
		return err
	}
	if len(inputs) == 0 || len(inputs) > 100 || !referenceCursorPattern.MatchString(next) || next == expected.Cursor {
		return fmt.Errorf("operations: invalid reference page bounds/cursor")
	}
	last := expected.Sequence
	for _, in := range inputs {
		if in.OwnerUserID != key.OwnerUserID || in.WorkspaceID != key.WorkspaceID || in.AccountFeedID == nil || *in.AccountFeedID != key.FeedID || in.SourceSequence <= last || in.SourceSequence > math.MaxInt64 {
			return fmt.Errorf("operations: reference page binding/order mismatch")
		}
		last = in.SourceSequence
	}
	return nil
}

func (r *GormRepository) LoadReferenceCheckpoint(ctx context.Context, key ReferenceCheckpointKey) (ReferenceCheckpoint, error) {
	if err := key.validate(); err != nil {
		return ReferenceCheckpoint{}, err
	}
	var row referenceCheckpointRow
	err := r.DB.WithContext(ctx).Where(key).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ReferenceCheckpoint{}, nil
	}
	if err != nil {
		return ReferenceCheckpoint{}, err
	}
	if err := row.ReferenceCheckpoint.validate(); err != nil || row.Sequence == 0 {
		return ReferenceCheckpoint{}, fmt.Errorf("operations: corrupt reference checkpoint; operator review required")
	}
	return row.ReferenceCheckpoint, nil
}

func (r *GormRepository) SyncReferencePage(ctx context.Context, key ReferenceCheckpointKey, controlRevision uint64, expected ReferenceCheckpoint, next string, inputs []NewOperationInput, now time.Time) ([]IngestResult, error) {
	if err := validateReferencePage(key, expected, next, inputs); err != nil {
		return nil, err
	}
	var results []IngestResult
	err := r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockReferenceSource(tx, key); err != nil {
			return err
		}
		repo := NewGormRepository(tx)
		control, err := repo.ReferenceSourceControl(ctx, key, nil, false, now)
		if err != nil {
			return err
		}
		if err := control.authorize(controlRevision); err != nil {
			return err
		}
		stored, err := repo.LoadReferenceCheckpoint(ctx, key)
		if err != nil {
			return err
		}
		if stored != expected {
			return ErrReferenceCheckpointConflict
		}
		// Deterministic lock order also composes with individual reference writes.
		ids := make([]string, 0, len(inputs))
		for _, in := range inputs {
			ids = append(ids, referenceID(in).String())
		}
		sort.Strings(ids)
		for i, id := range ids {
			if i == 0 || id != ids[i-1] {
				if err := lockReference(tx, uuid.MustParse(id)); err != nil {
					return err
				}
			}
		}
		results = make([]IngestResult, 0, len(inputs))
		for _, in := range inputs {
			result, err := syncReference(tx.Statement.Context, repo, in, now)
			if err != nil {
				return err
			}
			results = append(results, result)
		}
		row := referenceCheckpointRow{ReferenceCheckpointKey: key,
			ReferenceCheckpoint: ReferenceCheckpoint{Cursor: next, Sequence: inputs[len(inputs)-1].SourceSequence}, UpdatedAt: now}
		if stored == (ReferenceCheckpoint{}) {
			return tx.Create(&row).Error
		}
		return tx.Model(&referenceCheckpointRow{}).Where(key).Updates(map[string]any{
			"cursor": row.Cursor, "sequence": row.Sequence, "updated_at": now,
		}).Error
	})
	if err != nil {
		return nil, err // Never report rolled-back rows as imported.
	}
	return results, nil
}

func lockReference(tx *gorm.DB, id uuid.UUID) error {
	return tx.Exec("SELECT pg_advisory_xact_lock(?, ?)", int32(binary.BigEndian.Uint32(id[:4])), int32(binary.BigEndian.Uint32(id[4:8]))).Error
}

func (r *MemoryRepository) LoadReferenceCheckpoint(ctx context.Context, key ReferenceCheckpointKey) (ReferenceCheckpoint, error) {
	r.referenceMu.Lock()
	defer r.referenceMu.Unlock()
	if err := ctx.Err(); err != nil {
		return ReferenceCheckpoint{}, err
	}
	if err := key.validate(); err != nil {
		return ReferenceCheckpoint{}, err
	}
	cp := r.referenceCheckpoints[key]
	return cp, cp.validate()
}

func (r *MemoryRepository) SyncReferencePage(ctx context.Context, key ReferenceCheckpointKey, controlRevision uint64, expected ReferenceCheckpoint, next string, inputs []NewOperationInput, now time.Time) ([]IngestResult, error) {
	r.referenceMu.Lock()
	defer r.referenceMu.Unlock()
	if err := validateReferencePage(key, expected, next, inputs); err != nil {
		return nil, err
	}
	if err := r.referenceControls[key].authorize(controlRevision); err != nil {
		return nil, err
	}
	if r.referenceCheckpoints[key] != expected {
		return nil, ErrReferenceCheckpointConflict
	}
	// Stage only the bounded set of affected rows; failures cannot leak partial
	// updates. The in-memory implementation is not durable across process exit.
	staged := NewMemoryRepository()
	r.mu.Lock()
	for _, in := range inputs {
		id := referenceID(in)
		if op, ok := r.ops[id]; ok {
			staged.ops[id] = op
		}
	}
	for id, observation := range r.observations {
		staged.observations[id] = observation
	}
	for key, origin := range r.sourceOrigins {
		staged.sourceOrigins[key] = origin
	}
	for key, head := range r.sourceHeads {
		staged.sourceHeads[key] = head
	}
	for key, revision := range r.sourceHeadRevisions {
		staged.sourceHeadRevisions[key] = revision
	}
	r.mu.Unlock()
	results := make([]IngestResult, 0, len(inputs))
	for _, in := range inputs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		result, err := syncReference(ctx, staged, in, now)
		if err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.Lock()
	for id, op := range staged.ops {
		r.ops[id] = op
	}
	r.mu.Unlock()
	if r.referenceCheckpoints == nil {
		r.referenceCheckpoints = make(map[ReferenceCheckpointKey]ReferenceCheckpoint)
	}
	r.referenceCheckpoints[key] = ReferenceCheckpoint{Cursor: next, Sequence: inputs[len(inputs)-1].SourceSequence}
	return results, nil
}
