package operations

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"automation-hub-backend/internal/models"

	"github.com/google/uuid"
)

type safeEffectAuthorityBoundary func(context.Context, ExecutionClaim, models.Operation, func(context.Context) error) error

func safeEffectAuthorityBoundaries(fixture claimedEffectFixture) []struct {
	name string
	call safeEffectAuthorityBoundary
} {
	var nilGorm *GormRepository
	return []struct {
		name string
		call safeEffectAuthorityBoundary
	}{
		{"service", fixture.service.WithClaimedSafeEffect},
		{"memory", fixture.repo.WithClaimedSafeEffect},
		{"nil_gorm", nilGorm.WithClaimedSafeEffect},
		{"gorm_without_database", (&GormRepository{}).WithClaimedSafeEffect},
	}
}

// Rejection must finish while the real repository mutex is still held. Cleanup
// releases the mutex and joins the worker even when that assertion fails.
func safeEffectAuthorityCallWhileLocked(t *testing.T, fixture claimedEffectFixture, call func() error, cancel context.CancelFunc) error {
	t.Helper()
	started, done := make(chan struct{}), make(chan struct{})
	result := make(chan error, 1)
	fixture.repo.mu.Lock()
	locked := true
	defer func() {
		if cancel != nil {
			cancel()
		}
		if locked {
			fixture.repo.mu.Unlock()
		}
		select {
		case <-done:
		case <-time.After(6 * time.Second):
			t.Error("safe effect authority worker did not join after releasing the mutex")
		}
	}()
	go func() {
		defer close(done)
		close(started)
		result <- call()
	}()
	claimedEffectWait(t, started, "safe effect authority request")
	if cancel != nil {
		cancel()
	}
	claimedEffectWait(t, done, "authority rejection before mutex release")
	err := <-result
	fixture.repo.mu.Unlock()
	locked = false
	return err
}

func TestSafeEffectAuthorityRefusesInvalidContextBeforeStoreAccess(t *testing.T) {
	fixture := newClaimedEffectFixture(t, true)
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	expired, expire := context.WithDeadline(context.Background(), time.Now().Add(-time.Hour))
	defer expire()
	delayed := delayedEffectDeadlineContext{Context: context.Background(), deadline: time.Now().Add(-time.Hour)}
	if delayed.Err() != nil {
		t.Fatal("timer-lag fixture must have an expired absolute deadline but no delivered cancellation")
	}
	for _, boundary := range safeEffectAuthorityBoundaries(fixture) {
		for _, candidate := range []struct {
			name string
			ctx  context.Context
			want error
		}{
			{"nil", nil, ErrInvalidSourceObservation},
			{"canceled", canceled, context.Canceled},
			{"expired", expired, context.DeadlineExceeded},
			{"expired_without_timer_delivery", delayed, context.DeadlineExceeded},
		} {
			t.Run(boundary.name+"/"+candidate.name, func(t *testing.T) {
				before := claimedEffectTakeSnapshot(fixture.repo)
				var calls atomic.Int32
				err := safeEffectAuthorityCallWhileLocked(t, fixture, func() error {
					return boundary.call(candidate.ctx, fixture.claim, fixture.op, func(context.Context) error {
						calls.Add(1)
						return nil
					})
				}, nil)
				if !errors.Is(err, candidate.want) || calls.Load() != 0 {
					t.Fatalf("invalid authority entered boundary: err=%v want=%v calls=%d", err, candidate.want, calls.Load())
				}
				claimedEffectAssertUnchanged(t, fixture, before)
			})
		}
	}
}

func TestSafeEffectAuthorityBackgroundRemainsUsable(t *testing.T) {
	fixture := newClaimedEffectFixture(t, true)
	for _, boundary := range safeEffectAuthorityBoundaries(fixture)[:2] {
		t.Run(boundary.name, func(t *testing.T) {
			before := claimedEffectTakeSnapshot(fixture.repo)
			calls := 0
			var retained context.Context
			err := boundary.call(context.Background(), fixture.claim, fixture.op, func(ctx context.Context) error {
				calls++
				retained = context.WithoutCancel(ctx)
				scope, ok := CurrentSafeEffectScope(ctx)
				if !ok || scope != claimedEffectExpectedScope(fixture) {
					return fmt.Errorf("healthy boundary did not supply exact server scope: %+v / %t", scope, ok)
				}
				deadline, bounded := ctx.Deadline()
				if !bounded || !deadline.After(time.Now()) || time.Until(deadline) > safeEffectTimeout {
					return fmt.Errorf("healthy authority was not bounded: %v / %t", deadline, bounded)
				}
				if _, ok := CurrentSafeEffectScope(retained); !ok {
					return errors.New("live detached context lost server authority prematurely")
				}
				return nil
			})
			if err != nil || calls != 1 || retained == nil {
				t.Fatalf("healthy callback refused: %v / calls=%d", err, calls)
			}
			if _, ok := CurrentSafeEffectScope(retained); ok {
				t.Fatal("callback return left retained scope active")
			}
			claimedEffectAssertUnchanged(t, fixture, before)
		})
	}
	for _, boundary := range safeEffectAuthorityBoundaries(fixture)[2:] {
		calls := 0
		if err := boundary.call(context.Background(), fixture.claim, fixture.op, func(context.Context) error {
			calls++
			return nil
		}); !errors.Is(err, ErrSafeEffectUnsupported) || calls != 0 {
			t.Fatalf("%s healthy context bypassed absent database: %v / calls=%d", boundary.name, err, calls)
		}
	}
}

func TestSafeEffectAuthorityRejectsEndedDetachedScopes(t *testing.T) {
	for _, kind := range []string{"source_observation", "safe_effect"} {
		t.Run(kind, func(t *testing.T) {
			fixture := newClaimedEffectFixture(t, true)
			var retained context.Context
			capture := func(ctx context.Context) error {
				retained = context.WithoutCancel(ctx)
				return nil
			}
			var err error
			want := ErrInvalidSafeEffectAuthority
			if kind == "source_observation" {
				want = ErrInvalidSourceObservation
				start := SourceObservationStart{
					OwnerUserID: fixture.op.OwnerUserID, WorkspaceID: fixture.op.WorkspaceID,
					OriginID:     uuid.MustParse("00000000-0000-4000-8000-000000000711"),
					ConfigDigest: strings.Repeat("a", 64),
				}
				err = fixture.service.WithSourceObservation(context.Background(), start, func(ctx context.Context) error {
					if _, ok := CurrentSourceObservation(ctx); !ok {
						return errors.New("actual source observation callback lacked authority")
					}
					return capture(ctx)
				})
			} else {
				err = fixture.service.WithClaimedSafeEffect(context.Background(), fixture.claim, fixture.op, func(ctx context.Context) error {
					if _, ok := CurrentSafeEffectScope(ctx); !ok {
						return errors.New("actual safe effect callback lacked authority")
					}
					return capture(ctx)
				})
			}
			if err != nil || retained == nil || retained.Err() != nil {
				t.Fatalf("could not retain detached actual scope: %v / %v", err, retained)
			}
			if _, ok := CurrentSourceObservation(retained); ok {
				t.Fatal("ended observation still had authority")
			}
			if _, ok := CurrentSafeEffectScope(retained); ok {
				t.Fatal("ended effect still had authority")
			}
			for _, boundary := range safeEffectAuthorityBoundaries(fixture) {
				t.Run(boundary.name, func(t *testing.T) {
					before := claimedEffectTakeSnapshot(fixture.repo)
					var calls atomic.Int32
					err := safeEffectAuthorityCallWhileLocked(t, fixture, func() error {
						return boundary.call(retained, fixture.claim, fixture.op, func(context.Context) error {
							calls.Add(1)
							return nil
						})
					}, nil)
					if !errors.Is(err, want) || calls.Load() != 0 {
						t.Fatalf("ended detached scope acquired new authority: %v / calls=%d", err, calls.Load())
					}
					claimedEffectAssertUnchanged(t, fixture, before)
				})
			}
		})
	}
}

func TestSafeEffectAuthorityRejectsActiveDetachedReentry(t *testing.T) {
	for _, boundaryName := range []string{"service", "memory", "nil_gorm", "gorm_without_database"} {
		t.Run(boundaryName, func(t *testing.T) {
			fixture := newClaimedEffectFixture(t, true)
			before := claimedEffectTakeSnapshot(fixture.repo)
			var boundary safeEffectAuthorityBoundary
			for _, candidate := range safeEffectAuthorityBoundaries(fixture) {
				if candidate.name == boundaryName {
					boundary = candidate.call
				}
			}
			joined := make(chan struct{})
			result := make(chan error, 1)
			var calls atomic.Int32
			launched := false
			outerErr := fixture.service.WithClaimedSafeEffect(context.Background(), fixture.claim, fixture.op, func(ctx context.Context) error {
				detached := context.WithoutCancel(ctx)
				if _, ok := CurrentSafeEffectScope(detached); !ok {
					return errors.New("reentry control lacked active outer authority")
				}
				launched = true
				go func() {
					defer close(joined)
					result <- boundary(detached, fixture.claim, fixture.op, func(context.Context) error {
						calls.Add(1)
						return nil
					})
				}()
				select {
				case <-joined:
					return nil
				case <-time.After(2 * time.Second):
					return errors.New("nested effect waited on its own held memory mutex")
				}
			})
			if !launched {
				t.Fatalf("healthy outer boundary never launched nested admission: %v", outerErr)
			}
			// Join after the outer boundary releases its mutex, including regressions
			// that improperly waited instead of rejecting the nested admission.
			select {
			case <-joined:
			case <-time.After(6 * time.Second):
				t.Fatal("nested effect worker did not finish after outer boundary returned")
			}
			innerErr := <-result
			if outerErr != nil || !errors.Is(innerErr, ErrInvalidSafeEffectAuthority) || calls.Load() != 0 {
				t.Fatalf("nested authority accepted or blocked: outer=%v inner=%v calls=%d", outerErr, innerErr, calls.Load())
			}
			claimedEffectAssertUnchanged(t, fixture, before)
		})
	}
}

func TestSafeEffectAuthorityCallerAbsoluteDeadlineRevokesScopeWithoutTimer(t *testing.T) {
	fixture := newClaimedEffectFixture(t, true)
	before := claimedEffectTakeSnapshot(fixture.repo)
	calls := 0
	err := fixture.service.WithClaimedSafeEffect(context.Background(), fixture.claim, fixture.op, func(ctx context.Context) error {
		calls++
		if _, ok := CurrentSafeEffectScope(ctx); !ok {
			return errors.New("healthy server authority missing before caller deadline check")
		}
		caller := delayedEffectDeadlineContext{Context: ctx, deadline: time.Now().Add(-time.Hour)}
		if caller.Err() != nil {
			return errors.New("caller timer delivered unexpectedly in deterministic lag fixture")
		}
		if _, ok := CurrentSafeEffectScope(caller); ok {
			return errors.New("expired absolute caller deadline retained effect authority while Err was nil")
		}
		if _, ok := CurrentSafeEffectScope(ctx); !ok {
			return errors.New("negative caller check altered the still-live original server authority")
		}
		return nil
	})
	if err != nil || calls != 1 {
		t.Fatalf("absolute caller deadline check: %v / calls=%d", err, calls)
	}
	claimedEffectAssertUnchanged(t, fixture, before)
}

func TestSafeEffectAuthorityDetachedScopeCannotEscapeServerCancellation(t *testing.T) {
	fixture := newClaimedEffectFixture(t, true)
	before := claimedEffectTakeSnapshot(fixture.repo)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	var callbackErr error
	err := fixture.service.WithClaimedSafeEffect(ctx, fixture.claim, fixture.op, func(effectCtx context.Context) error {
		calls++
		if _, ok := CurrentSafeEffectScope(effectCtx); !ok {
			callbackErr = errors.New("callback started without server authority")
			return callbackErr
		}
		detached := context.WithoutCancel(effectCtx)
		cancel()
		for _, candidate := range []context.Context{detached, context.WithoutCancel(effectCtx)} {
			if candidate.Err() != nil {
				callbackErr = errors.New("negative control did not actually detach caller cancellation")
				return callbackErr
			}
			if _, ok := CurrentSafeEffectScope(candidate); ok {
				callbackErr = errors.New("detached caller regained canceled original authority inside synchronous callback")
				return callbackErr
			}
		}
		return nil
	})
	if !errors.Is(err, context.Canceled) || callbackErr != nil || calls != 1 {
		t.Fatalf("server cancellation not propagated or scope remained active: %v / callback=%v calls=%d", err, callbackErr, calls)
	}
	claimedEffectAssertUnchanged(t, fixture, before)
}

func TestSafeEffectAuthorityMemoryLockWaitHonorsCallerCancellation(t *testing.T) {
	for _, boundary := range []string{"service", "memory"} {
		t.Run(boundary, func(t *testing.T) {
			fixture := newClaimedEffectFixture(t, true)
			before := claimedEffectTakeSnapshot(fixture.repo)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var calls atomic.Int32
			call := fixture.service.WithClaimedSafeEffect
			if boundary == "memory" {
				call = fixture.repo.WithClaimedSafeEffect
			}
			err := safeEffectAuthorityCallWhileLocked(t, fixture, func() error {
				return call(ctx, fixture.claim, fixture.op, func(context.Context) error {
					calls.Add(1)
					return nil
				})
			}, cancel)
			if !errors.Is(err, context.Canceled) || calls.Load() != 0 {
				t.Fatalf("held mutex ignored caller cancellation: %v / calls=%d", err, calls.Load())
			}
			claimedEffectAssertUnchanged(t, fixture, before)
		})
	}
}
