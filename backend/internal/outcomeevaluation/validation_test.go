package outcomeevaluation

import (
	"errors"
	"math"
	"sync"
	"testing"
	"time"
)

func TestNormalizeAndValidateDoesNotMutateInput(t *testing.T) {
	request := validRequest()
	request.Outcome.ID = " outcome-1 "
	request.Outcome.Indicators[0].ID = " indicator-1 "
	request.Outcome.Indicators[0].Baseline.Sources[0].ID = " baseline-source "
	request.Observations = []Observation{
		observation(" obs-b ", 12, testStart.Add(15*24*time.Hour)),
		observation(" obs-a ", 11, testStart.Add(5*24*time.Hour)),
	}
	request.Observations[0].Sources[0].ID = " observation-source "
	request.Corrections = []UserCorrection{{
		ID: " correction-1 ", Scope: request.Outcome.Scope, ObservationID: " obs-b ", ActorID: " owner-1 ",
		UserConfirmed: true, CorrectedValue: 13, CorrectedVerification: VerificationUserConfirmed,
		Sources: []SourceReference{source(" correction-source ", testStart.Add(6*24*time.Hour))},
		Reason: " Correction ", CorrectedAt: testStart.Add(17 * 24 * time.Hour),
	}}

	normalized, err := normalizeAndValidate(request)
	if err != nil {
		t.Fatalf("normalizeAndValidate() error = %v", err)
	}
	if normalized.Outcome.ID != "outcome-1" || normalized.Outcome.Indicators[0].ID != "indicator-1" ||
		normalized.Observations[0].ID != "obs-a" || normalized.Observations[1].ID != "obs-b" ||
		normalized.Observations[1].Sources[0].ID != "observation-source" ||
		normalized.Corrections[0].ID != "correction-1" || normalized.Corrections[0].Sources[0].ID != "correction-source" {
		t.Fatalf("request was not normalized: %+v", normalized)
	}
	if request.Outcome.ID != " outcome-1 " || request.Outcome.Indicators[0].ID != " indicator-1 " ||
		request.Outcome.Indicators[0].Baseline.Sources[0].ID != " baseline-source " ||
		request.Observations[0].ID != " obs-b " || request.Observations[0].Sources[0].ID != " observation-source " ||
		request.Corrections[0].ID != " correction-1 " || request.Corrections[0].Sources[0].ID != " correction-source " {
		t.Fatalf("normalization mutated caller-owned request: %+v", request)
	}
}

func TestNormalizeAndValidateConcurrentSharedInput(t *testing.T) {
	request := validRequest()
	request.Outcome.Indicators[0].ID = " indicator-1 "
	request.Observations = []Observation{
		observation(" obs-b ", 12, testStart.Add(15*24*time.Hour)),
		observation(" obs-a ", 11, testStart.Add(5*24*time.Hour)),
	}
	request.Corrections = []UserCorrection{{
		ID: " correction ", Scope: request.Outcome.Scope, ObservationID: " obs-b ", ActorID: " owner-1 ",
		UserConfirmed: true, CorrectedValue: 13, CorrectedVerification: VerificationUserConfirmed,
		Reason: " Corrected ", CorrectedAt: testStart.Add(17 * 24 * time.Hour),
	}}

	const workers = 32
	start := make(chan struct{})
	var wait sync.WaitGroup
	errs := make(chan error, workers)
	for range workers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			_, err := normalizeAndValidate(request)
			errs <- err
		}()
	}
	close(start)
	wait.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("normalizeAndValidate() error = %v", err)
		}
	}
}

func TestNormalizeAndValidateOutcomeRequiresSupportedLifeDomain(t *testing.T) {
	outcome := validRequest().Outcome
	outcome.LifeDomain = ""
	if _, err := normalizeAndValidateOutcome(outcome, testAsOf); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("missing life domain error = %v", err)
	}
	outcome.LifeDomain = "unknown_domain"
	if _, err := normalizeAndValidateOutcome(outcome, testAsOf); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("unknown life domain error = %v", err)
	}
}

func TestEvaluateRejectsOwnerAndWorkspaceLeaks(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*EvaluationRequest)
	}{
		{"baseline owner", func(r *EvaluationRequest) { r.Outcome.Indicators[0].Baseline.Scope.OwnerID = "other-owner" }},
		{"observation workspace", func(r *EvaluationRequest) { r.Observations[0].Scope.WorkspaceID = "other-workspace" }},
		{"correction owner", func(r *EvaluationRequest) { r.Corrections[0].Scope.OwnerID = "other-owner" }},
		{"correction actor", func(r *EvaluationRequest) { r.Corrections[0].ActorID = "other-owner" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := validRequest()
			request.Observations = []Observation{
				observation("obs-1", 11, testStart.Add(5*24*time.Hour)),
				observation("obs-2", 12, testStart.Add(15*24*time.Hour)),
			}
			request.Corrections = []UserCorrection{{
				ID: "correction-1", Scope: request.Outcome.Scope, ObservationID: "obs-2", ActorID: "owner-1",
				UserConfirmed: true, CorrectedValue: 13, CorrectedVerification: VerificationUserConfirmed,
				Reason: "Correcting my entry.", CorrectedAt: testStart.Add(17 * 24 * time.Hour),
			}}
			tt.mutate(&request)
			_, err := Evaluate(request)
			if !errors.Is(err, ErrScopeViolation) {
				t.Fatalf("Evaluate() error = %v, want ErrScopeViolation", err)
			}
		})
	}
}

func TestEvaluateRequiresProvenanceForVerifiedEvidence(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*EvaluationRequest)
	}{
		{"verified baseline", func(r *EvaluationRequest) { r.Outcome.Indicators[0].Baseline.Sources = nil }},
		{"verified observation", func(r *EvaluationRequest) { r.Observations[0].Sources = nil }},
		{"source supported needs digest", func(r *EvaluationRequest) {
			r.Observations[0].Verification = VerificationSourceSupported
			r.Observations[0].Sources[0].Status = SourceSupported
			r.Observations[0].Sources[0].ContentDigest = ""
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := validRequest()
			request.Observations = []Observation{
				observation("obs-1", 11, testStart.Add(5*24*time.Hour)),
				observation("obs-2", 12, testStart.Add(15*24*time.Hour)),
			}
			tt.mutate(&request)
			_, err := Evaluate(request)
			if !errors.Is(err, ErrMissingProvenance) {
				t.Fatalf("Evaluate() error = %v, want ErrMissingProvenance", err)
			}
		})
	}
}

func TestEvaluateFailsClosedOnInvalidWindowsAndTimes(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*EvaluationRequest)
	}{
		{"reversed window", func(r *EvaluationRequest) { r.Outcome.Window.End = r.Outcome.Window.Start }},
		{"window too long", func(r *EvaluationRequest) {
			r.Outcome.Window.End = r.Outcome.Window.Start.Add(11 * 365 * 24 * time.Hour)
		}},
		{"as-of before window", func(r *EvaluationRequest) { r.AsOf = r.Outcome.Window.Start.Add(-time.Second) }},
		{"observation after as-of", func(r *EvaluationRequest) { r.AsOf = r.Observations[0].ObservedAt.Add(-time.Second) }},
		{"observation outside window", func(r *EvaluationRequest) { r.Observations[0].ObservedAt = r.Outcome.Window.End.Add(time.Second) }},
		{"correction before record", func(r *EvaluationRequest) {
			r.Corrections[0].CorrectedAt = r.Observations[0].RecordedAt.Add(-time.Second)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := validRequest()
			request.Observations = []Observation{
				observation("obs-1", 11, testStart.Add(5*24*time.Hour)),
				observation("obs-2", 12, testStart.Add(15*24*time.Hour)),
			}
			request.Corrections = []UserCorrection{{
				ID: "correction-1", Scope: request.Outcome.Scope, ObservationID: "obs-1", ActorID: "owner-1",
				UserConfirmed: true, CorrectedValue: 13, CorrectedVerification: VerificationUserConfirmed,
				Reason: "Correcting my entry.", CorrectedAt: testStart.Add(7 * 24 * time.Hour),
			}}
			tt.mutate(&request)
			_, err := Evaluate(request)
			if !errors.Is(err, ErrInvalidTimeWindow) {
				t.Fatalf("Evaluate() error = %v, want ErrInvalidTimeWindow", err)
			}
		})
	}
}

func TestEvaluateFailsClosedOnSecrets(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*EvaluationRequest)
	}{
		{"secret in text", func(r *EvaluationRequest) { r.Outcome.Statement = "api_key=super-secret-value" }},
		{"credential URI", func(r *EvaluationRequest) {
			r.Observations[0].Sources[0].URI = "https://user:password@example.test/evidence"
		}},
		{"credential query", func(r *EvaluationRequest) {
			r.Observations[0].Sources[0].URI = "https://example.test/evidence?access_token=abc"
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := validRequest()
			request.Observations = []Observation{
				observation("obs-1", 11, testStart.Add(5*24*time.Hour)),
				observation("obs-2", 12, testStart.Add(15*24*time.Hour)),
			}
			tt.mutate(&request)
			_, err := Evaluate(request)
			if !errors.Is(err, ErrSecretMaterial) {
				t.Fatalf("Evaluate() error = %v, want ErrSecretMaterial", err)
			}
		})
	}
}

func TestEvaluateRejectsInvalidNumbers(t *testing.T) {
	request := validRequest()
	request.Observations = []Observation{
		observation("obs-1", math.NaN(), testStart.Add(5*24*time.Hour)),
		observation("obs-2", 12, testStart.Add(15*24*time.Hour)),
	}
	if _, err := Evaluate(request); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("Evaluate() error = %v, want ErrInvalidInput", err)
	}
}

func TestConflictingCorrectionsRequireReconciliation(t *testing.T) {
	request := validRequest()
	request.Observations = []Observation{
		observation("obs-1", 11, testStart.Add(5*24*time.Hour)),
		observation("obs-2", 12, testStart.Add(15*24*time.Hour)),
	}
	correctedAt := testStart.Add(17 * 24 * time.Hour)
	request.Corrections = []UserCorrection{
		{ID: "correction-a", Scope: request.Outcome.Scope, ObservationID: "obs-2", ActorID: "owner-1", UserConfirmed: true, CorrectedValue: 13, CorrectedVerification: VerificationUserConfirmed, Reason: "First correction.", CorrectedAt: correctedAt},
		{ID: "correction-b", Scope: request.Outcome.Scope, ObservationID: "obs-2", ActorID: "owner-1", UserConfirmed: true, CorrectedValue: 18, CorrectedVerification: VerificationUserConfirmed, Reason: "Concurrent correction.", CorrectedAt: correctedAt},
	}

	result, err := Evaluate(request)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if result.Indicators[0].Evidence != EvidenceConflicting || !hasRecommendation(result, RecommendationReconcileEvidence) {
		t.Fatalf("conflicting corrections were not surfaced: %+v", result)
	}
}
