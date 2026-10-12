package workflow

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/models"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// This transport executes actual GORM SQL without connecting to a server.
// Commit/rollback counters are controlled acknowledgements, not database proof.
type recoverySQLProbe struct {
	item                    models.WorkflowItem
	loop                    models.WorkflowOpenLoop
	now                     time.Time
	queries                 []string
	contexts                []context.Context
	begin, commit, rollback int
	updated                 bool
	evidence                int
	failTable               string
	noAcknowledgement       bool
	commitErr               error
}

type recoverySQLConnector struct{ p *recoverySQLProbe }

func (c recoverySQLConnector) Connect(context.Context) (driver.Conn, error) {
	return &recoverySQLConnection{p: c.p}, nil
}
func (c recoverySQLConnector) Driver() driver.Driver { return c }
func (c recoverySQLConnector) Open(string) (driver.Conn, error) {
	return c.Connect(context.Background())
}

type recoverySQLConnection struct{ p *recoverySQLProbe }

func (*recoverySQLConnection) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (*recoverySQLConnection) Close() error { return nil }
func (c *recoverySQLConnection) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}
func (c *recoverySQLConnection) BeginTx(ctx context.Context, _ driver.TxOptions) (driver.Tx, error) {
	c.p.begin++
	c.p.contexts = append(c.p.contexts, ctx)
	return &recoverySQLTransaction{p: c.p}, nil
}
func (c *recoverySQLConnection) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	p := c.p
	p.queries = append(p.queries, query)
	p.contexts = append(p.contexts, ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !strings.HasPrefix(query, "UPDATE ") {
		return nil, errors.New("unexpected SQL execution")
	}
	clockMatched := false
	for _, arg := range args {
		if value, ok := arg.Value.(time.Time); ok && value.Equal(p.now) {
			clockMatched = true
		}
	}
	if !clockMatched {
		return nil, errors.New("recovery did not use database wall time")
	}
	lease := p.item.WorkerLeaseUntil
	if strings.Contains(query, "workflow_open_loops") {
		lease = p.loop.LeaseUntil
	}
	if lease != nil && lease.After(p.now) {
		return driver.RowsAffected(0), nil
	}
	p.updated = true
	return driver.RowsAffected(1), nil
}
func (c *recoverySQLConnection) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	p := c.p
	p.queries = append(p.queries, query)
	p.contexts = append(p.contexts, ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if strings.HasPrefix(query, "SELECT clock_timestamp()") {
		return &recoverySQLRows{columns: []string{"clock_timestamp"}, values: []driver.Value{p.now}}, nil
	}
	if strings.HasPrefix(query, "INSERT INTO ") {
		if strings.Contains(query, p.failTable) && p.failTable != "" {
			if p.noAcknowledgement {
				return &recoverySQLRows{columns: []string{"id"}}, nil
			}
			return nil, errors.New("token=private-evidence-error")
		}
		p.evidence++
		return &recoverySQLRows{columns: []string{"id"}, values: []driver.Value{args[0].Value}}, nil
	}
	if strings.Contains(query, `FROM "workflow_items"`) {
		if strings.Contains(query, "FOR UPDATE") {
			ownerBound := false
			for _, arg := range args {
				if arg.Value == p.item.OwnerIdentity {
					ownerBound = true
				}
			}
			if !strings.Contains(query, "owner_identity") || !ownerBound {
				return nil, errors.New("workflow lock did not bind authenticated owner")
			}
			if strings.Contains(query, "archived =") && p.item.Archived {
				return &recoverySQLRows{columns: []string{"id"}}, nil
			}
		}
		state := p.item.CurrentState
		if p.updated && strings.Contains(p.queries[len(p.queries)-2], `UPDATE "workflow_items"`) {
			state = StateBlocked
		}
		return &recoverySQLRows{columns: []string{"id", "owner_identity", "current_state", "updated_at", "worker_claim_id", "worker_lease_until", "approval_status", "archived"}, values: []driver.Value{p.item.ID.String(), p.item.OwnerIdentity, state, p.item.UpdatedAt, p.item.WorkerClaimID, *p.item.WorkerLeaseUntil, "approved", p.item.Archived}}, nil
	}
	if strings.Contains(query, `FROM "workflow_open_loops"`) {
		status := p.loop.Status
		if p.updated {
			status = "open"
		}
		return &recoverySQLRows{columns: []string{"id", "workflow_id", "status", "updated_at", "claim_id", "lease_until"}, values: []driver.Value{p.loop.ID.String(), p.loop.WorkflowID.String(), status, p.loop.UpdatedAt, p.loop.ClaimID, *p.loop.LeaseUntil}}, nil
	}
	return nil, errors.New("unexpected SQL query")
}

type recoverySQLTransaction struct{ p *recoverySQLProbe }

func (t *recoverySQLTransaction) Commit() error   { t.p.commit++; return t.p.commitErr }
func (t *recoverySQLTransaction) Rollback() error { t.p.rollback++; return nil }

type recoverySQLRows struct {
	columns []string
	values  []driver.Value
	read    bool
}

func (r *recoverySQLRows) Columns() []string { return r.columns }
func (*recoverySQLRows) Close() error        { return nil }
func (r *recoverySQLRows) Next(out []driver.Value) error {
	if r.read || r.values == nil {
		return io.EOF
	}
	r.read = true
	copy(out, r.values)
	return nil
}

func TestRecoveryAtomicSQLAcknowledgesStateAndHistoryTogether(t *testing.T) {
	for _, kind := range []string{"workflow", "open-loop"} {
		for _, scenario := range []string{"confirmed", "event-error", "event-not-acknowledged", "decision-error", "commit-unconfirmed", "live-lease", "archived-parent"} {
			t.Run(kind+"/"+scenario, func(t *testing.T) {
				now := time.Now().UTC().Truncate(time.Microsecond)
				lease, revision := now.Add(-time.Minute), now.Add(-2*time.Minute)
				if scenario == "live-lease" {
					lease = now.Add(time.Minute)
				}
				p := &recoverySQLProbe{now: now, item: models.WorkflowItem{ID: uuid.New(), OwnerIdentity: "alice", CurrentState: StateInProgress, WorkerClaimID: "owned-workflow", WorkerLeaseUntil: &lease, UpdatedAt: revision}, loop: models.WorkflowOpenLoop{ID: uuid.New(), Status: "processing", ClaimID: "owned-loop", LeaseUntil: &lease, UpdatedAt: revision}}
				p.loop.WorkflowID = p.item.ID
				p.item.Archived = scenario == "archived-parent"
				if strings.HasPrefix(scenario, "event-") {
					p.failTable = "workflow_events"
				}
				if scenario == "decision-error" {
					p.failTable = "workflow_decisions"
				}
				p.noAcknowledgement = scenario == "event-not-acknowledged"
				if scenario == "commit-unconfirmed" {
					p.commitErr = errors.New("commit acknowledgement unavailable")
				}
				pool := sql.OpenDB(recoverySQLConnector{p: p})
				t.Cleanup(func() { _ = pool.Close() })
				db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{DisableAutomaticPing: true, SkipDefaultTransaction: true, Logger: logger.Default.LogMode(logger.Silent)})
				if err != nil {
					t.Fatal(err)
				}
				ctx := context.WithValue(context.Background(), reminderStopContextKey{}, "recovery-sql")
				repo := &GormRepository{DB: db.WithContext(ctx)}
				var changed bool
				var outcome any
				// Deliberately ahead host clock: the protected update must use SQL time.
				if kind == "workflow" {
					var value *models.WorkflowItem
					value, changed, err = repo.RecoverExpiredWorkflowClaimAtomic(p.item, now.Add(time.Hour))
					if value != nil {
						outcome = value
					}
				} else {
					var value *models.WorkflowOpenLoop
					value, changed, err = repo.RecoverExpiredOpenLoopClaimAtomic("alice", p.loop, now.Add(time.Hour))
					if value != nil {
						outcome = value
					}
				}
				if p.begin != 1 {
					t.Fatal("recovery did not own one transaction")
				}
				if scenario == "confirmed" {
					wanted := 3
					if kind == "open-loop" {
						wanted = 2
					}
					if err != nil || !changed || outcome == nil || p.commit != 1 || p.rollback != 0 || p.evidence != wanted {
						t.Fatalf("missing confirmed history: changed=%v evidence=%d commit=%d rollback=%d err=%v", changed, p.evidence, p.commit, p.rollback, err)
					}
				} else if scenario == "live-lease" || scenario == "archived-parent" {
					if err != nil || changed || outcome != nil || p.updated || p.evidence != 0 || p.commit != 1 {
						t.Fatal("recovered a live lease or archived parent")
					}
				} else {
					if err == nil || changed || outcome != nil {
						t.Fatal("unconfirmed recovery reported a successful mutation")
					}
					if scenario == "commit-unconfirmed" {
						if p.commit != 1 {
							t.Fatal("commit uncertainty was not exercised")
						}
					} else if p.rollback != 1 || p.commit != 0 {
						t.Fatal("history failure did not roll back the owned transaction")
					}
				}
				for _, received := range p.contexts {
					if received.Value(reminderStopContextKey{}) != "recovery-sql" {
						t.Fatal("SQL detached from caller context")
					}
				}
				if !strings.Contains(strings.Join(p.queries, "\n"), "FOR UPDATE") {
					t.Fatal("recovery bypassed record locking")
				}
			})
		}
	}
}
