package llm

import (
	"automation-hub-backend/internal/infra"
	"automation-hub-backend/internal/models"
	"context"
	"crypto/sha256"
	"database/sql"
	"database/sql/driver"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ProbeHistoryRepository persists redacted readiness evidence independently of
// runtime provider configuration. It deliberately has no credential fields.
type ProbeHistoryRepository interface {
	RecordProviderProbe(probe *models.LLMProviderProbe) (*models.LLMProviderProbe, error)
	FindRecentProviderProbes(limit int) ([]models.LLMProviderProbe, error)
	FindLatestProviderProbe(providerID string) (*models.LLMProviderProbe, error)
}

// ContextProbeHistoryRepository is an optional extension for implementations
// that can cancel provider-probe history reads and writes. The base interface
// remains compatible with existing repositories.
type ContextProbeHistoryRepository interface {
	RecordProviderProbeWithContext(ctx context.Context, probe *models.LLMProviderProbe) (*models.LLMProviderProbe, error)
	FindRecentProviderProbesWithContext(ctx context.Context, limit int) ([]models.LLMProviderProbe, error)
	FindLatestProviderProbeWithContext(ctx context.Context, providerID string) (*models.LLMProviderProbe, error)
}

func recordProviderProbeWithContext(ctx context.Context, repository ProbeHistoryRepository, probe *models.LLMProviderProbe) (*models.LLMProviderProbe, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var (
		record *models.LLMProviderProbe
		err    error
	)
	if contextual, ok := repository.(ContextProbeHistoryRepository); ok {
		record, err = contextual.RecordProviderProbeWithContext(ctx, probe)
	} else {
		record, err = repository.RecordProviderProbe(probe)
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return record, err
}

func findRecentProviderProbesWithContext(ctx context.Context, repository ProbeHistoryRepository, limit int) ([]models.LLMProviderProbe, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var (
		records []models.LLMProviderProbe
		err     error
	)
	if contextual, ok := repository.(ContextProbeHistoryRepository); ok {
		records, err = contextual.FindRecentProviderProbesWithContext(ctx, limit)
	} else {
		records, err = repository.FindRecentProviderProbes(limit)
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return records, err
}

func findLatestProviderProbeWithContext(ctx context.Context, repository ProbeHistoryRepository, providerID string) (*models.LLMProviderProbe, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var (
		record *models.LLMProviderProbe
		err    error
	)
	if contextual, ok := repository.(ContextProbeHistoryRepository); ok {
		record, err = contextual.FindLatestProviderProbeWithContext(ctx, providerID)
	} else {
		record, err = repository.FindLatestProviderProbe(providerID)
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return record, err
}

// ModelMaintenanceRepository keeps daily model-maintenance decisions separate
// from provider readiness probes. A model can be reachable while still needing
// a refresh, so the records must not be conflated.
type ModelMaintenanceRepository interface {
	RecordModelMaintenance(record *models.LLMModelMaintenance) (*models.LLMModelMaintenance, error)
	FindLatestModelMaintenance(providerID, modelID string) (*models.LLMModelMaintenance, error)
	FindRecentModelMaintenance(limit int) ([]models.LLMModelMaintenance, error)
}

// ContextModelMaintenanceHistoryRepository is an optional extension for
// maintenance repositories that can cancel history reads. The base interface
// remains unchanged for existing in-memory and third-party implementations.
type ContextModelMaintenanceHistoryRepository interface {
	FindLatestModelMaintenanceWithContext(ctx context.Context, providerID, modelID string) (*models.LLMModelMaintenance, error)
	FindRecentModelMaintenanceWithContext(ctx context.Context, limit int) ([]models.LLMModelMaintenance, error)
}

type ContextModelMaintenanceWriter interface {
	RecordModelMaintenanceWithContext(ctx context.Context, record *models.LLMModelMaintenance) (*models.LLMModelMaintenance, error)
}

func findLatestModelMaintenance(ctx context.Context, repository ModelMaintenanceRepository, providerID, modelID string) (*models.LLMModelMaintenance, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var (
		record *models.LLMModelMaintenance
		err    error
	)
	if contextual, ok := repository.(ContextModelMaintenanceHistoryRepository); ok {
		record, err = contextual.FindLatestModelMaintenanceWithContext(ctx, providerID, modelID)
	} else {
		return nil, fmt.Errorf("model maintenance repository does not support cancellable history reads")
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return record, err
}

func findRecentModelMaintenance(ctx context.Context, repository ModelMaintenanceRepository, limit int) ([]models.LLMModelMaintenance, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var (
		records []models.LLMModelMaintenance
		err     error
	)
	if contextual, ok := repository.(ContextModelMaintenanceHistoryRepository); ok {
		records, err = contextual.FindRecentModelMaintenanceWithContext(ctx, limit)
	} else {
		return nil, fmt.Errorf("model maintenance repository does not support cancellable history reads")
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return records, err
}

// GenerationHistoryRepository stores aggregate, redacted model-call evidence.
// It must never receive prompts, outputs, source content, or credentials.
type GenerationHistoryRepository interface {
	RecordGeneration(record *models.LLMGenerationRecord) (*models.LLMGenerationRecord, error)
	FindRecentGenerations(limit int) ([]models.LLMGenerationRecord, error)
}

// GenerationUsageHistoryRepository exposes a bounded UTC-period aggregate for
// budgets without loading or retaining generation payloads in the service.
type GenerationUsageHistoryRepository interface {
	UsageBetween(start, end time.Time) ([]GenerationUsageAggregate, error)
}

var (
	ErrPaidBudgetExceeded      = errors.New("daily paid inference budget would be exceeded")
	ErrPaidBudgetUnavailable   = errors.New("durable paid inference budget accounting is unavailable")
	ErrPaidBudgetReservationID = errors.New("paid inference budget reservation id is invalid")
	ErrPaidPricingUnavailable  = errors.New("paid model pricing is unavailable or invalid")
)

const (
	paidReservationStatus    = "reserved"
	paidFinalizationStatus   = "paid_finalization"
	paidReservationMarker    = "hai-paid-reservation:"
	paidTerminalStatusMarker = "hai-paid-status:"
)

// PaidGenerationBudgetRepository atomically reserves estimated paid usage
// before provider dispatch and appends a settlement event afterward. The
// generation ledger is immutable, so a reservation left without a settlement
// remains counted conservatively.
type PaidGenerationBudgetRepository interface {
	ReservePaidGeneration(ctx context.Context, record *models.LLMGenerationRecord, dailyLimitEUR float64) error
	FinalizePaidGeneration(record *models.LLMGenerationRecord) error
}

type GenerationUsageAggregate struct {
	ProviderID       string
	ModelID          string
	BudgetUsedEUR    float64
	InputTokensUsed  int
	OutputTokensUsed int
}

type GormProbeHistoryRepository struct {
	DB *gorm.DB
}

func NewGormProbeHistoryRepository(db *gorm.DB) ProbeHistoryRepository {
	return &GormProbeHistoryRepository{DB: db}
}

func DefaultProbeHistoryRepository() (ProbeHistoryRepository, error) {
	db, err := infra.GetDefaultDB()
	if err != nil {
		return nil, fmt.Errorf("initialize provider probe history: %w", err)
	}
	return NewGormProbeHistoryRepository(db), nil
}

func DefaultModelMaintenanceRepository() (ModelMaintenanceRepository, error) {
	db, err := infra.GetDefaultDB()
	if err != nil {
		return nil, fmt.Errorf("initialize model maintenance history: %w", err)
	}
	return NewGormModelMaintenanceRepository(db), nil
}

func DefaultGenerationHistoryRepository() (GenerationHistoryRepository, error) {
	db, err := infra.GetDefaultDB()
	if err != nil {
		return nil, fmt.Errorf("initialize generation history: %w", err)
	}
	return NewGormGenerationHistoryRepository(db), nil
}

type GormModelMaintenanceRepository struct {
	DB *gorm.DB
}

// AcquireModelMaintenanceLease serializes one provider/model maintenance pass
// across backend processes. The session lock is released automatically if its
// process or database connection exits, avoiding a stale daily-maintenance
// block after a restart.
func (r *GormModelMaintenanceRepository) AcquireModelMaintenanceLease(ctx context.Context, providerID, modelID string) (func(), bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	providerID = strings.TrimSpace(providerID)
	modelID = strings.TrimSpace(modelID)
	if providerID == "" || modelID == "" {
		return nil, false, fmt.Errorf("provider id and model id are required")
	}
	db, err := r.DB.DB()
	if err != nil {
		return nil, false, err
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, false, err
	}
	key := modelMaintenanceLeaseKey(providerID, modelID)
	var acquired bool
	if err := conn.QueryRowContext(ctx, "SELECT pg_try_advisory_lock($1)", key).Scan(&acquired); err != nil {
		// The server may have acquired the session lock before the response was
		// lost. Discard this connection rather than returning uncertain session
		// state to the shared pool.
		discardModelMaintenanceLeaseConn(conn)
		return nil, false, err
	}
	if !acquired {
		_ = conn.Close()
		return func() {}, false, nil
	}

	var once sync.Once
	release := func() {
		once.Do(func() {
			releaseModelMaintenanceLease(conn, key)
		})
	}
	return release, true, nil
}

func releaseModelMaintenanceLease(conn *sql.Conn, key int64) {
	releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var released bool
	if err := conn.QueryRowContext(releaseCtx, "SELECT pg_advisory_unlock($1)", key).Scan(&released); err != nil || !released {
		// A failed or negative unlock leaves session state uncertain. Conn.Close
		// alone can return a healthy driver connection to the pool with its
		// advisory lock still held. Mark it bad so database/sql closes the
		// underlying session instead of allowing a later borrower to inherit it.
		discardModelMaintenanceLeaseConn(conn)
		return
	}
	_ = conn.Close()
}

func discardModelMaintenanceLeaseConn(conn *sql.Conn) {
	_ = conn.Raw(func(any) error { return driver.ErrBadConn })
	_ = conn.Close()
}

func modelMaintenanceLeaseKey(providerID, modelID string) int64 {
	digest := sha256.Sum256([]byte("hai:model-maintenance:" + strings.TrimSpace(providerID) + "\x00" + strings.TrimSpace(modelID)))
	return int64(binary.BigEndian.Uint64(digest[:8]))
}

type GormGenerationHistoryRepository struct {
	DB *gorm.DB
}

func NewGormModelMaintenanceRepository(db *gorm.DB) ModelMaintenanceRepository {
	return &GormModelMaintenanceRepository{DB: db}
}

func NewGormGenerationHistoryRepository(db *gorm.DB) GenerationHistoryRepository {
	return &GormGenerationHistoryRepository{DB: db}
}

func (r *GormGenerationHistoryRepository) RecordGeneration(record *models.LLMGenerationRecord) (*models.LLMGenerationRecord, error) {
	if record == nil || strings.TrimSpace(record.Status) == "" {
		return nil, fmt.Errorf("generation status is required")
	}
	if record.LoggedAt.IsZero() {
		record.LoggedAt = time.Now().UTC()
	}
	record.LoggedAt = record.LoggedAt.UTC()
	if err := r.DB.Create(record).Error; err != nil {
		return nil, err
	}
	return record, nil
}

func (r *GormGenerationHistoryRepository) ReservePaidGeneration(ctx context.Context, record *models.LLMGenerationRecord, dailyLimitEUR float64) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if r == nil || r.DB == nil || r.DB.Dialector.Name() != "postgres" {
		return ErrPaidBudgetUnavailable
	}
	if record == nil {
		return ErrPaidBudgetUnavailable
	}
	if record.ID == uuid.Nil {
		return ErrPaidBudgetReservationID
	}
	if record.EstimatedCostEUR <= 0 || dailyLimitEUR <= 0 ||
		math.IsNaN(record.EstimatedCostEUR) || math.IsInf(record.EstimatedCostEUR, 0) ||
		math.IsNaN(dailyLimitEUR) || math.IsInf(dailyLimitEUR, 0) ||
		record.InputTokens < 0 || record.OutputTokens < 0 || record.DurationMs < 0 ||
		strings.TrimSpace(record.ProviderID) == "" || strings.TrimSpace(record.ModelID) == "" {
		return ErrPaidBudgetUnavailable
	}
	if record.LoggedAt.IsZero() {
		return fmt.Errorf("%w: reservation timestamp is required", ErrPaidBudgetUnavailable)
	}
	if record.Status != paidReservationStatus || record.UsageSource != "estimated_uncertain" {
		return fmt.Errorf("%w: reservation must be marked pending and conservatively billable", ErrPaidBudgetUnavailable)
	}
	record.LoggedAt = record.LoggedAt.UTC()
	start := utcDayStart(record.LoggedAt)
	end := start.Add(24 * time.Hour)
	lockKey := "hai:llm:paid-budget:" + start.Format("2006-01-02")
	return r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", lockKey).Error; err != nil {
			return fmt.Errorf("%w: acquire daily reservation lock: %v", ErrPaidBudgetUnavailable, err)
		}
		usage, err := paidGenerationUsageAggregates(tx, start, end)
		if err != nil {
			return fmt.Errorf("%w: read daily inference usage: %v", ErrPaidBudgetUnavailable, err)
		}
		var usedEUR float64
		for _, aggregate := range usage {
			usedEUR += aggregate.BudgetUsedEUR
		}
		if usedEUR+record.EstimatedCostEUR > dailyLimitEUR {
			return fmt.Errorf("%w: used %.6f EUR, requested reservation %.6f EUR, limit %.6f EUR", ErrPaidBudgetExceeded, usedEUR, record.EstimatedCostEUR, dailyLimitEUR)
		}
		if err := tx.Create(record).Error; err != nil {
			return fmt.Errorf("%w: persist inference reservation: %v", ErrPaidBudgetUnavailable, err)
		}
		return nil
	})
}

func (r *GormGenerationHistoryRepository) FinalizePaidGeneration(record *models.LLMGenerationRecord) error {
	if r == nil || r.DB == nil || r.DB.Dialector.Name() != "postgres" || record == nil {
		return ErrPaidBudgetUnavailable
	}
	if record.ID == uuid.Nil {
		return ErrPaidBudgetReservationID
	}
	if !paidTerminalGenerationStatus(record.Status) || !paidUsageSource(record.UsageSource) ||
		record.EstimatedCostEUR < 0 || math.IsNaN(record.EstimatedCostEUR) || math.IsInf(record.EstimatedCostEUR, 0) ||
		record.InputTokens < 0 || record.OutputTokens < 0 || record.DurationMs < 0 {
		return fmt.Errorf("%w: paid generation settlement is invalid", ErrPaidBudgetUnavailable)
	}
	return r.DB.Transaction(func(tx *gorm.DB) error {
		var reservation models.LLMGenerationRecord
		if err := tx.Where("id = ?", record.ID).Take(&reservation).Error; err != nil {
			return fmt.Errorf("finalize paid inference reservation: load reservation: %w", err)
		}
		if reservation.Status != paidReservationStatus || reservation.UsageSource != "estimated_uncertain" {
			return fmt.Errorf("finalize paid inference reservation: row is not an open reservation")
		}
		dayStart := utcDayStart(reservation.LoggedAt)
		lockKey := "hai:llm:paid-budget:" + dayStart.Format("2006-01-02")
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", lockKey).Error; err != nil {
			return fmt.Errorf("finalize paid inference reservation: acquire daily accounting lock: %w", err)
		}

		marker := paidReservationMarker + reservation.ID.String()
		var alreadyFinalized bool
		if err := tx.Raw(
			"SELECT EXISTS (SELECT 1 FROM llm_generation_records WHERE status = ? AND fallback_path_json::jsonb @> jsonb_build_array(CAST(? AS text)))",
			paidFinalizationStatus, marker,
		).Scan(&alreadyFinalized).Error; err != nil {
			return fmt.Errorf("finalize paid inference reservation: check prior settlement: %w", err)
		}
		if alreadyFinalized {
			return fmt.Errorf("finalize paid inference reservation: reservation already has a settlement")
		}

		metadata, err := json.Marshal([]string{marker, paidTerminalStatusMarker + record.Status})
		if err != nil {
			return fmt.Errorf("finalize paid inference reservation: encode settlement link: %w", err)
		}
		settlement := &models.LLMGenerationRecord{
			ID: uuid.New(), ProviderID: reservation.ProviderID, ModelID: reservation.ModelID,
			ModelName: reservation.ModelName, Tier: reservation.Tier,
			Status: paidFinalizationStatus, Reason: record.Reason,
			EstimatedCostEUR: record.EstimatedCostEUR, InputTokens: record.InputTokens,
			OutputTokens: record.OutputTokens, UsageSource: record.UsageSource,
			DurationMs: record.DurationMs, FallbackPathJSON: string(metadata),
			LoggedAt: reservation.LoggedAt.UTC(),
		}
		if err := tx.Create(settlement).Error; err != nil {
			return fmt.Errorf("finalize paid inference reservation: append immutable settlement: %w", err)
		}
		return nil
	})
}

func (r *GormGenerationHistoryRepository) FindRecentGenerations(limit int) ([]models.LLMGenerationRecord, error) {
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	var records []models.LLMGenerationRecord
	if r == nil || r.DB == nil {
		return nil, ErrPaidBudgetUnavailable
	}
	fetchLimit := limit * 4
	if fetchLimit > 400 {
		fetchLimit = 400
	}
	if err := r.DB.Order("logged_at DESC, created_at DESC, id DESC").Limit(fetchLimit).Find(&records).Error; err != nil {
		return nil, err
	}
	records = collapsePaidFinalizationEvents(records)
	if len(records) > limit {
		records = records[:limit]
	}
	return records, nil
}

func (r *GormGenerationHistoryRepository) UsageBetween(start, end time.Time) ([]GenerationUsageAggregate, error) {
	if !end.After(start) {
		return []GenerationUsageAggregate{}, nil
	}
	if r == nil || r.DB == nil {
		return nil, ErrPaidBudgetUnavailable
	}
	return paidGenerationUsageAggregates(r.DB, start, end)
}

type generationUsageRow struct {
	ProviderID       string  `gorm:"column:provider_id"`
	ModelID          string  `gorm:"column:model_id"`
	BudgetUsedEUR    float64 `gorm:"column:budget_used_eur"`
	InputTokensUsed  int64   `gorm:"column:input_tokens_used"`
	OutputTokensUsed int64   `gorm:"column:output_tokens_used"`
}

func paidGenerationUsageAggregates(db *gorm.DB, start, end time.Time) ([]GenerationUsageAggregate, error) {
	var rows []generationUsageRow
	const query = `
SELECT record.provider_id, record.model_id,
       COALESCE(SUM(CASE
           WHEN record.status = ? AND record.usage_source = 'estimated_uncertain' AND settlement.id IS NOT NULL
               THEN GREATEST(settlement.estimated_cost_eur, 0)
           ELSE GREATEST(record.estimated_cost_eur, 0)
       END), 0) AS budget_used_eur,
       COALESCE(SUM(CASE
           WHEN record.status = ? AND record.usage_source = 'estimated_uncertain' AND settlement.id IS NOT NULL
               THEN GREATEST(settlement.input_tokens, 0)
           ELSE GREATEST(record.input_tokens, 0)
       END), 0) AS input_tokens_used,
       COALESCE(SUM(CASE
           WHEN record.status = ? AND record.usage_source = 'estimated_uncertain' AND settlement.id IS NOT NULL
               THEN GREATEST(settlement.output_tokens, 0)
           ELSE GREATEST(record.output_tokens, 0)
       END), 0) AS output_tokens_used
FROM llm_generation_records AS record
LEFT JOIN LATERAL (
    SELECT event.id, event.estimated_cost_eur, event.input_tokens, event.output_tokens
    FROM llm_generation_records AS event
    WHERE event.status = ?
      AND event.fallback_path_json::jsonb @> jsonb_build_array('hai-paid-reservation:' || record.id::text)
    LIMIT 1
) AS settlement ON TRUE
WHERE record.logged_at >= ? AND record.logged_at < ?
  AND record.usage_source IN ('provider_reported', 'provider_reported_partial', 'estimated', 'estimated_uncertain')
  AND record.status <> ?
GROUP BY record.provider_id, record.model_id`
	if err := db.Raw(
		query,
		paidReservationStatus, paidReservationStatus, paidReservationStatus,
		paidFinalizationStatus, start.UTC(), end.UTC(), paidFinalizationStatus,
	).Scan(&rows).Error; err != nil {
		return nil, err
	}
	aggregates := make([]GenerationUsageAggregate, 0, len(rows))
	for _, row := range rows {
		aggregates = append(aggregates, GenerationUsageAggregate{
			ProviderID: row.ProviderID, ModelID: row.ModelID,
			BudgetUsedEUR:   row.BudgetUsedEUR,
			InputTokensUsed: int(row.InputTokensUsed), OutputTokensUsed: int(row.OutputTokensUsed),
		})
	}
	return aggregates, nil
}

func collapsePaidFinalizationEvents(records []models.LLMGenerationRecord) []models.LLMGenerationRecord {
	type settlement struct {
		record models.LLMGenerationRecord
		status string
	}
	settlements := make(map[uuid.UUID]settlement)
	for _, record := range records {
		if record.Status != paidFinalizationStatus {
			continue
		}
		var markers []string
		if err := json.Unmarshal([]byte(record.FallbackPathJSON), &markers); err != nil {
			continue
		}
		var reservationID uuid.UUID
		var terminalStatus string
		for _, marker := range markers {
			switch {
			case strings.HasPrefix(marker, paidReservationMarker):
				parsed, err := uuid.Parse(strings.TrimPrefix(marker, paidReservationMarker))
				if err == nil {
					reservationID = parsed
				}
			case strings.HasPrefix(marker, paidTerminalStatusMarker):
				terminalStatus = strings.TrimPrefix(marker, paidTerminalStatusMarker)
			}
		}
		if reservationID != uuid.Nil && paidTerminalGenerationStatus(terminalStatus) {
			settlements[reservationID] = settlement{record: record, status: terminalStatus}
		}
	}

	collapsed := make([]models.LLMGenerationRecord, 0, len(records))
	for _, record := range records {
		if record.Status == paidFinalizationStatus {
			continue
		}
		if finalized, ok := settlements[record.ID]; ok {
			record.Status = finalized.status
			record.Reason = finalized.record.Reason
			record.EstimatedCostEUR = finalized.record.EstimatedCostEUR
			record.InputTokens = finalized.record.InputTokens
			record.OutputTokens = finalized.record.OutputTokens
			record.UsageSource = finalized.record.UsageSource
			record.DurationMs = finalized.record.DurationMs
			record.CreatedAt = finalized.record.CreatedAt
		}
		collapsed = append(collapsed, record)
	}
	return collapsed
}

func paidUsageSource(source string) bool {
	switch source {
	case "provider_reported", "provider_reported_partial", "estimated", "estimated_uncertain":
		return true
	default:
		return false
	}
}

func paidTerminalGenerationStatus(status string) bool {
	switch status {
	case "completed", "failed", "cancelled":
		return true
	default:
		return false
	}
}

func (r *GormModelMaintenanceRepository) RecordModelMaintenance(record *models.LLMModelMaintenance) (*models.LLMModelMaintenance, error) {
	return r.RecordModelMaintenanceWithContext(context.Background(), record)
}

func (r *GormModelMaintenanceRepository) RecordModelMaintenanceWithContext(ctx context.Context, record *models.LLMModelMaintenance) (*models.LLMModelMaintenance, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if record == nil {
		return nil, fmt.Errorf("model maintenance record is required")
	}
	if strings.TrimSpace(record.ProviderID) == "" || strings.TrimSpace(record.ModelID) == "" {
		return nil, fmt.Errorf("provider id and model id are required")
	}
	if record.CheckedAt.IsZero() {
		record.CheckedAt = time.Now().UTC()
	}
	record.CheckedAt = record.CheckedAt.UTC()
	if err := r.DB.WithContext(ctx).Create(record).Error; err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return record, nil
}

func (r *GormModelMaintenanceRepository) FindLatestModelMaintenance(providerID, modelID string) (*models.LLMModelMaintenance, error) {
	return r.FindLatestModelMaintenanceWithContext(context.Background(), providerID, modelID)
}

func (r *GormModelMaintenanceRepository) FindLatestModelMaintenanceWithContext(ctx context.Context, providerID, modelID string) (*models.LLMModelMaintenance, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	providerID = strings.TrimSpace(providerID)
	modelID = strings.TrimSpace(modelID)
	if providerID == "" || modelID == "" {
		return nil, fmt.Errorf("provider id and model id are required")
	}
	var record models.LLMModelMaintenance
	result := r.DB.WithContext(ctx).Where("provider_id = ? AND model_id = ?", providerID, modelID).Order("checked_at DESC, created_at DESC, id DESC").Limit(1).Find(&record)
	if result.Error != nil {
		return nil, result.Error
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}
	return &record, nil
}

func (r *GormModelMaintenanceRepository) FindRecentModelMaintenance(limit int) ([]models.LLMModelMaintenance, error) {
	return r.FindRecentModelMaintenanceWithContext(context.Background(), limit)
}

func (r *GormModelMaintenanceRepository) FindRecentModelMaintenanceWithContext(ctx context.Context, limit int) ([]models.LLMModelMaintenance, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	var records []models.LLMModelMaintenance
	if err := r.DB.WithContext(ctx).Order("checked_at DESC, created_at DESC, id DESC").Limit(limit).Find(&records).Error; err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return records, nil
}

func (r *GormProbeHistoryRepository) RecordProviderProbe(probe *models.LLMProviderProbe) (*models.LLMProviderProbe, error) {
	return r.RecordProviderProbeWithContext(context.Background(), probe)
}

func (r *GormProbeHistoryRepository) RecordProviderProbeWithContext(ctx context.Context, probe *models.LLMProviderProbe) (*models.LLMProviderProbe, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if probe == nil {
		return nil, fmt.Errorf("provider probe is required")
	}
	if strings.TrimSpace(probe.ProviderID) == "" {
		return nil, fmt.Errorf("provider id is required")
	}
	if probe.CheckedAt.IsZero() {
		probe.CheckedAt = time.Now().UTC()
	}
	probe.CheckedAt = probe.CheckedAt.UTC()
	if probe.Live {
		lastSuccess := probe.CheckedAt
		probe.LastSuccessfulAt = &lastSuccess
	} else {
		var previous models.LLMProviderProbe
		err := r.DB.WithContext(ctx).Where("provider_id = ?", probe.ProviderID).Order("checked_at DESC, created_at DESC, id DESC").First(&previous).Error
		if err == nil && previous.LastSuccessfulAt != nil {
			lastSuccess := previous.LastSuccessfulAt.UTC()
			probe.LastSuccessfulAt = &lastSuccess
		} else if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := r.DB.WithContext(ctx).Create(probe).Error; err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return probe, nil
}

func (r *GormProbeHistoryRepository) FindRecentProviderProbes(limit int) ([]models.LLMProviderProbe, error) {
	return r.FindRecentProviderProbesWithContext(context.Background(), limit)
}

func (r *GormProbeHistoryRepository) FindRecentProviderProbesWithContext(ctx context.Context, limit int) ([]models.LLMProviderProbe, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	var probes []models.LLMProviderProbe
	if err := r.DB.WithContext(ctx).Order("checked_at DESC, created_at DESC, id DESC").Limit(limit).Find(&probes).Error; err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return probes, nil
}

// FindLatestProviderProbe supplies the one readiness record that strict
// routing needs. A missing record is a normal, fail-closed state rather than
// an error: the caller must not route the provider until it has been probed.
func (r *GormProbeHistoryRepository) FindLatestProviderProbe(providerID string) (*models.LLMProviderProbe, error) {
	return r.FindLatestProviderProbeWithContext(context.Background(), providerID)
}

func (r *GormProbeHistoryRepository) FindLatestProviderProbeWithContext(ctx context.Context, providerID string) (*models.LLMProviderProbe, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	providerID = strings.TrimSpace(providerID)
	if providerID == "" {
		return nil, fmt.Errorf("provider id is required")
	}
	var probe models.LLMProviderProbe
	result := r.DB.WithContext(ctx).Where("provider_id = ?", providerID).Order("checked_at DESC, created_at DESC, id DESC").Limit(1).Find(&probe)
	if result.Error != nil {
		return nil, result.Error
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}
	return &probe, nil
}
