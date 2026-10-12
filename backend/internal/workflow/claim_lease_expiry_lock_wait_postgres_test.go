package workflow

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"gorm.io/gorm"
)

func testClaimLeaseExpiryRenewalLockWait(t *testing.T, db *gorm.DB) {
	t.Helper()
	// Cross-connection row locks cannot use session-local temporary tables.
	// Keep these fixtures in a uniquely named schema in the guarded test DB.
	schema := "lease_expiry_lock_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	quotedSchema := pgx.Identifier{schema}.Sanitize()
	if err := db.Exec("CREATE SCHEMA " + quotedSchema).Error; err != nil {
		t.Fatal(err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := db.WithContext(ctx).Exec("DROP SCHEMA " + quotedSchema + " CASCADE").Error; err != nil {
			t.Errorf("remove isolated lock-wait schema: %v", err)
		}
	}()
	for _, renewal := range leaseExpiryRenewals() {
		table := pgx.Identifier{schema, renewal.table}.Sanitize()
		if err := db.Exec("CREATE TABLE " + table + " (id uuid PRIMARY KEY, " + renewal.stateColumn +
			" text, " + renewal.claimColumn + " text, " + renewal.leaseColumn + " timestamptz, updated_at timestamptz)").Error; err != nil {
			t.Fatal(err)
		}
		for _, blocker := range []string{"lock_only", "row_version_update"} {
			for _, expire := range []bool{true, false} {
				outcome := "still_active"
				if expire {
					outcome = "expires_while_waiting"
				}
				t.Run(renewal.name+"/"+blocker+"/"+outcome, func(t *testing.T) {
					id := uuid.New()
					oldUpdated := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
					var leaseUntil time.Time
					if err := db.Raw("INSERT INTO "+table+" (id, "+renewal.stateColumn+", "+renewal.claimColumn+", "+renewal.leaseColumn+", updated_at) "+
						"VALUES (?, ?, ?, clock_timestamp() + INTERVAL '2 seconds', ?) RETURNING "+renewal.leaseColumn,
						id, renewal.state, "worker", oldUpdated).Row().Scan(&leaseUntil); err != nil {
						t.Fatal(err)
					}
					locker := db.Begin()
					if locker.Error != nil {
						t.Fatal(locker.Error)
					}
					defer locker.Rollback()
					if blocker == "lock_only" {
						var lockedID uuid.UUID
						if err := locker.Raw("SELECT id FROM "+table+" WHERE id = ? FOR UPDATE", id).Row().Scan(&lockedID); err != nil {
							t.Fatal(err)
						}
					} else if err := locker.Exec("UPDATE "+table+" SET updated_at = updated_at WHERE id = ?", id).Error; err != nil {
						t.Fatal(err)
					}
					worker := db.Begin()
					if worker.Error != nil {
						t.Fatal(worker.Error)
					}
					defer func() {
						_ = locker.Rollback().Error
						_ = worker.Rollback().Error
					}()
					if err := worker.Exec("SET LOCAL search_path = " + quotedSchema).Error; err != nil {
						t.Fatal(err)
					}
					var workerPID int
					if err := worker.Raw("SELECT pg_backend_pid()").Row().Scan(&workerPID); err != nil {
						t.Fatal(err)
					}
					type renewalResult struct {
						owned bool
						err   error
					}
					results := make(chan renewalResult, 1)
					until := leaseUntil.Add(time.Hour)
					go func() {
						owned, err := renewal.renew(NewGormRepository(worker), id, "worker", until)
						results <- renewalResult{owned: owned, err: err}
					}()
					leaseExpiryWaitFor(t, db, "SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE pid = ? AND wait_event_type = 'Lock')", workerPID)
					var alreadyExpired bool
					if err := db.Raw("SELECT clock_timestamp() >= ?", leaseUntil).Row().Scan(&alreadyExpired); err != nil {
						t.Fatal(err)
					}
					if alreadyExpired {
						t.Fatal("renewal did not reach the row lock before the lease expired")
					}
					if expire {
						leaseExpiryWaitFor(t, db, "SELECT clock_timestamp() >= ?", leaseUntil)
					}
					if err := locker.Commit().Error; err != nil {
						t.Fatal(err)
					}
					select {
					case result := <-results:
						if result.err != nil || result.owned == expire {
							t.Errorf("renewal after row-lock wait = (%t, %v), want (%t, nil)", result.owned, result.err, !expire)
						}
					case <-time.After(5 * time.Second):
						t.Fatal("renewal did not finish after releasing the row lock")
					}
					var row struct {
						State     string
						Claim     string
						Lease     time.Time
						UpdatedAt time.Time
					}
					if err := worker.Table(renewal.table).
						Select(renewal.stateColumn+" AS state, "+renewal.claimColumn+" AS claim, "+renewal.leaseColumn+" AS lease, updated_at").
						Where("id = ?", id).Take(&row).Error; err != nil {
						t.Fatal(err)
					}
					if row.State != renewal.state || row.Claim != "worker" {
						t.Fatalf("renewal changed state or claim: %#v", row)
					}
					if expire && (!row.Lease.Equal(leaseUntil) || !row.UpdatedAt.Equal(oldUpdated)) {
						t.Errorf("expired lease was revived after waiting for the row lock: %#v", row)
					}
					if !expire && !row.Lease.Equal(until) {
						t.Errorf("still-active lease was not renewed after waiting for the row lock: %#v", row)
					}
				})
			}
		}
	}
}

func leaseExpiryWaitFor(t *testing.T, db *gorm.DB, query string, args ...interface{}) {
	t.Helper()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var ready bool
		if err := db.Raw(query, args...).Row().Scan(&ready); err != nil {
			t.Fatal(err)
		}
		if ready {
			return
		}
		select {
		case <-ticker.C:
		case <-deadline.C:
			t.Fatal("timed out waiting for observed database row-lock/expiry state")
		}
	}
}
