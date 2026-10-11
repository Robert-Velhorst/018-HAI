package opscontrol

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"automation-hub-backend/internal/executionauth"

	"github.com/google/uuid"
)

const (
	clearEmergencyStopAction = "opscontrol.emergency-stop.clear"
	escalateAutonomyAction   = "opscontrol.autonomy-mode.escalate"

	emergencyStopResourceType = "opscontrol-emergency-stop"
	autonomyModeResourceType  = "opscontrol-autonomy-mode"
	controlAuthorizationTTL   = 5 * time.Minute
)

var (
	ErrUnauthenticated          = errors.New("authenticated actor is required")
	ErrAuthorizationUnavailable = errors.New("opscontrol authorization is unavailable")
	ErrAuthorizationDenied      = errors.New("opscontrol authorization was denied")
	ErrAuthorizationMismatch    = errors.New("opscontrol authorization does not match the requested control change")
)

// ExecutionAuthorizer is the consumed authorization boundary used immediately
// before a safety control is weakened. Production composition injects the
// shared executionauth service; tests can inject a strict spy.
type ExecutionAuthorizer interface {
	AuthorizeAndConsume(
		context.Context,
		executionauth.Request,
		string,
		string,
	) (executionauth.Receipt, error)
}

// ControlAuthorization contains only caller-selected references. Owner, action,
// resource, risk, authority, and effect identity are derived by the service.
type ControlAuthorization struct {
	ActorIdentity         string
	IdempotencyKey        string
	TaskID                string
	ApprovalSourceID      string
	ApprovalBindingDigest string
}

type controlEffect struct {
	Version       int    `json:"version"`
	OwnerIdentity string `json:"ownerIdentity"`
	Action        string `json:"action"`
	ResourceType  string `json:"resourceType"`
	ResourceID    string `json:"resourceId"`
	Target        string `json:"target"`
}

func (s *Service) authorizeSafetyChange(
	ctx context.Context,
	auth ControlAuthorization,
	action string,
	resourceType string,
	resourceID string,
	target string,
) error {
	auth.ActorIdentity = strings.TrimSpace(auth.ActorIdentity)
	auth.IdempotencyKey = strings.TrimSpace(auth.IdempotencyKey)
	auth.TaskID = strings.TrimSpace(auth.TaskID)
	auth.ApprovalSourceID = strings.TrimSpace(auth.ApprovalSourceID)
	auth.ApprovalBindingDigest = strings.ToLower(strings.TrimSpace(auth.ApprovalBindingDigest))

	if auth.ActorIdentity == "" {
		return ErrUnauthenticated
	}
	if s.authorization == nil {
		return ErrAuthorizationUnavailable
	}
	if auth.IdempotencyKey == "" || auth.TaskID == "" ||
		auth.ApprovalSourceID == "" || auth.ApprovalBindingDigest == "" {
		return fmt.Errorf("%w: fresh approval references are required", ErrAuthorizationDenied)
	}

	effectDigest, err := controlEffectDigest(controlEffect{
		Version:       1,
		OwnerIdentity: auth.ActorIdentity,
		Action:        action,
		ResourceType:  resourceType,
		ResourceID:    resourceID,
		Target:        target,
	})
	if err != nil {
		return fmt.Errorf("derive safety-control effect: %w", err)
	}
	if auth.ApprovalBindingDigest != effectDigest {
		return ErrAuthorizationMismatch
	}

	request := executionauth.Request{
		OwnerIdentity:         auth.ActorIdentity,
		IdempotencyKey:        auth.IdempotencyKey,
		ActorIdentity:         auth.ActorIdentity,
		ActorKind:             executionauth.ActorHuman,
		TaskID:                auth.TaskID,
		Action:                action,
		Stage:                 executionauth.StagePrivilegeEscalation,
		ResourceType:          resourceType,
		ResourceID:            resourceID,
		Domain:                "safety-control",
		RequiredAuthority:     10,
		RequestedAutonomy:     6,
		Risk:                  executionauth.RiskCritical,
		Reversible:            false,
		ApprovalSourceID:      auth.ApprovalSourceID,
		ApprovalBindingDigest: auth.ApprovalBindingDigest,
		EffectDigest:          effectDigest,
		Facts: map[string]string{
			"target": target,
		},
		SourceReferences: []string{"opscontrol"},
		RequestedAt:      s.now().UTC(),
	}
	receipt, err := s.authorization.AuthorizeAndConsume(
		ctx,
		request,
		"opscontrol",
		action+":"+resourceID,
	)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrAuthorizationDenied, err)
	}
	return s.validateSafetyReceipt(receipt, request)
}

func (s *Service) validateSafetyReceipt(
	receipt executionauth.Receipt,
	request executionauth.Request,
) error {
	now := s.now().UTC()
	approval := receipt.Evidence.Approval
	if receipt.Outcome != executionauth.OutcomeAuthorized ||
		receipt.ID == uuid.Nil ||
		receipt.ContractVersion != executionauth.ContractVersion ||
		receipt.OwnerIdentity != request.OwnerIdentity ||
		receipt.IdempotencyKey != request.IdempotencyKey ||
		receipt.ActorIdentity != request.ActorIdentity ||
		receipt.ActorKind != request.ActorKind ||
		receipt.TaskID != request.TaskID ||
		receipt.Action != request.Action ||
		receipt.Stage != request.Stage ||
		receipt.ResourceType != request.ResourceType ||
		receipt.ResourceID != request.ResourceID ||
		receipt.ApprovalSourceID != request.ApprovalSourceID ||
		receipt.EffectDigest != request.EffectDigest ||
		receipt.RequestDigest == "" ||
		receipt.DecisionDigest == "" ||
		receipt.RequiredAuthority != request.RequiredAuthority ||
		receipt.RequestedAutonomy != request.RequestedAutonomy ||
		receipt.Risk != request.Risk ||
		receipt.Reversible != request.Reversible ||
		approval.SourceID != request.ApprovalSourceID ||
		approval.DecisionID == "" ||
		approval.DecisionDigest == "" ||
		approval.ApprovedBy == "" {
		return ErrAuthorizationMismatch
	}
	if approval.ApprovedAt.IsZero() ||
		approval.ExpiresAt.IsZero() ||
		approval.ApprovedAt.After(now.Add(time.Minute)) ||
		now.After(approval.ExpiresAt) ||
		now.Sub(approval.ApprovedAt) > controlAuthorizationTTL {
		return fmt.Errorf("%w: approval is not fresh", ErrAuthorizationDenied)
	}
	if receipt.EvaluatedAt.IsZero() ||
		receipt.EvaluatedAt.After(now.Add(time.Minute)) ||
		now.Sub(receipt.EvaluatedAt) > controlAuthorizationTTL {
		return fmt.Errorf("%w: authorization receipt is not fresh", ErrAuthorizationDenied)
	}
	return nil
}

func controlEffectDigest(effect controlEffect) (string, error) {
	encoded, err := json.Marshal(effect)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

func emergencyStopResourceID(revision uint64) string {
	return fmt.Sprintf("emergency-stop:revision-%d", revision)
}

func autonomyModeResourceID(from, to string) string {
	return fmt.Sprintf("autonomy-mode:%s-to-%s", from, to)
}
