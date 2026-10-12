package agentruntime

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestOpenClawUsageSnapshotPreservesReportedModels(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	r := OpenClawGatewayReceipt{ExecutionReference: "test-reference", SessionKey: "agent:main:test", SessionID: "instance-1", CreatedAt: now.Add(-time.Minute)}
	p := usageTestPayload(now, r.SessionID)
	u := p["sessions"].([]any)[0].(map[string]any)["usage"].(map[string]any)
	u["modelUsage"] = []any{map[string]any{"provider": "ollama", "model": "qwen3:14b", "count": 2, "totals": map[string]any{"input": 12, "output": 4, "cacheRead": 3, "cacheWrite": 2, "totalTokens": 21, "totalCost": 0, "missingCostEntries": 0}}}
	data, _ := json.Marshal(p)
	snapshot, err := openClawSessionUsageSnapshot(data, r, now, now)
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.ModelsReported || len(snapshot.Models) != 1 || snapshot.Models[0].Provider != "ollama" || snapshot.Models[0].Model != "qwen3:14b" || snapshot.Models[0].Count != 2 || snapshot.Models[0].Totals.Input != 12 {
		t.Fatalf("reported model data lost: %#v", snapshot)
	}
	if snapshot.Totals.EstimatedCostUSD == nil || *snapshot.Totals.EstimatedCostUSD != 0.25 {
		t.Fatal("top-level estimate lost")
	}
	encoded, _ := json.Marshal(snapshot)
	if strings.Contains(string(encoded), "instance-1") || strings.Contains(string(encoded), "agent:main:test") || strings.Contains(string(encoded), "private-transcript") {
		t.Fatalf("private identity leaked: %s", encoded)
	}
	if !strings.Contains(string(encoded), `"input":12`) {
		t.Fatalf("snapshot is not structured: %s", encoded)
	}
}

func TestOpenClawUsageSnapshotMissingModelsRemainUnknown(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	r := OpenClawGatewayReceipt{SessionKey: "agent:main:test", SessionID: "instance-1", CreatedAt: now.Add(-time.Minute)}
	p := usageTestPayload(now, r.SessionID)
	data, _ := json.Marshal(p)
	snapshot, err := openClawSessionUsageSnapshot(data, r, now, now)
	if err != nil || snapshot.ModelsReported || len(snapshot.Models) != 0 {
		t.Fatalf("missing models were inferred: %#v %v", snapshot, err)
	}
}

func TestOpenClawUsageSnapshotRejectsInvalidModelGroups(t *testing.T) {
	for _, scenario := range []string{"duplicate", "missing_count", "negative_count", "fractional_count", "missing_total", "negative_total", "invalid_name", "too_many"} {
		t.Run(scenario, func(t *testing.T) {
			now := time.Now().UTC().Truncate(time.Millisecond)
			r := OpenClawGatewayReceipt{SessionKey: "agent:main:test", SessionID: "instance-1", CreatedAt: now.Add(-time.Minute)}
			p := usageTestPayload(now, r.SessionID)
			u := p["sessions"].([]any)[0].(map[string]any)["usage"].(map[string]any)
			totals := map[string]any{"input": 12, "output": 4, "cacheRead": 3, "cacheWrite": 2, "totalTokens": 21}
			model := map[string]any{"provider": "ollama", "model": "qwen3:14b", "count": 2, "totals": totals}
			groups := []any{model}
			switch scenario {
			case "duplicate":
				groups = append(groups, model)
			case "missing_count":
				delete(model, "count")
			case "negative_count":
				model["count"] = -1
			case "fractional_count":
				model["count"] = 1.5
			case "missing_total":
				delete(totals, "input")
			case "negative_total":
				totals["output"] = -1
			case "invalid_name":
				model["model"] = "https://host/private?token=secret"
			case "too_many":
				groups = make([]any, 33)
				for i := range groups {
					groups[i] = model
				}
			}
			u["modelUsage"] = groups
			data, err := json.Marshal(p)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := openClawSessionUsageSnapshot(data, r, now, now); err == nil {
				t.Fatalf("accepted invalid model usage: %s", scenario)
			}
		})
	}
}

func TestOpenClawUsageSnapshotModelPriceAndIdentityRemainSourceBacked(t *testing.T) {
	for _, knownPrice := range []bool{false, true} {
		now := time.Now().UTC().Truncate(time.Millisecond)
		r := OpenClawGatewayReceipt{SessionKey: "agent:main:test", SessionID: "instance-1", RequestedModel: "ollama/qwen3:14b", CreatedAt: now.Add(-time.Minute)}
		p := usageTestPayload(now, r.SessionID)
		u := p["sessions"].([]any)[0].(map[string]any)["usage"].(map[string]any)
		totals := map[string]any{"input": 0, "output": 0, "cacheRead": 0, "cacheWrite": 0, "totalTokens": 0, "totalCost": 0}
		if knownPrice {
			totals["missingCostEntries"] = 0
		}
		u["modelUsage"] = []any{map[string]any{"count": 1, "totals": totals}}
		data, _ := json.Marshal(p)
		snapshot, err := openClawSessionUsageSnapshot(data, r, now, now)
		if err != nil {
			t.Fatal(err)
		}
		m := snapshot.Models[0]
		if m.Provider != "" || m.Model != "" {
			t.Fatal("requested model substituted for missing observed identity")
		}
		if (m.Totals.EstimatedCostUSD != nil) != knownPrice {
			t.Fatal("unknown price confused with known zero")
		}
	}
}
