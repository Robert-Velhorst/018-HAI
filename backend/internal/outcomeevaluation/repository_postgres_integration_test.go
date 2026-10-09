package outcomeevaluation

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"automation-hub-backend/internal/pgtestguard"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// TestPostgresRepositoryLifecycle is opt-in. Set
// HAI_OUTCOME_EVALUATION_TEST_DATABASE_DSN to a disposable database where
// migration 0023 has already been applied.
func TestPostgresRepositoryLifecycle(t *testing.T) {
	db := openOutcomePostgresFixture(t)

	repository, err := NewPostgresRepositoryWithLimits(db, HistoryLimits{
		OutcomeRevisions: 2,
		Evaluations:      10,
		Corrections:      10,
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	suffix := fmt.Sprintf("%d", now.UnixNano())
	owner := "outcome-postgres-owner-" + suffix
	workspace := "workspace-" + suffix
	outcomeID := "outcome-" + suffix
	scope := Scope{OwnerID: owner, WorkspaceID: workspace}
	outcomeDefinition := validRequest().Outcome
	outcomeDefinition.ID = outcomeID
	outcomeDefinition.Scope = scope
	for index := range outcomeDefinition.Indicators {
		outcomeDefinition.Indicators[index].Baseline.Scope = scope
	}
	service := newService(repository, func() time.Time { return now })

	stored, created, err := service.StoreOutcome(context.Background(), owner, workspace, outcomeID, StoreOutcomeRequest{
		IdempotencyKey: "create-" + suffix, ExpectedRevision: 0, Outcome: outcomeDefinition,
	})
	if err != nil || !created || stored.Revision != 1 {
		t.Fatalf("StoreOutcome = (%d, %t, %v)", stored.Revision, created, err)
	}
	retry, created, err := service.StoreOutcome(context.Background(), owner, workspace, outcomeID, StoreOutcomeRequest{
		IdempotencyKey: "create-" + suffix, ExpectedRevision: 0, Outcome: outcomeDefinition,
	})
	if err != nil || created || retry.AuditDigest != stored.AuditDigest {
		t.Fatalf("idempotent StoreOutcome = (%t, %v)", created, err)
	}
	if _, err := repository.GetOutcome(context.Background(), "other-"+owner, workspace, outcomeID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-owner read = %v, want ErrNotFound", err)
	}

	for revision := int64(1); revision < 3; revision++ {
		outcomeDefinition.Statement = fmt.Sprintf("Outcome revision %d", revision+1)
		if _, _, err := service.StoreOutcome(context.Background(), owner, workspace, outcomeID, StoreOutcomeRequest{
			IdempotencyKey:   fmt.Sprintf("revision-%d-%s", revision+1, suffix),
			ExpectedRevision: revision, Outcome: outcomeDefinition,
		}); err != nil {
			t.Fatalf("store revision %d: %v", revision+1, err)
		}
	}
	if _, _, err := service.StoreOutcome(context.Background(), owner, workspace, outcomeID, StoreOutcomeRequest{
		IdempotencyKey: "stale-" + suffix, ExpectedRevision: 1, Outcome: outcomeDefinition,
	}); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale revision error = %v", err)
	}
	history, err := repository.ListOutcomeHistory(context.Background(), owner, workspace, outcomeID)
	if err != nil || len(history) != 2 || history[0].Revision != 2 || history[1].Revision != 3 {
		t.Fatalf("bounded outcome history = %#v, err %v", history, err)
	}
	exact, err := service.ResolveOutcomeRevision(context.Background(), owner, workspace, outcomeID, stored.Revision, stored.AuditDigest)
	if err != nil || exact.Revision != stored.Revision || exact.AuditDigest != stored.AuditDigest {
		t.Fatalf("exact historical outcome = %#v, err %v", exact, err)
	}
	if _, err := service.ResolveOutcomeRevision(context.Background(), owner, workspace, outcomeID, stored.Revision, history[1].AuditDigest); !errors.Is(err, ErrNotFound) {
		t.Fatalf("mismatched exact digest error = %v, want ErrNotFound", err)
	}
	if _, err := service.ResolveOutcomeRevision(context.Background(), "other-"+owner, workspace, outcomeID, stored.Revision, stored.AuditDigest); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-owner exact read error = %v, want ErrNotFound", err)
	}

	makeObservation := func(id string, value float64, observedAt time.Time) Observation {
		return Observation{
			ID: id, Scope: scope, IndicatorID: "indicator-1", Value: value,
			ObservedAt: observedAt, RecordedAt: observedAt.Add(time.Minute),
			Verification: VerificationVerified,
			Sources: []SourceReference{{
				ID: id + "-source", URI: "https://evidence.example/" + id,
				ContentDigest: strings.Repeat("a", 64), RetrievedAt: observedAt.Add(time.Minute), Status: SourceVerified,
			}},
			Attribution: Attribution{Method: AttributionControlledStudy, Confidence: 0.8, Rationale: "Postgres integration observation."},
		}
	}
	historicalEvaluation, created, err := service.CreateEvaluation(context.Background(), owner, workspace, outcomeID, CreateEvaluationRequest{
		IdempotencyKey:     "historical-evaluation-" + suffix,
		OutcomeRevision:    stored.Revision,
		OutcomeAuditDigest: stored.AuditDigest,
		Observations: []Observation{
			makeObservation("historical-a-"+suffix, 10, testStart.Add(5*24*time.Hour)),
			makeObservation("historical-b-"+suffix, 12, testStart.Add(15*24*time.Hour)),
		},
		AsOf: testAsOf,
	})
	if err != nil || !created || historicalEvaluation.OutcomeRevision != stored.Revision {
		t.Fatalf("historical CreateEvaluation = (%+v, %t, %v)", historicalEvaluation, created, err)
	}
	if _, _, err := service.CreateEvaluation(context.Background(), owner, workspace, outcomeID, CreateEvaluationRequest{
		IdempotencyKey:     "forged-historical-evaluation-" + suffix,
		OutcomeRevision:    stored.Revision,
		OutcomeAuditDigest: history[1].AuditDigest,
		Observations:       []Observation{makeObservation("forged-historical-"+suffix, 10, testStart.Add(5*24*time.Hour))},
		AsOf:               testAsOf,
	}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("forged historical evaluation error = %v, want ErrNotFound", err)
	}
	evaluationRequest := CreateEvaluationRequest{
		IdempotencyKey: "evaluation-" + suffix, OutcomeRevision: 3,
		Observations: []Observation{
			makeObservation("observation-a-"+suffix, 12, testStart.Add(5*24*time.Hour)),
			makeObservation("observation-b-"+suffix, 16, testStart.Add(15*24*time.Hour)),
		},
		AsOf: testAsOf,
	}
	start := make(chan struct{})
	results := make(chan struct {
		created bool
		err     error
	}, 12)
	var wait sync.WaitGroup
	for range 12 {
		// Independent callers own their request slices. Normalization mutates
		// them, so sharing one fixture would race before the database write.
		request, err := cloneValue(evaluationRequest)
		if err != nil {
			t.Fatal(err)
		}
		wait.Add(1)
		go func(request CreateEvaluationRequest) {
			defer wait.Done()
			<-start
			_, wasCreated, createErr := service.CreateEvaluation(context.Background(), owner, workspace, outcomeID, request)
			results <- struct {
				created bool
				err     error
			}{wasCreated, createErr}
		}(request)
	}
	close(start)
	wait.Wait()
	close(results)
	createdCount := 0
	for result := range results {
		if result.err != nil {
			t.Fatalf("concurrent evaluation: %v", result.err)
		}
		if result.created {
			createdCount++
		}
	}
	if createdCount != 1 {
		t.Fatalf("concurrent evaluation created count = %d, want 1", createdCount)
	}

	original := makeObservation("corrected-"+suffix, 9, testStart.Add(20*24*time.Hour))
	correctionRequest := StoreCorrectionRequest{
		IdempotencyKey: "correction-" + suffix, OutcomeRevision: 3,
		Observation: original,
		Correction: UserCorrection{
			ID: "correction-" + suffix, Scope: scope, ObservationID: original.ID,
			ActorID: owner, UserConfirmed: true, CorrectedValue: 11,
			CorrectedVerification: VerificationUserConfirmed,
			Reason:                "Owner-confirmed Postgres integration correction.", CorrectedAt: original.RecordedAt.Add(time.Hour),
		},
		AsOf: testAsOf,
	}
	correction, created, err := service.StoreCorrection(context.Background(), owner, workspace, outcomeID, correctionRequest)
	if err != nil || !created {
		t.Fatalf("StoreCorrection = (%t, %v)", created, err)
	}
	retriedCorrection, created, err := service.StoreCorrection(context.Background(), owner, workspace, outcomeID, correctionRequest)
	if err != nil || created || retriedCorrection.AuditDigest != correction.AuditDigest {
		t.Fatalf("idempotent StoreCorrection = (%t, %v)", created, err)
	}
}

func openOutcomePostgresFixture(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := pgtestguard.RequireDedicatedPostgresTestDSN(t, "HAI_OUTCOME_EVALUATION_TEST_DATABASE_DSN", "hai_outcome_evaluation_test")
	if strings.EqualFold(strings.TrimSpace(os.Getenv("HAI_OUTCOME_EVALUATION_TEST_BOOTSTRAP_SCHEMA")), "true") {
		t.Fatal("apply canonical migration 0023 before testing; handwritten schema bootstrap is not supported")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open dedicated Postgres: %v", err)
	}
	connection, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := connection.Close(); err != nil {
			t.Errorf("close dedicated Postgres: %v", err)
		}
	})
	var database string
	if err := db.Raw("SELECT current_database()").Row().Scan(&database); err != nil || database != "hai_outcome_evaluation_test" {
		t.Fatalf("connected database = %q, err %v", database, err)
	}
	for _, table := range []string{"outcome_evaluation_outcome_revisions", "outcome_evaluation_evaluations", "outcome_evaluation_corrections"} {
		var count int
		if err := db.Raw(`SELECT count(*) FROM pg_trigger
			WHERE tgrelid = to_regclass(?) AND NOT tgisinternal AND tgenabled = 'O'
			AND tgname IN (?, ?)`, "public."+table, "trg_"+table+"_immutable", "trg_"+table+"_no_truncate").Row().Scan(&count); err != nil || count != 2 {
			t.Fatalf("canonical 0023 append-only triggers for %s = %d, err %v", table, count, err)
		}
	}
	return db
}

func TestPostgresPinnedHistoricalEvaluationReplay(t *testing.T) {
	db := openOutcomePostgresFixture(t)
	repository, err := NewPostgresRepositoryWithLimits(db, HistoryLimits{OutcomeRevisions: 1, Evaluations: 10, Corrections: 10})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	suffix := fmt.Sprintf("%d", now.UnixNano())
	owner, workspace, outcomeID := "snapshot-owner-"+suffix, "snapshot-workspace-"+suffix, "snapshot-outcome-"+suffix
	definition := validRequest().Outcome
	definition.ID, definition.Scope = outcomeID, Scope{OwnerID: owner, WorkspaceID: workspace}
	for index := range definition.Indicators {
		definition.Indicators[index].Baseline.Scope = definition.Scope
	}
	service := newService(repository, func() time.Time { return now })
	original, created, err := service.StoreOutcome(t.Context(), owner, workspace, outcomeID, StoreOutcomeRequest{IdempotencyKey: "original", Outcome: definition})
	if err != nil || !created {
		t.Fatalf("original definition: created %v, err %v", created, err)
	}
	changed, err := cloneValue(original.Outcome)
	if err != nil {
		t.Fatal(err)
	}
	changed.Statement = "Later definition that must not replace the pinned historical target."
	changed.Indicators[0].Direction, changed.Indicators[0].TargetValue = DirectionLower, 5
	later, created, err := service.StoreOutcome(t.Context(), owner, workspace, outcomeID, StoreOutcomeRequest{IdempotencyKey: "later", ExpectedRevision: 1, Outcome: changed})
	if err != nil || !created {
		t.Fatalf("later definition: created %v, err %v", created, err)
	}
	history, err := service.OutcomeHistory(t.Context(), owner, workspace, outcomeID)
	if err != nil || len(history) != 1 || history[0].Revision != later.Revision {
		t.Fatalf("bounded history = %#v, err %v", history, err)
	}
	exact, err := service.ResolveOutcomeRevision(t.Context(), owner, workspace, outcomeID, original.Revision, original.AuditDigest)
	if err != nil || !reflect.DeepEqual(exact, original) {
		t.Fatalf("exact original definition = %#v, err %v", exact, err)
	}
	observations := []Observation{
		observation("pg-snapshot-a-"+suffix, 12, testStart.Add(5*24*time.Hour)),
		observation("pg-snapshot-b-"+suffix, 16, testStart.Add(15*24*time.Hour)),
	}
	for index := range observations {
		observations[index].Scope = definition.Scope
	}
	request := CreateEvaluationRequest{IdempotencyKey: "pinned", OutcomeRevision: original.Revision, OutcomeAuditDigest: original.AuditDigest, Observations: observations, AsOf: testAsOf}
	want, err := Evaluate(EvaluationRequest{Outcome: original.Outcome, Observations: observations, AsOf: testAsOf})
	if err != nil {
		t.Fatal(err)
	}
	currentResult, err := Evaluate(EvaluationRequest{Outcome: later.Outcome, Observations: observations, AsOf: testAsOf})
	if err != nil || currentResult.State == want.State {
		t.Fatalf("fixture must distinguish historical/current semantics: %s/%s, err %v", want.State, currentResult.State, err)
	}
	stored, created, err := service.CreateEvaluation(t.Context(), owner, workspace, outcomeID, request)
	if err != nil || !created || stored.OutcomeRevision != original.Revision || !reflect.DeepEqual(stored.Evaluation, want) {
		t.Fatalf("historical evaluation = %#v, created %v, err %v", stored, created, err)
	}
	before, err := loadEvaluationRow(db, owner, workspace, outcomeID, stored.Evaluation.ID)
	if err != nil {
		t.Fatal(err)
	}
	// A new repository/service proves replay comes from PostgreSQL, not a
	// retained service object, while the recording clock has advanced.
	reopened := newService(NewPostgresRepository(db), func() time.Time { return now.Add(time.Hour) })
	replayed, created, err := reopened.CreateEvaluation(t.Context(), owner, workspace, outcomeID, request)
	if err != nil || created || !reflect.DeepEqual(replayed, stored) {
		t.Fatalf("persisted exact replay = %#v, created %v, err %v", replayed, created, err)
	}
	if err := replayed.Evaluation.ValidateNoAuthority(); err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.ResolveOutcomeRevision(t.Context(), "other-"+owner, workspace, outcomeID, original.Revision, original.AuditDigest); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-owner historical selector = %v", err)
	}
	if _, created, err := reopened.CreateEvaluation(t.Context(), "other-"+owner, workspace, outcomeID, request); !errors.Is(err, ErrNotFound) || created {
		t.Fatalf("cross-owner pinned replay: created %v, err %v", created, err)
	}
	if _, err := reopened.GetEvaluation(t.Context(), "other-"+owner, workspace, outcomeID, stored.Evaluation.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-owner persisted receipt = %v", err)
	}
	token := WriteToken{Key: request.IdempotencyKey, RequestDigest: before.RequestDigest, OutcomeAuditDigest: later.AuditDigest}
	if _, created, err := repository.AppendEvaluation(t.Context(), owner, workspace, outcomeID, stored, token); !errors.Is(err, ErrNotFound) || created {
		t.Fatalf("forged historical digest on persisted replay: created %v, err %v", created, err)
	}
	token.OutcomeAuditDigest = original.AuditDigest
	if _, created, err := repository.AppendEvaluation(t.Context(), owner, workspace, outcomeID, stored, token); err != nil || created {
		t.Fatalf("repository exact replay: created %v, err %v", created, err)
	}
	if err := db.Exec(`UPDATE public.outcome_evaluation_evaluations SET recorded_at = recorded_at + interval '1 second'
		WHERE owner_identity = ? AND workspace_id = ? AND outcome_id = ? AND evaluation_id = ?`, owner, workspace, outcomeID, stored.Evaluation.ID).Error; err == nil || !strings.Contains(err.Error(), "outcome evaluation history is append-only") {
		t.Fatalf("canonical immutability trigger = %v", err)
	}
	after, err := loadEvaluationRow(db, owner, workspace, outcomeID, stored.Evaluation.ID)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("persisted receipt changed: before %#v, after %#v, err %v", before, after, err)
	}
	var count int
	if err := db.Raw(`SELECT count(*) FROM public.outcome_evaluation_evaluations WHERE owner_identity = ? AND workspace_id = ? AND outcome_id = ?`, owner, workspace, outcomeID).Row().Scan(&count); err != nil || count != 1 {
		t.Fatalf("persisted evaluation count = %d, err %v", count, err)
	}
}
