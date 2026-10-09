package agentcoordination

import (
	"fmt"
	"time"
)

type TimeoutAction string

const (
	TimeoutNone         TimeoutAction = "none"
	TimeoutRemind       TimeoutAction = "remind"
	TimeoutEscalate     TimeoutAction = "escalate"
	TimeoutExpire       TimeoutAction = "expire"
	TimeoutManualReview TimeoutAction = "manual_review"
)

type EscalationPolicy struct {
	AcknowledgmentTimeout time.Duration `json:"acknowledgmentTimeout"`
	CompletionTimeout     time.Duration `json:"completionTimeout"`
	ReminderInterval      time.Duration `json:"reminderInterval"`
	MaximumReminders      int           `json:"maximumReminders"`
	MaximumEscalations    int           `json:"maximumEscalations"`
	EscalationRecipients  []string      `json:"escalationRecipients"`
}

type TimeoutEvaluation struct {
	Action     TimeoutAction `json:"action"`
	Reason     string        `json:"reason"`
	DueAt      time.Time     `json:"dueAt"`
	Recipients []string      `json:"recipients,omitempty"`
}

func ValidateEscalationPolicy(policy EscalationPolicy) error {
	if policy.AcknowledgmentTimeout <= 0 ||
		policy.CompletionTimeout <= 0 ||
		policy.ReminderInterval <= 0 ||
		policy.MaximumReminders < 0 ||
		policy.MaximumEscalations < 0 {
		return fmt.Errorf("timeout and escalation policy is incomplete")
	}
	return nil
}

func EvaluateMessageTimeout(
	message Message,
	acknowledgment *Acknowledgment,
	policy EscalationPolicy,
	reminderCount int,
	escalationCount int,
	now time.Time,
) (TimeoutEvaluation, error) {
	if err := ValidateEscalationPolicy(policy); err != nil {
		return TimeoutEvaluation{}, err
	}
	if reminderCount < 0 || escalationCount < 0 {
		return TimeoutEvaluation{}, fmt.Errorf("timeout counters cannot be negative")
	}
	now = now.UTC()
	if acknowledgment != nil {
		if err := validateAcknowledgment(message, *acknowledgment, now, false); err != nil {
			return TimeoutEvaluation{}, fmt.Errorf("validate message acknowledgment: %w", err)
		}
	}
	if !message.ExpiresAt.After(now) {
		return TimeoutEvaluation{
			Action: TimeoutExpire,
			Reason: "message envelope expired",
			DueAt:  message.ExpiresAt.UTC(),
		}, nil
	}
	if !message.RequiresAck {
		return TimeoutEvaluation{Action: TimeoutNone, Reason: "acknowledgment not required"}, nil
	}
	if acknowledgment != nil {
		switch acknowledgment.Status {
		case AcknowledgmentAccepted:
			return TimeoutEvaluation{Action: TimeoutNone, Reason: "message acknowledged"}, nil
		case AcknowledgmentRejected:
			return escalationEvaluation(
				"recipient rejected the message",
				policy,
				escalationCount,
				now,
			), nil
		case AcknowledgmentDeferred:
			if acknowledgment.RetryAfter != nil && now.Before(acknowledgment.RetryAfter.UTC()) {
				return TimeoutEvaluation{
					Action: TimeoutNone,
					Reason: "recipient deferred acknowledgment",
					DueAt:  acknowledgment.RetryAfter.UTC(),
				}, nil
			}
		}
	}
	dueAt := message.CreatedAt.UTC().Add(policy.AcknowledgmentTimeout)
	if now.Before(dueAt) {
		return TimeoutEvaluation{
			Action: TimeoutNone,
			Reason: "acknowledgment window remains open",
			DueAt:  dueAt,
		}, nil
	}
	if reminderCount < policy.MaximumReminders {
		reminderDueAt := dueAt.Add(time.Duration(reminderCount) * policy.ReminderInterval)
		if now.Before(reminderDueAt) {
			return TimeoutEvaluation{
				Action: TimeoutNone,
				Reason: "next acknowledgment reminder interval remains open",
				DueAt:  reminderDueAt,
			}, nil
		}
		return TimeoutEvaluation{
			Action: TimeoutRemind,
			Reason: "acknowledgment is overdue",
			DueAt:  reminderDueAt,
		}, nil
	}
	return escalationEvaluation(
		"acknowledgment remained overdue after reminders",
		policy,
		escalationCount,
		now,
	), nil
}

func EvaluateDelegationTimeout(
	delegation DelegationEnvelope,
	policy EscalationPolicy,
	escalationCount int,
	now time.Time,
) (TimeoutEvaluation, error) {
	if err := ValidateEscalationPolicy(policy); err != nil {
		return TimeoutEvaluation{}, err
	}
	if escalationCount < 0 {
		return TimeoutEvaluation{}, fmt.Errorf("escalation count cannot be negative")
	}
	if terminalDelegation(delegation.Status) {
		return TimeoutEvaluation{
			Action: TimeoutNone,
			Reason: "delegation is in a terminal state",
		}, nil
	}
	now = now.UTC()
	dueAt := delegation.DueAt.UTC()
	if timeoutAt := delegation.CreatedAt.UTC().Add(policy.CompletionTimeout); timeoutAt.Before(dueAt) {
		dueAt = timeoutAt
	}
	if now.Before(dueAt) {
		return TimeoutEvaluation{
			Action: TimeoutNone,
			Reason: "delegation completion window remains open",
			DueAt:  dueAt,
		}, nil
	}
	return escalationEvaluation(
		"delegation completion is overdue",
		policy,
		escalationCount,
		now,
	), nil
}

func escalationEvaluation(
	reason string,
	policy EscalationPolicy,
	escalationCount int,
	now time.Time,
) TimeoutEvaluation {
	recipients := normalizeStrings(policy.EscalationRecipients)
	if escalationCount >= policy.MaximumEscalations || len(recipients) == 0 {
		return TimeoutEvaluation{
			Action: TimeoutManualReview,
			Reason: reason + "; automated escalation limit reached",
			DueAt:  now.UTC(),
		}
	}
	return TimeoutEvaluation{
		Action:     TimeoutEscalate,
		Reason:     reason,
		DueAt:      now.UTC(),
		Recipients: recipients,
	}
}
