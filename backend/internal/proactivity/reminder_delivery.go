package proactivity

import (
	"context"
	"fmt"

	"gorm.io/gorm"
)

// RecordSignalsInTransaction joins an internal caller's transaction without
// changing this service's repository or clock. Memory/cross-database sinks fail
// closed rather than committing an effect independently of its receipt.
func (s *Service) RecordSignalsInTransaction(ctx context.Context, tx *gorm.DB, owner, key string, signals []OpenLoopSignal) ([]SignalRecord, bool, error) {
	if s == nil || tx == nil || tx.Statement == nil || tx.Config == nil {
		return nil, false, ErrRepositoryUnavailable
	}
	repository, ok := s.repository.(*PostgresRepository)
	if !ok || repository == nil || repository.DB == nil || repository.DB.Config == nil ||
		repository.DB.Config.ConnPool == nil || repository.DB.Config.ConnPool != tx.Config.ConnPool {
		return nil, false, fmt.Errorf("internal signal transaction must use the configured PostgreSQL database")
	}
	if _, ok := tx.Statement.ConnPool.(gorm.TxCommitter); !ok {
		return nil, false, fmt.Errorf("internal signal transaction is not active")
	}
	bound := &Service{repository: NewPostgresRepository(tx), now: s.now}
	return bound.RecordSignals(ctx, owner, key, signals)
}
