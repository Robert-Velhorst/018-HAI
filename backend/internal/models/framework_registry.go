package models

import (
	"time"

	"github.com/google/uuid"
)

// FrameworkPreference is an owner-scoped override for one built-in framework.
// The catalog remains code-owned; this row stores only the operator's choices.
type FrameworkPreference struct {
	ID                   uuid.UUID `gorm:"type:uuid;primary_key;default:uuid_generate_v4()" json:"id,omitempty"`
	OwnerIdentity        string    `gorm:"type:varchar(255);not null;uniqueIndex:uq_framework_preferences_owner_framework" json:"-"`
	FrameworkID          string    `gorm:"type:varchar(160);not null;uniqueIndex:uq_framework_preferences_owner_framework" json:"frameworkId"`
	State                string    `gorm:"type:varchar(32);not null;default:'default';index" json:"state"`
	Pinned               bool      `gorm:"not null;default:false;index" json:"pinned"`
	MaximumAutonomyLevel *int      `gorm:"type:smallint" json:"maximumAutonomyLevel,omitempty"`
	AdaptationsJSON      string    `gorm:"type:jsonb;not null;default:'[]'" json:"-"`
	CreatedAt            time.Time `gorm:"not null" json:"createdAt"`
	UpdatedAt            time.Time `gorm:"not null" json:"updatedAt"`
}

func (FrameworkPreference) TableName() string { return "framework_preferences" }

// FrameworkPreferenceChange is an immutable audit event for an owner-scoped
// framework preference mutation. Database triggers prevent edits and removal.
type FrameworkPreferenceChange struct {
	ID                  uuid.UUID `gorm:"type:uuid;primaryKey;default:uuid_generate_v4()" json:"id"`
	OwnerIdentity       string    `gorm:"type:varchar(255);not null;uniqueIndex:uq_framework_preference_changes_owner_framework_sequence,priority:1;index:idx_framework_preference_changes_owner_created,priority:1" json:"-"`
	FrameworkID         string    `gorm:"type:varchar(160);not null;uniqueIndex:uq_framework_preference_changes_owner_framework_sequence,priority:2" json:"frameworkId"`
	Sequence            uint64    `gorm:"not null;uniqueIndex:uq_framework_preference_changes_owner_framework_sequence,priority:3" json:"sequence"`
	Actor               string    `gorm:"type:varchar(255);not null" json:"actor"`
	Reason              string    `gorm:"type:varchar(1024);not null" json:"reason"`
	BeforeJSON          *string   `gorm:"type:jsonb" json:"-"`
	AfterJSON           string    `gorm:"type:jsonb;not null" json:"-"`
	OccurredAt          time.Time `gorm:"not null;index:idx_framework_preference_changes_owner_created,priority:2" json:"occurredAt"`
	PreviousEventDigest string    `gorm:"type:char(64)" json:"previousEventDigest,omitempty"`
	EventDigest         string    `gorm:"type:char(64);not null" json:"eventDigest"`
}

func (FrameworkPreferenceChange) TableName() string { return "framework_preference_changes" }

// FrameworkSelectionRecord is an append-only chief-of-staff selection audit.
// It deliberately stores a compact redacted summary and request hash, never the
// raw request or credentials. PostgreSQL prevents updates and deletes.
type FrameworkSelectionRecord struct {
	ID                        uuid.UUID `gorm:"type:uuid;primary_key;default:uuid_generate_v4()" json:"id,omitempty"`
	OwnerIdentity             string    `gorm:"type:varchar(255);not null;index:idx_framework_selection_records_owner_created,priority:1" json:"-"`
	TaskPlanID                string    `gorm:"type:varchar(160);index" json:"taskPlanId,omitempty"`
	RequestHash               string    `gorm:"type:char(64);not null;index" json:"-"`
	RequestSummary            string    `gorm:"type:varchar(512);not null" json:"-"`
	CatalogVersion            string    `gorm:"type:varchar(32);not null" json:"catalogVersion"`
	CatalogDigest             string    `gorm:"type:char(64);not null;index:idx_framework_selection_records_reproducibility,priority:1" json:"catalogDigest"`
	SelectorAlgorithmVersion  string    `gorm:"type:varchar(64);not null;index:idx_framework_selection_records_reproducibility,priority:2" json:"selectorAlgorithmVersion"`
	TaskRiskLevel             *string   `gorm:"type:varchar(16)" json:"taskRiskLevel,omitempty"`
	EffectiveRiskCeiling      *string   `gorm:"type:varchar(16)" json:"effectiveRiskCeiling,omitempty"`
	EffectivePreferenceDigest string    `gorm:"type:char(64);not null;index:idx_framework_selection_records_reproducibility,priority:3" json:"effectivePreferenceDigest"`
	ConstitutionDigest        string    `gorm:"type:char(64);not null;index:idx_framework_selection_records_reproducibility,priority:4" json:"constitutionDigest"`
	LifeDomain                string    `gorm:"type:varchar(120);not null;index" json:"lifeDomain"`
	NeedOrCommitment          string    `gorm:"type:text;not null" json:"needOrCommitment"`
	SelectedJSON              string    `gorm:"type:jsonb;not null;default:'[]'" json:"-"`
	ConflictsJSON             string    `gorm:"type:jsonb;not null;default:'[]'" json:"-"`
	RequiredAgentsJSON        string    `gorm:"type:jsonb;not null;default:'[]'" json:"-"`
	MaximumAutonomyLevel      int       `gorm:"type:smallint;not null;default:0" json:"maximumAutonomyLevel"`
	AuthoritySummary          string    `gorm:"type:text;not null" json:"authoritySummary"`
	RequiresApproval          bool      `gorm:"not null;default:false;index" json:"requiresApproval"`
	ApprovalReasonsJSON       string    `gorm:"type:jsonb;not null;default:'[]'" json:"-"`
	EvidenceRequirementsJSON  string    `gorm:"type:jsonb;not null;default:'[]'" json:"-"`
	CompletionCriteriaJSON    string    `gorm:"type:jsonb;not null;default:'[]'" json:"-"`
	LearningPlanJSON          string    `gorm:"type:jsonb;not null;default:'[]'" json:"-"`
	ContextRequirementsJSON   string    `gorm:"type:jsonb;not null;default:'[]'" json:"-"`
	LifeDomainsJSON           string    `gorm:"type:jsonb;not null;default:'[]'" json:"-"`
	NeedsStateJSON            string    `gorm:"type:jsonb;not null;default:'[]'" json:"-"`
	CapacityJSON              string    `gorm:"type:jsonb;not null;default:'{}'" json:"-"`
	AgentCardsJSON            string    `gorm:"type:jsonb;not null;default:'[]'" json:"-"`
	DelegationsJSON           string    `gorm:"type:jsonb;not null;default:'[]'" json:"-"`
	CommunicationJSON         string    `gorm:"type:jsonb;not null;default:'{}'" json:"-"`
	CoordinationJSON          string    `gorm:"type:jsonb;not null;default:'{}'" json:"-"`
	ActionAutonomyJSON        string    `gorm:"type:jsonb;not null;default:'[]'" json:"-"`
	StopConditionsJSON        string    `gorm:"type:jsonb;not null;default:'[]'" json:"-"`
	OutcomeMonitoringJSON     string    `gorm:"type:jsonb;not null;default:'[]'" json:"-"`
	ChiefOfStaffJSON          string    `gorm:"type:jsonb;not null;default:'{}'" json:"-"`
	OperatingContractDigest   string    `gorm:"type:char(64);not null;default:'0000000000000000000000000000000000000000000000000000000000000000';index" json:"operatingContractDigest"`
	SelectionReason           string    `gorm:"type:text;not null" json:"selectionReason"`
	ConstitutionVersion       int       `gorm:"not null;default:0;index" json:"constitutionVersion"`
	ConstitutionSource        string    `gorm:"type:text;not null" json:"constitutionSource"`
	CreatedAt                 time.Time `gorm:"not null;index:idx_framework_selection_records_owner_created,priority:2,sort:desc" json:"createdAt"`
}

func (FrameworkSelectionRecord) TableName() string { return "framework_selection_records" }

// RobertConstitutionVersion is one immutable-content version of an owner's
// operating constitution. Activation changes lifecycle metadata only.
type RobertConstitutionVersion struct {
	ID                      uuid.UUID  `gorm:"type:uuid;primary_key;default:uuid_generate_v4()" json:"id,omitempty"`
	OwnerIdentity           string     `gorm:"type:varchar(255);not null;uniqueIndex:uq_robert_constitution_owner_version;index:idx_robert_constitution_owner_created,priority:1" json:"-"`
	Version                 int        `gorm:"not null;uniqueIndex:uq_robert_constitution_owner_version" json:"version"`
	BaseVersion             int        `gorm:"not null;default:0" json:"baseVersion"`
	Status                  string     `gorm:"type:varchar(32);not null;default:'draft';index" json:"status"`
	ValuesJSON              string     `gorm:"type:jsonb;not null;default:'[]'" json:"-"`
	ProhibitionsJSON        string     `gorm:"type:jsonb;not null;default:'[]'" json:"-"`
	StandingPermissionsJSON string     `gorm:"type:jsonb;not null;default:'[]'" json:"-"`
	PreferencesJSON         string     `gorm:"type:jsonb;not null;default:'[]'" json:"-"`
	RelationshipRulesJSON   string     `gorm:"type:jsonb;not null;default:'[]'" json:"-"`
	FinancialBoundariesJSON string     `gorm:"type:jsonb;not null;default:'[]'" json:"-"`
	CommunicationRulesJSON  string     `gorm:"type:jsonb;not null;default:'[]'" json:"-"`
	EscalationRulesJSON     string     `gorm:"type:jsonb;not null;default:'[]'" json:"-"`
	ProtectedRulesJSON      string     `gorm:"type:jsonb;not null;default:'[]'" json:"-"`
	ChangeSummary           string     `gorm:"type:text;not null" json:"changeSummary,omitempty"`
	ApprovedBy              string     `gorm:"type:varchar(255);not null;default:''" json:"approvedBy,omitempty"`
	ApprovalNote            string     `gorm:"type:varchar(1024);not null;default:''" json:"-"`
	ApprovedAt              *time.Time `json:"approvedAt,omitempty"`
	CreatedAt               time.Time  `gorm:"not null;index:idx_robert_constitution_owner_created,priority:2,sort:desc" json:"createdAt"`
}

func (RobertConstitutionVersion) TableName() string { return "robert_constitution_versions" }
