package outcomeevaluation

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type Service struct {
	repository Repository
	now        func() time.Time
	lifeGraph  LifeOntologyProjector
}

// A monitor claim and completion can advance a coarse host clock by one
// microsecond each. Keep that logical movement bounded so immediate handoffs
// work without permitting materially future outcome records.
const outcomeMonotonicClockAllowance = 2 * time.Microsecond

func NewService(repository Repository) *Service {
	return newService(repository, time.Now)
}

// NewServiceWithClock creates an outcome service with an explicit clock. It is
// intended for deterministic orchestration and release acceptance where every
// persisted timestamp must share one controlled time source.
func NewServiceWithClock(repository Repository, now func() time.Time) *Service {
	return newService(repository, now)
}

func newService(repository Repository, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{repository: repository, now: now}
}

func (s *Service) StoreOutcome(ctx context.Context, ownerID, workspaceID, outcomeID string, request StoreOutcomeRequest) (OutcomeRevision, bool, error) {
	ownerID, workspaceID, outcomeID, err := validateServiceScope(ownerID, workspaceID, outcomeID)
	if err != nil {
		return OutcomeRevision{}, false, err
	}
	if err := s.available(); err != nil {
		return OutcomeRevision{}, false, err
	}
	request.IdempotencyKey = strings.TrimSpace(request.IdempotencyKey)
	if err := validateText("idempotency key", request.IdempotencyKey, maxIDRunes, true); err != nil {
		return OutcomeRevision{}, false, err
	}
	if request.ExpectedRevision < 0 {
		return OutcomeRevision{}, false, invalid("expected revision must not be negative")
	}
	now := s.now().UTC()
	outcome, err := normalizeAndValidateOutcome(request.Outcome, now)
	if err != nil {
		return OutcomeRevision{}, false, err
	}
	if outcome.ID != outcomeID || outcome.Scope != (Scope{OwnerID: ownerID, WorkspaceID: workspaceID}) {
		return OutcomeRevision{}, false, ErrScopeViolation
	}
	requestDigest, err := hashValue(struct {
		ExpectedRevision int64
		Outcome          IntendedOutcome
	}{request.ExpectedRevision, outcome})
	if err != nil {
		return OutcomeRevision{}, false, err
	}
	record := OutcomeRevision{
		Outcome:    outcome,
		Revision:   request.ExpectedRevision + 1,
		RecordedAt: now,
	}
	record.AuditDigest, err = outcomeRevisionDigest(record)
	if err != nil {
		return OutcomeRevision{}, false, err
	}
	stored, created, err := s.repository.SaveOutcome(ctx, ownerID, workspaceID, record, request.ExpectedRevision, WriteToken{
		Key: request.IdempotencyKey, RequestDigest: requestDigest,
	})
	if err != nil {
		return OutcomeRevision{}, false, err
	}
	if err := verifyOutcomeRevisionScope(stored, ownerID, workspaceID, outcomeID); err != nil {
		return OutcomeRevision{}, false, err
	}
	s.projectOutcomeRevision(ctx, &stored)
	return stored, created, nil
}

func (s *Service) GetOutcome(ctx context.Context, ownerID, workspaceID, outcomeID string) (OutcomeRevision, error) {
	ownerID, workspaceID, outcomeID, err := validateServiceScope(ownerID, workspaceID, outcomeID)
	if err != nil {
		return OutcomeRevision{}, err
	}
	if err := s.available(); err != nil {
		return OutcomeRevision{}, err
	}
	record, err := s.repository.GetOutcome(ctx, ownerID, workspaceID, outcomeID)
	if err != nil {
		return OutcomeRevision{}, err
	}
	if err := verifyOutcomeRevisionScope(record, ownerID, workspaceID, outcomeID); err != nil {
		return OutcomeRevision{}, err
	}
	return record, nil
}

// ResolveOutcomeRevision reads one exact immutable historical revision. It
// never falls back to GetOutcome or the latest entry in outcome history.
func (s *Service) ResolveOutcomeRevision(ctx context.Context, ownerID, workspaceID, outcomeID string, revision int64, auditDigest string) (OutcomeRevision, error) {
	ownerID, workspaceID, outcomeID, err := validateServiceScope(ownerID, workspaceID, outcomeID)
	if err != nil {
		return OutcomeRevision{}, err
	}
	if err := validateOutcomeRevisionSelector(revision, auditDigest); err != nil {
		return OutcomeRevision{}, err
	}
	if err := s.available(); err != nil {
		return OutcomeRevision{}, err
	}
	resolver, ok := s.repository.(OutcomeRevisionResolver)
	if !ok {
		return OutcomeRevision{}, errorsUnavailable()
	}
	record, err := resolver.ResolveOutcomeRevision(ctx, ownerID, workspaceID, outcomeID, revision, auditDigest)
	if err != nil {
		return OutcomeRevision{}, err
	}
	if err := verifyOutcomeRevisionScope(record, ownerID, workspaceID, outcomeID); err != nil {
		return OutcomeRevision{}, err
	}
	if record.Revision != revision || !equalSHA256(record.AuditDigest, auditDigest) {
		return OutcomeRevision{}, ErrNotFound
	}
	return record, nil
}

func (s *Service) OutcomeHistory(ctx context.Context, ownerID, workspaceID, outcomeID string) ([]OutcomeRevision, error) {
	ownerID, workspaceID, outcomeID, err := validateServiceScope(ownerID, workspaceID, outcomeID)
	if err != nil {
		return nil, err
	}
	if err := s.available(); err != nil {
		return nil, err
	}
	records, err := s.repository.ListOutcomeHistory(ctx, ownerID, workspaceID, outcomeID)
	if err != nil {
		return nil, err
	}
	for _, record := range records {
		if err := verifyOutcomeRevisionScope(record, ownerID, workspaceID, outcomeID); err != nil {
			return nil, err
		}
	}
	return records, nil
}

func (s *Service) CreateEvaluation(ctx context.Context, ownerID, workspaceID, outcomeID string, request CreateEvaluationRequest) (EvaluationRecord, bool, error) {
	ownerID, workspaceID, outcomeID, err := validateServiceScope(ownerID, workspaceID, outcomeID)
	if err != nil {
		return EvaluationRecord{}, false, err
	}
	if err := s.available(); err != nil {
		return EvaluationRecord{}, false, err
	}
	request.IdempotencyKey = strings.TrimSpace(request.IdempotencyKey)
	if err := validateText("idempotency key", request.IdempotencyKey, maxIDRunes, true); err != nil {
		return EvaluationRecord{}, false, err
	}
	var current OutcomeRevision
	if request.OutcomeAuditDigest != "" {
		current, err = s.ResolveOutcomeRevision(ctx, ownerID, workspaceID, outcomeID, request.OutcomeRevision, request.OutcomeAuditDigest)
	} else {
		current, err = s.repository.GetOutcome(ctx, ownerID, workspaceID, outcomeID)
		if err == nil && current.Revision != request.OutcomeRevision {
			err = ErrRevisionConflict
		}
	}
	if err != nil {
		return EvaluationRecord{}, false, err
	}
	if err := verifyOutcomeRevisionScope(current, ownerID, workspaceID, outcomeID); err != nil {
		return EvaluationRecord{}, false, err
	}
	now := s.now().UTC()
	if request.AsOf.IsZero() || request.AsOf.After(now.Add(outcomeMonotonicClockAllowance)) {
		return EvaluationRecord{}, false, fmt.Errorf("%w: as-of time must not be in the future", ErrInvalidTimeWindow)
	}
	if request.AsOf.After(now) {
		now = request.AsOf.UTC()
	}
	normalized, err := normalizeAndValidate(EvaluationRequest{
		Outcome: current.Outcome, Observations: request.Observations,
		Corrections: request.Corrections, AsOf: request.AsOf,
	})
	if err != nil {
		return EvaluationRecord{}, false, err
	}
	requestDigest, err := hashValue(struct {
		OutcomeRevision    int64
		OutcomeAuditDigest string
		Request            EvaluationRequest
	}{request.OutcomeRevision, current.AuditDigest, normalized})
	if err != nil {
		return EvaluationRecord{}, false, err
	}
	evaluation, err := Evaluate(normalized)
	if err != nil {
		return EvaluationRecord{}, false, err
	}
	if err := evaluation.ValidateNoAuthority(); err != nil {
		return EvaluationRecord{}, false, err
	}
	record := EvaluationRecord{
		Evaluation: evaluation, OutcomeRevision: request.OutcomeRevision, RecordedAt: now,
	}
	record.RecordDigest, err = evaluationRecordDigest(record)
	if err != nil {
		return EvaluationRecord{}, false, err
	}
	stored, created, err := s.repository.AppendEvaluation(ctx, ownerID, workspaceID, outcomeID, record, WriteToken{
		Key: request.IdempotencyKey, RequestDigest: requestDigest, OutcomeAuditDigest: request.OutcomeAuditDigest,
	})
	if err != nil {
		return EvaluationRecord{}, false, err
	}
	if err := verifyEvaluationRecordScope(stored, ownerID, workspaceID, outcomeID); err != nil {
		return EvaluationRecord{}, false, err
	}
	s.projectEvaluationRecord(ctx, current, &stored)
	return stored, created, nil
}

func (s *Service) GetEvaluation(ctx context.Context, ownerID, workspaceID, outcomeID, evaluationID string) (EvaluationRecord, error) {
	ownerID, workspaceID, outcomeID, err := validateServiceScope(ownerID, workspaceID, outcomeID)
	if err != nil {
		return EvaluationRecord{}, err
	}
	evaluationID = strings.TrimSpace(evaluationID)
	if err := validateText("evaluation id", evaluationID, maxIDRunes+64, true); err != nil {
		return EvaluationRecord{}, err
	}
	if err := s.available(); err != nil {
		return EvaluationRecord{}, err
	}
	record, err := s.repository.GetEvaluation(ctx, ownerID, workspaceID, outcomeID, evaluationID)
	if err != nil {
		return EvaluationRecord{}, err
	}
	if record.Evaluation.ID != evaluationID {
		return EvaluationRecord{}, ErrScopeViolation
	}
	if err := verifyEvaluationRecordScope(record, ownerID, workspaceID, outcomeID); err != nil {
		return EvaluationRecord{}, err
	}
	return record, nil
}

func (s *Service) Evaluations(ctx context.Context, ownerID, workspaceID, outcomeID string) ([]EvaluationRecord, error) {
	ownerID, workspaceID, outcomeID, err := validateServiceScope(ownerID, workspaceID, outcomeID)
	if err != nil {
		return nil, err
	}
	if err := s.available(); err != nil {
		return nil, err
	}
	records, err := s.repository.ListEvaluations(ctx, ownerID, workspaceID, outcomeID)
	if err != nil {
		return nil, err
	}
	for _, record := range records {
		if err := verifyEvaluationRecordScope(record, ownerID, workspaceID, outcomeID); err != nil {
			return nil, err
		}
	}
	return records, nil
}

func (s *Service) StoreCorrection(ctx context.Context, ownerID, workspaceID, outcomeID string, request StoreCorrectionRequest) (CorrectionRecord, bool, error) {
	ownerID, workspaceID, outcomeID, err := validateServiceScope(ownerID, workspaceID, outcomeID)
	if err != nil {
		return CorrectionRecord{}, false, err
	}
	if err := s.available(); err != nil {
		return CorrectionRecord{}, false, err
	}
	request.IdempotencyKey = strings.TrimSpace(request.IdempotencyKey)
	if err := validateText("idempotency key", request.IdempotencyKey, maxIDRunes, true); err != nil {
		return CorrectionRecord{}, false, err
	}
	current, err := s.repository.GetOutcome(ctx, ownerID, workspaceID, outcomeID)
	if err != nil {
		return CorrectionRecord{}, false, err
	}
	if err := verifyOutcomeRevisionScope(current, ownerID, workspaceID, outcomeID); err != nil {
		return CorrectionRecord{}, false, err
	}
	if request.OutcomeRevision < 1 || current.Revision != request.OutcomeRevision {
		return CorrectionRecord{}, false, ErrRevisionConflict
	}
	now := s.now().UTC()
	if request.AsOf.IsZero() || request.AsOf.After(now.Add(outcomeMonotonicClockAllowance)) {
		return CorrectionRecord{}, false, fmt.Errorf("%w: as-of time must not be in the future", ErrInvalidTimeWindow)
	}
	if request.AsOf.After(now) {
		now = request.AsOf.UTC()
	}
	normalized, err := normalizeAndValidate(EvaluationRequest{
		Outcome: current.Outcome, Observations: []Observation{request.Observation},
		Corrections: []UserCorrection{request.Correction}, AsOf: request.AsOf,
	})
	if err != nil {
		return CorrectionRecord{}, false, err
	}
	requestDigest, err := hashValue(struct {
		OutcomeRevision int64
		Observation     Observation
		Correction      UserCorrection
		AsOf            time.Time
	}{request.OutcomeRevision, normalized.Observations[0], normalized.Corrections[0], normalized.AsOf})
	if err != nil {
		return CorrectionRecord{}, false, err
	}
	record := CorrectionRecord{
		OutcomeID: outcomeID, OutcomeRevision: request.OutcomeRevision,
		Observation: normalized.Observations[0], Correction: normalized.Corrections[0], RecordedAt: now,
	}
	record.AuditDigest, err = correctionRecordDigest(record)
	if err != nil {
		return CorrectionRecord{}, false, err
	}
	stored, created, err := s.repository.AppendCorrection(ctx, ownerID, workspaceID, outcomeID, record, WriteToken{
		Key: request.IdempotencyKey, RequestDigest: requestDigest,
	})
	if err != nil {
		return CorrectionRecord{}, false, err
	}
	if err := verifyCorrectionRecordScope(stored, ownerID, workspaceID, outcomeID); err != nil {
		return CorrectionRecord{}, false, err
	}
	return stored, created, nil
}

func (s *Service) Corrections(ctx context.Context, ownerID, workspaceID, outcomeID string) ([]CorrectionRecord, error) {
	ownerID, workspaceID, outcomeID, err := validateServiceScope(ownerID, workspaceID, outcomeID)
	if err != nil {
		return nil, err
	}
	if err := s.available(); err != nil {
		return nil, err
	}
	records, err := s.repository.ListCorrections(ctx, ownerID, workspaceID, outcomeID)
	if err != nil {
		return nil, err
	}
	for _, record := range records {
		if err := verifyCorrectionRecordScope(record, ownerID, workspaceID, outcomeID); err != nil {
			return nil, err
		}
	}
	return records, nil
}

func (s *Service) available() error {
	if s == nil || s.repository == nil || s.now == nil {
		return errorsUnavailable()
	}
	return nil
}

func validateServiceScope(ownerID, workspaceID, outcomeID string) (string, string, string, error) {
	ownerID = strings.TrimSpace(ownerID)
	workspaceID = strings.TrimSpace(workspaceID)
	outcomeID = strings.TrimSpace(outcomeID)
	if err := validateText("owner id", ownerID, maxIDRunes, true); err != nil {
		return "", "", "", err
	}
	if err := validateText("workspace id", workspaceID, maxIDRunes, true); err != nil {
		return "", "", "", err
	}
	if err := validateText("outcome id", outcomeID, maxIDRunes, true); err != nil {
		return "", "", "", err
	}
	return ownerID, workspaceID, outcomeID, nil
}

func verifyOutcomeRevisionScope(record OutcomeRevision, ownerID, workspaceID, outcomeID string) error {
	if record.Outcome.Scope != (Scope{OwnerID: ownerID, WorkspaceID: workspaceID}) || record.Outcome.ID != outcomeID {
		return ErrScopeViolation
	}
	return VerifyOutcomeRevisionDigest(record)
}

func verifyEvaluationRecordScope(record EvaluationRecord, ownerID, workspaceID, outcomeID string) error {
	if record.Evaluation.Scope != (Scope{OwnerID: ownerID, WorkspaceID: workspaceID}) || record.Evaluation.OutcomeID != outcomeID {
		return ErrScopeViolation
	}
	return VerifyEvaluationRecordDigest(record)
}

func verifyCorrectionRecordScope(record CorrectionRecord, ownerID, workspaceID, outcomeID string) error {
	expected := Scope{OwnerID: ownerID, WorkspaceID: workspaceID}
	if record.OutcomeID != outcomeID || record.Observation.Scope != expected || record.Correction.Scope != expected {
		return ErrScopeViolation
	}
	return VerifyCorrectionRecordDigest(record)
}
