package ambientmonitor

import (
	"automation-hub-backend/internal/infra"
	"automation-hub-backend/internal/lifeontology"
	"automation-hub-backend/internal/outcomeevaluation"
	"automation-hub-backend/internal/proactivity"
	"automation-hub-backend/migrations"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestPostgresSnapshotCursorKeyMatchesJSONBCanonicalText(t *testing.T) {
	got, err := postgresSnapshotCursorKey(`signal-"quoted"`, 7)
	if err != nil {
		t.Fatal(err)
	}
	if want := `["signal-\"quoted\"", 7]`; got != want {
		t.Fatalf("cursor key = %q, want %q", got, want)
	}
}

func TestPostgresCompositionRepositoryLifecycle(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("HAI_AMBIENT_MONITOR_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("HAI_AMBIENT_MONITOR_POSTGRES_TEST_DSN is not configured")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	if _, err := infra.ApplyMigrations(db, migrations.Files, "pre"); err != nil {
		t.Fatalf("apply migrations to ambient monitor test database: %v", err)
	}
	var tableName string
	if err := db.Raw(`SELECT COALESCE(to_regclass('public.outcome_monitor_composition_deliveries')::text, '')`).Row().Scan(&tableName); err != nil || tableName == "" {
		t.Fatalf("migration 0050 is not applied: table=%q err=%v", tableName, err)
	}

	repository := NewPostgresRepository(db)
	now := time.Now().UTC().Truncate(time.Microsecond).Add(-time.Minute)
	current := now
	scope := Scope{OwnerID: "owner-" + uuid.NewString(), WorkspaceID: "workspace-composition"}
	targetID := uuid.NewString()
	collector := &postgresMonitorCollector{value: CollectedObservation{
		Value: 7, ObservedAt: now, SourceDigest: strings.Repeat("d", 64),
	}}
	outcomeService := outcomeevaluation.NewService(outcomeevaluation.NewPostgresRepository(db))
	outcomeScope := outcomeevaluation.Scope{OwnerID: scope.OwnerID, WorkspaceID: scope.WorkspaceID}
	window := outcomeevaluation.LongitudinalWindow{Start: now.Add(-2 * time.Hour), End: now.Add(2 * time.Hour)}
	outcome := outcomeevaluation.IntendedOutcome{
		ID: "outcome-composition", Scope: outcomeScope,
		Statement: "Reduce pending work with verified completion evidence.", LifeDomain: lifeontology.DomainPersonalAdmin, Window: window,
		Indicators: []outcomeevaluation.Indicator{{
			ID: "indicator-pending", Name: "Pending work", Unit: "count", Direction: outcomeevaluation.DirectionLower,
			TargetValue: 0, TrendThresholdPerDay: 0.1, RegressionThreshold: 1, MinimumObservations: 2,
			Baseline: outcomeevaluation.Baseline{
				ID: "baseline-pending", Scope: outcomeScope, Value: 8, ObservedAt: window.Start.Add(-time.Minute),
				Verification: outcomeevaluation.VerificationUnverified,
			},
		}},
	}
	if _, created, err := outcomeService.StoreOutcome(t.Context(), scope.OwnerID, scope.WorkspaceID, outcome.ID, outcomeevaluation.StoreOutcomeRequest{
		IdempotencyKey: "store-" + targetID, Outcome: outcome,
	}); err != nil || !created {
		t.Fatalf("StoreOutcome() = (%v, %v)", created, err)
	}
	proactivityService := proactivity.NewService(proactivity.NewPostgresRepository(db))
	composer := NewComposer(repository, outcomeService, proactivityService)
	service := newService(repository, collector, composer, func() time.Time { return current })
	register := RegisterTargetRequest{
		IdempotencyKey: "register-" + targetID, Scope: scope, TargetID: targetID,
		OutcomeID: "outcome-composition", IndicatorID: "indicator-pending",
		SourceKind: SourceWorkflowOpenLoopCount, Enabled: true, Cadence: 10 * time.Minute,
		FirstRunAt: now, RequestedAt: now,
	}
	if _, created, err := service.RegisterTarget(t.Context(), register); err != nil || !created {
		t.Fatalf("RegisterTarget() = (%v, %v)", created, err)
	}
	current = now.Add(time.Second)
	monitorClaims, err := service.ClaimDue(t.Context(), ClaimDueRequest{
		Scope: scope, WorkerID: "monitor-worker", Now: current,
		LeaseDuration: time.Minute, Limit: 1,
	})
	if err != nil || len(monitorClaims) != 1 {
		t.Fatalf("ClaimDue() = (%+v, %v)", monitorClaims, err)
	}
	current = monitorClaims[0].Lease.ClaimedAt.Add(time.Second)
	collector.value.ObservedAt = current
	completion, err := service.ProcessClaim(t.Context(), ProcessClaimRequest{
		IdempotencyKey: "complete-" + targetID, Scope: scope, TargetID: targetID,
		WorkerID: "monitor-worker", LeaseGeneration: monitorClaims[0].Lease.Generation,
		CompletedAt: current,
	})
	if err != nil {
		t.Fatalf("ProcessClaim() error = %v", err)
	}
	if completion.Run.Status != RunCompleted || completion.Composition.Status != CompositionPending {
		t.Fatalf("completion did not atomically enqueue composition: %+v", completion)
	}

	runUUID, _ := postgresRecordUUID(completion.Run.ID, "run")
	observationUUID, _ := postgresRecordUUID(completion.Observation.ID, "obs")
	deliveryUUID, _ := postgresRecordUUID(completion.Composition.ID, "cmp")
	var runCount, observationCount, deliveryCount int64
	if err := db.Raw(`SELECT count(*) FROM public.outcome_monitor_runs WHERE owner_identity=? AND workspace_key=? AND run_id=?`, scope.OwnerID, scope.WorkspaceID, runUUID).Row().Scan(&runCount); err != nil {
		t.Fatal(err)
	}
	if err := db.Raw(`SELECT count(*) FROM public.outcome_observation_records WHERE owner_identity=? AND workspace_key=? AND observation_id=?`, scope.OwnerID, scope.WorkspaceID, observationUUID).Row().Scan(&observationCount); err != nil {
		t.Fatal(err)
	}
	if err := db.Raw(`SELECT count(*) FROM public.outcome_monitor_composition_deliveries WHERE owner_identity=? AND workspace_key=? AND delivery_id=? AND run_id=?`, scope.OwnerID, scope.WorkspaceID, deliveryUUID, runUUID).Row().Scan(&deliveryCount); err != nil {
		t.Fatal(err)
	}
	if runCount != 1 || observationCount != 1 || deliveryCount != 1 || deliveryUUID != runUUID {
		t.Fatalf("atomic completion counts run=%d observation=%d delivery=%d deliveryUUID=%s runUUID=%s", runCount, observationCount, deliveryCount, deliveryUUID, runUUID)
	}
	replayed, err := service.ProcessClaim(t.Context(), ProcessClaimRequest{
		IdempotencyKey: "complete-" + targetID, Scope: scope, TargetID: targetID,
		WorkerID: "monitor-worker", LeaseGeneration: monitorClaims[0].Lease.Generation,
		CompletedAt: current,
	})
	if err != nil || replayed.Created || replayed.Composition.ID != completion.Composition.ID {
		t.Fatalf("completion replay = (%+v, %v)", replayed, err)
	}
	if err := db.Raw(`SELECT count(*) FROM public.outcome_monitor_composition_deliveries WHERE owner_identity=? AND workspace_key=? AND run_id=?`, scope.OwnerID, scope.WorkspaceID, runUUID).Row().Scan(&deliveryCount); err != nil || deliveryCount != 1 {
		t.Fatalf("delivery count after replay = (%d, %v), want 1", deliveryCount, err)
	}
	current = completion.Composition.NextAttemptAt

	foreign := Scope{OwnerID: "owner-" + uuid.NewString(), WorkspaceID: scope.WorkspaceID}
	if _, err := repository.GetComposition(t.Context(), foreign.OwnerID, foreign.WorkspaceID, completion.Composition.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-owner GetComposition() error = %v, want %v", err, ErrNotFound)
	}
	if items, err := repository.ListCompositions(t.Context(), foreign.OwnerID, foreign.WorkspaceID, targetID, 10); err != nil || len(items) != 0 {
		t.Fatalf("cross-owner ListCompositions() = (%+v, %v)", items, err)
	}
	if items, err := repository.ClaimDueCompositions(t.Context(), foreign.OwnerID, foreign.WorkspaceID, "foreign-worker", current, time.Minute, 1); err != nil || len(items) != 0 {
		t.Fatalf("cross-owner ClaimDueCompositions() = (%+v, %v)", items, err)
	}

	type claimResult struct {
		items []CompositionDelivery
		err   error
	}
	start := make(chan struct{})
	results := make(chan claimResult, 2)
	var wait sync.WaitGroup
	for _, worker := range []string{"composition-worker-a", "composition-worker-b"} {
		worker := worker
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			items, claimErr := repository.ClaimDueCompositions(t.Context(), scope.OwnerID, scope.WorkspaceID, worker, current, time.Minute, 1)
			results <- claimResult{items: items, err: claimErr}
		}()
	}
	close(start)
	wait.Wait()
	close(results)
	var staleClaim CompositionDelivery
	winners := 0
	for result := range results {
		if result.err != nil {
			t.Fatalf("concurrent ClaimDueCompositions() error = %v", result.err)
		}
		if len(result.items) == 1 {
			staleClaim = result.items[0]
			winners++
		} else if len(result.items) != 0 {
			t.Fatalf("concurrent claim returned %d records", len(result.items))
		}
	}
	if winners != 1 {
		t.Fatalf("concurrent composition claim winners = %d, want 1", winners)
	}

	staleAttempt, err := postgresCompositionAttempt(staleClaim, CompositionAttemptSucceeded, "", staleClaim.Lease.ClaimedAt.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	recoveryAt := staleClaim.Lease.ExpiresAt.Add(time.Second)
	if recovered, err := repository.RecoverExpiredCompositionLeases(t.Context(), scope.OwnerID, scope.WorkspaceID, recoveryAt); err != nil || recovered != 1 {
		t.Fatalf("RecoverExpiredCompositionLeases() = (%d, %v), want 1", recovered, err)
	}
	freshClaims, err := repository.ClaimDueCompositions(t.Context(), scope.OwnerID, scope.WorkspaceID, "composition-worker-fresh", recoveryAt.Add(time.Microsecond), time.Minute, 1)
	if err != nil || len(freshClaims) != 1 {
		t.Fatalf("reclaim composition = (%+v, %v)", freshClaims, err)
	}
	if _, _, err := repository.CompleteComposition(t.Context(), scope.OwnerID, scope.WorkspaceID, staleClaim.ID, staleClaim.Lease.WorkerID, staleClaim.Lease.Generation, staleAttempt, staleAttempt.FinishedAt); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("stale CompleteComposition() error = %v, want %v", err, ErrLeaseLost)
	}

	failedAt := freshClaims[0].Lease.ClaimedAt.Add(time.Second)
	failedAttempt, err := postgresCompositionAttempt(freshClaims[0], CompositionAttemptFailed, "sink_unavailable", failedAt)
	if err != nil {
		t.Fatal(err)
	}
	retryAt := failedAt.Add(time.Minute)
	pending, storedFailure, err := repository.FailComposition(t.Context(), scope.OwnerID, scope.WorkspaceID, freshClaims[0].ID, freshClaims[0].Lease.WorkerID, freshClaims[0].Lease.Generation, failedAttempt, retryAt, false)
	if err != nil {
		t.Fatalf("FailComposition() error = %v", err)
	}
	if pending.Status != CompositionPending || pending.AttemptCount != 1 || pending.LastFailureCode != "sink_unavailable" || storedFailure.Status != CompositionAttemptFailed {
		t.Fatalf("retry projection = %+v attempt=%+v", pending, storedFailure)
	}
	if claims, err := repository.ClaimDueCompositions(t.Context(), scope.OwnerID, scope.WorkspaceID, "composition-worker-early", retryAt.Add(-time.Microsecond), time.Minute, 1); err != nil || len(claims) != 0 {
		t.Fatalf("early retry claim = (%+v, %v)", claims, err)
	}
	retryClaims, err := repository.ClaimDueCompositions(t.Context(), scope.OwnerID, scope.WorkspaceID, "composition-worker-success", retryAt, time.Minute, 1)
	if err != nil || len(retryClaims) != 1 {
		t.Fatalf("retry claim = (%+v, %v)", retryClaims, err)
	}
	succeededAt := retryClaims[0].Lease.ClaimedAt.Add(time.Second)
	successAttempt, err := postgresCompositionAttempt(retryClaims[0], CompositionAttemptSucceeded, "", succeededAt)
	if err != nil {
		t.Fatal(err)
	}
	succeeded, storedSuccess, err := repository.CompleteComposition(t.Context(), scope.OwnerID, scope.WorkspaceID, retryClaims[0].ID, retryClaims[0].Lease.WorkerID, retryClaims[0].Lease.Generation, successAttempt, succeededAt)
	if err != nil {
		t.Fatalf("CompleteComposition() error = %v", err)
	}
	if succeeded.Status != CompositionSucceeded || succeeded.AttemptCount != 2 || succeeded.Lease.Active() || storedSuccess.Status != CompositionAttemptSucceeded {
		t.Fatalf("success projection = %+v attempt=%+v", succeeded, storedSuccess)
	}

	attempts, err := repository.ListCompositionAttempts(t.Context(), scope.OwnerID, scope.WorkspaceID, succeeded.ID, 10)
	if err != nil || len(attempts) != 2 || attempts[0].AttemptNumber != 2 || attempts[1].AttemptNumber != 1 {
		t.Fatalf("ListCompositionAttempts() = (%+v, %v)", attempts, err)
	}
	if foreignAttempts, err := repository.ListCompositionAttempts(t.Context(), foreign.OwnerID, foreign.WorkspaceID, succeeded.ID, 10); err != nil || len(foreignAttempts) != 0 {
		t.Fatalf("cross-owner ListCompositionAttempts() = (%+v, %v)", foreignAttempts, err)
	}
	attemptUUID, _ := postgresRecordUUID(attempts[0].ID, "cat")
	if err := db.Exec(`UPDATE public.outcome_monitor_composition_attempts SET failure_code='tampered' WHERE attempt_id=?`, attemptUUID).Error; err == nil {
		t.Fatal("append-only composition attempt accepted an update")
	}
	if err := db.Exec(`DELETE FROM public.outcome_monitor_composition_attempts WHERE attempt_id=?`, attemptUUID).Error; err == nil {
		t.Fatal("append-only composition attempt accepted a delete")
	}
	if err := db.Raw(`SELECT count(*) FROM public.outcome_monitor_composition_attempts WHERE owner_identity=? AND workspace_key=? AND delivery_id=?`, scope.OwnerID, scope.WorkspaceID, deliveryUUID).Row().Scan(&deliveryCount); err != nil || deliveryCount != 2 {
		t.Fatalf("immutable attempt count = (%d, %v), want 2", deliveryCount, err)
	}
}

func postgresCompositionAttempt(delivery CompositionDelivery, status CompositionAttemptStatus, failureCode string, finishedAt time.Time) (CompositionAttempt, error) {
	attempt, err := newCompositionAttempt(delivery, delivery.Lease.WorkerID, finishedAt)
	if err != nil {
		return CompositionAttempt{}, err
	}
	attempt.Status = status
	attempt.FailureCode = failureCode
	attempt.RecordDigest, err = compositionAttemptDigest(attempt)
	return attempt, err
}
