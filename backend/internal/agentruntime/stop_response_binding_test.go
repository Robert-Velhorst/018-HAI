package agentruntime

import (
	"context"
	"strings"
	"testing"
)

type stopResponseAdapter struct {
	*fakeAdapter
	response StopResult
	calls    int
}

func (a *stopResponseAdapter) StopTaskWithReference(context.Context, string, string, string) StopResult {
	a.calls++
	return a.response
}

func TestRegistryStopDoesNotRepairMismatchedAdapterIdentity(t *testing.T) {
	for _, running := range []bool{false, true} {
		for _, mode := range []string{"valid", "wrong_runtime", "wrong_task", "wrong_reference", "missing_reference"} {
			name := mode + "/untracked"
			if running {
				name = mode + "/running"
			}
			t.Run(name, func(t *testing.T) {
				response := StopResult{RuntimeID: "openclaw", TaskID: "task-1", ExecutionReference: "ocgw:v2:run-1", Status: "cancellation_requested", Message: "foreign-private-response", AuditEvents: []string{"foreign-private-audit"}}
				switch mode {
				case "wrong_runtime":
					response.RuntimeID = "hermes"
				case "wrong_task":
					response.TaskID = "task-2"
				case "wrong_reference":
					response.ExecutionReference = "ocgw:v2:other-run"
				case "missing_reference":
					response.ExecutionReference = ""
				}
				adapter := &stopResponseAdapter{fakeAdapter: &fakeAdapter{info: Info{ID: "openclaw"}}, response: response}
				registry := NewRegistry(adapter)
				if running {
					_, cancel, registered := registry.registerRunningTask(context.Background(), "openclaw", Task{ID: "task-1", OwnerIdentity: "alice"})
					if !registered {
						t.Fatal("test task was not registered")
					}
					defer cancel()
				}
				result := registry.StopTaskWithReference(context.Background(), "openclaw", "task-1", "alice", "ocgw:v2:run-1")
				if adapter.calls != 1 {
					t.Fatal("adapter was repeated or not called")
				}
				if mode == "valid" {
					if result.Status != "cancellation_requested" {
						t.Fatal("valid cancellation acknowledgement lost")
					}
				} else {
					if result.Status != "indeterminate" || result.RuntimeID != "openclaw" || result.TaskID != "task-1" || result.ExecutionReference != "ocgw:v2:run-1" {
						t.Fatal("mismatched adapter result was trusted")
					}
					if strings.Contains(result.Message+strings.Join(result.AuditEvents, " "), "foreign-private") {
						t.Fatal("unbound response leaked into safe result")
					}
				}
			})
		}
	}
}
