package agentruntime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/websocket"
)

func TestOpenClawTerminalImportsSessionUsage(t *testing.T) {
	for _, mismatch := range []bool{false, true} {
		t.Run(map[bool]string{false: "matching_instance", true: "reused_key"}[mismatch], func(t *testing.T) {
			now := time.Now().UTC().Truncate(time.Millisecond)
			server := httptest.NewServer(websocket.Server{Handshake: func(*websocket.Config, *http.Request) error { return nil }, Handler: func(c *websocket.Conn) {
				defer c.Close()
				_ = websocket.JSON.Send(c, map[string]any{"type": "event", "event": "connect.challenge", "payload": map[string]any{"nonce": "test-nonce", "ts": now.UnixMilli()}})
				var frame map[string]any
				if websocket.JSON.Receive(c, &frame) != nil {
					return
				}
				params := frame["params"].(map[string]any)
				_ = websocket.JSON.Send(c, map[string]any{"type": "res", "id": frame["id"], "ok": true, "payload": map[string]any{
					"type": "hello-ok", "protocol": 4, "server": map[string]any{"version": "2026.9.1", "connId": "test"},
					"features": map[string]any{"methods": []string{"agent.wait", "sessions.usage"}}, "snapshot": map[string]any{},
					"auth": map[string]any{"role": "operator", "scopes": params["scopes"]}, "policy": map[string]any{"maxPayload": 65536, "maxBufferedBytes": 65536},
				}})
				if websocket.JSON.Receive(c, &frame) != nil {
					return
				}
				var payload any
				switch frame["method"] {
				case "agent.wait":
					payload = map[string]any{"runId": "run-1", "status": "ok", "endedAt": now.UnixMilli()}
				case "sessions.usage":
					p := frame["params"].(map[string]any)
					if p["key"] != "agent:main:test" || p["groupBy"] != "instance" || p["limit"] != float64(1) || p["includeContextWeight"] != false {
						t.Errorf("unbounded usage request: %#v", p)
					}
					id := "instance-1"
					if mismatch {
						id = "another-instance"
					}
					payload = usageTestPayload(now, id)
				default:
					t.Errorf("unexpected method: %v", frame["method"])
					return
				}
				_ = websocket.JSON.Send(c, map[string]any{"type": "res", "id": frame["id"], "ok": true, "payload": payload})
			}})
			defer server.Close()
			t.Setenv("OPENCLAW_GATEWAY_ENABLED", "true")
			t.Setenv("OPENCLAW_GATEWAY_DELEGATION_ENABLED", "true")
			t.Setenv("OPENCLAW_GATEWAY_DELEGATION_TOKEN", "test-write-token")
			t.Setenv("OPENCLAW_GATEWAY_TOKEN", "test-read-token")
			t.Setenv("OPENCLAW_GATEWAY_URL", "ws"+strings.TrimPrefix(server.URL, "http"))
			t.Setenv("AGENT_RUNTIME_ALLOWED_HOSTS", "127.0.0.1")
			t.Setenv("OPENCLAW_TIMEOUT_SECONDS", "2")
			a := newOpenClawAdapterFromEnv()
			ref := openClawGatewayReceiptReference()
			a.gatewayReceiptStore = &fakeOpenClawGatewayReceiptStore{receipts: []OpenClawGatewayReceipt{{ExecutionReference: ref, OwnerIdentity: "alice", RuntimeTaskID: "task-1", SessionKey: "agent:main:test", SessionID: "instance-1", RunID: "run-1", CreatedAt: now.Add(-time.Minute)}}}
			result := a.ReconcileDelegatedSession(context.Background(), "task-1", "alice", ref)
			audit := strings.Join(result.AuditEvents, "\n")
			if result.Status != "completed" {
				t.Fatalf("usage must not change terminal status: %#v", result)
			}
			if mismatch {
				if strings.Contains(audit, "input=12") || !strings.Contains(audit, "usage unavailable") {
					t.Fatalf("reused session key attributed: %s", audit)
				}
			} else if !strings.Contains(audit, "input=12") || !strings.Contains(audit, "estimated USD=0.25") || !strings.Contains(audit, "session-instance") {
				t.Fatalf("native usage not captured: %s", audit)
			}
			if strings.Contains(audit, "private-transcript") || strings.Contains(audit, "instance-1") {
				t.Fatalf("private usage metadata leaked: %s", audit)
			}
		})
	}
}

func usageTestPayload(now time.Time, id string) map[string]any {
	return map[string]any{"updatedAt": now.UnixMilli(), "startDate": now.Add(-time.Minute).Format("2006-01-02"), "endDate": now.Format("2006-01-02"), "sessions": []any{
		map[string]any{"key": "agent:main:test", "sessionId": id, "scope": "instance", "origin": "private-transcript", "usage": map[string]any{
			"sessionId": id, "input": 12, "output": 4, "cacheRead": 3, "cacheWrite": 2, "totalTokens": 21, "totalCost": 0.25, "missingCostEntries": 0,
		}},
	}}
}

func TestOpenClawUsageRejectsUnknownAndStaleCounters(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	receipt := OpenClawGatewayReceipt{SessionKey: "agent:main:test", SessionID: "instance-1", CreatedAt: now.Add(-time.Minute)}
	for _, mode := range []string{"missing", "negative", "stale", "family", "wrong_window", "nested_identity", "duplicate", "fractional", "future"} {
		p := usageTestPayload(now, receipt.SessionID)
		s := p["sessions"].([]any)[0].(map[string]any)
		u := s["usage"].(map[string]any)
		switch mode {
		case "missing":
			delete(u, "input")
		case "negative":
			u["output"] = -1
		case "stale":
			p["updatedAt"] = now.Add(-time.Hour).UnixMilli()
		case "family":
			s["scope"] = "family"
		case "wrong_window":
			p["startDate"] = "2000-01-01"
		case "nested_identity":
			u["sessionId"] = "another-instance"
		case "duplicate":
			p["sessions"] = []any{s, s}
		case "fractional":
			u["input"] = 1.5
		case "future":
			p["updatedAt"] = now.Add(time.Hour).UnixMilli()
		}
		data, _ := json.Marshal(p)
		if _, err := openClawSessionUsageSummary(data, receipt, now, now); err == nil {
			t.Errorf("accepted %s usage", mode)
		}
	}
}

func TestOpenClawUsageDistinguishesMissingPriceFromZero(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	receipt := OpenClawGatewayReceipt{SessionKey: "agent:main:test", SessionID: "instance-1", CreatedAt: now.Add(-time.Minute)}
	for _, missing := range []int{0, 1} {
		p := usageTestPayload(now, receipt.SessionID)
		u := p["sessions"].([]any)[0].(map[string]any)["usage"].(map[string]any)
		u["totalCost"], u["missingCostEntries"] = 0, missing
		data, _ := json.Marshal(p)
		summary, err := openClawSessionUsageSummary(data, receipt, now, now)
		if err != nil {
			t.Fatal(err)
		}
		if missing == 0 && !strings.Contains(summary, "estimated USD=0 ") {
			t.Fatalf("known zero lost: %s", summary)
		}
		if missing == 1 && (!strings.Contains(summary, "estimated cost unavailable") || strings.Contains(summary, "USD=0")) {
			t.Fatalf("missing price treated as zero: %s", summary)
		}
	}
}
