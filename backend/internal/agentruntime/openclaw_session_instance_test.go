package agentruntime

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestOpenClawCreatedSessionPreservesInstance(t *testing.T) {
	receipt, err := openClawGatewayCreatedSession(json.RawMessage(`{"ok":true,"key":"agent:main:hai-task","sessionId":"private-instance-123","runId":"private-run-123","runStarted":true}`))
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(receipt)
	if err != nil || !strings.Contains(string(encoded), `"SessionID":"private-instance-123"`) {
		t.Fatalf("session instance was discarded: %s (%v)", encoded, err)
	}
}

func TestOpenClawCreatedSessionRejectsMalformedInstance(t *testing.T) {
	for _, id := range []string{" ", "bad\ninstance", strings.Repeat("x", 257)} {
		payload, _ := json.Marshal(map[string]any{"ok": true, "key": "agent:main:hai-task", "sessionId": id, "runId": "run-123", "runStarted": true})
		if _, err := openClawGatewayCreatedSession(payload); err == nil {
			t.Errorf("accepted malformed instance %q", id)
		}
	}
}

func TestOpenClawCreatedSessionAcceptsLegacyMissingInstance(t *testing.T) {
	if _, err := openClawGatewayCreatedSession(json.RawMessage(`{"ok":true,"key":"agent:main:hai-task","runId":"run-123","runStarted":true}`)); err != nil {
		t.Fatal(err)
	}
}
