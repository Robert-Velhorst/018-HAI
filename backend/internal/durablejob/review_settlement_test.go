package durablejob

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

// Real GORM/database/sql calls over a scripted transport. This exercises
// acknowledgements and SQL fences, not PostgreSQL locks or crash durability.
type settlementCall struct {
	ctx   context.Context
	query string
	args  []driver.NamedValue
}

type settlementTransport struct {
	calls                                []settlementCall
	commits, rollbacks, updates, inserts int
	failCommit                           int
	commitErr                            error
	cancelAfterPrepare                   context.CancelFunc
	rejectUpdate                         int
	existing                             int64
	next                                 *models.DurableJob
}

type settlementConnector struct{ transport *settlementTransport }
type settlementDriver struct{}
type settlementConn struct{ transport *settlementTransport }
type settlementTx struct{ transport *settlementTransport }
type settlementRows struct {
	columns []string
	values  []driver.Value
	read    bool
}

func (c settlementConnector) Connect(context.Context) (driver.Conn, error) {
	return &settlementConn{c.transport}, nil
}
func (settlementConnector) Driver() driver.Driver { return settlementDriver{} }
func (settlementDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("unexpected driver open")
}
func (*settlementConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (*settlementConn) Close() error                { return nil }
func (c *settlementConn) Begin() (driver.Tx, error) { return &settlementTx{c.transport}, nil }
func (c *settlementConn) BeginTx(ctx context.Context, _ driver.TxOptions) (driver.Tx, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return c.Begin()
}
func (c *settlementConn) record(ctx context.Context, query string, args []driver.NamedValue) error {
	c.transport.calls = append(c.transport.calls, settlementCall{ctx, query, append([]driver.NamedValue(nil), args...)})
	return ctx.Err()
}
func (c *settlementConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if err := c.record(ctx, query, args); err != nil {
		return nil, err
	}
	if strings.HasPrefix(query, "UPDATE ") {
		c.transport.updates++
		if c.transport.updates == c.transport.rejectUpdate {
			return driver.RowsAffected(0), nil
		}
		return driver.RowsAffected(1), nil
	}
	if strings.Contains(query, "pg_advisory_xact_lock") {
		return driver.RowsAffected(1), nil
	}
	return nil, errors.New("unexpected settlement execution")
}
func (c *settlementConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if err := c.record(ctx, query, args); err != nil {
		return nil, err
	}
	if strings.Contains(query, "count(*)") {
		return &settlementRows{columns: []string{"count"}, values: []driver.Value{c.transport.existing}}, nil
	}
	if strings.HasPrefix(query, "INSERT ") {
		c.transport.inserts++
		return &settlementRows{columns: []string{"id"}, values: []driver.Value{c.transport.next.ID.String()}}, nil
	}
	return nil, errors.New("unexpected settlement query")
}
func (tx *settlementTx) Commit() error {
	tx.transport.commits++
	if tx.transport.commits == tx.transport.failCommit {
		return tx.transport.commitErr
	}
	if tx.transport.commits == 1 && tx.transport.cancelAfterPrepare != nil {
		tx.transport.cancelAfterPrepare()
	}
	return nil
}
func (tx *settlementTx) Rollback() error    { tx.transport.rollbacks++; return nil }
func (r *settlementRows) Columns() []string { return r.columns }
func (*settlementRows) Close() error        { return nil }
func (r *settlementRows) Next(dest []driver.Value) error {
	if r.read {
		return io.EOF
	}
	copy(dest, r.values)
	r.read = true
	return nil
}

func settlementDB(t *testing.T, transport *settlementTransport) *gorm.DB {
	t.Helper()
	pool := sql.OpenDB(settlementConnector{transport})
	pool.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = pool.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{DisableAutomaticPing: true, SkipDefaultTransaction: true, Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func settlementHasArg(call settlementCall, value driver.Value) bool {
	for _, arg := range call.args {
		if arg.Value == value {
			return true
		}
	}
	return false
}

func TestReviewSettlementSQLRequiresBothAcknowledgements(t *testing.T) {
	for _, scenario := range []string{"success", "existing-successor", "first-commit-lost", "canceled-after-prepare", "preparation-unowned", "release-unowned", "release-commit-lost"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			next := &models.DurableJob{Queue: "review", Kind: "ambient.scan", ReplayPolicy: models.DurableJobReviewUnknown}
			commitErr := errors.New("commit acknowledgement lost")
			transport := &settlementTransport{next: next, commitErr: commitErr}
			switch scenario {
			case "existing-successor":
				transport.existing = 1
			case "first-commit-lost":
				transport.failCommit = 1
			case "canceled-after-prepare":
				transport.cancelAfterPrepare = cancel
			case "preparation-unowned":
				transport.rejectUpdate = 1
			case "release-unowned":
				transport.rejectUpdate = 2
			case "release-commit-lost":
				transport.failCommit = 2
			}
			db := settlementDB(t, transport)
			id := uuid.New()
			now := time.Date(2026, 10, 2, 6, 7, 8, 123456789, time.FixedZone("operator", 7200))
			owned, created, err := (&gormRepository{db: db}).CompleteReviewRecurring(ctx, id, "worker", 7, now, models.DurableJobSucceeded, 2, "token=must-not-leak", next)
			success := scenario == "success" || scenario == "existing-successor"
			if owned != success || created != (scenario == "success") || (err == nil) != success {
				t.Fatalf("invalid acknowledgement: owned=%v created=%v err=%v", owned, created, err)
			}
			if strings.Contains(scenario, "commit-lost") && !errors.Is(err, commitErr) {
				t.Fatal("commit failure identity lost")
			}
			if scenario == "canceled-after-prepare" && !errors.Is(err, context.Canceled) {
				t.Fatal("cancellation lost")
			}
			if strings.Contains(scenario, "unowned") && !errors.Is(err, ErrJobSettlementUnconfirmed) {
				t.Fatal("unowned settlement accepted")
			}
			wantUpdates := 2
			if scenario == "first-commit-lost" || scenario == "canceled-after-prepare" || scenario == "preparation-unowned" {
				wantUpdates = 1
			}
			if transport.updates != wantUpdates {
				t.Fatalf("release crossed unconfirmed preparation: updates=%d want=%d", transport.updates, wantUpdates)
			}
			wantCommits := 2
			if scenario == "first-commit-lost" || scenario == "canceled-after-prepare" || scenario == "release-unowned" {
				wantCommits = 1
			}
			if scenario == "preparation-unowned" {
				wantCommits = 0
			}
			if transport.commits != wantCommits {
				t.Fatalf("incorrect commit sequence: %d want=%d", transport.commits, wantCommits)
			}
			wantInserts := 1
			if scenario == "existing-successor" || scenario == "preparation-unowned" {
				wantInserts = 0
			}
			if transport.inserts != wantInserts {
				t.Fatal("successor admission was not respected")
			}
			updates := []settlementCall{}
			for _, call := range transport.calls {
				if call.ctx != ctx {
					t.Fatal("SQL operation lost bounded caller context")
				}
				for _, arg := range call.args {
					if text, ok := arg.Value.(string); ok && strings.Contains(text, "must-not-leak") {
						t.Fatal("SQL retained recognized secret")
					}
				}
				if strings.HasPrefix(call.query, "UPDATE ") {
					updates = append(updates, call)
				}
			}
			prep := updates[0]
			where := strings.SplitN(prep.query, " WHERE ", 2)
			if len(where) != 2 {
				t.Fatal("preparation lacks conditional write")
			}
			for _, fence := range []string{"id =", "status =", "locked_by =", "lease_generation =", "queue =", "kind =", "replay_policy ="} {
				if !strings.Contains(where[1], fence) {
					t.Fatalf("preparation missing %s", fence)
				}
			}
			if !settlementHasArg(prep, models.DurableJobSettling) || !settlementHasArg(prep, models.DurableJobRunning) || !settlementHasArg(prep, id.String()) || !settlementHasArg(prep, int64(7)) || !settlementHasArg(prep, nil) {
				t.Fatal("preparation lost ownership or nonterminal barrier")
			}
			if strings.Contains(where[0], "locked_by") || strings.Contains(where[0], "locked_at") {
				t.Fatal("preparation cleared release identity")
			}
			if len(updates) > 1 {
				release := updates[1]
				where := strings.SplitN(release.query, " WHERE ", 2)
				for _, fence := range []string{"id =", "status =", "locked_by =", "lease_generation =", "queue =", "kind =", "replay_policy =", "attempts =", "updated_at =", "last_error =", "completed_at IS NULL"} {
					if len(where) != 2 || !strings.Contains(where[1], fence) {
						t.Fatalf("release missing %s", fence)
					}
				}
				if !settlementHasArg(release, now.UTC().Truncate(time.Microsecond)) || !settlementHasArg(release, models.DurableJobSettling) || !settlementHasArg(release, models.DurableJobSucceeded) {
					t.Fatal("release lost exact preparation identity")
				}
			}
		})
	}
}

func TestReviewSettlementRejectsInvalidRootsAndInputsBeforeSQL(t *testing.T) {
	for _, scenario := range []string{"nil-context", "canceled", "dry-run", "borrowed-transaction", "invalid-terminal", "legacy-policy", "reused-id", "attempted-successor", "claimed-successor"} {
		t.Run(scenario, func(t *testing.T) {
			next := &models.DurableJob{Queue: "review", Kind: "scan", ReplayPolicy: models.DurableJobReviewUnknown}
			transport := &settlementTransport{next: next}
			db := settlementDB(t, transport)
			id := uuid.New()
			terminal := models.DurableJobSucceeded
			ctx := context.Background()
			switch scenario {
			case "nil-context":
				ctx = nil
			case "canceled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "dry-run":
				db = db.Session(&gorm.Session{DryRun: true})
			case "borrowed-transaction":
				db.Statement.ConnPool = &reviewSQLTx{reviewSQLPool: &reviewSQLPool{}}
			case "invalid-terminal":
				terminal = models.DurableJobPending
			case "legacy-policy":
				next.ReplayPolicy = models.DurableJobReplayAtLeastOnce
			case "reused-id":
				next.ID = id
			case "attempted-successor":
				next.Attempts = 1
			case "claimed-successor":
				next.LockedBy = "other-worker"
			}
			owned, created, err := (&gormRepository{db: db}).CompleteReviewRecurring(ctx, id, "worker", 1, time.Now(), terminal, 1, "", next)
			if err == nil || owned || created || len(transport.calls) != 0 {
				t.Fatal("invalid completion reached SQL")
			}
		})
	}
}

func TestReviewSettlementCannotUseLegacyCompletion(t *testing.T) {
	p := &reviewSQLPool{}
	r := &gormRepository{db: reviewProbeDB(t, p, false)}
	owned, created, err := r.CompleteRecurring(uuid.New(), "worker", 1, time.Now(), models.DurableJobSucceeded, 1, "", &models.DurableJob{ReplayPolicy: models.DurableJobReviewUnknown})
	if !errors.Is(err, ErrReviewPersistenceUnavailable) || owned || created || len(p.history) != 0 {
		t.Fatal("legacy path bypassed review preparation")
	}
}

type incompleteReviewRepository struct {
	Repository
	ManualReviewRepository
	ReviewSchedulingRepository
}

func TestReviewSettlementCapabilityRequiredBeforeRegistration(t *testing.T) {
	q := &reviewQueue{fakeRepo: newFakeRepo(), acknowledged: true}
	repo := &incompleteReviewRepository{q, q, q}
	runner := NewRunner(repo, Options{Queue: "review"})
	if runner.SupportsManualReview() {
		t.Fatal("incomplete review adapter reported capability")
	}
	if err := runner.RegisterReviewRecurring("scan", time.Minute, 3, func(context.Context) error { t.Fatal("handler admitted"); return nil }); !errors.Is(err, ErrReviewPersistenceUnavailable) || len(q.jobs) != 0 || len(runner.handlers) != 0 {
		t.Fatal("registration bypassed contextual completion contract")
	}
}
