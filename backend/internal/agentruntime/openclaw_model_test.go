package agentruntime

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"strings"
	"testing"
)

func TestOpenClawModelBindsFinalEffect(t *testing.T) {
	task := approvedRuntimeTask("model-task", "Prepare a draft")
	before := runtimeFinalEffectRequest("openclaw", task, Info{RequiresApproval: true})
	if err := json.Unmarshal([]byte(`{"RuntimeModel":"ollama/qwen3:14b"}`), &task); err != nil {
		t.Fatal(err)
	}
	after := runtimeFinalEffectRequest("openclaw", task, Info{RequiresApproval: true})
	if finalEffectRequestDigest(before) == finalEffectRequestDigest(after) {
		t.Fatal("model selection must change the final-effect authorization binding")
	}
	proof := FinalEffectAuthorizationProof{ReceiptID: uuid.NewString(), RuntimeRequestDigest: finalEffectRequestDigest(before),
		AuthorizationRequestDigest: strings.Repeat("a", 64), DecisionDigest: strings.Repeat("b", 64)}
	if err := validateFinalEffectAuthorizationProof(before, proof); err != nil {
		t.Fatal(err)
	}
	if err := validateFinalEffectAuthorizationProof(after, proof); err == nil {
		t.Fatal("old proof authorized changed model")
	}
}

func TestOpenClawModelCannotSilentlyUseCLI(t *testing.T) {
	adapter := newOpenClawAdapterFromEnv()
	adapter.gatewayDelegationEnabled = false
	task := approvedRuntimeTask("model-task", "Prepare a draft")
	task.RuntimeModel = "ollama/qwen3:14b"
	result := adapter.ExecuteTask(context.Background(), task)
	if result.Status != "blocked" || !strings.Contains(result.Message, "run-bound") {
		t.Fatalf("CLI must not ignore model selection: %#v", result)
	}
}

func TestEmptyRuntimeModelPreservesLegacyEffectEncoding(t *testing.T) {
	request := runtimeFinalEffectRequest("openclaw", approvedRuntimeTask("task", "Draft"), Info{RequiresApproval: true})
	payload, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(payload), "RuntimeModel") {
		t.Fatal("empty model changed legacy authorization encoding")
	}
}

func TestOpenClawModelRejectedBeforeGatewayAccess(t *testing.T) {
	for _, model := range []string{"ollama/unapproved", "qwen3", "https://host/model", "ollama/../model", " ollama/qwen3:14b"} {
		t.Run(model, func(t *testing.T) {
			t.Setenv("OPENCLAW_GATEWAY_DELEGATION_ENABLED", "true")
			t.Setenv("OPENCLAW_GATEWAY_ALLOWED_MODELS", "ollama/qwen3:14b")
			adapter := newOpenClawAdapterFromEnv()
			task := approvedRuntimeTask("model-task", "Prepare a draft")
			payload, _ := json.Marshal(map[string]string{"RuntimeModel": model})
			if err := json.Unmarshal(payload, &task); err != nil {
				t.Fatal(err)
			}
			result := adapter.ExecuteTask(context.Background(), task)
			if result.Status != "blocked" || !strings.Contains(result.Message, "model") {
				t.Fatalf("expected model rejection before Gateway configuration/access, got %#v", result)
			}
		})
	}
}
