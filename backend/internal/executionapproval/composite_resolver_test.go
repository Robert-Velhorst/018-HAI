package executionapproval

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/executionauth"

	"github.com/google/uuid"
)

func TestCompositeResolverRoutesEachReferenceToExactlyOneResolver(t *testing.T) {
	digest := strings.Repeat("5", 64)
	taskApproval := executionauth.ResolvedApproval{SourceID: taskReviewPrefix + uuid.NewString()}
	workflowApproval := executionauth.ResolvedApproval{SourceID: workflowDecisionPrefix + uuid.NewString()}
	taskSpy := &approvalResolverSpy{result: taskApproval}
	workflowSpy := &approvalResolverSpy{result: workflowApproval}
	portfolioSpy := &approvalResolverSpy{result: executionauth.ResolvedApproval{
		SourceID: portfolioDecisionPrefix + uuid.NewString(),
	}}
	composite, err := NewCompositeResolver(taskSpy, workflowSpy, portfolioSpy)
	if err != nil {
		t.Fatalf("NewCompositeResolver: %v", err)
	}

	result, err := composite.Resolve(context.Background(), "alice", taskApproval.SourceID, digest)
	if err != nil || result.SourceID != taskApproval.SourceID {
		t.Fatalf("task result = %#v, %v", result, err)
	}
	if taskSpy.calls != 1 || workflowSpy.calls != 0 || portfolioSpy.calls != 0 {
		t.Fatalf("task calls=%d workflow calls=%d portfolio calls=%d", taskSpy.calls, workflowSpy.calls, portfolioSpy.calls)
	}

	result, err = composite.Resolve(context.Background(), "alice", workflowApproval.SourceID, digest)
	if err != nil || result.SourceID != workflowApproval.SourceID {
		t.Fatalf("workflow result = %#v, %v", result, err)
	}
	if taskSpy.calls != 1 || workflowSpy.calls != 1 || portfolioSpy.calls != 0 {
		t.Fatalf("task calls=%d workflow calls=%d portfolio calls=%d", taskSpy.calls, workflowSpy.calls, portfolioSpy.calls)
	}

	result, err = composite.Resolve(context.Background(), "alice", portfolioSpy.result.SourceID, digest)
	if err != nil || result.SourceID != portfolioSpy.result.SourceID || portfolioSpy.calls != 1 {
		t.Fatalf("portfolio result = %#v, %v calls=%d", result, err, portfolioSpy.calls)
	}
}

func TestCompositeResolverNeverFallsBackAfterSelectedResolverRejects(t *testing.T) {
	digest := strings.Repeat("6", 64)
	taskSpy := &approvalResolverSpy{err: ErrBindingMismatch}
	workflowSpy := &approvalResolverSpy{result: executionauth.ResolvedApproval{
		SourceID: taskReviewPrefix + uuid.NewString(),
	}}
	portfolioSpy := &approvalResolverSpy{}
	composite, err := NewCompositeResolver(taskSpy, workflowSpy, portfolioSpy)
	if err != nil {
		t.Fatalf("NewCompositeResolver: %v", err)
	}

	_, err = composite.Resolve(
		context.Background(),
		"alice",
		taskReviewPrefix+uuid.NewString(),
		digest,
	)
	if !errors.Is(err, ErrBindingMismatch) {
		t.Fatalf("task rejection error = %v", err)
	}
	if taskSpy.calls != 1 || workflowSpy.calls != 0 || portfolioSpy.calls != 0 {
		t.Fatalf("fallback occurred: task=%d workflow=%d", taskSpy.calls, workflowSpy.calls)
	}

	taskSpy.calls = 0
	workflowSpy.err = ErrWorkflowBindingMismatch
	workflowSpy.result = executionauth.ResolvedApproval{}
	_, err = composite.Resolve(
		context.Background(),
		"alice",
		workflowDecisionPrefix+uuid.NewString(),
		digest,
	)
	if !errors.Is(err, ErrWorkflowBindingMismatch) {
		t.Fatalf("workflow rejection error = %v", err)
	}
	if taskSpy.calls != 0 || workflowSpy.calls != 1 || portfolioSpy.calls != 0 {
		t.Fatalf("reverse fallback occurred: task=%d workflow=%d", taskSpy.calls, workflowSpy.calls)
	}
}

func TestCompositeResolverPreservesTaskReviewResolverValidation(t *testing.T) {
	now := time.Now().UTC()
	reviewID := uuid.New()
	taskDecision := validDecision(reviewID, "alice", now)
	taskRepository := &stubDecisionRepository{decision: &taskDecision}
	taskResolver := testResolver(t, taskRepository, now)
	workflowSpy := &approvalResolverSpy{}
	portfolioSpy := &approvalResolverSpy{}
	composite, err := NewCompositeResolver(taskResolver, workflowSpy, portfolioSpy)
	if err != nil {
		t.Fatalf("NewCompositeResolver: %v", err)
	}

	approval, err := composite.Resolve(
		context.Background(),
		"alice",
		taskDecision.ApprovalSourceID,
		taskDecision.RequestDigest,
	)
	if err != nil || approval.DecisionID != taskDecision.ID {
		t.Fatalf("task approval = %#v, %v", approval, err)
	}
	if workflowSpy.calls != 0 || portfolioSpy.calls != 0 {
		t.Fatalf("workflow resolver called %d times", workflowSpy.calls)
	}

	_, err = composite.Resolve(
		context.Background(),
		"alice",
		taskDecision.ApprovalSourceID,
		strings.Repeat("7", 64),
	)
	if !errors.Is(err, ErrBindingMismatch) {
		t.Fatalf("task binding error = %v", err)
	}
	if workflowSpy.calls != 0 {
		t.Fatalf("workflow fallback weakened task rejection")
	}
}

func TestCompositeResolverPreservesWorkflowResolverValidation(t *testing.T) {
	now := time.Now().UTC()
	digest := strings.Repeat("8", 64)
	decision := validWorkflowDecision("alice", digest, now)
	workflowResolver := testWorkflowResolver(
		t,
		&stubWorkflowApprovalRepository{decision: &decision, ownerScoped: true},
		now,
	)
	taskSpy := &approvalResolverSpy{}
	portfolioSpy := &approvalResolverSpy{}
	composite, err := NewCompositeResolver(taskSpy, workflowResolver, portfolioSpy)
	if err != nil {
		t.Fatalf("NewCompositeResolver: %v", err)
	}

	approval, err := composite.Resolve(
		context.Background(),
		"alice",
		workflowDecisionPrefix+decision.DecisionID,
		digest,
	)
	if err != nil || approval.DecisionID != decision.DecisionID {
		t.Fatalf("workflow approval = %#v, %v", approval, err)
	}
	if taskSpy.calls != 0 || portfolioSpy.calls != 0 {
		t.Fatalf("task resolver called %d times", taskSpy.calls)
	}

	_, err = composite.Resolve(
		context.Background(),
		"bob",
		workflowDecisionPrefix+decision.DecisionID,
		digest,
	)
	if !errors.Is(err, ErrWorkflowApprovalUnavailable) {
		t.Fatalf("workflow owner error = %v", err)
	}
	if taskSpy.calls != 0 {
		t.Fatalf("task fallback weakened workflow rejection")
	}
}

func TestCompositeResolverRejectsUnsupportedReferenceWithoutDelegation(t *testing.T) {
	taskSpy := &approvalResolverSpy{}
	workflowSpy := &approvalResolverSpy{}
	portfolioSpy := &approvalResolverSpy{}
	composite, err := NewCompositeResolver(taskSpy, workflowSpy, portfolioSpy)
	if err != nil {
		t.Fatalf("NewCompositeResolver: %v", err)
	}
	_, err = composite.Resolve(
		context.Background(),
		"alice",
		"caller-proof:"+uuid.NewString(),
		strings.Repeat("9", 64),
	)
	if !errors.Is(err, ErrUnsupportedApprovalReference) {
		t.Fatalf("unsupported reference error = %v", err)
	}
	if taskSpy.calls != 0 || workflowSpy.calls != 0 || portfolioSpy.calls != 0 {
		t.Fatalf("unsupported reference was delegated")
	}
}

func TestCompositeResolverRoutesRegisteredApprovalNamespace(t *testing.T) {
	digest := strings.Repeat("b", 64)
	taskSpy := &approvalResolverSpy{}
	workflowSpy := &approvalResolverSpy{}
	portfolioSpy := &approvalResolverSpy{}
	controlSpy := &approvalResolverSpy{result: executionauth.ResolvedApproval{
		SourceID: "control-decision:" + uuid.NewString(),
	}}
	composite, err := NewCompositeResolver(
		taskSpy,
		workflowSpy,
		portfolioSpy,
		RegisterApprovalResolver("control-decision:", controlSpy),
	)
	if err != nil {
		t.Fatalf("NewCompositeResolver: %v", err)
	}
	result, err := composite.Resolve(
		context.Background(), "alice", controlSpy.result.SourceID, digest,
	)
	if err != nil || result.SourceID != controlSpy.result.SourceID || controlSpy.calls != 1 {
		t.Fatalf("control result = %#v, %v calls=%d", result, err, controlSpy.calls)
	}
	if taskSpy.calls != 0 || workflowSpy.calls != 0 || portfolioSpy.calls != 0 {
		t.Fatal("registered namespace fell through to canonical resolvers")
	}
}

func TestCompositeResolverValidatesConstructionAndState(t *testing.T) {
	spy := &approvalResolverSpy{}
	var typedNil *approvalResolverSpy
	if _, err := NewCompositeResolver(nil, spy, spy); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("nil task resolver error = %v", err)
	}
	if _, err := NewCompositeResolver(typedNil, spy, spy); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("typed nil task resolver error = %v", err)
	}
	if _, err := NewCompositeResolver(spy, nil, spy); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("nil workflow resolver error = %v", err)
	}
	if _, err := NewCompositeResolver(spy, typedNil, spy); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("typed nil workflow resolver error = %v", err)
	}
	if _, err := NewCompositeResolver(spy, spy, nil); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("nil portfolio resolver error = %v", err)
	}
	if _, err := NewCompositeResolver(spy, spy, typedNil); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("typed nil portfolio resolver error = %v", err)
	}
	var resolver *CompositeResolver
	if _, err := resolver.Resolve(
		context.Background(),
		"alice",
		taskReviewPrefix+uuid.NewString(),
		strings.Repeat("a", 64),
	); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("nil composite error = %v", err)
	}
}

type approvalResolverSpy struct {
	result        executionauth.ResolvedApproval
	err           error
	calls         int
	owner         string
	sourceID      string
	bindingDigest string
}

func (r *approvalResolverSpy) Resolve(
	_ context.Context,
	ownerIdentity string,
	sourceID string,
	bindingDigest string,
) (executionauth.ResolvedApproval, error) {
	r.calls++
	r.owner = ownerIdentity
	r.sourceID = sourceID
	r.bindingDigest = bindingDigest
	return r.result, r.err
}
