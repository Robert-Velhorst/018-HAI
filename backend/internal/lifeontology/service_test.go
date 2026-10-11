package lifeontology

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestRecordsEveryWholeLifeEntityTypeWithDeterministicIdentity(t *testing.T) {
	types := []EntityType{
		EntityPerson, EntityNeed, EntityGoal, EntityAsset, EntityObligation, EntityProject, EntityCase,
		EntityOpportunity, EntityRisk, EntitySource, EntityDocument, EntityPursuit, EntityWorkflow,
		EntityTask, EntityMemory, EntityCommitment, EntityCost, EntityOutcome,
	}
	for _, kind := range types {
		t.Run(string(kind), func(t *testing.T) {
			repo := NewMemoryRepository()
			service := NewService(repo, func() time.Time { return fixedNow() })
			request := entityRequest(kind, string(kind))
			request.ExternalKeys = []ExternalKey{{Namespace: "github/issue", Value: "18"}, {Namespace: "trello/card", Value: "abc"}}
			request.Provenance = []Provenance{source("b"), source("a")}
			first, err := service.RecordEntity(context.Background(), request)
			if err != nil {
				t.Fatalf("record entity: %v", err)
			}
			request.ExternalKeys[0], request.ExternalKeys[1] = request.ExternalKeys[1], request.ExternalKeys[0]
			request.Provenance[0], request.Provenance[1] = request.Provenance[1], request.Provenance[0]
			second, err := service.RecordEntity(context.Background(), request)
			if err != nil {
				t.Fatalf("record duplicate: %v", err)
			}
			if !second.AlreadyExisted || first.Entity.ID != second.Entity.ID || first.Entity.EntityDigest != second.Entity.EntityDigest {
				t.Fatalf("deterministic idempotency failed: %#v %#v", first, second)
			}
			if len(first.Entity.EntityDigest) != 64 || len(first.Entity.ProvenanceDigest) != 64 {
				t.Fatalf("canonical hashes missing: %#v", first.Entity)
			}
		})
	}
}

func TestConflictingStableExternalKeyCreatesProposalWithoutOverwrite(t *testing.T) {
	service := NewService(nil, func() time.Time { return fixedNow() })
	firstRequest := entityRequest(EntityProject, "HAI original")
	firstRequest.ExternalKeys = []ExternalKey{{Namespace: "trello/card", Value: "card-1"}}
	first, err := service.RecordEntity(context.Background(), firstRequest)
	if err != nil {
		t.Fatal(err)
	}
	secondRequest := entityRequest(EntityProject, "HAI revised candidate")
	secondRequest.ExternalKeys = []ExternalKey{{Namespace: "TRELLO/CARD", Value: "card-1"}}
	secondRequest.Summary = "Conflicting source record"
	second, err := service.RecordEntity(context.Background(), secondRequest)
	if err != nil {
		t.Fatal(err)
	}
	if first.Entity.ID == second.Entity.ID || len(second.MergeProposals) != 1 {
		t.Fatalf("expected separate records and one proposal: %#v", second)
	}
	proposal := second.MergeProposals[0]
	if proposal.Match != MergeExternalKey || !proposal.AdvisoryOnly || proposal.CanExecute || proposal.GrantsAuthority || proposal.Status != "proposed" {
		t.Fatalf("proposal crossed authority boundary: %#v", proposal)
	}
	entities, err := service.QueryEntities(context.Background(), "owner-1", EntityQuery{AllowLocalOnly: true})
	if err != nil || len(entities) != 2 {
		t.Fatalf("candidate was overwritten: count=%d err=%v", len(entities), err)
	}
	stored, err := service.ListMergeProposals(context.Background(), "owner-1", 10)
	if err != nil || len(stored) != 1 || stored[0].ID != proposal.ID {
		t.Fatalf("proposal not durable: %#v %v", stored, err)
	}
}

func TestSemanticDuplicateProducesReviewProposal(t *testing.T) {
	service := NewService(nil, func() time.Time { return fixedNow() })
	first := entityRequest(EntityGoal, "Improve Health")
	first.Summary = "First interpretation"
	if _, err := service.RecordEntity(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	second := entityRequest(EntityGoal, "  improve   health ")
	second.Summary = "Second interpretation"
	result, err := service.RecordEntity(context.Background(), second)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.MergeProposals) != 1 || result.MergeProposals[0].Match != MergeSemanticIdentity || result.MergeProposals[0].Confidence != 0.85 {
		t.Fatalf("semantic proposal missing: %#v", result)
	}
}

func TestMergeProposalTimestampUsesPostgresPrecision(t *testing.T) {
	now := time.Date(2026, time.August, 4, 12, 0, 0, 123456789, time.UTC)
	proposal, err := buildMergeProposal(
		"owner-1",
		"life-entity-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"life-entity-bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		MergeSemanticIdentity,
		[]string{"same normalized identity"},
		0.85,
		now,
	)
	if err != nil {
		t.Fatal(err)
	}
	want := now.Round(time.Microsecond)
	if !proposal.CreatedAt.Equal(want) || proposal.CreatedAt.Nanosecond()%1000 != 0 {
		t.Fatalf("merge timestamp = %s, want PostgreSQL precision %s", proposal.CreatedAt, want)
	}
	if err := validateMergeProposal(proposal); err != nil {
		t.Fatalf("canonical proposal rejected: %v", err)
	}
}

func TestTypedSourceBackedRelationAndOwnerIsolation(t *testing.T) {
	service := NewService(nil, func() time.Time { return fixedNow() })
	person := mustEntity(t, service, entityRequest(EntityPerson, "Robert"))
	needRequest := entityRequest(EntityNeed, "Stable housing")
	needRequest.LocalOnly = true
	needRequest.Sensitivity = SensitivityRestricted
	need := mustEntity(t, service, needRequest)
	relation, err := service.RecordRelation(context.Background(), RecordRelationRequest{OwnerIdentity: "owner-1", Type: RelationHasNeed, FromEntityID: person.ID, ToEntityID: need.ID, ValidFrom: fixedNow().Add(-time.Hour), ObservedAt: fixedNow().Add(-time.Minute), Confidence: 0.9, VerificationStatus: VerificationSourceSupported, Provenance: []Provenance{source("relation")}})
	if err != nil {
		t.Fatalf("record relation: %v", err)
	}
	if !relation.Relation.LocalOnly || relation.Relation.Sensitivity != SensitivityRestricted {
		t.Fatalf("endpoint privacy was not inherited: %#v", relation.Relation)
	}
	if _, err := service.RecordRelation(context.Background(), RecordRelationRequest{OwnerIdentity: "owner-1", Type: RelationOwnsAsset, FromEntityID: person.ID, ToEntityID: need.ID, ValidFrom: fixedNow().Add(-time.Hour), ObservedAt: fixedNow().Add(-time.Minute), Confidence: 1, Provenance: []Provenance{source("wrong")}}); err == nil || !strings.Contains(err.Error(), "does not allow") {
		t.Fatalf("typed endpoint violation accepted: %v", err)
	}
	if _, err := service.GetEntity(context.Background(), "other-owner", person.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("owner isolation failed: %v", err)
	}
	if _, err := service.RecordRelation(context.Background(), RecordRelationRequest{OwnerIdentity: "other-owner", Type: RelationHasNeed, FromEntityID: person.ID, ToEntityID: need.ID}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-owner relation should fail closed: %v", err)
	}
	publicAssetRequest := entityRequest(EntityAsset, "Public asset")
	publicAssetRequest.Sensitivity = SensitivityPublic
	publicAsset := mustEntity(t, service, publicAssetRequest)
	publicPersonRequest := entityRequest(EntityPerson, "Public person")
	publicPersonRequest.Sensitivity = SensitivityPublic
	publicPerson := mustEntity(t, service, publicPersonRequest)
	publicRelation := relationRequest(RelationOwnsAsset, publicPerson.ID, publicAsset.ID)
	publicRelation.Sensitivity = SensitivityPublic
	publicResult, err := service.RecordRelation(context.Background(), publicRelation)
	if err != nil || publicResult.Relation.Sensitivity != SensitivityPublic {
		t.Fatalf("public endpoint sensitivity should stay public: %#v %v", publicResult, err)
	}
}

func TestQueriesFilterDomainTypeRelationTemporalAndLocalOnly(t *testing.T) {
	service := NewService(nil, func() time.Time { return fixedNow() })
	person := mustEntity(t, service, entityRequest(EntityPerson, "Robert"))
	goalRequest := entityRequest(EntityGoal, "Ship HAI")
	goalRequest.Domain = DomainWorkVenture
	goalRequest.ValidUntil = timePtr(fixedNow().Add(time.Hour))
	goal := mustEntity(t, service, goalRequest)
	privateRequest := entityRequest(EntityGoal, "Private goal")
	privateRequest.Domain = DomainWorkVenture
	privateRequest.LocalOnly = true
	mustEntity(t, service, privateRequest)
	_, err := service.RecordRelation(context.Background(), relationRequest(RelationPursuesGoal, person.ID, goal.ID))
	if err != nil {
		t.Fatal(err)
	}
	entities, err := service.QueryEntities(context.Background(), "owner-1", EntityQuery{Domains: []Domain{DomainWorkVenture}, Types: []EntityType{EntityGoal}, AsOf: timePtr(fixedNow()), Limit: 10})
	if err != nil || len(entities) != 1 || entities[0].ID != goal.ID {
		t.Fatalf("entity filters failed: %#v %v", entities, err)
	}
	after := fixedNow().Add(2 * time.Hour)
	entities, err = service.QueryEntities(context.Background(), "owner-1", EntityQuery{Domains: []Domain{DomainWorkVenture}, AsOf: &after, AllowLocalOnly: true})
	if err != nil || len(entities) != 1 || entities[0].Name != "Private goal" {
		t.Fatalf("temporal filter failed: %#v %v", entities, err)
	}
	relations, err := service.QueryRelations(context.Background(), "owner-1", RelationQuery{Types: []RelationType{RelationPursuesGoal}, FromEntityID: person.ID, AsOf: timePtr(fixedNow())})
	if err != nil || len(relations) != 1 {
		t.Fatalf("relation query failed: %#v %v", relations, err)
	}
}

func TestServiceUsesBoundedRepositoryQueries(t *testing.T) {
	repo := &boundedQueryRepositorySpy{MemoryRepository: NewMemoryRepository()}
	service := NewService(repo, func() time.Time { return fixedNow() })

	if _, err := service.QueryEntities(context.Background(), "owner-1", EntityQuery{
		ExternalKeys: []ExternalKey{{Namespace: " TRELLO/CARD ", Value: " card-1 "}},
	}); err != nil {
		t.Fatal(err)
	}
	if repo.entityCalls != 1 || repo.entityQuery.Limit != defaultLimit ||
		len(repo.entityQuery.ExternalKeys) != 1 ||
		repo.entityQuery.ExternalKeys[0] != (ExternalKey{Namespace: "trello/card", Value: "card-1"}) {
		t.Fatalf("bounded entity query = calls:%d query:%#v", repo.entityCalls, repo.entityQuery)
	}

	if _, err := service.QueryRelations(context.Background(), "owner-1", RelationQuery{Limit: 7}); err != nil {
		t.Fatal(err)
	}
	if repo.relationCalls != 1 || repo.relationQuery.Limit != 7 {
		t.Fatalf("bounded relation query = calls:%d query:%#v", repo.relationCalls, repo.relationQuery)
	}

	if _, err := service.ListMergeProposals(context.Background(), "owner-1", 9); err != nil {
		t.Fatal(err)
	}
	if repo.proposalCalls != 1 || repo.proposalLimit != 9 {
		t.Fatalf("bounded merge proposal query = calls:%d limit:%d", repo.proposalCalls, repo.proposalLimit)
	}
}

func TestSuggestionsAreExplainableDeterministicAndAuthorityFree(t *testing.T) {
	service := NewService(nil, func() time.Time { return fixedNow() })
	person := mustEntity(t, service, entityRequest(EntityPerson, "Robert"))
	riskRequest := entityRequest(EntityRisk, "Missed legal deadline")
	riskRequest.Domain = DomainLegalGovernment
	riskRequest.Priority = 100
	riskRequest.DueAt = timePtr(fixedNow().Add(-time.Hour))
	riskRequest.VerificationStatus = VerificationVerified
	risk := mustEntity(t, service, riskRequest)
	goalRequest := entityRequest(EntityGoal, "Write optional article")
	goalRequest.Priority = 10
	goalRequest.VerificationStatus = VerificationUncertain
	goal := mustEntity(t, service, goalRequest)
	if _, err := service.RecordRelation(context.Background(), relationRequest(RelationThreatens, risk.ID, goal.ID)); err != nil {
		t.Fatal(err)
	}
	if _, err := service.RecordRelation(context.Background(), relationRequest(RelationSupports, person.ID, risk.ID)); err != nil {
		t.Fatal(err)
	}
	request := ContextSuggestionRequest{OwnerIdentity: "owner-1", FocusEntityID: person.ID, AsOf: fixedNow(), AllowLocalOnly: true, Limit: 5}
	first, err := service.SuggestNextContext(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.SuggestNextContext(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) || first.DecisionDigest == "" {
		t.Fatalf("suggestions are not deterministic: %#v %#v", first, second)
	}
	if !first.AdvisoryOnly || first.CanExecute || first.GrantsAuthority {
		t.Fatalf("suggestions crossed authority boundary: %#v", first)
	}
	if len(first.Suggestions) < 2 || first.Suggestions[0].Entity.ID != risk.ID || len(first.Suggestions[0].Reasons) < 4 || len(first.Suggestions[0].RelatedRelationIDs) != 1 {
		t.Fatalf("ranking is not explainable: %#v", first.Suggestions)
	}
}

func TestSecretsAreRejectedAndCredentialURLsAreRedacted(t *testing.T) {
	service := NewService(nil, func() time.Time { return fixedNow() })
	request := entityRequest(EntityAsset, "API credential")
	request.Summary = "api_key=super-secret-value"
	if _, err := service.RecordEntity(context.Background(), request); err == nil || !strings.Contains(err.Error(), "secret material") {
		t.Fatalf("secret-bearing content accepted: %v", err)
	}
	request = entityRequest(EntityAsset, "Sanitized source")
	request.Provenance[0].URI = "https://example.test/item?token=super-secret-value&view=1"
	result, err := service.RecordEntity(context.Background(), request)
	if err != nil {
		t.Fatalf("redacted URL should be accepted: %v", err)
	}
	uri := result.Entity.Provenance[0].URI
	if strings.Contains(uri, "super-secret-value") || !strings.Contains(uri, "REDACTED") {
		t.Fatalf("credential URL not redacted: %q", uri)
	}
}

func TestConflictingProvenanceAndBoundsFailClosed(t *testing.T) {
	service := NewService(nil, func() time.Time { return fixedNow() })
	request := entityRequest(EntityCase, "Case")
	request.Provenance = []Provenance{source("same"), source("same")}
	request.Provenance[1].ContentDigest = strings.Repeat("b", 64)
	if _, err := service.RecordEntity(context.Background(), request); err == nil || !strings.Contains(err.Error(), "conflicting content digests") {
		t.Fatalf("conflicting provenance accepted: %v", err)
	}
	request = entityRequest(EntityCase, "Case")
	request.Summary = strings.Repeat("x", 2049)
	if _, err := service.RecordEntity(context.Background(), request); err == nil || !strings.Contains(err.Error(), "bounded") {
		t.Fatalf("oversized entity accepted: %v", err)
	}
	if _, err := service.QueryEntities(context.Background(), "owner-1", EntityQuery{Limit: 101}); err == nil {
		t.Fatal("unbounded query accepted")
	}
	if _, err := service.QueryEntities(context.Background(), "owner-1", EntityQuery{ExternalKeys: []ExternalKey{{Namespace: "", Value: ""}}}); err == nil {
		t.Fatal("empty external-key filter widened the query")
	}
}

func TestCorruptStorageFailsClosedAndReturnedRecordsAreCloned(t *testing.T) {
	repo := NewMemoryRepository()
	service := NewService(repo, func() time.Time { return fixedNow() })
	entity := mustEntity(t, service, entityRequest(EntityAsset, "Laptop"))
	got, err := service.GetEntity(context.Background(), "owner-1", entity.ID)
	if err != nil {
		t.Fatal(err)
	}
	got.Attributes["owner"] = "mutated"
	again, err := service.GetEntity(context.Background(), "owner-1", entity.ID)
	if err != nil {
		t.Fatal(err)
	}
	if again.Attributes["owner"] == "mutated" {
		t.Fatal("caller mutated repository state")
	}
	repo.mu.Lock()
	corrupted := repo.entities[entity.ID]
	corrupted.Name = "tampered"
	repo.entities[entity.ID] = corrupted
	repo.mu.Unlock()
	if _, err := service.GetEntity(context.Background(), "owner-1", entity.ID); !errors.Is(err, ErrCorruptStorage) {
		t.Fatalf("corrupt storage did not fail closed: %v", err)
	}
}

func TestPublicServiceHasNoExecutionOrApprovalMethods(t *testing.T) {
	typeOf := reflect.TypeOf(&Service{})
	for _, forbidden := range []string{"Execute", "Approve", "ApplyMerge", "Send", "RunTool"} {
		if _, exists := typeOf.MethodByName(forbidden); exists {
			t.Fatalf("advisory service exposes forbidden method %s", forbidden)
		}
	}
}

type boundedQueryRepositorySpy struct {
	*MemoryRepository
	entityCalls   int
	entityQuery   EntityQuery
	relationCalls int
	relationQuery RelationQuery
	proposalCalls int
	proposalLimit int
}

func (r *boundedQueryRepositorySpy) QueryEntities(_ context.Context, _ string, query EntityQuery) ([]Entity, error) {
	r.entityCalls++
	r.entityQuery = query
	return []Entity{}, nil
}

func (r *boundedQueryRepositorySpy) QueryRelations(_ context.Context, _ string, query RelationQuery) ([]Relation, error) {
	r.relationCalls++
	r.relationQuery = query
	return []Relation{}, nil
}

func (r *boundedQueryRepositorySpy) ListMergeProposalsWithLimit(_ context.Context, _ string, limit int) ([]MergeProposal, error) {
	r.proposalCalls++
	r.proposalLimit = limit
	return []MergeProposal{}, nil
}

func TestOperationalProjectionCreatesIdempotentCrossEntityGraph(t *testing.T) {
	service := NewService(nil, func() time.Time { return fixedNow() })
	request := OperationalProjectionRequest{
		OwnerIdentity: "owner-1", Type: EntityTask, RecordID: "task-42",
		Domain: DomainWorkVenture, Name: "Verify HAI task", Summary: "Durable task plan",
		Status: StatusActive, Priority: 80, ObservedAt: fixedNow().Add(-time.Minute),
		Confidence: 0.9, VerificationStatus: VerificationSchemaValidated,
		Attributes: map[string]string{"risk": "medium"}, Provenance: []Provenance{source("task-42")},
		Sensitivity: SensitivityInternal,
		Links: []OperationalLinkRequest{
			{Type: EntityProject, RecordID: "018-hai", Name: "018-HAI", Relation: RelationBelongsToProject},
			{Type: EntityPursuit, RecordID: "pursuit-7", Name: "Operational HAI", Relation: RelationBelongsToPursuit},
			{Type: EntityWorkflow, RecordID: "workflow-9", Name: "Verified delivery", Relation: RelationBelongsToWorkflow},
		},
	}
	first, err := service.ProjectOperationalRecord(context.Background(), request)
	if err != nil {
		t.Fatalf("project operational record: %v", err)
	}
	second, err := service.ProjectOperationalRecord(context.Background(), request)
	if err != nil {
		t.Fatalf("repeat projection: %v", err)
	}
	if !second.AlreadyExisted || first.Primary.ID != second.Primary.ID {
		t.Fatalf("primary projection is not idempotent: %#v %#v", first, second)
	}
	if len(first.LinkedEntities) != 3 || len(first.Relations) != 3 || len(second.LinkedEntities) != 3 || len(second.Relations) != 3 {
		t.Fatalf("cross-entity graph is incomplete: %#v %#v", first, second)
	}
	if !first.AdvisoryOnly || first.CanExecute || first.GrantsAuthority {
		t.Fatalf("projection crossed authority boundary: %#v", first)
	}
	entities, err := service.QueryEntities(context.Background(), "owner-1", EntityQuery{AllowLocalOnly: true, Limit: 100})
	if err != nil || len(entities) != 4 {
		t.Fatalf("projection duplicated stable graph entities: count=%d err=%v", len(entities), err)
	}
	relations, err := service.QueryRelations(context.Background(), "owner-1", RelationQuery{AllowLocalOnly: true, Limit: 100})
	if err != nil || len(relations) != 3 {
		t.Fatalf("projection duplicated stable graph relations: count=%d err=%v", len(relations), err)
	}
}

func TestOperationalProjectionValidatesAllLinksBeforeWriting(t *testing.T) {
	service := NewService(nil, func() time.Time { return fixedNow() })
	request := OperationalProjectionRequest{
		OwnerIdentity: "owner-1", Type: EntityTask, RecordID: "task-invalid",
		Domain: DomainPersonalAdmin, Name: "Invalid task link", Status: StatusActive,
		ObservedAt: fixedNow().Add(-time.Minute), Confidence: 0.5,
		VerificationStatus: VerificationNeedsReview, Provenance: []Provenance{source("task-invalid")},
		Sensitivity: SensitivityInternal,
		Links: []OperationalLinkRequest{{
			Type: EntityCost, RecordID: "cost-1", Name: "Cost", Relation: RelationAssignedTo,
		}},
	}
	if _, err := service.ProjectOperationalRecord(context.Background(), request); err == nil || !strings.Contains(err.Error(), "does not allow") {
		t.Fatalf("invalid link was accepted: %v", err)
	}
	entities, err := service.QueryEntities(context.Background(), "owner-1", EntityQuery{AllowLocalOnly: true})
	if err != nil || len(entities) != 0 {
		t.Fatalf("invalid projection wrote partial state: %#v %v", entities, err)
	}
}

func TestOperationalProjectionCanMakeLinkedContactMoreCautiousThanDocument(t *testing.T) {
	service := NewService(nil, func() time.Time { return fixedNow() })
	result, err := service.ProjectOperationalRecord(context.Background(), OperationalProjectionRequest{
		OwnerIdentity: "owner-1", Type: EntityDocument, RecordID: "message-1",
		Domain: DomainRelationships, Name: "Source-backed message", Status: StatusActive,
		ObservedAt: fixedNow().Add(-time.Minute), Confidence: 0.9,
		VerificationStatus: VerificationSourceSupported, Provenance: []Provenance{source("message-1")},
		Sensitivity: SensitivityInternal,
		Links: []OperationalLinkRequest{{
			Type: EntityPerson, RecordID: "contact-candidate-1", Name: "Candidate Person",
			Relation: RelationDocuments, Confidence: 0.35,
			VerificationStatus: VerificationNeedsReview, Sensitivity: SensitivitySensitive,
			LocalOnly: true,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.LinkedEntities) != 1 || len(result.Relations) != 1 {
		t.Fatalf("projection = %#v", result)
	}
	person := result.LinkedEntities[0]
	if person.Type != EntityPerson || person.Confidence != 0.35 ||
		person.VerificationStatus != VerificationNeedsReview ||
		person.Sensitivity != SensitivitySensitive || !person.LocalOnly {
		t.Fatalf("linked contact controls = %#v", person)
	}
	relation := result.Relations[0]
	if relation.Confidence != 0.35 || relation.VerificationStatus != VerificationNeedsReview ||
		relation.Sensitivity != SensitivitySensitive || !relation.LocalOnly {
		t.Fatalf("contact relation controls = %#v", relation)
	}
}

func entityRequest(kind EntityType, name string) RecordEntityRequest {
	return RecordEntityRequest{OwnerIdentity: "owner-1", Type: kind, Domain: DomainPersonalAdmin, Name: name, Summary: "Source-backed context", Attributes: map[string]string{"owner": "Robert"}, Status: StatusActive, Priority: 50, ValidFrom: fixedNow().Add(-24 * time.Hour), ObservedAt: fixedNow().Add(-time.Hour), Confidence: 0.8, VerificationStatus: VerificationSourceSupported, Provenance: []Provenance{source(name)}, Sensitivity: SensitivityInternal}
}

func relationRequest(kind RelationType, fromID, toID string) RecordRelationRequest {
	return RecordRelationRequest{OwnerIdentity: "owner-1", Type: kind, FromEntityID: fromID, ToEntityID: toID, Summary: "Source-backed relation", ValidFrom: fixedNow().Add(-time.Hour), ObservedAt: fixedNow().Add(-time.Minute), Confidence: 0.9, VerificationStatus: VerificationSourceSupported, Provenance: []Provenance{source(string(kind))}}
}

func source(id string) Provenance {
	return Provenance{ReferenceID: id, URI: "https://example.test/" + id, ContentDigest: strings.Repeat("a", 64), Authority: "primary record", CapturedAt: fixedNow().Add(-2 * time.Hour)}
}
func fixedNow() time.Time                { return time.Date(2026, time.July, 31, 12, 0, 0, 0, time.UTC) }
func timePtr(value time.Time) *time.Time { return &value }
func mustEntity(t *testing.T, service *Service, request RecordEntityRequest) Entity {
	t.Helper()
	result, err := service.RecordEntity(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	return result.Entity
}
