package ambient

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

// Exercise actual GORM SQL/acknowledgement behavior without a server. This
// transport records statements and supplies controlled affected-row counts.
type ambientSQLProbe struct {
	ctx   context.Context
	query string
	args  []interface{}
	rows  int64
}

func (*ambientSQLProbe) PrepareContext(context.Context, string) (*sql.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (p *ambientSQLProbe) ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	p.ctx, p.query, p.args = ctx, query, args
	return ambientAffectedRows(p.rows), ctx.Err()
}
func (*ambientSQLProbe) QueryContext(context.Context, string, ...interface{}) (*sql.Rows, error) {
	return nil, errors.New("unexpected query")
}
func (*ambientSQLProbe) QueryRowContext(context.Context, string, ...interface{}) *sql.Row {
	panic("unexpected row query")
}

type ambientAffectedRows int64

func (r ambientAffectedRows) RowsAffected() (int64, error) { return int64(r), nil }
func (ambientAffectedRows) LastInsertId() (int64, error)   { return 0, errors.New("not applicable") }

func ambientProbeDB(t *testing.T, pool *ambientSQLProbe, dryRun bool) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{DisableAutomaticPing: true, SkipDefaultTransaction: true, DryRun: dryRun, Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func TestAmbientContextGormOutcomeUsesConditionalUpdateAndScopedContext(t *testing.T) {
	for _, rows := range []int64{0, 1, 2} {
		pool := &ambientSQLProbe{rows: rows}
		root := &GormRepository{db: ambientProbeDB(t, pool, false)}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		scoped, err := root.withScanContext(ctx)
		if err != nil {
			t.Fatal(err)
		}
		started := time.Now().UTC().Add(-time.Minute)
		completed := time.Now().UTC()
		scan := &models.AmbientScan{ID: uuid.New(), OwnerIdentity: "alice", StartedAt: started, CompletedAt: &completed, Status: "failed", ErrorMessage: "token=must-not-be-stored"}
		updated, err := scoped.UpdateScan(scan)
		if rows == 1 {
			if err != nil || updated != scan {
				t.Fatal("acknowledged scan update discarded")
			}
		} else if !errors.Is(err, ErrScanOutcomeUnconfirmed) || updated != nil {
			t.Fatal("unacknowledged scan update accepted")
		}
		if pool.ctx != ctx || root.db.Statement.Context == ctx || !strings.HasPrefix(pool.query, "UPDATE ") || strings.Contains(pool.query, "INSERT") || !strings.Contains(pool.query, "owner_identity = ") || !strings.Contains(pool.query, "started_at = ") || !strings.Contains(pool.query, "status = ") {
			t.Fatalf("outcome lost context/fence or gained upsert: %s", pool.query)
		}
		for _, arg := range pool.args {
			if text, ok := arg.(string); ok && strings.Contains(text, "must-not-be-stored") {
				t.Fatal("SQL retained recognized credential")
			}
		}
		args := pool.args
		if args[len(args)-4] != scan.ID || args[len(args)-3] != "alice" || args[len(args)-2] != started.Truncate(time.Microsecond) || args[len(args)-1] != "running" {
			t.Fatal("conditional outcome identity/state changed")
		}
	}
}

func TestAmbientContextGormRefusesInvalidRootsAndOutcomes(t *testing.T) {
	for _, root := range []*GormRepository{nil, {}, {db: &gorm.DB{}}} {
		if _, err := root.withScanContext(context.Background()); !errors.Is(err, ErrScanContextUnavailable) {
			t.Fatal("invalid root accepted")
		}
	}
	pool := &ambientSQLProbe{rows: 1}
	repo := &GormRepository{db: ambientProbeDB(t, pool, false)}
	if _, err := repo.withScanContext(nil); !errors.Is(err, ErrScanContextUnavailable) {
		t.Fatal("nil context accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := repo.withScanContext(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled storage scope accepted")
	}
	if _, err := repo.UpdateScan(&models.AmbientScan{ID: uuid.New(), Status: "running"}); !errors.Is(err, ErrScanOutcomeUnconfirmed) || pool.query != "" {
		t.Fatal("nonterminal/invalid outcome performed SQL")
	}
}

func TestAmbientContextStorageRefusesDryRunAndMissingDialect(t *testing.T) {
	for _, scenario := range []string{"dry-run", "missing-dialect"} {
		t.Run(scenario, func(t *testing.T) {
			pool := &ambientSQLProbe{rows: 1}
			db := ambientProbeDB(t, pool, scenario == "dry-run")
			if scenario == "missing-dialect" {
				db.Config.Dialector = nil
			}
			root := &GormRepository{db: db}
			if _, err := root.withScanContext(context.Background()); !errors.Is(err, ErrScanContextUnavailable) || pool.query != "" {
				t.Fatal("nonpersisting storage accepted as a scan execution scope")
			}
		})
	}
}

func TestAmbientContextRetentionSQLScopesOwnerAndExcludesRunningScans(t *testing.T) {
	db := ambientProbeDB(t, &ambientSQLProbe{}, true)
	queries := []string{}
	vars := [][]interface{}{}
	capture := func(tx *gorm.DB) {
		queries = append(queries, tx.Statement.SQL.String())
		vars = append(vars, append([]interface{}{}, tx.Statement.Vars...))
	}
	if err := db.Callback().Query().After("gorm:query").Register("ambient-retention-query", capture); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Delete().After("gorm:delete").Register("ambient-retention-delete", capture); err != nil {
		t.Fatal(err)
	}
	repo := &GormRepository{db: db}
	if err := repo.PruneScansForOwner(" alice ", 10); err != nil {
		t.Fatal(err)
	}
	if len(queries) != 2 || !strings.HasPrefix(queries[1], "DELETE ") {
		t.Fatalf("retention query path changed: %v", queries)
	}
	for index, query := range queries {
		if !strings.Contains(query, "status IN ") || !strings.Contains(query, "owner_identity = ") {
			t.Fatalf("retention escaped owner/terminal scope: %s", query)
		}
		if vars[index][0] != "completed" || vars[index][1] != "failed" || vars[index][2] != "alice" {
			t.Fatal("retention includes running or foreign records")
		}
	}
	if !strings.Contains(queries[1], "AND (started_at < ") {
		t.Fatal("retention OR bypasses owner/terminal predicates")
	}
	if err := repo.PruneScansForOwner(" ", 10); err == nil || len(queries) != 2 {
		t.Fatal("ownerless personal retention allowed")
	}
}

type ambientOwnerRetentionProbe struct {
	*ambientRepositoryStub
	global int
	owners []string
}

func (r *ambientOwnerRetentionProbe) PruneScans(int) error { r.global++; return nil }
func (r *ambientOwnerRetentionProbe) PruneScansForOwner(owner string, _ int) error {
	r.owners = append(r.owners, owner)
	return nil
}

func TestAmbientContextPersonalScanNeverPrunesGlobalHistory(t *testing.T) {
	repo := &ambientOwnerRetentionProbe{ambientRepositoryStub: &ambientRepositoryStub{}}
	engine := NewServiceWithPursuits(repo, nil, nil, &ambientPursuitSpy{})
	if _, err := engine.ScanForOwner("alice", "test"); err != nil {
		t.Fatal(err)
	}
	if repo.global != 0 || len(repo.owners) != 1 || repo.owners[0] != "alice" {
		t.Fatal("personal scan performed global retention")
	}
}
