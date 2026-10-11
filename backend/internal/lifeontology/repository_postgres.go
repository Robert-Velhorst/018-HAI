package lifeontology

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"automation-hub-backend/internal/infra"

	"gorm.io/gorm"
)

// PostgresRepository persists the owner-scoped life ontology as immutable
// envelopes. Schema creation and append-only enforcement belong to migration
// 0019; the repository never creates or repairs schema implicitly.
type PostgresRepository struct {
	DB *gorm.DB
}

func NewPostgresRepository(db *gorm.DB) *PostgresRepository {
	return &PostgresRepository{DB: db}
}

// DefaultRepository opens the configured database and applies the versioned
// migration chain. It deliberately has no in-memory fallback.
func DefaultRepository() (Repository, error) {
	db, err := infra.GetDefaultDB()
	if err != nil {
		return nil, err
	}
	return NewPostgresRepository(db), nil
}

func (r *PostgresRepository) AppendEntity(ctx context.Context, entity Entity) (Entity, error) {
	if err := r.ready(); err != nil {
		return Entity{}, err
	}
	if err := validateStoredEntity(entity); err != nil {
		return Entity{}, err
	}
	payload, err := marshalPostgresEnvelope("life ontology entity", entity)
	if err != nil {
		return Entity{}, err
	}
	result := r.DB.WithContext(ctx).Exec(`
		INSERT INTO public.life_ontology_entities (
			owner_identity, entity_id, entity_type, life_domain,
			lifecycle_status, verification_status, sensitivity, local_only,
			priority, entity_digest, provenance_digest, valid_from,
			valid_until, observed_at, created_at, payload
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CAST(? AS jsonb))
		ON CONFLICT DO NOTHING`,
		entity.OwnerIdentity, entity.ID, entity.Type, entity.Domain,
		entity.Status, entity.VerificationStatus, entity.Sensitivity, entity.LocalOnly,
		entity.Priority, entity.EntityDigest, entity.ProvenanceDigest, entity.ValidFrom.UTC(),
		entity.ValidUntil, entity.ObservedAt.UTC(), entity.CreatedAt.UTC(), string(payload),
	)
	if result.Error != nil {
		return Entity{}, fmt.Errorf("append life ontology entity: %w", result.Error)
	}
	if result.RowsAffected == 1 {
		return cloneEntity(entity), nil
	}
	existing, err := r.GetEntity(ctx, entity.OwnerIdentity, entity.ID)
	if err != nil {
		return Entity{}, fmt.Errorf("resolve life ontology entity duplicate: %w", err)
	}
	if existing.EntityDigest != entity.EntityDigest {
		return Entity{}, fmt.Errorf("%w: deterministic entity identity collision", ErrCorruptStorage)
	}
	return existing, ErrExists
}

func (r *PostgresRepository) GetEntity(ctx context.Context, owner, id string) (Entity, error) {
	owner, err := normalizePostgresOwner(owner)
	if err != nil {
		return Entity{}, err
	}
	if err := r.ready(); err != nil {
		return Entity{}, err
	}
	var row postgresEntityRow
	query := r.DB.WithContext(ctx).Raw(`
		SELECT owner_identity, entity_id, entity_type, life_domain,
			lifecycle_status, verification_status, sensitivity, local_only,
			priority, entity_digest, provenance_digest, valid_from,
			valid_until, observed_at, created_at, payload::text AS payload
		FROM public.life_ontology_entities
		WHERE owner_identity = ? AND entity_id = ?`, owner, strings.TrimSpace(id)).Scan(&row)
	if query.Error != nil {
		return Entity{}, fmt.Errorf("read life ontology entity: %w", query.Error)
	}
	if query.RowsAffected != 1 {
		return Entity{}, ErrNotFound
	}
	return decodePostgresEntityRow(row, owner)
}

func (r *PostgresRepository) ListEntities(ctx context.Context, owner string) ([]Entity, error) {
	owner, err := normalizePostgresOwner(owner)
	if err != nil {
		return nil, err
	}
	if err := r.ready(); err != nil {
		return nil, err
	}
	var rows []postgresEntityRow
	if err := r.DB.WithContext(ctx).Raw(`
		SELECT owner_identity, entity_id, entity_type, life_domain,
			lifecycle_status, verification_status, sensitivity, local_only,
			priority, entity_digest, provenance_digest, valid_from,
			valid_until, observed_at, created_at, payload::text AS payload
		FROM public.life_ontology_entities
		WHERE owner_identity = ?
		ORDER BY entity_id ASC`, owner).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("list life ontology entities: %w", err)
	}
	result := make([]Entity, 0, len(rows))
	for _, row := range rows {
		entity, err := decodePostgresEntityRow(row, owner)
		if err != nil {
			return nil, err
		}
		result = append(result, entity)
	}
	return result, nil
}

func (r *PostgresRepository) QueryEntities(ctx context.Context, owner string, query EntityQuery) ([]Entity, error) {
	owner, err := normalizePostgresOwner(owner)
	if err != nil {
		return nil, err
	}
	if err := r.ready(); err != nil {
		return nil, err
	}
	limit, err := normalizedLimit(query.Limit)
	if err != nil {
		return nil, err
	}
	if _, err := domainSet(query.Domains); err != nil {
		return nil, err
	}
	if _, err := entityTypeSet(query.Types); err != nil {
		return nil, err
	}
	if _, err := statusSet(query.Statuses); err != nil {
		return nil, err
	}
	if _, err := verificationSet(query.VerificationStatuses); err != nil {
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

	db := r.DB.WithContext(ctx).
		Table("public.life_ontology_entities").
		Select(`owner_identity, entity_id, entity_type, life_domain,
			lifecycle_status, verification_status, sensitivity, local_only,
			priority, entity_digest, provenance_digest, valid_from,
			valid_until, observed_at, created_at, payload::text AS payload`).
		Where("owner_identity = ?", owner)
	if !query.AllowLocalOnly {
		db = db.Where("local_only = FALSE")
	}
	if len(query.Domains) > 0 {
		db = db.Where("life_domain IN ?", query.Domains)
	}
	if len(query.Types) > 0 {
		db = db.Where("entity_type IN ?", query.Types)
	}
	if len(query.Statuses) > 0 {
		db = db.Where("lifecycle_status IN ?", query.Statuses)
	}
	if len(query.VerificationStatuses) > 0 {
		db = db.Where("verification_status IN ?", query.VerificationStatuses)
	}
	if len(externalKeys) > 0 {
		predicates := make([]string, 0, len(externalKeys))
		arguments := make([]any, 0, len(externalKeys))
		for _, key := range externalKeys {
			encoded, marshalErr := json.Marshal(map[string]any{"externalKeys": []ExternalKey{key}})
			if marshalErr != nil {
				return nil, fmt.Errorf("encode external key filter: %w", marshalErr)
			}
			predicates = append(predicates, "payload @> CAST(? AS jsonb)")
			arguments = append(arguments, string(encoded))
		}
		db = db.Where("("+strings.Join(predicates, " OR ")+")", arguments...)
	}
	if query.AsOf != nil {
		asOf := query.AsOf.UTC()
		db = db.Where("valid_from <= ? AND (valid_until IS NULL OR valid_until > ?) AND observed_at <= ?", asOf, asOf, asOf)
	}
	if query.ObservedBy != nil {
		db = db.Where("observed_at <= ?", query.ObservedBy.UTC())
	}

	var rows []postgresEntityRow
	if err := db.Order("priority DESC, entity_id ASC").Limit(limit).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("query life ontology entities: %w", err)
	}
	result := make([]Entity, 0, len(rows))
	for _, row := range rows {
		entity, decodeErr := decodePostgresEntityRow(row, owner)
		if decodeErr != nil {
			return nil, decodeErr
		}
		result = append(result, entity)
	}
	return result, nil
}

func (r *PostgresRepository) AppendRelation(ctx context.Context, relation Relation) (Relation, error) {
	if err := r.ready(); err != nil {
		return Relation{}, err
	}
	if err := validateStoredRelation(relation); err != nil {
		return Relation{}, err
	}
	payload, err := marshalPostgresEnvelope("life ontology relation", relation)
	if err != nil {
		return Relation{}, err
	}
	result := r.DB.WithContext(ctx).Exec(`
		INSERT INTO public.life_ontology_relations (
			owner_identity, relation_id, relation_type, from_entity_id,
			to_entity_id, verification_status, sensitivity, local_only,
			relation_digest, provenance_digest, valid_from, valid_until,
			observed_at, created_at, payload
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CAST(? AS jsonb))
		ON CONFLICT DO NOTHING`,
		relation.OwnerIdentity, relation.ID, relation.Type, relation.FromEntityID,
		relation.ToEntityID, relation.VerificationStatus, relation.Sensitivity, relation.LocalOnly,
		relation.RelationDigest, relation.ProvenanceDigest, relation.ValidFrom.UTC(), relation.ValidUntil,
		relation.ObservedAt.UTC(), relation.CreatedAt.UTC(), string(payload),
	)
	if result.Error != nil {
		return Relation{}, fmt.Errorf("append life ontology relation: %w", result.Error)
	}
	if result.RowsAffected == 1 {
		return cloneRelation(relation), nil
	}
	existing, err := r.GetRelation(ctx, relation.OwnerIdentity, relation.ID)
	if err != nil {
		return Relation{}, fmt.Errorf("resolve life ontology relation duplicate: %w", err)
	}
	if existing.RelationDigest != relation.RelationDigest {
		return Relation{}, fmt.Errorf("%w: deterministic relation identity collision", ErrCorruptStorage)
	}
	return existing, ErrExists
}

func (r *PostgresRepository) GetRelation(ctx context.Context, owner, id string) (Relation, error) {
	owner, err := normalizePostgresOwner(owner)
	if err != nil {
		return Relation{}, err
	}
	if err := r.ready(); err != nil {
		return Relation{}, err
	}
	var row postgresRelationRow
	query := r.DB.WithContext(ctx).Raw(`
		SELECT owner_identity, relation_id, relation_type, from_entity_id,
			to_entity_id, verification_status, sensitivity, local_only,
			relation_digest, provenance_digest, valid_from, valid_until,
			observed_at, created_at, payload::text AS payload
		FROM public.life_ontology_relations
		WHERE owner_identity = ? AND relation_id = ?`, owner, strings.TrimSpace(id)).Scan(&row)
	if query.Error != nil {
		return Relation{}, fmt.Errorf("read life ontology relation: %w", query.Error)
	}
	if query.RowsAffected != 1 {
		return Relation{}, ErrNotFound
	}
	return decodePostgresRelationRow(row, owner)
}

func (r *PostgresRepository) ListRelations(ctx context.Context, owner string) ([]Relation, error) {
	owner, err := normalizePostgresOwner(owner)
	if err != nil {
		return nil, err
	}
	if err := r.ready(); err != nil {
		return nil, err
	}
	var rows []postgresRelationRow
	if err := r.DB.WithContext(ctx).Raw(`
		SELECT owner_identity, relation_id, relation_type, from_entity_id,
			to_entity_id, verification_status, sensitivity, local_only,
			relation_digest, provenance_digest, valid_from, valid_until,
			observed_at, created_at, payload::text AS payload
		FROM public.life_ontology_relations
		WHERE owner_identity = ?
		ORDER BY relation_id ASC`, owner).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("list life ontology relations: %w", err)
	}
	result := make([]Relation, 0, len(rows))
	for _, row := range rows {
		relation, err := decodePostgresRelationRow(row, owner)
		if err != nil {
			return nil, err
		}
		result = append(result, relation)
	}
	return result, nil
}

func (r *PostgresRepository) QueryRelations(ctx context.Context, owner string, query RelationQuery) ([]Relation, error) {
	owner, err := normalizePostgresOwner(owner)
	if err != nil {
		return nil, err
	}
	if err := r.ready(); err != nil {
		return nil, err
	}
	limit, err := normalizedLimit(query.Limit)
	if err != nil {
		return nil, err
	}
	if _, err := relationTypeSet(query.Types); err != nil {
		return nil, err
	}
	if query.FromEntityID != "" && !validEntityID(query.FromEntityID) {
		return nil, fmt.Errorf("invalid from entity id")
	}
	if query.ToEntityID != "" && !validEntityID(query.ToEntityID) {
		return nil, fmt.Errorf("invalid to entity id")
	}

	db := r.DB.WithContext(ctx).
		Table("public.life_ontology_relations").
		Select(`owner_identity, relation_id, relation_type, from_entity_id,
			to_entity_id, verification_status, sensitivity, local_only,
			relation_digest, provenance_digest, valid_from, valid_until,
			observed_at, created_at, payload::text AS payload`).
		Where("owner_identity = ?", owner)
	if !query.AllowLocalOnly {
		db = db.Where("local_only = FALSE")
	}
	if len(query.Types) > 0 {
		db = db.Where("relation_type IN ?", query.Types)
	}
	if query.FromEntityID != "" {
		db = db.Where("from_entity_id = ?", query.FromEntityID)
	}
	if query.ToEntityID != "" {
		db = db.Where("to_entity_id = ?", query.ToEntityID)
	}
	if query.AsOf != nil {
		asOf := query.AsOf.UTC()
		db = db.Where("valid_from <= ? AND (valid_until IS NULL OR valid_until > ?) AND observed_at <= ?", asOf, asOf, asOf)
	}
	if query.ObservedBy != nil {
		db = db.Where("observed_at <= ?", query.ObservedBy.UTC())
	}

	var rows []postgresRelationRow
	if err := db.Order("relation_id ASC").Limit(limit).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("query life ontology relations: %w", err)
	}
	result := make([]Relation, 0, len(rows))
	for _, row := range rows {
		relation, decodeErr := decodePostgresRelationRow(row, owner)
		if decodeErr != nil {
			return nil, decodeErr
		}
		result = append(result, relation)
	}
	return result, nil
}

func (r *PostgresRepository) AppendMergeProposal(ctx context.Context, proposal MergeProposal) (MergeProposal, error) {
	if err := r.ready(); err != nil {
		return MergeProposal{}, err
	}
	if err := validateMergeProposal(proposal); err != nil {
		return MergeProposal{}, err
	}
	payload, err := marshalPostgresEnvelope("life ontology merge proposal", proposal)
	if err != nil {
		return MergeProposal{}, err
	}
	result := r.DB.WithContext(ctx).Exec(`
		INSERT INTO public.life_ontology_merge_proposals (
			owner_identity, proposal_id, candidate_left_id, candidate_right_id,
			match_type, proposal_status, confidence, proposal_digest,
			created_at, payload
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, CAST(? AS jsonb))
		ON CONFLICT DO NOTHING`,
		proposal.OwnerIdentity, proposal.ID, proposal.CandidateEntityIDs[0], proposal.CandidateEntityIDs[1],
		proposal.Match, proposal.Status, proposal.Confidence, proposal.ProposalDigest,
		proposal.CreatedAt.UTC(), string(payload),
	)
	if result.Error != nil {
		return MergeProposal{}, fmt.Errorf("append life ontology merge proposal: %w", result.Error)
	}
	if result.RowsAffected == 1 {
		return cloneProposal(proposal), nil
	}
	existing, err := r.getMergeProposal(ctx, proposal.OwnerIdentity, proposal.ID)
	if err != nil {
		return MergeProposal{}, fmt.Errorf("resolve life ontology merge proposal duplicate: %w", err)
	}
	if existing.ProposalDigest != proposal.ProposalDigest {
		return MergeProposal{}, fmt.Errorf("%w: deterministic merge proposal identity collision", ErrCorruptStorage)
	}
	return existing, ErrExists
}

func (r *PostgresRepository) ListMergeProposals(ctx context.Context, owner string) ([]MergeProposal, error) {
	owner, err := normalizePostgresOwner(owner)
	if err != nil {
		return nil, err
	}
	if err := r.ready(); err != nil {
		return nil, err
	}
	var rows []postgresMergeProposalRow
	if err := r.DB.WithContext(ctx).Raw(`
		SELECT owner_identity, proposal_id, candidate_left_id,
			candidate_right_id, match_type, proposal_status, confidence,
			proposal_digest, created_at, payload::text AS payload
		FROM public.life_ontology_merge_proposals
		WHERE owner_identity = ?
		ORDER BY proposal_id ASC`, owner).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("list life ontology merge proposals: %w", err)
	}
	result := make([]MergeProposal, 0, len(rows))
	for _, row := range rows {
		proposal, err := decodePostgresMergeProposalRow(row, owner)
		if err != nil {
			return nil, err
		}
		result = append(result, proposal)
	}
	return result, nil
}

func (r *PostgresRepository) ListMergeProposalsWithLimit(ctx context.Context, owner string, limit int) ([]MergeProposal, error) {
	owner, err := normalizePostgresOwner(owner)
	if err != nil {
		return nil, err
	}
	if err := r.ready(); err != nil {
		return nil, err
	}
	limit, err = normalizedLimit(limit)
	if err != nil {
		return nil, err
	}
	var rows []postgresMergeProposalRow
	if err := r.DB.WithContext(ctx).Raw(`
		SELECT owner_identity, proposal_id, candidate_left_id,
			candidate_right_id, match_type, proposal_status, confidence,
			proposal_digest, created_at, payload::text AS payload
		FROM public.life_ontology_merge_proposals
		WHERE owner_identity = ?
		ORDER BY proposal_id ASC
		LIMIT ?`, owner, limit).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("list bounded life ontology merge proposals: %w", err)
	}
	result := make([]MergeProposal, 0, len(rows))
	for _, row := range rows {
		proposal, decodeErr := decodePostgresMergeProposalRow(row, owner)
		if decodeErr != nil {
			return nil, decodeErr
		}
		result = append(result, proposal)
	}
	return result, nil
}

func (r *PostgresRepository) GetMergeProposal(ctx context.Context, owner, id string) (MergeProposal, error) {
	owner, err := normalizePostgresOwner(owner)
	if err != nil {
		return MergeProposal{}, err
	}
	return r.getMergeProposal(ctx, owner, id)
}

func (r *PostgresRepository) getMergeProposal(ctx context.Context, owner, id string) (MergeProposal, error) {
	owner, err := normalizePostgresOwner(owner)
	if err != nil {
		return MergeProposal{}, err
	}
	if err := r.ready(); err != nil {
		return MergeProposal{}, err
	}
	var row postgresMergeProposalRow
	query := r.DB.WithContext(ctx).Raw(`
		SELECT owner_identity, proposal_id, candidate_left_id,
			candidate_right_id, match_type, proposal_status, confidence,
			proposal_digest, created_at, payload::text AS payload
		FROM public.life_ontology_merge_proposals
		WHERE owner_identity = ? AND proposal_id = ?`, owner, strings.TrimSpace(id)).Scan(&row)
	if query.Error != nil {
		return MergeProposal{}, fmt.Errorf("read life ontology merge proposal: %w", query.Error)
	}
	if query.RowsAffected != 1 {
		return MergeProposal{}, ErrNotFound
	}
	return decodePostgresMergeProposalRow(row, owner)
}

func (r *PostgresRepository) AppendContactReviewDecision(ctx context.Context, decision ContactReviewDecision, canonical *Entity) (ContactReviewDecision, error) {
	if err := r.ready(); err != nil {
		return ContactReviewDecision{}, err
	}
	if err := validateContactReviewDecision(decision); err != nil {
		return ContactReviewDecision{}, err
	}
	if canonical != nil {
		if err := validateStoredEntity(*canonical); err != nil {
			return ContactReviewDecision{}, err
		}
		if canonical.OwnerIdentity != decision.OwnerIdentity || canonical.ID != decision.CanonicalEntityID {
			return ContactReviewDecision{}, fmt.Errorf("canonical contact does not match review decision")
		}
	}
	payload, err := marshalPostgresEnvelope("contact review decision", decision)
	if err != nil {
		return ContactReviewDecision{}, err
	}
	err = r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		transactional := NewPostgresRepository(tx)
		if canonical != nil {
			stored, appendErr := transactional.AppendEntity(ctx, *canonical)
			if appendErr != nil && !errors.Is(appendErr, ErrExists) {
				return appendErr
			}
			if stored.EntityDigest != canonical.EntityDigest {
				return fmt.Errorf("%w: canonical contact identity collision", ErrCorruptStorage)
			}
		}
		result := tx.WithContext(ctx).Exec(`
			INSERT INTO public.life_ontology_contact_review_decisions (
				owner_identity, decision_id, idempotency_key, subject_kind,
				subject_id, action, candidate_left_id, candidate_right_id,
				merge_proposal_id, canonical_entity_id, request_digest,
				record_digest, decided_at, recorded_at, payload
			) VALUES (?, ?, ?, ?, ?, ?, ?, NULLIF(?, ''), NULLIF(?, ''), NULLIF(?, ''), ?, ?, ?, ?, CAST(? AS jsonb))`,
			decision.OwnerIdentity, decision.ID, decision.IdempotencyKey, decision.Subject,
			decision.SubjectID, decision.Action, decision.CandidateEntityIDs[0], contactReviewCandidateRight(decision),
			contactReviewMergeProposalID(decision), decision.CanonicalEntityID, decision.RequestDigest,
			decision.RecordDigest, decision.DecidedAt.UTC(), decision.RecordedAt.UTC(), string(payload),
		)
		return result.Error
	})
	if err == nil {
		return cloneContactReviewDecision(decision), nil
	}
	existing, lookupErr := r.GetContactReviewDecisionByIdempotency(ctx, decision.OwnerIdentity, decision.IdempotencyKey)
	if lookupErr == nil {
		return existing, ErrExists
	}
	if subjectExisting, subjectErr := r.getContactReviewDecisionBySubject(ctx, decision.OwnerIdentity, decision.Subject, decision.SubjectID); subjectErr == nil {
		return subjectExisting, ErrExists
	}
	return ContactReviewDecision{}, fmt.Errorf("append contact review decision: %w", err)
}

func (r *PostgresRepository) GetContactReviewDecisionByIdempotency(ctx context.Context, owner, key string) (ContactReviewDecision, error) {
	owner, err := normalizePostgresOwner(owner)
	if err != nil {
		return ContactReviewDecision{}, err
	}
	if err := r.ready(); err != nil {
		return ContactReviewDecision{}, err
	}
	var row postgresContactReviewDecisionRow
	query := r.DB.WithContext(ctx).Raw(`
		SELECT owner_identity, decision_id, idempotency_key, subject_kind,
			subject_id, action, COALESCE(canonical_entity_id, '') AS canonical_entity_id,
			request_digest, record_digest, decided_at, recorded_at, payload::text AS payload
		FROM public.life_ontology_contact_review_decisions
		WHERE owner_identity = ? AND idempotency_key = ?`, owner, compact(key)).Scan(&row)
	if query.Error != nil {
		return ContactReviewDecision{}, fmt.Errorf("read contact review decision: %w", query.Error)
	}
	if query.RowsAffected != 1 {
		return ContactReviewDecision{}, ErrNotFound
	}
	return decodePostgresContactReviewDecisionRow(row, owner)
}

func (r *PostgresRepository) getContactReviewDecisionBySubject(ctx context.Context, owner string, subject ContactReviewSubject, subjectID string) (ContactReviewDecision, error) {
	var row postgresContactReviewDecisionRow
	query := r.DB.WithContext(ctx).Raw(`
		SELECT owner_identity, decision_id, idempotency_key, subject_kind,
			subject_id, action, COALESCE(canonical_entity_id, '') AS canonical_entity_id,
			request_digest, record_digest, decided_at, recorded_at, payload::text AS payload
		FROM public.life_ontology_contact_review_decisions
		WHERE owner_identity = ? AND subject_kind = ? AND subject_id = ?`, owner, subject, compact(subjectID)).Scan(&row)
	if query.Error != nil {
		return ContactReviewDecision{}, fmt.Errorf("read contact review decision by subject: %w", query.Error)
	}
	if query.RowsAffected != 1 {
		return ContactReviewDecision{}, ErrNotFound
	}
	return decodePostgresContactReviewDecisionRow(row, owner)
}

func (r *PostgresRepository) ListContactReviewDecisions(ctx context.Context, owner string, limit int) ([]ContactReviewDecision, error) {
	owner, err := normalizePostgresOwner(owner)
	if err != nil {
		return nil, err
	}
	if err := r.ready(); err != nil {
		return nil, err
	}
	if limit < 1 || limit > maximumLimit {
		return nil, fmt.Errorf("contact review decision limit must be between 1 and %d", maximumLimit)
	}
	var rows []postgresContactReviewDecisionRow
	if err := r.DB.WithContext(ctx).Raw(`
		SELECT owner_identity, decision_id, idempotency_key, subject_kind,
			subject_id, action, COALESCE(canonical_entity_id, '') AS canonical_entity_id,
			request_digest, record_digest, decided_at, recorded_at, payload::text AS payload
		FROM public.life_ontology_contact_review_decisions
		WHERE owner_identity = ?
		ORDER BY recorded_at DESC, decision_id ASC
		LIMIT ?`, owner, limit).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("list contact review decisions: %w", err)
	}
	result := make([]ContactReviewDecision, 0, len(rows))
	for _, row := range rows {
		decision, decodeErr := decodePostgresContactReviewDecisionRow(row, owner)
		if decodeErr != nil {
			return nil, decodeErr
		}
		result = append(result, decision)
	}
	return result, nil
}

func contactReviewCandidateRight(decision ContactReviewDecision) string {
	if len(decision.CandidateEntityIDs) == 2 {
		return decision.CandidateEntityIDs[1]
	}
	return ""
}

func contactReviewMergeProposalID(decision ContactReviewDecision) string {
	if decision.Subject == ContactReviewMergeProposal {
		return decision.SubjectID
	}
	return ""
}

func (r *PostgresRepository) ready() error {
	if r == nil || r.DB == nil {
		return fmt.Errorf("life ontology Postgres database is required")
	}
	return nil
}

type postgresEntityRow struct {
	OwnerIdentity      string
	EntityID           string
	EntityType         EntityType
	LifeDomain         Domain
	LifecycleStatus    LifecycleStatus
	VerificationStatus VerificationStatus
	Sensitivity        Sensitivity
	LocalOnly          bool
	Priority           int
	EntityDigest       string
	ProvenanceDigest   string
	ValidFrom          time.Time
	ValidUntil         *time.Time
	ObservedAt         time.Time
	CreatedAt          time.Time
	Payload            string
}

type postgresRelationRow struct {
	OwnerIdentity      string
	RelationID         string
	RelationType       RelationType
	FromEntityID       string
	ToEntityID         string
	VerificationStatus VerificationStatus
	Sensitivity        Sensitivity
	LocalOnly          bool
	RelationDigest     string
	ProvenanceDigest   string
	ValidFrom          time.Time
	ValidUntil         *time.Time
	ObservedAt         time.Time
	CreatedAt          time.Time
	Payload            string
}

type postgresMergeProposalRow struct {
	OwnerIdentity    string
	ProposalID       string
	CandidateLeftID  string
	CandidateRightID string
	MatchType        MergeMatch
	ProposalStatus   string
	Confidence       float64
	ProposalDigest   string
	CreatedAt        time.Time
	Payload          string
}

type postgresContactReviewDecisionRow struct {
	OwnerIdentity     string
	DecisionID        string
	IdempotencyKey    string
	SubjectKind       ContactReviewSubject
	SubjectID         string
	Action            ContactReviewAction
	CanonicalEntityID string
	RequestDigest     string
	RecordDigest      string
	DecidedAt         time.Time
	RecordedAt        time.Time
	Payload           string
}

func decodePostgresEntityRow(row postgresEntityRow, expectedOwner string) (Entity, error) {
	var entity Entity
	if err := decodePostgresEnvelope(row.Payload, &entity); err != nil {
		return Entity{}, fmt.Errorf("%w: decode life ontology entity: %v", ErrCorruptStorage, err)
	}
	if row.OwnerIdentity != expectedOwner || entity.OwnerIdentity != expectedOwner ||
		row.EntityID != entity.ID || row.EntityType != entity.Type || row.LifeDomain != entity.Domain ||
		row.LifecycleStatus != entity.Status || row.VerificationStatus != entity.VerificationStatus ||
		row.Sensitivity != entity.Sensitivity || row.LocalOnly != entity.LocalOnly ||
		row.Priority != entity.Priority || row.EntityDigest != entity.EntityDigest ||
		row.ProvenanceDigest != entity.ProvenanceDigest ||
		!postgresTimeEqual(row.ValidFrom, entity.ValidFrom) || !postgresTimePtrEqual(row.ValidUntil, entity.ValidUntil) ||
		!postgresTimeEqual(row.ObservedAt, entity.ObservedAt) || !postgresTimeEqual(row.CreatedAt, entity.CreatedAt) {
		return Entity{}, fmt.Errorf("%w: life ontology entity metadata mismatch", ErrCorruptStorage)
	}
	if err := validateStoredEntity(entity); err != nil {
		return Entity{}, err
	}
	return cloneEntity(entity), nil
}

func decodePostgresRelationRow(row postgresRelationRow, expectedOwner string) (Relation, error) {
	var relation Relation
	if err := decodePostgresEnvelope(row.Payload, &relation); err != nil {
		return Relation{}, fmt.Errorf("%w: decode life ontology relation: %v", ErrCorruptStorage, err)
	}
	if row.OwnerIdentity != expectedOwner || relation.OwnerIdentity != expectedOwner ||
		row.RelationID != relation.ID || row.RelationType != relation.Type ||
		row.FromEntityID != relation.FromEntityID || row.ToEntityID != relation.ToEntityID ||
		row.VerificationStatus != relation.VerificationStatus || row.Sensitivity != relation.Sensitivity ||
		row.LocalOnly != relation.LocalOnly || row.RelationDigest != relation.RelationDigest ||
		row.ProvenanceDigest != relation.ProvenanceDigest ||
		!postgresTimeEqual(row.ValidFrom, relation.ValidFrom) || !postgresTimePtrEqual(row.ValidUntil, relation.ValidUntil) ||
		!postgresTimeEqual(row.ObservedAt, relation.ObservedAt) || !postgresTimeEqual(row.CreatedAt, relation.CreatedAt) {
		return Relation{}, fmt.Errorf("%w: life ontology relation metadata mismatch", ErrCorruptStorage)
	}
	if err := validateStoredRelation(relation); err != nil {
		return Relation{}, err
	}
	return cloneRelation(relation), nil
}

func decodePostgresMergeProposalRow(row postgresMergeProposalRow, expectedOwner string) (MergeProposal, error) {
	var proposal MergeProposal
	if err := decodePostgresEnvelope(row.Payload, &proposal); err != nil {
		return MergeProposal{}, fmt.Errorf("%w: decode life ontology merge proposal: %v", ErrCorruptStorage, err)
	}
	if len(proposal.CandidateEntityIDs) != 2 || row.OwnerIdentity != expectedOwner ||
		proposal.OwnerIdentity != expectedOwner || row.ProposalID != proposal.ID ||
		row.CandidateLeftID != proposal.CandidateEntityIDs[0] || row.CandidateRightID != proposal.CandidateEntityIDs[1] ||
		row.MatchType != proposal.Match || row.ProposalStatus != proposal.Status ||
		row.Confidence != proposal.Confidence || row.ProposalDigest != proposal.ProposalDigest ||
		!postgresTimeEqual(row.CreatedAt, proposal.CreatedAt) {
		return MergeProposal{}, fmt.Errorf("%w: life ontology merge proposal metadata mismatch", ErrCorruptStorage)
	}
	if err := validateMergeProposal(proposal); err != nil {
		return MergeProposal{}, fmt.Errorf("%w: %v", ErrCorruptStorage, err)
	}
	return cloneProposal(proposal), nil
}

func decodePostgresContactReviewDecisionRow(row postgresContactReviewDecisionRow, expectedOwner string) (ContactReviewDecision, error) {
	var decision ContactReviewDecision
	if err := decodePostgresEnvelope(row.Payload, &decision); err != nil {
		return ContactReviewDecision{}, fmt.Errorf("%w: decode contact review decision: %v", ErrCorruptStorage, err)
	}
	if row.OwnerIdentity != expectedOwner || decision.OwnerIdentity != expectedOwner ||
		row.DecisionID != decision.ID || row.IdempotencyKey != decision.IdempotencyKey ||
		row.SubjectKind != decision.Subject || row.SubjectID != decision.SubjectID ||
		row.Action != decision.Action || row.CanonicalEntityID != decision.CanonicalEntityID ||
		row.RequestDigest != decision.RequestDigest || row.RecordDigest != decision.RecordDigest ||
		!postgresTimeEqual(row.DecidedAt, decision.DecidedAt) || !postgresTimeEqual(row.RecordedAt, decision.RecordedAt) {
		return ContactReviewDecision{}, fmt.Errorf("%w: contact review decision metadata mismatch", ErrCorruptStorage)
	}
	if err := validateContactReviewDecision(decision); err != nil {
		return ContactReviewDecision{}, fmt.Errorf("%w: %v", ErrCorruptStorage, err)
	}
	return cloneContactReviewDecision(decision), nil
}

func marshalPostgresEnvelope(label string, value any) ([]byte, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode %s: %w", label, err)
	}
	return payload, nil
}

func decodePostgresEnvelope(raw string, target any) error {
	decoder := json.NewDecoder(bytes.NewBufferString(strings.TrimSpace(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return fmt.Errorf("persisted JSON must contain exactly one value")
	}
	return nil
}

func normalizePostgresOwner(owner string) (string, error) {
	owner = compact(owner)
	if err := validateOwner(owner); err != nil {
		return "", err
	}
	return owner, nil
}

func postgresTimeEqual(left, right time.Time) bool {
	return left.UTC().Truncate(time.Microsecond).Equal(right.UTC().Truncate(time.Microsecond))
}

func postgresTimePtrEqual(left, right *time.Time) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return postgresTimeEqual(*left, *right)
}

var _ Repository = (*PostgresRepository)(nil)
