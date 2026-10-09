package operations

import (
	"fmt"
	"strings"
	"time"

	"automation-hub-backend/internal/models"
	"github.com/google/uuid"
)

// NewOperationInput is the validated input to create an Operation.
type NewOperationInput struct {
	OwnerUserID        string
	WorkspaceID        string
	Title              string
	Description        string
	OperationType      string
	SourceType         string
	SourceID           *uuid.UUID
	SourceURI          string
	SourceReceivedAt   *time.Time
	SourceRevisionHash string
	SourceProvider     string
	SourceAccount      string
	SourceExternalID   string
	ProjectKey         string
	AccountFeedID      *uuid.UUID
	DedupeKey          string
	// LegacyDedupeKey is a rollout fence, not permission to overwrite old work.
	LegacyDedupeKey string
	EvidenceJSON    string
}

// NewOperation builds a valid Operation in the initial `new` status. Risk,
// autonomy, and decision are provisional until the policy engine classifies it.
func NewOperation(in NewOperationInput, now time.Time) (models.Operation, error) {
	if strings.TrimSpace(in.OwnerUserID) == "" {
		return models.Operation{}, fmt.Errorf("operation: ownerUserId required")
	}
	if strings.TrimSpace(in.Title) == "" {
		return models.Operation{}, fmt.Errorf("operation: title required")
	}
	if strings.TrimSpace(in.OperationType) == "" {
		return models.Operation{}, fmt.Errorf("operation: operationType required")
	}
	if strings.TrimSpace(in.SourceType) == "" {
		return models.Operation{}, fmt.Errorf("operation: sourceType required")
	}
	if strings.TrimSpace(in.DedupeKey) == "" {
		return models.Operation{}, fmt.Errorf("operation: dedupeKey required")
	}
	var sourceIdentity string
	if in.SourceProvider != "" || in.SourceAccount != "" || in.SourceExternalID != "" {
		var err error
		sourceIdentity, err = SourceIdentityDigest(in.SourceProvider, in.SourceAccount, in.SourceExternalID)
		if err != nil {
			return models.Operation{}, err
		}
	}
	op := models.Operation{
		OwnerUserID:         in.OwnerUserID,
		WorkspaceID:         firstNonEmpty(in.WorkspaceID, "local"),
		Title:               in.Title,
		Description:         in.Description,
		OperationType:       in.OperationType,
		SourceType:          in.SourceType,
		SourceID:            in.SourceID,
		SourceURI:           in.SourceURI,
		SourceReceivedAt:    in.SourceReceivedAt,
		SourceRevisionHash:  in.SourceRevisionHash,
		SourceProvider:      in.SourceProvider,
		SourceAccount:       in.SourceAccount,
		SourceExternalID:    in.SourceExternalID,
		SourceIdentityHash:  sourceIdentity,
		ProjectKey:          in.ProjectKey,
		AccountFeedID:       in.AccountFeedID,
		Status:              string(StatusNew),
		RiskLevel:           string(RiskLow),
		AutonomyLevel:       string(AutonomyObserve),
		OwnerType:           string(OwnerHAI),
		CurrentDecision:     string(DecisionObserveOnly),
		RequiresApproval:    false,
		VerificationStatus:  string(VerificationNotRequired),
		EvidenceJSON:        firstNonEmpty(in.EvidenceJSON, "{}"),
		WorldModelStateJSON: "{}",
		DedupeKey:           in.DedupeKey,
		CreatedAt:           now,
		UpdatedAt:           now,
		Version:             1,
	}
	return op, Validate(op)
}

// Validate enforces the Operation invariants (§10.7).
func Validate(op models.Operation) error {
	if err := validateOperationSourceIdentity(op); err != nil {
		return err
	}
	if strings.TrimSpace(op.OwnerUserID) == "" {
		return fmt.Errorf("operation: ownerUserId required")
	}
	if strings.TrimSpace(op.WorkspaceID) == "" {
		return fmt.Errorf("operation: workspaceId required")
	}
	if strings.TrimSpace(op.Title) == "" {
		return fmt.Errorf("operation: title required")
	}
	if strings.TrimSpace(op.DedupeKey) == "" {
		return fmt.Errorf("operation: dedupeKey required")
	}
	if !OperationStatus(op.Status).IsValid() {
		return fmt.Errorf("operation: invalid status %q", op.Status)
	}
	if !RiskLevel(op.RiskLevel).IsValid() {
		return fmt.Errorf("operation: invalid risk %q", op.RiskLevel)
	}
	if !AutonomyLevel(op.AutonomyLevel).IsValid() {
		return fmt.Errorf("operation: invalid autonomy %q", op.AutonomyLevel)
	}
	if !OwnerType(op.OwnerType).IsValid() {
		return fmt.Errorf("operation: invalid ownerType %q", op.OwnerType)
	}
	if !CurrentDecision(op.CurrentDecision).IsValid() {
		return fmt.Errorf("operation: invalid currentDecision %q", op.CurrentDecision)
	}
	if !VerificationStatus(op.VerificationStatus).IsValid() {
		return fmt.Errorf("operation: invalid verificationStatus %q", op.VerificationStatus)
	}
	// completedAt may only be set when Status=completed.
	if op.CompletedAt != nil && OperationStatus(op.Status) != StatusCompleted {
		return fmt.Errorf("operation: completedAt set but status is %q", op.Status)
	}
	return nil
}

// CanRun reports whether the Operation may execute now.
func CanRun(op models.Operation) bool {
	s := OperationStatus(op.Status)
	return s == StatusReady || s == StatusApproved
}

// NeedsRobert reports whether the Operation is waiting on Robert.
func NeedsRobert(op models.Operation) bool {
	return OperationStatus(op.Status) == StatusAwaitingApproval
}

// IsTerminal reports whether the Operation has reached a terminal status.
func IsTerminal(op models.Operation) bool {
	return OperationStatus(op.Status).IsTerminal()
}

// ApplyTransition validates a status change against the state machine and the
// §8 rules, mutates a copy of the operation, and returns the audit event.
func ApplyTransition(op models.Operation, to OperationStatus, actorType, actorID, message string, now time.Time) (models.Operation, models.OperationEvent, error) {
	from := OperationStatus(op.Status)
	if _, err := Transition(from, to); err != nil {
		return op, models.OperationEvent{}, err
	}
	// §8: no Operation may move to completed without verification unless
	// verificationStatus is not_required.
	if to == StatusCompleted {
		vs := VerificationStatus(op.VerificationStatus)
		if vs != VerificationPassed && vs != VerificationNotRequired {
			return op, models.OperationEvent{}, fmt.Errorf("operation: cannot complete with verification status %q", vs)
		}
	}
	op.Status = string(to)
	op.UpdatedAt = now
	op.Version++
	if to == StatusCompleted {
		completed := now
		op.CompletedAt = &completed
	}
	evt := models.OperationEvent{
		OperationID:  op.ID,
		EventType:    "status_change",
		ActorType:    actorType,
		ActorID:      actorID,
		BeforeStatus: string(from),
		AfterStatus:  string(to),
		Message:      message,
		PayloadJSON:  "{}",
		CreatedAt:    now,
	}
	return op, evt, nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
