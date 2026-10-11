package source

import "strings"

// sourceContentRequiresReview identifies clear instruction-override attempts
// in imported material. Source text is evidence, never an authority channel;
// these signals force the normal extraction path into review rather than
// allowing automated memory promotion or ungated workflow intake.
func sourceContentRequiresReview(content string) bool {
	normalized := strings.ToLower(normalizeSpaces(content))
	if normalized == "" {
		return false
	}
	for _, signal := range []string{
		"ignore previous instructions",
		"ignore all previous instructions",
		"disregard previous instructions",
		"disregard the system prompt",
		"ignore the system prompt",
		"reveal the system prompt",
		"bypass approval",
		"bypass the approval",
		"disable the safety",
		"override the safety",
		"do not follow your policy",
		"do not follow the policy",
	} {
		if strings.Contains(normalized, signal) {
			return true
		}
	}
	return false
}
