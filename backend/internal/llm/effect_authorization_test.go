package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func trustedTestEffectContext() *EffectContext {
	return &EffectContext{
		OwnerIdentity:         "owner:test-robert",
		ActorIdentity:         "actor:test-hai",
		ActorKind:             "system",
		TaskID:                "task:test-llm-effect",
		ProjectKey:            "project:test-018-hai",
		ApprovalSourceID:      "approval:test-reviewed",
		ApprovalBindingDigest: strings.Repeat("a", 64),
	}
}

func withTrustedTestEffect(request GenerateRequest) GenerateRequest {
	request.EffectContext = trustedTestEffectContext()
	return request
}

func disableModelMaintenanceForTest(t *testing.T) {
	t.Helper()
	t.Setenv("LLM_MODEL_MAINTENANCE_ENABLED", "false")
}

func withTrustedTestFinalEffects(t *testing.T, service *Service) *Service {
	t.Helper()
	effectContext := trustedTestEffectContext()
	service.WithMaintenanceEffectContext(*effectContext)
	return service.WithFinalEffectAuthorization(
		FinalEffectAuthorizerFunc(func(_ context.Context, request FinalEffectAuthorizationRequest) error {
			t.Helper()
			if request.OwnerIdentity != effectContext.OwnerIdentity ||
				request.ActorIdentity != effectContext.ActorIdentity ||
				request.ProjectKey != effectContext.ProjectKey {
				t.Fatalf("authorization received untrusted identity binding: %#v", request)
			}
			if request.Operation == EffectOperationModelPull {
				if !strings.HasPrefix(request.TaskID, "system:model-maintenance:") {
					t.Fatalf("maintenance pull did not receive a server-derived attempt task id: %#v", request)
				}
			} else if request.TaskID != effectContext.TaskID {
				t.Fatalf("authorization received an unexpected task binding: %#v", request)
			}
			if strings.TrimSpace(request.ProviderID) == "" ||
				strings.TrimSpace(request.ModelID) == "" ||
				strings.TrimSpace(request.EndpointKey) == "" ||
				strings.TrimSpace(request.PayloadDigest) == "" ||
				strings.TrimSpace(request.EffectDigest) == "" {
				t.Fatalf("authorization received an incomplete final-effect binding: %#v", request)
			}
			if request.RequiresApproval && request.ApprovalSourceID != effectContext.ApprovalSourceID {
				t.Fatalf("approval-gated effect was not bound to the reviewed approval source: %#v", request)
			}
			return nil
		}),
		EmergencyStopEvaluatorFunc(func(context.Context) (EmergencyStopState, error) {
			return EmergencyStopState{}, nil
		}),
	)
}

func TestModelMaintenanceAttemptContextUsesFreshServerDerivedTaskIdentity(t *testing.T) {
	base := trustedTestEffectContext()
	first := modelMaintenanceAttemptEffectContext(base, "ollama", "qwen2.5:0.5b", time.Unix(1_700_000_000, 1).UTC(), 1)
	second := modelMaintenanceAttemptEffectContext(base, "ollama", "qwen2.5:0.5b", time.Unix(1_700_000_000, 1).UTC(), 2)

	if first == nil || second == nil || first.TaskID == second.TaskID {
		t.Fatalf("attempt contexts must receive distinct task ids: first=%#v second=%#v", first, second)
	}
	if !strings.HasPrefix(first.TaskID, "system:model-maintenance:") || first.OwnerIdentity != base.OwnerIdentity || first.ActorIdentity != base.ActorIdentity || first.ProjectKey != base.ProjectKey {
		t.Fatalf("attempt context did not preserve trusted provenance: %#v", first)
	}
	if base.TaskID != "task:test-llm-effect" {
		t.Fatalf("base effect context was mutated: %#v", base)
	}
}

func TestRefreshOllamaModelUsesFreshFinalEffectAuthorizationPerAttempt(t *testing.T) {
	pulls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/tags":
			_ = json.NewEncoder(w).Encode(map[string]any{"models": []map[string]string{{"name": "qwen2.5:0.5b", "digest": "sha256:current"}}})
		case "/api/pull":
			pulls++
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "success"})
		default:
			t.Fatalf("unexpected Ollama path: %s", request.URL.Path)
		}
	}))
	defer server.Close()

	used := map[string]bool{}
	authorizations := []FinalEffectAuthorizationRequest{}
	service := (&Service{}).WithFinalEffectAuthorization(
		FinalEffectAuthorizerFunc(func(_ context.Context, request FinalEffectAuthorizationRequest) error {
			if used[request.EffectDigest] {
				t.Fatalf("final-effect authorization was reused: %#v", request)
			}
			used[request.EffectDigest] = true
			authorizations = append(authorizations, request)
			return nil
		}),
		EmergencyStopEvaluatorFunc(func(context.Context) (EmergencyStopState, error) {
			return EmergencyStopState{}, nil
		}),
	)
	provider := Provider{ID: "ollama", Name: "Ollama", EndpointURL: server.URL, Enabled: true, Local: true}
	model := Model{ID: "qwen2.5:0.5b", Name: "Qwen", Enabled: true, Tier: TierLocal}
	effectContext := trustedTestEffectContext()

	for attempt := 0; attempt < 2; attempt++ {
		result := service.refreshOllamaModel(context.Background(), provider, model, "test-fingerprint", false, effectContext)
		if result.Status != "current" || result.BlocksExecution {
			t.Fatalf("attempt %d result = %#v", attempt+1, result)
		}
	}
	if pulls != 2 || len(authorizations) != 2 || authorizations[0].EffectDigest == authorizations[1].EffectDigest || authorizations[0].TaskID == authorizations[1].TaskID {
		t.Fatalf("refresh attempts did not receive distinct one-time authorization: pulls=%d authorizations=%#v", pulls, authorizations)
	}
}
