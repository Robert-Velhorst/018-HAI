package models

import "time"

// ModelRunTelemetry is a durable record of one observed model call (§18/§10.9).
// It is only ever written from a real call — never fabricated.
type ModelRunTelemetry struct {
	ID               string    `gorm:"type:text;primary_key" json:"id"`
	ProviderID       string    `gorm:"type:text;not null;index" json:"providerId"`
	ModelID          string    `gorm:"type:text;not null;index" json:"modelId"`
	Lane             string    `gorm:"type:text;not null;index" json:"lane"`
	OperationID      string    `gorm:"type:text;index" json:"operationId,omitempty"`
	InputTokens      int       `gorm:"not null;default:0" json:"inputTokens"`
	OutputTokens     int       `gorm:"not null;default:0" json:"outputTokens"`
	UsageSource      string    `gorm:"type:varchar(32);not null;default:'estimated'" json:"usageSource"`
	DurationMs       int64     `gorm:"not null;default:0" json:"durationMs"`
	TokensPerSecond  float64   `gorm:"not null;default:0" json:"tokensPerSecond"`
	OK               bool      `gorm:"not null;default:false" json:"ok"`
	CacheHit         bool      `gorm:"not null;default:false" json:"cacheHit"`
	ValidationStatus string    `gorm:"type:varchar(32);not null;default:'unvalidated';index" json:"validationStatus"`
	ValidationMethod string    `gorm:"type:varchar(120);not null;default:''" json:"validationMethod,omitempty"`
	EstimatedCostEUR float64   `gorm:"type:numeric(12,6);not null;default:0" json:"estimatedCostEur"`
	FallbackDepth    int       `gorm:"not null;default:0" json:"fallbackDepth"`
	CreatedAt        time.Time `gorm:"index" json:"createdAt"`
}
