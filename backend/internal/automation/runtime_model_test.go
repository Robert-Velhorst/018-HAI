package automation

import (
	"automation-hub-backend/internal/models"
	"encoding/json"
	"testing"
)

func TestAutomationModelChangeInvalidatesApprovalBinding(t *testing.T) {
	item := &models.Automation{RuntimeType: "openclaw", LaunchType: "agent_runtime"}
	request := TaskLaunchRequest{Task: "Prepare a draft"}
	before := automationActionDigest(item, request)
	if err := json.Unmarshal([]byte(`{"runtimeModel":"ollama/qwen3:14b"}`), item); err != nil {
		t.Fatal(err)
	}
	if automationActionDigest(item, request) == before {
		t.Fatal("changing the stored runtime model must invalidate previous approval")
	}
}
