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
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// The synthetic driver checks transaction wiring, not PostgreSQL durability.
type creationSQL struct {
	operationID                                    uuid.UUID
	active                                         bool
	begins, operations, events, commits, rollbacks int
	operationErr, eventErr, commitErr              error
}

type creationConnector struct{ state *creationSQL }
type creationDriver struct{}
type creationConnection struct{ state *creationSQL }
type creationTransaction struct{ state *creationSQL }

func (c creationConnector) Connect(context.Context) (driver.Conn, error) {
	return &creationConnection{state: c.state}, nil
}
func (creationConnector) Driver() driver.Driver { return creationDriver{} }
func (creationDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("use creationConnector")
}
func (*creationConnection) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepared statement")
}
func (*creationConnection) Close() error { return nil }
func (c *creationConnection) Begin() (driver.Tx, error) {
	if c.state.active {
		return nil, errors.New("unexpected nested transaction")
	}
	c.state.active = true
	c.state.begins++
	return &creationTransaction{state: c.state}, nil
}
func (c *creationConnection) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if !c.state.active {
		return nil, errors.New("creation SQL outside transaction")
	}
	boundID := false
	for _, arg := range args {
		if fmt.Sprint(arg.Value) == c.state.operationID.String() {
			boundID = true
		}
	}
	if !boundID {
		return nil, errors.New("creation insert not bound to the expected operation")
	}
	switch {
	case strings.HasPrefix(query, `INSERT INTO "operations"`):
		if c.state.operations != 0 || c.state.events != 0 {
			return nil, errors.New("duplicate or out-of-order operation insert")
		}
		c.state.operations++
		if c.state.operationErr != nil {
			return nil, c.state.operationErr
		}
		return &mutationRows{columns: []string{"id"}, values: [][]driver.Value{{c.state.operationID.String()}}}, nil
	case strings.HasPrefix(query, `INSERT INTO "operation_events"`):
		if c.state.operations != 1 || c.state.operationErr != nil || c.state.events != 0 {
			return nil, errors.New("audit insert without a successful operation insert")
		}
		c.state.events++
		if c.state.eventErr != nil {
			return nil, c.state.eventErr
		}
		return &mutationRows{columns: []string{"id"}, values: [][]driver.Value{{uuid.New().String()}}}, nil
	default:
		return nil, fmt.Errorf("unexpected creation SQL: %s", query)
	}
}
func (tx *creationTransaction) Commit() error {
	tx.state.active = false
	tx.state.commits++
	return tx.state.commitErr
}
func (tx *creationTransaction) Rollback() error {
	tx.state.active = false
	tx.state.rollbacks++
	return nil
}

func creationTransactionPair(t *testing.T) (models.Operation, models.OperationEvent) {
	t.Helper()
	op, err := NewOperation(sampleInput(), time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	op.ID = uuid.New()
	return op, models.OperationEvent{OperationID: op.ID, EventType: "created", ActorType: string(OwnerHAI),
		AfterStatus: string(StatusNew), PayloadJSON: "{}", CreatedAt: op.CreatedAt}
}

func creationTransactionRepository(t *testing.T, state *creationSQL) *GormRepository {
	t.Helper()
	sqlDB := sql.OpenDB(creationConnector{state: state})
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB, PreferSimpleProtocol: true}),
		&gorm.Config{DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	return NewGormRepository(db)
}

func TestAtomicCreationGormTransactionWiring(t *testing.T) {
	for _, outcome := range []string{"success", "operation_failure", "active_dedupe_conflict", "other_unique_conflict", "non_unique_named_like_dedupe", "event_failure", "event_named_like_dedupe", "commit_failure"} {
		t.Run(outcome, func(t *testing.T) {
			op, audit := creationTransactionPair(t)
			beforeOp, beforeAudit := op, audit
			state := &creationSQL{operationID: op.ID}
			var want error
			switch outcome {
			case "operation_failure":
				state.operationErr = errors.New("operation insert rejected")
				want = state.operationErr
			case "active_dedupe_conflict":
				state.operationErr = &pgconn.PgError{Code: "23505", ConstraintName: "uq_operations_owner_workspace_dedupe_active"}
				want = ErrDuplicateDedupeKey
			case "other_unique_conflict":
				state.operationErr = &pgconn.PgError{Code: "23505", ConstraintName: "operations_pkey"}
				want = state.operationErr
			case "non_unique_named_like_dedupe":
				state.operationErr = &pgconn.PgError{Code: "42501", ConstraintName: "uq_operations_owner_workspace_dedupe_active"}
				want = state.operationErr
			case "event_failure":
				state.eventErr = errors.New("audit insert rejected")
				want = state.eventErr
			case "event_named_like_dedupe":
				state.eventErr = &pgconn.PgError{Code: "23505", ConstraintName: "uq_operations_owner_workspace_dedupe_active"}
				want = state.eventErr
			case "commit_failure":
				state.commitErr = errors.New("commit outcome unknown")
				want = state.commitErr
			}
			saved, err := creationTransactionRepository(t, state).CreateWithEvent(&op, &audit)
			if want == nil {
				if err != nil || saved == nil || !reflect.DeepEqual(*saved, beforeOp) || state.commits != 1 || state.rollbacks != 0 {
					t.Fatalf("creation success wiring: saved=%+v err=%v state=%+v", saved, err, state)
				}
			} else {
				if saved != nil || !errors.Is(err, want) {
					t.Fatalf("creation failure wiring: saved=%+v err=%v want=%v state=%+v", saved, err, want, state)
				}
				if outcome == "commit_failure" {
					// A failed Commit is not evidence of either durable success or rollback.
					if state.commits != 1 || state.rollbacks != 0 {
						t.Fatalf("unknown commit outcome was misclassified: %+v", state)
					}
				} else if state.commits != 0 || state.rollbacks != 1 {
					t.Fatalf("failed creation did not request rollback: %+v", state)
				}
			}
			wantEvents := 1
			if state.operationErr != nil {
				wantEvents = 0
			}
			if state.begins != 1 || state.operations != 1 || state.events != wantEvents || state.active ||
				!reflect.DeepEqual(op, beforeOp) || !reflect.DeepEqual(audit, beforeAudit) {
				t.Fatalf("unexpected transaction attempts or mutated inputs: %+v", state)
			}
			if outcome != "active_dedupe_conflict" && errors.Is(err, ErrDuplicateDedupeKey) {
				t.Fatal("non-operation dedupe error masked as a successful collision retry")
			}
		})
	}
}

func TestAtomicCreationGormRefusesUnboundAuditBeforeTransaction(t *testing.T) {
	op, audit := creationTransactionPair(t)
	audit.OperationID = uuid.New()
	state := &creationSQL{operationID: op.ID}
	saved, err := creationTransactionRepository(t, state).CreateWithEvent(&op, &audit)
	if saved != nil || !errors.Is(err, ErrInvalidCreationEvent) || state.begins != 0 || state.operations != 0 || state.events != 0 {
		t.Fatalf("invalid creation pair reached SQL: saved=%+v err=%v state=%+v", saved, err, state)
	}
}

func TestActiveDedupeConflictClassification(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"wrapped_pg", fmt.Errorf("insert: %w", &pgconn.PgError{Code: "23505", ConstraintName: "uq_operations_owner_workspace_dedupe_active"}), true},
		{"similar_name", &pgconn.PgError{Code: "23505", ConstraintName: "uq_operations_owner_workspace_dedupe_active_fake"}, false},
		{"mention_only", errors.New("permission denied for uq_operations_owner_workspace_dedupe_active"), false},
		{"driver_message", errors.New(`duplicate key value violates unique constraint "uq_operations_owner_workspace_dedupe_active" (SQLSTATE 23505)`), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := isActiveDedupeConflict(tc.err); got != tc.want {
				t.Fatalf("dedupe conflict classification = %v, want %v", got, tc.want)
			}
		})
	}
}
