package agentruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"golang.org/x/net/websocket"
)

const (
	openClawGatewayInterleavedEventLimit = 256
	openClawGatewayInterleavedByteLimit  = 1024 * 1024
)

// Each HAI connection has one outstanding RPC. Gateway events may interleave
// its response, but cannot settle that RPC or supply task evidence. Discard
// their payloads within both the connection deadline and bounded read budget.
func receiveOpenClawGatewayRPC(connection *websocket.Conn, requestID string) (json.RawMessage, error) {
	return receiveOpenClawGatewayRPCContext(context.Background(), connection, requestID)
}

func sendOpenClawGatewayJSONContext(ctx context.Context, connection *websocket.Conn, value any) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	stop := context.AfterFunc(ctx, func() { _ = connection.Close() })
	err := websocket.JSON.Send(connection, value)
	stop()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}

func receiveOpenClawGatewayRPCContext(ctx context.Context, connection *websocket.Conn, requestID string) (json.RawMessage, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	stop := context.AfterFunc(ctx, func() { _ = connection.Close() })
	defer stop()
	events, eventBytes := 0, 0
	for {
		var raw json.RawMessage
		if err := websocket.JSON.Receive(connection, &raw); err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, err
		}
		var frame struct {
			Type  string          `json:"type"`
			Event string          `json:"event"`
			Seq   json.RawMessage `json:"seq"`
		}
		if err := json.Unmarshal(raw, &frame); err != nil {
			return nil, fmt.Errorf("invalid OpenClaw Gateway frame")
		}
		if frame.Type == "event" {
			if !validGatewayEvidenceValue(frame.Event) || strings.TrimSpace(frame.Event) != frame.Event || !validOpenClawEventSequence(frame.Seq) {
				return nil, fmt.Errorf("invalid OpenClaw Gateway event frame")
			}
			events++
			eventBytes += len(raw)
			if events > openClawGatewayInterleavedEventLimit || eventBytes > openClawGatewayInterleavedByteLimit {
				return nil, fmt.Errorf("OpenClaw Gateway interleaved event limit exceeded")
			}
			continue
		}
		var response openClawGatewayRPCResponse
		if err := json.Unmarshal(raw, &response); err != nil {
			return nil, fmt.Errorf("invalid OpenClaw Gateway response frame")
		}
		return openClawGatewayResponsePayload(response, requestID)
	}
}

func receiveOpenClawGatewayJSONContext(ctx context.Context, connection *websocket.Conn, target any) error {
	stop := context.AfterFunc(ctx, func() { _ = connection.Close() })
	defer stop()
	if err := websocket.JSON.Receive(connection, target); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return err
	}
	return ctx.Err()
}

func validOpenClawEventSequence(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return true
	}
	// A supplied sequence is a non-negative integer, never null or a quoted number.
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return false
	}
	if _, ok := value.(float64); !ok {
		return false
	}
	number, err := json.Number(string(raw)).Int64()
	return err == nil && number >= 0
}
