package llm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

func admissionClaimKey(providerID, modelID, fingerprint string) string {
	return providerID + "\x00" + modelID + "\x00" + fingerprint
}

func (r *fakeModelMaintenanceRepository) GetModelMaintenanceAdmission(
	ctx context.Context,
	providerID, modelID, fingerprint string,
) (ModelMaintenanceAdmissionClaim, bool, error) {
	if err := ctx.Err(); err != nil {
		return ModelMaintenanceAdmissionClaim{}, false, err
	}
	key := admissionClaimKey(providerID, modelID, fingerprint)
	r.admissionMu.Lock()
	defer r.admissionMu.Unlock()
	claim, found := r.admissionClaims[key]
	claim.Token = ""
	return claim, found, nil
}

func (r *fakeModelMaintenanceRepository) AcquireModelMaintenanceAdmission(
	ctx context.Context,
	providerID, modelID, fingerprint string,
	lease time.Duration,
) (ModelMaintenanceAdmissionClaim, bool, error) {
	if err := ctx.Err(); err != nil {
		return ModelMaintenanceAdmissionClaim{}, false, err
	}
	if r.admissionWriteErr != nil {
		return ModelMaintenanceAdmissionClaim{}, false, r.admissionWriteErr
	}
	claim := ModelMaintenanceAdmissionClaim{
		ProviderID: providerID, ModelID: modelID, ConfigurationFingerprint: fingerprint,
		Token: uuid.NewString(), State: modelMaintenanceAdmissionInProgress,
		RetryAt: time.Now().UTC().Add(lease),
	}
	key := admissionClaimKey(providerID, modelID, fingerprint)
	r.admissionMu.Lock()
	defer r.admissionMu.Unlock()
	if existing, ok := r.admissionClaims[key]; ok && existing.RetryAt.After(time.Now().UTC()) {
		return ModelMaintenanceAdmissionClaim{
			ProviderID: providerID, ModelID: modelID, ConfigurationFingerprint: fingerprint,
			State: existing.State, RetryAt: existing.RetryAt,
		}, false, nil
	}
	if r.admissionClaims == nil {
		r.admissionClaims = make(map[string]ModelMaintenanceAdmissionClaim)
	}
	r.admissionClaims[key] = claim
	return claim, true, nil
}

func (r *fakeModelMaintenanceRepository) FinalizeModelMaintenanceAdmission(
	ctx context.Context,
	claim ModelMaintenanceAdmissionClaim,
	outcome string,
	retryAfter time.Duration,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if r.admissionFinalizeErr != nil {
		return r.admissionFinalizeErr
	}
	key := admissionClaimKey(claim.ProviderID, claim.ModelID, claim.ConfigurationFingerprint)
	r.admissionMu.Lock()
	defer r.admissionMu.Unlock()
	existing, ok := r.admissionClaims[key]
	if !ok || existing.Token != claim.Token || existing.State != modelMaintenanceAdmissionInProgress {
		return errModelMaintenanceAdmissionLost
	}
	switch outcome {
	case modelMaintenanceAdmissionVerified:
		existing.State = modelMaintenanceAdmissionSucceeded
	case modelMaintenanceAdmissionRetry:
		existing.State = modelMaintenanceAdmissionRetryWait
	default:
		return errors.New("unsupported fake admission outcome")
	}
	existing.RetryAt = time.Now().UTC().Add(retryAfter)
	r.admissionClaims[key] = existing
	return nil
}

func admissionOllamaFixture(serverURL string) (Provider, Policy) {
	model := Model{ID: "phi3:mini", Name: "Phi", Tier: TierLocal, Enabled: true}
	provider := Provider{
		ID: "ollama", Name: "Ollama", Enabled: true, Local: true,
		EndpointURL: serverURL, Models: []Model{model},
	}
	return provider, Policy{LocalModelsAllowed: true, Providers: []Provider{provider}}
}

func admissionOllamaServer(t *testing.T, counts *atomic.Int32, blockPull <-chan struct{}, pullStarted chan<- struct{}) *httptest.Server {
	t.Helper()
	var tagCalls atomic.Int32
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		counts.Add(1)
		switch r.URL.Path {
		case "/api/tags":
			digest := "sha256:old"
			if tagCalls.Add(1) > 1 {
				digest = "sha256:new"
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"models": []map[string]string{{"name": "phi3:mini", "digest": digest}}})
		case "/api/pull":
			if pullStarted != nil {
				select {
				case pullStarted <- struct{}{}:
				default:
				}
			}
			if blockPull != nil {
				<-blockPull
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "success"})
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestModelMaintenanceFailsClosedWhenHistoryIsUnavailable(t *testing.T) {
	t.Setenv("LLM_MODEL_MAINTENANCE_ENABLED", "true")
	var calls atomic.Int32
	server := admissionOllamaServer(t, &calls, nil, nil)
	defer server.Close()
	provider, policy := admissionOllamaFixture(server.URL)
	service := withTrustedTestFinalEffects(t, &Service{policy: policy})

	result, err := service.Generate(withTrustedTestEffect(GenerateRequest{
		Task: "Draft a local answer",
		RouteDecision: &RouteDecision{
			SelectedProviderID: provider.ID,
			SelectedModelID:    provider.Models[0].ID,
			SelectedModelName:  provider.Models[0].Name,
			Tier:               TierLocal,
		},
	}))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if result == nil || result.Status == "completed" || result.Output != "" || !strings.Contains(result.Reason, "durable maintenance history is unavailable") {
		t.Fatalf("generation result = %#v, want blocked admission without output", result)
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("provider requests = %d, want zero before maintenance history is available", got)
	}
}

func TestOllamaAdmissionClaimWriteFailureMakesNoProviderCalls(t *testing.T) {
	t.Setenv("LLM_MODEL_MAINTENANCE_ENABLED", "true")
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"models": []map[string]string{{"name": "phi3:mini", "digest": "sha256:old"}}})
	}))
	defer server.Close()
	provider, policy := admissionOllamaFixture(server.URL)
	repository := &fakeModelMaintenanceRepository{admissionWriteErr: errors.New("claim store unavailable")}
	service := withTrustedTestFinalEffects(t, &Service{policy: policy, maintenanceHistory: repository, maintenanceRunning: make(map[string]*sync.Mutex)})
	result := service.ensureModelFresh(provider, provider.Models[0], service.maintenanceEffectContext)
	if result.Status != "failed" || !result.BlocksExecution {
		t.Fatalf("result = %#v, want fail-closed claim error", result)
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("provider requests = %d, want zero when claim write fails", got)
	}
}

func TestOllamaAdmissionSurvivesHistoryFailureNewServiceAndOwner(t *testing.T) {
	t.Setenv("LLM_MODEL_MAINTENANCE_ENABLED", "true")
	var calls atomic.Int32
	server := admissionOllamaServer(t, &calls, nil, nil)
	defer server.Close()
	provider, policy := admissionOllamaFixture(server.URL)
	base := &fakeModelMaintenanceRepository{}
	failingHistory := &failingModelMaintenanceRepository{fakeModelMaintenanceRepository: base, err: errors.New("history write failed")}
	firstService := withTrustedTestFinalEffects(t, &Service{policy: policy, maintenanceHistory: failingHistory, maintenanceRunning: make(map[string]*sync.Mutex)})
	first := firstService.ensureModelFresh(provider, provider.Models[0], firstService.maintenanceEffectContext)
	if first.Status != "failed" || !first.BlocksExecution || !first.UpdateAttempted {
		t.Fatalf("first attempt = %#v, want failed and blocked after attempted pull", first)
	}

	secondService := &Service{policy: policy, maintenanceHistory: failingHistory, maintenanceRunning: make(map[string]*sync.Mutex)}
	second := secondService.ensureModelFreshWithContext(context.Background(), provider, provider.Models[0], &EffectContext{OwnerIdentity: "owner-b"})
	if second.Status != "failed" || !second.BlocksExecution || second.NextCheckDueAt == nil {
		t.Fatalf("new service/owner result = %#v, want shared durable retry block", second)
	}
	if got := calls.Load(); got != 3 {
		t.Fatalf("provider requests after fresh service = %d, want original tags/pull/tags only (3)", got)
	}
	fingerprint := modelMaintenanceFingerprint(provider, provider.Models[0], policy)
	base.admissionMu.Lock()
	claim := base.admissionClaims[admissionClaimKey(provider.ID, provider.Models[0].ID, fingerprint)]
	base.admissionMu.Unlock()
	if claim.State != modelMaintenanceAdmissionRetryWait || !claim.RetryAt.After(time.Now()) {
		t.Fatalf("durable fake claim = %#v, want bounded retry_wait state", claim)
	}
}

func TestOllamaAdmissionAllowsConfigurationChangeAndReusesSuccess(t *testing.T) {
	t.Setenv("LLM_MODEL_MAINTENANCE_ENABLED", "true")
	var callsA, callsB atomic.Int32
	serverA := admissionOllamaServer(t, &callsA, nil, nil)
	defer serverA.Close()
	serverB := admissionOllamaServer(t, &callsB, nil, nil)
	defer serverB.Close()
	provider, policy := admissionOllamaFixture(serverA.URL)
	base := &fakeModelMaintenanceRepository{}
	failing := &failingModelMaintenanceRepository{fakeModelMaintenanceRepository: base, err: errors.New("history write failed")}
	firstService := withTrustedTestFinalEffects(t, &Service{policy: policy, maintenanceHistory: failing, maintenanceRunning: make(map[string]*sync.Mutex)})
	first := firstService.ensureModelFresh(provider, provider.Models[0], firstService.maintenanceEffectContext)
	if first.Status != "failed" || !first.BlocksExecution {
		t.Fatalf("initial history-failure attempt = %#v", first)
	}

	provider.EndpointURL = serverB.URL
	policy.Providers[0].EndpointURL = serverB.URL
	changedService := withTrustedTestFinalEffects(t, &Service{policy: policy, maintenanceHistory: base, maintenanceRunning: make(map[string]*sync.Mutex)})
	changed := changedService.ensureModelFresh(provider, provider.Models[0], changedService.maintenanceEffectContext)
	if !isVerifiedLocalMaintenanceResult(provider, changed) || callsB.Load() != 3 {
		t.Fatalf("changed-configuration result = %#v, requests=%d; want verified refresh on new fingerprint", changed, callsB.Load())
	}

	reuseService := &Service{policy: policy, maintenanceHistory: base, maintenanceRunning: make(map[string]*sync.Mutex)}
	reused := reuseService.ensureModelFresh(provider, provider.Models[0])
	if !reused.Reused || !isVerifiedLocalMaintenanceResult(provider, reused) || callsB.Load() != 3 {
		t.Fatalf("success reuse = %#v, requests=%d; want durable history reuse and no extra provider I/O", reused, callsB.Load())
	}
	if callsA.Load() != 3 {
		t.Fatalf("original config provider requests = %d, want 3", callsA.Load())
	}
}

func TestOllamaAdmissionCoordinatesIndependentServices(t *testing.T) {
	t.Setenv("LLM_MODEL_MAINTENANCE_ENABLED", "true")
	var calls atomic.Int32
	pullStarted := make(chan struct{}, 1)
	releasePull := make(chan struct{})
	server := admissionOllamaServer(t, &calls, releasePull, pullStarted)
	defer server.Close()
	provider, policy := admissionOllamaFixture(server.URL)
	repository := &fakeModelMaintenanceRepository{}
	firstService := withTrustedTestFinalEffects(t, &Service{policy: policy, maintenanceHistory: repository, maintenanceRunning: make(map[string]*sync.Mutex)})
	firstDone := make(chan ModelMaintenanceResult, 1)
	go func() {
		firstDone <- firstService.ensureModelFresh(provider, provider.Models[0], firstService.maintenanceEffectContext)
	}()
	select {
	case <-pullStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("first service did not reach the provider pull")
	}

	secondService := &Service{policy: policy, maintenanceHistory: repository, maintenanceRunning: make(map[string]*sync.Mutex)}
	second := secondService.ensureModelFreshWithContext(context.Background(), provider, provider.Models[0], &EffectContext{OwnerIdentity: "owner-b"})
	if second.Status != "in_progress" || !second.BlocksExecution {
		t.Fatalf("concurrent second service result = %#v, want blocked in_progress", second)
	}
	close(releasePull)
	select {
	case first := <-firstDone:
		if !isVerifiedLocalMaintenanceResult(provider, first) {
			t.Fatalf("first service result = %#v, want verified", first)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("first service did not finish after pull was released")
	}
	if got := calls.Load(); got != 3 {
		t.Fatalf("provider requests across concurrent services = %d, want only first service's 3", got)
	}
}

type cancellingAdmissionRepository struct {
	*fakeModelMaintenanceRepository
	cancel context.CancelFunc
}

func (r *cancellingAdmissionRepository) AcquireModelMaintenanceAdmission(ctx context.Context, providerID, modelID, fingerprint string, lease time.Duration) (ModelMaintenanceAdmissionClaim, bool, error) {
	claim, acquired, err := r.fakeModelMaintenanceRepository.AcquireModelMaintenanceAdmission(ctx, providerID, modelID, fingerprint, lease)
	if acquired && err == nil {
		r.cancel()
	}
	return claim, acquired, err
}

func TestOllamaAdmissionCancellationFinalizesBoundedRetry(t *testing.T) {
	t.Setenv("LLM_MODEL_MAINTENANCE_ENABLED", "true")
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"models": []map[string]string{{"name": "phi3:mini", "digest": "sha256:old"}}})
	}))
	defer server.Close()
	provider, policy := admissionOllamaFixture(server.URL)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	base := &fakeModelMaintenanceRepository{}
	repository := &cancellingAdmissionRepository{fakeModelMaintenanceRepository: base, cancel: cancel}
	service := withTrustedTestFinalEffects(t, &Service{policy: policy, maintenanceHistory: repository, maintenanceRunning: make(map[string]*sync.Mutex)})
	result := service.ensureModelFreshWithContext(ctx, provider, provider.Models[0], service.maintenanceEffectContext)
	if result.Status != "cancelled" || !result.BlocksExecution || calls.Load() != 0 {
		t.Fatalf("cancellation result = %#v, provider requests=%d; want fail-closed before provider I/O", result, calls.Load())
	}
	fingerprint := modelMaintenanceFingerprint(provider, provider.Models[0], policy)
	base.admissionMu.Lock()
	claim := base.admissionClaims[admissionClaimKey(provider.ID, provider.Models[0].ID, fingerprint)]
	base.admissionMu.Unlock()
	if claim.State != modelMaintenanceAdmissionRetryWait || !claim.RetryAt.After(time.Now()) {
		t.Fatalf("claim after cancellation = %#v, want finalized bounded retry_wait", claim)
	}
}

func TestOllamaAdmissionFinalizationFailureBlocksProviderReuse(t *testing.T) {
	t.Setenv("LLM_MODEL_MAINTENANCE_ENABLED", "true")
	var calls atomic.Int32
	server := admissionOllamaServer(t, &calls, nil, nil)
	defer server.Close()
	provider, policy := admissionOllamaFixture(server.URL)
	repository := &fakeModelMaintenanceRepository{admissionFinalizeErr: errors.New("claim finalization unavailable")}
	service := withTrustedTestFinalEffects(t, &Service{policy: policy, maintenanceHistory: repository, maintenanceRunning: make(map[string]*sync.Mutex)})
	first := service.ensureModelFresh(provider, provider.Models[0], service.maintenanceEffectContext)
	if first.Status != "failed" || !first.BlocksExecution || calls.Load() != 3 {
		t.Fatalf("first result = %#v, calls=%d; want fail-closed finalization", first, calls.Load())
	}
	second := (&Service{policy: policy, maintenanceHistory: repository, maintenanceRunning: make(map[string]*sync.Mutex)}).
		ensureModelFresh(provider, provider.Models[0])
	if second.Status != "in_progress" || !second.BlocksExecution || second.Reused || calls.Load() != 3 {
		t.Fatalf("unfinalized claim must block even persisted history reuse: result=%#v calls=%d", second, calls.Load())
	}
}
