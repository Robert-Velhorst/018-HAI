package task

import (
	"context"
	"fmt"
	"strings"
	"time"

	"automation-hub-backend/internal/frameworkregistry"
	"automation-hub-backend/internal/lifeontology"
	"automation-hub-backend/internal/llm"
)

const lifeOntologyContextLimit = 6

// LifeOntologyContextProvider is a read-only advisory boundary. It can suggest
// relevant owner-scoped context but cannot write memory, approve, or execute.
type LifeOntologyContextProvider interface {
	SuggestNextContext(context.Context, lifeontology.ContextSuggestionRequest) (lifeontology.ContextSuggestionResult, error)
}

func WithLifeOntologyContext(base Service, provider LifeOntologyContextProvider) (Service, error) {
	implementation, ok := base.(*service)
	if !ok {
		return nil, fmt.Errorf("whole-life context requires the built-in task service")
	}
	if provider == nil {
		return nil, fmt.Errorf("whole-life context provider is required")
	}
	implementation.lifeOntology = provider
	return implementation, nil
}

func (s *service) retrieveLifeOntologyContext(
	request IntakeRequest,
	frameworkDecision *frameworkregistry.SelectionDecision,
	modelDecision llm.RouteDecision,
) ([]lifeontology.ContextSuggestion, string, error) {
	if s == nil || s.lifeOntology == nil {
		return nil, "Whole-life ontology context is not configured for this task service.", nil
	}
	owner := strings.TrimSpace(request.OwnerIdentity)
	if owner == "" {
		return nil, "Whole-life ontology context was skipped because no verified owner scope is available.", nil
	}
	domains := lifeOntologyDomains(frameworkDecision)
	if len(domains) == 0 {
		return nil, "Whole-life ontology context was skipped because no relevant life domain was selected.", nil
	}
	localRoute := strings.EqualFold(strings.TrimSpace(modelDecision.Tier), llm.TierLocal)
	result, err := s.lifeOntology.SuggestNextContext(taskExecutionContext(request), lifeontology.ContextSuggestionRequest{
		OwnerIdentity:  owner,
		Domains:        domains,
		AsOf:           time.Now().UTC(),
		AllowLocalOnly: localRoute,
		Limit:          lifeOntologyContextLimit,
	})
	if err != nil {
		return nil, "", err
	}
	if !result.AdvisoryOnly || result.CanExecute || result.GrantsAuthority {
		return nil, "", fmt.Errorf("whole-life context provider crossed its advisory-only authority boundary")
	}
	suggestions := filterLifeOntologyContextForRoute(result.Suggestions, localRoute)
	if len(suggestions) == 0 {
		return nil, "No current source-backed whole-life records matched the task domains.", nil
	}
	explanation := fmt.Sprintf(
		"Loaded %d bounded, owner-scoped whole-life records from %d relevant domains; advisory only and grants no execution authority.",
		len(suggestions),
		len(domains),
	)
	return suggestions, explanation, nil
}

func filterLifeOntologyContextForRoute(
	input []lifeontology.ContextSuggestion,
	localRoute bool,
) []lifeontology.ContextSuggestion {
	result := make([]lifeontology.ContextSuggestion, 0, min(len(input), lifeOntologyContextLimit))
	for _, suggestion := range input {
		entity := suggestion.Entity
		if !localRoute && (entity.LocalOnly || entity.Sensitivity == lifeontology.SensitivitySensitive || entity.Sensitivity == lifeontology.SensitivityRestricted) {
			continue
		}
		result = append(result, suggestion)
		if len(result) == lifeOntologyContextLimit {
			break
		}
	}
	return result
}

func lifeOntologyDomains(decision *frameworkregistry.SelectionDecision) []lifeontology.Domain {
	if decision == nil {
		return nil
	}
	seen := make(map[lifeontology.Domain]struct{})
	domains := make([]lifeontology.Domain, 0, len(decision.LifeDomains))
	for _, assignment := range decision.LifeDomains {
		mapped := mapLifeOntologyDomain(assignment.ID)
		if mapped == "" {
			continue
		}
		if _, exists := seen[mapped]; exists {
			continue
		}
		seen[mapped] = struct{}{}
		domains = append(domains, mapped)
	}
	return domains
}

func mapLifeOntologyDomain(value string) lifeontology.Domain {
	switch strings.TrimSpace(value) {
	case "safety_security", "emergency_continuity", "digital_accounts":
		return lifeontology.DomainSafetySecurity
	case "health_wellbeing", "food_nutrition":
		return lifeontology.DomainHealthWellbeing
	case "relationships_care", "family_household", "animals_dependants":
		return lifeontology.DomainRelationships
	case "home_assets", "possessions_inventory", "environment_sustainability":
		return lifeontology.DomainHousingAssets
	case "financial":
		return lifeontology.DomainFinancial
	case "work_venture":
		return lifeontology.DomainWorkVenture
	case "learning_growth", "creativity_expression":
		return lifeontology.DomainLearningGrowth
	case "meaning_values", "legacy_long_term":
		return lifeontology.DomainMeaningValues
	case "community_civic":
		return lifeontology.DomainCommunityCivic
	case "legal_government":
		return lifeontology.DomainLegalGovernment
	case "personal_productivity", "identity_roles", "communication_correspondence", "travel_mobility", "leisure_recreation", "general_operations":
		return lifeontology.DomainPersonalAdmin
	default:
		return ""
	}
}
