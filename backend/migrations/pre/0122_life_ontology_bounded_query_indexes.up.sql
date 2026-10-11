CREATE INDEX IF NOT EXISTS idx_life_ontology_entities_owner_visibility_priority
    ON public.life_ontology_entities
    (owner_identity, local_only, priority DESC, entity_id ASC);

CREATE INDEX IF NOT EXISTS idx_life_ontology_entities_owner_review_priority
    ON public.life_ontology_entities
    (owner_identity, entity_type, verification_status, local_only, priority DESC, entity_id ASC);

CREATE INDEX IF NOT EXISTS idx_life_ontology_relations_owner_visibility
    ON public.life_ontology_relations
    (owner_identity, local_only, relation_id ASC);
