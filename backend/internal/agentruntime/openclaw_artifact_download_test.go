package agentruntime

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"golang.org/x/net/websocket"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestNativeArtifactDownloadRegistryRunBoundLargeContent(t *testing.T) {
	for _, mode := range []string{"bytes", "url", "url-denied", "url-expired", "bytes-metadata-changed", "url-metadata-changed", "size-added-after-collection"} {
		t.Run(mode, func(t *testing.T) { testNativeArtifactDownloadRegistry(t, mode) })
	}
}

func testNativeArtifactDownloadRegistry(t *testing.T, mode string) {
	data := strings.Repeat("x", 100000)
	size := int64(len(data))
	summary := map[string]any{"id": "artifact_test", "title": "Report", "type": "file", "runId": "test-run", "sessionKey": "agent:main:test", "sizeBytes": size, "download": map[string]any{"mode": "bytes"}}
	var contentHits atomic.Int32
	contentServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		contentHits.Add(1)
		if request.Header.Get("Authorization") != "" || request.Header.Get("Cookie") != "" {
			t.Error("native token forwarded to content server")
		}
		_, _ = w.Write([]byte(data))
	}))
	defer contentServer.Close()
	if strings.HasPrefix(mode, "url") {
		summary["download"] = map[string]any{"mode": "url"}
	}
	done := make(chan struct{})
	var methods []string
	server := httptest.NewServer(websocket.Server{Handler: func(c *websocket.Conn) {
		defer close(done)
		defer c.Close()
		_ = c.SetDeadline(time.Now().Add(5 * time.Second))
		_ = websocket.JSON.Send(c, map[string]any{"type": "event", "event": "connect.challenge", "payload": map[string]any{"nonce": "challenge-nonce", "ts": 1737264000000}})
		for {
			var frame map[string]any
			if websocket.JSON.Receive(c, &frame) != nil {
				return
			}
			method, _ := frame["method"].(string)
			methods = append(methods, method)
			params := frame["params"].(map[string]any)
			var payload any
			switch method {
			case "connect":
				scopes := params["scopes"].([]any)
				if len(scopes) != 1 || scopes[0] != "operator.read" {
					t.Error("download requested write authority")
				}
				payload = map[string]any{"type": "hello-ok", "protocol": 4, "server": map[string]any{"version": "2026.9.1", "connId": "test"}, "features": map[string]any{"methods": []string{"artifacts.list", "artifacts.download"}}, "snapshot": map[string]any{}, "auth": map[string]any{"role": "operator", "scopes": []string{"operator.read"}}, "policy": map[string]any{"maxPayload": 65536, "maxBufferedBytes": 65536}}
			case "artifacts.list":
				if len(params) != 1 || params["runId"] != "test-run" {
					t.Error("unscoped list")
				}
				payload = map[string]any{"artifacts": []any{summary}}
			case "artifacts.download":
				if len(params) != 2 || params["runId"] != "test-run" || params["artifactId"] != "artifact_test" {
					t.Error("unscoped download")
				}
				_ = websocket.JSON.Send(c, map[string]any{"type": "event", "event": "tick", "payload": map[string]any{}})
				if strings.HasSuffix(mode, "metadata-changed") {
					summary["mimeType"] = "application/x-changed"
				}
				payload = map[string]any{"artifact": summary, "encoding": "base64", "data": base64.StdEncoding.EncodeToString([]byte(data))}
				if strings.HasPrefix(mode, "url") {
					expiry := time.Now().Add(time.Hour)
					if mode == "url-expired" {
						expiry = time.Now().Add(-time.Hour)
					}
					payload = map[string]any{"artifact": summary, "url": contentServer.URL + "/report?signature=private", "expiresAt": expiry.UTC().Format(time.RFC3339)}
				}
			default:
				t.Errorf("unexpected method %s", method)
				return
			}
			_ = websocket.JSON.Send(c, map[string]any{"type": "res", "id": frame["id"], "ok": true, "payload": payload})
			if method == "artifacts.download" {
				return
			}
		}
	}})
	defer server.Close()
	receipt := OpenClawGatewayReceipt{ExecutionReference: "ocgw:v2:" + uuid.NewString(), OwnerIdentity: "alice", RuntimeTaskID: "test-task", SessionKey: "agent:main:test", RunID: "test-run", TerminalStatus: "completed", TerminalAt: time.Now().UTC()}
	adapter := &openClawAdapter{gatewayEnabled: true, gatewayArtifactImportEnabled: true, gatewayToken: "test-token", gatewayURL: "ws" + strings.TrimPrefix(server.URL, "http"), timeout: 5 * time.Second, allowedHost: map[string]bool{"127.0.0.1": true}, gatewayReceiptStore: &fakeOpenClawGatewayReceiptStore{receipts: []OpenClawGatewayReceipt{receipt}}}
	if mode != "url-denied" {
		adapter.artifactDownloadOrigins = []string{contentServer.URL}
	}
	registry := &Registry{adapters: map[string]Adapter{"openclaw": adapter}}
	digest := sha256.Sum256([]byte("openclaw-artifact/v1\nartifact_test"))
	descriptorSize := &size
	if mode == "size-added-after-collection" {
		descriptorSize = nil
	}
	descriptor := GatewayArtifactDescriptor{Digest: hex.EncodeToString(digest[:]), Type: "file", SizeBytes: descriptorSize}
	wrong := receipt
	wrong.OwnerIdentity = "mallory"
	if _, err := registry.DownloadOpenClawGatewayArtifact(context.Background(), wrong, descriptor); err == nil {
		t.Fatal("wrong owner read native content")
	}
	content, err := registry.DownloadOpenClawGatewayArtifact(context.Background(), receipt, descriptor)
	if mode == "url" || mode == "url-denied" || mode == "url-expired" || strings.HasSuffix(mode, "metadata-changed") || mode == "size-added-after-collection" {
		if err == nil || content != nil || strings.Contains(err.Error(), "private") || contentHits.Load() != 0 {
			t.Fatalf("unauthorized/expired content accessed: %v", err)
		}
	} else if err != nil || content == nil || string(content.Data) != data {
		t.Fatalf("large native content: %v", err)
	}
	<-done
	wantMethods := "connect,artifacts.list,artifacts.download"
	if mode == "size-added-after-collection" {
		wantMethods = "connect,artifacts.list"
	}
	if strings.Join(methods, ",") != wantMethods {
		t.Fatalf("unexpected methods: %v", methods)
	}
}

func TestNativeArtifactDownloadContract(t *testing.T) {
	const summary = `{"id":"artifact_test","title":"Report","type":"file","runId":"test-run","sessionKey":"agent:main:test","sizeBytes":2,"download":{"mode":"bytes"}}`
	for _, tt := range []struct {
		name, payload string
		valid         bool
	}{
		{"bytes", `{"artifact":` + summary + `,"encoding":"base64","data":"aGk="}`, true},
		{"missing data", `{"artifact":` + summary + `,"encoding":"base64"}`, false},
		{"wrong size", `{"artifact":` + summary + `,"encoding":"base64","data":"aGVsbG8="}`, false},
		{"wrong id", `{"artifact":` + strings.Replace(summary, "artifact_test", "artifact_other", 1) + `,"encoding":"base64","data":"aGk="}`, false},
		{"wrong run", `{"artifact":` + strings.Replace(summary, "test-run", "another-run", 1) + `,"encoding":"base64","data":"aGk="}`, false},
		{"wrong session", `{"artifact":` + strings.Replace(summary, "agent:main:test", "agent:main:other", 1) + `,"encoding":"base64","data":"aGk="}`, false},
		{"duplicate artifact key", `{"artifact":` + summary + `,"artifact":` + summary + `,"encoding":"base64","data":"aGk="}`, false},
		{"duplicate artifact id", `{"artifact":` + strings.Replace(summary, `"id":"artifact_test"`, `"id":"artifact_test","id":"artifact_test"`, 1) + `,"encoding":"base64","data":"aGk="}`, false},
		{"case-folded duplicate artifact id", `{"artifact":` + strings.Replace(summary, `"id":"artifact_test"`, `"id":"artifact_test","ID":"artifact_test"`, 1) + `,"encoding":"base64","data":"aGk="}`, false},
		{"mixed url", `{"artifact":` + summary + `,"encoding":"base64","data":"aGk=","url":"http://127.0.0.1/private"}`, false},
		{"invalid base64", `{"artifact":` + summary + `,"encoding":"base64","data":"!"}`, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseOpenClawArtifactDownload(json.RawMessage(tt.payload), "artifact_test", OpenClawGatewayReceipt{RunID: "test-run", SessionKey: "agent:main:test"})
			if tt.valid {
				if err != nil || string(got.Data) != "hi" || len(got.ContentSHA256) != 64 {
					t.Fatalf("download: %#v %v", got, err)
				}
				sum := sha256.Sum256([]byte("hi"))
				if got.ContentSHA256 != hex.EncodeToString(sum[:]) {
					t.Fatal("download checksum did not match the returned content")
				}
			} else if err == nil {
				t.Fatal("invalid download accepted")
			}
		})
	}
	tooLarge := strings.Replace(summary, `"sizeBytes":2`, `"sizeBytes":8388609`, 1)
	if _, err := parseOpenClawArtifactDownload(json.RawMessage(`{"artifact":`+tooLarge+`,"encoding":"base64","data":""}`), "artifact_test", OpenClawGatewayReceipt{RunID: "test-run", SessionKey: "agent:main:test"}); !errors.Is(err, ErrOpenClawArtifactUnsupported) {
		t.Fatalf("oversized descriptor was not rejected at the boundary: %v", err)
	}
	negativeSize := strings.Replace(summary, `"sizeBytes":2`, `"sizeBytes":-1`, 1)
	if _, err := parseOpenClawArtifactDownload(json.RawMessage(`{"artifact":`+negativeSize+`,"encoding":"base64","data":""}`), "artifact_test", OpenClawGatewayReceipt{RunID: "test-run", SessionKey: "agent:main:test"}); err == nil {
		t.Fatal("negative descriptor size accepted")
	}
	payload := `{"artifact":` + strings.Replace(summary, `"bytes"`, `"url"`, 1) + `,"url":"https://example.test/private?token=secret"}`
	if _, err := parseOpenClawArtifactDownload(json.RawMessage(payload), "artifact_test", OpenClawGatewayReceipt{RunID: "test-run", SessionKey: "agent:main:test"}); !errors.Is(err, ErrOpenClawArtifactURL) || strings.Contains(err.Error(), "secret") {
		t.Fatal("URL download must be explicit and private")
	}
}

func TestValidateOpenClawArtifactJSONRejectsCaseFoldedDuplicateKeys(t *testing.T) {
	for name, payload := range map[string]string{
		"top-level field": `{"encoding":"base64","Encoding":"base64"}`,
		"nested field":    `{"artifact":{"sessionKey":"agent:main:test","session\u212Aey":"agent:main:test"}}`,
	} {
		t.Run(name, func(t *testing.T) {
			if err := validateOpenClawArtifactJSON([]byte(payload)); err == nil {
				t.Fatal("case-folded duplicate JSON fields were accepted")
			}
		})
	}
}

func TestNativeArtifactDownloadEnforcesExactContentByteLimit(t *testing.T) {
	artifactJSON, err := json.Marshal(map[string]any{
		"id": "artifact_test", "title": "Report", "type": "file", "runId": "test-run",
		"sessionKey": "agent:main:test", "download": map[string]string{"mode": "bytes"},
	})
	if err != nil {
		t.Fatal(err)
	}
	makePayload := func(data []byte) json.RawMessage {
		encodedJSON, err := json.Marshal(base64.StdEncoding.EncodeToString(data))
		if err != nil {
			t.Fatal(err)
		}
		payload := append([]byte(`{"artifact":`), artifactJSON...)
		payload = append(payload, []byte(`,"encoding":"base64","data":`)...)
		payload = append(payload, encodedJSON...)
		payload = append(payload, '}')
		return payload
	}

	data := make([]byte, OpenClawArtifactContentLimit+1)
	receipt := OpenClawGatewayReceipt{RunID: "test-run", SessionKey: "agent:main:test"}
	content, err := parseOpenClawArtifactDownload(makePayload(data[:OpenClawArtifactContentLimit]), "artifact_test", receipt)
	if err != nil || content == nil {
		t.Fatalf("exact limit was rejected: content=%v err=%v", content, err)
	}
	if len(content.Data) != OpenClawArtifactContentLimit {
		t.Fatalf("exact limit returned %d bytes", len(content.Data))
	}
	if _, err := parseOpenClawArtifactDownload(makePayload(data), "artifact_test", receipt); !errors.Is(err, ErrOpenClawArtifactUnsupported) {
		t.Fatalf("limit+1 bytes were not rejected: %v", err)
	}
}
