// Package lifeontology provides an owner-scoped, source-backed read model for
// whole-life context. It is advisory only and has no execution, messaging,
// approval, or tool interfaces.
package lifeontology

import (
	"context"
	"time"
)

const SchemaVersion = "life-ontology.v1"

type EntityType string

const (
	EntityPerson      EntityType = "person"
	EntityNeed        EntityType = "need"
	EntityGoal        EntityType = "goal"
	EntityAsset       EntityType = "asset"
	EntityObligation  EntityType = "obligation"
	EntityProject     EntityType = "project"
	EntityCase        EntityType = "case"
	EntityOpportunity EntityType = "opportunity"
	EntityRisk        EntityType = "risk"
	EntitySource      EntityType = "source"
	EntityDocument    EntityType = "document"
	EntityPursuit     EntityType = "pursuit"
	EntityWorkflow    EntityType = "workflow"
	EntityTask        EntityType = "task"
	EntityMemory      EntityType = "memory"
	EntityCommitment  EntityType = "commitment"
	EntityCost        EntityType = "cost"
	EntityOutcome     EntityType = "outcome"
)

type Domain string

const (
	DomainSafetySecurity  Domain = "safety_security"
	DomainHealthWellbeing Domain = "health_wellbeing"
	DomainRelationships   Domain = "relationships_care"
	DomainHousingAssets   Domain = "housing_assets"
	DomainFinancial       Domain = "financial"
	DomainWorkVenture     Domain = "work_venture"
	DomainLearningGrowth  Domain = "learning_growth"
	DomainMeaningValues   Domain = "meaning_values"
	DomainCommunityCivic  Domain = "community_civic"
	DomainLegalGovernment Domain = "legal_government"
	DomainPersonalAdmin   Domain = "personal_administration"
)

type RelationType string

const (
	RelationHasNeed            RelationType = "has_need"
	RelationPursuesGoal        RelationType = "pursues_goal"
	RelationOwnsAsset          RelationType = "owns_asset"
	RelationOwesObligation     RelationType = "owes_obligation"
	RelationAdvances           RelationType = "advances"
	RelationBelongsToProject   RelationType = "belongs_to_project"
	RelationRelatedToCase      RelationType = "related_to_case"
	RelationCreatesOpportunity RelationType = "creates_opportunity"
	RelationThreatens          RelationType = "threatens"
	RelationMitigates          RelationType = "mitigates"
	RelationDependsOn          RelationType = "depends_on"
	RelationSupports           RelationType = "supports"
	RelationConflictsWith      RelationType = "conflicts_with"
	RelationDerivedFrom        RelationType = "derived_from"
	RelationDocuments          RelationType = "documents"
	RelationProduces           RelationType = "produces"
	RelationFulfills           RelationType = "fulfills"
	RelationAssignedTo         RelationType = "assigned_to"
	RelationRequires           RelationType = "requires"
	RelationIncursCost         RelationType = "incurs_cost"
	RelationBelongsToPursuit   RelationType = "belongs_to_pursuit"
	RelationBelongsToWorkflow  RelationType = "belongs_to_workflow"
)

type VerificationStatus string

const (
	VerificationUnverified      VerificationStatus = "unverified"
	VerificationSourceSupported VerificationStatus = "source_supported"
	VerificationSchemaValidated VerificationStatus = "schema_validated"
	VerificationHumanApproved   VerificationStatus = "human_approved"
	VerificationVerified        VerificationStatus = "verified"
	VerificationUncertain       VerificationStatus = "uncertain"
	VerificationConflicting     VerificationStatus = "conflicting"
	VerificationUnsupported     VerificationStatus = "unsupported"
	VerificationNeedsReview     VerificationStatus = "needs_review"
)

type Sensitivity string

const (
	SensitivityPublic     Sensitivity = "public"
	SensitivityInternal   Sensitivity = "internal"
	SensitivitySensitive  Sensitivity = "sensitive"
	SensitivityRestricted Sensitivity = "restricted"
)

type LifecycleStatus string

const (
	StatusOpen      LifecycleStatus = "open"
	StatusActive    LifecycleStatus = "active"
	StatusWaiting   LifecycleStatus = "waiting"
	StatusCompleted LifecycleStatus = "completed"
	StatusArchived  LifecycleStatus = "archived"
	StatusUnknown   LifecycleStatus = "unknown"
)

// ExternalKey is a stable identifier in a source namespace, such as
// trello/card or github/issue. Values are normalized but never interpreted as
// authority.
type ExternalKey struct {
	Namespace string `json:"namespace"`
	Value     string `json:"value"`
}

type Provenance struct {
	ReferenceID   string    `json:"referenceId,omitempty"`
	URI           string    `json:"uri,omitempty"`
	ContentDigest string    `json:"contentDigest"`
	Authority     string    `json:"authority,omitempty"`
	CapturedAt    time.Time `json:"capturedAt"`
	LocalOnly     bool      `json:"localOnly"`
}

// Entity is immutable source-backed whole-life context. EntityDigest signs the
// complete canonical envelope; records are never overwritten by this package.
type Entity struct {
	ID                 string             `json:"id"`
	OwnerIdentity      string             `json:"ownerIdentity"`
	Type               EntityType         `json:"type"`
	Domain             Domain             `json:"domain"`
	Name               string             `json:"name"`
	Summary            string             `json:"summary,omitempty"`
	ExternalKeys       []ExternalKey      `json:"externalKeys,omitempty"`
	Attributes         map[string]string  `json:"attributes,omitempty"`
	Status             LifecycleStatus    `json:"status"`
	Priority           int                `json:"priority"`
	DueAt              *time.Time         `json:"dueAt,omitempty"`
	ValidFrom          time.Time          `json:"validFrom"`
	ValidUntil         *time.Time         `json:"validUntil,omitempty"`
	ObservedAt         time.Time          `json:"observedAt"`
	Confidence         float64            `json:"confidence"`
	VerificationStatus VerificationStatus `json:"verificationStatus"`
	Provenance         []Provenance       `json:"provenance"`
	ProvenanceDigest   string             `json:"provenanceDigest"`
	Sensitivity        Sensitivity        `json:"sensitivity"`
	LocalOnly          bool               `json:"localOnly"`
	EntityDigest       string             `json:"entityDigest"`
	CreatedAt          time.Time          `json:"createdAt"`
}

type Relation struct {
	ID                 string             `json:"id"`
	OwnerIdentity      string             `json:"ownerIdentity"`
	Type               RelationType       `json:"type"`
	FromEntityID       string             `json:"fromEntityId"`
	ToEntityID         string             `json:"toEntityId"`
	Summary            string             `json:"summary,omitempty"`
	Attributes         map[string]string  `json:"attributes,omitempty"`
	ValidFrom          time.Time          `json:"validFrom"`
	ValidUntil         *time.Time         `json:"validUntil,omitempty"`
	ObservedAt         time.Time          `json:"observedAt"`
	Confidence         float64            `json:"confidence"`
	VerificationStatus VerificationStatus `json:"verificationStatus"`
	Provenance         []Provenance       `json:"provenance"`
	ProvenanceDigest   string             `json:"provenanceDigest"`
	Sensitivity        Sensitivity        `json:"sensitivity"`
	LocalOnly          bool               `json:"localOnly"`
	RelationDigest     string             `json:"relationDigest"`
	CreatedAt          time.Time          `json:"createdAt"`
}

type MergeMatch string

const (
	MergeExternalKey      MergeMatch = "external_key"
	MergeSemanticIdentity MergeMatch = "semantic_identity"
)

// MergeProposal is review material only. There is deliberately no Apply or
// Approve method in this package.
type MergeProposal struct {
	ID                 string     `json:"id"`
	OwnerIdentity      string     `json:"ownerIdentity"`
	CandidateEntityIDs []string   `json:"candidateEntityIds"`
	Match              MergeMatch `json:"match"`
	Reasons            []string   `json:"reasons"`
	Confidence         float64    `json:"confidence"`
	Status             string     `json:"status"`
	ProposalDigest     string     `json:"proposalDigest"`
	CreatedAt          time.Time  `json:"createdAt"`
	AdvisoryOnly       bool       `json:"advisoryOnly"`
	CanExecute         bool       `json:"canExecute"`
	GrantsAuthority    bool       `json:"grantsAuthority"`
}

const ContactReviewContractVersion = "life-contact-review.v1"

type ContactReviewSubject string

const (
	ContactReviewCandidate     ContactReviewSubject = "candidate"
	ContactReviewMergeProposal ContactReviewSubject = "merge_proposal"
)

type ContactReviewAction string

const (
	ContactReviewPromote      ContactReviewAction = "promote"
	ContactReviewCorrect      ContactReviewAction = "correct"
	ContactReviewReject       ContactReviewAction = "reject"
	ContactReviewMerge        ContactReviewAction = "merge"
	ContactReviewKeepDistinct ContactReviewAction = "keep_distinct"
)

// ContactReviewDecision is an immutable owner decision over source-derived
// person candidates. CanonicalEntityID is populated only when a new
// human-approved person record was atomically appended with the decision.
type ContactReviewDecision struct {
	ContractVersion    string               `json:"contractVersion"`
	ID                 string               `json:"id"`
	OwnerIdentity      string               `json:"ownerIdentity"`
	IdempotencyKey     string               `json:"idempotencyKey"`
	Subject            ContactReviewSubject `json:"subject"`
	SubjectID          string               `json:"subjectId"`
	Action             ContactReviewAction  `json:"action"`
	CandidateEntityIDs []string             `json:"candidateEntityIds"`
	CanonicalEntityID  string               `json:"canonicalEntityId,omitempty"`
	CanonicalName      string               `json:"canonicalName,omitempty"`
	CanonicalSummary   string               `json:"canonicalSummary,omitempty"`
	Reason             string               `json:"reason"`
	DecidedAt          time.Time            `json:"decidedAt"`
	RecordedAt         time.Time            `json:"recordedAt"`
	RequestDigest      string               `json:"requestDigest"`
	RecordDigest       string               `json:"recordDigest"`
	LocalOnly          bool                 `json:"localOnly"`
	CanExecute         bool                 `json:"canExecute"`
	GrantsAuthority    bool                 `json:"grantsAuthority"`
}

type DecideContactCandidateRequest struct {
	OwnerIdentity    string
	CandidateID      string
	Action           ContactReviewAction
	CanonicalName    string
	CanonicalSummary string
	Reason           string
	IdempotencyKey   string
}

type DecideContactMergeRequest struct {
	OwnerIdentity    string
	ProposalID       string
	Action           ContactReviewAction
	CanonicalName    string
	CanonicalSummary string
	Reason           string
	IdempotencyKey   string
}

type ContactReviewDecisionResult struct {
	Decision        ContactReviewDecision `json:"decision"`
	CanonicalEntity *Entity               `json:"canonicalEntity,omitempty"`
	AlreadyExisted  bool                  `json:"alreadyExisted"`
}

type RecordEntityRequest struct {
	OwnerIdentity      string
	Type               EntityType
	Domain             Domain
	Name               string
	Summary            string
	ExternalKeys       []ExternalKey
	Attributes         map[string]string
	Status             LifecycleStatus
	Priority           int
	DueAt              *time.Time
	ValidFrom          time.Time
	ValidUntil         *time.Time
	ObservedAt         time.Time
	Confidence         float64
	VerificationStatus VerificationStatus
	Provenance         []Provenance
	Sensitivity        Sensitivity
	LocalOnly          bool
}

type RecordRelationRequest struct {
	OwnerIdentity      string
	Type               RelationType
	FromEntityID       string
	ToEntityID         string
	Summary            string
	Attributes         map[string]string
	ValidFrom          time.Time
	ValidUntil         *time.Time
	ObservedAt         time.Time
	Confidence         float64
	VerificationStatus VerificationStatus
	Provenance         []Provenance
	Sensitivity        Sensitivity
	LocalOnly          bool
}

type EntityWriteResult struct {
	Entity         Entity          `json:"entity"`
	AlreadyExisted bool            `json:"alreadyExisted"`
	MergeProposals []MergeProposal `json:"mergeProposals,omitempty"`
}

type RelationWriteResult struct {
	Relation       Relation `json:"relation"`
	AlreadyExisted bool     `json:"alreadyExisted"`
}

type EntityQuery struct {
	Domains              []Domain
	Types                []EntityType
	Statuses             []LifecycleStatus
	VerificationStatuses []VerificationStatus
	ExternalKeys         []ExternalKey
	AsOf                 *time.Time
	ObservedBy           *time.Time
	AllowLocalOnly       bool
	Limit                int
}

type RelationQuery struct {
	Types          []RelationType
	FromEntityID   string
	ToEntityID     string
	AsOf           *time.Time
	ObservedBy     *time.Time
	AllowLocalOnly bool
	Limit          int
}

type ContextSuggestionRequest struct {
	OwnerIdentity  string
	FocusEntityID  string
	Domains        []Domain
	Types          []EntityType
	AsOf           time.Time
	AllowLocalOnly bool
	Limit          int
}

type ContextSuggestion struct {
	Entity             Entity   `json:"entity"`
	Score              int      `json:"score"`
	Reasons            []string `json:"reasons"`
	RelatedRelationIDs []string `json:"relatedRelationIds,omitempty"`
	RecommendedUse     string   `json:"recommendedUse"`
}

type ContextSuggestionResult struct {
	AsOf            time.Time           `json:"asOf"`
	Suggestions     []ContextSuggestion `json:"suggestions"`
	Truncated       bool                `json:"truncated"`
	Explanation     string              `json:"explanation"`
	DecisionDigest  string              `json:"decisionDigest"`
	AdvisoryOnly    bool                `json:"advisoryOnly"`
	CanExecute      bool                `json:"canExecute"`
	GrantsAuthority bool                `json:"grantsAuthority"`
}

type Repository interface {
	AppendEntity(context.Context, Entity) (Entity, error)
	GetEntity(context.Context, string, string) (Entity, error)
	ListEntities(context.Context, string) ([]Entity, error)
	AppendRelation(context.Context, Relation) (Relation, error)
	GetRelation(context.Context, string, string) (Relation, error)
	ListRelations(context.Context, string) ([]Relation, error)
	AppendMergeProposal(context.Context, MergeProposal) (MergeProposal, error)
	GetMergeProposal(context.Context, string, string) (MergeProposal, error)
	ListMergeProposals(context.Context, string) ([]MergeProposal, error)
	AppendContactReviewDecision(context.Context, ContactReviewDecision, *Entity) (ContactReviewDecision, error)
	GetContactReviewDecisionByIdempotency(context.Context, string, string) (ContactReviewDecision, error)
	ListContactReviewDecisions(context.Context, string, int) ([]ContactReviewDecision, error)
}

// BoundedQueryRepository lets durable stores apply owner, privacy, temporal,
// and result bounds before transferring immutable envelopes to the service.
// Returned envelopes still pass the repository's full integrity validation.
type BoundedQueryRepository interface {
	QueryEntities(context.Context, string, EntityQuery) ([]Entity, error)
	QueryRelations(context.Context, string, RelationQuery) ([]Relation, error)
	ListMergeProposalsWithLimit(context.Context, string, int) ([]MergeProposal, error)
}
