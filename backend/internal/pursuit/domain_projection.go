package pursuit

import (
	"automation-hub-backend/internal/lifeops"
	"automation-hub-backend/internal/models"
	"fmt"
	"strings"
)

func canonicalPursuitDomain(value, contextText string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "-", "_")
	value = strings.ReplaceAll(value, " ", "_")
	if value == "" {
		value = classifyDomain(contextText)
	}
	if !lifeops.IsCanonicalDomain(lifeops.DomainID(value)) {
		return "", fmt.Errorf("domain must be one of HAI's canonical life domains, got %q", value)
	}
	return value, nil
}

// LifeDomainLinker keeps the canonical life-domain index independent from the
// pursuit repository. It is intentionally write-only: planning reads the
// life-operations ledger through its own bounded interfaces.
type LifeDomainLinker interface {
	LinkEntity(request lifeops.LinkEntityRequest) (*lifeops.EntityDomainLink, error)
}

type LifeDomainReconciliationResult struct {
	Scanned   int      `json:"scanned"`
	Projected int      `json:"projected"`
	Skipped   int      `json:"skipped"`
	Failed    int      `json:"failed"`
	Failures  []string `json:"failures,omitempty"`
}

// WithLifeDomainLinker attaches the durable life-domain projection used by
// cross-domain planning and context retrieval. Preview services and tests that
// do not supply a linker retain their current behavior.
func WithLifeDomainLinker(value Service, linker LifeDomainLinker) Service {
	concrete, ok := value.(*service)
	if !ok || concrete == nil || linker == nil {
		return value
	}
	concrete.lifeDomainLinker = linker
	return concrete
}

func (s *service) projectLifeDomain(pursuit *models.Pursuit, actor string) (bool, error) {
	if s.lifeDomainLinker == nil {
		return false, nil
	}
	if pursuit == nil || strings.TrimSpace(pursuit.OwnerIdentity) == "" {
		return false, nil
	}

	domainID := lifeops.DomainID(strings.TrimSpace(pursuit.Domain))
	if !lifeops.IsCanonicalDomain(domainID) {
		// Historical records may use pre-ontology labels. They remain readable;
		// only canonical domains are eligible for the shared index.
		return false, nil
	}

	_, err := s.lifeDomainLinker.LinkEntity(lifeops.LinkEntityRequest{
		OwnerIdentity:      pursuit.OwnerIdentity,
		EntityType:         "pursuit",
		EntityID:           pursuit.ID.String(),
		DomainID:           domainID,
		Primary:            true,
		Confidence:         pursuit.Confidence,
		SourceLabel:        "pursuit domain",
		Evidence:           []string{fmt.Sprintf("pursuit:%s", pursuit.ID)},
		VerificationStatus: "source_supported",
	})
	if err != nil {
		_, _ = s.recordActivity(
			pursuit.ID,
			"pursuit.life_domain_projection_failed",
			"Pursuit was saved, but its life-domain index could not be updated: "+err.Error(),
			firstNonEmpty(actor, "system"),
			"life_domain",
			string(domainID),
			"",
		)
		return false, err
	}

	_, _ = s.recordActivity(
		pursuit.ID,
		"pursuit.life_domain_projected",
		"Pursuit domain indexed for whole-life planning: "+string(domainID),
		firstNonEmpty(actor, "system"),
		"life_domain",
		string(domainID),
		"",
	)
	return true, nil
}

// ReconcileLifeDomainsForOwner repairs the local life-domain index for an
// owner's existing pursuits. It does not change pursuit content or execute
// external actions; callers receive per-run counts and concise failures.
func (s *service) ReconcileLifeDomainsForOwner(ownerIdentity, actor string) (*LifeDomainReconciliationResult, error) {
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	if ownerIdentity == "" {
		return nil, fmt.Errorf("owner identity is required")
	}
	if s.lifeDomainLinker == nil {
		return nil, fmt.Errorf("life-domain projection is unavailable")
	}
	pursuits, err := s.ListForOwner(ownerIdentity, true)
	if err != nil {
		return nil, err
	}
	result := &LifeDomainReconciliationResult{Failures: []string{}}
	for index := range pursuits {
		pursuit := &pursuits[index]
		result.Scanned++
		projected, err := s.projectLifeDomain(pursuit, firstNonEmpty(actor, "operator"))
		if err != nil {
			result.Failed++
			result.Failures = append(result.Failures, pursuit.ID.String()+": "+err.Error())
			continue
		}
		if !projected {
			result.Skipped++
			continue
		}
		result.Projected++
	}
	if len(result.Failures) == 0 {
		result.Failures = nil
	}
	return result, nil
}
