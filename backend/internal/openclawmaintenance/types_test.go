package openclawmaintenance

import (
	"strings"
	"testing"
	"time"
)

func freshTestProviderMetadata() *time.Time {
	at := time.Now().UTC()
	return &at
}

func TestOpenClawRevisionOrdering(t *testing.T) {
	for _, tt := range []struct {
		a, b string
		want bool
	}{
		{"2026.7.1-4", "2026.7.1-2", true}, {"2026.9.1", "2026.7.1-4", true},
		{"2026.7.1", "2026.7.1-4", false}, {"2026.7.1-beta.1", "2026.7.1", false},
		{"https://bad.example/pkg", "2026.7.1", false}, {"2026.7.1", "2026.7.1", false},
		{"2026.13.1", "2026.12.31", false}, {"2026.00.1", "2026.12.31", false},
		{"2026.9.18446744073709551616", "2026.9.1", false},
		{"2026.9.1-18446744073709551616", "2026.9.1", false},
	} {
		if got := Newer(tt.a, tt.b); got != tt.want {
			t.Errorf("Newer(%q,%q)=%v", tt.a, tt.b, got)
		}
	}
}

func TestValidVersionRejectsOutOfRangeAndOverflow(t *testing.T) {
	for _, version := range []string{"2026.0.1", "2026.13.1", "2026.9.18446744073709551616", "2026.9.1-18446744073709551616", "2026.9.1" + strings.Repeat("0", 40)} {
		if ValidVersion(version) {
			t.Errorf("accepted unsupported version %q", version)
		}
	}
	for _, version := range []string{"2026.1.1", "v2026.12.31", "2026.9.1-4"} {
		if !ValidVersion(version) {
			t.Errorf("rejected supported version %q", version)
		}
	}
}
func TestApplyReceiptRequiresExactVersionPublisherAndHealth(t *testing.T) {
	j := Job{Target: "gateway_core", Kind: "apply", Version: "2026.9.1", Evidence: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	r := Report{Installed: j.Version, Evidence: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", ProviderMetadataAt: freshTestProviderMetadata(), PublisherVerified: true, HealthOK: true, Outcome: "ok"}
	if err := ValidateReport(j, r); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*Report){func(r *Report) { r.Installed = "2026.8.2" }, func(r *Report) { r.PublisherVerified = false }, func(r *Report) { r.HealthOK = false }, func(r *Report) { r.Evidence = "unverified" }} {
		bad := r
		change(&bad)
		if ValidateReport(j, bad) == nil {
			t.Fatal("accepted unverified update")
		}
	}
}

func TestCompanionPublisherCannotBeReadyWithoutExplicitIdentityPin(t *testing.T) {
	job := Job{Target: "companion", Kind: "check"}
	r := Report{Installed: "2026.6.10", Available: "2026.9.1", Evidence: strings.Repeat("a", 64), ProviderMetadataAt: freshTestProviderMetadata(), PublisherVerified: true, Outcome: "ok"}
	if err := ValidateReport(job, r); err == nil {
		t.Fatal("Companion report with a blank publisher pin was accepted as verified")
	}
	r.PublisherPinStatus = "missing"
	r.PublisherVerified = false
	if err := ValidateReport(job, r); err != nil {
		t.Fatalf("informative blocked Companion version report was rejected: %v", err)
	}
	r.PublisherPinStatus = "verified"
	r.PublisherVerified = true
	if err := ValidateReport(job, r); err != nil {
		t.Fatalf("explicitly verified Companion pin was rejected: %v", err)
	}
}

func TestApplyReceiptCannotSubstituteEvidence(t *testing.T) {
	j := Job{Target: "gateway_core", Kind: "apply", Version: "2026.9.1", Evidence: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	r := Report{Installed: j.Version, Evidence: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", ProviderMetadataAt: freshTestProviderMetadata(), PublisherVerified: true, HealthOK: true, Outcome: "ok"}
	if ValidateReport(j, r) == nil {
		t.Fatal("accepted evidence for a different release")
	}
}

func TestSuccessfulMaintenanceReportRequiresProviderTimestamp(t *testing.T) {
	job := Job{Target: "gateway_core", Kind: "check"}
	report := Report{Installed: "2026.9.1", Available: "2026.9.1", Evidence: strings.Repeat("a", 64), Outcome: "ok"}
	if ValidateReport(job, report) == nil {
		t.Fatal("accepted successful check without provider freshness evidence")
	}
	report.ProviderMetadataAt = freshTestProviderMetadata()
	if err := ValidateReport(job, report); err != nil {
		t.Fatalf("rejected successful check with provider timestamp: %v", err)
	}
}

func TestProviderMetadataFreshnessBoundsAgeAndClockSkew(t *testing.T) {
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	for _, tt := range []struct {
		name string
		at   *time.Time
		want bool
	}{
		{name: "current", at: timePointer(now), want: true},
		{name: "maximum age inclusive", at: timePointer(now.Add(-providerMetadataMaxAge)), want: true},
		{name: "stale", at: timePointer(now.Add(-providerMetadataMaxAge - time.Second))},
		{name: "clock skew within allowance", at: timePointer(now.Add(providerClockSkewAllowance)), want: true},
		{name: "future beyond allowance", at: timePointer(now.Add(providerClockSkewAllowance + time.Second))},
		{name: "missing", at: nil},
		{name: "zero", at: timePointer(time.Time{})},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := providerMetadataFresh(tt.at, now); got != tt.want {
				t.Fatalf("providerMetadataFresh(%v) = %t, want %t", tt.at, got, tt.want)
			}
		})
	}
}

func timePointer(value time.Time) *time.Time { return &value }
