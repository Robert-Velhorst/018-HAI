package semantic

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type cleanupDBState struct {
	mu            sync.Mutex
	tableExists   bool
	tableCheckErr error
	deleteErr     error
	tableChecks   int
	deleteCalls   int
}

type cleanupDBDriver struct{ state *cleanupDBState }

type cleanupDBConn struct{ state *cleanupDBState }

type cleanupDBRows struct {
	value driver.Value
	done  bool
}

type cleanupDBTx struct{}

type cleanupDBStmt struct {
	conn  *cleanupDBConn
	query string
}

var cleanupDBDriverID atomic.Uint64

func (d cleanupDBDriver) Open(string) (driver.Conn, error) {
	return &cleanupDBConn{state: d.state}, nil
}

func (c *cleanupDBConn) Prepare(query string) (driver.Stmt, error) {
	return &cleanupDBStmt{conn: c, query: query}, nil
}

func (*cleanupDBConn) Close() error { return nil }

func (*cleanupDBConn) Begin() (driver.Tx, error) { return cleanupDBTx{}, nil }

func (*cleanupDBConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return cleanupDBTx{}, nil
}

func (c *cleanupDBConn) QueryContext(ctx context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.state.mu.Lock()
	defer c.state.mu.Unlock()
	if !strings.Contains(query, "to_regclass('semantic_memory_embeddings')") {
		return nil, fmt.Errorf("unexpected cleanup query: %s", query)
	}
	c.state.tableChecks++
	if c.state.tableCheckErr != nil {
		return nil, c.state.tableCheckErr
	}
	return &cleanupDBRows{value: c.state.tableExists}, nil
}

func (c *cleanupDBConn) ExecContext(ctx context.Context, query string, _ []driver.NamedValue) (driver.Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.state.mu.Lock()
	defer c.state.mu.Unlock()
	if !strings.Contains(query, "DELETE FROM semantic_memory_embeddings WHERE memory_id =") {
		return nil, fmt.Errorf("unexpected cleanup statement: %s", query)
	}
	c.state.deleteCalls++
	if c.state.deleteErr != nil {
		return nil, c.state.deleteErr
	}
	return driver.RowsAffected(1), nil
}

func (s *cleanupDBStmt) Close() error { return nil }
func (*cleanupDBStmt) NumInput() int  { return -1 }
func (s *cleanupDBStmt) Exec(args []driver.Value) (driver.Result, error) {
	named := cleanupNamedValues(args)
	return s.conn.ExecContext(context.Background(), s.query, named)
}
func (s *cleanupDBStmt) Query(args []driver.Value) (driver.Rows, error) {
	named := cleanupNamedValues(args)
	return s.conn.QueryContext(context.Background(), s.query, named)
}

func cleanupNamedValues(args []driver.Value) []driver.NamedValue {
	named := make([]driver.NamedValue, len(args))
	for i, value := range args {
		named[i] = driver.NamedValue{Ordinal: i + 1, Value: value}
	}
	return named
}

func (cleanupDBTx) Commit() error   { return nil }
func (cleanupDBTx) Rollback() error { return nil }

func (r *cleanupDBRows) Columns() []string { return []string{"exists"} }
func (*cleanupDBRows) Close() error        { return nil }
func (r *cleanupDBRows) Next(dest []driver.Value) error {
	if r.done {
		return io.EOF
	}
	r.done = true
	dest[0] = r.value
	return nil
}

func newCleanupTestDB(t *testing.T, state *cleanupDBState) *gorm.DB {
	t.Helper()
	driverName := fmt.Sprintf("semantic-cleanup-test-%d", cleanupDBDriverID.Add(1))
	sql.Register(driverName, cleanupDBDriver{state: state})
	sqlDB, err := sql.Open(driverName, "")
	if err != nil {
		t.Fatalf("open test SQL database: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{
		DisableAutomaticPing:   true,
		SkipDefaultTransaction: true,
	})
	if err != nil {
		t.Fatalf("open test GORM database: %v", err)
	}
	return db
}

func TestDisabledDeleteMemoryUsesDatabaseOnlyCleanupAndIsIdempotent(t *testing.T) {
	state := &cleanupDBState{tableExists: true}
	db := newCleanupTestDB(t, state)
	providerCalls := 0
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		providerCalls++
		_, _ = w.Write([]byte(`{"data":[{"embedding":[0.25]}]}`))
	}))
	defer provider.Close()

	service, err := NewService(db, Config{Enabled: false, BaseURL: provider.URL, Model: "local-embed"})
	if err != nil {
		t.Fatalf("create disabled semantic service: %v", err)
	}
	if service.Enabled() {
		t.Fatal("retrieval should remain disabled")
	}
	memoryID := uuid.New()
	for range 2 {
		if err := service.DeleteMemory(context.Background(), memoryID); err != nil {
			t.Fatalf("database-only delete: %v", err)
		}
	}
	if providerCalls != 0 {
		t.Fatalf("disabled cleanup called embedding provider %d times", providerCalls)
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.tableChecks != 2 || state.deleteCalls != 2 {
		t.Fatalf("cleanup checks=%d deletes=%d; want 2 each for idempotent retry", state.tableChecks, state.deleteCalls)
	}
}

func TestDisabledDeleteMemorySucceedsWhenEmbeddingTableIsAbsent(t *testing.T) {
	state := &cleanupDBState{}
	service, err := NewService(newCleanupTestDB(t, state), Config{Enabled: false})
	if err != nil {
		t.Fatalf("create disabled semantic service: %v", err)
	}
	if err := service.DeleteMemory(context.Background(), uuid.New()); err != nil {
		t.Fatalf("delete without an embedding table: %v", err)
	}
	if state.tableChecks != 1 || state.deleteCalls != 0 {
		t.Fatalf("cleanup checks=%d deletes=%d; want one check and no delete", state.tableChecks, state.deleteCalls)
	}
}

func TestDisabledServiceFromConfigRetainsDatabaseForCleanup(t *testing.T) {
	state := &cleanupDBState{tableExists: true}
	db := newCleanupTestDB(t, state)
	service := serviceFromConfig(db, Config{Enabled: false}, nil)
	if service.Enabled() {
		t.Fatal("retrieval should remain disabled")
	}
	if err := service.DeleteMemory(context.Background(), uuid.New()); err != nil {
		t.Fatalf("cleanup through environment-configured disabled service: %v", err)
	}
	if state.deleteCalls != 1 {
		t.Fatalf("database deletes=%d, want 1", state.deleteCalls)
	}
}

func TestDisabledDeleteMemoryFailsClosedWhenCleanupCannotBeConfirmed(t *testing.T) {
	t.Run("missing database handle", func(t *testing.T) {
		service := disabledService{reason: "database unavailable"}
		if err := service.DeleteMemory(context.Background(), uuid.New()); err == nil {
			t.Fatal("cleanup reported success without a database handle")
		}
	})
	t.Run("table check failure", func(t *testing.T) {
		state := &cleanupDBState{tableCheckErr: errors.New("database unavailable")}
		service, err := NewService(newCleanupTestDB(t, state), Config{Enabled: false})
		if err != nil {
			t.Fatalf("create disabled semantic service: %v", err)
		}
		if err := service.DeleteMemory(context.Background(), uuid.New()); err == nil {
			t.Fatal("cleanup reported success after table check failure")
		}
		if state.deleteCalls != 0 {
			t.Fatalf("issued delete after failed table check: %d", state.deleteCalls)
		}
	})
	t.Run("delete failure", func(t *testing.T) {
		state := &cleanupDBState{tableExists: true, deleteErr: errors.New("database unavailable")}
		service, err := NewService(newCleanupTestDB(t, state), Config{Enabled: false})
		if err != nil {
			t.Fatalf("create disabled semantic service: %v", err)
		}
		if err := service.DeleteMemory(context.Background(), uuid.New()); err == nil {
			t.Fatal("cleanup reported success after delete failure")
		}
		if state.deleteCalls != 1 {
			t.Fatalf("delete attempts=%d, want 1 retryable attempt", state.deleteCalls)
		}
	})
}

func TestValidateLocalURL(t *testing.T) {
	for _, raw := range []string{"http://127.0.0.1:8080", "http://host.docker.internal:8080", "https://localhost/v1"} {
		if err := validateLocalURL(raw); err != nil {
			t.Fatalf("%s should be allowed: %v", raw, err)
		}
	}
	for _, raw := range []string{"https://api.example.com", "http://169.254.169.254", "http://user:secret@localhost"} {
		if err := validateLocalURL(raw); err == nil {
			t.Fatalf("%s should be rejected", raw)
		}
	}
}

func TestVectorLiteralAndInputTrimming(t *testing.T) {
	if got := vectorLiteral([]float64{1, -0.5, 0.25}); got != "[1,-0.5,0.25]" {
		t.Fatalf("vector literal = %q", got)
	}
	if got := trimRunes("abcdef", 3); got != "abc" {
		t.Fatalf("trimmed input = %q", got)
	}

}

func TestBuildMemorySearchQueryQuarantinesOwnerlessMemories(t *testing.T) {
	query, args := buildMemorySearchQuery(
		MemorySearchRequest{OwnerIdentity: " alice ", ProjectKey: "legal-case"},
		"local-embed",
		[]float64{0.25, 0.75},
		8,
	)
	if !strings.Contains(query, "AND cm.owner_identity = ?") {
		t.Fatalf("owner-scoped query is missing the exact owner predicate: %s", query)
	}
	if strings.Contains(query, "cm.owner_identity = ''") || strings.Contains(query, "cm.owner_identity IS NULL") {
		t.Fatalf("owner-scoped query includes quarantined ownerless memories: %s", query)
	}
	if len(args) != 6 || args[0] != "[0.25,0.75]" || args[1] != "local-embed" || args[2] != "legal-case" || args[3] != "alice" || args[4] != "[0.25,0.75]" || args[5] != 8 {
		t.Fatalf("owner-scoped query args = %#v", args)
	}
}

func TestBuildMemorySearchQueryLeavesTrustedUnscopedSearchAvailable(t *testing.T) {
	query, args := buildMemorySearchQuery(
		MemorySearchRequest{ProjectKey: "legal-case"},
		"local-embed",
		[]float64{0.5},
		4,
	)
	if strings.Contains(query, "cm.owner_identity") {
		t.Fatalf("trusted unscoped query unexpectedly applies an owner filter: %s", query)
	}
	if len(args) != 5 || args[2] != "legal-case" || args[4] != 4 {
		t.Fatalf("trusted unscoped query args = %#v", args)
	}
}

func TestEmbedUsesLocalEndpointAndBearer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/embeddings" || r.Header.Get("Authorization") != "Bearer local-key" {
			t.Fatalf("unexpected embedding request: %s authorization=%q", r.URL.Path, r.Header.Get("Authorization"))
		}
		_, _ = w.Write([]byte(`{"data":[{"embedding":[0.25,0.75]}]}`))
	}))
	defer server.Close()

	service := &service{config: Config{BaseURL: server.URL, Model: "local-embed", APIKey: "local-key", InputLimit: 12000}, client: server.Client()}
	vector, err := service.embed(context.Background(), "source text")
	if err != nil || len(vector) != 2 || vector[0] != 0.25 {
		t.Fatalf("embedding = %#v, %v", vector, err)
	}
}
