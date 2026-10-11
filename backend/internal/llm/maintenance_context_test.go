package llm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunDueModelMaintenanceContextStopsBeforeWorkWhenCanceled(t *testing.T) {
	history := &fakeModelMaintenanceRepository{}
	service := &Service{
		policy:             testPolicyWithoutEndpoints(),
		maintenanceHistory: history,
		maintenanceRunning: map[string]*sync.Mutex{},
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	run, err := service.RunDueModelMaintenanceContext(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("RunDueModelMaintenanceContext error = %v, want context.Canceled", err)
	}
	if run.Eligible != 0 || len(run.Results) != 0 {
		t.Fatalf("canceled run performed work: %#v", run)
	}
	if len(history.records) != 0 {
		t.Fatalf("canceled run persisted maintenance records: %#v", history.records)
	}
}

func TestEnsureModelFreshContextCancelsWhileWaitingForRefreshLock(t *testing.T) {
	provider, model := maintenanceTestProviderAndModel(t, "http://127.0.0.1:11434")
	key := provider.ID + "/" + model.ID
	lock := &sync.Mutex{}
	lock.Lock()
	defer lock.Unlock()
	history := &fakeModelMaintenanceRepository{}
	service := &Service{
		policy:             Policy{Providers: []Provider{provider}},
		maintenanceHistory: history,
		maintenanceRunning: map[string]*sync.Mutex{key: lock},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer cancel()
	started := time.Now()

	result, err := service.ensureModelFreshContext(ctx, provider, model)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("ensureModelFreshContext error = %v, want deadline exceeded", err)
	}
	if result.Status != "interrupted" {
		t.Fatalf("maintenance result = %#v, want interrupted", result)
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("lock wait ignored cancellation for %s", elapsed)
	}
	if len(history.records) != 0 {
		t.Fatalf("lock cancellation persisted maintenance records: %#v", history.records)
	}
}

func TestEnsureModelFreshContextCancelsProviderInspectionWithoutRecordingFailure(t *testing.T) {
	requestStarted := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			t.Errorf("path = %s, want /api/tags", r.URL.Path)
			return
		}
		close(requestStarted)
		<-r.Context().Done()
	}))
	defer server.Close()

	provider, model := maintenanceTestProviderAndModel(t, server.URL)
	history := &fakeModelMaintenanceRepository{}
	service := &Service{
		policy:             Policy{Providers: []Provider{provider}},
		maintenanceHistory: history,
		maintenanceRunning: map[string]*sync.Mutex{},
	}
	ctx, cancel := context.WithCancel(context.Background())
	resultCh := make(chan ModelMaintenanceResult, 1)
	errCh := make(chan error, 1)
	go func() {
		result, err := service.ensureModelFreshContext(ctx, provider, model)
		resultCh <- result
		errCh <- err
	}()

	waitForMaintenanceRequest(t, requestStarted)
	cancel()
	result := waitForMaintenanceResult(t, resultCh)
	if err := <-errCh; !errors.Is(err, context.Canceled) {
		t.Fatalf("ensureModelFreshContext error = %v, want context.Canceled", err)
	}
	if result.Status != "interrupted" || len(history.records) != 0 {
		t.Fatalf("canceled inspection result=%#v records=%#v", result, history.records)
	}
}

func TestGenerateContextCancelsModelPullBeforeGenerationOrFallback(t *testing.T) {
	pullStarted := make(chan struct{})
	releasePull := make(chan struct{})
	var generationCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			_ = json.NewEncoder(w).Encode(map[string]any{"models": []map[string]string{{"name": "phi3:mini", "digest": "sha256:current"}}})
		case "/api/pull":
			close(pullStarted)
			<-releasePull
		case "/api/generate", "/v1/chat/completions":
			generationCalls.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]string{"response": "must not run"})
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer func() {
		close(releasePull)
		server.Close()
	}()

	provider, model := maintenanceTestProviderAndModel(t, server.URL)
	history := &fakeModelMaintenanceRepository{}
	generationHistory := &fakeGenerationHistoryRepository{}
	service := withTrustedTestFinalEffects(t, &Service{
		policy:             Policy{Providers: []Provider{provider}},
		usage:              map[string]UsageCounter{},
		maintenanceHistory: history,
		generationHistory:  generationHistory,
		maintenanceRunning: map[string]*sync.Mutex{},
	})
	ctx, cancel := context.WithCancel(context.Background())
	resultCh := make(chan *GenerationResult, 1)
	errCh := make(chan error, 1)
	go func() {
		result, err := service.GenerateContext(ctx, withTrustedTestEffect(GenerateRequest{
			Task: "Summarize this note",
			RouteDecision: &RouteDecision{
				SelectedProviderID: provider.ID,
				SelectedModelID:    model.ID,
				SelectedModelName:  model.Name,
				Tier:               model.Tier,
				FallbackPath: []FallbackOption{{
					ProviderID: "unused-fallback",
					ModelID:    "unused-model",
				}},
			},
		}))
		resultCh <- result
		errCh <- err
	}()

	waitForMaintenanceRequest(t, pullStarted)
	cancel()
	result := waitForGenerationResult(t, resultCh)
	if err := <-errCh; err != nil {
		t.Fatalf("GenerateContext: %v", err)
	}
	if result.Status != "stopped" || result.InputTokens != 0 || result.OutputTokens != 0 || result.EstimatedCostEUR != 0 {
		t.Fatalf("canceled generation = %#v", result)
	}
	if generationCalls.Load() != 0 || len(service.usage) != 0 {
		t.Fatalf("canceled maintenance continued to generation: calls=%d usage=%#v", generationCalls.Load(), service.usage)
	}
	if len(history.records) != 0 {
		t.Fatalf("canceled pull persisted a model failure: %#v", history.records)
	}
	if len(generationHistory.records) != 1 || generationHistory.records[0].Status != "stopped" {
		t.Fatalf("generation history = %#v, want one stopped record", generationHistory.records)
	}
}

func maintenanceTestProviderAndModel(t *testing.T, endpoint string) (Provider, Model) {
	t.Helper()
	policy := testPolicyWithoutEndpoints()
	provider := policy.Providers[providerIndex(t, policy, "ollama")]
	provider.EndpointURL = endpoint
	model := Model{
		ID:            "phi3:mini",
		Name:          "Phi small local",
		Tier:          TierLocal,
		Capabilities:  []string{"general", "extraction"},
		MaxDifficulty: 5,
		MaxReasoning:  "very_high",
		Enabled:       true,
	}
	provider.Models = []Model{model}
	return provider, model
}

func waitForMaintenanceRequest(t *testing.T, started <-chan struct{}) {
	t.Helper()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("model maintenance request did not start")
	}
}

func waitForMaintenanceResult(t *testing.T, results <-chan ModelMaintenanceResult) ModelMaintenanceResult {
	t.Helper()
	select {
	case result := <-results:
		return result
	case <-time.After(time.Second):
		t.Fatal("model maintenance did not return after cancellation")
		return ModelMaintenanceResult{}
	}
}

func waitForGenerationResult(t *testing.T, results <-chan *GenerationResult) *GenerationResult {
	t.Helper()
	select {
	case result := <-results:
		return result
	case <-time.After(time.Second):
		t.Fatal("generation did not return after maintenance cancellation")
		return nil
	}
}
