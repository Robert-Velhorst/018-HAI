package workflow

import (
	"automation-hub-backend/internal/infra"
	"automation-hub-backend/internal/models"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Repository interface {
	CreateItem(item *models.WorkflowItem) (*models.WorkflowItem, error)
	CreateItemIdempotent(item *models.WorkflowItem) (*models.WorkflowItem, bool, error)
	UpdateItem(item *models.WorkflowItem) (*models.WorkflowItem, error)
	UpdateWorkflowItemCAS(expected, updated *models.WorkflowItem) (*models.WorkflowItem, bool, error)
	CommitManualWorkflowTransition(finalization WorkflowTransitionFinalization) (*models.WorkflowItem, bool, error)
	CommitWorkflowIntake(finalization WorkflowIntakeFinalization) (*models.WorkflowItem, bool, error)
	CommitWorkflowCoordinationProjection(finalization WorkflowCoordinationProjectionFinalization) (*models.WorkflowItem, bool, error)
	CommitWorkflowCoordinationFailure(finalization WorkflowCoordinationProjectionFinalization) (*models.WorkflowItem, bool, error)
	ResolveInterruptedExecutionCAS(expected, updated *models.WorkflowItem, sourceLink *models.WorkflowSourceLink, evidenceClaim *models.WorkflowEvidenceClaim, qualityGate *models.WorkflowQualityGate) (*models.WorkflowItem, bool, error)
	ReleaseInterruptedExecutionClaim(id uuid.UUID, claimID string) (bool, error)
	ResolvePendingApproval(id uuid.UUID, resolution ApprovalResolutionMutation) (*models.WorkflowItem, bool, error)
	FindItem(id uuid.UUID) (*models.WorkflowItem, error)
	FindActiveItemBySourceIdentity(sourceType, sourceID string) (*models.WorkflowItem, error)
	FindActiveItemBySourceURI(sourceURI string) (*models.WorkflowItem, error)
	FindActiveItemBySourceIdentityForOwner(ownerIdentity, sourceType, sourceID string) (*models.WorkflowItem, error)
	FindActiveItemBySourceURIForOwner(ownerIdentity, sourceURI string) (*models.WorkflowItem, error)
	FindItems(includeArchived bool) ([]models.WorkflowItem, error)
	FindApprovalItems() ([]models.WorkflowItem, error)
	FindRunnableItems(now time.Time, limit int) ([]models.WorkflowItem, error)
	FindRunnableItemsForOwner(ownerIdentity string, now time.Time, limit int) ([]models.WorkflowItem, error)
	ClaimRunnableItem(id uuid.UUID, claimID string, now time.Time, leaseUntil time.Time) (*models.WorkflowItem, bool, error)
	ClaimRunnableItemForOwner(ownerIdentity string, id uuid.UUID, claimID string, now time.Time, leaseUntil time.Time) (*models.WorkflowItem, bool, error)
	RenewRunnableItemClaim(id uuid.UUID, claimID string, leaseUntil time.Time) (bool, error)
	UpdateClaimedItem(item *models.WorkflowItem, claimID string) (*models.WorkflowItem, bool, error)
	CompleteClaimedItem(item *models.WorkflowItem, claimID string, attestation *models.WorkflowCompletionAttestation) (*models.WorkflowItem, bool, error)
	FindExpiredWorkflowClaims(now time.Time, limit int) ([]models.WorkflowItem, error)
	FindExpiredWorkflowClaimsForOwner(ownerIdentity string, now time.Time, limit int) ([]models.WorkflowItem, error)
	RecoverExpiredWorkflowClaim(item models.WorkflowItem, now time.Time) (*models.WorkflowItem, bool, error)
	CreateChecklistItem(item *models.WorkflowChecklistItem) (*models.WorkflowChecklistItem, error)
	UpdateChecklistItem(item *models.WorkflowChecklistItem) (*models.WorkflowChecklistItem, error)
	CommitChecklistUpdate(expected, updated *models.WorkflowChecklistItem, event models.WorkflowEvent) (*models.WorkflowChecklistItem, bool, error)
	FindChecklist(workflowID uuid.UUID) ([]models.WorkflowChecklistItem, error)
	FindReminderCandidatesForOwner(ownerIdentity string, before time.Time, limit int) ([]WorkflowReminderCandidate, error)
	SaveIntakeRecord(record *models.WorkflowIntakeRecord) (*models.WorkflowIntakeRecord, error)
	FindIntakeRecords(workflowID uuid.UUID) ([]models.WorkflowIntakeRecord, error)
	CreateProjectMatch(match *models.WorkflowProjectMatch) (*models.WorkflowProjectMatch, error)
	FindProjectMatches(workflowID uuid.UUID) ([]models.WorkflowProjectMatch, error)
	FindLinkedPursuits(workflowID uuid.UUID) ([]WorkflowPursuitContext, error)
	CreateEvidenceClaim(claim *models.WorkflowEvidenceClaim) (*models.WorkflowEvidenceClaim, error)
	FindEvidenceClaims(workflowID uuid.UUID) ([]models.WorkflowEvidenceClaim, error)
	CreateOpenLoop(loop *models.WorkflowOpenLoop) (*models.WorkflowOpenLoop, error)
	UpdateOpenLoop(loop *models.WorkflowOpenLoop) (*models.WorkflowOpenLoop, error)
	FindOpenLoops(workflowID uuid.UUID) ([]models.WorkflowOpenLoop, error)
	FindDashboardOpenLoops(now time.Time) ([]models.WorkflowOpenLoop, error)
	FindDashboardOpenLoopsForOwner(ownerIdentity string, now time.Time) ([]models.WorkflowOpenLoop, error)
	ClaimDueOpenLoop(id uuid.UUID, claimID string, now time.Time, leaseUntil time.Time) (*models.WorkflowOpenLoop, bool, error)
	ClaimDueOpenLoopForOwner(ownerIdentity string, id uuid.UUID, claimID string, now time.Time, leaseUntil time.Time) (*models.WorkflowOpenLoop, bool, error)
	RenewOpenLoopClaim(id uuid.UUID, claimID string, leaseUntil time.Time) (bool, error)
	UpdateClaimedOpenLoop(loop *models.WorkflowOpenLoop, claimID string) (*models.WorkflowOpenLoop, bool, error)
	FindExpiredOpenLoopClaims(now time.Time, limit int) ([]models.WorkflowOpenLoop, error)
	FindExpiredOpenLoopClaimsForOwner(ownerIdentity string, now time.Time, limit int) ([]models.WorkflowOpenLoop, error)
	RecoverExpiredOpenLoopClaim(loop models.WorkflowOpenLoop, now time.Time) (*models.WorkflowOpenLoop, bool, error)
	CreateProposal(proposal *models.WorkflowProposal) (*models.WorkflowProposal, error)
	UpdateProposal(proposal *models.WorkflowProposal) (*models.WorkflowProposal, error)
	FindProposals(workflowID uuid.UUID) ([]models.WorkflowProposal, error)
	CreateQualityGate(gate *models.WorkflowQualityGate) (*models.WorkflowQualityGate, error)
	UpdateQualityGate(gate *models.WorkflowQualityGate) (*models.WorkflowQualityGate, error)
	FindQualityGates(workflowID uuid.UUID) ([]models.WorkflowQualityGate, error)
	SaveRule(rule *models.WorkflowRule) (*models.WorkflowRule, error)
	FindRules() ([]models.WorkflowRule, error)
	CreateTransition(transition *models.WorkflowTransition) (*models.WorkflowTransition, error)
	FindTransitions(workflowID uuid.UUID) ([]models.WorkflowTransition, error)
	CreateSourceLink(link *models.WorkflowSourceLink) (*models.WorkflowSourceLink, error)
	FindSourceLinks(workflowID uuid.UUID) ([]models.WorkflowSourceLink, error)
	CreateDecision(decision *models.WorkflowDecision) (*models.WorkflowDecision, error)
	FindDecisions(workflowID uuid.UUID) ([]models.WorkflowDecision, error)
	FindApprovalDecisionForOwner(
		ctx context.Context,
		ownerIdentity string,
		decisionID string,
	) (*ApprovalDecisionRecord, error)
	CreateEvent(event *models.WorkflowEvent) (*models.WorkflowEvent, error)
	FindEvents(workflowID uuid.UUID) ([]models.WorkflowEvent, error)
}

type GormRepository struct {
	DB *gorm.DB
}

// WorkflowIntakeFinalization commits the runnable state and the audit records
// that explain how intake reached that state as one durable operation.
type WorkflowIntakeFinalization struct {
	Expected   *models.WorkflowItem
	Updated    *models.WorkflowItem
	Transition models.WorkflowTransition
	Decisions  []models.WorkflowDecision
	Events     []models.WorkflowEvent
}

type WorkflowCoordinationProjectionFinalization struct {
	Expected   *models.WorkflowItem
	Updated    *models.WorkflowItem
	Transition *models.WorkflowTransition
	Decision   models.WorkflowDecision
	Event      models.WorkflowEvent
}

// atomicProposalResolution is the complete durable result of resolving one
// proposal. Memory and graph projections are deliberately handled by the
// service only after this transaction commits.
type atomicProposalResolution struct {
	Status         string
	SelectedOption string
	ResolutionNote string
	ResolvedBy     string
	ResolvedAt     time.Time
	UpdatedItem    *models.WorkflowItem
	Transitions    []models.WorkflowTransition
	Decisions      []models.WorkflowDecision
	Events         []models.WorkflowEvent
}

type atomicProposalResolutionPlanner func(
	repository *GormRepository,
	item *models.WorkflowItem,
	proposal *models.WorkflowProposal,
) (*atomicProposalResolution, error)

var (
	ErrWorkflowProposalAlreadyResolved = errors.New("proposal is already resolved")
	errWorkflowProposalConflict        = errors.New("workflow changed while resolving proposal; reload before deciding again")
	errWorkflowProposalPostgresOnly    = errors.New("atomic workflow proposal resolution requires PostgreSQL")
	errWorkflowProposalRootDBRequired  = errors.New("atomic workflow proposal resolution requires a root PostgreSQL database connection")
)

// resolveProposalAtomically serializes proposal decisions with source
// retraction by locking the shared workflow row first. The proposal's open
// status is checked under lock and again in the guarded update. Every durable
// state, proposal, transition, decision, and event write commits or rolls back
// as one unit.
func (r *GormRepository) resolveProposalAtomically(
	workflowID uuid.UUID,
	proposalID uuid.UUID,
	planner atomicProposalResolutionPlanner,
) (*models.WorkflowItem, bool, error) {
	if r == nil || r.DB == nil || r.DB.Dialector == nil || r.DB.Dialector.Name() != "postgres" {
		return nil, false, errWorkflowProposalPostgresOnly
	}
	if r.DB.Statement == nil {
		return nil, false, errWorkflowProposalRootDBRequired
	}
	if _, alreadyTransactional := r.DB.Statement.ConnPool.(gorm.TxCommitter); alreadyTransactional {
		return nil, false, errWorkflowProposalRootDBRequired
	}
	if workflowID == uuid.Nil || proposalID == uuid.Nil || planner == nil {
		return nil, false, fmt.Errorf("workflow, proposal, and resolution planner are required")
	}

	var committedItem *models.WorkflowItem
	resolved := false
	err := r.DB.Transaction(func(tx *gorm.DB) error {
		var item models.WorkflowItem
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ?", workflowID).
			First(&item).Error; err != nil {
			return fmt.Errorf("lock workflow for proposal resolution: %w", err)
		}

		var proposal models.WorkflowProposal
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("workflow_id = ? AND id = ?", workflowID, proposalID).
			First(&proposal).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("proposal not found")
			}
			return fmt.Errorf("lock workflow proposal: %w", err)
		}
		if proposal.Status != "open" {
			return ErrWorkflowProposalAlreadyResolved
		}

		txRepository := &GormRepository{DB: tx}
		resolution, err := planner(txRepository, &item, &proposal)
		if err != nil {
			return err
		}
		if resolution == nil || resolution.Status == "" || resolution.Status == "open" ||
			resolution.ResolvedBy == "" || resolution.ResolvedAt.IsZero() {
			return fmt.Errorf("proposal resolution is incomplete")
		}

		if resolution.UpdatedItem != nil {
			updated, changed, err := txRepository.UpdateWorkflowItemCAS(&item, resolution.UpdatedItem)
			if err != nil {
				return fmt.Errorf("persist workflow state for proposal resolution: %w", err)
			}
			if !changed || updated == nil {
				return errWorkflowProposalConflict
			}
			committedItem = updated
		} else {
			itemCopy := item
			committedItem = &itemCopy
		}

		proposalUpdate := map[string]interface{}{
			"status":          resolution.Status,
			"selected_option": resolution.SelectedOption,
			"resolution_note": resolution.ResolutionNote,
			"resolved_by":     resolution.ResolvedBy,
			"resolved_at":     resolution.ResolvedAt.UTC(),
			"updated_at":      time.Now().UTC(),
		}
		proposalResult := tx.Model(&models.WorkflowProposal{}).
			Where("workflow_id = ? AND id = ? AND status = ?", workflowID, proposalID, "open").
			Updates(proposalUpdate)
		if proposalResult.Error != nil {
			return fmt.Errorf("persist resolved workflow proposal: %w", proposalResult.Error)
		}
		if proposalResult.RowsAffected != 1 {
			return ErrWorkflowProposalAlreadyResolved
		}

		for index := range resolution.Transitions {
			transition := &resolution.Transitions[index]
			if transition.WorkflowID != workflowID {
				return fmt.Errorf("proposal transition belongs to a different workflow")
			}
			if err := tx.Create(transition).Error; err != nil {
				return workflowAuditPersistenceFailure("proposal transition record", err)
			}
		}
		for index := range resolution.Decisions {
			decision := &resolution.Decisions[index]
			if decision.WorkflowID != workflowID {
				return fmt.Errorf("proposal decision belongs to a different workflow")
			}
			if err := tx.Create(decision).Error; err != nil {
				return workflowAuditPersistenceFailure("proposal decision", err)
			}
		}
		for index := range resolution.Events {
			event := &resolution.Events[index]
			if event.WorkflowID != workflowID {
				return fmt.Errorf("proposal audit event belongs to a different workflow")
			}
			if err := tx.Create(event).Error; err != nil {
				return workflowAuditPersistenceFailure("proposal event", err)
			}
		}
		resolved = true
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	if !resolved || committedItem == nil {
		return nil, false, nil
	}
	return committedItem, true, nil
}

// SourceWorkflowSupersession identifies the exact active source revision that
// may be superseded. GormRepository applies it as a conditional update so a
// concurrent worker claim cannot be overwritten by an intake snapshot.
type SourceWorkflowSupersession struct {
	ID                     uuid.UUID
	OwnerIdentity          string
	SourceType             string
	SourceID               string
	ExpectedSourceRevision string
	ExpectedCurrentState   string
}

var ErrWorkflowSourceSupersessionConflict = errors.New("workflow source supersession conflict")

// ApprovalResolutionMutation contains only the fields a pending approval is
// allowed to change. The repository applies it with a state-guarded update.
type ApprovalResolutionMutation struct {
	Approved        bool
	RejectionReason string
	Actor           string
	DecisionRule    string
}

func NewGormRepository(db *gorm.DB) Repository {
	return &GormRepository{DB: db}
}

func DefaultRepository() Repository {
	db, err := infra.GetDefaultDB()
	if err != nil {
		panic(err)
	}
	return NewGormRepository(db)
}

func (r *GormRepository) CreateItem(item *models.WorkflowItem) (*models.WorkflowItem, error) {
	if err := r.DB.Create(item).Error; err != nil {
		return nil, err
	}
	created, err := r.FindItem(item.ID)
	if err != nil {
		return nil, fmt.Errorf("reload created workflow item: %w", err)
	}
	return created, nil
}

// CreateItemIdempotent uses the active source-identity index or a transaction-
// scoped advisory lock so concurrent source intake cannot create duplicate work.
// Replays receive the exact existing item only when its source revision matches.
func (r *GormRepository) CreateItemIdempotent(item *models.WorkflowItem) (*models.WorkflowItem, bool, error) {
	if item == nil {
		return nil, false, fmt.Errorf("workflow item is required")
	}
	owner := strings.TrimSpace(item.OwnerIdentity)
	sourceType := strings.TrimSpace(item.SourceType)
	sourceID := strings.TrimSpace(item.SourceID)
	sourceURI := strings.TrimSpace(item.SourceURI)
	if sourceType == "" || sourceID == "" {
		if sourceURI != "" {
			return r.createItemIdempotentWithAdvisoryLock(item, owner, "uri", "", "", sourceURI)
		}
		created, err := r.CreateItem(item)
		return created, err == nil, err
	}
	if owner == "" {
		return r.createItemIdempotentWithAdvisoryLock(item, owner, "identity", sourceType, sourceID, "")
	}
	result := r.DB.Clauses(clause.OnConflict{DoNothing: true}).Create(item)
	if result.Error != nil {
		return nil, false, result.Error
	}
	if result.RowsAffected == 1 {
		created, err := r.FindItem(item.ID)
		if err != nil {
			return nil, false, fmt.Errorf("reload created workflow source identity: %w", err)
		}
		return created, true, nil
	}
	existing, err := r.FindActiveItemBySourceIdentityForOwner(owner, sourceType, sourceID)
	if err != nil {
		return nil, false, err
	}
	if existing == nil {
		return nil, false, fmt.Errorf("workflow source identity conflict could not be resolved")
	}
	if existing.SourceRevision != item.SourceRevision {
		return nil, false, fmt.Errorf("workflow source identity is already active with a different revision")
	}
	return existing, false, nil
}

func (r *GormRepository) createItemIdempotentWithAdvisoryLock(item *models.WorkflowItem, owner, identityKind, sourceType, sourceID, sourceURI string) (*models.WorkflowItem, bool, error) {
	lockKey := workflowSourceAdvisoryLockKey(owner, identityKind, sourceType, sourceID, sourceURI)
	var existing *models.WorkflowItem
	created := false
	err := r.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", lockKey).Error; err != nil {
			return fmt.Errorf("lock workflow source intake identity: %w", err)
		}

		query := tx.Where("archived = ?", false)
		if owner != "" {
			query = query.Where("owner_identity = ?", owner)
		}
		if identityKind == "identity" {
			if strings.TrimSpace(sourceType) == "" || strings.TrimSpace(sourceID) == "" {
				return fmt.Errorf("workflow source identity is invalid")
			}
			query = query.Where("source_type = ? AND source_id = ?", sourceType, sourceID)
		} else if identityKind == "uri" && strings.TrimSpace(sourceURI) != "" {
			query = query.Where("source_uri = ?", sourceURI)
		} else {
			return fmt.Errorf("workflow source identity kind is invalid")
		}

		var candidate models.WorkflowItem
		lookupErr := query.Order("updated_at desc").First(&candidate).Error
		if lookupErr == nil {
			if candidate.SourceRevision != item.SourceRevision {
				return fmt.Errorf("workflow source identity is already active with a different revision")
			}
			existing = &candidate
			return nil
		}
		if !errors.Is(lookupErr, gorm.ErrRecordNotFound) {
			return fmt.Errorf("find active workflow source identity: %w", lookupErr)
		}
		if err := tx.Create(item).Error; err != nil {
			return fmt.Errorf("create workflow source identity: %w", err)
		}
		existing = item
		created = true
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	if created {
		persisted, err := r.FindItem(item.ID)
		if err != nil {
			return nil, false, fmt.Errorf("reload created workflow source identity: %w", err)
		}
		return persisted, true, nil
	}
	return existing, created, nil
}

func workflowSourceAdvisoryLockKey(owner, identityKind, sourceType, sourceID, sourceURI string) string {
	parts := []string{"workflow-intake", owner, identityKind, sourceType, sourceID, sourceURI}
	var key strings.Builder
	for _, part := range parts {
		key.WriteString(fmt.Sprintf("%d:%s", len(part), part))
	}
	return key.String()
}

func (r *GormRepository) UpdateItem(item *models.WorkflowItem) (*models.WorkflowItem, error) {
	if err := r.DB.Save(item).Error; err != nil {
		return nil, err
	}
	return item, nil
}

func nextWorkflowRevision(previous time.Time) time.Time {
	previous = previous.UTC().Truncate(time.Microsecond)
	now := time.Now().UTC().Truncate(time.Microsecond)
	if !now.After(previous) {
		return previous.Add(time.Microsecond)
	}
	return now
}

func workflowItemStateUpdates(item *models.WorkflowItem, revision time.Time) map[string]interface{} {
	return map[string]interface{}{
		"current_state": item.CurrentState, "automation_id": item.AutomationID,
		"coordination_plan_id":        item.CoordinationPlanID,
		"coordination_plan_revision":  item.CoordinationPlanRevision,
		"coordination_plan_digest":    item.CoordinationPlanDigest,
		"coordination_plan_node_id":   item.CoordinationPlanNodeID,
		"coordination_draft_plan_id":  item.CoordinationDraftPlanID,
		"coordination_draft_revision": item.CoordinationDraftRevision,
		"coordination_draft_digest":   item.CoordinationDraftDigest,
		"coordination_draft_node_id":  item.CoordinationDraftNodeID,
		"autonomy_level":              item.AutonomyLevel, "requires_approval": item.RequiresApproval,
		"approval_status": item.ApprovalStatus, "approval_reason": item.ApprovalReason,
		"blocked_reason": item.BlockedReason, "next_action": item.NextAction,
		"next_run_at": item.NextRunAt, "retry_count": item.RetryCount,
		"max_retries": item.MaxRetries, "completed_at": item.CompletedAt,
		"verification_status": item.VerificationStatus, "recovery_status": item.RecoveryStatus,
		"recovery_note": item.RecoveryNote, "last_worker_error": item.LastWorkerError,
		"archived": item.Archived, "updated_at": revision,
	}
}

// UpdateWorkflowItemCAS prevents stale operator actions from overwriting a
// worker claim or a newer recovery decision. Review-held work may only remain
// blocked through this ordinary-state mutation path.
func (r *GormRepository) UpdateWorkflowItemCAS(expected, updated *models.WorkflowItem) (*models.WorkflowItem, bool, error) {
	if expected == nil || updated == nil || expected.ID == uuid.Nil || expected.ID != updated.ID {
		return nil, false, fmt.Errorf("matching expected and updated workflow items are required")
	}
	if expected.RecoveryStatus == RecoveryNeedsReview && updated.CurrentState != StateBlocked {
		return nil, false, nil
	}
	revision := nextWorkflowRevision(expected.UpdatedAt)
	updates := workflowItemStateUpdates(updated, revision)
	query := r.DB.Model(&models.WorkflowItem{}).
		Where("id = ? AND owner_identity = ? AND current_state = ? AND updated_at = ? AND archived = ? AND COALESCE(recovery_status, '') = ?", expected.ID, expected.OwnerIdentity, expected.CurrentState, expected.UpdatedAt, expected.Archived, expected.RecoveryStatus).
		Where("(worker_claim_id IS NULL OR worker_claim_id = '') AND worker_lease_until IS NULL")
	if updated.CurrentState != StateBlocked && updated.CurrentState != StateArchived {
		query = query.
			Where("COALESCE(approval_reason, '') NOT LIKE ?", sourceRetractionQuarantinePrefix+"%").
			Where("NOT EXISTS (SELECT 1 FROM workflow_events AS source_retraction WHERE source_retraction.workflow_id = workflow_items.id AND source_retraction.event_type = ?)", "workflow.source_retracted")
	}
	result := query.Updates(updates)
	if result.Error != nil {
		return nil, false, result.Error
	}
	if result.RowsAffected != 1 {
		return nil, false, nil
	}
	updatedItem, err := r.FindItem(expected.ID)
	return updatedItem, err == nil, err
}

// CommitWorkflowIntake keeps a worker from observing an activated workflow
// before its initial transition, decisions, and intake events are durable.
func (r *GormRepository) CommitWorkflowIntake(finalization WorkflowIntakeFinalization) (*models.WorkflowItem, bool, error) {
	expected, updated := finalization.Expected, finalization.Updated
	if expected == nil || updated == nil || expected.ID == uuid.Nil || expected.ID != updated.ID ||
		expected.CurrentState != StateNewInput || updated.CurrentState == "" || updated.CurrentState == StateNewInput {
		return nil, false, fmt.Errorf("valid new-input and activated workflow items are required")
	}
	if finalization.Transition.WorkflowID != expected.ID || finalization.Transition.FromState != StateNewInput ||
		finalization.Transition.ToState != updated.CurrentState {
		return nil, false, fmt.Errorf("intake transition must match the workflow activation")
	}
	for _, decision := range finalization.Decisions {
		if decision.WorkflowID != expected.ID {
			return nil, false, fmt.Errorf("intake decision does not match the workflow")
		}
	}
	for _, event := range finalization.Events {
		if event.WorkflowID != expected.ID {
			return nil, false, fmt.Errorf("intake event does not match the workflow")
		}
	}
	if r == nil || r.DB == nil || r.DB.Dialector == nil || r.DB.Dialector.Name() != "postgres" {
		return nil, false, workflowPersistenceFailure("workflow intake finalization", fmt.Errorf("PostgreSQL is required"))
	}
	if r.DB.Statement == nil {
		return nil, false, workflowPersistenceFailure("workflow intake finalization", fmt.Errorf("root database connection is required"))
	}
	if _, alreadyTransactional := r.DB.Statement.ConnPool.(gorm.TxCommitter); alreadyTransactional {
		return nil, false, workflowPersistenceFailure("workflow intake finalization", fmt.Errorf("root database connection is required"))
	}

	var committed *models.WorkflowItem
	changed := false
	err := r.DB.Transaction(func(tx *gorm.DB) error {
		txRepository := &GormRepository{DB: tx}
		var locked models.WorkflowItem
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ?", expected.ID).Take(&locked).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return fmt.Errorf("lock workflow for intake finalization: %w", err)
		}
		if locked.OwnerIdentity != expected.OwnerIdentity || locked.CurrentState != expected.CurrentState ||
			!locked.UpdatedAt.Equal(expected.UpdatedAt) || locked.Archived != expected.Archived ||
			locked.RecoveryStatus != expected.RecoveryStatus || locked.WorkerClaimID != "" || locked.WorkerLeaseUntil != nil {
			return nil
		}
		var retracted models.WorkflowEvent
		if err := tx.Where("workflow_id = ? AND event_type = ?", expected.ID, "workflow.source_retracted").Take(&retracted).Error; err == nil {
			return nil
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("check workflow source status during intake finalization: %w", err)
		}
		item, didChange, err := txRepository.UpdateWorkflowItemCAS(expected, updated)
		if err != nil {
			return fmt.Errorf("update workflow during intake finalization: %w", err)
		}
		if !didChange || item == nil {
			return nil
		}
		if _, err := txRepository.CreateTransition(&finalization.Transition); err != nil {
			return workflowAuditPersistenceFailure("intake transition", err)
		}
		for index := range finalization.Decisions {
			if _, err := txRepository.CreateDecision(&finalization.Decisions[index]); err != nil {
				return workflowAuditPersistenceFailure("intake decisions", err)
			}
		}
		for index := range finalization.Events {
			if _, err := txRepository.CreateEvent(&finalization.Events[index]); err != nil {
				return workflowAuditPersistenceFailure("intake events", err)
			}
		}
		committed = item
		changed = true
		return nil
	})
	if err != nil {
		var auditErr *WorkflowAuditPersistenceError
		if errors.As(err, &auditErr) {
			return nil, false, err
		}
		return nil, false, workflowPersistenceFailure("workflow intake finalization", err)
	}
	return committed, changed, nil
}

// CommitWorkflowCoordinationProjection binds an advisory plan and, when it
// restores a projection-blocked workflow, records the state transition in the
// same commit. Workers gate on this durable receipt before claiming the item.
func (r *GormRepository) CommitWorkflowCoordinationProjection(finalization WorkflowCoordinationProjectionFinalization) (*models.WorkflowItem, bool, error) {
	expected, updated := finalization.Expected, finalization.Updated
	if expected == nil || updated == nil || expected.ID == uuid.Nil || expected.ID != updated.ID ||
		updated.CoordinationDraftPlanID == nil || updated.CoordinationPlanID != nil {
		return nil, false, fmt.Errorf("matching workflow revisions and an advisory draft binding are required")
	}
	if finalization.Decision.WorkflowID != expected.ID || finalization.Decision.DecisionType != "coordination_plan" ||
		finalization.Event.WorkflowID != expected.ID || finalization.Event.EventType != "workflow.coordination_draft_projected" {
		return nil, false, fmt.Errorf("coordination projection audit records must match the workflow")
	}
	return r.commitWorkflowCoordinationFinalization(finalization, "workflow coordination projection")
}

func (r *GormRepository) CommitWorkflowCoordinationFailure(finalization WorkflowCoordinationProjectionFinalization) (*models.WorkflowItem, bool, error) {
	expected, updated := finalization.Expected, finalization.Updated
	if expected == nil || updated == nil || expected.ID == uuid.Nil || expected.ID != updated.ID {
		return nil, false, fmt.Errorf("matching workflow revisions are required for coordination failure finalization")
	}
	if finalization.Decision.WorkflowID != expected.ID || finalization.Decision.DecisionType != "coordination_plan" ||
		finalization.Decision.Decision != "unavailable" || finalization.Event.WorkflowID != expected.ID ||
		finalization.Event.EventType != "workflow.coordination_draft_failed" {
		return nil, false, fmt.Errorf("coordination failure audit records must match the workflow")
	}
	return r.commitWorkflowCoordinationFinalization(finalization, "workflow coordination failure")
}

func (r *GormRepository) commitWorkflowCoordinationFinalization(finalization WorkflowCoordinationProjectionFinalization, operation string) (*models.WorkflowItem, bool, error) {
	expected, updated := finalization.Expected, finalization.Updated
	if (expected.CurrentState != updated.CurrentState) != (finalization.Transition != nil) {
		return nil, false, fmt.Errorf("coordination state changes require exactly one matching transition")
	}
	if finalization.Transition != nil && (finalization.Transition.WorkflowID != expected.ID ||
		finalization.Transition.FromState != expected.CurrentState || finalization.Transition.ToState != updated.CurrentState) {
		return nil, false, fmt.Errorf("coordination transition must match the workflow update")
	}
	if finalization.Event.FromState != expected.CurrentState || finalization.Event.ToState != updated.CurrentState {
		return nil, false, fmt.Errorf("coordination event must match the workflow update")
	}
	if r == nil || r.DB == nil || r.DB.Dialector == nil || r.DB.Dialector.Name() != "postgres" || r.DB.Statement == nil {
		return nil, false, workflowPersistenceFailure(operation, fmt.Errorf("root PostgreSQL connection is required"))
	}
	if _, alreadyTransactional := r.DB.Statement.ConnPool.(gorm.TxCommitter); alreadyTransactional {
		return nil, false, workflowPersistenceFailure(operation, fmt.Errorf("root database connection is required"))
	}

	var committed *models.WorkflowItem
	changed := false
	err := r.DB.Transaction(func(tx *gorm.DB) error {
		txRepository := &GormRepository{DB: tx}
		var locked models.WorkflowItem
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ?", expected.ID).Take(&locked).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return fmt.Errorf("lock workflow for coordination finalization: %w", err)
		}
		if locked.OwnerIdentity != expected.OwnerIdentity || locked.CurrentState != expected.CurrentState ||
			!locked.UpdatedAt.Equal(expected.UpdatedAt) || locked.Archived != expected.Archived ||
			locked.RecoveryStatus != expected.RecoveryStatus || locked.WorkerClaimID != "" || locked.WorkerLeaseUntil != nil {
			return nil
		}
		var existingDecision models.WorkflowDecision
		decisionErr := tx.Where("workflow_id = ? AND decision_type = ? AND decision = ? AND reason = ? AND rule_applied = ? AND actor = ?",
			finalization.Decision.WorkflowID, finalization.Decision.DecisionType, finalization.Decision.Decision,
			finalization.Decision.Reason, finalization.Decision.RuleApplied, finalization.Decision.Actor).
			Take(&existingDecision).Error
		decisionMissing := errors.Is(decisionErr, gorm.ErrRecordNotFound)
		if decisionErr != nil && !decisionMissing {
			return fmt.Errorf("check existing coordination decision: %w", decisionErr)
		}
		var existingEvent models.WorkflowEvent
		eventErr := tx.Where("workflow_id = ? AND event_type = ? AND message = ?",
			finalization.Event.WorkflowID, finalization.Event.EventType, finalization.Event.Message).
			Take(&existingEvent).Error
		eventMissing := errors.Is(eventErr, gorm.ErrRecordNotFound)
		if eventErr != nil && !eventMissing {
			return fmt.Errorf("check existing coordination event: %w", eventErr)
		}
		var retracted models.WorkflowEvent
		if err := tx.Where("workflow_id = ? AND event_type = ?", expected.ID, "workflow.source_retracted").Take(&retracted).Error; err == nil {
			return nil
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("check workflow source status during coordination finalization: %w", err)
		}
		item, didChange, err := txRepository.UpdateWorkflowItemCAS(expected, updated)
		if err != nil {
			return fmt.Errorf("update workflow during coordination finalization: %w", err)
		}
		if !didChange || item == nil {
			return nil
		}
		if finalization.Transition != nil {
			if _, err := txRepository.CreateTransition(finalization.Transition); err != nil {
				return workflowAuditPersistenceFailure("coordination transition", err)
			}
		}
		if decisionMissing {
			if _, err := txRepository.CreateDecision(&finalization.Decision); err != nil {
				return workflowAuditPersistenceFailure("coordination decision", err)
			}
		}
		if eventMissing {
			if _, err := txRepository.CreateEvent(&finalization.Event); err != nil {
				return workflowAuditPersistenceFailure("coordination event", err)
			}
		}
		committed = item
		changed = true
		return nil
	})
	if err != nil {
		var auditErr *WorkflowAuditPersistenceError
		if errors.As(err, &auditErr) {
			return nil, false, err
		}
		return nil, false, workflowPersistenceFailure(operation, err)
	}
	return committed, changed, nil
}

func independentlyReconciledWorkflowCompletion(
	item *models.WorkflowItem,
	sourceLink *models.WorkflowSourceLink,
	evidenceClaim *models.WorkflowEvidenceClaim,
	qualityGate *models.WorkflowQualityGate,
) bool {
	return item != nil && item.CurrentState == StateCompleted && item.RecoveryStatus == RecoveryCompletionConfirmed &&
		item.VerificationStatus == "human_approved" && item.CompletedAt != nil && !item.RequiresApproval &&
		item.ApprovalStatus == approvalStatus(false) && strings.TrimSpace(item.ApprovalReason) == "" &&
		sourceLink != nil && sourceLink.WorkflowID == item.ID && sourceLink.Relationship == "completion_evidence" &&
		strings.EqualFold(strings.TrimSpace(sourceLink.SourceType), "recovery_evidence") && strings.TrimSpace(sourceLink.SourceURI) != "" &&
		strings.TrimSpace(sourceLink.SourceURI) != strings.TrimSpace(item.SourceURI) &&
		evidenceClaim != nil && evidenceClaim.WorkflowID == item.ID && strings.TrimSpace(evidenceClaim.ClaimText) != "" && !evidenceClaim.NeedsReview &&
		strings.EqualFold(strings.TrimSpace(evidenceClaim.Status), "human_approved") &&
		strings.EqualFold(strings.TrimSpace(evidenceClaim.Reliability), "operator_attestation") &&
		strings.TrimSpace(evidenceClaim.SourceURI) == strings.TrimSpace(sourceLink.SourceURI) &&
		qualityGate != nil && qualityGate.WorkflowID == item.ID &&
		strings.EqualFold(strings.TrimSpace(qualityGate.Gate), "verification before completion") &&
		strings.EqualFold(strings.TrimSpace(qualityGate.Status), "passed") && strings.TrimSpace(qualityGate.Reason) != ""
}

// ResolveInterruptedExecutionCAS commits the state resolution and its
// supporting source/evidence/gate records as one revision-guarded transaction.
// A retry/completion resolution clears the exact stale claim in that same
// transaction; keep-blocked retains it. This lets an operator reconcile a
// crashed worker without making a second attempt claimable before the
// evidence-backed decision commits.
func (r *GormRepository) ResolveInterruptedExecutionCAS(
	expected, updated *models.WorkflowItem,
	sourceLink *models.WorkflowSourceLink,
	evidenceClaim *models.WorkflowEvidenceClaim,
	qualityGate *models.WorkflowQualityGate,
) (*models.WorkflowItem, bool, error) {
	if expected == nil || updated == nil || expected.ID == uuid.Nil || expected.ID != updated.ID ||
		expected.CurrentState != StateBlocked || expected.RecoveryStatus != RecoveryNeedsReview {
		return nil, false, fmt.Errorf("valid interrupted workflow revision is required")
	}
	revision := nextWorkflowRevision(expected.UpdatedAt)
	var resultItem models.WorkflowItem
	resolved := false
	err := r.DB.Transaction(func(tx *gorm.DB) error {
		query := tx.Model(&models.WorkflowItem{}).
			Where("id = ? AND owner_identity = ? AND current_state = ? AND recovery_status = ? AND updated_at = ? AND archived = ?", expected.ID, expected.OwnerIdentity, StateBlocked, RecoveryNeedsReview, expected.UpdatedAt, expected.Archived).
			Where("COALESCE(worker_claim_id, '') = ?", expected.WorkerClaimID)
		if updated.CurrentState != StateBlocked && updated.CurrentState != StateArchived &&
			!independentlyReconciledWorkflowCompletion(updated, sourceLink, evidenceClaim, qualityGate) {
			query = query.
				Where("COALESCE(approval_reason, '') NOT LIKE ?", sourceRetractionQuarantinePrefix+"%").
				Where("NOT EXISTS (SELECT 1 FROM workflow_events AS source_retraction WHERE source_retraction.workflow_id = workflow_items.id AND source_retraction.event_type = ?)", "workflow.source_retracted")
		}
		if expected.WorkerLeaseUntil == nil {
			query = query.Where("worker_lease_until IS NULL")
		} else {
			query = query.Where("worker_lease_until = ?", *expected.WorkerLeaseUntil)
		}
		updates := workflowItemStateUpdates(updated, revision)
		if updated.CurrentState != StateBlocked {
			updates["worker_claim_id"] = ""
			updates["worker_lease_until"] = nil
		}
		result := query.Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return nil
		}
		if sourceLink != nil {
			if err := tx.Create(sourceLink).Error; err != nil {
				return fmt.Errorf("store interrupted-execution source link: %w", err)
			}
		}
		if evidenceClaim != nil {
			if err := tx.Create(evidenceClaim).Error; err != nil {
				return fmt.Errorf("store interrupted-execution evidence: %w", err)
			}
		}
		if qualityGate != nil {
			gateResult := tx.Model(&models.WorkflowQualityGate{}).
				Where("workflow_id = ? AND LOWER(gate) = LOWER(?)", qualityGate.WorkflowID, qualityGate.Gate).
				Updates(map[string]interface{}{"status": qualityGate.Status, "reason": qualityGate.Reason, "updated_at": revision})
			if gateResult.Error != nil {
				return gateResult.Error
			}
			if gateResult.RowsAffected == 0 {
				qualityGate.CreatedAt = revision
				qualityGate.UpdatedAt = revision
				if err := tx.Create(qualityGate).Error; err != nil {
					return fmt.Errorf("store interrupted-execution quality gate: %w", err)
				}
			}
		}
		if err := tx.First(&resultItem, "id = ?", expected.ID).Error; err != nil {
			return err
		}
		resolved = true
		return nil
	})
	if err != nil || !resolved {
		return nil, resolved, err
	}
	return &resultItem, true, nil
}

// ReleaseInterruptedExecutionClaim is called only after the claimed runner
// has returned. Keeping the claim through lease recovery fences other attempts.
func (r *GormRepository) ReleaseInterruptedExecutionClaim(id uuid.UUID, claimID string) (bool, error) {
	claimID = strings.TrimSpace(claimID)
	if id == uuid.Nil || claimID == "" {
		return false, fmt.Errorf("workflow id and execution claim are required")
	}
	var item models.WorkflowItem
	if err := r.DB.Select("updated_at").First(&item, "id = ?", id).Error; err != nil {
		return false, err
	}
	result := r.DB.Model(&models.WorkflowItem{}).
		Where("id = ? AND current_state = ? AND recovery_status = ? AND worker_claim_id = ?", id, StateBlocked, RecoveryNeedsReview, claimID).
		Updates(map[string]interface{}{"worker_claim_id": "", "worker_lease_until": nil, "updated_at": nextWorkflowRevision(item.UpdatedAt)})
	return result.RowsAffected == 1, result.Error
}

// SupersedeSourceWorkflowCAS archives only the expected, unclaimed active
// source revision. It intentionally updates no unrelated workflow fields.
func (r *GormRepository) SupersedeSourceWorkflowCAS(expected SourceWorkflowSupersession) (bool, error) {
	if expected.ID == uuid.Nil {
		return false, fmt.Errorf("workflow id is required for source supersession")
	}
	if strings.TrimSpace(expected.ExpectedCurrentState) == "" {
		return false, fmt.Errorf("expected workflow state is required for source supersession")
	}
	result := r.DB.Model(&models.WorkflowItem{}).
		Where("id = ? AND owner_identity = ? AND source_type = ? AND source_id = ? AND source_revision = ? AND current_state = ? AND archived = ?", expected.ID, expected.OwnerIdentity, expected.SourceType, expected.SourceID, expected.ExpectedSourceRevision, expected.ExpectedCurrentState, false).
		Where("(worker_claim_id IS NULL OR worker_claim_id = '')").
		UpdateColumns(map[string]interface{}{
			"archived":           true,
			"current_state":      StateArchived,
			"next_action":        "superseded by revised source content",
			"next_run_at":        nil,
			"worker_claim_id":    "",
			"worker_lease_until": nil,
		})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected == 1, nil
}

// ResolvePendingApproval uses a conditional update inside a transaction as a
// compare-and-swap. Exactly one resolver can move an active pending approval
// out of needs_approval; stale or terminal state returns resolved=false.
func (r *GormRepository) ResolvePendingApproval(
	id uuid.UUID,
	resolution ApprovalResolutionMutation,
) (*models.WorkflowItem, bool, error) {
	if id == uuid.Nil {
		return nil, false, fmt.Errorf("workflow id is required")
	}
	actor := strings.TrimSpace(resolution.Actor)
	decisionRule := strings.TrimSpace(resolution.DecisionRule)
	if actor == "" || decisionRule == "" {
		return nil, false, fmt.Errorf("workflow approval actor and decision rule are required")
	}
	updates := map[string]interface{}{
		"updated_at": time.Now().UTC(),
	}
	decision := "rejected"
	if resolution.Approved {
		decision = "approved"
		updates["current_state"] = StateReady
		updates["approval_status"] = "approved"
		updates["blocked_reason"] = ""
		updates["next_action"] = "execute approved workflow steps"
	} else {
		updates["current_state"] = StateBlocked
		updates["approval_status"] = "rejected"
		updates["blocked_reason"] = firstNonEmpty(strings.TrimSpace(resolution.RejectionReason), "approval rejected")
		updates["next_action"] = "review rejection reason before continuing"
	}

	var updated models.WorkflowItem
	resolved := false
	err := r.DB.Transaction(func(tx *gorm.DB) error {
		query := tx.Model(&models.WorkflowItem{}).
			Where("id = ? AND current_state = ? AND approval_status = ? AND requires_approval = ? AND archived = ? AND COALESCE(recovery_status, '') <> ?",
				id, StateNeedsApproval, "pending", true, false, RecoveryNeedsReview).
			Where("(worker_claim_id IS NULL OR worker_claim_id = '') AND worker_lease_until IS NULL")
		if resolution.Approved {
			query = query.
				Where("COALESCE(approval_reason, '') NOT LIKE ?", sourceRetractionQuarantinePrefix+"%").
				Where("NOT EXISTS (SELECT 1 FROM workflow_events AS source_retraction WHERE source_retraction.workflow_id = workflow_items.id AND source_retraction.event_type = ?)", "workflow.source_retracted")
		}
		result := query.Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return nil
		}
		if err := tx.First(&updated, "id = ?", id).Error; err != nil {
			return err
		}
		transition := models.WorkflowTransition{
			WorkflowID: id, FromState: StateNeedsApproval, ToState: updated.CurrentState,
			Trigger: "approval_resolution", Actor: actor, Approved: resolution.Approved,
			Reason: strings.TrimSpace(resolution.RejectionReason),
		}
		if err := tx.Create(&transition).Error; err != nil {
			return workflowAuditPersistenceFailure("approval transition record", err)
		}
		approvalDecision := models.WorkflowDecision{
			WorkflowID: id, DecisionType: "approval", Decision: decision,
			Reason: strings.TrimSpace(resolution.RejectionReason), RuleApplied: decisionRule,
			Approved: resolution.Approved, Actor: actor,
		}
		if err := tx.Create(&approvalDecision).Error; err != nil {
			return workflowAuditPersistenceFailure("approval decision", err)
		}
		event := models.WorkflowEvent{
			WorkflowID: id, EventType: "workflow.approval",
			FromState: StateNeedsApproval, ToState: updated.CurrentState,
			Message: strings.TrimSpace(resolution.RejectionReason), Trigger: "approval_resolution",
			RuleApplied: decisionRule, SourceURI: updated.SourceURI, Actor: actor,
		}
		if err := tx.Create(&event).Error; err != nil {
			return workflowAuditPersistenceFailure("approval event", err)
		}
		resolved = true
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	if !resolved {
		return nil, false, nil
	}
	return &updated, true, nil
}

func (r *GormRepository) FindItem(id uuid.UUID) (*models.WorkflowItem, error) {
	var item models.WorkflowItem
	if err := r.DB.First(&item, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *GormRepository) FindActiveItemBySourceIdentity(sourceType, sourceID string) (*models.WorkflowItem, error) {
	var item models.WorkflowItem
	err := r.DB.Where(
		"source_type = ? AND source_id = ? AND archived = ?",
		sourceType,
		sourceID,
		false,
	).Order("updated_at desc").First(&item).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *GormRepository) FindActiveItemBySourceURI(sourceURI string) (*models.WorkflowItem, error) {
	var item models.WorkflowItem
	err := r.DB.Where("source_uri = ? AND archived = ?", sourceURI, false).Order("updated_at desc").First(&item).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *GormRepository) FindActiveItemBySourceIdentityForOwner(ownerIdentity, sourceType, sourceID string) (*models.WorkflowItem, error) {
	if ownerIdentity == "" {
		return r.FindActiveItemBySourceIdentity(sourceType, sourceID)
	}
	var item models.WorkflowItem
	err := r.DB.Where(
		"owner_identity = ? AND source_type = ? AND source_id = ? AND archived = ?",
		ownerIdentity,
		sourceType,
		sourceID,
		false,
	).Order("updated_at desc").First(&item).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *GormRepository) FindActiveItemBySourceURIForOwner(ownerIdentity, sourceURI string) (*models.WorkflowItem, error) {
	if ownerIdentity == "" {
		return r.FindActiveItemBySourceURI(sourceURI)
	}
	var item models.WorkflowItem
	err := r.DB.Where(
		"owner_identity = ? AND source_uri = ? AND archived = ?",
		ownerIdentity,
		sourceURI,
		false,
	).Order("updated_at desc").First(&item).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *GormRepository) FindItems(includeArchived bool) ([]models.WorkflowItem, error) {
	var items []models.WorkflowItem
	query := r.DB.Order("priority_score desc, updated_at desc")
	if !includeArchived {
		query = query.Where("archived = ?", false)
	}
	err := query.Find(&items).Error
	return items, err
}

func (r *GormRepository) FindApprovalItems() ([]models.WorkflowItem, error) {
	var items []models.WorkflowItem
	err := r.DB.
		Where("archived = ? AND current_state = ?", false, StateNeedsApproval).
		Order("priority_score desc, updated_at desc").
		Find(&items).Error
	return items, err
}

func (r *GormRepository) FindRunnableItems(now time.Time, limit int) ([]models.WorkflowItem, error) {
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	var items []models.WorkflowItem
	err := r.DB.
		Where("archived = ? AND current_state = ? AND COALESCE(recovery_status, '') <> ? AND retry_count < CASE WHEN max_retries <= 0 THEN 2 ELSE max_retries END", false, StateReady, RecoveryNeedsReview).
		Where("requires_approval = ? OR approval_status = ?", false, "approved").
		Where("COALESCE(approval_reason, '') NOT LIKE ?", sourceRetractionQuarantinePrefix+"%").
		Where("NOT EXISTS (SELECT 1 FROM workflow_events AS source_retraction WHERE source_retraction.workflow_id = workflow_items.id AND source_retraction.event_type = ?)", "workflow.source_retracted").
		Where("next_run_at IS NULL OR next_run_at <= ?", now).
		Order("priority_score desc, updated_at asc").
		Limit(limit).
		Find(&items).Error
	return items, err
}

func (r *GormRepository) FindRunnableItemsForOwner(ownerIdentity string, now time.Time, limit int) ([]models.WorkflowItem, error) {
	if ownerIdentity == "" {
		return r.FindRunnableItems(now, limit)
	}
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	var items []models.WorkflowItem
	err := r.DB.
		Where("owner_identity = ?", ownerIdentity).
		Where("archived = ? AND current_state = ? AND COALESCE(recovery_status, '') <> ? AND retry_count < CASE WHEN max_retries <= 0 THEN 2 ELSE max_retries END", false, StateReady, RecoveryNeedsReview).
		Where("requires_approval = ? OR approval_status = ?", false, "approved").
		Where("COALESCE(approval_reason, '') NOT LIKE ?", sourceRetractionQuarantinePrefix+"%").
		Where("NOT EXISTS (SELECT 1 FROM workflow_events AS source_retraction WHERE source_retraction.workflow_id = workflow_items.id AND source_retraction.event_type = ?)", "workflow.source_retracted").
		Where("next_run_at IS NULL OR next_run_at <= ?", now).
		Order("priority_score desc, updated_at asc").
		Limit(limit).
		Find(&items).Error
	return items, err
}

func (r *GormRepository) ClaimRunnableItem(id uuid.UUID, claimID string, now time.Time, leaseUntil time.Time) (*models.WorkflowItem, bool, error) {
	return r.claimRunnableItem("", id, claimID, now, leaseUntil)
}

func (r *GormRepository) ClaimRunnableItemForOwner(ownerIdentity string, id uuid.UUID, claimID string, now time.Time, leaseUntil time.Time) (*models.WorkflowItem, bool, error) {
	return r.claimRunnableItem(ownerIdentity, id, claimID, now, leaseUntil)
}

func (r *GormRepository) claimRunnableItem(ownerIdentity string, id uuid.UUID, claimID string, now time.Time, leaseUntil time.Time) (*models.WorkflowItem, bool, error) {
	query := r.DB.
		Model(&models.WorkflowItem{}).
		Where("id = ? AND archived = ? AND current_state = ? AND COALESCE(recovery_status, '') <> ? AND retry_count < CASE WHEN max_retries <= 0 THEN 2 ELSE max_retries END", id, false, StateReady, RecoveryNeedsReview).
		Where("requires_approval = ? OR approval_status = ?", false, "approved").
		Where("COALESCE(approval_reason, '') NOT LIKE ?", sourceRetractionQuarantinePrefix+"%").
		Where("NOT EXISTS (SELECT 1 FROM workflow_events AS source_retraction WHERE source_retraction.workflow_id = workflow_items.id AND source_retraction.event_type = ?)", "workflow.source_retracted").
		Where("next_run_at IS NULL OR next_run_at <= ?", now)
	if ownerIdentity != "" {
		query = query.Where("owner_identity = ?", ownerIdentity)
	}
	result := query.Updates(map[string]interface{}{
		"current_state":      StateInProgress,
		"last_run_at":        now,
		"next_action":        "task engine is executing claimed workflow item",
		"last_worker_error":  "",
		"worker_claim_id":    claimID,
		"worker_lease_until": leaseUntil,
		"updated_at":         now,
	})
	if result.Error != nil {
		return nil, false, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, false, nil
	}
	item, err := r.FindItem(id)
	if err != nil {
		return nil, false, err
	}
	r.startAutonomyAttempt(item, now)
	return item, true, nil
}

func (r *GormRepository) RenewRunnableItemClaim(id uuid.UUID, claimID string, leaseUntil time.Time) (bool, error) {
	currentTime := time.Now().UTC()
	// Materialization fences the wall-clock check behind row-lock acquisition,
	// even when the previous locker did not change the row version.
	result := r.DB.Exec(`
WITH locked_claim AS MATERIALIZED (
  SELECT id, worker_lease_until FROM workflow_items
  WHERE id = ? AND current_state = ? AND worker_claim_id = ?
  FOR UPDATE
)
UPDATE workflow_items
SET worker_lease_until = ?, updated_at = ?
FROM locked_claim
WHERE workflow_items.id = locked_claim.id
  AND locked_claim.worker_lease_until > clock_timestamp()`, id, StateInProgress, claimID, leaseUntil, currentTime)
	return result.RowsAffected == 1, result.Error
}

func (r *GormRepository) UpdateClaimedItem(item *models.WorkflowItem, claimID string) (*models.WorkflowItem, bool, error) {
	now := time.Now().UTC()
	result := r.DB.
		Model(&models.WorkflowItem{}).
		Where("id = ? AND current_state = ? AND worker_claim_id = ?", item.ID, StateInProgress, claimID).
		Updates(map[string]interface{}{
			"current_state":       item.CurrentState,
			"requires_approval":   item.RequiresApproval,
			"approval_status":     item.ApprovalStatus,
			"approval_reason":     item.ApprovalReason,
			"blocked_reason":      item.BlockedReason,
			"next_action":         item.NextAction,
			"retry_count":         item.RetryCount,
			"max_retries":         item.MaxRetries,
			"next_run_at":         item.NextRunAt,
			"last_run_at":         item.LastRunAt,
			"completed_at":        item.CompletedAt,
			"verification_status": item.VerificationStatus,
			"recovery_status":     item.RecoveryStatus,
			"recovery_note":       item.RecoveryNote,
			"last_task_plan_id":   item.LastTaskPlanID,
			"last_worker_error":   item.LastWorkerError,
			"worker_claim_id":     "",
			"worker_lease_until":  nil,
			"updated_at":          now,
		})
	if result.Error != nil {
		return nil, false, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, false, nil
	}
	updated, err := r.FindItem(item.ID)
	if err != nil {
		return nil, false, err
	}
	r.finishAutonomyAttempt(updated, now)
	return updated, true, nil
}

// CompleteClaimedItem changes the mutable workflow projection and appends its
// immutable completion attestation in one transaction. Downstream accounting
// must never infer completion from WorkflowItem alone.
func (r *GormRepository) CompleteClaimedItem(
	item *models.WorkflowItem,
	claimID string,
	attestation *models.WorkflowCompletionAttestation,
) (*models.WorkflowItem, bool, error) {
	if item == nil || attestation == nil || item.ID == uuid.Nil ||
		strings.TrimSpace(claimID) == "" {
		return nil, false, fmt.Errorf("valid claimed workflow completion evidence is required")
	}
	if item.CurrentState != StateCompleted {
		return nil, false, fmt.Errorf("workflow completion projection does not match its attestation")
	}
	if err := validateWorkflowCompletionAttestation(*item, attestation); err != nil {
		return nil, false, fmt.Errorf("workflow completion attestation rejected: %w", err)
	}
	var updated models.WorkflowItem
	owned := false
	now := time.Now().UTC()
	err := r.DB.Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&models.WorkflowItem{}).
			Where("id = ? AND current_state = ? AND worker_claim_id = ?", item.ID, StateInProgress, claimID).
			Updates(map[string]interface{}{
				"current_state": StateCompleted, "approval_status": item.ApprovalStatus,
				"blocked_reason": item.BlockedReason, "next_action": item.NextAction,
				"retry_count": item.RetryCount, "max_retries": item.MaxRetries,
				"next_run_at": nil, "last_run_at": item.LastRunAt,
				"completed_at": item.CompletedAt, "verification_status": item.VerificationStatus,
				"recovery_status": item.RecoveryStatus, "recovery_note": item.RecoveryNote,
				"last_task_plan_id": item.LastTaskPlanID, "last_worker_error": "",
				"worker_claim_id": "", "worker_lease_until": nil, "updated_at": now,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return nil
		}
		if err := tx.Create(attestation).Error; err != nil {
			return fmt.Errorf("append workflow completion attestation: %w", err)
		}
		if err := tx.Where("id = ?", item.ID).First(&updated).Error; err != nil {
			return err
		}
		owned = true
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	if !owned {
		return nil, false, nil
	}
	r.finishAutonomyAttempt(&updated, now)
	return &updated, true, nil
}

func (r *GormRepository) FindExpiredWorkflowClaims(now time.Time, limit int) ([]models.WorkflowItem, error) {
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	legacyBefore := now.Add(-claimLeaseDuration())
	var items []models.WorkflowItem
	err := r.DB.
		Where("archived = ? AND current_state = ?", false, StateInProgress).
		Where("(worker_claim_id <> ? AND worker_lease_until IS NOT NULL AND worker_lease_until <= ?) OR ((worker_claim_id = ? OR worker_claim_id IS NULL) AND worker_lease_until IS NULL AND last_run_at IS NOT NULL AND last_run_at <= ?)", "", now, "", legacyBefore).
		Order("worker_lease_until asc").
		Limit(limit).
		Find(&items).Error
	return items, err
}

func (r *GormRepository) FindExpiredWorkflowClaimsForOwner(ownerIdentity string, now time.Time, limit int) ([]models.WorkflowItem, error) {
	if ownerIdentity == "" {
		return r.FindExpiredWorkflowClaims(now, limit)
	}
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	legacyBefore := now.Add(-claimLeaseDuration())
	var items []models.WorkflowItem
	err := r.DB.
		Where("owner_identity = ?", ownerIdentity).
		Where("archived = ? AND current_state = ?", false, StateInProgress).
		Where("(worker_claim_id <> ? AND worker_lease_until IS NOT NULL AND worker_lease_until <= ?) OR ((worker_claim_id = ? OR worker_claim_id IS NULL) AND worker_lease_until IS NULL AND last_run_at IS NOT NULL AND last_run_at <= ?)", "", now, "", legacyBefore).
		Order("worker_lease_until asc").
		Limit(limit).
		Find(&items).Error
	return items, err
}

func (r *GormRepository) RecoverExpiredWorkflowClaim(item models.WorkflowItem, now time.Time) (*models.WorkflowItem, bool, error) {
	reason := "worker lease expired; execution outcome is unknown and requires human review"
	claimFence := item.WorkerClaimID
	if strings.TrimSpace(claimFence) == "" {
		// Legacy runs have no process-owned token that can prove termination.
		// Keep them fenced rather than making them retryable.
		claimFence = "legacy-recovery:" + uuid.NewString()
	}
	query := r.DB.Model(&models.WorkflowItem{}).
		Where("id = ? AND current_state = ? AND updated_at = ?", item.ID, StateInProgress, item.UpdatedAt)
	if item.WorkerClaimID == "" {
		query = query.Where("(worker_claim_id = ? OR worker_claim_id IS NULL) AND worker_lease_until IS NULL AND last_run_at IS NOT NULL AND last_run_at <= ?", "", now.Add(-claimLeaseDuration()))
	} else {
		query = query.Where("worker_claim_id = ? AND worker_lease_until <= ?", item.WorkerClaimID, now)
	}
	result := query.
		Updates(map[string]interface{}{
			"current_state":     StateBlocked,
			"blocked_reason":    reason,
			"next_action":       "review external side effects before retrying interrupted workflow",
			"last_worker_error": reason,
			"recovery_status":   RecoveryNeedsReview,
			"recovery_note":     "",
			"retry_count":       gorm.Expr("retry_count + 1"),
			"next_run_at":       nil,
			"worker_claim_id":   claimFence,
			"updated_at":        nextWorkflowRevision(item.UpdatedAt),
		})
	if result.Error != nil {
		return nil, false, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, false, nil
	}
	updated, err := r.FindItem(item.ID)
	if err != nil {
		return nil, false, err
	}
	return updated, true, nil
}

func (r *GormRepository) CreateChecklistItem(item *models.WorkflowChecklistItem) (*models.WorkflowChecklistItem, error) {
	if err := r.DB.Create(item).Error; err != nil {
		return nil, err
	}
	return item, nil
}

func (r *GormRepository) UpdateChecklistItem(item *models.WorkflowChecklistItem) (*models.WorkflowChecklistItem, error) {
	if err := r.DB.Save(item).Error; err != nil {
		return nil, err
	}
	return item, nil
}

func (r *GormRepository) FindChecklist(workflowID uuid.UUID) ([]models.WorkflowChecklistItem, error) {
	var items []models.WorkflowChecklistItem
	err := r.DB.Where("workflow_id = ?", workflowID).Order("position asc, created_at asc").Find(&items).Error
	return items, err
}

// CommitChecklistUpdate preserves the status and its audit event as one revision-guarded write.
func (r *GormRepository) CommitChecklistUpdate(expected, updated *models.WorkflowChecklistItem, event models.WorkflowEvent) (*models.WorkflowChecklistItem, bool, error) {
	if r == nil || r.DB == nil || r.DB.Dialector == nil || r.DB.Dialector.Name() != "postgres" {
		return nil, false, fmt.Errorf("atomic checklist update requires PostgreSQL")
	}
	if expected == nil || updated == nil || expected.ID == uuid.Nil || expected.WorkflowID == uuid.Nil ||
		updated.ID != expected.ID || updated.WorkflowID != expected.WorkflowID ||
		event.WorkflowID != expected.WorkflowID || event.EventType != "workflow.checklist" {
		return nil, false, fmt.Errorf("checklist update and audit references must match")
	}
	if updated.Status != "open" && updated.Status != "done" && updated.Status != "blocked" {
		return nil, false, fmt.Errorf("unsupported checklist status")
	}
	var committed *models.WorkflowChecklistItem
	err := r.DB.Transaction(func(tx *gorm.DB) error {
		var parent models.WorkflowItem
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&parent, "id = ?", expected.WorkflowID).Error; err != nil {
			return err
		}
		now := time.Now().UTC()
		result := tx.Model(&models.WorkflowChecklistItem{}).
			Where("id = ? AND workflow_id = ? AND status = ? AND updated_at = ?", expected.ID, expected.WorkflowID, expected.Status, expected.UpdatedAt).
			Updates(map[string]interface{}{"status": updated.Status, "updated_at": now})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return nil
		}
		if _, err := NewGormRepository(tx).CreateEvent(&event); err != nil {
			return workflowAuditPersistenceFailure("checklist event", err)
		}
		var persisted models.WorkflowChecklistItem
		if err := tx.First(&persisted, "id = ? AND workflow_id = ?", expected.ID, expected.WorkflowID).Error; err != nil {
			return err
		}
		committed = &persisted
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	return committed, committed != nil, nil
}

func (r *GormRepository) SaveIntakeRecord(record *models.WorkflowIntakeRecord) (*models.WorkflowIntakeRecord, error) {
	if err := r.DB.Save(record).Error; err != nil {
		return nil, err
	}
	return record, nil
}

func (r *GormRepository) FindIntakeRecords(workflowID uuid.UUID) ([]models.WorkflowIntakeRecord, error) {
	var records []models.WorkflowIntakeRecord
	err := r.DB.Where("workflow_id = ?", workflowID).Order("created_at desc").Find(&records).Error
	return records, err
}

func (r *GormRepository) CreateProjectMatch(match *models.WorkflowProjectMatch) (*models.WorkflowProjectMatch, error) {
	if err := r.DB.Create(match).Error; err != nil {
		return nil, err
	}
	return match, nil
}

func (r *GormRepository) FindProjectMatches(workflowID uuid.UUID) ([]models.WorkflowProjectMatch, error) {
	var matches []models.WorkflowProjectMatch
	err := r.DB.Where("workflow_id = ?", workflowID).Order("confidence desc, created_at desc").Find(&matches).Error
	return matches, err
}

func (r *GormRepository) FindLinkedPursuits(workflowID uuid.UUID) ([]WorkflowPursuitContext, error) {
	var rows []struct {
		ID                    uuid.UUID
		OwnerIdentity         string
		Title                 string
		Status                string
		RiskLevel             string
		PriorityScore         int
		Confidence            float64
		AutonomyLevel         string
		NeedCategory          string
		WhyItMatters          string
		DesiredOutcome        string
		CurrentStateSummary   string
		NextRecommendedAction string
		CompletionDefinition  string
		CompletionState       string
		LinkID                uuid.UUID
		Relationship          string
		SourceURI             string
		SourceLabel           string
		LinkConfidence        float64
	}
	err := r.DB.Table("pursuit_links AS links").
		Select(`pursuits.id,
			pursuits.owner_identity,
			pursuits.title,
			pursuits.status,
			pursuits.risk_level,
			pursuits.priority_score,
			pursuits.confidence,
			pursuits.autonomy_level,
			pursuits.need_category,
			pursuits.why_it_matters,
			pursuits.desired_outcome,
			pursuits.current_state_summary,
			pursuits.next_recommended_action,
			pursuits.completion_definition,
			pursuits.completion_state,
			links.id AS link_id,
			links.relationship,
			links.source_uri,
			links.source_label,
			links.confidence AS link_confidence`).
		Joins("JOIN pursuits ON pursuits.id = links.pursuit_id").
		Where("links.link_type = ? AND links.link_id = ? AND pursuits.archived = ?", "workflow", workflowID.String(), false).
		Order("pursuits.priority_score DESC, links.created_at DESC").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	result := make([]WorkflowPursuitContext, 0, len(rows))
	for _, row := range rows {
		result = append(result, WorkflowPursuitContext{
			ID:                    row.ID,
			OwnerIdentity:         row.OwnerIdentity,
			Title:                 row.Title,
			Status:                row.Status,
			RiskLevel:             row.RiskLevel,
			PriorityScore:         row.PriorityScore,
			Confidence:            row.Confidence,
			AutonomyLevel:         row.AutonomyLevel,
			NeedCategory:          row.NeedCategory,
			WhyItMatters:          row.WhyItMatters,
			DesiredOutcome:        row.DesiredOutcome,
			CurrentStateSummary:   row.CurrentStateSummary,
			NextRecommendedAction: row.NextRecommendedAction,
			CompletionDefinition:  row.CompletionDefinition,
			CompletionState:       row.CompletionState,
			LinkID:                row.LinkID,
			Relationship:          row.Relationship,
			SourceURI:             row.SourceURI,
			SourceLabel:           row.SourceLabel,
			LinkConfidence:        row.LinkConfidence,
		})
	}
	return result, nil
}

func (r *GormRepository) CreateEvidenceClaim(claim *models.WorkflowEvidenceClaim) (*models.WorkflowEvidenceClaim, error) {
	if err := r.DB.Create(claim).Error; err != nil {
		return nil, err
	}
	return claim, nil
}

func (r *GormRepository) FindEvidenceClaims(workflowID uuid.UUID) ([]models.WorkflowEvidenceClaim, error) {
	var claims []models.WorkflowEvidenceClaim
	err := r.DB.Where("workflow_id = ?", workflowID).Order("created_at desc").Find(&claims).Error
	return claims, err
}

func (r *GormRepository) CreateOpenLoop(loop *models.WorkflowOpenLoop) (*models.WorkflowOpenLoop, error) {
	if err := r.DB.Create(loop).Error; err != nil {
		return nil, err
	}
	return loop, nil
}

func (r *GormRepository) UpdateOpenLoop(loop *models.WorkflowOpenLoop) (*models.WorkflowOpenLoop, error) {
	if err := r.DB.Save(loop).Error; err != nil {
		return nil, err
	}
	return loop, nil
}

func (r *GormRepository) FindOpenLoops(workflowID uuid.UUID) ([]models.WorkflowOpenLoop, error) {
	var loops []models.WorkflowOpenLoop
	err := r.DB.Where("workflow_id = ?", workflowID).Order("follow_up_at asc NULLS LAST, updated_at desc").Find(&loops).Error
	return loops, err
}

func (r *GormRepository) FindDashboardOpenLoops(now time.Time) ([]models.WorkflowOpenLoop, error) {
	var loops []models.WorkflowOpenLoop
	err := r.DB.
		Table("workflow_open_loops").
		Joins("JOIN workflow_items ON workflow_items.id = workflow_open_loops.workflow_id").
		Where("workflow_open_loops.status = ?", "open").
		Where("workflow_open_loops.follow_up_at IS NULL OR workflow_open_loops.follow_up_at <= ?", now).
		Where("workflow_items.archived = ? AND workflow_items.current_state NOT IN ?", false, []string{StateArchived, StateCompleted}).
		Order("workflow_open_loops.follow_up_at asc NULLS LAST, workflow_open_loops.updated_at desc").
		Limit(50).
		Find(&loops).Error
	return loops, err
}

func (r *GormRepository) FindDashboardOpenLoopsForOwner(ownerIdentity string, now time.Time) ([]models.WorkflowOpenLoop, error) {
	if ownerIdentity == "" {
		return r.FindDashboardOpenLoops(now)
	}
	var loops []models.WorkflowOpenLoop
	err := r.DB.
		Table("workflow_open_loops").
		Joins("JOIN workflow_items ON workflow_items.id = workflow_open_loops.workflow_id").
		Where("workflow_items.owner_identity = ?", ownerIdentity).
		Where("workflow_open_loops.status = ?", "open").
		Where("workflow_open_loops.follow_up_at IS NULL OR workflow_open_loops.follow_up_at <= ?", now).
		Where("workflow_items.archived = ? AND workflow_items.current_state NOT IN ?", false, []string{StateArchived, StateCompleted}).
		Order("workflow_open_loops.follow_up_at asc NULLS LAST, workflow_open_loops.updated_at desc").
		Limit(50).
		Find(&loops).Error
	return loops, err
}

func (r *GormRepository) ClaimDueOpenLoop(id uuid.UUID, claimID string, now time.Time, leaseUntil time.Time) (*models.WorkflowOpenLoop, bool, error) {
	return r.claimDueOpenLoop("", id, claimID, now, leaseUntil)
}

func (r *GormRepository) ClaimDueOpenLoopForOwner(ownerIdentity string, id uuid.UUID, claimID string, now time.Time, leaseUntil time.Time) (*models.WorkflowOpenLoop, bool, error) {
	return r.claimDueOpenLoop(ownerIdentity, id, claimID, now, leaseUntil)
}

func (r *GormRepository) claimDueOpenLoop(ownerIdentity string, id uuid.UUID, claimID string, now time.Time, leaseUntil time.Time) (*models.WorkflowOpenLoop, bool, error) {
	existsQuery := "EXISTS (SELECT 1 FROM workflow_items WHERE workflow_items.id = workflow_open_loops.workflow_id AND workflow_items.archived = ? AND workflow_items.current_state NOT IN ?)"
	existsArgs := []interface{}{false, []string{StateArchived, StateCompleted}}
	if ownerIdentity != "" {
		existsQuery = "EXISTS (SELECT 1 FROM workflow_items WHERE workflow_items.id = workflow_open_loops.workflow_id AND workflow_items.owner_identity = ? AND workflow_items.archived = ? AND workflow_items.current_state NOT IN ?)"
		existsArgs = []interface{}{ownerIdentity, false, []string{StateArchived, StateCompleted}}
	}
	result := r.DB.
		Model(&models.WorkflowOpenLoop{}).
		Where("id = ? AND status = ?", id, "open").
		Where("follow_up_at IS NULL OR follow_up_at <= ?", now).
		Where(existsQuery, existsArgs...).
		Updates(map[string]interface{}{
			"status":      "processing",
			"claim_id":    claimID,
			"lease_until": leaseUntil,
			"updated_at":  now,
		})
	if result.Error != nil {
		return nil, false, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, false, nil
	}
	var loop models.WorkflowOpenLoop
	if err := r.DB.First(&loop, "id = ?", id).Error; err != nil {
		return nil, false, err
	}
	return &loop, true, nil
}

func (r *GormRepository) RenewOpenLoopClaim(id uuid.UUID, claimID string, leaseUntil time.Time) (bool, error) {
	currentTime := time.Now().UTC()
	// Match runnable-item renewal: check expiry only after acquiring the lock.
	result := r.DB.Exec(`
WITH locked_claim AS MATERIALIZED (
  SELECT id, lease_until FROM workflow_open_loops
  WHERE id = ? AND status = ? AND claim_id = ?
  FOR UPDATE
)
UPDATE workflow_open_loops
SET lease_until = ?, updated_at = ?
FROM locked_claim
WHERE workflow_open_loops.id = locked_claim.id
  AND locked_claim.lease_until > clock_timestamp()`, id, "processing", claimID, leaseUntil, currentTime)
	return result.RowsAffected == 1, result.Error
}

func (r *GormRepository) UpdateClaimedOpenLoop(loop *models.WorkflowOpenLoop, claimID string) (*models.WorkflowOpenLoop, bool, error) {
	now := time.Now().UTC()
	result := r.DB.
		Model(&models.WorkflowOpenLoop{}).
		Where("id = ? AND status = ? AND claim_id = ?", loop.ID, "processing", claimID).
		Updates(map[string]interface{}{
			"status":      loop.Status,
			"claim_id":    "",
			"lease_until": nil,
			"updated_at":  now,
		})
	if result.Error != nil {
		return nil, false, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, false, nil
	}
	var updated models.WorkflowOpenLoop
	if err := r.DB.First(&updated, "id = ?", loop.ID).Error; err != nil {
		return nil, false, err
	}
	return &updated, true, nil
}

func (r *GormRepository) FindExpiredOpenLoopClaims(now time.Time, limit int) ([]models.WorkflowOpenLoop, error) {
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	legacyBefore := now.Add(-claimLeaseDuration())
	var loops []models.WorkflowOpenLoop
	err := r.DB.
		Where("status = ?", "processing").
		Where("(claim_id <> ? AND lease_until IS NOT NULL AND lease_until <= ?) OR ((claim_id = ? OR claim_id IS NULL) AND lease_until IS NULL AND updated_at <= ?)", "", now, "", legacyBefore).
		Order("lease_until asc").
		Limit(limit).
		Find(&loops).Error
	return loops, err
}

func (r *GormRepository) FindExpiredOpenLoopClaimsForOwner(ownerIdentity string, now time.Time, limit int) ([]models.WorkflowOpenLoop, error) {
	if ownerIdentity == "" {
		return r.FindExpiredOpenLoopClaims(now, limit)
	}
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	legacyBefore := now.Add(-claimLeaseDuration())
	var loops []models.WorkflowOpenLoop
	err := r.DB.
		Table("workflow_open_loops").
		Joins("JOIN workflow_items ON workflow_items.id = workflow_open_loops.workflow_id").
		Where("workflow_items.owner_identity = ?", ownerIdentity).
		Where("workflow_open_loops.status = ?", "processing").
		Where("(workflow_open_loops.claim_id <> ? AND workflow_open_loops.lease_until IS NOT NULL AND workflow_open_loops.lease_until <= ?) OR ((workflow_open_loops.claim_id = ? OR workflow_open_loops.claim_id IS NULL) AND workflow_open_loops.lease_until IS NULL AND workflow_open_loops.updated_at <= ?)", "", now, "", legacyBefore).
		Order("workflow_open_loops.lease_until asc").
		Limit(limit).
		Find(&loops).Error
	return loops, err
}

func (r *GormRepository) RecoverExpiredOpenLoopClaim(loop models.WorkflowOpenLoop, now time.Time) (*models.WorkflowOpenLoop, bool, error) {
	query := r.DB.Model(&models.WorkflowOpenLoop{}).Where("id = ? AND status = ?", loop.ID, "processing")
	if loop.ClaimID == "" {
		query = query.Where("(claim_id = ? OR claim_id IS NULL) AND lease_until IS NULL AND updated_at <= ?", "", now.Add(-claimLeaseDuration()))
	} else {
		query = query.Where("claim_id = ? AND lease_until <= ?", loop.ClaimID, now)
	}
	result := query.
		Updates(map[string]interface{}{
			"status":      "open",
			"claim_id":    "",
			"lease_until": nil,
			"updated_at":  now,
		})
	if result.Error != nil {
		return nil, false, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, false, nil
	}
	var updated models.WorkflowOpenLoop
	if err := r.DB.First(&updated, "id = ?", loop.ID).Error; err != nil {
		return nil, false, err
	}
	return &updated, true, nil
}

func (r *GormRepository) CreateProposal(proposal *models.WorkflowProposal) (*models.WorkflowProposal, error) {
	if err := r.DB.Create(proposal).Error; err != nil {
		return nil, err
	}
	return proposal, nil
}

func (r *GormRepository) UpdateProposal(proposal *models.WorkflowProposal) (*models.WorkflowProposal, error) {
	if err := r.DB.Save(proposal).Error; err != nil {
		return nil, err
	}
	return proposal, nil
}

func (r *GormRepository) FindProposals(workflowID uuid.UUID) ([]models.WorkflowProposal, error) {
	var proposals []models.WorkflowProposal
	err := r.DB.Where("workflow_id = ?", workflowID).Order("created_at desc").Find(&proposals).Error
	return proposals, err
}

func (r *GormRepository) CreateQualityGate(gate *models.WorkflowQualityGate) (*models.WorkflowQualityGate, error) {
	if err := r.DB.Create(gate).Error; err != nil {
		return nil, err
	}
	return gate, nil
}

func (r *GormRepository) UpdateQualityGate(gate *models.WorkflowQualityGate) (*models.WorkflowQualityGate, error) {
	if err := r.DB.Save(gate).Error; err != nil {
		return nil, err
	}
	return gate, nil
}

func (r *GormRepository) FindQualityGates(workflowID uuid.UUID) ([]models.WorkflowQualityGate, error) {
	var gates []models.WorkflowQualityGate
	err := r.DB.Where("workflow_id = ?", workflowID).Order("created_at desc").Find(&gates).Error
	return gates, err
}

func (r *GormRepository) SaveRule(rule *models.WorkflowRule) (*models.WorkflowRule, error) {
	var existing models.WorkflowRule
	if rule.ID == uuid.Nil {
		err := r.DB.Where("rule_key = ?", rule.RuleKey).First(&existing).Error
		if err == nil {
			rule.ID = existing.ID
			if rule.CreatedAt.IsZero() {
				rule.CreatedAt = existing.CreatedAt
			}
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
	} else if rule.CreatedAt.IsZero() {
		if err := r.DB.First(&existing, "id = ?", rule.ID).Error; err == nil {
			rule.CreatedAt = existing.CreatedAt
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
	}
	if err := r.DB.Save(rule).Error; err != nil {
		return nil, err
	}
	return rule, nil
}

func (r *GormRepository) FindRules() ([]models.WorkflowRule, error) {
	var rules []models.WorkflowRule
	err := r.DB.Order("category asc, rule_key asc").Find(&rules).Error
	return rules, err
}

func (r *GormRepository) CreateTransition(transition *models.WorkflowTransition) (*models.WorkflowTransition, error) {
	if err := r.DB.Create(transition).Error; err != nil {
		return nil, err
	}
	return transition, nil
}

func (r *GormRepository) FindTransitions(workflowID uuid.UUID) ([]models.WorkflowTransition, error) {
	var transitions []models.WorkflowTransition
	err := r.DB.Where("workflow_id = ?", workflowID).Order("created_at desc").Find(&transitions).Error
	return transitions, err
}

func (r *GormRepository) CreateSourceLink(link *models.WorkflowSourceLink) (*models.WorkflowSourceLink, error) {
	if err := r.DB.Create(link).Error; err != nil {
		return nil, err
	}
	return link, nil
}

func (r *GormRepository) FindSourceLinks(workflowID uuid.UUID) ([]models.WorkflowSourceLink, error) {
	var links []models.WorkflowSourceLink
	err := r.DB.Where("workflow_id = ?", workflowID).Order("created_at desc").Find(&links).Error
	return links, err
}

func (r *GormRepository) CreateDecision(decision *models.WorkflowDecision) (*models.WorkflowDecision, error) {
	if err := r.DB.Create(decision).Error; err != nil {
		return nil, err
	}
	return decision, nil
}

func (r *GormRepository) FindDecisions(workflowID uuid.UUID) ([]models.WorkflowDecision, error) {
	var decisions []models.WorkflowDecision
	err := r.DB.Where("workflow_id = ?", workflowID).Order("created_at desc").Find(&decisions).Error
	return decisions, err
}

func (r *GormRepository) FindApprovalDecisionForOwner(
	ctx context.Context,
	ownerIdentity string,
	decisionID string,
) (*ApprovalDecisionRecord, error) {
	if ctx == nil {
		return nil, errors.New("workflow approval lookup context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	parsedDecisionID, err := uuid.Parse(decisionID)
	if err != nil || parsedDecisionID == uuid.Nil || decisionID != parsedDecisionID.String() {
		return nil, gorm.ErrRecordNotFound
	}

	var row struct {
		DecisionID    uuid.UUID
		WorkflowID    uuid.UUID
		OwnerIdentity string
		DecisionType  string
		Decision      string
		Reason        string
		ActionBinding string
		Approved      bool
		Actor         string
		CreatedAt     time.Time
	}
	err = r.DB.WithContext(ctx).
		Table("workflow_decisions AS decisions").
		Select(`decisions.id AS decision_id,
			decisions.workflow_id,
			items.owner_identity,
			decisions.decision_type,
			decisions.decision,
			decisions.reason,
			decisions.rule_applied AS action_binding,
			decisions.approved,
			decisions.actor,
			decisions.created_at`).
		Joins(
			"JOIN workflow_items AS items ON items.id = decisions.workflow_id AND items.owner_identity = ?",
			ownerIdentity,
		).
		Where("decisions.id = ?", parsedDecisionID).
		Take(&row).Error
	if err != nil {
		return nil, err
	}
	return &ApprovalDecisionRecord{
		DecisionID:    row.DecisionID.String(),
		WorkflowID:    row.WorkflowID.String(),
		OwnerIdentity: row.OwnerIdentity,
		DecisionType:  row.DecisionType,
		Decision:      row.Decision,
		Reason:        row.Reason,
		ActionBinding: row.ActionBinding,
		Approved:      row.Approved,
		Actor:         row.Actor,
		CreatedAt:     row.CreatedAt,
	}, nil
}

func (r *GormRepository) CreateEvent(event *models.WorkflowEvent) (*models.WorkflowEvent, error) {
	if err := r.DB.Create(event).Error; err != nil {
		return nil, err
	}
	return event, nil
}

func (r *GormRepository) FindEvents(workflowID uuid.UUID) ([]models.WorkflowEvent, error) {
	var events []models.WorkflowEvent
	err := r.DB.Where("workflow_id = ?", workflowID).Order("created_at desc").Find(&events).Error
	return events, err
}
