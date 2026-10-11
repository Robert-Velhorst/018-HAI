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

var (
	ErrInvalidControlDecisionReference = errors.New("invalid opscontrol approval reference")
	ErrControlDecisionUnavailable      = errors.New("durable opscontrol approval is unavailable")
	ErrControlDecisionBindingMismatch  = errors.New("opscontrol approval binding digest does not match")
)

type ControlApprovalResolver struct {
	repository ControlApprovalRepository
	now        func() time.Time
}

func NewControlApprovalResolver(repository ControlApprovalRepository) (*ControlApprovalResolver, error) {
	if repository == nil {
		return nil, ErrControlApprovalUnavailable
	}
	return &ControlApprovalResolver{
		repository: repository,
		now:        func() time.Time { return time.Now().UTC() },
	}, nil
}

func (r *ControlApprovalResolver) Resolve(
	ctx context.Context,
	ownerIdentity string,
	sourceID string,
	bindingDigest string,
) (executionauth.ResolvedApproval, error) {
	if r == nil || r.repository == nil || r.now == nil {
		return executionauth.ResolvedApproval{}, ErrControlApprovalUnavailable
	}
	if ctx == nil {
		return executionauth.ResolvedApproval{}, fmt.Errorf("control approval context is required")
	}
	if err := ctx.Err(); err != nil {
		return executionauth.ResolvedApproval{}, err
	}
	decisionID, err := parseControlDecisionSource(sourceID)
	if err != nil {
		return executionauth.ResolvedApproval{}, err
	}
	if len(bindingDigest) != 64 || bindingDigest != strings.ToLower(bindingDigest) {
		return executionauth.ResolvedApproval{}, ErrControlDecisionBindingMismatch
	}
	decision, err := r.repository.FindDecision(ctx, ownerIdentity, decisionID)
	if err != nil {
		return executionauth.ResolvedApproval{}, fmt.Errorf("%w: %v", ErrControlDecisionUnavailable, err)
	}
	if decision.ID != decisionID || decision.OwnerIdentity != ownerIdentity ||
		decision.Actor != ownerIdentity || decision.Request.OwnerIdentity != ownerIdentity ||
		decision.Request.ID != decision.RequestID ||
		decision.Decision != controlDecisionApprove {
		return executionauth.ResolvedApproval{}, ErrControlDecisionUnavailable
	}
	if decision.Request.BindingDigest != bindingDigest {
		return executionauth.ResolvedApproval{}, ErrControlDecisionBindingMismatch
	}
	now := r.now().UTC()
	if decision.CreatedAt.After(now.Add(time.Second)) ||
		!now.Before(decision.Request.ExpiresAt) ||
		now.Sub(decision.CreatedAt) > controlAuthorizationTTL {
		return executionauth.ResolvedApproval{}, ErrControlApprovalExpired
	}
	decisionDigest, err := controlApprovalDecisionDigest(decision)
	if err != nil {
		return executionauth.ResolvedApproval{}, err
	}
	return executionauth.ResolvedApproval{
		SourceID:       sourceID,
		DecisionID:     decision.ID.String(),
		DecisionDigest: decisionDigest,
		BindingDigest:  bindingDigest,
		ApprovedBy:     decision.Actor,
		ApproverRoles:  []string{"owner"},
		ApprovedAt:     decision.CreatedAt.UTC(),
		ExpiresAt:      decision.Request.ExpiresAt.UTC(),
	}, nil
}

func parseControlDecisionSource(sourceID string) (uuid.UUID, error) {
	if !strings.HasPrefix(sourceID, ControlDecisionPrefix) {
		return uuid.Nil, ErrInvalidControlDecisionReference
	}
	raw := strings.TrimPrefix(sourceID, ControlDecisionPrefix)
	id, err := uuid.Parse(raw)
	if err != nil || id == uuid.Nil || sourceID != ControlDecisionPrefix+id.String() {
		return uuid.Nil, ErrInvalidControlDecisionReference
	}
	return id, nil
}

type immutableControlApprovalDecisionV1 struct {
	ContractVersion int    `json:"contractVersion"`
	DecisionID      string `json:"decisionId"`
	RequestID       string `json:"requestId"`
	OwnerIdentity   string `json:"ownerIdentity"`
	Decision        string `json:"decision"`
	Reason          string `json:"reason"`
	Actor           string `json:"actor"`
	CreatedAt       string `json:"createdAt"`
	Action          string `json:"action"`
	ResourceType    string `json:"resourceType"`
	ResourceID      string `json:"resourceId"`
	Target          string `json:"target"`
	BindingDigest   string `json:"bindingDigest"`
	ExpiresAt       string `json:"expiresAt"`
}

func controlApprovalDecisionDigest(value ControlApprovalDecision) (string, error) {
	encoded, err := json.Marshal(immutableControlApprovalDecisionV1{
		ContractVersion: 1,
		DecisionID:      value.ID.String(),
		RequestID:       value.RequestID.String(),
		OwnerIdentity:   value.OwnerIdentity,
		Decision:        value.Decision,
		Reason:          value.Reason,
		Actor:           value.Actor,
		CreatedAt:       value.CreatedAt.UTC().Format(time.RFC3339Nano),
		Action:          value.Request.Action,
		ResourceType:    value.Request.ResourceType,
		ResourceID:      value.Request.ResourceID,
		Target:          value.Request.Target,
		BindingDigest:   value.Request.BindingDigest,
		ExpiresAt:       value.Request.ExpiresAt.UTC().Format(time.RFC3339Nano),
	})
	if err != nil {
		return "", fmt.Errorf("digest control approval decision: %w", err)
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

var _ executionauth.ApprovalResolver = (*ControlApprovalResolver)(nil)
