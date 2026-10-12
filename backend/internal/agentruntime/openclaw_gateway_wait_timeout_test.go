package agentruntime

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"golang.org/x/net/websocket"
)

func TestOpenClawGatewayReconcileWaitTimeoutRemainsIndeterminate(t *testing.T) {
	const timeout = 400 * time.Millisecond
	waitReceived := make(chan map[string]any, 1)
	connectionClosed := make(chan struct{})

	gateway := websocket.Server{Handler: func(connection *websocket.Conn) {
		defer connection.Close()
		defer close(connectionClosed)
		if err := websocket.JSON.Send(connection, map[string]any{
			"type": "event", "event": "connect.challenge",
			"payload": map[string]any{"nonce": "timeout-test-challenge", "ts": float64(1_737_264_000_000)},
		}); err != nil {
			return
		}

		var frame map[string]any
		if err := websocket.JSON.Receive(connection, &frame); err != nil {
			return
		}
		requestID, _ := frame["id"].(string)
		if frame["method"] != "connect" {
			return
		}
		if err := websocket.JSON.Send(connection, map[string]any{
			"type": "res", "id": requestID, "ok": true,
			"payload": map[string]any{
				"type": "hello-ok", "protocol": float64(4),
				"server":   map[string]any{"version": "2026.8.1", "connId": "timeout-test-connection"},
				"features": map[string]any{"methods": []string{"agent.wait"}}, "snapshot": map[string]any{},
				"auth":   map[string]any{"role": "operator", "scopes": []string{"operator.write"}},
				"policy": map[string]any{"maxPayload": float64(65536), "maxBufferedBytes": float64(65536)},
			},
		}); err != nil {
			return
		}

		if err := websocket.JSON.Receive(connection, &frame); err != nil {
			return
		}
		waitReceived <- frame
		// Deliberately leave agent.wait unanswered. Reconciliation must time out
		// without converting the missing receipt into a terminal result.
		_ = websocket.JSON.Receive(connection, &frame)
	}}
	mux := http.NewServeMux()
	mux.Handle("/", gateway)
	server := httptest.NewServer(mux)
	defer server.Close()

	t.Setenv("OPENCLAW_GATEWAY_ENABLED", "true")
	t.Setenv("OPENCLAW_GATEWAY_DELEGATION_ENABLED", "true")
	t.Setenv("OPENCLAW_GATEWAY_DELEGATION_TOKEN", "gateway-write-token")
	t.Setenv("OPENCLAW_GATEWAY_TOKEN", "")
	t.Setenv("OPENCLAW_GATEWAY_URL", "ws"+strings.TrimPrefix(server.URL, "http"))
	t.Setenv("AGENT_RUNTIME_ALLOWED_HOSTS", "127.0.0.1")
	t.Setenv("OPENCLAW_TIMEOUT_SECONDS", "1")

	reference := "ocgw:v2:" + uuid.NewString()
	adapter := newOpenClawAdapterFromEnv()
	adapter.timeout = timeout
	adapter.gatewayReceiptStore = &fakeOpenClawGatewayReceiptStore{receipts: []OpenClawGatewayReceipt{{
		ExecutionReference: reference,
		RuntimeTaskID:      "task-timeout",
		OwnerIdentity:      "alice",
		SessionKey:         "agent:main:hai-task-timeout",
		RunID:              "private-timeout-run",
		CreatedAt:          time.Now().UTC(),
	}}}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := time.Now()
	resultCh := make(chan DelegatedSessionReconcileResult, 1)
	go func() {
		resultCh <- adapter.ReconcileDelegatedSession(ctx, "task-timeout", "alice", reference)
	}()

	select {
	case frame := <-waitReceived:
		if frame["method"] != "agent.wait" {
			t.Fatalf("Gateway received method %v, want agent.wait", frame["method"])
		}
		params, ok := frame["params"].(map[string]any)
		if !ok || params["runId"] != "private-timeout-run" {
			t.Fatalf("agent.wait params = %#v, want the persisted run identity", frame["params"])
		}
	case <-time.After(2 * time.Second):
		cancel()
		t.Fatal("Gateway did not receive agent.wait")
	}

	var result DelegatedSessionReconcileResult
	select {
	case result = <-resultCh:
	case <-time.After(timeout + time.Second):
		cancel()
		<-resultCh
		t.Fatalf("ReconcileDelegatedSession exceeded configured timeout %s", timeout)
	}
	if elapsed := time.Since(started); elapsed > timeout+250*time.Millisecond {
		t.Fatalf("ReconcileDelegatedSession took %s, want return within configured timeout %s plus scheduling tolerance", elapsed, timeout)
	}
	if result.Status != "indeterminate" {
		t.Fatalf("reconciliation status = %q, want indeterminate after missing Gateway response", result.Status)
	}
	if !result.FinishedAt.IsZero() {
		t.Fatalf("FinishedAt = %s, want zero because no terminal receipt was returned", result.FinishedAt)
	}
	if result.Status == "completed" || result.Status == "failed" {
		t.Fatalf("reconciliation made a false terminal claim: %#v", result)
	}

	select {
	case <-connectionClosed:
	case <-time.After(time.Second):
		t.Fatal("Gateway connection was not closed after reconciliation timed out")
	}
}
