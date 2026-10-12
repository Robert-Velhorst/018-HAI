package agentruntime

import (
	"context"
	"github.com/google/uuid"
	"golang.org/x/net/websocket"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestArtifactRecoveryRegistryReadsOnlyNativeMetadata(t *testing.T) {
	done := make(chan struct{})
	var methods []string
	server := httptest.NewServer(websocket.Server{Handler: func(c *websocket.Conn) {
		defer close(done)
		defer c.Close()
		_ = c.SetDeadline(time.Now().Add(3 * time.Second))
		_ = websocket.JSON.Send(c, map[string]any{"type": "event", "event": "connect.challenge", "payload": map[string]any{"nonce": "challenge-nonce", "ts": 1737264000000}})
		for {
			var frame map[string]any
			if err := websocket.JSON.Receive(c, &frame); err != nil {
				return
			}
			method, _ := frame["method"].(string)
			methods = append(methods, method)
			var payload any
			switch method {
			case "connect":
				params := frame["params"].(map[string]any)
				scopes := params["scopes"].([]any)
				if len(scopes) != 1 || scopes[0] != "operator.read" {
					t.Error("requested write authority")
				}
				payload = map[string]any{"type": "hello-ok", "protocol": 4, "server": map[string]any{"version": "2026.9.1", "connId": "test"}, "features": map[string]any{"methods": []string{"artifacts.list"}}, "snapshot": map[string]any{}, "auth": map[string]any{"role": "operator", "scopes": []string{"operator.read"}}, "policy": map[string]any{"maxPayload": 65536, "maxBufferedBytes": 65536}}
			case "artifacts.list":
				params := frame["params"].(map[string]any)
				if len(params) != 1 || params["runId"] != "test-run" {
					t.Error("lost exact run scope")
				}
				payload = map[string]any{"artifacts": []any{}}
			default:
				t.Errorf("unexpected effect method %s", method)
				return
			}
			_ = websocket.JSON.Send(c, map[string]any{"type": "res", "id": frame["id"], "ok": true, "payload": payload})
			if method == "artifacts.list" {
				return
			}
		}
	}})
	defer server.Close()
	reference := "ocgw:v2:" + uuid.NewString()
	store := &fakeOpenClawGatewayArtifactStore{}
	adapter := &openClawAdapter{gatewayEnabled: true, gatewayArtifactImportEnabled: true, gatewayToken: "test-read-token", gatewayURL: "ws" + strings.TrimPrefix(server.URL, "http"), timeout: 3 * time.Second, allowedHost: map[string]bool{"127.0.0.1": true}, gatewayArtifactStore: store,
		gatewayReceiptStore: &fakeOpenClawGatewayReceiptStore{receipts: []OpenClawGatewayReceipt{{ExecutionReference: reference, RuntimeTaskID: "test-task", SessionKey: "agent:main:test", RunID: "test-run", TerminalStatus: "completed", TerminalAt: time.Now().UTC()}}}}
	registry := &Registry{adapters: map[string]Adapter{"openclaw": adapter}}
	if !registry.OpenClawArtifactRecoveryReady() {
		t.Fatal("configured reader not ready")
	}
	result, err := registry.ReadOpenClawGatewayArtifactDescriptors(context.Background(), "test-task", reference)
	if err != nil || result == nil || len(result) != 0 {
		t.Fatalf("native empty result lost: %#v %v", result, err)
	}
	<-done
	if len(methods) != 2 || len(store.descriptors) != 0 {
		t.Fatal("reader persisted data or invoked extra methods")
	}
	adapter.gatewayArtifactImportEnabled = false
	if registry.OpenClawArtifactRecoveryReady() {
		t.Fatal("disabled reader reported ready")
	}
	if _, err := registry.ReadOpenClawGatewayArtifactDescriptors(context.Background(), "test-task", reference); err == nil {
		t.Fatal("disabled reader contacted gateway")
	}
}
