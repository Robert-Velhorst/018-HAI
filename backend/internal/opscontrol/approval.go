package opscontrol

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"automation-hub-backend/internal/autonomypolicy"

	"github.com/google/uuid"
)

const (
	ControlDecisionPrefix = "control-decision:"

	controlApprovalResume  = "resume"
	controlApprovalSetMode = "set_mode"
	controlDecisionApprove = "approved"
	controlDecisionReject  = "rejected"
)

var (
	ErrControlApprovalUnavailable = errors.New("opscontrol approval storage is unavailable")
	ErrControlApprovalNotFound    = errors.New("opscontrol approval request was not found")
	ErrControlApprovalExpired     = errors.New("opscontrol approval request expired")
	ErrControlApprovalDecided     = errors.New("opscontrol approval request already has a decision")
	ErrControlApprovalStale       = errors.New("opscontrol approval no longer matches current safety state")
	ErrControlChangeNotRequired   = errors.New("requested safety-control change is not required")
	ErrControlApprovalNotRequired = errors.New("requested safety-control change does not require approval")
)

// ControlApprovalRequest is an immutable, server-derived request for one exact
// safety-control change. Client input never supplies its authority fields.
type ControlApprovalRequest struct {
	ID             uuid.UUID `json:"id"`
	OwnerIdentity  string    `json:"ownerIdentity"`
	IdempotencyKey string    `json:"idempotencyKey"`
	TaskID         string    `json:"taskId"`
	Action         string    `json:"action"`
	ResourceType   string    `json:"resourceType"`
	ResourceID     string    `json:"resourceId"`
	Target         string    `json:"target"`
	BindingDigest  string    `json:"bindingDigest"`
	CreatedBy      string    `json:"createdBy"`
	CreatedAt      time.Time `json:"createdAt"`
	ExpiresAt      time.Time `json:"expiresAt"`
}

// ControlApprovalDecision is append-only owner authority for one request.
type ControlApprovalDecision struct {
	ID            uuid.UUID              `json:"id"`
	RequestID     uuid.UUID              `json:"requestId"`
	OwnerIdentity string                 `json:"ownerIdentity"`
	Decision      string                 `json:"decision"`
	Reason        string                 `json:"reason,omitempty"`
	Actor         string                 `json:"actor"`
	CreatedAt     time.Time              `json:"createdAt"`
	Request       ControlApprovalRequest `json:"request"`
}

// ControlApprovalRepository is the durable source of truth used both by the
// approval API and the execution-authorization resolver.
type ControlApprovalRepository interface {
	CreateRequest(context.Context, ControlApprovalRequest) error
	FindRequest(context.Context, string, uuid.UUID) (ControlApprovalRequest, error)
	CreateDecision(context.Context, ControlApprovalDecision) error
	FindDecision(context.Context, string, uuid.UUID) (ControlApprovalDecision, error)
}

type PreparedControlApproval struct {
	RequestID     uuid.UUID `json:"requestId"`
	Action        string    `json:"action"`
	ResourceType  string    `json:"resourceType"`
	ResourceID    string    `json:"resourceId"`
	Target        string    `json:"target"`
	BindingDigest string    `json:"bindingDigest"`
	ExpiresAt     time.Time `json:"expiresAt"`
}

type DecidedControlApproval struct {
	RequestID             uuid.UUID `json:"requestId"`
	DecisionID            uuid.UUID `json:"decisionId"`
	Decision              string    `json:"decision"`
	IdempotencyKey        string    `json:"idempotencyKey,omitempty"`
	TaskID                string    `json:"taskId,omitempty"`
	ApprovalSourceID      string    `json:"approvalSourceId,omitempty"`
	ApprovalBindingDigest string    `json:"approvalBindingDigest,omitempty"`
	ExpiresAt             time.Time `json:"expiresAt"`
}

func (s *Service) WithControlApprovalRepository(repository ControlApprovalRepository) *Service {
	s.approvals = repository
	return s
}

// PrepareControlApproval derives one exact request from current persisted
// control state. The caller selects only the operation and optional target.
func (s *Service) PrepareControlApproval(
	ctx context.Context,
	actor string,
	operation string,
	target string,
) (PreparedControlApproval, error) {
	if s == nil || s.approvals == nil {
		return PreparedControlApproval{}, ErrControlApprovalUnavailable
	}
	actor = strings.TrimSpace(actor)
	if err := validateControlActor(actor); err != nil {
		return PreparedControlApproval{}, err
	}
	effect, err := s.currentControlEffect(actor, operation, target)
	if err != nil {
		return PreparedControlApproval{}, err
	}
	bindingDigest, err := controlEffectDigest(effect)
	if err != nil {
		return PreparedControlApproval{}, fmt.Errorf("derive control approval digest: %w", err)
	}
	now := s.now().UTC()
	id := uuid.New()
	request := ControlApprovalRequest{
		ID:             id,
		OwnerIdentity:  actor,
		IdempotencyKey: "opscontrol:" + id.String(),
		TaskID:         "opscontrol:" + id.String(),
		Action:         effect.Action,
		ResourceType:   effect.ResourceType,
		ResourceID:     effect.ResourceID,
		Target:         effect.Target,
		BindingDigest:  bindingDigest,
		CreatedBy:      actor,
		CreatedAt:      now,
		ExpiresAt:      now.Add(controlAuthorizationTTL),
	}
	if err := validateControlApprovalRequest(request); err != nil {
		return PreparedControlApproval{}, err
	}
	if err := s.approvals.CreateRequest(ctx, request); err != nil {
		return PreparedControlApproval{}, fmt.Errorf("persist control approval request: %w", err)
	}
	return PreparedControlApproval{
		RequestID:     request.ID,
		Action:        request.Action,
		ResourceType:  request.ResourceType,
		ResourceID:    request.ResourceID,
		Target:        request.Target,
		BindingDigest: request.BindingDigest,
		ExpiresAt:     request.ExpiresAt,
	}, nil
}

// DecideControlApproval records one irreversible owner decision. Approval
// returns only references; the execution endpoint still performs authorization
// and consumes the resulting receipt immediately before changing state.
func (s *Service) DecideControlApproval(
	ctx context.Context,
	actor string,
	requestID uuid.UUID,
	decision string,
	reason string,
) (DecidedControlApproval, error) {
	if s == nil || s.approvals == nil {
		return DecidedControlApproval{}, ErrControlApprovalUnavailable
	}
	actor = strings.TrimSpace(actor)
	if err := validateControlActor(actor); err != nil {
		return DecidedControlApproval{}, err
	}
	request, err := s.approvals.FindRequest(ctx, actor, requestID)
	if err != nil {
		return DecidedControlApproval{}, err
	}
	now := s.now().UTC()
	if !now.Before(request.ExpiresAt) {
		return DecidedControlApproval{}, ErrControlApprovalExpired
	}
	decision = strings.ToLower(strings.TrimSpace(decision))
	reason = strings.TrimSpace(reason)
	if decision != controlDecisionApprove && decision != controlDecisionReject {
		return DecidedControlApproval{}, fmt.Errorf("decision must be approved or rejected")
	}
	if decision == controlDecisionApprove {
		current, effectErr := s.currentControlEffect(
			request.OwnerIdentity,
			request.Action,
			request.Target,
		)
		if effectErr != nil {
			return DecidedControlApproval{}, fmt.Errorf("%w: %v", ErrControlApprovalStale, effectErr)
		}
		digest, digestErr := controlEffectDigest(current)
		if digestErr != nil || digest != request.BindingDigest ||
			current.ResourceID != request.ResourceID ||
			current.ResourceType != request.ResourceType {
			return DecidedControlApproval{}, ErrControlApprovalStale
		}
	}
	value := ControlApprovalDecision{
		ID:            uuid.New(),
		RequestID:     request.ID,
		OwnerIdentity: actor,
		Decision:      decision,
		Reason:        reason,
		Actor:         actor,
		CreatedAt:     now,
		Request:       request,
	}
	if err := validateControlApprovalDecision(value); err != nil {
		return DecidedControlApproval{}, err
	}
	if err := s.approvals.CreateDecision(ctx, value); err != nil {
		return DecidedControlApproval{}, fmt.Errorf("persist control approval decision: %w", err)
	}
	result := DecidedControlApproval{
		RequestID:  request.ID,
		DecisionID: value.ID,
		Decision:   value.Decision,
		ExpiresAt:  request.ExpiresAt,
	}
	if decision == controlDecisionApprove {
		result.IdempotencyKey = request.IdempotencyKey
		result.TaskID = request.TaskID
		result.ApprovalSourceID = ControlDecisionPrefix + value.ID.String()
		result.ApprovalBindingDigest = request.BindingDigest
	}
	return result, nil
}

func (s *Service) currentControlEffect(
	ownerIdentity string,
	operation string,
	target string,
) (controlEffect, error) {
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	if err := validateControlActor(ownerIdentity); err != nil {
		return controlEffect{}, err
	}
	operation = strings.ToLower(strings.TrimSpace(operation))
	switch operation {
	case controlApprovalResume, clearEmergencyStopAction:
		state, err := s.control.emergency.Status()
		if err != nil {
			return controlEffect{}, fmt.Errorf("%w: %v", ErrControlPersistence, err)
		}
		if !state.Engaged {
			return controlEffect{}, ErrControlChangeNotRequired
		}
		return controlEffect{
			Version:       1,
			OwnerIdentity: ownerIdentity,
			Action:        clearEmergencyStopAction,
			ResourceType:  emergencyStopResourceType,
			ResourceID:    emergencyStopResourceID(state.Revision),
			Target:        "disengaged",
		}, nil
	case controlApprovalSetMode, escalateAutonomyAction:
		targetMode, err := autonomypolicy.ParseMode(strings.TrimSpace(target))
		if err != nil {
			return controlEffect{}, err
		}
		current, modeErr := s.control.ModePersistenceStatus()
		if modeErr != nil {
			return controlEffect{}, fmt.Errorf("%w: %v", ErrControlPersistence, modeErr)
		}
		if current == targetMode {
			return controlEffect{}, ErrControlChangeNotRequired
		}
		if modeAuthorityRank(targetMode) <= modeAuthorityRank(current) {
			return controlEffect{}, ErrControlApprovalNotRequired
		}
		return controlEffect{
			Version:       1,
			OwnerIdentity: ownerIdentity,
			Action:        escalateAutonomyAction,
			ResourceType:  autonomyModeResourceType,
			ResourceID:    autonomyModeResourceID(string(current), string(targetMode)),
			Target:        string(targetMode),
		}, nil
	default:
		return controlEffect{}, fmt.Errorf("unsupported control approval action")
	}
}

func validateControlActor(actor string) error {
	if actor == "" || actor != strings.TrimSpace(actor) ||
		!utf8.ValidString(actor) || len(actor) > 255 {
		return ErrUnauthenticated
	}
	return nil
}

func validateControlApprovalRequest(value ControlApprovalRequest) error {
	if value.ID == uuid.Nil || value.OwnerIdentity == "" ||
		value.CreatedBy != value.OwnerIdentity || value.IdempotencyKey == "" ||
		value.TaskID == "" || value.ResourceID == "" || value.Target == "" ||
		len(value.BindingDigest) != 64 || value.CreatedAt.IsZero() ||
		value.ExpiresAt.IsZero() || !value.ExpiresAt.After(value.CreatedAt) ||
		value.ExpiresAt.After(value.CreatedAt.Add(controlAuthorizationTTL)) {
		return fmt.Errorf("control approval request is invalid")
	}
	return nil
}

func validateControlApprovalDecision(value ControlApprovalDecision) error {
	if value.ID == uuid.Nil || value.RequestID == uuid.Nil ||
		value.Request.ID != value.RequestID ||
		value.OwnerIdentity == "" || value.Actor != value.OwnerIdentity ||
		value.Request.OwnerIdentity != value.OwnerIdentity ||
		(value.Decision != controlDecisionApprove && value.Decision != controlDecisionReject) ||
		!utf8.ValidString(value.Reason) || utf8.RuneCountInString(value.Reason) > 2048 ||
		value.CreatedAt.IsZero() || value.CreatedAt.Before(value.Request.CreatedAt) ||
		value.CreatedAt.After(value.Request.ExpiresAt) {
		return fmt.Errorf("control approval decision is invalid")
	}
	return nil
}
