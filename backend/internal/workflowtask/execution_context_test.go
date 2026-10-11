package workflowtask

import (
	"context"
	"errors"
	"testing"

	"automation-hub-backend/internal/workflow"
)

func TestWorkflowTaskRunnerCarriesLifetimeAcrossPreviewAndRun(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svc := &capturingTaskService{}
	runner := NewRunner(svc)
	_, err := runner.RunWorkflowTaskContext(ctx, workflow.TaskRunRequest{WorkflowID: "workflow-cancel-test", OwnerIdentity: "alice", Request: "Summarize project notes"})
	if err != nil {
		t.Fatal(err)
	}
	if svc.previews != 1 || svc.runs != 1 || svc.previewRequest.ExecutionContext != ctx || svc.request.ExecutionContext != ctx {
		t.Fatal("workflow task lifetime did not reach preview and execution")
	}
	cancel()
	_, err = runner.RunWorkflowTaskContext(ctx, workflow.TaskRunRequest{WorkflowID: "workflow-cancel-test", OwnerIdentity: "alice", Request: "Summarize project notes"})
	if !errors.Is(err, context.Canceled) || svc.previews != 1 || svc.runs != 1 {
		t.Fatalf("cancelled runner performed work: %v", err)
	}
}

func TestDeferredRunnerRefusesContextlessDelegate(t *testing.T) {
	delegate := &fakeWorkflowTaskRunner{}
	runner := NewDeferredRunner()
	runner.Set(delegate)
	_, err := runner.RunWorkflowTaskContext(context.Background(), workflow.TaskRunRequest{Request: "Summarize project notes"})
	if !errors.Is(err, workflow.ErrTaskExecutionContextUnavailable) || delegate.seen.Request != "" {
		t.Fatalf("legacy delegate invoked: %v", err)
	}
}
