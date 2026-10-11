package infra

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"
)

func TestDefaultDatabaseStartupCancellationStopsNextStep(t *testing.T) {
	for _, stage := range []string{"before_migration", "after_migration", "after_seed"} {
		t.Run(stage, func(t *testing.T) {
			connector := &startupTestConnector{}
			db, _ := startupTestDatabase(t, connector)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if stage == "before_migration" {
				cancel()
			}
			migrationCalls, seedCalls := 0, 0
			result, err := prepareDefaultDatabase(db.WithContext(ctx), func(bound *gorm.DB) error {
				migrationCalls++
				if bound.Statement.Context != ctx {
					t.Fatal("migration lost startup context")
				}
				if stage == "after_migration" {
					cancel()
				}
				return nil
			}, func(bound *gorm.DB) error {
				seedCalls++
				if bound.Statement.Context != ctx {
					t.Fatal("seeding lost startup context")
				}
				cancel()
				return nil
			})
			if result != nil || !errors.Is(err, context.Canceled) || connector.closed.Load() != 1 {
				t.Fatal("cancelled startup published a database or retained its pool")
			}
			if stage == "before_migration" && migrationCalls != 0 {
				t.Fatal("cancelled startup still migrated")
			}
			if stage != "after_seed" && seedCalls != 0 {
				t.Fatal("cancelled migration still seeded accounts")
			}
		})
	}
}

func TestIDPDatabaseStartupTimeoutValidationPrecedesConnection(t *testing.T) {
	for _, raw := range []string{"0s", "-1m", "1", "synthetic-private-invalid-value"} {
		t.Run(raw, func(t *testing.T) {
			t.Setenv("IDP_DB_STARTUP_TIMEOUT", raw)
			db, err := GetDefaultDBContext(context.Background())
			if db != nil || err == nil || !strings.Contains(err.Error(), "IDP_DB_STARTUP_TIMEOUT") || strings.Contains(err.Error(), raw) {
				t.Fatal("invalid startup budget reached connection setup or exposed its value")
			}
		})
	}
}

func TestIDPStartupContextBudgetAndEarlierParentDeadline(t *testing.T) {
	for _, tc := range []struct {
		raw    string
		budget time.Duration
	}{{"", 5 * time.Minute}, {"2m", 2 * time.Minute}} {
		t.Setenv("IDP_DB_STARTUP_TIMEOUT", tc.raw)
		ctx, cancel, err := idpStartupContext(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > tc.budget || time.Until(deadline) < tc.budget-time.Minute {
			t.Fatal("startup budget was not applied")
		}
		cancel()
		if !errors.Is(ctx.Err(), context.Canceled) {
			t.Fatal("startup cancellation was not propagated")
		}
	}
	t.Setenv("IDP_DB_STARTUP_TIMEOUT", "5m")
	parent, parentCancel := context.WithTimeout(context.Background(), time.Second)
	defer parentCancel()
	ctx, cancel, err := idpStartupContext(parent)
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	parentDeadline, _ := parent.Deadline()
	deadline, ok := ctx.Deadline()
	if !ok || !deadline.Equal(parentDeadline) {
		t.Fatal("startup budget extended the caller deadline")
	}
}

func TestIDPStartupRejectsNilAndCancelledParentBeforeConnection(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, parent := range []context.Context{nil, ctx} {
		db, err := GetDefaultDBContext(parent)
		if db != nil || err == nil {
			t.Fatal("invalid startup context reached database opening")
		}
		if parent != nil && !errors.Is(err, context.Canceled) {
			t.Fatal("startup lost caller cancellation")
		}
	}
}
