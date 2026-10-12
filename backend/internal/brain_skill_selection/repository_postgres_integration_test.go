//go:build integration

// This test appends permanent audit events and must only run against a
// disposable, migrated HAI PostgreSQL test database. The audit table is
// append-only. The test never applies migrations, resets schema, or removes
// rows; it uses unique synthetic owner identities and rolls back mutation
// probes even if an expected database trigger is missing.
package brain_skill_selection

import (
	"context"
	"errors"
	"testing"
	"time"

	"automation-hub-backend/internal/brainskills"
	"automation-hub-backend/internal/pgtestguard"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const appendOnlySelectionError = "brain skill selection decisions are append-only"

var errMutationUnexpectedlySucceeded = errors.New("mutation unexpectedly succeeded")

func TestPostgresRepositoryAndSelectionServiceIntegration(t *testing.T) {
	db := openBrainSkillSelectionIntegrationDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	repository := NewPostgresRepository(db)
	service := NewSelectionService(repository, brainskills.DefaultCatalog())
	owner := "brain-skill-selection-test-" + uuid.NewString()
	otherOwner := "brain-skill-selection-test-" + uuid.NewString()
	const skillID = "frontend-design"

	enabledEvent, err := service.Set(ctx, owner, owner, skillID, true, currentCatalogFingerprint())
	if err != nil {
		t.Fatal("could not append enable decision")
	}
	if enabledEvent.ID <= 0 {
		t.Fatal("enable decision did not receive a positive database event ID")
	}

	guidance, err := service.GuidanceForTask(ctx, owner, "frontend", "Redesign the dashboard")
	if err != nil {
		t.Fatal("could not read enabled task guidance")
	}
	if len(guidance) != 1 || guidance[0].ID != skillID || guidance[0].SelectionDecisionID != enabledEvent.ID {
		t.Fatal("enabled task guidance did not match its persisted consent decision")
	}

	disabledEvent, err := service.Set(ctx, owner, owner, skillID, false, "")
	if err != nil {
		t.Fatal("could not append disable decision")
	}
	if disabledEvent.ID <= enabledEvent.ID {
		t.Fatal("selection event IDs were not positive and strictly increasing")
	}

	latest, err := repository.LatestForOwner(ctx, owner)
	if err != nil {
		t.Fatal("could not read latest owner selection")
	}
	if len(latest) != 1 || latest[0].ID != disabledEvent.ID || latest[0].Enabled {
		t.Fatal("latest owner selection is not the persisted disable decision")
	}

	states, err := service.StatesForOwner(ctx, owner)
	if err != nil {
		t.Fatal("could not read owner selection state")
	}
	state, found := integrationSelectionState(states, skillID)
	if !found || state.Status != StatusDisabled || state.Enabled || state.NeedsReapproval ||
		state.Decision == nil || state.Decision.ID != disabledEvent.ID || state.Decision.Enabled {
		t.Fatal("selection service did not report the latest disabled state")
	}

	guidance, err = service.GuidanceForTask(ctx, owner, "frontend", "Redesign the dashboard")
	if err != nil {
		t.Fatal("could not verify revoked task guidance")
	}
	if len(guidance) != 0 {
		t.Fatal("task guidance remained available after the owner disabled the skill")
	}

	otherEvents, err := repository.LatestForOwner(ctx, otherOwner)
	if err != nil {
		t.Fatal("could not read second owner's selection scope")
	}
	if len(otherEvents) != 0 {
		t.Fatal("second owner could see another owner's selection records")
	}
	otherStates, err := service.StatesForOwner(ctx, otherOwner)
	if err != nil {
		t.Fatal("could not read second owner's current state")
	}
	otherState, found := integrationSelectionState(otherStates, skillID)
	if !found || otherState.Status != StatusNotSelected || otherState.Enabled || otherState.Decision != nil {
		t.Fatal("second owner's state exposed another owner's decision")
	}
	otherGuidance, err := service.GuidanceForTask(ctx, otherOwner, "frontend", "Redesign the dashboard")
	if err != nil {
		t.Fatal("could not read second owner's task guidance")
	}
	if len(otherGuidance) != 0 {
		t.Fatal("second owner received another owner's task guidance")
	}

	assertAppendOnlyMutationRejected(t, ctx, db, "update", `
		UPDATE public.brain_skill_selection_events
		SET enabled = TRUE
		WHERE owner_identity = ? AND id = ?`, owner, disabledEvent.ID)
	assertAppendOnlyMutationRejected(t, ctx, db, "delete", `
		DELETE FROM public.brain_skill_selection_events
		WHERE owner_identity = ? AND id = ?`, owner, disabledEvent.ID)
	assertAppendOnlyMutationRejected(t, ctx, db, "truncate", `
		TRUNCATE TABLE public.brain_skill_selection_events`)

	var ownerEventCount int64
	if err := db.WithContext(ctx).
		Table("public.brain_skill_selection_events").
		Where("owner_identity = ?", owner).
		Count(&ownerEventCount).Error; err != nil {
		t.Fatal("could not verify append-only audit rows remain")
	}
	if ownerEventCount != 2 {
		t.Fatal("append-only mutation probes changed the owner's audit rows")
	}
	latest, err = repository.LatestForOwner(ctx, owner)
	if err != nil || len(latest) != 1 || latest[0].ID != disabledEvent.ID || latest[0].Enabled {
		t.Fatal("owner state did not remain unchanged after append-only mutation probes")
	}
}

func TestPostgresRepositoryConcurrentOpposingSelectionWrites(t *testing.T) {
	db := openBrainSkillSelectionIntegrationDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	repository := NewPostgresRepository(db)
	service := NewSelectionService(repository, brainskills.DefaultCatalog())
	owner := "brain-skill-selection-race-test-" + uuid.NewString()
	const skillID = "frontend-design"

	type writeResult struct {
		requested bool
		event     SelectionEvent
		err       error
	}
	start := make(chan struct{})
	ready := make(chan struct{}, 2)
	results := make(chan writeResult, 2)
	for _, enabled := range []bool{true, false} {
		enabled := enabled
		go func() {
			ready <- struct{}{}
			<-start
			event, err := service.Set(ctx, owner, owner, skillID, enabled, currentCatalogFingerprintFor(enabled))
			results <- writeResult{requested: enabled, event: event, err: err}
		}()
	}
	<-ready
	<-ready
	close(start)

	returned := make(map[int64]SelectionEvent, 2)
	for range 2 {
		select {
		case result := <-results:
			if result.err != nil {
				t.Fatalf("concurrent enabled=%t selection write failed: %v", result.requested, result.err)
			}
			if result.event.ID <= 0 || result.event.Enabled != result.requested {
				t.Fatalf("concurrent enabled=%t write returned unexpected event: %+v", result.requested, result.event)
			}
			if _, duplicate := returned[result.event.ID]; duplicate {
				t.Fatalf("concurrent writes returned the same event ID %d", result.event.ID)
			}
			returned[result.event.ID] = result.event
		case <-ctx.Done():
			t.Fatal("timed out waiting for concurrent selection writes")
		}
	}
	if len(returned) != 2 {
		t.Fatalf("persisted %d distinct decisions, want one enable and one disable", len(returned))
	}

	var persisted []SelectionEvent
	if err := db.WithContext(ctx).
		Table("public.brain_skill_selection_events").
		Where("owner_identity = ? AND skill_id = ?", owner, skillID).
		Order("id ASC").
		Find(&persisted).Error; err != nil {
		t.Fatal("could not read persisted concurrent selection history")
	}
	if len(persisted) != 2 || persisted[0].ID <= 0 || persisted[1].ID <= persisted[0].ID {
		t.Fatalf("persisted event order is not two distinct increasing decisions: %+v", persisted)
	}
	for _, event := range persisted {
		written, ok := returned[event.ID]
		if !ok || written.Enabled != event.Enabled || written.ActorIdentity != event.ActorIdentity ||
			written.CatalogFingerprint != event.CatalogFingerprint {
			t.Fatalf("persisted event %d does not match a completed write response: %+v", event.ID, event)
		}
	}
	if persisted[0].Enabled == persisted[1].Enabled {
		t.Fatalf("concurrent history did not contain opposing decisions: %+v", persisted)
	}

	lastPersisted := persisted[len(persisted)-1]
	latest, err := repository.LatestForOwner(ctx, owner)
	if err != nil {
		t.Fatal("could not read the effective persisted selection")
	}
	if len(latest) != 1 || latest[0].ID != lastPersisted.ID || latest[0].Enabled != lastPersisted.Enabled {
		t.Fatalf("repository latest state does not match final persisted event %d: %+v", lastPersisted.ID, latest)
	}

	inventory, err := NewOwnerSelectionStoreAdapter(service).StatesForOwner(
		ctx, owner, brainskills.DefaultCatalog().List(),
	)
	if err != nil {
		t.Fatal("could not read owner inventory after concurrent decisions")
	}
	inventoryState, ok := inventory[skillID]
	if !ok || inventoryState.Enabled != lastPersisted.Enabled || inventoryState.NeedsReapproval ||
		inventoryState.Decision == nil || inventoryState.Decision.ID != lastPersisted.ID ||
		inventoryState.Decision.ActorIdentity != lastPersisted.ActorIdentity ||
		!inventoryState.Decision.DecidedAt.Equal(lastPersisted.DecidedAt) {
		t.Fatalf("inventory state does not match final persisted event %d: %+v", lastPersisted.ID, inventoryState)
	}

	effectiveGuidance, err := service.GuidanceForTask(ctx, owner, "frontend", "Redesign the dashboard")
	if err != nil {
		t.Fatal("could not read effective task guidance after concurrent decisions")
	}
	if lastPersisted.Enabled {
		if len(effectiveGuidance) != 1 || effectiveGuidance[0].SelectionDecisionID != lastPersisted.ID {
			t.Fatalf("effective guidance does not match final enable event %d: %+v", lastPersisted.ID, effectiveGuidance)
		}
	} else if len(effectiveGuidance) != 0 {
		t.Fatalf("guidance remained effective after final disable event %d: %+v", lastPersisted.ID, effectiveGuidance)
	}
}

func openBrainSkillSelectionIntegrationDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := pgtestguard.RequireDedicatedPostgresTestDSN(t, "HAI_BRAIN_SKILL_SELECTION_TEST_DATABASE_DSN", "hai_brain_skill_selection_test")

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal("could not open the configured PostgreSQL integration database")
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal("could not access the configured PostgreSQL connection pool")
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	var databaseName string
	if err := db.Raw("SELECT current_database()").Scan(&databaseName).Error; err != nil {
		t.Fatal("could not verify the connected database identity")
	}
	if databaseName != "hai_brain_skill_selection_test" {
		t.Fatalf("refusing append-only integration writes against unexpected database identity %q", databaseName)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var migrationApplied bool
	if err := db.WithContext(ctx).Raw(`
		SELECT EXISTS (
			SELECT 1 FROM public.schema_migrations
			WHERE version = 'pre/0084_brain_skill_selection_events'
		)`).Scan(&migrationApplied).Error; err != nil {
		t.Fatal("could not verify migration 0084; use a disposable migrated HAI test database")
	}
	if !migrationApplied {
		t.Fatal("migration 0084 is not recorded; this test never applies migrations")
	}

	var enabledTriggerCount int64
	if err := db.WithContext(ctx).Raw(`
		SELECT count(*)
		FROM pg_trigger trigger_row
		JOIN pg_class table_row ON table_row.oid = trigger_row.tgrelid
		JOIN pg_namespace schema_row ON schema_row.oid = table_row.relnamespace
		WHERE schema_row.nspname = 'public'
		  AND table_row.relname = 'brain_skill_selection_events'
		  AND trigger_row.tgname IN (
			'trg_brain_skill_selection_events_immutable',
			'trg_brain_skill_selection_events_no_truncate'
		  )
		  AND trigger_row.tgenabled IN ('O', 'A')
	`).Scan(&enabledTriggerCount).Error; err != nil {
		t.Fatal("could not verify migration 0084 append-only triggers")
	}
	if enabledTriggerCount != 2 {
		t.Fatal("migration 0084 append-only triggers are missing or disabled; no writes attempted")
	}
	return db
}

func integrationSelectionState(states []SelectionState, skillID string) (SelectionState, bool) {
	for _, state := range states {
		if state.ID == skillID {
			return state, true
		}
	}
	return SelectionState{}, false
}

func assertAppendOnlyMutationRejected(t *testing.T, ctx context.Context, db *gorm.DB, operation, statement string, args ...any) {
	t.Helper()
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(statement, args...).Error; err != nil {
			return err
		}
		// All mutation probes run in a transaction. If a trigger is absent,
		// deliberately abort so even an unexpected mutation cannot persist.
		return errMutationUnexpectedlySucceeded
	})
	if errors.Is(err, errMutationUnexpectedlySucceeded) {
		t.Fatalf("append-only %s trigger did not reject the mutation", operation)
	}
	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) || postgresError.Code != "55000" || postgresError.Message != appendOnlySelectionError {
		t.Fatalf("append-only %s trigger did not return the expected PostgreSQL rejection", operation)
	}
}
