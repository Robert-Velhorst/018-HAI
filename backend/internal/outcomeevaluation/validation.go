package outcomeevaluation

import (
	"encoding/hex"
	"fmt"
	"math"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"automation-hub-backend/internal/lifeontology"
)

const (
	maxIDRunes        = 256
	maxTextRunes      = 8000
	maxIndicators     = 100
	maxObservations   = 10000
	maxCorrections    = 10000
	maxSources        = 100
	maxWindowDuration = 10 * 365 * 24 * time.Hour
)

var (
	sha256Pattern  = regexp.MustCompile(`^[a-f0-9]{64}$`)
	secretPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)-----BEGIN [A-Z ]*PRIVATE KEY-----`),
		regexp.MustCompile(`(?i)\b(?:password|passwd|api[_-]?key|access[_-]?token|client[_-]?secret|secret)\s*[:=]\s*[^\s,;]{4,}`),
		regexp.MustCompile(`(?i)\bbearer\s+[a-z0-9._~+/=-]{8,}`),
		regexp.MustCompile(`\b(?:ghp_|github_pat_|sk-)[A-Za-z0-9_-]{12,}`),
		regexp.MustCompile(`\bAKIA[A-Z0-9]{16}\b`),
	}
)

func normalizeAndValidate(request EvaluationRequest) (EvaluationRequest, error) {
	request.Observations = append([]Observation(nil), request.Observations...)
	request.Corrections = append([]UserCorrection(nil), request.Corrections...)
	request.AsOf = request.AsOf.UTC()
	request.Outcome = normalizeOutcome(request.Outcome)
	for i := range request.Observations {
		request.Observations[i] = normalizeObservation(request.Observations[i])
	}
	for i := range request.Corrections {
		request.Corrections[i] = normalizeCorrection(request.Corrections[i])
	}
	sort.Slice(request.Observations, func(i, j int) bool {
		if request.Observations[i].IndicatorID != request.Observations[j].IndicatorID {
			return request.Observations[i].IndicatorID < request.Observations[j].IndicatorID
		}
		if !request.Observations[i].ObservedAt.Equal(request.Observations[j].ObservedAt) {
			return request.Observations[i].ObservedAt.Before(request.Observations[j].ObservedAt)
		}
		return request.Observations[i].ID < request.Observations[j].ID
	})
	sort.Slice(request.Corrections, func(i, j int) bool {
		if request.Corrections[i].ObservationID != request.Corrections[j].ObservationID {
			return request.Corrections[i].ObservationID < request.Corrections[j].ObservationID
		}
		if !request.Corrections[i].CorrectedAt.Equal(request.Corrections[j].CorrectedAt) {
			return request.Corrections[i].CorrectedAt.Before(request.Corrections[j].CorrectedAt)
		}
		return request.Corrections[i].ID < request.Corrections[j].ID
	})
	if err := validateRequest(request); err != nil {
		return EvaluationRequest{}, err
	}
	return request, nil
}

func normalizeAndValidateOutcome(value IntendedOutcome, recordedAt time.Time) (IntendedOutcome, error) {
	value = normalizeOutcome(value)
	recordedAt = recordedAt.UTC()
	if recordedAt.IsZero() {
		return IntendedOutcome{}, invalid("recorded-at time is required")
	}
	if err := validateScope(value.Scope); err != nil {
		return IntendedOutcome{}, err
	}
	if err := validateText("outcome id", value.ID, maxIDRunes, true); err != nil {
		return IntendedOutcome{}, err
	}
	if err := validateText("outcome statement", value.Statement, maxTextRunes, true); err != nil {
		return IntendedOutcome{}, err
	}
	if !lifeontology.IsValidDomain(value.LifeDomain) {
		return IntendedOutcome{}, invalid("a supported life domain is required")
	}
	window := value.Window
	if window.Start.IsZero() || window.End.IsZero() || !window.Start.Before(window.End) || window.End.Sub(window.Start) > maxWindowDuration {
		return IntendedOutcome{}, fmt.Errorf("%w: start must precede end and duration must not exceed ten years", ErrInvalidTimeWindow)
	}
	if len(value.Indicators) == 0 || len(value.Indicators) > maxIndicators {
		return IntendedOutcome{}, invalid("between one and %d indicators are required", maxIndicators)
	}
	indicatorIDs := make(map[string]struct{}, len(value.Indicators))
	for _, indicator := range value.Indicators {
		if _, exists := indicatorIDs[indicator.ID]; exists {
			return IntendedOutcome{}, invalid("duplicate indicator id %q", indicator.ID)
		}
		indicatorIDs[indicator.ID] = struct{}{}
		if err := validateIndicator(value.Scope, window, recordedAt, indicator); err != nil {
			return IntendedOutcome{}, err
		}
	}
	return value, nil
}

func normalizeOutcome(value IntendedOutcome) IntendedOutcome {
	value.Indicators = append([]Indicator(nil), value.Indicators...)
	value.ID = strings.TrimSpace(value.ID)
	value.Scope = normalizeScope(value.Scope)
	value.Statement = strings.TrimSpace(value.Statement)
	value.Window.Start = value.Window.Start.UTC()
	value.Window.End = value.Window.End.UTC()
	for i := range value.Indicators {
		indicator := &value.Indicators[i]
		indicator.ID = strings.TrimSpace(indicator.ID)
		indicator.Name = strings.TrimSpace(indicator.Name)
		indicator.Unit = strings.TrimSpace(indicator.Unit)
		indicator.Baseline.ID = strings.TrimSpace(indicator.Baseline.ID)
		indicator.Baseline.Scope = normalizeScope(indicator.Baseline.Scope)
		indicator.Baseline.ObservedAt = indicator.Baseline.ObservedAt.UTC()
		indicator.Baseline.Sources = normalizeSources(indicator.Baseline.Sources)
	}
	sort.Slice(value.Indicators, func(i, j int) bool { return value.Indicators[i].ID < value.Indicators[j].ID })
	return value
}

func normalizeObservation(value Observation) Observation {
	value.ID = strings.TrimSpace(value.ID)
	value.Scope = normalizeScope(value.Scope)
	value.IndicatorID = strings.TrimSpace(value.IndicatorID)
	value.ObservedAt = value.ObservedAt.UTC()
	value.RecordedAt = value.RecordedAt.UTC()
	value.Sources = normalizeSources(value.Sources)
	value.Attribution.Rationale = strings.TrimSpace(value.Attribution.Rationale)
	return value
}

func normalizeCorrection(value UserCorrection) UserCorrection {
	value.ID = strings.TrimSpace(value.ID)
	value.Scope = normalizeScope(value.Scope)
	value.ObservationID = strings.TrimSpace(value.ObservationID)
	value.ActorID = strings.TrimSpace(value.ActorID)
	value.Reason = strings.TrimSpace(value.Reason)
	value.CorrectedAt = value.CorrectedAt.UTC()
	value.Sources = normalizeSources(value.Sources)
	return value
}

func normalizeScope(value Scope) Scope {
	value.OwnerID = strings.TrimSpace(value.OwnerID)
	value.WorkspaceID = strings.TrimSpace(value.WorkspaceID)
	return value
}

func normalizeSources(values []SourceReference) []SourceReference {
	result := append([]SourceReference(nil), values...)
	for i := range result {
		result[i].ID = strings.TrimSpace(result[i].ID)
		result[i].URI = strings.TrimSpace(result[i].URI)
		result[i].ContentDigest = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(result[i].ContentDigest), "sha256:"))
		result[i].RetrievedAt = result[i].RetrievedAt.UTC()
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}

func validateRequest(request EvaluationRequest) error {
	if request.AsOf.IsZero() {
		return invalid("as-of time is required")
	}
	if err := validateScope(request.Outcome.Scope); err != nil {
		return err
	}
	if err := validateText("outcome id", request.Outcome.ID, maxIDRunes, true); err != nil {
		return err
	}
	if err := validateText("outcome statement", request.Outcome.Statement, maxTextRunes, true); err != nil {
		return err
	}
	window := request.Outcome.Window
	if window.Start.IsZero() || window.End.IsZero() || !window.Start.Before(window.End) || window.End.Sub(window.Start) > maxWindowDuration || request.AsOf.Before(window.Start) {
		return fmt.Errorf("%w: start must precede end, duration must not exceed ten years, and as-of must not precede start", ErrInvalidTimeWindow)
	}
	if len(request.Outcome.Indicators) == 0 || len(request.Outcome.Indicators) > maxIndicators {
		return invalid("between one and %d indicators are required", maxIndicators)
	}
	if len(request.Observations) > maxObservations || len(request.Corrections) > maxCorrections {
		return invalid("observation or correction collection exceeds its limit")
	}

	indicatorIDs := make(map[string]struct{}, len(request.Outcome.Indicators))
	for _, indicator := range request.Outcome.Indicators {
		if _, exists := indicatorIDs[indicator.ID]; exists {
			return invalid("duplicate indicator id %q", indicator.ID)
		}
		indicatorIDs[indicator.ID] = struct{}{}
		if err := validateIndicator(request.Outcome.Scope, window, request.AsOf, indicator); err != nil {
			return err
		}
	}

	observations := make(map[string]Observation, len(request.Observations))
	for _, observation := range request.Observations {
		if _, exists := observations[observation.ID]; exists {
			return invalid("duplicate observation id %q", observation.ID)
		}
		if _, exists := indicatorIDs[observation.IndicatorID]; !exists {
			return invalid("observation %q references unknown indicator %q", observation.ID, observation.IndicatorID)
		}
		if err := validateObservation(request.Outcome.Scope, window, request.AsOf, observation); err != nil {
			return err
		}
		observations[observation.ID] = observation
	}

	correctionIDs := make(map[string]struct{}, len(request.Corrections))
	for _, correction := range request.Corrections {
		if _, exists := correctionIDs[correction.ID]; exists {
			return invalid("duplicate correction id %q", correction.ID)
		}
		correctionIDs[correction.ID] = struct{}{}
		observation, exists := observations[correction.ObservationID]
		if !exists {
			return invalid("correction %q references unknown observation %q", correction.ID, correction.ObservationID)
		}
		if err := validateCorrection(request.Outcome.Scope, request.AsOf, observation, correction); err != nil {
			return err
		}
	}
	return nil
}

func validateIndicator(scope Scope, window LongitudinalWindow, asOf time.Time, value Indicator) error {
	if err := validateText("indicator id", value.ID, maxIDRunes, true); err != nil {
		return err
	}
	if err := validateText("indicator name", value.Name, maxTextRunes, true); err != nil {
		return err
	}
	if err := validateText("indicator unit", value.Unit, maxIDRunes, true); err != nil {
		return err
	}
	if value.Direction != DirectionHigher && value.Direction != DirectionLower && value.Direction != DirectionMaintain {
		return invalid("indicator %q has unsupported direction %q", value.ID, value.Direction)
	}
	if !finite(value.TargetValue) || !finite(value.TargetTolerance) || !finite(value.TrendThresholdPerDay) || !finite(value.RegressionThreshold) || value.TargetTolerance < 0 || value.TrendThresholdPerDay < 0 || value.RegressionThreshold <= 0 {
		return invalid("indicator %q has invalid numeric thresholds", value.ID)
	}
	if value.MinimumObservations < 2 || value.MinimumObservations > maxObservations {
		return invalid("indicator %q minimum observations must be between 2 and %d", value.ID, maxObservations)
	}
	baseline := value.Baseline
	if err := validateText("baseline id", baseline.ID, maxIDRunes, true); err != nil {
		return err
	}
	if err := requireScope(scope, baseline.Scope, "baseline "+baseline.ID); err != nil {
		return err
	}
	if !finite(baseline.Value) || baseline.ObservedAt.IsZero() || baseline.ObservedAt.After(window.Start) || baseline.ObservedAt.After(asOf) {
		return fmt.Errorf("%w: baseline %q must be finite and observed no later than the window start", ErrInvalidTimeWindow, baseline.ID)
	}
	return validateEvidence("baseline "+baseline.ID, baseline.Verification, baseline.Sources, asOf)
}

func validateObservation(scope Scope, window LongitudinalWindow, asOf time.Time, value Observation) error {
	if err := validateText("observation id", value.ID, maxIDRunes, true); err != nil {
		return err
	}
	if err := validateText("observation indicator id", value.IndicatorID, maxIDRunes, true); err != nil {
		return err
	}
	if err := requireScope(scope, value.Scope, "observation "+value.ID); err != nil {
		return err
	}
	if !finite(value.Value) {
		return invalid("observation %q value must be finite", value.ID)
	}
	if value.ObservedAt.IsZero() || value.RecordedAt.IsZero() || value.ObservedAt.Before(window.Start) || value.ObservedAt.After(window.End) || value.ObservedAt.After(asOf) || value.RecordedAt.Before(value.ObservedAt) || value.RecordedAt.After(asOf) {
		return fmt.Errorf("%w: observation %q falls outside the evaluation snapshot", ErrInvalidTimeWindow, value.ID)
	}
	if err := validateEvidence("observation "+value.ID, value.Verification, value.Sources, asOf); err != nil {
		return err
	}
	if !validAttributionMethod(value.Attribution.Method) || !finite(value.Attribution.Confidence) || value.Attribution.Confidence < 0 || value.Attribution.Confidence > 1 {
		return invalid("observation %q has invalid attribution", value.ID)
	}
	if err := validateText("attribution rationale", value.Attribution.Rationale, maxTextRunes, value.Attribution.Method != AttributionUnknown); err != nil {
		return err
	}
	return nil
}

func validateCorrection(scope Scope, asOf time.Time, observation Observation, value UserCorrection) error {
	if err := validateText("correction id", value.ID, maxIDRunes, true); err != nil {
		return err
	}
	if err := requireScope(scope, value.Scope, "correction "+value.ID); err != nil {
		return err
	}
	if value.ActorID != scope.OwnerID {
		return fmt.Errorf("%w: correction %q actor must be the owner", ErrScopeViolation, value.ID)
	}
	if !value.UserConfirmed {
		return invalid("correction %q requires explicit user confirmation", value.ID)
	}
	if err := validateText("correction reason", value.Reason, maxTextRunes, true); err != nil {
		return err
	}
	if !finite(value.CorrectedValue) || value.CorrectedAt.IsZero() || value.CorrectedAt.Before(observation.RecordedAt) || value.CorrectedAt.After(asOf) {
		return fmt.Errorf("%w: correction %q has an invalid value or time", ErrInvalidTimeWindow, value.ID)
	}
	if value.CorrectedVerification == VerificationUnverified {
		return invalid("correction %q must be user-confirmed or source-supported", value.ID)
	}
	return validateEvidence("correction "+value.ID, value.CorrectedVerification, value.Sources, asOf)
}

func validateEvidence(name string, status VerificationStatus, sources []SourceReference, asOf time.Time) error {
	if status != VerificationUnverified && status != VerificationUserConfirmed && status != VerificationSourceSupported && status != VerificationVerified && status != VerificationDisputed {
		return invalid("%s has unsupported verification status %q", name, status)
	}
	if len(sources) > maxSources {
		return invalid("%s has too many sources", name)
	}
	seen := map[string]struct{}{}
	for _, source := range sources {
		if _, exists := seen[source.ID]; exists {
			return invalid("%s has duplicate source id %q", name, source.ID)
		}
		seen[source.ID] = struct{}{}
		if err := validateSource(source, asOf); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	if status == VerificationVerified {
		if len(sources) == 0 || !hasSourceStatus(sources, SourceVerified) {
			return fmt.Errorf("%w: %s needs a verified source", ErrMissingProvenance, name)
		}
	}
	if status == VerificationSourceSupported {
		if len(sources) == 0 || (!hasSourceStatus(sources, SourceSupported) && !hasSourceStatus(sources, SourceVerified)) {
			return fmt.Errorf("%w: %s needs a supporting source", ErrMissingProvenance, name)
		}
	}
	return nil
}

func validateSource(value SourceReference, asOf time.Time) error {
	if err := validateText("source id", value.ID, maxIDRunes, true); err != nil {
		return err
	}
	if err := validateText("source URI", value.URI, maxTextRunes, true); err != nil {
		return err
	}
	parsed, err := url.Parse(value.URI)
	if err != nil || parsed.Scheme == "" || parsed.Fragment != "" {
		return invalid("source %q URI must be absolute and contain no fragment", value.ID)
	}
	if parsed.User != nil {
		return fmt.Errorf("%w: source %q URI contains user information", ErrSecretMaterial, value.ID)
	}
	for key := range parsed.Query() {
		lower := strings.ToLower(key)
		for _, marker := range []string{"token", "key", "secret", "password", "credential", "signature", "auth"} {
			if strings.Contains(lower, marker) {
				return fmt.Errorf("%w: source %q URI contains a credential parameter", ErrSecretMaterial, value.ID)
			}
		}
	}
	if value.Status != SourceUnreviewed && value.Status != SourceSupported && value.Status != SourceVerified && value.Status != SourceDisputed {
		return invalid("source %q has unsupported status %q", value.ID, value.Status)
	}
	if value.RetrievedAt.IsZero() || value.RetrievedAt.After(asOf) {
		return fmt.Errorf("%w: source %q retrieval time is invalid", ErrInvalidTimeWindow, value.ID)
	}
	if value.Status == SourceSupported || value.Status == SourceVerified {
		if !sha256Pattern.MatchString(value.ContentDigest) {
			return fmt.Errorf("%w: source %q requires a lowercase SHA-256 digest", ErrMissingProvenance, value.ID)
		}
	} else if value.ContentDigest != "" {
		if _, err := hex.DecodeString(value.ContentDigest); err != nil || len(value.ContentDigest) != 64 {
			return invalid("source %q has invalid content digest", value.ID)
		}
	}
	return nil
}

func validateScope(value Scope) error {
	if err := validateText("owner id", value.OwnerID, maxIDRunes, true); err != nil {
		return err
	}
	return validateText("workspace id", value.WorkspaceID, maxIDRunes, true)
}

func requireScope(expected, actual Scope, name string) error {
	if actual != expected {
		return fmt.Errorf("%w: %s is outside %s/%s", ErrScopeViolation, name, expected.OwnerID, expected.WorkspaceID)
	}
	return nil
}

func validateText(name, value string, limit int, required bool) error {
	if required && value == "" {
		return invalid("%s is required", name)
	}
	if !utf8.ValidString(value) || utf8.RuneCountInString(value) > limit {
		return invalid("%s exceeds %d characters", name, limit)
	}
	for _, pattern := range secretPatterns {
		if pattern.MatchString(value) {
			return fmt.Errorf("%w: %s", ErrSecretMaterial, name)
		}
	}
	return nil
}

func hasSourceStatus(sources []SourceReference, status SourceStatus) bool {
	for _, source := range sources {
		if source.Status == status {
			return true
		}
	}
	return false
}

func validAttributionMethod(method AttributionMethod) bool {
	return method == AttributionUnknown || method == AttributionUserReport || method == AttributionCorrelation || method == AttributionControlledStudy || method == AttributionModelEstimate
}

func finite(value float64) bool { return !math.IsNaN(value) && !math.IsInf(value, 0) }

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidInput, fmt.Sprintf(format, args...))
}
