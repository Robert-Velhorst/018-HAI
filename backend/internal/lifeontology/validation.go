package lifeontology

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"automation-hub-backend/internal/safety"
)

const (
	defaultLimit = 50
	maximumLimit = 100
)

var sha256Pattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

var validEntityTypes = map[EntityType]struct{}{
	EntityPerson: {}, EntityNeed: {}, EntityGoal: {}, EntityAsset: {}, EntityObligation: {},
	EntityProject: {}, EntityCase: {}, EntityOpportunity: {}, EntityRisk: {},
	EntitySource: {}, EntityDocument: {}, EntityPursuit: {}, EntityWorkflow: {}, EntityTask: {},
	EntityMemory: {}, EntityCommitment: {}, EntityCost: {}, EntityOutcome: {},
}

var validDomains = map[Domain]struct{}{
	DomainSafetySecurity: {}, DomainHealthWellbeing: {}, DomainRelationships: {}, DomainHousingAssets: {},
	DomainFinancial: {}, DomainWorkVenture: {}, DomainLearningGrowth: {}, DomainMeaningValues: {},
	DomainCommunityCivic: {}, DomainLegalGovernment: {}, DomainPersonalAdmin: {},
}

// IsValidDomain is the shared validation boundary for records authored by
// other packages before they are projected into the whole-life graph.
func IsValidDomain(domain Domain) bool {
	_, ok := validDomains[domain]
	return ok
}

var validRelationTypes = map[RelationType]struct{}{
	RelationHasNeed: {}, RelationPursuesGoal: {}, RelationOwnsAsset: {}, RelationOwesObligation: {},
	RelationAdvances: {}, RelationBelongsToProject: {}, RelationRelatedToCase: {}, RelationCreatesOpportunity: {},
	RelationThreatens: {}, RelationMitigates: {}, RelationDependsOn: {}, RelationSupports: {}, RelationConflictsWith: {},
	RelationDerivedFrom: {}, RelationDocuments: {}, RelationProduces: {}, RelationFulfills: {},
	RelationAssignedTo: {}, RelationRequires: {}, RelationIncursCost: {},
	RelationBelongsToPursuit: {}, RelationBelongsToWorkflow: {},
}

var validVerificationStatuses = map[VerificationStatus]struct{}{
	VerificationUnverified: {}, VerificationSourceSupported: {}, VerificationSchemaValidated: {},
	VerificationHumanApproved: {}, VerificationVerified: {}, VerificationUncertain: {},
	VerificationConflicting: {}, VerificationUnsupported: {}, VerificationNeedsReview: {},
}

var validSensitivities = map[Sensitivity]struct{}{
	SensitivityPublic: {}, SensitivityInternal: {}, SensitivitySensitive: {}, SensitivityRestricted: {},
}

var validStatuses = map[LifecycleStatus]struct{}{
	StatusOpen: {}, StatusActive: {}, StatusWaiting: {}, StatusCompleted: {}, StatusArchived: {}, StatusUnknown: {},
}

type canonicalEntity struct {
	SchemaVersion      string             `json:"schemaVersion"`
	OwnerIdentity      string             `json:"ownerIdentity"`
	Type               EntityType         `json:"type"`
	Domain             Domain             `json:"domain"`
	Name               string             `json:"name"`
	Summary            string             `json:"summary"`
	ExternalKeys       []ExternalKey      `json:"externalKeys"`
	Attributes         map[string]string  `json:"attributes"`
	Status             LifecycleStatus    `json:"status"`
	Priority           int                `json:"priority"`
	DueAt              string             `json:"dueAt"`
	ValidFrom          string             `json:"validFrom"`
	ValidUntil         string             `json:"validUntil"`
	ObservedAt         string             `json:"observedAt"`
	Confidence         float64            `json:"confidence"`
	VerificationStatus VerificationStatus `json:"verificationStatus"`
	ProvenanceDigest   string             `json:"provenanceDigest"`
	Sensitivity        Sensitivity        `json:"sensitivity"`
	LocalOnly          bool               `json:"localOnly"`
}

type canonicalRelation struct {
	SchemaVersion      string             `json:"schemaVersion"`
	OwnerIdentity      string             `json:"ownerIdentity"`
	Type               RelationType       `json:"type"`
	FromEntityID       string             `json:"fromEntityId"`
	ToEntityID         string             `json:"toEntityId"`
	Summary            string             `json:"summary"`
	Attributes         map[string]string  `json:"attributes"`
	ValidFrom          string             `json:"validFrom"`
	ValidUntil         string             `json:"validUntil"`
	ObservedAt         string             `json:"observedAt"`
	Confidence         float64            `json:"confidence"`
	VerificationStatus VerificationStatus `json:"verificationStatus"`
	ProvenanceDigest   string             `json:"provenanceDigest"`
	Sensitivity        Sensitivity        `json:"sensitivity"`
	LocalOnly          bool               `json:"localOnly"`
}

func normalizeEntityRequest(request RecordEntityRequest, now time.Time) (Entity, error) {
	entity := Entity{
		OwnerIdentity: compact(request.OwnerIdentity), Type: request.Type, Domain: request.Domain,
		Name: compact(request.Name), Summary: compact(request.Summary),
		ExternalKeys: normalizeExternalKeys(request.ExternalKeys), Attributes: normalizeAttributes(request.Attributes),
		Status: request.Status, Priority: request.Priority, DueAt: cloneTime(request.DueAt),
		ValidFrom: request.ValidFrom.UTC(), ValidUntil: cloneTime(request.ValidUntil), ObservedAt: request.ObservedAt.UTC(),
		Confidence: request.Confidence, VerificationStatus: request.VerificationStatus,
		Provenance: normalizeProvenance(request.Provenance), Sensitivity: request.Sensitivity,
		LocalOnly: request.LocalOnly, CreatedAt: now.UTC(),
	}
	if entity.Status == "" {
		entity.Status = StatusUnknown
	}
	if entity.VerificationStatus == "" {
		entity.VerificationStatus = VerificationUnverified
	}
	if entity.Sensitivity == "" {
		entity.Sensitivity = SensitivityInternal
	}
	for _, source := range entity.Provenance {
		entity.LocalOnly = entity.LocalOnly || source.LocalOnly
	}
	if err := validateEntityShape(entity, now.UTC()); err != nil {
		return Entity{}, err
	}
	var err error
	entity.ProvenanceDigest, err = provenanceDigest(entity.Provenance)
	if err != nil {
		return Entity{}, err
	}
	entity.EntityDigest, err = entityDigest(entity)
	if err != nil {
		return Entity{}, err
	}
	entity.ID = "life-entity-" + entity.EntityDigest
	return entity, nil
}

func normalizeRelationRequest(request RecordRelationRequest, now time.Time) (Relation, error) {
	relation := Relation{
		OwnerIdentity: compact(request.OwnerIdentity), Type: request.Type,
		FromEntityID: compact(request.FromEntityID), ToEntityID: compact(request.ToEntityID),
		Summary: compact(request.Summary), Attributes: normalizeAttributes(request.Attributes),
		ValidFrom: request.ValidFrom.UTC(), ValidUntil: cloneTime(request.ValidUntil), ObservedAt: request.ObservedAt.UTC(),
		Confidence: request.Confidence, VerificationStatus: request.VerificationStatus,
		Provenance: normalizeProvenance(request.Provenance), Sensitivity: request.Sensitivity,
		LocalOnly: request.LocalOnly, CreatedAt: now.UTC(),
	}
	if relation.VerificationStatus == "" {
		relation.VerificationStatus = VerificationUnverified
	}
	if relation.Sensitivity == "" {
		relation.Sensitivity = SensitivityInternal
	}
	for _, source := range relation.Provenance {
		relation.LocalOnly = relation.LocalOnly || source.LocalOnly
	}
	if err := validateRelationShape(relation, now.UTC()); err != nil {
		return Relation{}, err
	}
	var err error
	relation.ProvenanceDigest, err = provenanceDigest(relation.Provenance)
	if err != nil {
		return Relation{}, err
	}
	relation.RelationDigest, err = relationDigest(relation)
	if err != nil {
		return Relation{}, err
	}
	relation.ID = "life-relation-" + relation.RelationDigest
	return relation, nil
}

func validateEntityShape(entity Entity, now time.Time) error {
	if err := validateOwner(entity.OwnerIdentity); err != nil {
		return err
	}
	if _, ok := validEntityTypes[entity.Type]; !ok {
		return fmt.Errorf("invalid entity type %q", entity.Type)
	}
	if _, ok := validDomains[entity.Domain]; !ok {
		return fmt.Errorf("invalid life domain %q", entity.Domain)
	}
	if entity.Name == "" || len(entity.Name) > 256 || len(entity.Summary) > 2048 {
		return fmt.Errorf("entity name is required and text must be bounded")
	}
	if len(entity.ExternalKeys) > 16 || len(entity.Attributes) > 32 {
		return fmt.Errorf("entity metadata exceeds bounds")
	}
	if err := validateExternalKeys(entity.ExternalKeys); err != nil {
		return err
	}
	if _, ok := validStatuses[entity.Status]; !ok {
		return fmt.Errorf("invalid lifecycle status %q", entity.Status)
	}
	if entity.Priority < 0 || entity.Priority > 100 {
		return fmt.Errorf("priority must be between 0 and 100")
	}
	if err := validateEvidenceEnvelope(entity.Confidence, entity.VerificationStatus, entity.Sensitivity, entity.ValidFrom, entity.ValidUntil, entity.ObservedAt, entity.Provenance, now); err != nil {
		return err
	}
	if entity.DueAt != nil && entity.DueAt.IsZero() {
		return fmt.Errorf("dueAt cannot be zero")
	}
	if err := rejectSecret("entity name", entity.Name); err != nil {
		return err
	}
	if err := rejectSecret("entity summary", entity.Summary); err != nil {
		return err
	}
	return validateAttributes(entity.Attributes)
}

func validateRelationShape(relation Relation, now time.Time) error {
	if err := validateOwner(relation.OwnerIdentity); err != nil {
		return err
	}
	if _, ok := validRelationTypes[relation.Type]; !ok {
		return fmt.Errorf("invalid relation type %q", relation.Type)
	}
	if !validEntityID(relation.FromEntityID) || !validEntityID(relation.ToEntityID) || relation.FromEntityID == relation.ToEntityID {
		return fmt.Errorf("relation requires two distinct canonical entity ids")
	}
	if len(relation.Summary) > 1024 || len(relation.Attributes) > 16 {
		return fmt.Errorf("relation metadata exceeds bounds")
	}
	if err := validateEvidenceEnvelope(relation.Confidence, relation.VerificationStatus, relation.Sensitivity, relation.ValidFrom, relation.ValidUntil, relation.ObservedAt, relation.Provenance, now); err != nil {
		return err
	}
	if err := rejectSecret("relation summary", relation.Summary); err != nil {
		return err
	}
	return validateAttributes(relation.Attributes)
}

func validateEvidenceEnvelope(confidence float64, verification VerificationStatus, sensitivity Sensitivity, validFrom time.Time, validUntil *time.Time, observedAt time.Time, provenance []Provenance, now time.Time) error {
	if !validUnitConfidence(confidence) {
		return fmt.Errorf("confidence must be between 0 and 1")
	}
	if _, ok := validVerificationStatuses[verification]; !ok {
		return fmt.Errorf("invalid verification status %q", verification)
	}
	if _, ok := validSensitivities[sensitivity]; !ok {
		return fmt.Errorf("invalid sensitivity %q", sensitivity)
	}
	if validFrom.IsZero() || observedAt.IsZero() {
		return fmt.Errorf("validFrom and observedAt are required")
	}
	if observedAt.After(now) {
		return fmt.Errorf("observedAt cannot be in the future")
	}
	if validUntil != nil && !validUntil.After(validFrom) {
		return fmt.Errorf("validUntil must be after validFrom")
	}
	if len(provenance) == 0 || len(provenance) > 16 {
		return fmt.Errorf("between 1 and 16 provenance records are required")
	}
	referenceDigests := make(map[string]string, len(provenance))
	for i, source := range provenance {
		if source.ReferenceID == "" && source.URI == "" {
			return fmt.Errorf("provenance %d requires referenceId or uri", i)
		}
		if !sha256Pattern.MatchString(source.ContentDigest) {
			return fmt.Errorf("provenance %d content digest must be a lowercase bare SHA-256", i)
		}
		if source.CapturedAt.IsZero() || source.CapturedAt.After(now) {
			return fmt.Errorf("provenance %d capturedAt is invalid", i)
		}
		if err := rejectSecret("provenance reference", source.ReferenceID+" "+source.Authority); err != nil {
			return err
		}
		reference := source.ReferenceID
		if reference == "" {
			reference = source.URI
		}
		if previous, exists := referenceDigests[reference]; exists && previous != source.ContentDigest {
			return fmt.Errorf("provenance reference %q has conflicting content digests", reference)
		}
		referenceDigests[reference] = source.ContentDigest
	}
	return nil
}

func validateStoredEntity(entity Entity) error {
	if entity.CreatedAt.IsZero() || !validEntityID(entity.ID) || !sha256Pattern.MatchString(entity.ProvenanceDigest) || !sha256Pattern.MatchString(entity.EntityDigest) {
		return fmt.Errorf("%w: invalid entity identity", ErrCorruptStorage)
	}
	if err := validateEntityShape(entity, entity.CreatedAt); err != nil {
		return fmt.Errorf("%w: %v", ErrCorruptStorage, err)
	}
	p, _ := provenanceDigest(entity.Provenance)
	d, _ := entityDigest(entity)
	if p != entity.ProvenanceDigest {
		return fmt.Errorf("%w: entity provenance digest mismatch", ErrCorruptStorage)
	}
	if d != entity.EntityDigest || entity.ID != "life-entity-"+d {
		return fmt.Errorf("%w: entity envelope digest mismatch (digestEqual=%t, idEqual=%t)", ErrCorruptStorage, d == entity.EntityDigest, entity.ID == "life-entity-"+d)
	}
	return nil
}

func validateStoredRelation(relation Relation) error {
	if relation.CreatedAt.IsZero() || !validRelationID(relation.ID) || !sha256Pattern.MatchString(relation.ProvenanceDigest) || !sha256Pattern.MatchString(relation.RelationDigest) {
		return fmt.Errorf("%w: invalid relation identity", ErrCorruptStorage)
	}
	if err := validateRelationShape(relation, relation.CreatedAt); err != nil {
		return fmt.Errorf("%w: %v", ErrCorruptStorage, err)
	}
	p, _ := provenanceDigest(relation.Provenance)
	d, _ := relationDigest(relation)
	if p != relation.ProvenanceDigest {
		return fmt.Errorf("%w: relation provenance digest mismatch", ErrCorruptStorage)
	}
	if d != relation.RelationDigest || relation.ID != "life-relation-"+d {
		return fmt.Errorf("%w: relation envelope digest mismatch", ErrCorruptStorage)
	}
	return nil
}

func provenanceDigest(values []Provenance) (string, error) {
	return hashCanonical(normalizeProvenance(values))
}

func entityDigest(entity Entity) (string, error) {
	return hashCanonical(canonicalEntity{SchemaVersion: SchemaVersion, OwnerIdentity: entity.OwnerIdentity, Type: entity.Type, Domain: entity.Domain, Name: entity.Name, Summary: entity.Summary, ExternalKeys: entity.ExternalKeys, Attributes: entity.Attributes, Status: entity.Status, Priority: entity.Priority, DueAt: formatTime(entity.DueAt), ValidFrom: entity.ValidFrom.UTC().Format(time.RFC3339Nano), ValidUntil: formatTime(entity.ValidUntil), ObservedAt: entity.ObservedAt.UTC().Format(time.RFC3339Nano), Confidence: entity.Confidence, VerificationStatus: entity.VerificationStatus, ProvenanceDigest: entity.ProvenanceDigest, Sensitivity: entity.Sensitivity, LocalOnly: entity.LocalOnly})
}

func relationDigest(relation Relation) (string, error) {
	return hashCanonical(canonicalRelation{SchemaVersion: SchemaVersion, OwnerIdentity: relation.OwnerIdentity, Type: relation.Type, FromEntityID: relation.FromEntityID, ToEntityID: relation.ToEntityID, Summary: relation.Summary, Attributes: relation.Attributes, ValidFrom: relation.ValidFrom.UTC().Format(time.RFC3339Nano), ValidUntil: formatTime(relation.ValidUntil), ObservedAt: relation.ObservedAt.UTC().Format(time.RFC3339Nano), Confidence: relation.Confidence, VerificationStatus: relation.VerificationStatus, ProvenanceDigest: relation.ProvenanceDigest, Sensitivity: relation.Sensitivity, LocalOnly: relation.LocalOnly})
}

func hashCanonical(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

func normalizeProvenance(values []Provenance) []Provenance {
	byKey := make(map[string]Provenance)
	for _, source := range values {
		source.ReferenceID = compact(source.ReferenceID)
		source.URI = strings.TrimSpace(safety.RedactURL(source.URI))
		source.ContentDigest = strings.TrimSpace(source.ContentDigest)
		source.Authority = compact(source.Authority)
		source.CapturedAt = source.CapturedAt.UTC()
		key := strings.Join([]string{source.ReferenceID, source.URI, source.ContentDigest, source.Authority, source.CapturedAt.Format(time.RFC3339Nano), fmt.Sprint(source.LocalOnly)}, "|")
		byKey[key] = source
	}
	result := make([]Provenance, 0, len(byKey))
	for _, source := range byKey {
		result = append(result, source)
	}
	sort.Slice(result, func(i, j int) bool { return provenanceKey(result[i]) < provenanceKey(result[j]) })
	return result
}

func normalizeExternalKeys(values []ExternalKey) []ExternalKey {
	set := make(map[string]ExternalKey)
	for _, key := range values {
		key.Namespace = strings.ToLower(compact(key.Namespace))
		key.Value = compact(key.Value)
		if key.Namespace != "" || key.Value != "" {
			set[key.Namespace+"\x00"+key.Value] = key
		}
	}
	if len(set) == 0 {
		return nil
	}
	result := make([]ExternalKey, 0, len(set))
	for _, key := range set {
		result = append(result, key)
	}
	sort.Slice(result, func(i, j int) bool { return externalKeyID(result[i]) < externalKeyID(result[j]) })
	return result
}

func normalizeAttributes(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	result := make(map[string]string, len(values))
	for key, value := range values {
		key = strings.ToLower(compact(key))
		if key != "" {
			result[key] = compact(value)
		}
	}
	return result
}

func validateAttributes(values map[string]string) error {
	for key, value := range values {
		if len(key) > 64 || len(value) > 1024 {
			return fmt.Errorf("attribute exceeds bounded length")
		}
		if err := rejectSecret("attribute", key+"="+value); err != nil {
			return err
		}
	}
	return nil
}

func validateExternalKeys(values []ExternalKey) error {
	if len(values) > 16 {
		return fmt.Errorf("external keys exceed limit of 16")
	}
	for _, key := range values {
		if key.Namespace == "" || key.Value == "" || len(key.Namespace) > 64 || len(key.Value) > 256 {
			return fmt.Errorf("external key is invalid or exceeds bounds")
		}
		if err := rejectSecret("external key", key.Namespace+"="+key.Value); err != nil {
			return err
		}
	}
	return nil
}

func validateOwner(owner string) error {
	if owner == "" || len(owner) > 256 {
		return fmt.Errorf("owner identity is required and must be bounded")
	}
	return rejectSecret("owner identity", owner)
}

func rejectSecret(label, value string) error {
	if safety.RedactSecrets(value) != value {
		return fmt.Errorf("%s contains secret material", label)
	}
	return nil
}
func compact(value string) string          { return strings.Join(strings.Fields(strings.TrimSpace(value)), " ") }
func normalized(value string) string       { return strings.ToLower(compact(value)) }
func externalKeyID(key ExternalKey) string { return key.Namespace + "\x00" + key.Value }
func provenanceKey(source Provenance) string {
	return strings.Join([]string{source.ReferenceID, source.URI, source.ContentDigest, source.Authority, source.CapturedAt.Format(time.RFC3339Nano), fmt.Sprint(source.LocalOnly)}, "|")
}
func formatTime(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}
func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	cloned := value.UTC()
	return &cloned
}
func validEntityID(value string) bool {
	return strings.HasPrefix(value, "life-entity-") && sha256Pattern.MatchString(strings.TrimPrefix(value, "life-entity-"))
}
func validRelationID(value string) bool {
	return strings.HasPrefix(value, "life-relation-") && sha256Pattern.MatchString(strings.TrimPrefix(value, "life-relation-"))
}
func normalizedLimit(value int) (int, error) {
	if value == 0 {
		return defaultLimit, nil
	}
	if value < 1 || value > maximumLimit {
		return 0, fmt.Errorf("limit must be between 1 and %d", maximumLimit)
	}
	return value, nil
}
func activeAt(from time.Time, until *time.Time, observed time.Time, asOf time.Time) bool {
	asOf = asOf.UTC()
	return !from.After(asOf) && (until == nil || asOf.Before(until.UTC())) && !observed.After(asOf)
}
