package pursuit

import (
	"automation-hub-backend/internal/models"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// pursuitDashboardBulkRepository is optional so alternate repositories keep
// the safe single-pursuit path. The Gorm implementation bounds dashboard query
// count independently of the number of visible pursuits.
type pursuitDashboardBulkRepository interface {
	FindVisibleLinksForPursuits(ownerIdentity string, pursuitIDs []uuid.UUID) ([]models.PursuitLink, error)
	FindActivitiesForPursuits(pursuitIDs []uuid.UUID, limitPerPursuit int) ([]models.PursuitActivity, error)
	FindTaskAttemptsForPursuits(ownerIdentity string, pursuitIDs []uuid.UUID, limitPerPursuit int) ([]models.PursuitTaskAttempt, error)
	FindRuntimeAttemptsForOwner(ownerIdentity string, automationIDs, launchIDs []uuid.UUID) ([]models.AutomationLaunchEvent, error)
	FindEvidenceProjectionForDashboard(workflowIDs, memoryIDs, sourceItemIDs, extractionIDs, verificationRunIDs []uuid.UUID) (pursuitDashboardEvidenceProjection, error)
	FindResourceProjectionForPursuits(ownerIdentity string, pursuits []models.Pursuit) (pursuitDashboardResourceProjection, error)
}

type pursuitDashboardEvidenceProjection struct {
	WorkflowEvidence     []models.WorkflowEvidenceClaim
	Memories             []models.ContextMemory
	SourceItems          []models.SourceRawItem
	SourceExtractions    []models.SourceExtraction
	VerificationClaims   []models.VerificationClaim
	VerificationEvidence []models.VerificationEvidence
}

type pursuitDashboardResourceProjection struct {
	Totals             map[uuid.UUID]PursuitResourceTotals
	ReservationTotals  map[uuid.UUID]PursuitResourceReservationTotals
	ActiveReservations map[uuid.UUID][]models.PursuitResourceReservation
}

var dashboardWorkflowEvidenceColumns = []string{"id", "workflow_id", "status", "needs_review", "created_at"}
var dashboardMemoryColumns = []string{"id"}
var dashboardSourceExtractionColumns = []string{
	"id", "source_id", "raw_item_id", "content_type", "summary", "source_uri", "source_label",
	"sensitive", "uncertain", "archived", "created_at", "updated_at",
}
var dashboardVerificationClaimColumns = []string{"id", "run_id", "status", "needs_review", "high_risk", "created_at", "updated_at"}
var dashboardVerificationEvidenceColumns = []string{"id", "run_id", "source_type", "source_uri", "used", "rejected", "created_at", "updated_at"}

// FindEvidenceProjectionForDashboard intentionally excludes large source,
// memory, extraction, claim, and evidence payloads. Dashboard derivation only
// needs counts, safety state, labels, and freshness; rich content remains on
// the separately owner-scoped detail and evidence endpoints.
func (r *GormRepository) FindEvidenceProjectionForDashboard(workflowIDs, memoryIDs, sourceItemIDs, extractionIDs, verificationRunIDs []uuid.UUID) (pursuitDashboardEvidenceProjection, error) {
	result := pursuitDashboardEvidenceProjection{
		WorkflowEvidence:     []models.WorkflowEvidenceClaim{},
		Memories:             []models.ContextMemory{},
		SourceItems:          []models.SourceRawItem{},
		SourceExtractions:    []models.SourceExtraction{},
		VerificationClaims:   []models.VerificationClaim{},
		VerificationEvidence: []models.VerificationEvidence{},
	}
	if ids := uniqueUUIDs(workflowIDs); len(ids) > 0 {
		if err := r.DB.Select(dashboardWorkflowEvidenceColumns).Where("workflow_id IN ?", ids).Order("created_at DESC").Find(&result.WorkflowEvidence).Error; err != nil {
			return result, fmt.Errorf("workflow evidence: %w", err)
		}
	}
	if ids := uniqueUUIDs(memoryIDs); len(ids) > 0 {
		if err := r.DB.Select(dashboardMemoryColumns).Where("id IN ?", ids).Order("updated_at DESC").Find(&result.Memories).Error; err != nil {
			return result, fmt.Errorf("memories: %w", err)
		}
	}
	if ids := uniqueUUIDs(sourceItemIDs); len(ids) > 0 {
		if err := r.DB.Select(pursuitSourceItemProjectionColumns).Where("id IN ?", ids).Order("updated_at DESC").Find(&result.SourceItems).Error; err != nil {
			return result, fmt.Errorf("source items: %w", err)
		}
	}
	if ids := uniqueUUIDs(extractionIDs); len(ids) > 0 {
		if err := r.DB.Select(dashboardSourceExtractionColumns).Where("id IN ?", ids).Order("updated_at DESC").Find(&result.SourceExtractions).Error; err != nil {
			return result, fmt.Errorf("source extractions: %w", err)
		}
	}
	if ids := uniqueUUIDs(verificationRunIDs); len(ids) > 0 {
		if err := r.DB.Select(dashboardVerificationClaimColumns).Where("run_id IN ?", ids).Order("created_at ASC").Find(&result.VerificationClaims).Error; err != nil {
			return result, fmt.Errorf("verification claims: %w", err)
		}
		if err := r.DB.Select(dashboardVerificationEvidenceColumns).Where("run_id IN ?", ids).Order("created_at ASC").Find(&result.VerificationEvidence).Error; err != nil {
			return result, fmt.Errorf("verification evidence: %w", err)
		}
	}
	return result, nil
}

func (r *GormRepository) FindVisibleLinksForPursuits(ownerIdentity string, pursuitIDs []uuid.UUID) ([]models.PursuitLink, error) {
	links := []models.PursuitLink{}
	if len(pursuitIDs) == 0 {
		return links, nil
	}
	if err := r.DB.Where("pursuit_id IN ?", pursuitIDs).Order("created_at DESC").Find(&links).Error; err != nil {
		return nil, err
	}
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	if ownerIdentity == "" || len(links) == 0 {
		return links, nil
	}

	idsByType := map[string][]uuid.UUID{}
	sourceKeys := []string{}
	for _, link := range links {
		key := strings.TrimSpace(link.LinkType)
		value := strings.TrimSpace(link.LinkID)
		if key == LinkSourceItem && value != "" {
			sourceKeys = append(sourceKeys, value)
		}
		if id, err := uuid.Parse(value); err == nil {
			idsByType[key] = append(idsByType[key], id)
		}
	}

	allowed := map[string]map[string]bool{}
	addAllowed := func(linkType string, ids []uuid.UUID) {
		if allowed[linkType] == nil {
			allowed[linkType] = map[string]bool{}
		}
		for _, id := range ids {
			allowed[linkType][id.String()] = true
		}
	}
	loadOwned := func(linkType string, model interface{}, exact bool) error {
		ids := uniqueUUIDs(idsByType[linkType])
		if len(ids) == 0 {
			return nil
		}
		visible := []uuid.UUID{}
		query := r.DB.Model(model).Where("id IN ?", ids)
		if exact {
			query = query.Where("owner_identity = ?", ownerIdentity)
		} else {
			query = query.Where(ownerVisibilitySQL("owner_identity", ownerIdentity), ownerIdentity)
		}
		if err := query.Pluck("id", &visible).Error; err != nil {
			return err
		}
		addAllowed(linkType, visible)
		return nil
	}
	if err := loadOwned(LinkPursuit, &models.Pursuit{}, false); err != nil {
		return nil, err
	}
	if err := loadOwned(LinkWorkflow, &models.WorkflowItem{}, false); err != nil {
		return nil, err
	}
	if err := loadOwned(LinkMemory, &models.ContextMemory{}, false); err != nil {
		return nil, err
	}
	if err := loadOwned(LinkAIConversation, &models.AIConversationArchive{}, false); err != nil {
		return nil, err
	}
	if err := loadOwned(LinkAmbientOpportunity, &models.AmbientOpportunity{}, true); err != nil {
		return nil, err
	}
	if err := loadOwned(LinkVerification, &models.VerificationRun{}, false); err != nil {
		return nil, err
	}
	if err := loadOwned(LinkAgentRuntime, &models.AutomationLaunchEvent{}, true); err != nil {
		return nil, err
	}

	if ids := uniqueUUIDs(idsByType[LinkSourceExtraction]); len(ids) > 0 {
		visible := []uuid.UUID{}
		if err := r.DB.Model(&models.SourceExtraction{}).
			Where("id IN ?", ids).
			Where("source_id IN (?)", r.visibleSourceIDs(ownerIdentity)).
			Pluck("id", &visible).Error; err != nil {
			return nil, err
		}
		addAllowed(LinkSourceExtraction, visible)
	}
	if len(sourceKeys) > 0 {
		type sourceIdentity struct {
			ID         uuid.UUID
			ExternalID string
		}
		visible := []sourceIdentity{}
		parsed := uniqueUUIDs(idsByType[LinkSourceItem])
		query := r.DB.Model(&models.SourceRawItem{}).Where("source_id IN (?)", r.visibleSourceIDs(ownerIdentity))
		if len(parsed) > 0 {
			query = query.Where("id IN ? OR external_id IN ?", parsed, sourceKeys)
		} else {
			query = query.Where("external_id IN ?", sourceKeys)
		}
		if err := query.Select("id", "external_id").Scan(&visible).Error; err != nil {
			return nil, err
		}
		allowed[LinkSourceItem] = map[string]bool{}
		for _, item := range visible {
			allowed[LinkSourceItem][item.ID.String()] = true
			allowed[LinkSourceItem][strings.TrimSpace(item.ExternalID)] = true
		}
	}

	visible := make([]models.PursuitLink, 0, len(links))
	for _, link := range links {
		typeKey := strings.TrimSpace(link.LinkType)
		linkKey := strings.TrimSpace(link.LinkID)
		normalizedKey := linkKey
		if id, err := uuid.Parse(linkKey); err == nil {
			normalizedKey = id.String()
		}
		switch typeKey {
		case LinkPursuit, LinkWorkflow, LinkMemory, LinkAIConversation, LinkAmbientOpportunity,
			LinkSourceItem, LinkSourceExtraction, LinkVerification, LinkAgentRuntime:
			if allowed[typeKey][linkKey] || allowed[typeKey][normalizedKey] {
				visible = append(visible, link)
			}
		default:
			visible = append(visible, link)
		}
	}
	return visible, nil
}

func (r *GormRepository) FindActivitiesForPursuits(pursuitIDs []uuid.UUID, limit int) ([]models.PursuitActivity, error) {
	items := []models.PursuitActivity{}
	if len(pursuitIDs) == 0 {
		return items, nil
	}
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	subquery := r.DB.Model(&models.PursuitActivity{}).
		Select("pursuit_activities.*, ROW_NUMBER() OVER (PARTITION BY pursuit_id ORDER BY created_at DESC) AS dashboard_rank").
		Where("pursuit_id IN ?", pursuitIDs)
	err := r.DB.Table("(?) AS ranked", subquery).
		Where("dashboard_rank <= ?", limit).
		Order("pursuit_id ASC, created_at DESC").
		Scan(&items).Error
	return items, err
}

func (r *GormRepository) FindTaskAttemptsForPursuits(ownerIdentity string, pursuitIDs []uuid.UUID, limit int) ([]models.PursuitTaskAttempt, error) {
	items := []models.PursuitTaskAttempt{}
	if len(pursuitIDs) == 0 {
		return items, nil
	}
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	subquery := r.DB.Model(&models.PursuitTaskAttempt{}).
		Select("pursuit_task_attempts.*, ROW_NUMBER() OVER (PARTITION BY pursuit_id ORDER BY updated_at DESC) AS dashboard_rank").
		Where("pursuit_id IN ?", pursuitIDs)
	if ownerIdentity = strings.TrimSpace(ownerIdentity); ownerIdentity != "" {
		subquery = subquery.Where("owner_identity = ?", ownerIdentity)
	}
	err := r.DB.Table("(?) AS ranked", subquery).
		Where("dashboard_rank <= ?", limit).
		Order("pursuit_id ASC, updated_at DESC").
		Scan(&items).Error
	return items, err
}

func (r *GormRepository) FindRuntimeAttemptsForOwner(ownerIdentity string, automationIDs, launchIDs []uuid.UUID) ([]models.AutomationLaunchEvent, error) {
	items := []models.AutomationLaunchEvent{}
	if len(automationIDs) == 0 && len(launchIDs) == 0 {
		return items, nil
	}
	query := r.DB.Model(&models.AutomationLaunchEvent{})
	switch {
	case len(automationIDs) > 0 && len(launchIDs) > 0:
		query = query.Where("automation_id IN ? OR id IN ?", automationIDs, launchIDs)
	case len(automationIDs) > 0:
		query = query.Where("automation_id IN ?", automationIDs)
	default:
		query = query.Where("id IN ?", launchIDs)
	}
	if ownerIdentity = strings.TrimSpace(ownerIdentity); ownerIdentity != "" {
		query = query.Where("owner_identity = ?", ownerIdentity)
	}
	err := query.Order("started_at DESC").Find(&items).Error
	return items, err
}

func (r *GormRepository) FindResourceProjectionForPursuits(ownerIdentity string, pursuits []models.Pursuit) (pursuitDashboardResourceProjection, error) {
	projection := pursuitDashboardResourceProjection{
		Totals:             map[uuid.UUID]PursuitResourceTotals{},
		ReservationTotals:  map[uuid.UUID]PursuitResourceReservationTotals{},
		ActiveReservations: map[uuid.UUID][]models.PursuitResourceReservation{},
	}
	limited := make([]models.Pursuit, 0, len(pursuits))
	for _, pursuit := range pursuits {
		if pursuit.ResourceLimits.MaxEffortHours > 0 || pursuit.ResourceLimits.MaxSpendEUR > 0 {
			limited = append(limited, pursuit)
		}
	}
	if len(limited) == 0 {
		return projection, nil
	}

	resourceScope := func(query *gorm.DB, pursuitColumn, ownerColumn string) *gorm.DB {
		ownerIdentity = strings.TrimSpace(ownerIdentity)
		if ownerIdentity != "" {
			ids := make([]uuid.UUID, 0, len(limited))
			for _, pursuit := range limited {
				ids = append(ids, pursuit.ID)
			}
			return query.Where(pursuitColumn+" IN ? AND "+ownerColumn+" = ?", ids, ownerIdentity)
		}
		conditions := []string{}
		args := []interface{}{}
		for _, pursuit := range limited {
			owner := strings.TrimSpace(pursuit.OwnerIdentity)
			if owner == "" {
				continue
			}
			conditions = append(conditions, "("+pursuitColumn+" = ? AND "+ownerColumn+" = ?)")
			args = append(args, pursuit.ID, owner)
		}
		if len(conditions) == 0 {
			return query.Where("1 = 0")
		}
		return query.Where(strings.Join(conditions, " OR "), args...)
	}

	type totalsRow struct {
		PursuitID uuid.UUID `gorm:"column:pursuit_id"`
		PursuitResourceTotals
	}
	totalRows := []totalsRow{}
	query := r.DB.Model(&models.PursuitResourceEvent{}).
		Select(`pursuit_id,
			COALESCE(SUM(CASE WHEN kind = 'effort_recorded' THEN effort_minutes ELSE 0 END), 0) AS effort_minutes,
			COALESCE(SUM(CASE WHEN kind = 'spend_incurred' THEN amount_minor ELSE 0 END), 0) AS incurred_minor,
			COALESCE(SUM(CASE WHEN kind = 'spend_refund' THEN amount_minor ELSE 0 END), 0) AS refunded_minor,
			COUNT(*) AS event_count, MAX(recorded_at) AS latest_recorded_at`).Group("pursuit_id")
	if err := resourceScope(query, "pursuit_id", "owner_identity").Scan(&totalRows).Error; err != nil {
		return projection, err
	}
	for _, row := range totalRows {
		projection.Totals[row.PursuitID] = row.PursuitResourceTotals
	}

	type reservationRow struct {
		PursuitID uuid.UUID `gorm:"column:pursuit_id"`
		PursuitResourceReservationTotals
	}
	reservationRows := []reservationRow{}
	query = r.DB.Model(&models.PursuitResourceReservation{}).Table("pursuit_resource_reservations AS r").
		Select(`r.pursuit_id,
			COALESCE(SUM(r.estimated_effort_minutes), 0) AS effort_minutes,
			COALESCE(SUM(r.estimated_cost_micros), 0) AS cost_micros,
			COUNT(*) AS reservation_count, MAX(r.reserved_at) AS latest_reserved_at`).
		Joins("LEFT JOIN pursuit_resource_reservation_settlements AS s ON s.reservation_id = r.id").
		Where("s.id IS NULL").Group("r.pursuit_id")
	if err := resourceScope(query, "r.pursuit_id", "r.owner_identity").Scan(&reservationRows).Error; err != nil {
		return projection, err
	}
	for _, row := range reservationRows {
		projection.ReservationTotals[row.PursuitID] = row.PursuitResourceReservationTotals
	}

	active := []models.PursuitResourceReservation{}
	query = r.DB.Model(&models.PursuitResourceReservation{}).Table("pursuit_resource_reservations AS r").
		Select("r.*, ROW_NUMBER() OVER (PARTITION BY r.pursuit_id ORDER BY r.reserved_at ASC, r.id ASC) AS dashboard_rank").
		Joins("LEFT JOIN pursuit_resource_reservation_settlements AS s ON s.reservation_id = r.id").
		Where("s.id IS NULL")
	query = resourceScope(query, "r.pursuit_id", "r.owner_identity")
	if err := r.DB.Table("(?) AS ranked", query).Where("dashboard_rank <= 50").Order("pursuit_id ASC, reserved_at ASC, id ASC").Scan(&active).Error; err != nil {
		return projection, err
	}
	for _, reservation := range active {
		projection.ActiveReservations[reservation.PursuitID] = append(projection.ActiveReservations[reservation.PursuitID], reservation)
	}
	return projection, nil
}
