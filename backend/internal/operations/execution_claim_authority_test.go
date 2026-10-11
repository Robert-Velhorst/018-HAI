package operations

import (
	"context"
	"database/sql"
	"database/sql/driver"
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

type claimAuthorityResult struct {
	claimed  *ClaimedOperation
	op       *models.Operation
	recovery RecoveryResult
	err      error
}

type claimAuthorityCall func(ClaimRepository, context.Context) claimAuthorityResult

func claimAuthorityCalls(t *testing.T, f claimedEffectFixture) map[string]claimAuthorityCall {
	t.Helper()
	updated, event, err := ApplyTransition(f.op, StatusInterrupted, "test", "", "claim authority regression", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	return map[string]claimAuthorityCall{
		"next": func(repo ClaimRepository, ctx context.Context) claimAuthorityResult {
			v, err := repo.ClaimNext(ctx, f.op.OwnerUserID, f.op.WorkspaceID, uuid.New(), time.Minute)
			return claimAuthorityResult{claimed: v, err: err}
		},
		"explicit": func(repo ClaimRepository, ctx context.Context) claimAuthorityResult {
			v, err := repo.ClaimOperation(ctx, f.op.OwnerUserID, f.op.WorkspaceID, f.op.ID, uuid.New(), time.Minute)
			return claimAuthorityResult{claimed: v, err: err}
		},
		"renew": func(repo ClaimRepository, ctx context.Context) claimAuthorityResult {
			return claimAuthorityResult{err: repo.RenewClaim(ctx, f.claim, time.Minute)}
		},
		"transition": func(repo ClaimRepository, ctx context.Context) claimAuthorityResult {
			v, err := repo.TransitionClaimed(ctx, f.claim, updated, event, true)
			return claimAuthorityResult{op: v, err: err}
		},
		"release": func(repo ClaimRepository, ctx context.Context) claimAuthorityResult {
			return claimAuthorityResult{err: repo.ReleaseClaim(ctx, f.claim)}
		},
		"recover": func(repo ClaimRepository, ctx context.Context) claimAuthorityResult {
			v, err := repo.RecoverExpiredClaims(ctx, f.op.OwnerUserID, f.op.WorkspaceID, 200)
			return claimAuthorityResult{recovery: v, err: err}
		},
	}
}

func assertClaimAuthorityRefused(t *testing.T, result claimAuthorityResult, want error) {
	t.Helper()
	if !errors.Is(result.err, want) || result.claimed != nil || result.op != nil || !reflect.DeepEqual(result.recovery, RecoveryResult{}) {
		t.Fatalf("claim authority refusal = %+v, want %v and no result", result, want)
	}
}

func rejectedClaimAuthority(t *testing.T, kind string) (context.Context, error) {
	t.Helper()
	if kind == "canceled" {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		return ctx, context.Canceled
	}
	if strings.HasPrefix(kind, "safe_") {
		authority, cancel := context.WithTimeout(context.Background(), time.Hour)
		t.Cleanup(cancel)
		var original context.Context = authority
		if kind == "safe_canceled" {
			cancel()
		} else if kind == "safe_expired" {
			original = delayedEffectDeadlineContext{context.Background(), time.Now().Add(-time.Hour)}
		}
		state := &activeSafeEffect{authority: original}
		state.active.Store(kind != "safe_ended")
		return context.WithoutCancel(context.WithValue(original, safeEffectContextKey{}, state)), ErrInvalidSafeEffectAuthority
	}
	return rejectedIntakeContext(kind)
}

func TestClaimLifecycleRejectsAbsentExpiredAndEndedAuthorityBeforeAccess(t *testing.T) {
	for _, storage := range []string{"memory", "gorm"} {
		for _, kind := range []string{"nil", "canceled", "delayed_expiry", "ended_observation", "canceled_original", "expired_original", "safe_ended", "safe_canceled", "safe_expired"} {
			f := newClaimedEffectFixture(t, false)
			for name, call := range claimAuthorityCalls(t, f) {
				t.Run(storage+"/"+kind+"/"+name, func(t *testing.T) {
					ctx, want := rejectedClaimAuthority(t, kind)
					before := claimedEffectTakeSnapshot(f.repo)
					var repo ClaimRepository = f.repo
					var state *claimAuthoritySQL
					if storage == "gorm" {
						state = &claimAuthoritySQL{op: f.op, claim: f.claim}
						repo = claimAuthorityGorm(t, state)
					}
					assertClaimAuthorityRefused(t, call(repo, ctx), want)
					claimedEffectAssertUnchanged(t, f, before)
					if state != nil && (state.connects.Load() != 0 || state.begins.Load() != 0 || state.queries.Load() != 0 || state.execs.Load() != 0 || state.commits.Load() != 0 || state.rollbacks.Load() != 0) {
						t.Fatalf("refused authority accessed SQL: %+v", state)
					}
				})
			}
		}
	}
}

func TestClaimLifecycleTypedNilStoresFailClosed(t *testing.T) {
	f := newClaimedEffectFixture(t, false)
	var memory *MemoryRepository
	var gormNil *GormRepository
	for storage, repo := range map[string]ClaimRepository{"memory": memory, "gorm": gormNil, "gorm_database": &GormRepository{}} {
		for name, call := range claimAuthorityCalls(t, f) {
			t.Run(storage+"/"+name, func(t *testing.T) {
				assertClaimAuthorityRefused(t, call(repo, context.Background()), ErrAtomicClaimsUnsupported)
				assertClaimAuthorityRefused(t, call(repo, nil), ErrInvalidSourceObservation)
			})
		}
	}
}

type claimAuthorityDeadline struct {
	context.Context
	deadline atomic.Int64
}

func newClaimAuthorityDeadline() *claimAuthorityDeadline {
	ctx := &claimAuthorityDeadline{Context: context.Background()}
	ctx.deadline.Store(time.Now().Add(time.Hour).UnixNano())
	return ctx
}

func (c *claimAuthorityDeadline) Deadline() (time.Time, bool) {
	return time.Unix(0, c.deadline.Load()), true
}

func (c *claimAuthorityDeadline) expire() { c.deadline.Store(time.Now().Add(-time.Hour).UnixNano()) }

type claimAuthorityWaitContext struct {
	context.Context
	arrived chan struct{}
	once    sync.Once
}

func (c *claimAuthorityWaitContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.arrived) })
	return c.Context.Done()
}

func TestMemoryClaimLifecycleEndsAuthorityWhileWaitingOnMutex(t *testing.T) {
	for _, ending := range []string{"caller_cancel", "absolute_expiry", "observation_cancel", "observation_return", "safe_cancel", "safe_return"} {
		f := newClaimedEffectFixture(t, false)
		for name, call := range claimAuthorityCalls(t, f) {
			t.Run(ending+"/"+name, func(t *testing.T) {
				base, cancel := context.WithCancel(context.Background())
				defer cancel()
				var ctx context.Context = base
				end, want := cancel, error(context.Canceled)
				if ending == "absolute_expiry" {
					deadline := newClaimAuthorityDeadline()
					ctx, end, want = deadline, deadline.expire, context.DeadlineExceeded
				} else if strings.HasPrefix(ending, "observation_") {
					state := &activeSourceObservation{authority: base}
					state.active.Store(true)
					ctx = context.WithoutCancel(context.WithValue(base, sourceObservationContextKey{}, state))
					want = ErrInvalidSourceObservation
					if ending == "observation_return" {
						end = func() { state.active.Store(false) }
					}
				} else if strings.HasPrefix(ending, "safe_") {
					authority, stop := context.WithTimeout(base, time.Hour)
					defer stop()
					state := &activeSafeEffect{authority: authority}
					state.active.Store(true)
					ctx = context.WithoutCancel(context.WithValue(authority, safeEffectContextKey{}, state))
					want = ErrInvalidSafeEffectAuthority
					if ending == "safe_return" {
						end = func() { state.active.Store(false) }
					}
				}
				waiting := &claimAuthorityWaitContext{Context: ctx, arrived: make(chan struct{})}
				before := claimedEffectTakeSnapshot(f.repo)
				f.repo.mu.Lock()
				locked := true
				defer func() {
					if locked {
						f.repo.mu.Unlock()
					}
				}()
				done := make(chan claimAuthorityResult, 1)
				go func() { done <- call(f.repo, waiting) }()
				select {
				case <-waiting.arrived:
				case <-time.After(2 * time.Second):
					f.repo.mu.Unlock()
					locked = false
					end()
					select {
					case <-done:
					case <-time.After(2 * time.Second):
					}
					t.Fatal("claim call did not enter held-mutex wait")
				}
				end()
				select {
				case result := <-done:
					assertClaimAuthorityRefused(t, result, want)
				case <-time.After(2 * time.Second):
					f.repo.mu.Unlock()
					locked = false
					select {
					case <-done:
					case <-time.After(2 * time.Second):
					}
					t.Fatal("ended claim authority did not release waiter before mutex unlock")
				}
				f.repo.mu.Unlock()
				locked = false
				claimedEffectAssertUnchanged(t, f, before)
			})
		}
	}
}

// This driver exercises guard/transaction ordering, never actual PostgreSQL.
type claimAuthoritySQL struct {
	op                                                   models.Operation
	claim                                                ExecutionClaim
	want                                                 context.Context
	connects, begins, queries, execs, commits, rollbacks atomic.Int32
	active                                               atomic.Bool
	ended                                                chan struct{}
	endOnce                                              sync.Once
	onBegin                                              func()
	onQuery                                              func()
	waiting                                              chan struct{}
}

type claimAuthorityConnector struct{ state *claimAuthoritySQL }
type claimAuthorityDriver struct{}
type claimAuthorityConnection struct{ state *claimAuthoritySQL }
type claimAuthorityTransaction struct{ state *claimAuthoritySQL }

func (c claimAuthorityConnector) Driver() driver.Driver { return claimAuthorityDriver{} }
func (claimAuthorityDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("use claim authority connector")
}
func (c claimAuthorityConnector) Connect(ctx context.Context) (driver.Conn, error) {
	c.state.connects.Add(1)
	if c.state.want != nil && ctx != c.state.want {
		return nil, errors.New("claim connection lost context")
	}
	return &claimAuthorityConnection{state: c.state}, nil
}
func (*claimAuthorityConnection) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepared claim SQL")
}
func (*claimAuthorityConnection) Close() error { return nil }
func (c *claimAuthorityConnection) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}
func (c *claimAuthorityConnection) BeginTx(ctx context.Context, _ driver.TxOptions) (driver.Tx, error) {
	if c.state.want != nil && ctx != c.state.want {
		return nil, errors.New("claim transaction lost context")
	}
	c.state.begins.Add(1)
	c.state.active.Store(true)
	if c.state.onBegin != nil {
		c.state.onBegin()
	}
	return &claimAuthorityTransaction{state: c.state}, nil
}
func (tx *claimAuthorityTransaction) Commit() error {
	tx.state.active.Store(false)
	tx.state.commits.Add(1)
	tx.state.endOnce.Do(func() { close(tx.state.ended) })
	return nil
}
func (tx *claimAuthorityTransaction) Rollback() error {
	tx.state.active.Store(false)
	tx.state.rollbacks.Add(1)
	tx.state.endOnce.Do(func() { close(tx.state.ended) })
	return nil
}
func (c *claimAuthorityConnection) QueryContext(ctx context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	if !c.state.active.Load() || (c.state.want != nil && ctx != c.state.want) {
		return nil, errors.New("claim query outside original transaction/context")
	}
	c.state.queries.Add(1)
	if c.state.waiting != nil {
		close(c.state.waiting)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
			return nil, errors.New("claim SQL wait did not inherit original authority cancellation")
		}
	}
	if c.state.onQuery != nil {
		c.state.onQuery()
	}
	switch {
	case strings.Contains(query, `SELECT * FROM "operations"`):
		return &mutationRows{columns: []string{"id", "owner_user_id", "workspace_id", "status", "version"},
			values: [][]driver.Value{{c.state.op.ID.String(), c.state.op.OwnerUserID, c.state.op.WorkspaceID, c.state.op.Status, c.state.op.Version}}}, nil
	case strings.Contains(query, "RETURNING operation_id"):
		return &mutationRows{columns: []string{"operation_id"}, values: [][]driver.Value{{c.state.op.ID.String()}}}, nil
	case strings.Contains(query, "GROUP BY status"):
		return &mutationRows{columns: []string{"status", "total"}}, nil
	default:
		return nil, fmt.Errorf("unexpected claim query: %s", query)
	}
}
func (c *claimAuthorityConnection) ExecContext(context.Context, string, []driver.NamedValue) (driver.Result, error) {
	c.state.execs.Add(1)
	return nil, errors.New("unexpected claim write after authority expiry")
}

func claimAuthorityGorm(t *testing.T, state *claimAuthoritySQL) *GormRepository {
	t.Helper()
	state.ended = make(chan struct{})
	db := sql.OpenDB(claimAuthorityConnector{state: state})
	t.Cleanup(func() { _ = db.Close() })
	gormDB, err := gorm.Open(postgres.New(postgres.Config{Conn: db, PreferSimpleProtocol: true}),
		&gorm.Config{DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	return NewGormRepository(gormDB)
}

func TestGormClaimLifecycleRechecksAuthorityAfterTransactionAndQueryWait(t *testing.T) {
	for _, boundary := range []string{"begin", "query"} {
		f := newClaimedEffectFixture(t, false)
		for name, call := range claimAuthorityCalls(t, f) {
			t.Run(boundary+"/"+name, func(t *testing.T) {
				ctx := newClaimAuthorityDeadline()
				state := &claimAuthoritySQL{op: f.op, claim: f.claim, want: ctx}
				if boundary == "begin" {
					state.onBegin = ctx.expire
				} else {
					state.onQuery = ctx.expire
				}
				assertClaimAuthorityRefused(t, call(claimAuthorityGorm(t, state), ctx), context.DeadlineExceeded)
				queries := int32(0)
				if boundary == "query" {
					queries = 1
				}
				if ctx.Err() != nil || state.begins.Load() != 1 || state.queries.Load() != queries || state.execs.Load() != 0 || state.commits.Load() != 0 || state.rollbacks.Load() != 1 || state.active.Load() {
					t.Fatalf("late authority guard failed/SQL escaped rollback: %+v", state)
				}
			})
		}
	}
}

func TestGormClaimLifecycleCancelsDetachedPrivateAuthorityDuringSQLWait(t *testing.T) {
	for _, scope := range []string{"observation", "safe_effect", "combined"} {
		f := newClaimedEffectFixture(t, false)
		for name, call := range claimAuthorityCalls(t, f) {
			t.Run(scope+"/"+name, func(t *testing.T) {
				authority, cancel := context.WithTimeout(context.Background(), time.Hour)
				defer cancel()
				var ctx context.Context
				if scope == "observation" || scope == "combined" {
					ticket, err := f.repo.BeginSourceObservation(authority, SourceObservationStart{
						OwnerUserID: f.op.OwnerUserID, WorkspaceID: f.op.WorkspaceID,
						OriginID: uuid.New(), ConfigDigest: strings.Repeat("a", 64),
					})
					if err != nil {
						t.Fatal(err)
					}
					state := &activeSourceObservation{observation: ticket, authority: authority}
					state.active.Store(true)
					ctx = context.WithoutCancel(context.WithValue(authority, sourceObservationContextKey{}, state))
					if scope == "combined" {
						// Ending only the inner safe scope must cancel SQL even while
						// the outer source observation remains live.
						safeAuthority, stop := context.WithTimeout(context.WithValue(authority, sourceObservationContextKey{}, state), time.Hour)
						defer stop()
						safe := &activeSafeEffect{authority: safeAuthority, scope: claimedEffectExpectedScope(f)}
						safe.active.Store(true)
						ctx = context.WithoutCancel(context.WithValue(safeAuthority, safeEffectContextKey{}, safe))
						cancel = stop
					}
				} else {
					state := &activeSafeEffect{authority: authority, scope: claimedEffectExpectedScope(f)}
					state.active.Store(true)
					ctx = context.WithoutCancel(context.WithValue(authority, safeEffectContextKey{}, state))
				}
				before := claimedEffectTakeSnapshot(f.repo)
				state := &claimAuthoritySQL{op: f.op, claim: f.claim, waiting: make(chan struct{})}
				repo := claimAuthorityGorm(t, state)
				done := make(chan claimAuthorityResult, 1)
				go func() { done <- call(repo, ctx) }()
				select {
				case <-state.waiting:
				case <-time.After(2 * time.Second):
					cancel()
					select {
					case <-done:
					case <-time.After(3 * time.Second):
					}
					t.Fatal("claim did not reach SQL wait")
				}
				cancel()
				select {
				case result := <-done:
					assertClaimAuthorityRefused(t, result, context.Canceled)
				case <-time.After(3 * time.Second):
					t.Fatal("detached SQL query outlived original authority")
				}
				select {
				case <-state.ended:
				case <-time.After(3 * time.Second):
					t.Fatal("canceled claim transaction did not finish rollback")
				}
				if ctx.Err() != nil || state.queries.Load() != 1 || state.execs.Load() != 0 || state.commits.Load() != 0 || state.active.Load() {
					t.Fatalf("detached authority changed durable state: %+v", state)
				}
				claimedEffectAssertUnchanged(t, f, before)
			})
		}
	}
}

type claimRecoveryDeadline struct {
	context.Context
	reads int
}

func (c *claimRecoveryDeadline) Deadline() (time.Time, bool) {
	c.reads++
	if c.reads >= 9 {
		return time.Now().Add(-time.Hour), true
	}
	return time.Now().Add(time.Hour), true
}

func TestMemoryClaimRecoveryDoesNotPublishPartialBatchOnExpiredAuthority(t *testing.T) {
	f := newClaimedEffectFixture(t, false)
	other := cloneOperation(f.op)
	other.ID, other.DedupeKey = uuid.New(), "second recovery operation"
	f.repo.ops[other.ID] = other
	for _, op := range []models.Operation{f.op, other} {
		f.repo.claims[op.ID] = memoryExecutionClaim{owner: f.claim.Owner, generation: f.claim.Generation, expiresAt: time.Now().Add(-time.Hour)}
	}
	before := claimedEffectTakeSnapshot(f.repo)
	ctx := &claimRecoveryDeadline{Context: context.Background()}
	result, err := f.repo.RecoverExpiredClaims(ctx, f.op.OwnerUserID, f.op.WorkspaceID, 200)
	assertClaimAuthorityRefused(t, claimAuthorityResult{recovery: result, err: err}, context.DeadlineExceeded)
	claimedEffectAssertUnchanged(t, f, before)
	if ctx.Err() != nil || ctx.reads < 9 {
		t.Fatalf("did not reach late absolute deadline: reads=%d error=%v", ctx.reads, ctx.Err())
	}
}

func TestMemoryClaimAuthorityHealthyLeaseTransitionAndRecovery(t *testing.T) {
	repo := NewMemoryRepository()
	op := mutationSeed(t, repo)
	ctx := context.Background()
	first, err := repo.ClaimNext(ctx, op.OwnerUserID, op.WorkspaceID, uuid.New(), time.Minute)
	if err != nil || first == nil || first.Operation.ID != op.ID {
		t.Fatalf("healthy next claim: %+v / %v", first, err)
	}
	if err := repo.RenewClaim(ctx, first.Claim, time.Minute); err != nil {
		t.Fatal(err)
	}
	if repo.claims[op.ID].generation != first.Claim.Generation {
		t.Fatal("renew changed claim generation")
	}
	if err := repo.ReleaseClaim(ctx, first.Claim); err != nil {
		t.Fatal(err)
	}
	second, err := repo.ClaimOperation(ctx, op.OwnerUserID, op.WorkspaceID, op.ID, uuid.New(), time.Minute)
	if err != nil || second == nil || second.Claim.Generation != first.Claim.Generation+2 {
		t.Fatalf("healthy reclaimed generation: %+v / %v", second, err)
	}
	intent := second.Operation
	intent.RuntimeID, intent.VerificationStatus = "hai-local-safe-worker", string(VerificationPending)
	intent.ResultSummary, intent.WorldModelStateJSON = "before-effect outcome unknown", `{"outcomeUncertain":true,"beforeEffect":false}`
	running, event, err := ApplyTransition(intent, StatusRunning, "hai", "", "persist running intent", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	stored, err := repo.TransitionClaimed(ctx, second.Claim, running, event, false)
	if err != nil || stored == nil || stored.Status != string(StatusRunning) {
		t.Fatalf("healthy fenced transition: %+v / %v", stored, err)
	}
	expired := repo.claims[op.ID]
	expired.expiresAt = time.Now().Add(-time.Hour)
	repo.claims[op.ID] = expired
	recovered, err := repo.RecoverExpiredClaims(ctx, op.OwnerUserID, op.WorkspaceID, 1)
	if err != nil || recovered.Recovered != 1 || repo.ops[op.ID].Status != string(StatusInterrupted) || len(repo.events) != 2 ||
		repo.claims[op.ID].owner != uuid.Nil || repo.claims[op.ID].generation != second.Claim.Generation+1 {
		t.Fatalf("healthy recovery lost uncertainty/audit/generation: %+v / %v", recovered, err)
	}
}
