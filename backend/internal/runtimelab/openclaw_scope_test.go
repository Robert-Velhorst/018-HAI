package runtimelab

import "testing"

func TestOpenClawIntegrationScopeKeepsUnwiredInfrastructureInBacklog(t *testing.T) {
	inventory, ok, err := newTestService(t).RuntimeFeatureParity("openclaw")
	if err != nil || !ok {
		t.Fatalf("OpenClaw inventory unavailable: %v", err)
	}
	byID := map[string]RuntimeFeature{}
	for _, item := range inventory.Features {
		byID[item.ID] = item
	}
	for _, id := range []string{"openclaw-planning", "openclaw-memory", "openclaw-scheduling", "openclaw-models", "openclaw-host-tools", "openclaw-product-shell"} {
		t.Run(id, func(t *testing.T) {
			item, found := byID[id]
			if !found {
				t.Fatal("required integration area is missing")
			}
			if item.ImplementationStatus == "implemented" || item.ImplementationStatus == "excluded" {
				t.Errorf("HAI-native equivalents do not prove OpenClaw integration: status=%s", item.ImplementationStatus)
			}
			if item.Disposition != DispositionDeferred && item.Disposition != DispositionAdaptedForHAI {
				t.Errorf("reusable infrastructure must stay in integration scope: disposition=%s", item.Disposition)
			}
			if item.BacklogPriority == "" || item.RecommendedPath == "" || len(item.Requirements) == 0 || item.ExclusionReason != "" {
				t.Errorf("unwired infrastructure needs actionable backlog, not exclusion: %#v", item)
			}
		})
	}
}
