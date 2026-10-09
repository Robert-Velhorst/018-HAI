package operations

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"automation-hub-backend/internal/models"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func rejectedIntakeContext(kind string) (context.Context, error) {
	switch kind {
	case "nil":
		return nil, ErrInvalidSourceObservation
	case "delayed_expiry":
		return delayedEffectDeadlineContext{context.Background(), time.Now().Add(-time.Hour)}, context.DeadlineExceeded
	case "ended_observation", "canceled_original", "expired_original":
		authority := context.Background()
		if kind == "canceled_original" {
			var cancel context.CancelFunc
			authority, cancel = context.WithCancel(authority)
			cancel()
		} else if kind == "expired_original" {
			authority = delayedEffectDeadlineContext{authority, time.Now().Add(-time.Hour)}
		}
		state := &activeSourceObservation{authority: authority}
		state.active.Store(kind != "ended_observation")
		return context.WithoutCancel(context.WithValue(authority, sourceObservationContextKey{}, state)), ErrInvalidSourceObservation
	default:
		panic("unknown intake context fixture")
	}
}

func TestContextualIntakeRefusesMissingOrEndedAuthorityBeforeStoreAccess(t *testing.T) {
	for _, storage := range []string{"memory", "gorm"} {
		for _, kind := range []string{"nil", "delayed_expiry", "ended_observation", "canceled_original", "expired_original"} {
			for _, path := range []string{"lookup", "create", "update"} {
				t.Run(storage+"/"+kind+"/"+path, func(t *testing.T) {
					ctx, want := rejectedIntakeContext(kind)
					op, event := creationTransactionPair(t)
					base := NewMemoryRepository()
					initial, err := NewService(base).Ingest(sampleInput())
					if err != nil || !initial.Created {
						t.Fatalf("healthy existing memory operation: %+v / %v", initial, err)
					}
					beforeOps, beforeEvents := ingestAtomicSnapshot(base)
					var repo ContextIntakeRepository = base
					var state *contextIntakeSQL
					if storage == "gorm" {
						state = &contextIntakeSQL{want: ctx, creation: &creationSQL{operationID: op.ID}}
						sqlDB := sql.OpenDB(contextIntakeConnector{state: state})
						t.Cleanup(func() { _ = sqlDB.Close() })
						db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB, PreferSimpleProtocol: true}),
							&gorm.Config{DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent)})
						if err != nil {
							t.Fatal(err)
						}
						repo = NewGormRepository(db)
					}
					var saved *models.Operation
					found := false
					switch path {
					case "lookup":
						saved, found, err = repo.FindByDedupeKeyContext(ctx, op.OwnerUserID, op.WorkspaceID, op.DedupeKey)
					case "create":
						saved, err = repo.CreateWithEventContext(ctx, &op, &event)
					case "update":
						updated := initial.Operation
						updated.Version++
						updated.EvidenceJSON = `{"new":"evidence"}`
						event.OperationID = updated.ID
						event.EventType = "source_evidence_refreshed"
						event.AfterStatus = updated.Status
						saved, err = repo.UpdateWithEventContext(ctx, &updated, &event)
					}
					if !errors.Is(err, want) || saved != nil || found {
						t.Fatalf("ended authority accepted: saved=%+v found=%t error=%v want=%v", saved, found, err, want)
					}
					ingestAtomicAssertSnapshot(t, base, beforeOps, beforeEvents)
					if state != nil && (state.lookups != 0 || state.begins != 0 || state.queries != 0 || state.execs != 0) {
						t.Fatalf("invalid authority accessed SQL: %+v", state)
					}
				})
			}
		}
	}
}

func TestIngestContextDoesNotReplaceMissingAuthorityWithBackground(t *testing.T) {
	for _, kind := range []string{"nil", "delayed_expiry", "ended_observation", "canceled_original", "expired_original"} {
		t.Run(kind, func(t *testing.T) {
			ctx, want := rejectedIntakeContext(kind)
			base := NewMemoryRepository()
			repo := &contextIntakeProbe{MemoryRepository: base, want: ctx}
			service := NewService(repo)
			service.now = func() time.Time { t.Error("expired intake consulted clock"); return time.Now() }
			result, err := service.IngestContext(ctx, sampleInput())
			if !errors.Is(err, want) || result.Created || result.Operation.ID != uuid.Nil || repo.lookups+repo.creates+repo.updates+repo.legacyCalls != 0 {
				t.Fatalf("expired service accessed storage: result=%+v error=%v probe=%+v", result, err, repo)
			}
			if len(base.ops) != 0 || len(base.events) != 0 {
				t.Fatal("expired service persisted operation or audit")
			}
		})
	}
}

// Deadline changes only synchronously at the probe, modeling delayed timer
// delivery while a lookup returns normally. No race with a timer/goroutine.
type intakeAdvancingDeadline struct {
	context.Context
	deadline time.Time
}

func (c *intakeAdvancingDeadline) Deadline() (time.Time, bool) { return c.deadline, true }

func TestIngestContextRechecksAbsoluteDeadlineAfterLookup(t *testing.T) {
	for _, path := range []string{"new", "evidence_refresh", "read_only_duplicate"} {
		t.Run(path, func(t *testing.T) {
			base := NewMemoryRepository()
			if path != "new" {
				if _, err := NewService(base).Ingest(sampleInput()); err != nil {
					t.Fatal(err)
				}
			}
			beforeOps, beforeEvents := ingestAtomicSnapshot(base)
			ctx := &intakeAdvancingDeadline{Context: context.Background(), deadline: time.Now().Add(time.Hour)}
			repo := &contextIntakeProbe{MemoryRepository: base, want: ctx}
			repo.find = func(owner, workspace, key string) (*models.Operation, bool, error) {
				op, found, err := base.FindByDedupeKey(owner, workspace, key)
				ctx.deadline = time.Now().Add(-time.Hour)
				return op, found, err
			}
			in := sampleInput()
			if path == "evidence_refresh" {
				in.EvidenceJSON = `{"revision":2}`
			}
			result, err := NewService(repo).IngestContext(ctx, in)
			if !errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil || result.Created || result.Operation.ID != uuid.Nil ||
				repo.lookups != 1 || repo.creates+repo.updates+repo.legacyCalls != 0 {
				t.Fatalf("late timer extended service authority: result=%+v error=%v probe=%+v", result, err, repo)
			}
			ingestAtomicAssertSnapshot(t, base, beforeOps, beforeEvents)
		})
	}
}

func TestGormContextualIntakeRefusesAbsentStoreWithoutPanic(t *testing.T) {
	for _, repo := range []*GormRepository{nil, {}} {
		ctx := context.Background()
		if op, found, err := repo.FindByDedupeKeyContext(ctx, "owner", "local", "key"); op != nil || found || !errors.Is(err, ErrContextIntakeUnsupported) {
			t.Fatalf("absent lookup store: %+v %t %v", op, found, err)
		}
		if op, err := repo.CreateWithEventContext(ctx, nil, nil); op != nil || !errors.Is(err, ErrContextIntakeUnsupported) {
			t.Fatalf("absent create store: %+v %v", op, err)
		}
		if op, err := repo.UpdateWithEventContext(ctx, nil, nil); op != nil || !errors.Is(err, ErrContextIntakeUnsupported) {
			t.Fatalf("absent update store: %+v %v", op, err)
		}
	}
}

type intakeDeadlineConnector struct {
	state *contextIntakeSQL
	ctx   *intakeAdvancingDeadline
}

func (c intakeDeadlineConnector) Driver() driver.Driver { return creationDriver{} }
func (c intakeDeadlineConnector) Connect(ctx context.Context) (driver.Conn, error) {
	if ctx != c.ctx {
		return nil, errors.New("lookup connected without original context")
	}
	return &intakeDeadlineConnection{contextIntakeConnection: &contextIntakeConnection{state: c.state}, ctx: c.ctx}, nil
}

type intakeDeadlineConnection struct {
	*contextIntakeConnection
	ctx *intakeAdvancingDeadline
}

func (c *intakeDeadlineConnection) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	rows, err := c.contextIntakeConnection.QueryContext(ctx, query, args)
	if err == nil && strings.HasPrefix(query, `SELECT * FROM "operations"`) && !strings.Contains(query, "FOR UPDATE") {
		c.ctx.deadline = time.Now().Add(-time.Hour)
	}
	return rows, err
}

func TestGormContextualLookupRefusesExpiredResultWithDelayedTimer(t *testing.T) {
	ctx := &intakeAdvancingDeadline{Context: context.Background(), deadline: time.Now().Add(time.Hour)}
	state := &contextIntakeSQL{want: ctx}
	sqlDB := sql.OpenDB(intakeDeadlineConnector{state: state, ctx: ctx})
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB, PreferSimpleProtocol: true}),
		&gorm.Config{DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	op, found, err := NewGormRepository(db).FindByDedupeKeyContext(ctx, "owner", "local", "key")
	if !errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil || op != nil || found ||
		state.lookups != 1 || state.queries != 1 || state.begins != 0 || state.execs != 0 {
		t.Fatalf("expired SQL lookup returned success: %+v %t %v state=%+v", op, found, err, state)
	}
}

type intakeLockArrivalContext struct {
	context.Context
	reads   atomic.Int32
	arrived chan struct{}
}

func (c *intakeLockArrivalContext) Value(key any) any {
	value := c.Context.Value(key)
	if _, ok := key.(sourceObservationContextKey); ok && c.reads.Add(1) == 6 {
		close(c.arrived)
	}
	return value
}

func TestMemoryDetachedContextStopsWaitingWhenOriginalAuthorityEnds(t *testing.T) {
	for _, ending := range []string{"server_cancel", "callback_return"} {
		t.Run(ending, func(t *testing.T) {
			base := NewMemoryRepository()
			observation, err := base.BeginSourceObservation(context.Background(), sourceObservationTestStart())
			if err != nil {
				t.Fatal(err)
			}
			authority, cancel := context.WithCancel(context.Background())
			defer cancel()
			state := &activeSourceObservation{observation: observation, authority: authority}
			state.active.Store(true)
			ctx := &intakeLockArrivalContext{
				Context: context.WithoutCancel(context.WithValue(authority, sourceObservationContextKey{}, state)),
				arrived: make(chan struct{}),
			}
			base.mu.Lock()
			locked := true
			defer func() {
				if locked {
					base.mu.Unlock()
				}
			}()
			done := make(chan error, 1)
			drainAfterUnlock := func() {
				base.mu.Unlock()
				locked = false
				select {
				case <-done:
				case <-time.After(2 * time.Second):
					t.Error("lookup did not finish during bounded cleanup")
				}
			}
			go func() {
				op, found, err := base.FindByDedupeKeyContext(ctx, observation.OwnerUserID, observation.WorkspaceID, "key")
				if op != nil || found {
					err = errors.Join(err, errors.New("ended lookup returned a record"))
				}
				done <- err
			}()
			select {
			case <-ctx.arrived:
			case <-time.After(2 * time.Second):
				drainAfterUnlock()
				t.Fatal("lookup did not reach the real held-lock wait")
			}
			if ending == "server_cancel" {
				cancel()
			} else {
				state.active.Store(false)
			}
			select {
			case err := <-done:
				if !errors.Is(err, ErrInvalidSourceObservation) || ctx.Err() != nil {
					t.Fatalf("detached lock wait extended original authority: %v", err)
				}
			case <-time.After(2 * time.Second):
				drainAfterUnlock()
				t.Fatal("ended authority did not release the lock waiter before mutex unlock")
			}
			if len(base.ops) != 0 || len(base.events) != 0 || len(base.observations) != 1 {
				t.Fatal("refused lock waiter mutated ledger or observation history")
			}
		})
	}
}
