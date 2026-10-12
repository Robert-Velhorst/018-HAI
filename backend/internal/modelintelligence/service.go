package modelintelligence

import (
	"context"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"automation-hub-backend/internal/safety"
)

// observed accumulates real metrics for a (provider, model) pair.
type observed struct {
	runs      int
	failures  int
	sumTPS    float64
	lastProbe *time.Time
	lastBench *time.Time
	claim     ClaimLevel
}

// Service is the model-intelligence orchestrator. It exposes the registry,
// router, budgets, cache, and telemetry, and runs bounded lane calls that
// record real telemetry. It never fabricates provider state or telemetry.
type Service struct {
	reg                     *Registry
	router                  *Router
	telemetry               *TelemetryStore
	cache                   *Cache
	now                     func() time.Time
	telemetryRepo           TelemetryRepository
	telemetryState          TelemetryPersistenceStatus
	requireDurableTelemetry bool

	mu              sync.Mutex
	budgetDefaults  OperationBudget
	observedByKey   map[string]*observed
	maintenanceGate ModelMaintenanceGate
	// Health sequences prevent slower, older responses from replacing newer health state.
	probeSequence      map[string]uint64
	probeCommitted     map[string]uint64
	benchmarksInFlight map[string]bool
}

type TelemetryPersistenceState string

const (
	TelemetryPersistenceDurable    TelemetryPersistenceState = "durable"
	TelemetryPersistenceMemoryOnly TelemetryPersistenceState = "memory_only"
	TelemetryPersistenceDegraded   TelemetryPersistenceState = "degraded"
)

// TelemetryPersistenceStatus describes whether model-run audit rows survive a restart.
type TelemetryPersistenceStatus struct {
	State   TelemetryPersistenceState `json:"state"`
	Message string                    `json:"message"`
}

// NewService builds a service over a registry.
func NewService(reg *Registry) *Service {
	if reg == nil {
		reg = &Registry{}
	}
	return &Service{
		reg:                reg,
		router:             NewRouter(reg),
		telemetry:          NewTelemetryStore(),
		cache:              NewCache(),
		now:                time.Now,
		budgetDefaults:     DefaultBudget(),
		observedByKey:      map[string]*observed{},
		probeSequence:      map[string]uint64{},
		probeCommitted:     map[string]uint64{},
		benchmarksInFlight: map[string]bool{},
		telemetryState:     TelemetryPersistenceStatus{State: TelemetryPersistenceMemoryOnly, Message: "Model-run history is stored in memory only and will not survive a restart."},
	}
}

// DefaultService builds a service from the environment with durable telemetry
// when a database is available (telemetry survives restart, §18).
func DefaultService() *Service {
	s := NewService(NewRegistryFromEnv())
	s.requireDurableTelemetry = true
	if repo := DefaultTelemetryRepository(); repo != nil {
		s.WithTelemetryRepository(repo)
	}
	return s
}

// WithTelemetryRepository seeds the store from durable telemetry and persists
// every future row. Returns the service for chaining.
func (s *Service) WithTelemetryRepository(repo TelemetryRepository) *Service {
	if repo == nil {
		return s
	}
	s.telemetryRepo = repo
	if rows, err := repo.LoadAll(); err == nil {
		s.telemetry.Replace(rows)
		for _, row := range rows {
			s.observeTelemetry(row)
		}
		s.setTelemetryPersistence(TelemetryPersistenceStatus{State: TelemetryPersistenceDurable, Message: "Model-run history is being saved to durable storage."})
	} else {
		s.setTelemetryPersistence(TelemetryPersistenceStatus{State: TelemetryPersistenceDegraded, Message: "Model-run history could not be loaded from durable storage; real model calls are paused."})
	}
	s.telemetry.SetPersistChecked(func(t ModelRunTelemetry) error { return repo.Save(t) })
	return s
}

// ProviderSummary is a provider's truthful status for the overview.
type ProviderSummary struct {
	ID                             string         `json:"id"`
	Name                           string         `json:"name"`
	Status                         ProviderStatus `json:"status"`
	ClaimLevel                     ClaimLevel     `json:"claimLevel"`
	Local                          bool           `json:"local"`
	EndpointLocal                  bool           `json:"endpointLocal"`
	LocalInferenceOperatorAttested bool           `json:"localInferenceOperatorAttested"`
	BillingStatus                  BillingStatus  `json:"billingStatus"`
	Deterministic                  bool           `json:"deterministic"`
	Models                         int            `json:"models"`
}

// Overview is the model-intelligence dashboard roll-up.
type Overview struct {
	Providers             []ProviderSummary          `json:"providers"`
	Lanes                 []RoutingLane              `json:"lanes"`
	TotalProfiles         int                        `json:"totalProfiles"`
	ActiveModels          int                        `json:"activeModels"`
	DeterministicProfiles int                        `json:"deterministicProfiles"`
	TelemetryRuns         int                        `json:"telemetryRuns"`
	EvaluatedRuns         int                        `json:"evaluatedRuns"`
	AcceptedOutputs       int                        `json:"acceptedOutputs"`
	UnvalidatedRuns       int                        `json:"unvalidatedRuns"`
	CacheHits             int                        `json:"cacheHits"`
	CacheMisses           int                        `json:"cacheMisses"`
	LaneWinners           []LaneWinner               `json:"laneWinners"`
	Calibration           CalibrationSummary         `json:"calibration"`
	TelemetryPersistence  TelemetryPersistenceStatus `json:"telemetryPersistence"`
}

// Overview returns the dashboard roll-up with truthful provider states.
func (s *Service) Overview() Overview {
	profs := s.Profiles()
	byProvider := map[string]*ProviderSummary{}
	order := []string{}
	active := 0
	deterministicProfiles := 0
	for _, prof := range profs {
		ps := byProvider[prof.ProviderID]
		if ps == nil {
			ps = &ProviderSummary{
				ID: prof.ProviderID, Name: prof.DisplayName, Status: prof.Status,
				ClaimLevel: prof.ClaimLevel, Local: prof.Local, EndpointLocal: prof.EndpointLocal,
				LocalInferenceOperatorAttested: prof.LocalInferenceOperatorAttested,
				BillingStatus:                  prof.BillingStatus,
				Deterministic:                  prof.Deterministic,
			}
			byProvider[prof.ProviderID] = ps
			order = append(order, prof.ProviderID)
		}
		ps.Models++
		ps.Deterministic = ps.Deterministic || prof.Deterministic
		if prof.Deterministic {
			deterministicProfiles++
		}
		if prof.Usable() && !prof.Deterministic {
			active++
		}
	}
	providers := make([]ProviderSummary, 0, len(order))
	for _, id := range order {
		providers = append(providers, *byProvider[id])
	}
	hits, misses := s.cache.Stats()
	calibration := s.Calibration()
	return Overview{
		Providers:             providers,
		Lanes:                 allLanes(),
		TotalProfiles:         len(profs),
		ActiveModels:          active,
		DeterministicProfiles: deterministicProfiles,
		TelemetryRuns:         calibration.TotalRuns,
		EvaluatedRuns:         calibration.EvaluatedRuns,
		AcceptedOutputs:       calibration.AcceptedOutputs,
		UnvalidatedRuns:       calibration.UnvalidatedRuns,
		CacheHits:             hits,
		CacheMisses:           misses,
		LaneWinners:           calibration.LaneLeaders,
		Calibration:           calibration,
		TelemetryPersistence:  s.TelemetryPersistence(),
	}
}

// TelemetryPersistence returns the current durable audit-storage state.
func (s *Service) TelemetryPersistence() TelemetryPersistenceStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.telemetryState
}

func (s *Service) setTelemetryPersistence(status TelemetryPersistenceStatus) {
	s.mu.Lock()
	s.telemetryState = status
	s.mu.Unlock()
}

func (s *Service) requireDurableTelemetryFor(profile ModelProfile) error {
	if !s.requireDurableTelemetry || profile.Deterministic {
		return nil
	}
	status := s.TelemetryPersistence()
	if status.State != TelemetryPersistenceDurable {
		return fmt.Errorf("modelintelligence: real inference is paused because durable model-run history is %s", status.State)
	}
	return nil
}

// Cache exposes the cache (for wiring/tests).
func (s *Service) Cache() *Cache { return s.cache }

// Telemetry returns all recorded telemetry.
func (s *Service) Telemetry() []ModelRunTelemetry {
	s.refreshTelemetry()
	return s.telemetry.All()
}

func (s *Service) Calibration() CalibrationSummary {
	s.refreshTelemetry()
	return s.telemetry.Calibration()
}

// LaneWinners returns the fastest observed model per lane.
func (s *Service) LaneWinners() []LaneWinner { return s.Calibration().LaneLeaders }

func (s *Service) refreshTelemetry() {
	if s.telemetryRepo == nil {
		return
	}
	if rows, err := s.telemetryRepo.LoadAll(); err == nil {
		s.telemetry.Replace(rows)
	} else {
		s.setTelemetryPersistence(TelemetryPersistenceStatus{State: TelemetryPersistenceDegraded, Message: "Model-run history could not be refreshed from durable storage."})
	}
}

// Profiles returns every profile merged with observed metrics.
func (s *Service) Profiles() []ModelProfile {
	s.mu.Lock()
	defer s.mu.Unlock()
	profs := s.reg.profilesAt(s.now().UTC())
	for i := range profs {
		if o := s.observedByKey[profs[i].Key()]; o != nil {
			profs[i].ObservedRuns = o.runs
			profs[i].ObservedFailures = o.failures
			if o.runs > 0 {
				profs[i].ObservedTokensPerSecond = o.sumTPS / float64(o.runs)
			}
			profs[i].LastProbedAt = o.lastProbe
			profs[i].LastBenchmarkedAt = o.lastBench
			if o.claim != "" {
				profs[i].ClaimLevel = o.claim
			}
		}
	}
	return profs
}

// Profile returns a single merged profile.
func (s *Service) Profile(providerID, modelID string) (ModelProfile, bool) {
	for _, p := range s.Profiles() {
		if p.ProviderID == providerID && p.ModelID == modelID {
			return p, true
		}
	}
	return ModelProfile{}, false
}

// Probe probes a provider and records the result truthfully.
func (s *Service) Probe(ctx context.Context, providerID string) (ProbeResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	p, ok := s.reg.provider(providerID)
	if !ok {
		return ProbeResult{}, fmt.Errorf("modelintelligence: unknown provider %q", providerID)
	}
	if ctx.Err() != nil {
		return ProbeResult{}, ctx.Err()
	}
	sequence := s.startHealthCheck(providerID)

	now := s.now().UTC()
	res := p.Probe(ctx, now)
	if ctx.Err() != nil {
		return ProbeResult{}, ctx.Err()
	}
	if res.ProviderID != providerID || !res.Status.IsValid() || res.Status == ProviderProbing {
		res = ProbeResult{
			ProviderID: providerID,
			Status:     ProviderFailed,
			Detail:     "provider returned inconsistent probe metadata",
			CheckedAt:  now,
		}
	} else if res.CheckedAt.IsZero() {
		res.CheckedAt = now
	}
	if res.CheckedAt.After(now) || (res.Status == ProviderActive && now.Sub(res.CheckedAt) >= modelProbeMaxAge) || res.ModelsSeen < 0 || res.DurationMs < 0 {
		res = ProbeResult{ProviderID: providerID, Status: ProviderFailed, Detail: "provider returned stale or invalid probe evidence", CheckedAt: now}
	}
	res.Detail = safety.RedactSecrets(res.Detail)
	profiles := p.Profiles()
	s.mu.Lock()
	if sequence > s.probeCommitted[providerID] {
		s.probeCommitted[providerID] = sequence
		for _, prof := range profiles {
			s.reg.recordProbeStatus(prof.Key(), res.Status, res.CheckedAt)
			o := s.ensureObserved(prof.Key())
			t := res.CheckedAt
			o.lastProbe = &t
			if res.Status == ProviderActive && claimRank(o.claim) < claimRank(ClaimProbed) {
				o.claim = ClaimProbed
			}
		}
	}
	s.mu.Unlock()
	return res, nil
}

// BenchmarkResult is the outcome of a bounded benchmark call.
type BenchmarkResult struct {
	ProviderID      string           `json:"providerId"`
	ModelID         string           `json:"modelId"`
	OK              bool             `json:"ok"`
	InputTokens     int              `json:"inputTokens"`
	OutputTokens    int              `json:"outputTokens"`
	UsageSource     TokenUsageSource `json:"usageSource"`
	DurationMs      int64            `json:"durationMs"`
	TokensPerSecond float64          `json:"tokensPerSecond"`
	ClaimLevel      ClaimLevel       `json:"claimLevel"`
	Detail          string           `json:"detail,omitempty"`
}

// Benchmark runs one bounded real call against a model and records telemetry.
// It never fabricates a result: if the provider cannot serve, it returns the
// truthful error and does not promote the claim level.
func (s *Service) Benchmark(ctx context.Context, providerID, modelID string) (BenchmarkResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return BenchmarkResult{}, err
	}
	prof, ok := s.Profile(providerID, modelID)
	if !ok {
		return BenchmarkResult{}, fmt.Errorf("modelintelligence: unknown model %s/%s", providerID, modelID)
	}
	releaseBenchmark, acquired := s.startBenchmark(prof.Key())
	if !acquired {
		return BenchmarkResult{ProviderID: providerID, ModelID: modelID, Detail: "a benchmark for this model is already running", ClaimLevel: prof.ClaimLevel}, nil
	}
	defer releaseBenchmark()
	if err := s.requireDurableTelemetryFor(prof); err != nil {
		return BenchmarkResult{ProviderID: providerID, ModelID: modelID, Detail: err.Error(), ClaimLevel: prof.ClaimLevel}, nil
	}
	benchmarkPrompt := "benchmark"
	limits, err := s.authorizeGeneration(prof, benchmarkPrompt, 128)
	if err != nil {
		return BenchmarkResult{
			ProviderID: providerID,
			ModelID:    modelID,
			OK:         false,
			Detail:     err.Error(),
			ClaimLevel: prof.ClaimLevel,
		}, nil
	}
	if !prof.Local {
		return BenchmarkResult{
			ProviderID: providerID,
			ModelID:    modelID,
			OK:         false,
			Detail:     "external model intelligence benchmarks are disabled; use the canonical LLM policy router so budget, approval, audit, and daily maintenance controls apply",
			ClaimLevel: prof.ClaimLevel,
		}, nil
	}
	p, ok := s.reg.provider(providerID)
	if !ok {
		return BenchmarkResult{}, fmt.Errorf("modelintelligence: provider %q is unavailable", providerID)
	}
	if err := s.ensureModelMaintenance(ctx, p, prof); err != nil {
		if ctx != nil && ctx.Err() != nil {
			return BenchmarkResult{}, ctx.Err()
		}
		return BenchmarkResult{
			ProviderID: providerID,
			ModelID:    modelID,
			OK:         false,
			Detail:     err.Error(),
			ClaimLevel: prof.ClaimLevel,
		}, nil
	}
	now := s.now().UTC()
	lane := LaneFastTriage
	if len(prof.Lanes) > 0 {
		lane = prof.Lanes[0]
	}
	req := InferenceRequest{
		Lane: lane, Prompt: benchmarkPrompt,
		MaxInputTokens: limits.maxInputTokens, MaxOutputTokens: limits.maxOutputTokens,
		RequireReportedOutputUsage: !prof.Deterministic,
		Effort:                     EffortLow,
	}
	if err := validateInferenceRequest(req); err != nil {
		return BenchmarkResult{ProviderID: providerID, ModelID: modelID, Detail: err.Error(), ClaimLevel: prof.ClaimLevel}, nil
	}
	if err := ctx.Err(); err != nil {
		return BenchmarkResult{}, err
	}
	sequence := s.startHealthCheck(providerID)
	res, err := p.Generate(ctx, req, now)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return BenchmarkResult{}, ctxErr
	}
	err = validateProviderInference(req, prof, &res, err)
	if err != nil {
		s.recordInferenceFailure(prof, sequence)
	}
	out := BenchmarkResult{ProviderID: providerID, ModelID: modelID, OK: err == nil}
	if err != nil {
		if telemetryErr := s.recordTelemetry(res, lane, "", false, false, ValidationUnvalidated, "", 0, 0); telemetryErr != nil {
			return BenchmarkResult{ProviderID: providerID, ModelID: modelID, Detail: telemetryErr.Error(), ClaimLevel: prof.ClaimLevel}, nil
		}
		out.Detail = err.Error()
		out.ClaimLevel = prof.ClaimLevel
		return out, nil // truthful non-fatal: benchmark attempted, provider not usable
	}
	if telemetryErr := s.recordTelemetry(res, lane, "", true, false, ValidationUnvalidated, "", 0, 0); telemetryErr != nil {
		return BenchmarkResult{ProviderID: providerID, ModelID: modelID, Detail: telemetryErr.Error(), ClaimLevel: prof.ClaimLevel}, nil
	}
	out.InputTokens = res.InputTokensEstimate
	out.OutputTokens = res.OutputTokensEstimate
	out.UsageSource = TokenUsageEstimated
	if res.InputUsageReported {
		out.InputTokens = res.InputTokensActual
	}
	if res.OutputUsageReported {
		out.OutputTokens = res.OutputTokensActual
	}
	switch {
	case res.InputUsageReported && res.OutputUsageReported:
		out.UsageSource = TokenUsageProviderReported
	case res.InputUsageReported || res.OutputUsageReported:
		out.UsageSource = TokenUsageProviderReportedPartial
	}
	out.DurationMs = res.DurationMs
	out.TokensPerSecond = res.TokensPerSecond
	s.mu.Lock()
	o := s.ensureObserved(prof.Key())
	t := now
	o.lastBench = &t
	if claimRank(o.claim) < claimRank(ClaimBenchmarked) {
		o.claim = ClaimBenchmarked
	}
	out.ClaimLevel = o.claim
	s.mu.Unlock()
	return out, nil
}

// RouteLanes classifies the work into lanes and routes each one.
func (s *Service) RouteLanes(in LaneInput) []RouteDecision {
	now := s.now().UTC()
	lanes := ClassifyLanes(in)
	out := make([]RouteDecision, 0, len(lanes))
	for _, lane := range lanes {
		if lane == LanePrivacyFilter {
			continue // handled by the privacyfilter package, not a model route
		}
		out = append(out, s.router.Route(lane, in, now))
	}
	return out
}

// RunLane routes a lane and, if routable, runs a bounded model call, recording
// telemetry and (when safe) caching the result. Returns the decision and, if a
// call was made, the inference result.
func (s *Service) RunLane(ctx context.Context, lane RoutingLane, in LaneInput, prompt, operationID string) (RouteDecision, *InferenceResult, error) {
	return s.runLane(ctx, lane, in, prompt, operationID, nil)
}

type resultValidator func(InferenceResult) (ValidationStatus, string)

func (s *Service) runLane(
	ctx context.Context,
	lane RoutingLane,
	in LaneInput,
	prompt, operationID string,
	validator resultValidator,
) (RouteDecision, *InferenceResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return RouteDecision{}, nil, err
	}
	now := s.now().UTC()
	dec := s.router.Route(lane, in, now)
	if !dec.Routable {
		return dec, nil, nil
	}
	profile, ok := s.Profile(dec.ProviderID, dec.ModelID)
	if !ok {
		return dec, nil, fmt.Errorf("modelintelligence: selected model %s/%s is unavailable", dec.ProviderID, dec.ModelID)
	}
	if !profile.Usable() {
		return dec, nil, fmt.Errorf("modelintelligence: selected model is no longer active; probe again before inference")
	}
	if err := s.requireDurableTelemetryFor(profile); err != nil {
		return dec, nil, err
	}
	limits, err := s.authorizeGeneration(profile, prompt, 256)
	if err != nil {
		return dec, nil, err
	}
	p, ok := s.reg.provider(dec.ProviderID)
	if !ok {
		return dec, nil, fmt.Errorf("modelintelligence: selected provider %q is unavailable", dec.ProviderID)
	}
	req := InferenceRequest{
		Lane: lane, Prompt: prompt,
		MaxInputTokens: limits.maxInputTokens, MaxOutputTokens: limits.maxOutputTokens,
		RequireReportedOutputUsage: !profile.Deterministic,
		Effort:                     EffortLow,
	}
	if err := validateInferenceRequest(req); err != nil {
		return dec, nil, err
	}
	if err := s.ensureModelMaintenance(ctx, p, profile); err != nil {
		return dec, nil, err
	}
	if err := ctx.Err(); err != nil {
		return dec, nil, err
	}
	sequence := s.startHealthCheck(dec.ProviderID)
	res, err := p.Generate(ctx, req, now)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return dec, nil, ctxErr
	}
	err = validateProviderInference(req, profile, &res, err)
	if err != nil {
		s.recordInferenceFailure(profile, sequence)
	}
	validationStatus, validationMethod := ValidationUnvalidated, ""
	if err == nil && validator != nil {
		validationStatus, validationMethod = validator(res)
	}
	if telemetryErr := s.recordTelemetry(res, lane, operationID, err == nil, false, validationStatus, validationMethod, 0, 0); telemetryErr != nil {
		return dec, nil, telemetryErr
	}
	if err != nil {
		return dec, nil, err
	}
	// Cache the deterministic result (safe: local, not high-risk action here).
	s.cache.Store(CacheDeterministicResult, prompt, res.Output, "", res.OK, in.SafeForCloud, in.HighRisk, now)
	return dec, &res, nil
}

// TriageResult is the fast-triage lane's effect on an operation.
type TriageResult struct {
	Category   string `json:"category"`
	Summary    string `json:"summary"`
	ProviderID string `json:"providerId"`
	ModelID    string `json:"modelId"`
	Routed     bool   `json:"routed"`
}

// Triage runs the fast-triage lane over an operation's text so the lane has a
// real behavioral effect (category + summary written back by the caller). It
// honors the privacy filter via SafeForCloud.
func (s *Service) Triage(ctx context.Context, operationType, title, content string, safeForCloud, highRisk bool, operationID string) (TriageResult, error) {
	in := LaneInput{OperationType: operationType, Title: title, Content: content, SafeForCloud: safeForCloud, HighRisk: highRisk}
	prompt := title + "\n" + content
	dec, res, err := s.runLane(ctx, LaneFastTriage, in, prompt, operationID, func(result InferenceResult) (ValidationStatus, string) {
		_, _, valid := parseTriageOutput(result.Output)
		if !valid {
			return ValidationFailed, "triage_schema_v1"
		}
		return ValidationSchemaValidated, "triage_schema_v1"
	})
	if err != nil {
		return TriageResult{}, err
	}
	if res == nil {
		return TriageResult{Routed: false}, nil
	}
	category, summary, valid := parseTriageOutput(res.Output)
	if !valid {
		return TriageResult{}, fmt.Errorf("modelintelligence: triage output failed triage_schema_v1 validation")
	}
	return TriageResult{Category: category, Summary: summary, ProviderID: dec.ProviderID, ModelID: dec.ModelID, Routed: true}, nil
}

func parseTriageOutput(out string) (string, string, bool) {
	category, summary := "", ""
	for _, part := range strings.Split(out, ";") {
		part = strings.TrimSpace(part)
		if v := strings.TrimPrefix(part, "category="); v != part {
			category = v
		} else if v := strings.TrimPrefix(part, "summary="); v != part {
			summary = v
		}
	}
	return category, summary, category != "" && summary != ""
}

// TokenBudgetDefaults returns the current default budget.
func (s *Service) TokenBudgetDefaults() OperationBudget {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.budgetDefaults
}

// SetTokenBudgetDefaults updates the default budget (validated).
func (s *Service) SetTokenBudgetDefaults(b OperationBudget) (OperationBudget, error) {
	if !b.MaximumReasoning.IsValid() {
		return OperationBudget{}, fmt.Errorf("modelintelligence: invalid reasoning effort %q", b.MaximumReasoning)
	}
	if !b.ContextStrategy.IsValid() {
		return OperationBudget{}, fmt.Errorf("modelintelligence: invalid context strategy %q", b.ContextStrategy)
	}
	if b.MaximumInputTokens <= 0 || b.MaximumOutputTokens <= 0 {
		return OperationBudget{}, fmt.Errorf("modelintelligence: token maxima must be positive")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.budgetDefaults = b
	return s.budgetDefaults, nil
}

func (s *Service) startHealthCheck(providerID string) uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.probeSequence[providerID]++
	return s.probeSequence[providerID]
}

func (s *Service) recordInferenceFailure(selected ModelProfile, sequence uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sequence <= s.probeCommitted[selected.ProviderID] {
		return
	}
	s.probeCommitted[selected.ProviderID] = sequence
	// This is a failed inference, not a fabricated live probe or check time.
	s.reg.recordProbeStatus(selected.Key(), ProviderFailed, time.Time{})
}

// Validate the adapter's success contract before accepting output. Audit the
// attempted route, not an untrusted or missing identity returned by the adapter.
func validateProviderInference(req InferenceRequest, selected ModelProfile, result *InferenceResult, err error) error {
	if result == nil {
		return fmt.Errorf("modelintelligence: provider returned no inference result")
	}
	if err == nil {
		switch {
		case !result.OK || strings.TrimSpace(result.Error) != "":
			err = fmt.Errorf("modelintelligence: provider did not report successful inference")
		case result.ProviderID != selected.ProviderID || result.ModelID != selected.ModelID || result.Lane != req.Lane:
			err = fmt.Errorf("modelintelligence: provider returned inconsistent inference identity")
		case strings.TrimSpace(result.Output) == "":
			err = fmt.Errorf("modelintelligence: provider returned no assistant text")
		case result.DurationMs < 0 || result.TokensPerSecond < 0 || math.IsNaN(result.TokensPerSecond) || math.IsInf(result.TokensPerSecond, 0):
			err = fmt.Errorf("modelintelligence: provider returned invalid inference metrics")
		default:
			err = validateInferenceResult(req, result)
		}
	}
	result.ProviderID, result.ModelID, result.Lane = selected.ProviderID, selected.ModelID, req.Lane
	if result.DurationMs < 0 {
		result.DurationMs = 0
	}
	if result.TokensPerSecond < 0 || math.IsNaN(result.TokensPerSecond) || math.IsInf(result.TokensPerSecond, 0) {
		result.TokensPerSecond = 0
	}
	err = redactModelError(err)
	if err != nil {
		result.OK = false
		result.Error = err.Error()
	}
	return err
}

func (s *Service) recordTelemetry(
	res InferenceResult,
	lane RoutingLane,
	operationID string,
	ok, cacheHit bool,
	validationStatus ValidationStatus,
	validationMethod string,
	estimatedCostEUR float64,
	fallbackDepth int,
) error {
	inputTokens, outputTokens := res.InputTokensEstimate, res.OutputTokensEstimate
	usageSource := TokenUsageEstimated
	invalidUsage := (res.InputUsageReported && res.InputTokensActual < 0) ||
		(res.OutputUsageReported && res.OutputTokensActual < 0)
	if invalidUsage {
		usageSource = TokenUsageProviderReportInvalid
	} else {
		switch {
		case res.InputUsageReported && res.OutputUsageReported:
			inputTokens, outputTokens = res.InputTokensActual, res.OutputTokensActual
			usageSource = TokenUsageProviderReported
		case res.InputUsageReported:
			inputTokens = res.InputTokensActual
			usageSource = TokenUsageProviderReportedPartial
		case res.OutputUsageReported:
			outputTokens = res.OutputTokensActual
			usageSource = TokenUsageProviderReportedPartial
		}
	}
	record, err := s.telemetry.RecordChecked(ModelRunTelemetry{
		ProviderID: res.ProviderID, ModelID: res.ModelID, Lane: lane, OperationID: operationID,
		InputTokens: inputTokens, OutputTokens: outputTokens, UsageSource: usageSource,
		DurationMs: res.DurationMs, TokensPerSecond: res.TokensPerSecond, OK: ok, CacheHit: cacheHit,
		ValidationStatus: validationStatus, ValidationMethod: validationMethod,
		EstimatedCostEUR: estimatedCostEUR, FallbackDepth: fallbackDepth,
		CreatedAt: s.now().UTC(),
	})
	if err != nil {
		s.setTelemetryPersistence(TelemetryPersistenceStatus{State: TelemetryPersistenceDegraded, Message: "Model-run history could not be saved; model output was withheld."})
		return fmt.Errorf("modelintelligence: model-run history could not be saved; inference output was withheld")
	}
	s.observeTelemetry(record)
	return nil
}

func (s *Service) startBenchmark(modelKey string) (func(), bool) {
	s.mu.Lock()
	if s.benchmarksInFlight == nil {
		s.benchmarksInFlight = make(map[string]bool)
	}
	if s.benchmarksInFlight[modelKey] {
		s.mu.Unlock()
		return nil, false
	}
	s.benchmarksInFlight[modelKey] = true
	s.mu.Unlock()
	return func() {
		s.mu.Lock()
		delete(s.benchmarksInFlight, modelKey)
		s.mu.Unlock()
	}, true
}

func (s *Service) observeTelemetry(record ModelRunTelemetry) {
	if record.ProviderID == "" || record.ModelID == "" {
		return
	}
	key := record.ProviderID + "/" + record.ModelID
	s.mu.Lock()
	defer s.mu.Unlock()
	o := s.ensureObserved(key)
	o.runs++
	if !record.OK {
		o.failures++
	}
	o.sumTPS += record.TokensPerSecond
	return
}

// ensureObserved must be called with s.mu held.
func (s *Service) ensureObserved(key string) *observed {
	o := s.observedByKey[key]
	if o == nil {
		o = &observed{}
		s.observedByKey[key] = o
	}
	return o
}

func claimRank(c ClaimLevel) int {
	for i, x := range allClaimLevels() {
		if x == c {
			return i
		}
	}
	return -1
}
