package workflow

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/models"

	"github.com/google/uuid"
)

type reminderDeliveryFakeRepo struct {
	*reminderActivationFakeRepo
	authorizations map[uuid.UUID]models.WorkflowReminderDeliveryAuthorization
	attempts       map[uuid.UUID][]models.WorkflowReminderDeliveryAttempt
}

func newReminderDeliveryFakeRepo() *reminderDeliveryFakeRepo {
	return &reminderDeliveryFakeRepo{reminderActivationFakeRepo: newReminderActivationFakeRepo(), authorizations: map[uuid.UUID]models.WorkflowReminderDeliveryAuthorization{}, attempts: map[uuid.UUID][]models.WorkflowReminderDeliveryAttempt{}}
}

func (r *reminderDeliveryFakeRepo) FindOrCreateReminderDeliveryAuthorization(wanted *models.WorkflowReminderDeliveryAuthorization) (*models.WorkflowReminderDeliveryAuthorization, bool, error) {
	for _, existing := range r.authorizations {
		if existing.OwnerIdentity == wanted.OwnerIdentity && existing.ActivationRequestID == wanted.ActivationRequestID &&
			existing.ActivationDecisionID == wanted.ActivationDecisionID && existing.Channel == wanted.Channel &&
			(existing.IdempotencyKey != wanted.IdempotencyKey || existing.RequestDigest != wanted.RequestDigest) {
			return nil, false, fmt.Errorf("approved reminder preparation already has a different delivery authorization")
		}
		if existing.OwnerIdentity == wanted.OwnerIdentity && existing.IdempotencyKey == wanted.IdempotencyKey {
			if existing.RequestDigest != wanted.RequestDigest || existing.ActivationRequestID != wanted.ActivationRequestID ||
				existing.ActivationDecisionID != wanted.ActivationDecisionID || existing.ReminderDigest != wanted.ReminderDigest ||
				existing.Channel != wanted.Channel {
				return nil, false, fmt.Errorf("reminder delivery idempotency key is bound to different evidence")
			}
			copy := existing
			return &copy, false, nil
		}
	}
	r.authorizations[wanted.ID] = *wanted
	copy := *wanted
	return &copy, true, nil
}

func (r *reminderDeliveryFakeRepo) FindDueReminderDeliveryAuthorizations(owner string, now time.Time, limit, maxAttempts int) ([]reminderDeliveryCandidate, error) {
	result := []reminderDeliveryCandidate{}
	for _, authorization := range r.authorizations {
		if owner != "" && authorization.OwnerIdentity != owner {
			continue
		}
		values := r.attempts[authorization.ID]
		terminal := false
		for _, attempt := range values {
			if attempt.Status == ReminderDeliveryStatusDelivered || attempt.Status == ReminderDeliveryStatusSuppressed || attempt.Status == ReminderDeliveryStatusDeadLettered || attempt.Status == ReminderDeliveryStatusExpired {
				terminal = true
			}
		}
		if !terminal && len(values) < maxAttempts && !authorization.ReminderAt.After(now) {
			result = append(result, reminderDeliveryCandidate{Authorization: authorization, AttemptCount: len(values)})
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Authorization.ReminderAt.Before(result[j].Authorization.ReminderAt)
	})
	if len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

func (r *reminderDeliveryFakeRepo) SaveReminderDeliveryAttempt(wanted *models.WorkflowReminderDeliveryAttempt) (*models.WorkflowReminderDeliveryAttempt, bool, error) {
	for _, existing := range r.attempts[wanted.AuthorizationID] {
		if existing.AttemptNumber == wanted.AttemptNumber {
			if existing.Status != wanted.Status || existing.Reason != wanted.Reason ||
				existing.ReminderDigest != wanted.ReminderDigest || existing.AuthorizationDigest != wanted.AuthorizationDigest ||
				existing.Authority != wanted.Authority {
				return nil, false, fmt.Errorf("reminder delivery attempt number is bound to different evidence")
			}
			copy := existing
			return &copy, false, nil
		}
	}
	r.attempts[wanted.AuthorizationID] = append(r.attempts[wanted.AuthorizationID], *wanted)
	copy := *wanted
	return &copy, true, nil
}

func (r *reminderDeliveryFakeRepo) ListReminderDeliveryAuthorizationsForOwner(owner string, limit int) ([]models.WorkflowReminderDeliveryAuthorization, error) {
	result := []models.WorkflowReminderDeliveryAuthorization{}
	for _, item := range r.authorizations {
		if item.OwnerIdentity == owner {
			result = append(result, item)
		}
	}
	if len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}
func (r *reminderDeliveryFakeRepo) ListReminderDeliveryAttemptsForOwner(owner string, limit int) ([]models.WorkflowReminderDeliveryAttempt, error) {
	result := []models.WorkflowReminderDeliveryAttempt{}
	for _, values := range r.attempts {
		for _, item := range values {
			if item.OwnerIdentity == owner {
				result = append(result, item)
			}
		}
	}
	if len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

type reminderDeliverySinkSpy struct {
	deliveries []ReminderDeliveryEnvelope
	err        error
}

func (s *reminderDeliverySinkSpy) DeliverInternalReminder(_ context.Context, envelope ReminderDeliveryEnvelope) error {
	s.deliveries = append(s.deliveries, envelope)
	return s.err
}

func TestReminderDeliveryRequiresSeparateExactAuthorizationAndWritesReceipt(t *testing.T) {
	repo := newReminderDeliveryFakeRepo()
	sink := &reminderDeliverySinkSpy{}
	workflowID, checklistID := uuid.New(), uuid.New()
	reminderAt := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)
	repo.items[workflowID] = &models.WorkflowItem{ID: workflowID, OwnerIdentity: "alice", Title: "Review appointment", CurrentState: StateReady, RiskLevel: "low"}
	repo.checklist[workflowID] = []models.WorkflowChecklistItem{{ID: checklistID, WorkflowID: workflowID, Label: "Review before appointment", Status: "open", ReminderAt: &reminderAt}}
	candidate := WorkflowReminderCandidate{Workflow: *repo.items[workflowID], Reminder: repo.checklist[workflowID][0]}
	digest, _ := reminderEvidenceDigest(candidate)
	base := NewService(repo)
	configured, err := WithReminderDeliverySink(base, sink)
	if err != nil {
		t.Fatal(err)
	}
	activationService := configured.(ReminderActivationService)
	deliveryService := configured.(ReminderDeliveryService)
	prepared, err := activationService.PrepareReminderActivationForOwner("alice", "alice", checklistID, ReminderActivationPrepareRequest{ExpectedReminderDigest: digest, IdempotencyKey: "delivery:test", ActivationKind: ReminderActivationKindInternal, Confirmation: ReminderActivationPrepareConfirmation})
	if err != nil {
		t.Fatal(err)
	}
	approved, err := activationService.DecideReminderActivationForOwner("alice", "alice", prepared.Request.ID, ReminderActivationDecisionRequest{Decision: ReminderActivationDecisionApproved, Reason: "Deliver one internal reminder.", Confirmation: ReminderActivationApproveConfirmation, ExpectedActivationRequestDigest: prepared.Request.RecordDigest})
	if err != nil {
		t.Fatal(err)
	}
	request := ReminderDeliveryAuthorizeRequest{ExpectedActivationRequestDigest: prepared.Request.RecordDigest, ExpectedActivationDecisionDigest: approved.Decision.RecordDigest, ExpectedReminderDigest: digest, IdempotencyKey: "delivery:authorize:test", Channel: ReminderDeliveryChannelInApp, Confirmation: ReminderDeliveryAuthorizeConfirmation}
	authorized, err := deliveryService.AuthorizeReminderDeliveryForOwner("alice", "alice", prepared.Request.ID, request)
	if err != nil {
		t.Fatal(err)
	}
	if !authorized.DeliveryAuthorized || authorized.CanExecute || authorized.Authority != ReminderDeliveryAuthorizationAuthority {
		t.Fatalf("authorization=%#v", authorized)
	}
	replayed, err := deliveryService.AuthorizeReminderDeliveryForOwner("alice", "alice", prepared.Request.ID, request)
	if err != nil || !replayed.Replayed || replayed.Authorization.ID != authorized.Authorization.ID {
		t.Fatalf("replay=%#v %v", replayed, err)
	}
	changedKey := request
	changedKey.IdempotencyKey = "delivery:authorize:changed"
	if _, err = deliveryService.AuthorizeReminderDeliveryForOwner("alice", "alice", prepared.Request.ID, changedKey); err == nil {
		t.Fatal("one approved preparation must not authorize a second delivery")
	}
	run, err := deliveryService.RunDueReminderDeliveriesForOwner("alice", RunDueRequest{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if run.Delivered != 1 || len(sink.deliveries) != 1 {
		t.Fatalf("run=%#v deliveries=%d", run, len(sink.deliveries))
	}
	second, err := deliveryService.RunDueReminderDeliveriesForOwner("alice", RunDueRequest{Limit: 10})
	if err != nil || second.Checked != 0 || len(sink.deliveries) != 1 {
		t.Fatalf("second=%#v deliveries=%d err=%v", second, len(sink.deliveries), err)
	}
	history, err := deliveryService.ReminderDeliveryHistoryForOwner("alice", 10)
	if err != nil || history.CanExecute || len(history.Attempts) != 1 || history.Attempts[0].Status != ReminderDeliveryStatusDelivered {
		t.Fatalf("history=%#v err=%v", history, err)
	}
}

func TestReminderDeliveryAuthorizationCoversSupportedHorizon(t *testing.T) {
	for _, test := range []struct {
		name          string
		offset        time.Duration
		wantPrepareOK bool
	}{
		{name: "at_maximum_supported_horizon", offset: workflowReminderMaxHorizon, wantPrepareOK: true},
		{name: "beyond_maximum_supported_horizon", offset: workflowReminderMaxHorizon + time.Minute},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo := newReminderDeliveryFakeRepo()
			sink := &reminderDeliverySinkSpy{}
			workflowID, checklistID := uuid.New(), uuid.New()
			reminderAt := time.Now().UTC().Add(test.offset).Truncate(time.Second)
			repo.items[workflowID] = &models.WorkflowItem{
				ID: workflowID, OwnerIdentity: "alice", Title: "Future reminder",
				CurrentState: StateReady, RiskLevel: "low",
			}
			repo.checklist[workflowID] = []models.WorkflowChecklistItem{{
				ID: checklistID, WorkflowID: workflowID, Label: "Review at scheduled time",
				Status: "open", ReminderAt: &reminderAt,
			}}
			candidate := WorkflowReminderCandidate{Workflow: *repo.items[workflowID], Reminder: repo.checklist[workflowID][0]}
			digest, err := reminderEvidenceDigest(candidate)
			if err != nil {
				t.Fatal(err)
			}
			configured, err := WithReminderDeliverySink(NewService(repo), sink)
			if err != nil {
				t.Fatal(err)
			}
			activationService := configured.(ReminderActivationService)
			prepared, err := activationService.PrepareReminderActivationForOwner("alice", "alice", checklistID, ReminderActivationPrepareRequest{
				ExpectedReminderDigest: digest, IdempotencyKey: "delivery:horizon:" + test.name,
				ActivationKind: ReminderActivationKindInternal, Confirmation: ReminderActivationPrepareConfirmation,
			})
			if !test.wantPrepareOK {
				if err == nil || prepared != nil {
					t.Fatalf("out-of-horizon reminder preparation = %#v, %v; want rejection", prepared, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("prepare reminder at supported horizon: %v", err)
			}
			approved, err := activationService.DecideReminderActivationForOwner("alice", "alice", prepared.Request.ID, ReminderActivationDecisionRequest{
				Decision: ReminderActivationDecisionApproved, Reason: "Deliver this scheduled internal reminder.",
				Confirmation:                    ReminderActivationApproveConfirmation,
				ExpectedActivationRequestDigest: prepared.Request.RecordDigest,
			})
			if err != nil {
				t.Fatalf("approve reminder preparation: %v", err)
			}
			deliveryService := configured.(ReminderDeliveryService)
			authorized, err := deliveryService.AuthorizeReminderDeliveryForOwner("alice", "alice", prepared.Request.ID, ReminderDeliveryAuthorizeRequest{
				ExpectedActivationRequestDigest:  prepared.Request.RecordDigest,
				ExpectedActivationDecisionDigest: approved.Decision.RecordDigest,
				ExpectedReminderDigest:           digest, IdempotencyKey: "delivery:horizon:authorize:" + test.name,
				Channel: ReminderDeliveryChannelInApp, Confirmation: ReminderDeliveryAuthorizeConfirmation,
			})
			if err != nil {
				t.Fatalf("authorize reminder at supported horizon: %v", err)
			}
			minimumExpiry := reminderAt.Add(workflowReminderDeliveryGrace)
			if authorized.Authorization.ExpiresAt.Before(minimumExpiry) {
				t.Fatalf("authorization expires at %s before the reminder grace ends at %s", authorized.Authorization.ExpiresAt, minimumExpiry)
			}
		})
	}
}

func TestReminderDeliverySuppressesRevokedAuthorityAndNeverCallsSink(t *testing.T) {
	repo := newReminderDeliveryFakeRepo()
	sink := &reminderDeliverySinkSpy{}
	workflowID, checklistID := uuid.New(), uuid.New()
	reminderAt := time.Now().UTC().Add(-time.Minute)
	repo.items[workflowID] = &models.WorkflowItem{ID: workflowID, OwnerIdentity: "alice", Title: "Current", CurrentState: StateReady, RiskLevel: "low"}
	repo.checklist[workflowID] = []models.WorkflowChecklistItem{{ID: checklistID, WorkflowID: workflowID, Label: "Current", Status: "open", ReminderAt: &reminderAt}}
	digest, _ := reminderEvidenceDigest(WorkflowReminderCandidate{Workflow: *repo.items[workflowID], Reminder: repo.checklist[workflowID][0]})
	configured, _ := WithReminderDeliverySink(NewService(repo), sink)
	activation := configured.(ReminderActivationService)
	delivery := configured.(ReminderDeliveryService)
	prepared, _ := activation.PrepareReminderActivationForOwner("alice", "alice", checklistID, ReminderActivationPrepareRequest{ExpectedReminderDigest: digest, IdempotencyKey: "delivery:revoke", ActivationKind: ReminderActivationKindInternal, Confirmation: ReminderActivationPrepareConfirmation})
	approved, _ := activation.DecideReminderActivationForOwner("alice", "alice", prepared.Request.ID, ReminderActivationDecisionRequest{Decision: ReminderActivationDecisionApproved, Reason: "Review.", Confirmation: ReminderActivationApproveConfirmation, ExpectedActivationRequestDigest: prepared.Request.RecordDigest})
	_, err := delivery.AuthorizeReminderDeliveryForOwner("alice", "alice", prepared.Request.ID, ReminderDeliveryAuthorizeRequest{ExpectedActivationRequestDigest: prepared.Request.RecordDigest, ExpectedActivationDecisionDigest: approved.Decision.RecordDigest, ExpectedReminderDigest: digest, IdempotencyKey: "delivery:revoke:authorization", Channel: ReminderDeliveryChannelInApp, Confirmation: ReminderDeliveryAuthorizeConfirmation})
	if err != nil {
		t.Fatal(err)
	}
	_, err = activation.DecideReminderActivationForOwner("alice", "alice", prepared.Request.ID, ReminderActivationDecisionRequest{Decision: ReminderActivationDecisionRevoked, Reason: "No longer wanted.", Confirmation: ReminderActivationRevokeConfirmation, ExpectedActivationRequestDigest: prepared.Request.RecordDigest, ExpectedPreviousDecisionID: approved.Decision.ID.String()})
	if err != nil {
		t.Fatal(err)
	}
	run, err := delivery.RunDueReminderDeliveriesForOwner("alice", RunDueRequest{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if run.Suppressed != 1 || len(sink.deliveries) != 0 {
		t.Fatalf("run=%#v deliveries=%d", run, len(sink.deliveries))
	}
}

func TestReminderDeliveryDeadLettersAfterThreeFailedAttempts(t *testing.T) {
	repo := newReminderDeliveryFakeRepo()
	sink := &reminderDeliverySinkSpy{err: errors.New("sink unavailable")}
	workflowID, checklistID := uuid.New(), uuid.New()
	reminderAt := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)
	repo.items[workflowID] = &models.WorkflowItem{ID: workflowID, OwnerIdentity: "alice", Title: "Retry reminder", CurrentState: StateReady, RiskLevel: "low"}
	repo.checklist[workflowID] = []models.WorkflowChecklistItem{{ID: checklistID, WorkflowID: workflowID, Label: "Retry internally", Status: "open", ReminderAt: &reminderAt}}
	digest, _ := reminderEvidenceDigest(WorkflowReminderCandidate{Workflow: *repo.items[workflowID], Reminder: repo.checklist[workflowID][0]})
	configured, _ := WithReminderDeliverySink(NewService(repo), sink)
	activation := configured.(ReminderActivationService)
	delivery := configured.(ReminderDeliveryService)
	prepared, _ := activation.PrepareReminderActivationForOwner("alice", "alice", checklistID, ReminderActivationPrepareRequest{ExpectedReminderDigest: digest, IdempotencyKey: "delivery:dead-letter", ActivationKind: ReminderActivationKindInternal, Confirmation: ReminderActivationPrepareConfirmation})
	approved, _ := activation.DecideReminderActivationForOwner("alice", "alice", prepared.Request.ID, ReminderActivationDecisionRequest{Decision: ReminderActivationDecisionApproved, Reason: "Deliver internally.", Confirmation: ReminderActivationApproveConfirmation, ExpectedActivationRequestDigest: prepared.Request.RecordDigest})
	_, err := delivery.AuthorizeReminderDeliveryForOwner("alice", "alice", prepared.Request.ID, ReminderDeliveryAuthorizeRequest{ExpectedActivationRequestDigest: prepared.Request.RecordDigest, ExpectedActivationDecisionDigest: approved.Decision.RecordDigest, ExpectedReminderDigest: digest, IdempotencyKey: "delivery:dead-letter:authorization", Channel: ReminderDeliveryChannelInApp, Confirmation: ReminderDeliveryAuthorizeConfirmation})
	if err != nil {
		t.Fatal(err)
	}
	for attempt := 1; attempt <= ReminderDeliveryMaxAttempts; attempt++ {
		run, runErr := delivery.RunDueReminderDeliveriesForOwner("alice", RunDueRequest{Limit: 10})
		if runErr != nil {
			t.Fatal(runErr)
		}
		if attempt < ReminderDeliveryMaxAttempts && (run.Retried != 1 || run.DeadLettered != 0) {
			t.Fatalf("attempt %d summary=%#v", attempt, run)
		}
		if attempt == ReminderDeliveryMaxAttempts && (run.Retried != 0 || run.DeadLettered != 1) {
			t.Fatalf("terminal attempt summary=%#v", run)
		}
	}
	final, err := delivery.RunDueReminderDeliveriesForOwner("alice", RunDueRequest{Limit: 10})
	if err != nil || final.Checked != 0 || len(sink.deliveries) != ReminderDeliveryMaxAttempts {
		t.Fatalf("final=%#v deliveries=%d err=%v", final, len(sink.deliveries), err)
	}
}

func TestReminderDeliveryRecordsExpiredAuthorizationWithoutCallingSink(t *testing.T) {
	repo := newReminderDeliveryFakeRepo()
	sink := &reminderDeliverySinkSpy{}
	configured, err := WithReminderDeliverySink(NewService(repo), sink)
	if err != nil {
		t.Fatal(err)
	}
	delivery := configured.(ReminderDeliveryService)
	now := time.Now().UTC().Truncate(time.Microsecond)
	authorization := models.WorkflowReminderDeliveryAuthorization{
		ID: uuid.New(), OwnerIdentity: "alice", ReminderAt: now.Add(-time.Minute), ExpiresAt: now.Add(-time.Second),
		ReminderDigest: strings.Repeat("a", 64), RecordDigest: strings.Repeat("b", 64),
	}
	repo.authorizations[authorization.ID] = authorization

	run, err := delivery.RunDueReminderDeliveriesForOwner("alice", RunDueRequest{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if run.Checked != 1 || run.Expired != 1 || run.Delivered != 0 || len(sink.deliveries) != 0 {
		t.Fatalf("expired reminder pass = %#v; sink calls=%d", run, len(sink.deliveries))
	}
	history, err := delivery.ReminderDeliveryHistoryForOwner("alice", 10)
	if err != nil || len(history.Attempts) != 1 || history.Attempts[0].Status != ReminderDeliveryStatusExpired {
		t.Fatalf("expired reminder history = %#v, err=%v", history, err)
	}
	second, err := delivery.RunDueReminderDeliveriesForOwner("alice", RunDueRequest{Limit: 10})
	if err != nil || second.Checked != 0 {
		t.Fatalf("expired authorization was not terminal: %#v, err=%v", second, err)
	}
}
