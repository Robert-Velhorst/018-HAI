package proactivity

import (
	"context"
	"database/sql"
	"testing"

	"gorm.io/gorm"
)

func TestReminderSignalTransactionBindingFailsClosed(t *testing.T) {
	pool := &sql.DB{}
	otherPool := &sql.DB{}
	configured := &gorm.DB{Config: &gorm.Config{ConnPool: pool}}
	for _, test := range []struct {
		name    string
		service *Service
		tx      *gorm.DB
	}{
		{"nil_service", nil, configured},
		{"nil_transaction", NewService(NewPostgresRepository(configured)), nil},
		{"invalid_transaction", NewService(NewPostgresRepository(configured)), &gorm.DB{}},
		{"memory_repository", NewService(NewMemoryRepository()), configured},
		{"nil_postgres_database", NewService(NewPostgresRepository(nil)), configured},
		{"different_database", NewService(NewPostgresRepository(configured)), &gorm.DB{Config: &gorm.Config{ConnPool: otherPool}, Statement: &gorm.Statement{ConnPool: otherPool}}},
		{"not_a_transaction", NewService(NewPostgresRepository(configured)), &gorm.DB{Config: &gorm.Config{ConnPool: pool}, Statement: &gorm.Statement{ConnPool: pool}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if records, created, err := test.service.RecordSignalsInTransaction(context.Background(), test.tx, "alice", "reminder:test", nil); err == nil || records != nil || created {
				t.Fatalf("unsafe transaction binding succeeded: records=%#v created=%t err=%v", records, created, err)
			}
		})
	}
}
