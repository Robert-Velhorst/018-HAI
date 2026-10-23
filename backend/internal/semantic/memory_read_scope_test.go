package semantic

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type memoryReadScopeState struct {
	id      uuid.UUID
	queries []string
	args    [][]driver.NamedValue
}

type memoryReadScopeDriver struct{ state *memoryReadScopeState }
type memoryReadScopeConn struct {
	*cleanupDBConn
	state *memoryReadScopeState
}
type memoryReadScopeRows struct {
	columns []string
	values  []driver.Value
	done    bool
}

func (d memoryReadScopeDriver) Open(string) (driver.Conn, error) {
	return &memoryReadScopeConn{cleanupDBConn: &cleanupDBConn{state: &cleanupDBState{}}, state: d.state}, nil
}

func (c *memoryReadScopeConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.state.queries = append(c.state.queries, query)
	c.state.args = append(c.state.args, append([]driver.NamedValue(nil), args...))
	if strings.Contains(query, "FROM semantic_memory_embeddings") {
		return &memoryReadScopeRows{columns: []string{"memory_id", "similarity"}, values: []driver.Value{c.state.id.String(), float64(0.9)}}, nil
	}
	if strings.Contains(query, "FROM semantic_embeddings") {
		return &memoryReadScopeRows{columns: []string{"extraction_id", "similarity"}, values: []driver.Value{c.state.id.String(), float64(0.9)}}, nil
	}
	if strings.Contains(query, `FROM "context_memories"`) {
		return &memoryReadScopeRows{columns: []string{"id", "owner_identity", "project_key", "archived"}, values: []driver.Value{c.state.id.String(), "alice", "case", false}}, nil
	}
	if strings.Contains(query, `FROM "source_extractions"`) {
		return &memoryReadScopeRows{columns: []string{"id", "project_key", "sensitive", "archived"}, values: []driver.Value{c.state.id.String(), "case", false, false}}, nil
	}
	return nil, fmt.Errorf("unexpected memory read query")
}

func TestSearchRechecksSourceEligibilityWhenLoadingRankedRecords(t *testing.T) {
	for _, request := range []SearchRequest{
		{OwnerIdentity: " alice ", ProjectKey: " case ", Query: "evidence"},
		{OwnerIdentity: "alice", Query: "evidence", IncludeSensitive: true},
		{Query: "evidence"},
	} {
		t.Run(request.OwnerIdentity+"/"+request.ProjectKey, func(t *testing.T) {
			state := &memoryReadScopeState{id: uuid.New()}
			name := fmt.Sprintf("semantic-source-scope-%d", cleanupDBDriverID.Add(1))
			sql.Register(name, memoryReadScopeDriver{state: state})
			pool, err := sql.Open(name, "")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = pool.Close() })
			db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{DisableAutomaticPing: true})
			if err != nil {
				t.Fatal(err)
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`{"data":[{"embedding":[0.25,0.75]}]}`))
			}))
			defer server.Close()
			svc := &service{db: db, config: Config{BaseURL: server.URL, Model: "local-embed", InputLimit: 12000}, client: server.Client()}
			matches, err := svc.Search(context.Background(), request)
			if err != nil || len(matches) != 1 || matches[0].Extraction.ID != state.id || len(state.queries) != 2 {
				t.Fatalf("ranked source read failed: matches=%v queries=%d err=%v", matches, len(state.queries), err)
			}
			query, args := state.queries[1], state.args[1]
			for _, predicate := range []string{"JOIN connected_sources cs", "source_extractions.archived =", "cs.revoked_at IS NULL", "LOWER(COALESCE(cs.status, '')) <> 'revoked'"} {
				if !strings.Contains(query, predicate) {
					t.Fatalf("second source read lost eligibility predicate: %s", predicate)
				}
			}
			if !request.IncludeSensitive && !strings.Contains(query, "source_extractions.sensitive =") {
				t.Fatal("second source read lost sensitive-data filter")
			}
			if request.IncludeSensitive && strings.Contains(query, "source_extractions.sensitive =") {
				t.Fatal("explicit sensitive read unexpectedly changed contract")
			}
			if owner := strings.TrimSpace(request.OwnerIdentity); owner != "" && (!strings.Contains(query, "cs.owner_identity =") || args[len(args)-1].Value != owner) {
				t.Fatal("second source read lost owner filter")
			}
			if strings.TrimSpace(request.ProjectKey) != "" && !strings.Contains(query, "source_extractions.project_key =") {
				t.Fatal("second source read lost project filter")
			}
		})
	}
}

func (r *memoryReadScopeRows) Columns() []string { return r.columns }
func (*memoryReadScopeRows) Close() error        { return nil }
func (r *memoryReadScopeRows) Next(dest []driver.Value) error {
	if r.done {
		return io.EOF
	}
	r.done = true
	copy(dest, r.values)
	return nil
}

func TestSearchMemoryRechecksScopeWhenLoadingRankedRecords(t *testing.T) {
	for _, request := range []MemorySearchRequest{
		{OwnerIdentity: " alice ", ProjectKey: " case ", Query: "evidence"},
		{OwnerIdentity: "alice", Query: "evidence"},
		{Query: "evidence"},
	} {
		t.Run(request.OwnerIdentity+"/"+request.ProjectKey, func(t *testing.T) {
			state := &memoryReadScopeState{id: uuid.New()}
			name := fmt.Sprintf("semantic-memory-scope-%d", cleanupDBDriverID.Add(1))
			sql.Register(name, memoryReadScopeDriver{state: state})
			pool, err := sql.Open(name, "")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = pool.Close() })
			db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{DisableAutomaticPing: true})
			if err != nil {
				t.Fatal(err)
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`{"data":[{"embedding":[0.25,0.75]}]}`))
			}))
			defer server.Close()
			svc := &service{db: db, config: Config{BaseURL: server.URL, Model: "local-embed", InputLimit: 12000}, client: server.Client()}
			matches, err := svc.SearchMemory(context.Background(), request)
			if err != nil || len(matches) != 1 || matches[0].Memory.ID != state.id || len(state.queries) != 2 {
				t.Fatalf("ranked memory read failed: matches=%v queries=%d err=%v", matches, len(state.queries), err)
			}
			query, args := state.queries[1], state.args[1]
			if !strings.Contains(query, "archived =") || len(args) < 2 || args[1].Value != false {
				t.Fatal("second memory read did not recheck archival eligibility")
			}
			owner, project := strings.TrimSpace(request.OwnerIdentity), strings.TrimSpace(request.ProjectKey)
			if owner != "" && (!strings.Contains(query, "owner_identity =") || len(args) < 3 || args[2].Value != owner) {
				t.Fatal("second memory read did not recheck exact owner identity")
			}
			if owner == "" && strings.Contains(query, "owner_identity") {
				t.Fatal("trusted unscoped read unexpectedly changed contract")
			}
			if project != "" && (!strings.Contains(query, "project_key =") || !strings.Contains(query, "project_key IS NULL") || args[len(args)-1].Value != project) {
				t.Fatal("second memory read lost project and shared-memory eligibility")
			}
		})
	}
}
