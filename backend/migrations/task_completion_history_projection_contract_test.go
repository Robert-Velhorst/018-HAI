package migrations

import (
	"strings"
	"testing"
)

func TestTaskCompletionHistoryProjectionIsBoundAndAppendOnly(t *testing.T) {
	upBytes, err := Files.ReadFile("pre/0064_task_completion_history_projection.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	downBytes, err := Files.ReadFile("pre/0064_task_completion_history_projection.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	up := strings.ToLower(string(upBytes))
	down := strings.ToLower(string(downBytes))

	for _, required := range []string{
		"create table if not exists public.task_completion_plan_history",
		"references public.task_completion_plan_logs(id) on delete restrict",
		"hai_enforce_task_completion_history_binding",
		"hai_project_task_completion_history",
		"trg_task_completion_plan_logs_project_history",
		"trg_task_completion_plan_history_immutable",
		"trg_task_completion_plan_history_no_truncate",
		"idx_task_completion_plan_history_owner_created",
		"insert into public.task_completion_plan_history",
	} {
		if !strings.Contains(up, required) {
			t.Fatalf("up migration missing %q", required)
		}
	}
	if !strings.Contains(down, "refusing to discard immutable task completion history projections") {
		t.Fatal("down migration must fail closed while projection history exists")
	}
	if !strings.Contains(down, "drop trigger if exists trg_task_completion_plan_logs_project_history") {
		t.Fatal("down migration must remove the source projection trigger before its function")
	}
}
