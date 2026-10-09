package models

import (
	"encoding/json"
	"testing"
)

func TestAutomationRuntimeModelValidation(t *testing.T) {
	for _, tc := range []struct {
		model, runtime string
		valid          bool
	}{
		{"", "hermes", true}, {"ollama/qwen3:14b", "openclaw", true},
		{"openrouter/vendor/model-v1", "openclaw", true},
		{"model-alias", "openclaw", false}, {"ollama/../model", "openclaw", false},
		{"https://user:password@host/model", "openclaw", false}, {"ollama/qwen3:14b", "hermes", false},
	} {
		t.Run(tc.runtime+"/"+tc.model, func(t *testing.T) {
			a := Automation{Name: "Draft", URLPath: "draft", Host: "localhost", Port: 80, LaunchType: "agent_runtime", RuntimeType: tc.runtime}
			payload, _ := json.Marshal(map[string]string{"runtimeModel": tc.model})
			if err := json.Unmarshal(payload, &a); err != nil {
				t.Fatal(err)
			}
			if err := a.Validate(); (err == nil) != tc.valid {
				t.Fatalf("valid=%v, error=%v", tc.valid, err)
			}
		})
	}
}
