package operations

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
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

type mutationCall func(*Service, models.Operation) (*models.Operation, error)

func mutationCalls() map[string]mutationCall {
	return map[string]mutationCall{
		"transition": func(s *Service, op models.Operation) (*models.Operation, error) {
			op.ResultSummary = "before-effect outcome uncertain; no automatic replay"
			op.WorldModelStateJSON = `{"outcomeUncertain":true,"beforeEffect":false}`
			return s.Transition(op, StatusRunning, "hai", "worker", "dispatch intent")
		},
		"save": func(s *Service, op models.Operation) (*models.Operation, error) {
			op.ResultSummary = "before-effect outcome uncertain; no automatic replay"
			op.WorldModelStateJSON = `{"outcomeUncertain":true,"beforeEffect":false}`
			return s.Save(op, "dispatch_intent", "hai", "dispatch intent")
		},
	}
}

func mutationSeed(t *testing.T, repo *MemoryRepository) models.Operation {
	t.Helper()
	op, err := NewOperation(sampleInput(), time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	op.Status = string(StatusReady)
	op.CurrentDecision = string(DecisionRunSafeLocalWorker)
	op.AutonomyLevel = string(AutonomyAuto)
	created, err := repo.Create(&op)
	if err != nil {
		t.Fatal(err)
	}
	return *created
}

func assertMutationUnchanged(t *testing.T, repo *MemoryRepository, before models.Operation, eventCount int) {
	t.Helper()
	stored, err := repo.GetByID(before.OwnerUserID, before.WorkspaceID, before.ID)
	if err != nil || !reflect.DeepEqual(stored, &before) {
		t.Fatalf("operation changed on failure: stored=%+v err=%v", stored, err)
	}
	events, err := repo.ListEvents(before.ID, 0)
	if err != nil || len(events) != eventCount {
		t.Fatalf("unexpected audit trail after refusal: events=%+v err=%v", events, err)
	}
}

func TestAtomicMutationServiceMemorySuccess(t *testing.T) {
	for name, call := range mutationCalls() {
		t.Run(name, func(t *testing.T) {
			repo := NewMemoryRepository()
			op := mutationSeed(t, repo)
			svc := NewService(repo)
			now := op.UpdatedAt.Add(time.Minute)
			svc.now = func() time.Time { return now }
			saved, err := call(svc, op)
			if err != nil || saved == nil {
				t.Fatalf("atomic mutation: saved=%+v err=%v", saved, err)
			}
			if saved.Version != op.Version+1 || !saved.UpdatedAt.Equal(now) ||
				saved.ResultSummary != "before-effect outcome uncertain; no automatic replay" ||
				saved.WorldModelStateJSON != `{"outcomeUncertain":true,"beforeEffect":false}` {
				t.Fatalf("mutation fields/version lost: %+v", saved)
			}
			stored, err := repo.GetByID(op.OwnerUserID, op.WorkspaceID, op.ID)
			if err != nil || !reflect.DeepEqual(stored, saved) {
				t.Fatalf("persisted operation mismatch: %+v err=%v", stored, err)
			}
			events, err := repo.ListEvents(op.ID, 0)
			if err != nil || len(events) != 1 {
				t.Fatalf("expected exactly one audit row: %+v err=%v", events, err)
			}
			event := events[0]
			if event.ID == uuid.Nil || event.OperationID != saved.ID || event.AfterStatus != saved.Status ||
				event.ActorType != "hai" || event.Message != "dispatch intent" || !event.CreatedAt.Equal(now) {
				t.Fatalf("audit fields mismatch: %+v", event)
			}
			if name == "transition" && (event.BeforeStatus != op.Status || event.EventType != "status_change" || event.ActorID != "worker") {
				t.Fatalf("transition audit fields mismatch: %+v", event)
			}
			if name == "save" && event.EventType != "dispatch_intent" {
				t.Fatalf("save audit fields mismatch: %+v", event)
			}
		})
	}
}

func TestAtomicMutationServiceStaleRetryPreservesWinningEvent(t *testing.T) {
	for name, call := range mutationCalls() {
		t.Run(name, func(t *testing.T) {
			repo := NewMemoryRepository()
			op := mutationSeed(t, repo)
			svc := NewService(repo)
			winner, err := call(svc, op)
			if err != nil || winner == nil {
				t.Fatalf("first mutation: saved=%+v err=%v", winner, err)
			}
			beforeEvents, err := repo.ListEvents(op.ID, 0)
			if err != nil || len(beforeEvents) != 1 {
				t.Fatalf("winning audit: events=%+v err=%v", beforeEvents, err)
			}
			saved, err := call(svc, op)
			if saved != nil || !errors.Is(err, ErrStaleOperation) {
				t.Fatalf("stale retry accepted: saved=%+v err=%v", saved, err)
			}
			assertMutationUnchanged(t, repo, *winner, 1)
			afterEvents, err := repo.ListEvents(op.ID, 0)
			if err != nil || !reflect.DeepEqual(afterEvents, beforeEvents) {
				t.Fatalf("stale retry altered winning audit: events=%+v err=%v", afterEvents, err)
			}
		})
	}
}

// Source-review red regression: a current version must not authenticate a
// caller-invented before-status. The parent owns the production fix and run.
func TestAtomicMutationTransitionRejectsForgedBeforeStatus(t *testing.T) {
	repo := NewMemoryRepository()
	before := mutationSeed(t, repo)
	forged := before
	forged.Status = string(StatusVerifying)
	forged.VerificationStatus = string(VerificationNotRequired)
	saved, err := NewService(repo).Transition(forged, StatusCompleted, "hai", "worker", "forged before-status")
	if saved != nil || err == nil {
		t.Fatalf("ready operation completed through forged verifying snapshot: saved=%+v err=%v", saved, err)
	}
	assertMutationUnchanged(t, repo, before, 0)
}

// Embedding only Repository deliberately hides optional atomic capabilities.
type mutationLegacySpy struct {
	Repository
	updates int
	appends int
}

func (r *mutationLegacySpy) Update(op *models.Operation) (*models.Operation, error) {
	r.updates++
	return r.Repository.Update(op)
}

func (r *mutationLegacySpy) AppendEvent(event *models.OperationEvent) error {
	r.appends++
	return r.Repository.AppendEvent(event)
}

type mutationAuditFailure struct {
	*mutationLegacySpy
	atomic  AtomicMutationRepository
	err     error
	eventID uuid.UUID
	calls   int
}

func (r *mutationAuditFailure) UpdateWithEvent(op *models.Operation, event *models.OperationEvent) (*models.Operation, error) {
	r.calls++
	if r.err != nil {
		return nil, r.err
	}
	audit := *event
	audit.ID = r.eventID
	return r.atomic.UpdateWithEvent(op, &audit)
}

func TestAtomicMutationServiceRefusesUnsupportedWithoutWrites(t *testing.T) {
	for name, call := range mutationCalls() {
		t.Run(name, func(t *testing.T) {
			base := NewMemoryRepository()
			op := mutationSeed(t, base)
			repo := &mutationLegacySpy{Repository: base}
			saved, err := call(NewService(repo), op)
			if saved != nil || !errors.Is(err, ErrAtomicMutationsUnsupported) {
				t.Fatalf("expected unsupported refusal: saved=%+v err=%v", saved, err)
			}
			if repo.updates != 0 || repo.appends != 0 {
				t.Fatalf("unsafe fallback writes: updates=%d appends=%d", repo.updates, repo.appends)
			}
			assertMutationUnchanged(t, base, op, 0)
		})
	}
}

func TestAtomicMutationServiceAuditFailureDoesNotMoveOperation(t *testing.T) {
	for name, call := range mutationCalls() {
		for _, failure := range []string{"repository_error", "duplicate_event"} {
			t.Run(name+"/"+failure, func(t *testing.T) {
				base := NewMemoryRepository()
				op := mutationSeed(t, base)
				repo := &mutationAuditFailure{mutationLegacySpy: &mutationLegacySpy{Repository: base}, atomic: base}
				eventCount := 0
				if failure == "repository_error" {
					repo.err = errors.New("audit storage unavailable")
				} else {
					repo.eventID = uuid.New()
					if err := base.AppendEvent(&models.OperationEvent{ID: repo.eventID, OperationID: op.ID}); err != nil {
						t.Fatal(err)
					}
					eventCount = 1
				}
				saved, err := call(NewService(repo), op)
				if saved != nil || err == nil || (repo.err != nil && !errors.Is(err, repo.err)) {
					t.Fatalf("audit failure not propagated: saved=%+v err=%v", saved, err)
				}
				if repo.calls != 1 || repo.updates != 0 || repo.appends != 0 {
					t.Fatalf("unexpected persistence path: %+v", repo)
				}
				assertMutationUnchanged(t, base, op, eventCount)
			})
		}
	}
}

func TestAtomicMutationMemoryGuards(t *testing.T) {
	for _, guard := range []string{"stale", "owner", "workspace", "claimed", "expired_claim", "nil_event", "wrong_operation", "wrong_status"} {
		t.Run(guard, func(t *testing.T) {
			repo := NewMemoryRepository()
			before := mutationSeed(t, repo)
			op := before
			op.Version++
			op.ResultSummary = "must not persist"
			event := &models.OperationEvent{OperationID: op.ID, AfterStatus: op.Status}
			var want error
			switch guard {
			case "stale":
				op.Version++
				want = ErrStaleOperation
			case "owner":
				op.OwnerUserID = "other-owner"
			case "workspace":
				op.WorkspaceID = "other-workspace"
			case "claimed", "expired_claim":
				if _, err := repo.ClaimOperation(context.Background(), op.OwnerUserID, op.WorkspaceID, op.ID, uuid.New(), time.Minute); err != nil {
					t.Fatal(err)
				}
				if guard == "expired_claim" {
					repo.mu.Lock()
					claim := repo.claims[op.ID]
					claim.expiresAt = time.Unix(0, 0).UTC()
					repo.claims[op.ID] = claim
					repo.mu.Unlock()
				}
				want = ErrOperationClaimed
			case "nil_event":
				event = nil
				want = ErrInvalidMutationEvent
			case "wrong_operation":
				event.OperationID = uuid.New()
				want = ErrInvalidMutationEvent
			case "wrong_status":
				event.AfterStatus = string(StatusCompleted)
				want = ErrInvalidMutationEvent
			}
			saved, err := repo.UpdateWithEvent(&op, event)
			if saved != nil || err == nil || (want != nil && !errors.Is(err, want)) {
				t.Fatalf("guard bypassed: saved=%+v err=%v want=%v", saved, err, want)
			}
			assertMutationUnchanged(t, repo, before, 0)
		})
	}
}

type mutationClaimsOnly struct {
	Repository
	ClaimRepository
}

func TestAtomicMutationClaimedPathStillUsesClaimRepository(t *testing.T) {
	base := NewMemoryRepository()
	op := mutationSeed(t, base)
	claimed, err := base.ClaimOperation(context.Background(), op.OwnerUserID, op.WorkspaceID, op.ID, uuid.New(), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	svc := NewService(&mutationClaimsOnly{Repository: base, ClaimRepository: base})
	saved, err := svc.TransitionClaimed(context.Background(), claimed.Claim, claimed.Operation, StatusRunning, "hai", "", "dispatch intent")
	if err != nil || saved == nil || saved.Status != string(StatusRunning) || saved.Version != op.Version+1 {
		t.Fatalf("claim path regressed: saved=%+v err=%v", saved, err)
	}
	if _, err := NewService(base).Save(*saved, "unfenced", "hai", "must refuse"); !errors.Is(err, ErrOperationClaimed) {
		t.Fatalf("unfenced save bypassed claim: %v", err)
	}
	if _, err := NewService(base).Transition(*saved, StatusVerifying, "hai", "", "must refuse"); !errors.Is(err, ErrOperationClaimed) {
		t.Fatalf("unfenced transition bypassed claim: %v", err)
	}
	assertMutationUnchanged(t, base, *saved, 1)
}

// This SQL driver checks Gorm transaction wiring without a server or new test
// dependency. It is not evidence of PostgreSQL locking or durability acceptance.
type mutationSQL struct {
	op         models.Operation
	opErr      error
	claimErr   error
	claimRow   bool
	claimOwner driver.Value
	casErr     error
	eventErr   error
	commitErr  error
	casRows    int64
	active     bool
	writes     int
	events     int
	commits    int
	rollbacks  int
}

type mutationConnector struct{ state *mutationSQL }
type mutationDriver struct{}
type mutationConnection struct{ state *mutationSQL }
type mutationTransaction struct{ state *mutationSQL }
type mutationRows struct {
	columns []string
	values  [][]driver.Value
}

func (c mutationConnector) Connect(context.Context) (driver.Conn, error) {
	return &mutationConnection{state: c.state}, nil
}

func (mutationConnector) Driver() driver.Driver { return mutationDriver{} }
func (mutationDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("use mutationConnector")
}

func (*mutationConnection) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepared statement")
}

func (*mutationConnection) Close() error { return nil }
func (c *mutationConnection) Begin() (driver.Tx, error) {
	if c.state.active {
		return nil, errors.New("unexpected nested transaction")
	}
	c.state.active = true
	return &mutationTransaction{state: c.state}, nil
}

func (c *mutationConnection) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	_, where, hasWhere := strings.Cut(query, " WHERE ")
	if !c.state.active || !strings.HasPrefix(query, `UPDATE "operations"`) ||
		!hasWhere || !strings.HasPrefix(where, "id =") || !strings.Contains(where, "owner_user_id =") ||
		!strings.Contains(where, "workspace_id =") || !strings.Contains(where, "version =") || len(args) < 4 {
		return nil, fmt.Errorf("unexpected or unfenced SQL: %s", query)
	}
	guards := args[len(args)-4:]
	op := c.state.op
	if fmt.Sprint(guards[0].Value) != op.ID.String() || guards[1].Value != op.OwnerUserID ||
		guards[2].Value != op.WorkspaceID || guards[3].Value != op.Version {
		return nil, fmt.Errorf("incorrect CAS identity/version arguments: %+v", guards)
	}
	c.state.writes++
	if c.state.casErr != nil {
		return nil, c.state.casErr
	}
	return driver.RowsAffected(c.state.casRows), nil
}

func (c *mutationConnection) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	if !c.state.active {
		return nil, errors.New("SQL outside transaction")
	}
	switch {
	case strings.HasPrefix(query, `SELECT * FROM "operations"`) && strings.Contains(query, "FOR UPDATE"):
		if c.state.opErr != nil {
			return nil, c.state.opErr
		}
		op := c.state.op
		return &mutationRows{columns: []string{"id", "owner_user_id", "workspace_id", "status", "version", "created_at"},
			values: [][]driver.Value{{op.ID.String(), op.OwnerUserID, op.WorkspaceID, op.Status, op.Version, op.CreatedAt}}}, nil
	case strings.Contains(query, "SELECT claim_owner::text") && strings.Contains(query, "FOR UPDATE"):
		if c.state.claimErr != nil {
			return nil, c.state.claimErr
		}
		if c.state.claimRow {
			return &mutationRows{columns: []string{"claim_owner"}, values: [][]driver.Value{{c.state.claimOwner}}}, nil
		}
		return &mutationRows{columns: []string{"claim_owner"}}, nil
	case strings.HasPrefix(query, `INSERT INTO "operation_events"`):
		if c.state.writes != 1 || c.state.casRows != 1 {
			return nil, errors.New("audit insert without successful CAS")
		}
		c.state.events++
		if c.state.eventErr != nil {
			return nil, c.state.eventErr
		}
		return &mutationRows{columns: []string{"id"}, values: [][]driver.Value{{uuid.New().String()}}}, nil
	default:
		return nil, fmt.Errorf("unexpected SQL: %s", query)
	}
}

func (tx *mutationTransaction) Commit() error {
	tx.state.active = false
	tx.state.commits++
	return tx.state.commitErr
}

func (tx *mutationTransaction) Rollback() error {
	tx.state.active = false
	tx.state.rollbacks++
	return nil
}

func (r *mutationRows) Columns() []string { return r.columns }
func (*mutationRows) Close() error        { return nil }
func (r *mutationRows) Next(dest []driver.Value) error {
	if len(r.values) == 0 {
		return io.EOF
	}
	copy(dest, r.values[0])
	r.values = r.values[1:]
	return nil
}

func TestAtomicMutationGormTransactionWiring(t *testing.T) {
	for _, outcome := range []string{"success", "null_claim_owner", "event_failure", "cas_conflict", "stale_snapshot", "owner", "workspace", "operation_lock_failure", "claimed", "claim_lookup_failure", "cas_failure", "commit_failure"} {
		t.Run(outcome, func(t *testing.T) {
			op := mutationSeed(t, NewMemoryRepository())
			state := &mutationSQL{op: op, casRows: 1}
			var want error
			identityRefusal := false
			switch outcome {
			case "null_claim_owner":
				state.claimRow = true
			case "event_failure":
				state.eventErr = errors.New("audit insert rejected")
				want = state.eventErr
			case "cas_conflict":
				state.casRows = 0
				want = ErrStaleOperation
			case "stale_snapshot":
				state.op.Version++
				want = ErrStaleOperation
			case "owner":
				op.OwnerUserID = "other-owner"
				identityRefusal = true
			case "workspace":
				op.WorkspaceID = "other-workspace"
				identityRefusal = true
			case "operation_lock_failure":
				state.opErr = errors.New("operation lock unavailable")
				want = state.opErr
			case "claimed":
				state.claimRow = true
				state.claimOwner = uuid.New().String()
				want = ErrOperationClaimed
			case "claim_lookup_failure":
				state.claimErr = errors.New("claim lookup unavailable")
				want = state.claimErr
			case "cas_failure":
				state.casErr = errors.New("operation update rejected")
				want = state.casErr
			case "commit_failure":
				state.commitErr = errors.New("commit outcome unknown")
				want = state.commitErr
			}
			sqlDB := sql.OpenDB(mutationConnector{state: state})
			t.Cleanup(func() { _ = sqlDB.Close() })
			db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB, PreferSimpleProtocol: true}),
				&gorm.Config{DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent)})
			if err != nil {
				t.Fatal(err)
			}
			op.Version++
			op.Status = string(StatusRunning)
			event := &models.OperationEvent{OperationID: op.ID, EventType: "status_change", BeforeStatus: state.op.Status, AfterStatus: op.Status, PayloadJSON: "{}"}
			beforeEvent := *event
			saved, err := NewGormRepository(db).UpdateWithEvent(&op, event)
			if outcome == "commit_failure" {
				// database/sql ends the transaction on Commit, even on error.
				// Counters show attempts, not durable rows or a known rollback.
				if saved != nil || !errors.Is(err, want) || state.commits != 1 || state.rollbacks != 0 || state.writes != 1 || state.events != 1 {
					t.Fatalf("commit error wiring: saved=%+v err=%v state=%+v", saved, err, state)
				}
			} else if identityRefusal {
				if saved != nil || err == nil || state.commits != 0 || state.rollbacks != 1 {
					t.Fatalf("identity guard wiring: saved=%+v err=%v state=%+v", saved, err, state)
				}
			} else if want == nil {
				if err != nil || saved == nil || state.commits != 1 || state.rollbacks != 0 || state.writes != 1 || state.events != 1 {
					t.Fatalf("success wiring: saved=%+v err=%v state=%+v", saved, err, state)
				}
			} else if saved != nil || !errors.Is(err, want) || state.commits != 0 || state.rollbacks != 1 {
				t.Fatalf("rollback wiring: saved=%+v err=%v want=%v state=%+v", saved, err, want, state)
			}
			if (outcome == "cas_conflict" || outcome == "cas_failure") && (state.writes != 1 || state.events != 0) {
				t.Fatal("CAS refusal missed update attempt or appended an event")
			}
			if (outcome == "stale_snapshot" || identityRefusal || outcome == "operation_lock_failure" || outcome == "claimed" || outcome == "claim_lookup_failure") &&
				(state.writes != 0 || state.events != 0) {
				t.Fatal("pre-write refusal attempted writes")
			}
			if state.active || !reflect.DeepEqual(*event, beforeEvent) {
				t.Fatal("transaction remained active or caller audit input was mutated")
			}
		})
	}
}
