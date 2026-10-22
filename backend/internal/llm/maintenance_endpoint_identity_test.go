package llm

import (
	"context"
	"testing"
)

func TestMaintenanceEndpointKeyDoesNotAliasDistinctTargets(t *testing.T) {
	for _, tc := range []struct {
		name, first, second string
	}{
		{"mount case", "http://localhost/API", "http://localhost/api"},
		{"nested mount case", "http://localhost/team/API/v1", "http://localhost/team/api/v1"},
		{"version case", "http://localhost/api/V1", "http://localhost/api/v1"},
		{"empty segment", "http://localhost/team//api", "http://localhost/team/api"},
		{"IPv6 port boundary", "http://[::1]:443", "http://[::1:443]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			first, firstErr := maintenanceEndpointKey(tc.first)
			second, secondErr := maintenanceEndpointKey(tc.second)
			if firstErr == nil && secondErr == nil && first == second {
				t.Fatalf("distinct endpoints share maintenance identity %q", first)
			}
		})
	}
}

func TestConfiguredPlanningMaintenanceRejectsOtherMountIdentity(t *testing.T) {
	t.Setenv("LLM_MODEL_MAINTENANCE_ENABLED", "false")
	provider := Provider{
		ID: "lm-studio", Enabled: true, Local: true,
		EndpointURL: "http://localhost/team/api", Models: []Model{{ID: "model-1", Enabled: true}},
	}
	service := &Service{policy: Policy{LocalModelsAllowed: true, Providers: []Provider{provider}}}
	if !service.maintenanceEligibleProvider(provider) {
		t.Fatal("configured fixture is not eligible for the planning gate")
	}
	if err := service.EnsureConfiguredLocalModelWithContext(context.Background(), provider.EndpointURL+"/v1", "model-1"); err != nil {
		t.Fatalf("configured mount was rejected: %v", err)
	}
	for _, endpoint := range []string{"http://localhost/team/API", "http://localhost/team//api", "http://localhost/team/api/V1"} {
		if err := service.EnsureConfiguredLocalModelWithContext(context.Background(), endpoint, "model-1"); err == nil {
			t.Errorf("planning gate accepted a different mount: %s", endpoint)
		}
	}
}

func TestMaintenanceEndpointKeyPreservesIPv6Authority(t *testing.T) {
	for endpoint, want := range map[string]string{
		"http://[::1]:11434/v1/":        "http://[::1]:11434",
		"http://[::1]:80/v1":            "http://[::1]",
		"https://LOCALHOST:443/API/v1/": "https://localhost/API",
	} {
		got, err := maintenanceEndpointKey(endpoint)
		if err != nil || got != want {
			t.Errorf("maintenanceEndpointKey(%q)=%q, %v; want %q", endpoint, got, err, want)
		}
	}
}
