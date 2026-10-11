package task

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/plangraph"

	"github.com/google/uuid"
)

type acceptedPlanResolverStub struct {
	binding *plangraph.AcceptedRevisionBinding
	err     error
	owner   string
}

type cancellationAwareAcceptedPlanResolver struct{}

func (cancellationAwareAcceptedPlanResolver) ResolveAccepted(ctx context.Context, _ string, _ plangraph.AcceptedRevisionReference) (*plangraph.AcceptedRevisionBinding, error) {
	return nil, ctx.Err()
}

func (stub *acceptedPlanResolverStub) ResolveAccepted(_ context.Context, owner string, _ plangraph.AcceptedRevisionReference) (*plangraph.AcceptedRevisionBinding, error) {
	stub.owner = owner
	return stub.binding, stub.err
}

func taskPlanReference() plangraph.AcceptedRevisionReference {
	return plangraph.AcceptedRevisionReference{
		PlanID:   uuid.MustParse("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"),
		Revision: 2, Digest: strings.Repeat("a", 64), NodeID: "task-node",
	}
}

func TestResolveCoordinationPlanIsOwnerScopedAdvisoryAndBindingAware(t *testing.T) {
	base := NewService(nil, nil)
	service := base.(*service)
	resolver := &acceptedPlanResolverStub{binding: &plangraph.AcceptedRevisionBinding{
		PlanID: taskPlanReference().PlanID, Revision: 2, Digest: strings.Repeat("a", 64), NodeID: "task-node",
		Node:       plangraph.Node{ID: "task-node", Bindings: plangraph.Bindings{PursuitID: "pursuit-1", WorkflowID: "workflow-1"}},
		AcceptedAt: time.Now().UTC(), CanExecute: false,
	}}
	configured, err := WithAcceptedPlanResolver(base, resolver)
	if err != nil || configured != service {
		t.Fatalf("configure resolver: service=%T err=%v", configured, err)
	}
	binding, err := service.resolveCoordinationPlan(IntakeRequest{
		OwnerIdentity: "owner-a", PursuitID: "pursuit-1", WorkflowID: "workflow-1",
		CoordinationPlan: taskPlanReference(),
	})
	if err != nil {
		t.Fatalf("resolve coordination plan: %v", err)
	}
	if binding == nil || binding.CanExecute || resolver.owner != "owner-a" {
		t.Fatalf("unexpected owner-scoped advisory binding: binding=%+v owner=%q", binding, resolver.owner)
	}
}

func TestResolveCoordinationPlanFailsClosedForUnavailableOrMismatchedBinding(t *testing.T) {
	service := NewService(nil, nil).(*service)
	request := IntakeRequest{OwnerIdentity: "owner-a", PursuitID: "pursuit-1", CoordinationPlan: taskPlanReference()}
	if _, err := service.resolveCoordinationPlan(request); err == nil || !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("missing resolver should fail closed, got %v", err)
	}
	service.acceptedPlanResolver = &acceptedPlanResolverStub{binding: &plangraph.AcceptedRevisionBinding{
		PlanID: taskPlanReference().PlanID, Revision: 2, Digest: strings.Repeat("a", 64), NodeID: "task-node",
		Node: plangraph.Node{ID: "task-node", Bindings: plangraph.Bindings{PursuitID: "other"}},
	}}
	if _, err := service.resolveCoordinationPlan(request); err == nil || !strings.Contains(err.Error(), "not bound") {
		t.Fatalf("mismatched pursuit binding should fail closed, got %v", err)
	}
	service.acceptedPlanResolver = &acceptedPlanResolverStub{binding: &plangraph.AcceptedRevisionBinding{CanExecute: true}}
	if _, err := service.resolveCoordinationPlan(request); err == nil || !strings.Contains(err.Error(), "advisory-only") {
		t.Fatalf("executable plan invariant should fail closed, got %v", err)
	}
}

func TestResolveCoordinationPlanAllowsAbsentOptionalReference(t *testing.T) {
	service := NewService(nil, nil).(*service)
	binding, err := service.resolveCoordinationPlan(IntakeRequest{OwnerIdentity: "owner-a"})
	if err != nil || binding != nil {
		t.Fatalf("absent optional reference should preserve existing behavior: binding=%+v err=%v", binding, err)
	}
}

func TestResolveCoordinationPlanPropagatesTaskCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	service := NewService(nil, nil).(*service)
	service.acceptedPlanResolver = cancellationAwareAcceptedPlanResolver{}

	_, err := service.resolveCoordinationPlan(IntakeRequest{
		OwnerIdentity:    "owner-a",
		CoordinationPlan: taskPlanReference(),
		executionContext: ctx,
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("resolve coordination plan error = %v, want context canceled", err)
	}
}
