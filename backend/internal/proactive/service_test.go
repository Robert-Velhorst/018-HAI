package proactive

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestOwnerIsolation(t *testing.T) {
	service, _, now := testService(t)
	rule := testRule("owner-a", now)
	mustPutRule(t, service, rule)
	result := mustEvaluate(t, service, "owner-a", rule.ID, testSignal("owner-a", SignalDeadline, now))
	if result.Proposal == nil {
		t.Fatal("expected proposal")
	}

	if _, err := service.GetProposal(context.Background(), "owner-b", result.Proposal.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-owner get error = %v, want not found", err)
	}
	if proposals, err := service.ListProposals(context.Background(), "owner-b", ProposalFilter{}); err != nil || len(proposals) != 0 {
		t.Fatalf("cross-owner list = %v, %v", proposals, err)
	}
}

func TestSourceFreshnessAndUncertaintySuppressProposals(t *testing.T) {
	service, _, now := testService(t)
	rule := testRule("owner-a", now)
	rule.MaximumSourceAge = time.Hour
	mustPutRule(t, service, rule)

	stale := testSignal("owner-a", SignalDeadline, now)
	stale.Sources[0].ObservedAt = now.Add(-2 * time.Hour)
	stale.Sources[0].RetrievedAt = now.Add(-time.Hour)
	result := mustEvaluate(t, service, "owner-a", rule.ID, stale)
	if !result.Suppressed || result.Suppression != SuppressionStale {
		t.Fatalf("stale result = %#v", result)
	}

	uncertain := testSignal("owner-a", SignalDeadline, now)
	uncertain.ID = "signal-uncertain"
	uncertain.IdempotencyKey = "idem-uncertain"
	uncertain.OpenLoopKey = "loop-uncertain"
	uncertain.Sources[0].Verification = VerificationConflicting
	result = mustEvaluate(t, service, "owner-a", rule.ID, uncertain)
	if !result.Suppressed || result.Suppression != SuppressionUncertain {
		t.Fatalf("uncertain result = %#v", result)
	}
}

func TestDuplicateSuppressionAndIdempotencyConflict(t *testing.T) {
	service, _, now := testService(t)
	rule := testRule("owner-a", now)
	rule.Cooldown = 24 * time.Hour
	mustPutRule(t, service, rule)

	firstSignal := testSignal("owner-a", SignalDeadline, now)
	first := mustEvaluate(t, service, "owner-a", rule.ID, firstSignal)
	replay := mustEvaluate(t, service, "owner-a", rule.ID, firstSignal)
	if !replay.IdempotentReplay || replay.Proposal == nil || replay.Proposal.ID != first.Proposal.ID {
		t.Fatalf("replay = %#v", replay)
	}

	changed := firstSignal
	changed.Summary = "same idempotency key but changed payload"
	if _, err := service.Evaluate(context.Background(), "owner-a", rule.ID, changed); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("changed replay error = %v, want idempotency conflict", err)
	}

	related := testSignal("owner-a", SignalDeadline, now)
	related.ID = "signal-related"
	related.IdempotencyKey = "idem-related"
	related.Summary = "new observation for the same open loop"
	result := mustEvaluate(t, service, "owner-a", rule.ID, related)
	if !result.Suppressed || result.Suppression != SuppressionCooldown {
		t.Fatalf("cooldown result = %#v", result)
	}
}

func TestQuietHoursDeferNotificationWithoutExecuting(t *testing.T) {
	now := time.Date(2026, 7, 30, 23, 15, 0, 0, time.UTC)
	repository := NewMemoryRepository()
	service, err := NewService(repository, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	rule := testRule("owner-a", now)
	rule.QuietHours = QuietHours{Enabled: true, StartMinute: 22 * 60, EndMinute: 6 * 60, TimeZone: "UTC"}
	mustPutRule(t, service, rule)

	result := mustEvaluate(t, service, "owner-a", rule.ID, testSignal("owner-a", SignalWaitingState, now))
	want := time.Date(2026, 7, 31, 6, 0, 0, 0, time.UTC)
	if !result.Deferred || result.Proposal == nil || !result.Proposal.NotifyAfter.Equal(want) {
		t.Fatalf("quiet-hours result = %#v, want notify %s", result, want)
	}
	if result.Proposal.ExecutionAllowed || result.Proposal.Action.ExternalEffect {
		t.Fatal("proactive proposal must never authorize or perform execution")
	}
}

func TestHighRiskAndRegulatedSignalsRequireApproval(t *testing.T) {
	service, _, now := testService(t)
	rule := testRule("owner-a", now)
	mustPutRule(t, service, rule)

	highRisk := testSignal("owner-a", SignalCommitment, now)
	highRisk.Risk = RiskHigh
	result := mustEvaluate(t, service, "owner-a", rule.ID, highRisk)
	if result.Proposal == nil || !result.Proposal.ApprovalRequired || result.Proposal.ApprovalReason == "" {
		t.Fatalf("high-risk proposal = %#v", result.Proposal)
	}

	legal := testSignal("owner-a", SignalReviewQueue, now)
	legal.ID = "legal-signal"
	legal.IdempotencyKey = "legal-idem"
	legal.OpenLoopKey = "legal-loop"
	legal.Domain = DomainLegal
	result = mustEvaluate(t, service, "owner-a", rule.ID, legal)
	if result.Proposal == nil || !result.Proposal.ApprovalRequired {
		t.Fatalf("legal proposal = %#v", result.Proposal)
	}
}

func TestSensitiveAndLowConfidenceSignalsAreSuppressed(t *testing.T) {
	service, _, now := testService(t)
	rule := testRule("owner-a", now)
	rule.MinimumConfidence = 0.75
	mustPutRule(t, service, rule)

	sensitive := testSignal("owner-a", SignalCapacityConstraint, now)
	sensitive.Sensitivity = SensitivityRestricted
	result := mustEvaluate(t, service, "owner-a", rule.ID, sensitive)
	if !result.Suppressed || result.Suppression != SuppressionSensitive {
		t.Fatalf("sensitive result = %#v", result)
	}

	uncertain := testSignal("owner-a", SignalCapacityConstraint, now)
	uncertain.ID = "low-confidence"
	uncertain.IdempotencyKey = "low-confidence-idem"
	uncertain.OpenLoopKey = "low-confidence-loop"
	uncertain.Confidence = 0.5
	result = mustEvaluate(t, service, "owner-a", rule.ID, uncertain)
	if !result.Suppressed || result.Suppression != SuppressionLowConfidence {
		t.Fatalf("low-confidence result = %#v", result)
	}
}

func TestDeadlineEscalationIsBounded(t *testing.T) {
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	current := now
	repository := NewMemoryRepository()
	service, err := NewService(repository, func() time.Time { return current })
	if err != nil {
		t.Fatal(err)
	}
	rule := testRule("owner-a", now)
	rule.Retry = RetryPolicy{
		Intervals:      []time.Duration{time.Hour},
		MaxAttempts:    1,
		MaxEscalations: 1,
	}
	mustPutRule(t, service, rule)
	signal := testSignal("owner-a", SignalDeadline, now)
	due := now.Add(-time.Hour)
	signal.DueAt = &due
	proposal := *mustEvaluate(t, service, "owner-a", rule.ID, signal).Proposal
	if proposal.Score.Components[1].Value != 1 {
		t.Fatalf("overdue urgency = %v, want 1", proposal.Score.Components[1].Value)
	}

	current = current.Add(time.Hour)
	proposal, err = service.RecordReviewAttempt(context.Background(), "owner-a", proposal.ID, proposal.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if proposal.EscalationCount != 1 || proposal.ReviewAttempts != 0 || proposal.NextReviewAt == nil {
		t.Fatalf("first escalation = %#v", proposal)
	}

	current = current.Add(time.Hour)
	proposal, err = service.RecordReviewAttempt(context.Background(), "owner-a", proposal.ID, proposal.Revision)
	if !errors.Is(err, ErrScheduleExhausted) {
		t.Fatalf("second attempt error = %v", err)
	}
	if proposal.Status != StatusExpired || proposal.EscalationCount != 1 || proposal.NextReviewAt != nil {
		t.Fatalf("exhausted proposal = %#v", proposal)
	}
}

func TestResolvedOpenLoopCannotBeReopenedByNewSignal(t *testing.T) {
	service, _, now := testService(t)
	rule := testRule("owner-a", now)
	mustPutRule(t, service, rule)
	signal := testSignal("owner-a", SignalWaitingState, now)
	proposal := mustEvaluate(t, service, "owner-a", rule.ID, signal).Proposal
	resolved, err := service.Transition(context.Background(), "owner-a", proposal.ID, proposal.Revision, StatusResolved)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Status != StatusResolved {
		t.Fatalf("status = %s", resolved.Status)
	}

	newSignal := signal
	newSignal.ID = "new-signal"
	newSignal.IdempotencyKey = "new-idempotency"
	newSignal.Summary = "new source observation after resolution"
	result := mustEvaluate(t, service, "owner-a", rule.ID, newSignal)
	if !result.Suppressed || result.Suppression != SuppressionResolved {
		t.Fatalf("resolved-loop result = %#v", result)
	}
}

func TestRuleDigestAndScoringAreDeterministic(t *testing.T) {
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	serviceA, _, _ := testServiceAt(t, now)
	serviceB, _, _ := testServiceAt(t, now)
	ruleA := mustPutRule(t, serviceA, testRule("owner-a", now))
	ruleBInput := testRule("owner-a", now)
	ruleBInput.SignalTypes = []SignalType{
		SignalReviewQueue, SignalCapacityConstraint, SignalRecurringObligation, SignalSourceChange,
		SignalStaleWork, SignalWaitingState, SignalCommitment, SignalDeadline,
	}
	ruleB := mustPutRule(t, serviceB, ruleBInput)
	if ruleA.Digest != ruleB.Digest {
		t.Fatalf("rule digests differ: %s != %s", ruleA.Digest, ruleB.Digest)
	}

	signal := testSignal("owner-a", SignalDeadline, now)
	resultA := mustEvaluate(t, serviceA, "owner-a", ruleA.ID, signal)
	resultB := mustEvaluate(t, serviceB, "owner-a", ruleB.ID, signal)
	if resultA.Proposal.ID != resultB.Proposal.ID {
		t.Fatalf("proposal ids differ: %s != %s", resultA.Proposal.ID, resultB.Proposal.ID)
	}
	if !reflect.DeepEqual(resultA.Proposal.Score, resultB.Proposal.Score) {
		t.Fatalf("scores differ:\n%#v\n%#v", resultA.Proposal.Score, resultB.Proposal.Score)
	}
}

func TestLearningChangesOnlyBoundedRankingWeights(t *testing.T) {
	service, _, now := testService(t)
	rule := testRule("owner-a", now)
	mustPutRule(t, service, rule)
	signal := testSignal("owner-a", SignalDeadline, now)
	signal.Risk = RiskCritical
	proposal := mustEvaluate(t, service, "owner-a", rule.ID, signal).Proposal

	weights := DefaultScoreWeights()
	for i := 0; i < 100; i++ {
		var err error
		weights, err = service.RecordFeedback(context.Background(), Feedback{
			OwnerIdentity: "owner-a",
			ProposalID:    proposal.ID,
			Outcome:       FeedbackNotUseful,
			Component:     ComponentRisk,
			OccurredAt:    now,
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if weights.Risk < 0.10 || weights.Risk > 0.50 {
		t.Fatalf("learned risk weight escaped bounds: %#v", weights)
	}

	next := testSignal("owner-a", SignalReviewQueue, now)
	next.ID = "next-high-risk"
	next.IdempotencyKey = "next-high-risk-idem"
	next.OpenLoopKey = "next-high-risk-loop"
	next.Risk = RiskHigh
	result := mustEvaluate(t, service, "owner-a", rule.ID, next)
	if result.Proposal == nil || !result.Proposal.ApprovalRequired || result.Proposal.ExecutionAllowed {
		t.Fatalf("learning relaxed safety: %#v", result.Proposal)
	}

	sensitive := testSignal("owner-a", SignalReviewQueue, now)
	sensitive.ID = "sensitive-after-learning"
	sensitive.IdempotencyKey = "sensitive-after-learning-idem"
	sensitive.OpenLoopKey = "sensitive-after-learning-loop"
	sensitive.Sensitivity = SensitivitySensitive
	result = mustEvaluate(t, service, "owner-a", rule.ID, sensitive)
	if !result.Suppressed || result.Suppression != SuppressionSensitive {
		t.Fatalf("learning relaxed sensitive suppression: %#v", result)
	}
}

func TestAllSupportedSignalTypesProduceTypedSafeActions(t *testing.T) {
	service, _, now := testService(t)
	rule := testRule("owner-a", now)
	mustPutRule(t, service, rule)
	types := []SignalType{
		SignalDeadline,
		SignalCommitment,
		SignalWaitingState,
		SignalStaleWork,
		SignalSourceChange,
		SignalRecurringObligation,
		SignalCapacityConstraint,
		SignalReviewQueue,
	}
	for index, signalType := range types {
		signal := testSignal("owner-a", signalType, now)
		signal.ID = "signal-" + string(signalType)
		signal.IdempotencyKey = "idem-" + string(signalType)
		signal.OpenLoopKey = "loop-" + string(signalType)
		signal.Sources[0].ID = "source-" + string(signalType)
		result := mustEvaluate(t, service, "owner-a", rule.ID, signal)
		if result.Proposal == nil || result.Proposal.Action.Kind == "" || result.Proposal.Action.ExternalEffect || result.Proposal.ExecutionAllowed {
			t.Fatalf("type[%d] %s produced unsafe action: %#v", index, signalType, result)
		}
	}
}

func testService(t *testing.T) (*Service, *MemoryRepository, time.Time) {
	t.Helper()
	return testServiceAt(t, time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC))
}

func testServiceAt(t *testing.T, now time.Time) (*Service, *MemoryRepository, time.Time) {
	t.Helper()
	repository := NewMemoryRepository()
	service, err := NewService(repository, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	return service, repository, now
}

func testRule(owner string, now time.Time) TriggerRule {
	return TriggerRule{
		ID:                "default-open-loop-rule",
		OwnerIdentity:     owner,
		Version:           1,
		Name:              "Default open-loop proposals",
		Enabled:           true,
		SignalTypes:       []SignalType{SignalDeadline, SignalCommitment, SignalWaitingState, SignalStaleWork, SignalSourceChange, SignalRecurringObligation, SignalCapacityConstraint, SignalReviewQueue},
		MinimumConfidence: 0.70,
		MaximumSourceAge:  24 * time.Hour,
		Cooldown:          0,
		ProposalTTL:       14 * 24 * time.Hour,
		Weights:           DefaultScoreWeights(),
		Retry:             RetryPolicy{Intervals: []time.Duration{time.Hour, 6 * time.Hour}, MaxAttempts: 2, MaxEscalations: 2},
		CreatedAt:         now,
	}
}

func testSignal(owner string, signalType SignalType, now time.Time) Signal {
	due := now.Add(12 * time.Hour)
	return Signal{
		ContractVersion: ContractVersion,
		ID:              "signal-1",
		IdempotencyKey:  "idem-test-1",
		OwnerIdentity:   owner,
		Type:            signalType,
		OpenLoopKey:     "open-loop-1",
		Title:           "Open work needs attention",
		Summary:         "A source-backed open loop has a concrete next review step.",
		Responsible:     ResponsibleRobert,
		Domain:          DomainGeneral,
		Risk:            RiskMedium,
		Sensitivity:     SensitivityStandard,
		Confidence:      0.95,
		Relevance:       0.85,
		Importance:      0.80,
		OccurredAt:      now.Add(-time.Hour),
		DueAt:           &due,
		Sources: []SourceReference{{
			ID:           "source-1",
			Kind:         "connected_record",
			Locator:      "local://records/1",
			ContentHash:  "sha256:0123456789abcdef",
			ObservedAt:   now.Add(-30 * time.Minute),
			RetrievedAt:  now.Add(-15 * time.Minute),
			Verification: VerificationSourceSupported,
		}},
	}
}

func mustPutRule(t *testing.T, service *Service, rule TriggerRule) TriggerRule {
	t.Helper()
	saved, err := service.PutRule(context.Background(), rule)
	if err != nil {
		t.Fatal(err)
	}
	return saved
}

func mustEvaluate(t *testing.T, service *Service, owner, ruleID string, signal Signal) EvaluationResult {
	t.Helper()
	result, err := service.Evaluate(context.Background(), owner, ruleID, signal)
	if err != nil {
		t.Fatal(err)
	}
	return result
}
