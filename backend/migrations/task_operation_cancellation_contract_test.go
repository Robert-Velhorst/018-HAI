package migrations

import (
	"strings"
	"testing"
)

func TestTaskOperationCancellationMigrationContract(t *testing.T) {
	upBytes, err := Files.ReadFile("pre/0067_task_operation_cancellation.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	up := string(upBytes)
	for _, required := range []string{
		"status IN ('running', 'completed', 'needs_review', 'canceled')",
		"status = 'canceled'",
		"task_plan_id = ''",
		"terminal task operation state is immutable",
	} {
		if !strings.Contains(up, required) {
			t.Errorf("task operation cancellation migration missing %q", required)
		}
	}
	if strings.Contains(strings.ToUpper(up), " CASCADE") {
		t.Fatal("task operation cancellation migration must not use CASCADE")
	}

	downBytes, err := Files.ReadFile("pre/0067_task_operation_cancellation.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	down := string(downBytes)
	if !strings.Contains(down, "refusing to remove task operation cancellation semantics") ||
		!strings.Contains(down, "WHERE status = 'canceled'") {
		t.Fatal("task operation cancellation rollback must preserve canceled audit state")
	}
	if strings.Contains(strings.ToUpper(down), " CASCADE") {
		t.Fatal("task operation cancellation rollback must not use CASCADE")
	}
}
