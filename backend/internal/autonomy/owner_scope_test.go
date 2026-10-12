package autonomy

import (
	"strings"
	"testing"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// Dry-run query capture needs no server or data. Live cross-owner acceptance
// must still exercise these queries against an isolated database.
func TestOwnerOverviewScopesAllWorkflowTelemetryBeforeLimits(t *testing.T) {
	db, err := gorm.Open(postgres.New(postgres.Config{
		DSN: "host=127.0.0.1 port=1 user=unused dbname=unused sslmode=disable",
	}), &gorm.Config{DryRun: true, DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	type query struct {
		sql  string
		vars []any
	}
	queries := []query{}
	err = db.Callback().Query().After("gorm:query").Register("test:capture", func(tx *gorm.DB) {
		queries = append(queries, query{tx.Statement.SQL.String(), append([]any(nil), tx.Statement.Vars...)})
	})
	if err != nil {
		t.Fatal(err)
	}
	owner := "owner' OR 1=1 --"
	result, err := NewService(db).OverviewForOwner(owner)
	if err != nil {
		t.Fatal(err)
	}
	telemetryQueries := make([]query, 0, 3)
	workflowSubqueries := make([]query, 0, 3)
	stressQueries := make([]query, 0, 1)
	for _, q := range queries {
		switch {
		case strings.Contains(q.sql, `FROM "autonomy_stress_runs"`):
			stressQueries = append(stressQueries, q)
		case strings.Contains(q.sql, `FROM "autonomy_`):
			telemetryQueries = append(telemetryQueries, q)
		case strings.Contains(q.sql, `FROM "workflow_items"`):
			workflowSubqueries = append(workflowSubqueries, q)
		}
	}
	if len(telemetryQueries) != 3 || len(workflowSubqueries) != 3 || len(stressQueries) != 1 {
		t.Fatalf("telemetry statements=%d, owner subquery callbacks=%d, system diagnostics=%d; total callbacks=%d",
			len(telemetryQueries), len(workflowSubqueries), len(stressQueries), len(queries))
	}
	for index, table := range []string{"autonomy_world_states", "autonomy_action_traces", "autonomy_evaluations"} {
		q := telemetryQueries[index]
		if !strings.Contains(q.sql, table) || !strings.Contains(q.sql, "workflow_id IN (SELECT") ||
			!strings.Contains(q.sql, "owner_identity = $") || strings.Contains(q.sql, owner) {
			t.Fatalf("unscoped or interpolated query: %s", q.sql)
		}
		if len(q.vars) == 0 || q.vars[0] != owner {
			t.Fatalf("owner parameter missing: %#v", q.vars)
		}
	}
	for _, q := range workflowSubqueries {
		if !strings.Contains(q.sql, `SELECT "id" FROM "workflow_items" WHERE owner_identity = $`) || strings.Contains(q.sql, owner) {
			t.Fatalf("unsafe workflow owner subquery: %s", q.sql)
		}
		if len(q.vars) == 0 || q.vars[0] != owner {
			t.Fatalf("owner parameter missing from workflow subquery: %#v", q.vars)
		}
	}
	if strings.Contains(stressQueries[0].sql, "owner_identity") || strings.Contains(stressQueries[0].sql, owner) {
		t.Fatal("deterministic stress summaries are system diagnostics")
	}
	if result.RecentActions == nil || result.RecentEvaluations == nil || result.RecentStressRuns == nil {
		t.Fatal("empty telemetry must contain explicit arrays")
	}
}

func TestBlankOwnerCannotFallBackToGlobalOverview(t *testing.T) {
	for _, owner := range []string{"", " \t\n"} {
		if _, err := NewService(nil).OverviewForOwner(owner); err == nil {
			t.Fatal("blank owner must be rejected before any database query")
		}
	}
}
