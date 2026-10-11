package models

import (
	"time"

	"github.com/google/uuid"
)

// LLMProviderProbe stores redacted provider-readiness evidence. It contains no
// provider credential or raw response payload, only the operator-safe result
// needed to distinguish configured endpoints from recently verified ones.
type LLMProviderProbe struct {
	ID               uuid.UUID  `gorm:"type:uuid;primary_key;default:uuid_generate_v4()" json:"id,omitempty"`
	ProviderID       string     `gorm:"type:varchar(120);index;not null" json:"providerId"`
	ProviderName     string     `gorm:"type:varchar(255);not null" json:"providerName"`
	Status           string     `gorm:"type:varchar(80);index;not null" json:"status"`
	Reason           string     `gorm:"type:text" json:"reason,omitempty"`
	EndpointURL      string     `gorm:"type:varchar(1024)" json:"endpointUrl,omitempty"`
	HTTPStatus       int        `json:"httpStatus,omitempty"`
	ModelsSeen       int        `json:"modelsSeen"`
	DurationMs       int64      `json:"durationMs"`
	Live             bool       `gorm:"index" json:"live"`
	RequiresReview   bool       `gorm:"index" json:"requiresReview"`
	CheckedAt        time.Time  `gorm:"index;not null" json:"checkedAt"`
	LastSuccessfulAt *time.Time `gorm:"index" json:"lastSuccessfulAt,omitempty"`
	CreatedAt        time.Time  `json:"createdAt"`
}

// LLMModelMaintenance stores a redacted, per-model maintenance decision. It
// makes the 24-hour local model refresh gate durable across API restarts while
// deliberately excluding provider credentials and raw runtime responses.
type LLMModelMaintenance struct {
	ID             uuid.UUID `gorm:"type:uuid;primary_key;default:uuid_generate_v4()" json:"id,omitempty"`
	ProviderID     string    `gorm:"type:varchar(120);index;not null" json:"providerId"`
	ProviderName   string    `gorm:"type:varchar(255);not null" json:"providerName"`
	ModelID        string    `gorm:"type:varchar(255);index;not null" json:"modelId"`
	ModelName      string    `gorm:"type:varchar(255);not null" json:"modelName"`
	Status         string    `gorm:"type:varchar(80);index;not null" json:"status"`
	Reason         string    `gorm:"type:text" json:"reason,omitempty"`
	PreviousDigest string    `gorm:"type:varchar(255)" json:"previousDigest,omitempty"`
	CurrentDigest  string    `gorm:"type:varchar(255)" json:"currentDigest,omitempty"`
	// ConfigurationFingerprint binds this maintenance decision to the
	// non-secret provider/model configuration that was actually checked. It
	// prevents a 24-hour result for an old endpoint from authorizing a changed
	// runtime with the same provider and model IDs.
	ConfigurationFingerprint string    `gorm:"type:char(64);index" json:"-"`
	ConfigurationChanged     bool      `gorm:"index" json:"configurationChanged"`
	UpdateAttempted          bool      `json:"updateAttempted"`
	UpdateApplied            bool      `json:"updateApplied"`
	BlocksExecution          bool      `gorm:"index" json:"blocksExecution"`
	CheckedAt                time.Time `gorm:"index;not null" json:"checkedAt"`
	CreatedAt                time.Time `json:"createdAt"`
}

// LLMGenerationRecord is the durable, redacted ledger for a model call. It
// deliberately excludes task text, prompts, completions, provider payloads,
// credentials, and source content; it is observability evidence, not a second
// conversation or memory store.
type LLMGenerationRecord struct {
	ID               uuid.UUID `gorm:"type:uuid;primary_key;default:uuid_generate_v4()" json:"id,omitempty"`
	ProviderID       string    `gorm:"type:varchar(120);index" json:"providerId,omitempty"`
	ModelID          string    `gorm:"type:varchar(255);index" json:"modelId,omitempty"`
	ModelName        string    `gorm:"type:varchar(255)" json:"modelName,omitempty"`
	Tier             string    `gorm:"type:varchar(80);index" json:"tier,omitempty"`
	Status           string    `gorm:"type:varchar(80);index;not null" json:"status"`
	Reason           string    `gorm:"type:text" json:"reason,omitempty"`
	EstimatedCostEUR float64   `json:"estimatedCostEur"`
	InputTokens      int       `json:"inputTokens"`
	OutputTokens     int       `json:"outputTokens"`
	UsageSource      string    `gorm:"type:varchar(80)" json:"usageSource,omitempty"`
	DurationMs       int64     `json:"durationMs"`
	FallbackPathJSON string    `gorm:"type:text" json:"-"`
	LoggedAt         time.Time `gorm:"index;not null" json:"loggedAt"`
	CreatedAt        time.Time `json:"createdAt"`
}
