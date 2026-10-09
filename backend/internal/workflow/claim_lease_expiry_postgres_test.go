package workflow

import (
	"context"
	"testing"
	"time"

	"automation-hub-backend/internal/pgtestguard"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestPostgresClaimLeaseExpiryRenewal(t *testing.T) {
	dsn := pgtestguard.RequireDedicatedPostgresTestDSN(t,
		"HAI_WORKFLOW_LEASE_EXPIRY_TEST_DSN", "hai_workflow_lease_expiry_test")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal("open dedicated lease-expiry PostgreSQL database")
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal("get dedicated database pool")
	}
	defer pool.Close()
	tx := db.WithContext(ctx).Begin()
	if tx.Error != nil {
		t.Fatal("begin dedicated lease-expiry test transaction")
	}
	defer tx.Rollback()
	if err := tx.Exec("SET LOCAL search_path = pg_temp").Error; err != nil {
		t.Fatal(err)
	}
	// Only transaction-local shadow tables are used; no application tables,
	// migrations, or preexisting data are modified, even in the dedicated DB.
	for _, schema := range []string{
		`CREATE TEMP TABLE workflow_items (
			id uuid PRIMARY KEY, current_state text, worker_claim_id text,
			worker_lease_until timestamptz, updated_at timestamptz) ON COMMIT DROP`,
		`CREATE TEMP TABLE workflow_open_loops (
			id uuid PRIMARY KEY, status text, claim_id text,
			lease_until timestamptz, updated_at timestamptz) ON COMMIT DROP`,
	} {
		if err := tx.Exec(schema).Error; err != nil {
			t.Fatal(err)
		}
	}

	for _, renewal := range leaseExpiryRenewals() {
		t.Run(renewal.name, func(t *testing.T) {
			now := time.Now().UTC().Truncate(time.Microsecond)
			past, active := now.Add(-time.Hour), now.Add(time.Hour)
			for _, test := range []struct {
				name        string
				lease       *time.Time
				state       string
				storedClaim string
				requestID   uuid.UUID
				owned       bool
			}{
				{name: "expired_unrecovered", lease: &past, state: renewal.state, storedClaim: "worker"},
				{name: "expires_at_current_time", lease: &now, state: renewal.state, storedClaim: "worker"},
				{name: "null_lease", state: renewal.state, storedClaim: "worker"},
				{name: "active", lease: &active, state: renewal.state, storedClaim: "worker", owned: true},
				{name: "wrong_claim", lease: &active, state: renewal.state, storedClaim: "other-worker"},
				{name: "wrong_state", lease: &active, state: "closed", storedClaim: "worker"},
				{name: "missing_id", lease: &active, state: renewal.state, storedClaim: "worker", requestID: uuid.New()},
			} {
				t.Run(test.name, func(t *testing.T) {
					id := uuid.New()
					oldUpdated := now.Add(-2 * time.Hour)
					if err := tx.Table(renewal.table).Create(map[string]interface{}{
						"id": id, renewal.stateColumn: test.state, renewal.claimColumn: test.storedClaim,
						renewal.leaseColumn: test.lease, "updated_at": oldUpdated,
					}).Error; err != nil {
						t.Fatal(err)
					}
					requestID := test.requestID
					if requestID == uuid.Nil {
						requestID = id
					}
					// The active lease is deliberately shorter than the requested
					// deadline: comparing to that deadline would reject valid renewal.
					until := now.Add(2 * time.Hour)
					before := time.Now().UTC()
					owned, err := renewal.renew(NewGormRepository(tx), requestID, "worker", until)
					after := time.Now().UTC()
					if err != nil || owned != test.owned {
						t.Fatalf("renewal = (%t, %v), want (%t, nil)", owned, err, test.owned)
					}
					var row struct {
						State     string
						Claim     string
						Lease     *time.Time
						UpdatedAt time.Time
					}
					if err := tx.Table(renewal.table).
						Select(renewal.stateColumn+" AS state, "+renewal.claimColumn+" AS claim, "+renewal.leaseColumn+" AS lease, updated_at").
						Where("id = ?", id).Take(&row).Error; err != nil {
						t.Fatal(err)
					}
					if row.State != test.state || row.Claim != test.storedClaim {
						t.Fatalf("renewal changed state or ownership: %#v", row)
					}
					if test.owned {
						if row.Lease == nil || !row.Lease.Equal(until) ||
							row.UpdatedAt.Before(before.Truncate(time.Microsecond)) || row.UpdatedAt.After(after) {
							t.Fatalf("active renewal did not persist deadline/current timestamp: %#v", row)
						}
					} else if !row.UpdatedAt.Equal(oldUpdated) ||
						(row.Lease == nil) != (test.lease == nil) ||
						(row.Lease != nil && !row.Lease.Equal(*test.lease)) {
						t.Fatalf("rejected renewal mutated stored lease or timestamp: %#v", row)
					}
				})
			}
		})
	}
	t.Run("lock_wait", func(t *testing.T) {
		testClaimLeaseExpiryRenewalLockWait(t, db.WithContext(ctx))
	})
}
