package modelintelligence

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"sync"
	"time"
)

type ValidationStatus string

type TokenUsageSource string

const (
	TokenUsageEstimated               TokenUsageSource = "estimated"
	TokenUsageProviderReported        TokenUsageSource = "provider_reported"
	TokenUsageProviderReportedPartial TokenUsageSource = "provider_reported_partial"
	TokenUsageEstimatedUncertain      TokenUsageSource = "estimated_uncertain"
	TokenUsageProviderReportInvalid   TokenUsageSource = "provider_report_invalid"
)

func normalizeTokenUsageSource(source TokenUsageSource) TokenUsageSource {
	switch source {
	case TokenUsageProviderReported, TokenUsageProviderReportedPartial, TokenUsageEstimatedUncertain, TokenUsageProviderReportInvalid:
		return source
	default:
		return TokenUsageEstimated
	}
}

const (
	ValidationUnvalidated     ValidationStatus = "unvalidated"
	ValidationSchemaValidated ValidationStatus = "schema_validated"
	ValidationSourceSupported ValidationStatus = "source_supported"
	ValidationTestPassed      ValidationStatus = "test_passed"
	ValidationHumanApproved   ValidationStatus = "human_approved"
	ValidationVerified        ValidationStatus = "verified"
	ValidationFailed          ValidationStatus = "failed"
	ValidationNeedsReview     ValidationStatus = "needs_review"
)

func (status ValidationStatus) accepted() bool {
	switch status {
	case ValidationSchemaValidated, ValidationSourceSupported, ValidationTestPassed,
		ValidationHumanApproved, ValidationVerified:
		return true
	default:
		return false
	}
}

func (status ValidationStatus) evaluated() bool {
	return status != "" && status != ValidationUnvalidated
}

func normalizeValidationStatus(status ValidationStatus) ValidationStatus {
	switch status {
	case ValidationSchemaValidated, ValidationSourceSupported, ValidationTestPassed,
		ValidationHumanApproved, ValidationVerified, ValidationFailed, ValidationNeedsReview:
		return status
	default:
		return ValidationUnvalidated
	}
}

// ModelRunTelemetry is one observed model call (§10.9 telemetry transaction).
// It is only ever created from a real call — never fabricated.
type ModelRunTelemetry struct {
	ID               string           `json:"id"`
	ProviderID       string           `json:"providerId"`
	ModelID          string           `json:"modelId"`
	Lane             RoutingLane      `json:"lane"`
	OperationID      string           `json:"operationId,omitempty"`
	InputTokens      int              `json:"inputTokens"`
	OutputTokens     int              `json:"outputTokens"`
	UsageSource      TokenUsageSource `json:"usageSource"`
	DurationMs       int64            `json:"durationMs"`
	TokensPerSecond  float64          `json:"tokensPerSecond"`
	OK               bool             `json:"ok"`
	CacheHit         bool             `json:"cacheHit"`
	ValidationStatus ValidationStatus `json:"validationStatus"`
	ValidationMethod string           `json:"validationMethod,omitempty"`
	EstimatedCostEUR float64          `json:"estimatedCostEur"`
	FallbackDepth    int              `json:"fallbackDepth"`
	CreatedAt        time.Time        `json:"createdAt"`
}

// TelemetryStore is an in-process store of model-run telemetry. It fronts an
// optional durable repository: rows are persisted on Record and seeded on start,
// so telemetry survives restart while queries stay in-memory-fast.
type TelemetryStore struct {
	mu             sync.Mutex
	records        []ModelRunTelemetry
	newID          func() (string, error)
	persist        func(ModelRunTelemetry) // optional legacy durable sink
	persistChecked func(ModelRunTelemetry) error
}

// NewTelemetryStore builds an empty store.
func NewTelemetryStore() *TelemetryStore { return &TelemetryStore{newID: newTelemetryID} }

func newTelemetryID() (string, error) {
	var id [16]byte
	if _, err := io.ReadFull(rand.Reader, id[:]); err != nil {
		return "", fmt.Errorf("create telemetry ID: %w", err)
	}
	return "mrt-" + hex.EncodeToString(id[:]), nil
}

// SetPersist installs a durable sink called for every recorded row.
func (s *TelemetryStore) SetPersist(fn func(ModelRunTelemetry)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.persist = fn
	s.persistChecked = nil
}

// SetPersistChecked installs a sink whose failures prevent a row from being
// reported as durably recorded.
func (s *TelemetryStore) SetPersistChecked(fn func(ModelRunTelemetry) error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.persistChecked = fn
	s.persist = nil
}

// Seed loads pre-existing (durable) telemetry into the store on startup.
func (s *TelemetryStore) Seed(rows []ModelRunTelemetry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for index := range rows {
		rows[index].ValidationStatus = normalizeValidationStatus(rows[index].ValidationStatus)
		rows[index].UsageSource = normalizeTokenUsageSource(rows[index].UsageSource)
	}
	s.records = append(s.records, rows...)
}

// Replace atomically refreshes the in-process view from the durable ledger.
// Other engines may write to that ledger without sharing this process-local
// store, so read paths use this instead of appending duplicate seed rows.
func (s *TelemetryStore) Replace(rows []ModelRunTelemetry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for index := range rows {
		rows[index].ValidationStatus = normalizeValidationStatus(rows[index].ValidationStatus)
		rows[index].UsageSource = normalizeTokenUsageSource(rows[index].UsageSource)
	}
	s.records = append([]ModelRunTelemetry{}, rows...)
}

// Record appends a telemetry row (persisting it if a sink is set) and returns it
// with an assigned id.
func (s *TelemetryStore) Record(t ModelRunTelemetry) ModelRunTelemetry {
	recorded, _ := s.RecordChecked(t)
	return recorded
}

// RecordChecked persists before publishing the row in the in-process view.
// Holding the store lock preserves ID/order consistency across concurrent runs.
func (s *TelemetryStore) RecordChecked(t ModelRunTelemetry) (ModelRunTelemetry, error) {
	s.mu.Lock()
	if t.ID == "" {
		newID := s.newID
		if newID == nil {
			newID = newTelemetryID
		}
		id, err := newID()
		if err != nil {
			s.mu.Unlock()
			return t, err
		}
		t.ID = id
	}
	t.ValidationStatus = normalizeValidationStatus(t.ValidationStatus)
	t.UsageSource = normalizeTokenUsageSource(t.UsageSource)
	if s.persistChecked != nil {
		if err := s.persistChecked(t); err != nil {
			s.mu.Unlock()
			return t, err
		}
	}
	s.records = append(s.records, t)
	persist := s.persist
	s.mu.Unlock()
	if persist != nil {
		persist(t)
	}
	return t, nil
}

// All returns a snapshot of all telemetry.
func (s *TelemetryStore) All() []ModelRunTelemetry {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]ModelRunTelemetry, len(s.records))
	copy(out, s.records)
	return out
}

// LaneWinner is the best observed model for a lane by tokens/sec.
type LaneWinner struct {
	Lane                 RoutingLane `json:"lane"`
	ProviderID           string      `json:"providerId"`
	ModelID              string      `json:"modelId"`
	TokensPerSecond      float64     `json:"tokensPerSecond"`
	ObservedSpeedSamples int         `json:"observedSpeedSamples"`
	Runs                 int         `json:"runs"`
	EvaluatedRuns        int         `json:"evaluatedRuns"`
	AcceptedOutputs      int         `json:"acceptedOutputs"`
	AcceptanceRate       float64     `json:"acceptanceRate"`
	Confidence           string      `json:"confidence"`
	AverageTokens        float64     `json:"averageTokens"`
	AverageDurationMs    float64     `json:"averageDurationMs"`
	AverageCostEUR       float64     `json:"averageCostEur"`
	Reason               string      `json:"reason"`
}

// LaneWinners computes the fastest observed model per lane from telemetry only
// (§dashboard: model lane winners). Lanes with no observed successful run are
// omitted — no winner is invented.
func (s *TelemetryStore) LaneWinners() []LaneWinner {
	return s.Calibration().LaneLeaders
}
