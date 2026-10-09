package workflow

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"

	"automation-hub-backend/internal/models"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrWorkflowPostgresTransactionRequired = errors.New("workflow source retraction requires a PostgreSQL repository and active transaction from the same database")

// SourceRetractionPostCommitProjection projects a committed workflow update
// into the advisory life graph. It must only be invoked after tx commits.
type SourceRetractionPostCommitProjection func(context.Context) error

// TransactionalSourceRetraction exposes the transaction-aware retraction path
// without changing the existing, non-transactional Service contract.
type TransactionalSourceRetraction interface {
	RetractSourceInTransaction(
		tx *gorm.DB,
		ownerIdentity string,
		actorIdentity string,
		sourceType string,
		sourceID string,
		reason string,
	) (SourceRetractionPostCommitProjection, error)
}

// RetractSourceInTransaction retracts only the active workflow with the exact
// source type and ID. Durable workflow and audit writes use the caller's
// transaction. On any error, the caller must roll back that transaction.
// Advisory life-graph projection is returned separately for post-commit use.
func (s *service) RetractSourceInTransaction(
	tx *gorm.DB,
	ownerIdentity string,
	actorIdentity string,
	sourceType string,
	sourceID string,
	reason string,
) (SourceRetractionPostCommitProjection, error) {
	if s == nil {
		return nil, ErrWorkflowPostgresTransactionRequired
	}
	baseRepository, ok := s.repo.(*GormRepository)
	if !ok || baseRepository == nil || baseRepository.DB == nil ||
		!sameWorkflowPostgresTransaction(baseRepository.DB, tx) {
		return nil, ErrWorkflowPostgresTransactionRequired
	}

	ownerIdentity = strings.TrimSpace(ownerIdentity)
	actorIdentity = strings.TrimSpace(actorIdentity)
	sourceType = strings.TrimSpace(sourceType)
	sourceID = strings.TrimSpace(sourceID)
	if ownerIdentity == "" || actorIdentity == "" || sourceType == "" || sourceID == "" {
		return nil, fmt.Errorf("owner, actor, source type, and source id are required")
	}

	transactionalRepository := &GormRepository{DB: tx}
	item, err := findActiveWorkflowBySourceIdentityForUpdate(transactionalRepository, ownerIdentity, sourceType, sourceID)
	if err != nil || item == nil {
		return nil, err
	}
	reason = firstNonEmpty(strings.TrimSpace(reason), "source record was retracted")
	if item.CurrentState == StateInProgress {
		return nil, fmt.Errorf("source workflow is currently in progress and requires interruption review before retraction")
	}
	if item.CurrentState == StateCompleted || item.CurrentState == StateArchived {
		if _, err := transactionalRepository.CreateEvent(&models.WorkflowEvent{
			WorkflowID:  item.ID,
			EventType:   "workflow.source_retracted_after_completion",
			FromState:   item.CurrentState,
			ToState:     item.CurrentState,
			Message:     reason,
			Trigger:     "source_retraction",
			RuleApplied: "completed workflow retained for audit",
			SourceURI:   item.SourceURI,
			Actor:       actorIdentity,
		}); err != nil {
			return nil, fmt.Errorf("record completed source workflow retraction: %w", err)
		}
		return nil, nil
	}

	from := item.CurrentState
	item.CurrentState = StateBlocked
	item.BlockedReason = reason
	item.NextAction = "review the retracted source record before any further execution"
	item.NextRunAt = nil
	// A recovered lease may still belong to a live runner. Keep its durable
	// fence until that runner returns or an explicit recovery decision reconciles
	// the prior external outcome.
	item.VerificationStatus = "needs_review"
	quarantineRetractedWorkflow(item, reason)
	if _, err := transactionalRepository.UpdateItem(item); err != nil {
		return nil, fmt.Errorf("update retracted source workflow: %w", err)
	}
	if _, err := transactionalRepository.CreateTransition(&models.WorkflowTransition{
		WorkflowID: item.ID,
		FromState:  from,
		ToState:    StateBlocked,
		Trigger:    "source_retraction",
		Actor:      actorIdentity,
		Approved:   false,
		Reason:     reason,
	}); err != nil {
		return nil, fmt.Errorf("record source retraction transition: %w", err)
	}
	if _, err := transactionalRepository.CreateDecision(&models.WorkflowDecision{
		WorkflowID:   item.ID,
		DecisionType: "source_retraction",
		Decision:     "blocked",
		Reason:       reason,
		RuleApplied:  "source-derived work must stop when its evidence is retracted",
		Approved:     false,
		Actor:        actorIdentity,
	}); err != nil {
		return nil, fmt.Errorf("record source retraction decision: %w", err)
	}
	if _, err := transactionalRepository.CreateEvent(&models.WorkflowEvent{
		WorkflowID:  item.ID,
		EventType:   "workflow.source_retracted",
		FromState:   from,
		ToState:     StateBlocked,
		Message:     reason,
		Trigger:     "source_retraction",
		RuleApplied: "source identity retraction",
		SourceURI:   item.SourceURI,
		Actor:       actorIdentity,
	}); err != nil {
		return nil, fmt.Errorf("record source retraction event: %w", err)
	}
	if s.lifeOntologyProjector == nil {
		return nil, nil
	}

	workflowID := item.ID
	var once sync.Once
	var projectionErr error
	return func(projectionContext context.Context) error {
		once.Do(func() {
			if projectionContext == nil {
				projectionContext = context.Background()
			}
			projectionErr = s.projectWorkflowToLifeGraph(projectionContext, workflowID)
		})
		return projectionErr
	}, nil
}

func findActiveWorkflowBySourceIdentityForUpdate(
	repository *GormRepository,
	ownerIdentity string,
	sourceType string,
	sourceID string,
) (*models.WorkflowItem, error) {
	var item models.WorkflowItem
	err := repository.DB.Clauses(clause.Locking{Strength: "UPDATE"}).Where(
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
		return nil, fmt.Errorf("find active workflow by exact source identity: %w", err)
	}
	return &item, nil
}

func sameWorkflowPostgresTransaction(database, tx *gorm.DB) bool {
	if database == nil || tx == nil || database.Dialector == nil || tx.Dialector == nil ||
		database.Dialector.Name() != "postgres" || tx.Dialector.Name() != "postgres" ||
		database.Error != nil || tx.Error != nil || database.Statement == nil || tx.Statement == nil {
		return false
	}
	if _, alreadyTransactional := database.Statement.ConnPool.(gorm.TxCommitter); alreadyTransactional {
		return false
	}
	committer, ok := tx.Statement.ConnPool.(gorm.TxCommitter)
	if !ok || committer == nil {
		return false
	}
	committerValue := reflect.ValueOf(committer)
	if (committerValue.Kind() == reflect.Ptr || committerValue.Kind() == reflect.Interface) && committerValue.IsNil() {
		return false
	}

	databasePool, err := database.DB()
	if err != nil || databasePool == nil {
		return false
	}
	txPool, err := tx.DB()
	return err == nil && txPool != nil && txPool == databasePool
}
