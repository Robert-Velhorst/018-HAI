package executionauth

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"strings"
	"testing"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestApprovalClaimReadFailsClosed(t *testing.T) {
	queryFailure := errors.New("approval lookup unavailable")
	for _, test := range []struct {
		name     string
		queryErr error
		values   []driver.Value
		wantErr  bool
	}{
		{name: "query failure", queryErr: queryFailure, wantErr: true},
		{name: "scan failure", values: []driver.Value{nil, "decision"}, wantErr: true},
		{name: "missing receipt"},
		{name: "receipt without approval", values: []driver.Value{"", ""}},
	} {
		t.Run(test.name, func(t *testing.T) {
			pool := sql.OpenDB(claimReadConnector{queryErr: test.queryErr, values: test.values})
			t.Cleanup(func() { _ = pool.Close() })
			db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{
				DisableAutomaticPing: true,
				Logger:               logger.Default.LogMode(logger.Silent),
			})
			if err != nil {
				t.Fatal(err)
			}
			err = lockAndCheckApprovalClaim(context.Background(), db, Consumption{})
			if test.wantErr {
				if err == nil || !strings.Contains(err.Error(), "read approval claim identity") {
					t.Fatalf("expected approval read failure, got %v", err)
				}
				if test.queryErr != nil && !errors.Is(err, test.queryErr) {
					t.Fatalf("query failure was not preserved: %v", err)
				}
			} else if err != nil {
				t.Fatalf("expected existing no-approval behavior, got %v", err)
			}
		})
	}
}

// Exercise database/sql query and scan failures without requiring a live database.
type claimReadConnector struct {
	queryErr error
	values   []driver.Value
}

func (c claimReadConnector) Connect(context.Context) (driver.Conn, error) {
	return claimReadConn{c}, nil
}
func (c claimReadConnector) Driver() driver.Driver { return claimReadDriver{c} }

type claimReadDriver struct{ connector claimReadConnector }

func (d claimReadDriver) Open(string) (driver.Conn, error) {
	return d.connector.Connect(context.Background())
}

type claimReadConn struct{ connector claimReadConnector }

func (claimReadConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (claimReadConn) Close() error              { return nil }
func (claimReadConn) Begin() (driver.Tx, error) { return nil, errors.New("unexpected transaction") }
func (c claimReadConn) QueryContext(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
	if c.connector.queryErr != nil {
		return nil, c.connector.queryErr
	}
	return &claimReadRows{values: c.connector.values}, nil
}

type claimReadRows struct{ values []driver.Value }

func (*claimReadRows) Columns() []string { return []string{"approval_source_id", "decision_id"} }
func (*claimReadRows) Close() error      { return nil }
func (r *claimReadRows) Next(values []driver.Value) error {
	if r.values == nil {
		return io.EOF
	}
	copy(values, r.values)
	r.values = nil
	return nil
}
