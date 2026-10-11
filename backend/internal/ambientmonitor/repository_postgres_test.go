package ambientmonitor

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestPostgresRepositoryRejectsUnavailableStorage(t *testing.T) {
	t.Parallel()
	repository := NewPostgresRepository(nil)
	if _, err := repository.GetTarget(context.Background(), "owner-a", "workspace-a", uuid.NewString()); !errors.Is(err, ErrRepositoryUnavailable) {
		t.Fatalf("GetTarget() error = %v, want %v", err, ErrRepositoryUnavailable)
	}
	if _, _, err := repository.FindCompletion(context.Background(), "owner-a", "workspace-a", "completion-a"); !errors.Is(err, ErrRepositoryUnavailable) {
		t.Fatalf("FindCompletion() error = %v, want %v", err, ErrRepositoryUnavailable)
	}
	if _, err := repository.ListTargets(nil, "owner-a", "workspace-a"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("ListTargets(nil) error = %v, want %v", err, ErrInvalidInput)
	}
}

func TestPostgresListDueScopesValidatesLimit(t *testing.T) {
	t.Parallel()
	repository := NewPostgresRepository(&gorm.DB{})
	if _, err := repository.ListDueScopes(context.Background(), time.Now().UTC(), 0); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("ListDueScopes() error = %v, want %v", err, ErrInvalidInput)
	}
}

func TestPostgresRecordIDRoundTrip(t *testing.T) {
	t.Parallel()
	for _, prefix := range []string{"obs", "run"} {
		original := prefix + "-00112233445566778899aabbccddeeff"
		stored, err := postgresRecordUUID(original, prefix)
		if err != nil {
			t.Fatalf("postgresRecordUUID(%q) error = %v", original, err)
		}
		restored, err := domainRecordID(stored.String(), prefix)
		if err != nil || restored != original {
			t.Fatalf("domainRecordID() = (%q, %v), want %q", restored, err, original)
		}
	}
	if _, err := postgresRecordUUID("obs-not-a-uuid", "obs"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("invalid record id error = %v, want %v", err, ErrInvalidInput)
	}
}

func TestPostgresTargetIDRequiresCanonicalUUID(t *testing.T) {
	t.Parallel()
	canonical := uuid.NewString()
	if parsed, err := postgresTargetUUID(canonical); err != nil || parsed.String() != canonical {
		t.Fatalf("canonical target UUID = (%s, %v)", parsed, err)
	}
	for _, invalid := range []string{"target-123", strings.ToUpper(canonical), " " + canonical} {
		if _, err := postgresTargetUUID(invalid); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("postgresTargetUUID(%q) error = %v, want %v", invalid, err, ErrInvalidInput)
		}
	}
}

func TestPostgresClaimIDFencesWorkerAndGeneration(t *testing.T) {
	t.Parallel()
	targetID := uuid.NewString()
	base := postgresClaimID("owner-a", "workspace-a", targetID, "worker-a", 2)
	if replay := postgresClaimID("owner-a", "workspace-a", targetID, "worker-a", 2); replay != base {
		t.Fatalf("claim token is not deterministic: %s != %s", replay, base)
	}
	if otherWorker := postgresClaimID("owner-a", "workspace-a", targetID, "worker-b", 2); otherWorker == base {
		t.Fatal("different workers received the same claim token")
	}
	if otherGeneration := postgresClaimID("owner-a", "workspace-a", targetID, "worker-a", 3); otherGeneration == base {
		t.Fatal("different generations received the same claim token")
	}
	longWorker := "worker/region-eu/node-" + strings.Repeat("a", 80)
	if err := validatePostgresWorker(longWorker); err != nil {
		t.Fatalf("bounded long worker rejected: %v", err)
	}
	if longClaim := postgresClaimID("owner-a", "workspace-a", targetID, longWorker, 4); longClaim == uuid.Nil || longClaim == base {
		t.Fatalf("long worker claim is not opaque and distinct: %s", longClaim)
	}
	if err := validatePostgresWorker("w" + strings.Repeat("a", 128)); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("long free-form worker error = %v, want %v", err, ErrInvalidInput)
	}
}

func TestDecodePostgresTargetPreservesScopeAndAdvisoryAuthority(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC)
	row := postgresTargetRow{
		TargetID: uuid.NewString(), OwnerIdentity: "owner-a", WorkspaceKey: "workspace-a",
		OutcomeID: "outcome-a", IndicatorID: "indicator-a", SourceKind: string(SourceWorkflowOpenLoopCount),
		CadenceSeconds: 600, NextRunAt: now, Enabled: true, Revision: 1, CreatedAt: now, UpdatedAt: now,
	}
	target, err := decodePostgresTarget(row, "owner-a", "workspace-a")
	if err != nil {
		t.Fatal(err)
	}
	if target.Scope != (Scope{OwnerID: "owner-a", WorkspaceID: "workspace-a"}) || target.Lease.Active() {
		t.Fatalf("decoded target = %+v", target)
	}
	if err := validateAuthority(target.Authority); err != nil {
		t.Fatalf("decoded authority = %+v: %v", target.Authority, err)
	}
	if _, err := decodePostgresTarget(row, "owner-b", "workspace-a"); !errors.Is(err, ErrCorruptStorage) {
		t.Fatalf("cross-owner decode error = %v, want %v", err, ErrCorruptStorage)
	}
}

func TestPostgresFailureEncodingRoundTrip(t *testing.T) {
	t.Parallel()
	encoded := encodePostgresFailure("collector_failed", "source snapshot unavailable")
	code, summary := decodePostgresFailure(encoded)
	if code != "collector_failed" || summary != "source snapshot unavailable" {
		t.Fatalf("decoded failure = (%q, %q)", code, summary)
	}
}

func TestPostgresRepositoryLifecycle(t *testing.T) {
	db := openAmbientMonitorPostgresTestDatabase(t)
	var tableName string
	if err := db.Raw(`SELECT COALESCE(to_regclass('public.outcome_monitor_targets')::text, '')`).Row().Scan(&tableName); err != nil || tableName == "" {
		t.Fatalf("migration 0049 is not applied: table=%q err=%v", tableName, err)
	}

	now := time.Now().UTC().Truncate(time.Microsecond).Add(-time.Minute)
	current := now
	collector := &postgresMonitorCollector{}
	repository := NewPostgresRepository(db)
	service := newService(repository, collector, nil, func() time.Time { return current })
	scope := Scope{OwnerID: "owner-" + uuid.NewString(), WorkspaceID: "workspace-hai"}
	targetID := uuid.NewString()
	register := RegisterTargetRequest{
		IdempotencyKey: "register-" + targetID, Scope: scope, TargetID: targetID,
		OutcomeID: "outcome-reliable-work", IndicatorID: "indicator-open-loops",
		SourceKind: SourceWorkflowOpenLoopCount, Enabled: true, Cadence: 10 * time.Minute,
		FirstRunAt: now, RequestedAt: now,
	}
	target, created, err := service.RegisterTarget(t.Context(), register)
	if err != nil || !created || target.ID != targetID {
		t.Fatalf("RegisterTarget() = (%+v, %v, %v)", target, created, err)
	}
	replayedTarget, replayCreated, err := service.RegisterTarget(t.Context(), register)
	if err != nil || replayCreated || replayedTarget.ID != target.ID {
		t.Fatalf("RegisterTarget replay = (%+v, %v, %v)", replayedTarget, replayCreated, err)
	}
	conflictingRegister := register
	conflictingRegister.OutcomeID = "outcome-conflict"
	if _, _, err := service.RegisterTarget(t.Context(), conflictingRegister); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("RegisterTarget same key with different digest error = %v, want %v", err, ErrIdempotencyConflict)
	}
	otherWorkspaceRegister := register
	otherWorkspaceRegister.Scope.WorkspaceID = "workspace-other"
	otherWorkspaceRegister.TargetID = uuid.NewString()
	otherWorkspaceRegister.FirstRunAt = now.Add(24 * time.Hour)
	if _, createdOther, err := service.RegisterTarget(t.Context(), otherWorkspaceRegister); err != nil || !createdOther {
		t.Fatalf("RegisterTarget same key in another workspace = (%v, %v)", createdOther, err)
	}
	if _, err := repository.GetTarget(t.Context(), scope.OwnerID, "workspace-other", targetID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-workspace GetTarget() error = %v, want %v", err, ErrNotFound)
	}
	dueScopes, err := repository.ListDueScopes(t.Context(), now, 1)
	if err != nil || len(dueScopes) != 1 || dueScopes[0] != scope {
		t.Fatalf("ListDueScopes() = (%+v, %v), want exact owner/workspace", dueScopes, err)
	}

	current = now.Add(time.Second)
	workerID := "worker/region-eu/node-" + strings.Repeat("a", 80)
	claims, err := service.ClaimDue(t.Context(), ClaimDueRequest{
		Scope: scope, WorkerID: workerID, Now: current, LeaseDuration: time.Minute, Limit: 1,
	})
	if err != nil || len(claims) != 1 || claims[0].Lease.WorkerID != workerID || claims[0].Lease.Generation < 2 {
		t.Fatalf("ClaimDue() = (%+v, %v)", claims, err)
	}
	claim := claims[0]
	if dueScopes, err = repository.ListDueScopes(t.Context(), current, 10); err != nil || len(dueScopes) != 0 {
		t.Fatalf("ListDueScopes() while leased = (%+v, %v), want none", dueScopes, err)
	}
	current = claim.Lease.ClaimedAt.Add(time.Second)
	collector.value = CollectedObservation{Value: 3, ObservedAt: current, SourceDigest: strings.Repeat("a", 64)}
	if _, err := service.Complete(t.Context(), CompleteRequest{
		IdempotencyKey: "wrong-worker-" + targetID, Scope: scope, TargetID: targetID,
		WorkerID: "worker-b", LeaseGeneration: claim.Lease.Generation,
		Collected: collector.value, CompletedAt: current,
	}); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("wrong-worker Complete() error = %v, want %v", err, ErrLeaseLost)
	}
	leaseRow, foundLease, err := loadPostgresTarget(db, scope.OwnerID, scope.WorkspaceID, targetID, false)
	if err != nil || !foundLease {
		t.Fatalf("load lease after fenced worker = (%+v, %v, %v)", leaseRow, foundLease, err)
	}
	expectedClaim := postgresClaimID(scope.OwnerID, scope.WorkspaceID, targetID, workerID, claim.Lease.Generation)
	if !leaseRow.LeaseID.Valid || leaseRow.LeaseID.String != expectedClaim.String() ||
		!leaseRow.LeaseOwner.Valid || leaseRow.LeaseOwner.String != workerID ||
		leaseRow.Revision != int64(claim.Lease.Generation) || !leaseRow.UpdatedAt.Equal(claim.Lease.ClaimedAt) ||
		!leaseRow.LeaseUntil.Valid || !leaseRow.LeaseUntil.Time.After(current) {
		t.Fatalf("persisted lease changed after fenced worker: row=%+v expectedClaim=%s claim=%+v", leaseRow, expectedClaim, claim.Lease)
	}
	activeNoopRequest := SetEnabledRequest{
		IdempotencyKey: "keep-active-" + targetID, Scope: scope, TargetID: targetID,
		Enabled: true, RequestedAt: current,
	}
	activeNoop, activeChanged, err := service.SetEnabled(t.Context(), activeNoopRequest)
	if err != nil || activeChanged || activeNoop != claim {
		t.Fatalf("SetEnabled(true) active no-op = (%+v, %v, %v), want exact claim %+v", activeNoop, activeChanged, err, claim)
	}

	disableRequest := SetEnabledRequest{
		IdempotencyKey: "disable-active-" + targetID, Scope: scope, TargetID: targetID,
		Enabled: false, RequestedAt: current.Add(time.Second),
	}
	current = disableRequest.RequestedAt
	disabled, changed, err := service.SetEnabled(t.Context(), disableRequest)
	if err != nil || !changed || disabled.Enabled || disabled.Lease.Active() {
		t.Fatalf("SetEnabled(false) with active lease = (%+v, %v, %v)", disabled, changed, err)
	}
	disabledRow, foundDisabled, err := loadPostgresTarget(db, scope.OwnerID, scope.WorkspaceID, targetID, false)
	if err != nil || !foundDisabled {
		t.Fatalf("load governed-disabled target = (%+v, %v, %v)", disabledRow, foundDisabled, err)
	}
	if err := verifyPostgresLease(disabledRow, scope.OwnerID, scope.WorkspaceID, workerID, claim.Lease.Generation, current.Add(time.Second)); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("stale worker fence after governed disable error = %v, want %v", err, ErrLeaseLost)
	}
	replayedActiveNoop, replayedActiveChanged, err := service.SetEnabled(t.Context(), activeNoopRequest)
	if err != nil || replayedActiveChanged || replayedActiveNoop != claim {
		t.Fatalf("SetEnabled(true) active no-op replay = (%+v, %v, %v), want exact claim %+v", replayedActiveNoop, replayedActiveChanged, err, claim)
	}
	enableRequest := SetEnabledRequest{
		IdempotencyKey: "reenable-" + targetID, Scope: scope, TargetID: targetID,
		Enabled: true, RequestedAt: current.Add(2 * time.Second),
	}
	current = enableRequest.RequestedAt
	if enabled, enabledChanged, enableErr := service.SetEnabled(t.Context(), enableRequest); enableErr != nil || !enabledChanged || !enabled.Enabled {
		t.Fatalf("SetEnabled(true) = (%+v, %v, %v)", enabled, enabledChanged, enableErr)
	}
	replayedDisable, replayedChange, err := service.SetEnabled(t.Context(), disableRequest)
	if err != nil || replayedChange || replayedDisable.Enabled || !replayedDisable.UpdatedAt.Equal(disableRequest.RequestedAt) {
		t.Fatalf("SetEnabled(false) exact replay = (%+v, %v, %v)", replayedDisable, replayedChange, err)
	}
	conflictingDisable := disableRequest
	conflictingDisable.Enabled = true
	if _, _, err := service.SetEnabled(t.Context(), conflictingDisable); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("SetEnabled same key with different digest error = %v, want %v", err, ErrIdempotencyConflict)
	}
	current = current.Add(time.Second)
	claims, err = service.ClaimDue(t.Context(), ClaimDueRequest{
		Scope: scope, WorkerID: workerID, Now: current, LeaseDuration: time.Minute, Limit: 1,
	})
	if err != nil || len(claims) != 1 {
		t.Fatalf("ClaimDue() after re-enable = (%+v, %v)", claims, err)
	}
	claim = claims[0]
	current = claim.Lease.ClaimedAt.Add(time.Second)
	collector.value.ObservedAt = current

	request := ProcessClaimRequest{
		IdempotencyKey: "complete-" + targetID, Scope: scope, TargetID: targetID,
		WorkerID: workerID, LeaseGeneration: claim.Lease.Generation, CompletedAt: current,
	}
	if found, ok, err := repository.FindCompletion(t.Context(), scope.OwnerID, scope.WorkspaceID, request.IdempotencyKey); err != nil || ok || found.Created {
		t.Fatalf("FindCompletion() before append = (%+v, %v, %v)", found, ok, err)
	}
	completion, err := service.ProcessClaim(t.Context(), request)
	if err != nil || !completion.Created || completion.Observation.Value != 3 || completion.Run.Status != RunCompleted {
		t.Fatalf("ProcessClaim() = (%+v, %v)", completion, err)
	}
	replay, err := service.ProcessClaim(t.Context(), request)
	if err != nil || replay.Created || replay.Run.ID != completion.Run.ID || replay.Observation.ID != completion.Observation.ID {
		t.Fatalf("ProcessClaim replay = (%+v, %v)", replay, err)
	}
	foundCompletion, found, err := repository.FindCompletion(t.Context(), scope.OwnerID, scope.WorkspaceID, request.IdempotencyKey)
	if err != nil || !found || foundCompletion.Created || foundCompletion.Composed ||
		foundCompletion.Run.ID != completion.Run.ID || foundCompletion.Observation.ID != completion.Observation.ID {
		t.Fatalf("FindCompletion() = (%+v, %v, %v)", foundCompletion, found, err)
	}
	if err := validateAuthority(foundCompletion.Authority); err != nil {
		t.Fatalf("FindCompletion authority = %+v: %v", foundCompletion.Authority, err)
	}
	if _, found, err := repository.FindCompletion(t.Context(), scope.OwnerID, "workspace-other", request.IdempotencyKey); err != nil || found {
		t.Fatalf("cross-workspace FindCompletion() = (%v, %v), want not found", found, err)
	}

	observations, err := repository.ListObservations(t.Context(), scope.OwnerID, scope.WorkspaceID, targetID, 1)
	if err != nil || len(observations) != 1 || observations[0].RecordDigest != completion.Observation.RecordDigest {
		t.Fatalf("ListObservations() = (%+v, %v)", observations, err)
	}
	runs, err := repository.ListRuns(t.Context(), scope.OwnerID, scope.WorkspaceID, targetID, 1)
	if err != nil || len(runs) != 1 || runs[0].RecordDigest != completion.Run.RecordDigest {
		t.Fatalf("ListRuns() = (%+v, %v)", runs, err)
	}
	stored, err := repository.GetTarget(t.Context(), scope.OwnerID, scope.WorkspaceID, targetID)
	if err != nil || stored.Lease.Active() || stored.Lease.Generation != claim.Lease.Generation || !stored.NextRunAt.After(current) {
		t.Fatalf("GetTarget() after completion = (%+v, %v)", stored, err)
	}
	replayedOriginal, replayCreated, err := service.RegisterTarget(t.Context(), register)
	if err != nil || replayCreated || !replayedOriginal.NextRunAt.Equal(register.FirstRunAt) || !replayedOriginal.UpdatedAt.Equal(register.RequestedAt) {
		t.Fatalf("RegisterTarget replay after later mutations = (%+v, %v, %v)", replayedOriginal, replayCreated, err)
	}

	observationUUID, _ := postgresRecordUUID(completion.Observation.ID, "obs")
	if err := db.Exec(`UPDATE public.outcome_observation_records SET numeric_value = 99 WHERE observation_id = ?`, observationUUID).Error; err == nil {
		t.Fatal("immutable observation ledger accepted an update")
	}
}

type postgresMonitorCollector struct {
	value CollectedObservation
}

func (c *postgresMonitorCollector) Collect(context.Context, MonitorTarget) (CollectedObservation, error) {
	return c.value, nil
}
