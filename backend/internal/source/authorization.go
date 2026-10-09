package source

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
	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/safety"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	revokeSourceAction           = "source.revoke"
	deleteExtractionAction       = "source.extraction.delete"
	connectedSourceResourceType  = "connected-source"
	sourceExtractionResourceType = "source-extraction"
	sourceAuthorizationConsumer  = "connected-source.final-effect"
	sourceEffectContractVersion  = 1
)

var (
	ErrDestructiveAuthorizationRequired  = errors.New("connected-source final-effect authorization is required")
	ErrDestructiveAuthorizationDenied    = errors.New("connected-source final-effect authorization was denied")
	ErrDestructiveAuthorizationMismatch  = errors.New("connected-source authorization receipt does not match the final effect")
	ErrDestructiveTransactionUnavailable = errors.New("connected-source deletion requires transaction-aware authorization and workflow services")
	ErrDestructiveOwnerMismatch          = errors.New("connected-source resource is not owned by the authenticated actor")
	ErrSourceEmergencyStopActive         = errors.New("emergency stop blocks connected-source mutation")
	ErrSourceRevoked                     = errors.New("revoked connected sources cannot be reactivated")
)

// FinalEffectAuthorizer atomically authorizes and consumes authority for one
// exact source mutation. It is deliberately compatible with executionauth.Service.
type FinalEffectAuthorizer interface {
	AuthorizeAndConsume(
		context.Context,
		executionauth.Request,
		string,
		string,
	) (executionauth.Receipt, error)
}

type transactionalFinalEffectAuthorizer interface {
	AuthorizeAndConsumeInTransaction(
		context.Context,
		*gorm.DB,
		executionauth.Request,
		string,
		string,
	) (executionauth.Receipt, executionauth.AuthorizationPostCommitProjection, error)
}

// DestructiveEffectService is intentionally separate from Service so read and
// sync consumers cannot accidentally acquire a destructive execution method.
type DestructiveEffectService interface {
	RevokeAuthorized(
		context.Context,
		uuid.UUID,
		DestructiveEffectAuthorization,
	) (*models.ConnectedSource, error)
	DeleteExtractionAuthorized(
		context.Context,
		uuid.UUID,
		DestructiveEffectAuthorization,
	) error
	WithDestructiveEffectAuthorization(
		FinalEffectAuthorizer,
		func() safety.EmergencyStopDecision,
	) Service
}

// DestructiveEffectAuthorization contains only server-derived identity and
// caller-selected durable approval references. Resource identity, provenance,
// risk, action, and effect digest are always derived from persisted records.
type DestructiveEffectAuthorization struct {
	OwnerIdentity         string
	ActorIdentity         string
	IdempotencyKey        string
	TaskID                string
	ApprovalSourceID      string
	ApprovalBindingDigest string
}

type sourceEffect struct {
	ContractVersion       int    `json:"contractVersion"`
	OwnerIdentity         string `json:"ownerIdentity"`
	Action                string `json:"action"`
	ResourceType          string `json:"resourceType"`
	ResourceID            string `json:"resourceId"`
	SourceID              string `json:"sourceId"`
	ConnectorKey          string `json:"connectorKey"`
	ProjectKey            string `json:"projectKey,omitempty"`
	RawItemID             string `json:"rawItemId,omitempty"`
	ContentHash           string `json:"contentHash,omitempty"`
	SourceRevision        string `json:"sourceRevision"`
	ResourceRevision      string `json:"resourceRevision"`
	SourceProvenanceHash  string `json:"sourceProvenanceHash"`
	ApprovalSourceID      string `json:"approvalSourceId"`
	ApprovalBindingDigest string `json:"approvalBindingDigest"`
}

func (s *service) WithDestructiveEffectAuthorization(
	authorizer FinalEffectAuthorizer,
	stop func() safety.EmergencyStopDecision,
) Service {
	if s == nil {
		return s
	}
	s.finalEffectAuthorizer = authorizer
	if stop != nil {
		s.emergencyStop = stop
	}
	return s
}

func (s *service) authorizeDestructiveEffect(
	ctx context.Context,
	auth DestructiveEffectAuthorization,
	source models.ConnectedSource,
	extraction *models.SourceExtraction,
) error {
	if s == nil || s.finalEffectAuthorizer == nil {
		return ErrDestructiveAuthorizationRequired
	}
	request, target, err := buildDestructiveAuthorizationRequest(
		auth,
		source,
		extraction,
	)
	if err != nil {
		return err
	}
	if decision := s.evaluateSourceEmergencyStop(); decision.Active {
		return fmt.Errorf(
			"%w: %s",
			ErrSourceEmergencyStopActive,
			safety.RedactSecrets(strings.TrimSpace(decision.Reason)),
		)
	}
	receipt, err := s.finalEffectAuthorizer.AuthorizeAndConsume(
		ctx,
		request,
		sourceAuthorizationConsumer,
		target,
	)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrDestructiveAuthorizationDenied, err)
	}
	if err := validateSourceAuthorizationReceipt(receipt, request); err != nil {
		return err
	}
	// Authorization consumption is not permission to ignore a stop engaged
	// concurrently. This is the final check before the caller mutates storage.
	if decision := s.evaluateSourceEmergencyStop(); decision.Active {
		return fmt.Errorf(
			"%w: %s",
			ErrSourceEmergencyStopActive,
			safety.RedactSecrets(strings.TrimSpace(decision.Reason)),
		)
	}
	return nil
}

func (s *service) authorizeDestructiveEffectInTransaction(
	ctx context.Context,
	tx *gorm.DB,
	auth DestructiveEffectAuthorization,
	source models.ConnectedSource,
	extraction *models.SourceExtraction,
) (executionauth.AuthorizationPostCommitProjection, error) {
	if s == nil || s.finalEffectAuthorizer == nil {
		return nil, ErrDestructiveAuthorizationRequired
	}
	authorizer, ok := s.finalEffectAuthorizer.(transactionalFinalEffectAuthorizer)
	if !ok {
		return nil, ErrDestructiveTransactionUnavailable
	}
	request, target, err := buildDestructiveAuthorizationRequest(auth, source, extraction)
	if err != nil {
		return nil, err
	}
	if decision := s.evaluateSourceEmergencyStop(); decision.Active {
		return nil, fmt.Errorf(
			"%w: %s",
			ErrSourceEmergencyStopActive,
			safety.RedactSecrets(strings.TrimSpace(decision.Reason)),
		)
	}
	receipt, projection, err := authorizer.AuthorizeAndConsumeInTransaction(
		ctx,
		tx,
		request,
		sourceAuthorizationConsumer,
		target,
	)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDestructiveAuthorizationDenied, err)
	}
	if err := validateSourceAuthorizationReceipt(receipt, request); err != nil {
		return nil, err
	}
	if decision := s.evaluateSourceEmergencyStop(); decision.Active {
		return nil, fmt.Errorf(
			"%w: %s",
			ErrSourceEmergencyStopActive,
			safety.RedactSecrets(strings.TrimSpace(decision.Reason)),
		)
	}
	return projection, nil
}

func buildDestructiveAuthorizationRequest(
	auth DestructiveEffectAuthorization,
	source models.ConnectedSource,
	extraction *models.SourceExtraction,
) (executionauth.Request, string, error) {
	auth.OwnerIdentity = strings.TrimSpace(auth.OwnerIdentity)
	auth.ActorIdentity = strings.TrimSpace(auth.ActorIdentity)
	auth.IdempotencyKey = strings.TrimSpace(auth.IdempotencyKey)
	auth.TaskID = strings.TrimSpace(auth.TaskID)
	auth.ApprovalSourceID = strings.TrimSpace(auth.ApprovalSourceID)
	auth.ApprovalBindingDigest = strings.ToLower(
		strings.TrimSpace(auth.ApprovalBindingDigest),
	)
	if auth.OwnerIdentity == "" ||
		auth.ActorIdentity == "" ||
		auth.OwnerIdentity != auth.ActorIdentity ||
		strings.TrimSpace(source.OwnerIdentity) != auth.OwnerIdentity {
		return executionauth.Request{}, "", ErrDestructiveOwnerMismatch
	}
	if auth.IdempotencyKey == "" ||
		auth.TaskID == "" ||
		auth.ApprovalSourceID == "" ||
		auth.ApprovalBindingDigest == "" {
		return executionauth.Request{}, "", fmt.Errorf(
			"%w: task, idempotency, approval source, and approval binding are required",
			ErrDestructiveAuthorizationDenied,
		)
	}

	action := revokeSourceAction
	resourceType := connectedSourceResourceType
	resourceID := source.ID.String()
	projectKey := strings.TrimSpace(source.DefaultProjectKey)
	rawItemID := ""
	contentHash := ""
	sourceURI := ""
	resourceRevision := source.UpdatedAt.UTC().Format(time.RFC3339Nano)
	if extraction != nil {
		if extraction.ID == uuid.Nil ||
			extraction.SourceID != source.ID {
			return executionauth.Request{}, "", ErrDestructiveOwnerMismatch
		}
		action = deleteExtractionAction
		resourceType = sourceExtractionResourceType
		resourceID = extraction.ID.String()
		projectKey = strings.TrimSpace(extraction.ProjectKey)
		rawItemID = extraction.RawItemID.String()
		contentHash = strings.TrimSpace(extraction.ContentHash)
		sourceURI = strings.TrimSpace(extraction.SourceURI)
		resourceRevision = extraction.UpdatedAt.UTC().Format(time.RFC3339Nano)
	}
	sourceRevision := source.UpdatedAt.UTC().Format(time.RFC3339Nano)
	provenanceHash := sourceProvenanceDigest(
		source.ID.String(),
		strings.TrimSpace(source.ConnectorKey),
		projectKey,
		rawItemID,
		contentHash,
		sourceURI,
		sourceRevision,
		resourceRevision,
	)
	effect := sourceEffect{
		ContractVersion:       sourceEffectContractVersion,
		OwnerIdentity:         auth.OwnerIdentity,
		Action:                action,
		ResourceType:          resourceType,
		ResourceID:            resourceID,
		SourceID:              source.ID.String(),
		ConnectorKey:          strings.TrimSpace(source.ConnectorKey),
		ProjectKey:            projectKey,
		RawItemID:             rawItemID,
		ContentHash:           contentHash,
		SourceRevision:        sourceRevision,
		ResourceRevision:      resourceRevision,
		SourceProvenanceHash:  provenanceHash,
		ApprovalSourceID:      auth.ApprovalSourceID,
		ApprovalBindingDigest: auth.ApprovalBindingDigest,
	}
	effectDigest, err := sourceEffectDigest(effect)
	if err != nil {
		return executionauth.Request{}, "", fmt.Errorf(
			"derive connected-source effect: %w",
			err,
		)
	}
	request := executionauth.Request{
		OwnerIdentity:         auth.OwnerIdentity,
		IdempotencyKey:        auth.IdempotencyKey,
		ActorIdentity:         auth.ActorIdentity,
		ActorKind:             executionauth.ActorHuman,
		TaskID:                auth.TaskID,
		Action:                action,
		Stage:                 executionauth.StageDeletion,
		ResourceType:          resourceType,
		ResourceID:            resourceID,
		ProjectKey:            projectKey,
		Domain:                "connected-source",
		DataScopes:            []string{strings.TrimSpace(source.Category)},
		RequiredAuthority:     8,
		RequestedAutonomy:     6,
		Risk:                  executionauth.RiskHigh,
		Reversible:            false,
		ApprovalSourceID:      auth.ApprovalSourceID,
		ApprovalBindingDigest: auth.ApprovalBindingDigest,
		EffectDigest:          effectDigest,
		Facts: map[string]string{
			"sourceId":             source.ID.String(),
			"connectorKey":         strings.TrimSpace(source.ConnectorKey),
			"sourceProvenanceHash": provenanceHash,
		},
		SourceReferences: []string{
			"connected-source:" + source.ID.String(),
			resourceType + ":" + resourceID,
		},
	}
	return request, "source-effect:" + effectDigest, nil
}

func validateSourceAuthorizationReceipt(
	receipt executionauth.Receipt,
	request executionauth.Request,
) error {
	approval := receipt.Evidence.Approval
	now := time.Now().UTC()
	if receipt.ContractVersion != executionauth.ContractVersion ||
		receipt.Outcome != executionauth.OutcomeAuthorized ||
		receipt.OwnerIdentity != request.OwnerIdentity ||
		receipt.IdempotencyKey != request.IdempotencyKey ||
		receipt.ActorIdentity != request.ActorIdentity ||
		receipt.ActorKind != request.ActorKind ||
		receipt.TaskID != request.TaskID ||
		receipt.Action != request.Action ||
		receipt.Stage != request.Stage ||
		receipt.ResourceType != request.ResourceType ||
		receipt.ResourceID != request.ResourceID ||
		receipt.ProjectKey != request.ProjectKey ||
		receipt.ApprovalSourceID != request.ApprovalSourceID ||
		receipt.EffectDigest != request.EffectDigest ||
		receipt.RequiredAuthority != request.RequiredAuthority ||
		receipt.RequestedAutonomy != request.RequestedAutonomy ||
		receipt.Risk != request.Risk ||
		receipt.Reversible != request.Reversible ||
		!validSourceDigest(receipt.RequestDigest) ||
		!validSourceDigest(receipt.DecisionDigest) ||
		approval.SourceID != request.ApprovalSourceID ||
		approval.DecisionID == "" ||
		!validSourceDigest(approval.DecisionDigest) ||
		approval.ApprovedBy == "" ||
		approval.ApprovedAt.IsZero() ||
		approval.ExpiresAt.IsZero() ||
		approval.ApprovedAt.After(now.Add(time.Minute)) ||
		!now.Before(approval.ExpiresAt) {
		return ErrDestructiveAuthorizationMismatch
	}
	return nil
}

func (s *service) evaluateSourceEmergencyStop() safety.EmergencyStopDecision {
	if s != nil && s.emergencyStop != nil {
		return s.emergencyStop()
	}
	return safety.EvaluateEmergencyStop()
}

func sourceEffectDigest(effect sourceEffect) (string, error) {
	encoded, err := json.Marshal(effect)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

func sourceProvenanceDigest(values ...string) string {
	encoded, _ := json.Marshal(values)
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

func validSourceDigest(value string) bool {
	if len(value) != sha256.Size*2 || value != strings.ToLower(value) {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
