package task

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/frameworkregistry"
	"automation-hub-backend/internal/lifeontology"
	"automation-hub-backend/internal/llm"
)

type capturingLifeOntologyProvider struct {
	request lifeontology.ContextSuggestionRequest
	result  lifeontology.ContextSuggestionResult
	err     error
}

type cancellationAwareLifeOntologyProvider struct{}

func (cancellationAwareLifeOntologyProvider) SuggestNextContext(ctx context.Context, _ lifeontology.ContextSuggestionRequest) (lifeontology.ContextSuggestionResult, error) {
	return lifeontology.ContextSuggestionResult{}, ctx.Err()
}

func (p *capturingLifeOntologyProvider) SuggestNextContext(
	_ context.Context,
	request lifeontology.ContextSuggestionRequest,
) (lifeontology.ContextSuggestionResult, error) {
	p.request = request
	return p.result, p.err
}

func TestLifeOntologyContextIsBoundedDomainScopedAndLocalAware(t *testing.T) {
	provider := &capturingLifeOntologyProvider{result: lifeontology.ContextSuggestionResult{
		Suggestions:  []lifeontology.ContextSuggestion{{Entity: lifeontology.Entity{ID: "life-entity-1", Name: "Evidence bundle"}}},
		AdvisoryOnly: true,
	}}
	svc := &service{lifeOntology: provider}
	decision := &frameworkregistry.SelectionDecision{LifeDomains: []frameworkregistry.LifeDomainAssignment{
		{ID: "legal_government"},
		{ID: "home_assets"},
		{ID: "home_assets"},
		{ID: "unknown"},
	}}

	got, explanation, err := svc.retrieveLifeOntologyContext(
		IntakeRequest{OwnerIdentity: "owner-1"},
		decision,
		llm.RouteDecision{Tier: llm.TierLocal},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || provider.request.OwnerIdentity != "owner-1" {
		t.Fatalf("unexpected owner-scoped suggestions: %#v / %#v", got, provider.request)
	}
	if provider.request.Limit != lifeOntologyContextLimit || !provider.request.AllowLocalOnly {
		t.Fatalf("context boundary not applied: %#v", provider.request)
	}
	if len(provider.request.Domains) != 2 || provider.request.Domains[0] != lifeontology.DomainLegalGovernment || provider.request.Domains[1] != lifeontology.DomainHousingAssets {
		t.Fatalf("domains = %#v", provider.request.Domains)
	}
	if !strings.Contains(explanation, "advisory only") {
		t.Fatalf("explanation = %q", explanation)
	}
	if provider.request.AsOf.IsZero() || time.Since(provider.request.AsOf) > time.Minute {
		t.Fatalf("as-of boundary missing: %v", provider.request.AsOf)
	}
}

func TestLifeOntologyContextRejectsAuthorityEscalation(t *testing.T) {
	provider := &capturingLifeOntologyProvider{result: lifeontology.ContextSuggestionResult{
		AdvisoryOnly:    true,
		CanExecute:      true,
		GrantsAuthority: true,
	}}
	svc := &service{lifeOntology: provider}
	_, _, err := svc.retrieveLifeOntologyContext(
		IntakeRequest{OwnerIdentity: "owner-1"},
		&frameworkregistry.SelectionDecision{LifeDomains: []frameworkregistry.LifeDomainAssignment{{ID: "financial"}}},
		llm.RouteDecision{Tier: llm.TierFree},
	)
	if err == nil || !strings.Contains(err.Error(), "authority boundary") {
		t.Fatalf("err = %v", err)
	}
	if provider.request.AllowLocalOnly {
		t.Fatal("cloud route must not receive local-only whole-life context")
	}
}

func TestLifeOntologyContextKeepsSensitiveRecordsOffCloudRoutes(t *testing.T) {
	provider := &capturingLifeOntologyProvider{result: lifeontology.ContextSuggestionResult{
		Suggestions: []lifeontology.ContextSuggestion{
			{Entity: lifeontology.Entity{ID: "public", Name: "Public", Sensitivity: lifeontology.SensitivityPublic}},
			{Entity: lifeontology.Entity{ID: "sensitive", Name: "Sensitive", Sensitivity: lifeontology.SensitivitySensitive}},
			{Entity: lifeontology.Entity{ID: "restricted", Name: "Restricted", Sensitivity: lifeontology.SensitivityRestricted}},
			{Entity: lifeontology.Entity{ID: "local", Name: "Local", Sensitivity: lifeontology.SensitivityInternal, LocalOnly: true}},
		},
		AdvisoryOnly: true,
	}}
	svc := &service{lifeOntology: provider}
	decision := &frameworkregistry.SelectionDecision{LifeDomains: []frameworkregistry.LifeDomainAssignment{{ID: "financial"}}}

	got, _, err := svc.retrieveLifeOntologyContext(
		IntakeRequest{OwnerIdentity: "owner-1"},
		decision,
		llm.RouteDecision{Tier: llm.TierFree},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Entity.ID != "public" {
		t.Fatalf("cloud-safe context = %#v", got)
	}
	if provider.request.AllowLocalOnly {
		t.Fatal("cloud route requested local-only records")
	}
}

func TestLifeOntologyContextDoesNotQueryEntireGraphWithoutRelevantDomain(t *testing.T) {
	provider := &capturingLifeOntologyProvider{result: lifeontology.ContextSuggestionResult{AdvisoryOnly: true}}
	svc := &service{lifeOntology: provider}
	got, explanation, err := svc.retrieveLifeOntologyContext(
		IntakeRequest{OwnerIdentity: "owner-1"},
		&frameworkregistry.SelectionDecision{},
		llm.RouteDecision{Tier: llm.TierLocal},
	)
	if err != nil || len(got) != 0 || !strings.Contains(explanation, "no relevant life domain") {
		t.Fatalf("got=%#v explanation=%q err=%v", got, explanation, err)
	}
	if !provider.request.AsOf.IsZero() {
		t.Fatal("provider should not be called without a bounded domain")
	}
}

func TestWithLifeOntologyContextRequiresBuiltInService(t *testing.T) {
	provider := &capturingLifeOntologyProvider{}
	if _, err := WithLifeOntologyContext(nil, provider); err == nil {
		t.Fatal("expected built-in service requirement")
	}
	if _, err := WithLifeOntologyContext(&service{}, nil); err == nil {
		t.Fatal("expected provider requirement")
	}
}

func TestLifeOntologyContextPropagatesTaskCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	svc := &service{lifeOntology: cancellationAwareLifeOntologyProvider{}}

	_, _, err := svc.retrieveLifeOntologyContext(
		IntakeRequest{OwnerIdentity: "owner-1", executionContext: ctx},
		&frameworkregistry.SelectionDecision{LifeDomains: []frameworkregistry.LifeDomainAssignment{{ID: "financial"}}},
		llm.RouteDecision{Tier: llm.TierLocal},
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("retrieve life ontology context error = %v, want context canceled", err)
	}
}

func TestLifeOntologyContextBecomesTraceableVerificationEvidence(t *testing.T) {
	observedAt := time.Now().UTC().Add(-time.Hour)
	plan := &CompletionPlan{ContextPlan: ContextPlan{LifeContext: []lifeontology.ContextSuggestion{{
		Entity: lifeontology.Entity{
			ID:         "life-entity-1",
			Type:       lifeontology.EntityObligation,
			Name:       "Reply to the source-backed request",
			ObservedAt: observedAt,
			Confidence: 0.9,
			Provenance: []lifeontology.Provenance{{
				ReferenceID: "source-42",
				URI:         "local://source/42",
				Authority:   "owner_record",
				CapturedAt:  observedAt,
			}},
		},
		Score: 82,
	}}}}

	evidence := evidenceFromPlan(plan)
	if len(evidence) != 1 || evidence[0].SourceType != "life_ontology" || evidence[0].SourceID != "source-42" || evidence[0].SourceURI != "local://source/42" {
		t.Fatalf("evidence = %#v", evidence)
	}
	context := generationContext(plan)
	if len(context) != 1 || context[0] != "Reply to the source-backed request" {
		t.Fatalf("generation context = %#v", context)
	}
	confidence := strings.Join(taskConfidenceEvidence(plan), " ")
	if !strings.Contains(confidence, "life-context-confidence:0.90") || !strings.Contains(confidence, "life-context-score:82") {
		t.Fatalf("confidence evidence = %q", confidence)
	}
}
