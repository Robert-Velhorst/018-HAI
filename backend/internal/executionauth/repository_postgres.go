package executionauth

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"automation-hub-backend/internal/infra"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
)

// PostgresRepository stores immutable authorization receipts and their
// single-use consumptions in the canonical PostgreSQL database.
type PostgresRepository struct {
	DB *gorm.DB
}

var _ Repository = (*PostgresRepository)(nil)

func NewPostgresRepository(db *gorm.DB) *PostgresRepository {
	return &PostgresRepository{DB: db}
}

// DefaultRepository opens the configured database, applies versioned
// migrations, and returns the durable execution-authorization repository.
func DefaultRepository() (Repository, error) {
	db, err := infra.GetDefaultDB()
	if err != nil {
		return nil, fmt.Errorf("open execution authorization database: %w", err)
	}
	return NewPostgresRepository(db), nil
}

func (r *PostgresRepository) CreateOrGet(
	ctx context.Context,
	receipt Receipt,
) (Receipt, bool, error) {
	if err := r.ready(); err != nil {
		return Receipt{}, false, err
	}
	evidence, references, err := encodeReceipt(receipt)
	if err != nil {
		return Receipt{}, false, err
	}
	db, finish, err := infra.PostgresExecutionDB(ctx, r.DB)
	if err != nil {
		return Receipt{}, false, err
	}
	defer finish()

	result := db.Exec(`
		INSERT INTO public.execution_authorization_receipts (
			id, contract_version, owner_identity, idempotency_key,
			actor_identity, actor_kind, task_id, action, stage,
			resource_type, resource_id, project_key, domain, runtime_id,
			approval_source_id, effect_digest, outcome, reason,
			request_digest, decision_digest, required_authority,
			requested_autonomy, effective_autonomy, risk, reversible,
			estimated_cost_eur, notification_required, evaluated_at,
			evidence_json, constitution_id, constitution_version,
			constitution_digest, mandate_id, mandate_decision_id,
			agent_id, assignment_id, task_review_decision_id,
			workflow_decision_id, portfolio_proposal_decision_id
		) VALUES (
			?, ?, ?, ?, ?, ?, ?, ?, ?, ?,
			?, ?, ?, ?, ?, ?, ?, ?, ?, ?,
			?, ?, ?, ?, ?, ?, ?, ?, CAST(? AS jsonb),
			?, ?, ?, ?, ?, ?, ?, ?, ?, ?
		)
		ON CONFLICT (owner_identity, idempotency_key) DO NOTHING`,
		receipt.ID,
		receipt.ContractVersion,
		receipt.OwnerIdentity,
		receipt.IdempotencyKey,
		receipt.ActorIdentity,
		string(receipt.ActorKind),
		receipt.TaskID,
		receipt.Action,
		string(receipt.Stage),
		receipt.ResourceType,
		receipt.ResourceID,
		receipt.ProjectKey,
		receipt.Domain,
		receipt.RuntimeID,
		receipt.ApprovalSourceID,
		receipt.EffectDigest,
		string(receipt.Outcome),
		receipt.Reason,
		receipt.RequestDigest,
		receipt.DecisionDigest,
		receipt.RequiredAuthority,
		receipt.RequestedAutonomy,
		receipt.EffectiveAutonomy,
		string(receipt.Risk),
		receipt.Reversible,
		receipt.EstimatedCostEUR,
		receipt.NotificationRequired,
		receipt.EvaluatedAt.UTC(),
		string(evidence),
		references.constitutionID,
		references.constitutionVersion,
		references.constitutionDigest,
		references.mandateID,
		references.mandateDecisionID,
		references.agentID,
		references.assignmentID,
		references.taskReviewDecisionID,
		references.workflowDecisionID,
		references.portfolioProposalDecisionID,
	)
	if result.Error != nil {
		return Receipt{}, false, fmt.Errorf(
			"create execution authorization receipt: %w",
			result.Error,
		)
	}
	if result.RowsAffected == 1 {
		return cloneReceipt(receipt), true, nil
	}

	existing, err := getReceiptByIdempotencyFromPostgres(
		db,
		receipt.OwnerIdentity,
		receipt.IdempotencyKey,
	)
	if err != nil {
		return Receipt{}, false, fmt.Errorf(
			"resolve execution authorization idempotency: %w",
			err,
		)
	}
	if existing.RequestDigest != receipt.RequestDigest {
		return Receipt{}, false, ErrIdempotencyConflict
	}
	return existing, false, nil
}

func (r *PostgresRepository) Get(
	ctx context.Context,
	owner string,
	id uuid.UUID,
) (Receipt, error) {
	if err := r.ready(); err != nil {
		return Receipt{}, err
	}
	if err := validateReceiptLookup(owner, id); err != nil {
		return Receipt{}, err
	}
	db, finish, err := infra.PostgresExecutionDB(ctx, r.DB)
	if err != nil {
		return Receipt{}, err
	}
	defer finish()
	return getReceiptFromPostgres(db, owner, id)
}

func getReceiptFromPostgres(db *gorm.DB, owner string, id uuid.UUID) (Receipt, error) {
	row := db.Raw(
		receiptSelect+`
		WHERE owner_identity = ? AND id = ?`,
		strings.TrimSpace(owner),
		id,
	).Row()
	receipt, err := scanReceipt(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Receipt{}, ErrNotFound
	}
	if err != nil {
		return Receipt{}, fmt.Errorf("get execution authorization receipt: %w", err)
	}
	return receipt, nil
}

func (r *PostgresRepository) List(
	ctx context.Context,
	owner string,
	limit int,
) ([]Receipt, error) {
	if err := r.ready(); err != nil {
		return nil, err
	}
	owner = strings.TrimSpace(owner)
	if err := validateIdentifier("owner identity", owner); err != nil {
		return nil, err
	}
	db, finish, err := infra.PostgresExecutionDB(ctx, r.DB)
	if err != nil {
		return nil, err
	}
	defer finish()
	rows, err := db.Raw(
		receiptSelect+`
		WHERE owner_identity = ?
		ORDER BY evaluated_at DESC, id DESC
		LIMIT ?`,
		owner,
		boundedLimit(limit),
	).Rows()
	if err != nil {
		return nil, fmt.Errorf("list execution authorization receipts: %w", err)
	}
	defer rows.Close()

	receipts := make([]Receipt, 0)
	for rows.Next() {
		receipt, err := scanReceipt(rows)
		if err != nil {
			return nil, fmt.Errorf("scan execution authorization receipt: %w", err)
		}
		receipts = append(receipts, receipt)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate execution authorization receipts: %w", err)
	}
	return receipts, nil
}

// Consume atomically verifies an authorized receipt and claims its one allowed
// execution. The INSERT ... SELECT and owner-scoped primary key ensure that at
// most one concurrent caller can succeed.
func (r *PostgresRepository) Consume(
	ctx context.Context,
	consumption Consumption,
) error {
	if err := r.ready(); err != nil {
		return err
	}
	if err := validateConsumption(consumption); err != nil {
		return err
	}
	db, finish, err := infra.PostgresExecutionDB(ctx, r.DB)
	if err != nil {
		return err
	}
	defer finish()
	return db.Transaction(func(tx *gorm.DB) error {
		return consumeInPostgresTransaction(db.Statement.Context, tx, consumption)
	})
}

func consumeInPostgresTransaction(
	ctx context.Context,
	tx *gorm.DB,
	consumption Consumption,
) error {
	if tx == nil {
		return ErrPostgresTransactionRequired
	}
	if err := lockAndCheckApprovalClaim(ctx, tx, consumption); err != nil {
		return err
	}
	result := tx.WithContext(ctx).Exec(`
		INSERT INTO public.execution_authorization_consumptions (
			owner_identity, receipt_id, consumer, execution_target,
			receipt_digest, consumed_at
		)
		SELECT
			owner_identity, id, ?, ?, decision_digest, ?
		FROM public.execution_authorization_receipts
		WHERE owner_identity = ?
		  AND id = ?
		  AND outcome = 'authorized'
		  AND decision_digest = ?
		  AND (approval_source_id = '' OR COALESCE(
		    NULLIF(evidence_json #>> '{approval,expiresAt}', '')::timestamptz > clock_timestamp(),
		    FALSE
		  ))
		  AND (approval_source_id = '' OR COALESCE(
		    NULLIF(evidence_json #>> '{approval,expiresAt}', '')::timestamptz > ?,
		    FALSE
		  ))
		ON CONFLICT DO NOTHING`,
		consumption.Consumer,
		consumption.ExecutionTarget,
		consumption.ConsumedAt.UTC(),
		consumption.OwnerIdentity,
		consumption.ReceiptID,
		consumption.ReceiptDigest,
		consumption.ConsumedAt.UTC(),
	)
	if result.Error != nil {
		var postgresError *pgconn.PgError
		if errors.As(result.Error, &postgresError) &&
			postgresError.Code == "23505" {
			switch postgresError.ConstraintName {
			case "uq_execution_authorization_task_review_claim_decision",
				"uq_execution_authorization_workflow_decision_claim_decision":
				return ErrApprovalAlreadyClaimed
			}
		}
		return fmt.Errorf("consume execution authorization receipt: %w", result.Error)
	}
	if result.RowsAffected == 1 {
		var taskReviewDecisionID, workflowDecisionID sql.NullString
		err := tx.WithContext(ctx).Raw(`
			SELECT task_review_decision_id::text, workflow_decision_id::text
			FROM public.execution_authorization_receipts
			WHERE owner_identity = ? AND id = ? AND decision_digest = ?`,
			consumption.OwnerIdentity,
			consumption.ReceiptID,
			consumption.ReceiptDigest,
		).Row().Scan(&taskReviewDecisionID, &workflowDecisionID)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("read consumed task-review approval reference: %w", err)
		}
		if taskReviewDecisionID.Valid {
			claim := tx.WithContext(ctx).Exec(`
				INSERT INTO public.execution_authorization_task_review_claims (
					owner_identity, task_review_decision_id, receipt_id, claimed_at
				)
				SELECT owner_identity, task_review_decision_id, id, ?
				FROM public.execution_authorization_receipts
				WHERE owner_identity = ? AND id = ? AND decision_digest = ?
				  AND task_review_decision_id = ?
				ON CONFLICT (owner_identity, task_review_decision_id) DO NOTHING`,
				consumption.ConsumedAt.UTC(),
				consumption.OwnerIdentity,
				consumption.ReceiptID,
				consumption.ReceiptDigest,
				taskReviewDecisionID.String,
			)
			if claim.Error != nil {
				return fmt.Errorf("claim task-review approval decision: %w", claim.Error)
			}
			if claim.RowsAffected == 0 {
				var claimedReceipt uuid.UUID
				err = tx.WithContext(ctx).Raw(`
					SELECT receipt_id
					FROM public.execution_authorization_task_review_claims
					WHERE owner_identity = ? AND task_review_decision_id = ?`,
					consumption.OwnerIdentity,
					taskReviewDecisionID.String,
				).Row().Scan(&claimedReceipt)
				if err == nil {
					if claimedReceipt != consumption.ReceiptID {
						return ErrApprovalAlreadyClaimed
					}
				} else if !errors.Is(err, sql.ErrNoRows) {
					return fmt.Errorf("inspect task-review approval claim: %w", err)
				} else {
					return ErrAuthorizationChanged
				}
			}
		}

		if workflowDecisionID.Valid {
			claim := tx.WithContext(ctx).Exec(`
				INSERT INTO public.execution_authorization_workflow_decision_claims (
					owner_identity, workflow_decision_id, receipt_id, claimed_at
				)
				SELECT owner_identity, workflow_decision_id, id, ?
				FROM public.execution_authorization_receipts
				WHERE owner_identity = ? AND id = ? AND decision_digest = ?
				  AND workflow_decision_id = ?
				ON CONFLICT (owner_identity, workflow_decision_id) DO NOTHING`,
				consumption.ConsumedAt.UTC(),
				consumption.OwnerIdentity,
				consumption.ReceiptID,
				consumption.ReceiptDigest,
				workflowDecisionID.String,
			)
			if claim.Error != nil {
				return fmt.Errorf("claim workflow approval decision: %w", claim.Error)
			}
			if claim.RowsAffected == 0 {
				var claimedReceipt uuid.UUID
				err = tx.WithContext(ctx).Raw(`
					SELECT receipt_id
					FROM public.execution_authorization_workflow_decision_claims
					WHERE owner_identity = ? AND workflow_decision_id = ?`,
					consumption.OwnerIdentity,
					workflowDecisionID.String,
				).Row().Scan(&claimedReceipt)
				if err == nil {
					if claimedReceipt != consumption.ReceiptID {
						return ErrApprovalAlreadyClaimed
					}
				} else if !errors.Is(err, sql.ErrNoRows) {
					return fmt.Errorf("inspect workflow approval claim: %w", err)
				} else {
					return ErrAuthorizationChanged
				}
			}
		}
		return nil
	}

	var outcome string
	var decisionDigest string
	var taskReviewDecisionID sql.NullString
	var approvalExpired bool
	err := tx.WithContext(ctx).Raw(`
		SELECT outcome, decision_digest, task_review_decision_id::text,
		       approval_source_id <> '' AND (
		         COALESCE(NULLIF(evidence_json #>> '{approval,expiresAt}', '')::timestamptz <= clock_timestamp(), TRUE)
		         OR COALESCE(NULLIF(evidence_json #>> '{approval,expiresAt}', '')::timestamptz <= ?, TRUE)
		       )
		FROM public.execution_authorization_receipts
		WHERE owner_identity = ? AND id = ?`,
		consumption.ConsumedAt.UTC(),
		consumption.OwnerIdentity,
		consumption.ReceiptID,
	).Row().Scan(&outcome, &decisionDigest, &taskReviewDecisionID, &approvalExpired)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("inspect unconsumed authorization receipt: %w", err)
	}
	if Outcome(outcome) != OutcomeAuthorized ||
		decisionDigest != consumption.ReceiptDigest {
		return ErrNotAuthorized
	}
	if approvalExpired {
		return ErrAuthorizationChanged
	}

	var consumedReceipt uuid.UUID
	err = tx.WithContext(ctx).Raw(`
		SELECT receipt_id
		FROM public.execution_authorization_consumptions
		WHERE owner_identity = ? AND receipt_id = ?`,
		consumption.OwnerIdentity,
		consumption.ReceiptID,
	).Row().Scan(&consumedReceipt)
	if err == nil {
		return ErrAlreadyConsumed
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("inspect execution authorization consumption: %w", err)
	}
	if taskReviewDecisionID.Valid {
		var claimedReceipt uuid.UUID
		err = tx.WithContext(ctx).Raw(`
			SELECT receipt_id
			FROM public.execution_authorization_task_review_claims
			WHERE owner_identity = ? AND task_review_decision_id = ?`,
			consumption.OwnerIdentity,
			taskReviewDecisionID.String,
		).Row().Scan(&claimedReceipt)
		if err == nil {
			return ErrApprovalAlreadyClaimed
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("inspect task review approval claim: %w", err)
		}
	}
	return ErrAlreadyConsumed
}

// lockAndCheckApprovalClaim makes every approval source single-use, including
// source types that do not yet have dedicated claim tables. The transaction
// lock serializes competing consumers across processes; immutable receipt and
// consumption rows provide the durable claim record.
func lockAndCheckApprovalClaim(
	ctx context.Context,
	tx *gorm.DB,
	consumption Consumption,
) error {
	var approvalSourceID, decisionID string
	err := tx.WithContext(ctx).Raw(`
		SELECT approval_source_id, COALESCE(evidence_json #>> '{approval,decisionId}', '')
		FROM public.execution_authorization_receipts
		WHERE owner_identity = ? AND id = ? AND outcome = 'authorized'
		  AND decision_digest = ?`,
		consumption.OwnerIdentity,
		consumption.ReceiptID,
		consumption.ReceiptDigest,
	).Row().Scan(&approvalSourceID, &decisionID)
	if errors.Is(err, sql.ErrNoRows) || approvalSourceID == "" {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read approval claim identity: %w", err)
	}
	if decisionID == "" {
		return ErrAuthorizationChanged
	}
	if strings.HasPrefix(approvalSourceID, "task-review:") ||
		strings.HasPrefix(approvalSourceID, "workflow-decision:") {
		return nil
	}
	var isolation string
	if err := tx.WithContext(ctx).Raw("SHOW transaction_isolation").Row().Scan(&isolation); err != nil {
		return fmt.Errorf("read approval claim transaction isolation: %w", err)
	}
	if !strings.EqualFold(strings.TrimSpace(isolation), "read committed") {
		return ErrApprovalClaimIsolationUnsupported
	}
	claimIdentity := consumption.OwnerIdentity + "\x00" + approvalSourceID + "\x00" + decisionID
	var lockAcquired bool
	if err := tx.WithContext(ctx).Raw(
		"SELECT pg_advisory_xact_lock(hashtextextended(?, 0)) IS NULL",
		claimIdentity,
	).Row().Scan(&lockAcquired); err != nil {
		return fmt.Errorf("lock execution approval claim: %w", err)
	}
	if !lockAcquired {
		return ErrAuthorizationChanged
	}
	var alreadyClaimed bool
	err = tx.WithContext(ctx).Raw(`
		SELECT EXISTS (
			SELECT 1
			FROM public.execution_authorization_receipts r
			JOIN public.execution_authorization_consumptions c
			  ON c.owner_identity = r.owner_identity
			 AND c.receipt_id = r.id
			 AND c.receipt_digest = r.decision_digest
			WHERE r.owner_identity = ?
			  AND r.approval_source_id = ?
			  AND r.evidence_json #>> '{approval,decisionId}' = ?
			  AND r.outcome = 'authorized'
			  AND r.id <> ?
		)`,
		consumption.OwnerIdentity,
		approvalSourceID,
		decisionID,
		consumption.ReceiptID,
	).Row().Scan(&alreadyClaimed)
	if err != nil {
		return fmt.Errorf("inspect prior execution approval claim: %w", err)
	}
	if alreadyClaimed {
		return ErrApprovalAlreadyClaimed
	}
	return nil
}

func (r *PostgresRepository) GetConsumption(
	ctx context.Context,
	owner string,
	receiptID uuid.UUID,
) (Consumption, error) {
	if err := r.ready(); err != nil {
		return Consumption{}, err
	}
	if err := validateReceiptLookup(owner, receiptID); err != nil {
		return Consumption{}, err
	}
	db, finish, err := infra.PostgresExecutionDB(ctx, r.DB)
	if err != nil {
		return Consumption{}, err
	}
	defer finish()
	return getConsumptionFromPostgres(db, owner, receiptID)
}

func getConsumptionFromPostgres(db *gorm.DB, owner string, receiptID uuid.UUID) (Consumption, error) {
	var result Consumption
	err := db.Raw(`
		SELECT
			receipt_id, owner_identity, consumer, execution_target,
			receipt_digest, consumed_at
		FROM public.execution_authorization_consumptions
		WHERE owner_identity = ? AND receipt_id = ?`,
		strings.TrimSpace(owner),
		receiptID,
	).Row().Scan(
		&result.ReceiptID,
		&result.OwnerIdentity,
		&result.Consumer,
		&result.ExecutionTarget,
		&result.ReceiptDigest,
		&result.ConsumedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Consumption{}, ErrNotFound
	}
	if err != nil {
		return Consumption{}, fmt.Errorf("get execution authorization consumption: %w", err)
	}
	result.ConsumedAt = result.ConsumedAt.UTC()
	if err := validateConsumption(result); err != nil {
		return Consumption{}, fmt.Errorf("stored consumption is invalid: %w", err)
	}
	return result, nil
}

func (r *PostgresRepository) ExerciseFinalEffect(
	ctx context.Context,
	exercise FinalEffectExercise,
) error {
	if err := r.ready(); err != nil {
		return err
	}
	if err := validateFinalEffectExercise(exercise); err != nil {
		return err
	}
	db, finish, err := infra.PostgresExecutionDB(ctx, r.DB)
	if err != nil {
		return err
	}
	defer finish()
	result := db.Exec(`
		WITH boundary AS MATERIALIZED (
			SELECT clock_timestamp() AS exercised_at
		)
		INSERT INTO public.execution_authorization_final_effect_exercises (
			owner_identity, receipt_id, runtime_id, task_id, action,
			resource_type, resource_id, project_key, approval_source_id,
			effect_digest, authorization_request_digest, decision_digest,
			runtime_request_digest, consumption_target, exercised_at
		)
		SELECT
			r.owner_identity, r.id, r.runtime_id, r.task_id, r.action,
			r.resource_type, r.resource_id, r.project_key,
			r.approval_source_id, r.effect_digest, r.request_digest,
			r.decision_digest, ?, c.execution_target, boundary.exercised_at
		FROM public.execution_authorization_receipts r
		JOIN public.execution_authorization_consumptions c
		  ON c.owner_identity = r.owner_identity
		 AND c.receipt_id = r.id
		 AND c.receipt_digest = r.decision_digest
		CROSS JOIN boundary
		WHERE r.owner_identity = ?
		  AND r.id = ?
		  AND r.outcome = 'authorized'
		  AND r.runtime_id = ?
		  AND r.task_id = ?
		  AND r.action = ?
		  AND r.resource_type = ?
		  AND r.resource_id = ?
		  AND r.project_key = ?
		  AND r.approval_source_id = ?
		  AND r.effect_digest = ?
		  AND r.request_digest = ?
		  AND r.decision_digest = ?
		  AND ? = r.effect_digest
		  AND c.execution_target = ?
		  AND c.consumed_at >= r.evaluated_at
		  AND c.consumed_at <= boundary.exercised_at
		  AND c.consumed_at >= boundary.exercised_at - (?::double precision * INTERVAL '1 second')
		  AND (
		    r.approval_source_id = ''
		    OR (
		      r.evidence_json #>> '{approval,sourceId}' = r.approval_source_id
		      AND NULLIF(r.evidence_json #>> '{approval,expiresAt}', '')::timestamptz > boundary.exercised_at
		    )
		  )
		ON CONFLICT (owner_identity, receipt_id) DO NOTHING`,
		exercise.RuntimeRequestDigest,
		exercise.OwnerIdentity,
		exercise.ReceiptID,
		exercise.RuntimeID,
		exercise.TaskID,
		exercise.Action,
		exercise.ResourceType,
		exercise.ResourceID,
		exercise.ProjectKey,
		exercise.ApprovalSourceID,
		exercise.EffectDigest,
		exercise.AuthorizationRequestDigest,
		exercise.DecisionDigest,
		exercise.RuntimeRequestDigest,
		exercise.ConsumptionTarget,
		finalEffectFreshnessWindow.Seconds(),
	)
	if result.Error != nil {
		return fmt.Errorf("exercise execution authorization final effect: %w", result.Error)
	}
	if result.RowsAffected == 1 {
		return nil
	}
	if _, err := getFinalEffectExerciseFromPostgres(
		db,
		exercise.OwnerIdentity,
		exercise.ReceiptID,
	); err == nil {
		return ErrAlreadyExercised
	} else if !errors.Is(err, ErrNotFound) {
		return err
	}
	receipt, err := getReceiptFromPostgres(db, exercise.OwnerIdentity, exercise.ReceiptID)
	if errors.Is(err, ErrNotFound) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	consumption, err := getConsumptionFromPostgres(
		db,
		exercise.OwnerIdentity,
		exercise.ReceiptID,
	)
	if errors.Is(err, ErrNotFound) {
		return ErrNotAuthorized
	}
	if err != nil {
		return err
	}
	if !finalEffectMatches(receipt, consumption, exercise) {
		return ErrFinalEffectMismatch
	}
	var boundaryTime time.Time
	if err := db.Raw("SELECT clock_timestamp()").Row().Scan(&boundaryTime); err != nil {
		return fmt.Errorf("read execution authorization final-effect time: %w", err)
	}
	exercise.ExercisedAt = boundaryTime.UTC()
	if !finalEffectAuthorityFresh(receipt, consumption, exercise.ExercisedAt) {
		return ErrFinalEffectExpired
	}
	return ErrNotAuthorized
}

func (r *PostgresRepository) GetFinalEffectExercise(
	ctx context.Context,
	owner string,
	receiptID uuid.UUID,
) (FinalEffectExercise, error) {
	if err := r.ready(); err != nil {
		return FinalEffectExercise{}, err
	}
	if err := validateReceiptLookup(owner, receiptID); err != nil {
		return FinalEffectExercise{}, err
	}
	db, finish, err := infra.PostgresExecutionDB(ctx, r.DB)
	if err != nil {
		return FinalEffectExercise{}, err
	}
	defer finish()
	return getFinalEffectExerciseFromPostgres(db, owner, receiptID)
}

func getFinalEffectExerciseFromPostgres(db *gorm.DB, owner string, receiptID uuid.UUID) (FinalEffectExercise, error) {
	var value FinalEffectExercise
	err := db.Raw(`
		SELECT
			receipt_id, owner_identity, runtime_id, task_id, action,
			resource_type, resource_id, project_key, approval_source_id,
			effect_digest, authorization_request_digest, decision_digest,
			runtime_request_digest, consumption_target, exercised_at
		FROM public.execution_authorization_final_effect_exercises
		WHERE owner_identity = ? AND receipt_id = ?`,
		strings.TrimSpace(owner),
		receiptID,
	).Row().Scan(
		&value.ReceiptID,
		&value.OwnerIdentity,
		&value.RuntimeID,
		&value.TaskID,
		&value.Action,
		&value.ResourceType,
		&value.ResourceID,
		&value.ProjectKey,
		&value.ApprovalSourceID,
		&value.EffectDigest,
		&value.AuthorizationRequestDigest,
		&value.DecisionDigest,
		&value.RuntimeRequestDigest,
		&value.ConsumptionTarget,
		&value.ExercisedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return FinalEffectExercise{}, ErrNotFound
	}
	if err != nil {
		return FinalEffectExercise{}, fmt.Errorf(
			"get execution authorization final effect exercise: %w",
			err,
		)
	}
	value.ExercisedAt = value.ExercisedAt.UTC()
	if err := validateFinalEffectExercise(value); err != nil {
		return FinalEffectExercise{}, fmt.Errorf(
			"stored final effect exercise is invalid: %w",
			err,
		)
	}
	return value, nil
}

func getReceiptByIdempotencyFromPostgres(
	db *gorm.DB,
	owner string,
	idempotencyKey string,
) (Receipt, error) {
	row := db.Raw(
		receiptSelect+`
		WHERE owner_identity = ? AND idempotency_key = ?`,
		owner,
		idempotencyKey,
	).Row()
	receipt, err := scanReceipt(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Receipt{}, ErrNotFound
	}
	if err != nil {
		return Receipt{}, err
	}
	return receipt, nil
}

func (r *PostgresRepository) ready() error {
	if r == nil || r.DB == nil {
		return fmt.Errorf("execution authorization database is required")
	}
	return nil
}

const receiptSelect = `
	SELECT
		id, contract_version, owner_identity, idempotency_key,
		actor_identity, actor_kind, task_id, action, stage,
		resource_type, resource_id, project_key, domain, runtime_id,
		approval_source_id, effect_digest, outcome, reason,
		request_digest, decision_digest, required_authority,
		requested_autonomy, effective_autonomy, risk, reversible,
		estimated_cost_eur, notification_required, evaluated_at,
		evidence_json, constitution_id, constitution_version,
		constitution_digest, mandate_id, mandate_decision_id,
		agent_id, assignment_id, task_review_decision_id,
		workflow_decision_id, portfolio_proposal_decision_id
	FROM public.execution_authorization_receipts
`

type receiptScanner interface {
	Scan(...any) error
}

func scanReceipt(scanner receiptScanner) (Receipt, error) {
	var receipt Receipt
	var evidence []byte
	var actorKind, stage, outcome, risk string
	var constitutionID, constitutionDigest sql.NullString
	var constitutionVersion sql.NullInt64
	var mandateID, mandateDecisionID sql.NullString
	var agentID, assignmentID sql.NullString
	var taskReviewDecisionID, workflowDecisionID, portfolioProposalDecisionID sql.NullString
	err := scanner.Scan(
		&receipt.ID,
		&receipt.ContractVersion,
		&receipt.OwnerIdentity,
		&receipt.IdempotencyKey,
		&receipt.ActorIdentity,
		&actorKind,
		&receipt.TaskID,
		&receipt.Action,
		&stage,
		&receipt.ResourceType,
		&receipt.ResourceID,
		&receipt.ProjectKey,
		&receipt.Domain,
		&receipt.RuntimeID,
		&receipt.ApprovalSourceID,
		&receipt.EffectDigest,
		&outcome,
		&receipt.Reason,
		&receipt.RequestDigest,
		&receipt.DecisionDigest,
		&receipt.RequiredAuthority,
		&receipt.RequestedAutonomy,
		&receipt.EffectiveAutonomy,
		&risk,
		&receipt.Reversible,
		&receipt.EstimatedCostEUR,
		&receipt.NotificationRequired,
		&receipt.EvaluatedAt,
		&evidence,
		&constitutionID,
		&constitutionVersion,
		&constitutionDigest,
		&mandateID,
		&mandateDecisionID,
		&agentID,
		&assignmentID,
		&taskReviewDecisionID,
		&workflowDecisionID,
		&portfolioProposalDecisionID,
	)
	if err != nil {
		return Receipt{}, err
	}
	receipt.ActorKind = ActorKind(actorKind)
	receipt.Stage = Stage(stage)
	receipt.Outcome = Outcome(outcome)
	receipt.Risk = RiskLevel(risk)
	receipt.EvaluatedAt = receipt.EvaluatedAt.UTC()
	if err := json.Unmarshal(evidence, &receipt.Evidence); err != nil {
		return Receipt{}, fmt.Errorf("decode receipt evidence: %w", err)
	}
	if err := validateReceipt(receipt); err != nil {
		return Receipt{}, fmt.Errorf("stored receipt is invalid: %w", err)
	}
	references, err := receiptReferencesFromEvidence(receipt.Evidence)
	if err != nil {
		return Receipt{}, fmt.Errorf("stored receipt references are invalid: %w", err)
	}
	if !references.match(
		constitutionID,
		constitutionVersion,
		constitutionDigest,
		mandateID,
		mandateDecisionID,
		agentID,
		assignmentID,
		taskReviewDecisionID,
		workflowDecisionID,
		portfolioProposalDecisionID,
	) {
		return Receipt{}, fmt.Errorf("stored receipt reference columns do not match evidence")
	}
	return cloneReceipt(receipt), nil
}

type receiptReferences struct {
	constitutionID              any
	constitutionVersion         any
	constitutionDigest          any
	mandateID                   any
	mandateDecisionID           any
	agentID                     any
	assignmentID                any
	taskReviewDecisionID        any
	workflowDecisionID          any
	portfolioProposalDecisionID any
}

// ownerControlApprovalSourcePrefix identifies the short-lived, signed approval
// used only to release an engaged emergency stop. It intentionally has no
// database decision row: the signature is verified by the approval resolver
// before the receipt is persisted, and the immutable receipt is its audit
// record. Keep this local to executionauth to avoid an import cycle with
// opscontrol, which depends on this package.
const ownerControlApprovalSourcePrefix = "opscontrol-owner:"

func encodeReceipt(receipt Receipt) ([]byte, receiptReferences, error) {
	if err := validateReceipt(receipt); err != nil {
		return nil, receiptReferences{}, err
	}
	evidence, err := json.Marshal(receipt.Evidence)
	if err != nil {
		return nil, receiptReferences{}, fmt.Errorf("encode receipt evidence: %w", err)
	}
	if len(evidence) < 2 || len(evidence) > 65536 || evidence[0] != '{' {
		return nil, receiptReferences{}, fmt.Errorf(
			"receipt evidence must be a bounded JSON object",
		)
	}
	references, err := receiptReferencesFromEvidence(receipt.Evidence)
	if err != nil {
		return nil, receiptReferences{}, err
	}
	return evidence, references, nil
}

func receiptReferencesFromEvidence(
	evidence DecisionEvidence,
) (receiptReferences, error) {
	var result receiptReferences
	if evidence.Constitution.ID != "" ||
		evidence.Constitution.Version != 0 ||
		evidence.Constitution.Digest != "" {
		if evidence.Constitution.Version <= 0 ||
			!validDigest(evidence.Constitution.Digest) ||
			strings.TrimSpace(evidence.Constitution.ID) == "" {
			return receiptReferences{}, fmt.Errorf(
				"constitution evidence requires id, version, and SHA-256 digest",
			)
		}
		if !isBuiltinConstitutionEvidence(evidence.Constitution) {
			id, err := uuid.Parse(evidence.Constitution.ID)
			if err != nil {
				return receiptReferences{}, fmt.Errorf(
					"persisted constitution evidence requires a UUID",
				)
			}
			result.constitutionID = id
			result.constitutionVersion = evidence.Constitution.Version
			result.constitutionDigest = evidence.Constitution.Digest
		}
	}
	mandateID, err := optionalUUID("mandate id", evidence.Mandate.ID)
	if err != nil {
		return receiptReferences{}, err
	}
	result.mandateID = mandateID
	mandateDecisionID, err := optionalUUID(
		"mandate decision id",
		evidence.Mandate.DecisionID,
	)
	if err != nil {
		return receiptReferences{}, err
	}
	if mandateDecisionID != nil && mandateID == nil {
		return receiptReferences{}, fmt.Errorf(
			"mandate decision requires mandate evidence",
		)
	}
	result.mandateDecisionID = mandateDecisionID
	if evidence.Agent.AgentID != "" {
		if err := validateIdentifier("agent id", evidence.Agent.AgentID); err != nil {
			return receiptReferences{}, err
		}
		result.agentID = evidence.Agent.AgentID
	}
	if evidence.Agent.AssignmentID != "" {
		if result.agentID == nil {
			return receiptReferences{}, fmt.Errorf(
				"agent assignment requires agent evidence",
			)
		}
		if err := validateIdentifier(
			"assignment id",
			evidence.Agent.AssignmentID,
		); err != nil {
			return receiptReferences{}, err
		}
		result.assignmentID = evidence.Agent.AssignmentID
	}
	sourceID := strings.TrimSpace(evidence.Approval.SourceID)
	decisionID := strings.TrimSpace(evidence.Approval.DecisionID)
	switch {
	case decisionID == "" && sourceID == "":
	case decisionID == "" || sourceID == "":
		return receiptReferences{}, fmt.Errorf(
			"approval evidence requires source and decision ids",
		)
	case strings.HasPrefix(sourceID, ownerControlApprovalSourcePrefix):
		// Owner-control approvals are HMAC-signed, effect-bound, and verified
		// before this receipt reaches persistence. Unlike workflow and task
		// approvals, their decision identity is a digest rather than a foreign
		// key. The receipt evidence remains the durable audit record.
		if !validDigest(decisionID) {
			return receiptReferences{}, fmt.Errorf(
				"owner control approval decision id must be a SHA-256 digest",
			)
		}
	case strings.HasPrefix(sourceID, "task-review:"):
		approvalID, err := optionalUUID("approval decision id", decisionID)
		if err != nil {
			return receiptReferences{}, err
		}
		if _, err := uuid.Parse(strings.TrimPrefix(sourceID, "task-review:")); err != nil {
			return receiptReferences{}, fmt.Errorf(
				"task review approval source must identify its review item",
			)
		}
		result.taskReviewDecisionID = approvalID
	case strings.HasPrefix(sourceID, "workflow-decision:"):
		approvalID, err := optionalUUID("approval decision id", decisionID)
		if err != nil {
			return receiptReferences{}, err
		}
		if strings.TrimPrefix(sourceID, "workflow-decision:") !=
			referenceString(approvalID) {
			return receiptReferences{}, fmt.Errorf(
				"workflow approval source does not match its decision",
			)
		}
		result.workflowDecisionID = approvalID
	case strings.HasPrefix(sourceID, "portfolio-decision:"):
		approvalID, err := optionalUUID("approval decision id", decisionID)
		if err != nil {
			return receiptReferences{}, err
		}
		if strings.TrimPrefix(sourceID, "portfolio-decision:") !=
			referenceString(approvalID) {
			return receiptReferences{}, fmt.Errorf(
				"portfolio approval source does not match its decision",
			)
		}
		result.portfolioProposalDecisionID = approvalID
	default:
		return receiptReferences{}, fmt.Errorf(
			"approval source type is unsupported",
		)
	}
	return result, nil
}

func isBuiltinConstitutionEvidence(evidence ConstitutionEvidence) bool {
	return strings.HasPrefix(
		strings.ToLower(strings.TrimSpace(evidence.Source)),
		"builtin-",
	)
}

func optionalUUID(label, value string) (any, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	id, err := uuid.Parse(value)
	if err != nil {
		return nil, fmt.Errorf("%s must be a UUID", label)
	}
	return id, nil
}

func (r receiptReferences) match(
	constitutionID sql.NullString,
	constitutionVersion sql.NullInt64,
	constitutionDigest sql.NullString,
	mandateID sql.NullString,
	mandateDecisionID sql.NullString,
	agentID sql.NullString,
	assignmentID sql.NullString,
	taskReviewDecisionID sql.NullString,
	workflowDecisionID sql.NullString,
	portfolioProposalDecisionID sql.NullString,
) bool {
	return referenceString(r.constitutionID) == nullString(constitutionID) &&
		referenceInt(r.constitutionVersion) == nullInt(constitutionVersion) &&
		referenceString(r.constitutionDigest) == nullString(constitutionDigest) &&
		referenceString(r.mandateID) == nullString(mandateID) &&
		referenceString(r.mandateDecisionID) == nullString(mandateDecisionID) &&
		referenceString(r.agentID) == nullString(agentID) &&
		referenceString(r.assignmentID) == nullString(assignmentID) &&
		referenceString(r.taskReviewDecisionID) == nullString(taskReviewDecisionID) &&
		referenceString(r.workflowDecisionID) == nullString(workflowDecisionID) &&
		referenceString(r.portfolioProposalDecisionID) == nullString(portfolioProposalDecisionID)
}

func referenceString(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case uuid.UUID:
		return typed.String()
	default:
		return fmt.Sprint(typed)
	}
}

func referenceInt(value any) int64 {
	switch typed := value.(type) {
	case nil:
		return 0
	case int:
		return int64(typed)
	case int64:
		return typed
	default:
		return -1
	}
}

func nullString(value sql.NullString) string {
	if !value.Valid {
		return ""
	}
	return value.String
}

func nullInt(value sql.NullInt64) int64 {
	if !value.Valid {
		return 0
	}
	return value.Int64
}

func validateReceipt(receipt Receipt) error {
	if receipt.ID == uuid.Nil {
		return fmt.Errorf("receipt id is required")
	}
	if receipt.ContractVersion <= 0 {
		return fmt.Errorf("receipt contract version must be positive")
	}
	for label, value := range map[string]string{
		"owner identity":  receipt.OwnerIdentity,
		"idempotency key": receipt.IdempotencyKey,
		"actor identity":  receipt.ActorIdentity,
		"task id":         receipt.TaskID,
		"action":          receipt.Action,
		"resource type":   receipt.ResourceType,
	} {
		if err := validateIdentifier(label, value); err != nil {
			return err
		}
	}
	if len(receipt.ResourceID) > 256 {
		return fmt.Errorf("resource id exceeds 256 characters")
	}
	switch receipt.ActorKind {
	case ActorSystem, ActorAgent, ActorHuman:
	default:
		return fmt.Errorf("invalid receipt actor kind %q", receipt.ActorKind)
	}
	switch receipt.Stage {
	case StageDataAccess, StageToolUse, StageExpenditure, StageCommunication,
		StageCommitment, StageExecution, StagePublication, StageDeletion,
		StagePrivilegeEscalation, StageSelfModification:
	default:
		return fmt.Errorf("invalid receipt stage %q", receipt.Stage)
	}
	switch receipt.Outcome {
	case OutcomeAuthorized, OutcomeRequiresApproval, OutcomeDenied:
	default:
		return fmt.Errorf("invalid receipt outcome %q", receipt.Outcome)
	}
	if strings.TrimSpace(receipt.Reason) == "" || len(receipt.Reason) > 4096 {
		return fmt.Errorf("receipt reason must contain at most 4096 characters")
	}
	if !validDigest(receipt.RequestDigest) ||
		!validDigest(receipt.DecisionDigest) ||
		!validDigest(receipt.EffectDigest) {
		return fmt.Errorf(
			"receipt request, decision, and effect digests must be SHA-256 digests",
		)
	}
	if len(receipt.ProjectKey) > 256 || len(receipt.Domain) > 64 ||
		len(receipt.RuntimeID) > 256 ||
		len(receipt.ApprovalSourceID) > 256 {
		return fmt.Errorf("receipt execution provenance exceeds its bound")
	}
	verifiedApprovalSource := receipt.Evidence.Approval.SourceID
	if verifiedApprovalSource != "" &&
		receipt.ApprovalSourceID != verifiedApprovalSource {
		return fmt.Errorf("receipt approval source does not match evidence")
	}
	if receipt.Outcome == OutcomeAuthorized &&
		receipt.ApprovalSourceID != verifiedApprovalSource {
		return fmt.Errorf("authorized receipt approval source does not match verified evidence")
	}
	if receipt.RequiredAuthority < 0 || receipt.RequiredAuthority > 10 ||
		receipt.RequestedAutonomy < 0 || receipt.RequestedAutonomy > 10 ||
		receipt.EffectiveAutonomy < 0 || receipt.EffectiveAutonomy > 10 {
		return fmt.Errorf("receipt authority and autonomy must be between 0 and 10")
	}
	switch receipt.Risk {
	case RiskLow, RiskMedium, RiskHigh, RiskCritical:
	default:
		return fmt.Errorf("invalid receipt risk %q", receipt.Risk)
	}
	if math.IsNaN(receipt.EstimatedCostEUR) ||
		math.IsInf(receipt.EstimatedCostEUR, 0) ||
		receipt.EstimatedCostEUR < 0 ||
		receipt.EstimatedCostEUR > 1_000_000 {
		return fmt.Errorf("receipt estimated cost is invalid")
	}
	if receipt.EvaluatedAt.IsZero() {
		return fmt.Errorf("receipt evaluation timestamp is required")
	}
	return nil
}

func validateConsumption(consumption Consumption) error {
	if err := validateReceiptLookup(
		consumption.OwnerIdentity,
		consumption.ReceiptID,
	); err != nil {
		return err
	}
	if err := validateIdentifier("consumer", consumption.Consumer); err != nil {
		return err
	}
	target := strings.TrimSpace(consumption.ExecutionTarget)
	if target == "" || len(target) > 1024 {
		return fmt.Errorf("execution target must contain at most 1024 characters")
	}
	if !validDigest(consumption.ReceiptDigest) {
		return fmt.Errorf("consumption receipt digest must be a SHA-256 digest")
	}
	if consumption.ConsumedAt.IsZero() {
		return fmt.Errorf("consumption timestamp is required")
	}
	return nil
}

func validateReceiptLookup(owner string, id uuid.UUID) error {
	if err := validateIdentifier("owner identity", strings.TrimSpace(owner)); err != nil {
		return err
	}
	if id == uuid.Nil {
		return fmt.Errorf("receipt id is required")
	}
	return nil
}
