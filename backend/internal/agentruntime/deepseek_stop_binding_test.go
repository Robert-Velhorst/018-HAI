package agentruntime

import (
	"context"
	"strings"
	"testing"

	"automation-hub-backend/internal/hostruntime"
	"github.com/google/uuid"
)

type mismatchedStopDispatcher struct {
	capturingHostRuntimeDispatcher
	result *hostruntime.Job
	calls  int
}

func (d *mismatchedStopDispatcher) CancelTask(context.Context, string, string, uuid.UUID) (*hostruntime.Job, bool, error) {
	d.calls++
	return d.result, true, nil
}

func TestDeepSeekStopReceiptIdentity(t *testing.T) {
	for _, path := range []string{"reference", "owner"} {
		for _, mode := range []string{"valid", "wrong_owner", "wrong_task", "nil_id", "wrong_id"} {
			t.Run(path+"/"+mode, func(t *testing.T) {
				id := uuid.New()
				job := &hostruntime.Job{ID: id, RuntimeID: "deepseek-harness", OwnerIdentity: "robert", TaskID: "task", Status: hostruntime.StatusCancelled}
				switch mode {
				case "wrong_owner":
					job.OwnerIdentity = "foreign-private-owner"
				case "wrong_task":
					job.TaskID = "foreign-private-task"
				case "nil_id":
					job.ID = uuid.Nil
				case "wrong_id":
					job.ID = uuid.New()
				}
				d := &mismatchedStopDispatcher{result: job}
				a := &deepSeekHarnessAdapter{dispatcher: d}
				var result StopResult
				if path == "reference" {
					result = a.StopTaskWithReference(context.Background(), "task", "robert", id.String())
				} else {
					result = a.StopTaskForOwner(context.Background(), "task", "robert", true)
				}
				valid := mode == "valid" || (path == "owner" && mode == "wrong_id")
				if d.calls != 1 {
					t.Fatalf("cancellation repeated: %d", d.calls)
				}
				if valid {
					if result.Status != "cancelled" || result.ExecutionReference != job.ID.String() {
						t.Fatalf("valid receipt = %#v", result)
					}
					return
				}
				if result.Status != "indeterminate" || result.TaskID != "task" {
					t.Fatalf("foreign receipt accepted: %#v", result)
				}
				wantReference := ""
				if path == "reference" {
					wantReference = id.String()
				}
				if result.ExecutionReference != wantReference {
					t.Fatalf("foreign reference substituted: %#v", result)
				}
				if strings.Contains(result.Message+strings.Join(result.AuditEvents, " "), "foreign-private") {
					t.Fatalf("foreign details leaked: %#v", result)
				}
			})
		}
	}
}
