package automation

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type proofConsumptionPool struct {
	configurationReadPool
	affected    int64
	execError   error
	cancelAfter bool
}

func (p *proofConsumptionPool) ExecContext(ctx context.Context, _ string, _ ...interface{}) (sql.Result, error) {
	p.ctx = ctx
	p.calls++
	if p.cancelAfter {
		p.cancel()
	}
	return driver.RowsAffected(p.affected), p.execError
}

type borrowedProofConsumptionPool struct{ *proofConsumptionPool }

func (*borrowedProofConsumptionPool) Commit() error   { return nil }
func (*borrowedProofConsumptionPool) Rollback() error { return nil }

func TestProofConsumptionRepositoryOwnsSQLScope(t *testing.T) {
	for _, boundary := range []string{"valid", "cancel_before", "cancel_after", "dry_run", "borrowed", "nil_root", "write_error", "unexpected_count", "duplicate"} {
		t.Run(boundary, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.WithValue(context.Background(), configurationContextKey{}, "proof-store"))
			defer cancel()
			pool := &proofConsumptionPool{configurationReadPool: configurationReadPool{cancel: cancel}, affected: 1}
			var conn gorm.ConnPool = pool
			if boundary == "borrowed" {
				conn = &borrowedProofConsumptionPool{pool}
			}
			db, err := gorm.Open(postgres.New(postgres.Config{Conn: conn}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent)})
			if err != nil {
				t.Fatal(err)
			}
			rootCtx := db.Statement.Context
			store := NewPostgresApprovalProofConsumptionStore(db)
			storageError := errors.New("controlled SQL consumption write error")
			switch boundary {
			case "cancel_before":
				cancel()
			case "cancel_after":
				pool.cancelAfter = true
			case "dry_run":
				store.DB = db.Session(&gorm.Session{DryRun: true})
			case "borrowed":
			case "nil_root":
				store.DB = nil
			case "write_error":
				pool.execError = storageError
			case "unexpected_count":
				pool.affected = 2
			case "duplicate":
				pool.affected = 0
			}
			now := time.Now().UTC()
			value := ApprovalProofConsumption{ContractVersion: approvalProofConsumptionContractVersion, ProofID: uuid.New(), OwnerIdentity: "alice", AutomationID: uuid.New(), ActionDigest: approvalTestDigest("action"), Scope: ApprovalScopeScript, ApprovalSourceID: "task-review:" + uuid.NewString(), NonceDigest: approvalTestDigest("nonce"), SignatureDigest: approvalTestDigest("signature"), IssuedAt: now, ExpiresAt: now.Add(time.Minute), ConsumedAt: now}
			value.RecordDigest = approvalProofConsumptionDigest(value)
			err = store.Consume(ctx, value)
			if boundary == "valid" {
				if err != nil || pool.calls != 1 {
					t.Fatalf("SQL consumption failed: %v", err)
				}
				if pool.ctx.Value(configurationContextKey{}) != "proof-store" {
					t.Fatal("SQL write lost caller context")
				}
				if _, ok := pool.ctx.Deadline(); !ok {
					t.Fatal("SQL consumption has no deadline")
				}
			} else if err == nil {
				t.Fatal("unsafe consumption returned success")
			}
			if db.Statement.Context != rootCtx {
				t.Fatal("consumption mutated shared root context")
			}
			if ctx.Err() != nil && !errors.Is(err, context.Canceled) {
				t.Fatal("SQL consumption lost cancellation")
			}
			if boundary == "write_error" && !errors.Is(err, storageError) {
				t.Fatal("SQL consumption lost storage error")
			}
			if boundary == "duplicate" && !errors.Is(err, ErrApprovalProofConsumed) {
				t.Fatal("duplicate proof lost replay rejection")
			}
			if boundary == "unexpected_count" && errors.Is(err, ErrApprovalProofConsumed) {
				t.Fatal("unexpected SQL count pretended to be a duplicate")
			}
			if (boundary == "dry_run" || boundary == "borrowed") && errors.Is(err, ErrApprovalProofConsumed) {
				t.Fatal("unsafe SQL root pretended to be duplicate consumption")
			}
			if (boundary == "write_error" || boundary == "cancel_after" || boundary == "unexpected_count") && !errors.Is(err, ErrApprovalProofConsumptionUnconfirmed) {
				t.Fatal("uncertain SQL acknowledgement lost reconciliation requirement")
			}
			if (boundary == "cancel_before" || boundary == "dry_run" || boundary == "borrowed" || boundary == "nil_root") && pool.calls != 0 {
				t.Fatal("unsafe proof write entered SQL pool")
			}
		})
	}
}
