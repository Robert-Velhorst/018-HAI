// Package providerfallback implements a pure, deterministic provider-selection
// policy. Given an ordered list of candidate providers, it picks the first
// available one, always preferring free/local providers over paid ones and
// never selecting a paid provider unless paid usage is explicitly allowed.
package providerfallback

import "strings"

// Provider is a candidate generation backend.
type Provider struct {
	Name      string `json:"name"`
	Available bool   `json:"available"`
	Paid      bool   `json:"paid"`
}

// Select returns the chosen provider following the fallback policy:
//  1. named, free/local available providers, in order;
//  2. only if allowPaid, named paid available providers, in order.
//
// Providers without a nonblank name are not selectable. It returns ok=false
// when nothing is selectable. Selection is deterministic: the same input
// always yields the same result.
func Select(providers []Provider, allowPaid bool) (Provider, bool) {
	// First pass: free/local providers.
	for _, p := range providers {
		if p.Available && !p.Paid && hasName(p.Name) {
			return p, true
		}
	}
	// Second pass: paid providers, only when explicitly allowed.
	if allowPaid {
		for _, p := range providers {
			if p.Available && p.Paid && hasName(p.Name) {
				return p, true
			}
		}
	}
	return Provider{}, false
}

func hasName(name string) bool {
	return strings.TrimSpace(name) != ""
}
