package workflow

import (
	"context"
	"strings"
	"testing"

	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/plangraph"

	"github.com/google/uuid"
)

type workflowAcceptedPlanResolverStub struct {
	binding        *plangraph.AcceptedRevisionBinding
	err            error
	historyBinding *plangraph.AcceptedRevisionBinding
	historyErr     error
	owner          string
	historyOwner   string
}

func (stub *workflowAcceptedPlanResolverStub) ResolveAcceptedRevision(_ context.Context, owner string, _ plangraph.AcceptedRevisionReference) (*plangraph.AcceptedRevisionBinding, error) {
	stub.historyOwner = owner
	if stub.historyBinding != nil || stub.historyErr != nil {
		return stub.historyBinding, stub.historyErr
	}
	return stub.binding, stub.err
}

func (stub *workflowAcceptedPlanResolverStub) ResolveAccepted(_ context.Context, owner string, _ plangraph.AcceptedRevisionReference) (*plangraph.AcceptedRevisionBinding, error) {
	stub.owner = owner
	return stub.binding, stub.err
}

func workflowPlanReference(digest string) plangraph.AcceptedRevisionReference {
	return plangraph.AcceptedRevisionReference{
		PlanID:   uuid.MustParse("bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"),
		Revision: 4, Digest: digest, NodeID: "workflow-node",
	}
}

func TestWorkflowCoordinationBindingPersistsAndResolvesForOwner(t *testing.T) {
	reference := workflowPlanReference(strings.Repeat("b", 64))
	resolver := &workflowAcceptedPlanResolverStub{binding: &plangraph.AcceptedRevisionBinding{
		PlanID: reference.PlanID, Revision: reference.Revision, Digest: reference.Digest,
		NodeID: reference.NodeID, Node: plangraph.Node{ID: reference.NodeID}, CanExecute: false,
	}}
	base := NewService(newFakeWorkflowRepo())
	configured, err := WithAcceptedPlanResolver(base, resolver)
	if err != nil {
		t.Fatalf("configure resolver: %v", err)
	}
	service := configured.(*service)
	binding, err := service.resolveAcceptedCoordinationPlan("owner-a", reference)
	if err != nil {
		t.Fatalf("resolve binding: %v", err)
	}
	item := &models.WorkflowItem{OwnerIdentity: "owner-a"}
	applyWorkflowCoordinationBinding(item, binding)
	if got := workflowCoordinationReference(*item); got != reference {
		t.Fatalf("workflow binding did not round trip: got=%+v want=%+v", got, reference)
	}
	if resolver.owner != "owner-a" || binding.CanExecute {
		t.Fatalf("binding was not owner-scoped advisory evidence: owner=%q binding=%+v", resolver.owner, binding)
	}
}

func TestWorkflowCoordinationBindingFailsClosedWhenConfiguredReferenceCannotBeValidated(t *testing.T) {
	service := NewService(newFakeWorkflowRepo()).(*service)
	reference := workflowPlanReference(strings.Repeat("b", 64))
	if _, err := service.resolveAcceptedCoordinationPlan("owner-a", reference); err == nil || !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("missing resolver should fail closed, got %v", err)
	}
	service.acceptedPlanResolver = &workflowAcceptedPlanResolverStub{binding: &plangraph.AcceptedRevisionBinding{CanExecute: true}}
	if _, err := service.resolveAcceptedCoordinationPlan("owner-a", reference); err == nil || !strings.Contains(err.Error(), "advisory-only") {
		t.Fatalf("executable binding should fail closed, got %v", err)
	}
}

func TestWorkflowSourceRevisionBindsCoordinationPlan(t *testing.T) {
	request := IntakeRequest{SourceType: "email", SourceID: "message-1", Input: "prepare reply"}
	first := workflowSourceRevision(request, request.Input)
	request.CoordinationPlan = workflowPlanReference(strings.Repeat("b", 64))
	second := workflowSourceRevision(request, request.Input)
	if first == second {
		t.Fatal("source revision did not bind accepted coordination revision")
	}
	request.CoordinationPlan.Digest = strings.Repeat("c", 64)
	third := workflowSourceRevision(request, request.Input)
	if second == third {
		t.Fatal("source revision did not change with coordination digest")
	}
}

func TestWorkflowSourceRevisionBindsRoutingContext(t *testing.T) {
	request := IntakeRequest{SourceType: "trello", SourceID: "card-1", Input: "run the approved task"}
	base := workflowSourceRevision(request, request.Input, `{"Required":true,"Candidates":[{"ID":"automation-a"}]}`)
	request.ProjectKeyHint = "project-a"
	projectChanged := workflowSourceRevision(request, request.Input, `{"Required":true,"Candidates":[{"ID":"automation-a"}]}`)
	if base == projectChanged {
		t.Fatal("source revision did not bind the project routing hint")
	}
	request.ProjectKeyHint = ""
	selectionChanged := workflowSourceRevision(request, request.Input, `{"Required":true,"Candidates":[{"ID":"automation-b"}]}`)
	if base == selectionChanged {
		t.Fatal("source revision did not bind the automation selection proposal")
	}
}

func TestWorkflowSourceRevisionBindsMandate(t *testing.T) {
	request := IntakeRequest{SourceType: "trello", SourceID: "card-1", Input: "run the approved task"}
	mandateA := uuid.New()
	mandateB := uuid.New()
	request.MandateID = mandateA.String()
	first := workflowSourceRevision(request, request.Input)
	request.MandateID = mandateB.String()
	second := workflowSourceRevision(request, request.Input)
	if first == second {
		t.Fatal("source revision did not bind the standing mandate")
	}
}

func TestEvidenceClaimsRequireAnAllowedExactStatus(t *testing.T) {
	for _, test := range []struct {
		status string
		want   bool
	}{
		{status: "source_linked", want: false},
		{status: " VERIFIED ", want: false},
		{status: "human_approved", want: false},
		{status: "unsupported", want: true},
		{status: "not_verified", want: true},
		{status: "source_linked_unverified", want: true},
		{status: "", want: true},
	} {
		claim := models.WorkflowEvidenceClaim{SourceURI: "https://example.test/evidence", Status: test.status}
		if got := hasEvidenceNeedingReview([]models.WorkflowEvidenceClaim{claim}); got != test.want {
			t.Errorf("hasEvidenceNeedingReview(%q) = %v, want %v", test.status, got, test.want)
		}
	}
}

func TestAuthorizedEffectRecoveryUsesHistoricalPlanWithoutGrantingExecution(t *testing.T) {
	reference := workflowPlanReference(strings.Repeat("d", 64))
	historical := &plangraph.AcceptedRevisionBinding{
		PlanID: reference.PlanID, Revision: reference.Revision, Digest: reference.Digest,
		NodeID: reference.NodeID, Node: plangraph.Node{ID: reference.NodeID}, CanExecute: false,
	}
	resolver := &workflowAcceptedPlanResolverStub{
		err: plangraph.ErrReferenceStale, historyBinding: historical,
	}
	configured, err := WithAcceptedPlanResolver(NewService(newFakeWorkflowRepo()), resolver)
	if err != nil {
		t.Fatal(err)
	}
	receiptID := uuid.New()
	request := IntakeRequest{
		OwnerIdentity: "owner-a", Input: "Recover the exact consumed local effect",
		SourceType: "portfolio_workflow_effect", SourceID: receiptID.String(),
		SourceURI:   "hai://execution-authorization-receipts/" + receiptID.String(),
		SourceLabel: "Consumed authorization receipt", ContentType: "portfolio_workflow_effect",
		Trigger: "portfolio_workflow_effect", Actor: "owner-a", RequiresReview: true,
		ReviewReason:     "Receipt recovery creates a review-gated workflow only.",
		CoordinationPlan: reference,
	}
	if _, err := configured.Intake(request); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("ordinary intake must reject stale plan provenance, got %v", err)
	}
	recovery := configured.(AuthorizedEffectRecoveryIntake)
	record, err := recovery.IntakeAuthorizedEffectRecovery(request)
	if err != nil {
		t.Fatalf("historical authorized-effect recovery: %v", err)
	}
	if record.Item.CurrentState != StateNeedsApproval || !record.Item.RequiresApproval || record.Item.ApprovalStatus != "pending" {
		t.Fatalf("recovered workflow bypassed review: %#v", record.Item)
	}
	if got := workflowCoordinationReference(record.Item); got != reference {
		t.Fatalf("historical plan provenance was not preserved: got=%#v want=%#v", got, reference)
	}
	if resolver.historyOwner != "owner-a" || historical.CanExecute {
		t.Fatalf("historical resolution crossed owner/authority boundary: owner=%q binding=%#v", resolver.historyOwner, historical)
	}
}
