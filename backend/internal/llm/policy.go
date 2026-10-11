package llm

import (
	"automation-hub-backend/internal/modelintelligence"
	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/safety"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

const (
	TierLocal      = "local"
	TierFree       = "free"
	TierCheap      = "cheap"
	TierAcceptable = "acceptable"
	TierHigh       = "high"
	TierPremium    = "premium"
	TierExpensive  = "expensive"
	TierUnknown    = "unknown"
)

var tierRank = map[string]int{
	TierLocal:      0,
	TierFree:       1,
	TierCheap:      2,
	TierAcceptable: 3,
	TierHigh:       4,
	TierPremium:    5,
	TierExpensive:  6,
	TierUnknown:    7,
}

var reasoningRank = map[string]int{
	"low":       1,
	"medium":    2,
	"high":      3,
	"very_high": 4,
}

type Policy struct {
	DailyPaidBudgetEUR               float64                 `json:"dailyPaidBudgetEur"`
	PaidCallsAllowed                 bool                    `json:"paidCallsAllowed"`
	LocalModelsAllowed               bool                    `json:"localModelsAllowed"`
	FreeCloudQuotaAllowed            bool                    `json:"freeCloudQuotaAllowed"`
	LocalFirst                       bool                    `json:"localFirst"`
	CacheRepeatedPrompts             bool                    `json:"cacheRepeatedPrompts"`
	RouteSimpleTasksToSmallModels    bool                    `json:"routeSimpleTasksToSmallModels"`
	RouteComplexTasksToBestFreeModel bool                    `json:"routeComplexTasksToBestAvailableFreeModel"`
	RequireApprovalBeforePaidUsage   bool                    `json:"requireApprovalBeforePaidUsage"`
	RequireRecentLiveProviderProbe   bool                    `json:"requireRecentLiveProviderProbe"`
	ProviderProbeMaxAgeSeconds       int                     `json:"providerProbeMaxAgeSeconds"`
	TierOrder                        []string                `json:"tierOrder"`
	DailyBudgetUsedEUR               float64                 `json:"dailyBudgetUsedEur"`
	InputTokensUsed                  int                     `json:"inputTokensUsed"`
	OutputTokensUsed                 int                     `json:"outputTokensUsed"`
	UsageAccountingStatus            string                  `json:"usageAccountingStatus"`
	UsagePeriodStart                 time.Time               `json:"usagePeriodStart"`
	UsageTimezone                    string                  `json:"usageTimezone"`
	Providers                        []Provider              `json:"providers"`
	InferenceInfrastructure          InferenceInfrastructure `json:"inferenceInfrastructure"`
}

type InferenceInfrastructure struct {
	KVCacheLoadStrategy             string `json:"kvCacheLoadStrategy"`
	DisaggregatedServingVerified    bool   `json:"disaggregatedServingVerified"`
	DualPathInfrastructureAvailable bool   `json:"dualPathInfrastructureAvailable"`
	Reason                          string `json:"reason"`
}

type Provider struct {
	ID                    string  `json:"id"`
	Name                  string  `json:"name"`
	Enabled               bool    `json:"enabled"`
	Local                 bool    `json:"local"`
	Paid                  bool    `json:"paid"`
	EndpointURL           string  `json:"endpointUrl,omitempty"`
	APIKeyEnv             string  `json:"apiKeyEnv,omitempty"`
	Configured            bool    `json:"configured"`
	ReadinessStatus       string  `json:"readinessStatus,omitempty"`
	ReadinessReason       string  `json:"readinessReason,omitempty"`
	QuotaRemaining        int     `json:"quotaRemaining"`
	DailyBudgetEUR        float64 `json:"dailyBudgetEur"`
	BudgetUsedEUR         float64 `json:"budgetUsedEur"`
	InputTokensUsed       int     `json:"inputTokensUsed"`
	OutputTokensUsed      int     `json:"outputTokensUsed"`
	UsageAccountingStatus string  `json:"usageAccountingStatus"`
	Models                []Model `json:"models"`
}

type Model struct {
	ID                            string   `json:"id"`
	Name                          string   `json:"name"`
	Tier                          string   `json:"tier"`
	Capabilities                  []string `json:"capabilities"`
	MaxDifficulty                 int      `json:"maxDifficulty"`
	MaxReasoning                  string   `json:"maxReasoning"`
	EstimatedCostEUR              float64  `json:"estimatedCostEur"`
	InputCostPerMillionTokensEUR  float64  `json:"inputCostPerMillionTokensEur"`
	OutputCostPerMillionTokensEUR float64  `json:"outputCostPerMillionTokensEur"`
	PricingUnit                   string   `json:"pricingUnit,omitempty"`
	PricingSource                 string   `json:"pricingSource,omitempty"`
	RequiresApproval              bool     `json:"requiresApproval"`
	Enabled                       bool     `json:"enabled"`
	BudgetUsedEUR                 float64  `json:"budgetUsedEur"`
	InputTokensUsed               int      `json:"inputTokensUsed"`
	OutputTokensUsed              int      `json:"outputTokensUsed"`
	UsageAccountingStatus         string   `json:"usageAccountingStatus"`
}

type RouteRequest struct {
	Task                      string `json:"task"`
	TaskType                  string `json:"taskType,omitempty"`
	Difficulty                int    `json:"difficulty,omitempty"`
	RequiredReasoning         string `json:"requiredReasoning,omitempty"`
	ValidationPassed          *bool  `json:"validationPassed,omitempty"`
	PreviousModelID           string `json:"previousModelId,omitempty"`
	excludedMaintenanceModels map[string]struct{}
}

type TaskClassification struct {
	TaskType             string   `json:"taskType"`
	Difficulty           int      `json:"difficulty"`
	RequiredReasoning    string   `json:"requiredReasoning"`
	RequiredCapabilities []string `json:"requiredCapabilities"`
	Reason               string   `json:"reason"`
}

type RouteDecision struct {
	SelectedProviderID    string             `json:"selectedProviderId"`
	SelectedModelID       string             `json:"selectedModelId"`
	SelectedModelName     string             `json:"selectedModelName"`
	Tier                  string             `json:"tier"`
	Reason                string             `json:"reason"`
	EstimatedCostEUR      float64            `json:"estimatedCostEur"`
	EstimatedInputTokens  int                `json:"estimatedInputTokens"`
	EstimatedOutputTokens int                `json:"estimatedOutputTokens"`
	PricingSource         string             `json:"pricingSource,omitempty"`
	RequiresApproval      bool               `json:"requiresApproval"`
	Classification        TaskClassification `json:"classification"`
	FallbackPath          []FallbackOption   `json:"fallbackPath"`
	Skipped               []SkippedModel     `json:"skipped"`
	Calibration           *RouteCalibration  `json:"calibration,omitempty"`
	LoggedAt              time.Time          `json:"loggedAt"`
}

// RouteCalibration is the bounded, redacted outcome evidence used by a route.
// It reports validator acceptance, not model truth or provider marketing data.
type RouteCalibration struct {
	Lane             string  `json:"lane"`
	EvaluatedRuns    int     `json:"evaluatedRuns"`
	AcceptedOutputs  int     `json:"acceptedOutputs"`
	AcceptanceRate   float64 `json:"acceptanceRate"`
	WilsonLowerBound float64 `json:"wilsonLowerBound"`
	Confidence       string  `json:"confidence"`
}

type GenerateRequest struct {
	Task          string         `json:"task"`
	SystemPrompt  string         `json:"systemPrompt,omitempty"`
	Context       []string       `json:"context,omitempty"`
	RouteRequest  *RouteRequest  `json:"routeRequest,omitempty"`
	RouteDecision *RouteDecision `json:"routeDecision,omitempty"`
	// EffectContext must be populated by trusted server-side composition from
	// authenticated task state. It is never accepted from public JSON.
	EffectContext *EffectContext `json:"-"`
	// AllowPaidApproved is retained for wire compatibility but no longer grants
	// authority. Paid execution requires an approval source in EffectContext
	// that the injected final-effect authorizer validates.
	AllowPaidApproved bool    `json:"allowPaidApproved,omitempty"`
	Temperature       float64 `json:"temperature,omitempty"`
	MaxTokens         int     `json:"maxTokens,omitempty"`
	// OperationID and FallbackDepth are trusted orchestration metadata. Public
	// callers cannot bind them from JSON.
	OperationID   string `json:"-"`
	FallbackDepth int    `json:"-"`
	// CancellationContext is supplied by trusted server composition and is not
	// accepted from or serialized to public JSON. It bounds probes, maintenance,
	// authorization, and provider calls to the lifetime of their caller.
	CancellationContext context.Context `json:"-"`
}

type GenerationResult struct {
	GenerationID          string    `json:"generationId,omitempty"`
	TelemetryID           string    `json:"telemetryId,omitempty"`
	ProviderID            string    `json:"providerId"`
	ModelID               string    `json:"modelId"`
	ModelName             string    `json:"modelName"`
	Tier                  string    `json:"tier"`
	Output                string    `json:"output,omitempty"`
	Status                string    `json:"status"`
	Reason                string    `json:"reason"`
	EstimatedCostEUR      float64   `json:"estimatedCostEur"`
	InputTokens           int       `json:"inputTokens"`
	OutputTokens          int       `json:"outputTokens"`
	UsageSource           string    `json:"usageSource"`
	AuditStatus           string    `json:"auditStatus"`
	DurationMs            int64     `json:"durationMs"`
	FallbackPath          []string  `json:"fallbackPath"`
	FallbackDepth         int       `json:"fallbackDepth"`
	ValidationStatus      string    `json:"validationStatus"`
	ValidationMethod      string    `json:"validationMethod,omitempty"`
	CalibrationAudit      string    `json:"calibrationAudit"`
	LoggedAt              time.Time `json:"loggedAt"`
	paidBudgetReservation bool
}

// providerUsage is intentionally limited to aggregate counts. HAI does not
// persist prompts, completions, or provider response payloads as usage data.
type providerUsage struct {
	InputTokens  int
	OutputTokens int
	HasInput     bool
	HasOutput    bool
}

func (usage providerUsage) source() string {
	switch {
	case usage.HasInput && usage.HasOutput:
		return "provider_reported"
	case usage.HasInput || usage.HasOutput:
		return "provider_reported_partial"
	default:
		return "estimated"
	}
}

type ProviderProbeResult struct {
	ProviderID       string     `json:"providerId"`
	ProviderName     string     `json:"providerName"`
	Status           string     `json:"status"`
	Reason           string     `json:"reason"`
	EndpointURL      string     `json:"endpointUrl,omitempty"`
	HTTPStatus       int        `json:"httpStatus,omitempty"`
	ModelsSeen       int        `json:"modelsSeen"`
	DurationMs       int64      `json:"durationMs"`
	Live             bool       `json:"live"`
	RequiresReview   bool       `json:"requiresReview"`
	CheckedAt        time.Time  `json:"checkedAt"`
	LastSuccessfulAt *time.Time `json:"lastSuccessfulAt,omitempty"`
	// ReportedModelIDs is transient evidence used by the model-maintenance
	// gate. It is not returned or persisted because the configured model ID is
	// already sufficient operator context for a maintenance decision.
	ReportedModelIDs []string `json:"-"`
}

type FallbackOption struct {
	ProviderID            string  `json:"providerId"`
	ModelID               string  `json:"modelId"`
	ModelName             string  `json:"modelName"`
	Tier                  string  `json:"tier"`
	EstimatedCostEUR      float64 `json:"estimatedCostEur"`
	EstimatedInputTokens  int     `json:"estimatedInputTokens"`
	EstimatedOutputTokens int     `json:"estimatedOutputTokens"`
	RequiresApproval      bool    `json:"requiresApproval"`
}

type SkippedModel struct {
	ProviderID string `json:"providerId"`
	ModelID    string `json:"modelId"`
	Reason     string `json:"reason"`
}

type Service struct {
	policy                           Policy
	mu                               sync.Mutex
	logs                             []RouteDecision
	usage                            map[string]UsageCounter
	usagePeriodStart                 time.Time
	now                              func() time.Time
	probeHistory                     ProbeHistoryRepository
	maintenanceHistory               ModelMaintenanceRepository
	generationHistory                GenerationHistoryRepository
	modelTelemetry                   modelintelligence.TelemetryRepository
	finalEffectAuthorizer            FinalEffectAuthorizer
	emergencyStop                    EmergencyStopEvaluator
	maintenanceEffectContext         *EffectContext
	maintenanceMu                    sync.Mutex
	maintenanceRunning               map[string]*sync.Mutex
	maintenanceCooldowns             map[modelMaintenanceCooldownKey]modelMaintenanceCooldown
	maintenanceCooldownOverflowUntil time.Time
	maintenanceWake                  chan struct{}
	maintenanceAttemptSequence       uint64
}

// WithModelTelemetryRepository connects actual routed generations to the same
// durable outcome ledger used by Model Intelligence. Direct generations remain
// unvalidated until a trusted task validator records their outcome.
func (s *Service) WithModelTelemetryRepository(repo modelintelligence.TelemetryRepository) *Service {
	s.modelTelemetry = repo
	return s
}

type UsageCounter struct {
	BudgetUsedEUR    float64
	InputTokensUsed  int
	OutputTokensUsed int
}

func NewServiceFromEnv() (*Service, error) {
	return newServiceFromEnv(nil, nil, nil)
}

// NewServiceFromEnvWithProbeHistory keeps live provider probing and durable
// readiness evidence together for the API process. Other in-process users can
// still construct a policy-only service without opening a database connection.
func NewServiceFromEnvWithProbeHistory(probeHistory ProbeHistoryRepository) (*Service, error) {
	return newServiceFromEnv(probeHistory, nil, nil)
}

// NewServiceFromEnvWithHistories wires the optional, durable provider-readiness
// and per-model maintenance histories into one router service.
func NewServiceFromEnvWithHistories(probeHistory ProbeHistoryRepository, maintenanceHistory ModelMaintenanceRepository) (*Service, error) {
	return newServiceFromEnv(probeHistory, maintenanceHistory, nil)
}

// NewServiceFromEnvWithOperationalHistories keeps readiness, local model
// maintenance, and redacted generation evidence durable for the API process.
func NewServiceFromEnvWithOperationalHistories(probeHistory ProbeHistoryRepository, maintenanceHistory ModelMaintenanceRepository, generationHistory GenerationHistoryRepository) (*Service, error) {
	return newServiceFromEnv(probeHistory, maintenanceHistory, generationHistory)
}

func newServiceFromEnv(probeHistory ProbeHistoryRepository, maintenanceHistory ModelMaintenanceRepository, generationHistory GenerationHistoryRepository) (*Service, error) {
	policy := defaultPolicy()
	if raw := strings.TrimSpace(os.Getenv("LLM_PROVIDERS_JSON")); raw != "" {
		var providers []Provider
		if err := json.Unmarshal([]byte(raw), &providers); err != nil {
			return nil, fmt.Errorf("invalid LLM_PROVIDERS_JSON: %w", err)
		}
		policy.Providers = providers
	}

	if raw := strings.TrimSpace(os.Getenv("LLM_POLICY_JSON")); raw != "" {
		if err := json.Unmarshal([]byte(raw), &policy); err != nil {
			return nil, fmt.Errorf("invalid LLM_POLICY_JSON: %w", err)
		}
	}

	policy = annotateInfrastructure(annotatePolicyReadiness(normalizeProbePolicy(policy)))
	now := time.Now
	return &Service{
		policy: policy, logs: []RouteDecision{}, usage: map[string]UsageCounter{},
		usagePeriodStart: utcDayStart(now()), now: now,
		probeHistory: probeHistory, maintenanceHistory: maintenanceHistory,
		generationHistory: generationHistory, emergencyStop: processEmergencyStopEvaluator{},
		maintenanceRunning: map[string]*sync.Mutex{}, maintenanceWake: make(chan struct{}, 1),
	}, nil
}

func (s *Service) Policy() Policy {
	return s.annotateUsage(annotateInfrastructure(annotatePolicyReadiness(normalizeProbePolicy(s.policy))))
}

func annotateInfrastructure(policy Policy) Policy {
	strategy := strings.ToLower(strings.TrimSpace(os.Getenv("LLM_KV_CACHE_LOAD_STRATEGY")))
	switch strategy {
	case "traditional", "storage_to_prefill", "storage_to_decode", "dual", "auto":
	default:
		strategy = "disabled"
	}
	verified := envEnabled("LLM_DUALPATH_INFRASTRUCTURE_VERIFIED")
	available := verified && (strategy == "dual" || strategy == "auto")
	reason := "DualPath is disabled; ordinary local model servers use their native KV-cache path."
	if strategy != "disabled" && !verified {
		reason = "KV-cache strategy is configured as a capability hint, but disaggregated prefill/decode, shared storage, transfer network, and scheduler infrastructure are not verified."
	}
	if available {
		reason = "Operator reports verified DualPath-compatible disaggregated serving infrastructure; provider/runtime telemetry must still confirm the selected data path."
	}
	policy.InferenceInfrastructure = InferenceInfrastructure{
		KVCacheLoadStrategy:             strategy,
		DisaggregatedServingVerified:    verified,
		DualPathInfrastructureAvailable: available,
		Reason:                          reason,
	}
	return policy
}

func envEnabled(name string) bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv(name)))
	return value == "1" || value == "true" || value == "yes"
}

func (s *Service) Logs() []RouteDecision {
	s.mu.Lock()
	defer s.mu.Unlock()

	copied := make([]RouteDecision, len(s.logs))
	copy(copied, s.logs)
	return copied
}

// GenerationHistory returns redacted generation evidence. The returned records
// never contain task text, prompts, model output, source content, credentials,
// or raw provider payloads.
func (s *Service) GenerationHistory(limit int) ([]GenerationResult, error) {
	if s.generationHistory == nil {
		return []GenerationResult{}, nil
	}
	records, err := s.generationHistory.FindRecentGenerations(limit)
	if err != nil {
		return nil, fmt.Errorf("load generation history: %w", err)
	}
	validationByID := map[string]modelintelligence.ModelRunTelemetry{}
	if s.modelTelemetry != nil {
		if telemetry, loadErr := s.modelTelemetry.LoadAll(); loadErr == nil {
			for _, record := range telemetry {
				validationByID[record.ID] = record
			}
		}
	}
	results := make([]GenerationResult, 0, len(records))
	for _, record := range records {
		fallbackPath := []string{}
		if strings.TrimSpace(record.FallbackPathJSON) != "" {
			_ = json.Unmarshal([]byte(record.FallbackPathJSON), &fallbackPath)
		}
		telemetryID := generationTelemetryID(record.ID.String())
		result := GenerationResult{
			GenerationID:     record.ID.String(),
			TelemetryID:      telemetryID,
			ProviderID:       record.ProviderID,
			ModelID:          record.ModelID,
			ModelName:        record.ModelName,
			Tier:             record.Tier,
			Status:           record.Status,
			Reason:           record.Reason,
			EstimatedCostEUR: record.EstimatedCostEUR,
			InputTokens:      record.InputTokens,
			OutputTokens:     record.OutputTokens,
			UsageSource:      record.UsageSource,
			AuditStatus:      "recorded",
			DurationMs:       record.DurationMs,
			FallbackPath:     fallbackPath,
			LoggedAt:         record.LoggedAt,
			ValidationStatus: string(modelintelligence.ValidationUnvalidated),
			CalibrationAudit: "not_recorded",
		}
		if telemetry, ok := validationByID[telemetryID]; ok {
			result.ValidationStatus = string(telemetry.ValidationStatus)
			result.ValidationMethod = telemetry.ValidationMethod
			result.FallbackDepth = telemetry.FallbackDepth
			result.CalibrationAudit = "recorded"
		}
		results = append(results, result)
	}
	return results, nil
}

func (s *Service) recordGeneration(result *GenerationResult, request GenerateRequest) {
	if result == nil {
		return
	}
	generationUUID, parseErr := uuid.Parse(result.GenerationID)
	if parseErr != nil {
		generationUUID = uuid.New()
		result.GenerationID = generationUUID.String()
	}
	result.TelemetryID = generationTelemetryID(result.GenerationID)
	result.FallbackDepth = boundedFallbackDepth(request.FallbackDepth)
	result.ValidationStatus = string(modelintelligence.ValidationUnvalidated)
	result.CalibrationAudit = "not_configured"
	if s.modelTelemetry != nil {
		if strings.TrimSpace(result.ProviderID) == "" || strings.TrimSpace(result.ModelID) == "" {
			if result.Status == "completed" {
				withholdUnrecordedModelOutput(result)
				result.CalibrationAudit = "record_failed"
			}
		} else {
			classification := classifyTask(RouteRequest{Task: request.Task})
			if request.RouteRequest != nil {
				classification = classifyTask(*request.RouteRequest)
			}
			if request.RouteDecision != nil && request.RouteDecision.SelectedModelID != "" {
				classification = request.RouteDecision.Classification
			}
			tokensPerSecond := 0.0
			if result.UsageSource == "provider_reported" && result.DurationMs > 0 && result.OutputTokens > 0 {
				tokensPerSecond = float64(result.OutputTokens) / (float64(result.DurationMs) / 1000)
			}
			if err := s.modelTelemetry.Save(modelintelligence.ModelRunTelemetry{
				ID: result.TelemetryID, ProviderID: result.ProviderID, ModelID: result.ModelID,
				Lane: routingLaneForClassification(classification), OperationID: strings.TrimSpace(request.OperationID),
				InputTokens: result.InputTokens, OutputTokens: result.OutputTokens,
				UsageSource: modelintelligence.TokenUsageSource(result.UsageSource),
				DurationMs:  result.DurationMs, TokensPerSecond: tokensPerSecond,
				OK: result.Status == "completed", ValidationStatus: modelintelligence.ValidationUnvalidated,
				EstimatedCostEUR: result.EstimatedCostEUR, FallbackDepth: result.FallbackDepth,
				CreatedAt: result.LoggedAt,
			}); err != nil {
				result.CalibrationAudit = "record_failed"
				if result.Status == "completed" {
					withholdUnrecordedModelOutput(result)
				}
			} else {
				result.CalibrationAudit = "recorded"
			}
		}
	}
	if s.generationHistory == nil {
		result.AuditStatus = "not_configured"
		return
	}
	fallbackPath, err := json.Marshal(result.FallbackPath)
	if err != nil {
		result.AuditStatus = "record_failed"
		if result.Status == "completed" {
			withholdUnrecordedModelOutput(result)
		}
		return
	}
	record := &models.LLMGenerationRecord{
		ID:               generationUUID,
		ProviderID:       result.ProviderID,
		ModelID:          result.ModelID,
		ModelName:        result.ModelName,
		Tier:             result.Tier,
		Status:           result.Status,
		Reason:           safety.RedactSecrets(result.Reason),
		EstimatedCostEUR: result.EstimatedCostEUR,
		InputTokens:      result.InputTokens,
		OutputTokens:     result.OutputTokens,
		UsageSource:      result.UsageSource,
		DurationMs:       result.DurationMs,
		FallbackPathJSON: string(fallbackPath),
		LoggedAt:         result.LoggedAt,
	}
	if result.paidBudgetReservation {
		repository, ok := s.generationHistory.(PaidGenerationBudgetRepository)
		if !ok {
			result.AuditStatus = "record_failed"
			if result.Status == "completed" {
				withholdUnrecordedModelOutput(result)
			}
			return
		}
		err = repository.FinalizePaidGeneration(record)
	} else {
		_, err = s.generationHistory.RecordGeneration(record)
	}
	if err != nil {
		result.AuditStatus = "record_failed"
		if result.Status == "completed" {
			withholdUnrecordedModelOutput(result)
		}
		return
	}
	result.AuditStatus = "recorded"
}

func withholdUnrecordedModelOutput(result *GenerationResult) {
	if result == nil {
		return
	}
	result.Output = ""
	result.Status = "failed"
	result.Reason = "generation audit evidence could not be durably recorded; model output was withheld"
}

func generationTelemetryID(generationID string) string {
	return "llm-generation:" + strings.TrimSpace(generationID)
}

func boundedFallbackDepth(depth int) int {
	if depth < 0 {
		return 0
	}
	if depth > 32 {
		return 32
	}
	return depth
}

// RecordGenerationValidation attaches a trusted validator outcome to a routed
// generation. It cannot be called successfully for unknown or synthetic rows.
func (s *Service) RecordGenerationValidation(telemetryID, status, method string) error {
	if s.modelTelemetry == nil {
		return fmt.Errorf("model outcome telemetry is not configured")
	}
	parsed := modelintelligence.ValidationStatus(strings.TrimSpace(status))
	switch parsed {
	case modelintelligence.ValidationSchemaValidated,
		modelintelligence.ValidationSourceSupported,
		modelintelligence.ValidationTestPassed,
		modelintelligence.ValidationHumanApproved,
		modelintelligence.ValidationVerified,
		modelintelligence.ValidationFailed,
		modelintelligence.ValidationNeedsReview:
	default:
		return fmt.Errorf("unsupported model validation status %q", status)
	}
	return s.modelTelemetry.UpdateValidation(telemetryID, parsed, method)
}

func (s *Service) ProbeProviders() []ProviderProbeResult {
	return s.ProbeProvidersWithContext(context.Background())
}

func (s *Service) ProbeProvidersWithContext(ctx context.Context) []ProviderProbeResult {
	if ctx == nil {
		ctx = context.Background()
	}
	policy := s.Policy()
	return s.probeProvidersWithPolicy(ctx, policy, policy.Providers)
}

func (s *Service) probeProvidersWithPolicy(ctx context.Context, policy Policy, providers []Provider) []ProviderProbeResult {
	if ctx == nil {
		ctx = context.Background()
	}
	results := []ProviderProbeResult{}
	for _, provider := range providers {
		if ctx.Err() != nil {
			break
		}
		result := probeProviderWithContext(ctx, provider, policy)
		results = append(results, result)
		if ctx.Err() != nil {
			break
		}
	}
	return results
}

// ProbeAndRecordProviders executes the same bounded live checks as
// ProbeProviders and persists only redacted readiness evidence when a history
// repository is configured.
func (s *Service) ProbeAndRecordProviders() ([]ProviderProbeResult, error) {
	return s.ProbeAndRecordProvidersWithContext(context.Background())
}

func (s *Service) ProbeAndRecordProvidersWithContext(ctx context.Context) ([]ProviderProbeResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	policy := s.Policy()
	return s.probeAndRecordProvidersWithPolicy(ctx, policy, policy.Providers)
}

func (s *Service) probeAndRecordProvidersWithPolicy(ctx context.Context, policy Policy, providers []Provider) ([]ProviderProbeResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	results := s.probeProvidersWithPolicy(ctx, policy, providers)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.probeHistory == nil {
		return results, nil
	}
	recorded := make([]ProviderProbeResult, 0, len(results))
	for _, result := range results {
		probe, err := recordProviderProbeWithContext(ctx, s.probeHistory, providerProbeRecord(result))
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, fmt.Errorf("record provider probe for %s: %w", result.ProviderID, err)
		}
		recorded = append(recorded, providerProbeResult(*probe))
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return recorded, nil
}

func (s *Service) ProviderProbeHistory(limit int) ([]ProviderProbeResult, error) {
	return s.ProviderProbeHistoryWithContext(context.Background(), limit)
}

func (s *Service) ProviderProbeHistoryWithContext(ctx context.Context, limit int) ([]ProviderProbeResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.probeHistory == nil {
		return []ProviderProbeResult{}, nil
	}
	probes, err := findRecentProviderProbesWithContext(ctx, s.probeHistory, limit)
	if err != nil {
		return nil, fmt.Errorf("load provider probe history: %w", err)
	}
	results := make([]ProviderProbeResult, 0, len(probes))
	for _, probe := range probes {
		results = append(results, providerProbeResult(probe))
	}
	return results, nil
}

func providerProbeRecord(result ProviderProbeResult) *models.LLMProviderProbe {
	return &models.LLMProviderProbe{
		ProviderID:       result.ProviderID,
		ProviderName:     result.ProviderName,
		Status:           result.Status,
		Reason:           safety.RedactSecrets(result.Reason),
		EndpointURL:      safety.RedactURL(result.EndpointURL),
		HTTPStatus:       result.HTTPStatus,
		ModelsSeen:       result.ModelsSeen,
		DurationMs:       result.DurationMs,
		Live:             result.Live,
		RequiresReview:   result.RequiresReview,
		CheckedAt:        result.CheckedAt,
		LastSuccessfulAt: result.LastSuccessfulAt,
	}
}

func providerProbeResult(probe models.LLMProviderProbe) ProviderProbeResult {
	return ProviderProbeResult{
		ProviderID:       probe.ProviderID,
		ProviderName:     probe.ProviderName,
		Status:           probe.Status,
		Reason:           probe.Reason,
		EndpointURL:      probe.EndpointURL,
		HTTPStatus:       probe.HTTPStatus,
		ModelsSeen:       probe.ModelsSeen,
		DurationMs:       probe.DurationMs,
		Live:             probe.Live,
		RequiresReview:   probe.RequiresReview,
		CheckedAt:        probe.CheckedAt,
		LastSuccessfulAt: probe.LastSuccessfulAt,
	}
}

func (s *Service) Route(request RouteRequest) (RouteDecision, error) {
	return s.RouteWithContext(context.Background(), request)
}

func (s *Service) RouteWithContext(ctx context.Context, request RouteRequest) (RouteDecision, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return RouteDecision{}, err
	}
	classification := classifyTask(request)
	candidates, skipped := s.candidatesWithContext(ctx, classification, request)
	if err := ctx.Err(); err != nil {
		return RouteDecision{}, err
	}
	estimatedInputTokens := estimateTokens(request.Task)
	estimatedOutputTokens := estimateRouteOutputTokens(classification)
	for len(candidates) > 0 {
		blockReason, err := s.modelMaintenanceRoutingBlockReasonWithContext(ctx, candidates[0].provider, candidates[0].model)
		if err != nil {
			return RouteDecision{}, err
		}
		if blockReason == "" {
			break
		}
		skipped = append(skipped, SkippedModel{ProviderID: candidates[0].provider.ID, ModelID: candidates[0].model.ID, Reason: "daily model maintenance blocked this model: " + blockReason})
		candidates = candidates[1:]
	}
	if len(candidates) == 0 {
		if err := ctx.Err(); err != nil {
			return RouteDecision{}, err
		}
		decision := RouteDecision{
			Reason:                "No enabled model satisfies the task, budget, quota, approval, and daily maintenance policy.",
			EstimatedInputTokens:  estimatedInputTokens,
			EstimatedOutputTokens: estimatedOutputTokens,
			Classification:        classification,
			Skipped:               skipped,
			LoggedAt:              time.Now().UTC(),
		}
		s.addLog(decision)
		return decision, nil
	}

	selected := candidates[0]
	estimatedCostEUR := estimateRouteCostEUR(selected.model, estimatedInputTokens, estimatedOutputTokens)
	decision := RouteDecision{
		SelectedProviderID:    selected.provider.ID,
		SelectedModelID:       selected.model.ID,
		SelectedModelName:     selected.model.Name,
		Tier:                  selected.model.Tier,
		Reason:                selectionReason(selected, classification, estimatedInputTokens, estimatedOutputTokens, estimatedCostEUR),
		EstimatedCostEUR:      estimatedCostEUR,
		EstimatedInputTokens:  estimatedInputTokens,
		EstimatedOutputTokens: estimatedOutputTokens,
		PricingSource:         selected.model.PricingSource,
		RequiresApproval:      selected.model.RequiresApproval || selected.provider.Paid,
		Classification:        classification,
		FallbackPath:          fallbackPath(candidates[1:], estimatedInputTokens, estimatedOutputTokens),
		Skipped:               skipped,
		LoggedAt:              time.Now().UTC(),
	}
	if selected.calibration != nil {
		decision.Calibration = routeCalibration(selected.calibration)
		decision.Reason += fmt.Sprintf(
			" Historical validator evidence: %d/%d accepted, conservative lower bound %.1f%% (%s confidence).",
			selected.calibration.AcceptedOutputs,
			selected.calibration.EvaluatedRuns,
			selected.calibration.WilsonLowerBound*100,
			selected.calibration.Confidence,
		)
	} else {
		decision.Reason += " No evaluated historical output exists for this model and lane; the result must remain unvalidated until checked."
	}
	if err := ctx.Err(); err != nil {
		return RouteDecision{}, err
	}

	s.addLog(decision)

	return decision, nil
}

func (s *Service) Generate(request GenerateRequest) (result *GenerationResult, err error) {
	started := time.Now().UTC()
	requestContext := request.CancellationContext
	if requestContext == nil {
		requestContext = context.Background()
	}
	var paidReservationID uuid.UUID
	var reservedCostEUR float64
	var reservedInputTokens, reservedOutputTokens int
	defer func() {
		if result != nil && paidReservationID != uuid.Nil {
			result.GenerationID = paidReservationID.String()
			result.paidBudgetReservation = true
			if result.Status == "failed" && result.UsageSource == "" {
				result.EstimatedCostEUR = reservedCostEUR
				result.InputTokens = reservedInputTokens
				result.OutputTokens = reservedOutputTokens
				result.UsageSource = "estimated_uncertain"
			}
		}
		s.recordGeneration(result, request)
	}()
	if err := requestContext.Err(); err != nil {
		return cancelledGenerationResultForDecision(started, request.RouteDecision), nil
	}
	stopState, stopErr := s.evaluateEmergencyStop(requestContext)
	if requestContext.Err() != nil {
		return cancelledGenerationResultForDecision(started, request.RouteDecision), nil
	}
	if stopErr != nil {
		return &GenerationResult{
			Status:     "blocked",
			Reason:     "emergency-stop state is unavailable",
			DurationMs: time.Since(started).Milliseconds(),
			LoggedAt:   time.Now().UTC(),
		}, nil
	}
	if stopState.Active {
		return &GenerationResult{
			Status:     "blocked",
			Reason:     safety.RedactSecrets(stopState.Reason),
			DurationMs: time.Since(started).Milliseconds(),
			LoggedAt:   time.Now().UTC(),
		}, nil
	}
	routeRequest := RouteRequest{Task: request.Task}
	if request.RouteRequest != nil {
		routeRequest = *request.RouteRequest
		if strings.TrimSpace(routeRequest.Task) == "" {
			routeRequest.Task = request.Task
		}
	}
	decision := request.RouteDecision
	if decision == nil || decision.SelectedModelID == "" {
		routed, err := s.RouteWithContext(requestContext, routeRequest)
		if err != nil {
			if requestContext.Err() != nil {
				return cancelledGenerationResultForDecision(started, request.RouteDecision), nil
			}
			return nil, err
		}
		decision = &routed
	}
	if decision.SelectedModelID == "" {
		return &GenerationResult{
			Status:       "skipped",
			Reason:       "no model was selected by the routing policy",
			DurationMs:   time.Since(started).Milliseconds(),
			FallbackPath: fallbackLabels(decision.FallbackPath),
			LoggedAt:     time.Now().UTC(),
		}, nil
	}

	provider, model, ok := s.findProviderModel(decision.SelectedProviderID, decision.SelectedModelID)
	if !ok {
		return nil, fmt.Errorf("selected provider/model not found: %s/%s", decision.SelectedProviderID, decision.SelectedModelID)
	}
	cancelledForSelection := func() *GenerationResult {
		return cancelledGenerationResultForModel(started, provider, model, decision)
	}
	if provider.ID == "ollama" && isOllamaCloudModelID(model.ID) &&
		(!s.policy.PaidCallsAllowed || s.policy.DailyPaidBudgetEUR <= 0) {
		return &GenerationResult{
			ProviderID:       provider.ID,
			ModelID:          model.ID,
			ModelName:        model.Name,
			Tier:             model.Tier,
			Status:           "blocked",
			Reason:           "Ollama cloud model billing is unknown; paid usage and a positive daily budget must be enabled before inference",
			EstimatedCostEUR: model.EstimatedCostEUR,
			DurationMs:       time.Since(started).Milliseconds(),
			FallbackPath:     fallbackLabels(decision.FallbackPath),
			LoggedAt:         time.Now().UTC(),
		}, nil
	}
	if provider.Paid && (!s.policy.PaidCallsAllowed || s.policy.DailyPaidBudgetEUR <= 0) {
		return &GenerationResult{
			ProviderID: provider.ID, ModelID: model.ID, ModelName: model.Name, Tier: model.Tier,
			Status: "blocked", Reason: "paid inference is disabled because paid calls or a positive daily budget are not enabled",
			EstimatedCostEUR: model.EstimatedCostEUR, DurationMs: time.Since(started).Milliseconds(),
			FallbackPath: fallbackLabels(decision.FallbackPath), LoggedAt: time.Now().UTC(),
		}, nil
	}
	if (provider.Paid || model.EstimatedCostEUR > 0 || model.Tier == TierExpensive || model.RequiresApproval) &&
		(request.EffectContext == nil || strings.TrimSpace(request.EffectContext.ApprovalSourceID) == "") {
		return &GenerationResult{
			ProviderID:       provider.ID,
			ModelID:          model.ID,
			ModelName:        model.Name,
			Tier:             model.Tier,
			Status:           "blocked",
			Reason:           "paid or approval-required model execution is disabled until manually approved",
			EstimatedCostEUR: model.EstimatedCostEUR,
			DurationMs:       time.Since(started).Milliseconds(),
			FallbackPath:     fallbackLabels(decision.FallbackPath),
			LoggedAt:         time.Now().UTC(),
		}, nil
	}
	endpoint := strings.TrimRight(strings.TrimSpace(provider.EndpointURL), "/")
	readiness := providerRuntimeReadiness(provider)
	if !readiness.configured {
		return &GenerationResult{
			ProviderID:       provider.ID,
			ModelID:          model.ID,
			ModelName:        model.Name,
			Tier:             model.Tier,
			Status:           generationStatusForReadiness(readiness.status),
			Reason:           readiness.reason,
			EstimatedCostEUR: model.EstimatedCostEUR,
			DurationMs:       time.Since(started).Milliseconds(),
			FallbackPath:     fallbackLabels(decision.FallbackPath),
			LoggedAt:         time.Now().UTC(),
		}, nil
	}
	maintenance := s.ensureModelFreshWithContext(requestContext, provider, model, request.EffectContext)
	if requestContext.Err() != nil {
		return cancelledForSelection(), nil
	}
	maintenanceReason := maintenance.Reason
	strictReason := ""
	if !maintenance.BlocksExecution {
		strictReason = s.strictProbeReasonWithContext(requestContext, provider, normalizeProbePolicy(s.policy), time.Now().UTC())
		if requestContext.Err() != nil {
			return cancelledForSelection(), nil
		}
		if strictReason != "" {
			maintenanceReason = strictReason
		}
	}
	if maintenance.BlocksExecution || strictReason != "" {
		// Exclude only the exact provider/model pair that failed readiness. Retry
		// routing until another candidate passes both maintenance and strict live
		// readiness, or all policy-eligible candidates have been considered.
		excluded := make(map[string]struct{})
		excluded[maintenanceModelSelectionKey(provider.ID, model.ID)] = struct{}{}
		for {
			fallbackRequest := routeRequest
			fallbackRequest.excludedMaintenanceModels = excluded
			rerouted, routeErr := s.RouteWithContext(requestContext, fallbackRequest)
			if routeErr != nil {
				if requestContext.Err() != nil {
					return cancelledForSelection(), nil
				}
				return nil, routeErr
			}
			if rerouted.SelectedModelID == "" {
				return &GenerationResult{
					ProviderID:       provider.ID,
					ModelID:          model.ID,
					ModelName:        model.Name,
					Tier:             model.Tier,
					Status:           "skipped",
					Reason:           "daily model maintenance or strict readiness blocked all remaining eligible models: " + safety.RedactSecrets(maintenanceReason),
					EstimatedCostEUR: model.EstimatedCostEUR,
					DurationMs:       time.Since(started).Milliseconds(),
					FallbackPath:     fallbackLabels(decision.FallbackPath),
					LoggedAt:         time.Now().UTC(),
				}, nil
			}

			decision = &rerouted
			provider, model, ok = s.findProviderModel(decision.SelectedProviderID, decision.SelectedModelID)
			if !ok {
				return nil, fmt.Errorf("rerouted provider/model not found: %s/%s", decision.SelectedProviderID, decision.SelectedModelID)
			}
			if (provider.Paid || model.EstimatedCostEUR > 0 || model.Tier == TierExpensive || model.RequiresApproval) &&
				(request.EffectContext == nil || strings.TrimSpace(request.EffectContext.ApprovalSourceID) == "") {
				return &GenerationResult{
					ProviderID:       provider.ID,
					ModelID:          model.ID,
					ModelName:        model.Name,
					Tier:             model.Tier,
					Status:           "blocked",
					Reason:           "daily model maintenance selected a paid or approval-required fallback, which remains disabled until manually approved",
					EstimatedCostEUR: model.EstimatedCostEUR,
					DurationMs:       time.Since(started).Milliseconds(),
					FallbackPath:     fallbackLabels(decision.FallbackPath),
					LoggedAt:         time.Now().UTC(),
				}, nil
			}
			endpoint = strings.TrimRight(strings.TrimSpace(provider.EndpointURL), "/")
			readiness = providerRuntimeReadiness(provider)
			if !readiness.configured {
				return &GenerationResult{
					ProviderID:       provider.ID,
					ModelID:          model.ID,
					ModelName:        model.Name,
					Tier:             model.Tier,
					Status:           generationStatusForReadiness(readiness.status),
					Reason:           readiness.reason,
					EstimatedCostEUR: model.EstimatedCostEUR,
					DurationMs:       time.Since(started).Milliseconds(),
					FallbackPath:     fallbackLabels(decision.FallbackPath),
					LoggedAt:         time.Now().UTC(),
				}, nil
			}

			maintenance = s.ensureModelFreshWithContext(requestContext, provider, model, request.EffectContext)
			if requestContext.Err() != nil {
				return cancelledForSelection(), nil
			}
			if maintenance.BlocksExecution {
				maintenanceReason = maintenance.Reason
				excluded[maintenanceModelSelectionKey(provider.ID, model.ID)] = struct{}{}
				continue
			}
			strictReason = s.strictProbeReasonWithContext(requestContext, provider, normalizeProbePolicy(s.policy), time.Now().UTC())
			if requestContext.Err() != nil {
				return cancelledForSelection(), nil
			}
			if strictReason != "" {
				maintenanceReason = strictReason
				excluded[maintenanceModelSelectionKey(provider.ID, model.ID)] = struct{}{}
				continue
			}
			break
		}
	}
	if provider.ID == "odysseus" {
		return &GenerationResult{
			ProviderID:       provider.ID,
			ModelID:          model.ID,
			ModelName:        model.Name,
			Tier:             model.Tier,
			Status:           "blocked",
			Reason:           odysseusExecutionBlockedReason(),
			EstimatedCostEUR: model.EstimatedCostEUR,
			DurationMs:       time.Since(started).Milliseconds(),
			FallbackPath:     fallbackLabels(decision.FallbackPath),
			LoggedAt:         time.Now().UTC(),
		}, nil
	}
	if provider.ID == "litellm" {
		probe := probeProviderWithContext(requestContext, provider, s.policy)
		if requestContext.Err() != nil {
			return cancelledForSelection(), nil
		}
		if !probe.Live {
			return &GenerationResult{
				ProviderID:       provider.ID,
				ModelID:          model.ID,
				ModelName:        model.Name,
				Tier:             model.Tier,
				Status:           "skipped",
				Reason:           "LiteLLM gateway must pass a live authenticated /v1/models probe before generation: " + probe.Reason,
				EstimatedCostEUR: model.EstimatedCostEUR,
				DurationMs:       time.Since(started).Milliseconds(),
				FallbackPath:     fallbackLabels(decision.FallbackPath),
				LoggedAt:         time.Now().UTC(),
			}, nil
		}
	}
	if provider.Paid {
		var reserveErr error
		paidReservationID, reservedCostEUR, reservedInputTokens, reservedOutputTokens, reserveErr = s.reservePaidInferenceBudget(
			requestContext, provider, model, request, decision,
		)
		if requestContext.Err() != nil {
			return cancelledForSelection(), nil
		}
		if reserveErr != nil {
			return &GenerationResult{
				ProviderID: provider.ID, ModelID: model.ID, ModelName: model.Name, Tier: model.Tier,
				Status: "blocked", Reason: safety.RedactSecrets(reserveErr.Error()),
				EstimatedCostEUR: model.EstimatedCostEUR, DurationMs: time.Since(started).Milliseconds(),
				FallbackPath: fallbackLabels(decision.FallbackPath), LoggedAt: time.Now().UTC(),
			}, nil
		}
	}

	output, reportedUsage, err := s.callProvider(requestContext, provider, model, endpoint, request)
	if err != nil {
		if requestContext.Err() != nil {
			inputTokens := estimateTokens(buildPrompt(request))
			classification := decision.Classification
			if classification.TaskType == "" {
				classification = classifyTask(routeRequest)
			}
			outputTokens := estimateRouteOutputTokens(classification)
			if request.MaxTokens > 0 && request.MaxTokens < outputTokens {
				outputTokens = request.MaxTokens
			}
			actualCostEUR := estimateModelUsageCostEUR(model, inputTokens, outputTokens)
			if actualCostEUR == 0 {
				actualCostEUR = model.EstimatedCostEUR
			}
			s.addUsage(provider.ID, model.ID, actualCostEUR, inputTokens, outputTokens)
			cancelled := cancelledForSelection()
			cancelled.Reason = "provider request was cancelled or timed out after dispatch; response and billed usage were not confirmed, so conservative usage estimates were recorded"
			cancelled.EstimatedCostEUR = actualCostEUR
			cancelled.InputTokens = inputTokens
			cancelled.OutputTokens = outputTokens
			cancelled.UsageSource = "estimated_uncertain"
			return cancelled, nil
		}
		return &GenerationResult{
			ProviderID:       provider.ID,
			ModelID:          model.ID,
			ModelName:        model.Name,
			Tier:             model.Tier,
			Status:           "failed",
			Reason:           redactProviderSecrets(provider, err.Error()),
			EstimatedCostEUR: model.EstimatedCostEUR,
			DurationMs:       time.Since(started).Milliseconds(),
			FallbackPath:     fallbackLabels(decision.FallbackPath),
			LoggedAt:         time.Now().UTC(),
		}, nil
	}
	inputTokens := estimateTokens(buildPrompt(request))
	outputTokens := estimateTokens(output)
	if reportedUsage.HasInput && reportedUsage.InputTokens < 0 {
		reportedUsage.InputTokens = 0
		reportedUsage.HasInput = false
	}
	if reportedUsage.HasOutput && reportedUsage.OutputTokens < 0 {
		reportedUsage.OutputTokens = 0
		reportedUsage.HasOutput = false
	}
	if reportedUsage.HasInput {
		inputTokens = reportedUsage.InputTokens
	}
	if reportedUsage.HasOutput {
		outputTokens = reportedUsage.OutputTokens
	}
	actualCostEUR := estimateModelUsageCostEUR(model, inputTokens, outputTokens)
	if actualCostEUR == 0 {
		actualCostEUR = model.EstimatedCostEUR
	}
	// A successful provider response may race cancellation. It is still billable,
	// so account for it before returning the cancelled operation state.
	s.addUsage(provider.ID, model.ID, actualCostEUR, inputTokens, outputTokens)
	if requestContext.Err() != nil {
		cancelled := cancelledForSelection()
		cancelled.Reason = "request was cancelled after the provider returned a successful response; usage was recorded"
		cancelled.EstimatedCostEUR = actualCostEUR
		cancelled.InputTokens = inputTokens
		cancelled.OutputTokens = outputTokens
		cancelled.UsageSource = reportedUsage.source()
		return cancelled, nil
	}
	return &GenerationResult{
		ProviderID:       provider.ID,
		ModelID:          model.ID,
		ModelName:        model.Name,
		Tier:             model.Tier,
		Output:           redactProviderSecrets(provider, strings.TrimSpace(output)),
		Status:           "completed",
		Reason:           "model endpoint returned a draft; verification must still ground important claims",
		EstimatedCostEUR: actualCostEUR,
		InputTokens:      inputTokens,
		OutputTokens:     outputTokens,
		UsageSource:      reportedUsage.source(),
		DurationMs:       time.Since(started).Milliseconds(),
		FallbackPath:     fallbackLabels(decision.FallbackPath),
		LoggedAt:         time.Now().UTC(),
	}, nil
}

func cancelledGenerationResult(started time.Time) *GenerationResult {
	return &GenerationResult{
		Status:     "cancelled",
		Reason:     "model generation was cancelled before the provider result was accepted",
		DurationMs: time.Since(started).Milliseconds(),
		LoggedAt:   time.Now().UTC(),
	}
}

func cancelledGenerationResultForDecision(started time.Time, decision *RouteDecision) *GenerationResult {
	result := cancelledGenerationResult(started)
	if decision == nil {
		return result
	}
	result.ProviderID = decision.SelectedProviderID
	result.ModelID = decision.SelectedModelID
	result.ModelName = decision.SelectedModelName
	result.Tier = decision.Tier
	result.EstimatedCostEUR = decision.EstimatedCostEUR
	result.FallbackPath = fallbackLabels(decision.FallbackPath)
	return result
}

func cancelledGenerationResultForModel(started time.Time, provider Provider, model Model, decision *RouteDecision) *GenerationResult {
	result := cancelledGenerationResult(started)
	result.ProviderID = provider.ID
	result.ModelID = model.ID
	result.ModelName = model.Name
	result.Tier = model.Tier
	result.EstimatedCostEUR = model.EstimatedCostEUR
	if decision != nil {
		result.FallbackPath = fallbackLabels(decision.FallbackPath)
	}
	return result
}

func (s *Service) findProviderModel(providerID, modelID string) (Provider, Model, bool) {
	for _, provider := range s.policy.Providers {
		if provider.ID != providerID {
			continue
		}
		for _, model := range provider.Models {
			if model.ID == modelID {
				provider, model = applyOllamaModelPolicy(provider, model)
				return provider, model, true
			}
		}
	}
	return Provider{}, Model{}, false
}

func (s *Service) callProvider(ctx context.Context, provider Provider, model Model, endpoint string, request GenerateRequest) (string, providerUsage, error) {
	ctx, cancel := context.WithTimeout(ctx, generationTimeout())
	defer cancel()
	prompt := buildPrompt(request)
	maxOutputTokens := request.MaxTokens
	if maxOutputTokens <= 0 {
		maxOutputTokens = 800
	}
	estimatedCostEUR := estimateModelUsageCostEUR(
		model,
		estimateTokens(prompt),
		maxOutputTokens,
	)
	if estimatedCostEUR == 0 {
		estimatedCostEUR = model.EstimatedCostEUR
	}
	switch provider.ID {
	case "ollama":
		return s.callOllama(ctx, endpoint, provider, model, prompt, estimatedCostEUR, request)
	case "odysseus":
		return "", providerUsage{}, errors.New(odysseusExecutionBlockedReason())
	default:
		return s.callOpenAICompatible(ctx, endpoint, provider, model, prompt, estimatedCostEUR, request)
	}
}

func generationTimeout() time.Duration {
	seconds := boundedMaintenanceEnv("LLM_GENERATION_TIMEOUT_SECONDS", 60, 1, 300)
	return time.Duration(seconds) * time.Second
}

func probeProvider(provider Provider, policy Policy) ProviderProbeResult {
	return probeProviderWithContext(context.Background(), provider, policy)
}

func providerProbeTimeout() time.Duration {
	seconds := boundedMaintenanceEnv("LLM_PROVIDER_PROBE_TIMEOUT_SECONDS", 5, 1, 30)
	return time.Duration(seconds) * time.Second
}

func probeProviderWithContext(parent context.Context, provider Provider, policy Policy) ProviderProbeResult {
	if parent == nil {
		parent = context.Background()
	}
	started := time.Now().UTC()
	result := ProviderProbeResult{
		ProviderID:   provider.ID,
		ProviderName: provider.Name,
		EndpointURL:  safety.RedactURL(provider.EndpointURL),
		CheckedAt:    started,
	}
	if parent.Err() != nil {
		return cancelledProviderProbe(result, started)
	}
	readiness := providerRuntimeReadiness(provider)
	if !readiness.configured {
		result.Status = readiness.status
		result.Reason = readiness.reason
		result.RequiresReview = readiness.status == "blocked_endpoint" || readiness.status == "invalid_endpoint"
		return result
	}
	if provider.Paid && (!policy.PaidCallsAllowed || policy.RequireApprovalBeforePaidUsage) {
		result.Status = "blocked"
		result.Reason = "paid provider probe is blocked until server-side paid approval exists"
		result.RequiresReview = true
		return result
	}
	if provider.Paid && !paidProviderProbeBudgetAvailable(policy) {
		result.Status = "blocked"
		result.Reason = "paid provider probe is blocked because the configured daily budget is unavailable or exhausted"
		result.RequiresReview = true
		return result
	}
	if provider.ID == "odysseus" {
		return probeOdysseusProvider(parent, provider, result, started)
	}

	endpoint := strings.TrimRight(strings.TrimSpace(provider.EndpointURL), "/")
	probePath := "/v1/models"
	if provider.ID == "ollama" {
		probePath = "/api/tags"
	}
	ctx, cancel := context.WithTimeout(parent, providerProbeTimeout())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+probePath, nil)
	if err != nil {
		result.Status = "failed"
		result.Reason = redactProviderSecrets(provider, err.Error())
		return result
	}
	req.Header.Set("User-Agent", "018-HAI-Provider-Probe/1.0")
	if provider.APIKeyEnv != "" {
		if key := strings.TrimSpace(os.Getenv(provider.APIKeyEnv)); key != "" {
			req.Header.Set("Authorization", "Bearer "+key)
		}
	}
	resp, err := providerHTTPClientFor(provider).Do(req)
	result.DurationMs = time.Since(started).Milliseconds()
	if err != nil {
		if parent.Err() != nil {
			return cancelledProviderProbe(result, started)
		}
		result.Status = "failed"
		result.Reason = redactProviderSecrets(provider, err.Error())
		return result
	}
	raw, readErr := readLimitedResponseBody(resp.Body, 256*1024)
	probeErr := ctx.Err()
	_ = resp.Body.Close()
	if parent.Err() != nil {
		return cancelledProviderProbe(result, started)
	}
	if probeErr != nil {
		result.Status = "failed"
		result.Reason = "provider probe timed out before readiness could be confirmed"
		return result
	}
	if readErr != nil {
		result.Status = "failed"
		result.Reason = "provider response could not be read: " + redactProviderSecrets(provider, readErr.Error())
		return result
	}
	result.HTTPStatus = resp.StatusCode
	if resp.StatusCode >= 300 {
		result.Status = "failed"
		result.Reason = fmt.Sprintf("probe returned HTTP %d: %s", resp.StatusCode, compactOutput([]byte(redactProviderSecrets(provider, string(raw))), 300))
		if resp.StatusCode >= 300 && resp.StatusCode < 400 {
			result.RequiresReview = true
		}
		return result
	}
	if !json.Valid(raw) {
		result.Status = "failed"
		result.Reason = "provider returned an invalid JSON model-list response"
		return result
	}
	if parent.Err() != nil {
		return cancelledProviderProbe(result, started)
	}
	if ctx.Err() != nil {
		result.Status = "failed"
		result.Reason = "provider probe timed out before readiness could be confirmed"
		return result
	}
	result.ReportedModelIDs = probeModelIDs(provider, raw)
	result.ModelsSeen = len(result.ReportedModelIDs)
	result.Live = true
	result.Status = "live"
	if result.ModelsSeen > 0 {
		result.Reason = fmt.Sprintf("provider endpoint responded and reported %d model(s)", result.ModelsSeen)
	} else {
		result.Reason = "provider endpoint responded; model count was not available"
	}
	return result
}

func paidProviderProbeBudgetAvailable(policy Policy) bool {
	if math.IsNaN(policy.DailyPaidBudgetEUR) || math.IsInf(policy.DailyPaidBudgetEUR, 0) ||
		math.IsNaN(policy.DailyBudgetUsedEUR) || math.IsInf(policy.DailyBudgetUsedEUR, 0) ||
		policy.DailyPaidBudgetEUR <= 0 || policy.DailyBudgetUsedEUR < 0 ||
		policy.DailyBudgetUsedEUR >= policy.DailyPaidBudgetEUR {
		return false
	}
	// A process-local snapshot cannot account for calls made by other backend
	// instances or survive a restart. Unknown/empty states are equally
	// insufficient evidence for authorizing any paid-provider probe.
	return policy.UsageAccountingStatus == "durable"
}

func cancelledProviderProbe(result ProviderProbeResult, started time.Time) ProviderProbeResult {
	result.Live = false
	result.Status = "cancelled"
	result.Reason = "provider probe was cancelled before readiness could be confirmed"
	result.DurationMs = time.Since(started).Milliseconds()
	result.ReportedModelIDs = nil
	result.ModelsSeen = 0
	return result
}

func probeOdysseusProvider(parent context.Context, provider Provider, result ProviderProbeResult, started time.Time) ProviderProbeResult {
	endpoint := strings.TrimRight(strings.TrimSpace(provider.EndpointURL), "/")
	paths := []string{"/api/health", "/health", "/api/v1/health", "/"}
	lastReason := ""
	ctx, cancel := context.WithTimeout(parent, providerProbeTimeout())
	defer cancel()

	for _, path := range paths {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+path, nil)
		if err != nil {
			result.Status = "failed"
			result.Reason = redactProviderSecrets(provider, err.Error())
			return result
		}
		req.Header.Set("User-Agent", "018-HAI-Odysseus-Probe/1.0")
		if provider.APIKeyEnv != "" {
			if key := strings.TrimSpace(os.Getenv(provider.APIKeyEnv)); key != "" {
				req.Header.Set("Authorization", "Bearer "+key)
			}
		}
		resp, err := providerHTTPClientFor(provider).Do(req)
		result.DurationMs = time.Since(started).Milliseconds()
		if err != nil {
			if parent.Err() != nil {
				return cancelledProviderProbe(result, started)
			}
			if ctx.Err() != nil {
				result.Status = "failed"
				result.Reason = "Odysseus provider probe timed out before readiness could be confirmed"
				return result
			}
			lastReason = redactProviderSecrets(provider, err.Error())
			continue
		}
		raw, readErr := readLimitedResponseBody(resp.Body, 256*1024)
		probeErr := ctx.Err()
		_ = resp.Body.Close()
		if parent.Err() != nil {
			return cancelledProviderProbe(result, started)
		}
		if probeErr != nil {
			result.Status = "failed"
			result.Reason = "Odysseus provider probe timed out before readiness could be confirmed"
			result.DurationMs = time.Since(started).Milliseconds()
			return result
		}
		if readErr != nil {
			result.Status = "failed"
			result.Reason = "provider response could not be read: " + redactProviderSecrets(provider, readErr.Error())
			return result
		}
		result.HTTPStatus = resp.StatusCode
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			result.Status = "auth_required"
			result.Reason = fmt.Sprintf("Odysseus endpoint responded with HTTP %d at %s; configure ODYSSEUS_API_TOKEN if auth is enabled", resp.StatusCode, path)
			result.RequiresReview = true
			return result
		}
		if resp.StatusCode >= 300 && resp.StatusCode < 400 {
			result.Status = "failed"
			result.Reason = fmt.Sprintf("Odysseus probe at %s returned redirect HTTP %d; redirects are not followed", path, resp.StatusCode)
			result.RequiresReview = true
			return result
		}
		if resp.StatusCode < 300 {
			result.Live = true
			result.Status = "live"
			result.ModelsSeen = countProbeModels(provider, raw)
			result.Reason = fmt.Sprintf("Odysseus workspace is reachable via %s; workspace-agent execution remains approval-gated and disabled until a reviewed task adapter exists", path)
			return result
		}
		lastReason = fmt.Sprintf("Odysseus probe at %s returned HTTP %d: %s", path, resp.StatusCode, compactOutput([]byte(redactProviderSecrets(provider, string(raw))), 300))
	}

	result.Status = "failed"
	result.Reason = firstNonEmpty(lastReason, "Odysseus endpoint did not respond on known health paths")
	return result
}

func countProbeModels(provider Provider, raw []byte) int {
	return len(probeModelIDs(provider, raw))
}

func probeModelIDs(provider Provider, raw []byte) []string {
	ids := []string{}
	if provider.ID == "ollama" {
		var decoded struct {
			Models []struct {
				Name string `json:"name"`
			} `json:"models"`
		}
		if err := json.Unmarshal(raw, &decoded); err == nil {
			for _, model := range decoded.Models {
				if id := strings.TrimSpace(model.Name); id != "" {
					ids = append(ids, id)
				}
			}
		}
		return uniqueStrings(ids)
	}
	var decoded struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &decoded); err == nil {
		for _, model := range decoded.Data {
			if id := strings.TrimSpace(model.ID); id != "" {
				ids = append(ids, id)
			}
		}
	}
	return uniqueStrings(ids)
}

func (s *Service) callOllama(
	ctx context.Context,
	endpoint string,
	provider Provider,
	model Model,
	prompt string,
	estimatedCostEUR float64,
	request GenerateRequest,
) (string, providerUsage, error) {
	payload := map[string]interface{}{
		"model":  model.ID,
		"prompt": prompt,
		"stream": false,
	}
	if request.Temperature > 0 {
		payload["options"] = map[string]interface{}{"temperature": request.Temperature}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", providerUsage{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"/api/generate", bytes.NewReader(body))
	if err != nil {
		return "", providerUsage{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if provider.APIKeyEnv != "" {
		if key := strings.TrimSpace(os.Getenv(provider.APIKeyEnv)); key != "" {
			req.Header.Set("Authorization", "Bearer "+key)
		}
	}
	authorization, err := buildFinalEffectAuthorizationRequest(
		EffectOperationGenerate,
		request.EffectContext,
		provider,
		model,
		endpoint,
		estimatedCostEUR,
		[]byte(prompt),
		body,
		"",
	)
	if err != nil {
		return "", providerUsage{}, err
	}
	if err := s.authorizeFinalEffect(ctx, authorization); err != nil {
		return "", providerUsage{}, err
	}
	resp, err := providerHTTPClientFor(provider).Do(req)
	if err != nil {
		return "", providerUsage{}, err
	}
	defer resp.Body.Close()
	if isEventStreamResponse(resp) {
		return "", providerUsage{}, errors.New("Ollama returned a streaming response to a non-streaming generation request")
	}
	raw, err := readLimitedResponseBody(resp.Body, 1024*1024)
	if err != nil {
		return "", providerUsage{}, fmt.Errorf("read Ollama generation response: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return "", providerUsage{}, fmt.Errorf("ollama returned HTTP %d: %s", resp.StatusCode, compactOutput([]byte(redactProviderSecrets(provider, string(raw))), 500))
	}
	var decoded struct {
		Response        string `json:"response"`
		Error           string `json:"error"`
		PromptEvalCount *int   `json:"prompt_eval_count"`
		EvalCount       *int   `json:"eval_count"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return "", providerUsage{}, err
	}
	if decoded.Error != "" {
		return "", providerUsage{}, fmt.Errorf("%s", redactProviderSecrets(provider, decoded.Error))
	}
	if strings.TrimSpace(decoded.Response) == "" {
		return "", providerUsage{}, errors.New("Ollama returned an empty generation response")
	}
	usage := providerUsage{}
	if decoded.PromptEvalCount != nil {
		usage.InputTokens = *decoded.PromptEvalCount
		usage.HasInput = true
	}
	if decoded.EvalCount != nil {
		usage.OutputTokens = *decoded.EvalCount
		usage.HasOutput = true
	}
	return decoded.Response, usage, nil
}

func (s *Service) callOpenAICompatible(
	ctx context.Context,
	endpoint string,
	provider Provider,
	model Model,
	prompt string,
	estimatedCostEUR float64,
	request GenerateRequest,
) (string, providerUsage, error) {
	maxTokens := request.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 800
	}
	payload := map[string]interface{}{
		"model": model.ID,
		"messages": []map[string]string{
			{"role": "system", "content": firstNonEmpty(request.SystemPrompt, "You are a careful local-first assistant. Use only provided context when factual grounding is required.")},
			{"role": "user", "content": prompt},
		},
		"temperature": request.Temperature,
		"max_tokens":  maxTokens,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", providerUsage{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", providerUsage{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if provider.APIKeyEnv != "" {
		if key := strings.TrimSpace(os.Getenv(provider.APIKeyEnv)); key != "" {
			req.Header.Set("Authorization", "Bearer "+key)
		}
	}
	authorization, err := buildFinalEffectAuthorizationRequest(
		EffectOperationGenerate,
		request.EffectContext,
		provider,
		model,
		endpoint,
		estimatedCostEUR,
		[]byte(prompt),
		body,
		"",
	)
	if err != nil {
		return "", providerUsage{}, err
	}
	if err := s.authorizeFinalEffect(ctx, authorization); err != nil {
		return "", providerUsage{}, err
	}
	resp, err := providerHTTPClientFor(provider).Do(req)
	if err != nil {
		return "", providerUsage{}, err
	}
	defer resp.Body.Close()
	if isEventStreamResponse(resp) {
		return "", providerUsage{}, errors.New("OpenAI-compatible endpoint returned a streaming response to a non-streaming generation request")
	}
	raw, err := readLimitedResponseBody(resp.Body, 1024*1024)
	if err != nil {
		return "", providerUsage{}, fmt.Errorf("read OpenAI-compatible generation response: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return "", providerUsage{}, fmt.Errorf("openai-compatible endpoint returned HTTP %d: %s", resp.StatusCode, compactOutput([]byte(redactProviderSecrets(provider, string(raw))), 500))
	}
	var decoded struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     *int `json:"prompt_tokens"`
			CompletionTokens *int `json:"completion_tokens"`
			InputTokens      *int `json:"input_tokens"`
			OutputTokens     *int `json:"output_tokens"`
		} `json:"usage"`
		Error interface{} `json:"error"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return "", providerUsage{}, err
	}
	if decoded.Error != nil {
		return "", providerUsage{}, fmt.Errorf("endpoint returned error: %s", redactProviderSecrets(provider, fmt.Sprint(decoded.Error)))
	}
	if len(decoded.Choices) == 0 {
		return "", providerUsage{}, fmt.Errorf("endpoint returned no choices")
	}
	if strings.TrimSpace(decoded.Choices[0].Message.Content) == "" {
		return "", providerUsage{}, errors.New("endpoint returned an empty message content")
	}
	usage := providerUsage{}
	if decoded.Usage.PromptTokens != nil {
		usage.InputTokens = *decoded.Usage.PromptTokens
		usage.HasInput = true
	} else if decoded.Usage.InputTokens != nil {
		usage.InputTokens = *decoded.Usage.InputTokens
		usage.HasInput = true
	}
	if decoded.Usage.CompletionTokens != nil {
		usage.OutputTokens = *decoded.Usage.CompletionTokens
		usage.HasOutput = true
	} else if decoded.Usage.OutputTokens != nil {
		usage.OutputTokens = *decoded.Usage.OutputTokens
		usage.HasOutput = true
	}
	return decoded.Choices[0].Message.Content, usage, nil
}

func (s *Service) addLog(decision RouteDecision) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.logs = append([]RouteDecision{decision}, s.logs...)
	if len(s.logs) > 50 {
		s.logs = s.logs[:50]
	}
}

func (s *Service) addUsage(providerID, modelID string, budgetEUR float64, inputTokens, outputTokens int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.addUsageLocked(providerID, modelID, budgetEUR, inputTokens, outputTokens)
}

func (s *Service) addUsageIfContextActive(ctx context.Context, providerID, modelID string, budgetEUR float64, inputTokens, outputTokens int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ctx != nil && ctx.Err() != nil {
		return false
	}
	s.addUsageLocked(providerID, modelID, budgetEUR, inputTokens, outputTokens)
	return true
}

func (s *Service) addUsageLocked(providerID, modelID string, budgetEUR float64, inputTokens, outputTokens int) {
	s.ensureUsagePeriodLocked(s.currentTime())
	if budgetEUR < 0 {
		budgetEUR = 0
	}
	if inputTokens < 0 {
		inputTokens = 0
	}
	if outputTokens < 0 {
		outputTokens = 0
	}
	if s.usage == nil {
		s.usage = map[string]UsageCounter{}
	}
	current := s.usage[providerID]
	current.BudgetUsedEUR += budgetEUR
	current.InputTokensUsed += inputTokens
	current.OutputTokensUsed += outputTokens
	s.usage[providerID] = current
	if strings.TrimSpace(modelID) != "" {
		modelUsage := s.usage[usageKey(providerID, modelID)]
		modelUsage.BudgetUsedEUR += budgetEUR
		modelUsage.InputTokensUsed += inputTokens
		modelUsage.OutputTokensUsed += outputTokens
		s.usage[usageKey(providerID, modelID)] = modelUsage
	}
}

func (s *Service) annotateUsage(policy Policy) Policy {
	periodStart := utcDayStart(s.currentTime())
	periodEnd := periodStart.Add(24 * time.Hour)
	usage, status := s.dailyUsage(periodStart, periodEnd)
	policy.UsageAccountingStatus = status
	policy.UsagePeriodStart = periodStart
	policy.UsageTimezone = "UTC"
	for index := range policy.Providers {
		providerUsage := usage[policy.Providers[index].ID]
		policy.Providers[index].BudgetUsedEUR = providerUsage.BudgetUsedEUR
		policy.Providers[index].InputTokensUsed = providerUsage.InputTokensUsed
		policy.Providers[index].OutputTokensUsed = providerUsage.OutputTokensUsed
		policy.Providers[index].UsageAccountingStatus = status
		policy.DailyBudgetUsedEUR += providerUsage.BudgetUsedEUR
		policy.InputTokensUsed += providerUsage.InputTokensUsed
		policy.OutputTokensUsed += providerUsage.OutputTokensUsed
		for modelIndex := range policy.Providers[index].Models {
			model := &policy.Providers[index].Models[modelIndex]
			modelUsage := usage[usageKey(policy.Providers[index].ID, model.ID)]
			model.BudgetUsedEUR = modelUsage.BudgetUsedEUR
			model.InputTokensUsed = modelUsage.InputTokensUsed
			model.OutputTokensUsed = modelUsage.OutputTokensUsed
			model.UsageAccountingStatus = status
		}
	}
	return policy
}

func (s *Service) dailyUsage(periodStart, periodEnd time.Time) (map[string]UsageCounter, string) {
	if repository, ok := s.generationHistory.(GenerationUsageHistoryRepository); ok {
		aggregates, err := repository.UsageBetween(periodStart, periodEnd)
		if err == nil {
			usage := make(map[string]UsageCounter, len(aggregates)*2)
			for _, aggregate := range aggregates {
				counter := UsageCounter{
					BudgetUsedEUR:    aggregate.BudgetUsedEUR,
					InputTokensUsed:  aggregate.InputTokensUsed,
					OutputTokensUsed: aggregate.OutputTokensUsed,
				}
				provider := usage[aggregate.ProviderID]
				provider.BudgetUsedEUR += counter.BudgetUsedEUR
				provider.InputTokensUsed += counter.InputTokensUsed
				provider.OutputTokensUsed += counter.OutputTokensUsed
				usage[aggregate.ProviderID] = provider
				if strings.TrimSpace(aggregate.ModelID) != "" {
					usage[usageKey(aggregate.ProviderID, aggregate.ModelID)] = counter
				}
			}
			return usage, "durable"
		}
		return s.processUsageSnapshot(periodStart), "unavailable"
	}
	return s.processUsageSnapshot(periodStart), "process_only"
}

func (s *Service) processUsageSnapshot(periodStart time.Time) map[string]UsageCounter {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureUsagePeriodLocked(periodStart)
	copy := make(map[string]UsageCounter, len(s.usage))
	for key, value := range s.usage {
		copy[key] = value
	}
	return copy
}

func (s *Service) ensureUsagePeriodLocked(now time.Time) {
	periodStart := utcDayStart(now)
	if s.usagePeriodStart.IsZero() {
		s.usagePeriodStart = periodStart
		return
	}
	if !utcDayStart(s.usagePeriodStart).Equal(periodStart) {
		s.usage = map[string]UsageCounter{}
		s.usagePeriodStart = periodStart
	}
}

func (s *Service) currentTime() time.Time {
	if s.now != nil {
		return s.now().UTC()
	}
	return time.Now().UTC()
}

func utcDayStart(value time.Time) time.Time {
	value = value.UTC()
	return time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, time.UTC)
}

func usageKey(providerID, modelID string) string {
	return providerID + "\x00" + modelID
}

func estimateModelUsageCostEUR(model Model, inputTokens, outputTokens int) float64 {
	return costFromMillionTokenPrice(model.InputCostPerMillionTokensEUR, inputTokens) +
		costFromMillionTokenPrice(model.OutputCostPerMillionTokensEUR, outputTokens)
}

func estimateRouteCostEUR(model Model, inputTokens, outputTokens int) float64 {
	cost := estimateModelUsageCostEUR(model, inputTokens, outputTokens)
	if cost > 0 {
		return cost
	}
	return model.EstimatedCostEUR
}

func estimateRouteOutputTokens(classification TaskClassification) int {
	reasoningMultiplier := map[string]int{
		"low":       1,
		"medium":    2,
		"high":      3,
		"very_high": 4,
	}
	multiplier := reasoningMultiplier[classification.RequiredReasoning]
	if multiplier == 0 {
		multiplier = 2
	}
	estimate := 180 + (classification.Difficulty * 120 * multiplier)
	if hasCapability(classification.RequiredCapabilities, "coding") {
		estimate += 500
	}
	if hasCapability(classification.RequiredCapabilities, "verification") {
		estimate += 350
	}
	if estimate > 4000 {
		return 4000
	}
	return estimate
}

func costFromMillionTokenPrice(priceEUR float64, tokens int) float64 {
	if priceEUR <= 0 || tokens <= 0 {
		return 0
	}
	return priceEUR * float64(tokens) / 1_000_000
}

func estimateTokens(value string) int {
	words := len(strings.Fields(value))
	chars := len([]rune(value))
	byChars := chars / 4
	if chars%4 != 0 {
		byChars++
	}
	return maxInt(words, byChars)
}

type candidate struct {
	provider    Provider
	model       Model
	calibration *modelintelligence.ModelCalibration
}

func (s *Service) candidates(classification TaskClassification, request RouteRequest) ([]candidate, []SkippedModel) {
	return s.candidatesWithContext(context.Background(), classification, request)
}

func (s *Service) candidatesWithContext(ctx context.Context, classification TaskClassification, request RouteRequest) ([]candidate, []SkippedModel) {
	candidates := []candidate{}
	skipped := []SkippedModel{}
	policy := normalizeProbePolicy(s.policy)

	for _, provider := range policy.Providers {
		if !provider.Enabled {
			for _, model := range provider.Models {
				skipped = append(skipped, SkippedModel{ProviderID: provider.ID, ModelID: model.ID, Reason: "provider disabled"})
			}
			continue
		}
		policyModels := make([]candidate, 0, len(provider.Models))
		for _, model := range provider.Models {
			modelProvider, policyModel := applyOllamaModelPolicy(provider, model)
			if modelProvider.Local && !policy.LocalModelsAllowed {
				skipped = append(skipped, SkippedModel{ProviderID: provider.ID, ModelID: model.ID, Reason: "local models disabled by policy"})
				continue
			}
			if modelProvider.Paid && (!policy.PaidCallsAllowed || policy.DailyPaidBudgetEUR <= 0) {
				skipped = append(skipped, SkippedModel{ProviderID: provider.ID, ModelID: model.ID, Reason: "paid usage disabled by policy"})
				continue
			}
			if !modelProvider.Local && !modelProvider.Paid && modelProvider.QuotaRemaining <= 0 && !policy.FreeCloudQuotaAllowed {
				skipped = append(skipped, SkippedModel{ProviderID: provider.ID, ModelID: model.ID, Reason: "free cloud quota unavailable"})
				continue
			}
			if !modelProvider.Local && !modelProvider.Paid && modelProvider.QuotaRemaining == 0 {
				skipped = append(skipped, SkippedModel{ProviderID: provider.ID, ModelID: model.ID, Reason: "free cloud quota exhausted or unknown"})
				continue
			}
			policyModels = append(policyModels, candidate{provider: modelProvider, model: policyModel})
		}
		if len(policyModels) == 0 {
			continue
		}
		readiness := providerRuntimeReadiness(provider)
		if !readiness.configured {
			for _, model := range policyModels {
				skipped = append(skipped, SkippedModel{ProviderID: provider.ID, ModelID: model.model.ID, Reason: readiness.reason})
			}
			continue
		}
		if strictReason := s.strictProbeReasonWithContext(ctx, provider, policy, time.Now().UTC()); strictReason != "" {
			for _, model := range policyModels {
				skipped = append(skipped, SkippedModel{ProviderID: provider.ID, ModelID: model.model.ID, Reason: strictReason})
			}
			continue
		}
		for _, modelCandidate := range policyModels {
			modelProvider, model := modelCandidate.provider, modelCandidate.model
			if _, excluded := request.excludedMaintenanceModels[maintenanceModelSelectionKey(provider.ID, model.ID)]; excluded {
				skipped = append(skipped, SkippedModel{ProviderID: provider.ID, ModelID: model.ID, Reason: "model was excluded after its maintenance or readiness check failed during this generation"})
				continue
			}
			reason := unsuitableReason(modelProvider, model, classification, policy, request)
			if reason != "" {
				skipped = append(skipped, SkippedModel{ProviderID: provider.ID, ModelID: model.ID, Reason: reason})
				continue
			}
			candidates = append(candidates, candidate{provider: modelProvider, model: model})
		}
	}

	calibration := s.calibrationByModel(classification)
	for index := range candidates {
		if observed, ok := calibration[modelCalibrationKey(candidates[index].provider.ID, candidates[index].model.ID)]; ok {
			copy := observed
			candidates[index].calibration = &copy
		}
	}
	eligible := make([]candidate, 0, len(candidates))
	knownWeak := make([]candidate, 0)
	for _, current := range candidates {
		if current.calibration != nil && current.calibration.EvaluatedRuns >= 5 && current.calibration.AcceptedOutputs == 0 {
			knownWeak = append(knownWeak, current)
			continue
		}
		eligible = append(eligible, current)
	}
	// Do not turn finite calibration evidence into a dead-end. Known weak models
	// are skipped only when at least one otherwise suitable candidate remains.
	if len(eligible) > 0 {
		for _, current := range knownWeak {
			skipped = append(skipped, SkippedModel{
				ProviderID: current.provider.ID,
				ModelID:    current.model.ID,
				Reason:     "five or more evaluated outputs produced no validator-accepted result for this routing lane",
			})
		}
		candidates = eligible
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		left := candidates[i]
		right := candidates[j]
		leftAccepted := left.calibration != nil && left.calibration.AcceptedOutputs > 0
		rightAccepted := right.calibration != nil && right.calibration.AcceptedOutputs > 0
		if leftAccepted != rightAccepted {
			return leftAccepted
		}
		if leftAccepted && rightAccepted {
			if left.calibration.WilsonLowerBound != right.calibration.WilsonLowerBound {
				return left.calibration.WilsonLowerBound > right.calibration.WilsonLowerBound
			}
			if left.calibration.EvaluatedRuns != right.calibration.EvaluatedRuns {
				return left.calibration.EvaluatedRuns > right.calibration.EvaluatedRuns
			}
		}
		if policy.LocalFirst && left.provider.Local != right.provider.Local {
			return left.provider.Local
		}
		if tierRank[left.model.Tier] != tierRank[right.model.Tier] {
			return tierRank[left.model.Tier] < tierRank[right.model.Tier]
		}
		if left.model.EstimatedCostEUR != right.model.EstimatedCostEUR {
			return left.model.EstimatedCostEUR < right.model.EstimatedCostEUR
		}
		return left.model.ID < right.model.ID
	})

	return candidates, skipped
}

func (s *Service) calibrationByModel(classification TaskClassification) map[string]modelintelligence.ModelCalibration {
	result := map[string]modelintelligence.ModelCalibration{}
	if s.modelTelemetry == nil {
		return result
	}
	rows, err := s.modelTelemetry.LoadAll()
	if err != nil {
		return result
	}
	store := modelintelligence.NewTelemetryStore()
	store.Seed(rows)
	lane := routingLaneForClassification(classification)
	for _, model := range store.Calibration().Models {
		if model.Lane == lane {
			result[modelCalibrationKey(model.ProviderID, model.ModelID)] = model
		}
	}
	return result
}

func modelCalibrationKey(providerID, modelID string) string {
	return strings.TrimSpace(providerID) + "\x00" + strings.TrimSpace(modelID)
}

func routeCalibration(model *modelintelligence.ModelCalibration) *RouteCalibration {
	if model == nil {
		return nil
	}
	return &RouteCalibration{
		Lane: string(model.Lane), EvaluatedRuns: model.EvaluatedRuns,
		AcceptedOutputs: model.AcceptedOutputs, AcceptanceRate: model.AcceptanceRate,
		WilsonLowerBound: model.WilsonLowerBound, Confidence: model.Confidence,
	}
}

func routingLaneForClassification(classification TaskClassification) modelintelligence.RoutingLane {
	taskType := strings.ToLower(strings.TrimSpace(classification.TaskType))
	switch {
	case taskType == "high_stakes" || hasCapability(classification.RequiredCapabilities, "verification"):
		return modelintelligence.LaneVerifier
	case taskType == "architecture" || classification.Difficulty >= 4 || reasoningRank[classification.RequiredReasoning] >= reasoningRank["high"]:
		return modelintelligence.LaneRecursiveDeepReview
	case taskType == "extraction" || classification.Difficulty <= 1:
		return modelintelligence.LaneFastTriage
	default:
		return modelintelligence.LaneDrafting
	}
}

// strictProbeReason keeps optional strict routing fail-closed. It deliberately
// checks the latest result, not merely a historical success, because a later
// failed probe is fresh evidence that the endpoint is unavailable.
func (s *Service) strictProbeReason(provider Provider, policy Policy, now time.Time) string {
	return s.strictProbeReasonWithContext(context.Background(), provider, policy, now)
}

func (s *Service) strictProbeReasonWithContext(ctx context.Context, provider Provider, policy Policy, now time.Time) string {
	if !policy.RequireRecentLiveProviderProbe {
		return ""
	}
	if s.probeHistory == nil {
		return "strict live-probe policy cannot verify provider readiness"
	}
	probe, err := findLatestProviderProbeWithContext(ctx, s.probeHistory, provider.ID)
	if err != nil {
		return "strict live-probe policy could not read provider readiness history"
	}
	if probe == nil {
		return "provider has not passed a persisted live readiness check"
	}
	if !probe.Live {
		return "provider latest readiness check is not live"
	}
	checkedAt := probe.CheckedAt.UTC()
	if checkedAt.IsZero() || checkedAt.After(now.UTC()) {
		return "provider live readiness check has an invalid or future timestamp"
	}
	maxAge := time.Duration(policy.ProviderProbeMaxAgeSeconds) * time.Second
	if maxAge <= 0 || now.Sub(checkedAt) >= maxAge {
		return "provider live readiness check is stale"
	}
	return ""
}

func maintenanceModelSelectionKey(providerID, modelID string) string {
	return strings.TrimSpace(providerID) + "\x00" + strings.TrimSpace(modelID)
}

func unsuitableReason(provider Provider, model Model, classification TaskClassification, policy Policy, request RouteRequest) string {
	if !model.Enabled {
		return "model disabled"
	}
	if request.ValidationPassed != nil && !*request.ValidationPassed && request.PreviousModelID == model.ID {
		return "previous validation failed"
	}
	if model.MaxDifficulty > 0 && model.MaxDifficulty < classification.Difficulty {
		return "model difficulty limit too low"
	}
	if reasoningRank[model.MaxReasoning] < reasoningRank[classification.RequiredReasoning] {
		return "model reasoning level too weak"
	}
	for _, capability := range classification.RequiredCapabilities {
		if !hasCapability(model.Capabilities, capability) {
			return "missing capability: " + capability
		}
	}
	if provider.Paid && !policy.PaidCallsAllowed {
		return "paid usage disabled by policy"
	}
	if (provider.Paid || model.Tier == TierExpensive) && policy.RequireApprovalBeforePaidUsage {
		return "manual approval required before paid or expensive usage"
	}
	return ""
}

func classifyTask(request RouteRequest) TaskClassification {
	task := strings.ToLower(request.Task)
	taskType := firstNonEmpty(request.TaskType, "general")
	difficulty := request.Difficulty
	reasoning := request.RequiredReasoning
	capabilities := []string{"general"}
	reasons := []string{}

	if strings.Contains(task, "code") || strings.Contains(task, "bug") || strings.Contains(task, "compile") || strings.Contains(task, "api") {
		taskType = firstNonEmpty(request.TaskType, "coding")
		capabilities = append(capabilities, "coding")
		difficulty = maxInt(difficulty, 3)
		reasoning = maxReasoning(reasoning, "medium")
		reasons = append(reasons, "coding terms detected")
	}
	if strings.Contains(task, "architecture") || strings.Contains(task, "multi-agent") || strings.Contains(task, "autonomous") {
		taskType = firstNonEmpty(request.TaskType, "architecture")
		capabilities = append(capabilities, "planning")
		difficulty = maxInt(difficulty, 4)
		reasoning = maxReasoning(reasoning, "high")
		reasons = append(reasons, "architecture terms detected")
	}
	if strings.Contains(task, "legal") || strings.Contains(task, "financial") || strings.Contains(task, "medical") {
		taskType = firstNonEmpty(request.TaskType, "high_stakes")
		capabilities = append(capabilities, "verification")
		difficulty = maxInt(difficulty, 5)
		reasoning = maxReasoning(reasoning, "very_high")
		reasons = append(reasons, "high-stakes terms detected")
	}
	if strings.Contains(task, "summarize") || strings.Contains(task, "extract") || strings.Contains(task, "classify") {
		taskType = firstNonEmpty(request.TaskType, "extraction")
		capabilities = append(capabilities, "extraction")
		difficulty = maxInt(difficulty, 1)
		reasoning = maxReasoning(reasoning, "low")
		reasons = append(reasons, "simple extraction/classification terms detected")
	}
	if len(task) > 800 {
		difficulty = maxInt(difficulty, 4)
		reasoning = maxReasoning(reasoning, "high")
		reasons = append(reasons, "long task context")
	}
	if difficulty == 0 {
		difficulty = 2
	}
	if reasoning == "" {
		reasoning = "medium"
	}
	if len(reasons) == 0 {
		reasons = append(reasons, "default general task classification")
	}

	return TaskClassification{
		TaskType:             taskType,
		Difficulty:           difficulty,
		RequiredReasoning:    reasoning,
		RequiredCapabilities: uniqueStrings(capabilities),
		Reason:               strings.Join(reasons, "; "),
	}
}

func selectionReason(selected candidate, classification TaskClassification, inputTokens, outputTokens int, estimatedCostEUR float64) string {
	return fmt.Sprintf(
		"Selected the cheapest suitable %s-tier model after classifying the task as %s difficulty %d with %s reasoning. The model satisfies required capabilities: %s. Estimated route size: %d input / %d output tokens, EUR %.6f.",
		selected.model.Tier,
		classification.TaskType,
		classification.Difficulty,
		classification.RequiredReasoning,
		strings.Join(classification.RequiredCapabilities, ", "),
		inputTokens,
		outputTokens,
		estimatedCostEUR,
	)
}

func fallbackPath(candidates []candidate, inputTokens, outputTokens int) []FallbackOption {
	path := []FallbackOption{}
	for _, candidate := range candidates {
		path = append(path, FallbackOption{
			ProviderID:            candidate.provider.ID,
			ModelID:               candidate.model.ID,
			ModelName:             candidate.model.Name,
			Tier:                  candidate.model.Tier,
			EstimatedCostEUR:      estimateRouteCostEUR(candidate.model, inputTokens, outputTokens),
			EstimatedInputTokens:  inputTokens,
			EstimatedOutputTokens: outputTokens,
			RequiresApproval:      candidate.provider.Paid || candidate.model.RequiresApproval,
		})
	}
	return path
}

func fallbackLabels(options []FallbackOption) []string {
	labels := []string{}
	for _, option := range options {
		labels = append(labels, option.ProviderID+"/"+option.ModelID)
	}
	return labels
}

type providerReadiness struct {
	configured bool
	status     string
	reason     string
}

func annotatePolicyReadiness(policy Policy) Policy {
	for index := range policy.Providers {
		readiness := providerRuntimeReadiness(policy.Providers[index])
		policy.Providers[index].Configured = readiness.configured
		policy.Providers[index].ReadinessStatus = readiness.status
		policy.Providers[index].ReadinessReason = readiness.reason
	}
	return policy
}

func providerRuntimeReadiness(provider Provider) providerReadiness {
	if !provider.Enabled {
		return providerReadiness{configured: false, status: "disabled", reason: "provider disabled"}
	}
	endpoint := strings.TrimSpace(provider.EndpointURL)
	if endpoint == "" {
		return providerReadiness{configured: false, status: "not_configured", reason: "provider endpoint is not configured"}
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil {
		return providerReadiness{configured: false, status: "invalid_endpoint", reason: "provider endpoint must be an absolute http or https URL without embedded credentials"}
	}
	host := parsed.Hostname()
	if unsafeEndpointHost(host) && strings.ToLower(strings.TrimSpace(os.Getenv("LLM_ALLOW_LINK_LOCAL_ENDPOINTS"))) != "true" {
		return providerReadiness{configured: false, status: "blocked_endpoint", reason: "provider endpoint uses link-local, metadata, or unspecified address space"}
	}
	if parsed.Scheme == "https" && !provider.Local && unsafeRemoteHTTPSAddress(host) {
		return providerReadiness{configured: false, status: "blocked_endpoint", reason: "non-local HTTPS provider endpoints cannot target loopback or private IP addresses"}
	}
	if parsed.Scheme == "http" && !isLocalModelHost(host) {
		return providerReadiness{configured: false, status: "blocked_endpoint", reason: "remote provider endpoints must use HTTPS"}
	}
	if isLoopbackOnlyProvider(provider.ID) && !isLocalModelHost(host) {
		return providerReadiness{configured: false, status: "blocked_endpoint", reason: localProviderDisplayName(provider.ID) + " endpoint must use localhost, loopback, or host.docker.internal"}
	}
	if provider.Local && !isLocalModelHost(host) {
		return providerReadiness{configured: false, status: "blocked_endpoint", reason: "provider marked local must use localhost, loopback, or host.docker.internal"}
	}
	if provider.APIKeyEnv != "" && strings.TrimSpace(os.Getenv(provider.APIKeyEnv)) == "" {
		return providerReadiness{configured: false, status: "missing_api_key", reason: "required API key environment variable " + provider.APIKeyEnv + " is not set"}
	}
	return providerReadiness{configured: true, status: "configured", reason: "provider endpoint and required credentials are configured"}
}

func isLoopbackOnlyProvider(providerID string) bool {
	switch providerID {
	case "ollama", "lm-studio", "llama-cpp", "localai", "vllm", "sglang", "mistral-rs", "dspark", "litellm":
		return true
	default:
		return false
	}
}

func localProviderDisplayName(providerID string) string {
	switch providerID {
	case "ollama":
		return "Ollama"
	case "lm-studio":
		return "LM Studio"
	case "llama-cpp":
		return "llama.cpp"
	case "localai":
		return "LocalAI"
	case "vllm":
		return "vLLM"
	case "sglang":
		return "SGLang"
	case "mistral-rs":
		return "mistral.rs"
	case "dspark":
		return "DSpark"
	case "litellm":
		return "LiteLLM gateway"
	default:
		return "local provider"
	}
}

func generationStatusForReadiness(status string) string {
	switch status {
	case "blocked_endpoint":
		return "blocked"
	default:
		return "skipped"
	}
}

func unsafeEndpointHost(host string) bool {
	ip := net.ParseIP(strings.TrimSpace(host))
	if ip == nil {
		return false
	}
	return ip.IsUnspecified() || ip.IsLinkLocalUnicast()
}

func unsafeRemoteHTTPSAddress(host string) bool {
	host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	address, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	address = address.Unmap().WithZone("")
	return address.IsLoopback() || address.IsPrivate() || address.IsLinkLocalUnicast() || address.IsUnspecified() || address.IsMulticast()
}

// isLocalModelHost allows a local server on the host OS when HAI itself runs
// in Docker. It intentionally rejects LAN and public endpoints for llama.cpp:
// this provider is the explicit local/offline route, not a cloud gateway.
func isLocalModelHost(host string) bool {
	host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	if host == "localhost" || host == "host.docker.internal" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// Provider endpoints can receive prompts and source-backed context. Do not
// inherit machine proxy settings; dial-time DNS validation pins connections to
// the exact vetted IP while the request URL retains its TLS hostname.
var providerHTTPClient = newProviderHTTPClient(false)

func noRedirectHTTPClient() *http.Client {
	return providerHTTPClient
}

func isEventStreamResponse(resp *http.Response) bool {
	if resp == nil {
		return false
	}
	mediaType, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	return err == nil && strings.EqualFold(mediaType, "text/event-stream")
}

func redactProviderSecrets(provider Provider, value string) string {
	if provider.APIKeyEnv != "" {
		if key := strings.TrimSpace(os.Getenv(provider.APIKeyEnv)); key != "" {
			value = strings.ReplaceAll(value, key, "[REDACTED]")
		}
	}
	return safety.RedactSecrets(value)
}

func readLimitedResponseBody(body io.Reader, limit int64) ([]byte, error) {
	if limit < 0 {
		return nil, fmt.Errorf("response body limit cannot be negative")
	}
	data, err := io.ReadAll(io.LimitReader(body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("response body exceeds %d bytes", limit)
	}
	return data, nil
}

func buildPrompt(request GenerateRequest) string {
	parts := []string{}
	if request.SystemPrompt != "" {
		parts = append(parts, request.SystemPrompt)
	}
	if len(request.Context) > 0 {
		parts = append(parts, "Relevant context:")
		for _, item := range request.Context {
			item = strings.TrimSpace(item)
			if item != "" {
				parts = append(parts, "- "+item)
			}
		}
	}
	parts = append(parts, "Task:")
	parts = append(parts, strings.TrimSpace(request.Task))
	return strings.Join(parts, "\n")
}

func compactOutput(value []byte, limit int) string {
	text := strings.Join(strings.Fields(safety.RedactSecrets(string(value))), " ")
	if len(text) <= limit {
		return text
	}
	return text[:limit] + "..."
}

func intEnv(name string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	var parsed int
	if _, err := fmt.Sscanf(value, "%d", &parsed); err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func normalizeProbePolicy(policy Policy) Policy {
	if policy.ProviderProbeMaxAgeSeconds <= 0 {
		policy.ProviderProbeMaxAgeSeconds = 900
	}
	return policy
}

// configuredOllamaModels keeps the router's active model set explicit. The
// catalog can describe many supported families, but a configured endpoint must
// not cause the daily maintenance scheduler to pull every catalog entry.
func configuredOllamaModels() []Model {
	available := []Model{
		{ID: "phi3:mini", Name: "Phi small local", Tier: TierLocal, Capabilities: []string{"general", "classification", "extraction"}, MaxDifficulty: 2, MaxReasoning: "low"},
		{ID: "phi4:latest", Name: "Phi general local", Tier: TierLocal, Capabilities: []string{"general", "classification", "extraction"}, MaxDifficulty: 3, MaxReasoning: "medium"},
		{ID: "gemma3:4b", Name: "Gemma compact local", Tier: TierLocal, Capabilities: []string{"general", "classification", "summarization"}, MaxDifficulty: 3, MaxReasoning: "medium"},
		{ID: "gemma3:12b", Name: "Gemma capable local", Tier: TierLocal, Capabilities: []string{"general", "planning", "extraction"}, MaxDifficulty: 4, MaxReasoning: "high"},
		{ID: "mistral:7b", Name: "Mistral local", Tier: TierLocal, Capabilities: []string{"general", "planning", "summarization"}, MaxDifficulty: 4, MaxReasoning: "high"},
		{ID: "mixtral:8x7b", Name: "Mixtral local", Tier: TierLocal, Capabilities: []string{"general", "planning", "verification"}, MaxDifficulty: 5, MaxReasoning: "very_high"},
		{ID: "llama3.1:8b", Name: "Llama local", Tier: TierLocal, Capabilities: []string{"general", "planning", "extraction"}, MaxDifficulty: 4, MaxReasoning: "high"},
		{ID: "llama3.1:70b", Name: "Llama large local", Tier: TierLocal, Capabilities: []string{"general", "planning", "verification"}, MaxDifficulty: 5, MaxReasoning: "very_high"},
		{ID: "qwen2.5:7b", Name: "Qwen general local", Tier: TierLocal, Capabilities: []string{"general", "planning", "extraction"}, MaxDifficulty: 4, MaxReasoning: "high"},
		{ID: "qwen2.5-coder:7b", Name: "Qwen coder local", Tier: TierLocal, Capabilities: []string{"general", "coding", "extraction"}, MaxDifficulty: 4, MaxReasoning: "high"},
		{ID: "qwen2.5-coder:32b", Name: "Qwen coder large local", Tier: TierLocal, Capabilities: []string{"general", "coding", "planning", "verification"}, MaxDifficulty: 5, MaxReasoning: "very_high"},
		{ID: "deepseek-coder:6.7b", Name: "DeepSeek coder local", Tier: TierLocal, Capabilities: []string{"general", "coding", "planning"}, MaxDifficulty: 4, MaxReasoning: "high"},
		{ID: "deepseek-r1:8b", Name: "DeepSeek reasoning local", Tier: TierLocal, Capabilities: []string{"general", "planning", "verification"}, MaxDifficulty: 4, MaxReasoning: "high"},
	}
	byID := make(map[string]Model, len(available))
	for _, model := range available {
		byID[model.ID] = model
	}
	requested := strings.Split(strings.TrimSpace(os.Getenv("OLLAMA_MODEL_IDS")), ",")
	if len(requested) == 1 && strings.TrimSpace(requested[0]) == "" {
		requested = []string{"phi3:mini"}
	}
	seen := map[string]bool{}
	models := make([]Model, 0, len(requested))
	for _, rawID := range requested {
		id := strings.TrimSpace(rawID)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		model, known := byID[id]
		if !known {
			model = Model{ID: id, Name: "Configured Ollama local model", Tier: TierLocal, Capabilities: []string{"general", "coding", "planning", "verification", "extraction"}, MaxDifficulty: 5, MaxReasoning: "very_high"}
		}
		if isOllamaCloudModelID(id) {
			model.Name = "Configured Ollama cloud model (billing unknown)"
			model.Tier = TierUnknown
			model.RequiresApproval = true
		}
		model.Enabled = true
		models = append(models, model)
	}
	return models
}

func isOllamaCloudModelID(modelID string) bool {
	modelID = strings.ToLower(strings.TrimSpace(modelID))
	return strings.HasSuffix(modelID, ":cloud") || strings.HasSuffix(modelID, "-cloud")
}

func applyOllamaModelPolicy(provider Provider, model Model) (Provider, Model) {
	if provider.ID != "ollama" || !isOllamaCloudModelID(model.ID) {
		return provider, model
	}

	// The local Ollama endpoint can proxy inference to Ollama Cloud. Treat the
	// cloud tag as paid with unknown billing, independently of provider defaults
	// or model metadata supplied by configuration.
	provider.Local = false
	provider.Paid = true
	provider.QuotaRemaining = 0
	model.Tier = TierUnknown
	model.RequiresApproval = true
	if model.Name == "" || model.Name == "Configured Ollama local model" {
		model.Name = "Configured Ollama cloud model (billing unknown)"
	}
	return provider, model
}

func configuredLocalModelID(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func defaultPolicy() Policy {
	ollamaEndpoint := strings.TrimSpace(os.Getenv("OLLAMA_BASE_URL"))
	ollamaModels := configuredOllamaModels()
	lmStudioEndpoint := strings.TrimSpace(os.Getenv("LM_STUDIO_BASE_URL"))
	lmStudioModelID := configuredLocalModelID("LM_STUDIO_MODEL_ID", "local-model")
	llamaCPPEndpoint := strings.TrimSpace(os.Getenv("LLAMA_CPP_BASE_URL"))
	llamaCPPModelID := configuredLocalModelID("LLAMA_CPP_MODEL_ID", "local-model")
	localAIEndpoint := strings.TrimSpace(os.Getenv("LOCALAI_BASE_URL"))
	localAIModelID := configuredLocalModelID("LOCALAI_MODEL_ID", "localai-default")
	vLLMEndpoint := strings.TrimSpace(os.Getenv("VLLM_BASE_URL"))
	vLLMModelID := configuredLocalModelID("VLLM_MODEL_ID", "vllm-default")
	sglangEndpoint := strings.TrimSpace(os.Getenv("SGLANG_BASE_URL"))
	sglangModelID := configuredLocalModelID("SGLANG_MODEL_ID", "sglang-default")
	mistralRSEndpoint := strings.TrimSpace(os.Getenv("MISTRAL_RS_BASE_URL"))
	mistralRSModelID := configuredLocalModelID("MISTRAL_RS_MODEL_ID", "mistralrs-default")
	dsparkEndpoint := strings.TrimSpace(os.Getenv("DSPARK_BASE_URL"))
	dsparkModelID := configuredLocalModelID("DSPARK_MODEL_ID", "dspark-default")
	dsparkEnabled := false
	if parsed, err := url.Parse(dsparkEndpoint); err == nil && isLocalModelHost(parsed.Hostname()) {
		dsparkEnabled = envEnabled("DSPARK_ENABLED")
	}
	liteLLMEnabled := envEnabled("LITELLM_ENABLED")
	liteLLMEndpoint := strings.TrimSpace(os.Getenv("LITELLM_BASE_URL"))
	liteLLMModelID := strings.TrimSpace(os.Getenv("LITELLM_MODEL_ID"))
	if liteLLMModelID == "" {
		liteLLMModelID = "local-model"
	}
	odysseusEndpoint := strings.TrimSpace(os.Getenv("ODYSSEUS_BASE_URL"))
	odysseusAPIKeyEnv := ""
	if strings.TrimSpace(os.Getenv("ODYSSEUS_API_TOKEN")) != "" {
		odysseusAPIKeyEnv = "ODYSSEUS_API_TOKEN"
	}
	freeCloudEndpoint := strings.TrimSpace(os.Getenv("FREE_CLOUD_OPENAI_BASE_URL"))
	nousEndpoint := strings.TrimSpace(os.Getenv("NOUS_PORTAL_BASE_URL"))
	nousAPIKeyEnv := ""
	if strings.TrimSpace(os.Getenv("NOUS_PORTAL_API_KEY")) != "" {
		nousAPIKeyEnv = "NOUS_PORTAL_API_KEY"
	}
	mixtureEndpoint := strings.TrimSpace(os.Getenv("MIXTURE_OF_AGENTS_BASE_URL"))
	mixtureAPIKeyEnv := ""
	if strings.TrimSpace(os.Getenv("MIXTURE_OF_AGENTS_API_KEY")) != "" {
		mixtureAPIKeyEnv = "MIXTURE_OF_AGENTS_API_KEY"
	}
	openAICodexEndpoint := strings.TrimSpace(os.Getenv("OPENAI_CODEX_BASE_URL"))
	openAICodexAPIKeyEnv := ""
	if strings.TrimSpace(os.Getenv("OPENAI_CODEX_API_KEY")) != "" {
		openAICodexAPIKeyEnv = "OPENAI_CODEX_API_KEY"
	}
	return Policy{
		DailyPaidBudgetEUR:               0,
		PaidCallsAllowed:                 false,
		LocalModelsAllowed:               true,
		FreeCloudQuotaAllowed:            true,
		LocalFirst:                       true,
		CacheRepeatedPrompts:             true,
		RouteSimpleTasksToSmallModels:    true,
		RouteComplexTasksToBestFreeModel: true,
		RequireApprovalBeforePaidUsage:   true,
		RequireRecentLiveProviderProbe:   envEnabled("LLM_REQUIRE_RECENT_LIVE_PROBE"),
		ProviderProbeMaxAgeSeconds:       intEnv("LLM_PROVIDER_PROBE_MAX_AGE_SECONDS", 900),
		TierOrder:                        []string{TierLocal, TierFree, TierCheap, TierAcceptable, TierHigh, TierPremium, TierExpensive},
		Providers: []Provider{
			{
				ID:             "ollama",
				Name:           "Ollama local",
				Enabled:        true,
				Local:          true,
				Paid:           false,
				EndpointURL:    ollamaEndpoint,
				QuotaRemaining: -1,
				Models:         ollamaModels,
			},
			{
				ID:             "lm-studio",
				Name:           "LM Studio local server",
				Enabled:        true,
				Local:          true,
				Paid:           false,
				EndpointURL:    lmStudioEndpoint,
				QuotaRemaining: -1,
				Models: []Model{
					{ID: lmStudioModelID, Name: "Configured LM Studio local model", Tier: TierLocal, Capabilities: []string{"general", "coding", "planning", "verification", "extraction"}, MaxDifficulty: 5, MaxReasoning: "very_high", Enabled: true},
				},
			},
			{
				ID:             "llama-cpp",
				Name:           "llama.cpp local server",
				Enabled:        true,
				Local:          true,
				Paid:           false,
				EndpointURL:    llamaCPPEndpoint,
				QuotaRemaining: -1,
				Models: []Model{
					{ID: llamaCPPModelID, Name: "Configured llama.cpp GGUF model", Tier: TierLocal, Capabilities: []string{"general", "coding", "planning", "verification", "extraction"}, MaxDifficulty: 5, MaxReasoning: "very_high", Enabled: true},
				},
			},
			{
				ID:             "localai",
				Name:           "LocalAI local server",
				Enabled:        true,
				Local:          true,
				Paid:           false,
				EndpointURL:    localAIEndpoint,
				QuotaRemaining: -1,
				Models: []Model{
					{ID: localAIModelID, Name: "Configured LocalAI local model", Tier: TierLocal, Capabilities: []string{"general", "coding", "planning", "verification", "extraction"}, MaxDifficulty: 5, MaxReasoning: "very_high", Enabled: true},
				},
			},
			{
				ID:             "vllm",
				Name:           "vLLM local server",
				Enabled:        true,
				Local:          true,
				Paid:           false,
				EndpointURL:    vLLMEndpoint,
				QuotaRemaining: -1,
				Models: []Model{
					{ID: vLLMModelID, Name: "Configured vLLM local model", Tier: TierLocal, Capabilities: []string{"general", "coding", "planning", "verification", "extraction"}, MaxDifficulty: 5, MaxReasoning: "very_high", Enabled: true},
				},
			},
			{
				ID:             "sglang",
				Name:           "SGLang local server",
				Enabled:        true,
				Local:          true,
				Paid:           false,
				EndpointURL:    sglangEndpoint,
				QuotaRemaining: -1,
				Models: []Model{
					{ID: sglangModelID, Name: "Configured SGLang local model", Tier: TierLocal, Capabilities: []string{"general", "coding", "planning", "verification", "extraction"}, MaxDifficulty: 5, MaxReasoning: "very_high", Enabled: true},
				},
			},
			{
				ID:             "mistral-rs",
				Name:           "mistral.rs local server",
				Enabled:        true,
				Local:          true,
				Paid:           false,
				EndpointURL:    mistralRSEndpoint,
				QuotaRemaining: -1,
				Models: []Model{
					{ID: mistralRSModelID, Name: "Configured mistral.rs local model", Tier: TierLocal, Capabilities: []string{"general", "coding", "planning", "verification", "extraction"}, MaxDifficulty: 5, MaxReasoning: "very_high", Enabled: true},
				},
			},
			{
				ID:             "dspark",
				Name:           "DSpark local server",
				Enabled:        dsparkEnabled,
				Local:          true,
				Paid:           false,
				EndpointURL:    dsparkEndpoint,
				QuotaRemaining: -1,
				Models: []Model{
					{ID: dsparkModelID, Name: "Configured DSpark local model", Tier: TierLocal, Capabilities: []string{"general", "coding", "planning", "verification", "extraction"}, MaxDifficulty: 5, MaxReasoning: "very_high", Enabled: true},
				},
			},
			{
				ID:             "litellm",
				Name:           "LiteLLM local gateway",
				Enabled:        liteLLMEnabled,
				Local:          true,
				Paid:           false,
				EndpointURL:    liteLLMEndpoint,
				APIKeyEnv:      "LITELLM_API_KEY",
				QuotaRemaining: -1,
				Models: []Model{
					{ID: liteLLMModelID, Name: "Configured LiteLLM local model alias", Tier: TierLocal, Capabilities: []string{"general", "coding", "planning", "verification", "extraction"}, MaxDifficulty: 5, MaxReasoning: "very_high", RequiresApproval: true, Enabled: true},
				},
			},
			{
				ID:             "odysseus",
				Name:           "Odysseus AI Workspace",
				Enabled:        true,
				Local:          true,
				Paid:           false,
				EndpointURL:    odysseusEndpoint,
				APIKeyEnv:      odysseusAPIKeyEnv,
				QuotaRemaining: -1,
				Models: []Model{
					{ID: "odysseus-workspace-agent", Name: "Odysseus workspace agent", Tier: TierLocal, Capabilities: []string{"general", "planning", "verification", "extraction"}, MaxDifficulty: 4, MaxReasoning: "high", RequiresApproval: true, Enabled: true},
				},
			},
			{
				ID:             "free-cloud",
				Name:           "Configured free cloud quota",
				Enabled:        false,
				Local:          false,
				Paid:           false,
				EndpointURL:    freeCloudEndpoint,
				APIKeyEnv:      "FREE_CLOUD_API_KEY",
				QuotaRemaining: 0,
				Models: []Model{
					{ID: "free-best-available", Name: "Best configured free model", Tier: TierFree, Capabilities: []string{"general", "coding", "planning", "verification", "extraction"}, MaxDifficulty: 5, MaxReasoning: "very_high", Enabled: true},
					{ID: "free-fast-classifier", Name: "Free fast classifier", Tier: TierFree, Capabilities: []string{"general", "classification", "extraction"}, MaxDifficulty: 2, MaxReasoning: "low", Enabled: true},
					{ID: "free-coder", Name: "Free coder quota model", Tier: TierFree, Capabilities: []string{"general", "coding", "extraction"}, MaxDifficulty: 4, MaxReasoning: "high", Enabled: true},
				},
			},
			{
				ID:             "nous-portal",
				Name:           "Nous Portal",
				Enabled:        true,
				Local:          false,
				Paid:           true,
				EndpointURL:    nousEndpoint,
				APIKeyEnv:      nousAPIKeyEnv,
				QuotaRemaining: 0,
				DailyBudgetEUR: 0,
				Models:         nousPortalModels(),
			},
			{
				ID:             "mixture-of-agents",
				Name:           "Mixture of Agents",
				Enabled:        true,
				Local:          false,
				Paid:           true,
				EndpointURL:    mixtureEndpoint,
				APIKeyEnv:      mixtureAPIKeyEnv,
				QuotaRemaining: 0,
				DailyBudgetEUR: 0,
				Models: []Model{
					catalogModel("moa-default", "Default", TierPremium, []string{"general", "coding", "planning", "verification", "extraction"}, 5, "very_high", 2, 8, true, "default estimate for multi-agent orchestration; verify with provider invoice or override through LLM_PROVIDERS_JSON"),
				},
			},
			{
				ID:             "openai-codex",
				Name:           "OpenAI Codex",
				Enabled:        true,
				Local:          false,
				Paid:           true,
				EndpointURL:    openAICodexEndpoint,
				APIKeyEnv:      openAICodexAPIKeyEnv,
				QuotaRemaining: 0,
				DailyBudgetEUR: 0,
				Models: []Model{
					catalogModel("gpt-5.5", "GPT-5.5", TierPremium, []string{"general", "coding", "planning", "verification", "extraction"}, 5, "very_high", 5, 15, true, "default estimate; verify with OpenAI/Codex invoice or override through LLM_PROVIDERS_JSON"),
					catalogModel("gpt-5.4", "GPT-5.4", TierHigh, []string{"general", "coding", "planning", "verification", "extraction"}, 5, "very_high", 3, 10, true, "default estimate; verify with OpenAI/Codex invoice or override through LLM_PROVIDERS_JSON"),
					catalogModel("gpt-5.4-mini", "GPT-5.4-mini", TierCheap, []string{"general", "coding", "planning", "extraction"}, 4, "high", 0.25, 1, true, "default estimate; verify with OpenAI/Codex invoice or override through LLM_PROVIDERS_JSON"),
					catalogModel("gpt-5.3-codex-spark", "GPT-5.3-codex-spark", TierCheap, []string{"general", "coding", "extraction"}, 4, "high", 0.15, 0.6, true, "default estimate; verify with OpenAI/Codex invoice or override through LLM_PROVIDERS_JSON"),
				},
			},
			{
				ID:             "cheap-provider",
				Name:           "Cheap API provider",
				Enabled:        false,
				Local:          false,
				Paid:           true,
				QuotaRemaining: 0,
				DailyBudgetEUR: 0,
				Models: []Model{
					{ID: "cheap-small", Name: "Cheap small model", Tier: TierCheap, Capabilities: []string{"general", "classification", "extraction"}, MaxDifficulty: 3, MaxReasoning: "medium", EstimatedCostEUR: 0.001, RequiresApproval: true, Enabled: true},
					{ID: "cheap-coder", Name: "Cheap coder model", Tier: TierCheap, Capabilities: []string{"general", "coding", "extraction"}, MaxDifficulty: 4, MaxReasoning: "high", EstimatedCostEUR: 0.003, RequiresApproval: true, Enabled: true},
				},
			},
			{
				ID:             "acceptable-provider",
				Name:           "Acceptable API provider",
				Enabled:        false,
				Local:          false,
				Paid:           true,
				QuotaRemaining: 0,
				DailyBudgetEUR: 0,
				Models: []Model{
					{ID: "acceptable-general", Name: "Acceptable general model", Tier: TierAcceptable, Capabilities: []string{"general", "planning", "extraction"}, MaxDifficulty: 4, MaxReasoning: "high", EstimatedCostEUR: 0.01, RequiresApproval: true, Enabled: true},
					{ID: "acceptable-coder", Name: "Acceptable coder model", Tier: TierAcceptable, Capabilities: []string{"general", "coding", "planning"}, MaxDifficulty: 4, MaxReasoning: "high", EstimatedCostEUR: 0.015, RequiresApproval: true, Enabled: true},
				},
			},
			{
				ID:             "high-provider",
				Name:           "High capability API provider",
				Enabled:        false,
				Local:          false,
				Paid:           true,
				QuotaRemaining: 0,
				DailyBudgetEUR: 0,
				Models: []Model{
					{ID: "high-reasoning", Name: "High reasoning model", Tier: TierHigh, Capabilities: []string{"general", "coding", "planning", "verification", "extraction"}, MaxDifficulty: 5, MaxReasoning: "very_high", EstimatedCostEUR: 0.03, RequiresApproval: true, Enabled: true},
				},
			},
			{
				ID:             "premium-provider",
				Name:           "Premium API provider",
				Enabled:        false,
				Local:          false,
				Paid:           true,
				QuotaRemaining: 0,
				DailyBudgetEUR: 0,
				Models: []Model{
					{ID: "premium-best", Name: "Premium best-available model", Tier: TierPremium, Capabilities: []string{"general", "coding", "planning", "verification", "extraction"}, MaxDifficulty: 5, MaxReasoning: "very_high", EstimatedCostEUR: 0.04, RequiresApproval: true, Enabled: true},
				},
			},
			{
				ID:             "paid-provider",
				Name:           "Custom paid provider (disabled by policy)",
				Enabled:        false,
				Local:          false,
				Paid:           true,
				QuotaRemaining: 0,
				DailyBudgetEUR: 0,
				Models: []Model{
					{ID: "paid-high-capability", Name: "Paid high capability model", Tier: TierExpensive, Capabilities: []string{"general", "coding", "planning", "verification", "extraction"}, MaxDifficulty: 5, MaxReasoning: "very_high", EstimatedCostEUR: 0.05, RequiresApproval: true, Enabled: true},
					{ID: "expensive-frontier", Name: "Expensive frontier model", Tier: TierExpensive, Capabilities: []string{"general", "coding", "planning", "verification", "extraction"}, MaxDifficulty: 5, MaxReasoning: "very_high", EstimatedCostEUR: 0.08, RequiresApproval: true, Enabled: true},
				},
			},
		},
	}
}

func nousPortalModels() []Model {
	frontier := []string{"general", "coding", "planning", "verification", "extraction"}
	reasoning := []string{"general", "planning", "verification", "extraction"}
	coding := []string{"general", "coding", "planning", "extraction"}
	fast := []string{"general", "classification", "extraction", "summarization"}

	return []Model{
		nousModel("opus-4.8", "Opus 4.8", TierExpensive, frontier, 5, "very_high", 15, 75),
		nousModel("sonnet-4.6", "Sonnet 4.6", TierHigh, frontier, 5, "very_high", 3, 15),
		nousModel("haiku-4.5", "Haiku 4.5", TierAcceptable, fast, 3, "medium", 0.8, 4),
		nousModel("gpt-5.5", "GPT-5.5", TierPremium, frontier, 5, "very_high", 5, 15),
		nousModel("gpt-5.5-pro", "GPT-5.5-pro", TierExpensive, frontier, 5, "very_high", 15, 60),
		nousModel("gpt-5.4-mini", "GPT-5.4-mini", TierCheap, coding, 3, "medium", 0.25, 1),
		nousModel("gemini-3-pro-preview", "Gemini 3 pro Preview", TierPremium, reasoning, 5, "very_high", 2.5, 15),
		nousModel("gemini-3.1-pro-preview", "Gemini 3.1 pro Preview", TierHigh, reasoning, 5, "very_high", 1.25, 10),
		nousModel("gemini-3.5-flash", "Gemini 3.5 flash", TierCheap, fast, 3, "medium", 0.3, 2.5),
		nousModel("grok-4.3", "Grok 4.3", TierHigh, frontier, 5, "very_high", 3, 15),
		nousModel("deepseek-v4-pro", "Deepseek V4 Pro", TierHigh, frontier, 5, "very_high", 0.55, 2.2),
		nousModel("deepseek-v4-flash", "Deepseek V4 Flash", TierCheap, coding, 4, "high", 0.14, 0.28),
		nousModel("qwen3.7-max", "Qwen3.7 Max", TierPremium, frontier, 5, "very_high", 1.6, 6.4),
		nousModel("qwen3.7-plus", "Qwen3.7 Plus", TierAcceptable, coding, 4, "high", 0.4, 1.2),
		nousModel("qwen3.6-35b-a3b", "Qwen3.6 35b A3b", TierCheap, coding, 4, "high", 0.2, 0.8),
		nousModel("kimi-k2.7-code", "Kimi K2.7 Code", TierHigh, coding, 5, "very_high", 0.6, 2.4),
		nousModel("minimax-m3", "Minimax M3", TierHigh, frontier, 5, "very_high", 0.5, 2),
		nousModel("glm-5.2", "Glm 5.2", TierHigh, reasoning, 5, "very_high", 0.5, 2),
		nousModel("glm-5.1", "Glm 5.1", TierAcceptable, reasoning, 4, "high", 0.25, 1),
		nousModel("mimo-v2.5-pro", "Mimo V2.5 Pro", TierHigh, frontier, 5, "very_high", 0.8, 3.2),
		nousModel("hy3-preview", "Hy3 Preview", TierPremium, reasoning, 5, "very_high", 1, 4),
		nousModel("step-3.7-flash", "Step 3.7 Flash", TierCheap, fast, 3, "medium", 0.2, 0.8),
		nousModel("nemotron-3-super-120b-a12b", "Nemotron 3 Super 120b A12b", TierPremium, frontier, 5, "very_high", 0.8, 3.2),
		catalogModel("step-3.7-flash-free", "Step 3.7 Flash:Free", TierFree, fast, 3, "medium", 0, 0, true, "free quota estimate; verify availability and quota with Nous Portal or override through LLM_PROVIDERS_JSON"),
	}
}

func nousModel(id, name, tier string, capabilities []string, maxDifficulty int, maxReasoning string, inputCostPerMillion, outputCostPerMillion float64) Model {
	return catalogModel(id, name, tier, capabilities, maxDifficulty, maxReasoning, inputCostPerMillion, outputCostPerMillion, true, "default estimate; verify with provider invoice or override through LLM_PROVIDERS_JSON")
}

func catalogModel(id, name, tier string, capabilities []string, maxDifficulty int, maxReasoning string, inputCostPerMillion, outputCostPerMillion float64, requiresApproval bool, pricingSource string) Model {
	return Model{
		ID:                            id,
		Name:                          name,
		Tier:                          tier,
		Capabilities:                  capabilities,
		MaxDifficulty:                 maxDifficulty,
		MaxReasoning:                  maxReasoning,
		EstimatedCostEUR:              estimateDefaultRouteCostEUR(inputCostPerMillion, outputCostPerMillion),
		InputCostPerMillionTokensEUR:  inputCostPerMillion,
		OutputCostPerMillionTokensEUR: outputCostPerMillion,
		PricingUnit:                   "EUR per 1M tokens",
		PricingSource:                 pricingSource,
		RequiresApproval:              requiresApproval,
		Enabled:                       true,
	}
}

func estimateDefaultRouteCostEUR(inputCostPerMillion, outputCostPerMillion float64) float64 {
	return costFromMillionTokenPrice(inputCostPerMillion, 1000) + costFromMillionTokenPrice(outputCostPerMillion, 1000)
}

func odysseusExecutionBlockedReason() string {
	return "Odysseus workspace is connected for discovery and health probing only; agent task execution must go through a reviewed controlled runtime adapter and approval queue before HAI may invoke Odysseus tools"
}

func hasCapability(capabilities []string, capability string) bool {
	for _, existing := range capabilities {
		if existing == capability {
			return true
		}
	}
	return false
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}

func maxReasoning(left, right string) string {
	if reasoningRank[left] >= reasoningRank[right] {
		return left
	}
	return right
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	result := []string{}
	for _, value := range values {
		if !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}
