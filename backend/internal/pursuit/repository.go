package pursuit

import (
	"automation-hub-backend/internal/identity"
	"automation-hub-backend/internal/infra"
	"automation-hub-backend/internal/models"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Repository interface {
	Create(pursuit *models.Pursuit) (*models.Pursuit, error)
	Update(pursuit *models.Pursuit) (*models.Pursuit, error)
	FindByID(id uuid.UUID) (*models.Pursuit, error)
	FindAll(includeArchived bool) ([]models.Pursuit, error)
	CreateLink(link *models.PursuitLink) (*models.PursuitLink, error)
	DeleteLink(pursuitID uuid.UUID, id uuid.UUID) error
	FindLinks(pursuitID uuid.UUID) ([]models.PursuitLink, error)
	FindLink(linkType, linkID string) (*models.PursuitLink, error)
	FindLinkBySourceURI(sourceURI string) (*models.PursuitLink, error)
	FindLinkForOwner(ownerIdentity, linkType, linkID string) (*models.PursuitLink, error)
	FindLinkBySourceURIForOwner(ownerIdentity, sourceURI string) (*models.PursuitLink, error)
	CreateActivity(activity *models.PursuitActivity) (*models.PursuitActivity, error)
	FindActivities(pursuitID uuid.UUID, limit int) ([]models.PursuitActivity, error)
	FindActivityByIdentity(pursuitID uuid.UUID, eventType, sourceType, sourceID, sourceURI string) (*models.PursuitActivity, bool, error)
	UpsertTaskAttempt(attempt *models.PursuitTaskAttempt) (*models.PursuitTaskAttempt, error)
	FindTaskAttempts(pursuitID uuid.UUID, limit int) ([]models.PursuitTaskAttempt, error)
	FindLinkedWorkflows(ids []uuid.UUID) ([]models.WorkflowItem, error)
	FindLinkedChecklistItems(workflowIDs []uuid.UUID) ([]models.WorkflowChecklistItem, error)
	FindLinkedOpenLoops(workflowIDs []uuid.UUID) ([]models.WorkflowOpenLoop, error)
	FindLinkedProposals(workflowIDs []uuid.UUID) ([]models.WorkflowProposal, error)
	FindLinkedQualityGates(workflowIDs []uuid.UUID) ([]models.WorkflowQualityGate, error)
	FindLinkedDecisions(workflowIDs []uuid.UUID) ([]models.WorkflowDecision, error)
	FindLinkedTransitions(workflowIDs []uuid.UUID) ([]models.WorkflowTransition, error)
	FindLinkedSourceLinks(workflowIDs []uuid.UUID) ([]models.WorkflowSourceLink, error)
	FindLinkedEvents(workflowIDs []uuid.UUID) ([]models.WorkflowEvent, error)
	FindLinkedEvidence(workflowIDs []uuid.UUID) ([]models.WorkflowEvidenceClaim, error)
	FindLinkedMemories(ids []uuid.UUID) ([]models.ContextMemory, error)
	FindLinkedConversations(ids []uuid.UUID) ([]models.AIConversationArchive, error)
	FindLinkedAmbientOpportunities(ids []uuid.UUID) ([]models.AmbientOpportunity, error)
	FindLinkedSourceItems(ids []uuid.UUID) ([]models.SourceRawItem, error)
	FindLinkedExtractions(ids []uuid.UUID) ([]models.SourceExtraction, error)
	FindLinkedVerificationRuns(ids []uuid.UUID) ([]models.VerificationRun, error)
	FindLinkedVerificationClaims(runIDs []uuid.UUID) ([]models.VerificationClaim, error)
	FindLinkedVerificationEvidence(runIDs []uuid.UUID) ([]models.VerificationEvidence, error)
	FindLinkedAutomations(ids []uuid.UUID) ([]models.Automation, error)
	FindLinkedAutomationLaunches(automationIDs []uuid.UUID, launchIDs []uuid.UUID, limit int) ([]models.AutomationLaunchEvent, error)
}

type GormRepository struct {
	DB *gorm.DB
}

func DefaultRepository() Repository {
	db, err := infra.GetDefaultDB()
	if err != nil {
		panic(fmt.Sprintf("failed to initialize pursuit repository: %v", err))
	}
	return &GormRepository{DB: db}
}

func (r *GormRepository) Create(pursuit *models.Pursuit) (*models.Pursuit, error) {
	if err := r.DB.Create(pursuit).Error; err != nil {
		return nil, err
	}
	return pursuit, nil
}

func (r *GormRepository) Update(pursuit *models.Pursuit) (*models.Pursuit, error) {
	if err := r.DB.Save(pursuit).Error; err != nil {
		return nil, err
	}
	return pursuit, nil
}

func (r *GormRepository) FindByID(id uuid.UUID) (*models.Pursuit, error) {
	var pursuit models.Pursuit
	if err := r.DB.First(&pursuit, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &pursuit, nil
}

func (r *GormRepository) FindAll(includeArchived bool) ([]models.Pursuit, error) {
	return r.findAllForOwner("", includeArchived)
}

// FindAllForOwner enforces the ownership predicate in Postgres. Only the
// configured migration owner can additionally inspect ownerless legacy rows.
func (r *GormRepository) FindAllForOwner(ownerIdentity string, includeArchived bool) ([]models.Pursuit, error) {
	return r.findAllForOwner(ownerIdentity, includeArchived)
}

func (r *GormRepository) findAllForOwner(ownerIdentity string, includeArchived bool) ([]models.Pursuit, error) {
	var pursuits []models.Pursuit
	query := r.DB.Order("priority_score DESC, updated_at DESC")
	if !includeArchived {
		query = query.Where("archived = ?", false)
	}
	if ownerIdentity != "" {
		query = query.Where(ownerVisibilitySQL("owner_identity", ownerIdentity), ownerIdentity)
	}
	if err := query.Find(&pursuits).Error; err != nil {
		return nil, err
	}
	return pursuits, nil
}

// LinkVisibleToOwner verifies links to records that are private to a user. A
// source item inherits its owner from ConnectedSource rather than duplicating
// the identity on every raw item and extraction.
func (r *GormRepository) LinkVisibleToOwner(ownerIdentity, linkType, linkID string) (bool, bool, error) {
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	if ownerIdentity == "" {
		return false, true, nil
	}
	visibleOwner := ownerVisibilitySQL("owner_identity", ownerIdentity)

	switch strings.TrimSpace(linkType) {
	case LinkPursuit:
		id, err := uuid.Parse(strings.TrimSpace(linkID))
		if err != nil {
			return true, false, nil
		}
		var count int64
		err = r.DB.Model(&models.Pursuit{}).Where("id = ?", id).Where(visibleOwner, ownerIdentity).Count(&count).Error
		return true, count > 0, err
	case LinkWorkflow:
		id, err := uuid.Parse(strings.TrimSpace(linkID))
		if err != nil {
			return true, false, nil
		}
		var count int64
		err = r.DB.Model(&models.WorkflowItem{}).Where("id = ?", id).Where(visibleOwner, ownerIdentity).Count(&count).Error
		return true, count > 0, err
	case LinkMemory:
		id, err := uuid.Parse(strings.TrimSpace(linkID))
		if err != nil {
			return true, false, nil
		}
		var count int64
		err = r.DB.Model(&models.ContextMemory{}).Where("id = ?", id).Where(visibleOwner, ownerIdentity).Count(&count).Error
		return true, count > 0, err
	case LinkAIConversation:
		id, err := uuid.Parse(strings.TrimSpace(linkID))
		if err != nil {
			return true, false, nil
		}
		var count int64
		err = r.DB.Model(&models.AIConversationArchive{}).Where("id = ?", id).Where(visibleOwner, ownerIdentity).Count(&count).Error
		return true, count > 0, err
	case LinkAmbientOpportunity:
		id, err := uuid.Parse(strings.TrimSpace(linkID))
		if err != nil {
			return true, false, nil
		}
		var count int64
		err = r.DB.Model(&models.AmbientOpportunity{}).Where("id = ? AND owner_identity = ?", id, ownerIdentity).Count(&count).Error
		return true, count > 0, err
	case LinkSourceItem:
		return r.sourceItemVisibleToOwner(ownerIdentity, linkID)
	case LinkSourceExtraction:
		id, err := uuid.Parse(strings.TrimSpace(linkID))
		if err != nil {
			return true, false, nil
		}
		var count int64
		err = r.DB.Model(&models.SourceExtraction{}).
			Where("id = ?", id).
			Where("source_id IN (?)", r.visibleSourceIDs(ownerIdentity)).
			Count(&count).Error
		return true, count > 0, err
	case LinkVerification:
		id, err := uuid.Parse(strings.TrimSpace(linkID))
		if err != nil {
			return true, false, nil
		}
		var count int64
		err = r.DB.Model(&models.VerificationRun{}).Where("id = ?", id).Where(visibleOwner, ownerIdentity).Count(&count).Error
		return true, count > 0, err
	case LinkAgentRuntime:
		id, err := uuid.Parse(strings.TrimSpace(linkID))
		if err != nil {
			return true, false, nil
		}
		var count int64
		err = r.DB.Model(&models.AutomationLaunchEvent{}).Where("id = ? AND owner_identity = ?", id, ownerIdentity).Count(&count).Error
		return true, count > 0, err
	default:
		return false, true, nil
	}
}

func (r *GormRepository) sourceItemVisibleToOwner(ownerIdentity, linkID string) (bool, bool, error) {
	query := r.DB.Model(&models.SourceRawItem{}).Where("source_id IN (?)", r.visibleSourceIDs(ownerIdentity))
	if id, err := uuid.Parse(strings.TrimSpace(linkID)); err == nil {
		query = query.Where("id = ? OR external_id = ?", id, linkID)
	} else {
		query = query.Where("external_id = ?", linkID)
	}
	var count int64
	err := query.Count(&count).Error
	return true, count > 0, err
}

func (r *GormRepository) visibleSourceIDs(ownerIdentity string) *gorm.DB {
	return r.DB.Model(&models.ConnectedSource{}).
		Select("id").
		Where(ownerVisibilitySQL("owner_identity", ownerIdentity), ownerIdentity)
}

func (r *GormRepository) CreateLink(link *models.PursuitLink) (*models.PursuitLink, error) {
	if link.Confidence == 0 {
		link.Confidence = 0.7
	}
	var existing models.PursuitLink
	err := r.DB.Where(
		"pursuit_id = ? AND link_type = ? AND link_id = ? AND relationship = ?",
		link.PursuitID,
		link.LinkType,
		link.LinkID,
		link.Relationship,
	).First(&existing).Error
	if err == nil {
		return &existing, nil
	}
	if err != nil && err != gorm.ErrRecordNotFound {
		return nil, err
	}
	result := r.DB.Clauses(clause.OnConflict{DoNothing: true}).Create(link)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		if err := r.DB.Where(
			"pursuit_id = ? AND link_type = ? AND link_id = ? AND relationship = ?",
			link.PursuitID,
			link.LinkType,
			link.LinkID,
			link.Relationship,
		).First(&existing).Error; err != nil {
			return nil, err
		}
		return &existing, nil
	}
	if link.ID == uuid.Nil {
		return nil, fmt.Errorf("pursuit link repository created no durable identity")
	}
	return link, nil
}

func (r *GormRepository) DeleteLink(pursuitID uuid.UUID, id uuid.UUID) error {
	result := r.DB.Delete(&models.PursuitLink{}, "id = ? AND pursuit_id = ?", id, pursuitID)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("pursuit link not found")
	}
	return nil
}

func (r *GormRepository) FindLinks(pursuitID uuid.UUID) ([]models.PursuitLink, error) {
	var links []models.PursuitLink
	if err := r.DB.Where("pursuit_id = ?", pursuitID).Order("created_at DESC").Find(&links).Error; err != nil {
		return nil, err
	}
	return links, nil
}

func (r *GormRepository) FindLink(linkType, linkID string) (*models.PursuitLink, error) {
	var link models.PursuitLink
	if err := r.DB.Where("link_type = ? AND link_id = ?", linkType, linkID).Order("confidence DESC, created_at DESC").First(&link).Error; err != nil {
		return nil, err
	}
	return &link, nil
}

func (r *GormRepository) FindLinkBySourceURI(sourceURI string) (*models.PursuitLink, error) {
	var link models.PursuitLink
	if err := r.DB.Where("source_uri = ?", sourceURI).Order("confidence DESC, created_at DESC").First(&link).Error; err != nil {
		return nil, err
	}
	return &link, nil
}

// FindLinkForOwner applies pursuit visibility before selecting an exact source
// match. This avoids allowing another user's more recent or higher-confidence
// link to hide an exact link that the current user is allowed to use.
func (r *GormRepository) FindLinkForOwner(ownerIdentity, linkType, linkID string) (*models.PursuitLink, error) {
	return r.findVisibleLink(ownerIdentity, "pursuit_links.link_type = ? AND pursuit_links.link_id = ?", linkType, linkID)
}

func (r *GormRepository) FindLinkBySourceURIForOwner(ownerIdentity, sourceURI string) (*models.PursuitLink, error) {
	return r.findVisibleLink(ownerIdentity, "pursuit_links.source_uri = ?", sourceURI)
}

func (r *GormRepository) findVisibleLink(ownerIdentity, condition string, args ...interface{}) (*models.PursuitLink, error) {
	var link models.PursuitLink
	query := r.DB.Table("pursuit_links").
		Select("pursuit_links.*").
		Joins("JOIN pursuits ON pursuits.id = pursuit_links.pursuit_id").
		Where(condition, args...)
	if ownerIdentity != "" {
		query = query.Where(ownerVisibilitySQL("pursuits.owner_identity", ownerIdentity), ownerIdentity)
	}
	if err := query.Order("pursuit_links.confidence DESC, pursuit_links.created_at DESC").First(&link).Error; err != nil {
		return nil, err
	}
	return &link, nil
}

func ownerVisibilitySQL(column, ownerIdentity string) string {
	if identity.CanReadLegacyOwnerlessData(ownerIdentity) {
		return column + " = ? OR " + column + " = '' OR " + column + " IS NULL"
	}
	return column + " = ?"
}

func (r *GormRepository) CreateActivity(activity *models.PursuitActivity) (*models.PursuitActivity, error) {
	if activity.CreatedAt.IsZero() {
		activity.CreatedAt = time.Now().UTC()
	}
	if strings.TrimSpace(activity.IdempotencyKey) == "" {
		if err := r.DB.Create(activity).Error; err != nil {
			return nil, err
		}
		return activity, nil
	}
	result := r.DB.Clauses(clause.OnConflict{DoNothing: true}).Create(activity)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		var existing models.PursuitActivity
		if err := r.DB.Where(
			"pursuit_id = ? AND idempotency_key = ?",
			activity.PursuitID,
			activity.IdempotencyKey,
		).First(&existing).Error; err != nil {
			return nil, err
		}
		return &existing, nil
	}
	if activity.ID == uuid.Nil {
		return nil, fmt.Errorf("pursuit activity repository created no durable identity")
	}
	return activity, nil
}

func (r *GormRepository) FindActivities(pursuitID uuid.UUID, limit int) ([]models.PursuitActivity, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	var activity []models.PursuitActivity
	if err := r.DB.Where("pursuit_id = ?", pursuitID).Order("created_at DESC").Limit(limit).Find(&activity).Error; err != nil {
		return nil, err
	}
	return activity, nil
}

func (r *GormRepository) FindActivityByIdentity(
	pursuitID uuid.UUID,
	eventType, sourceType, sourceID, sourceURI string,
) (*models.PursuitActivity, bool, error) {
	var activity models.PursuitActivity
	err := r.DB.Where(
		"pursuit_id = ? AND event_type = ? AND source_type = ? AND source_id = ? AND source_uri = ?",
		pursuitID,
		strings.TrimSpace(eventType),
		strings.TrimSpace(sourceType),
		strings.TrimSpace(sourceID),
		strings.TrimSpace(sourceURI),
	).Order("created_at ASC").First(&activity).Error
	if err == gorm.ErrRecordNotFound {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return &activity, true, nil
}

func (r *GormRepository) UpsertTaskAttempt(attempt *models.PursuitTaskAttempt) (*models.PursuitTaskAttempt, error) {
	var existing models.PursuitTaskAttempt
	err := r.DB.Where("task_plan_id = ?", attempt.TaskPlanID).First(&existing).Error
	if err == nil {
		if existing.PursuitID != attempt.PursuitID {
			return nil, fmt.Errorf("task attempt is already linked to another pursuit")
		}
		attempt.ID = existing.ID
		attempt.CreatedAt = existing.CreatedAt
		if err := r.DB.Save(attempt).Error; err != nil {
			return nil, err
		}
		return attempt, nil
	}
	if err != gorm.ErrRecordNotFound {
		return nil, err
	}
	if err := r.DB.Create(attempt).Error; err != nil {
		return nil, err
	}
	return attempt, nil
}

func (r *GormRepository) FindTaskAttempts(pursuitID uuid.UUID, limit int) ([]models.PursuitTaskAttempt, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	var attempts []models.PursuitTaskAttempt
	if err := r.DB.Where("pursuit_id = ?", pursuitID).Order("updated_at DESC").Limit(limit).Find(&attempts).Error; err != nil {
		return nil, err
	}
	return attempts, nil
}

func (r *GormRepository) FindLinkedWorkflows(ids []uuid.UUID) ([]models.WorkflowItem, error) {
	var items []models.WorkflowItem
	if len(ids) == 0 {
		return items, nil
	}
	if err := r.DB.Where("id IN ?", ids).Order("priority_score DESC, updated_at DESC").Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

func (r *GormRepository) FindLinkedChecklistItems(workflowIDs []uuid.UUID) ([]models.WorkflowChecklistItem, error) {
	var items []models.WorkflowChecklistItem
	if len(workflowIDs) == 0 {
		return items, nil
	}
	if err := r.DB.Where("workflow_id IN ?", workflowIDs).Order("workflow_id ASC, position ASC, created_at ASC").Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

func (r *GormRepository) FindLinkedOpenLoops(workflowIDs []uuid.UUID) ([]models.WorkflowOpenLoop, error) {
	var loops []models.WorkflowOpenLoop
	if len(workflowIDs) == 0 {
		return loops, nil
	}
	if err := r.DB.Where("workflow_id IN ?", workflowIDs).Order("follow_up_at ASC NULLS LAST, updated_at DESC").Find(&loops).Error; err != nil {
		return nil, err
	}
	return loops, nil
}

func (r *GormRepository) FindLinkedProposals(workflowIDs []uuid.UUID) ([]models.WorkflowProposal, error) {
	var proposals []models.WorkflowProposal
	if len(workflowIDs) == 0 {
		return proposals, nil
	}
	if err := r.DB.Where("workflow_id IN ?", workflowIDs).Order("created_at DESC").Find(&proposals).Error; err != nil {
		return nil, err
	}
	return proposals, nil
}

func (r *GormRepository) FindLinkedQualityGates(workflowIDs []uuid.UUID) ([]models.WorkflowQualityGate, error) {
	var gates []models.WorkflowQualityGate
	if len(workflowIDs) == 0 {
		return gates, nil
	}
	if err := r.DB.Where("workflow_id IN ?", workflowIDs).Order("updated_at DESC, created_at DESC").Find(&gates).Error; err != nil {
		return nil, err
	}
	return gates, nil
}

func (r *GormRepository) FindLinkedDecisions(workflowIDs []uuid.UUID) ([]models.WorkflowDecision, error) {
	var decisions []models.WorkflowDecision
	if len(workflowIDs) == 0 {
		return decisions, nil
	}
	if err := r.DB.Where("workflow_id IN ?", workflowIDs).Order("created_at DESC").Find(&decisions).Error; err != nil {
		return nil, err
	}
	return decisions, nil
}

func (r *GormRepository) FindLinkedTransitions(workflowIDs []uuid.UUID) ([]models.WorkflowTransition, error) {
	var transitions []models.WorkflowTransition
	if len(workflowIDs) == 0 {
		return transitions, nil
	}
	if err := r.DB.Where("workflow_id IN ?", workflowIDs).Order("created_at DESC").Find(&transitions).Error; err != nil {
		return nil, err
	}
	return transitions, nil
}

func (r *GormRepository) FindLinkedSourceLinks(workflowIDs []uuid.UUID) ([]models.WorkflowSourceLink, error) {
	var links []models.WorkflowSourceLink
	if len(workflowIDs) == 0 {
		return links, nil
	}
	if err := r.DB.Where("workflow_id IN ?", workflowIDs).Order("created_at DESC").Find(&links).Error; err != nil {
		return nil, err
	}
	return links, nil
}

func (r *GormRepository) FindLinkedEvents(workflowIDs []uuid.UUID) ([]models.WorkflowEvent, error) {
	var events []models.WorkflowEvent
	if len(workflowIDs) == 0 {
		return events, nil
	}
	if err := r.DB.Where("workflow_id IN ?", workflowIDs).Order("created_at DESC").Find(&events).Error; err != nil {
		return nil, err
	}
	return events, nil
}

func (r *GormRepository) FindLinkedEvidence(workflowIDs []uuid.UUID) ([]models.WorkflowEvidenceClaim, error) {
	var claims []models.WorkflowEvidenceClaim
	if len(workflowIDs) == 0 {
		return claims, nil
	}
	if err := r.DB.Where("workflow_id IN ?", workflowIDs).Order("created_at DESC").Find(&claims).Error; err != nil {
		return nil, err
	}
	return claims, nil
}

func (r *GormRepository) FindLinkedMemories(ids []uuid.UUID) ([]models.ContextMemory, error) {
	var memories []models.ContextMemory
	if len(ids) == 0 {
		return memories, nil
	}
	if err := r.DB.Where("id IN ?", ids).Order("updated_at DESC").Find(&memories).Error; err != nil {
		return nil, err
	}
	return memories, nil
}

// FindLinkedConversations deliberately selects metadata only. Conversation
// payloads remain encrypted and are available only through the memory-engine
// conversation endpoint after its separate owner and key checks.
func (r *GormRepository) FindLinkedConversations(ids []uuid.UUID) ([]models.AIConversationArchive, error) {
	var conversations []models.AIConversationArchive
	if len(ids) == 0 {
		return conversations, nil
	}
	if err := r.DB.Select("id", "owner_identity", "platform", "external_id", "title", "source_uri", "content_hash", "revision", "message_count", "captured_at", "last_message_at", "archived", "created_at", "updated_at").
		Where("id IN ?", ids).
		Order("captured_at DESC").
		Find(&conversations).Error; err != nil {
		return nil, err
	}
	return conversations, nil
}

func (r *GormRepository) FindLinkedAmbientOpportunities(ids []uuid.UUID) ([]models.AmbientOpportunity, error) {
	var opportunities []models.AmbientOpportunity
	if len(ids) == 0 {
		return opportunities, nil
	}
	if err := r.DB.Select("id", "need_key", "title", "rationale", "next_action", "source_type", "source_uri", "priority_score", "confidence", "risk", "requires_approval", "status", "last_seen_at", "resolution_note", "created_at", "updated_at").
		Where("id IN ?", ids).
		Order("last_seen_at DESC, updated_at DESC").
		Find(&opportunities).Error; err != nil {
		return nil, err
	}
	return opportunities, nil
}

func (r *GormRepository) FindLinkedSourceItems(ids []uuid.UUID) ([]models.SourceRawItem, error) {
	var items []models.SourceRawItem
	if len(ids) == 0 {
		return items, nil
	}
	// Pursuit responses expose provenance metadata, never the duplicated raw
	// source body. Rich content stays behind the owner-scoped source endpoint.
	if err := r.DB.Select(pursuitSourceItemProjectionColumns).
		Where("id IN ?", ids).
		Order("updated_at DESC").
		Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

var pursuitSourceItemProjectionColumns = []string{
	"id", "source_id", "external_id", "project_key", "item_type", "title",
	"source_uri", "metadata", "fetched_at", "created_at", "updated_at",
}

func (r *GormRepository) FindLinkedExtractions(ids []uuid.UUID) ([]models.SourceExtraction, error) {
	var extractions []models.SourceExtraction
	if len(ids) == 0 {
		return extractions, nil
	}
	if err := r.DB.Where("id IN ?", ids).Order("updated_at DESC").Find(&extractions).Error; err != nil {
		return nil, err
	}
	return extractions, nil
}

func (r *GormRepository) FindLinkedVerificationRuns(ids []uuid.UUID) ([]models.VerificationRun, error) {
	var runs []models.VerificationRun
	if len(ids) == 0 {
		return runs, nil
	}
	if err := r.DB.Where("id IN ?", ids).Order("created_at DESC").Find(&runs).Error; err != nil {
		return nil, err
	}
	return runs, nil
}

func (r *GormRepository) FindLinkedVerificationClaims(runIDs []uuid.UUID) ([]models.VerificationClaim, error) {
	var claims []models.VerificationClaim
	if len(runIDs) == 0 {
		return claims, nil
	}
	if err := r.DB.Where("run_id IN ?", runIDs).Order("created_at ASC").Find(&claims).Error; err != nil {
		return nil, err
	}
	return claims, nil
}

func (r *GormRepository) FindLinkedVerificationEvidence(runIDs []uuid.UUID) ([]models.VerificationEvidence, error) {
	var evidence []models.VerificationEvidence
	if len(runIDs) == 0 {
		return evidence, nil
	}
	if err := r.DB.Where("run_id IN ?", runIDs).Order("quality_score DESC, created_at DESC").Find(&evidence).Error; err != nil {
		return nil, err
	}
	return evidence, nil
}

func (r *GormRepository) FindLinkedAutomations(ids []uuid.UUID) ([]models.Automation, error) {
	var automations []models.Automation
	if len(ids) == 0 {
		return automations, nil
	}
	if err := r.DB.Where("id IN ?", ids).Order("name ASC").Find(&automations).Error; err != nil {
		return nil, err
	}
	return automations, nil
}

func (r *GormRepository) FindLinkedAutomationLaunches(automationIDs []uuid.UUID, launchIDs []uuid.UUID, limit int) ([]models.AutomationLaunchEvent, error) {
	var events []models.AutomationLaunchEvent
	if len(automationIDs) == 0 && len(launchIDs) == 0 {
		return events, nil
	}
	if limit <= 0 || limit > 50 {
		limit = 20
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
	if err := query.Order("started_at DESC").Limit(limit).Find(&events).Error; err != nil {
		return nil, err
	}
	return events, nil
}
