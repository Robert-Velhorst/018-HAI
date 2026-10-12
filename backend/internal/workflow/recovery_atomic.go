package workflow

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"automation-hub-backend/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	workflowRecoveryMessage = "expired workflow claim moved to review because execution outcome is unknown"
	openLoopRecoveryMessage = "expired idempotent follow-up claim reopened for retry"
)

func claimRecoveryRootDB(r *GormRepository) error {
	if r == nil || r.DB == nil || r.DB.Config == nil || r.DB.Dialector == nil || r.DB.Dialector.Name() != "postgres" || r.DB.Statement == nil || r.DB.Statement.ConnPool == nil || r.DB.DryRun {
		return ErrClaimRecoveryContextUnavailable
	}
	if _, nested := r.DB.Statement.ConnPool.(gorm.TxCommitter); nested {
		return ErrClaimRecoveryContextUnavailable
	}
	return nil
}

func (r *GormRepository) claimRecoveryTransaction(run func(*gorm.DB) error) error {
	if err := claimRecoveryRootDB(r); err != nil {
		return err
	}
	parent := r.DB.Statement.Context
	if parent == nil {
		return ErrClaimRecoveryContextUnavailable
	}
	if err := parent.Err(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	return r.DB.WithContext(ctx).Transaction(run, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
}

func recoveryRecordID(id uuid.UUID, revision time.Time, claim, kind string) uuid.UUID {
	return uuid.NewSHA1(id, []byte("workflow-claim-recovery/v1/"+revision.UTC().Format(time.RFC3339Nano)+"/"+claim+"/"+kind))
}

func persistRecoveryEvidence(tx *gorm.DB, records ...any) error {
	for _, record := range records {
		if err := tx.Statement.Context.Err(); err != nil {
			return err
		}
		result := tx.Create(record)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("claim recovery evidence write was not acknowledged")
		}
	}
	return tx.Statement.Context.Err()
}

func (r *GormRepository) RecoverExpiredWorkflowClaimAtomic(item models.WorkflowItem, _ time.Time) (*models.WorkflowItem, bool, error) {
	if item.ID == uuid.Nil {
		return nil, false, ErrClaimRecoveryContextUnavailable
	}
	var recovered *models.WorkflowItem
	changed := false
	err := r.claimRecoveryTransaction(func(tx *gorm.DB) error {
		var current models.WorkflowItem
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND owner_identity = ? AND archived = ?", item.ID, item.OwnerIdentity, false).First(&current).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if current.CurrentState != StateInProgress || !current.UpdatedAt.Equal(item.UpdatedAt) || current.WorkerClaimID != item.WorkerClaimID {
			return nil
		}
		// Host clock skew or lock waits cannot make a live lease expire early.
		var databaseNow time.Time
		if err := tx.Raw("SELECT clock_timestamp()").Scan(&databaseNow).Error; err != nil {
			return err
		}
		if databaseNow.IsZero() {
			return fmt.Errorf("claim recovery database clock is unavailable")
		}
		recovered, changed, err = (&GormRepository{DB: tx}).RecoverExpiredWorkflowClaim(item, databaseNow)
		if err != nil {
			return err
		}
		if !changed {
			return nil
		}
		if recovered == nil {
			return ErrClaimRecoveryOutcomeUnconfirmed
		}
		transition := models.WorkflowTransition{ID: recoveryRecordID(item.ID, item.UpdatedAt, item.WorkerClaimID, "transition"), WorkflowID: item.ID, FromState: StateInProgress, ToState: StateBlocked, Trigger: "worker_lease_expired", Actor: "workflow-recovery", Approved: recovered.ApprovalStatus == "approved", Reason: workflowRecoveryMessage}
		decision := models.WorkflowDecision{ID: recoveryRecordID(item.ID, item.UpdatedAt, item.WorkerClaimID, "decision"), WorkflowID: item.ID, DecisionType: "worker_recovery", Decision: "blocked", Reason: workflowRecoveryMessage, RuleApplied: "unknown external side effects require human review", Actor: "workflow-recovery"}
		event := models.WorkflowEvent{ID: recoveryRecordID(item.ID, item.UpdatedAt, item.WorkerClaimID, "event"), WorkflowID: item.ID, EventType: "workflow.worker_recovered", FromState: StateInProgress, ToState: StateBlocked, Message: workflowRecoveryMessage, Trigger: "worker_lease_expired", RuleApplied: "claim lease recovery", SourceURI: recovered.SourceURI, Actor: "workflow-recovery"}
		return persistRecoveryEvidence(tx, &transition, &decision, &event)
	})
	if err != nil {
		return nil, false, err
	}
	return recovered, changed, nil
}

func (r *GormRepository) RecoverExpiredOpenLoopClaimAtomic(owner string, loop models.WorkflowOpenLoop, _ time.Time) (*models.WorkflowOpenLoop, bool, error) {
	if loop.ID == uuid.Nil || loop.WorkflowID == uuid.Nil {
		return nil, false, ErrClaimRecoveryContextUnavailable
	}
	var recovered *models.WorkflowOpenLoop
	changed := false
	err := r.claimRecoveryTransaction(func(tx *gorm.DB) error {
		// Same workflow-first lock order as follow-up projection and retraction.
		parent, current, err := lockDueOpenLoop(tx, owner, loop.WorkflowID, loop.ID)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if parent.Archived || current.Status != "processing" || !current.UpdatedAt.Equal(loop.UpdatedAt) || current.ClaimID != loop.ClaimID {
			return nil
		}
		var databaseNow time.Time
		if err := tx.Raw("SELECT clock_timestamp()").Scan(&databaseNow).Error; err != nil {
			return err
		}
		if databaseNow.IsZero() {
			return fmt.Errorf("claim recovery database clock is unavailable")
		}
		recovered, changed, err = (&GormRepository{DB: tx}).RecoverExpiredOpenLoopClaim(loop, databaseNow)
		if err != nil {
			return err
		}
		if !changed {
			return nil
		}
		if recovered == nil {
			return ErrClaimRecoveryOutcomeUnconfirmed
		}
		decision := models.WorkflowDecision{ID: recoveryRecordID(loop.ID, loop.UpdatedAt, loop.ClaimID, "decision"), WorkflowID: loop.WorkflowID, DecisionType: "open_loop_recovery", Decision: "reopened", Reason: openLoopRecoveryMessage, RuleApplied: "idempotent follow-up artifacts", Actor: "workflow-recovery"}
		event := models.WorkflowEvent{ID: recoveryRecordID(loop.ID, loop.UpdatedAt, loop.ClaimID, "event"), WorkflowID: loop.WorkflowID, EventType: "workflow.open_loop_recovered", Message: openLoopRecoveryMessage, Trigger: "open_loop_lease_expired", RuleApplied: "claim lease recovery", Actor: "workflow-recovery"}
		return persistRecoveryEvidence(tx, &decision, &event)
	})
	if err != nil {
		return nil, false, err
	}
	return recovered, changed, nil
}
