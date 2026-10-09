package workflow

import (
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type leaseExpiryRenewal struct {
	name        string
	table       string
	stateColumn string
	state       string
	claimColumn string
	leaseColumn string
	renew       func(Repository, uuid.UUID, string, time.Time) (bool, error)
}

func leaseExpiryRenewals() []leaseExpiryRenewal {
	return []leaseExpiryRenewal{
		{
			name: "workflow", table: "workflow_items", stateColumn: "current_state",
			state: StateInProgress, claimColumn: "worker_claim_id", leaseColumn: "worker_lease_until",
			renew: func(r Repository, id uuid.UUID, claim string, until time.Time) (bool, error) {
				return r.RenewRunnableItemClaim(id, claim, until)
			},
		},
		{
			name: "open_loop", table: "workflow_open_loops", stateColumn: "status",
			state: "processing", claimColumn: "claim_id", leaseColumn: "lease_until",
			renew: func(r Repository, id uuid.UUID, claim string, until time.Time) (bool, error) {
				return r.RenewOpenLoopClaim(id, claim, until)
			},
		},
	}
}

// DryRun and a nonconnecting pool exercise the real repository's SQL builder
// without opening a database or requiring another test dependency.
func leaseExpiryDryRunDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: &sql.DB{}}), &gorm.Config{
		DryRun: true, DisableAutomaticPing: true, SkipDefaultTransaction: true,
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open dry-run repository: %v", err)
	}
	return db
}

func TestClaimLeaseExpiryRenewalUsesAtomicWallClockPredicateAfterLock(t *testing.T) {
	for _, renewal := range leaseExpiryRenewals() {
		t.Run(renewal.name, func(t *testing.T) {
			db := leaseExpiryDryRunDB(t)
			var statement *gorm.Statement
			if err := db.Callback().Raw().After("gorm:raw").Register("lease_expiry:capture", func(tx *gorm.DB) {
				statement = tx.Statement
			}); err != nil {
				t.Fatal(err)
			}
			id, claim := uuid.New(), "lease-expiry-worker"
			before := time.Now().UTC()
			until := before.Add(time.Hour)
			if _, err := renewal.renew(NewGormRepository(db), id, claim, until); err != nil {
				t.Fatalf("renew: %v", err)
			}
			after := time.Now().UTC()
			if statement == nil {
				t.Fatal("renewal did not issue an update")
			}
			query := strings.Join(strings.Fields(statement.SQL.String()), " ")
			wantQuery := "WITH locked_claim AS MATERIALIZED ( SELECT id, " + renewal.leaseColumn +
				" FROM " + renewal.table + " WHERE id = $1 AND " + renewal.stateColumn +
				" = $2 AND " + renewal.claimColumn + " = $3 FOR UPDATE ) UPDATE " + renewal.table +
				" SET " + renewal.leaseColumn + " = $4, updated_at = $5 FROM locked_claim WHERE " +
				renewal.table + ".id = locked_claim.id AND locked_claim." + renewal.leaseColumn + " > clock_timestamp()"
			if query != wantQuery {
				t.Fatalf("renewal must check database wall-clock expiry after locking, in one statement: %s", query)
			}
			if len(statement.Vars) != 5 || statement.Vars[0] != id ||
				statement.Vars[1] != renewal.state || statement.Vars[2] != claim || statement.Vars[3] != until {
				t.Fatalf("renewal changed ownership/state predicates: %#v", statement.Vars)
			}
			currentTime, ok := statement.Vars[4].(time.Time)
			if !ok || currentTime.Before(before) || currentTime.After(after) || currentTime.Location() != time.UTC {
				t.Fatalf("updated_at = %#v, want the existing application UTC timestamp", statement.Vars[4])
			}
		})
	}
}

func TestClaimLeaseExpiryRenewalPreservesResultSemantics(t *testing.T) {
	wantErr := errors.New("lease-expiry update failed")
	for _, renewal := range leaseExpiryRenewals() {
		for _, test := range []struct {
			name  string
			rows  int64
			err   error
			owned bool
		}{
			{name: "no_matching_claim", rows: 0},
			{name: "active_claim", rows: 1, owned: true},
			{name: "database_error", err: wantErr},
		} {
			t.Run(renewal.name+"/"+test.name, func(t *testing.T) {
				db := leaseExpiryDryRunDB(t)
				if err := db.Callback().Raw().After("gorm:raw").Register("lease_expiry:result", func(tx *gorm.DB) {
					tx.RowsAffected = test.rows
					if test.err != nil {
						tx.AddError(test.err)
					}
				}); err != nil {
					t.Fatal(err)
				}
				owned, err := renewal.renew(NewGormRepository(db), uuid.New(), "worker", time.Now().UTC().Add(time.Hour))
				if owned != test.owned || !errors.Is(err, test.err) {
					t.Fatalf("renewal = (%t, %v), want (%t, %v)", owned, err, test.owned, test.err)
				}
			})
		}
	}
}
