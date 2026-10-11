package opscontrol

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"automation-hub-backend/internal/infra"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type PostgresControlApprovalRepository struct {
	DB *gorm.DB
}

func NewPostgresControlApprovalRepository(db *gorm.DB) *PostgresControlApprovalRepository {
	return &PostgresControlApprovalRepository{DB: db}
}

func DefaultControlApprovalRepository() (ControlApprovalRepository, error) {
	db, err := infra.GetDefaultDB()
	if err != nil {
		return nil, fmt.Errorf("open opscontrol approval database: %w", err)
	}
	return NewPostgresControlApprovalRepository(db), nil
}

func (r *PostgresControlApprovalRepository) CreateRequest(ctx context.Context, value ControlApprovalRequest) error {
	if r == nil || r.DB == nil {
		return ErrControlApprovalUnavailable
	}
	if err := validateControlApprovalRequest(value); err != nil {
		return err
	}
	result := r.DB.WithContext(ctx).Exec(`
		INSERT INTO public.opscontrol_approval_requests (
			id, owner_identity, idempotency_key, task_id, action,
			resource_type, resource_id, target, binding_digest,
			created_by, created_at, expires_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		value.ID, value.OwnerIdentity, value.IdempotencyKey, value.TaskID,
		value.Action, value.ResourceType, value.ResourceID, value.Target,
		value.BindingDigest, value.CreatedBy, value.CreatedAt.UTC(),
		value.ExpiresAt.UTC(),
	)
	if result.Error != nil {
		return fmt.Errorf("%w: create request", ErrControlApprovalUnavailable)
	}
	return nil
}

func (r *PostgresControlApprovalRepository) FindRequest(ctx context.Context, owner string, id uuid.UUID) (ControlApprovalRequest, error) {
	if r == nil || r.DB == nil {
		return ControlApprovalRequest{}, ErrControlApprovalUnavailable
	}
	var value ControlApprovalRequest
	err := r.DB.WithContext(ctx).Raw(`
		SELECT id, owner_identity, idempotency_key, task_id, action,
		       resource_type, resource_id, target, binding_digest,
		       created_by, created_at, expires_at
		FROM public.opscontrol_approval_requests
		WHERE owner_identity = ? AND id = ?`, owner, id).Row().Scan(
		&value.ID, &value.OwnerIdentity, &value.IdempotencyKey, &value.TaskID,
		&value.Action, &value.ResourceType, &value.ResourceID, &value.Target,
		&value.BindingDigest, &value.CreatedBy, &value.CreatedAt, &value.ExpiresAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return ControlApprovalRequest{}, ErrControlApprovalNotFound
	}
	if err != nil {
		return ControlApprovalRequest{}, fmt.Errorf(
			"%w: find request",
			ErrControlApprovalUnavailable,
		)
	}
	value.CreatedAt = value.CreatedAt.UTC()
	value.ExpiresAt = value.ExpiresAt.UTC()
	if err := validateControlApprovalRequest(value); err != nil {
		return ControlApprovalRequest{}, fmt.Errorf("stored control approval request is invalid: %w", err)
	}
	return value, nil
}

func (r *PostgresControlApprovalRepository) CreateDecision(ctx context.Context, value ControlApprovalDecision) error {
	if r == nil || r.DB == nil {
		return ErrControlApprovalUnavailable
	}
	if err := validateControlApprovalDecision(value); err != nil {
		return err
	}
	result := r.DB.WithContext(ctx).Exec(`
		INSERT INTO public.opscontrol_approval_decisions (
			id, request_id, owner_identity, decision, reason, actor, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (owner_identity, request_id) DO NOTHING`,
		value.ID, value.RequestID, value.OwnerIdentity, value.Decision,
		value.Reason, value.Actor, value.CreatedAt.UTC(),
	)
	if result.Error != nil {
		return fmt.Errorf("%w: create decision", ErrControlApprovalUnavailable)
	}
	if result.RowsAffected != 1 {
		return ErrControlApprovalDecided
	}
	return nil
}

func (r *PostgresControlApprovalRepository) FindDecision(ctx context.Context, owner string, id uuid.UUID) (ControlApprovalDecision, error) {
	if r == nil || r.DB == nil {
		return ControlApprovalDecision{}, ErrControlApprovalUnavailable
	}
	var value ControlApprovalDecision
	var request ControlApprovalRequest
	err := r.DB.WithContext(ctx).Raw(`
		SELECT decision.id, decision.request_id, decision.owner_identity,
		       decision.decision, decision.reason, decision.actor,
		       decision.created_at,
		       request.id, request.owner_identity, request.idempotency_key,
		       request.task_id, request.action, request.resource_type,
		       request.resource_id, request.target, request.binding_digest,
		       request.created_by, request.created_at, request.expires_at
		FROM public.opscontrol_approval_decisions AS decision
		JOIN public.opscontrol_approval_requests AS request
		  ON request.owner_identity = decision.owner_identity
		 AND request.id = decision.request_id
		WHERE decision.owner_identity = ? AND decision.id = ?`, owner, id).Row().Scan(
		&value.ID, &value.RequestID, &value.OwnerIdentity, &value.Decision,
		&value.Reason, &value.Actor, &value.CreatedAt,
		&request.ID, &request.OwnerIdentity, &request.IdempotencyKey,
		&request.TaskID, &request.Action, &request.ResourceType,
		&request.ResourceID, &request.Target, &request.BindingDigest,
		&request.CreatedBy, &request.CreatedAt, &request.ExpiresAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return ControlApprovalDecision{}, ErrControlApprovalNotFound
	}
	if err != nil {
		return ControlApprovalDecision{}, fmt.Errorf(
			"%w: find decision",
			ErrControlApprovalUnavailable,
		)
	}
	value.CreatedAt = value.CreatedAt.UTC()
	request.CreatedAt = request.CreatedAt.UTC()
	request.ExpiresAt = request.ExpiresAt.UTC()
	value.Request = request
	if err := validateControlApprovalDecision(value); err != nil {
		return ControlApprovalDecision{}, fmt.Errorf("stored control approval decision is invalid: %w", err)
	}
	return value, nil
}

var _ ControlApprovalRepository = (*PostgresControlApprovalRepository)(nil)
