package llm

import (
	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/safety"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ModelMaintenanceResult is an operator-safe account of a model freshness
// check. Only Ollama has a supported local pull path; every other runtime is
// explicitly probe-only rather than being treated as an arbitrary updater.
type ModelMaintenanceResult struct {
	ProviderID               string     `json:"providerId"`
	ProviderName             string     `json:"providerName"`
	ModelID                  string     `json:"modelId"`
	ModelName                string     `json:"modelName"`
	Status                   string     `json:"status"`
	Reason                   string     `json:"reason"`
	PreviousDigest           string     `json:"previousDigest,omitempty"`
	CurrentDigest            string     `json:"currentDigest,omitempty"`
	ConfigurationFingerprint string     `json:"-"`
	ConfigurationChanged     bool       `json:"configurationChanged"`
	UpdateAttempted          bool       `json:"updateAttempted"`
	UpdateApplied            bool       `json:"updateApplied"`
	BlocksExecution          bool       `json:"blocksExecution"`
	Reused                   bool       `json:"reused"`
	CheckedAt                time.Time  `json:"checkedAt"`
	NextCheckDueAt           *time.Time `json:"nextCheckDueAt,omitempty"`
}

// ModelMaintenanceRun summarizes one background sweep. It includes every
// configured, enabled model that current routing policy can use and carries no
// prompt, token, or source content.
type ModelMaintenanceRun struct {
	Eligible int `json:"eligible"`
	// Checked counts local runtime checks and read-only provider catalog probes.
	Checked int `json:"checked"`
	// ProviderManaged counts cloud models checked through the provider's read-only catalog.
	ProviderManaged int `json:"providerManaged"`
	// HealthOnly counts runtimes whose probe confirms health but not model availability.
	HealthOnly int                      `json:"healthOnly"`
	Reused     int                      `json:"reused"`
	Updated    int                      `json:"updated"`
	InProgress int                      `json:"inProgress"`
	Failed     int                      `json:"failed"`
	Cancelled  bool                     `json:"cancelled"`
	Results    []ModelMaintenanceResult `json:"results"`
	RunAt      time.Time                `json:"runAt"`
}

type ollamaTagsResponse struct {
	Models []struct {
		Name   string `json:"name"`
		Digest string `json:"digest"`
	} `json:"models"`
}

type ollamaPullResponse struct {
	Status string `json:"status"`
	Error  string `json:"error"`
}

const (
	defaultModelMaintenanceIntervalHours       = 24
	minimumModelMaintenanceIntervalHours       = 24
	maximumModelMaintenanceIntervalHours       = 24
	defaultModelMaintenanceTimeoutSeconds      = 900
	minimumModelMaintenanceTimeoutSeconds      = 30
	maximumModelMaintenanceTimeoutSeconds      = 60 * 60
	defaultModelMaintenanceFailureRetryMinutes = 5
	minimumModelMaintenanceFailureRetryMinutes = 1
	maximumModelMaintenanceFailureRetryMinutes = 60
	failedRefreshDigestInspectionTimeout       = 5 * time.Second
	miniSWEOllamaProviderID                    = "miniswe-ollama"
	miniSWEOllamaEndpoint                      = "http://ollama-miniswe:11434"
)

var (
	errConfiguredOllamaModelNotInstalled      = errors.New("configured Ollama model is not installed")
	errConfiguredOllamaModelDigestUnavailable = errors.New("configured Ollama model has no verifiable digest")
)

// LocalModelMaintenanceGate is the narrow contract used by optional planning
// runners. A runner must name one exact local provider/model pair from the
// canonical routing policy before it can receive a task. This keeps model
// freshness, budget policy, and audit history in one place instead of letting
// an isolated framework own a parallel model lifecycle.
type LocalModelMaintenanceGate interface {
	EnsureConfiguredLocalModel(endpointURL, modelID string) error
}

// IsolatedOllamaMaintenanceGate is intentionally narrower than the main
// policy gate. mini-SWE owns a separate disposable Ollama volume so it cannot
// share a model endpoint with normal HAI work. This contract admits only that
// one Compose-internal endpoint and persists the same daily pull evidence.
type IsolatedOllamaMaintenanceGate interface {
	EnsureMiniSWEOllamaModel(endpointURL, modelID string) error
}

// modelMaintenanceLeaseRepository is optional so policy-only and focused-test
// services remain dependency-light. The production GORM history repository
// supplies a PostgreSQL-backed lease for each provider/model pair.
type modelMaintenanceLeaseRepository interface {
	AcquireModelMaintenanceLease(ctx context.Context, providerID, modelID string) (release func(), acquired bool, err error)
}

// EnsureConfiguredLocalModel verifies that an optional local planning runner
// is using an enabled local provider/model pair from the canonical LLM policy
// and applies the same durable daily maintenance gate used by normal routing.
// The endpoint comparison is deliberately exact after harmless trailing /v1
// normalization; it never aliases hosts, credentials, or arbitrary paths.
func (s *Service) EnsureConfiguredLocalModel(endpointURL, modelID string) error {
	return s.EnsureConfiguredLocalModelWithContext(context.Background(), endpointURL, modelID)
}

// EnsureConfiguredLocalModelWithContext lets auxiliary local inference paths
// share the canonical maintenance gate without detaching refresh work from the
// request that will consume the model.
func (s *Service) EnsureConfiguredLocalModelWithContext(ctx context.Context, endpointURL, modelID string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	endpointKey, err := localMaintenanceEndpointKey(endpointURL)
	if err != nil {
		return fmt.Errorf("configured planning model endpoint is invalid")
	}
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		return fmt.Errorf("configured planning model identifier is missing")
	}
	for _, provider := range s.Policy().Providers {
		if !s.maintenanceEligibleProvider(provider) {
			continue
		}
		providerEndpointKey, providerErr := localMaintenanceEndpointKey(provider.EndpointURL)
		if providerErr != nil || providerEndpointKey != endpointKey {
			continue
		}
		for _, model := range provider.Models {
			if !model.Enabled || strings.TrimSpace(model.ID) != modelID {
				continue
			}
			result := s.ensureModelFreshWithContext(ctx, provider, model, s.maintenanceEffectContext)
			if err := ctx.Err(); err != nil {
				return err
			}
			if result.BlocksExecution || maintenanceStatusBlocksExecution(result.Status) {
				return fmt.Errorf("configured planning model is blocked by daily maintenance: %s", result.Reason)
			}
			return nil
		}
	}
	return fmt.Errorf("configured planning model is not an enabled local model in the canonical LLM policy")
}

// EnsureMiniSWEOllamaModel refreshes the one model used by the isolated
// mini-SWE patch-proposal container. It rejects every endpoint except the
// Compose-internal Ollama service; the caller cannot repurpose it as a generic
// model updater or route a patch task through an external provider.
func (s *Service) EnsureMiniSWEOllamaModel(endpointURL, modelID string) error {
	if !isMiniSWEOllamaEndpoint(endpointURL) {
		return fmt.Errorf("mini-SWE model endpoint is not the isolated Ollama service")
	}
	modelID = strings.TrimSpace(modelID)
	if !validMaintenanceModelID(modelID) {
		return fmt.Errorf("mini-SWE model identifier is invalid")
	}
	result := s.ensureModelFresh(Provider{
		ID: miniSWEOllamaProviderID, Name: "mini-SWE isolated Ollama", EndpointURL: miniSWEOllamaEndpoint, Enabled: true, Local: true,
	}, Model{ID: modelID, Name: modelID, Enabled: true}, s.maintenanceEffectContext)
	if result.BlocksExecution || maintenanceStatusBlocksExecution(result.Status) {
		return fmt.Errorf("mini-SWE model is blocked by daily maintenance: %s", result.Reason)
	}
	return nil
}

func isMiniSWEOllamaEndpoint(raw string) bool {
	endpointKey, err := maintenanceEndpointKey(raw)
	return err == nil && endpointKey == miniSWEOllamaEndpoint
}

// localMaintenanceEndpointKey keeps optional planning runners on an actual
// local endpoint even if somebody incorrectly labels a custom public provider
// as Local in policy configuration. The canonical policy still decides which
// exact provider and model are eligible after this boundary check.
func localMaintenanceEndpointKey(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || !isLocalModelHost(parsed.Hostname()) {
		return "", fmt.Errorf("endpoint is not local")
	}
	return maintenanceEndpointKey(raw)
}

func maintenanceEndpointKey(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("invalid endpoint")
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "" {
		return "", fmt.Errorf("invalid endpoint host")
	}
	port := parsed.Port()
	if (parsed.Scheme == "http" && port == "80") || (parsed.Scheme == "https" && port == "443") {
		port = ""
	}
	path := strings.TrimRight(parsed.EscapedPath(), "/")
	if path == "/v1" {
		path = ""
	}
	if path != "" {
		return "", fmt.Errorf("unsupported endpoint path")
	}
	if port != "" {
		host += ":" + port
	}
	return strings.ToLower(parsed.Scheme) + "://" + host, nil
}

func (s *Service) ModelMaintenanceHistory(limit int) ([]ModelMaintenanceResult, error) {
	return s.ModelMaintenanceHistoryWithContext(context.Background(), limit)
}

func (s *Service) ModelMaintenanceHistoryWithContext(ctx context.Context, limit int) ([]ModelMaintenanceResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.maintenanceHistory == nil {
		return []ModelMaintenanceResult{}, nil
	}
	records, err := findRecentModelMaintenance(ctx, s.maintenanceHistory, limit)
	if err != nil {
		return nil, fmt.Errorf("load model maintenance history: %w", err)
	}
	results := make([]ModelMaintenanceResult, 0, len(records))
	for _, record := range records {
		results = append(results, modelMaintenanceResult(record))
	}
	return results, nil
}

// RunDueModelMaintenance runs the same daily gate used by routing for every
// enabled configured model that policy can use. Local Ollama tags may be
// refreshed; other local runtimes are checked read-only. Cloud providers get a
// read-only catalog availability probe, but their hosted artifact versions are
// not exposed or changed by HAI. Durable history prevents repeated provider I/O
// before the maintenance interval.
func (s *Service) RunDueModelMaintenance() ModelMaintenanceRun {
	return s.RunDueModelMaintenanceWithContext(context.Background())
}

// RunDueModelMaintenanceWithContext stops between models and cancels local
// requests when the scheduler/request is shutting down. A provider may continue
// a pull it accepted before cancellation; that result remains unverified and
// blocks the model until a later successful check.
func (s *Service) RunDueModelMaintenanceWithContext(ctx context.Context) ModelMaintenanceRun {
	if ctx == nil {
		ctx = context.Background()
	}
	run := ModelMaintenanceRun{Results: []ModelMaintenanceResult{}, RunAt: time.Now().UTC()}
	if ctx.Err() != nil {
		run.Cancelled = true
		return run
	}
	if !modelMaintenanceEnabled() || s.maintenanceHistory == nil {
		return run
	}
	for _, configuredProvider := range s.Policy().Providers {
		if ctx.Err() != nil {
			run.Cancelled = true
			return run
		}
		for _, configuredModel := range configuredProvider.Models {
			if ctx.Err() != nil {
				run.Cancelled = true
				return run
			}
			if !configuredModel.Enabled {
				continue
			}
			// mini-SWE's isolated runtime is local-only, not an Ollama Cloud
			// gateway. Reject a misconfigured cloud tag before it can create a
			// local maintenance failure and retry cooldown.
			if configuredProvider.ID == miniSWEOllamaProviderID && isOllamaCloudModelID(configuredModel.ID) {
				continue
			}
			provider, model := applyOllamaModelPolicy(configuredProvider, configuredModel)
			if !s.maintenanceEligibleProvider(provider) {
				continue
			}
			run.Eligible++
			result := s.ensureModelFreshWithContext(ctx, provider, model, s.maintenanceEffectContext)
			if result.Status == "cancelled" {
				run.Cancelled = true
				return run
			}
			run.Results = append(run.Results, result)
			if result.Reused {
				run.Reused++
			}
			if strings.EqualFold(result.Status, "provider_managed") {
				if strings.EqualFold(provider.ID, "odysseus") {
					run.HealthOnly++
				} else {
					run.ProviderManaged++
				}
			}
			if !result.Reused && result.Status != "not_enforced" && result.Status != "approval_required" {
				run.Checked++
			}
			if result.UpdateApplied {
				run.Updated++
			}
			if result.Status == "in_progress" {
				run.InProgress++
			} else if result.BlocksExecution || result.Status == "failed" {
				run.Failed++
			}
			if ctx.Err() != nil {
				run.Cancelled = true
				return run
			}
		}
	}
	return run
}

func (s *Service) maintenanceEligibleProvider(provider Provider) bool {
	if !provider.Enabled || !providerRuntimeReadiness(provider).configured {
		return false
	}
	if provider.Local {
		return s.policy.LocalModelsAllowed
	}
	if provider.Paid {
		return s.policy.PaidCallsAllowed && s.policy.DailyPaidBudgetEUR > 0
	}
	return s.policy.FreeCloudQuotaAllowed && provider.QuotaRemaining != 0
}

// ensureModelFresh performs at most one maintenance operation per configured
// model every LLM_MODEL_MAINTENANCE_INTERVAL_HOURS (24 by default).
// It runs immediately before routing/generation, so a stale successful result
// cannot silently bypass the maintenance policy after a service restart.
func (s *Service) ensureModelFresh(
	provider Provider,
	model Model,
	effectContexts ...*EffectContext,
) ModelMaintenanceResult {
	return s.ensureModelFreshWithContext(context.Background(), provider, model, effectContexts...)
}

func (s *Service) ensureModelFreshWithContext(
	ctx context.Context,
	provider Provider,
	model Model,
	effectContexts ...*EffectContext,
) ModelMaintenanceResult {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return cancelledMaintenanceResult(provider, model)
	}
	ownerIdentity := modelMaintenanceOwnerIdentity(effectContexts, s.maintenanceEffectContext)
	ctx = withModelMaintenanceScope(ctx, ownerIdentity, provider.Local)
	fingerprint := modelMaintenanceFingerprint(provider, model, s.policy)
	if !modelMaintenanceEnabled() {
		return ModelMaintenanceResult{ProviderID: provider.ID, ProviderName: provider.Name, ModelID: model.ID, ModelName: model.Name, Status: "not_enforced", Reason: "daily model maintenance is not configured for this runtime", CheckedAt: time.Now().UTC()}
	}
	if s.maintenanceHistory == nil {
		checkedAt := time.Now().UTC()
		retryAt := checkedAt.Add(modelMaintenanceFailureRetryInterval())
		return ModelMaintenanceResult{
			ProviderID: provider.ID, ProviderName: provider.Name, ModelID: model.ID, ModelName: model.Name,
			Status: "failed", Reason: "daily model freshness cannot be verified because durable maintenance history is unavailable",
			ConfigurationFingerprint: fingerprint, BlocksExecution: true,
			CheckedAt: checkedAt, NextCheckDueAt: &retryAt,
		}
	}
	s.clearLocalModelMaintenanceCooldown(ctx, provider.ID, model.ID, fingerprint)
	interval := modelMaintenanceInterval()
	now := time.Now().UTC()
	configurationChanged := false
	latest, err := findLatestModelMaintenance(ctx, s.maintenanceHistory, provider.ID, model.ID)
	if ctx.Err() != nil {
		return cancelledMaintenanceResult(provider, model)
	}
	if blocked, stop := s.blockOnDurableOllamaAdmission(ctx, provider, model, fingerprint); stop {
		return blocked
	}
	if err == nil && latest != nil && latest.ConfigurationFingerprint == fingerprint && maintenanceRecordReusableForProvider(provider, *latest, now, interval) {
		result := modelMaintenanceResult(*latest)
		result.Reused = true
		if isVerifiedLocalMaintenanceResult(provider, result) {
			s.clearVerifiedLocalModelMaintenanceCooldown(ctx, provider.ID, model.ID, fingerprint)
		}
		return result
	} else if latest != nil && latest.ConfigurationFingerprint != fingerprint {
		configurationChanged = true
	} else if err != nil {
		return s.recordMaintenanceWithContext(ctx, ModelMaintenanceResult{ProviderID: provider.ID, ProviderName: provider.Name, ModelID: model.ID, ModelName: model.Name, Status: "failed", Reason: "could not read daily model maintenance history", ConfigurationFingerprint: fingerprint, BlocksExecution: true, CheckedAt: time.Now().UTC()})
	}
	s.clearLocalModelMaintenanceCooldown(ctx, provider.ID, model.ID, fingerprint)
	if cooldown := s.localModelMaintenanceCooldown(ctx, provider, model, fingerprint, time.Now().UTC()); cooldown != nil {
		return *cooldown
	}
	if readiness := providerRuntimeReadiness(provider); !readiness.configured {
		return s.recordMaintenanceWithContext(ctx, ModelMaintenanceResult{ProviderID: provider.ID, ProviderName: provider.Name, ModelID: model.ID, ModelName: model.Name, Status: "failed", Reason: "daily model maintenance rejected this runtime endpoint: " + readiness.reason, ConfigurationFingerprint: fingerprint, BlocksExecution: true, CheckedAt: time.Now().UTC()})
	}

	key := provider.ID + "/" + model.ID
	s.maintenanceMu.Lock()
	if s.maintenanceRunning == nil {
		s.maintenanceRunning = map[string]*sync.Mutex{}
	}
	lock := s.maintenanceRunning[key]
	if lock == nil {
		lock = &sync.Mutex{}
		s.maintenanceRunning[key] = lock
	}
	s.maintenanceMu.Unlock()
	if !lockModelMaintenance(ctx, lock) {
		return cancelledMaintenanceResult(provider, model)
	}
	defer lock.Unlock()
	if ctx.Err() != nil {
		return cancelledMaintenanceResult(provider, model)
	}

	// Another request may have completed the daily operation while this request
	// waited for the per-model lock.
	now = time.Now().UTC()
	latest, err = findLatestModelMaintenance(ctx, s.maintenanceHistory, provider.ID, model.ID)
	if ctx.Err() != nil {
		return cancelledMaintenanceResult(provider, model)
	}
	if blocked, stop := s.blockOnDurableOllamaAdmission(ctx, provider, model, fingerprint); stop {
		return blocked
	}
	if err == nil && latest != nil && latest.ConfigurationFingerprint == fingerprint && maintenanceRecordReusableForProvider(provider, *latest, now, interval) {
		result := modelMaintenanceResult(*latest)
		result.Reused = true
		if isVerifiedLocalMaintenanceResult(provider, result) {
			s.clearVerifiedLocalModelMaintenanceCooldown(ctx, provider.ID, model.ID, fingerprint)
		}
		return result
	} else if latest != nil && latest.ConfigurationFingerprint != fingerprint {
		configurationChanged = true
	} else if err != nil {
		return s.recordMaintenanceWithContext(ctx, ModelMaintenanceResult{ProviderID: provider.ID, ProviderName: provider.Name, ModelID: model.ID, ModelName: model.Name, Status: "failed", Reason: "could not re-read daily model maintenance history after waiting for the model refresh lock", ConfigurationFingerprint: fingerprint, BlocksExecution: true, CheckedAt: now})
	}
	s.clearLocalModelMaintenanceCooldown(ctx, provider.ID, model.ID, fingerprint)
	if cooldown := s.localModelMaintenanceCooldown(ctx, provider, model, fingerprint, now); cooldown != nil {
		return *cooldown
	}
	if leaseRepository, ok := s.maintenanceHistory.(modelMaintenanceLeaseRepository); ok {
		leaseCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		release, acquired, err := leaseRepository.AcquireModelMaintenanceLease(leaseCtx, provider.ID, model.ID)
		if acquired && release != nil {
			defer release()
		}
		cancel()
		if ctx.Err() != nil {
			return cancelledMaintenanceResult(provider, model)
		}
		if err != nil {
			return s.recordMaintenanceWithContext(ctx, ModelMaintenanceResult{ProviderID: provider.ID, ProviderName: provider.Name, ModelID: model.ID, ModelName: model.Name, Status: "failed", Reason: "could not acquire the daily model maintenance lease", ConfigurationFingerprint: fingerprint, BlocksExecution: true, CheckedAt: time.Now().UTC()})
		}
		if !acquired {
			checkedAt := time.Now().UTC()
			nextCheck := checkedAt.Add(modelMaintenanceFailureRetryInterval())
			return ModelMaintenanceResult{ProviderID: provider.ID, ProviderName: provider.Name, ModelID: model.ID, ModelName: model.Name, Status: "in_progress", Reason: "daily model maintenance is already running on another backend process", ConfigurationFingerprint: fingerprint, BlocksExecution: true, CheckedAt: checkedAt, NextCheckDueAt: &nextCheck}
		}
		if release == nil {
			return s.recordMaintenanceWithContext(ctx, ModelMaintenanceResult{ProviderID: provider.ID, ProviderName: provider.Name, ModelID: model.ID, ModelName: model.Name, Status: "failed", Reason: "daily model maintenance lease was acquired without a release function", ConfigurationFingerprint: fingerprint, BlocksExecution: true, CheckedAt: time.Now().UTC()})
		}
		// A different process may have completed maintenance between the local
		// re-check and our successful lease acquisition. Reuse its durable record
		// instead of probing or pulling a model a second time.
		now = time.Now().UTC()
		latest, err := findLatestModelMaintenance(ctx, s.maintenanceHistory, provider.ID, model.ID)
		if ctx.Err() != nil {
			return cancelledMaintenanceResult(provider, model)
		}
		if err != nil {
			return s.recordMaintenanceWithContext(ctx, ModelMaintenanceResult{ProviderID: provider.ID, ProviderName: provider.Name, ModelID: model.ID, ModelName: model.Name, Status: "failed", Reason: "could not read daily model maintenance history after acquiring the model lease", ConfigurationFingerprint: fingerprint, BlocksExecution: true, CheckedAt: now})
		}
		if latest != nil && latest.ConfigurationFingerprint == fingerprint && maintenanceRecordReusableForProvider(provider, *latest, now, interval) {
			result := modelMaintenanceResult(*latest)
			result.Reused = true
			if isVerifiedLocalMaintenanceResult(provider, result) {
				s.clearVerifiedLocalModelMaintenanceCooldown(ctx, provider.ID, model.ID, fingerprint)
			}
			return result
		}
		if latest != nil && latest.ConfigurationFingerprint != fingerprint {
			configurationChanged = true
		}
		s.clearLocalModelMaintenanceCooldown(ctx, provider.ID, model.ID, fingerprint)
		if cooldown := s.localModelMaintenanceCooldown(ctx, provider, model, fingerprint, now); cooldown != nil {
			return *cooldown
		}
	}

	if !provider.Local {
		return s.recordProviderManagedModel(ctx, provider, model, fingerprint, configurationChanged)
	}

	var effectContext *EffectContext
	if len(effectContexts) > 0 {
		effectContext = effectContexts[0]
	}
	if provider.ID == "ollama" || provider.ID == miniSWEOllamaProviderID {
		return s.refreshOllamaModelWithAdmission(ctx, provider, model, fingerprint, configurationChanged, effectContext)
	}
	return s.verifyManagedModel(ctx, provider, model, fingerprint, configurationChanged)
}

func (s *Service) blockOnDurableOllamaAdmission(
	ctx context.Context,
	provider Provider,
	model Model,
	fingerprint string,
) (ModelMaintenanceResult, bool) {
	if provider.ID != "ollama" && provider.ID != miniSWEOllamaProviderID {
		return ModelMaintenanceResult{}, false
	}
	base := ModelMaintenanceResult{
		ProviderID: provider.ID, ProviderName: provider.Name, ModelID: model.ID, ModelName: model.Name,
		ConfigurationFingerprint: fingerprint, BlocksExecution: true, CheckedAt: time.Now().UTC(),
	}
	repository, ok := s.maintenanceHistory.(ModelMaintenanceAdmissionRepository)
	if !ok {
		base.Status = "failed"
		base.Reason = "durable Ollama maintenance admission storage is unavailable; cached results were not reused"
		return base, true
	}
	checkContext, cancel := context.WithTimeout(ctx, 5*time.Second)
	claim, found, err := repository.GetModelMaintenanceAdmission(checkContext, provider.ID, model.ID, fingerprint)
	cancel()
	if ctx.Err() != nil {
		return cancelledMaintenanceResult(provider, model), true
	}
	if err != nil {
		base.Status = "failed"
		base.Reason = "durable Ollama admission state could not be verified; cached results were not reused"
		return base, true
	}
	if !found || !claim.RetryAt.After(time.Now().UTC()) {
		return ModelMaintenanceResult{}, false
	}
	if claim.State != modelMaintenanceAdmissionInProgress && claim.State != modelMaintenanceAdmissionRetryWait {
		return ModelMaintenanceResult{}, false
	}
	return modelMaintenanceAdmissionBlockedResult(provider, model, fingerprint, claim), true
}

func (s *Service) refreshOllamaModelWithAdmission(
	ctx context.Context,
	provider Provider,
	model Model,
	fingerprint string,
	configurationChanged bool,
	effectContext *EffectContext,
) ModelMaintenanceResult {
	base := ModelMaintenanceResult{
		ProviderID: provider.ID, ProviderName: provider.Name, ModelID: model.ID, ModelName: model.Name,
		ConfigurationFingerprint: fingerprint, ConfigurationChanged: configurationChanged,
		BlocksExecution: true, CheckedAt: time.Now().UTC(),
	}
	repository, ok := s.maintenanceHistory.(ModelMaintenanceAdmissionRepository)
	if !ok {
		base.Status = "failed"
		base.Reason = "durable Ollama maintenance admission storage is unavailable; provider access was blocked"
		return s.recordMaintenanceWithContext(ctx, base)
	}
	claimContext, cancelClaim := context.WithTimeout(ctx, 5*time.Second)
	claim, acquired, err := repository.AcquireModelMaintenanceAdmission(
		claimContext, provider.ID, model.ID, fingerprint, modelMaintenanceAdmissionLeaseDuration(),
	)
	cancelClaim()
	if ctx.Err() != nil {
		if acquired {
			finalizeContext, cancelFinalize := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			_ = repository.FinalizeModelMaintenanceAdmission(finalizeContext, claim, modelMaintenanceAdmissionRetry, modelMaintenanceFailureRetryInterval())
			cancelFinalize()
		}
		return cancelledMaintenanceResult(provider, model)
	}
	if err != nil {
		base.Status = "failed"
		base.Reason = "durable Ollama maintenance admission could not be persisted; provider access was blocked"
		return s.recordMaintenanceWithContext(ctx, base)
	}
	if !acquired {
		return modelMaintenanceAdmissionBlockedResult(provider, model, fingerprint, claim)
	}

	result := s.refreshOllamaModel(ctx, provider, model, fingerprint, configurationChanged, effectContext)
	outcome := modelMaintenanceAdmissionRetry
	retryAfter := modelMaintenanceFailureRetryInterval()
	if isVerifiedLocalMaintenanceResult(provider, result) {
		outcome = modelMaintenanceAdmissionVerified
		retryAfter = modelMaintenanceInterval()
	}
	finalizeContext, cancelFinalize := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	finalizeErr := repository.FinalizeModelMaintenanceAdmission(finalizeContext, claim, outcome, retryAfter)
	cancelFinalize()
	if finalizeErr != nil {
		result.Status = "failed"
		result.Reason = "Ollama maintenance outcome could not be finalized durably; provider use remains blocked until the admission lease expires"
		result.BlocksExecution = true
		retryAt := claim.RetryAt.UTC()
		result.NextCheckDueAt = &retryAt
		return result
	}
	if outcome == modelMaintenanceAdmissionRetry && result.NextCheckDueAt == nil {
		retryAt := time.Now().UTC().Add(retryAfter)
		result.NextCheckDueAt = &retryAt
	}
	return result
}

func cancelledMaintenanceResult(provider Provider, model Model) ModelMaintenanceResult {
	return ModelMaintenanceResult{
		ProviderID: provider.ID, ProviderName: provider.Name, ModelID: model.ID, ModelName: model.Name,
		Status: "cancelled", Reason: "maintenance was cancelled before the model was verified", BlocksExecution: true,
		CheckedAt: time.Now().UTC(),
	}
}

func lockModelMaintenance(ctx context.Context, lock *sync.Mutex) bool {
	for {
		if lock.TryLock() {
			return true
		}
		timer := time.NewTimer(25 * time.Millisecond)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return false
		case <-timer.C:
		}
	}
}

// modelMaintenanceRoutingBlockReason is intentionally read-only. Routing and
// classification may inspect durable maintenance evidence, but they may never
// install or update a model merely to decide which model would be suitable.
// A due or absent record is enforced later at Generate with trusted context.
func (s *Service) modelMaintenanceRoutingBlockReason(provider Provider, model Model) string {
	reason, _ := s.modelMaintenanceRoutingBlockReasonWithContext(context.Background(), provider, model)
	return reason
}

func (s *Service) modelMaintenanceRoutingBlockReasonWithContext(ctx context.Context, provider Provider, model Model) (string, error) {
	if !modelMaintenanceEnabled() || s.maintenanceHistory == nil {
		return "", nil
	}
	latest, err := findLatestModelMaintenance(ctx, s.maintenanceHistory, provider.ID, model.ID)
	if err != nil {
		if ctx != nil && ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "could not read daily model maintenance history", nil
	}
	if latest == nil || latest.ConfigurationFingerprint != modelMaintenanceFingerprint(provider, model, s.policy) {
		return "", nil
	}
	if hasUnsupportedLocalVersionClaim(provider, *latest) {
		return "stored maintenance status lacks verifiable upstream version evidence for this local runtime", nil
	}
	if !maintenanceRecordReusableForProvider(provider, *latest, time.Now().UTC(), modelMaintenanceInterval()) {
		return "", nil
	}
	if maintenanceStatusBlocksExecution(latest.Status) || latest.BlocksExecution {
		return latest.Reason, nil
	}
	return "", nil
}

func maintenanceStillFresh(checkedAt, now time.Time, interval time.Duration) bool {
	checkedAt = checkedAt.UTC()
	if checkedAt.IsZero() || checkedAt.After(now) {
		return false
	}
	return now.Sub(checkedAt) < interval
}

// maintenanceRecordReusable treats verified local checks and provider catalog
// probes as valid for the configured interval. A failed check is deliberately
// retried sooner: keeping a model blocked for a whole day after a temporary
// download, network, or runtime outage would be neither safe nor useful.
func maintenanceRecordReusable(record models.LLMModelMaintenance, now time.Time, interval time.Duration) bool {
	if !maintenanceStillFresh(record.CheckedAt, now, interval) {
		return false
	}
	status := strings.ToLower(strings.TrimSpace(record.Status))
	if status == "operator_managed" {
		// The runtime can confirm that a configured name is present, but HAI
		// cannot verify that its artifact is current or refresh it. Keep that
		// unsupported decision for one daily cycle while it remains blocked.
		return true
	}
	if status == "approval_required" {
		// Policy fingerprints include paid-use and approval settings, so an
		// approval change invalidates this result immediately without polling a
		// provider every few minutes while approval is still absent.
		return true
	}
	if record.BlocksExecution || maintenanceStatusBlocksExecution(status) {
		return now.Sub(record.CheckedAt.UTC()) < modelMaintenanceFailureRetryInterval()
	}
	switch status {
	case "current", "updated", "installed", "provider_managed":
		return true
	default:
		// In particular, not_enforced is not evidence that a configured model
		// passed a daily check. Unknown statuses must never authorize cache reuse.
		return false
	}
}

// maintenanceRecordReusableForProvider prevents historical positive statuses
// from authorizing non-Ollama local runtimes. The only local runtime with an
// adapter that verifies artifact identity after an update is Ollama; an
// OpenAI-compatible /models response proves reachability and a configured
// name, not the model artifact's upstream version.
func maintenanceRecordReusableForProvider(provider Provider, record models.LLMModelMaintenance, now time.Time, interval time.Duration) bool {
	if hasUnsupportedLocalVersionClaim(provider, record) {
		return false
	}
	return maintenanceRecordReusable(record, now, interval)
}

func hasUnsupportedLocalVersionClaim(provider Provider, record models.LLMModelMaintenance) bool {
	if !provider.Local || provider.ID == "ollama" || provider.ID == miniSWEOllamaProviderID {
		return false
	}
	status := strings.TrimSpace(record.Status)
	return isVerifiedMaintenanceStatus(status) || strings.EqualFold(status, "provider_managed")
}

// verifyManagedModel checks non-pullable local runtimes and provider-managed
// cloud models through their bounded, read-only provider probe.
func (s *Service) verifyManagedModel(ctx context.Context, provider Provider, model Model, fingerprint string, configurationChanged bool) ModelMaintenanceResult {
	result := ModelMaintenanceResult{ProviderID: provider.ID, ProviderName: provider.Name, ModelID: model.ID, ModelName: model.Name, ConfigurationFingerprint: fingerprint, ConfigurationChanged: configurationChanged, CheckedAt: time.Now().UTC()}
	probe := probeProviderWithContext(ctx, provider, s.policy)
	if ctx.Err() != nil {
		return cancelledMaintenanceResult(provider, model)
	}
	if !probe.Live {
		if provider.Paid && probe.Status == "blocked" {
			result.Status, result.Reason = paidMaintenanceProbeBlock(s.policy)
			result.BlocksExecution = true
			return s.recordMaintenanceWithContext(ctx, result)
		}
		result.Status = "failed"
		result.Reason = "runtime availability check failed: " + probe.Reason
		result.BlocksExecution = true
		return s.recordMaintenanceWithContext(ctx, result)
	}
	if !containsReportedModel(probe.ReportedModelIDs, model.ID) {
		result.Status = "failed"
		result.Reason = "runtime did not report the exact configured model identifier during its daily check"
		result.BlocksExecution = true
		return s.recordMaintenanceWithContext(ctx, result)
	}
	result.Status = "operator_managed"
	result.Reason = "local runtime reported the exact configured model identifier, but HAI cannot verify its upstream version or update this operator-managed installation; model use remains blocked"
	result.BlocksExecution = true
	return s.recordMaintenanceWithContext(ctx, result)
}

func (s *Service) recordProviderManagedModel(ctx context.Context, provider Provider, model Model, fingerprint string, configurationChanged bool) ModelMaintenanceResult {
	result := ModelMaintenanceResult{
		ProviderID: provider.ID, ProviderName: provider.Name, ModelID: model.ID, ModelName: model.Name,
		ConfigurationFingerprint: fingerprint, ConfigurationChanged: configurationChanged, CheckedAt: time.Now().UTC(),
	}
	probe := probeProviderWithContext(ctx, provider, s.policy)
	if ctx.Err() != nil {
		return cancelledMaintenanceResult(provider, model)
	}
	if !probe.Live {
		if provider.Paid && probe.Status == "blocked" {
			result.Status, result.Reason = paidMaintenanceProbeBlock(s.policy)
		} else {
			result.Status = "failed"
			result.Reason = "cloud provider availability check failed: " + probe.Reason
		}
		result.BlocksExecution = true
		return s.recordMaintenanceWithContext(ctx, result)
	}

	result.Status = "provider_managed"
	if provider.ID == "odysseus" {
		result.Reason = "Odysseus health endpoint responded to a read-only check; this confirms workspace health only, not model availability or hosted model version"
	} else {
		result.Reason = "cloud provider endpoint responded to a read-only model catalog check; HAI cannot verify or update the hosted model version"
		if containsReportedModel(probe.ReportedModelIDs, model.ID) {
			result.Reason = "cloud provider catalog reports the configured model ID; HAI cannot verify or update the hosted model version"
		} else if len(probe.ReportedModelIDs) > 0 {
			result.Reason += "; the returned catalog did not expose this ID, so HAI does not infer that it is unavailable"
		} else {
			result.Reason += "; the endpoint did not expose machine-readable model IDs"
		}
	}
	return s.recordMaintenanceWithContext(ctx, result)
}

// paidMaintenanceProbeBlock keeps policy approval separate from temporary
// budget/accounting blocks. Approval state is part of the maintenance
// fingerprint and can remain actionable for a daily cycle; budget state can
// change at the next UTC day boundary or when usage accounting recovers, so it
// must use the short bounded failure retry instead of caching approval_required
// for 24 hours.
func paidMaintenanceProbeBlock(policy Policy) (status, reason string) {
	if !policy.PaidCallsAllowed || policy.RequireApprovalBeforePaidUsage {
		return "approval_required", "paid provider availability check is waiting for the configured approval gate"
	}
	if !paidProviderProbeBudgetAvailable(policy) {
		return "failed", "paid provider availability check is blocked because the daily budget is unavailable or exhausted"
	}
	return "failed", "paid provider availability check was blocked by provider policy"
}

func containsReportedModel(reported []string, modelID string) bool {
	modelID = strings.TrimSpace(modelID)
	for _, id := range reported {
		if strings.EqualFold(strings.TrimSpace(id), modelID) {
			return true
		}
	}
	return false
}

func modelMaintenanceEnabled() bool {
	raw := strings.TrimSpace(os.Getenv("LLM_MODEL_MAINTENANCE_ENABLED"))
	if raw == "" {
		return true
	}
	return envEnabled("LLM_MODEL_MAINTENANCE_ENABLED")
}

// modelMaintenanceInterval is intentionally fixed at one daily cycle. This
// keeps a configuration typo from either multiplying update checks or letting
// an idle configured model go longer than a day without a freshness check.
// Failed checks use the separate, short retry interval while remaining blocked.
func modelMaintenanceInterval() time.Duration {
	hours := intEnv("LLM_MODEL_MAINTENANCE_INTERVAL_HOURS", defaultModelMaintenanceIntervalHours)
	if hours < minimumModelMaintenanceIntervalHours {
		hours = minimumModelMaintenanceIntervalHours
	}
	if hours > maximumModelMaintenanceIntervalHours {
		hours = maximumModelMaintenanceIntervalHours
	}
	return time.Duration(hours) * time.Hour
}

func modelMaintenanceTimeout() time.Duration {
	seconds := boundedMaintenanceEnv("LLM_MODEL_MAINTENANCE_TIMEOUT_SECONDS", defaultModelMaintenanceTimeoutSeconds, minimumModelMaintenanceTimeoutSeconds, maximumModelMaintenanceTimeoutSeconds)
	return time.Duration(seconds) * time.Second
}

func modelMaintenanceFailureRetryInterval() time.Duration {
	minutes := boundedMaintenanceEnv("LLM_MODEL_MAINTENANCE_FAILURE_RETRY_MINUTES", defaultModelMaintenanceFailureRetryMinutes, minimumModelMaintenanceFailureRetryMinutes, maximumModelMaintenanceFailureRetryMinutes)
	return time.Duration(minutes) * time.Minute
}

// boundedMaintenanceEnv distinguishes an absent or malformed setting (use the
// documented default) from an explicit unsafe number (clamp to the safe
// boundary). intEnv intentionally treats both alike for older policy values.
func boundedMaintenanceEnv(name string, fallback, minimum, maximum int) int {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	if value < minimum {
		return minimum
	}
	if value > maximum {
		return maximum
	}
	return value
}

func (s *Service) refreshOllamaModel(
	ctx context.Context,
	provider Provider,
	model Model,
	fingerprint string,
	configurationChanged bool,
	effectContext *EffectContext,
) ModelMaintenanceResult {
	result := ModelMaintenanceResult{ProviderID: provider.ID, ProviderName: provider.Name, ModelID: model.ID, ModelName: model.Name, ConfigurationFingerprint: fingerprint, ConfigurationChanged: configurationChanged, CheckedAt: time.Now().UTC()}
	endpoint := strings.TrimRight(strings.TrimSpace(provider.EndpointURL), "/")
	previousDigest, err := ollamaModelDigest(ctx, endpoint, model.ID)
	if ctx.Err() != nil {
		return cancelledMaintenanceResult(provider, model)
	}
	missingBeforePull := errors.Is(err, errConfiguredOllamaModelNotInstalled)
	digestUnavailableBeforePull := errors.Is(
		err,
		errConfiguredOllamaModelDigestUnavailable,
	)
	if err != nil && !missingBeforePull && !digestUnavailableBeforePull {
		result.Status = "failed"
		result.Reason = "could not inspect installed Ollama model before refresh: " + safety.RedactSecrets(err.Error())
		result.BlocksExecution = true
		return s.recordMaintenanceWithContext(ctx, result)
	}
	result.PreviousDigest = previousDigest
	if !validMaintenanceModelID(model.ID) {
		result.Status = "failed"
		result.Reason = "configured Ollama model identifier is invalid for maintenance"
		result.BlocksExecution = true
		return s.recordMaintenanceWithContext(ctx, result)
	}

	payload, _ := json.Marshal(map[string]any{"name": model.ID, "stream": false})
	// A model pull is a final effect. Scheduled maintenance therefore gets a
	// fresh, server-derived task identity for each actual pull attempt instead
	// of reusing the long-lived scheduler identity whose authority was already
	// consumed by a prior successful run.
	attemptContext := modelMaintenanceAttemptEffectContext(
		effectContext,
		provider.ID,
		model.ID,
		result.CheckedAt,
		atomic.AddUint64(&s.maintenanceAttemptSequence, 1),
	)
	authorization, err := buildFinalEffectAuthorizationRequest(
		EffectOperationModelPull,
		attemptContext,
		provider,
		model,
		endpoint,
		0,
		nil,
		payload,
		fingerprint,
	)
	if err != nil {
		result.Status = "failed"
		result.Reason = "Ollama refresh authorization context is invalid: " + safety.RedactSecrets(err.Error())
		result.BlocksExecution = true
		return s.recordMaintenanceWithContext(ctx, result)
	}

	updateAttempted, err := s.pullOllamaModel(ctx, endpoint, model.ID, payload, authorization)
	result.UpdateAttempted = updateAttempted
	if err != nil {
		if ctx.Err() != nil && !updateAttempted {
			return cancelledMaintenanceResult(provider, model)
		}
		result.Status = "failed"
		result.Reason = "Ollama daily refresh failed; this model will not be used until the next successful check: " + safety.RedactSecrets(err.Error())
		result.BlocksExecution = true
		if updateAttempted {
			// A failed or cancelled pull can still have changed the local runtime.
			// Inspect it with a short independent read so the audit records the
			// observed artifact without treating it as verified or executable.
			inspectionContext, cancel := context.WithTimeout(context.Background(), failedRefreshDigestInspectionTimeout)
			currentDigest, inspectionErr := ollamaModelDigest(inspectionContext, endpoint, model.ID)
			cancel()
			if inspectionErr != nil {
				result.Reason += "; post-failure installed-model inspection failed: " + safety.RedactSecrets(inspectionErr.Error())
			} else {
				result.CurrentDigest = currentDigest
			}
		}
		return s.recordMaintenanceWithContext(ctx, result)
	}
	currentDigest, err := ollamaModelDigest(ctx, endpoint, model.ID)
	if err != nil {
		result.Status = "failed"
		result.Reason = "Ollama refresh completed but the installed model could not be verified: " + safety.RedactSecrets(err.Error())
		result.BlocksExecution = true
		return s.recordMaintenanceWithContext(ctx, result)
	}
	result.CurrentDigest = currentDigest
	result.Status = "current"
	result.Reason = "Ollama checked the configured tag against its registry before this model was used"
	if missingBeforePull {
		result.Status = "installed"
		result.UpdateApplied = true
		result.Reason = "Ollama installed the configured model tag before this model was used"
	} else if previousDigest != "" && currentDigest != "" && previousDigest != currentDigest {
		result.Status = "updated"
		result.UpdateApplied = true
		result.Reason = "Ollama refreshed the configured model tag before this model was used"
	}
	return s.recordMaintenanceWithContext(ctx, result)
}

func modelMaintenanceAttemptEffectContext(
	base *EffectContext,
	providerID string,
	modelID string,
	checkedAt time.Time,
	sequence uint64,
) *EffectContext {
	if base == nil {
		return nil
	}
	attempt := normalizeEffectContext(*base)
	identityDigest := sha256.Sum256([]byte(strings.TrimSpace(providerID) + "\x00" + strings.TrimSpace(modelID)))
	attempt.TaskID = "system:model-maintenance:" + checkedAt.UTC().Format("20060102T150405.000000000Z") + ":" + fmt.Sprintf("%x", identityDigest[:8]) + ":" + strconv.FormatUint(sequence, 10)
	return &attempt
}

func (s *Service) recordMaintenance(result ModelMaintenanceResult) ModelMaintenanceResult {
	return s.recordMaintenanceWithContext(context.Background(), result)
}

func (s *Service) recordMaintenanceWithContext(ctx context.Context, result ModelMaintenanceResult) ModelMaintenanceResult {
	if ctx == nil {
		ctx = context.Background()
	}
	persistenceContext := ctx
	if ctx.Err() != nil && result.UpdateAttempted && strings.EqualFold(result.Status, "failed") {
		// Persist an uncertain external update outcome with a short independent
		// deadline: cancellation stops provider I/O, but must not erase the audit
		// record needed to keep an unverified model blocked.
		var cancel context.CancelFunc
		persistenceContext, cancel = context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
	}
	if err := persistenceContext.Err(); err != nil {
		result.Status = "cancelled"
		result.Reason = "maintenance result was not persisted because the operation was cancelled"
		result.BlocksExecution = true
		return result
	}
	result.Reason = safety.RedactSecrets(result.Reason)
	if maintenanceStatusBlocksExecution(result.Status) {
		result.BlocksExecution = true
	}
	if isSuccessfulModelMaintenanceCheck(result) {
		// CheckedAt represents completion of the provider/runtime check, not the
		// time the attempt started. Set it immediately before persistence so a
		// slow probe cannot make a fresh result look older than it is.
		result.CheckedAt = time.Now().UTC()
	}
	if s.maintenanceHistory == nil {
		return result
	}
	if strings.EqualFold(result.Status, "failed") || result.BlocksExecution {
		// Start the durable retry cooldown when the failed attempt is ready to be
		// persisted, not when its provider I/O began.
		result.CheckedAt = time.Now().UTC()
	}
	writer, ok := s.maintenanceHistory.(ContextModelMaintenanceWriter)
	if !ok {
		result.Status = "failed"
		result.Reason = "model maintenance repository does not support cancellable writes"
		result.BlocksExecution = true
		nextCheck := time.Now().UTC().Add(modelMaintenanceFailureRetryInterval())
		result.NextCheckDueAt = &nextCheck
		s.rememberLocalModelMaintenanceFailure(persistenceContext, result.ProviderID, result.ModelID, result.ConfigurationFingerprint, nextCheck)
		return result
	}
	record, err := writer.RecordModelMaintenanceWithContext(persistenceContext, &models.LLMModelMaintenance{
		ProviderID: result.ProviderID, ProviderName: result.ProviderName, ModelID: result.ModelID, ModelName: result.ModelName,
		Status: result.Status, Reason: result.Reason, PreviousDigest: result.PreviousDigest, CurrentDigest: result.CurrentDigest,
		ConfigurationFingerprint: result.ConfigurationFingerprint, ConfigurationChanged: result.ConfigurationChanged,
		UpdateAttempted: result.UpdateAttempted, UpdateApplied: result.UpdateApplied, BlocksExecution: result.BlocksExecution, CheckedAt: result.CheckedAt,
	})
	if err != nil {
		result.Status = "failed"
		result.Reason = "could not persist daily model maintenance result"
		result.BlocksExecution = true
		nextCheck := time.Now().UTC().Add(modelMaintenanceFailureRetryInterval())
		result.NextCheckDueAt = &nextCheck
		s.rememberLocalModelMaintenanceFailure(persistenceContext, result.ProviderID, result.ModelID, result.ConfigurationFingerprint, nextCheck)
		return result
	}
	result = modelMaintenanceResult(*record)
	if isVerifiedMaintenanceStatus(result.Status) && !result.BlocksExecution {
		s.clearVerifiedLocalModelMaintenanceCooldown(persistenceContext, result.ProviderID, result.ModelID, result.ConfigurationFingerprint)
	}
	if !isModelMaintenanceSchedulerRun(ctx) {
		s.signalModelMaintenanceScheduler()
	}
	return result
}

func isSuccessfulModelMaintenanceCheck(result ModelMaintenanceResult) bool {
	if result.BlocksExecution {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(result.Status)) {
	case "current", "updated", "installed", "provider_managed":
		return true
	default:
		return false
	}
}

func modelMaintenanceResult(record models.LLMModelMaintenance) ModelMaintenanceResult {
	result := ModelMaintenanceResult{ProviderID: record.ProviderID, ProviderName: record.ProviderName, ModelID: record.ModelID, ModelName: record.ModelName, Status: record.Status, Reason: record.Reason, PreviousDigest: record.PreviousDigest, CurrentDigest: record.CurrentDigest, ConfigurationFingerprint: record.ConfigurationFingerprint, ConfigurationChanged: record.ConfigurationChanged, UpdateAttempted: record.UpdateAttempted, UpdateApplied: record.UpdateApplied, BlocksExecution: record.BlocksExecution || maintenanceStatusBlocksExecution(record.Status), CheckedAt: record.CheckedAt}
	if !record.CheckedAt.IsZero() {
		interval := modelMaintenanceInterval()
		if (record.BlocksExecution || strings.EqualFold(record.Status, "failed")) &&
			!strings.EqualFold(record.Status, "operator_managed") &&
			!strings.EqualFold(record.Status, "approval_required") {
			interval = modelMaintenanceFailureRetryInterval()
		}
		next := record.CheckedAt.UTC().Add(interval)
		result.NextCheckDueAt = &next
	}
	return result
}

// modelMaintenanceFingerprint binds a result to its provider/model identity
// without persisting an endpoint or a secret. The normalized endpoint strips
// harmless OpenAI compatibility suffixes while rejecting URLs with credentials,
// paths, queries, or fragments. Every provider probe binds to an opaque
// credential digest and relevant readiness/payment gates so credential or
// policy changes trigger an immediate recheck.
func modelMaintenanceFingerprint(provider Provider, model Model, policy Policy) string {
	endpoint, err := maintenanceEndpointKey(provider.EndpointURL)
	if err != nil {
		endpoint = "invalid-endpoint"
	}
	adapter := "verify-only"
	if provider.ID == "ollama" || provider.ID == miniSWEOllamaProviderID {
		adapter = "ollama-pull"
	} else if !provider.Local {
		adapter = "provider-catalog-probe"
	}
	credentialDigest := sha256.Sum256([]byte(strings.TrimSpace(provider.APIKeyEnv) + "\x00" + strings.TrimSpace(os.Getenv(provider.APIKeyEnv))))
	maintenanceConfiguration := []string{
		"credential=" + fmt.Sprintf("%x", credentialDigest[:]),
		"readiness=" + providerRuntimeReadiness(provider).status,
		fmt.Sprintf("paid=%t", provider.Paid),
		fmt.Sprintf("paid-calls-allowed=%t", policy.PaidCallsAllowed),
		fmt.Sprintf("paid-budget-enabled=%t", policy.DailyPaidBudgetEUR > 0),
		fmt.Sprintf("paid-approval-required=%t", policy.RequireApprovalBeforePaidUsage),
	}
	value := strings.Join([]string{
		"v5",
		strings.TrimSpace(provider.ID),
		strings.TrimSpace(model.ID),
		endpoint,
		adapter,
		fmt.Sprintf("local=%t", provider.Local),
		strings.Join(maintenanceConfiguration, "|"),
	}, "|")
	return fmt.Sprintf("%x", sha256.Sum256([]byte(value)))
}

func ollamaModelDigest(parent context.Context, endpoint, modelID string) (string, error) {
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, modelMaintenanceTimeout())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"/api/tags", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "018-HAI-Model-Maintenance/1.0")
	response, err := localProviderHTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode >= 300 {
		body, readErr := readLimitedResponseBody(response.Body, 64*1024)
		if readErr != nil {
			return "", fmt.Errorf("read Ollama tags response: %w", readErr)
		}
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", fmt.Errorf("Ollama tags returned HTTP %d: %s", response.StatusCode, compactOutput(body, 180))
	}
	body, err := readLimitedResponseBody(response.Body, 256*1024)
	if err != nil {
		return "", fmt.Errorf("read Ollama tags response: %w", err)
	}
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	var tags ollamaTagsResponse
	if err := json.Unmarshal(body, &tags); err != nil {
		return "", err
	}
	for _, installed := range tags.Models {
		if installed.Name == modelID {
			digest := strings.TrimSpace(installed.Digest)
			if digest == "" {
				// A successful pull response is not enough to prove which artifact
				// the runtime will execute. The tags endpoint documents a digest for
				// every installed model, so reject incomplete responses instead of
				// recording an unverifiable model as current.
				return "", fmt.Errorf(
					"%w: %s",
					errConfiguredOllamaModelDigestUnavailable,
					modelID,
				)
			}
			return digest, nil
		}
	}
	return "", fmt.Errorf("%w: %s", errConfiguredOllamaModelNotInstalled, modelID)
}

func (s *Service) pullOllamaModel(
	parent context.Context,
	endpoint,
	modelID string,
	payload []byte,
	authorization FinalEffectAuthorizationRequest,
) (bool, error) {
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, modelMaintenanceTimeout())
	defer cancel()
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"/api/pull", bytes.NewReader(payload))
	if err != nil {
		return false, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "018-HAI-Model-Maintenance/1.0")
	if err := s.authorizeFinalEffect(ctx, authorization); err != nil {
		return false, err
	}
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	response, err := localProviderHTTPClient.Do(req)
	if err != nil {
		return true, err
	}
	defer response.Body.Close()
	body, err := readLimitedResponseBody(response.Body, 128*1024)
	if err != nil {
		return true, fmt.Errorf("read Ollama pull response: %w", err)
	}
	if ctx.Err() != nil {
		return true, ctx.Err()
	}
	if response.StatusCode >= 300 {
		return true, fmt.Errorf("Ollama pull returned HTTP %d: %s", response.StatusCode, compactOutput(body, 240))
	}
	var pull ollamaPullResponse
	if err := json.Unmarshal(body, &pull); err != nil {
		return true, fmt.Errorf("decode Ollama pull response: %w", err)
	}
	if strings.TrimSpace(pull.Error) != "" {
		return true, fmt.Errorf("Ollama pull failed: %s", safety.RedactSecrets(pull.Error))
	}
	if !strings.EqualFold(strings.TrimSpace(pull.Status), "success") {
		return true, fmt.Errorf("Ollama pull did not confirm success (status %q)", strings.TrimSpace(pull.Status))
	}
	return true, nil
}

func validMaintenanceModelID(value string) bool {
	value = strings.TrimSpace(value)
	// Ollama registry names can contain a namespace separator (for example
	// "library/qwen2.5:7b"). The value is JSON data, never a filesystem path,
	// so allow slash while rejecting URL-like and control-character inputs.
	if value == "" || len(value) > 255 || strings.ContainsAny(value, "\\\r\n\t?#") || strings.Contains(value, "..") || strings.Contains(value, "://") {
		return false
	}
	return true
}
