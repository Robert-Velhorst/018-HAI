package workflow

import (
	"errors"
	"maps"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// The existing in-memory test repository gains this capability only in tests.
// Stage detached copies so failures cannot expose a partial projection.
func (r *fakeWorkflowRepo) commitDueOpenLoop(owner string, workflowID, loopID uuid.UUID, claimID string) (*followUpCommit, error) {
	item, err := r.FindItem(workflowID)
	if err != nil || (owner != "" && item.OwnerIdentity != owner) {
		return nil, gorm.ErrRecordNotFound
	}
	var loop models.WorkflowOpenLoop
	for _, candidate := range r.openLoops[workflowID] {
		if candidate.ID == loopID {
			loop = candidate
		}
	}
	active := loop.Status == "processing" && claimID != "" && loop.ClaimID == claimID && loop.LeaseUntil != nil && loop.LeaseUntil.After(time.Now().UTC())
	for _, receipt := range r.decisions[workflowID] {
		if receipt.ID == followUpRecordID(loopID, "decision") {
			if loop.Status == "processing" && active {
				loop.Status, loop.ClaimID, loop.LeaseUntil = receipt.Decision, "", nil
				if _, err := r.UpdateOpenLoop(&loop); err != nil {
					return nil, err
				}
			} else if loop.Status != receipt.Decision {
				return nil, errFollowUpClaimLost
			}
			return &followUpCommit{Item: item, FromState: item.CurrentState, Status: receipt.Decision, Replayed: true}, nil
		}
	}
	if !active || (loop.FollowUpAt != nil && loop.FollowUpAt.After(time.Now().UTC())) {
		return nil, errFollowUpClaimLost
	}
	projection, err := planDueOpenLoop(r, item, loop)
	if err != nil {
		return nil, err
	}
	staged := *r
	staged.items = maps.Clone(r.items)
	staged.checklist = maps.Clone(r.checklist)
	staged.checklist[workflowID] = slices.Clone(r.checklist[workflowID])
	staged.proposals = maps.Clone(r.proposals)
	staged.transitions = maps.Clone(r.transitions)
	staged.decisions = maps.Clone(r.decisions)
	staged.decisionWorkflow = maps.Clone(r.decisionWorkflow)
	staged.events = maps.Clone(r.events)
	staged.openLoops = maps.Clone(r.openLoops)
	staged.openLoops[workflowID] = slices.Clone(r.openLoops[workflowID])
	stored := item
	if projection.Updated != nil {
		var changed bool
		stored, changed, err = staged.UpdateWorkflowItemCAS(item, projection.Updated)
		if err != nil || !changed {
			return nil, errors.New("workflow projection conflict")
		}
	}
	if projection.Checklist != nil {
		if projection.ChecklistExists {
			_, err = staged.UpdateChecklistItem(projection.Checklist)
		} else {
			_, err = staged.CreateChecklistItem(projection.Checklist)
		}
		if err != nil {
			return nil, err
		}
	}
	if projection.Proposal != nil && !projection.ProposalExists {
		if _, err := staged.CreateProposal(projection.Proposal); err != nil {
			return nil, err
		}
	}
	if projection.Transition != nil {
		if _, err := staged.CreateTransition(projection.Transition); err != nil {
			return nil, err
		}
	}
	if _, err := staged.CreateDecision(&projection.Decision); err != nil {
		return nil, err
	}
	if _, err := staged.CreateEvent(&projection.Event); err != nil {
		return nil, err
	}
	loop.Status, loop.ClaimID, loop.LeaseUntil = projection.Status, "", nil
	if _, err := staged.UpdateOpenLoop(&loop); err != nil {
		return nil, err
	}
	*r = staged
	return &followUpCommit{Item: stored, FromState: item.CurrentState, Status: projection.Status}, nil
}

func (r *fakeWorkflowRepo) releaseDueOpenLoopClaim(owner string, workflowID, loopID uuid.UUID, claimID string) (bool, error) {
	if item := r.items[workflowID]; item == nil || (owner != "" && item.OwnerIdentity != owner) {
		return false, nil
	}
	for _, loop := range r.openLoops[workflowID] {
		if loop.ID == loopID && loop.Status == "processing" && loop.ClaimID == claimID && loop.LeaseUntil != nil && loop.LeaseUntil.After(time.Now().UTC()) {
			loop.Status, loop.ClaimID, loop.LeaseUntil = "open", "", nil
			_, err := r.UpdateOpenLoop(&loop)
			return err == nil, err
		}
	}
	return false, nil
}

func followUpMemoryFixture(t *testing.T) (*fakeWorkflowRepo, *service, models.WorkflowOpenLoop) {
	t.Helper()
	r := newFakeWorkflowRepo()
	item, err := r.CreateItem(&models.WorkflowItem{
		ID: uuid.New(), OwnerIdentity: "alice", Title: "Follow-up", CurrentState: StateWaitingInput,
		RiskLevel: "low", ApprovalStatus: "not_required", NextAction: "wait for response",
	})
	if err != nil {
		t.Fatal(err)
	}
	loop, err := r.CreateOpenLoop(&models.WorkflowOpenLoop{
		ID: uuid.New(), WorkflowID: item.ID, Status: "processing", ClaimID: "worker-a",
		LeaseUntil: timePtr(time.Now().Add(time.Hour)), FollowUpAt: timePtr(time.Now().Add(-time.Hour)),
		WaitingFor: "client response", NextAction: "prepare draft", ResponsibleParty: "assistant",
	})
	if err != nil {
		t.Fatal(err)
	}
	return r, NewService(r).(*service), *loop
}

func TestFollowUpProjectionRollsBackAndRetriesWithStableIDs(t *testing.T) {
	r, engine, loop := followUpMemoryFixture(t)
	before := *r.items[loop.WorkflowID]
	r.createProposalErr = errors.New("proposal store unavailable")
	result := engine.runOpenLoop(loop, loop.ClaimID)
	if result.Status != "skipped" || len(r.checklist[loop.WorkflowID]) != 0 || len(r.proposals[loop.WorkflowID]) != 0 ||
		len(r.decisions[loop.WorkflowID]) != 0 || len(r.events[loop.WorkflowID]) != 0 || !reflect.DeepEqual(before, *r.items[loop.WorkflowID]) ||
		r.openLoops[loop.WorkflowID][0].Status != "processing" {
		t.Fatalf("failed projection was partially persisted: %+v", result)
	}
	r.createProposalErr = nil
	result = engine.runOpenLoop(loop, loop.ClaimID)
	if result.Status != "triggered" || r.checklist[loop.WorkflowID][0].ID != followUpRecordID(loop.ID, "checklist") ||
		r.proposals[loop.WorkflowID][0].ID != followUpRecordID(loop.ID, "proposal") ||
		r.decisions[loop.WorkflowID][0].ID != followUpRecordID(loop.ID, "decision") ||
		r.events[loop.WorkflowID][0].ID != followUpRecordID(loop.ID, "event") {
		t.Fatalf("retry did not persist deterministic projection: %+v", result)
	}
}

func TestFollowUpReplayPreservesLaterReviewDecisions(t *testing.T) {
	r, engine, loop := followUpMemoryFixture(t)
	if result := engine.runOpenLoop(loop, loop.ClaimID); result.Status != "triggered" {
		t.Fatal(result)
	}
	r.items[loop.WorkflowID].CurrentState = StateBlocked
	r.items[loop.WorkflowID].RecoveryStatus = RecoveryNeedsReview
	r.items[loop.WorkflowID].NextAction = "review uncertain external outcome"
	r.proposals[loop.WorkflowID][0].Status = "rejected"
	r.checklist[loop.WorkflowID][0].Status = "completed"
	before := *r.items[loop.WorkflowID]
	for i := 0; i < 3; i++ {
		result := engine.runOpenLoop(loop, loop.ClaimID)
		if result.Status != "triggered" || !strings.Contains(result.Message, "not reapplied") {
			t.Fatalf("replay = %+v", result)
		}
	}
	if !reflect.DeepEqual(before, *r.items[loop.WorkflowID]) || r.proposals[loop.WorkflowID][0].Status != "rejected" ||
		r.checklist[loop.WorkflowID][0].Status != "completed" || len(r.decisions[loop.WorkflowID]) != 1 || len(r.events[loop.WorkflowID]) != 1 {
		t.Fatal("replay changed later review decisions or duplicated durable records")
	}
}

func TestFollowUpProjectionRejectsExpiredStolenForeignAndExecutionClaims(t *testing.T) {
	for _, name := range []string{"expired", "null_lease", "stolen", "foreign_owner", "execution", "review_fence", "retracted"} {
		t.Run(name, func(t *testing.T) {
			r, engine, loop := followUpMemoryFixture(t)
			owner := "alice"
			switch name {
			case "expired":
				r.openLoops[loop.WorkflowID][0].LeaseUntil = timePtr(time.Now().Add(-time.Hour))
			case "null_lease":
				r.openLoops[loop.WorkflowID][0].LeaseUntil = nil
			case "stolen":
				r.openLoops[loop.WorkflowID][0].ClaimID = "worker-b"
			case "foreign_owner":
				owner = "bob"
			case "execution":
				r.items[loop.WorkflowID].WorkerClaimID = "execution-worker"
			case "review_fence":
				r.items[loop.WorkflowID].RecoveryStatus = RecoveryNeedsReview
			case "retracted":
				quarantineRetractedWorkflow(r.items[loop.WorkflowID], "removed")
			}
			before := *r.items[loop.WorkflowID]
			result := engine.runOpenLoopForOwner(owner, loop, loop.ClaimID)
			if result.Status != "skipped" || len(r.checklist[loop.WorkflowID]) != 0 || len(r.proposals[loop.WorkflowID]) != 0 ||
				len(r.decisions[loop.WorkflowID]) != 0 || !reflect.DeepEqual(before, *r.items[loop.WorkflowID]) {
				t.Fatalf("unsafe projection = %+v", result)
			}
		})
	}
}

func TestFollowUpProjectionDistinctLoopsWithIdenticalText(t *testing.T) {
	r, engine, loop := followUpMemoryFixture(t)
	other := loop
	other.ID = uuid.New()
	if _, err := r.CreateOpenLoop(&other); err != nil {
		t.Fatal(err)
	}
	for _, current := range []models.WorkflowOpenLoop{loop, other} {
		if result := engine.runOpenLoop(current, current.ClaimID); result.Status != "triggered" {
			t.Fatal(result)
		}
	}
	if len(r.checklist[loop.WorkflowID]) != 2 || len(r.proposals[loop.WorkflowID]) != 2 || len(r.decisions[loop.WorkflowID]) != 2 {
		t.Fatal("distinct loop identities were collapsed by matching text")
	}
}

func TestFollowUpProjectionRejectsAmbiguousLegacyAdoption(t *testing.T) {
	for _, kind := range []string{"checklist", "proposal"} {
		t.Run(kind, func(t *testing.T) {
			r, engine, loop := followUpMemoryFixture(t)
			other := loop
			other.ID = uuid.New()
			if _, err := r.CreateOpenLoop(&other); err != nil {
				t.Fatal(err)
			}
			if kind == "checklist" {
				_, err := r.CreateChecklistItem(&models.WorkflowChecklistItem{
					ID: uuid.New(), WorkflowID: loop.WorkflowID, Status: "open",
					Label: "Resolve due open loop: " + compact(loop.WaitingFor, 160),
				})
				if err != nil {
					t.Fatal(err)
				}
			} else {
				_, err := r.CreateProposal(&models.WorkflowProposal{
					ID: uuid.New(), WorkflowID: loop.WorkflowID, Status: "open",
					RecommendedAction: "Follow-up due: " + firstNonEmpty(loop.NextAction, loop.WaitingFor),
				})
				if err != nil {
					t.Fatal(err)
				}
			}
			before := *r.items[loop.WorkflowID]
			checklistBefore := slices.Clone(r.checklist[loop.WorkflowID])
			proposalsBefore := slices.Clone(r.proposals[loop.WorkflowID])
			for _, current := range []models.WorkflowOpenLoop{loop, other} {
				result := engine.runOpenLoop(current, current.ClaimID)
				if result.Status != "skipped" || !strings.Contains(result.Message, "matches multiple") {
					t.Fatalf("ambiguous legacy adoption = %+v", result)
				}
			}
			if !reflect.DeepEqual(before, *r.items[loop.WorkflowID]) ||
				!reflect.DeepEqual(checklistBefore, r.checklist[loop.WorkflowID]) || !reflect.DeepEqual(proposalsBefore, r.proposals[loop.WorkflowID]) ||
				len(r.decisions[loop.WorkflowID]) != 0 || len(r.events[loop.WorkflowID]) != 0 {
				t.Fatal("ambiguous adoption changed workflow, legacy artifacts, or audit history")
			}
		})
	}
}

func TestFollowUpProjectionPreservesApprovalAndReviewWithoutExecuting(t *testing.T) {
	for _, name := range []string{"high_risk", "approval_required", "robert_responsible", "approval_state", "blocked_review"} {
		t.Run(name, func(t *testing.T) {
			r, engine, loop := followUpMemoryFixture(t)
			runner := &fakeTaskRunner{}
			engine.taskRunner = runner
			switch name {
			case "high_risk":
				r.items[loop.WorkflowID].RiskLevel = "high"
			case "approval_required":
				r.items[loop.WorkflowID].RequiresApproval = true
			case "robert_responsible":
				loop.ResponsibleParty = "Robert"
				r.openLoops[loop.WorkflowID][0] = loop
			case "approval_state":
				r.items[loop.WorkflowID].CurrentState = StateNeedsApproval
			case "blocked_review":
				r.items[loop.WorkflowID].CurrentState = StateBlocked
				r.items[loop.WorkflowID].RecoveryStatus = RecoveryNeedsReview
				r.items[loop.WorkflowID].BlockedReason = "uncertain external outcome"
				r.items[loop.WorkflowID].NextAction = "review uncertain external outcome"
			}
			result := engine.runOpenLoop(loop, loop.ClaimID)
			if result.Status != "triggered" || len(runner.requests) != 0 || r.items[loop.WorkflowID].CompletedAt != nil || r.decisions[loop.WorkflowID][0].Approved {
				t.Fatalf("follow-up executed or fabricated completion/approval: %+v", result)
			}
			if name == "blocked_review" {
				if result.State != StateBlocked || r.items[loop.WorkflowID].NextAction != "review uncertain external outcome" {
					t.Fatal("follow-up cleared interruption review")
				}
			} else if result.State != StateNeedsApproval || r.items[loop.WorkflowID].ApprovalStatus != "pending" ||
				!r.checklist[loop.WorkflowID][0].RequiresApproval || strings.Contains(r.proposals[loop.WorkflowID][0].Options, "automatically") {
				t.Fatal("follow-up bypassed approval")
			}
		})
	}
}

type followUpUnsupportedRepo struct{ Repository }

func TestFollowUpProjectionFailsClosedWithoutTransactionalCapability(t *testing.T) {
	r, _, loop := followUpMemoryFixture(t)
	engine := NewService(&followUpUnsupportedRepo{Repository: r}).(*service)
	result := engine.runOpenLoop(loop, loop.ClaimID)
	if result.Status != "skipped" || !strings.Contains(result.Message, "unavailable") || len(r.checklist[loop.WorkflowID]) != 0 || len(r.proposals[loop.WorkflowID]) != 0 {
		t.Fatalf("unsupported repository did not fail closed: %+v", result)
	}
	if _, err := (*GormRepository)(nil).commitDueOpenLoop("alice", loop.WorkflowID, loop.ID, loop.ClaimID); err == nil {
		t.Fatal("nil database accepted a durable projection")
	}
	r.openLoops[loop.WorkflowID][0].Status = "open"
	before := r.openLoops[loop.WorkflowID][0]
	summary, err := engine.RunDueOpenLoops(RunDueRequest{Limit: 5})
	if err != nil || summary.Skipped != 1 || !reflect.DeepEqual(before, r.openLoops[loop.WorkflowID][0]) {
		t.Fatal("unsupported follow-up repository acquired a claim")
	}
}
