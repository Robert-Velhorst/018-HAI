package lifeontology

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"automation-hub-backend/internal/infra"
	"automation-hub-backend/migrations"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestPostgresRepositoryDurabilityIsolationIdempotencyAndImmutability(t *testing.T) {
	repository, db := openLifeOntologyPostgresRepository(t)
	ctx := context.Background()
	owner := "life-ontology-test-" + uuid.NewString()
	now := time.Now().UTC().Truncate(time.Second)
	service := NewService(repository, func() time.Time { return now })

	assetRequest := integrationEntityRequest(owner, EntityAsset, "Primary workstation", now)
	type writeResult struct {
		result EntityWriteResult
		err    error
	}
	results := make(chan writeResult, 2)
	var wait sync.WaitGroup
	for range 2 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			result, err := service.RecordEntity(ctx, assetRequest)
			results <- writeResult{result: result, err: err}
		}()
	}
	wait.Wait()
	close(results)
	alreadyExisted := 0
	var asset Entity
	for result := range results {
		if result.err != nil {
			t.Fatalf("concurrent deterministic append: %v", result.err)
		}
		if result.result.AlreadyExisted {
			alreadyExisted++
		}
		asset = result.result.Entity
	}
	if alreadyExisted != 1 {
		t.Fatalf("duplicate writes marked already-existing %d times, want 1", alreadyExisted)
	}

	restarted := NewPostgresRepository(db)
	storedAsset, err := restarted.GetEntity(ctx, owner, asset.ID)
	if err != nil || storedAsset.EntityDigest != asset.EntityDigest {
		t.Fatalf("durable entity = %#v, err %v", storedAsset, err)
	}
	if _, err := restarted.GetEntity(ctx, "other-owner", asset.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-owner entity lookup error = %v", err)
	}

	personResult, err := service.RecordEntity(ctx, integrationEntityRequest(owner, EntityPerson, "Owner", now))
	if err != nil {
		t.Fatal(err)
	}
	goalResult, err := service.RecordEntity(ctx, integrationEntityRequest(owner, EntityGoal, "Durable objective", now))
	if err != nil {
		t.Fatal(err)
	}
	relationRequest := RecordRelationRequest{
		OwnerIdentity: owner, Type: RelationPursuesGoal,
		FromEntityID: personResult.Entity.ID, ToEntityID: goalResult.Entity.ID,
		Summary: "Source-backed durable relation", ValidFrom: now.Add(-time.Hour),
		ObservedAt: now.Add(-time.Minute), Confidence: 0.9,
		VerificationStatus: VerificationSourceSupported,
		Provenance:         []Provenance{integrationProvenance("relation", now)},
		Sensitivity:        SensitivityInternal,
	}
	relationResult, err := service.RecordRelation(ctx, relationRequest)
	if err != nil {
		t.Fatal(err)
	}
	storedRelation, err := restarted.GetRelation(ctx, owner, relationResult.Relation.ID)
	if err != nil || storedRelation.RelationDigest != relationResult.Relation.RelationDigest {
		t.Fatalf("durable relation = %#v, err %v", storedRelation, err)
	}
	boundedGoals, err := NewService(restarted, func() time.Time { return now }).QueryEntities(ctx, owner, EntityQuery{
		Types: []EntityType{EntityGoal}, AllowLocalOnly: true, Limit: 1,
	})
	if err != nil || len(boundedGoals) != 1 || boundedGoals[0].ID != goalResult.Entity.ID {
		t.Fatalf("bounded entity query = %#v, err %v", boundedGoals, err)
	}
	boundedRelations, err := NewService(restarted, func() time.Time { return now }).QueryRelations(ctx, owner, RelationQuery{
		Types: []RelationType{RelationPursuesGoal}, FromEntityID: personResult.Entity.ID,
		AllowLocalOnly: true, Limit: 1,
	})
	if err != nil || len(boundedRelations) != 1 || boundedRelations[0].ID != relationResult.Relation.ID {
		t.Fatalf("bounded relation query = %#v, err %v", boundedRelations, err)
	}

	firstCandidate := integrationEntityRequest(owner, EntityProject, "HAI source A", now)
	firstCandidate.ExternalKeys = []ExternalKey{{Namespace: "trello/card", Value: "card-" + uuid.NewString()}}
	if _, err := service.RecordEntity(ctx, firstCandidate); err != nil {
		t.Fatal(err)
	}
	secondCandidate := firstCandidate
	secondCandidate.Name = "HAI source B"
	secondCandidate.Summary = "Independent source interpretation"
	secondCandidate.Provenance = []Provenance{integrationProvenance("candidate-b", now)}
	secondResult, err := service.RecordEntity(ctx, secondCandidate)
	if err != nil || len(secondResult.MergeProposals) != 1 {
		t.Fatalf("merge proposal result = %#v, err %v", secondResult, err)
	}
	proposals, err := NewService(restarted, func() time.Time { return now }).ListMergeProposals(ctx, owner, 10)
	if err != nil || len(proposals) != 1 || proposals[0].ID != secondResult.MergeProposals[0].ID {
		t.Fatalf("durable proposals = %#v, err %v", proposals, err)
	}
	keyMatches, err := NewService(restarted, func() time.Time { return now }).QueryEntities(ctx, owner, EntityQuery{
		ExternalKeys: firstCandidate.ExternalKeys, AllowLocalOnly: true, Limit: 1,
	})
	if err != nil || len(keyMatches) != 1 || keyMatches[0].ExternalKeys[0] != firstCandidate.ExternalKeys[0] {
		t.Fatalf("bounded external-key query = %#v, err %v", keyMatches, err)
	}

	if err := db.Exec(`
		UPDATE public.life_ontology_entities
		SET priority = priority + 1
		WHERE owner_identity = ? AND entity_id = ?`, owner, asset.ID).Error; err == nil ||
		!strings.Contains(err.Error(), "append-only") {
		t.Fatalf("immutable update error = %v", err)
	}
	if err := db.Exec(`
		DELETE FROM public.life_ontology_relations
		WHERE owner_identity = ? AND relation_id = ?`, owner, relationResult.Relation.ID).Error; err == nil ||
		!strings.Contains(err.Error(), "append-only") {
		t.Fatalf("immutable delete error = %v", err)
	}
	if err := db.Exec(`TRUNCATE TABLE public.life_ontology_merge_proposals`).Error; err == nil ||
		(!strings.Contains(err.Error(), "append-only") && !strings.Contains(err.Error(), "referenced in a foreign key constraint")) {
		t.Fatalf("truncate guard error = %v", err)
	}
}

func TestPostgresRepositoryRejectsCrossOwnerRelationAtDatabaseBoundary(t *testing.T) {
	repository, db := openLifeOntologyPostgresRepository(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	ownerA := "life-ontology-owner-a-" + uuid.NewString()
	ownerB := "life-ontology-owner-b-" + uuid.NewString()
	serviceA := NewService(repository, func() time.Time { return now })
	serviceB := NewService(NewPostgresRepository(db), func() time.Time { return now })
	person, err := serviceA.RecordEntity(ctx, integrationEntityRequest(ownerA, EntityPerson, "Owner A", now))
	if err != nil {
		t.Fatal(err)
	}
	goal, err := serviceB.RecordEntity(ctx, integrationEntityRequest(ownerB, EntityGoal, "Owner B goal", now))
	if err != nil {
		t.Fatal(err)
	}

	relation, err := normalizeRelationRequest(RecordRelationRequest{
		OwnerIdentity: ownerA, Type: RelationPursuesGoal,
		FromEntityID: person.Entity.ID, ToEntityID: goal.Entity.ID,
		ValidFrom: now.Add(-time.Hour), ObservedAt: now.Add(-time.Minute),
		Confidence: 0.8, VerificationStatus: VerificationSourceSupported,
		Provenance:  []Provenance{integrationProvenance("cross-owner", now)},
		Sensitivity: SensitivityInternal,
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.AppendRelation(ctx, relation); err == nil ||
		!strings.Contains(strings.ToLower(err.Error()), "foreign key") {
		t.Fatalf("cross-owner endpoint error = %v", err)
	}
}

func TestPostgresContactReviewDecisionIsAtomicDurableAndImmutable(t *testing.T) {
	repository, db := openLifeOntologyPostgresRepository(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	owner := "contact-review-owner-" + uuid.NewString()
	service := NewService(repository, func() time.Time { return now })

	firstRequest := integrationEntityRequest(owner, EntityPerson, "Confirmed contact", now)
	firstRequest.Domain = DomainRelationships
	firstRequest.Attributes = map[string]string{"candidate": "true", "review_required": "true"}
	firstRequest.ExternalKeys = []ExternalKey{{Namespace: "source/contact-candidate", Value: "shared-" + uuid.NewString()}}
	firstRequest.Confidence = 0.35
	firstRequest.VerificationStatus = VerificationNeedsReview
	firstRequest.Sensitivity = SensitivitySensitive
	firstRequest.LocalOnly = true
	firstResult, err := service.RecordEntity(ctx, firstRequest)
	if err != nil {
		t.Fatal(err)
	}
	secondRequest := firstRequest
	secondRequest.Summary = "Independent source candidate"
	secondRequest.Provenance = []Provenance{integrationProvenance("contact-review-second", now)}
	secondResult, err := service.RecordEntity(ctx, secondRequest)
	if err != nil || len(secondResult.MergeProposals) != 1 {
		t.Fatalf("contact merge proposal = %#v err=%v", secondResult, err)
	}

	decision, err := service.DecideContactMerge(ctx, DecideContactMergeRequest{
		OwnerIdentity: owner, ProposalID: secondResult.MergeProposals[0].ID,
		Action: ContactReviewMerge, CanonicalName: "Confirmed contact",
		CanonicalSummary: "Human-approved merged identity",
		Reason:           "Authenticated owner confirmed both source records",
		IdempotencyKey:   "postgres-contact-review-" + uuid.NewString(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if decision.CanonicalEntity == nil || decision.CanonicalEntity.VerificationStatus != VerificationHumanApproved {
		t.Fatalf("canonical contact = %#v", decision)
	}
	restarted := NewService(NewPostgresRepository(db), func() time.Time { return now })
	history, err := restarted.ListContactReviewDecisions(ctx, owner, 10)
	if err != nil || len(history) != 1 || history[0].ID != decision.Decision.ID {
		t.Fatalf("durable contact decisions = %#v err=%v", history, err)
	}
	canonical, err := restarted.GetEntity(ctx, owner, decision.Decision.CanonicalEntityID)
	if err != nil || canonical.ID != decision.Decision.CanonicalEntityID {
		t.Fatalf("durable canonical contact = %#v err=%v", canonical, err)
	}
	if other, err := restarted.ListContactReviewDecisions(ctx, owner+"-other", 10); err != nil || len(other) != 0 {
		t.Fatalf("cross-owner contact decisions = %#v err=%v", other, err)
	}
	if err := db.Exec(`UPDATE public.life_ontology_contact_review_decisions SET action = 'reject' WHERE owner_identity = ? AND decision_id = ?`, owner, decision.Decision.ID).Error; err == nil || !strings.Contains(err.Error(), "append-only") {
		t.Fatalf("immutable contact decision update error = %v", err)
	}
	if err := db.Exec(`TRUNCATE TABLE public.life_ontology_contact_review_decisions`).Error; err == nil || !strings.Contains(err.Error(), "append-only") {
		t.Fatalf("immutable contact decision truncate error = %v", err)
	}
	_ = firstResult
}

func TestPostgresContactReviewAllowsOnlyOneConcurrentFinalDecision(t *testing.T) {
	repository, db := openLifeOntologyPostgresRepository(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	owner := "contact-review-race-" + uuid.NewString()
	service := NewService(repository, func() time.Time { return now })
	request := integrationEntityRequest(owner, EntityPerson, "Race candidate", now)
	request.Domain = DomainRelationships
	request.Attributes = map[string]string{"candidate": "true", "review_required": "true"}
	request.ExternalKeys = []ExternalKey{{Namespace: "source/contact-candidate", Value: uuid.NewString()}}
	request.Confidence = 0.35
	request.VerificationStatus = VerificationNeedsReview
	request.Sensitivity = SensitivitySensitive
	request.LocalOnly = true
	candidate, err := service.RecordEntity(ctx, request)
	if err != nil {
		t.Fatal(err)
	}

	services := []*Service{
		NewService(NewPostgresRepository(db), func() time.Time { return now }),
		NewService(NewPostgresRepository(db), func() time.Time { return now }),
	}
	requests := []DecideContactCandidateRequest{
		{OwnerIdentity: owner, CandidateID: candidate.Entity.ID, Action: ContactReviewPromote, Reason: "Promote after owner review", IdempotencyKey: "race-promote-" + uuid.NewString()},
		{OwnerIdentity: owner, CandidateID: candidate.Entity.ID, Action: ContactReviewReject, Reason: "Reject after owner review", IdempotencyKey: "race-reject-" + uuid.NewString()},
	}
	results := make(chan error, len(services))
	var wg sync.WaitGroup
	for index := range services {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			_, decisionErr := services[index].DecideContactCandidate(ctx, requests[index])
			results <- decisionErr
		}(index)
	}
	wg.Wait()
	close(results)
	successes, conflicts := 0, 0
	for resultErr := range results {
		if resultErr == nil {
			successes++
		} else if errors.Is(resultErr, ErrContactReviewConflict) {
			conflicts++
		} else {
			t.Fatalf("unexpected concurrent decision error: %v", resultErr)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("concurrent decisions successes=%d conflicts=%d", successes, conflicts)
	}
	history, err := service.ListContactReviewDecisions(ctx, owner, 10)
	if err != nil || len(history) != 1 {
		t.Fatalf("concurrent decision history = %#v err=%v", history, err)
	}
	var canonicalCount int64
	if err := db.Raw(`
		SELECT COUNT(*) FROM public.life_ontology_entities
		WHERE owner_identity = ? AND payload #>> '{attributes,canonical}' = 'true'`, owner).Scan(&canonicalCount).Error; err != nil {
		t.Fatal(err)
	}
	wantCanonical := int64(0)
	if history[0].Action == ContactReviewPromote {
		wantCanonical = 1
	}
	if canonicalCount != wantCanonical {
		t.Fatalf("orphan canonical contacts=%d want=%d winningAction=%s", canonicalCount, wantCanonical, history[0].Action)
	}
}

func TestPostgresOperationalProjectionPersistsSparseLinkedRecords(t *testing.T) {
	repository, _ := openLifeOntologyPostgresRepository(t)
	ctx := context.Background()
	now := time.Now().UTC()
	owner := "life-ontology-projection-" + uuid.NewString()
	service := NewService(repository, func() time.Time { return now.Add(time.Second) })

	result, err := service.ProjectOperationalRecord(ctx, OperationalProjectionRequest{
		OwnerIdentity:      owner,
		Type:               EntityTask,
		RecordID:           "task-" + uuid.NewString(),
		Domain:             DomainWorkVenture,
		Name:               "Verify operational projection",
		Summary:            "Postgres projection fixture",
		Status:             StatusActive,
		Priority:           50,
		ObservedAt:         now,
		Confidence:         0.9,
		VerificationStatus: VerificationSchemaValidated,
		Provenance:         []Provenance{integrationProvenance("operational-projection", now.Add(2*time.Hour))},
		Sensitivity:        SensitivityInternal,
		LocalOnly:          true,
		Links: []OperationalLinkRequest{{
			Type: EntityProject, RecordID: "018-HAI", Name: "Project 018-HAI",
			Relation: RelationBelongsToProject,
		}},
	})
	if err != nil {
		t.Fatalf("project task into durable operational graph: %v", err)
	}
	if result.Primary.Type != EntityTask || len(result.LinkedEntities) != 1 ||
		result.LinkedEntities[0].Type != EntityProject || len(result.Relations) != 1 ||
		result.Relations[0].Type != RelationBelongsToProject {
		t.Fatalf("operational projection = %#v", result)
	}
	if !result.AdvisoryOnly || result.CanExecute || result.GrantsAuthority {
		t.Fatalf("operational projection crossed authority boundary: %#v", result)
	}
}

func openLifeOntologyPostgresRepository(t *testing.T) (*PostgresRepository, *gorm.DB) {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("HAI_TEST_DATABASE_DSN"))
	if dsn == "" {
		t.Skip("HAI_TEST_DATABASE_DSN not set; skipping life ontology Postgres integration test")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open Postgres: %v", err)
	}
	if _, err := infra.ApplyMigrations(db, migrations.Files, "pre"); err != nil {
		t.Fatalf("apply pre migrations: %v", err)
	}
	return NewPostgresRepository(db), db
}

func integrationEntityRequest(owner string, kind EntityType, name string, now time.Time) RecordEntityRequest {
	return RecordEntityRequest{
		OwnerIdentity: owner, Type: kind, Domain: DomainPersonalAdmin,
		Name: name, Summary: "Postgres persistence fixture", Status: StatusActive,
		Priority: 50, ValidFrom: now.Add(-24 * time.Hour), ObservedAt: now.Add(-time.Hour),
		Confidence: 0.9, VerificationStatus: VerificationSourceSupported,
		Provenance:  []Provenance{integrationProvenance(name, now)},
		Sensitivity: SensitivityInternal,
	}
}

func integrationProvenance(id string, now time.Time) Provenance {
	return Provenance{
		ReferenceID: id, URI: "https://example.test/" + id,
		ContentDigest: strings.Repeat("a", 64), Authority: "integration fixture",
		CapturedAt: now.Add(-2 * time.Hour),
	}
}
