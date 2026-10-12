package agentruntime

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
)

type otherStopResponseAdapter struct {
	*fakeAdapter
	response StopResult
	calls    int
}

func (a *otherStopResponseAdapter) StopTaskForOwner(context.Context, string, string, bool) StopResult {
	a.calls++
	return a.response
}
func (a *otherStopResponseAdapter) StopOpenClawGatewayReceipt(context.Context, OpenClawGatewayReceipt) StopResult {
	a.calls++
	return a.response
}

func TestRegistryOtherStopRoutesRequireBoundAcknowledgement(t *testing.T) {
	for _, route := range []string{"owner_running", "owner_untracked", "exact_receipt"} {
		for _, mode := range []string{"valid", "wrong_runtime", "wrong_task", "missing_task", "wrong_reference"} {
			if route != "exact_receipt" && mode == "wrong_reference" {
				continue
			}
			t.Run(route+"/"+mode, func(t *testing.T) {
				reference := "ocgw:v2:" + uuid.NewString()
				response := StopResult{RuntimeID: "openclaw", TaskID: "task-1", ExecutionReference: reference, Status: "cancellation_requested", Message: "foreign-private-response", AuditEvents: []string{"foreign-private-audit"}}
				switch mode {
				case "wrong_runtime":
					response.RuntimeID = "hermes"
				case "wrong_task":
					response.TaskID = "task-2"
				case "missing_task":
					response.TaskID = ""
				case "wrong_reference":
					response.ExecutionReference = "ocgw:v2:" + uuid.NewString()
				}
				adapter := &otherStopResponseAdapter{fakeAdapter: &fakeAdapter{info: Info{ID: "openclaw"}}, response: response}
				registry := NewRegistry(adapter)
				if route == "owner_running" {
					_, cancel, registered := registry.registerRunningTask(context.Background(), "openclaw", Task{ID: "task-1", OwnerIdentity: "alice"})
					if !registered {
						t.Fatal("test task was not registered")
					}
					defer cancel()
				}
				var result StopResult
				if route == "exact_receipt" {
					result = registry.StopOpenClawGatewayReceipt(context.Background(), OpenClawGatewayReceipt{RuntimeTaskID: "task-1", OwnerIdentity: "alice", ExecutionReference: reference, SessionKey: "session-1", RunID: "run-1", Status: "admitted"})
				} else {
					result = registry.StopTask(context.Background(), "openclaw", "task-1", "alice")
				}
				if adapter.calls != 1 {
					t.Fatal("adapter was repeated or not called")
				}
				if mode == "valid" {
					if result.Status != "cancellation_requested" || result.ExecutionReference != reference {
						t.Fatal("valid result or adapter-discovered reference lost")
					}
				} else {
					if result.Status != "indeterminate" || result.RuntimeID != "openclaw" || result.TaskID != "task-1" {
						t.Fatal("unbound adapter response was trusted")
					}
					if route == "exact_receipt" && result.ExecutionReference != reference {
						t.Fatal("requested receipt reference lost")
					}
					if strings.Contains(result.Message+strings.Join(result.AuditEvents, " "), "foreign-private") {
						t.Fatal("unbound response payload leaked")
					}
				}
			})
		}
	}
}
