package agentruntime

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/websocket"
)

type gatewayFrameScript func(*websocket.Conn, map[string]any)

func gatewayFrameRoundTrip(t *testing.T, method string, script gatewayFrameScript) (json.RawMessage, error) {
	t.Helper()
	done := make(chan struct{})
	requests := 0
	server := httptest.NewServer(websocket.Server{Handler: func(c *websocket.Conn) {
		defer close(done)
		defer c.Close()
		_ = c.SetDeadline(time.Now().Add(3 * time.Second))
		var request map[string]any
		if err := websocket.JSON.Receive(c, &request); err != nil {
			t.Errorf("receive request: %v", err)
			return
		}
		requests++
		if request["method"] != method {
			t.Errorf("method = %v, want %s", request["method"], method)
		}
		script(c, request)
		for websocket.JSON.Receive(c, &request) == nil {
			requests++
		}
	}})
	defer server.Close()
	c, err := websocket.Dial("ws"+strings.TrimPrefix(server.URL, "http"), "", "http://hai.local")
	if err != nil {
		t.Fatal(err)
	}
	c.MaxPayloadBytes = 64 * 1024
	_ = c.SetDeadline(time.Now().Add(2 * time.Second))
	var payload json.RawMessage
	if _, readOnly := openClawGatewayReadOnlyMethods[method]; readOnly {
		payload, err = openClawGatewayReadOnlyRequest(c, method, map[string]any{})
	} else {
		payload, err = openClawGatewayRequest(c, method, map[string]any{})
	}
	_ = c.Close()
	<-done
	if requests != 1 {
		t.Fatalf("sent %d requests, want one without replay", requests)
	}
	return payload, err
}

func TestOpenClawRequestsAcceptInterleavedEvents(t *testing.T) {
	methods := []string{"agents.list", "artifacts.list", "commands.list", "models.list", "skills.status", "tasks.list", "tools.catalog", "sessions.create", "sessions.abort", "agent.wait"}
	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			payload, err := gatewayFrameRoundTrip(t, method, func(c *websocket.Conn, request map[string]any) {
				for i, event := range []string{"tick", "agent", "future.compatible-event"} {
					if err := websocket.JSON.Send(c, map[string]any{"type": "event", "event": event, "seq": i, "payload": map[string]any{"text": "private unrelated content", "runId": "another-run"}}); err != nil {
						return
					}
				}
				_ = websocket.JSON.Send(c, map[string]any{"type": "res", "id": request["id"], "ok": true, "payload": map[string]any{"expected": true}})
			})
			if err != nil || string(payload) != `{"expected":true}` {
				t.Fatalf("interleaved events prevented the matching response: payload=%s err=%v", payload, err)
			}
		})
	}
}

func TestOpenClawEventsCannotReplaceOrHideRPCFailure(t *testing.T) {
	_, err := gatewayFrameRoundTrip(t, "sessions.create", func(c *websocket.Conn, request map[string]any) {
		_ = websocket.JSON.Send(c, map[string]any{"type": "event", "event": "agent", "payload": map[string]any{"status": "completed", "secret": "private-content"}})
		_ = websocket.JSON.Send(c, map[string]any{"type": "res", "id": request["id"], "ok": false, "error": map[string]any{"code": "FORBIDDEN", "message": "private-content", "details": map[string]any{"token": "private-content"}}})
	})
	var gatewayErr *openClawGatewayResponseError
	if !errors.As(err, &gatewayErr) || gatewayErr.Category != openClawGatewayErrorAccessDenied || strings.Contains(err.Error(), "private-content") {
		t.Fatalf("RPC denial was hidden or leaked by an event: %v", err)
	}
}

func TestOpenClawRequestsRejectInvalidInterleavedFrames(t *testing.T) {
	for name, frame := range map[string]any{
		"missing_event_name":  map[string]any{"type": "event"},
		"empty_event_name":    map[string]any{"type": "event", "event": ""},
		"invalid_sequence":    map[string]any{"type": "event", "event": "tick", "seq": -1},
		"fractional_sequence": map[string]any{"type": "event", "event": "tick", "seq": 1.5},
		"quoted_sequence":     map[string]any{"type": "event", "event": "tick", "seq": "1"},
		"null_sequence":       map[string]any{"type": "event", "event": "tick", "seq": nil},
		"unrelated_response":  map[string]any{"type": "res", "id": "another-request", "ok": true, "payload": map[string]any{"expected": true}},
		"server_request":      map[string]any{"type": "req", "id": "server-request", "method": "exec"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := gatewayFrameRoundTrip(t, "agent.wait", func(c *websocket.Conn, request map[string]any) {
				_ = websocket.JSON.Send(c, frame)
				_ = websocket.JSON.Send(c, map[string]any{"type": "res", "id": request["id"], "ok": true, "payload": map[string]any{"expected": true}})
			})
			if err == nil {
				t.Fatal("invalid frame was silently skipped")
			}
		})
	}
}

func TestOpenClawEventsAloneCannotCompleteRPC(t *testing.T) {
	_, err := gatewayFrameRoundTrip(t, "agent.wait", func(c *websocket.Conn, _ map[string]any) {
		_ = websocket.JSON.Send(c, map[string]any{"type": "event", "event": "agent", "payload": map[string]any{"status": "completed"}})
		_ = c.Close()
	})
	if err == nil {
		t.Fatal("a lifecycle event without a matching RPC response completed the request")
	}
}

func TestOpenClawAcceptsResponseAtEventCountLimit(t *testing.T) {
	payload, err := gatewayFrameRoundTrip(t, "agent.wait", func(c *websocket.Conn, request map[string]any) {
		for i := 0; i < 256; i++ {
			if websocket.JSON.Send(c, map[string]any{"type": "event", "event": "tick", "seq": i}) != nil {
				return
			}
		}
		_ = websocket.JSON.Send(c, map[string]any{"type": "res", "id": request["id"], "ok": true, "payload": map[string]any{"expected": true}})
	})
	if err != nil || string(payload) != `{"expected":true}` {
		t.Fatalf("response at event limit was rejected: payload=%s err=%v", payload, err)
	}
}

func TestOpenClawEventFloodIsBounded(t *testing.T) {
	for _, large := range []bool{false, true} {
		t.Run(map[bool]string{false: "frame_count", true: "aggregate_bytes"}[large], func(t *testing.T) {
			_, err := gatewayFrameRoundTrip(t, "agent.wait", func(c *websocket.Conn, request map[string]any) {
				body := ""
				count := 257
				if large {
					body = strings.Repeat("x", 60*1024)
					count = 20
				}
				for i := 0; i < count; i++ {
					if websocket.JSON.Send(c, map[string]any{"type": "event", "event": "agent", "seq": i, "payload": body}) != nil {
						return
					}
				}
				_ = websocket.JSON.Send(c, map[string]any{"type": "res", "id": request["id"], "ok": true, "payload": map[string]any{"expected": true}})
			})
			if err == nil || !strings.Contains(err.Error(), "event limit") {
				t.Fatalf("event flood should exhaust the bounded event limit: %v", err)
			}
		})
	}
}

func TestOpenClawGatewayRPCReadStopsWhenContextIsCancelled(t *testing.T) {
	serverDone := make(chan struct{})
	server := httptest.NewServer(websocket.Server{Handler: func(connection *websocket.Conn) {
		defer close(serverDone)
		defer connection.Close()
		var request map[string]any
		if err := websocket.JSON.Receive(connection, &request); err != nil {
			return
		}
		var unexpected map[string]any
		if err := websocket.JSON.Receive(connection, &unexpected); err == nil {
			t.Errorf("received an unexpected follow-up frame: %#v", unexpected)
		}
	}})
	defer server.Close()

	connection, err := websocket.Dial("ws"+strings.TrimPrefix(server.URL, "http"), "", "http://hai.local")
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer connection.Close()
	if err := websocket.JSON.Send(connection, map[string]any{"type": "req", "id": "request-1", "method": "agent.wait"}); err != nil {
		t.Fatalf("send request: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err = receiveOpenClawGatewayRPCContext(ctx, connection, "request-1")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("RPC read error = %v, want context deadline", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("cancelled RPC read took %s", elapsed)
	}
	select {
	case <-serverDone:
	case <-time.After(time.Second):
		t.Fatal("cancellation did not close the Gateway connection")
	}
}

func TestOpenClawGatewayRequestDoesNotSendAfterCancellation(t *testing.T) {
	serverDone := make(chan struct{})
	receivedRequest := make(chan bool, 1)
	server := httptest.NewServer(websocket.Server{Handler: func(connection *websocket.Conn) {
		defer close(serverDone)
		defer connection.Close()
		_ = connection.SetReadDeadline(time.Now().Add(time.Second))
		var request map[string]any
		err := websocket.JSON.Receive(connection, &request)
		receivedRequest <- err == nil
	}})
	defer server.Close()

	connection, err := websocket.Dial("ws"+strings.TrimPrefix(server.URL, "http"), "", "http://hai.local")
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = openClawGatewayRequestContext(ctx, connection, "sessions.abort", map[string]any{"runId": "run-test"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("request error = %v, want context cancellation", err)
	}
	_ = connection.Close()
	select {
	case received := <-receivedRequest:
		if received {
			t.Fatal("a cancelled context sent an OpenClaw Gateway mutation")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Gateway did not observe the closed connection")
	}
	select {
	case <-serverDone:
	case <-time.After(time.Second):
		t.Fatal("Gateway handler did not finish after connection close")
	}
}
