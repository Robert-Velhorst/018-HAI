package outcomeevaluation

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestCreateEvaluationAllowsOnlyBoundedMonotonicClockAdvance(t *testing.T) {
	ctx := context.Background()
	now := testAsOf
	service := NewServiceWithClock(NewMemoryRepository(), func() time.Time { return now })
	outcome := validRequest().Outcome
	stored, created, err := service.StoreOutcome(ctx, "owner-1", "workspace-1", "outcome-1", StoreOutcomeRequest{
		IdempotencyKey: "monotonic-outcome", ExpectedRevision: 0, Outcome: outcome,
	})
	if err != nil || !created {
		t.Fatalf("StoreOutcome() = (%+v, %v, %v)", stored, created, err)
	}
	request := CreateEvaluationRequest{
		IdempotencyKey: "monotonic-evaluation", OutcomeRevision: stored.Revision, OutcomeAuditDigest: stored.AuditDigest,
		Observations: []Observation{
			observation("monotonic-obs-1", 12, testStart.Add(5*24*time.Hour)),
			observation("monotonic-obs-2", 16, testStart.Add(15*24*time.Hour)),
		},
		AsOf: now.Add(outcomeMonotonicClockAllowance),
	}
	record, created, err := service.CreateEvaluation(ctx, "owner-1", "workspace-1", "outcome-1", request)
	if err != nil || !created || !record.RecordedAt.Equal(request.AsOf) {
		t.Fatalf("bounded evaluation = (%+v, %v, %v)", record, created, err)
	}
	request.IdempotencyKey = "future-evaluation"
	request.AsOf = now.Add(outcomeMonotonicClockAllowance + time.Microsecond)
	if _, _, err := service.CreateEvaluation(ctx, "owner-1", "workspace-1", "outcome-1", request); !errors.Is(err, ErrInvalidTimeWindow) {
		t.Fatalf("materially future evaluation error = %v, want %v", err, ErrInvalidTimeWindow)
	}
}

func TestServiceStoresOwnerScopedAdvisoryRecordsIdempotently(t *testing.T) {
	now := time.Date(2026, time.February, 2, 12, 0, 0, 0, time.UTC)
	repository := NewMemoryRepository()
	service := newService(repository, func() time.Time { return now })
	outcome := validRequest().Outcome

	stored, created, err := service.StoreOutcome(context.Background(), "owner-1", "workspace-1", "outcome-1", StoreOutcomeRequest{
		IdempotencyKey: "store-outcome-1", ExpectedRevision: 0, Outcome: outcome,
	})
	if err != nil || !created || stored.Revision != 1 {
		t.Fatalf("StoreOutcome() = revision %d, created %v, err %v", stored.Revision, created, err)
	}
	if err := VerifyOutcomeRevisionDigest(stored); err != nil {
		t.Fatalf("VerifyOutcomeRevisionDigest() error = %v", err)
	}

	retry, created, err := service.StoreOutcome(context.Background(), "owner-1", "workspace-1", "outcome-1", StoreOutcomeRequest{
		IdempotencyKey: "store-outcome-1", ExpectedRevision: 0, Outcome: outcome,
	})
	if err != nil || created || retry.AuditDigest != stored.AuditDigest {
		t.Fatalf("idempotent retry = created %v, err %v, record %#v", created, err, retry)
	}
	changed := outcome
	changed.Statement = "A different intended outcome."
	if _, _, err := service.StoreOutcome(context.Background(), "owner-1", "workspace-1", "outcome-1", StoreOutcomeRequest{
		IdempotencyKey: "store-outcome-1", ExpectedRevision: 0, Outcome: changed,
	}); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("idempotency mismatch error = %v", err)
	}

	evaluation, created, err := service.CreateEvaluation(context.Background(), "owner-1", "workspace-1", "outcome-1", CreateEvaluationRequest{
		IdempotencyKey: "evaluation-1", OutcomeRevision: 1,
		Observations: []Observation{
			observation("obs-1", 12, testStart.Add(5*24*time.Hour)),
			observation("obs-2", 16, testStart.Add(15*24*time.Hour)),
		},
		AsOf: testAsOf,
	})
	if err != nil || !created {
		t.Fatalf("CreateEvaluation() created %v, error = %v", created, err)
	}
	if err := VerifyEvaluationRecordDigest(evaluation); err != nil {
		t.Fatalf("VerifyEvaluationRecordDigest() error = %v", err)
	}
	if err := evaluation.Evaluation.ValidateNoAuthority(); err != nil {
		t.Fatalf("stored evaluation grants authority: %v", err)
	}
	for _, recommendation := range evaluation.Evaluation.Recommendations {
		if recommendation.Control.MayExecute || recommendation.Control.MayChangePolicy || !recommendation.Control.AdvisoryOnly {
			t.Fatalf("unsafe recommendation persisted: %#v", recommendation)
		}
	}

	correction := UserCorrection{
		ID: "correction-1", Scope: outcome.Scope, ObservationID: "obs-2", ActorID: "owner-1",
		UserConfirmed: true, CorrectedValue: 13, CorrectedVerification: VerificationUserConfirmed,
		Reason: "I entered the wrong value.", CorrectedAt: testStart.Add(17 * 24 * time.Hour),
	}
	correctionRecord, created, err := service.StoreCorrection(context.Background(), "owner-1", "workspace-1", "outcome-1", StoreCorrectionRequest{
		IdempotencyKey: "correction-write-1", OutcomeRevision: 1,
		Observation: observation("obs-2", 16, testStart.Add(15*24*time.Hour)),
		Correction:  correction, AsOf: testAsOf,
	})
	if err != nil || !created {
		t.Fatalf("StoreCorrection() created %v, error = %v", created, err)
	}
	if err := VerifyCorrectionRecordDigest(correctionRecord); err != nil {
		t.Fatalf("VerifyCorrectionRecordDigest() error = %v", err)
	}

	stored.Outcome.Statement = "mutated by caller"
	got, err := service.GetOutcome(context.Background(), "owner-1", "workspace-1", "outcome-1")
	if err != nil || got.Outcome.Statement != outcome.Statement {
		t.Fatalf("repository retained caller mutation: %#v, err %v", got, err)
	}
	if _, err := service.GetOutcome(context.Background(), "other-owner", "workspace-1", "outcome-1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-owner read error = %v, want ErrNotFound", err)
	}
	if _, err := service.GetOutcome(context.Background(), "owner-1", "other-workspace", "outcome-1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-workspace read error = %v, want ErrNotFound", err)
	}
}

func TestMemoryRepositoryBoundsHistoriesAndDetectsTampering(t *testing.T) {
	now := time.Date(2026, time.February, 2, 12, 0, 0, 0, time.UTC)
	repository, err := NewMemoryRepositoryWithLimits(HistoryLimits{OutcomeRevisions: 2, Evaluations: 2, Corrections: 2})
	if err != nil {
		t.Fatal(err)
	}
	service := newService(repository, func() time.Time { return now })
	outcome := validRequest().Outcome
	for revision := int64(0); revision < 3; revision++ {
		outcome.Statement = fmt.Sprintf("Revision %d", revision+1)
		if _, _, err := service.StoreOutcome(context.Background(), "owner-1", "workspace-1", "outcome-1", StoreOutcomeRequest{
			IdempotencyKey: fmt.Sprintf("outcome-revision-%d", revision+1), ExpectedRevision: revision, Outcome: outcome,
		}); err != nil {
			t.Fatalf("store revision %d: %v", revision+1, err)
		}
	}
	history, err := service.OutcomeHistory(context.Background(), "owner-1", "workspace-1", "outcome-1")
	if err != nil || len(history) != 2 || history[0].Revision != 2 || history[1].Revision != 3 {
		t.Fatalf("bounded history = %#v, err %v", history, err)
	}
	for index := 0; index < 3; index++ {
		if _, _, err := service.CreateEvaluation(context.Background(), "owner-1", "workspace-1", "outcome-1", CreateEvaluationRequest{
			IdempotencyKey: fmt.Sprintf("bounded-evaluation-%d", index), OutcomeRevision: 3,
			Observations: []Observation{
				observation(fmt.Sprintf("bounded-obs-a-%d", index), 12+float64(index), testStart.Add(5*24*time.Hour)),
				observation(fmt.Sprintf("bounded-obs-b-%d", index), 16+float64(index), testStart.Add(15*24*time.Hour)),
			},
			AsOf: testAsOf,
		}); err != nil {
			t.Fatalf("store bounded evaluation %d: %v", index, err)
		}

		original := observation(fmt.Sprintf("bounded-correction-obs-%d", index), 10+float64(index), testStart.Add(10*24*time.Hour))
		correction := UserCorrection{
			ID: fmt.Sprintf("bounded-correction-%d", index), Scope: outcome.Scope,
			ObservationID: original.ID, ActorID: "owner-1", UserConfirmed: true,
			CorrectedValue: 11 + float64(index), CorrectedVerification: VerificationUserConfirmed,
			Reason: "Owner-confirmed correction.", CorrectedAt: original.RecordedAt.Add(24 * time.Hour),
		}
		if _, _, err := service.StoreCorrection(context.Background(), "owner-1", "workspace-1", "outcome-1", StoreCorrectionRequest{
			IdempotencyKey: fmt.Sprintf("bounded-correction-write-%d", index), OutcomeRevision: 3,
			Observation: original, Correction: correction, AsOf: testAsOf,
		}); err != nil {
			t.Fatalf("store bounded correction %d: %v", index, err)
		}
	}
	evaluations, err := service.Evaluations(context.Background(), "owner-1", "workspace-1", "outcome-1")
	if err != nil || len(evaluations) != 2 {
		t.Fatalf("bounded evaluations = %d, err %v", len(evaluations), err)
	}
	corrections, err := service.Corrections(context.Background(), "owner-1", "workspace-1", "outcome-1")
	if err != nil || len(corrections) != 2 {
		t.Fatalf("bounded corrections = %d, err %v", len(corrections), err)
	}

	key := repositoryKey{ownerID: "owner-1", workspaceID: "workspace-1", outcomeID: "outcome-1"}
	repository.mu.Lock()
	repository.data[key].revisions[1].Outcome.Statement = "tampered"
	repository.mu.Unlock()
	if _, err := service.GetOutcome(context.Background(), "owner-1", "workspace-1", "outcome-1"); !errors.Is(err, ErrIntegrityViolation) {
		t.Fatalf("tampered outcome error = %v, want ErrIntegrityViolation", err)
	}
}

func TestServiceRejectsStaleRevisionsAndInvalidProvenance(t *testing.T) {
	now := time.Date(2026, time.February, 2, 12, 0, 0, 0, time.UTC)
	service := newService(NewMemoryRepository(), func() time.Time { return now })
	outcome := validRequest().Outcome
	if _, _, err := service.StoreOutcome(context.Background(), "owner-1", "workspace-1", "outcome-1", StoreOutcomeRequest{
		IdempotencyKey: "outcome-1", ExpectedRevision: 0, Outcome: outcome,
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.StoreOutcome(context.Background(), "owner-1", "workspace-1", "outcome-1", StoreOutcomeRequest{
		IdempotencyKey: "stale", ExpectedRevision: 0, Outcome: outcome,
	}); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale revision error = %v", err)
	}

	bad := observation("obs-bad", 12, testStart.Add(5*24*time.Hour))
	bad.Sources[0].ContentDigest = ""
	if _, _, err := service.CreateEvaluation(context.Background(), "owner-1", "workspace-1", "outcome-1", CreateEvaluationRequest{
		IdempotencyKey: "bad-provenance", OutcomeRevision: 1,
		Observations: []Observation{bad, observation("obs-2", 14, testStart.Add(15*24*time.Hour))}, AsOf: testAsOf,
	}); !errors.Is(err, ErrMissingProvenance) {
		t.Fatalf("invalid provenance error = %v", err)
	}
}
