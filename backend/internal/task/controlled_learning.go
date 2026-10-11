package task

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"automation-hub-backend/internal/controlledlearning"
)

const controlledLearningRecordTimeout = 5 * time.Second

// ControlledLearningRecorder is the narrow write boundary used by the task
// engine. It accepts only the verified outcome and explicit correction
// records defined by controlledlearning; it cannot activate a learning
// proposal or modify policy.
type ControlledLearningRecorder interface {
	RecordOutcome(
		context.Context,
		controlledlearning.RecordOutcomeRequest,
	) (controlledlearning.OutcomeRecord, error)
}

// WithControlledLearning attaches the durable controlled-learning ledger to a
// freshly composed task service. Unsupported service implementations are
// returned unchanged so test doubles and protocol previews remain side-effect
// free.
func WithControlledLearning(
	base Service,
	recorder ControlledLearningRecorder,
) Service {
	implementation, ok := base.(*service)
	if !ok || recorder == nil {
		return base
	}
	implementation.controlledLearning = recorder
	return implementation
}

func (s *service) recordVerifiedLearningOutcome(plan *CompletionPlan) {
	if s == nil || s.controlledLearning == nil || plan == nil ||
		strings.TrimSpace(plan.ID) == "" ||
		strings.TrimSpace(plan.OwnerIdentity) == "" ||
		plan.ExecutionResult == nil || executionOutcomeUncertain(plan.ExecutionResult) {
		return
	}
	verificationStatus, ok := controlledLearningVerification(
		plan.ExecutionResult.VerificationStatus,
	)
	if !ok {
		return
	}

	retrievedAt := plan.ExecutionResult.CompletedAt.UTC()
	if retrievedAt.IsZero() {
		retrievedAt = time.Now().UTC()
	}
	sourceID := "task-plan:" + strings.TrimSpace(plan.ID)
	sourceURI := "task-plan://" + strings.TrimSpace(plan.ID)
	sourceDigest := sha256.Sum256([]byte(strings.Join([]string{
		plan.ID,
		plan.CompletionStatus,
		plan.ValidationResult.Status,
		plan.ExecutionResult.VerificationStatus,
		fmt.Sprintf("%t", plan.ValidationResult.Passed),
	}, "\x00")))
	criteria := controlledLearningCriteria(plan.ValidationResult.Criteria, sourceID)
	status := controlledlearning.OutcomePartial
	switch {
	case plan.CompletionStatus == "validated" && plan.ValidationResult.Passed:
		status = controlledlearning.OutcomeSucceeded
	case len(plan.ValidationResult.Failures) > 0:
		status = controlledlearning.OutcomeFailed
	}
	tags := []string{
		"task-engine",
		strings.TrimSpace(plan.Intake.TaskType),
		strings.TrimSpace(plan.ModelDecision.SelectedModelID),
	}
	if plan.FrameworkDecision != nil {
		for _, selected := range plan.FrameworkDecision.Selected {
			tags = append(tags, strings.TrimSpace(selected.ID))
		}
	}

	ctx, cancel := context.WithTimeout(
		context.Background(),
		controlledLearningRecordTimeout,
	)
	defer cancel()
	outcome, err := s.controlledLearning.RecordOutcome(
		ctx,
		controlledlearning.RecordOutcomeRequest{
			OwnerIdentity:  strings.TrimSpace(plan.OwnerIdentity),
			IdempotencyKey: "task-outcome:" + strings.TrimSpace(plan.ID),
			OperationID:    strings.TrimSpace(plan.ID),
			ProjectKey:     strings.TrimSpace(plan.ProjectKey),
			Basis:          controlledlearning.EvidenceVerifiedOutcome,
			Status:         status,
			Summary:        controlledLearningOutcomeSummary(plan, status),
			Verification:   verificationStatus,
			Sources: []controlledlearning.SourceReference{{
				ID:          sourceID,
				Kind:        "task_completion_plan",
				URI:         sourceURI,
				RetrievedAt: retrievedAt,
				ContentHash: hex.EncodeToString(sourceDigest[:]),
			}},
			Criteria:   criteria,
			Tags:       tags,
			OccurredAt: retrievedAt,
		},
	)
	if err != nil {
		plan.Events = append(
			plan.Events,
			event(
				"learning",
				"verified outcome could not be added to the controlled-learning ledger",
			),
		)
		return
	}
	plan.Events = append(
		plan.Events,
		event(
			"learning",
			"verified outcome recorded for controlled review: "+outcome.ID,
		),
	)
}

func (s *service) recordHumanCorrection(
	ownerIdentity string,
	item ReviewQueueItem,
	decision ReviewDecisionRecord,
) string {
	if s == nil || s.controlledLearning == nil ||
		strings.TrimSpace(decision.ResolutionNote) == "" ||
		decision.Decision != "rejected" {
		return ""
	}
	occurredAt := decision.ResolvedAt.UTC()
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	sourceID := "task-review:" + strings.TrimSpace(item.ID)
	ctx, cancel := context.WithTimeout(
		context.Background(),
		controlledLearningRecordTimeout,
	)
	defer cancel()
	outcome, err := s.controlledLearning.RecordOutcome(
		ctx,
		controlledlearning.RecordOutcomeRequest{
			OwnerIdentity:  strings.TrimSpace(ownerIdentity),
			IdempotencyKey: "task-correction:" + strings.TrimSpace(decision.ID),
			OperationID:    firstNonEmpty(strings.TrimSpace(item.TaskID), strings.TrimSpace(item.ID)),
			ProjectKey:     strings.TrimSpace(item.Request.ProjectKey),
			Basis:          controlledlearning.EvidenceHumanCorrection,
			Status:         controlledlearning.OutcomeCorrected,
			Summary:        "Human review rejected the proposed task action and supplied a correction.",
			ActorIdentity:  strings.TrimSpace(decision.ResolvedBy),
			HumanConfirmed: true,
			Correction:     strings.TrimSpace(decision.ResolutionNote),
			Verification:   controlledlearning.VerificationHumanApproved,
			Sources: []controlledlearning.SourceReference{{
				ID:          sourceID,
				Kind:        "task_review_decision",
				URI:         "task-review://" + strings.TrimSpace(item.ID),
				RetrievedAt: occurredAt,
				ContentHash: strings.TrimSpace(decision.RequestDigest),
			}},
			Tags:       []string{"task-engine", "human-correction"},
			OccurredAt: occurredAt,
		},
	)
	if err != nil {
		return ""
	}
	return outcome.ID
}

func controlledLearningVerification(
	value string,
) (controlledlearning.VerificationStatus, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case string(controlledlearning.VerificationVerified):
		return controlledlearning.VerificationVerified, true
	case string(controlledlearning.VerificationSourceSupported):
		return controlledlearning.VerificationSourceSupported, true
	case string(controlledlearning.VerificationTestPassed):
		return controlledlearning.VerificationTestPassed, true
	case string(controlledlearning.VerificationHumanApproved):
		return controlledlearning.VerificationHumanApproved, true
	default:
		return "", false
	}
}

func controlledLearningCriteria(
	values []ValidationCriterionResult,
	sourceID string,
) []controlledlearning.CriterionResult {
	result := make([]controlledlearning.CriterionResult, 0, len(values))
	for index, value := range values {
		description := strings.TrimSpace(value.Criterion)
		if description == "" {
			continue
		}
		result = append(result, controlledlearning.CriterionResult{
			ID:          fmt.Sprintf("criterion-%03d", index+1),
			Description: description,
			Passed: value.Status == validationCriterionPassed ||
				value.Status == validationCriterionNotApplicable,
			SourceIDs: []string{sourceID},
		})
		if len(result) == 100 {
			break
		}
	}
	return result
}

func controlledLearningOutcomeSummary(
	plan *CompletionPlan,
	status controlledlearning.OutcomeStatus,
) string {
	return fmt.Sprintf(
		"Task %s finished with outcome %s, completion state %s, and verification %s.",
		strings.TrimSpace(plan.ID),
		status,
		strings.TrimSpace(plan.CompletionStatus),
		strings.TrimSpace(plan.ExecutionResult.VerificationStatus),
	)
}
