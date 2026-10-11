package migrations_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestIsolatedMigrationFailurePreservesClassWithoutPrivateDiagnostics(t *testing.T) {
	private := "postgres://operator:do-not-log@127.0.0.1/private"
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{"identity", nil, "unexpected database identity"},
		{"connection", errors.New(private), "connection or database I/O failure; private diagnostics withheld"},
		{"deadline", fmt.Errorf("%s: %w", private, context.DeadlineExceeded), "deadline exceeded"},
		{"cancelled", errors.Join(context.Canceled, errors.New(private)), "cancelled"},
		{"sqlstate", fmt.Errorf("%s: %w", private, &pgconn.PgError{Code: "23505", Message: private, Detail: private}), "SQLSTATE 23505"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := isolatedMigrationFailure(tc.err); got != tc.want || strings.Contains(got, private) {
				t.Fatalf("safe diagnostic differs: got=%q want=%q", got, tc.want)
			}
		})
	}
}

func TestIsolatedMigrationDatabaseConfigurationIsDedicatedAndBounded(t *testing.T) {
	for _, host := range []string{"127.0.0.1", "::1"} {
		config, err := isolatedMigrationDatabaseConfig("host=" + host + " user=test dbname=hai_migration_runner_test sslmode=disable statement_timeout=0 lock_timeout=0 search_path=private")
		if err != nil {
			t.Fatal(err)
		}
		if config.Database != "hai_migration_runner_test" || config.Host != host || config.ConnectTimeout != 5*time.Second ||
			config.DefaultQueryExecMode != pgx.QueryExecModeSimpleProtocol || config.RuntimeParams["search_path"] != "public,pg_catalog" ||
			config.RuntimeParams["statement_timeout"] != "120000" || config.RuntimeParams["lock_timeout"] != "5000" ||
			config.RuntimeParams["idle_in_transaction_session_timeout"] != "120000" {
			t.Fatal("isolated migration configuration lost identity, namespace or deadlines")
		}
	}
	for _, dsn := range []string{
		"", "malformed-dsn", "host=localhost dbname=hai_migration_runner_test",
		"host=127.0.0.1 dbname=hai_migration_runner_test_extra",
		"host=127.0.0.1 dbname=hai_production",
		"host=192.0.2.10 dbname=hai_migration_runner_test",
		"host=127.0.0.1,192.0.2.10 dbname=hai_migration_runner_test",
		"host=127.0.0.1,::1 dbname=hai_migration_runner_test",
		"host=/tmp dbname=hai_migration_runner_test",
	} {
		config, err := isolatedMigrationDatabaseConfig(dsn)
		if err == nil || config != nil {
			t.Fatal("unsafe DSN yielded an executable test connection configuration")
		}
	}
}

func TestIsolatedMigrationDatabaseDisabledBeforeProvisioning(t *testing.T) {
	t.Setenv("HAI_ALLOW_DESTRUCTIVE_DATABASE_TESTS", "false")
	t.Setenv("HAI_TEST_DATABASE_DSN", "host=127.0.0.1 port=1 dbname=hai_migration_runner_test connect_timeout=1 sslmode=disable")
	returned := false
	t.Run("explicitly_disabled", func(t *testing.T) {
		openIsolatedMigrationDatabase(t)
		returned = true
	})
	if returned {
		t.Fatal("disabled helper reached database provisioning")
	}
}

// Source contract for cleanup authority/order, not executed PostgreSQL proof.
func TestIsolatedMigrationDatabaseGuardPrecedesConnectionsAndForbidsForce(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "postgres_test_database_test.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var helper *ast.FuncDecl
	for _, declaration := range file.Decls {
		if function, ok := declaration.(*ast.FuncDecl); ok && function.Name.Name == "openIsolatedMigrationDatabase" {
			helper = function
		}
	}
	if helper == nil {
		t.Fatal("missing isolated migration database helper")
	}
	render := func(node ast.Node) string {
		var out bytes.Buffer
		// Compared expressions come from separate parses, not shared positions.
		if err := format.Node(&out, token.NewFileSet(), node); err != nil {
			t.Fatal(err)
		}
		return strings.Join(strings.Fields(out.String()), " ")
	}
	matches := func(node ast.Node, expression string) bool {
		want, err := parser.ParseExpr(expression)
		if err != nil {
			t.Fatal(err)
		}
		return render(node) == render(want)
	}
	var guard token.Pos
	var connections []token.Pos
	var drop *ast.CallExpr
	assignments := make(map[string][]*ast.AssignStmt)
	caps := make(map[string]int)
	var literals strings.Builder
	ast.Inspect(helper, func(node ast.Node) bool {
		if assignment, ok := node.(*ast.AssignStmt); ok {
			for _, lhs := range assignment.Lhs {
				key := render(lhs)
				if key == "adminConfig.Database" {
					t.Fatal("administration database must never be rewritten")
				}
				assignments[key] = append(assignments[key], assignment)
			}
		}
		if call, ok := node.(*ast.CallExpr); ok {
			switch render(call.Fun) {
			case "pgtestguard.RequireDedicatedPostgresTestDSN":
				if guard != token.NoPos || !matches(call, `pgtestguard.RequireDedicatedPostgresTestDSN(t, "HAI_TEST_DATABASE_DSN", "hai_migration_runner_test")`) {
					t.Fatal("helper must invoke the exact dedicated guard once")
				}
				guard = call.Pos()
			case "stdlib.OpenDB":
				connections = append(connections, call.Pos())
			case "adminSQL.SetMaxOpenConns", "adminSQL.SetMaxIdleConns", "testSQL.SetMaxOpenConns", "testSQL.SetMaxIdleConns":
				want := "4"
				if strings.HasPrefix(render(call.Fun), "adminSQL.") {
					want = "1"
				}
				if len(call.Args) != 1 || render(call.Args[0]) != want {
					t.Fatal("pool caps must not be overridden")
				}
			case "adminSQL.ExecContext":
				if len(call.Args) > 1 && strings.Contains(render(call.Args[1]), "DROP DATABASE") {
					if drop != nil || !matches(call, `adminSQL.ExecContext(cleanupCtx, "DROP DATABASE " + quotedDatabase)`) {
						t.Fatal("DROP must occur once and target only the safely quoted owned database")
					}
					drop = call
				}
			}
			caps[render(call)]++
		}
		if literal, ok := node.(*ast.BasicLit); ok && literal.Kind == token.STRING {
			literals.WriteString(literal.Value)
			literals.WriteByte('\n')
		}
		return true
	})
	if guard == token.NoPos || len(connections) != 2 || drop == nil {
		t.Fatal("helper must guard two pools and define one owned database drop")
	}
	for _, connect := range connections {
		if guard >= connect {
			t.Fatal("dedicated destructive-test guard must precede every pool")
		}
	}
	for key, want := range map[string]string{
		"databaseName":        `"hai_migration_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")`,
		"quotedDatabase":      `pgx.Identifier{databaseName}.Sanitize()`,
		"testConfig.Database": "databaseName",
	} {
		got := assignments[key]
		before := drop.Pos()
		if key == "testConfig.Database" {
			before = connections[1]
		}
		if len(got) != 1 || len(got[0].Rhs) != 1 || !matches(got[0].Rhs[0], want) || got[0].End() >= before {
			t.Fatalf("owned database derivation must be exact and unreassigned: %s", key)
		}
	}
	for _, call := range []string{
		"adminSQL.SetMaxOpenConns(1)", "adminSQL.SetMaxIdleConns(1)",
		"testSQL.SetMaxOpenConns(4)", "testSQL.SetMaxIdleConns(4)",
	} {
		if caps[call] != 1 {
			t.Fatalf("pool bound missing or repeated: %s", call)
		}
	}
	registrations := make(map[string]token.Pos)
	var dropCleanup *ast.BlockStmt
	for _, statement := range helper.Body.List {
		expression, ok := statement.(*ast.ExprStmt)
		if !ok {
			continue
		}
		call, ok := expression.X.(*ast.CallExpr)
		if !ok || render(call.Fun) != "t.Cleanup" || len(call.Args) != 1 {
			continue
		}
		callback, ok := call.Args[0].(*ast.FuncLit)
		if !ok {
			continue
		}
		ast.Inspect(callback.Body, func(node ast.Node) bool {
			if inner, ok := node.(*ast.CallExpr); ok {
				key := render(inner.Fun)
				if inner == drop {
					key, dropCleanup = "drop", callback.Body
				}
				if key == "drop" || key == "adminSQL.Close" || key == "testSQL.Close" {
					if registrations[key] != token.NoPos {
						t.Fatalf("duplicate cleanup: %s", key)
					}
					registrations[key] = call.Pos()
				}
			}
			return true
		})
	}
	if registrations["adminSQL.Close"] == token.NoPos || registrations["drop"] <= registrations["adminSQL.Close"] ||
		registrations["testSQL.Close"] <= registrations["drop"] || dropCleanup == nil {
		t.Fatal("cleanup registration must be admin close, owned drop, test close for safe LIFO execution")
	}
	var zeroGate, mismatchGate, deadline, cancel token.Pos
	for _, statement := range dropCleanup.List {
		if assignment, ok := statement.(*ast.AssignStmt); ok && len(assignment.Lhs) == 2 && len(assignment.Rhs) == 1 &&
			render(assignment.Lhs[0]) == "cleanupCtx" && render(assignment.Lhs[1]) == "cleanupCancel" &&
			matches(assignment.Rhs[0], "context.WithTimeout(context.Background(), 15 * time.Second)") {
			deadline = assignment.End()
		}
		if deferred, ok := statement.(*ast.DeferStmt); ok && render(deferred.Call) == "cleanupCancel()" {
			cancel = deferred.End()
		}
		branch, ok := statement.(*ast.IfStmt)
		if !ok {
			continue
		}
		condition := render(branch.Cond)
		if condition != "ownedDatabaseOID == 0" && condition != "err != nil || currentOID != ownedDatabaseOID" {
			continue
		}
		if branch.Else != nil || len(branch.Body.List) == 0 {
			t.Fatal("ownership failure must return directly")
		}
		last, ok := branch.Body.List[len(branch.Body.List)-1].(*ast.ReturnStmt)
		if !ok || len(last.Results) != 0 || branch.End() >= drop.Pos() {
			t.Fatal("ownership failure must return before DROP")
		}
		if condition == "ownedDatabaseOID == 0" {
			zeroGate = branch.End()
		} else {
			query, ok := branch.Init.(*ast.AssignStmt)
			if !ok || len(query.Lhs) != 1 || render(query.Lhs[0]) != "err" || len(query.Rhs) != 1 ||
				!matches(query.Rhs[0], `adminSQL.QueryRowContext(cleanupCtx, "SELECT oid::bigint FROM pg_catalog.pg_database WHERE datname = $1", databaseName).Scan(&currentOID)`) {
				t.Fatal("ownership mismatch guard must freshly query the exact owned database OID")
			}
			mismatchGate = branch.End()
		}
	}
	if deadline == token.NoPos || cancel <= deadline || zeroGate <= cancel || mismatchGate <= zeroGate {
		t.Fatal("bounded Background cleanup and both returning ownership gates must precede DROP")
	}
	text := strings.ToLower(literals.String())
	for _, forbidden := range []string{"with (force)", "cascade", "pg_terminate_backend", "if not exists"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("test cleanup/provisioning has forbidden authority: %s", forbidden)
		}
	}
	for _, required := range []string{"hai_migration_runner_test", "create database", "template template0", "oid::bigint", "drop database"} {
		if !strings.Contains(text, required) {
			t.Fatalf("owned isolated database source contract missing %s", required)
		}
	}
}
