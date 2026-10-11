package agentcoordination

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestEvaluateMessageTimeoutProgressesFromWaitToReminderToEscalation(t *testing.T) {
	t.Parallel()

	now := fixedNow()
	message := validMessage(t, now)
	policy := EscalationPolicy{
		AcknowledgmentTimeout: 5 * time.Minute,
		CompletionTimeout:     time.Hour,
		ReminderInterval:      5 * time.Minute,
		MaximumReminders:      2,
		MaximumEscalations:    1,
		EscalationRecipients:  []string{"human-operator"},
	}
	evaluation, err := EvaluateMessageTimeout(message, nil, policy, 0, 0, now.Add(time.Minute))
	if err != nil || evaluation.Action != TimeoutNone {
		t.Fatalf("initial evaluation = %#v, %v", evaluation, err)
	}
	evaluation, err = EvaluateMessageTimeout(message, nil, policy, 0, 0, now.Add(6*time.Minute))
	if err != nil || evaluation.Action != TimeoutRemind {
		t.Fatalf("reminder evaluation = %#v, %v", evaluation, err)
	}
	evaluation, err = EvaluateMessageTimeout(message, nil, policy, 2, 0, now.Add(20*time.Minute))
	if err != nil || evaluation.Action != TimeoutEscalate {
		t.Fatalf("escalation evaluation = %#v, %v", evaluation, err)
	}
	evaluation, err = EvaluateMessageTimeout(message, nil, policy, 2, 1, now.Add(25*time.Minute))
	if err != nil || evaluation.Action != TimeoutManualReview {
		t.Fatalf("manual review evaluation = %#v, %v", evaluation, err)
	}
}

func TestEvaluateMessageTimeoutRejectsMisattributedAcknowledgment(t *testing.T) {
	t.Parallel()

	now := fixedNow()
	message := validMessage(t, now)
	policy := EscalationPolicy{
		AcknowledgmentTimeout: 5 * time.Minute,
		CompletionTimeout:     time.Hour,
		ReminderInterval:      5 * time.Minute,
		MaximumReminders:      0,
		MaximumEscalations:    1,
		EscalationRecipients:  []string{"human-operator"},
	}
	base := Acknowledgment{
		ID:             uuid.NewString(),
		MessageID:      message.ID,
		CorrelationID:  message.CorrelationID,
		RecipientID:    message.Recipient.ID,
		Status:         AcknowledgmentAccepted,
		CreatedAt:      now.Add(5 * time.Minute),
		IdempotencyKey: uuid.NewString(),
	}
	validEvaluation, err := EvaluateMessageTimeout(
		message,
		&base,
		policy,
		0,
		0,
		now.Add(6*time.Minute),
	)
	if err != nil || validEvaluation.Action != TimeoutNone {
		t.Fatalf("matching acknowledgment evaluation = %#v, %v", validEvaluation, err)
	}

	tests := map[string]func(*Acknowledgment){
		"different message": func(ack *Acknowledgment) {
			ack.MessageID = uuid.NewString()
		},
		"different correlation": func(ack *Acknowledgment) {
			ack.CorrelationID = uuid.NewString()
		},
		"different recipient": func(ack *Acknowledgment) {
			ack.RecipientID = "another-agent"
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			acknowledgment := base
			mutate(&acknowledgment)
			evaluation, err := EvaluateMessageTimeout(
				message,
				&acknowledgment,
				policy,
				0,
				0,
				now.Add(6*time.Minute),
			)
			if err == nil {
				t.Fatalf("misattributed acknowledgment suppressed timeout: %#v", evaluation)
			}
		})
	}
}

func TestEvaluateMessageTimeoutResumesAfterDeferredRetryDeadline(t *testing.T) {
	t.Parallel()

	now := fixedNow()
	message := validMessage(t, now)
	createdAt := now.Add(time.Minute)
	retryAfter := now.Add(5 * time.Minute)
	acknowledgment := Acknowledgment{
		ID:             uuid.NewString(),
		MessageID:      message.ID,
		CorrelationID:  message.CorrelationID,
		RecipientID:    message.Recipient.ID,
		Status:         AcknowledgmentDeferred,
		Reason:         "temporarily unavailable",
		CreatedAt:      createdAt,
		RetryAfter:     &retryAfter,
		IdempotencyKey: uuid.NewString(),
	}
	policy := EscalationPolicy{
		AcknowledgmentTimeout: 3 * time.Minute,
		CompletionTimeout:     time.Hour,
		ReminderInterval:      time.Minute,
		MaximumReminders:      1,
		MaximumEscalations:    0,
	}
	evaluation, err := EvaluateMessageTimeout(message, &acknowledgment, policy, 0, 0, now.Add(6*time.Minute))
	if err != nil || evaluation.Action != TimeoutRemind {
		t.Fatalf("timeout after deferred retry = %#v, %v; want reminder", evaluation, err)
	}
	if err := ValidateAcknowledgment(message, acknowledgment, now.Add(6*time.Minute)); err == nil {
		t.Fatal("ingress validation accepted an acknowledgment whose retry time already passed")
	}
}

func TestEvaluateMessageTimeoutWaitsForReminderInterval(t *testing.T) {
	t.Parallel()

	now := fixedNow()
	message := validMessage(t, now)
	policy := EscalationPolicy{
		AcknowledgmentTimeout: 5 * time.Minute,
		CompletionTimeout:     time.Hour,
		ReminderInterval:      5 * time.Minute,
		MaximumReminders:      2,
		MaximumEscalations:    1,
		EscalationRecipients:  []string{"human-operator"},
	}
	evaluation, err := EvaluateMessageTimeout(message, nil, policy, 1, 0, now.Add(6*time.Minute))
	if err != nil || evaluation.Action != TimeoutNone {
		t.Fatalf("evaluation before next reminder = %#v, %v; want no action", evaluation, err)
	}
	wantDueAt := message.CreatedAt.Add(10 * time.Minute)
	if !evaluation.DueAt.Equal(wantDueAt) {
		t.Fatalf("next reminder due at %s, want %s", evaluation.DueAt, wantDueAt)
	}
	evaluation, err = EvaluateMessageTimeout(message, nil, policy, 1, 0, now.Add(10*time.Minute))
	if err != nil || evaluation.Action != TimeoutRemind {
		t.Fatalf("evaluation at next reminder = %#v, %v; want reminder", evaluation, err)
	}
}

func TestEvaluateDelegationTimeoutDoesNotEscalateTerminalWork(t *testing.T) {
	t.Parallel()

	now := fixedNow()
	delegation := validDelegation(now)
	delegation.Status = DelegationCompleted
	evaluation, err := EvaluateDelegationTimeout(
		delegation,
		EscalationPolicy{
			AcknowledgmentTimeout: time.Minute,
			CompletionTimeout:     time.Minute,
			ReminderInterval:      time.Minute,
			MaximumReminders:      1,
			MaximumEscalations:    1,
			EscalationRecipients:  []string{"human-operator"},
		},
		0,
		now.Add(time.Hour),
	)
	if err != nil || evaluation.Action != TimeoutNone {
		t.Fatalf("terminal delegation evaluation = %#v, %v", evaluation, err)
	}
}
