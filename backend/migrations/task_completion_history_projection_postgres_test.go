//go:build integration

package migrations_test

import (
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/infra"
	"automation-hub-backend/internal/task"

	"github.com/google/uuid"
)

func TestTaskCompletionHistoryProjectionIsDurableBoundedAndImmutable(t *testing.T) {
	db := openIsolatedMigrationDatabase(t)
	files := migrationFilesThrough(t, "pre/0064_task_completion_history_projection")
	if _, err := infra.ApplyMigrations(db, files, "pre"); err != nil {
		t.Fatalf("apply pre migrations: %v", err)
	}

	repo := task.NewPostgresTaskStateRepository(db)
	owner := "history-projection-" + uuid.NewString()
	now := time.Now().UTC().Truncate(time.Microsecond)
	plan := task.CompletionPlan{
		ID:               "plan-" + uuid.NewString(),
		OwnerIdentity:    owner,
		CreatedAt:        now,
		Request:          "Prepare a source-grounded project brief",
		ProjectKey:       "018-HAI",
		Intake:           task.IntakeAnalysis{TaskType: "research", SuccessCriteria: []string{"sources are cited"}},
		CompletionStatus: "verified",
	}
	if err := repo.AppendCompletionPlan(owner, plan); err != nil {
		t.Fatalf("append completion plan: %v", err)
	}

	history, err := repo.ListCompletionPlanHistory(owner, 1)
	if err != nil {
		t.Fatalf("read compact history: %v", err)
	}
	if len(history) != 1 || history[0].ID != plan.ID || history[0].Request != plan.Request ||
		history[0].ProjectKey != plan.ProjectKey || history[0].Intake.TaskType != plan.Intake.TaskType ||
		len(history[0].Intake.SuccessCriteria) != 1 || history[0].CompletionStatus != plan.CompletionStatus {
		t.Fatalf("compact history = %#v", history)
	}
	foreign, err := repo.ListCompletionPlanHistory("foreign-"+owner, 1)
	if err != nil || len(foreign) != 0 {
		t.Fatalf("foreign compact history = %#v, err=%v", foreign, err)
	}

	for _, mutation := range []struct {
		statement string
		args      []any
	}{
		{statement: `UPDATE public.task_completion_plan_history SET request_summary = 'tampered' WHERE owner_identity = ?`, args: []any{owner}},
		{statement: `DELETE FROM public.task_completion_plan_history WHERE owner_identity = ?`, args: []any{owner}},
		{statement: `TRUNCATE TABLE public.task_completion_plan_history`},
	} {
		query := db.Exec(mutation.statement, mutation.args...)
		if query.Error == nil || !strings.Contains(query.Error.Error(), "task audit") {
			t.Fatalf("projection mutation %q error = %v", mutation.statement, query.Error)
		}
	}

	if err := db.Exec(`
		INSERT INTO public.task_completion_plan_history (
			completion_log_id, owner_identity, task_plan_id, created_at,
			request_summary, project_key, task_type, success_criteria,
			completion_status, source_payload_digest
		)
		SELECT completion_log_id, owner_identity, task_plan_id, created_at,
			'forged summary', project_key, task_type, success_criteria,
			completion_status, source_payload_digest
		FROM public.task_completion_plan_history
		WHERE owner_identity = ?`, owner).Error; err == nil ||
		!strings.Contains(err.Error(), "does not match immutable source payload") {
		t.Fatalf("forged projection error = %v", err)
	}
}
