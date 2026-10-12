package haios

import (
	"errors"
	"strings"
	"testing"

	"automation-hub-backend/internal/models"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestOverviewMetricsUseOwnerBoundQueries(t *testing.T) {
	db, err := gorm.Open(postgres.New(postgres.Config{
		DSN: "host=127.0.0.1 port=1 user=unused dbname=unused sslmode=disable",
	}), &gorm.Config{DryRun: true, DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	var sql string
	var vars []interface{}
	if err := db.Callback().Query().After("gorm:query").Register("test:os-metric-scope", func(tx *gorm.DB) {
		sql = tx.Statement.SQL.String()
		vars = append([]interface{}(nil), tx.Statement.Vars...)
	}); err != nil {
		t.Fatal(err)
	}
	owner := "owner' OR 1=1 --"
	handler := NewHandler(db, nil)
	for _, entry := range []struct {
		model  interface{}
		parent string
	}{
		{&models.WorkflowItem{}, ""}, {&models.ConnectedSource{}, ""}, {&models.ContextMemory{}, ""}, {&models.VerificationRun{}, ""},
		{&models.WorkflowProposal{}, "workflow_id"}, {&models.WorkflowQualityGate{}, "workflow_id"}, {&models.WorkflowOpenLoop{}, "workflow_id"},
		{&models.SourceExtraction{}, "source_id"}, {&models.VerificationClaim{}, "run_id"},
	} {
		sql, vars = "", nil
		if _, err := handler.countForOwner(owner, entry.model); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(sql, "owner_identity = $") || strings.Contains(sql, owner) || len(vars) != 1 || vars[0] != owner {
			t.Fatalf("unbound owner query: sql=%s vars=%#v", sql, vars)
		}
		if entry.parent != "" && !strings.Contains(sql, entry.parent+" IN (SELECT") {
			t.Fatalf("missing parent scope: %s", sql)
		}
	}
	for _, model := range []interface{}{&models.Automation{}, &models.AutomationAlert{}} {
		if _, err := handler.countForOwner(owner, model); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(sql, "owner_identity") {
			t.Fatal("legacy shared registry must be labelled as shared, not given a fictitious owner column")
		}
	}
	if _, err := handler.countForOwner(" ", &models.WorkflowItem{}); err == nil {
		t.Fatal("blank owner accepted")
	}
	if _, err := handler.countForOwner(owner, &models.AmbientScan{}); err == nil {
		t.Fatal("unclassified metric ownership accepted")
	}
	cause := errors.New("synthetic query failure")
	if err := db.Callback().Query().After("gorm:query").Register("test:os-metric-failure", func(tx *gorm.DB) { tx.AddError(cause) }); err != nil {
		t.Fatal(err)
	}
	if _, err := handler.countForOwner(owner, &models.WorkflowItem{}); !errors.Is(err, cause) {
		t.Fatalf("count failure was hidden: %v", err)
	}
}

func TestOverviewMissingStorageIsNotZeroHealthyState(t *testing.T) {
	handler := NewHandler(nil, nil)
	if _, err := handler.countForOwner("alice", &models.WorkflowItem{}); err == nil {
		t.Fatal("missing database was treated as an empty count")
	}
	view := PursuitOverview{AmbientProposals: 3}
	if err := handler.attachAmbientPursuitState("alice", &view); err == nil || view.AmbientProposals != 3 {
		t.Fatal("missing ambient storage overwrote the prior data or reported success")
	}
}
