package operations

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Explicit contextual overrides keep injection on the decorator, not its base.
type contextIntakeProbe struct {
	*MemoryRepository
	want          context.Context
	lookups       int
	creates       int
	updates       int
	keys          []string
	find          func(string, string, string) (*models.Operation, bool, error)
	create        func(*models.Operation, *models.OperationEvent) (*models.Operation, error)
	update        func(*models.Operation, *models.OperationEvent) (*models.Operation, error)
	legacyCalls   int
	legacyFailure error
}

func (r *contextIntakeProbe) FindByDedupeKeyContext(ctx context.Context, owner, workspace, key string) (*models.Operation, bool, error) {
	r.lookups++
	r.keys = append(r.keys, key)
	if ctx != r.want {
		return nil, false, errors.New("lookup lost request context")
	}
	if r.find != nil {
		return r.find(owner, workspace, key)
	}
	return r.MemoryRepository.FindByDedupeKeyContext(ctx, owner, workspace, key)
}

func (r *contextIntakeProbe) CreateWithEventContext(ctx context.Context, op *models.Operation, event *models.OperationEvent) (*models.Operation, error) {
	r.creates++
	if ctx != r.want {
		return nil, errors.New("creation lost request context")
	}
	if r.create != nil {
		return r.create(op, event)
	}
	return r.MemoryRepository.CreateWithEventContext(ctx, op, event)
}

func (r *contextIntakeProbe) UpdateWithEventContext(ctx context.Context, op *models.Operation, event *models.OperationEvent) (*models.Operation, error) {
	r.updates++
	if ctx != r.want {
		return nil, errors.New("refresh lost request context")
	}
	if r.update != nil {
		return r.update(op, event)
	}
	return r.MemoryRepository.UpdateWithEventContext(ctx, op, event)
}

func (r *contextIntakeProbe) FindByDedupeKey(owner, workspace, key string) (*models.Operation, bool, error) {
	r.legacyCalls++
	return r.MemoryRepository.FindByDedupeKey(owner, workspace, key)
}

func (r *contextIntakeProbe) CreateWithEvent(op *models.Operation, event *models.OperationEvent) (*models.Operation, error) {
	r.legacyCalls++
	if r.legacyFailure != nil {
		return nil, r.legacyFailure
	}
	return r.MemoryRepository.CreateWithEvent(op, event)
}

func (r *contextIntakeProbe) UpdateWithEvent(op *models.Operation, event *models.OperationEvent) (*models.Operation, error) {
	r.legacyCalls++
	if r.legacyFailure != nil {
		return nil, r.legacyFailure
	}
	return r.MemoryRepository.UpdateWithEvent(op, event)
}

func TestIngestContextCancellationBoundaries(t *testing.T) {
	for _, boundary := range []string{"initial", "lookup_miss", "lookup_match", "read_only_match", "before_refresh_write", "before_race_lookup", "race_lookup", "before_race_refresh_write"} {
		t.Run(boundary, func(t *testing.T) {
			base := NewMemoryRepository()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			repo := &contextIntakeProbe{MemoryRepository: base, want: ctx}
			in := sampleInput()
			in.EvidenceJSON = `{"revision":2}`
			if boundary != "initial" && boundary != "lookup_miss" {
				if _, err := NewService(base).Ingest(sampleInput()); err != nil {
					t.Fatal(err)
				}
			}
			ops, events := ingestAtomicSnapshot(base)
			race := strings.Contains(boundary, "race")
			repo.find = func(owner, workspace, key string) (*models.Operation, bool, error) {
				if race && repo.lookups == 1 {
					return nil, false, nil
				}
				op, found, err := base.FindByDedupeKey(owner, workspace, key)
				if boundary == "lookup_miss" || boundary == "lookup_match" || boundary == "read_only_match" || boundary == "race_lookup" {
					cancel()
				}
				return op, found, err
			}
			if race {
				repo.create = func(*models.Operation, *models.OperationEvent) (*models.Operation, error) {
					if boundary == "before_race_lookup" {
						cancel()
					}
					return nil, ErrDuplicateDedupeKey
				}
			}
			svc := NewService(repo)
			clockCalls := 0
			svc.now = func() time.Time {
				clockCalls++
				if clockCalls == 2 && (boundary == "before_refresh_write" || boundary == "before_race_refresh_write") {
					cancel()
				}
				return time.Date(2026, 10, 1, 12, 0, clockCalls, 0, time.UTC)
			}
			if boundary == "initial" {
				cancel()
			} else if boundary == "read_only_match" {
				in.EvidenceJSON = "{}"
			}
			result, err := svc.IngestContext(ctx, in)
			if !errors.Is(err, context.Canceled) || result.Created || result.Operation.ID != uuid.Nil || repo.updates != 0 || repo.legacyCalls != 0 {
				t.Fatalf("cancellation boundary %s: result=%+v err=%v probe=%+v", boundary, result, err, repo)
			}
			wantLookups, wantCreates := 1, 0
			if boundary == "initial" {
				wantLookups = 0
			}
			if race {
				wantCreates = 1
				if boundary != "before_race_lookup" {
					wantLookups = 2
				}
			}
			if repo.lookups != wantLookups || repo.creates != wantCreates {
				t.Fatalf("unexpected cancellation dispatch: lookups=%d creates=%d", repo.lookups, repo.creates)
			}
			ingestAtomicAssertSnapshot(t, base, ops, events)
		})
	}
}

func TestIngestContextExplicitDecoratorDispatch(t *testing.T) {
	for _, path := range []string{"create", "refresh", "race_refresh"} {
		t.Run(path, func(t *testing.T) {
			base := NewMemoryRepository()
			ctx := context.Background()
			repo := &contextIntakeProbe{MemoryRepository: base, want: ctx}
			if path != "create" {
				if _, err := NewService(base).Ingest(sampleInput()); err != nil {
					t.Fatal(err)
				}
			}
			ops, events := ingestAtomicSnapshot(base)
			failure := errors.New("decorator audit failure")
			repo.create = func(*models.Operation, *models.OperationEvent) (*models.Operation, error) {
				if path == "race_refresh" {
					return nil, ErrDuplicateDedupeKey
				}
				return nil, failure
			}
			repo.update = func(*models.Operation, *models.OperationEvent) (*models.Operation, error) { return nil, failure }
			if path == "race_refresh" {
				repo.find = func(owner, workspace, key string) (*models.Operation, bool, error) {
					if repo.lookups == 1 {
						return nil, false, nil
					}
					return base.FindByDedupeKey(owner, workspace, key)
				}
			}
			in := sampleInput()
			in.EvidenceJSON = `{"revision":2}`
			result, err := NewService(repo).IngestContext(ctx, in)
			if !errors.Is(err, failure) || result.Created || result.Operation.ID != uuid.Nil || repo.legacyCalls != 0 {
				t.Fatalf("contextual decorator bypass: result=%+v err=%v probe=%+v", result, err, repo)
			}
			ingestAtomicAssertSnapshot(t, base, ops, events)
		})
	}
}

func TestIngestPreservesLegacyDecoratorDispatch(t *testing.T) {
	for _, path := range []string{"create", "refresh"} {
		t.Run(path, func(t *testing.T) {
			base := NewMemoryRepository()
			if path == "refresh" {
				if _, err := NewService(base).Ingest(sampleInput()); err != nil {
					t.Fatal(err)
				}
			}
			ops, events := ingestAtomicSnapshot(base)
			failure := errors.New("legacy decorator audit failure")
			repo := &contextIntakeProbe{MemoryRepository: base, legacyFailure: failure}
			in := sampleInput()
			in.EvidenceJSON = `{"revision":2}`
			_, err := NewService(repo).Ingest(in)
			if !errors.Is(err, failure) || repo.legacyCalls != 2 || repo.lookups != 0 || repo.creates != 0 || repo.updates != 0 {
				t.Fatalf("legacy decorator bypass: err=%v probe=%+v", err, repo)
			}
			ingestAtomicAssertSnapshot(t, base, ops, events)
		})
	}
}

func TestIngestContextRefusesLegacyStore(t *testing.T) {
	base := NewMemoryRepository()
	// Hide optional context methods, even though the backing store supports them.
	repo := &ingestAtomicLegacyProbe{Repository: base}
	for _, canceled := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		want := ErrContextIntakeUnsupported
		if canceled {
			cancel()
			want = context.Canceled
		}
		result, err := NewService(repo).IngestContext(ctx, sampleInput())
		cancel()
		if !errors.Is(err, want) || result.Created || result.Operation.ID != uuid.Nil || repo.lookups != 0 {
			t.Fatalf("unsupported context store accepted: result=%+v err=%v", result, err)
		}
		ingestAtomicAssertNoSplit(t, repo)
	}
}

func TestIngestContextMemoryAtomicWriteCancellation(t *testing.T) {
	for _, path := range []string{"create", "refresh"} {
		t.Run(path, func(t *testing.T) {
			base := NewMemoryRepository()
			if path == "refresh" {
				if _, err := NewService(base).Ingest(sampleInput()); err != nil {
					t.Fatal(err)
				}
			}
			ops, events := ingestAtomicSnapshot(base)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			repo := &contextIntakeProbe{MemoryRepository: base, want: ctx}
			repo.create = func(op *models.Operation, event *models.OperationEvent) (*models.Operation, error) {
				cancel()
				return base.CreateWithEventContext(ctx, op, event)
			}
			repo.update = func(op *models.Operation, event *models.OperationEvent) (*models.Operation, error) {
				cancel()
				return base.UpdateWithEventContext(ctx, op, event)
			}
			in := sampleInput()
			in.EvidenceJSON = `{"revision":2}`
			result, err := NewService(repo).IngestContext(ctx, in)
			if !errors.Is(err, context.Canceled) || result.Created || result.Operation.ID != uuid.Nil || repo.creates+repo.updates != 1 {
				t.Fatalf("repository write cancellation missed: result=%+v err=%v probe=%+v", result, err, repo)
			}
			ingestAtomicAssertSnapshot(t, base, ops, events)
		})
	}
}

func TestIngestContextCreatesAndRefreshesAtomicPairs(t *testing.T) {
	base := NewMemoryRepository()
	ctx := context.Background()
	repo := &contextIntakeProbe{MemoryRepository: base, want: ctx}
	svc := NewService(repo)
	in := sampleInput()
	in.LegacyDedupeKey = "missing-historical-key"
	created, err := svc.IngestContext(ctx, in)
	if err != nil || !created.Created || repo.lookups != 2 || repo.creates != 1 || repo.updates != 0 {
		t.Fatalf("contextual creation: result=%+v err=%v probe=%+v", created, err, repo)
	}
	in.EvidenceJSON = `{"revision":2}`
	refreshed, err := svc.IngestContext(ctx, in)
	if err != nil || refreshed.Created || refreshed.Operation.ID != created.Operation.ID ||
		refreshed.Operation.Version != created.Operation.Version+1 || refreshed.Operation.EvidenceJSON != in.EvidenceJSON ||
		repo.lookups != 3 || repo.creates != 1 || repo.updates != 1 || repo.legacyCalls != 0 {
		t.Fatalf("contextual refresh: result=%+v err=%v probe=%+v", refreshed, err, repo)
	}
	ops, events := ingestAtomicSnapshot(base)
	if len(ops) != 1 || len(events) != 2 || events[0].EventType != "created" ||
		events[1].EventType != "source_evidence_refreshed" || events[1].OperationID != created.Operation.ID {
		t.Fatalf("contextual intake split operation/audit rows: operations=%+v events=%+v", ops, events)
	}
}

func TestIngestSourceIdentityMigrationFence(t *testing.T) {
	for _, contextual := range []bool{false, true} {
		for _, canonicalExists := range []bool{false, true} {
			t.Run(fmt.Sprintf("context=%t/canonical=%t", contextual, canonicalExists), func(t *testing.T) {
				base := NewMemoryRepository()
				legacy, err := NewService(base).Ingest(sampleInput())
				if err != nil {
					t.Fatal(err)
				}
				// A rollout fence must leave approved historical records intact too.
				base.mu.Lock()
				old := base.ops[legacy.Operation.ID]
				old.Status = string(StatusApproved)
				base.ops[old.ID] = old
				base.mu.Unlock()
				in := sampleInput()
				in.DedupeKey = "feed:v2:provider:account:item:revision"
				var canonical IngestResult
				if canonicalExists {
					canonical, err = NewService(base).Ingest(in)
					if err != nil {
						t.Fatal(err)
					}
				}
				ops, events := ingestAtomicSnapshot(base)
				in.LegacyDedupeKey = sampleInput().DedupeKey
				in.EvidenceJSON = `{"revision":2}`
				in.OwnerUserID, in.WorkspaceID = " user-1 ", " local "
				ctx := context.Background()
				repo := &contextIntakeProbe{MemoryRepository: base, want: ctx}
				svc := NewService(repo)
				var result IngestResult
				if contextual {
					result, err = svc.IngestContext(ctx, in)
				} else {
					result, err = svc.Ingest(in)
				}
				if !canonicalExists {
					if !errors.Is(err, ErrSourceIdentityMigrationRequired) || result.Created || result.Operation.ID != uuid.Nil {
						t.Fatalf("legacy-only rollout accepted: result=%+v err=%v", result, err)
					}
					ingestAtomicAssertSnapshot(t, base, ops, events)
				} else {
					if err != nil || result.Created || result.Operation.ID != canonical.Operation.ID || result.Operation.EvidenceJSON != in.EvidenceJSON {
						t.Fatalf("canonical match did not win: result=%+v err=%v", result, err)
					}
					afterOps, afterEvents := ingestAtomicSnapshot(base)
					if !reflect.DeepEqual(afterOps[old.ID], ops[old.ID]) || len(afterOps) != 2 || len(afterEvents) != len(events)+1 ||
						afterEvents[len(afterEvents)-1].OperationID != canonical.Operation.ID {
						t.Fatal("canonical refresh changed historical rows or created a new operation")
					}
				}
				if contextual {
					wantKeys := []string{in.DedupeKey}
					if !canonicalExists {
						wantKeys = append(wantKeys, in.LegacyDedupeKey)
					}
					if !reflect.DeepEqual(repo.keys, wantKeys) || repo.legacyCalls != 0 || repo.creates != 0 {
						t.Fatalf("migration lookups escaped contextual dispatch: %+v", repo)
					}
				}
			})
		}
	}
}

func TestIngestContextLegacyLookupCancellation(t *testing.T) {
	base := NewMemoryRepository()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	repo := &contextIntakeProbe{MemoryRepository: base, want: ctx}
	in := sampleInput()
	in.LegacyDedupeKey = "historical-key"
	repo.find = func(owner, workspace, key string) (*models.Operation, bool, error) {
		if key == in.LegacyDedupeKey {
			cancel()
		}
		return nil, false, nil
	}
	result, err := NewService(repo).IngestContext(ctx, in)
	if !errors.Is(err, context.Canceled) || result.Created || repo.lookups != 2 || repo.creates != 0 || repo.updates != 0 {
		t.Fatalf("legacy lookup cancellation missed: result=%+v err=%v probe=%+v", result, err, repo)
	}
	ops, events := ingestAtomicSnapshot(base)
	if len(ops) != 0 || len(events) != 0 {
		t.Fatal("canceled legacy lookup persisted rows")
	}
}

// Wrap the existing synthetic SQL drivers to inspect request context at every
// lookup, transaction begin, lock, CAS and audit insert. No server is involved.
type contextIntakeSQL struct {
	want     context.Context
	creation *creationSQL
	mutation *mutationSQL
	lookups  int
	begins   int
	queries  int
	execs    int
}

type contextIntakeConnector struct{ state *contextIntakeSQL }
type contextIntakeConnection struct{ state *contextIntakeSQL }

func (c contextIntakeConnector) Connect(ctx context.Context) (driver.Conn, error) {
	if ctx != c.state.want {
		return nil, errors.New("connection lost request context")
	}
	return &contextIntakeConnection{state: c.state}, nil
}

func (contextIntakeConnector) Driver() driver.Driver { return creationDriver{} }
func (*contextIntakeConnection) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepared statement")
}
func (*contextIntakeConnection) Close() error { return nil }
func (*contextIntakeConnection) Begin() (driver.Tx, error) {
	return nil, errors.New("context-free transaction begin")
}

func (c *contextIntakeConnection) BeginTx(ctx context.Context, _ driver.TxOptions) (driver.Tx, error) {
	if ctx != c.state.want {
		return nil, errors.New("transaction lost request context")
	}
	c.state.begins++
	if c.state.creation != nil {
		return (&creationConnection{state: c.state.creation}).Begin()
	}
	return (&mutationConnection{state: c.state.mutation}).Begin()
}

func (c *contextIntakeConnection) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if ctx != c.state.want {
		return nil, errors.New("query lost request context")
	}
	c.state.queries++
	if strings.HasPrefix(query, `SELECT * FROM "operations"`) && !strings.Contains(query, "FOR UPDATE") {
		c.state.lookups++
		return &mutationRows{columns: []string{"id"}}, nil
	}
	if c.state.creation != nil {
		return (&creationConnection{state: c.state.creation}).QueryContext(ctx, query, args)
	}
	return (&mutationConnection{state: c.state.mutation}).QueryContext(ctx, query, args)
}

func (c *contextIntakeConnection) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if ctx != c.state.want {
		return nil, errors.New("CAS lost request context")
	}
	c.state.execs++
	if c.state.mutation == nil {
		return nil, errors.New("unexpected creation exec")
	}
	return (&mutationConnection{state: c.state.mutation}).ExecContext(ctx, query, args)
}

func TestGormContextIntakeCompositionWithoutServer(t *testing.T) {
	for _, path := range []string{"create", "refresh"} {
		t.Run(path, func(t *testing.T) {
			op, event := creationTransactionPair(t)
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			state := &contextIntakeSQL{want: ctx}
			if path == "create" {
				state.creation = &creationSQL{operationID: op.ID}
			} else {
				state.mutation = &mutationSQL{op: op, casRows: 1}
			}
			sqlDB := sql.OpenDB(contextIntakeConnector{state: state})
			t.Cleanup(func() { _ = sqlDB.Close() })
			db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB, PreferSimpleProtocol: true}),
				&gorm.Config{DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent)})
			if err != nil {
				t.Fatal(err)
			}
			repo := NewGormRepository(db)
			if _, found, err := repo.FindByDedupeKeyContext(ctx, op.OwnerUserID, op.WorkspaceID, op.DedupeKey); err != nil || found {
				t.Fatalf("context-bound lookup: found=%t err=%v", found, err)
			}
			var saved *models.Operation
			if path == "create" {
				saved, err = repo.CreateWithEventContext(ctx, &op, &event)
			} else {
				op.Version++
				op.EvidenceJSON = `{"revision":2}`
				event.EventType = "source_evidence_refreshed"
				saved, err = repo.UpdateWithEventContext(ctx, &op, &event)
			}
			if err != nil || saved == nil || state.lookups != 1 || state.begins != 1 || db.Statement.Context == ctx {
				t.Fatalf("context composition mutated base or lost a step: saved=%+v err=%v state=%+v", saved, err, state)
			}
			if path == "create" {
				if state.queries != 3 || state.execs != 0 || state.creation.commits != 1 || state.creation.events != 1 {
					t.Fatalf("creation context wiring incomplete: %+v", state)
				}
			} else if state.queries != 4 || state.execs != 1 || state.mutation.commits != 1 || state.mutation.events != 1 {
				t.Fatalf("refresh context wiring incomplete: %+v", state)
			}
		})
	}
}
