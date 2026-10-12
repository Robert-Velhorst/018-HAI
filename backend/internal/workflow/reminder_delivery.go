package workflow

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/proactivity"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

var errReminderDeliveryExpired = errors.New("internal reminder authority expired during signal recording")

const (
	ReminderDeliveryChannelInApp           = "in_app"
	ReminderDeliveryAuthorizationAuthority = "internal_reminder_delivery_authorization"
	ReminderDeliveryAttemptAuthority       = "internal_reminder_delivery_receipt"
	ReminderDeliveryAuthorizeConfirmation  = "AUTHORIZE ONE INTERNAL HAI REMINDER"
	ReminderDeliveryStatusDelivered        = "delivered"
	ReminderDeliveryStatusRetryableFailure = "retryable_failure"
	ReminderDeliveryStatusSuppressed       = "suppressed"
	ReminderDeliveryStatusDeadLettered     = "dead_lettered"
	ReminderDeliveryStatusExpired          = "expired"
	ReminderDeliveryMaxAttempts            = 3
)

type ReminderDeliveryAuthorizeRequest struct {
	ExpectedActivationRequestDigest  string `json:"expectedActivationRequestDigest" binding:"required"`
	ExpectedActivationDecisionDigest string `json:"expectedActivationDecisionDigest" binding:"required"`
	ExpectedReminderDigest           string `json:"expectedReminderDigest" binding:"required"`
	IdempotencyKey                   string `json:"idempotencyKey" binding:"required"`
	Channel                          string `json:"channel" binding:"required"`
	Confirmation                     string `json:"confirmation" binding:"required"`
}

type ReminderDeliveryAuthorizationResult struct {
	Authorization      models.WorkflowReminderDeliveryAuthorization `json:"authorization"`
	Replayed           bool                                         `json:"replayed"`
	Authority          string                                       `json:"authority"`
	DeliveryAuthorized bool                                         `json:"deliveryAuthorized"`
	CanExecute         bool                                         `json:"canExecute"`
}

type ReminderDeliveryEnvelope struct {
	Authorization models.WorkflowReminderDeliveryAuthorization
	Source        WorkflowReminderCandidate
}

type ReminderDeliverySink interface {
	DeliverInternalReminder(context.Context, ReminderDeliveryEnvelope) error
}

// TransactionalReminderDeliverySink may only write internal database effects
// through tx. External delivery cannot participate in this atomic boundary.
type TransactionalReminderDeliverySink interface {
	ReminderDeliverySink
	DeliverInternalReminderInTransaction(context.Context, *gorm.DB, ReminderDeliveryEnvelope) error
}

type ReminderDeliveryRunResult struct {
	AuthorizationID uuid.UUID `json:"authorizationId"`
	Status          string    `json:"status"`
	Reason          string    `json:"reason"`
}

type ReminderDeliveryRunSummary struct {
	Checked      int                         `json:"checked"`
	Delivered    int                         `json:"delivered"`
	Retried      int                         `json:"retried"`
	Suppressed   int                         `json:"suppressed"`
	DeadLettered int                         `json:"deadLettered"`
	Expired      int                         `json:"expired"`
	Results      []ReminderDeliveryRunResult `json:"results"`
}

type ReminderDeliveryHistory struct {
	Authorizations []models.WorkflowReminderDeliveryAuthorization `json:"authorizations"`
	Attempts       []models.WorkflowReminderDeliveryAttempt       `json:"attempts"`
	Authority      string                                         `json:"authority"`
	CanExecute     bool                                           `json:"canExecute"`
}

type ReminderDeliveryService interface {
	AuthorizeReminderDeliveryForOwner(string, string, uuid.UUID, ReminderDeliveryAuthorizeRequest) (*ReminderDeliveryAuthorizationResult, error)
	RunDueReminderDeliveries(RunDueRequest) (*ReminderDeliveryRunSummary, error)
	RunDueReminderDeliveriesForOwner(string, RunDueRequest) (*ReminderDeliveryRunSummary, error)
	ReminderDeliveryHistoryForOwner(string, int) (*ReminderDeliveryHistory, error)
}

type reminderDeliveryCandidate struct {
	Authorization models.WorkflowReminderDeliveryAuthorization
	AttemptCount  int
}

type reminderDeliveryRepository interface {
	FindOrCreateReminderDeliveryAuthorization(*models.WorkflowReminderDeliveryAuthorization) (*models.WorkflowReminderDeliveryAuthorization, bool, error)
	FindDueReminderDeliveryAuthorizations(string, time.Time, int, int) ([]reminderDeliveryCandidate, error)
	SaveReminderDeliveryAttempt(*models.WorkflowReminderDeliveryAttempt) (*models.WorkflowReminderDeliveryAttempt, bool, error)
	ListReminderDeliveryAuthorizationsForOwner(string, int) ([]models.WorkflowReminderDeliveryAuthorization, error)
	ListReminderDeliveryAttemptsForOwner(string, int) ([]models.WorkflowReminderDeliveryAttempt, error)
}

type reminderDeliveryExecutionRepository interface {
	reminderActivationRepository
	reminderDeliveryRepository
}

type transactionalReminderDeliveryRepository interface {
	ProcessReminderDelivery(reminderDeliveryCandidate, ReminderDeliverySink) (*ReminderDeliveryRunResult, error)
}

// Implemented only by explicit in-memory test fixtures, never by a database
// repository. Unknown repositories must not silently lose the atomic boundary.
type memoryReminderDeliveryRepository interface {
	reminderDeliveryExecutionRepository
	reminderDeliveryMemoryOnly()
}

func WithReminderDeliverySink(current Service, sink ReminderDeliverySink) (Service, error) {
	implementation, ok := current.(*service)
	if !ok || implementation == nil || sink == nil {
		return nil, fmt.Errorf("workflow reminder delivery needs the canonical service and an internal sink")
	}
	implementation.reminderDeliverySink = sink
	return implementation, nil
}

func (s *service) AuthorizeReminderDeliveryForOwner(owner, actor string, requestID uuid.UUID, request ReminderDeliveryAuthorizeRequest) (*ReminderDeliveryAuthorizationResult, error) {
	owner, actor = strings.TrimSpace(owner), strings.TrimSpace(actor)
	request.IdempotencyKey = strings.TrimSpace(request.IdempotencyKey)
	if owner == "" || actor != owner || requestID == uuid.Nil || request.Channel != ReminderDeliveryChannelInApp ||
		request.Confirmation != ReminderDeliveryAuthorizeConfirmation || !reminderActivationIdempotencyPattern.MatchString(request.IdempotencyKey) ||
		!validReminderDigest(request.ExpectedActivationRequestDigest) || !validReminderDigest(request.ExpectedActivationDecisionDigest) ||
		!validReminderDigest(request.ExpectedReminderDigest) {
		return nil, fmt.Errorf("valid owner-approved internal reminder delivery evidence is required")
	}
	repository, ok := s.repo.(interface {
		reminderActivationRepository
		reminderDeliveryRepository
	})
	if !ok {
		return nil, fmt.Errorf("durable reminder delivery storage is unavailable")
	}
	activation, latest, err := repository.LoadReminderActivationRequestForOwner(owner, requestID)
	if err != nil || activation == nil || latest == nil {
		return nil, firstReminderActivationError(err, "approved reminder preparation is unavailable")
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	if activation.RecordDigest != request.ExpectedActivationRequestDigest || latest.RecordDigest != request.ExpectedActivationDecisionDigest ||
		activation.ReminderDigest != request.ExpectedReminderDigest || latest.Decision != ReminderActivationDecisionApproved ||
		latest.ExpiresAt == nil || now.After(*latest.ExpiresAt) {
		return nil, fmt.Errorf("a current exact approved reminder preparation is required")
	}
	source, err := repository.LoadReminderActivationSourceForOwner(owner, activation.ChecklistItemID)
	if err != nil || source == nil {
		return nil, firstReminderActivationError(err, "reminder is no longer current")
	}
	currentDigest, err := reminderEvidenceDigest(*source)
	if err != nil || currentDigest != activation.ReminderDigest {
		return nil, fmt.Errorf("reminder changed; prepare and approve it again")
	}
	expiresAt := activation.ReminderAt.Add(workflowReminderDeliveryGrace)
	maximumExpiry := now.Add(workflowReminderMaxAuthorizationLifetime)
	if expiresAt.After(maximumExpiry) {
		expiresAt = maximumExpiry
	}
	if !expiresAt.After(now) {
		return nil, fmt.Errorf("reminder delivery window has expired")
	}
	authorization := &models.WorkflowReminderDeliveryAuthorization{
		ID: uuid.New(), ActivationRequestID: activation.ID, ActivationDecisionID: latest.ID,
		OwnerIdentity: owner, WorkflowID: activation.WorkflowID, ChecklistItemID: activation.ChecklistItemID,
		ReminderAt: activation.ReminderAt.UTC(), ReminderDigest: activation.ReminderDigest,
		ActivationRequestDigest: activation.RecordDigest, ActivationDecisionDigest: latest.RecordDigest,
		Channel: request.Channel, IdempotencyKey: request.IdempotencyKey,
		Authority: ReminderDeliveryAuthorizationAuthority, Actor: actor, Confirmation: request.Confirmation,
		AuthorizedAt: now, ExpiresAt: expiresAt,
	}
	authorization.RequestDigest, err = digestReminderActivationPayload(request)
	if err != nil {
		return nil, err
	}
	authorization.RecordDigest, err = digestReminderActivationPayload(authorization)
	if err != nil {
		return nil, err
	}
	stored, created, err := repository.FindOrCreateReminderDeliveryAuthorization(authorization)
	if err != nil {
		return nil, err
	}
	return &ReminderDeliveryAuthorizationResult{Authorization: *stored, Replayed: !created, Authority: ReminderDeliveryAuthorizationAuthority, DeliveryAuthorized: true, CanExecute: false}, nil
}

func (s *service) RunDueReminderDeliveries(request RunDueRequest) (*ReminderDeliveryRunSummary, error) {
	return s.runDueReminderDeliveries("", request)
}

func (s *service) RunDueReminderDeliveriesForOwner(owner string, request RunDueRequest) (*ReminderDeliveryRunSummary, error) {
	owner = strings.TrimSpace(owner)
	if owner == "" {
		return nil, fmt.Errorf("authenticated reminder owner is required")
	}
	return s.runDueReminderDeliveries(owner, request)
}

func (s *service) runDueReminderDeliveries(owner string, request RunDueRequest) (*ReminderDeliveryRunSummary, error) {
	return s.runDueReminderDeliveriesContext(context.Background(), owner, request)
}

func (s *service) runDueReminderDeliveriesContext(ctx context.Context, owner string, request RunDueRequest) (*ReminderDeliveryRunSummary, error) {
	if ctx == nil || s == nil {
		return nil, ErrReminderDeliveryContextUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.reminderDeliverySink == nil {
		return nil, fmt.Errorf("internal reminder delivery sink is unavailable")
	}
	repository, ok := s.repo.(interface {
		reminderActivationRepository
		reminderDeliveryRepository
	})
	if !ok {
		return nil, fmt.Errorf("durable reminder delivery storage is unavailable")
	}
	limit := request.Limit
	if limit <= 0 {
		limit = 10
	}
	if limit > 100 {
		return nil, fmt.Errorf("reminder delivery limit must not exceed 100")
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	candidates, err := repository.FindDueReminderDeliveryAuthorizations(owner, now, limit, ReminderDeliveryMaxAttempts)
	if err != nil {
		return nil, err
	}
	result := &ReminderDeliveryRunSummary{Results: []ReminderDeliveryRunResult{}}
	for _, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			return reminderDeliveryPartialResult(result, err)
		}
		var outcome *ReminderDeliveryRunResult
		if durable, ok := repository.(transactionalReminderDeliveryRepository); ok {
			outcome, err = durable.ProcessReminderDelivery(candidate, s.reminderDeliverySink)
		} else if _, memoryOnly := repository.(memoryReminderDeliveryRepository); memoryOnly {
			outcome, err = processReminderDeliveryContext(ctx, repository, candidate, s.reminderDeliverySink)
		} else {
			return nil, fmt.Errorf("atomic reminder delivery storage is unavailable")
		}
		if err != nil {
			return reminderDeliveryPartialResult(result, err)
		}
		if outcome == nil {
			continue
		}
		result.Checked++
		result.Results = append(result.Results, *outcome)
		switch outcome.Status {
		case ReminderDeliveryStatusDelivered:
			result.Delivered++
		case ReminderDeliveryStatusRetryableFailure:
			result.Retried++
		case ReminderDeliveryStatusDeadLettered:
			result.DeadLettered++
		case ReminderDeliveryStatusExpired:
			result.Expired++
		default:
			result.Suppressed++
		}
	}
	if err := ctx.Err(); err != nil {
		return reminderDeliveryPartialResult(result, err)
	}
	return result, nil
}

func reminderDeliveryPartialResult(result *ReminderDeliveryRunSummary, err error) (*ReminderDeliveryRunSummary, error) {
	if result == nil || result.Checked == 0 {
		return nil, err
	}
	return result, err
}

func processReminderDelivery(repository reminderDeliveryExecutionRepository, candidate reminderDeliveryCandidate, sink ReminderDeliverySink) (*ReminderDeliveryRunResult, error) {
	return processReminderDeliveryContext(context.Background(), repository, candidate, sink)
}

func processReminderDeliveryContext(ctx context.Context, repository reminderDeliveryExecutionRepository, candidate reminderDeliveryCandidate, sink ReminderDeliverySink) (*ReminderDeliveryRunResult, error) {
	if ctx == nil {
		return nil, ErrReminderDeliveryContextUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	authorization := candidate.Authorization
	status, reason := ReminderDeliveryStatusDelivered, "internal reminder signal recorded"
	if !authorization.ExpiresAt.After(time.Now().UTC()) {
		status, reason = ReminderDeliveryStatusExpired, "delivery authorization expired before the reminder was processed"
	} else {
		activation, latest, loadErr := repository.LoadReminderActivationRequestForOwner(authorization.OwnerIdentity, authorization.ActivationRequestID)
		if err := ctx.Err(); err != nil {
			return nil, errors.Join(err, loadErr)
		}
		if errors.Is(loadErr, context.Canceled) || errors.Is(loadErr, context.DeadlineExceeded) {
			return nil, loadErr
		}
		source, sourceErr := repository.LoadReminderActivationSourceForOwner(authorization.OwnerIdentity, authorization.ChecklistItemID)
		if err := ctx.Err(); err != nil {
			return nil, errors.Join(err, sourceErr)
		}
		if errors.Is(sourceErr, context.Canceled) || errors.Is(sourceErr, context.DeadlineExceeded) {
			return nil, sourceErr
		}
		if loadErr != nil || sourceErr != nil {
			status, reason = ReminderDeliveryStatusRetryableFailure, "current reminder authority could not be revalidated"
		} else if activation == nil || latest == nil || source == nil || latest.ID != authorization.ActivationDecisionID ||
			latest.Decision != ReminderActivationDecisionApproved || activation.RecordDigest != authorization.ActivationRequestDigest ||
			latest.RecordDigest != authorization.ActivationDecisionDigest {
			status, reason = ReminderDeliveryStatusSuppressed, "reminder authority was revoked, replaced, or became unavailable"
		} else if digest, digestErr := reminderEvidenceDigest(*source); digestErr != nil || digest != authorization.ReminderDigest {
			status, reason = ReminderDeliveryStatusSuppressed, "reminder source changed before delivery"
		} else if !authorization.ExpiresAt.After(time.Now().UTC()) {
			status, reason = ReminderDeliveryStatusExpired, "delivery authorization expired during reminder revalidation"
		} else if deliverErr := sink.DeliverInternalReminder(ctx, ReminderDeliveryEnvelope{Authorization: authorization, Source: *source}); deliverErr != nil {
			if errors.Is(deliverErr, context.Canceled) || errors.Is(deliverErr, context.DeadlineExceeded) {
				return nil, deliverErr
			}
			if errors.Is(deliverErr, errReminderDeliveryExpired) {
				status, reason = ReminderDeliveryStatusExpired, "delivery authorization expired during internal signal recording"
			} else {
				status, reason = ReminderDeliveryStatusRetryableFailure, "internal reminder sink failed"
			}
		}
	}
	// Cancellation is not a retry receipt. In the durable path this returns an
	// error to the existing signal-plus-receipt transaction for rollback.
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	attemptNumber := candidate.AttemptCount + 1
	if status == ReminderDeliveryStatusRetryableFailure && attemptNumber >= ReminderDeliveryMaxAttempts {
		status = ReminderDeliveryStatusDeadLettered
		reason = fmt.Sprintf("delivery exhausted after %d attempts: %s", ReminderDeliveryMaxAttempts, reason)
	}
	attempt := &models.WorkflowReminderDeliveryAttempt{
		ID: uuid.New(), AuthorizationID: authorization.ID, OwnerIdentity: authorization.OwnerIdentity,
		AttemptNumber: attemptNumber, Status: status, Reason: reason,
		ReminderDigest: authorization.ReminderDigest, AuthorizationDigest: authorization.RecordDigest,
		Authority: ReminderDeliveryAttemptAuthority, AttemptedAt: time.Now().UTC().Truncate(time.Microsecond),
	}
	var err error
	attempt.RecordDigest, err = digestReminderActivationPayload(attempt)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if _, _, err = repository.SaveReminderDeliveryAttempt(attempt); err != nil {
		return nil, err
	}
	return &ReminderDeliveryRunResult{AuthorizationID: authorization.ID, Status: status, Reason: reason}, nil
}

func (s *service) ReminderDeliveryHistoryForOwner(owner string, limit int) (*ReminderDeliveryHistory, error) {
	owner = strings.TrimSpace(owner)
	repository, ok := s.repo.(reminderDeliveryRepository)
	if owner == "" || limit < 1 || limit > 100 || !ok {
		return nil, fmt.Errorf("valid reminder delivery history scope is required")
	}
	authorizations, err := repository.ListReminderDeliveryAuthorizationsForOwner(owner, limit)
	if err != nil {
		return nil, err
	}
	attempts, err := repository.ListReminderDeliveryAttemptsForOwner(owner, limit)
	if err != nil {
		return nil, err
	}
	return &ReminderDeliveryHistory{Authorizations: authorizations, Attempts: attempts, Authority: ReminderDeliveryAttemptAuthority, CanExecute: false}, nil
}

type ProactivityReminderDeliverySink struct{ service *proactivity.Service }

func NewProactivityReminderDeliverySink(service *proactivity.Service) ReminderDeliverySink {
	return &ProactivityReminderDeliverySink{service: service}
}

func (s *ProactivityReminderDeliverySink) DeliverInternalReminder(ctx context.Context, envelope ReminderDeliveryEnvelope) error {
	return s.deliver(ctx, nil, envelope)
}

func (s *ProactivityReminderDeliverySink) DeliverInternalReminderInTransaction(ctx context.Context, tx *gorm.DB, envelope ReminderDeliveryEnvelope) error {
	if tx == nil {
		return fmt.Errorf("internal reminder transaction is required")
	}
	return s.deliver(ctx, tx, envelope)
}

func (s *ProactivityReminderDeliverySink) deliver(ctx context.Context, tx *gorm.DB, envelope ReminderDeliveryEnvelope) error {
	if s == nil || s.service == nil {
		return fmt.Errorf("proactivity reminder sink is unavailable")
	}
	authorization, source := envelope.Authorization, envelope.Source
	observedAt := authorization.ReminderAt.UTC()
	if observedAt.IsZero() {
		observedAt = authorization.AuthorizedAt.UTC()
	}
	deadline := authorization.ReminderAt
	if source.Reminder.DueAt != nil {
		deadline = source.Reminder.DueAt.UTC()
	}
	signal := proactivity.OpenLoopSignal{
		ContractVersion: proactivity.ContractVersion, ID: "workflow-reminder-" + authorization.ID.String(),
		OwnerIdentity: authorization.OwnerIdentity, OpenLoopKey: "workflow-reminder-" + authorization.ChecklistItemID.String(),
		Title: source.Workflow.Title, Summary: source.Reminder.Label, Status: proactivity.StatusOpen,
		Risk: reminderProactivityRisk(source.Workflow.RiskLevel), ObservedAt: observedAt, LastActivityAt: observedAt,
		Deadline: &deadline, StaleAfter: 7 * 24 * time.Hour, Impact: 0.6, Urgency: 0.8, Confidence: 1,
		Evidence: []proactivity.EvidenceReference{{ID: authorization.ChecklistItemID.String(), Kind: "workflow_reminder", Digest: authorization.ReminderDigest, ObservedAt: observedAt}},
	}
	key := "workflow-reminder:" + authorization.ID.String()
	if tx != nil {
		_, _, err := s.service.RecordSignalsInTransaction(ctx, tx, authorization.OwnerIdentity, key, []proactivity.OpenLoopSignal{signal})
		return err
	}
	_, _, err := s.service.RecordSignals(ctx, authorization.OwnerIdentity, key, []proactivity.OpenLoopSignal{signal})
	return err
}

func reminderProactivityRisk(value string) proactivity.RiskLevel {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "critical":
		return proactivity.RiskCritical
	case "high":
		return proactivity.RiskHigh
	case "medium":
		return proactivity.RiskMedium
	default:
		return proactivity.RiskLow
	}
}
