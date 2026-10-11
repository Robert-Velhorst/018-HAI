package lifeontology

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

type Clock func() time.Time

type Service struct {
	repo  Repository
	clock Clock
}

func NewService(repo Repository, clock Clock) *Service {
	if repo == nil {
		repo = NewMemoryRepository()
	}
	if clock == nil {
		clock = time.Now
	}
	return &Service{repo: repo, clock: clock}
}

func (s *Service) RecordEntity(ctx context.Context, request RecordEntityRequest) (EntityWriteResult, error) {
	if s == nil || s.repo == nil || s.clock == nil {
		return EntityWriteResult{}, fmt.Errorf("life ontology service is unavailable")
	}
	entity, err := normalizeEntityRequest(request, s.clock().UTC())
	if err != nil {
		return EntityWriteResult{}, err
	}
	created, err := s.repo.AppendEntity(ctx, entity)
	if errors.Is(err, ErrExists) {
		existing, getErr := s.repo.GetEntity(ctx, entity.OwnerIdentity, entity.ID)
		if getErr != nil {
			return EntityWriteResult{}, getErr
		}
		return EntityWriteResult{Entity: existing, AlreadyExisted: true}, nil
	}
	if err != nil {
		return EntityWriteResult{}, err
	}
	proposals, err := s.proposeMerges(ctx, created)
	if err != nil {
		return EntityWriteResult{}, err
	}
	return EntityWriteResult{Entity: created, MergeProposals: proposals}, nil
}

func (s *Service) RecordRelation(ctx context.Context, request RecordRelationRequest) (RelationWriteResult, error) {
	if s == nil || s.repo == nil || s.clock == nil {
		return RelationWriteResult{}, fmt.Errorf("life ontology service is unavailable")
	}
	if err := validateOwner(compact(request.OwnerIdentity)); err != nil {
		return RelationWriteResult{}, err
	}
	from, err := s.repo.GetEntity(ctx, compact(request.OwnerIdentity), compact(request.FromEntityID))
	if err != nil {
		return RelationWriteResult{}, fmt.Errorf("from entity: %w", err)
	}
	to, err := s.repo.GetEntity(ctx, compact(request.OwnerIdentity), compact(request.ToEntityID))
	if err != nil {
		return RelationWriteResult{}, fmt.Errorf("to entity: %w", err)
	}
	if err := validateRelationEndpoints(request.Type, from.Type, to.Type); err != nil {
		return RelationWriteResult{}, err
	}
	request.LocalOnly = request.LocalOnly || from.LocalOnly || to.LocalOnly
	request.Sensitivity = strongestSensitivity(request.Sensitivity, from.Sensitivity, to.Sensitivity)
	relation, err := normalizeRelationRequest(request, s.clock().UTC())
	if err != nil {
		return RelationWriteResult{}, err
	}
	created, err := s.repo.AppendRelation(ctx, relation)
	if errors.Is(err, ErrExists) {
		existing, getErr := s.repo.GetRelation(ctx, relation.OwnerIdentity, relation.ID)
		if getErr != nil {
			return RelationWriteResult{}, getErr
		}
		return RelationWriteResult{Relation: existing, AlreadyExisted: true}, nil
	}
	if err != nil {
		return RelationWriteResult{}, err
	}
	return RelationWriteResult{Relation: created}, nil
}

func (s *Service) GetEntity(ctx context.Context, owner, id string) (Entity, error) {
	if s == nil || s.repo == nil {
		return Entity{}, fmt.Errorf("life ontology service is unavailable")
	}
	if err := validateOwner(compact(owner)); err != nil {
		return Entity{}, err
	}
	return s.repo.GetEntity(ctx, compact(owner), compact(id))
}

func (s *Service) GetRelation(ctx context.Context, owner, id string) (Relation, error) {
	if s == nil || s.repo == nil {
		return Relation{}, fmt.Errorf("life ontology service is unavailable")
	}
	if err := validateOwner(compact(owner)); err != nil {
		return Relation{}, err
	}
	return s.repo.GetRelation(ctx, compact(owner), compact(id))
}

func (s *Service) QueryEntities(ctx context.Context, owner string, query EntityQuery) ([]Entity, error) {
	if s == nil || s.repo == nil {
		return nil, fmt.Errorf("life ontology service is unavailable")
	}
	owner = compact(owner)
	if err := validateOwner(owner); err != nil {
		return nil, err
	}
	limit, err := normalizedLimit(query.Limit)
	if err != nil {
		return nil, err
	}
	domains, err := domainSet(query.Domains)
	if err != nil {
		return nil, err
	}
	types, err := entityTypeSet(query.Types)
	if err != nil {
		return nil, err
	}
	statuses, err := statusSet(query.Statuses)
	if err != nil {
		return nil, err
	}
	verification, err := verificationSet(query.VerificationStatuses)
	if err != nil {
		return nil, err
	}
	externalKeys := normalizeExternalKeys(query.ExternalKeys)
	if len(query.ExternalKeys) > 0 {
		if len(externalKeys) == 0 {
			return nil, fmt.Errorf("external key filter cannot be empty")
		}
		if err := validateExternalKeys(externalKeys); err != nil {
			return nil, err
		}
	}
	query.ExternalKeys = externalKeys
	query.Limit = limit
	if bounded, ok := s.repo.(BoundedQueryRepository); ok {
		return bounded.QueryEntities(ctx, owner, query)
	}
	entities, err := s.repo.ListEntities(ctx, owner)
	if err != nil {
		return nil, err
	}
	result := make([]Entity, 0, min(limit, len(entities)))
	for _, entity := range entities {
		if entity.LocalOnly && !query.AllowLocalOnly {
			continue
		}
		if len(domains) > 0 {
			if _, ok := domains[entity.Domain]; !ok {
				continue
			}
		}
		if len(types) > 0 {
			if _, ok := types[entity.Type]; !ok {
				continue
			}
		}
		if len(statuses) > 0 {
			if _, ok := statuses[entity.Status]; !ok {
				continue
			}
		}
		if len(verification) > 0 {
			if _, ok := verification[entity.VerificationStatus]; !ok {
				continue
			}
		}
		if len(externalKeys) > 0 && !hasAnyExternalKey(entity.ExternalKeys, externalKeys) {
			continue
		}
		if query.AsOf != nil && !activeAt(entity.ValidFrom, entity.ValidUntil, entity.ObservedAt, *query.AsOf) {
			continue
		}
		if query.ObservedBy != nil && entity.ObservedAt.After(query.ObservedBy.UTC()) {
			continue
		}
		result = append(result, entity)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Priority != result[j].Priority {
			return result[i].Priority > result[j].Priority
		}
		return result[i].ID < result[j].ID
	})
	if len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

func (s *Service) QueryRelations(ctx context.Context, owner string, query RelationQuery) ([]Relation, error) {
	if s == nil || s.repo == nil {
		return nil, fmt.Errorf("life ontology service is unavailable")
	}
	owner = compact(owner)
	if err := validateOwner(owner); err != nil {
		return nil, err
	}
	limit, err := normalizedLimit(query.Limit)
	if err != nil {
		return nil, err
	}
	types, err := relationTypeSet(query.Types)
	if err != nil {
		return nil, err
	}
	if query.FromEntityID != "" && !validEntityID(query.FromEntityID) {
		return nil, fmt.Errorf("invalid from entity id")
	}
	if query.ToEntityID != "" && !validEntityID(query.ToEntityID) {
		return nil, fmt.Errorf("invalid to entity id")
	}
	query.Limit = limit
	if bounded, ok := s.repo.(BoundedQueryRepository); ok {
		return bounded.QueryRelations(ctx, owner, query)
	}
	relations, err := s.repo.ListRelations(ctx, owner)
	if err != nil {
		return nil, err
	}
	result := make([]Relation, 0, min(limit, len(relations)))
	for _, relation := range relations {
		if relation.LocalOnly && !query.AllowLocalOnly {
			continue
		}
		if len(types) > 0 {
			if _, ok := types[relation.Type]; !ok {
				continue
			}
		}
		if query.FromEntityID != "" && relation.FromEntityID != query.FromEntityID {
			continue
		}
		if query.ToEntityID != "" && relation.ToEntityID != query.ToEntityID {
			continue
		}
		if query.AsOf != nil && !activeAt(relation.ValidFrom, relation.ValidUntil, relation.ObservedAt, *query.AsOf) {
			continue
		}
		if query.ObservedBy != nil && relation.ObservedAt.After(query.ObservedBy.UTC()) {
			continue
		}
		result = append(result, relation)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	if len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

func (s *Service) ListMergeProposals(ctx context.Context, owner string, limit int) ([]MergeProposal, error) {
	if s == nil || s.repo == nil {
		return nil, fmt.Errorf("life ontology service is unavailable")
	}
	owner = compact(owner)
	if err := validateOwner(owner); err != nil {
		return nil, err
	}
	limit, err := normalizedLimit(limit)
	if err != nil {
		return nil, err
	}
	if bounded, ok := s.repo.(BoundedQueryRepository); ok {
		return bounded.ListMergeProposalsWithLimit(ctx, owner, limit)
	}
	proposals, err := s.repo.ListMergeProposals(ctx, owner)
	if err != nil {
		return nil, err
	}
	if len(proposals) > limit {
		proposals = proposals[:limit]
	}
	return proposals, nil
}

func (s *Service) SuggestNextContext(ctx context.Context, request ContextSuggestionRequest) (ContextSuggestionResult, error) {
	if s == nil || s.repo == nil {
		return ContextSuggestionResult{}, fmt.Errorf("life ontology service is unavailable")
	}
	request.OwnerIdentity = compact(request.OwnerIdentity)
	if err := validateOwner(request.OwnerIdentity); err != nil {
		return ContextSuggestionResult{}, err
	}
	if request.AsOf.IsZero() {
		return ContextSuggestionResult{}, fmt.Errorf("asOf is required")
	}
	limit, err := normalizedLimit(request.Limit)
	if err != nil {
		return ContextSuggestionResult{}, err
	}
	if _, err := domainSet(request.Domains); err != nil {
		return ContextSuggestionResult{}, err
	}
	if _, err := entityTypeSet(request.Types); err != nil {
		return ContextSuggestionResult{}, err
	}
	if request.FocusEntityID != "" {
		if _, err := s.repo.GetEntity(ctx, request.OwnerIdentity, request.FocusEntityID); err != nil {
			return ContextSuggestionResult{}, fmt.Errorf("focus entity: %w", err)
		}
	}
	entities, err := s.QueryEntities(ctx, request.OwnerIdentity, EntityQuery{Domains: request.Domains, Types: request.Types, AsOf: &request.AsOf, AllowLocalOnly: request.AllowLocalOnly, Limit: maximumLimit})
	if err != nil {
		return ContextSuggestionResult{}, err
	}
	relations, err := s.QueryRelations(ctx, request.OwnerIdentity, RelationQuery{AsOf: &request.AsOf, AllowLocalOnly: request.AllowLocalOnly, Limit: maximumLimit})
	if err != nil {
		return ContextSuggestionResult{}, err
	}
	suggestions := make([]ContextSuggestion, 0, len(entities))
	for _, entity := range entities {
		if entity.ID == request.FocusEntityID {
			continue
		}
		if entity.VerificationStatus == VerificationUnsupported {
			continue
		}
		suggestion := scoreContext(entity, relations, request.FocusEntityID, request.AsOf.UTC())
		suggestions = append(suggestions, suggestion)
	}
	sort.Slice(suggestions, func(i, j int) bool {
		if suggestions[i].Score != suggestions[j].Score {
			return suggestions[i].Score > suggestions[j].Score
		}
		return suggestions[i].Entity.ID < suggestions[j].Entity.ID
	})
	truncated := len(suggestions) > limit
	if truncated {
		suggestions = suggestions[:limit]
	}
	digest, err := hashCanonical(struct {
		Schema      string              `json:"schema"`
		Owner       string              `json:"owner"`
		AsOf        string              `json:"asOf"`
		Focus       string              `json:"focus"`
		Suggestions []ContextSuggestion `json:"suggestions"`
	}{Schema: SchemaVersion, Owner: request.OwnerIdentity, AsOf: request.AsOf.UTC().Format(time.RFC3339Nano), Focus: request.FocusEntityID, Suggestions: suggestions})
	if err != nil {
		return ContextSuggestionResult{}, err
	}
	return ContextSuggestionResult{AsOf: request.AsOf.UTC(), Suggestions: suggestions, Truncated: truncated, Explanation: "Ranked from current source-backed records using urgency, priority, verification, confidence, entity type, and direct graph proximity.", DecisionDigest: digest, AdvisoryOnly: true, CanExecute: false, GrantsAuthority: false}, nil
}

func (s *Service) proposeMerges(ctx context.Context, entity Entity) ([]MergeProposal, error) {
	entities, err := s.repo.ListEntities(ctx, entity.OwnerIdentity)
	if err != nil {
		return nil, err
	}
	result := make([]MergeProposal, 0)
	for _, candidate := range entities {
		if candidate.ID == entity.ID || candidate.Type != entity.Type {
			continue
		}
		match, reasons, confidence := mergeEvidence(candidate, entity)
		if match == "" {
			continue
		}
		proposal, err := buildMergeProposal(entity.OwnerIdentity, candidate.ID, entity.ID, match, reasons, confidence, s.clock().UTC())
		if err != nil {
			return nil, err
		}
		created, appendErr := s.repo.AppendMergeProposal(ctx, proposal)
		if errors.Is(appendErr, ErrExists) {
			created = proposal
		} else if appendErr != nil {
			return nil, appendErr
		}
		result = append(result, created)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, nil
}

func buildMergeProposal(owner, leftID, rightID string, match MergeMatch, reasons []string, confidence float64, now time.Time) (MergeProposal, error) {
	ids := []string{leftID, rightID}
	sort.Strings(ids)
	reasons = normalizeStrings(reasons)
	// PostgreSQL stores timestamptz values at microsecond precision. Canonicalize
	// before serializing the append-only payload so its indexed timestamp remains
	// byte-for-byte consistent with the value enforced by the database constraint.
	now = now.UTC().Round(time.Microsecond)
	digest, err := hashCanonical(struct {
		Schema, Owner string
		IDs           []string
		Match         MergeMatch
		Reasons       []string
		Confidence    float64
	}{Schema: SchemaVersion, Owner: owner, IDs: ids, Match: match, Reasons: reasons, Confidence: confidence})
	if err != nil {
		return MergeProposal{}, err
	}
	return MergeProposal{ID: "life-merge-" + digest, OwnerIdentity: owner, CandidateEntityIDs: ids, Match: match, Reasons: reasons, Confidence: confidence, Status: "proposed", ProposalDigest: digest, CreatedAt: now, AdvisoryOnly: true}, nil
}

func validateMergeProposal(proposal MergeProposal) error {
	if err := validateOwner(proposal.OwnerIdentity); err != nil {
		return err
	}
	if len(proposal.CandidateEntityIDs) != 2 || proposal.CandidateEntityIDs[0] >= proposal.CandidateEntityIDs[1] || !validEntityID(proposal.CandidateEntityIDs[0]) || !validEntityID(proposal.CandidateEntityIDs[1]) {
		return fmt.Errorf("merge proposal requires two sorted canonical candidates")
	}
	if proposal.Match != MergeExternalKey && proposal.Match != MergeSemanticIdentity {
		return fmt.Errorf("invalid merge match")
	}
	if len(proposal.Reasons) == 0 || len(proposal.Reasons) > 8 || proposal.Confidence < 0 || proposal.Confidence > 1 || proposal.Status != "proposed" {
		return fmt.Errorf("invalid merge proposal evidence")
	}
	if !proposal.AdvisoryOnly || proposal.CanExecute || proposal.GrantsAuthority {
		return fmt.Errorf("merge proposal cannot grant authority")
	}
	if proposal.CreatedAt.IsZero() || !sha256Pattern.MatchString(proposal.ProposalDigest) || proposal.ID != "life-merge-"+proposal.ProposalDigest {
		return fmt.Errorf("invalid merge proposal identity")
	}
	expected, err := buildMergeProposal(proposal.OwnerIdentity, proposal.CandidateEntityIDs[0], proposal.CandidateEntityIDs[1], proposal.Match, proposal.Reasons, proposal.Confidence, proposal.CreatedAt)
	if err != nil || expected.ProposalDigest != proposal.ProposalDigest {
		return fmt.Errorf("merge proposal digest mismatch")
	}
	return nil
}

func mergeEvidence(left, right Entity) (MergeMatch, []string, float64) {
	shared := sharedExternalKeys(left.ExternalKeys, right.ExternalKeys)
	if len(shared) > 0 {
		return MergeExternalKey, []string{"same stable external key: " + strings.Join(shared, ", ")}, 1
	}
	if left.Domain == right.Domain && normalized(left.Name) == normalized(right.Name) {
		return MergeSemanticIdentity, []string{"same normalized name, type, and life domain"}, 0.85
	}
	return "", nil, 0
}

func scoreContext(entity Entity, relations []Relation, focusID string, asOf time.Time) ContextSuggestion {
	base := map[EntityType]int{EntityRisk: 30, EntityObligation: 28, EntityNeed: 24, EntityGoal: 20, EntityOpportunity: 18, EntityCase: 16, EntityProject: 14, EntityAsset: 8, EntityPerson: 6}[entity.Type]
	reasons := []string{fmt.Sprintf("%s context carries base relevance %d", entity.Type, base)}
	score := base + entity.Priority/4
	if entity.Priority > 0 {
		reasons = append(reasons, fmt.Sprintf("priority %d adds urgency", entity.Priority))
	}
	if entity.Status == StatusOpen || entity.Status == StatusActive {
		score += 12
		reasons = append(reasons, "record is open or active")
	} else if entity.Status == StatusWaiting {
		score += 8
		reasons = append(reasons, "record is waiting on an open loop")
	} else if entity.Status == StatusCompleted || entity.Status == StatusArchived {
		score -= 25
		reasons = append(reasons, "completed or archived context is deprioritized")
	}
	if entity.DueAt != nil {
		days := int(entity.DueAt.Sub(asOf).Hours() / 24)
		if entity.DueAt.Before(asOf) {
			score += 40
			reasons = append(reasons, "due date is overdue")
		} else if days <= 7 {
			score += 30
			reasons = append(reasons, "due within seven days")
		} else if days <= 30 {
			score += 12
			reasons = append(reasons, "due within thirty days")
		}
	}
	verificationBonus := map[VerificationStatus]int{VerificationVerified: 15, VerificationHumanApproved: 13, VerificationSourceSupported: 11, VerificationSchemaValidated: 8, VerificationNeedsReview: 2, VerificationUncertain: -5, VerificationConflicting: -10, VerificationUnsupported: -20}[entity.VerificationStatus]
	score += verificationBonus + int(entity.Confidence*10)
	reasons = append(reasons, fmt.Sprintf("verification %s and confidence %.2f affect reliability", entity.VerificationStatus, entity.Confidence))
	relationIDs := make([]string, 0)
	if focusID != "" {
		for _, relation := range relations {
			if (relation.FromEntityID == focusID && relation.ToEntityID == entity.ID) || (relation.ToEntityID == focusID && relation.FromEntityID == entity.ID) {
				relationIDs = append(relationIDs, relation.ID)
			}
		}
		if len(relationIDs) > 0 {
			score += 25
			reasons = append(reasons, "directly related to the focus entity")
		}
	}
	sort.Strings(relationIDs)
	if score < 0 {
		score = 0
	}
	return ContextSuggestion{Entity: entity, Score: score, Reasons: reasons, RelatedRelationIDs: relationIDs, RecommendedUse: recommendedUse(entity)}
}

func recommendedUse(entity Entity) string {
	switch entity.Type {
	case EntityRisk:
		return "Review the risk, its evidence, and mitigations before planning."
	case EntityObligation:
		return "Check the obligation, responsible party, and due date."
	case EntityNeed:
		return "Use this need to test whether the next plan improves the owner's situation."
	case EntityGoal:
		return "Use this goal to align success criteria and next actions."
	case EntityOpportunity:
		return "Assess the opportunity against active goals, risks, and capacity."
	default:
		return "Use this source-backed context when it is relevant to the current decision."
	}
}

func validateRelationEndpoints(kind RelationType, from, to EntityType) error {
	valid := true
	switch kind {
	case RelationHasNeed:
		valid = from == EntityPerson && to == EntityNeed
	case RelationPursuesGoal:
		valid = from == EntityPerson && to == EntityGoal
	case RelationOwnsAsset:
		valid = from == EntityPerson && to == EntityAsset
	case RelationOwesObligation:
		valid = from == EntityPerson && to == EntityObligation
	case RelationAdvances:
		valid = (from == EntityProject || from == EntityOpportunity) && to == EntityGoal
	case RelationBelongsToProject:
		valid = to == EntityProject
	case RelationBelongsToPursuit:
		valid = to == EntityPursuit
	case RelationBelongsToWorkflow:
		valid = to == EntityWorkflow
	case RelationRelatedToCase:
		valid = to == EntityCase
	case RelationCreatesOpportunity:
		valid = (from == EntityPerson || from == EntityProject) && to == EntityOpportunity
	case RelationThreatens:
		valid = from == EntityRisk
	case RelationMitigates:
		valid = to == EntityRisk
	case RelationDerivedFrom:
		valid = to == EntitySource || to == EntityDocument
	case RelationDocuments:
		valid = from == EntitySource || from == EntityDocument
	case RelationProduces:
		valid = from == EntityTask || from == EntityWorkflow || from == EntityPursuit || from == EntityProject
	case RelationFulfills:
		valid = (from == EntityTask || from == EntityOutcome) && (to == EntityCommitment || to == EntityObligation || to == EntityGoal)
	case RelationAssignedTo:
		valid = to == EntityPerson
	case RelationIncursCost:
		valid = to == EntityCost
	case RelationDependsOn, RelationSupports, RelationConflictsWith, RelationRequires:
	default:
		valid = false
	}
	if !valid {
		return fmt.Errorf("relation %q does not allow %q -> %q", kind, from, to)
	}
	return nil
}

func strongestSensitivity(values ...Sensitivity) Sensitivity {
	rank := map[Sensitivity]int{"": 1, SensitivityPublic: 0, SensitivityInternal: 1, SensitivitySensitive: 2, SensitivityRestricted: 3}
	best := SensitivityPublic
	seen := false
	for _, value := range values {
		if value == "" {
			continue
		}
		seen = true
		if rank[value] > rank[best] {
			best = value
		}
	}
	if !seen {
		return SensitivityInternal
	}
	return best
}
func normalizeStrings(values []string) []string {
	set := make(map[string]string)
	for _, value := range values {
		if compact(value) != "" {
			set[normalized(value)] = compact(value)
		}
	}
	result := make([]string, 0, len(set))
	for _, value := range set {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
func sharedExternalKeys(left, right []ExternalKey) []string {
	rightSet := make(map[string]struct{})
	for _, key := range right {
		rightSet[externalKeyID(key)] = struct{}{}
	}
	result := make([]string, 0)
	for _, key := range left {
		if _, ok := rightSet[externalKeyID(key)]; ok {
			result = append(result, key.Namespace+":"+key.Value)
		}
	}
	sort.Strings(result)
	return result
}
func hasAnyExternalKey(entity, query []ExternalKey) bool {
	return len(sharedExternalKeys(entity, query)) > 0
}
func domainSet(values []Domain) (map[Domain]struct{}, error) {
	result := make(map[Domain]struct{})
	for _, value := range values {
		if _, ok := validDomains[value]; !ok {
			return nil, fmt.Errorf("invalid life domain %q", value)
		}
		result[value] = struct{}{}
	}
	return result, nil
}
func entityTypeSet(values []EntityType) (map[EntityType]struct{}, error) {
	result := make(map[EntityType]struct{})
	for _, value := range values {
		if _, ok := validEntityTypes[value]; !ok {
			return nil, fmt.Errorf("invalid entity type %q", value)
		}
		result[value] = struct{}{}
	}
	return result, nil
}
func relationTypeSet(values []RelationType) (map[RelationType]struct{}, error) {
	result := make(map[RelationType]struct{})
	for _, value := range values {
		if _, ok := validRelationTypes[value]; !ok {
			return nil, fmt.Errorf("invalid relation type %q", value)
		}
		result[value] = struct{}{}
	}
	return result, nil
}
func statusSet(values []LifecycleStatus) (map[LifecycleStatus]struct{}, error) {
	result := make(map[LifecycleStatus]struct{})
	for _, value := range values {
		if _, ok := validStatuses[value]; !ok {
			return nil, fmt.Errorf("invalid lifecycle status %q", value)
		}
		result[value] = struct{}{}
	}
	return result, nil
}
func verificationSet(values []VerificationStatus) (map[VerificationStatus]struct{}, error) {
	result := make(map[VerificationStatus]struct{})
	for _, value := range values {
		if _, ok := validVerificationStatuses[value]; !ok {
			return nil, fmt.Errorf("invalid verification status %q", value)
		}
		result[value] = struct{}{}
	}
	return result, nil
}
