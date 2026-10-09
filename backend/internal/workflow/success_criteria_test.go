package workflow

import (
	"reflect"
	"strings"
	"testing"

	"automation-hub-backend/migrations"
)

func TestNormalizeWorkflowSuccessCriteriaValidatesAndTrims(t *testing.T) {
	got, err := normalizeWorkflowSuccessCriteria([]string{"  first criterion  ", "second criterion"})
	if err != nil {
		t.Fatalf("normalizeWorkflowSuccessCriteria: %v", err)
	}
	if !reflect.DeepEqual(got, []string{"first criterion", "second criterion"}) {
		t.Fatalf("normalized criteria = %#v", got)
	}
}

func TestNormalizeWorkflowSuccessCriteriaRejectsInvalidInputs(t *testing.T) {
	tests := []struct {
		name     string
		criteria []string
		contains string
	}{
		{name: "empty criterion", criteria: []string{""}, contains: "is empty"},
		{name: "too many", criteria: makeCriteria(51), contains: "at most 50"},
		{name: "too long", criteria: []string{strings.Repeat("x", 1001)}, contains: "exceeds 1000"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := normalizeWorkflowSuccessCriteria(test.criteria)
			if err == nil || !strings.Contains(err.Error(), test.contains) {
				t.Fatalf("error = %v, want substring %q", err, test.contains)
			}
		})
	}
}

func TestWorkflowSourceRevisionIncludesSuccessCriteria(t *testing.T) {
	request := IntakeRequest{SourceType: "email", SourceID: "message-1", Input: "Review email"}
	first := workflowSourceRevision(request, request.Input)
	request.SuccessCriteria = []string{"Keep draft only"}
	second := workflowSourceRevision(request, request.Input)
	if first == second {
		t.Fatal("source revision did not change when explicit success criteria changed")
	}
}

func TestWorkflowSuccessCriteriaMigrationAddsBoundedJSONArrayContract(t *testing.T) {
	up, err := migrations.Files.ReadFile("pre/0116_workflow_success_criteria.up.sql")
	if err != nil {
		t.Fatalf("read up migration: %v", err)
	}
	down, err := migrations.Files.ReadFile("pre/0116_workflow_success_criteria.down.sql")
	if err != nil {
		t.Fatalf("read down migration: %v", err)
	}
	if !strings.Contains(string(up), "ADD COLUMN IF NOT EXISTS success_criteria jsonb NOT NULL DEFAULT '[]'::jsonb") ||
		!strings.Contains(string(up), "jsonb_typeof(success_criteria) = 'array'") ||
		!strings.Contains(string(up), "THEN jsonb_array_length(success_criteria) <= 50") {
		t.Fatalf("up migration lacks a durable JSON-array contract: %s", up)
	}
	if !strings.Contains(string(down), "DROP COLUMN IF EXISTS success_criteria") {
		t.Fatalf("down migration does not remove the additive column: %s", down)
	}
	if !strings.Contains(string(down), "cannot drop workflow success criteria while workflow records contain criteria") {
		t.Fatalf("down migration does not protect stored criteria: %s", down)
	}
}

func makeCriteria(count int) []string {
	criteria := make([]string, count)
	for index := range criteria {
		criteria[index] = "criterion"
	}
	return criteria
}
