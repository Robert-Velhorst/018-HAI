package workflow

import (
	"errors"
	"fmt"
	"strings"

	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/safety"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var errFollowUpClaimLost = errors.New("open-loop claim is no longer owned or has expired")

// This capability is separate from Repository so implementations without a
// durable transaction cannot silently fall back to independent writes.
type transactionalFollowUpRepository interface {
	commitDueOpenLoop(owner string, workflowID, loopID uuid.UUID, claimID string) (*followUpCommit, error)
	releaseDueOpenLoopClaim(owner string, workflowID, loopID uuid.UUID, claimID string) (bool, error)
}

type followUpCommit struct {
	Item      *models.WorkflowItem
	FromState string
	Status    string
	Replayed  bool
}

type followUpProjection struct {
	Updated         *models.WorkflowItem
	Checklist       *models.WorkflowChecklistItem
	ChecklistExists bool
	Proposal        *models.WorkflowProposal
	ProposalExists  bool
	Transition      *models.WorkflowTransition
	Decision        models.WorkflowDecision
	Event           models.WorkflowEvent
	Status          string
}

func followUpRecordID(loopID uuid.UUID, kind string) uuid.UUID {
	return uuid.NewSHA1(loopID, []byte("workflow-followup/v1/"+kind))
}

func followUpRootDB(r *GormRepository) error {
	if r == nil || r.DB == nil || r.DB.Config == nil || r.DB.Dialector == nil || r.DB.Dialector.Name() != "postgres" || r.DB.Statement == nil {
		return workflowPersistenceFailure("follow-up projection", fmt.Errorf("root PostgreSQL connection is required"))
	}
	if _, transactional := r.DB.Statement.ConnPool.(gorm.TxCommitter); transactional {
		return workflowPersistenceFailure("follow-up projection", fmt.Errorf("root PostgreSQL connection is required"))
	}
	return nil
}

// Lock workflow first, matching proposal resolution and source retraction.
// The lease is checked using database wall time only AFTER both locks arrive.
func lockDueOpenLoop(tx *gorm.DB, owner string, workflowID, loopID uuid.UUID) (*models.WorkflowItem, *models.WorkflowOpenLoop, error) {
	var item models.WorkflowItem
	query := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", workflowID)
	if owner = strings.TrimSpace(owner); owner != "" {
		query = query.Where("owner_identity = ?", owner)
	}
	if err := query.Take(&item).Error; err != nil {
		return nil, nil, err
	}
	var loop models.WorkflowOpenLoop
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("workflow_id = ? AND id = ?", workflowID, loopID).Take(&loop).Error; err != nil {
		return nil, nil, err
	}
	return &item, &loop, nil
}

func activeDueOpenLoopClaim(tx *gorm.DB, loop *models.WorkflowOpenLoop, claimID string) error {
	if strings.TrimSpace(claimID) == "" || loop.Status != "processing" || loop.ClaimID != claimID {
		return errFollowUpClaimLost
	}
	var active bool
	if err := tx.Model(&models.WorkflowOpenLoop{}).
		Select("lease_until > clock_timestamp() AND (follow_up_at IS NULL OR follow_up_at <= clock_timestamp())").
		Where("id = ? AND lease_until IS NOT NULL", loop.ID).Scan(&active).Error; err != nil {
		return err
	}
	if !active {
		return errFollowUpClaimLost
	}
	return nil
}

func finishDueOpenLoop(tx *gorm.DB, loop *models.WorkflowOpenLoop, claimID, status string) error {
	result := tx.Model(&models.WorkflowOpenLoop{}).
		Where("id = ? AND workflow_id = ? AND status = 'processing' AND claim_id = ? AND lease_until > clock_timestamp()", loop.ID, loop.WorkflowID, claimID).
		Updates(map[string]interface{}{"status": status, "claim_id": "", "lease_until": nil, "updated_at": gorm.Expr("clock_timestamp()")})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return errFollowUpClaimLost
	}
	// An UPDATE trigger can take time after its WHERE fence was evaluated.
	// Confirm the original locked lease once more after that statement returns.
	var active bool
	if err := tx.Raw("SELECT ?::timestamptz > clock_timestamp()", loop.LeaseUntil).Scan(&active).Error; err != nil {
		return err
	}
	if !active {
		return errFollowUpClaimLost
	}
	return nil
}

// commitDueOpenLoop commits the entire local projection or nothing. There is
// no task execution, graph projection, provider call, or approval in this path.
func (r *GormRepository) commitDueOpenLoop(owner string, workflowID, loopID uuid.UUID, claimID string) (*followUpCommit, error) {
	if err := followUpRootDB(r); err != nil {
		return nil, err
	}
	if workflowID == uuid.Nil || loopID == uuid.Nil || strings.TrimSpace(claimID) == "" {
		return nil, errFollowUpClaimLost
	}
	var committed *followUpCommit
	err := r.DB.Transaction(func(tx *gorm.DB) error {
		item, loop, err := lockDueOpenLoop(tx, owner, workflowID, loopID)
		if err != nil {
			return err
		}
		// A durable, loop-specific receipt proves a prior commit, including
		// an acknowledgement lost by the caller. Never reapply workflow state.
		var receipt models.WorkflowDecision
		receiptErr := tx.Where("id = ? AND workflow_id = ?", followUpRecordID(loopID, "decision"), workflowID).Take(&receipt).Error
		if receiptErr != nil && !errors.Is(receiptErr, gorm.ErrRecordNotFound) {
			return receiptErr
		}
		if receiptErr == nil {
			if receipt.DecisionType != "open_loop" || (receipt.Decision != "triggered" && receipt.Decision != "resolved") {
				return fmt.Errorf("invalid follow-up commit receipt; review required")
			}
			if loop.Status == "processing" {
				if err := activeDueOpenLoopClaim(tx, loop, claimID); err != nil {
					return err
				}
				if err := finishDueOpenLoop(tx, loop, claimID, receipt.Decision); err != nil {
					return err
				}
			} else if loop.Status != receipt.Decision {
				return errFollowUpClaimLost
			}
			committed = &followUpCommit{Item: item, FromState: item.CurrentState, Status: receipt.Decision, Replayed: true}
			return nil
		}
		if err := activeDueOpenLoopClaim(tx, loop, claimID); err != nil {
			return err
		}
		if decision := safety.EvaluateEmergencyStopForExecution(); decision.Active {
			return fmt.Errorf("follow-up projection blocked: %s", decision.Reason)
		}
		txRepo := &GormRepository{DB: tx}
		projection, err := planDueOpenLoop(txRepo, item, *loop)
		if err != nil {
			return err
		}
		stored := item
		if projection.Updated != nil {
			var changed bool
			stored, changed, err = txRepo.UpdateWorkflowItemCAS(item, projection.Updated)
			if err != nil {
				return err
			}
			if !changed || stored == nil {
				return fmt.Errorf("workflow changed or holds an execution/review fence; retry after review")
			}
		}
		if projection.Checklist != nil {
			if !projection.ChecklistExists {
				if _, err := txRepo.CreateChecklistItem(projection.Checklist); err != nil {
					return fmt.Errorf("create follow-up checklist: %w", err)
				}
			} else if projection.Checklist.RequiresApproval {
				if err := tx.Model(&models.WorkflowChecklistItem{}).Where("id = ? AND workflow_id = ?", projection.Checklist.ID, workflowID).
					Update("requires_approval", true).Error; err != nil {
					return err
				}
			}
		}
		if projection.Proposal != nil && !projection.ProposalExists {
			if _, err := txRepo.CreateProposal(projection.Proposal); err != nil {
				return fmt.Errorf("create follow-up proposal: %w", err)
			}
		}
		if projection.Transition != nil {
			if _, err := txRepo.CreateTransition(projection.Transition); err != nil {
				return workflowAuditPersistenceFailure("follow-up transition", err)
			}
		}
		if _, err := txRepo.CreateDecision(&projection.Decision); err != nil {
			return workflowAuditPersistenceFailure("follow-up decision", err)
		}
		if _, err := txRepo.CreateEvent(&projection.Event); err != nil {
			return workflowAuditPersistenceFailure("follow-up event", err)
		}
		// Check expiry again after all writes; failure rolls everything back.
		if err := finishDueOpenLoop(tx, loop, claimID, projection.Status); err != nil {
			return err
		}
		committed = &followUpCommit{Item: stored, FromState: item.CurrentState, Status: projection.Status}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return committed, nil
}

func (r *GormRepository) releaseDueOpenLoopClaim(owner string, workflowID, loopID uuid.UUID, claimID string) (bool, error) {
	if err := followUpRootDB(r); err != nil {
		return false, err
	}
	err := r.DB.Transaction(func(tx *gorm.DB) error {
		_, loop, err := lockDueOpenLoop(tx, owner, workflowID, loopID)
		if err != nil {
			return err
		}
		if err := activeDueOpenLoopClaim(tx, loop, claimID); err != nil {
			return err
		}
		return finishDueOpenLoop(tx, loop, claimID, "open")
	})
	if errors.Is(err, errFollowUpClaimLost) || errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	return err == nil, err
}

func planDueOpenLoop(repo Repository, item *models.WorkflowItem, loop models.WorkflowOpenLoop) (*followUpProjection, error) {
	projection := &followUpProjection{Status: "resolved"}
	from, to := item.CurrentState, item.CurrentState
	message, reason := "open loop resolved because workflow is closed", "workflow already completed or archived"
	if !item.Archived && from != StateArchived && from != StateCompleted {
		if err := (&service{repo: repo}).ensureWorkflowSourceNotRetracted(item); err != nil {
			return nil, err
		}
		if item.WorkerClaimID != "" || item.WorkerLeaseUntil != nil || from == StateInProgress {
			return nil, fmt.Errorf("workflow execution fence is active; follow-up deferred")
		}
		if item.RecoveryStatus == RecoveryNeedsReview && from != StateBlocked {
			return nil, fmt.Errorf("workflow requires interruption review; follow-up deferred")
		}
		checklist, err := repo.FindChecklist(item.ID)
		if err != nil {
			return nil, fmt.Errorf("inspect follow-up checklist: %w", err)
		}
		proposals, err := repo.FindProposals(item.ID)
		if err != nil {
			return nil, fmt.Errorf("inspect follow-up proposals: %w", err)
		}
		loops, err := repo.FindOpenLoops(item.ID)
		if err != nil {
			return nil, fmt.Errorf("inspect follow-up identities: %w", err)
		}
		projection, err = buildFollowUpProjection(item, loop, checklist, proposals, loops)
		if err != nil {
			return nil, err
		}
		to = projection.Updated.CurrentState
		message, reason = "due open loop projected proposal and checklist step", loop.WaitingFor
	}
	projection.Decision = models.WorkflowDecision{
		ID: followUpRecordID(loop.ID, "decision"), WorkflowID: item.ID,
		DecisionType: "open_loop", Decision: projection.Status, Reason: reason,
		RuleApplied: "follow-up engine", Actor: "workflow-followup", Approved: false,
	}
	projection.Event = models.WorkflowEvent{
		ID: followUpRecordID(loop.ID, "event"), WorkflowID: item.ID,
		EventType: "workflow.open_loop", FromState: from, ToState: to, Message: message,
		Trigger: "followup_worker", RuleApplied: "follow-up engine", SourceURI: item.SourceURI, Actor: "workflow-followup",
	}
	return projection, nil
}

func buildFollowUpProjection(item *models.WorkflowItem, loop models.WorkflowOpenLoop, checklist []models.WorkflowChecklistItem, proposals []models.WorkflowProposal, loops []models.WorkflowOpenLoop) (*followUpProjection, error) {
	updated := *item
	requiresApproval := item.RequiresApproval || item.RiskLevel == "high" || loop.ResponsibleParty == "Robert" || item.CurrentState == StateNeedsApproval
	optionsItem := *item
	optionsItem.RequiresApproval = requiresApproval
	projection := &followUpProjection{Updated: &updated, Status: "triggered"}
	projection.Checklist = &models.WorkflowChecklistItem{
		ID: followUpRecordID(loop.ID, "checklist"), WorkflowID: item.ID,
		Label: "Resolve due open loop: " + compact(loop.WaitingFor, 160), Status: "open",
		Position: 950, RequiresApproval: requiresApproval, DueAt: loop.FollowUpAt,
	}
	projection.Proposal = &models.WorkflowProposal{
		ID: followUpRecordID(loop.ID, "proposal"), WorkflowID: item.ID,
		RecommendedAction: "Follow-up due: " + firstNonEmpty(loop.NextAction, loop.WaitingFor),
		Options:           strings.Join(followUpOptions(&optionsItem, loop), "\n"), Status: "open",
	}
	// Adopt unfinished artifacts from the old independent-write path, but
	// never coalesce different new loops merely because their text is equal.
	knownIDs := make(map[uuid.UUID]bool)
	matchingLabels, matchingActions := 0, 0
	for _, other := range loops {
		knownIDs[followUpRecordID(other.ID, "checklist")] = true
		knownIDs[followUpRecordID(other.ID, "proposal")] = true
		if "Resolve due open loop: "+compact(other.WaitingFor, 160) == projection.Checklist.Label {
			matchingLabels++
		}
		if "Follow-up due: "+firstNonEmpty(other.NextAction, other.WaitingFor) == projection.Proposal.RecommendedAction {
			matchingActions++
		}
	}
	for _, existing := range checklist {
		if existing.ID == projection.Checklist.ID {
			if existing.Status != "open" {
				return nil, fmt.Errorf("follow-up checklist was already decided without a commit receipt; review required")
			}
			existing.RequiresApproval = existing.RequiresApproval || requiresApproval
			projection.Checklist, projection.ChecklistExists = &existing, true
			break
		}
	}
	if !projection.ChecklistExists {
		for _, existing := range checklist {
			if !knownIDs[existing.ID] && existing.Status == "open" && existing.Label == projection.Checklist.Label {
				if matchingLabels > 1 {
					return nil, fmt.Errorf("legacy follow-up checklist matches multiple open loops; review its binding before retrying")
				}
				existing.RequiresApproval = existing.RequiresApproval || requiresApproval
				projection.Checklist, projection.ChecklistExists = &existing, true
				break
			}
		}
	}
	for _, existing := range proposals {
		if existing.ID == projection.Proposal.ID {
			if existing.Status != "open" {
				return nil, fmt.Errorf("follow-up proposal was already decided without a commit receipt; review required")
			}
			projection.Proposal, projection.ProposalExists = &existing, true
			break
		}
	}
	if !projection.ProposalExists {
		for _, existing := range proposals {
			if !knownIDs[existing.ID] && existing.Status == "open" && existing.RecommendedAction == projection.Proposal.RecommendedAction {
				if matchingActions > 1 {
					return nil, fmt.Errorf("legacy follow-up proposal matches multiple open loops; review its binding before retrying")
				}
				projection.Proposal, projection.ProposalExists = &existing, true
				break
			}
		}
	}
	if updated.CurrentState == StateBlocked {
		if updated.RecoveryStatus != RecoveryNeedsReview {
			updated.NextAction = "review due follow-up proposal when blocker is cleared"
		}
		updated.BlockedReason = firstNonEmpty(updated.BlockedReason, "due follow-up is blocked")
	} else if requiresApproval {
		updated.CurrentState, updated.RequiresApproval, updated.ApprovalStatus = StateNeedsApproval, true, "pending"
		updated.ApprovalReason = firstNonEmpty(updated.ApprovalReason, "due open loop requires Robert decision")
		updated.NextAction = "review due follow-up proposal"
	} else {
		if updated.CurrentState == StateWaitingInput {
			updated.CurrentState, updated.BlockedReason = StateReady, ""
		}
		updated.NextAction = "execute due follow-up through workflow worker"
	}
	if item.CurrentState != updated.CurrentState {
		projection.Transition = &models.WorkflowTransition{
			ID: followUpRecordID(loop.ID, "transition"), WorkflowID: item.ID,
			FromState: item.CurrentState, ToState: updated.CurrentState, Trigger: "followup_worker",
			Actor: "workflow-followup", Approved: false, Reason: "due open loop triggered next action",
		}
	}
	return projection, nil
}
