package operations

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"automation-hub-backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type claimedEffectFixture struct {
	repo    *MemoryRepository
	service *Service
	op      models.Operation
	claim   ExecutionClaim
}

func newClaimedEffectFixture(t *testing.T, identified bool) claimedEffectFixture {
	t.Helper()
	repo := NewMemoryRepository()
	service := NewService(repo)
	service.now = func() time.Time { return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC) }
	in := NewOperationInput{
		OwnerUserID: "effect-owner", WorkspaceID: "effect-workspace",
		Title: "Local safe effect", OperationType: "review_source_item", SourceType: "local_json_file",
		DedupeKey: "claimed-effect-record", SourceRevisionHash: "opaque revision:v1",
	}
	if identified {
		in.SourceProvider, in.SourceAccount, in.SourceExternalID = "gmail", "effect-account", "effect-record"
	}
	var ingested IngestResult
	var err error
	if identified {
		origin := uuid.MustParse("00000000-0000-4000-8000-000000000901")
		in.AccountFeedID = &origin
		err = service.WithSourceObservation(context.Background(), SourceObservationStart{OwnerUserID: in.OwnerUserID, WorkspaceID: in.WorkspaceID, OriginID: origin, ConfigDigest: strings.Repeat("a", 64)}, func(ctx context.Context) error {
			ingested, err = service.IngestContext(ctx, in)
			return err
		})
	} else {
		ingested, err = service.Ingest(in)
	}
	if err != nil || !ingested.Created {
		t.Fatalf("ingest fixture: %+v / %v", ingested, err)
	}
	op := ingested.Operation
	op.RiskLevel, op.AutonomyLevel, op.OwnerType = string(RiskLow), string(AutonomyAuto), string(OwnerHAI)
	op.CurrentDecision, op.RequiresApproval = string(DecisionRunSafeLocalWorker), false
	classified, err := service.Transition(op, StatusClassified, "test", "", "classify local work")
	if err != nil || classified == nil {
		t.Fatalf("classify fixture: %+v / %v", classified, err)
	}
	ready, err := service.Transition(*classified, StatusReady, "test", "", "ready local work")
	if err != nil || ready == nil {
		t.Fatalf("ready fixture: %+v / %v", ready, err)
	}
	worker := uuid.MustParse("00000000-0000-4000-8000-000000000301")
	claimed, err := service.ClaimOperation(context.Background(), ready.OwnerUserID, ready.WorkspaceID, ready.ID, worker, time.Minute)
	if err != nil || claimed == nil {
		t.Fatalf("claim fixture: %+v / %v", claimed, err)
	}
	op = claimed.Operation
	op.RuntimeID, op.VerificationStatus = "hai-local-safe-worker", string(VerificationPending)
	op.ResultSummary = "running intent; effect outcome not yet known"
	op.WorldModelStateJSON = `{"outcomeUncertain":true,"beforeEffect":false}`
	running, err := service.TransitionClaimed(context.Background(), claimed.Claim, op, StatusRunning, "test", worker.String(), "persist running intent")
	if err != nil || running == nil {
		t.Fatalf("running fixture: %+v / %v", running, err)
	}
	return claimedEffectFixture{repo: repo, service: service, op: *running, claim: claimed.Claim}
}

type claimedEffectSnapshot struct {
	ops    map[uuid.UUID]models.Operation
	events []models.OperationEvent
	claims map[uuid.UUID]memoryExecutionClaim
}

func claimedEffectTakeSnapshot(repo *MemoryRepository) claimedEffectSnapshot {
	repo.mu.Lock()
	defer repo.mu.Unlock()
	snapshot := claimedEffectSnapshot{
		ops: make(map[uuid.UUID]models.Operation), events: append([]models.OperationEvent{}, repo.events...),
		claims: make(map[uuid.UUID]memoryExecutionClaim),
	}
	for id, op := range repo.ops {
		snapshot.ops[id] = op
	}
	for id, claim := range repo.claims {
		snapshot.claims[id] = claim
	}
	return snapshot
}

func claimedEffectAssertUnchanged(t *testing.T, fixture claimedEffectFixture, before claimedEffectSnapshot) {
	t.Helper()
	if !reflect.DeepEqual(claimedEffectTakeSnapshot(fixture.repo), before) {
		t.Fatal("boundary changed stored running intent, audit trail or claim")
	}
}

func claimedEffectExpectedScope(fixture claimedEffectFixture) SafeEffectScope {
	scope := SafeEffectScope{
		OperationID: fixture.op.ID, Version: fixture.op.Version,
		OwnerUserID: fixture.op.OwnerUserID, WorkspaceID: fixture.op.WorkspaceID,
		ClaimOwner: fixture.claim.Owner, ClaimGeneration: fixture.claim.Generation,
		SourceIdentityHash: fixture.op.SourceIdentityHash, SourceRevisionHash: fixture.op.SourceRevisionHash,
	}
	if fixture.op.SourceObservationID != nil {
		scope.SourceObservationID = fixture.op.SourceObservationID.String()
		scope.SourceObservationGeneration = fixture.op.SourceObservationGeneration
		head := fixture.repo.sourceHeads[sourceHeadKey{fixture.op.OwnerUserID, fixture.op.WorkspaceID, fixture.op.SourceIdentityHash}]
		scope.SourceHeadGeneration, scope.SourceConfigEpoch = head.ObservationGeneration, head.ConfigEpoch
	}
	return scope
}

func claimedEffectWait(t *testing.T, done <-chan struct{}, label string) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", label)
	}
}

type claimedEffectLegacyRepository struct{ Repository }

type claimedEffectForwardRepository struct {
	Repository
	forward func(context.Context, ExecutionClaim, models.Operation, func(context.Context) error) error
}

func (r *claimedEffectForwardRepository) WithClaimedSafeEffect(ctx context.Context, claim ExecutionClaim, expected models.Operation, effect func(context.Context) error) error {
	return r.forward(ctx, claim, expected, effect)
}

func TestWithClaimedSafeEffectRequiresCapabilityAndNonNilCallback(t *testing.T) {
	fixture := newClaimedEffectFixture(t, true)
	before := claimedEffectTakeSnapshot(fixture.repo)
	called := false
	effect := func(context.Context) error { called = true; return nil }
	legacy := NewService(claimedEffectLegacyRepository{Repository: fixture.repo})
	if err := legacy.WithClaimedSafeEffect(context.Background(), fixture.claim, fixture.op, effect); !errors.Is(err, ErrSafeEffectUnsupported) || called {
		t.Fatalf("missing capability fell back to an unfenced callback: %v / called=%t", err, called)
	}
	for _, call := range []func() error{
		func() error {
			return fixture.service.WithClaimedSafeEffect(context.Background(), fixture.claim, fixture.op, nil)
		},
		func() error {
			return fixture.repo.WithClaimedSafeEffect(context.Background(), fixture.claim, fixture.op, nil)
		},
		func() error {
			return (&GormRepository{}).WithClaimedSafeEffect(context.Background(), fixture.claim, fixture.op, effect)
		},
	} {
		if err := call(); !errors.Is(err, ErrSafeEffectUnsupported) {
			t.Fatalf("unsupported/nil callback accepted: %v", err)
		}
	}
	if called {
		t.Fatal("unsupported boundary invoked a callback")
	}
	claimedEffectAssertUnchanged(t, fixture, before)
}

func TestWithClaimedSafeEffectServiceForwardsBoundaryArguments(t *testing.T) {
	fixture := newClaimedEffectFixture(t, true)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	effectErr := errors.New("callback result")
	forwardErr := errors.New("repository boundary result")
	calls, effects := 0, 0
	adapter := &claimedEffectForwardRepository{Repository: fixture.repo}
	adapter.forward = func(gotCtx context.Context, claim ExecutionClaim, expected models.Operation, effect func(context.Context) error) error {
		calls++
		if gotCtx != ctx || claim != fixture.claim || !reflect.DeepEqual(expected, fixture.op) || effect == nil {
			t.Error("service changed forwarded context, claim, expected snapshot or callback")
		}
		if err := effect(gotCtx); err != effectErr {
			t.Errorf("forwarded callback changed: %v", err)
		}
		return forwardErr
	}
	err := NewService(adapter).WithClaimedSafeEffect(ctx, fixture.claim, fixture.op, func(context.Context) error {
		effects++
		return effectErr
	})
	if err != forwardErr || calls != 1 || effects != 1 {
		t.Fatalf("service did not forward exactly once: %v / boundary=%d effect=%d", err, calls, effects)
	}
}

type claimedEffectPayloadKey struct{}

func TestWithClaimedSafeEffectSuppliesOnlyActiveServerScope(t *testing.T) {
	for _, identified := range []bool{false, true} {
		t.Run(fmt.Sprintf("identified_%t", identified), func(t *testing.T) {
			fixture := newClaimedEffectFixture(t, identified)
			before := claimedEffectTakeSnapshot(fixture.repo)
			want := claimedEffectExpectedScope(fixture)
			fake := want
			fake.OwnerUserID, fake.Version = "payload-owner", 999
			raw, err := json.Marshal(fake)
			if err != nil {
				t.Fatal(err)
			}
			var decoded SafeEffectScope
			if err := json.Unmarshal(raw, &decoded); err != nil {
				t.Fatal(err)
			}
			ctx := context.WithValue(context.Background(), claimedEffectPayloadKey{}, fake)
			if _, ok := CurrentSafeEffectScope(nil); ok {
				t.Fatal("nil context supplied effect authority")
			}
			if _, ok := CurrentSafeEffectScope(ctx); ok {
				t.Fatal("exported payload scope activated server-only authority")
			}
			if _, ok := CurrentSafeEffectScope(context.WithValue(ctx, safeEffectContextKey{}, decoded)); ok {
				t.Fatal("JSON-decoded exported scope was accepted as a private live token")
			}
			// Non-authority fields in the caller copy cannot supply the locked policy.
			expected := fixture.op
			expected.Status, expected.RuntimeID, expected.OwnerType = "payload-status", "payload-runtime", string(OwnerRobert)
			expected.RiskLevel, expected.AutonomyLevel, expected.CurrentDecision = string(RiskHigh), string(AutonomyObserve), string(DecisionBlock)
			expected.RequiresApproval, expected.VerificationStatus = true, string(VerificationPassed)
			var saved context.Context
			calls := 0
			err = fixture.service.WithClaimedSafeEffect(ctx, fixture.claim, expected, func(effectCtx context.Context) error {
				calls++
				saved = effectCtx
				scope, ok := CurrentSafeEffectScope(effectCtx)
				if !ok || !reflect.DeepEqual(scope, want) {
					t.Errorf("callback scope not copied from server authority: %+v / %t; want %+v", scope, ok, want)
				}
				scope.OwnerUserID = "mutated local copy"
				again, ok := CurrentSafeEffectScope(effectCtx)
				if !ok || !reflect.DeepEqual(again, want) {
					t.Error("scope value mutation changed server authority")
				}
				canceledChild, cancelChild := context.WithCancel(effectCtx)
				cancelChild()
				if _, ok := CurrentSafeEffectScope(canceledChild); ok {
					t.Error("canceled supplied context retained authority while original context was live")
				}
				if scope, ok := CurrentSafeEffectScope(effectCtx); !ok || !reflect.DeepEqual(scope, want) {
					t.Error("canceling a child context revoked the original live callback authority")
				}
				deadline, ok := effectCtx.Deadline()
				if !ok || !deadline.After(time.Now()) || time.Until(deadline) > 5*time.Second {
					t.Error("callback did not receive a live context bounded to five seconds")
				}
				return nil
			})
			if err != nil || calls != 1 || saved == nil {
				t.Fatalf("safe effect not invoked exactly once: %v / calls=%d", err, calls)
			}
			if saved.Err() == nil {
				t.Fatal("callback context remained live after return")
			}
			for _, retained := range []context.Context{saved, context.WithoutCancel(saved)} {
				if _, ok := CurrentSafeEffectScope(retained); ok {
					t.Fatal("retained context extended private effect authority after callback return")
				}
			}
			claimedEffectAssertUnchanged(t, fixture, before)
		})
	}
}

func TestWithClaimedSafeEffectRejectsForgedSnapshotsAndClaims(t *testing.T) {
	for _, kind := range []string{
		"expected_id", "expected_owner", "empty_expected_owner", "expected_workspace", "empty_expected_workspace", "zero_version", "stale_version", "future_version",
		"source_provider", "source_account", "source_external", "source_hash", "source_revision", "legacy_revision",
		"claim_id", "claim_owner_scope", "empty_claim_scope", "claim_workspace", "empty_claim_workspace", "zero_claim_owner", "other_worker", "zero_generation", "wrong_generation", "missing_operation", "missing_claim", "expired_lease",
	} {
		t.Run(kind, func(t *testing.T) {
			fixture := newClaimedEffectFixture(t, kind != "legacy_revision")
			expected, claim := fixture.op, fixture.claim
			wantErr := ErrClaimLost
			switch kind {
			case "expected_id":
				expected.ID = uuid.Nil
			case "expected_owner":
				expected.OwnerUserID = "another-owner"
			case "empty_expected_owner":
				expected.OwnerUserID = ""
			case "expected_workspace":
				expected.WorkspaceID = "another-workspace"
			case "empty_expected_workspace":
				expected.WorkspaceID = ""
			case "zero_version":
				expected.Version = 0
				wantErr = ErrStaleOperation
			case "stale_version":
				expected.Version--
				wantErr = ErrStaleOperation
			case "future_version":
				expected.Version++
				wantErr = ErrStaleOperation
			case "source_provider":
				expected.SourceProvider = "github"
				wantErr = ErrSourceIdentityImmutable
			case "source_account":
				expected.SourceAccount += ":other"
				wantErr = ErrSourceIdentityImmutable
			case "source_external":
				expected.SourceExternalID += ":other"
				wantErr = ErrSourceIdentityImmutable
			case "source_hash":
				expected.SourceIdentityHash = strings.Repeat("0", 64)
				wantErr = ErrSourceIdentityImmutable
			case "source_revision", "legacy_revision":
				expected.SourceRevisionHash = "forged opaque revision"
				wantErr = ErrSourceIdentityImmutable
			case "claim_id":
				claim.OperationID = uuid.Nil
			case "claim_owner_scope":
				claim.OwnerUserID = "another-owner"
			case "empty_claim_scope":
				claim.OwnerUserID = ""
			case "claim_workspace":
				claim.WorkspaceID = "another-workspace"
			case "empty_claim_workspace":
				claim.WorkspaceID = ""
			case "zero_claim_owner":
				claim.Owner = uuid.Nil
			case "other_worker":
				claim.Owner = uuid.MustParse("00000000-0000-4000-8000-000000000302")
			case "zero_generation":
				claim.Generation = 0
			case "wrong_generation":
				claim.Generation++
			case "missing_operation":
				delete(fixture.repo.ops, fixture.op.ID)
			case "missing_claim":
				delete(fixture.repo.claims, fixture.op.ID)
			case "expired_lease":
				stored := fixture.repo.claims[fixture.op.ID]
				stored.expiresAt = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
				fixture.repo.claims[fixture.op.ID] = stored
			}
			if kind == "source_provider" || kind == "source_account" || kind == "source_external" {
				var err error
				expected.SourceIdentityHash, err = SourceIdentityDigest(expected.SourceProvider, expected.SourceAccount, expected.SourceExternalID)
				if err != nil {
					t.Fatal(err)
				}
			}
			before := claimedEffectTakeSnapshot(fixture.repo)
			calls := 0
			err := fixture.service.WithClaimedSafeEffect(context.Background(), claim, expected, func(context.Context) error { calls++; return nil })
			if !errors.Is(err, wantErr) || calls != 0 {
				t.Fatalf("forged/missing/expired authority entered callback: %v / calls=%d; want %v", err, calls, wantErr)
			}
			claimedEffectAssertUnchanged(t, fixture, before)
		})
	}
}

func TestWithClaimedSafeEffectUsesLockedCurrentPolicy(t *testing.T) {
	for _, kind := range []string{"status", "runtime", "verification", "risk", "autonomy", "owner", "approval", "decision"} {
		t.Run(kind, func(t *testing.T) {
			fixture := newClaimedEffectFixture(t, true)
			current := fixture.op
			switch kind {
			case "status":
				current.Status = string(StatusReady)
			case "runtime":
				current.RuntimeID = "another-runtime"
			case "verification":
				current.VerificationStatus = string(VerificationNotRequired)
			case "risk":
				current.RiskLevel = string(RiskHigh)
			case "autonomy":
				current.AutonomyLevel = string(AutonomyObserve)
			case "owner":
				current.OwnerType = string(OwnerRobert)
			case "approval":
				current.RequiresApproval = true
			case "decision":
				current.CurrentDecision = string(DecisionObserveOnly)
			}
			fixture.repo.ops[current.ID] = current
			before := claimedEffectTakeSnapshot(fixture.repo)
			calls := 0
			err := fixture.service.WithClaimedSafeEffect(context.Background(), fixture.claim, fixture.op, func(context.Context) error { calls++; return nil })
			if !errors.Is(err, ErrOperationNotClaimable) || calls != 0 {
				t.Fatalf("caller snapshot bypassed locked policy: %v / calls=%d", err, calls)
			}
			claimedEffectAssertUnchanged(t, fixture, before)
		})
	}
}

func TestWithClaimedSafeEffectCancellationBeforeCallback(t *testing.T) {
	fixture := newClaimedEffectFixture(t, true)
	for _, expired := range []bool{false, true} {
		t.Run(fmt.Sprintf("expired_%t", expired), func(t *testing.T) {
			var ctx context.Context
			var cancel context.CancelFunc
			wantErr := context.Canceled
			if expired {
				ctx, cancel = context.WithDeadline(context.Background(), time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC))
				wantErr = context.DeadlineExceeded
			} else {
				ctx, cancel = context.WithCancel(context.Background())
				cancel()
			}
			defer cancel()
			before := claimedEffectTakeSnapshot(fixture.repo)
			calls := 0
			err := fixture.service.WithClaimedSafeEffect(ctx, fixture.claim, fixture.op, func(context.Context) error { calls++; return nil })
			if !errors.Is(err, wantErr) || calls != 0 {
				t.Fatalf("canceled request entered callback: %v / calls=%d", err, calls)
			}
			claimedEffectAssertUnchanged(t, fixture, before)
		})
	}
}

func TestWithClaimedSafeEffectCanceledLockWaitDoesNotEnterCallback(t *testing.T) {
	fixture := newClaimedEffectFixture(t, true)
	before := claimedEffectTakeSnapshot(fixture.repo)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started, done := make(chan struct{}), make(chan struct{})
	result := make(chan error, 1)
	var called atomic.Bool
	fixture.repo.mu.Lock()
	locked := true
	t.Cleanup(func() {
		cancel()
		if locked {
			fixture.repo.mu.Unlock()
			locked = false
		}
		claimedEffectWait(t, done, "canceled lock-wait cleanup")
	})
	go func() {
		defer close(done)
		close(started)
		result <- fixture.service.WithClaimedSafeEffect(ctx, fixture.claim, fixture.op, func(context.Context) error {
			called.Store(true)
			return nil
		})
	}()
	claimedEffectWait(t, started, "boundary attempt")
	cancel()
	claimedEffectWait(t, done, "canceled boundary while mutex remains held")
	if err := <-result; !errors.Is(err, context.Canceled) || called.Load() {
		t.Fatalf("lock wait ignored cancellation: %v / called=%t", err, called.Load())
	}
	fixture.repo.mu.Unlock()
	locked = false
	claimedEffectAssertUnchanged(t, fixture, before)
}

func TestWithClaimedSafeEffectErrorRetainsRunningIntentAndRevokesScope(t *testing.T) {
	fixture := newClaimedEffectFixture(t, true)
	before := claimedEffectTakeSnapshot(fixture.repo)
	effectErr := errors.New("effect outcome uncertain")
	var saved context.Context
	calls := 0
	err := fixture.service.WithClaimedSafeEffect(context.Background(), fixture.claim, fixture.op, func(ctx context.Context) error {
		calls++
		saved = ctx
		return effectErr
	})
	if err != effectErr || calls != 1 {
		t.Fatalf("callback error lost or automatically retried: %v / calls=%d", err, calls)
	}
	if _, ok := CurrentSafeEffectScope(context.WithoutCancel(saved)); ok {
		t.Fatal("error return retained effect authority")
	}
	// State retention is not proof that the callback produced no filesystem effect.
	claimedEffectAssertUnchanged(t, fixture, before)
}

func TestWithClaimedSafeEffectDeadlineDoesNotExceedLeaseOrCaller(t *testing.T) {
	for _, bound := range []string{"five_seconds", "live_lease", "caller_deadline"} {
		t.Run(bound, func(t *testing.T) {
			fixture := newClaimedEffectFixture(t, true)
			ctx := context.Background()
			var cancel context.CancelFunc
			lease := fixture.repo.claims[fixture.op.ID]
			if bound == "live_lease" {
				lease.expiresAt = time.Now().Add(time.Second)
				fixture.repo.claims[fixture.op.ID] = lease
			}
			if bound == "caller_deadline" {
				ctx, cancel = context.WithTimeout(ctx, time.Second)
				defer cancel()
			}
			err := fixture.service.WithClaimedSafeEffect(ctx, fixture.claim, fixture.op, func(effectCtx context.Context) error {
				deadline, ok := effectCtx.Deadline()
				if !ok || time.Until(deadline) > 5*time.Second || deadline.After(lease.expiresAt) {
					t.Errorf("deadline not bounded by five seconds and exact lease expiry: %v / lease=%v", deadline, lease.expiresAt)
				}
				if parentDeadline, ok := ctx.Deadline(); ok && deadline.After(parentDeadline) {
					t.Error("callback extended caller deadline")
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestWithClaimedSafeEffectContextExpiryRevokesScopeInsideCallback(t *testing.T) {
	for _, expired := range []bool{false, true} {
		t.Run(fmt.Sprintf("deadline_expiry_%t", expired), func(t *testing.T) {
			fixture := newClaimedEffectFixture(t, true)
			before := claimedEffectTakeSnapshot(fixture.repo)
			var ctx context.Context
			var cancel context.CancelFunc
			wantErr := context.Canceled
			if expired {
				ctx, cancel = context.WithTimeout(context.Background(), 250*time.Millisecond)
				wantErr = context.DeadlineExceeded
			} else {
				ctx, cancel = context.WithCancel(context.Background())
			}
			defer cancel()
			var saved context.Context
			err := fixture.service.WithClaimedSafeEffect(ctx, fixture.claim, fixture.op, func(effectCtx context.Context) error {
				saved = effectCtx
				if _, ok := CurrentSafeEffectScope(effectCtx); !ok {
					t.Error("callback began without live scope")
				}
				detachedBeforeExpiry := context.WithoutCancel(effectCtx)
				if _, ok := CurrentSafeEffectScope(detachedBeforeExpiry); !ok {
					t.Error("detached context lost authority before original context expired")
				}
				if !expired {
					cancel()
				}
				select {
				case <-effectCtx.Done():
				case <-time.After(2 * time.Second):
					return errors.New("callback context did not inherit cancellation/deadline")
				}
				if _, ok := CurrentSafeEffectScope(effectCtx); ok {
					t.Error("expired callback context still supplied authority before return")
				}
				// Still inside the synchronous callback: the active token has not
				// been revoked by return, so the original authority context must fence it.
				for _, detached := range []context.Context{detachedBeforeExpiry, context.WithoutCancel(effectCtx)} {
					if detached.Err() != nil {
						t.Error("WithoutCancel did not detach the supplied context as expected")
					}
					if _, ok := CurrentSafeEffectScope(detached); ok {
						t.Error("WithoutCancel regained authority after original context expiry while callback was active")
					}
				}
				return effectCtx.Err()
			})
			if !errors.Is(err, wantErr) || saved == nil || saved.Err() == nil {
				t.Fatalf("callback expiry not propagated: %v; want %v", err, wantErr)
			}
			if _, ok := CurrentSafeEffectScope(context.WithoutCancel(saved)); ok {
				t.Fatal("context expiry retained private authority after callback return")
			}
			claimedEffectAssertUnchanged(t, fixture, before)
		})
	}
}

func TestWithClaimedSafeEffectHoldsMutexAndBlocksReleaseUntilReturn(t *testing.T) {
	fixture := newClaimedEffectFixture(t, true)
	before := claimedEffectTakeSnapshot(fixture.repo)
	entered, resume, boundaryDone := make(chan struct{}), make(chan struct{}), make(chan struct{})
	boundaryResult := make(chan error, 1)
	var resumeOnce sync.Once
	unblock := func() { resumeOnce.Do(func() { close(resume) }) }
	t.Cleanup(func() {
		unblock()
		claimedEffectWait(t, boundaryDone, "boundary cleanup")
	})
	go func() {
		defer close(boundaryDone)
		boundaryResult <- fixture.service.WithClaimedSafeEffect(context.Background(), fixture.claim, fixture.op, func(ctx context.Context) error {
			close(entered)
			select {
			case <-resume:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()
	claimedEffectWait(t, entered, "callback entry")
	// Direct lock observation, not a sleep or scheduling delay, establishes exclusion.
	if fixture.repo.mu.TryLock() {
		fixture.repo.mu.Unlock()
		t.Fatal("repository released its mutex while callback was active")
	}
	attempted, releaseDone := make(chan struct{}), make(chan struct{})
	releaseResult := make(chan error, 1)
	t.Cleanup(func() {
		unblock()
		claimedEffectWait(t, releaseDone, "release cleanup")
	})
	go func() {
		defer close(releaseDone)
		close(attempted)
		releaseResult <- fixture.service.ReleaseClaim(context.Background(), fixture.claim)
	}()
	claimedEffectWait(t, attempted, "release attempt")
	select {
	case <-releaseDone:
		t.Fatal("claim release completed while callback held the repository lock")
	default:
	}
	unblock()
	claimedEffectWait(t, boundaryDone, "callback return")
	claimedEffectWait(t, releaseDone, "release after callback")
	if err := <-boundaryResult; err != nil {
		t.Fatalf("boundary failed: %v", err)
	}
	if err := <-releaseResult; err != nil {
		t.Fatalf("release failed after callback: %v", err)
	}
	after := claimedEffectTakeSnapshot(fixture.repo)
	if !reflect.DeepEqual(after.ops, before.ops) || !reflect.DeepEqual(after.events, before.events) {
		t.Fatal("effect boundary/release changed running intent or audit rows")
	}
	claim := after.claims[fixture.op.ID]
	if claim.owner != uuid.Nil || claim.generation != fixture.claim.Generation+1 || !claim.expiresAt.IsZero() {
		t.Fatalf("release did not complete after callback: %+v", claim)
	}
}

type claimedEffectSQLState struct {
	op                       models.Operation
	head                     SourceHead
	origin                   SourceOrigin
	originLocked, headLocked bool
	claim                    ExecutionClaim
	expiresAt                time.Time
	dbNow                    time.Time
	missingOp, missingClaim  bool
	active                   bool
	phase                    int
	commits, rollbacks       int
}

func TestGormClaimedEffectRefusesSingleConnectionPoolBeforeTransaction(t *testing.T) {
	fixture := newClaimedEffectFixture(t, true)
	state := &claimedEffectSQLState{op: fixture.op, claim: fixture.claim}
	sqlDB := sql.OpenDB(claimedEffectSQLConnector{state: state})
	t.Cleanup(func() { _ = sqlDB.Close() })
	sqlDB.SetMaxOpenConns(1)
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB, PreferSimpleProtocol: true}), &gorm.Config{DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	called := false
	err = NewGormRepository(db).WithClaimedSafeEffect(context.Background(), fixture.claim, fixture.op, func(context.Context) error {
		called = true
		return nil
	})
	if !errors.Is(err, ErrSafeEffectPoolCapacity) || called || state.active || state.phase != 0 || state.commits != 0 || state.rollbacks != 0 {
		t.Fatalf("undersized pool entered transaction or effect: %v / called=%t / %+v", err, called, state)
	}
}

type claimedEffectSQLConnector struct{ state *claimedEffectSQLState }
type claimedEffectSQLDriver struct{}
type claimedEffectSQLConnection struct{ state *claimedEffectSQLState }
type claimedEffectSQLTransaction struct{ state *claimedEffectSQLState }

func (c claimedEffectSQLConnector) Connect(context.Context) (driver.Conn, error) {
	return &claimedEffectSQLConnection{state: c.state}, nil
}

func (claimedEffectSQLConnector) Driver() driver.Driver { return claimedEffectSQLDriver{} }
func (claimedEffectSQLDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("use claimedEffectSQLConnector")
}

func (*claimedEffectSQLConnection) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepared statement at effect boundary")
}

func (*claimedEffectSQLConnection) Close() error { return nil }
func (c *claimedEffectSQLConnection) Begin() (driver.Tx, error) {
	if c.state.active {
		return nil, errors.New("unexpected nested effect transaction")
	}
	c.state.active = true
	return &claimedEffectSQLTransaction{state: c.state}, nil
}

func (c *claimedEffectSQLConnection) BeginTx(ctx context.Context, _ driver.TxOptions) (driver.Tx, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return c.Begin()
}

func (c *claimedEffectSQLConnection) ExecContext(ctx context.Context, query string, _ []driver.NamedValue) (driver.Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !c.state.active || c.state.phase != 0 || query != "SET LOCAL lock_timeout = '5s'" {
		return nil, fmt.Errorf("unexpected effect boundary SQL write: %s", query)
	}
	return driver.RowsAffected(0), nil
}

func (c *claimedEffectSQLConnection) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !c.state.active {
		return nil, errors.New("effect boundary SQL outside transaction")
	}
	switch {
	case strings.HasPrefix(query, `SELECT * FROM "operation_source_origins"`) && strings.Contains(query, "FOR UPDATE"):
		if c.state.phase != 0 || c.state.originLocked || len(args) < 3 || args[0].Value != c.state.claim.OwnerUserID || args[1].Value != c.state.claim.WorkspaceID || fmt.Sprint(args[2].Value) != c.state.origin.OriginID.String() {
			return nil, errors.New("source origin must be locked first in exact tenant scope")
		}
		c.state.originLocked = true
		origin := c.state.origin
		return &mutationRows{columns: []string{"owner_user_id", "workspace_id", "origin_id", "config_digest", "config_epoch"}, values: [][]driver.Value{{origin.OwnerUserID, origin.WorkspaceID, origin.OriginID.String(), origin.ConfigDigest, origin.ConfigEpoch}}}, nil
	case strings.HasPrefix(query, `SELECT * FROM "operation_source_heads"`) && strings.Contains(query, "FOR UPDATE"):
		if c.state.phase != 0 || !c.state.originLocked || c.state.headLocked || len(args) < 3 || args[0].Value != c.state.claim.OwnerUserID || args[1].Value != c.state.claim.WorkspaceID || args[2].Value != c.state.head.SourceIdentityHash {
			return nil, errors.New("source head must be locked after origin and before operation")
		}
		c.state.headLocked = true
		head := c.state.head
		return &mutationRows{columns: []string{"owner_user_id", "workspace_id", "source_identity_hash", "origin_id", "operation_id", "revision_hash", "observation_id", "observation_generation", "config_epoch", "state"}, values: [][]driver.Value{{head.OwnerUserID, head.WorkspaceID, head.SourceIdentityHash, head.OriginID.String(), head.OperationID.String(), head.RevisionHash, head.ObservationID.String(), head.ObservationGeneration, head.ConfigEpoch, head.State}}}, nil
	case strings.Contains(query, `count(*) FROM "operation_source_observations"`):
		if len(args) == 7 {
			if c.state.phase != 1 || !c.state.headLocked || fmt.Sprint(args[0].Value) != c.state.head.ObservationID.String() || args[1].Value != c.state.head.OwnerUserID || args[2].Value != c.state.head.WorkspaceID || args[3].Value != c.state.head.ObservationGeneration || fmt.Sprint(args[4].Value) != c.state.head.OriginID.String() || args[5].Value != c.state.head.ConfigEpoch || args[6].Value != c.state.origin.ConfigDigest {
				return nil, errors.New("wrong accepted head observation authority")
			}
			return &mutationRows{columns: []string{"count"}, values: [][]driver.Value{{int64(1)}}}, nil
		}
		if c.state.phase != 1 || !c.state.headLocked || len(args) != 5 || fmt.Sprint(args[0].Value) != c.state.op.SourceObservationID.String() || args[1].Value != c.state.op.OwnerUserID || args[2].Value != c.state.op.WorkspaceID || args[3].Value != c.state.op.SourceObservationGeneration || fmt.Sprint(args[4].Value) != c.state.op.AccountFeedID.String() {
			return nil, errors.New("wrong immutable observation reference at effect")
		}
		return &mutationRows{columns: []string{"count"}, values: [][]driver.Value{{int64(1)}}}, nil
	case strings.HasPrefix(query, `SELECT * FROM "operations"`) && strings.Contains(query, "FOR UPDATE"):
		if c.state.phase != 0 || !strings.Contains(query, "owner_user_id =") || !strings.Contains(query, "workspace_id =") || len(args) < 3 ||
			fmt.Sprint(args[0].Value) != c.state.claim.OperationID.String() || args[1].Value != c.state.claim.OwnerUserID || args[2].Value != c.state.claim.WorkspaceID {
			return nil, errors.New("operation was not locked first with exact server scope")
		}
		if c.state.op.SourceIdentityHash != "" && !c.state.headLocked {
			return nil, errors.New("identified operation locked before source authority")
		}
		c.state.phase = 1
		op := c.state.op
		rows := &mutationRows{columns: []string{
			"id", "owner_user_id", "workspace_id", "title", "dedupe_key", "operation_type", "source_type",
			"status", "risk_level", "autonomy_level", "owner_type", "current_decision", "verification_status", "requires_approval", "runtime_id", "version",
			"source_provider", "source_account", "source_external_id", "source_identity_hash", "source_revision_hash", "created_at", "updated_at",
			"account_feed_id", "source_observation_id", "source_observation_generation",
		}}
		if !c.state.missingOp {
			rows.values = [][]driver.Value{{
				op.ID.String(), op.OwnerUserID, op.WorkspaceID, op.Title, op.DedupeKey, op.OperationType, op.SourceType,
				op.Status, op.RiskLevel, op.AutonomyLevel, op.OwnerType, op.CurrentDecision, op.VerificationStatus, op.RequiresApproval, op.RuntimeID, op.Version,
				op.SourceProvider, op.SourceAccount, op.SourceExternalID, op.SourceIdentityHash, op.SourceRevisionHash, op.CreatedAt, op.UpdatedAt,
				op.AccountFeedID.String(), op.SourceObservationID.String(), op.SourceObservationGeneration,
			}}
		}
		return rows, nil
	case strings.Contains(query, "FROM public.operation_execution_claims") && strings.Contains(query, "FOR UPDATE"):
		if c.state.phase != 1 || len(args) != 1 || fmt.Sprint(args[0].Value) != c.state.claim.OperationID.String() {
			return nil, errors.New("claim was not locked after the scoped operation")
		}
		c.state.phase = 2
		rows := &mutationRows{columns: []string{"owner", "generation", "expires_at"}}
		if !c.state.missingClaim {
			rows.values = [][]driver.Value{{c.state.claim.Owner.String(), c.state.claim.Generation, c.state.expiresAt}}
		}
		return rows, nil
	case strings.Contains(query, "SELECT clock_timestamp()"):
		if c.state.phase != 2 {
			return nil, errors.New("lease time read before acquiring both locks")
		}
		c.state.phase = 3
		return &mutationRows{columns: []string{"clock_timestamp"}, values: [][]driver.Value{{c.state.dbNow}}}, nil
	default:
		return nil, fmt.Errorf("unexpected effect boundary SQL: %s", query)
	}
}

func (tx *claimedEffectSQLTransaction) Commit() error {
	tx.state.active = false
	tx.state.commits++
	return nil
}

func (tx *claimedEffectSQLTransaction) Rollback() error {
	tx.state.active = false
	tx.state.rollbacks++
	return nil
}

func TestWithClaimedSafeEffectGormLockOrderAndCallbackTransaction(t *testing.T) {
	for _, outcome := range []string{"success", "short_lease", "callback_error", "stale_version", "source_revision", "unsafe_policy", "wrong_generation", "expired_lease", "missing_operation", "missing_claim"} {
		t.Run(outcome, func(t *testing.T) {
			fixture := newClaimedEffectFixture(t, true)
			state := &claimedEffectSQLState{
				op: fixture.op, claim: fixture.claim,
				expiresAt: fixture.repo.claims[fixture.op.ID].expiresAt, dbNow: time.Now().UTC(),
			}
			state.head = fixture.repo.sourceHeads[sourceHeadKey{fixture.op.OwnerUserID, fixture.op.WorkspaceID, fixture.op.SourceIdentityHash}]
			state.origin = fixture.repo.sourceOrigins[sourceOriginKey{fixture.op.OwnerUserID, fixture.op.WorkspaceID, *fixture.op.AccountFeedID}]
			expected := fixture.op
			effectErr := errors.New("uncertain local effect result")
			var wantErr error
			switch outcome {
			case "short_lease":
				state.expiresAt = state.dbNow.Add(2 * time.Second)
			case "callback_error":
				wantErr = effectErr
			case "stale_version":
				expected.Version--
				wantErr = ErrStaleOperation
			case "source_revision":
				expected.SourceRevisionHash = "other revision"
				wantErr = ErrSourceIdentityImmutable
			case "unsafe_policy":
				state.op.RequiresApproval = true
				wantErr = ErrOperationNotClaimable
			case "wrong_generation":
				state.claim.Generation++
				wantErr = ErrClaimLost
			case "expired_lease":
				state.expiresAt = state.dbNow.Add(-time.Second)
				wantErr = ErrClaimLost
			case "missing_operation":
				state.missingOp = true
				wantErr = ErrClaimLost
			case "missing_claim":
				state.missingClaim = true
				wantErr = ErrClaimLost
			}
			before := state.op
			// Connector-backed SQL only: no Postgres instance, network or subprocess.
			sqlDB := sql.OpenDB(claimedEffectSQLConnector{state: state})
			t.Cleanup(func() { _ = sqlDB.Close() })
			db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB, PreferSimpleProtocol: true}),
				&gorm.Config{DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent)})
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			var saved context.Context
			err = NewService(NewGormRepository(db)).WithClaimedSafeEffect(context.Background(), fixture.claim, expected, func(ctx context.Context) error {
				calls++
				saved = ctx
				if !state.active || state.phase != 3 || state.commits != 0 || state.rollbacks != 0 {
					t.Error("callback ran before both locks or after transaction ended")
				}
				scope, ok := CurrentSafeEffectScope(ctx)
				if !ok || !reflect.DeepEqual(scope, claimedEffectExpectedScope(fixture)) {
					t.Errorf("Gorm callback scope not copied from locked server state: %+v / %t", scope, ok)
				}
				deadline, ok := ctx.Deadline()
				if !ok || time.Until(deadline) > 5*time.Second || deadline.After(state.expiresAt) {
					t.Errorf("Gorm callback deadline exceeded five seconds or exact lease expiry: %v / lease=%v", deadline, state.expiresAt)
				}
				if outcome == "callback_error" {
					return effectErr
				}
				return nil
			})
			wantCalls, wantCommits, wantRollbacks := 0, 0, 1
			if outcome == "success" || outcome == "short_lease" {
				wantCalls, wantCommits, wantRollbacks = 1, 1, 0
			} else if outcome == "callback_error" {
				wantCalls = 1
			}
			if (wantErr == nil && err != nil) || (wantErr != nil && !errors.Is(err, wantErr)) || calls != wantCalls ||
				state.active || state.commits != wantCommits || state.rollbacks != wantRollbacks || !reflect.DeepEqual(state.op, before) {
				t.Fatalf("Gorm boundary wiring: err=%v want=%v calls=%d state=%+v", err, wantErr, calls, state)
			}
			if saved != nil {
				if _, ok := CurrentSafeEffectScope(context.WithoutCancel(saved)); ok {
					t.Fatal("Gorm callback retained private authority after transaction return")
				}
			}
			if outcome == "callback_error" && err != effectErr {
				t.Fatal("Gorm boundary changed the callback error value")
			}
		})
	}
}
