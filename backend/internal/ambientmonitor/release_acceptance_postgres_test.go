package ambientmonitor

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"automation-hub-backend/internal/infra"
	"automation-hub-backend/internal/lifeontology"
	"automation-hub-backend/internal/outcomeevaluation"
	"automation-hub-backend/internal/proactivity"
	"automation-hub-backend/migrations"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const (
	releaseRecoveryTargetID   = "51000000-0000-0000-0000-000000000001"
	releaseOpenTargetID       = "51000000-0000-0000-0000-000000000002"
	releaseCompletionTargetID = "51000000-0000-0000-0000-000000000003"
	releaseOverdueTargetID    = "51000000-0000-0000-0000-000000000004"
	releaseForeignTargetID    = "51000000-0000-0000-0000-000000000005"
)

// TestPostgresAdvisoryReleaseLifecycle is the release contract for the full
// outcome-monitor handoff. It intentionally uses the production Postgres
// repositories, fixed SQL collectors, outcome evaluator, and proactivity
// advisor. The only injected fault is one transient advisory composition
// failure, allowing exact retry behavior to be proven without an external
// effect provider.
func TestPostgresAdvisoryReleaseLifecycle(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("HAI_AMBIENT_MONITOR_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("HAI_AMBIENT_MONITOR_POSTGRES_TEST_DSN is not configured")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Error)})
	if err != nil {
		t.Fatalf("open Postgres release database: %v", err)
	}
	if _, err := infra.ApplyMigrations(db, migrations.Files, "pre"); err != nil {
		t.Fatalf("apply migrations to ambient monitor release database: %v", err)
	}
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatalf("begin release transaction: %v", tx.Error)
	}
	t.Cleanup(func() { _ = tx.Rollback().Error })

	createCollectorTestTables(t, tx)
	base := time.Now().UTC().Truncate(time.Microsecond)
	ownerA := "owner-release-a-" + uuid.NewString()
	ownerB := "owner-release-b-" + uuid.NewString()
	seedReleaseCollectorRows(t, tx, ownerA, ownerB, base)

	current := base
	repository := NewPostgresRepository(tx)
	collector, err := NewGormCollector(tx, func() time.Time { return current })
	if err != nil {
		t.Fatalf("create release collector: %v", err)
	}
	countedCollector := &releaseCountingCollector{delegate: collector, calls: make(map[string]int)}
	outcomeService := outcomeevaluation.NewServiceWithClock(outcomeevaluation.NewPostgresRepository(tx), func() time.Time { return current })
	proactivityService := proactivity.NewServiceWithClock(proactivity.NewPostgresRepository(tx), func() time.Time { return current })
	composer := NewComposer(repository, outcomeService, proactivityService)
	faultedComposer := &releaseFailOnceComposer{delegate: composer, failuresRemaining: 1}
	service := newService(repository, countedCollector, faultedComposer, func() time.Time { return current })

	scopeA := Scope{OwnerID: ownerA, WorkspaceID: "workspace-release"}
	scopeB := Scope{OwnerID: ownerB, WorkspaceID: "workspace-release"}
	storeReleaseOutcomes(t, outcomeService, scopeA, base)
	storeReleaseOutcomes(t, outcomeService, scopeB, base)
	for _, scope := range []Scope{scopeA, scopeB} {
		if _, created, err := proactivityService.RecordPolicy(t.Context(), scope.OwnerID, "release-policy-"+scope.OwnerID, proactivity.DefaultPreferences(scope.OwnerID)); err != nil || !created {
			t.Fatalf("RecordPolicy(%s) = (created %v, err %v)", scope.OwnerID, created, err)
		}
	}

	registerReleaseTarget(t, service, scopeA, releaseRecoveryTargetID, "outcome-recovery", "indicator-recovery", SourceWorkflowOpenLoopCount, base)
	registerReleaseTarget(t, service, scopeA, releaseOpenTargetID, "outcome-open", "indicator-open", SourceWorkflowOpenLoopCount, base)
	registerReleaseTarget(t, service, scopeA, releaseCompletionTargetID, "outcome-completion", "indicator-completion", SourceWorkflowVerifiedCompletionCount, base)
	registerReleaseTarget(t, service, scopeA, releaseOverdueTargetID, "outcome-overdue", "indicator-overdue", SourceOverdueCommitmentCount, base)
	registerReleaseTarget(t, service, scopeB, releaseForeignTargetID, "outcome-open", "indicator-open", SourceWorkflowOpenLoopCount, base)
	assertReleaseOutcomeResolvable(t, tx, outcomeService, scopeA, "outcome-open", base.Add(3*time.Second))
	assertReleaseOutcomeResolvable(t, tx, outcomeService, scopeA, "outcome-completion", base.Add(3*time.Second))

	before := snapshotReleaseTables(t, tx)

	current = base.Add(time.Second)
	paused, created, err := service.SetEnabled(t.Context(), SetEnabledRequest{
		IdempotencyKey: "release-pause-overdue", Scope: scopeA, TargetID: releaseOverdueTargetID,
		Enabled: false, RequestedAt: current,
	})
	if err != nil || !created || paused.Enabled {
		t.Fatalf("pause target = (%+v, created %v, err %v)", paused, created, err)
	}

	current = base.Add(2 * time.Second)
	crashClaims, err := service.ClaimDue(t.Context(), ClaimDueRequest{
		Scope: scopeA, WorkerID: "release-crashed-worker", Now: current,
		LeaseDuration: time.Minute, Limit: 1,
	})
	if err != nil || len(crashClaims) != 1 || crashClaims[0].ID != releaseRecoveryTargetID {
		t.Fatalf("crash claim = (%+v, %v), want recovery target", crashClaims, err)
	}

	current = base.Add(3 * time.Second)
	first, err := service.ProcessDue(t.Context(), ProcessDueRequest{
		Scope: scopeA, WorkerID: "release-worker-a", Now: current,
		LeaseDuration: time.Minute, Limit: 10,
	})
	if err != nil || first.Claimed != 2 || len(first.Completions) != 2 || len(first.Failures) != 0 ||
		first.Compositions.Claimed != 2 || first.Compositions.Succeeded != 1 || len(first.Compositions.Failures) != 1 ||
		!first.Compositions.Failures[0].Retrying {
		targets, targetsErr := service.Targets(t.Context(), scopeA)
		openCompositions, openErr := service.Compositions(t.Context(), scopeA, releaseOpenTargetID, 10)
		completionCompositions, completionErr := service.Compositions(t.Context(), scopeA, releaseCompletionTargetID, 10)
		t.Fatalf("first release pass = (%+v, %v), targets=(%+v, %v), open compositions=(%+v, %v), completion compositions=(%+v, %v), snapshot errors=%v, collector calls=%v", first, err, targets, targetsErr, openCompositions, openErr, completionCompositions, completionErr, faultedComposer.snapshotErrors(), countedCollector.snapshot())
	}
	failedTarget := faultedComposer.failedTarget()
	if failedTarget == "" || countedCollector.count(failedTarget) != 1 {
		t.Fatalf("transient failure target=%q collector calls=%d, want one source read", failedTarget, countedCollector.count(failedTarget))
	}

	current = base.Add(30 * time.Second)
	if recovered, err := service.RecoverExpiredLeases(t.Context(), scopeA, current); err != nil || recovered != 0 {
		t.Fatalf("active lease recovery = (%d, %v), want 0", recovered, err)
	}
	current = base.Add(64 * time.Second)
	if recovered, err := service.RecoverExpiredLeases(t.Context(), scopeA, current); err != nil || recovered != 1 {
		t.Fatalf("expired lease recovery = (%d, %v), want 1", recovered, err)
	}
	resumed, created, err := service.SetEnabled(t.Context(), SetEnabledRequest{
		IdempotencyKey: "release-resume-overdue", Scope: scopeA, TargetID: releaseOverdueTargetID,
		Enabled: true, RequestedAt: current,
	})
	if err != nil || !created || !resumed.Enabled {
		t.Fatalf("resume target = (%+v, created %v, err %v)", resumed, created, err)
	}

	collectorCallsBeforeRetry := countedCollector.count(failedTarget)
	current = base.Add(65 * time.Second)
	second, err := service.ProcessDue(t.Context(), ProcessDueRequest{
		Scope: scopeA, WorkerID: "release-worker-a-recovered", Now: current,
		LeaseDuration: time.Minute, Limit: 10,
	})
	if err != nil || second.Claimed != 2 || len(second.Completions) != 2 || len(second.Failures) != 0 ||
		second.Compositions.Claimed != 3 || second.Compositions.Succeeded != 3 || len(second.Compositions.Failures) != 0 {
		t.Fatalf("recovery and retry pass = (%+v, %v), collector errors=%v, snapshot errors=%v", second, err, countedCollector.snapshotErrors(), faultedComposer.snapshotErrors())
	}
	if got := countedCollector.count(failedTarget); got != collectorCallsBeforeRetry {
		t.Fatalf("composition retry recollected %s: calls %d -> %d", failedTarget, collectorCallsBeforeRetry, got)
	}

	current = base.Add(66 * time.Second)
	foreign, err := service.ProcessDue(t.Context(), ProcessDueRequest{
		Scope: scopeB, WorkerID: "release-worker-b", Now: current,
		LeaseDuration: time.Minute, Limit: 10,
	})
	if err != nil || foreign.Claimed != 1 || len(foreign.Completions) != 1 || foreign.Compositions.Succeeded != 1 {
		t.Fatalf("foreign owner pass = (%+v, %v)", foreign, err)
	}

	current = base.Add(67 * time.Second)
	replay, err := service.ProcessDue(t.Context(), ProcessDueRequest{
		Scope: scopeA, WorkerID: "release-worker-a-replay", Now: current,
		LeaseDuration: time.Minute, Limit: 10,
	})
	if err != nil || replay.Claimed != 0 || replay.Compositions.Claimed != 0 {
		t.Fatalf("exact replay pass = (%+v, %v), want no new work", replay, err)
	}

	assertReleaseLedgers(t, tx, service, proactivityService, scopeA, scopeB, failedTarget)
	after := snapshotReleaseTables(t, tx)
	assertOnlyAdvisoryTablesChanged(t, before, after)
}

type releaseCountingCollector struct {
	delegate Collector
	mu       sync.Mutex
	calls    map[string]int
	errors   map[string][]string
}

func (c *releaseCountingCollector) Collect(ctx context.Context, target MonitorTarget) (CollectedObservation, error) {
	c.mu.Lock()
	c.calls[target.ID]++
	c.mu.Unlock()
	result, err := c.delegate.Collect(ctx, target)
	if err != nil {
		c.mu.Lock()
		if c.errors == nil {
			c.errors = make(map[string][]string)
		}
		c.errors[target.ID] = append(c.errors[target.ID], err.Error())
		c.mu.Unlock()
	}
	return result, err
}

func (c *releaseCountingCollector) count(targetID string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls[targetID]
}

func (c *releaseCountingCollector) snapshot() map[string]int {
	c.mu.Lock()
	defer c.mu.Unlock()
	result := make(map[string]int, len(c.calls))
	for targetID, count := range c.calls {
		result[targetID] = count
	}
	return result
}

func (c *releaseCountingCollector) snapshotErrors() map[string][]string {
	c.mu.Lock()
	defer c.mu.Unlock()
	result := make(map[string][]string, len(c.errors))
	for targetID, values := range c.errors {
		result[targetID] = append([]string(nil), values...)
	}
	return result
}

type releaseFailOnceComposer struct {
	delegate          *Composer
	mu                sync.Mutex
	failuresRemaining int
	failedTargetID    string
	captureErrors     []string
	composeErrors     []string
}

func (c *releaseFailOnceComposer) CaptureSnapshot(ctx context.Context, signal AdvisorySignal) (CompositionSnapshot, error) {
	snapshot, err := c.delegate.CaptureSnapshot(ctx, signal)
	if err != nil {
		c.mu.Lock()
		c.captureErrors = append(c.captureErrors, err.Error())
		c.mu.Unlock()
	}
	return snapshot, err
}

func (c *releaseFailOnceComposer) Compose(ctx context.Context, signal AdvisorySignal) (CompositionResult, error) {
	c.mu.Lock()
	if c.failuresRemaining > 0 {
		c.failuresRemaining--
		c.failedTargetID = signal.Run.TargetID
		c.mu.Unlock()
		return CompositionResult{}, fmt.Errorf("temporary release fault: %w", ErrSinkFailed)
	}
	c.mu.Unlock()
	result, err := c.delegate.Compose(ctx, signal)
	if err != nil {
		c.mu.Lock()
		c.composeErrors = append(c.composeErrors, err.Error())
		c.mu.Unlock()
	}
	return result, err
}

func (c *releaseFailOnceComposer) failedTarget() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.failedTargetID
}

func (c *releaseFailOnceComposer) snapshotErrors() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	values := append([]string(nil), c.captureErrors...)
	return append(values, c.composeErrors...)
}

func storeReleaseOutcomes(t *testing.T, service *outcomeevaluation.Service, scope Scope, at time.Time) {
	t.Helper()
	window := outcomeevaluation.LongitudinalWindow{Start: at.Add(-24 * time.Hour), End: at.Add(24 * time.Hour)}
	items := []struct {
		id        string
		indicator string
		name      string
		direction outcomeevaluation.DesiredDirection
		target    float64
		baseline  float64
	}{
		{"outcome-recovery", "indicator-recovery", "Recovered open loops", outcomeevaluation.DirectionLower, 0, 2},
		{"outcome-open", "indicator-open", "Open loops", outcomeevaluation.DirectionLower, 0, 2},
		{"outcome-completion", "indicator-completion", "Verified completions", outcomeevaluation.DirectionHigher, 1, 0},
		{"outcome-overdue", "indicator-overdue", "Overdue commitments", outcomeevaluation.DirectionLower, 0, 2},
	}
	for _, item := range items {
		outcomeScope := outcomeevaluation.Scope{OwnerID: scope.OwnerID, WorkspaceID: scope.WorkspaceID}
		outcome := outcomeevaluation.IntendedOutcome{
			ID: item.id, Scope: outcomeScope,
			Statement:  "Track " + strings.ToLower(item.name) + " with source-backed advisory evidence.",
			LifeDomain: lifeontology.DomainPersonalAdmin, Window: window,
			Indicators: []outcomeevaluation.Indicator{{
				ID: item.indicator, Name: item.name, Unit: "count", Direction: item.direction,
				TargetValue: item.target, TrendThresholdPerDay: 0.1, RegressionThreshold: 1, MinimumObservations: 2,
				Baseline: outcomeevaluation.Baseline{
					ID: "baseline-" + item.indicator, Scope: outcomeScope, Value: item.baseline,
					ObservedAt: window.Start.Add(-time.Minute), Verification: outcomeevaluation.VerificationUnverified,
				},
			}},
		}
		if _, created, err := service.StoreOutcome(t.Context(), scope.OwnerID, scope.WorkspaceID, item.id, outcomeevaluation.StoreOutcomeRequest{
			IdempotencyKey: "release-store-" + scope.OwnerID + "-" + item.id, Outcome: outcome,
		}); err != nil || !created {
			t.Fatalf("StoreOutcome(%s, %s) = (created %v, err %v)", scope.OwnerID, item.id, created, err)
		}
	}
}

func registerReleaseTarget(t *testing.T, service *Service, scope Scope, targetID, outcomeID, indicatorID string, source SourceKind, at time.Time) {
	t.Helper()
	if _, created, err := service.RegisterTarget(t.Context(), RegisterTargetRequest{
		IdempotencyKey: "release-register-" + scope.OwnerID + "-" + targetID,
		Scope:          scope, TargetID: targetID, OutcomeID: outcomeID, IndicatorID: indicatorID,
		SourceKind: source, Enabled: true, Cadence: 10 * time.Minute, FirstRunAt: at, RequestedAt: at,
	}); err != nil || !created {
		t.Fatalf("RegisterTarget(%s, %s) = (created %v, err %v)", scope.OwnerID, targetID, created, err)
	}
}

func assertReleaseOutcomeResolvable(t *testing.T, db *gorm.DB, service *outcomeevaluation.Service, scope Scope, outcomeID string, at time.Time) {
	t.Helper()
	revision, err := service.GetOutcome(t.Context(), scope.OwnerID, scope.WorkspaceID, outcomeID)
	if err != nil {
		t.Fatalf("GetOutcome(%s) error = %v", outcomeID, err)
	}
	var count int64
	if err := db.Raw(`SELECT count(*) FROM public.outcome_evaluation_outcome_revisions
		WHERE owner_identity=? AND workspace_id=? AND outcome_id=? AND revision=? AND audit_digest=? AND recorded_at<=?`,
		scope.OwnerID, scope.WorkspaceID, outcomeID, revision.Revision, revision.AuditDigest, at).Row().Scan(&count); err != nil || count != 1 {
		t.Fatalf("outcome snapshot %s is not trigger-resolvable at %s: count=%d err=%v revision=%+v", outcomeID, at, count, err, revision)
	}
}

func seedReleaseCollectorRows(t *testing.T, db *gorm.DB, ownerA, ownerB string, now time.Time) {
	t.Helper()
	const digestA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const digestB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	if err := db.Exec(`INSERT INTO workflow_items (id, owner_identity, archived, current_state) VALUES
		('41000000-0000-0000-0000-000000000001', ?, false, 'active'),
		('41000000-0000-0000-0000-000000000002', ?, false, 'active')`, ownerA, ownerB).Error; err != nil {
		t.Fatalf("seed release workflow items: %v", err)
	}
	if err := db.Exec(`INSERT INTO workflow_open_loops (id, workflow_id, status, follow_up_at, updated_at) VALUES
		('42000000-0000-0000-0000-000000000001', '41000000-0000-0000-0000-000000000001', 'open', ?, ?),
		('42000000-0000-0000-0000-000000000002', '41000000-0000-0000-0000-000000000002', 'open', ?, ?)`,
		now.Add(-time.Hour), now.Add(-time.Hour), now.Add(-time.Hour), now.Add(-time.Hour)).Error; err != nil {
		t.Fatalf("seed release open loops: %v", err)
	}
	if err := db.Exec(`INSERT INTO workflow_completion_attestations
		(id, workflow_id, owner_identity, completion_status, verification_status, record_digest, completed_at) VALUES
		('43000000-0000-0000-0000-000000000001', '41000000-0000-0000-0000-000000000001', ?, 'completed', 'verified', ?, ?),
		('43000000-0000-0000-0000-000000000002', '41000000-0000-0000-0000-000000000002', ?, 'completed', 'test_passed', ?, ?)`,
		ownerA, digestA, now.Add(-time.Hour), ownerB, digestB, now.Add(-time.Hour)).Error; err != nil {
		t.Fatalf("seed release completions: %v", err)
	}
	if err := db.Exec(`INSERT INTO life_ledger_commitment_revisions
		(owner_identity, commitment_key, revision, record_digest, payload, recorded_at) VALUES
		(?, 'release-overdue-a', 1, ?, jsonb_build_object('status','active','dueAt',CAST(? AS text)), ?),
		(?, 'release-overdue-b', 1, ?, jsonb_build_object('status','active','dueAt',CAST(? AS text)), ?)`,
		ownerA, digestA, now.Add(-time.Hour).Format(time.RFC3339Nano), now.Add(-time.Hour),
		ownerB, digestB, now.Add(-time.Hour).Format(time.RFC3339Nano), now.Add(-time.Hour)).Error; err != nil {
		t.Fatalf("seed release commitments: %v", err)
	}
}

func assertReleaseLedgers(t *testing.T, db *gorm.DB, service *Service, advisor *proactivity.Service, scopeA, scopeB Scope, failedTarget string) {
	t.Helper()
	assertReleaseOwnerCount(t, db, "outcome_observation_records", scopeA.OwnerID, 4)
	assertReleaseOwnerCount(t, db, "outcome_monitor_runs", scopeA.OwnerID, 4)
	assertReleaseOwnerCount(t, db, "outcome_monitor_composition_deliveries", scopeA.OwnerID, 4)
	assertReleaseOwnerCount(t, db, "outcome_monitor_composition_attempts", scopeA.OwnerID, 5)
	assertReleaseOwnerCount(t, db, "outcome_observation_records", scopeB.OwnerID, 1)
	assertReleaseOwnerCount(t, db, "outcome_monitor_runs", scopeB.OwnerID, 1)
	assertReleaseOwnerCount(t, db, "outcome_monitor_composition_deliveries", scopeB.OwnerID, 1)
	assertReleaseOwnerCount(t, db, "outcome_monitor_composition_attempts", scopeB.OwnerID, 1)

	observations, err := service.Observations(t.Context(), scopeA, failedTarget, 10)
	if err != nil || len(observations) != 1 {
		t.Fatalf("failed-target observations = (%+v, %v), want one immutable source read", observations, err)
	}
	deliveries, err := service.Compositions(t.Context(), scopeA, failedTarget, 10)
	if err != nil || len(deliveries) != 1 || deliveries[0].Status != CompositionSucceeded || deliveries[0].AttemptCount != 2 {
		t.Fatalf("failed-target delivery = (%+v, %v), want one succeeded two-attempt delivery", deliveries, err)
	}
	attempts, err := service.CompositionAttempts(t.Context(), scopeA, deliveries[0].ID, 10)
	if err != nil || len(attempts) != 2 || attempts[0].Status != CompositionAttemptSucceeded || attempts[1].Status != CompositionAttemptFailed {
		t.Fatalf("failed-target attempts = (%+v, %v)", attempts, err)
	}
	if foreign, err := service.Observations(t.Context(), scopeB, failedTarget, 10); err != nil || len(foreign) != 0 {
		t.Fatalf("cross-owner observations = (%+v, %v), want none", foreign, err)
	}

	for _, scope := range []Scope{scopeA, scopeB} {
		inbox, err := advisor.Inbox(t.Context(), scope.OwnerID, 100)
		if err != nil || inbox.Authority != proactivity.InboxAuthority || inbox.CanExecute {
			t.Fatalf("Inbox(%s) = (%+v, %v)", scope.OwnerID, inbox, err)
		}
		for _, item := range inbox.Items {
			if item.CanExecute || item.DeliveryAuthorized || item.ExecutionAuthorized ||
				item.Decision.Decision.AuthorityGranted || item.Decision.Decision.DeliveryAuthorized || item.Decision.Decision.ExecutionAuthorized {
				t.Fatalf("inbox granted effect authority for %s: %+v", scope.OwnerID, item)
			}
		}
	}
}

func assertReleaseOwnerCount(t *testing.T, db *gorm.DB, table, owner string, want int64) {
	t.Helper()
	var count int64
	query := fmt.Sprintf(`SELECT count(*) FROM public.%s WHERE owner_identity = ?`, quoteReleaseIdentifier(table))
	if err := db.Raw(query, owner).Row().Scan(&count); err != nil || count != want {
		t.Fatalf("%s owner %s count = (%d, %v), want %d", table, owner, count, err, want)
	}
}

type releaseTableFingerprint struct {
	Rows   int64
	Digest string
}

var releaseIdentifierPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

func snapshotReleaseTables(t *testing.T, db *gorm.DB) map[string]releaseTableFingerprint {
	t.Helper()
	var tables []string
	if err := db.Raw(`SELECT tablename FROM pg_tables WHERE schemaname='public' ORDER BY tablename`).Scan(&tables).Error; err != nil {
		t.Fatalf("list public release tables: %v", err)
	}
	result := make(map[string]releaseTableFingerprint, len(tables))
	for _, table := range tables {
		if !releaseIdentifierPattern.MatchString(table) {
			t.Fatalf("unsafe public table identifier %q", table)
		}
		var fingerprint releaseTableFingerprint
		query := fmt.Sprintf(`SELECT count(*) AS rows,
			COALESCE(md5(string_agg(row_to_json(t)::text, '' ORDER BY row_to_json(t)::text)), md5('')) AS digest
			FROM public.%s AS t`, quoteReleaseIdentifier(table))
		if err := db.Raw(query).Scan(&fingerprint).Error; err != nil {
			t.Fatalf("fingerprint public table %s: %v", table, err)
		}
		result[table] = fingerprint
	}
	return result
}

func assertOnlyAdvisoryTablesChanged(t *testing.T, before, after map[string]releaseTableFingerprint) {
	t.Helper()
	allowed := map[string]struct{}{
		"outcome_observation_records": {}, "outcome_monitor_targets": {}, "outcome_monitor_commands": {},
		"outcome_monitor_runs": {}, "outcome_monitor_composition_deliveries": {}, "outcome_monitor_composition_attempts": {},
		"outcome_evaluation_evaluations": {}, "proactivity_idempotency": {}, "proactivity_signal_batches": {},
		"proactivity_signal_records": {}, "proactivity_decision_batches": {}, "proactivity_decision_records": {},
	}
	changed := make([]string, 0)
	for table, afterFingerprint := range after {
		beforeFingerprint, found := before[table]
		if !found {
			t.Fatalf("public table %s appeared during advisory lifecycle", table)
		}
		if beforeFingerprint == afterFingerprint {
			continue
		}
		changed = append(changed, table)
		if _, ok := allowed[table]; !ok {
			t.Fatalf("advisory lifecycle mutated forbidden table %s: before=%+v after=%+v", table, beforeFingerprint, afterFingerprint)
		}
	}
	for table := range before {
		if _, found := after[table]; !found {
			t.Fatalf("public table %s disappeared during advisory lifecycle", table)
		}
	}
	sort.Strings(changed)
	for required := range allowed {
		if required == "outcome_monitor_targets" || required == "outcome_monitor_commands" ||
			required == "outcome_monitor_runs" || required == "outcome_observation_records" ||
			required == "outcome_monitor_composition_deliveries" || required == "outcome_monitor_composition_attempts" ||
			required == "outcome_evaluation_evaluations" || required == "proactivity_signal_records" ||
			required == "proactivity_decision_records" {
			if !containsReleaseTable(changed, required) {
				t.Fatalf("required advisory table %s did not change; changed=%v", required, changed)
			}
		}
	}
}

func quoteReleaseIdentifier(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

func containsReleaseTable(items []string, expected string) bool {
	index := sort.SearchStrings(items, expected)
	return index < len(items) && items[index] == expected
}
