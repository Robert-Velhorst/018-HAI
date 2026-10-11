package operations

import (
	"context"
	"database/sql"
	"database/sql/driver"
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

func equalTimestampOperations(t *testing.T, count int) (*MemoryRepository, []models.Operation) {
	t.Helper()
	repo := NewMemoryRepository()
	at := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	ops := make([]models.Operation, count)
	for i := range ops {
		in := sampleInput()
		in.DedupeKey = fmt.Sprintf("tie-%d", i)
		op, err := NewOperation(in, at)
		if err != nil {
			t.Fatal(err)
		}
		op.ID = uuid.MustParse(fmt.Sprintf("00000000-0000-4000-8000-%012d", i+1))
		if _, err := repo.Create(&op); err != nil {
			t.Fatal(err)
		}
		ops[i] = op
	}
	return repo, ops
}

func TestOperationPaginationAndDashboardHaveDeterministicTimestampTies(t *testing.T) {
	repo, want := equalTimestampOperations(t, 31)
	for attempt := 0; attempt < 20; attempt++ {
		var all []models.Operation
		for offset := 0; offset < len(want); offset += 7 {
			page, err := repo.List(Filter{OwnerUserID: "user-1", WorkspaceID: "local", Offset: offset, Limit: 7})
			if err != nil {
				t.Fatal(err)
			}
			all = append(all, page...)
		}
		if !reflect.DeepEqual(all, want) {
			t.Fatal("equal timestamps produced missing, repeated or reordered rows")
		}
		dashboard, err := repo.Dashboard("user-1", "local")
		if err != nil || !reflect.DeepEqual(dashboard.Recent, want[:20]) {
			t.Fatalf("dashboard tie order: %+v / %v", dashboard.Recent, err)
		}
	}
	// Primary timestamp ordering must still override the secondary identity key.
	repo.mu.Lock()
	last := repo.ops[want[30].ID]
	last.UpdatedAt = last.UpdatedAt.Add(time.Second)
	repo.ops[last.ID] = last
	repo.mu.Unlock()
	page, err := repo.List(Filter{OwnerUserID: "user-1", WorkspaceID: "local", Offset: -5, Limit: 1})
	if err != nil || len(page) != 1 || page[0].ID != last.ID {
		t.Fatalf("primary sorting/negative offset: %+v / %v", page, err)
	}
}

func TestOperationDueSelectionExcludesFutureReviewAndKeepsStableTies(t *testing.T) {
	repo, all := equalTimestampOperations(t, 8)
	future := time.Now().UTC().Add(24 * time.Hour)
	past := time.Now().UTC().Add(-24 * time.Hour)
	repo.mu.Lock()
	for i := range all {
		op := repo.ops[all[i].ID]
		switch i {
		case 1:
			op.NextReviewAt = &future
		case 2:
			op.Status = string(StatusArchived)
		case 3:
			op.OwnerUserID = "other-owner"
		case 4:
			op.WorkspaceID = "other-workspace"
		case 5:
			op.NextReviewAt = &past
		}
		repo.ops[op.ID] = op
	}
	repo.mu.Unlock()
	want := []uuid.UUID{all[0].ID, all[5].ID, all[6].ID, all[7].ID}
	for attempt := 0; attempt < 20; attempt++ {
		got, err := repo.ListDue("user-1", "local", 200)
		if err != nil {
			t.Fatal(err)
		}
		ids := make([]uuid.UUID, len(got))
		for i := range got {
			ids[i] = got[i].ID
		}
		if !reflect.DeepEqual(ids, want) {
			t.Fatalf("due order or eligibility: %v, want %v", ids, want)
		}
	}
}

type orderingSQLLog struct{ statements []string }

func (l *orderingSQLLog) LogMode(logger.LogLevel) logger.Interface { return l }
func (*orderingSQLLog) Info(context.Context, string, ...any)       {}
func (*orderingSQLLog) Warn(context.Context, string, ...any)       {}
func (*orderingSQLLog) Error(context.Context, string, ...any)      {}
func (l *orderingSQLLog) Trace(_ context.Context, _ time.Time, query func() (string, int64), _ error) {
	sql, _ := query()
	l.statements = append(l.statements, sql)
}

type orderingQueryConnector struct{}
type orderingQueryDriver struct{}
type orderingQueryConnection struct{}
type orderingQueryTransaction struct{}
type orderingQueryRows struct{}

func (orderingQueryConnector) Connect(context.Context) (driver.Conn, error) {
	return orderingQueryConnection{}, nil
}
func (orderingQueryConnector) Driver() driver.Driver                { return orderingQueryDriver{} }
func (orderingQueryDriver) Open(string) (driver.Conn, error)        { return orderingQueryConnection{}, nil }
func (orderingQueryConnection) Prepare(string) (driver.Stmt, error) { return nil, driver.ErrSkip }
func (orderingQueryConnection) Begin() (driver.Tx, error)           { return orderingQueryTransaction{}, nil }
func (orderingQueryConnection) Close() error                        { return nil }
func (orderingQueryConnection) QueryContext(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
	return orderingQueryRows{}, nil
}
func (orderingQueryTransaction) Commit() error      { return nil }
func (orderingQueryTransaction) Rollback() error    { return nil }
func (orderingQueryRows) Columns() []string         { return []string{"key", "n"} }
func (orderingQueryRows) Close() error              { return nil }
func (orderingQueryRows) Next([]driver.Value) error { return io.EOF }

func TestPostgresOperationQueriesKeepDeterministicSecondaryOrdering(t *testing.T) {
	log := &orderingSQLLog{}
	// A recording driver supports Dashboard's Scan calls, which GORM's DryRun
	// refuses. It returns empty rows, performs no network I/O and proves only SQL.
	pool := sql.OpenDB(orderingQueryConnector{})
	t.Cleanup(func() { _ = pool.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}),
		&gorm.Config{DisableAutomaticPing: true, Logger: log})
	if err != nil {
		t.Fatal(err)
	}
	repo := NewGormRepository(db)
	for _, query := range []struct {
		name, order string
		call        func() error
	}{
		{"claim-next", "ORDER BY created_at ASC, updated_at ASC, id ASC", func() error {
			_, err := repo.ClaimNext(context.Background(), "user-1", "local", uuid.New(), time.Minute)
			return err
		}},
		{"list", "ORDER BY updated_at DESC,id ASC", func() error {
			_, err := repo.List(Filter{OwnerUserID: "user-1", WorkspaceID: "local", Offset: -5, Limit: 7})
			return err
		}},
		{"due", "ORDER BY created_at ASC,id ASC", func() error { _, err := repo.ListDue("user-1", "local", 7); return err }},
		{"dashboard", "ORDER BY updated_at DESC,id ASC", func() error { _, err := repo.Dashboard("user-1", "local"); return err }},
	} {
		t.Run(query.name, func(t *testing.T) {
			log.statements = nil
			if err := query.call(); err != nil {
				t.Fatal(err)
			}
			if len(log.statements) == 0 {
				t.Fatal("no SQL was inspected")
			}
			sql := log.statements[len(log.statements)-1]
			if !strings.Contains(sql, query.order) || strings.Contains(sql, "OFFSET -") || !strings.Contains(sql, "owner_user_id = 'user-1' AND workspace_id = 'local'") {
				t.Fatalf("unstable or unscoped SQL: %s", sql)
			}
			if query.name == "due" && !strings.Contains(sql, "next_review_at IS NULL OR next_review_at <= now()") {
				t.Fatalf("due filter missing: %s", sql)
			}
			if query.name == "claim-next" &&
				(!strings.Contains(sql, "(status = 'approved' OR next_review_at IS NULL OR next_review_at <= clock_timestamp())") ||
					!strings.Contains(sql, "payload_json ? 'sourceApproval'")) {
				t.Fatalf("claim query must let only receipt-backed approval bypass a review reminder: %s", sql)
			}
		})
	}
}
