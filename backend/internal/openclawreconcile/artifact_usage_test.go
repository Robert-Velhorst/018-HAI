package openclawreconcile

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
)

// Use a database/sql result fixture so this runs Gorm's real Scan path without
// requiring a database server or replacing Gorm's destination handling.
func TestArtifactUsageKeepsPolicyLimitsAfterAggregateScan(t *testing.T) {
	connection := sql.OpenDB(artifactUsageConnector{t: t})
	t.Cleanup(func() { _ = connection.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: connection}), &gorm.Config{DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	archive := &ArtifactArchive{repo: NewRepository(db)}
	usage, err := archive.Usage(context.Background(), "fixture-owner")
	if err != nil {
		t.Fatal(err)
	}
	if usage.BytesUsed != 512 || usage.FilesUsed != 2 {
		t.Fatalf("aggregate values lost: %#v", usage)
	}
	if usage.BytesLimit != 64<<20 || usage.FilesLimit != 128 {
		t.Fatalf("aggregate Scan cleared storage policy limits: %#v", usage)
	}
}

type artifactUsageConnector struct{ t *testing.T }

func (c artifactUsageConnector) Connect(context.Context) (driver.Conn, error) {
	return artifactUsageConnection{t: c.t}, nil
}
func (c artifactUsageConnector) Driver() driver.Driver { return artifactUsageDriver{t: c.t} }

type artifactUsageDriver struct{ t *testing.T }

func (d artifactUsageDriver) Open(string) (driver.Conn, error) {
	return artifactUsageConnection{t: d.t}, nil
}

type artifactUsageConnection struct{ t *testing.T }

func (artifactUsageConnection) Close() error { return nil }
func (artifactUsageConnection) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepared statement")
}
func (artifactUsageConnection) Begin() (driver.Tx, error) {
	return nil, errors.New("unexpected transaction")
}
func (c artifactUsageConnection) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if !strings.Contains(query, "owner_identity = $1") || len(args) != 1 || args[0].Value != "fixture-owner" {
		c.t.Errorf("usage aggregate did not bind the exact owner: %s %#v", query, args)
		return nil, errors.New("unexpected owner query")
	}
	return &artifactUsageRows{}, nil
}

type artifactUsageRows struct{ read bool }

func (*artifactUsageRows) Columns() []string { return []string{"bytes_used", "files_used"} }
func (*artifactUsageRows) Close() error      { return nil }
func (r *artifactUsageRows) Next(values []driver.Value) error {
	if r.read {
		return io.EOF
	}
	r.read = true
	values[0], values[1] = int64(512), int64(2)
	return nil
}
