// Package privacyfilter is a deterministic, local privacy/PII scanner run before
// storage, indexing, model calls, and external provider use (§20). It is
// rule-based (regex) and never claims perfect PII detection; it fails safe by
// marking anything with detected secrets as unsafe for cloud models.
package privacyfilter

import (
	"regexp"
	"strings"
)

// RiskLevel is the privacy risk of a scanned item.
type RiskLevel string

const (
	RiskNone     RiskLevel = "none"
	RiskLow      RiskLevel = "low"
	RiskMedium   RiskLevel = "medium"
	RiskHigh     RiskLevel = "high"
	RiskCritical RiskLevel = "critical"
)

func (r RiskLevel) String() string { return string(r) }

func (r RiskLevel) IsValid() bool {
	switch r {
	case RiskNone, RiskLow, RiskMedium, RiskHigh, RiskCritical:
		return true
	}
	return false
}

// ScanResult is a PrivacyScanRecord's computed fields (§20).
type ScanResult struct {
	PrivacyRiskLevel                     RiskLevel `json:"privacyRiskLevel"`
	SensitiveFields                      []string  `json:"sensitiveFields"`
	RedactionApplied                     bool      `json:"redactionApplied"`
	RedactedPreview                      string    `json:"redactedPreview"`
	SafeForCloudModel                    bool      `json:"safeForCloudModel"`
	SafeForLocalModel                    bool      `json:"safeForLocalModel"`
	SafeForMemory                        bool      `json:"safeForMemory"`
	RequiresReviewBeforeExternalProvider bool      `json:"requiresReviewBeforeExternalProvider"`
}

type rule struct {
	field   string
	re      *regexp.Regexp
	secret  bool      // if matched, content is not safe for cloud
	minRisk RiskLevel // risk floor contributed by this rule
}

var (
	rules = []rule{
		{"private_key", regexp.MustCompile(`(?s)-----BEGIN [A-Z ]*PRIVATE KEY-----.*?-----END [A-Z ]*PRIVATE KEY-----`), true, RiskCritical},
		{"jwt", regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{5,}\.[A-Za-z0-9_-]{5,}\.[A-Za-z0-9_-]{0,}`), true, RiskCritical},
		{"bearer_token", regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9._-]{12,}`), true, RiskCritical},
		{"github_token", regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{20,}\b`), true, RiskCritical},
		{"api_key", regexp.MustCompile(`(?i)(api[_-]?key|secret|token)\s*[:=]\s*['"]?[A-Za-z0-9._-]{12,}`), true, RiskCritical},
		{"password", regexp.MustCompile(`(?i)password\s*[:=]\s*\S+`), true, RiskCritical},
		{"iban", regexp.MustCompile(`\b[A-Z]{2}\d{2}[A-Z0-9]{10,30}\b`), false, RiskHigh},
		{"bsn", regexp.MustCompile(`\b\d{9}\b`), false, RiskHigh},
		{"email", regexp.MustCompile(`\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}\b`), false, RiskMedium},
		{"phone", regexp.MustCompile(`(?:\+?\d[\d\s().-]{7,}\d)`), false, RiskLow},
		{"account_url", regexp.MustCompile(`https?://[^\s]*\b(token|auth|session)=`), true, RiskHigh},
	}
	riskOrder = map[RiskLevel]int{RiskNone: 0, RiskLow: 1, RiskMedium: 2, RiskHigh: 3, RiskCritical: 4}
)

// Scan runs the deterministic rules over content and returns the privacy result.
// maxPreview bounds the redacted preview length (<=0 uses a default of 280).
func Scan(content string, maxPreview int) ScanResult {
	if maxPreview <= 0 {
		maxPreview = 280
	}
	res := ScanResult{
		PrivacyRiskLevel:  RiskNone,
		SafeForCloudModel: true,
		SafeForLocalModel: true,
		SafeForMemory:     true,
	}
	redacted := content
	seen := map[string]bool{}
	hasSecret := false

	for _, ru := range rules {
		if !ru.re.MatchString(content) {
			continue
		}
		if !seen[ru.field] {
			res.SensitiveFields = append(res.SensitiveFields, ru.field)
			seen[ru.field] = true
		}
		if riskOrder[ru.minRisk] > riskOrder[res.PrivacyRiskLevel] {
			res.PrivacyRiskLevel = ru.minRisk
		}
		if ru.secret {
			hasSecret = true
		}
		redacted = ru.re.ReplaceAllString(redacted, "[REDACTED:"+ru.field+"]")
		res.RedactionApplied = true
	}

	if hasSecret {
		res.SafeForCloudModel = false
	}
	// Medium+ risk requires review before an external provider is used.
	if riskOrder[res.PrivacyRiskLevel] >= riskOrder[RiskMedium] {
		res.RequiresReviewBeforeExternalProvider = true
	}
	// High/critical raw content must not be reused freely.
	if riskOrder[res.PrivacyRiskLevel] >= riskOrder[RiskHigh] {
		res.SafeForMemory = false
	}

	res.RedactedPreview = boundedPreview(redacted, maxPreview)
	return res
}

func boundedPreview(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}
