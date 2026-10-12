package durablejob

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/models"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Actual GORM transport/SQL with controlled acknowledgements; no server or
// transaction durability is established by these probes.
type reviewSQLPool struct {
	ctx         context.Context
	query       string
	args        []interface{}
	rows        int64
	history     []string
	historyArgs [][]interface{}
	commitError error
	commits     int
}

func (*reviewSQLPool) PrepareContext(context.Context, string) (*sql.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (p *reviewSQLPool) ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	p.ctx, p.query, p.args = ctx, query, args
	p.history = append(p.history, query)
	p.historyArgs = append(p.historyArgs, append([]interface{}(nil), args...))
	return reviewSQLResult(p.rows), ctx.Err()
}
func (p *reviewSQLPool) QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error) {
	p.ctx, p.query, p.args = ctx, query, args
	p.history = append(p.history, query)
	p.historyArgs = append(p.historyArgs, append([]interface{}(nil), args...))
	return nil, errors.New("controlled query boundary")
}
func (*reviewSQLPool) QueryRowContext(context.Context, string, ...interface{}) *sql.Row {
	panic("unexpected row")
}
func (p *reviewSQLPool) BeginTx(context.Context, *sql.TxOptions) (gorm.ConnPool, error) {
	return &reviewSQLTx{reviewSQLPool: p}, nil
}

type reviewSQLTx struct{ *reviewSQLPool }

func (tx *reviewSQLTx) Commit() error { tx.commits++; return tx.commitError }
func (*reviewSQLTx) Rollback() error  { return nil }

type reviewSQLResult int64

func (r reviewSQLResult) RowsAffected() (int64, error) { return int64(r), nil }
func (reviewSQLResult) LastInsertId() (int64, error)   { return 0, errors.New("not applicable") }

func reviewProbeDB(t *testing.T, p *reviewSQLPool, dry bool) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: p}), &gorm.Config{DisableAutomaticPing: true, SkipDefaultTransaction: true, DryRun: dry, Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func TestReviewSQLWritesOnlyOwnedLeaseWithContextAndRedaction(t *testing.T) {
	for _, affected := range []int64{0, 1, 2} {
		p := &reviewSQLPool{rows: affected}
		r := &gormRepository{db: reviewProbeDB(t, p, false)}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		id := uuid.New()
		owned, err := r.MarkForReview(ctx, id, "worker", 7, time.Now(), 2, "token=must-not-leak")
		if err != nil || owned != (affected == 1) {
			t.Fatalf("incorrect review acknowledgement: %v %v", owned, err)
		}
		if p.ctx != ctx || !strings.HasPrefix(p.query, "UPDATE ") || strings.Contains(p.query, "INSERT") || !strings.Contains(p.query, "lease_generation = ") || !strings.Contains(p.query, "locked_by = ") {
			t.Fatalf("review lost context or ownership fence: %s", p.query)
		}
		if p.args[len(p.args)-4] != id || p.args[len(p.args)-3] != models.DurableJobRunning || p.args[len(p.args)-2] != "worker" || p.args[len(p.args)-1] != int64(7) {
			t.Fatal("review fence identity changed")
		}
		for _, arg := range p.args {
			if s, ok := arg.(string); ok && strings.Contains(s, "must-not-leak") {
				t.Fatal("review write retained credential")
			}
		}
	}
}

func TestOrdinarySettlementWritesRedactProviderErrors(t *testing.T) {
	for _, scenario := range []string{"retry", "defer", "dead"} {
		t.Run(scenario, func(t *testing.T) {
			pool := &reviewSQLPool{rows: 1}
			repo := &gormRepository{db: reviewProbeDB(t, pool, false)}
			now := time.Now().UTC()
			var owned bool
			var err error
			switch scenario {
			case "retry":
				owned, err = repo.MarkForRetry(uuid.New(), "worker", 7, now.Add(time.Minute), 2, "provider failed: token=must-not-leak")
			case "defer":
				owned, err = repo.MarkDeferred(uuid.New(), "worker", 7, now.Add(time.Minute), "provider paused: token=must-not-leak")
			case "dead":
				owned, err = repo.MarkDead(uuid.New(), "worker", 7, now, 2, "provider failed: token=must-not-leak")
			}
			if err != nil || !owned {
				t.Fatalf("settlement = owned %t, err %v", owned, err)
			}
			foundRedaction := false
			for _, arg := range pool.args {
				text, ok := arg.(string)
				if !ok {
					continue
				}
				if strings.Contains(text, "must-not-leak") {
					t.Fatalf("credential-bearing handler error reached SQL arguments: %q", text)
				}
				foundRedaction = foundRedaction || strings.Contains(text, "[REDACTED]")
			}
			if !foundRedaction {
				t.Fatalf("SQL arguments contain no redacted error value: %#v", pool.args)
			}
		})
	}
}

func TestReviewSQLSchedulingAndRecurringReplacementHonorHeldWork(t *testing.T) {
	for _, scope := range []string{"singleton", "payload", "count", "recurring"} {
		t.Run(scope, func(t *testing.T) {
			db := reviewProbeDB(t, &reviewSQLPool{}, true)
			found, created := false, false
			if err := db.Callback().Query().After("gorm:query").Register("review-count", func(tx *gorm.DB) {
				for _, value := range tx.Statement.Vars {
					if value == models.DurableJobNeedsReview {
						found = true
					}
				}
				if count, ok := tx.Statement.Dest.(*int64); ok && found {
					*count = 1
					tx.RowsAffected = 1
				}
			}); err != nil {
				t.Fatal(err)
			}
			if err := db.Callback().Create().After("gorm:create").Register("review-insert", func(*gorm.DB) { created = true }); err != nil {
				t.Fatal(err)
			}
			if err := db.Callback().Update().After("gorm:update").Register("review-owned-update", func(tx *gorm.DB) { tx.RowsAffected = 1 }); err != nil {
				t.Fatal(err)
			}
			r := &gormRepository{db: db}
			job := &models.DurableJob{Queue: "ambient", Kind: "ambient.scan", Payload: "{}"}
			var err error
			switch scope {
			case "singleton":
				_, err = r.EnqueueIfNoActive(job)
			case "payload":
				_, err = r.EnqueueIfNoActiveMatchingPayload(job)
			case "count":
				_, err = r.CountActiveByKind(job.Kind)
			case "recurring":
				_, _, err = r.CompleteRecurring(uuid.New(), "worker", 1, time.Now(), models.DurableJobSucceeded, 1, "", job)
			}
			if err != nil || !found || created {
				t.Fatalf("review hold did not block %s: err=%v found=%v created=%v", scope, err, found, created)
			}
		})
	}
}

func TestReviewSQLInvalidRootsCannotRecordHold(t *testing.T) {
	for _, scenario := range []string{"dry-run", "missing-config"} {
		t.Run(scenario, func(t *testing.T) {
			p := &reviewSQLPool{rows: 1}
			db := reviewProbeDB(t, p, true)
			if scenario == "missing-config" {
				db = &gorm.DB{}
			}
			r := &gormRepository{db: db}
			if owned, err := r.MarkForReview(context.Background(), uuid.New(), "worker", 1, time.Now(), 1, "unknown"); owned || !errors.Is(err, ErrReviewPersistenceUnavailable) || p.query != "" {
				t.Fatal("invalid review store was acknowledged")
			}
		})
	}
}

func TestReviewSQLClaimRejectsOtherPendingWorkBehindSameKindHold(t *testing.T) {
	p := &reviewSQLPool{}
	r := &gormRepository{db: reviewProbeDB(t, p, false)}
	_, err := r.ClaimDue("worker", "ambient", time.Now(), 1)
	if err == nil || !strings.Contains(p.query, "NOT EXISTS") || !strings.Contains(p.query, "held.queue = candidate.queue") || !strings.Contains(p.query, "held.kind = candidate.kind") || !reviewHasValue(p.args, models.DurableJobNeedsReview) || !reviewHasValue(p.args, models.DurableJobSettling) {
		t.Fatalf("claim query bypasses review hold: %s args=%v err=%v", p.query, p.args, err)
	}
}

func TestReviewSQLExpiredLeaseRecoveryRequiresPersistedReplayPolicy(t *testing.T) {
	db := reviewProbeDB(t, &reviewSQLPool{}, true)
	var statements []string
	var values [][]interface{}
	if err := db.Callback().Update().After("gorm:update").Register("capture-recovery", func(tx *gorm.DB) {
		statements = append(statements, tx.Statement.SQL.String())
		values = append(values, append([]interface{}(nil), tx.Statement.Vars...))
	}); err != nil {
		t.Fatal(err)
	}
	r := &gormRepository{db: db}
	if _, err := r.ReapExpiredLeasesForQueue("ambient", time.Now(), time.Minute); err != nil {
		t.Fatal(err)
	}
	if len(statements) != 3 {
		t.Fatalf("recovery must quarantine uncertainty before retry/dead paths; got %d updates: %v", len(statements), statements)
	}
	for i, query := range statements {
		if !strings.Contains(query, "replay_policy") || !strings.Contains(query, "queue =") || !strings.Contains(query, "locked_at") {
			t.Fatalf("recovery update %d lacks persisted policy/queue/expiry fence: %s", i, query)
		}
	}
	if !reviewHasValue(values[0], "needs_review") || !reviewHasValue(values[1], "at_least_once") || !reviewHasValue(values[2], "at_least_once") {
		t.Fatal("review quarantine or explicit replay eligibility missing")
	}
	for _, query := range statements[1:] {
		where := strings.SplitN(query, " WHERE ", 2)
		if len(where) != 2 || !strings.Contains(where[1], "replay_policy =") {
			t.Fatal("retry/dead update must require explicit replay permission, not merely write policy")
		}
	}
}

func TestReviewSQLStartupUpgradesExistingPolicyBeforeAdmission(t *testing.T) {
	p := &reviewSQLPool{}
	r := &gormRepository{db: reviewProbeDB(t, p, false)}
	runner := NewRunner(r, Options{Queue: "ambient"})
	err := runner.RegisterReviewRecurring("ambient.scan", time.Minute, 3, func(context.Context) error { t.Fatal("unconfirmed registration executed"); return nil })
	if !errors.Is(err, ErrReviewPersistenceUnavailable) || len(runner.handlers) != 0 || len(runner.recurring) != 0 {
		t.Fatal("failed policy persistence published an executable handler")
	}
	if len(p.history) != 3 || !strings.Contains(p.history[0], "pg_advisory_xact_lock") ||
		!strings.HasPrefix(p.history[1], "UPDATE ") || !strings.Contains(p.history[1], "replay_policy") ||
		!strings.Contains(p.history[1], "queue =") || !strings.Contains(p.history[1], "kind =") ||
		!reviewHasValue(p.historyArgs[1], models.DurableJobReviewUnknown) ||
		!reviewHasValue(p.historyArgs[1], models.DurableJobNeedsReview) ||
		!strings.Contains(p.history[2], "count(*)") {
		t.Fatalf("startup did not fence and upgrade active work before counting: %v", p.history)
	}
}

func TestReviewSQLStartupRejectsDryRunAndBorrowedTransaction(t *testing.T) {
	for _, scenario := range []string{"dry-run", "borrowed-transaction"} {
		p := &reviewSQLPool{}
		db := reviewProbeDB(t, p, scenario == "dry-run")
		if scenario == "borrowed-transaction" {
			db.Statement.ConnPool = &reviewSQLTx{reviewSQLPool: p}
		}
		runner := NewRunner(&gormRepository{db: db}, Options{Queue: "ambient"})
		if err := runner.RegisterReviewRecurring("ambient.scan", time.Minute, 3, func(context.Context) error { return nil }); !errors.Is(err, ErrReviewPersistenceUnavailable) || len(p.history) != 0 || len(runner.handlers) != 0 {
			t.Fatal("invalid policy persistence admitted work")
		}
	}
}

func reviewHasValue(args []interface{}, expected string) bool {
	for _, arg := range args {
		if s, ok := arg.(string); ok && s == expected {
			return true
		}
	}
	return false
}
