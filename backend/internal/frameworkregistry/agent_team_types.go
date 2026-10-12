package frameworkregistry

import (
	"time"

	"automation-hub-backend/internal/agentcoordination"
)

const (
	AgentTeamDraft     = "draft"
	AgentTeamActive    = "active"
	AgentTeamSuspended = "suspended"
	AgentTeamRetired   = "retired"
	AgentTeamRevoked   = "revoked"

	TeamMemberInvited   = "invited"
	TeamMemberActive    = "active"
	TeamMemberSuspended = "suspended"
	TeamMemberLeft      = "left"
	TeamMemberRevoked   = "revoked"

	TeamRiskLow      = "low"
	TeamRiskMedium   = "medium"
	TeamRiskHigh     = "high"
	TeamRiskCritical = "critical"

	TeamConsensusUnanimous = "unanimous"
	TeamConsensusMajority  = "majority"
	TeamConsensusQuorum    = "quorum"

	TeamOutcomeReached      = "reached"
	TeamOutcomeConflicted   = "conflicted"
	TeamOutcomeEscalated    = "escalated"
	TeamOutcomeInsufficient = "insufficient_participation"

	TeamVoteSupport = "support"
	TeamVoteOppose  = "oppose"
	TeamVoteAbstain = "abstain"

	TeamEventCreated           = "team_created"
	TeamEventVersionCreated    = "version_created"
	TeamEventActivated         = "team_activated"
	TeamEventSuspended         = "team_suspended"
	TeamEventRetired           = "team_retired"
	TeamEventRevoked           = "team_revoked"
	TeamEventMemberAdded       = "member_added"
	TeamEventMembershipChanged = "membership_changed"
	TeamEventConsensusRecorded = "consensus_recorded"
)

// AgentTeamContract is durable lifecycle metadata around the canonical
// agentcoordination contracts. It bounds advisory coordination but cannot
// grant tool, approval, or execution authority.
type AgentTeamContract struct {
	ID                             string                             `json:"id"`
	Key                            string                             `json:"key"`
	Version                        string                             `json:"version"`
	Revision                       uint64                             `json:"revision"`
	Status                         string                             `json:"status"`
	Name                           string                             `json:"name"`
	Purpose                        string                             `json:"purpose"`
	AuthorityCeiling               int                                `json:"authorityCeiling"`
	RiskCeiling                    string                             `json:"riskCeiling"`
	MaximumDelegatedAuthority      int                                `json:"maximumDelegatedAuthority"`
	MaximumDelegatedRisk           string                             `json:"maximumDelegatedRisk"`
	AdvisoryOnly                   bool                               `json:"advisoryOnly"`
	GrantsExecutionAuthority       bool                               `json:"grantsExecutionAuthority"`
	ExecutionAuthorizationRequired bool                               `json:"executionAuthorizationRequired"`
	Roles                          []TeamRoleContract                 `json:"roles"`
	Capabilities                   []TeamCapabilityContract           `json:"capabilities"`
	Members                        []TeamMembership                   `json:"members"`
	CoordinationPolicy             agentcoordination.ValidationPolicy `json:"coordinationPolicy"`
	Consensus                      TeamConsensusPolicy                `json:"consensus"`
	EvidenceRefs                   []string                           `json:"evidenceRefs"`
	Provenance                     TeamProvenance                     `json:"provenance"`
	PreviousVersionDigest          string                             `json:"previousVersionDigest,omitempty"`
	ContractDigest                 string                             `json:"contractDigest"`
	CreatedAt                      time.Time                          `json:"createdAt"`
	UpdatedAt                      time.Time                          `json:"updatedAt"`
	ActivatedAt                    *time.Time                         `json:"activatedAt,omitempty"`
	SuspendedAt                    *time.Time                         `json:"suspendedAt,omitempty"`
	RetiredAt                      *time.Time                         `json:"retiredAt,omitempty"`
	RevokedAt                      *time.Time                         `json:"revokedAt,omitempty"`
	RevocationReason               string                             `json:"revocationReason,omitempty"`
}

type TeamRoleContract struct {
	ID                         string   `json:"id"`
	Name                       string   `json:"name"`
	Purpose                    string   `json:"purpose"`
	CapabilityIDs              []string `json:"capabilityIds"`
	AllowedRecommendationTypes []string `json:"allowedRecommendationTypes"`
	ProhibitedActions          []string `json:"prohibitedActions"`
	EvidenceRequirements       []string `json:"evidenceRequirements"`
	AuthorityCeiling           int      `json:"authorityCeiling"`
	RiskCeiling                string   `json:"riskCeiling"`
	MayCoordinate              bool     `json:"mayCoordinate"`
	MayVote                    bool     `json:"mayVote"`
	AdvisoryOnly               bool     `json:"advisoryOnly"`
}

type TeamCapabilityContract struct {
	ID                string   `json:"id"`
	Name              string   `json:"name"`
	Description       string   `json:"description"`
	InputSchema       string   `json:"inputSchema"`
	OutputSchema      string   `json:"outputSchema"`
	EvidenceRequired  []string `json:"evidenceRequired"`
	ProhibitedActions []string `json:"prohibitedActions"`
	AuthorityCeiling  int      `json:"authorityCeiling"`
	RiskCeiling       string   `json:"riskCeiling"`
	AdvisoryOnly      bool     `json:"advisoryOnly"`
}

type TeamMembership struct {
	ID               string     `json:"id"`
	AgentID          string     `json:"agentId"`
	AgentVersion     string     `json:"agentVersion"`
	RoleIDs          []string   `json:"roleIds"`
	CapabilityIDs    []string   `json:"capabilityIds"`
	Status           string     `json:"status"`
	AuthorityCeiling int        `json:"authorityCeiling"`
	RiskCeiling      string     `json:"riskCeiling"`
	EvidenceRefs     []string   `json:"evidenceRefs"`
	ProvenanceDigest string     `json:"provenanceDigest"`
	JoinedAt         *time.Time `json:"joinedAt,omitempty"`
	StatusChangedAt  time.Time  `json:"statusChangedAt"`
	RevokedAt        *time.Time `json:"revokedAt,omitempty"`
	RevocationReason string     `json:"revocationReason,omitempty"`
}

type TeamConsensusPolicy struct {
	Mode                       string `json:"mode"`
	DecisionPayloadSchema      string `json:"decisionPayloadSchema"`
	Quorum                     int    `json:"quorum"`
	MinimumSupport             int    `json:"minimumSupport"`
	AllowAbstention            bool   `json:"allowAbstention"`
	RequireEvidence            bool   `json:"requireEvidence"`
	ConflictEscalationRequired bool   `json:"conflictEscalationRequired"`
	TieOutcome                 string `json:"tieOutcome"`
}

type TeamProvenance struct {
	Source         string    `json:"source"`
	Reference      string    `json:"reference,omitempty"`
	AuthoredBy     string    `json:"authoredBy"`
	RegisteredBy   string    `json:"registeredBy"`
	RegisteredAt   time.Time `json:"registeredAt"`
	EvidenceDigest string    `json:"evidenceDigest"`
}

// TeamConsensusOutcome records the deterministic interpretation of canonical
// agentcoordination decision messages and conflicts. It remains advisory.
type TeamConsensusOutcome struct {
	ID                             string                       `json:"id"`
	TeamID                         string                       `json:"teamId"`
	TeamVersion                    string                       `json:"teamVersion"`
	CorrelationID                  string                       `json:"correlationId"`
	IdempotencyKey                 string                       `json:"idempotencyKey"`
	Issue                          string                       `json:"issue"`
	Mode                           string                       `json:"mode"`
	Status                         string                       `json:"status"`
	Recommendation                 string                       `json:"recommendation"`
	DecisionMessageIDs             []string                     `json:"decisionMessageIds"`
	Conflicts                      []agentcoordination.Conflict `json:"conflicts"`
	SupportCount                   int                          `json:"supportCount"`
	OpposeCount                    int                          `json:"opposeCount"`
	AbstainCount                   int                          `json:"abstainCount"`
	EvidenceRefs                   []string                     `json:"evidenceRefs"`
	ProvenanceDigest               string                       `json:"provenanceDigest"`
	OutcomeDigest                  string                       `json:"outcomeDigest"`
	AdvisoryOnly                   bool                         `json:"advisoryOnly"`
	GrantsExecutionAuthority       bool                         `json:"grantsExecutionAuthority"`
	ExecutionAuthorizationRequired bool                         `json:"executionAuthorizationRequired"`
	RecordedAt                     time.Time                    `json:"recordedAt"`
}

// TeamDelegationAssessment only states whether a canonical delegation fits
// this advisory team contract. It is never an execution authorization result.
type TeamDelegationAssessment struct {
	TeamID                         string `json:"teamId"`
	TeamVersion                    string `json:"teamVersion"`
	DelegationID                   string `json:"delegationId"`
	ContractValid                  bool   `json:"contractValid"`
	AdvisoryOnly                   bool   `json:"advisoryOnly"`
	GrantsExecutionAuthority       bool   `json:"grantsExecutionAuthority"`
	ExecutionAuthorizationRequired bool   `json:"executionAuthorizationRequired"`
	ContractDigest                 string `json:"contractDigest"`
}

const (
	TeamMessageAttentionNotRequired  = "not_required"
	TeamMessageAttentionWaiting      = "waiting"
	TeamMessageAttentionDeferred     = "deferred"
	TeamMessageAttentionAcknowledged = "acknowledged"
	TeamMessageAttentionRejected     = "rejected"
	TeamMessageAttentionOverdue      = "overdue"
	TeamMessageAttentionExpired      = "expired"
)

// TeamMessageAttention is a derived, advisory read model over immutable
// messages and append-only acknowledgments. It can request review but cannot
// send a reminder, execute work, or grant authority.
type TeamMessageAttention struct {
	MessageID                      string                            `json:"messageId"`
	CorrelationID                  string                            `json:"correlationId"`
	RecipientID                    string                            `json:"recipientId"`
	Subject                        string                            `json:"subject"`
	RequiresAcknowledgment         bool                              `json:"requiresAcknowledgment"`
	State                          string                            `json:"state"`
	Reason                         string                            `json:"reason"`
	DueAt                          *time.Time                        `json:"dueAt,omitempty"`
	ExpiresAt                      time.Time                         `json:"expiresAt"`
	LatestAcknowledgment           *agentcoordination.Acknowledgment `json:"latestAcknowledgment,omitempty"`
	HumanReviewRequired            bool                              `json:"humanReviewRequired"`
	AdvisoryOnly                   bool                              `json:"advisoryOnly"`
	GrantsExecutionAuthority       bool                              `json:"grantsExecutionAuthority"`
	ExecutionAuthorizationRequired bool                              `json:"executionAuthorizationRequired"`
}

type TeamMessageAttentionPage struct {
	GeneratedAt time.Time              `json:"generatedAt"`
	Messages    []TeamMessageAttention `json:"messages"`
}

// TeamDecisionOverview serves the inspector's full immutable messages and
// derived attention from the same bounded repository read. It is advisory
// context only and never changes coordination, approval, or execution state.
type TeamDecisionOverview struct {
	GeneratedAt time.Time                   `json:"generatedAt"`
	Messages    []agentcoordination.Message `json:"messages"`
	Attention   []TeamMessageAttention      `json:"attention"`
}

// TeamMessageAttentionIndex is the owner-scoped overview read model. It keeps
// the detailed per-team endpoint available for inspectors while letting an
// overview avoid one HTTP request and team lookup per team.
type TeamMessageAttentionIndex struct {
	GeneratedAt time.Time                    `json:"generatedAt"`
	Contracts   []AgentTeamContract          `json:"contracts"`
	Teams       []TeamMessageAttentionByTeam `json:"teams"`
}

type TeamMessageAttentionByTeam struct {
	TeamID      string                 `json:"teamId"`
	TeamVersion string                 `json:"teamVersion"`
	Messages    []TeamMessageAttention `json:"messages"`
}

// TeamLifecycleEvent is append-only and hash-linked per team version.
type TeamLifecycleEvent struct {
	Sequence            uint64    `json:"sequence"`
	ID                  string    `json:"id"`
	TeamID              string    `json:"teamId"`
	TeamVersion         string    `json:"teamVersion"`
	Revision            uint64    `json:"revision"`
	Type                string    `json:"type"`
	Actor               string    `json:"actor"`
	SubjectID           string    `json:"subjectId,omitempty"`
	Reason              string    `json:"reason"`
	EvidenceRefs        []string  `json:"evidenceRefs"`
	ProvenanceDigest    string    `json:"provenanceDigest"`
	OccurredAt          time.Time `json:"occurredAt"`
	PreviousEventDigest string    `json:"previousEventDigest,omitempty"`
	EventDigest         string    `json:"eventDigest"`
}

type CreateAgentTeamRequest struct {
	Key                       string                             `json:"key"`
	Version                   string                             `json:"version"`
	Name                      string                             `json:"name"`
	Purpose                   string                             `json:"purpose"`
	AuthorityCeiling          int                                `json:"authorityCeiling"`
	RiskCeiling               string                             `json:"riskCeiling"`
	MaximumDelegatedAuthority int                                `json:"maximumDelegatedAuthority"`
	MaximumDelegatedRisk      string                             `json:"maximumDelegatedRisk"`
	Roles                     []TeamRoleContract                 `json:"roles"`
	Capabilities              []TeamCapabilityContract           `json:"capabilities"`
	CoordinationPolicy        agentcoordination.ValidationPolicy `json:"coordinationPolicy"`
	Consensus                 TeamConsensusPolicy                `json:"consensus"`
	EvidenceRefs              []string                           `json:"evidenceRefs"`
	Provenance                TeamProvenance                     `json:"provenance"`
	Actor                     string                             `json:"actor"`
}

// CreateGuidedAgentTeamRequest is the bounded operator-facing team charter.
// The service expands it into the canonical role, capability, coordination,
// consensus, provenance, and authority contracts; callers cannot weaken those
// invariants or submit their own digests.
type CreateGuidedAgentTeamRequest struct {
	Key                       string   `json:"key"`
	Version                   string   `json:"version"`
	Name                      string   `json:"name"`
	Purpose                   string   `json:"purpose"`
	AuthorityCeiling          int      `json:"authorityCeiling"`
	RiskCeiling               string   `json:"riskCeiling"`
	MaximumDelegatedAuthority int      `json:"maximumDelegatedAuthority"`
	MaximumDelegatedRisk      string   `json:"maximumDelegatedRisk"`
	ConsensusMode             string   `json:"consensusMode"`
	Quorum                    int      `json:"quorum"`
	MinimumSupport            int      `json:"minimumSupport"`
	AllowAbstention           bool     `json:"allowAbstention"`
	EvidenceRefs              []string `json:"evidenceRefs"`
	Actor                     string   `json:"actor"`
}

// CreateTeamDecisionMessageRequest lets an operator record one bounded vote
// without manufacturing the canonical coordination envelope in the browser.
// IDs, agent references, timestamps, authority, schema, and digest are owned by
// the service.
type CreateTeamDecisionMessageRequest struct {
	SenderMembershipID     string   `json:"senderMembershipId"`
	RecipientMembershipID  string   `json:"recipientMembershipId"`
	CorrelationID          string   `json:"correlationId"`
	IdempotencyKey         string   `json:"idempotencyKey"`
	Issue                  string   `json:"issue"`
	Position               string   `json:"position"`
	Recommendation         string   `json:"recommendation"`
	EvidenceRefs           []string `json:"evidenceRefs"`
	RequiresAcknowledgment bool     `json:"requiresAcknowledgment"`
	ExpiresInMinutes       int      `json:"expiresInMinutes"`
}

// CreateTeamAcknowledgmentRequest binds an acknowledgment to the persisted
// message and its recipient. The service creates the record identity and time.
type CreateTeamAcknowledgmentRequest struct {
	Status            string `json:"status"`
	Reason            string `json:"reason"`
	RetryAfterMinutes int    `json:"retryAfterMinutes"`
	IdempotencyKey    string `json:"idempotencyKey"`
}

type CreateAgentTeamVersionRequest struct {
	PreviousVersion           string                             `json:"previousVersion"`
	Version                   string                             `json:"version"`
	Name                      string                             `json:"name"`
	Purpose                   string                             `json:"purpose"`
	AuthorityCeiling          int                                `json:"authorityCeiling"`
	RiskCeiling               string                             `json:"riskCeiling"`
	MaximumDelegatedAuthority int                                `json:"maximumDelegatedAuthority"`
	MaximumDelegatedRisk      string                             `json:"maximumDelegatedRisk"`
	Roles                     []TeamRoleContract                 `json:"roles"`
	Capabilities              []TeamCapabilityContract           `json:"capabilities"`
	CoordinationPolicy        agentcoordination.ValidationPolicy `json:"coordinationPolicy"`
	Consensus                 TeamConsensusPolicy                `json:"consensus"`
	EvidenceRefs              []string                           `json:"evidenceRefs"`
	Provenance                TeamProvenance                     `json:"provenance"`
	ExpectedPreviousDigest    string                             `json:"expectedPreviousDigest"`
	Actor                     string                             `json:"actor"`
}

type TeamTransitionRequest struct {
	ExpectedRevision uint64   `json:"expectedRevision"`
	Actor            string   `json:"actor"`
	Reason           string   `json:"reason"`
	EvidenceRefs     []string `json:"evidenceRefs"`
}

type AddTeamMemberRequest struct {
	ExpectedRevision uint64         `json:"expectedRevision"`
	Actor            string         `json:"actor"`
	Member           TeamMembership `json:"member"`
	Reason           string         `json:"reason"`
}

type ChangeTeamMembershipRequest struct {
	ExpectedRevision uint64   `json:"expectedRevision"`
	Actor            string   `json:"actor"`
	Status           string   `json:"status"`
	Reason           string   `json:"reason"`
	EvidenceRefs     []string `json:"evidenceRefs"`
}
