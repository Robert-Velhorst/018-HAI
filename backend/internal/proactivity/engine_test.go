package proactivity

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestEvaluateIsDeterministicAndOrderIndependent(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	first := testSignal("owner-a", "signal-b", "loop-b", now)
	second := testSignal("owner-a", "signal-a", "loop-a", now)
	second.Impact = 0.4
	requestA := testRequest("owner-a", now, []OpenLoopSignal{first, second})
	requestB := testRequest("owner-a", now, []OpenLoopSignal{second, first})

	resultA, err := Evaluate(requestA)
	if err != nil {
		t.Fatal(err)
	}
	resultB, err := Evaluate(requestB)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(resultA, resultB) {
		t.Fatalf("results differ:\n%#v\n%#v", resultA, resultB)
	}
	if resultA.Decisions[0].SignalID != "signal-b" {
		t.Fatalf("decisions are not ranked: %#v", resultA.Decisions)
	}
}

func TestOwnerIsolationFailsClosed(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	request := testRequest("owner-a", now, []OpenLoopSignal{testSignal("owner-b", "signal-a", "loop-a", now)})
	if _, err := Evaluate(request); err == nil || !strings.Contains(err.Error(), "owner") {
		t.Fatalf("cross-owner signal error = %v", err)
	}

	request = testRequest("owner-a", now, []OpenLoopSignal{testSignal("owner-a", "signal-a", "loop-a", now)})
	request.History = []DecisionHistory{{
		ContractVersion: ContractVersion,
		OwnerIdentity:   "owner-b",
		OpenLoopKey:     "loop-a",
		SignalDigest:    strings.Repeat("a", 64),
		Outcome:         OutcomeNotify,
		DecidedAt:       now.Add(-time.Hour),
	}}
	if _, err := Evaluate(request); err == nil || !strings.Contains(err.Error(), "owner") {
		t.Fatalf("cross-owner history error = %v", err)
	}
}

func TestQuietHoursDowngradeNotification(t *testing.T) {
	now := time.Date(2026, 7, 31, 23, 15, 0, 0, time.UTC)
	request := testRequest("owner-a", now, []OpenLoopSignal{testSignal("owner-a", "signal-a", "loop-a", now)})
	request.Preferences.QuietHours = QuietHours{
		Enabled: true, StartMinute: 22 * 60, EndMinute: 6 * 60, TimeZone: "UTC",
	}
	result, err := Evaluate(request)
	if err != nil {
		t.Fatal(err)
	}
	decision := result.Decisions[0]
	if decision.Outcome != OutcomeDailyBrief || decision.BudgetCost != 0 {
		t.Fatalf("quiet-hours decision = %#v", decision)
	}
	assertNoAuthority(t, decision)
}

func TestHighRiskLowConfidenceRequiresReview(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	signal := testSignal("owner-a", "signal-a", "loop-a", now)
	signal.Risk = RiskHigh
	signal.Confidence = 0.2
	request := testRequest("owner-a", now, []OpenLoopSignal{signal})
	result, err := Evaluate(request)
	if err != nil {
		t.Fatal(err)
	}
	decision := result.Decisions[0]
	if decision.Outcome != OutcomeRequireReview || !containsReason(decision.Reasons, "confidence") {
		t.Fatalf("high-risk uncertain decision = %#v", decision)
	}
	assertNoAuthority(t, decision)
}

func TestExactDuplicateAndCooldownAreSuppressed(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	signal := testSignal("owner-a", "signal-a", "loop-a", now)
	first, err := Evaluate(testRequest("owner-a", now, []OpenLoopSignal{signal}))
	if err != nil {
		t.Fatal(err)
	}
	history := DecisionHistory{
		ContractVersion: ContractVersion,
		OwnerIdentity:   "owner-a",
		OpenLoopKey:     "loop-a",
		SignalDigest:    first.Decisions[0].SignalDigest,
		Outcome:         first.Decisions[0].Outcome,
		DecidedAt:       now.Add(-time.Hour),
	}
	request := testRequest("owner-a", now, []OpenLoopSignal{signal})
	request.History = []DecisionHistory{history}
	duplicate, err := Evaluate(request)
	if err != nil {
		t.Fatal(err)
	}
	if duplicate.Decisions[0].Outcome != OutcomeSuppress || !containsReason(duplicate.Decisions[0].Reasons, "identical") {
		t.Fatalf("duplicate decision = %#v", duplicate.Decisions[0])
	}

	changed := signal
	changed.Summary = "new evidence for the same open loop"
	request.Signals = []OpenLoopSignal{changed}
	cooldown, err := Evaluate(request)
	if err != nil {
		t.Fatal(err)
	}
	decision := cooldown.Decisions[0]
	if decision.Outcome != OutcomeSuppress || decision.NextEligibleAt == nil ||
		!decision.NextEligibleAt.Equal(history.DecidedAt.Add(request.Preferences.Cooldown)) {
		t.Fatalf("cooldown decision = %#v", decision)
	}
}

func TestAttentionBudgetGoesToHighestRankedSignal(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	high := testSignal("owner-a", "high", "loop-high", now)
	low := testSignal("owner-a", "low", "loop-low", now)
	low.Impact = 0.65
	low.Urgency = 0.75
	request := testRequest("owner-a", now, []OpenLoopSignal{low, high})
	request.Preferences.AttentionBudget.MaxInterruptionsPerDay = 1
	request.Preferences.ReviewThreshold = 1
	result, err := Evaluate(request)
	if err != nil {
		t.Fatal(err)
	}
	if result.Decisions[0].SignalID != "high" || result.Decisions[0].Outcome != OutcomeNotify || result.Decisions[0].BudgetCost != 1 {
		t.Fatalf("highest decision = %#v", result.Decisions[0])
	}
	if result.Decisions[1].Outcome != OutcomeDailyBrief || result.Decisions[1].BudgetCost != 0 {
		t.Fatalf("lower decision = %#v", result.Decisions[1])
	}
	if result.InterruptionsUsed != 1 || result.InterruptionsRemaining != 0 {
		t.Fatalf("budget result = %#v", result)
	}
}

func TestExistingDailyBudgetIsRespectedInOwnerTimeZone(t *testing.T) {
	now := time.Date(2026, 7, 31, 1, 0, 0, 0, time.UTC)
	request := testRequest("owner-a", now, []OpenLoopSignal{testSignal("owner-a", "signal-a", "loop-a", now)})
	request.Preferences.TimeZone = "Europe/Amsterdam"
	request.Preferences.AttentionBudget.MaxInterruptionsPerDay = 1
	request.History = []DecisionHistory{{
		ContractVersion: ContractVersion,
		OwnerIdentity:   "owner-a",
		OpenLoopKey:     "other-loop",
		SignalDigest:    strings.Repeat("a", 64),
		Outcome:         OutcomeNotify,
		DecidedAt:       time.Date(2026, 7, 30, 23, 30, 0, 0, time.UTC),
	}}
	result, err := Evaluate(request)
	if err != nil {
		t.Fatal(err)
	}
	if result.Decisions[0].Outcome != OutcomeDailyBrief || result.InterruptionsUsed != 1 {
		t.Fatalf("time-zone budget result = %#v", result)
	}
}

func TestRecommendedChannelsStayLocalFirstAndBounded(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	request := testRequest("owner-a", now, []OpenLoopSignal{testSignal("owner-a", "signal-a", "loop-a", now)})
	request.Preferences.AllowExternalChannels = true
	request.Preferences.Channels = []ChannelPreference{
		{Channel: ChannelEmail, Enabled: true, Order: 0},
		{Channel: ChannelInApp, Enabled: true, Order: 20},
		{Channel: ChannelDesktop, Enabled: true, Order: 10},
		{Channel: ChannelSMS, Enabled: true, Order: 1},
	}
	result, err := Evaluate(request)
	if err != nil {
		t.Fatal(err)
	}
	want := []Channel{ChannelDesktop, ChannelInApp, ChannelEmail}
	if !reflect.DeepEqual(result.Decisions[0].RecommendedChannels, want) {
		t.Fatalf("channels = %#v, want %#v", result.Decisions[0].RecommendedChannels, want)
	}
	assertNoAuthority(t, result.Decisions[0])
}

func TestExternalChannelsRequireExplicitOptIn(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	request := testRequest("owner-a", now, []OpenLoopSignal{testSignal("owner-a", "signal-a", "loop-a", now)})
	request.Preferences.Channels = []ChannelPreference{{Channel: ChannelEmail, Enabled: true}}
	result, err := Evaluate(request)
	if err != nil {
		t.Fatal(err)
	}
	if result.Decisions[0].Outcome != OutcomeDailyBrief || len(result.Decisions[0].RecommendedChannels) != 0 {
		t.Fatalf("external-only decision = %#v", result.Decisions[0])
	}
}

func TestOutputRedactsSecrets(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	signal := testSignal("owner-a", "signal-a", "loop-a", now)
	apiKey := "very" + "-secret-value"
	providerToken := "sk-" + "supersecret123"
	githubToken := "ghp_" + "1234567890abcdefghijklmn"
	signal.Title = "Review api_key=" + apiKey
	signal.Summary = "Connector failed with Authorization: Bearer hidden-token-value, " + providerToken + ", and " + githubToken
	result, err := Evaluate(testRequest("owner-a", now, []OpenLoopSignal{signal}))
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{apiKey, "hidden-token-value", providerToken, githubToken} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("output leaked %q: %s", secret, encoded)
		}
	}
	if !strings.Contains(string(encoded), "[redacted]") {
		t.Fatalf("output does not disclose redaction: %s", encoded)
	}
}

func TestDeadlineAndStalenessAreExplained(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	signal := testSignal("owner-a", "signal-a", "loop-a", now)
	deadline := now.Add(-time.Hour)
	signal.Deadline = &deadline
	signal.LastActivityAt = now.Add(-72 * time.Hour)
	signal.StaleAfter = 24 * time.Hour
	result, err := Evaluate(testRequest("owner-a", now, []OpenLoopSignal{signal}))
	if err != nil {
		t.Fatal(err)
	}
	components := result.Decisions[0].Components
	if componentValue(components, "deadline") != 1 || componentValue(components, "staleness") != 1 {
		t.Fatalf("components = %#v", components)
	}
}

func TestResolvedAndLowConfidenceSignalsSuppress(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	resolved := testSignal("owner-a", "resolved", "resolved-loop", now)
	resolved.Status = StatusResolved
	uncertain := testSignal("owner-a", "uncertain", "uncertain-loop", now)
	uncertain.Confidence = 0.1
	result, err := Evaluate(testRequest("owner-a", now, []OpenLoopSignal{resolved, uncertain}))
	if err != nil {
		t.Fatal(err)
	}
	for _, decision := range result.Decisions {
		if decision.Outcome != OutcomeSuppress {
			t.Fatalf("decision was not suppressed: %#v", decision)
		}
		assertNoAuthority(t, decision)
	}
}

func TestAllPolicyOutcomesAreReachable(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		mutate func(*OpenLoopSignal)
		want   Outcome
	}{
		{
			name: "suppress",
			mutate: func(signal *OpenLoopSignal) {
				signal.Deadline = nil
				signal.Impact = 0.1
				signal.Urgency = 0.1
				signal.Confidence = 0.6
			},
			want: OutcomeSuppress,
		},
		{
			name: "ambient",
			mutate: func(signal *OpenLoopSignal) {
				signal.Deadline = nil
				signal.Impact = 0.45
				signal.Urgency = 0.45
				signal.Confidence = 0.8
			},
			want: OutcomeAmbient,
		},
		{
			name: "daily brief",
			mutate: func(signal *OpenLoopSignal) {
				deadline := now.Add(5 * 24 * time.Hour)
				signal.Deadline = &deadline
				signal.Impact = 0.6
				signal.Urgency = 0.6
			},
			want: OutcomeDailyBrief,
		},
		{
			name:   "notify",
			mutate: func(*OpenLoopSignal) {},
			want:   OutcomeNotify,
		},
		{
			name: "require review",
			mutate: func(signal *OpenLoopSignal) {
				signal.Risk = RiskHigh
			},
			want: OutcomeRequireReview,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			signal := testSignal("owner-a", "signal-a", "loop-a", now)
			test.mutate(&signal)
			result, err := Evaluate(testRequest("owner-a", now, []OpenLoopSignal{signal}))
			if err != nil {
				t.Fatal(err)
			}
			if result.Decisions[0].Outcome != test.want {
				t.Fatalf("outcome = %s, want %s; decision=%#v", result.Decisions[0].Outcome, test.want, result.Decisions[0])
			}
			assertNoAuthority(t, result.Decisions[0])
		})
	}
}

func TestEvaluationIsBounded(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	request := testRequest("owner-a", now, make([]OpenLoopSignal, MaxSignals+1))
	if _, err := Evaluate(request); err == nil || !strings.Contains(err.Error(), "signal count") {
		t.Fatalf("oversized request error = %v", err)
	}
}

func testRequest(owner string, now time.Time, signals []OpenLoopSignal) EvaluationRequest {
	return EvaluationRequest{
		ContractVersion: ContractVersion,
		OwnerIdentity:   owner,
		Now:             now,
		Preferences:     DefaultPreferences(owner),
		Signals:         signals,
	}
}

func testSignal(owner, id, openLoop string, now time.Time) OpenLoopSignal {
	deadline := now.Add(time.Hour)
	return OpenLoopSignal{
		ContractVersion: ContractVersion,
		ID:              id,
		OwnerIdentity:   owner,
		OpenLoopKey:     openLoop,
		Title:           "Open loop " + id,
		Summary:         "A bounded source-backed open loop needs attention.",
		Status:          StatusOpen,
		Risk:            RiskLow,
		ObservedAt:      now.Add(-time.Hour),
		LastActivityAt:  now.Add(-time.Hour),
		Deadline:        &deadline,
		StaleAfter:      24 * time.Hour,
		Impact:          0.8,
		Urgency:         0.8,
		Confidence:      0.9,
	}
}

func assertNoAuthority(t *testing.T, decision Decision) {
	t.Helper()
	if decision.ExecutionAuthorized || decision.DeliveryAuthorized || decision.AuthorityGranted {
		t.Fatalf("proactivity decision granted authority: %#v", decision)
	}
}

func containsReason(reasons []string, fragment string) bool {
	for _, reason := range reasons {
		if strings.Contains(reason, fragment) {
			return true
		}
	}
	return false
}

func componentValue(components []ScoreComponent, name string) float64 {
	for _, component := range components {
		if component.Name == name {
			return component.Value
		}
	}
	return -1
}
