package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"automation-hub-backend/internal/models"
)

type synchronizedProviderProbeHistory struct {
	mu       sync.Mutex
	probes   []models.LLMProviderProbe
	writeErr error
}

func (r *synchronizedProviderProbeHistory) RecordProviderProbe(probe *models.LLMProviderProbe) (*models.LLMProviderProbe, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.writeErr != nil {
		return nil, r.writeErr
	}
	copy := *probe
	if copy.CheckedAt.IsZero() {
		copy.CheckedAt = time.Now().UTC()
	}
	if copy.Live {
		lastSuccess := copy.CheckedAt
		copy.LastSuccessfulAt = &lastSuccess
	} else {
		for index := len(r.probes) - 1; index >= 0; index-- {
			if r.probes[index].ProviderID == copy.ProviderID && r.probes[index].LastSuccessfulAt != nil {
				lastSuccess := *r.probes[index].LastSuccessfulAt
				copy.LastSuccessfulAt = &lastSuccess
				break
			}
		}
	}
	r.probes = append(r.probes, copy)
	return &copy, nil
}

func (r *synchronizedProviderProbeHistory) FindRecentProviderProbes(limit int) ([]models.LLMProviderProbe, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if limit <= 0 || limit > len(r.probes) {
		limit = len(r.probes)
	}
	results := make([]models.LLMProviderProbe, 0, limit)
	for index := len(r.probes) - 1; index >= 0 && len(results) < limit; index-- {
		results = append(results, r.probes[index])
	}
	return results, nil
}

func (r *synchronizedProviderProbeHistory) FindLatestProviderProbe(providerID string) (*models.LLMProviderProbe, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var latest *models.LLMProviderProbe
	for index := range r.probes {
		probe := r.probes[index]
		if probe.ProviderID == providerID && (latest == nil || probe.CheckedAt.After(latest.CheckedAt)) {
			copy := probe
			latest = &copy
		}
	}
	return latest, nil
}

func (r *synchronizedProviderProbeHistory) RecordProviderProbeWithContext(ctx context.Context, probe *models.LLMProviderProbe) (*models.LLMProviderProbe, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return r.RecordProviderProbe(probe)
}

func (r *synchronizedProviderProbeHistory) FindRecentProviderProbesWithContext(ctx context.Context, limit int) ([]models.LLMProviderProbe, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return r.FindRecentProviderProbes(limit)
}

func (r *synchronizedProviderProbeHistory) FindLatestProviderProbeWithContext(ctx context.Context, providerID string) (*models.LLMProviderProbe, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return r.FindLatestProviderProbe(providerID)
}

type providerProbeSchedulerMaintenanceRepository struct {
	*fakeModelMaintenanceRepository
	mu   sync.Mutex
	held bool
}

func (r *providerProbeSchedulerMaintenanceRepository) AcquireModelMaintenanceLease(context.Context, string, string) (func(), bool, error) {
	r.mu.Lock()
	if r.held {
		r.mu.Unlock()
		return func() {}, false, nil
	}
	r.held = true
	r.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			r.mu.Lock()
			r.held = false
			r.mu.Unlock()
		})
	}, true, nil
}

func newProviderProbeSchedulerTestService(endpoint string, history *synchronizedProviderProbeHistory) *Service {
	return &Service{
		policy: Policy{
			LocalModelsAllowed:             true,
			FreeCloudQuotaAllowed:          true,
			RequireRecentLiveProviderProbe: true,
			ProviderProbeMaxAgeSeconds:     90,
			Providers: []Provider{{
				ID: "probe-test", Name: "Probe test", Enabled: true,
				EndpointURL: endpoint, QuotaRemaining: 1,
				Models: []Model{{ID: "test-model", Name: "Test model", Enabled: true}},
			}},
		},
		probeHistory: history,
		maintenanceHistory: &providerProbeSchedulerMaintenanceRepository{
			fakeModelMaintenanceRepository: &fakeModelMaintenanceRepository{},
		},
	}
}

func newProviderProbeTestServer(handler http.HandlerFunc) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/models" {
			http.Error(w, "unexpected provider probe request", http.StatusMethodNotAllowed)
			return
		}
		handler(w, r)
	}))
}

func writeProviderProbeSuccess(w http.ResponseWriter) {
	_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]string{{"id": "test-model"}}})
}

func TestStrictProviderProbeSchedulerRefreshesFreshnessThroughProbeContract(t *testing.T) {
	var requests atomic.Int32
	server := newProviderProbeTestServer(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		writeProviderProbeSuccess(w)
	})
	defer server.Close()
	history := &synchronizedProviderProbeHistory{}
	service := newProviderProbeSchedulerTestService(server.URL, history)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	wait := func(context.Context, time.Duration, <-chan struct{}) modelMaintenanceSchedulerEvent {
		cancel()
		return modelMaintenanceSchedulerCancelled
	}

	runProviderProbeSchedulerWithWait(ctx, func() bool { return true }, service, wait, time.Now)

	latest, err := findLatestProviderProbeWithContext(context.Background(), history, "probe-test")
	if err != nil {
		t.Fatalf("read persisted provider probe: %v", err)
	}
	if latest == nil || !latest.Live || latest.Status != "live" {
		t.Fatalf("latest probe = %#v, want fresh live evidence", latest)
	}
	if reason := service.strictProbeReason(service.policy.Providers[0], service.policy, time.Now().UTC()); reason != "" {
		t.Fatalf("strict provider freshness still blocks after scheduler refresh: %s", reason)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("provider transport requests = %d, want one GET probe", got)
	}
}

func TestStrictProviderProbeSchedulerPausesAndResumesOnGlobalGate(t *testing.T) {
	var requests atomic.Int32
	server := newProviderProbeTestServer(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		writeProviderProbeSuccess(w)
	})
	defer server.Close()
	service := newProviderProbeSchedulerTestService(server.URL, &synchronizedProviderProbeHistory{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var allowed atomic.Bool
	var waits atomic.Int32
	wait := func(context.Context, time.Duration, <-chan struct{}) modelMaintenanceSchedulerEvent {
		if waits.Add(1) == 1 {
			if got := requests.Load(); got != 0 {
				t.Fatalf("scheduler probed while globally paused: requests=%d", got)
			}
			allowed.Store(true)
			return modelMaintenanceSchedulerPermissionPoll
		}
		cancel()
		return modelMaintenanceSchedulerCancelled
	}

	runProviderProbeSchedulerWithWait(ctx, allowed.Load, service, wait, time.Now)

	if got := requests.Load(); got != 1 {
		t.Fatalf("provider transport requests after resume = %d, want one", got)
	}
}

func TestStrictProviderProbeSchedulerCancelsInFlightProbeWhenPaused(t *testing.T) {
	started := make(chan struct{})
	cancelled := make(chan struct{})
	server := newProviderProbeTestServer(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
		close(cancelled)
	})
	defer server.Close()
	history := &synchronizedProviderProbeHistory{}
	service := newProviderProbeSchedulerTestService(server.URL, history)
	ctx, cancel := context.WithCancel(context.Background())
	var allowed atomic.Bool
	allowed.Store(true)
	done := make(chan struct{})
	go func() {
		defer close(done)
		wait := func(waitCtx context.Context, _ time.Duration, _ <-chan struct{}) modelMaintenanceSchedulerEvent {
			<-waitCtx.Done()
			return modelMaintenanceSchedulerCancelled
		}
		runProviderProbeSchedulerWithWait(ctx, allowed.Load, service, wait, time.Now)
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		cancel()
		t.Fatal("scheduled provider probe did not reach the fake transport")
	}
	allowed.Store(false)
	select {
	case <-cancelled:
	case <-time.After(2 * time.Second):
		cancel()
		t.Fatal("global pause did not cancel the in-flight provider request")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("provider probe scheduler did not stop after cancellation")
	}
	latest, err := findLatestProviderProbeWithContext(context.Background(), history, "probe-test")
	if err != nil {
		t.Fatalf("read provider history: %v", err)
	}
	if latest != nil {
		t.Fatalf("cancelled probe persisted readiness evidence: %#v", latest)
	}
}

func TestStrictProviderProbeFailureUsesDurableBackoff(t *testing.T) {
	var requests atomic.Int32
	server := newProviderProbeTestServer(func(w http.ResponseWriter, _ *http.Request) {
		if requests.Add(1) == 1 {
			http.Error(w, "temporarily unavailable", http.StatusBadGateway)
			return
		}
		writeProviderProbeSuccess(w)
	})
	defer server.Close()
	history := &synchronizedProviderProbeHistory{}
	service := newProviderProbeSchedulerTestService(server.URL, history)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var retryDelay time.Duration
	wait := func(_ context.Context, delay time.Duration, _ <-chan struct{}) modelMaintenanceSchedulerEvent {
		retryDelay = delay
		cancel()
		return modelMaintenanceSchedulerCancelled
	}
	runProviderProbeSchedulerWithWait(ctx, func() bool { return true }, service, wait, time.Now)

	failed, err := findLatestProviderProbeWithContext(context.Background(), history, "probe-test")
	if err != nil {
		t.Fatalf("read failed provider probe: %v", err)
	}
	if failed == nil || failed.Live || failed.Status != "failed" {
		t.Fatalf("latest probe = %#v, want persisted failure", failed)
	}
	if retryDelay < providerProbeSchedulerFailureBackoff/2 || retryDelay > providerProbeSchedulerFailureBackoff {
		t.Fatalf("scheduled retry delay = %s, want bounded durable backoff near %s", retryDelay, providerProbeSchedulerFailureBackoff)
	}

	policy := service.Policy()
	restarted := newProviderProbeSchedulerTestService(server.URL, history)
	provider, dueAt, err := restarted.nextScheduledProviderProbe(context.Background(), policy, failed.CheckedAt.Add(time.Second))
	if err != nil {
		t.Fatalf("read persisted retry schedule after restart: %v", err)
	}
	if provider == nil || !dueAt.Equal(failed.CheckedAt.Add(providerProbeSchedulerFailureBackoff)) {
		t.Fatalf("durable retry = provider %#v at %s, want retry at %s", provider, dueAt, failed.CheckedAt.Add(providerProbeSchedulerFailureBackoff))
	}

	probed, err := restarted.runNextDueProviderProbeWithLease(context.Background(), dueAt)
	if err != nil || !probed {
		t.Fatalf("retry after durable backoff = probed %v, error %v", probed, err)
	}
	latest, err := findLatestProviderProbeWithContext(context.Background(), history, "probe-test")
	if err != nil || latest == nil || !latest.Live {
		t.Fatalf("latest retry probe = %#v, error %v; want recovered live evidence", latest, err)
	}
}

func TestStrictProviderProbeSchedulerDoesNotProbeOverBudgetPaidProvider(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requests.Add(1)
	}))
	defer server.Close()
	now := time.Now().UTC()
	policy := Policy{
		PaidCallsAllowed:               true,
		DailyPaidBudgetEUR:             1,
		RequireApprovalBeforePaidUsage: false,
		FreeCloudQuotaAllowed:          true,
		RequireRecentLiveProviderProbe: true,
		ProviderProbeMaxAgeSeconds:     90,
		Providers: []Provider{{
			ID: "paid-probe-test", Name: "Paid probe test", Enabled: true, Paid: true,
			EndpointURL: server.URL, QuotaRemaining: 1,
			Models: []Model{{ID: "paid-model", Name: "Paid model", Enabled: true}},
		}},
	}
	usage := &fakeGenerationHistoryRepository{records: []models.LLMGenerationRecord{{
		ProviderID: "paid-probe-test", ModelID: "paid-model", Status: "completed",
		EstimatedCostEUR: 1, UsageSource: "estimated", LoggedAt: now,
	}}}
	service := &Service{
		policy: policy, generationHistory: usage,
		probeHistory:       &synchronizedProviderProbeHistory{},
		maintenanceHistory: &providerProbeSchedulerMaintenanceRepository{fakeModelMaintenanceRepository: &fakeModelMaintenanceRepository{}},
	}

	results := service.ProbeProviders()
	if len(results) != 1 || results[0].Status != "blocked" {
		t.Fatalf("paid provider probe = %#v, want blocked by exhausted budget", results)
	}
	ctx, cancel := context.WithCancel(context.Background())
	wait := func(_ context.Context, _ time.Duration, _ <-chan struct{}) modelMaintenanceSchedulerEvent {
		cancel()
		return modelMaintenanceSchedulerCancelled
	}
	runProviderProbeSchedulerWithWait(ctx, func() bool { return true }, service, wait, time.Now)
	if got := requests.Load(); got != 0 {
		t.Fatalf("paid provider transport requests = %d after budget exhaustion, want zero", got)
	}
	if len(usage.records) != 1 {
		t.Fatalf("probe changed generation/billing history: records=%d, want unchanged", len(usage.records))
	}
}

func TestStrictProviderProbeSchedulerRequiresCrossProcessLease(t *testing.T) {
	var requests atomic.Int32
	server := newProviderProbeTestServer(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		writeProviderProbeSuccess(w)
	})
	defer server.Close()
	service := newProviderProbeSchedulerTestService(server.URL, &synchronizedProviderProbeHistory{})
	service.maintenanceHistory = &fakeModelMaintenanceRepository{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if startProviderProbeScheduler(ctx, service, func() bool { return true }) {
		t.Fatal("provider probe scheduler started without a cross-process lease")
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("provider transport requests without lease = %d, want zero", got)
	}
}

func TestStartModelMaintenanceSchedulerStartsStrictProbeRefresh(t *testing.T) {
	t.Setenv("LLM_MODEL_MAINTENANCE_SCHEDULER_ENABLED", "true")
	t.Setenv("LLM_MODEL_MAINTENANCE_ENABLED", "false")
	requested := make(chan struct{}, 1)
	server := newProviderProbeTestServer(func(w http.ResponseWriter, _ *http.Request) {
		requested <- struct{}{}
		writeProviderProbeSuccess(w)
	})
	defer server.Close()
	service := newProviderProbeSchedulerTestService(server.URL, &synchronizedProviderProbeHistory{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	StartModelMaintenanceScheduler(ctx, service, func() bool { return true })
	select {
	case <-requested:
	case <-time.After(2 * time.Second):
		t.Fatal("strict provider probe scheduler did not run from the shared scheduler entrypoint")
	}
	cancel()
}
