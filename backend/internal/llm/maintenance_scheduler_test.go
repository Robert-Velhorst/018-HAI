package llm

import (
	"automation-hub-backend/internal/models"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type cancellationLeaseRepository struct {
	*fakeModelMaintenanceRepository
	cancel   context.CancelFunc
	releases int
}

func (r *cancellationLeaseRepository) AcquireModelMaintenanceLease(context.Context, string, string) (func(), bool, error) {
	r.cancel()
	return func() { r.releases++ }, true, nil
}

type missingReleaseLeaseRepository struct {
	*fakeModelMaintenanceRepository
}

func (r *missingReleaseLeaseRepository) AcquireModelMaintenanceLease(context.Context, string, string) (func(), bool, error) {
	return nil, true, nil
}

type observedModelMaintenanceLeaseRepository struct {
	*fakeModelMaintenanceRepository
	leaseHeld atomic.Bool
	releases  atomic.Int32
}

func (r *observedModelMaintenanceLeaseRepository) AcquireModelMaintenanceLease(context.Context, string, string) (func(), bool, error) {
	r.leaseHeld.Store(true)
	return func() {
		r.leaseHeld.Store(false)
		r.releases.Add(1)
	}, true, nil
}

type completedDuringModelMaintenanceLeaseRepository struct {
	*fakeModelMaintenanceRepository
	record   models.LLMModelMaintenance
	releases atomic.Int32
}

func (r *completedDuringModelMaintenanceLeaseRepository) AcquireModelMaintenanceLease(ctx context.Context, _, _ string) (func(), bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	if _, err := r.fakeModelMaintenanceRepository.RecordModelMaintenance(&r.record); err != nil {
		return nil, false, err
	}
	return func() { r.releases.Add(1) }, true, nil
}

type blockingContextMaintenanceHistoryRepository struct {
	*fakeModelMaintenanceRepository
	started chan string
}

type failingModelMaintenanceRepository struct {
	*fakeModelMaintenanceRepository
	err error
}

type legacyOnlyMaintenanceRepository struct{ inner ModelMaintenanceRepository }

func (r legacyOnlyMaintenanceRepository) RecordModelMaintenance(record *models.LLMModelMaintenance) (*models.LLMModelMaintenance, error) {
	return r.inner.RecordModelMaintenance(record)
}

func (r legacyOnlyMaintenanceRepository) FindLatestModelMaintenance(providerID, modelID string) (*models.LLMModelMaintenance, error) {
	return r.inner.FindLatestModelMaintenance(providerID, modelID)
}

func (r legacyOnlyMaintenanceRepository) FindRecentModelMaintenance(limit int) ([]models.LLMModelMaintenance, error) {
	return r.inner.FindRecentModelMaintenance(limit)
}

func (r *failingModelMaintenanceRepository) RecordModelMaintenance(*models.LLMModelMaintenance) (*models.LLMModelMaintenance, error) {
	return nil, r.err
}

func (r *failingModelMaintenanceRepository) RecordModelMaintenanceWithContext(ctx context.Context, _ *models.LLMModelMaintenance) (*models.LLMModelMaintenance, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, r.err
}

func (r *blockingContextMaintenanceHistoryRepository) FindLatestModelMaintenanceWithContext(ctx context.Context, _, _ string) (*models.LLMModelMaintenance, error) {
	r.started <- "latest"
	<-ctx.Done()
	return nil, ctx.Err()
}

func (r *blockingContextMaintenanceHistoryRepository) FindRecentModelMaintenanceWithContext(ctx context.Context, _ int) ([]models.LLMModelMaintenance, error) {
	r.started <- "recent"
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestLockModelMaintenanceStopsWaitingWhenContextIsCancelled(t *testing.T) {
	lock := &sync.Mutex{}
	lock.Lock()
	defer lock.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan bool, 1)
	go func() {
		done <- lockModelMaintenance(ctx, lock)
	}()
	cancel()

	select {
	case acquired := <-done:
		if acquired {
			t.Fatal("cancelled wait unexpectedly acquired the model lock")
		}
	case <-time.After(time.Second):
		t.Fatal("lock wait did not stop after cancellation")
	}
}

func TestRunDueModelMaintenanceReturnsWithoutWorkWhenAlreadyCancelled(t *testing.T) {
	t.Setenv("LLM_MODEL_MAINTENANCE_ENABLED", "true")
	history := &fakeModelMaintenanceRepository{}
	service := &Service{policy: testPolicyWithoutEndpoints(), maintenanceHistory: history, maintenanceRunning: map[string]*sync.Mutex{}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	run := service.RunDueModelMaintenanceWithContext(ctx)
	if !run.Cancelled || run.Eligible != 0 || len(run.Results) != 0 {
		t.Fatalf("cancelled run = %#v, want no work and cancelled=true", run)
	}
	if len(history.records) != 0 {
		t.Fatalf("cancelled run wrote maintenance history: %#v", history.records)
	}
}

func TestEnsureModelFreshReleasesLeaseWhenContextCancelsDuringAcquisition(t *testing.T) {
	t.Setenv("LLM_MODEL_MAINTENANCE_ENABLED", "true")
	policy := testPolicyWithoutEndpoints()
	policy.Providers[0].EndpointURL = "http://127.0.0.1:11434"
	model := policy.Providers[0].Models[0]
	ctx, cancel := context.WithCancel(context.Background())
	history := &cancellationLeaseRepository{fakeModelMaintenanceRepository: &fakeModelMaintenanceRepository{}, cancel: cancel}
	service := &Service{policy: policy, maintenanceHistory: history, maintenanceRunning: map[string]*sync.Mutex{}}

	result := service.ensureModelFreshWithContext(ctx, policy.Providers[0], model)
	if result.Status != "cancelled" {
		t.Fatalf("maintenance result = %#v, want cancelled", result)
	}
	if history.releases != 1 {
		t.Fatalf("acquired maintenance leases released = %d, want 1", history.releases)
	}
	if len(history.records) != 0 {
		t.Fatalf("cancelled pre-verification maintenance wrote history: %#v", history.records)
	}
}

func TestEnsureModelFreshFailsClosedWhenAcquiredLeaseCannotBeReleased(t *testing.T) {
	t.Setenv("LLM_MODEL_MAINTENANCE_ENABLED", "true")
	var providerCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		providerCalls.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]string{{"id": "configured-model"}}})
	}))
	defer server.Close()

	provider := Provider{
		ID: "lm-studio", Name: "LM Studio", Enabled: true, Local: true, EndpointURL: server.URL,
		Models: []Model{{ID: "configured-model", Name: "Configured model", Enabled: true}},
	}
	history := &missingReleaseLeaseRepository{fakeModelMaintenanceRepository: &fakeModelMaintenanceRepository{}}
	service := &Service{policy: Policy{LocalModelsAllowed: true, Providers: []Provider{provider}}, maintenanceHistory: history, maintenanceRunning: map[string]*sync.Mutex{}}
	result := service.ensureModelFresh(provider, provider.Models[0])
	if result.Status != "failed" || !result.BlocksExecution {
		t.Fatalf("maintenance result = %#v, want fail-closed lease error", result)
	}
	if got := providerCalls.Load(); got != 0 {
		t.Fatalf("provider was contacted %d times without a releasable lease", got)
	}
	if len(history.records) != 1 || history.records[0].Status != "failed" || !history.records[0].BlocksExecution {
		t.Fatalf("lease contract failure was not durably blocked: %#v", history.records)
	}
}

func TestRunDueModelMaintenanceCountsLeaseContentionAsInProgressAndBlocksUse(t *testing.T) {
	t.Setenv("LLM_MODEL_MAINTENANCE_ENABLED", "true")
	provider := Provider{
		ID: "test-local", Name: "Test local", Enabled: true, Local: true,
		EndpointURL: "http://127.0.0.1:11434",
		Models:      []Model{{ID: "configured-model", Name: "Configured model", Enabled: true}},
	}
	history := &leasedModelMaintenanceRepository{
		fakeModelMaintenanceRepository: &fakeModelMaintenanceRepository{},
		acquired:                       false,
	}
	service := &Service{
		policy:             Policy{LocalModelsAllowed: true, Providers: []Provider{provider}},
		maintenanceHistory: history, maintenanceRunning: map[string]*sync.Mutex{},
	}

	run := service.RunDueModelMaintenance()
	if run.InProgress != 1 || run.Failed != 0 || run.Checked != 1 || len(run.Results) != 1 {
		t.Fatalf("lease-contention run = %#v, want one in-progress check and no failures", run)
	}
	if result := run.Results[0]; result.Status != "in_progress" || !result.BlocksExecution || result.NextCheckDueAt == nil {
		t.Fatalf("lease-contention result = %#v, want in_progress and fail-closed", result)
	}
	if delay := modelMaintenanceSchedulerNextDelay(run, time.Now().UTC()); delay > modelMaintenanceFailureRetryInterval() || delay <= 0 {
		t.Fatalf("scheduler delay after lease contention = %s, want short positive retry no longer than %s", delay, modelMaintenanceFailureRetryInterval())
	}
	if len(history.records) != 0 {
		t.Fatalf("lease contention persisted misleading failure history: %#v", history.records)
	}
}

func TestModelMaintenanceReadinessFailureHonorsCooldownAndCredentialRecovery(t *testing.T) {
	t.Setenv("LLM_MODEL_MAINTENANCE_ENABLED", "true")
	t.Setenv("LLM_TEST_PROVIDER_KEY", "")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Fatalf("provider probe path = %s, want /v1/models", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer restored-key" {
			t.Fatalf("provider authorization = %q, want restored credential", r.Header.Get("Authorization"))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]string{{"id": "configured-model"}}})
	}))
	defer server.Close()

	provider := Provider{
		ID: "test-local", Name: "Test local runtime", Enabled: true, Local: true, EndpointURL: server.URL,
		APIKeyEnv: "LLM_TEST_PROVIDER_KEY", Models: []Model{{ID: "configured-model", Name: "Configured model", Enabled: true}},
	}
	history := &fakeModelMaintenanceRepository{}
	service := &Service{policy: Policy{LocalModelsAllowed: true, Providers: []Provider{provider}}, maintenanceHistory: history, maintenanceRunning: map[string]*sync.Mutex{}}

	first := service.ensureModelFresh(provider, provider.Models[0])
	if first.Status != "failed" || !first.BlocksExecution || len(history.records) != 1 {
		t.Fatalf("missing-credential result = %#v, history=%#v", first, history.records)
	}
	second := service.ensureModelFresh(provider, provider.Models[0])
	if second.Status != "failed" || !second.BlocksExecution || !second.Reused || len(history.records) != 1 {
		t.Fatalf("cooldown result = %#v, history=%#v; expected reuse without another write", second, history.records)
	}

	t.Setenv("LLM_TEST_PROVIDER_KEY", "restored-key")
	recovered := service.ensureModelFresh(provider, provider.Models[0])
	if recovered.Status != "operator_managed" || !recovered.BlocksExecution || recovered.Reused || len(history.records) != 2 {
		t.Fatalf("credential recovery result = %#v, history=%#v; changed readiness must trigger an immediate blocked version check", recovered, history.records)
	}
}

func TestModelMaintenanceFailureRetryCooldownStartsAfterDelayedFailure(t *testing.T) {
	t.Setenv("LLM_MODEL_MAINTENANCE_ENABLED", "true")
	t.Setenv("LLM_MODEL_MAINTENANCE_FAILURE_RETRY_MINUTES", "5")
	t.Setenv("LLM_TEST_PROVIDER_KEY_DELAYED_FAILURE", "test-key")
	requestStarted := make(chan struct{}, 1)
	releaseFailure := make(chan struct{})
	var releaseOnce sync.Once
	releaseProvider := func() { releaseOnce.Do(func() { close(releaseFailure) }) }
	providerFailureCompleted := make(chan time.Time, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requestStarted <- struct{}{}
		<-releaseFailure
		providerFailureCompleted <- time.Now().UTC()
		http.Error(w, "temporary provider failure", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	defer releaseProvider()

	provider := Provider{
		ID: "test-local", Name: "Test local runtime", Enabled: true, Local: true, EndpointURL: server.URL,
		APIKeyEnv: "LLM_TEST_PROVIDER_KEY_DELAYED_FAILURE",
		Models:    []Model{{ID: "configured-model", Name: "Configured model", Enabled: true}},
	}
	history := &fakeModelMaintenanceRepository{}
	service := &Service{policy: Policy{LocalModelsAllowed: true, Providers: []Provider{provider}}, maintenanceHistory: history, maintenanceRunning: map[string]*sync.Mutex{}}
	startedAt := time.Now().UTC()
	finished := make(chan ModelMaintenanceResult, 1)
	go func() { finished <- service.ensureModelFresh(provider, provider.Models[0]) }()

	select {
	case <-requestStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("maintenance did not reach the local test provider")
	}
	delay := time.NewTimer(75 * time.Millisecond)
	select {
	case <-delay.C:
	case <-time.After(2 * time.Second):
		delay.Stop()
		t.Fatal("timed out waiting to release the delayed provider failure")
	}
	releaseProvider()
	result := <-finished
	failureCompletedAt := <-providerFailureCompleted
	if result.Status != "failed" || !result.BlocksExecution {
		t.Fatalf("delayed provider result = %#v, want fail-closed failure", result)
	}
	if failureCompletedAt.Sub(startedAt) < 60*time.Millisecond {
		t.Fatalf("provider failure completed after %s, want a deliberately delayed failure", failureCompletedAt.Sub(startedAt))
	}
	if result.CheckedAt.Before(failureCompletedAt) {
		t.Fatalf("durable checkedAt %s predates failed provider completion %s", result.CheckedAt, failureCompletedAt)
	}
	if result.NextCheckDueAt == nil || result.NextCheckDueAt.Sub(failureCompletedAt) < 5*time.Minute {
		t.Fatalf("retry deadline %v does not preserve a full five-minute cooldown after completion %s", result.NextCheckDueAt, failureCompletedAt)
	}
	if len(history.records) != 1 || !history.records[0].CheckedAt.Equal(result.CheckedAt) {
		t.Fatalf("persisted failure history = %#v, want the completion-based timestamp", history.records)
	}
}

func TestModelMaintenanceCredentialRotationInvalidatesPersistedSuccess(t *testing.T) {
	t.Setenv("LLM_MODEL_MAINTENANCE_ENABLED", "true")
	t.Setenv("LLM_TEST_PROVIDER_KEY_ROTATION", "key-A")
	credentials := make(chan string, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		credentials <- r.Header.Get("Authorization")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]string{{"id": "configured-model"}}})
	}))
	defer server.Close()

	provider := Provider{
		ID: "test-local", Name: "Test local runtime", Enabled: true, Local: true, EndpointURL: server.URL,
		APIKeyEnv: "LLM_TEST_PROVIDER_KEY_ROTATION",
		Models:    []Model{{ID: "configured-model", Name: "Configured model", Enabled: true}},
	}
	history := &fakeModelMaintenanceRepository{}
	firstService := &Service{policy: Policy{LocalModelsAllowed: true, Providers: []Provider{provider}}, maintenanceHistory: history, maintenanceRunning: map[string]*sync.Mutex{}}
	first := firstService.ensureModelFresh(provider, provider.Models[0])
	if first.Status != "operator_managed" || !first.BlocksExecution || first.Reused || len(history.records) != 1 {
		t.Fatalf("initial credential check = %#v, persisted records = %d", first, len(history.records))
	}
	if got := <-credentials; got != "Bearer key-A" {
		t.Fatalf("first provider authorization = %q, want key-A", got)
	}
	firstFingerprint := history.records[0].ConfigurationFingerprint

	t.Setenv("LLM_TEST_PROVIDER_KEY_ROTATION", "key-B")
	secondService := &Service{policy: Policy{LocalModelsAllowed: true, Providers: []Provider{provider}}, maintenanceHistory: history, maintenanceRunning: map[string]*sync.Mutex{}}
	second := secondService.ensureModelFresh(provider, provider.Models[0])
	if second.Status != "operator_managed" || !second.BlocksExecution || second.Reused || len(history.records) != 2 {
		t.Fatalf("rotated credential check = %#v, persisted records = %d; want immediate recheck", second, len(history.records))
	}
	if got := <-credentials; got != "Bearer key-B" {
		t.Fatalf("rotated provider authorization = %q, want key-B", got)
	}
	if firstFingerprint == history.records[1].ConfigurationFingerprint {
		t.Fatal("valid-to-valid credential rotation did not change the persisted configuration fingerprint")
	}
	for _, record := range history.records {
		if len(record.ConfigurationFingerprint) != 64 || strings.Contains(record.ConfigurationFingerprint, "key-A") || strings.Contains(record.ConfigurationFingerprint, "key-B") {
			t.Fatalf("persisted credential fingerprint is not an opaque SHA-256 digest: %q", record.ConfigurationFingerprint)
		}
	}
	serialized, err := json.Marshal(history.records)
	if err != nil {
		t.Fatalf("serialize persisted maintenance history: %v", err)
	}
	if strings.Contains(string(serialized), "key-A") || strings.Contains(string(serialized), "key-B") {
		t.Fatal("persisted maintenance history exposed a raw provider credential")
	}
}

func TestPaidProviderMaintenanceRequiresApprovalAndChecksCatalogAfterApproval(t *testing.T) {
	t.Setenv("LLM_MODEL_MAINTENANCE_ENABLED", "true")
	t.Setenv("LLM_MODEL_MAINTENANCE_INTERVAL_HOURS", "24")
	t.Setenv("LLM_PAID_CLOUD_MAINTENANCE_KEY", "catalog-secret")
	var providerCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/models" || r.ContentLength != 0 {
			t.Errorf("provider request = %s %s content-length=%d; want bodyless GET /v1/models", r.Method, r.URL.Path, r.ContentLength)
			http.Error(w, "unexpected request", http.StatusMethodNotAllowed)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer catalog-secret" {
			t.Errorf("provider authorization = %q, want configured bearer credential", got)
		}
		providerCalls.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]string{{"id": "paid-model"}}})
	}))
	defer server.Close()

	provider := Provider{
		ID: "paid-provider", Name: "Paid provider", Enabled: true, Paid: true, EndpointURL: server.URL, APIKeyEnv: "LLM_PAID_CLOUD_MAINTENANCE_KEY",
		Models: []Model{{ID: "paid-model", Name: "Paid model", Enabled: true}},
	}
	history := &fakeModelMaintenanceRepository{}
	service := &Service{
		policy: Policy{
			PaidCallsAllowed: true, DailyPaidBudgetEUR: 10, RequireApprovalBeforePaidUsage: true,
			UsageAccountingStatus: "durable",
			Providers:             []Provider{provider},
		},
		maintenanceHistory: history,
		maintenanceRunning: map[string]*sync.Mutex{},
	}

	first := service.RunDueModelMaintenance()
	if first.Failed != 1 || first.Checked != 0 || first.ProviderManaged != 0 || len(first.Results) != 1 {
		t.Fatalf("approval-gated maintenance run = %#v", first)
	}
	result := first.Results[0]
	if result.Status != "approval_required" || !result.BlocksExecution || result.Reused || result.UpdateAttempted || result.UpdateApplied {
		t.Fatalf("approval-gated result = %#v", result)
	}
	if result.NextCheckDueAt == nil || !result.NextCheckDueAt.Equal(result.CheckedAt.Add(24*time.Hour)) {
		t.Fatalf("approval-gated result due time = %v; want one day after the check", result.NextCheckDueAt)
	}
	if providerCalls.Load() != 0 {
		t.Fatalf("provider was contacted %d times before approval", providerCalls.Load())
	}
	if len(history.records) != 1 || history.records[0].Status != "approval_required" || !history.records[0].BlocksExecution {
		t.Fatalf("approval gate was not persisted as a blocked state: %#v", history.records)
	}

	second := service.RunDueModelMaintenance()
	if second.Checked != 0 || second.Failed != 1 || second.Reused != 1 || len(second.Results) != 1 || !second.Results[0].Reused {
		t.Fatalf("approval-required state was not reused without retry churn: %#v", second)
	}
	if providerCalls.Load() != 0 {
		t.Fatalf("provider was contacted %d times while approval remained required", providerCalls.Load())
	}

	service.policy.RequireApprovalBeforePaidUsage = false
	approvedPolicyRun := service.RunDueModelMaintenance()
	if approvedPolicyRun.Checked != 1 || approvedPolicyRun.ProviderManaged != 1 || approvedPolicyRun.Failed != 0 || len(approvedPolicyRun.Results) != 1 || approvedPolicyRun.Results[0].Status != "provider_managed" || approvedPolicyRun.Results[0].Reused {
		t.Fatalf("approved cloud catalog check = %#v", approvedPolicyRun)
	}
	if providerCalls.Load() != 1 {
		t.Fatalf("provider calls after approval = %d, want one read-only catalog request", providerCalls.Load())
	}
}

func TestPaidNonOllamaLocalModelRequiresExplicitMaintenanceApproval(t *testing.T) {
	t.Setenv("LLM_MODEL_MAINTENANCE_ENABLED", "true")
	var providerCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		providerCalls.Add(1)
		if r.URL.Path != "/v1/models" {
			t.Fatalf("paid local provider path = %s, want /v1/models", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]string{{"id": "paid-local-model"}}})
	}))
	defer server.Close()

	provider := Provider{
		ID: "paid-local-runtime", Name: "Paid local runtime", Enabled: true, Local: true, Paid: true, EndpointURL: server.URL,
		Models: []Model{{ID: "paid-local-model", Name: "Paid local model", Enabled: true}},
	}
	history := &fakeModelMaintenanceRepository{}
	service := &Service{
		policy: Policy{
			LocalModelsAllowed: true, PaidCallsAllowed: true, DailyPaidBudgetEUR: 10,
			RequireApprovalBeforePaidUsage: true, UsageAccountingStatus: "durable", Providers: []Provider{provider},
		},
		maintenanceHistory: history, maintenanceRunning: map[string]*sync.Mutex{},
	}

	err := service.EnsureConfiguredLocalModelWithContext(context.Background(), server.URL, "paid-local-model")
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "approval") {
		t.Fatalf("unapproved paid local model admission error = %v, want explicit approval block", err)
	}
	if got := providerCalls.Load(); got != 0 {
		t.Fatalf("paid local provider was contacted %d times before approval", got)
	}
	if len(history.records) != 1 || history.records[0].Status != "approval_required" || !history.records[0].BlocksExecution {
		t.Fatalf("approval-required state = %#v, want one durable execution-blocking decision", history.records)
	}

	service.policy.RequireApprovalBeforePaidUsage = false
	if err := service.EnsureConfiguredLocalModelWithContext(context.Background(), server.URL, "paid-local-model"); err == nil || !strings.Contains(err.Error(), "cannot verify its upstream version") {
		t.Fatalf("model admission after explicit paid-use approval = %v, want the independent version-verification block", err)
	}
	if got := providerCalls.Load(); got != 1 {
		t.Fatalf("provider checks after explicit approval = %d, want exactly one", got)
	}
	if len(history.records) != 2 || history.records[1].Status != "operator_managed" || !history.records[1].BlocksExecution {
		t.Fatalf("approved local model verification = %#v, want a blocked result because its version remains unverifiable", history.records)
	}
}

func TestMaintenanceHistoryWriteFailureUsesOwnerModelScopedCooldown(t *testing.T) {
	t.Setenv("LLM_MODEL_MAINTENANCE_ENABLED", "true")
	var providerCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		providerCalls.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]string{
			{"id": "configured-model"}, {"id": "other-model"},
		}})
	}))
	defer server.Close()

	provider := Provider{
		ID: "test-local", Name: "Test local runtime", Enabled: true, Local: true, EndpointURL: server.URL,
		Models: []Model{
			{ID: "configured-model", Name: "Configured model", Enabled: true},
			{ID: "other-model", Name: "Other model", Enabled: true},
		},
	}
	history := &failingModelMaintenanceRepository{fakeModelMaintenanceRepository: &fakeModelMaintenanceRepository{}, err: errors.New("database unavailable")}
	service := &Service{
		policy:             Policy{LocalModelsAllowed: true, Providers: []Provider{provider}},
		maintenanceHistory: history, maintenanceRunning: map[string]*sync.Mutex{},
	}
	ownerA := &EffectContext{OwnerIdentity: "owner-a"}
	ownerB := &EffectContext{OwnerIdentity: "owner-b"}

	first := service.ensureModelFreshWithContext(context.Background(), provider, provider.Models[0], ownerA)
	if first.Status != "failed" || !first.BlocksExecution || first.NextCheckDueAt == nil {
		t.Fatalf("initial persistence failure = %#v, want blocked retry deadline", first)
	}
	second := service.ensureModelFreshWithContext(context.Background(), provider, provider.Models[0], ownerA)
	if second.Status != "failed" || !second.BlocksExecution || second.NextCheckDueAt == nil || !strings.Contains(second.Reason, "local retry cooldown") {
		t.Fatalf("repeated same-owner check = %#v, want local persistence-failure cooldown", second)
	}
	if got := providerCalls.Load(); got != 1 {
		t.Fatalf("same-owner model check reached provider %d times, want one", got)
	}

	otherOwner := service.ensureModelFreshWithContext(context.Background(), provider, provider.Models[0], ownerB)
	if otherOwner.Status != "failed" || !otherOwner.BlocksExecution {
		t.Fatalf("different-owner check = %#v, want its own failed persistence result", otherOwner)
	}
	otherModel := service.ensureModelFreshWithContext(context.Background(), provider, provider.Models[1], ownerA)
	if otherModel.Status != "failed" || !otherModel.BlocksExecution {
		t.Fatalf("different-model check = %#v, want its own failed persistence result", otherModel)
	}
	if got := providerCalls.Load(); got != 3 {
		t.Fatalf("owner/model-scoped provider checks = %d, want 3 independent checks", got)
	}
	if len(service.maintenanceCooldowns) != 3 {
		t.Fatalf("local cooldown entries = %d, want three owner/model scopes", len(service.maintenanceCooldowns))
	}
}

func TestMaintenanceHistoryWriteFailurePreventsRepeatedOllamaPull(t *testing.T) {
	t.Setenv("LLM_MODEL_MAINTENANCE_ENABLED", "true")
	var tagsCalls, pullCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			digest := "sha256:old"
			if tagsCalls.Add(1) > 1 {
				digest = "sha256:new"
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"models": []map[string]string{{"name": "phi3:mini", "digest": digest}}})
		case "/api/pull":
			pullCalls.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "success"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	policy := testPolicyWithoutEndpoints()
	policy.LocalModelsAllowed = true
	index := providerIndex(t, policy, "ollama")
	policy.Providers[index].Enabled = true
	policy.Providers[index].EndpointURL = server.URL
	policy.Providers[index].Models = []Model{{ID: "phi3:mini", Name: "Phi", Tier: TierLocal, Enabled: true}}
	history := &failingModelMaintenanceRepository{fakeModelMaintenanceRepository: &fakeModelMaintenanceRepository{}, err: errors.New("database unavailable")}
	service := withTrustedTestFinalEffects(t, &Service{
		policy: policy, maintenanceHistory: history, maintenanceRunning: map[string]*sync.Mutex{},
	})
	provider := service.policy.Providers[index]
	model := provider.Models[0]

	first := service.ensureModelFresh(provider, model, service.maintenanceEffectContext)
	if first.Status != "failed" || !first.BlocksExecution || !first.UpdateAttempted || first.NextCheckDueAt == nil {
		t.Fatalf("Ollama result after history-write failure = %#v, want blocked persisted-failure cooldown", first)
	}
	second := service.ensureModelFresh(provider, model, service.maintenanceEffectContext)
	if second.Status != "failed" || !second.BlocksExecution || second.NextCheckDueAt == nil || !strings.Contains(second.Reason, "durable model maintenance admission") {
		t.Fatalf("second Ollama maintenance result = %#v, want durable retry-admission block", second)
	}
	if got := pullCalls.Load(); got != 1 {
		t.Fatalf("Ollama pull attempts = %d, want one while failed history write is cooling down", got)
	}
	if got := tagsCalls.Load(); got != 2 {
		t.Fatalf("Ollama tag checks = %d, want no provider I/O during local cooldown", got)
	}
}

func TestMaintenanceHistoryWriteFailureCooldownClearsOnConfigChangeOrVerifiedSuccess(t *testing.T) {
	t.Setenv("LLM_MODEL_MAINTENANCE_ENABLED", "true")
	var firstEndpointCalls, secondEndpointCalls atomic.Int32
	newServer := func(calls *atomic.Int32) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/tags" {
				http.NotFound(w, r)
				return
			}
			calls.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{"models": []map[string]string{{"name": "phi3:mini", "digest": "sha256:verified"}}})
		}))
	}
	firstServer := newServer(&firstEndpointCalls)
	defer firstServer.Close()
	secondServer := newServer(&secondEndpointCalls)
	defer secondServer.Close()

	provider := Provider{
		ID: "ollama", Name: "Ollama", Enabled: true, Local: true, EndpointURL: firstServer.URL,
		Models: []Model{{ID: "phi3:mini", Name: "Phi", Tier: TierLocal, Enabled: true}},
	}
	history := &failingModelMaintenanceRepository{fakeModelMaintenanceRepository: &fakeModelMaintenanceRepository{}, err: errors.New("database unavailable")}
	service := &Service{
		policy:             Policy{LocalModelsAllowed: true, Providers: []Provider{provider}},
		maintenanceHistory: history, maintenanceRunning: map[string]*sync.Mutex{},
	}
	owner := &EffectContext{OwnerIdentity: "owner-a"}
	initial := service.ensureModelFreshWithContext(context.Background(), provider, provider.Models[0], owner)
	if initial.Status != "failed" || len(service.maintenanceCooldowns) != 1 {
		t.Fatalf("initial result = %#v, cooldowns=%d; want one cached persistence failure", initial, len(service.maintenanceCooldowns))
	}

	provider.EndpointURL = secondServer.URL
	changed := service.ensureModelFreshWithContext(context.Background(), provider, provider.Models[0], owner)
	if changed.Status != "failed" || !changed.BlocksExecution || firstEndpointCalls.Load() != 1 || secondEndpointCalls.Load() != 1 {
		t.Fatalf("configuration-change result = %#v; endpoint calls %d/%d", changed, firstEndpointCalls.Load(), secondEndpointCalls.Load())
	}
	changedFingerprint := modelMaintenanceFingerprint(provider, provider.Models[0], service.policy)
	key := modelMaintenanceCooldownKeyFor(modelMaintenanceScope{ownerIdentity: owner.OwnerIdentity, local: true}, provider.ID, provider.Models[0].ID)
	if cooldown, ok := service.maintenanceCooldowns[key]; !ok || cooldown.configurationFingerprint != changedFingerprint {
		t.Fatalf("cooldown after configuration change = %#v, found=%v; want only the new fingerprint", cooldown, ok)
	}

	service.maintenanceHistory = &fakeModelMaintenanceRepository{records: []models.LLMModelMaintenance{{
		ProviderID: provider.ID, ProviderName: provider.Name, ModelID: provider.Models[0].ID, ModelName: provider.Models[0].Name,
		Status: "current", ConfigurationFingerprint: changedFingerprint, CheckedAt: time.Now().UTC(),
	}}}
	verified := service.ensureModelFreshWithContext(context.Background(), provider, provider.Models[0], owner)
	if verified.Status != "current" || !verified.Reused {
		t.Fatalf("persisted verified result = %#v, want recent success reuse", verified)
	}
	if _, ok := service.maintenanceCooldowns[key]; ok {
		t.Fatal("verified successful maintenance did not clear its owner/model cooldown")
	}
}

func TestLocalModelMaintenanceCooldownRegistryIsBoundedAndFailsClosed(t *testing.T) {
	service := &Service{}
	deadline := time.Now().UTC().Add(time.Minute)
	var writers sync.WaitGroup
	for index := 0; index <= maxLocalModelMaintenanceCooldowns; index++ {
		owner := fmt.Sprintf("owner-%d", index)
		writers.Add(1)
		go func() {
			defer writers.Done()
			ctx := withModelMaintenanceScope(context.Background(), owner, true)
			service.rememberLocalModelMaintenanceFailure(ctx, "provider", "model", "fingerprint", deadline)
		}()
	}
	writers.Wait()
	if len(service.maintenanceCooldowns) != maxLocalModelMaintenanceCooldowns {
		t.Fatalf("cooldown registry size = %d, want bounded size %d", len(service.maintenanceCooldowns), maxLocalModelMaintenanceCooldowns)
	}
	blocked := service.localModelMaintenanceCooldown(
		withModelMaintenanceScope(context.Background(), "untracked-owner", true),
		Provider{ID: "provider", Local: true}, Model{ID: "model"}, "fingerprint", time.Now().UTC(),
	)
	if blocked == nil || !blocked.BlocksExecution || blocked.NextCheckDueAt == nil {
		t.Fatalf("cooldown registry overflow result = %#v, want bounded fail-closed retry block", blocked)
	}
}

func TestCloudMaintenanceRequiresDistributedLeaseBeforeCatalogProbe(t *testing.T) {
	t.Setenv("LLM_MODEL_MAINTENANCE_ENABLED", "true")
	t.Setenv("LLM_CLOUD_MAINTENANCE_KEY", "read-only-key")
	var providerCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		providerCalls.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]string{{"id": "cloud-model"}}})
	}))
	defer server.Close()

	provider := Provider{
		ID: "paid-cloud", Name: "Paid cloud", Enabled: true, Paid: true,
		EndpointURL: server.URL, APIKeyEnv: "LLM_CLOUD_MAINTENANCE_KEY",
		Models: []Model{{ID: "cloud-model", Enabled: true}},
	}
	history := &leasedModelMaintenanceRepository{
		fakeModelMaintenanceRepository: &fakeModelMaintenanceRepository{},
		acquired:                       false,
	}
	service := &Service{
		policy: Policy{
			PaidCallsAllowed: true, DailyPaidBudgetEUR: 10,
			RequireApprovalBeforePaidUsage: false, UsageAccountingStatus: "durable", Providers: []Provider{provider},
		},
		maintenanceHistory: history, maintenanceRunning: map[string]*sync.Mutex{},
	}

	run := service.RunDueModelMaintenance()
	if run.InProgress != 1 || run.Failed != 0 || len(run.Results) != 1 {
		t.Fatalf("cloud lease contention run = %#v; want one fail-closed in-progress result", run)
	}
	if result := run.Results[0]; result.Status != "in_progress" || !result.BlocksExecution {
		t.Fatalf("cloud lease contention result = %#v; want blocked in-progress status", result)
	}
	if got := providerCalls.Load(); got != 0 {
		t.Fatalf("cloud catalog was probed %d time(s) without the cross-process lease", got)
	}
	if len(history.records) != 0 {
		t.Fatalf("lease contention wrote misleading cloud history: %#v", history.records)
	}
}

func TestCloudMaintenanceReusesResultWrittenBeforeLeaseAcquisition(t *testing.T) {
	t.Setenv("LLM_MODEL_MAINTENANCE_ENABLED", "true")
	t.Setenv("LLM_CLOUD_MAINTENANCE_KEY", "read-only-key")
	var providerCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		providerCalls.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]string{{"id": "cloud-model"}}})
	}))
	defer server.Close()

	provider := Provider{
		ID: "paid-cloud", Name: "Paid cloud", Enabled: true, Paid: true,
		EndpointURL: server.URL, APIKeyEnv: "LLM_CLOUD_MAINTENANCE_KEY",
		Models: []Model{{ID: "cloud-model", Enabled: true}},
	}
	policy := Policy{
		PaidCallsAllowed: true, DailyPaidBudgetEUR: 10,
		RequireApprovalBeforePaidUsage: false, Providers: []Provider{provider},
	}
	history := &completedDuringModelMaintenanceLeaseRepository{
		fakeModelMaintenanceRepository: &fakeModelMaintenanceRepository{},
		record: models.LLMModelMaintenance{
			ProviderID: provider.ID, ModelID: provider.Models[0].ID, Status: "provider_managed",
			ConfigurationFingerprint: modelMaintenanceFingerprint(provider, provider.Models[0], policy),
			CheckedAt:                time.Now().UTC(),
		},
	}
	service := &Service{policy: policy, maintenanceHistory: history, maintenanceRunning: map[string]*sync.Mutex{}}

	run := service.RunDueModelMaintenance()
	if len(run.Results) != 1 || run.Results[0].Status != "provider_managed" || !run.Results[0].Reused {
		t.Fatalf("cloud maintenance after lease acquisition = %#v; want reuse of the result that appeared under the lease", run)
	}
	if got := providerCalls.Load(); got != 0 {
		t.Fatalf("cloud catalog was probed %d time(s) despite a fresh result found under the lease", got)
	}
	if history.releases.Load() != 1 {
		t.Fatalf("distributed lease releases = %d; want one", history.releases.Load())
	}
}

func TestCloudMaintenanceHoldsDistributedLeaseThroughCatalogProbe(t *testing.T) {
	t.Setenv("LLM_MODEL_MAINTENANCE_ENABLED", "true")
	t.Setenv("LLM_CLOUD_MAINTENANCE_KEY", "read-only-key")
	var providerCalls atomic.Int32
	history := &observedModelMaintenanceLeaseRepository{fakeModelMaintenanceRepository: &fakeModelMaintenanceRepository{}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !history.leaseHeld.Load() {
			t.Error("cloud catalog probe ran without holding the distributed maintenance lease")
		}
		providerCalls.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]string{{"id": "cloud-model"}}})
	}))
	defer server.Close()

	provider := Provider{
		ID: "paid-cloud", Name: "Paid cloud", Enabled: true, Paid: true,
		EndpointURL: server.URL, APIKeyEnv: "LLM_CLOUD_MAINTENANCE_KEY",
		Models: []Model{{ID: "cloud-model", Enabled: true}},
	}
	service := &Service{
		policy: Policy{
			PaidCallsAllowed: true, DailyPaidBudgetEUR: 10,
			RequireApprovalBeforePaidUsage: false, UsageAccountingStatus: "durable", Providers: []Provider{provider},
		},
		maintenanceHistory: history, maintenanceRunning: map[string]*sync.Mutex{},
	}

	run := service.RunDueModelMaintenance()
	if len(run.Results) != 1 || run.Results[0].Status != "provider_managed" || run.Results[0].Reused {
		t.Fatalf("cloud maintenance under lease = %#v; want one fresh provider-managed result", run)
	}
	if got := providerCalls.Load(); got != 1 {
		t.Fatalf("cloud catalog probes = %d; want one while the lease is held", got)
	}
	if history.leaseHeld.Load() || history.releases.Load() != 1 {
		t.Fatalf("distributed lease held=%t releases=%d after run; want released exactly once", history.leaseHeld.Load(), history.releases.Load())
	}
}

func TestCloudModelMaintenanceChecksCatalogAndRepeatsDailyWithoutUpdatingModel(t *testing.T) {
	t.Setenv("LLM_MODEL_MAINTENANCE_ENABLED", "true")
	t.Setenv("LLM_MODEL_MAINTENANCE_INTERVAL_HOURS", "24")
	t.Setenv("LLM_CLOUD_MAINTENANCE_KEY", "read-only-key")
	var providerCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/models" || r.ContentLength != 0 {
			t.Errorf("provider request = %s %s content-length=%d; want bodyless GET /v1/models", r.Method, r.URL.Path, r.ContentLength)
			http.Error(w, "unexpected request", http.StatusMethodNotAllowed)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer read-only-key" {
			t.Errorf("provider authorization = %q, want configured bearer credential", got)
		}
		providerCalls.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]string{{"id": "cloud-model"}}})
	}))
	defer server.Close()

	provider := Provider{
		ID: "paid-cloud", Name: "Paid cloud", Enabled: true, Paid: true, EndpointURL: server.URL, APIKeyEnv: "LLM_CLOUD_MAINTENANCE_KEY",
		Models: []Model{{ID: "cloud-model", Name: "Cloud model", Enabled: true}},
	}
	history := &leasedModelMaintenanceRepository{
		fakeModelMaintenanceRepository: &fakeModelMaintenanceRepository{},
		acquired:                       true,
	}
	service := &Service{
		policy: Policy{
			PaidCallsAllowed: true, DailyPaidBudgetEUR: 10, RequireApprovalBeforePaidUsage: false,
			UsageAccountingStatus: "durable",
			Providers:             []Provider{provider},
		},
		maintenanceHistory: history,
		maintenanceRunning: map[string]*sync.Mutex{},
	}

	first := service.RunDueModelMaintenance()
	if first.Eligible != 1 || first.Checked != 1 || first.ProviderManaged != 1 || first.Updated != 0 || len(first.Results) != 1 {
		t.Fatalf("cloud maintenance run = %#v, want one read-only catalog check and no update", first)
	}
	result := first.Results[0]
	if result.Status != "provider_managed" || result.BlocksExecution || result.UpdateAttempted || result.UpdateApplied {
		t.Fatalf("cloud maintenance result = %#v, want non-blocking provider-managed status without an update", result)
	}
	if result.NextCheckDueAt == nil || !result.NextCheckDueAt.Equal(result.CheckedAt.Add(24*time.Hour)) {
		t.Fatalf("provider-managed model next check = %v; want 24-hour catalog check deadline", result.NextCheckDueAt)
	}
	if got := providerCalls.Load(); got != 1 {
		t.Fatalf("cloud provider requests = %d; want one read-only catalog probe", got)
	}
	if !strings.Contains(strings.ToLower(result.Reason), "catalog reports the configured model id") || !strings.Contains(strings.ToLower(result.Reason), "cannot verify or update the hosted model version") {
		t.Fatalf("cloud maintenance reason = %q; want catalog evidence and explicit version limitation", result.Reason)
	}

	second := service.RunDueModelMaintenance()
	if len(second.Results) != 1 || !second.Results[0].Reused || second.Results[0].Status != "provider_managed" || second.ProviderManaged != 1 || second.Checked != 0 {
		t.Fatalf("repeated cloud maintenance run = %#v, want reuse of provider-managed status", second)
	}
	if got := providerCalls.Load(); got != 1 {
		t.Fatalf("cloud provider requests after cached run = %d; want one", got)
	}

	history.fakeModelMaintenanceRepository.mu.Lock()
	history.fakeModelMaintenanceRepository.records[0].CheckedAt = time.Now().UTC().Add(-modelMaintenanceInterval() - time.Second)
	history.fakeModelMaintenanceRepository.mu.Unlock()
	third := service.RunDueModelMaintenance()
	if third.Checked != 1 || third.ProviderManaged != 1 || third.Results[0].Reused || providerCalls.Load() != 2 {
		t.Fatalf("expired cloud check did not run again: run=%#v requests=%d", third, providerCalls.Load())
	}
	if third.Results[0].UpdateAttempted || third.Results[0].UpdateApplied {
		t.Fatalf("cloud maintenance claimed a model update: %#v", third.Results[0])
	}
}

func TestStartedModelMaintenanceSchedulerRunsReadOnlyCatalogCheck(t *testing.T) {
	t.Setenv("LLM_MODEL_MAINTENANCE_ENABLED", "true")
	t.Setenv("LLM_MODEL_MAINTENANCE_SCHEDULER_ENABLED", "true")
	t.Setenv("LLM_MODEL_MAINTENANCE_INTERVAL_HOURS", "24")
	t.Setenv("LLM_CLOUD_MAINTENANCE_KEY", "test-read-only-key")

	var providerCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/models" || r.ContentLength != 0 {
			t.Errorf("provider request = %s %s content-length=%d; want bodyless GET /v1/models", r.Method, r.URL.Path, r.ContentLength)
			http.Error(w, "unexpected request", http.StatusMethodNotAllowed)
			return
		}
		providerCalls.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]string{{"id": "cloud-model"}}})
	}))
	defer server.Close()

	provider := Provider{
		ID: "paid-cloud", Name: "Paid cloud", Enabled: true, Paid: true,
		EndpointURL: server.URL, APIKeyEnv: "LLM_CLOUD_MAINTENANCE_KEY",
		Models: []Model{{ID: "cloud-model", Name: "Cloud model", Enabled: true}},
	}
	history := &fakeModelMaintenanceRepository{}
	service := &Service{
		policy: Policy{
			PaidCallsAllowed: true, DailyPaidBudgetEUR: 10, RequireApprovalBeforePaidUsage: false,
			UsageAccountingStatus: "durable",
			Providers:             []Provider{provider},
		},
		maintenanceHistory: history,
		maintenanceRunning: map[string]*sync.Mutex{},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	StartModelMaintenanceScheduler(ctx, service, func() bool { return true })

	deadline := time.Now().Add(2 * time.Second)
	var record *models.LLMModelMaintenance
	for time.Now().Before(deadline) {
		var err error
		record, err = history.FindLatestModelMaintenanceWithContext(ctx, provider.ID, "cloud-model")
		if err != nil {
			t.Fatalf("read scheduler maintenance result: %v", err)
		}
		if record != nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if record == nil {
		t.Fatal("starting the maintenance scheduler did not run the startup catalog check")
	}
	if record.Status != "provider_managed" || record.BlocksExecution || record.UpdateAttempted || record.UpdateApplied {
		t.Fatalf("scheduled catalog result = %#v; want non-blocking catalog evidence and no hosted-model upgrade", record)
	}
	result := modelMaintenanceResult(*record)
	if result.NextCheckDueAt == nil || !result.NextCheckDueAt.Equal(record.CheckedAt.Add(24*time.Hour)) {
		t.Fatalf("scheduled next check = %v; want 24 hours after catalog check", result.NextCheckDueAt)
	}
	if got := providerCalls.Load(); got != 1 {
		t.Fatalf("local test catalog requests = %d; want exactly one", got)
	}
}

func TestCloudCatalogOmissionDoesNotClaimConfiguredModelIsUnavailable(t *testing.T) {
	for _, test := range []struct {
		name    string
		payload map[string]any
		want    string
	}{
		{name: "catalog lists a different model", payload: map[string]any{"data": []map[string]string{{"id": "other-model"}}}, want: "does not infer that it is unavailable"},
		{name: "catalog omits machine-readable IDs", payload: map[string]any{"data": []any{}}, want: "endpoint did not expose machine-readable model ids"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("LLM_MODEL_MAINTENANCE_ENABLED", "true")
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != "/v1/models" || r.ContentLength != 0 {
					t.Errorf("provider request = %s %s content-length=%d; want bodyless GET /v1/models", r.Method, r.URL.Path, r.ContentLength)
					http.Error(w, "unexpected request", http.StatusMethodNotAllowed)
					return
				}
				_ = json.NewEncoder(w).Encode(test.payload)
			}))
			defer server.Close()

			provider := Provider{ID: "cloud", Name: "Cloud", Enabled: true, EndpointURL: server.URL, QuotaRemaining: 1, Models: []Model{{ID: "configured-model", Enabled: true}}}
			service := &Service{
				policy:             Policy{FreeCloudQuotaAllowed: true, Providers: []Provider{provider}},
				maintenanceHistory: &fakeModelMaintenanceRepository{},
				maintenanceRunning: map[string]*sync.Mutex{},
			}
			run := service.RunDueModelMaintenance()
			if run.Checked != 1 || run.Failed != 0 || run.ProviderManaged != 1 || len(run.Results) != 1 {
				t.Fatalf("catalog omission run = %#v", run)
			}
			result := run.Results[0]
			if result.BlocksExecution || result.UpdateAttempted || result.UpdateApplied || !strings.Contains(strings.ToLower(result.Reason), test.want) {
				t.Fatalf("catalog omission result = %#v; want non-blocking evidence %q", result, test.want)
			}
		})
	}
}

func TestOdysseusMaintenanceReportsHealthOnly(t *testing.T) {
	t.Setenv("LLM_MODEL_MAINTENANCE_ENABLED", "true")
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/health" {
			t.Errorf("Odysseus request = %s %s; want GET /api/health", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		requests.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "healthy"})
	}))
	defer server.Close()

	provider := Provider{
		ID: "odysseus", Name: "Odysseus", Enabled: true, EndpointURL: server.URL, QuotaRemaining: 1,
		Models: []Model{{ID: "odysseus-agent", Name: "Odysseus agent", Enabled: true}},
	}
	service := &Service{
		policy:             Policy{FreeCloudQuotaAllowed: true, Providers: []Provider{provider}},
		maintenanceHistory: &fakeModelMaintenanceRepository{},
		maintenanceRunning: map[string]*sync.Mutex{},
	}
	run := service.RunDueModelMaintenance()
	if run.Checked != 1 || run.ProviderManaged != 0 || run.HealthOnly != 1 || run.Failed != 0 || len(run.Results) != 1 || requests.Load() != 1 {
		t.Fatalf("Odysseus maintenance = %#v; requests=%d", run, requests.Load())
	}
	result := run.Results[0]
	if result.Status != "provider_managed" || result.BlocksExecution || !strings.Contains(strings.ToLower(result.Reason), "health endpoint") || !strings.Contains(strings.ToLower(result.Reason), "not model availability") || strings.Contains(strings.ToLower(result.Reason), "catalog") {
		t.Fatalf("Odysseus maintenance result overstates health evidence: %#v", result)
	}
}

func TestModelMaintenanceLatestHistoryReadUsesCancellableRepositoryMethod(t *testing.T) {
	t.Setenv("LLM_MODEL_MAINTENANCE_ENABLED", "true")
	policy := testPolicyWithoutEndpoints()
	policy.Providers[0].EndpointURL = "http://127.0.0.1:11434"
	history := &blockingContextMaintenanceHistoryRepository{
		fakeModelMaintenanceRepository: &fakeModelMaintenanceRepository{},
		started:                        make(chan string, 1),
	}
	service := &Service{policy: policy, maintenanceHistory: history, maintenanceRunning: map[string]*sync.Mutex{}}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan ModelMaintenanceResult, 1)
	go func() {
		done <- service.ensureModelFreshWithContext(ctx, policy.Providers[0], policy.Providers[0].Models[0])
	}()

	select {
	case operation := <-history.started:
		if operation != "latest" {
			t.Fatalf("history operation = %q, want latest", operation)
		}
		cancel()
	case <-time.After(time.Second):
		t.Fatal("maintenance did not start a context-aware history read")
	}
	select {
	case result := <-done:
		if result.Status != "cancelled" {
			t.Fatalf("maintenance result = %#v, want cancelled", result)
		}
	case <-time.After(time.Second):
		t.Fatal("maintenance history read did not stop when context was cancelled")
	}
}

func TestModelMaintenanceRecentHistoryReadUsesCancellableRepositoryMethod(t *testing.T) {
	history := &blockingContextMaintenanceHistoryRepository{
		fakeModelMaintenanceRepository: &fakeModelMaintenanceRepository{},
		started:                        make(chan string, 1),
	}
	service := &Service{maintenanceHistory: history}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := service.ModelMaintenanceHistoryWithContext(ctx, 10)
		done <- err
	}()

	select {
	case operation := <-history.started:
		if operation != "recent" {
			t.Fatalf("history operation = %q, want recent", operation)
		}
		cancel()
	case <-time.After(time.Second):
		t.Fatal("history endpoint did not start a context-aware read")
	}
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("history read error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("recent history read did not stop when context was cancelled")
	}
}

func TestRunDueModelMaintenanceCancelsLocalOllamaPullWaitAndBlocksUnverifiedUse(t *testing.T) {
	t.Setenv("LLM_MODEL_MAINTENANCE_ENABLED", "true")
	pullStarted := make(chan struct{}, 1)
	pullRelease := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(pullRelease) }) }
	defer release()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			_ = json.NewEncoder(w).Encode(map[string]any{"models": []map[string]string{{"name": "phi3:mini", "digest": "sha256:old"}}})
		case "/api/pull":
			pullStarted <- struct{}{}
			<-pullRelease
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	policy := testPolicyWithoutEndpoints()
	policy.LocalModelsAllowed = true
	index := providerIndex(t, policy, "ollama")
	policy.Providers[index].Enabled = true
	policy.Providers[index].EndpointURL = server.URL
	policy.Providers[index].Models = []Model{{ID: "phi3:mini", Name: "Phi", Tier: TierLocal, Enabled: true}}
	history := &fakeModelMaintenanceRepository{}
	service := withTrustedTestFinalEffects(t, &Service{policy: policy, maintenanceHistory: history, maintenanceRunning: map[string]*sync.Mutex{}})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan ModelMaintenanceRun, 1)
	go func() { done <- service.RunDueModelMaintenanceWithContext(ctx) }()

	select {
	case <-pullStarted:
		cancel()
	case <-time.After(3 * time.Second):
		t.Fatal("maintenance never reached the Ollama pull")
	}
	select {
	case run := <-done:
		release()
		if !run.Cancelled || run.Failed != 1 || len(run.Results) != 1 {
			t.Fatalf("cancelled pull run = %#v", run)
		}
		if run.Results[0].Status != "failed" || !run.Results[0].BlocksExecution || !run.Results[0].UpdateAttempted {
			t.Fatalf("interrupted update was not recorded as blocked and uncertain: %#v", run.Results[0])
		}
		if len(history.records) != 1 || history.records[0].Status != "failed" || !history.records[0].BlocksExecution {
			t.Fatalf("interrupted update history = %#v", history.records)
		}
	case <-time.After(3 * time.Second):
		release()
		t.Fatal("maintenance did not stop waiting after its local pull request was cancelled")
	}
}

func TestFailedOllamaRefreshRecordsObservedPostFailureDigest(t *testing.T) {
	t.Setenv("LLM_MODEL_MAINTENANCE_ENABLED", "true")
	var tagsCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			digest := "sha256:old"
			if tagsCalls.Add(1) > 1 {
				digest = "sha256:new"
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"models": []map[string]string{{"name": "phi3:mini", "digest": digest}}})
		case "/api/pull":
			http.Error(w, "registry unavailable", http.StatusBadGateway)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	policy := testPolicyWithoutEndpoints()
	policy.LocalModelsAllowed = true
	index := providerIndex(t, policy, "ollama")
	policy.Providers[index].Enabled = true
	policy.Providers[index].EndpointURL = server.URL
	policy.Providers[index].Models = []Model{{ID: "phi3:mini", Name: "Phi", Tier: TierLocal, Enabled: true}}
	history := &fakeModelMaintenanceRepository{}
	service := withTrustedTestFinalEffects(t, &Service{policy: policy, maintenanceHistory: history, maintenanceRunning: map[string]*sync.Mutex{}})

	run := service.RunDueModelMaintenance()
	if run.Failed != 1 || len(run.Results) != 1 {
		t.Fatalf("failed refresh run = %#v, want one blocked failure", run)
	}
	result := run.Results[0]
	if result.Status != "failed" || !result.BlocksExecution || !result.UpdateAttempted || result.UpdateApplied {
		t.Fatalf("partial refresh result = %#v, want failed, attempted, unapplied, and blocked", result)
	}
	if result.PreviousDigest != "sha256:old" || result.CurrentDigest != "sha256:new" {
		t.Fatalf("partial refresh digests = %q -> %q, want sha256:old -> sha256:new", result.PreviousDigest, result.CurrentDigest)
	}
	if tagsCalls.Load() != 2 {
		t.Fatalf("Ollama tag inspections = %d, want a post-failure inspection", tagsCalls.Load())
	}
	if len(history.records) != 1 || history.records[0].CurrentDigest != "sha256:new" || !history.records[0].BlocksExecution {
		t.Fatalf("durable failed refresh = %#v, want observed digest persisted while execution stays blocked", history.records)
	}
}

func TestScheduledModelMaintenanceCancelsRunWhenEmergencyStopActivates(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var backgroundAllowed atomic.Bool
	backgroundAllowed.Store(true)
	started := make(chan struct{})
	completed := make(chan struct{})
	go func() {
		runScheduledModelMaintenance(ctx, func() bool { return backgroundAllowed.Load() }, func(runCtx context.Context) ModelMaintenanceRun {
			close(started)
			<-runCtx.Done()
			return ModelMaintenanceRun{Cancelled: true}
		})
		close(completed)
	}()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("scheduled maintenance did not start")
	}
	backgroundAllowed.Store(false)
	select {
	case <-completed:
	case <-time.After(time.Second):
		t.Fatal("emergency stop did not cancel the scheduled run")
	}
}

func TestScheduledModelMaintenancePropagatesParentCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	completed := make(chan struct{})
	go func() {
		runScheduledModelMaintenance(ctx, func() bool { return true }, func(runCtx context.Context) ModelMaintenanceRun {
			close(started)
			<-runCtx.Done()
			return ModelMaintenanceRun{Cancelled: true}
		})
		close(completed)
	}()

	select {
	case <-started:
	case <-time.After(time.Second):
		cancel()
		t.Fatal("scheduled maintenance did not start")
	}
	cancel()
	select {
	case <-completed:
	case <-time.After(time.Second):
		t.Fatal("parent cancellation did not stop the scheduled run")
	}
}

func TestModelMaintenanceSchedulerDefaultsAndExplicitDisable(t *testing.T) {
	t.Setenv("LLM_MODEL_MAINTENANCE_SCHEDULER_ENABLED", "")
	if !modelMaintenanceSchedulerEnabled() {
		t.Fatal("scheduler should be enabled by default")
	}

	t.Setenv("LLM_MODEL_MAINTENANCE_SCHEDULER_ENABLED", "false")
	if modelMaintenanceSchedulerEnabled() {
		t.Fatal("scheduler should honor explicit disable")
	}
}

func TestModelMaintenanceSchedulerFailsClosedWithoutSafetyCallback(t *testing.T) {
	t.Setenv("LLM_MODEL_MAINTENANCE_SCHEDULER_ENABLED", "true")
	if modelMaintenanceBackgroundAllowed(nil) {
		t.Fatal("missing background-safety callback was treated as authorization")
	}

	called := false
	run := runScheduledModelMaintenance(context.Background(), nil, func(context.Context) ModelMaintenanceRun {
		called = true
		return ModelMaintenanceRun{}
	})
	if !run.Cancelled || called {
		t.Fatalf("scheduled run without a safety callback = %#v, called=%t; want denied without execution", run, called)
	}

	service := &Service{}
	StartModelMaintenanceScheduler(context.Background(), service, nil)
	if _, started := activeModelMaintenanceSchedulers.Load(service); started {
		t.Fatal("exported scheduler registered a background loop without a safety callback")
	}
}

func TestModelMaintenanceSchedulerStartIsSingleFlightPerService(t *testing.T) {
	service := &Service{}
	started := make(chan struct{})
	release := make(chan struct{})
	var accepted atomic.Int32
	var runs atomic.Int32
	const callers = 16
	var group sync.WaitGroup
	group.Add(callers)
	for range callers {
		go func() {
			defer group.Done()
			if startModelMaintenanceScheduler(service, func() {
				runs.Add(1)
				close(started)
				<-release
			}) {
				accepted.Add(1)
			}
		}()
	}
	group.Wait()
	if got := accepted.Load(); got != 1 {
		t.Fatalf("concurrent scheduler starts accepted=%d; want exactly one", got)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("accepted scheduler did not start")
	}
	if got := runs.Load(); got != 1 {
		t.Fatalf("concurrent scheduler runs=%d; want one", got)
	}
	close(release)

	deadline := time.Now().Add(time.Second)
	for {
		restarted := make(chan struct{}, 1)
		if startModelMaintenanceScheduler(service, func() {
			runs.Add(1)
			restarted <- struct{}{}
		}) {
			select {
			case <-restarted:
			case <-time.After(time.Second):
				t.Fatal("scheduler did not restart after the prior run exited")
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("scheduler registration was not released after the prior run exited")
		}
		time.Sleep(time.Millisecond)
	}
	if got := runs.Load(); got != 2 {
		t.Fatalf("scheduler runs after restart=%d; want two sequential runs", got)
	}
}

func TestModelMaintenanceSchedulerStartupSweepConsumesPriorWake(t *testing.T) {
	wake := make(chan struct{}, 1)
	wake <- struct{}{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runs := 0
	runModelMaintenanceSchedulerWithWait(ctx, func() bool { return true }, func(context.Context) ModelMaintenanceRun {
		runs++
		return ModelMaintenanceRun{}
	}, wake, func(context.Context, time.Duration, <-chan struct{}) modelMaintenanceSchedulerEvent {
		select {
		case <-wake:
			t.Fatal("startup sweep left a pre-existing persisted-state wake queued")
		default:
		}
		return modelMaintenanceSchedulerCancelled
	}, time.Now)
	if runs != 1 {
		t.Fatalf("startup sweep count=%d; want one sweep covering the prior wake", runs)
	}
}

func TestModelMaintenanceSchedulerUsesDurableDueAndDailyFallback(t *testing.T) {
	t.Setenv("LLM_MODEL_MAINTENANCE_FAILURE_RETRY_MINUTES", "5")
	now := time.Date(2026, time.September, 25, 12, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name string
		due  *time.Time
		want time.Duration
	}{
		{name: "no durable results", want: 24 * time.Hour},
		{name: "daily due", due: timePointer(now.Add(24 * time.Hour)), want: 24 * time.Hour},
		{name: "nearest retry", due: timePointer(now.Add(5 * time.Minute)), want: 5 * time.Minute},
		{name: "already due", due: timePointer(now.Add(-time.Second)), want: 0},
		{name: "fallback caps distant due", due: timePointer(now.Add(72 * time.Hour)), want: 24 * time.Hour},
	} {
		t.Run(test.name, func(t *testing.T) {
			run := ModelMaintenanceRun{}
			if test.due != nil {
				run.Results = []ModelMaintenanceResult{{NextCheckDueAt: test.due}}
			}
			if got := modelMaintenanceSchedulerNextDelay(run, now); got != test.want {
				t.Fatalf("next delay = %s, want %s", got, test.want)
			}
		})
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runCount := 0
	var waitFor time.Duration
	runModelMaintenanceSchedulerWithWait(ctx, func() bool { return true }, func(context.Context) ModelMaintenanceRun {
		runCount++
		return ModelMaintenanceRun{}
	}, nil, func(context.Context, time.Duration, <-chan struct{}) modelMaintenanceSchedulerEvent {
		waitFor = modelMaintenanceSchedulerMaximumFallback
		return modelMaintenanceSchedulerCancelled
	}, func() time.Time { return now })
	if runCount != 1 || waitFor != 24*time.Hour {
		t.Fatalf("idle scheduler runs=%d wait=%s, want one startup sweep then a 24h fallback", runCount, waitFor)
	}
}

func TestRequestPersistedFailureWakesSchedulerAndSchedulesExactRetry(t *testing.T) {
	t.Setenv("LLM_MODEL_MAINTENANCE_FAILURE_RETRY_MINUTES", "5")
	history := &fakeModelMaintenanceRepository{}
	service := &Service{maintenanceHistory: history, maintenanceRunning: map[string]*sync.Mutex{}}
	wake := service.modelMaintenanceWakeChannel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	clock := time.Now().UTC()
	due := time.Time{}
	runCount := 0
	waitCount := 0
	runModelMaintenanceSchedulerWithWait(ctx, func() bool { return true }, func(context.Context) ModelMaintenanceRun {
		runCount++
		switch runCount {
		case 1:
			return ModelMaintenanceRun{Results: []ModelMaintenanceResult{{Status: "current", NextCheckDueAt: timePointer(clock.Add(24 * time.Hour))}}}
		case 2:
			record := history.records[len(history.records)-1]
			maintenance := modelMaintenanceResult(record)
			if maintenance.Status != "failed" || maintenance.NextCheckDueAt == nil {
				t.Fatalf("wake sweep did not observe the persisted failure: %#v", maintenance)
			}
			due = *maintenance.NextCheckDueAt
			if !clock.Equal(record.CheckedAt) {
				t.Fatalf("fake clock=%s, persisted failure time=%s", clock, record.CheckedAt)
			}
			return ModelMaintenanceRun{Results: []ModelMaintenanceResult{maintenance}}
		default:
			if !clock.Equal(due) {
				t.Fatalf("retry run at %s, want exact durable due time %s", clock, due)
			}
			cancel()
			return ModelMaintenanceRun{Cancelled: true}
		}
	}, wake, func(_ context.Context, delay time.Duration, wake <-chan struct{}) modelMaintenanceSchedulerEvent {
		waitCount++
		switch waitCount {
		case 1:
			if delay != 24*time.Hour {
				t.Fatalf("initial idle delay=%s, want 24h", delay)
			}
			requestFailure := service.recordMaintenance(ModelMaintenanceResult{
				ProviderID: "ollama", ModelID: "test-model", Status: "failed", BlocksExecution: true,
			})
			if requestFailure.NextCheckDueAt == nil {
				t.Fatal("persisted request failure has no durable retry deadline")
			}
			clock = requestFailure.CheckedAt
			select {
			case <-wake:
				return modelMaintenanceSchedulerWake
			default:
				t.Fatal("successful persistence did not queue a scheduler wake")
				return modelMaintenanceSchedulerCancelled
			}
		case 2:
			if delay != 5*time.Minute {
				t.Fatalf("retry delay=%s, want full five-minute durable cooldown", delay)
			}
			clock = clock.Add(delay)
			return modelMaintenanceSchedulerDue
		default:
			t.Fatalf("unexpected scheduler wait %d", waitCount)
			return modelMaintenanceSchedulerCancelled
		}
	}, func() time.Time { return clock })
	if runCount != 3 || waitCount != 2 || !clock.Equal(due) {
		t.Fatalf("runs=%d waits=%d clock=%s due=%s, want wake sweep then exact retry", runCount, waitCount, clock, due)
	}
}

func TestModelMaintenancePersistenceFailureDoesNotSignalScheduler(t *testing.T) {
	history := &failingModelMaintenanceRepository{
		fakeModelMaintenanceRepository: &fakeModelMaintenanceRepository{},
		err:                            errors.New("test persistence failure"),
	}
	service := &Service{maintenanceHistory: history, maintenanceRunning: map[string]*sync.Mutex{}}
	wake := service.modelMaintenanceWakeChannel()
	result := service.recordMaintenance(ModelMaintenanceResult{
		ProviderID: "ollama", ModelID: "test-model", Status: "failed", BlocksExecution: true,
	})
	if result.Status != "failed" || !result.BlocksExecution || result.NextCheckDueAt == nil {
		t.Fatalf("unpersisted maintenance result = %#v, want blocked failure", result)
	}
	if delay := modelMaintenanceSchedulerNextDelay(ModelMaintenanceRun{Results: []ModelMaintenanceResult{result}}, time.Now().UTC()); delay > modelMaintenanceFailureRetryInterval() || delay <= 0 {
		t.Fatalf("scheduler delay after failed persistence = %s, want short positive retry no longer than %s", delay, modelMaintenanceFailureRetryInterval())
	}
	select {
	case <-wake:
		t.Fatal("scheduler was signaled even though maintenance persistence failed")
	default:
	}
}

func TestModelMaintenanceRepositoryWithoutContextSupportFailsClosed(t *testing.T) {
	inner := &fakeModelMaintenanceRepository{}
	repository := legacyOnlyMaintenanceRepository{inner: inner}
	if _, err := findLatestModelMaintenance(context.Background(), repository, "localai", "model"); err == nil || !strings.Contains(err.Error(), "cancellable") {
		t.Fatalf("non-contextual history read error = %v, want explicit fail-closed error", err)
	}
	service := &Service{maintenanceHistory: repository, maintenanceRunning: map[string]*sync.Mutex{}}
	result := service.recordMaintenance(ModelMaintenanceResult{ProviderID: "localai", ModelID: "model", Status: "current"})
	if result.Status != "failed" || !result.BlocksExecution || result.NextCheckDueAt == nil {
		t.Fatalf("non-contextual history write result = %#v, want blocked retryable failure", result)
	}
	if len(inner.records) != 0 {
		t.Fatalf("maintenance fell back to a contextless write: %#v", inner.records)
	}
}

func TestModelMaintenanceSchedulerPausesAndResumesAfterEmergencyStop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var backgroundAllowed atomic.Bool
	backgroundAllowed.Store(true)
	clock := time.Now().UTC()
	runCount := 0
	waitCount := 0
	runModelMaintenanceSchedulerWithWait(ctx, func() bool { return backgroundAllowed.Load() }, func(context.Context) ModelMaintenanceRun {
		runCount++
		if runCount == 1 {
			return ModelMaintenanceRun{Results: []ModelMaintenanceResult{{NextCheckDueAt: timePointer(clock.Add(24 * time.Hour))}}}
		}
		cancel()
		return ModelMaintenanceRun{Cancelled: true}
	}, nil, func(_ context.Context, delay time.Duration, _ <-chan struct{}) modelMaintenanceSchedulerEvent {
		waitCount++
		switch waitCount {
		case 1:
			if delay != 24*time.Hour {
				t.Fatalf("active idle delay=%s, want 24h", delay)
			}
			backgroundAllowed.Store(false)
			return modelMaintenanceSchedulerPermissionPoll
		case 2:
			if delay != modelMaintenanceSchedulerPermissionPollInterval || runCount != 1 {
				t.Fatalf("paused delay=%s runs=%d, want 60s and no database/provider run", delay, runCount)
			}
			backgroundAllowed.Store(true)
			return modelMaintenanceSchedulerPermissionPoll
		default:
			t.Fatalf("unexpected scheduler wait %d", waitCount)
			return modelMaintenanceSchedulerCancelled
		}
	}, func() time.Time { return clock })
	if runCount != 2 || waitCount != 2 {
		t.Fatalf("runs=%d waits=%d, want startup sweep, paused poll, and one resumed sweep", runCount, waitCount)
	}
}

func TestModelMaintenanceSchedulerStopsOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	runCount := 0
	runModelMaintenanceSchedulerWithWait(ctx, func() bool { return true }, func(context.Context) ModelMaintenanceRun {
		runCount++
		return ModelMaintenanceRun{}
	}, nil, func(ctx context.Context, _ time.Duration, _ <-chan struct{}) modelMaintenanceSchedulerEvent {
		cancel()
		<-ctx.Done()
		return modelMaintenanceSchedulerCancelled
	}, time.Now)
	if runCount != 1 {
		t.Fatalf("scheduler runs=%d after cancellation, want only startup run", runCount)
	}
}

func TestModelMaintenanceSchedulerWaitKeepsBufferedWakeAndHonorsCancellation(t *testing.T) {
	t.Run("buffered wake", func(t *testing.T) {
		ctx := context.Background()
		wake := make(chan struct{}, 1)
		wake <- struct{}{}
		if event := waitForModelMaintenanceScheduler(ctx, 24*time.Hour, wake); event != modelMaintenanceSchedulerWake {
			t.Fatalf("wait event=%v, want buffered wake", event)
		}
	})

	t.Run("cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if event := waitForModelMaintenanceScheduler(ctx, 24*time.Hour, nil); event != modelMaintenanceSchedulerCancelled {
			t.Fatalf("wait event=%v, want cancellation", event)
		}
	})
}

func timePointer(value time.Time) *time.Time { return &value }

func TestModelMaintenanceIntervalPreventsPerRequestRefreshLoops(t *testing.T) {
	t.Setenv("LLM_MODEL_MAINTENANCE_INTERVAL_HOURS", "")
	if interval := modelMaintenanceInterval(); interval != 24*time.Hour {
		t.Fatalf("default interval = %s, want 24h", interval)
	}

	for _, value := range []string{"0", "-5", "1", "12", "9999"} {
		t.Setenv("LLM_MODEL_MAINTENANCE_INTERVAL_HOURS", value)
		if interval := modelMaintenanceInterval(); interval != 24*time.Hour {
			t.Fatalf("interval for %q = %s, want fixed 24h", value, interval)
		}
	}
}

func TestModelMaintenanceTimeoutIsBounded(t *testing.T) {
	t.Setenv("LLM_MODEL_MAINTENANCE_TIMEOUT_SECONDS", "")
	if timeout := modelMaintenanceTimeout(); timeout != 15*time.Minute {
		t.Fatalf("default timeout = %s, want 15m", timeout)
	}

	t.Setenv("LLM_MODEL_MAINTENANCE_TIMEOUT_SECONDS", "0")
	if timeout := modelMaintenanceTimeout(); timeout != 30*time.Second {
		t.Fatalf("zero timeout = %s, want 30s minimum", timeout)
	}

	t.Setenv("LLM_MODEL_MAINTENANCE_TIMEOUT_SECONDS", "99999")
	if timeout := modelMaintenanceTimeout(); timeout != time.Hour {
		t.Fatalf("large timeout = %s, want 1h maximum", timeout)
	}
}

func TestModelMaintenanceResultReportsTheNextDailyCheck(t *testing.T) {
	t.Setenv("LLM_MODEL_MAINTENANCE_INTERVAL_HOURS", "9999")
	checkedAt := time.Date(2026, time.July, 21, 9, 0, 0, 0, time.UTC)
	result := modelMaintenanceResult(models.LLMModelMaintenance{
		ProviderID:   "ollama",
		ProviderName: "Ollama",
		ModelID:      "qwen2.5:7b",
		ModelName:    "Qwen",
		Status:       "current",
		CheckedAt:    checkedAt,
	})
	if result.NextCheckDueAt == nil || !result.NextCheckDueAt.Equal(checkedAt.Add(24*time.Hour)) {
		t.Fatalf("next check due = %v, want %v", result.NextCheckDueAt, checkedAt.Add(24*time.Hour))
	}
}

func TestOperatorManagedModelRemainsBlockedUntilItsNextDailyCheck(t *testing.T) {
	checkedAt := time.Date(2026, time.July, 21, 9, 0, 0, 0, time.UTC)
	provider := Provider{ID: "localai", Local: true}
	record := models.LLMModelMaintenance{
		ProviderID: "localai", ModelID: "configured-model", Status: "operator_managed",
		BlocksExecution: true, CheckedAt: checkedAt,
	}
	result := modelMaintenanceResult(record)
	if !result.BlocksExecution || result.NextCheckDueAt == nil || !result.NextCheckDueAt.Equal(checkedAt.Add(24*time.Hour)) {
		t.Fatalf("unsupported local maintenance result = %#v, want blocked through its next 24-hour check", result)
	}
	if !maintenanceRecordReusableForProvider(provider, record, checkedAt.Add(24*time.Hour-time.Nanosecond), 24*time.Hour) {
		t.Fatal("unsupported local result was not retained for the daily interval")
	}
	if maintenanceRecordReusableForProvider(provider, record, checkedAt.Add(24*time.Hour), 24*time.Hour) {
		t.Fatal("unsupported local result remained reusable at the 24-hour boundary")
	}
}

func TestUnsupportedLocalRuntimeDoesNotReuseVersionSuccessWithoutEvidence(t *testing.T) {
	t.Setenv("LLM_MODEL_MAINTENANCE_ENABLED", "true")
	var probes atomic.Int32
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/models" {
			t.Errorf("runtime request = %s %s; want read-only GET /v1/models", r.Method, r.URL.Path)
			http.Error(w, "unexpected request", http.StatusMethodNotAllowed)
			return
		}
		probes.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]string{{"id": "configured-model"}}})
	}))
	defer runtime.Close()

	provider := Provider{
		ID: "localai", Name: "LocalAI", Enabled: true, Local: true, EndpointURL: runtime.URL,
		Models: []Model{{ID: "configured-model", Name: "Configured model", Enabled: true}},
	}
	model := provider.Models[0]
	policy := Policy{LocalModelsAllowed: true, Providers: []Provider{provider}}
	fingerprint := modelMaintenanceFingerprint(provider, model, policy)
	history := &fakeModelMaintenanceRepository{records: []models.LLMModelMaintenance{{
		ProviderID: provider.ID, ModelID: model.ID, Status: "current", CheckedAt: time.Now().UTC(),
		ConfigurationFingerprint: fingerprint,
	}}}
	service := &Service{policy: policy, maintenanceHistory: history, maintenanceRunning: map[string]*sync.Mutex{}}

	blockReason, err := service.modelMaintenanceRoutingBlockReasonWithContext(context.Background(), provider, model)
	if err != nil {
		t.Fatalf("routing maintenance check: %v", err)
	}
	if blockReason == "" {
		t.Fatal("routing treated a non-Ollama local success record as version evidence")
	}

	result := service.ensureModelFresh(provider, model)
	if result.Status != "operator_managed" || !result.BlocksExecution || result.Reused {
		t.Fatalf("maintenance result = %#v; want freshly checked, blocked operator-managed result", result)
	}
	if probes.Load() != 1 {
		t.Fatalf("local runtime probes = %d; want exactly one daily verification probe", probes.Load())
	}
	if len(history.records) != 2 || history.records[1].Status != "operator_managed" || !history.records[1].BlocksExecution {
		t.Fatalf("maintenance records = %#v; want the stale success replaced by a blocked result", history.records)
	}
	if result.NextCheckDueAt == nil || !result.NextCheckDueAt.Equal(result.CheckedAt.Add(24*time.Hour)) {
		t.Fatalf("unsupported runtime next check = %v; want 24 hours after the blocked check", result.NextCheckDueAt)
	}
}

func TestProviderAwareMaintenanceCachePreservesOllamaAndRejectsLocalVersionClaims(t *testing.T) {
	now := time.Date(2026, time.July, 21, 9, 0, 0, 0, time.UTC)
	record := models.LLMModelMaintenance{Status: "updated", CheckedAt: now.Add(-time.Hour)}
	for _, test := range []struct {
		name      string
		provider  Provider
		wantReuse bool
	}{
		{name: "supported Ollama refresh", provider: Provider{ID: "ollama", Local: true}, wantReuse: true},
		{name: "unsupported LocalAI runtime", provider: Provider{ID: "localai", Local: true}, wantReuse: false},
		{name: "cloud catalog path", provider: Provider{ID: "cloud"}, wantReuse: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := maintenanceRecordReusableForProvider(test.provider, record, now, 24*time.Hour); got != test.wantReuse {
				t.Fatalf("reusable = %t; want %t", got, test.wantReuse)
			}
		})
	}
}

func TestProviderManagedModelAdvertisesDailyCatalogCheck(t *testing.T) {
	t.Setenv("LLM_MODEL_MAINTENANCE_INTERVAL_HOURS", "24")
	checkedAt := time.Date(2026, time.July, 21, 9, 0, 0, 0, time.UTC)
	record := models.LLMModelMaintenance{
		ProviderID: "cloud", ModelID: "provider-model", Status: "provider_managed", CheckedAt: checkedAt,
	}
	result := modelMaintenanceResult(record)
	if result.NextCheckDueAt == nil || !result.NextCheckDueAt.Equal(checkedAt.Add(24*time.Hour)) {
		t.Fatalf("provider-managed result next check = %v; want daily catalog check", result.NextCheckDueAt)
	}
	if !maintenanceRecordReusable(record, checkedAt.Add(24*time.Hour-time.Nanosecond), 24*time.Hour) {
		t.Fatal("provider-managed result should be reused until the daily boundary")
	}
	if maintenanceRecordReusable(record, checkedAt.Add(24*time.Hour), 24*time.Hour) {
		t.Fatal("provider-managed result remained reusable at the daily check boundary")
	}
}

func TestCloudMaintenanceFingerprintChangesWithCredentialsAndPaidGates(t *testing.T) {
	t.Setenv("LLM_CLOUD_MAINTENANCE_KEY", "first-secret")
	provider := Provider{ID: "cloud", EndpointURL: "https://api.example.test", APIKeyEnv: "LLM_CLOUD_MAINTENANCE_KEY", Paid: true}
	model := Model{ID: "provider-model"}
	policy := Policy{PaidCallsAllowed: false, DailyPaidBudgetEUR: 0, RequireApprovalBeforePaidUsage: true}
	baseline := modelMaintenanceFingerprint(provider, model, policy)
	if baseline == "" || strings.Contains(baseline, "first-secret") {
		t.Fatalf("baseline fingerprint is empty or exposes credential: %q", baseline)
	}
	cases := []struct {
		name   string
		change func()
	}{
		{name: "credential rotation", change: func() { t.Setenv("LLM_CLOUD_MAINTENANCE_KEY", "rotated-secret") }},
		{name: "paid usage approval", change: func() { policy.PaidCallsAllowed = true }},
		{name: "paid budget enabled", change: func() { policy.DailyPaidBudgetEUR = 10 }},
		{name: "approval requirement removed", change: func() { policy.RequireApprovalBeforePaidUsage = false }},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("LLM_CLOUD_MAINTENANCE_KEY", "first-secret")
			policy.PaidCallsAllowed = false
			policy.DailyPaidBudgetEUR = 0
			policy.RequireApprovalBeforePaidUsage = true
			test.change()
			got := modelMaintenanceFingerprint(provider, model, policy)
			if got == baseline {
				t.Fatal("provider credential or paid-gate change did not invalidate the catalog check")
			}
			if strings.Contains(got, "first-secret") || strings.Contains(got, "rotated-secret") {
				t.Fatalf("fingerprint exposed a provider credential: %q", got)
			}
		})
	}
}

func TestMaintenanceStillFreshRejectsFutureAndExpiredRecords(t *testing.T) {
	now := time.Date(2026, time.July, 21, 12, 0, 0, 0, time.UTC)
	interval := 24 * time.Hour
	if !maintenanceStillFresh(now.Add(-23*time.Hour), now, interval) {
		t.Fatal("recent maintenance record should be reusable")
	}
	if maintenanceStillFresh(now.Add(-24*time.Hour), now, interval) {
		t.Fatal("record at the daily boundary must be refreshed")
	}
	if maintenanceStillFresh(now.Add(time.Minute), now, interval) {
		t.Fatal("future-dated maintenance record must not suppress a real check")
	}
}

func TestModelMaintenanceFingerprintChangesWithTheCheckedRuntimeConfiguration(t *testing.T) {
	provider := Provider{ID: "ollama", EndpointURL: "http://host.docker.internal:11434", Local: true}
	model := Model{ID: "qwen2.5:7b"}
	baseline := modelMaintenanceFingerprint(provider, model, Policy{})
	if baseline == "" || len(baseline) != 64 {
		t.Fatalf("maintenance fingerprint = %q, want a SHA-256 hex digest", baseline)
	}
	if baseline == modelMaintenanceFingerprint(Provider{ID: "ollama", EndpointURL: "http://host.docker.internal:11435", Local: true}, model, Policy{}) {
		t.Fatal("endpoint change must invalidate a daily maintenance record")
	}
	if baseline == modelMaintenanceFingerprint(Provider{ID: "ollama", EndpointURL: "http://host.docker.internal:11434", Local: false}, model, Policy{}) {
		t.Fatal("provider locality change must invalidate a daily maintenance record")
	}
	if baseline == modelMaintenanceFingerprint(provider, Model{ID: "qwen2.5:14b"}, Policy{}) {
		t.Fatal("model change must invalidate a daily maintenance record")
	}
}

func TestMaintenanceFailureRecordsRetryBeforeTheNextDailyCycle(t *testing.T) {
	t.Setenv("LLM_MODEL_MAINTENANCE_FAILURE_RETRY_MINUTES", "5")
	now := time.Date(2026, time.July, 21, 12, 0, 0, 0, time.UTC)
	failed := models.LLMModelMaintenance{Status: "failed", BlocksExecution: true, CheckedAt: now.Add(-4 * time.Minute)}
	if !maintenanceRecordReusable(failed, now, 24*time.Hour) {
		t.Fatal("a recent failure should observe the bounded retry cooldown")
	}
	if maintenanceRecordReusable(models.LLMModelMaintenance{Status: "failed", BlocksExecution: true, CheckedAt: now.Add(-5 * time.Minute)}, now, 24*time.Hour) {
		t.Fatal("a failure at the retry boundary must be checked again")
	}

	result := modelMaintenanceResult(failed)
	if result.NextCheckDueAt == nil || !result.NextCheckDueAt.Equal(now.Add(time.Minute)) {
		t.Fatalf("failure retry due = %v, want %v", result.NextCheckDueAt, now.Add(time.Minute))
	}
}

func TestModelMaintenanceFailureRetryIntervalIsBounded(t *testing.T) {
	t.Setenv("LLM_MODEL_MAINTENANCE_FAILURE_RETRY_MINUTES", "0")
	if interval := modelMaintenanceFailureRetryInterval(); interval != time.Minute {
		t.Fatalf("minimum retry interval = %s, want 1m", interval)
	}
	t.Setenv("LLM_MODEL_MAINTENANCE_FAILURE_RETRY_MINUTES", "999")
	if interval := modelMaintenanceFailureRetryInterval(); interval != time.Hour {
		t.Fatalf("maximum retry interval = %s, want 1h", interval)
	}
}

func TestMaintenanceModelIDAllowsRegistryNamespacesWithoutAcceptingURLs(t *testing.T) {
	if !validMaintenanceModelID("library/qwen2.5:7b") {
		t.Fatal("registry namespace should be valid")
	}
	for _, value := range []string{"", "../qwen", "https://example.test/model", "qwen?tag=latest", "qwen\nnext"} {
		if validMaintenanceModelID(value) {
			t.Fatalf("invalid model ID accepted: %q", value)
		}
	}
}

func TestMaintenanceEndpointKeyOnlyNormalizesTheOpenAICompatibilitySuffix(t *testing.T) {
	for _, endpoint := range []string{"http://host.docker.internal:11434", "http://host.docker.internal:11434/v1/"} {
		got, err := maintenanceEndpointKey(endpoint)
		if err != nil || got != "http://host.docker.internal:11434" {
			t.Fatalf("endpoint key for %q = %q, %v", endpoint, got, err)
		}
	}
	for _, endpoint := range []string{"https://user@example.test", "https://example.test/v1/other", "https://example.test/?token=secret"} {
		if _, err := maintenanceEndpointKey(endpoint); err == nil {
			t.Fatalf("unsafe endpoint accepted: %q", endpoint)
		}
	}
}

func TestLocalMaintenanceEndpointKeyRejectsPublicHosts(t *testing.T) {
	for _, endpoint := range []string{"https://example.test/v1", "http://192.168.1.20:11434"} {
		if _, err := localMaintenanceEndpointKey(endpoint); err == nil {
			t.Fatalf("non-local planning endpoint accepted: %q", endpoint)
		}
	}
	for _, endpoint := range []string{"http://127.0.0.1:11434/v1", "http://host.docker.internal:11434"} {
		if _, err := localMaintenanceEndpointKey(endpoint); err != nil {
			t.Fatalf("local planning endpoint rejected: %q: %v", endpoint, err)
		}
	}
}

func TestMiniSWEOllamaEndpointIsExactAndCannotAliasAnotherLocalModelServer(t *testing.T) {
	if !isMiniSWEOllamaEndpoint("http://ollama-miniswe:11434/") {
		t.Fatal("isolated mini-SWE Ollama endpoint was rejected")
	}
	for _, endpoint := range []string{"http://host.docker.internal:11434", "http://ollama:11434", "https://ollama-miniswe:11434"} {
		if isMiniSWEOllamaEndpoint(endpoint) {
			t.Fatalf("non-isolated endpoint accepted: %q", endpoint)
		}
	}
}
