package agentruntime

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"automation-hub-backend/internal/safety"
	"github.com/google/uuid"
	"golang.org/x/net/websocket"
)

func TestOpenClawAdmissionWaitsForPolicyAttestation(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	t.Setenv("OPENCLAW_AGENT_ENABLED", "true")
	t.Setenv("OPENCLAW_GATEWAY_ENABLED", "true")
	t.Setenv("OPENCLAW_GATEWAY_DELEGATION_ENABLED", "true")
	t.Setenv("OPENCLAW_GATEWAY_DELEGATION_TOKEN", "separate-write-token")
	t.Setenv("OPENCLAW_GATEWAY_URL", "ws"+strings.TrimPrefix(server.URL, "http"))
	t.Setenv("AGENT_RUNTIME_ALLOWED_HOSTS", "127.0.0.1")

	store := &fakeOpenClawGatewayReceiptStore{}
	adapter := newOpenClawAdapterFromEnv()
	adapter.gatewayReceiptStore = store
	adapter.maintenanceGate = func(context.Context) (func(), error) { return func() {}, nil }
	result := adapter.ExecuteTask(context.Background(), approvedRuntimeTask("task-1", "Prepare a private draft."))

	if result.Status != "blocked" || !strings.Contains(result.Message, gatewayPolicyAttestationBlockReason) {
		t.Fatalf("unattested admission result = %#v", result)
	}
	if requests.Load() != 0 || len(store.receipts) != 0 {
		t.Fatalf("Gateway or durable admission state changed before attestation: requests=%d receipts=%#v", requests.Load(), store.receipts)
	}
}

func TestEmergencyStopPreventsUnattestedGatewayAdmission(t *testing.T) {
	t.Setenv("HAI_EMERGENCY_STOP", "false")
	t.Setenv("AUTONOMY_EMERGENCY_STOP", "false")
	t.Setenv("EMERGENCY_STOP", "false")
	restore := safety.SetEmergencyStopProvider(safety.EmergencyStopProviderFunc(func() (bool, string, error) {
		return true, "operator emergency stop", nil
	}))
	defer restore()

	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	t.Setenv("OPENCLAW_AGENT_ENABLED", "true")
	t.Setenv("OPENCLAW_GATEWAY_ENABLED", "true")
	t.Setenv("OPENCLAW_GATEWAY_DELEGATION_ENABLED", "true")
	t.Setenv("OPENCLAW_GATEWAY_DELEGATION_TOKEN", "separate-write-token")
	t.Setenv("OPENCLAW_GATEWAY_URL", "ws"+strings.TrimPrefix(server.URL, "http"))
	t.Setenv("AGENT_RUNTIME_ALLOWED_HOSTS", "127.0.0.1")

	store := &fakeOpenClawGatewayReceiptStore{}
	adapter := newOpenClawAdapterFromEnv()
	adapter.gatewayReceiptStore = store
	result := adapter.ExecuteTask(context.Background(), approvedRuntimeTask("task-stop", "Prepare a private draft."))
	if result.Status != "blocked" || requests.Load() != 0 || len(store.receipts) != 0 {
		t.Fatalf("emergency stop did not prevent Gateway admission: result=%#v requests=%d receipts=%#v", result, requests.Load(), store.receipts)
	}
}

func TestOpenClawReconciliationRejectsOtherOrMissingRunIdentity(t *testing.T) {
	for _, runID := range []string{"other-run", ""} {
		t.Run("returned_"+runID, func(t *testing.T) {
			done := make(chan struct{})
			server := httptest.NewServer(websocket.Server{Handler: func(c *websocket.Conn) {
				defer close(done)
				defer c.Close()
				_ = c.SetDeadline(time.Now().Add(2 * time.Second))
				_ = websocket.JSON.Send(c, map[string]any{"type": "event", "event": "connect.challenge", "payload": map[string]any{"nonce": "test-nonce", "ts": int64(1737264000000)}})
				var frame map[string]any
				if websocket.JSON.Receive(c, &frame) != nil {
					return
				}
				_ = websocket.JSON.Send(c, map[string]any{"type": "res", "id": frame["id"], "ok": true, "payload": map[string]any{
					"type": "hello-ok", "protocol": 4, "server": map[string]any{"version": "2026.9.1", "connId": "test-connection"},
					"features": map[string]any{"methods": []string{"agent.wait"}}, "snapshot": map[string]any{},
					"auth": map[string]any{"role": "operator", "scopes": []string{"operator.write"}}, "policy": map[string]any{"maxPayload": 65536, "maxBufferedBytes": 65536},
				}})
				if websocket.JSON.Receive(c, &frame) != nil {
					return
				}
				if frame["method"] != "agent.wait" {
					t.Errorf("unexpected method %v", frame["method"])
					return
				}
				_ = websocket.JSON.Send(c, map[string]any{"type": "res", "id": frame["id"], "ok": true, "payload": map[string]any{"runId": runID, "status": "ok", "endedAt": time.Now().UnixMilli(), "terminalReply": "private content"}})
			}})
			defer server.Close()
			t.Setenv("OPENCLAW_GATEWAY_ENABLED", "true")
			t.Setenv("OPENCLAW_GATEWAY_DELEGATION_ENABLED", "true")
			t.Setenv("OPENCLAW_GATEWAY_DELEGATION_TOKEN", "test-write-token")
			t.Setenv("OPENCLAW_GATEWAY_URL", "ws"+strings.TrimPrefix(server.URL, "http"))
			t.Setenv("AGENT_RUNTIME_ALLOWED_HOSTS", "127.0.0.1")
			t.Setenv("OPENCLAW_TIMEOUT_SECONDS", "2")
			adapter := newOpenClawAdapterFromEnv()
			reference := openClawGatewayReceiptReference()
			adapter.gatewayReceiptStore = &fakeOpenClawGatewayReceiptStore{receipts: []OpenClawGatewayReceipt{{ExecutionReference: reference, RuntimeTaskID: "task-1", OwnerIdentity: "alice", SessionKey: "agent:main:hai-task", RunID: "hai-owned-run"}}}
			result := adapter.ReconcileDelegatedSession(context.Background(), "task-1", "alice", reference)
			<-done
			if result.Status != "indeterminate" || !result.FinishedAt.IsZero() {
				t.Fatalf("another run was accepted as this task's completion: %#v", result)
			}
			if strings.Contains(result.Message, "private content") {
				t.Fatal("reply content leaked")
			}
		})
	}
}

func TestOpenClawStopWithoutExactRunDoesNotContactGateway(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		t.Run(map[bool]string{false: "missing_run", true: "legacy_session"}[legacy], func(t *testing.T) {
			var contacts atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				contacts.Add(1)
				w.WriteHeader(http.StatusBadRequest)
			}))
			defer server.Close()
			t.Setenv("OPENCLAW_GATEWAY_ENABLED", "true")
			t.Setenv("OPENCLAW_GATEWAY_DELEGATION_ENABLED", "true")
			t.Setenv("OPENCLAW_GATEWAY_DELEGATION_TOKEN", "test-write-token")
			t.Setenv("OPENCLAW_GATEWAY_URL", "ws"+strings.TrimPrefix(server.URL, "http"))
			t.Setenv("AGENT_RUNTIME_ALLOWED_HOSTS", "127.0.0.1")
			adapter := newOpenClawAdapterFromEnv()
			reference := openClawGatewayReceiptReference()
			adapter.gatewayReceiptStore = &fakeOpenClawGatewayReceiptStore{receipts: []OpenClawGatewayReceipt{{ExecutionReference: reference, RuntimeTaskID: "task-1", OwnerIdentity: "alice", SessionKey: "agent:main:hai-task"}}}
			if legacy {
				reference = openClawGatewaySessionReference("agent:main:hai-task")
			}
			result := adapter.StopTaskWithReference(context.Background(), "task-1", "alice", reference)
			if result.Status != "indeterminate" || !strings.Contains(result.Message, "run") || result.ExecutionReference == "" {
				t.Fatalf("missing exact run must remain indeterminate with its trusted reference: %#v", result)
			}
			if contacts.Load() != 0 {
				t.Fatalf("Gateway was contacted without an exact run identity: contacts=%d", contacts.Load())
			}
		})
	}
}

func TestEmergencyStopAllowsOnlyExactOwnerBoundRunAbortAndReconciliation(t *testing.T) {
	t.Setenv("HAI_EMERGENCY_STOP", "false")
	t.Setenv("AUTONOMY_EMERGENCY_STOP", "false")
	t.Setenv("EMERGENCY_STOP", "false")
	restore := safety.SetEmergencyStopProvider(safety.EmergencyStopProviderFunc(func() (bool, string, error) {
		return true, "operator e-stop", nil
	}))
	defer restore()

	var waitCount atomic.Int32
	abortParams := make(chan map[string]any, 1)
	waitParams := make(chan map[string]any, 2)
	finished := make(chan struct{}, 3)
	server := httptest.NewServer(websocket.Server{Handler: func(connection *websocket.Conn) {
		defer func() { finished <- struct{}{} }()
		defer connection.Close()
		_ = connection.SetDeadline(time.Now().Add(2 * time.Second))
		if err := websocket.JSON.Send(connection, map[string]any{"type": "event", "event": "connect.challenge", "payload": map[string]any{"nonce": "stop-test", "ts": int64(1737264000000)}}); err != nil {
			return
		}
		var frame map[string]any
		if websocket.JSON.Receive(connection, &frame) != nil {
			return
		}
		_ = websocket.JSON.Send(connection, map[string]any{"type": "res", "id": frame["id"], "ok": true, "payload": map[string]any{
			"type": "hello-ok", "protocol": 4, "server": map[string]any{"version": "2026.9.1", "connId": "e-stop-test"},
			"features": map[string]any{"methods": []string{"sessions.abort", "agent.wait"}}, "snapshot": map[string]any{},
			"auth": map[string]any{"role": "operator", "scopes": []string{"operator.write"}}, "policy": map[string]any{"maxPayload": 65536, "maxBufferedBytes": 65536},
		}})
		if websocket.JSON.Receive(connection, &frame) != nil {
			return
		}
		params, _ := frame["params"].(map[string]any)
		switch frame["method"] {
		case "sessions.abort":
			abortParams <- params
			_ = websocket.JSON.Send(connection, map[string]any{"type": "res", "id": frame["id"], "ok": true, "payload": map[string]any{"ok": true}})
		case "agent.wait":
			waitParams <- params
			status := "pending"
			if waitCount.Add(1) > 1 {
				status = "ok"
			}
			payload := map[string]any{"runId": "exact-run-77", "status": status}
			if status == "ok" {
				payload["endedAt"] = int64(1788521600000)
			}
			_ = websocket.JSON.Send(connection, map[string]any{"type": "res", "id": frame["id"], "ok": true, "payload": payload})
		default:
			t.Errorf("unexpected Gateway method during emergency stop: %v", frame["method"])
		}
	}})
	defer server.Close()
	t.Setenv("OPENCLAW_GATEWAY_ENABLED", "true")
	t.Setenv("OPENCLAW_GATEWAY_DELEGATION_ENABLED", "true")
	t.Setenv("OPENCLAW_GATEWAY_DELEGATION_TOKEN", "separate-write-token")
	t.Setenv("OPENCLAW_GATEWAY_URL", "ws"+strings.TrimPrefix(server.URL, "http"))
	t.Setenv("AGENT_RUNTIME_ALLOWED_HOSTS", "127.0.0.1")
	t.Setenv("OPENCLAW_TIMEOUT_SECONDS", "2")

	reference := "ocgw:v2:" + uuid.NewString()
	adapter := newOpenClawAdapterFromEnv()
	adapter.gatewayReceiptStore = &fakeOpenClawGatewayReceiptStore{receipts: []OpenClawGatewayReceipt{{
		ExecutionReference: reference, RuntimeTaskID: "task-e-stop", OwnerIdentity: "alice", Status: "admitted",
		SessionKey: "agent:main:private", RunID: "exact-run-77", CreatedAt: time.Now().UTC(),
	}}}
	registry := NewRegistry(adapter)

	stopped := registry.StopTaskWithReference(context.Background(), "openclaw", "task-e-stop", "alice", reference)
	if stopped.Status != "cancellation_requested" || stopped.ExecutionReference != reference || strings.Contains(strings.ToLower(stopped.Message), "completed") {
		t.Fatalf("emergency stop did not preserve a nonterminal exact-run cancellation result: %#v", stopped)
	}
	select {
	case params := <-abortParams:
		if params["key"] != "agent:main:private" || params["runId"] != "exact-run-77" || len(params) != 2 {
			t.Fatalf("e-stop abort was not scoped to the exact run: %#v", params)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("emergency-stop exact-run abort was not sent")
	}

	observed := registry.ReconcileOpenClawGatewaySession(context.Background(), "alice", "task-e-stop", reference)
	if observed.Status != "running" || !observed.FinishedAt.IsZero() || observed.ExecutionReference != reference {
		t.Fatalf("timeout/abort acknowledgment was treated as terminal: %#v", observed)
	}
	terminal := registry.ReconcileOpenClawGatewaySession(context.Background(), "alice", "task-e-stop", reference)
	if terminal.Status != "completed" || terminal.ExecutionReference != reference {
		t.Fatalf("terminal observation for the exact run was unavailable during e-stop: %#v", terminal)
	}
	for i := 0; i < 2; i++ {
		select {
		case params := <-waitParams:
			if params["runId"] != "exact-run-77" || params["timeoutMs"] != float64(0) || len(params) != 2 {
				t.Fatalf("e-stop reconciliation was not exact-run scoped: %#v", params)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("exact-run terminal reconciliation was not sent during e-stop")
		}
	}
	if waitCount.Load() != 2 {
		t.Fatalf("unexpected number of terminal observations: %d", waitCount.Load())
	}
	for i := 0; i < 3; i++ {
		select {
		case <-finished:
		case <-time.After(3 * time.Second):
			t.Fatal("Gateway test handler did not finish")
		}
	}
}
