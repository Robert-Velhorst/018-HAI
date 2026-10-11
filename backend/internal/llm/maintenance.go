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
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
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
	Eligible int                      `json:"eligible"`
	Checked  int                      `json:"checked"`
	Reused   int                      `json:"reused"`
	Updated  int                      `json:"updated"`
	Failed   int                      `json:"failed"`
	Results  []ModelMaintenanceResult `json:"results"`
	RunAt    time.Time                `json:"runAt"`
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

// EnsureConfiguredLocalModel verifies that an optional local planning runner
// is using an enabled local provider/model pair from the canonical LLM policy
// and applies the same durable daily maintenance gate used by normal routing.
// The endpoint comparison is deliberately exact after harmless trailing /v1
// normalization; it never aliases hosts, credentials, or arbitrary paths.
func (s *Service) EnsureConfiguredLocalModel(endpointURL, modelID string) error {
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
			result := s.ensureModelFresh(provider, model, s.maintenanceEffectContext)
			if result.BlocksExecution || strings.EqualFold(result.Status, "failed") {
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
	if result.BlocksExecution || strings.EqualFold(result.Status, "failed") {
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
	if s.maintenanceHistory == nil {
		return []ModelMaintenanceResult{}, nil
	}
	records, err := s.maintenanceHistory.FindRecentModelMaintenance(limit)
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
// refreshed; other runtimes and cloud providers are verified read-only. The
// durable history means a scheduler can call it frequently without repeating
// provider I/O before the configured maintenance interval expires.
func (s *Service) RunDueModelMaintenance() ModelMaintenanceRun {
	run, _ := s.RunDueModelMaintenanceContext(context.Background())
	return run
}

// RunDueModelMaintenanceContext is the interactive form of the daily sweep.
// It stops before another model operation when its caller is gone, while the
// scheduler continues to use RunDueModelMaintenance with a durable context.
func (s *Service) RunDueModelMaintenanceContext(ctx context.Context) (ModelMaintenanceRun, error) {
	ctx = normalizeMaintenanceContext(ctx)
	run := ModelMaintenanceRun{Results: []ModelMaintenanceResult{}, RunAt: time.Now().UTC()}
	if err := ctx.Err(); err != nil {
		return run, err
	}
	if !modelMaintenanceEnabled() || s.maintenanceHistory == nil {
		return run, nil
	}
	for _, provider := range s.Policy().Providers {
		if !s.maintenanceEligibleProvider(provider) {
			continue
		}
		for _, model := range provider.Models {
			if err := ctx.Err(); err != nil {
				return run, err
			}
			if !model.Enabled {
				continue
			}
			run.Eligible++
			result, err := s.ensureModelFreshContext(ctx, provider, model, s.maintenanceEffectContext)
			if err != nil {
				return run, err
			}
			run.Results = append(run.Results, result)
			if result.Reused {
				run.Reused++
			} else if result.Status != "not_enforced" {
				run.Checked++
			}
			if result.UpdateApplied {
				run.Updated++
			}
			if result.BlocksExecution || result.Status == "failed" {
				run.Failed++
			}
		}
	}
	return run, nil
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
	result, _ := s.ensureModelFreshContext(context.Background(), provider, model, effectContexts...)
	return result
}

func (s *Service) ensureModelFreshContext(
	ctx context.Context,
	provider Provider,
	model Model,
	effectContexts ...*EffectContext,
) (ModelMaintenanceResult, error) {
	ctx = normalizeMaintenanceContext(ctx)
	if err := ctx.Err(); err != nil {
		return interruptedMaintenanceResult(provider, model), err
	}
	fingerprint := modelMaintenanceFingerprint(provider, model)
	if !modelMaintenanceEnabled() || s.maintenanceHistory == nil {
		return ModelMaintenanceResult{ProviderID: provider.ID, ProviderName: provider.Name, ModelID: model.ID, ModelName: model.Name, Status: "not_enforced", Reason: "daily model maintenance is not configured for this runtime", CheckedAt: time.Now().UTC()}, nil
	}
	if readiness := providerRuntimeReadiness(provider); !readiness.configured {
		return s.recordMaintenance(ModelMaintenanceResult{ProviderID: provider.ID, ProviderName: provider.Name, ModelID: model.ID, ModelName: model.Name, Status: "failed", Reason: "daily model maintenance rejected this runtime endpoint: " + readiness.reason, ConfigurationFingerprint: fingerprint, BlocksExecution: true, CheckedAt: time.Now().UTC()}), nil
	}

	interval := modelMaintenanceInterval()
	now := time.Now().UTC()
	configurationChanged := false
	if latest, err := s.maintenanceHistory.FindLatestModelMaintenance(provider.ID, model.ID); err == nil && latest != nil && latest.ConfigurationFingerprint == fingerprint && maintenanceRecordReusable(*latest, now, interval) {
		result := modelMaintenanceResult(*latest)
		result.Reused = true
		return result, nil
	} else if latest != nil && latest.ConfigurationFingerprint != fingerprint {
		configurationChanged = true
	} else if err != nil {
		return s.recordMaintenance(ModelMaintenanceResult{ProviderID: provider.ID, ProviderName: provider.Name, ModelID: model.ID, ModelName: model.Name, Status: "failed", Reason: "could not read daily model maintenance history", ConfigurationFingerprint: fingerprint, BlocksExecution: true, CheckedAt: time.Now().UTC()}), nil
	}
	if err := ctx.Err(); err != nil {
		return interruptedMaintenanceResult(provider, model), err
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
	if err := lockMaintenanceContext(ctx, lock); err != nil {
		return interruptedMaintenanceResult(provider, model), err
	}
	defer lock.Unlock()

	// Another request may have completed the daily operation while this request
	// waited for the per-model lock.
	now = time.Now().UTC()
	if latest, err := s.maintenanceHistory.FindLatestModelMaintenance(provider.ID, model.ID); err == nil && latest != nil && latest.ConfigurationFingerprint == fingerprint && maintenanceRecordReusable(*latest, now, interval) {
		result := modelMaintenanceResult(*latest)
		result.Reused = true
		return result, nil
	} else if latest != nil && latest.ConfigurationFingerprint != fingerprint {
		configurationChanged = true
	} else if err != nil {
		return s.recordMaintenance(ModelMaintenanceResult{ProviderID: provider.ID, ProviderName: provider.Name, ModelID: model.ID, ModelName: model.Name, Status: "failed", Reason: "could not re-read daily model maintenance history after waiting for the model refresh lock", ConfigurationFingerprint: fingerprint, BlocksExecution: true, CheckedAt: now}), nil
	}
	if err := ctx.Err(); err != nil {
		return interruptedMaintenanceResult(provider, model), err
	}

	var effectContext *EffectContext
	if len(effectContexts) > 0 {
		effectContext = effectContexts[0]
	}
	if provider.ID == "ollama" || provider.ID == miniSWEOllamaProviderID {
		return s.refreshOllamaModelContext(ctx, provider, model, fingerprint, configurationChanged, effectContext)
	}
	return s.verifyManagedModelContext(ctx, provider, model, fingerprint, configurationChanged)
}

func normalizeMaintenanceContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func lockMaintenanceContext(ctx context.Context, lock *sync.Mutex) error {
	ctx = normalizeMaintenanceContext(ctx)
	if lock.TryLock() {
		return nil
	}
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if lock.TryLock() {
				return nil
			}
		}
	}
}

func interruptedMaintenanceResult(provider Provider, model Model) ModelMaintenanceResult {
	return ModelMaintenanceResult{
		ProviderID:      provider.ID,
		ProviderName:    provider.Name,
		ModelID:         model.ID,
		ModelName:       model.Name,
		Status:          "interrupted",
		Reason:          "model maintenance stopped because the caller was cancelled",
		BlocksExecution: true,
		CheckedAt:       time.Now().UTC(),
	}
}

// modelMaintenanceRoutingBlockReason is intentionally read-only. Routing and
// classification may inspect durable maintenance evidence, but they may never
// install or update a model merely to decide which model would be suitable.
// A due or absent record is enforced later at Generate with trusted context.
func (s *Service) modelMaintenanceRoutingBlockReason(provider Provider, model Model) string {
	if !modelMaintenanceEnabled() || s.maintenanceHistory == nil {
		return ""
	}
	latest, err := s.maintenanceHistory.FindLatestModelMaintenance(provider.ID, model.ID)
	if err != nil {
		return "could not read daily model maintenance history"
	}
	if latest == nil || latest.ConfigurationFingerprint != modelMaintenanceFingerprint(provider, model) {
		return ""
	}
	if !maintenanceRecordReusable(*latest, time.Now().UTC(), modelMaintenanceInterval()) {
		return ""
	}
	if latest.BlocksExecution || strings.EqualFold(latest.Status, "failed") {
		return latest.Reason
	}
	return ""
}

func maintenanceStillFresh(checkedAt, now time.Time, interval time.Duration) bool {
	checkedAt = checkedAt.UTC()
	if checkedAt.IsZero() || checkedAt.After(now) {
		return false
	}
	return now.Sub(checkedAt) < interval
}

// maintenanceRecordReusable treats a successful daily check as valid for the
// configured interval. A failed check is deliberately retried sooner: keeping
// a model blocked for a whole day after a temporary local download, network,
// or provider outage would be neither safe nor operationally useful.
func maintenanceRecordReusable(record models.LLMModelMaintenance, now time.Time, interval time.Duration) bool {
	if !maintenanceStillFresh(record.CheckedAt, now, interval) {
		return false
	}
	if !record.BlocksExecution && !strings.EqualFold(record.Status, "failed") {
		return true
	}
	return now.Sub(record.CheckedAt.UTC()) < modelMaintenanceFailureRetryInterval()
}

// verifyManagedModel confirms that a non-Ollama runtime reports the exact
// configured model. HAI intentionally never guesses a download, image pull,
// GGUF replacement, or cloud model-name upgrade. Cloud provider versions are
// managed by the provider; a changed model ID is an explicit operator decision.
func (s *Service) verifyManagedModelContext(ctx context.Context, provider Provider, model Model, fingerprint string, configurationChanged bool) (ModelMaintenanceResult, error) {
	result := ModelMaintenanceResult{ProviderID: provider.ID, ProviderName: provider.Name, ModelID: model.ID, ModelName: model.Name, ConfigurationFingerprint: fingerprint, ConfigurationChanged: configurationChanged, CheckedAt: time.Now().UTC()}
	probe := probeProviderContext(ctx, provider, s.policy)
	if err := ctx.Err(); err != nil {
		return interruptedMaintenanceResult(provider, model), err
	}
	if !probe.Live {
		result.Status = "failed"
		result.Reason = "runtime availability check failed: " + probe.Reason
		result.BlocksExecution = true
		return s.recordMaintenance(result), nil
	}
	if !containsReportedModel(probe.ReportedModelIDs, model.ID) {
		result.Status = "failed"
		result.Reason = "runtime did not report the exact configured model identifier during its daily check"
		result.BlocksExecution = true
		return s.recordMaintenance(result), nil
	}
	if provider.Local {
		result.Status = "current"
		result.Reason = "runtime reported the exact configured model identifier; HAI did not alter the operator-managed installation"
	} else {
		result.Status = "provider_managed"
		result.Reason = "provider reported the exact configured model identifier; provider-managed model versions are not silently replaced by HAI"
	}
	return s.recordMaintenance(result), nil
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

func (s *Service) refreshOllamaModelContext(
	ctx context.Context,
	provider Provider,
	model Model,
	fingerprint string,
	configurationChanged bool,
	effectContext *EffectContext,
) (ModelMaintenanceResult, error) {
	result := ModelMaintenanceResult{ProviderID: provider.ID, ProviderName: provider.Name, ModelID: model.ID, ModelName: model.Name, ConfigurationFingerprint: fingerprint, ConfigurationChanged: configurationChanged, CheckedAt: time.Now().UTC()}
	endpoint := strings.TrimRight(strings.TrimSpace(provider.EndpointURL), "/")
	previousDigest, err := ollamaModelDigestContext(ctx, endpoint, model.ID)
	if requestCancelled(err) {
		return interruptedMaintenanceResult(provider, model), err
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
		return s.recordMaintenance(result), nil
	}
	result.PreviousDigest = previousDigest
	if !validMaintenanceModelID(model.ID) {
		result.Status = "failed"
		result.Reason = "configured Ollama model identifier is invalid for maintenance"
		result.BlocksExecution = true
		return s.recordMaintenance(result), nil
	}

	payload, _ := json.Marshal(map[string]any{"name": model.ID, "stream": false})
	authorization, err := buildFinalEffectAuthorizationRequest(
		EffectOperationModelPull,
		effectContext,
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
		return s.recordMaintenance(result), nil
	}

	result.UpdateAttempted = true
	if err := s.pullOllamaModelContext(ctx, endpoint, model.ID, payload, authorization); err != nil {
		if requestCancelled(err) {
			return interruptedMaintenanceResult(provider, model), err
		}
		result.Status = "failed"
		result.Reason = "Ollama daily refresh failed; this model will not be used until the next successful check: " + safety.RedactSecrets(err.Error())
		result.BlocksExecution = true
		return s.recordMaintenance(result), nil
	}
	currentDigest, err := ollamaModelDigestContext(ctx, endpoint, model.ID)
	if requestCancelled(err) {
		return interruptedMaintenanceResult(provider, model), err
	}
	if err != nil {
		result.Status = "failed"
		result.Reason = "Ollama refresh completed but the installed model could not be verified: " + safety.RedactSecrets(err.Error())
		result.BlocksExecution = true
		return s.recordMaintenance(result), nil
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
	return s.recordMaintenance(result), nil
}

func requestCancelled(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

func (s *Service) recordMaintenance(result ModelMaintenanceResult) ModelMaintenanceResult {
	result.Reason = safety.RedactSecrets(result.Reason)
	if s.maintenanceHistory == nil {
		return result
	}
	record, err := s.maintenanceHistory.RecordModelMaintenance(&models.LLMModelMaintenance{
		ProviderID: result.ProviderID, ProviderName: result.ProviderName, ModelID: result.ModelID, ModelName: result.ModelName,
		Status: result.Status, Reason: result.Reason, PreviousDigest: result.PreviousDigest, CurrentDigest: result.CurrentDigest,
		ConfigurationFingerprint: result.ConfigurationFingerprint, ConfigurationChanged: result.ConfigurationChanged,
		UpdateAttempted: result.UpdateAttempted, UpdateApplied: result.UpdateApplied, BlocksExecution: result.BlocksExecution, CheckedAt: result.CheckedAt,
	})
	if err != nil {
		result.Status = "failed"
		result.Reason = "could not persist daily model maintenance result"
		result.BlocksExecution = true
		return result
	}
	return modelMaintenanceResult(*record)
}

func modelMaintenanceResult(record models.LLMModelMaintenance) ModelMaintenanceResult {
	result := ModelMaintenanceResult{ProviderID: record.ProviderID, ProviderName: record.ProviderName, ModelID: record.ModelID, ModelName: record.ModelName, Status: record.Status, Reason: record.Reason, PreviousDigest: record.PreviousDigest, CurrentDigest: record.CurrentDigest, ConfigurationFingerprint: record.ConfigurationFingerprint, ConfigurationChanged: record.ConfigurationChanged, UpdateAttempted: record.UpdateAttempted, UpdateApplied: record.UpdateApplied, BlocksExecution: record.BlocksExecution, CheckedAt: record.CheckedAt}
	if !record.CheckedAt.IsZero() {
		interval := modelMaintenanceInterval()
		if record.BlocksExecution || strings.EqualFold(record.Status, "failed") {
			interval = modelMaintenanceFailureRetryInterval()
		}
		next := record.CheckedAt.UTC().Add(interval)
		result.NextCheckDueAt = &next
	}
	return result
}

// modelMaintenanceFingerprint deliberately binds a result to the configuration
// that was checked without persisting an endpoint or a secret. The normalized
// endpoint strips harmless OpenAI compatibility suffixes while rejecting URLs
// with credentials, paths, queries, or fragments. A changed endpoint, runtime
// locality, payment mode, or model ID must force a new check immediately.
func modelMaintenanceFingerprint(provider Provider, model Model) string {
	endpoint, err := maintenanceEndpointKey(provider.EndpointURL)
	if err != nil {
		endpoint = "invalid-endpoint"
	}
	adapter := "verify-only"
	if provider.ID == "ollama" || provider.ID == miniSWEOllamaProviderID {
		adapter = "ollama-pull"
	}
	value := strings.Join([]string{
		"v1",
		strings.TrimSpace(provider.ID),
		strings.TrimSpace(model.ID),
		endpoint,
		adapter,
		fmt.Sprintf("local=%t", provider.Local),
		fmt.Sprintf("paid=%t", provider.Paid),
	}, "|")
	return fmt.Sprintf("%x", sha256.Sum256([]byte(value)))
}

func ollamaModelDigest(endpoint, modelID string) (string, error) {
	return ollamaModelDigestContext(context.Background(), endpoint, modelID)
}

func ollamaModelDigestContext(parent context.Context, endpoint, modelID string) (string, error) {
	parent = normalizeMaintenanceContext(parent)
	ctx, cancel := context.WithTimeout(parent, modelMaintenanceTimeout())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"/api/tags", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "018-HAI-Model-Maintenance/1.0")
	response, err := noRedirectHTTPClient().Do(req)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 64*1024))
		return "", fmt.Errorf("Ollama tags returned HTTP %d: %s", response.StatusCode, compactOutput(body, 180))
	}
	var tags ollamaTagsResponse
	if err := json.NewDecoder(io.LimitReader(response.Body, 256*1024)).Decode(&tags); err != nil {
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
	endpoint,
	modelID string,
	payload []byte,
	authorization FinalEffectAuthorizationRequest,
) error {
	return s.pullOllamaModelContext(context.Background(), endpoint, modelID, payload, authorization)
}

func (s *Service) pullOllamaModelContext(
	parent context.Context,
	endpoint,
	modelID string,
	payload []byte,
	authorization FinalEffectAuthorizationRequest,
) error {
	parent = normalizeMaintenanceContext(parent)
	ctx, cancel := context.WithTimeout(parent, modelMaintenanceTimeout())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"/api/pull", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "018-HAI-Model-Maintenance/1.0")
	if err := s.authorizeFinalEffect(ctx, authorization); err != nil {
		return err
	}
	response, err := noRedirectHTTPClient().Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(response.Body, 128*1024))
	if response.StatusCode >= 300 {
		return fmt.Errorf("Ollama pull returned HTTP %d: %s", response.StatusCode, compactOutput(body, 240))
	}
	var pull ollamaPullResponse
	if err := json.Unmarshal(body, &pull); err != nil {
		return fmt.Errorf("decode Ollama pull response: %w", err)
	}
	if strings.TrimSpace(pull.Error) != "" {
		return fmt.Errorf("Ollama pull failed: %s", safety.RedactSecrets(pull.Error))
	}
	if strings.TrimSpace(pull.Status) != "" && !strings.EqualFold(strings.TrimSpace(pull.Status), "success") {
		return fmt.Errorf("Ollama pull finished with status %q", strings.TrimSpace(pull.Status))
	}
	return nil
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
