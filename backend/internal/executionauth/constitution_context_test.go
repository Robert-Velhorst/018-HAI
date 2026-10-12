package executionauth

import (
	"context"
	"errors"
	"testing"

	"automation-hub-backend/internal/frameworkregistry"
)

type contextConstitutionEvaluator struct {
	contexts []context.Context
	base     *fakeConstitutionEvaluator
}

func (e *contextConstitutionEvaluator) EvaluateExecutionPolicy(ctx context.Context, owner string, capabilities []string, authority int) (ConstitutionDecision, error) {
	e.contexts = append(e.contexts, ctx)
	if err := ctx.Err(); err != nil {
		return ConstitutionDecision{}, err
	}
	return e.base.EvaluateExecutionPolicy(ctx, owner, capabilities, authority)
}

func TestExecutionAuthorizationAndRecheckUseOriginalPolicyContext(t *testing.T) {
	evaluator := &contextConstitutionEvaluator{base: permissiveConstitution()}
	service := newTestService(t, NewMemoryRepository(), evaluator, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	receipt, err := service.AuthorizeAndConsume(ctx, baseRequest("original-policy-context"), "worker", "local-file")
	if err != nil || receipt.Outcome != OutcomeAuthorized {
		t.Fatalf("authorization: %+v / %v", receipt, err)
	}
	if len(evaluator.contexts) != 2 || evaluator.contexts[0] != ctx || evaluator.contexts[1] != ctx {
		t.Fatalf("policy did not retain context for initial and final checks: %v", evaluator.contexts)
	}
}

func TestConstitutionAdapterRejectsCanceledAndNilContext(t *testing.T) {
	frameworks, err := frameworkregistry.NewService(frameworkregistry.NewMemoryRepository())
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := NewConstitutionPolicyAdapter(frameworks)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.EvaluateExecutionPolicy(nil, "robert", []string{"local-execution"}, 1); err == nil {
		t.Fatal("nil context accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := adapter.EvaluateExecutionPolicy(ctx, "robert", []string{"local-execution"}, 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled policy lookup: %v", err)
	}
}
