package memory

import (
	"testing"
	"time"
)

func TestPersistedPromotionAcceptsOnlyTrustedFreshEvidence(t *testing.T) {
	for _, test := range []struct {
		value string
		want  bool
	}{
		{value: "connected_source:provenance_verified", want: true},
		{value: "trusted_external:official registry", want: true},
		{value: "", want: false},
		{value: "connected_source:unverified", want: false},
		{value: "trusted_external: ", want: false},
	} {
		if got := isPersistedTrustedAuthority(test.value); got != test.want {
			t.Errorf("isPersistedTrustedAuthority(%q) = %t, want %t", test.value, got, test.want)
		}
	}
	for _, test := range []struct {
		value string
		want  bool
	}{
		{value: "fresh", want: true},
		{value: " RECENT ", want: true},
		{value: "", want: false},
		{value: "stale", want: false},
		{value: "unknown", want: false},
	} {
		if got := isPersistedFreshness(test.value); got != test.want {
			t.Errorf("isPersistedFreshness(%q) = %t, want %t", test.value, got, test.want)
		}
	}
}

func TestPersistedPromotionRequiresExactCompleteEvidenceSentence(t *testing.T) {
	const claim = "The review is scheduled for Friday."
	for _, test := range []struct {
		name    string
		snippet string
		want    bool
	}{
		{name: "exact sentence in source", snippet: "Meeting notes. The review is scheduled for Friday. Next steps follow.", want: true},
		{name: "normalized whitespace and case", snippet: "THE   REVIEW IS SCHEDULED FOR FRIDAY!", want: true},
		{name: "paraphrase", snippet: "The review will happen Friday.", want: false},
		{name: "partial sentence", snippet: "The review is scheduled for Friday afternoon.", want: false},
		{name: "substring in longer word", snippet: "Thereview is scheduled for Friday.", want: false},
		{name: "missing evidence", snippet: "  ", want: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := containsExactEvidenceSentence(claim, test.snippet); got != test.want {
				t.Fatalf("containsExactEvidenceSentence(%q, %q) = %t, want %t", claim, test.snippet, got, test.want)
			}
		})
	}
}

func TestPersistedPromotionRejectsMissingStaleOrFutureTimestamps(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name string
		at   time.Time
		want bool
	}{
		{name: "recent", at: now.Add(-29 * 24 * time.Hour), want: true},
		{name: "at age limit", at: now.Add(-maximumPersistedEvidenceAge), want: true},
		{name: "stale", at: now.Add(-maximumPersistedEvidenceAge - time.Second), want: false},
		{name: "missing", at: time.Time{}, want: false},
		{name: "far future", at: now.Add(6 * time.Minute), want: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := isRecentPersistedTimestamp(test.at, now); got != test.want {
				t.Fatalf("isRecentPersistedTimestamp(%v) = %t, want %t", test.at, got, test.want)
			}
		})
	}
}
