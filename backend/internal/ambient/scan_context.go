package ambient

import (
	"context"
	"errors"
	"time"

	"automation-hub-backend/internal/durablejob"
	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/safety"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

var ErrScanContextUnavailable = errors.New("context-aware ambient scan is unavailable")
var ErrScanOutcomeUnconfirmed = errors.New("ambient scan outcome could not be confirmed")

// Separate capability: older adapters must not silently acquire a method they
// cannot implement. This is trusted system-wide scanning, not owner HTTP access.
type ContextualScanService interface {
	ScanContext(context.Context, string, ...func() bool) (*models.AmbientScan, error)
}

type contextualScanRepository interface {
	withScanContext(context.Context) (Repository, error)
}

func (r *GormRepository) withScanContext(ctx context.Context) (Repository, error) {
	if ctx == nil {
		return nil, ErrScanContextUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r == nil || r.db == nil || r.db.Config == nil || r.db.Statement == nil || r.db.Statement.ConnPool == nil ||
		r.db.DryRun || r.db.Dialector == nil || r.db.Dialector.Name() != "postgres" {
		return nil, ErrScanContextUnavailable
	}
	if _, transaction := r.db.Statement.ConnPool.(gorm.TxCommitter); transaction {
		return nil, ErrScanContextUnavailable
	}
	return &GormRepository{db: r.db.WithContext(ctx)}, nil
}

func scanCheckpoint(ctx context.Context, allowed func() bool) error {
	if ctx == nil {
		return ErrScanContextUnavailable
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if allowed == nil || !allowed() {
		return durablejob.Defer("background processing is paused by safety policy")
	}
	return nil
}

func (s *service) ScanContext(ctx context.Context, trigger string, allowed ...func() bool) (*models.AmbientScan, error) {
	gate := func() bool { return true }
	if len(allowed) > 0 {
		gate = allowed[0]
	}
	if err := scanCheckpoint(ctx, gate); err != nil {
		return nil, err
	}
	if s == nil {
		return nil, ErrScanContextUnavailable
	}
	repo, ok := s.repo.(contextualScanRepository)
	if !ok {
		return nil, ErrScanContextUnavailable
	}
	if !s.scanning.CompareAndSwap(false, true) {
		return nil, ErrScanInProgress
	}
	defer s.scanning.Store(false)
	scoped, err := repo.withScanContext(ctx)
	if err != nil {
		return nil, err
	}
	if scoped == nil {
		return nil, ErrScanContextUnavailable
	}
	// Do not copy the atomic scanning guard. The original service owns admission;
	// this view scopes persistence without mutating another worker's session.
	engine := &service{repo: scoped, workflows: s.workflows, memoryEngine: s.memoryEngine, memory: s.memory, pursuits: s.pursuits}
	return engine.scan(ctx, trigger, gate)
}

func (s *service) failScan(ctx context.Context, scan *models.AmbientScan, cause error) (*models.AmbientScan, error) {
	if scan == nil {
		return nil, cause
	}
	copy := snapshotScan(scan)
	completed := time.Now().UTC()
	copy.Status, copy.CompletedAt, copy.ErrorMessage = "failed", &completed, safety.RedactSecrets(cause.Error())
	repo := s.repo
	if contextual, ok := repo.(contextualScanRepository); ok {
		// Outcome-only recording survives cancellation, but is still bounded by a
		// cooperative database context. Never continue scanning with this context.
		settleCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer cancel()
		var err error
		repo, err = contextual.withScanContext(settleCtx)
		if err != nil || repo == nil {
			return unconfirmedScan(&copy, errors.Join(cause, err, ErrScanContextUnavailable))
		}
	}
	request := snapshotScan(&copy)
	updated, err := repo.UpdateScan(&request)
	if err != nil || !scanOutcomeMatches(copy, updated) {
		return unconfirmedScan(&copy, errors.Join(cause, err))
	}
	return updated, cause
}

// Keep known results independent of mutable repository arguments, including
// the pointed-to completion time. Database-managed timestamps are not outcomes.
func snapshotScan(scan *models.AmbientScan) models.AmbientScan {
	copy := *scan
	if scan.CompletedAt != nil {
		completed := *scan.CompletedAt
		copy.CompletedAt = &completed
	}
	return copy
}

func (s *service) startScan(scan *models.AmbientScan) (*models.AmbientScan, error) {
	expected := snapshotScan(scan)
	expected.ID = uuid.New()
	expected.StartedAt = expected.StartedAt.UTC().Truncate(time.Microsecond)
	request := snapshotScan(&expected)
	created, err := s.repo.CreateScan(&request)
	if err != nil || !scanRecordsMatch(expected, created) {
		// A lost creation acknowledgement is not proof that the insert failed.
		return unconfirmedScan(&expected, err)
	}
	return created, nil
}

func scanOutcomeMatches(expected models.AmbientScan, actual *models.AmbientScan) bool {
	if actual == nil || expected.StartedAt.IsZero() || expected.CompletedAt == nil || actual.CompletedAt == nil ||
		(expected.Status != "completed" && expected.Status != "failed") ||
		expected.ID == uuid.Nil {
		return false
	}
	return scanRecordsMatch(expected, actual)
}

func scanRecordsMatch(expected models.AmbientScan, actual *models.AmbientScan) bool {
	if actual == nil || expected.ID == uuid.Nil || expected.StartedAt.IsZero() ||
		(expected.CompletedAt == nil) != (actual.CompletedAt == nil) {
		return false
	}
	if expected.CompletedAt != nil && !expected.CompletedAt.UTC().Truncate(time.Microsecond).Equal(actual.CompletedAt.UTC().Truncate(time.Microsecond)) {
		return false
	}
	got := *actual
	expected.StartedAt = expected.StartedAt.UTC().Truncate(time.Microsecond)
	got.StartedAt = got.StartedAt.UTC().Truncate(time.Microsecond)
	expected.CompletedAt, got.CompletedAt = nil, nil
	expected.CreatedAt, expected.UpdatedAt = time.Time{}, time.Time{}
	got.CreatedAt, got.UpdatedAt = time.Time{}, time.Time{}
	return expected == got
}

func unconfirmedScan(scan *models.AmbientScan, cause error) (*models.AmbientScan, error) {
	err := errors.Join(ErrScanOutcomeUnconfirmed, cause)
	copy := snapshotScan(scan)
	copy.Status, copy.ErrorMessage = "outcome_unconfirmed", safety.RedactSecrets(err.Error())
	return &copy, err
}
