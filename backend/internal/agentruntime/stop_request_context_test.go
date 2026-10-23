package agentruntime

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

type contextStopAdapter struct{ *otherStopResponseAdapter }

func (a *contextStopAdapter) StopTaskWithReference(context.Context, string, string, string) StopResult {
	a.calls++
	return a.response
}

func TestRegistryStopRequiresLiveRequestBeforeAnyEffect(t *testing.T) {
	for _, route := range []string{"owner", "reference", "exact"} {
		for _, running := range []bool{false, true} {
			if route == "exact" && running {
				continue
			}
			for _, state := range []string{"missing", "cancelled", "expired", "live"} {
				name := route + "/" + state
				if running {
					name += "/running"
				}
				t.Run(name, func(t *testing.T) {
					reference := "ocgw:v2:" + uuid.NewString()
					adapter := &contextStopAdapter{&otherStopResponseAdapter{
						fakeAdapter: &fakeAdapter{info: Info{ID: "openclaw"}},
						response:    StopResult{RuntimeID: "openclaw", TaskID: "task", ExecutionReference: reference, Status: "cancellation_requested"},
					}}
					registry := NewRegistry(adapter)
					var active context.Context
					if running {
						var cancel context.CancelFunc
						var registered bool
						active, cancel, registered = registry.registerRunningTask(context.Background(), "openclaw", Task{ID: "task", OwnerIdentity: "alice"})
						if !registered {
							t.Fatal("test task was not registered")
						}
						defer cancel()
					}
					var request context.Context
					switch state {
					case "cancelled":
						ctx, cancel := context.WithCancel(context.Background())
						cancel()
						request = ctx
					case "expired":
						ctx, cancel := context.WithDeadline(context.Background(), time.Unix(1, 0))
						defer cancel()
						request = ctx
					case "live":
						request = context.Background()
					}
					var result StopResult
					switch route {
					case "owner":
						result = registry.StopTask(request, "openclaw", "task", "alice")
					case "reference":
						result = registry.StopTaskWithReference(request, "openclaw", "task", "alice", reference)
					case "exact":
						result = registry.StopOpenClawGatewayReceipt(request, OpenClawGatewayReceipt{RuntimeTaskID: "task", OwnerIdentity: "alice", ExecutionReference: reference, SessionKey: "session", RunID: "run", Status: "admitted"})
					}
					if state == "live" {
						if adapter.calls != 1 || result.Status != "cancellation_requested" || (running && active.Err() == nil) {
							t.Fatalf("live stop changed: calls=%d result=%#v", adapter.calls, result)
						}
					} else if adapter.calls != 0 || result.Status != "blocked" || (running && active.Err() != nil) {
						t.Fatalf("invalid request caused a stop effect: calls=%d result=%#v", adapter.calls, result)
					}
				})
			}
		}
	}
}

func TestOpenClawArchiveRecognizesVercelProviderWithoutInventingAvailability(t *testing.T) {
	empty := openClawEcosystemInventory{}
	classifyOpenClawExtensions(&empty)
	if len(empty.providers) != 0 {
		t.Fatal("empty inventory invented a provider")
	}
	inventory := openClawEcosystemInventory{extensions: []string{"vercel-ai-gateway"}}
	classifyOpenClawExtensions(&inventory)
	if !containsString(inventory.providers, "vercel-ai-gateway") {
		t.Fatal("archive provider extension lost its provider classification")
	}
}
