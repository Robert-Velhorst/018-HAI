package autonomy

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"automation-hub-backend/internal/models"

	"github.com/google/uuid"
)

func TestPublicOverviewWithholdsPayloadsWithoutChangingEvidence(t *testing.T) {
	id := uuid.New()
	source := &Overview{
		RecentWorldStates: []models.AutonomyWorldState{{WorkflowID: id, Snapshot: "opaque-private-snapshot"}},
		RecentActions: []models.AutonomyActionTrace{{WorkflowID: id, ActionPayload: "opaque-private-argument", PolicyReason: "password=private-password", ResultSummary: "token=private-token"}},
		RecentEvaluations: []models.AutonomyEvaluation{{WorkflowID: id, FailureMode: "secret=private-failure"}},
		RecentStressRuns: []models.AutonomyStressRun{{Results: "opaque-private-stress"}},
		Warnings: []string{"api_key=private-warning"},
		DecisionDiscipline: map[string]any{"enabled": true, "order": []string{"necessity"}, "internal": "opaque-private-config"},
	}
	before, err := json.Marshal(source)
	if err != nil {
		t.Fatal(err)
	}
	projected := publicOverview(source)
	encoded, err := json.Marshal(projected)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"opaque-private", "private-password", "private-token", "private-failure", "private-warning"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("public telemetry leaked %q", forbidden)
		}
	}
	if projected.RecentActions[0].WorkflowID != id {
		t.Fatal("correlation identity changed")
	}
	after, err := json.Marshal(source)
	if err != nil || string(before) != string(after) {
		t.Fatal("projection changed original evidence")
	}
	if publicOverview(nil) != nil {
		t.Fatal("nil overview was invented")
	}
}

func TestTelemetryTextRedactsBeforeUnicodeBound(t *testing.T) {
	value := `{"note":"` + strings.Repeat("界", 4200) + `","token":"private-token"}`
	result := publicTelemetryText(value)
	if !utf8.ValidString(result) || utf8.RuneCountInString(result) > 4108 || strings.Contains(result, "private-token") {
		t.Fatal("invalid, oversized or unredacted public text")
	}
}
