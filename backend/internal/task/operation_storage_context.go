package task

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
)

const taskOperationStorageTimeout = 30 * time.Second

var ErrTaskStorageContextUnavailable = errors.New("owned task storage context is unavailable")

type ContextualTaskStateRepository interface {
	WithTaskStateContext(context.Context) (TaskStateRepository, error)
}

func (r *PostgresTaskStateRepository) WithTaskStateContext(ctx context.Context) (TaskStateRepository, error) {
	if ctx == nil {
		return nil, ErrTaskStorageContextUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r == nil || r.DB == nil || r.DB.Config == nil || r.DB.Statement == nil || r.DB.Statement.ConnPool == nil || r.DB.Dialector == nil || r.DB.Dialector.Name() != "postgres" || r.DB.DryRun {
		return nil, ErrTaskStorageContextUnavailable
	}
	if _, borrowed := r.DB.Statement.ConnPool.(gorm.TxCommitter); borrowed {
		return nil, ErrTaskStorageContextUnavailable
	}
	return NewPostgresTaskStateRepository(r.DB.Session(&gorm.Session{NewDB: true, Context: ctx})), nil
}

func (r *MemoryTaskStateRepository) WithTaskStateContext(ctx context.Context) (TaskStateRepository, error) {
	if ctx == nil || r == nil {
		return nil, ErrTaskStorageContextUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// This test/local store has no external transport. The scoped SQL repository
	// carries cancellation through entered queries and transactions instead.
	return r, nil
}

func (s *service) taskOperationStorage(ctx context.Context, required bool) (TaskStateRepository, context.CancelFunc, error) {
	ctx, cancel := context.WithTimeout(ctx, taskOperationStorageTimeout)
	if err := ctx.Err(); err != nil {
		cancel()
		return nil, func() {}, err
	}
	if factory, ok := s.stateRepository.(ContextualTaskStateRepository); ok {
		repo, err := factory.WithTaskStateContext(ctx)
		if err != nil || repo == nil {
			cancel()
			return nil, func() {}, errors.Join(ErrTaskStorageContextUnavailable, err)
		}
		return repo, cancel, nil
	}
	if required || s.stateRepository == nil {
		cancel()
		return nil, func() {}, ErrTaskStorageContextUnavailable
	}
	return s.stateRepository, cancel, nil
}
