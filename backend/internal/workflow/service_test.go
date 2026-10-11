package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"automation-hub-backend/internal/memory"
	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/safety"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestEngineCapabilitiesDescribeGitHubConnectorSupportPrecisely(t *testing.T) {
	capabilities := make(map[string]EngineCapability)
	for _, capability := range engineCapabilities() {
		capabilities[capability.ID] = capability
	}

	for _, id := range []string{"adapters", "developer-github", "event-triggers", "universal-intake"} {
		if _, ok := capabilities[id]; !ok {
			t.Fatalf("engine capability %q is missing", id)
		}
	}

	assertEntry := func(capabilityID, section, expected string) {
		t.Helper()
		capability := capabilities[capabilityID]
		var entries []string
		switch section {
		case "implemented":
			entries = capability.Implemented
		case "next":
			entries = capability.Next
		default:
			t.Fatalf("unsupported capability section %q", section)
		}
		if !strings.Contains(strings.Join(entries, "\n"), expected) {
			t.Errorf("%s.%s does not document %q; entries: %q", capabilityID, section, expected, entries)
		}
	}

	assertEntry("adapters", "implemented", "read-only GitHub REST sync for repository, issue, pull request, commit, and Actions-run records; branch records are not imported")
	assertEntry("developer-github", "implemented", "read-only GitHub REST sync imports repository, issue, pull request, commit, and Actions-run records; branch records are not imported")
	assertEntry("developer-github", "next", "read-only branch comparison and repository acceptance reports (not implemented; no branch-record import)")
	assertEntry("adapters", "next", "GitHub webhook delivery (not implemented)")
	assertEntry("adapters", "implemented", "read-only Trello signed webhook intake and durable read-only board reconciliation")
	assertEntry("adapters", "next", "dedicated GitHub historical backfill mode (not implemented)")
	assertEntry("event-triggers", "implemented", "Trello signed callbacks are signature-verified, durable, idempotent, and source-bound")
	assertEntry("event-triggers", "next", "webhook workers for other connectors (not implemented)")
	assertEntry("universal-intake", "implemented", "Trello signed webhook callbacks enter the durable source-processing path")
	assertEntry("universal-intake", "next", "connector webhook intake for other providers (not implemented)")
	if capabilities["event-triggers"].Status != "partial" || capabilities["universal-intake"].Status != "partial" {
		t.Fatalf("connector-specific webhook coverage must remain partial: event-triggers=%q universal-intake=%q", capabilities["event-triggers"].Status, capabilities["universal-intake"].Status)
	}

	for _, id := range []string{"adapters", "developer-github", "event-triggers", "universal-intake"} {
		for _, implemented := range capabilities[id].Implemented {
			lower := strings.ToLower(implemented)
			if strings.Contains(lower, "not implemented") || strings.Contains(lower, "historical backfill") {
				t.Errorf("%s claims unimplemented behavior as implemented: %q", id, implemented)
			}
			if strings.Contains(lower, "branch") && !strings.Contains(lower, "not imported") {
				t.Errorf("%s claims branch capability without identifying that branch records are not imported: %q", id, implemented)
			}
		}
	}
}

func TestDeveloperQualityRuleDoesNotRequireUnsupportedBranchEvidence(t *testing.T) {
	for _, rule := range defaultWorkflowRules() {
		if rule.RuleKey != "developer.github_quality_gate" {
			continue
		}
		if strings.Contains(strings.ToLower(rule.Description), "branch evidence") {
			t.Fatalf("developer quality rule requires unsupported branch evidence: %q", rule.Description)
		}
		if !strings.Contains(rule.Description, "human review") || !strings.Contains(rule.Description, "Worker prose and labels alone are not proof") {
			t.Fatalf("developer quality rule omits fail-closed evidence guidance: %q", rule.Description)
		}
		return
	}
	t.Fatal("developer.github_quality_gate rule is missing")
}

func TestIntakeCreatesApprovalGatedLegalWorkflow(t *testing.T) {
	repo := newFakeWorkflowRepo()
	service := NewService(repo)

	record, err := service.Intake(IntakeRequest{
		Input:           "Email from lawyer about Vivare legal hearing tomorrow. Draft formal reply.",
		SuccessCriteria: []string{"Reply remains a draft", "Cite each attached source"},
		ProjectKey:      "Vivare dispute",
		SourceType:      "email",
		SourceURI:       "mailto:lawyer@example.test",
		Trigger:         "email.sync",
	})
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}
	if record.Item.CurrentState != StateNeedsApproval {
		t.Fatalf("state = %q, want needs approval", record.Item.CurrentState)
	}
	if !reflect.DeepEqual(record.Item.SuccessCriteria, []string{"Reply remains a draft", "Cite each attached source"}) {
		t.Fatalf("workflow success criteria = %#v", record.Item.SuccessCriteria)
	}
	if !record.Item.RequiresApproval {
		t.Fatalf("expected approval requirement")
	}
	if record.Item.PriorityScore < 80 {
		t.Fatalf("priority = %d, want high legal priority", record.Item.PriorityScore)
	}
	if len(record.Checklist) == 0 {
		t.Fatalf("expected generated checklist")
	}
	if !hasApprovalChecklist(record.Checklist) {
		t.Fatalf("expected approval-marked checklist item")
	}
	if len(record.Intake) != 1 {
		t.Fatalf("intake records = %d, want 1", len(record.Intake))
	}
	if len(record.Matches) != 1 {
		t.Fatalf("project matches = %d, want 1", len(record.Matches))
	}
	if len(record.OpenLoops) != 1 {
		t.Fatalf("open loops = %d, want 1 approval loop", len(record.OpenLoops))
	}
	if len(record.Proposals) != 1 {
		t.Fatalf("proposals = %d, want 1", len(record.Proposals))
	}
	if len(record.QualityGates) == 0 {
		t.Fatalf("expected quality gates")
	}
	if len(record.Evidence) == 0 {
		t.Fatalf("expected evidence claims from legal/hearing input")
	}
	if len(record.Events) < 2 {
		t.Fatalf("events = %d, want intake normalization and audit events", len(record.Events))
	}
	if len(record.SourceLinks) != 1 {
		t.Fatalf("source links = %d, want 1", len(record.SourceLinks))
	}
	if len(record.Decisions) < 3 {
		t.Fatalf("decisions = %d, want classification/priority/approval decisions", len(record.Decisions))
	}
	if len(record.Transitions) != 1 {
		t.Fatalf("transitions = %d, want intake transition", len(record.Transitions))
	}
}

func TestTrelloDefaultProjectHintIsNotStoredAsAConfirmedMatch(t *testing.T) {
	repo := newFakeWorkflowRepo()
	service := NewService(repo)
	record, err := service.Intake(IntakeRequest{
		OwnerIdentity:  "alice",
		Input:          "Tasks: review the supplier checklist and verify its source links",
		ProjectKeyHint: "018-HAI",
		SourceType:     "trello",
		SourceID:       "trello-extraction-1",
		SourceURI:      "https://trello.com/c/card-1",
		RequiresReview: true,
		ReviewReason:   "Trello evidence needs owner review",
	})
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}
	if record.Item.ProjectKey != "" {
		t.Fatalf("workflow ProjectKey = %q, want no unverified Trello default", record.Item.ProjectKey)
	}
	if len(record.Matches) != 0 {
		t.Fatalf("project matches = %#v, want no confirmed project match", record.Matches)
	}
	if len(record.Intake) != 1 || record.Intake[0].PossibleProject != "018-HAI" {
		t.Fatalf("intake possible-project hint = %#v, want retained 018-HAI hint", record.Intake)
	}
	foundUnverified := false
	for _, decision := range record.Decisions {
		if decision.DecisionType == "project_match" && decision.Decision == "unverified" {
			foundUnverified = true
			break
		}
	}
	if !foundUnverified {
		t.Fatalf("decisions = %#v, want an explicit unverified project-key decision", record.Decisions)
	}
}

func TestTrelloProjectHintDoesNotOverrideContentMatchOrOtherExplicitKeys(t *testing.T) {
	key, confidence, reasons, _, _ := matchProject(IntakeRequest{
		ProjectKeyHint: "018-HAI",
		SourceType:     "trello",
	}, "vivare hearing evidence", "legal")
	if key != "Vivare dispute" || confidence != 0.88 || len(reasons) == 0 {
		t.Fatalf("Trello content match = (%q, %0.2f, %v), want Vivare heuristic match", key, confidence, reasons)
	}

	key, confidence, _, _, _ = matchProject(IntakeRequest{
		ProjectKey: "custom-manual-project",
		SourceType: "workflow_api",
	}, "unmatched administrative work", "administrative")
	if key != "custom-manual-project" || confidence != 0.95 {
		t.Fatalf("non-Trello explicit match = (%q, %0.2f), want existing explicit-key behavior", key, confidence)
	}

	key, confidence, _, _, _ = matchProject(IntakeRequest{
		ProjectKey: "confirmed-trello-project",
		SourceType: "trello",
	}, "unmatched administrative work", "administrative")
	if key != "confirmed-trello-project" || confidence != 0.95 {
		t.Fatalf("explicitly confirmed Trello project = (%q, %0.2f), want the supplied project key", key, confidence)
	}
}

func TestReminderProposalsAreOwnerScopedCurrentAndNonExecuting(t *testing.T) {
	repo := newFakeWorkflowRepo()
	service := NewService(repo)
	reminderService, ok := service.(ReminderProposalService)
	if !ok {
		t.Fatalf("workflow service does not expose reminder proposals")
	}
	due := time.Now().UTC().Add(48 * time.Hour).Truncate(time.Second)
	record, err := service.Intake(IntakeRequest{
		OwnerIdentity: "alice",
		Input:         "Calendar event: Evidence review\nStart: " + due.Format(time.RFC3339),
		ProjectKey:    "legal-case",
		SourceType:    "calendar",
		SourceID:      "event-1",
		SourceURI:     "calendar://event-1",
		SourceLabel:   "Evidence review",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Intake(IntakeRequest{
		OwnerIdentity: "bob",
		Input:         "Calendar event: Private review\nStart: " + due.Format(time.RFC3339),
		SourceType:    "calendar",
		SourceID:      "event-2",
	}); err != nil {
		t.Fatal(err)
	}
	var reminderAt time.Time
	for _, item := range record.Checklist {
		if item.ReminderAt != nil {
			reminderAt = *item.ReminderAt
			break
		}
	}
	if reminderAt.IsZero() {
		t.Fatal("deadline intake did not persist a reminder")
	}
	snapshot, err := reminderService.ReminderProposalsForOwner("alice", reminderAt.Add(time.Minute), 168, 100)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Authority != ReminderProposalAuthority || snapshot.CanExecute ||
		snapshot.Freshness.Status != ReminderProposalFreshness || !snapshot.Freshness.RevalidationRequired ||
		snapshot.Due != 1 || snapshot.Upcoming != 0 || len(snapshot.Items) != 1 {
		t.Fatalf("snapshot=%#v", snapshot)
	}
	proposal := snapshot.Items[0]
	if proposal.WorkflowID != record.Item.ID || proposal.Status != "due" || proposal.CanExecute ||
		proposal.Authority != ReminderProposalAuthority || proposal.SourceURI != "calendar://event-1" {
		t.Fatalf("proposal=%#v", proposal)
	}
	if _, err := reminderService.ReminderProposalsForOwner("alice", time.Now(), 721, 100); err == nil {
		t.Fatal("oversized reminder horizon was accepted")
	}
}

func TestIntakeBlocksWorkflowWhenDeadlineReminderCannotBePersisted(t *testing.T) {
	repo := newFakeWorkflowRepo()
	repo.createDeadlineReminderErr = fmt.Errorf("checklist store unavailable")
	service := NewService(repo)
	due := time.Now().UTC().Add(48 * time.Hour).Truncate(time.Second)

	_, err := service.Intake(IntakeRequest{
		OwnerIdentity: "alice",
		Input:         "Calendar event: Evidence review\nStart: " + due.Format(time.RFC3339),
		SourceType:    "calendar",
		SourceID:      "deadline-reminder-failure",
		SourceURI:     "calendar://deadline-reminder-failure",
		SourceLabel:   "Evidence review",
	})
	if err == nil || !strings.Contains(err.Error(), "deadline reminder persistence failed") {
		t.Fatalf("Intake error = %v, want deadline-reminder persistence failure", err)
	}
	if len(repo.items) != 1 {
		t.Fatalf("persisted workflow count = %d, want the intake retained as blocked work", len(repo.items))
	}
	var item *models.WorkflowItem
	for _, stored := range repo.items {
		item = stored
	}
	if item.CurrentState != StateBlocked || item.NextRunAt != nil ||
		!strings.Contains(item.BlockedReason, "deadline reminder") ||
		!strings.Contains(item.NextAction, "reminder storage") {
		t.Fatalf("workflow after reminder failure = state %q, nextRunAt %v, reason %q, nextAction %q; want blocked, unscheduled, and recoverable", item.CurrentState, item.NextRunAt, item.BlockedReason, item.NextAction)
	}
	for _, decision := range repo.decisions[item.ID] {
		if decision.DecisionType == "deadline_reminder" && decision.Decision == "created" {
			t.Fatal("workflow recorded a successful deadline-reminder decision after persistence failed")
		}
	}
	if transitions := repo.transitions[item.ID]; len(transitions) == 0 ||
		transitions[0].FromState != StateNewInput || transitions[0].ToState != StateBlocked ||
		transitions[0].Trigger != "deadline_reminder_persistence" {
		t.Fatalf("workflow transitions = %#v, want intake-to-blocked deadline-reminder failure transition", transitions)
	}
}

func TestIntakeBlocksWorkflowWhenRequiredChecklistCannotBePersisted(t *testing.T) {
	repo := newFakeWorkflowRepo()
	repo.createChecklistItemErr = fmt.Errorf("checklist store unavailable")
	service := NewService(repo)

	_, err := service.Intake(IntakeRequest{
		OwnerIdentity: "alice",
		Input:         "Create a low-risk checklist for a client quote.",
		SourceType:    "manual",
		SourceID:      "required-checklist-failure",
	})
	if err == nil || !strings.Contains(err.Error(), "checklist persistence failed") {
		t.Fatalf("Intake error = %v, want required-checklist persistence failure", err)
	}
	if len(repo.items) != 1 {
		t.Fatalf("persisted workflow count = %d, want the intake retained as blocked work", len(repo.items))
	}
	var item *models.WorkflowItem
	for _, stored := range repo.items {
		item = stored
	}
	if item.CurrentState != StateBlocked || item.NextRunAt != nil ||
		!strings.Contains(item.BlockedReason, "required checklist") ||
		!strings.Contains(item.NextAction, "checklist storage") {
		t.Fatalf("workflow after checklist failure = state %q, nextRunAt %v, reason %q, nextAction %q; want blocked, unscheduled, and recoverable", item.CurrentState, item.NextRunAt, item.BlockedReason, item.NextAction)
	}
	if len(repo.checklist[item.ID]) != 0 {
		t.Fatalf("persisted checklist = %#v, want no falsely accepted checklist items", repo.checklist[item.ID])
	}
	if transitions := repo.transitions[item.ID]; len(transitions) == 0 ||
		transitions[0].FromState != StateNewInput || transitions[0].ToState != StateBlocked ||
		transitions[0].Trigger != "checklist_persistence" {
		t.Fatalf("workflow transitions = %#v, want intake-to-blocked checklist failure transition", transitions)
	}
}

func TestIntakeBlocksApprovalWorkflowWhenProposalCannotBePersisted(t *testing.T) {
	repo := newFakeWorkflowRepo()
	repo.createProposalErr = fmt.Errorf("proposal store unavailable")
	service := NewService(repo)

	_, err := service.Intake(IntakeRequest{
		OwnerIdentity: "alice",
		Input:         "Email from lawyer about the hearing tomorrow. Draft a formal reply.",
		SourceType:    "email",
		SourceID:      "approval-proposal-failure",
		SourceURI:     "mailto:lawyer@example.test",
	})
	if err == nil || !strings.Contains(err.Error(), "proposal persistence failed") {
		t.Fatalf("Intake error = %v, want approval-proposal persistence failure", err)
	}
	var item *models.WorkflowItem
	for _, stored := range repo.items {
		item = stored
	}
	if item == nil || item.CurrentState != StateBlocked || item.NextRunAt != nil ||
		!item.RequiresApproval || item.ApprovalStatus != "pending" ||
		!strings.Contains(item.BlockedReason, "required workflow proposal") {
		t.Fatalf("workflow after proposal failure = %#v, want retained, approval-gated, unscheduled blocked work", item)
	}
	if len(repo.proposals[item.ID]) != 0 {
		t.Fatalf("persisted proposals = %#v, want none after repository failure", repo.proposals[item.ID])
	}
	if transitions := repo.transitions[item.ID]; len(transitions) == 0 ||
		transitions[0].FromState != StateNewInput || transitions[0].ToState != StateBlocked ||
		transitions[0].Trigger != "proposal_persistence" {
		t.Fatalf("workflow transitions = %#v, want intake-to-blocked proposal failure transition", transitions)
	}
}

func TestIntakeBlocksEvidenceRequiredWorkflowWhenClaimCannotBePersisted(t *testing.T) {
	repo := newFakeWorkflowRepo()
	repo.createEvidenceClaimErr = fmt.Errorf("evidence claim store unavailable")
	service := NewService(repo)

	_, err := service.Intake(IntakeRequest{
		Input:       "Email from lawyer: Vivare confirmed the hearing date. Draft a formal reply.",
		SourceType:  "email",
		SourceURI:   "mailto:lawyer@example.test",
		SourceLabel: "Lawyer email",
	})
	if err == nil || !strings.Contains(err.Error(), "evidence claim") {
		t.Fatalf("Intake error = %v, want evidence-claim persistence failure", err)
	}
	if len(repo.items) != 1 {
		t.Fatalf("persisted workflow items = %d, want blocked intake record", len(repo.items))
	}
	for _, item := range repo.items {
		if item.CurrentState != StateBlocked || item.NextRunAt != nil || !strings.Contains(item.BlockedReason, "evidence") {
			t.Fatalf("item was queued without durable evidence claims: %#v", item)
		}
	}
}

func TestEvidenceLinkedClaimsGateBlocksSensitiveWorkflowWithoutClaims(t *testing.T) {
	repo := newFakeWorkflowRepo()
	service := NewService(repo).(*service)
	record, err := service.Intake(IntakeRequest{
		Input:      "Prepare a legal response for the case.",
		SourceType: "manual",
		SourceURI:  "https://example.test/case-source",
	})
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}
	item := record.Item
	item.RequiresApproval = false
	delete(repo.evidence, item.ID)

	result := service.evaluateQualityGates(item, &TaskRunResult{
		VerificationStatus: "verified",
		Passed:             true,
	})
	if result.Passed || !result.ReviewRequired {
		t.Fatalf("quality gate result = %#v, want review-required failure without claims", result)
	}
	if !strings.Contains(strings.Join(result.Failures, "; "), "evidence-linked claims") {
		t.Fatalf("quality gate failures = %#v, want evidence-linked claims failure", result.Failures)
	}
	if !hasGateStatus(repo.qualityGate[item.ID], "evidence-linked claims", "needs_review") {
		t.Fatalf("evidence-linked claims gate was not left in needs_review: %#v", repo.qualityGate[item.ID])
	}
}

func TestIntakePersistsVerifiedOwnerIdentity(t *testing.T) {
	service := NewService(newFakeWorkflowRepo())
	record, err := service.Intake(IntakeRequest{
		OwnerIdentity: "alice",
		Input:         "Create a low-risk source-linked checklist for Alice's project.",
		SourceType:    "manual",
		SourceID:      "alice-intake-1",
	})
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}
	if record.Item.OwnerIdentity != "alice" {
		t.Fatalf("workflow owner = %q, want alice", record.Item.OwnerIdentity)
	}
}

func TestIntakePersistsExplicitStandingMandateBinding(t *testing.T) {
	mandateID := uuid.New()
	service := NewService(newFakeWorkflowRepo())
	record, err := service.Intake(IntakeRequest{
		OwnerIdentity: "alice",
		Input:         "Prepare the low-risk project status summary.",
		MandateID:     mandateID.String(),
		SourceType:    "manual",
		SourceID:      "mandated-intake-1",
	})
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}
	if record.Item.MandateID == nil || *record.Item.MandateID != mandateID {
		t.Fatalf("workflow mandate = %#v, want %s", record.Item.MandateID, mandateID)
	}
}

func TestIntakeRejectsMalformedStandingMandateBinding(t *testing.T) {
	service := NewService(newFakeWorkflowRepo())
	record, err := service.Intake(IntakeRequest{
		OwnerIdentity: "alice",
		Input:         "Prepare the project status summary.",
		MandateID:     "not-a-mandate-id",
	})
	if err == nil || !strings.Contains(err.Error(), "standing mandate id must be a UUID") {
		t.Fatalf("error = %v, want standing mandate UUID validation", err)
	}
	if record != nil {
		t.Fatalf("invalid mandate created workflow: %#v", record)
	}
}

func TestIntakeBindsSingleSuitableAutomationBeforeApproval(t *testing.T) {
	automationID := uuid.NewString()
	runner := &selectingTaskRunner{
		bindingTaskRunner: &bindingTaskRunner{
			fakeTaskRunner: &fakeTaskRunner{},
			binding:        "automation-action:" + strings.Repeat("a", 64),
		},
		candidates: []AutomationCandidate{{
			ID: automationID, Name: "Mail draft runtime", RuntimeType: "api", Score: 8, Reason: "email draft capability matched",
		}},
	}
	service := NewServiceWithTaskRunner(newFakeWorkflowRepo(), runner)
	record, err := service.Intake(IntakeRequest{
		OwnerIdentity: "alice",
		Input:         "Draft an email reply to the lawyer, but do not send it.",
		SourceType:    "email",
		SourceID:      "mail-1",
	})
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}
	if record.Item.AutomationID != automationID {
		t.Fatalf("automationId = %q, want %q", record.Item.AutomationID, automationID)
	}
	if len(runner.selectionRequests) != 1 || runner.selectionRequests[0].OwnerIdentity != "alice" {
		t.Fatalf("selection requests = %#v", runner.selectionRequests)
	}
}

func TestIntakeRequiresExplicitSelectionForNamedSingleRuntime(t *testing.T) {
	automationID := uuid.NewString()
	runner := &selectingTaskRunner{
		bindingTaskRunner: &bindingTaskRunner{fakeTaskRunner: &fakeTaskRunner{}},
		candidates: []AutomationCandidate{{
			ID: automationID, Name: "HAI backend readiness probe", RuntimeType: "api", Score: 20, Reason: "read-only readiness capability matched",
		}},
	}
	service := NewServiceWithTaskRunner(newFakeWorkflowRepo(), runner)
	record, err := service.Intake(IntakeRequest{
		OwnerIdentity: "operator@example.com",
		Input:         "Run the selected HAI backend readiness probe and record its read-only verification result. Do not send anything externally.",
		SourceType:    "manual",
		SourceID:      "e2e-readiness-single-runtime",
	})
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}
	if record.Item.AutomationID != "" || record.Item.CurrentState != StateNeedsApproval {
		t.Fatalf("explicit runtime selection was skipped: %#v", record.Item)
	}
	for index := range record.Proposals {
		if isAutomationSelectionProposal(&record.Proposals[index]) {
			return
		}
	}
	t.Fatalf("selection proposal = %#v, want an explicit runtime selection", record.Proposals)
}

func TestAmbiguousLowRiskAutomationSelectionDoesNotCreateActionApproval(t *testing.T) {
	firstID := uuid.NewString()
	secondID := uuid.NewString()
	runner := &selectingTaskRunner{
		bindingTaskRunner: &bindingTaskRunner{
			fakeTaskRunner: &fakeTaskRunner{},
			binding:        "automation-action:" + strings.Repeat("b", 64),
		},
		candidates: []AutomationCandidate{
			{ID: firstID, Name: "First runtime", Score: 7, Reason: "matched"},
			{ID: secondID, Name: "Second runtime", Score: 7, Reason: "matched"},
		},
	}
	service := NewServiceWithTaskRunner(newFakeWorkflowRepo(), runner)
	record, err := service.Intake(IntakeRequest{
		OwnerIdentity: "alice",
		Input:         "Run the project test suite and attach the result.",
		SourceType:    "github",
		SourceID:      "issue-12",
	})
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}
	if record.Item.AutomationID != "" || record.Item.CurrentState != StateNeedsApproval {
		t.Fatalf("ambiguous intake item = %#v", record.Item)
	}
	var selection *models.WorkflowProposal
	for index := range record.Proposals {
		if isAutomationSelectionProposal(&record.Proposals[index]) {
			selection = &record.Proposals[index]
			break
		}
	}
	if selection == nil {
		t.Fatal("runtime-selection proposal was not created")
	}
	if _, err := service.ResolveApproval(record.Item.ID, ApprovalResolutionRequest{
		Approved: true,
		Actor:    "alice",
		Note:     "approve without choosing a runtime",
	}); err == nil || !strings.Contains(err.Error(), "select the exact automation") {
		t.Fatalf("direct approval bypass error = %v", err)
	}
	selectedOption := strings.Split(selection.Options, "\n")[1]
	resolved, err := service.ResolveProposal(record.Item.ID, selection.ID, ProposalResolutionRequest{
		Status:         "approved",
		SelectedOption: selectedOption,
		Actor:          "alice",
		Note:           "Use the reviewed second runtime",
	})
	if err != nil {
		t.Fatalf("ResolveProposal: %v", err)
	}
	if resolved.Item.AutomationID != secondID || resolved.Item.CurrentState != StateReady || resolved.Item.RequiresApproval || resolved.Item.ApprovalStatus != "not_required" {
		t.Fatalf("resolved item = %#v, want second runtime ready without action approval", resolved.Item)
	}
	if len(runner.bindingRequests) != 0 {
		t.Fatalf("selection-only workflow unexpectedly prepared approval bindings: %#v", runner.bindingRequests)
	}
}

func TestReadOnlyReadinessSelectionPreservesAutomaticRuntimeAuthority(t *testing.T) {
	firstID := uuid.NewString()
	secondID := uuid.NewString()
	runner := &selectingTaskRunner{
		bindingTaskRunner: &bindingTaskRunner{fakeTaskRunner: &fakeTaskRunner{}},
		candidates: []AutomationCandidate{
			{ID: firstID, Name: "HAI backend readiness probe", Score: 20, Reason: "read-only readiness capability matched"},
			{ID: secondID, Name: "Other readiness probe", Score: 10, Reason: "read-only readiness capability matched"},
		},
	}
	service := NewServiceWithTaskRunner(newFakeWorkflowRepo(), runner)
	record, err := service.Intake(IntakeRequest{
		OwnerIdentity: "operator@example.com",
		Input:         "Run the selected HAI backend readiness probe-12345 for E2E governed pursuit 12345 (e2e-governed-12345) and record its read-only verification result. Do not send anything externally.",
		ProjectKey:    "e2e-governed-12345",
		SourceType:    "manual",
		SourceID:      "e2e-readiness-12345",
	})
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}
	if record.Item.RiskLevel != "low" || !record.Item.RequiresApproval || record.Item.ApprovalStatus != "pending" {
		t.Fatalf("selection gate should be the only temporary blocker: %#v", record.Item)
	}
	var selection *models.WorkflowProposal
	for index := range record.Proposals {
		if isAutomationSelectionProposal(&record.Proposals[index]) {
			selection = &record.Proposals[index]
			break
		}
	}
	if selection == nil {
		t.Fatal("runtime-selection proposal was not created")
	}
	selectedOption := strings.Split(selection.Options, "\n")[0]
	resolved, err := service.ResolveProposal(record.Item.ID, selection.ID, ProposalResolutionRequest{
		Status:         "approved",
		SelectedOption: selectedOption,
		Actor:          "operator@example.com",
		Note:           "Use the read-only readiness probe.",
	})
	if err != nil {
		t.Fatalf("ResolveProposal: %v", err)
	}
	if resolved.Item.AutomationID != firstID || resolved.Item.CurrentState != StateReady || resolved.Item.RequiresApproval || resolved.Item.ApprovalStatus != "not_required" {
		t.Fatalf("readiness selection changed execution authority: %#v", resolved.Item)
	}
	if len(runner.bindingRequests) != 0 {
		t.Fatalf("readiness selection created an approval binding: %#v", runner.bindingRequests)
	}
}

func TestReadOnlyAutomationSelectionUsesWorkflowApprovalWithoutActionProof(t *testing.T) {
	automationID := uuid.NewString()
	runner := &selectingTaskRunner{
		bindingTaskRunner: &bindingTaskRunner{
			fakeTaskRunner: &fakeTaskRunner{},
			binding:        "automation-action:automation.api.read:" + strings.Repeat("c", 64),
		},
		candidates: []AutomationCandidate{
			{ID: automationID, Name: "Read-only health probe", Score: 20, Reason: "matched"},
			{ID: uuid.NewString(), Name: "Other health probe", Score: 10, Reason: "matched"},
		},
	}
	service := NewServiceWithTaskRunner(newFakeWorkflowRepo(), runner)
	record, err := service.Intake(IntakeRequest{
		OwnerIdentity: "alice",
		Input:         "Email from a lawyer: run the read-only health probe and attach the result.",
		SourceType:    "email",
		SourceID:      "message-42",
	})
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}
	var selection *models.WorkflowProposal
	for index := range record.Proposals {
		if isAutomationSelectionProposal(&record.Proposals[index]) {
			selection = &record.Proposals[index]
			break
		}
	}
	if selection == nil {
		t.Fatal("runtime-selection proposal was not created")
	}
	selectedOption := strings.Split(selection.Options, "\n")[0]
	resolved, err := service.ResolveProposal(record.Item.ID, selection.ID, ProposalResolutionRequest{
		Status:         "approved",
		SelectedOption: selectedOption,
		Actor:          "alice",
		Note:           "Approve the read-only runtime for this reviewed legal workflow.",
	})
	if err != nil {
		t.Fatalf("ResolveProposal: %v", err)
	}
	if resolved.Item.AutomationID != automationID || resolved.Item.CurrentState != StateReady || resolved.Item.ApprovalStatus != "approved" {
		t.Fatalf("resolved item = %#v", resolved.Item)
	}
	if len(runner.bindingRequests) != 2 {
		t.Fatalf("approval binding requests = %#v, want selection preflight and recorded workflow approval", runner.bindingRequests)
	}
}

func TestAutomationSelectionCannotApproveConfigurePlaceholder(t *testing.T) {
	runner := &selectingTaskRunner{
		bindingTaskRunner: &bindingTaskRunner{fakeTaskRunner: &fakeTaskRunner{}},
	}
	service := NewServiceWithTaskRunner(newFakeWorkflowRepo(), runner)
	record, err := service.Intake(IntakeRequest{OwnerIdentity: "alice", Input: "Run a local code deployment task."})
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}
	var selection models.WorkflowProposal
	for _, proposal := range record.Proposals {
		if isAutomationSelectionProposal(&proposal) {
			selection = proposal
			break
		}
	}
	if selection.ID == uuid.Nil {
		t.Fatal("configure-automation proposal was not created")
	}
	_, err = service.ResolveProposal(record.Item.ID, selection.ID, ProposalResolutionRequest{
		Status:         "approved",
		SelectedOption: strings.TrimSpace(selection.Options),
		Actor:          "alice",
	})
	if err == nil || !strings.Contains(err.Error(), "configure a suitable automation") {
		t.Fatalf("placeholder approval error = %v", err)
	}
	stored, getErr := service.Get(record.Item.ID)
	if getErr != nil || stored.Item.AutomationID != "" || stored.Item.ApprovalStatus == "approved" {
		t.Fatalf("placeholder approval mutated workflow: record=%#v err=%v", stored, getErr)
	}
}

func TestOwnerScopedIntakeDoesNotReuseForeignOrLegacySourceWorkflows(t *testing.T) {
	repo := newFakeWorkflowRepo()
	service := NewService(repo)

	bob, err := service.Intake(IntakeRequest{
		OwnerIdentity: "bob",
		Input:         "Review Bob's connector message.",
		SourceType:    "connected_source",
		SourceID:      "shared-message-id",
		SourceURI:     "source://mail/shared-message-id",
	})
	if err != nil {
		t.Fatalf("bob Intake: %v", err)
	}
	alice, err := service.Intake(IntakeRequest{
		OwnerIdentity: "alice",
		Input:         "Review Alice's connector message.",
		SourceType:    "connected_source",
		SourceID:      "shared-message-id",
		SourceURI:     "source://mail/shared-message-id",
	})
	if err != nil {
		t.Fatalf("alice Intake: %v", err)
	}
	if alice.Item.ID == bob.Item.ID || alice.Item.OwnerIdentity != "alice" {
		t.Fatalf("Alice intake reused Bob's source workflow: alice=%#v bob=%#v", alice.Item, bob.Item)
	}

	legacy, err := service.Intake(IntakeRequest{
		Input:      "Review the ownerless imported source.",
		SourceType: "connected_source",
		SourceID:   "legacy-message-id",
		SourceURI:  "source://mail/legacy-message-id",
	})
	if err != nil {
		t.Fatalf("legacy Intake: %v", err)
	}
	owned, err := service.Intake(IntakeRequest{
		OwnerIdentity: "alice",
		Input:         "Review Alice's version of the imported source.",
		SourceType:    "connected_source",
		SourceID:      "legacy-message-id",
		SourceURI:     "source://mail/legacy-message-id",
	})
	if err != nil {
		t.Fatalf("owned Intake: %v", err)
	}
	if owned.Item.ID == legacy.Item.ID || owned.Item.OwnerIdentity != "alice" {
		t.Fatalf("authenticated intake adopted ownerless workflow: owned=%#v legacy=%#v", owned.Item, legacy.Item)
	}
	if _, err := service.GetForOwner("alice", legacy.Item.ID); err == nil {
		t.Fatal("authenticated owner could read an ownerless legacy workflow")
	}

	storedBob, err := repo.FindItem(bob.Item.ID)
	if err != nil || storedBob.OwnerIdentity != "bob" || storedBob.Archived {
		t.Fatalf("foreign workflow changed by Alice intake: item=%#v err=%v", storedBob, err)
	}
}

func TestOwnerScopedIntakeDoesNotReuseForeignSourceURI(t *testing.T) {
	service := NewService(newFakeWorkflowRepo())
	bob, err := service.Intake(IntakeRequest{
		OwnerIdentity: "bob",
		Input:         "Review Bob's URI-only source.",
		SourceURI:     "source://calendar/shared-event",
	})
	if err != nil {
		t.Fatalf("bob Intake: %v", err)
	}
	alice, err := service.Intake(IntakeRequest{
		OwnerIdentity: "alice",
		Input:         "Review Alice's URI-only source.",
		SourceURI:     "source://calendar/shared-event",
	})
	if err != nil {
		t.Fatalf("alice Intake: %v", err)
	}
	if alice.Item.ID == bob.Item.ID || alice.Item.OwnerIdentity != "alice" {
		t.Fatalf("Alice URI intake reused Bob's workflow: alice=%#v bob=%#v", alice.Item, bob.Item)
	}
}

func TestGetIncludesLinkedPursuitContext(t *testing.T) {
	repo := newFakeWorkflowRepo()
	service := NewService(repo)
	record, err := service.Intake(IntakeRequest{
		Input:      "Collect ASR insurance claim documents and prepare evidence bundle.",
		ProjectKey: "ASR claim",
	})
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}
	pursuitID := uuid.New()
	linkID := uuid.New()
	repo.pursuits[record.Item.ID] = []WorkflowPursuitContext{
		{
			ID:                    pursuitID,
			Title:                 "Finish ASR insurance claim",
			Status:                "active",
			RiskLevel:             "medium",
			PriorityScore:         83,
			AutonomyLevel:         "approve_before_execute",
			WhyItMatters:          "A source-linked claim prevents an unsupported insurance response.",
			DesiredOutcome:        "Complete the claim with source-linked evidence.",
			CurrentStateSummary:   "Evidence collection is in progress.",
			NextRecommendedAction: "Ask Robert to approve the missing-document request.",
			CompletionState:       "open",
			LinkID:                linkID,
			Relationship:          "operational_work",
			SourceURI:             "pursuit://asr-claim",
			SourceLabel:           "ASR claim pursuit",
			LinkConfidence:        0.9,
		},
	}

	detail, err := service.Get(record.Item.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(detail.Pursuits) != 1 {
		t.Fatalf("pursuits = %d, want 1", len(detail.Pursuits))
	}
	got := detail.Pursuits[0]
	if got.ID != pursuitID || got.LinkID != linkID || got.Title != "Finish ASR insurance claim" {
		t.Fatalf("unexpected pursuit context: %#v", got)
	}
	if got.NextRecommendedAction == "" || got.WhyItMatters == "" || got.Relationship != "operational_work" {
		t.Fatalf("pursuit context lost rationale, next action, or relationship: %#v", got)
	}
}

func TestOwnerScopedWorkflowViewsHideForeignWorkAndLegacyPursuits(t *testing.T) {
	repo := newFakeWorkflowRepo()
	service := NewService(repo)
	alice, err := service.Intake(IntakeRequest{
		OwnerIdentity: "alice",
		Input:         "Draft and send a legal reply about Alice's case.",
	})
	if err != nil {
		t.Fatalf("alice Intake: %v", err)
	}
	bob, err := service.Intake(IntakeRequest{
		OwnerIdentity: "bob",
		Input:         "Draft and send a legal reply about Bob's case.",
	})
	if err != nil {
		t.Fatalf("bob Intake: %v", err)
	}
	repo.pursuits[alice.Item.ID] = []WorkflowPursuitContext{
		{ID: uuid.New(), OwnerIdentity: "alice", Title: "Alice legal pursuit", Relationship: "operational_work"},
		{ID: uuid.New(), OwnerIdentity: "bob", Title: "Bob legacy pursuit", Relationship: "legacy_import"},
	}

	items, err := service.ItemsForOwner("alice", false)
	if err != nil {
		t.Fatalf("ItemsForOwner: %v", err)
	}
	if len(items) != 1 || items[0].ID != alice.Item.ID {
		t.Fatalf("alice items = %#v, want only Alice workflow", items)
	}
	dashboard, err := service.DashboardForOwner("alice")
	if err != nil {
		t.Fatalf("DashboardForOwner: %v", err)
	}
	if dashboard.Counts["total"] != 1 || dashboard.Counts["approvals"] != 1 {
		t.Fatalf("alice dashboard counts = %#v, want only Alice workflow", dashboard.Counts)
	}
	detail, err := service.GetForOwner("alice", alice.Item.ID)
	if err != nil {
		t.Fatalf("GetForOwner alice: %v", err)
	}
	if len(detail.Pursuits) != 1 || detail.Pursuits[0].Title != "Alice legal pursuit" {
		t.Fatalf("owner detail exposed foreign pursuit context: %#v", detail.Pursuits)
	}
	if _, err := service.GetForOwner("alice", bob.Item.ID); err == nil {
		t.Fatalf("GetForOwner exposed Bob workflow to Alice")
	}
}

func TestDashboardSurfacesQueuesAndRules(t *testing.T) {
	repo := newFakeWorkflowRepo()
	service := NewService(repo)
	legal, err := service.Intake(IntakeRequest{Input: "Email from lawyer about Vivare hearing tomorrow. Draft reply only."})
	if err != nil {
		t.Fatalf("Intake legal: %v", err)
	}
	admin, err := service.Intake(IntakeRequest{Input: "Create Trello checklist for low risk admin work"})
	if err != nil {
		t.Fatalf("Intake admin: %v", err)
	}
	blocked, err := service.Intake(IntakeRequest{Input: "Need access, missing login credentials for local folder source."})
	if err != nil {
		t.Fatalf("Intake blocked: %v", err)
	}

	dashboard, err := service.Dashboard()
	if err != nil {
		t.Fatalf("Dashboard: %v", err)
	}
	if dashboard.Counts["approvals"] == 0 {
		t.Fatalf("expected approval count, got %#v", dashboard.Counts)
	}
	if dashboard.Counts["ready"] == 0 {
		t.Fatalf("expected ready count for %s, got %#v", admin.Item.ID, dashboard.Counts)
	}
	if dashboard.Counts["blocked"] == 0 {
		t.Fatalf("expected blocked count for %s, got %#v", blocked.Item.ID, dashboard.Counts)
	}
	if len(dashboard.Rules) < 10 {
		t.Fatalf("rules = %d, want default rulebook", len(dashboard.Rules))
	}
	if len(dashboard.HighRiskItems) == 0 || dashboard.HighRiskItems[0].ID != legal.Item.ID {
		t.Fatalf("expected high-risk legal item in dashboard")
	}
}

func TestTransitionRequiresApprovalFromNeedsApprovalToReady(t *testing.T) {
	repo := newFakeWorkflowRepo()
	service := NewService(repo)
	record, err := service.Intake(IntakeRequest{Input: "Publish public Medium article from Trello card"})
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}

	if _, err := service.Transition(record.Item.ID, TransitionRequest{TargetState: StateReady}); err == nil {
		t.Fatalf("expected transition without approval to fail")
	}
	if _, err := service.Transition(record.Item.ID, TransitionRequest{TargetState: StateReady, Approved: true, Message: "Robert approved draft-only workflow"}); err == nil {
		t.Fatal("generic transition must not resolve approval")
	}
	approved, err := service.ResolveApproval(record.Item.ID, ApprovalResolutionRequest{Approved: true, Note: "Robert approved draft-only workflow"})
	if err != nil {
		t.Fatalf("ResolveApproval: %v", err)
	}
	if approved.Item.CurrentState != StateReady {
		t.Fatalf("state = %q, want ready", approved.Item.CurrentState)
	}
	if approved.Item.ApprovalStatus != "approved" {
		t.Fatalf("approval status = %q, want approved", approved.Item.ApprovalStatus)
	}
	if len(approved.Events) < 2 {
		t.Fatalf("expected transition audit event")
	}
}

func TestApprovalResolutionRejectsBlockedApprovalWorkflow(t *testing.T) {
	repo := newFakeWorkflowRepo()
	service := NewService(repo)
	record, err := service.Intake(IntakeRequest{Input: "Email from lawyer about legal hearing. Draft formal reply."})
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}
	item := record.Item
	item.CurrentState = StateBlocked
	item.RequiresApproval = true
	item.ApprovalStatus = "pending"
	if _, err := repo.UpdateItem(&item); err != nil {
		t.Fatalf("UpdateItem: %v", err)
	}

	if _, err := service.Transition(record.Item.ID, TransitionRequest{TargetState: StateReady}); err == nil {
		t.Fatalf("expected blocked approval-required workflow to require approval before ready")
	}
	if _, err := service.Transition(record.Item.ID, TransitionRequest{TargetState: StateReady, Approved: true, Message: "Robert approved controlled draft preparation"}); err == nil {
		t.Fatal("generic transition must not resolve approval")
	}
	if _, err := service.ResolveApproval(record.Item.ID, ApprovalResolutionRequest{Approved: true, Note: "Robert approved controlled draft preparation"}); err == nil {
		t.Fatal("approval resolution must reject an item outside the pending approval state")
	}
	updated, err := repo.FindItem(record.Item.ID)
	if err != nil {
		t.Fatalf("FindItem: %v", err)
	}
	if updated.CurrentState != StateBlocked || updated.ApprovalStatus != "pending" {
		t.Fatalf("state/status = %q/%q, want blocked/pending unchanged", updated.CurrentState, updated.ApprovalStatus)
	}
}

func TestResolveApprovalRejectsNonPendingAndTerminalItems(t *testing.T) {
	cases := []struct {
		name             string
		state            string
		approvalStatus   string
		requiresApproval bool
		archived         bool
	}{
		{name: "completed", state: StateCompleted, approvalStatus: "pending", requiresApproval: true},
		{name: "archived state", state: StateArchived, approvalStatus: "pending", requiresApproval: true, archived: true},
		{name: "archived flag", state: StateNeedsApproval, approvalStatus: "pending", requiresApproval: true, archived: true},
		{name: "already approved", state: StateReady, approvalStatus: "approved", requiresApproval: true},
		{name: "already rejected", state: StateBlocked, approvalStatus: "rejected", requiresApproval: true},
		{name: "blocked despite pending status", state: StateBlocked, approvalStatus: "pending", requiresApproval: true},
		{name: "approval not required", state: StateNeedsApproval, approvalStatus: "pending"},
		{name: "missing approval status", state: StateNeedsApproval, requiresApproval: true},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			repo := newFakeWorkflowRepo()
			service := NewService(repo)
			record, err := service.Intake(IntakeRequest{Input: "Email from lawyer about legal hearing. Draft formal reply."})
			if err != nil {
				t.Fatalf("Intake: %v", err)
			}
			item := record.Item
			item.CurrentState = testCase.state
			item.ApprovalStatus = testCase.approvalStatus
			item.RequiresApproval = testCase.requiresApproval
			item.Archived = testCase.archived
			if _, err := repo.UpdateItem(&item); err != nil {
				t.Fatalf("UpdateItem: %v", err)
			}
			before, err := repo.FindItem(item.ID)
			if err != nil {
				t.Fatalf("FindItem before resolution: %v", err)
			}
			transitionCount := len(repo.transitions[item.ID])
			approvalDecisionCount := countApprovalDecisions(repo.decisions[item.ID])

			if _, err := service.ResolveApproval(item.ID, ApprovalResolutionRequest{Approved: true, Note: "duplicate or stale approval"}); err == nil {
				t.Fatal("ResolveApproval succeeded for a non-pending or terminal workflow")
			}
			after, err := repo.FindItem(item.ID)
			if err != nil {
				t.Fatalf("FindItem after resolution: %v", err)
			}
			if after.CurrentState != before.CurrentState || after.ApprovalStatus != before.ApprovalStatus ||
				after.RequiresApproval != before.RequiresApproval || after.Archived != before.Archived {
				t.Fatalf("rejected resolution mutated approval state: before=%#v after=%#v", before, after)
			}
			if len(repo.transitions[item.ID]) != transitionCount || countApprovalDecisions(repo.decisions[item.ID]) != approvalDecisionCount {
				t.Fatal("rejected resolution appended approval transitions or decisions")
			}
		})
	}
}

func TestConcurrentResolveApprovalAllowsOnlyOneResolution(t *testing.T) {
	baseRepo := newFakeWorkflowRepo()
	barrierRepo := &concurrentApprovalWorkflowRepo{
		fakeWorkflowRepo: baseRepo,
		arrivals:         make(chan struct{}, 2),
		release:          make(chan struct{}),
	}
	service := NewService(barrierRepo)
	record, err := service.Intake(IntakeRequest{Input: "Email from lawyer about legal hearing. Draft formal reply."})
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}
	transitionCount := len(baseRepo.transitions[record.Item.ID])
	approvalDecisionCount := countApprovalDecisions(baseRepo.decisions[record.Item.ID])

	type result struct {
		record *WorkflowRecord
		err    error
	}
	results := make(chan result, 2)
	var wait sync.WaitGroup
	for range 2 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			resolved, resolveErr := service.ResolveApproval(record.Item.ID, ApprovalResolutionRequest{
				Approved: true,
				Note:     "approve this pending workflow",
				Actor:    "robert",
			})
			results <- result{record: resolved, err: resolveErr}
		}()
	}
	for range 2 {
		<-barrierRepo.arrivals
	}
	close(barrierRepo.release)
	wait.Wait()
	close(results)

	successes := 0
	failures := 0
	for resolution := range results {
		if resolution.err == nil {
			successes++
			if resolution.record == nil || resolution.record.Item.CurrentState != StateReady || resolution.record.Item.ApprovalStatus != "approved" {
				t.Errorf("successful resolution returned invalid record: %#v", resolution.record)
			}
		} else {
			failures++
			if !strings.Contains(resolution.err.Error(), "no longer pending") {
				t.Errorf("losing resolution error = %v, want stale pending-state rejection", resolution.err)
			}
		}
	}
	if successes != 1 || failures != 1 {
		t.Fatalf("concurrent resolutions: successes=%d failures=%d, want exactly one winner and one stale rejection", successes, failures)
	}
	stored, err := baseRepo.FindItem(record.Item.ID)
	if err != nil {
		t.Fatalf("FindItem after concurrent resolution: %v", err)
	}
	if stored.CurrentState != StateReady || stored.ApprovalStatus != "approved" {
		t.Fatalf("final approval state = %q/%q, want ready/approved", stored.CurrentState, stored.ApprovalStatus)
	}
	if countApprovalResolutionTransitions(baseRepo.transitions[record.Item.ID]) != 1 ||
		countApprovalDecisions(baseRepo.decisions[record.Item.ID]) != approvalDecisionCount+1 ||
		len(baseRepo.transitions[record.Item.ID]) != transitionCount+1 {
		t.Fatalf("concurrent resolution appended duplicate approval records: transitions=%#v decisions=%#v", baseRepo.transitions[record.Item.ID], baseRepo.decisions[record.Item.ID])
	}
}

func countApprovalDecisions(decisions []models.WorkflowDecision) int {
	count := 0
	for _, decision := range decisions {
		if decision.DecisionType == "approval" {
			count++
		}
	}
	return count
}

func countApprovalResolutionTransitions(transitions []models.WorkflowTransition) int {
	count := 0
	for _, transition := range transitions {
		if transition.Trigger == "approval_resolution" {
			count++
		}
	}
	return count
}

func TestTransitionRejectsManualCompletion(t *testing.T) {
	repo := newFakeWorkflowRepo()
	service := NewService(repo)
	record, err := service.Intake(IntakeRequest{Input: "Create Trello checklist for low risk admin work"})
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}

	if _, err := service.Transition(record.Item.ID, TransitionRequest{TargetState: StateCompleted, Message: "looks done"}); err == nil {
		t.Fatalf("expected manual completion to be rejected")
	}
}

func TestRunDueSkipsReadyApprovalWorkflowWithoutApproval(t *testing.T) {
	repo := newFakeWorkflowRepo()
	runner := &fakeTaskRunner{result: &TaskRunResult{
		PlanID:             "plan-approval",
		CompletionStatus:   "validated",
		VerificationStatus: "verified",
		Output:             "completed",
		Passed:             true,
	}}
	service := NewServiceWithTaskRunner(repo, runner)
	record, err := service.Intake(IntakeRequest{Input: "Email from lawyer about legal hearing. Draft formal reply."})
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}
	item := record.Item
	item.CurrentState = StateReady
	item.RequiresApproval = true
	item.ApprovalStatus = "pending"
	if _, err := repo.UpdateItem(&item); err != nil {
		t.Fatalf("UpdateItem: %v", err)
	}

	summary, err := service.RunDue(RunDueRequest{Limit: 5})
	if err != nil {
		t.Fatalf("RunDue: %v", err)
	}
	if summary.Checked != 0 {
		t.Fatalf("checked = %d, want 0 for unapproved ready workflow", summary.Checked)
	}
	if len(runner.requests) != 0 {
		t.Fatalf("runner requests = %d, want 0", len(runner.requests))
	}
}

func TestResolveProposalApprovesWorkflow(t *testing.T) {
	repo := newFakeWorkflowRepo()
	service := NewService(repo)
	record, err := service.Intake(IntakeRequest{Input: "Publish public Medium article from Trello card"})
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}
	approved, err := service.ResolveProposal(record.Item.ID, record.Proposals[0].ID, ProposalResolutionRequest{
		Approved:       true,
		SelectedOption: "Approve draft-only workflow",
		Note:           "Robert approved draft preparation only.",
		Actor:          "Robert",
	})
	if err != nil {
		t.Fatalf("ResolveProposal: %v", err)
	}
	if approved.Item.CurrentState != StateReady {
		t.Fatalf("state = %q, want ready", approved.Item.CurrentState)
	}
	if approved.Item.ApprovalStatus != "approved" {
		t.Fatalf("approval status = %q, want approved", approved.Item.ApprovalStatus)
	}
	if approved.Proposals[0].Status != "approved" {
		t.Fatalf("proposal status = %q, want approved", approved.Proposals[0].Status)
	}
}

func TestResolveProposalDefaultsToChangesRequested(t *testing.T) {
	repo := newFakeWorkflowRepo()
	service := NewService(repo)
	record, err := service.Intake(IntakeRequest{Input: "Create Trello checklist for low risk admin work"})
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}
	updated, err := service.ResolveProposal(record.Item.ID, record.Proposals[0].ID, ProposalResolutionRequest{
		Note:  "Needs more detail before execution.",
		Actor: "Robert",
	})
	if err != nil {
		t.Fatalf("ResolveProposal: %v", err)
	}
	if updated.Proposals[0].Status != "changes_requested" {
		t.Fatalf("proposal status = %q, want changes_requested", updated.Proposals[0].Status)
	}
	if updated.Item.CurrentState != StateWaitingInput {
		t.Fatalf("state = %q, want waiting_external_input", updated.Item.CurrentState)
	}
}

func TestResolveProposalChangesRequestedStoresFeedbackLesson(t *testing.T) {
	repo := newFakeWorkflowRepo()
	mem := &fakeWorkflowMemoryService{}
	service := NewServiceWithMemory(repo, mem)
	record, err := service.Intake(IntakeRequest{
		Input:      "Create Trello checklist for client quote workflow",
		ProjectKey: "garden-ops",
		SourceType: "trello",
		SourceURI:  "trello://card/quote",
	})
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}

	_, err = service.ResolveProposal(record.Item.ID, record.Proposals[0].ID, ProposalResolutionRequest{
		Note:  "Make future checklists include customer address, parking, access, before photos, and after photos.",
		Actor: "Robert",
	})
	if err != nil {
		t.Fatalf("ResolveProposal: %v", err)
	}

	if len(mem.created) != 1 {
		t.Fatalf("created memories = %d, want 1", len(mem.created))
	}
	created := mem.created[0]
	if created.Kind != "lesson" || created.ProjectKey != "garden-ops" {
		t.Fatalf("memory = %#v, want lesson for project", created)
	}
	if !strings.Contains(created.Content, "customer address") || !strings.Contains(created.Content, "Future behavior") {
		t.Fatalf("memory content does not preserve correction and future behavior: %q", created.Content)
	}
	if !strings.Contains(strings.Join(created.Tags, ","), "workflow-feedback") {
		t.Fatalf("tags = %#v, want workflow-feedback", created.Tags)
	}
}

func TestResolveApprovalGenericRejectionDoesNotStoreNoise(t *testing.T) {
	repo := newFakeWorkflowRepo()
	mem := &fakeWorkflowMemoryService{}
	service := NewServiceWithMemory(repo, mem)
	record, err := service.Intake(IntakeRequest{
		Input:      "Email from lawyer about Vivare legal hearing tomorrow. Draft formal reply.",
		ProjectKey: "Vivare dispute",
	})
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}

	if _, err := service.ResolveApproval(record.Item.ID, ApprovalResolutionRequest{Approved: false, Note: "rejected", Actor: "Robert"}); err != nil {
		t.Fatalf("ResolveApproval: %v", err)
	}
	if len(mem.created) != 0 {
		t.Fatalf("created memories = %d, want 0 for generic rejection", len(mem.created))
	}
}

func TestResolveApprovalReturnsErrorWhenAtomicAuditPersistenceFails(t *testing.T) {
	repo := newFakeWorkflowRepo()
	service := NewService(repo)
	record, err := service.Intake(IntakeRequest{Input: "Email from lawyer about legal hearing. Draft formal reply."})
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}
	beforeTransitions := len(repo.transitions[record.Item.ID])
	beforeEvents := len(repo.events[record.Item.ID])
	beforeDecisions := len(repo.decisions[record.Item.ID])
	repo.resolvePendingApprovalErr = fmt.Errorf("workflow approval event store unavailable")

	if _, err := service.ResolveApproval(record.Item.ID, ApprovalResolutionRequest{
		Approved: true, Actor: "alice", Note: "Approve the draft-only workflow.",
	}); err == nil || !strings.Contains(err.Error(), "event store unavailable") {
		t.Fatalf("ResolveApproval error = %v, want audit-persistence failure", err)
	}
	stored := repo.items[record.Item.ID]
	if stored.CurrentState != StateNeedsApproval || stored.ApprovalStatus != "pending" {
		t.Fatalf("workflow state/status = %q/%q, want pending approval after failed transaction", stored.CurrentState, stored.ApprovalStatus)
	}
	if len(repo.transitions[record.Item.ID]) != beforeTransitions ||
		len(repo.events[record.Item.ID]) != beforeEvents ||
		len(repo.decisions[record.Item.ID]) != beforeDecisions {
		t.Fatalf("audit records changed on failed approval: transitions=%d events=%d decisions=%d", len(repo.transitions[record.Item.ID])-beforeTransitions, len(repo.events[record.Item.ID])-beforeEvents, len(repo.decisions[record.Item.ID])-beforeDecisions)
	}
}

func TestResolveApprovalApprovedNoteStoresSpecificLearning(t *testing.T) {
	repo := newFakeWorkflowRepo()
	mem := &fakeWorkflowMemoryService{}
	service := NewServiceWithMemory(repo, mem)
	record, err := service.Intake(IntakeRequest{
		Input:      "Email from lawyer about Vivare legal hearing tomorrow. Draft formal reply.",
		ProjectKey: "Vivare dispute",
	})
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}

	_, err = service.ResolveApproval(record.Item.ID, ApprovalResolutionRequest{
		Approved: true,
		Note:     "For future lawyer emails, keep replies formal Dutch, evidence-linked, and never send automatically.",
		Actor:    "Robert",
	})
	if err != nil {
		t.Fatalf("ResolveApproval: %v", err)
	}

	if len(mem.created) != 1 {
		t.Fatalf("created memories = %d, want 1", len(mem.created))
	}
	created := mem.created[0]
	if created.Kind != "lesson" || created.ProjectKey != "Vivare dispute" {
		t.Fatalf("memory = %#v, want project lesson", created)
	}
	if !strings.Contains(created.Content, "formal Dutch") || !strings.Contains(created.Content, "approval_approved") {
		t.Fatalf("memory content did not preserve approval feedback: %q", created.Content)
	}
	if !strings.Contains(strings.Join(created.Tags, ","), "approval_approved") {
		t.Fatalf("tags = %#v, want approval_approved", created.Tags)
	}
}

func TestResolveInterruptedExecutionStoresReviewLesson(t *testing.T) {
	repo := newFakeWorkflowRepo()
	mem := &fakeWorkflowMemoryService{}
	service := NewServiceWithMemory(repo, mem)
	record := recoverInterruptedWorkflow(t, repo, service, "Create Trello checklist for low risk admin work")

	_, err := service.ResolveInterruptedExecution(record.Item.ID, InterruptedExecutionResolutionRequest{
		Decision:                 "retry",
		Note:                     "Before retrying script-based tasks, check target logs and confirm no duplicate Trello card was created.",
		EvidenceURI:              "https://trello.example/card/reconciliation",
		PriorExecutionReconciled: true,
		Actor:                    "Robert",
	})
	if err != nil {
		t.Fatalf("ResolveInterruptedExecution: %v", err)
	}

	if len(mem.created) != 1 {
		t.Fatalf("created memories = %d, want 1", len(mem.created))
	}
	created := mem.created[0]
	if !strings.Contains(created.Content, "duplicate Trello card") || !strings.Contains(created.Content, "interruption_retry") {
		t.Fatalf("memory content did not capture interrupted execution review: %q", created.Content)
	}
	if !strings.Contains(strings.Join(created.Tags, ","), "interruption_retry") {
		t.Fatalf("tags = %#v, want interruption_retry", created.Tags)
	}
}

func TestChecklistBlockedStoresFeedbackLesson(t *testing.T) {
	repo := newFakeWorkflowRepo()
	mem := &fakeWorkflowMemoryService{}
	service := NewServiceWithMemory(repo, mem)
	record, err := service.Intake(IntakeRequest{
		Input:      "Create Trello checklist for client quote workflow",
		ProjectKey: "garden-ops",
	})
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}

	_, err = service.UpdateChecklistItem(record.Item.ID, record.Checklist[0].ID, ChecklistUpdateRequest{
		Status: "blocked",
		Note:   "This step is blocked until the supplier access code is available; ask Robert first next time.",
		Actor:  "Robert",
	})
	if err != nil {
		t.Fatalf("UpdateChecklistItem: %v", err)
	}

	if len(mem.created) != 1 {
		t.Fatalf("created memories = %d, want 1", len(mem.created))
	}
	created := mem.created[0]
	if !strings.Contains(created.Content, "supplier access code") || !strings.Contains(created.Content, "checklist_blocked") {
		t.Fatalf("memory content did not capture checklist correction: %q", created.Content)
	}
	if !strings.Contains(strings.Join(created.Tags, ","), "checklist_blocked") {
		t.Fatalf("tags = %#v, want checklist_blocked", created.Tags)
	}
}

func TestIntakeAppliesRelevantFeedbackMemoryToFutureWorkflow(t *testing.T) {
	repo := newFakeWorkflowRepo()
	lessonID := uuid.New()
	mem := &fakeWorkflowMemoryService{
		retrieveResult: &memory.RetrieveResult{
			Explanation: "Retrieved relevant correction lesson only.",
			UsedContext: []memory.RankedMemory{
				{
					Memory: models.ContextMemory{
						ID:          lessonID,
						ProjectKey:  "garden-ops",
						Kind:        "lesson",
						Content:     "Robert corrected HAI workflow behavior. Correction: include customer address, parking, access, before photos, and after photos.",
						Summary:     "Include customer address, parking, access, before photos, and after photos in similar client quote checklists.",
						Tags:        "workflow-feedback,correction,proposal_changes_requested",
						Confidence:  0.86,
						SourceURI:   "workflow://previous-feedback",
						SourceLabel: "Prior Robert correction",
					},
					Score:       0.92,
					Explanation: "same project, checklist terms matched",
				},
			},
		},
	}
	service := NewServiceWithMemory(repo, mem)

	record, err := service.Intake(IntakeRequest{
		Input:      "Create a Trello checklist for a client quote workflow with site visit planning.",
		ProjectKey: "garden-ops",
		SourceType: "trello",
		SourceURI:  "trello://card/new-quote",
		Trigger:    "trello.sync",
	})
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}

	if len(mem.retrieveRequests) != 1 {
		t.Fatalf("retrieve requests = %d, want 1", len(mem.retrieveRequests))
	}
	request := mem.retrieveRequests[0]
	if request.ProjectKey != "garden-ops" || request.Limit != 3 {
		t.Fatalf("retrieve request = %#v, want scoped project and limit 3", request)
	}
	if !strings.Contains(request.Query, "client quote") {
		t.Fatalf("retrieve query = %q, want workflow input", request.Query)
	}
	if !hasChecklistContaining(record.Checklist, "Apply learned context", "customer address") {
		t.Fatalf("checklist %#v does not include learned customer-address correction", record.Checklist)
	}
	if !hasDecision(record.Decisions, "memory_context", "applied") {
		t.Fatalf("decisions %#v missing memory_context applied decision", record.Decisions)
	}
	if !hasSourceRelationship(record.SourceLinks, "planning_context") {
		t.Fatalf("source links %#v missing memory planning context", record.SourceLinks)
	}
	if !hasEventType(record.Events, "workflow.memory_context") {
		t.Fatalf("events %#v missing memory context audit", record.Events)
	}
}

func TestIntakeSkipsNonActionableRetrievedMemory(t *testing.T) {
	repo := newFakeWorkflowRepo()
	mem := &fakeWorkflowMemoryService{
		retrieveResult: &memory.RetrieveResult{
			Explanation: "Retrieved low-value context.",
			UsedContext: []memory.RankedMemory{
				{
					Memory: models.ContextMemory{
						ID:         uuid.New(),
						ProjectKey: "garden-ops",
						Kind:       "project",
						Content:    "General project description that should not become a learned checklist step.",
						Confidence: 0.9,
					},
					Score: 0.8,
				},
				{
					Memory: models.ContextMemory{
						ID:         uuid.New(),
						ProjectKey: "garden-ops",
						Kind:       "lesson",
						Content:    "Low confidence correction.",
						Confidence: 0.2,
					},
					Score: 0.7,
				},
			},
		},
	}
	service := NewServiceWithMemory(repo, mem)

	record, err := service.Intake(IntakeRequest{
		Input:      "Create a Trello checklist for a client quote workflow.",
		ProjectKey: "garden-ops",
	})
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}
	if hasChecklistContaining(record.Checklist, "Apply learned context", "General project description") {
		t.Fatalf("non-actionable project memory was applied to checklist: %#v", record.Checklist)
	}
	if hasDecision(record.Decisions, "memory_context", "applied") {
		t.Fatalf("memory_context decision should not be created for skipped memories: %#v", record.Decisions)
	}
}

func TestResolveProposalRejectsClosedWorkflow(t *testing.T) {
	repo := newFakeWorkflowRepo()
	runner := &fakeTaskRunner{result: &TaskRunResult{
		PlanID:             "plan-closed",
		CompletionStatus:   "validated",
		VerificationStatus: "verified",
		Output:             "completed",
		Passed:             true,
	}}
	service := NewServiceWithTaskRunner(repo, runner)
	record, err := service.Intake(IntakeRequest{Input: "Create Trello checklist for low risk admin work"})
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}
	if _, err := service.RunDue(RunDueRequest{Limit: 5}); err != nil {
		t.Fatalf("RunDue: %v", err)
	}
	if _, err := service.ResolveProposal(record.Item.ID, record.Proposals[0].ID, ProposalResolutionRequest{Approved: true}); err == nil {
		t.Fatalf("expected closed workflow proposal resolution to fail")
	}
	updated, err := service.Get(record.Item.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if updated.Item.CurrentState != StateCompleted {
		t.Fatalf("state = %q, want completed", updated.Item.CurrentState)
	}
	if updated.Proposals[0].Status != "open" {
		t.Fatalf("proposal status = %q, want unchanged open", updated.Proposals[0].Status)
	}
}

func TestRunDueOpenLoopsCreatesFollowUpProposal(t *testing.T) {
	repo := newFakeWorkflowRepo()
	service := NewService(repo)
	record, err := service.Intake(IntakeRequest{Input: "Email from lawyer about Vivare hearing tomorrow. Draft reply only."})
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}
	loops := repo.openLoops[record.Item.ID]
	if len(loops) == 0 {
		t.Fatalf("expected intake open loop")
	}
	loops[0].FollowUpAt = timePtr(time.Now().UTC().Add(-time.Hour))
	repo.openLoops[record.Item.ID] = loops

	summary, err := service.RunDueOpenLoops(RunDueRequest{Limit: 5})
	if err != nil {
		t.Fatalf("RunDueOpenLoops: %v", err)
	}
	if summary.Triggered != 1 {
		t.Fatalf("triggered = %d, want 1: %#v", summary.Triggered, summary)
	}
	updated, err := service.Get(record.Item.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(updated.Proposals) < 2 {
		t.Fatalf("proposals = %d, want follow-up proposal added", len(updated.Proposals))
	}
	if updated.OpenLoops[0].Status != "triggered" {
		t.Fatalf("open loop status = %q, want triggered", updated.OpenLoops[0].Status)
	}
	if updated.Item.CurrentState != StateNeedsApproval {
		t.Fatalf("state = %q, want needs approval for legal follow-up", updated.Item.CurrentState)
	}
}

func TestOwnerScopedOpenLoopRunLeavesForeignFollowUpsUntouched(t *testing.T) {
	repo := newFakeWorkflowRepo()
	service := NewService(repo)
	alice, err := service.Intake(IntakeRequest{
		OwnerIdentity: "alice",
		Input:         "Email from lawyer about Alice's hearing tomorrow. Draft reply only.",
	})
	if err != nil {
		t.Fatalf("alice Intake: %v", err)
	}
	bob, err := service.Intake(IntakeRequest{
		OwnerIdentity: "bob",
		Input:         "Email from lawyer about Bob's hearing tomorrow. Draft reply only.",
	})
	if err != nil {
		t.Fatalf("bob Intake: %v", err)
	}
	for _, record := range []WorkflowRecord{*alice, *bob} {
		loops := repo.openLoops[record.Item.ID]
		if len(loops) == 0 {
			t.Fatalf("workflow %s has no open loop", record.Item.ID)
		}
		loops[0].FollowUpAt = timePtr(time.Now().UTC().Add(-time.Hour))
		repo.openLoops[record.Item.ID] = loops
	}

	summary, err := service.RunDueOpenLoopsForOwner("alice", RunDueRequest{Limit: 5})
	if err != nil {
		t.Fatalf("RunDueOpenLoopsForOwner: %v", err)
	}
	if summary.Checked != 1 || summary.Triggered != 1 {
		t.Fatalf("summary = %#v, want Alice follow-up only", summary)
	}
	if repo.openLoops[bob.Item.ID][0].Status != "open" {
		t.Fatalf("Bob follow-up was changed by Alice worker run: %#v", repo.openLoops[bob.Item.ID][0])
	}
}

func TestRunDueOpenLoopsSkipsWhenEmergencyStopActive(t *testing.T) {
	t.Setenv("HAI_EMERGENCY_STOP", "true")
	repo := newFakeWorkflowRepo()
	service := NewService(repo)
	record, err := service.Intake(IntakeRequest{Input: "Email from lawyer about Vivare hearing tomorrow. Draft reply only."})
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}
	loops := repo.openLoops[record.Item.ID]
	if len(loops) == 0 {
		t.Fatalf("expected intake open loop")
	}
	loops[0].FollowUpAt = timePtr(time.Now().UTC().Add(-time.Hour))
	repo.openLoops[record.Item.ID] = loops

	summary, err := service.RunDueOpenLoops(RunDueRequest{Limit: 5})
	if err != nil {
		t.Fatalf("RunDueOpenLoops: %v", err)
	}
	if summary.Skipped != 1 || summary.Triggered != 0 {
		t.Fatalf("summary = %#v, want one skipped and no triggered loops", summary)
	}
	updated, err := service.Get(record.Item.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if updated.OpenLoops[0].Status != "open" {
		t.Fatalf("open loop status = %q, want open", updated.OpenLoops[0].Status)
	}
}

func TestRunDueOpenLoopsKeepsBlockedWorkflowBlocked(t *testing.T) {
	repo := newFakeWorkflowRepo()
	service := NewService(repo)
	record, err := service.Intake(IntakeRequest{Input: "Need access, missing login credentials for local folder source."})
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}
	loops := repo.openLoops[record.Item.ID]
	if len(loops) == 0 {
		t.Fatalf("expected intake open loop")
	}
	loops[0].FollowUpAt = timePtr(time.Now().UTC().Add(-time.Hour))
	repo.openLoops[record.Item.ID] = loops

	summary, err := service.RunDueOpenLoops(RunDueRequest{Limit: 5})
	if err != nil {
		t.Fatalf("RunDueOpenLoops: %v", err)
	}
	if summary.Triggered != 1 {
		t.Fatalf("triggered = %d, want 1: %#v", summary.Triggered, summary)
	}
	updated, err := service.Get(record.Item.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if updated.Item.CurrentState != StateBlocked {
		t.Fatalf("state = %q, want blocked", updated.Item.CurrentState)
	}
	if updated.Item.BlockedReason != "missing information or access" {
		t.Fatalf("blocked reason = %q, want original blocker", updated.Item.BlockedReason)
	}
	if updated.OpenLoops[0].Status != "triggered" {
		t.Fatalf("open loop status = %q, want triggered", updated.OpenLoops[0].Status)
	}
}

func TestRunDueOpenLoopsSkipsOpenLoopClaimedByAnotherWorker(t *testing.T) {
	repo := newFakeWorkflowRepo()
	service := NewService(repo)
	record, err := service.Intake(IntakeRequest{
		Input: "Waiting for a client response; follow up tomorrow.",
	})
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}
	if len(record.OpenLoops) == 0 {
		t.Fatalf("expected an open loop")
	}
	loops := repo.openLoops[record.Item.ID]
	loops[0].FollowUpAt = timePtr(time.Now().UTC().Add(-time.Hour))
	repo.openLoops[record.Item.ID] = loops
	claimedAt := time.Now().UTC()
	claimed, acquired, err := repo.ClaimDueOpenLoop(loops[0].ID, "test-claim", claimedAt, claimedAt.Add(time.Minute))
	if err != nil {
		t.Fatalf("ClaimDueOpenLoop: %v", err)
	}
	if !acquired || claimed == nil || claimed.Status != "processing" {
		t.Fatalf("claim = %#v, acquired = %t, want processing claim", claimed, acquired)
	}
	claimed.Status = "open"
	claimed.ClaimID = ""
	claimed.LeaseUntil = nil
	if _, err := repo.UpdateOpenLoop(claimed); err != nil {
		t.Fatalf("release test claim: %v", err)
	}
	repo.rejectOpenLoopClaims = true

	summary, err := service.RunDueOpenLoops(RunDueRequest{Limit: 5})
	if err != nil {
		t.Fatalf("RunDueOpenLoops: %v", err)
	}
	if summary.Skipped != 1 || summary.Triggered != 0 {
		t.Fatalf("summary = %#v, want one skipped and no triggered follow-up", summary)
	}
	updated, err := service.Get(record.Item.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(updated.Proposals) != len(record.Proposals) || len(updated.Checklist) != len(record.Checklist) {
		t.Fatalf("claim rejection created duplicate follow-up artifacts")
	}
}

func TestRunDueOpenLoopsReusesExistingFollowUpArtifacts(t *testing.T) {
	repo := newFakeWorkflowRepo()
	service := NewService(repo)
	record, err := service.Intake(IntakeRequest{Input: "Email from lawyer about Vivare hearing tomorrow. Draft reply only."})
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}
	loops := repo.openLoops[record.Item.ID]
	if len(loops) == 0 {
		t.Fatalf("expected an open loop")
	}
	loops[0].FollowUpAt = timePtr(time.Now().UTC().Add(-time.Hour))
	repo.openLoops[record.Item.ID] = loops
	checklistLabel := "Resolve due open loop: " + compact(loops[0].WaitingFor, 160)
	recommendedAction := "Follow-up due: " + firstNonEmpty(loops[0].NextAction, loops[0].WaitingFor)
	if _, err := repo.CreateChecklistItem(&models.WorkflowChecklistItem{
		WorkflowID: record.Item.ID,
		Label:      checklistLabel,
		Status:     "open",
	}); err != nil {
		t.Fatalf("CreateChecklistItem: %v", err)
	}
	if _, err := repo.CreateProposal(&models.WorkflowProposal{
		WorkflowID:        record.Item.ID,
		RecommendedAction: recommendedAction,
		Status:            "open",
	}); err != nil {
		t.Fatalf("CreateProposal: %v", err)
	}
	checklistCount := len(repo.checklist[record.Item.ID])
	proposalCount := len(repo.proposals[record.Item.ID])

	summary, err := service.RunDueOpenLoops(RunDueRequest{Limit: 5})
	if err != nil {
		t.Fatalf("RunDueOpenLoops: %v", err)
	}
	if summary.Triggered != 1 {
		t.Fatalf("summary = %#v, want one triggered follow-up", summary)
	}
	updated, err := service.Get(record.Item.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(updated.Checklist) != checklistCount || len(updated.Proposals) != proposalCount {
		t.Fatalf("existing follow-up artifacts were duplicated")
	}
}

func TestChecklistUpdateAuditsProgress(t *testing.T) {
	service := NewService(newFakeWorkflowRepo())
	record, err := service.Intake(IntakeRequest{Input: "Create Trello checklist for Docker build issue"})
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}
	updated, err := service.UpdateChecklistItem(record.Item.ID, record.Checklist[0].ID, ChecklistUpdateRequest{Status: "done"})
	if err != nil {
		t.Fatalf("UpdateChecklistItem: %v", err)
	}
	if updated.Checklist[0].Status != "done" {
		t.Fatalf("checklist status = %q, want done", updated.Checklist[0].Status)
	}
	if len(updated.Events) < 2 {
		t.Fatalf("expected checklist audit event")
	}
}

func TestRunDueConsumesApprovedWorkflowWithTaskRunner(t *testing.T) {
	repo := newFakeWorkflowRepo()
	selection := testFrameworkSelection("plan-1")
	runner := &fakeTaskRunner{result: &TaskRunResult{
		PlanID:                 "plan-1",
		CompletionStatus:       "validated",
		VerificationStatus:     "verified",
		Output:                 "completed",
		RuntimeEvidenceURI:     "automation-launch://11111111-1111-1111-1111-111111111111",
		RuntimeEvidenceLabel:   "script runtime",
		ExternalActionExecuted: true,
		RuntimeRouteTrace: &models.AutomationRuntimeRouteTrace{
			RuntimeID:         "openclaw",
			Intent:            "code_review",
			ExecutionMode:     "read_only",
			RiskLevel:         "medium",
			RecommendedSkills: []string{"autoreview", "gitcrawl"},
			BlockedSurfaces:   []string{"external_message_sending"},
		},
		Passed:             true,
		FrameworkSelection: &selection,
	}}
	service := NewServiceWithTaskRunner(repo, runner)
	record, err := service.Intake(IntakeRequest{Input: "Create Trello checklist for low risk admin work"})
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}
	if record.Item.CurrentState != StateReady {
		t.Fatalf("state = %q, want ready", record.Item.CurrentState)
	}
	summary, err := service.RunDue(RunDueRequest{Limit: 5})
	if err != nil {
		t.Fatalf("RunDue: %v", err)
	}
	if summary.Completed != 1 {
		t.Fatalf("completed = %d, want 1: %#v", summary.Completed, summary)
	}
	if len(summary.Results) != 1 ||
		summary.Results[0].FrameworkSelection == nil ||
		summary.Results[0].FrameworkSelection.SelectionDecisionID != selection.SelectionDecisionID {
		t.Fatalf("workflow run summary omitted framework selection provenance: %#v", summary.Results)
	}
	updated, err := service.Get(record.Item.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if updated.Item.CurrentState != StateCompleted {
		t.Fatalf("state = %q, want completed", updated.Item.CurrentState)
	}
	if updated.Item.LastTaskPlanID != "plan-1" {
		t.Fatalf("plan id = %q, want plan-1", updated.Item.LastTaskPlanID)
	}
	attestation, ok := repo.attestations[record.Item.ID]
	if !ok || attestation.TaskPlanID != "plan-1" || attestation.VerificationStatus != "verified" ||
		attestation.RecordDigest == "" || attestation.RuntimeEvidenceURI != runner.result.RuntimeEvidenceURI {
		t.Fatalf("immutable completion attestation = %#v", attestation)
	}
	if len(runner.requests) != 1 {
		t.Fatalf("runner requests = %d, want 1", len(runner.requests))
	}
	if !hasGateStatus(updated.QualityGates, "verification before completion", "passed") {
		t.Fatalf("expected verification quality gate to pass")
	}
	foundRuntimeEvidence := false
	for _, claim := range updated.Evidence {
		if claim.SourceURI == "automation-launch://11111111-1111-1111-1111-111111111111" && claim.Reliability == "controlled_runtime" {
			foundRuntimeEvidence = true
			if claim.Status != "verified" || !strings.Contains(claim.ClaimText, "script runtime") || !strings.Contains(claim.ClaimText, "runtime=openclaw") {
				t.Fatalf("runtime evidence claim = %#v", claim)
			}
		}
	}
	if !foundRuntimeEvidence {
		t.Fatalf("runtime evidence claim missing from workflow detail: %#v", updated.Evidence)
	}
	if len(updated.FrameworkSelections) != 1 || updated.FrameworkSelections[0] != selection {
		t.Fatalf("workflow detail framework selections = %#v, want %#v", updated.FrameworkSelections, selection)
	}
	foundDecision := false
	for _, decision := range updated.Decisions {
		if decision.DecisionType == frameworkSelectionDecisionType &&
			decision.Decision == selection.SelectionDecisionID &&
			strings.Contains(decision.Reason, selection.CatalogDigest) &&
			strings.Contains(decision.Reason, selection.ConstitutionDigest) {
			foundDecision = true
			break
		}
	}
	if !foundDecision {
		t.Fatalf("durable framework selection decision missing: %#v", updated.Decisions)
	}
	foundEvent := false
	for _, event := range updated.Events {
		if event.EventType == frameworkSelectionEventType &&
			event.SourceURI == "framework-selection://"+selection.SelectionDecisionID &&
			strings.Contains(event.Message, selection.SelectorAlgorithmVersion) {
			foundEvent = true
			break
		}
	}
	if !foundEvent {
		t.Fatalf("framework selection audit event missing: %#v", updated.Events)
	}
}

func TestRunDueRoutesUnsupportedCompletionVerificationToReviewAndReleasesClaim(t *testing.T) {
	repo := newFakeWorkflowRepo()
	runner := &fakeTaskRunner{result: &TaskRunResult{
		PlanID: "source-supported-plan", CompletionStatus: "validated",
		VerificationStatus: "source_supported", Passed: true,
	}}
	service := NewServiceWithTaskRunner(repo, runner)
	record, err := service.Intake(IntakeRequest{Input: "Prepare a sourced low-risk admin checklist"})
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}

	summary, err := service.RunDue(RunDueRequest{Limit: 5})
	if err != nil {
		t.Fatalf("RunDue: %v", err)
	}
	if summary.Blocked != 1 || len(summary.Results) != 1 || summary.Results[0].State != StateBlocked {
		t.Fatalf("unsupported completion verification must reach review: %#v", summary)
	}
	updated, err := service.Get(record.Item.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if updated.Item.CurrentState != StateBlocked || updated.Item.VerificationStatus != "source_supported" ||
		updated.Item.WorkerClaimID != "" || updated.Item.WorkerLeaseUntil != nil {
		t.Fatalf("review state or released worker claim is incorrect: %#v", updated.Item)
	}
	if _, exists := repo.attestations[record.Item.ID]; exists {
		t.Fatal("source-supported output must not create a completion attestation")
	}
}

func TestFrameworkSelectionProvenanceSurvivesRepositoryRoundTrip(t *testing.T) {
	repo := newFakeWorkflowRepo()
	engine := NewService(repo)
	record, err := engine.Intake(IntakeRequest{
		OwnerIdentity: "alice",
		Input:         "Create a low-risk administrative checklist.",
	})
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}
	selection := testFrameworkSelection("round-trip-plan")
	runResult := &TaskRunResult{
		PlanID:             "round-trip-plan",
		CompletionStatus:   "validated",
		VerificationStatus: "verified",
		Passed:             true,
		FrameworkSelection: &selection,
	}
	implementation, ok := engine.(*service)
	if !ok {
		t.Fatalf("unexpected workflow service implementation %T", engine)
	}
	if err := implementation.storeTaskFrameworkSelection(record.Item.ID, runResult); err != nil {
		t.Fatalf("storeTaskFrameworkSelection: %v", err)
	}
	if err := implementation.storeTaskFrameworkSelection(record.Item.ID, runResult); err != nil {
		t.Fatalf("idempotent storeTaskFrameworkSelection: %v", err)
	}

	decisions, err := repo.FindDecisions(record.Item.ID)
	if err != nil {
		t.Fatalf("FindDecisions: %v", err)
	}
	selectionDecisionCount := 0
	for _, decision := range decisions {
		if decision.DecisionType != frameworkSelectionDecisionType {
			continue
		}
		selectionDecisionCount++
		decoded, err := decodeFrameworkSelectionDecision(decision)
		if err != nil {
			t.Fatalf("decodeFrameworkSelectionDecision: %v", err)
		}
		if decoded != selection {
			t.Fatalf("repository round trip = %#v, want %#v", decoded, selection)
		}
	}
	if selectionDecisionCount != 1 {
		t.Fatalf("framework selection decision count = %d, want 1: %#v", selectionDecisionCount, decisions)
	}
	events, err := repo.FindEvents(record.Item.ID)
	if err != nil {
		t.Fatalf("FindEvents: %v", err)
	}
	selectionEventCount := 0
	for _, event := range events {
		if event.EventType == frameworkSelectionEventType &&
			event.SourceURI == "framework-selection://"+selection.SelectionDecisionID {
			selectionEventCount++
		}
	}
	if selectionEventCount != 1 {
		t.Fatalf("framework selection event count = %d, want 1: %#v", selectionEventCount, events)
	}
	detail, err := engine.GetForOwner("alice", record.Item.ID)
	if err != nil {
		t.Fatalf("GetForOwner: %v", err)
	}
	if len(detail.FrameworkSelections) != 1 || detail.FrameworkSelections[0] != selection {
		t.Fatalf("owner-scoped workflow detail = %#v", detail.FrameworkSelections)
	}
	if _, err := engine.GetForOwner("bob", record.Item.ID); err == nil {
		t.Fatalf("foreign owner could retrieve framework selection provenance")
	}
}

func TestRunDuePassesSingleOwnerPursuitToTaskRunner(t *testing.T) {
	repo := newFakeWorkflowRepo()
	runner := &fakeTaskRunner{result: &TaskRunResult{
		PlanID:             "pursuit-linked-plan",
		CompletionStatus:   "validated",
		VerificationStatus: "verified",
		Passed:             true,
	}}
	service := NewServiceWithTaskRunner(repo, runner)
	record, err := service.Intake(IntakeRequest{OwnerIdentity: "alice", Input: "Create a low-risk pursuit workflow."})
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}
	pursuitID := uuid.New()
	repo.pursuits[record.Item.ID] = []WorkflowPursuitContext{{ID: pursuitID, OwnerIdentity: "alice", Title: "Launch HAI"}}

	if _, err := service.RunDueForOwner("alice", RunDueRequest{Limit: 1}); err != nil {
		t.Fatalf("RunDueForOwner: %v", err)
	}
	if len(runner.requests) != 1 || runner.requests[0].PursuitID != pursuitID.String() {
		t.Fatalf("task runner pursuit context = %#v, want %s", runner.requests, pursuitID)
	}
}

func TestRunOneForOwnerExecutesOnlyTheSelectedReadyWorkflow(t *testing.T) {
	repo := newFakeWorkflowRepo()
	runner := &fakeTaskRunner{result: &TaskRunResult{
		PlanID:             "exact-workflow-plan",
		CompletionStatus:   "validated",
		VerificationStatus: "verified",
		Passed:             true,
	}}
	service := NewServiceWithTaskRunner(repo, runner)
	selected, err := service.Intake(IntakeRequest{OwnerIdentity: "alice", Input: "Create the selected low-risk workflow."})
	if err != nil {
		t.Fatalf("selected Intake: %v", err)
	}
	other, err := service.Intake(IntakeRequest{OwnerIdentity: "alice", Input: "Create another low-risk workflow."})
	if err != nil {
		t.Fatalf("other Intake: %v", err)
	}

	result, err := service.RunOneForOwner("alice", selected.Item.ID)
	if err != nil {
		t.Fatalf("RunOneForOwner: %v", err)
	}
	if result.WorkflowID != selected.Item.ID || result.Status != "completed" || result.State != StateCompleted {
		t.Fatalf("exact run result = %#v", result)
	}
	if len(runner.requests) != 1 || runner.requests[0].WorkflowID != selected.Item.ID.String() {
		t.Fatalf("task runner requests = %#v, want selected workflow only", runner.requests)
	}
	if repo.items[other.Item.ID].CurrentState != StateReady {
		t.Fatalf("unselected workflow state = %q, want ready", repo.items[other.Item.ID].CurrentState)
	}

	replay, err := service.RunOneForOwner("alice", selected.Item.ID)
	if err != nil {
		t.Fatalf("replay RunOneForOwner: %v", err)
	}
	if replay.Status != "skipped" || len(runner.requests) != 1 {
		t.Fatalf("completed workflow reran: result=%#v requests=%#v", replay, runner.requests)
	}
	if _, err := service.RunOneForOwner("bob", other.Item.ID); err == nil {
		t.Fatal("foreign owner could run Alice workflow")
	}
}

func TestRunDueBlocksAmbiguousOwnerPursuitLinksForReview(t *testing.T) {
	repo := newFakeWorkflowRepo()
	runner := &fakeTaskRunner{result: &TaskRunResult{
		PlanID:             "ambiguous-pursuit-plan",
		CompletionStatus:   "validated",
		VerificationStatus: "verified",
		Passed:             true,
	}}
	service := NewServiceWithTaskRunner(repo, runner)
	record, err := service.Intake(IntakeRequest{OwnerIdentity: "alice", Input: "Create a low-risk shared evidence workflow."})
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}
	repo.pursuits[record.Item.ID] = []WorkflowPursuitContext{
		{ID: uuid.New(), OwnerIdentity: "alice", Title: "First pursuit"},
		{ID: uuid.New(), OwnerIdentity: "alice", Title: "Second pursuit"},
	}

	summary, err := service.RunDueForOwner("alice", RunDueRequest{Limit: 1})
	if err != nil {
		t.Fatalf("RunDueForOwner: %v", err)
	}
	if len(runner.requests) != 0 {
		t.Fatalf("ambiguous workflow reached task runner: %#v", runner.requests)
	}
	if summary.Blocked != 1 || len(summary.Results) != 1 || summary.Results[0].VerificationStatus != "needs_review" {
		t.Fatalf("run summary = %#v, want one needs-review blocker", summary)
	}
	updated, err := service.GetForOwner("alice", record.Item.ID)
	if err != nil {
		t.Fatalf("GetForOwner: %v", err)
	}
	if updated.Item.CurrentState != StateBlocked || updated.Item.VerificationStatus != "needs_review" {
		t.Fatalf("ambiguous workflow state = %#v, want blocked needs_review", updated.Item)
	}
	if !strings.Contains(updated.Item.BlockedReason, "multiple pursuits") ||
		!strings.Contains(updated.Item.NextAction, "review task execution evidence") {
		t.Fatalf("ambiguous workflow review context = blocker %q, next action %q", updated.Item.BlockedReason, updated.Item.NextAction)
	}
}

func TestRunDuePreservesStandaloneWorkflowWithoutPursuitLinks(t *testing.T) {
	repo := newFakeWorkflowRepo()
	runner := &fakeTaskRunner{result: &TaskRunResult{
		PlanID:             "standalone-plan",
		CompletionStatus:   "validated",
		VerificationStatus: "verified",
		Passed:             true,
	}}
	service := NewServiceWithTaskRunner(repo, runner)
	if _, err := service.Intake(IntakeRequest{OwnerIdentity: "alice", Input: "Create a low-risk standalone workflow."}); err != nil {
		t.Fatalf("Intake: %v", err)
	}

	if _, err := service.RunDueForOwner("alice", RunDueRequest{Limit: 1}); err != nil {
		t.Fatalf("RunDueForOwner: %v", err)
	}
	if len(runner.requests) != 1 || runner.requests[0].PursuitID != "" {
		t.Fatalf("standalone workflow pursuit context = %#v, want one unscoped request", runner.requests)
	}
}

func TestRunDueUsesOnlyOwnerScopedPursuitLink(t *testing.T) {
	repo := newFakeWorkflowRepo()
	runner := &fakeTaskRunner{result: &TaskRunResult{
		PlanID:             "owner-scoped-plan",
		CompletionStatus:   "validated",
		VerificationStatus: "verified",
		Passed:             true,
	}}
	service := NewServiceWithTaskRunner(repo, runner)
	record, err := service.Intake(IntakeRequest{OwnerIdentity: "alice", Input: "Create a low-risk owner-scoped workflow."})
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}
	wantPursuitID := uuid.New()
	repo.pursuits[record.Item.ID] = []WorkflowPursuitContext{
		{ID: uuid.New(), OwnerIdentity: "bob", Title: "Foreign pursuit"},
		{ID: wantPursuitID, OwnerIdentity: "alice", Title: "Alice pursuit"},
	}

	if _, err := service.RunDueForOwner("alice", RunDueRequest{Limit: 1}); err != nil {
		t.Fatalf("RunDueForOwner: %v", err)
	}
	if len(runner.requests) != 1 || runner.requests[0].PursuitID != wantPursuitID.String() {
		t.Fatalf("owner-scoped pursuit context = %#v, want %s", runner.requests, wantPursuitID)
	}
}

func TestRunDueSkipsWorkflowClaimedByAnotherWorker(t *testing.T) {
	repo := newFakeWorkflowRepo()
	runner := &fakeTaskRunner{result: &TaskRunResult{
		CompletionStatus:   "validated",
		VerificationStatus: "verified",
		Passed:             true,
	}}
	service := NewServiceWithTaskRunner(repo, runner)
	record, err := service.Intake(IntakeRequest{Input: "Create Trello checklist for low risk admin work"})
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}
	repo.rejectWorkflowClaims = true

	summary, err := service.RunDue(RunDueRequest{Limit: 5})
	if err != nil {
		t.Fatalf("RunDue: %v", err)
	}
	if summary.Skipped != 1 || summary.Completed != 0 {
		t.Fatalf("summary = %#v, want one skipped and no completed workflow", summary)
	}
	if len(runner.requests) != 0 {
		t.Fatalf("task runner executed an unclaimed workflow")
	}
	updated, err := service.Get(record.Item.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if updated.Item.CurrentState != StateReady {
		t.Fatalf("state = %q, want ready after claim rejection", updated.Item.CurrentState)
	}
}

func TestRecoverStaleWorkflowClaimBlocksUnknownOutcome(t *testing.T) {
	repo := newFakeWorkflowRepo()
	service := NewService(repo)
	record, err := service.Intake(IntakeRequest{Input: "Create Trello checklist for low risk admin work"})
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}
	claimedAt := time.Now().UTC().Add(-2 * time.Minute)
	claimed, acquired, err := repo.ClaimRunnableItem(record.Item.ID, "expired-workflow-claim", claimedAt, claimedAt.Add(time.Minute))
	if err != nil {
		t.Fatalf("ClaimRunnableItem: %v", err)
	}
	if !acquired || claimed == nil {
		t.Fatalf("expected workflow claim")
	}

	summary, err := service.RecoverStaleClaims(RunDueRequest{Limit: 5})
	if err != nil {
		t.Fatalf("RecoverStaleClaims: %v", err)
	}
	if summary.WorkflowsBlocked != 1 {
		t.Fatalf("summary = %#v, want one blocked workflow", summary)
	}
	updated, err := service.Get(record.Item.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if updated.Item.CurrentState != StateBlocked {
		t.Fatalf("state = %q, want blocked", updated.Item.CurrentState)
	}
	if updated.Item.WorkerClaimID != "expired-workflow-claim" || updated.Item.WorkerLeaseUntil == nil {
		t.Fatalf("expired claim fence was not retained: %#v", updated.Item)
	}
	if updated.Item.RetryCount != 1 {
		t.Fatalf("retry count = %d, want interrupted attempt recorded", updated.Item.RetryCount)
	}
	if !strings.Contains(updated.Item.BlockedReason, "outcome is unknown") {
		t.Fatalf("blocked reason = %q, want unknown-outcome review", updated.Item.BlockedReason)
	}
	if updated.Item.RecoveryStatus != RecoveryNeedsReview {
		t.Fatalf("recovery status = %q, want needs_review", updated.Item.RecoveryStatus)
	}
	dashboard, err := service.Dashboard()
	if err != nil {
		t.Fatalf("Dashboard: %v", err)
	}
	if dashboard.Counts["interruptedReview"] != 1 {
		t.Fatalf("interrupted review count = %d, want 1", dashboard.Counts["interruptedReview"])
	}
}

func TestLostLeaseKeepsRetryFencedUntilOriginalRunnerReturns(t *testing.T) {
	repo := newFakeWorkflowRepo()
	started := make(chan struct{})
	releaseRunner := make(chan struct{})
	runner := &fakeTaskRunner{result: &TaskRunResult{
		PlanID:                 "blocked-runner",
		VerificationStatus:     "needs_review",
		ExternalActionExecuted: true,
		ReviewRequired:         true,
		FailureReason:          "external result requires reconciliation",
	}}
	runner.onRun = func(TaskRunRequest) {
		close(started)
		<-releaseRunner
	}
	service := NewServiceWithTaskRunner(repo, runner)
	record, err := service.Intake(IntakeRequest{OwnerIdentity: "alice", Input: "Create a low-risk Trello checklist."})
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}
	runDone := make(chan struct{})
	go func() {
		defer close(runDone)
		_, _ = service.RunOneForOwner("alice", record.Item.ID)
	}()
	<-started

	stored := repo.items[record.Item.ID]
	if stored == nil || stored.WorkerClaimID == "" {
		t.Fatalf("runner did not hold a claim: %#v", stored)
	}
	claimID := stored.WorkerClaimID
	stored.WorkerLeaseUntil = timePtr(time.Now().UTC().Add(-time.Minute))
	if _, err := service.RecoverStaleClaims(RunDueRequest{Limit: 5}); err != nil {
		t.Fatalf("RecoverStaleClaims: %v", err)
	}
	recovered, err := service.Get(record.Item.ID)
	if err != nil {
		t.Fatalf("Get recovered workflow: %v", err)
	}
	if recovered.Item.CurrentState != StateBlocked || recovered.Item.RecoveryStatus != RecoveryNeedsReview || recovered.Item.WorkerClaimID != claimID {
		t.Fatalf("recovery did not preserve the old execution fence: %#v", recovered.Item)
	}

	resolution := InterruptedExecutionResolutionRequest{
		Decision: "retry", Note: "External outcome checked", EvidenceURI: "https://example.test/reconciliation",
		PriorExecutionReconciled: true, Actor: "Robert",
	}
	if _, err := service.ResolveInterruptedExecution(record.Item.ID, resolution); err == nil {
		t.Fatal("retry was allowed while the original runner still held its execution fence")
	}
	if _, acquired, err := repo.ClaimRunnableItem(record.Item.ID, "overlapping-attempt", time.Now().UTC(), time.Now().UTC().Add(time.Minute)); err != nil || acquired {
		t.Fatalf("new attempt was claimable before original runner returned: acquired=%t err=%v", acquired, err)
	}

	close(releaseRunner)
	<-runDone
	finished, err := service.Get(record.Item.ID)
	if err != nil {
		t.Fatalf("Get finished workflow: %v", err)
	}
	if finished.Item.WorkerClaimID != "" || finished.Item.CurrentState != StateBlocked || finished.Item.RecoveryStatus != RecoveryNeedsReview {
		t.Fatalf("returned runner did not release only its fence while preserving review: %#v", finished.Item)
	}
	if _, err := service.ResolveInterruptedExecution(record.Item.ID, resolution); err != nil {
		t.Fatalf("resolve after runner termination and evidence: %v", err)
	}
	if _, acquired, err := repo.ClaimRunnableItem(record.Item.ID, "subsequent-attempt", time.Now().UTC(), time.Now().UTC().Add(time.Minute)); err != nil || !acquired {
		t.Fatalf("reconciled workflow was not claimable after runner returned: acquired=%t err=%v", acquired, err)
	}
}

func TestInterruptedExecutionCannotBypassReview(t *testing.T) {
	repo := newFakeWorkflowRepo()
	service := NewService(repo)
	record := recoverInterruptedWorkflow(t, repo, service, "Create Trello checklist for low risk admin work")

	if _, err := service.Transition(record.Item.ID, TransitionRequest{TargetState: StateReady}); err == nil {
		t.Fatalf("expected generic transition to reject interrupted workflow")
	}
	if _, err := service.ResolveApproval(record.Item.ID, ApprovalResolutionRequest{Approved: true, Note: "approve"}); err == nil {
		t.Fatalf("expected approval resolution to reject interrupted workflow")
	}
	if _, err := service.ResolveProposal(record.Item.ID, record.Proposals[0].ID, ProposalResolutionRequest{Approved: true}); err == nil {
		t.Fatalf("expected proposal resolution to reject interrupted workflow")
	}
}

func TestResolveInterruptedExecutionRetryMakesLowRiskWorkflowReady(t *testing.T) {
	repo := newFakeWorkflowRepo()
	service := NewService(repo)
	record := recoverInterruptedWorkflow(t, repo, service, "Create Trello checklist for low risk admin work")

	updated, err := service.ResolveInterruptedExecution(record.Item.ID, InterruptedExecutionResolutionRequest{
		Decision:                 "retry",
		Note:                     "Checked Trello and no checklist was created by the interrupted attempt.",
		EvidenceURI:              "https://trello.example/card/reconciliation",
		PriorExecutionReconciled: true,
		Actor:                    "Robert",
	})
	if err != nil {
		t.Fatalf("ResolveInterruptedExecution: %v", err)
	}
	if updated.Item.CurrentState != StateReady {
		t.Fatalf("state = %q, want ready", updated.Item.CurrentState)
	}
	if updated.Item.RecoveryStatus != RecoveryRetryConfirmed {
		t.Fatalf("recovery status = %q, want retry_confirmed", updated.Item.RecoveryStatus)
	}
	if updated.Item.BlockedReason != "" || updated.Item.LastWorkerError != "" {
		t.Fatalf("resolved retry retained active error: %#v", updated.Item)
	}
	if !hasDecision(updated.Decisions, "interrupted_execution", "retry") {
		t.Fatalf("expected interrupted-execution retry decision")
	}
}

func TestResolveInterruptedExecutionCanReconcileOrphanedClaimAfterOperatorEvidence(t *testing.T) {
	repo := newFakeWorkflowRepo()
	service := NewService(repo)
	record, err := service.Intake(IntakeRequest{Input: "Create a Trello checklist for routine office work."})
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}
	claimedAt := time.Now().UTC().Add(-2 * time.Minute)
	claimID := "worker-from-crashed-process"
	claimed, acquired, err := repo.ClaimRunnableItem(record.Item.ID, claimID, claimedAt, claimedAt.Add(time.Minute))
	if err != nil || !acquired || claimed == nil {
		t.Fatalf("ClaimRunnableItem = (%#v, %v, %v), want stale worker claim", claimed, acquired, err)
	}
	if _, err := service.RecoverStaleClaims(RunDueRequest{Limit: 5}); err != nil {
		t.Fatalf("RecoverStaleClaims: %v", err)
	}
	blocked, err := service.Get(record.Item.ID)
	if err != nil {
		t.Fatalf("Get recovered item: %v", err)
	}
	if blocked.Item.RecoveryStatus != RecoveryNeedsReview || blocked.Item.WorkerClaimID != claimID {
		t.Fatalf("recovered item lost its execution fence: %#v", blocked.Item)
	}
	if _, acquired, err := repo.ClaimRunnableItem(record.Item.ID, "overlapping-worker", time.Now().UTC(), time.Now().UTC().Add(time.Minute)); err != nil || acquired {
		t.Fatalf("new worker claimed before operator resolution: acquired=%v err=%v", acquired, err)
	}

	resolved, err := service.ResolveInterruptedExecution(record.Item.ID, InterruptedExecutionResolutionRequest{
		Decision: "retry", Note: "The prior process terminated; its provider outcome was checked and no checklist was created.",
		EvidenceURI: "https://trello.example/reconciliation/worker-from-crashed-process", PriorExecutionReconciled: true, Actor: "Robert",
	})
	if err != nil {
		t.Fatalf("ResolveInterruptedExecution: %v", err)
	}
	if resolved.Item.CurrentState != StateReady || resolved.Item.WorkerClaimID != "" || resolved.Item.WorkerLeaseUntil != nil {
		t.Fatalf("evidence-backed resolution did not atomically release the orphaned fence: %#v", resolved.Item)
	}
	if _, acquired, err := repo.ClaimRunnableItem(record.Item.ID, "reconciled-worker", time.Now().UTC(), time.Now().UTC().Add(time.Minute)); err != nil || !acquired {
		t.Fatalf("reconciled workflow was not claimable: acquired=%v err=%v", acquired, err)
	}
}

func TestResolveInterruptedExecutionRetryRequiresFreshApprovalForHighRiskWorkflow(t *testing.T) {
	repo := newFakeWorkflowRepo()
	service := NewService(repo)
	record := recoverInterruptedWorkflow(t, repo, service, "Email lawyer about legal hearing and draft formal reply.")

	updated, err := service.ResolveInterruptedExecution(record.Item.ID, InterruptedExecutionResolutionRequest{
		Decision:                 "retry",
		Note:                     "Checked sent mail and confirmed that no message was sent.",
		EvidenceURI:              "https://mail.example/reconciliation",
		PriorExecutionReconciled: true,
		Actor:                    "Robert",
	})
	if err != nil {
		t.Fatalf("ResolveInterruptedExecution: %v", err)
	}
	if updated.Item.CurrentState != StateNeedsApproval {
		t.Fatalf("state = %q, want needs_approval", updated.Item.CurrentState)
	}
	if updated.Item.ApprovalStatus != "pending" {
		t.Fatalf("approval status = %q, want pending", updated.Item.ApprovalStatus)
	}
	if updated.Item.MaxRetries <= updated.Item.RetryCount {
		t.Fatalf("operator-approved retry has no remaining attempt: %d/%d", updated.Item.RetryCount, updated.Item.MaxRetries)
	}
}

func TestInterruptedExecutionRetryCompletesThroughWorker(t *testing.T) {
	repo := newFakeWorkflowRepo()
	runner := &fakeTaskRunner{result: &TaskRunResult{
		PlanID:             "recovery-plan",
		CompletionStatus:   "validated",
		VerificationStatus: "verified",
		Output:             "verified completion after controlled retry",
		Passed:             true,
	}}
	service := NewServiceWithTaskRunner(repo, runner)
	record := recoverInterruptedWorkflow(t, repo, service, "Create Trello checklist for low risk admin work")
	retried, err := service.ResolveInterruptedExecution(record.Item.ID, InterruptedExecutionResolutionRequest{
		Decision:                 "retry",
		Note:                     "Checked Trello and confirmed the interrupted attempt created no checklist.",
		EvidenceURI:              "https://trello.example/card/reconciliation",
		PriorExecutionReconciled: true,
		Actor:                    "Robert",
	})
	if err != nil {
		t.Fatalf("ResolveInterruptedExecution: %v", err)
	}

	summary, err := service.RunDue(RunDueRequest{Limit: 5})
	if err != nil {
		t.Fatalf("RunDue: %v", err)
	}
	if summary.Completed != 1 {
		t.Fatalf("summary = %#v, want controlled retry completion", summary)
	}
	completed, err := service.Get(retried.Item.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if completed.Item.RecoveryStatus != RecoveryCompletedAfterRetry {
		t.Fatalf("recovery status = %q, want completed_after_retry", completed.Item.RecoveryStatus)
	}
}

func TestResolveInterruptedExecutionCompletionRequiresEvidence(t *testing.T) {
	repo := newFakeWorkflowRepo()
	service := NewService(repo)
	record := recoverInterruptedWorkflow(t, repo, service, "Create Trello checklist for low risk admin work")

	if _, err := service.ResolveInterruptedExecution(record.Item.ID, InterruptedExecutionResolutionRequest{
		Decision: "confirm_completed",
		Note:     "The checklist exists.",
	}); err == nil {
		t.Fatalf("expected completion without evidence URI to fail")
	}
}

func TestResolveInterruptedExecutionCompletesWithEvidence(t *testing.T) {
	repo := newFakeWorkflowRepo()
	service := NewService(repo)
	record := recoverInterruptedWorkflow(t, repo, service, "Create Trello checklist for low risk admin work")

	updated, err := service.ResolveInterruptedExecution(record.Item.ID, InterruptedExecutionResolutionRequest{
		Decision:                 "confirm_completed",
		Note:                     "Verified the expected checklist exists and contains the requested items.",
		EvidenceURI:              "https://trello.example/card/123",
		EvidenceLabel:            "Trello checklist",
		PriorExecutionReconciled: true,
		Actor:                    "Robert",
	})
	if err != nil {
		t.Fatalf("ResolveInterruptedExecution: %v", err)
	}
	if updated.Item.CurrentState != StateCompleted || updated.Item.CompletedAt == nil {
		t.Fatalf("workflow was not completed: %#v", updated.Item)
	}
	if updated.Item.RecoveryStatus != RecoveryCompletionConfirmed || updated.Item.VerificationStatus != "human_approved" {
		t.Fatalf("recovery verification not recorded: %#v", updated.Item)
	}
	if !hasSourceRelationship(updated.SourceLinks, "completion_evidence") {
		t.Fatalf("expected completion evidence source link")
	}
	if !hasEvidenceStatus(updated.Evidence, "human_approved") {
		t.Fatalf("expected human-approved evidence claim")
	}
	if !hasGateStatus(updated.QualityGates, "verification before completion", "passed") {
		t.Fatalf("expected passed completion verification gate")
	}
}

func TestResolveInterruptedExecutionKeepBlockedRecordsReview(t *testing.T) {
	repo := newFakeWorkflowRepo()
	service := NewService(repo)
	record := recoverInterruptedWorkflow(t, repo, service, "Create Trello checklist for low risk admin work")

	updated, err := service.ResolveInterruptedExecution(record.Item.ID, InterruptedExecutionResolutionRequest{
		Decision: "keep_blocked",
		Note:     "The external system is unavailable, so side effects cannot be confirmed yet.",
		Actor:    "Robert",
	})
	if err != nil {
		t.Fatalf("ResolveInterruptedExecution: %v", err)
	}
	if updated.Item.CurrentState != StateBlocked || updated.Item.RecoveryStatus != RecoveryNeedsReview {
		t.Fatalf("workflow should remain in interrupted review: %#v", updated.Item)
	}
	if updated.Item.RecoveryNote == "" {
		t.Fatalf("expected operator review note")
	}
}

func TestRecoverStaleClaimsLeavesActiveWorkflowLeaseOwned(t *testing.T) {
	repo := newFakeWorkflowRepo()
	service := NewService(repo)
	record, err := service.Intake(IntakeRequest{Input: "Create Trello checklist for low risk admin work"})
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}
	claimedAt := time.Now().UTC()
	claimed, acquired, err := repo.ClaimRunnableItem(record.Item.ID, "active-workflow-claim", claimedAt, claimedAt.Add(time.Hour))
	if err != nil {
		t.Fatalf("ClaimRunnableItem: %v", err)
	}
	if !acquired || claimed == nil {
		t.Fatalf("expected workflow claim")
	}

	summary, err := service.RecoverStaleClaims(RunDueRequest{Limit: 5})
	if err != nil {
		t.Fatalf("RecoverStaleClaims: %v", err)
	}
	if summary.Checked != 0 || summary.WorkflowsBlocked != 0 {
		t.Fatalf("summary = %#v, active lease must not be recovered", summary)
	}
	updated, err := service.Get(record.Item.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if updated.Item.CurrentState != StateInProgress || updated.Item.WorkerClaimID != "active-workflow-claim" {
		t.Fatalf("active claim changed: %#v", updated.Item)
	}
}

func TestOwnerScopedClaimRecoveryLeavesForeignWorkflowUntouched(t *testing.T) {
	repo := newFakeWorkflowRepo()
	service := NewService(repo)
	alice, err := service.Intake(IntakeRequest{OwnerIdentity: "alice", Input: "Create Alice's low-risk admin checklist."})
	if err != nil {
		t.Fatalf("alice Intake: %v", err)
	}
	bob, err := service.Intake(IntakeRequest{OwnerIdentity: "bob", Input: "Create Bob's low-risk admin checklist."})
	if err != nil {
		t.Fatalf("bob Intake: %v", err)
	}
	expiredAt := time.Now().UTC().Add(-time.Minute)
	for _, id := range []uuid.UUID{alice.Item.ID, bob.Item.ID} {
		item := repo.items[id]
		item.CurrentState = StateInProgress
		item.WorkerClaimID = "expired-" + id.String()
		item.WorkerLeaseUntil = timePtr(expiredAt)
		item.LastRunAt = timePtr(expiredAt)
	}

	summary, err := service.RecoverStaleClaimsForOwner("alice", RunDueRequest{Limit: 5})
	if err != nil {
		t.Fatalf("RecoverStaleClaimsForOwner: %v", err)
	}
	if summary.Checked != 1 || summary.WorkflowsBlocked != 1 {
		t.Fatalf("summary = %#v, want Alice claim only", summary)
	}
	if repo.items[alice.Item.ID].CurrentState != StateBlocked {
		t.Fatalf("Alice stale claim was not recovered: %#v", repo.items[alice.Item.ID])
	}
	if repo.items[bob.Item.ID].CurrentState != StateInProgress || repo.items[bob.Item.ID].WorkerClaimID == "" {
		t.Fatalf("Bob stale claim was changed by Alice recovery: %#v", repo.items[bob.Item.ID])
	}
}

func TestRecoverStaleOpenLoopClaimReopensIdempotentFollowUp(t *testing.T) {
	repo := newFakeWorkflowRepo()
	service := NewService(repo)
	record, err := service.Intake(IntakeRequest{Input: "Waiting for a client response; follow up tomorrow."})
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}
	loops := repo.openLoops[record.Item.ID]
	if len(loops) == 0 {
		t.Fatalf("expected an open loop")
	}
	loops[0].FollowUpAt = timePtr(time.Now().UTC().Add(-time.Hour))
	repo.openLoops[record.Item.ID] = loops
	claimedAt := time.Now().UTC().Add(-2 * time.Minute)
	claimed, acquired, err := repo.ClaimDueOpenLoop(loops[0].ID, "expired-open-loop-claim", claimedAt, claimedAt.Add(time.Minute))
	if err != nil {
		t.Fatalf("ClaimDueOpenLoop: %v", err)
	}
	if !acquired || claimed == nil {
		t.Fatalf("expected open-loop claim")
	}

	summary, err := service.RecoverStaleClaims(RunDueRequest{Limit: 5})
	if err != nil {
		t.Fatalf("RecoverStaleClaims: %v", err)
	}
	if summary.OpenLoopsReopened != 1 {
		t.Fatalf("summary = %#v, want one reopened open loop", summary)
	}
	updated, err := service.Get(record.Item.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if updated.OpenLoops[0].Status != "open" || updated.OpenLoops[0].ClaimID != "" || updated.OpenLoops[0].LeaseUntil != nil {
		t.Fatalf("open-loop claim was not safely reopened: %#v", updated.OpenLoops[0])
	}
}

func TestRecoverStaleClaimsMigratesLegacyUnownedRows(t *testing.T) {
	t.Setenv("WORKFLOW_CLAIM_LEASE_SECONDS", "60")
	repo := newFakeWorkflowRepo()
	service := NewService(repo)
	record, err := service.Intake(IntakeRequest{Input: "Waiting for a client response; follow up tomorrow."})
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}
	legacyTime := time.Now().UTC().Add(-2 * time.Minute)
	item := repo.items[record.Item.ID]
	item.CurrentState = StateInProgress
	item.LastRunAt = timePtr(legacyTime)
	item.WorkerClaimID = ""
	item.WorkerLeaseUntil = nil
	loops := repo.openLoops[record.Item.ID]
	if len(loops) == 0 {
		t.Fatalf("expected an open loop")
	}
	loops[0].Status = "processing"
	loops[0].ClaimID = ""
	loops[0].LeaseUntil = nil
	loops[0].UpdatedAt = legacyTime
	repo.openLoops[record.Item.ID] = loops

	summary, err := service.RecoverStaleClaims(RunDueRequest{Limit: 5})
	if err != nil {
		t.Fatalf("RecoverStaleClaims: %v", err)
	}
	if summary.WorkflowsBlocked != 1 || summary.OpenLoopsReopened != 1 {
		t.Fatalf("summary = %#v, want legacy workflow blocked and open loop reopened", summary)
	}
	updated, err := service.Get(record.Item.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if updated.Item.CurrentState != StateBlocked || updated.OpenLoops[0].Status != "open" {
		t.Fatalf("legacy rows were not recovered: item=%#v loop=%#v", updated.Item, updated.OpenLoops[0])
	}
}

func TestRunDueRecoversTaskRunnerPanicIntoReview(t *testing.T) {
	repo := newFakeWorkflowRepo()
	runner := &fakeTaskRunner{panicValue: "provider adapter crashed"}
	service := NewServiceWithTaskRunner(repo, runner)
	record, err := service.Intake(IntakeRequest{Input: "Create Trello checklist for low risk admin work"})
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}

	summary, err := service.RunDue(RunDueRequest{Limit: 5})
	if err != nil {
		t.Fatalf("RunDue: %v", err)
	}
	if summary.Blocked != 1 || summary.Retried != 0 {
		t.Fatalf("summary = %#v, want outcome-uncertain review without retry", summary)
	}
	updated, err := service.Get(record.Item.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if updated.Item.CurrentState != StateBlocked || updated.Item.NextRunAt != nil {
		t.Fatalf("state/run time = %q/%v, want non-rerunnable review block", updated.Item.CurrentState, updated.Item.NextRunAt)
	}
	if !strings.Contains(updated.Item.LastWorkerError, "panic recovered") || !strings.Contains(updated.Item.LastWorkerError, "uncertain") {
		t.Fatalf("last worker error = %q, want recovered panic and uncertain outcome", updated.Item.LastWorkerError)
	}
}

func TestRunDuePassesWorkflowOwnerToTaskRunner(t *testing.T) {
	repo := newFakeWorkflowRepo()
	runner := &fakeTaskRunner{result: &TaskRunResult{
		CompletionStatus:   "validated",
		VerificationStatus: "verified",
		Passed:             true,
	}}
	service := NewServiceWithTaskRunner(repo, runner)
	record, err := service.Intake(IntakeRequest{
		OwnerIdentity: "alice",
		Input:         "Create Trello checklist for low risk admin work",
	})
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}
	if _, err := service.RunDue(RunDueRequest{Limit: 5}); err != nil {
		t.Fatalf("RunDue: %v", err)
	}
	if len(runner.requests) != 1 {
		t.Fatalf("task runner calls = %d, want 1", len(runner.requests))
	}
	if runner.requests[0].OwnerIdentity != "alice" {
		t.Fatalf("task run owner = %q, want alice for workflow %s", runner.requests[0].OwnerIdentity, record.Item.ID)
	}
}

func TestOwnerScopedRunDueExecutesOnlyOwnedWorkflow(t *testing.T) {
	repo := newFakeWorkflowRepo()
	selection := testFrameworkSelection("owner-scoped-plan")
	runner := &fakeTaskRunner{result: &TaskRunResult{
		PlanID:             "owner-scoped-plan",
		CompletionStatus:   "validated",
		VerificationStatus: "verified",
		Passed:             true,
		FrameworkSelection: &selection,
	}}
	service := NewServiceWithTaskRunner(repo, runner)
	alice, err := service.Intake(IntakeRequest{OwnerIdentity: "alice", Input: "Create Alice's low-risk admin checklist."})
	if err != nil {
		t.Fatalf("alice Intake: %v", err)
	}
	bob, err := service.Intake(IntakeRequest{OwnerIdentity: "bob", Input: "Create Bob's low-risk admin checklist."})
	if err != nil {
		t.Fatalf("bob Intake: %v", err)
	}

	summary, err := service.RunDueForOwner("alice", RunDueRequest{Limit: 5})
	if err != nil {
		t.Fatalf("RunDueForOwner: %v", err)
	}
	if summary.Checked != 1 || summary.Completed != 1 || len(runner.requests) != 1 {
		t.Fatalf("summary=%#v requests=%#v, want Alice workflow only", summary, runner.requests)
	}
	if runner.requests[0].OwnerIdentity != "alice" {
		t.Fatalf("runner owner = %q, want alice", runner.requests[0].OwnerIdentity)
	}
	if runner.requests[0].HumanApproved || runner.requests[0].ApprovalSourceID != "" {
		t.Fatalf("low-risk workflow synthesized approval provenance: %#v", runner.requests[0])
	}
	if repo.items[alice.Item.ID].CurrentState != StateCompleted {
		t.Fatalf("Alice workflow was not completed by Alice worker run: %#v", repo.items[alice.Item.ID])
	}
	if repo.items[bob.Item.ID].CurrentState != StateReady {
		t.Fatalf("Bob workflow was executed by Alice worker run: %#v", repo.items[bob.Item.ID])
	}
	aliceDetail, err := service.GetForOwner("alice", alice.Item.ID)
	if err != nil {
		t.Fatalf("GetForOwner Alice: %v", err)
	}
	if len(aliceDetail.FrameworkSelections) != 1 ||
		aliceDetail.FrameworkSelections[0].SelectionDecisionID != selection.SelectionDecisionID {
		t.Fatalf("Alice selection provenance = %#v", aliceDetail.FrameworkSelections)
	}
	if _, err := service.GetForOwner("bob", alice.Item.ID); err == nil {
		t.Fatalf("Bob could read Alice's workflow framework selection provenance")
	}
}

func TestApprovedWorkflowPassesExactWorkflowReviewAsApprovalSource(t *testing.T) {
	repo := newFakeWorkflowRepo()
	runner := &fakeTaskRunner{result: &TaskRunResult{
		PlanID:             "approved-plan",
		CompletionStatus:   "validated",
		VerificationStatus: "verified",
		Passed:             true,
	}}
	service := NewServiceWithTaskRunner(repo, runner)
	record, err := service.Intake(IntakeRequest{
		OwnerIdentity: "alice",
		Input:         "Email the lawyer with the approved evidence summary.",
	})
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}
	if _, err := service.ResolveApproval(record.Item.ID, ApprovalResolutionRequest{
		Approved: true,
		Note:     "Approved this exact workflow action.",
		Actor:    "alice",
	}); err != nil {
		t.Fatalf("ResolveApproval: %v", err)
	}
	if _, err := service.RunDueForOwner("alice", RunDueRequest{Limit: 1}); err != nil {
		t.Fatalf("RunDueForOwner: %v", err)
	}
	if len(runner.requests) != 1 {
		t.Fatalf("runner requests = %d, want one", len(runner.requests))
	}
	request := runner.requests[0]
	if !request.HumanApproved || !strings.HasPrefix(request.ApprovalSourceID, "workflow-decision:") {
		t.Fatalf("workflow approval provenance = %#v, want durable workflow decision", request)
	}
	if request.ApprovalActorIdentity != "alice" || request.ApprovalApprovedAt == nil {
		t.Fatalf("workflow approval actor/time evidence = %#v", request)
	}
	decisionID, err := uuid.Parse(strings.TrimPrefix(request.ApprovalSourceID, "workflow-decision:"))
	if err != nil {
		t.Fatalf("approval source is not a decision UUID: %v", err)
	}
	decisions, err := repo.FindDecisions(record.Item.ID)
	if err != nil {
		t.Fatalf("FindDecisions: %v", err)
	}
	found := false
	for _, decision := range decisions {
		if decision.ID == decisionID && decision.DecisionType == "approval" && decision.Decision == "approved" && decision.Approved {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("approval source %s does not identify the durable approved decision: %#v", decisionID, decisions)
	}
}

func TestAutomationWorkflowApprovalStoresExactActionBinding(t *testing.T) {
	repo := newFakeWorkflowRepo()
	binding := "automation-action:script:" + strings.Repeat("d", 64)
	runner := &bindingTaskRunner{
		fakeTaskRunner: &fakeTaskRunner{result: &TaskRunResult{PlanID: "bound-plan", Passed: true, VerificationStatus: "verified"}},
		binding:        binding,
	}
	service := NewServiceWithTaskRunner(repo, runner)
	automationID := uuid.NewString()
	record, err := service.Intake(IntakeRequest{
		OwnerIdentity: "alice",
		Input:         "Email the lawyer with the approved evidence summary.",
		ProjectKey:    "018-hai",
		AutomationID:  automationID,
	})
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}

	approved, err := service.ResolveApproval(record.Item.ID, ApprovalResolutionRequest{
		Approved: true,
		Note:     "Approve this exact automation action.",
		Actor:    "alice",
	})
	if err != nil {
		t.Fatalf("ResolveApproval: %v", err)
	}
	if len(runner.bindingRequests) != 1 {
		t.Fatalf("approval binding requests = %d, want one", len(runner.bindingRequests))
	}
	request := runner.bindingRequests[0]
	if request.OwnerIdentity != "alice" ||
		request.WorkflowID != record.Item.ID.String() ||
		request.AutomationID != automationID ||
		request.ProjectKey != "018-hai" ||
		request.Request != record.Item.Description {
		t.Fatalf("approval binding request = %#v", request)
	}
	found := false
	for _, decision := range approved.Decisions {
		if decision.DecisionType == "approval" &&
			decision.Decision == "approved" &&
			decision.Approved &&
			decision.RuleApplied == binding {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("approved workflow decision has no exact automation binding: %#v", approved.Decisions)
	}
	if _, err := service.RunDueForOwner("alice", RunDueRequest{Limit: 1}); err != nil {
		t.Fatalf("RunDueForOwner: %v", err)
	}
	if len(runner.requests) != 1 {
		t.Fatalf("runner requests = %d, want one", len(runner.requests))
	}
	proof := runner.requests[0]
	if proof.ApprovalBindingDigest != strings.Repeat("d", 64) || proof.ApprovalActorIdentity != "alice" || proof.ApprovalApprovedAt == nil {
		t.Fatalf("exact workflow approval proof = %#v", proof)
	}
}

func TestApprovedWorkflowWithoutDurableDecisionFailsClosed(t *testing.T) {
	repo := newFakeWorkflowRepo()
	runner := &fakeTaskRunner{result: &TaskRunResult{Passed: true}}
	service := NewServiceWithTaskRunner(repo, runner)
	record, err := service.Intake(IntakeRequest{
		OwnerIdentity: "alice",
		Input:         "Create a low-risk admin checklist.",
	})
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}
	item := repo.items[record.Item.ID]
	item.ApprovalStatus = "approved"
	repo.items[record.Item.ID] = item

	summary, err := service.RunDueForOwner("alice", RunDueRequest{Limit: 1})
	if err != nil {
		t.Fatalf("RunDueForOwner: %v", err)
	}
	if summary.Blocked != 1 || len(runner.requests) != 0 {
		t.Fatalf("missing durable approval was not blocked before task execution: summary=%#v requests=%#v", summary, runner.requests)
	}
	updated, err := service.Get(record.Item.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if updated.Item.CurrentState != StateNeedsApproval || updated.Item.ApprovalStatus != "pending" {
		t.Fatalf("missing durable approval did not return to approval queue: %#v", updated.Item)
	}
}

func TestRuntimeApprovalRequirementMovesLowRiskWorkflowToApprovalQueue(t *testing.T) {
	repo := newFakeWorkflowRepo()
	runner := &fakeTaskRunner{result: &TaskRunResult{
		CompletionStatus:   "review_required",
		VerificationStatus: "needs_review",
		ReviewRequired:     true,
		ApprovalRequired:   true,
		FailureReason:      "action-bound human approval is required at the launcher boundary",
	}}
	service := NewServiceWithTaskRunner(repo, runner)
	record, err := service.Intake(IntakeRequest{
		OwnerIdentity: "alice",
		Input:         "Create a low-risk admin checklist using the configured automation.",
	})
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}
	summary, err := service.RunDueForOwner("alice", RunDueRequest{Limit: 1})
	if err != nil {
		t.Fatalf("RunDueForOwner: %v", err)
	}
	if summary.Blocked != 1 {
		t.Fatalf("summary = %#v, want one approval block", summary)
	}
	updated, err := service.Get(record.Item.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if updated.Item.CurrentState != StateNeedsApproval ||
		!updated.Item.RequiresApproval ||
		updated.Item.ApprovalStatus != "pending" {
		t.Fatalf("runtime approval requirement was not queued correctly: %#v", updated.Item)
	}
}

func TestRunDueCannotPersistResultAfterClaimOwnershipChanges(t *testing.T) {
	repo := newFakeWorkflowRepo()
	var workflowID uuid.UUID
	runner := &fakeTaskRunner{
		result: &TaskRunResult{
			CompletionStatus:   "validated",
			VerificationStatus: "verified",
			Passed:             true,
		},
		onRun: func(request TaskRunRequest) {
			repo.items[workflowID].WorkerClaimID = "replacement-worker"
		},
	}
	service := NewServiceWithTaskRunner(repo, runner)
	record, err := service.Intake(IntakeRequest{Input: "Create Trello checklist for low risk admin work"})
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}
	workflowID = record.Item.ID

	summary, err := service.RunDue(RunDueRequest{Limit: 5})
	if err != nil {
		t.Fatalf("RunDue: %v", err)
	}
	if summary.Blocked != 1 || summary.Completed != 0 {
		t.Fatalf("summary = %#v, lost claim must not complete workflow", summary)
	}
	updated, err := service.Get(record.Item.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if updated.Item.CurrentState != StateInProgress || updated.Item.WorkerClaimID != "replacement-worker" {
		t.Fatalf("stale worker overwrote replacement claim: %#v", updated.Item)
	}
}

func TestRunDueBlocksWorkflowWhenEmergencyStopActive(t *testing.T) {
	t.Setenv("HAI_EMERGENCY_STOP", "true")
	repo := newFakeWorkflowRepo()
	runner := &fakeTaskRunner{result: &TaskRunResult{Passed: true}}
	service := NewServiceWithTaskRunner(repo, runner)
	record, err := service.Intake(IntakeRequest{Input: "Create Trello checklist for low risk admin work"})
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}

	summary, err := service.RunDue(RunDueRequest{Limit: 5})
	if err != nil {
		t.Fatalf("RunDue: %v", err)
	}
	if summary.Blocked != 1 {
		t.Fatalf("blocked = %d, want 1: %#v", summary.Blocked, summary)
	}
	if len(runner.requests) != 0 {
		t.Fatalf("task runner was called while emergency stop was active")
	}
	updated, err := service.Get(record.Item.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if updated.Item.CurrentState != StateReady {
		t.Fatalf("state = %q, want ready because emergency stop should not consume the item", updated.Item.CurrentState)
	}
}

func TestRunDueBlocksWorkflowWhenPersistedEmergencyStopActive(t *testing.T) {
	restore := safety.SetEmergencyStopProvider(safety.EmergencyStopProviderFunc(func() (bool, string, error) {
		return true, "operator paused execution", nil
	}))
	defer restore()

	repo := newFakeWorkflowRepo()
	runner := &fakeTaskRunner{result: &TaskRunResult{Passed: true}}
	service := NewServiceWithTaskRunner(repo, runner)
	record, err := service.Intake(IntakeRequest{Input: "Create Trello checklist for low risk admin work"})
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}
	summary, err := service.RunDue(RunDueRequest{Limit: 5})
	if err != nil {
		t.Fatalf("RunDue: %v", err)
	}
	if summary.Blocked != 1 || len(runner.requests) != 0 {
		t.Fatalf("persisted stop did not block workflow: summary=%#v calls=%d", summary, len(runner.requests))
	}
	updated, err := service.Get(record.Item.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if updated.Item.CurrentState != StateReady {
		t.Fatalf("state = %q, want ready", updated.Item.CurrentState)
	}
}

func TestRunDueBlocksTechnicalWorkflowWhenQualityEvidenceMissing(t *testing.T) {
	repo := newFakeWorkflowRepo()
	runner := &fakeTaskRunner{result: &TaskRunResult{
		PlanID:             "plan-tech",
		CompletionStatus:   "validated",
		VerificationStatus: "verified",
		Output:             "completed",
		Passed:             true,
	}}
	service := NewServiceWithTaskRunner(repo, runner)
	record, err := service.Intake(IntakeRequest{Input: "Review GitHub developer claim that code feature is done"})
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}
	summary, err := service.RunDue(RunDueRequest{Limit: 5})
	if err != nil {
		t.Fatalf("RunDue: %v", err)
	}
	if summary.Blocked != 1 || summary.Retried != 0 {
		t.Fatalf("blocked/retried = %d/%d, want 1/0 because missing quality evidence requires review: %#v", summary.Blocked, summary.Retried, summary)
	}
	updated, err := service.Get(record.Item.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if updated.Item.CurrentState != StateBlocked {
		t.Fatalf("state = %q, want blocked for quality evidence review", updated.Item.CurrentState)
	}
	if updated.Item.LastWorkerError == "" {
		t.Fatalf("expected quality gate failure reason")
	}
	if !hasGateStatus(updated.QualityGates, "tests or build evidence", "needs_review") {
		t.Fatalf("expected tests/build gate to need review")
	}
}

func TestIntakeDoesNotQueueWorkflowWhenRequiredQualityGateWriteFails(t *testing.T) {
	repo := newFakeWorkflowRepo()
	repo.createQualityGateErr = fmt.Errorf("quality gate store unavailable")
	runner := &fakeTaskRunner{result: &TaskRunResult{
		PlanID:             "plan-gate-write-failure",
		CompletionStatus:   "validated",
		VerificationStatus: "verified",
		Passed:             true,
	}}
	service := NewServiceWithTaskRunner(repo, runner)

	if _, err := service.Intake(IntakeRequest{Input: "Create Trello checklist for low risk admin work"}); err == nil {
		t.Fatal("Intake succeeded without durably persisting required quality gates")
	}
	if len(repo.items) != 1 {
		t.Fatalf("workflow items = %d, want one persisted fail-closed item", len(repo.items))
	}
	var item *models.WorkflowItem
	for _, persisted := range repo.items {
		item = persisted
	}
	if item.CurrentState != StateBlocked || !strings.Contains(item.BlockedReason, "quality gates") {
		t.Fatalf("workflow state = %q, blocked reason = %q; want persisted quality-gate block", item.CurrentState, item.BlockedReason)
	}
	if summary, err := service.RunDue(RunDueRequest{Limit: 5}); err != nil {
		t.Fatalf("RunDue: %v", err)
	} else if summary.Completed != 0 || len(runner.requests) != 0 {
		t.Fatalf("workflow ran without required gates: summary=%#v runner calls=%d", summary, len(runner.requests))
	}
	if len(repo.attestations) != 0 {
		t.Fatalf("completion attestations = %d, want none", len(repo.attestations))
	}
}

func TestRunDueFailsClosedWhenRequiredQualityGateReadFails(t *testing.T) {
	repo := newFakeWorkflowRepo()
	runner := &fakeTaskRunner{result: &TaskRunResult{
		PlanID:             "plan-gate-read-failure",
		CompletionStatus:   "validated",
		VerificationStatus: "verified",
		Passed:             true,
	}}
	service := NewServiceWithTaskRunner(repo, runner)
	record, err := service.Intake(IntakeRequest{Input: "Create Trello checklist for low risk admin work"})
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}
	repo.findQualityGatesErr = fmt.Errorf("quality gate store unavailable")

	summary, err := service.RunDue(RunDueRequest{Limit: 5})
	if err != nil {
		t.Fatalf("RunDue: %v", err)
	}
	if summary.Blocked != 1 || len(runner.requests) != 0 {
		t.Fatalf("failed quality-gate read did not block before task execution: summary=%#v runner calls=%d", summary, len(runner.requests))
	}
	if item := repo.items[record.Item.ID]; item.CurrentState != StateBlocked {
		t.Fatalf("workflow state = %q, want persisted blocked state", item.CurrentState)
	}
	if len(repo.attestations) != 0 {
		t.Fatalf("completion attestations = %d, want none", len(repo.attestations))
	}
}

func TestRunDueFailsClosedWhenQualityGateEvidenceReadsFail(t *testing.T) {
	cases := []struct {
		name string
		fail func(*fakeWorkflowRepo)
	}{
		{name: "source links", fail: func(repo *fakeWorkflowRepo) { repo.findSourceLinksErr = fmt.Errorf("source-link store unavailable") }},
		{name: "evidence claims", fail: func(repo *fakeWorkflowRepo) { repo.findEvidenceClaimsErr = fmt.Errorf("evidence store unavailable") }},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			repo := newFakeWorkflowRepo()
			runner := &fakeTaskRunner{result: &TaskRunResult{
				PlanID:             "plan-evidence-read-failure",
				CompletionStatus:   "validated",
				VerificationStatus: "verified",
				Passed:             true,
			}}
			service := NewServiceWithTaskRunner(repo, runner)
			record, err := service.Intake(IntakeRequest{Input: "Create Trello checklist for low risk admin work"})
			if err != nil {
				t.Fatalf("Intake: %v", err)
			}
			testCase.fail(repo)

			summary, err := service.RunDue(RunDueRequest{Limit: 5})
			if err != nil {
				t.Fatalf("RunDue: %v", err)
			}
			if summary.Blocked != 1 || len(runner.requests) != 1 {
				t.Fatalf("failed evidence read did not persist review after task run: summary=%#v runner calls=%d", summary, len(runner.requests))
			}
			if item := repo.items[record.Item.ID]; item.CurrentState != StateBlocked {
				t.Fatalf("workflow state = %q, want persisted blocked state", item.CurrentState)
			}
			if len(repo.attestations) != 0 {
				t.Fatalf("completion attestations = %d, want none", len(repo.attestations))
			}
		})
	}
}

func TestRunDueFailsClosedWhenQualityGateStatusWriteFails(t *testing.T) {
	repo := newFakeWorkflowRepo()
	runner := &fakeTaskRunner{result: &TaskRunResult{
		PlanID:             "plan-gate-status-write-failure",
		CompletionStatus:   "validated",
		VerificationStatus: "verified",
		Passed:             true,
	}}
	service := NewServiceWithTaskRunner(repo, runner)
	record, err := service.Intake(IntakeRequest{Input: "Create Trello checklist for low risk admin work"})
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}
	repo.updateQualityGateErr = fmt.Errorf("quality gate status store unavailable")

	summary, err := service.RunDue(RunDueRequest{Limit: 5})
	if err != nil {
		t.Fatalf("RunDue: %v", err)
	}
	if summary.Blocked != 1 || len(runner.requests) != 1 {
		t.Fatalf("quality-gate status write failure did not block completion: summary=%#v runner calls=%d", summary, len(runner.requests))
	}
	if item := repo.items[record.Item.ID]; item.CurrentState != StateBlocked {
		t.Fatalf("workflow state = %q, want persisted blocked state", item.CurrentState)
	}
	if len(repo.attestations) != 0 {
		t.Fatalf("completion attestations = %d, want none", len(repo.attestations))
	}
}

func TestIntakeBlocksSourcedWorkWhenProvenancePersistenceFails(t *testing.T) {
	cases := []struct {
		name  string
		setup func(*fakeWorkflowRepo)
	}{
		{name: "intake record", setup: func(repo *fakeWorkflowRepo) { repo.saveIntakeRecordErr = fmt.Errorf("intake store unavailable") }},
		{name: "origin source link", setup: func(repo *fakeWorkflowRepo) { repo.createSourceLinkErr = fmt.Errorf("source-link store unavailable") }},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			repo := newFakeWorkflowRepo()
			testCase.setup(repo)
			runner := &fakeTaskRunner{result: &TaskRunResult{Passed: true, VerificationStatus: "verified"}}
			service := NewServiceWithTaskRunner(repo, runner)
			_, err := service.Intake(IntakeRequest{
				OwnerIdentity: "alice",
				Input:         "Create a low-risk checklist from the imported task.",
				SourceType:    "trello",
				SourceID:      "card-123",
				SourceURI:     "https://trello.example/card-123",
				SourceLabel:   "Imported card",
			})
			if err == nil || !strings.Contains(strings.ToLower(err.Error()), "source provenance") {
				t.Fatalf("Intake error = %v, want source-provenance persistence failure", err)
			}
			if len(repo.items) != 1 {
				t.Fatalf("persisted workflow count = %d, want one blocked record", len(repo.items))
			}
			var item *models.WorkflowItem
			for _, stored := range repo.items {
				item = stored
			}
			if item.CurrentState != StateBlocked || !strings.Contains(strings.ToLower(item.BlockedReason), "source provenance") {
				t.Fatalf("workflow state/reason = %q/%q, want blocked for missing provenance", item.CurrentState, item.BlockedReason)
			}
			if _, err := service.RunDue(RunDueRequest{Limit: 5}); err != nil {
				t.Fatalf("RunDue: %v", err)
			}
			if len(runner.requests) != 0 {
				t.Fatalf("task runner calls = %d, want no execution without durable source provenance", len(runner.requests))
			}
		})
	}
}

func TestRunDueBlocksSourcedWorkWhenOriginProvenanceIsMissing(t *testing.T) {
	cases := []struct {
		name    string
		corrupt func(*fakeWorkflowRepo, uuid.UUID)
	}{
		{name: "missing intake record", corrupt: func(repo *fakeWorkflowRepo, id uuid.UUID) { delete(repo.intake, id) }},
		{name: "intake record read failure", corrupt: func(repo *fakeWorkflowRepo, _ uuid.UUID) {
			repo.findIntakeRecordsErr = fmt.Errorf("intake store unavailable")
		}},
		{name: "missing origin link", corrupt: func(repo *fakeWorkflowRepo, id uuid.UUID) { delete(repo.sourceLinks, id) }},
		{name: "source-link read failure", corrupt: func(repo *fakeWorkflowRepo, _ uuid.UUID) {
			repo.findSourceLinksErr = fmt.Errorf("source-link store unavailable")
		}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			repo := newFakeWorkflowRepo()
			runner := &fakeTaskRunner{result: &TaskRunResult{Passed: true, VerificationStatus: "verified"}}
			service := NewServiceWithTaskRunner(repo, runner)
			record, err := service.Intake(IntakeRequest{
				OwnerIdentity: "alice",
				Input:         "Create a low-risk checklist from the imported task.",
				SourceType:    "trello",
				SourceID:      "card-456",
				SourceURI:     "https://trello.example/card-456",
				SourceLabel:   "Imported card",
			})
			if err != nil {
				t.Fatalf("Intake: %v", err)
			}
			testCase.corrupt(repo, record.Item.ID)

			summary, err := service.RunDue(RunDueRequest{Limit: 5})
			if err != nil {
				t.Fatalf("RunDue: %v", err)
			}
			if summary.Blocked != 1 || len(runner.requests) != 0 {
				t.Fatalf("missing provenance did not block before execution: summary=%#v runner calls=%d", summary, len(runner.requests))
			}
			if item := repo.items[record.Item.ID]; item.CurrentState != StateBlocked {
				t.Fatalf("workflow state = %q, want blocked", item.CurrentState)
			}
		})
	}
}

func TestIntakeUsesAtomicIdempotencyWhenURIPreflightMissesConcurrentInsert(t *testing.T) {
	repo := &staleURILookupWorkflowRepo{fakeWorkflowRepo: newFakeWorkflowRepo(), preflightMisses: 2}
	service := NewService(repo)
	request := IntakeRequest{
		OwnerIdentity: "alice",
		Input:         "Follow up on the imported source task.",
		SourceType:    "trello",
		SourceURI:     "https://trello.example/card-race",
		Trigger:       "source.extraction",
	}

	first, err := service.Intake(request)
	if err != nil {
		t.Fatalf("first Intake: %v", err)
	}
	second, err := service.Intake(request)
	if err != nil {
		t.Fatalf("second Intake after simulated stale preflight: %v", err)
	}
	if first.Item.ID != second.Item.ID {
		t.Fatalf("URI replay created workflow %s after first workflow %s", second.Item.ID, first.Item.ID)
	}
	if len(repo.items) != 1 || repo.idempotentCreateCalls != 2 {
		t.Fatalf("stored items=%d idempotent writes=%d; want one item and atomic write path for both deliveries", len(repo.items), repo.idempotentCreateCalls)
	}
}

func TestRunDueDoesNotCompleteWhenFrameworkSelectionProvenanceIsInvalid(t *testing.T) {
	repo := newFakeWorkflowRepo()
	selection := testFrameworkSelection("invalid-selection-plan")
	selection.CatalogDigest = "not-a-digest"
	runner := &fakeTaskRunner{result: &TaskRunResult{
		PlanID:             "invalid-selection-plan",
		CompletionStatus:   "validated",
		VerificationStatus: "verified",
		Output:             "completed",
		Passed:             true,
		FrameworkSelection: &selection,
	}}
	service := NewServiceWithTaskRunner(repo, runner)
	record, err := service.Intake(IntakeRequest{Input: "Create Trello checklist for low risk admin work"})
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}

	summary, err := service.RunDue(RunDueRequest{Limit: 1})
	if err != nil {
		t.Fatalf("RunDue: %v", err)
	}
	if summary.Completed != 0 || summary.Blocked != 1 || len(summary.Results) != 1 {
		t.Fatalf("invalid selection was described as complete: %#v", summary)
	}
	if summary.Results[0].Status == "completed" || summary.Results[0].FrameworkSelection != nil {
		t.Fatalf("invalid selection leaked as completed provenance: %#v", summary.Results[0])
	}
	updated, err := service.Get(record.Item.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if updated.Item.CurrentState != StateBlocked ||
		!strings.Contains(updated.Item.BlockedReason, "framework selection provenance could not be stored") {
		t.Fatalf("invalid framework selection state = %#v", updated.Item)
	}
	if len(updated.FrameworkSelections) != 0 {
		t.Fatalf("invalid framework selection was persisted: %#v", updated.FrameworkSelections)
	}
}

func TestRunDuePropagatesWorkflowRiskFloorToTaskRunner(t *testing.T) {
	repo := newFakeWorkflowRepo()
	runner := &fakeTaskRunner{result: &TaskRunResult{
		PlanID:             "risk-floor-plan",
		CompletionStatus:   "validated",
		VerificationStatus: "verified",
		Output:             "completed",
		Passed:             true,
	}}
	service := NewServiceWithTaskRunner(repo, runner)
	record, err := service.Intake(IntakeRequest{Input: "Create Trello checklist for low risk admin work"})
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}
	if _, err := service.RunDue(RunDueRequest{Limit: 1}); err != nil {
		t.Fatalf("RunDue: %v", err)
	}
	if len(runner.requests) != 1 {
		t.Fatalf("task runner requests = %d, want 1", len(runner.requests))
	}
	if runner.requests[0].RiskLevel != record.Item.RiskLevel || runner.requests[0].RiskLevel == "" {
		t.Fatalf("task runner risk floor = %q, workflow risk = %q", runner.requests[0].RiskLevel, record.Item.RiskLevel)
	}
}

func TestFrameworkSelectionV5RiskContractValidation(t *testing.T) {
	selection := testFrameworkSelection("risk-contract-plan")
	selection.SelectorAlgorithmVersion = "selector-v5"
	selection.TaskRiskLevel = "high"
	selection.EffectiveRiskCeiling = "high"
	selection.OperatingContractDigest = strings.Repeat("d", 64)
	maximumAutonomy := 6
	requiresApproval := true
	selection.MaximumAutonomyLevel = &maximumAutonomy
	selection.RequiresApproval = &requiresApproval
	if err := selection.Validate(selection.TaskPlanID); err != nil {
		t.Fatalf("valid selector-v5 risk contract rejected: %v", err)
	}

	for _, test := range []struct {
		name    string
		mutate  func(*FrameworkSelectionProvenance)
		message string
	}{
		{name: "missing task risk", mutate: func(value *FrameworkSelectionProvenance) { value.TaskRiskLevel = "" }, message: "task risk level"},
		{name: "missing ceiling", mutate: func(value *FrameworkSelectionProvenance) { value.EffectiveRiskCeiling = "" }, message: "effective risk ceiling"},
		{name: "ceiling downgrade", mutate: func(value *FrameworkSelectionProvenance) { value.EffectiveRiskCeiling = "medium" }, message: "below task risk"},
		{name: "missing autonomy", mutate: func(value *FrameworkSelectionProvenance) { value.MaximumAutonomyLevel = nil }, message: "autonomy and approval"},
		{name: "missing approval", mutate: func(value *FrameworkSelectionProvenance) { value.RequiresApproval = nil }, message: "autonomy and approval"},
	} {
		t.Run(test.name, func(t *testing.T) {
			candidate := selection
			test.mutate(&candidate)
			if err := candidate.Validate(candidate.TaskPlanID); err == nil || !strings.Contains(err.Error(), test.message) {
				t.Fatalf("error = %v, want %q", err, test.message)
			}
		})
	}
}

func TestLegacyFrameworkSelectionRemainsReadableWithoutRiskInference(t *testing.T) {
	selection := testFrameworkSelection("legacy-plan")
	selection.SelectorAlgorithmVersion = "selector-v4"
	selection.TaskRiskLevel = ""
	selection.EffectiveRiskCeiling = ""
	payload, err := json.Marshal(selection)
	if err != nil {
		t.Fatalf("marshal legacy selection: %v", err)
	}
	decoded, err := decodeFrameworkSelectionDecision(models.WorkflowDecision{
		DecisionType: frameworkSelectionDecisionType,
		Decision:     selection.SelectionDecisionID,
		Reason:       string(payload),
	})
	if err != nil {
		t.Fatalf("decode legacy selection: %v", err)
	}
	if decoded.TaskRiskLevel != "" || decoded.EffectiveRiskCeiling != "" {
		t.Fatalf("legacy risk contract was inferred: %#v", decoded)
	}
}

func TestRunDueDoesNotRepeatCompletedExternalActionAfterQualityGateFailure(t *testing.T) {
	repo := newFakeWorkflowRepo()
	runner := &fakeTaskRunner{result: &TaskRunResult{
		PlanID:                 "plan-runtime-quality-review",
		CompletionStatus:       "validated",
		VerificationStatus:     "verified",
		Output:                 "runtime completed",
		Passed:                 true,
		ExternalActionExecuted: true,
	}}
	service := NewServiceWithTaskRunner(repo, runner)
	record, err := service.Intake(IntakeRequest{
		Input:        "Review GitHub developer claim and run tests",
		AutomationID: uuid.NewString(),
	})
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}
	item := record.Item
	item.MaxRetries = 3
	if _, err := repo.UpdateItem(&item); err != nil {
		t.Fatalf("UpdateItem: %v", err)
	}

	summary, err := service.RunDue(RunDueRequest{Limit: 5})
	if err != nil {
		t.Fatalf("RunDue: %v", err)
	}
	if summary.Blocked != 1 || summary.Retried != 0 {
		t.Fatalf("summary = %#v, completed external action must not auto-retry", summary)
	}
	updated, err := service.Get(record.Item.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !strings.Contains(updated.Item.BlockedReason, "review evidence before any retry") {
		t.Fatalf("blocked reason = %q, want explicit duplicate-action protection", updated.Item.BlockedReason)
	}
}

func TestRunDueRequiresReviewWhenLeaseIsLostAfterExternalAction(t *testing.T) {
	repo := newFakeWorkflowRepo()
	repo.loseRunnableItemRenewalAfter = 1
	runner := &fakeTaskRunner{result: &TaskRunResult{
		PlanID:                 "external-action-plan",
		CompletionStatus:       "executed",
		VerificationStatus:     "verified",
		FailureReason:          "claim lease could not be confirmed after execution",
		Output:                 "do not persist this free-form output",
		RuntimeEvidenceURI:     "automation-launch://" + uuid.NewString(),
		RuntimeEvidenceLabel:   "controlled runtime launch",
		ExternalActionExecuted: true,
	}, err: errors.New("runtime connector response was lost after dispatch")}
	service := NewServiceWithTaskRunner(repo, runner)
	record, err := service.Intake(IntakeRequest{Input: "Create a checklist in Trello for low-risk administration"})
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}

	summary, err := service.RunDue(RunDueRequest{Limit: 5})
	if err != nil {
		t.Fatalf("RunDue: %v", err)
	}
	if summary.Blocked != 1 || summary.Retried != 0 {
		t.Fatalf("summary = %#v, want review-blocked item and no retry", summary)
	}
	updated, err := service.Get(record.Item.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if updated.Item.CurrentState != StateBlocked || updated.Item.NextRunAt != nil {
		t.Fatalf("workflow was retried after uncertain external action: %#v", updated.Item)
	}
	if updated.Item.LastTaskPlanID != "external-action-plan" {
		t.Fatalf("last task plan ID = %q, want retained execution plan", updated.Item.LastTaskPlanID)
	}
	if len(updated.FrameworkSelections) != 1 {
		t.Fatalf("framework selections = %d, want evidence retained before review", len(updated.FrameworkSelections))
	}
	runtimeEvidenceFound := false
	for _, claim := range updated.Evidence {
		if claim.SourceURI == runner.result.RuntimeEvidenceURI {
			runtimeEvidenceFound = true
			if strings.Contains(claim.ClaimText, "do not persist this free-form output") {
				t.Fatalf("uncertain runtime evidence persisted free-form task output: %q", claim.ClaimText)
			}
		}
	}
	if !runtimeEvidenceFound {
		t.Fatalf("lease-loss review did not retain runtime evidence: %#v", updated.Evidence)
	}
	if !strings.Contains(strings.ToLower(updated.Item.BlockedReason), "lease") ||
		!strings.Contains(strings.ToLower(updated.Item.BlockedReason), "review") ||
		!strings.Contains(updated.Item.BlockedReason, "runtime connector response was lost") {
		t.Fatalf("blocked reason = %q, want lease uncertainty, runner error, and human review", updated.Item.BlockedReason)
	}
	if len(runner.requests) != 1 {
		t.Fatalf("task runner calls = %d, want exactly one", len(runner.requests))
	}
	if _, err := service.RunDue(RunDueRequest{Limit: 5}); err != nil {
		t.Fatalf("second RunDue: %v", err)
	}
	if len(runner.requests) != 1 {
		t.Fatalf("task runner calls after second scan = %d, want no duplicate side effect", len(runner.requests))
	}
}

func TestRunDueDoesNotStartTaskWhenLeaseCannotBeConfirmedBeforeDispatch(t *testing.T) {
	repo := newFakeWorkflowRepo()
	repo.loseRunnableItemRenewal = true
	runner := &fakeTaskRunner{result: &TaskRunResult{CompletionStatus: "completed"}}
	service := NewServiceWithTaskRunner(repo, runner)
	record, err := service.Intake(IntakeRequest{Input: "Create a checklist in Trello for low-risk administration"})
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}

	summary, err := service.RunDue(RunDueRequest{Limit: 5})
	if err != nil {
		t.Fatalf("RunDue: %v", err)
	}
	if summary.Blocked != 1 || len(runner.requests) != 0 {
		t.Fatalf("summary=%+v runner calls=%d; expected blocked before dispatch", summary, len(runner.requests))
	}
	updated, err := service.Get(record.Item.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if updated.Item.CurrentState != StateBlocked || !strings.Contains(updated.Item.BlockedReason, "before task start") {
		t.Fatalf("workflow was not held before task dispatch: %+v", updated.Item)
	}
}

func TestRunDueTreatsUnknownTaskRunnerErrorAsOutcomeUncertain(t *testing.T) {
	repo := newFakeWorkflowRepo()
	runner := &fakeTaskRunner{err: errors.New("provider timed out after dispatch")}
	service := NewServiceWithTaskRunner(repo, runner)
	record, err := service.Intake(IntakeRequest{Input: "Create a checklist in Trello for low-risk administration"})
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}

	summary, err := service.RunDue(RunDueRequest{Limit: 5})
	if err != nil {
		t.Fatalf("RunDue: %v", err)
	}
	if summary.Blocked != 1 || summary.Retried != 0 {
		t.Fatalf("summary = %#v, want blocked uncertain result without retry", summary)
	}
	updated, err := service.Get(record.Item.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if updated.Item.CurrentState != StateBlocked || updated.Item.NextRunAt != nil {
		t.Fatalf("state/run time = %q/%v, want non-rerunnable review block", updated.Item.CurrentState, updated.Item.NextRunAt)
	}
	if !strings.Contains(updated.Item.BlockedReason, "outcome is uncertain") || !strings.Contains(updated.Item.BlockedReason, "provider timed out") {
		t.Fatalf("blocked reason = %q, want uncertainty and provider error", updated.Item.BlockedReason)
	}
}

func TestRunDueRetriesExplicitSafeNoSideEffectError(t *testing.T) {
	repo := newFakeWorkflowRepo()
	runner := &fakeTaskRunner{err: MarkTaskFailureSafeNoSideEffect(errors.New("framework preview rejected before execution"))}
	service := NewServiceWithTaskRunner(repo, runner)
	record, err := service.Intake(IntakeRequest{Input: "Create a checklist in Trello for low-risk administration"})
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}

	summary, err := service.RunDue(RunDueRequest{Limit: 5})
	if err != nil {
		t.Fatalf("RunDue: %v", err)
	}
	if summary.Retried != 1 || summary.Blocked != 0 {
		t.Fatalf("summary = %#v, want one safe retry and no review block", summary)
	}
	updated, err := service.Get(record.Item.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if updated.Item.CurrentState != StateReady || updated.Item.NextRunAt == nil {
		t.Fatalf("state/run time = %q/%v, want scheduled safe retry", updated.Item.CurrentState, updated.Item.NextRunAt)
	}
}

func TestRunDueReviewBlocksSideEffectResultEvenWhenErrorIsMarkedSafe(t *testing.T) {
	repo := newFakeWorkflowRepo()
	runtimeURI := "automation-launch://" + uuid.NewString()
	runner := &fakeTaskRunner{
		result: &TaskRunResult{
			PlanID:                 "partial-external-plan",
			VerificationStatus:     "needs_review",
			RuntimeEvidenceURI:     runtimeURI,
			RuntimeEvidenceLabel:   "controlled runtime launch",
			ExternalActionExecuted: true,
		},
		err: MarkTaskFailureSafeNoSideEffect(errors.New("contradictory runner error marker")),
	}
	service := NewServiceWithTaskRunner(repo, runner)
	record, err := service.Intake(IntakeRequest{Input: "Create a checklist in Trello for low-risk administration"})
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}

	summary, err := service.RunDue(RunDueRequest{Limit: 5})
	if err != nil {
		t.Fatalf("RunDue: %v", err)
	}
	if summary.Blocked != 1 || summary.Retried != 0 {
		t.Fatalf("summary = %#v, an executed external action must override the safe marker", summary)
	}
	updated, err := service.Get(record.Item.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if updated.Item.LastTaskPlanID != "partial-external-plan" || updated.Item.CurrentState != StateBlocked {
		t.Fatalf("uncertain result was not retained in review state: %#v", updated.Item)
	}
	if len(updated.FrameworkSelections) != 1 {
		t.Fatalf("framework selections = %d, want partial result provenance persisted", len(updated.FrameworkSelections))
	}
	if len(updated.Evidence) != 1 || updated.Evidence[0].SourceURI != runtimeURI {
		t.Fatalf("runtime evidence = %#v, want durable launch evidence", updated.Evidence)
	}
}

func TestRunDueAuditsUncertainEvidencePersistenceFailure(t *testing.T) {
	repo := newFakeWorkflowRepo()
	repo.loseRunnableItemRenewalAfter = 1
	repo.createEvidenceClaimErr = errors.New("runtime evidence store unavailable")
	runner := &fakeTaskRunner{result: &TaskRunResult{
		PlanID:               "plan-with-unpersisted-runtime-evidence",
		VerificationStatus:   "needs_review",
		RuntimeEvidenceURI:   "automation-launch://" + uuid.NewString(),
		RuntimeEvidenceLabel: "controlled runtime launch",
	}}
	service := NewServiceWithTaskRunner(repo, runner)
	record, err := service.Intake(IntakeRequest{Input: "Create a checklist in Trello for low-risk administration"})
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}

	summary, err := service.RunDue(RunDueRequest{Limit: 5})
	if err != nil {
		t.Fatalf("RunDue: %v", err)
	}
	if summary.Blocked != 1 || summary.Retried != 0 {
		t.Fatalf("summary = %#v, want blocked review and no retry", summary)
	}
	updated, err := service.Get(record.Item.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !strings.Contains(updated.Item.BlockedReason, "evidence persistence failed") || !strings.Contains(updated.Item.BlockedReason, "runtime evidence store unavailable") {
		t.Fatalf("blocked reason = %q, want evidence persistence failure", updated.Item.BlockedReason)
	}
	audited := false
	for _, event := range updated.Events {
		if event.EventType == "workflow.worker_review_required" && strings.Contains(event.Message, "runtime evidence store unavailable") {
			audited = true
			break
		}
	}
	if !audited {
		t.Fatalf("review audit did not record evidence persistence failure: %#v", updated.Events)
	}
}

func TestDefaultRulesPreserveCreatedAtAcrossUpserts(t *testing.T) {
	service := NewService(newFakeWorkflowRepo())
	first := service.Overview()
	firstRule, ok := findRule(first.Rules, "approval.legal_external")
	if !ok {
		t.Fatalf("expected default rule")
	}
	second := service.Overview()
	secondRule, ok := findRule(second.Rules, "approval.legal_external")
	if !ok {
		t.Fatalf("expected default rule on second overview")
	}
	if !firstRule.CreatedAt.Equal(secondRule.CreatedAt) {
		t.Fatalf("created at changed from %s to %s", firstRule.CreatedAt, secondRule.CreatedAt)
	}
	if firstRule.ID != secondRule.ID {
		t.Fatalf("rule ID changed from %s to %s", firstRule.ID, secondRule.ID)
	}
}

func TestRunDueRetriesAndBlocksAfterLimit(t *testing.T) {
	repo := newFakeWorkflowRepo()
	runner := &fakeTaskRunner{result: &TaskRunResult{
		CompletionStatus:   "review_required",
		VerificationStatus: "needs_review",
		FailureReason:      "unsupported claims",
		Passed:             false,
		ReviewRequired:     true,
	}}
	service := NewServiceWithTaskRunner(repo, runner)
	record, err := service.Intake(IntakeRequest{Input: "Create Trello checklist for low risk admin work"})
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}
	item := record.Item
	item.MaxRetries = 1
	if _, err := repo.UpdateItem(&item); err != nil {
		t.Fatalf("UpdateItem: %v", err)
	}
	summary, err := service.RunDue(RunDueRequest{Limit: 5})
	if err != nil {
		t.Fatalf("RunDue: %v", err)
	}
	if summary.Blocked != 1 {
		t.Fatalf("blocked = %d, want 1: %#v", summary.Blocked, summary)
	}
	updated, err := service.Get(record.Item.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if updated.Item.CurrentState != StateBlocked {
		t.Fatalf("state = %q, want blocked", updated.Item.CurrentState)
	}
	if updated.Item.RetryCount != 1 {
		t.Fatalf("retry count = %d, want 1", updated.Item.RetryCount)
	}
}

func TestRunDueBlocksReviewRequiredTaskWithoutAutomaticRetry(t *testing.T) {
	repo := newFakeWorkflowRepo()
	runner := &fakeTaskRunner{result: &TaskRunResult{
		CompletionStatus:   "review_required",
		VerificationStatus: "needs_review",
		FailureReason:      "controlled runtime execution requires operator review",
		Passed:             false,
		ReviewRequired:     true,
	}}
	service := NewServiceWithTaskRunner(repo, runner)
	record, err := service.Intake(IntakeRequest{Input: "Run local script tests", AutomationID: uuid.NewString()})
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}
	item := record.Item
	item.MaxRetries = 3
	if _, err := repo.UpdateItem(&item); err != nil {
		t.Fatalf("UpdateItem: %v", err)
	}

	summary, err := service.RunDue(RunDueRequest{Limit: 5})
	if err != nil {
		t.Fatalf("RunDue: %v", err)
	}
	if summary.Blocked != 1 || summary.Retried != 0 {
		t.Fatalf("summary = %#v, review-required task must block without retry", summary)
	}
	if len(summary.Results) != 1 || !summary.Results[0].ReviewRequired {
		t.Fatalf("required review was omitted from the API result: %#v", summary.Results)
	}
	updated, err := service.Get(record.Item.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if updated.Item.CurrentState != StateBlocked || updated.Item.NextRunAt != nil {
		t.Fatalf("review-required workflow was scheduled again: %#v", updated.Item)
	}
	if updated.Item.RetryCount != 1 {
		t.Fatalf("attempt count = %d, want 1", updated.Item.RetryCount)
	}
}

func TestWorkflowWorkerPassesConfiguredAutomationID(t *testing.T) {
	repo := newFakeWorkflowRepo()
	runner := &fakeTaskRunner{result: &TaskRunResult{
		PlanID:             "automation-plan",
		CompletionStatus:   "validated",
		VerificationStatus: "verified",
		Output:             "runtime completed",
		Passed:             true,
	}}
	service := NewServiceWithTaskRunner(repo, runner)
	automationID := uuid.NewString()
	_, err := service.Intake(IntakeRequest{
		Input:        "Run local script and verify completion",
		ProjectKey:   "018-HAI",
		AutomationID: automationID,
	})
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}
	if _, err := service.RunDue(RunDueRequest{Limit: 5}); err != nil {
		t.Fatalf("RunDue: %v", err)
	}
	if len(runner.requests) != 1 || runner.requests[0].AutomationID != automationID {
		t.Fatalf("task runner requests = %#v, automation ID was not propagated", runner.requests)
	}
}

func TestIntakeDeduplicatesBySourceURI(t *testing.T) {
	service := NewService(newFakeWorkflowRepo())
	first, err := service.Intake(IntakeRequest{
		Input:     "Follow up: request missing evidence bundle from legal contact.",
		SourceURI: "local://source/item/123",
		Trigger:   "source.extraction",
	})
	if err != nil {
		t.Fatalf("Intake first: %v", err)
	}
	second, err := service.Intake(IntakeRequest{
		Input:     "Follow up: request missing evidence bundle from legal contact.",
		SourceURI: "local://source/item/123",
		Trigger:   "source.extraction",
	})
	if err != nil {
		t.Fatalf("Intake second: %v", err)
	}
	if first.Item.ID != second.Item.ID {
		t.Fatalf("deduped ID = %s, want %s", second.Item.ID, first.Item.ID)
	}
	if len(second.Events) < 2 {
		t.Fatalf("expected dedupe audit event")
	}
}

func TestIntakeUsesSourceRecordIdentityBeforeSharedURIAndSupersedesChanges(t *testing.T) {
	repo := newFakeWorkflowRepo()
	service := NewService(repo)
	first, err := service.Intake(IntakeRequest{
		Input:      "Follow up: prepare the first project record.",
		SourceType: "email",
		SourceID:   "message-1",
		SourceURI:  "mailto:shared@example.test",
	})
	if err != nil {
		t.Fatalf("Intake first: %v", err)
	}
	second, err := service.Intake(IntakeRequest{
		Input:      "Follow up: prepare the second project record.",
		SourceType: "email",
		SourceID:   "message-2",
		SourceURI:  "mailto:shared@example.test",
	})
	if err != nil {
		t.Fatalf("Intake second: %v", err)
	}
	if first.Item.ID == second.Item.ID {
		t.Fatalf("separate source records sharing a URI were incorrectly deduplicated")
	}

	repeated, err := service.Intake(IntakeRequest{
		Input:      "Updated text for the first project record.",
		SourceType: "email",
		SourceID:   "message-1",
		SourceURI:  "mailto:changed@example.test",
	})
	if err != nil {
		t.Fatalf("Intake repeated: %v", err)
	}
	if repeated.Item.ID == first.Item.ID {
		t.Fatalf("changed source revision reused stale workflow")
	}
	archived, err := repo.FindItem(first.Item.ID)
	if err != nil {
		t.Fatalf("FindItem superseded: %v", err)
	}
	if !archived.Archived || archived.CurrentState != StateArchived {
		t.Fatalf("prior workflow was not archived: %#v", archived)
	}
	if repeated.Item.SourceURI != "mailto:changed@example.test" || repeated.Item.Description != "Updated text for the first project record." {
		t.Fatalf("new workflow did not use revised source content: %#v", repeated.Item)
	}
	if !hasDecision(repeated.Decisions, "classification", repeated.Item.TaskType) {
		t.Fatalf("new source revision was not freshly classified")
	}
	dashboard, err := service.Dashboard()
	if err != nil {
		t.Fatalf("Dashboard: %v", err)
	}
	for _, loop := range dashboard.DueOpenLoops {
		if loop.WorkflowID == first.Item.ID {
			t.Fatalf("superseded workflow remained in due open-loop queue")
		}
	}
}

func TestIntakeDeduplicatesUnchangedSourceRevision(t *testing.T) {
	service := NewService(newFakeWorkflowRepo())
	request := IntakeRequest{
		Input:          "Follow up: prepare the first project record.",
		ProjectKey:     "018-HAI",
		SourceType:     "email",
		SourceID:       "message-unchanged",
		SourceURI:      "mailto:project@example.test",
		SourceLabel:    "Project message",
		RequiresReview: true,
		ReviewReason:   "extraction is uncertain",
	}
	first, err := service.Intake(request)
	if err != nil {
		t.Fatalf("Intake first: %v", err)
	}
	second, err := service.Intake(request)
	if err != nil {
		t.Fatalf("Intake second: %v", err)
	}
	if first.Item.ID != second.Item.ID {
		t.Fatalf("unchanged revision created duplicate workflow")
	}
	if first.Item.SourceRevision == "" || second.Item.SourceRevision != first.Item.SourceRevision {
		t.Fatalf("source revision was not stable: %q/%q", first.Item.SourceRevision, second.Item.SourceRevision)
	}
}

func TestIntakeDoesNotReportIncompleteSourceAsDeduplicatedSuccess(t *testing.T) {
	repo := newFakeWorkflowRepo()
	service := NewService(repo)
	request := IntakeRequest{
		OwnerIdentity:  "alice",
		Input:          "Follow up: prepare the first project record.",
		SourceType:     "email",
		SourceID:       "message-incomplete-replay",
		SourceURI:      "mailto:project@example.test",
		RequiresReview: true,
		ReviewReason:   "extraction needs operator review",
	}
	first, err := service.Intake(request)
	if err != nil {
		t.Fatalf("Intake first: %v", err)
	}
	stored := *repo.items[first.Item.ID]
	stored.CurrentState = StateNewInput
	repo.items[stored.ID] = &stored

	if record, err := service.Intake(request); !errors.Is(err, ErrWorkflowIntakeIncomplete) {
		t.Fatalf("Intake replay = record %#v, error %v; want explicit incomplete-intake error", record, err)
	}
	if repo.items[stored.ID].CurrentState != StateNewInput {
		t.Fatalf("incomplete intake state was changed by replay: %#v", repo.items[stored.ID])
	}
}

func TestIntakeSourceRevisionChangesWhenTrelloProjectHintChanges(t *testing.T) {
	repo := newFakeWorkflowRepo()
	service := NewService(repo)
	request := IntakeRequest{
		Input:          "Review the source notes and prepare the next action.",
		ProjectKeyHint: "board-project-a",
		SourceType:     "trello",
		SourceID:       "card-hint-change",
		SourceURI:      "https://trello.com/c/card-hint-change",
		RequiresReview: true,
	}
	first, err := service.Intake(request)
	if err != nil {
		t.Fatalf("Intake first: %v", err)
	}
	request.ProjectKeyHint = "board-project-b"
	second, err := service.Intake(request)
	if err != nil {
		t.Fatalf("Intake with changed project hint: %v", err)
	}
	if first.Item.ID == second.Item.ID || first.Item.SourceRevision == second.Item.SourceRevision {
		t.Fatalf("changed project routing context reused stale workflow: first=%#v second=%#v", first.Item, second.Item)
	}
	archived, err := service.Get(first.Item.ID)
	if err != nil {
		t.Fatalf("Get previous workflow: %v", err)
	}
	if archived.Item.CurrentState != StateArchived {
		t.Fatalf("previous source workflow state = %q, want archived after source revision change", archived.Item.CurrentState)
	}
}

func TestChangedSourceReviewStatusInvalidatesPriorApproval(t *testing.T) {
	repo := newFakeWorkflowRepo()
	service := NewService(repo)
	first, err := service.Intake(IntakeRequest{
		Input:          "Email the lawyer with the prepared evidence summary.",
		SourceType:     "email",
		SourceID:       "legal-message",
		RequiresReview: true,
		ReviewReason:   "extraction is uncertain",
	})
	if err != nil {
		t.Fatalf("Intake first: %v", err)
	}
	approved, err := service.ResolveApproval(first.Item.ID, ApprovalResolutionRequest{
		Approved: true,
		Note:     "Approved this exact draft.",
	})
	if err != nil {
		t.Fatalf("ResolveApproval: %v", err)
	}
	if approved.Item.ApprovalStatus != "approved" {
		t.Fatalf("first approval was not recorded")
	}

	revised, err := service.Intake(IntakeRequest{
		Input:          "Email the lawyer with the revised evidence summary and additional request.",
		SourceType:     "email",
		SourceID:       "legal-message",
		RequiresReview: true,
		ReviewReason:   "extraction contains sensitive content",
	})
	if err != nil {
		t.Fatalf("Intake revised: %v", err)
	}
	if revised.Item.ID == first.Item.ID {
		t.Fatalf("revised instructions retained the approved workflow")
	}
	if revised.Item.ApprovalStatus != "pending" || revised.Item.CurrentState != StateNeedsApproval {
		t.Fatalf("revised workflow inherited stale approval: %#v", revised.Item)
	}
	old, _ := repo.FindItem(first.Item.ID)
	if !old.Archived {
		t.Fatalf("approved prior revision was not preserved as archived history")
	}
}

func TestChangedSourceCannotSupersedeInProgressWorkflow(t *testing.T) {
	repo := newFakeWorkflowRepo()
	service := NewService(repo)
	first, err := service.Intake(IntakeRequest{
		Input:      "Follow up: prepare the project record.",
		SourceType: "email",
		SourceID:   "active-message",
	})
	if err != nil {
		t.Fatalf("Intake first: %v", err)
	}
	item := first.Item
	item.CurrentState = StateInProgress
	item.WorkerClaimID = "active-worker"
	if _, err := repo.UpdateItem(&item); err != nil {
		t.Fatalf("UpdateItem: %v", err)
	}
	if _, err := service.Intake(IntakeRequest{
		Input:      "Follow up: replace the project record while it is running.",
		SourceType: "email",
		SourceID:   "active-message",
	}); err == nil {
		t.Fatalf("changed source superseded an in-progress workflow")
	}
	stored, _ := repo.FindItem(first.Item.ID)
	if stored.Archived || stored.CurrentState != StateInProgress || stored.WorkerClaimID != "active-worker" {
		t.Fatalf("in-progress workflow changed after rejected supersession: %#v", stored)
	}
}

func TestIntakeExplicitReviewGatePreventsAutonomousReadyState(t *testing.T) {
	service := NewService(newFakeWorkflowRepo())
	record, err := service.Intake(IntakeRequest{
		Input:          "Todo: contact the project owner.",
		SourceType:     "email",
		SourceID:       "uncertain-message",
		RequiresReview: true,
		ReviewReason:   "extraction is uncertain",
	})
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}
	if record.Item.CurrentState != StateNeedsApproval || !record.Item.RequiresApproval {
		t.Fatalf("review-gated source record entered autonomous queue: %#v", record.Item)
	}
	if record.Item.ApprovalReason != "extraction is uncertain" || record.Item.AutonomyLevel != "approve_before_execute" {
		t.Fatalf("review reason was not preserved: %#v", record.Item)
	}
}

func TestRetractSourceBlocksPendingSourceDerivedWorkflow(t *testing.T) {
	service := NewService(newFakeWorkflowRepo())
	record, err := service.Intake(IntakeRequest{
		Input:      "Follow up: prepare the project checklist.",
		SourceType: "email",
		SourceID:   "message-to-retract",
		SourceURI:  "local://message-to-retract",
	})
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}
	if record.Item.CurrentState != StateReady {
		t.Fatalf("initial state = %q, want ready", record.Item.CurrentState)
	}
	if err := service.RetractSource("email", "message-to-retract", "operator deleted the extraction"); err != nil {
		t.Fatalf("RetractSource: %v", err)
	}
	updated, err := service.Get(record.Item.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if updated.Item.CurrentState != StateBlocked || updated.Item.VerificationStatus != "needs_review" ||
		!updated.Item.RequiresApproval || updated.Item.ApprovalStatus != "pending" ||
		!strings.HasPrefix(updated.Item.ApprovalReason, sourceRetractionQuarantinePrefix) {
		t.Fatalf("retracted workflow remained executable: %#v", updated.Item)
	}
	if !hasDecision(updated.Decisions, "source_retraction", "blocked") {
		t.Fatalf("source retraction decision was not recorded")
	}
}

func TestRetractedWorkflowCannotReuseApprovalOrApprovedProposal(t *testing.T) {
	repo := newFakeWorkflowRepo()
	service := NewService(repo)
	record, err := service.Intake(IntakeRequest{
		OwnerIdentity: "alice",
		Input:         "Email the lawyer with a formal response about the legal claim.",
		SourceType:    "email",
		SourceID:      "retracted-approved-message",
		SourceURI:     "local://message/retracted-approved",
	})
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}
	if !record.Item.RequiresApproval || record.Item.CurrentState != StateNeedsApproval {
		t.Fatalf("legal workflow did not require approval: %#v", record.Item)
	}
	approved, err := service.ResolveApproval(record.Item.ID, ApprovalResolutionRequest{
		Approved: true,
		Note:     "Approved the original source-derived action.",
		Actor:    "alice",
	})
	if err != nil {
		t.Fatalf("ResolveApproval: %v", err)
	}
	if approved.Item.ApprovalStatus != "approved" || approved.Item.CurrentState != StateReady {
		t.Fatalf("initial approval was not recorded: %#v", approved.Item)
	}
	proposal, err := repo.CreateProposal(&models.WorkflowProposal{
		WorkflowID:        record.Item.ID,
		RecommendedAction: "Approve the source-derived follow-up",
		Status:            "open",
	})
	if err != nil {
		t.Fatalf("CreateProposal: %v", err)
	}
	proposalsBeforeRetraction, err := repo.FindProposals(record.Item.ID)
	if err != nil {
		t.Fatalf("FindProposals before retraction: %v", err)
	}
	if err := service.RetractSource("email", "retracted-approved-message", "operator removed the source"); err != nil {
		t.Fatalf("RetractSource: %v", err)
	}
	updated, err := service.Get(record.Item.ID)
	if err != nil {
		t.Fatalf("Get retracted workflow: %v", err)
	}
	if updated.Item.CurrentState != StateBlocked || !updated.Item.RequiresApproval || updated.Item.ApprovalStatus != "pending" {
		t.Fatalf("source retraction did not revoke execution authorization: %#v", updated.Item)
	}

	if _, err := service.Transition(record.Item.ID, TransitionRequest{TargetState: StateReady, Actor: "alice"}); !errors.Is(err, ErrWorkflowSourceRetracted) {
		t.Fatal("source-retracted workflow was manually returned to ready")
	}
	if _, err := service.ResolveApproval(record.Item.ID, ApprovalResolutionRequest{Approved: true, Actor: "alice"}); !errors.Is(err, ErrWorkflowSourceRetracted) {
		t.Fatal("source-retracted workflow accepted a new approval")
	}
	if _, err := service.ResolveProposal(record.Item.ID, proposal.ID, ProposalResolutionRequest{Status: "approved", Actor: "alice"}); !errors.Is(err, ErrWorkflowSourceRetracted) {
		t.Fatal("source-retracted workflow accepted an approved proposal")
	}
	proposals, err := repo.FindProposals(record.Item.ID)
	if err != nil || !reflect.DeepEqual(proposals, proposalsBeforeRetraction) {
		t.Fatalf("proposal state after rejected approval = %#v, err=%v; retraction and rejected approval must not mutate proposals from their prior state %#v", proposals, err, proposalsBeforeRetraction)
	}
	final, err := service.Get(record.Item.ID)
	if err != nil || final.Item.CurrentState != StateBlocked || final.Item.ApprovalStatus != "pending" {
		t.Fatalf("workflow state after rejected reactivation = %#v, err=%v", final, err)
	}
}

func TestRetractedLegacyReadyWorkflowNeverReachesTaskRunner(t *testing.T) {
	repo := newFakeWorkflowRepo()
	runner := &fakeTaskRunner{result: &TaskRunResult{Passed: true, VerificationStatus: "verified"}}
	service := NewServiceWithTaskRunner(repo, runner)
	record, err := service.Intake(IntakeRequest{
		OwnerIdentity: "alice",
		Input:         "Follow up: prepare the source project checklist.",
		SourceType:    "email",
		SourceID:      "retracted-low-risk-message",
		SourceURI:     "local://message/retracted-low-risk",
	})
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}
	if record.Item.RequiresApproval {
		t.Fatalf("fixture unexpectedly requires approval: %#v", record.Item)
	}
	if err := service.RetractSource("email", "retracted-low-risk-message", "operator removed the source"); err != nil {
		t.Fatalf("RetractSource: %v", err)
	}

	// Simulate a pre-fix/legacy row that was manually set ready after the
	// retraction marker had been persisted.
	legacy := *repo.items[record.Item.ID]
	legacy.CurrentState = StateReady
	if _, err := repo.UpdateItem(&legacy); err != nil {
		t.Fatalf("simulate legacy ready row: %v", err)
	}
	result, err := service.RunOneForOwner("alice", record.Item.ID)
	if err != nil {
		t.Fatalf("RunOneForOwner: %v", err)
	}
	if result.Status != "blocked" || len(runner.requests) != 0 {
		t.Fatalf("retracted legacy workflow reached execution: result=%#v taskRequests=%#v", result, runner.requests)
	}
}

func TestRetractedInterruptedWorkflowCannotBeRetried(t *testing.T) {
	repo := newFakeWorkflowRepo()
	service := NewService(repo)
	record, err := service.Intake(IntakeRequest{
		OwnerIdentity: "alice",
		Input:         "Follow up: prepare a project checklist after the interrupted action.",
		SourceType:    "email",
		SourceID:      "retracted-interrupted-message",
		SourceURI:     "local://message/retracted-interrupted",
	})
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}
	item := record.Item
	item.CurrentState = StateBlocked
	item.RecoveryStatus = RecoveryNeedsReview
	item.WorkerClaimID = ""
	item.WorkerLeaseUntil = nil
	if _, err := repo.UpdateItem(&item); err != nil {
		t.Fatalf("prepare interrupted workflow: %v", err)
	}
	if err := service.RetractSource("email", "retracted-interrupted-message", "operator removed the source"); err != nil {
		t.Fatalf("RetractSource: %v", err)
	}

	recovery := InterruptedExecutionResolutionRequest{
		Decision:                 "retry",
		Note:                     "The earlier attempt was reconciled.",
		PriorExecutionReconciled: true,
		EvidenceURI:              "local://recovery/reconciled",
	}
	_, err = service.ResolveInterruptedExecution(record.Item.ID, recovery)
	if !errors.Is(err, ErrWorkflowSourceRetracted) {
		t.Fatalf("retry resolution error = %v, want source-retracted quarantine", err)
	}
	stored, err := repo.FindItem(record.Item.ID)
	if err != nil || stored.CurrentState != StateBlocked || stored.RecoveryStatus != RecoveryNeedsReview {
		t.Fatalf("retracted interruption changed after retry attempt: %#v err=%v", stored, err)
	}

	recovery.Decision = "confirm_completed"
	recovery.Note = "Confirmed the completed outcome from the independent reconciliation record."
	recovery.EvidenceURI = record.Item.SourceURI
	if _, err := service.ResolveInterruptedExecution(record.Item.ID, recovery); err == nil {
		t.Fatal("retracted workflow source must not be accepted as independent completion evidence")
	}
	recovery.EvidenceURI = "local://recovery/reconciled"
	reconciled, err := service.ResolveInterruptedExecution(record.Item.ID, recovery)
	if err != nil {
		t.Fatalf("evidence-backed completion reconciliation: %v", err)
	}
	if reconciled.Item.CurrentState != StateCompleted || reconciled.Item.VerificationStatus != "human_approved" ||
		reconciled.Item.RecoveryStatus != RecoveryCompletionConfirmed || reconciled.Item.RequiresApproval ||
		reconciled.Item.ApprovalStatus != approvalStatus(false) || reconciled.Item.ApprovalReason != "" {
		t.Fatalf("operator-reconciled completion state = %#v", reconciled.Item)
	}
	foundCompletionEvidence := false
	for _, sourceLink := range reconciled.SourceLinks {
		if sourceLink.Relationship == "completion_evidence" && sourceLink.SourceURI == recovery.EvidenceURI {
			foundCompletionEvidence = true
			break
		}
	}
	if !foundCompletionEvidence {
		t.Fatalf("independent completion evidence was not attached: %#v", reconciled.SourceLinks)
	}
}

func TestIndependentCompletionRecoveryRequiresFinalStateAndEvidence(t *testing.T) {
	now := time.Now().UTC()
	workflowID := uuid.New()
	item := &models.WorkflowItem{
		ID: workflowID, CurrentState: StateCompleted, RecoveryStatus: RecoveryCompletionConfirmed,
		VerificationStatus: "human_approved", CompletedAt: &now, ApprovalStatus: approvalStatus(false),
	}
	link := &models.WorkflowSourceLink{
		WorkflowID: workflowID, SourceType: "recovery_evidence", SourceURI: "local://recovery/completion",
		Relationship: "completion_evidence",
	}
	claim := &models.WorkflowEvidenceClaim{
		WorkflowID: workflowID, ClaimText: "Operator confirmed the independently reconciled outcome.",
		SourceURI: link.SourceURI, Reliability: "operator_attestation", Status: "human_approved",
	}
	gate := &models.WorkflowQualityGate{
		WorkflowID: workflowID, Gate: "verification before completion", Status: "passed",
		Reason: "Operator supplied separate completion evidence.",
	}
	if !independentlyReconciledWorkflowCompletion(item, link, claim, gate) {
		t.Fatal("valid evidence-backed operator completion should be accepted")
	}

	invalidCases := []struct {
		name   string
		change func(*models.WorkflowItem, *models.WorkflowSourceLink, *models.WorkflowEvidenceClaim, *models.WorkflowQualityGate)
	}{
		{
			name: "recovery not marked complete",
			change: func(item *models.WorkflowItem, _ *models.WorkflowSourceLink, _ *models.WorkflowEvidenceClaim, _ *models.WorkflowQualityGate) {
				item.RecoveryStatus = RecoveryNeedsReview
			},
		},
		{
			name: "approval still pending",
			change: func(item *models.WorkflowItem, _ *models.WorkflowSourceLink, _ *models.WorkflowEvidenceClaim, _ *models.WorkflowQualityGate) {
				item.RequiresApproval = true
				item.ApprovalStatus = "pending"
			},
		},
		{
			name: "source is not independent recovery evidence",
			change: func(_ *models.WorkflowItem, link *models.WorkflowSourceLink, _ *models.WorkflowEvidenceClaim, _ *models.WorkflowQualityGate) {
				link.SourceType = "email"
			},
		},
		{
			name: "reuses original source",
			change: func(item *models.WorkflowItem, link *models.WorkflowSourceLink, _ *models.WorkflowEvidenceClaim, _ *models.WorkflowQualityGate) {
				item.SourceURI = link.SourceURI
			},
		},
		{
			name: "empty operator claim",
			change: func(_ *models.WorkflowItem, _ *models.WorkflowSourceLink, claim *models.WorkflowEvidenceClaim, _ *models.WorkflowQualityGate) {
				claim.ClaimText = " "
			},
		},
		{
			name: "empty verification reason",
			change: func(_ *models.WorkflowItem, _ *models.WorkflowSourceLink, _ *models.WorkflowEvidenceClaim, gate *models.WorkflowQualityGate) {
				gate.Reason = ""
			},
		},
	}
	for _, testCase := range invalidCases {
		t.Run(testCase.name, func(t *testing.T) {
			invalidItem := *item
			invalidLink := *link
			invalidClaim := *claim
			invalidGate := *gate
			testCase.change(&invalidItem, &invalidLink, &invalidClaim, &invalidGate)
			if independentlyReconciledWorkflowCompletion(&invalidItem, &invalidLink, &invalidClaim, &invalidGate) {
				t.Fatal("incomplete or non-independent reconciliation evidence was accepted")
			}
		})
	}
}

type sourceRetractionClaimRaceRepo struct {
	*fakeWorkflowRepo
}

func (r *sourceRetractionClaimRaceRepo) UpdateWorkflowItemCAS(
	expected, updated *models.WorkflowItem,
) (*models.WorkflowItem, bool, error) {
	if expected.CurrentState != StateReady {
		return r.fakeWorkflowRepo.UpdateWorkflowItemCAS(expected, updated)
	}
	claimed := *r.items[expected.ID]
	leaseUntil := time.Now().UTC().Add(time.Minute)
	claimed.CurrentState = StateInProgress
	claimed.WorkerClaimID = "concurrent-worker-claim"
	claimed.WorkerLeaseUntil = &leaseUntil
	claimed.UpdatedAt = expected.UpdatedAt.Add(time.Second)
	r.items[claimed.ID] = &claimed
	return r.fakeWorkflowRepo.UpdateWorkflowItemCAS(expected, updated)
}

func TestRetractSourceDoesNotOverwriteConcurrentWorkerClaim(t *testing.T) {
	repo := &sourceRetractionClaimRaceRepo{fakeWorkflowRepo: newFakeWorkflowRepo()}
	service := NewService(repo)
	record, err := service.Intake(IntakeRequest{
		Input:      "Review the source-backed task.",
		SourceType: "email",
		SourceID:   "concurrent-retraction-message",
	})
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}
	if err := service.RetractSource("email", "concurrent-retraction-message", "operator removed the source"); err == nil {
		t.Fatal("source retraction should report that the workflow changed during its update")
	}

	stored, err := repo.FindItem(record.Item.ID)
	if err != nil {
		t.Fatalf("FindItem after concurrent claim: %v", err)
	}
	if stored.CurrentState != StateInProgress || stored.WorkerClaimID != "concurrent-worker-claim" || stored.WorkerLeaseUntil == nil {
		t.Fatalf("source retraction overwrote the active worker claim: %#v", stored)
	}
	for _, event := range repo.events[stored.ID] {
		if event.EventType == "workflow.source_retracted" {
			t.Fatal("source retraction audit was recorded although its compare-and-swap lost to the worker claim")
		}
	}
}

func TestRetractSourceRefusesWorkflowCurrentlyExecuting(t *testing.T) {
	repo := newFakeWorkflowRepo()
	service := NewService(repo)
	record, err := service.Intake(IntakeRequest{
		Input:      "Follow up: prepare the project checklist.",
		SourceType: "email",
		SourceID:   "message-in-progress",
	})
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}
	item := record.Item
	item.CurrentState = StateInProgress
	if _, err := repo.UpdateItem(&item); err != nil {
		t.Fatalf("UpdateItem: %v", err)
	}
	if err := service.RetractSource("email", "message-in-progress", "operator deleted the extraction"); err == nil {
		t.Fatalf("expected in-progress source workflow retraction to require interruption review")
	}
}

func TestDetectDueDateUsesCalendarStartBeforeOtherDates(t *testing.T) {
	due := detectDueDate("Google Calendar event: Review\nStart: 2026-08-05T09:00:00+02:00\nDescription: Evidence from 2025-01-01")
	want := time.Date(2026, time.August, 5, 7, 0, 0, 0, time.UTC)
	if due == nil || !due.Equal(want) {
		t.Fatalf("due = %v, want %v", due, want)
	}
}

func hasApprovalChecklist(items []models.WorkflowChecklistItem) bool {
	for _, item := range items {
		if item.RequiresApproval {
			return true
		}
	}
	return false
}

func hasChecklistContaining(items []models.WorkflowChecklistItem, parts ...string) bool {
	for _, item := range items {
		label := strings.ToLower(item.Label)
		matches := true
		for _, part := range parts {
			if !strings.Contains(label, strings.ToLower(part)) {
				matches = false
				break
			}
		}
		if matches {
			return true
		}
	}
	return false
}

func hasGateStatus(gates []models.WorkflowQualityGate, gateName, status string) bool {
	for _, gate := range gates {
		if gate.Gate == gateName && gate.Status == status {
			return true
		}
	}
	return false
}

func hasDecision(decisions []models.WorkflowDecision, decisionType, decision string) bool {
	for _, item := range decisions {
		if item.DecisionType == decisionType && item.Decision == decision {
			return true
		}
	}
	return false
}

func hasSourceRelationship(links []models.WorkflowSourceLink, relationship string) bool {
	for _, link := range links {
		if link.Relationship == relationship {
			return true
		}
	}
	return false
}

func hasEventType(events []models.WorkflowEvent, eventType string) bool {
	for _, event := range events {
		if event.EventType == eventType {
			return true
		}
	}
	return false
}

func hasEvidenceStatus(evidence []models.WorkflowEvidenceClaim, status string) bool {
	for _, claim := range evidence {
		if claim.Status == status {
			return true
		}
	}
	return false
}

func recoverInterruptedWorkflow(t *testing.T, repo *fakeWorkflowRepo, service Service, input string) *WorkflowRecord {
	t.Helper()
	record, err := service.Intake(IntakeRequest{Input: input})
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}
	if record.Item.RequiresApproval {
		record, err = service.ResolveApproval(record.Item.ID, ApprovalResolutionRequest{
			Approved: true,
			Note:     "Initial controlled execution approved for test.",
			Actor:    "Robert",
		})
		if err != nil {
			t.Fatalf("ResolveApproval: %v", err)
		}
	}
	claimedAt := time.Now().UTC().Add(-2 * time.Minute)
	claimed, acquired, err := repo.ClaimRunnableItem(record.Item.ID, "expired-workflow-claim", claimedAt, claimedAt.Add(time.Minute))
	if err != nil {
		t.Fatalf("ClaimRunnableItem: %v", err)
	}
	if !acquired || claimed == nil {
		t.Fatalf("expected workflow claim")
	}
	if _, err := service.RecoverStaleClaims(RunDueRequest{Limit: 5}); err != nil {
		t.Fatalf("RecoverStaleClaims: %v", err)
	}
	recovered, err := service.Get(record.Item.ID)
	if err != nil {
		t.Fatalf("Get recovered workflow: %v", err)
	}
	// This helper models the original runner having returned after recovery;
	// tests that keep the runner blocked exercise the retained fence directly.
	if claimed.WorkerClaimID != "" {
		if _, err := repo.ReleaseInterruptedExecutionClaim(record.Item.ID, claimed.WorkerClaimID); err != nil {
			t.Fatalf("release simulated returned runner fence: %v", err)
		}
	}
	return recovered
}

func findRule(rules []models.WorkflowRule, ruleKey string) (models.WorkflowRule, bool) {
	for _, rule := range rules {
		if rule.RuleKey == ruleKey {
			return rule, true
		}
	}
	return models.WorkflowRule{}, false
}

func timePtr(value time.Time) *time.Time {
	return &value
}

func testFrameworkSelection(planID string) FrameworkSelectionProvenance {
	return FrameworkSelectionProvenance{
		SelectionDecisionID:       uuid.NewString(),
		TaskPlanID:                planID,
		CatalogVersion:            "v1",
		CatalogDigest:             strings.Repeat("a", 64),
		SelectorAlgorithmVersion:  "chief-of-staff-v1",
		EffectivePreferenceDigest: strings.Repeat("b", 64),
		ConstitutionVersion:       1,
		ConstitutionDigest:        strings.Repeat("c", 64),
		ConstitutionSource:        "builtin-robert-constitution-v1:v1",
	}
}

type fakeWorkflowRepo struct {
	items                        map[uuid.UUID]*models.WorkflowItem
	checklist                    map[uuid.UUID][]models.WorkflowChecklistItem
	intake                       map[uuid.UUID][]models.WorkflowIntakeRecord
	matches                      map[uuid.UUID][]models.WorkflowProjectMatch
	pursuits                     map[uuid.UUID][]WorkflowPursuitContext
	evidence                     map[uuid.UUID][]models.WorkflowEvidenceClaim
	openLoops                    map[uuid.UUID][]models.WorkflowOpenLoop
	proposals                    map[uuid.UUID][]models.WorkflowProposal
	qualityGate                  map[uuid.UUID][]models.WorkflowQualityGate
	rules                        map[string]models.WorkflowRule
	transitions                  map[uuid.UUID][]models.WorkflowTransition
	sourceLinks                  map[uuid.UUID][]models.WorkflowSourceLink
	decisions                    map[uuid.UUID][]models.WorkflowDecision
	decisionWorkflow             map[uuid.UUID]uuid.UUID
	events                       map[uuid.UUID][]models.WorkflowEvent
	attestations                 map[uuid.UUID]models.WorkflowCompletionAttestation
	rejectWorkflowClaims         bool
	rejectOpenLoopClaims         bool
	createQualityGateErr         error
	findQualityGatesErr          error
	updateQualityGateErr         error
	findSourceLinksErr           error
	findEvidenceClaimsErr        error
	saveIntakeRecordErr          error
	createSourceLinkErr          error
	findIntakeRecordsErr         error
	resolvePendingApprovalErr    error
	createDeadlineReminderErr    error
	createChecklistItemErr       error
	createProposalErr            error
	createEvidenceClaimErr       error
	loseRunnableItemRenewal      bool
	loseRunnableItemRenewalAfter int
	runnableItemRenewalCalls     int
}

type staleURILookupWorkflowRepo struct {
	*fakeWorkflowRepo
	preflightMisses       int
	idempotentCreateCalls int
}

type concurrentApprovalWorkflowRepo struct {
	*fakeWorkflowRepo
	arrivals chan struct{}
	release  chan struct{}
	mu       sync.Mutex
}

func (r *concurrentApprovalWorkflowRepo) ResolvePendingApproval(
	id uuid.UUID,
	resolution ApprovalResolutionMutation,
) (*models.WorkflowItem, bool, error) {
	r.arrivals <- struct{}{}
	<-r.release
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.fakeWorkflowRepo.ResolvePendingApproval(id, resolution)
}

func (r *staleURILookupWorkflowRepo) FindActiveItemBySourceURIForOwner(ownerIdentity, sourceURI string) (*models.WorkflowItem, error) {
	if r.preflightMisses > 0 {
		r.preflightMisses--
		return nil, nil
	}
	return r.fakeWorkflowRepo.FindActiveItemBySourceURIForOwner(ownerIdentity, sourceURI)
}

func (r *staleURILookupWorkflowRepo) CreateItemIdempotent(item *models.WorkflowItem) (*models.WorkflowItem, bool, error) {
	r.idempotentCreateCalls++
	var existing *models.WorkflowItem
	var err error
	if strings.TrimSpace(item.SourceType) != "" && strings.TrimSpace(item.SourceID) != "" {
		existing, err = r.fakeWorkflowRepo.FindActiveItemBySourceIdentityForOwner(item.OwnerIdentity, item.SourceType, item.SourceID)
	} else if strings.TrimSpace(item.SourceURI) != "" {
		existing, err = r.fakeWorkflowRepo.FindActiveItemBySourceURIForOwner(item.OwnerIdentity, item.SourceURI)
	}
	if err != nil {
		return nil, false, err
	}
	if existing != nil {
		if existing.SourceRevision != item.SourceRevision {
			return nil, false, fmt.Errorf("workflow source identity is already active with a different revision")
		}
		return existing, false, nil
	}
	created, err := r.fakeWorkflowRepo.CreateItem(item)
	return created, err == nil, err
}

func newFakeWorkflowRepo() *fakeWorkflowRepo {
	return &fakeWorkflowRepo{
		items:            map[uuid.UUID]*models.WorkflowItem{},
		checklist:        map[uuid.UUID][]models.WorkflowChecklistItem{},
		intake:           map[uuid.UUID][]models.WorkflowIntakeRecord{},
		matches:          map[uuid.UUID][]models.WorkflowProjectMatch{},
		pursuits:         map[uuid.UUID][]WorkflowPursuitContext{},
		evidence:         map[uuid.UUID][]models.WorkflowEvidenceClaim{},
		openLoops:        map[uuid.UUID][]models.WorkflowOpenLoop{},
		proposals:        map[uuid.UUID][]models.WorkflowProposal{},
		qualityGate:      map[uuid.UUID][]models.WorkflowQualityGate{},
		rules:            map[string]models.WorkflowRule{},
		transitions:      map[uuid.UUID][]models.WorkflowTransition{},
		sourceLinks:      map[uuid.UUID][]models.WorkflowSourceLink{},
		decisions:        map[uuid.UUID][]models.WorkflowDecision{},
		decisionWorkflow: map[uuid.UUID]uuid.UUID{},
		events:           map[uuid.UUID][]models.WorkflowEvent{},
		attestations:     map[uuid.UUID]models.WorkflowCompletionAttestation{},
	}
}

func (r *fakeWorkflowRepo) CreateItem(item *models.WorkflowItem) (*models.WorkflowItem, error) {
	if item.ID == uuid.Nil {
		item.ID = uuid.New()
	}
	now := time.Now().UTC()
	item.CreatedAt = now
	item.UpdatedAt = now
	r.items[item.ID] = item
	return item, nil
}

func (r *fakeWorkflowRepo) CreateItemIdempotent(item *models.WorkflowItem) (*models.WorkflowItem, bool, error) {
	if item == nil {
		return nil, false, fmt.Errorf("workflow item is required")
	}
	var existing *models.WorkflowItem
	var err error
	if strings.TrimSpace(item.SourceType) != "" && strings.TrimSpace(item.SourceID) != "" {
		existing, err = r.FindActiveItemBySourceIdentityForOwner(item.OwnerIdentity, item.SourceType, item.SourceID)
	} else if strings.TrimSpace(item.SourceURI) != "" {
		existing, err = r.FindActiveItemBySourceURIForOwner(item.OwnerIdentity, item.SourceURI)
	}
	if err != nil {
		return nil, false, err
	}
	if existing != nil {
		if existing.SourceRevision != item.SourceRevision {
			return nil, false, fmt.Errorf("workflow source identity is already active with a different revision")
		}
		return existing, false, nil
	}
	created, err := r.CreateItem(item)
	return created, err == nil, err
}

func (r *fakeWorkflowRepo) UpdateItem(item *models.WorkflowItem) (*models.WorkflowItem, error) {
	item.UpdatedAt = time.Now().UTC()
	r.items[item.ID] = item
	return item, nil
}

func (r *fakeWorkflowRepo) UpdateWorkflowItemCAS(expected, updated *models.WorkflowItem) (*models.WorkflowItem, bool, error) {
	stored, ok := r.items[expected.ID]
	if !ok || updated == nil || stored.OwnerIdentity != expected.OwnerIdentity ||
		stored.CurrentState != expected.CurrentState || !stored.UpdatedAt.Equal(expected.UpdatedAt) ||
		stored.RecoveryStatus != expected.RecoveryStatus || stored.Archived != expected.Archived ||
		stored.WorkerClaimID != "" || stored.WorkerLeaseUntil != nil ||
		(expected.RecoveryStatus == RecoveryNeedsReview && updated.CurrentState != StateBlocked) {
		return nil, false, nil
	}
	copy := *updated
	copy.UpdatedAt = nextWorkflowRevision(expected.UpdatedAt)
	r.items[copy.ID] = &copy
	returned := copy
	return &returned, true, nil
}

func (r *fakeWorkflowRepo) CommitWorkflowIntake(finalization WorkflowIntakeFinalization) (*models.WorkflowItem, bool, error) {
	if finalization.Expected == nil || finalization.Updated == nil {
		return nil, false, fmt.Errorf("expected and updated workflow items are required")
	}
	if hasWorkflowEvent(r.events[finalization.Expected.ID], "workflow.source_retracted") {
		return nil, false, nil
	}
	item, changed, err := r.UpdateWorkflowItemCAS(finalization.Expected, finalization.Updated)
	if err != nil || !changed {
		return item, changed, err
	}
	if _, err := r.CreateTransition(&finalization.Transition); err != nil {
		return nil, false, err
	}
	for index := range finalization.Decisions {
		if _, err := r.CreateDecision(&finalization.Decisions[index]); err != nil {
			return nil, false, err
		}
	}
	for index := range finalization.Events {
		if _, err := r.CreateEvent(&finalization.Events[index]); err != nil {
			return nil, false, err
		}
	}
	return item, true, nil
}

func (r *fakeWorkflowRepo) CommitWorkflowCoordinationProjection(finalization WorkflowCoordinationProjectionFinalization) (*models.WorkflowItem, bool, error) {
	return r.commitWorkflowCoordination(finalization)
}

func (r *fakeWorkflowRepo) CommitWorkflowCoordinationFailure(finalization WorkflowCoordinationProjectionFinalization) (*models.WorkflowItem, bool, error) {
	return r.commitWorkflowCoordination(finalization)
}

func (r *fakeWorkflowRepo) commitWorkflowCoordination(finalization WorkflowCoordinationProjectionFinalization) (*models.WorkflowItem, bool, error) {
	if finalization.Expected == nil || finalization.Updated == nil ||
		hasWorkflowEvent(r.events[finalization.Expected.ID], "workflow.source_retracted") {
		return nil, false, nil
	}
	decisionMissing := true
	for _, decision := range r.decisions[finalization.Expected.ID] {
		if decision.DecisionType == finalization.Decision.DecisionType && decision.Decision == finalization.Decision.Decision &&
			decision.Reason == finalization.Decision.Reason && decision.RuleApplied == finalization.Decision.RuleApplied &&
			decision.Actor == finalization.Decision.Actor {
			decisionMissing = false
			break
		}
	}
	eventMissing := true
	for _, event := range r.events[finalization.Expected.ID] {
		if event.EventType == finalization.Event.EventType && event.Message == finalization.Event.Message {
			eventMissing = false
			break
		}
	}
	item, changed, err := r.UpdateWorkflowItemCAS(finalization.Expected, finalization.Updated)
	if err != nil || !changed || item == nil {
		return item, changed, err
	}
	if finalization.Transition != nil {
		if _, err := r.CreateTransition(finalization.Transition); err != nil {
			return nil, false, err
		}
	}
	if decisionMissing {
		if _, err := r.CreateDecision(&finalization.Decision); err != nil {
			return nil, false, err
		}
	}
	if eventMissing {
		if _, err := r.CreateEvent(&finalization.Event); err != nil {
			return nil, false, err
		}
	}
	return item, true, nil
}

func (r *fakeWorkflowRepo) ResolveInterruptedExecutionCAS(
	expected, updated *models.WorkflowItem,
	sourceLink *models.WorkflowSourceLink,
	evidenceClaim *models.WorkflowEvidenceClaim,
	qualityGate *models.WorkflowQualityGate,
) (*models.WorkflowItem, bool, error) {
	stored, ok := r.items[expected.ID]
	if !ok || updated == nil || stored.OwnerIdentity != expected.OwnerIdentity ||
		stored.CurrentState != StateBlocked || stored.RecoveryStatus != RecoveryNeedsReview ||
		!stored.UpdatedAt.Equal(expected.UpdatedAt) || stored.WorkerClaimID != expected.WorkerClaimID ||
		!sameOptionalTime(stored.WorkerLeaseUntil, expected.WorkerLeaseUntil) || stored.Archived != expected.Archived {
		return nil, false, nil
	}
	copy := *updated
	copy.UpdatedAt = nextWorkflowRevision(expected.UpdatedAt)
	if copy.CurrentState != StateBlocked {
		copy.WorkerClaimID = ""
		copy.WorkerLeaseUntil = nil
	}
	r.items[copy.ID] = &copy
	if sourceLink != nil {
		r.sourceLinks[copy.ID] = append(r.sourceLinks[copy.ID], *sourceLink)
	}
	if evidenceClaim != nil {
		r.evidence[copy.ID] = append(r.evidence[copy.ID], *evidenceClaim)
	}
	if qualityGate != nil {
		found := false
		for index := range r.qualityGate[copy.ID] {
			if strings.EqualFold(r.qualityGate[copy.ID][index].Gate, qualityGate.Gate) {
				r.qualityGate[copy.ID][index] = *qualityGate
				found = true
			}
		}
		if !found {
			r.qualityGate[copy.ID] = append(r.qualityGate[copy.ID], *qualityGate)
		}
	}
	returned := copy
	return &returned, true, nil
}

func (r *fakeWorkflowRepo) ReleaseInterruptedExecutionClaim(id uuid.UUID, claimID string) (bool, error) {
	stored, ok := r.items[id]
	if !ok || stored.CurrentState != StateBlocked || stored.RecoveryStatus != RecoveryNeedsReview || stored.WorkerClaimID != claimID {
		return false, nil
	}
	updated := *stored
	updated.WorkerClaimID = ""
	updated.WorkerLeaseUntil = nil
	updated.UpdatedAt = nextWorkflowRevision(stored.UpdatedAt)
	r.items[id] = &updated
	return true, nil
}

func sameOptionalTime(left, right *time.Time) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}

func (r *fakeWorkflowRepo) SupersedeSourceWorkflowCAS(expected SourceWorkflowSupersession) (bool, error) {
	item, ok := r.items[expected.ID]
	if !ok || item == nil || item.Archived || item.OwnerIdentity != expected.OwnerIdentity ||
		item.SourceType != expected.SourceType || item.SourceID != expected.SourceID ||
		item.SourceRevision != expected.ExpectedSourceRevision || item.CurrentState != expected.ExpectedCurrentState ||
		strings.TrimSpace(item.WorkerClaimID) != "" {
		return false, nil
	}
	updated := *item
	updated.Archived = true
	updated.CurrentState = StateArchived
	updated.NextAction = "superseded by revised source content"
	updated.NextRunAt = nil
	updated.WorkerClaimID = ""
	updated.WorkerLeaseUntil = nil
	updated.UpdatedAt = time.Now().UTC()
	r.items[expected.ID] = &updated
	return true, nil
}

func (r *fakeWorkflowRepo) ResolvePendingApproval(
	id uuid.UUID,
	resolution ApprovalResolutionMutation,
) (*models.WorkflowItem, bool, error) {
	if r.resolvePendingApprovalErr != nil {
		return nil, false, r.resolvePendingApprovalErr
	}
	item, ok := r.items[id]
	if !ok || item.Archived || item.CurrentState != StateNeedsApproval ||
		!item.RequiresApproval || item.ApprovalStatus != "pending" {
		return nil, false, nil
	}
	updated := *item
	if resolution.Approved {
		updated.CurrentState = StateReady
		updated.ApprovalStatus = "approved"
		updated.BlockedReason = ""
		updated.NextAction = "execute approved workflow steps"
	} else {
		updated.CurrentState = StateBlocked
		updated.ApprovalStatus = "rejected"
		updated.BlockedReason = firstNonEmpty(strings.TrimSpace(resolution.RejectionReason), "approval rejected")
		updated.NextAction = "review rejection reason before continuing"
	}
	updated.UpdatedAt = time.Now().UTC()
	r.items[id] = &updated
	decision := "rejected"
	if resolution.Approved {
		decision = "approved"
	}
	transition := models.WorkflowTransition{
		ID: uuid.New(), WorkflowID: id, FromState: StateNeedsApproval,
		ToState: updated.CurrentState, Trigger: "approval_resolution",
		Actor: resolution.Actor, Approved: resolution.Approved,
		Reason: resolution.RejectionReason, CreatedAt: updated.UpdatedAt,
	}
	r.transitions[id] = append([]models.WorkflowTransition{transition}, r.transitions[id]...)
	approvalDecision := models.WorkflowDecision{
		ID: uuid.New(), WorkflowID: id, DecisionType: "approval", Decision: decision,
		Reason: resolution.RejectionReason, RuleApplied: resolution.DecisionRule,
		Approved: resolution.Approved, Actor: resolution.Actor, CreatedAt: updated.UpdatedAt,
	}
	r.decisions[id] = append([]models.WorkflowDecision{approvalDecision}, r.decisions[id]...)
	r.decisionWorkflow[approvalDecision.ID] = id
	event := models.WorkflowEvent{
		ID: uuid.New(), WorkflowID: id, EventType: "workflow.approval",
		FromState: StateNeedsApproval, ToState: updated.CurrentState,
		Message: resolution.RejectionReason, Trigger: "approval_resolution",
		RuleApplied: resolution.DecisionRule, SourceURI: updated.SourceURI,
		Actor: resolution.Actor, CreatedAt: updated.UpdatedAt,
	}
	r.events[id] = append([]models.WorkflowEvent{event}, r.events[id]...)
	return &updated, true, nil
}

func (r *fakeWorkflowRepo) FindItem(id uuid.UUID) (*models.WorkflowItem, error) {
	item, ok := r.items[id]
	if !ok {
		return nil, gorm.ErrRecordNotFound
	}
	copied := *item
	return &copied, nil
}

func (r *fakeWorkflowRepo) FindActiveItemBySourceIdentity(sourceType, sourceID string) (*models.WorkflowItem, error) {
	for _, item := range r.items {
		if item.SourceType == sourceType && item.SourceID == sourceID && !item.Archived {
			copied := *item
			return &copied, nil
		}
	}
	return nil, nil
}

func (r *fakeWorkflowRepo) FindActiveItemBySourceURI(sourceURI string) (*models.WorkflowItem, error) {
	for _, item := range r.items {
		if item.SourceURI == sourceURI && !item.Archived {
			copied := *item
			return &copied, nil
		}
	}
	return nil, nil
}

func (r *fakeWorkflowRepo) FindActiveItemBySourceIdentityForOwner(ownerIdentity, sourceType, sourceID string) (*models.WorkflowItem, error) {
	if ownerIdentity == "" {
		return r.FindActiveItemBySourceIdentity(sourceType, sourceID)
	}
	for _, item := range r.items {
		if item.OwnerIdentity == ownerIdentity && item.SourceType == sourceType && item.SourceID == sourceID && !item.Archived {
			copied := *item
			return &copied, nil
		}
	}
	return nil, nil
}

func (r *fakeWorkflowRepo) FindActiveItemBySourceURIForOwner(ownerIdentity, sourceURI string) (*models.WorkflowItem, error) {
	if ownerIdentity == "" {
		return r.FindActiveItemBySourceURI(sourceURI)
	}
	for _, item := range r.items {
		if item.OwnerIdentity == ownerIdentity && item.SourceURI == sourceURI && !item.Archived {
			copied := *item
			return &copied, nil
		}
	}
	return nil, nil
}

func (r *fakeWorkflowRepo) FindItems(includeArchived bool) ([]models.WorkflowItem, error) {
	result := []models.WorkflowItem{}
	for _, item := range r.items {
		if includeArchived || !item.Archived {
			result = append(result, *item)
		}
	}
	return result, nil
}

func (r *fakeWorkflowRepo) FindApprovalItems() ([]models.WorkflowItem, error) {
	result := []models.WorkflowItem{}
	for _, item := range r.items {
		if item.CurrentState == StateNeedsApproval && !item.Archived {
			result = append(result, *item)
		}
	}
	return result, nil
}

func (r *fakeWorkflowRepo) FindRunnableItems(now time.Time, limit int) ([]models.WorkflowItem, error) {
	result := []models.WorkflowItem{}
	for _, item := range r.items {
		if item.CurrentState != StateReady || item.RecoveryStatus == RecoveryNeedsReview || item.Archived || item.RetryCount >= item.MaxRetries {
			continue
		}
		if item.RequiresApproval && item.ApprovalStatus != "approved" {
			continue
		}
		if item.NextRunAt != nil && item.NextRunAt.After(now) {
			continue
		}
		result = append(result, *item)
	}
	if limit > 0 && len(result) > limit {
		return result[:limit], nil
	}
	return result, nil
}

func (r *fakeWorkflowRepo) FindRunnableItemsForOwner(ownerIdentity string, now time.Time, limit int) ([]models.WorkflowItem, error) {
	if ownerIdentity == "" {
		return r.FindRunnableItems(now, limit)
	}
	items, err := r.FindRunnableItems(now, 0)
	if err != nil {
		return nil, err
	}
	owned := make([]models.WorkflowItem, 0, len(items))
	for _, item := range items {
		if item.OwnerIdentity == ownerIdentity {
			owned = append(owned, item)
		}
	}
	if limit > 0 && len(owned) > limit {
		return owned[:limit], nil
	}
	return owned, nil
}

func (r *fakeWorkflowRepo) ClaimRunnableItem(id uuid.UUID, claimID string, now time.Time, leaseUntil time.Time) (*models.WorkflowItem, bool, error) {
	if r.rejectWorkflowClaims {
		return nil, false, nil
	}
	item, ok := r.items[id]
	if !ok || item.Archived || item.CurrentState != StateReady || item.RecoveryStatus == RecoveryNeedsReview || item.RetryCount >= item.MaxRetries {
		return nil, false, nil
	}
	if item.RequiresApproval && item.ApprovalStatus != "approved" {
		return nil, false, nil
	}
	if item.NextRunAt != nil && item.NextRunAt.After(now) {
		return nil, false, nil
	}
	item.CurrentState = StateInProgress
	item.LastRunAt = timePtr(now)
	item.NextAction = "task engine is executing claimed workflow item"
	item.LastWorkerError = ""
	item.WorkerClaimID = claimID
	item.WorkerLeaseUntil = timePtr(leaseUntil)
	item.UpdatedAt = now
	copied := *item
	return &copied, true, nil
}

func (r *fakeWorkflowRepo) ClaimRunnableItemForOwner(ownerIdentity string, id uuid.UUID, claimID string, now time.Time, leaseUntil time.Time) (*models.WorkflowItem, bool, error) {
	if ownerIdentity != "" {
		item, ok := r.items[id]
		if !ok || item.OwnerIdentity != ownerIdentity {
			return nil, false, nil
		}
	}
	return r.ClaimRunnableItem(id, claimID, now, leaseUntil)
}

func (r *fakeWorkflowRepo) RenewRunnableItemClaim(id uuid.UUID, claimID string, leaseUntil time.Time) (bool, error) {
	r.runnableItemRenewalCalls++
	if r.loseRunnableItemRenewal || (r.loseRunnableItemRenewalAfter > 0 && r.runnableItemRenewalCalls > r.loseRunnableItemRenewalAfter) {
		return false, nil
	}
	item, ok := r.items[id]
	if !ok || item.CurrentState != StateInProgress || item.WorkerClaimID != claimID {
		return false, nil
	}
	item.WorkerLeaseUntil = timePtr(leaseUntil)
	item.UpdatedAt = time.Now().UTC()
	return true, nil
}

func (r *fakeWorkflowRepo) UpdateClaimedItem(item *models.WorkflowItem, claimID string) (*models.WorkflowItem, bool, error) {
	stored, ok := r.items[item.ID]
	if !ok || stored.CurrentState != StateInProgress || stored.WorkerClaimID != claimID {
		return nil, false, nil
	}
	copied := *item
	copied.WorkerClaimID = ""
	copied.WorkerLeaseUntil = nil
	copied.UpdatedAt = time.Now().UTC()
	r.items[item.ID] = &copied
	result := copied
	return &result, true, nil
}

func (r *fakeWorkflowRepo) CompleteClaimedItem(item *models.WorkflowItem, claimID string, attestation *models.WorkflowCompletionAttestation) (*models.WorkflowItem, bool, error) {
	if attestation == nil || attestation.WorkflowID != item.ID || attestation.RecordDigest == "" {
		return nil, false, fmt.Errorf("valid workflow completion attestation is required")
	}
	if _, exists := r.attestations[item.ID]; exists {
		return nil, false, fmt.Errorf("workflow completion attestation already exists")
	}
	updated, owned, err := r.UpdateClaimedItem(item, claimID)
	if err != nil || !owned {
		return updated, owned, err
	}
	r.attestations[item.ID] = *attestation
	return updated, true, nil
}

func (r *fakeWorkflowRepo) FindExpiredWorkflowClaims(now time.Time, limit int) ([]models.WorkflowItem, error) {
	result := []models.WorkflowItem{}
	for _, item := range r.items {
		if item.CurrentState != StateInProgress {
			continue
		}
		expiredLease := item.WorkerClaimID != "" && item.WorkerLeaseUntil != nil && !item.WorkerLeaseUntil.After(now)
		expiredLegacyClaim := item.WorkerClaimID == "" && item.WorkerLeaseUntil == nil && item.LastRunAt != nil && !item.LastRunAt.After(now.Add(-claimLeaseDuration()))
		if !expiredLease && !expiredLegacyClaim {
			continue
		}
		result = append(result, *item)
	}
	if limit > 0 && len(result) > limit {
		return result[:limit], nil
	}
	return result, nil
}

func (r *fakeWorkflowRepo) FindExpiredWorkflowClaimsForOwner(ownerIdentity string, now time.Time, limit int) ([]models.WorkflowItem, error) {
	if ownerIdentity == "" {
		return r.FindExpiredWorkflowClaims(now, limit)
	}
	items, err := r.FindExpiredWorkflowClaims(now, 0)
	if err != nil {
		return nil, err
	}
	owned := make([]models.WorkflowItem, 0, len(items))
	for _, item := range items {
		if item.OwnerIdentity == ownerIdentity {
			owned = append(owned, item)
		}
	}
	if limit > 0 && len(owned) > limit {
		return owned[:limit], nil
	}
	return owned, nil
}

func (r *fakeWorkflowRepo) RecoverExpiredWorkflowClaim(item models.WorkflowItem, now time.Time) (*models.WorkflowItem, bool, error) {
	stored, ok := r.items[item.ID]
	if !ok || stored.CurrentState != StateInProgress || stored.WorkerClaimID != item.WorkerClaimID || !stored.UpdatedAt.Equal(item.UpdatedAt) {
		return nil, false, nil
	}
	expiredLease := stored.WorkerClaimID != "" && stored.WorkerLeaseUntil != nil && !stored.WorkerLeaseUntil.After(now)
	expiredLegacyClaim := stored.WorkerClaimID == "" && stored.WorkerLeaseUntil == nil && stored.LastRunAt != nil && !stored.LastRunAt.After(now.Add(-claimLeaseDuration()))
	if !expiredLease && !expiredLegacyClaim {
		return nil, false, nil
	}
	stored.CurrentState = StateBlocked
	stored.BlockedReason = "worker lease expired; execution outcome is unknown and requires human review"
	stored.NextAction = "review external side effects before retrying interrupted workflow"
	stored.LastWorkerError = stored.BlockedReason
	stored.RecoveryStatus = RecoveryNeedsReview
	stored.RecoveryNote = ""
	stored.RetryCount++
	stored.NextRunAt = nil
	if stored.WorkerClaimID == "" {
		stored.WorkerClaimID = "legacy-recovery:" + uuid.NewString()
	}
	stored.UpdatedAt = nextWorkflowRevision(item.UpdatedAt)
	copied := *stored
	return &copied, true, nil
}

func (r *fakeWorkflowRepo) CreateChecklistItem(item *models.WorkflowChecklistItem) (*models.WorkflowChecklistItem, error) {
	if r.createChecklistItemErr != nil {
		return nil, r.createChecklistItemErr
	}
	if item.Label == "Follow up or check before detected deadline" && r.createDeadlineReminderErr != nil {
		return nil, r.createDeadlineReminderErr
	}
	if item.ID == uuid.Nil {
		item.ID = uuid.New()
	}
	now := time.Now().UTC()
	item.CreatedAt = now
	item.UpdatedAt = now
	r.checklist[item.WorkflowID] = append(r.checklist[item.WorkflowID], *item)
	return item, nil
}

func (r *fakeWorkflowRepo) UpdateChecklistItem(item *models.WorkflowChecklistItem) (*models.WorkflowChecklistItem, error) {
	item.UpdatedAt = time.Now().UTC()
	items := r.checklist[item.WorkflowID]
	for index := range items {
		if items[index].ID == item.ID {
			items[index] = *item
			r.checklist[item.WorkflowID] = items
			return item, nil
		}
	}
	return nil, gorm.ErrRecordNotFound
}

func (r *fakeWorkflowRepo) FindChecklist(workflowID uuid.UUID) ([]models.WorkflowChecklistItem, error) {
	return append([]models.WorkflowChecklistItem{}, r.checklist[workflowID]...), nil
}

func (r *fakeWorkflowRepo) FindReminderCandidatesForOwner(
	ownerIdentity string,
	before time.Time,
	limit int,
) ([]WorkflowReminderCandidate, error) {
	result := []WorkflowReminderCandidate{}
	for workflowID, checklist := range r.checklist {
		workflow, ok := r.items[workflowID]
		if !ok || workflow.OwnerIdentity != ownerIdentity || workflow.Archived ||
			workflow.CurrentState == StateCompleted || workflow.CurrentState == StateArchived {
			continue
		}
		for _, item := range checklist {
			if item.Status != "open" || item.ReminderAt == nil || item.ReminderAt.After(before) {
				continue
			}
			result = append(result, WorkflowReminderCandidate{Workflow: *workflow, Reminder: item})
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Reminder.ReminderAt.Before(*result[j].Reminder.ReminderAt)
	})
	if len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

func (r *fakeWorkflowRepo) SaveIntakeRecord(record *models.WorkflowIntakeRecord) (*models.WorkflowIntakeRecord, error) {
	if r.saveIntakeRecordErr != nil {
		return nil, r.saveIntakeRecordErr
	}
	if record.ID == uuid.Nil {
		record.ID = uuid.New()
	}
	record.CreatedAt = time.Now().UTC()
	r.intake[record.WorkflowID] = append([]models.WorkflowIntakeRecord{*record}, r.intake[record.WorkflowID]...)
	return record, nil
}

func (r *fakeWorkflowRepo) FindIntakeRecords(workflowID uuid.UUID) ([]models.WorkflowIntakeRecord, error) {
	if r.findIntakeRecordsErr != nil {
		return nil, r.findIntakeRecordsErr
	}
	return append([]models.WorkflowIntakeRecord{}, r.intake[workflowID]...), nil
}

func (r *fakeWorkflowRepo) CreateProjectMatch(match *models.WorkflowProjectMatch) (*models.WorkflowProjectMatch, error) {
	if match.ID == uuid.Nil {
		match.ID = uuid.New()
	}
	match.CreatedAt = time.Now().UTC()
	r.matches[match.WorkflowID] = append([]models.WorkflowProjectMatch{*match}, r.matches[match.WorkflowID]...)
	return match, nil
}

func (r *fakeWorkflowRepo) FindProjectMatches(workflowID uuid.UUID) ([]models.WorkflowProjectMatch, error) {
	return append([]models.WorkflowProjectMatch{}, r.matches[workflowID]...), nil
}

func (r *fakeWorkflowRepo) FindLinkedPursuits(workflowID uuid.UUID) ([]WorkflowPursuitContext, error) {
	return append([]WorkflowPursuitContext{}, r.pursuits[workflowID]...), nil
}

func (r *fakeWorkflowRepo) CreateEvidenceClaim(claim *models.WorkflowEvidenceClaim) (*models.WorkflowEvidenceClaim, error) {
	if r.createEvidenceClaimErr != nil {
		return nil, r.createEvidenceClaimErr
	}
	if claim.ID == uuid.Nil {
		claim.ID = uuid.New()
	}
	claim.CreatedAt = time.Now().UTC()
	r.evidence[claim.WorkflowID] = append([]models.WorkflowEvidenceClaim{*claim}, r.evidence[claim.WorkflowID]...)
	return claim, nil
}

func (r *fakeWorkflowRepo) FindEvidenceClaims(workflowID uuid.UUID) ([]models.WorkflowEvidenceClaim, error) {
	if r.findEvidenceClaimsErr != nil {
		return nil, r.findEvidenceClaimsErr
	}
	return append([]models.WorkflowEvidenceClaim{}, r.evidence[workflowID]...), nil
}

func (r *fakeWorkflowRepo) CreateOpenLoop(loop *models.WorkflowOpenLoop) (*models.WorkflowOpenLoop, error) {
	if loop.ID == uuid.Nil {
		loop.ID = uuid.New()
	}
	now := time.Now().UTC()
	loop.CreatedAt = now
	loop.UpdatedAt = now
	r.openLoops[loop.WorkflowID] = append([]models.WorkflowOpenLoop{*loop}, r.openLoops[loop.WorkflowID]...)
	return loop, nil
}

func (r *fakeWorkflowRepo) UpdateOpenLoop(loop *models.WorkflowOpenLoop) (*models.WorkflowOpenLoop, error) {
	loop.UpdatedAt = time.Now().UTC()
	loops := r.openLoops[loop.WorkflowID]
	for index := range loops {
		if loops[index].ID == loop.ID {
			loops[index] = *loop
			r.openLoops[loop.WorkflowID] = loops
			return loop, nil
		}
	}
	return nil, gorm.ErrRecordNotFound
}

func (r *fakeWorkflowRepo) FindOpenLoops(workflowID uuid.UUID) ([]models.WorkflowOpenLoop, error) {
	return append([]models.WorkflowOpenLoop{}, r.openLoops[workflowID]...), nil
}

func (r *fakeWorkflowRepo) FindDashboardOpenLoops(now time.Time) ([]models.WorkflowOpenLoop, error) {
	result := []models.WorkflowOpenLoop{}
	for workflowID, loops := range r.openLoops {
		item := r.items[workflowID]
		if item == nil || item.Archived || item.CurrentState == StateArchived || item.CurrentState == StateCompleted {
			continue
		}
		for _, loop := range loops {
			if loop.Status != "open" {
				continue
			}
			if loop.FollowUpAt == nil || !loop.FollowUpAt.After(now) {
				result = append(result, loop)
			}
		}
	}
	return result, nil
}

func (r *fakeWorkflowRepo) FindDashboardOpenLoopsForOwner(ownerIdentity string, now time.Time) ([]models.WorkflowOpenLoop, error) {
	if ownerIdentity == "" {
		return r.FindDashboardOpenLoops(now)
	}
	loops, err := r.FindDashboardOpenLoops(now)
	if err != nil {
		return nil, err
	}
	owned := make([]models.WorkflowOpenLoop, 0, len(loops))
	for _, loop := range loops {
		if item := r.items[loop.WorkflowID]; item != nil && item.OwnerIdentity == ownerIdentity {
			owned = append(owned, loop)
		}
	}
	return owned, nil
}

func (r *fakeWorkflowRepo) ClaimDueOpenLoop(id uuid.UUID, claimID string, now time.Time, leaseUntil time.Time) (*models.WorkflowOpenLoop, bool, error) {
	if r.rejectOpenLoopClaims {
		return nil, false, nil
	}
	for workflowID, loops := range r.openLoops {
		item := r.items[workflowID]
		if item == nil || item.Archived || item.CurrentState == StateArchived || item.CurrentState == StateCompleted {
			continue
		}
		for index := range loops {
			if loops[index].ID != id || loops[index].Status != "open" {
				continue
			}
			if loops[index].FollowUpAt != nil && loops[index].FollowUpAt.After(now) {
				return nil, false, nil
			}
			loops[index].Status = "processing"
			loops[index].ClaimID = claimID
			loops[index].LeaseUntil = timePtr(leaseUntil)
			loops[index].UpdatedAt = now
			r.openLoops[workflowID] = loops
			copied := loops[index]
			return &copied, true, nil
		}
	}
	return nil, false, nil
}

func (r *fakeWorkflowRepo) ClaimDueOpenLoopForOwner(ownerIdentity string, id uuid.UUID, claimID string, now time.Time, leaseUntil time.Time) (*models.WorkflowOpenLoop, bool, error) {
	if ownerIdentity != "" {
		for workflowID, loops := range r.openLoops {
			for _, loop := range loops {
				if loop.ID == id {
					item := r.items[workflowID]
					if item == nil || item.OwnerIdentity != ownerIdentity {
						return nil, false, nil
					}
				}
			}
		}
	}
	return r.ClaimDueOpenLoop(id, claimID, now, leaseUntil)
}

func (r *fakeWorkflowRepo) RenewOpenLoopClaim(id uuid.UUID, claimID string, leaseUntil time.Time) (bool, error) {
	for workflowID, loops := range r.openLoops {
		for index := range loops {
			if loops[index].ID != id || loops[index].Status != "processing" || loops[index].ClaimID != claimID {
				continue
			}
			loops[index].LeaseUntil = timePtr(leaseUntil)
			loops[index].UpdatedAt = time.Now().UTC()
			r.openLoops[workflowID] = loops
			return true, nil
		}
	}
	return false, nil
}

func (r *fakeWorkflowRepo) UpdateClaimedOpenLoop(loop *models.WorkflowOpenLoop, claimID string) (*models.WorkflowOpenLoop, bool, error) {
	for workflowID, loops := range r.openLoops {
		for index := range loops {
			stored := &loops[index]
			if stored.ID != loop.ID || stored.Status != "processing" || stored.ClaimID != claimID {
				continue
			}
			stored.Status = loop.Status
			stored.ClaimID = ""
			stored.LeaseUntil = nil
			stored.UpdatedAt = time.Now().UTC()
			r.openLoops[workflowID] = loops
			copied := *stored
			return &copied, true, nil
		}
	}
	return nil, false, nil
}

func (r *fakeWorkflowRepo) FindExpiredOpenLoopClaims(now time.Time, limit int) ([]models.WorkflowOpenLoop, error) {
	result := []models.WorkflowOpenLoop{}
	for _, loops := range r.openLoops {
		for _, loop := range loops {
			if loop.Status != "processing" {
				continue
			}
			expiredLease := loop.ClaimID != "" && loop.LeaseUntil != nil && !loop.LeaseUntil.After(now)
			expiredLegacyClaim := loop.ClaimID == "" && loop.LeaseUntil == nil && !loop.UpdatedAt.After(now.Add(-claimLeaseDuration()))
			if !expiredLease && !expiredLegacyClaim {
				continue
			}
			result = append(result, loop)
		}
	}
	if limit > 0 && len(result) > limit {
		return result[:limit], nil
	}
	return result, nil
}

func (r *fakeWorkflowRepo) FindExpiredOpenLoopClaimsForOwner(ownerIdentity string, now time.Time, limit int) ([]models.WorkflowOpenLoop, error) {
	if ownerIdentity == "" {
		return r.FindExpiredOpenLoopClaims(now, limit)
	}
	loops, err := r.FindExpiredOpenLoopClaims(now, 0)
	if err != nil {
		return nil, err
	}
	owned := make([]models.WorkflowOpenLoop, 0, len(loops))
	for _, loop := range loops {
		if item := r.items[loop.WorkflowID]; item != nil && item.OwnerIdentity == ownerIdentity {
			owned = append(owned, loop)
		}
	}
	if limit > 0 && len(owned) > limit {
		return owned[:limit], nil
	}
	return owned, nil
}

func (r *fakeWorkflowRepo) RecoverExpiredOpenLoopClaim(loop models.WorkflowOpenLoop, now time.Time) (*models.WorkflowOpenLoop, bool, error) {
	for workflowID, loops := range r.openLoops {
		for index := range loops {
			stored := &loops[index]
			if stored.ID != loop.ID || stored.Status != "processing" || stored.ClaimID != loop.ClaimID {
				continue
			}
			expiredLease := stored.ClaimID != "" && stored.LeaseUntil != nil && !stored.LeaseUntil.After(now)
			expiredLegacyClaim := stored.ClaimID == "" && stored.LeaseUntil == nil && !stored.UpdatedAt.After(now.Add(-claimLeaseDuration()))
			if !expiredLease && !expiredLegacyClaim {
				continue
			}
			stored.Status = "open"
			stored.ClaimID = ""
			stored.LeaseUntil = nil
			stored.UpdatedAt = now
			r.openLoops[workflowID] = loops
			copied := *stored
			return &copied, true, nil
		}
	}
	return nil, false, nil
}

func (r *fakeWorkflowRepo) CreateProposal(proposal *models.WorkflowProposal) (*models.WorkflowProposal, error) {
	if r.createProposalErr != nil {
		return nil, r.createProposalErr
	}
	if proposal.ID == uuid.Nil {
		proposal.ID = uuid.New()
	}
	now := time.Now().UTC()
	proposal.CreatedAt = now
	proposal.UpdatedAt = now
	r.proposals[proposal.WorkflowID] = append([]models.WorkflowProposal{*proposal}, r.proposals[proposal.WorkflowID]...)
	return proposal, nil
}

func (r *fakeWorkflowRepo) UpdateProposal(proposal *models.WorkflowProposal) (*models.WorkflowProposal, error) {
	proposal.UpdatedAt = time.Now().UTC()
	proposals := r.proposals[proposal.WorkflowID]
	for index := range proposals {
		if proposals[index].ID == proposal.ID {
			proposals[index] = *proposal
			r.proposals[proposal.WorkflowID] = proposals
			return proposal, nil
		}
	}
	return nil, gorm.ErrRecordNotFound
}

func (r *fakeWorkflowRepo) FindProposals(workflowID uuid.UUID) ([]models.WorkflowProposal, error) {
	return append([]models.WorkflowProposal{}, r.proposals[workflowID]...), nil
}

func (r *fakeWorkflowRepo) CreateQualityGate(gate *models.WorkflowQualityGate) (*models.WorkflowQualityGate, error) {
	if r.createQualityGateErr != nil {
		return nil, r.createQualityGateErr
	}
	if gate.ID == uuid.Nil {
		gate.ID = uuid.New()
	}
	now := time.Now().UTC()
	gate.CreatedAt = now
	gate.UpdatedAt = now
	r.qualityGate[gate.WorkflowID] = append([]models.WorkflowQualityGate{*gate}, r.qualityGate[gate.WorkflowID]...)
	return gate, nil
}

func (r *fakeWorkflowRepo) UpdateQualityGate(gate *models.WorkflowQualityGate) (*models.WorkflowQualityGate, error) {
	if r.updateQualityGateErr != nil {
		return nil, r.updateQualityGateErr
	}
	gate.UpdatedAt = time.Now().UTC()
	gates := r.qualityGate[gate.WorkflowID]
	for index := range gates {
		if gates[index].ID == gate.ID {
			gates[index] = *gate
			r.qualityGate[gate.WorkflowID] = gates
			return gate, nil
		}
	}
	return nil, gorm.ErrRecordNotFound
}

func (r *fakeWorkflowRepo) FindQualityGates(workflowID uuid.UUID) ([]models.WorkflowQualityGate, error) {
	if r.findQualityGatesErr != nil {
		return nil, r.findQualityGatesErr
	}
	return append([]models.WorkflowQualityGate{}, r.qualityGate[workflowID]...), nil
}

func (r *fakeWorkflowRepo) SaveRule(rule *models.WorkflowRule) (*models.WorkflowRule, error) {
	existing, exists := r.rules[rule.RuleKey]
	if rule.ID == uuid.Nil {
		if exists {
			rule.ID = existing.ID
		} else {
			rule.ID = uuid.New()
		}
	}
	now := time.Now().UTC()
	if rule.CreatedAt.IsZero() {
		if exists {
			rule.CreatedAt = existing.CreatedAt
		} else {
			rule.CreatedAt = now
		}
	}
	rule.UpdatedAt = now
	r.rules[rule.RuleKey] = *rule
	return rule, nil
}

func (r *fakeWorkflowRepo) FindRules() ([]models.WorkflowRule, error) {
	result := []models.WorkflowRule{}
	for _, rule := range r.rules {
		result = append(result, rule)
	}
	return result, nil
}

func (r *fakeWorkflowRepo) CreateTransition(transition *models.WorkflowTransition) (*models.WorkflowTransition, error) {
	if transition.ID == uuid.Nil {
		transition.ID = uuid.New()
	}
	transition.CreatedAt = time.Now().UTC()
	r.transitions[transition.WorkflowID] = append([]models.WorkflowTransition{*transition}, r.transitions[transition.WorkflowID]...)
	return transition, nil
}

func (r *fakeWorkflowRepo) FindTransitions(workflowID uuid.UUID) ([]models.WorkflowTransition, error) {
	return append([]models.WorkflowTransition{}, r.transitions[workflowID]...), nil
}

func (r *fakeWorkflowRepo) CreateSourceLink(link *models.WorkflowSourceLink) (*models.WorkflowSourceLink, error) {
	if r.createSourceLinkErr != nil {
		return nil, r.createSourceLinkErr
	}
	if link.ID == uuid.Nil {
		link.ID = uuid.New()
	}
	link.CreatedAt = time.Now().UTC()
	r.sourceLinks[link.WorkflowID] = append([]models.WorkflowSourceLink{*link}, r.sourceLinks[link.WorkflowID]...)
	return link, nil
}

func (r *fakeWorkflowRepo) FindSourceLinks(workflowID uuid.UUID) ([]models.WorkflowSourceLink, error) {
	if r.findSourceLinksErr != nil {
		return nil, r.findSourceLinksErr
	}
	return append([]models.WorkflowSourceLink{}, r.sourceLinks[workflowID]...), nil
}

func (r *fakeWorkflowRepo) CreateDecision(decision *models.WorkflowDecision) (*models.WorkflowDecision, error) {
	if decision.ID == uuid.Nil {
		decision.ID = uuid.New()
	}
	decision.CreatedAt = time.Now().UTC()
	r.decisions[decision.WorkflowID] = append([]models.WorkflowDecision{*decision}, r.decisions[decision.WorkflowID]...)
	r.decisionWorkflow[decision.ID] = decision.WorkflowID
	return decision, nil
}

func (r *fakeWorkflowRepo) FindDecisions(workflowID uuid.UUID) ([]models.WorkflowDecision, error) {
	return append([]models.WorkflowDecision{}, r.decisions[workflowID]...), nil
}

func (r *fakeWorkflowRepo) FindApprovalDecisionForOwner(
	ctx context.Context,
	ownerIdentity string,
	decisionID string,
) (*ApprovalDecisionRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	parsedDecisionID, err := uuid.Parse(decisionID)
	if err != nil || parsedDecisionID == uuid.Nil || decisionID != parsedDecisionID.String() {
		return nil, gorm.ErrRecordNotFound
	}
	workflowID, ok := r.decisionWorkflow[parsedDecisionID]
	if !ok {
		return nil, gorm.ErrRecordNotFound
	}
	item := r.items[workflowID]
	if item == nil || item.OwnerIdentity != ownerIdentity {
		return nil, gorm.ErrRecordNotFound
	}
	for _, decision := range r.decisions[workflowID] {
		if decision.ID != parsedDecisionID {
			continue
		}
		return &ApprovalDecisionRecord{
			DecisionID:    decision.ID.String(),
			WorkflowID:    decision.WorkflowID.String(),
			OwnerIdentity: item.OwnerIdentity,
			DecisionType:  decision.DecisionType,
			Decision:      decision.Decision,
			Reason:        decision.Reason,
			ActionBinding: decision.RuleApplied,
			Approved:      decision.Approved,
			Actor:         decision.Actor,
			CreatedAt:     decision.CreatedAt,
		}, nil
	}
	return nil, gorm.ErrRecordNotFound
}

func (r *fakeWorkflowRepo) CreateEvent(event *models.WorkflowEvent) (*models.WorkflowEvent, error) {
	if event.ID == uuid.Nil {
		event.ID = uuid.New()
	}
	event.CreatedAt = time.Now().UTC()
	r.events[event.WorkflowID] = append([]models.WorkflowEvent{*event}, r.events[event.WorkflowID]...)
	return event, nil
}

func (r *fakeWorkflowRepo) FindEvents(workflowID uuid.UUID) ([]models.WorkflowEvent, error) {
	return append([]models.WorkflowEvent{}, r.events[workflowID]...), nil
}

type fakeWorkflowMemoryService struct {
	created          []memory.CreateRequest
	retrieveRequests []memory.RetrieveRequest
	retrieveResult   *memory.RetrieveResult
	retrieveErr      error
}

func (s *fakeWorkflowMemoryService) Create(request memory.CreateRequest) (*models.ContextMemory, error) {
	s.created = append(s.created, request)
	return &models.ContextMemory{
		ID:          uuid.New(),
		ProjectKey:  request.ProjectKey,
		Kind:        request.Kind,
		Content:     request.Content,
		Summary:     request.Summary,
		SourceURI:   request.SourceURI,
		SourceLabel: request.SourceLabel,
		Confidence:  request.Confidence,
	}, nil
}

func (s *fakeWorkflowMemoryService) Update(id uuid.UUID, request memory.UpdateRequest) (*models.ContextMemory, error) {
	return &models.ContextMemory{ID: id, Content: request.Content, Kind: request.Kind}, nil
}

func (s *fakeWorkflowMemoryService) FindAll(projectKey string, includeArchived bool) ([]models.ContextMemory, error) {
	return nil, nil
}

func (s *fakeWorkflowMemoryService) FindByID(id uuid.UUID) (*models.ContextMemory, error) {
	return &models.ContextMemory{ID: id}, nil
}

func (s *fakeWorkflowMemoryService) Archive(id uuid.UUID, archived bool) (*models.ContextMemory, error) {
	return &models.ContextMemory{ID: id, Archived: archived}, nil
}

func (s *fakeWorkflowMemoryService) Delete(id uuid.UUID) error {
	return nil
}

func (s *fakeWorkflowMemoryService) Retrieve(request memory.RetrieveRequest) (*memory.RetrieveResult, error) {
	s.retrieveRequests = append(s.retrieveRequests, request)
	if s.retrieveErr != nil {
		return nil, s.retrieveErr
	}
	if s.retrieveResult != nil {
		result := *s.retrieveResult
		if result.Query == "" {
			result.Query = request.Query
		}
		if result.ProjectKey == "" {
			result.ProjectKey = request.ProjectKey
		}
		return &result, nil
	}
	return &memory.RetrieveResult{Query: request.Query, ProjectKey: request.ProjectKey}, nil
}

type fakeTaskRunner struct {
	result     *TaskRunResult
	err        error
	requests   []TaskRunRequest
	panicValue interface{}
	onRun      func(TaskRunRequest)
}

func (r *fakeTaskRunner) RunWorkflowTaskContext(ctx context.Context, request TaskRunRequest) (*TaskRunResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	request.ExecutionContext = ctx
	return r.RunWorkflowTask(request)
}

func (r *fakeTaskRunner) RunWorkflowTask(request TaskRunRequest) (*TaskRunResult, error) {
	r.requests = append(r.requests, request)
	if r.onRun != nil {
		r.onRun(request)
	}
	if r.panicValue != nil {
		panic(r.panicValue)
	}
	if r.result == nil {
		return nil, r.err
	}
	result := *r.result
	if result.FrameworkSelection == nil {
		if strings.TrimSpace(result.PlanID) == "" {
			result.PlanID = "fake-workflow-plan"
		}
		selection := testFrameworkSelection(result.PlanID)
		result.FrameworkSelection = &selection
	}
	return &result, r.err
}

type bindingTaskRunner struct {
	*fakeTaskRunner
	binding         string
	bindingErr      error
	bindingRequests []WorkflowApprovalBindingRequest
}

func (r *bindingTaskRunner) PrepareWorkflowApprovalBinding(request WorkflowApprovalBindingRequest) (string, error) {
	r.bindingRequests = append(r.bindingRequests, request)
	return r.binding, r.bindingErr
}

type selectingTaskRunner struct {
	*bindingTaskRunner
	candidates        []AutomationCandidate
	selectionErr      error
	selectionRequests []AutomationSelectionRequest
}

func (r *selectingTaskRunner) SelectWorkflowAutomations(request AutomationSelectionRequest) ([]AutomationCandidate, error) {
	r.selectionRequests = append(r.selectionRequests, request)
	return append([]AutomationCandidate(nil), r.candidates...), r.selectionErr
}
