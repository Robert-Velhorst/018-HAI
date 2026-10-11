package proactivity

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestCaptureEvaluationSnapshotAllowsOnlyBoundedMonotonicClockAdvance(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	owner := "owner-monotonic-clock"
	now := time.Date(2026, time.August, 14, 10, 0, 0, 0, time.UTC)
	service := newService(NewMemoryRepository(), func() time.Time { return now })
	if _, _, err := service.RecordPolicy(ctx, owner, "policy", DefaultPreferences(owner)); err != nil {
		t.Fatal(err)
	}
	if snapshot, err := service.CaptureEvaluationSnapshot(ctx, owner, now.Add(snapshotMonotonicClockAllowance)); err != nil ||
		!snapshot.CapturedAt.Equal(now.Add(snapshotMonotonicClockAllowance)) {
		t.Fatalf("bounded monotonic capture = (%+v, %v)", snapshot, err)
	}
	if _, err := service.CaptureEvaluationSnapshot(ctx, owner, now.Add(snapshotMonotonicClockAllowance+time.Microsecond)); !errors.Is(err, ErrSnapshotInvalid) {
		t.Fatalf("materially future capture error = %v, want %v", err, ErrSnapshotInvalid)
	}
}

func TestEvaluateStoredSnapshotKeepsEmptyAdditionalSignalsCompatible(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	owner := "owner-empty-additions"
	clock := time.Date(2026, time.August, 5, 7, 0, 0, 0, time.UTC)
	service := newService(NewMemoryRepository(), func() time.Time { return clock })
	if _, _, err := service.RecordPolicy(ctx, owner, "policy", DefaultPreferences(owner)); err != nil {
		t.Fatal(err)
	}
	snapshot, err := service.CaptureEvaluationSnapshot(ctx, owner, clock)
	if err != nil {
		t.Fatal(err)
	}
	request := EvaluateStoredSnapshotRequest{IdempotencyKey: "empty-additions", Now: clock, Snapshot: snapshot}
	batch, created, err := service.EvaluateStoredSnapshot(ctx, owner, request)
	if err != nil || !created || len(batch.Result.Decisions) != 0 || batch.AdditionalSignalsDigest != "" {
		t.Fatalf("empty-addition exact evaluation = created %v, batch %#v, err %v", created, batch, err)
	}
	replayed, created, err := service.EvaluateStoredSnapshot(ctx, owner, request)
	if err != nil || created || !reflect.DeepEqual(replayed, batch) {
		t.Fatalf("empty-addition replay = created %v, batch %#v, err %v", created, replayed, err)
	}
}

func TestEvaluationSnapshotCanonicalizesStorageTimesToUTCMicroseconds(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	owner := "owner-storage-time"
	clock := time.Date(2026, time.August, 5, 7, 30, 0, 987654321, time.FixedZone("capture-zone", 2*60*60))
	service := newService(NewMemoryRepository(), func() time.Time { return clock })

	policyRecord, _, err := service.RecordPolicy(ctx, owner, "policy", DefaultPreferences(owner))
	if err != nil {
		t.Fatal(err)
	}
	signal := testSignal(owner, "storage-signal", "storage-loop", clock)
	signalRecords, _, err := service.RecordSignals(ctx, owner, "signals", []OpenLoopSignal{signal})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := service.CaptureEvaluationSnapshot(ctx, owner, clock)
	if err != nil {
		t.Fatal(err)
	}
	assertSnapshotStorageTime(t, "policy record", policyRecord.RecordedAt)
	assertSnapshotStorageTime(t, "signal record", signalRecords[0].RecordedAt)
	assertSnapshotStorageTime(t, "captured at", snapshot.CapturedAt)
	assertSnapshotStorageTime(t, "policy cursor", snapshot.Policy.RecordedAt)
	if snapshot.Signals.Cursor == nil {
		t.Fatal("signal cursor is missing")
	}
	assertSnapshotStorageTime(t, "signal cursor", snapshot.Signals.Cursor.RecordedAt)
	if !snapshot.CapturedAt.Equal(snapshotTime(clock)) {
		t.Fatalf("captured at = %s, want %s", snapshot.CapturedAt, snapshotTime(clock))
	}
	if err := VerifyEvaluationSnapshot(owner, snapshot); err != nil {
		t.Fatalf("VerifyEvaluationSnapshot() error = %v", err)
	}

	clock = clock.Add(time.Second + 444*time.Nanosecond)
	batch, created, err := service.EvaluateStoredSnapshot(ctx, owner, EvaluateStoredSnapshotRequest{
		IdempotencyKey: "evaluation", Now: clock, Snapshot: snapshot,
	})
	if err != nil || !created || len(batch.Result.Decisions) != 1 {
		t.Fatalf("EvaluateStoredSnapshot() = created %v, batch %#v, err %v", created, batch, err)
	}
	assertSnapshotStorageTime(t, "evaluation decided at", batch.Result.DecidedAt)
	assertSnapshotStorageTime(t, "decision recorded at", batch.RecordedAt)

	decisionSnapshot, err := service.CaptureEvaluationSnapshot(ctx, owner, clock)
	if err != nil {
		t.Fatal(err)
	}
	if decisionSnapshot.Decisions.Cursor == nil {
		t.Fatal("decision cursor is missing")
	}
	assertSnapshotStorageTime(t, "decision cursor", decisionSnapshot.Decisions.Cursor.RecordedAt)
	encoded, err := json.Marshal(decisionSnapshot)
	if err != nil {
		t.Fatal(err)
	}
	var persisted EvaluationSnapshot
	if err := json.Unmarshal(encoded, &persisted); err != nil {
		t.Fatal(err)
	}
	if persisted.CapturedAt != decisionSnapshot.CapturedAt {
		t.Fatalf("JSON captured at = %s, want exact %s", persisted.CapturedAt, decisionSnapshot.CapturedAt)
	}
	if err := VerifyEvaluationSnapshot(owner, persisted); err != nil {
		t.Fatalf("persisted canonical snapshot verification error = %v", err)
	}
}

func TestEvaluateStoredSnapshotExcludesLaterStateAndIncludesEphemeralSignal(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	owner := "owner-snapshot"
	clock := time.Date(2026, time.August, 5, 8, 0, 0, 0, time.UTC)
	repository := NewMemoryRepository()
	service := newService(repository, func() time.Time { return clock })

	if _, created, err := service.RecordPolicy(ctx, owner, "policy-baseline", DefaultPreferences(owner)); err != nil || !created {
		t.Fatalf("RecordPolicy() = created %v, err %v", created, err)
	}
	clock = clock.Add(time.Minute)
	baseline := testSignal(owner, "signal-baseline", "loop-baseline", clock)
	if _, created, err := service.RecordSignals(ctx, owner, "signals-baseline", []OpenLoopSignal{baseline}); err != nil || !created {
		t.Fatalf("RecordSignals(baseline) = created %v, err %v", created, err)
	}
	snapshot, err := service.CaptureEvaluationSnapshot(ctx, owner, clock)
	if err != nil {
		t.Fatalf("CaptureEvaluationSnapshot() error = %v", err)
	}
	if err := VerifyEvaluationSnapshot(owner, snapshot); err != nil {
		t.Fatalf("VerifyEvaluationSnapshot() error = %v", err)
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	var persisted EvaluationSnapshot
	if err := json.Unmarshal(encoded, &persisted); err != nil {
		t.Fatal(err)
	}
	if err := VerifyEvaluationSnapshot(owner, persisted); err != nil {
		t.Fatalf("persisted JSON snapshot verification error = %v", err)
	}
	if snapshot.Policy.IdempotencyKey != "policy-baseline" || snapshot.Signals.Count != 1 || snapshot.Signals.Cursor == nil ||
		snapshot.Decisions.Count != 0 || snapshot.Decisions.Cursor != nil || snapshot.Feedback.Count != 0 || snapshot.Feedback.Cursor != nil {
		t.Fatalf("captured snapshot = %#v", snapshot)
	}
	sameTimestampLater := testSignal(owner, "signal-same-timestamp-later", "loop-same-timestamp-later", clock)
	if _, created, err := service.RecordSignals(ctx, owner, "zz-signals-same-timestamp-later", []OpenLoopSignal{sameTimestampLater}); err != nil || !created {
		t.Fatalf("RecordSignals(same timestamp later) = created %v, err %v", created, err)
	}

	clock = clock.Add(time.Minute)
	laterPolicy := DefaultPreferences(owner)
	laterPolicy.MinimumConfidence = 0.99
	if _, _, err := service.RecordPolicy(ctx, owner, "policy-later", laterPolicy); err != nil {
		t.Fatal(err)
	}
	laterStored := testSignal(owner, "signal-later-stored", "loop-later-stored", clock)
	if _, _, err := service.RecordSignals(ctx, owner, "signals-later", []OpenLoopSignal{laterStored}); err != nil {
		t.Fatal(err)
	}
	ephemeral := testSignal(owner, "signal-monitor-addition", "loop-monitor-addition", clock)
	request := EvaluateStoredSnapshotRequest{
		IdempotencyKey:    "snapshot-evaluation-a",
		Now:               clock,
		Snapshot:          snapshot,
		AdditionalSignals: []OpenLoopSignal{ephemeral},
	}
	batch, created, err := service.EvaluateStoredSnapshot(ctx, owner, request)
	if err != nil || !created {
		t.Fatalf("EvaluateStoredSnapshot() = created %v, err %v", created, err)
	}
	if batch.SnapshotInputDigest != snapshot.InputDigest || !digestPattern.MatchString(batch.AdditionalSignalsDigest) {
		t.Fatalf("decision batch is not input-bound: %#v", batch)
	}
	assertDecisionSignalIDs(t, batch.Result.Decisions, "signal-baseline", "signal-monitor-addition")
	for _, decision := range batch.Result.Decisions {
		assertNoAuthority(t, decision)
	}

	storedSignals, err := service.Signals(ctx, owner, 10)
	if err != nil || len(storedSignals) != 3 {
		t.Fatalf("stored signals = (%#v, %v), want three persisted signals", storedSignals, err)
	}
	for _, record := range storedSignals {
		if record.Signal.ID == ephemeral.ID {
			t.Fatal("ephemeral snapshot signal was persisted")
		}
	}

	request.IdempotencyKey = "snapshot-evaluation-b"
	replayedFromInputs, created, err := service.EvaluateStoredSnapshot(ctx, owner, request)
	if err != nil || !created {
		t.Fatalf("second exact evaluation = created %v, err %v", created, err)
	}
	if !reflect.DeepEqual(replayedFromInputs.Result, batch.Result) {
		t.Fatalf("later stored state changed exact result:\nfirst=%#v\nsecond=%#v", batch.Result, replayedFromInputs.Result)
	}
	assertDecisionSignalIDs(t, replayedFromInputs.Result.Decisions, "signal-baseline", "signal-monitor-addition")
}

func TestEvaluateStoredSnapshotBindsAdditionalSignalsIntoIdempotency(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	owner := "owner-addition-digest"
	clock := time.Date(2026, time.August, 5, 9, 0, 0, 0, time.UTC)
	service := newService(NewMemoryRepository(), func() time.Time { return clock })
	if _, _, err := service.RecordPolicy(ctx, owner, "policy", DefaultPreferences(owner)); err != nil {
		t.Fatal(err)
	}
	snapshot, err := service.CaptureEvaluationSnapshot(ctx, owner, clock)
	if err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(time.Minute)
	first := testSignal(owner, "monitor-a", "loop-a", clock)
	request := EvaluateStoredSnapshotRequest{IdempotencyKey: "exact-key", Now: clock, Snapshot: snapshot, AdditionalSignals: []OpenLoopSignal{first}}
	if _, created, err := service.EvaluateStoredSnapshot(ctx, owner, request); err != nil || !created {
		t.Fatalf("first exact evaluation = created %v, err %v", created, err)
	}
	changed := testSignal(owner, "monitor-b", "loop-b", clock)
	request.AdditionalSignals = []OpenLoopSignal{changed}
	if _, _, err := service.EvaluateStoredSnapshot(ctx, owner, request); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("changed additional signal error = %v, want %v", err, ErrIdempotencyConflict)
	}
}

func TestEvaluateStoredSnapshotRejectsCrossOwnerAndDuplicateAdditionalSignals(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	owner := "owner-addition-scope"
	clock := time.Date(2026, time.August, 5, 10, 0, 0, 0, time.UTC)
	service := newService(NewMemoryRepository(), func() time.Time { return clock })
	if _, _, err := service.RecordPolicy(ctx, owner, "policy", DefaultPreferences(owner)); err != nil {
		t.Fatal(err)
	}
	baseline := testSignal(owner, "baseline", "loop", clock)
	if _, _, err := service.RecordSignals(ctx, owner, "signals", []OpenLoopSignal{baseline}); err != nil {
		t.Fatal(err)
	}
	snapshot, err := service.CaptureEvaluationSnapshot(ctx, owner, clock)
	if err != nil {
		t.Fatal(err)
	}

	crossOwner := testSignal("owner-other", "monitor", "monitor-loop", clock)
	_, _, err = service.EvaluateStoredSnapshot(ctx, owner, EvaluateStoredSnapshotRequest{
		IdempotencyKey: "cross-owner", Now: clock, Snapshot: snapshot, AdditionalSignals: []OpenLoopSignal{crossOwner},
	})
	if err == nil || !strings.Contains(err.Error(), "owner") {
		t.Fatalf("cross-owner additional signal error = %v", err)
	}
	duplicate := testSignal(owner, baseline.ID, "different-loop", clock)
	_, _, err = service.EvaluateStoredSnapshot(ctx, owner, EvaluateStoredSnapshotRequest{
		IdempotencyKey: "duplicate", Now: clock, Snapshot: snapshot, AdditionalSignals: []OpenLoopSignal{duplicate},
	})
	if err == nil || !strings.Contains(err.Error(), "duplicates") {
		t.Fatalf("duplicate additional signal error = %v", err)
	}
}

func TestEvaluationSnapshotValidationAndExactPolicyFailClosed(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	owner := "owner-snapshot-validation"
	clock := time.Date(2026, time.August, 5, 11, 0, 0, 0, time.UTC)
	service := newService(NewMemoryRepository(), func() time.Time { return clock })
	if _, _, err := service.RecordPolicy(ctx, owner, "policy-exact", DefaultPreferences(owner)); err != nil {
		t.Fatal(err)
	}
	snapshot, err := service.CaptureEvaluationSnapshot(ctx, owner, clock)
	if err != nil {
		t.Fatal(err)
	}

	tampered := cloneEvaluationSnapshot(snapshot)
	tampered.InputDigest = strings.Repeat("f", 64)
	if err := VerifyEvaluationSnapshot(owner, tampered); !errors.Is(err, ErrSnapshotInvalid) {
		t.Fatalf("tampered input digest error = %v", err)
	}
	if err := VerifyEvaluationSnapshot("owner-other", snapshot); err == nil {
		t.Fatal("cross-owner snapshot verified")
	}

	wrongPolicy := cloneEvaluationSnapshot(snapshot)
	wrongPolicy.Policy.PayloadDigest = strings.Repeat("e", 64)
	wrongPolicy.InputDigest, err = evaluationSnapshotDigest(owner, wrongPolicy)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = service.EvaluateStoredSnapshot(ctx, owner, EvaluateStoredSnapshotRequest{
		IdempotencyKey: "wrong-policy", Now: clock, Snapshot: wrongPolicy,
	})
	if !errors.Is(err, ErrSnapshotUnavailable) {
		t.Fatalf("wrong exact policy error = %v, want %v", err, ErrSnapshotUnavailable)
	}

	badCursor := cloneEvaluationSnapshot(snapshot)
	badCursor.Signals = SnapshotWatermark{Cursor: &SnapshotRecordCursor{
		RecordedAt: clock.Add(time.Second), IdempotencyKey: "future", Ordinal: 0, PayloadDigest: strings.Repeat("a", 64),
	}, Count: 1, WindowDigest: strings.Repeat("b", 64)}
	badCursor.InputDigest, err = evaluationSnapshotDigest(owner, badCursor)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyEvaluationSnapshot(owner, badCursor); !errors.Is(err, ErrSnapshotInvalid) {
		t.Fatalf("future cursor error = %v", err)
	}
}

func TestEvaluationSnapshotFeedbackCursorExcludesLaterFeedback(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	owner := "owner-feedback-snapshot"
	clock := time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC)
	repository := NewMemoryRepository()
	service := newService(repository, func() time.Time { return clock })
	policy := DefaultPreferences(owner)
	policy.Cooldown = 0
	if _, _, err := service.RecordPolicy(ctx, owner, "policy", policy); err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(time.Minute)
	signal := testSignal(owner, "feedback-signal", "feedback-loop", clock)
	if _, _, err := service.RecordSignals(ctx, owner, "signals", []OpenLoopSignal{signal}); err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(time.Minute)
	batch, _, err := service.EvaluateStored(ctx, owner, EvaluateStoredRequest{IdempotencyKey: "decision", Now: clock})
	if err != nil || len(batch.Result.Decisions) != 1 {
		t.Fatalf("EvaluateStored() = %#v, %v", batch, err)
	}
	decision := batch.Result.Decisions[0]
	clock = clock.Add(time.Minute)
	suppressed, _, err := service.RecordFeedback(ctx, owner, FeedbackRequest{
		IdempotencyKey: "feedback-suppress", SignalID: decision.SignalID, OpenLoopKey: decision.OpenLoopKey,
		SignalDigest: decision.SignalDigest, Action: FeedbackSuppress, Reason: "Pause this advisory open loop.",
	})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := service.CaptureEvaluationSnapshot(ctx, owner, clock)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Decisions.Count != 1 || snapshot.Feedback.Count != 1 || snapshot.Feedback.Cursor == nil || snapshot.Feedback.Cursor.RecordDigest != suppressed.RecordDigest {
		t.Fatalf("decision/feedback snapshot = %#v", snapshot)
	}

	clock = clock.Add(time.Minute)
	if _, _, err := service.RecordFeedback(ctx, owner, FeedbackRequest{
		IdempotencyKey: "feedback-resume", SignalID: decision.SignalID, OpenLoopKey: decision.OpenLoopKey,
		SignalDigest: decision.SignalDigest, Action: FeedbackResume, Reason: "Resume attention.",
	}); err != nil {
		t.Fatal(err)
	}
	state, err := repository.resolveEvaluationSnapshotState(ctx, owner, snapshot)
	if err != nil {
		t.Fatalf("resolveEvaluationSnapshotState() error = %v", err)
	}
	if len(state.Feedback) != 1 || state.Feedback[0].Record.Action != FeedbackSuppress {
		t.Fatalf("resolved feedback = %#v, want only captured suppress", state.Feedback)
	}
}

func TestEvaluationSnapshotRejectsAuthorityBearingDecisionMaterial(t *testing.T) {
	t.Parallel()
	owner := "owner-authority-snapshot"
	now := time.Date(2026, time.August, 5, 13, 0, 0, 0, time.UTC)
	policy := PolicyRecord{ContractVersion: ContractVersion, OwnerIdentity: owner, Policy: DefaultPreferences(owner), RecordedAt: now}
	policyDigest, err := advisoryDigest(idempotencyKindPolicy, owner, policy.Policy)
	if err != nil {
		t.Fatal(err)
	}
	unsafe := validPostgresDecisionBatch(owner, now)
	unsafe.Result.Decisions[0].AuthorityGranted = true
	state := evaluationSnapshotState{
		Policy: snapshotPolicyMaterial{Reference: PolicySnapshotReference{IdempotencyKey: "policy", PayloadDigest: policyDigest, RecordedAt: now}, Record: policy},
		Decisions: []snapshotDecisionMaterial{{
			Cursor: SnapshotRecordCursor{RecordedAt: now, IdempotencyKey: "unsafe", Ordinal: 0, PayloadDigest: strings.Repeat("a", 64)},
			Record: DecisionRecord{ContractVersion: ContractVersion, OwnerIdentity: owner, Decision: unsafe.Result.Decisions[0], RecordedAt: now},
		}},
	}
	if err := validateEvaluationSnapshotState(owner, now, state); !errors.Is(err, ErrSnapshotUnavailable) {
		t.Fatalf("authority-bearing snapshot state error = %v", err)
	}
}

func assertDecisionSignalIDs(t *testing.T, decisions []Decision, expected ...string) {
	t.Helper()
	actual := make(map[string]struct{}, len(decisions))
	for _, decision := range decisions {
		actual[decision.SignalID] = struct{}{}
	}
	if len(actual) != len(expected) {
		t.Fatalf("decision signal ids = %#v, want %v", actual, expected)
	}
	for _, id := range expected {
		if _, found := actual[id]; !found {
			t.Fatalf("decision signal ids = %#v, missing %s", actual, id)
		}
	}
}

func assertSnapshotStorageTime(t *testing.T, name string, value time.Time) {
	t.Helper()
	if value != snapshotTime(value) {
		t.Fatalf("%s = %s, want canonical UTC microseconds %s", name, value, snapshotTime(value))
	}
}
