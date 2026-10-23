package task

import (
	"database/sql"
	"testing"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// This fixture exposes pool identity only; it never opens a database or proves
// PostgreSQL transaction/locking behavior.
type taskStateGuardTransaction struct{ *sql.DB }

func (*taskStateGuardTransaction) Commit() error   { return nil }
func (*taskStateGuardTransaction) Rollback() error { return nil }
func (tx *taskStateGuardTransaction) GetDBConn() (*sql.DB, error) {
	return tx.DB, nil
}

func TestTaskStateTransactionGuardRejectsMissingConfigAndDryRun(t *testing.T) {
	pool := &sql.DB{}
	dialect := postgres.New(postgres.Config{})
	root := &gorm.DB{Config: &gorm.Config{Dialector: dialect, ConnPool: pool}, Statement: &gorm.Statement{ConnPool: pool}}
	transaction := &taskStateGuardTransaction{DB: pool}
	tx := &gorm.DB{Config: &gorm.Config{Dialector: dialect, ConnPool: transaction}, Statement: &gorm.Statement{ConnPool: transaction}}
	rootDryRun := *root
	rootConfig := *root.Config
	rootConfig.DryRun = true
	rootDryRun.Config = &rootConfig
	txDryRun := *tx
	txConfig := *tx.Config
	txConfig.DryRun = true
	txDryRun.Config = &txConfig

	for _, test := range []struct {
		name     string
		root, tx *gorm.DB
	}{
		{name: "missing root configuration", root: &gorm.DB{}, tx: tx},
		{name: "missing transaction configuration", root: root, tx: &gorm.DB{}},
		{name: "dry-run root", root: &rootDryRun, tx: tx},
		{name: "dry-run transaction", root: root, tx: &txDryRun},
		{name: "nil root", tx: tx},
		{name: "nil transaction", root: root},
		{name: "root is already transactional", root: tx, tx: tx},
	} {
		t.Run(test.name, func(t *testing.T) {
			defer func() {
				if recovered := recover(); recovered != nil {
					t.Fatal("invalid transaction configuration caused a panic")
				}
			}()
			if sameTaskStatePostgresTransaction(test.root, test.tx) {
				t.Fatal("invalid transaction configuration was accepted")
			}
			result, err := (&PostgresTaskStateRepository{DB: test.root}).FindApprovedReviewDecisionInPostgresTransaction(
				test.tx, "synthetic-owner", "11111111-1111-4111-8111-111111111111",
			)
			if result != nil || err == nil {
				t.Fatal("approval lookup did not reject invalid storage before SQL")
			}
		})
	}

	if !sameTaskStatePostgresTransaction(root, tx) {
		t.Fatal("ordinary same-pool structural control was rejected")
	}
	otherPool := &sql.DB{}
	otherRoot := &gorm.DB{Config: &gorm.Config{Dialector: dialect, ConnPool: otherPool}, Statement: &gorm.Statement{ConnPool: otherPool}}
	if sameTaskStatePostgresTransaction(otherRoot, tx) {
		t.Fatal("foreign-pool structural control was accepted")
	}
}
