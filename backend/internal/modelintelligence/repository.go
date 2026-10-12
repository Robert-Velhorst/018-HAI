package modelintelligence

import (
	"automation-hub-backend/internal/infra"
	"automation-hub-backend/internal/models"
	"fmt"
	"strings"

	"gorm.io/gorm"
)

// TelemetryRepository persists model-run telemetry durably (§18: "records
// durable model telemetry"). Telemetry survives restart.
type TelemetryRepository interface {
	Save(t ModelRunTelemetry) error
	LoadAll() ([]ModelRunTelemetry, error)
	UpdateValidation(id string, status ValidationStatus, method string) error
}

// GormTelemetryRepository is the Postgres-backed telemetry repository.
type GormTelemetryRepository struct{ DB *gorm.DB }

// NewGormTelemetryRepository builds a repository over db.
func NewGormTelemetryRepository(db *gorm.DB) *GormTelemetryRepository {
	return &GormTelemetryRepository{DB: db}
}

// DefaultTelemetryRepository builds a repository over the default DB, or returns
// nil (in-memory only) if no DB is available.
func DefaultTelemetryRepository() TelemetryRepository {
	db, err := infra.GetDefaultDB()
	if err != nil {
		return nil
	}
	return NewGormTelemetryRepository(db)
}

func (r *GormTelemetryRepository) Save(t ModelRunTelemetry) error {
	return r.DB.Create(&models.ModelRunTelemetry{
		ID:               t.ID,
		ProviderID:       t.ProviderID,
		ModelID:          t.ModelID,
		Lane:             string(t.Lane),
		OperationID:      t.OperationID,
		InputTokens:      t.InputTokens,
		OutputTokens:     t.OutputTokens,
		UsageSource:      string(normalizeTokenUsageSource(t.UsageSource)),
		DurationMs:       t.DurationMs,
		TokensPerSecond:  t.TokensPerSecond,
		OK:               t.OK,
		CacheHit:         t.CacheHit,
		ValidationStatus: string(normalizeValidationStatus(t.ValidationStatus)),
		ValidationMethod: t.ValidationMethod,
		EstimatedCostEUR: t.EstimatedCostEUR,
		FallbackDepth:    t.FallbackDepth,
		CreatedAt:        t.CreatedAt,
	}).Error
}

func (r *GormTelemetryRepository) LoadAll() ([]ModelRunTelemetry, error) {
	var rows []models.ModelRunTelemetry
	if err := r.DB.Order("created_at ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]ModelRunTelemetry, 0, len(rows))
	for _, row := range rows {
		out = append(out, ModelRunTelemetry{
			ID:               row.ID,
			ProviderID:       row.ProviderID,
			ModelID:          row.ModelID,
			Lane:             RoutingLane(row.Lane),
			OperationID:      row.OperationID,
			InputTokens:      row.InputTokens,
			OutputTokens:     row.OutputTokens,
			UsageSource:      normalizeTokenUsageSource(TokenUsageSource(row.UsageSource)),
			DurationMs:       row.DurationMs,
			TokensPerSecond:  row.TokensPerSecond,
			OK:               row.OK,
			CacheHit:         row.CacheHit,
			ValidationStatus: normalizeValidationStatus(ValidationStatus(row.ValidationStatus)),
			ValidationMethod: row.ValidationMethod,
			EstimatedCostEUR: row.EstimatedCostEUR,
			FallbackDepth:    row.FallbackDepth,
			CreatedAt:        row.CreatedAt,
		})
	}
	return out, nil
}

func (r *GormTelemetryRepository) UpdateValidation(id string, status ValidationStatus, method string) error {
	id = strings.TrimSpace(id)
	method = strings.TrimSpace(method)
	status = normalizeValidationStatus(status)
	if id == "" {
		return fmt.Errorf("model telemetry id is required")
	}
	if status == ValidationUnvalidated {
		return fmt.Errorf("an explicit evaluated validation status is required")
	}
	if len(method) == 0 || len(method) > 120 || strings.ContainsAny(method, "\r\n") {
		return fmt.Errorf("validation method must contain 1 to 120 single-line characters")
	}
	result := r.DB.Model(&models.ModelRunTelemetry{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"validation_status": string(status),
			"validation_method": method,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("model telemetry %s not found", id)
	}
	return nil
}
