package operations

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ReferenceSourceControl is a durable stop switch and generation fence. A
// disable/re-enable cycle never authorizes a page fetched under an older version.
type ReferenceSourceControl struct {
	Enabled  bool
	Revision uint64
}

type referenceControlRow struct {
	ReferenceCheckpointKey `gorm:"embedded"`
	ReferenceSourceControl `gorm:"embedded"`
	UpdatedAt              time.Time
}

func (referenceControlRow) TableName() string { return "public.account_feed_reference_controls" }

var ErrReferenceSourceDisabled = errors.New("operations: reference source disabled")
var ErrReferenceSourceChanged = errors.New("operations: reference source control changed; start a new sync")

type referenceControlRepository interface {
	ReferenceSourceControl(context.Context, ReferenceCheckpointKey, *bool, bool, time.Time) (ReferenceSourceControl, error)
}

func (s *Service) referenceControl(ctx context.Context, key ReferenceCheckpointKey, enabled *bool, initialize bool) (ReferenceSourceControl, error) {
	if err := key.validate(); err != nil {
		return ReferenceSourceControl{}, err
	}
	repo, ok := s.repo.(referenceControlRepository)
	if !ok {
		return ReferenceSourceControl{}, fmt.Errorf("operations: reference source control storage unavailable")
	}
	return repo.ReferenceSourceControl(ctx, key, enabled, initialize, s.now().UTC())
}

// EnsureReferenceSource preserves any existing enabled state, including false.
func (s *Service) EnsureReferenceSource(ctx context.Context, key ReferenceCheckpointKey, enabled bool) (ReferenceSourceControl, error) {
	return s.referenceControl(ctx, key, &enabled, true)
}

func (s *Service) GetReferenceSourceControl(ctx context.Context, key ReferenceCheckpointKey) (ReferenceSourceControl, error) {
	return s.referenceControl(ctx, key, nil, false)
}

func (s *Service) SetReferenceSourceEnabled(ctx context.Context, key ReferenceCheckpointKey, enabled bool) (ReferenceSourceControl, error) {
	return s.referenceControl(ctx, key, &enabled, false)
}

func (c ReferenceSourceControl) authorize(revision uint64) error {
	if c.Revision == 0 || c.Revision > math.MaxInt64 {
		return fmt.Errorf("operations: reference source control unavailable")
	}
	if !c.Enabled {
		return ErrReferenceSourceDisabled
	}
	if c.Revision != revision {
		return ErrReferenceSourceChanged
	}
	return nil
}

func lockReferenceSource(tx *gorm.DB, key ReferenceCheckpointKey) error {
	encoded, _ := json.Marshal(key)
	return lockReference(tx, uuid.NewSHA1(uuid.NameSpaceURL, append([]byte("reference-checkpoint:"), encoded...)))
}

func (r *GormRepository) ReferenceSourceControl(ctx context.Context, key ReferenceCheckpointKey, enabled *bool, initialize bool, now time.Time) (ReferenceSourceControl, error) {
	if err := key.validate(); err != nil {
		return ReferenceSourceControl{}, err
	}
	var row referenceControlRow
	read := func(db *gorm.DB) error {
		if err := db.Where(key).Take(&row).Error; err != nil {
			return err
		}
		if row.Revision == 0 || row.Revision > math.MaxInt64 {
			return fmt.Errorf("operations: corrupt reference source control")
		}
		return nil
	}
	if enabled == nil {
		err := read(r.DB.WithContext(ctx))
		return row.ReferenceSourceControl, err
	}
	err := r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockReferenceSource(tx, key); err != nil {
			return err
		}
		err := read(tx)
		if errors.Is(err, gorm.ErrRecordNotFound) && initialize {
			row = referenceControlRow{ReferenceCheckpointKey: key, ReferenceSourceControl: ReferenceSourceControl{Enabled: *enabled, Revision: 1}, UpdatedAt: now}
			return tx.Create(&row).Error
		}
		if err != nil || initialize || row.Enabled == *enabled {
			return err
		}
		if row.Revision == math.MaxInt64 {
			return fmt.Errorf("operations: reference control revision exhausted")
		}
		row.Enabled, row.Revision, row.UpdatedAt = *enabled, row.Revision+1, now
		return tx.Model(&referenceControlRow{}).Where(key).Updates(map[string]any{"enabled": row.Enabled, "revision": row.Revision, "updated_at": now}).Error
	})
	if err != nil {
		return ReferenceSourceControl{}, err
	}
	return row.ReferenceSourceControl, nil
}

func (r *MemoryRepository) ReferenceSourceControl(ctx context.Context, key ReferenceCheckpointKey, enabled *bool, initialize bool, now time.Time) (ReferenceSourceControl, error) {
	r.referenceMu.Lock()
	defer r.referenceMu.Unlock()
	if err := ctx.Err(); err != nil {
		return ReferenceSourceControl{}, err
	}
	if err := key.validate(); err != nil {
		return ReferenceSourceControl{}, err
	}
	control, exists := r.referenceControls[key]
	if !exists {
		if !initialize || enabled == nil {
			return ReferenceSourceControl{}, ErrNotFound
		}
		control = ReferenceSourceControl{Enabled: *enabled, Revision: 1}
	} else if control.Revision == 0 || control.Revision > math.MaxInt64 {
		return ReferenceSourceControl{}, fmt.Errorf("operations: corrupt reference source control")
	} else if enabled != nil && !initialize && *enabled != control.Enabled {
		if control.Revision == math.MaxInt64 {
			return ReferenceSourceControl{}, fmt.Errorf("operations: reference control revision exhausted")
		}
		control.Enabled, control.Revision = *enabled, control.Revision+1
	}
	if r.referenceControls == nil {
		r.referenceControls = make(map[ReferenceCheckpointKey]ReferenceSourceControl)
	}
	r.referenceControls[key] = control
	return control, nil
}
