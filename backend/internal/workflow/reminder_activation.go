package workflow

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"automation-hub-backend/internal/models"

	"github.com/google/uuid"
)

const (
	ReminderActivationKindInternal          = "internal_notification"
	ReminderActivationRequestAuthority      = "reminder_activation_request_only"
	ReminderActivationDecisionAuthority     = "reminder_activation_decision_only"
	ReminderActivationHistoryAuthority      = "reminder_activation_history_only"
	ReminderActivationPrepareConfirmation   = "PREPARE INTERNAL REMINDER ONLY"
	ReminderActivationApproveConfirmation   = "APPROVE INTERNAL REMINDER PREPARATION"
	ReminderActivationRejectConfirmation    = "REJECT INTERNAL REMINDER PREPARATION"
	ReminderActivationClarifyConfirmation   = "REQUEST REMINDER CLARIFICATION"
	ReminderActivationRevokeConfirmation    = "REVOKE INTERNAL REMINDER PREPARATION"
	ReminderActivationDecisionApproved      = "approved"
	ReminderActivationDecisionRejected      = "rejected"
	ReminderActivationDecisionClarification = "needs_clarification"
	ReminderActivationDecisionRevoked       = "revoked"
)

var reminderActivationIdempotencyPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,160}$`)

type ReminderActivationPrepareRequest struct {
	ExpectedReminderDigest string `json:"expectedReminderDigest" binding:"required"`
	IdempotencyKey         string `json:"idempotencyKey" binding:"required"`
	ActivationKind         string `json:"activationKind" binding:"required"`
	Confirmation           string `json:"confirmation" binding:"required"`
}

type ReminderActivationDecisionRequest struct {
	Decision                        string `json:"decision" binding:"required"`
	Reason                          string `json:"reason" binding:"required"`
	Confirmation                    string `json:"confirmation" binding:"required"`
	ExpectedActivationRequestDigest string `json:"expectedActivationRequestDigest" binding:"required"`
	ExpectedPreviousDecisionID      string `json:"expectedPreviousDecisionId"`
}

type ReminderActivationRequestResult struct {
	Request    models.WorkflowReminderActivationRequest `json:"request"`
	Replayed   bool                                     `json:"replayed"`
	Authority  string                                   `json:"authority"`
	CanExecute bool                                     `json:"canExecute"`
}

type ReminderActivationDecisionResult struct {
	Decision   models.WorkflowReminderActivationDecision `json:"decision"`
	Replayed   bool                                      `json:"replayed"`
	Authority  string                                    `json:"authority"`
	CanExecute bool                                      `json:"canExecute"`
}

type ReminderActivationHistoryItem struct {
	Request        models.WorkflowReminderActivationRequest   `json:"request"`
	LatestDecision *models.WorkflowReminderActivationDecision `json:"latestDecision,omitempty"`
	Status         string                                     `json:"status"`
	Current        bool                                       `json:"current"`
	CanExecute     bool                                       `json:"canExecute"`
}

type ReminderActivationHistorySnapshot struct {
	Items      []ReminderActivationHistoryItem `json:"items"`
	Authority  string                          `json:"authority"`
	CanExecute bool                            `json:"canExecute"`
	CheckedAt  time.Time                       `json:"checkedAt"`
}

type ReminderActivationDecisionHistory struct {
	Decisions  []models.WorkflowReminderActivationDecision `json:"decisions"`
	Authority  string                                      `json:"authority"`
	CanExecute bool                                        `json:"canExecute"`
}

type ReminderActivationService interface {
	PrepareReminderActivationForOwner(string, string, uuid.UUID, ReminderActivationPrepareRequest) (*ReminderActivationRequestResult, error)
	ReminderActivationHistoryForOwner(string, int) (*ReminderActivationHistorySnapshot, error)
	DecideReminderActivationForOwner(string, string, uuid.UUID, ReminderActivationDecisionRequest) (*ReminderActivationDecisionResult, error)
	ReminderActivationDecisionHistoryForOwner(string, uuid.UUID, int) (*ReminderActivationDecisionHistory, error)
}

type reminderActivationRepository interface {
	LoadReminderActivationSourceForOwner(string, uuid.UUID) (*WorkflowReminderCandidate, error)
	FindOrCreateReminderActivationRequest(*models.WorkflowReminderActivationRequest) (*models.WorkflowReminderActivationRequest, bool, error)
	ListReminderActivationRequestsForOwner(string, int) ([]models.WorkflowReminderActivationRequest, error)
	LoadReminderActivationRequestForOwner(string, uuid.UUID) (*models.WorkflowReminderActivationRequest, *models.WorkflowReminderActivationDecision, error)
	SaveReminderActivationDecision(*models.WorkflowReminderActivationDecision) (*models.WorkflowReminderActivationDecision, bool, error)
	ListReminderActivationDecisionsForOwner(string, uuid.UUID, int) ([]models.WorkflowReminderActivationDecision, error)
}

func (s *service) PrepareReminderActivationForOwner(
	ownerIdentity, actor string,
	checklistItemID uuid.UUID,
	request ReminderActivationPrepareRequest,
) (*ReminderActivationRequestResult, error) {
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	actor = strings.TrimSpace(actor)
	request.IdempotencyKey = strings.TrimSpace(request.IdempotencyKey)
	request.ExpectedReminderDigest = strings.TrimSpace(request.ExpectedReminderDigest)
	if ownerIdentity == "" || actor != ownerIdentity || checklistItemID == uuid.Nil {
		return nil, fmt.Errorf("authenticated owner evidence is required")
	}
	if request.ActivationKind != ReminderActivationKindInternal ||
		request.Confirmation != ReminderActivationPrepareConfirmation ||
		!validReminderDigest(request.ExpectedReminderDigest) ||
		!reminderActivationIdempotencyPattern.MatchString(request.IdempotencyKey) {
		return nil, fmt.Errorf("valid internal reminder preparation evidence is required")
	}
	repository, ok := s.repo.(reminderActivationRepository)
	if !ok {
		return nil, fmt.Errorf("durable reminder activation storage is unavailable")
	}
	source, err := repository.LoadReminderActivationSourceForOwner(ownerIdentity, checklistItemID)
	if err != nil {
		return nil, err
	}
	if source == nil {
		return nil, fmt.Errorf("reminder is unavailable to this owner")
	}
	currentDigest, err := reminderEvidenceDigest(*source)
	if err != nil || currentDigest != request.ExpectedReminderDigest {
		return nil, fmt.Errorf("reminder changed; inspect a fresh reminder snapshot")
	}
	preparedAt := time.Now().UTC()
	if source.Reminder.ReminderAt.After(preparedAt.Add(workflowReminderMaxHorizon)) {
		return nil, fmt.Errorf("reminder is outside the supported 30-day delivery horizon")
	}
	now := preparedAt.Truncate(time.Second)
	activation := &models.WorkflowReminderActivationRequest{
		ID: uuid.New(), OwnerIdentity: ownerIdentity, WorkflowID: source.Workflow.ID,
		ChecklistItemID: source.Reminder.ID, ActivationKind: ReminderActivationKindInternal,
		WorkflowState: source.Workflow.CurrentState, ChecklistStatus: source.Reminder.Status,
		ReminderAt: source.Reminder.ReminderAt.UTC(), DueAt: utcTimePointer(source.Reminder.DueAt),
		ReminderDigest: currentDigest, IdempotencyKey: request.IdempotencyKey,
		Authority: ReminderActivationRequestAuthority, Actor: actor,
		Confirmation: request.Confirmation, RequestedAt: now, ExpiresAt: now.Add(15 * time.Minute),
	}
	activation.RequestDigest, err = digestReminderActivationPayload(struct {
		ChecklistItemID, ExpectedReminderDigest, IdempotencyKey, ActivationKind, Confirmation string
	}{checklistItemID.String(), request.ExpectedReminderDigest, request.IdempotencyKey, request.ActivationKind, request.Confirmation})
	if err != nil {
		return nil, err
	}
	activation.RecordDigest, err = digestReminderActivationRequest(activation)
	if err != nil {
		return nil, err
	}
	stored, created, err := repository.FindOrCreateReminderActivationRequest(activation)
	if err != nil {
		return nil, err
	}
	if err := validateReminderActivationRequest(stored); err != nil {
		return nil, err
	}
	return &ReminderActivationRequestResult{
		Request: *stored, Replayed: !created,
		Authority: ReminderActivationRequestAuthority, CanExecute: false,
	}, nil
}

func (s *service) ReminderActivationHistoryForOwner(ownerIdentity string, limit int) (*ReminderActivationHistorySnapshot, error) {
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	if ownerIdentity == "" || limit < 1 || limit > 100 {
		return nil, fmt.Errorf("valid owner and history limit are required")
	}
	repository, ok := s.repo.(reminderActivationRepository)
	if !ok {
		return nil, fmt.Errorf("durable reminder activation storage is unavailable")
	}
	requests, err := repository.ListReminderActivationRequestsForOwner(ownerIdentity, limit)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	result := &ReminderActivationHistorySnapshot{
		Items: []ReminderActivationHistoryItem{}, Authority: ReminderActivationHistoryAuthority,
		CanExecute: false, CheckedAt: now,
	}
	for _, request := range requests {
		if err := validateReminderActivationRequest(&request); err != nil {
			return nil, err
		}
		stored, latest, err := repository.LoadReminderActivationRequestForOwner(ownerIdentity, request.ID)
		if err != nil || stored == nil {
			return nil, firstReminderActivationError(err, "reminder activation history changed")
		}
		item := ReminderActivationHistoryItem{Request: *stored, Status: "prepared", Current: true, CanExecute: false}
		if latest != nil {
			if err := validateReminderActivationDecision(stored, latest); err != nil {
				return nil, err
			}
			item.LatestDecision = latest
			item.Status = latest.Decision
		}
		source, sourceErr := repository.LoadReminderActivationSourceForOwner(ownerIdentity, stored.ChecklistItemID)
		if sourceErr != nil {
			return nil, sourceErr
		}
		if source == nil {
			item.Current = false
			item.Status = "stale"
		} else if digest, digestErr := reminderEvidenceDigest(*source); digestErr != nil || digest != stored.ReminderDigest {
			item.Current = false
			item.Status = "stale"
		}
		if now.After(stored.ExpiresAt) && item.Status == "prepared" {
			item.Status = "expired"
		}
		if latest != nil && latest.Decision == ReminderActivationDecisionApproved &&
			(latest.ExpiresAt == nil || now.After(*latest.ExpiresAt)) {
			item.Status = "expired"
		}
		result.Items = append(result.Items, item)
	}
	return result, nil
}

func (s *service) DecideReminderActivationForOwner(
	ownerIdentity, actor string,
	activationRequestID uuid.UUID,
	request ReminderActivationDecisionRequest,
) (*ReminderActivationDecisionResult, error) {
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	actor = strings.TrimSpace(actor)
	request.Reason = strings.TrimSpace(request.Reason)
	request.ExpectedActivationRequestDigest = strings.TrimSpace(request.ExpectedActivationRequestDigest)
	request.ExpectedPreviousDecisionID = strings.TrimSpace(request.ExpectedPreviousDecisionID)
	var expectedPreviousDecisionID *uuid.UUID
	if request.ExpectedPreviousDecisionID != "" {
		parsed, parseErr := uuid.Parse(request.ExpectedPreviousDecisionID)
		if parseErr != nil {
			return nil, fmt.Errorf("expected previous reminder decision is invalid")
		}
		expectedPreviousDecisionID = &parsed
	}
	confirmation, ok := reminderActivationDecisionConfirmation(request.Decision)
	if ownerIdentity == "" || actor != ownerIdentity || activationRequestID == uuid.Nil || !ok ||
		request.Confirmation != confirmation || utf8.RuneCountInString(request.Reason) < 1 ||
		utf8.RuneCountInString(request.Reason) > 2000 || !validReminderDigest(request.ExpectedActivationRequestDigest) {
		return nil, fmt.Errorf("valid owner reminder decision evidence is required")
	}
	repository, ok := s.repo.(reminderActivationRepository)
	if !ok {
		return nil, fmt.Errorf("durable reminder activation storage is unavailable")
	}
	activation, latest, err := repository.LoadReminderActivationRequestForOwner(ownerIdentity, activationRequestID)
	if err != nil {
		return nil, err
	}
	if activation == nil || activation.RecordDigest != request.ExpectedActivationRequestDigest {
		return nil, fmt.Errorf("reminder activation request changed or is unavailable")
	}
	if err := validateReminderActivationRequest(activation); err != nil {
		return nil, err
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	if latest != nil && !now.After(latest.DecidedAt) {
		now = latest.DecidedAt.Add(time.Microsecond)
	}
	// Revocation withdraws an existing grant; its preparation window and live
	// source must not prevent the owner from cancelling a scheduled reminder.
	if request.Decision != ReminderActivationDecisionRevoked {
		if now.After(activation.ExpiresAt) {
			return nil, fmt.Errorf("reminder activation request expired; prepare a fresh request")
		}
		source, err := repository.LoadReminderActivationSourceForOwner(ownerIdentity, activation.ChecklistItemID)
		if err != nil {
			return nil, err
		}
		if source == nil {
			return nil, fmt.Errorf("reminder is no longer current")
		}
		currentDigest, err := reminderEvidenceDigest(*source)
		if err != nil || currentDigest != activation.ReminderDigest {
			return nil, fmt.Errorf("reminder changed; prepare a fresh activation request")
		}
	}
	decision := &models.WorkflowReminderActivationDecision{
		ID: uuid.New(), ActivationRequestID: activation.ID, OwnerIdentity: ownerIdentity,
		Decision: request.Decision, Reason: request.Reason, Actor: actor,
		Confirmation: request.Confirmation, ActivationRequestDigest: activation.RecordDigest,
		Authority: ReminderActivationDecisionAuthority, DecidedAt: now,
	}
	decision.PreviousDecisionID = expectedPreviousDecisionID
	if decision.Decision == ReminderActivationDecisionApproved {
		expires := now.Add(10 * time.Minute)
		decision.ExpiresAt = &expires
	}
	decision.RequestDigest, err = digestReminderActivationPayload(struct {
		ActivationRequestID, ActivationDigest, Decision, Reason, Confirmation, PreviousDecisionID string
	}{
		activation.ID.String(), activation.RecordDigest, request.Decision, request.Reason,
		request.Confirmation, request.ExpectedPreviousDecisionID,
	})
	if err != nil {
		return nil, err
	}
	if decision.Decision == ReminderActivationDecisionRevoked {
		if latest == nil {
			return nil, fmt.Errorf("only the latest approved reminder preparation can be revoked")
		}
		if err := validateReminderActivationDecision(activation, latest); err != nil {
			return nil, err
		}
		if latest.Decision == ReminderActivationDecisionRevoked && latest.RequestDigest == decision.RequestDigest {
			return &ReminderActivationDecisionResult{
				Decision: *latest, Replayed: true,
				Authority: ReminderActivationDecisionAuthority, CanExecute: false,
			}, nil
		}
		if latest.Decision != ReminderActivationDecisionApproved {
			return nil, fmt.Errorf("only the latest approved reminder preparation can be revoked")
		}
		if expectedPreviousDecisionID == nil || *expectedPreviousDecisionID != latest.ID {
			return nil, fmt.Errorf("reminder activation decision chain changed; retry from current history")
		}
	}
	decision.RecordDigest, err = digestReminderActivationDecision(decision)
	if err != nil {
		return nil, err
	}
	stored, created, err := repository.SaveReminderActivationDecision(decision)
	if err != nil {
		return nil, err
	}
	if err := validateReminderActivationDecision(activation, stored); err != nil {
		return nil, err
	}
	return &ReminderActivationDecisionResult{
		Decision: *stored, Replayed: !created,
		Authority: ReminderActivationDecisionAuthority, CanExecute: false,
	}, nil
}

func (s *service) ReminderActivationDecisionHistoryForOwner(ownerIdentity string, requestID uuid.UUID, limit int) (*ReminderActivationDecisionHistory, error) {
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	if ownerIdentity == "" || requestID == uuid.Nil || limit < 1 || limit > 100 {
		return nil, fmt.Errorf("valid owner, activation request, and history limit are required")
	}
	repository, ok := s.repo.(reminderActivationRepository)
	if !ok {
		return nil, fmt.Errorf("durable reminder activation storage is unavailable")
	}
	activation, _, err := repository.LoadReminderActivationRequestForOwner(ownerIdentity, requestID)
	if err != nil {
		return nil, err
	}
	if activation == nil {
		return nil, fmt.Errorf("reminder activation request is unavailable to this owner")
	}
	records, err := repository.ListReminderActivationDecisionsForOwner(ownerIdentity, requestID, limit)
	if err != nil {
		return nil, err
	}
	for index := range records {
		if err := validateReminderActivationDecision(activation, &records[index]); err != nil {
			return nil, err
		}
		if index+1 < len(records) &&
			(records[index].PreviousDecisionID == nil || *records[index].PreviousDecisionID != records[index+1].ID) {
			return nil, fmt.Errorf("reminder activation decision history chain verification failed")
		}
	}
	return &ReminderActivationDecisionHistory{
		Decisions: records, Authority: ReminderActivationDecisionAuthority, CanExecute: false,
	}, nil
}

func reminderActivationDecisionConfirmation(decision string) (string, bool) {
	switch decision {
	case ReminderActivationDecisionApproved:
		return ReminderActivationApproveConfirmation, true
	case ReminderActivationDecisionRejected:
		return ReminderActivationRejectConfirmation, true
	case ReminderActivationDecisionClarification:
		return ReminderActivationClarifyConfirmation, true
	case ReminderActivationDecisionRevoked:
		return ReminderActivationRevokeConfirmation, true
	default:
		return "", false
	}
}

func reminderEvidenceDigest(candidate WorkflowReminderCandidate) (string, error) {
	due := ""
	if candidate.Reminder.DueAt != nil {
		due = candidate.Reminder.DueAt.UTC().Format(time.RFC3339Nano)
	}
	reminder := ""
	if candidate.Reminder.ReminderAt != nil {
		reminder = candidate.Reminder.ReminderAt.UTC().Format(time.RFC3339Nano)
	}
	return digestReminderActivationPayload(struct {
		WorkflowID, ChecklistItemID, Title, Label, ProjectKey, WorkflowState, RiskLevel string
		RequiresApproval, ChecklistRequiresApproval                                     bool
		ChecklistStatus, ReminderAt, DueAt, SourceURI, SourceLabel                      string
	}{
		candidate.Workflow.ID.String(), candidate.Reminder.ID.String(), candidate.Workflow.Title,
		candidate.Reminder.Label, candidate.Workflow.ProjectKey, candidate.Workflow.CurrentState,
		candidate.Workflow.RiskLevel, candidate.Workflow.RequiresApproval,
		candidate.Reminder.RequiresApproval, candidate.Reminder.Status, reminder, due,
		candidate.Workflow.SourceURI, candidate.Workflow.SourceLabel,
	})
}

func digestReminderActivationRequest(value *models.WorkflowReminderActivationRequest) (string, error) {
	due := ""
	if value.DueAt != nil {
		due = value.DueAt.UTC().Format(time.RFC3339Nano)
	}
	return digestReminderActivationPayload(struct {
		ID, Owner, WorkflowID, ChecklistItemID, ActivationKind, WorkflowState, ChecklistStatus string
		ReminderAt, DueAt, ReminderDigest, IdempotencyKey, Authority, Actor, Confirmation      string
		RequestDigest, RequestedAt, ExpiresAt                                                  string
	}{
		value.ID.String(), value.OwnerIdentity, value.WorkflowID.String(), value.ChecklistItemID.String(),
		value.ActivationKind, value.WorkflowState, value.ChecklistStatus,
		value.ReminderAt.UTC().Format(time.RFC3339Nano), due, value.ReminderDigest,
		value.IdempotencyKey, value.Authority, value.Actor, value.Confirmation, value.RequestDigest,
		value.RequestedAt.UTC().Format(time.RFC3339Nano), value.ExpiresAt.UTC().Format(time.RFC3339Nano),
	})
}

func digestReminderActivationDecision(value *models.WorkflowReminderActivationDecision) (string, error) {
	previous := ""
	if value.PreviousDecisionID != nil {
		previous = value.PreviousDecisionID.String()
	}
	expires := ""
	if value.ExpiresAt != nil {
		expires = value.ExpiresAt.UTC().Format(time.RFC3339Nano)
	}
	return digestReminderActivationPayload(struct {
		ID, ActivationRequestID, Owner, Decision, Reason, Actor, Confirmation string
		ActivationRequestDigest, PreviousDecisionID, Authority, RequestDigest string
		DecidedAt, ExpiresAt                                                  string
	}{
		value.ID.String(), value.ActivationRequestID.String(), value.OwnerIdentity, value.Decision,
		value.Reason, value.Actor, value.Confirmation, value.ActivationRequestDigest, previous,
		value.Authority, value.RequestDigest, value.DecidedAt.UTC().Format(time.RFC3339Nano), expires,
	})
}

func digestReminderActivationPayload(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func validateReminderActivationRequest(value *models.WorkflowReminderActivationRequest) error {
	if value == nil || value.ID == uuid.Nil || strings.TrimSpace(value.OwnerIdentity) == "" ||
		value.Actor != value.OwnerIdentity || value.WorkflowID == uuid.Nil || value.ChecklistItemID == uuid.Nil ||
		value.ActivationKind != ReminderActivationKindInternal || value.ChecklistStatus != "open" ||
		value.Authority != ReminderActivationRequestAuthority || value.Confirmation != ReminderActivationPrepareConfirmation ||
		value.ReminderAt.IsZero() || value.RequestedAt.IsZero() || !value.ExpiresAt.After(value.RequestedAt) ||
		value.ExpiresAt.After(value.RequestedAt.Add(30*time.Minute)) || !validReminderDigest(value.ReminderDigest) ||
		!validReminderDigest(value.RequestDigest) || !validReminderDigest(value.RecordDigest) ||
		!reminderActivationIdempotencyPattern.MatchString(value.IdempotencyKey) {
		return fmt.Errorf("reminder activation request contains invalid immutable evidence")
	}
	expected, err := digestReminderActivationRequest(value)
	if err != nil || expected != value.RecordDigest {
		return fmt.Errorf("reminder activation request digest verification failed")
	}
	return nil
}

func validateReminderActivationDecision(
	activation *models.WorkflowReminderActivationRequest,
	value *models.WorkflowReminderActivationDecision,
) error {
	confirmation, ok := reminderActivationDecisionConfirmation(value.Decision)
	if activation == nil || value == nil || value.ID == uuid.Nil || value.ActivationRequestID != activation.ID ||
		value.OwnerIdentity != activation.OwnerIdentity || value.Actor != value.OwnerIdentity || !ok ||
		value.Confirmation != confirmation || value.ActivationRequestDigest != activation.RecordDigest ||
		value.Authority != ReminderActivationDecisionAuthority || strings.TrimSpace(value.Reason) != value.Reason ||
		utf8.RuneCountInString(value.Reason) < 1 || utf8.RuneCountInString(value.Reason) > 2000 ||
		value.DecidedAt.IsZero() || !validReminderDigest(value.RequestDigest) || !validReminderDigest(value.RecordDigest) {
		return fmt.Errorf("reminder activation decision contains invalid immutable evidence")
	}
	if value.Decision == ReminderActivationDecisionApproved {
		if value.ExpiresAt == nil || !value.ExpiresAt.After(value.DecidedAt) ||
			value.ExpiresAt.After(value.DecidedAt.Add(15*time.Minute)) {
			return fmt.Errorf("approved reminder activation decision has invalid bounded expiry")
		}
	} else if value.ExpiresAt != nil {
		return fmt.Errorf("non-approval reminder activation decision cannot carry expiry")
	}
	expected, err := digestReminderActivationDecision(value)
	if err != nil || expected != value.RecordDigest {
		return fmt.Errorf("reminder activation decision digest verification failed")
	}
	return nil
}

func validReminderDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil && value == strings.ToLower(value)
}

func firstReminderActivationError(err error, fallback string) error {
	if err != nil {
		return err
	}
	return fmt.Errorf("%s", fallback)
}
