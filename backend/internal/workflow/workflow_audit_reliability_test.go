package workflow

import (
	"errors"
	"strings"
	"testing"

	"automation-hub-backend/internal/models"
	"github.com/google/uuid"
)

type workflowAuditFailureRepository struct {
	*fakeWorkflowRepo
	transitionErr error
	decisionErr   error
	eventErr      error
	eventType     string
}

func (r *workflowAuditFailureRepository) CreateTransition(transition *models.WorkflowTransition) (*models.WorkflowTransition, error) {
	if r.transitionErr != nil {
		return nil, r.transitionErr
	}
	return r.fakeWorkflowRepo.CreateTransition(transition)
}

func (r *workflowAuditFailureRepository) CreateDecision(decision *models.WorkflowDecision) (*models.WorkflowDecision, error) {
	if r.decisionErr != nil {
		return nil, r.decisionErr
	}
	return r.fakeWorkflowRepo.CreateDecision(decision)
}

func (r *workflowAuditFailureRepository) CreateEvent(event *models.WorkflowEvent) (*models.WorkflowEvent, error) {
	if r.eventErr != nil && (r.eventType == "" || r.eventType == event.EventType) {
		return nil, r.eventErr
	}
	return r.fakeWorkflowRepo.CreateEvent(event)
}

func (r *workflowAuditFailureRepository) CommitWorkflowIntake(finalization WorkflowIntakeFinalization) (*models.WorkflowItem, bool, error) {
	if r.transitionErr != nil {
		return nil, false, workflowAuditPersistenceFailure("intake transition", r.transitionErr)
	}
	if r.decisionErr != nil {
		return nil, false, workflowAuditPersistenceFailure("intake decisions", r.decisionErr)
	}
	if r.eventErr != nil {
		for _, event := range finalization.Events {
			if r.eventType == "" || r.eventType == event.EventType {
				return nil, false, workflowAuditPersistenceFailure("intake events", r.eventErr)
			}
		}
	}
	return r.fakeWorkflowRepo.CommitWorkflowIntake(finalization)
}

func TestManualTransitionReportsAuditPersistenceFailures(t *testing.T) {
	for _, test := range []struct {
		name  string
		fail  func(*workflowAuditFailureRepository, error)
		check func(*testing.T, *workflowAuditFailureRepository, uuid.UUID, int, int)
	}{
		{
			name: "transition record",
			fail: func(repo *workflowAuditFailureRepository, err error) { repo.transitionErr = err },
			check: func(t *testing.T, repo *workflowAuditFailureRepository, id uuid.UUID, transitionsBefore, eventsBefore int) {
				if got := len(repo.transitions[id]); got != transitionsBefore {
					t.Fatalf("transition records = %d, want unchanged count %d", got, transitionsBefore)
				}
				if got := len(repo.events[id]); got != eventsBefore {
					t.Fatalf("events = %d, want unchanged count %d", got, eventsBefore)
				}
			},
		},
		{
			name: "transition event",
			fail: func(repo *workflowAuditFailureRepository, err error) {
				repo.eventErr = err
				repo.eventType = "workflow.transition"
			},
			check: func(t *testing.T, repo *workflowAuditFailureRepository, id uuid.UUID, transitionsBefore, eventsBefore int) {
				if got := len(repo.transitions[id]); got != transitionsBefore {
					t.Fatalf("transition records = %d, want unchanged count %d", got, transitionsBefore)
				}
				if got := len(repo.events[id]); got != eventsBefore {
					t.Fatalf("events = %d, want unchanged count %d", got, eventsBefore)
				}
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			cause := errors.New("audit store unavailable")
			repo := &workflowAuditFailureRepository{fakeWorkflowRepo: newFakeWorkflowRepo()}
			service := NewService(repo)
			record, err := service.Intake(IntakeRequest{OwnerIdentity: "alice", Input: "Create an internal low-risk checklist."})
			if err != nil {
				t.Fatalf("Intake: %v", err)
			}
			transitionsBefore := len(repo.transitions[record.Item.ID])
			eventsBefore := len(repo.events[record.Item.ID])
			originalState := repo.items[record.Item.ID].CurrentState
			test.fail(repo, cause)

			_, err = service.Transition(record.Item.ID, TransitionRequest{TargetState: StateBlocked, Message: "manual blocker"})
			assertWorkflowAuditPersistenceFailure(t, err, cause)
			if item := repo.items[record.Item.ID]; item.CurrentState != originalState {
				t.Fatalf("workflow state = %q, want unchanged state %q after audit failure", item.CurrentState, originalState)
			}
			test.check(t, repo, record.Item.ID, transitionsBefore, eventsBefore)
		})
	}
}

func TestGenericProposalAuditFailuresStopBeforeWorkflowStateAdvance(t *testing.T) {
	for _, test := range []struct {
		name              string
		fail              func(*workflowAuditFailureRepository, error)
		wantState         string
		wantTransitions   int
		wantProposalEvent bool
	}{
		{
			name:      "proposal decision",
			fail:      func(repo *workflowAuditFailureRepository, err error) { repo.decisionErr = err },
			wantState: StateWaitingInput,
		},
		{
			name: "proposal event",
			fail: func(repo *workflowAuditFailureRepository, err error) {
				repo.eventErr = err
				repo.eventType = "workflow.proposal"
			},
			wantState:         StateWaitingInput,
			wantProposalEvent: false,
		},
		{
			name:              "proposal transition record",
			fail:              func(repo *workflowAuditFailureRepository, err error) { repo.transitionErr = err },
			wantState:         StateReady,
			wantTransitions:   0,
			wantProposalEvent: true,
		},
		{
			name: "proposal transition event",
			fail: func(repo *workflowAuditFailureRepository, err error) {
				repo.eventErr = err
				repo.eventType = "workflow.transition"
			},
			wantState:         StateReady,
			wantTransitions:   1,
			wantProposalEvent: true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			cause := errors.New("workflow audit database unavailable")
			repo := &workflowAuditFailureRepository{fakeWorkflowRepo: newFakeWorkflowRepo()}
			service := NewService(repo)
			record, err := service.Intake(IntakeRequest{OwnerIdentity: "alice", Input: "Create an internal low-risk checklist."})
			if err != nil {
				t.Fatalf("Intake: %v", err)
			}
			putWorkflowInWaitingInputState(repo, record.Item.ID)
			transitionsBefore := len(repo.transitions[record.Item.ID])
			eventsBefore := len(repo.events[record.Item.ID])
			test.fail(repo, cause)

			_, err = service.ResolveProposal(record.Item.ID, record.Proposals[0].ID, ProposalResolutionRequest{
				Approved: true,
				Actor:    "alice",
				Note:     "approve the reviewed checklist",
			})
			assertWorkflowAuditPersistenceFailure(t, err, cause)
			if item := repo.items[record.Item.ID]; item.CurrentState != test.wantState {
				t.Fatalf("workflow state = %q, want %q", item.CurrentState, test.wantState)
			}
			if got := len(repo.transitions[record.Item.ID]) - transitionsBefore; got != test.wantTransitions {
				t.Fatalf("new transition records = %d, want %d", got, test.wantTransitions)
			}
			if got := hasWorkflowEvent(repo.events[record.Item.ID][0:len(repo.events[record.Item.ID])-eventsBefore], "workflow.proposal"); got != test.wantProposalEvent {
				t.Fatalf("proposal event persisted = %t, want %t", got, test.wantProposalEvent)
			}
			if test.wantState == StateWaitingInput && repo.proposals[record.Item.ID][0].Status != "approved" {
				t.Fatalf("proposal status = %q, want accepted decision persisted before audit failure", repo.proposals[record.Item.ID][0].Status)
			}
		})
	}
}

func TestGenericProposalSelectionDecisionFailureLeavesProposalOpen(t *testing.T) {
	cause := errors.New("decision store unavailable")
	repo := &workflowAuditFailureRepository{fakeWorkflowRepo: newFakeWorkflowRepo()}
	service := NewService(repo)
	record, err := service.Intake(IntakeRequest{OwnerIdentity: "alice", Input: "Create an internal low-risk checklist."})
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}
	putWorkflowInWaitingInputState(repo, record.Item.ID)
	automationID := uuid.NewString()
	option := "Use Demo [automation:" + automationID + "] - reviewed candidate"
	selection, err := repo.CreateProposal(&models.WorkflowProposal{
		WorkflowID:        record.Item.ID,
		RecommendedAction: automationSelectionProposalAction,
		Options:           option,
		Status:            "open",
	})
	if err != nil {
		t.Fatalf("CreateProposal: %v", err)
	}
	repo.decisionErr = cause

	_, err = service.ResolveProposal(record.Item.ID, selection.ID, ProposalResolutionRequest{
		Approved:       true,
		SelectedOption: option,
		Actor:          "alice",
	})
	assertWorkflowAuditPersistenceFailure(t, err, cause)
	if item := repo.items[record.Item.ID]; item.CurrentState != StateWaitingInput {
		t.Fatalf("workflow state = %q, want waiting_input", item.CurrentState)
	}
	if item := repo.items[record.Item.ID]; item.AutomationID != automationID {
		t.Fatalf("selected automation = %q, want selection persisted before audit error", item.AutomationID)
	}
	if proposals := repo.proposals[record.Item.ID]; proposals[0].ID != selection.ID || proposals[0].Status != "open" {
		t.Fatalf("selection proposal = %#v, want open after decision audit failure", proposals[0])
	}
}

func TestDeduplicatedIntakeReportsAuditPersistenceFailure(t *testing.T) {
	cause := errors.New("event store unavailable")
	repo := &workflowAuditFailureRepository{fakeWorkflowRepo: newFakeWorkflowRepo()}
	service := NewService(repo)
	request := IntakeRequest{
		OwnerIdentity: "alice",
		Input:         "Follow up: prepare the first project record.",
		SourceType:    "email",
		SourceID:      "message-dedupe-audit",
		SourceURI:     "mailto:project@example.test",
	}
	if _, err := service.Intake(request); err != nil {
		t.Fatalf("Intake first: %v", err)
	}
	repo.eventErr = cause
	repo.eventType = "workflow.intake_deduped"

	if _, err := service.Intake(request); err == nil {
		t.Fatal("deduplicated intake succeeded despite a missing audit event")
	} else {
		assertWorkflowAuditPersistenceFailure(t, err, cause)
	}
}

func TestIntakeFinalizationAuditFailureNeverActivatesWorkflow(t *testing.T) {
	cause := errors.New("intake audit store unavailable")
	for _, test := range []struct {
		name  string
		setup func(*workflowAuditFailureRepository)
	}{
		{name: "transition", setup: func(repo *workflowAuditFailureRepository) { repo.transitionErr = cause }},
		{name: "decision", setup: func(repo *workflowAuditFailureRepository) { repo.decisionErr = cause }},
		{name: "event", setup: func(repo *workflowAuditFailureRepository) {
			repo.eventErr = cause
			repo.eventType = "workflow.intake"
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo := &workflowAuditFailureRepository{fakeWorkflowRepo: newFakeWorkflowRepo()}
			test.setup(repo)
			service := NewService(repo)
			_, err := service.Intake(IntakeRequest{
				OwnerIdentity: "alice",
				Input:         "Create an internal low-risk checklist.",
				SourceType:    "email",
				SourceID:      "finalization-audit-" + test.name,
			})
			var persistenceErr *WorkflowPersistenceError
			if !errors.As(err, &persistenceErr) {
				t.Fatalf("Intake error = %v, want a typed persistence failure", err)
			}
			item, findErr := repo.FindActiveItemBySourceIdentityForOwner("alice", "email", "finalization-audit-"+test.name)
			if findErr != nil || item == nil {
				t.Fatalf("find retained intake item = %#v, %v", item, findErr)
			}
			if item.CurrentState != StateBlocked {
				t.Fatalf("workflow state = %q, want fail-closed blocked state", item.CurrentState)
			}
			for _, transition := range repo.transitions[item.ID] {
				if transition.ToState == StateReady {
					t.Fatal("failed intake finalization recorded an activation transition")
				}
			}
			if len(repo.decisions[item.ID]) != 0 {
				t.Fatalf("partial intake decisions were retained: %#v", repo.decisions[item.ID])
			}
			if hasWorkflowEvent(repo.events[item.ID], "workflow.intake") {
				t.Fatal("failed intake finalization retained its completion event")
			}
		})
	}
}

func putWorkflowInWaitingInputState(repo *workflowAuditFailureRepository, id uuid.UUID) {
	item := *repo.items[id]
	item.CurrentState = StateWaitingInput
	item.RequiresApproval = false
	item.ApprovalStatus = approvalStatus(false)
	item.ApprovalReason = ""
	repo.items[id] = &item
}

func hasWorkflowEvent(events []models.WorkflowEvent, eventType string) bool {
	for _, event := range events {
		if event.EventType == eventType {
			return true
		}
	}
	return false
}

func assertWorkflowAuditPersistenceFailure(t *testing.T, err, cause error) {
	t.Helper()
	var auditErr *WorkflowAuditPersistenceError
	if !errors.As(err, &auditErr) {
		t.Fatalf("error = %v, want *WorkflowAuditPersistenceError", err)
	}
	if !errors.Is(err, cause) {
		t.Fatalf("error = %v, want wrapped cause %v", err, cause)
	}
	if !strings.Contains(err.Error(), "audit history is incomplete") {
		t.Fatalf("error = %q, want visible incomplete-audit context", err)
	}
}
