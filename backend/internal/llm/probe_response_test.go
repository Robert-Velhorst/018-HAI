package llm

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestProviderProbeRejectsNonCatalogJSONAndErrorEnvelopes(t *testing.T) {
	for _, providerID := range []string{"ollama", "custom-openai-compatible"} {
		for _, body := range []string{
			`null`, `true`, `42`, `"PRIVATE_SENTINEL"`, `[]`,
			`{"error":"PRIVATE_SENTINEL"}`,
			`{"error":{"message":"PRIVATE_SENTINEL"},"data":[]}`,
		} {
			t.Run(providerID+"/"+body, func(t *testing.T) {
				requests := 0
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					requests++
					if r.Method != http.MethodGet {
						t.Errorf("probe method = %s; want GET", r.Method)
					}
					w.Header().Set("Content-Type", "application/json")
					_, _ = io.WriteString(w, body)
				}))
				defer server.Close()
				result := probeProviderWithContext(context.Background(), Provider{
					ID: providerID, Enabled: true, Local: true, EndpointURL: server.URL,
				}, Policy{})
				if result.Live || result.Status != "failed" || result.ModelsSeen != 0 || len(result.ReportedModelIDs) != 0 {
					t.Fatalf("non-catalog probe = %#v; want failed with no model evidence", result)
				}
				if strings.Contains(result.Reason, "PRIVATE_SENTINEL") {
					t.Fatal("probe exposed raw response details")
				}
				if requests != 1 {
					t.Fatalf("probe requests = %d; want one read-only attempt", requests)
				}
			})
		}
	}
}

func TestFailedCatalogEvidenceBlocksStrictGenerationDespiteEarlierSuccess(t *testing.T) {
	disableModelMaintenanceForTest(t)
	var failCatalog atomic.Bool
	var generations atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/models" {
			generations.Add(1)
			http.Error(w, "unexpected generation", http.StatusBadRequest)
			return
		}
		if failCatalog.Load() {
			_, _ = io.WriteString(w, `{"error":{"message":"PRIVATE_SENTINEL"}}`)
			return
		}
		_, _ = io.WriteString(w, `{"data":[{"id":"local-model"}]}`)
	}))
	defer server.Close()
	policy := testPolicyWithoutEndpoints()
	provider := policy.Providers[providerIndex(t, policy, "lm-studio")]
	provider.EndpointURL = server.URL
	policy.Providers = []Provider{provider}
	policy.RequireRecentLiveProviderProbe = true
	history := &fakeProbeHistoryRepository{}
	service := withTrustedTestFinalEffects(t, &Service{policy: policy, probeHistory: history})
	first, err := service.ProbeAndRecordProvidersWithContext(context.Background())
	if err != nil || len(first) != 1 || !first[0].Live || first[0].LastSuccessfulAt == nil {
		t.Fatalf("initial catalog evidence = %#v, %v; want recorded success", first, err)
	}
	// The simple history fixture orders by CheckedAt only. Make the earlier
	// observation distinct even on hosts with coarse wall-clock resolution.
	lastSuccess := first[0].LastSuccessfulAt.Add(-time.Second)
	history.probes[0].CheckedAt = lastSuccess
	history.probes[0].LastSuccessfulAt = &lastSuccess
	failCatalog.Store(true)
	second, err := service.ProbeAndRecordProvidersWithContext(context.Background())
	if err != nil || len(second) != 1 || second[0].Live || second[0].Status != "failed" || second[0].LastSuccessfulAt == nil || !second[0].LastSuccessfulAt.Equal(lastSuccess) {
		t.Fatalf("failed catalog evidence = %#v, %v; want failure retaining historical success only", second, err)
	}
	if reason := service.strictProbeReasonWithContext(context.Background(), provider, normalizeProbePolicy(policy), second[0].CheckedAt); reason == "" {
		t.Fatalf("latest failed evidence did not block strict readiness: %#v", history.probes)
	}
	result, err := service.Generate(withTrustedTestEffect(GenerateRequest{
		Task:          "Draft a short response",
		RouteDecision: &RouteDecision{SelectedProviderID: provider.ID, SelectedModelID: "local-model", Tier: TierLocal},
	}))
	if err != nil || result == nil || result.Status != "skipped" || result.Output != "" || generations.Load() != 0 {
		t.Fatalf("strict generation = %#v, %v, calls=%d; want no inference after failed catalog evidence", result, err, generations.Load())
	}
}

func TestProviderProbePreservesEmptyCatalogAndOptionalEmptyError(t *testing.T) {
	for _, providerID := range []string{"ollama", "custom-openai-compatible"} {
		for _, body := range []string{`{}`, `{"models":[],"data":[]}`, `{"error":null,"models":[],"data":[]}`, `{"error":"","models":[],"data":[]}`} {
			t.Run(providerID+"/"+body, func(t *testing.T) {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					_, _ = io.WriteString(w, body)
				}))
				defer server.Close()
				result := probeProviderWithContext(context.Background(), Provider{
					ID: providerID, Enabled: true, Local: true, EndpointURL: server.URL,
				}, Policy{})
				if !result.Live || result.Status != "live" || result.ModelsSeen != 0 {
					t.Fatalf("empty catalog probe = %#v; want live without claiming model availability", result)
				}
			})
		}
	}
}
