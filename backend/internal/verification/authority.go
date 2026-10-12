package verification

import "strings"

const (
	authorityConnectedProvenance     = "connected_source:provenance_verified"
	authorityConnectedUnverified     = "connected_source:unverified"
	authorityConnectedReviewRequired = "connected_source:owner_review_required"
	authorityExternalUntrusted       = "untrusted_external"
	trustedExternalPrefix            = "trusted_external:"
)

// EvidenceAuthorityResolver is an in-process provenance validation boundary.
// Implementations must authenticate evidence from server-controlled data such
// as a connector record, signed import, or trusted source registry. They must
// not treat the caller-provided authority flags on EvidenceInput as proof.
type EvidenceAuthorityResolver interface {
	ResolveExternalEvidence(request AnswerRequest, evidence EvidenceInput) EvidenceAuthorityResolution
}

type EvidenceAuthorityResolverFunc func(request AnswerRequest, evidence EvidenceInput) EvidenceAuthorityResolution

func (f EvidenceAuthorityResolverFunc) ResolveExternalEvidence(request AnswerRequest, evidence EvidenceInput) EvidenceAuthorityResolution {
	return f(request, evidence)
}

type EvidenceAuthorityResolution struct {
	Trusted   bool
	Authority string
	Official  bool
	Primary   bool
	Reason    string
}

type untrustedExternalAuthorityResolver struct{}

func (untrustedExternalAuthorityResolver) ResolveExternalEvidence(AnswerRequest, EvidenceInput) EvidenceAuthorityResolution {
	return EvidenceAuthorityResolution{
		Authority: authorityExternalUntrusted,
		Reason:    "external evidence provenance was not authenticated by a trusted in-process resolver",
	}
}

func normalizeAuthorityResolution(resolution EvidenceAuthorityResolution) EvidenceAuthorityResolution {
	if !resolution.Trusted {
		resolution.Trusted = false
		resolution.Authority = authorityExternalUntrusted
		resolution.Official = false
		resolution.Primary = false
		if strings.TrimSpace(resolution.Reason) == "" {
			resolution.Reason = "external evidence provenance was not authenticated"
		}
		return resolution
	}

	label := strings.TrimSpace(resolution.Authority)
	if label == "" {
		label = "validated"
	}
	resolution.Authority = trustedExternalPrefix + label
	return resolution
}

func isTrustedEvidence(evidenceAuthority string) bool {
	authority := strings.ToLower(strings.TrimSpace(evidenceAuthority))
	return strings.HasPrefix(authority, trustedExternalPrefix)
}

func isSourceSupportedEvidence(evidenceAuthority string) bool {
	authority := strings.ToLower(strings.TrimSpace(evidenceAuthority))
	return authority == authorityConnectedProvenance || isTrustedEvidence(authority)
}
