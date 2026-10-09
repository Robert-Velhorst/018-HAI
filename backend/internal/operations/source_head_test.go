package operations

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"automation-hub-backend/internal/models"

	"github.com/google/uuid"
)

func sourceHeadTestInput(revision string) NewOperationInput {
	in := sourceObservationTestInput()
	in.DedupeKey = "source-head-" + revision
	in.SourceRevisionHash = "opaque revision:" + revision
	return in
}

func sourceHeadTestObserve(t *testing.T, svc *Service, start SourceObservationStart, in NewOperationInput) (IngestResult, SourceObservation) {
	t.Helper()
	var result IngestResult
	var observation SourceObservation
	err := svc.WithSourceObservation(context.Background(), start, func(ctx context.Context) error {
		var ok bool
		observation, ok = CurrentSourceObservation(ctx)
		if !ok {
			return errors.New("source head fixture has no active observation")
		}
		var err error
		result, err = svc.IngestContext(ctx, in)
		return err
	})
	if err != nil || result.Operation.ID == uuid.Nil {
		t.Fatalf("observe source head: result=%+v observation=%+v err=%v", result, observation, err)
	}
	return result, observation
}

func sourceHeadTestReady(t *testing.T, svc *Service, op models.Operation) models.Operation {
	t.Helper()
	op.RiskLevel, op.AutonomyLevel, op.OwnerType = string(RiskLow), string(AutonomyAuto), string(OwnerHAI)
	op.CurrentDecision, op.RequiresApproval = string(DecisionRunSafeLocalWorker), false
	classified, err := svc.Transition(op, StatusClassified, string(OwnerHAI), "", "classify observed local work")
	if err != nil || classified == nil {
		t.Fatalf("classify head fixture: %+v / %v", classified, err)
	}
	ready, err := svc.Transition(*classified, StatusReady, string(OwnerHAI), "", "ready observed local work")
	if err != nil || ready == nil {
		t.Fatalf("ready head fixture: %+v / %v", ready, err)
	}
	return *ready
}

func sourceHeadTestRunning(t *testing.T, repo *MemoryRepository, svc *Service, op models.Operation) claimedEffectFixture {
	t.Helper()
	ready := sourceHeadTestReady(t, svc, op)
	worker := uuid.MustParse("00000000-0000-4000-8000-000000000721")
	claimed, err := svc.ClaimOperation(context.Background(), ready.OwnerUserID, ready.WorkspaceID, ready.ID, worker, time.Minute)
	if err != nil || claimed == nil {
		t.Fatalf("claim head fixture: %+v / %v", claimed, err)
	}
	op = claimed.Operation
	op.RuntimeID, op.VerificationStatus = "hai-local-safe-worker", string(VerificationPending)
	op.ResultSummary = "running intent; effect outcome not yet known"
	op.WorldModelStateJSON = `{"outcomeUncertain":true,"beforeEffect":false}`
	running, err := svc.TransitionClaimed(context.Background(), claimed.Claim, op, StatusRunning, string(OwnerHAI), worker.String(), "persist observed running intent")
	if err != nil || running == nil {
		t.Fatalf("running head fixture: %+v / %v", running, err)
	}
	return claimedEffectFixture{repo: repo, service: svc, op: *running, claim: claimed.Claim}
}

func sourceHeadTestClaimRefused(t *testing.T, repo *MemoryRepository, svc *Service, op models.Operation, want error) {
	t.Helper()
	worker := uuid.MustParse("00000000-0000-4000-8000-000000000723")
	for _, boundary := range []struct {
		name string
		one  func(context.Context, string, string, uuid.UUID, uuid.UUID, time.Duration) (*ClaimedOperation, error)
		next func(context.Context, string, string, uuid.UUID, time.Duration) (*ClaimedOperation, error)
	}{
		{"service", svc.ClaimOperation, svc.ClaimNext},
		{"memory", repo.ClaimOperation, repo.ClaimNext},
	} {
		before := claimedEffectTakeSnapshot(repo)
		claim, err := boundary.one(context.Background(), op.OwnerUserID, op.WorkspaceID, op.ID, worker, time.Minute)
		if claim != nil || !errors.Is(err, want) {
			t.Fatalf("%s minted a claim without accepted source authority: %+v / %v want=%v", boundary.name, claim, err, want)
		}
		claimedEffectAssertUnchanged(t, claimedEffectFixture{repo: repo}, before)
		claim, err = boundary.next(context.Background(), op.OwnerUserID, op.WorkspaceID, worker, time.Minute)
		if err != nil || claim != nil {
			t.Fatalf("%s selected an unsafe head candidate: %+v / %v", boundary.name, claim, err)
		}
		claimedEffectAssertUnchanged(t, claimedEffectFixture{repo: repo}, before)
	}
}

func sourceHeadTestAssertHead(t *testing.T, repo *MemoryRepository, op models.Operation, observed SourceObservation, state string) {
	t.Helper()
	repo.mu.Lock()
	head, exists := repo.sourceHeads[sourceHeadKey{op.OwnerUserID, op.WorkspaceID, op.SourceIdentityHash}]
	repo.mu.Unlock()
	if !exists || head.OwnerUserID != op.OwnerUserID || head.WorkspaceID != op.WorkspaceID ||
		head.SourceIdentityHash != op.SourceIdentityHash || head.OriginID != observed.OriginID ||
		head.OperationID != op.ID || head.RevisionHash != op.SourceRevisionHash ||
		head.ObservationID != observed.ID || head.ObservationGeneration != observed.Generation ||
		head.ConfigEpoch != observed.ConfigEpoch || head.State != state {
		t.Fatalf("wrong persisted source head: %+v exists=%t operation=%s observation=%+v state=%s", head, exists, op.ID, observed, state)
	}
}

func sourceHeadTestAssertPublicationOnly(t *testing.T, fixture claimedEffectFixture, before claimedEffectSnapshot) {
	t.Helper()
	after := claimedEffectTakeSnapshot(fixture.repo)
	if !reflect.DeepEqual(after.ops, before.ops) || !reflect.DeepEqual(after.claims, before.claims) ||
		len(after.events) != len(before.events)+1 || !reflect.DeepEqual(after.events[:len(before.events)], before.events) {
		t.Fatal("head publication rewrote operations, claims, or preceding audit history")
	}
	event := after.events[len(before.events)]
	if event.OperationID != fixture.op.ID || event.EventType != "source_head_published" || event.ActorType != string(OwnerHAI) {
		t.Fatalf("head publication did not append its own audit event: %+v", event)
	}
}

func sourceHeadTestEffect(t *testing.T, fixture claimedEffectFixture, observation SourceObservation, want error) {
	t.Helper()
	for _, boundary := range []struct {
		name string
		call func(context.Context, ExecutionClaim, models.Operation, func(context.Context) error) error
	}{
		{"service", fixture.service.WithClaimedSafeEffect},
		{"memory", fixture.repo.WithClaimedSafeEffect},
	} {
		before := claimedEffectTakeSnapshot(fixture.repo)
		calls := 0
		err := boundary.call(context.Background(), fixture.claim, fixture.op, func(ctx context.Context) error {
			calls++
			scope, ok := CurrentSafeEffectScope(ctx)
			if !ok || scope.OperationID != fixture.op.ID || scope.Version != fixture.op.Version ||
				scope.OwnerUserID != fixture.op.OwnerUserID || scope.WorkspaceID != fixture.op.WorkspaceID ||
				scope.ClaimOwner != fixture.claim.Owner || scope.ClaimGeneration != fixture.claim.Generation ||
				scope.SourceIdentityHash != fixture.op.SourceIdentityHash || scope.SourceRevisionHash != fixture.op.SourceRevisionHash ||
				scope.SourceHeadGeneration != observation.Generation || scope.SourceConfigEpoch != observation.ConfigEpoch ||
				scope.SourceHeadGeneration <= 0 || scope.SourceConfigEpoch <= 0 {
				return fmt.Errorf("wrong head-backed server scope: %+v / active=%t want observation=%+v", scope, ok, observation)
			}
			return nil
		})
		if want == nil {
			if err != nil || calls != 1 {
				t.Fatalf("%s healthy head callback: err=%v calls=%d", boundary.name, err, calls)
			}
		} else if !errors.Is(err, want) || calls != 0 {
			t.Fatalf("%s unsafe head entered effect: err=%v want=%v calls=%d", boundary.name, err, want, calls)
		}
		claimedEffectAssertUnchanged(t, fixture, before)
	}
}

func TestSourceHeadFirstObservedRevisionAllowsBoundedClaimedEffect(t *testing.T) {
	repo := NewMemoryRepository()
	svc := sourceObservationTestService(repo)
	op, observation := sourceObservationTestIngest(t, svc, sourceObservationTestStart(), sourceHeadTestInput("A"))
	if observation.ConfigEpoch != 1 {
		t.Fatalf("first configuration epoch=%d want=1", observation.ConfigEpoch)
	}
	sourceHeadTestAssertHead(t, repo, op, observation, "accepted")
	fixture := sourceHeadTestRunning(t, repo, svc, op)
	sourceHeadTestEffect(t, fixture, observation, nil)
}

func TestSourceHeadAcceptedNewRevisionFencesOlderRunningOperation(t *testing.T) {
	repo := NewMemoryRepository()
	svc := sourceObservationTestService(repo)
	start := sourceObservationTestStart()
	opA, observedA := sourceObservationTestIngest(t, svc, start, sourceHeadTestInput("A"))
	fixture := sourceHeadTestRunning(t, repo, svc, opA)
	opB, observedB := sourceObservationTestIngest(t, svc, start, sourceHeadTestInput("B"))
	if opA.ID == opB.ID || opA.SourceIdentityHash != opB.SourceIdentityHash || observedB.Generation <= observedA.Generation {
		t.Fatal("new revision fixture did not preserve identity while advancing operation and observation")
	}
	sourceHeadTestAssertHead(t, repo, opB, observedB, "accepted")
	sourceHeadTestEffect(t, fixture, observedA, ErrSourceHeadSuperseded)
}

func TestSourceHeadNewerObservationFinishesFirstAndOlderResultIsZero(t *testing.T) {
	repo := NewMemoryRepository()
	svc := sourceObservationTestService(repo)
	start := sourceObservationTestStart()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered := make(chan SourceObservation, 1)
	release, done := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	finishOld := func() { releaseOnce.Do(func() { close(release) }) }
	var oldResult IngestResult
	var oldErr error
	t.Cleanup(func() {
		cancel()
		finishOld()
		claimedEffectWait(t, done, "older source observation cleanup")
	})
	go func() {
		defer close(done)
		oldErr = svc.WithSourceObservation(ctx, start, func(observedCtx context.Context) error {
			observation, ok := CurrentSourceObservation(observedCtx)
			if !ok {
				return errors.New("older reader started without authority")
			}
			entered <- observation
			select {
			case <-release:
			case <-observedCtx.Done():
				return observedCtx.Err()
			}
			var err error
			oldResult, err = svc.IngestContext(observedCtx, sourceHeadTestInput("A"))
			return err
		})
	}()
	var observedA SourceObservation
	select {
	case observedA = <-entered:
	case <-done:
		t.Fatalf("older observation failed before read barrier: %v", oldErr)
	case <-time.After(2 * time.Second):
		t.Fatal("older observation never entered its read barrier")
	}
	opB, observedB := sourceObservationTestIngest(t, svc, start, sourceHeadTestInput("B"))
	if observedB.Generation <= observedA.Generation {
		t.Fatalf("observations allocated out of order: old=%+v new=%+v", observedA, observedB)
	}
	beforeOld := claimedEffectTakeSnapshot(repo)
	finishOld()
	claimedEffectWait(t, done, "superseded older completion")
	if !errors.Is(oldErr, ErrSourceHeadSuperseded) || !reflect.DeepEqual(oldResult, IngestResult{}) {
		t.Fatalf("older completion exposed intake success: result=%+v err=%v", oldResult, oldErr)
	}
	claimedEffectAssertUnchanged(t, claimedEffectFixture{repo: repo}, beforeOld)
	repo.mu.Lock()
	retained, exists := repo.observations[observedA.ID]
	repo.mu.Unlock()
	if !exists || retained != observedA {
		t.Fatalf("prewrite rejection lost the durable older observation: %+v exists=%t want=%+v", retained, exists, observedA)
	}
	sourceHeadTestAssertHead(t, repo, opB, observedB, "accepted")
	fixture := sourceHeadTestRunning(t, repo, svc, opB)
	sourceHeadTestEffect(t, fixture, observedB, nil)
}

func TestSourceHeadDuplicateAdvancesGenerationWithoutRewritingProvenance(t *testing.T) {
	repo := NewMemoryRepository()
	svc := sourceObservationTestService(repo)
	start, in := sourceObservationTestStart(), sourceHeadTestInput("A")
	op, first := sourceObservationTestIngest(t, svc, start, in)
	fixture := sourceHeadTestRunning(t, repo, svc, op)
	before := claimedEffectTakeSnapshot(repo)
	result, second := sourceHeadTestObserve(t, svc, start, in)
	if result.Created || result.Operation.ID != fixture.op.ID || result.Operation.Version != fixture.op.Version || second.Generation <= first.Generation {
		t.Fatalf("same revision replayed an operation instead of advancing head: %+v / %+v", result, second)
	}
	assertSourceObservationMetadata(t, result.Operation, first)
	sourceHeadTestAssertPublicationOnly(t, fixture, before)
	sourceHeadTestAssertHead(t, repo, result.Operation, second, "accepted")
	sourceHeadTestEffect(t, fixture, second, nil)
}

func TestSourceHeadOlderDuplicateEvidenceCannotOverwriteNewerObservation(t *testing.T) {
	repo := NewMemoryRepository()
	svc := sourceObservationTestService(repo)
	start, in := sourceObservationTestStart(), sourceHeadTestInput("A")
	in.EvidenceJSON = `{"rawMetadata":{"label":"seed"}}`
	seed, first := sourceObservationTestIngest(t, svc, start, in)
	oldInput, newInput := in, in
	oldInput.EvidenceJSON = `{"rawMetadata":{"label":"older"}}`
	newInput.EvidenceJSON = `{"rawMetadata":{"label":"newer"}}`
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered := make(chan SourceObservation, 1)
	release, done := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	finishOld := func() { releaseOnce.Do(func() { close(release) }) }
	var oldResult IngestResult
	var oldErr error
	t.Cleanup(func() {
		cancel()
		finishOld()
		claimedEffectWait(t, done, "older duplicate evidence cleanup")
	})
	go func() {
		defer close(done)
		oldErr = svc.WithSourceObservation(ctx, start, func(observedCtx context.Context) error {
			observed, ok := CurrentSourceObservation(observedCtx)
			if !ok {
				return errors.New("older evidence reader has no actual observation")
			}
			entered <- observed
			select {
			case <-release:
			case <-observedCtx.Done():
				return observedCtx.Err()
			}
			var err error
			oldResult, err = svc.IngestContext(observedCtx, oldInput)
			return err
		})
	}()
	var older SourceObservation
	select {
	case older = <-entered:
	case <-done:
		t.Fatalf("older evidence reader failed before barrier: %v", oldErr)
	case <-time.After(2 * time.Second):
		t.Fatal("older evidence reader never entered its read barrier")
	}
	newResult, newer := sourceHeadTestObserve(t, svc, start, newInput)
	if newResult.Created || newResult.Operation.ID != seed.ID || newResult.Operation.SourceIdentityHash != seed.SourceIdentityHash ||
		newResult.Operation.SourceRevisionHash != seed.SourceRevisionHash || newResult.Operation.DedupeKey != seed.DedupeKey ||
		newResult.Operation.EvidenceJSON != newInput.EvidenceJSON || newResult.Operation.Version != seed.Version+1 ||
		newer.Generation <= older.Generation || older.Generation <= first.Generation {
		t.Fatalf("newer same-semantic evidence did not refresh the existing operation: %+v older=%+v newer=%+v", newResult, older, newer)
	}
	assertSourceObservationMetadata(t, newResult.Operation, first)
	sourceHeadTestAssertHead(t, repo, newResult.Operation, newer, "accepted")
	beforeOld := claimedEffectTakeSnapshot(repo)
	created, refreshed := 0, 0
	for _, event := range beforeOld.events {
		if event.OperationID != seed.ID {
			continue
		}
		switch event.EventType {
		case "created":
			created++
		case "source_evidence_refreshed":
			refreshed++
		}
	}
	if len(beforeOld.ops) != 1 || created != 1 || refreshed != 1 {
		t.Fatalf("healthy newer duplicate fixture did not retain one creation and one refresh: operations=%d created=%d refreshed=%d", len(beforeOld.ops), created, refreshed)
	}
	finishOld()
	claimedEffectWait(t, done, "superseded older evidence completion")
	if !errors.Is(oldErr, ErrSourceHeadSuperseded) || !reflect.DeepEqual(oldResult, IngestResult{}) {
		t.Fatalf("older duplicate evidence exposed success: %+v / %v", oldResult, oldErr)
	}
	stored, err := svc.Get(seed.OwnerUserID, seed.WorkspaceID, seed.ID)
	if err != nil || stored == nil || !reflect.DeepEqual(*stored, newResult.Operation) {
		t.Fatalf("older duplicate overwrote newer evidence or version: %+v / %v want=%+v", stored, err, newResult.Operation)
	}
	claimedEffectAssertUnchanged(t, claimedEffectFixture{repo: repo}, beforeOld)
	sourceHeadTestAssertHead(t, repo, *stored, newer, "accepted")
}

func TestSourceHeadConfigurationEpochsAdvanceABAAndRevokeBeforeRead(t *testing.T) {
	repo := NewMemoryRepository()
	svc := sourceObservationTestService(repo)
	start, in := sourceObservationTestStart(), sourceHeadTestInput("A")
	op, first := sourceObservationTestIngest(t, svc, start, in)
	fixture := sourceHeadTestRunning(t, repo, svc, op)
	if first.ConfigEpoch != 1 {
		t.Fatalf("first configuration epoch=%d want=1", first.ConfigEpoch)
	}
	for index, digest := range []string{strings.Repeat("b", 64), start.ConfigDigest} {
		start.ConfigDigest = digest
		var observed SourceObservation
		var result IngestResult
		err := svc.WithSourceObservation(context.Background(), start, func(ctx context.Context) error {
			var ok bool
			observed, ok = CurrentSourceObservation(ctx)
			if !ok || observed.ConfigEpoch != int64(index+2) {
				return fmt.Errorf("configuration epoch reused: %+v want=%d", observed, index+2)
			}
			// Ticket allocation must revoke the preceding head before the reader
			// has returned any item, not merely when intake completes.
			sourceHeadTestEffect(t, fixture, observed, ErrSourceHeadSuperseded)
			var err error
			result, err = svc.IngestContext(ctx, in)
			return err
		})
		if err != nil || result.Created || result.Operation.ID != fixture.op.ID {
			t.Fatalf("configuration change could not republish equal deduped operation: %+v / %v", result, err)
		}
		assertSourceObservationMetadata(t, result.Operation, first)
		sourceHeadTestAssertHead(t, repo, result.Operation, observed, "accepted")
		sourceHeadTestEffect(t, fixture, observed, nil)
	}
}

func TestSourceHeadReturningEarlierRevisionNeverReactivatesOldAttempt(t *testing.T) {
	repo := NewMemoryRepository()
	svc := sourceObservationTestService(repo)
	start := sourceObservationTestStart()
	inA := sourceHeadTestInput("A")
	opA, first := sourceObservationTestIngest(t, svc, start, inA)
	fixture := sourceHeadTestRunning(t, repo, svc, opA)
	_, observedB := sourceObservationTestIngest(t, svc, start, sourceHeadTestInput("B"))
	before := claimedEffectTakeSnapshot(repo)
	var result IngestResult
	var returned SourceObservation
	err := svc.WithSourceObservation(context.Background(), start, func(ctx context.Context) error {
		var ok bool
		returned, ok = CurrentSourceObservation(ctx)
		if !ok {
			return errors.New("returning reader has no active source observation")
		}
		var err error
		result, err = svc.IngestContext(ctx, inA)
		return err
	})
	if !errors.Is(err, ErrSourceHeadReconciliation) || !reflect.DeepEqual(result, IngestResult{}) || returned.Generation <= observedB.Generation {
		t.Fatalf("returning earlier revision replayed an operation: result=%+v observation=%+v err=%v", result, returned, err)
	}
	stored, err := svc.Get(fixture.op.OwnerUserID, fixture.op.WorkspaceID, fixture.op.ID)
	if err != nil || stored == nil {
		t.Fatalf("returning revision lost original operation: %+v / %v", stored, err)
	}
	assertSourceObservationMetadata(t, *stored, first)
	sourceHeadTestAssertPublicationOnly(t, fixture, before)
	sourceHeadTestAssertHead(t, repo, *stored, returned, "reconciliation_required")
	sourceHeadTestEffect(t, fixture, returned, ErrSourceHeadReconciliation)
}

func TestSourceHeadSameIdentityFromDifferentOriginRequiresReconciliation(t *testing.T) {
	repo := NewMemoryRepository()
	svc := sourceObservationTestService(repo)
	start := sourceObservationTestStart()
	opA, first := sourceObservationTestIngest(t, svc, start, sourceHeadTestInput("A"))
	other := start
	other.OriginID = uuid.MustParse("00000000-0000-4000-8000-000000000722")
	in := sourceHeadTestInput("B")
	in.AccountFeedID = &other.OriginID
	var result IngestResult
	err := svc.WithSourceObservation(context.Background(), other, func(ctx context.Context) error {
		var err error
		result, err = svc.IngestContext(ctx, in)
		return err
	})
	if !errors.Is(err, ErrSourceHeadReconciliation) || !reflect.DeepEqual(result, IngestResult{}) {
		t.Fatalf("other origin acquired the existing semantic identity: result=%+v err=%v", result, err)
	}
	stored, err := svc.Get(opA.OwnerUserID, opA.WorkspaceID, opA.ID)
	if err != nil || stored == nil || !reflect.DeepEqual(*stored, opA) {
		t.Fatalf("cross-origin observation rewrote the first operation: %+v / %v", stored, err)
	}
	assertSourceObservationMetadata(t, *stored, first)
	sourceHeadTestAssertHead(t, repo, opA, first, "accepted")
}

func TestSourceHeadSameObservationIsIdempotentWithoutAuthorityAdvance(t *testing.T) {
	repo := NewMemoryRepository()
	svc := sourceObservationTestService(repo)
	start, in := sourceObservationTestStart(), sourceHeadTestInput("A")
	var first, repeated IngestResult
	var observed SourceObservation
	var repeatErr error
	err := svc.WithSourceObservation(context.Background(), start, func(ctx context.Context) error {
		var ok bool
		observed, ok = CurrentSourceObservation(ctx)
		if !ok {
			return errors.New("repeat fixture has no actual observation")
		}
		var err error
		first, err = svc.IngestContext(ctx, in)
		if err != nil {
			return err
		}
		before := claimedEffectTakeSnapshot(repo)
		repo.mu.Lock()
		headBefore := repo.sourceHeads[sourceHeadKey{first.Operation.OwnerUserID, first.Operation.WorkspaceID, first.Operation.SourceIdentityHash}]
		repo.mu.Unlock()
		repeated, repeatErr = svc.IngestContext(ctx, in)
		fixture := claimedEffectFixture{repo: repo}
		claimedEffectAssertUnchanged(t, fixture, before)
		repo.mu.Lock()
		headAfter := repo.sourceHeads[sourceHeadKey{first.Operation.OwnerUserID, first.Operation.WorkspaceID, first.Operation.SourceIdentityHash}]
		repo.mu.Unlock()
		if headAfter != headBefore {
			return fmt.Errorf("same observation advanced authority: before=%+v after=%+v", headBefore, headAfter)
		}
		return nil
	})
	if err != nil || !first.Created || repeatErr != nil || repeated.Created || !reflect.DeepEqual(repeated.Operation, first.Operation) {
		t.Fatalf("identical same-generation publication was not an inert duplicate: first=%+v repeated=%+v err=%v repeatErr=%v", first, repeated, err, repeatErr)
	}
	sourceHeadTestAssertHead(t, repo, first.Operation, observed, "accepted")
}

func TestSourceHeadObservedOperationWithoutPublishedHeadCannotExecute(t *testing.T) {
	for _, phase := range []string{"final_boundary", "claim_preflight"} {
		t.Run(phase, func(t *testing.T) {
			repo := NewMemoryRepository()
			svc := sourceObservationTestService(repo)
			op, observed := sourceObservationTestIngest(t, svc, sourceObservationTestStart(), sourceHeadTestInput("A"))
			sourceHeadTestAssertHead(t, repo, op, observed, "accepted")
			var fixture claimedEffectFixture
			if phase == "final_boundary" {
				fixture = sourceHeadTestRunning(t, repo, svc, op)
				sourceHeadTestEffect(t, fixture, observed, nil)
			} else {
				ready := sourceHeadTestReady(t, svc, op)
				worker := uuid.MustParse("00000000-0000-4000-8000-000000000725")
				claimed, err := svc.ClaimOperation(context.Background(), ready.OwnerUserID, ready.WorkspaceID, ready.ID, worker, time.Minute)
				if err != nil || claimed == nil {
					t.Fatalf("head-backed preflight control did not mint valid claim: %+v / %v", claimed, err)
				}
				fixture = claimedEffectFixture{repo: repo, service: svc, op: claimed.Operation, claim: claimed.Claim}
			}
			// Simulate a missing durable head only after a real accepted observation
			// and valid server claim; never fabricate a live claim for unsequenced input.
			repo.mu.Lock()
			delete(repo.sourceHeads, sourceHeadKey{op.OwnerUserID, op.WorkspaceID, op.SourceIdentityHash})
			repo.mu.Unlock()
			if phase == "final_boundary" {
				sourceHeadTestEffect(t, fixture, observed, ErrSourceHeadReconciliation)
			} else {
				sourceHeadTestClaimRefused(t, repo, svc, fixture.op, ErrSourceHeadReconciliation)
			}
		})
	}
}

func TestSourceHeadUnsequencedIdentifiedIntakeCannotExecute(t *testing.T) {
	for _, path := range []string{"legacy_intake", "explicit_background_intake"} {
		t.Run(path, func(t *testing.T) {
			repo := NewMemoryRepository()
			svc := sourceObservationTestService(repo)
			in := sourceHeadTestInput("A")
			var result IngestResult
			var err error
			if path == "legacy_intake" {
				result, err = svc.Ingest(in)
			} else {
				result, err = svc.IngestContext(context.Background(), in)
			}
			if err != nil || !result.Created || result.Operation.SourceIdentityHash == "" ||
				result.Operation.SourceObservationID != nil || result.Operation.SourceObservationGeneration != 0 {
				t.Fatalf("unsequenced compatibility fixture invalid: %+v / %v", result, err)
			}
			ready := sourceHeadTestReady(t, svc, result.Operation)
			sourceHeadTestClaimRefused(t, repo, svc, ready, ErrSourceHeadReconciliation)
		})
	}
}

func TestSourceHeadClaimPreflightSkipsOlderReadyRevision(t *testing.T) {
	for _, boundary := range []string{"service", "memory"} {
		t.Run(boundary, func(t *testing.T) {
			repo := NewMemoryRepository()
			svc := sourceObservationTestService(repo)
			start := sourceObservationTestStart()
			opA, _ := sourceObservationTestIngest(t, svc, start, sourceHeadTestInput("A"))
			readyA := sourceHeadTestReady(t, svc, opA)
			svc.now = func() time.Time { return sourceIdentityContractTime.Add(time.Second) }
			opB, observedB := sourceObservationTestIngest(t, svc, start, sourceHeadTestInput("B"))
			claimOne, claimNext := svc.ClaimOperation, svc.ClaimNext
			if boundary == "memory" {
				claimOne, claimNext = repo.ClaimOperation, repo.ClaimNext
			}
			beforeSelection := claimedEffectTakeSnapshot(repo)
			worker := uuid.MustParse("00000000-0000-4000-8000-000000000724")
			stale, err := claimOne(context.Background(), readyA.OwnerUserID, readyA.WorkspaceID, readyA.ID, worker, time.Minute)
			if stale != nil || !errors.Is(err, ErrSourceHeadSuperseded) {
				t.Fatalf("explicit stale candidate received a claim: %+v / %v", stale, err)
			}
			claimedEffectAssertUnchanged(t, claimedEffectFixture{repo: repo}, beforeSelection)
			// New observe-only work can legitimately be claimed for classification.
			// Selecting it must not revive A or prevent B's later classification.
			selected, err := claimNext(context.Background(), readyA.OwnerUserID, readyA.WorkspaceID, worker, time.Minute)
			if selected != nil {
				if releaseErr := svc.ReleaseClaim(context.Background(), selected.Claim); releaseErr != nil {
					t.Fatalf("release classification preflight claim: %v", releaseErr)
				}
			}
			if err != nil || (selected != nil && (selected.Operation.ID == readyA.ID || selected.Operation.ID != opB.ID)) {
				t.Fatalf("ClaimNext selected stale rather than current work: %+v / %v", selected, err)
			}
			sourceHeadTestAssertHead(t, repo, opB, observedB, "accepted")
			afterSelection := claimedEffectTakeSnapshot(repo)
			if !reflect.DeepEqual(beforeSelection.ops, afterSelection.ops) || !reflect.DeepEqual(beforeSelection.events, afterSelection.events) {
				t.Fatal("classification preflight changed operation or audit history")
			}
			if _, staleClaim := afterSelection.claims[readyA.ID]; staleClaim {
				t.Fatal("older ready revision received a replay claim during classification selection")
			}
			readyB := sourceHeadTestReady(t, svc, opB)
			before := claimedEffectTakeSnapshot(repo)
			claimed, err := claimNext(context.Background(), readyB.OwnerUserID, readyB.WorkspaceID, worker, time.Minute)
			if err != nil || claimed == nil || claimed.Operation.ID != readyB.ID || claimed.Claim.OperationID != readyB.ID ||
				claimed.Claim.Owner != worker || claimed.Claim.Generation != before.claims[readyB.ID].generation+1 {
				t.Fatalf("ClaimNext did not select the accepted newer revision: %+v / %v", claimed, err)
			}
			after := claimedEffectTakeSnapshot(repo)
			if !reflect.DeepEqual(before.ops, after.ops) || !reflect.DeepEqual(before.events, after.events) || len(after.claims) != 1 {
				t.Fatal("healthy claim selection rewrote ledger or also claimed a stale candidate")
			}
			if _, staleClaim := after.claims[readyA.ID]; staleClaim {
				t.Fatal("older ready revision received a replay claim")
			}
			if releaseErr := svc.ReleaseClaim(context.Background(), claimed.Claim); releaseErr != nil {
				t.Fatalf("release accepted ready revision claim: %v", releaseErr)
			}
		})
	}
}

func TestSourceHeadReturningReadyRevisionCannotAcquireReplayClaim(t *testing.T) {
	repo := NewMemoryRepository()
	svc := sourceObservationTestService(repo)
	start, in := sourceObservationTestStart(), sourceHeadTestInput("A")
	opA, _ := sourceObservationTestIngest(t, svc, start, in)
	readyA := sourceHeadTestReady(t, svc, opA)
	_, _ = sourceObservationTestIngest(t, svc, start, sourceHeadTestInput("B"))
	before := claimedEffectTakeSnapshot(repo)
	var result IngestResult
	var returned SourceObservation
	err := svc.WithSourceObservation(context.Background(), start, func(ctx context.Context) error {
		var ok bool
		returned, ok = CurrentSourceObservation(ctx)
		if !ok {
			return errors.New("returning ready revision has no observation authority")
		}
		var err error
		result, err = svc.IngestContext(ctx, in)
		return err
	})
	if !errors.Is(err, ErrSourceHeadReconciliation) || !reflect.DeepEqual(result, IngestResult{}) {
		t.Fatalf("returning ready revision exposed replay success: %+v / %v", result, err)
	}
	sourceHeadTestAssertPublicationOnly(t, claimedEffectFixture{repo: repo, op: readyA}, before)
	sourceHeadTestAssertHead(t, repo, readyA, returned, "reconciliation_required")
	sourceHeadTestClaimRefused(t, repo, svc, readyA, ErrSourceHeadReconciliation)
}

func TestSourceHeadClearingExpectedIdentityCannotBypassFinalFence(t *testing.T) {
	repo := NewMemoryRepository()
	svc := sourceObservationTestService(repo)
	start := sourceObservationTestStart()
	opA, observedA := sourceObservationTestIngest(t, svc, start, sourceHeadTestInput("A"))
	fixture := sourceHeadTestRunning(t, repo, svc, opA)
	_, _ = sourceObservationTestIngest(t, svc, start, sourceHeadTestInput("B"))
	fixture.op.SourceProvider, fixture.op.SourceAccount, fixture.op.SourceExternalID, fixture.op.SourceIdentityHash = "", "", "", ""
	sourceHeadTestEffect(t, fixture, observedA, ErrSourceIdentityImmutable)
}

func TestSourceHeadObservedSourceLessIntakeIsRefused(t *testing.T) {
	repo := NewMemoryRepository()
	svc := sourceObservationTestService(repo)
	in := sourceHeadTestInput("A")
	in.SourceProvider, in.SourceAccount, in.SourceExternalID = "", "", ""
	before := claimedEffectTakeSnapshot(repo)
	var result IngestResult
	err := svc.WithSourceObservation(context.Background(), sourceObservationTestStart(), func(ctx context.Context) error {
		var err error
		result, err = svc.IngestContext(ctx, in)
		return err
	})
	if !errors.Is(err, ErrInvalidSourceObservation) || !reflect.DeepEqual(result, IngestResult{}) {
		t.Fatalf("source-less input borrowed observation authority: %+v / %v", result, err)
	}
	claimedEffectAssertUnchanged(t, claimedEffectFixture{repo: repo}, before)
	repo.mu.Lock()
	headCount := len(repo.sourceHeads)
	repo.mu.Unlock()
	if headCount != 0 {
		t.Fatal("source-less intake published a semantic source head")
	}
}

func TestSourceHeadUnidentifiedLegacySafeWorkStillRunsWithoutHead(t *testing.T) {
	repo := NewMemoryRepository()
	svc := sourceObservationTestService(repo)
	in := sourceHeadTestInput("legacy")
	in.SourceProvider, in.SourceAccount, in.SourceExternalID = "", "", ""
	result, err := svc.Ingest(in)
	if err != nil || !result.Created || result.Operation.SourceIdentityHash != "" {
		t.Fatalf("unidentified legacy fixture invalid: %+v / %v", result, err)
	}
	fixture := sourceHeadTestRunning(t, repo, svc, result.Operation)
	before := claimedEffectTakeSnapshot(repo)
	calls := 0
	err = svc.WithClaimedSafeEffect(context.Background(), fixture.claim, fixture.op, func(ctx context.Context) error {
		calls++
		scope, ok := CurrentSafeEffectScope(ctx)
		if !ok || scope.OperationID != fixture.op.ID || scope.SourceIdentityHash != "" ||
			scope.SourceHeadGeneration != 0 || scope.SourceConfigEpoch != 0 || scope.SourceObservationID != "" {
			return fmt.Errorf("legacy scope falsely claimed source-head authority: %+v / %t", scope, ok)
		}
		return nil
	})
	if err != nil || calls != 1 {
		t.Fatalf("head fencing regressed legacy safe work: %v / calls=%d", err, calls)
	}
	claimedEffectAssertUnchanged(t, fixture, before)
}
