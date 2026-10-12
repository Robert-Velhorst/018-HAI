package operations

import (
	"context"
	"errors"
	"maps"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"automation-hub-backend/internal/models"

	"github.com/google/uuid"
)

func sourceConfigurationTestStart() SourceObservationStart {
	start := sourceObservationTestStart()
	start.RegistryManaged = true
	start.ConfigVersion = 1
	return start
}

type sourceConfigurationTestState struct {
	ledger       claimedEffectSnapshot
	origins      map[sourceOriginKey]SourceOrigin
	observations map[uuid.UUID]SourceObservation
	clocks       map[sourceObservationScope]int64
	heads        map[sourceHeadKey]SourceHead
	revisions    map[sourceRevisionKey]sourceHeadRevision
}

func sourceConfigurationTestSnapshot(repo *MemoryRepository) sourceConfigurationTestState {
	repo.mu.Lock()
	defer repo.mu.Unlock()
	ops := make(map[uuid.UUID]models.Operation, len(repo.ops))
	for id, op := range repo.ops {
		ops[id] = cloneOperation(op)
	}
	return sourceConfigurationTestState{
		ledger: claimedEffectSnapshot{
			ops: ops, claims: maps.Clone(repo.claims),
			events: append([]models.OperationEvent{}, repo.events...),
		},
		origins: maps.Clone(repo.sourceOrigins), observations: maps.Clone(repo.observations),
		clocks: maps.Clone(repo.observationClocks), heads: maps.Clone(repo.sourceHeads), revisions: maps.Clone(repo.sourceHeadRevisions),
	}
}

func sourceConfigurationTestAssertUnchanged(t *testing.T, repo *MemoryRepository, before sourceConfigurationTestState) {
	t.Helper()
	if !reflect.DeepEqual(sourceConfigurationTestSnapshot(repo), before) {
		t.Fatal("refused configuration/observation changed authority, tickets, heads, operations, claims, or audit history")
	}
}

func sourceConfigurationTestWrite(t *testing.T, svc *Service, start SourceObservationStart, enabled bool) {
	t.Helper()
	calls := 0
	err := svc.WithRegistrySourceConfiguration(context.Background(), start, enabled, func() error { calls++; return nil })
	if err != nil || calls != 1 {
		t.Fatalf("canonical registry configuration commit: %v / calls=%d", err, calls)
	}
}

func sourceConfigurationTestAssertOrigin(t *testing.T, repo *MemoryRepository, start SourceObservationStart, enabled bool, epoch int64) SourceOrigin {
	t.Helper()
	repo.mu.Lock()
	origin, exists := repo.sourceOrigins[sourceOriginKey{start.OwnerUserID, start.WorkspaceID, start.OriginID}]
	repo.mu.Unlock()
	if !exists || origin.OwnerUserID != start.OwnerUserID || origin.WorkspaceID != start.WorkspaceID || origin.OriginID != start.OriginID ||
		origin.ConfigDigest != start.ConfigDigest || origin.ConfigVersion != start.ConfigVersion || origin.ConfigEpoch != epoch || !origin.RegistryManaged || origin.Enabled != enabled {
		t.Fatalf("wrong canonical managed origin: %+v exists=%t want start=%+v enabled=%t epoch=%d", origin, exists, start, enabled, epoch)
	}
	return origin
}

func TestManagedSourceObservationCannotMintMissingUnmanagedDisabledOrStaleAuthority(t *testing.T) {
	for _, setup := range []string{"missing", "unmanaged", "disabled", "stale_digest", "stale_version", "unmanaged_opt_out_stale", "unmanaged_opt_out_disabled", "unmanaged_opt_out_version"} {
		for _, path := range []string{"service", "memory"} {
			t.Run(setup+"/"+path, func(t *testing.T) {
				repo := NewMemoryRepository()
				svc := sourceObservationTestService(repo)
				canonical := sourceConfigurationTestStart()
				attempt := canonical
				switch setup {
				case "unmanaged":
					standalone := sourceObservationTestStart()
					if _, err := repo.BeginSourceObservation(context.Background(), standalone); err != nil {
						t.Fatalf("healthy standalone observation fixture: %v", err)
					}
				case "disabled", "unmanaged_opt_out_disabled":
					sourceConfigurationTestWrite(t, svc, canonical, false)
				case "stale_digest", "unmanaged_opt_out_stale":
					sourceConfigurationTestWrite(t, svc, canonical, true)
					attempt.ConfigDigest = strings.Repeat("b", 64)
				case "stale_version", "unmanaged_opt_out_version":
					canonical.ConfigVersion = 2
					sourceConfigurationTestWrite(t, svc, canonical, true)
				}
				if strings.HasPrefix(setup, "unmanaged_opt_out_") {
					attempt.RegistryManaged = false
				}
				before := sourceConfigurationTestSnapshot(repo)
				var observed SourceObservation
				calls := 0
				var err error
				if path == "service" {
					err = svc.WithSourceObservation(context.Background(), attempt, func(ctx context.Context) error {
						calls++
						observed, _ = CurrentSourceObservation(ctx)
						return nil
					})
				} else {
					observed, err = repo.BeginSourceObservation(context.Background(), attempt)
				}
				if !errors.Is(err, ErrSourceHeadSuperseded) || observed != (SourceObservation{}) || calls != 0 {
					t.Fatalf("invalid managed start acquired read authority: %+v err=%v calls=%d", observed, err, calls)
				}
				sourceConfigurationTestAssertUnchanged(t, repo, before)
			})
		}
	}
}

func TestRegistrySourceConfigurationEpochIncludesManagedAndEnabledAuthority(t *testing.T) {
	repo := NewMemoryRepository()
	svc := sourceObservationTestService(repo)
	start := sourceConfigurationTestStart()
	for index, step := range []struct {
		digest  string
		enabled bool
		version int64
		epoch   int64
	}{
		{start.ConfigDigest, true, 1, 1},
		{start.ConfigDigest, true, 1, 1},
		{start.ConfigDigest, false, 2, 2},
		{start.ConfigDigest, true, 3, 3},
		{strings.Repeat("b", 64), true, 4, 4},
		{start.ConfigDigest, true, 5, 5},
		{start.ConfigDigest, true, 10, 6},
	} {
		start.ConfigDigest = step.digest
		start.ConfigVersion = step.version
		var before SourceOrigin
		if index == 1 {
			before = sourceConfigurationTestAssertOrigin(t, repo, start, true, 1)
		}
		sourceConfigurationTestWrite(t, svc, start, step.enabled)
		got := sourceConfigurationTestAssertOrigin(t, repo, start, step.enabled, step.epoch)
		if index == 1 && got != before {
			t.Fatal("identical canonical configuration advanced epoch or changed authority timestamp")
		}
	}
	state := sourceConfigurationTestSnapshot(repo)
	if len(state.observations) != 0 || len(state.clocks) != 0 || len(state.heads) != 0 || len(state.ledger.ops) != 0 || len(state.ledger.events) != 0 || len(state.ledger.claims) != 0 {
		t.Fatal("canonical configuration update fabricated observations, accepted heads, operations, audit, or claims")
	}
}

func TestRegistrySourceConfigurationDisableImmediatelyFencesClaimAndEffect(t *testing.T) {
	for _, phase := range []string{"claim", "effect"} {
		t.Run(phase, func(t *testing.T) {
			repo := NewMemoryRepository()
			svc := sourceObservationTestService(repo)
			start := sourceConfigurationTestStart()
			sourceConfigurationTestWrite(t, svc, start, true)
			op, observed := sourceObservationTestIngest(t, svc, start, sourceHeadTestInput("managed-A"))
			var fixture claimedEffectFixture
			if phase == "effect" {
				fixture = sourceHeadTestRunning(t, repo, svc, op)
				sourceHeadTestEffect(t, fixture, observed, nil)
			} else {
				fixture = claimedEffectFixture{repo: repo, service: svc, op: sourceHeadTestReady(t, svc, op)}
			}
			before := claimedEffectTakeSnapshot(repo)
			// Enabled is independently authoritative even when the digest is unchanged.
			start.ConfigVersion = 2
			sourceConfigurationTestWrite(t, svc, start, false)
			sourceConfigurationTestAssertOrigin(t, repo, start, false, 2)
			claimedEffectAssertUnchanged(t, fixture, before)
			sourceHeadTestAssertHead(t, repo, fixture.op, observed, "accepted")
			if phase == "effect" {
				sourceHeadTestEffect(t, fixture, observed, ErrSourceHeadSuperseded)
			} else {
				sourceHeadTestClaimRefused(t, repo, svc, fixture.op, ErrSourceHeadSuperseded)
			}
			start.ConfigVersion = 3
			sourceConfigurationTestWrite(t, svc, start, true)
			sourceConfigurationTestAssertOrigin(t, repo, start, true, 3)
			if phase == "effect" {
				sourceHeadTestEffect(t, fixture, observed, ErrSourceHeadSuperseded)
			} else {
				sourceHeadTestClaimRefused(t, repo, svc, fixture.op, ErrSourceHeadSuperseded)
			}
		})
	}
}

func TestManagedSourceConfigurationABARejectsOldObservationAndRequiresFreshHead(t *testing.T) {
	repo := NewMemoryRepository()
	svc := sourceObservationTestService(repo)
	startA := sourceConfigurationTestStart()
	sourceConfigurationTestWrite(t, svc, startA, true)
	in := sourceHeadTestInput("managed-A")
	op, first := sourceObservationTestIngest(t, svc, startA, in)
	fixture := sourceHeadTestRunning(t, repo, svc, op)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered := make(chan SourceObservation, 1)
	release, done := make(chan struct{}), make(chan struct{})
	var once sync.Once
	finishOld := func() { once.Do(func() { close(release) }) }
	var oldResult IngestResult
	var oldErr error
	t.Cleanup(func() { cancel(); finishOld(); claimedEffectWait(t, done, "managed A-B-A older reader cleanup") })
	go func() {
		defer close(done)
		oldErr = svc.WithSourceObservation(ctx, startA, func(oldCtx context.Context) error {
			observation, ok := CurrentSourceObservation(oldCtx)
			if !ok {
				return errors.New("old managed observation has no authority")
			}
			entered <- observation
			select {
			case <-release:
			case <-oldCtx.Done():
				return oldCtx.Err()
			}
			var err error
			oldResult, err = svc.IngestContext(oldCtx, in)
			return err
		})
	}()
	var older SourceObservation
	select {
	case older = <-entered:
	case <-done:
		t.Fatalf("old managed reader failed before barrier: %v", oldErr)
	case <-time.After(2 * time.Second):
		t.Fatal("old managed reader never entered read barrier")
	}
	startB := startA
	startB.ConfigDigest = strings.Repeat("b", 64)
	startB.ConfigVersion = 2
	sourceConfigurationTestWrite(t, svc, startB, true)
	sourceConfigurationTestAssertOrigin(t, repo, startB, true, 2)
	sourceHeadTestEffect(t, fixture, first, ErrSourceHeadSuperseded)
	beforeStaleStart := sourceConfigurationTestSnapshot(repo)
	if got, err := repo.BeginSourceObservation(context.Background(), startA); !errors.Is(err, ErrSourceHeadSuperseded) || got != (SourceObservation{}) {
		t.Fatalf("cached A reminted canonical B: %+v / %v", got, err)
	}
	sourceConfigurationTestAssertUnchanged(t, repo, beforeStaleStart)
	restoredA := startA
	restoredA.ConfigVersion = 3
	sourceConfigurationTestWrite(t, svc, restoredA, true)
	sourceConfigurationTestAssertOrigin(t, repo, restoredA, true, 3)
	sourceHeadTestEffect(t, fixture, first, ErrSourceHeadSuperseded)
	beforeOld := sourceConfigurationTestSnapshot(repo)
	if got, err := repo.BeginSourceObservation(context.Background(), startA); !errors.Is(err, ErrSourceHeadSuperseded) || got != (SourceObservation{}) {
		t.Fatalf("original version-1 A snapshot reminted restored version-3 A: %+v / %v", got, err)
	}
	sourceConfigurationTestAssertUnchanged(t, repo, beforeOld)
	finishOld()
	claimedEffectWait(t, done, "managed A-B-A stale completion")
	if !errors.Is(oldErr, ErrSourceHeadSuperseded) || !reflect.DeepEqual(oldResult, IngestResult{}) {
		t.Fatalf("original A ticket regained authority after configuration returned to A: %+v / %v", oldResult, oldErr)
	}
	sourceConfigurationTestAssertUnchanged(t, repo, beforeOld)
	result, fresh := sourceHeadTestObserve(t, svc, restoredA, in)
	if result.Created || result.Operation.ID != fixture.op.ID || fresh.ConfigEpoch != 3 || fresh.Generation <= older.Generation {
		t.Fatalf("fresh canonical A did not republish equal operation under the new epoch: %+v / %+v", result, fresh)
	}
	assertSourceObservationMetadata(t, result.Operation, first)
	sourceHeadTestEffect(t, fixture, fresh, nil)
}

func TestRegistrySourceConfigurationAdoptionRevokesUnmanagedAuthority(t *testing.T) {
	repo := NewMemoryRepository()
	svc := sourceObservationTestService(repo)
	standalone := sourceObservationTestStart()
	op, first := sourceObservationTestIngest(t, svc, standalone, sourceHeadTestInput("standalone-A"))
	fixture := sourceHeadTestRunning(t, repo, svc, op)
	sourceHeadTestEffect(t, fixture, first, nil)
	managed := standalone
	managed.RegistryManaged = true
	managed.ConfigVersion = 1
	sourceConfigurationTestWrite(t, svc, managed, true)
	sourceConfigurationTestAssertOrigin(t, repo, managed, true, 2)
	sourceHeadTestEffect(t, fixture, first, ErrSourceHeadSuperseded)
	before := sourceConfigurationTestSnapshot(repo)
	standalone.ConfigDigest = strings.Repeat("b", 64)
	if got, err := repo.BeginSourceObservation(context.Background(), standalone); !errors.Is(err, ErrSourceHeadSuperseded) || got != (SourceObservation{}) {
		t.Fatalf("unmanaged start downgraded adopted registry authority: %+v / %v", got, err)
	}
	sourceConfigurationTestAssertUnchanged(t, repo, before)
	result, fresh := sourceHeadTestObserve(t, svc, managed, sourceHeadTestInput("standalone-A"))
	if result.Created || fresh.ConfigEpoch != 2 {
		t.Fatalf("adopted managed authority did not require a new canonical observation: %+v / %+v", result, fresh)
	}
	assertSourceObservationMetadata(t, result.Operation, first)
	sourceHeadTestEffect(t, fixture, fresh, nil)
}

func TestRegistrySourceConfigurationCommitErrorLeavesAuthorityUnchanged(t *testing.T) {
	repo := NewMemoryRepository()
	svc := sourceObservationTestService(repo)
	start := sourceConfigurationTestStart()
	sourceConfigurationTestWrite(t, svc, start, true)
	before := sourceConfigurationTestSnapshot(repo)
	start.ConfigDigest = strings.Repeat("b", 64)
	start.ConfigVersion = 2
	want := errors.New("registry commit refused before its write")
	calls := 0
	err := svc.WithRegistrySourceConfiguration(context.Background(), start, false, func() error { calls++; return want })
	if !errors.Is(err, want) || calls != 1 {
		t.Fatalf("registry commit failure lost: %v / calls=%d", err, calls)
	}
	sourceConfigurationTestAssertUnchanged(t, repo, before)
}

func TestRegistrySourceConfigurationCancellationAfterCommitKeepsAuthorityConsistent(t *testing.T) {
	repo := NewMemoryRepository()
	svc := sourceObservationTestService(repo)
	start := sourceConfigurationTestStart()
	sourceConfigurationTestWrite(t, svc, start, true)
	start.ConfigDigest = strings.Repeat("b", 64)
	start.ConfigVersion = 2
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	committedDigest := ""
	calls := 0
	err := svc.WithRegistrySourceConfiguration(ctx, start, false, func() error {
		calls++
		committedDigest = start.ConfigDigest
		cancel()
		return nil
	})
	if !errors.Is(err, context.Canceled) || calls != 1 || committedDigest != start.ConfigDigest {
		t.Fatalf("post-commit cancellation contract changed: %v / calls=%d committed=%q", err, calls, committedDigest)
	}
	// A cancellation error after the closure committed is not a rollback receipt.
	sourceConfigurationTestAssertOrigin(t, repo, start, false, 2)
}

func TestRegistrySourceConfigurationRefusesCanceledMutationBeforeCommit(t *testing.T) {
	for _, path := range []string{"service", "memory"} {
		for _, mode := range []string{"nil", "canceled", "expired_without_timer_delivery", "held_mutex_deadline"} {
			t.Run(path+"/"+mode, func(t *testing.T) {
				repo := NewMemoryRepository()
				svc := sourceObservationTestService(repo)
				start := sourceConfigurationTestStart()
				sourceConfigurationTestWrite(t, svc, start, true)
				before := sourceConfigurationTestSnapshot(repo)
				start.ConfigDigest = strings.Repeat("b", 64)
				start.ConfigVersion = 2
				var ctx context.Context
				cancel := func() {}
				want := ErrInvalidSourceObservation
				switch mode {
				case "canceled":
					ctx, cancel = context.WithCancel(context.Background())
					cancel()
					want = context.Canceled
				case "expired_without_timer_delivery":
					ctx = delayedEffectDeadlineContext{Context: context.Background(), deadline: time.Now().Add(-time.Hour)}
					want = context.DeadlineExceeded
				case "held_mutex_deadline":
					ctx, cancel = context.WithTimeout(context.Background(), 20*time.Millisecond)
					want = context.DeadlineExceeded
				}
				defer cancel()
				var calls atomic.Int32
				call := svc.WithRegistrySourceConfiguration
				if path == "memory" {
					call = repo.WithRegistrySourceConfiguration
				}
				invoke := func() error {
					return call(ctx, start, false, func() error { calls.Add(1); return nil })
				}
				var err error
				if mode == "held_mutex_deadline" {
					err = safeEffectAuthorityCallWhileLocked(t, claimedEffectFixture{repo: repo}, invoke, nil)
				} else {
					err = invoke()
				}
				if !errors.Is(err, want) || calls.Load() != 0 {
					t.Fatalf("canceled mutation reached commit: %v want=%v calls=%d", err, want, calls.Load())
				}
				sourceConfigurationTestAssertUnchanged(t, repo, before)
			})
		}
	}
}

func TestRegistrySourceConfigurationRequiresCapabilityAndCommit(t *testing.T) {
	repo := NewMemoryRepository()
	start := sourceConfigurationTestStart()
	before := sourceConfigurationTestSnapshot(repo)
	legacy := NewService(struct{ Repository }{repo})
	var nilMemory *MemoryRepository
	calls := 0
	commit := func() error { calls++; return nil }
	for _, call := range []func() error{
		func() error { return legacy.WithRegistrySourceConfiguration(context.Background(), start, true, commit) },
		func() error {
			return nilMemory.WithRegistrySourceConfiguration(context.Background(), start, true, commit)
		},
		func() error {
			return NewService(repo).WithRegistrySourceConfiguration(context.Background(), start, true, nil)
		},
		func() error { return repo.WithRegistrySourceConfiguration(context.Background(), start, true, nil) },
		func() error { return PublishRegistrySourceConfiguration(nil, start, true) },
	} {
		if err := call(); !errors.Is(err, ErrSourceConfigurationUnsupported) || calls != 0 {
			t.Fatalf("unsupported configuration path committed anyway: %v / calls=%d", err, calls)
		}
	}
	sourceConfigurationTestAssertUnchanged(t, repo, before)
}

func TestRegistrySourceConfigurationRefusesInvalidAndStaleVersionsBeforeCommit(t *testing.T) {
	for _, path := range []string{"service", "memory"} {
		for _, mode := range []string{"unmanaged_canonical", "zero_version", "negative_version", "older_version", "same_version_digest_change", "same_version_enabled_change"} {
			t.Run(path+"/"+mode, func(t *testing.T) {
				repo := NewMemoryRepository()
				svc := sourceObservationTestService(repo)
				canonical := sourceConfigurationTestStart()
				canonical.ConfigVersion = 2
				sourceConfigurationTestWrite(t, svc, canonical, true)
				before := sourceConfigurationTestSnapshot(repo)
				attempt, enabled := canonical, true
				want := ErrInvalidSourceObservation
				switch mode {
				case "unmanaged_canonical":
					attempt.RegistryManaged = false
				case "zero_version":
					attempt.ConfigVersion = 0
				case "negative_version":
					attempt.ConfigVersion = -1
				case "older_version":
					attempt.ConfigVersion = 1
					want = ErrSourceHeadSuperseded
				case "same_version_digest_change":
					attempt.ConfigDigest = strings.Repeat("b", 64)
					want = ErrSourceHeadSuperseded
				case "same_version_enabled_change":
					enabled = false
					want = ErrSourceHeadSuperseded
				}
				call := svc.WithRegistrySourceConfiguration
				if path == "memory" {
					call = repo.WithRegistrySourceConfiguration
				}
				calls := 0
				err := call(context.Background(), attempt, enabled, func() error { calls++; return nil })
				if !errors.Is(err, want) || calls != 0 {
					t.Fatalf("invalid/stale canonical version reached commit: err=%v want=%v calls=%d", err, want, calls)
				}
				sourceConfigurationTestAssertUnchanged(t, repo, before)
				sourceConfigurationTestAssertOrigin(t, repo, canonical, true, 1)
			})
		}
	}
}

func TestManagedSourceObservationCannotDetachVersionAfterABA(t *testing.T) {
	for _, path := range []string{"service", "memory"} {
		for _, managed := range []bool{true, false} {
			for _, version := range []int64{0, -1} {
				name := "managed"
				if !managed {
					name = "unmanaged_opt_out"
				}
				if version < 0 {
					name += "_negative_version"
				} else {
					name += "_missing_version"
				}
				t.Run(path+"/"+name, func(t *testing.T) {
					repo := NewMemoryRepository()
					svc := sourceObservationTestService(repo)
					original := sourceConfigurationTestStart()
					sourceConfigurationTestWrite(t, svc, original, true)
					changed := original
					changed.ConfigVersion = 2
					changed.ConfigDigest = strings.Repeat("b", 64)
					sourceConfigurationTestWrite(t, svc, changed, true)
					restored := original
					restored.ConfigVersion = 3
					sourceConfigurationTestWrite(t, svc, restored, true)
					sourceConfigurationTestAssertOrigin(t, repo, restored, true, 3)
					// The canonical helper permits a repeated digest with a newer
					// version; the stored managed flag must enforce both fields.
					attempt := original
					attempt.RegistryManaged, attempt.ConfigVersion = managed, version
					before := sourceConfigurationTestSnapshot(repo)
					var observed SourceObservation
					calls := 0
					var err error
					if path == "service" {
						err = svc.WithSourceObservation(context.Background(), attempt, func(ctx context.Context) error {
							calls++
							observed, _ = CurrentSourceObservation(ctx)
							return nil
						})
					} else {
						observed, err = repo.BeginSourceObservation(context.Background(), attempt)
					}
					if !errors.Is(err, ErrSourceHeadSuperseded) || observed != (SourceObservation{}) || calls != 0 {
						t.Fatalf("detaching stale version bypassed managed A-B-A authority: %+v / %v calls=%d", observed, err, calls)
					}
					sourceConfigurationTestAssertUnchanged(t, repo, before)
				})
			}
		}
	}
}
