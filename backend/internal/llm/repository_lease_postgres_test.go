package llm

import (
	"context"
	"testing"
	"time"

	"automation-hub-backend/internal/pgtestguard"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestPostgresModelMaintenanceLeaseLifecycle(t *testing.T) {
	dsn := pgtestguard.RequireDedicatedPostgresTestDSN(t, "HAI_LLM_MAINTENANCE_LEASE_TEST_DSN", "hai_llm_maintenance_lease_test")
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	newRepository := func() *GormModelMaintenanceRepository {
		config, err := pgx.ParseConfig(dsn)
		if err != nil {
			t.Fatal("cannot parse validated PostgreSQL test configuration")
		}
		db := stdlib.OpenDB(*config)
		db.SetMaxOpenConns(1)
		db.SetMaxIdleConns(1)
		t.Cleanup(func() {
			if err := db.Close(); err != nil {
				t.Error("cannot close maintenance lease test pool")
			}
		})
		if err := db.PingContext(ctx); err != nil {
			t.Fatal("maintenance lease PostgreSQL is unavailable")
		}
		gormDB, err := gorm.Open(postgres.New(postgres.Config{Conn: db}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent)})
		if err != nil {
			t.Fatal("cannot initialize maintenance lease test repository")
		}
		return &GormModelMaintenanceRepository{DB: gormDB}
	}
	first, second := newRepository(), newRepository()
	model := "lease-fixture-" + uuid.NewString()
	acquire := func(repository *GormModelMaintenanceRepository) func() {
		release, acquired, err := repository.AcquireModelMaintenanceLease(ctx, "fixture-provider", model)
		if err != nil || !acquired || release == nil {
			t.Fatalf("independent session could not acquire released lease: acquired=%t err=%v", acquired, err)
		}
		t.Cleanup(release)
		return release
	}
	releaseFirst := acquire(first)
	releaseBusy, acquired, err := second.AcquireModelMaintenanceLease(ctx, "fixture-provider", model)
	if releaseBusy != nil {
		releaseBusy()
	}
	if err != nil || acquired {
		t.Fatalf("second session bypassed contention: acquired=%t err=%v", acquired, err)
	}
	releaseFirst()
	releaseFirst()
	releaseSecond := acquire(second)
	releaseSecond()
	acquire(first)()
}
