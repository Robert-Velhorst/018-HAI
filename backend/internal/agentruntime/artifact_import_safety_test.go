package agentruntime

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/safety"

	"github.com/google/uuid"
	"golang.org/x/net/websocket"
)

func TestDeepSeekEnvironmentOutputRedactsExplicitSecretValues(t *testing.T) {
	secret := "synthetic-unusual-secret-value"
	got := redactRuntimeOutput("child echoed "+secret, 128, []string{"CUSTOM_RUNTIME_SECRET=" + secret})
	if strings.Contains(got, secret) || !strings.Contains(got, "[REDACTED_ENV_SECRET]") {
		t.Fatalf("runtime output did not redact explicitly forwarded secret: %q", got)
	}
	allow := nonSensitiveEnvironmentAllowlist([]string{"CUSTOM_RUNTIME_SECRET", "HAI_DSH_TEST_VERSION_OUTPUT"})
	if len(allow) != 1 || allow[0] != "HAI_DSH_TEST_VERSION_OUTPUT" {
		t.Fatalf("version-probe environment allowlist = %#v, want only non-sensitive key", allow)
	}
}

func TestOpenClawArtifactImportStopDuringReadPreventsFinalMetadataWrite(t *testing.T) {
	stopped := installAdmissionStopProvider(t)
	done := make(chan struct{})
	gateway := websocket.Server{Handler: func(connection *websocket.Conn) {
		defer close(done)
		defer connection.Close()
		if err := websocket.JSON.Send(connection, map[string]any{
			"type": "event", "event": "connect.challenge",
			"payload": map[string]any{"nonce": "synthetic-challenge", "ts": float64(1_737_264_000_000)},
		}); err != nil {
			return
		}
		_ = connection.SetDeadline(time.Now().Add(time.Second))
		for {
			var frame map[string]any
			if err := websocket.JSON.Receive(connection, &frame); err != nil {
				return
			}
			requestID, _ := frame["id"].(string)
			switch frame["method"] {
			case "connect":
				_ = websocket.JSON.Send(connection, map[string]any{
					"type": "res", "id": requestID, "ok": true,
					"payload": map[string]any{
						"type": "hello-ok", "protocol": float64(4),
						"server": map[string]any{"version": "2026.8.1", "connId": "synthetic-connection"},
						"features": map[string]any{"methods": []string{"artifacts.list"}},
						"snapshot": map[string]any{},
						"auth": map[string]any{"role": "operator", "scopes": []string{"operator.read"}},
						"policy": map[string]any{"maxPayload": float64(65536), "maxBufferedBytes": float64(65536)},
					},
				})
			case "artifacts.list":
				// The stop lands after the initial check and while the external read
				// is in flight, immediately before the final metadata persistence.
				release := safety.AcquireEmergencyStopMutationFence()
				stopped.Store(true)
				release()
				_ = websocket.JSON.Send(connection, map[string]any{
					"type": "res", "id": requestID, "ok": true,
					"payload": map[string]any{"artifacts": []map[string]any{{
						"id": "synthetic-artifact", "title": "Synthetic artifact", "type": "document", "mimeType": "text/plain",
						"sizeBytes": float64(12), "runId": "synthetic-run", "download": map[string]any{"mode": "unsupported"},
					}}},
				})
				return
			default:
				return
			}
		}
	}}
	mux := http.NewServeMux()
	mux.Handle("/", gateway)
	server := httptest.NewServer(mux)
	defer server.Close()
	t.Setenv("OPENCLAW_GATEWAY_ENABLED", "true")
	t.Setenv("OPENCLAW_GATEWAY_ARTIFACT_IMPORT_ENABLED", "true")
	t.Setenv("OPENCLAW_GATEWAY_TOKEN", "synthetic-read-token")
	t.Setenv("OPENCLAW_GATEWAY_URL", "ws"+strings.TrimPrefix(server.URL, "http"))
	t.Setenv("AGENT_RUNTIME_ALLOWED_HOSTS", "127.0.0.1")
	t.Setenv("OPENCLAW_TIMEOUT_SECONDS", "2")

	reference := "ocgw:v2:" + uuid.NewString()
	receipts := &fakeOpenClawGatewayReceiptStore{receipts: []OpenClawGatewayReceipt{{
		ExecutionReference: reference, RuntimeTaskID: "synthetic-task", OwnerIdentity: "synthetic-owner",
		SessionKey: "agent:main:hai-task", RunID: "synthetic-run", CreatedAt: time.Now().UTC(),
		TerminalStatus: "completed", TerminalAt: time.Now().UTC(),
	}}}
	artifacts := &fakeOpenClawGatewayArtifactStore{}
	adapter := newOpenClawAdapterFromEnv()
	adapter.gatewayReceiptStore = receipts
	adapter.gatewayArtifactStore = artifacts

	result := adapter.ImportDelegatedArtifacts(context.Background(), "synthetic-task", reference)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("synthetic Gateway did not finish artifact lookup")
	}
	if result.Status != "blocked" {
		t.Fatalf("import status = %q, want blocked after stop during Gateway read: %#v", result.Status, result)
	}
	if len(artifacts.descriptors) != 0 {
		t.Fatalf("artifact descriptors were persisted after emergency stop: %#v", artifacts.descriptors)
	}
}
