package workflow

import (
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/models"

	"github.com/google/uuid"
)

type historicalReminderFixture struct {
	source        WorkflowReminderCandidate
	activation    models.WorkflowReminderActivationRequest
	approval      models.WorkflowReminderActivationDecision
	authorization models.WorkflowReminderDeliveryAuthorization
}

func newHistoricalReminderFixture(t *testing.T, requestedAt time.Time) historicalReminderFixture {
	t.Helper()
	return newHistoricalReminderFixtureForOwner(t, "alice", requestedAt)
}

func newHistoricalReminderFixtureForOwner(t *testing.T, owner string, requestedAt time.Time) historicalReminderFixture {
	t.Helper()
	reminderAt := time.Now().UTC().Add(-time.Minute).Truncate(time.Microsecond)
	f := historicalReminderFixture{source: WorkflowReminderCandidate{
		Workflow: models.WorkflowItem{ID: uuid.New(), OwnerIdentity: owner, Title: "Review appointment", CurrentState: StateReady, TaskType: "administrative", AutonomyLevel: "manual", RiskLevel: "low"},
		Reminder: models.WorkflowChecklistItem{ID: uuid.New(), Label: "Review appointment internally", Status: "open", ReminderAt: &reminderAt},
	}}
	f.source.Reminder.WorkflowID = f.source.Workflow.ID
	digest, err := reminderEvidenceDigest(f.source)
	if err != nil {
		t.Fatal(err)
	}
	f.activation = models.WorkflowReminderActivationRequest{
		ID: uuid.New(), OwnerIdentity: owner, WorkflowID: f.source.Workflow.ID, ChecklistItemID: f.source.Reminder.ID,
		ActivationKind: ReminderActivationKindInternal, WorkflowState: StateReady, ChecklistStatus: "open",
		ReminderAt: reminderAt, ReminderDigest: digest, IdempotencyKey: "historical:prepare:" + uuid.NewString(),
		Authority: ReminderActivationRequestAuthority, Actor: owner, Confirmation: ReminderActivationPrepareConfirmation,
		RequestDigest: strings.Repeat("a", 64), RequestedAt: requestedAt, ExpiresAt: requestedAt.Add(15 * time.Minute),
	}
	f.activation.RecordDigest, err = digestReminderActivationRequest(&f.activation)
	if err != nil {
		t.Fatal(err)
	}
	approvedAt := requestedAt.Add(time.Minute)
	approvalExpiry := approvedAt.Add(10 * time.Minute)
	f.approval = models.WorkflowReminderActivationDecision{
		ID: uuid.New(), ActivationRequestID: f.activation.ID, OwnerIdentity: owner, Decision: ReminderActivationDecisionApproved,
		Reason: "Owner reviewed the internal reminder.", Actor: owner, Confirmation: ReminderActivationApproveConfirmation,
		ActivationRequestDigest: f.activation.RecordDigest, Authority: ReminderActivationDecisionAuthority,
		RequestDigest: strings.Repeat("b", 64), DecidedAt: approvedAt, ExpiresAt: &approvalExpiry,
	}
	f.approval.RecordDigest, err = digestReminderActivationDecision(&f.approval)
	if err != nil {
		t.Fatal(err)
	}
	f.authorization = models.WorkflowReminderDeliveryAuthorization{
		ID: uuid.New(), ActivationRequestID: f.activation.ID, ActivationDecisionID: f.approval.ID, OwnerIdentity: owner,
		WorkflowID: f.source.Workflow.ID, ChecklistItemID: f.source.Reminder.ID, ReminderAt: reminderAt, ReminderDigest: digest,
		ActivationRequestDigest: f.activation.RecordDigest, ActivationDecisionDigest: f.approval.RecordDigest,
		Channel: ReminderDeliveryChannelInApp, IdempotencyKey: "historical:delivery:" + uuid.NewString(),
		Authority: ReminderDeliveryAuthorizationAuthority, Actor: owner, Confirmation: ReminderDeliveryAuthorizeConfirmation,
		RequestDigest: strings.Repeat("c", 64), AuthorizedAt: approvedAt.Add(time.Minute), ExpiresAt: reminderAt.Add(workflowReminderDeliveryGrace),
	}
	f.authorization.RecordDigest, err = digestReminderActivationPayload(&f.authorization)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateReminderActivationRequest(&f.activation); err != nil {
		t.Fatal(err)
	}
	if err := validateReminderActivationDecision(&f.activation, &f.approval); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f historicalReminderFixture) seed(repo *reminderDeliveryFakeRepo) {
	workflow := f.source.Workflow
	repo.items[workflow.ID] = &workflow
	repo.checklist[workflow.ID] = []models.WorkflowChecklistItem{f.source.Reminder}
	repo.requests[f.activation.ID] = f.activation
	repo.decisions[f.activation.ID] = []models.WorkflowReminderActivationDecision{f.approval}
	repo.authorizations[f.authorization.ID] = f.authorization
}

func (f historicalReminderFixture) revokeRequest() ReminderActivationDecisionRequest {
	return ReminderActivationDecisionRequest{
		Decision: ReminderActivationDecisionRevoked, Reason: "Cancel this internal reminder.", Confirmation: ReminderActivationRevokeConfirmation,
		ExpectedActivationRequestDigest: f.activation.RecordDigest, ExpectedPreviousDecisionID: f.approval.ID.String(),
	}
}

func TestReminderRevocationSurvivesPreparationExpiryAndSourceChanges(t *testing.T) {
	for _, test := range []struct {
		name    string
		expired bool
		mutate  func(*reminderDeliveryFakeRepo, historicalReminderFixture)
	}{
		{name: "expired preparation", expired: true},
		{name: "changed source", mutate: func(repo *reminderDeliveryFakeRepo, f historicalReminderFixture) {
			repo.checklist[f.source.Workflow.ID][0].Label = "Updated reminder"
		}},
		{name: "closed source", mutate: func(repo *reminderDeliveryFakeRepo, f historicalReminderFixture) {
			repo.items[f.source.Workflow.ID].CurrentState = StateCompleted
		}},
		{name: "expired and archived source", expired: true, mutate: func(repo *reminderDeliveryFakeRepo, f historicalReminderFixture) {
			repo.items[f.source.Workflow.ID].Archived = true
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			requestedAt := time.Now().UTC().Add(-3 * time.Minute)
			if test.expired {
				requestedAt = requestedAt.Add(-time.Hour)
			}
			f := newHistoricalReminderFixture(t, requestedAt)
			repo := newReminderDeliveryFakeRepo()
			f.seed(repo)
			if test.mutate != nil {
				test.mutate(repo, f)
			}
			sink := &reminderDeliverySinkSpy{}
			configured, err := WithReminderDeliverySink(NewService(repo), sink)
			if err != nil {
				t.Fatal(err)
			}
			result, err := configured.(ReminderActivationService).DecideReminderActivationForOwner("alice", "alice", f.activation.ID, f.revokeRequest())
			if err != nil || result == nil {
				t.Fatalf("owner revocation must remain available: result=%#v err=%v", result, err)
			}
			if result.CanExecute || result.Decision.Decision != ReminderActivationDecisionRevoked ||
				result.Decision.PreviousDecisionID == nil || *result.Decision.PreviousDecisionID != f.approval.ID {
				t.Fatalf("revocation lost its approval boundary: %#v", result)
			}
			run, err := configured.(ReminderDeliveryService).RunDueReminderDeliveriesForOwner("alice", RunDueRequest{Limit: 10})
			if err != nil || run.Suppressed != 1 || len(sink.deliveries) != 0 {
				t.Fatalf("revoked reminder reached delivery: run=%#v sink=%d err=%v", run, len(sink.deliveries), err)
			}
			if len(repo.decisions[f.activation.ID]) != 2 || repo.decisions[f.activation.ID][0].RecordDigest != f.approval.RecordDigest {
				t.Fatal("revocation must append evidence without replacing the approval")
			}
			replayed, err := configured.(ReminderActivationService).DecideReminderActivationForOwner("alice", "alice", f.activation.ID, f.revokeRequest())
			if err != nil || replayed == nil || !replayed.Replayed || replayed.Decision.ID != result.Decision.ID || len(repo.decisions[f.activation.ID]) != 2 {
				t.Fatalf("revocation retry must reuse its receipt: result=%#v err=%v", replayed, err)
			}
		})
	}
}

func TestReminderRevocationStillRequiresOwnerAndExactApprovalEvidence(t *testing.T) {
	for _, test := range []struct {
		name   string
		owner  string
		actor  string
		mutate func(*ReminderActivationDecisionRequest)
	}{
		{name: "foreign owner", owner: "bob", actor: "bob"},
		{name: "foreign actor", owner: "alice", actor: "bob"},
		{name: "wrong digest", owner: "alice", actor: "alice", mutate: func(request *ReminderActivationDecisionRequest) {
			request.ExpectedActivationRequestDigest = strings.Repeat("d", 64)
		}},
		{name: "wrong confirmation", owner: "alice", actor: "alice", mutate: func(request *ReminderActivationDecisionRequest) {
			request.Confirmation = "CANCEL"
		}},
		{name: "stale approval", owner: "alice", actor: "alice", mutate: func(request *ReminderActivationDecisionRequest) {
			request.ExpectedPreviousDecisionID = uuid.NewString()
		}},
		{name: "missing approval binding", owner: "alice", actor: "alice", mutate: func(request *ReminderActivationDecisionRequest) {
			request.ExpectedPreviousDecisionID = ""
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newHistoricalReminderFixture(t, time.Now().UTC().Add(-time.Hour))
			repo := newReminderDeliveryFakeRepo()
			f.seed(repo)
			request := f.revokeRequest()
			if test.mutate != nil {
				test.mutate(&request)
			}
			result, err := NewService(repo).(ReminderActivationService).DecideReminderActivationForOwner(test.owner, test.actor, f.activation.ID, request)
			if err == nil || result != nil || len(repo.decisions[f.activation.ID]) != 1 {
				t.Fatalf("unsafe revocation accepted: result=%#v err=%v", result, err)
			}
		})
	}
}

func TestReminderApprovalStillRejectsExpiredOrChangedPreparation(t *testing.T) {
	for _, expired := range []bool{false, true} {
		t.Run(map[bool]string{false: "changed source", true: "expired preparation"}[expired], func(t *testing.T) {
			requestedAt := time.Now().UTC().Add(-3 * time.Minute)
			if expired {
				requestedAt = requestedAt.Add(-time.Hour)
			}
			f := newHistoricalReminderFixture(t, requestedAt)
			repo := newReminderDeliveryFakeRepo()
			f.seed(repo)
			if !expired {
				repo.checklist[f.source.Workflow.ID][0].Label = "Changed source"
			}
			request := f.revokeRequest()
			request.Decision = ReminderActivationDecisionApproved
			request.Confirmation = ReminderActivationApproveConfirmation
			result, err := NewService(repo).(ReminderActivationService).DecideReminderActivationForOwner("alice", "alice", f.activation.ID, request)
			if err == nil || result != nil || len(repo.decisions[f.activation.ID]) != 1 {
				t.Fatalf("unsafe approval accepted: result=%#v err=%v", result, err)
			}
		})
	}
}
